package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func TestDesktopOAuthPollRateLimitUsesHandoffTokenKey(t *testing.T) {
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

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/oauth/desktop/poll?handoff_token=token-a", nil)
		request.RemoteAddr = "127.0.0.1:12345"
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected token-a request %d to pass, got %d", i+1, recorder.Code)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/oauth/desktop/poll?handoff_token=token-b", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected token-b to use an independent rate-limit bucket, got %d", recorder.Code)
	}
}

func TestDesktopOAuthPollRateLimitRejectsExcessRequests(t *testing.T) {
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

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/oauth/desktop/poll?handoff_token=token-limit", nil)
		request.RemoteAddr = "127.0.0.1:22345"
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected request %d to pass, got %d", i+1, recorder.Code)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/oauth/desktop/poll?handoff_token=token-limit", nil)
	request.RemoteAddr = "127.0.0.1:22345"
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected excess desktop oauth poll to be rate limited, got %d", recorder.Code)
	}
}
