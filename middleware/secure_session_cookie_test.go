package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// A non-nil Request.TLS is what gin and the handler read to mean "this request
// arrived over TLS"; the zero ConnectionState is enough for that check.
var tlsDummyState = tls.ConnectionState{}

// The session cookie carries the login session. Over HTTPS it must be marked
// Secure so a browser never sends it on a plaintext request. The flag is set
// per request from the request's own scheme, so a deployment that terminates
// TLS at a proxy (X-Forwarded-Proto) is covered without breaking a plain-HTTP
// deployment, which would otherwise never receive the cookie back.
func TestSecureSessionCookieMarksHTTPSRequestsSecure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name        string
		tls         bool
		protoHeader string
		wantSecure  bool
	}{
		{name: "plain http stays insecure", wantSecure: false},
		{name: "direct tls is secure", tls: true, wantSecure: true},
		{name: "forwarded https is secure", protoHeader: "https", wantSecure: true},
		{name: "forwarded http stays insecure", protoHeader: "http", wantSecure: false},
		{name: "forwarded https with port is secure", protoHeader: "https, http", wantSecure: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			store := cookie.NewStore([]byte("secure-cookie-test-secret"))
			store.Options(sessions.Options{Path: "/", HttpOnly: true, Secure: false})
			router.Use(sessions.Sessions("session", store))
			router.Use(SecureSessionCookie())
			router.GET("/set", func(c *gin.Context) {
				session := sessions.Default(c)
				session.Set("id", 1)
				if err := session.Save(); err != nil {
					c.String(http.StatusInternalServerError, "save: %v", err)
					return
				}
				c.String(http.StatusOK, "ok")
			})

			req := httptest.NewRequest(http.MethodGet, "/set", nil)
			if tc.tls {
				req.TLS = &tlsDummyState
			}
			if tc.protoHeader != "" {
				req.Header.Set("X-Forwarded-Proto", tc.protoHeader)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			cookies := rec.Result().Cookies()
			var sessionCookie *http.Cookie
			for _, ck := range cookies {
				if ck.Name == "session" {
					sessionCookie = ck
					break
				}
			}
			if sessionCookie == nil {
				t.Fatalf("no session cookie set; headers=%v", rec.Header())
			}
			if sessionCookie.Secure != tc.wantSecure {
				t.Fatalf("cookie Secure = %v, want %v", sessionCookie.Secure, tc.wantSecure)
			}
		})
	}
}
