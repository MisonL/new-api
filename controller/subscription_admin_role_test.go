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

// The subscription admin endpoints move real money and real group membership:
// AdminBindSubscription and AdminCreateUserSubscription can raise a user's
// group (plan.UpgradeGroup), and the invalidate/delete pair can cancel
// anyone's plan. None of them checked the target's role, so a plain admin
// could grant or revoke a root user's entitlements. UpdateUser and
// AdminClearUserBinding already refuse an equal-or-higher target; these had no
// such guard.

func setupSubscriptionAdminTest(t *testing.T) {
	t.Helper()
	setupHeaderPolicyControllerTestDB(t,
		&model.User{},
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
		&model.Token{},
	)
	// NOTE: the subscription plan cache is process-wide (sync.Once + in-memory
	// hot cache), so a plan seeded by an earlier test survives into this one even
	// though each test gets its own sqlite database. Every plan id below is
	// therefore distinct per test, and seedSubscriptionPlan evicts its own key.
	//
	// commonGroupCol is set by the unexported initCol(), which production reaches
	// through InitDB. Without it getUserGroupByIdTx builds "SELECT  FROM users",
	// a syntax error -- which would make the "rejects" cases below pass for the
	// wrong reason. KeyColumn() reaches the same initializer.
	model.KeyColumn()
}

func seedSubscriptionUser(t *testing.T, id int, role int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{
		Id:       id,
		Username: "sub-target-" + strconv.Itoa(id),
		Role:     role,
		Status:   common.UserStatusEnabled,
		Quota:    1000,
	}).Error)
}

func seedSubscriptionPlan(t *testing.T, id int, upgradeGroup string) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:                 id,
		Title:              "plan-" + strconv.Itoa(id),
		Currency:           "USD",
		DurationUnit:       "month",
		DurationValue:      1,
		Enabled:            true,
		UpgradeGroup:       upgradeGroup,
		MaxPurchasePerUser: 0,
	}).Error)
	// Evict any entry a previous test left under this id.
	model.InvalidateSubscriptionPlanCache(id)
}

func seedUserSubscription(t *testing.T, id int, userId int, planId int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:          id,
		UserId:      userId,
		PlanId:      planId,
		Status:      "active",
		Source:      "admin",
		AmountTotal: 10000,
		AmountUsed:  0,
	}).Error)
}

func adminJSONRequest(t *testing.T, callerRole int, method string, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var reader *strings.Reader
	if body != nil {
		payload, err := common.Marshal(body)
		require.NoError(t, err)
		reader = strings.NewReader(string(payload))
	} else {
		reader = strings.NewReader("")
	}
	c.Request = httptest.NewRequest(method, path, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", callerRole)
	c.Set("id", 999)
	return w
}

func setSubscriptionParam(c *gin.Context, id string) {
	c.Params = gin.Params{{Key: "id", Value: id}}
}

// TestAdminCreateUserSubscriptionRejectsHigherRoleTarget is the regression for
// the bind path: binding a plan can raise the target's group, so an admin must
// not be able to do that for the root user.
func TestAdminCreateUserSubscriptionRejectsHigherRoleTarget(t *testing.T) {
	setupSubscriptionAdminTest(t)
	seedSubscriptionUser(t, 21, common.RoleRootUser)
	seedSubscriptionPlan(t, 101, "vip")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/21", strings.NewReader(`{"plan_id":101}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "21"}}
	c.Set("role", common.RoleAdminUser)
	c.Set("id", 999)

	AdminCreateUserSubscription(c)

	var count int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).
		Where("user_id = ?", 21).Count(&count).Error)
	require.Zero(t, count, "普通管理员不得给更高角色用户绑定套餐")
}

