package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type settleRecordingBilling struct {
	actual  []int
	err     error
	refunds int
}

func (b *settleRecordingBilling) Settle(actual int) error {
	b.actual = append(b.actual, actual)
	return b.err
}
func (b *settleRecordingBilling) Refund(*gin.Context)      { b.refunds++ }
func (b *settleRecordingBilling) NeedsRefund() bool        { return true }
func (b *settleRecordingBilling) GetPreConsumedQuota() int { return 10 }
func (b *settleRecordingBilling) Reserve(int) error        { return nil }

func TestPostTextConsumeQuotaPersistsSettlementFailure(t *testing.T) {
	const requestID = "fix4-text-settlement-failure"
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.BillingOperation{}))
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	t.Cleanup(func() {
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	})

	c := newBillingLifecycleTestContext(t, requestID, 900001)
	model.StartRequestLog(c, "gpt-test", false, "default")
	billing := &settleRecordingBilling{err: errors.New("forced settlement failure")}
	info := newTextBillingTestRelayInfo(billing)

	PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}, nil)

	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	require.False(t, c.GetBool("billing_settled"))
	require.Equal(t, []int{5}, billing.actual)
	var row model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).First(&row).Error)
	require.Equal(t, model.BillingStatusSettlementFail, row.BillingStatus)
	other, err := common.StrToMap(row.Other)
	require.NoError(t, err)
	require.Equal(t, "pending", other["billing_status"])
	require.NotEqual(t, "no_charge", other["billing_status"])
}

func TestPostTextConsumeQuotaPersistsUnacquiredClaimAsPending(t *testing.T) {
	const requestID = "fix4-text-claim-not-acquired"
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.BillingOperation{}))
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	t.Cleanup(func() {
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	})

	c := newBillingLifecycleTestContext(t, requestID, 900002)
	model.StartRequestLog(c, "gpt-test", false, "default")
	claimed, err := model.ClaimBillingOperation(requestID, "settlement", 1)
	require.NoError(t, err)
	require.True(t, claimed)
	billing := &settleRecordingBilling{}
	info := newTextBillingTestRelayInfo(billing)

	PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}, nil)

	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	require.Empty(t, billing.actual)
	var row model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).First(&row).Error)
	require.Equal(t, model.BillingStatusPending, row.BillingStatus)
	require.Equal(t, "settlement_failed", row.UsageSource)
}

func TestPostTextConsumeQuotaRetryMissingThenTrustedUsageSettlesOnce(t *testing.T) {
	const requestID = "fix4-text-retry-missing-success"
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.BillingOperation{}))
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	t.Cleanup(func() {
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	})

	c := newBillingLifecycleTestContext(t, requestID, 900003)
	require.NotNil(t, model.DB)
	seedUser(t, 900003, 100000)
	seedChannel(t, 900003)
	t.Cleanup(func() {
		model.DB.Where("id = ?", 900003).Delete(&model.User{})
		model.DB.Where("id = ?", 900003).Delete(&model.Channel{})
	})
	model.StartRequestLog(c, "gpt-test", false, "default")
	billing := &settleRecordingBilling{}
	info := newTextBillingTestRelayInfo(billing)
	require.NotNil(t, info)
	info.UserId = 900003
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: 900003}

	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	PostTextConsumeQuota(c, info, nil, nil)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))

	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, false)
	common.SetContextKey(c, constant.ContextKeyUpstreamUsagePresent, true)
	PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}, nil)

	require.Equal(t, []int{5}, billing.actual)
	require.True(t, c.GetBool("billing_settled"))
	var row model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).First(&row).Error)
	require.Equal(t, model.LogTypeConsume, row.Type)
	require.Equal(t, model.BillingStatusCharged, row.BillingStatus)
	other, err := common.StrToMap(row.Other)
	require.NoError(t, err)
	require.Equal(t, model.BillingStatusCharged, other["billing_status"])
	var operation model.BillingOperation
	require.NoError(t, model.LOG_DB.Where("request_id = ? AND kind = ? AND version = ?", requestID, "settlement", 1).First(&operation).Error)
	require.Equal(t, model.BillingOperationDone, operation.Status)
	require.Equal(t, "completed", operation.Phase)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
}

