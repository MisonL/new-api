package cachex

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/samber/hot"
)

const (
	defaultRedisOpTimeout   = 2 * time.Second
	defaultRedisScanTimeout = 30 * time.Second
	defaultRedisDelTimeout  = 10 * time.Second
)

type HybridCacheConfig[V any] struct {
	Namespace Namespace

	// Redis is used when RedisEnabled returns true (or RedisEnabled is nil) and Redis is not nil.
	Redis        *redis.Client
	RedisCodec   ValueCodec[V]
	RedisEnabled func() bool

	// RedisFunc, when set, is resolved once per operation and takes precedence
	// over the static Redis client. A cache built once per process via
	// sync.Once must follow a replaced process-wide client: a client captured
	// at construction outlives a reconnect and every write then fails with
	// "redis: client is closed" while RedisEnabled still reports usable.
	RedisFunc func() *redis.Client

	// Memory builds a hot cache used when Redis is disabled. Keys stored in memory are fully namespaced.
	Memory func() *hot.HotCache[string, V]
}

// HybridCache is a small helper that uses Redis when enabled, otherwise falls back to in-memory hot cache.
type HybridCache[V any] struct {
	ns Namespace

	redis        *redis.Client
	redisFunc    func() *redis.Client
	redisCodec   ValueCodec[V]
	redisEnabled func() bool

	memOnce sync.Once
	memInit func() *hot.HotCache[string, V]
	mem     *hot.HotCache[string, V]
}

func NewHybridCache[V any](cfg HybridCacheConfig[V]) *HybridCache[V] {
	return &HybridCache[V]{
		ns:           cfg.Namespace,
		redis:        cfg.Redis,
		redisFunc:    cfg.RedisFunc,
		redisCodec:   cfg.RedisCodec,
		redisEnabled: cfg.RedisEnabled,
		memInit:      cfg.Memory,
	}
}

func (c *HybridCache[V]) FullKey(key string) string {
	return c.ns.FullKey(key)
}

// redisConfigured reports whether the cache is configured for Redis at all
// (codec present and the enable gate passes). It deliberately does not resolve
// the client: resolution belongs to the operation, so a dynamic RedisFunc is
// called exactly once per operation rather than once by the gate and again by
// the operation.
func (c *HybridCache[V]) redisConfigured() bool {
	hasClient := c.redisFunc != nil || c.redis != nil
	return hasClient && c.redisCodec != nil && (c.redisEnabled == nil || c.redisEnabled())
}

// resolveRedis returns the client for the current operation: RedisFunc when
// configured (resolved per call so the cache follows a replaced process-wide
// client), else the static client. The gate and the operation must resolve
// through this single point; resolving separately would let one approve a
// client while the other used a different one.
func (c *HybridCache[V]) resolveRedis() *redis.Client {
	if c.redisFunc != nil {
		return c.redisFunc()
	}
	return c.redis
}

func (c *HybridCache[V]) memCache() *hot.HotCache[string, V] {
	c.memOnce.Do(func() {
		if c.memInit == nil {
			c.mem = hot.NewHotCache[string, V](hot.LRU, 1).Build()
			return
		}
		c.mem = c.memInit()
	})
	return c.mem
}

func (c *HybridCache[V]) Get(key string) (value V, found bool, err error) {
	full := c.ns.FullKey(key)
	if full == "" {
		var zero V
		return zero, false, nil
	}

	if client := c.resolveRedis(); client != nil && c.redisConfigured() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultRedisOpTimeout)
		defer cancel()

		raw, e := client.Get(ctx, full).Result()
		if e == nil {
			v, decErr := c.redisCodec.Decode(raw)
			if decErr != nil {
				var zero V
				return zero, false, decErr
			}
			return v, true, nil
		}
		if errors.Is(e, redis.Nil) {
			var zero V
			return zero, false, nil
		}
		var zero V
		return zero, false, e
	}

	return c.memCache().Get(full)
}

func (c *HybridCache[V]) SetWithTTL(key string, v V, ttl time.Duration) error {
	full := c.ns.FullKey(key)
	if full == "" {
		return nil
	}

	if client := c.resolveRedis(); client != nil && c.redisConfigured() {
		raw, err := c.redisCodec.Encode(v)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultRedisOpTimeout)
		defer cancel()
		return client.Set(ctx, full, raw, ttl).Err()
	}

	c.memCache().SetWithTTL(full, v, ttl)
	return nil
}

