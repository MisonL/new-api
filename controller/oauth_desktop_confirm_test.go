package controller

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 桌面端 login 的回调发生在系统浏览器，而会话由桌面端 poll 领取。若回调一到
// 就完成 handoff，任何拿到回调链接的第三方页面都能让受害者浏览器把受害者
// 账号交给发起 poll 的攻击者。因此回调只登记"待确认"，只有完成授权的同一台
// 浏览器、输入桌面端显示的确认码之后，poll 才能领取会话。

func newDesktopConfirmTestRouter() *gin.Engine {
	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-test-secret"))))
	router.GET("/api/oauth/desktop/start", StartDesktopOAuth)
	router.GET("/api/oauth/desktop/poll", PollDesktopOAuth)
	router.GET("/api/oauth/desktop/confirm", DesktopOAuthConfirmPage)
	router.POST("/api/oauth/desktop/confirm", DesktopOAuthConfirmSubmit)
	router.GET("/api/oauth/:provider", HandleOAuth)
	return router
}

func registerDesktopConfirmProvider(t *testing.T, slug string) {
	t.Helper()
	oauth.Register(slug, &desktopOAuthTestProvider{
		name: "Desktop Confirm Provider",
		user: &oauth.OAuthUser{
			ProviderUserID: slug + "-provider-user",
			Username:       slug + "-user",
			DisplayName:    slug + "-user",
			Email:          slug + "@example.local",
		},
	})
	t.Cleanup(func() { oauth.Unregister(slug) })
}

