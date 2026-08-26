package ratio_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/types"
)

func TestResolveGroupRatio_MembershipMultiply(t *testing.T) {
	// Isolate maps for this test
	groupRatioMap.Clear()
	groupGroupRatioMap.Clear()
	// Ensure membership flags use defaults (svip is membership).
	if groupRatioSetting.MembershipGroups != nil {
		groupRatioSetting.MembershipGroups.Clear()
		groupRatioSetting.MembershipGroups.AddAll(defaultMembershipGroups)
	}
	groupRatioMap.AddAll(map[string]float64{
		"default": 1,
		"grok":    0.05,
		"svip":    0.85,
	})

	// SVIP user + default token → 1 * 0.85
	final, special, has := ResolveGroupRatio("svip", "default")
	if !has || special != 0.85 || final != 0.85 {
		t.Fatalf("svip+default: final=%v special=%v has=%v", final, special, has)
	}

	// SVIP user + grok token → 0.05 * 0.85
	final, special, has = ResolveGroupRatio("svip", "grok")
	if !has || special != 0.85 || final != 0.05*0.85 {
		t.Fatalf("svip+grok: final=%v special=%v has=%v", final, special, has)
	}

	// Normal user + grok → no membership
	final, special, has = ResolveGroupRatio("default", "grok")
	if has || special != -1 || final != 0.05 {
		t.Fatalf("default+grok: final=%v special=%v has=%v", final, special, has)
	}

	// Same group: no double multiply
	final, special, has = ResolveGroupRatio("svip", "svip")
	if has || final != 0.85 {
		t.Fatalf("svip+svip: final=%v special=%v has=%v", final, special, has)
	}

	// Misconfigured user group = model pool name must NOT multiply
	final, special, has = ResolveGroupRatio("grok", "default")
	if has || special != -1 || final != 1 {
		t.Fatalf("user=grok should not multiply: final=%v special=%v has=%v", final, special, has)
	}

	// Absolute override wins
	groupGroupRatioMap.AddAll(map[string]map[string]float64{
		"svip": {"grok": 0.01},
	})
	final, special, has = ResolveGroupRatio("svip", "grok")
	if !has || final != 0.01 || special != 0.01 {
		t.Fatalf("override: final=%v special=%v has=%v", final, special, has)
	}

	// Membership ratio 0 must NOT free the request (treat as no membership discount)
	groupGroupRatioMap.Clear()
	groupRatioMap.AddAll(map[string]float64{
		"default": 1,
		"grok":    0.05,
		"svip":    0,
	})
	final, special, has = ResolveGroupRatio("svip", "grok")
	if has || special != -1 || final != 0.05 {
		t.Fatalf("svip=0 should ignore membership discount: final=%v special=%v has=%v", final, special, has)
	}
}

func TestIsMembershipGroup_HalfConfigFallback(t *testing.T) {
	// Partial admin config: only mark ssvip; unlisted defaults still membership.
	if groupRatioSetting.MembershipGroups == nil {
		groupRatioSetting.MembershipGroups = types.NewRWMap[string, bool]()
	}
	groupRatioSetting.MembershipGroups.Clear()
	groupRatioSetting.MembershipGroups.AddAll(map[string]bool{"ssvip": true})

	if !IsMembershipGroup("ssvip") {
		t.Fatal("ssvip configured true")
	}
	if !IsMembershipGroup("svip") {
		t.Fatal("svip should fall back to default membership set")
	}
	if IsMembershipGroup("grok") {
		t.Fatal("grok is never membership")
	}

	// Explicit false wins over default
	groupRatioSetting.MembershipGroups.AddAll(map[string]bool{"ssvip": true, "svip": false})
	if IsMembershipGroup("svip") {
		t.Fatal("explicit false should win")
	}

	// restore defaults for other tests
	groupRatioSetting.MembershipGroups.Clear()
	groupRatioSetting.MembershipGroups.AddAll(defaultMembershipGroups)
}
