package model

import (
	"testing"

	"github.com/stretchr/testify/require"
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
