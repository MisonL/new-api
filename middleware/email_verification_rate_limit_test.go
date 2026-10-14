package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// newEmailVerificationRequest builds a request with a fixed client IP so the
// limiter's per-IP key is stable across calls.
func newEmailVerificationRequest(t *testing.T, clientIP string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/verification?email=a@b.c", nil)
	ctx.Request.RemoteAddr = clientIP + ":12345"
	return ctx, recorder
}

// The Redis path is the one that runs whenever Redis is configured, which is the
// production configuration. It answered 429 without a Retry-After header, so a
// client could only learn the wait by parsing the localized message.
func TestEmailVerificationRedisRateLimitSetsRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	// The limiter's own budget is two requests per window; the third must be
	// rejected. Requests are run through the whole middleware so the test
	// exercises the Redis branch selected by EmailVerificationRateLimit.
	for i := 0; i < EmailVerificationMaxRequests; i++ {
		ctx, recorder := newEmailVerificationRequest(t, "198.51.100.11")
		EmailVerificationRateLimit()(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
	}

	ctx, recorder := newEmailVerificationRequest(t, "198.51.100.11")
	EmailVerificationRateLimit()(ctx)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, strconv.Itoa(EmailVerificationDuration), recorder.Header().Get("Retry-After"))

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.NotEmpty(t, payload.Message)
}

// The in-memory path is the fallback when Redis is absent or its INCR fails.
func TestEmailVerificationMemoryRateLimitSetsRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousRedisEnabled := common.RedisEnabled
	// inMemoryRateLimiter is a package-level singleton and Request() records one
	// entry per call, so a fresh limiter is swapped in rather than initialised in
	// place: the shared instance would carry this test's own three requests into
	// the next run of it (and into any other test using the same key), which made
	// the test fail under -shuffle and -count>1. Same pattern as
	// rate_limit_429_response_test.go.
	previousLimiter := inMemoryRateLimiter
	inMemoryRateLimiter = &common.InMemoryRateLimiter{}
	common.RedisEnabled = false
	t.Cleanup(func() {
		inMemoryRateLimiter = previousLimiter
		common.RedisEnabled = previousRedisEnabled
	})

	for i := 0; i < EmailVerificationMaxRequests; i++ {
		ctx, recorder := newEmailVerificationRequest(t, "198.51.100.22")
		EmailVerificationRateLimit()(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
	}

	ctx, recorder := newEmailVerificationRequest(t, "198.51.100.22")
	EmailVerificationRateLimit()(ctx)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, strconv.Itoa(EmailVerificationDuration), recorder.Header().Get("Retry-After"))
}
