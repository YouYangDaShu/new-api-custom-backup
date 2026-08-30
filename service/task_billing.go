package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo) {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		if len(info.PriceData.OtherRatios) > 0 {
			var contents []string
			for key, ra := range info.PriceData.OtherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
			if len(contents) > 0 {
				logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
			}
		}
	}
	other := make(map[string]interface{})
	other["is_task"] = true
	other["billing_source"] = info.BillingSource
	if info.TaskRelayInfo != nil {
		// Async tasks are pre-consumed at submission and count only after polling determines the final charge.
		other["defer_lottery"] = true
	}
	if taskID := c.GetString("task_id"); taskID != "" {
		other["task_id"] = taskID
	}
	other["request_path"] = c.Request.URL.Path
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	attachQuotaSaturation(c, info, other)
	other["status_code"] = 200
	other["request_status"] = "completed"
	if info.PriceData.Quota > 0 {
		other["billing_status"] = "charged"
	} else {
		other["billing_status"] = "no_charge"
	}
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     info.PriceData.Quota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	})
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, info.PriceData.Quota)
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// taskIsSubscription 判断任务是否通过订阅计费。
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

// taskAdjustFunding 调整任务的资金来源（钱包或订阅），delta > 0 表示扣费，delta < 0 表示退还。
func taskAdjustFunding(task *model.Task, delta int) error {
	if taskIsSubscription(task) {
		return model.PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
	}
	if delta > 0 {
		return model.DecreaseUserQuota(task.UserId, delta, false)
	}
	return model.IncreaseUserQuota(task.UserId, -delta, false)
}

// taskAdjustTokenQuota 调整任务的令牌额度，delta > 0 表示扣费，delta < 0 表示退还。
// 令牌 Key 需要运行时查询（不从 PrivateData 中读取，用于 Redis 缓存操作）。
// 令牌已被删除时视为无额度可调整并跳过（返回 nil），不让整个结算/退款失败；
// 其他查询或更新错误原样返回，由调用方补偿回滚。
func taskAdjustTokenQuota(ctx context.Context, task *model.Task, delta int) error {
	if task.PrivateData.TokenId <= 0 || delta == 0 {
		return nil
	}
	token, err := model.GetTokenById(task.PrivateData.TokenId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.LogWarn(ctx, fmt.Sprintf("令牌已删除，跳过令牌额度调整 (tokenId=%d, task=%s)", task.PrivateData.TokenId, task.TaskID))
			return nil
		}
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌失败 (tokenId=%d, task=%s): %s", task.PrivateData.TokenId, task.TaskID, err.Error()))
		return err
	}
	if delta > 0 {
		err = model.DecreaseTokenQuota(task.PrivateData.TokenId, token.Key, delta)
	} else {
		err = model.IncreaseTokenQuota(task.PrivateData.TokenId, token.Key, -delta)
	}
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("调整令牌额度失败 (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
	}
	return err
}

// revertTaskFunding 回滚一次已生效的资金来源调整（结算/退款后续步骤失败时的补偿）。
// 补偿本身失败时无法自动恢复，记录带残留金额的错误日志供人工核对，并返回 false。
func revertTaskFunding(ctx context.Context, task *model.Task, appliedDelta int) bool {
	if err := taskAdjustFunding(task, -appliedDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("任务结算补偿失败：资金来源存在未回滚差额 %s，需人工核对 task %s: %s",
			logger.LogQuota(appliedDelta), task.TaskID, err.Error()))
		return false
	}
	return true
}

// revertTaskTokenQuota 回滚一次已生效的令牌额度调整（结算/退款后续步骤失败时的补偿）。
// 补偿本身失败时无法自动恢复，记录带残留金额的错误日志供人工核对，并返回 false。
func revertTaskTokenQuota(ctx context.Context, task *model.Task, appliedDelta int) bool {
	if err := taskAdjustTokenQuota(ctx, task, -appliedDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("任务结算补偿失败：令牌额度存在未回滚差额 %s，需人工核对 task %s: %s",
			logger.LogQuota(appliedDelta), task.TaskID, err.Error()))
		return false
	}
	return true
}

