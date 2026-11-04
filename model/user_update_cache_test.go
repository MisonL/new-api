package model

import (
	"context"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// User.Update / Edit 先用 DB.First 载入整行，再 Updates，最后无条件
// updateUserCache(*user)。这留下两个问题：
//  1. First 对软删除行返回 not found（错误被丢弃），Updates 命中 0 行，
//     缓存仍会写入调用方构造的“启用”快照，让已删除用户在 TTL 内复活；
//  2. First 拿到的是更新前的行，Updates 不修改结构体，缓存写入的是旧值。
// 修复后：First 失败直接返回错误；成功更新后重新载入再写缓存。

func setupUserUpdateCacheTest(t *testing.T) {
	t.Helper()
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
}

func readCachedUserBase(t *testing.T, id int) *UserBase {
	t.Helper()
	var cached UserBase
	require.NoError(t, common.RedisHGetObj("user:"+strconv.Itoa(id), &cached))
	return &cached
}

func TestUserUpdateFailsForSoftDeletedUserAndSkipsCache(t *testing.T) {
	setupUserUpdateCacheTest(t)

	seed := User{
		Id:       701,
		Username: "gone",
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
		Quota:    100,
	}
	require.NoError(t, DB.Create(&seed).Error)
	require.NoError(t, DB.Delete(&seed).Error)

	stale := User{
		Id:       701,
		Username: "gone",
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
	}
	require.Error(t, stale.Update(false), "对软删除行的 Update 必须显式失败")

	exists, err := common.RDB.Exists(context.Background(), "user:701").Result()
	require.NoError(t, err)
	require.Zero(t, exists, "更新失败时不能把“启用”快照写进用户缓存")

	var deletedCount int64
	require.NoError(t, DB.Unscoped().Model(&User{}).
		Where("id = ? AND deleted_at IS NOT NULL", 701).Count(&deletedCount).Error)
	require.Equal(t, int64(1), deletedCount)
}

func TestUserUpdateWritesPostUpdateStateToCache(t *testing.T) {
	setupUserUpdateCacheTest(t)

	seed := User{
		Id:       702,
		Username: "before",
		Group:    "default",
		Status:   common.UserStatusDisabled,
		Role:     common.RoleCommonUser,
		Quota:    500,
	}
	require.NoError(t, DB.Create(&seed).Error)

	var loaded User
	require.NoError(t, DB.First(&loaded, 702).Error)
	loaded.Username = "after"
	loaded.Group = "vip"
	loaded.Status = common.UserStatusEnabled
	require.NoError(t, loaded.Update(false))

	cached := readCachedUserBase(t, 702)
	require.Equal(t, "after", cached.Username, "缓存必须是更新后的用户名")
	require.Equal(t, "vip", cached.Group, "缓存必须是更新后的分组")
	require.Equal(t, common.UserStatusEnabled, cached.Status, "缓存必须是更新后的状态")
	require.Equal(t, 500, cached.Quota)
}

func TestUserEditWritesPostEditStateToCache(t *testing.T) {
	setupUserUpdateCacheTest(t)

	seed := User{
		Id:       703,
		Username: "edit-before",
		Group:    "default",
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
		Quota:    300,
	}
	require.NoError(t, DB.Create(&seed).Error)

	var loaded User
	require.NoError(t, DB.First(&loaded, 703).Error)
	loaded.Username = "edit-after"
	loaded.DisplayName = "edited"
	loaded.Group = "svip"
	require.NoError(t, loaded.Edit(false))

	cached := readCachedUserBase(t, 703)
	require.Equal(t, "edit-after", cached.Username)
	require.Equal(t, "svip", cached.Group)
}
