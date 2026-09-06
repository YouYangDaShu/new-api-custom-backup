package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"gorm.io/gorm"
)

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		condition, pattern, err := buildLogLikeCondition(column, value)
		if err != nil {
			return nil, err
		}
		return tx.Where(condition, pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

func buildLogLikeCondition(column string, value string) (string, string, error) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		pattern, err := sanitizeClickHouseLikePattern(value)
		if err != nil {
			return "", "", err
		}
		return column + " LIKE ?", pattern, nil
	}

	pattern, err := sanitizeLikePattern(value)
	if err != nil {
		return "", "", err
	}
	return column + " LIKE ? ESCAPE '!'", pattern, nil
}

func sanitizeClickHouseLikePattern(input string) (string, error) {
	input = strings.ReplaceAll(input, `\`, `\\`)
	input = strings.ReplaceAll(input, `_`, `\_`)

	if err := validateLikePattern(input); err != nil {
		return "", err
	}
	return input, nil
}

type Log struct {
	Id                int    `json:"id" gorm:"index:idx_created_at_id,priority:2;index:idx_user_id_id,priority:2"`
	UserId            int    `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:1;index:idx_created_at_type"`
	FinishedAt        int64  `json:"finished_at" gorm:"bigint;default:0;index"`
	LastActivityAt    int64  `json:"-" gorm:"bigint;index"`
	Type              int    `json:"type" gorm:"index:idx_created_at_type"`
	Content           string `json:"content"`
	Username          string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName         string `json:"token_name" gorm:"index;default:''"`
	ModelName         string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota             int    `json:"quota" gorm:"default:0"`
	PromptTokens      int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens  int    `json:"completion_tokens" gorm:"default:0"`
	UseTime           int    `json:"use_time" gorm:"default:0"`
	IsStream          bool   `json:"is_stream"`
	ChannelId         int    `json:"channel" gorm:"index"`
	ChannelName       string `json:"channel_name" gorm:"->"`
	TokenId           int    `json:"token_id" gorm:"default:0;index"`
	Group             string `json:"group" gorm:"index"`
	Ip                string `json:"ip" gorm:"index;default:''"`
	RequestId         string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	UpstreamRequestId string `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_id;default:''"`
	Other             string `json:"other"`
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown    = 0
	LogTypeTopup      = 1
	LogTypeConsume    = 2
	LogTypeManage     = 3
	LogTypeSystem     = 4
	LogTypeError      = 5
	LogTypeRefund     = 6
	LogTypeLogin      = 7
	LogTypeProcessing = 8 // in-flight request lifecycle row (updated on finish)
)

func ensureLogRequestId(log *Log) {
	if log != nil && log.RequestId == "" {
		log.RequestId = common.NewRequestId()
	}
}

func createLog(log *Log) error {
	ensureLogRequestId(log)
	if log.FinishedAt == 0 && log.Type != LogTypeProcessing {
		log.FinishedAt = common.GetTimestamp()
	}
	return LOG_DB.Create(log).Error
}

const staleProcessingLogTimeout = 15 * time.Minute

func processingLogTimeout() time.Duration {
	minutes := common.ProcessingLogTimeoutMinutes
	if minutes < 1 {
		minutes = 15
	}
	return time.Duration(minutes) * time.Minute
}

// CloseStaleProcessingLogs is intentionally a no-op in the F3-based candidate.
// Automatic cleanup is excluded so interrupted requests keep F3's direct
// billing/refund lifecycle and are never reclassified into a deferred state.
func CloseStaleProcessingLogs(now time.Time) (int64, error) {
	return 0, nil
}

func clickHouseLogOrder(prefix string) string {
	return prefix + "created_at desc, " + prefix + "request_id desc"
}

func assignDisplayLogIds(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].Id = startIdx + i + 1
	}
}

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			// Remove operation-audit details (operator/route info), admin-only.
			delete(otherMap, "audit_info")
			// delete(otherMap, "reject_reason")
			delete(otherMap, "stream_status")
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
	}
	assignDisplayLogIds(logs, startIdx)
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	order := "id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("")
	}
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order(order).Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0)
	return logs, err
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordLogWithAdminInfo 记录操作日志，并将管理员相关信息存入 Other.admin_info，
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo map[string]interface{}) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(adminInfo) > 0 {
		other := map[string]interface{}{
			"admin_info": adminInfo,
		}
		log.Other = common.MapToJsonStr(other)
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// buildOpField 构建语言无关的操作描述（写入 Other.op）。
// 前端依据 action(稳定操作标识) + params(结构化参数) 在渲染期用 i18n 本地化展示，
// 因此不在数据库中存储自然语言句子。
func buildOpField(action string, params map[string]interface{}) map[string]interface{} {
	op := map[string]interface{}{
		"action": action,
	}
	if len(params) > 0 {
		op["params"] = params
	}
	return op
}

// RecordLoginLog 记录用户登录成功的审计日志（type=LogTypeLogin）。
// username 由调用方传入（登录流程已持有用户对象），避免额外的数据库查询。
// content 为英文兜底文本（用于导出/经典前端）；action+params 供前端本地化渲染。
// extra 可携带 login_method、user_agent 等附加信息（普通用户可见）。
func RecordLoginLog(userId int, username string, content string, ip string, action string, params map[string]interface{}, extra map[string]interface{}) {
	other := map[string]interface{}{}
	for k, v := range extra {
		other[k] = v
	}
	other["op"] = buildOpField(action, params)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeLogin,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record login log: " + err.Error())
	}
}

