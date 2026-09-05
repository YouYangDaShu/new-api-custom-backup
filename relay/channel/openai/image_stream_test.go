package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingImageResponseWriter struct {
	header http.Header
}

func (w *failingImageResponseWriter) Header() http.Header {
	return w.header
}

func (w *failingImageResponseWriter) WriteHeader(int) {}

func (w *failingImageResponseWriter) Write([]byte) (int, error) {
	return 0, nil
}

func (w *failingImageResponseWriter) Flush() {
	panic("client write failed")
}

func newImageTestContext(t *testing.T, body, contentType string, isStream bool) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{contentType}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		IsStream:    isStream,
	}
	return c, recorder, resp, info
}

// TestOpenaiImageStreamHandlerForwardsSSEAndUsage covers the core SSE path:
// chunks are forwarded with rebuilt event lines, usage is extracted and
// normalized (input_tokens -> prompt_tokens with details), and [DONE] is
// re-emitted to the client.
func TestOpenaiImageStreamHandlerForwardsSSEAndUsage(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`event: image_generation.partial_image`,
		`data: {"type":"image_generation.partial_image","b64_json":"partial"}`,
		``,
		`data: {"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"input_tokens_details":{"image_tokens":2,"text_tokens":1}}}`,
		``,
		`data: {"type":"image_generation.completed","b64_json":"final"}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.Equal(t, 2, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 1, usage.PromptTokensDetails.TextTokens)
	require.Contains(t, recorder.Body.String(), `event: image_generation.partial_image`)
	require.Contains(t, recorder.Body.String(), `data: {"type":"image_generation.partial_image","b64_json":"partial"}`)
	require.Contains(t, recorder.Body.String(), `data: {"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"input_tokens_details":{"image_tokens":2,"text_tokens":1}}}`)
	require.Contains(t, recorder.Body.String(), `data: [DONE]`)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
}

func TestOpenaiImageHandlerRequiresImageResultForGenerationAndEdit(t *testing.T) {
	tests := []struct {
		name      string
		relayMode int
		path      string
		body      string
		wantError bool
	}{
		{name: "generation empty object", relayMode: relayconstant.RelayModeImagesGenerations, path: "/v1/images/generations", body: `{}`, wantError: true},
		{name: "generation empty data", relayMode: relayconstant.RelayModeImagesGenerations, path: "/v1/images/generations", body: `{"data":[]}`, wantError: true},
		{name: "generation usage only", relayMode: relayconstant.RelayModeImagesGenerations, path: "/v1/images/generations", body: `{"usage":{"total_tokens":7}}`, wantError: true},
		{name: "generation empty image item", relayMode: relayconstant.RelayModeImagesGenerations, path: "/v1/images/generations", body: `{"data":[{}]}`, wantError: true},
		{name: "generation URL", relayMode: relayconstant.RelayModeImagesGenerations, path: "/v1/images/generations", body: `{"data":[{"url":"https://example.com/image.png"}]}`},
		{name: "generation base64", relayMode: relayconstant.RelayModeImagesGenerations, path: "/v1/images/generations", body: `{"data":[{"b64_json":"aW1hZ2U="}]}`},
		{name: "edit empty object", relayMode: relayconstant.RelayModeImagesEdits, path: "/v1/images/edits", body: `{}`, wantError: true},
		{name: "edit empty data", relayMode: relayconstant.RelayModeImagesEdits, path: "/v1/images/edits", body: `{"data":[]}`, wantError: true},
		{name: "edit usage only", relayMode: relayconstant.RelayModeImagesEdits, path: "/v1/images/edits", body: `{"usage":{"total_tokens":7}}`, wantError: true},
		{name: "edit empty image item", relayMode: relayconstant.RelayModeImagesEdits, path: "/v1/images/edits", body: `{"data":[{}]}`, wantError: true},
		{name: "edit URL", relayMode: relayconstant.RelayModeImagesEdits, path: "/v1/images/edits", body: `{"data":[{"url":"https://example.com/image.png"}]}`},
		{name: "edit base64", relayMode: relayconstant.RelayModeImagesEdits, path: "/v1/images/edits", body: `{"data":[{"b64_json":"aW1hZ2U="}]}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, recorder, resp, info := newImageTestContext(t, test.body, "application/json", false)
			c.Request = httptest.NewRequest(http.MethodPost, test.path, nil)
			info.RelayMode = test.relayMode

			usage, err := (&Adaptor{}).DoResponse(c, resp, info)
			if test.wantError {
				require.NotNil(t, err)
				if strings.Contains(test.name, "usage only") {
					require.NotNil(t, usage)
					require.Equal(t, 7, usage.(*dto.Usage).TotalTokens)
					require.True(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
				} else {
					require.Nil(t, usage)
				}
				require.Empty(t, recorder.Body.String())
				return
			}
			require.Nil(t, err)
			require.NotNil(t, usage)
			require.Equal(t, "image_per_call", usage.(*dto.Usage).UsageSource)
			require.JSONEq(t, test.body, recorder.Body.String())
		})
	}
}

// TestOpenaiImageStreamHandlerWrapsJSONResponse covers the non-SSE fallback:
// a JSON upstream response is wrapped into pseudo-SSE completed events.
func TestOpenaiImageStreamHandlerWrapsJSONResponse(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"created":1710000000,"data":[{"b64_json":"final","revised_prompt":"draw a cat"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"input_tokens_details":{"image_tokens":2,"text_tokens":1}}}`

	c, recorder, resp, info := newImageTestContext(t, body, "application/json", true)

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.Equal(t, 2, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 1, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("Content-Length"))
	require.Contains(t, recorder.Body.String(), `event: image_generation.completed`)
	require.Contains(t, recorder.Body.String(), `"type":"image_generation.completed"`)
	require.Contains(t, recorder.Body.String(), `"b64_json":"final"`)
	require.Contains(t, recorder.Body.String(), `"revised_prompt":"draw a cat"`)
	require.Contains(t, recorder.Body.String(), `data: [DONE]`)
}

// TestOpenaiImageHandlersReturnJSONError covers JSON error responses for both
// entry points: the non-streaming handler and the stream handler's non-SSE
// fallback. Neither must leak the error body to the client.
func TestOpenaiImageHandlersReturnJSONError(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"error":{"message":"content moderation failed","type":"upstream_error","code":"content_moderation_failed","status":502}}`

	t.Run("non-streaming handler", func(t *testing.T) {
		c, recorder, resp, info := newImageTestContext(t, body, "application/json", false)

		usage, err := OpenaiImageHandler(c, info, resp)
		require.Nil(t, usage)
		require.NotNil(t, err)
		require.Equal(t, http.StatusOK, err.StatusCode)
		oaiError := err.ToOpenAIError()
		require.Equal(t, "content moderation failed", oaiError.Message)
		require.Equal(t, "upstream_error", oaiError.Type)
		require.Equal(t, "content_moderation_failed", oaiError.Code)
		require.Empty(t, recorder.Body.String())
	})

	t.Run("stream handler JSON fallback", func(t *testing.T) {
		c, recorder, resp, info := newImageTestContext(t, body, "application/json", true)

		usage, err := OpenaiImageStreamHandler(c, info, resp)
		require.Nil(t, usage)
		require.NotNil(t, err)
		require.Equal(t, http.StatusOK, err.StatusCode)
		require.Equal(t, "content moderation failed", err.ToOpenAIError().Message)
		require.Empty(t, recorder.Body.String())
	})
}

func TestOpenaiImageHandlersPreserveUsageOnError(t *testing.T) {
	body := `{"error":{"message":"generation failed","type":"upstream_error"},"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`
	for _, test := range []struct {
		name   string
		stream bool
	}{
		{name: "non-streaming"},
		{name: "JSON stream fallback", stream: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, recorder, resp, info := newImageTestContext(t, body, "application/json", test.stream)
			resp.StatusCode = http.StatusTooManyRequests
			var usage *dto.Usage
			var err *types.NewAPIError
			if test.stream {
				usage, err = OpenaiImageStreamHandler(c, info, resp)
			} else {
				usage, err = OpenaiImageHandler(c, info, resp)
			}
			require.NotNil(t, err)
			require.NotNil(t, usage)
			require.Equal(t, 3, usage.PromptTokens)
			require.Equal(t, 4, usage.CompletionTokens)
			require.Equal(t, 7, usage.TotalTokens)
			require.Equal(t, "image_per_call", usage.UsageSource)
			require.True(t, common.GetContextKeyBool(c, constant.ContextKeyUpstreamUsagePresent))
			require.True(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestOpenaiImageHandlersPreserveUsageWhenImageShapeIsMalformed(t *testing.T) {
	body := `{"data":"invalid","usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`
	for _, stream := range []bool{false, true} {
		c, _, resp, info := newImageTestContext(t, body, "application/json", stream)
		var usage *dto.Usage
		var err *types.NewAPIError
		if stream {
			usage, err = OpenaiImageStreamHandler(c, info, resp)
		} else {
			usage, err = OpenaiImageHandler(c, info, resp)
		}
		require.NotNil(t, err)
		require.NotNil(t, usage)
		require.Equal(t, 7, usage.TotalTokens)
		require.True(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
	}
}

// TestOpenaiImageStreamHandlerRecordsUpstreamErrorEvent verifies that an error
// event inside the SSE stream is recorded as a soft error while the payload is
// still forwarded to the client.
func TestOpenaiImageStreamHandlerRecordsUpstreamErrorEvent(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`event: image_generation.partial_image`,
		`data: {"type":"image_generation.partial_image","b64_json":"partial"}`,
		``,
		`event: error`,
		`data: {"type":"upstream_error","error":{"message":"stream error: stream ID 77; INTERNAL_ERROR; received from peer"}}`,
		``,
	}, "\n")

	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.NotNil(t, info.StreamStatus)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.True(t, types.IsSkipRetryError(err))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	require.Equal(t, "usage_without_completed_result", common.GetContextKeyString(c, constant.ContextKeyBillingPendingReason))
	reason, _ := info.StreamStatus.Snapshot()
	require.Equal(t, relaycommon.StreamEndReasonHandlerStop, reason)
	errorCount, errors := info.StreamStatus.ErrorsSnapshot()
	require.Equal(t, 1, errorCount)
	require.Contains(t, errors[0].Message, "INTERNAL_ERROR")
	// The scanner strips the upstream "event: error" line; the event name is
	// rebuilt from the JSON "type" field (upstream_error). The error message
	// is still forwarded in the data: payload (stream ID 77).
	require.Contains(t, recorder.Body.String(), `event: upstream_error`)
	require.Contains(t, recorder.Body.String(), `stream ID 77`)
}

func TestOpenaiImageStreamHandlerRejectsIncompleteStream(t *testing.T) {
	body := "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n"
	c, _, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.True(t, types.IsSkipRetryError(err))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	require.Equal(t, "usage_without_completed_result", common.GetContextKeyString(c, constant.ContextKeyBillingPendingReason))
	reason, _ := info.StreamStatus.Snapshot()
	require.Equal(t, relaycommon.StreamEndReasonEOF, reason)
}

func TestOpenaiImageStreamHandlerEmptyStreamIsRetryable(t *testing.T) {
	c, _, resp, info := newImageTestContext(t, "", "text/event-stream", true)
	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.Error(t, err)
	require.False(t, types.IsSkipRetryError(err))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
}

func TestOpenaiImageStreamHandlerMalformedBeforeDeliveryIsRetryableAndNotPending(t *testing.T) {
	c, _, resp, _ := newImageTestContext(t, "data: {malformed}\n", "text/event-stream", true)
	usage, err := OpenaiImageStreamHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, DisablePing: true}, resp)
	require.Nil(t, usage)
	require.Error(t, err)
	require.False(t, types.IsSkipRetryError(err))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
}

func TestOpenaiImageStreamHandlerRejectsUsageWithoutCompletedEvent(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "usage only",
			body: "data: {\"usage\":{\"input_tokens\":3,\"output_tokens\":4,\"total_tokens\":7}}\n\ndata: [DONE]\n\n",
		},
		{
			name: "partial and usage",
			body: "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\ndata: {\"usage\":{\"total_tokens\":7}}\n\ndata: [DONE]\n\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _, resp, info := newImageTestContext(t, test.body, "text/event-stream", true)

			usage, err := OpenaiImageStreamHandler(c, info, resp)
			require.NotNil(t, err)
			require.True(t, types.IsSkipRetryError(err))
			require.True(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
			require.False(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			require.NotNil(t, usage)
			require.Equal(t, 7, usage.TotalTokens)
			if test.name == "usage only" {
				require.False(t, common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered), "usage/status frames are not image payload")
			} else {
				require.True(t, common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered), "partial image is real image payload")
			}
		})
	}
}

func TestOpenaiImageStreamHandlerAcceptsCompletedEventWithoutUsage(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"image_generation.completed","b64_json":"final"}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	c, _, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Zero(t, usage.PromptTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Zero(t, usage.TotalTokens)
	require.Equal(t, "image_per_call", usage.UsageSource)
}

func TestOpenaiImageJSONAsStreamWriteFailurePreservesValidResult(t *testing.T) {
	writer := &failingImageResponseWriter{header: make(http.Header)}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"final"}]}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, IsStream: true, StreamStatus: relaycommon.NewStreamStatus()}

	usage, err := OpenaiImageJSONAsStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, "image_per_call", usage.UsageSource)
	reason, endErr := info.StreamStatus.Snapshot()
	require.Equal(t, relaycommon.StreamEndReasonClientGone, reason)
	require.ErrorContains(t, endErr, "client write failed")
}
