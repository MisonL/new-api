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

// 订阅一旦被终态结算推到超额（used > total），严格路径上的退款必须仍然
// 生效：失败请求的预扣要能退回。原检查对任何 delta 都做 newUsed > total
// 判定，退款算出 1200 > 1000 同样被拒，用户多扣的钱永远退不回来，而且
// 后续预扣也被负剩余挡住，等于双重损失。
func TestPostConsumeRefundAcceptedAfterOverage(t *testing.T) {
	// total = 1000：先用到 900，再超额结算 400 -> 1300。
	sub, cleanup := openSubscriptionSettleDB(t, 900)
	defer cleanup()

	require.NoError(t, SettleUserSubscriptionDelta(sub.Id, 400))
	require.Equal(t, int64(1300), readSubscriptionUsed(t, sub.Id))

	// 另一失败请求退还其预扣：1300 - 200 = 1100，仍高于 total。
	require.NoError(t, PostConsumeUserSubscriptionDelta(sub.Id, -200),
		"超额状态下的退款必须被接受，否则用户多扣的额度无法退回")
	require.Equal(t, int64(1100), readSubscriptionUsed(t, sub.Id))
}

// 超额状态下的全额退款（退到仍高于 total）同样必须接受。
func TestPostConsumePartialRefundStillAboveTotalAccepted(t *testing.T) {
	sub, cleanup := openSubscriptionSettleDB(t, 100)
	defer cleanup()

	require.NoError(t, SettleUserSubscriptionDelta(sub.Id, 1200))
	require.Equal(t, int64(1300), readSubscriptionUsed(t, sub.Id))

	require.NoError(t, PostConsumeUserSubscriptionDelta(sub.Id, -100))
	require.Equal(t, int64(1200), readSubscriptionUsed(t, sub.Id),
		"退款后仍高于 total 也必须落账")
}
