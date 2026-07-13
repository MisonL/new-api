package volcengine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHandleTTSWebSocketResponsePreservesRateLimitAndConsumesFollowup(t *testing.T) {
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
	_, err := handleTTSWebSocketResponse(
		ctx,
		"ws"+strings.TrimPrefix(upstream.URL, "http"),
		VolcengineTTSRequest{},
		&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "app-id|access-token"}},
		"mp3",
	)

	require.NotNil(t, err)
	require.Equal(t, types.ErrorTypeOpenAIError, err.GetErrorType())
	require.Equal(t, http.StatusTooManyRequests, err.StatusCode)
	state, ok := service.GetUpstreamRateLimitRetryState(ctx)
	require.True(t, ok)
	require.Equal(t, 1, state.Used)
	require.True(t, state.Terminal)

	_, err = handleTTSWebSocketResponse(
		ctx,
		"ws"+strings.TrimPrefix(upstream.URL, "http"),
		VolcengineTTSRequest{},
		&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "app-id|access-token"}},
		"mp3",
	)
	require.NotNil(t, err)
	require.Equal(t, http.StatusTooManyRequests, err.StatusCode)
	require.Equal(t, int32(1), calls.Load())
}
