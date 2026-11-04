package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 非 MySQL 方言上，旧的复合唯一索引由 dropLegacySoftDeleteUniqueIndex 以
// DROP INDEX IF EXISTS 清除。SQLite 由 meta_unique_legacy_index_test.go 覆盖；
// 这里覆盖 PostgreSQL 的同一分支——生产主库是 PG，而两种方言的 Migrator
// 在 HasIndex 与 DROP 语法上并不相同，不能互相代替。
//
// 用例以 NEW_API_POSTGRES_TEST_DSN / POSTGRES_TEST_DSN 门控，未设置时跳过。
// 复用 openBareLogsSchema 的 schema 隔离：所有表都建在一次性 schema 里，
// 不会碰到宿主库的 public schema。
func TestLegacySoftDeleteCompositeIndexDroppedOnPostgres(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL legacy index drop test")
	}

	db, cleanup := openBareLogsSchema(t, dsn, "meta_drop_test_")
	defer cleanup()

	cases := []struct {
		model  interface{}
		table  string
		index  string
		column string
	}{
		{&Model{}, modelMetaTable, modelMetaNameIndex + "_delete_at", "model_name"},
		{&Vendor{}, vendorMetaTable, vendorMetaNameIndex + "_delete_at", "name"},
	}
	for _, tc := range cases {
		require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+tc.table).Error)
		require.NoError(t, db.Exec("CREATE TABLE "+tc.table+
			" (id bigserial PRIMARY KEY, "+tc.column+" varchar(128))").Error)
		require.NoError(t, db.Exec("CREATE UNIQUE INDEX "+tc.index+" ON "+
			tc.table+" ("+tc.column+", id)").Error)
		require.True(t, db.Migrator().HasIndex(tc.model, tc.index),
			"前置：%s 已手工建立", tc.index)

		require.NoError(t, dropLegacySoftDeleteUniqueIndex(
			gormPrefillGroupMigrationExecutor{db: db}, "postgres", tc.model, tc.table, tc.index))
		require.False(t, db.Migrator().HasIndex(tc.model, tc.index),
			"PostgreSQL 升级后旧复合唯一索引 %s 必须被删除", tc.index)

		// 索引已不存在时必须是空操作，不能报错。
		require.NoError(t, dropLegacySoftDeleteUniqueIndex(
			gormPrefillGroupMigrationExecutor{db: db}, "postgres", tc.model, tc.table, tc.index))
	}
}
