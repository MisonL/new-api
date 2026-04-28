package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseChannelTestOptionsRuntimeOffDisablesSubOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/test/1?runtime_config=false&header_config=true&proxy=true", nil)

	options := parseChannelTestOptions(ctx)

	require.False(t, options.UseRuntimeConfig)
	require.False(t, options.UseHeaderConfig)
	require.False(t, options.UseParamOverride)
	require.False(t, options.UseProxy)
	require.False(t, options.UseModelMapping)
}

func TestParseChannelTestOptionsParsesProtocolAndRequestParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/test/1?response_protocol=chat_completions&test_prompt=explain%20compact&max_tokens=64", nil)
	ctx.Request.Header.Set("User-Agent", "Codex/1.0")
	ctx.Request.Header.Set("X-Codex-Beta-Features", "compact-edit")
	ctx.Request.Header.Set("Cookie", "session=secret")
	ctx.Request.Header.Set("New-Api-User", "12")

	options := parseChannelTestOptions(ctx)

	require.Equal(t, channelTestResponseProtocolChatCompletions, options.ResponseProtocol)
	require.Equal(t, "explain compact", options.TestPrompt)
	require.NotNil(t, options.MaxTokens)
	require.Equal(t, uint(64), *options.MaxTokens)
	require.Equal(t, "Codex/1.0", options.SourceHeaders["User-Agent"])
	require.Equal(t, "compact-edit", options.SourceHeaders["X-Codex-Beta-Features"])
	_, hasCookie := options.SourceHeaders["Cookie"]
	require.False(t, hasCookie)
	_, hasUser := options.SourceHeaders["New-Api-User"]
	require.False(t, hasUser)
}

func TestBuildTestRequestAppliesResponsesPromptAndMaxTokens(t *testing.T) {
	maxTokens := uint(64)
	options := defaultChannelTestOptions()
	options.TestPrompt = "explain compact"
	options.MaxTokens = &maxTokens

	request := buildTestRequest("gpt-5.5", string(constant.EndpointTypeOpenAIResponse), nil, false, options)

	responsesReq, ok := request.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, `[{"role":"user","content":"explain compact"}]`, string(responsesReq.Input))
	require.NotNil(t, responsesReq.MaxOutputTokens)
	require.Equal(t, uint(64), *responsesReq.MaxOutputTokens)
}

func TestBuildTestRequestAppliesImageGenerationPrompt(t *testing.T) {
	options := defaultChannelTestOptions()
	options.TestPrompt = "draw a compact robot"

	request := buildTestRequest("gpt-image-2", string(constant.EndpointTypeImageGeneration), nil, false, options)

	imageReq, ok := request.(*dto.ImageRequest)
	require.True(t, ok)
	require.Equal(t, "gpt-image-2", imageReq.Model)
	require.Equal(t, "draw a compact robot", imageReq.Prompt)
	require.Equal(t, "1024x1024", imageReq.Size)
	require.NotNil(t, imageReq.N)
	require.Equal(t, uint(1), *imageReq.N)
}

func TestApplyChannelTestProtocolStrategyConvertsResponsesCompactToChat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	maxTokens := uint(64)
	options := defaultChannelTestOptions()
	options.ResponseProtocol = channelTestResponseProtocolChatCompletions
	options.MaxTokens = &maxTokens
	info := &relaycommon.RelayInfo{
		RelayMode:              relayconstant.RelayModeResponsesCompact,
		RequestURLPath:         "/v1/responses/compact",
		RelayFormat:            types.RelayFormatOpenAIResponsesCompaction,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAIResponsesCompaction},
	}
	request := &dto.OpenAIResponsesCompactionRequest{
		Model: "gpt-5.5-openai-compact",
		Input: json.RawMessage(`[{"role":"user","content":"summarize"}]`),
	}

	converted, err := applyChannelTestProtocolStrategy(ctx, info, request, options)

	require.NoError(t, err)
	chatReq, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Equal(t, "gpt-5.5-openai-compact", chatReq.Model)
	require.NotNil(t, chatReq.MaxTokens)
	require.Equal(t, uint(64), *chatReq.MaxTokens)
	require.Equal(t, relayconstant.RelayModeChatCompletions, info.RelayMode)
	require.Equal(t, "/v1/chat/completions", info.RequestURLPath)
	require.Equal(t, "/v1/chat/completions", ctx.Request.URL.Path)
	require.Equal(t, []types.RelayFormat{types.RelayFormatOpenAIResponsesCompaction, types.RelayFormatOpenAI}, info.RequestConversionChain)
}

