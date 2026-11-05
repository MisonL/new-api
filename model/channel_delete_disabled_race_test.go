package model

import (
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// DeleteDisabledChannel 先 Pluck 再按 id 删除：两次读取之间管理员重新启用某个
// 渠道时，旧实现仍会按过期 id 列表把它删掉（连同 ability）。删除语句必须重新
// 断言 status，并且只清理真正被删掉的渠道的 ability。
func TestDeleteDisabledChannelSkipsChannelReEnabledAfterPluck(t *testing.T) {
	prepareChannelCacheTest(t)

	tag := "reenable-race"
	priority := int64(10)
	const keepID = 93001
	const removeID = 93002
	for _, id := range []int{keepID, removeID} {
		require.NoError(t, DB.Create(&Channel{
			Id: id, Name: tag, Status: common.ChannelStatusManuallyDisabled,
			Group: "default", Models: "reenable-race-model",
			Tag: &tag, Priority: &priority, Weight: common.GetPointer(uint(1)),
		}).Error)
		require.NoError(t, DB.Create(&Ability{
			Group: "default", Model: "reenable-race-model", ChannelId: id,
			Enabled: true, Priority: &priority, Weight: 1, Tag: &tag,
		}).Error)
	}

	// 在渠道删除语句执行前，把 keepID 重新启用，模拟管理员的并发操作。
	// 使用 tx 上的新 session，保持同一事务/连接，避免 SQLite 跨连接写锁。
	var reEnabled atomic.Bool
	const callbackName = "test:reenable_before_channel_delete"
	require.NoError(t, DB.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "channels" {
			return
		}
		if !reEnabled.CompareAndSwap(false, true) {
			return
		}
		_ = tx.Session(&gorm.Session{NewDB: true}).
			Model(&Channel{}).
			Where("id = ?", keepID).
			Update("status", common.ChannelStatusEnabled).Error
	}))
	t.Cleanup(func() { _ = DB.Callback().Delete().Remove(callbackName) })

	deleted, err := DeleteDisabledChannel()
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted, "只应删除仍处于禁用状态的渠道")

	var kept Channel
	require.NoError(t, DB.Where("id = ?", keepID).First(&kept).Error)
	require.Equal(t, common.ChannelStatusEnabled, kept.Status,
		"Pluck 之后被重新启用的渠道不得被删除")

	var keptAbilities int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", keepID).Count(&keptAbilities).Error)
	require.Equal(t, int64(1), keptAbilities,
		"存活渠道的 ability 不得被清理")

	var removedCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", removeID).Count(&removedCount).Error)
	require.Equal(t, int64(0), removedCount)
	var removedAbilities int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", removeID).Count(&removedAbilities).Error)
	require.Equal(t, int64(0), removedAbilities)
}

// 批量删除必须分块：一次 IN 列表过大时 SQLite 的绑定变量上限、MySQL/PG 的
// 语句大小都会触发失败。205 个渠道跨过 200 的分块边界，必须全部删净。
func TestDeleteDisabledChannelHandlesMultipleChunks(t *testing.T) {
	prepareChannelCacheTest(t)

	const count = 205
	tag := "delete-disabled-chunked"
	priority := int64(10)
	for i := 0; i < count; i++ {
		id := 94000 + i
		require.NoError(t, DB.Create(&Channel{
			Id: id, Name: tag, Status: common.ChannelStatusAutoDisabled,
			Group: "default", Models: "delete-disabled-chunked-model",
			Tag: &tag, Priority: &priority, Weight: common.GetPointer(uint(1)),
		}).Error)
		require.NoError(t, DB.Create(&Ability{
			Group: "default", Model: "delete-disabled-chunked-model", ChannelId: id,
			Enabled: true, Priority: &priority, Weight: 1, Tag: &tag,
		}).Error)
	}

	deleted, err := DeleteDisabledChannel()
	require.NoError(t, err)
	require.Equal(t, int64(count), deleted)

	var channelCount, abilityCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("status IN ?", []int{common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled}).Count(&channelCount).Error)
	require.Equal(t, int64(0), channelCount)
	require.NoError(t, DB.Model(&Ability{}).Where("model = ?", "delete-disabled-chunked-model").Count(&abilityCount).Error)
	require.Equal(t, int64(0), abilityCount, "跨分块的批量删除留下了 ability")
}