// markTaskBillingManual 冻结一笔补偿失败的任务账务：钱包/令牌状态已偏离结算前
// 基线，自动重试可能重复应用差额，因此恢复清扫必须跳过该任务。残留金额已由
// revert helper 记入错误日志，供人工核对。
func markTaskBillingManual(ctx context.Context, task *model.Task) {
	if err := task.UpdateBillingState(model.TaskBillingStateManual); err != nil {
		logger.LogError(ctx, fmt.Sprintf("任务 %s 人工核对标记写入失败（账务仍需人工处理）: %s", task.TaskID, err.Error()))
	}
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	other["defer_lottery"] = true
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if len(bc.OtherRatios) > 0 {
			for k, v := range bc.OtherRatios {
				other[k] = v
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	return other
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，将预扣的 quota 退还给用户（支持钱包和订阅），并退还令牌额度。
//
// 钱包/令牌走批量更新与 Redis 缓存、订阅走独立事务，三者无法合并进单个数据库
// 事务，因此退款不是原子的，而是按 资金来源 → 令牌 → 任务快照 顺序执行并显式
// 补偿：任何一步失败会回滚已完成的步骤并返回 false，任务保持"已扣费"的一致状态，
// 之后可安全重试。全部成功后把 task.Quota 置 0 并持久化，作为幂等标记——重复的
// 终态处理会命中顶部的 quota==0 早返回，不会重复退款。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) bool {
	quota := task.Quota
	if quota == 0 {
		return true
	}

	// 1. 退还资金来源（钱包或订阅）
	if err := taskAdjustFunding(task, -quota); err != nil {
		logger.LogError(ctx, fmt.Sprintf("退还资金来源失败 task %s: %s", task.TaskID, err.Error()))
		return false
	}

	// 2. 退还令牌额度，失败时回滚资金退款
	if err := taskAdjustTokenQuota(ctx, task, -quota); err != nil {
		logger.LogError(ctx, fmt.Sprintf("退还令牌额度失败 task %s: %s", task.TaskID, err.Error()))
		if !revertTaskFunding(ctx, task, -quota) {
			markTaskBillingManual(ctx, task)
		}
		return false
	}

	// 3. 持久化最终扣费为 0（幂等标记）。回写失败时回滚整笔退款，
	// 避免"用户已退款但任务仍显示已扣费"的不一致状态。
	task.Quota = 0
	if task.ID != 0 {
		if err := task.UpdateQuota(); err != nil {
			logger.LogError(ctx, fmt.Sprintf("退款回写 quota 失败 task %s: %s", task.TaskID, err.Error()))
			task.Quota = quota
			tokenReverted := revertTaskTokenQuota(ctx, task, -quota)
			fundingReverted := revertTaskFunding(ctx, task, -quota)
			if !tokenReverted || !fundingReverted {
				markTaskBillingManual(ctx, task)
			}
			return false
		}
	}

	// 4. 记录日志
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		Content:   "",
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     quota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})
	return true
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
// clamps 可选：若计算 actualQuota 时发生额度饱和，将其记入日志 admin_info（仅管理员可见）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string, clamps ...*common.QuotaClamp) bool {
	if actualQuota <= 0 || actualQuota > common.MaxQuota {
		logger.LogError(ctx, fmt.Sprintf("任务 %s 拒绝异常实际额度：%d（%s）", task.TaskID, actualQuota, reason))
		return false
	}
	for _, clamp := range clamps {
		if clamp != nil && clamp.Kind == common.QuotaClampOverflow && clamp.Original > float64(common.MaxQuota) {
			logger.LogError(ctx, fmt.Sprintf("任务 %s 拒绝饱和后的异常实际额度：原始值=%g（%s）", task.TaskID, clamp.Original, reason))
			return false
		}
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）",
			task.TaskID, logger.LogQuota(actualQuota), reason))
		return true
	}

	logger.LogInfo(ctx, fmt.Sprintf("任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	// 结算按 资金来源 → 令牌 → 任务快照 的顺序执行。钱包/令牌走批量更新与
	// Redis 缓存、订阅走独立事务，三者无法合并进单个数据库事务，因此结算不是
	// 原子的，而是依赖显式补偿：任何一步失败会回滚已完成的步骤并返回 false，
	// 账务回到预扣费基线，之后可安全重试；task.Quota 仅在全部成功后才更新，
	// 重复结算会得到 quotaDelta == 0，不会重复扣退。

	// 1. 调整资金来源
	if err := taskAdjustFunding(task, quotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算资金调整失败 task %s: %s", task.TaskID, err.Error()))
		return false
	}

	// 2. 调整令牌额度，失败时回滚资金调整
	if err := taskAdjustTokenQuota(ctx, task, quotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算令牌调整失败 task %s: %s", task.TaskID, err.Error()))
		if !revertTaskFunding(ctx, task, quotaDelta) {
			markTaskBillingManual(ctx, task)
		}
		return false
	}

	// 3. 回写任务最终额度。Tasks finalized in production always have a
	// persisted primary key. Some callers intentionally settle an in-memory
	// task (for example, focused billing tests), so there is no row to update
	// in that case. Preserve the accounting result while still surfacing real
	// persistence failures for persisted tasks. 回写失败时恢复内存值并回滚
	// 已应用的额度，保证账务与任务快照一致。
	task.Quota = actualQuota
	if task.ID != 0 {
		if err := task.UpdateQuota(); err != nil {
			logger.LogError(ctx, fmt.Sprintf("差额结算回写 quota 失败 task %s: %s", task.TaskID, err.Error()))
			task.Quota = preConsumedQuota
			tokenReverted := revertTaskTokenQuota(ctx, task, quotaDelta)
			fundingReverted := revertTaskFunding(ctx, task, quotaDelta)
			if !tokenReverted || !fundingReverted {
				markTaskBillingManual(ctx, task)
			}
			return false
		}
	}

	var logType int
	var logQuota int
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
		model.UpdateUserUsedQuotaAndRequestCount(task.UserId, quotaDelta)
		model.UpdateChannelUsedQuota(task.ChannelId, quotaDelta)
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = preConsumedQuota
	other["actual_quota"] = actualQuota
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   logType,
		Content:   reason,
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     logQuota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
		NodeName:  task.PrivateData.NodeName,
	})
	return true
}

