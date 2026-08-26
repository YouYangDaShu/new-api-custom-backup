package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUpdateCustomOptionRejectsInvalidValuesWithoutPersisting(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized")
	}
	require.NoError(t, DB.AutoMigrate(&Option{}))
	InitOptionMap()
	oldReward := common.WeeklyRankReward1
	oldTimeout := common.ProcessingLogTimeoutMinutes
	t.Cleanup(func() {
		common.WeeklyRankReward1 = oldReward
		common.ProcessingLogTimeoutMinutes = oldTimeout
		_ = DB.Where("key IN ?", []string{"WeeklyRankReward1", "ProcessingLogTimeoutMinutes"}).Delete(&Option{}).Error
	})

	require.NoError(t, UpdateOption("WeeklyRankReward1", "1.5"))
	require.NoError(t, UpdateOption("ProcessingLogTimeoutMinutes", "30"))

	invalidRewards := []string{"-1", "NaN", "+Inf", "not-a-number"}
	for _, value := range invalidRewards {
		require.Error(t, UpdateOption("WeeklyRankReward1", value), value)
	}
	require.Error(t, UpdateOption("WeeklyRankReward1", "5000"), "reward quota must fit the int32 quota column")
	require.Equal(t, 1.5, common.WeeklyRankReward1)

	for _, value := range []string{"0", "10081", "not-a-number"} {
		require.Error(t, UpdateOption("ProcessingLogTimeoutMinutes", value), value)
	}
	require.Equal(t, 30, common.ProcessingLogTimeoutMinutes)

	var rewardOpt, timeoutOpt Option
	require.NoError(t, DB.First(&rewardOpt, "key = ?", "WeeklyRankReward1").Error)
	require.NoError(t, DB.First(&timeoutOpt, "key = ?", "ProcessingLogTimeoutMinutes").Error)
	require.Equal(t, "1.5", rewardOpt.Value)
	require.Equal(t, "30", timeoutOpt.Value)
	require.False(t, math.IsNaN(common.WeeklyRankReward1))
}
