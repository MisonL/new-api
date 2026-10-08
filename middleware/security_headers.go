package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders sets a small, non-breaking set of security response headers
// on every response, including the SPA fallback and the relay 404 that run
// through NoRoute.
//
// Only headers that cannot change how a correctly-typed response is handled are
// set here. Two candidates are deliberately left out and belong at the reverse
// proxy instead:
//
//   - X-Frame-Options / Content-Security-Policy: whether the console may be
//     framed depends on the deployment, and a CSP has to be reconciled with the
//     inline scripts both frontends emit. Guessing here would break embedding
//     rather than protect it.
//   - Strict-Transport-Security: only meaningful at the TLS terminator, which a
//     process behind a proxy cannot identify. A browser ignores the header when
//     it arrives over plain HTTP, so emitting it from here would be decoration.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Stop a browser from re-interpreting a response as a type other than the
		// one declared, which is how an uploaded file or a mis-typed asset would
		// otherwise be executed as script on this origin.
		c.Header("X-Content-Type-Options", "nosniff")
		// Console URLs carry query parameters worth keeping out of other origins'
		// logs and Referer headers.
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}
