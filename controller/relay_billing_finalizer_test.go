package controller

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	claudechannel "github.com/QuantumNous/new-api/relay/channel/claude"
	openaichannel "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type finalizerBilling struct {
	mu        sync.Mutex
	settles   int
	settleErr error
	refunds   int
	refunded  bool
}

func (b *finalizerBilling) Settle(int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.settles++
	return b.settleErr
}
func (b *finalizerBilling) NeedsRefund() bool        { return true }
func (b *finalizerBilling) GetPreConsumedQuota() int { return 10 }
func (b *finalizerBilling) Reserve(int) error        { return nil }
func (b *finalizerBilling) Refund(*gin.Context) {
	b.mu.Lock()
	if !b.refunded {
		b.refunds++
		b.refunded = true
	}
	b.mu.Unlock()
}

func TestFinalizeFailedRelayBilling(t *testing.T) {
	tests := []struct {
		name        string
		missing     bool
		delivered   bool
		settled     bool
		wantPending bool
		wantSettles int
		wantRefunds int
	}{
		{name: "missing usage without payload refunds", missing: true, wantSettles: 1},
		{name: "missing usage after payload refunds", missing: true, delivered: true, wantSettles: 1},
		{name: "plain failure refunds", wantSettles: 1},
		{name: "settled on error does not refund", settled: true},
		{name: "missing after settlement does not become pending", missing: true, settled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			billing := &finalizerBilling{}
			info := &relaycommon.RelayInfo{Billing: billing, FinalPreConsumedQuota: 10}
			common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, tt.missing)
			common.SetContextKey(c, constant.ContextKeyModelPayloadDelivered, tt.delivered)
			c.Set("billing_settled", tt.settled)

			finalizeFailedRelayBilling(c, info)
			finalizeFailedRelayBilling(c, info)

			assert.Equal(t, tt.wantPending, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
			billing.mu.Lock()
			assert.Equal(t, tt.wantSettles, billing.settles)
			assert.Equal(t, tt.wantRefunds, billing.refunds)
			billing.mu.Unlock()
		})
	}
}

func TestShouldKeepBillingPendingOnlyForSettlementFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	common.SetContextKey(c, constant.ContextKeyBillingPending, true)
	assert.False(t, shouldKeepBillingPending(c))

	common.SetContextKey(c, constant.ContextKeyModelPayloadDelivered, true)
	assert.False(t, shouldKeepBillingPending(c))

	common.SetContextKey(c, constant.ContextKeyBillingPendingReason, "usage_without_completed_result")
	assert.False(t, shouldKeepBillingPending(c))

	common.SetContextKey(c, constant.ContextKeyBillingPendingReason, "settlement_failed")
	assert.True(t, shouldKeepBillingPending(c))
}

func assertSynchronousNoCharge(t *testing.T, c *gin.Context, billing *finalizerBilling) {
	t.Helper()
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	assert.True(t, c.GetBool("billing_settled"))
	assert.Equal(t, model.BillingStatusNoCharge, common.GetContextKeyString(c, constant.ContextKeyBillingFinalStatus))
	billing.mu.Lock()
	defer billing.mu.Unlock()
	assert.Equal(t, 1, billing.settles)
	assert.Zero(t, billing.refunds)
}

func TestSuccessfulRelayMissingUsageWithoutPayloadRefunds(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	billing := &finalizerBilling{}
	info := &relaycommon.RelayInfo{Billing: billing, FinalPreConsumedQuota: 10}
	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	common.SetContextKey(c, constant.ContextKeyBillingPending, true)

	assert.False(t, finalizeSuccessfulRelayBilling(c, info))
	assertSynchronousNoCharge(t, c, billing)
}

func TestSuccessfulRelayMissingUsageAfterPayloadRefunds(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	billing := &finalizerBilling{}
	info := &relaycommon.RelayInfo{Billing: billing, FinalPreConsumedQuota: 10}
	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	common.SetContextKey(c, constant.ContextKeyModelPayloadDelivered, true)

	assert.False(t, finalizeSuccessfulRelayBilling(c, info))
	assertSynchronousNoCharge(t, c, billing)
}

func TestPostWssMissingUsageIsRefundedByControllerFinalizer(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	billing := &finalizerBilling{}
	info := &relaycommon.RelayInfo{Billing: billing, FinalPreConsumedQuota: 10}

	service.PostWssConsumeQuota(c, info, "gpt-realtime-test", nil, "")
	finalizeFailedRelayBilling(c, info)

	assertSynchronousNoCharge(t, c, billing)
}

func TestAudioSettlementFailureRemainsPendingAndIsNotRefunded(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	billing := &finalizerBilling{settleErr: errors.New("audio settlement failed")}
	info := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 10,
		OriginModelName:       "gpt-audio-test",
		StartTime:             time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	usage := &dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}
	usage.PromptTokensDetails.TextTokens = 2
	usage.CompletionTokenDetails.AudioTokens = 3

	service.PostAudioConsumeQuota(c, info, usage, "")
	finalizeFailedRelayBilling(c, info)

	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	assert.False(t, c.GetBool("billing_settled"))
	assert.Equal(t, "pending", failedRelayBillingLogStatus(c))
	billing.mu.Lock()
	assert.Equal(t, 1, billing.settles)
	assert.Zero(t, billing.refunds)
	billing.mu.Unlock()
}

func TestFinalizeFailedRelayBillingWithoutSessionDoesNotCreatePending(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)

	finalizeFailedRelayBilling(c, &relaycommon.RelayInfo{FinalPreConsumedQuota: 10})

	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
}

