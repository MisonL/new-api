package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type updateUserSettingTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func TestUpdateUserSettingPreservesUnrelatedSettings(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := &model.User{
		Username:    "setting-user",
		Password:    "password123",
		DisplayName: "setting-user",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	user.SetSetting(dto.UserSetting{
		NotifyType:            dto.NotifyTypeWebhook,
		QuotaWarningThreshold: 123456,
		WebhookUrl:            "https://example.com/hook",
		WebhookSecret:         "secret-keep",
		SidebarModules:        `{"console":{"enabled":true}}`,
		BillingPreference:     "subscription",
		Language:              "en",
	})
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	payload, err := common.Marshal(map[string]any{
		"notify_type":                    dto.NotifyTypeEmail,
		"quota_warning_threshold":        8888,
		"notification_email":             "notify@example.com",
		"accept_unset_model_ratio_model": true,
		"record_ip_log":                  true,
		"record_request_content_log":     true,
		"record_response_content_log":    false,
	})
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", user.Id)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/user/setting", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateUserSetting(ctx)

	var response updateUserSettingTestResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success, got %s", response.Message)
	}

	reloadedUser, err := model.GetUserById(user.Id, false)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	settings := reloadedUser.GetSetting()
	if settings.SidebarModules != `{"console":{"enabled":true}}` {
		t.Fatalf("expected sidebar modules to be preserved, got %s", settings.SidebarModules)
	}
	if settings.BillingPreference != "subscription" {
		t.Fatalf("expected billing preference to be preserved, got %s", settings.BillingPreference)
	}
	if settings.Language != "en" {
		t.Fatalf("expected language to be preserved, got %s", settings.Language)
	}
	if settings.NotifyType != dto.NotifyTypeEmail {
		t.Fatalf("expected notify type to be updated, got %s", settings.NotifyType)
	}
	if settings.NotificationEmail != "notify@example.com" {
		t.Fatalf("expected notification email to be updated, got %s", settings.NotificationEmail)
	}
	if settings.WebhookUrl != "" || settings.WebhookSecret != "" {
		t.Fatalf("expected webhook fields to be cleared, got url=%q secret=%q", settings.WebhookUrl, settings.WebhookSecret)
	}
	if !settings.RecordIpLog || !settings.RecordRequestContentLog || settings.RecordResponseContentLog {
		t.Fatalf("unexpected payload/ip log switches: %+v", settings)
	}
}

func TestUpdateUserSettingPreservesContentLogSettingsWhenFieldsOmitted(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := &model.User{
		Username:    "setting-user-omit",
		Password:    "password123",
		DisplayName: "setting-user-omit",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	user.SetSetting(dto.UserSetting{
		NotifyType:               dto.NotifyTypeWebhook,
		QuotaWarningThreshold:    123456,
		WebhookUrl:               "https://example.com/hook",
		WebhookSecret:            "secret-keep",
		RecordIpLog:              true,
		RecordRequestContentLog:  true,
		RecordResponseContentLog: true,
	})
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	payload, err := common.Marshal(map[string]any{
		"notify_type":                    dto.NotifyTypeWebhook,
		"quota_warning_threshold":        9999,
		"webhook_url":                    "https://example.com/new-hook",
		"webhook_secret":                 "",
		"accept_unset_model_ratio_model": false,
		"record_ip_log":                  false,
	})
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", user.Id)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/user/setting", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateUserSetting(ctx)

	var response updateUserSettingTestResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success, got %s", response.Message)
	}

	reloadedUser, err := model.GetUserById(user.Id, false)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	settings := reloadedUser.GetSetting()
	if !settings.RecordRequestContentLog || !settings.RecordResponseContentLog {
		t.Fatalf("expected omitted content log fields to be preserved, got %+v", settings)
	}
	if settings.WebhookSecret != "" {
		t.Fatalf("expected webhook secret to be cleared, got %q", settings.WebhookSecret)
	}
}
