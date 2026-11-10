package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 超过 7 天的已消费预扣记录不应被清理：它们仍然需要用于跨周期结算守卫。
// 清理只应针对已退款（refunded）的幂等记录。
func TestCleanupSubscriptionPreConsumeRecordsRefusesConsumedRecords(t *testing.T) {
	db := openTestDB(t, &SubscriptionPreConsumeRecord{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	// 创建一条 10 天前的消费记录
	record := &SubscriptionPreConsumeRecord{
		RequestId:          "old-consumed",
		UserId:             1,
		UserSubscriptionId: 1,
		PreConsumed:        100,
		Status:             "consumed",
	}
	require.NoError(t, db.Create(record).Error)
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).
		Where("id = ?", record.Id).Update("updated_at", GetDBTimestamp()-10*86400).Error)

	CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)

	var count int64
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).Count(&count).Error)
	require.Equal(t, int64(1), count, "已消费的记录不能被清理")
}

// 已退款的幂等记录应被清理以减少表大小。
func TestCleanupSubscriptionPreConsumeRecordsDeletesOldRefunded(t *testing.T) {
	db := openTestDB(t, &SubscriptionPreConsumeRecord{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	record := &SubscriptionPreConsumeRecord{
		RequestId:          "old-refunded",
		UserId:             1,
		UserSubscriptionId: 1,
		PreConsumed:        100,
		Status:             "refunded",
	}
	require.NoError(t, db.Create(record).Error)
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).
		Where("id = ?", record.Id).Update("updated_at", GetDBTimestamp()-10*86400).Error)

	affected, err := CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)
	require.NoError(t, err)
	require.Equal(t, int64(1), affected, "已退款的旧记录应被清理")
}

// 最近的已退款记录不应被清理（保护短 TTL 场景）。
func TestCleanupSubscriptionPreConsumeRecordsKeepsRecentRefunded(t *testing.T) {
	db := openTestDB(t, &SubscriptionPreConsumeRecord{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	record := &SubscriptionPreConsumeRecord{
		RequestId:          "recent-refunded",
		UserId:             1,
		UserSubscriptionId: 1,
		PreConsumed:        100,
		Status:             "refunded",
	}
	require.NoError(t, db.Create(record).Error)

	affected, err := CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)
	require.NoError(t, err)
	require.Equal(t, int64(0), affected, "近期的已退款记录不应被清理")
}
