package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
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
// whose handler stamps the session fields for the current request the same way
// setupLogin does, including status.
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
			session.Set("status", common.UserStatusEnabled)
		}
		c.Next()
	})
	router.GET("/probe", handler, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return router
}

// Each test restores a snapshot of the mutable rate-limit settings on cleanup
// so that a test which disables a limiter cannot leak that state into the next
// one. Go runs the tests in a single process and -shuffle reorders them, so a
// hard-coded restore to the default would silently depend on run order.
func snapshotRateLimitGlobals() func() {
	apiEnable, apiNum, apiDur := common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration
	webEnable, webNum, webDur := common.GlobalWebRateLimitEnable, common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration
	critEnable, critNum, critDur := common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration
	searchEnable, searchNum, searchDur := common.SearchRateLimitEnable, common.SearchRateLimitNum, common.SearchRateLimitDuration
	suggEnable, suggNum, suggDur := common.SuggestionRateLimitEnable, common.SuggestionRateLimitNum, common.SuggestionRateLimitDuration
	pollEnable, pollNum, pollDur := common.DesktopOAuthPollRateLimitEnable, common.DesktopOAuthPollRateLimitNum, common.DesktopOAuthPollRateLimitDuration
	dlNum, dlDur := common.DownloadRateLimitNum, common.DownloadRateLimitDuration
	upNum, upDur := common.UploadRateLimitNum, common.UploadRateLimitDuration

	return func() {
		common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = apiEnable, apiNum, apiDur
		common.GlobalWebRateLimitEnable, common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration = webEnable, webNum, webDur
		common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = critEnable, critNum, critDur
		common.SearchRateLimitEnable, common.SearchRateLimitNum, common.SearchRateLimitDuration = searchEnable, searchNum, searchDur
		common.SuggestionRateLimitEnable, common.SuggestionRateLimitNum, common.SuggestionRateLimitDuration = suggEnable, suggNum, suggDur
		common.DesktopOAuthPollRateLimitEnable, common.DesktopOAuthPollRateLimitNum, common.DesktopOAuthPollRateLimitDuration = pollEnable, pollNum, pollDur
		common.DownloadRateLimitNum, common.DownloadRateLimitDuration = dlNum, dlDur
		common.UploadRateLimitNum, common.UploadRateLimitDuration = upNum, upDur
	}
}

func withAdminExempt(t *testing.T, enabled bool, configure func()) {
	t.Helper()
	original := operation_setting.GetRateLimitAdminExempt()
	originalLimiter := inMemoryRateLimiter
	originalRedisEnabled := common.RedisEnabled
	restoreGlobals := snapshotRateLimitGlobals()

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
		restoreGlobals()
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

	router := setupAdminExemptRouter(t, SuggestionRateLimit(), common.RoleAdminUser, true)
	if code := probeTwice(t, router, 2); code != http.StatusOK {
		t.Fatalf("admin should be exempt from the suggestion rate limit, got %d", code)
	}
}

// A session for an account that has since been disabled must not be exempt.
// The middleware runs before UserAuth, so the session status is the only
// signal available about whether the account is still allowed to act.
func TestGlobalAPIRateLimitStillLimitsDisabledAdmin(t *testing.T) {
	withAdminExempt(t, true, func() {
		common.GlobalApiRateLimitEnable = true
		common.GlobalApiRateLimitNum = 2
		common.GlobalApiRateLimitDuration = 60
	})

	router := gin.New()
	gin.SetMode(gin.TestMode)
	store := cookie.NewStore([]byte("admin-exempt-test-secret"))
	router.Use(sessions.Sessions("session", store))
	router.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 1)
		session.Set("role", common.RoleAdminUser)
		session.Set("status", common.UserStatusDisabled)
		c.Next()
	})
	router.GET("/probe", GlobalAPIRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	if code := probeTwice(t, router, 2); code != http.StatusTooManyRequests {
		t.Fatalf("a disabled admin session must stay rate limited, got %d", code)
	}
}

