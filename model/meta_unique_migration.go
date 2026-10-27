package model

import (
	"fmt"
)

// 软删除表用 composite unique (name, deleted_at) 对未删除行不约束：
// SQL 里 NULL 互不相等，两行 name 相同、deleted_at 均为 NULL 时索引判定
// 不冲突。PostgreSQL/SQLite 通过 where:deleted_at IS NULL 生成部分唯一索引
// 即可；MySQL 没有部分索引，用生成列 active_xxx_name（删除后坍缩为 NULL）
// 并在其上建 UNIQUE，恢复"未删除行内唯一"。
//
// 下面两个迁移沿用 prefill_groups 的同一套执行器接口与分支结构。

const (
	modelActiveNameColumn  = "active_model_name"
	modelMetaTable         = "models"
	modelMetaNameIndex     = "uk_model_name"
	vendorActiveNameColumn = "active_vendor_name"
	vendorMetaTable        = "vendors"
	vendorMetaNameIndex    = "uk_vendor_name"
)

// modelMetaColumns lists the table's ordinary columns in creation order so a
// pre-existing table can be brought up to date without a second AutoMigrate.
var modelMetaColumns = []string{
	"model_name",
	"description",
	"icon",
	"tags",
	"vendor_id",
	"endpoints",
	"status",
	"sync_official",
	"created_time",
	"updated_time",
	"deleted_at",
}

var vendorMetaColumns = []string{
	"name",
	"description",
	"icon",
	"status",
	"created_time",
	"updated_time",
	"deleted_at",
}

func migrateModelMetaUniqueIndex() error {
	dialect := ""
	if DB != nil && DB.Dialector != nil {
		dialect = DB.Dialector.Name()
	}
	return migrateSoftDeleteUniqueIndexWithExecutor(
		gormPrefillGroupMigrationExecutor{db: DB},
		dialect,
		&Model{},
		modelMetaTable,
		modelMetaNameIndex,
		modelActiveNameColumn,
		"model_name",
		modelMetaColumns,
		128,
	)
}

func migrateVendorMetaUniqueIndex() error {
	dialect := ""
	if DB != nil && DB.Dialector != nil {
		dialect = DB.Dialector.Name()
	}
	return migrateSoftDeleteUniqueIndexWithExecutor(
		gormPrefillGroupMigrationExecutor{db: DB},
		dialect,
		&Vendor{},
		vendorMetaTable,
		vendorMetaNameIndex,
		vendorActiveNameColumn,
		"name",
		vendorMetaColumns,
		128,
	)
}

// migrateSoftDeleteUniqueIndexWithExecutor brings a soft-deleted table's name
// uniqueness to "unique among live rows" on all three dialects.
func migrateSoftDeleteUniqueIndexWithExecutor(
	executor prefillGroupMigrationExecutor,
	dialect string,
	model interface{},
	table string,
	indexName string,
	activeColumn string,
	sourceColumn string,
	columns []string,
	maxLen int,
) error {
	if !executor.HasTable(model) {
		if err := executor.AutoMigrate(model); err != nil {
			return err
		}
		// PG/SQLite 的驱动把 where 条件写进 DDL，建表即得部分唯一索引；
		// MySQL 必须继续走生成列分支。
		if dialect != "mysql" {
			return nil
		}
	}

	if dialect != "mysql" {
		// 非 MySQL：确保表结构里有模型声明的部分唯一索引。AutoMigrate 会
		// 按 gorm tag 的 where 条件重建索引。
		return executor.AutoMigrate(model)
	}

	// 不能在这里再调一次 AutoMigrate：表已存在时 gorm 会进入
	// MigrateColumnUnique，把 name 列上的唯一索引误认成约束并尝试删除。
	for _, column := range columns {
		if executor.HasColumn(model, column) {
			continue
		}
		if err := executor.AddColumn(model, column); err != nil {
			return fmt.Errorf("failed to add %s.%s: %w", table, column, err)
		}
	}

	hasActive, err := executor.TableHasColumn(table, activeColumn)
	if err != nil {
		return fmt.Errorf("failed to inspect %s.%s: %w", table, activeColumn, err)
	}
	if !hasActive {
		if err := executor.Exec(fmt.Sprintf(
			"ALTER TABLE `%s` ADD COLUMN `%s` varchar(%d) GENERATED ALWAYS AS (IF(`deleted_at` IS NULL, `%s`, NULL)) STORED",
			table, activeColumn, maxLen, sourceColumn,
		)); err != nil {
			return fmt.Errorf("failed to add %s.%s: %w", table, activeColumn, err)
		}
	}

	covers, err := executor.IndexCoversColumn(table, indexName, activeColumn)
	if err != nil {
		return fmt.Errorf("failed to inspect %s index %s: %w", table, indexName, err)
	}
	if covers {
		return nil
	}
	if executor.HasIndex(model, indexName) {
		if err := executor.Exec("ALTER TABLE `" + table + "` DROP INDEX `" + indexName + "`"); err != nil {
			return fmt.Errorf("failed to drop degraded %s index %s: %w", table, indexName, err)
		}
	}
	if err := executor.Exec("CREATE UNIQUE INDEX `" + indexName + "` ON `" + table + "` (`" + activeColumn + "`)"); err != nil {
		return fmt.Errorf("failed to create %s unique index on %s: %w", table, activeColumn, err)
	}
	return nil
}
