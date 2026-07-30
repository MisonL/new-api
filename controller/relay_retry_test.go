package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	"github.com/QuantumNous/new-api/setting/operation_setting"
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

func TestShouldRetrySuppressesOpaqueEncryptedReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		err  *types.NewAPIError
	}{
		{
			name: "channel error",
			err:  types.NewError(errors.New("channel unavailable"), types.ErrorCodeGetChannelFailed),
		},
		{
			name: "temporary upstream error",
			err:  types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
		},
		{
			name: "rate limit",
			err:  types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(nil)
			relaycommon.MarkResponsesEncryptedReasoningReplayDecision(ctx, dto.ResponsesEncryptedReasoningReplayCapabilitySupported)

			require.False(t, shouldRetry(ctx, tt.err, 3))
			require.Equal(t, "suppressed_cross_channel", common.GetContextKeyString(ctx, constant.ContextKey("responses_encrypted_reasoning_replay_retry_action")))
		})
	}
}

func TestShouldRetryReadResponseBodyFailureRequiresSafeReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newReadFailure := func() *types.NewAPIError {
		return types.NewOpenAIError(
			errors.New("unexpected EOF while reading upstream response"),
			types.ErrorCodeReadResponseBodyFailed,
			http.StatusInternalServerError,
		)
	}

	t.Run("pre write active request remains retryable", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

		require.True(t, shouldRetry(ctx, newReadFailure(), 1))
	})

	t.Run("opaque replay never changes channel", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		relaycommon.MarkResponsesEncryptedReasoningReplayDecision(ctx, dto.ResponsesEncryptedReasoningReplayCapabilitySupported)

		require.False(t, shouldRetry(ctx, newReadFailure(), 1))
	})

	t.Run("cancelled client request does not retry", func(t *testing.T) {
		requestContext, cancel := context.WithCancel(context.Background())
		cancel()
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequestWithContext(requestContext, http.MethodPost, "/v1/responses", nil)

		require.False(t, shouldRetry(ctx, newReadFailure(), 1))
	})

	t.Run("written downstream response does not retry", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		_, writeErr := ctx.Writer.Write([]byte("data: partial response\n\n"))
		require.NoError(t, writeErr)

		require.False(t, shouldRetry(ctx, newReadFailure(), 1))
	})
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

func TestShouldRetryStopsAfterOneUpstreamRateLimitFollowup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLimit := operation_setting.GetUpstreamRateLimitRetryLimit()
	operation_setting.SetUpstreamRateLimitRetryLimit(1)
	t.Cleanup(func() {
		operation_setting.SetUpstreamRateLimitRetryLimit(previousLimit)
	})

	ctx, _ := gin.CreateTestContext(nil)
	firstRateLimit := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	service.ObserveUpstreamRateLimitRetry(ctx, 206)
	require.True(t, shouldRetry(ctx, firstRateLimit, 3))

	service.StartUpstreamRateLimitRetryFollowup(ctx)
	followupFailure := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	require.False(t, shouldRetry(ctx, followupFailure, 2))
}

func TestShouldRetryStopsImmediatelyWhenUpstreamRateLimitFollowupLimitIsZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLimit := operation_setting.GetUpstreamRateLimitRetryLimit()
	operation_setting.SetUpstreamRateLimitRetryLimit(0)
	t.Cleanup(func() {
		operation_setting.SetUpstreamRateLimitRetryLimit(previousLimit)
	})

	ctx, _ := gin.CreateTestContext(nil)
	service.ObserveUpstreamRateLimitRetry(ctx, 206)
	rateLimit := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	require.False(t, shouldRetry(ctx, rateLimit, 3))
}

func TestShouldRetryTaskStopsAfterOneUpstreamRateLimitFollowup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLimit := operation_setting.GetUpstreamRateLimitRetryLimit()
	operation_setting.SetUpstreamRateLimitRetryLimit(1)
	t.Cleanup(func() {
		operation_setting.SetUpstreamRateLimitRetryLimit(previousLimit)
	})

	ctx, _ := gin.CreateTestContext(nil)
	firstRateLimit := &dto.TaskError{StatusCode: http.StatusTooManyRequests}
	service.ObserveUpstreamRateLimitRetry(ctx, 206)
	require.True(t, shouldRetryTaskRelay(ctx, 206, firstRateLimit, 3))

	service.StartUpstreamRateLimitRetryFollowup(ctx)
	followupFailure := &dto.TaskError{StatusCode: http.StatusBadGateway}
	require.False(t, shouldRetryTaskRelay(ctx, 207, followupFailure, 2))
}

