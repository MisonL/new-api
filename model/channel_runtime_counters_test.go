package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// used_quota / balance 这类由网关运行时累加的列，不能被编辑表单的 payload 覆盖。
// 更新通道来自任意管理端：前端回传的是它打开弹窗时的快照，刷新计数与提交之间
// 可能又有流量经过，快照里的旧值会把累计值改回去；调用方甚至可以伪造一个
// 更大的 used_quota。编辑保存只应改写可编辑字段。
func TestChannelUpdateDoesNotOverwriteRuntimeCounters(t *testing.T) {
	db := openTestDB(t, &Channel{}, &Ability{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	ch := &Channel{
		Id: 701, Name: "runtime-counters", Key: "sk-x", Status: 1,
		Models: "gpt-4", Group: "default", OtherSettings: "{}",
	}
	require.NoError(t, db.Create(ch).Error)
	UpdateChannelUsedQuota(ch.Id, 5000)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", ch.Id).
		Update("balance", 42.5).Error)

	// 编辑表单提交：快照里带着过期（或伪造）的运行时计数。
	edit := &Channel{
		Id: ch.Id, Name: "runtime-counters-renamed", Key: "sk-x", Status: 1,
		Models: "gpt-4", Group: "default", OtherSettings: "{}",
		UsedQuota: 123,
		Balance:   7.5,
	}
	require.NoError(t, edit.Update())

	var after Channel
	require.NoError(t, db.First(&after, ch.Id).Error)
	require.Equal(t, "runtime-counters-renamed", after.Name, "可编辑字段必须更新")
	require.Equal(t, int64(5000), after.UsedQuota,
		"used_quota 是运行时累计值，编辑保存不得覆盖")
	require.InDelta(t, 42.5, after.Balance, 1e-9,
		"balance 是运行时值，编辑保存不得覆盖")
}

// 状态更新走 UpdateChannelStatus：它先读快照、改状态、再 Save 整行。
// 同一窗口里若完成了一次计费，整行 Save 会把 used_quota / balance 写回
// 快照值，把这次计费冲掉。这里用 gorm 回调在 Save 语句执行前向库里再记
// 一笔用量，模拟读快照与保存之间落库的并发计费。
func TestChannelStatusSaveDoesNotOverwriteRuntimeCounters(t *testing.T) {
	db := openTestDB(t, &Channel{}, &Ability{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	ch := &Channel{
		Id: 702, Name: "status-save", Key: "sk-x", Status: 1,
		Models: "gpt-4", Group: "default", OtherSettings: "{}",
	}
	require.NoError(t, db.Create(ch).Error)
	UpdateChannelUsedQuota(ch.Id, 9000)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", ch.Id).
		Update("balance", 33.25).Error)

	injected := false
	require.NoError(t, db.Callback().Update().Before("gorm:update").
		Register("test:charge_between_snapshot_and_save", func(tx *gorm.DB) {
			if injected || tx.Statement == nil || tx.Statement.Schema == nil {
				return
			}
			if tx.Statement.Schema.Table != "channels" {
				return
			}
			injected = true
			// 独立连接写入，避免把本次 Save 自己的语句也改掉。
			if err := db.Exec(
				"UPDATE channels SET used_quota = 9500, balance = 30.75 WHERE id = ?", ch.Id,
			).Error; err != nil {
				tx.AddError(err)
			}
		}))

	require.True(t, UpdateChannelStatus(ch.Id, "", 2, "test-disabled"), "状态更新必须成功")
	require.True(t, injected, "探针必须真的在保存前写入并发计费")

	var after Channel
	require.NoError(t, db.First(&after, ch.Id).Error)
	require.Equal(t, 2, after.Status, "状态更新必须生效")
	require.Equal(t, int64(9500), after.UsedQuota, "used_quota 不得被快照回退")
	require.InDelta(t, 30.75, after.Balance, 1e-9, "balance 不得被快照回退")
}
