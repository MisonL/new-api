package oauth

import (
	"regexp"
	"strings"
)

// 上游身份服务的错误响应可能回带 token、密码或整页 HTML，直接拼进错误信息
// 会把敏感内容经 OAuth 失败页回显给浏览器。统一折叠空白、遮蔽敏感键值并
// 截断长度；CAS 校验与 JWT-direct 的票据/用户信息/JWKS 请求共用这一口径。
var oauthSensitiveResponsePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)("?(?:access_token|refresh_token|id_token|token|password|passwd|secret)"?\s*[:=]\s*"?)([^",<\s]+)`),
	regexp.MustCompile(`(?i)(<(?:access_token|refresh_token|id_token|token|password|passwd|secret)>)([^<]*)(</(?:access_token|refresh_token|id_token|token|password|passwd|secret)>)`),
}

func sanitizeOAuthUpstreamErrorBody(raw string) string {
	const maxErrorBodyLength = 200

	collapsed := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if collapsed == "" {
		return ""
	}
	sanitized := collapsed
	for _, pattern := range oauthSensitiveResponsePatterns {
		sanitized = pattern.ReplaceAllString(sanitized, "$1[redacted]$3")
	}
	if len(sanitized) <= maxErrorBodyLength {
		return sanitized
	}
	return sanitized[:maxErrorBodyLength] + "...(truncated)"
}