func TestShouldRetryTaskRelayDoesNotSwitchForLocalRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	taskErr := &dto.TaskError{
		StatusCode: http.StatusTooManyRequests,
		LocalError: true,
	}
	require.False(t, shouldRetryTaskRelay(ctx, 206, taskErr, 3))
}

func TestObserveUpstreamRateLimitRetryIgnoresLocalRelayError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	localErr := types.NewErrorWithStatusCode(
		errors.New("local rate limit"),
		types.ErrorCodePreConsumeTokenQuotaFailed,
		http.StatusTooManyRequests,
	)

	observeUpstreamRateLimitRetry(ctx, 206, localErr)
	_, ok := service.GetUpstreamRateLimitRetryState(ctx)
	require.False(t, ok)
}

func TestObserveUpstreamRateLimitRetryUsesOriginalMappedStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	upstreamErr := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	service.ResetStatusCode(upstreamErr, `{"429":503}`)

	observeUpstreamRateLimitRetry(ctx, 206, upstreamErr)
	state, ok := service.GetUpstreamRateLimitRetryState(ctx)
	require.True(t, ok)
	require.Equal(t, 206, state.TriggerChannelID)
}

func TestObserveUpstreamRateLimitRetrySkipsOpaqueResponsesReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetResponsesRequestMetadata(ctx, common.ResponsesRequestMetadata{HasOpaqueEncryptedContent: true})
	upstreamErr := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)

	observeUpstreamRateLimitRetry(ctx, 206, upstreamErr)

	_, ok := service.GetUpstreamRateLimitRetryState(ctx)
	require.False(t, ok)
}

func TestObserveUpstreamRateLimitRetryRecordsNonOpaqueResponsesReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetResponsesRequestMetadata(ctx, common.ResponsesRequestMetadata{HasOpaqueEncryptedContent: false})
	upstreamErr := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)

	observeUpstreamRateLimitRetry(ctx, 206, upstreamErr)

	state, ok := service.GetUpstreamRateLimitRetryState(ctx)
	require.True(t, ok)
	require.Equal(t, 206, state.TriggerChannelID)
}

func TestShouldStopLockedTaskUpstreamRateLimitRetry(t *testing.T) {
	lockedInfo := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{LockedChannel: &model.Channel{Id: 206}},
	}
	unlockedInfo := &relaycommon.RelayInfo{}
	upstreamRateLimit := &dto.TaskError{StatusCode: http.StatusTooManyRequests}
	localRateLimit := &dto.TaskError{StatusCode: http.StatusTooManyRequests, LocalError: true}

	require.True(t, shouldStopLockedTaskUpstreamRateLimitRetry(lockedInfo, upstreamRateLimit))
	require.False(t, shouldStopLockedTaskUpstreamRateLimitRetry(lockedInfo, localRateLimit))
	require.False(t, shouldStopLockedTaskUpstreamRateLimitRetry(unlockedInfo, upstreamRateLimit))
}

func TestAppendChannelErrorAdminInfoIncludesUpstreamRateLimitRetryState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	service.ObserveUpstreamRateLimitRetry(ctx, 206)
	service.StartUpstreamRateLimitRetryFollowup(ctx)

	adminInfo := make(map[string]interface{})
	appendChannelErrorAdminInfo(ctx, adminInfo)
	require.Equal(t, 1, adminInfo["upstream_rate_limit_retry_limit"])
	require.Equal(t, 1, adminInfo["upstream_rate_limit_retry_used"])
	require.Equal(t, true, adminInfo["upstream_rate_limit_retry_terminal"])
	require.Equal(t, 206, adminInfo["upstream_rate_limit_trigger_channel_id"])
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

func TestShouldSkipChannelForResponsesCompactionTriggerIgnoresConditionalToolBridgeRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "conditional-tool-bridge",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{205},
			ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			Options: &model_setting.ProtocolConversionOptions{
				EnableCustomToolBridge:     true,
				OnlyWhenToolBridgeRequired: true,
			},
		}},
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := compactionTriggerRelayInfo()
	channel := &model.Channel{Id: 205, Type: constant.ChannelTypeOpenAI}

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
}

func TestShouldSkipChannelForResponsesRemoteCompactionIgnoresConditionalToolBridgeRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "conditional-tool-bridge",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{205},
			ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			Options: &model_setting.ProtocolConversionOptions{
				EnableCustomToolBridge:     true,
				OnlyWhenToolBridgeRequired: true,
			},
		}},
	}

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := remoteCompactionRelayInfo()
	channel := &model.Channel{Id: 205, Type: constant.ChannelTypeOpenAI}

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
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

