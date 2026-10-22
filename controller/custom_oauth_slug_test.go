package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-gonic/gin"
)

// slug 的冲突检查必须在归一化之后进行。model.validateCustomOAuthProvider
// 会把 slug 转小写后落库，registry 也以小写为键；若检查用的是未归一化的
// 原始输入，"GitHub" 这类大小写变体就能绕过内置冲突检查，覆盖注册表里的
// 内置 "github"，使内置 GitHub 登录全局失效。删除该自定义提供商时还会把
// 内置 provider 一并从注册表删掉，直到进程重启才恢复。

func setupCustomOAuthSlugTestDB(t *testing.T) {
	t.Helper()
	setupCustomOAuthJWTControllerTestDB(t)
}

func seedCustomOAuthProvider(t *testing.T, slug string) *model.CustomOAuthProvider {
	t.Helper()
	provider := &model.CustomOAuthProvider{
		Name:                  "Slug Probe",
		Slug:                  slug,
		Kind:                  model.CustomOAuthProviderKindOAuthCode,
		Enabled:               true,
		ClientId:              "slug-probe-client",
		AuthorizationEndpoint: "https://issuer.example.com/oauth2/authorize",
		TokenEndpoint:         "https://issuer.example.com/oauth2/token",
		UserInfoEndpoint:      "https://issuer.example.com/oauth2/userinfo",
	}
	if err := model.CreateCustomOAuthProvider(provider); err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return provider
}

func callCreateCustomOAuthProvider(t *testing.T, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/custom-oauth-provider/", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	CreateCustomOAuthProvider(ctx)
	return recorder
}

// 内置 github 已注册在 registry 中，小写形式的冲突必须被拦。
func TestCreateCustomProviderRejectsBuiltinSlugLowercase(t *testing.T) {
	setupCustomOAuthSlugTestDB(t)

	recorder := callCreateCustomOAuthProvider(t, map[string]any{
		"name":                   "Fake GitHub",
		"slug":                   "github",
		"kind":                   model.CustomOAuthProviderKindOAuthCode,
		"enabled":                true,
		"client_id":              "c",
		"authorization_endpoint": "https://evil.example/a",
		"token_endpoint":         "https://evil.example/t",
		"user_info_endpoint":     "https://evil.example/u",
	})

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := common.UnmarshalJsonStr(recorder.Body.String(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Success {
		t.Fatalf("lowercase built-in slug 'github' must be rejected, got success")
	}
}

// 大小写变体 "GitHub" 落库后是 "github"，同一条冲突检查也必须拦。
func TestCreateCustomProviderRejectsBuiltinSlugCaseVariant(t *testing.T) {
	setupCustomOAuthSlugTestDB(t)

	recorder := callCreateCustomOAuthProvider(t, map[string]any{
		"name":                   "Fake GitHub",
		"slug":                   "GitHub",
		"kind":                   model.CustomOAuthProviderKindOAuthCode,
		"enabled":                true,
		"client_id":              "c",
		"authorization_endpoint": "https://evil.example/a",
		"token_endpoint":         "https://evil.example/t",
		"user_info_endpoint":     "https://evil.example/u",
	})

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := common.UnmarshalJsonStr(recorder.Body.String(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Success {
		t.Fatalf("case variant 'GitHub' normalizes to built-in 'github' and must be rejected, got success")
	}

	// 确认注册表里的内置 github 没有被覆盖成自定义 provider。
	if oauth.IsCustomProvider("github") {
		t.Fatalf("built-in github was replaced by a custom provider")
	}
	if !oauth.IsProviderRegistered("github") {
		t.Fatalf("built-in github disappeared from the registry")
	}
}

// 自定义 provider 之间的 slug 冲突同样按归一化后的值判定。
func TestCreateCustomProviderRejectsCaseVariantOfExistingCustomSlug(t *testing.T) {
	setupCustomOAuthSlugTestDB(t)
	seedCustomOAuthProvider(t, "acme-sso")

	recorder := callCreateCustomOAuthProvider(t, map[string]any{
		"name":                   "Acme SSO Duplicate",
		"slug":                   "Acme-SSO",
		"kind":                   model.CustomOAuthProviderKindOAuthCode,
		"enabled":                true,
		"client_id":              "c",
		"authorization_endpoint": "https://evil.example/a",
		"token_endpoint":         "https://evil.example/t",
		"user_info_endpoint":     "https://evil.example/u",
	})

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := common.UnmarshalJsonStr(recorder.Body.String(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Success {
		t.Fatalf("case variant of an existing custom slug must be rejected, got success")
	}
}

// 更新路径同样：把已有 provider 的 slug 改成内置名的大小写变体必须被拦。
func TestUpdateCustomProviderRejectsBuiltinSlugCaseVariant(t *testing.T) {
	setupCustomOAuthSlugTestDB(t)
	provider := seedCustomOAuthProvider(t, "acme-sso")

	payload, err := common.Marshal(map[string]any{"slug": "GitHub"})
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(provider.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/custom-oauth-provider/"+strconv.Itoa(provider.Id), bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateCustomOAuthProvider(ctx)

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := common.UnmarshalJsonStr(recorder.Body.String(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Success {
		t.Fatalf("update to case variant of built-in slug must be rejected, got success")
	}

	reloaded, err := model.GetCustomOAuthProviderById(provider.Id)
	if err != nil {
		t.Fatalf("failed to reload provider: %v", err)
	}
	if reloaded.Slug != "acme-sso" {
		t.Fatalf("slug must stay unchanged after a rejected update, got %q", reloaded.Slug)
	}
	if oauth.IsCustomProvider("github") {
		t.Fatalf("built-in github was replaced by a custom provider via update")
	}
}
