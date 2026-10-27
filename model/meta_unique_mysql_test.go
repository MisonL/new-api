package model

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// models/vendors 的 Name 用 gorm 标签声明了部分唯一索引
// (uniqueIndex:...,where:deleted_at IS NULL)。MySQL 驱动没有 BuildIndexOptions，
// 该条件会被静默丢弃，实际建出的是 name 上的普通唯一索引：软删除一行后
// 同名行永远无法再创建，而接口层的 IsXxxNameDuplicated 只看未删除行、会放行，
// 于是用户看到"名称不存在"却被数据库拒绝。只能在真实 MySQL 上验证 DDL 形态。
// 仅在 NEW_API_TEST_MYSQL_DSN 指向一次性数据库时运行。
func openMetaMySQLDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL partial-index semantics not covered by this run")
	}
	db, err := gorm.Open(MySQLDialector(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	previous := DB
	DB = db
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS models").Error
		_ = db.Exec("DROP TABLE IF EXISTS vendors").Error
		DB = previous
	})
	require.NoError(t, migrateModelMetaUniqueIndex())
	require.NoError(t, migrateVendorMetaUniqueIndex())
	return db
}

func TestModelNameUniqueAmongLiveRowsOnMySQL(t *testing.T) {
	db := openMetaMySQLDB(t)

	var indexColumns string
	require.NoError(t, db.Raw(
		"SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'models' AND index_name = ?",
		modelMetaNameIndex,
	).Scan(&indexColumns).Error)
	require.Equal(t, modelActiveNameColumn, indexColumns,
		"唯一索引必须落在活动行生成列上，而不是 model_name 列")

	require.NoError(t, (&Model{ModelName: "dup-live", Status: 1}).Insert())
	err := (&Model{ModelName: "dup-live", Status: 1}).Insert()
	require.Error(t, err, "同名未删除模型必须被拒绝")

	var first Model
	require.NoError(t, db.Where("model_name = ?", "dup-live").First(&first).Error)
	require.NoError(t, DB.Delete(&Model{}, first.Id).Error)

	require.NoError(t, (&Model{ModelName: "dup-live", Status: 1}).Insert(),
		"软删除后同名模型必须可以重新创建")
}

func TestVendorNameUniqueAmongLiveRowsOnMySQL(t *testing.T) {
	db := openMetaMySQLDB(t)

	var indexColumns string
	require.NoError(t, db.Raw(
		"SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'vendors' AND index_name = ?",
		vendorMetaNameIndex,
	).Scan(&indexColumns).Error)
	require.Equal(t, vendorActiveNameColumn, indexColumns,
		"唯一索引必须落在活动行生成列上，而不是 name 列")

	require.NoError(t, (&Vendor{Name: "dup-live-vendor", Status: 1}).Insert())
	err := (&Vendor{Name: "dup-live-vendor", Status: 1}).Insert()
	require.Error(t, err, "同名未删除供应商必须被拒绝")

	var first Vendor
	require.NoError(t, db.Where("name = ?", "dup-live-vendor").First(&first).Error)
	require.NoError(t, DB.Delete(&Vendor{}, first.Id).Error)

	require.NoError(t, (&Vendor{Name: "dup-live-vendor", Status: 1}).Insert(),
		"软删除后同名供应商必须可以重新创建")
}
