package common

import "time"

const (
	TaskPollingInterval               = 15 * time.Second
	MidjourneyTaskPollingInterval     = 15 * time.Second
	MidjourneyTaskPollingInitialDelay = 5 * time.Second
	ChannelCacheSyncInitialDelay      = 7 * time.Second
	TaskPollingInitialDelay           = 10 * time.Second
	SubscriptionResetInitialDelay     = 20 * time.Second
	OptionsSyncInitialDelay           = 37 * time.Second
)

func SleepBeforeMaintenanceLoop(delay time.Duration) {
	if delay <= 0 {
		return
	}
	time.Sleep(delay)
}
