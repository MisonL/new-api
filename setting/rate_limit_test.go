package setting

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// The group rate-limit map is replaced during a config load and read on every
// rate-limited request. The write must hold the write lock: guarding it with
// RLock let a config save run concurrently with a reader, which is a concurrent
// map write -- a fatal runtime error, not a recoverable panic. Run with -race.
func TestUpdateModelRequestRateLimitGroupRacesWithReaders(t *testing.T) {
	original := ModelRequestRateLimitGroup
	t.Cleanup(func() {
		ModelRequestRateLimitMutex.Lock()
		ModelRequestRateLimitGroup = original
		ModelRequestRateLimitMutex.Unlock()
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					GetGroupRateLimit("default")
				}
			}
		}()
	}

	for i := 0; i < 200; i++ {
		payload, err := json.Marshal(map[string][2]int{
			"default": {i, i + 1},
			"vip":     {i * 2, i*2 + 1},
		})
		require.NoError(t, err)
		require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(string(payload)))
	}

	close(stop)
	wg.Wait()

	total, success, found := GetGroupRateLimit("vip")
	require.True(t, found)
	require.Equal(t, 398, total)
	require.Equal(t, 399, success)
}

// A malformed value must not clear the limits that are already in effect.
func TestUpdateModelRequestRateLimitGroupKeepsPreviousOnInvalidJSON(t *testing.T) {
	original := ModelRequestRateLimitGroup
	t.Cleanup(func() {
		ModelRequestRateLimitMutex.Lock()
		ModelRequestRateLimitGroup = original
		ModelRequestRateLimitMutex.Unlock()
	})

	require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(`{"default":[5,6]}`))

	err := UpdateModelRequestRateLimitGroupByJSONString(`{"default":`)
	require.Error(t, err)

	total, success, found := GetGroupRateLimit("default")
	require.True(t, found, "an invalid value must not clear the existing limits")
	require.Equal(t, 5, total)
	require.Equal(t, 6, success)
}

// A valid replacement must be visible to readers immediately and completely.
func TestUpdateModelRequestRateLimitGroupReplacesAtomically(t *testing.T) {
	original := ModelRequestRateLimitGroup
	t.Cleanup(func() {
		ModelRequestRateLimitMutex.Lock()
		ModelRequestRateLimitGroup = original
		ModelRequestRateLimitMutex.Unlock()
	})

	require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(`{"a":[1,2],"b":[3,4]}`))
	require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(`{"c":[7,8]}`))

	_, _, foundA := GetGroupRateLimit("a")
	_, _, foundB := GetGroupRateLimit("b")
	_, _, foundC := GetGroupRateLimit("c")
	require.False(t, foundA, "the replaced map must not retain the old entries")
	require.False(t, foundB)
	require.True(t, foundC)
}

func TestCheckModelRequestRateLimitGroupRejectsNegativeLimits(t *testing.T) {
	for _, payload := range []string{
		`{"default":[-1,10]}`,
		`{"default":[1,0]}`,
		fmt.Sprintf(`{"default":[%d,1]}`, int64(1)<<40),
	} {
		require.Error(t, CheckModelRequestRateLimitGroup(payload), payload)
	}
	require.NoError(t, CheckModelRequestRateLimitGroup(`{"default":[10,20]}`))
}
