package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// pass_headers was guarded against forwarding a client credential upstream, but
// copy_header and move_header read the same client request headers and were
// not. `copy_header {from: Authorization, to: X-Upstream-Auth}` therefore still
// handed the caller's own new-api token to a third-party upstream.
//
// The guard keys on where the value comes from, not on the header's name:
// getHeaderValueFromContext consults header_override before request_headers, so
// an operator who deliberately wrote a value under that name with set_header is
// copying their own configuration and must keep working.

func copyInfo(from string, to string, requestHeaders map[string]string, override map[string]any) *RelayInfo {
	ops := []any{map[string]any{"mode": "copy_header", "from": from, "to": to}}
	return &RelayInfo{
		RequestHeaders: requestHeaders,
		ChannelMeta: &ChannelMeta{
			ParamOverride: map[string]any{"operations": ops},
			HeadersOverride: func() map[string]any {
				if override == nil {
					return nil
				}
				return override
			}(),
		},
	}
}

// TestCopyHeaderRefusesCredentialSource is the regression: a credential must
// not be readable as a copy_header source when it came from the client.
func TestCopyHeaderRefusesCredentialSource(t *testing.T) {
	for _, from := range []string{"Authorization", "api-key", "X-Api-Key", "X-Goog-Api-Key", "Cookie"} {
		t.Run(from, func(t *testing.T) {
			info := copyInfo(from, "X-Upstream-Auth", map[string]string{from: "Bearer client-new-api-token"}, nil)

			_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

			require.Error(t, err, "copy_header 不得以客户端凭证头 %s 为来源", from)
			require.NotEqual(t, "Bearer client-new-api-token", info.RuntimeHeadersOverride["x-upstream-auth"],
				"客户端凭证不得进入上游请求头")
		})
	}
}

// TestMoveHeaderRefusesCredentialSource covers move_header, which delegates to
// copy_header and must inherit the same refusal.
func TestMoveHeaderRefusesCredentialSource(t *testing.T) {
	info := &RelayInfo{
		RequestHeaders: map[string]string{"Authorization": "Bearer client-new-api-token"},
		ChannelMeta: &ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{"mode": "move_header", "from": "Authorization", "to": "X-Upstream-Auth"},
				},
			},
		},
	}

	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

	require.Error(t, err, "move_header 不得以客户端凭证头为来源")
	require.NotEqual(t, "Bearer client-new-api-token", info.RuntimeHeadersOverride["x-upstream-auth"])
}

// TestCopyHeaderKeepsNormalSource is the reverse check: an ordinary header must
// still copy, so the refusal does not widen into "copy_header is broken".
func TestCopyHeaderKeepsNormalSource(t *testing.T) {
	info := copyInfo("X-Client-Request-Id", "Session_id",
		map[string]string{"X-Client-Request-Id": "req-123"}, nil)

	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

	require.NoError(t, err)
	require.Equal(t, "req-123", info.RuntimeHeadersOverride["session_id"])
}

// TestCopyHeaderAllowsOperatorConfiguredCredentialName pins the boundary. When
// the operator has themselves written a value under the credential name, the
// copy reads that value -- not the caller's token -- so it must be allowed.
// Without this case the guard could be "fixed" by rejecting the name outright,
// which would break a legitimate configuration.
func TestCopyHeaderAllowsOperatorConfiguredCredentialName(t *testing.T) {
	info := &RelayInfo{
		RequestHeaders: map[string]string{"Authorization": "Bearer client-new-api-token"},
		ChannelMeta: &ChannelMeta{
			HeadersOverride: map[string]any{"Authorization": "Bearer operator-configured-key"},
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{"mode": "copy_header", "from": "Authorization", "to": "X-Upstream-Auth"},
				},
			},
		},
	}

	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

	require.NoError(t, err, "运维自己配置的凭证名不得被拒")
	require.Equal(t, "Bearer operator-configured-key", info.RuntimeHeadersOverride["x-upstream-auth"],
		"复制的必须是运维配置的值，不是调用方的令牌")
}

// TestCopyHeaderAllowsCredentialAsTarget pins the other boundary: writing a
// value INTO a credential-named header is deliberate configuration.
func TestCopyHeaderAllowsCredentialAsTarget(t *testing.T) {
	info := copyInfo("X-Client-Request-Id", "X-Api-Key",
		map[string]string{"X-Client-Request-Id": "req-123"}, nil)

	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

	require.NoError(t, err, "目标是凭证名不应被拒，来源才是判据")
	require.Equal(t, "req-123", info.RuntimeHeadersOverride["x-api-key"])
}
