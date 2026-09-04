package model

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestGinContext(requestId string, userId int) *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request = req
	c.Set(common.RequestIdKey, requestId)
	c.Set("id", userId)
	c.Set("username", "tester")
	c.Set("token_name", "tok")
	c.Set("token_id", 7)
	c.Set("group", "default")
	c.Set("original_model", "test-model")
	return c
}

func TestRequestLifecycle_RecordsIPForEveryUser(t *testing.T) {
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	const reqID = "req-life-real-ip"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})
	t.Cleanup(func() { LOG_DB.Where("request_id = ?", reqID).Delete(&Log{}) })

	c := newTestGinContext(reqID, 99899)
	c.Request.Header.Set("X-Forwarded-For", "203.0.113.9, 172.18.0.2")
	StartRequestLog(c, "gpt-test", true, "default")
	var log Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).First(&log).Error)
	require.Equal(t, "203.0.113.9", log.Ip)
}

func TestRequestLifecycle_StartThenFinalizeSuccess(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	common.LogConsumeEnabled = true

	reqID := "req-life-success-1"
	// cleanup
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})

	c := newTestGinContext(reqID, 42)
	StartRequestLog(c, "glm-test", true, "default")
	require.True(t, c.GetBool("request_log_started"))

	var processing []Log
	require.NoError(t, LOG_DB.Where("request_id = ? AND type = ?", reqID, LogTypeProcessing).Find(&processing).Error)
	require.Len(t, processing, 1)
	assert.Equal(t, "processing", processing[0].Content)

	// success finalize via RecordConsumeLog path
	RecordConsumeLog(c, 42, RecordConsumeLogParams{
		ChannelId:        9,
		PromptTokens:     100,
		CompletionTokens: 20,
		ModelName:        "glm-test",
		TokenName:        "tok",
		Quota:            123,
		Content:          "ok",
		TokenId:          7,
		UseTimeSeconds:   3,
		IsStream:         true,
		Group:            "default",
		Other:            map[string]interface{}{"status_code": 200},
	})
	require.True(t, c.GetBool("request_log_finalized"))

	var rows []Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).Find(&rows).Error)
	// still one row (updated, not duplicated)
	require.Len(t, rows, 1)
	assert.Equal(t, LogTypeConsume, rows[0].Type)
	assert.Equal(t, 123, rows[0].Quota)
	assert.Equal(t, 100, rows[0].PromptTokens)
	assert.Equal(t, 20, rows[0].CompletionTokens)
	other, err := common.StrToMap(rows[0].Other)
	require.NoError(t, err)
	assert.Equal(t, float64(200), other["status_code"])
	assert.Equal(t, "completed", other["request_status"])
}

func TestRequestLifecycle_PendingBillingRemainsPendingOnZeroQuotaFinalize(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	const reqID = "req-life-pending-zero"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})
	t.Cleanup(func() { LOG_DB.Where("request_id = ?", reqID).Delete(&Log{}) })
	c := newTestGinContext(reqID, 43)
	StartRequestLog(c, "gpt-test", true, "default")
	RecordConsumeLog(c, 43, RecordConsumeLogParams{
		ModelName: "gpt-test", TokenId: 7, IsStream: true, Group: "default", Other: map[string]interface{}{
			"billing_status": "pending", "usage_source": "missing", "status_code": 200,
		},
	})
	var log Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).First(&log).Error)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	assert.Equal(t, "pending", other["billing_status"])
}

