package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"
)

// TaskPollingAdaptor 定义轮询所需的最小适配器接口，避免 service -> relay 的循环依赖
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

// GetTaskAdaptorFunc 由 main 包注入，用于获取指定平台的任务适配器。
// 打破 service -> relay -> relay/channel -> service 的循环依赖。
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

var pendingBillingWorkerSeq uint64

var terminalBillingLeaseSeconds int64 = 300
var terminalBillingLeaseHeartbeat = 10 * time.Second

var executeTaskSettlementForBilling = executeTaskSettlement

func newPendingBillingWorkerID() string {
	if key, err := common.GenerateRandomCharsKey(24); err == nil {
		return "billing-worker-" + key
	}
	return fmt.Sprintf("billing-worker-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), atomic.AddUint64(&pendingBillingWorkerSeq, 1))
}

func taskBillingOperationKey(task *model.Task) string {
	if task == nil {
		return ""
	}
	if task.TaskID != "" {
		return "task:" + task.TaskID + ":terminal-billing"
	}
	return fmt.Sprintf("task-db:%d:terminal-billing", task.ID)
}

func startTaskBillingLeaseHeartbeat(ctx context.Context, taskID int64, workerID, operationKey string) (*atomic.Bool, func()) {
	lost := &atomic.Bool{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(terminalBillingLeaseHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				until := time.Now().Unix() + terminalBillingLeaseSeconds
				ok, err := model.RenewTerminalTaskBillingLease(taskID, workerID, until)
				if err != nil || !ok {
					lost.Store(true)
					return
				}
				if operationKey != "" {
					ok, err = model.RenewTaskBillingOperation(operationKey, workerID, until)
					if err != nil || !ok {
						lost.Store(true)
						return
					}
				}
			case <-stop:
				return
			case <-ctx.Done():
				lost.Store(true)
				return
			}
		}
	}()
	return lost, func() {
		close(stop)
		<-done
	}
}

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
func sweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks := model.GetTimedOutUnfinishedTasks(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	const legacyTaskCutoff int64 = 1740182400 // 2026-02-22 00:00:00 UTC
	reason := fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes)
	legacyReason := "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < legacyTaskCutoff

		oldStatus := task.Status
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		if isLegacy {
			task.FailReason = legacyReason
		} else {
			task.FailReason = reason
		}

		if isLegacy || task.Quota == 0 {
			task.BillingState = model.TaskBillingStateDone
		} else {
			task.BillingState = model.TaskBillingStatePending
		}

		won, err := task.UpdateWithStatus(oldStatus)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
		finishTerminalTaskBilling(ctx, task)
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

// sweepPendingBillingTasks retries terminal tasks whose settlement or refund
// did not complete when they were finalized (billing_state = "pending"), so a
// crash or transient DB error between the terminal CAS and the money movement
// cannot silently strand a charge or refund. The updated-at grace avoids
// racing a finalization that is still in flight; tasks downgraded to "manual"
// (a compensation failed) are intentionally excluded from automatic retries.
func sweepPendingBillingTasks(ctx context.Context) {
	const graceSeconds = 60
	tasks := model.GetTerminalTasksWithPendingBilling(time.Now().Unix()-graceSeconds, 100)
	if len(tasks) == 0 {
		return
	}
	now := time.Now().Unix()
	workerID := newPendingBillingWorkerID()
	recovered := 0
	for _, candidate := range tasks {
		if ctx.Err() != nil {
			return
		}
		task, claimed, err := model.ClaimTerminalTaskBilling(candidate.ID, workerID, now, now+300)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepPendingBillingTasks claim error for task %s: %v", candidate.TaskID, err))
			continue
		}
		if !claimed {
			continue
		}
		if finishTerminalTaskBillingWithLease(ctx, task, workerID) {
			recovered++
		}
	}
	logger.LogInfo(ctx, fmt.Sprintf("sweepPendingBillingTasks: recovered %d/%d tasks with unfinished terminal billing", recovered, len(tasks)))
}

// TaskPollSummary is the result recorded on an async_task_poll system task row,
// summarizing one polling pass.
type TaskPollSummary struct {
	UnfinishedTasks  int `json:"unfinished_tasks"`
	PlatformsScanned int `json:"platforms_scanned"`
	NullTasksFailed  int `json:"null_tasks_failed"`
}

