package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

var timeFormat = "2006-01-02T15:04:05.000Z"

const (
	desktopOAuthHandoffTokenMinLength = 1
	desktopOAuthHandoffTokenMaxLength = 128
)

var webStaticRootPaths = map[string]struct{}{
	"/azure_model_name.png": {},
	"/cover-4.webp":         {},
	"/favicon.ico":          {},
	"/index.html":           {},
	"/logo.png":             {},
	"/pay-apple.png":        {},
	"/pay-card.png":         {},
	"/pay-google.png":       {},
	"/ratio.png":            {},
	"/robots.txt":           {},
}

var inMemoryRateLimiter = &common.InMemoryRateLimiter{}

var defNext = func(c *gin.Context) {
	c.Next()
}

// adminRateLimitExempt reports whether the current request may skip the
// non-critical rate limiters because it carries an authenticated administrator
// session. It fails closed: a missing or unreadable session, a disabled
// account, or any role below RoleAdminUser keeps the request rate limited.
//
// A router that never registered the session middleware is treated as a missing
// session rather than a panic: sessions.Default calls MustGet, which panics when
// the key is absent. main.go registers the middleware on the engine before any
// route, so this guard is unreachable there, but the limiters are exported and a
// narrower engine must fail closed rather than crash.
//
// Only GlobalWebRateLimit, GlobalAPIRateLimit, SearchRateLimit and
// SuggestionRateLimit consult this. CriticalRateLimit deliberately does not, so
// the login, registration, password-reset and payment endpoints keep their
// IP-level protection even for administrators.
//
// This runs as a global middleware, well before UserAuth, so it cannot rely on
// the user record having been re-read from the database. It therefore reads the
// role and status that setupLogin wrote into the session and refuses to exempt
// anything whose status is not UserStatusEnabled. A session that predates a
// disable or a demotion stays exempt until its next authenticated request
// refreshes it through syncSessionUserIdentity; that window is bounded by the
// same staleness UserAuth already tolerates for these fields.
func adminRateLimitExempt(c *gin.Context) bool {
	if !operation_setting.GetRateLimitAdminExempt() {
		return false
	}
	if _, registered := c.Get(sessions.DefaultKey); !registered {
		return false
	}
	session := sessions.Default(c)
	role, ok := session.Get("role").(int)
	if !ok {
		return false
	}
	status, ok := session.Get("status").(int)
	if !ok {
		return false
	}
	if status != common.UserStatusEnabled {
		return false
	}
	return role >= common.RoleAdminUser
}

// exemptableLimiter wraps a non-critical limiter so that a request carrying an
// authenticated administrator session skips it while the exemption option is on.
// It is the single place that decides the exemption applies to a limiter: only
// the limiters wrapped here honor it, and CriticalRateLimit is deliberately not
// one of them.
func exemptableLimiter(limiter gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if adminRateLimitExempt(c) {
			c.Next()
			return
		}
		limiter(c)
	}
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	redisRateLimiterWithKey(c, maxRequestNum, duration, "rateLimit:"+mark+c.ClientIP())
}

