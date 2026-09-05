package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTokenRankingAdminAggregatesAllRewardLedgers(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.WeeklyRankingReward{}))

	common.OptionMapRWMutex.Lock()
	originalEnabled := common.TokenRankingEnabled
	common.TokenRankingEnabled = true
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.TokenRankingEnabled = originalEnabled
		common.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, db.Create(&[]model.WeeklyRankingReward{
		{WeekStart: "2026-08-10", UserID: 1, Rank: 1, RewardQuota: 100},
		{WeekStart: "2026-08-10", UserID: 2, Rank: 2, RewardQuota: 50},
		{WeekStart: "2026-08-17", UserID: 2, Rank: 2, RewardQuota: 250},
		{WeekStart: "2026-08-24", UserID: 3, Rank: 3, RewardQuota: 400},
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/weekly-user-ranking/admin/", nil)
	GetTokenRankingAdmin(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			History          []any `json:"history"`
			TotalRewardCount int64 `json:"total_reward_count"`
			TotalRewardQuota int64 `json:"total_reward_quota"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Len(t, response.Data.History, 4)
	assert.Equal(t, int64(3), response.Data.TotalRewardCount)
	assert.Equal(t, int64(800), response.Data.TotalRewardQuota)
}

func TestAdminRankingHistoryOmitsRecipientIdentity(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:          101,
		Username:    "alice",
		DisplayName: "Alice Admin",
		Password:    "password",
	}).Error)

	history := adminRankingHistory([]model.WeeklyRankingReward{
		{WeekStart: "2026-08-17", UserID: 101, Rank: 1},
		{WeekStart: "2026-08-17", UserID: 999, Rank: 2},
	})

	require.Len(t, history, 2)
	assert.Equal(t, "A***", history[0].DisplayName)

	encoded := string(mustMarshal(history))
	assert.Contains(t, encoded, `"display_name":"A***"`)
	assert.NotContains(t, encoded, `"user_id"`)
	assert.NotContains(t, encoded, "password")
}
