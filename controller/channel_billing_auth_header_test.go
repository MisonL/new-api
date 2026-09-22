package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// The Anthropic Messages API accepts the credential either as the canonical
// `Authorization: Bearer` header or as the legacy `x-api-key` fallback, and
// documents `x-api-key` as consulted only when Authorization is absent.
// Third-party Anthropic-compatible gateways often implement only the bearer
// form, so the channel emits both from the same channel key. The channel
// "fetch models" button used to send only x-api-key and therefore failed with
// 401 against such gateways.
func TestGetClaudeAuthHeaderSendsBothAnthropicAuthHeaders(t *testing.T) {
	h := GetClaudeAuthHeader("sk-ant-test-key")

	require.Equal(t, "sk-ant-test-key", h.Get("x-api-key"))
	require.Equal(t, "Bearer sk-ant-test-key", h.Get("Authorization"))
	require.Equal(t, "2023-06-01", h.Get("anthropic-version"))
}

// Both headers must always describe the same credential. A request whose
// bearer token disagrees with the channel key would authenticate as the wrong
// principal against gateways that read Authorization first.
func TestGetClaudeAuthHeaderAnthropicAuthHeadersAgree(t *testing.T) {
	h := GetClaudeAuthHeader("channel-key-123")

	bearer, ok := h["Authorization"]
	require.True(t, ok, "Authorization must be present")
	require.Len(t, bearer, 1, "Authorization must carry exactly one credential")
	require.Equal(t, "Bearer "+h.Get("x-api-key"), bearer[0])
}

// The x-api-key header must not be lost while adding Authorization: the two
// Anthropic-compatible forms are alternatives, and dropping either would break
// the gateways that implement only that one.
func TestGetClaudeAuthHeaderKeepsLegacyApiKeyHeader(t *testing.T) {
	h := GetClaudeAuthHeader("k")

	require.Contains(t, h, "X-Api-Key", "the legacy x-api-key header must still be sent")
	require.Len(t, h["X-Api-Key"], 1, "x-api-key must carry exactly one credential")
}

// An empty channel key must still produce syntactically valid headers rather
// than a malformed value. net/http renders "Bearer " as "Bearer" on the wire,
// which the upstream rejects with an ordinary auth error; the important part
// is that no header becomes malformed.
func TestGetClaudeAuthHeaderHandlesEmptyChannelKey(t *testing.T) {
	h := GetClaudeAuthHeader("")

	require.Equal(t, "Bearer ", h.Get("Authorization"))
	require.Contains(t, h, "Authorization")
	require.Contains(t, h, "X-Api-Key")
	require.Equal(t, "2023-06-01", h.Get("anthropic-version"))
}

// End-to-end proof that the channel "fetch models" path puts both Anthropic
// auth headers on the wire. A channel pointing at a local echo server is
// exercised through the real buildFetchModelsHeaders -> GetResponseBody chain,
// and the headers the server actually received are asserted. The unit tests
// above cover the header builder; this one covers the wiring that carries its
// result to the upstream request.
func TestFetchModelsSendsBothAnthropicAuthHeadersOnTheWire(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-3-5-sonnet"}]}`))
	}))
	defer srv.Close()

	base := srv.URL
	ch := &model.Channel{Id: 1, Type: constant.ChannelTypeAnthropic, Key: "wire-probe-key", BaseURL: &base}

	headers, err := buildFetchModelsHeaders(ch, "wire-probe-key")
	require.NoError(t, err)

	_, err = GetResponseBody(http.MethodGet, srv.URL+"/v1/models", ch, headers)
	require.NoError(t, err)

	require.NotNil(t, got, "echo server must have received the request")
	require.Equal(t, "wire-probe-key", got.Get("x-api-key"))
	require.Equal(t, "Bearer wire-probe-key", got.Get("Authorization"))
	require.Equal(t, "2023-06-01", got.Get("anthropic-version"))
}
