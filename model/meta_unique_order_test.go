package model

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// 启动时的大 AutoMigrate 会按结构体标签把唯一索引直接建出来，而它跑在
// 各专用迁移之前。对"必须先归并或改写存量行才能建索引"的表，这意味着
// 索引抢在专用迁移前面建，存量数据让建索引失败并把整个启动打断：
// 50dce9191 修的是 quota_data 的这个顺序，Model/Vendor 是同一类隐患
// （MySQL 上 where:deleted_at IS NULL 被静默丢弃，建出的是普通唯一索引，
// 只要有过"软删除后同名重建"就 Error 1062 启动失败）。
//
// 这条用例把契约固化下来：这些表只能由各自的专用迁移创建与同步。
func TestBigAutoMigrateListExcludesDedicatedMigrationTables(t *testing.T) {
	dedicated := []interface{}{
		&Model{},
		&Vendor{},
		&QuotaData{},
		&PrefillGroup{},
	}
	for _, model := range dedicated {
		name := reflect.TypeOf(model).Elem().Name()
		for _, entry := range autoMigrateModels {
			require.NotEqualf(t, name, reflect.TypeOf(entry).Elem().Name(),
				"%s 的索引依赖先处理存量数据，不能放进大 AutoMigrate 列表，否则索引会先于专用迁移建出并打断启动", name)
		}
	}
	require.NotEmpty(t, autoMigrateModels, "大 AutoMigrate 列表不应为空")
}

// migrateDB 是启动迁移的真实入口。Model/Vendor 移出大列表后，表的创建、
// 列同步与部分唯一索引都必须仍由专用迁移完成，否则全新安装会缺表缺索引。
func TestMigrateDBOwnsModelAndVendorUniqueIndexes(t *testing.T) {
	db := openTestDB(t)
	previousDB, previousLogDB := DB, LOG_DB
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	require.NoError(t, migrateDB())

	require.True(t, db.Migrator().HasTable(&Model{}), "migrateDB 必须建出 models 表")
	require.True(t, db.Migrator().HasTable(&Vendor{}), "migrateDB 必须建出 vendors 表")
	require.True(t, db.Migrator().HasIndex(&Model{}, modelMetaNameIndex))
	require.True(t, db.Migrator().HasIndex(&Vendor{}, vendorMetaNameIndex))

	// 索引必须是"活动行内唯一"的部分索引：同名活动行被拒绝，软删除后同名可重建。
	require.NoError(t, (&Model{ModelName: "migrate-db-model", Status: 1}).Insert())
	require.Error(t, (&Model{ModelName: "migrate-db-model", Status: 1}).Insert())

	var first Model
	require.NoError(t, db.Where("model_name = ?", "migrate-db-model").First(&first).Error)
	require.NoError(t, db.Delete(&Model{}, first.Id).Error)
	require.NoError(t, (&Model{ModelName: "migrate-db-model", Status: 1}).Insert(),
		"软删除后必须能重建同名模型")

	require.NoError(t, (&Vendor{Name: "migrate-db-vendor", Status: 1}).Insert())
	require.Error(t, (&Vendor{Name: "migrate-db-vendor", Status: 1}).Insert())

	var firstVendor Vendor
	require.NoError(t, db.Where("name = ?", "migrate-db-vendor").First(&firstVendor).Error)
	require.NoError(t, db.Delete(&Vendor{}, firstVendor.Id).Error)
	require.NoError(t, (&Vendor{Name: "migrate-db-vendor", Status: 1}).Insert(),
		"软删除后必须能重建同名供应商")
}
