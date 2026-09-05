package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestGPTContextEligibilityAndTwoChoicePolicy(t *testing.T) {
	require.True(t, IsGPTTextRelay("gpt-5.4", relayconstant.RelayModeResponses))
	require.True(t, IsGPTTextRelay("gpt-5.5-pro", relayconstant.RelayModeChatCompletions))
	require.False(t, IsGPTTextRelay("gpt-4.1", relayconstant.RelayModeResponses))
	require.False(t, IsGPTTextRelay("gpt-5.4-image", relayconstant.RelayModeResponses))
	require.Error(t, ValidateGPTContextMode("unsupported"))

	original := ratio_setting.GPTContextModes2JSONString()
	t.Cleanup(func() { _ = ratio_setting.UpdateGPTContextModesByJSONString(original) })
	require.NoError(t, ratio_setting.UpdateGPTContextModesByJSONString(`{"long":"gpt_long_context"}`))
	require.Equal(t, dto.GPTContextModeLongContext, GPTContextModeForGroup("long"))
	require.Equal(t, dto.GPTContextModeStandard, GPTContextModeForGroup("missing"))
}

func TestGPTContextExpressionAndLengthPolicy(t *testing.T) {
	original := ratio_setting.GPTContextModes2JSONString()
	t.Cleanup(func() { _ = ratio_setting.UpdateGPTContextModesByJSONString(original) })
	require.NoError(t, ratio_setting.UpdateGPTContextModesByJSONString(`{"long":"gpt_long_context"}`))
	require.Error(t, ValidateGPTLongContextBillingExpr("gpt-5.4", relayconstant.RelayModeResponses, GPTLongContextThreshold+1, `tier("base", p)`, "long"))
	require.NoError(t, ValidateGPTLongContextBillingExpr("gpt-5.4", relayconstant.RelayModeResponses, GPTLongContextThreshold+1, `len > 272000 ? tier("long", p) : tier("base", p)`, "long"))
	require.Equal(t, GPTLongContextThreshold, NormalizeGPTContextLen("gpt-5.4", relayconstant.RelayModeResponses, GPTLongContextThreshold+1, dto.GPTContextModeStandard))
	require.Equal(t, GPTLongContextThreshold+1, NormalizeGPTContextLen("gpt-5.4", relayconstant.RelayModeResponses, GPTLongContextThreshold+1, dto.GPTContextModeLongContext))
}
