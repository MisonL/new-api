package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

type failFirstRedisEvalHook struct {
	failed atomic.Bool
}

func (hook *failFirstRedisEvalHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if cmd.Name() == "eval" && hook.failed.CompareAndSwap(false, true) {
		return ctx, errors.New("temporary redis release failure")
	}
	return ctx, nil
}

func (*failFirstRedisEvalHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *failFirstRedisEvalHook) BeforeProcessPipeline(ctx context.Context, cmds []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (*failFirstRedisEvalHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

type failAfterFirstRedisEvalHook struct {
	failed atomic.Bool
}

func (*failAfterFirstRedisEvalHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (hook *failAfterFirstRedisEvalHook) AfterProcess(_ context.Context, cmd redis.Cmder) error {
	if cmd.Name() == "eval" && hook.failed.CompareAndSwap(false, true) {
		return errors.New("temporary redis response failure after execution")
	}
	return nil
}

func (*failAfterFirstRedisEvalHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (*failAfterFirstRedisEvalHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

type failAllRedisEvalHook struct{}

func (*failAllRedisEvalHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if cmd.Name() == "eval" {
		return ctx, errors.New("redis coordinator unavailable")
	}
	return ctx, nil
}

func (*failAllRedisEvalHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (*failAllRedisEvalHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (*failAllRedisEvalHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestGetChannelConcurrencySnapshotMarksLocalFallbackUnconfirmed(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "false")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	lease, err := AcquireChannelConcurrency(nil, &model.Channel{Id: 90401, MaxConcurrency: 2})
	require.NoError(t, err)
	require.NotNil(t, lease)
	snapshot := GetChannelConcurrencySnapshot(90401, 2)
	require.False(t, snapshot.RedisAvailable)
	require.Equal(t, "local_fallback", snapshot.Source)
	require.False(t, snapshot.Confirmed)
	require.Equal(t, 1, snapshot.CurrentInFlight)
	lease.Release()
}

func TestGetChannelConcurrencySnapshotUsesRedisAsConfirmedSource(t *testing.T) {
	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "true")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		_ = client.Close()
		server.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	lease, err := AcquireChannelConcurrency(nil, &model.Channel{Id: 90402, MaxConcurrency: 2})
	require.NoError(t, err)
	require.NotNil(t, lease)
	snapshot := GetChannelConcurrencySnapshot(90402, 2)
	require.True(t, snapshot.RedisAvailable)
	require.Equal(t, "redis", snapshot.Source)
	require.True(t, snapshot.Confirmed)
	require.Equal(t, 1, snapshot.CurrentInFlight)
	lease.Release()
}

func TestChannelConcurrencyLeaseRenewLoopRetriesTransientRedisErrors(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "true")
	t.Setenv("CHANNEL_CONCURRENCY_LEASE_TTL_SECONDS", "5")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		_ = client.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	lease, err := AcquireChannelConcurrency(nil, &model.Channel{Id: 90403, MaxConcurrency: 1})
	require.NoError(t, err)
	require.NotNil(t, lease)
	t.Cleanup(lease.Release)

	initialExpiry, err := common.RDB.ZScore(context.Background(), channelConcurrencyKey(90403), lease.token).Result()
	require.NoError(t, err)
	failureBaseline := channelConcurrencyMetrics.renewFailure.Load()
	server.SetError("LOADING Redis is loading the dataset in memory")
	require.Eventually(t, func() bool {
		return channelConcurrencyMetrics.renewFailure.Load() > failureBaseline
	}, 4*time.Second, 20*time.Millisecond)

	server.SetError("")
	require.Eventually(t, func() bool {
		expiry, scoreErr := common.RDB.ZScore(context.Background(), channelConcurrencyKey(90403), lease.token).Result()
		return scoreErr == nil && expiry > initialExpiry
	}, 4*time.Second, 20*time.Millisecond)
}

func TestChannelConcurrencyRenewIntervalLeavesFailureMarginBeforeLeaseExpiry(t *testing.T) {
	ttl := 5 * time.Second
	interval := channelConcurrencyRenewInterval(ttl)

	require.Less(t, time.Duration(channelConcurrencyRenewFailureLimit)*interval, ttl)
}

func TestChannelConcurrencyLeaseReleaseRetriesTransientRedisError(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "true")
	t.Setenv("CHANNEL_CONCURRENCY_LEASE_TTL_SECONDS", "5")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		_ = client.Close()
		server.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	lease, err := AcquireChannelConcurrency(nil, &model.Channel{Id: 90407, MaxConcurrency: 1})
	require.NoError(t, err)
	require.NotNil(t, lease)

	hook := &failFirstRedisEvalHook{}
	client.AddHook(hook)
	lease.Release()

	require.True(t, hook.failed.Load())
	count, err := client.ZCard(context.Background(), channelConcurrencyKey(90407)).Result()
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestChannelConcurrencyAcquireErrorFallbackCleansRemoteLease(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "false")
	t.Setenv("CHANNEL_CONCURRENCY_LEASE_TTL_SECONDS", "5")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		_ = client.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	hook := &failAfterFirstRedisEvalHook{}
	client.AddHook(hook)
	lease, err := AcquireChannelConcurrency(nil, &model.Channel{Id: 90408, MaxConcurrency: 1})
	require.NoError(t, err)
	require.NotNil(t, lease)
	_, err = client.ZScore(context.Background(), channelConcurrencyKey(90408), lease.token).Result()
	require.NoError(t, err)

	lease.Release()
	require.Eventually(t, func() bool {
		count, _ := client.ZCard(context.Background(), channelConcurrencyKey(90408)).Result()
		return count == 0
	}, time.Second, 20*time.Millisecond)
}

func TestChannelConcurrencyStrictAcquireErrorCleansRemoteLease(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "true")
	t.Setenv("CHANNEL_CONCURRENCY_LEASE_TTL_SECONDS", "5")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		_ = client.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	hook := &failAllRedisEvalHook{}
	client.AddHook(hook)
	lease, err := AcquireChannelConcurrency(nil, &model.Channel{Id: 90409, MaxConcurrency: 1})
	require.ErrorIs(t, err, ErrChannelConcurrencyUnavailable)
	require.Nil(t, lease)
	count, err := client.ZCard(context.Background(), channelConcurrencyKey(90409)).Result()
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestChannelConcurrencyUncertainAcquireFailsClosedInNonStrictMode(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "false")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		_ = client.Close()
		server.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	// The hook makes both the reservation response and the compensating cleanup
	// fail. The reservation outcome is therefore unknown; local fallback must be
	// rejected even though strict mode is disabled.
	client.AddHook(&failAllRedisEvalHook{})
	lease, err := AcquireChannelConcurrency(nil, &model.Channel{Id: 90412, MaxConcurrency: 1})
	require.ErrorIs(t, err, ErrChannelConcurrencyUnavailable)
	require.Nil(t, lease)
}

func TestChannelConcurrencyStrictAcquireRetriesTransientEvalError(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "true")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		_ = client.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	hook := &failAfterFirstRedisEvalHook{}
	client.AddHook(hook)
	lease, err := AcquireChannelConcurrency(nil, &model.Channel{Id: 90410, MaxConcurrency: 1})
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.True(t, hook.failed.Load())
	lease.Release()
	require.Eventually(t, func() bool {
		count, _ := client.ZCard(context.Background(), channelConcurrencyKey(90410)).Result()
		return count == 0
	}, time.Second, 20*time.Millisecond)
}

func TestChannelConcurrencyRenewRejectsExpiredSortedSetScore(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "true")
	t.Setenv("CHANNEL_CONCURRENCY_LEASE_TTL_SECONDS", "5")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		_ = client.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	lease, err := AcquireChannelConcurrency(nil, &model.Channel{Id: 90404, MaxConcurrency: 1})
	require.NoError(t, err)
	require.NotNil(t, lease)
	t.Cleanup(lease.Release)

	require.NoError(t, common.RDB.ZAdd(context.Background(), channelConcurrencyKey(90404), &redis.Z{
		Score:  float64(time.Now().Add(-time.Second).UnixMilli()),
		Member: lease.token,
	}).Err())
	require.Equal(t, channelConcurrencyRenewLeaseMissing, lease.renewStatus())
	_, err = common.RDB.ZScore(context.Background(), channelConcurrencyKey(90404), lease.token).Result()
	require.ErrorIs(t, err, redis.Nil)
}

func TestChannelConcurrencyRenewFailureThresholdCancelsRequest(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "true")
	t.Setenv("CHANNEL_CONCURRENCY_LEASE_TTL_SECONDS", "5")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		_ = client.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	originalRequest := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request = originalRequest
	lease, err := AcquireChannelConcurrency(c, &model.Channel{Id: 90405, MaxConcurrency: 1})
	require.NoError(t, err)
	require.NotNil(t, lease)
	server.SetError("LOADING Redis is loading the dataset in memory")
	require.Eventually(t, func() bool {
		return lease.request != nil && lease.request.Context().Err() != nil
	}, 8*time.Second, 25*time.Millisecond)
	require.ErrorIs(t, lease.request.Context().Err(), context.Canceled)
	server.SetError("")
	lease.Release()
	require.Same(t, originalRequest, c.Request)
	require.Eventually(t, func() bool {
		count, _ := common.RDB.ZCard(context.Background(), channelConcurrencyKey(90405)).Result()
		return count == 0
	}, time.Second, 20*time.Millisecond)
}

func TestChannelConcurrencySequentialLeasesShareRootRequestContext(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "false")
	t.Setenv("CHANNEL_CONCURRENCY_LEASE_TTL_SECONDS", "5")
	t.Cleanup(func() {
		resetChannelConcurrencyStateForTest()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	originalRequest := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request = originalRequest
	channel := &model.Channel{Id: 90411, MaxConcurrency: 2}

	first, err := AcquireChannelConcurrency(c, channel)
	require.NoError(t, err)
	require.NotNil(t, first)
	SetChannelConcurrencyLease(c, first)
	second, err := AcquireChannelConcurrency(c, channel)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.NotSame(t, first.request, second.request)
	require.Same(t, originalRequest, first.rootRequest)
	require.Same(t, originalRequest, second.rootRequest)

	// Replacing the request-scoped lease releases the first reservation. Its
	// cancellation must not cascade into the second attempt's context.
	SetChannelConcurrencyLease(c, second)
	require.ErrorIs(t, first.request.Context().Err(), context.Canceled)
	require.NoError(t, second.request.Context().Err())
	require.Same(t, second.request, c.Request)

	second.Release()
	require.Same(t, originalRequest, c.Request)
}
