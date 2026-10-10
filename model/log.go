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
	LogTypeProcessing = 8
)

type LogScope string

const (
	LogScopeBusiness  LogScope = "business"
	LogScopeModelTest LogScope = "model_test"
	modelTestLogMark           = "模型测试"
)

func isModelTestLog(tokenName, content string) bool {
	return tokenName == modelTestLogMark || content == modelTestLogMark
}

func applyLogScope(tx *gorm.DB, scope LogScope, prefix string) *gorm.DB {
	tokenNameColumn := prefix + "token_name"
	contentColumn := prefix + "content"
	if scope == LogScopeModelTest {
		return tx.Where("("+tokenNameColumn+" = ? OR "+contentColumn+" = ?)", modelTestLogMark, modelTestLogMark)
	}
	return tx.Where("("+logCoalesceTextExpr(tokenNameColumn)+" <> ? AND "+logCoalesceTextExpr(contentColumn)+" <> ?)", modelTestLogMark, modelTestLogMark)
}

func logCoalesceTextExpr(column string) string {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return "ifNull(" + column + ", '')"
	}
	return "COALESCE(" + column + ", '')"
}

func ensureLogRequestId(log *Log) {
	if log != nil && log.RequestId == "" {
		log.RequestId = common.NewRequestId()
	}
}

func createLog(log *Log) error {
	ensureLogRequestId(log)
	if log != nil && log.FinishedAt == 0 && log.Type != LogTypeProcessing {
		log.FinishedAt = common.GetTimestamp()
	}
	return LOG_DB.Create(log).Error
}

const staleProcessingLogTimeout = 15 * time.Minute

// CloseStaleProcessingLogs closes lifecycle rows whose request heartbeat has
// expired. It changes display state only and never mutates quota or billing.
func CloseStaleProcessingLogs(now time.Time) (int64, error) {
	if LOG_DB == nil {
		return 0, nil
	}
	cutoff := now.Add(-staleProcessingLogTimeout).Unix()
	other := common.MapToJsonStr(map[string]any{
		"request_status": "timeout",
		"status_code":    408,
		"error_code":     "stale_processing_closed",
		"error_type":     "new_api_error",
	})
	result := LOG_DB.Model(&Log{}).
		Where("type = ? AND content = ? AND (((last_activity_at IS NULL OR last_activity_at = 0) AND created_at < ?) OR (last_activity_at > 0 AND last_activity_at < ?))", LogTypeProcessing, "processing", cutoff, cutoff).
		Updates(map[string]any{
			"type": LogTypeError, "content": "status_code=408, 请求超时已关闭",
			"finished_at": now.Unix(), "other": other,
		})
	return result.RowsAffected, result.Error
}

func clickHouseLogOrder(prefix string) string {
	return fmt.Sprintf("if(%stype = %d, 0, 1), if(%stype = %d, %screated_at, if(%sfinished_at > 0, %sfinished_at, %screated_at)) desc, %sid desc", prefix, LogTypeProcessing, prefix, LogTypeProcessing, prefix, prefix, prefix, prefix, prefix)
}

func logOrder(prefix string) string {
	return fmt.Sprintf("CASE WHEN %stype = %d THEN 0 ELSE 1 END, CASE WHEN %stype = %d THEN %screated_at ELSE CASE WHEN %sfinished_at > 0 THEN %sfinished_at ELSE %screated_at END END desc, %sid desc", prefix, LogTypeProcessing, prefix, LogTypeProcessing, prefix, prefix, prefix, prefix, prefix)
}

func assignDisplayLogIds(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].Id = startIdx + i + 1
	}
}

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		logs[i].Other = formatLogOtherJSON(logs[i].Other, logOtherVisibilityUser)
	}
	assignDisplayLogIds(logs, startIdx)
}

// FormatAdminLogs removes root-only diagnostics while retaining operational
// admin_info. Root callers must not pass their results through this formatter.
func FormatAdminLogs(logs []*Log) {
	for i := range logs {
		logs[i].Other = formatLogOtherJSON(logs[i].Other, logOtherVisibilityAdmin)
	}
}

