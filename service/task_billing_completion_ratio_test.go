package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

// RecalculateTaskQuotaByTokens 曾把 totalTokens 整体按输入价计价
// （totalTokens * modelRatio * groupRatio），输出 token 没有乘 completionRatio。
// 任务模型普遍给输出定价 2-8 倍，按整票价重算会系统性少收。
//
// 修复后：调用方提供输入/输出拆分时按拆分计价；只给 totalTokens 时不得再用
// 整体输入价悄悄少收，而是要显式退回（保持预扣额度不变）。
func TestRecalculateTaskQuotaByTokensAppliesCompletionRatio(t *testing.T) {
	truncate(t)
	model.KeyColumn()
	common.BatchUpdateEnabled = false

	const userId = 51
	require.NoError(t, model.DB.Create(&model.User{
		Id: userId, Username: "task_billing_split", Password: "password123",
		Status: common.UserStatusEnabled, Quota: 1_000_000, Group: "default",
	}).Error)

	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"task-billing-split-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"task-billing-split-model":4}`))

	task := &model.Task{
		ID:     51,
		TaskID: "task-billing-split-51",
		UserId: userId,
		Group:  "default",
		Quota:  100,
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{OriginModelName: "task-billing-split-model"},
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	// 输入 1000 + 输出 1000：按 ratio=1、completion=4 应是 1000 + 4000 = 5000。
	require.NoError(t, RecalculateTaskQuotaByTokensWithUsage(nil, task, 1000, 1000))
	require.Equal(t, 5000, task.Quota,
		"输出 token 必须乘 completionRatio；整体按输入价会得出 2000")
}
