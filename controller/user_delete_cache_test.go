package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// DeleteUser hard-deletes the row but left the Redis user cache in place.
// TokenAuth reads that cache on every request (middleware/auth.go:442) and only
// checks userCache.Status, so a deleted user's tokens kept working until the
// cache TTL expired. DisableUser already invalidates both caches for exactly
// this reason; the delete path has to as well.
//
// The token cache matters too: an admin deleting a user who is mid-flight
// should not have that user's in-flight requests keep succeeding.

func setupUserDeleteCacheTest(t *testing.T) {
	t.Helper()
	setupHeaderPolicyControllerTestDB(t, &model.User{}, &model.Token{})

	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})
}

// userCacheExists reads the cache the way TokenAuth would: a hit means the
// deleted user is still being honoured.
func userCacheExists(t *testing.T, id int) bool {
	t.Helper()
	var cached model.UserBase
	err := common.RedisHGetObj("user:"+strconv.Itoa(id), &cached)
	return err == nil
}

func seedCachedUser(t *testing.T, id int, role int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{
		Id:       id,
		Username: "delete-target-" + strconv.Itoa(id),
		Role:     role,
		Status:   common.UserStatusEnabled,
		Quota:    1000,
	}).Error)

	cached := model.UserBase{
		Id:       id,
		Username: "delete-target-" + strconv.Itoa(id),
		Role:     role,
		Status:   common.UserStatusEnabled,
		Quota:    1000,
		Group:    "default",
	}
	require.NoError(t, common.RedisHSetObj("user:"+strconv.Itoa(id), &cached, 3600))
}

func callDeleteUser(t *testing.T, callerRole int, targetId int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/user/"+strconv.Itoa(targetId), nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(targetId)}}
	c.Set("role", callerRole)
	c.Set("id", 999)
	DeleteUser(c)
	return recorder
}

// TestDeleteUserInvalidatesUserCache is the regression: after a hard delete the
// user cache must be gone, so TokenAuth stops honouring the deleted user.
func TestDeleteUserInvalidatesUserCache(t *testing.T) {
	setupUserDeleteCacheTest(t)
	seedCachedUser(t, 31, common.RoleCommonUser)

	// Precondition: the cache is populated, which is what makes this a real
	// finding rather than a no-op.
	require.True(t, userCacheExists(t, 31), "前置条件：删除前缓存应存在")

	callDeleteUser(t, common.RoleAdminUser, 31)

	require.False(t, userCacheExists(t, 31),
		"硬删除用户后必须清理用户缓存，否则 TokenAuth 在 TTL 内继续放行")
}

// TestDeleteUserRemovesRow pins that the delete itself still happens -- the
// cache test above must not pass because the delete silently no-oped.
func TestDeleteUserRemovesRow(t *testing.T) {
	setupUserDeleteCacheTest(t)
	seedCachedUser(t, 32, common.RoleCommonUser)

	callDeleteUser(t, common.RoleAdminUser, 32)

	_, err := model.GetUserById(32, false)
	require.Error(t, err, "用户行必须真的被删除")
}

// TestDeleteUserRefusesEqualRoleTarget is the reverse check: the cache
// invalidation must not bypass the role guard.
func TestDeleteUserRefusesEqualRoleTarget(t *testing.T) {
	setupUserDeleteCacheTest(t)
	seedCachedUser(t, 33, common.RoleAdminUser)

	callDeleteUser(t, common.RoleAdminUser, 33)

	require.True(t, userCacheExists(t, 33),
		"同级管理员不得删除对方，缓存也不得被清")
	_, err := model.GetUserById(33, false)
	require.NoError(t, err, "目标用户必须仍在")
}
