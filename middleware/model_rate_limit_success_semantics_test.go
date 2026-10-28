package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 内存版模型限流用 successKey+"_check" 的临时键做预检，该键会记录所有请求，
// 于是"仅成功请求"的配额实际把失败请求也计入，与 Redis 版语义不一致：连续
// 失败达到限额后，成功的请求也会被误 429。
func TestMemoryModelRateLimitCountsOnlySuccesses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalEnabled := setting.ModelRequestRateLimitEnabled
	originalDuration := setting.ModelRequestRateLimitDurationMinutes
	originalCount := setting.ModelRequestRateLimitCount
	originalSuccess := setting.ModelRequestRateLimitSuccessCount
	originalRedis := common.RedisEnabled
	originalRDB := common.RDB
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = originalEnabled
		setting.ModelRequestRateLimitDurationMinutes = originalDuration
		setting.ModelRequestRateLimitCount = originalCount
		setting.ModelRequestRateLimitSuccessCount = originalSuccess
		common.RedisEnabled = originalRedis
		common.RDB = originalRDB
	})
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitCount = 100
	setting.ModelRequestRateLimitSuccessCount = 2
	common.RedisEnabled = false
	common.RDB = nil

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 987654321)
		c.Next()
	})
	router.Use(ModelRequestRateLimit())
	router.POST("/fail", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	router.POST("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	call := func(path string) int {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.RemoteAddr = "10.4.4.4:1234"
		router.ServeHTTP(recorder, request)
		return recorder.Code
	}

	// 连续失败不消耗"成功请求"配额。
	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusInternalServerError, call("/fail"), "failed request %d", i+1)
	}

	// 两次成功仍在配额内。
	require.Equal(t, http.StatusOK, call("/ok"), "first successful request must pass")
	require.Equal(t, http.StatusOK, call("/ok"), "second successful request must pass")

	// 第三次成功应被成功配额挡住。
	require.Equal(t, http.StatusTooManyRequests, call("/ok"), "third successful request must be limited")
}

// 阈值 0 在本仓库统一解释为"不限制"（见 GlobalAPIRateLimit 的修复）。内存版
// 模型限流此前用 Request 预检，阈值 0 时第一次调用建键、第二次起恒返回 false，
// 于是"关闭成功次数限制"实际变成"第二个请求起全部 429"。
func TestMemoryModelRateLimitZeroSuccessCountIsUnlimited(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalEnabled := setting.ModelRequestRateLimitEnabled
	originalDuration := setting.ModelRequestRateLimitDurationMinutes
	originalCount := setting.ModelRequestRateLimitCount
	originalSuccess := setting.ModelRequestRateLimitSuccessCount
	originalRedis := common.RedisEnabled
	originalRDB := common.RDB
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = originalEnabled
		setting.ModelRequestRateLimitDurationMinutes = originalDuration
		setting.ModelRequestRateLimitCount = originalCount
		setting.ModelRequestRateLimitSuccessCount = originalSuccess
		common.RedisEnabled = originalRedis
		common.RDB = originalRDB
	})
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitCount = 0
	setting.ModelRequestRateLimitSuccessCount = 0
	common.RedisEnabled = false
	common.RDB = nil

	router := gin.New()
	router.Use(func(c *gin.Context) {
		// 内存限流器的 store 是进程级单例，用独立用户避免与其他用例串味。
		c.Set("id", 987654322)
		c.Next()
	})
	router.Use(ModelRequestRateLimit())
	router.POST("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	for i := 0; i < 5; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/ok", nil)
		request.RemoteAddr = "10.4.4.5:1234"
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code,
			"zero success count must disable the success limit, got %d on request %d", recorder.Code, i+1)
	}
}