func TestDiagnoseChannelTestErrorDetectsCompactUnavailable(t *testing.T) {
	summary := &channelTestRuntimeSummary{
		RequestPath:      "/v1/responses/compact",
		FinalRequestPath: "/v1/responses/compact",
	}

	diagnosis := diagnoseChannelTestError(
		errors.New("No available channel for model gpt-5.5-openai-compact under group test"),
		nil,
		summary,
	)

	require.Equal(t, "compact_unavailable", diagnosis.Category)
	require.Contains(t, diagnosis.Suggestion, "Chat Completions")
}

func TestBuildChannelForTestOptionsDisablesRuntimeSettingsWithoutMutatingChannel(t *testing.T) {
	headerOverride := `{"User-Agent":"Codex/1.0"}`
	paramOverride := `{"operations":[{"mode":"pass_headers","value":["User-Agent"]}]}`
	modelMapping := `{"gpt-5.5":"gpt-5.4"}`
	setting := `{"proxy":"http://user:pass@127.0.0.1:7890"}`
	otherSettings := marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
		HeaderProfileStrategy: &dto.HeaderProfileStrategy{
			Enabled:            true,
			Mode:               dto.HeaderProfileModeFixed,
			SelectedProfileIDs: []string{"codex-cli"},
		},
		OverrideHeaderUserAgent: true,
		UserAgentStrategy: &dto.UserAgentStrategy{
			Enabled:    true,
			Mode:       "fixed",
			UserAgents: []string{"Codex/1.0"},
		},
	})
	channel := &model.Channel{
		Setting:        &setting,
		HeaderOverride: &headerOverride,
		ParamOverride:  &paramOverride,
		ModelMapping:   &modelMapping,
		OtherSettings:  otherSettings,
	}

	cloned, err := buildChannelForTestOptions(channel, channelTestOptions{
		UseRuntimeConfig: true,
		UseHeaderConfig:  false,
		UseParamOverride: false,
		UseProxy:         false,
		UseModelMapping:  false,
	})

	require.NoError(t, err)
	require.NotNil(t, cloned)
	require.Equal(t, setting, *channel.Setting)
	require.Equal(t, headerOverride, *channel.HeaderOverride)
	require.Equal(t, paramOverride, *channel.ParamOverride)
	require.Equal(t, modelMapping, *channel.ModelMapping)
	require.Equal(t, otherSettings, channel.OtherSettings)
	require.Nil(t, cloned.HeaderOverride)
	require.Nil(t, cloned.ParamOverride)
	require.Nil(t, cloned.ModelMapping)

	clonedSetting, err := parseChannelTestSetting(cloned)
	require.NoError(t, err)
	require.Empty(t, clonedSetting.Proxy)

	clonedOtherSettings, err := parseChannelTestOtherSettings(cloned)
	require.NoError(t, err)
	require.Nil(t, clonedOtherSettings.HeaderProfileStrategy)
	require.False(t, clonedOtherSettings.OverrideHeaderUserAgent)
	require.Nil(t, clonedOtherSettings.UserAgentStrategy)
}

