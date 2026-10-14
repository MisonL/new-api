package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// useSubscriptionRedisForTest installs a fresh miniredis and returns its client
// together with the full cache key prefix for the subscription plan cache. The
// previous Redis state is restored on cleanup.
func useSubscriptionRedisForTest(t *testing.T) *redis.Client {
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

// The subscription plan cache is built once per process via sync.Once, so its
// Redis client has to be resolved per call: a captured client would outlive a
// replaced common.RDB and every write would go to the dead connection.
//
// This asserts on the cache itself rather than on getSubscriptionPlanByIdTx's
// return value. That function falls back to the database on a cache miss, so it
// returns the same plan whether or not the cache worked -- an earlier version of
// this test asserted only that and passed with the defect reintroduced.
func TestSubscriptionPlanCacheWritesToTheCurrentRedisClient(t *testing.T) {
	db := openTestDB(t, &SubscriptionPlan{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	plan := &SubscriptionPlan{
		Id:            91001,
		Title:         "Cache Follows Client",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, db.Create(plan).Error)

	// Warm the cache against the first client, then replace the client. The cache
	// instance survives (sync.Once); only the client behind RedisFunc changes.
	useSubscriptionRedisForTest(t)
	_, err := getSubscriptionPlanByIdTx(nil, plan.Id)
	require.NoError(t, err)

	current := useSubscriptionRedisForTest(t)

	// Clear any cached entry so the next read must repopulate through the cache,
	// then read again. The write has to land in the current client's server.
	InvalidateSubscriptionPlanCache(plan.Id)
	_, err = getSubscriptionPlanByIdTx(nil, plan.Id)
	require.NoError(t, err)

	fullKey := getSubscriptionPlanCache().FullKey(subscriptionPlanCacheKey(plan.Id))
	require.NotEmpty(t, fullKey)
	stored, err := current.Get(context.Background(), fullKey).Result()
	require.NoError(t, err,
		"the plan must be cached in the current Redis client, not a replaced one")
	require.Contains(t, stored, plan.Title)
}
