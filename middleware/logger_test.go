package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeAccessLogPathRedactsQueryValues(t *testing.T) {
	path := "/api/channel/search?keyword=sk-sensitive-value&group=default&group=backup"

	got := sanitizeAccessLogPath(path)

	require.Equal(t, "/api/channel/search?group=REDACTED&group=REDACTED&keyword=REDACTED", got)
	require.NotContains(t, got, "sk-sensitive-value")
	require.NotContains(t, got, "default")
}

func TestSanitizeAccessLogPathRedactsMalformedQuery(t *testing.T) {
	got := sanitizeAccessLogPath("/api/channel/search?keyword=%zz")

	require.Equal(t, "/api/channel/search?REDACTED", got)
}

// 路径体也会进日志。gin 的 param.Path 已被百分号解码，"%0A" 在日志里变成真实
// 换行，把一条访问记录拆成伪造的多行（第十轮：攻击者可用它伪造状态码、IP 与
// 任意日志内容）。换行与回车必须折叠成转义序列，保证一行请求一行日志。
func TestSanitizeAccessLogPathNeutralizesLineBreaksInPath(t *testing.T) {
	// gin 的 param.Path 已解码：%0A 在传入前就是真实换行。
	got := sanitizeAccessLogPath("/v1/chat/completions\n[GIN] 2026/01/01 - 00:00:00 | 200 | fake")

	require.NotContains(t, got, "\n", "路径中的换行必须被中和，否则日志可被拆行伪造")
	require.NotContains(t, got, "\r")
	require.Contains(t, got, "\\n", "换行应保留为转义序列以便排查")
}

// 真实换行（URL 里直接出现的控制字符场景，由调用方传入已解码路径）同样处理。
func TestSanitizeAccessLogPathNeutralizesRawLineBreaks(t *testing.T) {
	got := sanitizeAccessLogPath("/api/x\n[GIN] forged line")

	require.NotContains(t, got, "\n")
	require.Contains(t, got, "\\n")
}
