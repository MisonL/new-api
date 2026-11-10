package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// User.Update 用整对象 Updates 回写调用方快照。若快照里带着 password 哈希，
// 在「读到旧对象 -> 用户重置密码 -> 保存资料」这一常见管理操作里，旧哈希会被
// 写回去，而这条路径不递增 session_epoch：旧密码重新可登录，且被盗的会话不会
// 因为这次改密失效。Update(false) 不得写 password 列。
func TestUserUpdateWithoutPasswordDoesNotRestoreStalePassword(t *testing.T) {
	t.Cleanup(func() {
		DB = openTestDB(t, &User{})
	})
	DB = openTestDB(t, &User{})
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	const (
		oldPassword = "old-password-123"
		newPassword = "new-password-456"
	)
	user := &User{
		Username:    "stale-password-user",
		Password:    oldPassword,
		DisplayName: "Stale Password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Email:       "stale-password@example.com",
	}
	require.NoError(t, user.Insert(0))
	userId := user.Id

	// 管理员先读到用户对象（含 password 哈希），这一步就是陈旧快照的来源。
	stale, err := GetUserById(userId, true)
	require.NoError(t, err)
	require.NotEmpty(t, stale.Password, "前置条件：管理员读取的快照里带 password")

	var beforeReset User
	require.NoError(t, DB.First(&beforeReset, userId).Error)

	require.NoError(t, ResetUserPasswordByEmail(user.Email, newPassword))

	var afterReset User
	require.NoError(t, DB.First(&afterReset, userId).Error)
	require.True(t, common.ValidatePasswordAndHash(newPassword, afterReset.Password),
		"前置条件：重置后库里是新密码")
	require.Greater(t, afterReset.SessionEpoch, beforeReset.SessionEpoch,
		"前置条件：重置会递增会话世代")

	// 管理员在陈旧快照上保存资料。
	stale.DisplayName = "renamed-after-reset"
	require.NoError(t, stale.Update(false))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, userId).Error)
	require.Equal(t, "renamed-after-reset", reloaded.DisplayName,
		"对照：资料字段仍应正常保存")
	require.True(t, common.ValidatePasswordAndHash(newPassword, reloaded.Password),
		"保存资料不得把陈旧快照里的旧密码写回去")
	require.False(t, common.ValidatePasswordAndHash(oldPassword, reloaded.Password),
		"旧密码不得重新可登录")
	require.Equal(t, afterReset.SessionEpoch, reloaded.SessionEpoch,
		"保存资料不得改动会话世代")
}

// 对照：把 Password 移出整对象回写之后，updatePassword=true 的显式写入必须
// 真的落库，否则「陈旧快照不覆盖密码」会退化成「任何入口都改不了密码」。
func TestUserUpdateWithPasswordWritesNewHash(t *testing.T) {
	t.Cleanup(func() {
		DB = openTestDB(t, &User{})
	})
	DB = openTestDB(t, &User{})
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	const newPassword = "changed-password-789"
	user := &User{
		Username:    "explicit-password-user",
		Password:    "initial-password-000",
		DisplayName: "Explicit Password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Email:       "explicit-password@example.com",
	}
	require.NoError(t, user.Insert(0))
	userId := user.Id

	loaded, err := GetUserById(userId, true)
	require.NoError(t, err)
	loaded.Password = newPassword
	require.NoError(t, loaded.Update(true))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, userId).Error)
	require.True(t, common.ValidatePasswordAndHash(newPassword, reloaded.Password),
		"Update(true) 必须写入新密码哈希")
	require.False(t, common.ValidatePasswordAndHash("initial-password-000", reloaded.Password),
		"旧密码必须失效")
	require.Equal(t, int64(1), reloaded.SessionEpoch,
		"改密码必须递增会话世代")
}
