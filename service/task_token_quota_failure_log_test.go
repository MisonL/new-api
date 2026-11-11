package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// 令牌额度调整发生在资金来源变动之后，无法安全重试；失败只写 stderr 时，
// 钱包与 API key 可用额度之间的缺口没有任何对账依据。失败必须落在计费
// 日志行本身，与 usage_stats_error 同一口径。
//
// 注入方式：在 tokens 表上挂 BEFORE UPDATE 触发器，让扣减/退还语句失败，
// 而读取与其它表不受影响。truncate 只做 DELETE，触发器必须在清理里摘掉。

func installTokenQuotaUpdateFailure(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.Exec(
		`CREATE TRIGGER token_quota_update_fail BEFORE UPDATE ON tokens BEGIN SELECT RAISE(ABORT, 'injected token quota failure'); END;`,
	).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Exec("DROP TRIGGER IF EXISTS token_quota_update_fail").Error)
	})
}

func TestRefundTaskQuotaRecordsTokenQuotaFailureInLog(t *testing.T) {
	truncate(t)
	model.KeyColumn()

	const userId, tokenId, channelId = 71, 71, 71
	seedUser(t, userId, 1_000_000)
	seedToken(t, tokenId, userId, "token-quota-failure-refund-key", 1_000_000)
	seedChannel(t, channelId)

	task := makeTask(userId, channelId, 800, tokenId, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	installTokenQuotaUpdateFailure(t)

	require.NoError(t, RefundTaskQuota(context.Background(), task, "probe failure"))

	logRow := getLastLog(t)
	require.NotNil(t, logRow, "退款必须留下消费日志")
	require.Equal(t, model.LogTypeRefund, logRow.Type)

	other, err := common.StrToMap(logRow.Other)
	require.NoError(t, err)
	require.Contains(t, other, tokenQuotaErrorLogField,
		"令牌额度退还失败必须写进日志行，而不是只写 stderr")
	require.Contains(t, other[tokenQuotaErrorLogField], "injected token quota failure")
}

func TestRecalculateTaskQuotaRecordsTokenQuotaFailureInLog(t *testing.T) {
	truncate(t)
	model.KeyColumn()

	const userId, tokenId, channelId = 72, 72, 72
	const preConsumed = 100
	const actualQuota = 1200
	seedUser(t, userId, 1_000_000)
	seedToken(t, tokenId, userId, "token-quota-failure-settle-key", 1_000_000)
	seedChannel(t, channelId)

	task := makeTask(userId, channelId, preConsumed, tokenId, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	installTokenQuotaUpdateFailure(t)

	require.NoError(t, RecalculateTaskQuota(context.Background(), task, actualQuota, "probe settle"))

	logRow := getLastLog(t)
	require.NotNil(t, logRow, "差额结算必须留下消费日志")
	require.Equal(t, model.LogTypeConsume, logRow.Type)

	other, err := common.StrToMap(logRow.Other)
	require.NoError(t, err)
	require.Contains(t, other, tokenQuotaErrorLogField,
		"令牌额度补扣失败必须写进日志行，而不是只写 stderr")
	require.Contains(t, other[tokenQuotaErrorLogField], "injected token quota failure")
}

func TestRefundMidjourneyTaskQuotaRecordsTokenQuotaFailureInLog(t *testing.T) {
	truncate(t)
	model.KeyColumn()

	const userId, tokenId = 73, 73
	const quota = 25_000
	seedUser(t, userId, 1_000_000-quota)
	seedToken(t, tokenId, userId, "mj-token-quota-failure-key", 1_000_000-quota)

	task := &model.Midjourney{
		UserId: userId,
		Code:   midjourneyTaskCodeSubmitted,
		MjId:   "mj-token-quota-failure",
		Action: "IMAGINE",
		Quota:  quota,
		PrivateData: model.TaskPrivateData{
			TokenId: tokenId,
		},
	}

	installTokenQuotaUpdateFailure(t)

	RefundMidjourneyTaskQuota(nil, task, "probe failure")

	logRow := getLastLog(t)
	require.NotNil(t, logRow, "MJ 退款必须留下日志")
	require.Equal(t, model.LogTypeRefund, logRow.Type)

	other, err := common.StrToMap(logRow.Other)
	require.NoError(t, err)
	require.Contains(t, other, tokenQuotaErrorLogField,
		"MJ 令牌额度退还失败必须写进日志行，而不是只写 stderr")
	require.Contains(t, other[tokenQuotaErrorLogField], "injected token quota failure")
}
