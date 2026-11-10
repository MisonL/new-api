package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
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
