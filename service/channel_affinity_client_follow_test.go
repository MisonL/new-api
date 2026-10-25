package service

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// channelAffinityCache 由 sync.Once 建一次，创建时把当时的 common.RDB 抓进
// Redis 字段。测试逐个替换 RDB（每个用例一个 miniredis）后，缓存仍指向第一个
// 已经关闭的客户端，于是同一进程里第二次运行的用例写不进缓存：
// -count=2 下 TestGetChannelInitialAffinityMultiKeySkipsCooledCredential
// 第二次必然 require.True(t, found) 失败。
//
// 这里只验证缓存能跟随被替换的 RDB，不依赖 miniredis 之外的东西。
func TestChannelAffinityCacheFollowsReplacedRedisClient(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	setting := operation_setting.GetChannelAffinitySetting()
	previousEnabled := setting.Enabled
	previousRules := append([]operation_setting.ChannelAffinityRule(nil), setting.Rules...)
	t.Cleanup(func() {
		setting.Enabled = previousEnabled
		setting.Rules = previousRules
	})

	setting.Enabled = true
	setting.Rules = []operation_setting.ChannelAffinityRule{{
		Name:            "affinity-follows-client",
		ModelRegex:      []string{"^affinity-follows-.*$"},
		IncludeRuleName: true,
	}}

	resetChannelAffinityCacheForTest()
	t.Cleanup(resetChannelAffinityCacheForTest)

	firstServer := startAffinityTestRedis(t)
	setAffinityTestRedis(t, firstServer)
	firstClient := common.RDB

	// 第一个客户端可用时能写入并读回。
	writeAffinityTestEntry(t, "affinity-follows-1")
	requireAffinityTestEntry(t, "affinity-follows-1", true)

	// 模拟下一个用例：换一个全新的 Redis，旧客户端关闭。
	secondServer := startAffinityTestRedis(t)
	setAffinityTestRedis(t, secondServer)
	require.NotSame(t, firstClient, common.RDB)

	// 旧条目不该出现（新实例是干净的），新写入必须落到新客户端。
	requireAffinityTestEntry(t, "affinity-follows-1", false)
	writeAffinityTestEntry(t, "affinity-follows-2")
	requireAffinityTestEntry(t, "affinity-follows-2", true)
}

func resetChannelAffinityCacheForTest() {
	channelAffinityCacheOnce = sync.Once{}
	channelAffinityCache = nil
}

func writeAffinityTestEntry(t *testing.T, key string) {
	t.Helper()
	cache := getChannelAffinityCache()
	require.NotNil(t, cache)
	require.NoError(t, cache.SetWithTTL(key, 206, time.Minute))
}

func requireAffinityTestEntry(t *testing.T, key string, want bool) {
	t.Helper()
	cache := getChannelAffinityCache()
	require.NotNil(t, cache)
	_, found, err := cache.Get(key)
	require.NoError(t, err, "affinity cache lookup must not error for %q", key)
	require.Equal(t, want, found, "affinity cache entry %q presence mismatch", key)
}

func startAffinityTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	return server
}

func setAffinityTestRedis(t *testing.T, server *miniredis.Miniredis) {
	t.Helper()
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RedisEnabled = true
}
