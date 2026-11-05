package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

type desktopOAuthStartResponse struct {
	State        string `json:"state"`
	HandoffToken string `json:"handoff_token"`
	Mode         string `json:"mode"`
	ConfirmCode  string `json:"confirm_code"`
}

type desktopOAuthTestProvider struct {
	name    string
	user    *oauth.OAuthUser
	infoErr error
}

type unavailableDesktopOAuthStore struct{}

func (s *unavailableDesktopOAuthStore) Create(request *desktopOAuthRequest) error {
	return errDesktopOAuthStoreUnavailable
}

func (s *unavailableDesktopOAuthStore) GetByState(state string) (*desktopOAuthRequest, bool, error) {
	return nil, false, errDesktopOAuthStoreUnavailable
}

func (s *unavailableDesktopOAuthStore) GetByHandoff(handoffToken string) (*desktopOAuthRequest, bool, error) {
	return nil, false, errDesktopOAuthStoreUnavailable
}

func (s *unavailableDesktopOAuthStore) Complete(state string, resultUserID int) error {
	return errDesktopOAuthStoreUnavailable
}

func (s *unavailableDesktopOAuthStore) Fail(state string, message string) error {
	return errDesktopOAuthStoreUnavailable
}

func (s *unavailableDesktopOAuthStore) Consume(handoffToken string) (*desktopOAuthRequest, bool, error) {
	return nil, false, errDesktopOAuthStoreUnavailable
}

func (s *unavailableDesktopOAuthStore) AwaitConfirmation(state string, pendingUserID int, pendingProviderUserID string, browserToken string) error {
	return errDesktopOAuthStoreUnavailable
}

func (s *unavailableDesktopOAuthStore) IncrementConfirmationAttempts(state string) (int, error) {
	return 0, errDesktopOAuthStoreUnavailable
}

func (p *desktopOAuthTestProvider) GetName() string {
	return p.name
}

func (p *desktopOAuthTestProvider) IsEnabled() bool {
	return true
}

func (p *desktopOAuthTestProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*oauth.OAuthToken, error) {
	return &oauth.OAuthToken{
		AccessToken: "desktop-test-token",
		TokenType:   "Bearer",
	}, nil
}

func (p *desktopOAuthTestProvider) GetUserInfo(ctx context.Context, token *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	if p.infoErr != nil {
		return nil, p.infoErr
	}
	if p.user != nil {
		return p.user, nil
	}
	return &oauth.OAuthUser{
		ProviderUserID: "desktop-test-provider-user",
		Username:       "desktop-oauth-login",
		DisplayName:    "Desktop OAuth Login",
		Email:          "desktop-oauth-login@example.local",
	}, nil
}

func (p *desktopOAuthTestProvider) IsUserIDTaken(providerUserID string) bool {
	return false
}

func (p *desktopOAuthTestProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	return nil
}

func (p *desktopOAuthTestProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.GitHubId = providerUserID
}

func (p *desktopOAuthTestProvider) GetProviderPrefix() string {
	return "desktop_test_"
}

func useRedisDesktopOAuthStoreForTest(t *testing.T) {
	t.Helper()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalRedisEnabled := common.RedisEnabled
	originalRDB := common.RDB
	resetDesktopOAuthStoreForTest(nil)
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRDB
		resetDesktopOAuthStoreForTest(nil)
	})
}

func newDesktopOAuthTestRouter() *gin.Engine {
	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-test-secret"))))
	router.GET("/api/oauth/desktop/start", StartDesktopOAuth)
	router.GET("/api/oauth/desktop/poll", PollDesktopOAuth)
	router.GET("/api/oauth/desktop/confirm", DesktopOAuthConfirmPage)
	router.POST("/api/oauth/desktop/confirm", DesktopOAuthConfirmSubmit)
	router.GET("/api/oauth/:provider", HandleOAuth)
	return router
}

