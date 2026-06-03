package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

func TestRecordedNpmCLIVersionOptionsRoundTripThroughOptionMap(t *testing.T) {
	setupNpmCLIVersionOptionTestDB(t)
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	fetchedAt := time.Unix(1000, 0).UTC()
	setCachedNpmCLIVersionOptionsWithFetchedAt("@openai/codex", []NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.0.0)", IsLatest: true, ResolvedVersion: "1.0.0"},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false, ResolvedVersion: "1.0.0"},
	}, fetchedAt)
	persistRecordedNpmCLIVersionOptions()

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