// RunTaskPollingOnce performs one async-task (Suno/video) polling pass
// synchronously. It honors ctx cancellation (the system-task runner cancels it
// when the lease is lost) and, when report is non-nil, reports progress as
// (processedPlatforms, totalPlatforms). It returns immediately if the task
// adaptor factory has not been wired yet, to avoid a nil call during startup.
func RunTaskPollingOnce(ctx context.Context, report func(processed, total int)) TaskPollSummary {
	summary := TaskPollSummary{}
	if GetTaskAdaptorFunc == nil {
		return summary
	}
	if ctx == nil {
		ctx = context.Background()
	}

	common.SysLog("任务进度轮询开始")
	sweepTimedOutTasks(ctx)
	sweepPendingBillingTasks(ctx)
	allTasks := model.GetAllUnFinishSyncTasks(constant.TaskQueryLimit)
	summary.UnfinishedTasks = len(allTasks)
	platformTask := make(map[constant.TaskPlatform][]*model.Task)
	for _, t := range allTasks {
		platformTask[t.Platform] = append(platformTask[t.Platform], t)
	}

	totalPlatforms := len(platformTask)
	processedPlatforms := 0
	for platform, tasks := range platformTask {
		if ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedPlatforms, totalPlatforms)
		}
		processedPlatforms++
		if len(tasks) == 0 {
			continue
		}
		summary.PlatformsScanned++
		taskChannelM := make(map[int][]string)
		taskM := make(map[string]*model.Task)
		nullTaskIds := make([]int64, 0)
		for _, task := range tasks {
			upstreamID := task.GetUpstreamTaskID()
			if upstreamID == "" {
				// 统计失败的未完成任务
				nullTaskIds = append(nullTaskIds, task.ID)
				continue
			}
			taskM[upstreamID] = task
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
		}
		if len(nullTaskIds) > 0 {
			summary.NullTasksFailed += len(nullTaskIds)
			err := model.TaskBulkUpdateByID(nullTaskIds, map[string]any{
				"status":   "FAILURE",
				"progress": "100%",
			})
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Fix null task_id task error: %v", err))
			} else {
				logger.LogInfo(ctx, fmt.Sprintf("Fix null task_id task success: %v", nullTaskIds))
			}
		}
		if len(taskChannelM) == 0 {
			continue
		}

		DispatchPlatformUpdate(ctx, platform, taskChannelM, taskM)
	}
	if report != nil && ctx.Err() == nil {
		report(totalPlatforms, totalPlatforms)
	}
	common.SysLog("任务进度轮询完成")
	return summary
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	switch platform {
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		_ = UpdateSunoTasks(ctx, taskChannelM, taskM)
	default:
		if err := UpdateVideoTasks(ctx, platform, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateSunoTasks 按渠道更新所有 Suno 任务
func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := updateSunoTasks(ctx, channelId, taskIds, taskM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateSunoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := model.CacheGetChannel(channelId)
	if err != nil {
		common.SysLog(fmt.Sprintf("CacheGetChannel: %v", err))
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		err = model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if err != nil {
			common.SysLog(fmt.Sprintf("UpdateSunoTask error: %v", err))
		}
		return err
	}
	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		return errors.New("adaptor not found")
	}
	proxy := ch.GetSetting().Proxy
	resp, err := adaptor.FetchTask(*ch.BaseURL, ch.Key, map[string]any{
		"ids": taskIds,
	}, proxy)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Task Do req error: %v", err))
		return err
	}
	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		return fmt.Errorf("Get Task status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		return err
	}
	var responseItems dto.TaskResponse[[]dto.SunoDataResponse]
	err = common.Unmarshal(responseBody, &responseItems)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Suno Task parse body error2: %v, body: %s", err, string(responseBody)))
		return err
	}
	if !responseItems.IsSuccess() {
		common.SysLog(fmt.Sprintf("渠道 #%d 未完成的任务有: %d, 成功获取到任务数: %s", channelId, len(taskIds), string(responseBody)))
		return err
	}

	for _, responseItem := range responseItems.Data {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task := taskM[responseItem.TaskID]
		if task == nil {
			logger.LogWarn(ctx, fmt.Sprintf("Suno task response ignored: unknown task_id=%s", responseItem.TaskID))
			continue
		}
		if !taskNeedsUpdate(task, responseItem) {
			continue
		}

		previousStatus := task.Status
		task.Status = lo.If(model.TaskStatus(responseItem.Status) != "", model.TaskStatus(responseItem.Status)).Else(task.Status)
		task.FailReason = lo.If(responseItem.FailReason != "", responseItem.FailReason).Else(task.FailReason)
		task.SubmitTime = lo.If(responseItem.SubmitTime != 0, responseItem.SubmitTime).Else(task.SubmitTime)
		task.StartTime = lo.If(responseItem.StartTime != 0, responseItem.StartTime).Else(task.StartTime)
		task.FinishTime = lo.If(responseItem.FinishTime != 0, responseItem.FinishTime).Else(task.FinishTime)
		if responseItem.FailReason != "" || task.Status == model.TaskStatusFailure {
			logger.LogInfo(ctx, task.TaskID+" 构建失败，"+task.FailReason)
			task.Status = model.TaskStatusFailure
			task.Progress = "100%"
		}
		if task.Status == model.TaskStatusSuccess {
			task.Progress = "100%"
		}
		task.Data = responseItem.Data

		isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
		if isDone && previousStatus != task.Status {
			// CAS-guard the terminal write so a failed task cannot be refunded
			// twice under polling races or update failures, and a successful
			// wallet charge accrues lottery progress exactly once.
			FinalizeTerminalTaskBilling(ctx, adaptor, task, &relaycommon.TaskInfo{
				TaskID: responseItem.TaskID,
				Status: string(task.Status),
				Reason: task.FailReason,
			}, previousStatus)
			continue
		}

		err = task.Update()
		if err != nil {
			common.SysLog("UpdateSunoTask task error: " + err.Error())
		}
	}
	return nil
}

