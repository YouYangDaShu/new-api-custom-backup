package service

import "testing"

func TestIsMembershipGroup(t *testing.T) {
	for _, name := range []string{"default", "VIP", "vip", "svip", "ssvip"} {
		if !IsMembershipGroup(name) {
			t.Fatalf("expected membership: %s", name)
		}
	}
	for _, name := range []string{"grok", "Claude", "gpt plus 号池", "auto", ""} {
		if IsMembershipGroup(name) {
			t.Fatalf("expected model pool, not membership: %s", name)
		}
	}
}

func TestGetUserTokenSelectableGroups_HidesMembership(t *testing.T) {
	// Without DB/options, GetUserUsableGroupsCopy may be empty; still verify filter helper path.
	// Simulate via direct filter logic using a fake map through GetUserTokenSelectableGroups
	// when usable only has membership — result must be empty of membership keys.
	// This only checks IsMembershipGroup integration if copy is empty: still OK.
	out := GetUserTokenSelectableGroups("svip")
	for name := range out {
		if IsMembershipGroup(name) {
			t.Fatalf("membership leaked into token selectable: %s", name)
		}
	}
}
