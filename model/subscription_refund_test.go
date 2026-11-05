package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// RefundSubscriptionPreConsume must apply the quota refund and the status
// change atomically. The refund used to run in its own transaction on the root
// handle while the outer transaction kept going; if the outer one then failed,
// the quota was already committed and a retry refunded it again.
//
// This drives the failure by making the final status write fail (a unique-index
// conflict on request_id), then asserts the quota delta was rolled back too.
func TestRefundSubscriptionPreConsumeIsAtomic(t *testing.T) {
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

	record := &SubscriptionPreConsumeRecord{
		RequestId:          "req-atomic-1",
		UserId:             1,
		UserSubscriptionId: sub.Id,
		PreConsumed:        100,
		Status:             "consumed",
	}
	require.NoError(t, db.Create(record).Error)

	// Force the outer transaction's final write to fail: make the record
	// unreadable-as-updatable by dropping the table mid-transaction is not
	// possible, so instead inject a failure via a callback on the record's
	// UPDATE.
	require.NoError(t, db.Callback().Update().Before("gorm:update").
		Register("test:fail_refund_status_write", func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "subscription_pre_consume_records" {
				tx.AddError(gorm.ErrInvalidTransaction)
			}
		}))

	err := RefundSubscriptionPreConsume("req-atomic-1")
	require.Error(t, err, "the injected failure must surface")

	// The quota refund must NOT have been committed.
	var after UserSubscription
	require.NoError(t, db.First(&after, sub.Id).Error)
	require.Equal(t, int64(500), after.AmountUsed,
		"quota was refunded even though the surrounding transaction failed")

	var afterRecord SubscriptionPreConsumeRecord
	require.NoError(t, db.Where("request_id = ?", "req-atomic-1").First(&afterRecord).Error)
	require.Equal(t, "consumed", afterRecord.Status,
		"status must not advance when the transaction rolled back")
}

// 订阅周期重置会把 AmountUsed 归零。重置之后到达的退款（预扣记录创建于
// 上一个周期）此前按负 delta 直接扣减：新周期刚产生的用量被冲掉，负值再被
// 静默夹到 0，退款接口仍返回成功，平台少计费。跨周期的退款只应把记录标记
// 为已退款，不得再调整新周期的用量。
func TestRefundSubscriptionPreConsumeDoesNotConsumePostResetUsage(t *testing.T) {
	db := openTestDB(t, &UserSubscription{}, &SubscriptionPreConsumeRecord{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	const resetAt = int64(1700000000)
	sub := &UserSubscription{
		UserId:        1,
		PlanId:        1,
		AmountTotal:   100000,
		AmountUsed:    100, // 新周期已经产生的用量
		Status:        "active",
		StartTime:     resetAt - 30*86400,
		LastResetTime: resetAt,
	}
	require.NoError(t, db.Create(sub).Error)

	record := &SubscriptionPreConsumeRecord{
		RequestId:          "req-cross-period",
		UserId:             1,
		UserSubscriptionId: sub.Id,
		PreConsumed:        300,
		Status:             "consumed",
	}
	require.NoError(t, db.Create(record).Error)
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).
		Where("id = ?", record.Id).Update("created_at", resetAt-60).Error)

	require.NoError(t, RefundSubscriptionPreConsume("req-cross-period"))

	var after UserSubscription
	require.NoError(t, db.First(&after, sub.Id).Error)
	require.Equal(t, int64(100), after.AmountUsed,
		"跨周期的退款不得冲掉新周期已经产生的用量")

	var afterRecord SubscriptionPreConsumeRecord
	require.NoError(t, db.Where("request_id = ?", "req-cross-period").First(&afterRecord).Error)
	require.Equal(t, "refunded", afterRecord.Status, "记录仍要标记为已退款，避免反复重试")
}

// 反向对照：同一周期内的退款必须照常生效。
func TestRefundSubscriptionPreConsumeStillRefundsWithinOnePeriod(t *testing.T) {
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
		RequestId:          "req-same-period",
		UserId:             1,
		UserSubscriptionId: sub.Id,
		PreConsumed:        300,
		Status:             "consumed",
	}
	require.NoError(t, db.Create(record).Error)
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).
		Where("id = ?", record.Id).Update("created_at", resetAt+60).Error)

	require.NoError(t, RefundSubscriptionPreConsume("req-same-period"))

	var after UserSubscription
	require.NoError(t, db.First(&after, sub.Id).Error)
	require.Equal(t, int64(200), after.AmountUsed)
}
