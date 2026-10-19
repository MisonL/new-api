package common

import (
	"os"
	"testing"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func openHashProbeRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("NEW_API_REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("NEW_API_REDIS_TEST_ADDR not set")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("NEW_API_REDIS_TEST_PASSWORD"),
		DB:       15,
	})
	require.NoError(t, client.FlushDB(client.Context()).Err())
	t.Cleanup(func() { client.Close() })
	return client
}

// key 不存在时 HINCRBY / HSETFIELD 不能假装成功：调用方只在 err != nil 时记
// 日志，返回 nil 意味着缓存扣减/回写被静默丢弃，额度在缓存里长期偏高。
// 正确语义是重建这个字段（并带上默认 TTL），或者返回明确错误。
func TestRedisHIncrByRebuildsMissingKey(t *testing.T) {
	client := openHashProbeRedis(t)
	prev := RDB
	RDB = client
	t.Cleanup(func() { RDB = prev })

	key := "probe:hash:incrby"
	require.NoError(t, RedisHIncrBy(key, "Quota", -50))

	got, err := client.HGet(client.Context(), key, "Quota").Result()
	require.NoError(t, err, "the field must exist after HINCRBY on a missing key")
	require.Equal(t, "-50", got)

	// 重建出来的 key 必须有 TTL，否则永不过期。
	ttl, err := client.TTL(client.Context(), key).Result()
	require.NoError(t, err)
	require.Greater(t, ttl.Milliseconds(), int64(0), "a rebuilt key must carry a TTL")
}

func TestRedisHSetFieldRebuildsMissingKey(t *testing.T) {
	client := openHashProbeRedis(t)
	prev := RDB
	RDB = client
	t.Cleanup(func() { RDB = prev })

	key := "probe:hash:hset"
	require.NoError(t, RedisHSetField(key, "Status", 1))

	got, err := client.HGet(client.Context(), key, "Status").Result()
	require.NoError(t, err, "the field must exist after HSET on a missing key")
	require.Equal(t, "1", got)
}
