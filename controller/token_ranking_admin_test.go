package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminRankingHistoryIncludesRecipientIdentity(t *testing.T) {
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
	assert.Equal(t, 101, history[0].UserID)
	assert.Equal(t, "A***", history[0].DisplayName)
	assert.Equal(t, 999, history[1].UserID)
	assert.Equal(t, "-", history[1].DisplayName)

	encoded := string(mustMarshal(history))
	assert.Contains(t, encoded, `"user_id":101`)
	assert.Contains(t, encoded, `"display_name":"A***"`)
	assert.NotContains(t, encoded, "password")
}
