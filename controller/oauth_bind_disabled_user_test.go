package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 绑定回调是公开路由（不经 UserAuth 中间件），会话 cookie 可能是账号被禁用
// 之前签发的。绑定路径只读 session id 并 FillUserById，从不复读 status：
// 被禁用的账号仍可用旧会话完成第三方绑定，之后再靠该 IdP 登录（登录路径会
// 再读状态拒绝）之外的路径恢复访问，也破坏了"禁用即冻结身份"的语义。

func TestUnifiedOAuthBindRejectsDisabledUserSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)

	const providerSlug = "disabled-bind-provider"
	oauth.Register(providerSlug, &desktopOAuthTestProvider{
		name: "Disabled Bind Provider",
		user: &oauth.OAuthUser{
			ProviderUserID: "disabled-bind-provider-user",
			Username:       "disabled-bind-provider-user",
			Email:          "disabled-bind-provider-user@example.local",
		},
	})
	t.Cleanup(func() { oauth.Unregister(providerSlug) })

	user := &model.User{
		Username:    "disabled-bind-local",
		Password:    "12345678",
		DisplayName: "Disabled Bind Local",
		Email:       "disabled-bind-local@example.local",
		Role:        1,
		Status:      common.UserStatusDisabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	router := gin.New()
	router.Use(sessions.Sessions("disabled-bind-test", cookie.NewStore([]byte("disabled-bind-test-secret"))))
	router.GET("/test/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("oauth_state", "disabled-bind-state")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/api/oauth/:provider", HandleOAuth)

	seed := httptest.NewRecorder()
	router.ServeHTTP(seed, httptest.NewRequest(http.MethodGet, "/test/seed", nil))
	cookies := seed.Result().Cookies()
	require.NotEmpty(t, cookies)

	req := httptest.NewRequest(http.MethodGet,
		"/api/oauth/"+providerSlug+"?state=disabled-bind-state&code=disabled-bind-code", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.NotContains(t, recorder.Body.String(), `"action":"bind"`,
		"被禁用账号的旧会话不得完成绑定：%s", recorder.Body.String())
	var body struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.False(t, body.Success, "绑定必须失败：%s", recorder.Body.String())

	reloaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	require.Empty(t, reloaded.GitHubId, "被禁用账号不得写入第三方绑定")
}

// 反向对照：启用状态的用户照常可以绑定。
func TestUnifiedOAuthBindStillWorksForEnabledUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)

	const providerSlug = "enabled-bind-provider"
	oauth.Register(providerSlug, &desktopOAuthTestProvider{
		name: "Enabled Bind Provider",
		user: &oauth.OAuthUser{
			ProviderUserID: "enabled-bind-provider-user",
			Username:       "enabled-bind-provider-user",
			Email:          "enabled-bind-provider-user@example.local",
		},
	})
	t.Cleanup(func() { oauth.Unregister(providerSlug) })

	user := &model.User{
		Username:    "enabled-bind-local",
		Password:    "12345678",
		DisplayName: "Enabled Bind Local",
		Email:       "enabled-bind-local@example.local",
		Role:        1,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	router := gin.New()
	router.Use(sessions.Sessions("enabled-bind-test", cookie.NewStore([]byte("enabled-bind-test-secret"))))
	router.GET("/test/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("oauth_state", "enabled-bind-state")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/api/oauth/:provider", HandleOAuth)

	seed := httptest.NewRecorder()
	router.ServeHTTP(seed, httptest.NewRequest(http.MethodGet, "/test/seed", nil))
	cookies := seed.Result().Cookies()

	req := httptest.NewRequest(http.MethodGet,
		"/api/oauth/"+providerSlug+"?state=enabled-bind-state&code=enabled-bind-code", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"action":"bind"`)
}
