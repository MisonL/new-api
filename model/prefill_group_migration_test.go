package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fakePrefillGroupMigrationExecutor struct {
	hasTable         bool
	autoMigrateCalls int
	hasColumns       map[string]bool
	addedColumns     []string
	addColumnErrs    map[string]error
	hasIndexes       map[string]bool
	tableColumns     map[string]bool
	indexColumns     map[string]map[string]bool
	executedSQL      []string
	execErrs         map[string]error
}

func (f *fakePrefillGroupMigrationExecutor) HasTable(dst interface{}) bool {
	return f.hasTable
}

func (f *fakePrefillGroupMigrationExecutor) AutoMigrate(dst ...interface{}) error {
	f.autoMigrateCalls++
	return nil
}

func (f *fakePrefillGroupMigrationExecutor) HasColumn(dst interface{}, field string) bool {
	return f.hasColumns[field]
}

func (f *fakePrefillGroupMigrationExecutor) AddColumn(dst interface{}, field string) error {
	f.addedColumns = append(f.addedColumns, field)
	return f.addColumnErrs[field]
}

func (f *fakePrefillGroupMigrationExecutor) HasIndex(dst interface{}, name string) bool {
	return f.hasIndexes[name]
}

// DB 返回 nil：替身只为 DDL 判定服务，不承载 DML；需要 DML 的迁移用例用真实库。
func (f *fakePrefillGroupMigrationExecutor) DB() *gorm.DB {
	return nil
}

func (f *fakePrefillGroupMigrationExecutor) TableHasColumn(table, column string) (bool, error) {
	return f.tableColumns[column], nil
}

func (f *fakePrefillGroupMigrationExecutor) IndexCoversColumn(table, indexName, column string) (bool, error) {
	return f.indexColumns[indexName][column], nil
}

func (f *fakePrefillGroupMigrationExecutor) Exec(sql string) error {
	f.executedSQL = append(f.executedSQL, sql)
	return f.execErrs[sql]
}

func TestMigratePrefillGroupTableWithExecutorAutoMigratesWhenTableMissing(t *testing.T) {
	executor := &fakePrefillGroupMigrationExecutor{
		hasColumns:    map[string]bool{},
		addColumnErrs: map[string]error{},
		execErrs:      map[string]error{},
	}

	require.NoError(t, migratePrefillGroupTableWithExecutor(executor, "postgres"))
	require.Equal(t, 1, executor.autoMigrateCalls)
	require.Empty(t, executor.addedColumns)
	require.Empty(t, executor.executedSQL)
}

// 全新 MySQL 库：建表后仍要继续修复，否则建出来的就是退化的普通唯一索引。
func TestMigratePrefillGroupTableWithExecutorRepairsFreshMySQLDatabase(t *testing.T) {
	executor := &fakePrefillGroupMigrationExecutor{
		hasColumns:    map[string]bool{},
		addColumnErrs: map[string]error{},
		execErrs:      map[string]error{},
		hasIndexes:    map[string]bool{prefillNameIndex: true},
		tableColumns:  map[string]bool{},
		indexColumns:  map[string]map[string]bool{},
	}

	require.NoError(t, migratePrefillGroupTableWithExecutor(executor, "mysql"))
	require.Equal(t, 1, executor.autoMigrateCalls, "建表一次")
	require.Len(t, executor.executedSQL, 3, "修复分支仍需补生成列并重建索引")
}

func TestMigratePrefillGroupTableWithExecutorAutoMigratesOnSQLite(t *testing.T) {
	executor := &fakePrefillGroupMigrationExecutor{
		hasTable:      true,
		hasColumns:    map[string]bool{},
		addColumnErrs: map[string]error{},
		execErrs:      map[string]error{},
	}

	require.NoError(t, migratePrefillGroupTableWithExecutor(executor, "sqlite"))
	require.Equal(t, 1, executor.autoMigrateCalls)
	require.Empty(t, executor.addedColumns)
	require.Equal(t, []string{
		`DROP INDEX IF EXISTS "idx_prefill_groups_name"`,
		`DROP INDEX IF EXISTS "uni_prefill_groups_name"`,
	}, executor.executedSQL)
}

