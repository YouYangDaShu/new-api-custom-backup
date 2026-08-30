package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A blocked settlement can outlive the task lease. The replacement worker must
// observe the durable operation key and refrain from replaying the money step.
func TestTerminalBillingLeaseTakeoverDoesNotReplayBlockedSettlement(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	task := makeTask(64, 64, 100, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	task.BillingState = model.TaskBillingStatePending
	require.NoError(t, model.DB.Create(task).Error)

	originalLeaseSeconds := terminalBillingLeaseSeconds
	originalHeartbeat := terminalBillingLeaseHeartbeat
	t.Cleanup(func() {
		terminalBillingLeaseSeconds = originalLeaseSeconds
		terminalBillingLeaseHeartbeat = originalHeartbeat
		executeTaskSettlementForBilling = executeTaskSettlement
	})
	terminalBillingLeaseSeconds = 1
	terminalBillingLeaseHeartbeat = time.Hour

	workerOne := "blocked-worker"
	now := time.Now().Unix()
	claimed, won, err := model.ClaimTerminalTaskBilling(task.ID, workerOne, now, now+1)
	require.NoError(t, err)
	require.True(t, won)

	started := make(chan struct{})
	release := make(chan struct{})
	var settlementCalls atomic.Int32
	executeTaskSettlementForBilling = func(_ context.Context, task *model.Task) bool {
		settlementCalls.Add(1)
		close(started)
		<-release
		task.Quota = 100
		return true
	}

	oldDone := make(chan bool, 1)
	go func() { oldDone <- finishTerminalTaskBillingWithLease(ctx, claimed, workerOne) }()
	<-started
	// Advance both durable leases explicitly while the first worker remains
	// blocked; this keeps the takeover deterministic and does not rely on wall
	// clock sleeps.
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).
		Update("billing_lease_until", int64(1)).Error)
	require.NoError(t, model.DB.Model(&model.TaskBillingOperation{}).
		Where("operation_key = ?", taskBillingOperationKey(task)).Update("lease_until", int64(1)).Error)

	workerTwo := "replacement-worker"
	takeoverNow := time.Now().Unix()
	replacement, won, err := model.ClaimTerminalTaskBilling(task.ID, workerTwo, takeoverNow, takeoverNow+300)
	require.NoError(t, err)
	require.True(t, won)
	require.False(t, finishTerminalTaskBillingWithLease(ctx, replacement, workerTwo))

	close(release)
	require.False(t, <-oldDone)
	assert.Equal(t, int32(1), settlementCalls.Load())
	var op model.TaskBillingOperation
	require.NoError(t, model.DB.Where("operation_key = ?", taskBillingOperationKey(task)).First(&op).Error)
	assert.Equal(t, model.TaskBillingOperationManual, op.State)
	assert.Equal(t, model.TaskBillingStateManual, getTaskBillingState(t, task.ID))
}

// backdateTaskUpdatedAt moves a task's updated_at past the recovery sweep's
// grace window so GetTerminalTasksWithPendingBilling can pick it up.
func backdateTaskUpdatedAt(t *testing.T, taskID int64) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", taskID).
		UpdateColumn("updated_at", time.Now().Unix()-120).Error)
}

func getTaskBillingState(t *testing.T, taskID int64) string {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.First(&task, taskID).Error)
	return task.BillingState
}

// A successful terminal transition whose settlement fails after the CAS must
// leave a persisted "pending" marker, and the recovery sweep must later apply
// the same settlement exactly once from the persisted plan.
func TestFinalize_SettleFailureIsPersistedAndRecoveredOnce(t *testing.T) {
	truncate(t)
	saveLotteryTestConfig(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 60, 60, 60
	const initQuota, preConsumed, actualQuota = 10000, 2000, 5000
	const tokenRemain = 9000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recover-settle", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	fromStatus := task.Status
	task.Status = model.TaskStatus(model.TaskStatusSuccess)
	task.Progress = "100%"
	task.FinishTime = 9999

	restore := hideTable(t, "tokens")
	// CAS wins, but the token step of the settlement fails: the funding charge
	// is compensated and the task must not report completed billing.
	require.True(t, FinalizeTerminalTaskBilling(ctx, &mockAdaptor{adjustReturn: actualQuota},
		task, &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}, fromStatus))

	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, model.TaskBillingStatePending, getTaskBillingState(t, task.ID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, persisted.Status)
	assert.Equal(t, actualQuota, persisted.PrivateData.PendingSettleQuota)

	// The recovery sweep replays the persisted plan after the failure clears.
	restore()
	backdateTaskUpdatedAt(t, task.ID)
	sweepPendingBillingTasks(ctx)

	assert.Equal(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain-(actualQuota-preConsumed), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, model.TaskBillingStateDone, getTaskBillingState(t, task.ID))
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, actualQuota, persisted.Quota)
	// Lottery accrues the final settled quota exactly once (threshold 20).
	progress := getLotteryProgress(t, userID)
	assert.Equal(t, actualQuota/20, progress.AvailableDraws)

	// A repeated sweep finds nothing and moves no money.
	backdateTaskUpdatedAt(t, task.ID)
	sweepPendingBillingTasks(ctx)
	assert.Equal(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))
	progress = getLotteryProgress(t, userID)
	assert.Equal(t, actualQuota/20, progress.AvailableDraws)
}

