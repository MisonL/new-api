package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestIsAllowedNpmCLIPackage(t *testing.T) {
	tests := []struct {
		name        string
		packageName string
		allowed     bool
	}{
		{name: "codex", packageName: "@openai/codex", allowed: true},
		{name: "claude", packageName: "@anthropic-ai/claude-code", allowed: true},
		{name: "gemini", packageName: "@google/gemini-cli", allowed: true},
		{name: "qwen", packageName: "@qwen-code/qwen-code", allowed: true},
		{name: "droid", packageName: "droid", allowed: true},
		{name: "trimmed", packageName: "  @openai/codex  ", allowed: true},
		{name: "unknown", packageName: "@scope/unknown", allowed: false},
		{name: "empty", packageName: " ", allowed: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.allowed, IsAllowedNpmCLIPackage(test.packageName))
		})
	}
}

func TestBuildNpmCLIVersionOptionsLatestFirstThenStableDesc(t *testing.T) {
	options := buildNpmCLIVersionOptions(npmPackageMetadata{
		DistTags: map[string]string{
			"latest": "1.2.3",
		},
		Versions: map[string]common.RawMessage{
			"0.9.0":        {},
			"1.2.3":        {},
			"1.10.0":       {},
			"2.0.0":        {},
			"3.0.0-beta.1": {},
			"10.0.0":       {},
			"invalid":      {},
		},
	})

	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.2.3)", IsLatest: true, ResolvedVersion: "1.2.3"},
		{Value: "1.2.3", Label: "1.2.3", IsLatest: false, ResolvedVersion: "1.2.3"},
		{Value: "10.0.0", Label: "10.0.0", IsLatest: false, ResolvedVersion: "10.0.0"},
		{Value: "2.0.0", Label: "2.0.0", IsLatest: false, ResolvedVersion: "2.0.0"},
		{Value: "1.10.0", Label: "1.10.0", IsLatest: false, ResolvedVersion: "1.10.0"},
		{Value: "0.9.0", Label: "0.9.0", IsLatest: false, ResolvedVersion: "0.9.0"},
	}, options)
}

func TestBuildNpmCLIVersionOptionsKeepsPrereleaseLatestOnly(t *testing.T) {
	options := buildNpmCLIVersionOptions(npmPackageMetadata{
		DistTags: map[string]string{
			"latest": "3.0.0-beta.1",
		},
		Versions: map[string]common.RawMessage{
			"1.0.0":        {},
			"2.0.0":        {},
			"3.0.0-beta.1": {},
		},
	})

	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (3.0.0-beta.1)", IsLatest: true, ResolvedVersion: "3.0.0-beta.1"},
		{Value: "3.0.0-beta.1", Label: "3.0.0-beta.1", IsLatest: false, ResolvedVersion: "3.0.0-beta.1"},
		{Value: "2.0.0", Label: "2.0.0", IsLatest: false, ResolvedVersion: "2.0.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	}, options)
}

func TestBuildNpmCLIVersionOptionsFallsBackToHighestStableWhenLatestMissing(t *testing.T) {
	options := buildNpmCLIVersionOptions(npmPackageMetadata{
		DistTags: map[string]string{},
		Versions: map[string]common.RawMessage{
			"1.0.0": {},
			"1.2.0": {},
		},
	})

	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.2.0)", IsLatest: true, ResolvedVersion: "1.2.0"},
		{Value: "1.2.0", Label: "1.2.0", IsLatest: false, ResolvedVersion: "1.2.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	}, options)
}

func TestBuildNpmCLIVersionOptionsRejectsInvalidLatestTag(t *testing.T) {
	options := buildNpmCLIVersionOptions(npmPackageMetadata{
		DistTags: map[string]string{
			"latest": "1.2.0\nInjected",
		},
		Versions: map[string]common.RawMessage{
			"1.0.0": {},
			"1.2.0": {},
		},
	})

	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.2.0)", IsLatest: true, ResolvedVersion: "1.2.0"},
		{Value: "1.2.0", Label: "1.2.0", IsLatest: false, ResolvedVersion: "1.2.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	}, options)
}

func TestBuildNpmCLIVersionOptionsRequiresLatestTagInVersions(t *testing.T) {
	options := buildNpmCLIVersionOptions(npmPackageMetadata{
		DistTags: map[string]string{
			"latest": "9.9.9",
		},
		Versions: map[string]common.RawMessage{
			"1.0.0": {},
			"1.2.0": {},
		},
	})

	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.2.0)", IsLatest: true, ResolvedVersion: "1.2.0"},
		{Value: "1.2.0", Label: "1.2.0", IsLatest: false, ResolvedVersion: "1.2.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	}, options)
}

func TestFetchNpmCLIVersionOptionsRejectsEmptyUsableRegistryVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := common.Marshal(map[string]any{
			"dist-tags": map[string]string{
				"latest": "latest",
			},
			"versions": map[string]any{
				"not-a-version": map[string]any{},
			},
		})
		require.NoError(t, err)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	_, err := fetchNpmCLIVersionOptions(context.Background(), "@openai/codex", server.Client(), server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "contains no usable versions")
	require.Equal(t, NpmCLIVersionEmptyCode, NpmCLIVersionErrorCode(err))
}

func TestFetchNpmCLIVersionOptionsRejectsUnsupportedPackage(t *testing.T) {
	_, err := fetchNpmCLIVersionOptions(context.Background(), "@scope/unknown", http.DefaultClient, "https://registry.npmjs.org")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported npm package")
}

func TestFetchNpmCLIVersionOptionsRequiresRecordedOptions(t *testing.T) {
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("registry should not be requested")
	})}

	_, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.Error(t, err)
	require.Contains(t, err.Error(), "npm version options not recorded")
}

func TestFetchNpmCLIVersionOptionsCachesSuccessfulResults(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptions("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	})

	firstOptions, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.NoError(t, err)
	firstOptions[0].Value = "mutated"
	secondOptions, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.NoError(t, err)

	require.Equal(t, "latest", secondOptions[0].Value)
	require.Equal(t, "1.0.0", secondOptions[0].ResolvedVersion)
}

func TestFetchNpmCLIVersionOptionsResponseIncludesRecordedMetadata(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	fetchedAt := time.Unix(1000, 0).UTC()
	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	}, fetchedAt)

	response, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.NoError(t, err)

	require.Equal(t, "@openai/codex", response.PackageName)
	require.Equal(t, "recorded", response.Source)
	require.NotNil(t, response.RefreshedAt)
	require.Equal(t, fetchedAt, *response.RefreshedAt)
	require.Equal(t, "1.0.0", response.LatestVersion)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	}, response.Options)
}

func TestFetchNpmCLIVersionOptionsResponseOmitsZeroRefreshedAt(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	}, time.Time{})

	response, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Nil(t, response.RefreshedAt)

	payload, err := common.Marshal(response)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "refreshed_at")
	require.NotContains(t, string(payload), "0001-01-01T00:00:00Z")
}

func TestFetchNpmCLIVersionOptionsResponseRequiresRecordedOptions(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	response, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.Nil(t, response)
	require.Error(t, err)
	require.Equal(t, NpmCLIVersionNotRecordedCode, NpmCLIVersionErrorCode(err))
	require.Contains(t, err.Error(), "npm version options not recorded")
}

