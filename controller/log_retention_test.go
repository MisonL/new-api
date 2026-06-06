package controller

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/log_retention_setting"
	"github.com/stretchr/testify/require"
)

func TestSelectServerLogFilesForRetentionSkipsActiveLog(t *testing.T) {
	previousLogDir := *common.LogDir
	tempDir := t.TempDir()
	*common.LogDir = tempDir
	t.Cleanup(func() {
		*common.LogDir = previousLogDir
	})

	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	files := []LogFileInfo{
		{Name: "oneapi-20260604120000.log", Size: 10, ModTime: now},
		{Name: "oneapi-20260603120000.log", Size: 10, ModTime: now.AddDate(0, 0, -1)},
		{Name: "oneapi-20260602120000.log", Size: 10, ModTime: now.AddDate(0, 0, -2)},
	}
	active := filepath.Join(tempDir, "oneapi-20260602120000.log")

	selected := selectServerLogFilesForRetention(files, log_retention_setting.LogRetentionSetting{
		ServerLogKeepFiles: 1,
	}, now, active)

	require.Len(t, selected, 1)
	require.Equal(t, "oneapi-20260603120000.log", selected[0].Name)
}

func TestSelectServerLogFilesForRetentionCombinesAgeAndSize(t *testing.T) {
	previousLogDir := *common.LogDir
	tempDir := t.TempDir()
	*common.LogDir = tempDir
	t.Cleanup(func() {
		*common.LogDir = previousLogDir
	})

	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	files := []LogFileInfo{
		{Name: "oneapi-20260604120000.log", Size: 8 * 1024 * 1024, ModTime: now},
		{Name: "oneapi-20260603120000.log", Size: 8 * 1024 * 1024, ModTime: now.AddDate(0, 0, -1)},
		{Name: "oneapi-20260501120000.log", Size: 8 * 1024 * 1024, ModTime: now.AddDate(0, 0, -34)},
	}

	selected := selectServerLogFilesForRetention(files, log_retention_setting.LogRetentionSetting{
		ServerLogKeepDays:       30,
		ServerLogMaxTotalSizeMB: 16,
	}, now, "")

	require.Len(t, selected, 1)
	require.Equal(t, "oneapi-20260501120000.log", selected[0].Name)
}

func TestLogRetentionSleepDurationPollsWhenDisabled(t *testing.T) {
	sleepDuration := logRetentionSleepDuration(log_retention_setting.LogRetentionSetting{
		Enabled:          false,
		RunIntervalHours: 24,
	})

	require.Equal(t, logRetentionDisabledPollInterval, sleepDuration)
}

func TestLogRetentionSleepDurationUsesConfiguredIntervalWhenEnabled(t *testing.T) {
	sleepDuration := logRetentionSleepDuration(log_retention_setting.LogRetentionSetting{
		Enabled:          true,
		RunIntervalHours: 2,
	})

	require.Equal(t, 2*time.Hour, sleepDuration)
}