// RecordTaskLotteryConsumption records one successful async task's final wallet charge.
// The task ID makes repeated polling/finalization idempotent.
func RecordTaskLotteryConsumption(task *model.Task) error {
	if task == nil || task.Quota <= 0 || task.TaskID == "" || task.PrivateData.BillingSource == BillingSourceSubscription {
		return nil
	}
	if err := model.AccumulateLotteryConsumption(task.UserId, task.Quota, "task:"+task.TaskID); err != nil {
		common.SysError("failed to accumulate lottery task final consumption: " + err.Error())
		return err
	}
	return nil
}

// RecordMjTaskLotteryConsumption records one successful Midjourney task's wallet
// charge toward lottery progress. Submission logs defer accrual (see
// GenerateMjOtherInfo), so this runs only on the poller's terminal success
// transition; the MJ task ID makes repeated polls idempotent. MJ tasks are
// always wallet-billed, so no subscription exclusion is needed here.
func RecordMjTaskLotteryConsumption(task *model.Midjourney) error {
	if task == nil || task.Quota <= 0 || task.MjId == "" {
		return nil
	}
	if err := model.AccumulateLotteryConsumption(task.UserId, task.Quota, "mj:"+task.MjId); err != nil {
		common.SysError("failed to accumulate lottery mj task consumption: " + err.Error())
		return err
	}
	return nil
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) bool {
	if totalTokens <= 0 {
		return false
	}

	modelName := taskModelName(task)

	// 获取模型价格和倍率
	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	// 只有配置了倍率(非固定价格)时才按 token 重新计费
	if !hasRatioSetting || modelRatio <= 0 {
		// The pre-consumed quota remains the final charge when token repricing
		// is not applicable (for example, fixed-price task models).
		return task.Quota > 0
	}

	// 获取用户和组的倍率信息
	group := task.Group
	if group == "" {
		user, err := model.GetUserById(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		// Keep the successful pre-consumption as the final charge when there is
		// no group context from which to calculate a token-based adjustment.
		return task.Quota > 0
	}

	// task.Group here is the billing/using group stored on the task; user group may differ.
	userGroup := group
	if user, err := model.GetUserById(task.UserId, false); err == nil && user.Group != "" {
		userGroup = user.Group
	}
	finalGroupRatio, _, _ := ratio_setting.ResolveGroupRatio(userGroup, group)

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if bc := task.PrivateData.BillingContext; bc != nil {
		for _, r := range bc.OtherRatios {
			if r != 1.0 && r > 0 {
				otherMultiplier *= r
			}
		}
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio * otherMultiplier（饱和转换，防止溢出成负数）
	actualQuota, clamp := common.QuotaFromFloatChecked(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)

	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	if clamp != nil && clamp.Kind == common.QuotaClampOverflow && clamp.Original > float64(common.MaxQuota) {
		logger.LogError(ctx, fmt.Sprintf("任务 %s 拒绝饱和后的异常实际额度：原始值=%g（%s）", task.TaskID, clamp.Original, reason))
		// The anomalous upstream total is not retryable. Keep the valid
		// pre-consumption as the final charge instead of stranding the task in
		// pending settlement forever.
		return task.Quota > 0
	}
	return RecalculateTaskQuota(ctx, task, actualQuota, reason, clamp)
}
