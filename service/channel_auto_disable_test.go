package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestShouldDisableChannelDoesNotAutoDisableTransientTransportInterruption(t *testing.T) {
	originalEnabled := common.AutomaticDisableChannelEnabled
	originalRanges := operation_setting.AutomaticDisableStatusCodeRanges
	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 500, End: 599}}
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = originalEnabled
		operation_setting.AutomaticDisableStatusCodeRanges = originalRanges
	})

	transportInterruption := types.NewOpenAIError(
		errors.New("stream error: stream ID 37; INTERNAL_ERROR; received from peer"),
		types.ErrorCodeUpstreamTransportInterrupted,
		http.StatusBadGateway,
	)
	durableUpstreamFailure := types.NewOpenAIError(
		errors.New("upstream returned a durable server failure"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)

	require.False(t, ShouldDisableChannel(transportInterruption))
	require.True(t, ShouldDisableChannel(durableUpstreamFailure))
}