// A session with no status at all is unreadable identity, so it fails closed.
func TestGlobalAPIRateLimitStillLimitsSessionWithoutStatus(t *testing.T) {
	withAdminExempt(t, true, func() {
		common.GlobalApiRateLimitEnable = true
		common.GlobalApiRateLimitNum = 2
		common.GlobalApiRateLimitDuration = 60
	})

	router := gin.New()
	gin.SetMode(gin.TestMode)
	store := cookie.NewStore([]byte("admin-exempt-test-secret"))
	router.Use(sessions.Sessions("session", store))
	router.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 1)
		session.Set("role", common.RoleAdminUser)
		c.Next()
	})
	router.GET("/probe", GlobalAPIRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	if code := probeTwice(t, router, 2); code != http.StatusTooManyRequests {
		t.Fatalf("a session without a status must stay rate limited, got %d", code)
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

	router := setupAdminExemptRouter(t, CriticalRateLimit(), common.RoleRootUser, true)
	if code := probeTwice(t, router, 2); code != http.StatusTooManyRequests {
		t.Fatalf("critical rate limit must still apply to administrators, got %d", code)
	}
}

// The exemption is opt-in per limiter: a limiter honors it only when it is
// wrapped in exemptableLimiter. Pin the documented set so that adding a new
// limiter cannot silently inherit or silently miss the exemption.
//
// DownloadRateLimit and DesktopOAuthPollRateLimit are deliberately absent: the
// first has no enable switch and is not part of the documented set, the second
// exists precisely to throttle polling of the desktop OAuth handoff.
func TestExemptableLimiterCoversExactlyTheDocumentedLimiters(t *testing.T) {
	wantExempt := map[string]func() gin.HandlerFunc{
		"GlobalWebRateLimit":  func() gin.HandlerFunc { return GlobalWebRateLimit() },
		"GlobalAPIRateLimit":  func() gin.HandlerFunc { return GlobalAPIRateLimit() },
		"SearchRateLimit":     func() gin.HandlerFunc { return SearchRateLimit() },
		"SuggestionRateLimit": func() gin.HandlerFunc { return SuggestionRateLimit() },
	}
	wantLimited := map[string]func() gin.HandlerFunc{
		"CriticalRateLimit":         func() gin.HandlerFunc { return CriticalRateLimit() },
		"DownloadRateLimit":         func() gin.HandlerFunc { return DownloadRateLimit() },
		"DesktopOAuthPollRateLimit": func() gin.HandlerFunc { return DesktopOAuthPollRateLimit() },
		"UploadRateLimit":           func() gin.HandlerFunc { return UploadRateLimit() },
	}

	// Every limiter is probed with the exemption on and an administrator
	// session, so a limiter exempts administrators exactly when it is wrapped.
	withAdminExempt(t, true, func() {
		common.GlobalWebRateLimitEnable = true
		common.GlobalWebRateLimitNum = 2
		common.GlobalWebRateLimitDuration = 60
		common.GlobalApiRateLimitEnable = true
		common.GlobalApiRateLimitNum = 2
		common.GlobalApiRateLimitDuration = 60
		common.SearchRateLimitEnable = true
		common.SearchRateLimitNum = 2
		common.SearchRateLimitDuration = 60
		common.SuggestionRateLimitEnable = true
		common.SuggestionRateLimitNum = 2
		common.SuggestionRateLimitDuration = 60
		common.CriticalRateLimitEnable = true
		common.CriticalRateLimitNum = 2
		common.CriticalRateLimitDuration = 60
		common.DesktopOAuthPollRateLimitEnable = true
		common.DesktopOAuthPollRateLimitNum = 2
		common.DesktopOAuthPollRateLimitDuration = 60
		common.DownloadRateLimitNum = 2
		common.DownloadRateLimitDuration = 60
		common.UploadRateLimitNum = 2
		common.UploadRateLimitDuration = 60
	})

	for name, factory := range wantExempt {
		router := setupAdminExemptRouter(t, factory(), common.RoleAdminUser, true)
		if code := probeTwice(t, router, 2); code != http.StatusOK {
			t.Fatalf("%s should exempt administrators, got %d", name, code)
		}
	}
	for name, factory := range wantLimited {
		router := setupAdminExemptRouter(t, factory(), common.RoleAdminUser, true)
		if code := probeTwice(t, router, 2); code != http.StatusTooManyRequests {
			t.Fatalf("%s must keep rate limiting administrators, got %d", name, code)
		}
	}
}

// A router without the session middleware must fail closed, not panic.
// sessions.Default calls MustGet, so reading the session on an engine that
// never registered the middleware would abort the request with a panic.
func TestAdminExemptionFailsClosedWithoutSessionMiddleware(t *testing.T) {
	withAdminExempt(t, true, func() {
		common.GlobalApiRateLimitEnable = true
		common.GlobalApiRateLimitNum = 2
		common.GlobalApiRateLimitDuration = 60
	})

	router := gin.New()
	gin.SetMode(gin.TestMode)
	router.GET("/probe", GlobalAPIRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	if code := probeTwice(t, router, 2); code != http.StatusTooManyRequests {
		t.Fatalf("a router without sessions must stay rate limited, got %d", code)
	}
}

// exportedLimiterFuncs parses rate-limit.go and returns the name of every
// exported function that returns a rate-limit middleware.
//
// The probe test below can only check limiters someone remembered to list. This
// reads the source instead, so a newly added limiter shows up without anyone
// updating a map - which is exactly the case the map-based test missed: adding
// an unwrapped limiter left it passing.
func exportedLimiterFuncs(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "rate-limit.go", nil, 0)
	if err != nil {
		t.Fatalf("failed to parse rate-limit.go: %v", err)
	}

	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !fn.Name.IsExported() {
			continue
		}
		// A limiter returns a middleware: func(...) func(*gin.Context) or
		// func(...) gin.HandlerFunc.
		results := fn.Type.Results
		if results == nil || len(results.List) != 1 {
			continue
		}
		switch typeText(results.List[0].Type) {
		case "func(*gin.Context)", "gin.HandlerFunc":
			names = append(names, fn.Name.Name)
		}
	}
	return names
}

