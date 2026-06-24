package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestShouldRetryHonorsAlwaysSkipTimeoutStatusCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name       string
		relayMode  int
		statusCode int
		want       bool
	}{
		{
			name:       "compact skips gateway timeout",
			relayMode:  relayconstant.RelayModeResponsesCompact,
			statusCode: http.StatusGatewayTimeout,
			want:       false,
		},
		{
			name:       "compact skips upstream 524 timeout",
			relayMode:  relayconstant.RelayModeResponsesCompact,
			statusCode: 524,
			want:       false,
		},
		{
			name:       "normal responses skips upstream 524 timeout",
			relayMode:  relayconstant.RelayModeResponses,
			statusCode: 524,
			want:       false,
		},
		{
			name:       "compact retries retryable non skip status",
			relayMode:  relayconstant.RelayModeResponsesCompact,
			statusCode: http.StatusServiceUnavailable,
			want:       true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			c.Set("relay_mode", tc.relayMode)
			err := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, tc.statusCode)

			require.Equal(t, tc.want, shouldRetry(c, err, 1))
		})
	}
}

func TestShouldRetryResponsesCompactTimeoutHonorsAlwaysSkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("relay_mode", relayconstant.RelayModeResponsesCompact)
	err := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, 524)

	require.False(t, shouldRetry(c, err, 0))
}

func TestShouldRetryGatewayTimeoutHonorsAlwaysSkipDespiteChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name       string
		statusCode int
	}{
		{
			name:       "gateway timeout cannot switch channel",
			statusCode: http.StatusGatewayTimeout,
		},
		{
			name:       "cloudflare timeout cannot switch channel",
			statusCode: 524,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			c.Set("channel_affinity_skip_retry_on_failure", true)
			err := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, tc.statusCode)

			require.False(t, shouldRetry(c, err, 1))
			require.False(t, shouldRetry(c, err, 0))
		})
	}
}

func TestShouldRetryResponsesCompactUpstream413DespiteChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("relay_mode", relayconstant.RelayModeResponsesCompact)
	c.Set("channel_affinity_skip_retry_on_failure", true)
	err := types.InitOpenAIError(types.ErrorCodeUpstreamRequestTooLarge, http.StatusRequestEntityTooLarge)

	require.True(t, shouldRetry(c, err, 1))
}

func TestShouldRetryResponsesCompactUpstream413KeepsScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("normal responses keep affinity skip", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		c.Set("relay_mode", relayconstant.RelayModeResponses)
		c.Set("channel_affinity_skip_retry_on_failure", true)
		err := types.InitOpenAIError(types.ErrorCodeUpstreamRequestTooLarge, http.StatusRequestEntityTooLarge)

		require.False(t, shouldRetry(c, err, 1))
	})

	t.Run("local oversized body still skips retry", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		c.Set("relay_mode", relayconstant.RelayModeResponsesCompact)
		err := types.NewErrorWithStatusCode(
			errors.New("request body too large"),
			types.ErrorCodeReadRequestBodyFailed,
			http.StatusRequestEntityTooLarge,
			types.ErrOptionWithSkipRetry(),
		)

		require.False(t, shouldRetry(c, err, 1))
	})
}

func TestShouldRetryAllowsRateLimitDespiteChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{
			name:       "rate limit can switch channel",
			statusCode: http.StatusTooManyRequests,
			want:       true,
		},
		{
			name:       "non rate limit keeps affinity skip",
			statusCode: http.StatusInternalServerError,
			want:       false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			c.Set("channel_affinity_skip_retry_on_failure", true)
			err := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, tc.statusCode)

			require.Equal(t, tc.want, shouldRetry(c, err, 1))
		})
	}
}

func TestShouldRetryAllowsModelCapacityDespiteChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name       string
		message    string
		statusCode int
		want       bool
	}{
		{
			name:       "exact codex capacity message can switch channel",
			message:    "Selected model is at capacity. Please try a different model.",
			statusCode: http.StatusBadRequest,
			want:       true,
		},
		{
			name:       "case and whitespace insensitive capacity message can switch channel",
			message:    "selected MODEL is at capacity.\nplease try a different model.",
			statusCode: http.StatusServiceUnavailable,
			want:       true,
		},
		{
			name:       "unrelated capacity message keeps affinity skip",
			message:    "token cache capacity exceeded",
			statusCode: http.StatusBadRequest,
			want:       false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			c.Set("channel_affinity_skip_retry_on_failure", true)
			err := types.WithOpenAIError(types.OpenAIError{
				Message: tc.message,
				Type:    "server_error",
				Code:    string(types.ErrorCodeBadResponseStatusCode),
			}, tc.statusCode)

			require.Equal(t, tc.want, shouldRetry(c, err, 1))
		})
	}
}

