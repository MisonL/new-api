package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// Midjourney 提交时 PostConsumeQuota 同时扣了用户钱包与令牌 remain_quota，
// 失败退款却只调 IncreaseUserQuota：用户钱包回补，令牌额度永久减少。
// 通用任务路径用 taskAdjustTokenQuota 有正确实现，MJ 侧是遗漏。
func TestRefundMidjourneyTaskQuotaRestoresTokenQuota(t *testing.T) {
	truncate(t)

	const userId = 31
	const tokenKey = "mj-refund-probe-key"
	const initial = 1_000_000
	const quota = 25_000

	seedUser(t, userId, initial-quota) // 已扣过费：钱包少了 quota
	seedToken(t, userId, userId, tokenKey, initial-quota)

	task := &model.Midjourney{
		UserId: userId,
		Code:   midjourneyTaskCodeSubmitted,
		Quota:  quota,
		PrivateData: model.TaskPrivateData{
			TokenId: userId,
		},
	}
	RefundMidjourneyTaskQuota(nil, task, "probe failure")

	require.Equal(t, initial, readUserQuota(t, userId), "钱包必须退回")
	require.Equal(t, initial, readTokenRemainQuota(t, userId),
		"令牌额度必须一起退回；只退钱包会让 API key 的可用额度永久减少")
}
