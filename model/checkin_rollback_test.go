package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// SQLite 签到此前是"插入记录 → 加额度 → 失败则删除记录"的手动回滚。
// 删除失败时错误被丢弃：记录留在库里、额度没到账，而 HasCheckedInToday
// 只查记录，用户当天再也签不了到。改为事务后插入与加额度同生共死。
//
// 用触发器挡住 DELETE 来暴露差别：旧实现删不掉记录（且吞掉错误），
// 新实现根本不需要删除——插入随事务一起回滚。
func TestUserCheckinRollsBackInsertWhenQuotaUpdateFails(t *testing.T) {
	db := openTestDB(t, &Checkin{})
	previousDB := DB
	DB = db
	previousSQLite := common.UsingSQLite
	common.UsingSQLite = true
	t.Cleanup(func() {
		DB = previousDB
		common.UsingSQLite = previousSQLite
	})

	// users 表不存在 → 加额度必定失败。
	// checkins 的 DELETE 被触发器阻断 → 手动回滚也会失败。
	require.NoError(t, db.Exec(
		"CREATE TRIGGER block_checkin_delete BEFORE DELETE ON checkins BEGIN SELECT RAISE(ABORT, 'blocked'); END",
	).Error)

	_, err := userCheckinWithoutTransaction(&Checkin{
		UserId:       81,
		CheckinDate:  "2026-10-27",
		QuotaAwarded: 100,
	}, 81, 100)
	require.Error(t, err, "加额度失败必须上报")

	var count int64
	require.NoError(t, db.Model(&Checkin{}).Where("user_id = ?", 81).Count(&count).Error)
	require.Zero(t, count, "额度没到账时不能留下签到记录，否则用户当天无法再次签到")
}
