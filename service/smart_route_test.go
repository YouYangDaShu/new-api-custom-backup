package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSmartMode(t *testing.T) {
	assert.Equal(t, SmartModeBalanced, ParseSmartMode("@smart:balanced"))
	assert.Equal(t, SmartModePrice, ParseSmartMode("@smart:price"))
	assert.Equal(t, SmartModePrice, ParseSmartMode("@smart:cheap"))
	assert.Equal(t, SmartModeSuccessRate, ParseSmartMode("@smart:success_rate"))
	assert.Equal(t, SmartModeBalanced, ParseSmartMode("@smart:"))
}

func TestEncodeSmartTokenGroup(t *testing.T) {
	assert.Equal(t, "@smart:price", EncodeSmartTokenGroup("price"))
	assert.Equal(t, "@smart:balanced", EncodeSmartTokenGroup("balanced"))
	assert.Equal(t, "@smart:success_rate", EncodeSmartTokenGroup("success_rate"))
}

func TestIsMultiGroupTokenIncludesSmart(t *testing.T) {
	if !IsMultiGroupToken("@smart:price") {
		t.Fatal("smart should be multi")
	}
	if !IsSmartTokenGroup("@smart:balanced") {
		t.Fatal("is smart")
	}
}

func TestRankGroupsForModelSuccessRateUsesObservedCounts(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		model.DB.Where("model = ?", "smart-route-success-test").Delete(&model.Ability{})
		model.DB.Where("name LIKE ?", "smart-route-success-%").Delete(&model.Channel{})
		model.InitChannelCache()
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		smartPerfMu.Lock()
		for _, group := range []string{"perfect", "partial", "zero", "unknown"} {
			delete(smartPerfCache, smartPerfKey("smart-route-success-test", group))
		}
		smartPerfMu.Unlock()
	})

	priority := int64(0)
	for _, group := range []string{"perfect", "partial", "zero", "unknown"} {
		channel := model.Channel{
			Name: "smart-route-success-" + group, Key: "test", Group: group,
			Models: "smart-route-success-test", Status: common.ChannelStatusEnabled,
		}
		require.NoError(t, model.DB.Create(&channel).Error)
		require.NoError(t, model.DB.Create(&model.Ability{
			Group: group, Model: "smart-route-success-test", ChannelId: channel.Id,
			Enabled: true, Priority: &priority,
		}).Error)
	}
	model.InitChannelCache()

	now := time.Now().Add(time.Minute)
	smartPerfMu.Lock()
	smartPerfCache[smartPerfKey("smart-route-success-test", "perfect")] = smartPerfSnap{successRate: 1, avgLatency: 100, requestCount: 5, successCount: 5, expireAt: now}
	smartPerfCache[smartPerfKey("smart-route-success-test", "partial")] = smartPerfSnap{successRate: 0.8, avgLatency: 50, requestCount: 10, successCount: 8, expireAt: now}
	smartPerfCache[smartPerfKey("smart-route-success-test", "zero")] = smartPerfSnap{successRate: 0, avgLatency: 10, requestCount: 2, successCount: 0, expireAt: now}
	smartPerfCache[smartPerfKey("smart-route-success-test", "unknown")] = smartPerfSnap{successRate: -1, avgLatency: -1, requestCount: 0, successCount: 0, expireAt: now}
	smartPerfMu.Unlock()

	got := RankGroupsForModel("default", "smart-route-success-test", SmartModeSuccessRate, "", []string{"unknown", "zero", "partial", "perfect"})
	assert.Equal(t, []string{"perfect", "partial", "unknown"}, got)
}

func TestSuccessRateUnavailableRequiresObservedZeroSuccesses(t *testing.T) {
	assert.True(t, successRateUnavailable(3, 0))
	assert.False(t, successRateUnavailable(0, 0))
	assert.False(t, successRateUnavailable(3, 1))
}

func TestDisplayTokenGroupSmart(t *testing.T) {
	if DisplayTokenGroup("@smart:price", "grok") != "smart:price(grok)" {
		t.Fatal(DisplayTokenGroup("@smart:price", "grok"))
	}
}
