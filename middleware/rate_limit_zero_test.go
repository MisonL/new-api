package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// 限流参数为 0 的语义必须一致：模型限流已把 0 解释为"不限制"。通用限流
// 的两条路径此前不一致——Redis 路径在空列表上 LIndex(-1) 得到空串，
// time.Parse 失败直接 500；内存路径第二个请求起恒为 429。运维把
// GLOBAL_API_RATE_LIMIT 等设为 0 期望关闭限流时，一个全站 500、一个全站 429。
func TestZeroRateLimitDisablesLimiting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalEnable := common.GlobalApiRateLimitEnable
	originalNum := common.GlobalApiRateLimitNum
	originalDuration := common.GlobalApiRateLimitDuration
	originalRedisEnabled := common.RedisEnabled
	originalRDB := common.RDB
	t.Cleanup(func() {
		common.GlobalApiRateLimitEnable = originalEnable
		common.GlobalApiRateLimitNum = originalNum
		common.GlobalApiRateLimitDuration = originalDuration
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRDB
	})
	// 限流中间件在构造时捕获参数，必须先设置再构造路由。
	common.GlobalApiRateLimitEnable = true
	common.GlobalApiRateLimitNum = 0
	common.GlobalApiRateLimitDuration = 60

	router := gin.New()
	router.GET("/limited", GlobalAPIRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 内存限流的 store 是进程级单例，旧键在 60s 内会滑窗放行、把缺陷掩盖掉；
	// 每次用唯一客户端 IP 保证拿到全新的桶。
	var ipSeq atomic.Int64
	assertUnlimited := func(mode string) {
		ip := fmt.Sprintf("10.%d.%d.%d", byte(time.Now().UnixNano()), byte(ipSeq.Add(1)), byte(ipSeq.Load()*7))
		for i := 0; i < 5; i++ {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/limited", nil)
			request.RemoteAddr = ip + ":42424"
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
