package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	channelMonitorMinInterval  = 30
	channelMonitorMaxInterval  = 86400
	channelMonitorHistorySlots = 60
)

var monitorRunMu sync.Mutex
var monitorRunning = map[int64]bool{}

type channelMonitorProbeContextKey struct{}

type channelMonitorInput struct {
	Name            *string   `json:"name"`
	Group           *string   `json:"group"`
	PrimaryModel    *string   `json:"primary_model"`
	ExtraModels     *[]string `json:"extra_models"`
	IntervalSeconds *int      `json:"interval_seconds"`
	Enabled         *bool     `json:"enabled"`
}

type monitorPublic struct {
	Name            string                       `json:"name"`
	Group           string                       `json:"group"`
	Models          map[string]monitorPublicStat `json:"models"`
	IntervalSeconds int                          `json:"interval_seconds"`
}

type monitorAdmin struct {
	ID              int64                  `json:"id"`
	Name            string                 `json:"name"`
	Group           string                 `json:"group"`
	PrimaryModel    string                 `json:"primary_model"`
	ExtraModels     []string               `json:"extra_models"`
	IntervalSeconds int                    `json:"interval_seconds"`
	Enabled         bool                   `json:"enabled"`
	LastCheckedAt   int64                  `json:"last_checked_at"`
	Models          map[string]monitorStat `json:"models"`
	CreatedAt       int64                  `json:"created_at"`
	UpdatedAt       int64                  `json:"updated_at"`
	ModuleEnabled   bool                   `json:"module_enabled"`
}

type monitorPublicStat struct {
	Status       string             `json:"status"`
	LatencyMS    int64              `json:"latency_ms"`
	Availability map[string]float64 `json:"availability"`
	History      []monitorHistory   `json:"history,omitempty"`
}

type monitorStat struct {
	Status       string             `json:"status"`
	LatencyMS    int64              `json:"latency_ms"`
	ErrorSummary string             `json:"error_summary,omitempty"`
	LastChecked  int64              `json:"last_checked"`
	Availability map[string]float64 `json:"availability"`
	History      []monitorHistory   `json:"history,omitempty"`
}

type monitorHistory struct {
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	CheckedAt int64  `json:"checked_at"`
}

func parseMonitorModels(m *model.ChannelMonitor) []string {
	var extra []string
	if m.ExtraModelsJSON != "" {
		_ = common.Unmarshal([]byte(m.ExtraModelsJSON), &extra)
	}
	return extra
}

func validateMonitorGroup(group string) (string, error) {
	if strings.ContainsAny(group, "\r\n") {
		return "", errors.New("invalid monitor group")
	}
	group = strings.TrimSpace(group)
	if group == "" || len(group) > 128 {
		return "", errors.New("invalid monitor group")
	}
	return group, nil
}

func availableMonitorModels(group string) []string {
	models := model.GetGroupEnabledModels(group)
	sort.Strings(models)
	result := make([]string, 0, len(models))
	for _, name := range models {
		if name == "" || len(result) > 0 && result[len(result)-1] == name {
			continue
		}
		result = append(result, name)
	}
	return result
}

