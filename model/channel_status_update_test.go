package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// UpdateChannelStatus 原实现先在锁外 GetChannelById 读整行，锁内再整行
// Save（只排除 key/used_quota/balance）。两个后果：
//  1. 读与改不在同一临界区，多密钥渠道并发禁用不同 key 时后写者抹掉先写者；
//  2. 整行 Save 会把管理员在“读→写”窗口内保存的 models/group/settings 等
//     列回滚成旧快照。
// 修复：读-改-写放进同一把 per-channel 锁，写库只更新 status 与 channel_info。

func TestUpdateChannelStatusDoesNotOverwriteConcurrentColumnChanges(t *testing.T) {
	db := openTestDB(t, &Channel{}, &Ability{})
	previousDB := DB
	DB = db
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
	})

	channel := Channel{
		Id:      9001,
		Name:    "concurrent-edit",
		Key:     "sk-test",
		Models:  "model-a",
		Group:   "default",
		Status:  common.ChannelStatusEnabled,
		Other:   "",
		Setting: common.GetPointer(`{"keep":true}`),
	}
	require.NoError(t, db.Create(&channel).Error)

	// 在状态更新的读-写窗口内模拟管理员保存渠道：直接改 models 列。
	var once sync.Once
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(
		"audit:concurrent_channel_edit",
		func(tx *gorm.DB) {
			if tx.Statement.Table != "channels" {
				return
			}
			once.Do(func() {
				require.NoError(t, tx.Exec(
					"UPDATE channels SET models = ? WHERE id = ?",
					"model-a,model-b", channel.Id,
				).Error)
			})
		},
	))

	require.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusAutoDisabled, "test"))

	var reloaded Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.Equal(t, "model-a,model-b", reloaded.Models,
		"状态更新只能写 status/channel_info，不能回滚并发写入的 models 列")
	require.Equal(t, common.ChannelStatusAutoDisabled, reloaded.Status)
	require.NotNil(t, reloaded.Setting)
	require.Equal(t, `{"keep":true}`, *reloaded.Setting)
}

func TestUpdateChannelStatusDisablesChannelWhenAllKeysDisabled(t *testing.T) {
	db := openTestDB(t, &Channel{}, &Ability{})
	previousDB := DB
	DB = db
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
	})

	channel := Channel{
		Id:     9002,
		Name:   "multi-key",
		Key:    "sk-key-0\nsk-key-1",
		Models: "model-a",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, db.Create(&channel).Error)

	require.True(t, UpdateChannelStatus(channel.Id, "sk-key-0", common.ChannelStatusAutoDisabled, "key0 failed"))
	require.True(t, UpdateChannelStatus(channel.Id, "sk-key-1", common.ChannelStatusAutoDisabled, "key1 failed"))

	var reloaded Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.Equal(t, common.ChannelStatusAutoDisabled, reloaded.Status,
		"全部 key 禁用后渠道必须整体禁用")
	require.Equal(t, common.ChannelStatusAutoDisabled, reloaded.ChannelInfo.MultiKeyStatusList[0])
	require.Equal(t, common.ChannelStatusAutoDisabled, reloaded.ChannelInfo.MultiKeyStatusList[1])
}