func TestShouldRetryStopsAfterResponsesPayloadWasWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	_, writeErr := c.Writer.Write([]byte("event: response.output_text.delta\n\n"))
	require.NoError(t, writeErr)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, false)

	err := types.NewOpenAIError(
		errors.New("stream disconnected before completion"),
		types.ErrorCodeUpstreamTransportInterrupted,
		http.StatusBadGateway,
	)

	require.False(t, shouldRetry(c, err, 1))
}

func TestShouldRetryAllowsResponsesPreludeRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	_, writeErr := c.Writer.Write([]byte(": PING\n\n"))
	require.NoError(t, writeErr)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)

	err := types.NewOpenAIError(
		errors.New("stream disconnected before completion"),
		types.ErrorCodeUpstreamTransportInterrupted,
		http.StatusBadGateway,
	)

	require.True(t, shouldRetry(c, err, 1))
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

func TestRelayStopsAfterOneOutboundUpstreamRateLimitFollowup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelControllerTestDB(t)

	var firstCalls atomic.Int32
	var secondCalls atomic.Int32
	var thirdCalls atomic.Int32
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`))
	}))
	defer firstUpstream.Close()
	secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"gateway failure","type":"server_error","code":"server_error"}}`))
	}))
	defer secondUpstream.Close()
	thirdUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		thirdCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"gateway failure","type":"server_error","code":"server_error"}}`))
	}))
	defer thirdUpstream.Close()

	modelName := "relay-429-limit-test"
	initialQuota := 1_000_000
	priority := int64(10)
	weight := uint(1)
	autoBan := 0
	channels := []*model.Channel{
		{
			Id:       206,
			Name:     "rate-limited-upstream",
			Key:      "test-key-206",
			Type:     constant.ChannelTypeOpenAI,
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   modelName,
			BaseURL:  &firstUpstream.URL,
			Priority: &priority,
			Weight:   &weight,
			AutoBan:  &autoBan,
		},
		{
			Id:       207,
			Name:     "followup-upstream-a",
			Key:      "test-key-207",
			Type:     constant.ChannelTypeOpenAI,
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   modelName,
			BaseURL:  &secondUpstream.URL,
			Priority: &priority,
			Weight:   &weight,
			AutoBan:  &autoBan,
		},
		{
			Id:       208,
			Name:     "followup-upstream-b",
			Key:      "test-key-208",
			Type:     constant.ChannelTypeOpenAI,
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   modelName,
			BaseURL:  &thirdUpstream.URL,
			Priority: &priority,
			Weight:   &weight,
			AutoBan:  &autoBan,
		},
	}
	for _, channel := range channels {
		require.NoError(t, db.Create(channel).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group:     "default",
			Model:     modelName,
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}
	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "relay-429-limit-user",
		Password: "test-password",
		Quota:    initialQuota,
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	previousRetryTimes := common.RetryTimes
	previousLimit := operation_setting.GetUpstreamRateLimitRetryLimit()
	previousErrorLogEnabled := constant.ErrorLogEnabled
	previousCountToken := constant.CountToken
	common.RetryTimes = 3
	operation_setting.SetUpstreamRateLimitRetryLimit(1)
	constant.ErrorLogEnabled = false
	constant.CountToken = false
	service.InitHttpClient()
	t.Cleanup(func() {
		common.RetryTimes = previousRetryTimes
		operation_setting.SetUpstreamRateLimitRetryLimit(previousLimit)
		constant.ErrorLogEnabled = previousErrorLogEnabled
		constant.CountToken = previousCountToken
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/pg/v1/chat/completions",
		strings.NewReader(`{"model":"relay-429-limit-test","messages":[{"role":"user","content":"ping"}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("relay_mode", relayconstant.RelayModeChatCompletions)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 1)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{
		AcceptUnsetRatioModel: true,
		BillingPreference:     "wallet_only",
	})
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, channels[0], modelName))

	Relay(ctx, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Equal(t, int32(1), firstCalls.Load())
	require.Equal(t, int32(1), secondCalls.Load()+thirdCalls.Load())
	require.Len(t, ctx.GetStringSlice("use_channel"), 2)
	require.Equal(t, "206", ctx.GetStringSlice("use_channel")[0])
	require.Eventually(t, func() bool {
		var quota int
		if err := db.Model(&model.User{}).Where("id = ?", 1).Select("quota").Scan(&quota).Error; err != nil {
			return false
		}
		return quota == initialQuota
	}, time.Second, 10*time.Millisecond, "asynchronous billing refund did not finish before test cleanup")
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
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNative, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
	require.Equal(t, string(dto.ResponsesEncryptedReasoningReplayCapabilitySupported), common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: chatOnly.Id,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay,
	}})
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, native.Id, info.ChannelMeta.ChannelId)
}