func validateMonitorFields(name, group, primary string, extraModels []string, interval int, availableModels []string) ([]string, error) {
	if !utf8.ValidString(name) {
		return nil, errors.New("invalid monitor name")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return nil, errors.New("invalid monitor name")
		}
	}
	validatedGroup, groupErr := validateMonitorGroup(group)
	if strings.ContainsAny(primary, "\r\n") {
		return nil, errors.New("invalid monitor input")
	}
	name, group, primary = strings.TrimSpace(name), validatedGroup, strings.TrimSpace(primary)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		return nil, errors.New("invalid monitor name")
	}
	if groupErr != nil || primary == "" || len(primary) > 128 {
		return nil, errors.New("invalid monitor name, group, or model")
	}
	if interval == 0 {
		interval = common.ChannelMonitorDefaultIntervalSeconds
	}
	if interval < channelMonitorMinInterval || interval > channelMonitorMaxInterval {
		return nil, errors.New("interval must be between 30 and 86400 seconds")
	}
	if len(extraModels) > 10 {
		return nil, errors.New("too many extra models")
	}
	seen := map[string]bool{primary: true}
	extra := make([]string, 0, len(extraModels))
	for _, raw := range extraModels {
		name := strings.TrimSpace(raw)
		if name == "" || len(name) > 128 || strings.ContainsAny(name, "\r\n") || seen[name] {
			continue
		}
		seen[name] = true
		extra = append(extra, name)
	}
	if len(availableModels) == 0 {
		return nil, errors.New("selected group has no available models")
	}
	available := make(map[string]struct{}, len(availableModels))
	for _, name := range availableModels {
		available[name] = struct{}{}
	}
	if _, ok := available[primary]; !ok {
		return nil, errors.New("primary model is not available in selected group")
	}
	for _, name := range extra {
		if _, ok := available[name]; !ok {
			return nil, errors.New("one or more extra models are not available in selected group")
		}
	}
	return extra, nil
}

func parseMonitorID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid monitor id")
	}
	return id, nil
}

func monitorBadRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
}

func monitorAdminView(m model.ChannelMonitor, checks []model.ChannelMonitorCheck) monitorAdmin {
	extra := parseMonitorModels(&m)
	admin := monitorAdmin{ID: m.ID, Name: m.Name, Group: m.Group, PrimaryModel: m.PrimaryModel, ExtraModels: extra, IntervalSeconds: m.IntervalSeconds, Enabled: m.Enabled, LastCheckedAt: m.LastCheckedAt, Models: map[string]monitorStat{}, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, ModuleEnabled: common.ChannelMonitorEnabled}
	now := time.Now().Unix()
	windows := []int64{int64(24 * time.Hour.Seconds()), int64(7 * 24 * time.Hour.Seconds()), int64(30 * 24 * time.Hour.Seconds())}
	for _, name := range append([]string{m.PrimaryModel}, extra...) {
		stat := monitorStat{Status: "unknown", Availability: map[string]float64{}}
		totals := make([]int, len(windows))
		successes := make([]int, len(windows))
		modelChecks := make([]model.ChannelMonitorCheck, 0, len(checks))
		for _, check := range checks {
			if check.Model == name {
				modelChecks = append(modelChecks, check)
			}
		}
		sort.SliceStable(modelChecks, func(i, j int) bool {
			return modelChecks[i].CheckedAt > modelChecks[j].CheckedAt
		})
		for _, check := range modelChecks {
			if stat.LastChecked == 0 {
				stat.Status, stat.LatencyMS, stat.ErrorSummary, stat.LastChecked = check.Status, check.LatencyMS, check.ErrorBrief, check.CheckedAt
			}
			for i, window := range windows {
				if check.CheckedAt >= now-window {
					totals[i]++
					if check.Status == model.ChannelMonitorStatusSuccess {
						successes[i]++
					}
				}
			}
			if len(stat.History) < channelMonitorHistorySlots {
				stat.History = append(stat.History, monitorHistory{Status: check.Status, LatencyMS: check.LatencyMS, CheckedAt: check.CheckedAt})
			}
		}
		for left, right := 0, len(stat.History)-1; left < right; left, right = left+1, right-1 {
			stat.History[left], stat.History[right] = stat.History[right], stat.History[left]
		}
		for i, window := range windows {
			if totals[i] > 0 {
				stat.Availability[fmt.Sprintf("%dh", window/3600)] = float64(successes[i]) / float64(totals[i]) * 100
			}
		}
		admin.Models[name] = stat
	}
	return admin
}