func TestNpmCLIVersionErrorCodeUsesTypedErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "required",
			err:      newNpmCLIVersionCodedError(NpmCLIPackageRequiredCode, "package is required"),
			expected: NpmCLIPackageRequiredCode,
		},
		{
			name:     "unsupported",
			err:      newNpmCLIVersionCodedError(NpmCLIPackageUnsupportedCode, "unsupported npm package: %s", "@scope/unknown"),
			expected: NpmCLIPackageUnsupportedCode,
		},
		{
			name:     "not recorded",
			err:      newNpmCLIVersionCodedError(NpmCLIVersionNotRecordedCode, "npm version options not recorded for package: %s", "@openai/codex"),
			expected: NpmCLIVersionNotRecordedCode,
		},
		{
			name:     "persist failed",
			err:      newNpmCLIVersionCodedError(NpmCLIVersionPersistFailedCode, "persist failed"),
			expected: NpmCLIVersionPersistFailedCode,
		},
		{
			name:     "wrapped",
			err:      fmt.Errorf("wrapped: %w", newNpmCLIVersionCodedError(NpmCLIVersionNotRecordedCode, "npm version options not recorded")),
			expected: NpmCLIVersionNotRecordedCode,
		},
		{
			name:     "generic",
			err:      errors.New("registry unavailable"),
			expected: NpmCLIVersionLoadFailedCode,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, NpmCLIVersionErrorCode(test.err))
		})
	}
}

type timeoutErrorForNpmCLIVersionTest struct{}

func (timeoutErrorForNpmCLIVersionTest) Error() string {
	return "temporary timeout"
}

func (timeoutErrorForNpmCLIVersionTest) Timeout() bool {
	return true
}

func (timeoutErrorForNpmCLIVersionTest) Temporary() bool {
	return true
}

var _ net.Error = timeoutErrorForNpmCLIVersionTest{}

func TestClassifyNpmCLIVersionFetchError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "typed error",
			err:      newNpmCLIVersionCodedError(NpmCLIVersionEmptyCode, "empty options"),
			expected: NpmCLIVersionEmptyCode,
		},
		{
			name:     "context deadline",
			err:      context.DeadlineExceeded,
			expected: NpmCLIVersionTimeoutCode,
		},
		{
			name:     "net timeout",
			err:      timeoutErrorForNpmCLIVersionTest{},
			expected: NpmCLIVersionTimeoutCode,
		},
		{
			name:     "http status",
			err:      errors.New("npm registry returned status 503 for package @openai/codex"),
			expected: NpmCLIVersionHTTPStatusCode,
		},
		{
			name:     "decode",
			err:      errors.New("decode npm registry metadata for package @openai/codex: invalid character '<' looking for beginning of value"),
			expected: NpmCLIVersionDecodeFailedCode,
		},
		{
			name:     "oversize",
			err:      errors.New("npm registry metadata exceeds maximum size for package @openai/codex"),
			expected: NpmCLIVersionDecodeFailedCode,
		},
		{
			name:     "generic",
			err:      errors.New("connection reset by peer"),
			expected: NpmCLIVersionLoadFailedCode,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, classifyNpmCLIVersionFetchError(test.err))
		})
	}
}

func TestSetCachedNpmCLIVersionOptionsNormalizesLegacyLatestOptions(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptions("@openai/codex", []NpmCLIVersionOption{
		{Value: "1.0.0", Label: "1.0.0 (latest)", IsLatest: true},
		{Value: "1.0.0", Label: "duplicate"},
		{Value: "0.9.0", Label: "0.9.0"},
	})

	options, ok := getCachedNpmCLIVersionOptions("@openai/codex")
	require.True(t, ok)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
		{Value: "0.9.0", Label: "0.9.0", IsLatest: false, ResolvedVersion: "0.9.0"},
	}, options)
}

func TestSetCachedNpmCLIVersionOptionsRejectsInvalidLatestOptions(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptions("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest", IsLatest: true},
		{Value: "not-a-version", Label: "not-a-version"},
	})

	_, ok := getCachedNpmCLIVersionOptions("@openai/codex")
	require.False(t, ok)
}

func TestRunNpmCLIVersionRefreshOnceDoesNotCacheErrors(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	callsByPackage := map[string]int{}
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		packageName, err := url.QueryUnescape(strings.TrimPrefix(request.URL.EscapedPath(), "/"))
		require.NoError(t, err)
		callsByPackage[packageName]++
		if packageName == "@openai/codex" && callsByPackage[packageName] == 1 {
			return nil, errors.New("temporary registry error")
		}
		payload, err := common.Marshal(map[string]any{
			"dist-tags": map[string]string{"latest": "1.0.1"},
			"versions": map[string]any{
				"1.0.1": map[string]any{},
			},
		})
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}

	runNpmCLIVersionRefreshOnce()
	_, ok := getCachedNpmCLIVersionOptions("@openai/codex")
	require.False(t, ok)
	runNpmCLIVersionRefreshOnce()
	options, ok := getCachedNpmCLIVersionOptions("@openai/codex")
	require.True(t, ok)

	require.Equal(t, 2, callsByPackage["@openai/codex"])
	require.Equal(t, "latest", options[0].Value)
	require.Equal(t, "1.0.1", options[0].ResolvedVersion)
}

func TestRunNpmCLIVersionRefreshOnceDoesNotMutateCacheWhenPersistFails(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (0.9.0)", IsLatest: true, ResolvedVersion: "0.9.0"},
	}, time.Unix(1000, 0).UTC())

	payload, err := common.Marshal(map[string]any{
		"dist-tags": map[string]string{"latest": "1.0.1"},
		"versions": map[string]any{
			"1.0.1": map[string]any{},
		},
	})
	require.NoError(t, err)
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}

	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	runNpmCLIVersionRefreshOnce()

	entry, ok := getCachedNpmCLIVersionOptionsEntry("@openai/codex")
	require.True(t, ok)
	require.Equal(t, "0.9.0", entry.latestVersion)
	require.Equal(t, time.Unix(1000, 0).UTC(), entry.fetchedAt)
}

func TestFetchNpmCLIVersionOptionsUsesServiceHTTPClientThroughRefreshTask(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	called := false
	payload, err := common.Marshal(map[string]any{
		"dist-tags": map[string]string{"latest": "1.0.0"},
		"versions": map[string]any{
			"1.0.0": map[string]any{},
		},
	})
	require.NoError(t, err)

	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		called = true
		require.Equal(t, "registry.npmjs.org", request.URL.Host)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}

	runNpmCLIVersionRefreshOnce()
	options, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	}, options)
}

func TestRefreshNpmCLIVersionOptionsResponseFetchesAndPersistsRegistryOptions(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	requestedPackages := map[string]int{}
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		packageName, err := url.QueryUnescape(strings.TrimPrefix(request.URL.EscapedPath(), "/"))
		require.NoError(t, err)
		requestedPackages[packageName]++
		payload, err := common.Marshal(map[string]any{
			"dist-tags": map[string]string{"latest": "2.0.0"},
			"versions": map[string]any{
				"2.0.0": map[string]any{},
				"1.9.0": map[string]any{},
			},
		})
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}

	response, err := RefreshNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Equal(t, 1, requestedPackages["@openai/codex"])
	require.Equal(t, "@openai/codex", response.PackageName)
	require.Equal(t, "npm", response.Source)
	require.Equal(t, "2.0.0", response.LatestVersion)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (2.0.0)", IsLatest: true, ResolvedVersion: "2.0.0"},
		{Value: "2.0.0", Label: "2.0.0", IsLatest: false, ResolvedVersion: "2.0.0"},
		{Value: "1.9.0", Label: "1.9.0", IsLatest: false, ResolvedVersion: "1.9.0"},
	}, response.Options)
	require.NotEmpty(t, strings.TrimSpace(common.OptionMap[npmCliVersionRecordedOptionsKey]))

	resetNpmCLIVersionCacheForTest()
	recordedResponse, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Equal(t, "recorded", recordedResponse.Source)
	require.Equal(t, "2.0.0", recordedResponse.LatestVersion)
}

