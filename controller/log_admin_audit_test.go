package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupLogAdminAuditControllerTestDB(t *testing.T) {
	t.Helper()
	setupCustomOAuthJWTControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TwoFA{}, &model.TwoFABackupCode{}))
}

func TestManageUserAddQuotaStoresOperatorInAdminInfo(t *testing.T) {
	setupLogAdminAuditControllerTestDB(t)

	targetUser := &model.User{
		Username:    "quota-target",
		Password:    "password123",
		DisplayName: "quota-target",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, model.DB.Create(targetUser).Error)

	payload := []byte(`{"id":` + strconv.Itoa(targetUser.Id) + `,"action":"add_quota","mode":"add","value":1000}`)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 900)
	ctx.Set("role", common.RoleRootUser)
	ctx.Set("username", "root-admin")

	ManageUser(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)

	var stored model.Log
	require.NoError(t, model.DB.Where("user_id = ? AND type = ?", targetUser.Id, model.LogTypeManage).Last(&stored).Error)
	require.Contains(t, stored.Content, "管理员增加用户额度")
	require.NotContains(t, stored.Content, "root-admin")
	require.NotContains(t, stored.Content, "管理员(")

	other, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	adminInfo := other["admin_info"].(map[string]interface{})
	require.EqualValues(t, 900, adminInfo["admin_id"])
	require.Equal(t, "root-admin", adminInfo["admin_username"])
}

func TestAdminDisable2FAStoresOperatorInAdminInfo(t *testing.T) {
	setupLogAdminAuditControllerTestDB(t)

	targetUser := &model.User{
		Username:    "twofa-target",
		Password:    "password123",
		DisplayName: "twofa-target",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, model.DB.Create(targetUser).Error)
	require.NoError(t, model.DB.Create(&model.TwoFA{
		UserId:    targetUser.Id,
		Secret:    "secret",
		IsEnabled: true,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/2fa/admin/"+strconv.Itoa(targetUser.Id)+"/disable", nil)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(targetUser.Id)}}
	ctx.Set("id", 901)
	ctx.Set("role", common.RoleRootUser)
	ctx.Set("username", "security-admin")

	AdminDisable2FA(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)

	var stored model.Log
	require.NoError(t, model.DB.Where("user_id = ? AND type = ?", targetUser.Id, model.LogTypeManage).Last(&stored).Error)
	require.Equal(t, "管理员强制禁用了用户的两步验证", stored.Content)

	other, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	adminInfo := other["admin_info"].(map[string]interface{})
	require.EqualValues(t, 901, adminInfo["admin_id"])
	require.Equal(t, "security-admin", adminInfo["admin_username"])
}
