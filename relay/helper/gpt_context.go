package helper

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

const GPTLongContextThreshold = 272000

func IsGPTTextRelay(modelName string, relayMode int) bool {
	name := strings.ToLower(strings.TrimSpace(modelName))
	// The repository has a confirmed 272K tier rule for GPT-5.4/GPT-5.5 only.
	// Do not infer the same boundary or prices for other GPT models.
	if !(name == "gpt-5.4" || strings.HasPrefix(name, "gpt-5.4-") || name == "gpt-5.5" || strings.HasPrefix(name, "gpt-5.5-")) {
		return false
	}
	if strings.Contains(name, "-image") || strings.Contains(name, "-audio") || strings.Contains(name, "-video") || strings.HasPrefix(name, "gpt-oss") {
		return false
	}
	return relayMode == relayconstant.RelayModeChatCompletions || relayMode == relayconstant.RelayModeCompletions || relayMode == relayconstant.RelayModeResponses
}

// GPTContextModeForGroup resolves the authoritative billing-group policy.
// Missing and unknown groups intentionally use standard pricing; the legacy
// channel JSON field remains parseable but does not influence new billing.
func GPTContextModeForGroup(group string) dto.GPTContextMode {
	mode, _ := ratio_setting.GetGPTContextMode(group)
	return dto.GPTContextMode(mode)
}

func ValidateGPTContextMode(mode dto.GPTContextMode) error {
	if !mode.Valid() {
		return fmt.Errorf("invalid gpt_context_mode: %q", mode)
	}
	return nil
}

// ValidateGPTContextRequest enforces mode-level request limits independently
// of the billing backend. This keeps gpt_reject effective for tiered pricing,
// whose expression is otherwise the sole source of price calculation.
func ValidateGPTContextRequest(modelName string, relayMode, promptTokens int, group string) *types.NewAPIError {
	if !IsGPTTextRelay(modelName, relayMode) || promptTokens <= GPTLongContextThreshold {
		return nil
	}
	if GPTContextModeForGroup(group) != dto.GPTContextModeReject {
		return nil
	}
	return types.WithOpenAIError(types.OpenAIError{Message: fmt.Sprintf("context length exceeds %d tokens", GPTLongContextThreshold), Type: "context_length_exceeded", Code: "context_length_exceeded"}, 400, types.ErrOptionWithSkipRetry())
}

// ValidateGPTLongContextBillingExpr verifies that long-context mode is backed
// by an administrator-configured context-sensitive tiered expression. The
// expression remains the sole pricing source; this check does not add a hidden
// multiplier or assume provider prices.
func ValidateGPTLongContextBillingExpr(modelName string, relayMode, promptTokens int, exprStr, group string) *types.NewAPIError {
	if !IsGPTTextRelay(modelName, relayMode) || promptTokens <= GPTLongContextThreshold || GPTContextModeForGroup(group) != dto.GPTContextModeLongContext {
		return nil
	}
	if !billingexpr.UsedVars(exprStr)["len"] {
		return types.NewError(fmt.Errorf("gpt_long_context requires a tiered billing expression that uses len for context-sensitive pricing"), types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(400), types.ErrOptionWithSkipRetry())
	}
	return nil
}

func ApplyGPTContextPricing(price *types.PriceData, modelName string, relayMode, promptTokens int, group string) *types.NewAPIError {
	if price == nil || price.UsePrice || !IsGPTTextRelay(modelName, relayMode) {
		return nil
	}
	if rejectErr := ValidateGPTContextRequest(modelName, relayMode, promptTokens, group); rejectErr != nil {
		return rejectErr
	}
	mode := GPTContextModeForGroup(group)
	if mode == dto.GPTContextModeStandard {
		return nil
	}
	if promptTokens <= GPTLongContextThreshold {
		return nil
	}
	if mode == dto.GPTContextModeReject {
		return nil
	}
	// Ratio billing keeps the established explicit long-context multipliers.
	// This does not synthesize or force a tiered_expr configuration for Mini or
	// any other model; tiered_expr is used only when explicitly configured.
	price.InputRatioMultiplier = 2
	price.OutputRatioMultiplier = 1.5
	return nil
}