func TestFinalizeFailedRelayBillingRefundsIncompleteResultWithoutSettlement(t *testing.T) {
	oldLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = oldLogDB })
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
	const requestID = "controller-pending-usage-present"
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	t.Cleanup(func() { model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{}) })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Set(common.RequestIdKey, requestID)
	c.Set("id", 99130)
	c.Set("token_name", "tok")
	c.Set("token_id", 7)
	c.Set("group", "default")
	c.Set("original_model", "image-model")
	model.StartRequestLog(c, "image-model", true, "default")
	common.SetContextKey(c, constant.ContextKeyBillingPending, true)
	common.SetContextKey(c, constant.ContextKeyBillingPendingReason, "usage_without_completed_result")
	info := &relaycommon.RelayInfo{Billing: &finalizerBilling{}, FinalPreConsumedQuota: 19}

	finalizeFailedRelayBilling(c, info)
	finalizeFailedRelayBilling(c, info)
	model.FinalizeRequestLog(c, model.FinalizeRequestLogParams{
		Success:    false,
		StatusCode: http.StatusBadGateway,
		Content:    "incomplete result",
		ForceType:  model.LogTypeError,
		Other:      map[string]interface{}{"billing_status": "no_charge"},
	})

	var row model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).First(&row).Error)
	assert.Equal(t, model.BillingStatusNoCharge, row.BillingStatus)
	assertSynchronousNoCharge(t, c, info.Billing.(*finalizerBilling))
	other, err := common.StrToMap(row.Other)
	require.NoError(t, err)
	assert.Equal(t, "no_charge", other["billing_status"])
}

func TestSettlementFailureRemainsPendingAndIsNotRefunded(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	billing := &finalizerBilling{settleErr: errors.New("settlement failed")}
	info := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 10,
		OriginModelName:       "gpt-test",
		StartTime:             time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}

	service.PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}, nil)
	finalizeFailedRelayBilling(c, info)

	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	assert.False(t, c.GetBool("billing_settled"))
	assert.Equal(t, "pending", failedRelayBillingLogStatus(c))
	billing.mu.Lock()
	assert.Equal(t, 1, billing.settles)
	assert.Zero(t, billing.refunds)
	billing.mu.Unlock()
}

func TestFailedRelayBillingLogStatus(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.Equal(t, "no_charge", failedRelayBillingLogStatus(c))
	common.SetContextKey(c, constant.ContextKeyBillingPending, true)
	assert.Equal(t, "pending", failedRelayBillingLogStatus(c))
	common.SetContextKey(c, constant.ContextKeyBillingPending, false)
	c.Set("billing_settled", true)
	assert.Equal(t, "charged", failedRelayBillingLogStatus(c))
}

func TestResetRelayAttemptBillingMarkersPreservesDurableState(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	common.SetContextKey(c, constant.ContextKeyUpstreamUsagePresent, true)
	common.SetContextKey(c, constant.ContextKeyErrorUsagePresent, true)
	common.SetContextKey(c, constant.ContextKeyModelPayloadDelivered, true)
	common.SetContextKey(c, constant.ContextKeyBillingPending, true)
	c.Set("billing_settled", true)

	resetRelayAttemptBillingMarkers(c)

	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyMissingUpstreamUsage))
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyUpstreamUsagePresent))
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyErrorUsagePresent))
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered))
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	assert.True(t, c.GetBool("billing_settled"))
}

func TestResetRelayAttemptBillingMarkersClearsStalePendingBeforeNextAttempt(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyBillingPending, true)
	common.SetContextKey(c, constant.ContextKeyBillingPendingReason, "usage_without_completed_result")

	resetRelayAttemptBillingMarkers(c)

	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyBillingPendingReason))
	assert.False(t, shouldKeepBillingPending(c))
}

func TestResponsesTerminalErrorStopsRetryButOrdinaryFailureCanRetry(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	terminal := types.NewOpenAIError(errors.New("upstream terminal"), types.ErrorCode("channel:upstream_terminal"), http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	assert.False(t, shouldRetry(c, terminal, 1))
	ordinary := types.NewOpenAIError(errors.New("connection failed"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	require.True(t, shouldRetry(c, ordinary, 1))
}

func TestHandlerEvidenceControlsOuterRetry(t *testing.T) {
	tests := []struct {
		name      string
		run       func(*gin.Context) *types.NewAPIError
		wantRetry bool
	}{
		{
			name: "responses malformed before delivery",
			run: func(c *gin.Context) *types.NewAPIError {
				resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {malformed}\n")), Header: make(http.Header)}
				info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
				_, err := openaichannel.OaiResponsesStreamHandler(c, info, resp)
				return err
			},
			wantRetry: true,
		},
		{
			name: "responses malformed after delivery",
			run: func(c *gin.Context) *types.NewAPIError {
				body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\ndata: {malformed}\n"
				resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
				info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
				_, err := openaichannel.OaiResponsesStreamHandler(c, info, resp)
				return err
			},
		},
		{
			name: "responses explicit terminal",
			run: func(c *gin.Context) *types.NewAPIError {
				body := `data: {"type":"response.failed","response":{"status":"failed"}}` + "\n"
				resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
				info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
				_, err := openaichannel.OaiResponsesStreamHandler(c, info, resp)
				return err
			},
		},
		{
			name: "claude malformed before delivery",
			run: func(c *gin.Context) *types.NewAPIError {
				resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {malformed}\n")), Header: make(http.Header)}
				info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
				_, err := claudechannel.ClaudeStreamHandler(c, resp, info)
				return err
			},
			wantRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			err := tt.run(c)
			require.Error(t, err)
			assert.Equal(t, tt.wantRetry, shouldRetry(c, err, 1))
		})
	}
}
