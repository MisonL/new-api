package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 通知失败的错误会被 NotifyRootUser 写进系统日志。url.Error 默认带完整 URL，
// 而 Bark 的设备 key 在路径里、Gotify 的 token 在 query 里，因此错误文案
// 必须先脱敏再返回。
func TestNotifyTransportErrorMasksBarkDeviceKey(t *testing.T) {
	raw := errors.New(`Get "https://api.day.app/DEVICEKEY123/hello": dial tcp 10.1.2.3:443: connect: connection refused`)
	err := notifyTransportError("bark", raw)
	require.Error(t, err)
	msg := err.Error()
	require.NotContains(t, msg, "DEVICEKEY123")
	require.NotContains(t, msg, "10.1.2.3")
	require.Contains(t, msg, "failed to send bark request")
}

func TestNotifyTransportErrorMasksGotifyToken(t *testing.T) {
	raw := errors.New(`Post "https://gotify.example.com/message?token=AAtokensecret": EOF`)
	err := notifyTransportError("gotify", raw)
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "AAtokensecret"), "gotify token must not reach the log: %s", err.Error())
}

func TestNotifyTransportErrorPassesNil(t *testing.T) {
	require.NoError(t, notifyTransportError("bark", nil))
}

// webhook 地址可能带 userinfo 或 query 中的令牌，同样不能进日志。
func TestNotifyTransportErrorMasksWebhookURL(t *testing.T) {
	raw := errors.New(`Post "https://user:pass@hooks.example.com/cb?token=whooksecret": EOF`)
	err := notifyTransportError("webhook", raw)
	require.Error(t, err)
	msg := err.Error()
	require.NotContains(t, msg, "whooksecret")
	require.NotContains(t, msg, "pass")
}
