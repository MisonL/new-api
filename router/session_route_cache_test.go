package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 登录/注册/2FA 与访问令牌接口会在成功响应里写会话 Cookie，或在响应体里
// 下发明文令牌。缺 Cache-Control 时，共享代理与浏览器都可能把这份响应缓存
// 下来，之后向其他用户复放。DisableCache 必须在校验链路最前，这样被拒绝
// 的响应同样带 no-store，不会留下任何可缓存的中间态。
func TestSessionIssuingRoutesDisableCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.POST("/api/user/login",
		middleware.DisableCache(),
		func(c *gin.Context) { c.JSON(http.StatusUnauthorized, gin.H{"success": false}) },
	)
	engine.GET("/api/user/token",
		middleware.DisableCache(),
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": "access-token"}) },
	)

	rejected := httptest.NewRecorder()
	engine.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/api/user/login", nil))
	require.Equal(t, http.StatusUnauthorized, rejected.Code)
	require.Contains(t, rejected.Header().Get("Cache-Control"), "no-store",
		"被拒绝的登录响应同样不得被缓存")
	require.Equal(t, "no-cache", rejected.Header().Get("Pragma"))

	issued := httptest.NewRecorder()
	engine.ServeHTTP(issued, httptest.NewRequest(http.MethodGet, "/api/user/token", nil))
	require.Equal(t, http.StatusOK, issued.Code)
	require.Contains(t, issued.Header().Get("Cache-Control"), "no-store",
		"访问令牌响应不得被缓存")
}
