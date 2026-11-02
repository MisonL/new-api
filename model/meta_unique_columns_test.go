package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// migrateSoftDeleteUniqueIndexWithExecutor 的 MySQL 分支不调用 AutoMigrate，
// 既有表只能靠 modelMetaColumns / vendorMetaColumns 那份清单补齐缺列。
// 模型加了新列却忘记同步清单时，MySQL 上的既有表会缺列，而列同步不会失败、
// 只在读写该列时报错。这条用例把清单与模型定义钉在一起。
func TestMetaUniqueColumnListsMatchModels(t *testing.T) {
	previous := DB
	DB = openTestDB(t)
	t.Cleanup(func() { DB = previous })

	assertColumnListMatchesModel(t, &Model{}, modelMetaColumns)
	assertColumnListMatchesModel(t, &Vendor{}, vendorMetaColumns)
}

// assertColumnListMatchesModel 取出模型里真实落库的字段名（排除 gorm:"-" 的
// 内存字段），要求它等于给定的清单集合。
func assertColumnListMatchesModel(t *testing.T, model interface{}, columns []string) {
	t.Helper()

	stmt := &gorm.Statement{DB: DB}
	require.NoError(t, stmt.Parse(model))

	declared := make(map[string]struct{}, len(stmt.Schema.Fields))
	for _, field := range stmt.Schema.Fields {
		if field.IgnoreMigration || field.DBName == "" || field.PrimaryKey {
			continue
		}
		declared[field.DBName] = struct{}{}
	}

	listed := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		listed[column] = struct{}{}
	}

	for column := range declared {
		_, ok := listed[column]
		require.Truef(t, ok, "模型新增了列 %q，必须同步进迁移的列清单，否则 MySQL 既有表不会补列", column)
	}
	for column := range listed {
		_, ok := declared[column]
		require.Truef(t, ok, "列清单里的 %q 在模型里不存在，清单已过期", column)
	}
}
