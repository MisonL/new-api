package controller

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func TestCreateAndListUserHeaderTemplates(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t, &model.UserHeaderTemplate{})

	ctx, recorder := newHeaderPolicyContext(t, http.MethodPost, "/api/user/header-templates", map[string]any{
		"name":    "My Template",
		"content": `{"user-agent":"agent-a","X-Debug":true}`,
	}, 1)
	CreateUserHeaderTemplate(ctx)

	response := decodeHeaderPolicyResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success, got %s", response.Message)
	}

	var stored model.UserHeaderTemplate
	if err := model.DB.First(&stored, "user_id = ? AND name = ?", 1, "My Template").Error; err != nil {
		t.Fatalf("failed to load stored template: %v", err)
	}
	if !strings.Contains(stored.Content, `"User-Agent":"agent-a"`) {
		t.Fatalf("expected canonicalized header content, got %s", stored.Content)
	}

	listCtx, listRecorder := newHeaderPolicyContext(t, http.MethodGet, "/api/user/header-templates", nil, 1)
	ListUserHeaderTemplates(listCtx)

	listResponse := decodeHeaderPolicyResponse(t, listRecorder)
	if !listResponse.Success {
		t.Fatalf("expected success, got %s", listResponse.Message)
	}
	if !strings.Contains(string(listResponse.Data), `"name":"My Template"`) {
		t.Fatalf("expected template in list response, got %s", string(listResponse.Data))
	}
}

func TestCreateUserHeaderTemplateRejectsBlankContent(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t, &model.UserHeaderTemplate{})

	ctx, recorder := newHeaderPolicyContext(t, http.MethodPost, "/api/user/header-templates", map[string]any{
		"name":    "Empty",
		"content": "   ",
	}, 1)
	CreateUserHeaderTemplate(ctx)

	response := decodeHeaderPolicyResponse(t, recorder)
	if response.Success {
		t.Fatal("expected request to fail")
	}
	if !strings.Contains(response.Message, "不能为空") {
		t.Fatalf("unexpected error message: %s", response.Message)
	}
}

func TestUpdateUserHeaderTemplateRejectsDuplicateName(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t, &model.UserHeaderTemplate{})

	first := &model.UserHeaderTemplate{UserId: 1, Name: "A", Content: `{"X-Test":"1"}`, CreatedAt: 1, UpdatedAt: 1}
	second := &model.UserHeaderTemplate{UserId: 1, Name: "B", Content: `{"X-Test":"2"}`, CreatedAt: 1, UpdatedAt: 1}
	if err := model.DB.Create(first).Error; err != nil {
		t.Fatalf("failed to seed first template: %v", err)
	}
	if err := model.DB.Create(second).Error; err != nil {
		t.Fatalf("failed to seed second template: %v", err)
	}

	ctx, recorder := newHeaderPolicyContext(t, http.MethodPut, "/api/user/header-templates/"+strconv.Itoa(second.Id), map[string]any{
		"name":    "A",
		"content": `{"X-Test":"3"}`,
	}, 1)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(second.Id)}}
	UpdateUserHeaderTemplate(ctx)

	response := decodeHeaderPolicyResponse(t, recorder)
	if response.Success {
		t.Fatal("expected update to fail")
	}
	if !strings.Contains(response.Message, "模板名称已存在") {
		t.Fatalf("unexpected error message: %s", response.Message)
	}
}

func TestDeleteUserHeaderTemplateIsScopedToCurrentUser(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t, &model.UserHeaderTemplate{})

	record := &model.UserHeaderTemplate{UserId: 2, Name: "B", Content: `{"X-Test":"2"}`, CreatedAt: 1, UpdatedAt: 1}
	if err := model.DB.Create(record).Error; err != nil {
		t.Fatalf("failed to seed template: %v", err)
	}

	ctx, recorder := newHeaderPolicyContext(t, http.MethodDelete, "/api/user/header-templates/"+strconv.Itoa(record.Id), nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(record.Id)}}
	DeleteUserHeaderTemplate(ctx)

	response := decodeHeaderPolicyResponse(t, recorder)
	if response.Success {
		t.Fatal("expected delete to fail")
	}
	if !strings.Contains(response.Message, "模板不存在") {
		t.Fatalf("unexpected error message: %s", response.Message)
	}
}
