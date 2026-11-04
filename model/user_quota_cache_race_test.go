package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// 额度缓存同时有两个写者：GetUserQuota 回源后用读库快照整值写回
// （updateUserQuotaCache），扣减/退款用 HINCRBY 增量。若整值写回无条件
// 覆盖，并发下会把已扣掉的额度在缓存里复活。修复后：整值只在字段缺失时
// 建立基数（HSETNX），已有值一律保留；增量只在字段存在时执行，避免在
// 空哈希上造出只有增量的假余额。
func setupUserQuotaCacheRaceTest(t *testing.T) {
	t.Helper()
	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRDB, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		common.RDB, common.RedisEnabled = previousRDB, previousEnabled
	})
}

func readQuotaField(t *testing.T, key string) string {
	t.Helper()
	value, err := common.RDB.HGet(context.Background(), key, "Quota").Result()
	require.NoError(t, err)
	return value
}

func TestUserQuotaCacheWritebackDoesNotClobberDecrement(t *testing.T) {
	setupUserQuotaCacheRaceTest(t)

	key := "user:9901"
	require.NoError(t, common.RedisHSetField(key, "Quota", "100"))

	// 扣 30 后再来一次旧快照回写：缓存必须保持 70，而不是被写回 100。
	require.NoError(t, cacheDecrUserQuota(9901, 30))
	require.NoError(t, updateUserQuotaCache(9901, 100))
	require.Equal(t, "70", readQuotaField(t, key), "整值回写不能覆盖并发扣减")
}

func TestUserQuotaCacheEstablishesBaseWhenAbsent(t *testing.T) {
	setupUserQuotaCacheRaceTest(t)

	key := "user:9902"
	// 缓存缺失时回源写回应建立基数。
	require.NoError(t, updateUserQuotaCache(9902, 500))
	require.Equal(t, "500", readQuotaField(t, key))

	// 字段缺失时增量应被跳过，避免 HINCRBY 造出只有增量的假余额。
	require.NoError(t, common.RDB.Del(context.Background(), key).Err())
	require.NoError(t, cacheDecrUserQuota(9902, 100))
	exists, err := common.RDB.Exists(context.Background(), key).Result()
	require.NoError(t, err)
	require.Zero(t, exists, "字段缺失时增量不能凭空创建余额")
}

