package common

import (
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// 验证码原先只存在进程内 map：多实例部署时，签发实例与校验实例不同就会
// 报“验证码错误/过期”。Redis 开启时必须以 Redis 为准，验证码才能跨实例。
func useRedisVerificationStoreForTest(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalEnabled := RedisEnabled
	originalRDB := RDB
	RedisEnabled = true
	RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		RedisEnabled = originalEnabled
		RDB = originalRDB
		clearVerificationMemoryForTest()
	})
	return server
}

func clearVerificationMemoryForTest() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}

func TestVerificationCodeIsReadableAfterLocalMemoryCleared(t *testing.T) {
	useRedisVerificationStoreForTest(t)

	const email = "multi-instance@example.com"
	RegisterVerificationCodeWithKey(email, "123456", EmailVerificationPurpose)

	// 模拟另一个实例：本进程内存里没有这份验证码，只能从 Redis 读到。
	clearVerificationMemoryForTest()

	require.True(t, VerifyCodeWithKey(email, "123456", EmailVerificationPurpose))
}

func TestVerificationCodeExpiresInRedis(t *testing.T) {
	server := useRedisVerificationStoreForTest(t)

	const email = "expire@example.com"
	RegisterVerificationCodeWithKey(email, "654321", EmailVerificationPurpose)
	require.True(t, VerifyCodeWithKey(email, "654321", EmailVerificationPurpose))

	server.FastForward(time.Duration(VerificationValidMinutes)*time.Minute + time.Second)
	require.False(t, VerifyCodeWithKey(email, "654321", EmailVerificationPurpose))
}

func TestDeleteKeyRemovesRedisVerificationCode(t *testing.T) {
	useRedisVerificationStoreForTest(t)

	const email = "delete@example.com"
	RegisterVerificationCodeWithKey(email, "abcdef", PasswordResetPurpose)
	DeleteKey(email, PasswordResetPurpose)
	require.False(t, VerifyCodeWithKey(email, "abcdef", PasswordResetPurpose))
}

// Redis 未启用时保持原有单机行为。
func TestVerificationCodeFallsBackToMemoryWithoutRedis(t *testing.T) {
	originalEnabled := RedisEnabled
	originalRDB := RDB
	RedisEnabled = false
	RDB = nil
	t.Cleanup(func() {
		RedisEnabled = originalEnabled
		RDB = originalRDB
		clearVerificationMemoryForTest()
	})

	const email = "memory@example.com"
	RegisterVerificationCodeWithKey(email, "999999", EmailVerificationPurpose)
	require.True(t, VerifyCodeWithKey(email, "999999", EmailVerificationPurpose))
	require.False(t, VerifyCodeWithKey(email, "000000", EmailVerificationPurpose))
}
