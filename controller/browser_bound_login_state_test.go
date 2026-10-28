package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 微信与 Telegram 登录是公开 GET，且没有 IdP 侧的 state 往返：攻击者用
// 自己的账号拿到 code / 签名 payload 后，把回调 URL 送进受害者浏览器，
// 受害者就会被登进攻击者账号。两者都要求会话内的 state（由
// /api/oauth/state 签发），把登录绑定到发起请求的浏览器。

func newBrowserBoundLoginRouter() *gin.Engine {
	router := gin.New()
	router.Use(sessions.Sessions("login-state-test", cookie.NewStore([]byte("login-state-secret"))))
	router.GET("/api/oauth/state", GenerateOAuthCode)
	router.GET("/api/oauth/wechat", WeChatAuth)
	router.GET("/api/oauth/telegram/login", TelegramLogin)
	router.GET("/test/set-state", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("oauth_state", c.Query("value"))
		if err := session.Save(); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	return router
}

// withLoginState 在一个请求里签发并复用会话 Cookie，模拟真实浏览器。
func withLoginState(t *testing.T, router *gin.Engine, state string) []*http.Cookie {
	t.Helper()
	seed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/state", nil)
	router.ServeHTTP(seed, req)
	cookies := seed.Result().Cookies()
	require.NotEmpty(t, cookies, "state endpoint must set a session cookie")
	if state == "" {
		return cookies
	}
	// 覆盖成测试可控的 state。
	return overwriteOAuthState(t, router, cookies, state)
}

func overwriteOAuthState(t *testing.T, router *gin.Engine, cookies []*http.Cookie, state string) []*http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/set-state?value="+state, nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	router.ServeHTTP(recorder, req)
	if updated := recorder.Result().Cookies(); len(updated) > 0 {
		return updated
	}
	return cookies
}

func TestWeChatAuthRequiresBrowserState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalEnabled := common.WeChatAuthEnabled
	common.WeChatAuthEnabled = true
	t.Cleanup(func() { common.WeChatAuthEnabled = originalEnabled })

	router := newBrowserBoundLoginRouter()
	cookies := withLoginState(t, router, "wechat-state")

	// 没有 state：必须拒绝，且不去兑换 code。
	noState := httptest.NewRecorder()
	router.ServeHTTP(noState, httptest.NewRequest(http.MethodGet, "/api/oauth/wechat?code=abc", nil))
	require.Equal(t, http.StatusForbidden, noState.Code,
		"WeChat login without state must be rejected: %s", noState.Body.String())

	// 带别人的 state（攻击者自己会话里的 state）对受害者会话无效。
	wrong := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/wechat?code=abc&state=attacker-state", nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	router.ServeHTTP(wrong, req)
	require.Equal(t, http.StatusForbidden, wrong.Code,
		"WeChat login with a foreign state must be rejected: %s", wrong.Body.String())

	// 正确 state 才会走到 code 兑换（这里没有微信服务，失败也不是 403）。
	right := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/oauth/wechat?code=abc&state=wechat-state", nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	router.ServeHTTP(right, req)
	require.NotEqual(t, http.StatusForbidden, right.Code,
		"a matching state must pass the browser binding check")
}

func TestTelegramLoginRequiresBrowserState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	originalEnabled := common.TelegramOAuthEnabled
	originalToken := common.TelegramBotToken
	common.TelegramOAuthEnabled = true
	common.TelegramBotToken = "login-state-bot-token"
	t.Cleanup(func() {
		common.TelegramOAuthEnabled = originalEnabled
		common.TelegramBotToken = originalToken
	})

	user := &model.User{
		Username:    "tg-login-state-user",
		Password:    "12345678",
		DisplayName: "TG Login State User",
		Email:       "tg-login-state@example.local",
		Role:        1,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		TelegramId:  "424242",
	}
	require.NoError(t, user.Insert(0))

	router := newBrowserBoundLoginRouter()
	cookies := withLoginState(t, router, "tg-state")
	signedQuery := telegramSignedQuery(t, "login-state-bot-token", map[string]string{
		"id":        "424242",
		"first_name": "Victim",
		"username":  "tg_login_state_user",
	})

	// 没有 state：拒绝，且不签发会话。
	noState := httptest.NewRecorder()
	router.ServeHTTP(noState, httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/login?"+signedQuery, nil))
	require.Equal(t, http.StatusForbidden, noState.Code,
		"Telegram login without state must be rejected: %s", noState.Body.String())

	// 受害者浏览器带着自己会话的 state：正常登录并签发会话。
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/login?"+signedQuery+"&state=tg-state", nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	ok := httptest.NewRecorder()
	router.ServeHTTP(ok, req)
	require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
	require.Contains(t, ok.Body.String(), `"success":true`)
}

// telegramSignedQuery reproduces the widget's HMAC scheme: hash =
// HMAC_SHA256(key=SHA256(bot_token), data=sorted "k=v" lines joined by \n).
func telegramSignedQuery(t *testing.T, botToken string, fields map[string]string) string {
	t.Helper()
	fields["auth_date"] = strconv.FormatInt(time.Now().Unix(), 10)
	lines := make([]string, 0, len(fields))
	for k, v := range fields {
		lines = append(lines, k+"="+v)
	}
	sort.Strings(lines)
	payload := strings.Join(lines, "\n")

	key := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, key[:])
	_, err := mac.Write([]byte(payload))
	require.NoError(t, err)
	fields["hash"] = hex.EncodeToString(mac.Sum(nil))

	query := strings.Builder{}
	for i, name := range []string{"auth_date", "first_name", "id", "username", "hash"} {
		if i > 0 {
			query.WriteByte('&')
		}
		query.WriteString(name + "=" + queryEscape(fields[name]))
	}
	return query.String()
}

func queryEscape(value string) string {
	return strings.NewReplacer(" ", "%20", "+", "%2B").Replace(value)
}