func TestGetChannelAllResponsesViaChatRemoteCompactionInputKeepsNativeRoute(t *testing.T) {
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
		Enabled: true,
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

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNative, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: chatOnly.Id,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay,
	}})
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
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNative, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: chatOnly.Id,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay,
	}})

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
	require.ErrorIs(t, err, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
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
		Enabled: true,
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
	require.ErrorIs(t, err, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
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
		Enabled: true,
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
	require.ErrorIs(t, err, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
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
		Enabled: true,
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
		Enabled: true,
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
	capability := otherSettings.ResolveResponsesChannelCapability(channel.Type)
	require.Equal(t, dto.ResponsesCompactionItemPassthroughCapabilityUnknown, capability.CompactionItemPassthroughCapability)
	require.Nil(t, capability.SupportsCompactionItemPassthrough)
}

func TestResponsesLocalCompactionTriggerAllChannelsSkipsOnlyUnsupportedPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := compactionTriggerRelayInfo()
	rule := &model_setting.ProtocolConversionRule{AllChannels: true}
	legacyOpenAI := &model.Channel{
		Id:   178,
		Type: constant.ChannelTypeOpenAI,
	}
	genericProxy := &model.Channel{
		Id:   168,
		Type: constant.ChannelTypeOpenAI,
	}
	genericProxy.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
	})

	require.False(t, responsesLocalCompactionTriggerRuleRequiresChannelSkip(ctx, info, rule, legacyOpenAI))
	require.True(t, responsesLocalCompactionTriggerRuleRequiresChannelSkip(ctx, info, rule, genericProxy))
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
		Enabled: true,
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
	capability := otherSettings.ResolveResponsesChannelCapability(channel.Type)
	require.Equal(t, dto.ResponsesCompactionItemPassthroughCapabilityUnknown, capability.CompactionItemPassthroughCapability)
	require.Nil(t, capability.SupportsCompactionItemPassthrough)
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

func TestResponsesToChatPrecheckKeepsRestorableNativeOpaqueLocalState(t *testing.T) {
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
		ChannelId:   206,
		ChannelType: constant.ChannelTypeOpenAI,
	}
	info.Request = &dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}
	channel := &model.Channel{
		Id:   206,
		Type: constant.ChannelTypeOpenAI,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
	})
	require.NoError(t, db.Create(channel).Error)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, channel, "gpt-5.5"))
	info.InitChannelMeta(ctx)

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
		Enabled: true,
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
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNative, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
	require.Equal(t, string(dto.ResponsesEncryptedReasoningReplayCapabilitySupported), common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: proxy.Id,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay,
	}})
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
	require.ErrorIs(t, err, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
}

func TestGetChannelAllowsUnverifiedGenericOpenAIRemoteCompactionPassthrough(t *testing.T) {
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

	require.Nil(t, err)
	require.NotNil(t, selected)
	require.Equal(t, channel.Id, selected.Id)
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
	channelSettings := selected.GetOtherSettings()
	require.Equal(t, dto.ResponsesCompactionItemPassthroughCapabilityUnknown, channelSettings.ResolveResponsesCompactionItemPassthroughCapability(selected.Type))
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

func TestShouldNotSkipNativeResponsesChannelForNamespaceTools(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6",
			Input: common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]`),
			Tools: common.RawMessage(`[{"type":"namespace","name":"codex_app","tools":[{"type":"function","name":"read_thread","parameters":{"type":"object"}}]}]`),
		},
	}
	unknown := &model.Channel{Id: 99, Type: constant.ChannelTypeOpenAI}

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, unknown)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
}

func TestShouldSkipNativeResponsesChannelWhenNamespaceToolsExplicitlyUnsupported(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6",
			Input: common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]`),
			Tools: common.RawMessage(`[{"type":"namespace","name":"codex_app","tools":[{"type":"function","name":"read_thread","parameters":{"type":"object"}}]}]`),
		},
	}
	supportsNamespaceTools := false
	unsupported := &model.Channel{Id: 150, Type: constant.ChannelTypeOpenAI}
	unsupported.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsNamespaceTools: &supportsNamespaceTools,
		},
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, unsupported)

	require.NoError(t, err)
	require.True(t, skip)
	require.NotNil(t, info.LastError)
	require.True(t, errors.Is(info.LastError, errResponsesNamespaceToolsUnsupported))
}

