package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TelegramBind 是已登录用户把 Telegram 账户绑到自己账号上的入口。签名
// payload 只能证明它由 bot 签发，不能证明绑定动作由本浏览器发起：攻击者
// 拿自己的签名 payload 拼成 /api/oauth/telegram/bind 链接送进受害者浏览器，
// 受害者账号就会被绑到攻击者的 Telegram，之后攻击者可用 Telegram 登录
// 进入受害者账户。与 TelegramLogin/WeChatAuth 相同，必须校验会话内 state。

func newTelegramBindRouter() *gin.Engine {
	router := gin.New()
	router.Use(sessions.Sessions("telegram-bind-test", cookie.NewStore([]byte("telegram-bind-secret"))))
	router.GET("/api/oauth/telegram/bind", TelegramBind)
	router.GET("/test/set-session", func(c *gin.Context) {
		session := sessions.Default(c)
		if id := c.Query("id"); id != "" {
			value, err := strconv.Atoi(id)
			if err != nil {
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			session.Set("id", value)
		}
		session.Set("oauth_state", c.Query("state"))
		if err := session.Save(); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	return router
}

func seedTelegramBindUser(t *testing.T, username string) *model.User {
	t.Helper()
	user := &model.User{
		Username:    username,
		Password:    "12345678",
		DisplayName: username,
		Email:       username + "@example.local",
		Role:        1,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))
	require.NotZero(t, user.Id)
	return user
}

func telegramBindSessionCookies(t *testing.T, router *gin.Engine, userId int, state string) []*http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/set-session?id="+strconv.Itoa(userId)+"&state="+state, nil)
	router.ServeHTTP(recorder, req)
	cookies := recorder.Result().Cookies()
	require.NotEmpty(t, cookies, "session store must issue a cookie")
	return cookies
}

func enableTelegramBindTest(t *testing.T) {
	t.Helper()
	originalEnabled := common.TelegramOAuthEnabled
	originalToken := common.TelegramBotToken
	common.TelegramOAuthEnabled = true
	common.TelegramBotToken = "bind-state-bot-token"
	t.Cleanup(func() {
		common.TelegramOAuthEnabled = originalEnabled
		common.TelegramBotToken = originalToken
	})
}

func TestTelegramBindRequiresBrowserState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupHeaderPolicyControllerTestDB(t, &model.User{})
	enableTelegramBindTest(t)

	user := seedTelegramBindUser(t, "tg-bind-no-state")
	router := newTelegramBindRouter()
	cookies := telegramBindSessionCookies(t, router, user.Id, "bind-state")

	signedQuery := telegramSignedQuery(t, common.TelegramBotToken, map[string]string{
		"id":         "515151",
		"first_name": "NoState",
		"username":   "no_state_user",
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/bind?"+signedQuery, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusForbidden, recorder.Code,
		"bind without state must be rejected: %s", recorder.Body.String())

	reloaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	require.Empty(t, reloaded.TelegramId, "a rejected bind must not write the telegram id")
}

func TestTelegramBindRejectsForeignState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupHeaderPolicyControllerTestDB(t, &model.User{})
	enableTelegramBindTest(t)

	user := seedTelegramBindUser(t, "tg-bind-foreign-state")
	router := newTelegramBindRouter()
	cookies := telegramBindSessionCookies(t, router, user.Id, "victim-state")

	signedQuery := telegramSignedQuery(t, common.TelegramBotToken, map[string]string{
		"id":         "525252",
		"first_name": "ForeignState",
		"username":   "foreign_state_user",
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/bind?"+signedQuery+"&state=attacker-state", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusForbidden, recorder.Code,
		"bind with a state from another session must be rejected: %s", recorder.Body.String())

	reloaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	require.Empty(t, reloaded.TelegramId, "a rejected bind must not write the telegram id")
}

func TestTelegramBindWithMatchingStateBindsOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupHeaderPolicyControllerTestDB(t, &model.User{})
	enableTelegramBindTest(t)

	user := seedTelegramBindUser(t, "tg-bind-ok")
	router := newTelegramBindRouter()
	cookies := telegramBindSessionCookies(t, router, user.Id, "bind-ok-state")

	signedQuery := telegramSignedQuery(t, common.TelegramBotToken, map[string]string{
		"id":         "535353",
		"first_name": "Bound",
		"username":   "bound_user",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/bind?"+signedQuery+"&state=bind-ok-state", nil)
	req.Header.Set("Accept", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var body struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.True(t, body.Success, recorder.Body.String())

	reloaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, "535353", reloaded.TelegramId)

	// state 是一次性 nonce：成功的绑定必须把它消费掉，重放同一 state
	// （即使 payload 换一份）也要被拒绝。
	replayQuery := telegramSignedQuery(t, common.TelegramBotToken, map[string]string{
		"id":         "545454",
		"first_name": "Replay",
		"username":   "replay_user",
	})
	replay := httptest.NewRecorder()
	replayReq := httptest.NewRequest(http.MethodGet, "/api/oauth/telegram/bind?"+replayQuery+"&state=bind-ok-state", nil)
	for _, c := range recorder.Result().Cookies() {
		replayReq.AddCookie(c)
	}
	if len(recorder.Result().Cookies()) == 0 {
		for _, c := range cookies {
			replayReq.AddCookie(c)
		}
	}
	router.ServeHTTP(replay, replayReq)
	require.Equal(t, http.StatusForbidden, replay.Code,
		"a consumed state must not be replayable: %s", replay.Body.String())

	stillBound, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, "535353", stillBound.TelegramId, "a replayed bind must not overwrite the binding")
}
