package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 迁移锁在 PostgreSQL 上走 database/sql 的原生 SQL，占位符必须是 $1。
// 写成 ? 时 pgx 会报 "unused argument: 0"，acquireMigrationLock 返回错误，
// runDBMigration 把错误上交 InitDB，进程直接退出——生产 PG 因此起不来。
// 仅在对 NEW_API_TEST_POSTGRES_DSN 指向一次性库时运行。
func TestAcquireMigrationLockOnPostgres(t *testing.T) {
	dsn := os.Getenv("NEW_API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_POSTGRES_DSN not set; PostgreSQL migration lock not covered by this run")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)

	previous := common.UsingPostgreSQL
	common.UsingPostgreSQL = true
	t.Cleanup(func() { common.UsingPostgreSQL = previous })

	release, acquired, lockErr := acquireMigrationLock(db, "local", false)
	require.NoError(t, lockErr, "PostgreSQL 迁移锁的 SQL 必须能被 pgx 执行")
	require.True(t, acquired)
	require.NotNil(t, release)
	release()

	// 释放后同一进程可以再次拿到锁。
	release2, acquired2, lockErr2 := acquireMigrationLock(db, "local", false)
	require.NoError(t, lockErr2)
	require.True(t, acquired2)
	release2()
}

// 主库与日志库是两个独立库时，两把锁必须互不阻塞：同进程分别获取两者都应
// 成功且可同时持有，从而不存在跨库的环等。仅当 NEW_API_TEST_POSTGRES_DSN
// 与 NEW_API_TEST_POSTGRES_LOG_DSN 指向不同库时运行。
func TestMigrationLockMainAndLogDoNotBlockEachOther(t *testing.T) {
	mainDSN := os.Getenv("NEW_API_TEST_POSTGRES_DSN")
	logDSN := os.Getenv("NEW_API_TEST_POSTGRES_LOG_DSN")
	if mainDSN == "" || logDSN == "" {
		t.Skip("NEW_API_TEST_POSTGRES_DSN / NEW_API_TEST_POSTGRES_LOG_DSN not set")
	}
	mainDB, err := gorm.Open(postgres.New(postgres.Config{DSN: mainDSN, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	logDB, err := gorm.Open(postgres.New(postgres.Config{DSN: logDSN, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)

	previous := common.UsingPostgreSQL
	common.UsingPostgreSQL = true
	t.Cleanup(func() { common.UsingPostgreSQL = previous })

	releaseMain, okMain, errMain := acquireMigrationLock(mainDB, mainDSN, false)
	require.NoError(t, errMain)
	require.True(t, okMain)
	// 在持有主库锁的情况下获取日志库锁：若两者共用一把键，这里在单进程内
	// 仍可重入（advisory lock 同会话可重入）；真正保证不同库互不阻塞的是键
	// 不同，已在 TestMigrationLockIsScopedPerDatabase 覆盖。本测试验证真实
	// 执行路径不报错。
	releaseLog, okLog, errLog := acquireMigrationLock(logDB, logDSN, true)
	require.NoError(t, errLog)
	require.True(t, okLog)

	releaseLog()
	releaseMain()
}