func TestRefreshNpmCLIVersionOptionsResponseDedupeConcurrentSamePackage(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	var registryRequests atomic.Int64
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		registryRequests.Add(1)
		time.Sleep(50 * time.Millisecond)
		payload, err := common.Marshal(map[string]any{
			"dist-tags": map[string]string{"latest": "2.0.0"},
			"versions": map[string]any{
				"2.0.0": map[string]any{},
				"1.9.0": map[string]any{},
			},
		})
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	responses := make([]*NpmCLIVersionOptionsResponse, workers)
	errors := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			responses[index], errors[index] = RefreshNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
		}(i)
	}
	close(start)
	wg.Wait()

	require.Equal(t, int64(1), registryRequests.Load())
	for i := 0; i < workers; i++ {
		require.NoError(t, errors[i])
		require.Equal(t, "2.0.0", responses[i].LatestVersion)
	}
	metrics := snapshotNpmCLIVersionRefreshMetrics()
	require.Equal(t, int64(workers), metrics.ManualRuns)
	require.Equal(t, int64(workers), metrics.ManualSuccesses)
	require.Equal(t, int64(0), metrics.ManualFailures)
}

func TestRefreshNpmCLIVersionOptionsResponsePreservesOtherRecordedPackagesWhenCacheCold(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(1000, 0).UTC(),
				LatestVersion: "1.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
				},
			},
			"@anthropic-ai/claude-code": {
				FetchedAt:     time.Unix(1001, 0).UTC(),
				LatestVersion: "1.1.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.1.0)", IsLatest: true, ResolvedVersion: "1.1.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[npmCliVersionRecordedOptionsKey] = string(payload)
	common.OptionMapRWMutex.Unlock()

	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		packageName, err := url.QueryUnescape(strings.TrimPrefix(request.URL.EscapedPath(), "/"))
		require.NoError(t, err)
		require.Equal(t, "@openai/codex", packageName)
		payload, err := common.Marshal(map[string]any{
			"dist-tags": map[string]string{"latest": "2.0.0"},
			"versions": map[string]any{
				"2.0.0": map[string]any{},
			},
		})
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}

	response, err := RefreshNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", response.LatestVersion)

	resetNpmCLIVersionCacheForTest()
	claudeResponse, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@anthropic-ai/claude-code")
	require.NoError(t, err)
	require.Equal(t, "recorded", claudeResponse.Source)
	require.Equal(t, "1.1.0", claudeResponse.LatestVersion)
}

func TestRefreshNpmCLIVersionOptionsResponseDoesNotMutateCacheWhenPersistFails(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	}, time.Unix(1000, 0).UTC())
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		payload, err := common.Marshal(map[string]any{
			"dist-tags": map[string]string{"latest": "2.0.0"},
			"versions": map[string]any{
				"2.0.0": map[string]any{},
			},
		})
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	response, err := RefreshNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.Nil(t, response)
	require.Error(t, err)
	require.Equal(t, NpmCLIVersionPersistFailedCode, NpmCLIVersionErrorCode(err))

	entry, ok := getCachedNpmCLIVersionOptionsEntry("@openai/codex")
	require.True(t, ok)
	require.Equal(t, "1.0.0", entry.latestVersion)
	require.Equal(t, time.Unix(1000, 0).UTC(), entry.fetchedAt)
}

func TestFetchNpmCLIVersionOptionsUsesRecordedOptionsWithoutRegistryRequest(t *testing.T) {
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptions("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	})
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("registry should not be requested")
	})}

	options, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	}, options)
}

func TestFetchNpmCLIVersionOptionsResponseClonesCachedOptionsForCaller(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptions("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	})

	response, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.NoError(t, err)
	response.Options[0].Label = "mutated"
	response.Options = append(response.Options, NpmCLIVersionOption{Value: "0.9.0", Label: "0.9.0"})

	nextResponse, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	}, nextResponse.Options)
}

func TestFetchNpmCLIVersionOptionsReloadsNewerRecordedOptions(t *testing.T) {
	previousClient := httpClient
	previousOptionMap := common.OptionMap
	defer func() {
		httpClient = previousClient
		common.OptionMap = previousOptionMap
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	}, time.Unix(1000, 0).UTC())
	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(2000, 0).UTC(),
				LatestVersion: "2.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (2.0.0)", IsLatest: true, ResolvedVersion: "2.0.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	common.OptionMap = map[string]string{
		npmCliVersionRecordedOptionsKey: string(payload),
	}
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("registry should not be requested")
	})}

	options, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", options[0].ResolvedVersion)
}

func TestResolveNpmCLILatestVersionReloadsChangedRecordedOptionsOverCachedEntry(t *testing.T) {
	previousOptionMap := common.OptionMap
	defer func() {
		common.OptionMap = previousOptionMap
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	}, time.Unix(1000, 0).UTC())
	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(2000, 0).UTC(),
				LatestVersion: "2.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (2.0.0)", IsLatest: true, ResolvedVersion: "2.0.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	common.OptionMap = map[string]string{
		npmCliVersionRecordedOptionsKey: string(payload),
	}

	version, ok := ResolveNpmCLILatestVersion("@openai/codex")
	require.True(t, ok)
	require.Equal(t, "2.0.0", version)
}

func TestFetchNpmCLIVersionOptionsLoadsRecordedOptionsFromOptionMap(t *testing.T) {
	previousClient := httpClient
	previousOptionMap := common.OptionMap
	defer func() {
		httpClient = previousClient
		common.OptionMap = previousOptionMap
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(1000, 0).UTC(),
				LatestVersion: "1.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	common.OptionMap = map[string]string{
		npmCliVersionRecordedOptionsKey: string(payload),
	}
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("registry should not be requested")
	})}

	options, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	}, options)
}

func TestFetchNpmCLIVersionOptionsLoadsRecordedOptionsFromDatabaseOnCacheMiss(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(3000, 0).UTC(),
				LatestVersion: "3.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (3.0.0)", IsLatest: true, ResolvedVersion: "3.0.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Option{
		Key:   npmCliVersionRecordedOptionsKey,
		Value: string(payload),
	}).Error)
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("registry should not be requested")
	})}

	response, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Equal(t, "recorded", response.Source)
	require.Equal(t, "3.0.0", response.LatestVersion)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (3.0.0)", IsLatest: true, ResolvedVersion: "3.0.0"},
	}, response.Options)
	common.OptionMapRWMutex.RLock()
	optionMapValue := common.OptionMap[npmCliVersionRecordedOptionsKey]
	common.OptionMapRWMutex.RUnlock()
	require.Equal(t, string(payload), optionMapValue)
}

func TestResolveNpmCLILatestVersionLoadsRecordedOptionsFromDatabaseOnCacheMiss(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(4000, 0).UTC(),
				LatestVersion: "4.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (4.0.0)", IsLatest: true, ResolvedVersion: "4.0.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Option{
		Key:   npmCliVersionRecordedOptionsKey,
		Value: string(payload),
	}).Error)
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("registry should not be requested")
	})}

	version, ok := ResolveNpmCLILatestVersion("@openai/codex")
	require.True(t, ok)
	require.Equal(t, "4.0.0", version)
	common.OptionMapRWMutex.RLock()
	optionMapValue := common.OptionMap[npmCliVersionRecordedOptionsKey]
	common.OptionMapRWMutex.RUnlock()
	require.Equal(t, string(payload), optionMapValue)
}

