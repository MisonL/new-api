package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ResetUserPasswordByEmail 是忘记密码流程的落库点。users.email 非唯一索引，
// 按 email 直接 UPDATE 会改写所有匹配账户的密码并把新明文返回给请求者
// （见 controller/oauth.go resolveOAuthRegistrationEmail 的注释：共享地址即
// 跨账户接管）。绑定与注册侧的复查只保证新数据不产生重复，历史重复行仍然
// 存在，消费侧必须 fail-closed。

func openPasswordResetTestDB(t *testing.T) {
	t.Helper()
	previous := DB
	DB = openTestDB(t, &User{})
	t.Cleanup(func() { DB = previous })
}

func TestResetUserPasswordByEmailRejectsDuplicateEmailRows(t *testing.T) {
	openPasswordResetTestDB(t)
	createIdentityTestUser(t, DB, "dup-one", "dup@example.com")
	createIdentityTestUser(t, DB, "dup-two", "dup@example.com")

	err := ResetUserPasswordByEmail("dup@example.com", "new-password")

	require.Error(t, err, "重复邮箱必须拒绝重置，而不是改写所有匹配行")

	var first, second User
	require.NoError(t, DB.Where("username = ?", "dup-one").First(&first).Error)
	require.NoError(t, DB.Where("username = ?", "dup-two").First(&second).Error)
	require.Equal(t, "hashed-placeholder", first.Password, "dup-one 的密码不得被改写")
	require.Equal(t, "hashed-placeholder", second.Password, "dup-two 的密码不得被改写")
}

func TestResetUserPasswordByEmailUpdatesSingleAccount(t *testing.T) {
	openPasswordResetTestDB(t)
	createIdentityTestUser(t, DB, "solo", "solo@example.com")
	createIdentityTestUser(t, DB, "other", "other@example.com")

	require.NoError(t, ResetUserPasswordByEmail("solo@example.com", "new-password"))

	var solo, other User
	require.NoError(t, DB.Where("username = ?", "solo").First(&solo).Error)
	require.NoError(t, DB.Where("username = ?", "other").First(&other).Error)
	require.NotEqual(t, "hashed-placeholder", solo.Password, "唯一匹配的账户必须被重置")
	require.Equal(t, "hashed-placeholder", other.Password, "其他账户不得被波及")
}

func TestResetUserPasswordByEmailIgnoresSoftDeletedRows(t *testing.T) {
	openPasswordResetTestDB(t)
	createIdentityTestUser(t, DB, "solo", "solo@example.com")
	createIdentityTestUser(t, DB, "tombstone", "solo@example.com")
	require.NoError(t, DB.Where("username = ?", "tombstone").Delete(&User{}).Error)

	require.NoError(t, ResetUserPasswordByEmail("solo@example.com", "new-password"))

	var solo User
	require.NoError(t, DB.Where("username = ?", "solo").First(&solo).Error)
	require.NotEqual(t, "hashed-placeholder", solo.Password, "墓碑行不应挡住在用账户的重置")
}

func TestResetUserPasswordByEmailRejectsUnknownEmail(t *testing.T) {
	openPasswordResetTestDB(t)

	err := ResetUserPasswordByEmail("nobody@example.com", "new-password")

	require.Error(t, err, "不存在的邮箱必须拒绝，而不是生成一个无处生效的密码")
}

func TestResetUserPasswordByEmailRejectsEmptyInput(t *testing.T) {
	openPasswordResetTestDB(t)

	require.Error(t, ResetUserPasswordByEmail("", "new-password"))
	require.Error(t, ResetUserPasswordByEmail("a@example.com", ""))
}
