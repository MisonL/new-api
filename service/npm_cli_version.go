package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"golang.org/x/sync/singleflight"
)

const (
	npmCliVersionOptionLimit       = 5
	npmCliVersionRecentErrorLimit  = 5
	npmCliVersionRefreshInterval   = 10 * time.Minute
	npmRegistryRequestTimeout      = 5 * time.Second
	npmRegistryMetadataMaxBytes    = 4 << 20
	defaultNpmRegistryMetadataURL  = "https://registry.npmjs.org"
	NpmCLIVersionLatestAlias       = "latest"
	NpmCLIVersionLoadFailedCode    = "npm_version_load_failed"
	NpmCLIVersionEmptyCode         = "npm_version_empty"
	NpmCLIVersionNotRecordedCode   = "npm_version_not_recorded"
	NpmCLIVersionPersistFailedCode = "npm_version_persist_failed"
	NpmCLIPackageRequiredCode      = "npm_package_required"
	NpmCLIPackageUnsupportedCode   = "npm_package_unsupported"
	NpmCLIVersionHTTPStatusCode    = "npm_registry_http_status"
	NpmCLIVersionDecodeFailedCode  = "npm_registry_decode_failed"
	NpmCLIVersionTimeoutCode       = "npm_registry_timeout"

	npmCLIVersionActionNone                         = "none"
	npmCLIVersionActionRefreshPackage               = "refresh_package"
	npmCLIVersionActionCheckNpmRegistryConnectivity = "check_npm_registry_connectivity"
	npmCLIVersionActionCheckDatabasePersistence     = "check_database_persistence"
	npmCLIVersionActionInspectRegistryMetadata      = "inspect_registry_metadata"
	npmCLIVersionActionCheckSchedulerOrMasterNode   = "check_scheduler_or_master_node"
)

type npmCLIVersionCodedError struct {
	code    string
	message string
}

func (err *npmCLIVersionCodedError) Error() string {
	return err.message
}

func (err *npmCLIVersionCodedError) NpmCLIVersionCode() string {
	return err.code
}

func newNpmCLIVersionCodedError(code string, format string, args ...any) error {
	return &npmCLIVersionCodedError{
		code:    code,
		message: fmt.Sprintf(format, args...),
	}
}

var allowedNpmCLIPackages = map[string]struct{}{
	"@openai/codex":             {},
	"@anthropic-ai/claude-code": {},
	"@google/gemini-cli":        {},
	"@qwen-code/qwen-code":      {},
	"droid":                     {},
}

var npmCliVersionCache = struct {
	sync.RWMutex
	items map[string]npmCliVersionCacheEntry
}{
	items: make(map[string]npmCliVersionCacheEntry),
}

var npmCliVersionLastErrors = struct {
	sync.RWMutex
	items map[string]NpmCLIVersionLastError
}{
	items: make(map[string]NpmCLIVersionLastError),
}

var npmCliVersionRecentErrors = struct {
	sync.RWMutex
	items map[string][]NpmCLIVersionLastError
}{
	items: make(map[string][]NpmCLIVersionLastError),
}

var npmCLIVersionManualRefreshGroup singleflight.Group

type NpmCLIVersionOption struct {
	Value           string `json:"value"`
	Label           string `json:"label"`
	IsLatest        bool   `json:"isLatest"`
	ResolvedVersion string `json:"resolvedVersion,omitempty"`
	Source          string `json:"source,omitempty"`
}

type NpmCLIVersionOptionsResponse struct {
	PackageName   string                `json:"package"`
	Source        string                `json:"source"`
	RefreshedAt   *time.Time            `json:"refreshed_at,omitempty"`
	LatestVersion string                `json:"latest_version,omitempty"`
	Options       []NpmCLIVersionOption `json:"options"`
}

