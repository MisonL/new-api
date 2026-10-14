package cachex

import (
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/samber/hot"
	"github.com/stretchr/testify/require"
)

// RedisEnabled already decides per call whether Redis is in play, so a caller
// that swaps the process-wide client must be followed rather than pinned to the
// client that happened to exist when the cache was built. A cache built once via
// sync.Once used to keep writing to the original connection after it was
// replaced, which failed every write with "redis: client is closed" while
// RedisEnabled still reported Redis as usable.
func TestHybridCacheRedisFuncFollowsAReplacedClient(t *testing.T) {
	server1, err := miniredis.Run()
	require.NoError(t, err)
	client1 := redis.NewClient(&redis.Options{Addr: server1.Addr()})

	current := client1
	cache := NewHybridCache[string](HybridCacheConfig[string]{
		Namespace:  Namespace("test:redis-func:v1"),
		RedisFunc:  func() *redis.Client { return current },
		RedisCodec: StringCodec{},
		Memory: func() *hot.HotCache[string, string] {
			return hot.NewHotCache[string, string](hot.LRU, 8).WithTTL(time.Minute).Build()
		},
	})

	require.NoError(t, cache.SetWithTTL("k1", "v1", time.Minute))
	value, found, err := cache.Get("k1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v1", value)

	// Replace the client exactly as a reconnect would: the old one is closed and
	// a new one points at a different server.
	require.NoError(t, client1.Close())
	server1.Close()

	server2, err := miniredis.Run()
	require.NoError(t, err)
	defer server2.Close()
	current = redis.NewClient(&redis.Options{Addr: server2.Addr()})
	defer func() { _ = current.Close() }()

	// The cache is the same instance; only the client behind RedisFunc changed.
	require.NoError(t, cache.SetWithTTL("k2", "v2", time.Minute),
		"a write after the client was replaced must reach the new client")
	value, found, err = cache.Get("k2")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v2", value)

	// The old server's value must not be served from the replaced connection.
	_, found, err = cache.Get("k1")
	require.NoError(t, err)
	require.False(t, found, "the value lived on the replaced client's server")
}

// The gate and the operation must resolve the client once between them. If each
// resolved it separately, a RedisFunc that returns a different client on each
// call would let redisOn() approve one client while the operation used another,
// which is the mismatch this type exists to prevent.
func TestHybridCacheResolvesTheClientOncePerOperation(t *testing.T) {
	server, err := miniredis.Run()
	require.NoError(t, err)
	defer server.Close()
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()

	resolutions := 0
	cache := NewHybridCache[string](HybridCacheConfig[string]{
		Namespace: Namespace("test:single-resolution:v1"),
		RedisFunc: func() *redis.Client {
			resolutions++
			return client
		},
		RedisCodec: StringCodec{},
		Memory: func() *hot.HotCache[string, string] {
			return hot.NewHotCache[string, string](hot.LRU, 8).WithTTL(time.Minute).Build()
		},
	})

	resolutions = 0
	require.NoError(t, cache.SetWithTTL("k", "v", time.Minute))
	require.Equal(t, 1, resolutions, "SetWithTTL must resolve the client exactly once")

	resolutions = 0
	_, _, err = cache.Get("k")
	require.NoError(t, err)
	require.Equal(t, 1, resolutions, "Get must resolve the client exactly once")
}