// RecordOperationAuditLog 记录管理/高危操作审计日志（type=LogTypeManage）。
// logUserId 为日志归属者，管理审计日志应归属实际操作者；目标资源/用户放入
// action params。username 内部按 logUserId 查询。content 为英文兜底文本（导出/经典前端用）。
// action+params 写入 Other.op，供前端本地化渲染（普通用户可见，不含敏感信息）。
// adminInfo 存放操作者身份（写入 Other.admin_info，普通用户查询时剥离）；
// auditInfo 存放路由/方法/结果等中间件兜底信息（写入 Other.audit_info，普通用户查询时剥离）。
func RecordOperationAuditLog(logUserId int, content string, ip string, action string, params map[string]interface{}, adminInfo map[string]interface{}, auditInfo map[string]interface{}) {
	username, _ := GetUsernameById(logUserId, false)
	other := map[string]interface{}{
		"op": buildOpField(action, params),
	}
	if len(adminInfo) > 0 {
		other["admin_info"] = adminInfo
	}
	if len(auditInfo) > 0 {
		other["audit_info"] = auditInfo
	}
	log := &Log{
		UserId:    logUserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeManage,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record operation audit log: " + err.Error())
	}
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := GetUsernameById(userId, false)
	adminInfo := map[string]interface{}{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	}
	other := map[string]interface{}{
		"admin_info": adminInfo,
	}
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other:     common.MapToJsonStr(other),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	// Prefer updating the in-flight request row so one request stays one log line.
	if other == nil {
		other = map[string]interface{}{}
	}
	if _, ok := other["status_code"]; !ok {
		other["status_code"] = 0
	}
	if FinalizeRequestLog(c, FinalizeRequestLogParams{
		Success:        false,
		StatusCode:     asInt(other["status_code"]),
		ChannelId:      channelId,
		ModelName:      modelName,
		TokenName:      tokenName,
		TokenId:        tokenId,
		UseTimeSeconds: useTimeSeconds,
		IsStream:       isStream,
		Group:          group,
		Content:        content,
		Other:          other,
		ForceType:      LogTypeError,
	}) {
		return
	}
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	otherStr := common.MapToJsonStr(other)
	log := &Log{
		UserId:            userId,
		Username:          username,
		CreatedAt:         common.GetTimestamp(),
		Type:              LogTypeError,
		Content:           content,
		PromptTokens:      0,
		CompletionTokens:  0,
		TokenName:         tokenName,
		ModelName:         modelName,
		Quota:             0,
		ChannelId:         channelId,
		TokenId:           tokenId,
		UseTime:           useTimeSeconds,
		IsStream:          isStream,
		Group:             group,
		Ip:                lifecycleClientIP(c),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

func asInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}

func lifecycleClientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.ClientIP()
}