func TestHandleOAuthMarksDesktopRequestFailedWhenProviderReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() {
		resetDesktopOAuthStoreForTest(nil)
	})

	const providerSlug = "desktop-error-provider"
	oauth.RegisterCustom(providerSlug, &desktopOAuthTestProvider{name: "Desktop Error Provider"})
	t.Cleanup(func() {
		oauth.Unregister(providerSlug)
	})

	request, err := createDesktopOAuthRequest(providerSlug, desktopOAuthModeLogin, 0, "")
	if err != nil {
		t.Fatalf("failed to create desktop oauth request: %v", err)
	}
	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-test-secret"))))
	router.GET("/api/oauth/:provider", HandleOAuth)

	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/"+providerSlug+"?state="+request.State+"&error=access_denied&error_description=user_cancelled",
		nil,
	)
	router.ServeHTTP(recorder, httpRequest)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected callback to return 200, got %d", recorder.Code)
	}

	stored, found, err := getDesktopOAuthRequestByHandoff(request.HandoffToken)
	if err != nil {
		t.Fatalf("failed to load desktop oauth request by handoff: %v", err)
	}
	if !found {
		t.Fatalf("expected desktop oauth request to remain available for polling")
	}
	if stored.ErrorMessage != desktopOAuthProviderErrorMessage {
		t.Fatalf("expected desktop oauth error to be stored as the fixed message, got %q", stored.ErrorMessage)
	}
	if strings.Contains(stored.ErrorMessage, "user_cancelled") {
		t.Fatalf("expected provider error text not to be reflected, got %q", stored.ErrorMessage)
	}
	if stored.CompletedAt.IsZero() {
		t.Fatalf("expected failed desktop oauth request to have completion timestamp")
	}
}

func TestHandleOAuthDesktopLoginIgnoresExistingBrowserSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() {
		resetDesktopOAuthStoreForTest(nil)
	})

	browserUser := &model.User{
		Username:    "browser-session-user",
		Password:    "12345678",
		DisplayName: "Browser Session User",
		Email:       "browser-session@example.local",
		Role:        1,
		Status:      1,
		Group:       "default",
	}
	if err := browserUser.Insert(0); err != nil {
		t.Fatalf("failed to insert browser session user: %v", err)
	}

	const providerSlug = "desktop-login-provider"
	oauth.Register(providerSlug, &desktopOAuthTestProvider{
		name: "Desktop Login Provider",
		user: &oauth.OAuthUser{
			ProviderUserID: "desktop-login-provider-user",
			Username:       "desktop-login-user",
			DisplayName:    "Desktop Login User",
			Email:          "desktop-login-user@example.local",
		},
	})
	t.Cleanup(func() {
		oauth.Unregister(providerSlug)
	})

	request, err := createDesktopOAuthRequest(providerSlug, desktopOAuthModeLogin, 0, "")
	if err != nil {
		t.Fatalf("failed to create desktop oauth request: %v", err)
	}
	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-test-secret"))))
	router.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", browserUser.Id)
		session.Set("username", browserUser.Username)
		session.Set("role", browserUser.Role)
		session.Set("status", browserUser.Status)
		session.Set("group", browserUser.Group)
		if err := session.Save(); err != nil {
			t.Fatalf("failed to persist browser session: %v", err)
		}
		c.Next()
	})
	router.GET("/api/oauth/:provider", HandleOAuth)

	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/"+providerSlug+"?state="+request.State+"&code=desktop-login-code",
		nil,
	)
	router.ServeHTTP(recorder, httpRequest)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected callback to return 200, got %d", recorder.Code)
	}

	stored, found, err := getDesktopOAuthRequestByHandoff(request.HandoffToken)
	if err != nil {
		t.Fatalf("failed to load desktop oauth request by handoff: %v", err)
	}
	if !found {
		t.Fatalf("expected desktop oauth request to remain available for polling")
	}
	if !stored.ConfirmationRequired || !stored.CompletedAt.IsZero() {
		t.Fatalf("expected desktop oauth login request to wait for browser confirmation, got %+v", stored)
	}
	if stored.PendingUserID == 0 {
		t.Fatalf("expected desktop oauth login request to capture a pending login user")
	}
	if stored.PendingUserID == browserUser.Id {
		t.Fatalf("expected desktop oauth login to ignore existing browser session user %d", browserUser.Id)
	}
}

