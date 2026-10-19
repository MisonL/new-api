package controller

import (
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// /api/user/、/api/user/search、/api/user/:id 都返回 model.User。管理员视图
// 里不能带其他用户的推送凭据（webhook_secret / gotify_token / bark_url）。
func TestAdminUserEndpointsStripNotificationSecrets(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := &model.User{
		Username:    "admin-view-target",
		Password:    "password123",
		DisplayName: "admin-view-target",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	user.SetSetting(dto.UserSetting{
		NotifyType:    dto.NotifyTypeWebhook,
		WebhookUrl:    "https://example.com/hook",
		WebhookSecret: "whsec_live_secret",
		GotifyToken:   "AAtoken_secret",
		BarkUrl:       "https://api.day.app/DEVICEKEY/hello",
	})
	require.NoError(t, model.DB.Create(user).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest("GET", "/api/user/"+strconv.Itoa(user.Id), nil)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(user.Id)}}

	GetUser(ctx)

	require.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, body, "whsec_live_secret")
	require.NotContains(t, body, "AAtoken_secret")
	require.NotContains(t, body, "DEVICEKEY")
	require.Contains(t, body, "https://example.com/hook", "non-secret settings stay visible for admin editing")

	// 数据库中的原始设置不受影响，计费与通知路径仍读到完整值。
	stored, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	require.Contains(t, stored.Setting, "whsec_live_secret")
}
