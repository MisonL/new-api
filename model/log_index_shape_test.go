package model

import (
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

// parsedLogIndexColumns 返回 GORM 从 Log 标签解析出的每个索引的列序列。
// 与 parsedLogIndexNames 不同，这里保留列顺序：索引是否重复、复合索引是否
// 真的包含 type 列，只有列序列能回答。
func parsedLogIndexColumns(t *testing.T) map[string][]string {
	t.Helper()
	s, err := schema.Parse(&Log{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)

	columns := make(map[string][]string)
	for _, idx := range s.ParseIndexes() {
		cols := make([]string, 0, len(idx.Fields))
		for _, field := range idx.Fields {
			cols = append(cols, field.DBName)
		}
		columns[idx.Name] = cols
	}
	return columns
}

func indexColumnKey(columns []string) string {
	return strings.Join(columns, ",")
}

// 索引名不同但列完全相同，在数据库里就是两份同形索引：写入时双倍维护，
// 查询时优化器只会用其中一个。eade672f6 收敛索引时删掉了 Type 上的
// priority:2 声明，六个 *_type_created_at 复合索引退化成对应 (维度, created_at)
// 索引的副本（升级库因索引已存在仍是三列，全新库建成两列，schema 分叉）。
func TestLogIndexesHaveNoDuplicateColumnSequences(t *testing.T) {
	columns := parsedLogIndexColumns(t)

	seen := make(map[string]string, len(columns))
	for name, cols := range columns {
		key := indexColumnKey(cols)
		if other, ok := seen[key]; ok {
			t.Errorf("indexes %s and %s are declared with identical columns (%s)", name, other, key)
			continue
		}
		seen[key] = name
	}
}

// 按 type 过滤的列表查询（后台日志页带 type 条件 + 单一维度）需要
// (维度, type, created_at) 形状的复合索引，否则 type 只能靠回表过滤。
// 这个不变量同时保证全新库与已升级库建出同一形状。
func TestLogTypeCompositeIndexesIncludeTypeColumn(t *testing.T) {
	columns := parsedLogIndexColumns(t)

	typeComposite := []string{
		"idx_logs_user_type_created_at",
		"idx_logs_username_type_created_at",
		"idx_logs_token_type_created_at",
		"idx_logs_model_type_created_at",
		"idx_logs_channel_type_created_at",
		"idx_logs_group_type_created_at",
	}
	sort.Strings(typeComposite)
	for _, name := range typeComposite {
		cols, ok := columns[name]
		require.True(t, ok, "index %s must be declared", name)
		require.Contains(t, cols, "type", "index %s must include the type column", name)
		require.Equal(t, "created_at", cols[len(cols)-1],
			"index %s must keep created_at as its trailing column", name)
	}
}
