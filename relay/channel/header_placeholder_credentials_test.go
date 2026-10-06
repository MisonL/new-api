package channel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A `{client_header:<name>}` placeholder resolves to an incoming request
// header, so it reads exactly the values the wildcard passthrough rule and the
// copy_header operation were already forbidden to read. Without the same guard,
// `{"X-Upstream-Auth": "{client_header:Authorization}"}` handed the caller's own
// new-api token to the upstream.
//
// The rest of the codebase already treats this placeholder as client-dynamic
// and refuses to freeze it into a static policy
// (service/task_route_snapshot.go errors on it,
// service/channel_header_policy_runtime.go skips it); only the live relay path
// resolved it, and it resolved any name.

func placeholderHeadersFor(t *testing.T, target string, template string, incoming map[string]string) (map[string]string, error) {
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
			HeadersOverride: map[string]any{target: template},
		},
	}

	return processHeaderOverride(info, ctx)
}

// TestProcessHeaderOverride_ClientHeaderPlaceholderRefusesCredential is the
// regression: the placeholder must not resolve a client credential.
func TestProcessHeaderOverride_ClientHeaderPlaceholderRefusesCredential(t *testing.T) {
	t.Parallel()

	credentials := []string{
		"Authorization",
		"Proxy-Authorization",
		"Api-Key",
		"X-Api-Key",
		"X-Goog-Api-Key",
		"Cookie",
	}

	for _, name := range credentials {
		t.Run(name, func(t *testing.T) {
			headers, err := placeholderHeadersFor(t, "X-Upstream-Auth",
				"{client_header:"+name+"}", map[string]string{name: "client-secret-value"})

			require.Errorf(t, err, "client_header 占位符不得读取客户端凭证头 %s", name)
			require.NotEqual(t, "client-secret-value", headers["x-upstream-auth"],
				"客户端凭证不得进入上游请求头")
		})
	}
}

// TestProcessHeaderOverride_ClientHeaderPlaceholderKeepsNormalHeader is the
// reverse check: an ordinary header must still be forwarded, so the refusal
// does not widen into "the placeholder is broken".
func TestProcessHeaderOverride_ClientHeaderPlaceholderKeepsNormalHeader(t *testing.T) {
	t.Parallel()

	headers, err := placeholderHeadersFor(t, "X-Upstream-Auth",
		"{client_header:X-Trace-Id}", map[string]string{"X-Trace-Id": "trace-123"})

	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-auth"])
}
