package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 会话吊销依赖签发时记录用户当前的会话世代；漏写这个字段的话，改密码后的
// 新会话世代是 0，与递增后的数据库值不一致，登录会立刻失效。这里守住签发
// 收口写入的世代与用户当前值一致。
func TestSetupLoginRecordsSessionEpoch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)

	user := &model.User{
		Username:    "session-epoch-login-user",
		Password:    "12345678",
		DisplayName: "Session Epoch Login User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).
		UpdateColumn("session_epoch", int64(3)).Error)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	require.Equal(t, int64(3), user.SessionEpoch)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("session-epoch-login-secret"))))
	router.GET("/test/login", func(c *gin.Context) {
		issued, reason := setupLoginWithResult(user, c)
		c.JSON(http.StatusOK, gin.H{"issued": issued, "reason": reason})
	})
	router.GET("/test/epoch", func(c *gin.Context) {
		session := sessions.Default(c)
		raw := session.Get(constant.SessionKeyIDEpoch)
		epoch, _ := raw.(int64)
		c.JSON(http.StatusOK, gin.H{"epoch": epoch, "hasEpoch": raw != nil})
	})

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/test/login", nil))
	require.Contains(t, login.Body.String(), `"issued":true`, login.Body.String())
	cookies := login.Result().Cookies()
	require.NotEmpty(t, cookies)

	probe := httptest.NewRequest(http.MethodGet, "/test/epoch", nil)
	for _, sessionCookie := range cookies {
		probe.AddCookie(sessionCookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, probe)
	require.Contains(t, recorder.Body.String(), `"hasEpoch":true`, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"epoch":3`, recorder.Body.String())
}
