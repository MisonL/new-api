package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestFormatChannelTestResponseLogDoesNotIncludeBody(t *testing.T) {
	body := []byte(`{"instructions":"sensitive prompt","output":"sensitive result"}`)

	got := formatChannelTestResponseLog(205, http.StatusOK, len(body))

	require.Equal(t, "testing channel #205 completed: status_code=200 response_bytes=63", got)
	require.NotContains(t, got, "sensitive prompt")
	require.NotContains(t, got, "sensitive result")
}

// localErr 可能来自 URL 解析或构造请求失败，Go 的错误文本会原样嵌入完整
// URL。Vertex 的 api_key 模式与 Baidu 的 access_token 都把密钥放在 URL 里，
// 因此渠道测试的错误响应必须脱敏。同级分支用 newAPIError.Error()（内部
// 已脱敏），localErr 这一支曾是唯一裸回显。
func TestChannelTestErrorResponseMasksSecretsInLocalErr(t *testing.T) {
	raw := `parse "https://bad host/v1beta/models/gemini-2.0-flash:generateContent?key=AIzaSECRETKEY123456": invalid character " " in host name`

	masked := common.MaskSensitiveInfo(raw)

	require.NotContains(t, masked, "AIzaSECRETKEY123456",
		"the channel key must not survive masking")
	require.Contains(t, masked, "invalid character",
		"the diagnostic part of the message must be preserved")
}