func TestShouldSkipChannelForResponsesNativeCompactionUnsupportedCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	unsupported := false

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"compaction","encrypted_content":"opaque-native-compact"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`),
		},
	}
	channel := &model.Channel{
		Id:   207,
		Type: constant.ChannelTypeOpenAI,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			Source:                            "observed_calls",
			Observed:                          &dto.ResponsesCapabilityObservation{ObservedAt: time.Now().Unix(), StatusCode: http.StatusBadRequest, Reason: "Invalid input type 'compaction'"},
			SupportsCompactionItemPassthrough: &unsupported,
		},
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.True(t, skip)
	require.NotNil(t, info.LastError)
	require.Equal(t, "channel_skipped_unsupported_compaction", common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
}

func TestShouldSkipChannelForResponsesProxyCompactionTriggerWithoutRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := model_setting.GetGlobalSettings()
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := compactionTriggerRelayInfo()
	channel := &model.Channel{
		Id:   168,
		Type: constant.ChannelTypeOpenAI,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileChatOnlyProxy,
	})
	channelOtherSettings := channel.GetOtherSettings()
	require.True(t, info.Request.(*dto.OpenAIResponsesRequest).HasCompactionTrigger())
	require.True(t, channelOtherSettings.HasResponsesProxyCompatibilityProfile())

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.True(t, skip)
	require.NotNil(t, info.LastError)
	require.Equal(t, "channel_skipped_unsupported_compaction", common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
}

func TestShouldRetryModelCapacityHonorsRetryBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "Selected model is at capacity. Please try a different model.",
		Type:    "server_error",
		Code:    string(types.ErrorCodeBadResponseStatusCode),
	}, http.StatusBadRequest)

	require.True(t, shouldRetry(c, err, 1))
	require.False(t, shouldRetry(c, err, 0))
}

func TestShouldRetryHonorsAlwaysSkipCodeBeforeTemporaryRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "Upstream service temporarily unavailable",
		Type:    "server_error",
		Code:    string(types.ErrorCodeBadResponseBody),
	}, http.StatusBadGateway)

	require.False(t, shouldRetry(c, err, 1))
}

func TestShouldRetryAllowsTemporaryUpstreamErrorsDespiteChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name       string
		message    string
		statusCode int
		want       bool
	}{
		{
			name:       "temporary unavailable can switch channel",
			message:    "Upstream service temporarily unavailable",
			statusCode: http.StatusBadGateway,
			want:       true,
		},
		{
			name:       "permanently unavailable does not bypass affinity skip",
			message:    "This model is permanently unavailable: service temporarily unavailable",
			statusCode: http.StatusServiceUnavailable,
			want:       false,
		},
		{
			name:       "subscription temporarily unavailable can switch channel",
			message:    "当前订阅额度不足或暂不可用，请稍后再试或联系管理员",
			statusCode: http.StatusForbidden,
			want:       true,
		},
		{
			name:       "unrelated 503 keeps affinity skip",
			message:    "upstream maintenance in progress",
			statusCode: http.StatusServiceUnavailable,
			want:       false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			c.Set("channel_affinity_skip_retry_on_failure", true)
			err := types.WithOpenAIError(types.OpenAIError{
				Message: tc.message,
				Type:    "server_error",
				Code:    string(types.ErrorCodeBadResponseStatusCode),
			}, tc.statusCode)

			require.Equal(t, tc.want, shouldRetry(c, err, 1))
		})
	}
}

func TestShouldRetryAllowsUpstreamStreamInterruptedDespiteChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("channel_affinity_skip_retry_on_failure", true)
	err := types.NewOpenAIError(
		errors.New("stream disconnected before completion"),
		types.ErrorCodeUpstreamTransportInterrupted,
		http.StatusBadGateway,
	)

	require.True(t, shouldRetry(c, err, 1))
	require.False(t, shouldRetry(c, err, 0))
}

func TestShouldRetryAllowsNoAuthAvailableDespiteChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("channel_affinity_skip_retry_on_failure", true)
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "auth_not_found: no auth available (providers=codex, model=gpt-5.5)",
		Type:    "server_error",
		Code:    string(types.ErrorCodeBadResponseStatusCode),
	}, http.StatusServiceUnavailable)

	require.True(t, shouldRetry(c, err, 1))
	require.False(t, shouldRetry(c, err, 0))
}

func TestShouldRetryTaskRelayAllowsRateLimitDespiteChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{
			name:       "rate limit can switch task channel",
			statusCode: http.StatusTooManyRequests,
			want:       true,
		},
		{
			name:       "non rate limit keeps affinity skip",
			statusCode: http.StatusInternalServerError,
			want:       false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			c.Set("channel_affinity_skip_retry_on_failure", true)
			taskErr := &dto.TaskError{StatusCode: tc.statusCode}

			require.Equal(t, tc.want, shouldRetryTaskRelay(c, 1, taskErr, 1))
		})
	}
}

func TestGetChannelInitialRequestUsesDistributedChannel(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	rateLimited, _ := seedRateLimitRetryChannels(t, db)
	ctx, _ := gin.CreateTestContext(nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, rateLimited, "gpt-5.5"))

	info := &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		OriginModelName: "gpt-5.5",
	}
	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 206, channel.Id)
	require.Nil(t, info.ChannelMeta)
}

func TestGetChannelRateLimitRetrySwitchesChannelAndRefreshesMeta(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	rateLimited, fallback := seedRateLimitRetryChannels(t, db)
	ctx, _ := gin.CreateTestContext(nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, rateLimited, "gpt-5.5"))
	addUsedChannel(ctx, rateLimited.Id)

	info := &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		OriginModelName: "gpt-5.5",
	}
	info.InitChannelMeta(ctx)
	err429 := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	require.True(t, shouldRetry(ctx, err429, 1))

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(1),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, fallback.Id, channel.Id)
	require.Equal(t, fallback.Id, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, fallback.Id, info.ChannelMeta.ChannelId)
	require.Equal(t, fallback.Name, common.GetContextKeyString(ctx, constant.ContextKeyChannelName))
}

func TestGetChannelSkipsResponsesViaChatForRemoteCompactionInput(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)

	// This test mutates global protocol conversion settings; do not add t.Parallel().
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(168)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := remoteCompactionRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	require.Equal(t, "channel_skipped_unsupported_compaction", common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, native.Id, info.ChannelMeta.ChannelId)
}

func TestGetChannelAllResponsesViaChatRemoteCompactionInputReturnsUnsupported(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			remoteCompactionChatOnlyPolicy(chatOnly.Id).Rules[0],
			remoteCompactionChatOnlyPolicy(native.Id).Rules[0],
		},
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := remoteCompactionRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeChannelModelMappedError, err.GetErrorCode())
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Contains(t, err.Error(), "cannot handle native compaction input")
	require.ElementsMatch(t, []string{"168", "206"}, ctx.GetStringSlice("use_channel"))
}

func TestGetChannelDoesNotReusePreviousCompactionSkipForLaterCalls(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(168)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	firstInfo := remoteCompactionRelayInfo()

	firstChannel, firstErr := getChannel(ctx, firstInfo, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})
	require.Nil(t, firstErr)
	require.NotNil(t, firstChannel)
	require.Equal(t, native.Id, firstChannel.Id)
	require.Equal(t, "channel_skipped_unsupported_compaction", common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))

	ctx.Set("use_channel", []string{"168", "206"})
	secondInfo := &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`),
		},
	}

	secondChannel, secondErr := getChannel(ctx, secondInfo, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(1),
	})
	require.Nil(t, secondChannel)
	require.NotNil(t, secondErr)
	require.Equal(t, types.ErrorCodeGetChannelFailed, secondErr.GetErrorCode())
	require.NotContains(t, secondErr.Error(), "cannot handle native compaction input")
}

func TestGetChannelSpecificResponsesViaChatRejectsRemoteCompactionInput(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, _ := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(168)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("specific_channel_id", "168")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := remoteCompactionRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Contains(t, err.Error(), "cannot handle native compaction input")
}

func TestGetChannelSpecificProxyProfileRejectsRemoteCompactionInput(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, _ := seedRemoteCompactionRouteChannels(t, db)
	chatOnly.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
	})
	require.NoError(t, db.Save(chatOnly).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "global-responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("specific_channel_id", "168")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := remoteCompactionRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Contains(t, err.Error(), "cannot handle native compaction input")
}

func TestGetChannelReturnsNativeCompactionSkipErrorWithoutChannelWrap(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)
	native.Status = common.ChannelStatusManuallyDisabled
	require.NoError(t, db.Save(native).Error)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", native.Id).Update("enabled", false).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				ChannelIDs:     []int{chatOnly.Id},
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := remoteCompactionRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Contains(t, err.Error(), "cannot handle native compaction input")
	require.NotContains(t, err.Error(), "可用渠道不存在")
}