func TestDesktopOAuthRedisLoginFlowCompletesViaPoll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	useRedisDesktopOAuthStoreForTest(t)

	const providerSlug = "desktop-redis-login-provider"
	oauth.Register(providerSlug, &desktopOAuthTestProvider{
		name: "Desktop Redis Login Provider",
		user: &oauth.OAuthUser{
			ProviderUserID: "desktop-redis-provider-user",
			Username:       "desktop-redis-user",
			DisplayName:    "Desktop Redis User",
			Email:          "desktop-redis-user@example.local",
		},
	})
	t.Cleanup(func() {
		oauth.Unregister(providerSlug)
	})

	request, err := createDesktopOAuthRequest(providerSlug, desktopOAuthModeLogin, 0, "")
	if err != nil {
		t.Fatalf("failed to create desktop oauth request: %v", err)
	}

	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-test-secret"))))
	router.GET("/api/oauth/:provider", HandleOAuth)
	router.GET("/api/oauth/desktop/poll", PollDesktopOAuth)
	router.GET("/api/oauth/desktop/confirm", DesktopOAuthConfirmPage)
	router.POST("/api/oauth/desktop/confirm", DesktopOAuthConfirmSubmit)

	callbackRecorder := httptest.NewRecorder()
	callbackRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/"+providerSlug+"?state="+request.State+"&code=desktop-redis-code",
		nil,
	)
	router.ServeHTTP(callbackRecorder, callbackRequest)
	if callbackRecorder.Code != http.StatusOK {
		t.Fatalf("expected callback to return 200, got %d", callbackRecorder.Code)
	}

	stored, found, err := getDesktopOAuthRequestByHandoff(request.HandoffToken)
	if err != nil {
		t.Fatalf("failed to load redis-backed desktop oauth request: %v", err)
	}
	if !found {
		t.Fatalf("expected redis-backed desktop oauth request to remain available for polling")
	}
	if stored.PendingUserID == 0 || !stored.ConfirmationRequired || !stored.CompletedAt.IsZero() {
		t.Fatalf("expected redis-backed desktop oauth request to wait for confirmation, got %+v", stored)
	}
	confirmDesktopOAuthForTest(t, router, callbackRecorder, request.State, request.ConfirmCode)

	pollRecorder := httptest.NewRecorder()
	pollRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/desktop/poll?handoff_token="+request.HandoffToken,
		nil,
	)
	router.ServeHTTP(pollRecorder, pollRequest)
	if pollRecorder.Code != http.StatusOK {
		t.Fatalf("expected poll to return 200, got %d", pollRecorder.Code)
	}
	if body := pollRecorder.Body.String(); body == "" {
		t.Fatalf("expected poll response body to be non-empty")
	} else if !strings.Contains(body, `"success":true`) || !strings.Contains(body, `"username":"desktop-redis-user"`) {
		t.Fatalf("expected poll response to include desktop login result, got %s", body)
	}

	if _, found, err := getDesktopOAuthRequestByHandoff(request.HandoffToken); err != nil {
		t.Fatalf("failed to verify redis-backed request cleanup: %v", err)
	} else if found {
		t.Fatalf("expected redis-backed desktop oauth request to be consumed after polling")
	}
}

