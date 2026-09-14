package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFastTokenCountMetaForPricingKeepsOpaqueResponsesMaxOutputTokens(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
		Model:           "gpt-5.6-sol",
		ModelPresent:    true,
		InputPresent:    true,
		InputKind:       common.ResponsesJSONValueKindArray,
		MaxOutputTokens: &maxOutputTokens,
	})

	meta := fastTokenCountMetaForPricing(request)

	require.Equal(t, int(maxOutputTokens), meta.MaxTokens)
}

func TestFastTokenCountMetaForPricingKeepsResponsesCompactionMaxOutputTokens(t *testing.T) {
	maxOutputTokens := uint(4096)
	request := &dto.OpenAIResponsesCompactionRequest{
		Model:           "gpt-5.6-sol",
		MaxOutputTokens: &maxOutputTokens,
	}

	meta := fastTokenCountMetaForPricing(request)

	require.Equal(t, int(maxOutputTokens), meta.MaxTokens)
}

func TestRelayDefersPayloadAuditUntilOpaqueResponsesReplayDetection(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		relayFormat types.RelayFormat
		body        string
	}{
		{
			name:        "ordinary responses encrypted reasoning",
			path:        "/v1/responses",
			relayFormat: types.RelayFormatOpenAIResponses,
			body:        `{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]}`,
		},
		{
			name:        "responses compact encrypted content",
			path:        "/v1/responses/compact",
			relayFormat: types.RelayFormatOpenAIResponsesCompaction,
			body:        `{"model":"gpt-5.6-sol","input":[{"type":"compaction","encrypted_content":"opaque-compact-state"}]}`,
		},
		{
			name:        "non string encrypted content",
			path:        "/v1/responses",
			relayFormat: types.RelayFormatOpenAIResponses,
			body:        `{"model":"gpt-5.6-sol","input":[{"type":"future_item","state":{"encrypted_content":{"version":2}}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupChannelControllerTestDB(t)
			previousCountToken := constant.CountToken
			previousErrorLogEnabled := constant.ErrorLogEnabled
			constant.CountToken = false
			constant.ErrorLogEnabled = false
			t.Cleanup(func() {
				constant.CountToken = previousCountToken
				constant.ErrorLogEnabled = previousErrorLogEnabled
			})

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-5.6-sol")
			common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{
				AcceptUnsetRatioModel:    true,
				BillingPreference:        "wallet_only",
				RecordRequestContentLog:  true,
				RecordResponseContentLog: true,
			})

			Relay(ctx, tt.relayFormat)

			_, requestAuditInstalled := ctx.Get(string(constant.ContextKeyLogRequestPayload))
			_, responseAuditInstalled := ctx.Get(string(constant.ContextKeyLogResponsePayload))
			require.False(t, requestAuditInstalled)
			require.False(t, responseAuditInstalled)
			require.True(t, relaycommon.IsResponsesOpaqueEncryptedReplay(ctx))
		})
	}
}

func TestDetectAndMarkOpaqueResponsesReplay(t *testing.T) {
	tests := []struct {
		name       string
		request    dto.Request
		setup      func(t *testing.T, c *gin.Context) dto.Request
		wantOpaque bool
		wantErr    string
	}{
		{
			name: "opaque request metadata",
			request: dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
				Model:                     "gpt-5.6-sol",
				ModelPresent:              true,
				InputPresent:              true,
				InputKind:                 common.ResponsesJSONValueKindArray,
				HasOpaqueEncryptedContent: true,
			}),
			wantOpaque: true,
		},
		{
			name: "parsed reasoning encrypted content",
			request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]`),
			},
			wantOpaque: true,
		},
		{
			name: "compaction encrypted content",
			request: &dto.OpenAIResponsesCompactionRequest{
				Model: "gpt-5.6-sol",
				Input: common.RawMessage(`[{"type":"compaction","encrypted_content":"opaque-compact-state"}]`),
			},
			wantOpaque: true,
		},
		{
			name: "nested non string encrypted content",
			request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: common.RawMessage(`[{"type":"future_item","state":{"encrypted_content":{"version":2}}}]`),
			},
			wantOpaque: true,
		},
		{
			name: "visible input",
			request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]`),
			},
			wantOpaque: false,
		},
		{
			name: "stored local native opaque compact marker",
			request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
			},
			setup: func(t *testing.T, c *gin.Context) dto.Request {
				db := setupChannelControllerTestDB(t)
				require.NoError(t, db.AutoMigrate(&model.Option{}, &model.SyntheticCompactStateRecord{}))
				state, err := service.StoreNativeOpaqueCompactState(
					c.Request.Context(),
					service.SyntheticCompactStateScope{UserID: 1, TokenID: 1, Group: "default", Model: "gpt-5.6-sol"},
					"gpt-5.6-sol",
					"resp_native_compact",
					"opaque-compact-state",
					0,
				)
				require.NoError(t, err)
				return &dto.OpenAIResponsesRequest{
					Model: "gpt-5.6-sol",
					Input: common.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:` + state.ID + `"}]`),
				}
			},
			wantOpaque: true,
		},
		{
			name: "malformed input",
			request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":`),
			},
			wantErr: "parse responses input for encrypted content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			request := tt.request
			if tt.setup != nil {
				request = tt.setup(t, ctx)
			}

			got, err := detectAndMarkOpaqueResponsesReplay(ctx, request)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantOpaque, got)
			require.Equal(t, tt.wantOpaque, common.GetContextKeyBool(ctx, constant.ContextKeyResponsesOpaqueEncryptedReplay))
			require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
			require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
		})
	}
}

