package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type userConcurrencyResponse struct {
	Limit       int    `json:"limit"`
	Used        any    `json:"used"`
	Queued      any    `json:"queued"`
	Remaining   any    `json:"remaining"`
	Unlimited   bool   `json:"unlimited"`
	Status      string `json:"status"`
	AsOf        int64  `json:"as_of"`
	Source      string `json:"source"`
	Current     any    `json:"current,omitempty"`
	AdminExempt bool   `json:"admin_exempt"`
}

type userConcurrencyMutationRequest struct {
	Source string          `json:"source"`
	Limit  json.RawMessage `json:"limit"`
}

type userConcurrencyBatchRequest struct {
	UserIDs []int           `json:"user_ids"`
	Source  string          `json:"source"`
	Limit   json.RawMessage `json:"limit"`
}

func decodeStrictJSON(c *gin.Context, value any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func parseConcurrencyLimit(raw json.RawMessage, required bool) (int, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" {
		if required {
			return 0, errors.New("limit is required")
		}
		return common.GetUserConcurrencyDefault(), nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || strconv.Itoa(parsed) != value || parsed < 0 || parsed > common.MaxUserConcurrency {
		return 0, fmt.Errorf("concurrency must be an integer between 0 and %d", common.MaxUserConcurrency)
	}
	return parsed, nil
}

func parseConcurrencyMutation(raw json.RawMessage, source string) (int, bool, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if source != "default" && source != "custom" {
		return 0, false, errors.New("source must be default or custom")
	}
	limit, err := parseConcurrencyLimit(raw, source == "custom")
	if err != nil {
		return 0, false, err
	}
	if source == "default" {
		return common.GetUserConcurrencyDefault(), false, nil
	}
	return limit, true, nil
}

func writeConcurrencyError(c *gin.Context, err error) {
	common.ApiErrorMsg(c, err.Error())
}

func concurrencyResponse(user *model.User, status service.UserConcurrencyStatus, statusErr error) userConcurrencyResponse {
	response := userConcurrencyResponse{
		Limit:       status.Limit,
		Unlimited:   status.Unlimited,
		Status:      status.Status,
		AsOf:        status.AsOf,
		Source:      user.ConcurrencySource(),
		AdminExempt: user.Role >= common.RoleAdminUser,
	}
	if user.Role >= common.RoleAdminUser {
		response.Status = "exempt"
		response.Source = "admin_exempt"
		response.Used = 0
		response.Queued = 0
		response.Remaining = nil
		response.Current = 0
		return response
	}
	if statusErr != nil {
		response.Status = "unavailable"
		response.Used = nil
		response.Queued = nil
		response.Remaining = nil
		response.Current = nil
		return response
	}
	response.Used = status.Used
	response.Queued = status.Queued
	response.Remaining = status.Remaining
	response.Current = status.Used
	return response
}

func userConcurrencyStatus(c *gin.Context, user *model.User) userConcurrencyResponse {
	limit := user.EffectiveConcurrencyLimit()
	status, err := service.GetUserConcurrencyStatus(c.Request.Context(), user.Id, limit)
	return concurrencyResponse(user, status, err)
}

func fillUserConcurrencyStatuses(c *gin.Context, users []*model.User) {
	limits := make(map[int]int, len(users))
	for _, user := range users {
		if user == nil || user.Id <= 0 {
			continue
		}
		user.ConcurrencySourceValue = user.ConcurrencySource()
		if user.Role < common.RoleAdminUser {
			limits[user.Id] = user.EffectiveConcurrencyLimit()
		}
	}
	statuses, err := service.GetUsersConcurrencyStatus(c.Request.Context(), limits)
	for _, user := range users {
		if user == nil || user.Id <= 0 {
			continue
		}
		limit := user.EffectiveConcurrencyLimit()
		status := service.UserConcurrencyStatus{Limit: limit, Unlimited: limit == 0}
		statusErr := err
		if user.Role >= common.RoleAdminUser {
			statusErr = nil
		}
		if statusValue, ok := statuses[user.Id]; ok {
			status = statusValue
		}
		response := concurrencyResponse(user, status, statusErr)
		effectiveLimit := response.Limit
		user.EffectiveConcurrency = &effectiveLimit
		if used, ok := response.Used.(int); ok {
			user.CurrentConcurrency = &used
		}
		if queued, ok := response.Queued.(int); ok {
			user.QueuedConcurrency = &queued
		}
		if remaining, ok := response.Remaining.(int); ok {
			user.RemainingConcurrency = &remaining
		}
		user.ConcurrencySourceValue = response.Source
		user.ConcurrencyStatus = response.Status
	}
}

func GetSelfConcurrency(c *gin.Context) {
	user, err := model.GetSelfUserById(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, userConcurrencyStatus(c, user))
}

func UpdateUserConcurrency(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		writeConcurrencyError(c, errors.New("invalid user id"))
		return
	}
	var request userConcurrencyMutationRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeConcurrencyError(c, err)
		return
	}
	limit, override, err := parseConcurrencyMutation(request.Limit, request.Source)
	if err != nil {
		writeConcurrencyError(c, err)
		return
	}
	target, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !canManageTargetRole(c.GetInt("role"), target.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}
	oldLimit := target.EffectiveConcurrencyLimit()
	oldSource := target.ConcurrencySource()
	updated, err := model.UpdateUserConcurrency(id, limit, override)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, id, "user.concurrency_update", map[string]any{
		"id":         id,
		"username":   target.Username,
		"old_limit":  oldLimit,
		"new_limit":  updated.EffectiveConcurrencyLimit(),
		"old_source": oldSource,
		"source":     updated.ConcurrencySource(),
	})
	common.ApiSuccess(c, userConcurrencyStatus(c, updated))
}

