package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
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
	"gorm.io/gorm"
)

const npmCliVersionRecordedOptionsKey = "NpmCLIVersionRecordedOptions"
const npmCliVersionRecordedPersistMaxAttempts = 5
const npmCliVersionRecordedPersistRetryBaseDelay = 5 * time.Millisecond

var (
	npmCliVersionRefreshOnce       sync.Once
	npmCliVersionRefreshRunning    atomic.Bool
	npmCliVersionRecordedPersistMu sync.Mutex
	npmCliVersionRecordedState     = struct {
		sync.Mutex
		raw string
	}{}
	sleepBeforeNpmCLIVersionRecordedRetry = time.Sleep
	npmCliVersionRecordedRetryJitter      = func() time.Duration {
		return time.Duration(rand.Int63n(int64(npmCliVersionRecordedPersistRetryBaseDelay * 2)))
	}
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
	startedAt := time.Now()
	refreshed := 0
	failed := 0
	packageNames := allowedNpmCLIPackageNames()
	for _, packageName := range packageNames {
		options, err := fetchNpmCLIVersionOptions(ctx, packageName, defaultNpmRegistryHTTPClient(), defaultNpmRegistryMetadataURL)
		if err != nil {
			failed++
			recordNpmCLIVersionLastError(ctx, packageName, "npm", classifyNpmCLIVersionFetchError(err), err)
			logger.LogWarn(ctx, fmt.Sprintf("npm cli version refresh package failed: package=%s error=%v", packageName, err))
			continue
		}
		normalizedOptions := normalizeRecordedNpmCLIVersionOptions(options)
		latestVersion := latestNpmCLIVersionFromOptions(normalizedOptions)
		if len(normalizedOptions) == 0 || latestVersion == "" {
			failed++
			recordNpmCLIVersionLastError(ctx, packageName, "npm", NpmCLIVersionEmptyCode, fmt.Errorf("empty npm version options"))
			logger.LogWarn(ctx, fmt.Sprintf("npm cli version refresh package empty: package=%s options=%d latest=%q", packageName, len(normalizedOptions), latestVersion))
			continue
		}
		fetchedAt := time.Now()
		if err := persistRecordedNpmCLIVersionOptionsWithPackage(ctx, packageName, normalizedOptions, fetchedAt); err != nil {
			failed++
			recordNpmCLIVersionLastError(ctx, packageName, "persist", NpmCLIVersionPersistFailedCode, err)
			logger.LogWarn(ctx, fmt.Sprintf("npm cli version refresh persist package failed: package=%s error=%v", packageName, err))
			continue
		}
		clearNpmCLIVersionLastError(packageName)
		setCachedNpmCLIVersionOptionsFromRecord(
			packageName,
			normalizedOptions,
			fetchedAt,
			latestVersion,
			"npm",
		)
		refreshed++
	}
	durationMs := time.Since(startedAt).Milliseconds()
	recordNpmCLIVersionScheduledRefresh(startedAt, refreshed, failed)
	if refreshed == 0 && failed > 0 {
		logger.LogWarn(ctx, fmt.Sprintf("npm cli version refresh completed without usable package records: packages=%d refreshed=%d failed=%d duration_ms=%d", len(packageNames), refreshed, failed, durationMs))
	}
	logger.LogInfo(ctx, fmt.Sprintf("npm cli version refresh completed: packages=%d refreshed=%d failed=%d duration_ms=%d", len(packageNames), refreshed, failed, durationMs))
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
	if latestVersion, ok := resolveCachedNpmCLILatestVersion(normalizedPackageName); ok {
		return latestVersion, true
	}
	loadRecordedNpmCLIVersionOptionsFromDatabase()
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
	entry, ok := getCachedNpmCLIVersionOptionsEntry(packageName)
	if !ok {
		return nil, false
	}
	return cloneNpmCLIVersionOptions(entry.options), true
}

func getCachedNpmCLIVersionOptionsEntry(packageName string) (npmCliVersionCacheEntry, bool) {
	npmCliVersionCache.RLock()
	entry, exists := npmCliVersionCache.items[packageName]
	npmCliVersionCache.RUnlock()
	if !exists || len(entry.options) == 0 {
		return npmCliVersionCacheEntry{}, false
	}
	return entry, true
}

func setCachedNpmCLIVersionOptions(packageName string, options []NpmCLIVersionOption) {
	setCachedNpmCLIVersionOptionsWithFetchedAt(packageName, options, time.Now())
}