func TestRequestLifecycle_ClosesOnlyStalePendingRows(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))

	now := time.Now().Truncate(time.Second)
	staleID := "req-life-stale-processing"
	freshID := "req-life-fresh-processing"
	chargedID := "req-life-charged-processing"
	LOG_DB.Where("request_id IN ?", []string{staleID, freshID, chargedID}).Delete(&Log{})
	pending := common.MapToJsonStr(map[string]interface{}{"billing_status": "pending"})
	require.NoError(t, createLog(&Log{RequestId: staleID, Type: LogTypeProcessing, Content: "processing", Quota: 0, CreatedAt: now.Add(-staleProcessingLogTimeout - time.Second).Unix(), Other: pending}))
	require.NoError(t, createLog(&Log{RequestId: freshID, Type: LogTypeProcessing, Content: "processing", Quota: 0, CreatedAt: now.Unix(), Other: pending}))
	require.NoError(t, createLog(&Log{RequestId: chargedID, Type: LogTypeProcessing, Content: "processing", Quota: 1, CreatedAt: now.Add(-staleProcessingLogTimeout - time.Second).Unix(), Other: pending}))

	closed, err := CloseStaleProcessingLogs(now)
	require.NoError(t, err)
	require.GreaterOrEqual(t, closed, int64(1))

	var stale, fresh, charged Log
	require.NoError(t, LOG_DB.Where("request_id = ?", staleID).First(&stale).Error)
	require.NoError(t, LOG_DB.Where("request_id = ?", freshID).First(&fresh).Error)
	require.NoError(t, LOG_DB.Where("request_id = ?", chargedID).First(&charged).Error)
	assert.Equal(t, LogTypeError, stale.Type)
	assert.Contains(t, stale.Content, "408")
	assert.Equal(t, LogTypeProcessing, fresh.Type)
	assert.Equal(t, LogTypeProcessing, charged.Type)
}

func TestRequestLifecycle_ActiveLeaseSurvivesOldTimeout(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	now := time.Now().Truncate(time.Second)
	const reqID = "req-life-active-lease"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})
	t.Cleanup(func() { LOG_DB.Where("request_id = ?", reqID).Delete(&Log{}) })
	pending := common.MapToJsonStr(map[string]interface{}{"billing_status": "pending"})
	require.NoError(t, createLog(&Log{RequestId: reqID, Type: LogTypeProcessing, Content: "processing", Quota: 0,
		CreatedAt: now.Add(-2 * staleProcessingLogTimeout).Unix(), LastActivityAt: now.Add(-2 * staleProcessingLogTimeout).Unix(), Other: pending}))
	heartbeatCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runRequestLogHeartbeat(heartbeatCtx, reqID, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		var row Log
		return LOG_DB.Where("request_id = ?", reqID).First(&row).Error == nil && row.LastActivityAt >= now.Add(-time.Second).Unix()
	}, time.Second, 10*time.Millisecond)

	_, err := CloseStaleProcessingLogs(now)
	require.NoError(t, err)
	var row Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).First(&row).Error)
	assert.Equal(t, LogTypeProcessing, row.Type)
	assert.True(t, TouchRequestLog(reqID))
}

func TestRequestLifecycle_FinalizeIsIdempotent(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	common.LogConsumeEnabled = true
	const reqID = "req-life-finalize-idempotent"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})
	t.Cleanup(func() { LOG_DB.Where("request_id = ?", reqID).Delete(&Log{}) })
	c := newTestGinContext(reqID, 99898)
	StartRequestLog(c, "gpt-test", false, "default")
	require.True(t, FinalizeRequestLog(c, FinalizeRequestLogParams{Success: true, Quota: 77, Content: "ok"}))
	assert.False(t, FinalizeRequestLog(c, FinalizeRequestLogParams{Success: true, Quota: 99, Content: "duplicate"}))
	var rows []Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, 77, rows[0].Quota)
}

func TestRequestLifecycle_BillingClaimIsPersistentAndSingleWinner(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	const reqID = "req-life-billing-claim"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})
	t.Cleanup(func() { LOG_DB.Where("request_id = ?", reqID).Delete(&Log{}) })
	c := newTestGinContext(reqID, 99123)
	StartRequestLog(c, "claim-model", true, "default")
	ctxs := make([]*gin.Context, 2)
	for i := range ctxs {
		ctxs[i] = newTestGinContext(reqID, 99123)
	}
	var wg sync.WaitGroup
	results := make([]bool, 2)
	for i := range ctxs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], _ = ClaimRequestBilling(ctxs[i], 42, 77, "provider") }(i)
	}
	wg.Wait()
	assert.NotEqual(t, results[0], results[1])
	var row Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).First(&row).Error)
	assert.Equal(t, BillingStatusSettling, row.BillingStatus)
	assert.Equal(t, 77, row.PreConsumedQuota)
}

func TestRequestLifecycle_PendingSurvivesFinalizeAndStaleCloser(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	const reqID = "req-life-billing-pending"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})
	t.Cleanup(func() { LOG_DB.Where("request_id = ?", reqID).Delete(&Log{}) })
	c := newTestGinContext(reqID, 99124)
	StartRequestLog(c, "pending-model", true, "default")
	require.True(t, MarkRequestBillingPending(c, 88, "missing"))
	assert.True(t, FinalizeRequestLog(c, FinalizeRequestLogParams{Success: true, Content: "completed"}))
	var row Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).First(&row).Error)
	assert.Equal(t, BillingStatusPending, row.BillingStatus)
	_, err := CloseStaleProcessingLogs(time.Now().Add(24 * time.Hour))
	require.NoError(t, err)
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).First(&row).Error)
	assert.Equal(t, BillingStatusPending, row.BillingStatus)
}