// taskNeedsUpdate 检查 Suno 任务是否需要更新
func taskNeedsUpdate(oldTask *model.Task, newTask dto.SunoDataResponse) bool {
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if string(oldTask.Status) != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}

	if (oldTask.Status == model.TaskStatusFailure || oldTask.Status == model.TaskStatusSuccess) && oldTask.Progress != "100%" {
		return true
	}

	oldData, _ := common.Marshal(oldTask.Data)
	newData, _ := common.Marshal(newTask.Data)

	sort.Slice(oldData, func(i, j int) bool {
		return oldData[i] < oldData[j]
	})
	sort.Slice(newData, func(i, j int) bool {
		return newData[i] < newData[j]
	})

	if string(oldData) != string(newData) {
		return true
	}
	return false
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	channelIDs := make([]int, 0, len(taskChannelM))
	for channelID := range taskChannelM {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)

	var wg sync.WaitGroup
	for _, channelId := range channelIDs {
		taskIds := taskChannelM[channelId]
		if len(taskIds) == 0 {
			continue
		}
		taskIds = append([]string(nil), taskIds...)

		wg.Add(1)
		gopool.Go(func() {
			defer wg.Done()
			if err := updateVideoTasks(ctx, platform, channelId, taskIds, taskM); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
			}
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		errUpdate := model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("Failed to get channel info, channel ID: %d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if errUpdate != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTask error: %v", errUpdate))
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelBaseUrl: cacheGetChannel.GetBaseURL(),
	}
	info.ApiKey = cacheGetChannel.Key
	adaptor.Init(info)
	disablePollingSleep := cacheGetChannel.GetOtherSettings().DisableTaskPollingSleep
	for i, taskId := range taskIds {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
		if disablePollingSleep || i == len(taskIds)-1 {
			continue
		}

		// sleep 1 second between tasks for this channel only.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, taskId string, taskM map[string]*model.Task) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	baseURL := constant.ChannelBaseURLs[ch.Type]
	if ch.GetBaseURL() != "" {
		baseURL = ch.GetBaseURL()
	}
	proxy := ch.GetSetting().Proxy

	task := taskM[taskId]
	if task == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	key := ch.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil {
		return fmt.Errorf("fetchTask failed for task %s: %w", taskId, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("readAll failed for task %s: %w", taskId, err)
	}

	logger.LogDebug(ctx, "updateVideoSingleTask response: %s", responseBody)

	snap := task.Snapshot()

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems dto.TaskResponse[model.Task]
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		logger.LogDebug(ctx, "updateVideoSingleTask parsed as new api response format: %+v", responseItems)
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		task.Data = t.Data
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	}

	task.Data = redactVideoResponseBody(responseBody)

	logger.LogDebug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)

	now := time.Now().Unix()
	if taskResult.Status == "" {
		//taskResult = relaycommon.FailTaskInfo("upstream returned empty status")
		errorResult := &dto.GeneralErrorResponse{}
		if err = common.Unmarshal(responseBody, &errorResult); err == nil {
			openaiError := errorResult.TryToOpenAIError()
			if openaiError != nil {
				// 返回规范的 OpenAI 错误格式，提取错误信息，判断错误是否为任务失败
				if openaiError.Code == "429" {
					// 429 错误通常表示请求过多或速率限制，暂时不认为是任务失败，保持原状态等待下一轮轮询
					return nil
				}

				// 其他错误认为是任务失败，记录错误信息并更新任务状态
				taskResult = relaycommon.FailTaskInfo("upstream returned error")
			} else {
				// unknown error format, log original response
				logger.LogError(ctx, fmt.Sprintf("Task %s returned empty status with unrecognized error format, response: %s", taskId, string(responseBody)))
				taskResult = relaycommon.FailTaskInfo("upstream returned unrecognized message")
			}
		}
	}

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if strings.HasPrefix(taskResult.Url, "data:") {
			// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		} else if taskResult.Url != "" {
			// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.)
			task.PrivateData.ResultURL = taskResult.Url
		} else {
			// No URL from adaptor — construct proxy URL using public task ID
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
	case model.TaskStatusFailure:
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", taskId), task)
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = taskcommon.ProgressComplete
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, task.TaskID)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone && snap.Status != task.Status {
		FinalizeTerminalTaskBilling(ctx, adaptor, task, taskResult, snap.Status)
	} else if !snap.Equal(task.Snapshot()) {
		if _, err := task.UpdateWithStatus(snap.Status); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update task %s: %s", task.TaskID, err.Error()))
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, "No update needed for task %s", task.TaskID)
	}

	return nil
}

