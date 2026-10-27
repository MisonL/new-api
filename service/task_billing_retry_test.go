package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// 终态先落库、计费后执行：资金来源调整失败时只写日志，任务已经变成
// FAILURE，轮询只查未完成任务，这笔退款就永远不会再被尝试，用户预扣的
// 额度凭空消失。失败必须把终态回退，让下一轮轮询重新驱动计费。
func TestMarkTaskPollingFailureRevertsStatusWhenRefundFundingFails(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 61, 61
	const taskQuota = 3000
	seedUser(t, userID, 10000)
	seedChannel(t, channelID)

	// 订阅计费但订阅行不存在：资金来源退款必定失败，且失败发生在任何
	// 资金变动之前（PostConsumeUserSubscriptionDelta 在事务内查不到行）。
	task := makeTask(userID, channelID, taskQuota, 0, BillingSourceSubscription, 999)
	task.TaskID = "refund-revert-" + time.Now().Format("150405.000000")
	task.Status = model.TaskStatusInProgress
	task.Progress = "40%"
	require.NoError(t, model.DB.Create(task).Error)

	err := markTaskPollingFailure(ctx, task, "upstream rejected task")
	require.Error(t, err, "退款失败必须上报给调用方，而不是只写日志")

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	require.EqualValues(t, model.TaskStatusInProgress, persisted.Status,
		"退款失败后任务必须回到未完成状态，否则轮询永远不会再尝试退款")
	require.Equal(t, "40%", persisted.Progress)
	require.Empty(t, persisted.FailReason)
}

// 永久性失败不能无限重试：计数达到上限后保留终态并停止回退，否则每轮
// 轮询都会重新请求上游并再次失败。
func TestMarkTaskPollingFailureStopsRetryingAtLimit(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 63, 63
	seedUser(t, userID, 10000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, 3000, 0, BillingSourceSubscription, 999)
	task.TaskID = "refund-limit-" + time.Now().Format("150405.000000")
	task.Status = model.TaskStatusInProgress
	task.PrivateData.BillingRetryCount = TaskBillingRetryLimit
	require.NoError(t, model.DB.Create(task).Error)

	require.Error(t, markTaskPollingFailure(ctx, task, "upstream rejected task"))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	require.EqualValues(t, model.TaskStatusFailure, persisted.Status,
		"重试次数用尽后必须保留终态，避免永久失败每轮轮询重复请求上游")
	require.Equal(t, TaskBillingRetryLimit, persisted.PrivateData.BillingRetryCount)
}

// 每次回退必须把重试计数落库，否则进程重启后计数清零，上限形同虚设。
func TestRevertTaskTerminalTransitionPersistsRetryCount(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 64, 64
	seedUser(t, userID, 10000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, 3000, 0, BillingSourceSubscription, 999)
	task.TaskID = "refund-count-" + time.Now().Format("150405.000000")
	task.Status = model.TaskStatusInProgress
	task.Progress = "40%"
	require.NoError(t, model.DB.Create(task).Error)
	snap := task.Snapshot()

	task.Status = model.TaskStatusFailure
	won, err := task.UpdateWithStatus(model.TaskStatusInProgress)
	require.NoError(t, err)
	require.True(t, won)

	RevertTaskTerminalTransition(ctx, task, model.TaskStatusFailure, snap)

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	require.EqualValues(t, model.TaskStatusInProgress, persisted.Status)
	require.Equal(t, 1, persisted.PrivateData.BillingRetryCount)
}

func TestMarkTaskPollingFailureKeepsStatusWhenRefundSucceeds(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 62, 62, 62
	const initialQuota, taskQuota = 10000, 3000
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, "refund-keep-key", 5000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, taskQuota, tokenID, BillingSourceWallet, 0)
	task.TaskID = "refund-keep-" + time.Now().Format("150405.000000")
	task.Status = model.TaskStatusInProgress
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, markTaskPollingFailure(ctx, task, "upstream rejected task"))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	require.EqualValues(t, model.TaskStatusFailure, persisted.Status)
	require.Equal(t, initialQuota+taskQuota, getUserQuota(t, userID))
}