// Keys returns keys with valid values. In Redis, it returns all matching keys.
func (c *HybridCache[V]) Keys() ([]string, error) {
	if client := c.resolveRedis(); client != nil && c.redisConfigured() {
		return c.scanKeys(c.ns.MatchPattern())
	}
	return c.memCache().Keys(), nil
}

func (c *HybridCache[V]) scanKeys(match string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultRedisScanTimeout)
	defer cancel()

	client := c.resolveRedis()
	var cursor uint64
	keys := make([]string, 0, 1024)
	for {
		k, next, err := client.Scan(ctx, cursor, match, 1000).Result()
		if err != nil {
			return keys, err
		}
		keys = append(keys, k...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return keys, nil
}

func (c *HybridCache[V]) Purge() error {
	if client := c.resolveRedis(); client != nil && c.redisConfigured() {
		keys, err := c.scanKeys(c.ns.MatchPattern())
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			return nil
		}
		_, err = c.DeleteMany(keys)
		return err
	}

	c.memCache().Purge()
	return nil
}

func (c *HybridCache[V]) DeleteByPrefix(prefix string) (int, error) {
	fullPrefix := c.ns.FullKey(prefix)
	if fullPrefix == "" {
		return 0, nil
	}
	if !strings.HasSuffix(fullPrefix, ":") {
		fullPrefix += ":"
	}

	if client := c.resolveRedis(); client != nil && c.redisConfigured() {
		match := fullPrefix + "*"
		keys, err := c.scanKeys(match)
		if err != nil {
			return 0, err
		}
		if len(keys) == 0 {
			return 0, nil
		}

		res, err := c.DeleteMany(keys)
		if err != nil {
			return 0, err
		}
		deleted := 0
		for _, ok := range res {
			if ok {
				deleted++
			}
		}
		return deleted, nil
	}

	// In memory, we filter keys and bulk delete.
	allKeys := c.memCache().Keys()
	keys := make([]string, 0, 128)
	for _, k := range allKeys {
		if strings.HasPrefix(k, fullPrefix) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return 0, nil
	}
	res, _ := c.DeleteMany(keys)
	deleted := 0
	for _, ok := range res {
		if ok {
			deleted++
		}
	}
	return deleted, nil
}

// DeleteMany accepts either fully namespaced keys or raw keys and deletes them.
// It returns a map keyed by fully namespaced keys.
func (c *HybridCache[V]) DeleteMany(keys []string) (map[string]bool, error) {
	res := make(map[string]bool, len(keys))
	if len(keys) == 0 {
		return res, nil
	}

	fullKeys := make([]string, 0, len(keys))
	for _, k := range keys {
		k = c.ns.FullKey(k)
		if k == "" {
			continue
		}
		fullKeys = append(fullKeys, k)
	}
	if len(fullKeys) == 0 {
		return res, nil
	}

	if client := c.resolveRedis(); client != nil && c.redisConfigured() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultRedisDelTimeout)
		defer cancel()

		pipe := client.Pipeline()
		cmds := make([]*redis.IntCmd, 0, len(fullKeys))
		for _, k := range fullKeys {
			// UNLINK is non-blocking vs DEL for large key batches.
			cmds = append(cmds, pipe.Unlink(ctx, k))
		}
		_, err := pipe.Exec(ctx)
		if err != nil && !errors.Is(err, redis.Nil) {
			return res, err
		}
		for i, cmd := range cmds {
			deleted := cmd != nil && cmd.Err() == nil && cmd.Val() > 0
			res[fullKeys[i]] = deleted
		}
		return res, nil
	}

	return c.memCache().DeleteMany(fullKeys), nil
}

func (c *HybridCache[V]) Capacity() (mainCacheCapacity int, missingCacheCapacity int) {
	if c.redisConfigured() {
		return 0, 0
	}
	return c.memCache().Capacity()
}

func (c *HybridCache[V]) Algorithm() (mainCacheAlgorithm string, missingCacheAlgorithm string) {
	if c.redisConfigured() {
		return "redis", ""
	}
	return c.memCache().Algorithm()
}