func AdminUpdateChannelMonitorConfig(c *gin.Context) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.Enabled == nil {
		monitorBadRequest(c, errors.New("enabled is required"))
		return
	}
	if err := model.UpdateOption("ChannelMonitorEnabled", strconv.FormatBool(*req.Enabled)); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"enabled": *req.Enabled})
}

func monitorPublicView(m model.ChannelMonitor, checks []model.ChannelMonitorCheck) monitorPublic {
	admin := monitorAdminView(m, checks)
	pub := monitorPublic{Name: m.Name, Group: m.Group, Models: map[string]monitorPublicStat{}, IntervalSeconds: m.IntervalSeconds}
	for name, stat := range admin.Models {
		pub.Models[name] = monitorPublicStat{Status: stat.Status, LatencyMS: stat.LatencyMS, Availability: stat.Availability, History: stat.History}
	}
	return pub
}

func GetChannelMonitors(c *gin.Context) {
	if !common.ChannelMonitorEnabled {
		common.ApiSuccess(c, []monitorPublic{})
		return
	}
	rows, err := model.ListChannelMonitors()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	activeRows := make([]model.ChannelMonitor, 0, len(rows))
	for _, row := range rows {
		if row.Enabled {
			activeRows = append(activeRows, row)
		}
	}
	rows = activeRows
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	allChecks, err := model.ListChannelMonitorChecksForMonitors(ids, time.Now().Add(-30*24*time.Hour).Unix(), model.ChannelMonitorMaxChecksPerSeries)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	checksByMonitor := map[int64][]model.ChannelMonitorCheck{}
	for _, check := range allChecks {
		checksByMonitor[check.MonitorID] = append(checksByMonitor[check.MonitorID], check)
	}
	result := make([]monitorPublic, 0, len(rows))
	for _, row := range rows {
		result = append(result, monitorPublicView(row, checksByMonitor[row.ID]))
	}
	common.ApiSuccess(c, result)
}

func AdminGetChannelMonitors(c *gin.Context) {
	rows, err := model.ListChannelMonitors()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	allChecks, e := model.ListChannelMonitorChecksForMonitors(ids, time.Now().Add(-30*24*time.Hour).Unix(), model.ChannelMonitorMaxChecksPerSeries)
	if e != nil {
		common.ApiError(c, e)
		return
	}
	checksByMonitor := map[int64][]model.ChannelMonitorCheck{}
	for _, check := range allChecks {
		checksByMonitor[check.MonitorID] = append(checksByMonitor[check.MonitorID], check)
	}
	result := make([]monitorAdmin, 0, len(rows))
	for _, row := range rows {
		result = append(result, monitorAdminView(row, checksByMonitor[row.ID]))
	}
	common.ApiSuccess(c, result)
}

func AdminGetChannelMonitorModels(c *gin.Context) {
	group, err := validateMonitorGroup(c.Query("group"))
	if err != nil {
		monitorBadRequest(c, err)
		return
	}
	common.ApiSuccess(c, availableMonitorModels(group))
}

func AdminCreateChannelMonitor(c *gin.Context) {
	var in channelMonitorInput
	if err := common.DecodeJson(c.Request.Body, &in); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	if in.Group == nil || in.PrimaryModel == nil {
		common.ApiErrorMsg(c, "group and model are required")
		return
	}
	extraModels := []string{}
	if in.ExtraModels != nil {
		extraModels = *in.ExtraModels
	}
	group, err := validateMonitorGroup(*in.Group)
	if err != nil {
		monitorBadRequest(c, err)
		return
	}
	name := group
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		name = *in.Name
	}
	interval := common.ChannelMonitorDefaultIntervalSeconds
	if in.IntervalSeconds != nil {
		interval = *in.IntervalSeconds
	}
	extra, err := validateMonitorFields(name, group, *in.PrimaryModel, extraModels, interval, availableMonitorModels(group))
	if err != nil {
		monitorBadRequest(c, err)
		return
	}
	raw, _ := common.Marshal(extra)
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	row := model.ChannelMonitor{Name: strings.TrimSpace(name), Group: group, PrimaryModel: strings.TrimSpace(*in.PrimaryModel), ExtraModelsJSON: string(raw), IntervalSeconds: interval, Enabled: enabled}
	if err := model.DB.Create(&row).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, row)
}

