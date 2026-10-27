package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// inviteUser 曾用 DB.Save 把整行快照写回：先读全行、在内存里自增邀请字段，
// 再 Save。GORM 的 Save 对带主键的结构体写全部列，读与写之间发生的相对扣减
// （quota = quota - ?）会被写回旧值，等于把并发结算掉的额度白送回去；
// used_quota / request_count / password 也会被一并回滚。
//
// 竞态窗口用「users 表第一次 UPDATE 之前注入一次并发结算」确定性复现：
// 旧实现随后用 Save 写回读到的旧快照，覆盖掉这次扣减；修复后用 SQL 相对
// 更新只触碰 aff_* 列，扣减得以保留。
func TestInviteUserMustNotRevertConcurrentSettlement(t *testing.T) {
	truncateTables(t)

	previous := common.QuotaForInviter
	common.QuotaForInviter = 100
	t.Cleanup(func() { common.QuotaForInviter = previous })

	const userId = 900101
	require.NoError(t, DB.Create(&User{
		Id: userId, Username: "invite_probe", Password: "password123",
		Status: common.UserStatusEnabled, Quota: 1000, UsedQuota: 50,
	}).Error)

	var once sync.Once
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("test:concurrent_settle", func(tx *gorm.DB) {
		if tx.Statement.Table != "users" {
			return
		}
		once.Do(func() {
			// 读与写之间落地的结算，直接跑在同一条连接上。
			_ = tx.Exec(
				"UPDATE users SET quota = quota - 300, used_quota = used_quota + 20 WHERE id = ?",
				userId,
			).Error
		})
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Update().Remove("test:concurrent_settle")
	})

	require.NoError(t, inviteUser(userId))

	var got User
	require.NoError(t, DB.First(&got, "id = ?", userId).Error)
	require.Equal(t, 700, got.Quota, "邀请奖励写入不得把并发扣减掉的额度写回旧值")
	require.Equal(t, 70, got.UsedQuota, "邀请奖励写入不得回滚并发累加的 used_quota")
	require.Equal(t, 1, got.AffCount, "邀请计数必须自增")
	require.Equal(t, 100, got.AffQuota, "邀请额度必须自增")
	require.Equal(t, 100, got.AffHistoryQuota, "邀请历史额度必须自增")
}