type NpmCLIVersionLastError struct {
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NpmCLIVersionDiagnostic struct {
	PackageName       string                   `json:"package"`
	Source            string                   `json:"source"`
	RefreshedAt       *time.Time               `json:"refreshed_at,omitempty"`
	CacheAgeMs        int64                    `json:"cache_age_ms,omitempty"`
	LatestVersion     string                   `json:"latest_version,omitempty"`
	OptionCount       int                      `json:"option_count"`
	Recorded          bool                     `json:"recorded"`
	RecommendedAction string                   `json:"recommended_action"`
	LastError         *NpmCLIVersionLastError  `json:"last_error,omitempty"`
	LastErrorScope    string                   `json:"last_error_scope,omitempty"`
	RecentErrors      []NpmCLIVersionLastError `json:"recent_errors"`
}

type NpmCLIVersionDiagnosticsResponse struct {
	GeneratedAt       time.Time                       `json:"generated_at"`
	RefreshIntervalMs int64                           `json:"refresh_interval_ms"`
	RegistryTimeoutMs int64                           `json:"registry_timeout_ms"`
	Summary           NpmCLIVersionDiagnosticsSummary `json:"summary"`
	Metrics           NpmCLIVersionRefreshMetrics     `json:"metrics"`
	Packages          []NpmCLIVersionDiagnostic       `json:"packages"`
}

type npmCliVersionCacheEntry struct {
	fetchedAt     time.Time
	latestVersion string
	options       []NpmCLIVersionOption
	source        string
}

type npmPackageMetadata struct {
	DistTags map[string]string            `json:"dist-tags"`
	Versions map[string]common.RawMessage `json:"versions"`
}

func IsAllowedNpmCLIPackage(packageName string) bool {
	_, exists := allowedNpmCLIPackages[strings.TrimSpace(packageName)]
	return exists
}

func NpmCLIVersionErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var codedError interface {
		NpmCLIVersionCode() string
	}
	if errors.As(err, &codedError) {
		return codedError.NpmCLIVersionCode()
	}
	return NpmCLIVersionLoadFailedCode
}

func NpmCLIVersionPublicErrorMessage(err error) string {
	code := NpmCLIVersionErrorCode(err)
	if code == NpmCLIPackageRequiredCode || code == NpmCLIPackageUnsupportedCode {
		return err.Error()
	}
	return npmCLIVersionSafeLastErrorMessage(code)
}

func npmCLIVersionSafeLastErrorMessage(code string) string {
	switch code {
	case NpmCLIVersionTimeoutCode:
		return "npm registry request timed out"
	case NpmCLIVersionHTTPStatusCode:
		return "npm registry returned an unsuccessful HTTP status"
	case NpmCLIVersionDecodeFailedCode:
		return "npm registry metadata could not be decoded"
	case NpmCLIVersionPersistFailedCode:
		return "npm version options could not be persisted"
	case NpmCLIVersionEmptyCode:
		return "npm registry returned no usable versions"
	case NpmCLIVersionNotRecordedCode:
		return "npm version options are not recorded"
	case NpmCLIPackageRequiredCode:
		return "npm package is required"
	case NpmCLIPackageUnsupportedCode:
		return "npm package is unsupported"
	default:
		return "npm version options could not be loaded"
	}
}

func classifyNpmCLIVersionFetchError(err error) string {
	if err == nil {
		return ""
	}
	code := NpmCLIVersionErrorCode(err)
	if code != NpmCLIVersionLoadFailedCode {
		return code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NpmCLIVersionTimeoutCode
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return NpmCLIVersionTimeoutCode
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "context deadline exceeded"),
		strings.Contains(message, "timeout"),
		strings.Contains(message, "timed out"):
		return NpmCLIVersionTimeoutCode
	case strings.Contains(message, "status "):
		return NpmCLIVersionHTTPStatusCode
	case strings.Contains(message, "decode npm registry metadata"),
		strings.Contains(message, "invalid character"),
		strings.Contains(message, "metadata exceeds maximum size"):
		return NpmCLIVersionDecodeFailedCode
	default:
		return NpmCLIVersionLoadFailedCode
	}
}

