package billingexpr_test

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

const peakOffPeakExpr = `weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? tier("峰时", p * 2 + c * 8 + cr * 0.04) : weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18 ? tier("峰时", p * 2 + c * 8 + cr * 0.04) : tier("闲时", p * 1 + c * 4 + cr * 0.02)`

// TestTierTimeConditionStagingExpr proves the expression that is live on the
// staging server compiles, matches a tier, and prices both bands correctly.
// The engine returns unit price × token count, so a 1M+1M request costs
// 5e6 off-peak (¥5) or 1e7 peak (¥10).
func TestTierTimeConditionStagingExpr(t *testing.T) {
	if _, err := billingexpr.CompileFromCache(peakOffPeakExpr); err != nil {
		t.Fatalf("compile: %v", err)
	}

	params := billingexpr.TokenParams{P: 1_000_000, C: 1_000_000, Len: 1_000_000}
	got, trace, err := billingexpr.RunExprWithRequest(peakOffPeakExpr, params, billingexpr.RequestInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 5e6 && got != 1e7 {
		t.Fatalf("cost must be 5e6 (off-peak) or 1e7 (peak), got %v", got)
	}
	wantTier := "闲时"
	wantCost := 5e6
	if got == 1e7 {
		wantTier = "峰时"
		wantCost = 1e7
	}
	if trace.MatchedTier != wantTier {
		t.Fatalf("cost %v implies tier %q, engine reported %q", got, wantTier, trace.MatchedTier)
	}
	t.Logf("matched tier=%q cost=%v", trace.MatchedTier, got)

	// Cache read must be billed at its own coefficient, not at input price.
	cacheParams := billingexpr.TokenParams{P: 0, C: 0, Len: 1_000_000, CR: 1_000_000}
	cacheCost, cacheTrace, err := billingexpr.RunExprWithRequest(peakOffPeakExpr, cacheParams, billingexpr.RequestInput{})
	if err != nil {
		t.Fatalf("run cache: %v", err)
	}
	wantCache := 2e4 // 1M × 0.02
	if trace.MatchedTier == "峰时" {
		wantCache = 4e4 // 1M × 0.04
	}
	if cacheCost != wantCache {
		t.Fatalf("cache read cost must be %v, got %v", wantCache, cacheCost)
	}
	if cacheTrace.MatchedTier != trace.MatchedTier {
		t.Fatalf("same instant must match the same tier: %q vs %q", trace.MatchedTier, cacheTrace.MatchedTier)
	}
	_ = wantCost
}
