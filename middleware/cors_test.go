package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The fetch spec forbids pairing "Access-Control-Allow-Origin: *" with
// "Access-Control-Allow-Credentials: true", and gin-contrib/cors hardcodes the
// wildcard whenever AllowAllOrigins is set. Emitting both produced a preflight
// response browsers reject, while the wildcard alone is what clients need.
func TestCORSWildcardDoesNotAdvertiseCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORS())
	router.GET("/api/status", func(c *gin.Context) { c.Status(http.StatusOK) })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.Header.Set("Origin", "https://app.cross-origin.test")
	router.ServeHTTP(recorder, request)

	require.Equal(t, "*", recorder.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, recorder.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSPreflightStillAllowsConfiguredMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORS())
	router.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(http.StatusOK) })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	request.Header.Set("Origin", "https://app.cross-origin.test")
	request.Header.Set("Access-Control-Request-Method", "POST")
	router.ServeHTTP(recorder, request)

	require.Equal(t, "*", recorder.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, recorder.Header().Get("Access-Control-Allow-Methods"), "POST")
	require.Empty(t, recorder.Header().Get("Access-Control-Allow-Credentials"))
}
