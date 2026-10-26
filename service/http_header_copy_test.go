package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 上游响应头镜像必须走同一道过滤。video_proxy 与 minimax TTS 曾经整表
// 透传，Set-Cookie 会落到网关自己的 origin 上（cookie tossing），
// hop-by-hop 头（Connection/Transfer-Encoding）也会被原样转发给客户端。
func TestCopyUpstreamHeadersSkipsDenylisted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	upstream := map[string][]string{
		"Content-Type":      {"application/json"},
		"Set-Cookie":        {"session=attacker"},
		"Connection":        {"keep-alive"},
		"Transfer-Encoding": {"chunked"},
		"X-Custom":          {"ok"},
	}

	CopyUpstreamHeaders(c, upstream)

	require.Equal(t, "application/json", c.Writer.Header().Get("Content-Type"))
	require.Equal(t, "ok", c.Writer.Header().Get("X-Custom"))
	require.Empty(t, c.Writer.Header().Values("Set-Cookie"),
		"Set-Cookie from upstream must never reach the gateway origin")
	require.Empty(t, c.Writer.Header().Values("Connection"))
	require.Empty(t, c.Writer.Header().Values("Transfer-Encoding"))
}
