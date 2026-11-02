package model

import (
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// F1 真实复现场景：MySQL 8.4 上模拟生产库的旧复合唯一索引结构
// (model_name, deleted_at)，插入一行软删除 + 一行同名活动行（F1 原文描述），
// 然后运行 migrateDB()。必须成功启动，并最终把唯一索引落在
// active_model_name 生成列上，拒绝同名活动行、允许软删除后重建。
func TestF1ModelMetaMySQLBackwardCompat(t *testing.T) {
	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL F1 backward compat not covered by this run")
	}
	db, err := gorm.Open(MySQLDialector(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousMySQL, previousPG := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	DB, LOG_DB = db, db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = false, true, false
	initCol()
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS models").Error
		_ = db.Exec("DROP TABLE IF EXISTS vendors").Error
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = previousSQLite, previousMySQL, previousPG
		initCol()
	})

	// Step 1: 用 Raw SQL 建表，模拟 82dcfcf93 之前的旧复合唯一索引结构
	require.NoError(t, db.Exec(`
		CREATE TABLE models (
			id int NOT NULL AUTO_INCREMENT,
			model_name varchar(128) NOT NULL,
			description text,
			icon varchar(128),
			tags varchar(255),
			vendor_id int,
			endpoints text,
			status int DEFAULT 1,
			sync_official int DEFAULT 1,
			created_time bigint,
			updated_time bigint,
			deleted_at bigint DEFAULT NULL,
			name_rule int DEFAULT 0,
			PRIMARY KEY (id),
			INDEX idx_models_deleted_at (deleted_at),
			UNIQUE INDEX uk_model_name_delete_at (model_name, deleted_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`).Error)

	// Step 2: 插入软删除行 + 同名活动行（旧结构下两者不冲突，因为 deleted_at 的 NULL 互不相等）
	now := time.Now().Unix()
	require.NoError(t, db.Exec(
		"INSERT INTO models (model_name, status, created_time, updated_time, deleted_at) VALUES (?, 1, ?, ?, ?)",
		"f1-dup-model", now, now, now,
	).Error) // 软删除行
	require.NoError(t, db.Exec(
		"INSERT INTO models (model_name, status, created_time, updated_time) VALUES (?, 1, ?, ?)",
		"f1-dup-model", now, now,
	).Error) // 同名活动行（旧结构允许）

	// Step 3: 此时运行 migrateDB()。Model 已不在 autoMigrateModels 中，
	// AutoMigrate 不会抢建 uk_model_name。专用迁移 migrateModelMetaUniqueIndex
	// 会在 MySQL 分支走生成列路径，把唯一索引落在 active_model_name 上。
	// 活动行在生成列中非 NULL，软删除行为 NULL，所以不会冲突。
	require.NoError(t, migrateDB(),
		"F1 场景：软删除行 + 同名活动行的既有 MySQL 库，迁移必须成功")

	// Step 4: 验证唯一索引落在 active_model_name 上
	var indexColumns string
	require.NoError(t, db.Raw(
		"SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'models' AND index_name = ?",
		modelMetaNameIndex,
	).Scan(&indexColumns).Error)
	require.Equal(t, modelActiveNameColumn, indexColumns,
		"唯一索引必须落在活动行生成列上，而不是 model_name 列")

	// Step 5: 拒绝新的同名活动行
	insertNow := now + 1
	require.NoError(t, db.Exec(
		"INSERT INTO models (model_name, status, created_time, updated_time) VALUES (?, 1, ?, ?)",
		"f1-new-live", insertNow, insertNow,
	).Error) // 新的唯一名，应成功
	err = db.Exec(
		"INSERT INTO models (model_name, status, created_time, updated_time) VALUES (?, 1, ?, ?)",
		"f1-new-live", insertNow+1, insertNow+1,
	).Error
	require.Error(t, err, "同名活动行必须被拒绝")

	// Step 6: 软删除既有活动行后，同名可重建。这是旧结构下丢掉的语义：
	// 旧复合索引让软删除行仍占位，重建同名会被 1062 拒绝。生成列方案下
	// 软删除行的 active_model_name 坍缩为 NULL，不参与唯一约束。
	require.NoError(t, db.Exec(
		"UPDATE models SET deleted_at = ? WHERE model_name = ? AND deleted_at IS NULL",
		time.Now().Unix(), "f1-new-live",
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO models (model_name, status, created_time, updated_time) VALUES (?, 1, ?, ?)",
		"f1-new-live", insertNow+2, insertNow+2,
	).Error, "软删除后同名模型必须可以重建")
}

// 同上，验证 Vendor 表
func TestF1VendorMetaMySQLBackwardCompat(t *testing.T) {
	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL F1 backward compat not covered by this run")
	}
	db, err := gorm.Open(MySQLDialector(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousMySQL, previousPG := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	DB, LOG_DB = db, db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = false, true, false
	initCol()
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS models").Error
		_ = db.Exec("DROP TABLE IF EXISTS vendors").Error
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = previousSQLite, previousMySQL, previousPG
		initCol()
	})

	// 建 vendors 表（旧复合唯一索引结构）
	require.NoError(t, db.Exec(`
		CREATE TABLE vendors (
			id int NOT NULL AUTO_INCREMENT,
			name varchar(128) NOT NULL,
			description text,
			icon varchar(128),
			status int DEFAULT 1,
			created_time bigint,
			updated_time bigint,
			deleted_at bigint DEFAULT NULL,
			PRIMARY KEY (id),
			INDEX idx_vendors_deleted_at (deleted_at),
			UNIQUE INDEX uk_vendor_name_delete_at (name, deleted_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`).Error)

	now := time.Now().Unix()
	require.NoError(t, db.Exec(
		"INSERT INTO vendors (name, status, created_time, updated_time, deleted_at) VALUES (?, 1, ?, ?, ?)",
		"f1-dup-vendor", now, now, now,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO vendors (name, status, created_time, updated_time) VALUES (?, 1, ?, ?)",
		"f1-dup-vendor", now, now,
	).Error)

	require.NoError(t, migrateDB(),
		"F1 场景：软删除行 + 同名活动行的既有 MySQL Vendor 表，迁移必须成功")

	var indexColumns string
	require.NoError(t, db.Raw(
		"SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'vendors' AND index_name = ?",
		vendorMetaNameIndex,
	).Scan(&indexColumns).Error)
	require.Equal(t, vendorActiveNameColumn, indexColumns,
		"唯一索引必须落在活动行生成列上，而不是 name 列")
}