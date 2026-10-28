package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func setupTokenCacheRedis(t *testing.T) {
	t.Helper()
	srv, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	previousRDB := common.RDB
	previousEnabled := common.RedisEnabled
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB = previousRDB
		common.RedisEnabled = previousEnabled
		_ = client.Close()
		srv.Close()
	})
}

// RedisHIncrBy 在 key 过期后会重建一个只含 RemainQuota 的哈希。令牌缓存读取
// 没有完整性校验（用户缓存有），命中这个残缺哈希时拿到 Id=0、Status=0 的
// Token，ValidateUserToken 判为无效，该令牌在残缺哈希过期前（最长 60 秒）的
// 请求全部 401。这里断言残缺哈希必须被识别为缓存缺失并回源数据库。
func TestTokenCacheRejectsPartialHash(t *testing.T) {
	setupTokenCacheRedis(t)

	key := "sk-partial-hash-probe"
	hmacKey := common.GenerateHMAC(key)
	redisKey := fmt.Sprintf("token:%s", hmacKey)

	// 模拟：完整缓存过期后，结算时 HINCRBY 重建出只含 RemainQuota 的哈希。
	require.NoError(t, common.RedisHIncrBy(redisKey, "RemainQuota", -500))

	cached, err := cacheGetTokenByKey(key)
	require.Error(t, err, "a partial hash must not be served as a valid token: %+v", cached)
	require.Nil(t, cached)
}

// 完整缓存必须照常命中，完整性校验不能把正常路径挡掉。
func TestTokenCacheStillServesCompleteHash(t *testing.T) {
	setupTokenCacheRedis(t)

	token := Token{
		Id:             42,
		UserId:         7,
		Key:            "sk-complete-hash-probe",
		Status:         common.TokenStatusEnabled,
		Name:           "probe",
		ExpiredTime:    -1,
		RemainQuota:    1000,
		UnlimitedQuota: false,
		Group:          "default",
	}
	require.NoError(t, cacheSetToken(token))

	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	require.NotNil(t, cached)
	require.Equal(t, token.Id, cached.Id)
	require.Equal(t, common.TokenStatusEnabled, cached.Status)
	require.Equal(t, 1000, cached.RemainQuota)
}
