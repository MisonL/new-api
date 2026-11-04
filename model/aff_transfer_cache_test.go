package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// TransferAffQuotaToQuota 在事务内直接写 quota 列，却不失效/同步 Redis
// 额度缓存。Redis 热缓存下转移成功后最长 60 秒 GetUserQuota(..., false)
// 仍读到旧余额，请求被判"额度不足"，而 /api/user/self 已经是新余额。
func TestTransferAffQuotaInvalidatesQuotaCache(t *testing.T) {
	db := openTestDB(t, &User{})
	previousDB := DB
	DB = db
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
		DB = previousDB
	})

	user := &User{
		Username: "aff-cache-probe",
		AffQuota: int(common.QuotaPerUnit),
		Quota:    0,
	}
	require.NoError(t, db.Create(user).Error)
	// 预热一条旧额度缓存。
	require.NoError(t, updateUserCache(*user))

	transferUser := &User{Id: user.Id}
	require.NoError(t, transferUser.TransferAffQuotaToQuota(int(common.QuotaPerUnit)))

	var cached UserBase
	require.Error(t, common.RedisHGetObj(getUserCacheKey(user.Id), &cached),
		"转移成功后必须失效额度缓存，否则 GetUserQuota 会持续读到旧余额")

	var after User
	require.NoError(t, db.First(&after, user.Id).Error)
	require.Equal(t, int(common.QuotaPerUnit), after.Quota)
	require.Equal(t, 0, after.AffQuota, "全部邀请额度已转出")
}