// FinalizeTerminalTaskBilling persists a task's transition into a terminal
// status with a CAS guard and, only when this caller wins the transition, runs
// the terminal billing exactly once: settlement plus lottery accrual on
// success, refund on failure. Callers must already have applied the terminal
// fields (Status/Progress/FinishTime/FailReason) to the task. It is shared by
// the polling loops and the Gemini/Vertex realtime fetch path so both execute
// the same completion behavior without double settlement or double refunds.
func FinalizeTerminalTaskBilling(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo, fromStatus model.TaskStatus) bool {
	// Resolve the billing work and persist it inside the same CAS write as
	// the terminal transition, so a task can never sit in a terminal status
	// without a durable record of unfinished settlement/refund work. If the
	// money movement below fails, sweepPendingBillingTasks replays it from
	// the persisted plan.
	if task.Status == model.TaskStatusSuccess {
		prepareTaskSettlementPlan(task, adaptor, taskResult)
		if task.Quota != 0 || task.PrivateData.PendingSettleQuota > 0 || task.PrivateData.PendingSettleTokens > 0 {
			task.BillingState = model.TaskBillingStatePending
		} else {
			task.BillingState = model.TaskBillingStateDone
		}
	} else if task.Quota != 0 {
		task.BillingState = model.TaskBillingStatePending
	} else {
		task.BillingState = model.TaskBillingStateDone
	}

	won, err := task.UpdateWithStatus(fromStatus)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("UpdateWithStatus failed for task %s: %s", task.TaskID, err.Error()))
		return false
	}
	if !won {
		logger.LogWarn(ctx, fmt.Sprintf("Task %s already transitioned by another process, skip billing", task.TaskID))
		return false
	}
	finishTerminalTaskBilling(ctx, task)
	return true
}

// finishTerminalTaskBilling executes the billing work recorded for a terminal
// task (settlement + lottery accrual on success, refund on failure) and marks
// it done only after it fully completed. On failure the persisted "pending"
// marker stays in place for sweepPendingBillingTasks, unless the billing layer
// downgraded the task to "manual" because a compensation step failed.
// Idempotent: shared by the finalization paths and the recovery sweep.
func finishTerminalTaskBilling(ctx context.Context, task *model.Task) bool {
	if task != nil && task.ID != 0 && task.BillingState == model.TaskBillingStatePending && task.BillingLeaseOwner == "" {
		now := time.Now().Unix()
		workerID := newPendingBillingWorkerID()
		claimedTask, claimed, err := model.ClaimTerminalTaskBilling(task.ID, workerID, now, now+terminalBillingLeaseSeconds)
		if err != nil || !claimed {
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("任务 %s 账务 claim 失败: %s", task.TaskID, err.Error()))
			}
			return false
		}
		completed := finishTerminalTaskBillingWithLease(ctx, claimedTask, workerID)
		*task = *claimedTask
		return completed
	}
	return finishTerminalTaskBillingWithLease(ctx, task, "")
}

