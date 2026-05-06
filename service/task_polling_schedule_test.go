package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTaskPollingIntervalStaysAtFifteenSeconds(t *testing.T) {
	require.Equal(t, 15*time.Second, taskPollingInterval)
}
