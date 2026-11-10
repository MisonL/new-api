package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 主库与日志库可以是两个独立的同引擎实例。若两者共用同一把锁键，进程 A
// 持日志库锁等主库锁、进程 B 持主库锁等日志库锁，会形成环等，两个进程都
// 卡在启动。锁键/锁名必须按目标库区分，同一库内的多个 master 仍互斥。
func TestMigrationLockIsScopedPerDatabase(t *testing.T) {
	mainDSN := "postgres://u:p@db-main:5432/newapi?sslmode=disable"
	logDSN := "postgres://u:p@db-log:5432/newapi-logs?sslmode=disable"

	mainKey := migrationAdvisoryLockKeyFor(mainDSN)
	logKey := migrationAdvisoryLockKeyFor(logDSN)
	require.NotEqual(t, mainKey, logKey, "不同数据库实例的 advisory lock 键必须不同")
	require.Equal(t, mainKey, migrationAdvisoryLockKeyFor(mainDSN), "同一 DSN 必须稳定得到同一个键")

	require.NotEqual(t, migrationLockNameFor(mainDSN), migrationLockNameFor(logDSN),
		"不同数据库实例的 MySQL 锁名必须不同")
	require.Equal(t, migrationLockNameFor(mainDSN), migrationLockNameFor(mainDSN))
}
