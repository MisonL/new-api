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

	release, acquired, lockErr := acquireMigrationLock(db, false)
	require.NoError(t, lockErr, "PostgreSQL 迁移锁的 SQL 必须能被 pgx 执行")
	require.True(t, acquired)
	require.NotNil(t, release)
	release()

	// 释放后同一进程可以再次拿到锁。
	release2, acquired2, lockErr2 := acquireMigrationLock(db, false)
	require.NoError(t, lockErr2)
	require.True(t, acquired2)
	release2()
}