func setCachedNpmCLIVersionOptionsWithFetchedAt(packageName string, options []NpmCLIVersionOption, fetchedAt time.Time) {
	setCachedNpmCLIVersionOptionsWithFetchedAtAndSource(packageName, options, fetchedAt, "recorded")
}

func setCachedNpmCLIVersionOptionsWithFetchedAtAndSource(packageName string, options []NpmCLIVersionOption, fetchedAt time.Time, source string) {
	setCachedNpmCLIVersionOptionsFromRecord(packageName, options, fetchedAt, "", source)
}

func setCachedNpmCLIVersionOptionsFromRecord(packageName string, options []NpmCLIVersionOption, fetchedAt time.Time, recordedLatestVersion string, source ...string) {
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
	effectiveSource := "recorded"
	if len(source) > 0 && strings.TrimSpace(source[0]) != "" {
		effectiveSource = strings.TrimSpace(source[0])
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
		source:        effectiveSource,
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
	Packages     map[string]recordedNpmCLIPackageOptions `json:"packages"`
	LastErrors   map[string]NpmCLIVersionLastError       `json:"last_errors,omitempty"`
	RecentErrors map[string][]NpmCLIVersionLastError     `json:"recent_errors,omitempty"`
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
	npmCliVersionRecordedState.Lock()
	npmCliVersionRecordedState.raw = raw
	npmCliVersionRecordedState.Unlock()
}

func loadRecordedNpmCLIVersionOptionsFromDatabase(contexts ...context.Context) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	raw, exists, err := readRecordedNpmCLIVersionOptionValue(ctx)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("load npm cli version recorded options from database failed: %v", err))
		return
	}
	if !exists || strings.TrimSpace(raw) == "" {
		return
	}
	updateRecordedNpmCLIVersionOptionMap(raw)
	loadRecordedNpmCLIVersionOptions()
}

func persistRecordedNpmCLIVersionOptions() error {
	npmCliVersionRecordedPersistMu.Lock()
	defer npmCliVersionRecordedPersistMu.Unlock()

	return persistRecordedNpmCLIVersionOptionsSnapshot(context.Background(), snapshotRecordedNpmCLIVersionOptions())
}

func persistRecordedNpmCLIVersionOptionsWithPackage(ctx context.Context, packageName string, options []NpmCLIVersionOption, fetchedAt time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	normalizedPackageName, normalizedRecord, err := normalizeRecordedNpmCLIPackageOptions(
		packageName,
		recordedNpmCLIPackageOptions{
			FetchedAt: fetchedAt,
			Options:   options,
		},
	)
	if err != nil {
		return err
	}
	npmCliVersionRecordedPersistMu.Lock()
	defer npmCliVersionRecordedPersistMu.Unlock()

	recorded, err := recordedNpmCLIVersionOptionsForUpdate()
	if err != nil {
		return err
	}
	recorded.Packages[normalizedPackageName] = normalizedRecord
	clearRecordedNpmCLIVersionLastErrorCoveredByPackage(&recorded, normalizedPackageName, normalizedRecord.FetchedAt)
	return persistRecordedNpmCLIVersionOptionsSnapshot(ctx, recorded)
}

func persistRecordedNpmCLIVersionLastError(ctx context.Context, packageName string, lastError *NpmCLIVersionLastError) error {
	if ctx == nil {
		ctx = context.Background()
	}
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return fmt.Errorf("unsupported npm package: %s", normalizedPackageName)
	}
	var normalizedLastError NpmCLIVersionLastError
	if lastError != nil {
		var ok bool
		normalizedLastError, ok = normalizeRecordedNpmCLIVersionLastError(*lastError)
		if !ok {
			return fmt.Errorf("invalid npm cli version last error for package: %s", normalizedPackageName)
		}
	}

	npmCliVersionRecordedPersistMu.Lock()
	defer npmCliVersionRecordedPersistMu.Unlock()

	recorded, err := recordedNpmCLIVersionOptionsForUpdate()
	if err != nil {
		return err
	}
	if lastError == nil {
		if recorded.LastErrors != nil {
			delete(recorded.LastErrors, normalizedPackageName)
		}
	} else {
		if recorded.LastErrors == nil {
			recorded.LastErrors = map[string]NpmCLIVersionLastError{}
		}
		if recorded.RecentErrors == nil {
			recorded.RecentErrors = map[string][]NpmCLIVersionLastError{}
		}
		recorded.LastErrors[normalizedPackageName] = normalizedLastError
		recorded.RecentErrors[normalizedPackageName] = mergeNpmCLIVersionRecentErrors(
			[]NpmCLIVersionLastError{normalizedLastError},
			recorded.RecentErrors[normalizedPackageName],
		)
	}
	return persistRecordedNpmCLIVersionOptionsSnapshot(ctx, recorded)
}

