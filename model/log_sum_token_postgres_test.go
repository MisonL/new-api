package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SumUsedToken 的聚合 SQL 必须在三个方言上都能执行。此前它用 MySQL/SQLite
// 才有的 ifnull，PostgreSQL 直接报 SQLSTATE 42883。
//
// SumUsedToken 本身丢弃 Scan 错误，方言不兼容同样返回 0，所以"返回 0"不能
// 证明 SQL 成功。测试针对构造查询的函数断言，能直接拿到错误。
//
// SQLite 在默认测试库上验证；PostgreSQL 需要 NEW_API_POSTGRES_TEST_DSN。
func TestSumUsedTokenQueryRunsOnSQLite(t *testing.T) {
	db := openTestDB(t, &Log{})

	var probe int64
	require.NoError(t,
		sumUsedTokenQuery(db, 0, 0, "", "", "").Scan(&probe).Error,
	)
	require.Equal(t, 0, SumUsedToken(0, 0, 0, "", "", ""))
}

func TestSumUsedTokenQueryRunsOnPostgres(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL check")
	}

	db := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, db.AutoMigrate(&Log{}))

	oldLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = oldLogDB })

	// 用不可能命中数据的时间窗口，避免读到隔离库里的既有日志：
	// 这里验证方言兼容，不是聚合数值。
	const farFuture = 4102444800 // 2100-01-01
	var probe int64
	require.NoError(t,
		sumUsedTokenQuery(db, farFuture, farFuture+1, "", "", "").Scan(&probe).Error,
		"the aggregate must run on PostgreSQL; ifnull is not a PostgreSQL function",
	)
	require.Equal(t, int64(0), probe)
}
