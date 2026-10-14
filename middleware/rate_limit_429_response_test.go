package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A bare c.Status(429) told the client nothing about when to retry. Every local
// limiter now emits Retry-After plus a JSON body so SDK retry logic and
// operators both have something to act on.
func TestAbortRateLimitedEmitsRetryAfterAndBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	abortRateLimited(ctx, 180)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "180", recorder.Header().Get("Retry-After"))
	require.JSONEq(t, `{"error":{"message":"rate limit exceeded, please retry later","type":"rate_limit_error","code":""}}`,
		recorder.Body.String())
	require.True(t, ctx.IsAborted())
}

// A limiter with no configured window must still answer with a body; the
// Retry-After header is simply omitted rather than emitted as zero.
func TestAbortRateLimitedOmitsRetryAfterWithoutWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	abortRateLimited(ctx, 0)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Empty(t, recorder.Header().Get("Retry-After"))
	require.NotEmpty(t, recorder.Body.String())
}

func TestGlobalWebRateLimitEmitsRetryAfter(t *testing.T) {
	originalRedis := common.RedisEnabled
	originalEnable := common.GlobalWebRateLimitEnable
	originalNum := common.GlobalWebRateLimitNum
	originalDuration := common.GlobalWebRateLimitDuration
	common.RedisEnabled = false
	common.GlobalWebRateLimitEnable = true
	common.GlobalWebRateLimitNum = 1
	common.GlobalWebRateLimitDuration = 90
	t.Cleanup(func() {
		common.RedisEnabled = originalRedis
		common.GlobalWebRateLimitEnable = originalEnable
		common.GlobalWebRateLimitNum = originalNum
		common.GlobalWebRateLimitDuration = originalDuration
	})

	gin.SetMode(gin.TestMode)
	previousLimiter := inMemoryRateLimiter
	inMemoryRateLimiter = &common.InMemoryRateLimiter{}
	t.Cleanup(func() { inMemoryRateLimiter = previousLimiter })
	router := gin.New()
	router.Use(GlobalWebRateLimit())
	router.GET("/api/status", func(c *gin.Context) { c.Status(http.StatusOK) })

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	require.Equal(t, http.StatusOK, first.Code)

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	require.Equal(t, http.StatusTooManyRequests, second.Code)
	require.Equal(t, "90", second.Header().Get("Retry-After"))
	require.Contains(t, second.Body.String(), "rate limit exceeded")
}
