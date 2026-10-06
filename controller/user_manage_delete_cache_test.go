package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ManageUser 的 delete 分支没有 return：控制流落到 switch 之后的
// user.Update(false)。model.User.Update 忽略 DB.First 的错误（用户行已被软删，
// 必然 not found），末尾无条件 updateUserCache，于是刚被 user.Delete() 清掉的
// 用户缓存又被以 Status=enabled 写回 Redis。TokenAuth 只查 userCache.Status
// （middleware/auth.go:442），被删用户在缓存 TTL 内仍可继续调用 API。

func callManageUserDelete(t *testing.T, callerId int, targetId int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := fmt.Sprintf(`{"id":%d,"action":"delete"}`, targetId)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", common.RoleAdminUser)
	c.Set("id", callerId)
	c.Set("username", "admin")
	ManageUser(c)
	return recorder
}

// TestManageUserDeleteDoesNotRecacheUser is the regression: after an admin
// soft-deletes a user through ManageUser, the user cache must stay gone so
// TokenAuth stops honouring the deleted user.
func TestManageUserDeleteDoesNotRecacheUser(t *testing.T) {
	setupUserDeleteCacheTest(t)
	model.KeyColumn()
	seedCachedUser(t, 41, common.RoleCommonUser)

	// Precondition: the cache is populated, which is what makes this a real
	// finding rather than a no-op.
	require.True(t, userCacheExists(t, 41), "前置条件：删除前缓存应存在")

	recorder := callManageUserDelete(t, 999, 41)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	require.False(t, userCacheExists(t, 41),
		"管理员删除用户后不得把已删用户重新写回缓存，否则 TokenAuth 在 TTL 内继续放行")

	_, err := model.GetUserById(41, false)
	require.Error(t, err, "用户行必须真的被软删除")
}

// TestManageUserDisableStillUpdatesUser pins the neighbouring behaviour the
// delete fix must not disturb: disable still goes through user.Update and
// leaves the user cached with the disabled status.
func TestManageUserDisableStillUpdatesUser(t *testing.T) {
	setupUserDeleteCacheTest(t)
	model.KeyColumn()
	seedCachedUser(t, 42, common.RoleCommonUser)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage",
		strings.NewReader(`{"id":42,"action":"disable"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", common.RoleAdminUser)
	c.Set("id", 999)
	ManageUser(c)

	require.Contains(t, recorder.Body.String(), `"success":true`)
	user, err := model.GetUserById(42, false)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusDisabled, user.Status, "disable 必须落库")
}