func TestShouldSkipNativeResponsesChannelWhenToolSearchExplicitlyUnsupported(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"tool_search"}]},
				{"type":"message","role":"user","content":"find a tool"}
			]`),
		},
	}
	supportsToolSearch := false
	unsupported := &model.Channel{Id: 208, Type: constant.ChannelTypeOpenAI}
	unsupported.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsToolSearch: &supportsToolSearch,
		},
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, unsupported)

	require.NoError(t, err)
	require.True(t, skip)
	require.NotNil(t, info.LastError)
	require.True(t, errors.Is(info.LastError, errResponsesToolSearchUnsupported))
}

func TestShouldSkipNativeResponsesChannelWhenCustomToolsExplicitlyUnsupported(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6",
			Input: common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"apply the patch"}]}]`),
			Tools: common.RawMessage(`[{"type":"custom","name":"apply_patch","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}]`),
		},
	}
	supportsCustomTools := false
	unsupported := &model.Channel{Id: 155, Type: constant.ChannelTypeOpenAI}
	unsupported.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsCustomTools: &supportsCustomTools,
		},
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, unsupported)

	require.NoError(t, err)
	require.True(t, skip)
	require.NotNil(t, info.LastError)
	require.True(t, errors.Is(info.LastError, errResponsesCustomToolsUnsupported))
}

func TestGetChannelSkipsExplicitlyUnsupportedCustomToolChannel(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	unsupported, supported := seedRemoteCompactionRouteChannels(t, db)
	supportsCustomTools := false
	unsupported.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsCustomTools: &supportsCustomTools,
		},
	})
	require.NoError(t, db.Save(unsupported).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
	settings.PassThroughRequestEnabled = false

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, unsupported, "gpt-5.5"))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[{"type":"message","role":"user","content":"apply the patch"}]`),
			Tools: common.RawMessage(`[{"type":"custom","name":"apply_patch","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}]`),
		},
	}

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, supported.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
}

func TestGetChannelSkipsExplicitlyUnsupportedFunctionToolChannel(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	unsupported, supported := seedRemoteCompactionRouteChannels(t, db)
	supportsFunctionTools := false
	unsupported.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsFunctionTools: &supportsFunctionTools,
		},
	})
	require.NoError(t, db.Save(unsupported).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
	settings.PassThroughRequestEnabled = false

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, unsupported, "gpt-5.5"))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"additional_tools","role":"developer","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]},
				{"type":"message","role":"user","content":"call lookup"}
			]`),
			ToolChoice: common.RawMessage(`{"type":"function","name":"lookup"}`),
		},
	}

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, supported.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	require.True(t, errors.Is(info.LastError, errResponsesFunctionToolsUnsupported))
}

func TestGetChannelSkipsExplicitlyUnsupportedToolSearchChannel(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	unsupported, supported := seedRemoteCompactionRouteChannels(t, db)
	supportsToolSearch := false
	unsupported.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsToolSearch: &supportsToolSearch,
		},
	})
	require.NoError(t, db.Save(unsupported).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
	settings.PassThroughRequestEnabled = false

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, unsupported, "gpt-5.5"))
	info := toolSearchRelayInfo()

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, supported.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
}

func TestGetChannelSkipsExplicitlyUnsupportedNativeNamespaceChannel(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	unsupported, supported := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
	settings.PassThroughRequestEnabled = false

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, unsupported, "gpt-5.5"))
	info := namespaceToolRelayInfo(false)

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, supported.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, supported.Id, info.ChannelMeta.ChannelId)
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

func TestGetChannelExhaustsSamePriorityBeforeProtocolIncompatibleLowerPriority(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)
	setChannelPriorityForTest(t, db, chatOnly, 10)
	setChannelPriorityForTest(t, db, native, 20)
	failedPriority := int64(20)
	weight := uint(1)
	failed := &model.Channel{
		Id:       199,
		Name:     "failed-native-responses",
		Key:      "sk-failed-native",
		Type:     constant.ChannelTypeOpenAI,
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &failedPriority,
		Weight:   &weight,
	}
	require.NoError(t, db.Create(failed).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: failed.Id,
		Enabled:   true,
		Priority:  failed.Priority,
		Weight:    weight,
	}).Error)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(chatOnly.Id)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, failed, "gpt-5.5"))
	ctx.Set(relayInitialChannelConsumedKey, true)
	addUsedChannel(ctx, failed.Id)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[{"type":"message","role":"user","content":"apply the patch"}]`),
			Tools: common.RawMessage(`[{"type":"custom","name":"apply_patch","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}]`),
		},
		LastError: types.NewOpenAIError(
			errors.New("Service temporarily unavailable"),
			types.ErrorCodeBadResponseStatusCode,
			http.StatusServiceUnavailable,
		),
	}
	info.InitChannelMeta(ctx)

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(1),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	require.Equal(t, []string{"199"}, ctx.GetStringSlice("use_channel"))
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
		Enabled: true,
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
}

