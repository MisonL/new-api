package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
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
		{Value: "1.2.3", Label: "1.2.3 (latest)", IsLatest: true},
		{Value: "10.0.0", Label: "10.0.0", IsLatest: false},
		{Value: "2.0.0", Label: "2.0.0", IsLatest: false},
		{Value: "1.10.0", Label: "1.10.0", IsLatest: false},
		{Value: "0.9.0", Label: "0.9.0", IsLatest: false},
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
		{Value: "3.0.0-beta.1", Label: "3.0.0-beta.1 (latest)", IsLatest: true},
		{Value: "2.0.0", Label: "2.0.0", IsLatest: false},
		{Value: "1.0.0", Label: "1.0.0", IsLatest: false},
	}, options)
}

func TestFetchNpmCLIVersionOptionsRejectsUnsupportedPackage(t *testing.T) {
	_, err := fetchNpmCLIVersionOptions(context.Background(), "@scope/unknown", http.DefaultClient, "https://registry.npmjs.org")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported npm package")
}

func TestFetchNpmCLIVersionOptionsUsesServiceHTTPClient(t *testing.T) {
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

	options, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, []NpmCLIVersionOption{
		{Value: "1.0.0", Label: "1.0.0 (latest)", IsLatest: true},
	}, options)
}

func TestFetchNpmCLIVersionOptionsCachesSuccessfulResults(t *testing.T) {
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	calls := 0
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
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

	firstOptions, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.NoError(t, err)
	firstOptions[0].Value = "mutated"
	secondOptions, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.NoError(t, err)

	require.Equal(t, 1, calls)
	require.Equal(t, "1.0.0", secondOptions[0].Value)
}

func TestFetchNpmCLIVersionOptionsDoesNotCacheErrors(t *testing.T) {
	previousClient := httpClient
	defer func() {
		httpClient = previousClient
		resetNpmCLIVersionCacheForTest()
	}()
	resetNpmCLIVersionCacheForTest()

	calls := 0
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
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

	_, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.Error(t, err)
	options, err := FetchNpmCLIVersionOptions(context.Background(), "@openai/codex")
	require.NoError(t, err)

	require.Equal(t, 2, calls)
	require.Equal(t, "1.0.1", options[0].Value)
}

func TestGetCachedNpmCLIVersionOptionsExpiresEntries(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	defer resetNpmCLIVersionCacheForTest()

	now := time.Unix(100, 0)
	setCachedNpmCLIVersionOptions("@openai/codex", []NpmCLIVersionOption{
		{Value: "1.0.0", Label: "1.0.0 (latest)", IsLatest: true},
	}, now.Add(time.Second))

	options, ok := getCachedNpmCLIVersionOptions("@openai/codex", now)
	require.True(t, ok)
	require.Equal(t, "1.0.0", options[0].Value)

	_, ok = getCachedNpmCLIVersionOptions("@openai/codex", now.Add(2*time.Second))
	require.False(t, ok)
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
		{Value: "0.2.0", Label: "0.2.0 (latest)", IsLatest: true},
		{Value: "0.1.0", Label: "0.1.0", IsLatest: false},
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
}
