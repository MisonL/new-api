package controller

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// uptime 状态接口是匿名路由，每次请求都会按分组配置发起多次出站抓取。
// 没有缓存时，匿名用户可以把网关变成一个持续敲上游状态页的放大器。
func TestUptimeStatusCacheServesRepeatedReadsFromOneFetch(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	var fetches int32
	cache := &uptimeStatusCache{ttl: time.Minute, now: func() time.Time { return now }}

	fetch := func() []UptimeGroupResult {
		atomic.AddInt32(&fetches, 1)
		return []UptimeGroupResult{{CategoryName: "g", Monitors: []Monitor{{Name: "m", Status: 1}}}}
	}

	first := cache.Get("k", fetch)
	second := cache.Get("k", fetch)

	require.Equal(t, int32(1), atomic.LoadInt32(&fetches), "TTL 内的重复读取不得再次出站")
	require.Equal(t, first, second)
}

func TestUptimeStatusCacheRefetchesAfterTTL(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	var fetches int32
	cache := &uptimeStatusCache{ttl: time.Minute, now: func() time.Time { return now }}

	fetch := func() []UptimeGroupResult {
		atomic.AddInt32(&fetches, 1)
		return nil
	}

	cache.Get("k", fetch)
	now = now.Add(time.Minute + time.Second)
	cache.Get("k", fetch)

	require.Equal(t, int32(2), atomic.LoadInt32(&fetches), "TTL 过后必须重新出站")
}

// 分组配置变化时 key 变化，旧缓存不得被复用。
func TestUptimeStatusCacheRefetchesWhenKeyChanges(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	var fetches int32
	cache := &uptimeStatusCache{ttl: time.Minute, now: func() time.Time { return now }}

	fetch := func() []UptimeGroupResult {
		atomic.AddInt32(&fetches, 1)
		return nil
	}

	cache.Get("k1", fetch)
	cache.Get("k2", fetch)

	require.Equal(t, int32(2), atomic.LoadInt32(&fetches), "配置变化后必须重新出站")
}

// 并发请求必须共用同一次抓取，而不是各自出站。
func TestUptimeStatusCacheCoalescesConcurrentFetches(t *testing.T) {
	t.Parallel()

	var fetches int32
	cache := &uptimeStatusCache{ttl: time.Minute, now: time.Now}

	release := make(chan struct{})
	fetch := func() []UptimeGroupResult {
		atomic.AddInt32(&fetches, 1)
		<-release
		return nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Get("k", fetch)
		}()
	}

	// 等所有调用者都进入 or 等待同一次抓取，再放行。
	for atomic.LoadInt32(&fetches) == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	require.Equal(t, int32(1), atomic.LoadInt32(&fetches), "并发请求必须共用同一次抓取")
}
