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

// 绑定回调与 Passkey 操作不走 UserAuth 中间件，直接读会话里的 id 再查库。
// 如果只比对账号状态、不比对会话世代，改密码后被吊销的会话仍能继续绑定
// 第三方账号或改 Passkey，等于换一条路把账号接管。这里要求这两条路径与
// authHelper 使用同一套世代判定。
func TestSessionDerivedUserHelpersRejectRevokedSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)

	user := &model.User{
		Username:    "revoked-bind-user",
		Password:    "12345678",
		DisplayName: "Revoked Bind User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("revoked-bind-secret"))))
	router.GET("/test/bind", func(c *gin.Context) {
		bound, err := resolveEnabledBindUser(c)
		c.JSON(http.StatusOK, gin.H{"ok": err == nil, "id": boundId(bound)})
	})
	router.GET("/test/session-user", func(c *gin.Context) {
		current, err := getSessionUser(c)
		c.JSON(http.StatusOK, gin.H{"ok": err == nil, "id": boundId(current)})
	})
	router.GET("/test/login", func(c *gin.Context) {
		var dbUser model.User
		require.NoError(t, model.DB.First(&dbUser, user.Id).Error)
		session := sessions.Default(c)
		session.Set("id", dbUser.Id)
		session.Set("username", dbUser.Username)
		session.Set("role", dbUser.Role)
		session.Set("status", dbUser.Status)
		session.Set("group", dbUser.Group)
		session.Set(constant.SessionKeyIDEpoch, dbUser.SessionEpoch)
		require.NoError(t, session.Save())
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/test/login", nil))
	cookies := login.Result().Cookies()
	require.NotEmpty(t, cookies)

	fetch := func(path string) string {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, sessionCookie := range cookies {
			req.AddCookie(sessionCookie)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder.Body.String()
	}

	require.Contains(t, fetch("/test/bind"), `"ok":true`, "未吊销的会话必须可用")
	require.Contains(t, fetch("/test/session-user"), `"ok":true`, "未吊销的会话必须可用")

	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).
		UpdateColumn("session_epoch", user.SessionEpoch+1).Error)

	require.Contains(t, fetch("/test/bind"), `"ok":false`,
		"改密码后被吊销的会话不得继续绑定第三方账号")
	require.Contains(t, fetch("/test/session-user"), `"ok":false`,
		"改密码后被吊销的会话不得继续操作 Passkey 等自助功能")
}

func boundId(user *model.User) int {
	if user == nil {
		return 0
	}
	return user.Id
}