func TestPrepareChannelTestRequestHeadersSeedsAndFreezesHeaderProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	channel := &model.Channel{
		OtherSettings: marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
			HeaderProfileStrategy: &dto.HeaderProfileStrategy{
				Enabled:            true,
				Mode:               dto.HeaderProfileModeFixed,
				SelectedProfileIDs: []string{"codex-cli"},
			},
		}),
	}

	profileID, err := prepareChannelTestRequestHeaders(ctx, channel)

	require.NoError(t, err)
	require.Equal(t, "codex-cli", profileID)
	require.Equal(t, "OpenAI Codex CLI/0.1", ctx.Request.Header.Get("User-Agent"))
	require.Equal(t, "codex-cli", ctx.Request.Header.Get("X-Client-Name"))

	settings, err := parseChannelTestOtherSettings(channel)
	require.NoError(t, err)
	require.NotNil(t, settings.HeaderProfileStrategy)
	require.Equal(t, dto.HeaderProfileModeFixed, settings.HeaderProfileStrategy.Mode)
	require.Equal(t, []string{"codex-cli"}, settings.HeaderProfileStrategy.SelectedProfileIDs)
	require.Len(t, settings.HeaderProfileStrategy.Profiles, 1)
	require.Equal(t, "codex-cli", settings.HeaderProfileStrategy.Profiles[0].ID)
}

func TestPreparedHeaderProfileFeedsChannelTestPassHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	channel := &model.Channel{
		OtherSettings: marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
			HeaderProfileStrategy: &dto.HeaderProfileStrategy{
				Enabled:            true,
				Mode:               dto.HeaderProfileModeFixed,
				SelectedProfileIDs: []string{"codex-cli"},
			},
		}),
	}

	_, err := prepareChannelTestRequestHeaders(ctx, channel)
	require.NoError(t, err)
	settings, err := parseChannelTestOtherSettings(channel)
	require.NoError(t, err)

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		RequestHeaders: map[string]string{
			"User-Agent":    ctx.Request.Header.Get("User-Agent"),
			"X-Client-Name": ctx.Request.Header.Get("X-Client-Name"),
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"mode":  "pass_headers",
						"value": []interface{}{"User-Agent", "X-Client-Name"},
					},
				},
			},
			ChannelOtherSettings: settings,
		},
	}

	_, err = relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-5.5"}`), info)
	require.NoError(t, err)
	headers := relaycommon.GetEffectiveHeaderOverride(info)
	require.Equal(t, "OpenAI Codex CLI/0.1", headers["user-agent"])
	require.Equal(t, "codex-cli", headers["x-client-name"])
}

func TestApplyChannelTestSourceHeadersSkipsSensitiveHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	applyChannelTestSourceHeaders(ctx, map[string]string{
		"User-Agent":      "Codex/1.0",
		"X-Codex-Trace":   "trace-1",
		"Authorization":   "Bearer secret",
		"Cookie":          "session=secret",
		"New-Api-User":    "12",
		"X-Forwarded-For": "127.0.0.1",
	})

	require.Equal(t, "Codex/1.0", ctx.Request.Header.Get("User-Agent"))
	require.Equal(t, "trace-1", ctx.Request.Header.Get("X-Codex-Trace"))
	require.Equal(t, "127.0.0.1", ctx.Request.Header.Get("X-Forwarded-For"))
	require.Empty(t, ctx.Request.Header.Get("Authorization"))
	require.Empty(t, ctx.Request.Header.Get("Cookie"))
	require.Empty(t, ctx.Request.Header.Get("New-Api-User"))
}

func TestFinalizeChannelTestRuntimeSummaryMarksRuntimeHeaderParamOverrideApplied(t *testing.T) {
	summary := &channelTestRuntimeSummary{
		ParamOverrideConfigured: true,
	}
	info := &relaycommon.RelayInfo{
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]interface{}{
			"user-agent": "OpenAI Codex CLI/0.1",
		},
	}

	finalizeChannelTestRuntimeSummary(summary, nil, info)

	require.True(t, summary.ParamOverrideApplied)
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}
