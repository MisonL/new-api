package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// 上游已经交付、但终态结算失败被回退的任务，超时清扫与本地失败标记都不得
// 按普通失败整笔退款——那会把已经完成的任务变成免费。这里钉住资金方向。
func TestSweepTimedOutTasksSkipsRefundForUpstreamSucceededTask(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userId = 21
	seedUser(t, userId, 900) // 已预扣 100

	task := makeTask(userId, 0, 100, 0, "wallet", 0)
	task.TaskID = "task-upstream-succeeded-timeout"
	task.SubmitTime = time.Now().Add(-2 * time.Hour).Unix()
	task.PrivateData.UpstreamSucceeded = true
	require.NoError(t, model.DB.Create(task).Error)

	restore := setTaskTimeoutForTest(t, 60)
	defer restore()

	sweepTimedOutTasks(context.Background())

	require.Equal(t, 900, readUserQuota(t, userId),
		"上游已成功的任务不得整笔退款；预扣应保留待人工核对该收的差额")

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status,
		"超时后仍应结束轮询，避免每轮重复请求上游")
	require.Contains(t, reloaded.FailReason, "结算",
		"失败原因必须点明是结算待核对，而不是普通超时")
}

// 对照：普通超时任务（上游未报告成功）必须照旧整笔退款。
func TestSweepTimedOutTasksStillRefundsOrdinaryTimedOutTask(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userId = 22
	seedUser(t, userId, 900)

	task := makeTask(userId, 0, 100, 0, "wallet", 0)
	task.TaskID = "task-ordinary-timeout"
	task.SubmitTime = time.Now().Add(-2 * time.Hour).Unix()
	require.NoError(t, model.DB.Create(task).Error)

	restore := setTaskTimeoutForTest(t, 60)
	defer restore()

	sweepTimedOutTasks(context.Background())

	require.Equal(t, 1000, readUserQuota(t, userId),
		"上游未交付的任务超时后必须整笔退款")
}

// 结算失败把成功终态回退时，必须同时落下「上游已成功」标记，否则后续
// 超时清扫无从区分。
func TestRevertTaskTerminalTransitionMarksUpstreamSucceeded(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userId = 23
	seedUser(t, userId, 900)

	task := makeTask(userId, 0, 100, 0, "wallet", 0)
	task.TaskID = "task-revert-success"
	task.SubmitTime = time.Now().Add(-10 * time.Minute).Unix()
	require.NoError(t, model.DB.Create(task).Error)

	snap := task.Snapshot()
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.FinishTime = time.Now().Unix()
	require.NoError(t, model.DB.Model(task).Where("id = ?", task.ID).
		Updates(map[string]any{"status": model.TaskStatusSuccess, "progress": "100%"}).Error)

	RevertTaskTerminalTransition(context.Background(), task, model.TaskStatusSuccess, snap)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloaded.Status,
		"回退后应回到未完成状态等待重试")
	require.True(t, reloaded.PrivateData.UpstreamSucceeded,
		"回退成功终态必须落下上游已成功标记")
	require.Equal(t, 1, reloaded.PrivateData.BillingRetryCount,
		"回退必须计入重试次数，避免无限重试")
}

func setTaskTimeoutForTest(t *testing.T, minutes int) func() {
	t.Helper()
	old := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = minutes
	return func() { constant.TaskTimeoutMinutes = old }
}