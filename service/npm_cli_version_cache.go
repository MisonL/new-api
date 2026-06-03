package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const npmCliVersionRecordedOptionsKey = "NpmCLIVersionRecordedOptions"

var (
	npmCliVersionRefreshOnce    sync.Once
	npmCliVersionRefreshRunning atomic.Bool
	npmCliVersionRecordedState  = struct {
		sync.Mutex
		raw string
	}{}
)

func init() {
	dto.ResolveNpmCLILatestVersion = ResolveNpmCLILatestVersion
}

func StartNpmCLIVersionRefreshTask() {
	npmCliVersionRefreshOnce.Do(func() {
		loadRecordedNpmCLIVersionOptions()
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("npm cli version refresh task started: tick=%s master=%t", npmCliVersionRefreshInterval, common.IsMasterNode))
			ticker := time.NewTicker(npmCliVersionRefreshInterval)
			defer ticker.Stop()

			runNpmCLIVersionScheduledTick()
			for range ticker.C {
				runNpmCLIVersionScheduledTick()
			}
		})
	})
}

func runNpmCLIVersionScheduledTick() {
	loadRecordedNpmCLIVersionOptions()
	if common.IsMasterNode {
		runNpmCLIVersionRefreshOnce()
	}
}

func runNpmCLIVersionRefreshOnce() {
	if !npmCliVersionRefreshRunning.CompareAndSwap(false, true) {
		return
	}
	defer npmCliVersionRefreshRunning.Store(false)

	ctx := context.Background()
	refreshed := 0
	failed := 0
	for _, packageName := range allowedNpmCLIPackageNames() {
		options, err := fetchNpmCLIVersionOptions(ctx, packageName, defaultNpmRegistryHTTPClient(), defaultNpmRegistryMetadataURL)
		if err != nil {
			failed++
			logger.LogWarn(ctx, fmt.Sprintf("npm cli version refresh failed for %s: %v", packageName, err))
			continue
		}
		setCachedNpmCLIVersionOptions(packageName, options)
		refreshed++
	}
	if refreshed > 0 {
		persistRecordedNpmCLIVersionOptions()
	}
	logger.LogInfo(ctx, fmt.Sprintf("npm cli version refresh completed: refreshed=%d failed=%d", refreshed, failed))
}

func allowedNpmCLIPackageNames() []string {
	packages := make([]string, 0, len(allowedNpmCLIPackages))
	for packageName := range allowedNpmCLIPackages {
		packages = append(packages, packageName)
	}
	sort.Strings(packages)
	return packages
}

func ResolveNpmCLILatestVersion(packageName string) (string, bool) {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" {
		return "", false
	}
	if !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return "", false
	}
	loadRecordedNpmCLIVersionOptions()
	return resolveCachedNpmCLILatestVersion(normalizedPackageName)
}

func resolveCachedNpmCLILatestVersion(packageName string) (string, bool) {
	npmCliVersionCache.RLock()
	entry, exists := npmCliVersionCache.items[packageName]
	npmCliVersionCache.RUnlock()
	if !exists {
		return "", false
	}
	if strings.TrimSpace(entry.latestVersion) != "" {
		return strings.TrimSpace(entry.latestVersion), true
	}
	latestVersion := latestNpmCLIVersionFromOptions(entry.options)
	if latestVersion == "" {
		return "", false
	}
	return latestVersion, true
}

func getCachedNpmCLIVersionOptions(packageName string) ([]NpmCLIVersionOption, bool) {
	npmCliVersionCache.RLock()
	entry, exists := npmCliVersionCache.items[packageName]
	npmCliVersionCache.RUnlock()
	if !exists || len(entry.options) == 0 {
		return nil, false
	}
	return cloneNpmCLIVersionOptions(entry.options), true
}

func setCachedNpmCLIVersionOptions(packageName string, options []NpmCLIVersionOption) {
	setCachedNpmCLIVersionOptionsWithFetchedAt(packageName, options, time.Now())
}

func setCachedNpmCLIVersionOptionsWithFetchedAt(packageName string, options []NpmCLIVersionOption, fetchedAt time.Time) {
	setCachedNpmCLIVersionOptionsFromRecord(packageName, options, fetchedAt, "")
}

func setCachedNpmCLIVersionOptionsFromRecord(packageName string, options []NpmCLIVersionOption, fetchedAt time.Time, recordedLatestVersion string) {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return
	}
	normalizedOptions := normalizeRecordedNpmCLIVersionOptionsWithLatest(options, recordedLatestVersion)
	if len(normalizedOptions) == 0 {
		return
	}
	latestVersion := latestNpmCLIVersionFromOptions(normalizedOptions)
	if latestVersion == "" {
		return
	}
	npmCliVersionCache.Lock()
	if current, exists := npmCliVersionCache.items[normalizedPackageName]; exists && current.fetchedAt.After(fetchedAt) {
		npmCliVersionCache.Unlock()
		return
	}
	npmCliVersionCache.items[normalizedPackageName] = npmCliVersionCacheEntry{
		fetchedAt:     fetchedAt,
		latestVersion: latestVersion,
		options:       normalizedOptions,
	}
	npmCliVersionCache.Unlock()
}