func finishTerminalTaskBillingWithLease(ctx context.Context, task *model.Task, workerID string) bool {
	if task == nil {
		return false
	}
	if task.BillingState != model.TaskBillingStatePending {
		return task.BillingState == model.TaskBillingStateDone
	}
	operationKey := ""
	if task.ID != 0 && workerID != "" {
		operationKey = taskBillingOperationKey(task)
	}
	var stopHeartbeat func()
	var leaseLost *atomic.Bool
	if workerID != "" && task.ID != 0 {
		valid, err := model.HasTerminalTaskBillingLease(task.ID, workerID, time.Now().Unix())
		if err != nil || !valid {
			return false
		}
	}

	operationState := model.TaskBillingOperationInProgress
	if operationKey != "" {
		var err error
		operationState, err = model.BeginTaskBillingOperation(operationKey, task.ID, workerID, time.Now().Unix(), task.BillingLeaseUntil)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("任务 %s 账务 operation claim 失败: %s", task.TaskID, err.Error()))
			return false
		}
		if operationState == "in_flight" {
			logger.LogWarn(ctx, fmt.Sprintf("任务 %s 账务 operation 仍由其他 worker 执行，保留 pending", task.TaskID))
			if workerID != "" {
				_ = model.ReleaseTerminalTaskBillingLease(task.ID, workerID)
			}
			return false
		}
		if operationState == model.TaskBillingOperationManual {
			logger.LogError(ctx, fmt.Sprintf("任务 %s 账务 operation 已进入 manual，需人工核对", task.TaskID))
			_ = task.UpdateBillingState(model.TaskBillingStateManual)
			if workerID != "" {
				_ = model.ReleaseTerminalTaskBillingLease(task.ID, workerID)
			}
			return false
		}
	}
	if workerID != "" && task.ID != 0 {
		opHeartbeatKey := ""
		if operationState == model.TaskBillingOperationInProgress {
			opHeartbeatKey = operationKey
		}
		leaseLost, stopHeartbeat = startTaskBillingLeaseHeartbeat(ctx, task.ID, workerID, opHeartbeatKey)
		defer stopHeartbeat()
	}

	if workerID != "" && (ctx.Err() != nil || leaseLost.Load()) {
		_ = model.FailTaskBillingOperation(operationKey, workerID, ctx.Err())
		_ = model.ReleaseTerminalTaskBillingLease(task.ID, workerID)
		return false
	}
	operationApplied := operationState == model.TaskBillingOperationDone
	completed := operationApplied
	if !operationApplied {
		if task.Status == model.TaskStatusSuccess {
			if executeTaskSettlementForBilling(ctx, task) {
				completed = true
			} else {
				// A false settlement with no remaining charge means there is no
				// billing work left (nothing was ever charged), not a retryable
				// failure; retryable failures keep task.Quota at the pre-consumed
				// value.
				completed = task.Quota == 0
			}
		} else {
			completed = task.Quota == 0 || RefundTaskQuota(ctx, task, task.FailReason)
		}
		if completed && operationKey != "" {
			won, err := model.CompleteTaskBillingOperation(operationKey, workerID)
			if err != nil || !won {
				// The mutation may already have committed. Never downgrade this
				// ambiguity to pending, which would replay the debit/refund.
				_ = model.MarkTaskBillingOperationManual(operationKey, workerID, "economic mutation completed but durable completion marker failed")
				_ = task.UpdateBillingState(model.TaskBillingStateManual)
				completed = false
			}
		}
	}
	if completed && task.Status == model.TaskStatusSuccess {
		if workerID == "" || (ctx.Err() == nil && !leaseLost.Load()) {
			if err := RecordTaskLotteryConsumption(task); err != nil {
				// Funds are already settled (and the operation is done). Keep the
				// terminal marker pending so recovery retries only this event.
				logger.LogWarn(ctx, fmt.Sprintf("任务 %s 抽奖累计未完成，等待恢复清扫重试: %s", task.TaskID, err.Error()))
				if workerID != "" {
					_ = model.ReleaseTerminalTaskBillingLease(task.ID, workerID)
				}
				return false
			}
		}
	}
	if !completed {
		if operationKey != "" && workerID != "" && !leaseLost.Load() && ctx.Err() == nil {
			_ = model.FailTaskBillingOperation(operationKey, workerID, errors.New("terminal billing attempt failed"))
		}
		logger.LogWarn(ctx, fmt.Sprintf("任务 %s 终态账务未完成，等待恢复清扫重试", task.TaskID))
		if workerID != "" {
			if err := model.ReleaseTerminalTaskBillingLease(task.ID, workerID); err != nil {
				logger.LogError(ctx, fmt.Sprintf("任务 %s 账务 lease 释放失败: %s", task.TaskID, err.Error()))
			}
		}
		return false
	}
	if workerID != "" && (ctx.Err() != nil || leaseLost.Load()) {
		logger.LogWarn(ctx, fmt.Sprintf("任务 %s 账务 lease 在资金操作后失效，保留 pending", task.TaskID))
		_ = model.ReleaseTerminalTaskBillingLease(task.ID, workerID)
		return false
	}
	if workerID != "" {
		won, err := model.CompleteTerminalTaskBilling(task.ID, workerID)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("任务 %s 账务完成标记写入失败: %s", task.TaskID, err.Error()))
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("任务 %s 账务 lease 已失效，保留 pending 供恢复", task.TaskID))
		} else {
			task.BillingState = model.TaskBillingStateDone
			task.BillingLeaseOwner = ""
			task.BillingLeaseUntil = 0
		}
		return won && err == nil
	}
	if err := task.UpdateBillingState(model.TaskBillingStateDone); err != nil {
		// The billing itself succeeded; a retried finalization is a no-op
		// (delta 0 / quota already 0 / lottery event dedup).
		logger.LogError(ctx, fmt.Sprintf("任务 %s 账务完成标记写入失败: %s", task.TaskID, err.Error()))
	}
	return true
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	b, err := common.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}

