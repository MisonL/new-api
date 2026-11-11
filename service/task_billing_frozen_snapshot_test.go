package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

// 任务提交后可能运行数分钟到数小时。轮询期结算若现查倍率，管理员在任务
// 运行期间改配置就会让同一笔任务的预扣与结算用两套价格。快照里的值必须
// 优先，且 0 是合法倍率：未定价模型靠它 fail-closed，分组倍率 0 表示该组免费。
// 因此"有没有快照"必须由显式标记表达，不能用数值大小推断。

func seedFrozenSnapshotUser(t *testing.T, userId int, username string) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{
		Id: userId, Username: username, Password: "password123",
		Status: common.UserStatusEnabled, Quota: 1_000_000, Group: "default",
	}).Error)
}

// 冻结的补全倍率必须优先于实时配置。
func TestRecalculateTaskQuotaPrefersFrozenCompletionRatio(t *testing.T) {
	truncate(t)
	model.KeyColumn()
	common.BatchUpdateEnabled = false

	const userId = 61
	seedFrozenSnapshotUser(t, userId, "task_frozen_completion")
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"task-frozen-completion-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	// 实时配置是 4，提交时冻结的是 2。
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"task-frozen-completion-model":4}`))

	task := &model.Task{
		ID:     61,
		TaskID: "task-frozen-completion-61",
		UserId: userId,
		Group:  "default",
		Quota:  3000,
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "task-frozen-completion-model",
				ModelRatio:      1,
				GroupRatio:      1,
				CompletionRatio: 2,
				Frozen:          true,
			},
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	// 冻结口径：1000 + 1000*2 = 3000；实时配置会算出 5000。
	require.NoError(t, RecalculateTaskQuotaByTokensWithUsage(nil, task, 1000, 1000))
	require.Equal(t, 3000, task.Quota,
		"结算必须使用提交时冻结的补全倍率，不能回查实时配置")
}

// 冻结的 0 补全倍率是合法值：输出免费，不能被替换成默认 1。
func TestRecalculateTaskQuotaHonorsFrozenZeroCompletionRatio(t *testing.T) {
	truncate(t)
	model.KeyColumn()
	common.BatchUpdateEnabled = false

	const userId = 62
	seedFrozenSnapshotUser(t, userId, "task_frozen_zero_completion")
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"task-frozen-zero-completion-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"task-frozen-zero-completion-model":4}`))

	task := &model.Task{
		ID:     62,
		TaskID: "task-frozen-zero-completion-62",
		UserId: userId,
		Group:  "default",
		Quota:  2000,
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "task-frozen-zero-completion-model",
				ModelRatio:      1,
				GroupRatio:      1,
				CompletionRatio: 0,
				Frozen:          true,
			},
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	// 冻结口径：1000 + 1000*0 = 1000，退款 1000；实时配置会算出 5000。
	require.NoError(t, RecalculateTaskQuotaByTokensWithUsage(nil, task, 1000, 1000))
	require.Equal(t, 1000, task.Quota,
		"冻结的 0 补全倍率必须原样生效")
}

// 冻结的 0 分组倍率表示该分组免费，结算应全额退款而不是保持预扣。
func TestRecalculateTaskQuotaHonorsFrozenZeroGroupRatio(t *testing.T) {
	truncate(t)
	model.KeyColumn()
	common.BatchUpdateEnabled = false

	const userId = 63
	seedFrozenSnapshotUser(t, userId, "task_frozen_zero_group")
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"task-frozen-zero-group-model":1}`))
	// 实时配置是 1，提交时冻结的是 0（免费组）。
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	task := &model.Task{
		ID:     63,
		TaskID: "task-frozen-zero-group-63",
		UserId: userId,
		Group:  "default",
		Quota:  1000,
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "task-frozen-zero-group-model",
				ModelRatio:      1,
				GroupRatio:      0,
				CompletionRatio: 1,
				Frozen:          true,
			},
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuotaByTokensWithUsage(nil, task, 1000, 0))
	require.Equal(t, 0, task.Quota,
		"冻结的 0 分组倍率必须让任务结算为 0 并全额退款")
}