// A failed terminal transition whose refund fails after the CAS must leave a
// persisted "pending" marker, and the recovery sweep must refund exactly once.
func TestFinalize_RefundFailureIsPersistedAndRecoveredOnce(t *testing.T) {
	truncate(t)
	saveLotteryTestConfig(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 61, 61, 61
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recover-refund", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	fromStatus := task.Status
	task.Status = model.TaskStatus(model.TaskStatusFailure)
	task.Progress = "100%"
	task.FinishTime = 9999
	task.FailReason = "upstream error"

	restore := hideTable(t, "tokens")
	require.True(t, FinalizeTerminalTaskBilling(ctx, &mockAdaptor{}, task,
		&relaycommon.TaskInfo{Status: model.TaskStatusFailure}, fromStatus))

	// Wallet compensated back to baseline, task terminal but billing pending.
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, model.TaskBillingStatePending, getTaskBillingState(t, task.ID))

	restore()
	backdateTaskUpdatedAt(t, task.ID)
	sweepPendingBillingTasks(ctx)

	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, model.TaskBillingStateDone, getTaskBillingState(t, task.ID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, 0, persisted.Quota)
	// Refunded failures never accrue lottery progress.
	progress := getLotteryProgress(t, userID)
	assert.Zero(t, progress.AvailableDraws)
	assert.Zero(t, progress.Accumulated)

	// A repeated sweep must not refund twice.
	backdateTaskUpdatedAt(t, task.ID)
	sweepPendingBillingTasks(ctx)
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
}

// The happy path marks billing done in the database, so the recovery sweep
// only ever sees genuinely unfinished work.
func TestFinalize_CompletedBillingIsMarkedDone(t *testing.T) {
	truncate(t)
	saveLotteryTestConfig(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 62, 62, 62
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-recover-done", 8000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, 3000, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	fromStatus := task.Status
	task.Status = model.TaskStatus(model.TaskStatusSuccess)
	task.Progress = "100%"
	require.True(t, FinalizeTerminalTaskBilling(ctx, &mockAdaptor{}, task,
		&relaycommon.TaskInfo{Status: model.TaskStatusSuccess}, fromStatus))

	assert.Equal(t, model.TaskBillingStateDone, getTaskBillingState(t, task.ID))
	backdateTaskUpdatedAt(t, task.ID)
	assert.Empty(t, model.GetTerminalTasksWithPendingBilling(time.Now().Unix(), 100))
}

func TestFinalize_LotteryFailureRecoversWithoutRepeatingFunds(t *testing.T) {
	truncate(t)
	saveLotteryTestConfig(t)
	ctx := context.Background()
	const userID, channelID = 64, 64
	seedUser(t, userID, 10000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 1000, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusInProgress
	require.NoError(t, model.DB.Create(task).Error)

	fromStatus := task.Status
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	var settlementCalls atomic.Int32
	executeTaskSettlementForBilling = func(ctx context.Context, task *model.Task) bool {
		settlementCalls.Add(1)
		return executeTaskSettlement(ctx, task)
	}
	t.Cleanup(func() { executeTaskSettlementForBilling = executeTaskSettlement })
	restore := hideTable(t, "lottery_consumption_events")
	require.True(t, FinalizeTerminalTaskBilling(ctx, &mockAdaptor{adjustReturn: 2000}, task,
		&relaycommon.TaskInfo{Status: model.TaskStatusSuccess}, fromStatus))
	restore()

	assert.Equal(t, model.TaskBillingStatePending, getTaskBillingState(t, task.ID))
	assert.Equal(t, 9000, getUserQuota(t, userID))
	var operation model.TaskBillingOperation
	require.NoError(t, model.DB.Where("operation_key = ?", taskBillingOperationKey(task)).First(&operation).Error)
	assert.Equal(t, model.TaskBillingOperationDone, operation.State)
	assert.Equal(t, int32(1), settlementCalls.Load())

	backdateTaskUpdatedAt(t, task.ID)
	sweepPendingBillingTasks(ctx)
	assert.Equal(t, model.TaskBillingStateDone, getTaskBillingState(t, task.ID))
	assert.Equal(t, 9000, getUserQuota(t, userID))
	assert.Equal(t, int32(1), settlementCalls.Load())
	progress := getLotteryProgress(t, userID)
	assert.Equal(t, 100, progress.AvailableDraws)
	var events int64
	require.NoError(t, model.DB.Model(&model.LotteryConsumptionEvent{}).Where("event_key = ?", "task:"+task.TaskID).Count(&events).Error)
	assert.EqualValues(t, 1, events)
}

// Tasks downgraded to "manual" (failed compensation) are excluded from the
// automatic retry query: an automatic retry could double-apply a delta.
func TestPendingBillingSweep_SkipsManualTasks(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 63, 63
	seedUser(t, userID, 10000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, 2500, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusFailure)
	task.BillingState = model.TaskBillingStatePending
	require.NoError(t, model.DB.Create(task).Error)
	backdateTaskUpdatedAt(t, task.ID)
	require.Len(t, model.GetTerminalTasksWithPendingBilling(time.Now().Unix(), 100), 1)

	markTaskBillingManual(ctx, task)

	assert.Equal(t, model.TaskBillingStateManual, getTaskBillingState(t, task.ID))
	assert.Empty(t, model.GetTerminalTasksWithPendingBilling(time.Now().Unix(), 100))
	// The sweep therefore moves no money for it.
	sweepPendingBillingTasks(ctx)
	assert.Equal(t, 10000, getUserQuota(t, userID))
}
