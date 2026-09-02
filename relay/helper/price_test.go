package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPriceHelperTieredUsesPreloadedRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"tiered-test-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"tiered-test-model":"param(\"stream\") == true ? tier(\"stream\", p * 3) : tier(\"base\", p * 2)"}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/channel/test/1", nil)
	req.Body = nil
	req.ContentLength = 0
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: "tiered-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"stream":true}`),
		},
	}

	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 1500, priceData.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, "stream", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Equal(t, common.QuotaPerUnit, info.TieredBillingSnapshot.QuotaPerUnit)
}

func TestModelPriceHelperGPTLongContextRequiresAndUsesLenTier(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":          `{"gpt-5.4":"tiered_expr","gpt-5.4-flat":"tiered_expr","gpt-5.4-reject":"tiered_expr"}`,
		"billing_setting.billing_expr":          `{"gpt-5.4":"len <= 272000 ? tier(\"standard\", p * 2.5 + c * 15) : tier(\"long_context\", p * 5 + c * 22.5)","gpt-5.4-flat":"tier(\"base\", p * 2.5 + c * 15)","gpt-5.4-reject":"len <= 272000 ? tier(\"standard\", p * 2.5) : tier(\"long_context\", p * 5)"}`,
		"group_ratio_setting.gpt_context_modes": `{"long":"gpt_long_context","reject":"gpt_reject"}`,
	}))

	newContext := func() *gin.Context {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		ctx.Set("group", "default")
		return ctx
	}
	newInfo := func(model, group string) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			OriginModelName: model,
			UserGroup:       "default",
			UsingGroup:      group,
			RelayMode:       relayconstant.RelayModeResponses,
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelOtherSettings: dto.ChannelOtherSettings{GPTContextMode: dto.GPTContextModeReject},
			},
		}
	}

	longInfo := newInfo("gpt-5.4", "long")
	priceData, err := ModelPriceHelper(newContext(), longInfo, GPTLongContextThreshold+1, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.NotNil(t, longInfo.TieredBillingSnapshot)
	require.Equal(t, "long_context", longInfo.TieredBillingSnapshot.EstimatedTier)
	require.Positive(t, priceData.QuotaToPreConsume)

	_, err = ModelPriceHelper(newContext(), newInfo("gpt-5.4-flat", "long"), GPTLongContextThreshold+1, &types.TokenCountMeta{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "uses len")

	rejectInfo := newInfo("gpt-5.4-reject", "reject")
	_, err = ModelPriceHelper(newContext(), rejectInfo, GPTLongContextThreshold+1, &types.TokenCountMeta{})
	require.Error(t, err)
	apiErr, ok := err.(*types.NewAPIError)
	require.True(t, ok)
	require.Equal(t, 400, apiErr.StatusCode)
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Nil(t, rejectInfo.TieredBillingSnapshot)
}

func TestModelPriceHelperGPTLongContextPreConsumeFloorAndZeroRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	oldPre := common.PreConsumedQuota
	common.PreConsumedQuota = GPTLongContextThreshold + 100
	t.Cleanup(func() { common.PreConsumedQuota = oldPre })
	oldRatios := ratio_setting.ModelRatio2JSONString()
	oldCompletion := ratio_setting.CompletionRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-5.4":1,"gpt-5.4-zero":0}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"gpt-5.4":1,"gpt-5.4-zero":100}`))
	originalModes := ratio_setting.GPTContextModes2JSONString()
	require.NoError(t, ratio_setting.UpdateGPTContextModesByJSONString(`{"long":"gpt_long_context"}`))
	t.Cleanup(func() {
		_ = ratio_setting.UpdateModelRatioByJSONString(oldRatios)
		_ = ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion)
		_ = ratio_setting.UpdateGPTContextModesByJSONString(originalModes)
	})
	newContext := func() *gin.Context {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		ctx.Set("group", "default")
		return ctx
	}
	newInfo := func(model string) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{OriginModelName: model, UserGroup: "default", UsingGroup: "long",
			RelayMode:   relayconstant.RelayModeChatCompletions,
			ChannelMeta: &relaycommon.ChannelMeta{ChannelOtherSettings: dto.ChannelOtherSettings{GPTContextMode: dto.GPTContextModeStandard}}}
	}
	// Ratio billing uses the explicit long-context multipliers.
	price, err := ModelPriceHelper(newContext(), newInfo("gpt-5.4"), GPTLongContextThreshold+1, &types.TokenCountMeta{MaxTokens: 100})
	require.NoError(t, err)
	expected := common.QuotaRound(float64(common.PreConsumedQuota)*2 + 100*price.CompletionRatio*1.5)
	require.Equal(t, expected, price.QuotaToPreConsume)
	require.Equal(t, 2.0, price.InputRatioMultiplier)
	require.Equal(t, 1.5, price.OutputRatioMultiplier)

	price, err = ModelPriceHelper(newContext(), newInfo("gpt-5.4-zero"), GPTLongContextThreshold+1, &types.TokenCountMeta{MaxTokens: 100})
	require.NoError(t, err)
	require.Equal(t, 0, price.QuotaToPreConsume)

	// Oversized long-context products saturate instead of wrapping negative.
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-5.4":1e308}`))
	info := newInfo("gpt-5.4")
	price, err = ModelPriceHelper(newContext(), info, GPTLongContextThreshold+1, &types.TokenCountMeta{MaxTokens: 100})
	require.NoError(t, err)
	require.Equal(t, common.MaxQuota, price.QuotaToPreConsume)
	require.NotNil(t, info.QuotaClamp)
	require.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
}