func TestRemoteCompactionSkipKeepsGlobalCompatibilityRuleCandidate(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	_, native := seedRemoteCompactionRouteChannels(t, db)
	native.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
	})
	require.NoError(t, db.Save(native).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "global-responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := remoteCompactionRelayInfo()

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, native)
	require.NoError(t, err)
	require.False(t, skip)
}

func TestDefaultProfileAllowsCompactionTriggerPassthroughForLegacyChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := compactionTriggerRelayInfo()
	channel := &model.Channel{
		Id:   178,
		Type: constant.ChannelTypeOpenAI,
	}

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "global-responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
	otherSettings := channel.GetOtherSettings()
	require.True(t, otherSettings.ResolveResponsesChannelCapability(channel.Type).SupportsCompactionItemPassthrough)
}

func TestDefaultProfileAllowsRemoteCompactionPassthroughForLegacyChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := remoteCompactionRelayInfo()
	channel := &model.Channel{
		Id:   208,
		Type: constant.ChannelTypeOpenAI,
	}

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "global-responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
	otherSettings := channel.GetOtherSettings()
	require.True(t, otherSettings.ResolveResponsesChannelCapability(channel.Type).SupportsCompactionItemPassthrough)
}

func TestResponsesToChatPrecheckAllowsStaleLocalSyntheticMarkerWithVisibleInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.SyntheticCompactStateRecord{}))

	info := staleLocalSyntheticCompactionRelayInfo()
	channel := &model.Channel{
		Id:   168,
		Type: constant.ChannelTypeOpenAI,
	}

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(channel.Id)

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
}

func TestResponsesToChatPrecheckSkipsNativeOpaqueLocalState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.SyntheticCompactStateRecord{}))

	scope := service.SyntheticCompactStateScope{
		UserID:      7,
		TokenID:     8,
		Group:       "default",
		Model:       "gpt-5.5",
		ChannelID:   206,
		ChannelType: constant.ChannelTypeOpenAI,
	}
	state, err := service.StoreNativeOpaqueCompactState(context.Background(), scope, "gpt-5.5", "resp_native_upstream", "opaque-token", 1710000000)
	require.NoError(t, err)
	require.NotNil(t, state)

	info := remoteCompactionRelayInfo()
	info.UserId = scope.UserID
	info.TokenId = scope.TokenID
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelId:   168,
		ChannelType: constant.ChannelTypeOpenAI,
	}
	info.Request = &dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}
	channel := &model.Channel{
		Id:   168,
		Type: constant.ChannelTypeOpenAI,
	}

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(channel.Id)

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.True(t, skip)
	require.NotNil(t, info.LastError)
	require.Equal(t, http.StatusServiceUnavailable, info.LastError.StatusCode)
	require.Equal(t, types.ErrorCodeChannelModelMappedError, info.LastError.GetErrorCode())
	require.Equal(t, "channel_skipped_unsupported_compaction", common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
}

func TestGetChannelSkipsGlobalResponsesViaChatForRemoteCompactionInput(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)
	chatOnly.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsCompactionItemPassthrough: common.GetPointer(false),
		},
	})
	native.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
	})
	require.NoError(t, db.Save(chatOnly).Error)
	require.NoError(t, db.Save(native).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "global-responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := remoteCompactionRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	require.Equal(t, "channel_skipped_unsupported_compaction", common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, native.Id, info.ChannelMeta.ChannelId)
}

func TestGetChannelSkipsProxyProfileRemoteCompactionWithoutConversionRule(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	proxy, native := seedRemoteCompactionRouteChannels(t, db)
	proxy.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
	})
	require.NoError(t, db.Save(proxy).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, proxy, "gpt-5.5"))
	info := remoteCompactionRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	require.Equal(t, "channel_skipped_unsupported_compaction", common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
}

func TestGetChannelSpecificChatOnlyProfileRejectsRemoteCompactionWithoutConversionRule(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, _ := seedRemoteCompactionRouteChannels(t, db)
	chatOnly.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileChatOnlyProxy,
	})
	require.NoError(t, db.Save(chatOnly).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("specific_channel_id", "168")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := remoteCompactionRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Contains(t, err.Error(), "cannot handle native compaction input")
}

func TestGetChannelRejectsGenericOpenAIRemoteCompactionWithoutPassthroughCapability(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	channel, _ := seedRemoteCompactionRouteChannels(t, db)
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericOpenAI,
	})
	require.NoError(t, db.Save(channel).Error)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id <> ?", channel.Id).Update("enabled", false).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, channel, "gpt-5.5"))
	info := remoteCompactionRelayInfo()

	selected, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, selected)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Contains(t, err.Error(), "cannot handle native compaction input")
	require.Equal(t, "channel_skipped_unsupported_compaction", common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
}

func TestGetChannelSkipsResponsesViaChatForCompactionTrigger(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(168)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := compactionTriggerRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, native.Id, info.ChannelMeta.ChannelId)
}

func TestGetChannelSkipsResponsesViaChatThenFallsBackToLowerPriorityForCompactionTrigger(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)
	setChannelPriorityForTest(t, db, chatOnly, 20)
	setChannelPriorityForTest(t, db, native, 10)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(168)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := compactionTriggerRelayInfo()

	retryParam := &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	}
	channel, err := getChannel(ctx, info, retryParam)

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	require.Equal(t, 0, retryParam.GetRetry())
	require.Equal(t, 0, info.RetryIndex)
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, native.Id, info.ChannelMeta.ChannelId)
}

func TestGetChannelAutoGroupCompactionSkipFallsBackToLowerPriority(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)
	setChannelPriorityForTest(t, db, chatOnly, 20)
	setChannelPriorityForTest(t, db, native, 10)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	oldAutoGroups := setting.AutoGroups2JsonString()
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(oldAutoGroups))
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(168)
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default"]`))

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := compactionTriggerRelayInfo()
	info.TokenGroup = "auto"
	info.UsingGroup = "auto"

	retryParam := &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "auto",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	}
	channel, err := getChannel(ctx, info, retryParam)

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	require.Equal(t, 0, retryParam.GetRetry())
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, native.Id, info.ChannelMeta.ChannelId)
	require.Equal(t, "default", info.UsingGroup)
}

func TestGetChannelKeepsPassThroughResponsesViaChatForCompactionTrigger(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, _ := seedRemoteCompactionRouteChannels(t, db)
	chatOnly.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: true})
	require.NoError(t, db.Save(chatOnly).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(168)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := compactionTriggerRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, chatOnly.Id, channel.Id)
	require.Empty(t, ctx.GetStringSlice("use_channel"))
	require.Nil(t, info.ChannelMeta)
}

func TestGetChannelSkipsResponsesViaChatForUnsupportedNamespaceTool(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(168)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := namespaceToolRelayInfo(false)

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, native.Id, info.ChannelMeta.ChannelId)
}

