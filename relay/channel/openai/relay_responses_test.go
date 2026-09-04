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
		status      string
		usageJSON   string
		wantUsage   bool
		wantSettled bool
		wantMissing bool
		wantTotal   int
	}{
		{name: "failed with usage", status: "failed", usageJSON: `,"usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}`, wantUsage: true, wantSettled: true, wantTotal: 9},
		{name: "error missing usage", status: "error", wantMissing: true},
		{name: "cancelled with zero usage", status: "cancelled", usageJSON: `,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}`, wantUsage: true, wantSettled: true},
		{name: "incomplete missing usage", status: "incomplete", wantMissing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"status":"` + tt.status + `"` + tt.usageJSON + `}`
			c, _, resp, info := newResponsesChatTestContext(t, body, false)
			usage, err := OaiResponsesHandler(c, info, resp)
			require.Error(t, err)
			require.Equal(t, tt.wantSettled, common.GetContextKeyBool(c, constant.ContextKeyBillingSettledOnError))
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			if tt.wantUsage {
				require.NotNil(t, usage)
				require.Equal(t, tt.wantTotal, usage.TotalTokens)
			} else {
				require.Nil(t, usage)
			}
		})
	}
}

func TestOaiResponsesStreamHandlerFailureUsageContract(t *testing.T) {
	tests := []struct {
		status      string
		usageJSON   string
		wantUsage   bool
		wantSettled bool
		wantMissing bool
		wantTotal   int
	}{
		{status: "response.failed", usageJSON: `,"usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}`, wantUsage: true, wantSettled: true, wantTotal: 9},
		{status: "response.failed", wantMissing: true},
		{status: "response.error", usageJSON: `,"usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}`, wantUsage: true, wantSettled: true, wantTotal: 9},
		{status: "response.error", wantMissing: true},
		{status: "response.cancelled", usageJSON: `,"usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}`, wantUsage: true, wantSettled: true, wantTotal: 9},
		{status: "response.cancelled", wantMissing: true},
		{status: "response.incomplete", usageJSON: `,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}`, wantUsage: true, wantSettled: true},
		{status: "response.incomplete", wantMissing: true},
	}
	for _, tt := range tests {
		name := tt.status + "/missing usage"
		if tt.wantUsage {
			name = tt.status + "/usage present"
		}
		t.Run(name, func(t *testing.T) {
			body := `data: {"type":"` + tt.status + `","response":{"status":"failed"` + tt.usageJSON + `}}` + "\n"
			c, _, resp, info := newResponsesChatTestContext(t, body, true)
			usage, err := OaiResponsesStreamHandler(c, info, resp)
			require.Error(t, err)
			require.Equal(t, tt.status, common.GetContextKeyString(c, constant.ContextKeyUpstreamTerminalStatus))
			require.Equal(t, tt.wantSettled, common.GetContextKeyBool(c, constant.ContextKeyBillingSettledOnError))
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			if tt.wantUsage {
				require.NotNil(t, usage)
				require.Equal(t, tt.wantTotal, usage.TotalTokens)
			} else {
				require.Nil(t, usage)
			}
		})
	}
}

func TestOaiResponsesStreamHandlerRejectsMalformedJSON(t *testing.T) {
	c, _, resp, info := newResponsesChatTestContext(t, "data: {malformed}\n", true)
	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Error(t, err)
	require.Nil(t, usage)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
}

func TestOaiResponsesStreamHandlerRejectsEOFWithoutTerminal(t *testing.T) {
	body := `data: {"type":"response.output_text.delta","delta":"partial"}` + "\n"
	c, _, resp, info := newResponsesChatTestContext(t, body, true)
	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Error(t, err)
	require.Nil(t, usage)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
}

func TestOaiResponsesStreamHandlerSuccessUsageContract(t *testing.T) {
	tests := []struct {
		status      string
		usageJSON   string
		wantMissing bool
	}{
		{status: "response.completed", usageJSON: `,"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}`},
		{status: "response.done", usageJSON: `,"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}`},
		{status: "response.completed", wantMissing: true},
		{status: "response.done", wantMissing: true},
	}
	for _, tt := range tests {
		name := tt.status + "/usage present"
		if tt.wantMissing {
			name = tt.status + "/missing usage"
		}
		t.Run(name, func(t *testing.T) {
			body := `data: {"type":"` + tt.status + `","response":{"status":"completed"` + tt.usageJSON + `}}` + "\n"
			c, _, resp, info := newResponsesChatTestContext(t, body, true)
			usage, err := OaiResponsesStreamHandler(c, info, resp)
			require.Nil(t, err)
			require.NotNil(t, usage)
			require.False(t, common.GetContextKeyBool(c, constant.ContextKeyBillingSettledOnError))
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
		})
	}
}
