package common

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// ErrKeyLockTimeout 表示在等待时间内没有拿到 key 锁。调用方应当把它当作
// “另一个请求正在处理同一 key”处理（例如返回冲突），而不是内部错误。
var ErrKeyLockTimeout = errors.New("key lock acquire timeout")

const keyLockRedisPrefix = "key_lock:"

// keyLockLocalEntry 是进程内锁条目。refs 记录持有者与等待者数量，归零后
// 才从 map 删除，避免 unlock 时删掉别人刚创建的新锁。
type keyLockLocalEntry struct {
	mutex sync.Mutex
	refs  int
}

var keyLockLocal = struct {
	mu      sync.Mutex
	entries map[string]*keyLockLocalEntry
}{entries: make(map[string]*keyLockLocalEntry)}

func acquireLocalKeyLock(key string) *keyLockLocalEntry {
	keyLockLocal.mu.Lock()
	defer keyLockLocal.mu.Unlock()
	entry := keyLockLocal.entries[key]
	if entry == nil {
		entry = &keyLockLocalEntry{}
		keyLockLocal.entries[key] = entry
	}
	entry.refs++
	return entry
}

func releaseLocalKeyLock(key string, entry *keyLockLocalEntry) {
	keyLockLocal.mu.Lock()
	defer keyLockLocal.mu.Unlock()
	entry.refs--
	if entry.refs == 0 {
		delete(keyLockLocal.entries, key)
	}
}

func keyLockRedisKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return keyLockRedisPrefix + hex.EncodeToString(sum[:])
}

// releaseKeyLockScript 只有仍持有锁的请求才能删除，避免租约过期后误删
// 下一个持有者的锁。
var releaseKeyLockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

// WithKeyLock 在 key 上串行执行 fn，跨实例生效（Redis），Redis 不可用时退回
// 进程内互斥。ttl 是 Redis 租约上限，防止持有者崩溃后死锁；acquireTimeout
// 是等待上限，超时返回 ErrKeyLockTimeout。
func WithKeyLock(ctx context.Context, key string, acquireTimeout time.Duration, ttl time.Duration, fn func() error) error {
	if fn == nil {
		return nil
	}
	localEntry := acquireLocalKeyLock(key)
	defer releaseLocalKeyLock(key, localEntry)

	deadline := time.Now().Add(acquireTimeout)
	for !localEntry.mutex.TryLock() {
		if time.Now().After(deadline) {
			return ErrKeyLockTimeout
		}
		time.Sleep(2 * time.Millisecond)
	}
	defer localEntry.mutex.Unlock()

	client := keyLockRedisClient()
	if client == nil {
		return fn()
	}

	token := GetUUID()
	redisKey := keyLockRedisKey(key)
	for {
		acquired, err := client.SetNX(ctx, redisKey, token, ttl).Result()
		if err != nil {
			// Redis 故障不能阻断业务：退回进程内锁（已持有）继续执行。
			SysLog("key lock redis unavailable, fallback to local lock: " + err.Error())
			return fn()
		}
		if acquired {
			break
		}
		if time.Now().After(deadline) {
			return ErrKeyLockTimeout
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := releaseKeyLockScript.Run(releaseCtx, client, []string{redisKey}, token).Err(); err != nil {
			SysLog("failed to release key lock: " + err.Error())
		}
	}()
	return fn()
}

func keyLockRedisClient() *redis.Client {
	if !RedisEnabled || RDB == nil {
		return nil
	}
	return RDB
}

// WithKeyLocks 按字典序依次获取多个 key 锁，再执行 fn。固定顺序避免两个
// 请求以相反顺序持有不同锁造成死锁；重复 key 只获取一次。
func WithKeyLocks(ctx context.Context, keys []string, acquireTimeout time.Duration, ttl time.Duration, fn func() error) error {
	unique := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, key)
	}
	sort.Strings(unique)
	var run func(index int) error
	run = func(index int) error {
		if index == len(unique) {
			return fn()
		}
		return WithKeyLock(ctx, unique[index], acquireTimeout, ttl, func() error {
			return run(index + 1)
		})
	}
	return run(0)
}
