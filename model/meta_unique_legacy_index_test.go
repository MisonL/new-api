package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 软删除表旧的复合唯一索引 (name, deleted_at) 在改为部分唯一索引后必须被清除。
// 升级路径来自真实库：老 schema 建过 uk_model_name_delete_at /
// uk_vendor_name_delete_at，AutoMigrate 只会补建新索引、不会删旧索引。旧索引
// 对未删除行没有约束力（NULL 互不相等），只会增加写入开销并在软删除时间戳
// 相同时阻止同名重建。
//
// 建表用 AutoMigrate，再手工补上旧索引——若改为手工 CREATE TABLE，GORM 的
// SQLite 迁移器会因列类型不一致整表重建，旧索引随表一起消失，用例就会在修复
// 被回退时也通过（假通过）。
func TestLegacySoftDeleteCompositeIndexIsDropped(t *testing.T) {
	db := openTestDB(t, &Model{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	require.NoError(t, db.Exec(
		`CREATE UNIQUE INDEX uk_model_name_delete_at ON models (model_name, deleted_at)`).Error)
	require.True(t, db.Migrator().HasIndex(&Model{}, "uk_model_name_delete_at"),
		"前置：旧复合唯一索引已手工建立")

	require.NoError(t, migrateModelMetaUniqueIndex())

	require.False(t, db.Migrator().HasIndex(&Model{}, "uk_model_name_delete_at"),
		"升级后旧复合唯一索引必须被删除")
	require.True(t, db.Migrator().HasIndex(&Model{}, modelMetaNameIndex),
		"新的部分唯一索引必须在位")
}

// Vendor 表同样清理旧的 uk_vendor_name_delete_at。
func TestLegacyVendorCompositeIndexIsDropped(t *testing.T) {
	db := openTestDB(t, &Vendor{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	require.NoError(t, db.Exec(
		`CREATE UNIQUE INDEX uk_vendor_name_delete_at ON vendors (name, deleted_at)`).Error)

	require.NoError(t, migrateVendorMetaUniqueIndex())

	require.False(t, db.Migrator().HasIndex(&Vendor{}, "uk_vendor_name_delete_at"),
		"升级后旧复合唯一索引必须被删除")
}

// 旧索引不存在时是空操作，不得报错——新库或已清理过的库都会走这条路径。
func TestLegacySoftDeleteCompositeIndexDropIsNoopWhenAbsent(t *testing.T) {
	db := openTestDB(t, &Vendor{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	require.NoError(t, migrateVendorMetaUniqueIndex())
	require.False(t, db.Migrator().HasIndex(&Vendor{}, "uk_vendor_name_delete_at"))
}
