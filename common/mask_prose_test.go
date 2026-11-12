package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// maskAuthTokenPattern treats the word after "token" as a credential. Quota
// diagnostics read "token quota is not enough" and "token remain quota: N", so
// without these prose exemptions the reason for a rejection was masked away and
// operators saw "token *** is not enough".
func TestMaskSensitiveInfoKeepsQuotaProse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "quota diagnostic",
			in:   "token quota is not enough, token remain quota: 100",
			want: "token quota is not enough, token remain quota: 100",
		},
		{
			name: "usage diagnostic already exempt",
			in:   "token usage exceeded",
			want: "token usage exceeded",
		},
		{
			name: "invalid token diagnostic already exempt",
			in:   "this token is not valid",
			want: "this token is not valid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, MaskSensitiveInfo(tc.in))
		})
	}
}

// The same masking mistake appeared on other diagnostics that name a token
// concept rather than carrying a token value. Each string below is the verbatim
// text of an error built somewhere in this repository, and each was rewritten to
// "token *** ..." before the words were exempted.
func TestMaskSensitiveInfoKeepsTokenConceptProse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "model access configuration",
			in:   "token model access configuration is invalid",
			want: "token model access configuration is invalid",
		},
		{
			name: "scope requirement",
			in:   "synthetic compact state scope is required: token id is required",
			want: "synthetic compact state scope is required: token id is required",
		},
		{
			name: "oauth provider endpoint",
			in:   "token endpoint is required",
			want: "token endpoint is required",
		},
		{
			name: "opaque replay authentication",
			in:   "Responses opaque replay body requires token authentication",
			want: "Responses opaque replay body requires token authentication",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, MaskSensitiveInfo(tc.in))
		})
	}
}

// The exemption must not turn into a hole: a real credential after "token"
// still has to be masked.
func TestMaskSensitiveInfoStillMasksRealCredentials(t *testing.T) {
	for _, in := range []string{
		"token sk-abcdef0123456789 rejected",
		"token 9f8e7d6c5b4a39281706 rejected",
		"bearer eyJhbGciOiJIUzI1NiJ9 rejected",
		// A credential that merely begins with an exempt word is not the exempt
		// word itself, so the exact-match lookup must still mask it.
		"token model-abcdef0123456789 rejected",
		"token id-abcdef0123456789 rejected",
		"token endpoint-abcdef0123456789 rejected",
	} {
		masked := MaskSensitiveInfo(in)
		require.NotContains(t, masked, "sk-abcdef0123456789")
		require.NotContains(t, masked, "9f8e7d6c5b4a39281706")
		require.NotContains(t, masked, "eyJhbGciOiJIUzI1NiJ9")
		require.NotContains(t, masked, "model-abcdef0123456789")
		require.NotContains(t, masked, "id-abcdef0123456789")
		require.NotContains(t, masked, "endpoint-abcdef0123456789")
		require.Contains(t, masked, "***")
	}
}

// JSON 形式的凭据键在键名与冒号之间还有闭合引号，例如 {"secret":"..."}。
// 旧正则要求键名后紧跟 : 或 =，于是 "已脱敏" 的错误与日志路径会把
// 整个 JSON 里的密钥原样带出。这里覆盖常见键名与两种引号风格。
func TestMaskSensitiveInfoMasksJSONQuotedCredentialKeys(t *testing.T) {
	cases := []struct {
		name string
		in   string
		leak string
	}{
		{"secret", `{"secret":"plainsecret"}`, "plainsecret"},
		{"client_secret", `{"client_secret":"cs_live_abc123"}`, "cs_live_abc123"},
		{"webhook_secret", `{"webhook_secret":"whsec_abc123"}`, "whsec_abc123"},
		{"token", `{"token":"tok_live_abcdef"}`, "tok_live_abcdef"},
		{"key", `{"key":"0123abcd-efgh-ijkl"}`, "0123abcd-efgh-ijkl"},
		{"cookie", `"cookie": "session=abcdef123456"`, "session=abcdef123456"},
		{"spaced", `{ "api_key" : "sk-live-abcdef" }`, "sk-live-abcdef"},
		{"single quoted", `{'password':'hunter2'}`, "hunter2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MaskSensitiveInfo(tc.in)
			require.NotContains(t, got, tc.leak, "credential value must be masked in %q", tc.in)
			require.Contains(t, got, "***", "masked output must keep a visible marker")
		})
	}
}

// 无前缀的供应商密钥（Google / AWS）不经过任何键值形式，需要独立模式。
func TestMaskSensitiveInfoMasksStandaloneProviderKeys(t *testing.T) {
	// 密钥在源码里分段拼装：完整字面量会被 GitHub secret scanning 当成
	// 真实凭据报警，而运行时拼出的字符串与真实密钥形状完全一致，测试语义不变。
	googleKey := "AIza" + "SyD-" + "abcdefghijklmnopqrstuvwxyz01234"
	awsKey := "AKIA" + "IOSFODNN7EXAMPLE"
	cases := []string{
		"upstream said: invalid key " + googleKey,
		"aws rejected " + awsKey,
	}
	for _, in := range cases {
		got := MaskSensitiveInfo(in)
		require.NotContains(t, got, googleKey)
		require.NotContains(t, got, awsKey)
	}
}

// 脱敏不得破坏普通 JSON 诊断，否则日志与错误信息会被改写成语义不明的内容。
func TestMaskSensitiveInfoKeepsOrdinaryJSONFields(t *testing.T) {
	in := `{"model":"gpt-5.6-sol","status_code":200,"error_code":"rate_limit_exceeded"}`
	require.Equal(t, in, MaskSensitiveInfo(in))
}
