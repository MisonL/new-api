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
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func setupChannelControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousDB := model.DB
	previousLogDB := model.LOG_DB

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		model.DB = previousDB
		model.LOG_DB = previousLogDB

		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func newChannelControllerContext(t *testing.T, method string, target string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	payload, err := common.Marshal(body)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx, recorder
}

func decodeChannelAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) channelAPIResponse {
	t.Helper()

	var response channelAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func marshalChannelOtherSettingsForTest(t *testing.T, settings dto.ChannelOtherSettings) string {
	t.Helper()

	raw, err := common.Marshal(settings)
	require.NoError(t, err)
	return string(raw)
}

func seedChannelForHeaderProfileTest(t *testing.T) *model.Channel {
	t.Helper()

	channel := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-test-channel",
		Status: common.ChannelStatusEnabled,
		Name:   "header-profile-test-channel",
		Group:  "default",
		Models: "gpt-4o-mini",
	}
	require.NoError(t, model.DB.Create(channel).Error)
	return channel
}

func TestUpdateChannelPersistsHeaderProfileStrategy(t *testing.T) {
	setupChannelControllerTestDB(t)
	channel := seedChannelForHeaderProfileTest(t)

	ctx, recorder := newChannelControllerContext(t, http.MethodPut, fmt.Sprintf("/api/channel/%d", channel.Id), map[string]any{
		"id":     channel.Id,
		"type":   channel.Type,
		"key":    channel.Key,
		"status": channel.Status,
		"name":   channel.Name,
		"group":  channel.Group,
		"models": channel.Models,
		"settings": marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
			HeaderProfileStrategy: &dto.HeaderProfileStrategy{
				Enabled: true,
				Mode:    dto.HeaderProfileModeRoundRobin,
				SelectedProfileIDs: []string{
					"profile-a",
					"profile-b",
				},
				Profiles: []dto.HeaderProfile{
					{ID: "profile-a", Headers: map[string]string{"User-Agent": "A/1.0"}},
					{ID: "profile-b", Headers: map[string]string{"User-Agent": "B/1.0"}},
				},
			},
		}),
	})

	UpdateChannel(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	loaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)

	strategy := loaded.GetOtherSettings().HeaderProfileStrategy
	require.NotNil(t, strategy)
	require.True(t, strategy.Enabled)
	require.Equal(t, dto.HeaderProfileModeRoundRobin, strategy.Mode)
	require.Equal(t, []string{"profile-a", "profile-b"}, strategy.SelectedProfileIDs)
	require.Len(t, strategy.Profiles, 2)
	require.Equal(t, "A/1.0", strategy.Profiles[0].Headers["User-Agent"])
}

func TestUpdateChannelAllowsBuiltinHeaderProfileWithoutSnapshot(t *testing.T) {
	setupChannelControllerTestDB(t)
	channel := seedChannelForHeaderProfileTest(t)

	ctx, recorder := newChannelControllerContext(t, http.MethodPut, fmt.Sprintf("/api/channel/%d", channel.Id), map[string]any{
		"id":     channel.Id,
		"type":   channel.Type,
		"key":    channel.Key,
		"status": channel.Status,
		"name":   channel.Name,
		"group":  channel.Group,
		"models": channel.Models,
		"settings": marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
			HeaderProfileStrategy: &dto.HeaderProfileStrategy{
				Enabled:            true,
				Mode:               dto.HeaderProfileModeFixed,
				SelectedProfileIDs: []string{"codex-cli"},
			},
		}),
	})

	UpdateChannel(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	loaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, []string{"codex-cli"}, loaded.GetOtherSettings().HeaderProfileStrategy.SelectedProfileIDs)
}

func TestUpdateChannelRejectsMissingCustomHeaderProfileSnapshot(t *testing.T) {
	setupChannelControllerTestDB(t)
	channel := seedChannelForHeaderProfileTest(t)

	ctx, recorder := newChannelControllerContext(t, http.MethodPut, fmt.Sprintf("/api/channel/%d", channel.Id), map[string]any{
		"id":     channel.Id,
		"type":   channel.Type,
		"key":    channel.Key,
		"status": channel.Status,
		"name":   channel.Name,
		"group":  channel.Group,
		"models": channel.Models,
		"settings": marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
			HeaderProfileStrategy: &dto.HeaderProfileStrategy{
				Enabled:            true,
				Mode:               dto.HeaderProfileModeFixed,
				SelectedProfileIDs: []string{"profile-a"},
			},
		}),
	})

	UpdateChannel(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "缺少快照")
}

