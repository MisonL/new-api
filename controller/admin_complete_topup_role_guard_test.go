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

// AdminCompleteTopUp 只校验调用者是不是管理员，不校验订单所属用户的角色。
// ManageUser 的 add_quota 明确禁止管理员操作同级或更高角色（含自己）：
// myRole <= user.Role && myRole != RoleRootUser。补单接口会直接给订单所属用户
// 充值，缺这道护栏时管理员可以给自己的待支付订单补单，或给同级管理员补单，
// 绕过 add_quota 的限制。

func setupAdminCompleteTopUpTest(t *testing.T) {
	t.Helper()
	setupHeaderPolicyControllerTestDB(t, &model.User{}, &model.TopUp{}, &model.Log{})
	model.KeyColumn()
}

func seedTopUpTarget(t *testing.T, id int, role int, quota int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{
		Id:       id,
		Username: fmt.Sprintf("topup-target-%d", id),
		Role:     role,
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}).Error)
}

func seedPendingTopUp(t *testing.T, tradeNo string, userId int, amount int64) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:     userId,
		TradeNo:    tradeNo,
		Amount:     amount,
		Status:     common.TopUpStatusPending,
		CreateTime: common.GetTimestamp(),
	}).Error)
}

func callAdminCompleteTopUp(t *testing.T, callerRole int, callerId int, tradeNo string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/topup/complete",
		strings.NewReader(fmt.Sprintf(`{"trade_no":%q}`, tradeNo)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", callerRole)
	c.Set("id", callerId)
	AdminCompleteTopUp(c)
	return recorder
}

func requireTopUpStillPending(t *testing.T, tradeNo string, targetId int, quotaBefore int) {
	t.Helper()
	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", tradeNo).First(&topUp).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status, "订单不得被补单")
	user, err := model.GetUserById(targetId, false)
	require.NoError(t, err)
	require.Equal(t, quotaBefore, user.Quota, "额度不得变化")
}

// TestAdminCompleteTopUpRefusesSelfOrder：管理员不得给自己的待支付订单补单。
func TestAdminCompleteTopUpRefusesSelfOrder(t *testing.T) {
	setupAdminCompleteTopUpTest(t)
	seedTopUpTarget(t, 502, common.RoleAdminUser, 1000)
	seedPendingTopUp(t, "trade-self-502", 502, 10)

	recorder := callAdminCompleteTopUp(t, common.RoleAdminUser, 502, "trade-self-502")

	require.Contains(t, recorder.Body.String(), `"success":false`,
		"给自己的订单补单必须被拒绝，响应=%s", recorder.Body.String())
	requireTopUpStillPending(t, "trade-self-502", 502, 1000)
}

// TestAdminCompleteTopUpRefusesEqualRoleTarget：管理员不得给同级管理员的订单补单。
func TestAdminCompleteTopUpRefusesEqualRoleTarget(t *testing.T) {
	setupAdminCompleteTopUpTest(t)
	seedTopUpTarget(t, 503, common.RoleAdminUser, 1000)
	seedPendingTopUp(t, "trade-equal-503", 503, 10)

	recorder := callAdminCompleteTopUp(t, common.RoleAdminUser, 502, "trade-equal-503")

	require.Contains(t, recorder.Body.String(), `"success":false`,
		"同级管理员的订单不得被补单，响应=%s", recorder.Body.String())
	requireTopUpStillPending(t, "trade-equal-503", 503, 1000)
}

// TestAdminCompleteTopUpRefusesRootTarget：管理员不得给 root 的订单补单。
func TestAdminCompleteTopUpRefusesRootTarget(t *testing.T) {
	setupAdminCompleteTopUpTest(t)
	seedTopUpTarget(t, 504, common.RoleRootUser, 1000)
	seedPendingTopUp(t, "trade-root-504", 504, 10)

	recorder := callAdminCompleteTopUp(t, common.RoleAdminUser, 502, "trade-root-504")

	require.Contains(t, recorder.Body.String(), `"success":false`,
		"root 的订单不得被管理员补单，响应=%s", recorder.Body.String())
	requireTopUpStillPending(t, "trade-root-504", 504, 1000)
}

// TestAdminCompleteTopUpAllowsCommonUserOrder 是正向对照：护栏不得挡住正常的
// 客服补单路径——管理员为普通用户的订单补单必须照常完成并充值。
func TestAdminCompleteTopUpAllowsCommonUserOrder(t *testing.T) {
	setupAdminCompleteTopUpTest(t)
	seedTopUpTarget(t, 505, common.RoleCommonUser, 0)
	seedPendingTopUp(t, "trade-common-505", 505, 10)

	recorder := callAdminCompleteTopUp(t, common.RoleAdminUser, 502, "trade-common-505")

	require.Contains(t, recorder.Body.String(), `"success":true`,
		"普通用户订单的补单必须照常成功，响应=%s", recorder.Body.String())
	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", "trade-common-505").First(&topUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	user, err := model.GetUserById(505, false)
	require.NoError(t, err)
	require.Equal(t, int(10*common.QuotaPerUnit), user.Quota, "订单金额必须充值到账")
}

// TestAdminCompleteTopUpRootCanCompleteAnyOrder：root 不受层级护栏限制。
func TestAdminCompleteTopUpRootCanCompleteAnyOrder(t *testing.T) {
	setupAdminCompleteTopUpTest(t)
	seedTopUpTarget(t, 506, common.RoleAdminUser, 0)
	seedPendingTopUp(t, "trade-rootcaller-506", 506, 10)

	recorder := callAdminCompleteTopUp(t, common.RoleRootUser, 501, "trade-rootcaller-506")

	require.Contains(t, recorder.Body.String(), `"success":true`,
		"root 补单必须照常成功，响应=%s", recorder.Body.String())
	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", "trade-rootcaller-506").First(&topUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
}
