package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func setupTaskPollingClaimRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	previousEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled = previousEnabled
		common.RDB = previousRDB
	})
	return server
}

func taskPollingClaimTestTask(id int64) *model.Task {
	return &model.Task{ID: id, ChannelId: 201, TaskID: "task-claim-test"}
}

func TestAcquireTaskPollingClaimExcludesAndReleasesByToken(t *testing.T) {
	setupTaskPollingClaimRedis(t)
	first, err := AcquireTaskPollingClaim(context.Background(), taskPollingClaimTestTask(701))
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := AcquireTaskPollingClaim(context.Background(), taskPollingClaimTestTask(701))
	require.Nil(t, second)
	require.ErrorIs(t, err, ErrTaskPollingClaimBusy)

	first.Release()
	first.Release()
	second, err = AcquireTaskPollingClaim(context.Background(), taskPollingClaimTestTask(701))
	require.NoError(t, err)
	require.NotNil(t, second)
	second.Release()
}

func TestAcquireTaskPollingClaimExpiredOwnerCannotReleaseNewOwner(t *testing.T) {
	server := setupTaskPollingClaimRedis(t)
	t.Setenv("TASK_POLLING_CLAIM_LEASE_TTL_SECONDS", "5")
	first, err := AcquireTaskPollingClaim(context.Background(), taskPollingClaimTestTask(702))
	require.NoError(t, err)
	server.FastForward(6 * time.Second)

	second, err := AcquireTaskPollingClaim(context.Background(), taskPollingClaimTestTask(702))
	require.NoError(t, err)
	require.NotNil(t, second)
	first.Release()

	third, err := AcquireTaskPollingClaim(context.Background(), taskPollingClaimTestTask(702))
	require.Nil(t, third)
	require.ErrorIs(t, err, ErrTaskPollingClaimBusy)
	second.Release()
}

func TestAcquireTaskPollingClaimUnavailablePolicyIsExplicit(t *testing.T) {
	previousEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousRDB
	})
	t.Setenv("TASK_POLLING_CLAIM_STRICT", "true")

	lease, err := AcquireTaskPollingClaim(context.Background(), taskPollingClaimTestTask(703))
	require.Nil(t, lease)
	require.ErrorIs(t, err, ErrTaskPollingClaimUnavailable)
}

func TestAcquireTaskPollingClaimAllowsExplicitSingleInstanceFallback(t *testing.T) {
	previousEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousRDB
	})
	t.Setenv("TASK_POLLING_CLAIM_STRICT", "false")

	first, err := AcquireTaskPollingClaim(context.Background(), taskPollingClaimTestTask(704))
	require.NoError(t, err)
	require.NotNil(t, first)
	second, err := AcquireTaskPollingClaim(context.Background(), taskPollingClaimTestTask(704))
	require.Nil(t, second)
	require.True(t, errors.Is(err, ErrTaskPollingClaimBusy))
	first.Release()
}

func TestLocalTaskPollingClaimRejectsStaleOwnerOperations(t *testing.T) {
	localTaskPollingClaims.mu.Lock()
	localTaskPollingClaims.leases = make(map[string]localTaskPollingClaimLease)
	localTaskPollingClaims.mu.Unlock()
	t.Cleanup(func() {
		localTaskPollingClaims.mu.Lock()
		localTaskPollingClaims.leases = make(map[string]localTaskPollingClaimLease)
		localTaskPollingClaims.mu.Unlock()
	})

	const key = "local-claim-test"
	const firstToken = "first-token"
	const secondToken = "second-token"
	require.True(t, acquireLocalTaskPollingClaim(key, firstToken, time.Hour))

	localTaskPollingClaims.mu.Lock()
	localTaskPollingClaims.leases[key] = localTaskPollingClaimLease{
		token:     firstToken,
		expiresAt: time.Now().Add(-time.Second),
	}
	localTaskPollingClaims.mu.Unlock()

	require.True(t, acquireLocalTaskPollingClaim(key, secondToken, time.Hour))
	require.False(t, renewLocalTaskPollingClaim(key, firstToken, time.Hour))
	require.True(t, renewLocalTaskPollingClaim(key, secondToken, time.Hour))
	releaseLocalTaskPollingClaim(key, firstToken)

	localTaskPollingClaims.mu.Lock()
	lease, exists := localTaskPollingClaims.leases[key]
	localTaskPollingClaims.mu.Unlock()
	require.True(t, exists)
	require.Equal(t, secondToken, lease.token)

	releaseLocalTaskPollingClaim(key, secondToken)
	localTaskPollingClaims.mu.Lock()
	_, exists = localTaskPollingClaims.leases[key]
	localTaskPollingClaims.mu.Unlock()
	require.False(t, exists)
}
