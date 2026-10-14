package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newTokenGroupContext(t *testing.T, userGroup string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/token/", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, userGroup)
	return ctx
}

// A group the user can actually use is accepted. This is the case the old
// behaviour already handled; it guards against the new check over-rejecting.
// "default" and "vip" are the built-in usable groups.
func TestValidateTokenGroupAcceptsUsableGroup(t *testing.T) {
	ctx := newTokenGroupContext(t, "default")

	require.NoError(t, validateTokenGroup(ctx, "vip"))
}

// An empty group means "use the user's own group" and must stay valid.
func TestValidateTokenGroupAcceptsEmptyAndAuto(t *testing.T) {
	ctx := newTokenGroupContext(t, "default")

	require.NoError(t, validateTokenGroup(ctx, ""))
	require.NoError(t, validateTokenGroup(ctx, "   "))
	require.NoError(t, validateTokenGroup(ctx, "auto"))
}

// A group the user cannot use was previously stored and only failed at request
// time with "无权访问 <group> 分组". It must be rejected at write time instead.
func TestValidateTokenGroupRejectsUnusableGroup(t *testing.T) {
	ctx := newTokenGroupContext(t, "default")

	err := validateTokenGroup(ctx, "../../etc/passwd")

	require.Error(t, err)
	require.Contains(t, err.Error(), "无权访问")
}

// The validation has to be wired into the handlers, not just available as a
// helper: a token whose group the user cannot use used to be stored and then
// failed every request with "无权访问 <group> 分组".
func TestAddTokenRejectsUnusableGroup(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	_ = db

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/token/", map[string]any{
		"name":            "group-check",
		"remain_quota":    100,
		"unlimited_quota": true,
		"group":           "../../etc/passwd",
	}, 1)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")

	AddToken(ctx)

	response := decodeAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "无权访问")

	var stored int64
	require.NoError(t, db.Model(&model.Token{}).Where("name = ?", "group-check").Count(&stored).Error)
	require.Zero(t, stored, "an unusable group must not be persisted")
}

// A usable group still goes through, so the new guard does not block the
// supported path.
func TestAddTokenAcceptsUsableGroup(t *testing.T) {
	db := setupTokenControllerTestDB(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/token/", map[string]any{
		"name":            "group-ok",
		"remain_quota":    100,
		"unlimited_quota": true,
		"group":           "vip",
	}, 1)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")

	AddToken(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var stored int64
	require.NoError(t, db.Model(&model.Token{}).Where("name = ?", "group-ok").Count(&stored).Error)
	require.Equal(t, int64(1), stored)
}
