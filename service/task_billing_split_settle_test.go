package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

// 终态结算只拿到 total_tokens 时，若不知道输出 token 数，就无法把输出部分
// 按 completionRatio 计价。整体按输入价重算会系统性少收，因此必须保持预扣额度。
func TestSettle_TotalTokensWithoutCompletionSplit_KeepsPreConsumed(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 33, 33, 33
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 7000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-total-only", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"`+task.PrivateData.BillingContext.OriginModelName+`":1}`))

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	require.Equal(t, initQuota, getUserQuota(t, userID), "缺少拆分时不得按 totalTokens 重算")
	require.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	require.Equal(t, preConsumed, task.Quota)
	require.Equal(t, int64(0), countLogs(t))
}

// 有 total 与 completion 拆分时，输出 token 必须乘 completionRatio，
// 不能按输入价一并计算。
func TestSettle_CompletionSplit_ChargesOutputAtCompletionRatio(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 34, 34, 34
	const initQuota, preConsumed = 100000, 1000
	const tokenRemain = 100000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-split-out", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	modelName := task.PrivateData.BillingContext.OriginModelName
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"`+modelName+`":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"`+modelName+`":4}`))
	common.BatchUpdateEnabled = false

	adaptor := &mockAdaptor{adjustReturn: 0}
	// total 2000, completion 1000 => input 1000; 1000*1 + 1000*4 = 5000
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 2000, CompletionTokens: 1000}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	require.Equal(t, 5000, task.Quota, "输出 token 必须按 completionRatio=4 计价")
}
