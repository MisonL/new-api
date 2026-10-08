package router

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newAnonymousBodyLimitTestEngine builds the real /api router. Rate limits are
// switched off before registration (they are count-based, not size-based, and
// would only add noise), the anonymous ceiling is pinned to the startup
// default because env initialization does not run under `go test`, and a
// recovery middleware turns any handler panic into a 500 instead of killing
// the test binary.
func newAnonymousBodyLimitTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	originalAnonymousKB := constant.AnonymousRequestBodyLimitKB
	originalGlobalRate := common.GlobalApiRateLimitEnable
	originalCriticalRate := common.CriticalRateLimitEnable
	t.Cleanup(func() {
		constant.AnonymousRequestBodyLimitKB = originalAnonymousKB
		common.GlobalApiRateLimitEnable = originalGlobalRate
		common.CriticalRateLimitEnable = originalCriticalRate
	})
	constant.AnonymousRequestBodyLimitKB = 512
	common.GlobalApiRateLimitEnable = false
	common.CriticalRateLimitEnable = false

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("anonymous-body-limit-router-test-secret"))))
	SetApiRouter(engine)
	return engine
}

// Every route reachable without authentication must carry
// AnonymousRequestBodyLimit: /api has no group-level body ceiling of its own
// beyond the 128MB ingress backstop, and these routes buffer whatever a client
// sends. An oversized body has to be rejected before the handler runs, which
// is observable as a 413.
func TestAnonymousRoutesRejectOversizedBodies(t *testing.T) {
	engine := newAnonymousBodyLimitTestEngine(t)

	routes := []string{
		"/api/setup",
		"/api/user/reset",
		"/api/oauth/email/bind",
		"/api/oauth/wechat/bind",
		"/api/auth/external/test-provider/jwt/login",
		"/api/auth/external/test-provider/header/login",
		"/api/stripe/webhook",
		"/api/creem/webhook",
		"/api/waffo/webhook",
		"/api/user/register",
		"/api/user/login",
		"/api/user/login/2fa",
		"/api/user/passkey/login/begin",
		"/api/user/passkey/login/finish",
		"/api/user/epay/notify",
		"/api/subscription/epay/notify",
		"/api/subscription/epay/return",
	}
	for _, path := range routes {
		rec := httptest.NewRecorder()
		body := bytes.NewReader(bytes.Repeat([]byte("a"), (512<<10)+1))
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, body))
		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code,
			"%s must reject an oversized body before the handler runs (got %d)", path, rec.Code)
	}
}

// Negative control: a session-gated route must not carry the anonymous
// ceiling. An oversized body there has to reach authentication (401); a 413
// would mean the limit was mounted too widely.
func TestAuthenticatedRouteIsNotBoundedByAnonymousLimit(t *testing.T) {
	engine := newAnonymousBodyLimitTestEngine(t)

	rec := httptest.NewRecorder()
	body := bytes.NewReader(bytes.Repeat([]byte("a"), (512<<10)+1))
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/user/self", body))

	require.Equal(t, http.StatusUnauthorized, rec.Code,
		"an oversized body on a session-gated route must reach auth, not the anonymous size limit")
}
