package xunfei

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestXunfeiWebSocketHandshakePreservesRateLimitAndConsumesFollowup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLimit := operation_setting.GetUpstreamRateLimitRetryLimit()
	operation_setting.SetUpstreamRateLimitRetryLimit(1)
	t.Cleanup(func() {
		operation_setting.SetUpstreamRateLimitRetryLimit(previousLimit)
	})

	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	service.ObserveUpstreamRateLimitRetry(ctx, 206)
	_, _, err := xunfeiMakeRequest(
		ctx,
		dto.GeneralOpenAIRequest{},
		"general",
		"ws"+strings.TrimPrefix(upstream.URL, "http"),
		"app-id",
	)

	require.NotNil(t, err)
	require.Equal(t, types.ErrorTypeOpenAIError, err.GetErrorType())
	require.Equal(t, http.StatusTooManyRequests, err.StatusCode)
	state, ok := service.GetUpstreamRateLimitRetryState(ctx)
	require.True(t, ok)
	require.Equal(t, 1, state.Used)
	require.True(t, state.Terminal)

	_, _, err = xunfeiMakeRequest(
		ctx,
		dto.GeneralOpenAIRequest{},
		"general",
		"ws"+strings.TrimPrefix(upstream.URL, "http"),
		"app-id",
	)
	require.NotNil(t, err)
	require.Equal(t, http.StatusTooManyRequests, err.StatusCode)
	require.Equal(t, int32(1), calls.Load())
}
