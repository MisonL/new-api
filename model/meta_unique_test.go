package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 软删除表的复合唯一索引 (name, deleted_at) 对未删除行形同虚设：SQL 里
// NULL 互不相等，两行 name 相同、deleted_at 均为 NULL 时索引判定不冲突。
// 索引必须是 deleted_at IS NULL 的部分唯一索引才有约束力。
func TestLiveModelNameIsUniqueUnderSoftDelete(t *testing.T) {
	db := openTestDB(t, &Model{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	first := &Model{ModelName: "duplicate-probe", Status: 1}
	require.NoError(t, first.Insert())

	second := &Model{ModelName: "duplicate-probe", Status: 1}
	err := second.Insert()
	require.Error(t, err, "同名未删除行必须被拒绝，否则名称唯一性只靠先查后插的竞态窗口")
}

func TestLiveVendorNameIsUniqueUnderSoftDelete(t *testing.T) {
	db := openTestDB(t, &Vendor{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	require.NoError(t, (&Vendor{Name: "dup-vendor", Status: 1}).Insert())
	err := (&Vendor{Name: "dup-vendor", Status: 1}).Insert()
	require.Error(t, err, "同名未删除供应商必须被拒绝")
}
