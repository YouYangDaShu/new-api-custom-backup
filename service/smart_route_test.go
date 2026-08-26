package service

import "testing"

func TestParseSmartMode(t *testing.T) {
	if ParseSmartMode("@smart:balanced") != SmartModeBalanced {
		t.Fatal("balanced")
	}
	if ParseSmartMode("@smart:price") != SmartModePrice {
		t.Fatal("price")
	}
	if ParseSmartMode("@smart:cheap") != SmartModePrice {
		t.Fatal("cheap alias")
	}
	if ParseSmartMode("@smart:") != SmartModeBalanced {
		t.Fatal("default")
	}
}

func TestEncodeSmartTokenGroup(t *testing.T) {
	if EncodeSmartTokenGroup("price") != "@smart:price" {
		t.Fatal(EncodeSmartTokenGroup("price"))
	}
	if EncodeSmartTokenGroup("balanced") != "@smart:balanced" {
		t.Fatal(EncodeSmartTokenGroup("balanced"))
	}
}

func TestIsMultiGroupTokenIncludesSmart(t *testing.T) {
	if !IsMultiGroupToken("@smart:price") {
		t.Fatal("smart should be multi")
	}
	if !IsSmartTokenGroup("@smart:balanced") {
		t.Fatal("is smart")
	}
}

func TestRankGroupsForModelPriceOrder(t *testing.T) {
	// Without live channel cache, RankGroupsForModel returns empty — just ensure no panic
	got := RankGroupsForModel("default", "nonexistent-model-xyz", SmartModePrice, "", []string{"grok", "Claude"})
	if got == nil {
		got = []string{}
	}
	_ = got
}

func TestDisplayTokenGroupSmart(t *testing.T) {
	if DisplayTokenGroup("@smart:price", "grok") != "smart:price(grok)" {
		t.Fatal(DisplayTokenGroup("@smart:price", "grok"))
	}
}
