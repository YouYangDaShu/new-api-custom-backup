package billing_setting

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

// prodDeepseekExpr is the expression exactly as stored in production for
// deepseek-v4.1-flash.
const prodDeepseekExpr = `weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18)) ? tier("峰时", p * 2 + c * 8 + cr * 0.04 + cc1h * 0.034) : tier("闲时", p * 1 + c * 4 + cr * 0.02 + cc1h * 0.017)`

// withClock substitutes a literal weekday/hour into the real expression text so
// the same string can be evaluated for any point in the week. The operators,
// grouping and tier bodies are untouched — only the two clock reads are pinned.
func withClock(expr string, weekday, hour int) string {
	out := expr
	replacements := []struct{ from, to string }{
		{`weekday("Asia/Shanghai")`, fmt.Sprintf("%d", weekday)},
		{`hour("Asia/Shanghai")`, fmt.Sprintf("%d", hour)},
	}
	for _, r := range replacements {
		out = replaceAll(out, r.from, r.to)
	}
	return out
}

func replaceAll(s, from, to string) string {
	for {
		i := indexOf(s, from)
		if i < 0 {
			return s
		}
		s = s[:i] + to + s[i+len(from):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestDeepseekWeekMatrix walks all 7 x 24 clock points through the real
// production expression and asserts exactly the intended cells bill at the
// peak rate. This is the test that catches "peak window never fires" and
// "peak window fires all day" — the two ways this pricing silently breaks.
func TestDeepseekWeekMatrix(t *testing.T) {
	unit := billingexpr.TokenParams{P: 1_000_000, C: 1_000_000, CR: 1_000_000, CC1h: 1_000_000}
	const quotaPerCNY = 1_000_000

	const offPeakCNY = 1.0 + 4.0 + 0.02 + 0.017
	const peakCNY = 2.0 + 8.0 + 0.04 + 0.034

	peakHours := map[int]bool{9: true, 10: true, 11: true, 14: true, 15: true, 16: true, 17: true}

	peakCells := 0
	for weekday := 0; weekday <= 6; weekday++ { // Go time.Weekday: 0=Sunday
		for hour := 0; hour < 24; hour++ {
			expr := withClock(prodDeepseekExpr, weekday, hour)
			cost, trace, err := billingexpr.RunExpr(expr, unit)
			if err != nil {
				t.Fatalf("weekday=%d hour=%d: %v", weekday, hour, err)
			}
			got := cost / quotaPerCNY

			wantPeak := weekday >= 1 && weekday <= 5 && peakHours[hour]
			wantTier := "闲时"
			wantCost := offPeakCNY
			if wantPeak {
				wantTier = "峰时"
				wantCost = peakCNY
				peakCells++
			}
			if trace.MatchedTier != wantTier {
				t.Fatalf("weekday=%d hour=%d: tier=%q want %q", weekday, hour, trace.MatchedTier, wantTier)
			}
			if got != wantCost {
				t.Fatalf("weekday=%d hour=%d: cost=%v CNY want %v", weekday, hour, got, wantCost)
			}
		}
	}

	// 5 weekdays x 7 peak hours (9,10,11 and 14,15,16,17).
	if peakCells != 35 {
		t.Fatalf("peak cells = %d, want 35", peakCells)
	}
	t.Logf("all 168 clock points priced correctly; %d at peak (%v CNY), %d at off-peak (%v CNY)",
		peakCells, peakCNY, 168-peakCells, offPeakCNY)
}

// TestDeepseekBoundaries pins the exact minute boundaries so an off-by-one in
// the half-open ranges cannot slip through: 11:59 is peak, 12:00 is not.
func TestDeepseekBoundaries(t *testing.T) {
	peakHours := map[int]bool{9: true, 10: true, 11: true, 14: true, 15: true, 16: true, 17: true}
	for _, hour := range []int{8, 9, 11, 12, 13, 14, 17, 18, 19} {
		expr := withClock(prodDeepseekExpr, 3, hour) // Wednesday
		_, trace, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{P: 1, C: 1, Len: 1})
		if err != nil {
			t.Fatalf("hour=%d: %v", hour, err)
		}
		want := "闲时"
		if peakHours[hour] {
			want = "峰时"
		}
		if trace.MatchedTier != want {
			t.Fatalf("Wednesday %02d:00 -> %q, want %q", hour, trace.MatchedTier, want)
		}
		t.Logf("Wed %02d:00 -> %s", hour, trace.MatchedTier)
	}
}