// settleTaskBillingOnComplete 任务完成时的统一计费调整。
// 优先级：1. adaptor.AdjustBillingOnComplete 返回正数 → 使用 adaptor 计算的额度
//
//  2. taskResult.TotalTokens > 0 → 按 token 重算
//  3. 都不满足 → 保持预扣额度不变
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) bool {
	if task == nil || task.TaskID == "" {
		return false
	}
	prepareTaskSettlementPlan(task, adaptor, taskResult)
	return executeTaskSettlement(ctx, task)
}

// prepareTaskSettlementPlan resolves the settlement decision for a successful
// task and records it on the task (PrivateData.PendingSettle*), so the plan is
// persisted by the terminal CAS write and the recovery sweep can replay the
// exact same settlement without re-fetching upstream state.
func prepareTaskSettlementPlan(task *model.Task, adaptor TaskPollingAdaptor, taskResult *relaycommon.TaskInfo) {
	if task == nil {
		return
	}
	// 按次计费的任务不做差额结算，无需结算计划
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		return
	}
	// 1. 优先让 adaptor 决定最终额度
	if adaptor != nil && taskResult != nil {
		if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
			if actualQuota > common.MaxQuota {
				common.SysError(fmt.Sprintf("task %s returned invalid actual quota %d", task.TaskID, actualQuota))
				return
			}
			task.PrivateData.PendingSettleQuota = actualQuota
			return
		}
	}
	// 2. 回退到 token 重算
	if taskResult != nil && taskResult.TotalTokens > 0 {
		task.PrivateData.PendingSettleTokens = taskResult.TotalTokens
	}
}

// executeTaskSettlement applies the recorded settlement plan. Returns true
// when the final charge is in place and non-zero (the caller then accrues
// lottery progress); false on settlement failure or when nothing was charged.
func executeTaskSettlement(ctx context.Context, task *model.Task) bool {
	if task == nil || task.TaskID == "" {
		return false
	}
	// 0. 按次计费的任务不做差额结算
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 按次计费，跳过差额结算", task.TaskID))
		return task.Quota > 0
	}
	// 1. adaptor 决定的最终额度
	if actualQuota := task.PrivateData.PendingSettleQuota; actualQuota > 0 {
		return RecalculateTaskQuota(ctx, task, actualQuota, "adaptor计费调整")
	}
	// 2. token 重算
	if tokens := task.PrivateData.PendingSettleTokens; tokens > 0 {
		return RecalculateTaskQuotaByTokens(ctx, task, tokens)
	}
	// 3. 无调整，保持预扣额度
	return task.Quota > 0
}