func TestDesktopOAuthRedisFlowSurvivesCrossRouterLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	useRedisDesktopOAuthStoreForTest(t)

	const providerSlug = "desktop-cross-router-provider"
	oauth.Register(providerSlug, &desktopOAuthTestProvider{
		name: "Desktop Cross Router Provider",
		user: &oauth.OAuthUser{
			ProviderUserID: "desktop-cross-router-provider-user",
			Username:       "desktop-cross-router-user",
			DisplayName:    "Desktop Cross Router User",
			Email:          "desktop-cross-router-user@example.local",
		},
	})
	t.Cleanup(func() {
		oauth.Unregister(providerSlug)
	})

	routerA := newDesktopOAuthTestRouter()
	routerB := newDesktopOAuthTestRouter()

	startRecorder := httptest.NewRecorder()
	startRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/desktop/start?provider="+providerSlug+"&mode=login",
		nil,
	)
	routerA.ServeHTTP(startRecorder, startRequest)
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("expected desktop oauth start to return 200, got %d", startRecorder.Code)
	}

	var startResponse oauthJWTAPIResponse
	if err := common.Unmarshal([]byte(startRecorder.Body.String()), &startResponse); err != nil {
		t.Fatalf("failed to decode start response: %v", err)
	}
	if !startResponse.Success {
		t.Fatalf("expected desktop oauth start to succeed, got %s", startRecorder.Body.String())
	}

	var payload desktopOAuthStartResponse
	if err := common.Unmarshal(startResponse.Data, &payload); err != nil {
		t.Fatalf("failed to decode desktop oauth start payload: %v", err)
	}
	if payload.State == "" || payload.HandoffToken == "" {
		t.Fatalf("expected start payload to include state and handoff token")
	}
	if payload.Mode != desktopOAuthModeLogin {
		t.Fatalf("expected start payload mode %q, got %q", desktopOAuthModeLogin, payload.Mode)
	}

	callbackRecorder := httptest.NewRecorder()
	callbackRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/"+providerSlug+"?state="+payload.State+"&code=desktop-cross-router-code",
		nil,
	)
	routerB.ServeHTTP(callbackRecorder, callbackRequest)
	if callbackRecorder.Code != http.StatusOK {
		t.Fatalf("expected callback on second router to return 200, got %d", callbackRecorder.Code)
	}
	confirmDesktopOAuthForTest(t, routerB, callbackRecorder, payload.State, payload.ConfirmCode)

	pollRecorder := httptest.NewRecorder()
	pollRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/desktop/poll?handoff_token="+payload.HandoffToken,
		nil,
	)
	routerA.ServeHTTP(pollRecorder, pollRequest)
	if pollRecorder.Code != http.StatusOK {
		t.Fatalf("expected poll on first router to return 200, got %d", pollRecorder.Code)
	}

	var pollResponse oauthJWTAPIResponse
	if err := common.Unmarshal([]byte(pollRecorder.Body.String()), &pollResponse); err != nil {
		t.Fatalf("failed to decode poll response: %v", err)
	}
	if !pollResponse.Success {
		t.Fatalf("expected poll to succeed, got %s", pollRecorder.Body.String())
	}
	var loginResponse oauthJWTLoginResponse
	if err := common.Unmarshal(pollResponse.Data, &loginResponse); err != nil {
		t.Fatalf("failed to decode poll login payload: %v", err)
	}
	if loginResponse.ID == 0 {
		t.Fatalf("expected poll to return a logged-in user id, got %+v", loginResponse)
	}
	if loginResponse.Username == "" {
		t.Fatalf("expected poll to return a non-empty username, got %+v", loginResponse)
	}

	if _, found, err := getDesktopOAuthRequestByHandoff(payload.HandoffToken); err != nil {
		t.Fatalf("failed to verify cross-router request cleanup: %v", err)
	} else if found {
		t.Fatalf("expected cross-router desktop oauth request to be consumed after polling")
	}
}

func TestPollDesktopOAuthReturnsServiceUnavailableWhenStoreUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetDesktopOAuthStoreForTest(&unavailableDesktopOAuthStore{})
	t.Cleanup(func() {
		resetDesktopOAuthStoreForTest(nil)
	})

	router := gin.New()
	router.GET("/api/oauth/desktop/poll", PollDesktopOAuth)

	token := strings.ReplaceAll(t.Name(), "/", "-")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/desktop/poll?handoff_token="+token,
		nil,
	)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected poll to return 503 when store is unavailable, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "状态存储暂不可用") {
		t.Fatalf("expected service unavailable message, got %s", recorder.Body.String())
	}
}

