package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGPTContextModeAndEligibility(t *testing.T) {
	for _, model := range []string{"GPT-5.4", "gpt-5.4-pro", "gpt-5.4-2026-03-05", "gpt-5.5", "gpt-5.5-pro"} {
		assert.True(t, IsGPTTextRelay(model, relayconstant.RelayModeChatCompletions), model)
	}
	for _, model := range []string{"gpt-5", "gpt-5.6", "gpt-5.4foo", "gpt-5.40", "gpt-5.4-image", "gpt-5.4-audio", "gpt-5.4-video", "gpt-image-1", "gpt-audio-1", "gpt-video-1", "gpt-oss-20b", "gptfoo", "gpt-4.1", "gpt-4o"} {
		assert.False(t, IsGPTTextRelay(model, relayconstant.RelayModeChatCompletions), model)
	}
	assert.False(t, IsGPTTextRelay("gpt-5.4", relayconstant.RelayModeImagesGenerations))
	require.Error(t, ValidateGPTContextMode("invalid"))
}

func TestGPTContextModeForGroupOverridesLegacyChannel(t *testing.T) {
	original := ratio_setting.GPTContextModes2JSONString()
	t.Cleanup(func() { _ = ratio_setting.UpdateGPTContextModesByJSONString(original) })
	require.NoError(t, ratio_setting.UpdateGPTContextModesByJSONString(`{"group-a":"standard","group-b":"gpt_long_context","group-c":"gpt_reject"}`))
	legacy := dto.ChannelOtherSettings{GPTContextMode: dto.GPTContextModeReject}
	assert.Equal(t, dto.GPTContextModeStandard, GPTContextModeForGroup("group-a"))
	assert.Equal(t, dto.GPTContextModeLongContext, GPTContextModeForGroup("group-b"))
	assert.Equal(t, dto.GPTContextModeReject, GPTContextModeForGroup("group-c"))
	assert.Equal(t, dto.GPTContextModeStandard, GPTContextModeForGroup("unknown"))
	assert.Equal(t, dto.GPTContextModeReject, legacy.GPTContextMode, "legacy JSON remains parseable but is not authoritative")
}

func TestApplyGPTContextPricingThresholdAndReject(t *testing.T) {
	original := ratio_setting.GPTContextModes2JSONString()
	t.Cleanup(func() { _ = ratio_setting.UpdateGPTContextModesByJSONString(original) })
	require.NoError(t, ratio_setting.UpdateGPTContextModesByJSONString(`{"long":"gpt_long_context","reject":"gpt_reject"}`))
	base := types.PriceData{ModelRatio: 2, CompletionRatio: 4, CacheRatio: 1, CacheCreationRatio: 3, CacheCreation5mRatio: 3, CacheCreation1hRatio: 3}
	require.Nil(t, ApplyGPTContextPricing(&base, "gpt-5.4", relayconstant.RelayModeChatCompletions, 272000, "long"))
	assert.Equal(t, float64(2), base.ModelRatio)
	require.Nil(t, ApplyGPTContextPricing(&base, "gpt-5.4", relayconstant.RelayModeChatCompletions, 272001, "long"))
	assert.Equal(t, 2.0, base.InputRatioMultiplier)
	assert.Equal(t, 1.5, base.OutputRatioMultiplier)
	reject := types.PriceData{ModelRatio: 1}
	err := ApplyGPTContextPricing(&reject, "gpt-5.4", relayconstant.RelayModeResponses, 272001, "reject")
	require.NotNil(t, err)
	assert.Equal(t, 400, err.StatusCode)
	assert.True(t, types.IsSkipRetryError(err))
	oai := err.ToOpenAIError()
	assert.Equal(t, "context_length_exceeded", oai.Type)
	assert.Equal(t, "context_length_exceeded", oai.Code)
}

func TestApplyGPTContextPricingIgnoresUnknownCacheUsage(t *testing.T) {
	price := types.PriceData{ModelRatio: 1}
	require.Nil(t, ApplyGPTContextPricing(&price, "gpt-5.4", relayconstant.RelayModeChatCompletions, 100, "unknown"))
}

func TestValidateGPTContextRequestRejectsAcrossBillingModes(t *testing.T) {
	original := ratio_setting.GPTContextModes2JSONString()
	t.Cleanup(func() { _ = ratio_setting.UpdateGPTContextModesByJSONString(original) })
	require.NoError(t, ratio_setting.UpdateGPTContextModesByJSONString(`{"reject":"gpt_reject"}`))
	require.Nil(t, ValidateGPTContextRequest("gpt-5.4", relayconstant.RelayModeResponses, GPTLongContextThreshold, "reject"))
	err := ValidateGPTContextRequest("gpt-5.4", relayconstant.RelayModeResponses, GPTLongContextThreshold+1, "reject")
	require.NotNil(t, err)
	assert.Equal(t, 400, err.StatusCode)
	assert.True(t, types.IsSkipRetryError(err))
}

func TestValidateGPTLongContextBillingExprRequiresLen(t *testing.T) {
	original := ratio_setting.GPTContextModes2JSONString()
	t.Cleanup(func() { _ = ratio_setting.UpdateGPTContextModesByJSONString(original) })
	require.NoError(t, ratio_setting.UpdateGPTContextModesByJSONString(`{"long":"gpt_long_context"}`))
	err := ValidateGPTLongContextBillingExpr("gpt-5.4", relayconstant.RelayModeResponses, GPTLongContextThreshold+1, `tier("base", p * 2.5 + c * 15)`, "long")
	require.NotNil(t, err)
	assert.True(t, types.IsSkipRetryError(err))
	assert.Contains(t, err.Error(), "uses len")

	err = ValidateGPTLongContextBillingExpr("gpt-5.4", relayconstant.RelayModeResponses, GPTLongContextThreshold+1, `len <= 272000 ? tier("standard", p * 2.5 + c * 15) : tier("long_context", p * 5 + c * 22.5)`, "long")
	require.Nil(t, err)
}