func TestLoadRecordedNpmCLIVersionOptionsRetriesAfterInvalidRaw(t *testing.T) {
	previousOptionMap := common.OptionMap
	defer func() {
		common.OptionMap = previousOptionMap
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	common.OptionMap = map[string]string{
		npmCliVersionRecordedOptionsKey: "{invalid",
	}
	loadRecordedNpmCLIVersionOptions()

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(1000, 0).UTC(),
				LatestVersion: "1.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	common.OptionMap[npmCliVersionRecordedOptionsKey] = string(payload)

	loadRecordedNpmCLIVersionOptions()

	version, ok := ResolveNpmCLILatestVersion("@openai/codex")
	require.True(t, ok)
	require.Equal(t, "1.0.0", version)
}

func TestLoadRecordedNpmCLIVersionOptionsUsesRecordedLatestVersionFallback(t *testing.T) {
	previousOptionMap := common.OptionMap
	defer func() {
		common.OptionMap = previousOptionMap
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(1000, 0).UTC(),
				LatestVersion: "1.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "1.0.0", Label: "1.0.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	common.OptionMap = map[string]string{
		npmCliVersionRecordedOptionsKey: string(payload),
	}

	loadRecordedNpmCLIVersionOptions()
	options, ok := getCachedNpmCLIVersionOptions("@openai/codex")
	require.True(t, ok)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	}, options)
}

func TestGetCachedNpmCLIVersionOptionsKeepsRecordedEntries(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	}, time.Unix(100, 0))

	options, ok := getCachedNpmCLIVersionOptions("@openai/codex")
	require.True(t, ok)
	require.Equal(t, "latest", options[0].Value)
	require.Equal(t, "1.0.0", options[0].ResolvedVersion)
}

func TestResolveNpmCLILatestVersionUsesRecordedCache(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptions("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	})

	version, ok := ResolveNpmCLILatestVersion("@openai/codex")
	require.True(t, ok)
	require.Equal(t, "1.0.0", version)
}

func TestResolveNpmCLILatestVersionRejectsUnsupportedPackage(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptions("@scope/unknown", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	})

	version, ok := ResolveNpmCLILatestVersion("@scope/unknown")
	require.False(t, ok)
	require.Empty(t, version)
}

func TestResolveNpmCLILatestVersionLoadsRecordedOptions(t *testing.T) {
	previousOptionMap := common.OptionMap
	defer func() {
		common.OptionMap = previousOptionMap
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(1000, 0).UTC(),
				LatestVersion: "1.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	common.OptionMap = map[string]string{
		npmCliVersionRecordedOptionsKey: string(payload),
	}

	version, ok := ResolveNpmCLILatestVersion("@openai/codex")
	require.True(t, ok)
	require.Equal(t, "1.0.0", version)
}

func TestLoadRecordedNpmCLIVersionOptionsKeepsNewerCacheEntry(t *testing.T) {
	previousOptionMap := common.OptionMap
	defer func() {
		common.OptionMap = previousOptionMap
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (2.0.0)", IsLatest: true, ResolvedVersion: "2.0.0"},
	}, time.Unix(2000, 0).UTC())

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(1000, 0).UTC(),
				LatestVersion: "1.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	common.OptionMap = map[string]string{
		npmCliVersionRecordedOptionsKey: string(payload),
	}

	loadRecordedNpmCLIVersionOptions()
	version, ok := ResolveNpmCLILatestVersion("@openai/codex")
	require.True(t, ok)
	require.Equal(t, "2.0.0", version)
}

func TestRunNpmCLIVersionRefreshOnceRecordsAllAllowedPackages(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	requestedPackages := map[string]bool{}
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		packageName, err := url.QueryUnescape(strings.TrimPrefix(request.URL.EscapedPath(), "/"))
		require.NoError(t, err)
		requestedPackages[packageName] = true
		payload, err := common.Marshal(map[string]any{
			"dist-tags": map[string]string{"latest": "1.0.0"},
			"versions": map[string]any{
				"1.0.0": map[string]any{},
			},
		})
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}

	runNpmCLIVersionRefreshOnce()

	for _, packageName := range allowedNpmCLIPackageNames() {
		require.True(t, requestedPackages[packageName], packageName)
		options, ok := getCachedNpmCLIVersionOptions(packageName)
		require.True(t, ok, packageName)
		require.Equal(t, "latest", options[0].Value, packageName)
		require.Equal(t, "1.0.0", options[0].ResolvedVersion, packageName)
		version, ok := ResolveNpmCLILatestVersion(packageName)
		require.True(t, ok, packageName)
		require.Equal(t, "1.0.0", version, packageName)
	}
	require.NotEmpty(t, strings.TrimSpace(common.OptionMap[npmCliVersionRecordedOptionsKey]))
}

func TestRunNpmCLIVersionRefreshOncePreservesRecordedPackagesWhenRefreshFailsAndCacheCold(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@anthropic-ai/claude-code": {
				FetchedAt:     time.Unix(1001, 0).UTC(),
				LatestVersion: "1.1.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.1.0)", IsLatest: true, ResolvedVersion: "1.1.0"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[npmCliVersionRecordedOptionsKey] = string(payload)
	common.OptionMapRWMutex.Unlock()

	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		packageName, err := url.QueryUnescape(strings.TrimPrefix(request.URL.EscapedPath(), "/"))
		require.NoError(t, err)
		if packageName == "@anthropic-ai/claude-code" {
			return nil, errors.New("temporary registry error")
		}
		payload, err := common.Marshal(map[string]any{
			"dist-tags": map[string]string{"latest": "2.0.0"},
			"versions": map[string]any{
				"2.0.0": map[string]any{},
			},
		})
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}

	runNpmCLIVersionRefreshOnce()

	diagnostics := GetNpmCLIVersionDiagnostics()
	var claudeDiag NpmCLIVersionDiagnostic
	for _, item := range diagnostics.Packages {
		if item.PackageName == "@anthropic-ai/claude-code" {
			claudeDiag = item
			break
		}
	}
	require.NotNil(t, claudeDiag.LastError)
	require.Equal(t, NpmCLIVersionLoadFailedCode, claudeDiag.LastError.Code)
	require.Equal(t, "npm", claudeDiag.LastError.Source)

	resetNpmCLIVersionCacheForTest()
	claudeResponse, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@anthropic-ai/claude-code")
	require.NoError(t, err)
	require.Equal(t, "recorded", claudeResponse.Source)
	require.Equal(t, "1.1.0", claudeResponse.LatestVersion)
	codexResponse, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", codexResponse.LatestVersion)
}

func TestRunNpmCLIVersionRefreshOncePersistsSuccessfulPackagesIndividually(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		packageName, err := url.QueryUnescape(strings.TrimPrefix(request.URL.EscapedPath(), "/"))
		require.NoError(t, err)
		if packageName == "@anthropic-ai/claude-code" {
			return nil, errors.New("temporary registry error")
		}
		payload, err := common.Marshal(map[string]any{
			"dist-tags": map[string]string{"latest": "2.0.0"},
			"versions": map[string]any{
				"2.0.0": map[string]any{},
			},
		})
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}

	runNpmCLIVersionRefreshOnce()
	resetNpmCLIVersionCacheForTest()

	codexResponse, err := FetchNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", codexResponse.LatestVersion)
	_, err = FetchNpmCLIVersionOptionsResponse(context.Background(), "@anthropic-ai/claude-code")
	require.Error(t, err)
	require.Equal(t, NpmCLIVersionNotRecordedCode, NpmCLIVersionErrorCode(err))
}

