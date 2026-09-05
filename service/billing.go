package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	return settleBilling(ctx, relayInfo, actualQuota, false)
}

func settleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int, allowMissingUsage bool) error {
	if ctx != nil {
		ctx.Set("billing_settled", false)
	}
	if !allowMissingUsage && ctx != nil && common.GetContextKeyBool(ctx, constant.ContextKeyMissingUpstreamUsage) {
		return fmt.Errorf("missing usage for pre-consumed billing; automatic refund required")
	}
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return err
		}
		if ctx != nil {
			ctx.Set("billing_settled", true)
			status := model.BillingStatusCharged
			if actualQuota == 0 {
				status = model.BillingStatusNoCharge
			}
			common.SetContextKey(ctx, constant.ContextKeyBillingFinalStatus, status)
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		err := PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true)
		if err == nil && ctx != nil {
			ctx.Set("billing_settled", true)
		}
		return err
	}
	if ctx != nil {
		ctx.Set("billing_settled", true)
		status := model.BillingStatusCharged
		if actualQuota == 0 {
			status = model.BillingStatusNoCharge
		}
		common.SetContextKey(ctx, constant.ContextKeyBillingFinalStatus, status)
	}
	return nil
}

// FinalizeMissingUsageNoCharge synchronously releases every pre-consumed
// quota component and durably closes the request as no-charge. This is the
// product-policy terminal path for a response that never supplied trustworthy
// provider usage; it deliberately does not use the asynchronous Refund path.
func FinalizeMissingUsageNoCharge(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) error {
	return FinalizeUnsettledNoCharge(ctx, relayInfo, "missing_auto_refund")
}

// FinalizeUnsettledNoCharge synchronously releases a pre-consume for a
// definitively uncharged request. Ambiguous economic outcomes must not call it.
func FinalizeUnsettledNoCharge(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usageSource string) error {
	if ctx == nil || relayInfo == nil || relayInfo.Billing == nil {
		return fmt.Errorf("missing billing session for automatic no-charge finalization")
	}
	if usageSource == "" {
		usageSource = "failure_auto_refund"
	}
	if session, ok := relayInfo.Billing.(*BillingSession); ok {
		unlock := session.lockFinalization()
		defer unlock()
	}
	if ctx.GetBool("billing_settled") && common.GetContextKeyString(ctx, constant.ContextKeyBillingFinalStatus) == model.BillingStatusNoCharge {
		return nil
	}

	claimed, persistent := model.ClaimRequestBilling(ctx, 0, relayInfo.FinalPreConsumedQuota, usageSource)
	if persistent && !claimed {
		operation, lookupErr := model.GetRequestBillingOperation(ctx.GetString(common.RequestIdKey))
		if lookupErr == nil && operation.Status == model.BillingOperationDone && operation.Result == model.BillingStatusNoCharge {
			ctx.Set("billing_settled", true)
			common.SetContextKey(ctx, constant.ContextKeyBillingFinalStatus, model.BillingStatusNoCharge)
			return nil
		}
		err := fmt.Errorf("durable automatic refund claim was not acquired")
		common.SetContextKey(ctx, constant.ContextKeyBillingPending, true)
		common.SetContextKey(ctx, constant.ContextKeyBillingPendingReason, "settlement_failed")
		return err
	}
	if !persistent && !relayInfo.BillingFinalized.CompareAndSwap(false, true) {
		if ctx.GetBool("billing_settled") && common.GetContextKeyString(ctx, constant.ContextKeyBillingFinalStatus) == model.BillingStatusNoCharge {
			return nil
		}
		err := fmt.Errorf("automatic refund finalizer was already claimed")
		markBillingSettlementFailed(ctx, relayInfo, err)
		return err
	}

	var settleErr error
	for attempt := 0; attempt < 3; attempt++ {
		settleErr = settleBilling(ctx, relayInfo, 0, true)
		if settleErr == nil {
			break
		}
		if attempt < 2 {
			time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
		}
	}
	if settleErr != nil {
		markBillingSettlementFailed(ctx, relayInfo, settleErr)
		return settleErr
	}
	if persistent && !model.CompleteRequestBilling(ctx, model.BillingStatusNoCharge) {
		err := fmt.Errorf("automatic refund completed but durable no-charge completion failed")
		markBillingSettlementFailed(ctx, relayInfo, err)
		return err
	}
	return nil
}

func markBillingSettlementFailed(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, err error) {
	common.SetContextKey(ctx, constant.ContextKeyBillingPending, true)
	common.SetContextKey(ctx, constant.ContextKeyBillingPendingReason, "settlement_failed")
	if model.FailRequestBilling(ctx, err) {
		return
	}
	preConsumedQuota := 0
	if relayInfo != nil {
		preConsumedQuota = relayInfo.FinalPreConsumedQuota
	}
	model.MarkRequestBillingPending(ctx, preConsumedQuota, "settlement_failed")
}
