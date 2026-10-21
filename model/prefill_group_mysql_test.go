package model

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PrefillGroup 的 Name 字段用 gorm 标签声明了部分唯一索引：
//
//	uniqueIndex:uk_prefill_name,where:deleted_at IS NULL
//
// 这个 where 只有支持 BuildIndexOptions 的方言才会落进 DDL。GORM 的 MySQL
// 驱动没有实现该接口，标签里的条件被静默丢弃，实际建出的是 name 上的
// 普通唯一索引。后果：软删除一个组之后，同名组永远无法再创建（逻辑删除行
// 仍占着唯一键），而 IsPrefillGroupNameDuplicated 只查未删除行、会放行。
// 只能在真实 MySQL 上验证 DDL 形态。
// 仅在 NEW_API_TEST_MYSQL_DSN 指向一次性数据库时运行。
func openPrefillMySQLDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL partial-index semantics not covered by this run")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	previous := DB
	DB = db
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS prefill_groups").Error
		DB = previous
	})
	// 走生产迁移入口，而不是直接 AutoMigrate：MySQL 上的退化索引正是由
	// AutoMigrate 建出来的，只有迁移函数能把它替换掉。
	require.NoError(t, migratePrefillGroupTable())
	return db
}

func TestPrefillGroupNameReusableAfterSoftDeleteOnMySQL(t *testing.T) {
	db := openPrefillMySQLDB(t)

	var activeNameDDL string
	require.NoError(t, db.Raw(
		"SELECT column_name FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'prefill_groups' AND column_name = ?",
		activePrefillNameColumn,
	).Scan(&activeNameDDL).Error)
	require.Equal(t, activePrefillNameColumn, activeNameDDL, "MySQL 迁移必须建立活动行生成列")

	var indexColumns string
	require.NoError(t, db.Raw(
		"SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'prefill_groups' AND index_name = ?",
		prefillNameIndex,
	).Scan(&indexColumns).Error)
	require.Equal(t, activePrefillNameColumn, indexColumns,
		"唯一索引必须落在活动行生成列上，而不是 name 列")

	first := &PrefillGroup{Name: "shared-name", Type: "model", Items: JSONValue(`["a"]`)}
	require.NoError(t, db.Create(first).Error)

	require.NoError(t, DeletePrefillGroupByID(first.Id), "软删除必须成功")

	duplicated, err := IsPrefillGroupNameDuplicated(0, "shared-name")
	require.NoError(t, err)
	require.False(t, duplicated, "已删除行不参与重名判断")

	second := &PrefillGroup{Name: "shared-name", Type: "model", Items: JSONValue(`["b"]`)}
	require.NoError(t, db.Create(second).Error,
		"软删除后同名组必须可以重建；MySQL 上被忽略的 where 条件会让这里唯一键冲突")
}