func TestUpdateChannelRejectsFixedModeWithMultipleProfiles(t *testing.T) {
	setupChannelControllerTestDB(t)
	channel := seedChannelForHeaderProfileTest(t)

	ctx, recorder := newChannelControllerContext(t, http.MethodPut, fmt.Sprintf("/api/channel/%d", channel.Id), map[string]any{
		"id":     channel.Id,
		"type":   channel.Type,
		"key":    channel.Key,
		"status": channel.Status,
		"name":   channel.Name,
		"group":  channel.Group,
		"models": channel.Models,
		"settings": marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
			HeaderProfileStrategy: &dto.HeaderProfileStrategy{
				Enabled: true,
				Mode:    dto.HeaderProfileModeFixed,
				SelectedProfileIDs: []string{
					"profile-a",
					"profile-b",
				},
			},
		}),
	})

	UpdateChannel(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "fixed")

	loaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Nil(t, loaded.GetOtherSettings().HeaderProfileStrategy)
}

func TestAddChannelRejectsRandomModeWithoutProfiles(t *testing.T) {
	setupChannelControllerTestDB(t)

	ctx, recorder := newChannelControllerContext(t, http.MethodPost, "/api/channel", map[string]any{
		"mode": "single",
		"channel": map[string]any{
			"type":   constant.ChannelTypeOpenAI,
			"key":    "sk-add-test-channel",
			"status": common.ChannelStatusEnabled,
			"name":   "add-header-profile-test-channel",
			"group":  "default",
			"models": "gpt-4o-mini",
			"settings": marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
				HeaderProfileStrategy: &dto.HeaderProfileStrategy{
					Enabled:            true,
					Mode:               dto.HeaderProfileModeRandom,
					SelectedProfileIDs: []string{},
				},
			}),
		},
	})

	AddChannel(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "random")

	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Count(&count).Error)
	require.EqualValues(t, 0, count)
}

func TestAddChannelRejectsInvalidHeaderPolicyMode(t *testing.T) {
	setupChannelControllerTestDB(t)

	ctx, recorder := newChannelControllerContext(t, http.MethodPost, "/api/channel", map[string]any{
		"mode": "single",
		"channel": map[string]any{
			"type":   constant.ChannelTypeOpenAI,
			"key":    "sk-add-invalid-header-policy-mode",
			"status": common.ChannelStatusEnabled,
			"name":   "add-invalid-header-policy-mode",
			"group":  "default",
			"models": "gpt-4o-mini",
			"settings": marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
				HeaderPolicyMode: dto.HeaderPolicyMode("broken"),
			}),
		},
	})

	AddChannel(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "请求头优先级模式不合法")

	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Count(&count).Error)
	require.EqualValues(t, 0, count)
}

func TestUpdateChannelRejectsInvalidUserAgentStrategy(t *testing.T) {
	setupChannelControllerTestDB(t)
	channel := seedChannelForHeaderProfileTest(t)

	ctx, recorder := newChannelControllerContext(t, http.MethodPut, fmt.Sprintf("/api/channel/%d", channel.Id), map[string]any{
		"id":     channel.Id,
		"type":   channel.Type,
		"key":    channel.Key,
		"status": channel.Status,
		"name":   channel.Name,
		"group":  channel.Group,
		"models": channel.Models,
		"settings": marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
			UserAgentStrategy: &dto.UserAgentStrategy{
				Enabled: true,
				Mode:    "broken",
				UserAgents: []string{
					"ua-1",
				},
			},
		}),
	})

	UpdateChannel(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "UA 策略模式不合法")

	loaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Nil(t, loaded.GetOtherSettings().UserAgentStrategy)
}

func TestUpdateChannelNormalizesHeaderPolicySettings(t *testing.T) {
	setupChannelControllerTestDB(t)
	channel := seedChannelForHeaderProfileTest(t)

	ctx, recorder := newChannelControllerContext(t, http.MethodPut, fmt.Sprintf("/api/channel/%d", channel.Id), map[string]any{
		"id":     channel.Id,
		"type":   channel.Type,
		"key":    channel.Key,
		"status": channel.Status,
		"name":   channel.Name,
		"group":  channel.Group,
		"models": channel.Models,
		"settings": marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
			HeaderPolicyMode: dto.HeaderPolicyMode(" prefer_tag "),
			UserAgentStrategy: &dto.UserAgentStrategy{
				Enabled:    false,
				Mode:       " random ",
				UserAgents: []string{" ua-1 ", "ua-1", "ua-2"},
			},
		}),
	})

	UpdateChannel(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	loaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	settings := loaded.GetOtherSettings()
	require.Equal(t, dto.HeaderPolicyModePreferTag, settings.HeaderPolicyMode)
	require.NotNil(t, settings.UserAgentStrategy)
	require.False(t, settings.UserAgentStrategy.Enabled)
	require.Equal(t, "random", settings.UserAgentStrategy.Mode)
	require.Equal(t, []string{"ua-1", "ua-2"}, settings.UserAgentStrategy.UserAgents)
}

