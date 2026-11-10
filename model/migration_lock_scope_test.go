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

// 同一个目标库可以有多种等价 DSN 写法（scheme 别名、ssl 参数、凭据轮换）。
// 它们必须得到同一个锁键，否则连接同一库的多个 master 会各拿一把锁、并发
// 执行迁移。注意 PG 的默认端口与 postgres:// 的默认值相同，要按同一身份处
// 理；MySQL DSN 用 url.Parse 无法解析，按连接目标再做一次规范化。
func TestMigrationLockIdentityNormalizesEquivalentDSNs(t *testing.T) {
	groups := [][]string{
		{
			"postgres://u:p@db:5432/newapi?sslmode=disable",
			"postgresql://u:p@db:5432/newapi?sslmode=disable",
			"postgres://u:p@db:5432/newapi?sslmode=require",
			"postgres://u:p@db:5432/newapi",
			"postgres://other:secret@db:5432/newapi?sslmode=disable",
			"postgres://u:p@db:5432/newapi?sslmode=disable&application_name=master-a",
		},
		{
			"u:p@tcp(db:3306)/newapi?parseTime=true",
			"u2:p2@tcp(db:3306)/newapi?parseTime=true&charset=utf8mb4",
			"u@tcp(db:3306)/newapi",
		},
	}
	for _, group := range groups {
		want := migrationLockIdentity(group[0])
		for _, dsn := range group[1:] {
			require.Equal(t, want, migrationLockIdentity(dsn),
				"%q 与 %q 指向同一库，必须得到同一锁身份", dsn, group[0])
		}
	}
	// 不同库必须区分开。
	require.NotEqual(t,
		migrationLockIdentity("postgres://u:p@db:5432/newapi"),
		migrationLockIdentity("postgres://u:p@db:5432/newapi_logs"))
	require.NotEqual(t,
		migrationLockIdentity("u:p@tcp(db:3306)/newapi"),
		migrationLockIdentity("u:p@tcp(db2:3306)/newapi"))
	require.NotEqual(t,
		migrationLockIdentity("u:p@tcp(db:3306)/newapi"),
		migrationLockIdentity("u:p@tcp(db:3307)/newapi"))
}