func recordNpmCLIVersionLastError(ctx context.Context, packageName string, source string, code string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return
	}
	if strings.TrimSpace(code) == "" {
		code = NpmCLIVersionLoadFailedCode
	}
	lastError := NpmCLIVersionLastError{
		Code:      code,
		Message:   npmCLIVersionSafeLastErrorMessage(code),
		Source:    strings.TrimSpace(source),
		UpdatedAt: time.Now(),
	}
	npmCliVersionLastErrors.Lock()
	npmCliVersionLastErrors.items[normalizedPackageName] = lastError
	npmCliVersionLastErrors.Unlock()
	appendNpmCLIVersionRecentError(normalizedPackageName, lastError)
	if err := persistRecordedNpmCLIVersionLastError(ctx, normalizedPackageName, &lastError); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("persist npm cli version last error failed: package=%s error=%v", normalizedPackageName, err))
	}
}

func appendNpmCLIVersionRecentError(packageName string, lastError NpmCLIVersionLastError) {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" || !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return
	}
	normalizedLastError, ok := normalizeRecordedNpmCLIVersionLastError(lastError)
	if !ok {
		return
	}
	npmCliVersionRecentErrors.Lock()
	npmCliVersionRecentErrors.items[normalizedPackageName] = mergeNpmCLIVersionRecentErrors(
		[]NpmCLIVersionLastError{normalizedLastError},
		npmCliVersionRecentErrors.items[normalizedPackageName],
	)
	npmCliVersionRecentErrors.Unlock()
}

func clearNpmCLIVersionLastError(packageName string) {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" {
		return
	}
	npmCliVersionLastErrors.Lock()
	delete(npmCliVersionLastErrors.items, normalizedPackageName)
	npmCliVersionLastErrors.Unlock()
}

func getNpmCLIVersionLastError(packageName string) *NpmCLIVersionLastError {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" {
		return nil
	}
	npmCliVersionLastErrors.RLock()
	lastError, ok := npmCliVersionLastErrors.items[normalizedPackageName]
	npmCliVersionLastErrors.RUnlock()
	if !ok {
		return nil
	}
	return &lastError
}

func getNpmCLIVersionRecentErrors(packageName string) []NpmCLIVersionLastError {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" {
		return nil
	}
	npmCliVersionRecentErrors.RLock()
	recentErrors := cloneNpmCLIVersionLastErrors(npmCliVersionRecentErrors.items[normalizedPackageName])
	npmCliVersionRecentErrors.RUnlock()
	return recentErrors
}

func getNpmCLIVersionDiagnosticLastError(packageName string, refreshedAt time.Time) (*NpmCLIVersionLastError, string) {
	if lastError := getNpmCLIVersionLastError(packageName); lastError != nil && !npmCLIVersionLastErrorCoveredByRefresh(lastError, refreshedAt) {
		return lastError, "process"
	}
	if lastError := getRecordedNpmCLIVersionLastError(packageName); lastError != nil && !npmCLIVersionLastErrorCoveredByRefresh(lastError, refreshedAt) {
		return lastError, "recorded"
	}
	return nil, ""
}

func getNpmCLIVersionDiagnosticRecentErrors(packageName string) []NpmCLIVersionLastError {
	return mergeNpmCLIVersionRecentErrors(
		getNpmCLIVersionRecentErrors(packageName),
		getRecordedNpmCLIVersionRecentErrors(packageName),
	)
}

func npmCLIVersionLastErrorCoveredByRefresh(lastError *NpmCLIVersionLastError, refreshedAt time.Time) bool {
	return lastError != nil && !refreshedAt.IsZero() && !refreshedAt.Before(lastError.UpdatedAt)
}

func npmCLIVersionCacheAgeMilliseconds(now time.Time, fetchedAt time.Time) int64 {
	if now.IsZero() || fetchedAt.IsZero() || fetchedAt.After(now) {
		return 0
	}
	return now.Sub(fetchedAt).Milliseconds()
}

func FetchNpmCLIVersionOptions(ctx context.Context, packageName string) ([]NpmCLIVersionOption, error) {
	response, err := FetchNpmCLIVersionOptionsResponse(ctx, packageName)
	if err != nil {
		return nil, err
	}
	return response.Options, nil
}

