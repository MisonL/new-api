package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// logIndexCleanupMigrationOptionKey marks the one-time removal of legacy
// combinatorial indexes on the logs table. The marker is stored in the main
// database because the repository has no versioned migration table and the
// options table only exists there; the indexes themselves are dropped from
// whichever database holds logs (LOG_DB).
const logIndexCleanupMigrationOptionKey = "migration.log_index_cleanup.20260921"

// legacyLogIndexes are indexes that no query in this repository reaches.
// They were introduced by "perf: optimize log and dashboard loading" as a
// combinatorial set (every pair of filter dimensions). The admin log filter
// sends one value per dimension, so each dimension's leading column is
// already served by its own (dimension, created_at) index; the combinations
// only added insert cost. logs_pkey is deliberately absent: it backs the
// primary key constraint and cannot be dropped as a plain index.
var legacyLogIndexes = []string{
	"idx_created_at_id",
	"idx_user_id_id",
	"idx_logs_channel_id",
	"idx_logs_channel_group_type_created_id",
	"idx_logs_channel_model_type_created_id",
	"idx_logs_group",
	"idx_logs_ip",
	"idx_logs_model_group_type_created_id",
	"idx_logs_model_name",
	"idx_logs_request_id",
	"idx_logs_token_group_type_created_id",
	"idx_logs_token_model_type_created_id",
	"idx_logs_token_name",
	"idx_logs_upstream_request_id",
	"idx_logs_upstream_request_id_created_at",
	"idx_logs_user_id",
	"idx_logs_username",
	"idx_logs_username_group_type_created_id",
	"idx_logs_username_model_type_created_id",
	"idx_logs_username_token_group_type_created_id",
	"idx_logs_username_token_type_created_id",
}

type logIndexCleanupExecutor interface {
	HasTable(dst interface{}) bool
	HasIndex(dst interface{}, name string) bool
	DropIndex(dst interface{}, name string) error
}

// dropIndexConcurrently drops an index off PostgreSQL's logs table without
// taking an ACCESS EXCLUSIVE lock for the duration of the drop.
//
// A plain DROP INDEX holds ACCESS EXCLUSIVE on the table while it unlinks the
// index: every concurrent read and insert on logs waits, and on a multi-GB
// table with many indexes to remove that is a long outage. PostgreSQL's
// CONCURRENTLY form does the work in two shorter lock acquisitions instead.
//
// CONCURRENTLY cannot run inside a transaction block and cannot be prepared as
// a statement, so it must go through Exec as literal SQL rather than through
// the migrator (which emits a plain DROP INDEX) or PrepareStmt. The caller
// verifies PostgreSQL first, and every other dialect keeps the plain path.
func dropLogIndexConcurrently(db *gorm.DB, name string) error {
	return db.Exec("DROP INDEX CONCURRENTLY IF EXISTS " + quoteIdentifier(name)).Error
}

// quoteIdentifier double-quotes a PostgreSQL identifier and escapes embedded
// quotes so a name can never terminate the identifier and inject SQL.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// isPostgreSQL reports whether db is backed by PostgreSQL, where the
// concurrent drop is both available and necessary.
func isPostgreSQL(db *gorm.DB) bool {
	return db != nil && db.Dialector != nil && db.Dialector.Name() == "postgres"
}

type logIndexCleanupMarker interface {
	OptionExists(key string) (bool, error)
	MarkOptionDone(key string) error
}

type gormLogIndexCleanupExecutor struct {
	db *gorm.DB
}

func (e gormLogIndexCleanupExecutor) HasTable(dst interface{}) bool {
	return e.db.Migrator().HasTable(dst)
}

func (e gormLogIndexCleanupExecutor) HasIndex(dst interface{}, name string) bool {
	return e.db.Migrator().HasIndex(dst, name)
}

func (e gormLogIndexCleanupExecutor) DropIndex(dst interface{}, name string) error {
	if isPostgreSQL(e.db) {
		return dropLogIndexConcurrently(e.db, name)
	}
	return e.db.Migrator().DropIndex(dst, name)
}

type gormLogIndexCleanupMarker struct {
	db *gorm.DB
}

func (m gormLogIndexCleanupMarker) OptionExists(key string) (bool, error) {
	var marker Option
	err := m.db.First(&marker, "key = ?", key).Error
	if err == nil {
		return true, nil
	}
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	return false, err
}

func (m gormLogIndexCleanupMarker) MarkOptionDone(key string) error {
	return m.db.Save(&Option{Key: key, Value: "done"}).Error
}

// migrateLogIndexCleanup drops the legacy combinatorial logs indexes exactly
// once per database. Dropping them is not required for correctness: GORM's
// AutoMigrate only creates the indexes still declared on the Log struct and
// never deletes one whose tag was removed, so without this pass the indexes
// would linger and keep charging every log insert.
func migrateLogIndexCleanup() error {
	return migrateLogIndexCleanupWithExecutor(
		gormLogIndexCleanupExecutor{db: LOG_DB},
		gormLogIndexCleanupMarker{db: DB},
	)
}

func migrateLogIndexCleanupWithExecutor(executor logIndexCleanupExecutor, marker logIndexCleanupMarker) error {
	if !executor.HasTable(&Log{}) {
		return nil
	}
	done, err := marker.OptionExists(logIndexCleanupMigrationOptionKey)
	if err != nil {
		return fmt.Errorf("failed to read log index cleanup marker: %w", err)
	}
	if done {
		return nil
	}

	dropped := 0
	for _, name := range legacyLogIndexes {
		if !executor.HasIndex(&Log{}, name) {
			continue
		}
		if err := executor.DropIndex(&Log{}, name); err != nil {
			return fmt.Errorf("failed to drop legacy logs index %s: %w", name, err)
		}
		dropped++
	}
	if err := marker.MarkOptionDone(logIndexCleanupMigrationOptionKey); err != nil {
		return fmt.Errorf("failed to record log index cleanup marker: %w", err)
	}
	common.SysLog(fmt.Sprintf("log index cleanup finished: dropped_indexes=%d", dropped))
	return nil
}
