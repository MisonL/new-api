package service

import (
	"strings"
	"sync"
	"time"
)

type NpmCLIVersionDiagnosticsSummary struct {
	PackageCount           int        `json:"package_count"`
	RecordedCount          int        `json:"recorded_count"`
	MissingCount           int        `json:"missing_count"`
	LastErrorCount         int        `json:"last_error_count"`
	ProcessLastErrorCount  int        `json:"process_last_error_count"`
	RecordedLastErrorCount int        `json:"recorded_last_error_count"`
	NewestRefreshedAt      *time.Time `json:"newest_refreshed_at,omitempty"`
	OldestRefreshedAt      *time.Time `json:"oldest_refreshed_at,omitempty"`
	MaxCacheAgeMs          int64      `json:"max_cache_age_ms,omitempty"`
}

type NpmCLIVersionRefreshMetrics struct {
	ScheduledRuns               int64      `json:"scheduled_runs"`
	ScheduledSuccessfulPackages int64      `json:"scheduled_successful_packages"`
	ScheduledFailedPackages     int64      `json:"scheduled_failed_packages"`
	ManualRuns                  int64      `json:"manual_runs"`
	ManualSuccesses             int64      `json:"manual_successes"`
	ManualFailures              int64      `json:"manual_failures"`
	LastRunAt                   *time.Time `json:"last_run_at,omitempty"`
	LastRunDurationMs           int64      `json:"last_run_duration_ms,omitempty"`
	LastRunRefreshed            int        `json:"last_run_refreshed"`
	LastRunFailed               int        `json:"last_run_failed"`
	LastManualRefreshAt         *time.Time `json:"last_manual_refresh_at,omitempty"`
	LastManualDurationMs        int64      `json:"last_manual_duration_ms,omitempty"`
	LastManualPackage           string     `json:"last_manual_package,omitempty"`
	LastManualCode              string     `json:"last_manual_code,omitempty"`
}

var npmCLIVersionRefreshMetricsState = struct {
	sync.Mutex
	scheduledRuns               int64
	scheduledSuccessfulPackages int64
	scheduledFailedPackages     int64
	manualRuns                  int64
	manualSuccesses             int64
	manualFailures              int64
	lastRunAt                   time.Time
	lastRunDurationMs           int64
	lastRunRefreshed            int
	lastRunFailed               int
	lastManualRefreshAt         time.Time
	lastManualDurationMs        int64
	lastManualPackage           string
	lastManualCode              string
}{}

func summarizeNpmCLIVersionDiagnostics(items []NpmCLIVersionDiagnostic) NpmCLIVersionDiagnosticsSummary {
	summary := NpmCLIVersionDiagnosticsSummary{PackageCount: len(items)}
	var newestRefreshedAt time.Time
	var oldestRefreshedAt time.Time
	for _, item := range items {
		if item.Recorded {
			summary.RecordedCount++
		} else {
			summary.MissingCount++
		}
		if item.LastError != nil {
			summary.LastErrorCount++
			switch item.LastErrorScope {
			case "process":
				summary.ProcessLastErrorCount++
			case "recorded":
				summary.RecordedLastErrorCount++
			}
		}
		if item.RefreshedAt != nil && !item.RefreshedAt.IsZero() {
			refreshedAt := *item.RefreshedAt
			if newestRefreshedAt.IsZero() || refreshedAt.After(newestRefreshedAt) {
				newestRefreshedAt = refreshedAt
			}
			if oldestRefreshedAt.IsZero() || refreshedAt.Before(oldestRefreshedAt) {
				oldestRefreshedAt = refreshedAt
			}
		}
		if item.CacheAgeMs > summary.MaxCacheAgeMs {
			summary.MaxCacheAgeMs = item.CacheAgeMs
		}
	}
	summary.NewestRefreshedAt = npmCLIVersionTimePointer(newestRefreshedAt)
	summary.OldestRefreshedAt = npmCLIVersionTimePointer(oldestRefreshedAt)
	return summary
}

func recordNpmCLIVersionScheduledRefresh(startedAt time.Time, refreshed int, failed int) {
	completedAt := time.Now()
	npmCLIVersionRefreshMetricsState.Lock()
	defer npmCLIVersionRefreshMetricsState.Unlock()
	npmCLIVersionRefreshMetricsState.scheduledRuns++
	npmCLIVersionRefreshMetricsState.scheduledSuccessfulPackages += int64(refreshed)
	npmCLIVersionRefreshMetricsState.scheduledFailedPackages += int64(failed)
	npmCLIVersionRefreshMetricsState.lastRunAt = completedAt
	npmCLIVersionRefreshMetricsState.lastRunDurationMs = npmCLIVersionDurationMilliseconds(startedAt, completedAt)
	npmCLIVersionRefreshMetricsState.lastRunRefreshed = refreshed
	npmCLIVersionRefreshMetricsState.lastRunFailed = failed
}

