package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUsageBreakdownAggregatesCacheFieldsAndLimitsModels(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create([]Log{
		{UserId: 11, Username: "alice", ModelName: "gpt-a", Type: LogTypeConsume, CreatedAt: now - 10, Quota: 100, PromptTokens: 100, CompletionTokens: 20, Other: `{"cache_tokens":30,"cache_creation_tokens":10}`},
		{UserId: 11, Username: "alice", ModelName: "gpt-a", Type: LogTypeConsume, CreatedAt: now - 20, Quota: 50, PromptTokens: 200, CompletionTokens: 30, Other: `{"cache_tokens":40,"cache_write_tokens":7}`},
		{UserId: 12, Username: "bob", ModelName: "gpt-b", Type: LogTypeConsume, CreatedAt: now - 20, Quota: 999, PromptTokens: 1, CompletionTokens: 1},
		{UserId: 11, Username: "alice", ModelName: "error", Type: LogTypeError, CreatedAt: now - 20, Quota: 999},
	}).Error)

	result, err := GetUsageBreakdown(now-60, now, 11, "", 1)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	assert.Equal(t, int64(2), result.RequestCount)
	assert.Equal(t, int64(150), result.Quota)
	assert.Equal(t, int64(300), result.PromptTokens)
	assert.Equal(t, int64(50), result.CompletionTokens)
	assert.Equal(t, int64(70), result.CacheReadTokens)
	assert.Equal(t, int64(17), result.CacheCreateTokens)
	assert.Equal(t, "gpt-a", result.Models[0].ModelName)
}