func TestBuildFetchModelsHeadersNormalizesNonStringHeaderOverrideValues(t *testing.T) {
	channel := &model.Channel{
		Type: constant.ChannelTypeOpenAI,
		HeaderOverride: common.GetPointer(`{
			"*": true,
			"X-Debug": true,
			"X-Count": 123,
			"Authorization": "Bearer {api_key}"
		}`),
	}

	headers, err := buildFetchModelsHeaders(channel, "sk-test")
	require.NoError(t, err)
	require.Equal(t, "true", headers.Get("X-Debug"))
	require.Equal(t, "123", headers.Get("X-Count"))
	require.Equal(t, "Bearer sk-test", headers.Get("Authorization"))
}

func TestBuildFetchModelsHeadersAppliesHeaderProfileStrategy(t *testing.T) {
	channel := &model.Channel{
		Type: constant.ChannelTypeOpenAI,
		OtherSettings: marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
			HeaderProfileStrategy: &dto.HeaderProfileStrategy{
				Enabled:            true,
				Mode:               dto.HeaderProfileModeFixed,
				SelectedProfileIDs: []string{"codex-cli"},
			},
		}),
	}

	headers, err := buildFetchModelsHeaders(channel, "sk-test")
	require.NoError(t, err)
	require.Equal(t, "OpenAI Codex CLI/0.1", headers.Get("User-Agent"))
	require.Equal(t, "codex-cli", headers.Get("X-Client-Name"))
	require.Equal(t, "Bearer sk-test", headers.Get("Authorization"))
}

func TestBuildChannelRuntimeRequestHeadersSkipsClientHeaderPlaceholders(t *testing.T) {
	channel := &model.Channel{
		Type: constant.ChannelTypeOpenAI,
		HeaderOverride: common.GetPointer(`{
			"User-Agent": "{client_header:User-Agent}",
			"X-Api-Key": "{api_key}"
		}`),
	}

	headers, err := service.BuildChannelRuntimeRequestHeaders(channel, "sk-test", http.Header{})
	require.NoError(t, err)
	require.Empty(t, headers.Get("User-Agent"))
	require.Equal(t, "sk-test", headers.Get("X-Api-Key"))
}

func TestBuildChannelRuntimeRequestHeadersKeepsCustomAuthHeader(t *testing.T) {
	channel := &model.Channel{
		Type: constant.ChannelTypeAIProxy,
		OtherSettings: marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
			HeaderProfileStrategy: &dto.HeaderProfileStrategy{
				Enabled:            true,
				Mode:               dto.HeaderProfileModeFixed,
				SelectedProfileIDs: []string{"codex-cli"},
			},
		}),
	}
	baseHeaders := http.Header{}
	baseHeaders.Set("Api-Key", "sk-balance")

	headers, err := service.BuildChannelRuntimeRequestHeaders(channel, "sk-balance", baseHeaders)
	require.NoError(t, err)
	require.Equal(t, "sk-balance", headers.Get("Api-Key"))
	require.Equal(t, "OpenAI Codex CLI/0.1", headers.Get("User-Agent"))
}

func TestApplyRuntimeRequestHeadersSetsHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://origin.example/v1/models", nil)
	headers := http.Header{}
	headers.Set("Host", "upstream.example")
	headers.Set("User-Agent", "Codex-Test")
	headers.Add("X-Multi", "one")
	headers.Add("X-Multi", "two")

	service.ApplyRuntimeRequestHeaders(req, headers)

	require.Equal(t, "upstream.example", req.Host)
	require.Equal(t, "Codex-Test", req.Header.Get("User-Agent"))
	require.Equal(t, []string{"one", "two"}, req.Header.Values("X-Multi"))
}

func TestBuildFetchModelsHeadersSkipsNilHeaderOverrideValues(t *testing.T) {
	channel := &model.Channel{
		Type:           constant.ChannelTypeOpenAI,
		HeaderOverride: common.GetPointer(`{"X-Debug":null,"Authorization":"Bearer {api_key}"}`),
	}

	headers, err := buildFetchModelsHeaders(channel, "sk-test")
	require.NoError(t, err)
	require.Empty(t, headers.Get("X-Debug"))
	require.Equal(t, "Bearer sk-test", headers.Get("Authorization"))
}

func TestAddChannelRejectsHeaderProfileStrategyWithBlankProfileIDs(t *testing.T) {
	setupChannelControllerTestDB(t)

	ctx, recorder := newChannelControllerContext(t, http.MethodPost, "/api/channel", map[string]any{
		"mode": "single",
		"channel": map[string]any{
			"type":   constant.ChannelTypeOpenAI,
			"key":    "sk-add-blank-profile-id",
			"status": common.ChannelStatusEnabled,
			"name":   "add-header-profile-blank-id-test-channel",
			"group":  "default",
			"models": "gpt-4o-mini",
			"settings": marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
				HeaderProfileStrategy: &dto.HeaderProfileStrategy{
					Enabled: true,
					Mode:    dto.HeaderProfileModeRandom,
					SelectedProfileIDs: []string{
						"   ",
					},
				},
			}),
		},
	})

	AddChannel(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "random")

	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Count(&count).Error)
	require.EqualValues(t, 0, count)
}