func normalizeRecordedNpmCLIVersionOptions(options []NpmCLIVersionOption) []NpmCLIVersionOption {
	return normalizeRecordedNpmCLIVersionOptionsWithLatest(options, "")
}

func normalizeRecordedNpmCLIVersionOptionsWithLatest(options []NpmCLIVersionOption, recordedLatestVersion string) []NpmCLIVersionOption {
	latestVersion := latestNpmCLIVersionFromOptions(options)
	if latestVersion == "" {
		latestVersion = normalizeNpmCLIVersionValue(recordedLatestVersion)
	}
	if latestVersion == "" {
		return nil
	}
	normalizedOptions := []NpmCLIVersionOption{
		{
			Value:           NpmCLIVersionLatestAlias,
			Label:           fmt.Sprintf("%s (%s)", NpmCLIVersionLatestAlias, latestVersion),
			IsLatest:        true,
			ResolvedVersion: latestVersion,
		},
	}
	seenValues := map[string]struct{}{
		NpmCLIVersionLatestAlias: {},
	}
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		if value == "" || value == NpmCLIVersionLatestAlias {
			continue
		}
		normalizedValue := normalizeNpmCLIVersionValue(value)
		if normalizedValue == "" {
			continue
		}
		if _, exists := seenValues[normalizedValue]; exists {
			continue
		}
		seenValues[normalizedValue] = struct{}{}
		normalizedOptions = append(normalizedOptions, NpmCLIVersionOption{
			Value:           normalizedValue,
			Label:           normalizedValue,
			IsLatest:        false,
			ResolvedVersion: normalizedValue,
		})
		if len(normalizedOptions) >= npmCliVersionOptionLimit+1 {
			break
		}
	}
	return normalizedOptions
}

type recordedNpmCLIVersionOptions struct {
	Packages map[string]recordedNpmCLIPackageOptions `json:"packages"`
}

type recordedNpmCLIPackageOptions struct {
	FetchedAt     time.Time             `json:"fetched_at"`
	LatestVersion string                `json:"latest_version,omitempty"`
	Options       []NpmCLIVersionOption `json:"options"`
}

func loadRecordedNpmCLIVersionOptions() {
	common.OptionMapRWMutex.RLock()
	raw := strings.TrimSpace(common.OptionMap[npmCliVersionRecordedOptionsKey])
	common.OptionMapRWMutex.RUnlock()
	if raw == "" {
		return
	}

	npmCliVersionRecordedState.Lock()
	if raw == npmCliVersionRecordedState.raw {
		npmCliVersionRecordedState.Unlock()
		return
	}
	npmCliVersionRecordedState.raw = raw
	npmCliVersionRecordedState.Unlock()

	var recorded recordedNpmCLIVersionOptions
	if err := common.Unmarshal([]byte(raw), &recorded); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("load npm cli version recorded options failed: %v", err))
		return
	}
	for packageName, packageRecord := range recorded.Packages {
		normalizedPackageName := strings.TrimSpace(packageName)
		if !IsAllowedNpmCLIPackage(normalizedPackageName) || len(packageRecord.Options) == 0 {
			continue
		}
		setCachedNpmCLIVersionOptionsFromRecord(
			normalizedPackageName,
			packageRecord.Options,
			packageRecord.FetchedAt,
			packageRecord.LatestVersion,
		)
	}
}

func persistRecordedNpmCLIVersionOptions() {
	recorded := snapshotRecordedNpmCLIVersionOptions()
	payload, err := common.Marshal(recorded)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("marshal npm cli version recorded options failed: %v", err))
		return
	}
	if err := model.UpdateOption(npmCliVersionRecordedOptionsKey, string(payload)); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("persist npm cli version recorded options failed: %v", err))
	}
}

func snapshotRecordedNpmCLIVersionOptions() recordedNpmCLIVersionOptions {
	npmCliVersionCache.RLock()
	defer npmCliVersionCache.RUnlock()

	recorded := recordedNpmCLIVersionOptions{
		Packages: make(map[string]recordedNpmCLIPackageOptions, len(npmCliVersionCache.items)),
	}
	for _, packageName := range allowedNpmCLIPackageNames() {
		entry, exists := npmCliVersionCache.items[packageName]
		if !exists || len(entry.options) == 0 {
			continue
		}
		recorded.Packages[packageName] = recordedNpmCLIPackageOptions{
			FetchedAt:     entry.fetchedAt,
			LatestVersion: entry.latestVersion,
			Options:       cloneNpmCLIVersionOptions(entry.options),
		}
	}
	return recorded
}
