package openai

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingResponseWriter struct {
	gin.ResponseWriter
}

func (failingResponseWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type cancelOnWriteResponseWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w cancelOnWriteResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.cancel()
	return n, err
}

func (w cancelOnWriteResponseWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func TestOpenaiHandlerUsagePresence(t *testing.T) {
	tests := []struct {
		name        string
		usageJSON   string
		wantMissing bool
		wantPresent bool
		wantTotal   int
	}{
		{name: "absent", wantMissing: true},
		{name: "null", usageJSON: `,"usage":null`, wantMissing: true},
		{name: "explicit zero", usageJSON: `,"usage":{}`, wantPresent: true},
		{name: "nonzero", usageJSON: `,"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}`, wantPresent: true, wantTotal: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			body := `{"id":"chat_1","model":"gpt-test","choices":[]` + tt.usageJSON + `}`
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}

			usage, err := OpenaiHandler(c, info, resp)

			require.Nil(t, err)
			require.NotNil(t, usage)
			require.Equal(t, tt.wantTotal, usage.TotalTokens)
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			require.Equal(t, tt.wantPresent, common.GetContextKeyBool(c, constant.ContextKeyUpstreamUsagePresent))
		})
	}
}

func TestOpenaiHandlerMarksOnlyRealNonStreamingPayloadAsDelivered(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		delivered bool
	}{
		{name: "assistant text", body: `{"id":"chat_1","choices":[{"message":{"role":"assistant","content":"ok"}}]}`, delivered: true},
		{name: "assistant reasoning", body: `{"id":"chat_1","choices":[{"message":{"role":"assistant","reasoning_content":"think"}}]}`, delivered: true},
		{name: "assistant tool call", body: `{"id":"chat_1","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`, delivered: true},
		{name: "finish-only", body: `{"id":"chat_1","choices":[{"message":{"role":"assistant"},"finish_reason":"stop"}]}`},
		{name: "empty choices", body: `{"id":"chat_1","choices":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tt.body)), Header: make(http.Header)}
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatOpenAI,
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: "gpt-test",
				},
			}

			_, err := OpenaiHandler(c, info, resp)
			require.Nil(t, err)
			require.Equal(t, tt.delivered, common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered))
		})
	}
}

func TestOpenaiHandlerDoesNotMarkPayloadWhenClientWriteFails(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Writer = failingResponseWriter{ResponseWriter: c.Writer}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)), Header: make(http.Header)}
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}

	_, err := OpenaiHandler(c, info, resp)

	require.Nil(t, err)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered))
}

func TestOpenaiHandlerErrorUsagePresence(t *testing.T) {
	for _, tt := range []struct {
		name        string
		usage       string
		wantUsage   bool
		wantMissing bool
	}{
		{name: "empty object", usage: `,"usage":{}`, wantUsage: true},
		{name: "nonzero", usage: `,"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}`, wantUsage: true},
		{name: "null", usage: `,"usage":null`, wantMissing: true},
		{name: "absent", wantMissing: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			body := `{"error":{"type":"server_error","message":"failed"}` + tt.usage + `}`
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}

			usage, err := OpenaiHandler(c, info, resp)
			require.Error(t, err)
			require.True(t, types.IsSkipRetryError(err))
			require.Equal(t, tt.wantUsage, usage != nil)
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			require.Equal(t, !tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
		})
	}
}

func TestOaiStreamHandlerUsagePresence(t *testing.T) {
	tests := []struct {
		name        string
		usageJSON   string
		wantMissing bool
		wantPresent bool
		wantTotal   int
	}{
		{name: "absent", wantMissing: true},
		{name: "null", usageJSON: `,"usage":null`, wantMissing: true},
		{name: "explicit zero", usageJSON: `,"usage":{}`, wantPresent: true},
		{name: "nonzero", usageJSON: `,"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}`, wantPresent: true, wantTotal: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			body := `data: {"id":"chat_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]` + tt.usageJSON + `}` + "\ndata: [DONE]\n"
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatOpenAI,
				RelayMode:   relayconstant.RelayModeChatCompletions,
				DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
			}

			usage, err := OaiStreamHandler(c, info, resp)

			require.Nil(t, err)
			require.NotNil(t, usage)
			require.Equal(t, tt.wantTotal, usage.TotalTokens)
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			require.Equal(t, tt.wantPresent, common.GetContextKeyBool(c, constant.ContextKeyUpstreamUsagePresent))
		})
	}
}

