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
	if !strings.HasPrefix(name, "gpt-") || strings.HasPrefix(name, "gpt-oss") {
		return false
	}
	if strings.Contains(name, "-image") || strings.Contains(name, "-audio") || strings.Contains(name, "-video") {
		return false
	}
	return relayMode == relayconstant.RelayModeChatCompletions || relayMode == relayconstant.RelayModeCompletions || relayMode == relayconstant.RelayModeResponses
}

// DefaultGPTLongContextBillingExpr is the official GPT long-context rule used
// when an administrator enables gpt_long_context for a group. An explicit
// per-model tiered expression still takes precedence.
const DefaultGPTLongContextBillingExpr = `len <= 272000 ? tier("standard", p * 2.5 + c * 15 + cr * 0.25) : tier("long_context", p * 5 + c * 22.5 + cr * 0.5)`

func GPTGroupUsesTieredBilling(modelName string, relayMode int, group string) bool {
	return IsGPTTextRelay(modelName, relayMode) && GPTContextModeForGroup(group) == dto.GPTContextModeLongContext
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
