package controller

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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
			name:       "native payload disallowed content falls back",
			statusCode: http.StatusBadRequest,
			message:    "request contains disallowed content",
			want:       true,
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

			require.Equal(t, tc.want, shouldFallbackResponsesCompactAuto(c, compactAutoFallbackRelayInfo(), err))
			_, exists := c.Get("responses_compact_auto_fallback_attempted")
			require.False(t, exists)
		})
	}
}

func TestResponsesCompactNativeCompatibilityPayloadRejection(t *testing.T) {
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "request contains disallowed content",
		Code:    string(types.ErrorCodeBadResponseStatusCode),
	}, http.StatusBadRequest)

	require.True(t, isResponsesCompactNativeCompatibilityError(err))
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

func TestShouldFallbackResponsesCompactAutoSkipsActiveFallbackWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := compactAutoFallbackRelayInfo()
	info.ChannelOtherSettings.ResponsesCompactAutoFallbackDate = dto.ResponsesCompactAutoFallbackDate(time.Now())
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
