package model

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

func TestChannelGPTContextSettingsJSONRoundTripAndDefault(t *testing.T) {
	channel := Channel{}
	if got := channel.GetOtherSettings().NormalizeGPTContextMode(); got != dto.GPTContextModeStandard {
		t.Fatalf("missing mode default = %q", got)
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{GPTContextMode: dto.GPTContextModeLongContext})
	if got := channel.GetOtherSettings().GPTContextMode; got != dto.GPTContextModeLongContext {
		t.Fatalf("round-trip mode = %q", got)
	}
}

func TestChannelValidateSettingsRejectsInvalidGPTContextMode(t *testing.T) {
	channel := Channel{OtherSettings: `{"gpt_context_mode":"invalid"}`}
	if err := channel.ValidateSettings(); err == nil {
		t.Fatal("invalid gpt_context_mode must be rejected")
	}
}
