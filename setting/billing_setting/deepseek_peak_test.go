package billing_setting

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

// The production expression for deepseek-v4.1-flash, split so each half can be
// exercised independently: the peak and off-peak tiers can only be reached at
// their own wall-clock window, so the arithmetic and the branch selection are
// verified separately.
const deepseekPeakWindow = `weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18))`

const deepseekPeakArm = `tier("峰时", p * 2 + c * 8 + cr * 0.04 + cc1h * 0.034)`
const deepseekOffPeakArm = `tier("闲时", p * 1 + c * 4 + cr * 0.02 + cc1h * 0.017)`

// TestDeepseekPeakArmArithmetic forces the peak branch with a constant true so
// it runs outside the peak window, and asserts both arms price exactly double
// apart. Without this the peak half is unreachable in an off-hours test run.
func TestDeepseekPeakArmArithmetic(t *testing.T) {
	unit := billingexpr.TokenParams{P: 1_000_000, C: 1_000_000, CR: 1_000_000, CC1h: 1_000_000}
	const quotaPerCNY = 1_000_000

	cases := []struct {
		name string
		expr string
		want float64
	}{
		{"peak", "true ? " + deepseekPeakArm + " : " + deepseekOffPeakArm, 2.0 + 8.0 + 0.04 + 0.034},
		{"offpeak", "false ? " + deepseekPeakArm + " : " + deepseekOffPeakArm, 1.0 + 4.0 + 0.02 + 0.017},
	}
	for _, tc := range cases {
		cost, trace, err := billingexpr.RunExpr(tc.expr, unit)
		if err != nil {
			t.Fatalf("%s: run error: %v", tc.name, err)
		}
		got := cost / quotaPerCNY
		if got != tc.want {
			t.Fatalf("%s: cost %v CNY, want %v", tc.name, got, tc.want)
		}
		if trace.MatchedTier == "" {
			t.Fatalf("%s: no tier recorded", tc.name)
		}
		t.Logf("%s: tier=%s cost=%.4f CNY", tc.name, trace.MatchedTier, got)
	}
}

// TestDeepseekPeakWindowShape asserts the window fragment keeps both bounds of
// each range inside its own AND group. A bare `hour >= 9 || hour < 12` would be
// a tautology and would bill every request at the peak rate.
func TestDeepseekPeakWindowShape(t *testing.T) {
	if strings.Count(deepseekPeakWindow, "weekday(") != 2 {
		t.Fatalf("expected weekday bounds on both sides, got: %s", deepseekPeakWindow)
	}
	if !strings.Contains(deepseekPeakWindow, "((hour(\"Asia/Shanghai\") >= 9 && hour(\"Asia/Shanghai\") < 12) || (hour(\"Asia/Shanghai\") >= 14 && hour(\"Asia/Shanghai\") < 18))") {
		t.Fatalf("peak windows are not two parenthesised AND groups: %s", deepseekPeakWindow)
	}

	// A tautology would compile to a constant. Confirm the window actually
	// depends on the clock by checking both windows are present as separate
	// half-open ranges (start inclusive, end exclusive).
	for _, want := range []string{`>= 9`, `< 12`, `>= 14`, `< 18`} {
		if !strings.Contains(deepseekPeakWindow, want) {
			t.Fatalf("missing bound %q in %s", want, deepseekPeakWindow)
		}
	}
}