func buildNpmCLIVersionOptionsResponse(packageName string, source string, entry npmCliVersionCacheEntry) *NpmCLIVersionOptionsResponse {
	effectiveSource := strings.TrimSpace(source)
	if effectiveSource == "" {
		effectiveSource = strings.TrimSpace(entry.source)
	}
	if effectiveSource == "" {
		effectiveSource = "recorded"
	}
	response := &NpmCLIVersionOptionsResponse{
		PackageName:   packageName,
		Source:        effectiveSource,
		LatestVersion: entry.latestVersion,
		Options:       cloneNpmCLIVersionOptions(entry.options),
	}
	if !entry.fetchedAt.IsZero() {
		refreshedAt := entry.fetchedAt
		response.RefreshedAt = &refreshedAt
	}
	return response
}

func cloneNpmCLIVersionOptionsResponse(response *NpmCLIVersionOptionsResponse) *NpmCLIVersionOptionsResponse {
	if response == nil {
		return nil
	}
	cloned := *response
	cloned.Options = cloneNpmCLIVersionOptions(response.Options)
	if response.RefreshedAt != nil {
		refreshedAt := *response.RefreshedAt
		cloned.RefreshedAt = &refreshedAt
	}
	return &cloned
}

func GetNpmCLIVersionDiagnostics() NpmCLIVersionDiagnosticsResponse {
	return GetNpmCLIVersionDiagnosticsWithContext(context.Background())
}

func GetNpmCLIVersionDiagnosticsWithContext(ctx context.Context) NpmCLIVersionDiagnosticsResponse {
	loadRecordedNpmCLIVersionOptions()
	loadRecordedNpmCLIVersionOptionsFromDatabase(ctx)

	now := time.Now()
	packageNames := allowedNpmCLIPackageNames()
	items := make([]NpmCLIVersionDiagnostic, 0, len(packageNames))
	for _, packageName := range packageNames {
		entry, ok := getCachedNpmCLIVersionOptionsEntry(packageName)
		lastError, lastErrorScope := getNpmCLIVersionDiagnosticLastError(packageName, entry.fetchedAt)
		diag := NpmCLIVersionDiagnostic{
			PackageName:  packageName,
			Source:       "missing",
			LastError:    lastError,
			RecentErrors: getNpmCLIVersionDiagnosticRecentErrors(packageName),
		}
		if lastError != nil {
			diag.LastErrorScope = lastErrorScope
		}
		if ok {
			source := strings.TrimSpace(entry.source)
			if source == "" {
				source = "recorded"
			}
			diag.Source = source
			if !entry.fetchedAt.IsZero() {
				refreshedAt := entry.fetchedAt
				diag.RefreshedAt = &refreshedAt
				diag.CacheAgeMs = npmCLIVersionCacheAgeMilliseconds(now, entry.fetchedAt)
			}
			diag.LatestVersion = entry.latestVersion
			diag.OptionCount = len(entry.options)
			diag.Recorded = true
		}
		diag.RecommendedAction = recommendNpmCLIVersionDiagnosticAction(diag)
		items = append(items, diag)
	}
	return NpmCLIVersionDiagnosticsResponse{
		GeneratedAt:       now,
		RefreshIntervalMs: npmCliVersionRefreshInterval.Milliseconds(),
		RegistryTimeoutMs: npmRegistryRequestTimeout.Milliseconds(),
		Summary:           summarizeNpmCLIVersionDiagnostics(items),
		Metrics:           snapshotNpmCLIVersionRefreshMetrics(),
		Packages:          items,
	}
}

func recommendNpmCLIVersionDiagnosticAction(diag NpmCLIVersionDiagnostic) string {
	if diag.LastError != nil {
		switch diag.LastError.Code {
		case NpmCLIVersionPersistFailedCode:
			return npmCLIVersionActionCheckDatabasePersistence
		case NpmCLIVersionTimeoutCode, NpmCLIVersionHTTPStatusCode, NpmCLIVersionLoadFailedCode:
			return npmCLIVersionActionCheckNpmRegistryConnectivity
		case NpmCLIVersionDecodeFailedCode, NpmCLIVersionEmptyCode:
			return npmCLIVersionActionInspectRegistryMetadata
		}
	}
	if !diag.Recorded {
		return npmCLIVersionActionRefreshPackage
	}
	if diag.CacheAgeMs > 2*npmCliVersionRefreshInterval.Milliseconds() {
		return npmCLIVersionActionCheckSchedulerOrMasterNode
	}
	return npmCLIVersionActionNone
}

