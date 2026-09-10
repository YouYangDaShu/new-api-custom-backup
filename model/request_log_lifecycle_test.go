package model

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requestLogTestContext(t *testing.T, requestID string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set(common.RequestIdKey, requestID)
	c.Set("id", 991001)
	c.Set("username", "request-log-test")
	c.Set("token_name", "test-token")
	c.Set("token_id", 991002)
	c.Set("group", "default")
	return c
}

func cleanupRequestLogRows(t *testing.T, requestIDs ...string) {
	t.Helper()
	require.NoError(t, LOG_DB.Where("request_id IN ?", requestIDs).Delete(&Log{}).Error)
	t.Cleanup(func() {
		_ = LOG_DB.Where("request_id IN ?", requestIDs).Delete(&Log{}).Error
	})
}

func TestRequestLogLifecycleFinalizesSingleRow(t *testing.T) {
	requestID := fmt.Sprintf("request-log-lifecycle-%d", time.Now().UnixNano())
	cleanupRequestLogRows(t, requestID)
	c := requestLogTestContext(t, requestID)

	StartRequestLog(c, "gpt-5.6-sol", true, "default")
	require.True(t, c.GetBool("request_log_started"))

	var processing Log
	require.NoError(t, LOG_DB.Where("request_id = ?", requestID).First(&processing).Error)
	assert.Equal(t, LogTypeProcessing, processing.Type)
	assert.Zero(t, processing.FinishedAt)

	terminal := &Log{
		Type: LogTypeConsume, Content: "completed", PromptTokens: 300000,
		CompletionTokens: 42, Quota: 1234, UseTime: 7,
		Other: common.MapToJsonStr(map[string]any{"status_code": 200}),
	}
	require.True(t, FinalizeRequestLog(c, terminal))

	var rows []Log
	require.NoError(t, LOG_DB.Where("request_id = ?", requestID).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, LogTypeConsume, rows[0].Type)
	assert.Positive(t, rows[0].FinishedAt)
	assert.Equal(t, processing.CreatedAt, rows[0].CreatedAt)
	assert.Equal(t, 300000, rows[0].PromptTokens)
	assert.Contains(t, rows[0].Other, `"request_status":"completed"`)
	assert.True(t, c.GetBool("request_log_finalized"))
}

func TestRequestLogOrderingPinsProcessingThenUsesFinishedAt(t *testing.T) {
	prefix := fmt.Sprintf("request-log-order-%d", time.Now().UnixNano())
	ids := []string{prefix + "-processing", prefix + "-new-finish", prefix + "-old-finish", prefix + "-fallback"}
	cleanupRequestLogRows(t, ids...)

	logs := []*Log{
		{UserId: 991001, RequestId: ids[0], Type: LogTypeProcessing, CreatedAt: 100, Content: "processing"},
		{UserId: 991001, RequestId: ids[1], Type: LogTypeConsume, CreatedAt: 10, FinishedAt: 400, Content: "new finish"},
		{UserId: 991001, RequestId: ids[2], Type: LogTypeConsume, CreatedAt: 300, FinishedAt: 350, Content: "old finish"},
		{UserId: 991001, RequestId: ids[3], Type: LogTypeConsume, CreatedAt: 375, FinishedAt: 0, Content: "fallback"},
	}
	for _, row := range logs {
		require.NoError(t, LOG_DB.Create(row).Error)
	}

	var got []Log
	require.NoError(t, LOG_DB.Where("request_id IN ?", ids).Order(logOrder("")).Find(&got).Error)
	require.Len(t, got, 4)
	assert.Equal(t, []string{ids[0], ids[1], ids[3], ids[2]}, []string{got[0].RequestId, got[1].RequestId, got[2].RequestId, got[3].RequestId})
}

func TestRequestLogLifecycleClosesOnlyExpiredLease(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	prefix := fmt.Sprintf("request-log-timeout-%d", time.Now().UnixNano())
	staleID := prefix + "-stale"
	freshID := prefix + "-fresh"
	cleanupRequestLogRows(t, staleID, freshID)

	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 991001, RequestId: staleID, Type: LogTypeProcessing,
		Content: "processing", CreatedAt: now.Add(-staleProcessingLogTimeout - time.Second).Unix(),
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 991001, RequestId: freshID, Type: LogTypeProcessing,
		Content: "processing", CreatedAt: now.Unix(), LastActivityAt: now.Unix(),
	}).Error)

	closed, err := CloseStaleProcessingLogs(now)
	require.NoError(t, err)
	require.Equal(t, int64(1), closed)

	var stale, fresh Log
	require.NoError(t, LOG_DB.Where("request_id = ?", staleID).First(&stale).Error)
	require.NoError(t, LOG_DB.Where("request_id = ?", freshID).First(&fresh).Error)
	assert.Equal(t, LogTypeError, stale.Type)
	assert.Equal(t, now.Unix(), stale.FinishedAt)
	assert.Contains(t, stale.Other, `"request_status":"timeout"`)
	assert.Equal(t, LogTypeProcessing, fresh.Type)
}

func TestRequestLogLifecycleHeartbeatProtectsActiveRequest(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	requestID := fmt.Sprintf("request-log-heartbeat-%d", time.Now().UnixNano())
	cleanupRequestLogRows(t, requestID)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 991001, RequestId: requestID, Type: LogTypeProcessing,
		Content: "processing", CreatedAt: now.Add(-2 * staleProcessingLogTimeout).Unix(),
		LastActivityAt: now.Add(-2 * staleProcessingLogTimeout).Unix(),
	}).Error)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runRequestLogHeartbeat(ctx, requestID, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		var row Log
		return LOG_DB.Where("request_id = ?", requestID).First(&row).Error == nil && row.LastActivityAt >= now.Unix()
	}, time.Second, 10*time.Millisecond)

	closed, err := CloseStaleProcessingLogs(now)
	require.NoError(t, err)
	assert.Zero(t, closed)
	var row Log
	require.NoError(t, LOG_DB.Where("request_id = ?", requestID).First(&row).Error)
	assert.Equal(t, LogTypeProcessing, row.Type)
}
