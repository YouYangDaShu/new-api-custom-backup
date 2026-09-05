package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumeResponsesErrorUsage(t *testing.T) {
	tests := []struct {
		name              string
		usage             any
		errorUsagePresent bool
		wantCalls         int
	}{
		{name: "trusted usage is consumed", usage: &dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}, errorUsagePresent: true, wantCalls: 1},
		{name: "missing usage is not consumed", errorUsagePresent: true},
		{name: "unmarked usage is not consumed", usage: &dto.Usage{TotalTokens: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			common.SetContextKey(c, constant.ContextKeyErrorUsagePresent, tt.errorUsagePresent)
			calls := 0
			var gotUsage *dto.Usage
			consume := func(_ *gin.Context, _ *relaycommon.RelayInfo, usage *dto.Usage, _ []string) {
				calls++
				gotUsage = usage
			}

			consumeResponsesErrorUsageWith(c, &relaycommon.RelayInfo{}, tt.usage, consume)

			assert.Equal(t, tt.wantCalls, calls)
			if tt.wantCalls > 0 {
				require.NotNil(t, gotUsage)
				assert.Equal(t, 5, gotUsage.TotalTokens)
			}
		})
	}
}

func TestConsumeDirectResponsesErrorUsageRouting(t *testing.T) {
	tests := []struct {
		name              string
		model             string
		usage             any
		errorUsagePresent bool
		wantAudioCalls    int
		wantTextCalls     int
	}{
		{name: "audio model uses audio quota", model: "gpt-4o-audio-preview", usage: &dto.Usage{TotalTokens: 5}, errorUsagePresent: true, wantAudioCalls: 1},
		{name: "text model uses text quota", model: "gpt-5.6-sol", usage: &dto.Usage{TotalTokens: 5}, errorUsagePresent: true, wantTextCalls: 1},
		{name: "missing usage is not consumed", model: "gpt-4o-audio-preview", errorUsagePresent: true},
		{name: "unmarked usage is not consumed", model: "gpt-5.6-sol", usage: &dto.Usage{TotalTokens: 5}},
		{name: "nil usage is not consumed", model: "gpt-5.6-sol", usage: (*dto.Usage)(nil), errorUsagePresent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			common.SetContextKey(c, constant.ContextKeyErrorUsagePresent, tt.errorUsagePresent)
			info := &relaycommon.RelayInfo{OriginModelName: tt.model}
			audioCalls, textCalls := 0, 0

			consumeDirectResponsesErrorUsageWith(
				c,
				info,
				tt.usage,
				func(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.Usage, _ string) { audioCalls++ },
				func(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.Usage, _ []string) { textCalls++ },
			)

			assert.Equal(t, tt.wantAudioCalls, audioCalls)
			assert.Equal(t, tt.wantTextCalls, textCalls)
		})
	}
}

func TestCleanInputItemIDsRemovesOnlyInvalidMessageID(t *testing.T) {
	input := []byte(`[
		{"type":"message","id":"item_bad","role":"assistant","content":[{"type":"output_text","text":"hello"}]},
		{"type":"message","id":"msg_good","role":"assistant","content":[{"type":"output_text","text":"kept"}]},
		{"type":"reasoning","id":"item_reasoning","summary":[]},
		{"type":"custom_tool_call","id":"item_tool","call_id":"call_1","name":"exec","input":"{}"}
	]`)

	cleaned, err := cleanInputItemIDs(input)
	require.NoError(t, err)

	var items []map[string]any
	require.NoError(t, common.Unmarshal(cleaned, &items))
	require.Len(t, items, 4)

	_, hasInvalidMessageID := items[0]["id"]
	assert.False(t, hasInvalidMessageID)
	assert.Equal(t, "msg_good", items[1]["id"])
	assert.Equal(t, "item_reasoning", items[2]["id"])
	assert.Equal(t, "item_tool", items[3]["id"])
}

func TestCleanInputItemIDsLeavesStringInputUntouched(t *testing.T) {
	input := []byte(`"hello"`)

	cleaned, err := cleanInputItemIDs(input)
	require.NoError(t, err)
	assert.JSONEq(t, string(input), string(cleaned))
}
