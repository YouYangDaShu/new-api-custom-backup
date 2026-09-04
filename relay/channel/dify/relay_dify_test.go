package dify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func TestDifyHandlerUsagePresence(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		metadata    string
		wantUsage   dto.Usage
		wantMissing bool
	}{
		{name: "missing metadata", metadata: "", wantMissing: true},
		{name: "missing usage", metadata: `,"metadata":{}`, wantMissing: true},
		{name: "null usage", metadata: `,"metadata":{"usage":null}`, wantMissing: true},
		{name: "explicit zero", metadata: `,"metadata":{"usage":{}}`},
		{name: "reported usage", metadata: `,"metadata":{"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`, wantUsage: dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			body := `{"conversation_id":"conversation","answer":"ok"` + tt.metadata + `}`

			usage, newAPIError := difyHandler(c, &relaycommon.RelayInfo{}, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, newAPIError)
			require.Equal(t, tt.wantUsage, *usage)
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
		})
	}
}

func TestDifyStreamHandlerUsagePresence(t *testing.T) {
	for _, tt := range []struct {
		name        string
		metadata    string
		wantUsage   dto.Usage
		wantMissing bool
	}{
		{name: "missing metadata", metadata: "", wantMissing: true},
		{name: "missing usage", metadata: `,"metadata":{}`, wantMissing: true},
		{name: "null usage", metadata: `,"metadata":{"usage":null}`, wantMissing: true},
		{name: "explicit zero", metadata: `,"metadata":{"usage":{}}`},
		{name: "reported usage", metadata: `,"metadata":{"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`, wantUsage: dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{DisablePing: true}
			body := `data: {"event":"message_end"` + tt.metadata + "}\n\n"

			usage, newAPIError := difyStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, newAPIError)
			require.Equal(t, tt.wantUsage, *usage)
			require.Equal(t, tt.wantMissing, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
			require.Contains(t, recorder.Body.String(), "[DONE]")
		})
	}
}

func TestDifyStreamHandlerErrorDoesNotSendDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{DisablePing: true}
	body := "data: {\"event\":\"error\",\"message\":\"upstream failed\"}\n\ndata: {\"event\":\"message_end\",\"metadata\":{\"usage\":{}}}\n\n"

	usage, newAPIError := difyStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	require.NotNil(t, newAPIError)
	require.Equal(t, &dto.Usage{}, usage)
	require.NotContains(t, recorder.Body.String(), "[DONE]")
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
}