func TestGetNpmCLIVersionDiagnosticsReportsCacheAndLastError(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	fetchedAt := time.Now().Add(-time.Minute)
	require.NoError(t, persistRecordedNpmCLIVersionOptionsWithPackage(
		context.Background(),
		"@openai/codex",
		[]NpmCLIVersionOption{
			{Value: "latest", Label: "latest (1.2.3)", IsLatest: true, ResolvedVersion: "1.2.3"},
		},
		fetchedAt,
	))
	recordNpmCLIVersionLastError(context.Background(), "@anthropic-ai/claude-code", "npm", NpmCLIVersionTimeoutCode, context.DeadlineExceeded)

	diagnostics := GetNpmCLIVersionDiagnostics()
	byPackage := map[string]NpmCLIVersionDiagnostic{}
	for _, item := range diagnostics.Packages {
		byPackage[item.PackageName] = item
	}
	codex := byPackage["@openai/codex"]
	require.Equal(t, "recorded", codex.Source)
	require.True(t, codex.Recorded)
	require.NotNil(t, codex.RefreshedAt)
	require.False(t, codex.RefreshedAt.IsZero())
	require.Equal(t, "1.2.3", codex.LatestVersion)
	require.Equal(t, 1, codex.OptionCount)
	require.GreaterOrEqual(t, codex.CacheAgeMs, int64(0))
	require.Nil(t, codex.LastError)
	claude := byPackage["@anthropic-ai/claude-code"]
	require.Equal(t, "missing", claude.Source)
	require.False(t, claude.Recorded)
	require.Nil(t, claude.RefreshedAt)
	require.NotNil(t, claude.LastError)
	require.Equal(t, "process", claude.LastErrorScope)
	require.Equal(t, NpmCLIVersionTimeoutCode, claude.LastError.Code)
	require.Equal(t, npmCLIVersionActionCheckNpmRegistryConnectivity, claude.RecommendedAction)
	require.Len(t, claude.RecentErrors, 1)
	require.Equal(t, NpmCLIVersionTimeoutCode, claude.RecentErrors[0].Code)
	require.Equal(t, npmCLIVersionActionNone, codex.RecommendedAction)

	payload, err := common.Marshal(diagnostics)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "0001-01-01T00:00:00Z")
	require.False(t, diagnostics.GeneratedAt.IsZero())
	require.Equal(t, npmCliVersionRefreshInterval.Milliseconds(), diagnostics.RefreshIntervalMs)
	require.Equal(t, npmRegistryRequestTimeout.Milliseconds(), diagnostics.RegistryTimeoutMs)
	require.Equal(t, len(allowedNpmCLIPackages), diagnostics.Summary.PackageCount)
	require.Equal(t, 1, diagnostics.Summary.RecordedCount)
	require.Equal(t, len(allowedNpmCLIPackages)-1, diagnostics.Summary.MissingCount)
	require.Equal(t, 1, diagnostics.Summary.LastErrorCount)
	require.Equal(t, 1, diagnostics.Summary.ProcessLastErrorCount)
	require.Equal(t, 0, diagnostics.Summary.RecordedLastErrorCount)
	require.NotNil(t, diagnostics.Summary.NewestRefreshedAt)
	require.NotNil(t, diagnostics.Summary.OldestRefreshedAt)
	require.GreaterOrEqual(t, diagnostics.Summary.MaxCacheAgeMs, int64(0))
}

func TestNpmCLIVersionRecentErrorsAreBoundedAndNewestFirst(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	for i := 0; i < npmCliVersionRecentErrorLimit+2; i++ {
		code := NpmCLIVersionTimeoutCode
		if i%2 == 1 {
			code = NpmCLIVersionHTTPStatusCode
		}
		recordNpmCLIVersionLastError(context.Background(), "@openai/codex", "npm", code, context.DeadlineExceeded)
		time.Sleep(time.Millisecond)
	}

	diagnostics := GetNpmCLIVersionDiagnostics()
	byPackage := map[string]NpmCLIVersionDiagnostic{}
	for _, item := range diagnostics.Packages {
		byPackage[item.PackageName] = item
	}
	recentErrors := byPackage["@openai/codex"].RecentErrors
	require.Len(t, recentErrors, npmCliVersionRecentErrorLimit)
	for i := 1; i < len(recentErrors); i++ {
		require.False(t, recentErrors[i-1].UpdatedAt.Before(recentErrors[i].UpdatedAt))
	}
}

func TestNpmCLIVersionCacheAgeMillisecondsClampsFutureFetchedAt(t *testing.T) {
	now := time.Unix(1000, 0).UTC()

	require.Equal(t, int64(0), npmCLIVersionCacheAgeMilliseconds(now, now.Add(time.Second)))
	require.Equal(t, int64(0), npmCLIVersionCacheAgeMilliseconds(now, time.Time{}))
	require.Equal(t, int64(0), npmCLIVersionCacheAgeMilliseconds(time.Time{}, now))
	require.Equal(t, int64(1500), npmCLIVersionCacheAgeMilliseconds(now, now.Add(-1500*time.Millisecond)))
}

func TestNpmCLIVersionRefreshMetricsTrackScheduledAndManualAttempts(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	startedAt := time.Now().Add(-20 * time.Millisecond)
	recordNpmCLIVersionScheduledRefresh(startedAt, 4, 1)
	_, err := RefreshNpmCLIVersionOptionsResponse(context.Background(), "@scope/unknown")
	require.Error(t, err)

	diagnostics := GetNpmCLIVersionDiagnostics()
	require.Equal(t, int64(1), diagnostics.Metrics.ScheduledRuns)
	require.Equal(t, int64(4), diagnostics.Metrics.ScheduledSuccessfulPackages)
	require.Equal(t, int64(1), diagnostics.Metrics.ScheduledFailedPackages)
	require.Equal(t, 4, diagnostics.Metrics.LastRunRefreshed)
	require.Equal(t, 1, diagnostics.Metrics.LastRunFailed)
	require.NotNil(t, diagnostics.Metrics.LastRunAt)
	require.Equal(t, int64(1), diagnostics.Metrics.ManualRuns)
	require.Equal(t, int64(0), diagnostics.Metrics.ManualSuccesses)
	require.Equal(t, int64(1), diagnostics.Metrics.ManualFailures)
	require.Equal(t, "@scope/unknown", diagnostics.Metrics.LastManualPackage)
	require.Equal(t, NpmCLIPackageUnsupportedCode, diagnostics.Metrics.LastManualCode)
	require.NotNil(t, diagnostics.Metrics.LastManualRefreshAt)
	require.GreaterOrEqual(t, diagnostics.Metrics.LastManualDurationMs, int64(0))
}

