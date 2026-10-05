package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A relayed response mirrors nearly every upstream header onto new-api's own
// origin. Set-Cookie is the dangerous one: the upstream's cookie then lands on
// the new-api domain, so an upstream can set a session cookie the operator's
// site never created -- cookie tossing, and a foothold for session fixation.
// The RFC 7230 hop-by-hop set has the same problem class: forwarding them
// describes a connection that does not exist between client and gateway.

func TestShouldCopyUpstreamHeaderRefusesCookieAndHopByHop(t *testing.T) {
	refused := []string{
		"Set-Cookie",
		"set-cookie",
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	}

	for _, name := range refused {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			require.Falsef(t, ShouldCopyUpstreamHeader(ctx, name, []string{"anything"}),
				"上游响应头 %s 不得镜像到 new-api 源", name)
		})
	}
}

// TestIOCopyBytesGracefullyDoesNotMirrorSetCookie is the end-to-end regression:
// the cookie must not reach the client's response.
func TestIOCopyBytesGracefullyDoesNotMirrorSetCookie(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Set-Cookie":   []string{"session=upstream-value; Path=/"},
			"Content-Type": []string{"application/json"},
		},
	}

	IOCopyBytesGracefully(ctx, resp, []byte(`{"ok":true}`))

	require.Empty(t, recorder.Header().Get("Set-Cookie"),
		"上游 Set-Cookie 不得落到 new-api 源")
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"),
		"普通响应头仍须透传，修复不得变成全量丢弃")
}

// TestIOCopyBytesGracefullyKeepsOrdinaryHeaders is the reverse check: the
// denylist must not strip headers the relay is expected to pass through.
func TestIOCopyBytesGracefullyKeepsOrdinaryHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                []string{"application/json"},
			"X-RateLimit-Limit":           []string{"1000"},
			"X-Organization":              []string{"acme"},
			"Access-Control-Allow-Origin": []string{"*"},
		},
	}

	IOCopyBytesGracefully(ctx, resp, []byte(`{"ok":true}`))

	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "1000", recorder.Header().Get("X-RateLimit-Limit"))
	require.Equal(t, "acme", recorder.Header().Get("X-Organization"))
	require.Equal(t, "*", recorder.Header().Get("Access-Control-Allow-Origin"))
}
