package gemini

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiChatHandlerCompletionTokensExcludeToolUsePromptTokens(t *testing.T) {
	t.Parallel()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiStreamHandlerCompletionTokensExcludeToolUsePromptTokens(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		DisablePing:     true,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiChatStreamHandlerAbnormalEndRetryEvidence(t *testing.T) {
	valid := `{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`
	for _, tt := range []struct {
		name     string
		body     string
		wantSkip bool
	}{
		{name: "malformed first", body: "data: {malformed}\n", wantSkip: false},
		{name: "malformed after payload", body: "data: " + valid + "\ndata: {malformed}\n", wantSkip: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(tt.body))}
			_, err := GeminiChatStreamHandler(c, info, resp)
			require.Error(t, err)
			require.Equal(t, tt.wantSkip, types.IsSkipRetryError(err))
		})
	}
}

func TestGeminiChatStreamHandlerRoleOnlyDoesNotCreateRetryEvidence(t *testing.T) {
	body := `{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}` + "\ndata: {malformed}\n"
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}

	_, err := GeminiChatStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	require.Error(t, err)
	require.False(t, types.IsSkipRetryError(err))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered))
}

func TestGeminiTextGenerationHandlerPromptTokensIncludeToolUsePromptTokens(t *testing.T) {
	t.Parallel()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-flash-preview:generateContent", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiTextGenerationHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestBuildUsageFromGeminiMetadataPreservesModalitiesAndCache(t *testing.T) {
	t.Parallel()

	usage := buildUsageFromGeminiMetadata(dto.GeminiUsageMetadata{
		PromptTokenCount:        7,
		ToolUsePromptTokenCount: 5,
		CandidatesTokenCount:    11,
		TotalTokenCount:         23,
		CachedContentTokenCount: 3,
		PromptTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "TEXT", TokenCount: 7},
		},
		ToolUsePromptTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "TEXT", TokenCount: 5},
		},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "IMAGE", TokenCount: 11},
		},
	})

	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 12, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 11, usage.CompletionTokens)
	require.Equal(t, 11, usage.CompletionTokenDetails.ImageTokens)
	require.Equal(t, 23, usage.TotalTokens)
}

func TestGeminiChatHandlerFailsClosedWithoutUsageMetadata(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		usageJSON   string
		wantMissing bool
	}{
		{name: "missing", usageJSON: "", wantMissing: true},
		{name: "null", usageJSON: `,"usageMetadata":null`, wantMissing: true},
		{name: "explicit zero", usageJSON: `,"usageMetadata":{}`, wantMissing: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatGemini, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}
			info.SetEstimatePromptTokens(20)
			body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]` + tt.usageJSON + `}`

			usage, newAPIError := GeminiChatHandler(c, info, &http.Response{Body: io.NopCloser(bytes.NewBufferString(body))})
			require.Nil(t, newAPIError)
			require.Equal(t, &dto.Usage{}, usage)
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
		})
	}
}

func TestGeminiChatHandlerEmptyCandidatesPreservesOnlyReportedUsage(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		usageJSON   string
		wantUsage   dto.Usage
		wantMissing bool
	}{
		{name: "missing", usageJSON: "", wantMissing: true},
		{name: "reported", usageJSON: `,"usageMetadata":{"promptTokenCount":4,"totalTokenCount":4}`, wantUsage: dto.Usage{PromptTokens: 4, TotalTokens: 4, PromptTokensDetails: dto.InputTokenDetails{TextTokens: 4}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}
			body := `{"candidates":[]` + tt.usageJSON + `}`

			usage, newAPIError := GeminiChatHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, newAPIError)
			require.Equal(t, tt.wantUsage, *usage)
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			require.Equal(t, http.StatusInternalServerError, recorder.Code)
		})
	}
}

func TestGeminiStreamHandlerFailsClosedWithoutUsageMetadata(t *testing.T) {
	for _, tt := range []struct {
		name        string
		usageJSON   string
		wantMissing bool
	}{
		{name: "missing", usageJSON: "", wantMissing: true},
		{name: "null", usageJSON: `,"usageMetadata":null`, wantMissing: true},
		{name: "explicit zero", usageJSON: `,"usageMetadata":{}`, wantMissing: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}
			info.SetEstimatePromptTokens(20)
			chunk := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]` + tt.usageJSON + `}`

			usage, newAPIError := geminiStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader("data: " + chunk + "\n\ndata: [DONE]\n\n"))}, func(string, *dto.GeminiChatResponse) bool { return true })
			require.Nil(t, newAPIError)
			require.Equal(t, &dto.Usage{}, usage)
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
		})
	}
}

func TestGeminiTextGenerationHandlerFailsClosedWithoutUsageMetadata(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		usageJSON   string
		wantMissing bool
	}{
		{name: "missing", usageJSON: "", wantMissing: true},
		{name: "null", usageJSON: `,"usageMetadata":null`, wantMissing: true},
		{name: "explicit zero", usageJSON: `,"usageMetadata":{}`, wantMissing: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:generateContent", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}
			info.SetEstimatePromptTokens(20)
			body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]` + tt.usageJSON + `}`

			usage, newAPIError := GeminiTextGenerationHandler(c, info, &http.Response{Body: io.NopCloser(bytes.NewBufferString(body))})
			require.Nil(t, newAPIError)
			require.Equal(t, &dto.Usage{}, usage)
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
		})
	}
}