func TestOaiStreamHandlerPreservesObservedUsageBeforeTrailingEvent(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"id":"chat_1","model":"gpt-test","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`,
		`data: {"id":"chat_1","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		RelayMode:   relayconstant.RelayModeChatCompletions,
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
	}

	usage, err := OaiStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 5, usage.TotalTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyUpstreamUsagePresent))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
}

func TestOaiStreamHandlerFailsClosedOnAbnormalEnd(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantSkip bool
	}{
		{name: "empty", body: "", wantSkip: false},
		{name: "malformed first", body: "data: {malformed}\n", wantSkip: false},
		{name: "payload then eof", body: `data: {"id":"chat_1","choices":[{"index":0,"delta":{"content":"partial"}}]}` + "\n", wantSkip: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tt.body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
			_, err := OaiStreamHandler(c, info, resp)
			require.Error(t, err)
			require.Equal(t, tt.wantSkip, types.IsSkipRetryError(err))
		})
	}
}

func TestOaiStreamHandlerRoleOnlyChunkRemainsRetryableOnAbnormalEnd(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := "data: {\"id\":\"chat_1\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}

	_, err := OaiStreamHandler(c, info, resp)
	require.Error(t, err)
	require.False(t, types.IsSkipRetryError(err))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered))
}

func TestOaiStreamHandlerUsageOnlyChunkSkipsRetryWithoutPayloadMarker(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := "data: {\"id\":\"chat_1\",\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}

	usage, err := OaiStreamHandler(c, info, resp)
	require.Error(t, err)
	require.True(t, types.IsSkipRetryError(err))
	require.NotNil(t, usage)
	require.Equal(t, 5, usage.TotalTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyUpstreamUsagePresent))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered))
}

func TestOaiStreamHandlerAbnormalEndUsageMarkerRequiresStructuredUsage(t *testing.T) {
	tests := []struct {
		name             string
		usage            string
		wantPresent      bool
		wantMissing      bool
		wantErrorPresent bool
	}{
		{name: "structured explicit zero", usage: `,"usage":{}`, wantPresent: true, wantErrorPresent: true},
		{name: "structured nonzero", usage: `,"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}`, wantPresent: true, wantErrorPresent: true},
		{name: "null", usage: `,"usage":null`, wantMissing: true, wantErrorPresent: false},
		{name: "absent", wantMissing: true, wantErrorPresent: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			body := `data: {"id":"chat_1","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n"
			if tt.usage != "" {
				body = `data: {"id":"chat_1","choices":[]` + tt.usage + "}\n"
			} else {
				body = `data: {"id":"chat_1","choices":[]}` + "\n"
			}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}

			usage, err := OaiStreamHandler(c, info, resp)
			require.Error(t, err)
			require.NotNil(t, usage)
			require.Equal(t, tt.wantPresent, common.GetContextKeyBool(c, constant.ContextKeyUpstreamUsagePresent))
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			require.Equal(t, tt.wantErrorPresent, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
		})
	}
}

func TestOaiStreamHandlerDelayedUsageAfterClientCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	c.Writer = cancelOnWriteResponseWriter{ResponseWriter: c.Writer, cancel: cancel}
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	done := make(chan struct{})
	var usage *dto.Usage
	var streamErr *types.NewAPIError
	go func() {
		usage, streamErr = OaiStreamHandler(c, info, resp)
		close(done)
	}()

	_, err := io.WriteString(pw, "data: {\"id\":\"chat_1\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n")
	require.NoError(t, err)
	_, err = io.WriteString(pw, "data: {\"id\":\"chat_1\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n")
	require.NoError(t, err)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for downstream cancellation")
	}
	_, err = io.WriteString(pw, "data: {\"id\":\"chat_1\",\"choices\":[],\"usage\":{\"prompt_tokens\":49553,\"completion_tokens\":916,\"total_tokens\":50469}}\n")
	require.NoError(t, err)
	_, err = io.WriteString(pw, "data: [DONE]\n")
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delayed usage stream")
	}
	require.Error(t, streamErr)
	require.NotNil(t, usage)
	require.Equal(t, 50469, usage.TotalTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
}