func redisRateLimiterWithKey(c *gin.Context, maxRequestNum int, duration int64, key string) {
	ctx := context.Background()
	rdb := common.RDB
	listLength, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		fmt.Println(err.Error())
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if listLength < int64(maxRequestNum) {
		rdb.LPush(ctx, key, time.Now().Format(timeFormat))
		rdb.Expire(ctx, key, common.RateLimitKeyExpirationDuration)
	} else {
		oldTimeStr, _ := rdb.LIndex(ctx, key, -1).Result()
		oldTime, err := time.Parse(timeFormat, oldTimeStr)
		if err != nil {
			fmt.Println(err)
			c.Status(http.StatusInternalServerError)
			c.Abort()
			return
		}
		nowTimeStr := time.Now().Format(timeFormat)
		nowTime, err := time.Parse(timeFormat, nowTimeStr)
		if err != nil {
			fmt.Println(err)
			c.Status(http.StatusInternalServerError)
			c.Abort()
			return
		}
		// time.Since will return negative number!
		// See: https://stackoverflow.com/questions/50970900/why-is-time-since-returning-negative-durations-on-windows
		if int64(nowTime.Sub(oldTime).Seconds()) < duration {
			rdb.Expire(ctx, key, common.RateLimitKeyExpirationDuration)
			abortRateLimited(c, int(duration))
			return
		} else {
			rdb.LPush(ctx, key, time.Now().Format(timeFormat))
			rdb.LTrim(ctx, key, 0, int64(maxRequestNum-1))
			rdb.Expire(ctx, key, common.RateLimitKeyExpirationDuration)
		}
	}
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := mark + c.ClientIP()
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		abortRateLimited(c, int(duration))
		return
	}
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	// 参数为 0 表示"不限制"，与模型限流对 totalMaxCount=0 的解释一致。
	// 不加这个出口时：Redis 路径在空列表上 LIndex(-1) 得到空串，time.Parse
	// 失败直接 500；内存路径第二个请求起恒为 429。运维把限额设为 0 期望关闭
	// 限流时，实际得到一个全站 500、一个全站 429。
	if maxRequestNum <= 0 {
		return defNext
	}
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	} else {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		return func(c *gin.Context) {
			memoryRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
}

func GlobalWebRateLimit() func(c *gin.Context) {
	if common.GlobalWebRateLimitEnable {
		limiter := rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
		exempt := exemptableLimiter(limiter)
		return func(c *gin.Context) {
			// The static-asset check stays outside the wrapper on purpose: it
			// short circuits before the session read that the exemption needs,
			// so a page load's asset requests never pay for it.
			if IsWebStaticResourcePath(c.Request.URL.Path) {
				c.Next()
				return
			}
			exempt(c)
		}
	}
	return defNext
}

func IsWebStaticResourcePath(resourcePath string) bool {
	if strings.HasPrefix(resourcePath, "/assets/") ||
		strings.HasPrefix(resourcePath, "/static/") ||
		strings.HasPrefix(resourcePath, "/.well-known/") {
		return true
	}
	_, ok := webStaticRootPaths[resourcePath]
	return ok
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	if common.GlobalApiRateLimitEnable {
		limiter := rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA")
		return exemptableLimiter(limiter)
	}
	return defNext
}

func CriticalRateLimit() func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	}
	return defNext
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func DesktopOAuthPollRateLimit() func(c *gin.Context) {
	if !common.DesktopOAuthPollRateLimitEnable {
		return defNext
	}
	limiter := requestKeyRateLimitFactory(
		common.DesktopOAuthPollRateLimitNum,
		common.DesktopOAuthPollRateLimitDuration,
		"DOP",
		func(c *gin.Context) string {
			return sanitizeDesktopOAuthHandoffToken(c.Query("handoff_token"))
		},
	)
	return func(c *gin.Context) {
		c.Set("desktop_oauth_poll_rate_limited", true)
		limiter(c)
	}
}

func sanitizeDesktopOAuthHandoffToken(token string) string {
	normalized := strings.TrimSpace(token)
	if normalized == "" {
		return ""
	}
	if len(normalized) < desktopOAuthHandoffTokenMinLength ||
		len(normalized) > desktopOAuthHandoffTokenMaxLength {
		return ""
	}
	for _, char := range normalized {
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= '0' && char <= '9' {
			continue
		}
		if char == '-' || char == '_' {
			continue
		}
		return ""
	}
	return normalized
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

func requestKeyRateLimitFactory(maxRequestNum int, duration int64, mark string, keyFunc func(c *gin.Context) string) func(c *gin.Context) {
	// 同 rateLimitFactory：0 表示不限制。
	if maxRequestNum <= 0 {
		return defNext
	}
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRequestKeyRateLimiter(c, maxRequestNum, duration, mark, keyFunc)
		}
	}
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		memoryRequestKeyRateLimiter(c, maxRequestNum, duration, mark, keyFunc)
	}
}

func memoryRequestKeyRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string, keyFunc func(c *gin.Context) string) {
	key := buildRateLimitKey(mark, c, keyFunc)
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		abortRateLimited(c, int(duration))
		return
	}
}

func redisRequestKeyRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string, keyFunc func(c *gin.Context) string) {
	redisRateLimiterWithKey(c, maxRequestNum, duration, "rateLimit:"+buildRateLimitKey(mark, c, keyFunc))
}

func buildRateLimitKey(mark string, c *gin.Context, keyFunc func(c *gin.Context) string) string {
	if keyFunc != nil {
		if key := keyFunc(c); key != "" {
			return mark + ":" + key
		}
	}
	return mark + ":" + c.ClientIP()
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			userId := c.GetInt("id")
			if userId == 0 {
				c.Status(http.StatusUnauthorized)
				c.Abort()
				return
			}
			key := fmt.Sprintf("rateLimit:%s:user:%d", mark, userId)
			redisRateLimiterWithKey(c, maxRequestNum, duration, key)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		key := fmt.Sprintf("%s:user:%d", mark, userId)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			abortRateLimited(c, int(duration))
			return
		}
	}
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() func(c *gin.Context) {
	if !common.SearchRateLimitEnable {
		return defNext
	}
	limiter := userRateLimitFactory(common.SearchRateLimitNum, common.SearchRateLimitDuration, "SR")
	return exemptableLimiter(limiter)
}

// SuggestionRateLimit returns a per-user rate limiter for autocomplete suggestion endpoints.
// Configurable via SUGGESTION_RATE_LIMIT_ENABLE / SUGGESTION_RATE_LIMIT / SUGGESTION_RATE_LIMIT_DURATION.
func SuggestionRateLimit() func(c *gin.Context) {
	if !common.SuggestionRateLimitEnable {
		return defNext
	}
	limiter := userRateLimitFactory(common.SuggestionRateLimitNum, common.SuggestionRateLimitDuration, "SG")
	return exemptableLimiter(limiter)
}
