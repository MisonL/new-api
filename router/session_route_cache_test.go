package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"bytes"
	"io"
	"log"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 登录/注册/2FA 与访问令牌接口会在成功响应里写会话 Cookie，或在响应体里
// 下发明文令牌。缺 Cache-Control 时，共享代理与浏览器都可能把这份响应缓存
// 下来，之后向其他用户复放。DisableCache 必须在校验链路最前，这样被拒绝
// 的响应同样带 no-store，不会留下任何可缓存的中间态。
func TestSessionIssuingRoutesDisableCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.POST("/api/user/login",
		middleware.DisableCache(),
		func(c *gin.Context) { c.JSON(http.StatusUnauthorized, gin.H{"success": false}) },
	)
	engine.GET("/api/user/token",
		middleware.DisableCache(),
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": "access-token"}) },
	)

	rejected := httptest.NewRecorder()
	engine.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/api/user/login", nil))
	require.Equal(t, http.StatusUnauthorized, rejected.Code)
	require.Contains(t, rejected.Header().Get("Cache-Control"), "no-store",
		"被拒绝的登录响应同样不得被缓存")
	require.Equal(t, "no-cache", rejected.Header().Get("Pragma"))

	issued := httptest.NewRecorder()
	engine.ServeHTTP(issued, httptest.NewRequest(http.MethodGet, "/api/user/token", nil))
	require.Equal(t, http.StatusOK, issued.Code)
	require.Contains(t, issued.Header().Get("Cache-Control"), "no-store",
		"访问令牌响应不得被缓存")
}

// OAuth 起始与桌面轮询接口同样会下发会话 Cookie：/api/oauth/state 在响应里
// 写 session，桌面 poll 在授权完成时签发会话。这两类响应若被共享代理或浏览器
// 缓存，等同于把某个用户的会话复放给下一个人，必须与登录/注册同等待遇。
func TestOAuthSessionRoutesDisableCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 企业认证 handler 会立即查库；不打桩时 model.DB 为 nil 会 panic，
	// 掩盖缓存头断言。用内存库承载，未注册的 provider 会正常返回错误响应。
	prevDB, prevLogDB := model.DB, model.LOG_DB
	prevUsingSQLite := common.UsingSQLite
	db, err := gorm.Open(sqlite.Open("file:oauth_session_route_cache?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.New(log.New(io.Discard, "", 0), gormlogger.Config{
			LogLevel: gormlogger.Silent,
		}),
	})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite = true
	require.NoError(t, db.AutoMigrate(&model.User{}))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = prevDB, prevLogDB
		common.UsingSQLite = prevUsingSQLite
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	// 桌面 poll 用的是 Redis 限流器，测试环境没有 Redis 会直接空指针。
	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	oldRedisEnabled := common.RedisEnabled
	oldRDB := common.RDB
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
		_ = client.Close()
		server.Close()
	})
	common.RedisEnabled = true
	common.RDB = client

	engine := gin.New()
	store := cookie.NewStore([]byte("oauth-session-route-test-secret"))
	engine.Use(sessions.Sessions("session", store))
	SetApiRouter(engine)

	for _, target := range []string{
		"/api/oauth/state",
		"/api/oauth/desktop/poll?handoff_token=probe-token",
		// WeChat / Telegram 的登录与绑定端点同样调用 setupLogin 签发会话。
		// telegram/bind 与前端调用一致，是 GET。
		"/api/oauth/wechat",
		"/api/oauth/telegram/login",
		"/api/oauth/telegram/bind",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = "192.0.2.9:12345"
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		require.NotEqual(t, http.StatusNotFound, rec.Code, "%s 必须已注册", target)
		require.Contains(t, rec.Header().Get("Cache-Control"), "no-store",
			"%s 会下发或引用会话，必须带 no-store（实际码 %d）", target, rec.Code)
	}

	// wechat/bind 是 POST：只验证路由存在且带 no-store，不触发绑定逻辑。
	for _, target := range []string{
		"/api/oauth/wechat/bind",
	} {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		req.RemoteAddr = "192.0.2.9:12345"
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		require.NotEqual(t, http.StatusNotFound, rec.Code, "%s 必须已注册", target)
		require.Contains(t, rec.Header().Get("Cache-Control"), "no-store",
			"%s 绑定成功后同样签发会话，必须带 no-store（实际码 %d）", target, rec.Code)
	}

	// 企业认证：JWT / header 登录与 CAS 回调都调用 setupLoginWithResult 签发
	// 会话，start 跳转也会写 state 会话，必须与标准 OAuth 同一契约。
	for method, targets := range map[string][]string{
		http.MethodPost: {
			"/api/auth/external/probe/jwt/login",
			"/api/auth/external/probe/header/login",
		},
		http.MethodGet: {
			"/api/auth/external/probe/cas/start",
			"/api/auth/external/probe/cas/callback",
		},
	} {
		for _, target := range targets {
			req := httptest.NewRequest(method, target, nil)
			req.RemoteAddr = "192.0.2.9:12345"
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			require.NotEqual(t, http.StatusNotFound, rec.Code, "%s %s 必须已注册", method, target)
			require.Contains(t, rec.Header().Get("Cache-Control"), "no-store",
				"%s %s 会下发或消费会话，必须带 no-store（实际码 %d）", method, target, rec.Code)
		}
	}
}

// 真实路由表上的顺序验证：超过匿名上限的请求体在 body-limit 中间件就被
// 413 拦下，只有 DisableCache 排在这个中间件之前，被拒绝的响应才带
// no-store。否则共享代理可能缓存这条 413，影响后续同 URL 的请求。
func TestLoginRouteRejectsOversizedBodyWithNoStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	constant.AnonymousRequestBodyLimitKB = 1
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = 0 })

	engine := gin.New()
	SetApiRouter(engine)

	body := bytes.Repeat([]byte("a"), (1<<10)+1)
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(body))
	req.RemoteAddr = "192.0.2.7:12345"
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code,
		"超限请求体应被匿名上限拦截，实际: %d", rec.Code)
	require.Contains(t, rec.Header().Get("Cache-Control"), "no-store",
		"被匿名上限拒绝的登录响应也必须带 no-store")
}
