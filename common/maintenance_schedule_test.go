package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMaintenanceScheduleStaggersBackgroundLoops(t *testing.T) {
	require.Equal(t, 15*time.Second, MidjourneyTaskPollingInterval)
	require.Equal(t, 15*time.Second, TaskPollingInterval)

	delays := []time.Duration{
		MidjourneyTaskPollingInitialDelay,
		ChannelCacheSyncInitialDelay,
		TaskPollingInitialDelay,
		SubscriptionResetInitialDelay,
		OptionsSyncInitialDelay,
	}
	require.Equal(t, []time.Duration{
		5 * time.Second,
		7 * time.Second,
		10 * time.Second,
		20 * time.Second,
		37 * time.Second,
	}, delays)
}