func AdminUpdateChannelMonitor(c *gin.Context) {
	idNum, idErr := parseMonitorID(c)
	if idErr != nil {
		monitorBadRequest(c, idErr)
		return
	}
	row, err := model.GetChannelMonitor(idNum)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var in channelMonitorInput
	if err := common.DecodeJson(c.Request.Body, &in); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	name, group, primary := row.Name, row.Group, row.PrimaryModel
	if in.Name != nil {
		name = *in.Name
	}
	if in.Group != nil {
		group = *in.Group
	}
	if in.PrimaryModel != nil {
		primary = *in.PrimaryModel
	}
	extraModels := parseMonitorModels(row)
	if in.ExtraModels != nil {
		extraModels = *in.ExtraModels
	}
	interval := row.IntervalSeconds
	if in.IntervalSeconds != nil {
		interval = *in.IntervalSeconds
	}
	group, err = validateMonitorGroup(group)
	if err != nil {
		monitorBadRequest(c, err)
		return
	}
	extra, err := validateMonitorFields(name, group, primary, extraModels, interval, availableMonitorModels(group))
	if err != nil {
		monitorBadRequest(c, err)
		return
	}
	raw, _ := common.Marshal(extra)
	row.Name, row.Group, row.PrimaryModel, row.ExtraModelsJSON, row.IntervalSeconds = strings.TrimSpace(name), strings.TrimSpace(group), strings.TrimSpace(primary), string(raw), interval
	if in.Enabled != nil {
		row.Enabled = *in.Enabled
	}
	if err := model.DB.Save(row).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, row)
}