func recordNpmCLIVersionManualRefresh(packageName string, code string, success bool, startedAt time.Time) {
	completedAt := time.Now()
	npmCLIVersionRefreshMetricsState.Lock()
	defer npmCLIVersionRefreshMetricsState.Unlock()
	npmCLIVersionRefreshMetricsState.manualRuns++
	if success {
		npmCLIVersionRefreshMetricsState.manualSuccesses++
	} else {
		npmCLIVersionRefreshMetricsState.manualFailures++
	}
	npmCLIVersionRefreshMetricsState.lastManualRefreshAt = completedAt
	npmCLIVersionRefreshMetricsState.lastManualDurationMs = npmCLIVersionDurationMilliseconds(startedAt, completedAt)
	npmCLIVersionRefreshMetricsState.lastManualPackage = strings.TrimSpace(packageName)
	npmCLIVersionRefreshMetricsState.lastManualCode = strings.TrimSpace(code)
}

func snapshotNpmCLIVersionRefreshMetrics() NpmCLIVersionRefreshMetrics {
	npmCLIVersionRefreshMetricsState.Lock()
	defer npmCLIVersionRefreshMetricsState.Unlock()
	return NpmCLIVersionRefreshMetrics{
		ScheduledRuns:               npmCLIVersionRefreshMetricsState.scheduledRuns,
		ScheduledSuccessfulPackages: npmCLIVersionRefreshMetricsState.scheduledSuccessfulPackages,
		ScheduledFailedPackages:     npmCLIVersionRefreshMetricsState.scheduledFailedPackages,
		ManualRuns:                  npmCLIVersionRefreshMetricsState.manualRuns,
		ManualSuccesses:             npmCLIVersionRefreshMetricsState.manualSuccesses,
		ManualFailures:              npmCLIVersionRefreshMetricsState.manualFailures,
		LastRunAt:                   npmCLIVersionTimePointer(npmCLIVersionRefreshMetricsState.lastRunAt),
		LastRunDurationMs:           npmCLIVersionRefreshMetricsState.lastRunDurationMs,
		LastRunRefreshed:            npmCLIVersionRefreshMetricsState.lastRunRefreshed,
		LastRunFailed:               npmCLIVersionRefreshMetricsState.lastRunFailed,
		LastManualRefreshAt:         npmCLIVersionTimePointer(npmCLIVersionRefreshMetricsState.lastManualRefreshAt),
		LastManualDurationMs:        npmCLIVersionRefreshMetricsState.lastManualDurationMs,
		LastManualPackage:           npmCLIVersionRefreshMetricsState.lastManualPackage,
		LastManualCode:              npmCLIVersionRefreshMetricsState.lastManualCode,
	}
}

func npmCLIVersionDurationMilliseconds(startedAt time.Time, completedAt time.Time) int64 {
	if startedAt.IsZero() || completedAt.IsZero() || completedAt.Before(startedAt) {
		return 0
	}
	return completedAt.Sub(startedAt).Milliseconds()
}

func npmCLIVersionTimePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copied := value
	return &copied
}

func resetNpmCLIVersionRefreshMetricsForTest() {
	npmCLIVersionRefreshMetricsState.Lock()
	npmCLIVersionRefreshMetricsState.scheduledRuns = 0
	npmCLIVersionRefreshMetricsState.scheduledSuccessfulPackages = 0
	npmCLIVersionRefreshMetricsState.scheduledFailedPackages = 0
	npmCLIVersionRefreshMetricsState.manualRuns = 0
	npmCLIVersionRefreshMetricsState.manualSuccesses = 0
	npmCLIVersionRefreshMetricsState.manualFailures = 0
	npmCLIVersionRefreshMetricsState.lastRunAt = time.Time{}
	npmCLIVersionRefreshMetricsState.lastRunDurationMs = 0
	npmCLIVersionRefreshMetricsState.lastRunRefreshed = 0
	npmCLIVersionRefreshMetricsState.lastRunFailed = 0
	npmCLIVersionRefreshMetricsState.lastManualRefreshAt = time.Time{}
	npmCLIVersionRefreshMetricsState.lastManualDurationMs = 0
	npmCLIVersionRefreshMetricsState.lastManualPackage = ""
	npmCLIVersionRefreshMetricsState.lastManualCode = ""
	npmCLIVersionRefreshMetricsState.Unlock()
}
