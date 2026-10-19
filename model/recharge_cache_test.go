package model

import (
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func openIsolatedRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("NEW_API_REDIS_TEST_ADDR")
	pass := os.Getenv("NEW_API_REDIS_TEST_PASSWORD")
	if addr == "" {
		t.Skip("NEW_API_REDIS_TEST_ADDR not set")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, Password: pass, DB: 15})
	require.NoError(t, client.FlushDB(client.Context()).Err())
	return client
}

// 充值/兑换直接写库后必须失效用户缓存。否则用户付款成功、或刚兑换完兑换码，
// 立刻发请求仍会被 PreConsumeQuota 判为"额度不足"，最长持续一个 SyncFrequency
// （默认 60 秒）。
//
// 需要真实 Redis：NEW_API_REDIS_TEST_ADDR / NEW_API_REDIS_TEST_PASSWORD，
// 未设置时跳过。
func TestRechargeInvalidatesQuotaCache(t *testing.T) {
	client := openIsolatedRedis(t)
	db := setupTopUpPaymentSecurityTestDB(t)
	// setupTopUpPaymentSecurityTestDB 会把 RedisEnabled 置回 false，必须在它之后
	// 再打开，否则 GetUserQuota 根本不走缓存，测试也就证明不了失效逻辑。
	prevRDB, prevEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = prevRDB, prevEnabled })
	user := &User{Username: "recharge-cache", Password: "x", AffCode: "rc1", Status: 1, Quota: 0}
	require.NoError(t, db.Create(user).Error)

	// 先写入一个"充值前"的完整缓存（带 TTL）。注意不能用
	// updateUserQuotaCache 单独建 key：RedisHSetField 在 key 不存在时静默
	// no-op，那样测不到任何缓存。
	previousSync := common.SyncFrequency
	common.SyncFrequency = 60
	t.Cleanup(func() { common.SyncFrequency = previousSync })
	require.NoError(t, updateUserCache(*user))
	cached, err := getUserQuotaCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 0, cached)

	// 充值写库 + 失效缓存
	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).Update("quota", 500).Error)
	InvalidateUserQuotaAfterRecharge(user.Id)

	// 下一次读取必须回源到新值，而不是 60 秒前的旧缓存。
	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, 500, quota, "after a recharge the cached quota must not shadow the new balance")
	time.Sleep(10 * time.Millisecond)
}
