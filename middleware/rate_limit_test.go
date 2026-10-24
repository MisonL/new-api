package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
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
	inMemoryRateLimiter = &common.InMemoryRateLimiter{}

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
	inMemoryRateLimiter = &common.InMemoryRateLimiter{}

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


// 配置了独立限流的分组此前只用 group 的阈值覆盖，计数 key 仍是 userId：
// 小限流分组会被该用户的全局用量顶满，本应宽松的分组也会被小分组拖累。
// 修复要求有独立限流的分组按 "userId:group" 单独计数，无独立限流的分组
// 仍共用该用户的全局计数。
func TestRateLimitSubjectSeparatesGroupWithOwnLimit(t *testing.T) {
	previousGroup := setting.ModelRequestRateLimitGroup
	setting.ModelRequestRateLimitGroup = map[string][2]int{
		"strict": {2, 2},
		"loose":  {500, 500},
	}
	t.Cleanup(func() { setting.ModelRequestRateLimitGroup = previousGroup })

	for _, tc := range []struct {
		name  string
		group string
		want  string
	}{
		{name: "group with own limit", group: "strict", want: "42:strict"},
		{name: "group without own limit", group: "default", want: "42"},
		{name: "empty group", group: "", want: "42"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("id", 42)
			common.SetContextKey(c, constant.ContextKeyUserGroup, tc.group)

			if got := rateLimitSubject(c); got != tc.want {
				t.Fatalf("rateLimitSubject = %q, want %q", got, tc.want)
			}
		})
	}
}

// 端到端：用户先在宽松分组（限额 3）耗尽 3 次配额，再到限额 2 的严格分组。
// 修复前两个分组共用 userId 计数，严格分组的第一个请求就被宽松分组的用量
// 顶成 429；修复后各有独立预算。
func TestGroupRateLimitBudgetIsNotSharedAcrossGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	originalEnabled := setting.ModelRequestRateLimitEnabled
	originalDuration := setting.ModelRequestRateLimitDurationMinutes
	originalGroup := setting.ModelRequestRateLimitGroup
	originalLimiter := inMemoryRateLimiter
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		setting.ModelRequestRateLimitEnabled = originalEnabled
		setting.ModelRequestRateLimitDurationMinutes = originalDuration
		setting.ModelRequestRateLimitGroup = originalGroup
		inMemoryRateLimiter = originalLimiter
	})

	common.RedisEnabled = false
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitGroup = map[string][2]int{
		"loose":  {3, 3},
		"strict": {2, 2},
	}
	inMemoryRateLimiter = &common.InMemoryRateLimiter{}

	router := gin.New()
	// router.ServeHTTP 会新建 Context，用户身份与分组由前置中间件从请求头注入
	router.Use(func(c *gin.Context) {
		c.Set("id", 42)
		common.SetContextKey(c, constant.ContextKeyUserGroup, c.GetHeader("X-Test-Group"))
		c.Next()
	})
	router.Use(ModelRequestRateLimit())
	router.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(http.StatusOK) })

	call := func(group string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("X-Test-Group", group)
		router.ServeHTTP(w, req)
		return w.Code
	}

	for i := 0; i < 3; i++ {
		if code := call("loose"); code != http.StatusOK {
			t.Fatalf("loose request %d = %d, want 200", i+1, code)
		}
	}
	// 宽松分组的 3 次用量不得挤占严格分组的独立预算（限额 2）。
	if code := call("strict"); code != http.StatusOK {
		t.Fatalf("strict first request = %d, want 200", code)
	}
	if code := call("strict"); code != http.StatusOK {
		t.Fatalf("strict second request = %d, want 200", code)
	}
	if code := call("strict"); code != http.StatusTooManyRequests {
		t.Fatalf("strict third request = %d, want 429", code)
	}
}
