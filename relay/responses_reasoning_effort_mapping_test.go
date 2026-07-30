package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	openairelay "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupResponsesReasoningEffortMappingTestDB(t *testing.T) {
	t.Helper()

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousBatchUpdateEnabled := common.BatchUpdateEnabled
	previousLogConsumeEnabled := common.LogConsumeEnabled
	previousDataExportEnabled := common.DataExportEnabled

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Log{}))
	require.NoError(t, db.Create(&model.User{
		Id:       7,
		Username: "responses-reasoning-test",
		Password: "test-password",
		Status:   common.UserStatusEnabled,
		Quota:    100000,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     206,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Name:   "responses-reasoning-test",
		Status: common.ChannelStatusEnabled,
	}).Error)
	model.DB = db
	model.LOG_DB = db

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		common.BatchUpdateEnabled = previousBatchUpdateEnabled
		common.LogConsumeEnabled = previousLogConsumeEnabled
		common.DataExportEnabled = previousDataExportEnabled
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestBuildOpenAIResponsesRequestBodyPreservesMappedLogicalEffortContract(t *testing.T) {
	tests := []struct {
		name           string
		originModel    string
		upstreamModel  string
		explicitEffort string
		wantEffort     string
	}{
		{
			name:          "suffix applies effort after mapping",
			originModel:   "gpt-5.6-sol-high",
			upstreamModel: "provider/gpt-5.6-sol",
			wantEffort:    "high",
		},
		{
			name:           "suffix retains existing override precedence",
			originModel:    "gpt-5.6-sol-low",
			upstreamModel:  "provider/gpt-5.6-sol",
			explicitEffort: "high",
			wantEffort:     "low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rawBody := []byte(`{"model":"` + tt.originModel + `","input":"continue"}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
			c.Request.Header.Set("Content-Type", "application/json")
			storage, err := common.CreateBodyStorage(rawBody)
			require.NoError(t, err)
			t.Cleanup(func() { _ = storage.Close() })
			c.Set(common.KeyBodyStorage, storage)

			request := &dto.OpenAIResponsesRequest{}
			require.NoError(t, common.Unmarshal(rawBody, request))
			if tt.explicitEffort != "" {
				request.Reasoning = &dto.Reasoning{Effort: tt.explicitEffort}
			}
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeResponses,
				OriginModelName: tt.originModel,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:     206,
					ChannelType:   constant.ChannelTypeOpenAI,
					IsModelMapped: true,
					ChannelSetting: dto.ChannelSettings{
						PassThroughBodyEnabled: true,
					},
					ChannelOtherSettings: dto.ChannelOtherSettings{
						ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
					},
				},
			}
			c.Set("model_mapping", `{"`+tt.originModel+`":"`+tt.upstreamModel+`"}`)
			require.NoError(t, relayhelper.ModelMappedHelper(c, info, request))
			require.Equal(t, tt.upstreamModel, request.Model)

			body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

			require.Nil(t, apiErr)
			bodyBytes, err := io.ReadAll(body)
			require.NoError(t, err)
			var bodyMap map[string]json.RawMessage
			require.NoError(t, common.Unmarshal(bodyBytes, &bodyMap))
			var upstreamModel string
			require.NoError(t, common.Unmarshal(bodyMap["model"], &upstreamModel))
			require.Equal(t, tt.upstreamModel, upstreamModel)
			var reasoning map[string]json.RawMessage
			require.NoError(t, common.Unmarshal(bodyMap["reasoning"], &reasoning))
			require.JSONEq(t, `"`+tt.wantEffort+`"`, string(reasoning["effort"]))
			require.Equal(t, tt.upstreamModel, converted.Model)
			require.NotNil(t, converted.Reasoning)
			require.Equal(t, tt.wantEffort, converted.Reasoning.Effort)
			require.Equal(t, tt.wantEffort, info.ReasoningEffort)
		})
	}
}

func TestResponsesHelperMapsLogicalEffortBeforeResponsesToChat(t *testing.T) {
	setupResponsesReasoningEffortMappingTestDB(t)
	service.InitHttpClient()

	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	previousPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
		settings.PassThroughRequestEnabled = previousPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "mapped-reasoning-effort-chat-bridge",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{206},
			ModelPatterns:  []string{"gpt-5.6-sol-high"},
		}},
	}
	var upstreamRequest dto.GeneralOpenAIRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.NoError(t, common.DecodeJson(r.Body, &upstreamRequest))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "id":"chatcmpl_mapped_reasoning_effort",
  "object":"chat.completion",
  "created":1710000000,
  "model":"provider/gpt-5.6-sol",
  "choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}
}`))
	}))
	t.Cleanup(upstream.Close)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(`{"model":"gpt-5.6-sol-high","input":"continue"}`)))
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 206)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.6-sol-high")
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"gpt-5.6-sol-high":"provider/gpt-5.6-sol"}`)
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol-high",
		UserId:          7,
		StartTime:       time.Now(),
		UsingGroup:      "default",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol-high",
			Input: json.RawMessage(`"continue"`),
		},
	}

	apiErr := ResponsesHelper(c, info)

	require.Nil(t, apiErr)
	require.Equal(t, "high", info.ReasoningEffort)
	require.Equal(t, "provider/gpt-5.6-sol", upstreamRequest.Model)
	require.Equal(t, "high", upstreamRequest.ReasoningEffort)
}

func TestResponsesHelperDoesNotInjectLogicalEffortForOpaqueReplay(t *testing.T) {
	setupResponsesReasoningEffortMappingTestDB(t)
	service.InitHttpClient()

	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	previousPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
		settings.PassThroughRequestEnabled = previousPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}

	rawBody := []byte(`{"model":"gpt-5.6-sol-high","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	var upstreamBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/responses", r.URL.Path)
		var err error
		upstreamBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_opaque_replay","object":"response","created_at":1710000000,"model":"provider/gpt-5.6-sol","output":[{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`))
	}))
	t.Cleanup(upstream.Close)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 206)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.6-sol-high")
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"gpt-5.6-sol-high":"provider/gpt-5.6-sol"}`)
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "gpt-5.6-sol-high",
		UserId:          7,
		StartTime:       time.Now(),
		UsingGroup:      "default",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol-high",
			Input: json.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]`),
		},
	}

	apiErr := ResponsesHelper(c, info)

	require.Nil(t, apiErr)
	require.Empty(t, info.ReasoningEffort)
	require.NotEmpty(t, upstreamBody)
	var bodyMap map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(upstreamBody, &bodyMap))
	var upstreamModel string
	require.NoError(t, common.Unmarshal(bodyMap["model"], &upstreamModel))
	require.Equal(t, "provider/gpt-5.6-sol", upstreamModel)
	_, hasTopLevelReasoning := bodyMap["reasoning"]
	require.False(t, hasTopLevelReasoning)
	expectedBody := []byte(`{"model":"provider/gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	require.Equal(t, expectedBody, upstreamBody)
}

func TestResponsesToChatCarriesMappedLogicalReasoningEffort(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model:     "provider/gpt-5.6-sol",
		Input:     json.RawMessage(`"continue"`),
		Reasoning: &dto.Reasoning{Effort: "high"},
	}

	chatRequest, _, err := service.ResponsesRequestToChatCompletionsRequestWithState(request, service.ResponsesChatCompatibilityOptions{})

	require.NoError(t, err)
	require.Equal(t, "provider/gpt-5.6-sol", chatRequest.Model)
	require.Equal(t, "high", chatRequest.ReasoningEffort)
}
