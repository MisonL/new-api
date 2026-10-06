package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// copy_header and move_header were guarded against reading a client credential
// (see header_credentials_copy_test.go), but sync_fields reads the same client
// request headers through the same getHeaderValueFromContext helper and carried
// no such guard. `sync_fields {from: header:Authorization, to: header:X-Upstream-Auth}`
// therefore still handed the caller's own new-api token to a third-party
// upstream.
//
// The judgement is the same as copy_header's: refuse reading a credential that
// came from the client, and keep allowing the case where the operator has
// themselves written a value under that name.

func syncInfo(from string, to string, requestHeaders map[string]string, override map[string]any) *RelayInfo {
	ops := []any{map[string]any{"mode": "sync_fields", "from": from, "to": to}}
	return &RelayInfo{
		RequestHeaders: requestHeaders,
		ChannelMeta: &ChannelMeta{
			ParamOverride:   map[string]any{"operations": ops},
			HeadersOverride: override,
		},
	}
}

// TestSyncFieldsRefusesCredentialSource is the regression: a credential must
// not be readable as a sync_fields source when it came from the client.
func TestSyncFieldsRefusesCredentialSource(t *testing.T) {
	for _, from := range []string{"Authorization", "api-key", "X-Api-Key", "X-Goog-Api-Key", "Cookie"} {
		t.Run(from, func(t *testing.T) {
			info := syncInfo("header:"+from, "header:X-Upstream-Auth",
				map[string]string{from: "Bearer client-new-api-token"}, nil)

			_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

			require.Error(t, err, "sync_fields 不得以客户端凭证头 %s 为来源", from)
			require.NotEqual(t, "Bearer client-new-api-token", info.RuntimeHeadersOverride["x-upstream-auth"],
				"客户端凭证不得进入上游请求头")
		})
	}
}

// TestSyncFieldsRefusesCredentialIntoJsonTarget covers the other direction of
// the same read: the credential is not made safe by writing it into the body
// instead of a header.
func TestSyncFieldsRefusesCredentialIntoJsonTarget(t *testing.T) {
	info := syncInfo("header:Authorization", "json:metadata.auth",
		map[string]string{"Authorization": "Bearer client-new-api-token"}, nil)

	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

	require.Error(t, err, "凭证不得经由 sync_fields 写进请求体")
}

// TestSyncFieldsKeepsNormalSource is the reverse check: an ordinary header must
// still sync, so the refusal does not widen into "sync_fields is broken".
func TestSyncFieldsKeepsNormalSource(t *testing.T) {
	info := syncInfo("header:X-Client-Request-Id", "header:Session_id",
		map[string]string{"X-Client-Request-Id": "req-123"}, nil)

	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

	require.NoError(t, err)
	require.Equal(t, "req-123", info.RuntimeHeadersOverride["session_id"])
}

// TestSyncFieldsAllowsOperatorConfiguredCredentialName pins the boundary. When
// the operator has themselves written a value under the credential name, the
// sync reads that value -- not the caller's token -- so it must stay allowed.
func TestSyncFieldsAllowsOperatorConfiguredCredentialName(t *testing.T) {
	info := syncInfo("header:Authorization", "header:X-Upstream-Auth",
		map[string]string{"Authorization": "Bearer client-new-api-token"},
		map[string]any{"Authorization": "Bearer operator-configured-key"})

	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)

	require.NoError(t, err, "运维自己配置的凭证名不得被拒")
	require.Equal(t, "Bearer operator-configured-key", info.RuntimeHeadersOverride["x-upstream-auth"],
		"同步的必须是运维配置的值，不是调用方的令牌")
}