// StartRequestLog inserts a processing row for the current request_id.
// One request should keep one log line; finish paths update this row.
func StartRequestLog(c *gin.Context, modelName string, isStream bool, group string) {
	if c == nil {
		return
	}
	requestId := c.GetString(common.RequestIdKey)
	if requestId == "" {
		return
	}
	userId := c.GetInt("id")
	if userId == 0 {
		return
	}
	var existing int64
	if err := LOG_DB.Model(&Log{}).Where("request_id = ? AND type = ?", requestId, LogTypeProcessing).Limit(1).Count(&existing).Error; err == nil && existing > 0 {
		c.Set("request_log_started", true)
		startRequestLogHeartbeat(c, requestId)
		return
	}
	username := c.GetString("username")
	tokenName := c.GetString("token_name")
	tokenId := c.GetInt("token_id")
	if group == "" {
		group = c.GetString("group")
	}
	if modelName == "" {
		modelName = c.GetString("original_model")
	}
	other := map[string]interface{}{
		"request_status": "processing",
		"status_code":    0,
		"billing_status": "pending",
	}
	if c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	now := common.GetTimestamp()
	log := &Log{
		UserId:         userId,
		Username:       username,
		CreatedAt:      now,
		LastActivityAt: now,
		Type:           LogTypeProcessing,
		Content:        "processing",
		TokenName:      tokenName,
		ModelName:      modelName,
		TokenId:        tokenId,
		IsStream:       isStream,
		Group:          group,
		Ip:             lifecycleClientIP(c),
		RequestId:      requestId,
		Other:          common.MapToJsonStr(other),
	}
	if err := createLog(log); err != nil {
		logger.LogError(c, "failed to start request log: "+err.Error())
		return
	}
	c.Set("request_log_started", true)
	startRequestLogHeartbeat(c, requestId)
}

// TouchRequestLog refreshes the ownership lease while the request is still running.
// The stale closer only reclaims rows whose lease has expired, so long upstream
// requests remain chargeable while abandoned work is still recoverable.
func TouchRequestLog(requestId string) bool {
	if LOG_DB == nil || requestId == "" {
		return false
	}
	result := LOG_DB.Model(&Log{}).
		Where("request_id = ? AND type = ?", requestId, LogTypeProcessing).
		Update("last_activity_at", common.GetTimestamp())
	return result.Error == nil && result.RowsAffected == 1
}

func startRequestLogHeartbeat(c *gin.Context, requestId string) {
	if c == nil || c.Request == nil || requestId == "" {
		return
	}
	interval := processingLogTimeout() / 3
	if interval < time.Second {
		interval = time.Second
	}
	go runRequestLogHeartbeat(c.Request.Context(), requestId, interval)
}

