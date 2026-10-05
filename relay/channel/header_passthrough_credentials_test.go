package channel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A "*" (or regex) passthrough rule copies every incoming header name to the
// upstream. Credentials must never ride along: the client's own key would be
// handed to an unrelated upstream, and for Azure-style channels `api-key` is
// exactly the credential header. x-api-key, x-goog-api-key, authorization and
// proxy-authorization were already blocked; `api-key` was not.

func passthroughHeadersFor(t *testing.T, incoming map[string]string) map[string]string {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	for name, value := range incoming {
		ctx.Request.Header.Set(name, value)
	}

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	return headers
}

// TestProcessHeaderOverride_WildcardPassthroughSkipsCredentialHeaders is the
// regression: no credential header may be forwarded by a wildcard rule.
func TestProcessHeaderOverride_WildcardPassthroughSkipsCredentialHeaders(t *testing.T) {
	t.Parallel()

	credentials := []string{
		"Authorization",
		"Proxy-Authorization",
		"Api-Key",
		"X-Api-Key",
		"X-Goog-Api-Key",
		"Cookie",
	}

	incoming := make(map[string]string, len(credentials))
	for _, name := range credentials {
		incoming[name] = "client-secret-value"
	}
	incoming["X-Trace-Id"] = "trace-123"

	headers := passthroughHeadersFor(t, incoming)

	for _, name := range credentials {
		_, present := headers[strings.ToLower(name)]
		require.Falsef(t, present, "凭证头 %s 不得被通配透传转发到上游", name)
	}
}

// TestProcessHeaderOverride_WildcardPassthroughKeepsNormalHeaders is the reverse
// check: the fix must not turn the wildcard rule into a no-op.
func TestProcessHeaderOverride_WildcardPassthroughKeepsNormalHeaders(t *testing.T) {
	t.Parallel()

	headers := passthroughHeadersFor(t, map[string]string{
		"X-Trace-Id": "trace-123",
		"X-Client-Hint": "hint",
	})

	require.Equal(t, "trace-123", headers["x-trace-id"])
	require.Equal(t, "hint", headers["x-client-hint"])
}