func TestResponsesToChatCompatibilityRequestForPrecheckDropsEmptyContextCompaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"context_compaction"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	prepared, err := responsesToChatCompatibilityRequestForPrecheck(ctx, &relaycommon.RelayInfo{}, request)

	require.NoError(t, err)
	require.NotSame(t, request, prepared)
	require.NotContains(t, string(prepared.Input), `"type":"context_compaction"`)
	require.Contains(t, string(prepared.Input), `"type":"message"`)
}

func TestGetChannelRoutesBridgeableNamespaceToolViaChatWhenBridgeEnabled(t *testing.T) {
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

func requireResponsesRoutingChannelSkips(t *testing.T, c *gin.Context, want []dto.ResponsesRoutingChannelSkip) {
	t.Helper()
	got, ok := common.GetContextKeyType[[]dto.ResponsesRoutingChannelSkip](c, constant.ContextKeyResponsesRoutingChannelSkips)
	require.True(t, ok)
	require.Equal(t, want, got)
}

func TestShouldSkipResponsesChannelWithExplicitlyUnsupportedEncryptedReasoningReplay(t *testing.T) {
	unsupported := false
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},
				{"type":"message","role":"user","content":"continue"}
			]`),
		},
	}
	channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsEncryptedReasoningReplay: &unsupported,
		},
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.True(t, skip)
	require.ErrorIs(t, info.LastError, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionRejectedUnsupported, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
	require.Equal(t, string(dto.ResponsesEncryptedReasoningReplayCapabilityUnsupported), common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: channel.Id,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay,
	}})
}

func TestShouldAllowUnknownResponsesEncryptedReasoningReplayCapability(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]`),
		},
	}
	channel := &model.Channel{Id: 160, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
}

func TestShouldSkipOpaqueReplayRequestWithKnownIncompatibleChannel(t *testing.T) {
	unsupported := false
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
			Model:                     "gpt-5.6-sol",
			ModelPresent:              true,
			InputPresent:              true,
			InputKind:                 common.ResponsesJSONValueKindArray,
			HasOpaqueEncryptedContent: true,
		}),
	}
	channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsEncryptedReasoningReplay: &unsupported,
		},
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.True(t, skip)
	require.ErrorIs(t, info.LastError, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: channel.Id,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay,
	}})
}

func TestShouldAllowUnknownOpaqueReplayRequestOnce(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
			Model:                     "gpt-5.6-sol",
			ModelPresent:              true,
			InputPresent:              true,
			InputKind:                 common.ResponsesJSONValueKindArray,
			HasOpaqueEncryptedContent: true,
		}),
	}
	channel := &model.Channel{Id: 160, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
	require.Equal(t, string(dto.ResponsesEncryptedReasoningReplayCapabilityUnknown), common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
}

func TestShouldSkipOpaqueReplayCompactionWithoutPassthrough(t *testing.T) {
	supported := true
	unsupported := false
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
			Model:                     "gpt-5.6-sol",
			ModelPresent:              true,
			InputPresent:              true,
			InputKind:                 common.ResponsesJSONValueKindArray,
			HasOpaqueEncryptedContent: true,
			HasCompactionItem:         true,
		}),
	}
	channel := &model.Channel{Id: 208, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericOpenAI,
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsEncryptedReasoningReplay:  &supported,
			SupportsCompactionItemPassthrough: &unsupported,
		},
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.True(t, skip)
	require.ErrorIs(t, info.LastError, errResponsesNativeCompactionUnsupported)
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionRejectedUnsupported, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: channel.Id,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedCompaction,
	}})
}

func TestShouldAllowUnknownOpaqueReplayCompactionPassthroughOnce(t *testing.T) {
	supported := true
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
			Model:                     "gpt-5.6-sol",
			ModelPresent:              true,
			InputPresent:              true,
			InputKind:                 common.ResponsesJSONValueKindArray,
			HasOpaqueEncryptedContent: true,
			HasCompactionItem:         true,
		}),
	}
	channel := &model.Channel{Id: 160, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsEncryptedReasoningReplay: &supported,
		},
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
	channelSettings := channel.GetOtherSettings()
	require.Equal(t, dto.ResponsesCompactionItemPassthroughCapabilityUnknown, channelSettings.ResolveResponsesCompactionItemPassthroughCapability(channel.Type))
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNative, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}