func runRequestLogHeartbeat(ctx context.Context, requestId string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !TouchRequestLog(requestId) {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

type FinalizeRequestLogParams struct {
	Success          bool
	StatusCode       int
	ChannelId        int
	PromptTokens     int
	CompletionTokens int
	ModelName        string
	TokenName        string
	Quota            int
	Content          string
	TokenId          int
	UseTimeSeconds   int
	IsStream         bool
	Group            string
	Other            map[string]interface{}
	// ForceType overrides derived type (consume/error). 0 = auto.
	ForceType int
}

// FinalizeRequestLog updates the processing row for this request_id.
// Returns true if an existing processing row was updated.
func FinalizeRequestLog(c *gin.Context, params FinalizeRequestLogParams) bool {
	if c == nil {
		return false
	}
	requestId := c.GetString(common.RequestIdKey)
	if requestId == "" {
		return false
	}

	var log Log
	if err := LOG_DB.Where("request_id = ? AND type = ?", requestId, LogTypeProcessing).Order("id desc").First(&log).Error; err != nil {
		return false
	}

	otherMap, _ := common.StrToMap(log.Other)
	if otherMap == nil {
		otherMap = map[string]interface{}{}
	}
	for k, v := range params.Other {
		otherMap[k] = v
	}

	statusCode := params.StatusCode
	if statusCode == 0 {
		if params.Success {
			statusCode = 200
		} else {
			statusCode = 500
		}
	}
	otherMap["status_code"] = statusCode
	if params.Success {
		otherMap["request_status"] = "completed"
		if params.Quota > 0 {
			otherMap["billing_status"] = "charged"
		} else if v, ok := otherMap["billing_status"]; !ok || v == "pending" {
			otherMap["billing_status"] = "no_charge"
		}
	} else {
		otherMap["request_status"] = "failed"
		if v, ok := otherMap["billing_status"]; !ok || v == "pending" {
			otherMap["billing_status"] = "no_charge"
		}
	}

	logType := params.ForceType
	if logType == 0 {
		if params.Success {
			logType = LogTypeConsume
		} else {
			logType = LogTypeError
		}
	}

	content := params.Content
	if content == "" {
		if params.Success {
			content = "completed"
		} else {
			content = "failed"
		}
	}

	updates := map[string]interface{}{
		"type":              logType,
		"finished_at":       common.GetTimestamp(),
		"content":           content,
		"prompt_tokens":     params.PromptTokens,
		"completion_tokens": params.CompletionTokens,
		"quota":             params.Quota,
		"use_time":          params.UseTimeSeconds,
		"is_stream":         params.IsStream,
		"other":             common.MapToJsonStr(otherMap),
	}
	if params.ChannelId > 0 {
		updates["channel_id"] = params.ChannelId
	}
	if params.ModelName != "" {
		updates["model_name"] = params.ModelName
	}
	if params.TokenName != "" {
		updates["token_name"] = params.TokenName
	}
	if params.TokenId > 0 {
		updates["token_id"] = params.TokenId
	}
	if params.Group != "" {
		// GORM map Updates already quotes identifiers; do NOT use logGroupCol
		// (which is already dialect-quoted for raw SQL WHERE clauses).
		updates["group"] = params.Group
	}
	if upstream := c.GetString(common.UpstreamRequestIdKey); upstream != "" {
		updates["upstream_request_id"] = upstream
	}

	result := LOG_DB.Model(&Log{}).Where("id = ? AND type = ?", log.Id, LogTypeProcessing).Updates(updates)
	if result.Error != nil {
		logger.LogError(c, "failed to finalize request log: "+result.Error.Error())
		return false
	}
	if result.RowsAffected != 1 {
		return false
	}
	c.Set("request_log_finalized", true)
	return true
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	if params.Other == nil {
		params.Other = map[string]interface{}{}
	}
	// Lottery accumulation is tied to the finalized, successful wallet charge,
	// not to whether consume logging is enabled. The request id makes this
	// idempotent across duplicate finalization attempts.
	billingSource, _ := params.Other["billing_source"].(string)
	violationFee, _ := params.Other["violation_fee"].(bool)
	requestStatus, _ := params.Other["request_status"].(string)
	statusCode := asInt(params.Other["status_code"])
	deferLottery, _ := params.Other["defer_lottery"].(bool)
	billingSettled := false
	if c != nil {
		billingSettled = c.GetBool("billing_settled")
	}
	if params.Quota > 0 && billingSettled && billingSource != "subscription" && !deferLottery && !violationFee && requestStatus != "failed" && statusCode < 400 {
		eventKey := ""
		if c != nil {
			eventKey = c.GetString(common.RequestIdKey)
		}
		if err := AccumulateLotteryConsumption(userId, params.Quota, eventKey); err != nil {
			logger.LogError(c, "failed to accumulate lottery consumption: "+err.Error())
		}
	}
	if !common.LogConsumeEnabled {
		return
	}
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	if _, ok := params.Other["status_code"]; !ok {
		params.Other["status_code"] = 200
	}
	// Violation-fee rows are extra charges on top of the main request and must
	// never rewrite the in-flight / main lifecycle row.
	isViolationFee, _ := params.Other["violation_fee"].(bool)
	if !isViolationFee {
		if FinalizeRequestLog(c, FinalizeRequestLogParams{
			Success:          true,
			StatusCode:       asInt(params.Other["status_code"]),
			ChannelId:        params.ChannelId,
			PromptTokens:     params.PromptTokens,
			CompletionTokens: params.CompletionTokens,
			ModelName:        params.ModelName,
			TokenName:        params.TokenName,
			Quota:            params.Quota,
			Content:          params.Content,
			TokenId:          params.TokenId,
			UseTimeSeconds:   params.UseTimeSeconds,
			IsStream:         params.IsStream,
			Group:            params.Group,
			Other:            params.Other,
			ForceType:        LogTypeConsume,
		}) {
			username := c.GetString("username")
			createdAt := common.GetTimestamp()
			if common.DataExportEnabled {
				LogQuotaData(QuotaDataLogParams{
					UserID:    userId,
					Username:  username,
					ModelName: params.ModelName,
					Quota:     params.Quota,
					CreatedAt: createdAt,
					TokenUsed: params.PromptTokens + params.CompletionTokens,
					UseGroup:  params.Group,
					TokenID:   params.TokenId,
					ChannelID: params.ChannelId,
					NodeName:  common.NodeName,
				})
			}
			return
		}
	}
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	createdAt := common.GetTimestamp()
	otherStr := common.MapToJsonStr(params.Other)
	log := &Log{
		UserId:            userId,
		Username:          username,
		CreatedAt:         createdAt,
		Type:              LogTypeConsume,
		Content:           params.Content,
		PromptTokens:      params.PromptTokens,
		CompletionTokens:  params.CompletionTokens,
		TokenName:         params.TokenName,
		ModelName:         params.ModelName,
		Quota:             params.Quota,
		ChannelId:         params.ChannelId,
		TokenId:           params.TokenId,
		UseTime:           params.UseTimeSeconds,
		IsStream:          params.IsStream,
		Group:             params.Group,
		Ip:                lifecycleClientIP(c),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	if common.DataExportEnabled {
		LogQuotaData(QuotaDataLogParams{
			UserID:    userId,
			Username:  username,
			ModelName: params.ModelName,
			Quota:     params.Quota,
			CreatedAt: createdAt,
			TokenUsed: params.PromptTokens + params.CompletionTokens,
			UseGroup:  params.Group,
			TokenID:   params.TokenId,
			ChannelID: params.ChannelId,
			NodeName:  common.NodeName,
		})
	}
}

type RecordTaskBillingLogParams struct {
	UserId    int
	LogType   int
	Content   string
	ChannelId int
	ModelName string
	Quota     int
	TokenId   int
	Group     string
	Other     map[string]interface{}
	NodeName  string // 任务发起节点；为空时回退当前节点
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	createdAt := common.GetTimestamp()
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: createdAt,
		Type:      params.LogType,
		Content:   params.Content,
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     common.MapToJsonStr(params.Other),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
	if params.LogType == LogTypeConsume && common.DataExportEnabled {
		nodeName := params.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		LogQuotaData(QuotaDataLogParams{
			UserID:    params.UserId,
			Username:  username,
			ModelName: params.ModelName,
			Quota:     params.Quota,
			CreatedAt: createdAt,
			UseGroup:  params.Group,
			TokenID:   params.TokenId,
			ChannelID: params.ChannelId,
			NodeName:  nodeName,
		})
	}
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB
	} else {
		tx = LOG_DB.Where("logs.type = ?", logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", username); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	order := fmt.Sprintf("CASE WHEN logs.type = %d THEN 0 ELSE 1 END, logs.created_at desc, logs.id desc", LogTypeProcessing)
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = fmt.Sprintf("if(logs.type = %d, 0, 1), %s", LogTypeProcessing, clickHouseLogOrder("logs."))
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, startIdx)
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

const logSearchCountLimit = 10000

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	order := fmt.Sprintf("CASE WHEN logs.type = %d THEN 0 ELSE 1 END, logs.created_at desc, logs.id desc", LogTypeProcessing)
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = fmt.Sprintf("if(logs.type = %d, 0, 1), %s", LogTypeProcessing, clickHouseLogOrder("logs."))
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

// Stat is the usage-logs top summary bar: filtered-range totals + last-60s rates.
type Stat struct {
	Quota             int64   `json:"quota"`
	Rpm               int64   `json:"rpm"`
	Tpm               int64   `json:"tpm"`
	RequestCount      int64   `json:"request_count"`
	PromptTokens      int64   `json:"prompt_tokens"`
	CompletionTokens  int64   `json:"completion_tokens"`
	TotalTokens       int64   `json:"total_tokens"`
	CacheReadTokens   int64   `json:"cache_read_tokens"`
	CacheCreateTokens int64   `json:"cache_create_tokens"`
	CacheRate         float64 `json:"cache_rate"`
	// Last-60s rates for cache/output (same window as rpm/tpm).
	CacheReadTpm   int64 `json:"cache_read_tpm"`
	CacheCreateTpm int64 `json:"cache_create_tpm"`
	CompletionTpm  int64 `json:"completion_tpm"`
}

func applyLogStatFilters(tx *gorm.DB, username, tokenName, modelName string, channel int, group string, startTimestamp, endTimestamp int64) (*gorm.DB, error) {
	var err error
	if tx, err = applyExplicitLogTextFilter(tx, "username", username); err != nil {
		return tx, err
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return tx, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
	}
	return tx.Where("type = ?", LogTypeConsume), nil
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	_ = logType // historical param; consume-only stats below
	cacheReadExpr, cacheCreateExpr := cacheTokenSelectExpr()

	// Range totals (respect filters / time window).
	rangeSelect := fmt.Sprintf(`
COALESCE(SUM(quota), 0) as quota,
COUNT(*) as request_count,
COALESCE(SUM(prompt_tokens), 0) as prompt_tokens,
COALESCE(SUM(completion_tokens), 0) as completion_tokens,
COALESCE(SUM(%s), 0) as cache_read_tokens,
COALESCE(SUM(%s), 0) as cache_create_tokens
`, cacheReadExpr, cacheCreateExpr)

	type rangeRow struct {
		Quota             int64
		RequestCount      int64
		PromptTokens      int64
		CompletionTokens  int64
		CacheReadTokens   int64
		CacheCreateTokens int64
	}
	var rr rangeRow
	tx := LOG_DB.Table("logs").Select(rangeSelect)
	if tx, err = applyLogStatFilters(tx, username, tokenName, modelName, channel, group, startTimestamp, endTimestamp); err != nil {
		return stat, err
	}
	if err = tx.Scan(&rr).Error; err != nil {
		// Fallback without JSON extract if DB rejects it.
		common.SysError("failed to query log range stat with cache extract, fallback: " + err.Error())
		tx2 := LOG_DB.Table("logs").Select(`
COALESCE(SUM(quota), 0) as quota,
COUNT(*) as request_count,
COALESCE(SUM(prompt_tokens), 0) as prompt_tokens,
COALESCE(SUM(completion_tokens), 0) as completion_tokens,
0 as cache_read_tokens,
0 as cache_create_tokens
`)
		if tx2, err = applyLogStatFilters(tx2, username, tokenName, modelName, channel, group, startTimestamp, endTimestamp); err != nil {
			return stat, err
		}
		if err = tx2.Scan(&rr).Error; err != nil {
			common.SysError("failed to query log range stat: " + err.Error())
			return stat, errors.New("查询统计数据失败")
		}
	}

	// Last 60 seconds rates (same filters except forced recent window).
	liveStart := time.Now().Add(-60 * time.Second).Unix()
	liveSelect := fmt.Sprintf(`
COUNT(*) as rpm,
COALESCE(SUM(prompt_tokens), 0) + COALESCE(SUM(completion_tokens), 0) as tpm,
COALESCE(SUM(%s), 0) as cache_read_tpm,
COALESCE(SUM(%s), 0) as cache_create_tpm,
COALESCE(SUM(completion_tokens), 0) as completion_tpm
`, cacheReadExpr, cacheCreateExpr)
	type liveRow struct {
		Rpm            int64
		Tpm            int64
		CacheReadTpm   int64
		CacheCreateTpm int64
		CompletionTpm  int64
	}
	var lr liveRow
	liveTx := LOG_DB.Table("logs").Select(liveSelect)
	if liveTx, err = applyLogStatFilters(liveTx, username, tokenName, modelName, channel, group, liveStart, 0); err != nil {
		return stat, err
	}
	if err = liveTx.Scan(&lr).Error; err != nil {
		common.SysError("failed to query rpm/tpm with cache extract, fallback: " + err.Error())
		liveTx2 := LOG_DB.Table("logs").Select(`
COUNT(*) as rpm,
COALESCE(SUM(prompt_tokens), 0) + COALESCE(SUM(completion_tokens), 0) as tpm,
0 as cache_read_tpm,
0 as cache_create_tpm,
COALESCE(SUM(completion_tokens), 0) as completion_tpm
`)
		if liveTx2, err = applyLogStatFilters(liveTx2, username, tokenName, modelName, channel, group, liveStart, 0); err != nil {
			return stat, err
		}
		if err = liveTx2.Scan(&lr).Error; err != nil {
			common.SysError("failed to query rpm/tpm stat: " + err.Error())
			return stat, errors.New("查询统计数据失败")
		}
	}

	stat = Stat{
		Quota:             rr.Quota,
		Rpm:               lr.Rpm,
		Tpm:               lr.Tpm,
		RequestCount:      rr.RequestCount,
		PromptTokens:      rr.PromptTokens,
		CompletionTokens:  rr.CompletionTokens,
		TotalTokens:       rr.PromptTokens + rr.CompletionTokens,
		CacheReadTokens:   rr.CacheReadTokens,
		CacheCreateTokens: rr.CacheCreateTokens,
		CacheRate:         calcCacheRate(rr.CacheReadTokens, rr.PromptTokens),
		CacheReadTpm:      lr.CacheReadTpm,
		CacheCreateTpm:    lr.CacheCreateTpm,
		CompletionTpm:     lr.CompletionTpm,
	}
	return stat, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

// UsageModelStat is one model row in the full usage breakdown dashboard.
type UsageModelStat struct {
	ModelName         string  `json:"model_name"`
	Count             int64   `json:"count"`
	Quota             int64   `json:"quota"`
	PromptTokens      int64   `json:"prompt_tokens"`
	CompletionTokens  int64   `json:"completion_tokens"`
	CacheReadTokens   int64   `json:"cache_read_tokens"`
	CacheCreateTokens int64   `json:"cache_create_tokens"`
	TotalTokens       int64   `json:"total_tokens"`
	CacheRate         float64 `json:"cache_rate"`
}

// UsageBreakdown is the full usage summary for dashboard models section.
type UsageBreakdown struct {
	RequestCount      int64            `json:"request_count"`
	Quota             int64            `json:"quota"`
	PromptTokens      int64            `json:"prompt_tokens"`
	CompletionTokens  int64            `json:"completion_tokens"`
	CacheReadTokens   int64            `json:"cache_read_tokens"`
	CacheCreateTokens int64            `json:"cache_create_tokens"`
	TotalTokens       int64            `json:"total_tokens"`
	CacheRate         float64          `json:"cache_rate"`
	Models            []UsageModelStat `json:"models"`
}

func cacheTokenSelectExpr() (cacheRead string, cacheCreate string) {
	// Prefer DB-native JSON extract. Fall back to 0 when unsupported.
	if common.UsingLogDatabase(common.DatabaseTypeSQLite) {
		return "COALESCE(CAST(json_extract(CASE WHEN json_valid(other) THEN other ELSE '{}' END, '$.cache_tokens') AS INTEGER), 0)",
			"COALESCE(CAST(json_extract(CASE WHEN json_valid(other) THEN other ELSE '{}' END, '$.cache_creation_tokens') AS INTEGER), 0) + COALESCE(CAST(json_extract(CASE WHEN json_valid(other) THEN other ELSE '{}' END, '$.cache_write_tokens') AS INTEGER), 0)"
	}
	if common.UsingLogDatabase(common.DatabaseTypeMySQL) {
		return "COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_tokens')) AS SIGNED), 0)",
			"COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_creation_tokens')) AS SIGNED), 0) + COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_write_tokens')) AS SIGNED), 0)"
	}
	if common.UsingLogDatabase(common.DatabaseTypePostgreSQL) {
		return "COALESCE((other::jsonb->>'cache_tokens')::bigint, 0)",
			"COALESCE((other::jsonb->>'cache_creation_tokens')::bigint, 0) + COALESCE((other::jsonb->>'cache_write_tokens')::bigint, 0)"
	}
	// ClickHouse / unknown: no reliable JSON extract here → 0
	return "0", "0"
}

func calcCacheRate(cacheRead, promptTokens int64) float64 {
	if promptTokens <= 0 {
		return 0
	}
	return float64(cacheRead) * 100.0 / float64(promptTokens)
}

// GetUsageBreakdown aggregates consume logs into summary + per-model top list.
// userId > 0 limits to that user; username non-empty further filters (admin).
// topN <= 0 defaults to 8.
func GetUsageBreakdown(startTimestamp, endTimestamp int64, userId int, username string, topN int) (*UsageBreakdown, error) {
	if topN <= 0 {
		topN = 8
	}
	cacheReadExpr, cacheCreateExpr := cacheTokenSelectExpr()

	type aggRow struct {
		ModelName         string
		Count             int64
		Quota             int64
		PromptTokens      int64
		CompletionTokens  int64
		CacheReadTokens   int64
		CacheCreateTokens int64
	}

	selectSQL := fmt.Sprintf(`
model_name as model_name,
COUNT(*) as count,
COALESCE(SUM(quota), 0) as quota,
COALESCE(SUM(prompt_tokens), 0) as prompt_tokens,
COALESCE(SUM(completion_tokens), 0) as completion_tokens,
COALESCE(SUM(%s), 0) as cache_read_tokens,
COALESCE(SUM(%s), 0) as cache_create_tokens
`, cacheReadExpr, cacheCreateExpr)

	tx := LOG_DB.Table("logs").Select(selectSQL).Where("type = ?", LogTypeConsume)
	if startTimestamp > 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp > 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if userId > 0 {
		tx = tx.Where("user_id = ?", userId)
	}
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	tx = tx.Group("model_name").Order("count DESC")

	var rows []aggRow
	if err := tx.Scan(&rows).Error; err != nil {
		// Fallback without JSON extract if DB rejects it
		common.SysError("usage breakdown with cache extract failed, fallback without cache: " + err.Error())
		tx2 := LOG_DB.Table("logs").Select(`
model_name as model_name,
COUNT(*) as count,
COALESCE(SUM(quota), 0) as quota,
COALESCE(SUM(prompt_tokens), 0) as prompt_tokens,
COALESCE(SUM(completion_tokens), 0) as completion_tokens,
0 as cache_read_tokens,
0 as cache_create_tokens
`).Where("type = ?", LogTypeConsume)
		if startTimestamp > 0 {
			tx2 = tx2.Where("created_at >= ?", startTimestamp)
		}
		if endTimestamp > 0 {
			tx2 = tx2.Where("created_at <= ?", endTimestamp)
		}
		if userId > 0 {
			tx2 = tx2.Where("user_id = ?", userId)
		}
		if username != "" {
			tx2 = tx2.Where("username = ?", username)
		}
		if err2 := tx2.Group("model_name").Order("count DESC").Scan(&rows).Error; err2 != nil {
			return nil, err2
		}
	}

	capN := topN
	if capN > len(rows) {
		capN = len(rows)
	}
	out := &UsageBreakdown{Models: make([]UsageModelStat, 0, capN)}
	for i, r := range rows {
		totalTokens := r.PromptTokens + r.CompletionTokens
		rate := calcCacheRate(r.CacheReadTokens, r.PromptTokens)
		out.RequestCount += r.Count
		out.Quota += r.Quota
		out.PromptTokens += r.PromptTokens
		out.CompletionTokens += r.CompletionTokens
		out.CacheReadTokens += r.CacheReadTokens
		out.CacheCreateTokens += r.CacheCreateTokens
		out.TotalTokens += totalTokens
		if i < topN {
			name := r.ModelName
			if name == "" {
				name = "(unknown)"
			}
			out.Models = append(out.Models, UsageModelStat{
				ModelName:         name,
				Count:             r.Count,
				Quota:             r.Quota,
				PromptTokens:      r.PromptTokens,
				CompletionTokens:  r.CompletionTokens,
				CacheReadTokens:   r.CacheReadTokens,
				CacheCreateTokens: r.CacheCreateTokens,
				TotalTokens:       totalTokens,
				CacheRate:         rate,
			})
		}
	}
	out.CacheRate = calcCacheRate(out.CacheReadTokens, out.PromptTokens)
	return out, nil
}

func CountOldLog(ctx context.Context, targetTimestamp int64) (int64, error) {
	var total int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("created_at < ?", targetTimestamp).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func DeleteOldLogBatch(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if nil != ctx.Err() {
		return 0, ctx.Err()
	}

	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		// ClickHouse DELETE is a heavy mutation that rewrites data parts, so
		// per-batch mutations would be pathologically slow. Remove all matching
		// rows in a single synchronous mutation regardless of limit; the reported
		// count lets the caller's progress loop complete in one pass.
		total, err := CountOldLog(ctx, targetTimestamp)
		if err != nil {
			return 0, err
		}
		if total == 0 {
			return 0, nil
		}
		if err := LOG_DB.WithContext(ctx).Exec(
			"ALTER TABLE logs DELETE WHERE created_at < ? SETTINGS mutations_sync = 1",
			targetTimestamp,
		).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	result := LOG_DB.WithContext(ctx).Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
	if nil != result.Error {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}

	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		rowsAffected, err := DeleteOldLogBatch(ctx, targetTimestamp, limit)
		if nil != err {
			return total, err
		}

		total += rowsAffected

		if rowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}
