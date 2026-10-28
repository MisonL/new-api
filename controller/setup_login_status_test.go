package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// setupLoginWithResult 是会话签发的唯一收口。禁用账号不得拿到会话：Telegram
// 登录与 2FA 完成登录此前不检查状态，签发的是 status=disabled 的会话，违反
// "禁用账号不签发会话"的不变量，任何只校验"会话存在"的入口都会继承缺口。
func TestSetupLoginRefusesDisabledUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)

	user := &model.User{
		Username:    "disabled-session-user",
		Password:    "12345678",
		DisplayName: "Disabled Session User",
		Role:        1,
		Status:      common.UserStatusDisabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("setup-login-secret"))))
	router.GET("/test/login", func(c *gin.Context) {
		issued, reason := setupLoginWithResult(user, c)
		c.JSON(http.StatusOK, gin.H{"issued": issued, "reason": reason})
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test/login", nil))

	require.NotContains(t, recorder.Body.String(), `"issued":true`,
		"a disabled account must not be issued a session: %s", recorder.Body.String())
	for _, sessionCookie := range recorder.Result().Cookies() {
		require.NotEqual(t, "session", sessionCookie.Name,
			"a disabled account must not receive a session cookie")
	}
}