func TestMigratePrefillGroupTableWithExecutorRepairsPostgresSchemaIdempotently(t *testing.T) {
	executor := &fakePrefillGroupMigrationExecutor{
		hasTable: true,
		hasColumns: map[string]bool{
			"name":         true,
			"type":         true,
			"items":        false,
			"description":  false,
			"created_time": true,
			"updated_time": false,
			"deleted_at":   true,
		},
		addColumnErrs: map[string]error{},
		execErrs:      map[string]error{},
	}

	require.NoError(t, migratePrefillGroupTableWithExecutor(executor, "postgres"))
	require.Zero(t, executor.autoMigrateCalls)
	require.Equal(t, []string{"items", "description", "updated_time"}, executor.addedColumns)
	require.Equal(
		t,
		[]string{
			`ALTER TABLE "prefill_groups" DROP CONSTRAINT IF EXISTS "idx_prefill_groups_name"`,
			`ALTER TABLE "prefill_groups" DROP CONSTRAINT IF EXISTS "uni_prefill_groups_name"`,
			`DROP INDEX IF EXISTS "idx_prefill_groups_name"`,
			`DROP INDEX IF EXISTS "uni_prefill_groups_name"`,
			`CREATE INDEX IF NOT EXISTS "idx_prefill_groups_type" ON "prefill_groups" ("type")`,
			`CREATE INDEX IF NOT EXISTS "idx_prefill_groups_deleted_at" ON "prefill_groups" ("deleted_at")`,
			`CREATE INDEX IF NOT EXISTS "idx_prefill_groups_type_deleted_updated" ON "prefill_groups" ("type","deleted_at","updated_time" DESC)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS "uk_prefill_name" ON "prefill_groups" ("name") WHERE deleted_at IS NULL`,
		},
		executor.executedSQL,
	)
}

func TestMigratePrefillGroupTableWithExecutorReturnsExecError(t *testing.T) {
	sql := `ALTER TABLE "prefill_groups" DROP CONSTRAINT IF EXISTS "uni_prefill_groups_name"`
	executor := &fakePrefillGroupMigrationExecutor{
		hasTable: true,
		hasColumns: map[string]bool{
			"name":         true,
			"type":         true,
			"items":        true,
			"description":  true,
			"created_time": true,
			"updated_time": true,
			"deleted_at":   true,
		},
		addColumnErrs: map[string]error{},
		execErrs: map[string]error{
			sql: errors.New("boom"),
		},
	}

	err := migratePrefillGroupTableWithExecutor(executor, "postgres")
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to drop implicit prefill_groups unique constraint")
	require.ErrorContains(t, err, "boom")
}

// MySQL 没有部分索引，gorm 的 where 条件会连同标签一起被丢弃，最终建出的是
// name 上的普通唯一索引。迁移必须把它换成基于生成列的活动行唯一索引。
func TestMigratePrefillGroupTableWithExecutorRepairsMySQLDegradedIndex(t *testing.T) {
	executor := &fakePrefillGroupMigrationExecutor{
		hasTable:      true,
		hasColumns:    map[string]bool{},
		addColumnErrs: map[string]error{},
		execErrs:      map[string]error{},
		hasIndexes:    map[string]bool{prefillNameIndex: true},
		tableColumns:  map[string]bool{},
		indexColumns:  map[string]map[string]bool{},
	}

	require.NoError(t, migratePrefillGroupTableWithExecutor(executor, "mysql"))
	require.Zero(t, executor.autoMigrateCalls,
		"表已存在时不得再走 AutoMigrate：它会误删幻影唯一约束并返回 1091")
	require.Equal(t, []string{
		"ALTER TABLE `prefill_groups` ADD COLUMN `active_name` varchar(64) GENERATED ALWAYS AS (IF(`deleted_at` IS NULL, `name`, NULL)) STORED",
		"ALTER TABLE `prefill_groups` DROP INDEX `uk_prefill_name`",
		"CREATE UNIQUE INDEX `uk_prefill_name` ON `prefill_groups` (`active_name`)",
	}, executor.executedSQL)
}

func TestMigratePrefillGroupTableWithExecutorMySQLIsIdempotent(t *testing.T) {
	executor := &fakePrefillGroupMigrationExecutor{
		hasTable:      true,
		hasColumns:    map[string]bool{},
		addColumnErrs: map[string]error{},
		execErrs:      map[string]error{},
		hasIndexes:    map[string]bool{prefillNameIndex: true},
		tableColumns:  map[string]bool{activePrefillNameColumn: true},
		indexColumns:  map[string]map[string]bool{prefillNameIndex: {activePrefillNameColumn: true}},
	}

	require.NoError(t, migratePrefillGroupTableWithExecutor(executor, "mysql"))
	require.Empty(t, executor.executedSQL, "已修复过时必须不执行任何 DDL")
}
