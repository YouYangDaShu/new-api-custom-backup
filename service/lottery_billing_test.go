package service

import (
	"errors"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type lotteryBillingSettlerStub struct {
	pre int
	err error
}

func (s *lotteryBillingSettlerStub) Settle(int) error         { return s.err }
func (s *lotteryBillingSettlerStub) Refund(*gin.Context)      {}
func (s *lotteryBillingSettlerStub) NeedsRefund() bool        { return false }
func (s *lotteryBillingSettlerStub) GetPreConsumedQuota() int { return s.pre }
func (s *lotteryBillingSettlerStub) Reserve(int) error        { return nil }

func lotteryBillingTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	return ctx
}

func TestSettleBillingMarksLotteryOnlyAfterSuccessfulWalletSettlement(t *testing.T) {
	ctx := lotteryBillingTestContext()
	info := &relaycommon.RelayInfo{Billing: &lotteryBillingSettlerStub{}, BillingSource: BillingSourceWallet}
	require.NoError(t, SettleBilling(ctx, info, 25))
	require.True(t, ctx.GetBool("billing_settled"))
}

func TestSettleBillingDoesNotMarkLotteryOnFailure(t *testing.T) {
	ctx := lotteryBillingTestContext()
	ctx.Set("billing_settled", true)
	info := &relaycommon.RelayInfo{Billing: &lotteryBillingSettlerStub{err: errors.New("settle failed")}, BillingSource: BillingSourceWallet}
	require.Error(t, SettleBilling(ctx, info, 25))
	require.False(t, ctx.GetBool("billing_settled"))
}

func TestSettleBillingDoesNotMarkLotteryForSubscriptionOrZeroQuota(t *testing.T) {
	ctx := lotteryBillingTestContext()
	info := &relaycommon.RelayInfo{Billing: &lotteryBillingSettlerStub{}, BillingSource: BillingSourceSubscription}
	require.NoError(t, SettleBilling(ctx, info, 25))
	require.False(t, ctx.GetBool("billing_settled"))

	ctx = lotteryBillingTestContext()
	info = &relaycommon.RelayInfo{Billing: &lotteryBillingSettlerStub{}, BillingSource: BillingSourceWallet}
	require.NoError(t, SettleBilling(ctx, info, 0))
	require.False(t, ctx.GetBool("billing_settled"))
}
