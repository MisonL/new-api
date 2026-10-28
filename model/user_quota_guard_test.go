package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// 预扣费是"读余额 → Go 里比较 → SQL 相对扣减"三步，没有在 SQL 层守卫余额。
// 并发请求都能通过准入检查，随后各自执行 quota = quota - ?，把余额扣成负数
// （负余额 = 已交付未付费的服务）。这里断言带守卫的扣减在并发下不会透支。
func TestDecreaseUserQuotaGuardedNeverOverdraws(t *testing.T) {
	db := openTestDB(t, &User{})
	previousDB := DB
	previousRedisEnabled := common.RedisEnabled
	DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		common.RedisEnabled = previousRedisEnabled
	})

	user := &User{
		Username: "overdraft-guard-user",
		Password: "12345678",
		Role:     1,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1000,
	}
	require.NoError(t, db.Create(user).Error)

	const (
		reservation = 400
		attempts    = 10
	)
	var (
		mu       sync.Mutex
		accepted int
		wg       sync.WaitGroup
	)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := DecreaseUserQuotaGuarded(user.Id, reservation)
			if err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
				return
			}
			require.ErrorIs(t, err, ErrUserQuotaInsufficient)
		}()
	}
	wg.Wait()

	var final User
	require.NoError(t, db.First(&final, user.Id).Error)
	require.Equal(t, 2, accepted, "only 2 reservations of 400 fit in 1000")
	require.GreaterOrEqual(t, final.Quota, 0,
		"guarded decrement must never drive the wallet negative (got %d)", final.Quota)
	require.Equal(t, 1000-2*reservation, final.Quota)
}

// 守卫失败时不得改动余额，也不得返回成功。
func TestDecreaseUserQuotaGuardedRejectsInsufficientBalance(t *testing.T) {
	db := openTestDB(t, &User{})
	previousDB := DB
	previousRedisEnabled := common.RedisEnabled
	DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		common.RedisEnabled = previousRedisEnabled
	})

	user := &User{
		Username: "overdraft-guard-user-2",
		Password: "12345678",
		Role:     1,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100,
	}
	require.NoError(t, db.Create(user).Error)

	require.ErrorIs(t, DecreaseUserQuotaGuarded(user.Id, 200), ErrUserQuotaInsufficient)

	var final User
	require.NoError(t, db.First(&final, user.Id).Error)
	require.Equal(t, 100, final.Quota)
}
