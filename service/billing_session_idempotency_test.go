package service

import (
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingFunding struct {
	mu      sync.Mutex
	deltas  []int
	refunds int
	done    chan struct{}
}

func testBillingContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c
}

func (f *recordingFunding) Source() string       { return BillingSourceWallet }
func (f *recordingFunding) PreConsume(int) error { return nil }
func (f *recordingFunding) Settle(delta int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deltas = append(f.deltas, delta)
	return nil
}
func (f *recordingFunding) Refund() error {
	f.mu.Lock()
	f.refunds++
	f.mu.Unlock()
	if f.done != nil {
		close(f.done)
	}
	return nil
}

func TestBillingSession_FinalOutcomesAreIdempotent(t *testing.T) {
	t.Run("success or timeout with usage settles final charge once", func(t *testing.T) {
		funding := &recordingFunding{}
		session := &BillingSession{
			relayInfo:        &relaycommon.RelayInfo{IsPlayground: true},
			funding:          funding,
			preConsumedQuota: 40,
		}
		require.NoError(t, session.Settle(75))
		require.NoError(t, session.Settle(75))
		session.Refund(nil)
		funding.mu.Lock()
		defer funding.mu.Unlock()
		assert.Equal(t, []int{35}, funding.deltas)
		assert.Zero(t, funding.refunds)
	})

	t.Run("failure refunds pre-consume once", func(t *testing.T) {
		funding := &recordingFunding{done: make(chan struct{})}
		session := &BillingSession{
			relayInfo:        &relaycommon.RelayInfo{IsPlayground: true},
			funding:          funding,
			preConsumedQuota: 40,
			tokenConsumed:    40,
		}
		session.Refund(testBillingContext())
		session.Refund(testBillingContext())
		select {
		case <-funding.done:
		case <-time.After(time.Second):
			t.Fatal("refund did not complete")
		}
		funding.mu.Lock()
		defer funding.mu.Unlock()
		assert.Equal(t, 1, funding.refunds)
		assert.Empty(t, funding.deltas)
	})
}

func TestBillingSession_SettleRetriesTokenAfterFundingCommit(t *testing.T) {
	user := &model.User{Username: "session-settle-user", Password: "password", Quota: 100, AffCode: "session-settle-aff"}
	require.NoError(t, model.DB.Create(user).Error)
	t.Cleanup(func() { model.DB.Delete(&model.User{}, user.Id) })
	funding := &WalletFunding{userId: user.Id, consumed: 10}
	const tokenID = 987654
	requestID := "session-token-retry"
	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{RequestId: requestID, UserId: user.Id, TokenId: tokenID},
		funding:          funding,
		preConsumedQuota: 10,
	}
	err := session.Settle(20)
	require.Error(t, err)
	var after model.User
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	assert.Equal(t, 90, after.Quota)
	assert.False(t, session.settled)
	assert.True(t, session.fundingSettled)

	token := &model.Token{Id: tokenID, UserId: user.Id, Key: "retry-token", RemainQuota: 100, UsedQuota: 0}
	require.NoError(t, model.DB.Create(token).Error)
	require.NoError(t, session.Settle(20))
	require.NoError(t, session.Settle(20))
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	assert.Equal(t, 90, after.Quota)
	var gotToken model.Token
	require.NoError(t, model.DB.First(&gotToken, tokenID).Error)
	assert.Equal(t, 90, gotToken.RemainQuota)
	assert.Equal(t, 10, gotToken.UsedQuota)
}

func TestBillingSession_RefundChildOperationsAreIdempotent(t *testing.T) {
	user := &model.User{Username: "session-refund-user", Password: "password", Quota: 90, AffCode: "session-refund-aff"}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{Id: 987655, UserId: user.Id, Key: "refund-token", RemainQuota: 90, UsedQuota: 10}
	require.NoError(t, model.DB.Create(token).Error)
	t.Cleanup(func() {
		model.DB.Delete(&model.User{}, user.Id)
		model.DB.Delete(&model.Token{}, token.Id)
	})
	requestID := "session-refund-idempotent"
	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{RequestId: requestID, UserId: user.Id, TokenId: token.Id},
		funding:          &WalletFunding{userId: user.Id, consumed: 10},
		tokenConsumed:    10,
		preConsumedQuota: 10,
	}
	session.Refund(testBillingContext())
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var gotUser model.User
		var gotToken model.Token
		if model.DB.First(&gotUser, user.Id).Error == nil && model.DB.First(&gotToken, token.Id).Error == nil && gotUser.Quota == 100 && gotToken.RemainQuota == 100 && gotToken.UsedQuota == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var gotUser model.User
	var gotToken model.Token
	require.NoError(t, model.DB.First(&gotUser, user.Id).Error)
	require.NoError(t, model.DB.First(&gotToken, token.Id).Error)
	assert.Equal(t, 100, gotUser.Quota)
	assert.Equal(t, 100, gotToken.RemainQuota)
	assert.Equal(t, 0, gotToken.UsedQuota)
	session.Refund(testBillingContext())
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, model.DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 100, gotUser.Quota)
	op, err := model.GetEconomicBillingOperation(requestID + ":refund:funding")
	require.NoError(t, err)
	assert.Equal(t, "refund", op.OperationType)
}
