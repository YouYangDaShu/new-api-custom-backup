package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
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

func TestGPTContextModeOptionRejectsInvalidValueWithoutPersisting(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := ratio_setting.GPTContextModes2JSONString()
	t.Cleanup(func() {
		_ = ratio_setting.UpdateGPTContextModesByJSONString(original)
		_ = DB.Where("key = ?", "group_ratio_setting.gpt_context_modes").Delete(&Option{}).Error
	})

	require.NoError(t, UpdateOption("group_ratio_setting.gpt_context_modes", `{"plus":"standard","pro":"gpt_long_context","stable":"gpt_reject"}`))
	require.Error(t, UpdateOption("group_ratio_setting.gpt_context_modes", `{"plus":"invalid"}`))

	var option Option
	require.NoError(t, DB.First(&option, "key = ?", "group_ratio_setting.gpt_context_modes").Error)
	require.JSONEq(t, `{"plus":"standard","pro":"gpt_long_context","stable":"gpt_reject"}`, option.Value)
	require.JSONEq(t, option.Value, ratio_setting.GPTContextModes2JSONString())
	require.Contains(t, config.GlobalConfig.ExportAllConfigs(), "group_ratio_setting.gpt_context_modes")
}
