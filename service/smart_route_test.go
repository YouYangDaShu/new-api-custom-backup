package service

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestParseAndEncodeSmartTokenGroup(t *testing.T) {
	assert.Equal(t, SmartModeBalanced, ParseSmartMode("@smart:balanced"))
	assert.Equal(t, SmartModePrice, ParseSmartMode("@smart:cheap"))
	assert.Equal(t, SmartModeSuccessRate, ParseSmartMode("@smart:success_rate"))
	assert.Equal(t, SmartModeBalanced, ParseSmartMode("@smart:unknown"))
	assert.Equal(t, "@smart:price", EncodeSmartTokenGroup("price"))
	assert.True(t, IsMultiGroupToken("@smart:balanced"))
}

func setupSmartRouteTest(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	originalMemoryCache := common.MemoryCacheEnabled
	originalGroups := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalMax := setting.GetMaxTokenAutoGroups()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.PerfMetric{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","cheap":"Cheap","fast":"Fast","extra":"Extra"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"cheap":0.5,"fast":2,"extra":1}`))
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("3"))
	smartPerfMu.Lock()
	smartPerfCache = map[string]smartPerfSnap{}
	smartPerfMu.Unlock()
	t.Cleanup(func() {
		model.DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCache
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(fmt.Sprintf("%d", originalMax)))
		if originalMemoryCache && originalDB != nil {
			model.InitChannelCache()
		}
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func addSmartRouteChannel(t *testing.T, db *gorm.DB, id int, group, modelName string) {
	t.Helper()
	priority := int64(0)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Key: fmt.Sprintf("key-%d", id), Status: common.ChannelStatusEnabled, Name: fmt.Sprintf("channel-%d", id), Weight: &weight, Models: modelName, Group: group, Priority: &priority}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: group, Model: modelName, ChannelId: id, Enabled: true, Priority: &priority, Weight: weight}).Error)
}

func addSmartRouteMetric(t *testing.T, db *gorm.DB, modelName, group string, requests, successes int64, latency int64) {
	t.Helper()
	require.NoError(t, db.Create(&model.PerfMetric{ModelName: modelName, Group: group, BucketTs: time.Now().Unix(), RequestCount: requests, SuccessCount: successes, TotalLatencyMs: latency}).Error)
}

func TestRankGroupsForModelModesAndSamples(t *testing.T) {
	db := setupSmartRouteTest(t)
	modelName := "smart-ranking-model"
	addSmartRouteChannel(t, db, 3101, "default", modelName)
	addSmartRouteChannel(t, db, 3102, "cheap", modelName)
	addSmartRouteChannel(t, db, 3103, "fast", modelName)
	addSmartRouteMetric(t, db, modelName, "default", 10, 9, 900)
	addSmartRouteMetric(t, db, modelName, "cheap", 10, 3, 800)
	addSmartRouteMetric(t, db, modelName, "fast", 10, 10, 100)
	model.InitChannelCache()
	candidates := []string{"default", "cheap", "fast", "extra"}
	assert.Equal(t, []string{"cheap", "default", "fast"}, RankGroupsForModel("default", modelName, SmartModePrice, "/v1/chat/completions", candidates))
	assert.Equal(t, []string{"fast", "default", "cheap"}, RankGroupsForModel("default", modelName, SmartModeSuccessRate, "/v1/chat/completions", candidates))
	assert.Equal(t, []string{"fast", "default", "cheap"}, RankGroupsForModel("default", modelName, SmartModeBalanced, "/v1/chat/completions", candidates))
}

func TestSmartSuccessRateUnknownAndZeroSuccess(t *testing.T) {
	db := setupSmartRouteTest(t)
	modelName := "smart-success-samples"
	addSmartRouteChannel(t, db, 3201, "default", modelName)
	addSmartRouteChannel(t, db, 3202, "cheap", modelName)
	addSmartRouteChannel(t, db, 3203, "fast", modelName)
	addSmartRouteMetric(t, db, modelName, "default", 4, 0, 400)
	addSmartRouteMetric(t, db, modelName, "cheap", 0, 0, 0)
	addSmartRouteMetric(t, db, modelName, "fast", 4, 2, 400)
	model.InitChannelCache()
	got := RankGroupsForModel("default", modelName, SmartModeSuccessRate, "", []string{"default", "cheap", "fast"})
	assert.Equal(t, []string{"fast", "cheap"}, got)
}

func TestSmartRouteUsesOnlyTokenAutoGroupsAndRealSelection(t *testing.T) {
	db := setupSmartRouteTest(t)
	modelName := "smart-token-scope"
	addSmartRouteChannel(t, db, 3301, "cheap", modelName)
	addSmartRouteChannel(t, db, 3302, "default", modelName)
	addSmartRouteChannel(t, db, 3303, "extra", modelName)
	addSmartRouteMetric(t, db, modelName, "cheap", 5, 5, 100)
	addSmartRouteMetric(t, db, modelName, "default", 5, 5, 100)
	addSmartRouteMetric(t, db, modelName, "extra", 5, 5, 1)
	model.InitChannelCache()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"default", "cheap"})
	retry := 0
	channel, selected, err := CacheGetRandomSatisfiedChannel(&RetryParam{Ctx: ctx, TokenGroup: "@smart:balanced", ModelName: modelName, RequestPath: "/v1/chat/completions", Retry: &retry})
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Contains(t, []string{"default", "cheap"}, selected)
	assert.NotEqual(t, "extra", selected)
	assert.Equal(t, selected, common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
}
