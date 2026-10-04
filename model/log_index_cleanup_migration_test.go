package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openLogIndexCleanupSQLite returns a private in-memory database holding the
// logs table, so the real executor's dialect branch can be exercised without a
// server. The named DSN is closed on cleanup: a shared-cache memory database
// survives until its last connection closes, and leaving it open would leak
// rows into a second run of this package.
func openLogIndexCleanupSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	t.Cleanup(func() {
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

type fakeLogIndexCleanupExecutor struct {
	hasTable    bool
	present     map[string]bool
	dropErrs    map[string]error
	dropped     []string
	hasIndexErr error
}

func (f *fakeLogIndexCleanupExecutor) HasTable(dst interface{}) bool { return f.hasTable }

func (f *fakeLogIndexCleanupExecutor) HasIndex(dst interface{}, name string) (bool, error) {
	if f.hasIndexErr != nil {
		return false, f.hasIndexErr
	}
	return f.present[name], nil
}

func (f *fakeLogIndexCleanupExecutor) DropIndex(dst interface{}, name string) error {
	if err := f.dropErrs[name]; err != nil {
		return err
	}
	f.dropped = append(f.dropped, name)
	delete(f.present, name)
	return nil
}

type fakeLogIndexCleanupMarker struct {
	exists     bool
	existsErr  error
	markErr    error
	markedKeys []string
}

func (f *fakeLogIndexCleanupMarker) OptionExists(key string) (bool, error) {
	return f.exists, f.existsErr
}

func (f *fakeLogIndexCleanupMarker) MarkOptionDone(key string) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.markedKeys = append(f.markedKeys, key)
	return nil
}

func newFakeLogIndexCleanup() (*fakeLogIndexCleanupExecutor, *fakeLogIndexCleanupMarker) {
	executor := &fakeLogIndexCleanupExecutor{
		hasTable: true,
		present:  map[string]bool{},
		dropErrs: map[string]error{},
	}
	for _, name := range legacyLogIndexes {
		executor.present[name] = true
	}
	return executor, &fakeLogIndexCleanupMarker{}
}

func TestLogIndexCleanupDropsEveryLegacyIndexOnce(t *testing.T) {
	executor, marker := newFakeLogIndexCleanup()

	require.NoError(t, migrateLogIndexCleanupWithExecutor(executor, marker))
	require.ElementsMatch(t, legacyLogIndexes, executor.dropped)
	require.Equal(t, []string{logIndexCleanupMigrationOptionKey}, marker.markedKeys)
}

func TestLogIndexCleanupIsIdempotentWhenMarkerPresent(t *testing.T) {
	executor, marker := newFakeLogIndexCleanup()
	marker.exists = true

	require.NoError(t, migrateLogIndexCleanupWithExecutor(executor, marker))
	require.Empty(t, executor.dropped)
	require.Empty(t, marker.markedKeys)
}

func TestLogIndexCleanupSkipsMissingIndexes(t *testing.T) {
	executor, marker := newFakeLogIndexCleanup()
	missing := legacyLogIndexes[0]
	delete(executor.present, missing)

	require.NoError(t, migrateLogIndexCleanupWithExecutor(executor, marker))
	require.NotContains(t, executor.dropped, missing)
	require.Len(t, executor.dropped, len(legacyLogIndexes)-1)
}

func TestLogIndexCleanupSkipsWhenLogTableMissing(t *testing.T) {
	executor, marker := newFakeLogIndexCleanup()
	executor.hasTable = false

	require.NoError(t, migrateLogIndexCleanupWithExecutor(executor, marker))
	require.Empty(t, executor.dropped)
	require.Empty(t, marker.markedKeys)
}

func TestLogIndexCleanupStopsOnDropError(t *testing.T) {
	executor, marker := newFakeLogIndexCleanup()
	failing := legacyLogIndexes[2]
	executor.dropErrs[failing] = errors.New("boom")

	err := migrateLogIndexCleanupWithExecutor(executor, marker)
	require.ErrorContains(t, err, failing)
	require.NotContains(t, executor.dropped, failing)
	require.Empty(t, marker.markedKeys, "marker must not be written when a drop fails")
}

func TestLogIndexCleanupFailsWhenMarkerReadFails(t *testing.T) {
	executor, marker := newFakeLogIndexCleanup()
	marker.existsErr = errors.New("read failed")

	err := migrateLogIndexCleanupWithExecutor(executor, marker)
	require.ErrorContains(t, err, "read failed")
	require.Empty(t, executor.dropped, "must not drop anything before the marker is read")
}

// An index lookup that fails must abort the migration instead of being read as
// "the index is gone": otherwise the drop is skipped and the done-marker is
// still written, so the legacy index is stranded forever and never retried.
func TestLogIndexCleanupFailsWhenIndexLookupFails(t *testing.T) {
	executor, marker := newFakeLogIndexCleanup()
	executor.hasIndexErr = errors.New("lookup failed")

	err := migrateLogIndexCleanupWithExecutor(executor, marker)
	require.ErrorContains(t, err, "lookup failed")
	require.Empty(t, executor.dropped, "must not drop anything after a failed lookup")
	require.Empty(t, marker.markedKeys, "marker must not be written when an index lookup fails")
}

func TestLogIndexCleanupFailsWhenMarkerWriteFails(t *testing.T) {
	executor, marker := newFakeLogIndexCleanup()
	marker.markErr = errors.New("write failed")

	err := migrateLogIndexCleanupWithExecutor(executor, marker)
	require.ErrorContains(t, err, "write failed")
	require.ElementsMatch(t, legacyLogIndexes, executor.dropped)
}

func TestLegacyLogIndexesExcludePrimaryKey(t *testing.T) {
	require.NotContains(t, legacyLogIndexes, "logs_pkey",
		"logs_pkey backs the primary key constraint and cannot be dropped")
}

// TestLegacyLogIndexesAreNotDeclaredOnModel guards the invariant that makes
// this migration necessary and sufficient: every index it drops must already
// be absent from the Log struct, otherwise AutoMigrate would recreate it on
// the next start and the drop would be undone.
func TestLegacyLogIndexesAreNotDeclaredOnModel(t *testing.T) {
	declared := parsedLogIndexNames(t)
	for _, name := range legacyLogIndexes {
		require.NotContains(t, declared, name,
			"index %s is still declared on Log; AutoMigrate would recreate it", name)
	}
}

// The fake above pins the control flow; this exercises the real executor's
// SQLite branch against a fresh database, where gorm's own HasIndex was the
// silent-error version this migration replaced.
func TestGormExecutorHasIndexReportsExistenceOnSQLite(t *testing.T) {
	db := openLogIndexCleanupSQLite(t)
	executor := gormLogIndexCleanupExecutor{db: db}

	present, err := executor.HasIndex(&Log{}, "idx_logs_absent_probe")
	require.NoError(t, err)
	require.False(t, present, "an uncreated index must report absent")

	require.NoError(t, db.Exec("CREATE INDEX idx_logs_absent_probe ON logs(created_at)").Error)

	present, err = executor.HasIndex(&Log{}, "idx_logs_absent_probe")
	require.NoError(t, err)
	require.True(t, present, "a created index must report present")
}