func TestGetChannelSkipsInitialOpaqueReplayChannelWithBodyParamOverride(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	incompatible, compatible := seedRemoteCompactionRouteChannels(t, db)
	incompatible.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
	})
	paramOverride := `{"operations":[{"mode":"set","path":"instructions","value":"must not rewrite opaque replay"}]}`
	incompatible.ParamOverride = &paramOverride
	require.NoError(t, db.Save(incompatible).Error)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, incompatible, "gpt-5.5"))

	channel, err := getChannel(ctx, opaqueReplayRouteInfoForTest(), &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, compatible.Id, channel.Id)
	require.Equal(t, []string{strconv.Itoa(incompatible.Id)}, ctx.GetStringSlice("use_channel"))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: incompatible.Id,
		Reason:    responsesRoutingChannelSkipReasonOpaqueReplayParamOverride,
	}})
}

func TestGetChannelSkipsInitialOpaqueReplayChannelWithNonStaticHeaderOnlyParamOverride(t *testing.T) {
	tests := []struct {
		name          string
		paramOverride string
		newRouteInfo  func() *relaycommon.RelayInfo
	}{
		{
			name:          "legacy override",
			paramOverride: `{"instructions":"must not rewrite opaque replay"}`,
			newRouteInfo:  opaqueReplayRouteInfoForTest,
		},
		{
			name:          "conditional header operation",
			paramOverride: `{"operations":[{"mode":"set_header","path":"X-Upstream-Client","value":"codex","conditions":[{"path":"model","mode":"full","value":"gpt-5.5"}]}]}`,
			newRouteInfo:  opaqueReplayRouteInfoForTest,
		},
		{
			name:          "parsed encrypted request legacy override",
			paramOverride: `{"instructions":"must not rewrite encrypted replay"}`,
			newRouteInfo:  parsedEncryptedResponsesRouteInfoForTest,
		},
		{
			name:          "parsed encrypted request conditional header operation",
			paramOverride: `{"operations":[{"mode":"set_header","path":"X-Upstream-Client","value":"codex","conditions":[{"path":"model","mode":"full","value":"gpt-5.5"}]}]}`,
			newRouteInfo:  parsedEncryptedResponsesRouteInfoForTest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupChannelControllerTestDB(t)
			incompatible, compatible := seedRemoteCompactionRouteChannels(t, db)
			incompatible.SetOtherSettings(dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
			})
			incompatible.ParamOverride = &tt.paramOverride
			require.NoError(t, db.Save(incompatible).Error)

			ctx, _ := gin.CreateTestContext(nil)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, incompatible, "gpt-5.5"))

			channel, err := getChannel(ctx, tt.newRouteInfo(), &service.RetryParam{
				Ctx:        ctx,
				TokenGroup: "default",
				ModelName:  "gpt-5.5",
				Retry:      common.GetPointer(0),
			})

			require.Nil(t, err)
			require.NotNil(t, channel)
			require.Equal(t, compatible.Id, channel.Id)
			require.Equal(t, []string{strconv.Itoa(incompatible.Id)}, ctx.GetStringSlice("use_channel"))
			requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
				ChannelID: incompatible.Id,
				Reason:    responsesRoutingChannelSkipReasonOpaqueReplayParamOverride,
			}})
		})
	}
}

