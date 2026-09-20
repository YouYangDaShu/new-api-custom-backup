package common

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamResponseModelAuditStrictTruthTable(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		sent      string
		returned  string
		mismatch  bool
	}{
		{name: "A to A to B", requested: "A", sent: "A", returned: "B", mismatch: true},
		{name: "A to B to B", requested: "A", sent: "B", returned: "B", mismatch: false},
		{name: "A to B to A", requested: "A", sent: "B", returned: "A", mismatch: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var audit UpstreamResponseModelAudit
			audit.SetSentModel(test.sent)
			audit.Observe(test.returned, true)
			observation := audit.Observation(test.requested)
			require.NotNil(t, observation)
			assert.Equal(t, test.requested, observation.RequestedModel)
			assert.Equal(t, test.sent, observation.UpstreamModel)
			assert.Equal(t, test.returned, observation.ReturnedModel)
			assert.Equal(t, test.mismatch, observation.Mismatch())
		})
	}
}

func TestUpstreamResponseModelAuditProtocols(t *testing.T) {
	tests := []struct {
		name string
		act  func(*UpstreamResponseModelAudit)
		want string
	}{
		{"openai chat", func(a *UpstreamResponseModelAudit) { a.ObserveOpenAI([]byte(`{"model":"gpt-5.6-sol"}`), "") }, "gpt-5.6-sol"},
		{"openai responses", func(a *UpstreamResponseModelAudit) {
			a.ObserveOpenAI([]byte(`{"response":{"model":"gpt-5.6-luna"}}`), "response.completed")
		}, "gpt-5.6-luna"},
		{"claude", func(a *UpstreamResponseModelAudit) { a.ObserveClaude([]byte(`{"message":{"model":"claude-opus-5"}}`)) }, "claude-opus-5"},
		{"gemini native", func(a *UpstreamResponseModelAudit) { a.ObserveGemini([]byte(`{"modelVersion":"gemini-3-pro"}`)) }, "gemini-3-pro"},
		{"gemini wrapper", func(a *UpstreamResponseModelAudit) {
			a.ObserveGemini([]byte(`{"response":{"modelVersion":"gemini-3-flash"}}`))
		}, "gemini-3-flash"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var audit UpstreamResponseModelAudit
			test.act(&audit)
			assert.Equal(t, test.want, audit.ResponseModel())
		})
	}
}

func TestUpstreamResponseModelAuditTerminalWins(t *testing.T) {
	var audit UpstreamResponseModelAudit
	audit.ObserveOpenAI([]byte(`{"response":{"model":"gpt-5.6-sol"}}`), "response.created")
	audit.ObserveOpenAI([]byte(`{"response":{"model":"gpt-5.6-luna"}}`), "response.completed")
	assert.Equal(t, "gpt-5.6-luna", audit.ResponseModel())
	assert.True(t, audit.Conflict)
}

func TestUpstreamResponseModelAuditResetPerAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	info := &RelayInfo{}
	info.UpstreamResponseModelAudit.SetSentModel("attempt-one")
	info.UpstreamResponseModelAudit.Observe("wrong-model", false)

	info.InitChannelMeta(ctx)

	assert.Empty(t, info.UpstreamResponseModelAudit.SentModel)
	assert.Empty(t, info.UpstreamResponseModelAudit.ResponseModel())
	assert.False(t, info.UpstreamResponseModelAudit.Conflict)
}

func TestUpstreamResponseModelAuditRejectsMalformedAndBoundsValue(t *testing.T) {
	var audit UpstreamResponseModelAudit
	audit.ObserveOpenAI([]byte(`{"model":"bad"`), "")
	assert.Empty(t, audit.ResponseModel())
	audit.Observe("  "+strings.Repeat("模", upstreamResponseModelMaxLength+1)+"  ", false)
	assert.Len(t, []rune(audit.ResponseModel()), upstreamResponseModelMaxLength)
}
