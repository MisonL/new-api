package controller

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp/totp"
)

// The second factor gate lives in setupLoginWithResult, the only place a login
// session is created. These tests pin both directions: a two-factor account
// must be challenged on every login path (not only the password form), and a
// user without two-factor must keep signing in with no extra step.

type twoFAGateResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Data    common.RawMessage `json:"data"`
}

type twoFAGateData struct {
	ID         int  `json:"id"`
	Require2FA bool `json:"require_2fa"`
}

type twoFAGateSessionState struct {
	ID         *int `json:"id"`
	PendingID  *int `json:"pending_user_id"`
	VerifiedID *int `json:"twofa_verified_user_id"`
}

func setupTwoFAGateTestDB(t *testing.T) {
	t.Helper()
	// The shared fixture already migrates the two-factor tables; the gate reads
	// two_fas on every login, so any fixture that reaches setupLogin needs them.
	setupCustomOAuthJWTControllerTestDB(t)
}

func enableTwoFAForTest(t *testing.T, user *model.User) string {
	t.Helper()
	key, err := common.GenerateTOTPSecret(user.Username)
	if err != nil {
		t.Fatalf("failed to generate totp secret: %v", err)
	}
	secret := key.Secret()
	record := &model.TwoFA{
		UserId:    user.Id,
		Secret:    secret,
		IsEnabled: true,
	}
	if err := model.DB.Create(record).Error; err != nil {
		t.Fatalf("failed to enable two-factor for user %d: %v", user.Id, err)
	}
	return secret
}

func currentTOTPForTest(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("failed to generate totp code: %v", err)
	}
	return code
}

func decodeTwoFAGateResponse(t *testing.T, body []byte) twoFAGateResponse {
	t.Helper()
	var response twoFAGateResponse
	if err := common.Unmarshal(body, &response); err != nil {
		t.Fatalf("failed to decode response %q: %v", string(body), err)
	}
	return response
}

func (r twoFAGateResponse) loginData(t *testing.T) twoFAGateData {
	t.Helper()
	var data twoFAGateData
	if len(r.Data) == 0 {
		return data
	}
	if err := common.Unmarshal(r.Data, &data); err != nil {
		t.Fatalf("failed to decode response data %q: %v", string(r.Data), err)
	}
	return data
}

func getTwoFAGateSessionState(t *testing.T, client *http.Client, baseURL string) twoFAGateSessionState {
	t.Helper()
	resp, err := client.Get(baseURL + "/test/session-state")
	if err != nil {
		t.Fatalf("failed to read session state: %v", err)
	}
	defer resp.Body.Close()
	var state twoFAGateSessionState
	if err := common.DecodeJson(resp.Body, &state); err != nil {
		t.Fatalf("failed to decode session state: %v", err)
	}
	return state
}

func postTwoFAGateJSON(t *testing.T, client *http.Client, url string, payload map[string]any) twoFAGateResponse {
	t.Helper()
	body, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to post %s: %v", url, err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(resp.Body); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	return decodeTwoFAGateResponse(t, raw.Bytes())
}

// newTwoFAGateRouter wires the routes a login path needs plus a probe that
// stands in for any non-password exit (OAuth callback, Passkey, Telegram, the
// desktop handoff) which all reach setupLogin.
func newTwoFAGateRouter() *gin.Engine {
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("twofa-gate-test-secret"))))
	router.POST("/api/user/login", Login)
	router.POST("/api/user/login/2fa", Verify2FALogin)
	router.GET("/test/non-password-login/:id", func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "bad id"})
			return
		}
		user, err := model.GetUserById(id, false)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
			return
		}
		setupLogin(user, c)
	})
	router.GET("/test/session-state", func(c *gin.Context) {
		session := sessions.Default(c)
		c.JSON(http.StatusOK, gin.H{
			"id":                     session.Get("id"),
			"pending_user_id":        session.Get("pending_user_id"),
			"twofa_verified_user_id": session.Get("twofa_verified_user_id"),
		})
	})
	return router
}

