package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 统一 OAuth 路由的绑定路径必须和 github/discord/oidc/linuxdo 各自的绑定
// 路径一样消费一次性 state。否则 state 随 30 天会话长期有效：观察到一次
// 回调 URL 的人可以重放它，把自己的 IdP 身份绑到受害者账户上。
func TestUnifiedOAuthBindConsumesState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)

	const providerSlug = "state-consume-bind-provider"
	oauth.Register(providerSlug, &desktopOAuthTestProvider{
		name: "State Consume Bind Provider",
		user: &oauth.OAuthUser{
			ProviderUserID: "state-consume-bind-user",
			Username:       "state-consume-bind-user",
			Email:          "state-consume-bind-user@example.local",
		},
	})
	t.Cleanup(func() { oauth.Unregister(providerSlug) })

	user := &model.User{
		Username:    "state-consume-bind-local",
		Password:    "12345678",
		DisplayName: "State Consume Bind Local",
		Email:       "state-consume-bind-local@example.local",
		Role:        1,
		Status:      1,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-test-secret"))))
	router.GET("/test/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("oauth_state", "bind-once-state")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/api/oauth/:provider", HandleOAuth)

	// 先拿到带 state 的会话 Cookie，后续请求复用它（真实浏览器的行为）。
	seed := httptest.NewRecorder()
	router.ServeHTTP(seed, httptest.NewRequest(http.MethodGet, "/test/seed", nil))
	sessionCookies := seed.Result().Cookies()
	require.NotEmpty(t, sessionCookies)

	// 模拟浏览器的 Cookie 罐：每次响应都替换会话 Cookie。
	jar := map[string]*http.Cookie{}
	for _, cookie := range sessionCookies {
		jar[cookie.Name] = cookie
	}
	call := func(code string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet,
			"/api/oauth/"+providerSlug+"?state=bind-once-state&code="+code, nil)
		for _, cookie := range jar {
			req.AddCookie(cookie)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		for _, cookie := range recorder.Result().Cookies() {
			if cookie.MaxAge < 0 {
				delete(jar, cookie.Name)
				continue
			}
			jar[cookie.Name] = cookie
		}
		return recorder
	}

	first := call("first-bind-code")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.Contains(t, first.Body.String(), `"action":"bind"`)

	// 重放同一个 state：state 已被消费，必须被拒绝。
	replay := call("second-bind-code")
	require.Equal(t, http.StatusForbidden, replay.Code,
		"重放已消费的 state 必须被拒绝，body=%s", replay.Body.String())
}
