package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// userSettingNotificationSecrets 列出用户 setting 中属于第三方推送/回调的凭据。
// 它们只对该用户本人有意义，管理员也没有使用或查看它们的需要。
var userSettingNotificationSecrets = []string{"webhook_secret", "gotify_token", "bark_url"}

// 管理员用户详情接口此前直接返回 setting 原文，包含 webhook_secret、
// gotify_token 与带设备 key 的 bark_url。管理员编辑用户时只需要非敏感字段。
func TestUserForAdminViewStripsNotificationSecrets(t *testing.T) {
	user := &User{Id: 7, Username: "target", Role: common.RoleCommonUser}
	user.SetSetting(dto.UserSetting{
		NotifyType:            dto.NotifyTypeWebhook,
		QuotaWarningThreshold: 1000,
		WebhookUrl:            "https://example.com/hook",
		WebhookSecret:         "whsec_live_secret",
		GotifyUrl:             "https://gotify.example.com",
		GotifyToken:           "AAtoken_secret",
		BarkUrl:               "https://api.day.app/DEVICEKEY/hello",
	})

	view := user.ForAdminView()
	require.Equal(t, user.Id, view.Id)
	require.Equal(t, user.Username, view.Username)

	setting := view.GetSetting()
	require.Equal(t, dto.NotifyTypeWebhook, setting.NotifyType)
	require.Equal(t, float64(1000), setting.QuotaWarningThreshold)
	require.Equal(t, "https://example.com/hook", setting.WebhookUrl)
	require.Equal(t, "https://gotify.example.com", setting.GotifyUrl)
	require.Empty(t, setting.WebhookSecret)
	require.Empty(t, setting.GotifyToken)
	require.Empty(t, setting.BarkUrl)

	for _, key := range userSettingNotificationSecrets {
		require.NotContains(t, view.Setting, key)
	}
	// 原对象不被修改，其它内部调用仍能读到完整设置。
	require.Contains(t, user.Setting, "whsec_live_secret")
}
