package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// 令牌额度准入此前是"读 remain_quota → Go 里比较 → 相对扣减"，并发请求
// 都能通过检查，各自执行 remain_quota = remain_quota - ?，把令牌预算扣成
// 负数。这里断言带守卫的扣减在并发下不会透支。
func TestDecreaseTokenQuotaGuardedNeverOverdraws(t *testing.T) {
	db := openTestDB(t, &Token{})
	previousDB := DB
	previousRedisEnabled := common.RedisEnabled
	DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		common.RedisEnabled = previousRedisEnabled
	})

	token := &Token{
		UserId:      1,
		Key:         "overdraft-guard-token",
		Name:        "overdraft-guard-token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 1000,
	}
	require.NoError(t, db.Create(token).Error)

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
			err := DecreaseTokenQuotaGuarded(token.Id, token.Key, reservation)
			if err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
				return
			}
			require.ErrorIs(t, err, ErrTokenQuotaInsufficient)
		}()
	}
	wg.Wait()

	var final Token
	require.NoError(t, db.First(&final, token.Id).Error)
	require.Equal(t, 2, accepted, "only 2 reservations of 400 fit in 1000")
	require.GreaterOrEqual(t, final.RemainQuota, 0,
		"guarded decrement must never drive the token budget negative (got %d)", final.RemainQuota)
	require.Equal(t, 1000-2*reservation, final.RemainQuota)
	require.Equal(t, 2*reservation, final.UsedQuota, "used_quota must track accepted reservations")
}

// 守卫失败时不得改动额度，也不得返回成功。
func TestDecreaseTokenQuotaGuardedRejectsInsufficientBudget(t *testing.T) {
	db := openTestDB(t, &Token{})
	previousDB := DB
	previousRedisEnabled := common.RedisEnabled
	DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		common.RedisEnabled = previousRedisEnabled
	})

	token := &Token{
		UserId:      1,
		Key:         "overdraft-guard-token-2",
		Name:        "overdraft-guard-token-2",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 100,
	}
	require.NoError(t, db.Create(token).Error)

	require.ErrorIs(t, DecreaseTokenQuotaGuarded(token.Id, token.Key, 200), ErrTokenQuotaInsufficient)

	var final Token
	require.NoError(t, db.First(&final, token.Id).Error)
	require.Equal(t, 100, final.RemainQuota)
	require.Equal(t, 0, final.UsedQuota)
}
