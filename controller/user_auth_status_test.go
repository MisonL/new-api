package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type statusRegisterAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		RegisterEnabled         bool `json:"register_enabled"`
		PasswordRegisterEnabled bool `json:"password_register_enabled"`
	} `json:"data"`
}

type userDeleteAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func TestGetStatusExposesRegisterSwitches(t *testing.T) {
	prevRegisterEnabled := common.RegisterEnabled
	prevPasswordRegisterEnabled := common.PasswordRegisterEnabled
	common.RegisterEnabled = false
	common.PasswordRegisterEnabled = false
	t.Cleanup(func() {
		common.RegisterEnabled = prevRegisterEnabled
		common.PasswordRegisterEnabled = prevPasswordRegisterEnabled
	})

	customOAuthStatusCache.Lock()
	prevExpiresAt := customOAuthStatusCache.expiresAt
	prevPayload := append([]customOAuthStatusInfo(nil), customOAuthStatusCache.payload...)
	prevRefreshing := customOAuthStatusCache.refreshing
	prevRefreshingGeneration := customOAuthStatusCache.refreshingGeneration
	customOAuthStatusCache.expiresAt = time.Now().Add(time.Minute)
	customOAuthStatusCache.payload = make([]customOAuthStatusInfo, 0)
	customOAuthStatusCache.refreshing = false
	customOAuthStatusCache.refreshingGeneration = 0
	customOAuthStatusCache.Unlock()
	t.Cleanup(func() {
		customOAuthStatusCache.Lock()
		customOAuthStatusCache.expiresAt = prevExpiresAt
		customOAuthStatusCache.payload = prevPayload
		customOAuthStatusCache.refreshing = prevRefreshing
		customOAuthStatusCache.refreshingGeneration = prevRefreshingGeneration
		customOAuthStatusCache.Unlock()
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

	GetStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response statusRegisterAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.False(t, response.Data.RegisterEnabled)
	require.False(t, response.Data.PasswordRegisterEnabled)
}

func TestDeleteUserReturnsSuccessAfterHardDelete(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	targetUser := createDeleteUserTarget(t, "delete-target")
	recorder := performDeleteUser(t, targetUser.Id)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response userDeleteAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)

	var count int64
	require.NoError(t, model.DB.Unscoped().Model(&model.User{}).Where("id = ?", targetUser.Id).Count(&count).Error)
	require.Zero(t, count)
}

func TestDeleteUserReturnsErrorWhenHardDeleteFails(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	targetUser := createDeleteUserTarget(t, "delete-fail-target")
	expectedErr := errors.New("forced hard delete failure")
	require.NoError(t, model.DB.Callback().Delete().Before("gorm:delete").Register("test:force_delete_error", func(db *gorm.DB) {
		db.AddError(expectedErr)
	}))

	recorder := performDeleteUser(t, targetUser.Id)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response userDeleteAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.True(t, strings.Contains(response.Message, expectedErr.Error()))

	_, err := model.GetUserById(targetUser.Id, false)
	require.NoError(t, err)
}

func createDeleteUserTarget(t *testing.T, username string) *model.User {
	t.Helper()

	user := &model.User{
		Username:    username,
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, model.DB.Create(user).Error)
	return user
}

func performDeleteUser(t *testing.T, userID int) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	idParam := strconv.Itoa(userID)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/user/"+idParam, nil)
	ctx.Params = gin.Params{{Key: "id", Value: idParam}}
	ctx.Set("role", common.RoleRootUser)

	DeleteUser(ctx)
	return recorder
}