func TestGetChannelSkipsGlobalResponsesViaChatForUnsupportedNamespaceTool(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, _ := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "global-responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := namespaceToolRelayInfo(false)

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeGetChannelFailed, err.GetErrorCode())
	require.Contains(t, err.Error(), "cannot safely convert this Responses request")
	require.Equal(t, []string{"168", "206"}, ctx.GetStringSlice("use_channel"))
}

func TestGetChannelSpecificResponsesViaChatRejectsUnsupportedNamespaceTool(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, _ := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(168)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("specific_channel_id", "168")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := namespaceToolRelayInfo(false)

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Contains(t, err.Error(), "cannot safely convert this Responses request")
	require.Contains(t, err.Error(), "custom tool bridge is not enabled")
}

func TestGetChannelKeepsResponsesViaChatForBridgeableNamespaceTool(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, _ := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(168)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := namespaceToolRelayInfo(true)

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, chatOnly.Id, channel.Id)
	require.Empty(t, ctx.GetStringSlice("use_channel"))
}

func TestGetChannelRejectsMalformedRemoteCompactionInput(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, _ := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(168)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := malformedRemoteCompactionRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Equal(t, types.ErrorCodeInvalidRequest, err.GetErrorCode())
	require.Contains(t, err.Error(), "missing encrypted_content")
}

func seedRemoteCompactionRouteChannels(t *testing.T, gormDB *gorm.DB) (*model.Channel, *model.Channel) {
	t.Helper()
	priority := int64(10)
	weight := uint(1)
	chatOnly := &model.Channel{
		Id:       168,
		Name:     "responses-via-chat",
		Key:      "sk-chat",
		Type:     constant.ChannelTypeOpenAI,
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &priority,
		Weight:   &weight,
	}
	native := &model.Channel{
		Id:       206,
		Name:     "native-responses",
		Key:      "sk-native",
		Type:     constant.ChannelTypeOpenAI,
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &priority,
		Weight:   &weight,
	}
	native.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
	})
	for _, channel := range []*model.Channel{chatOnly, native} {
		require.NoError(t, gormDB.Create(channel).Error)
		require.NoError(t, gormDB.Create(&model.Ability{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}
	return chatOnly, native
}

func setChannelPriorityForTest(t *testing.T, gormDB *gorm.DB, channel *model.Channel, priority int64) {
	t.Helper()
	channel.Priority = &priority
	require.NoError(t, gormDB.Save(channel).Error)
	require.NoError(t, gormDB.Model(&model.Ability{}).Where("channel_id = ?", channel.Id).Update("priority", priority).Error)
}

func remoteCompactionChatOnlyPolicy(channelID int) model_setting.ChatCompletionsToResponsesPolicy {
	return model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				ChannelIDs:     []int{channelID},
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}
}

func remoteCompactionChatOnlyPolicyWithCustomToolBridge(channelID int) model_setting.ChatCompletionsToResponsesPolicy {
	policy := remoteCompactionChatOnlyPolicy(channelID)
	policy.Rules[0].Options = &model_setting.ProtocolConversionOptions{
		EnableCustomToolBridge: true,
	}
	return policy
}

func remoteCompactionRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"compaction","encrypted_content":"opaque-native-compact"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`),
		},
	}
}

func staleLocalSyntheticCompactionRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:nffffffffffffffffffffffffffffffff:resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_missing"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`),
		},
	}
}

func namespaceToolRelayInfo(bridgeable bool) *relaycommon.RelayInfo {
	nestedToolType := "custom"
	if bridgeable {
		nestedToolType = "function"
	}
	return &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`),
			Tools: common.RawMessage(`[
				{"type":"namespace","tools":[{"type":"` + nestedToolType + `","name":"repo_status","description":"inspect repo","parameters":{"type":"object","properties":{}}}]}
			]`),
		},
	}
}

func compactionTriggerRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},
				{"type":"compaction_trigger"}
			]`),
		},
	}
}

func malformedRemoteCompactionRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"compaction"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`),
		},
	}
}

func TestShouldRecordResponsesCapabilityObservationKeepsOrdinaryResponses400Out(t *testing.T) {
	err := types.NewErrorWithStatusCode(errors.New("missing required field"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)

	require.False(t, shouldRecordResponsesCapabilityObservation(relayconstant.RelayModeResponses, err))
}

func TestShouldRecordResponsesCapabilityObservationRecordsCompactSignals(t *testing.T) {
	testCases := []struct {
		name      string
		relayMode int
		err       *types.NewAPIError
		want      bool
	}{
		{
			name:      "responses records compaction keyword",
			relayMode: relayconstant.RelayModeResponses,
			err:       types.NewErrorWithStatusCode(errors.New(`input item type "compaction" is not supported in chat compatibility mode`), types.ErrorCodeInvalidRequest, http.StatusBadRequest),
			want:      true,
		},
		{
			name:      "responses records responses compact path keyword",
			relayMode: relayconstant.RelayModeResponses,
			err:       types.NewErrorWithStatusCode(errors.New("upstream /v1/responses/compact failed"), types.ErrorCodeInvalidRequest, http.StatusBadRequest),
			want:      true,
		},
		{
			name:      "responses records previous response id underscore",
			relayMode: relayconstant.RelayModeResponses,
			err:       types.NewErrorWithStatusCode(errors.New("previous_response_id only supported on ws v2"), types.ErrorCodeInvalidRequest, http.StatusBadRequest),
			want:      true,
		},
		{
			name:      "responses records previous response id spaced",
			relayMode: relayconstant.RelayModeResponses,
			err:       types.NewErrorWithStatusCode(errors.New("previous response id unsupported"), types.ErrorCodeInvalidRequest, http.StatusBadRequest),
			want:      true,
		},
		{
			name:      "responses records case insensitive compact signal",
			relayMode: relayconstant.RelayModeResponses,
			err:       types.NewErrorWithStatusCode(errors.New("CHAT COMPATIBILITY MODE rejects compaction"), types.ErrorCodeInvalidRequest, http.StatusBadRequest),
			want:      true,
		},
		{
			name:      "responses records entity too large without keyword",
			relayMode: relayconstant.RelayModeResponses,
			err:       types.InitOpenAIError(types.ErrorCodeUpstreamRequestTooLarge, http.StatusRequestEntityTooLarge),
			want:      true,
		},
		{
			name:      "responses skips ordinary bad request",
			relayMode: relayconstant.RelayModeResponses,
			err:       types.NewErrorWithStatusCode(errors.New("missing required field"), types.ErrorCodeInvalidRequest, http.StatusBadRequest),
			want:      false,
		},
		{
			name:      "compact route records ordinary bad request",
			relayMode: relayconstant.RelayModeResponsesCompact,
			err:       types.NewErrorWithStatusCode(errors.New("missing required field"), types.ErrorCodeInvalidRequest, http.StatusBadRequest),
			want:      true,
		},
		{
			name:      "chat route skips compact signal",
			relayMode: relayconstant.RelayModeChatCompletions,
			err:       types.NewErrorWithStatusCode(errors.New("compaction unsupported"), types.ErrorCodeInvalidRequest, http.StatusBadRequest),
			want:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, shouldRecordResponsesCapabilityObservation(tc.relayMode, tc.err))
		})
	}
}

func seedRateLimitRetryChannels(t *testing.T, gormDB *gorm.DB) (*model.Channel, *model.Channel) {
	t.Helper()

	priority := int64(10)
	weight := uint(1)
	rateLimited := &model.Channel{
		Id:       206,
		Name:     "rate-limited",
		Key:      "sk-rate-limited",
		Type:     constant.ChannelTypeOpenAI,
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &priority,
		Weight:   &weight,
	}
	fallback := &model.Channel{
		Id:       207,
		Name:     "fallback",
		Key:      "sk-fallback",
		Type:     constant.ChannelTypeOpenAI,
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &priority,
		Weight:   &weight,
	}
	for _, channel := range []*model.Channel{rateLimited, fallback} {
		require.NoError(t, gormDB.Create(channel).Error)
		require.NoError(t, gormDB.Create(&model.Ability{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}
	return rateLimited, fallback
}

func TestShouldFallbackResponsesCompactAutoRequiresCompatibilityError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name           string
		statusCode     int
		message        string
		payloadRequest bool
		want           bool
	}{
		{
			name:       "model 404 does not fallback",
			statusCode: http.StatusNotFound,
			message:    "model gpt-5.5 was not found",
			want:       false,
		},
		{
			name:       "compact route 404 falls back",
			statusCode: http.StatusNotFound,
			message:    "no route for /v1/responses/compact",
			want:       true,
		},
		{
			name:       "generic compact 404 falls back",
			statusCode: http.StatusNotFound,
			message:    "bad response status code 404",
			want:       true,
		},
		{
			name:       "generic compact method not allowed falls back",
			statusCode: http.StatusMethodNotAllowed,
			message:    "bad response status code 405",
			want:       true,
		},
		{
			name:       "native compact upstream 500 falls back",
			statusCode: http.StatusInternalServerError,
			message:    "do_request_failed",
			want:       true,
		},
		{
			name:       "native compact upstream 502 falls back",
			statusCode: http.StatusBadGateway,
			message:    "bad response status code 502",
			want:       true,
		},
		{
			name:       "native compact upstream 503 falls back",
			statusCode: http.StatusServiceUnavailable,
			message:    "Service temporarily unavailable",
			want:       true,
		},
		{
			name:       "native compact rate limit does not fallback",
			statusCode: http.StatusTooManyRequests,
			message:    "Concurrency limit exceeded for account, please retry later",
			want:       false,
		},
		{
			name:       "ordinary bad request does not fallback",
			statusCode: http.StatusBadRequest,
			message:    "unsupported parameter: temperature",
			want:       false,
		},
		{
			name:       "compact parameter error does not fallback",
			statusCode: http.StatusBadRequest,
			message:    "unsupported parameter: temperature for /v1/responses/compact",
			want:       false,
		},
		{
			name:       "positive supported wording does not fallback",
			statusCode: http.StatusBadRequest,
			message:    "responses compact is supported by this upstream",
			want:       false,
		},
		{
			name:       "compact bad request falls back",
			statusCode: http.StatusBadRequest,
			message:    "responses compact endpoint is not supported",
			want:       true,
		},
		{
			name:           "native payload content rejection falls back",
			statusCode:     http.StatusBadRequest,
			message:        "请求包含不允许的内容，请修改后重试",
			payloadRequest: true,
			want:           true,
		},
		{
			name:           "native payload spaced chinese content rejection falls back",
			statusCode:     http.StatusBadRequest,
			message:        "请求 包含 不允许 的内容，请修改后重试",
			payloadRequest: true,
			want:           true,
		},
		{
			name:           "native payload disallowed content falls back",
			statusCode:     http.StatusBadRequest,
			message:        "request contains disallowed content",
			payloadRequest: true,
			want:           true,
		},
		{
			name:           "native payload input disallowed content falls back",
			statusCode:     http.StatusBadRequest,
			message:        "input contains disallowed content",
			payloadRequest: true,
			want:           true,
		},
		{
			name:           "native payload content not allowed falls back",
			statusCode:     http.StatusBadRequest,
			message:        "content is not allowed",
			payloadRequest: true,
			want:           true,
		},
		{
			name:       "model lookup still wins over payload content rejection",
			statusCode: http.StatusNotFound,
			message:    "model gpt-5 not found: request contains disallowed content",
			want:       false,
		},
		{
			name:       "parameter error still wins over payload content rejection",
			statusCode: http.StatusBadRequest,
			message:    "unsupported parameter temperature: payload contains disallowed content",
			want:       false,
		},
		{
			name:       "generic content policy block does not fallback",
			statusCode: http.StatusBadRequest,
			message:    "content policy violation",
			want:       false,
		},
		{
			name:       "compact unprocessable entity falls back",
			statusCode: http.StatusUnprocessableEntity,
			message:    "Responses compact is unsupported by this upstream",
			want:       true,
		},
		{
			name:       "model lookup error with compact path does not fallback",
			statusCode: http.StatusNotFound,
			message:    "model gpt-5 was not found for /v1/responses/compact",
			want:       false,
		},
		{
			name:       "model unavailable with compact path does not fallback",
			statusCode: http.StatusNotFound,
			message:    "model gpt-5 is not available for /v1/responses/compact",
			want:       false,
		},
		{
			name:       "extra spaced compact route error falls back",
			statusCode: http.StatusNotFound,
			message:    "no route for /v1/responses   compact",
			want:       true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			err := types.WithOpenAIError(types.OpenAIError{
				Message: tc.message,
				Code:    string(types.ErrorCodeBadResponseStatusCode),
			}, tc.statusCode)

			info := compactAutoFallbackRelayInfo()
			if tc.payloadRequest {
				info.Request = compactContentRejectionPayloadRequest()
			} else {
				info.Request = compactVisiblePayloadRequest()
			}
			require.Equal(t, tc.want, shouldFallbackResponsesCompactAuto(c, info, err))
			_, exists := c.Get("responses_compact_auto_fallback_attempted")
			require.False(t, exists)
		})
	}
}

func TestShouldFallbackResponsesCompactAutoSkipsRemoteOpaqueCompaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "Responses compact is unsupported by this upstream",
		Code:    string(types.ErrorCodeBadResponseStatusCode),
	}, http.StatusUnprocessableEntity)
	info := compactAutoFallbackRelayInfo()
	info.Request = compactPayloadRequest()

	require.False(t, shouldFallbackResponsesCompactAuto(c, info, err))
}

func TestShouldFallbackResponsesCompactAutoRequiresContextPayloadForContentRejection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "request contains disallowed content",
		Code:    string(types.ErrorCodeBadResponseStatusCode),
	}, http.StatusBadRequest)
	info := compactAutoFallbackRelayInfo()
	info.Request = &dto.OpenAIResponsesCompactionRequest{
		Model: "gpt-5.5-openai-compact",
		Input: []byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"unsafe user prompt"}]}]`),
	}

	require.False(t, shouldFallbackResponsesCompactAuto(c, info, err))
}