func RefreshNpmCLIVersionOptionsResponse(ctx context.Context, packageName string) (*NpmCLIVersionOptionsResponse, error) {
	startedAt := time.Now()
	normalizedPackageName := strings.TrimSpace(packageName)
	refreshCode := ""
	refreshSucceeded := false
	defer func() {
		recordNpmCLIVersionManualRefresh(normalizedPackageName, refreshCode, refreshSucceeded, startedAt)
	}()
	if normalizedPackageName == "" {
		refreshCode = NpmCLIPackageRequiredCode
		return nil, newNpmCLIVersionCodedError(NpmCLIPackageRequiredCode, "package is required")
	}
	if !IsAllowedNpmCLIPackage(normalizedPackageName) {
		refreshCode = NpmCLIPackageUnsupportedCode
		return nil, newNpmCLIVersionCodedError(NpmCLIPackageUnsupportedCode, "unsupported npm package: %s", normalizedPackageName)
	}

	value, err, _ := npmCLIVersionManualRefreshGroup.Do(normalizedPackageName, func() (any, error) {
		return refreshNpmCLIVersionOptionsResponse(ctx, normalizedPackageName)
	})
	if err != nil {
		refreshCode = NpmCLIVersionErrorCode(err)
		return nil, err
	}
	response, ok := value.(*NpmCLIVersionOptionsResponse)
	if !ok || response == nil {
		refreshCode = NpmCLIVersionLoadFailedCode
		return nil, newNpmCLIVersionCodedError(NpmCLIVersionLoadFailedCode, "refreshed npm version options returned no response for package: %s", normalizedPackageName)
	}
	refreshSucceeded = true
	return cloneNpmCLIVersionOptionsResponse(response), nil
}

func refreshNpmCLIVersionOptionsResponse(ctx context.Context, normalizedPackageName string) (*NpmCLIVersionOptionsResponse, error) {
	options, err := fetchNpmCLIVersionOptions(ctx, normalizedPackageName, defaultNpmRegistryHTTPClient(), defaultNpmRegistryMetadataURL)
	if err != nil {
		code := classifyNpmCLIVersionFetchError(err)
		recordNpmCLIVersionLastError(ctx, normalizedPackageName, "npm", code, err)
		return nil, newNpmCLIVersionCodedError(code, "%v", err)
	}
	fetchedAt := time.Now()
	if err := persistRecordedNpmCLIVersionOptionsWithPackage(ctx, normalizedPackageName, options, fetchedAt); err != nil {
		recordNpmCLIVersionLastError(ctx, normalizedPackageName, "persist", NpmCLIVersionPersistFailedCode, err)
		return nil, newNpmCLIVersionCodedError(
			NpmCLIVersionPersistFailedCode,
			"persist refreshed npm version options for %s: %v",
			normalizedPackageName,
			err,
		)
	}
	clearNpmCLIVersionLastError(normalizedPackageName)
	setCachedNpmCLIVersionOptionsWithFetchedAtAndSource(normalizedPackageName, options, fetchedAt, "npm")
	entry, ok := getCachedNpmCLIVersionOptionsEntry(normalizedPackageName)
	if !ok {
		return nil, newNpmCLIVersionCodedError(NpmCLIVersionLoadFailedCode, "refreshed npm version options not cached for package: %s", normalizedPackageName)
	}
	return buildNpmCLIVersionOptionsResponse(normalizedPackageName, "npm", entry), nil
}