func typeText(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			return x.Name + "." + e.Sel.Name
		}
	case *ast.FuncType:
		return "func(*gin.Context)"
	}
	return ""
}

// Every limiter the source exports must appear in one of the two documented
// sets, so adding a limiter forces a decision instead of silently defaulting to
// "not exempt" (or worse, being wrapped and never noticed).
func TestEveryExportedLimiterIsDocumented(t *testing.T) {
	documented := map[string]struct{}{
		"GlobalWebRateLimit":        {},
		"GlobalAPIRateLimit":        {},
		"SearchRateLimit":           {},
		"SuggestionRateLimit":       {},
		"CriticalRateLimit":         {},
		"DownloadRateLimit":         {},
		"DesktopOAuthPollRateLimit": {},
		"UploadRateLimit":           {},
	}

	exported := exportedLimiterFuncs(t)
	if len(exported) == 0 {
		t.Fatal("parsed no exported limiter functions; the parser or the file layout changed")
	}
	for _, name := range exported {
		if _, ok := documented[name]; !ok {
			t.Errorf("rate-limit.go exports limiter %s, which is in neither the exempt nor the limited set; decide whether it honors the administrator exemption and add it to TestExemptableLimiterCoversExactlyTheDocumentedLimiters", name)
		}
	}
	for name := range documented {
		found := false
		for _, exported := range exported {
			if exported == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is documented as a limiter but rate-limit.go no longer exports it; remove it from the documented set", name)
		}
	}
}