func TestResponsesCompactRequestHasContextPayload(t *testing.T) {
	testCases := []struct {
		name string
		req  dto.Request
		want bool
	}{
		{
			name: "previous response id",
			req: &dto.OpenAIResponsesCompactionRequest{
				PreviousResponseID: "resp_123",
			},
			want: true,
		},
		{
			name: "compaction item",
			req: &dto.OpenAIResponsesCompactionRequest{
				Input: []byte(`[{"type":"compaction","encrypted_content":"opaque"}]`),
			},
			want: true,
		},
		{
			name: "compaction summary item",
			req: &dto.OpenAIResponsesCompactionRequest{
				Input: []byte(`[{"type":"compaction_summary","encrypted_content":"opaque"}]`),
			},
			want: true,
		},
		{
			name: "encrypted reasoning item",
			req: &dto.OpenAIResponsesCompactionRequest{
				Input: []byte(`[{"type":"reasoning","encrypted_content":"opaque"}]`),
			},
			want: true,
		},
		{
			name: "visible input only",
			req: &dto.OpenAIResponsesCompactionRequest{
				Input: []byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]`),
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, responsesCompactRequestHasContextPayload(&relaycommon.RelayInfo{Request: tc.req}))
		})
	}
}

func TestShouldFallbackResponsesCompactAutoHonorsAttemptedFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("responses_compact_auto_fallback_attempted", true)
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "no route for /v1/responses/compact",
		Code:    string(types.ErrorCodeBadResponseStatusCode),
	}, http.StatusNotFound)

	require.False(t, shouldFallbackResponsesCompactAuto(c, compactAutoFallbackRelayInfo(), err))
}

func TestShouldFallbackResponsesCompactAutoSkipsSyntheticAttemptedSameChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	markResponsesCompactSyntheticFallbackAttempted(c, info)
	err := types.NewOpenAIError(
		errors.New("provider returned malformed compact output: no compaction output"),
		types.ErrorCodeBadResponseBody,
		http.StatusBadGateway,
	)

	require.False(t, shouldFallbackResponsesCompactAuto(c, info, err))

	nextChannel := compactAutoFallbackRelayInfo()
	nextChannel.ChannelMeta.ChannelId = info.ChannelMeta.ChannelId + 1
	require.True(t, shouldFallbackResponsesCompactAuto(c, nextChannel, err))
}

func TestShouldFallbackResponsesCompactAutoHandlesMalformedNativeOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	err := types.NewOpenAIError(
		errors.New("provider returned malformed compact output: no compaction output"),
		types.ErrorCodeBadResponseBody,
		http.StatusBadGateway,
	)

	require.True(t, shouldFallbackResponsesCompactAuto(c, compactAutoFallbackRelayInfo(), err))
}

func TestShouldFallbackResponsesCompactPreviousResponseIDSkipsRemoteOpaqueCompaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	err := types.NewErrorWithStatusCode(
		service.ErrResponsesRESTPreviousIDUnsupported,
		types.ErrorCodeConvertRequestFailed,
		http.StatusBadRequest,
	)
	info := compactAutoFallbackRelayInfo()
	info.Request = &dto.OpenAIResponsesCompactionRequest{
		Model:              "gpt-5.5-openai-compact",
		PreviousResponseID: "resp_prev_remote",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"opaque-native-compact"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	require.False(t, shouldFallbackResponsesCompactPreviousResponseID(c, info, err))
}

func TestShouldFallbackResponsesCompactAutoSkipsActiveFallbackWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactAutoFallbackAt = time.Now().Unix()
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "no route for /v1/responses/compact",
		Code:    string(types.ErrorCodeBadResponseStatusCode),
	}, http.StatusNotFound)

	require.True(t, info.ChannelOtherSettings.HasActiveResponsesCompactAutoFallback(time.Now()))
	require.False(t, shouldFallbackResponsesCompactAuto(c, info, err))
	_, exists := c.Get("responses_compact_auto_fallback_attempted")
	require.False(t, exists)
}

func TestShouldFallbackResponsesCompactNativeContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeNative
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "This model's maximum context length is 128000 tokens. Your request has too many tokens.",
		Code:    "context_length_exceeded",
	}, http.StatusBadRequest)

	require.True(t, shouldFallbackResponsesCompactNativeContext(c, info, err))

	disabled := false
	info.ChannelOtherSettings.ResponsesCompactContextFallback = &disabled
	require.False(t, shouldFallbackResponsesCompactNativeContext(c, info, err))
}

func TestShouldFallbackResponsesCompactNativeContextSkipsRemoteOpaqueCompaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeNative
	info.Request = compactPayloadRequest()
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "This model's maximum context length is 128000 tokens. Your request has too many tokens.",
		Code:    "context_length_exceeded",
	}, http.StatusBadRequest)

	require.False(t, shouldFallbackResponsesCompactNativeContext(c, info, err))
}

func TestRetryResponsesCompactSyntheticSummaryRestoresModeOnFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, info.ChannelOtherSettings)
	triggerErr := types.NewOpenAIError(
		errors.New("provider returned malformed compact output: no compaction output"),
		types.ErrorCodeBadResponseBody,
		http.StatusBadGateway,
	)

	err := retryResponsesCompactSyntheticSummary(c, info, failingSeekBody{}, triggerErr)

	require.NotNil(t, err)
	require.Equal(t, dto.ResponsesCompactModeAuto, info.ChannelOtherSettings.ResponsesCompactMode)
	ctxSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	require.True(t, ok)
	require.Equal(t, dto.ResponsesCompactModeAuto, ctxSettings.ResponsesCompactMode)
}

func TestSetResponsesCompactSyntheticModeUpdatesRelayInfoSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()

	setResponsesCompactSyntheticMode(c, info)

	require.Equal(t, dto.ResponsesCompactModeSynthetic, info.ChannelMeta.ChannelOtherSettings.ResponsesCompactMode)
	require.Equal(t, dto.ResponsesCompactModeSynthetic, info.ChannelOtherSettings.ResponsesCompactMode)
	require.True(t, relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info))
	ctxSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	require.True(t, ok)
	require.Equal(t, dto.ResponsesCompactModeSynthetic, ctxSettings.ResponsesCompactMode)
}

func TestRestoreResponsesCompactModeUpdatesRelayInfoSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	setResponsesCompactSyntheticMode(c, info)

	original := dto.ChannelOtherSettings{ResponsesCompactMode: dto.ResponsesCompactModeAuto}
	restoreResponsesCompactMode(c, info, original)

	require.Equal(t, dto.ResponsesCompactModeAuto, info.ChannelMeta.ChannelOtherSettings.ResponsesCompactMode)
	require.Equal(t, dto.ResponsesCompactModeAuto, info.ChannelOtherSettings.ResponsesCompactMode)
	require.False(t, relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info))
	ctxSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	require.True(t, ok)
	require.Equal(t, dto.ResponsesCompactModeAuto, ctxSettings.ResponsesCompactMode)
}

func TestResponsesCompactFallbackContextSnapshotRestoresFailedAttemptMarkers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, 1)
	c.Set("responses_compact_context_fallback_attempted", true)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, "gpt-5.4")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactStateLookup, "original_hit")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactRouteDecision, "original_route")
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, compactAutoFallbackRelayInfo().ChannelOtherSettings)

	snapshot := snapshotResponsesCompactFallbackContext(c)
	c.Set("responses_compact_auto_fallback_attempted", true)
	c.Set("responses_compact_previous_response_id_fallback_attempted", true)
	c.Set("responses_compact_summary_model_fallback_attempted", true)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactStateLookup, "attempt_hit")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactStateScopeResult, "attempt_scope")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactMarkerKind, "attempt_marker")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactRouteDecision, "attempt_route")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactFallbackReason, "attempt_reason")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactChannelSkip, "attempt_skip")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactStateRestored, true)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactModelChanged, true)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactStateHash, "attempt_hash")
	common.SetContextKey(c, constant.ContextKeyResponsesPreviousIDAction, "attempt_previous_id_action")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, "gpt-5.3")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModels, []string{"gpt-5.3"})
	service.MarkResponsesCompactNativeFallback(c, compactAutoFallbackRelayInfo(), http.StatusBadGateway, "status_code=502", time.Now())
	setResponsesCompactVisibleOnly(c, true)

	info := compactAutoFallbackRelayInfo()
	info.ChannelMeta.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic

	restoreResponsesCompactFallbackContext(c, info, snapshot)

	require.False(t, c.GetBool("responses_compact_auto_fallback_attempted"))
	require.True(t, c.GetBool("responses_compact_context_fallback_attempted"))
	require.False(t, c.GetBool("responses_compact_previous_response_id_fallback_attempted"))
	require.False(t, c.GetBool("responses_compact_summary_model_fallback_attempted"))
	require.Equal(t, "gpt-5.4", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactSummaryModel))
	require.Equal(t, "original_hit", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactStateLookup))
	require.Equal(t, "original_route", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactRouteDecision))
	require.Equal(t, dto.ResponsesCompactModeAuto, info.ChannelOtherSettings.ResponsesCompactMode)
	require.Equal(t, dto.ResponsesCompactModeAuto, info.ChannelMeta.ChannelOtherSettings.ResponsesCompactMode)
	_, exists := common.GetContextKey(c, constant.ContextKeyResponsesCompactStateScopeResult)
	require.False(t, exists)
	_, exists = common.GetContextKey(c, constant.ContextKeyResponsesCompactMarkerKind)
	require.False(t, exists)
	_, exists = common.GetContextKey(c, constant.ContextKeyResponsesCompactFallbackReason)
	require.False(t, exists)
	_, exists = common.GetContextKey(c, constant.ContextKeyResponsesCompactChannelSkip)
	require.False(t, exists)
	_, exists = common.GetContextKey(c, constant.ContextKeyResponsesCompactStateRestored)
	require.False(t, exists)
	_, exists = common.GetContextKey(c, constant.ContextKeyResponsesCompactModelChanged)
	require.False(t, exists)
	_, exists = common.GetContextKey(c, constant.ContextKeyResponsesCompactStateHash)
	require.False(t, exists)
	_, exists = common.GetContextKey(c, constant.ContextKeyResponsesPreviousIDAction)
	require.False(t, exists)
	_, exists = common.GetContextKey(c, constant.ContextKeyResponsesCompactSummaryModels)
	require.False(t, exists)
	_, exists = common.GetContextKey(c, constant.ContextKeyResponsesCompactVisibleOnly)
	require.False(t, exists)
	nativeFallback, exists := common.GetContextKeyType[service.ResponsesCompactNativeFallbackLog](c, constant.ContextKeyResponsesCompactNativeFallback)
	require.True(t, exists)
	require.True(t, nativeFallback.Attempted)
	require.Equal(t, http.StatusBadGateway, nativeFallback.StatusCode)
	require.Equal(t, false, c.Request.Context().Value(constant.ContextKeyResponsesCompactVisibleOnly))
}

func TestResponsesCompactFallbackContextDoesNotRestoreSettingsAcrossChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, 1)
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeAuto,
	})
	snapshot := snapshotResponsesCompactFallbackContext(c)
	common.SetContextKey(c, constant.ContextKeyChannelId, 2)
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   2,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeNative,
			},
		},
	}
	info.ChannelOtherSettings = dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	}

	restoreResponsesCompactFallbackContext(c, info, snapshot)

	require.Equal(t, dto.ResponsesCompactModeNative, info.ChannelMeta.ChannelOtherSettings.ResponsesCompactMode)
	require.Equal(t, dto.ResponsesCompactModeNative, info.ChannelOtherSettings.ResponsesCompactMode)
	ctxSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	require.True(t, ok)
	require.Equal(t, dto.ResponsesCompactModeNative, ctxSettings.ResponsesCompactMode)
}

func TestShouldFallbackResponsesCompactVisibleOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "input too large for context window",
		Code:    "context_length_exceeded",
	}, http.StatusRequestEntityTooLarge)

	require.True(t, shouldFallbackResponsesCompactVisibleOnly(c, info, err))

	common.SetContextKey(c, constant.ContextKeyResponsesCompactVisibleOnlyFallbackAttempted, true)
	require.False(t, shouldFallbackResponsesCompactVisibleOnly(c, info, err))
}

func TestShouldFallbackResponsesCompactVisibleOnlySkipsRemoteOpaqueCompaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	info.Request = compactPayloadRequest()
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "input too large for context window",
		Code:    "context_length_exceeded",
	}, http.StatusRequestEntityTooLarge)

	require.False(t, shouldFallbackResponsesCompactVisibleOnly(c, info, err))
}

func TestShouldFallbackResponsesCompactVisibleOnlySkipsAlreadyVisibleOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "input too large for context window",
		Code:    "context_length_exceeded",
	}, http.StatusRequestEntityTooLarge)

	setResponsesCompactVisibleOnly(c, true)

	require.False(t, shouldFallbackResponsesCompactVisibleOnly(c, info, err))
}

func TestShouldFallbackResponsesCompactPreviousResponseID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.Request = compactLocalSyntheticPayloadRequest()
	err := types.NewErrorWithStatusCode(service.ErrResponsesRESTPreviousIDUnsupported, types.ErrorCodeConvertRequestFailed, http.StatusBadRequest)

	require.True(t, shouldFallbackResponsesCompactPreviousResponseID(c, info, err))

	c.Set("responses_compact_previous_response_id_fallback_attempted", true)
	require.False(t, shouldFallbackResponsesCompactPreviousResponseID(c, info, err))
}

func TestShouldFallbackResponsesCompactPreviousResponseIDSkipsUpstreamPreviousID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.Request = &dto.OpenAIResponsesCompactionRequest{
		Model:              "gpt-5.5-openai-compact",
		PreviousResponseID: "resp_upstream_previous",
		Input:              []byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]`),
	}
	err := types.NewErrorWithStatusCode(service.ErrResponsesRESTPreviousIDUnsupported, types.ErrorCodeConvertRequestFailed, http.StatusBadRequest)

	require.False(t, shouldFallbackResponsesCompactPreviousResponseID(c, info, err))
}

