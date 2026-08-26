package service

import "testing"

func TestParseManualTokenGroups(t *testing.T) {
	got := ParseManualTokenGroups("@manual:claude_kiro反代,codex_pro,codex_混合,grok,image2")
	want := []string{"claude_kiro反代", "codex_pro", "codex_混合", "grok", "image2"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("i=%d got=%q want=%q", i, got[i], want[i])
		}
	}
}

func TestEncodeManualTokenGroup(t *testing.T) {
	if g := EncodeManualTokenGroup([]string{"grok"}); g != "grok" {
		t.Fatalf("single: %q", g)
	}
	if g := EncodeManualTokenGroup([]string{"grok", "gpt plus 号池", "grok"}); g != "@manual:grok,gpt plus 号池" {
		t.Fatalf("multi: %q", g)
	}
	if g := EncodeManualTokenGroup([]string{"", "auto", "@manual:x"}); g != "" {
		t.Fatalf("empty: %q", g)
	}
}

func TestIsMultiGroupToken(t *testing.T) {
	if !IsMultiGroupToken("auto") {
		t.Fatal("auto")
	}
	if !IsMultiGroupToken("@manual:a,b") {
		t.Fatal("manual")
	}
	if IsMultiGroupToken("grok") {
		t.Fatal("single")
	}
}

func TestNormalizeTokenGroup(t *testing.T) {
	got := NormalizeTokenGroup("@manual: grok , Claude , grok ")
	if got != "@manual:grok,Claude" {
		t.Fatalf("got %q", got)
	}
}

func TestDisplayTokenGroup(t *testing.T) {
	if DisplayTokenGroup("@manual:a,b", "grok") != "manual(grok)" {
		t.Fatal("manual display")
	}
	if DisplayTokenGroup("auto", "grok") != "auto(grok)" {
		t.Fatal("auto display")
	}
}