func TestGetNpmCLIVersionDiagnosticsReportsRecordedLastError(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	updatedAt := time.Unix(3000, 0).UTC()
	olderUpdatedAt := updatedAt.Add(-time.Minute)
	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{},
		LastErrors: map[string]NpmCLIVersionLastError{
			"@anthropic-ai/claude-code": {
				Code:      NpmCLIVersionTimeoutCode,
				Message:   "dial tcp 10.0.0.5:443: registry timeout",
				Source:    "npm",
				UpdatedAt: updatedAt,
			},
		},
		RecentErrors: map[string][]NpmCLIVersionLastError{
			"@anthropic-ai/claude-code": {
				{
					Code:      NpmCLIVersionHTTPStatusCode,
					Message:   "registry status 503",
					Source:    "npm",
					UpdatedAt: olderUpdatedAt,
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(npmCliVersionRecordedOptionsKey, string(payload)))

	diagnostics := GetNpmCLIVersionDiagnostics()
	byPackage := map[string]NpmCLIVersionDiagnostic{}
	for _, item := range diagnostics.Packages {
		byPackage[item.PackageName] = item
	}
	claude := byPackage["@anthropic-ai/claude-code"]
	require.NotNil(t, claude.LastError)
	require.Equal(t, "recorded", claude.LastErrorScope)
	require.Equal(t, NpmCLIVersionTimeoutCode, claude.LastError.Code)
	require.Equal(t, "npm", claude.LastError.Source)
	require.Equal(t, updatedAt, claude.LastError.UpdatedAt)
	require.Equal(t, "npm registry request timed out", claude.LastError.Message)
	require.NotContains(t, claude.LastError.Message, "10.0.0.5")
	require.Equal(t, npmCLIVersionActionCheckNpmRegistryConnectivity, claude.RecommendedAction)
	require.Len(t, claude.RecentErrors, 2)
	require.Equal(t, updatedAt, claude.RecentErrors[0].UpdatedAt)
	require.Equal(t, olderUpdatedAt, claude.RecentErrors[1].UpdatedAt)
	require.Equal(t, "npm registry returned an unsuccessful HTTP status", claude.RecentErrors[1].Message)
}

func TestGetNpmCLIVersionDiagnosticsSuppressesProcessErrorCoveredByRefresh(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	updatedAt := time.Now().Add(-time.Minute)
	npmCliVersionLastErrors.Lock()
	npmCliVersionLastErrors.items["@openai/codex"] = NpmCLIVersionLastError{
		Code:      NpmCLIVersionTimeoutCode,
		Message:   "registry timeout",
		Source:    "npm",
		UpdatedAt: updatedAt,
	}
	npmCliVersionLastErrors.Unlock()
	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
	}, updatedAt.Add(time.Second))

	diagnostics := GetNpmCLIVersionDiagnostics()
	byPackage := map[string]NpmCLIVersionDiagnostic{}
	for _, item := range diagnostics.Packages {
		byPackage[item.PackageName] = item
	}
	codex := byPackage["@openai/codex"]

	require.True(t, codex.Recorded)
	require.Nil(t, codex.LastError)
	require.Empty(t, codex.LastErrorScope)
}

func TestPersistRecordedNpmCLIVersionOptionsWithPackageClearsRecordedLastError(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	recordNpmCLIVersionLastError(context.Background(), "@openai/codex", "npm", NpmCLIVersionTimeoutCode, context.DeadlineExceeded)
	var option model.Option
	require.NoError(t, model.DB.First(&option, "key = ?", npmCliVersionRecordedOptionsKey).Error)
	var recorded recordedNpmCLIVersionOptions
	require.NoError(t, common.Unmarshal([]byte(option.Value), &recorded))
	require.Contains(t, recorded.LastErrors, "@openai/codex")
	require.Contains(t, recorded.RecentErrors, "@openai/codex")
	lastErrorUpdatedAt := recorded.LastErrors["@openai/codex"].UpdatedAt

	err := persistRecordedNpmCLIVersionOptionsWithPackage(
		context.Background(),
		"@openai/codex",
		[]NpmCLIVersionOption{
			{Value: "latest", Label: "latest (1.2.3)", IsLatest: true, ResolvedVersion: "1.2.3"},
		},
		lastErrorUpdatedAt.Add(time.Second),
	)
	require.NoError(t, err)

	require.NoError(t, model.DB.First(&option, "key = ?", npmCliVersionRecordedOptionsKey).Error)
	recorded = recordedNpmCLIVersionOptions{}
	require.NoError(t, common.Unmarshal([]byte(option.Value), &recorded))
	require.NotContains(t, recorded.LastErrors, "@openai/codex")
	require.Contains(t, recorded.RecentErrors, "@openai/codex")
	require.Len(t, recorded.RecentErrors["@openai/codex"], 1)
	require.Equal(t, "1.2.3", recorded.Packages["@openai/codex"].LatestVersion)
}

func TestClearNpmCLIVersionLastErrorOnlyClearsProcessState(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	recordNpmCLIVersionLastError(context.Background(), "@openai/codex", "npm", NpmCLIVersionTimeoutCode, context.DeadlineExceeded)
	require.NotNil(t, getNpmCLIVersionLastError("@openai/codex"))

	clearNpmCLIVersionLastError("@openai/codex")
	require.Nil(t, getNpmCLIVersionLastError("@openai/codex"))

	var option model.Option
	require.NoError(t, model.DB.First(&option, "key = ?", npmCliVersionRecordedOptionsKey).Error)
	var recorded recordedNpmCLIVersionOptions
	require.NoError(t, common.Unmarshal([]byte(option.Value), &recorded))
	require.Contains(t, recorded.LastErrors, "@openai/codex")
}

func TestPersistRecordedNpmCLIVersionOptionsWithPackagePreservesNewerRecordedLastError(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	recorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{},
		LastErrors: map[string]NpmCLIVersionLastError{
			"@openai/codex": {
				Code:      NpmCLIVersionTimeoutCode,
				Message:   "newer registry timeout",
				Source:    "npm",
				UpdatedAt: time.Unix(5000, 0).UTC(),
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(npmCliVersionRecordedOptionsKey, string(payload)))

	err = persistRecordedNpmCLIVersionOptionsWithPackage(
		context.Background(),
		"@openai/codex",
		[]NpmCLIVersionOption{
			{Value: "latest", Label: "latest (1.2.3)", IsLatest: true, ResolvedVersion: "1.2.3"},
		},
		time.Unix(4000, 0).UTC(),
	)
	require.NoError(t, err)

	var option model.Option
	require.NoError(t, model.DB.First(&option, "key = ?", npmCliVersionRecordedOptionsKey).Error)
	recorded = recordedNpmCLIVersionOptions{}
	require.NoError(t, common.Unmarshal([]byte(option.Value), &recorded))
	require.Contains(t, recorded.LastErrors, "@openai/codex")
	require.Equal(t, time.Unix(5000, 0).UTC(), recorded.LastErrors["@openai/codex"].UpdatedAt)
	require.Equal(t, "1.2.3", recorded.Packages["@openai/codex"].LatestVersion)
}

func TestPersistRecordedNpmCLIVersionOptionsMergesLatestDatabaseValueWhenOptionMapIsStale(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	dbRecorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@anthropic-ai/claude-code": {
				FetchedAt:     time.Unix(2000, 0).UTC(),
				LatestVersion: "2.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (2.0.0)", IsLatest: true, ResolvedVersion: "2.0.0"},
				},
			},
		},
	}
	dbPayload, err := common.Marshal(dbRecorded)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(npmCliVersionRecordedOptionsKey, string(dbPayload)))

	staleRecorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{},
	}
	stalePayload, err := common.Marshal(staleRecorded)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[npmCliVersionRecordedOptionsKey] = string(stalePayload)
	common.OptionMapRWMutex.Unlock()

	err = persistRecordedNpmCLIVersionOptionsWithPackage(
		context.Background(),
		"@openai/codex",
		[]NpmCLIVersionOption{
			{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		},
		time.Unix(3000, 0).UTC(),
	)
	require.NoError(t, err)

	var option model.Option
	require.NoError(t, model.DB.First(&option, "key = ?", npmCliVersionRecordedOptionsKey).Error)
	var recorded recordedNpmCLIVersionOptions
	require.NoError(t, common.Unmarshal([]byte(option.Value), &recorded))
	require.Contains(t, recorded.Packages, "@anthropic-ai/claude-code")
	require.Contains(t, recorded.Packages, "@openai/codex")
	require.Equal(t, "2.0.0", recorded.Packages["@anthropic-ai/claude-code"].LatestVersion)
	require.Equal(t, "1.0.0", recorded.Packages["@openai/codex"].LatestVersion)
	common.OptionMapRWMutex.RLock()
	optionMapValue := common.OptionMap[npmCliVersionRecordedOptionsKey]
	common.OptionMapRWMutex.RUnlock()
	require.JSONEq(t, option.Value, optionMapValue)
}

