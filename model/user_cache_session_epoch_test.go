package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func useUserCacheRedisForTest(t *testing.T) *redis.Client {
	t.Helper()

	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		common.RedisEnabled = previousEnabled
		common.RDB = previousRDB
	})
	return client
}

func readCachedSessionEpoch(t *testing.T, client *redis.Client, userId int) string {
	t.Helper()
	value, err := client.HGet(context.Background(), getUserCacheKey(userId), "SessionEpoch").Result()
	require.NoError(t, err)
	return value
}

// 会话世代必须只增不减：调用方可能持有一个比并发递增更早的快照，
// 整值回写会把已递增的世代调低，使旧 Cookie 重新满足 stored == current。
func TestUpdateUserCacheNeverLowersSessionEpoch(t *testing.T) {
	client := useUserCacheRedisForTest(t)

	require.NoError(t, updateUserCache(User{
		Id: 901, Username: "epoch-user", Role: common.RoleCommonUser, SessionEpoch: 3,
	}))
	require.Equal(t, "3", readCachedSessionEpoch(t, client, 901))

	// 旧快照不得覆盖已递增的世代。
	require.NoError(t, updateUserCache(User{
		Id: 901, Username: "epoch-user", Role: common.RoleCommonUser, SessionEpoch: 1,
	}))
	require.Equal(t, "3", readCachedSessionEpoch(t, client, 901),
		"更旧的快照不得把会话世代调低")

	// 相等值保持原值。
	require.NoError(t, updateUserCache(User{
		Id: 901, Username: "epoch-user", Role: common.RoleCommonUser, SessionEpoch: 3,
	}))
	require.Equal(t, "3", readCachedSessionEpoch(t, client, 901))

	// 递增后的世代必须生效。
	require.NoError(t, updateUserCache(User{
		Id: 901, Username: "epoch-user", Role: common.RoleCommonUser, SessionEpoch: 7,
	}))
	require.Equal(t, "7", readCachedSessionEpoch(t, client, 901),
		"递增后的会话世代必须写入缓存")
}

// 首次建缓存时字段不存在，必须按「缺失即写入」建立基数，否则读端拿到 0，
// 会让世代非 0 的用户被误判为已吊销。
func TestUpdateUserCacheSeedsMissingSessionEpoch(t *testing.T) {
	client := useUserCacheRedisForTest(t)

	require.NoError(t, updateUserCache(User{
		Id: 902, Username: "epoch-seed", Role: common.RoleCommonUser, SessionEpoch: 5,
	}))
	require.Equal(t, "5", readCachedSessionEpoch(t, client, 902))
}

// 读端必须能读回写进去的世代：缓存里世代与数据库一致时，会话不应被判为吊销。
func TestGetUserCacheReturnsSessionEpoch(t *testing.T) {
	client := useUserCacheRedisForTest(t)

	require.NoError(t, updateUserCache(User{
		Id: 903, Username: "epoch-read", Role: common.RoleCommonUser, SessionEpoch: 9,
	}))

	cached, err := GetUserCache(903)
	require.NoError(t, err)
	require.Equal(t, int64(9), cached.SessionEpoch)
	require.Equal(t, "9", readCachedSessionEpoch(t, client, 903))
}