func persistRecordedNpmCLIVersionOptionsWithRecords(records map[string]recordedNpmCLIPackageOptions) error {
	npmCliVersionRecordedPersistMu.Lock()
	defer npmCliVersionRecordedPersistMu.Unlock()

	recorded, err := recordedNpmCLIVersionOptionsForUpdate()
	if err != nil {
		return err
	}
	validRecords := 0
	for packageName, packageRecord := range records {
		normalizedPackageName, normalizedRecord, err := normalizeRecordedNpmCLIPackageOptions(packageName, packageRecord)
		if err != nil {
			continue
		}
		recorded.Packages[normalizedPackageName] = normalizedRecord
		clearRecordedNpmCLIVersionLastErrorCoveredByPackage(&recorded, normalizedPackageName, normalizedRecord.FetchedAt)
		validRecords++
	}
	if validRecords == 0 {
		return fmt.Errorf("no valid npm version records to persist")
	}
	return persistRecordedNpmCLIVersionOptionsSnapshot(context.Background(), recorded)
}

func normalizeRecordedNpmCLIPackageOptions(packageName string, packageRecord recordedNpmCLIPackageOptions) (string, recordedNpmCLIPackageOptions, error) {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return "", recordedNpmCLIPackageOptions{}, fmt.Errorf("unsupported npm package: %s", normalizedPackageName)
	}
	normalizedOptions := normalizeRecordedNpmCLIVersionOptionsWithLatest(packageRecord.Options, packageRecord.LatestVersion)
	if len(normalizedOptions) == 0 {
		return "", recordedNpmCLIPackageOptions{}, fmt.Errorf("npm version options not recorded for package: %s", normalizedPackageName)
	}
	latestVersion := latestNpmCLIVersionFromOptions(normalizedOptions)
	if latestVersion == "" {
		return "", recordedNpmCLIPackageOptions{}, fmt.Errorf("npm version options missing latest version for package: %s", normalizedPackageName)
	}
	return normalizedPackageName, recordedNpmCLIPackageOptions{
		FetchedAt:     packageRecord.FetchedAt,
		LatestVersion: latestVersion,
		Options:       cloneNpmCLIVersionOptions(normalizedOptions),
	}, nil
}

func normalizeRecordedNpmCLIVersionLastError(lastError NpmCLIVersionLastError) (NpmCLIVersionLastError, bool) {
	code := strings.TrimSpace(lastError.Code)
	if code == "" {
		return NpmCLIVersionLastError{}, false
	}
	source := strings.TrimSpace(lastError.Source)
	if source == "" {
		source = "unknown"
	}
	updatedAt := lastError.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	return NpmCLIVersionLastError{
		Code:      code,
		Message:   npmCLIVersionSafeLastErrorMessage(code),
		Source:    source,
		UpdatedAt: updatedAt,
	}, true
}

func clearRecordedNpmCLIVersionLastErrorCoveredByPackage(recorded *recordedNpmCLIVersionOptions, packageName string, fetchedAt time.Time) {
	if recorded == nil || recorded.LastErrors == nil || fetchedAt.IsZero() {
		return
	}
	lastError, exists := recorded.LastErrors[packageName]
	if !exists {
		return
	}
	if !lastError.UpdatedAt.After(fetchedAt) {
		delete(recorded.LastErrors, packageName)
	}
}

