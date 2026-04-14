package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakePrefillGroupMigrationExecutor struct {
	hasTable         bool
	autoMigrateCalls int
	hasColumns       map[string]bool
	addedColumns     []string
	addColumnErrs    map[string]error
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

	require.NoError(t, migratePrefillGroupTableWithExecutor(executor, true))
	require.Equal(t, 1, executor.autoMigrateCalls)
	require.Empty(t, executor.addedColumns)
	require.Empty(t, executor.executedSQL)
}

func TestMigratePrefillGroupTableWithExecutorAutoMigratesOnNonPostgres(t *testing.T) {
	executor := &fakePrefillGroupMigrationExecutor{
		hasTable:      true,
		hasColumns:    map[string]bool{},
		addColumnErrs: map[string]error{},
		execErrs:      map[string]error{},
	}

	require.NoError(t, migratePrefillGroupTableWithExecutor(executor, false))
	require.Equal(t, 1, executor.autoMigrateCalls)
	require.Empty(t, executor.addedColumns)
	require.Empty(t, executor.executedSQL)
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

	require.NoError(t, migratePrefillGroupTableWithExecutor(executor, true))
	require.Zero(t, executor.autoMigrateCalls)
	require.Equal(t, []string{"items", "description", "updated_time"}, executor.addedColumns)
	require.Equal(
		t,
		[]string{
			`ALTER TABLE "prefill_groups" DROP CONSTRAINT IF EXISTS "idx_prefill_groups_name"`,
			`ALTER TABLE "prefill_groups" DROP CONSTRAINT IF EXISTS "uni_prefill_groups_name"`,
			`CREATE INDEX IF NOT EXISTS "idx_prefill_groups_type" ON "prefill_groups" ("type")`,
			`CREATE INDEX IF NOT EXISTS "idx_prefill_groups_deleted_at" ON "prefill_groups" ("deleted_at")`,
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

	err := migratePrefillGroupTableWithExecutor(executor, true)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to drop implicit prefill_groups unique constraint")
	require.ErrorContains(t, err, "boom")
}
