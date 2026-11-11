package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

// RecalculateTaskQuotaByTokens 曾用 GetGroupGroupRatio(group, group) 查询
// 分组专属倍率：两个参数都是"实际使用分组"，用户所属分组根本没有参与。
// 配置了 group_group_ratio 的站点在这里必然查不中，重算按普通分组倍率
// 补扣/退款，与预扣口径不一致。
func TestRecalculateTaskQuotaByTokensUsesUserGroupSpecialRatio(t *testing.T) {
	truncate(t)
	model.KeyColumn()
	common.BatchUpdateEnabled = false

	const userId = 41
	require.NoError(t, model.DB.Create(&model.User{
		Id: userId, Username: "task_billing_probe", Password: "password123",
		Status: common.UserStatusEnabled, Quota: 1_000_000, Group: "vip",
	}).Error)

	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"task-billing-probe-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"default":0.5}}`))

	const preConsumed = 100
	task := &model.Task{
		ID:     41,
		TaskID: "task-billing-probe-41",
		UserId: userId,
		Group:  "default", // 提交时解析出的实际使用分组
		Quota:  preConsumed,
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{OriginModelName: "task-billing-probe-model"},
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	// vip 用户走 default 组的专属倍率是 0.5：1000 tokens * 1 * 0.5 = 500。
	RecalculateTaskQuotaByTokensWithUsage(nil, task, 1000, 0)

	require.Equal(t, 500, task.Quota,
		"重算必须使用 (用户分组, 使用分组) 查询专属倍率；按 (default, default) 查不中会回落到 1.0 并多扣")
}

// 有计费快照时必须用快照里的分组倍率：那才是预扣实际使用的口径。
// 两者不一致（例如站点在任务提交后改了分组倍率）时，若重算去现查配置，
// 同一笔任务的预扣与结算就会用两套倍率，差额结算凭空产生。
func TestRecalculateTaskQuotaByTokensPrefersFrozenGroupRatio(t *testing.T) {
	truncate(t)
	model.KeyColumn()
	common.BatchUpdateEnabled = false

	const userId = 42
	require.NoError(t, model.DB.Create(&model.User{
		Id: userId, Username: "task_billing_frozen", Password: "password123",
		Status: common.UserStatusEnabled, Quota: 1_000_000, Group: "default",
	}).Error)

	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"task-billing-frozen-model":1}`))
	// 现查配置给的是 2.0，快照冻结的是 0.5。
	// 分组倍率是包级全局表，必须还原：无快照的旧行会现查它，残留的 2.0
	// 会让后续结算用例按两倍计价。
	prevGroupRatio := ratio_setting.GetGroupRatioCopy()
	t.Cleanup(func() {
		payload, err := common.Marshal(prevGroupRatio)
		require.NoError(t, err)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(string(payload)))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2}`))

	const preConsumed = 500
	task := &model.Task{
		ID:     42,
		TaskID: "task-billing-frozen-42",
		UserId: userId,
		Group:  "default",
		Quota:  preConsumed,
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "task-billing-frozen-model",
				ModelRatio:      1,
				GroupRatio:      0.5,
				CompletionRatio: 1,
				// 新行必须显式声明快照存在；旧行没有这个标记时按现查配置处理。
				Frozen: true,
			},
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	RecalculateTaskQuotaByTokensWithUsage(nil, task, 1000, 0)

	require.Equal(t, 500, task.Quota,
		"重算必须沿用提交时冻结的分组倍率，不能按当前配置重新解析")
}