// confirmDesktopOAuthForTest 用回调响应下发的浏览器凭据完成确认，模拟用户
// 在完成授权的同一台浏览器里输入桌面端确认码。
func confirmDesktopOAuthForTest(t *testing.T, router *gin.Engine, callbackRecorder *httptest.ResponseRecorder, state, code string) {
	t.Helper()
	var cookies []*http.Cookie
	for _, cookie := range callbackRecorder.Result().Cookies() {
		if cookie.Name == desktopOAuthConfirmCookieName {
			cookies = append(cookies, cookie)
		}
	}
	if len(cookies) == 0 {
		t.Fatalf("expected callback to hand the confirmation cookie to the browser")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/desktop/confirm",
		strings.NewReader("state="+state+"&code="+code))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected browser confirmation to succeed, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

// 回调的 error 分支不需要换码：任何知道 state 的人都能构造
// /api/oauth/<provider>?state=...&error=...&error_description=任意文本。
// 把这段文本写进请求记录，桌面端 poll 与确认页就会原样显示攻击者选定的
// 文案（文案反射，可用于社工诱导）。失败原因只存固定提示，原始值仅进服务端日志。
func TestDesktopOAuthProviderErrorMessageIsNotReflected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() {
		resetDesktopOAuthStoreForTest(nil)
	})

	const providerSlug = "desktop-error-reflect-provider"
	oauth.RegisterCustom(providerSlug, &desktopOAuthTestProvider{name: "Desktop Error Reflect Provider"})
	t.Cleanup(func() {
		oauth.Unregister(providerSlug)
	})

	request, err := createDesktopOAuthRequest(providerSlug, desktopOAuthModeLogin, 0, "")
	require.NoError(t, err)

	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-test-secret"))))
	router.GET("/api/oauth/:provider", HandleOAuth)
	router.GET("/api/oauth/desktop/poll", PollDesktopOAuth)

	const marker = "attacker-controlled-notice-9f2c"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/"+providerSlug+"?state="+request.State+"&error=access_denied&error_description="+marker,
		nil,
	))
	require.Equal(t, http.StatusOK, recorder.Code)

	stored, found, err := getDesktopOAuthRequestByHandoff(request.HandoffToken)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, stored.CompletedAt.IsZero(), "provider error must still fail the pending request")
	require.NotEmpty(t, stored.ErrorMessage)
	require.NotContains(t, stored.ErrorMessage, marker, "provider error text must not be stored verbatim")

	pollRecorder := httptest.NewRecorder()
	router.ServeHTTP(pollRecorder, httptest.NewRequest(http.MethodGet,
		"/api/oauth/desktop/poll?handoff_token="+request.HandoffToken, nil))
	require.NotContains(t, pollRecorder.Body.String(), marker)
}

// OAuthError.Error() 返回的是 RawError，也就是上游响应里的原始错误文本。它同样
// 会被写进桌面端请求记录，poll 与确认页原样显示，等于把上游/攻击者可控的文案
// 反射给用户。失败提示必须只取 i18n 文案或固定兜底，原始文本进服务端日志。
func TestDesktopOAuthUpstreamErrorRawTextIsNotReflected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() {
		resetDesktopOAuthStoreForTest(nil)
	})

	const marker = "upstream-controlled-text-4a1d"
	const providerSlug = "desktop-upstream-error-provider"
	oauth.RegisterCustom(providerSlug, &desktopOAuthTestProvider{
		name:    "Desktop Upstream Error Provider",
		infoErr: oauth.NewOAuthErrorWithRaw(i18n.MsgOAuthGetUserErr, nil, marker),
	})
	t.Cleanup(func() {
		oauth.Unregister(providerSlug)
	})

	request, err := createDesktopOAuthRequest(providerSlug, desktopOAuthModeLogin, 0, "")
	require.NoError(t, err)

	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-test-secret"))))
	router.GET("/api/oauth/:provider", HandleOAuth)
	router.GET("/api/oauth/desktop/poll", PollDesktopOAuth)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet,
		"/api/oauth/"+providerSlug+"?state="+request.State+"&code=desktop-upstream-error-code",
		nil,
	))

	stored, found, err := getDesktopOAuthRequestByHandoff(request.HandoffToken)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, stored.CompletedAt.IsZero(), "upstream error must fail the pending request")
	require.NotEmpty(t, stored.ErrorMessage)
	require.NotContains(t, stored.ErrorMessage, marker, "upstream error raw text must not be stored verbatim")

	pollRecorder := httptest.NewRecorder()
	router.ServeHTTP(pollRecorder, httptest.NewRequest(http.MethodGet,
		"/api/oauth/desktop/poll?handoff_token="+request.HandoffToken, nil))
	require.NotContains(t, pollRecorder.Body.String(), marker)
}
