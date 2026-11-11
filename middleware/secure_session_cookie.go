package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// SessionCookieOptions is the one definition of the session cookie's options.
// The store in main.go and the per-request override below both derive from it,
// so the two cannot drift: a change to Path, MaxAge, HttpOnly, or SameSite made
// in one place would otherwise silently stop applying on TLS requests.
func SessionCookieOptions(secure bool) sessions.Options {
	return sessions.Options{
		Path:     "/",
		MaxAge:   2592000, // 30 days
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	}
}

// SecureSessionCookie marks the session cookie Secure on requests that arrived
// over TLS, so a browser never returns the login session over a plaintext
// connection. It runs per request because the flag depends on the request's own
// scheme: a deployment that terminates TLS at a proxy sets X-Forwarded-Proto,
// and a plain-HTTP deployment (a LAN install, the desktop sidecar) must keep
// the cookie usable, so the flag is not hardcoded on.
//
// It must be registered after sessions.Sessions, which is what installs the
// session the Options apply to.
func SecureSessionCookie() gin.HandlerFunc {
	return func(c *gin.Context) {
		if RequestIsHTTPS(c) {
			session := sessions.Default(c)
			if session != nil {
				session.Options(SessionCookieOptions(true))
			}
		}
		c.Next()
	}
}

// RequestIsHTTPS reports whether the request reached the server over TLS,
// either directly or through a proxy that forwarded the scheme.
//
// It is exported because session-adjacent cookies outside this package (the
// desktop OAuth confirm credential) must use the same rule: those responses
// also serve deployments that terminate TLS at a proxy, and a second,
// narrower "Request.TLS != nil" check would silently drop Secure there.
func RequestIsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	// X-Forwarded-Proto may carry a comma-separated chain; the first entry is
	// the scheme the client used.
	forwarded := c.Request.Header.Get("X-Forwarded-Proto")
	if idx := strings.IndexByte(forwarded, ','); idx >= 0 {
		forwarded = forwarded[:idx]
	}
	return strings.EqualFold(strings.TrimSpace(forwarded), "https")
}
