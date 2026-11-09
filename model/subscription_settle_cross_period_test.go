package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 订阅在该请求的预扣之后发生过周期重置：重置已把 AmountUsed 归零，那笔预扣
// 也随之从新周期的用量口径里消失。此时终态结算的负 delta（实际用量低于预扣）
// 若照旧回落，等于把上个周期的剩余额度变成新周期的免费额度。跨周期的负向
// 结算只应结束这笔差额，不得调整新周期用量。
func TestSettleUserSubscriptionDeltaForRequestDropsCrossPeriodCredit(t *testing.T) {
	db := openTestDB(t, &UserSubscription{}, &SubscriptionPreConsumeRecord{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	const resetAt = int64(1700000000)
	sub := &UserSubscription{
		UserId:        1,
		PlanId:        1,
		AmountTotal:   100000,
		AmountUsed:    100, // 新周期已经产生的真实用量
		Status:        "active",
		StartTime:     resetAt - 30*86400,
		LastResetTime: resetAt,
	}
	require.NoError(t, db.Create(sub).Error)

	record := &SubscriptionPreConsumeRecord{
		RequestId:          "req-settle-cross",
		UserId:             1,
		UserSubscriptionId: sub.Id,
		PreConsumed:        300,
		Status:             "consumed",
	}
	require.NoError(t, db.Create(record).Error)
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).
		Where("id = ?", record.Id).Update("created_at", resetAt-60).Error)

	// 实际消耗 100，低于预扣 300，结算差额 -200 属于上一个周期。
	require.NoError(t, SettleUserSubscriptionDeltaForRequest("req-settle-cross", sub.Id, -200))

	var after UserSubscription
	require.NoError(t, db.First(&after, sub.Id).Error)
	require.Equal(t, int64(100), after.AmountUsed,
		"跨周期的负向结算不得冲掉新周期已经产生的用量")
}

// 同一周期内的负向结算必须照常回落，否则退款被守卫误伤。
func TestSettleUserSubscriptionDeltaForRequestAppliesWithinOnePeriod(t *testing.T) {
	db := openTestDB(t, &UserSubscription{}, &SubscriptionPreConsumeRecord{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	const resetAt = int64(1700000000)
	sub := &UserSubscription{
		UserId:        1,
		PlanId:        1,
		AmountTotal:   100000,
		AmountUsed:    500,
		Status:        "active",
		StartTime:     resetAt - 30*86400,
		LastResetTime: resetAt,
	}
	require.NoError(t, db.Create(sub).Error)

	record := &SubscriptionPreConsumeRecord{
		RequestId:          "req-settle-same",
		UserId:             1,
		UserSubscriptionId: sub.Id,
		PreConsumed:        300,
		Status:             "consumed",
	}
	require.NoError(t, db.Create(record).Error)
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).
		Where("id = ?", record.Id).Update("created_at", resetAt+60).Error)

	require.NoError(t, SettleUserSubscriptionDeltaForRequest("req-settle-same", sub.Id, -200))

	var after UserSubscription
	require.NoError(t, db.First(&after, sub.Id).Error)
	require.Equal(t, int64(300), after.AmountUsed,
		"同周期内的负向结算必须回落")
}

// 跨周期的正向差额是真实发生的超额消耗，必须落账，不能因为跨周期而白送。
func TestSettleUserSubscriptionDeltaForRequestChargesCrossPeriodOverage(t *testing.T) {
	db := openTestDB(t, &UserSubscription{}, &SubscriptionPreConsumeRecord{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	const resetAt = int64(1700000000)
	sub := &UserSubscription{
		UserId:        1,
		PlanId:        1,
		AmountTotal:   100000,
		AmountUsed:    100,
		Status:        "active",
		StartTime:     resetAt - 30*86400,
		LastResetTime: resetAt,
	}
	require.NoError(t, db.Create(sub).Error)

	record := &SubscriptionPreConsumeRecord{
		RequestId:          "req-settle-overage",
		UserId:             1,
		UserSubscriptionId: sub.Id,
		PreConsumed:        300,
		Status:             "consumed",
	}
	require.NoError(t, db.Create(record).Error)
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).
		Where("id = ?", record.Id).Update("created_at", resetAt-60).Error)

	require.NoError(t, SettleUserSubscriptionDeltaForRequest("req-settle-overage", sub.Id, 200))

	var after UserSubscription
	require.NoError(t, db.First(&after, sub.Id).Error)
	require.Equal(t, int64(300), after.AmountUsed,
		"跨周期的正向超额必须落账")
}

// 没有预扣记录（历史任务行、信任旁路等）时保持原行为，不能因为查不到记录而丢结算。
func TestSettleUserSubscriptionDeltaForRequestWithoutRecordKeepsBehavior(t *testing.T) {
	db := openTestDB(t, &UserSubscription{}, &SubscriptionPreConsumeRecord{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	sub := &UserSubscription{
		UserId:      1,
		PlanId:      1,
		AmountTotal: 100000,
		AmountUsed:  500,
		Status:      "active",
	}
	require.NoError(t, db.Create(sub).Error)

	require.NoError(t, SettleUserSubscriptionDeltaForRequest("req-unknown", sub.Id, -200))

	var after UserSubscription
	require.NoError(t, db.First(&after, sub.Id).Error)
	require.Equal(t, int64(300), after.AmountUsed)
}

// 任务失败退款走的是严格路径（PostConsume）：跨周期时同样只能结束这笔差额，
// 否则新周期刚产生的用量会被上个周期的退款冲掉。
func TestPostConsumeUserSubscriptionDeltaForRequestDropsCrossPeriodRefund(t *testing.T) {
	db := openTestDB(t, &UserSubscription{}, &SubscriptionPreConsumeRecord{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	const resetAt = int64(1700000000)
	sub := &UserSubscription{
		UserId:        1,
		PlanId:        1,
		AmountTotal:   100000,
		AmountUsed:    100,
		Status:        "active",
		StartTime:     resetAt - 30*86400,
		LastResetTime: resetAt,
	}
	require.NoError(t, db.Create(sub).Error)

	record := &SubscriptionPreConsumeRecord{
		RequestId:          "req-refund-cross",
		UserId:             1,
		UserSubscriptionId: sub.Id,
		PreConsumed:        300,
		Status:             "consumed",
	}
	require.NoError(t, db.Create(record).Error)
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).
		Where("id = ?", record.Id).Update("created_at", resetAt-60).Error)

	require.NoError(t, PostConsumeUserSubscriptionDeltaForRequest("req-refund-cross", sub.Id, -300))

	var after UserSubscription
	require.NoError(t, db.First(&after, sub.Id).Error)
	require.Equal(t, int64(100), after.AmountUsed,
		"跨周期的任务退款不得冲掉新周期用量")
}
