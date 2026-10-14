package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func newOAuthStateTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("test-session-secret"))))
	router.GET("/api/oauth/state", GenerateOAuthCode)
	return router
}

// The affiliate code is copied from the query string into the session cookie.
// gorilla/securecookie caps a cookie at 4096 bytes, and past that cap
// session.Save() fails for the browser on every later request -- so an
// oversized ?aff= would not merely break this login, it would leave the visitor
// unable to start any OAuth login at all.
func TestGenerateOAuthCodeRejectsOverlongAffiliateCode(t *testing.T) {
	router := newOAuthStateTestRouter()

	for _, length := range []int{4, 64, 65, 4000, 8000} {
		req := httptest.NewRequest(
			http.MethodGet,
			"/api/oauth/state?aff="+strings.Repeat("a", length),
			nil,
		)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if !strings.Contains(recorder.Body.String(), `"success":true`) {
			t.Fatalf("aff length %d broke the state endpoint: %s", length, recorder.Body.String())
		}
		if len(recorder.Header().Values("Set-Cookie")) == 0 {
			t.Fatalf("aff length %d produced no session cookie", length)
		}
		if strings.Contains(recorder.Body.String(), "securecookie") {
			t.Fatalf("aff length %d leaked a securecookie error: %s", length, recorder.Body.String())
		}
	}
}

// A code that fits is still stored, so attribution keeps working.
func TestGenerateOAuthCodeStoresNormalAffiliateCode(t *testing.T) {
	router := newOAuthStateTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/oauth/state?aff=abcd", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	cookies := recorder.Header().Values("Set-Cookie")
	if len(cookies) == 0 {
		t.Fatal("expected a session cookie")
	}
	if !strings.Contains(recorder.Body.String(), `"success":true`) {
		t.Fatalf("expected success, got %s", recorder.Body.String())
	}
}
