package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// AdminResetPasskey deletes another user's passkey outright. The sibling admin
// endpoint AdminDisable2FA refuses to act on a target whose role is equal to or
// higher than the caller's, so a common admin cannot strip a peer admin's
// second factor. AdminResetPasskey had no such check, which let any admin
// delete a root user's passkey -- a strictly stronger action than the one the
// 2FA endpoint already guards.

func setupPasskeyAdminTest(t *testing.T) {
	t.Helper()
	setupHeaderPolicyControllerTestDB(t, &model.User{}, &model.PasskeyCredential{})
}

func seedPasskeyTarget(t *testing.T, id int, role int) {
	t.Helper()
	user := &model.User{
		Id:       id,
		Username: "passkey-target-" + strconv.Itoa(id),
		Role:     role,
		Status:   common.UserStatusEnabled,
		Quota:    100,
	}
	require.NoError(t, model.DB.Create(user).Error)

	credential := &model.PasskeyCredential{
		UserID:       id,
		CredentialID: "cred-" + strconv.Itoa(id),
		PublicKey:    "pk",
	}
	require.NoError(t, model.DB.Create(credential).Error)
}

func passkeyStillExists(t *testing.T, userId int) bool {
	t.Helper()
	_, err := model.GetPasskeyByUserID(userId)
	return err == nil
}

// callAdminResetPasskey invokes the handler with the caller's role already set,
// the way AdminAuth would have populated it.
func callAdminResetPasskey(t *testing.T, callerRole int, targetId int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/user/"+strconv.Itoa(targetId)+"/reset_passkey", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(targetId)}}
	c.Set("role", callerRole)
	c.Set("id", 999)
	AdminResetPasskey(c)
	return w
}

// TestAdminResetPasskeyRejectsEqualRoleTarget is the regression: an admin must
// not be able to delete a passkey belonging to a user of the same role.
func TestAdminResetPasskeyRejectsEqualRoleTarget(t *testing.T) {
	setupPasskeyAdminTest(t)
	seedPasskeyTarget(t, 11, common.RoleAdminUser)

	callAdminResetPasskey(t, common.RoleAdminUser, 11)

	require.True(t, passkeyStillExists(t, 11),
		"同级管理员不得重置对方 Passkey")
}

// TestAdminResetPasskeyRejectsHigherRoleTarget covers the case that matters
// most: a common admin must not strip the root user's passkey.
func TestAdminResetPasskeyRejectsHigherRoleTarget(t *testing.T) {
	setupPasskeyAdminTest(t)
	seedPasskeyTarget(t, 12, common.RoleRootUser)

	callAdminResetPasskey(t, common.RoleAdminUser, 12)

	require.True(t, passkeyStillExists(t, 12),
		"普通管理员不得重置超级管理员的 Passkey")
}

// TestAdminResetPasskeyAllowsLowerRoleTarget is the reverse check: the guard
// must not block the legitimate case it exists to serve.
func TestAdminResetPasskeyAllowsLowerRoleTarget(t *testing.T) {
	setupPasskeyAdminTest(t)
	seedPasskeyTarget(t, 13, common.RoleCommonUser)

	callAdminResetPasskey(t, common.RoleAdminUser, 13)

	require.False(t, passkeyStillExists(t, 13),
		"管理员重置普通用户 Passkey 必须成功")
}

// TestAdminResetPasskeyAllowsRootOnAdmin pins the root bypass: the sibling 2FA
// endpoint exempts root from the comparison, and this endpoint must match.
func TestAdminResetPasskeyAllowsRootOnAdmin(t *testing.T) {
	setupPasskeyAdminTest(t)
	seedPasskeyTarget(t, 14, common.RoleAdminUser)

	callAdminResetPasskey(t, common.RoleRootUser, 14)

	require.False(t, passkeyStillExists(t, 14),
		"超级管理员必须能重置管理员 Passkey")
}
