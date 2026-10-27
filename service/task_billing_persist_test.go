package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// 差额结算只改了内存里的 task.Quota，数据库行仍是预扣时的金额。
// 任务列表（TaskDto.Quota）展示的是库里的值，用户看到的扣费与实际
// 结算额不一致；任何按库中 Quota 计算后续动作的路径也会拿到旧值。
func TestRecalculateTaskQuotaPersistsSettledQuota(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 51, 51, 51
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000 // 补扣 1000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-persist", 5000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	stored, exists, err := model.GetByTaskId(userID, task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, actualQuota, stored.Quota,
		"差额结算后必须把结算额度写回任务行，否则列表展示的仍是预扣金额")
}
