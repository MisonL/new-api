package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// userRateLimitFactory 没有 rateLimitFactory 的 maxRequestNum <= 0 出口：
// SEARCH_RATE_LIMIT=0 / SUGGESTION_RATE_LIMIT=0 时 Redis 路径会在空列表上
// LIndex(-1) 拿到空串、time.Parse 失败直接 500，内存路径第二个请求起恒 429。
// 两条路径都必须把 0 解释为“不限制”。
func TestZeroUserRateLimitDisablesLimiting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalEnable := common.SearchRateLimitEnable
	originalNum := common.SearchRateLimitNum
	originalDuration := common.SearchRateLimitDuration
	originalRedisEnabled := common.RedisEnabled
	originalRDB := common.RDB
	t.Cleanup(func() {
		common.SearchRateLimitEnable = originalEnable
		common.SearchRateLimitNum = originalNum
		common.SearchRateLimitDuration = originalDuration
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRDB
	})

	common.SearchRateLimitEnable = true
	common.SearchRateLimitNum = 0
	common.SearchRateLimitDuration = 60

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 77001)
	})
	router.GET("/search", SearchRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	assertUnlimited := func(mode string) {
		for i := 0; i < 5; i++ {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/search", nil)
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusOK, recorder.Code,
				"zero limit must disable limiting (%s, request %d), got %d", mode, i+1, recorder.Code)
		}
	}

	common.RedisEnabled = false
	common.RDB = nil
	assertUnlimited("memory")

	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})
	common.RedisEnabled = true
	common.RDB = client
	assertUnlimited("redis")
}