func recordedNpmCLIVersionOptionsForUpdate() (recordedNpmCLIVersionOptions, error) {
	common.OptionMapRWMutex.RLock()
	raw := strings.TrimSpace(common.OptionMap[npmCliVersionRecordedOptionsKey])
	common.OptionMapRWMutex.RUnlock()

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{},
	}
	if raw != "" {
		if err := common.Unmarshal([]byte(raw), &recorded); err != nil {
			return recordedNpmCLIVersionOptions{}, fmt.Errorf("decode recorded options: %w", err)
		}
		if recorded.Packages == nil {
			recorded.Packages = map[string]recordedNpmCLIPackageOptions{}
		}
		if recorded.LastErrors == nil {
			recorded.LastErrors = map[string]NpmCLIVersionLastError{}
		}
	}

	cached := snapshotRecordedNpmCLIVersionOptions()
	for packageName, packageRecord := range cached.Packages {
		current, exists := recorded.Packages[packageName]
		if !exists || packageRecord.FetchedAt.After(current.FetchedAt) {
			recorded.Packages[packageName] = packageRecord
		}
	}
	if recorded.LastErrors == nil {
		recorded.LastErrors = map[string]NpmCLIVersionLastError{}
	}
	if recorded.RecentErrors == nil {
		recorded.RecentErrors = map[string][]NpmCLIVersionLastError{}
	}
	for packageName, recentErrors := range cached.RecentErrors {
		recorded.RecentErrors[packageName] = mergeNpmCLIVersionRecentErrors(
			recentErrors,
			recorded.RecentErrors[packageName],
		)
	}
	for packageName, lastError := range cached.LastErrors {
		recorded.LastErrors[packageName] = lastError
		recorded.RecentErrors[packageName] = mergeNpmCLIVersionRecentErrors(
			[]NpmCLIVersionLastError{lastError},
			recorded.RecentErrors[packageName],
		)
	}
	return recorded, nil
}

func persistRecordedNpmCLIVersionOptionsSnapshot(ctx context.Context, recorded recordedNpmCLIVersionOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var lastErr error
	for attempt := 0; attempt < npmCliVersionRecordedPersistMaxAttempts; attempt++ {
		currentRaw, exists, err := readRecordedNpmCLIVersionOptionValue(ctx)
		if err != nil {
			return err
		}
		currentRecorded, err := decodeRecordedNpmCLIVersionOptions(currentRaw)
		if err != nil {
			return err
		}
		merged := mergeRecordedNpmCLIVersionOptions(currentRecorded, recorded)
		payload, err := common.Marshal(merged)
		if err != nil {
			return fmt.Errorf("marshal recorded options: %w", err)
		}
		nextRaw := string(payload)
		if !exists {
			if err := model.DB.WithContext(ctx).Create(&model.Option{Key: npmCliVersionRecordedOptionsKey, Value: nextRaw}).Error; err != nil {
				lastErr = fmt.Errorf("create recorded options: %w", err)
				waitBeforeNpmCLIVersionRecordedRetry(attempt)
				continue
			}
			updateRecordedNpmCLIVersionOptionMap(nextRaw)
			return nil
		}
		if currentRaw == nextRaw {
			updateRecordedNpmCLIVersionOptionMap(nextRaw)
			return nil
		}
		valueSQL, valueArgs := recordedNpmCLIVersionCASValuePredicate(currentRaw)
		result := model.DB.WithContext(ctx).Model(&model.Option{}).
			Where("key = ?", npmCliVersionRecordedOptionsKey).
			Where(valueSQL, valueArgs...).
			Update("value", nextRaw)
		if result.Error != nil {
			return fmt.Errorf("update recorded options: %w", result.Error)
		}
		if result.RowsAffected > 0 {
			updateRecordedNpmCLIVersionOptionMap(nextRaw)
			return nil
		}
		lastErr = fmt.Errorf("recorded options changed during update")
		waitBeforeNpmCLIVersionRecordedRetry(attempt)
	}
	if lastErr != nil {
		return fmt.Errorf("update recorded options conflicted after %d attempts: %w", npmCliVersionRecordedPersistMaxAttempts, lastErr)
	}
	return fmt.Errorf("update recorded options conflicted after %d attempts", npmCliVersionRecordedPersistMaxAttempts)
}

func recordedNpmCLIVersionCASValuePredicate(currentRaw string) (string, []any) {
	if common.UsingMySQL {
		return "BINARY value = BINARY ?", []any{currentRaw}
	}
	return "value = ?", []any{currentRaw}
}

func waitBeforeNpmCLIVersionRecordedRetry(attempt int) {
	if attempt+1 >= npmCliVersionRecordedPersistMaxAttempts {
		return
	}
	delay := time.Duration(attempt+1) * npmCliVersionRecordedPersistRetryBaseDelay
	if jitter := npmCliVersionRecordedRetryJitter(); jitter > 0 {
		delay += jitter
	}
	sleepBeforeNpmCLIVersionRecordedRetry(delay)
}