func TestPostTextConsumeQuotaStopsAfterDurableCompletionFailure(t *testing.T) {
	tests := []struct {
		name          string
		requestID     string
		triggerName   string
		triggerColumn string
		triggerWhen   string
	}{
		{
			name:          "processing update",
			requestID:     "fix4-service-completion-log-failure",
			triggerName:   "fail_service_completion_log",
			triggerColumn: "billing_status",
			triggerWhen:   "NEW.billing_status = 'charged'",
		},
		{
			name:          "operation metadata update",
			requestID:     "fix4-service-completion-metadata-failure",
			triggerName:   "fail_service_completion_metadata",
			triggerColumn: "phase",
			triggerWhen:   "NEW.phase = 'completed'",
		},
		{
			name:          "claimed to done update",
			requestID:     "fix4-service-completion-done-failure",
			triggerName:   "fail_service_completion_done",
			triggerColumn: "status",
			triggerWhen:   "NEW.status = 'done'",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, model.LOG_DB.AutoMigrate(&model.BillingOperation{}))
			userID := 900010 + i
			model.LOG_DB.Where("request_id = ?", tt.requestID).Delete(&model.BillingOperation{})
			model.LOG_DB.Where("request_id = ?", tt.requestID).Delete(&model.Log{})
			model.LOG_DB.Exec("DROP TRIGGER IF EXISTS " + tt.triggerName)
			seedBillingLifecycleAccount(t, userID)
			t.Cleanup(func() {
				model.LOG_DB.Exec("DROP TRIGGER IF EXISTS " + tt.triggerName)
				model.LOG_DB.Where("request_id = ?", tt.requestID).Delete(&model.BillingOperation{})
				model.LOG_DB.Where("request_id = ?", tt.requestID).Delete(&model.Log{})
			})

			c := newBillingLifecycleTestContext(t, tt.requestID, userID)
			model.StartRequestLog(c, "gpt-test", false, "default")
			table := "billing_operations"
			if tt.triggerColumn == "billing_status" {
				table = "logs"
			}
			triggerSQL := fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE OF %s ON %s
				WHEN NEW.request_id = '%s' AND %s
				BEGIN SELECT RAISE(ABORT, 'durable completion failed'); END`, tt.triggerName, tt.triggerColumn, table, tt.requestID, tt.triggerWhen)
			require.NoError(t, model.LOG_DB.Exec(triggerSQL).Error)

			billing := &settleRecordingBilling{}
			info := newTextBillingTestRelayInfo(billing)
			info.UserId = userID
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: userID}
			usage := &dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}

			PostTextConsumeQuota(c, info, usage, nil)
			PostTextConsumeQuota(c, info, usage, nil)

			assert.Equal(t, []int{5}, billing.actual)
			assert.Zero(t, billing.refunds)
			assert.True(t, c.GetBool("billing_settled"))
			assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))

			var row model.Log
			require.NoError(t, model.LOG_DB.Where("request_id = ?", tt.requestID).First(&row).Error)
			assert.Equal(t, model.LogTypeProcessing, row.Type)
			assert.Equal(t, model.BillingStatusSettlementFail, row.BillingStatus)
			var operation model.BillingOperation
			require.NoError(t, model.LOG_DB.Where("request_id = ? AND kind = ? AND version = ?", tt.requestID, "settlement", 1).First(&operation).Error)
			assert.Equal(t, model.BillingOperationUnknown, operation.Status)
			assert.Equal(t, "unknown", operation.Phase)
			assert.Equal(t, "unknown", operation.Result)
			assert.Equal(t, int64(5), operation.ActualQuota)
			assert.Equal(t, int64(10), operation.PreConsumedQuota)

			var user model.User
			require.NoError(t, model.DB.Where("id = ?", userID).First(&user).Error)
			assert.Zero(t, user.UsedQuota)
			assert.Zero(t, user.RequestCount)
			var channel model.Channel
			require.NoError(t, model.DB.Where("id = ?", userID).First(&channel).Error)
			assert.Zero(t, channel.UsedQuota)
		})
	}
}

func TestPostAudioConsumeQuotaStopsAfterDurableCompletionFailure(t *testing.T) {
	const requestID = "fix4-audio-completion-failure"
	const triggerName = "fail_audio_completion_metadata"
	const userID = 900020
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.BillingOperation{}))
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	model.LOG_DB.Exec("DROP TRIGGER IF EXISTS " + triggerName)
	seedBillingLifecycleAccount(t, userID)
	t.Cleanup(func() {
		model.LOG_DB.Exec("DROP TRIGGER IF EXISTS " + triggerName)
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	})

	c := newBillingLifecycleTestContext(t, requestID, userID)
	model.StartRequestLog(c, "gpt-test", false, "default")
	require.NoError(t, model.LOG_DB.Exec(`CREATE TRIGGER fail_audio_completion_metadata BEFORE UPDATE OF phase ON billing_operations
		WHEN NEW.request_id = 'fix4-audio-completion-failure' AND NEW.phase = 'completed'
		BEGIN SELECT RAISE(ABORT, 'audio durable completion failed'); END`).Error)
	billing := &settleRecordingBilling{}
	info := newTextBillingTestRelayInfo(billing)
	info.UserId = userID
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: userID}
	usage := &dto.Usage{
		PromptTokens:     2,
		CompletionTokens: 3,
		TotalTokens:      5,
		PromptTokensDetails: dto.InputTokenDetails{
			TextTokens: 2,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{
			TextTokens: 3,
		},
	}

	PostAudioConsumeQuota(c, info, usage, "")
	PostAudioConsumeQuota(c, info, usage, "")

	assert.Equal(t, []int{8}, billing.actual)
	assert.Zero(t, billing.refunds)
	assert.True(t, c.GetBool("billing_settled"))
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	var row model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).First(&row).Error)
	assert.Equal(t, model.LogTypeProcessing, row.Type)
	assert.Equal(t, model.BillingStatusSettlementFail, row.BillingStatus)
	var operation model.BillingOperation
	require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).First(&operation).Error)
	assert.Equal(t, model.BillingOperationUnknown, operation.Status)
	assert.Equal(t, int64(8), operation.ActualQuota)
	assert.Equal(t, int64(10), operation.PreConsumedQuota)
}

func TestPostTextConsumeQuotaWithoutPersistentLogDoesNotBecomePending(t *testing.T) {
	const requestID = "fix4-no-persistent-log"
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	t.Cleanup(func() {
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	})

	c := newBillingLifecycleTestContext(t, requestID, 900030)
	billing := &settleRecordingBilling{}
	info := newTextBillingTestRelayInfo(billing)
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	usage := &dto.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}

	PostTextConsumeQuota(c, info, usage, nil)
	PostTextConsumeQuota(c, info, usage, nil)

	assert.Equal(t, []int{5}, billing.actual)
	assert.Zero(t, billing.refunds)
	assert.True(t, c.GetBool("billing_settled"))
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.BillingOperation{}).Where("request_id = ?", requestID).Count(&count).Error)
	assert.Zero(t, count)
}

func seedBillingLifecycleAccount(t *testing.T, id int) {
	t.Helper()
	user := &model.User{
		Id:       id,
		Username: fmt.Sprintf("billing_%d", id),
		AffCode:  fmt.Sprintf("billing-aff-%d", id),
		Quota:    100000,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:     id,
		Name:   fmt.Sprintf("billing-channel-%d", id),
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
	}).Error)
	t.Cleanup(func() {
		model.DB.Unscoped().Where("id = ?", id).Delete(&model.User{})
		model.DB.Unscoped().Where("id = ?", id).Delete(&model.Channel{})
	})
}

func newTextBillingTestRelayInfo(billing relaycommon.BillingSettler) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
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
}

func newBillingLifecycleTestContext(t *testing.T, requestID string, userID int) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
	c.Set(common.RequestIdKey, requestID)
	c.Set("id", userID)
	return c
}

func TestSettleBillingUsesMissingMarkerInsteadOfZeroValue(t *testing.T) {
	t.Run("explicit zero settles", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		billing := &settleRecordingBilling{}
		err := SettleBilling(c, &relaycommon.RelayInfo{Billing: billing}, 0)
		require.NoError(t, err)
		assert.Equal(t, []int{0}, billing.actual)
	})

	t.Run("missing usage does not settle", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		billing := &settleRecordingBilling{}
		err := SettleBilling(c, &relaycommon.RelayInfo{Billing: billing}, 0)
		require.Error(t, err)
		assert.Empty(t, billing.actual)
	})
}

func TestFinalizeMissingUsageNoChargeFailureDoesNotPretendRefunded(t *testing.T) {
	const requestID = "fix4-missing-usage-refund-failure"
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.BillingOperation{}))
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	t.Cleanup(func() {
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	})

	c := newBillingLifecycleTestContext(t, requestID, 900041)
	model.StartRequestLog(c, "gpt-test", true, "default")
	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	billing := &settleRecordingBilling{err: errors.New("forced automatic refund failure")}
	info := newTextBillingTestRelayInfo(billing)

	err := FinalizeMissingUsageNoCharge(c, info)
	require.ErrorContains(t, err, "forced automatic refund failure")
	require.Equal(t, []int{0, 0, 0}, billing.actual)
	require.False(t, c.GetBool("billing_settled"))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyBillingPending))
	require.Equal(t, "settlement_failed", common.GetContextKeyString(c, constant.ContextKeyBillingPendingReason))
	var row model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).First(&row).Error)
	require.Equal(t, model.BillingStatusSettlementFail, row.BillingStatus)
	require.NotEqual(t, model.BillingStatusNoCharge, row.BillingStatus)
	var operation model.BillingOperation
	require.NoError(t, model.LOG_DB.Where("request_id = ? AND kind = ? AND version = ?", requestID, "settlement", 1).First(&operation).Error)
	require.Equal(t, model.BillingOperationUnknown, operation.Status)
}

func TestFinalizeMissingUsageNoChargeRestoresSubscriptionAndTokenBeforeReturn(t *testing.T) {
	const requestID = "fix4-missing-usage-subscription-refund"
	const tokenID = 987657
	user := &model.User{Username: "missing-sub-refund-user", Password: "password", Quota: 0, AffCode: "missing-sub-refund-aff"}
	require.NoError(t, model.DB.AutoMigrate(&model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}))
	require.NoError(t, model.DB.Create(user).Error)
	sub := &model.UserSubscription{UserId: user.Id, AmountTotal: 100, AmountUsed: 10, Status: "active"}
	require.NoError(t, model.DB.Create(sub).Error)
	record := &model.SubscriptionPreConsumeRecord{RequestId: requestID, UserId: user.Id, UserSubscriptionId: sub.Id, PreConsumed: 10, Status: "consumed"}
	require.NoError(t, model.DB.Create(record).Error)
	token := &model.Token{Id: tokenID, UserId: user.Id, Key: "missing-sub-refund-token", RemainQuota: 90, UsedQuota: 10}
	require.NoError(t, model.DB.Create(token).Error)
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.BillingOperation{}))
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	model.DB.Where("request_id = ?", requestID).Delete(&model.EconomicBillingOperation{})
	t.Cleanup(func() {
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
		model.DB.Where("request_id = ?", requestID).Delete(&model.EconomicBillingOperation{})
		model.DB.Delete(&model.Token{}, tokenID)
		model.DB.Delete(&model.SubscriptionPreConsumeRecord{}, record.Id)
		model.DB.Delete(&model.UserSubscription{}, sub.Id)
		model.DB.Delete(&model.User{}, user.Id)
	})

	c := newBillingLifecycleTestContext(t, requestID, user.Id)
	model.StartRequestLog(c, "gpt-test", true, "default")
	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	info := newTextBillingTestRelayInfo(nil)
	info.RequestId = requestID
	info.UserId = user.Id
	info.TokenId = tokenID
	info.SubscriptionId = sub.Id
	info.BillingSource = BillingSourceSubscription
	info.Billing = &BillingSession{
		relayInfo:        info,
		funding:          &SubscriptionFunding{requestId: requestID, userId: user.Id, subscriptionId: sub.Id, preConsumed: 10},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}

	require.NoError(t, FinalizeMissingUsageNoCharge(c, info))

	var gotSub model.UserSubscription
	require.NoError(t, model.DB.First(&gotSub, sub.Id).Error)
	require.Zero(t, gotSub.AmountUsed)
	var gotRecord model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&gotRecord).Error)
	require.Equal(t, "refunded", gotRecord.Status)
	var gotToken model.Token
	require.NoError(t, model.DB.First(&gotToken, tokenID).Error)
	require.Equal(t, 100, gotToken.RemainQuota)
	require.Zero(t, gotToken.UsedQuota)
	fundingOp, err := model.GetEconomicBillingOperation(requestID + ":settlement:funding")
	require.NoError(t, err)
	require.Equal(t, model.EconomicOperationDone, fundingOp.Status)
	require.Equal(t, "refund", fundingOp.OperationType)
}

func TestFinalizeMissingUsageNoChargeRestoresWalletAndTokenBeforeReturn(t *testing.T) {
	const requestID = "fix4-missing-usage-real-refund"
	const tokenID = 987656
	user := &model.User{Username: "missing-usage-refund-user", Password: "password", Quota: 90, AffCode: "missing-usage-refund-aff"}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{Id: tokenID, UserId: user.Id, Key: "missing-refund-token", RemainQuota: 90, UsedQuota: 10}
	require.NoError(t, model.DB.Create(token).Error)
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.BillingOperation{}))
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	model.DB.Where("request_id = ?", requestID).Delete(&model.EconomicBillingOperation{})
	t.Cleanup(func() {
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
		model.DB.Where("request_id = ?", requestID).Delete(&model.EconomicBillingOperation{})
		model.DB.Delete(&model.Token{}, tokenID)
		model.DB.Delete(&model.User{}, user.Id)
	})

	c := newBillingLifecycleTestContext(t, requestID, user.Id)
	model.StartRequestLog(c, "gpt-test", true, "default")
	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	info := newTextBillingTestRelayInfo(nil)
	info.RequestId = requestID
	info.UserId = user.Id
	info.TokenId = tokenID
	info.BillingSource = BillingSourceWallet
	info.Billing = &BillingSession{
		relayInfo:        info,
		funding:          &WalletFunding{userId: user.Id, consumed: 10},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}

	require.NoError(t, FinalizeMissingUsageNoCharge(c, info))

	var gotUser model.User
	require.NoError(t, model.DB.First(&gotUser, user.Id).Error)
	require.Equal(t, 100, gotUser.Quota)
	var gotToken model.Token
	require.NoError(t, model.DB.First(&gotToken, tokenID).Error)
	require.Equal(t, 100, gotToken.RemainQuota)
	require.Zero(t, gotToken.UsedQuota)
	for _, key := range []string{requestID + ":settlement:funding", requestID + ":settlement:token"} {
		op, err := model.GetEconomicBillingOperation(key)
		require.NoError(t, err)
		require.Equal(t, model.EconomicOperationDone, op.Status)
		require.Equal(t, "settlement", op.OperationType)
	}
	var operation model.BillingOperation
	require.NoError(t, model.LOG_DB.Where("request_id = ? AND kind = ? AND version = ?", requestID, "settlement", 1).First(&operation).Error)
	require.Equal(t, model.BillingOperationDone, operation.Status)
	require.Equal(t, model.BillingStatusNoCharge, operation.Result)
}

func TestFinalizeUnsettledNoChargeConcurrentCallersSettleOnce(t *testing.T) {
	const requestID = "fix4-concurrent-auto-refund"
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.BillingOperation{}))
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	t.Cleanup(func() {
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	})
	c := newBillingLifecycleTestContext(t, requestID, 900042)
	model.StartRequestLog(c, "gpt-test", true, "default")
	funding := &recordingFunding{}
	info := newTextBillingTestRelayInfo(nil)
	info.RequestId = requestID
	info.IsPlayground = true
	info.Billing = &BillingSession{relayInfo: info, funding: funding, preConsumedQuota: 10}

	const callers = 8
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- FinalizeUnsettledNoCharge(c, info, "failure_auto_refund")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	funding.mu.Lock()
	require.Equal(t, []int{-10}, funding.deltas)
	funding.mu.Unlock()
	var operation model.BillingOperation
	require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).First(&operation).Error)
	require.Equal(t, model.BillingOperationDone, operation.Status)
	require.Equal(t, model.BillingStatusNoCharge, operation.Result)
}

func TestFinalizeMissingUsageNoChargeIsSynchronousDurableAndIdempotent(t *testing.T) {
	const requestID = "fix4-missing-usage-auto-refund"
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.BillingOperation{}))
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
	model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	t.Cleanup(func() {
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.BillingOperation{})
		model.LOG_DB.Where("request_id = ?", requestID).Delete(&model.Log{})
	})

	c := newBillingLifecycleTestContext(t, requestID, 900040)
	model.StartRequestLog(c, "gpt-test", true, "default")
	common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	billing := &settleRecordingBilling{}
	info := newTextBillingTestRelayInfo(billing)

	require.NoError(t, FinalizeMissingUsageNoCharge(c, info))
	require.NoError(t, FinalizeMissingUsageNoCharge(c, info))

	require.Equal(t, []int{0}, billing.actual)
	require.Zero(t, billing.refunds)
	require.True(t, c.GetBool("billing_settled"))
	require.Equal(t, model.BillingStatusNoCharge, common.GetContextKeyString(c, constant.ContextKeyBillingFinalStatus))
	var row model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).First(&row).Error)
	require.Equal(t, model.BillingStatusNoCharge, row.BillingStatus)
	var operation model.BillingOperation
	require.NoError(t, model.LOG_DB.Where("request_id = ? AND kind = ? AND version = ?", requestID, "settlement", 1).First(&operation).Error)
	require.Equal(t, model.BillingOperationDone, operation.Status)
	require.Equal(t, model.BillingStatusNoCharge, operation.Result)
	require.Zero(t, operation.ActualQuota)
	require.Equal(t, int64(10), operation.PreConsumedQuota)
}
