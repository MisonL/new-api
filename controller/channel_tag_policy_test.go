package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type headerPolicyAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func setupHeaderPolicyControllerTestDB(t *testing.T, tables ...interface{}) *gorm.DB {
	t.Helper()

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousDB := model.DB
	previousLogDB := model.LOG_DB

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

	if len(tables) > 0 {
		if err := db.AutoMigrate(tables...); err != nil {
			t.Fatalf("failed to migrate test tables: %v", err)
		}
	}

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		model.DB = previousDB
		model.LOG_DB = previousLogDB

		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func newHeaderPolicyContext(t *testing.T, method string, target string, body any, userID int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var requestBody *bytes.Reader
	if body != nil {
		payload, err := common.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(payload)
	} else {
		requestBody = bytes.NewReader(nil)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, requestBody)
	if body != nil {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	if userID > 0 {
		ctx.Set("id", userID)
	}

	return ctx, recorder
}

func decodeHeaderPolicyResponse(t *testing.T, recorder *httptest.ResponseRecorder) headerPolicyAPIResponse {
	t.Helper()

	var response headerPolicyAPIResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode api response: %v", err)
	}
	return response
}

func TestUpsertAndGetTagHeaderPolicy(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t, &model.TagRequestHeaderPolicy{})

	body := map[string]any{
		"tag":                         "tag-a",
		"header_override":             `{"user-agent":"agent-a","X-Debug":true}`,
		"header_policy_mode":          "prefer_tag",
		"override_header_user_agent":  true,
		"ua_strategy": dto.UserAgentStrategy{
			Enabled:    false,
			Mode:       " random ",
			UserAgents: []string{" ua-1 ", "ua-1", "ua-2"},
		},
	}

	ctx, recorder := newHeaderPolicyContext(t, http.MethodPut, "/api/channel/tag/policy", body, 0)
	UpsertTagHeaderPolicy(ctx)

	response := decodeHeaderPolicyResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success, got %s", response.Message)
	}

	var stored model.TagRequestHeaderPolicy
	if err := model.DB.First(&stored, "tag = ?", "tag-a").Error; err != nil {
		t.Fatalf("failed to load stored policy: %v", err)
	}
	if stored.HeaderPolicyMode != "prefer_tag" {
		t.Fatalf("unexpected header policy mode: %s", stored.HeaderPolicyMode)
	}
	if !stored.OverrideHeaderUserAgent {
		t.Fatal("expected override header user agent to be true")
	}
	if stored.HeaderOverride != `{"User-Agent":"agent-a","X-Debug":"true"}` && stored.HeaderOverride != `{"X-Debug":"true","User-Agent":"agent-a"}` {
		t.Fatalf("unexpected stored header override: %s", stored.HeaderOverride)
	}

	var storedStrategy dto.UserAgentStrategy
	if err := common.UnmarshalJsonStr(stored.UserAgentStrategyJSON, &storedStrategy); err != nil {
		t.Fatalf("failed to decode stored strategy: %v", err)
	}
	if storedStrategy.Enabled {
		t.Fatalf("expected stored strategy to remain disabled: %+v", storedStrategy)
	}
	if storedStrategy.Mode != "random" {
		t.Fatalf("expected normalized strategy mode, got %s", storedStrategy.Mode)
	}
	if strings.Join(storedStrategy.UserAgents, ",") != "ua-1,ua-2" {
		t.Fatalf("unexpected stored user agents: %+v", storedStrategy.UserAgents)
	}

	getCtx, getRecorder := newHeaderPolicyContext(t, http.MethodGet, "/api/channel/tag/policy?tag=tag-a", nil, 0)
	GetTagHeaderPolicy(getCtx)

	getResponse := decodeHeaderPolicyResponse(t, getRecorder)
	if !getResponse.Success {
		t.Fatalf("expected success, got %s", getResponse.Message)
	}
	if !strings.Contains(string(getResponse.Data), `"exists":true`) {
		t.Fatalf("expected exists=true response, got %s", string(getResponse.Data))
	}
	if !strings.Contains(string(getResponse.Data), `"header_policy_mode":"prefer_tag"`) {
		t.Fatalf("unexpected get response data: %s", string(getResponse.Data))
	}
	if !strings.Contains(string(getResponse.Data), `"mode":"random"`) {
		t.Fatalf("expected normalized strategy in response, got %s", string(getResponse.Data))
	}
}

func TestUpsertTagHeaderPolicyRejectsInvalidHeaderOverride(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t, &model.TagRequestHeaderPolicy{})

	ctx, recorder := newHeaderPolicyContext(t, http.MethodPut, "/api/channel/tag/policy", map[string]any{
		"tag":             "tag-a",
		"header_override": `{"Bad Header":"x"}`,
	}, 0)
	UpsertTagHeaderPolicy(ctx)

	response := decodeHeaderPolicyResponse(t, recorder)
	if response.Success {
		t.Fatal("expected request to fail")
	}
	if !strings.Contains(response.Message, "请求头名称不合法") {
		t.Fatalf("unexpected error message: %s", response.Message)
	}
}

func TestDeleteTagHeaderPolicyRemovesRecord(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t, &model.TagRequestHeaderPolicy{})

	record := &model.TagRequestHeaderPolicy{
		Tag:                     "tag-a",
		HeaderPolicyMode:        "prefer_tag",
		OverrideHeaderUserAgent: true,
	}
	if err := model.DB.Create(record).Error; err != nil {
		t.Fatalf("failed to seed policy: %v", err)
	}

	ctx, recorder := newHeaderPolicyContext(t, http.MethodDelete, "/api/channel/tag/policy?tag=tag-a", nil, 0)
	DeleteTagHeaderPolicy(ctx)

	response := decodeHeaderPolicyResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success, got %s", response.Message)
	}

	var count int64
	if err := model.DB.Model(&model.TagRequestHeaderPolicy{}).Where("tag = ?", "tag-a").Count(&count).Error; err != nil {
		t.Fatalf("failed to count policies: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected policy to be deleted, count=%d", count)
	}
}

func TestEditTagChannelsRejectsDeprecatedHeaderOverride(t *testing.T) {
	ctx, recorder := newHeaderPolicyContext(t, http.MethodPut, "/api/channel/tag", map[string]any{
		"tag":             "tag-a",
		"header_override": `{"User-Agent":"agent-a"}`,
	}, 0)
	EditTagChannels(ctx)

	response := decodeHeaderPolicyResponse(t, recorder)
	if response.Success {
		t.Fatal("expected request to fail")
	}
	if !strings.Contains(response.Message, "标签请求头策略") {
		t.Fatalf("unexpected error message: %s", response.Message)
	}
}
