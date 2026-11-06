package common

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func useRedisKeyLockForTest(t *testing.T) *miniredis.Miniredis {
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
	})
	return server
}

// 同一 key 的并发调用必须串行：注册的 check-then-insert 依赖这个互斥。
func TestWithKeyLockSerializesSameKeyLocally(t *testing.T) {
	originalEnabled := RedisEnabled
	originalRDB := RDB
	RedisEnabled = false
	RDB = nil
	t.Cleanup(func() {
		RedisEnabled = originalEnabled
		RDB = originalRDB
	})

	var current int32
	var maxConcurrent int32
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- WithKeyLock(context.Background(), "same-register-key", 2*time.Second, 10*time.Second, func() error {
				n := atomic.AddInt32(&current, 1)
				for {
					old := atomic.LoadInt32(&maxConcurrent)
					if n <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt32(&current, -1)
				return nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&maxConcurrent))
}

// Redis 可用时，另一个持有者未释放前，本进程的获取必须等待并超时。
func TestWithKeyLockWaitsForRedisHolder(t *testing.T) {
	useRedisKeyLockForTest(t)

	acquired := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		_ = WithKeyLock(context.Background(), "cross-instance-key", 2*time.Second, 10*time.Second, func() error {
			close(acquired)
			<-release
			return nil
		})
	}()
	<-acquired

	ran := false
	err := WithKeyLock(context.Background(), "cross-instance-key", 100*time.Millisecond, 10*time.Second, func() error {
		ran = true
		return nil
	})
	require.ErrorIs(t, err, ErrKeyLockTimeout)
	require.False(t, ran)

	close(release)
	<-holderDone
}

// Redis 故障时应退回进程内锁，不能把业务请求直接失败掉。
func TestWithKeyLockFallsBackWhenRedisUnavailable(t *testing.T) {
	originalEnabled := RedisEnabled
	originalRDB := RDB
	RedisEnabled = true
	RDB = redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
		MaxRetries:   -1,
	})
	t.Cleanup(func() {
		_ = RDB.Close()
		RedisEnabled = originalEnabled
		RDB = originalRDB
	})

	ran := false
	err := WithKeyLock(context.Background(), "fallback-key", time.Second, 10*time.Second, func() error {
		ran = true
		return nil
	})
	require.NoError(t, err)
	require.True(t, ran)
}