func TestModelPriceHelperUsesActualAutoSelectedGroupForGPTPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalModes := ratio_setting.GPTContextModes2JSONString()
	originalRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		_ = ratio_setting.UpdateGPTContextModesByJSONString(originalModes)
		_ = ratio_setting.UpdateModelRatioByJSONString(originalRatios)
	})
	require.NoError(t, ratio_setting.UpdateGPTContextModesByJSONString(`{"requested":"standard","selected":"gpt_reject"}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-5.4":1}`))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("auto_group", "selected")
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.4",
		UserGroup:       "default",
		UsingGroup:      "requested",
		RelayMode:       relayconstant.RelayModeResponses,
	}

	_, err := ModelPriceHelper(ctx, info, GPTLongContextThreshold+1, &types.TokenCountMeta{})
	require.Error(t, err)
	require.Equal(t, "selected", info.UsingGroup)
	apiErr, ok := err.(*types.NewAPIError)
	require.True(t, ok)
	require.Equal(t, 400, apiErr.StatusCode)
}

func TestGPT54MiniRequiresExplicitTieredExpr(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":          `{"gpt-5.4-mini-explicit":"tiered_expr"}`,
		"billing_setting.billing_expr":          `{"gpt-5.4-mini-explicit":"len <= 272000 ? tier(\"standard\", p * 1) : tier(\"long_context\", p * 2)"}`,
		"group_ratio_setting.gpt_context_modes": `{"long":"gpt_long_context"}`,
	}))
	originalRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() { _ = ratio_setting.UpdateModelRatioByJSONString(originalRatios) })
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-5.4-mini-plain":1}`))

	newInfo := func(model string) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{OriginModelName: model, UserGroup: "default", UsingGroup: "long", RelayMode: relayconstant.RelayModeResponses}
	}
	newContext := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		return ctx
	}

	explicit := newInfo("gpt-5.4-mini-explicit")
	_, err := ModelPriceHelper(newContext(), explicit, GPTLongContextThreshold+1, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.NotNil(t, explicit.TieredBillingSnapshot)
	require.Equal(t, "long_context", explicit.TieredBillingSnapshot.EstimatedTier)

	plain := newInfo("gpt-5.4-mini-plain")
	price, err := ModelPriceHelper(newContext(), plain, GPTLongContextThreshold+1, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Nil(t, plain.TieredBillingSnapshot)
	require.Equal(t, 2.0, price.InputRatioMultiplier)
}

func TestModelPriceHelperTieredPreConsumeFloorDoesNotChangeLenTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"tiered-floor-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"tiered-floor-model":"len <= 500 ? tier(\"standard\", p * 1) : tier(\"long_context\", p * 100)"}`,
	}))
	oldPre := common.PreConsumedQuota
	common.PreConsumedQuota = 1000
	t.Cleanup(func() { common.PreConsumedQuota = oldPre })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{OriginModelName: "tiered-floor-model", UserGroup: "default", UsingGroup: "default"}
	price, err := ModelPriceHelper(ctx, info, 100, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, common.QuotaFromFloat(1000.0/1_000_000*common.QuotaPerUnit), price.QuotaToPreConsume)
	require.Equal(t, "standard", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, 1000, info.TieredBillingSnapshot.EstimatedPromptTokens)
	require.Equal(t, 100, info.TieredBillingSnapshot.EstimatedPromptLen)
	settled, err := billingexpr.ComputeTieredQuota(info.TieredBillingSnapshot, billingexpr.TokenParams{P: 100, Len: 100})
	require.NoError(t, err)
	require.Equal(t, "standard", settled.MatchedTier)
}

func TestModelPriceHelperTieredPreConsumeQuotaClampIsAudited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"tiered-clamp-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"tiered-clamp-model":"tier(\"base\", p * 1e308)"}`,
	}))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{OriginModelName: "tiered-clamp-model", UserGroup: "default", UsingGroup: "default"}
	price, err := ModelPriceHelper(ctx, info, 100, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, common.MaxQuota, price.QuotaToPreConsume)
	require.NotNil(t, info.QuotaClamp)
	require.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
}