func TestRequestLifecycle_ClientCancellationReleasesLease(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	original := common.ProcessingLogTimeoutMinutes
	common.ProcessingLogTimeoutMinutes = 1
	t.Cleanup(func() { common.ProcessingLogTimeoutMinutes = original })
	const reqID = "req-life-client-cancel"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})
	t.Cleanup(func() { LOG_DB.Where("request_id = ?", reqID).Delete(&Log{}) })
	c := newTestGinContext(reqID, 99897)
	requestCtx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(requestCtx)
	StartRequestLog(c, "gpt-test", true, "default")
	cancel()

	staleAt := time.Now().Add(-2 * time.Minute).Unix()
	require.NoError(t, LOG_DB.Model(&Log{}).Where("request_id = ?", reqID).Update("last_activity_at", staleAt).Error)
	closed, err := CloseStaleProcessingLogs(time.Now())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, closed, int64(1))
	var row Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).First(&row).Error)
	assert.Equal(t, LogTypeError, row.Type)
}

func TestRequestLifecycle_UsesConfiguredProcessingTimeout(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))

	original := common.ProcessingLogTimeoutMinutes
	common.ProcessingLogTimeoutMinutes = 30
	t.Cleanup(func() { common.ProcessingLogTimeoutMinutes = original })

	now := time.Now().Truncate(time.Second)
	insideID := "req-life-config-inside"
	outsideID := "req-life-config-outside"
	LOG_DB.Where("request_id IN ?", []string{insideID, outsideID}).Delete(&Log{})
	pending := common.MapToJsonStr(map[string]interface{}{"billing_status": "pending"})
	require.NoError(t, createLog(&Log{RequestId: insideID, Type: LogTypeProcessing, Content: "processing", Quota: 0, CreatedAt: now.Add(-29 * time.Minute).Unix(), Other: pending}))
	require.NoError(t, createLog(&Log{RequestId: outsideID, Type: LogTypeProcessing, Content: "processing", Quota: 0, CreatedAt: now.Add(-31 * time.Minute).Unix(), Other: pending}))

	_, err := CloseStaleProcessingLogs(now)
	require.NoError(t, err)

	var inside, outside Log
	require.NoError(t, LOG_DB.Where("request_id = ?", insideID).First(&inside).Error)
	require.NoError(t, LOG_DB.Where("request_id = ?", outsideID).First(&outside).Error)
	assert.Equal(t, LogTypeProcessing, inside.Type)
	assert.Equal(t, LogTypeError, outside.Type)
}

func TestRequestLifecycle_ClosesLegacyEmptyOtherPendingRows(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))

	original := common.ProcessingLogTimeoutMinutes
	common.ProcessingLogTimeoutMinutes = 15
	t.Cleanup(func() { common.ProcessingLogTimeoutMinutes = original })

	now := time.Now().Truncate(time.Second)
	reqID := "req-life-legacy-empty-other"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})
	require.NoError(t, createLog(&Log{RequestId: reqID, Type: LogTypeProcessing, Content: "processing", Quota: 0, CreatedAt: now.Add(-16 * time.Minute).Unix(), Other: ""}))

	_, err := CloseStaleProcessingLogs(now)
	require.NoError(t, err)

	var row Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).First(&row).Error)
	assert.Equal(t, LogTypeError, row.Type)
	assert.Contains(t, row.Content, "408")
}

