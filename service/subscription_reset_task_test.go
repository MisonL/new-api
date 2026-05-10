package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaResetTaskEnabledDefaultsToTrue(t *testing.T) {
	t.Setenv("SUBSCRIPTION_QUOTA_RESET_TASK_ENABLED", "")

	require.True(t, subscriptionQuotaResetTaskEnabled())
}

func TestSubscriptionQuotaResetTaskEnabledCanBeDisabled(t *testing.T) {
	t.Setenv("SUBSCRIPTION_QUOTA_RESET_TASK_ENABLED", "false")

	require.False(t, subscriptionQuotaResetTaskEnabled())
}
