package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeLogIndexCleanupExecutor struct {
	hasTable    bool
	present     map[string]bool
	dropErrs    map[string]error
	dropped     []string
	hasIndexErr error
}

func (f *fakeLogIndexCleanupExecutor) HasTable(dst interface{}) bool { return f.hasTable }

func (f *fakeLogIndexCleanupExecutor) HasIndex(dst interface{}, name string) bool {
	return f.present[name]
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
