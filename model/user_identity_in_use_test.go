package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// identityInUse 是六处 Is*AlreadyTaken 的公共实现。这些身份检查是注册/绑定的
// 安全边界：必须回答"这个邮箱 / 第三方 id 是否已经有人用"，而不是"是否恰好
// 有一行"。

func newIdentityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}))
	return db
}

func createIdentityTestUser(t *testing.T, db *gorm.DB, username string, email string) {
	t.Helper()
	require.NoError(t, db.Create(&User{
		Username: username,
		Email:    email,
		Password: "hashed-placeholder",
		AffCode:  "aff-" + username,
	}).Error)
}

// 重复身份（同一邮箱两行）必须判为已占用。这个测试同时固定住前置条件：
// 库里确实存在两行同邮箱记录，断言不是靠"恰好一行"侥幸成立。
func TestIdentityInUseTreatsDuplicateRowsAsTaken(t *testing.T) {
	db := newIdentityTestDB(t)
	createIdentityTestUser(t, db, "dup-one", "dup@example.com")
	createIdentityTestUser(t, db, "dup-two", "dup@example.com")

	var rows int64
	require.NoError(t, db.Unscoped().Model(&User{}).Where("email = ?", "dup@example.com").Count(&rows).Error)
	require.Equal(t, int64(2), rows, "fixture precondition: two rows share the address")

	require.True(t, identityInUse(db.Unscoped().Where("email = ?", "dup@example.com")),
		"an address used by two accounts is still taken")
}

func TestIdentityInUseReportsUnusedIdentityAsFree(t *testing.T) {
	db := newIdentityTestDB(t)
	createIdentityTestUser(t, db, "solo", "solo@example.com")

	require.False(t, identityInUse(db.Unscoped().Where("email = ?", "nobody@example.com")))
	require.True(t, identityInUse(db.Unscoped().Where("email = ?", "solo@example.com")))
}

// 身份检查刻意包含软删行（调用方传 Unscoped），这样被注销账号的邮箱不会被
// 静默回收；传 scoped 时同一行不可见。这里把两种作用域都固定下来，避免日后
// 有人把作用域改掉却以为只是"清理写法"。
func TestIdentityInUseHonoursCallerScope(t *testing.T) {
	db := newIdentityTestDB(t)
	createIdentityTestUser(t, db, "gone", "gone@example.com")
	require.NoError(t, db.Where("username = ?", "gone").Delete(&User{}).Error)

	require.True(t, identityInUse(db.Unscoped().Where("email = ?", "gone@example.com")),
		"a soft-deleted account still holds its address")
	require.False(t, identityInUse(db.Where("email = ?", "gone@example.com")),
		"a scoped query cannot see the tombstone")
}

// 计数失败时必须保守判为"已占用"。身份检查没有唯一索引兜底（email 只是普通
// 索引），把"查不出来"当成"没人用"会直接放行重复注册。
func TestIdentityInUseFailsClosedWhenCountFails(t *testing.T) {
	db := newIdentityTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&User{}))

	require.True(t, identityInUse(db.Unscoped().Where("email = ?", "x@example.com")),
		"a failed count must not be reported as a free identity")
}
