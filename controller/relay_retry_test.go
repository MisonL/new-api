package controller

import (
	"errors"
	"net/http"
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
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestShouldRetryResponsesCompactTimeoutStatusCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name       string
		relayMode  int
		statusCode int
		want       bool
	}{
		{
			name:       "compact retries gateway timeout",
			relayMode:  relayconstant.RelayModeResponsesCompact,
			statusCode: http.StatusGatewayTimeout,
			want:       true,
		},
		{
			name:       "compact retries upstream 524 timeout",
			relayMode:  relayconstant.RelayModeResponsesCompact,
			statusCode: 524,
			want:       true,
		},
		{
			name:       "normal responses keep legacy 524 behavior",
			relayMode:  relayconstant.RelayModeResponses,
			statusCode: 524,
			want:       false,
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

func TestShouldRetryResponsesCompactTimeoutStillHonorsRetryBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("relay_mode", relayconstant.RelayModeResponsesCompact)
	err := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, 524)

	require.False(t, shouldRetry(c, err, 0))
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
		name       string
		statusCode int
		message    string
		want       bool
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
			name:       "native payload content rejection falls back",
			statusCode: http.StatusBadRequest,
			message:    "请求包含不允许的内容，请修改后重试",
			want:       true,
		},
		{
			name:       "native payload spaced chinese content rejection falls back",
			statusCode: http.StatusBadRequest,
			message:    "请求 包含 不允许 的内容，请修改后重试",
			want:       true,
		},
		{
			name:       "native payload disallowed content falls back",
			statusCode: http.StatusBadRequest,
			message:    "request contains disallowed content",
			want:       true,
		},
		{
			name:       "native payload input disallowed content falls back",
			statusCode: http.StatusBadRequest,
			message:    "input contains disallowed content",
			want:       true,
		},
		{
			name:       "native payload content not allowed falls back",
			statusCode: http.StatusBadRequest,
			message:    "content is not allowed",
			want:       true,
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
			info.Request = compactPayloadRequest()
			require.Equal(t, tc.want, shouldFallbackResponsesCompactAuto(c, info, err))
			_, exists := c.Get("responses_compact_auto_fallback_attempted")
			require.False(t, exists)
		})
	}
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

func TestResponsesCompactFallbackContextSnapshotRestoresFailedAttemptMarkers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("responses_compact_context_fallback_attempted", true)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, "gpt-5.4")

	snapshot := snapshotResponsesCompactFallbackContext(c)
	c.Set("responses_compact_auto_fallback_attempted", true)
	c.Set("responses_compact_summary_model_fallback_attempted", true)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, "gpt-5.3")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModels, []string{"gpt-5.3"})

	restoreResponsesCompactFallbackContext(c, snapshot)

	require.False(t, c.GetBool("responses_compact_auto_fallback_attempted"))
	require.True(t, c.GetBool("responses_compact_context_fallback_attempted"))
	require.False(t, c.GetBool("responses_compact_summary_model_fallback_attempted"))
	require.Equal(t, "gpt-5.4", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactSummaryModel))
	_, exists := common.GetContextKey(c, constant.ContextKeyResponsesCompactSummaryModels)
	require.False(t, exists)
}

func TestShouldFallbackResponsesCompactSummaryModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "input too large for context window",
		Code:    "context_length_exceeded",
	}, http.StatusRequestEntityTooLarge)

	require.True(t, shouldFallbackResponsesCompactSummaryModel(c, info, err))

	c.Set("responses_compact_summary_model_fallback_attempted", true)
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

type failingSeekBody struct{}

func (failingSeekBody) Read(_ []byte) (int, error) {
	return 0, errors.New("read not used")
}

func (failingSeekBody) Seek(_ int64, _ int) (int64, error) {
	return 0, errors.New("seek failed")
}