// TestAdminCreateUserSubscriptionAllowsLowerRoleTarget is the reverse check:
// the guard must not block the ordinary case.
func TestAdminCreateUserSubscriptionAllowsLowerRoleTarget(t *testing.T) {
	setupSubscriptionAdminTest(t)
	seedSubscriptionUser(t, 22, common.RoleCommonUser)
	seedSubscriptionPlan(t, 201, "")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/22", strings.NewReader(`{"plan_id":201}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "22"}}
	c.Set("role", common.RoleAdminUser)
	c.Set("id", 999)

	AdminCreateUserSubscription(c)

	var count int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).
		Where("user_id = ?", 22).Count(&count).Error)
	require.Equal(t, int64(1), count, "管理员绑定普通用户套餐必须成功")
}

// TestAdminBindSubscriptionRejectsHigherRoleTarget covers the other bind route,
// which takes user_id in the body rather than the path.
func TestAdminBindSubscriptionRejectsHigherRoleTarget(t *testing.T) {
	setupSubscriptionAdminTest(t)
	seedSubscriptionUser(t, 23, common.RoleRootUser)
	seedSubscriptionPlan(t, 102, "vip")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/admin/bind",
		strings.NewReader(`{"user_id":23,"plan_id":102}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", common.RoleAdminUser)
	c.Set("id", 999)

	AdminBindSubscription(c)

	var count int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).
		Where("user_id = ?", 23).Count(&count).Error)
	require.Zero(t, count, "普通管理员不得给更高角色用户绑定套餐")
}

// TestAdminInvalidateUserSubscriptionRejectsHigherRoleTarget covers the
// cancel route, which addresses the subscription rather than the user, so the
// role check has to resolve the owner first.
func TestAdminInvalidateUserSubscriptionRejectsHigherRoleTarget(t *testing.T) {
	setupSubscriptionAdminTest(t)
	seedSubscriptionUser(t, 24, common.RoleRootUser)
	seedSubscriptionPlan(t, 202, "")
	seedUserSubscription(t, 41, 24, 1)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/41", nil)
	c.Params = gin.Params{{Key: "id", Value: "41"}}
	c.Set("role", common.RoleAdminUser)
	c.Set("id", 999)

	AdminInvalidateUserSubscription(c)

	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 41).Error)
	require.Equal(t, "active", sub.Status, "普通管理员不得作废更高角色用户的订阅")
}

// TestAdminDeleteUserSubscriptionRejectsHigherRoleTarget covers hard delete.
func TestAdminDeleteUserSubscriptionRejectsHigherRoleTarget(t *testing.T) {
	setupSubscriptionAdminTest(t)
	seedSubscriptionUser(t, 25, common.RoleRootUser)
	seedSubscriptionPlan(t, 203, "")
	seedUserSubscription(t, 51, 25, 1)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodDelete, "/51", nil)
	c.Params = gin.Params{{Key: "id", Value: "51"}}
	c.Set("role", common.RoleAdminUser)
	c.Set("id", 999)

	AdminDeleteUserSubscription(c)

	var count int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).
		Where("id = ?", 51).Count(&count).Error)
	require.Equal(t, int64(1), count, "普通管理员不得删除更高角色用户的订阅")
}

// TestAdminListUserSubscriptionsRejectsHigherRoleTarget covers the read route.
// The codebase reports refusals as HTTP 200 with success:false (common.ApiErrorMsg),
// so the assertion is on the body, not the status code.
func TestAdminListUserSubscriptionsRejectsHigherRoleTarget(t *testing.T) {
	setupSubscriptionAdminTest(t)
	seedSubscriptionUser(t, 26, common.RoleRootUser)
	seedSubscriptionPlan(t, 204, "")
	seedUserSubscription(t, 61, 26, 204)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/26/subscriptions", nil)
	c.Params = gin.Params{{Key: "id", Value: "26"}}
	c.Set("role", common.RoleAdminUser)
	c.Set("id", 999)

	AdminListUserSubscriptions(c)

	var resp struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &resp))
	require.False(t, resp.Success, "普通管理员不得列出更高角色用户的订阅")
}
