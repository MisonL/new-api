package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// A deployment may point the log database at a different engine than the main
// one, and the supported shape of that is a PostgreSQL main database with a
// MySQL log database. The column quoting cannot follow the main database then:
// MySQL does not treat "group" as an identifier unless ANSI_QUOTES is set, so a
// double-quoted name is a string literal there and the query either errors or
// silently compares a constant.
//
// initCol's LOG_SQL_DSN branch only special-cased a PostgreSQL log database and
// otherwise copied the main database's quoting, so main=PG + log=MySQL produced
// `"group"` for a MySQL log database.

// withInitColGlobals saves and restores everything initCol reads or writes, so
// a test can drive it without leaking state into the rest of the package.
func withInitColGlobals(t *testing.T, usingPostgreSQL bool, logSQLType string) {
	t.Helper()

	prevUsingPostgreSQL := common.UsingPostgreSQL
	prevUsingMySQL := common.UsingMySQL
	prevUsingSQLite := common.UsingSQLite
	prevLogSQLType := common.LogSqlType
	prevCommonGroupCol := commonGroupCol
	prevCommonKeyCol := commonKeyCol
	prevLogGroupCol := logGroupCol

	t.Cleanup(func() {
		common.UsingPostgreSQL = prevUsingPostgreSQL
		common.UsingMySQL = prevUsingMySQL
		common.UsingSQLite = prevUsingSQLite
		common.LogSqlType = prevLogSQLType
		commonGroupCol = prevCommonGroupCol
		commonKeyCol = prevCommonKeyCol
		logGroupCol = prevLogGroupCol
	})

	common.UsingPostgreSQL = usingPostgreSQL
	common.UsingMySQL = !usingPostgreSQL
	common.UsingSQLite = false
	common.LogSqlType = logSQLType
}

// TestLogGroupColFollowsTheLogDatabaseDialect is the regression: a MySQL log
// database must be quoted the MySQL way even when the main database is
// PostgreSQL.
func TestLogGroupColFollowsTheLogDatabaseDialect(t *testing.T) {
	t.Setenv("LOG_SQL_DSN", "root:pass@tcp(127.0.0.1:3306)/new-api-log")
	withInitColGlobals(t, true, common.DatabaseTypeMySQL)

	initCol()

	require.Equal(t, "`group`", logGroupCol,
		"日志库是 MySQL 时分组列必须用反引号，用主库 PostgreSQL 的双引号会被当成字符串字面量")
}

// TestLogGroupColKeepsPostgreSQLQuotingForPostgreSQLLog covers the branch that
// was already correct, so the fix does not trade one dialect for the other.
func TestLogGroupColKeepsPostgreSQLQuotingForPostgreSQLLog(t *testing.T) {
	t.Setenv("LOG_SQL_DSN", "host=127.0.0.1 user=root dbname=new-api-log")
	withInitColGlobals(t, false, common.DatabaseTypePostgreSQL)

	initCol()

	require.Equal(t, `"group"`, logGroupCol,
		"日志库是 PostgreSQL 时分组列必须用双引号")
}

// TestLogGroupColFollowsMainDatabaseWhenNoLogDSN pins the other branch: with no
// separate log database the log columns and the main columns must agree, since
// they address the same database.
func TestLogGroupColFollowsMainDatabaseWhenNoLogDSN(t *testing.T) {
	t.Setenv("LOG_SQL_DSN", "")
	withInitColGlobals(t, true, common.DatabaseTypeSQLite)

	initCol()

	require.Equal(t, commonGroupCol, logGroupCol,
		"未配置独立日志库时，日志列引用必须与主库一致")
	require.Equal(t, `"group"`, logGroupCol)
}

// TestLogGroupColNeverCopiesMainDialectForMySQLLog is the general form of the
// regression: whatever the main database is, a MySQL log database gets MySQL
// quoting.
func TestLogGroupColNeverCopiesMainDialectForMySQLLog(t *testing.T) {
	for _, mainIsPostgreSQL := range []bool{true, false} {
		name := "main=mysql"
		if mainIsPostgreSQL {
			name = "main=postgres"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("LOG_SQL_DSN", "root:pass@tcp(127.0.0.1:3306)/new-api-log")
			withInitColGlobals(t, mainIsPostgreSQL, common.DatabaseTypeMySQL)

			initCol()

			require.Equal(t, "`group`", logGroupCol)
		})
	}
}

// TestInitColDoesNotPanicWithoutLogDSN is a cheap guard that the env lookup
// stays optional.
func TestInitColDoesNotPanicWithoutLogDSN(t *testing.T) {
	require.NoError(t, os.Unsetenv("LOG_SQL_DSN"))
	withInitColGlobals(t, false, common.DatabaseTypeMySQL)

	initCol()

	require.NotEmpty(t, logGroupCol)
}
