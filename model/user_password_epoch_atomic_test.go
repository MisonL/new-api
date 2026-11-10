package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// 改密码必须同时生效「新密码」与「会话世代递增」。两者若分两次提交，第二次
// 写失败时新密码已经生效而世代没变，被盗的旧 Cookie 仍能通过鉴权，受害者
// 改密码也无法把盗号者赶下线——账号恢复手段失效。这里用 SQLite trigger 让
// 世代更新必然失败，验证密码写入被一并回滚。
func TestPasswordChangeRollsBackWhenEpochBumpFails(t *testing.T) {
	t.Cleanup(func() {
		DB = openTestDB(t, &User{})
	})
	DB = openTestDB(t, &User{})
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	user := &User{
		Username:    "epoch-atomic-user",
		Password:    "12345678",
		DisplayName: "Epoch Atomic",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Email:       "epoch-atomic@example.com",
	}
	require.NoError(t, user.Insert(0))
	userId := user.Id
	var before User
	require.NoError(t, DB.First(&before, userId).Error)

	// 任何对 session_epoch 的 UPDATE 都失败。
	require.NoError(t, DB.Exec(`
CREATE TRIGGER reject_epoch_bump BEFORE UPDATE OF session_epoch ON users
BEGIN
  SELECT RAISE(ABORT, 'epoch bump rejected');
END;
`).Error)

	reload := func() User {
		var got User
		require.NoError(t, DB.First(&got, userId).Error)
		return got
	}

	t.Run("User.Update", func(t *testing.T) {
		u := reload()
		u.Password = "new-password-1"
		err := u.Update(true)
		require.Error(t, err, "世代递增失败必须让整次改密报错")
		require.Equal(t, before.Password, reload().Password,
			"世代递增失败时新密码不得落库，否则旧会话仍有效")
	})

	t.Run("User.Edit", func(t *testing.T) {
		u := reload()
		u.Password = "new-password-2"
		err := u.Edit(true)
		require.Error(t, err)
		require.Equal(t, before.Password, reload().Password)
	})

	t.Run("ResetUserPasswordByEmail", func(t *testing.T) {
		err := ResetUserPasswordByEmail(before.Email, "new-password-3")
		require.Error(t, err)
		require.Equal(t, before.Password, reload().Password)
	})

	// 对照：不涉及改密码的更新仍能成功（trigger 只拦 session_epoch 列）。
	u := reload()
	u.DisplayName = "renamed"
	require.NoError(t, u.Update(false))
	require.Equal(t, "renamed", reload().DisplayName)
}