func TestRequestLifecycle_FinalizeFallsBackAfterStaleCloserWins(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	common.LogConsumeEnabled = true

	reqID := "req-life-stale-race"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})
	t.Cleanup(func() { LOG_DB.Where("request_id = ?", reqID).Delete(&Log{}) })
	pending := common.MapToJsonStr(map[string]interface{}{"billing_status": "pending"})
	require.NoError(t, createLog(&Log{
		RequestId: reqID, UserId: 45, Username: "tester", Type: LogTypeProcessing,
		Content: "processing", Quota: 0, CreatedAt: time.Now().Add(-time.Hour).Unix(), Other: pending,
	}))

	closed, err := CloseStaleProcessingLogs(time.Now())
	require.NoError(t, err)
	require.GreaterOrEqual(t, closed, int64(1))

	c := newTestGinContext(reqID, 45)
	RecordConsumeLog(c, 45, RecordConsumeLogParams{
		ChannelId: 1, ModelName: "gpt-test", Quota: 123, Content: "charged after slow request",
		Group: "default", Other: map[string]interface{}{"status_code": 200},
	})

	var rows []Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).Order("id asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, LogTypeError, rows[0].Type)
	assert.Equal(t, LogTypeConsume, rows[1].Type)
	assert.Equal(t, 123, rows[1].Quota)
}

func TestRequestLifecycle_FailureFinalize(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))

	reqID := "req-life-fail-1"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})

	c := newTestGinContext(reqID, 43)
	StartRequestLog(c, "glm-test", false, "default")
	ok := FinalizeRequestLog(c, FinalizeRequestLogParams{
		Success:        false,
		StatusCode:     503,
		ModelName:      "glm-test",
		Content:        "busy",
		UseTimeSeconds: 2,
		ForceType:      LogTypeError,
		Other: map[string]interface{}{
			"request_status": "failed",
			"billing_status": "no_charge",
		},
	})
	require.True(t, ok)
	require.True(t, c.GetBool("request_log_finalized"))

	var rows []Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, LogTypeError, rows[0].Type)
	assert.Equal(t, "busy", rows[0].Content)
	other, err := common.StrToMap(rows[0].Other)
	require.NoError(t, err)
	assert.Equal(t, float64(503), other["status_code"])
	assert.Equal(t, "failed", other["request_status"])
}

func TestRequestLifecycle_ViolationFeeDoesNotOverwriteMain(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	common.LogConsumeEnabled = true

	reqID := "req-life-violation-1"
	LOG_DB.Where("request_id = ?", reqID).Delete(&Log{})

	c := newTestGinContext(reqID, 44)
	StartRequestLog(c, "grok", false, "default")
	// fail main first
	require.True(t, FinalizeRequestLog(c, FinalizeRequestLogParams{
		Success:    false,
		StatusCode: 400,
		Content:    "policy",
		ForceType:  LogTypeError,
	}))
	// violation fee should insert separate consume, not flip main row
	RecordConsumeLog(c, 44, RecordConsumeLogParams{
		ChannelId: 1,
		ModelName: "grok",
		TokenName: "tok",
		Quota:     50,
		Content:   "violation fee",
		TokenId:   7,
		Group:     "default",
		Other: map[string]interface{}{
			"violation_fee": true,
			"status_code":   200,
		},
	})

	var rows []Log
	require.NoError(t, LOG_DB.Where("request_id = ?", reqID).Order("id asc").Find(&rows).Error)
	require.GreaterOrEqual(t, len(rows), 2)
	// first row remains error
	assert.Equal(t, LogTypeError, rows[0].Type)
	// second is consume violation
	foundViolation := false
	for _, r := range rows {
		if r.Type == LogTypeConsume && r.Content == "violation fee" {
			foundViolation = true
		}
	}
	assert.True(t, foundViolation)
}

func TestGetAllLogs_PinsProcessingThenRestoresTimeOrder(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))

	username := "pin-processing-admin"
	LOG_DB.Where("username = ?", username).Delete(&Log{})
	t.Cleanup(func() { LOG_DB.Where("username = ?", username).Delete(&Log{}) })

	require.NoError(t, createLog(&Log{
		UserId: 1, Username: username, CreatedAt: 300, Type: LogTypeConsume,
		Content: "newest completed", RequestId: "pin-admin-new",
	}))
	require.NoError(t, createLog(&Log{
		UserId: 1, Username: username, CreatedAt: 100, Type: LogTypeProcessing,
		Content: "processing", RequestId: "pin-admin-processing",
	}))
	require.NoError(t, createLog(&Log{
		UserId: 1, Username: username, CreatedAt: 200, Type: LogTypeConsume,
		Content: "older completed", RequestId: "pin-admin-old",
	}))

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", username, "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 3)
	assert.Equal(t, "pin-admin-processing", logs[0].RequestId)
	assert.Equal(t, "pin-admin-new", logs[1].RequestId)
	assert.Equal(t, "pin-admin-old", logs[2].RequestId)

	firstPage, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", username, "", 0, 1, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	assert.Equal(t, "pin-admin-processing", firstPage[0].RequestId)

	require.NoError(t, LOG_DB.Model(&Log{}).
		Where("request_id = ?", "pin-admin-processing").
		Updates(map[string]interface{}{"type": LogTypeConsume, "content": "completed"}).Error)

	logs, _, err = GetAllLogs(LogTypeUnknown, 0, 0, "", username, "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 3)
	assert.Equal(t, "pin-admin-new", logs[0].RequestId)
	assert.Equal(t, "pin-admin-old", logs[1].RequestId)
	assert.Equal(t, "pin-admin-processing", logs[2].RequestId)
}

