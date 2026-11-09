package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
// settleErr 是本次提交的结算结果：失败必须写进消费日志 other（与文本/音频/
// realtime 路径的 MarkSettleFailure 一致），否则这笔差额只有一行 stderr，
// 无法与具体请求对账。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo, settleErr error) {
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
	usageStatsErr := RecordUsageStats(info.UserId, info.ChannelId, info.PriceData.Quota)
	MarkUsageStatsFailure(c, info, info.PriceData.Quota, usageStatsErr, other)
	MarkSettleFailure(c, info, info.PriceData.Quota, settleErr, other)
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
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// resolveTokenKey 通过 TokenId 运行时获取令牌 Key（用于 Redis 缓存操作）。
// 如果令牌已被删除或查询失败，返回空字符串。
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskIsSubscription 判断任务是否通过订阅计费。
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

// taskAdjustFunding 调整任务的资金来源（钱包或订阅），delta > 0 表示扣费，delta < 0 表示退还。
//
// allowSubscriptionOverage 供请求已执行完的差额结算使用：订阅超额必须落账，
// 否则整笔差额被拒，这次执行就成了免费。退款与预扣路径保持严格拒绝。
func taskAdjustFunding(task *model.Task, delta int, allowSubscriptionOverage bool) error {
	if taskIsSubscription(task) {
		if allowSubscriptionOverage {
			return model.SettleUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
		}
		return model.PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
	}
	if delta > 0 {
		return model.DecreaseUserQuota(task.UserId, delta, false)
	}
	return model.IncreaseUserQuota(task.UserId, -delta, false)
}

// taskAdjustTokenQuota 调整任务的令牌额度，delta > 0 表示扣费，delta < 0 表示退还。
// 需要通过 resolveTokenKey 运行时获取 key（不从 PrivateData 中读取）。
func taskAdjustTokenQuota(ctx context.Context, task *model.Task, delta int) {
	if task.PrivateData.TokenId <= 0 || delta == 0 {
		return
	}
	tokenKey := resolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	if tokenKey == "" {
		return
	}
	var err error
	if delta > 0 {
		err = model.DecreaseTokenQuota(task.PrivateData.TokenId, tokenKey, delta)
	} else {
		err = model.IncreaseTokenQuota(task.PrivateData.TokenId, tokenKey, -delta)
	}
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("调整令牌额度失败 (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
	}
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
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

// TaskBillingRetryLimit bounds how many times a failed terminal settlement or
// refund may roll the task back into the polling pool. Without a limit, a
// permanent funding failure (a deleted subscription, a vanished user) would
// re-enter the poller forever and, for a success settlement, re-poll the
// provider on every cycle.
const TaskBillingRetryLimit = 5

// RevertTaskTerminalTransition rolls a terminal transition back to its
// pre-transition snapshot when the billing step that owns it failed before any
// money moved. Without the revert the task stays terminal, the poller never
// selects it again, and the settlement or refund is lost for good. The revert
// is CAS-guarded by the terminal status, so a concurrent writer that already
// moved the row is never overwritten.
//
// Retries are bounded: after TaskBillingRetryLimit attempts the task keeps its
// terminal state and the failure is logged for operators, so a permanent
// funding failure stops looping instead of retrying on every poll cycle.
func RevertTaskTerminalTransition(ctx context.Context, task *model.Task, terminalStatus model.TaskStatus, snap model.TaskSnapshot) {
	if task.PrivateData.BillingRetryCount >= TaskBillingRetryLimit {
		logger.LogError(ctx, fmt.Sprintf("任务 %s 终态计费已连续失败 %d 次，保留终态不再重试，需要人工核对",
			task.TaskID, task.PrivateData.BillingRetryCount))
		return
	}
	task.PrivateData.BillingRetryCount++
	won, err := task.RestoreSnapshot(terminalStatus, snap)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("任务 %s 计费失败后回退终态出错: %s", task.TaskID, err.Error()))
		return
	}
	if !won {
		logger.LogWarn(ctx, fmt.Sprintf("任务 %s 计费失败后回退终态未生效（状态已被其他进程改写）", task.TaskID))
		return
	}
	task.Status = snap.Status
	task.Progress = snap.Progress
	task.StartTime = snap.StartTime
	task.FinishTime = snap.FinishTime
	task.FailReason = snap.FailReason
	task.Data = snap.Data
	logger.LogWarn(ctx, fmt.Sprintf("任务 %s 计费失败，已回退到 %s 等待下一轮重试（第 %d 次）",
		task.TaskID, snap.Status, task.PrivateData.BillingRetryCount))
}

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，将预扣的 quota 退还给用户（支持钱包和订阅），并退还令牌额度。
//
// 返回值只表示资金来源（钱包或订阅）是否退款成功：那是第一步不可逆操作，
// 失败时数据库未发生任何资金变动，调用方可以安全地回退任务终态并等待重试。
// 令牌额度与日志失败发生在资金已变动之后，只记录错误，重试会导致重复退款。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) error {
	quota := task.Quota
	if quota == 0 {
		return nil
	}

	// 1. 退还资金来源（钱包或订阅）
	if err := taskAdjustFunding(task, -quota, false); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("退还资金来源失败 task %s: %s", task.TaskID, err.Error()))
		return err
	}

	// 2. 退还令牌额度
	taskAdjustTokenQuota(ctx, task, -quota)

	// 3. 记录日志
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
	return nil
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
//
// 与 RefundTaskQuota 相同，返回值只报告资金来源调整是否成功；失败表示
// 数据库未发生资金变动，调用方可以安全回退任务终态等待重试。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string) error {
	if actualQuota <= 0 {
		return nil
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）",
			task.TaskID, logger.LogQuota(actualQuota), reason))
		return nil
	}

	logger.LogInfo(ctx, fmt.Sprintf("任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	// 调整资金来源（任务已执行完，订阅超额必须落账）
	if err := taskAdjustFunding(task, quotaDelta, true); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算资金调整失败 task %s: %s", task.TaskID, err.Error()))
		return err
	}

	// 调整令牌额度
	taskAdjustTokenQuota(ctx, task, quotaDelta)

	task.Quota = actualQuota
	// 结算额度必须落库：任务列表读的是库里的 quota，只改内存会让用户看到的
	// 扣费停留在预扣值，任何按行内 quota 计算的后续动作也会拿到旧值。
	if task.ID > 0 {
		if err := model.UpdateTaskQuota(task.ID, actualQuota); err != nil {
			logger.LogError(ctx, fmt.Sprintf("持久化任务结算额度失败 task %s: %s", task.TaskID, err.Error()))
		}
	}

	var logType int
	var logQuota int
	var usageStatsErr error
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
		usageStatsErr = RecordUsageStats(task.UserId, task.ChannelId, quotaDelta)
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = preConsumedQuota
	other["actual_quota"] = actualQuota
	if usageStatsErr != nil {
		other[usageStatsErrorLogField] = usageStatsErr.Error()
		logger.LogError(ctx, fmt.Sprintf("error recording task usage stats: %s (userId %d, channelId %d, quota %d)",
			usageStatsErr.Error(), task.UserId, task.ChannelId, quotaDelta))
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
	})
	return nil
}

