package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// override 直写数据库，与 add/subtract（走 Increase/DecreaseUserQuota 同步
// Redis）不同：不失效用户缓存时，GetUserQuota 在 TTL 内继续返回旧额度，
// 鉴权与预扣费按旧值判定（第十轮 P3）。回归断言：override 后缓存被清除，
// 下一次读取从数据库拿到新额度。
func TestManageUserOverrideInvalidatesQuotaCache(t *testing.T) {
	setupUserDeleteCacheTest(t)

	target := &model.User{
		Id:       61,
		Username: "override-61",
		Password: "password123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1000,
		AffCode:  "aff-61",
	}
	require.NoError(t, model.DB.Create(target).Error)

	// 前置条件：缓存里有旧额度。
	require.NoError(t, common.RedisHSetObj("user:61", &model.UserBase{
		Id: 61, Username: "override-61", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Quota: 1000, Group: "default",
	}, 3600))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage",
		strings.NewReader(`{"id":61,"action":"add_quota","mode":"override","value":250}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", common.RoleRootUser)
	c.Set("id", 999)
	c.Set("username", "root")
	ManageUser(c)

	var resp struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp), "body=%s", recorder.Body.String())
	require.True(t, resp.Success, "override 应成功：%s", recorder.Body.String())

	// 缓存可能已被清理，也可能已被后续读路径以新值回填（GetUserQuota 的
	// DB 读会异步回写）；两种状态都说明旧值不再生效。真正要防的是"缓存里
	// 仍是 1000"。
	var cached model.UserBase
	if err := common.RedisHGetObj("user:"+strconv.Itoa(61), &cached); err == nil {
		require.Equal(t, 250, cached.Quota,
			"override 后缓存不得继续持有旧额度，否则 60s 内按旧值鉴权/预扣")
	}

	quota, err := model.GetUserQuota(61, false)
	require.NoError(t, err)
	require.Equal(t, 250, quota)
}