func readRecordedNpmCLIVersionOptionValue(contexts ...context.Context) (string, bool, error) {
	if model.DB == nil {
		return "", false, fmt.Errorf("database is not initialized")
	}
	db := model.DB
	if len(contexts) > 0 && contexts[0] != nil {
		db = db.WithContext(contexts[0])
	}
	option := model.Option{Key: npmCliVersionRecordedOptionsKey}
	err := db.First(&option).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read recorded options: %w", err)
	}
	return option.Value, true, nil
}

func decodeRecordedNpmCLIVersionOptions(raw string) (recordedNpmCLIVersionOptions, error) {
	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{},
	}
	if strings.TrimSpace(raw) == "" {
		return recorded, nil
	}
	if err := common.Unmarshal([]byte(raw), &recorded); err != nil {
		return recordedNpmCLIVersionOptions{}, fmt.Errorf("decode recorded options: %w", err)
	}
	if recorded.Packages == nil {
		recorded.Packages = map[string]recordedNpmCLIPackageOptions{}
	}
	if recorded.LastErrors == nil {
		recorded.LastErrors = map[string]NpmCLIVersionLastError{}
	}
	if recorded.RecentErrors == nil {
		recorded.RecentErrors = map[string][]NpmCLIVersionLastError{}
	}
	return recorded, nil
}

func mergeRecordedNpmCLIVersionOptions(base recordedNpmCLIVersionOptions, overlay recordedNpmCLIVersionOptions) recordedNpmCLIVersionOptions {
	if base.Packages == nil {
		base.Packages = map[string]recordedNpmCLIPackageOptions{}
	}
	if base.LastErrors == nil {
		base.LastErrors = map[string]NpmCLIVersionLastError{}
	}
	if base.RecentErrors == nil {
		base.RecentErrors = map[string][]NpmCLIVersionLastError{}
	}
	for packageName, lastError := range base.LastErrors {
		normalizedPackageName := strings.TrimSpace(packageName)
		if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) {
			continue
		}
		base.RecentErrors[normalizedPackageName] = mergeNpmCLIVersionRecentErrors(
			[]NpmCLIVersionLastError{lastError},
			base.RecentErrors[normalizedPackageName],
		)
	}
	for packageName, packageRecord := range overlay.Packages {
		normalizedPackageName := strings.TrimSpace(packageName)
		if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) || len(packageRecord.Options) == 0 {
			continue
		}
		current, exists := base.Packages[normalizedPackageName]
		if !exists || !packageRecord.FetchedAt.Before(current.FetchedAt) {
			_, normalizedRecord, err := normalizeRecordedNpmCLIPackageOptions(normalizedPackageName, packageRecord)
			if err != nil {
				continue
			}
			base.Packages[normalizedPackageName] = normalizedRecord
			clearRecordedNpmCLIVersionLastErrorCoveredByPackage(&base, normalizedPackageName, normalizedRecord.FetchedAt)
		}
	}
	for packageName, lastError := range overlay.LastErrors {
		normalizedPackageName := strings.TrimSpace(packageName)
		if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) {
			continue
		}
		normalizedLastError, ok := normalizeRecordedNpmCLIVersionLastError(lastError)
		if !ok {
			delete(base.LastErrors, normalizedPackageName)
			continue
		}
		if packageRecord, exists := base.Packages[normalizedPackageName]; exists &&
			!packageRecord.FetchedAt.IsZero() &&
			!packageRecord.FetchedAt.Before(normalizedLastError.UpdatedAt) {
			base.RecentErrors[normalizedPackageName] = mergeNpmCLIVersionRecentErrors(
				[]NpmCLIVersionLastError{normalizedLastError},
				base.RecentErrors[normalizedPackageName],
			)
			continue
		}
		current, exists := base.LastErrors[normalizedPackageName]
		if !exists || !normalizedLastError.UpdatedAt.Before(current.UpdatedAt) {
			base.LastErrors[normalizedPackageName] = normalizedLastError
		}
		base.RecentErrors[normalizedPackageName] = mergeNpmCLIVersionRecentErrors(
			[]NpmCLIVersionLastError{normalizedLastError},
			base.RecentErrors[normalizedPackageName],
		)
	}
	for packageName, recentErrors := range overlay.RecentErrors {
		normalizedPackageName := strings.TrimSpace(packageName)
		if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) {
			continue
		}
		base.RecentErrors[normalizedPackageName] = mergeNpmCLIVersionRecentErrors(
			recentErrors,
			base.RecentErrors[normalizedPackageName],
		)
	}
	return base
}

func updateRecordedNpmCLIVersionOptionMap(raw string) {
	common.OptionMapRWMutex.Lock()
	common.OptionMap[npmCliVersionRecordedOptionsKey] = raw
	common.OptionMapRWMutex.Unlock()
}