// startDesktopFlow 模拟桌面应用：无会话发起登录，拿到 state、handoff_token
// 与要显示给用户的确认码。
func startDesktopFlow(t *testing.T, router *gin.Engine, providerSlug string) (state, handoffToken, confirmCode string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/oauth/desktop/start?provider="+providerSlug, nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			State        string `json:"state"`
			HandoffToken string `json:"handoff_token"`
			ConfirmCode  string `json:"confirm_code"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, recorder.Body.String())
	return payload.Data.State, payload.Data.HandoffToken, payload.Data.ConfirmCode
}

func desktopConfirmBrowserCookies(recorder *httptest.ResponseRecorder) []*http.Cookie {
	cookies := recorder.Result().Cookies()
	kept := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		if c.Name == desktopOAuthConfirmCookieName {
			kept = append(kept, c)
		}
	}
	return kept
}

func TestDesktopOAuthLoginRequiresBrowserConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() { resetDesktopOAuthStoreForTest(nil) })

	const providerSlug = "desktop-confirm-provider"
	registerDesktopConfirmProvider(t, providerSlug)
	router := newDesktopConfirmTestRouter()

	state, handoffToken, confirmCode := startDesktopFlow(t, router, providerSlug)
	require.NotEmpty(t, state)
	require.NotEmpty(t, handoffToken)
	require.Len(t, confirmCode, desktopOAuthConfirmCodeLength,
		"start 必须下发确认码，桌面端要把它显示给用户")

	// 1. 受害者浏览器完成 IdP 回调：只能跳到确认页，不能直接完成 handoff。
	callback := httptest.NewRequest(http.MethodGet,
		"/api/oauth/"+providerSlug+"?state="+state+"&code=desktop-confirm-code", nil)
	callback.Header.Set("Accept", "text/html,application/xhtml+xml")
	callbackRecorder := httptest.NewRecorder()
	router.ServeHTTP(callbackRecorder, callback)
	require.Equal(t, http.StatusFound, callbackRecorder.Code,
		"浏览器导航到回调时必须跳到确认页，body=%s", callbackRecorder.Body.String())
	confirmURL := callbackRecorder.Header().Get("Location")
	require.Contains(t, confirmURL, "/api/oauth/desktop/confirm?state=")
	browserCookies := desktopConfirmBrowserCookies(callbackRecorder)
	require.NotEmpty(t, browserCookies, "回调响应必须给完成授权的浏览器下发 HttpOnly 确认凭据")

	stored, found, err := getDesktopOAuthRequestByHandoff(handoffToken)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, stored.ConfirmationRequired, "回调后必须处于待确认状态")
	require.True(t, stored.CompletedAt.IsZero(), "回调不得直接完成 handoff")
	require.NotZero(t, stored.PendingUserID)

	// 2. 未确认前 poll 不得签发会话。
	pollRecorder := httptest.NewRecorder()
	router.ServeHTTP(pollRecorder, httptest.NewRequest(http.MethodGet,
		"/api/oauth/desktop/poll?handoff_token="+handoffToken, nil))
	require.Equal(t, http.StatusOK, pollRecorder.Code)
	require.NotContains(t, pollRecorder.Body.String(), providerSlug+"-user",
		"未确认的 handoff 不能把用户身份交给轮询方，body=%s", pollRecorder.Body.String())
	for _, cookie := range pollRecorder.Result().Cookies() {
		require.False(t, strings.Contains(strings.ToLower(cookie.Name), "session"),
			"未确认不得签发会话 Cookie，got %s", cookie.Name)
	}

	// 3. 攻击者即使知道 state 与确认码，没有受害者浏览器的凭据也不能确认。
	forgeRecorder := httptest.NewRecorder()
	forgeReq := httptest.NewRequest(http.MethodPost, "/api/oauth/desktop/confirm",
		strings.NewReader("state="+state+"&code="+confirmCode))
	forgeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(forgeRecorder, forgeReq)
	require.Equal(t, http.StatusForbidden, forgeRecorder.Code,
		"缺少浏览器确认凭据的提交必须被拒绝")

	// 4. 完成授权的浏览器打开确认页，输入桌面端显示的确认码后 handoff 完成。
	pageReq := httptest.NewRequest(http.MethodGet, confirmURL, nil)
	for _, cookie := range browserCookies {
		pageReq.AddCookie(cookie)
	}
	pageRecorder := httptest.NewRecorder()
	router.ServeHTTP(pageRecorder, pageReq)
	require.Equal(t, http.StatusOK, pageRecorder.Code, "确认页必须可渲染")

	submitReq := httptest.NewRequest(http.MethodPost, "/api/oauth/desktop/confirm",
		strings.NewReader("state="+state+"&code="+strings.ToLower(confirmCode)))
	submitReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	submitReq.Host = pageReq.Host
	for _, cookie := range browserCookies {
		submitReq.AddCookie(cookie)
	}
	submitRecorder := httptest.NewRecorder()
	router.ServeHTTP(submitRecorder, submitReq)
	require.Equal(t, http.StatusOK, submitRecorder.Code, "浏览器内确认应成功：%s", submitRecorder.Body.String())

	// 5. 确认后，合法轮询才拿到会话。
	pollRecorder2 := httptest.NewRecorder()
	router.ServeHTTP(pollRecorder2, httptest.NewRequest(http.MethodGet,
		"/api/oauth/desktop/poll?handoff_token="+handoffToken, nil))
	require.Equal(t, http.StatusOK, pollRecorder2.Code)
	require.Contains(t, pollRecorder2.Body.String(), providerSlug+"-user",
		"确认完成后轮询应返回登录结果，body=%s", pollRecorder2.Body.String())
}

func TestDesktopOAuthConfirmRejectsCrossOriginSubmit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() { resetDesktopOAuthStoreForTest(nil) })

	const providerSlug = "desktop-cross-origin-provider"
	registerDesktopConfirmProvider(t, providerSlug)
	router := newDesktopConfirmTestRouter()

	state, handoffToken, confirmCode := startDesktopFlow(t, router, providerSlug)

	callback := httptest.NewRequest(http.MethodGet,
		"/api/oauth/"+providerSlug+"?state="+state+"&code=cross-origin-code", nil)
	callback.Header.Set("Accept", "text/html")
	callbackRecorder := httptest.NewRecorder()
	router.ServeHTTP(callbackRecorder, callback)
	require.Equal(t, http.StatusFound, callbackRecorder.Code)
	browserCookies := desktopConfirmBrowserCookies(callbackRecorder)
	require.NotEmpty(t, browserCookies)

	submitReq := httptest.NewRequest(http.MethodPost, "/api/oauth/desktop/confirm",
		strings.NewReader("state="+state+"&code="+confirmCode))
	submitReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	submitReq.Header.Set("Origin", "https://evil.example")
	for _, cookie := range browserCookies {
		submitReq.AddCookie(cookie)
	}
	submitRecorder := httptest.NewRecorder()
	router.ServeHTTP(submitRecorder, submitReq)
	require.Equal(t, http.StatusForbidden, submitRecorder.Code,
		"跨站表单即使带上了浏览器凭据也不得完成确认")

	stored, found, err := getDesktopOAuthRequestByHandoff(handoffToken)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, stored.CompletedAt.IsZero())
}

func TestDesktopOAuthCallbackUsesJSONForXHR(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() { resetDesktopOAuthStoreForTest(nil) })

	const providerSlug = "desktop-confirm-xhr-provider"
	registerDesktopConfirmProvider(t, providerSlug)
	router := newDesktopConfirmTestRouter()

	state, handoffToken, _ := startDesktopFlow(t, router, providerSlug)

	// 前端 SPA 用 XHR 调回调，拿到的是带确认链接的 JSON，而不是 302。
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/oauth/"+providerSlug+"?state="+state+"&code=desktop-confirm-xhr-code", nil)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			DesktopHandoff  bool   `json:"desktop_handoff"`
			ConfirmRequired bool   `json:"confirm_required"`
			ConfirmURL      string `json:"confirm_url"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Data.DesktopHandoff)
	require.True(t, payload.Data.ConfirmRequired)
	require.Contains(t, payload.Data.ConfirmURL, "/api/oauth/desktop/confirm?state="+state)

	pollRecorder := httptest.NewRecorder()
	router.ServeHTTP(pollRecorder, httptest.NewRequest(http.MethodGet,
		"/api/oauth/desktop/poll?handoff_token="+handoffToken, nil))
	require.NotContains(t, pollRecorder.Body.String(), providerSlug+"-user",
		"未确认不得签发会话，body=%s", pollRecorder.Body.String())
}

// 确认凭据 Cookie 的 Secure 判定必须与同页的来源校验同源：TLS 在反向代理
// 终止时 Go 只看到明文请求，但 X-Forwarded-Proto 已说明客户端用的是 https。
// 只看 c.Request.TLS 会让凭据缺 Secure，浏览器随后可能把它明文回传。
func TestDesktopOAuthConfirmCookieSecureFollowsForwardedProto(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name       string
		tls        bool
		proto      string
		wantSecure bool
	}{
		{name: "plain http stays insecure"},
		{name: "direct tls is secure", tls: true, wantSecure: true},
		{name: "forwarded https is secure", proto: "https", wantSecure: true},
		{name: "forwarded https chain is secure", proto: "https, http", wantSecure: true},
		{name: "forwarded http stays insecure", proto: "http"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			req := httptest.NewRequest(http.MethodGet, "/api/oauth/desktop/confirm", nil)
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if tc.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			c.Request = req

			setDesktopOAuthConfirmBrowserCookie(c, "probe-confirm-token")

			cookies := recorder.Result().Cookies()
			require.Len(t, cookies, 1)
			require.Equal(t, desktopOAuthConfirmCookieName, cookies[0].Name)
			require.Equal(t, tc.wantSecure, cookies[0].Secure,
				"Secure 判定必须与 requestIsHTTPS 一致")
		})
	}
}
