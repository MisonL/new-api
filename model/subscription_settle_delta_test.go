package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 订阅额度的准入由 PreConsumeUserSubscription 在请求开始时把关（remain <
// amount 即拒绝）。逐 turn 的 Reserve 与终态结算共用同一个 delta 原语时，
// 若把超额一律当错误返回，会出现两种后果：
//   - 会话中途 Reserve 失败：本意是提醒「订阅已用尽，应停止」，需要保留；
//   - 请求结束后的终态结算失败：请求已经跑完，整笔差额被丢弃，等于白送。
//
// 因此终态结算使用允许超额的变体：把真实用量记进 AmountUsed，由剩余额度
// 变负来体现超额，而不是拒绝落账。

func openSubscriptionSettleDB(t *testing.T, amountUsed int64) (*UserSubscription, func()) {
	t.Helper()
	db := openTestDB(t, &UserSubscription{})
	previous := DB
	DB = db

	sub := &UserSubscription{
		UserId:      1,
		PlanId:      1,
		AmountTotal: 1000,
		AmountUsed:  amountUsed,
		Status:      "active",
	}
	require.NoError(t, db.Create(sub).Error)
	return sub, func() { DB = previous }
}

func readSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub UserSubscription
	require.NoError(t, DB.Select("amount_used").Where("id = ?", id).First(&sub).Error)
	return sub.AmountUsed
}

func TestSettleUserSubscriptionDeltaRecordsOverage(t *testing.T) {
	sub, cleanup := openSubscriptionSettleDB(t, 900)
	defer cleanup()

	require.NoError(t, SettleUserSubscriptionDelta(sub.Id, 500),
		"终态结算不得因超出总额而整笔失败")

	require.Equal(t, int64(1400), readSubscriptionUsed(t, sub.Id),
		"真实用量必须完整落账，超额由剩余额度变负体现")
}

func TestPostConsumeUserSubscriptionDeltaStillRejectsOverage(t *testing.T) {
	sub, cleanup := openSubscriptionSettleDB(t, 900)
	defer cleanup()

	err := PostConsumeUserSubscriptionDelta(sub.Id, 500)
	require.Error(t, err, "中途 Reserve 必须保留超额拒绝，作为停止会话的信号")
	require.Equal(t, int64(900), readSubscriptionUsed(t, sub.Id))
}

func TestSubscriptionDeltaRefundClampsAtZero(t *testing.T) {
	sub, cleanup := openSubscriptionSettleDB(t, 100)
	defer cleanup()

	require.NoError(t, SettleUserSubscriptionDelta(sub.Id, -500))
	require.Equal(t, int64(0), readSubscriptionUsed(t, sub.Id),
		"退款不得把已用额度减成负数")

	require.NoError(t, PostConsumeUserSubscriptionDelta(sub.Id, -500))
	require.Equal(t, int64(0), readSubscriptionUsed(t, sub.Id))
}

func TestSettleUserSubscriptionDeltaUnlimitedDoesNotReject(t *testing.T) {
	db := openTestDB(t, &UserSubscription{})
	previous := DB
	DB = db
	defer func() { DB = previous }()

	sub := &UserSubscription{UserId: 2, PlanId: 1, AmountTotal: 0, AmountUsed: 0, Status: "active"}
	require.NoError(t, db.Create(sub).Error)

	require.NoError(t, SettleUserSubscriptionDelta(sub.Id, 5000))
	require.Equal(t, int64(5000), readSubscriptionUsed(t, sub.Id))
}