func FetchNpmCLIVersionOptionsResponse(ctx context.Context, packageName string) (*NpmCLIVersionOptionsResponse, error) {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" {
		return nil, newNpmCLIVersionCodedError(NpmCLIPackageRequiredCode, "package is required")
	}
	if !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return nil, newNpmCLIVersionCodedError(NpmCLIPackageUnsupportedCode, "unsupported npm package: %s", normalizedPackageName)
	}

	loadRecordedNpmCLIVersionOptions()
	if entry, ok := getCachedNpmCLIVersionOptionsEntry(normalizedPackageName); ok {
		return buildNpmCLIVersionOptionsResponse(normalizedPackageName, "recorded", entry), nil
	}
	loadRecordedNpmCLIVersionOptionsFromDatabase(ctx)
	if entry, ok := getCachedNpmCLIVersionOptionsEntry(normalizedPackageName); ok {
		return buildNpmCLIVersionOptionsResponse(normalizedPackageName, "recorded", entry), nil
	}

	return nil, newNpmCLIVersionCodedError(NpmCLIVersionNotRecordedCode, "npm version options not recorded for package: %s", normalizedPackageName)
}

func fetchNpmCLIVersionOptions(ctx context.Context, packageName string, client *http.Client, registryBaseURL string) ([]NpmCLIVersionOption, error) {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" {
		return nil, newNpmCLIVersionCodedError(NpmCLIPackageRequiredCode, "package is required")
	}
	if !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return nil, newNpmCLIVersionCodedError(NpmCLIPackageUnsupportedCode, "unsupported npm package: %s", normalizedPackageName)
	}
	if client == nil {
		client = defaultNpmRegistryHTTPClient()
	}
	if strings.TrimSpace(registryBaseURL) == "" {
		registryBaseURL = defaultNpmRegistryMetadataURL
	}
	if ctx == nil {
		ctx = context.Background()
	}

	requestCtx, cancel := context.WithTimeout(ctx, npmRegistryRequestTimeout)
	defer cancel()

	requestURL := strings.TrimRight(registryBaseURL, "/") + "/" + url.QueryEscape(normalizedPackageName)
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build npm registry request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.npm.install-v1+json")

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch npm registry metadata for %s: %w", normalizedPackageName, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("npm registry responded with status %d for %s", response.StatusCode, normalizedPackageName)
	}

	var metadata npmPackageMetadata
	if err := decodeNpmPackageMetadata(response.Body, &metadata); err != nil {
		return nil, fmt.Errorf("decode npm registry metadata for %s: %w", normalizedPackageName, err)
	}

	options := buildNpmCLIVersionOptions(metadata)
	if len(options) == 0 {
		return nil, newNpmCLIVersionCodedError(NpmCLIVersionEmptyCode, "npm registry metadata for %s contains no usable versions", normalizedPackageName)
	}
	return options, nil
}

func defaultNpmRegistryHTTPClient() *http.Client {
	if client := GetHttpClient(); client != nil {
		return client
	}
	return http.DefaultClient
}

func cloneNpmCLIVersionOptions(options []NpmCLIVersionOption) []NpmCLIVersionOption {
	if len(options) == 0 {
		return []NpmCLIVersionOption{}
	}
	cloned := make([]NpmCLIVersionOption, len(options))
	copy(cloned, options)
	return cloned
}

func decodeNpmPackageMetadata(reader io.Reader, metadata *npmPackageMetadata) error {
	data, err := io.ReadAll(io.LimitReader(reader, npmRegistryMetadataMaxBytes+1))
	if err != nil {
		return fmt.Errorf("read metadata: %w", err)
	}
	if len(data) > npmRegistryMetadataMaxBytes {
		return fmt.Errorf("metadata exceeds maximum size of %d bytes", npmRegistryMetadataMaxBytes)
	}
	return common.Unmarshal(data, metadata)
}