func TestGetChannelKeepsInitialOpaqueReplayChannelWithHeaderOnlyParamOverride(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	compatible, _ := seedRemoteCompactionRouteChannels(t, db)
	compatible.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
	})
	paramOverride := `{"operations":[{"mode":"pass_headers","value":["Originator"]}]}`
	compatible.ParamOverride = &paramOverride
	require.NoError(t, db.Save(compatible).Error)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, compatible, "gpt-5.5"))

	channel, err := getChannel(ctx, opaqueReplayRouteInfoForTest(), &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, compatible.Id, channel.Id)
	require.Empty(t, ctx.GetStringSlice("use_channel"))
	_, skipped := common.GetContextKeyType[[]dto.ResponsesRoutingChannelSkip](ctx, constant.ContextKeyResponsesRoutingChannelSkips)
	require.False(t, skipped)
}

func TestOpaqueResponsesReplayDoesNotUseConditionalResponsesToChatRule(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "only-when-tool-bridge-required",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{205},
			ModelPatterns:  []string{`^gpt-5\\.5$`},
			Options: &model_setting.ProtocolConversionOptions{
				OnlyWhenToolBridgeRequired: true,
			},
		}},
	}
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	channel := &model.Channel{Id: 205, Type: constant.ChannelTypeOpenAI}

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, opaqueReplayRouteInfoForTest(), channel)

	require.NoError(t, err)
	require.False(t, skip)
	_, recorded := common.GetContextKeyType[[]dto.ResponsesRoutingChannelSkip](ctx, constant.ContextKeyResponsesRoutingChannelSkips)
	require.False(t, recorded)
}

func TestGetChannelReturns503WhenAllOpaqueReplayChannelsHaveBodyParamOverride(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	first, second := seedRemoteCompactionRouteChannels(t, db)
	for _, channel := range []*model.Channel{first, second} {
		channel.SetOtherSettings(dto.ChannelOtherSettings{
			ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
		})
		paramOverride := `{"operations":[{"mode":"set","path":"instructions","value":"must not rewrite opaque replay"}]}`
		channel.ParamOverride = &paramOverride
		require.NoError(t, db.Save(channel).Error)
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, first, "gpt-5.5"))

	channel, err := getChannel(ctx, opaqueReplayRouteInfoForTest(), &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Contains(t, err.Error(), "param override")
	require.ErrorIs(t, err, relaycommon.ErrResponsesOpaqueReplayParamOverrideUnsupported)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{
		{ChannelID: first.Id, Reason: responsesRoutingChannelSkipReasonOpaqueReplayParamOverride},
		{ChannelID: second.Id, Reason: responsesRoutingChannelSkipReasonOpaqueReplayParamOverride},
	})
}

