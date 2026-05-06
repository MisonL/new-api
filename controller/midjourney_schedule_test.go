package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyTaskPollingIntervalStaysAtFifteenSeconds(t *testing.T) {
	require.Equal(t, 15*time.Second, common.MidjourneyTaskPollingInterval)
	require.Equal(t, 5*time.Second, common.MidjourneyTaskPollingInitialDelay)
}
