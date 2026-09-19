package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiStreamHandlerChannelTestRejectsEOFWithoutDone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"id\":\"chatcmpl-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n",
		)),
	}
	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		IsStream:      true,
		RelayFormat:   types.RelayFormatOpenAI,
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "test-model"},
	}

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.NotNil(t, info.StreamStatus)
	require.Equal(t, relaycommon.StreamEndReasonEOF, info.StreamStatus.EndReason)
}

func TestOaiStreamHandlerChannelTestAcceptsDone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"id\":\"chatcmpl-test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n" +
				"data: [DONE]\n\n",
		)),
	}
	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		IsStream:      true,
		RelayFormat:   types.RelayFormatOpenAI,
		ChannelMeta:   &relaycommon.ChannelMeta{UpstreamModelName: "test-model"},
	}

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 2, usage.TotalTokens)
	require.NotNil(t, info.StreamStatus)
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
}
