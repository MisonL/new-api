package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type setupAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func setupSetupControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	model.DB = db
	model.LOG_DB = db
	model.InitOptionMap()

	if err := db.AutoMigrate(&model.User{}, &model.Option{}, &model.Setup{}); err != nil {
		t.Fatalf("failed to migrate setup tables: %v", err)
	}

	previousSetup := constant.Setup
	previousSelfUseMode := operation_setting.SelfUseModeEnabled
	previousDemoSiteMode := operation_setting.DemoSiteEnabled
	previousServerAddress := system_setting.ServerAddress

	constant.Setup = false
	operation_setting.SelfUseModeEnabled = false
	operation_setting.DemoSiteEnabled = false
	system_setting.ServerAddress = ""

	t.Cleanup(func() {
		constant.Setup = previousSetup
		operation_setting.SelfUseModeEnabled = previousSelfUseMode
		operation_setting.DemoSiteEnabled = previousDemoSiteMode
		system_setting.ServerAddress = previousServerAddress

		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func newSetupContext(t *testing.T, body any) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	payload, err := common.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal setup request: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:13000/api/setup", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx, recorder
}

func decodeSetupResponse(t *testing.T, recorder *httptest.ResponseRecorder) setupAPIResponse {
	t.Helper()

	var response setupAPIResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode setup response: %v", err)
	}
	return response
}

func TestPostSetupRejectsBlankUsername(t *testing.T) {
	setupSetupControllerTestDB(t)

	ctx, recorder := newSetupContext(t, map[string]any{
		"username":        "   ",
		"password":        "DesktopTest123!",
		"confirmPassword": "DesktopTest123!",
	})

	PostSetup(ctx)

	response := decodeSetupResponse(t, recorder)
	if response.Success {
		t.Fatal("expected blank username setup to fail")
	}
	if response.Message != "请输入管理员用户名" {
		t.Fatalf("expected blank username message, got %q", response.Message)
	}
}

func TestPostSetupRejectsUsernameLongerThanMaxLength(t *testing.T) {
	setupSetupControllerTestDB(t)

	ctx, recorder := newSetupContext(t, map[string]any{
		"username":        strings.Repeat("a", model.UserNameMaxLength+1),
		"password":        "DesktopTest123!",
		"confirmPassword": "DesktopTest123!",
	})

	PostSetup(ctx)

	response := decodeSetupResponse(t, recorder)
	if response.Success {
		t.Fatal("expected overlong username setup to fail")
	}

	expectedMessage := fmt.Sprintf("用户名长度不能超过%d个字符", model.UserNameMaxLength)
	if response.Message != expectedMessage {
		t.Fatalf("expected overlong username message %q, got %q", expectedMessage, response.Message)
	}
}

func TestPostSetupAcceptsMaxUnicodeUsernameAndPersistsSetup(t *testing.T) {
	db := setupSetupControllerTestDB(t)
	maxLengthUsername := strings.Repeat("𠮷", model.UserNameMaxLength)

	ctx, recorder := newSetupContext(t, map[string]any{
		"username":           maxLengthUsername,
		"password":           "DesktopTest123!",
		"confirmPassword":    "DesktopTest123!",
		"SelfUseModeEnabled": true,
		"DemoSiteEnabled":    false,
	})

	PostSetup(ctx)

	response := decodeSetupResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected max-length username setup to succeed, got message: %s", response.Message)
	}

	var rootUser model.User
	if err := db.Where("role = ?", common.RoleRootUser).First(&rootUser).Error; err != nil {
		t.Fatalf("expected root user to be created: %v", err)
	}
	if rootUser.Username != maxLengthUsername {
		t.Fatalf("expected root username %q, got %q", maxLengthUsername, rootUser.Username)
	}

	var setupRecord model.Setup
	if err := db.First(&setupRecord).Error; err != nil {
		t.Fatalf("expected setup record to be created: %v", err)
	}

	var selfUseOption model.Option
	if err := db.Where("key = ?", "SelfUseModeEnabled").First(&selfUseOption).Error; err != nil {
		t.Fatalf("expected SelfUseModeEnabled option to be saved: %v", err)
	}
	if selfUseOption.Value != "true" {
		t.Fatalf("expected SelfUseModeEnabled to be true, got %q", selfUseOption.Value)
	}
}
