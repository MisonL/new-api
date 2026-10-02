package middleware

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// setupAdminExemptRouter builds a router whose session middleware mirrors
// production ordering (sessions are registered before the rate limiters) and
// whose handler stamps the session fields for the current request the same way
// setupLogin does, including status. An optional contextSetup stamps any
// further context values the limiter under test reads.
func setupAdminExemptRouter(t *testing.T, handler gin.HandlerFunc, role int, withSession bool, contextSetup ...func(c *gin.Context)) *gin.Engine {
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
		for _, setup := range contextSetup {
			if setup != nil {
				setup(c)
			}
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
// wrapped in exemptableLimiter. The two maps below are the single source of
// truth for that decision, and both the source-enumeration test and the
// behavioural probe test read them, so a limiter cannot be added to one
// without the other noticing.
//
// contextSetup stamps the gin context values a limiter needs beyond the
// administrator session. ModelRequestRateLimit keys on c.GetInt("id"), which
// the session middleware does not provide.
type limiterCase struct {
	build        func() gin.HandlerFunc
	contextSetup func(c *gin.Context)
}

// exemptLimiters must stop limiting an enabled administrator. This is the
// documented exempt set; wrapping is what makes a limiter exempt, so a
// regression that removes a wrap fails the probe below.
var exemptLimiters = map[string]limiterCase{
	"GlobalWebRateLimit":  {build: func() gin.HandlerFunc { return GlobalWebRateLimit() }},
	"GlobalAPIRateLimit":  {build: func() gin.HandlerFunc { return GlobalAPIRateLimit() }},
	"SearchRateLimit":     {build: func() gin.HandlerFunc { return SearchRateLimit() }},
	"SuggestionRateLimit": {build: func() gin.HandlerFunc { return SuggestionRateLimit() }},
}

// limitedLimiters must keep limiting administrators even with the exemption
// on. CriticalRateLimit protects login, registration, password reset and
// payment; DownloadRateLimit and UploadRateLimit are operator actions;
// DesktopOAuthPollRateLimit exists to throttle polling; ModelRequestRateLimit
// is a per-user quota, and EmailVerificationRateLimit guards mail abuse.
var limitedLimiters = map[string]limiterCase{
	"CriticalRateLimit":          {build: func() gin.HandlerFunc { return CriticalRateLimit() }},
	"DownloadRateLimit":          {build: func() gin.HandlerFunc { return DownloadRateLimit() }},
	"DesktopOAuthPollRateLimit":  {build: func() gin.HandlerFunc { return DesktopOAuthPollRateLimit() }},
	"UploadRateLimit":            {build: func() gin.HandlerFunc { return UploadRateLimit() }},
	"ModelRequestRateLimit":      {build: func() gin.HandlerFunc { return ModelRequestRateLimit() }, contextSetup: func(c *gin.Context) { c.Set("id", 1) }},
	"EmailVerificationRateLimit": {build: func() gin.HandlerFunc { return EmailVerificationRateLimit() }},
}

func TestExemptableLimiterCoversExactlyTheDocumentedLimiters(t *testing.T) {
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

	// ModelRequestRateLimit reads its thresholds from the setting package and
	// is a no-op while disabled, so turn it on for the probe and restore after.
	originalEnabled := setting.ModelRequestRateLimitEnabled
	originalCount := setting.ModelRequestRateLimitCount
	originalSuccess := setting.ModelRequestRateLimitSuccessCount
	originalDuration := setting.ModelRequestRateLimitDurationMinutes
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitCount = 0
	setting.ModelRequestRateLimitSuccessCount = 2
	setting.ModelRequestRateLimitDurationMinutes = 1
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = originalEnabled
		setting.ModelRequestRateLimitCount = originalCount
		setting.ModelRequestRateLimitSuccessCount = originalSuccess
		setting.ModelRequestRateLimitDurationMinutes = originalDuration
	})

	for name, limiter := range exemptLimiters {
		router := setupAdminExemptRouter(t, limiter.build(), common.RoleAdminUser, true, limiter.contextSetup)
		if code := probeTwice(t, router, 2); code != http.StatusOK {
			t.Fatalf("%s should exempt administrators, got %d", name, code)
		}
	}
	for name, limiter := range limitedLimiters {
		router := setupAdminExemptRouter(t, limiter.build(), common.RoleAdminUser, true, limiter.contextSetup)
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

// limiterSourceMarkers identify a file as a rate-limiter source: a limiter
// reaches one of these when it rejects a request. A middleware that references
// none of them is not a limiter and needs no exemption decision.
//
// "429" is included because a limiter may reject without going through any of
// the shared helpers, for instance with its own c.JSON(429, ...). The only
// non-limiter file that carries the literal is utils.go, which exports no
// middleware, so the marker adds no false positive.
var limiterSourceMarkers = []string{
	"StatusTooManyRequests",
	"abortRateLimited",
	"inMemoryRateLimiter",
	"checkRedisRateLimit",
	"redisRateLimiter",
	"429",
}

// exportedLimiterFuncs returns the name of every exported function that returns
// a rate-limit middleware, across every limiter source file in the package.
//
// Parsing only rate-limit.go left the limiters in model-rate-limit.go and
// email-verification-rate-limit.go invisible: adding an unwrapped limiter to
// either file passed both this test and the probe test. Scanning every file for
// the primitives a limiter must reference closes that gap without depending on
// the file name, so a limiter added under any name still forces a decision.
func exportedLimiterFuncs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("failed to read the middleware package directory: %v", err)
	}

	fset := token.NewFileSet()
	var names []string
	sources := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}
		isLimiterSource := false
		for _, marker := range limiterSourceMarkers {
			if bytes.Contains(source, []byte(marker)) {
				isLimiterSource = true
				break
			}
		}
		if !isLimiterSource {
			continue
		}
		sources++

		file, err := parser.ParseFile(fset, name, source, 0)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", name, err)
		}
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
	}
	if sources == 0 {
		t.Fatal("found no rate-limiter source files; the marker list or the package layout changed")
	}
	sort.Strings(names)
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
// "not exempt" (or worse, being wrapped and never noticed). The sets are the
// same ones the behavioural probe above drives, so a limiter cannot be listed
// here without a probe case, or wrapped without the probe disagreeing.
func TestEveryExportedLimiterIsDocumented(t *testing.T) {
	documented := map[string]struct{}{}
	for name := range exemptLimiters {
		documented[name] = struct{}{}
	}
	for name := range limitedLimiters {
		documented[name] = struct{}{}
	}

	exported := exportedLimiterFuncs(t)
	if len(exported) == 0 {
		t.Fatal("parsed no exported limiter functions; the parser or the file layout changed")
	}
	for _, name := range exported {
		if _, ok := documented[name]; !ok {
			t.Errorf("the middleware package exports limiter %s, which is in neither exemptLimiters nor limitedLimiters; decide whether it honors the administrator exemption and add a probe case", name)
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
			t.Errorf("%s is listed as a limiter but no limiter source file exports it any more; remove it from the documented sets", name)
		}
	}
}