func buildNpmCLIVersionOptions(metadata npmPackageMetadata) []NpmCLIVersionOption {
	availableVersions := make(map[string]struct{}, len(metadata.Versions))
	stableVersions := make([]string, 0, len(metadata.Versions))
	for version := range metadata.Versions {
		normalizedVersion := normalizeNpmCLIVersionValue(version)
		if normalizedVersion == "" {
			continue
		}
		if _, exists := availableVersions[normalizedVersion]; exists {
			continue
		}
		availableVersions[normalizedVersion] = struct{}{}
		if _, ok := parseStableVersion(normalizedVersion); ok {
			stableVersions = append(stableVersions, normalizedVersion)
		}
	}

	latestVersion := normalizeNpmCLIVersionValue(metadata.DistTags["latest"])
	if latestVersion != "" {
		if _, exists := availableVersions[latestVersion]; !exists {
			latestVersion = ""
		}
	}
	sort.SliceStable(stableVersions, func(i, j int) bool {
		return compareStableVersionsDesc(stableVersions[i], stableVersions[j]) < 0
	})
	if latestVersion == "" && len(stableVersions) > 0 {
		latestVersion = stableVersions[0]
	}

	selectedVersions := make([]string, 0, npmCliVersionOptionLimit)
	selectedVersions = addUniqueNpmVersion(selectedVersions, latestVersion)
	for _, version := range stableVersions {
		selectedVersions = addUniqueNpmVersion(selectedVersions, version)
		if len(selectedVersions) >= npmCliVersionOptionLimit {
			break
		}
	}

	options := make([]NpmCLIVersionOption, 0, len(selectedVersions)+1)
	if latestVersion != "" {
		options = append(options, NpmCLIVersionOption{
			Value:           NpmCLIVersionLatestAlias,
			Label:           fmt.Sprintf("%s (%s)", NpmCLIVersionLatestAlias, latestVersion),
			IsLatest:        true,
			ResolvedVersion: latestVersion,
		})
	}
	for _, version := range selectedVersions {
		option := NpmCLIVersionOption{
			Value:           version,
			Label:           version,
			IsLatest:        false,
			ResolvedVersion: version,
		}
		options = append(options, option)
	}
	return options
}

func latestNpmCLIVersionFromOptions(options []NpmCLIVersionOption) string {
	for _, option := range options {
		if strings.TrimSpace(option.Value) == NpmCLIVersionLatestAlias {
			if version := normalizeNpmCLIVersionValue(option.ResolvedVersion); version != "" {
				return version
			}
			continue
		}
		if option.IsLatest {
			if version := normalizeNpmCLIVersionValue(option.ResolvedVersion); version != "" {
				return version
			}
			if version := normalizeNpmCLIVersionValue(option.Value); version != "" {
				return version
			}
		}
	}
	return ""
}

func addUniqueNpmVersion(target []string, version string) []string {
	normalizedVersion := normalizeNpmCLIVersionValue(version)
	if normalizedVersion == "" {
		return target
	}
	for _, existing := range target {
		if existing == normalizedVersion {
			return target
		}
	}
	return append(target, normalizedVersion)
}

func normalizeNpmCLIVersionValue(version string) string {
	normalizedVersion := strings.TrimSpace(version)
	if normalizedVersion == "" ||
		normalizedVersion == NpmCLIVersionLatestAlias ||
		len(normalizedVersion) > 64 {
		return ""
	}
	for index, char := range normalizedVersion {
		if index == 0 && (char < '0' || char > '9') {
			return ""
		}
		if char >= '0' && char <= '9' {
			continue
		}
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char == '.' || char == '-' || char == '+' {
			continue
		}
		return ""
	}
	return normalizedVersion
}

func compareStableVersionsDesc(left string, right string) int {
	leftParts, leftOK := parseStableVersion(left)
	rightParts, rightOK := parseStableVersion(right)
	if !leftOK && !rightOK {
		return 0
	}
	if !leftOK {
		return 1
	}
	if !rightOK {
		return -1
	}
	for index := 0; index < len(leftParts); index++ {
		if leftParts[index] != rightParts[index] {
			return rightParts[index] - leftParts[index]
		}
	}
	return 0
}

func parseStableVersion(version string) ([3]int, bool) {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var parsed [3]int
	for index, part := range parts {
		if part == "" {
			return [3]int{}, false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return [3]int{}, false
			}
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, false
		}
		parsed[index] = value
	}
	return parsed, true
}