func TestShouldSkipOpaqueResponsesCompactChannelWithoutNativeCompact(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		Request: &dto.OpenAIResponsesCompactionRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]`),
		},
	}
	channel := &model.Channel{Id: 208, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode:     dto.ResponsesCompactModeSynthetic,
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.True(t, skip)
	require.ErrorIs(t, info.LastError, errResponsesNativeCompactionUnsupported)
	require.Equal(t, "channel_skipped_unsupported_compaction", common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
	require.Equal(t, string(dto.ResponsesEncryptedReasoningReplayCapabilitySupported), common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionRejectedUnsupported, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: channel.Id,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedCompaction,
	}})
}

func TestShouldAllowUnverifiedOpaqueRemoteCompactionPassthroughPreservesReplayCapability(t *testing.T) {
	supported := true
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[{"type":"compaction","encrypted_content":"opaque-reasoning-state"}]`),
		},
	}
	channel := &model.Channel{Id: 208, Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericOpenAI,
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsEncryptedReasoningReplay: &supported,
		},
	})

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
	require.Equal(t, string(dto.ResponsesEncryptedReasoningReplayCapabilitySupported), common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNative, common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip))
	_, hasRoutingSkips := common.GetContextKeyType[[]dto.ResponsesRoutingChannelSkip](ctx, constant.ContextKeyResponsesRoutingChannelSkips)
	require.False(t, hasRoutingSkips)
}

func TestShouldAllowOpaqueResponsesCompactOnNativeCodexChannel(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		Request: &dto.OpenAIResponsesCompactionRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]`),
		},
	}
	channel := &model.Channel{Id: 206, Type: constant.ChannelTypeCodex}

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
}

func TestGetChannelSkipsUnsupportedEncryptedReasoningReplayChannel(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	unsupported, supported := seedRemoteCompactionRouteChannels(t, db)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, unsupported, "gpt-5.5"))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]`),
		},
	}

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, supported.Id, channel.Id)
	require.Equal(t, []string{"168"}, ctx.GetStringSlice("use_channel"))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: unsupported.Id,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay,
	}})
}

