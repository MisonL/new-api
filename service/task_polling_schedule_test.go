package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestTaskPollingIntervalStaysAtFifteenSeconds(t *testing.T) {
	require.Equal(t, 15*time.Second, common.TaskPollingInterval)
	require.Equal(t, 10*time.Second, common.TaskPollingInitialDelay)
}
