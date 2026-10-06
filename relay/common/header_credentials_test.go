package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// pass_headers copies a named header straight off the incoming client request
// into the upstream request. A client's `Authorization` is their new-api
// token, so forwarding it hands that token to a third-party upstream. The rule
// has to name the header explicitly, which does not help: a copied channel
// configuration or a preset can name one, and the operator has no way to see
// that the caller's request carries a credential under that name.

func passHeadersInfo(headerNames []any, requestHeaders map[string]string) *RelayInfo {
	return &RelayInfo{
		RequestHeaders: requestHeaders,
		ChannelMeta: &ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": headerNames,
					},
				},
			},
		},
	}
}

// TestApplyParamOverridePassHeadersRejectsCredentialHeaders is the regression:
// a credential header must be refused loudly rather than forwarded. Swallowing
// it would leave the operator believing the header is being passed.
func TestApplyParamOverridePassHeadersRejectsCredentialHeaders(t *testing.T) {
	for _, name := range []string{
		"Authorization",
		"Proxy-Authorization",
		"Api-Key",
		"X-Api-Key",
		"X-Goog-Api-Key",
		"Cookie",
	} {
		t.Run(name, func(t *testing.T) {
			info := passHeadersInfo([]any{name}, map[string]string{name: "client-secret"})

			_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

			require.Errorf(t, err, "pass_headers 不得透传凭证头 %s", name)
			require.NotEqual(t, "client-secret", info.RuntimeHeadersOverride[normalizeHeaderContextKey(name)],
				"客户端凭证不得进入上游请求头")
		})
	}
}

// TestApplyParamOverridePassHeadersKeepsNormalHeaders is the reverse check:
// the denylist must not turn pass_headers into a no-op.
func TestApplyParamOverridePassHeadersKeepsNormalHeaders(t *testing.T) {
	info := passHeadersInfo(
		[]any{"Session_id", "Originator"},
		map[string]string{"Session_id": "sess-123", "Originator": "Codex CLI"},
	)

	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

	require.NoError(t, err)
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
}

// TestIsCredentialHeader pins the shared helper both call sites rely on.
func TestIsCredentialHeader(t *testing.T) {
	require.True(t, IsCredentialHeader("Authorization"))
	require.True(t, IsCredentialHeader("  api-KEY  "))
	require.False(t, IsCredentialHeader("Session_id"))
	require.False(t, IsCredentialHeader(""))
}