func snapshotRecordedNpmCLIVersionOptions() recordedNpmCLIVersionOptions {
	npmCliVersionCache.RLock()
	defer npmCliVersionCache.RUnlock()

	recorded := recordedNpmCLIVersionOptions{
		Packages:     make(map[string]recordedNpmCLIPackageOptions, len(npmCliVersionCache.items)),
		LastErrors:   snapshotNpmCLIVersionLastErrors(),
		RecentErrors: snapshotNpmCLIVersionRecentErrors(),
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

func getRecordedNpmCLIVersionLastError(packageName string) *NpmCLIVersionLastError {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return nil
	}
	common.OptionMapRWMutex.RLock()
	raw := strings.TrimSpace(common.OptionMap[npmCliVersionRecordedOptionsKey])
	common.OptionMapRWMutex.RUnlock()
	recorded, err := decodeRecordedNpmCLIVersionOptions(raw)
	if err != nil {
		return nil
	}
	lastError, exists := recorded.LastErrors[normalizedPackageName]
	if !exists {
		return nil
	}
	normalizedLastError, ok := normalizeRecordedNpmCLIVersionLastError(lastError)
	if !ok {
		return nil
	}
	return &normalizedLastError
}

func getRecordedNpmCLIVersionRecentErrors(packageName string) []NpmCLIVersionLastError {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return nil
	}
	common.OptionMapRWMutex.RLock()
	raw := strings.TrimSpace(common.OptionMap[npmCliVersionRecordedOptionsKey])
	common.OptionMapRWMutex.RUnlock()
	recorded, err := decodeRecordedNpmCLIVersionOptions(raw)
	if err != nil {
		return nil
	}
	return mergeNpmCLIVersionRecentErrors(
		recorded.RecentErrors[normalizedPackageName],
		[]NpmCLIVersionLastError{recorded.LastErrors[normalizedPackageName]},
	)
}

func snapshotNpmCLIVersionLastErrors() map[string]NpmCLIVersionLastError {
	npmCliVersionLastErrors.RLock()
	defer npmCliVersionLastErrors.RUnlock()

	lastErrors := map[string]NpmCLIVersionLastError{}
	for _, packageName := range allowedNpmCLIPackageNames() {
		lastError, exists := npmCliVersionLastErrors.items[packageName]
		if !exists {
			continue
		}
		normalizedLastError, ok := normalizeRecordedNpmCLIVersionLastError(lastError)
		if !ok {
			continue
		}
		lastErrors[packageName] = normalizedLastError
	}
	return lastErrors
}

func snapshotNpmCLIVersionRecentErrors() map[string][]NpmCLIVersionLastError {
	npmCliVersionRecentErrors.RLock()
	defer npmCliVersionRecentErrors.RUnlock()

	recentErrors := map[string][]NpmCLIVersionLastError{}
	for _, packageName := range allowedNpmCLIPackageNames() {
		history := mergeNpmCLIVersionRecentErrors(npmCliVersionRecentErrors.items[packageName])
		if len(history) == 0 {
			continue
		}
		recentErrors[packageName] = history
	}
	return recentErrors
}

func cloneNpmCLIVersionLastErrors(errors []NpmCLIVersionLastError) []NpmCLIVersionLastError {
	if len(errors) == 0 {
		return nil
	}
	cloned := make([]NpmCLIVersionLastError, len(errors))
	copy(cloned, errors)
	return cloned
}

func mergeNpmCLIVersionRecentErrors(errorLists ...[]NpmCLIVersionLastError) []NpmCLIVersionLastError {
	seen := map[string]struct{}{}
	merged := make([]NpmCLIVersionLastError, 0, npmCliVersionRecentErrorLimit)
	for _, errorList := range errorLists {
		for _, lastError := range errorList {
			normalizedLastError, ok := normalizeRecordedNpmCLIVersionLastError(lastError)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%s\x00%s\x00%d", normalizedLastError.Code, normalizedLastError.Source, normalizedLastError.UpdatedAt.UnixNano())
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, normalizedLastError)
		}
	}
	sort.SliceStable(merged, func(i int, j int) bool {
		return merged[i].UpdatedAt.After(merged[j].UpdatedAt)
	})
	if len(merged) > npmCliVersionRecentErrorLimit {
		merged = merged[:npmCliVersionRecentErrorLimit]
	}
	return merged
}
