package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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

func saveLotteryTestConfig(t *testing.T) {
	t.Helper()
	require.NoError(t, model.SaveLotteryConfig(model.LotteryConfig{
		Enabled:       true,
		Threshold:     20,
		Prizes:        []model.LotteryPrize{{ID: "thanks", Name: "Thanks", Weight: 1}},
		SchemaVersion: 2,
		Unit:          "raw_quota",
	}))
}

func getLotteryProgress(t *testing.T, userID int) *model.UserLotteryProgress {
	t.Helper()
	progress, err := model.GetLotteryProgress(userID)
	require.NoError(t, err)
	return progress
}

// ===========================================================================
// Midjourney: submit logs defer accrual; poll-time success accrues exactly once
// ===========================================================================

func TestMjSubmitLogDefersLotteryUntilTaskSuccess(t *testing.T) {
	truncate(t)
	saveLotteryTestConfig(t)

	const userID = 40
	seedUser(t, userID, 100000)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/mj/submit/imagine", nil)
	c.Set(common.RequestIdKey, "mj-submit-req-1")
	c.Set("billing_settled", true)

	other := GenerateMjOtherInfo(&relaycommon.RelayInfo{}, types.PriceData{})
	other["status_code"] = 200
	other["request_status"] = "completed"
	model.RecordConsumeLog(c, userID, model.RecordConsumeLogParams{Quota: 25, Other: other})

	// Submission must not accrue: the MJ charge is refunded in full when the
	// async task later fails.
	progress := getLotteryProgress(t, userID)
	assert.Zero(t, progress.Accumulated)
	assert.Zero(t, progress.AvailableDraws)

	// Poll-time success accrues exactly once, keyed by the MJ task id.
	mjTask := &model.Midjourney{UserId: userID, MjId: "mj-task-1", Quota: 25}
	require.NoError(t, RecordMjTaskLotteryConsumption(mjTask))
	require.NoError(t, RecordMjTaskLotteryConsumption(mjTask))
	progress = getLotteryProgress(t, userID)
	assert.Equal(t, 5, progress.Accumulated)
	assert.Equal(t, 1, progress.AvailableDraws)
}

func TestMjTaskLotteryConsumptionSkipsRefundedAndUnidentifiedTasks(t *testing.T) {
	truncate(t)
	saveLotteryTestConfig(t)

	const userID = 41
	require.NoError(t, RecordMjTaskLotteryConsumption(&model.Midjourney{UserId: userID, MjId: "", Quota: 25}))
	require.NoError(t, RecordMjTaskLotteryConsumption(&model.Midjourney{UserId: userID, MjId: "mj-task-2", Quota: 0}))
	progress := getLotteryProgress(t, userID)
	assert.Zero(t, progress.Accumulated)
	assert.Zero(t, progress.AvailableDraws)
}

// ===========================================================================
// FinalizeTerminalTaskBilling: shared by the pollers and the Gemini/Vertex
// realtime fetch path — settlement/refund must run exactly once under races.
// ===========================================================================