func TestPersistRecordedNpmCLIVersionOptionsWithRecordsDerivesLatestFromOptions(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	err := persistRecordedNpmCLIVersionOptionsWithRecords(map[string]recordedNpmCLIPackageOptions{
		"@openai/codex": {
			FetchedAt:     time.Unix(3000, 0).UTC(),
			LatestVersion: "9.9.9",
			Options: []NpmCLIVersionOption{
				{Value: "2.0.0", Label: "2.0.0", IsLatest: true},
				{Value: "1.9.0", Label: "1.9.0"},
			},
		},
	})
	require.NoError(t, err)

	var option model.Option
	require.NoError(t, model.DB.First(&option, "key = ?", npmCliVersionRecordedOptionsKey).Error)
	var recorded recordedNpmCLIVersionOptions
	require.NoError(t, common.Unmarshal([]byte(option.Value), &recorded))
	require.Equal(t, "2.0.0", recorded.Packages["@openai/codex"].LatestVersion)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (2.0.0)", IsLatest: true, ResolvedVersion: "2.0.0"},
		{Value: "2.0.0", Label: "2.0.0", IsLatest: false, ResolvedVersion: "2.0.0"},
		{Value: "1.9.0", Label: "1.9.0", IsLatest: false, ResolvedVersion: "1.9.0"},
	}, recorded.Packages["@openai/codex"].Options)
}

func TestPersistRecordedNpmCLIVersionOptionsWithPackageHonorsCanceledContext(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := persistRecordedNpmCLIVersionOptionsWithPackage(
		ctx,
		"@openai/codex",
		[]NpmCLIVersionOption{
			{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		},
		time.Unix(3000, 0).UTC(),
	)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func captureNpmCLIVersionRecordedRetrySleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	previousSleep := sleepBeforeNpmCLIVersionRecordedRetry
	previousJitter := npmCliVersionRecordedRetryJitter
	sleeps := []time.Duration{}
	sleepBeforeNpmCLIVersionRecordedRetry = func(delay time.Duration) {
		sleeps = append(sleeps, delay)
	}
	npmCliVersionRecordedRetryJitter = func() time.Duration {
		return 0
	}
	t.Cleanup(func() {
		sleepBeforeNpmCLIVersionRecordedRetry = previousSleep
		npmCliVersionRecordedRetryJitter = previousJitter
	})
	return &sleeps
}

func TestWaitBeforeNpmCLIVersionRecordedRetryUsesBoundedBackoffWithJitter(t *testing.T) {
	sleeps := captureNpmCLIVersionRecordedRetrySleeps(t)
	npmCliVersionRecordedRetryJitter = func() time.Duration {
		return 2 * time.Millisecond
	}

	for attempt := 0; attempt < npmCliVersionRecordedPersistMaxAttempts; attempt++ {
		waitBeforeNpmCLIVersionRecordedRetry(attempt)
	}

	require.Equal(t, []time.Duration{
		7 * time.Millisecond,
		12 * time.Millisecond,
		17 * time.Millisecond,
		22 * time.Millisecond,
	}, *sleeps)
}

func TestPersistRecordedNpmCLIVersionOptionsPreservesCreateErrorCause(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()
	sleeps := captureNpmCLIVersionRecordedRetrySleeps(t)

	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("npm_cli_version_create_error", func(tx *gorm.DB) {
		tx.AddError(errors.New("forced create failure"))
	}))

	err := persistRecordedNpmCLIVersionOptionsWithPackage(
		context.Background(),
		"@openai/codex",
		[]NpmCLIVersionOption{
			{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		},
		time.Unix(3000, 0).UTC(),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "update recorded options conflicted after")
	require.Contains(t, err.Error(), "create recorded options")
	require.Contains(t, err.Error(), "forced create failure")
	require.Len(t, *sleeps, npmCliVersionRecordedPersistMaxAttempts-1)
}

func TestPersistRecordedNpmCLIVersionOptionsReturnsConflictAfterCASRetryExhaustion(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()
	sleeps := captureNpmCLIVersionRecordedRetrySleeps(t)

	initialRecorded := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(1000, 0).UTC(),
				LatestVersion: "1.0.0",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
				},
			},
		},
	}
	initialPayload, err := common.Marshal(initialRecorded)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(npmCliVersionRecordedOptionsKey, string(initialPayload)))

	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("npm_cli_version_update_conflict", func(tx *gorm.DB) {
		tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "1 = 0"}}})
	}))

	err = persistRecordedNpmCLIVersionOptionsWithPackage(
		context.Background(),
		"@openai/codex",
		[]NpmCLIVersionOption{
			{Value: "latest", Label: "latest (2.0.0)", IsLatest: true, ResolvedVersion: "2.0.0"},
		},
		time.Unix(3000, 0).UTC(),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "update recorded options conflicted after")
	require.Contains(t, err.Error(), "recorded options changed during update")
	require.Len(t, *sleeps, npmCliVersionRecordedPersistMaxAttempts-1)

	var option model.Option
	require.NoError(t, model.DB.First(&option, "key = ?", npmCliVersionRecordedOptionsKey).Error)
	var recorded recordedNpmCLIVersionOptions
	require.NoError(t, common.Unmarshal([]byte(option.Value), &recorded))
	require.Equal(t, "1.0.0", recorded.Packages["@openai/codex"].LatestVersion)
}

func TestRefreshNpmCLIVersionOptionsResponseReturnsClassifiedRegistryError(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
	}()

	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader("registry unavailable")),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}

	_, err := RefreshNpmCLIVersionOptionsResponse(context.Background(), "@openai/codex")
	require.Error(t, err)
	require.Equal(t, NpmCLIVersionHTTPStatusCode, NpmCLIVersionErrorCode(err))

	lastError := getNpmCLIVersionLastError("@openai/codex")
	require.NotNil(t, lastError)
	require.Equal(t, NpmCLIVersionHTTPStatusCode, lastError.Code)
	require.Equal(t, "npm", lastError.Source)
	require.Equal(t, "npm registry returned an unsuccessful HTTP status", lastError.Message)
	require.NotContains(t, lastError.Message, "registry unavailable")
}

func TestNpmCLIVersionPublicErrorMessageHidesInternalFailures(t *testing.T) {
	err := newNpmCLIVersionCodedError(
		NpmCLIVersionPersistFailedCode,
		"persist refreshed npm version options for @openai/codex: dial tcp 10.0.0.5:5432: connection refused",
	)

	require.Equal(t, "npm version options could not be persisted", NpmCLIVersionPublicErrorMessage(err))
	require.NotContains(t, NpmCLIVersionPublicErrorMessage(err), "10.0.0.5")
	require.Equal(t,
		"unsupported npm package: @scope/unknown",
		NpmCLIVersionPublicErrorMessage(newNpmCLIVersionCodedError(
			NpmCLIPackageUnsupportedCode,
			"unsupported npm package: %s",
			"@scope/unknown",
		)),
	)
}

