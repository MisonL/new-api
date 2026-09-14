package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	rootcommon "github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsDeterministicModelNotFoundError(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		err        *types.NewAPIError
		want       bool
	}{
		{
			name:       "structured model not found at bad request",
			statusCode: http.StatusBadRequest,
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "Model gpt-5.6-sol not found",
				Type:    "invalid_request_error",
				Code:    string(types.ErrorCodeModelNotFound),
			}, http.StatusBadRequest),
			want: true,
		},
		{
			name:       "message model not found at not found",
			statusCode: http.StatusNotFound,
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "The requested model was not found",
				Type:    "invalid_request_error",
				Code:    string(types.ErrorCodeBadResponseStatusCode),
			}, http.StatusNotFound),
			want: true,
		},
		{
			name:       "temporary model unavailable is not deterministic",
			statusCode: http.StatusBadRequest,
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "model is temporarily unavailable",
				Type:    "server_error",
				Code:    string(types.ErrorCodeBadResponseStatusCode),
			}, http.StatusBadRequest),
			want: false,
		},
		{
			name:       "endpoint not found is not a model lookup failure",
			statusCode: http.StatusNotFound,
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "Endpoint not found for model gpt-5.6-sol",
				Type:    "invalid_request_error",
				Code:    string(types.ErrorCodeBadResponseStatusCode),
			}, http.StatusNotFound),
			want: false,
		},
		{
			name:       "other status is not deterministic",
			statusCode: http.StatusBadGateway,
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "model gpt-5.6-sol not found",
				Type:    "invalid_request_error",
				Code:    string(types.ErrorCodeModelNotFound),
			}, http.StatusBadGateway),
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isDeterministicModelNotFoundError(tc.err))
			require.Equal(t, tc.statusCode, tc.err.StatusCode)
		})
	}
}

func TestRelayModelNotFoundStatePreservesOriginalErrorWhenAllAttemptsMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	modelErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Model gpt-5.6-sol not found",
		Type:    "invalid_request_error",
		Code:    string(types.ErrorCodeModelNotFound),
	}, http.StatusBadRequest)

	recordRelayErrorAttempt(ctx, modelErr)
	lastModelErr := types.WithOpenAIError(types.OpenAIError{
		Message: "unknown model gpt-5.6-sol",
		Type:    "invalid_request_error",
		Code:    string(types.ErrorCodeModelNotFound),
	}, http.StatusNotFound)
	recordRelayErrorAttempt(ctx, lastModelErr)

	terminalErr := terminalRelayErrorAfterChannelExhaustion(ctx, types.NewError(
		errors.New("no available channel"),
		types.ErrorCodeGetChannelFailed,
		types.ErrOptionWithSkipRetry(),
	))

	require.Same(t, lastModelErr, terminalErr)
	require.Equal(t, http.StatusNotFound, terminalErr.StatusCode)
	require.Equal(t, types.ErrorCodeModelNotFound, terminalErr.GetErrorCode())
}

func TestRelayModelNotFoundStateDoesNotPreserveAfterOtherFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	modelErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Model gpt-5.6-sol not found",
		Type:    "invalid_request_error",
		Code:    string(types.ErrorCodeModelNotFound),
	}, http.StatusNotFound)
	otherErr := types.NewOpenAIError(
		errors.New("upstream connection failed"),
		types.ErrorCodeUpstreamTransportInterrupted,
		http.StatusBadGateway,
	)

	recordRelayErrorAttempt(ctx, modelErr)
	recordRelayErrorAttempt(ctx, otherErr)

	terminalErr := terminalRelayErrorAfterChannelExhaustion(ctx, otherErr)

	require.Same(t, otherErr, terminalErr)
}

func TestGetChannelPreservesModelNotFoundAfterCandidateExhaustion(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	first, second := seedRateLimitRetryChannels(t, db)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	addUsedChannel(ctx, first.Id)
	addUsedChannel(ctx, second.Id)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeChatCompletions,
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		OriginModelName: "gpt-5.5",
		LastError: types.WithOpenAIError(types.OpenAIError{
			Message: "Model gpt-5.5 not found",
			Type:    "invalid_request_error",
			Code:    string(types.ErrorCodeModelNotFound),
		}, http.StatusBadRequest),
	}
	recordRelayErrorAttempt(ctx, info.LastError)

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      rootcommon.GetPointer(1),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Equal(t, types.ErrorCodeModelNotFound, err.GetErrorCode())
}
