package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type prefillGroupMigrationExecutor interface {
	HasTable(dst interface{}) bool
	AutoMigrate(dst ...interface{}) error
	HasColumn(dst interface{}, field string) bool
	AddColumn(dst interface{}, field string) error
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

func (e gormPrefillGroupMigrationExecutor) Exec(sql string) error {
	return e.db.Exec(sql).Error
}

func migratePrefillGroupTable() error {
	return migratePrefillGroupTableWithExecutor(
		gormPrefillGroupMigrationExecutor{db: DB},
		common.UsingPostgreSQL,
	)
}

func migratePrefillGroupTableWithExecutor(
	executor prefillGroupMigrationExecutor,
	usingPostgreSQL bool,
) error {
	if !executor.HasTable(&PrefillGroup{}) {
		return executor.AutoMigrate(&PrefillGroup{})
	}
	if !usingPostgreSQL {
		return executor.AutoMigrate(&PrefillGroup{})
	}

	columns := []string{
		"name",
		"type",
		"items",
		"description",
		"created_time",
		"updated_time",
		"deleted_at",
	}
	for _, column := range columns {
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
	if err := executor.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS "uk_prefill_name" ON "prefill_groups" ("name") WHERE deleted_at IS NULL`); err != nil {
		return fmt.Errorf("failed to ensure prefill_groups partial unique index: %w", err)
	}
	return nil
}
