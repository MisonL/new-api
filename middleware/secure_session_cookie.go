package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

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
		if requestIsHTTPS(c) {
			session := sessions.Default(c)
			if session != nil {
				// The interface exposes only a setter, so the whole option set
				// is restated. Keep it in step with the store defaults in
				// main.go: Path/MaxAge/HttpOnly/SameSite, plus Secure here.
				session.Options(sessions.Options{
					Path:     "/",
					MaxAge:   2592000, // 30 days
					HttpOnly: true,
					Secure:   true,
					SameSite: http.SameSiteStrictMode,
				})
			}
		}
		c.Next()
	}
}

// requestIsHTTPS reports whether the request reached the server over TLS,
// either directly or through a proxy that forwarded the scheme.
func requestIsHTTPS(c *gin.Context) bool {
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
