package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestInstallChannelCandidatePreflightIsScopedToRetryAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}
	var calls int
	retry := &service.RetryParam{
		Ctx:   c,
		Retry: constantIntPointer(0),
		CandidatePreflight: func(channel *model.Channel) (bool, error) {
			calls++
			return channel != nil, nil
		},
	}
	installChannelCandidatePreflight(c, info, retry)

	channelA := &model.Channel{Id: 301}
	channelB := &model.Channel{Id: 302}
	allowed, err := retry.CandidatePreflight(channelA)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = retry.CandidatePreflight(channelA)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 1, calls, "same channel and retry attempt should be memoized")

	allowed, err = retry.CandidatePreflight(channelB)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 2, calls, "different channel must have an independent memo entry")

	retry.SetRetry(1)
	allowed, err = retry.CandidatePreflight(channelA)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 3, calls, "a retry must re-run request-aware preflight")
}

func constantIntPointer(value int) *int {
	return &value
}
