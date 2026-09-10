package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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
