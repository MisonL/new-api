package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

type desktopOAuthStartResponse struct {
	State        string `json:"state"`
	HandoffToken string `json:"handoff_token"`
	Mode         string `json:"mode"`
}

type desktopOAuthTestProvider struct {
	name string
	user *oauth.OAuthUser
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
	if stored.ErrorMessage != "user_cancelled" {
		t.Fatalf("expected desktop oauth error to be captured, got %q", stored.ErrorMessage)
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
	if stored.CompletedAt.IsZero() {
		t.Fatalf("expected desktop oauth login request to complete even when browser session exists")
	}
	if stored.ResultUserID == 0 {
		t.Fatalf("expected desktop oauth login request to capture a login user")
	}
	if stored.ResultUserID == browserUser.Id {
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
	if stored.ResultUserID == 0 || stored.CompletedAt.IsZero() {
		t.Fatalf("expected redis-backed desktop oauth request to be completed before polling")
	}

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