func TestShouldFallbackResponsesCompactPreviousResponseIDSkipsRemoteOpaqueCompactionByRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.Request = compactPayloadRequest()
	err := types.NewErrorWithStatusCode(service.ErrResponsesRESTPreviousIDUnsupported, types.ErrorCodeConvertRequestFailed, http.StatusBadRequest)

	require.False(t, shouldFallbackResponsesCompactPreviousResponseID(c, info, err))
}

func TestResponsesCompactPreviousResponseIDUnsupportedErrorMatchesSub2APIMessage(t *testing.T) {
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "previous_response_id is only supported on Responses WebSocket v2",
		Code:    "invalid_request_error",
	}, http.StatusBadRequest)

	require.True(t, isResponsesCompactPreviousResponseIDUnsupportedError(err))
}

func TestResponsesCompactPreviousResponseIDUnsupportedErrorIgnoresGenericWebSocketV2Message(t *testing.T) {
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "Responses WebSocket v2 session expired",
		Code:    "invalid_request_error",
	}, http.StatusBadRequest)

	require.False(t, isResponsesCompactPreviousResponseIDUnsupportedError(err))
}

func TestShouldFallbackResponsesCompactSummaryModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	info.Request = compactVisiblePayloadRequest()
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "input too large for context window",
		Code:    "context_length_exceeded",
	}, http.StatusRequestEntityTooLarge)

	require.True(t, shouldFallbackResponsesCompactSummaryModel(c, info, err))

	c.Set("responses_compact_summary_model_fallback_attempted", true)
	require.False(t, shouldFallbackResponsesCompactSummaryModel(c, info, err))
}

