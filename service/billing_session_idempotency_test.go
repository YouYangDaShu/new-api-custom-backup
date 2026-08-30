package service

import (
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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
