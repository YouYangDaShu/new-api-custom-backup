package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesHandlerFailureUsageContract(t *testing.T) {
	tests := []struct {
		name        string
		usageJSON   string
		wantUsage   bool
		wantSettled bool
		wantMissing bool
	}{
		{name: "usage present", usageJSON: `,"usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}`, wantUsage: true, wantSettled: true},
		{name: "usage missing", wantMissing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"status":"failed"` + tt.usageJSON + `}`
			c, _, resp, info := newResponsesChatTestContext(t, body, false)
			usage, err := OaiResponsesHandler(c, info, resp)
			require.Error(t, err)
			require.Equal(t, tt.wantSettled, common.GetContextKeyBool(c, constant.ContextKeyBillingSettledOnError))
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			if tt.wantUsage {
				require.NotNil(t, usage)
				require.Equal(t, 9, usage.TotalTokens)
			} else {
				require.Nil(t, usage)
			}
		})
	}
}

func TestOaiResponsesStreamHandlerFailureUsageContract(t *testing.T) {
	tests := []struct {
		name        string
		usageJSON   string
		wantUsage   bool
		wantSettled bool
		wantMissing bool
	}{
		{name: "usage present", usageJSON: `,"usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}`, wantUsage: true, wantSettled: true},
		{name: "usage missing", wantMissing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `data: {"type":"response.failed","response":{"status":"failed"` + tt.usageJSON + `}}` + "\n"
			c, _, resp, info := newResponsesChatTestContext(t, body, true)
			usage, err := OaiResponsesStreamHandler(c, info, resp)
			require.Error(t, err)
			require.Equal(t, "response.failed", common.GetContextKeyString(c, constant.ContextKeyUpstreamTerminalStatus))
			require.Equal(t, tt.wantSettled, common.GetContextKeyBool(c, constant.ContextKeyBillingSettledOnError))
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			if tt.wantUsage {
				require.NotNil(t, usage)
				require.Equal(t, 9, usage.TotalTokens)
			} else {
				require.Nil(t, usage)
			}
		})
	}
}