func TestShouldFallbackResponsesCompactSummaryModelSkipsRemoteOpaqueCompaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	info.Request = compactPayloadRequest()
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "context window exceeded",
		Code:    "context_length_exceeded",
	}, http.StatusBadRequest)

	require.False(t, shouldFallbackResponsesCompactSummaryModel(c, info, err))
}

func TestShouldFallbackResponsesCompactSummaryModelSkipsCurrentModelOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.UpstreamModelName = "gpt-5.4"
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	info.ChannelOtherSettings.ResponsesCompactSummaryFallbackModels = []string{"gpt-5.4"}
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "context window exceeded",
		Code:    "context_length_exceeded",
	}, http.StatusBadRequest)

	require.False(t, shouldFallbackResponsesCompactSummaryModel(c, info, err))
}

func TestResponsesCompactContextLengthErrorRejectsModelLookup(t *testing.T) {
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "model gpt-5.4 was not found",
		Code:    string(types.ErrorCodeBadResponseStatusCode),
	}, http.StatusNotFound)

	require.False(t, isResponsesCompactContextLengthError(err))
}

func TestResponsesCompactContextLengthErrorRejectsGenericLimit(t *testing.T) {
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "quota exceeds the limit for this account",
		Code:    string(types.ErrorCodeBadResponseStatusCode),
	}, http.StatusBadRequest)

	require.False(t, isResponsesCompactContextLengthError(err))
}

func TestResponsesCompactContextLengthErrorAcceptsUpstreamRequestTooLarge(t *testing.T) {
	err := types.NewOpenAIError(
		errors.New("upstream request too large: status code 413"),
		types.ErrorCodeUpstreamRequestTooLarge,
		http.StatusRequestEntityTooLarge,
	)

	require.True(t, isResponsesCompactContextLengthError(err))
}

func TestFormatNoAvailableChannelErrorMessageIncludesLastError(t *testing.T) {
	lastErr := types.NewErrorWithStatusCode(
		errors.New("upstream transport interrupted: do request failed"),
		types.ErrorCodeUpstreamTransportInterrupted,
		http.StatusBadGateway,
	)

	message := formatNoAvailableChannelErrorMessage("default", "claude-opus-4-8-thinking", lastErr)

	require.Contains(t, message, "分组 default 下模型 claude-opus-4-8-thinking 的可用渠道不存在（retry）")
	require.Contains(t, message, "上一错误")
	require.Contains(t, message, "status_code=502")
	require.Contains(t, message, "upstream transport interrupted")
}

func compactAutoFallbackRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelId:   1,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeAuto,
			},
		},
	}
}

func compactPayloadRequest() *dto.OpenAIResponsesCompactionRequest {
	return &dto.OpenAIResponsesCompactionRequest{
		Model: "gpt-5.5-openai-compact",
		Input: []byte(`[{"type":"compaction","encrypted_content":"opaque"}]`),
	}
}

func compactVisiblePayloadRequest() *dto.OpenAIResponsesCompactionRequest {
	return &dto.OpenAIResponsesCompactionRequest{
		Model: "gpt-5.5-openai-compact",
		Input: []byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]`),
	}
}

func compactLocalSyntheticPayloadRequest() *dto.OpenAIResponsesCompactionRequest {
	return &dto.OpenAIResponsesCompactionRequest{
		Model:              "gpt-5.5-openai-compact",
		PreviousResponseID: "resp_newapi_synthcmp_missing",
		Input:              []byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]`),
	}
}

func compactContentRejectionPayloadRequest() *dto.OpenAIResponsesCompactionRequest {
	return &dto.OpenAIResponsesCompactionRequest{
		Model:              "gpt-5.5-openai-compact",
		PreviousResponseID: "resp_native_payload",
		Input:              []byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"unsafe user prompt"}]}]`),
	}
}

type failingSeekBody struct{}

func (failingSeekBody) Read(_ []byte) (int, error) {
	return 0, errors.New("read not used")
}

func (failingSeekBody) Seek(_ int64, _ int) (int64, error) {
	return 0, errors.New("seek failed")
}
