package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Turnstile 通过标记以前是会话里的布尔值，而会话 cookie 有效 30 天：
// 用户解一次谜，登录、注册、验证码、签到端点在这一个月内都不再做人工校验。
// 标记必须带时效，且旧格式（bool true）按过期处理。
func TestTurnstileCheckRejectsStaleSessionPass(t *testing.T) {
	previousEnabled := common.TurnstileCheckEnabled
	previousSecret := common.TurnstileSecretKey
	common.TurnstileCheckEnabled = true
	common.TurnstileSecretKey = "test-secret"
	t.Cleanup(func() {
		common.TurnstileCheckEnabled = previousEnabled
		common.TurnstileSecretKey = previousSecret
	})

	gin.SetMode(gin.TestMode)
	store := cookie.NewStore([]byte("turnstile-test-session-key"))
	router := gin.New()
	router.Use(sessions.Sessions("session", store))
	seed := func(path string, value any) {
		router.GET(path, func(c *gin.Context) {
			session := sessions.Default(c)
			session.Set("turnstile", value)
			require.NoError(t, session.Save())
			c.Status(http.StatusNoContent)
		})
	}
	seed("/seed-stale", time.Now().Add(-2*turnstilePassTTL).Unix())
	seed("/seed-fresh", time.Now().Unix())
	seed("/seed-legacy-bool", true)
	router.Use(TurnstileCheck())
	handlerRan := false
	router.GET("/protected", func(c *gin.Context) {
		handlerRan = true
		c.String(http.StatusOK, "handler")
	})

	serve := func(path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	for _, tc := range []struct {
		seedPath string
		wantPass bool
	}{
		{seedPath: "/seed-stale", wantPass: false},
		{seedPath: "/seed-legacy-bool", wantPass: false},
		{seedPath: "/seed-fresh", wantPass: true},
	} {
		t.Run(tc.seedPath, func(t *testing.T) {
			seedResponse := serve(tc.seedPath, nil)
			require.Equal(t, http.StatusNoContent, seedResponse.Code)
			cookies := seedResponse.Result().Cookies()
			require.NotEmpty(t, cookies)

			handlerRan = false
			response := serve("/protected", cookies)
			require.Equal(t, tc.wantPass, handlerRan, "handler execution: %s", response.Body.String())
			if !tc.wantPass {
				require.Contains(t, response.Body.String(), "Turnstile token 为空")
			}
		})
	}
}