func TestRecordedNpmCLIVersionCASValuePredicateUsesBinaryCompareForMySQL(t *testing.T) {
	previousUsingMySQL := common.UsingMySQL
	defer func() {
		common.UsingMySQL = previousUsingMySQL
	}()

	common.UsingMySQL = false
	sql, args := recordedNpmCLIVersionCASValuePredicate("payload")
	require.Equal(t, "value = ?", sql)
	require.Equal(t, []any{"payload"}, args)

	common.UsingMySQL = true
	sql, args = recordedNpmCLIVersionCASValuePredicate("payload")
	require.Equal(t, "BINARY value = BINARY ?", sql)
	require.Equal(t, []any{"payload"}, args)
}

func TestMergeRecordedNpmCLIVersionOptionsSkipsLastErrorOlderThanPackageRecord(t *testing.T) {
	base := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(4000, 0).UTC(),
				LatestVersion: "1.2.3",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.2.3)", IsLatest: true, ResolvedVersion: "1.2.3"},
				},
			},
		},
		LastErrors: map[string]NpmCLIVersionLastError{},
	}
	overlay := recordedNpmCLIVersionOptions{
		LastErrors: map[string]NpmCLIVersionLastError{
			"@openai/codex": {
				Code:      NpmCLIVersionTimeoutCode,
				Message:   "old registry timeout",
				Source:    "npm",
				UpdatedAt: time.Unix(3000, 0).UTC(),
			},
		},
	}

	merged := mergeRecordedNpmCLIVersionOptions(base, overlay)

	require.NotContains(t, merged.LastErrors, "@openai/codex")
	require.Equal(t, "1.2.3", merged.Packages["@openai/codex"].LatestVersion)
}

func TestMergeRecordedNpmCLIVersionOptionsPreservesLastErrorNewerThanPackageRecord(t *testing.T) {
	base := recordedNpmCLIVersionOptions{
		LastErrors: map[string]NpmCLIVersionLastError{
			"@openai/codex": {
				Code:      NpmCLIVersionTimeoutCode,
				Message:   "newer registry timeout",
				Source:    "npm",
				UpdatedAt: time.Unix(5000, 0).UTC(),
			},
		},
	}
	overlay := recordedNpmCLIVersionOptions{
		Packages: map[string]recordedNpmCLIPackageOptions{
			"@openai/codex": {
				FetchedAt:     time.Unix(4000, 0).UTC(),
				LatestVersion: "1.2.3",
				Options: []NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.2.3)", IsLatest: true, ResolvedVersion: "1.2.3"},
				},
			},
		},
	}

	merged := mergeRecordedNpmCLIVersionOptions(base, overlay)

	require.Contains(t, merged.LastErrors, "@openai/codex")
	require.Equal(t, time.Unix(5000, 0).UTC(), merged.LastErrors["@openai/codex"].UpdatedAt)
	require.Equal(t, "1.2.3", merged.Packages["@openai/codex"].LatestVersion)
}

func TestPersistRecordedNpmCLIVersionOptionsWithRecordsRejectsEmptyNormalizedRecords(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	err := persistRecordedNpmCLIVersionOptionsWithRecords(map[string]recordedNpmCLIPackageOptions{
		"@openai/codex": {
			FetchedAt: time.Now(),
			Options: []NpmCLIVersionOption{
				{Value: "bad\nversion", Label: "bad\nversion"},
			},
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "no valid npm version records to persist")

	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where("key = ?", npmCliVersionRecordedOptionsKey).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestRecordedNpmCLIVersionOptionsRoundTripThroughOptionMap(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	fetchedAt := time.Unix(1000, 0).UTC()
	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	}, fetchedAt)
	require.NoError(t, persistRecordedNpmCLIVersionOptions())

	resetNpmCLIVersionCacheForTest()
	loadRecordedNpmCLIVersionOptions()

	options, ok := getCachedNpmCLIVersionOptions("@openai/codex")
	require.True(t, ok)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	}, options)
	version, ok := ResolveNpmCLILatestVersion("@openai/codex")
	require.True(t, ok)
	require.Equal(t, "1.0.0", version)
}

func TestFetchNpmCLIVersionOptionsHandlesRegistryStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("registry unavailable"))
	}))
	defer server.Close()

	_, err := fetchNpmCLIVersionOptions(context.Background(), "@openai/codex", server.Client(), server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "status 503")
}

func TestFetchNpmCLIVersionOptionsHandlesRegistryJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{invalid"))
	}))
	defer server.Close()

	_, err := fetchNpmCLIVersionOptions(context.Background(), "@openai/codex", server.Client(), server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode npm registry metadata")
}

func TestFetchNpmCLIVersionOptionsBuildsRegistryRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/%40openai%2Fcodex", r.URL.EscapedPath())
		require.Equal(t, "application/vnd.npm.install-v1+json", r.Header.Get("Accept"))

		payload, err := common.Marshal(map[string]any{
			"dist-tags": map[string]string{"latest": "0.2.0"},
			"versions": map[string]any{
				"0.1.0": map[string]any{},
				"0.2.0": map[string]any{},
			},
		})
		require.NoError(t, err)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	options, err := fetchNpmCLIVersionOptions(context.Background(), "@openai/codex", server.Client(), server.URL)
	require.NoError(t, err)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (0.2.0)", IsLatest: true, ResolvedVersion: "0.2.0"},
		{Value: "0.2.0", Label: "0.2.0", IsLatest: false, ResolvedVersion: "0.2.0"},
		{Value: "0.1.0", Label: "0.1.0", IsLatest: false, ResolvedVersion: "0.1.0"},
	}, options)
}

func TestFetchNpmCLIVersionOptionsRejectsOversizedRegistryMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(bytes.Repeat([]byte(" "), npmRegistryMetadataMaxBytes+1))
	}))
	defer server.Close()

	_, err := fetchNpmCLIVersionOptions(context.Background(), "@openai/codex", server.Client(), server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "metadata exceeds maximum size")
}

func TestFetchNpmCLIVersionOptionsPropagatesRequestError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})}

	_, err := fetchNpmCLIVersionOptions(context.Background(), "@openai/codex", client, "https://registry.npmjs.org")
	require.Error(t, err)
	require.Contains(t, err.Error(), "network unavailable")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func resetNpmCLIVersionCacheForTest() {
	npmCliVersionCache.Lock()
	npmCliVersionCache.items = make(map[string]npmCliVersionCacheEntry)
	npmCliVersionCache.Unlock()
	npmCliVersionRecordedState.Lock()
	npmCliVersionRecordedState.raw = ""
	npmCliVersionRecordedState.Unlock()
	npmCliVersionLastErrors.Lock()
	npmCliVersionLastErrors.items = make(map[string]NpmCLIVersionLastError)
	npmCliVersionLastErrors.Unlock()
	npmCliVersionRecentErrors.Lock()
	npmCliVersionRecentErrors.items = make(map[string][]NpmCLIVersionLastError)
	npmCliVersionRecentErrors.Unlock()
	npmCLIVersionManualRefreshGroup = singleflight.Group{}
	resetNpmCLIVersionRefreshMetricsForTest()
}

func setupNpmCLIVersionOptionTestDB(t *testing.T) {
	t.Helper()

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousOptionMap := common.OptionMap

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.OptionMap = map[string]string{}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Option{}))

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.OptionMap = previousOptionMap
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}
