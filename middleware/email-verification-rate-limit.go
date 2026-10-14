package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

const (
	EmailVerificationRateLimitMark = "EV"
	EmailVerificationMaxRequests   = 2  // 30秒内最多2次
	EmailVerificationDuration      = 30 // 30秒时间窗口
)

func redisEmailVerificationRateLimiter(c *gin.Context) {
	ctx := context.Background()
	rdb := common.RDB
	key := "emailVerification:" + EmailVerificationRateLimitMark + ":" + c.ClientIP()

	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		// fallback
		memoryEmailVerificationRateLimiter(c)
		return
	}

	// 第一次设置键时设置过期时间
	if count == 1 {
		_ = rdb.Expire(ctx, key, time.Duration(EmailVerificationDuration)*time.Second).Err()
	}

	// 检查是否超出限制
	if count <= int64(EmailVerificationMaxRequests) {
		c.Next()
		return
	}

	// 获取剩余等待时间
	ttl, err := rdb.TTL(ctx, key).Result()
	waitSeconds := int64(EmailVerificationDuration)
	if err == nil && ttl > 0 {
		waitSeconds = int64(ttl.Seconds())
	}

	c.Header("Retry-After", strconv.FormatInt(waitSeconds, 10))
	c.JSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"message": fmt.Sprintf("发送过于频繁，请等待 %d 秒后再试", waitSeconds),
	})
	c.Abort()
}

func memoryEmailVerificationRateLimiter(c *gin.Context) {
	key := EmailVerificationRateLimitMark + ":" + c.ClientIP()

	if !inMemoryRateLimiter.Request(key, EmailVerificationMaxRequests, EmailVerificationDuration) {
		// The in-memory limiter tracks a fixed window of the configured
		// duration, so the full window is the honest hint.
		c.Header("Retry-After", strconv.Itoa(EmailVerificationDuration))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": "发送过于频繁，请稍后再试",
		})
		c.Abort()
		return
	}

	c.Next()
}

func EmailVerificationRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.RedisEnabled {
			redisEmailVerificationRateLimiter(c)
		} else {
			inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
			memoryEmailVerificationRateLimiter(c)
		}
	}
}
