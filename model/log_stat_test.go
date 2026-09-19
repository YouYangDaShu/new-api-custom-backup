package model

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSumUsedQuotaReturnsFilteredTotalsAndRecentRates(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	matching := []Log{
		{Username: "alice", TokenName: "token-a", ModelName: "gpt-a", ChannelId: 3, Group: "paid", Type: LogTypeConsume, CreatedAt: now - 10, Quota: 100, PromptTokens: 100, CompletionTokens: 20, Other: `{"cache_tokens":30,"cache_creation_tokens":10,"cache_write_tokens":12}`},
		{Username: "alice", TokenName: "token-a", ModelName: "gpt-a", ChannelId: 3, Group: "paid", Type: LogTypeConsume, CreatedAt: now - 120, Quota: 50, PromptTokens: 200, CompletionTokens: 30, Other: `{"cache_tokens":40,"cache_creation_tokens":7}`},
	}
	rejectedByFilters := []Log{
		{Username: "bob", TokenName: "token-a", ModelName: "gpt-a", ChannelId: 3, Group: "paid", Type: LogTypeConsume, CreatedAt: now - 10, Quota: 1},
		{Username: "alice", TokenName: "token-b", ModelName: "gpt-a", ChannelId: 3, Group: "paid", Type: LogTypeConsume, CreatedAt: now - 10, Quota: 1},
		{Username: "alice", TokenName: "token-a", ModelName: "gpt-b", ChannelId: 3, Group: "paid", Type: LogTypeConsume, CreatedAt: now - 10, Quota: 1},
		{Username: "alice", TokenName: "token-a", ModelName: "gpt-a", ChannelId: 4, Group: "paid", Type: LogTypeConsume, CreatedAt: now - 10, Quota: 1},
		{Username: "alice", TokenName: "token-a", ModelName: "gpt-a", ChannelId: 3, Group: "free", Type: LogTypeConsume, CreatedAt: now - 10, Quota: 1},
		{Username: "alice", TokenName: "token-a", ModelName: "gpt-a", ChannelId: 3, Group: "paid", Type: LogTypeConsume, CreatedAt: now - 400, Quota: 1},
		{Username: "alice", TokenName: "token-a", ModelName: "gpt-a", ChannelId: 3, Group: "paid", Type: LogTypeError, CreatedAt: now - 10, Quota: 1},
	}
	require.NoError(t, LOG_DB.Create(append(matching, rejectedByFilters...)).Error)

	stat, err := SumUsedQuota(LogTypeUnknown, now-300, now, "gpt-a", "alice", "token-a", 3, "paid")
	require.NoError(t, err)

	assert.Equal(t, int64(150), stat.Quota)
	assert.Equal(t, int64(2), stat.RequestCount)
	assert.Equal(t, int64(300), stat.PromptTokens)
	assert.Equal(t, int64(50), stat.CompletionTokens)
	assert.Equal(t, int64(350), stat.TotalTokens)
	assert.Equal(t, int64(70), stat.CacheReadTokens)
	assert.Equal(t, int64(19), stat.CacheCreateTokens)
	assert.InDelta(t, 23.333333, stat.CacheRate, 0.000001)
	assert.Equal(t, int64(1), stat.Rpm)
	assert.Equal(t, int64(120), stat.Tpm)
	assert.Equal(t, int64(30), stat.CacheReadTpm)
	assert.Equal(t, int64(12), stat.CacheCreateTpm)
	assert.Equal(t, int64(20), stat.CompletionTpm)
}

func TestSumUsedQuotaFallsBackWhenCacheJSONExpressionFails(t *testing.T) {
	truncateTables(t)
	originalDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() { common.SetLogDatabaseType(originalDatabaseType) })
	common.SetLogDatabaseType(common.DatabaseTypePostgreSQL)

	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&Log{Username: "alice", Type: LogTypeConsume, CreatedAt: now - 10, Quota: 5, PromptTokens: 10, CompletionTokens: 2, Other: `{"cache_tokens":4}`}).Error)

	stat, err := SumUsedQuota(LogTypeUnknown, now-60, now, "", "alice", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, int64(5), stat.Quota)
	assert.Equal(t, int64(1), stat.RequestCount)
	assert.Equal(t, int64(12), stat.TotalTokens)
	assert.Zero(t, stat.CacheReadTokens)
	assert.Zero(t, stat.CacheCreateTokens)
	assert.Zero(t, stat.CacheRate)
	assert.Equal(t, int64(1), stat.Rpm)
	assert.Equal(t, int64(12), stat.Tpm)
	assert.Zero(t, stat.CacheReadTpm)
	assert.Zero(t, stat.CacheCreateTpm)
}

