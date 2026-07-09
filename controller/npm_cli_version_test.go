package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type npmVersionOptionsAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Code    string `json:"code"`
	Data    struct {
		PackageName   string                        `json:"package"`
		Source        string                        `json:"source"`
		Code          string                        `json:"code"`
		RefreshedAt   *time.Time                    `json:"refreshed_at"`
		LatestVersion string                        `json:"latest_version"`
		Options       []service.NpmCLIVersionOption `json:"options"`
	} `json:"data"`
}

type npmVersionDiagnosticsAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		GeneratedAt       time.Time                               `json:"generated_at"`
		RefreshIntervalMs int64                                   `json:"refresh_interval_ms"`
		RegistryTimeoutMs int64                                   `json:"registry_timeout_ms"`
		Summary           service.NpmCLIVersionDiagnosticsSummary `json:"summary"`
		Metrics           service.NpmCLIVersionRefreshMetrics     `json:"metrics"`
		Packages          []service.NpmCLIVersionDiagnostic       `json:"packages"`
	} `json:"data"`
}

func TestGetChannelNpmVersionOptionsReturnsRecordedObjectContract(t *testing.T) {
	setNpmVersionRecordedOptionsForControllerTest(t, map[string]any{
		"packages": map[string]any{
			"@openai/codex": map[string]any{
				"fetched_at":     time.Unix(1000, 0).UTC(),
				"latest_version": "1.2.3",
				"options": []service.NpmCLIVersionOption{
					{
						Value:           "latest",
						Label:           "latest (1.2.3)",
						IsLatest:        true,
						ResolvedVersion: "1.2.3",
					},
				},
			},
		},
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/npm_version_options?package=@openai/codex", nil)

	GetChannelNpmVersionOptions(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "0001-01-01T00:00:00Z")
	body := decodeNpmVersionOptionsAPIResponse(t, recorder)
	require.True(t, body.Success)
	require.Empty(t, body.Code)
	require.Equal(t, "@openai/codex", body.Data.PackageName)
	require.Equal(t, "recorded", body.Data.Source)
	require.NotNil(t, body.Data.RefreshedAt)
	require.Equal(t, time.Unix(1000, 0).UTC(), *body.Data.RefreshedAt)
	require.Equal(t, "1.2.3", body.Data.LatestVersion)
	require.Equal(t, []service.NpmCLIVersionOption{
		{Value: "latest", Label: "latest (1.2.3)", IsLatest: true, ResolvedVersion: "1.2.3"},
	}, body.Data.Options)
}

func TestGetChannelNpmVersionOptionsOmitsZeroRefreshedAt(t *testing.T) {
	setNpmVersionRecordedOptionsForControllerTest(t, map[string]any{
		"packages": map[string]any{
			"@anthropic-ai/claude-code": map[string]any{
				"fetched_at":     time.Time{},
				"latest_version": "1.2.3",
				"options": []service.NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.2.3)", IsLatest: true, ResolvedVersion: "1.2.3"},
				},
			},
		},
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/npm_version_options?package=@anthropic-ai/claude-code", nil)

	GetChannelNpmVersionOptions(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "refreshed_at")
	require.NotContains(t, recorder.Body.String(), "0001-01-01T00:00:00Z")
	body := decodeNpmVersionOptionsAPIResponse(t, recorder)
	require.True(t, body.Success)
	require.Nil(t, body.Data.RefreshedAt)
}

func TestGetChannelNpmVersionOptionsReturnsStableFailureCodeContract(t *testing.T) {
	setNpmVersionRecordedOptionsForControllerTest(t, nil)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/npm_version_options?package=@scope/unknown", nil)

	GetChannelNpmVersionOptions(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := decodeNpmVersionOptionsAPIResponse(t, recorder)
	require.False(t, body.Success)
	require.Equal(t, service.NpmCLIPackageUnsupportedCode, body.Code)
	require.Equal(t, service.NpmCLIPackageUnsupportedCode, body.Data.Code)
	require.Contains(t, body.Message, "unsupported npm package")
	require.Equal(t, "@scope/unknown", body.Data.PackageName)
	require.Equal(t, "unavailable", body.Data.Source)
}

func TestRefreshChannelNpmVersionOptionsReturnsStableFailureCodeContract(t *testing.T) {
	setNpmVersionRecordedOptionsForControllerTest(t, nil)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/npm_version_options/refresh?package=@scope/unknown", nil)

	RefreshChannelNpmVersionOptions(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := decodeNpmVersionOptionsAPIResponse(t, recorder)
	require.False(t, body.Success)
	require.Equal(t, service.NpmCLIPackageUnsupportedCode, body.Code)
	require.Equal(t, service.NpmCLIPackageUnsupportedCode, body.Data.Code)
	require.Contains(t, body.Message, "unsupported npm package")
	require.Equal(t, "@scope/unknown", body.Data.PackageName)
	require.Equal(t, "npm", body.Data.Source)
}

func TestWriteNpmVersionOptionsErrorHidesInternalFailureMessage(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/npm_version_options/refresh?package=@openai/codex", nil)

	writeNpmVersionOptionsError(
		ctx,
		"@openai/codex",
		"persist",
		npmVersionCodedTestError{
			code:    service.NpmCLIVersionPersistFailedCode,
			message: "persist refreshed npm version options for @openai/codex: dial tcp 10.0.0.5:5432: connection refused",
		},
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := decodeNpmVersionOptionsAPIResponse(t, recorder)
	require.False(t, body.Success)
	require.Equal(t, service.NpmCLIVersionPersistFailedCode, body.Code)
	require.Equal(t, service.NpmCLIVersionPersistFailedCode, body.Data.Code)
	require.Equal(t, "npm version options could not be persisted", body.Message)
	require.NotContains(t, body.Message, "10.0.0.5")
}

func TestNpmVersionOptionsErrorSourceReportsPersistFailures(t *testing.T) {
	require.Equal(t, "persist", npmVersionOptionsErrorSource(
		"npm",
		npmVersionCodedTestError{code: service.NpmCLIVersionPersistFailedCode},
	))
	require.Equal(t, "npm", npmVersionOptionsErrorSource("npm", errors.New("registry down")))
}

func TestGetChannelNpmVersionDiagnosticsReturnsPackageStates(t *testing.T) {
	setNpmVersionRecordedOptionsForControllerTest(t, map[string]any{
		"packages": map[string]any{
			"@openai/codex": map[string]any{
				"fetched_at":     time.Unix(1000, 0).UTC(),
				"latest_version": "1.2.3",
				"options": []service.NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.2.3)", IsLatest: true, ResolvedVersion: "1.2.3"},
				},
			},
		},
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/npm_version_options/diagnostics", nil)

	GetChannelNpmVersionDiagnostics(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "0001-01-01T00:00:00Z")
	var body npmVersionDiagnosticsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.False(t, body.Data.GeneratedAt.IsZero())
	require.Greater(t, body.Data.RefreshIntervalMs, int64(0))
	require.Greater(t, body.Data.RegistryTimeoutMs, int64(0))
	require.Equal(t, len(body.Data.Packages), body.Data.Summary.PackageCount)
	require.GreaterOrEqual(t, body.Data.Summary.RecordedCount, 1)
	require.Equal(t, len(body.Data.Packages)-body.Data.Summary.RecordedCount, body.Data.Summary.MissingCount)
	require.NotEmpty(t, body.Data.Packages)
	var raw map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &raw))
	rawData, ok := raw["data"].(map[string]any)
	require.True(t, ok)
	rawPackages, ok := rawData["packages"].([]any)
	require.True(t, ok)
	for _, rawPackage := range rawPackages {
		item, ok := rawPackage.(map[string]any)
		require.True(t, ok)
		require.Contains(t, item, "recent_errors")
		_, ok = item["recent_errors"].([]any)
		require.True(t, ok)
	}
	var codex service.NpmCLIVersionDiagnostic
	for _, item := range body.Data.Packages {
		if item.PackageName == "@openai/codex" {
			codex = item
			break
		}
	}
	require.Equal(t, "recorded", codex.Source)
	require.True(t, codex.Recorded)
	require.Equal(t, "1.2.3", codex.LatestVersion)
	require.Equal(t, 1, codex.OptionCount)
}

func setNpmVersionRecordedOptionsForControllerTest(t *testing.T, recorded any) {
	t.Helper()

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	if recorded != nil {
		payload, err := common.Marshal(recorded)
		require.NoError(t, err)
		common.OptionMap["NpmCLIVersionRecordedOptions"] = string(payload)
	}
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})
}

func decodeNpmVersionOptionsAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) npmVersionOptionsAPIResponse {
	t.Helper()

	var body npmVersionOptionsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

type npmVersionCodedTestError struct {
	code    string
	message string
}

func (err npmVersionCodedTestError) Error() string {
	if err.message != "" {
		return err.message
	}
	return err.code
}

func (err npmVersionCodedTestError) NpmCLIVersionCode() string {
	return err.code
}