func AdminDeleteChannelMonitor(c *gin.Context) {
	idNum, idErr := parseMonitorID(c)
	if idErr != nil {
		monitorBadRequest(c, idErr)
		return
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("monitor_id = ?", idNum).Delete(&model.ChannelMonitorCheck{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.ChannelMonitor{}, idNum).Error
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminSetChannelMonitorEnabled(c *gin.Context) { AdminUpdateChannelMonitor(c) }

func AdminRunChannelMonitor(c *gin.Context) {
	idNum, idErr := parseMonitorID(c)
	if idErr != nil {
		monitorBadRequest(c, idErr)
		return
	}
	row, err := model.GetChannelMonitor(idNum)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !row.Enabled {
		common.ApiErrorMsg(c, "monitor is disabled")
		return
	}
	monitorRunMu.Lock()
	if monitorRunning[idNum] {
		monitorRunMu.Unlock()
		common.ApiErrorMsg(c, "monitor is already running")
		return
	}
	monitorRunMu.Unlock()
	runMonitor(c.Request.Context(), row)
	checks, err := model.ListChannelMonitorChecksForMonitors([]int64{row.ID}, time.Now().Add(-30*24*time.Hour).Unix(), model.ChannelMonitorMaxChecksPerSeries)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, monitorAdminView(*row, checks))
}

func probeChannelMonitorModel(
	ctx context.Context,
	monitorID int64,
	group string,
	modelName string,
	userID int,
	retryTimes int,
	selectChannel func(string, string, int, string) (*model.Channel, error),
	probe func(context.Context, *model.Channel, int, string, string, bool) testResult,
) model.ChannelMonitorCheck {
	if retryTimes < 0 {
		retryTimes = 0
	}
	started := time.Now()
	check := model.ChannelMonitorCheck{
		MonitorID:  monitorID,
		Model:      modelName,
		CheckedAt:  started.Unix(),
		Status:     model.ChannelMonitorStatusFailed,
		ErrorCode:  "no_channel",
		ErrorBrief: "no available channel",
	}

	for attempt := 0; attempt <= retryTimes; attempt++ {
		channel, err := selectChannel(group, modelName, attempt, "")
		if err != nil || channel == nil {
			check.LatencyMS = time.Since(started).Milliseconds()
			return check
		}

		probeCtx, cancel := context.WithTimeout(context.WithValue(ctx, channelMonitorProbeContextKey{}, true), 60*time.Second)
		result := probe(probeCtx, channel, userID, modelName, "", false)
		timedOut := errors.Is(result.localErr, context.DeadlineExceeded) || errors.Is(probeCtx.Err(), context.DeadlineExceeded)
		cancel()

		if result.localErr == nil && result.newAPIError == nil {
			check.Status = model.ChannelMonitorStatusSuccess
			check.ErrorCode = ""
			check.ErrorBrief = ""
			check.LatencyMS = time.Since(started).Milliseconds()
			return check
		}
		if timedOut {
			check.Status = model.ChannelMonitorStatusTimeout
			check.ErrorCode = "timeout"
			check.ErrorBrief = "probe timed out"
		} else {
			check.Status = model.ChannelMonitorStatusFailed
			check.ErrorCode = "probe_failed"
			check.ErrorBrief = "model request failed"
		}

		remainingRetries := retryTimes - attempt
		if result.context == nil || types.IsSkipRetryError(result.newAPIError) || !shouldRetry(result.context, result.newAPIError, remainingRetries) {
			check.LatencyMS = time.Since(started).Milliseconds()
			return check
		}
	}

	check.LatencyMS = time.Since(started).Milliseconds()
	return check
}

func runMonitor(ctx context.Context, row *model.ChannelMonitor) {
	monitorRunMu.Lock()
	if monitorRunning[row.ID] {
		monitorRunMu.Unlock()
		return
	}
	monitorRunning[row.ID] = true
	monitorRunMu.Unlock()
	defer func() { monitorRunMu.Lock(); delete(monitorRunning, row.ID); monitorRunMu.Unlock() }()
	userID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return
	}
	models := append([]string{row.PrimaryModel}, parseMonitorModels(row)...)
	for _, modelName := range models {
		check := probeChannelMonitorModel(ctx, row.ID, row.Group, modelName, userID, common.RetryTimes, model.GetRandomSatisfiedChannel, testChannel)
		if err := model.DB.Create(&check).Error; err != nil {
			common.SysError("channel monitor check write failed: " + err.Error())
		}
	}
	if err := model.DB.Model(row).Updates(map[string]any{"last_checked_at": time.Now().Unix()}).Error; err != nil {
		common.SysError("channel monitor update failed: " + err.Error())
	}
}

type channelMonitorHandler struct{}

func (channelMonitorHandler) Type() string  { return model.SystemTaskTypeChannelMonitor }
func (channelMonitorHandler) Enabled() bool { return common.ChannelMonitorEnabled }
func (channelMonitorHandler) Interval() time.Duration {
	rows, err := model.ListChannelMonitors()
	if err != nil {
		return time.Duration(common.ChannelMonitorDefaultIntervalSeconds) * time.Second
	}
	minInterval := 0
	for _, row := range rows {
		if !row.Enabled || row.IntervalSeconds <= 0 {
			continue
		}
		if minInterval == 0 || row.IntervalSeconds < minInterval {
			minInterval = row.IntervalSeconds
		}
	}
	if minInterval == 0 {
		minInterval = common.ChannelMonitorDefaultIntervalSeconds
	}
	return time.Duration(minInterval) * time.Second
}
func (channelMonitorHandler) NewPayload() any { return nil }
func (channelMonitorHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	rows, _ := model.ListChannelMonitors()
	for i := range rows {
		if rows[i].Enabled && time.Now().Unix()-rows[i].LastCheckedAt >= int64(rows[i].IntervalSeconds) {
			runMonitor(ctx, &rows[i])
		}
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, nil, nil)
}