func TestGetUserLogs_PinsProcessingThenRestoresTimeOrder(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))

	const userID = 99871
	LOG_DB.Where("user_id = ?", userID).Delete(&Log{})
	t.Cleanup(func() { LOG_DB.Where("user_id = ?", userID).Delete(&Log{}) })

	require.NoError(t, createLog(&Log{
		UserId: userID, Username: "pin-user", CreatedAt: 300, Type: LogTypeConsume,
		Content: "newest completed", RequestId: "pin-user-new",
	}))
	require.NoError(t, createLog(&Log{
		UserId: userID, Username: "pin-user", CreatedAt: 100, Type: LogTypeProcessing,
		Content: "processing", RequestId: "pin-user-processing",
	}))
	require.NoError(t, createLog(&Log{
		UserId: userID, Username: "pin-user", CreatedAt: 200, Type: LogTypeConsume,
		Content: "older completed", RequestId: "pin-user-old",
	}))

	logs, _, err := GetUserLogs(userID, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 3)
	assert.Equal(t, "pin-user-processing", logs[0].RequestId)
	assert.Equal(t, "pin-user-new", logs[1].RequestId)
	assert.Equal(t, "pin-user-old", logs[2].RequestId)

	firstPage, _, err := GetUserLogs(userID, LogTypeUnknown, 0, 0, "", "", 0, 1, "", "", "")
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	assert.Equal(t, "pin-user-processing", firstPage[0].RequestId)

	require.NoError(t, LOG_DB.Model(&Log{}).
		Where("request_id = ?", "pin-user-processing").
		Updates(map[string]interface{}{"type": LogTypeConsume, "content": "completed"}).Error)

	logs, _, err = GetUserLogs(userID, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 3)
	assert.Equal(t, "pin-user-new", logs[0].RequestId)
	assert.Equal(t, "pin-user-old", logs[1].RequestId)
	assert.Equal(t, "pin-user-processing", logs[2].RequestId)
}

func TestSumUsedQuota_OnlyConsumeAndCacheRate(t *testing.T) {
	if LOG_DB == nil {
		t.Skip("LOG_DB not initialized")
	}
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))

	// isolated markers
	LOG_DB.Where("username = ?", "stat-user-x").Delete(&Log{})
	now := common.GetTimestamp()
	// consume with cache in other
	require.NoError(t, createLog(&Log{
		UserId: 1, Username: "stat-user-x", CreatedAt: now, Type: LogTypeConsume,
		Quota: 1000, PromptTokens: 100, CompletionTokens: 10,
		Other: `{"cache_tokens":80}`,
	}))
	// processing should be ignored by stats
	require.NoError(t, createLog(&Log{
		UserId: 1, Username: "stat-user-x", CreatedAt: now, Type: LogTypeProcessing,
		Quota: 0, PromptTokens: 999, CompletionTokens: 999,
	}))
	// error ignored
	require.NoError(t, createLog(&Log{
		UserId: 1, Username: "stat-user-x", CreatedAt: now, Type: LogTypeError,
		Quota: 0, PromptTokens: 1, CompletionTokens: 1,
	}))

	stat, err := SumUsedQuota(0, now-10, now+10, "", "stat-user-x", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, int64(1000), stat.Quota)
	assert.Equal(t, int64(1), stat.RequestCount)
	assert.Equal(t, int64(100), stat.PromptTokens)
	assert.Equal(t, int64(10), stat.CompletionTokens)
	assert.Equal(t, int64(110), stat.TotalTokens)
	assert.Equal(t, int64(80), stat.CacheReadTokens)
	assert.InDelta(t, 80.0, stat.CacheRate, 0.01)
}