func TestLogScopesAreMutuallyExclusiveForListsAndStats(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	rows := []Log{
		{UserId: 1, Username: "alice", TokenName: "business", Content: "business", Type: LogTypeConsume, CreatedAt: now, Quota: 10},
		{UserId: 1, Username: "alice", TokenName: modelTestLogMark, Content: "business", Type: LogTypeConsume, CreatedAt: now, Quota: 20},
		{UserId: 1, Username: "alice", TokenName: "business", Content: modelTestLogMark, Type: LogTypeConsume, CreatedAt: now, Quota: 30},
		{UserId: 2, Username: "bob", TokenName: modelTestLogMark, Content: modelTestLogMark, Type: LogTypeConsume, CreatedAt: now, Quota: 40},
	}
	require.NoError(t, LOG_DB.Create(&rows).Error)

	business, businessTotal, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, businessTotal)
	require.Equal(t, "business", business[0].TokenName)

	tests, testTotal, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "", "", LogScopeModelTest)
	require.NoError(t, err)
	require.EqualValues(t, 3, testTotal)
	require.Len(t, tests, 3)

	self, selfTotal, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, selfTotal)
	require.Len(t, self, 1)

	businessStat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "", "", "", 0, "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, businessStat.RequestCount)
	assert.EqualValues(t, 10, businessStat.Quota)

	testStat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "", "", "", 0, "", LogScopeModelTest)
	require.NoError(t, err)
	assert.EqualValues(t, 3, testStat.RequestCount)
	assert.EqualValues(t, 90, testStat.Quota)
}

func TestBusinessLogScopeKeepsNullLegacyFields(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Exec("INSERT INTO logs (user_id, created_at, type, token_name, content) VALUES (?, ?, ?, NULL, NULL)", 1, time.Now().Unix(), LogTypeConsume).Error)

	logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
}

func TestRecordConsumeLogKeepsModelTestWhenConsumptionLoggingDisabled(t *testing.T) {
	truncateTables(t)
	originalEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = originalEnabled })

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set(common.RequestIdKey, "model-test-log-disabled-consume")

	RecordConsumeLog(ctx, 991001, RecordConsumeLogParams{
		ModelName: "test-model",
		TokenName: modelTestLogMark,
		Content:   modelTestLogMark,
		Other:     NewLogOther(),
	})

	var log Log
	require.NoError(t, LOG_DB.Where("request_id = ?", "model-test-log-disabled-consume").First(&log).Error)
	assert.Equal(t, LogTypeConsume, log.Type)
	assert.Equal(t, modelTestLogMark, log.TokenName)
	assert.Equal(t, modelTestLogMark, log.Content)
}

func TestGetModelTestTodayStat(t *testing.T) {
	truncateTables(t)
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, loc)
	dayStart, _ := ShanghaiDayRange(now)
	rows := []Log{
		{TokenName: modelTestLogMark, Content: "ok", CreatedAt: dayStart + 1, Quota: 10, UseTime: 2, Other: `{"status_code":200}`},
		{TokenName: "other", Content: modelTestLogMark, CreatedAt: dayStart + 2, Quota: 20, UseTime: 4, Other: `{"status_code":299}`},
		{TokenName: modelTestLogMark, Content: modelTestLogMark, CreatedAt: dayStart + 3, Quota: 30, UseTime: 6, Other: `{"status_code":500}`},
		{TokenName: modelTestLogMark, Content: modelTestLogMark, CreatedAt: dayStart - 1, Quota: 999, UseTime: 99, Other: `{"status_code":200}`},
		{TokenName: "business", Content: "business", CreatedAt: dayStart + 4, Quota: 999, UseTime: 99, Other: `{"status_code":200}`},
	}
	require.NoError(t, LOG_DB.Create(&rows).Error)

	stat, err := GetModelTestTodayStat(now)
	require.NoError(t, err)
	assert.EqualValues(t, 3, stat.RequestCount)
	assert.EqualValues(t, 60, stat.Quota)
	assert.InDelta(t, 4, stat.AverageUseTime, 0.000001)
	assert.InDelta(t, 66.666666, stat.SuccessRate, 0.000001)
}
