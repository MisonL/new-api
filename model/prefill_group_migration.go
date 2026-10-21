package model

import (
	"fmt"

	"gorm.io/gorm"
)

type prefillGroupMigrationExecutor interface {
	HasTable(dst interface{}) bool
	AutoMigrate(dst ...interface{}) error
	HasColumn(dst interface{}, field string) bool
	AddColumn(dst interface{}, field string) error
	HasIndex(dst interface{}, name string) bool
	// TableHasColumn reports a physical column that has no model field, such as
	// a generated column that exists only in the database.
	TableHasColumn(table, column string) (bool, error)
	// IndexCoversColumn reports whether indexName on table includes column.
	IndexCoversColumn(table, indexName, column string) (bool, error)
	Exec(sql string) error
}

type gormPrefillGroupMigrationExecutor struct {
	db *gorm.DB
}

func (e gormPrefillGroupMigrationExecutor) HasTable(dst interface{}) bool {
	return e.db.Migrator().HasTable(dst)
}

func (e gormPrefillGroupMigrationExecutor) AutoMigrate(dst ...interface{}) error {
	return e.db.AutoMigrate(dst...)
}

func (e gormPrefillGroupMigrationExecutor) HasColumn(dst interface{}, field string) bool {
	return e.db.Migrator().HasColumn(dst, field)
}

func (e gormPrefillGroupMigrationExecutor) AddColumn(dst interface{}, field string) error {
	return e.db.Migrator().AddColumn(dst, field)
}

func (e gormPrefillGroupMigrationExecutor) HasIndex(dst interface{}, name string) bool {
	return e.db.Migrator().HasIndex(dst, name)
}

