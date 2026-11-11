package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// LOG_SQL_DSN 为空时，日志索引清理跑在主库上。它必须与主迁移共用同一把
// 迁移锁：主迁移因另一 master 持锁被跳过时，本进程不得再对同一库做
// HasIndex→DROP 类 DDL，否则与持锁方的迁移并发（P1-2）。
//
// 可观测面：清理完成后会写入 options 标记。测试持有迁移锁再调用 InitLogDB，
// 修复前清理会照常执行（标记被写入），修复后经 runDBMigration 取锁失败被跳过
// （标记缺席）。SQLite 分支为此使用 TryLock，进程内可模拟「另一 master 持锁」
// 而不会死锁。
func TestLogIndexCleanupSkippedWhenMainMigrationLockHeld(t *testing.T) {
	origDB, origLogDB := DB, LOG_DB
	t.Cleanup(func() { DB, LOG_DB = origDB, origLogDB })
	DB = openTestDB(t, &Log{}, &Option{})
	KeyColumn()
	LOG_DB = DB

	oldMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = oldMaster })
	t.Setenv("LOG_SQL_DSN", "")

	// 前置：标记缺席，清理一旦执行就会写入它。
	marker := gormLogIndexCleanupMarker{db: DB}
	done, err := marker.OptionExists(logIndexCleanupMigrationOptionKey)
	require.NoError(t, err)
	require.False(t, done, "前置条件：清理标记应缺席")

	// 另一个 master 持有迁移锁。
	release, acquired, err := acquireMigrationLock(DB, "", false)
	require.NoError(t, err)
	require.True(t, acquired, "前置条件：测试应能先取得迁移锁")
	defer release()

	require.NoError(t, InitLogDB())

	done, err = marker.OptionExists(logIndexCleanupMigrationOptionKey)
	require.NoError(t, err)
	require.False(t, done,
		"主迁移持锁时，日志索引清理必须被跳过，而不是对同一库并发执行 DDL")
}

// 对照：锁空闲时清理照常执行（保持既有行为）。
func TestLogIndexCleanupRunsWhenLockFree(t *testing.T) {
	origDB, origLogDB := DB, LOG_DB
	t.Cleanup(func() { DB, LOG_DB = origDB, origLogDB })
	DB = openTestDB(t, &Log{}, &Option{})
	KeyColumn()
	LOG_DB = DB

	oldMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = oldMaster })
	t.Setenv("LOG_SQL_DSN", "")

	require.NoError(t, InitLogDB())

	marker := gormLogIndexCleanupMarker{db: DB}
	done, err := marker.OptionExists(logIndexCleanupMigrationOptionKey)
	require.NoError(t, err)
	require.True(t, done, "锁空闲时清理必须照常执行并写入标记")
}
