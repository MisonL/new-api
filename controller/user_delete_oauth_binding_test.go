package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// 硬删用户必须同时释放 user_oauth_bindings 行。身份检查
// IsProviderUserIdTaken 统计的是绑定表全量行（不区分用户是否已删），残留的
// 孤儿绑定会让该 OAuth 身份永久登录失败：登录流程看到"已被占用"，随后的
// GetUserByOAuthBinding 又因用户行不存在报 record not found，既不注册也不
// 合并（controller/oauth.go:399-402）。管理员按 UI 文案（"释放其邮箱与第三方
// 绑定"）删除账号后，该身份必须能被重新登录/重新绑定。
func TestDeleteUserReleasesOAuthBindings(t *testing.T) {
	setupUserDeleteCacheTest(t)
	// 绑定表要参与迁移；用户表中转的 key/group 列名也初始化，避免陈旧
	// 令牌缓存清理路径拼出 `SELECT  FROM` 的测试环境噪音。
	require.NoError(t, model.DB.AutoMigrate(&model.UserOAuthBinding{}))
	model.KeyColumn()

	target := &model.User{
		Id:       51,
		Username: "binding-holder-51",
		Password: "password123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "aff-51",
	}
	require.NoError(t, model.DB.Create(target).Error)
	require.NoError(t, model.DB.Create(&model.UserOAuthBinding{
		UserId:         target.Id,
		ProviderId:     9,
		ProviderUserId: "idp-subject-51",
	}).Error)

	// 前置条件：身份已被占用且能解析到用户。
	require.True(t, model.IsProviderUserIdTaken(9, "idp-subject-51"))
	bound, err := model.GetUserByOAuthBinding(9, "idp-subject-51")
	require.NoError(t, err)
	require.Equal(t, target.Id, bound.Id)

	recorder := callDeleteUser(t, common.RoleAdminUser, target.Id)
	require.Equal(t, http.StatusOK, recorder.Code)

	var bindingRows int64
	require.NoError(t, model.DB.Model(&model.UserOAuthBinding{}).
		Where("user_id = ?", target.Id).Count(&bindingRows).Error)
	require.EqualValues(t, 0, bindingRows, "硬删后不得残留孤儿绑定行")

	// 决定性断言：该身份重新可用（可重新登录/重新绑定）。
	require.False(t, model.IsProviderUserIdTaken(9, "idp-subject-51"),
		"硬删后 OAuth 身份必须释放，否则该账号永久无法再登录")
}

// 同一事务语义：不得误删其他用户的绑定。
func TestDeleteUserKeepsOtherUsersOAuthBindings(t *testing.T) {
	setupUserDeleteCacheTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.UserOAuthBinding{}))

	doomed := &model.User{Id: 52, Username: "doomed-52", Password: "password123",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "aff-52"}
	keeper := &model.User{Id: 53, Username: "keeper-53", Password: "password123",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "aff-53"}
	require.NoError(t, model.DB.Create(doomed).Error)
	require.NoError(t, model.DB.Create(keeper).Error)
	require.NoError(t, model.DB.Create(&model.UserOAuthBinding{UserId: 52, ProviderId: 9, ProviderUserId: "idp-doomed-52"}).Error)
	require.NoError(t, model.DB.Create(&model.UserOAuthBinding{UserId: 53, ProviderId: 9, ProviderUserId: "idp-keeper-53"}).Error)

	recorder := callDeleteUser(t, common.RoleAdminUser, 52)
	require.Equal(t, http.StatusOK, recorder.Code)

	require.True(t, model.IsProviderUserIdTaken(9, "idp-keeper-53"), "他人绑定不得被误删")
	keeperRow, err := model.GetUserByOAuthBinding(9, "idp-keeper-53")
	require.NoError(t, err)
	require.Equal(t, 53, keeperRow.Id)
}
