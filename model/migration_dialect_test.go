package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 日志库可以与主库不同（LOG_SQL_DSN 指向另一个实例/另一种数据库）。锁方言
// 必须按目标库自身判定，否则主库 MySQL、日志库 PostgreSQL 的组合会在日志库
// 上执行 MySQL 的 GET_LOCK，启动失败。这里验证方言判定的取值来源。
func TestMigrationDialectFollowsTargetDatabase(t *testing.T) {
	prevUsingPostgreSQL, prevUsingMySQL := common.UsingPostgreSQL, common.UsingMySQL
	prevLogSQLType := common.LogSqlType
	t.Cleanup(func() {
		common.UsingPostgreSQL, common.UsingMySQL = prevUsingPostgreSQL, prevUsingMySQL
		common.LogSqlType = prevLogSQLType
	})

	// 主库 MySQL，日志库独立 PostgreSQL。
	common.UsingPostgreSQL = false
	common.UsingMySQL = true
	common.LogSqlType = common.DatabaseTypePostgreSQL

	require.Equal(t, migrationDialectMySQL, migrationDialect(false), "主库应为 MySQL")
	require.Equal(t, migrationDialectPostgreSQL, migrationDialect(true), "日志库应为 PostgreSQL")

	// 日志库与主库同为 MySQL。
	common.LogSqlType = common.DatabaseTypeMySQL
	require.Equal(t, migrationDialectMySQL, migrationDialect(true))

	// 日志库 SQLite。
	common.LogSqlType = common.DatabaseTypeSQLite
	require.Equal(t, migrationDialectSQLite, migrationDialect(true))
}

// 回归：runDBMigration 必须把调用方的 isLog 透传给 migrationDialect。
// 只测 migrationDialect 本身挡不住调用点把 true 写成 false——那样日志库会
// 按主库方言选锁，主库 MySQL + 日志库 PostgreSQL 的组合就会在日志库上执行
// GET_LOCK。这里经 runDBMigration 传入日志库 DSN，断言选中的是 PG 锁分支。
func TestAcquireMigrationLockHonorsLogDialect(t *testing.T) {
	prevUsingPostgreSQL, prevUsingMySQL := common.UsingPostgreSQL, common.UsingMySQL
	prevLogSQLType := common.LogSqlType
	t.Cleanup(func() {
		common.UsingPostgreSQL, common.UsingMySQL = prevUsingPostgreSQL, prevUsingMySQL
		common.LogSqlType = prevLogSQLType
	})

	// 主库按 SQLite 处理（两个 engine 标志都为 false），日志库是 PostgreSQL。
	common.UsingPostgreSQL = false
	common.UsingMySQL = false
	common.LogSqlType = common.DatabaseTypePostgreSQL

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 必须经 runDBMigration 调用，这样调用点把 isLog 写成 false 也会被拦住。
	// 走 PG 分支时会在 sqlite 连接上执行 pg_try_advisory_lock，必然报错；走
	// SQLite 分支则直接拿到进程锁、migrate 正常执行。
	migrated := false
	err = runDBMigration("log-dialect", db, "postgres://u:p@split-dialect:5432/logs", true, func() error {
		migrated = true
		return nil
	})
	require.Error(t, err, "日志库为 PostgreSQL 时必须走 PG 锁分支（这里会在 sqlite 上执行失败）")
	require.Contains(t, err.Error(), "advisory_lock", "错误应来自 pg_try_advisory_lock")
	require.False(t, migrated, "拿锁失败时不得执行迁移")
}
