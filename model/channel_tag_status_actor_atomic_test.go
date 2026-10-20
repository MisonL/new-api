package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 生产入口 DisableTagChannels/EnableTagChannels 调用的是 WithActor 版本。
// 它必须和标签级事务一样，把渠道状态与 ability 写在同一事务里，并且只在
// 提交后逐渠道写审计。若渠道更新先逐条提交、ability 写失败，渠道已下线而
// 能力仍在，审计还会记录一次并未发生的变更。
func TestDisableChannelByTagWithActorRollsBackAndAuditsOnCommit(t *testing.T) {
	prepareStatusAuditTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	tag := "tag-status-actor-atomic"
	ids := []int{97101, 97102}
	seedTagStatusChannels(t, tag, ids)

	actor := ChannelStatusActor{Source: "tag_disable"}

	const callbackName = "test:fail_ability_write_for_actor_path"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "abilities" {
			tx.AddError(gorm.ErrInvalidTransaction)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callbackName) })

	err := DisableChannelByTagWithActor(tag, actor)
	require.Error(t, err, "the injected ability-write failure must surface")

	for _, id := range ids {
		var channel Channel
		require.NoError(t, DB.First(&channel, id).Error)
		require.Equal(t, common.ChannelStatusEnabled, channel.Status,
			"channel %d was disabled although the ability write failed", id)
		require.Empty(t, auditEntriesForChannel(t, id),
			"no audit entry may be written for a rolled-back change")
	}

	require.NoError(t, DB.Callback().Update().Remove(callbackName))

	require.NoError(t, DisableChannelByTagWithActor(tag, actor))
	for _, id := range ids {
		var channel Channel
		require.NoError(t, DB.First(&channel, id).Error)
		require.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)
		require.Len(t, auditEntriesForChannel(t, id), 1,
			"each changed channel must get exactly one audit entry")
	}
}
