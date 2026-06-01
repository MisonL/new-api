package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func TestSuggestionRateLimitUsesDedicatedSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalRedisEnabled := common.RedisEnabled
	originalEnable := common.SuggestionRateLimitEnable
	originalNum := common.SuggestionRateLimitNum
	originalDuration := common.SuggestionRateLimitDuration
	originalLimiter := inMemoryRateLimiter

	common.RedisEnabled = false
	common.SuggestionRateLimitEnable = true
	common.SuggestionRateLimitNum = 1
	common.SuggestionRateLimitDuration = 60
	inMemoryRateLimiter = common.InMemoryRateLimiter{}

	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.SuggestionRateLimitEnable = originalEnable
		common.SuggestionRateLimitNum = originalNum
		common.SuggestionRateLimitDuration = originalDuration
		inMemoryRateLimiter = originalLimiter
	})

	router := gin.New()
	router.GET(
		"/suggestions",
		func(c *gin.Context) {
			c.Set("id", 1)
			c.Next()
		},
		SuggestionRateLimit(),
		func(c *gin.Context) {
			c.Status(http.StatusOK)
		},
	)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/suggestions", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("expected first suggestion request to pass, got %d", first.Code)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/suggestions", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second suggestion request to be rate limited, got %d", second.Code)
	}
}

func TestGlobalWebRateLimitSkipsStaticAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalRedisEnabled := common.RedisEnabled
	originalEnable := common.GlobalWebRateLimitEnable
	originalNum := common.GlobalWebRateLimitNum
	originalDuration := common.GlobalWebRateLimitDuration
	originalLimiter := inMemoryRateLimiter

	common.RedisEnabled = false
	common.GlobalWebRateLimitEnable = true
	common.GlobalWebRateLimitNum = 1
	common.GlobalWebRateLimitDuration = 60
	inMemoryRateLimiter = common.InMemoryRateLimiter{}

	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.GlobalWebRateLimitEnable = originalEnable
		common.GlobalWebRateLimitNum = originalNum
		common.GlobalWebRateLimitDuration = originalDuration
		inMemoryRateLimiter = originalLimiter
	})

	router := gin.New()
	router.Use(GlobalWebRateLimit())
	router.GET("/assets/chunk.js", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/static/js/chunk.js", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/static/css/index.css", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/static/font/public-sans.woff2", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/index.html", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/logo.png", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/robots.txt", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/.well-known/appspecific/com.chrome.devtools.json", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/console/midjourney", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/unknown.js", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	newRequest := func(path string, remoteAddr string) *http.Request {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.RemoteAddr = remoteAddr
		return request
	}

	staticPaths := []string{
		"/assets/chunk.js",
		"/static/js/chunk.js",
		"/static/css/index.css",
		"/static/font/public-sans.woff2",
		"/index.html",
		"/logo.png",
		"/robots.txt",
		"/.well-known/appspecific/com.chrome.devtools.json",
	}
	for _, path := range staticPaths {
		for i := 0; i < 2; i++ {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(
				recorder,
				httptest.NewRequest(http.MethodGet, path, nil),
			)
			if recorder.Code != http.StatusOK {
				t.Fatalf("expected static request %s #%d to pass, got %d", path, i+1, recorder.Code)
			}
		}
	}

	firstUnknown := httptest.NewRecorder()
	router.ServeHTTP(firstUnknown, newRequest("/unknown.js", "203.0.113.10:10001"))
	if firstUnknown.Code != http.StatusOK {
		t.Fatalf("expected first unknown static-looking path to pass, got %d", firstUnknown.Code)
	}

	secondUnknown := httptest.NewRecorder()
	router.ServeHTTP(secondUnknown, newRequest("/unknown.js", "203.0.113.10:10002"))
	if secondUnknown.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second unknown static-looking path to stay rate limited, got %d", secondUnknown.Code)
	}

	firstPage := httptest.NewRecorder()
	router.ServeHTTP(
		firstPage,
		newRequest("/console/midjourney", "203.0.113.11:10001"),
	)
	if firstPage.Code != http.StatusOK {
		t.Fatalf("expected first page request to pass, got %d", firstPage.Code)
	}

	secondPage := httptest.NewRecorder()
	router.ServeHTTP(
		secondPage,
		newRequest("/console/midjourney", "203.0.113.11:10002"),
	)
	if secondPage.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second page request to be rate limited, got %d", secondPage.Code)
	}
}
