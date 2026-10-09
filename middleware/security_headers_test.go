package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newSecurityHeaderProbe(register func(r *gin.Engine)) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())
	register(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	return w
}

func TestSecurityHeadersSetsSafeDefaults(t *testing.T) {
	w := newSecurityHeaderProbe(func(r *gin.Engine) {
		r.GET("/probe", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})
	})

	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy = %q, want %q", got, "strict-origin-when-cross-origin")
	}
}

func TestSecurityHeadersCoverUnmatchedRoutes(t *testing.T) {
	// The SPA fallback and the relay 404 both run through NoRoute, so a path
	// that matches no handler must still carry the headers.
	w := newSecurityHeaderProbe(func(r *gin.Engine) {
		r.NoRoute(func(c *gin.Context) {
			c.Data(http.StatusNotFound, "application/json", []byte(`{"error":{}}`))
		})
	})

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy = %q, want %q", got, "strict-origin-when-cross-origin")
	}
}

func TestSecurityHeadersLeaveFramingAndTransportPoliciesUnset(t *testing.T) {
	// These are deliberately absent: framing depends on whether the deployment
	// embeds the console in an iframe, a CSP has to be reconciled with the
	// frontends' inline scripts, and HSTS is only meaningful at the TLS
	// terminator, which this process cannot see. Adding one here must be a
	// deliberate decision, so this test fails loudly if one appears.
	w := newSecurityHeaderProbe(func(r *gin.Engine) {
		r.GET("/probe", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})
	})

	for _, header := range []string{
		"X-Frame-Options",
		"Content-Security-Policy",
		"Strict-Transport-Security",
	} {
		if got := w.Header().Get(header); got != "" {
			t.Fatalf("%s = %q, want it left unset", header, got)
		}
	}
}

// TestSecurityHeadersIsRegisteredInMainChain pins the wiring, not just the
// handler. The header tests above build their own engine and call
// r.Use(SecurityHeaders()) directly, so deleting the registration in main.go
// would leave every one of them green while production served no headers at
// all. This reads the real main.go and requires the middleware to be installed
// before router.SetRouter, which is what makes it cover every route and both
// NoRoute fallbacks.
//
// Matching is per line, not per substring: a commented-out or renamed
// registration must fail this test, so an Index() over the whole file (which
// still finds the text inside a comment) would defeat the purpose.
func TestSecurityHeadersIsRegisteredInMainChain(t *testing.T) {
	source, err := os.ReadFile("../main.go")
	if err != nil {
		t.Fatalf("failed to read main.go: %v", err)
	}

	const registration = "server.Use(middleware.SecurityHeaders())"
	const setRouter = "router.SetRouter("
	regLine, routerLine := -1, -1
	for i, line := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == registration && regLine < 0 {
			regLine = i
		}
		if strings.HasPrefix(trimmed, setRouter) && routerLine < 0 {
			routerLine = i
		}
	}

	if regLine < 0 {
		t.Fatalf("main.go does not register %s as a live statement", registration)
	}
	if routerLine < 0 {
		t.Fatalf("main.go does not call %s", setRouter)
	}
	if regLine > routerLine {
		t.Fatalf("%s is registered at line %d, after %s at line %d; the headers would miss the SPA and relay NoRoute fallbacks",
			registration, regLine+1, setRouter, routerLine+1)
	}
}
