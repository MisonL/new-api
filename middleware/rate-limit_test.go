package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

var desktopOAuthRateLimitTestMu sync.Mutex

// inMemoryRateLimiter.store 是进程级单例且 Init 不清空，同一测试二进制内
// -count>1 或多个测试共用 store；token 必须每次执行唯一，否则上一轮的
// 计数残留会让本测试以错误原因失败。
var desktopOAuthTokenSeq atomic.Int64

func uniqueDesktopOAuthToken(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, desktopOAuthTokenSeq.Add(1))
}

func TestDesktopOAuthPollRateLimitUsesHandoffTokenKey(t *testing.T) {
	desktopOAuthRateLimitTestMu.Lock()
	defer desktopOAuthRateLimitTestMu.Unlock()

	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	originalEnable := common.DesktopOAuthPollRateLimitEnable
	originalNum := common.DesktopOAuthPollRateLimitNum
	originalDuration := common.DesktopOAuthPollRateLimitDuration
	common.RedisEnabled = false
	common.DesktopOAuthPollRateLimitEnable = true
	common.DesktopOAuthPollRateLimitNum = 2
	common.DesktopOAuthPollRateLimitDuration = 60
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.DesktopOAuthPollRateLimitEnable = originalEnable
		common.DesktopOAuthPollRateLimitNum = originalNum
		common.DesktopOAuthPollRateLimitDuration = originalDuration
	})

	router := gin.New()
	router.GET("/api/oauth/desktop/poll", DesktopOAuthPollRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	tokenA := uniqueDesktopOAuthToken("token-a")
	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/oauth/desktop/poll?handoff_token="+tokenA, nil)
		request.RemoteAddr = "127.0.0.1:12345"
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected token-a request %d to pass, got %d", i+1, recorder.Code)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/oauth/desktop/poll?handoff_token="+uniqueDesktopOAuthToken("token-b"), nil)
	request.RemoteAddr = "127.0.0.1:12345"
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected token-b to use an independent rate-limit bucket, got %d", recorder.Code)
	}
}

func TestDesktopOAuthPollRateLimitRejectsExcessRequests(t *testing.T) {
	desktopOAuthRateLimitTestMu.Lock()
	defer desktopOAuthRateLimitTestMu.Unlock()

	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	originalEnable := common.DesktopOAuthPollRateLimitEnable
	originalNum := common.DesktopOAuthPollRateLimitNum
	originalDuration := common.DesktopOAuthPollRateLimitDuration
	common.RedisEnabled = false
	common.DesktopOAuthPollRateLimitEnable = true
	common.DesktopOAuthPollRateLimitNum = 2
	common.DesktopOAuthPollRateLimitDuration = 60
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.DesktopOAuthPollRateLimitEnable = originalEnable
		common.DesktopOAuthPollRateLimitNum = originalNum
		common.DesktopOAuthPollRateLimitDuration = originalDuration
	})

	router := gin.New()
	router.GET("/api/oauth/desktop/poll", DesktopOAuthPollRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	tokenLimit := uniqueDesktopOAuthToken("token-limit")
	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/oauth/desktop/poll?handoff_token="+tokenLimit, nil)
		request.RemoteAddr = "127.0.0.1:22345"
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected request %d to pass, got %d", i+1, recorder.Code)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/oauth/desktop/poll?handoff_token="+tokenLimit, nil)
	request.RemoteAddr = "127.0.0.1:22345"
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected excess desktop oauth poll to be rate limited, got %d", recorder.Code)
	}
}
