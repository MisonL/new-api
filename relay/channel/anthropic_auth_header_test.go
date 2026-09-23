package channel

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The wire header names are part of the contract with every upstream, so they
// are pinned as literals here. Asserting through the package constants instead
// would make this test self-referential: changing a constant's value to
// something the upstream does not read would still pass.
func TestAnthropicAuthHeaderNamesMatchTheWireContract(t *testing.T) {
	require.Equal(t, "Authorization", AnthropicAuthorizationHeaderName)
	require.Equal(t, "x-api-key", AnthropicApiKeyHeaderName)
	require.Equal(t, "anthropic-version", AnthropicVersionHeaderName)
	require.Equal(t, "2023-06-01", DefaultAnthropicVersion)
}

// Both Anthropic credential forms must be emitted. The Messages API documents
// x-api-key as a fallback for Authorization: Bearer, and third-party
// Anthropic-compatible gateways often implement only the bearer form.
func TestSetAnthropicAuthHeadersSendsBothForms(t *testing.T) {
	header := http.Header{}
	SetAnthropicAuthHeaders(header, "sk-ant-test-key")

	// Literal names: this is what an upstream actually receives.
	require.Equal(t, "sk-ant-test-key", header.Get("x-api-key"))
	require.Equal(t, "Bearer sk-ant-test-key", header.Get("Authorization"))
}

// Both headers must always describe the same credential. A request whose
// bearer token disagrees with x-api-key would authenticate as the wrong
// principal against gateways that read Authorization first.
func TestSetAnthropicAuthHeadersFormsAgree(t *testing.T) {
	header := http.Header{}
	SetAnthropicAuthHeaders(header, "channel-key-123")

	bearer, ok := header[AnthropicAuthorizationHeaderName]
	require.True(t, ok, "Authorization must be present")
	require.Len(t, bearer, 1, "Authorization must carry exactly one credential")
	require.Equal(t, "Bearer "+header.Get(AnthropicApiKeyHeaderName), bearer[0])
}

// A credential planted on the header set by an earlier step must be replaced,
// never appended to. Exactly one value may survive, and it is the token.
func TestSetAnthropicAuthHeadersReplacesExistingCredential(t *testing.T) {
	header := http.Header{}
	header.Set(AnthropicAuthorizationHeaderName, "Bearer attacker-supplied-token")
	header.Set(AnthropicApiKeyHeaderName, "attacker-supplied-token")

	SetAnthropicAuthHeaders(header, "channel-key")

	require.Equal(t, "Bearer channel-key", header.Get(AnthropicAuthorizationHeaderName))
	require.Len(t, header[AnthropicAuthorizationHeaderName], 1, "must be replaced, not appended")
	require.Len(t, header[http.CanonicalHeaderKey(AnthropicApiKeyHeaderName)], 1, "must be replaced, not appended")
	require.Equal(t, "channel-key", header.Get(AnthropicApiKeyHeaderName))
}

// An empty token must still produce syntactically valid headers rather than a
// malformed value. net/http renders "Bearer " as "Bearer" on the wire, which
// an upstream rejects with an ordinary auth error.
func TestSetAnthropicAuthHeadersHandlesEmptyToken(t *testing.T) {
	header := http.Header{}
	SetAnthropicAuthHeaders(header, "")

	require.Equal(t, "Bearer ", header.Get(AnthropicAuthorizationHeaderName))
	require.Contains(t, header, http.CanonicalHeaderKey(AnthropicApiKeyHeaderName))
	require.Contains(t, header, http.CanonicalHeaderKey(AnthropicAuthorizationHeaderName))
}

// The constructor form must carry the same credential pair plus the default
// Messages API version.
func TestAnthropicAuthHeadersIncludesDefaultVersion(t *testing.T) {
	header := AnthropicAuthHeaders("k")

	require.Equal(t, "k", header.Get(AnthropicApiKeyHeaderName))
	require.Equal(t, "Bearer k", header.Get(AnthropicAuthorizationHeaderName))
	require.Equal(t, DefaultAnthropicVersion, header.Get(AnthropicVersionHeaderName))
}
