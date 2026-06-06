package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	npmCliVersionOptionLimit      = 5
	npmCliVersionRefreshInterval  = 10 * time.Minute
	npmRegistryRequestTimeout     = 5 * time.Second
	npmRegistryMetadataMaxBytes   = 4 << 20
	defaultNpmRegistryMetadataURL = "https://registry.npmjs.org"
	NpmCLIVersionLatestAlias      = "latest"
)

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

type NpmCLIVersionOption struct {
	Value           string `json:"value"`
	Label           string `json:"label"`
	IsLatest        bool   `json:"isLatest"`
	ResolvedVersion string `json:"resolvedVersion,omitempty"`
}

type npmCliVersionCacheEntry struct {
	fetchedAt     time.Time
	latestVersion string
	options       []NpmCLIVersionOption
}

type npmPackageMetadata struct {
	DistTags map[string]string            `json:"dist-tags"`
	Versions map[string]common.RawMessage `json:"versions"`
}

func IsAllowedNpmCLIPackage(packageName string) bool {
	_, exists := allowedNpmCLIPackages[strings.TrimSpace(packageName)]
	return exists
}

func FetchNpmCLIVersionOptions(ctx context.Context, packageName string) ([]NpmCLIVersionOption, error) {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" {
		return nil, fmt.Errorf("package is required")
	}
	if !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return nil, fmt.Errorf("unsupported npm package: %s", normalizedPackageName)
	}

	loadRecordedNpmCLIVersionOptions()
	if options, ok := getCachedNpmCLIVersionOptions(normalizedPackageName); ok {
		return options, nil
	}

	return nil, fmt.Errorf("npm version options not recorded for package: %s", normalizedPackageName)
}

func fetchNpmCLIVersionOptions(ctx context.Context, packageName string, client *http.Client, registryBaseURL string) ([]NpmCLIVersionOption, error) {
	normalizedPackageName := strings.TrimSpace(packageName)
	if normalizedPackageName == "" {
		return nil, fmt.Errorf("package is required")
	}
	if !IsAllowedNpmCLIPackage(normalizedPackageName) {
		return nil, fmt.Errorf("unsupported npm package: %s", normalizedPackageName)
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
		return nil, fmt.Errorf("npm registry metadata for %s contains no usable versions", normalizedPackageName)
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