func TestGetChannelSkipsInitialParsedEncryptedResponsesChannelWithBodyParamOverride(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	incompatible, compatible := seedRemoteCompactionRouteChannels(t, db)
	incompatible.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
	})
	paramOverride := `{"operations":[{"mode":"set","path":"instructions","value":"must not rewrite encrypted replay"}]}`
	incompatible.ParamOverride = &paramOverride
	require.NoError(t, db.Save(incompatible).Error)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, incompatible, "gpt-5.5"))

	channel, err := getChannel(ctx, parsedEncryptedResponsesRouteInfoForTest(), &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, compatible.Id, channel.Id)
	require.Equal(t, []string{strconv.Itoa(incompatible.Id)}, ctx.GetStringSlice("use_channel"))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: incompatible.Id,
		Reason:    responsesRoutingChannelSkipReasonOpaqueReplayParamOverride,
	}})
}

func TestGetChannelKeepsInitialParsedEncryptedResponsesChannelWithHeaderOnlyParamOverride(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	compatible, _ := seedRemoteCompactionRouteChannels(t, db)
	compatible.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
	})
	paramOverride := `{"operations":[{"mode":"pass_headers","value":["Originator"]}]}`
	compatible.ParamOverride = &paramOverride
	require.NoError(t, db.Save(compatible).Error)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, compatible, "gpt-5.5"))

	channel, err := getChannel(ctx, parsedEncryptedResponsesRouteInfoForTest(), &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, compatible.Id, channel.Id)
	require.Empty(t, ctx.GetStringSlice("use_channel"))
	_, skipped := common.GetContextKeyType[[]dto.ResponsesRoutingChannelSkip](ctx, constant.ContextKeyResponsesRoutingChannelSkips)
	require.False(t, skipped)
}

func TestGetChannelReturns503WhenAllParsedEncryptedResponsesChannelsHaveBodyParamOverride(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	first, second := seedRemoteCompactionRouteChannels(t, db)
	for _, channel := range []*model.Channel{first, second} {
		channel.SetOtherSettings(dto.ChannelOtherSettings{
			ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
		})
		paramOverride := `{"operations":[{"mode":"set","path":"instructions","value":"must not rewrite encrypted replay"}]}`
		channel.ParamOverride = &paramOverride
		require.NoError(t, db.Save(channel).Error)
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, first, "gpt-5.5"))

	channel, err := getChannel(ctx, parsedEncryptedResponsesRouteInfoForTest(), &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Contains(t, err.Error(), "param override")
	require.ErrorIs(t, err, relaycommon.ErrResponsesOpaqueReplayParamOverrideUnsupported)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{
		{ChannelID: first.Id, Reason: responsesRoutingChannelSkipReasonOpaqueReplayParamOverride},
		{ChannelID: second.Id, Reason: responsesRoutingChannelSkipReasonOpaqueReplayParamOverride},
	})
}

func TestGetChannelReturns503ForOpaqueResponsesCompactWithoutNativeCompact(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	first, second := seedRemoteCompactionRouteChannels(t, db)
	for _, channel := range []*model.Channel{first, second} {
		channel.SetOtherSettings(dto.ChannelOtherSettings{
			ResponsesCompactMode:     dto.ResponsesCompactModeSynthetic,
			ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
		})
		require.NoError(t, db.Save(channel).Error)
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, first, "gpt-5.5"))

	channel, err := getChannel(ctx, opaqueReplayCompactRouteInfoForTest(), &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.ErrorIs(t, err, errResponsesNativeCompactionUnsupported)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{
		{ChannelID: first.Id, Reason: responsesRoutingChannelSkipReasonUnsupportedCompaction},
		{ChannelID: second.Id, Reason: responsesRoutingChannelSkipReasonUnsupportedCompaction},
	})
}

func opaqueReplayRouteInfoForTest() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
			Model:                     "gpt-5.5",
			ModelPresent:              true,
			InputPresent:              true,
			InputKind:                 common.ResponsesJSONValueKindArray,
			HasOpaqueEncryptedContent: true,
		}),
	}
}

func opaqueReplayCompactRouteInfoForTest() *relaycommon.RelayInfo {
	info := opaqueReplayRouteInfoForTest()
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	return info
}

func parsedEncryptedResponsesRouteInfoForTest() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]`),
		},
	}
}