func BatchUpdateUserConcurrency(c *gin.Context) {
	var request userConcurrencyBatchRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeConcurrencyError(c, err)
		return
	}
	if len(request.UserIDs) == 0 {
		writeConcurrencyError(c, errors.New("user_ids must not be empty"))
		return
	}
	seen := make(map[int]struct{}, len(request.UserIDs))
	ids := make([]int, 0, len(request.UserIDs))
	for _, id := range request.UserIDs {
		if id <= 0 {
			writeConcurrencyError(c, errors.New("user_ids must contain positive integers"))
			return
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	limit, override, err := parseConcurrencyMutation(request.Limit, request.Source)
	if err != nil {
		writeConcurrencyError(c, err)
		return
	}
	var users []model.User
	if err := model.DB.Where("id IN ?", ids).Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if len(users) != len(ids) {
		common.ApiError(c, gorm.ErrRecordNotFound)
		return
	}
	for _, user := range users {
		if !canManageTargetRole(c.GetInt("role"), user.Role) {
			common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
			return
		}
	}
	if err := model.UpdateUsersConcurrency(ids, limit, override); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "user.concurrency_batch_update", map[string]any{
		"user_count": len(ids),
		"user_ids":   ids,
		"limit":      limit,
		"source":     map[bool]string{true: "custom", false: "default"}[override],
	})
	common.ApiSuccess(c, gin.H{"updated": len(ids), "limit": limit, "source": map[bool]string{true: "custom", false: "default"}[override]})
}

func ResetAllUserConcurrency(c *gin.Context) {
	limit := common.GetUserConcurrencyDefault()
	ids, err := model.ResetUserConcurrencyToDefault(limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "user.concurrency_reset", map[string]any{
		"user_count": len(ids),
		"limit":      limit,
	})
	common.ApiSuccess(c, gin.H{"updated": len(ids), "limit": limit, "source": "default"})
}

func applyUserConcurrencyStatus(c *gin.Context, users []*model.User) {
	fillUserConcurrencyStatuses(c, users)
}

func sortUserIDs(ids []int) []int {
	result := append([]int(nil), ids...)
	sort.Ints(result)
	return result
}
