package relay

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type responsesErrorAdaptor struct {
	response *http.Response
}

func (a *responsesErrorAdaptor) Init(*relaycommon.RelayInfo)                          {}
func (a *responsesErrorAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) { return "", nil }
func (a *responsesErrorAdaptor) SetupRequestHeader(*gin.Context, *http.Header, *relaycommon.RelayInfo) error {
	return nil
}
func (a *responsesErrorAdaptor) ConvertOpenAIRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeneralOpenAIRequest) (any, error) {
	return nil, nil
}
func (a *responsesErrorAdaptor) ConvertRerankRequest(*gin.Context, int, dto.RerankRequest) (any, error) {
	return nil, nil
}
func (a *responsesErrorAdaptor) ConvertEmbeddingRequest(*gin.Context, *relaycommon.RelayInfo, dto.EmbeddingRequest) (any, error) {
	return nil, nil
}
func (a *responsesErrorAdaptor) ConvertAudioRequest(*gin.Context, *relaycommon.RelayInfo, dto.AudioRequest) (io.Reader, error) {
	return nil, nil
}
func (a *responsesErrorAdaptor) ConvertImageRequest(*gin.Context, *relaycommon.RelayInfo, dto.ImageRequest) (any, error) {
	return nil, nil
}
func (a *responsesErrorAdaptor) ConvertOpenAIResponsesRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return request, nil
}
func (a *responsesErrorAdaptor) DoRequest(*gin.Context, *relaycommon.RelayInfo, io.Reader) (any, error) {
	return a.response, nil
}
func (a *responsesErrorAdaptor) DoResponse(*gin.Context, *http.Response, *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	return nil, nil
}
func (a *responsesErrorAdaptor) GetModelList() []string { return nil }
func (a *responsesErrorAdaptor) GetChannelName() string { return "test" }
func (a *responsesErrorAdaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	return nil, nil
}
func (a *responsesErrorAdaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, nil
}

func TestChatCompletionsViaResponsesPreservesErrorUsage(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "buffered stream", contentType: "text/event-stream", body: "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n"},
		{name: "ordinary response", contentType: "application/json", body: `{"id":"resp_1","status":"failed","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{}`))
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(tt.body))}
			resp.Header.Set("Content-Type", tt.contentType)
			info := &relaycommon.RelayInfo{Request: &dto.GeneralOpenAIRequest{Model: "gpt-test"}, RelayMode: 1, RequestURLPath: "/v1/chat/completions", ChannelMeta: &relaycommon.ChannelMeta{}}

			usage, err := chatCompletionsViaResponses(c, info, &responsesErrorAdaptor{response: resp}, info.Request.(*dto.GeneralOpenAIRequest))
			require.Error(t, err)
			require.NotNil(t, usage)
			assert.Equal(t, 5, usage.TotalTokens)
		})
	}
}

func TestIsResponsesEventStreamContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{name: "plain", contentType: "text/event-stream", want: true},
		{name: "mixed case with charset", contentType: "Text/Event-Stream; charset=utf-8", want: true},
		{name: "json", contentType: "application/json", want: false},
		{name: "empty", contentType: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isResponsesEventStreamContentType(tt.contentType))
		})
	}
}