func TestFinalizeTerminalTaskSuccessSettlesAndAccruesOnce(t *testing.T) {
	truncate(t)
	saveLotteryTestConfig(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 42, 42, 42
	const initQuota, preConsumed, actualQuota = 10000, 5000, 3000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-finalize-success", 8000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	fromStatus := task.Status
	task.Status = model.TaskStatus(model.TaskStatusSuccess)
	task.Progress = "100%"
	task.FinishTime = 9999
	adaptor := &mockAdaptor{adjustReturn: actualQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	require.True(t, FinalizeTerminalTaskBilling(ctx, adaptor, task, taskResult, fromStatus))

	// Settlement refunded the over-charge and the final quota accrued lottery
	// progress (3000 / threshold 20 = 150 draws).
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, actualQuota, task.Quota)
	progress := getLotteryProgress(t, userID)
	assert.Equal(t, 150, progress.AvailableDraws)
	assert.Zero(t, progress.Accumulated)

	// A racing finalize from the stale status (e.g. the poller after a realtime
	// fetch already settled) loses the CAS: no double settlement or accrual.
	require.False(t, FinalizeTerminalTaskBilling(ctx, adaptor, task, taskResult, fromStatus))
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	progress = getLotteryProgress(t, userID)
	assert.Equal(t, 150, progress.AvailableDraws)
}

func TestFinalizeTerminalTaskFailureRefundsOnce(t *testing.T) {
	truncate(t)
	saveLotteryTestConfig(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 43, 43, 43
	const initQuota, preConsumed = 10000, 4000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-finalize-failure", 6000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	fromStatus := task.Status
	task.Status = model.TaskStatus(model.TaskStatusFailure)
	task.Progress = "100%"
	task.FailReason = "upstream error"
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "upstream error"}

	require.True(t, FinalizeTerminalTaskBilling(ctx, &mockAdaptor{}, task, taskResult, fromStatus))
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Failed tasks never accrue lottery progress.
	progress := getLotteryProgress(t, userID)
	assert.Zero(t, progress.Accumulated)
	assert.Zero(t, progress.AvailableDraws)

	// A second finalize from the stale status must not refund again.
	require.False(t, FinalizeTerminalTaskBilling(ctx, &mockAdaptor{}, task, taskResult, fromStatus))
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
}

// ===========================================================================
// Suno polling: success accrues exactly once, failure refunds exactly once
// ===========================================================================

type sunoPollStubAdaptor struct {
	items []dto.SunoDataResponse
}

func (a *sunoPollStubAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *sunoPollStubAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	body, err := common.Marshal(dto.TaskResponse[[]dto.SunoDataResponse]{
		Code: dto.TaskSuccessCode,
		Data: a.items,
	})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func (a *sunoPollStubAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (a *sunoPollStubAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func seedSunoChannel(t *testing.T, id int) {
	t.Helper()
	baseURL := "http://suno.local"
	ch := &model.Channel{
		Id:      id,
		Type:    constant.ChannelTypeSunoAPI,
		Name:    "suno_channel",
		Key:     "sk-suno",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(ch).Error)
}

func seedSunoTask(t *testing.T, channelID, userID, quota int, upstreamID string) *model.Task {
	t.Helper()
	task := makeTask(userID, channelID, quota, 0, BillingSourceWallet, 0)
	task.Platform = constant.TaskPlatformSuno
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	task.PrivateData.UpstreamTaskID = upstreamID
	require.NoError(t, model.DB.Create(task).Error)
	return task
}

func stubSunoAdaptor(t *testing.T, adaptor TaskPollingAdaptor) {
	t.Helper()
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })
}

func TestSunoSuccessAccruesLotteryExactlyOnce(t *testing.T) {
	truncate(t)
	saveLotteryTestConfig(t)
	ctx := context.Background()

	const userID, channelID = 44, 44
	const preConsumed = 25
	seedUser(t, userID, 100000)
	seedSunoChannel(t, channelID)
	task := seedSunoTask(t, channelID, userID, preConsumed, "suno-up-1")

	stubSunoAdaptor(t, &sunoPollStubAdaptor{items: []dto.SunoDataResponse{{
		TaskID: "suno-up-1",
		Status: model.TaskStatusSuccess,
		Data:   json.RawMessage(`{}`),
	}}})

	taskM := map[string]*model.Task{"suno-up-1": task}
	require.NoError(t, updateSunoTasks(ctx, channelID, []string{"suno-up-1"}, taskM))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)

	// The final wallet charge counted exactly once (25 / threshold 20).
	progress := getLotteryProgress(t, userID)
	assert.Equal(t, 1, progress.AvailableDraws)
	assert.Equal(t, 5, progress.Accumulated)
	// No refund/settlement delta for the fixed pre-consumed charge.
	assert.Equal(t, 100000, getUserQuota(t, userID))

	// A repeated poll of the already-successful task must not accrue again.
	taskM = map[string]*model.Task{"suno-up-1": &reloaded}
	require.NoError(t, updateSunoTasks(ctx, channelID, []string{"suno-up-1"}, taskM))
	progress = getLotteryProgress(t, userID)
	assert.Equal(t, 1, progress.AvailableDraws)
	assert.Equal(t, 5, progress.Accumulated)
}

func TestSunoFailureRefundsExactlyOnceUnderPollingRaces(t *testing.T) {
	truncate(t)
	saveLotteryTestConfig(t)
	ctx := context.Background()

	const userID, channelID = 45, 45
	const initQuota, preConsumed = 10000, 3000
	seedUser(t, userID, initQuota)
	seedSunoChannel(t, channelID)
	task := seedSunoTask(t, channelID, userID, preConsumed, "suno-up-2")

	stubSunoAdaptor(t, &sunoPollStubAdaptor{items: []dto.SunoDataResponse{{
		TaskID:     "suno-up-2",
		Status:     model.TaskStatusFailure,
		FailReason: "generation failed",
		Data:       json.RawMessage(`{}`),
	}}})

	taskM := map[string]*model.Task{"suno-up-2": task}
	require.NoError(t, updateSunoTasks(ctx, channelID, []string{"suno-up-2"}, taskM))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Failed tasks never accrue lottery progress.
	progress := getLotteryProgress(t, userID)
	assert.Zero(t, progress.Accumulated)
	assert.Zero(t, progress.AvailableDraws)

	// Simulate a second poller holding a stale in-memory snapshot (still
	// IN_PROGRESS) while the row is already FAILURE: the CAS must lose and no
	// second refund may be issued.
	stale := reloaded
	stale.Status = model.TaskStatus(model.TaskStatusInProgress)
	stale.Progress = "30%"
	taskM = map[string]*model.Task{"suno-up-2": &stale}
	require.NoError(t, updateSunoTasks(ctx, channelID, []string{"suno-up-2"}, taskM))
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	var refundLogs int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeRefund).Count(&refundLogs).Error)
	assert.EqualValues(t, 1, refundLogs)
}
