package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ManageUser 用 model.DB.Unscoped().Where(&user).First(&user) 载入目标用户。
// req.Id=0 时 Where 不产生条件，First 返回最低 ID 行（通常是 root），
// `if user.Id == 0` 永不命中：root 的 add_quota 等操作会落到错误目标上。
func TestManageUserRejectsZeroIdBeforeTouchingDatabase(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t, &model.User{})
	require.NoError(t, model.DB.Create(&model.User{
		Id:       1,
		Username: "root",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Quota:    0,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage",
		strings.NewReader(`{"id":0,"action":"add_quota","mode":"add","value":100}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", common.RoleRootUser)
	c.Set("id", 1)
	c.Set("username", "root")

	ManageUser(c)

	var root model.User
	require.NoError(t, model.DB.First(&root, 1).Error)
	require.Zero(t, root.Quota, "id=0 不能落到第一行用户上执行操作")
	require.Contains(t, recorder.Body.String(), `"success":false`)
}