// FormatRootLogs normalizes legacy metadata into the current scoped shape
// without removing root-only diagnostics.
func FormatRootLogs(logs []*Log) {
	for i := range logs {
		logs[i].Other = formatLogOtherJSON(logs[i].Other, logOtherVisibilityRoot)
	}
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	order := logOrder("")
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

// RecordLogWithAdminInfo stores operator metadata under other.admin_info and
// an optional, user-visible operation descriptor under other.op for localization.
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo *AuditAdminInfo, operation *AuditOperation, request ...*gin.Context) {
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
	if logType == LogTypeManage {
		var c *gin.Context
		if len(request) > 0 {
			c = request[0]
		}
		actorRole := 0
		if c != nil {
			actorRole = c.GetInt("role")
		}
		RecordAuditLog(c, AuditLog{UserId: userId, Username: username, ActorRole: actorRole, Category: AuditCategoryOperation, Content: content, Other: AuditOther{AdminInfo: adminInfo, Op: operation}, Success: true})
		return
	}
	if len(request) > 0 && request[0] != nil {
		log.RequestId = request[0].GetString(common.RequestIdKey)
	}
	if adminInfo != nil || operation != nil {
		data, err := common.Marshal(AuditOther{AdminInfo: adminInfo, Op: operation})
		if err != nil {
			common.SysError("failed to encode log admin info: " + err.Error())
			return
		}
		log.Other = string(data)
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// StartRequestLog records the in-flight request row that is later updated by
// the successful or failed terminal log. It is intentionally independent of
// billing so disabling consume logs does not hide request lifecycle state.
func StartRequestLog(c *gin.Context, modelName string, isStream bool, group string) {
	if c == nil || LOG_DB == nil {
		return
	}
	requestID := c.GetString(common.RequestIdKey)
	userID := c.GetInt("id")
	if requestID == "" || userID == 0 {
		return
	}
	var count int64
	if err := LOG_DB.Model(&Log{}).Where("request_id = ? AND type = ?", requestID, LogTypeProcessing).Count(&count).Error; err != nil || count > 0 {
		if count > 0 {
			c.Set("request_log_started", true)
		}
		return
	}
	if modelName == "" {
		modelName = c.GetString("original_model")
	}
	if group == "" {
		group = c.GetString("group")
	}
	other := map[string]any{"request_status": "processing", "status_code": 0}
	if c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	log := &Log{
		UserId: userID, Username: c.GetString("username"), CreatedAt: common.GetTimestamp(),
		Type: LogTypeProcessing, Content: "processing", TokenName: c.GetString("token_name"),
		ModelName: modelName, TokenId: c.GetInt("token_id"), IsStream: isStream,
		Group: group, Ip: c.ClientIP(), RequestId: requestID, Other: common.MapToJsonStr(other),
		LastActivityAt: common.GetTimestamp(),
	}
	if err := createLog(log); err != nil {
		logger.LogError(c, "failed to start request log: "+err.Error())
		return
	}
	c.Set("request_log_started", true)
	startRequestLogHeartbeat(c, requestID)
}

// TouchRequestLog refreshes the lease while the request is still active.
func TouchRequestLog(requestID string) bool {
	if LOG_DB == nil || requestID == "" {
		return false
	}
	result := LOG_DB.Model(&Log{}).
		Where("request_id = ? AND type = ?", requestID, LogTypeProcessing).
		Update("last_activity_at", common.GetTimestamp())
	return result.Error == nil && result.RowsAffected == 1
}

func startRequestLogHeartbeat(c *gin.Context, requestID string) {
	if c == nil || c.Request == nil || requestID == "" {
		return
	}
	go runRequestLogHeartbeat(c.Request.Context(), requestID, staleProcessingLogTimeout/3)
}

func runRequestLogHeartbeat(ctx context.Context, requestID string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !TouchRequestLog(requestID) {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// FinalizeRequestLog updates an existing processing row and returns whether a
// row was found. Callers retain the current fallback behavior when it is false.
func FinalizeRequestLog(c *gin.Context, log *Log) bool {
	if c == nil || LOG_DB == nil || log == nil {
		return false
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		return false
	}
	var processing Log
	if err := LOG_DB.Where("request_id = ? AND type = ?", requestID, LogTypeProcessing).Order("id desc").First(&processing).Error; err != nil {
		return false
	}
	log.RequestId = requestID
	log.Id = processing.Id
	log.CreatedAt = processing.CreatedAt
	log.FinishedAt = common.GetTimestamp()
	if log.Username == "" {
		log.Username = processing.Username
	}
	if log.ModelName == "" {
		log.ModelName = processing.ModelName
	}
	if log.Group == "" {
		log.Group = processing.Group
	}
	other, _ := common.StrToMap(processing.Other)
	if other == nil {
		other = make(map[string]any)
	}
	terminalOther, _ := common.StrToMap(log.Other)
	for key, value := range terminalOther {
		other[key] = value
	}
	if log.Type == LogTypeError {
		other["request_status"] = "failed"
	} else {
		other["request_status"] = "completed"
	}
	log.Other = common.MapToJsonStr(other)
	updates := map[string]any{
		"type": log.Type, "finished_at": log.FinishedAt, "content": log.Content,
		"username": log.Username, "token_name": log.TokenName, "model_name": log.ModelName,
		"quota": log.Quota, "prompt_tokens": log.PromptTokens, "completion_tokens": log.CompletionTokens,
		"use_time": log.UseTime, "is_stream": log.IsStream, "channel_id": log.ChannelId,
		"token_id": log.TokenId, "group": log.Group, "ip": log.Ip, "other": log.Other,
	}
	result := LOG_DB.Model(&Log{}).Where("request_id = ? AND type = ?", requestID, LogTypeProcessing).Updates(updates)
	if result.Error == nil && result.RowsAffected == 1 {
		c.Set("request_log_finalized", true)
		return true
	}
	return false
}

// RecordLoginLog writes new login events to the independent audit table.
// username 由调用方传入（登录流程已持有用户对象），避免额外的数据库查询。
// content 为英文兜底文本（用于导出）；action+params 供前端本地化渲染。
// other 包含 login_method、user_agent 等结构化信息。
func RecordLoginLog(userId, actorRole int, username string, content string, ip string, action string, params map[string]any, other AuditOther, request ...*gin.Context) {
	other.Op = &AuditOperation{Action: action, Params: params}
	var c *gin.Context
	if len(request) > 0 {
		c = request[0]
	}
	RecordAuditLog(c, AuditLog{UserId: userId, Username: username, ActorRole: actorRole, Category: AuditCategoryLogin, Action: action, Content: content, Ip: ip, Other: other, Success: true})
}

// RecordOperationAuditLog writes new operation/security events to the audit table.
// logUserId 为日志归属者，管理审计日志应归属实际操作者；目标资源/用户放入
// action params。username 内部按 logUserId 查询。content 为英文兜底文本（供导出使用）。
// action+params 写入 Other.op，供前端本地化渲染（普通用户可见，不含敏感信息）。
// adminInfo 存放操作者身份（写入 Other.admin_info，普通用户查询时剥离）；
// auditInfo 存放路由/方法/结果等中间件兜底信息（写入 Other.audit_info，普通用户查询时剥离）。
func RecordOperationAuditLog(logUserId, actorRole int, content string, ip string, action string, params map[string]any, adminInfo *AuditAdminInfo, auditInfo *AuditRequestInfo, request ...*gin.Context) {
	username, _ := GetUsernameById(logUserId, false)
	other := AuditOther{
		Op:        &AuditOperation{Action: action, Params: params},
		AdminInfo: adminInfo,
		AuditInfo: auditInfo,
	}
	var c *gin.Context
	if len(request) > 0 {
		c = request[0]
	}
	category := AuditCategoryOperation
	if adminInfo == nil {
		category = AuditCategorySecurity
	}
	status, success := 200, true
	if auditInfo != nil {
		status = auditInfo.Status
		success = auditInfo.Success
	}
	RecordAuditLog(c, AuditLog{UserId: logUserId, Username: username, ActorRole: actorRole, Category: category, Action: action, Content: content, Ip: ip, Status: status, Success: success, Other: other})
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := GetUsernameById(userId, false)
	other := NewLogOther()
	other.MergeAdmin(map[string]any{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	})
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other:     other.JSONString(),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other *LogOther) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	otherStr := other.JSONString()
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	if FinalizeRequestLog(c, log) {
		return
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int       `json:"channel_id"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	ModelName        string    `json:"model_name"`
	TokenName        string    `json:"token_name"`
	Quota            int       `json:"quota"`
	Content          string    `json:"content"`
	TokenId          int       `json:"token_id"`
	UseTimeSeconds   int       `json:"use_time_seconds"`
	IsStream         bool      `json:"is_stream"`
	Group            string    `json:"group"`
	Other            *LogOther `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	createdAt := common.GetTimestamp()
	otherStr := params.Other.JSONString()
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        createdAt,
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	if !common.LogConsumeEnabled && !isModelTestLog(params.TokenName, params.Content) {
		FinalizeRequestLog(c, log)
		accumulateLotteryConsumption(c, userId, params.Quota, requestId)
		return
	}
	if !c.GetBool("request_log_failed") {
		if FinalizeRequestLog(c, log) {
			if common.DataExportEnabled {
				LogQuotaData(QuotaDataLogParams{
					UserID: userId, Username: username, ModelName: params.ModelName,
					Quota: params.Quota, CreatedAt: createdAt,
					TokenUsed: params.PromptTokens + params.CompletionTokens,
					UseGroup:  params.Group, TokenID: params.TokenId,
					ChannelID: params.ChannelId, NodeName: common.NodeName,
				})
			}
			accumulateLotteryConsumption(c, userId, params.Quota, requestId)
			return
		}
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	accumulateLotteryConsumption(c, userId, params.Quota, requestId)
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

func accumulateLotteryConsumption(c *gin.Context, userID, quota int, requestID string) {
	if quota <= 0 || strings.TrimSpace(requestID) == "" || !c.GetBool("billing_settled") {
		return
	}
	if err := AccumulateLotteryConsumption(userID, quota, requestID); err != nil {
		logger.LogError(c, "failed to accumulate lottery consumption: "+err.Error())
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
	Other     *LogOther
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
		Other:     params.Other.JSONString(),
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

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string, scopes ...LogScope) (logs []*Log, total int64, err error) {
	return GetAllLogsEx(logType, startTimestamp, endTimestamp, modelName, username, tokenName, startIdx, num, channel, group, requestId, upstreamRequestId, false, scopes...)
}

func GetAllLogsEx(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string, skipCount bool, scopes ...LogScope) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB
	} else {
		tx = LOG_DB.Where("logs.type = ?", logType)
	}
	scope := LogScopeBusiness
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	tx = applyLogScope(tx, scope, "logs.")

	if scope == LogScopeModelTest && startTimestamp == 0 && endTimestamp == 0 {
		startTimestamp, endTimestamp = ShanghaiDayRange(time.Now())
		endTimestamp--
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
	if !skipCount {
		err = tx.Model(&Log{}).Count(&total).Error
		if err != nil {
			return nil, 0, err
		}
	} else {
		total = -1
	}
	order := logOrder("logs.")
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
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

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string, scopes ...LogScope) (logs []*Log, total int64, err error) {
	return GetUserLogsEx(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, startIdx, num, group, requestId, upstreamRequestId, false, scopes...)
}

func GetUserLogsEx(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string, skipCount bool, scopes ...LogScope) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}
	scope := LogScopeBusiness
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	tx = applyLogScope(tx, scope, "logs.")

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
	if !skipCount {
		err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
		if err != nil {
			common.SysError("failed to count user logs: " + err.Error())
			return nil, 0, errors.New("查询日志失败")
		}
	} else {
		total = -1
	}
	order := logOrder("logs.")
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

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
	CacheReadTpm      int64   `json:"cache_read_tpm"`
	CacheCreateTpm    int64   `json:"cache_create_tpm"`
	CompletionTpm     int64   `json:"completion_tpm"`
	AverageUseTime    float64 `json:"average_use_time"`
	SuccessRate       float64 `json:"success_rate"`
}

func applyLogCommonStatFilters(tx *gorm.DB, username, tokenName, modelName string, channel int, group string, startTimestamp, endTimestamp int64, scope LogScope) (*gorm.DB, error) {
	var err error
	tx = applyLogScope(tx, scope, "")
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
	return tx, nil
}

func applyLogStatFilters(tx *gorm.DB, username, tokenName, modelName string, channel int, group string, startTimestamp, endTimestamp int64, scope LogScope) (*gorm.DB, error) {
	tx, err := applyLogCommonStatFilters(tx, username, tokenName, modelName, channel, group, startTimestamp, endTimestamp, scope)
	if err != nil {
		return tx, err
	}
	return tx.Where("type = ?", LogTypeConsume), nil
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string, scopes ...LogScope) (stat Stat, err error) {
	_ = logType // Usage statistics have historically included consume logs only.
	cacheReadExpr, cacheCreateExpr := logCacheTokenSelectExpr()
	scope := LogScopeBusiness
	if len(scopes) > 0 {
		scope = scopes[0]
	}

	totalSelect := fmt.Sprintf(`
COALESCE(SUM(quota), 0) AS quota,
COUNT(*) AS request_count,
COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
COALESCE(SUM(%s), 0) AS cache_read_tokens,
COALESCE(SUM(%s), 0) AS cache_create_tokens`, cacheReadExpr, cacheCreateExpr)
	totalQuery := LOG_DB.Table("logs").Select(totalSelect)
	if totalQuery, err = applyLogStatFilters(totalQuery, username, tokenName, modelName, channel, group, startTimestamp, endTimestamp, scope); err != nil {
		return stat, err
	}
	if err = totalQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log totals with cache statistics, fallback without cache: " + err.Error())
		totalQuery = LOG_DB.Table("logs").Select(`
COALESCE(SUM(quota), 0) AS quota,
COUNT(*) AS request_count,
COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
0 AS cache_read_tokens,
0 AS cache_create_tokens`)
		if totalQuery, err = applyLogStatFilters(totalQuery, username, tokenName, modelName, channel, group, startTimestamp, endTimestamp, scope); err != nil {
			return stat, err
		}
		if err = totalQuery.Scan(&stat).Error; err != nil {
			common.SysError("failed to query log totals: " + err.Error())
			return stat, errors.New("查询统计数据失败")
		}
	}
	stat.TotalTokens = stat.PromptTokens + stat.CompletionTokens
	if stat.PromptTokens > 0 {
		stat.CacheRate = float64(stat.CacheReadTokens) * 100 / float64(stat.PromptTokens)
	}

	rateSelect := fmt.Sprintf(`
COUNT(*) AS rpm,
COALESCE(SUM(prompt_tokens), 0) + COALESCE(SUM(completion_tokens), 0) AS tpm,
COALESCE(SUM(%s), 0) AS cache_read_tpm,
COALESCE(SUM(%s), 0) AS cache_create_tpm,
COALESCE(SUM(completion_tokens), 0) AS completion_tpm`, cacheReadExpr, cacheCreateExpr)
	type rateStat struct {
		Rpm            int64
		Tpm            int64
		CacheReadTpm   int64
		CacheCreateTpm int64
		CompletionTpm  int64
	}
	var rate rateStat
	rateQuery := LOG_DB.Table("logs").Select(rateSelect)
	rateQuery, err = applyLogStatFilters(rateQuery, username, tokenName, modelName, channel, group, time.Now().Add(-60*time.Second).Unix(), 0, scope)
	if err != nil {
		return stat, err
	}
	if err = rateQuery.Scan(&rate).Error; err != nil {
		common.SysError("failed to query log rates with cache statistics, fallback without cache: " + err.Error())
		rateQuery = LOG_DB.Table("logs").Select(`
COUNT(*) AS rpm,
COALESCE(SUM(prompt_tokens), 0) + COALESCE(SUM(completion_tokens), 0) AS tpm,
0 AS cache_read_tpm,
0 AS cache_create_tpm,
COALESCE(SUM(completion_tokens), 0) AS completion_tpm`)
		rateQuery, err = applyLogStatFilters(rateQuery, username, tokenName, modelName, channel, group, time.Now().Add(-60*time.Second).Unix(), 0, scope)
		if err != nil {
			return stat, err
		}
		if err = rateQuery.Scan(&rate).Error; err != nil {
			common.SysError("failed to query log rates: " + err.Error())
			return stat, errors.New("查询统计数据失败")
		}
	}
	stat.Rpm = rate.Rpm
	stat.Tpm = rate.Tpm
	stat.CacheReadTpm = rate.CacheReadTpm
	stat.CacheCreateTpm = rate.CacheCreateTpm
	stat.CompletionTpm = rate.CompletionTpm

	return stat, nil
}

func GetModelTestStat(now time.Time, logType int, startTimestamp, endTimestamp int64, modelName, username, tokenName string, channel int, group, requestId, upstreamRequestId string) (stat Stat, err error) {
	if startTimestamp == 0 && endTimestamp == 0 {
		startTimestamp, endTimestamp = ShanghaiDayRange(now)
		endTimestamp--
	}
	type modelTestStat struct {
		Quota          int64
		RequestCount   int64
		AverageUseTime float64
		SuccessCount   int64
	}
	var result modelTestStat
	statusCodeExpr := logStatusCodeSelectExpr()
	selectSQL := fmt.Sprintf(`
COALESCE(SUM(quota), 0) AS quota,
COUNT(*) AS request_count,
COALESCE(AVG(use_time), 0) AS average_use_time,
COALESCE(SUM(CASE WHEN %s BETWEEN 200 AND 299 THEN 1 ELSE 0 END), 0) AS success_count`, statusCodeExpr)
	query := LOG_DB.Table("logs").Select(selectSQL)
	if query, err = applyLogCommonStatFilters(query, username, tokenName, modelName, channel, group, startTimestamp, endTimestamp, LogScopeModelTest); err != nil {
		return stat, err
	}
	if requestId != "" {
		query = query.Where("request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		query = query.Where("upstream_request_id = ?", upstreamRequestId)
	}
	if logType != LogTypeUnknown {
		query = query.Where("type = ?", logType)
	}
	if err = query.Scan(&result).Error; err != nil {
		return stat, err
	}
	stat.Quota = result.Quota
	stat.RequestCount = result.RequestCount
	stat.AverageUseTime = result.AverageUseTime
	if result.RequestCount > 0 {
		stat.SuccessRate = float64(result.SuccessCount) * 100 / float64(result.RequestCount)
	}
	return stat, nil
}

func GetModelTestTodayStat(now time.Time) (Stat, error) {
	return GetModelTestStat(now, LogTypeUnknown, 0, 0, "", "", "", 0, "", "", "")
}

func logStatusCodeSelectExpr() string {
	switch common.LogDatabaseType() {
	case common.DatabaseTypeSQLite:
		return "COALESCE(CAST(json_extract(CASE WHEN json_valid(other) THEN other ELSE '{}' END, '$.status_code') AS INTEGER), 0)"
	case common.DatabaseTypeMySQL:
		return "COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(CASE WHEN JSON_VALID(other) THEN other ELSE '{}' END, '$.status_code')) AS SIGNED), 0)"
	case common.DatabaseTypePostgreSQL:
		return "COALESCE(NULLIF(substring(other from '\"status_code\"[[:space:]]*:[[:space:]]*([0-9]+)'), '')::integer, 0)"
	case common.DatabaseTypeClickHouse:
		return "JSONExtractInt(other, 'status_code')"
	default:
		return "0"
	}
}

func logCacheTokenSelectExpr() (cacheRead string, cacheCreate string) {
	return logCacheTokenSelectExprForType(common.LogDatabaseType())
}

func logCacheTokenSelectExprForDB(db *gorm.DB) (cacheRead string, cacheCreate string) {
	return logCacheTokenSelectExprForType(databaseTypeForDB(db, common.LogDatabaseType()))
}

func databaseTypeForDB(db *gorm.DB, fallback common.DatabaseType) common.DatabaseType {
	if db == nil || db.Dialector == nil {
		return fallback
	}
	switch db.Dialector.Name() {
	case "sqlite":
		return common.DatabaseTypeSQLite
	case "mysql":
		return common.DatabaseTypeMySQL
	case "postgres", "postgresql":
		return common.DatabaseTypePostgreSQL
	case "clickhouse":
		return common.DatabaseTypeClickHouse
	default:
		return fallback
	}
}

func logCacheTokenSelectExprForType(databaseType common.DatabaseType) (cacheRead string, cacheCreate string) {
	switch databaseType {
	case common.DatabaseTypeSQLite:
		validOther := "CASE WHEN json_valid(other) THEN other ELSE '{}' END"
		return fmt.Sprintf("COALESCE(CAST(json_extract(%s, '$.cache_tokens') AS INTEGER), 0)", validOther),
			fmt.Sprintf("COALESCE(CAST(json_extract(%[1]s, '$.cache_write_tokens') AS INTEGER), CASE WHEN COALESCE(CAST(json_extract(%[1]s, '$.cache_creation_tokens_5m') AS INTEGER), 0) > 0 OR COALESCE(CAST(json_extract(%[1]s, '$.cache_creation_tokens_1h') AS INTEGER), 0) > 0 THEN COALESCE(CAST(json_extract(%[1]s, '$.cache_creation_tokens_5m') AS INTEGER), 0) + COALESCE(CAST(json_extract(%[1]s, '$.cache_creation_tokens_1h') AS INTEGER), 0) ELSE COALESCE(CAST(json_extract(%[1]s, '$.cache_creation_tokens') AS INTEGER), 0) END, 0)", validOther)
	case common.DatabaseTypeMySQL:
		return "COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_tokens')) AS SIGNED), 0)",
			"COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_write_tokens')) AS SIGNED), CASE WHEN COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_creation_tokens_5m')) AS SIGNED), 0) > 0 OR COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_creation_tokens_1h')) AS SIGNED), 0) > 0 THEN COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_creation_tokens_5m')) AS SIGNED), 0) + COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_creation_tokens_1h')) AS SIGNED), 0) ELSE COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_creation_tokens')) AS SIGNED), 0) END, 0)"
	case common.DatabaseTypePostgreSQL:
		return "COALESCE((other::jsonb->>'cache_tokens')::bigint, 0)",
			"COALESCE((other::jsonb->>'cache_write_tokens')::bigint, CASE WHEN COALESCE((other::jsonb->>'cache_creation_tokens_5m')::bigint, 0) > 0 OR COALESCE((other::jsonb->>'cache_creation_tokens_1h')::bigint, 0) > 0 THEN COALESCE((other::jsonb->>'cache_creation_tokens_5m')::bigint, 0) + COALESCE((other::jsonb->>'cache_creation_tokens_1h')::bigint, 0) ELSE COALESCE((other::jsonb->>'cache_creation_tokens')::bigint, 0) END, 0)"
	case common.DatabaseTypeClickHouse:
		return "JSONExtractInt(other, 'cache_tokens')",
			"coalesce(nullIf(JSONExtractInt(other, 'cache_write_tokens'), 0), if(JSONExtractInt(other, 'cache_creation_tokens_5m') > 0 OR JSONExtractInt(other, 'cache_creation_tokens_1h') > 0, JSONExtractInt(other, 'cache_creation_tokens_5m') + JSONExtractInt(other, 'cache_creation_tokens_1h'), JSONExtractInt(other, 'cache_creation_tokens')))"
	default:
		return "0", "0"
	}
}

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

func GetUsageBreakdown(startTimestamp, endTimestamp int64, userId int, username string, topN int) (*UsageBreakdown, error) {
	if topN <= 0 {
		topN = 8
	}
	cacheReadExpr, cacheCreateExpr := logCacheTokenSelectExpr()
	type row struct {
		ModelName                                                                        string
		Count, Quota, PromptTokens, CompletionTokens, CacheReadTokens, CacheCreateTokens int64
	}
	selectSQL := fmt.Sprintf("model_name, COUNT(*) AS count, COALESCE(SUM(quota), 0) AS quota, COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, COALESCE(SUM(completion_tokens), 0) AS completion_tokens, COALESCE(SUM(%s), 0) AS cache_read_tokens, COALESCE(SUM(%s), 0) AS cache_create_tokens", cacheReadExpr, cacheCreateExpr)
	query := LOG_DB.Table("logs").Select(selectSQL).Where("type = ?", LogTypeConsume)
	if startTimestamp > 0 {
		query = query.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp > 0 {
		query = query.Where("created_at <= ?", endTimestamp)
	}
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if username != "" {
		query = query.Where("username = ?", username)
	}
	var rows []row
	if err := query.Group("model_name").Order("count DESC").Scan(&rows).Error; err != nil {
		fallback := LOG_DB.Table("logs").Select("model_name, COUNT(*) AS count, COALESCE(SUM(quota), 0) AS quota, COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, COALESCE(SUM(completion_tokens), 0) AS completion_tokens, 0 AS cache_read_tokens, 0 AS cache_create_tokens").Where("type = ?", LogTypeConsume)
		if startTimestamp > 0 {
			fallback = fallback.Where("created_at >= ?", startTimestamp)
		}
		if endTimestamp > 0 {
			fallback = fallback.Where("created_at <= ?", endTimestamp)
		}
		if userId > 0 {
			fallback = fallback.Where("user_id = ?", userId)
		}
		if username != "" {
			fallback = fallback.Where("username = ?", username)
		}
		if fallbackErr := fallback.Group("model_name").Order("count DESC").Scan(&rows).Error; fallbackErr != nil {
			return nil, fallbackErr
		}
	}
	out := &UsageBreakdown{Models: make([]UsageModelStat, 0, min(topN, len(rows)))}
	for i, item := range rows {
		total := item.PromptTokens + item.CompletionTokens
		rate := 0.0
		if item.PromptTokens > 0 {
			rate = float64(item.CacheReadTokens) * 100 / float64(item.PromptTokens)
		}
		out.RequestCount += item.Count
		out.Quota += item.Quota
		out.PromptTokens += item.PromptTokens
		out.CompletionTokens += item.CompletionTokens
		out.CacheReadTokens += item.CacheReadTokens
		out.CacheCreateTokens += item.CacheCreateTokens
		out.TotalTokens += total
		if i < topN {
			name := item.ModelName
			if name == "" {
				name = "(unknown)"
			}
			out.Models = append(out.Models, UsageModelStat{ModelName: name, Count: item.Count, Quota: item.Quota, PromptTokens: item.PromptTokens, CompletionTokens: item.CompletionTokens, CacheReadTokens: item.CacheReadTokens, CacheCreateTokens: item.CacheCreateTokens, TotalTokens: total, CacheRate: rate})
		}
	}
	if out.PromptTokens > 0 {
		out.CacheRate = float64(out.CacheReadTokens) * 100 / float64(out.PromptTokens)
	}
	return out, nil
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
