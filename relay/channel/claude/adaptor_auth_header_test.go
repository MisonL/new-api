package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newClaudeAuthHeaderContext(t *testing.T, apiKey string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: 14, ChannelId: 188}
	info.ApiKey = apiKey
	return c, info
}

// The Anthropic Messages API accepts the credential either as the legacy
// `x-api-key` header or as the canonical `Authorization: Bearer` form, and
// documents `x-api-key` as a fallback that is only consulted when
// Authorization is absent. Third-party Anthropic-compatible gateways often
// implement only the bearer form, so the channel emits both from the same
// channel key. Both headers always carry the identical credential, so the
// authenticated principal cannot depend on which header an upstream reads.
func TestSetupRequestHeaderSendsBothAnthropicAuthHeaders(t *testing.T) {
	c, info := newClaudeAuthHeaderContext(t, "sk-ant-test-key")

	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))

	require.Equal(t, "sk-ant-test-key", header.Get("x-api-key"))
	require.Equal(t, "Bearer sk-ant-test-key", header.Get("Authorization"))
	require.Equal(t, "2023-06-01", header.Get("anthropic-version"))
}

// Both headers must always describe the same credential. A request whose
// bearer token disagrees with the channel key would authenticate as the
// wrong principal against gateways that read Authorization first.
func TestSetupRequestHeaderAnthropicAuthHeadersAgree(t *testing.T) {
	c, info := newClaudeAuthHeaderContext(t, "channel-key-123")

	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))

	bearer, ok := header["Authorization"]
	require.True(t, ok, "Authorization must be present")
	require.Len(t, bearer, 1, "Authorization must carry exactly one credential")
	require.Equal(t, "Bearer "+header.Get("x-api-key"), bearer[0])
}

// A caller-supplied anthropic-version must keep winning over the default.
func TestSetupRequestHeaderKeepsExplicitAnthropicVersion(t *testing.T) {
	c, info := newClaudeAuthHeaderContext(t, "k")
	c.Request.Header.Set("anthropic-version", "2024-10-22")

	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))

	require.Equal(t, "2024-10-22", header.Get("anthropic-version"))
	require.Equal(t, "Bearer k", header.Get("Authorization"))
}

// A pre-existing Authorization on the outbound header must be replaced, never
// appended to or skipped. SetupRequestHeader assigns the channel credential
// with Set, so a credential planted by any earlier step cannot ride along to
// the upstream: exactly one Authorization value survives, and it is the
// channel key. (The inbound request header is not a source here - the value is
// derived from info.ApiKey - so the credential a client sends cannot reach the
// upstream through this path at all.)
func TestSetupRequestHeaderOverridesCallerSuppliedAuthorization(t *testing.T) {
	c, info := newClaudeAuthHeaderContext(t, "channel-key")
	c.Request.Header.Set("Authorization", "Bearer attacker-supplied-token")

	header := http.Header{}
	header.Set("Authorization", "Bearer attacker-supplied-token")
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))

	require.Equal(t, "Bearer channel-key", header.Get("Authorization"))
	require.Len(t, header["Authorization"], 1, "the planted credential must be replaced, not appended")
	require.Equal(t, "channel-key", header.Get("x-api-key"))
}

// An empty channel key must still produce a syntactically valid header rather
// than a bare "Bearer" with no value or a missing header. net/http renders
// "Bearer " as "Bearer" on the wire, which the upstream rejects with a normal
// auth error; the important part is that neither header becomes malformed and
// no request-construction error is introduced.
func TestSetupRequestHeaderHandlesEmptyChannelKey(t *testing.T) {
	c, info := newClaudeAuthHeaderContext(t, "")

	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))

	require.Equal(t, "Bearer ", header.Get("Authorization"))
	require.Contains(t, header, "Authorization", "Authorization must still be present")
	require.Contains(t, header, "X-Api-Key", "x-api-key must still be present")
}
