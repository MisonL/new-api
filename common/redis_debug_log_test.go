package common

import (
	"bytes"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// DEBUG=true 是排障开关，但缓存写入的调试日志此前直接打印完整对象与字段值：
// UserBase.Setting 内含 webhook_secret/gotify_token，原样落盘等于把用户推送
// 凭据写进 oneapi-*.log。日志应保留 key/字段名等诊断信息，凭据值必须脱敏。
func TestRedisDebugLogDoesNotLeakCachedSecrets(t *testing.T) {
	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	originalRDB := RDB
	originalRedisEnabled := RedisEnabled
	originalDebug := DebugEnabled
	RDB = client
	RedisEnabled = true
	DebugEnabled = true
	t.Cleanup(func() {
		RDB = originalRDB
		RedisEnabled = originalRedisEnabled
		DebugEnabled = originalDebug
	})

	logBuffer := &bytes.Buffer{}
	LogWriterMu.Lock()
	oldWriter := gin.DefaultWriter
	gin.DefaultWriter = logBuffer
	LogWriterMu.Unlock()
	t.Cleanup(func() {
		LogWriterMu.Lock()
		gin.DefaultWriter = oldWriter
		LogWriterMu.Unlock()
	})

	const webhookSecret = "whsec-leaked-webhook-value"
	const gotifyToken = "gotify-leaked-token-value"
	setting := `{"webhook_secret":"` + webhookSecret + `","gotify_token":"` + gotifyToken + `"}`

	type cacheEntry struct {
		Id      int
		Setting string
	}
	require.NoError(t, RedisHSetObj("user:debug-log-test", &cacheEntry{Id: 7, Setting: setting}, time.Minute))
	require.NoError(t, RedisHSetField("user:debug-log-test", "Setting", setting))

	logged := logBuffer.String()
	require.Contains(t, logged, "user:debug-log-test", "the debug log must keep the cache key for diagnosis")
	require.NotContains(t, logged, webhookSecret, "debug log leaked webhook_secret: %s", logged)
	require.NotContains(t, logged, gotifyToken, "debug log leaked gotify_token: %s", logged)
}