// twoFAGateOAuthProvider resolves an existing account by provider user id, so a
// two-factor account can reach the OAuth login exit the way GitHub/OIDC do.
type twoFAGateOAuthProvider struct {
	name string
	user *oauth.OAuthUser
}

func (p *twoFAGateOAuthProvider) GetName() string           { return p.name }
func (p *twoFAGateOAuthProvider) IsEnabled() bool           { return true }
func (p *twoFAGateOAuthProvider) GetProviderPrefix() string { return "gatetest_" }

func (p *twoFAGateOAuthProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*oauth.OAuthToken, error) {
	return &oauth.OAuthToken{AccessToken: "twofa-gate-token", TokenType: "Bearer"}, nil
}

func (p *twoFAGateOAuthProvider) GetUserInfo(ctx context.Context, token *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return p.user, nil
}

func (p *twoFAGateOAuthProvider) IsUserIDTaken(providerUserID string) bool {
	var count int64
	if err := model.DB.Model(&model.User{}).Where("github_id = ?", providerUserID).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func (p *twoFAGateOAuthProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	return model.DB.Where("github_id = ?", providerUserID).First(user).Error
}

func (p *twoFAGateOAuthProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.GitHubId = providerUserID
}

func newTwoFAGateOAuthRouter() *gin.Engine {
	router := newTwoFAGateRouter()
	router.GET("/api/oauth/state", GenerateOAuthCode)
	router.GET("/api/oauth/:provider", HandleOAuth)
	return router
}

// TestOAuthCallbackChallengesTwoFactorUser is the reported defect: an account
// with two-factor enabled used to receive a full session from the OAuth
// callback, so the second factor only ever protected the password form.
func TestOAuthCallbackChallengesTwoFactorUser(t *testing.T) {
	setupTwoFAGateTestDB(t)

	user := createUserForBindTest(t, "gate-oauth-user")
	const providerUserID = "gate-oauth-ext-1"
	if err := model.DB.Model(user).Update("github_id", providerUserID).Error; err != nil {
		t.Fatalf("failed to bind provider id: %v", err)
	}
	enableTwoFAForTest(t, user)

	const providerSlug = "twofa-gate-oauth"
	oauth.Register(providerSlug, &twoFAGateOAuthProvider{
		name: "TwoFA Gate OAuth",
		user: &oauth.OAuthUser{ProviderUserID: providerUserID, Username: user.Username},
	})
	t.Cleanup(func() { oauth.Unregister(providerSlug) })

	router := newTwoFAGateOAuthRouter()
	server := httptest.NewServer(router)
	defer server.Close()

	client := newTestHTTPClient(t)
	state := fetchOAuthStateForTest(t, client, server.URL)
	resp, err := client.Get(server.URL + "/api/oauth/" + providerSlug + "?code=gate-code&state=" + state)
	if err != nil {
		t.Fatalf("failed to call oauth callback: %v", err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(resp.Body); err != nil {
		t.Fatalf("failed to read callback response: %v", err)
	}
	response := decodeTwoFAGateResponse(t, raw.Bytes())

	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}
	data := response.loginData(t)
	if !data.Require2FA {
		t.Fatalf("expected OAuth callback to challenge a two-factor account, got data: %s", string(response.Data))
	}
	if data.ID != 0 {
		t.Fatalf("expected no session user id in the challenge response, got %d", data.ID)
	}
	state2 := getTwoFAGateSessionState(t, client, server.URL)
	if state2.ID != nil {
		t.Fatalf("expected no logged-in session before the second factor, got id %d", *state2.ID)
	}
	if state2.PendingID == nil || *state2.PendingID != user.Id {
		t.Fatalf("expected pending user %d, got %v", user.Id, state2.PendingID)
	}
}

// TestNonPasswordLoginExitChallengesTwoFactorUser drives setupLogin directly,
// standing in for every exit that is not the password form.
func TestNonPasswordLoginExitChallengesTwoFactorUser(t *testing.T) {
	setupTwoFAGateTestDB(t)
	user := createUserForBindTest(t, "gate-direct-user")
	enableTwoFAForTest(t, user)

	router := newTwoFAGateRouter()
	server := httptest.NewServer(router)
	defer server.Close()
	client := newTestHTTPClient(t)

	resp, err := client.Get(server.URL + "/test/non-password-login/" + strconv.Itoa(user.Id))
	if err != nil {
		t.Fatalf("failed to call login exit: %v", err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(resp.Body); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	response := decodeTwoFAGateResponse(t, raw.Bytes())

	if !response.Success || !response.loginData(t).Require2FA {
		t.Fatalf("expected require_2fa challenge, got %s", string(raw.Bytes()))
	}
	if state := getTwoFAGateSessionState(t, client, server.URL); state.ID != nil {
		t.Fatalf("expected no session identity, got id %d", *state.ID)
	}
}

// TestPasswordLoginChallengesTwoFactorUser keeps the original contract of the
// password form, which must stay byte-identical after the gate moves.
func TestPasswordLoginChallengesTwoFactorUser(t *testing.T) {
	setupTwoFAGateTestDB(t)
	user := createUserForBindTest(t, "gate-password-user")
	enableTwoFAForTest(t, user)

	router := newTwoFAGateRouter()
	server := httptest.NewServer(router)
	defer server.Close()
	client := newTestHTTPClient(t)

	response := postTwoFAGateJSON(t, client, server.URL+"/api/user/login", map[string]any{
		"username": user.Username,
		"password": "12345678",
	})
	if !response.Success || !response.loginData(t).Require2FA {
		t.Fatalf("expected password login to challenge two-factor account, got %s", string(response.Data))
	}
	state := getTwoFAGateSessionState(t, client, server.URL)
	if state.ID != nil {
		t.Fatalf("expected no session identity after password challenge, got id %d", *state.ID)
	}
	if state.PendingID == nil || *state.PendingID != user.Id {
		t.Fatalf("expected pending user %d, got %v", user.Id, state.PendingID)
	}
}

// TestTwoFactorVerificationCompletesLogin covers the full flow: challenge, code
// entry, session. It also pins the marker that lets the gate recognize the
// verification instead of challenging again.
func TestTwoFactorVerificationCompletesLogin(t *testing.T) {
	setupTwoFAGateTestDB(t)
	user := createUserForBindTest(t, "gate-verify-user")
	secret := enableTwoFAForTest(t, user)

	router := newTwoFAGateRouter()
	server := httptest.NewServer(router)
	defer server.Close()
	client := newTestHTTPClient(t)

	challenge := postTwoFAGateJSON(t, client, server.URL+"/api/user/login", map[string]any{
		"username": user.Username,
		"password": "12345678",
	})
	if !challenge.loginData(t).Require2FA {
		t.Fatalf("expected challenge before verification, got %s", string(challenge.Data))
	}

	verified := postTwoFAGateJSON(t, client, server.URL+"/api/user/login/2fa", map[string]any{
		"code": currentTOTPForTest(t, secret),
	})
	if !verified.Success {
		t.Fatalf("expected verification to succeed, got %s", verified.Message)
	}
	if verified.loginData(t).ID != user.Id {
		t.Fatalf("expected verified login to return user %d, got %s", user.Id, string(verified.Data))
	}
	state := getTwoFAGateSessionState(t, client, server.URL)
	if state.ID == nil || *state.ID != user.Id {
		t.Fatalf("expected session identity %d, got %v", user.Id, state.ID)
	}
	if state.PendingID != nil {
		t.Fatalf("expected pending state to be cleared, got %v", *state.PendingID)
	}
}

// TestTwoFactorMarkerIsScopedToVerifiedUser makes sure a verification cannot
// leak to another account through the same browser session.
func TestTwoFactorMarkerIsScopedToVerifiedUser(t *testing.T) {
	setupTwoFAGateTestDB(t)
	first := createUserForBindTest(t, "gate-scope-first")
	firstSecret := enableTwoFAForTest(t, first)
	second := createUserForBindTest(t, "gate-scope-second")
	enableTwoFAForTest(t, second)

	router := newTwoFAGateRouter()
	server := httptest.NewServer(router)
	defer server.Close()
	client := newTestHTTPClient(t)

	postTwoFAGateJSON(t, client, server.URL+"/api/user/login", map[string]any{
		"username": first.Username,
		"password": "12345678",
	})
	postTwoFAGateJSON(t, client, server.URL+"/api/user/login/2fa", map[string]any{
		"code": currentTOTPForTest(t, firstSecret),
	})
	if state := getTwoFAGateSessionState(t, client, server.URL); state.ID == nil || *state.ID != first.Id {
		t.Fatalf("expected first user session, got %v", state.ID)
	}

	// A second account logging in on the same session must still be challenged.
	resp, err := client.Get(server.URL + "/test/non-password-login/" + strconv.Itoa(second.Id))
	if err != nil {
		t.Fatalf("failed to call second login exit: %v", err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(resp.Body); err != nil {
		t.Fatalf("failed to read second response: %v", err)
	}
	response := decodeTwoFAGateResponse(t, raw.Bytes())
	if !response.loginData(t).Require2FA {
		t.Fatalf("expected the second two-factor account to be challenged, got %s", string(raw.Bytes()))
	}
	state := getTwoFAGateSessionState(t, client, server.URL)
	if state.PendingID == nil || *state.PendingID != second.Id {
		t.Fatalf("expected pending user %d, got %v", second.Id, state.PendingID)
	}
	if state.VerifiedID != nil {
		t.Fatalf("expected the previous user's verification marker to be dropped, got %v", *state.VerifiedID)
	}
}

// TestLoginExitWithoutTwoFactorSignsIn keeps the gate from taxing accounts that
// never enabled a second factor.
func TestLoginExitWithoutTwoFactorSignsIn(t *testing.T) {
	setupTwoFAGateTestDB(t)
	user := createUserForBindTest(t, "gate-plain-user")

	router := newTwoFAGateRouter()
	server := httptest.NewServer(router)
	defer server.Close()
	client := newTestHTTPClient(t)

	resp, err := client.Get(server.URL + "/test/non-password-login/" + strconv.Itoa(user.Id))
	if err != nil {
		t.Fatalf("failed to call login exit: %v", err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(resp.Body); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	response := decodeTwoFAGateResponse(t, raw.Bytes())
	if !response.Success || response.loginData(t).Require2FA {
		t.Fatalf("expected plain login to succeed, got %s", string(raw.Bytes()))
	}
	if response.loginData(t).ID != user.Id {
		t.Fatalf("expected user %d in response, got %s", user.Id, string(response.Data))
	}
	if state := getTwoFAGateSessionState(t, client, server.URL); state.ID == nil || *state.ID != user.Id {
		t.Fatalf("expected session identity %d, got %v", user.Id, state.ID)
	}
}

func newTwoFAGateDesktopRouter() *gin.Engine {
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("twofa-gate-test-secret"))))
	router.GET("/api/oauth/desktop/poll", PollDesktopOAuth)
	router.POST("/api/user/login/2fa", Verify2FALogin)
	return router
}

// TestDesktopHandoffPollChallengesTwoFactorUser covers the desktop handoff: the
// Tauri window polls in its own cookie jar, and a two-factor account must
// complete the code there before the poll hands it a session.
func TestDesktopHandoffPollChallengesTwoFactorUser(t *testing.T) {
	setupTwoFAGateTestDB(t)
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() { resetDesktopOAuthStoreForTest(nil) })

	user := createUserForBindTest(t, "gate-desktop-user")
	secret := enableTwoFAForTest(t, user)

	request, err := createDesktopOAuthRequest("twofa-gate-oauth", desktopOAuthModeLogin, 0, "")
	if err != nil {
		t.Fatalf("failed to create desktop oauth request: %v", err)
	}
	if err := completeDesktopOAuthRequest(request.State, user.Id); err != nil {
		t.Fatalf("failed to complete desktop oauth request: %v", err)
	}

	router := newTwoFAGateDesktopRouter()
	server := httptest.NewServer(router)
	defer server.Close()
	client := newTestHTTPClient(t)

	resp, err := client.Get(server.URL + "/api/oauth/desktop/poll?handoff_token=" + request.HandoffToken)
	if err != nil {
		t.Fatalf("failed to poll desktop oauth: %v", err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(resp.Body); err != nil {
		t.Fatalf("failed to read poll response: %v", err)
	}
	polled := decodeTwoFAGateResponse(t, raw.Bytes())
	if !polled.Success || !polled.loginData(t).Require2FA {
		t.Fatalf("expected desktop poll to challenge two-factor account, got %s", string(raw.Bytes()))
	}

	verified := postTwoFAGateJSON(t, client, server.URL+"/api/user/login/2fa", map[string]any{
		"code": currentTOTPForTest(t, secret),
	})
	if !verified.Success || verified.loginData(t).ID != user.Id {
		t.Fatalf("expected desktop session after verification, got %s", string(verified.Data))
	}
}

// getLatestSystemLogByIDForTest orders by primary key: two audit rows written
// in the same second share created_at on SQLite, and ORDER BY created_at DESC
// then returns the older row — the ordering by id is the one that stays total.
func getLatestSystemLogByIDForTest(t *testing.T, userID int) *model.Log {
	t.Helper()
	var entry model.Log
	if err := model.LOG_DB.Where("user_id = ? AND type = ?", userID, model.LogTypeSystem).Order("id desc").First(&entry).Error; err != nil {
		t.Fatalf("failed to load latest system log for user %d: %v", userID, err)
	}
	return &entry
}

// TestCustomOAuthJWTLoginAuditsTwoFactorChallenge covers the audit trail: a
// challenge is not a session failure, and the enterprise auth log must say so.
func TestCustomOAuthJWTLoginAuditsTwoFactorChallenge(t *testing.T) {
	setupTwoFAGateTestDB(t)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}
	createJWTDirectProviderForTest(t, privateKey, jwtDirectProviderTestOptions{AutoRegister: true})

	router := newCustomOAuthJWTRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	claims := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss":                "https://issuer.example.com",
			"aud":                "new-api",
			"sub":                "gate-ext-user",
			"preferred_username": "gate-jwt-user",
			"name":               "Gate JWT User",
			"email":              "gate-jwt@example.com",
			"exp":                time.Now().Add(time.Hour).Unix(),
		}
	}

	firstClient := newTestHTTPClient(t)
	firstState := fetchOAuthStateForTest(t, firstClient, server.URL)
	created := postJWTLoginForTest(t, firstClient, server.URL, firstState, signJWTForControllerTest(t, privateKey, claims()))
	if !created.Success {
		t.Fatalf("expected first login to create the user, got %s", created.Message)
	}
	var createdUser model.User
	if err := model.DB.Where("username = ?", "gate-jwt-user").First(&createdUser).Error; err != nil {
		t.Fatalf("failed to load created user: %v", err)
	}
	enableTwoFAForTest(t, &createdUser)

	// A fresh browser session signs in again; the account now owes a code.
	secondClient := newTestHTTPClient(t)
	secondState := fetchOAuthStateForTest(t, secondClient, server.URL)
	second := postJWTLoginForTest(t, secondClient, server.URL, secondState, signJWTForControllerTest(t, privateKey, claims()))
	if !second.Success {
		t.Fatalf("expected challenge response, got failure: %s", second.Message)
	}
	var data twoFAGateData
	if err := common.Unmarshal(second.Data, &data); err != nil {
		t.Fatalf("failed to decode challenge data: %v", err)
	}
	if !data.Require2FA {
		t.Fatalf("expected custom oauth login to challenge two-factor account, got %s", string(second.Data))
	}

	log := getLatestSystemLogByIDForTest(t, createdUser.Id)
	if !strings.Contains(log.Content, "failure_reason=twofa_required") {
		t.Fatalf("expected audit log to record the two-factor challenge, got: %s", log.Content)
	}
	if strings.Contains(log.Content, "failure_reason=session_save_failed") {
		t.Fatalf("expected audit log not to blame the session for a challenge, got: %s", log.Content)
	}
}