func TestGetChannelSpecificUnsupportedEncryptedReasoningReplayReturns503(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	unsupported, _ := seedRemoteCompactionRouteChannels(t, db)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("specific_channel_id", "168")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, unsupported, "gpt-5.5"))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]`),
		},
	}

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.ErrorIs(t, err, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
}

func TestGetChannelAllUnsupportedEncryptedReasoningReplayReturns503(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	first, second := seedRemoteCompactionRouteChannels(t, db)
	second.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileChatOnlyProxy,
	})
	require.NoError(t, db.Save(second).Error)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, first, "gpt-5.5"))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]`),
		},
	}

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.ErrorIs(t, err, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{
		{ChannelID: first.Id, Reason: responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay},
		{ChannelID: second.Id, Reason: responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay},
	})
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
	chatOnly.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileChatOnlyProxy,
	})
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
		Enabled: true,
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
				{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:nffffffffffffffffffffffffffffffff:resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_66666666666666666666666666666666"},
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
				{"type":"namespace","name":"codex","tools":[{"type":"` + nestedToolType + `","name":"repo_status","description":"inspect repo","parameters":{"type":"object","properties":{}}}]}
			]`),
		},
	}
}

func toolSearchRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"tool_search"}]},
				{"type":"message","role":"user","content":"find a tool"}
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

func TestShouldFallbackRemoteResponsesCompaction(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newInfo := func(input string) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RelayMode:       relayconstant.RelayModeResponses,
			IsStream:        true,
			OriginModelName: "gpt-5.6-sol",
			Request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: common.RawMessage(input),
			},
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelId:   169,
				ChannelType: constant.ChannelTypeOpenAI,
				ChannelOtherSettings: dto.ChannelOtherSettings{
					ResponsesCompactMode: dto.ResponsesCompactModeNative,
				},
			},
		}
	}
	visibleInput := `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"compact this conversation"}]},
		{"type":"compaction_trigger"}
	]`
	opaqueInput := `[
		{"type":"compaction","encrypted_content":"opaque-native-compact"},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},
		{"type":"compaction_trigger"}
	]`

	testCases := []struct {
		name       string
		statusCode int
		errorCode  types.ErrorCode
		input      string
		want       bool
	}{
		{name: "bad gateway recovers", statusCode: http.StatusBadGateway, input: visibleInput, want: true},
		{name: "first data timeout recovers", statusCode: http.StatusBadGateway, errorCode: types.ErrorCodeUpstreamTransportInterrupted, input: visibleInput, want: true},
		{name: "service unavailable recovers", statusCode: http.StatusServiceUnavailable, input: visibleInput, want: true},
		{name: "upstream request too large recovers", statusCode: http.StatusRequestEntityTooLarge, errorCode: types.ErrorCodeUpstreamRequestTooLarge, input: visibleInput, want: true},
		{name: "unclassified request too large does not recover", statusCode: http.StatusRequestEntityTooLarge, input: visibleInput, want: false},
		{name: "rate limit keeps cross channel retry", statusCode: http.StatusTooManyRequests, input: visibleInput, want: false},
		{name: "opaque compaction cannot recover locally", statusCode: http.StatusBadGateway, input: opaqueInput, want: false},
		{name: "ordinary responses do not recover", statusCode: http.StatusBadGateway, input: `[{"type":"message","role":"user","content":"hello"}]`, want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("X-Codex-Beta-Features", "remote_compaction_v2")
			errorCode := tc.errorCode
			if errorCode == "" {
				errorCode = types.ErrorCodeBadResponseStatusCode
			}
			err := types.NewOpenAIError(
				errors.New("upstream service temporarily unavailable"),
				errorCode,
				tc.statusCode,
			)

			require.Equal(t, tc.want, shouldFallbackRemoteResponsesCompaction(c, newInfo(tc.input), err))
		})
	}

	t.Run("missing protocol header does not recover", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		err := types.NewOpenAIError(errors.New("bad gateway"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
		require.False(t, shouldFallbackRemoteResponsesCompaction(c, newInfo(visibleInput), err))
	})
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
	common.SetContextKey(c, constant.ContextKeyResponsesCompactCodexContextPruned, true)
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
	attemptInfo := compactAutoFallbackRelayInfo()
	markResponsesCompactSyntheticFallbackAttempted(c, attemptInfo)
	require.True(t, responsesCompactSyntheticFallbackAttemptedForChannel(c, attemptInfo.ChannelMeta.ChannelId))

	info := compactAutoFallbackRelayInfo()
	info.ChannelMeta.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic

	restoreResponsesCompactFallbackContext(c, info, snapshot)

	require.False(t, c.GetBool("responses_compact_auto_fallback_attempted"))
	require.True(t, c.GetBool("responses_compact_context_fallback_attempted"))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned))
	require.False(t, c.GetBool("responses_compact_previous_response_id_fallback_attempted"))
	require.False(t, responsesCompactSyntheticFallbackAttemptedForChannel(c, info.ChannelMeta.ChannelId))
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

func TestShouldFallbackResponsesCompactSummaryModelRecoversSyntheticServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	info.Request = compactVisiblePayloadRequest()

	retryable := types.NewErrorWithStatusCode(errors.New("bad response status code 502"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	require.True(t, shouldFallbackResponsesCompactSummaryModel(c, info, retryable))

	rateLimited := types.NewErrorWithStatusCode(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	require.False(t, shouldFallbackResponsesCompactSummaryModel(c, info, rateLimited))

	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeNative
	require.False(t, shouldFallbackResponsesCompactSummaryModel(c, info, retryable))

	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeAuto
	info.RelayMode = relayconstant.RelayModeResponses
	common.SetContextKey(c, constant.ContextKeyResponsesRemoteCompactSyntheticFallback, true)
	require.True(t, shouldFallbackResponsesCompactSummaryModel(c, info, retryable))

	common.SetContextKey(c, constant.ContextKeyResponsesRemoteCompactSyntheticFallback, false)
	require.False(t, shouldFallbackResponsesCompactSummaryModel(c, info, retryable))
}

func TestShouldFallbackResponsesCompactSummaryModelSupportsCodex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelMeta.ChannelType = constant.ChannelTypeCodex
	info.UpstreamModelName = "gpt-5.5"
	info.Request = compactVisiblePayloadRequest()
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "context window exceeded",
		Code:    "context_length_exceeded",
	}, http.StatusBadRequest)

	require.True(t, shouldFallbackResponsesCompactSummaryModel(c, info, err))
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
		PreviousResponseID: "resp_newapi_synthcmp_55555555555555555555555555555555",
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
