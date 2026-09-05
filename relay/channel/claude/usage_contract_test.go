package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newClaudeUsageTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	return c
}

func TestClaudeStreamHandlerPreservesUsageBeforeError(t *testing.T) {
	c := newClaudeUsageTestContext()
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":7,"output_tokens":0}}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
		`data: {"type":"error","error":{"type":"overloaded_error","message":"upstream failed"}}`,
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}

	usage, err := ClaudeStreamHandler(c, resp, info)
	require.NotNil(t, err)
	require.True(t, types.IsSkipRetryError(err))
	require.NotNil(t, usage)
	require.Equal(t, 7, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 11, usage.TotalTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
}

func TestClaudeStreamHandlerErrorWithoutUsageIsMissing(t *testing.T) {
	c := newClaudeUsageTestContext()
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`data: {"type":"error","error":{"type":"overloaded_error","message":"upstream failed"}}`)),
		Header:     make(http.Header),
	}

	usage, err := ClaudeStreamHandler(c, resp, info)
	require.Error(t, err)
	require.True(t, types.IsSkipRetryError(err))
	require.Nil(t, usage)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
}

func TestClaudeStreamHandlerPreservesExplicitZeroUsageBeforeError(t *testing.T) {
	c := newClaudeUsageTestContext()
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{}}}`,
		`data: {"type":"error","error":{"type":"overloaded_error","message":"upstream failed"}}`,
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}

	usage, err := ClaudeStreamHandler(c, resp, info)
	require.Error(t, err)
	require.True(t, types.IsSkipRetryError(err))
	require.NotNil(t, usage)
	require.Zero(t, usage.TotalTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
}

func TestClaudeHandlerErrorUsagePresence(t *testing.T) {
	tests := []struct {
		name      string
		usageJSON string
		wantTotal int
	}{
		{name: "explicit zero", usageJSON: `{}`, wantTotal: 0},
		{name: "nonzero", usageJSON: `{"input_tokens":3,"output_tokens":2}`, wantTotal: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newClaudeUsageTestContext()
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			body := `{"type":"error","error":{"type":"overloaded_error","message":"upstream failed"},"usage":` + tt.usageJSON + `}`
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}

			usage, err := ClaudeHandler(c, resp, info)
			require.Error(t, err)
			require.True(t, types.IsSkipRetryError(err))
			require.NotNil(t, usage)
			require.Equal(t, tt.wantTotal, usage.TotalTokens)
			require.True(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
			require.False(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
		})
	}
}

func TestClaudeStreamHandlerMalformedRetryBoundary(t *testing.T) {
	t.Run("before delivery", func(t *testing.T) {
		c := newClaudeUsageTestContext()
		info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {malformed}\n")), Header: make(http.Header)}

		usage, err := ClaudeStreamHandler(c, resp, info)

		require.Error(t, err)
		require.Nil(t, usage)
		require.False(t, types.IsSkipRetryError(err))
	})

	t.Run("after delivery", func(t *testing.T) {
		c := newClaudeUsageTestContext()
		info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
		body := strings.Join([]string{
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}`,
			`data: {malformed}`,
		}, "\n")
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}

		usage, err := ClaudeStreamHandler(c, resp, info)

		require.Error(t, err)
		require.Nil(t, usage)
		require.True(t, types.IsSkipRetryError(err))
	})
}

func TestClaudeStreamHandlerMissingTerminalBeforeDeliveryCanRetry(t *testing.T) {
	c := newClaudeUsageTestContext()
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}

	usage, err := ClaudeStreamHandler(c, resp, info)

	require.Error(t, err)
	require.Nil(t, usage)
	require.False(t, types.IsSkipRetryError(err))
}

func TestClaudeLifecycleFramesDoNotCreateDeliveryEvidence(t *testing.T) {
	for _, event := range []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-test"}}`,
		`{"type":"ping"}`,
	} {
		c := newClaudeUsageTestContext()
		info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
		body := "data: " + event + "\ndata: {malformed}\n"
		_, err := ClaudeStreamHandler(c, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, info)
		require.Error(t, err)
		require.False(t, types.IsSkipRetryError(err))
		require.False(t, common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered))
	}
}