// taskFrozenGroupRatio returns the group ratio captured at submission time.
// The task row stores the concrete group that was actually used, not the
// user's own group, so re-deriving the ratio from configuration can pick a
// different value than the one the pre-consume charged.
func taskFrozenGroupRatio(task *model.Task) (float64, bool) {
	if task == nil || task.PrivateData.BillingContext == nil {
		return 0, false
	}
	ratio := task.PrivateData.BillingContext.GroupRatio
	if ratio <= 0 {
		return 0, false
	}
	return ratio, true
}

// resolveTaskGroupRatio re-derives the group ratio for legacy rows that carry
// no billing snapshot. The special ratio is keyed by (user group, using group);
// passing the using group twice never matches a configured entry.
func resolveTaskGroupRatio(task *model.Task) float64 {
	if task == nil {
		return 0
	}
	usingGroup := task.Group
	userGroup := ""
	if task.UserId > 0 {
		if user, err := model.GetUserById(task.UserId, false); err == nil {
			userGroup = user.Group
		}
	}
	if usingGroup == "" {
		usingGroup = userGroup
	}
	if usingGroup == "" {
		return 0
	}
	if special, ok := ratio_setting.GetGroupGroupRatio(userGroup, usingGroup); ok {
		return special
	}
	return ratio_setting.GetGroupRatio(usingGroup)
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。
//
// 只有 totalTokens、没有输入/输出拆分时，无法区分输出 token 的价格：任务模型
// 普遍给输出定价数倍于输入，整体按输入价重算会系统性少收。因此这里不再估算，
// 而是要求调用方提供拆分（见 RecalculateTaskQuotaByTokensWithUsage）；确实拿不到
// 拆分时保持预扣额度不变，不能拿一个偏低的金额覆盖已按真实单价预扣的结果。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) error {
	if totalTokens <= 0 {
		return nil
	}
	reason := fmt.Sprintf("token重算跳过：仅有 totalTokens=%d，缺少输入/输出拆分，保留预扣额度", totalTokens)
	common.SysLog("task billing: " + reason)
	return nil
}

// RecalculateTaskQuotaByTokensWithUsage 按输入/输出拆分重新计费：
// inputTokens * modelRatio + outputTokens * modelRatio * completionRatio，
// 再乘分组倍率与其它倍率。这是任务终态唯一应走的重算路径。
func RecalculateTaskQuotaByTokensWithUsage(ctx context.Context, task *model.Task, inputTokens, outputTokens int) error {
	if inputTokens < 0 || outputTokens < 0 {
		return nil
	}
	total := inputTokens + outputTokens
	if total <= 0 {
		return nil
	}

	modelName := taskModelName(task)

	// 获取模型价格和倍率
	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	// 只有配置了倍率(非固定价格)时才按 token 重新计费
	if !hasRatioSetting || modelRatio <= 0 {
		return nil
	}

	// 分组倍率优先取提交时冻结的快照：预扣用的就是它，重算另取一份配置
	// 会让同一笔任务在补扣/退款时换一套口径。只有旧行没有快照时才回退到
	// 现查，且必须按 (用户分组, 使用分组) 查专属倍率。
	finalGroupRatio, ok := taskFrozenGroupRatio(task)
	if !ok {
		finalGroupRatio = resolveTaskGroupRatio(task)
	}
	if finalGroupRatio <= 0 {
		return nil
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if bc := task.PrivateData.BillingContext; bc != nil {
		for _, r := range bc.OtherRatios {
			if r != 1.0 && r > 0 {
				otherMultiplier *= r
			}
		}
	}

	// 输出 token 按完成倍率计价；GetCompletionRatio 对未配置的模型返回默认值。
	completionRatio := ratio_setting.GetCompletionRatio(modelName)
	if completionRatio <= 0 {
		completionRatio = 1
	}

	weightedTokens := float64(inputTokens) + float64(outputTokens)*completionRatio
	actualQuota := common.SafeQuotaIntFromFloat64(weightedTokens * modelRatio * finalGroupRatio * otherMultiplier)

	reason := fmt.Sprintf("token重算：in=%d, out=%d, completionRatio=%.2f, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f",
		inputTokens, outputTokens, completionRatio, modelRatio, finalGroupRatio, otherMultiplier)
	return RecalculateTaskQuota(ctx, task, actualQuota, reason)
}