func (e gormPrefillGroupMigrationExecutor) TableHasColumn(table, column string) (bool, error) {
	var count int64
	err := e.db.Raw(
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		table, column,
	).Scan(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (e gormPrefillGroupMigrationExecutor) IndexCoversColumn(table, indexName, column string) (bool, error) {
	var count int64
	err := e.db.Raw(
		"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ? AND column_name = ?",
		table, indexName, column,
	).Scan(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (e gormPrefillGroupMigrationExecutor) Exec(sql string) error {
	return e.db.Exec(sql).Error
}

func migratePrefillGroupTable() error {
	dialect := ""
	if DB != nil && DB.Dialector != nil {
		dialect = DB.Dialector.Name()
	}
	return migratePrefillGroupTableWithExecutor(gormPrefillGroupMigrationExecutor{db: DB}, dialect)
}

func migratePrefillGroupTableWithExecutor(
	executor prefillGroupMigrationExecutor,
	dialect string,
) error {
	if !executor.HasTable(&PrefillGroup{}) {
		if err := executor.AutoMigrate(&PrefillGroup{}); err != nil {
			return err
		}
		// PostgreSQL 与 SQLite 的驱动会把 where 条件写进 DDL，建表即得部分
		// 唯一索引；MySQL 没有该能力，AutoMigrate 建出的仍是 name 列的普通
		// 唯一索引，必须继续走修复分支。
		if dialect != "mysql" {
			return nil
		}
	}
	switch dialect {
	case "postgres":
		return migratePrefillGroupPostgres(executor)
	case "mysql":
		return migratePrefillGroupMySQL(executor)
	default:
		return executor.AutoMigrate(&PrefillGroup{})
	}
}

func migratePrefillGroupPostgres(executor prefillGroupMigrationExecutor) error {
	for _, column := range prefillGroupColumns {
		if executor.HasColumn(&PrefillGroup{}, column) {
			continue
		}
		if err := executor.AddColumn(&PrefillGroup{}, column); err != nil {
			return fmt.Errorf("failed to add prefill_groups.%s: %w", column, err)
		}
	}

	if err := executor.Exec(`ALTER TABLE "prefill_groups" DROP CONSTRAINT IF EXISTS "idx_prefill_groups_name"`); err != nil {
		return fmt.Errorf("failed to drop legacy prefill_groups name constraint: %w", err)
	}
	if err := executor.Exec(`ALTER TABLE "prefill_groups" DROP CONSTRAINT IF EXISTS "uni_prefill_groups_name"`); err != nil {
		return fmt.Errorf("failed to drop implicit prefill_groups unique constraint: %w", err)
	}
	if err := executor.Exec(`CREATE INDEX IF NOT EXISTS "idx_prefill_groups_type" ON "prefill_groups" ("type")`); err != nil {
		return fmt.Errorf("failed to ensure prefill_groups type index: %w", err)
	}
	if err := executor.Exec(`CREATE INDEX IF NOT EXISTS "idx_prefill_groups_deleted_at" ON "prefill_groups" ("deleted_at")`); err != nil {
		return fmt.Errorf("failed to ensure prefill_groups deleted_at index: %w", err)
	}
	if err := executor.Exec(`CREATE INDEX IF NOT EXISTS "idx_prefill_groups_type_deleted_updated" ON "prefill_groups" ("type","deleted_at","updated_time" DESC)`); err != nil {
		return fmt.Errorf("failed to ensure prefill_groups type/deleted_at/updated_time index: %w", err)
	}
	if err := executor.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS "uk_prefill_name" ON "prefill_groups" ("name") WHERE deleted_at IS NULL`); err != nil {
		return fmt.Errorf("failed to ensure prefill_groups partial unique index: %w", err)
	}
	return nil
}

// activePrefillNameColumn holds the group name only while the row is alive.
//
// The model declares its unique index as
// uniqueIndex:uk_prefill_name,where:deleted_at IS NULL, which only the
// dialects implementing BuildIndexOptions (PostgreSQL, SQLite) turn into a
// partial index. GORM's MySQL driver has no BuildIndexOptions, so the where
// clause is silently dropped and MySQL builds a plain UNIQUE(name): a
// soft-deleted row keeps occupying its name forever, and the API's own
// IsPrefillGroupNameDuplicated check (which only looks at live rows) lets a
// duplicate through to a database error.
//
// MySQL has no partial indexes, so the equivalent is a generated column that
// collapses to NULL once the row is soft-deleted, with the unique index moved
// onto it. MySQL treats NULLs as distinct in a unique index, which restores
// exactly "unique among live rows".
const (
	activePrefillNameColumn = "active_name"
	prefillGroupTable       = "prefill_groups"
	prefillNameIndex        = "uk_prefill_name"
)

// prefillGroupColumns lists the table's ordinary columns in creation order so a
// pre-existing table can be brought up to date without a second AutoMigrate.
var prefillGroupColumns = []string{
	"name",
	"type",
	"items",
	"description",
	"created_time",
	"updated_time",
	"deleted_at",
}

func migratePrefillGroupMySQL(executor prefillGroupMigrationExecutor) error {
	// 不能在这里再调一次 AutoMigrate：表已存在时 gorm 会进入 MigrateColumnUnique，
	// 把 name 列上的唯一索引误认成名为 uni_prefill_groups_name 的约束并尝试删除，
	// MySQL 返回 1091 "Can't DROP ... check that column/key exists"。
	// 新增列按清单补齐即可。
	for _, column := range prefillGroupColumns {
		if executor.HasColumn(&PrefillGroup{}, column) {
			continue
		}
		if err := executor.AddColumn(&PrefillGroup{}, column); err != nil {
			return fmt.Errorf("failed to add prefill_groups.%s: %w", column, err)
		}
	}

	hasActiveName, err := executor.TableHasColumn(prefillGroupTable, activePrefillNameColumn)
	if err != nil {
		return fmt.Errorf("failed to inspect prefill_groups.%s: %w", activePrefillNameColumn, err)
	}
	if !hasActiveName {
		if err := executor.Exec("ALTER TABLE `prefill_groups` ADD COLUMN `" + activePrefillNameColumn +
			"` varchar(64) GENERATED ALWAYS AS (IF(`deleted_at` IS NULL, `name`, NULL)) STORED"); err != nil {
			return fmt.Errorf("failed to add prefill_groups.%s: %w", activePrefillNameColumn, err)
		}
	}

	coversActiveName, err := executor.IndexCoversColumn(prefillGroupTable, prefillNameIndex, activePrefillNameColumn)
	if err != nil {
		return fmt.Errorf("failed to inspect prefill_groups index %s: %w", prefillNameIndex, err)
	}
	if coversActiveName {
		return nil
	}
	if executor.HasIndex(&PrefillGroup{}, prefillNameIndex) {
		if err := executor.Exec("ALTER TABLE `prefill_groups` DROP INDEX `" + prefillNameIndex + "`"); err != nil {
			return fmt.Errorf("failed to drop degraded prefill_groups index %s: %w", prefillNameIndex, err)
		}
	}
	if err := executor.Exec("CREATE UNIQUE INDEX `" + prefillNameIndex + "` ON `prefill_groups` (`" + activePrefillNameColumn + "`)"); err != nil {
		return fmt.Errorf("failed to create prefill_groups unique index on %s: %w", activePrefillNameColumn, err)
	}
	return nil
}
