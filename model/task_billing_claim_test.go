package model

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimTerminalTaskBilling_OnlyOneWorkerWins(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:       "task_billing_claim_race",
		Status:       TaskStatusSuccess,
		BillingState: TaskBillingStatePending,
		UpdatedAt:    time.Now().Unix() - 120,
	}
	insertTask(t, task)

	const workers = 8
	wins := make([]bool, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	now := time.Now().Unix()
	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wg.Done()
			_, won, err := ClaimTerminalTaskBilling(task.ID, "worker-race-"+string(rune('a'+index)), now, now+300)
			if err == nil {
				wins[index] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, won := range wins {
		if won {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one recovery worker should claim a pending task")
}

func TestClaimTerminalTaskBilling_ReclaimsExpiredLease(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:            "task_billing_claim_expired",
		Status:            TaskStatusFailure,
		BillingState:      TaskBillingStatePending,
		BillingLeaseOwner: "crashed-worker",
		BillingLeaseUntil: time.Now().Unix() - 1,
	}
	insertTask(t, task)

	now := time.Now().Unix()
	claimed, won, err := ClaimTerminalTaskBilling(task.ID, "replacement-worker", now, now+300)
	require.NoError(t, err)
	require.True(t, won)
	require.NotNil(t, claimed)
	assert.Equal(t, "replacement-worker", claimed.BillingLeaseOwner)

	completed, err := CompleteTerminalTaskBilling(task.ID, "crashed-worker")
	require.NoError(t, err)
	assert.False(t, completed, "a stale worker must not complete a reclaimed lease")
	completed, err = CompleteTerminalTaskBilling(task.ID, "replacement-worker")
	require.NoError(t, err)
	assert.True(t, completed)

	var persisted Task
	require.NoError(t, DB.First(&persisted, task.ID).Error)
	assert.Equal(t, TaskBillingStateDone, persisted.BillingState)
	assert.Empty(t, persisted.BillingLeaseOwner)
}
