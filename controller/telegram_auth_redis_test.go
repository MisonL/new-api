package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// 多实例部署下进程内的 telegramUsedAuth 各存一份，同一签名可以在另一实例上
// 重放。配置 Redis 时 SET NX 必须跨实例拦截重放；Redis 不可用时按桌面端
// OAuth handoff 同一约定显式失败，不静默降级成各实例各放行一次。
func setupTelegramRedisForTest(t *testing.T) {
	t.Helper()
	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})
}

func TestTelegramAuthOnceBlocksReplayAcrossInstances(t *testing.T) {
	setupTelegramRedisForTest(t)
	// 模拟另一实例已消费同一签名：直接占位。
	hash := "cross-instance-signature"
	require.NoError(t, common.RDB.SetNX(
		common.RDB.Context(),
		telegramUsedAuthRedisPrefix+hash,
		time.Now().Unix(),
		telegramMaxAuthAge,
	).Err())

	require.False(t, telegramAuthOnce(hash), "另一实例已消费的签名在本实例必须被拒")
}

func TestTelegramAuthOnceFailsClosedWhenRedisUnavailable(t *testing.T) {
	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	server.Close() // Redis 在线启用但已宕机

	require.False(t, telegramAuthOnce("unavailable-redis-signature"),
		"Redis 不可用必须显式失败，不能退回进程内集合放行")
}

func TestCheckTelegramAuthorizationEndToEndWithRedis(t *testing.T) {
	setupTelegramRedisForTest(t)
	token := "probe-bot-token"
	params := telegramParamsWithAge(nextTelegramID(), time.Now().Unix(), token)

	require.True(t, checkTelegramAuthorization(params, token), "首次使用必须放行")
	require.False(t, checkTelegramAuthorization(params, token), "重放必须被拒（跨实例路径）")
}

// 反向守卫：Redis 未启用时进程内兜底仍要拦截重放。
func TestCheckTelegramAuthorizationInProcessFallbackStillBlocks(t *testing.T) {
	// 前面用例可能把 Redis 置为启用；这里显式回到进程内兜底基座。
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})
	token := "probe-bot-token"
	params := telegramParamsWithAge(nextTelegramID(), time.Now().Unix(), token)

	require.True(t, checkTelegramAuthorization(params, token))
	require.False(t, checkTelegramAuthorization(params, token))
}
