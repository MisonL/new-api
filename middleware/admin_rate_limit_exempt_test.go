package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// setupAdminExemptRouter builds a router whose session middleware mirrors
// production ordering (sessions are registered before the rate limiters) and
// whose handler stamps the session role for the current request.
func setupAdminExemptRouter(t *testing.T, handler gin.HandlerFunc, role int, withSession bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	store := cookie.NewStore([]byte("admin-exempt-test-secret"))
	router.Use(sessions.Sessions("session", store))
	router.Use(func(c *gin.Context) {
		if withSession {
			session := sessions.Default(c)
			session.Set("id", 1)
			session.Set("role", role)
		}
		c.Next()
	})
	router.GET("/probe", handler, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return router
}

func withAdminExempt(t *testing.T, enabled bool, configure func()) {
	t.Helper()
	original := operation_setting.GetRateLimitAdminExempt()
	originalLimiter := inMemoryRateLimiter
	originalRedisEnabled := common.RedisEnabled

	operation_setting.SetRateLimitAdminExempt(enabled)
	inMemoryRateLimiter = &common.InMemoryRateLimiter{}
	common.RedisEnabled = false
	if configure != nil {
		configure()
	}

	t.Cleanup(func() {
		operation_setting.SetRateLimitAdminExempt(original)
		inMemoryRateLimiter = originalLimiter
		common.RedisEnabled = originalRedisEnabled
	})
}

func probeTwice(t *testing.T, router *gin.Engine, limit int) int {
	t.Helper()
	code := 0
	for i := 0; i <= limit; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/probe", nil)
		request.RemoteAddr = "10.9.9.9:12345"
		router.ServeHTTP(recorder, request)
		code = recorder.Code
	}
	return code
}

func TestGlobalAPIRateLimitExemptsAdminWhenEnabled(t *testing.T) {
	originalNum := common.GlobalApiRateLimitNum
	originalDuration := common.GlobalApiRateLimitDuration
	originalEnable := common.GlobalApiRateLimitEnable
	t.Cleanup(func() {
		common.GlobalApiRateLimitNum = originalNum
		common.GlobalApiRateLimitDuration = originalDuration
		common.GlobalApiRateLimitEnable = originalEnable
	})

	for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
		withAdminExempt(t, true, func() {
			common.GlobalApiRateLimitEnable = true
			common.GlobalApiRateLimitNum = 2
			common.GlobalApiRateLimitDuration = 60
		})
		router := setupAdminExemptRouter(t, GlobalAPIRateLimit(), role, true)
		if code := probeTwice(t, router, 2); code != http.StatusOK {
			t.Fatalf("role %d should be exempt from the global API rate limit, got %d", role, code)
		}
	}
}

func TestGlobalAPIRateLimitStillLimitsCommonUserWhenAdminExemptEnabled(t *testing.T) {
	withAdminExempt(t, true, func() {
		common.GlobalApiRateLimitEnable = true
		common.GlobalApiRateLimitNum = 2
		common.GlobalApiRateLimitDuration = 60
	})
	t.Cleanup(func() {
		common.GlobalApiRateLimitEnable = true
	})

	router := setupAdminExemptRouter(t, GlobalAPIRateLimit(), common.RoleCommonUser, true)
	if code := probeTwice(t, router, 2); code != http.StatusTooManyRequests {
		t.Fatalf("common user should still be rate limited, got %d", code)
	}
}

func TestGlobalAPIRateLimitStillLimitsAdminWhenExemptDisabled(t *testing.T) {
	withAdminExempt(t, false, func() {
		common.GlobalApiRateLimitEnable = true
		common.GlobalApiRateLimitNum = 2
		common.GlobalApiRateLimitDuration = 60
	})
	t.Cleanup(func() {
		common.GlobalApiRateLimitEnable = true
	})

	router := setupAdminExemptRouter(t, GlobalAPIRateLimit(), common.RoleAdminUser, true)
	if code := probeTwice(t, router, 2); code != http.StatusTooManyRequests {
		t.Fatalf("admin should be limited while the exemption is off, got %d", code)
	}
}

func TestGlobalAPIRateLimitStillLimitsAnonymousWhenAdminExemptEnabled(t *testing.T) {
	withAdminExempt(t, true, func() {
		common.GlobalApiRateLimitEnable = true
		common.GlobalApiRateLimitNum = 2
		common.GlobalApiRateLimitDuration = 60
	})
	t.Cleanup(func() {
		common.GlobalApiRateLimitEnable = true
	})

	router := setupAdminExemptRouter(t, GlobalAPIRateLimit(), 0, false)
	if code := probeTwice(t, router, 2); code != http.StatusTooManyRequests {
		t.Fatalf("anonymous request should still be rate limited, got %d", code)
	}
}

func TestGlobalWebRateLimitExemptsAdminWhenEnabled(t *testing.T) {
	withAdminExempt(t, true, func() {
		common.GlobalWebRateLimitEnable = true
		common.GlobalWebRateLimitNum = 2
		common.GlobalWebRateLimitDuration = 60
	})
	t.Cleanup(func() {
		common.GlobalWebRateLimitEnable = true
	})

	router := setupAdminExemptRouter(t, GlobalWebRateLimit(), common.RoleAdminUser, true)
	if code := probeTwice(t, router, 2); code != http.StatusOK {
		t.Fatalf("admin should be exempt from the global web rate limit, got %d", code)
	}
}

func TestSearchRateLimitExemptsAdminWhenEnabled(t *testing.T) {
	withAdminExempt(t, true, func() {
		common.SearchRateLimitEnable = true
		common.SearchRateLimitNum = 2
		common.SearchRateLimitDuration = 60
	})
	t.Cleanup(func() {
		common.SearchRateLimitEnable = true
	})

	router := setupAdminExemptRouter(t, SearchRateLimit(), common.RoleAdminUser, true)
	if code := probeTwice(t, router, 2); code != http.StatusOK {
		t.Fatalf("admin should be exempt from the search rate limit, got %d", code)
	}
}

func TestSuggestionRateLimitExemptsAdminWhenEnabled(t *testing.T) {
	withAdminExempt(t, true, func() {
		common.SuggestionRateLimitEnable = true
		common.SuggestionRateLimitNum = 2
		common.SuggestionRateLimitDuration = 60
	})
	t.Cleanup(func() {
		common.SuggestionRateLimitEnable = true
	})

	router := setupAdminExemptRouter(t, SuggestionRateLimit(), common.RoleAdminUser, true)
	if code := probeTwice(t, router, 2); code != http.StatusOK {
		t.Fatalf("admin should be exempt from the suggestion rate limit, got %d", code)
	}
}

// CriticalRateLimit protects login, registration and payment endpoints. It must
// never exempt administrators, otherwise a compromised admin account loses the
// IP-level brute-force protection on the very endpoints that mint sessions.
func TestCriticalRateLimitNeverExemptsAdmin(t *testing.T) {
	withAdminExempt(t, true, func() {
		common.CriticalRateLimitEnable = true
		common.CriticalRateLimitNum = 2
		common.CriticalRateLimitDuration = 60
	})
	t.Cleanup(func() {
		common.CriticalRateLimitEnable = true
	})

	router := setupAdminExemptRouter(t, CriticalRateLimit(), common.RoleRootUser, true)
	if code := probeTwice(t, router, 2); code != http.StatusTooManyRequests {
		t.Fatalf("critical rate limit must still apply to administrators, got %d", code)
	}
}
