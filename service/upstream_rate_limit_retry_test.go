package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpstreamRateLimitRetryStateAllowsOneFollowupAttempt(t *testing.T) {
	previousLimit := operation_setting.GetUpstreamRateLimitRetryLimit()
	operation_setting.SetUpstreamRateLimitRetryLimit(1)
	t.Cleanup(func() {
		operation_setting.SetUpstreamRateLimitRetryLimit(previousLimit)
	})

	ctx, _ := gin.CreateTestContext(nil)
	ObserveUpstreamRateLimitRetry(ctx, 206)

	state, ok := GetUpstreamRateLimitRetryState(ctx)
	require.True(t, ok)
	require.True(t, state.Triggered)
	require.Equal(t, 206, state.TriggerChannelID)
	require.Equal(t, 1, state.Limit)
	require.Zero(t, state.Used)
	require.False(t, state.Terminal)

	require.True(t, StartUpstreamRateLimitRetryFollowup(ctx))
	require.Equal(t, 1, state.Used)
	require.True(t, state.Terminal)

	require.False(t, StartUpstreamRateLimitRetryFollowup(ctx))
	require.Equal(t, 1, state.Used)
}

func TestUpstreamRateLimitRetryStateTerminatesWhenLimitIsZero(t *testing.T) {
	previousLimit := operation_setting.GetUpstreamRateLimitRetryLimit()
	operation_setting.SetUpstreamRateLimitRetryLimit(0)
	t.Cleanup(func() {
		operation_setting.SetUpstreamRateLimitRetryLimit(previousLimit)
	})

	ctx, _ := gin.CreateTestContext(nil)
	ObserveUpstreamRateLimitRetry(ctx, 205)

	state, ok := GetUpstreamRateLimitRetryState(ctx)
	require.True(t, ok)
	require.True(t, state.Terminal)
	require.Zero(t, state.Used)
}

func TestTerminateUpstreamRateLimitRetryStopsFurtherRetries(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	ObserveUpstreamRateLimitRetry(ctx, 206)
	TerminateUpstreamRateLimitRetry(ctx)

	state, ok := GetUpstreamRateLimitRetryState(ctx)
	require.True(t, ok)
	require.True(t, state.Terminal)
}
