package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// 会话 Cookie 有效期 30 天，且没有服务端吊销手段：用户修改或重置密码后，
// 之前签发的 Cookie 仍然可用满 30 天。盗号者只要拿到过一次会话，受害者改
// 密码也无法把它赶下线。这里要求密码变更递增会话世代，鉴权时比对世代，
// 不匹配即视为已吊销。
func TestUserPasswordChangeBumpsSessionEpoch(t *testing.T) {
	DB = openTestDB(t, &User{})
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	user := &User{
		Username:    "session-epoch-user",
		Password:    "12345678",
		DisplayName: "Session Epoch",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Email:       "session-epoch@example.com",
	}
	require.NoError(t, user.Insert(0))
	userId := user.Id

	reload := func() User {
		var got User
		require.NoError(t, DB.First(&got, userId).Error)
		return got
	}
	require.Equal(t, int64(0), reload().SessionEpoch, "新用户会话世代从 0 开始")

	// 只改资料不吊销会话
	profile := reload()
	profile.DisplayName = "Session Epoch Renamed"
	require.NoError(t, profile.Update(false))
	require.Equal(t, int64(0), reload().SessionEpoch, "改资料不得吊销已有会话")

	// 自助改密码吊销
	withPassword := reload()
	withPassword.Password = "87654321"
	require.NoError(t, withPassword.Update(true))
	require.Equal(t, int64(1), reload().SessionEpoch, "改密码必须递增会话世代")

	// 管理员通过 Edit 改密码同样吊销
	adminEdit := reload()
	adminEdit.Password = "87654321a"
	require.NoError(t, adminEdit.Edit(true))
	require.Equal(t, int64(2), reload().SessionEpoch, "管理员改密码必须递增会话世代")

	// 邮箱重置密码同样吊销
	require.NoError(t, ResetUserPasswordByEmail(user.Email, "87654321b"))
	require.Equal(t, int64(3), reload().SessionEpoch, "邮箱重置密码必须递增会话世代")
}
