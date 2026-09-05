package helper

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const GPTLongContextThreshold = 272000

// IsGPTTextRelay limits the policy to the GPT text models covered by the
// established long-context pricing rule.
func IsGPTTextRelay(modelName string, relayMode int) bool {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if !(name == "gpt-5.4" || strings.HasPrefix(name, "gpt-5.4-") || name == "gpt-5.5" || strings.HasPrefix(name, "gpt-5.5-")) {
		return false
	}
	if strings.Contains(name, "-image") || strings.Contains(name, "-audio") || strings.Contains(name, "-video") || strings.HasPrefix(name, "gpt-oss") {
		return false
	}
	return relayMode == relayconstant.RelayModeChatCompletions || relayMode == relayconstant.RelayModeCompletions || relayMode == relayconstant.RelayModeResponses
}

func GPTContextModeForGroup(group string) dto.GPTContextMode {
	mode, _ := ratio_setting.GetGPTContextMode(group)
	return dto.GPTContextMode(mode).Normalized()
}

func ValidateGPTContextMode(mode dto.GPTContextMode) error {
	if !mode.Valid() {
		return fmt.Errorf("invalid gpt_context_mode: %q", mode)
	}
	return nil
}

// ValidateGPTLongContextBillingExpr ensures the configured expression, rather
// than a hidden model price, controls the long-context tier.
func ValidateGPTLongContextBillingExpr(modelName string, relayMode, promptTokens int, exprString, group string) error {
	if !IsGPTTextRelay(modelName, relayMode) || promptTokens <= GPTLongContextThreshold || GPTContextModeForGroup(group) != dto.GPTContextModeLongContext {
		return nil
	}
	if !billingexpr.UsedVars(exprString)["len"] {
		return fmt.Errorf("gpt_long_context requires a tiered billing expression that uses len for context-sensitive pricing")
	}
	return nil
}

// NormalizeGPTContextLen keeps standard groups on the standard tier while
// leaving the configured expression as the only source of prices.
func NormalizeGPTContextLen(modelName string, relayMode, promptTokens int, mode dto.GPTContextMode) int {
	if !IsGPTTextRelay(modelName, relayMode) || mode == dto.GPTContextModeLongContext {
		return promptTokens
	}
	if promptTokens > GPTLongContextThreshold {
		return GPTLongContextThreshold
	}
	return promptTokens
}
