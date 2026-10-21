package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// 桌面端 OAuth 的回调发生在系统浏览器，而桌面应用的会话由 poll 在
// 桌面 webview 内签发。回调本来就不该给系统浏览器签发会话：state 只对
// 状态存储校验、不与任何浏览器会话绑定，任何拿到回调 URL 的第三方页面
// 都能让受害者浏览器登录进攻击者账号（登录 CSRF），而这把浏览器会话对
// 桌面端没有任何用途。回调只完成 handoff 并提示返回应用。

func newDesktopLoginCallbackRouter(t *testing.T) *gin.Engine {
	t.Helper()
	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-test-secret"))))
	router.GET("/api/oauth/:provider", HandleOAuth)
	return router
}

func registerDesktopLoginTestProvider(t *testing.T, slug string) {
	t.Helper()
	oauth.Register(slug, &desktopOAuthTestProvider{
		name: "Desktop Login CSRF Provider",
		user: &oauth.OAuthUser{
			ProviderUserID: slug + "-provider-user",
			Username:       slug + "-user",
			DisplayName:    slug + "-user",
			Email:          slug + "@example.local",
		},
	})
	t.Cleanup(func() { oauth.Unregister(slug) })
}

func TestDesktopOAuthLoginCallbackDoesNotIssueBrowserSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() { resetDesktopOAuthStoreForTest(nil) })

	const providerSlug = "desktop-csrf-provider"
	registerDesktopLoginTestProvider(t, providerSlug)

	request, err := createDesktopOAuthRequest(providerSlug, desktopOAuthModeLogin, 0, "")
	if err != nil {
		t.Fatalf("failed to create desktop oauth request: %v", err)
	}

	router := newDesktopLoginCallbackRouter(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/"+providerSlug+"?state="+request.State+"&code=desktop-csrf-code",
		nil,
	))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected callback to return 200, got %d", recorder.Code)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if strings.EqualFold(cookie.Name, "desktop-oauth-test") && cookie.Value != "" {
			t.Fatalf("桌面端 login 回调不得向系统浏览器签发会话 cookie，got cookie %s", cookie.Name)
		}
	}
	if !strings.Contains(recorder.Body.String(), `"desktop_handoff"`) {
		t.Fatalf("回调必须返回 desktop_handoff 标记以提示返回应用，got %s", recorder.Body.String())
	}

	// handoff 必须照常完成，桌面端才能通过 poll 拿到会话。
	stored, found, err := getDesktopOAuthRequestByHandoff(request.HandoffToken)
	if err != nil {
		t.Fatalf("failed to load desktop oauth request: %v", err)
	}
	if !found {
		t.Fatalf("expected desktop oauth request to remain available for polling")
	}
	if stored.CompletedAt.IsZero() || stored.ResultUserID == 0 {
		t.Fatalf("expected desktop handoff to complete, got %+v", stored)
	}
}

// 同一回调若来自普通 Web 登录（无桌面请求），必须照旧签发浏览器会话。
func TestWebOAuthLoginCallbackStillIssuesBrowserSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() { resetDesktopOAuthStoreForTest(nil) })

	const providerSlug = "web-session-provider"
	registerDesktopLoginTestProvider(t, providerSlug)

	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-test-secret"))))
	router.Use(func(c *gin.Context) {
		// 浏览器先取 state，与常规 Web 登录流程一致。
		session := sessions.Default(c)
		session.Set("oauth_state", "web-login-state")
		if err := session.Save(); err != nil {
			t.Fatalf("failed to persist oauth state: %v", err)
		}
		c.Next()
	})
	router.GET("/api/oauth/:provider", HandleOAuth)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/"+providerSlug+"?state=web-login-state&code=web-code",
		nil,
	))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected callback to return 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"success":true`) {
		t.Fatalf("web login callback must succeed, got %s", recorder.Body.String())
	}
	var found bool
	for _, cookie := range recorder.Result().Cookies() {
		if strings.EqualFold(cookie.Name, "desktop-oauth-test") && cookie.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("web login callback must issue a browser session cookie")
	}
	var user model.User
	if err := model.DB.Where("username = ?", "desktop_test_1").First(&user).Error; err != nil {
		t.Fatalf("expected web login to resolve a user: %v", err)
	}
	_ = common.RoleCommonUser
}
