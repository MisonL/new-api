package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 会话 Cookie 有效期 30 天且无法服务端吊销：用户改密码后，旧 Cookie 仍然
// 可以继续访问受保护接口。签发会话时记下用户的会话世代，鉴权时与数据库/
// 缓存中的当前值比对，不一致即视为已吊销。
func TestUserAuthRejectsSessionFromBeforePasswordChange(t *testing.T) {
	setupAuthMiddlewareTestDB(t)

	user := &model.User{
		Username:    "session-epoch-auth-user",
		Password:    "12345678",
		DisplayName: "Session Epoch Auth User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("session-epoch-secret"))))
	// 与 setupLoginWithResult 一致的签发内容；每次签发从数据库读取当前世代，
	// 模拟改密码后重新登录。
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
	// 升级前的旧会话没有世代字段，必须按世代 0 处理，否则上线即把所有在线
	// 用户踢下线。
	router.GET("/test/login-legacy", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("status", user.Status)
		session.Set("group", user.Group)
		require.NoError(t, session.Save())
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	router.GET("/test/me", UserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	router.GET("/test/pricing", TryUserAuth(), func(c *gin.Context) {
		id, exists := c.Get("id")
		c.JSON(http.StatusOK, gin.H{"success": true, "id": id, "idSet": exists})
	})

	login := func(path string) []*http.Cookie {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		cookies := recorder.Result().Cookies()
		require.NotEmpty(t, cookies)
		return cookies
	}
	fetchMe := func(cookies []*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/test/me", nil)
		req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
		for _, sessionCookie := range cookies {
			req.AddCookie(sessionCookie)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	cookies := login("/test/login")
	require.Contains(t, fetchMe(cookies).Body.String(), `"success":true`,
		"刚签发的会话必须可用")

	legacyCookies := login("/test/login-legacy")
	require.Contains(t, fetchMe(legacyCookies).Body.String(), `"success":true`,
		"没有世代字段的旧会话必须按世代 0 继续有效")

	// 改密码：递增会话世代。
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).
		UpdateColumn("session_epoch", user.SessionEpoch+1).Error)

	revoked := fetchMe(cookies)
	require.Equal(t, http.StatusUnauthorized, revoked.Code,
		"密码变更后旧会话必须被拒绝：%s", revoked.Body.String())

	legacyRevoked := fetchMe(legacyCookies)
	require.Equal(t, http.StatusUnauthorized, legacyRevoked.Code,
		"没有世代字段的旧会话在密码变更后同样必须被拒绝：%s", legacyRevoked.Body.String())

	fetchPricing := func(cookies []*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/test/pricing", nil)
		for _, sessionCookie := range cookies {
			req.AddCookie(sessionCookie)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	require.Contains(t, fetchPricing(cookies).Body.String(), `"idSet":false`,
		"已吊销的会话不得再暴露用户 id")

	// 重新登录签发的新会话携带新世代，必须可用。
	freshCookies := login("/test/login")
	require.Contains(t, fetchMe(freshCookies).Body.String(), `"success":true`,
		"改密码后重新登录的会话必须可用")
}
