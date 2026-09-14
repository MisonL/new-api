package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	modelsetting "github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func addCurrentTaskRouteHeaderPolicyForTest(t *testing.T, snapshot *model.TaskRouteSnapshot) {
	t.Helper()
	ciphertext, err := model.EncryptTaskRouteHeaderPolicy(nil)
	require.NoError(t, err)
	snapshot.HeaderPolicyCaptured = true
	snapshot.HeaderPolicyCiphertext = ciphertext
	snapshot.HeaderPolicyFingerprint = model.ComputeTaskRouteHeaderPolicyFingerprint(nil)
}

func TestBuildTaskRouteSnapshotFallbackUsesEffectiveGeminiModelVersion(t *testing.T) {
	settings := modelsetting.GetGeminiSettings()
	previous := settings.VersionSettings
	settings.VersionSettings = map[string]string{
		"default":        "v1beta",
		"gemini-1.0-pro": "v1",
	}
	t.Cleanup(func() { settings.VersionSettings = previous })

	snapshot, err := BuildTaskRouteSnapshot(nil, &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-model",
		UpstreamRequestPath: "",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			ChannelId:         9961,
			ChannelBaseUrl:    "https://gemini.example.test",
			UpstreamModelName: "gemini-1.0-pro",
		},
	}, constant.TaskPlatform("24"), constant.TaskActionGenerate)

	require.NoError(t, err)
	require.Equal(t, "/v1/operations", snapshot.SubmitPath)
	require.Equal(t, "/v1/operations", snapshot.PollingPath)
}

func TestBuildTaskRouteSnapshotAcceptsCanonicalViduPlatform(t *testing.T) {
	snapshot, err := BuildTaskRouteSnapshot(nil, &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "vidu-model",
		UpstreamRequestPath: "/ent/v2/text2video",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeVidu,
			ChannelId:         9962,
			ChannelBaseUrl:    "https://vidu.example.test",
			UpstreamModelName: "viduq2",
		},
	}, constant.TaskPlatformVidu, constant.TaskActionGenerate)

	require.NoError(t, err)
	require.Equal(t, string(constant.TaskPlatformVidu), snapshot.AdapterPlatform)
	require.Equal(t, "task-adapter/v1/52/vidu", snapshot.AdapterIdentity)
}

func TestTaskRouteIdentityFromSnapshotRejectsFingerprintMismatch(t *testing.T) {
	channel := &model.Channel{
		Id:   201,
		Type: constant.ChannelTypeOpenAI,
		Key:  "key-a\nkey-b",
		Keys: []string{"key-a", "key-b"},
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}
	valid := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "client-model",
		EffectiveUpstreamModel: "provider-model",
		FinalProtocol:          "openai",
		Operation:              "task.openai.generate",
		UpstreamPath:           "/v1/tasks/fetch",
		ChannelID:              channel.Id,
		MultiKeyIndex:          1,
		CredentialScoped:       true,
	}.Normalized()

	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:           taskRouteSnapshotSchemaVersion,
		Group:                   "default",
		BaseURL:                 "https://submit.example.test",
		RequestedModel:          valid.RequestedModel,
		EffectiveUpstreamModel:  valid.EffectiveUpstreamModel,
		Protocol:                valid.FinalProtocol,
		Operation:               valid.Operation,
		PollingOperation:        valid.Operation + ".poll",
		ContentOperation:        valid.Operation + ".content",
		SubmitPath:              valid.UpstreamPath,
		PollingPath:             valid.UpstreamPath,
		ContentPath:             valid.UpstreamPath,
		RouteFingerprint:        valid.Fingerprint,
		PollingRouteFingerprint: valid.Fingerprint,
		ContentRouteFingerprint: valid.Fingerprint,
		RouteScopeVersion:       "v1:test",
		ChannelID:               channel.Id,
		ChannelType:             channel.Type,
		AdapterPlatform:         "1",
		AdapterIdentity:         "task-adapter/v1/1/1",
		ProviderRuntime:         &model.TaskRouteProviderSnapshot{},
		MultiKeyIndex:           valid.MultiKeyIndex,
		CredentialScoped:        valid.CredentialScoped,
		ProxyCaptured:           true,
		ProxyCiphertext:         mustEncryptTaskRouteProxyForServiceTest(t, ""),
		ProxyFingerprint:        model.ComputeTaskRouteProxyFingerprint(""),
	}
	addCurrentTaskRouteHeaderPolicyForTest(t, snapshot)
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	route, err := TaskRouteIdentityFromSnapshot(snapshot, "submit", channel)
	require.NoError(t, err)
	require.Equal(t, valid.Fingerprint, route.Fingerprint)

	snapshot.EffectiveUpstreamModel = "different-provider-model"
	_, err = TaskRouteIdentityFromSnapshot(snapshot, "submit", channel)
	require.Error(t, err)
	require.ErrorContains(t, err, "fingerprint mismatch")
}

func TestTaskContentRoutePathForModelUsesMiniMaxFileRetrieveEndpoint(t *testing.T) {
	require.Equal(t, "/v1/files/retrieve", TaskContentRoutePathForModel(constant.ChannelTypeMiniMax, "MiniMax-Hailuo-2.3", constant.TaskActionGenerate))
	require.Equal(t, "/v1/files/retrieve", VideoContentHealthRoutePathForModel(constant.ChannelTypeMiniMax, "MiniMax-Hailuo-2.3"))
	require.Equal(t, "/v1/videos/content", TaskContentRoutePathForModel(constant.ChannelTypeOpenAI, "sora-2", constant.TaskActionGenerate))
}

func mustEncryptTaskRouteProxyForServiceTest(t *testing.T, proxy string) string {
	t.Helper()
	ciphertext, err := model.EncryptTaskRouteProxy(proxy)
	require.NoError(t, err)
	return ciphertext
}

func TestBuildTaskRouteSnapshotCapturesCurrentRouteScopeVersion(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousSQLite, previousMySQL, previousPostgreSQL := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = previousSQLite, previousMySQL, previousPostgreSQL
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})

	priority := int64(10)
	weight := uint(model.RouteWeightTotalBPS)
	channelID := 9941
	require.NoError(t, db.Create(&model.Channel{
		Id:     channelID,
		Name:   "scope-version-channel",
		Type:   constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:                    "default",
		Model:                    "provider-model",
		ChannelId:                channelID,
		Enabled:                  true,
		RoutePriority:            &priority,
		RouteWeightBPS:           &weight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)

	scopeID := model.EncodeRouteScopeID("default", "provider-model", priority)
	scope, err := model.GetChannelRouteScope(scopeID)
	require.NoError(t, err)
	require.NotEmpty(t, scope.Version)

	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteDiagnostics, dto.ChannelRouteSelectionDiagnostics{
		Group:               "default",
		EffectiveRouteModel: "provider-model",
		SelectedChannelID:   channelID,
		RouteScope:          scopeID,
	})
	diagnostics, diagnosticsOK := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](ctx, constant.ContextKeyChannelRouteDiagnostics)
	require.True(t, diagnosticsOK)
	require.Equal(t, scopeID, diagnostics.RouteScope)
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-model",
		UpstreamRequestPath: "/v1/videos",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         channelID,
			ChannelBaseUrl:    "https://submit.example.test",
			UpstreamModelName: "provider-model",
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(ctx, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.NoError(t, err)
	require.Equal(t, scope.Version, snapshot.RouteScopeVersion)
}

func TestBuildTaskRouteSnapshotCapturesStaticParamOverrideHeaders(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-model",
		UpstreamRequestPath: "/v1/videos",
		RequestHeaders:      map[string]string{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         9951,
			ChannelBaseUrl:    "https://submit.example.test",
			ApiKey:            "snapshot-key",
			UpstreamModelName: "provider-model",
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"mode":  "set_header",
						"path":  "X-Captured-Header",
						"value": "captured-value",
					},
				},
			},
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.NoError(t, err)
	captured, err := model.DecryptTaskRouteHeaderPolicy(snapshot.HeaderPolicyCiphertext)
	require.NoError(t, err)
	require.Equal(t, "captured-value", captured["X-Captured-Header"])
	require.True(t, info.UseRuntimeHeadersOverride)
}

func TestBuildTaskRouteSnapshotCapturesProviderRuntimeWithoutSensitiveHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("channel_other", "frozen-api-version")
	ctx.Set("api_version", "frozen-api-version-query")
	common.SetContextKey(ctx, constant.ContextKeyChannelStatusCodeMapping, `{"504":429}`)
	common.SetContextKey(ctx, constant.ContextKeyChannelOrganization, "frozen-organization")
	common.SetContextKey(ctx, constant.ContextKeyChannelSetting, dto.ChannelSettings{
		ForceFormat: true,
		Proxy:       "http://proxy-secret.example.test:8080",
	})
	supportsResponses := true
	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		AzureResponsesVersion:                 "2025-01-01",
		RequestBodyLimit:                      &dto.ChannelRequestBodyLimit{MaxBytes: 128 * 1024, Source: "test"},
		ResponsesCompactSummaryFallbackModels: []string{"fallback-a"},
		ResponsesCapabilityRegistry:           &dto.ResponsesChannelCapabilityRegistry{SupportsResponses: &supportsResponses},
	})
	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSettingRaw, `{"responses_stream_bootstrap_recovery_enabled":true}`)
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-model",
		UpstreamRequestPath: "/v1/videos",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         9953,
			ChannelBaseUrl:    "https://frozen.example.test",
			ApiKey:            "snapshot-key",
			UpstreamModelName: "provider-model",
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(ctx, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.NoError(t, err)
	require.NotNil(t, snapshot.ProviderRuntime)
	require.Equal(t, "frozen-api-version", snapshot.ProviderRuntime.Other)
	require.Equal(t, "frozen-api-version-query", snapshot.ProviderRuntime.ApiVersion)
	require.Equal(t, `{"504":429}`, snapshot.ProviderRuntime.StatusCodeMapping)
	require.Equal(t, "frozen-organization", snapshot.ProviderRuntime.OpenAIOrganization)
	require.True(t, snapshot.ProviderRuntime.ChannelSettings.ForceFormat)
	require.Empty(t, snapshot.ProviderRuntime.ChannelSettings.Proxy)
	require.Equal(t, "2025-01-01", snapshot.ProviderRuntime.ChannelOtherSettings.AzureResponsesVersion)
	require.Equal(t, []string{"fallback-a"}, snapshot.ProviderRuntime.ChannelOtherSettings.ResponsesCompactSummaryFallbackModels)
	require.Equal(t, int64(128*1024), snapshot.ProviderRuntime.ChannelOtherSettings.RequestBodyLimit.MaxBytes)
	require.True(t, *snapshot.ProviderRuntime.ChannelOtherSettings.ResponsesCapabilityRegistry.SupportsResponses)
	require.Nil(t, snapshot.ProviderRuntime.ChannelOtherSettings.HeaderProfileStrategy)
	require.True(t, snapshot.ProviderRuntime.ResponsesBootstrapRecoveryEnabled)
	settings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](ctx, constant.ContextKeyChannelOtherSetting)
	require.True(t, ok)
	settings.ResponsesCompactSummaryFallbackModels[0] = "mutated-after-capture"
	require.Equal(t, []string{"fallback-a"}, snapshot.ProviderRuntime.ChannelOtherSettings.ResponsesCompactSummaryFallbackModels)
	runtime, err := TaskRouteSnapshotProviderRuntime(snapshot)
	require.NoError(t, err)
	runtime.ChannelOtherSettings.ResponsesCompactSummaryFallbackModels[0] = "mutated-runtime"
	runtime.ChannelOtherSettings.RequestBodyLimit.MaxBytes = 256 * 1024
	*runtime.ChannelOtherSettings.ResponsesCapabilityRegistry.SupportsResponses = false
	require.Equal(t, []string{"fallback-a"}, snapshot.ProviderRuntime.ChannelOtherSettings.ResponsesCompactSummaryFallbackModels)
	require.Equal(t, int64(128*1024), snapshot.ProviderRuntime.ChannelOtherSettings.RequestBodyLimit.MaxBytes)
	require.True(t, *snapshot.ProviderRuntime.ChannelOtherSettings.ResponsesCapabilityRegistry.SupportsResponses)
	encoded, err := common.Marshal(snapshot.ProviderRuntime)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "proxy-secret")
}

func TestBeginTaskHealthAttemptUsesCapturedProviderRuntime(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-model",
		UpstreamRequestPath: "/v1/videos",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeOpenAI,
			ChannelId:            9954,
			ChannelBaseUrl:       "https://frozen.example.test",
			ApiKey:               "snapshot-key",
			ChannelOther:         "frozen-other",
			ApiVersion:           "frozen-query-version",
			StatusCodeMapping:    `{"504":429}`,
			Organization:         "frozen-organization",
			ChannelSetting:       dto.ChannelSettings{ForceFormat: true, Proxy: "http://frozen-proxy.example.test:8080"},
			ChannelOtherSettings: dto.ChannelOtherSettings{AzureResponsesVersion: "2025-01-01"},
			UpstreamModelName:    "provider-model",
		},
	}
	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.NoError(t, err)
	channelBaseURL := "https://live.example.test"
	channel := &model.Channel{
		Id:      9954,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "snapshot-key",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &channelBaseURL,
		Other:   "live-other",
	}
	channel.SetSetting(dto.ChannelSettings{ForceFormat: false, Proxy: "http://live-proxy.example.test:8080"})
	channel.SetOtherSettings(dto.ChannelOtherSettings{AzureResponsesVersion: "live-version"})
	statusMapping := `{"500":503}`
	channel.StatusCodeMapping = &statusMapping
	organization := "live-organization"
	channel.OpenAIOrganization = &organization
	task := &model.Task{
		TaskID:    "task-provider-runtime",
		ChannelId: channel.Id,
		Group:     "default",
		Platform:  constant.TaskPlatform("1"),
		PrivateData: model.TaskPrivateData{
			Key:           "snapshot-key",
			RouteSnapshot: snapshot,
		},
	}

	attempt, err := BeginTaskHealthAttempt(context.Background(), task, channel, "snapshot-key", "poll")
	require.NoError(t, err)
	defer attempt.Release()
	require.Equal(t, "frozen-other", attempt.Info.ChannelMeta.ChannelOther)
	require.Equal(t, "frozen-query-version", attempt.Info.ChannelMeta.ApiVersion)
	require.Equal(t, `{"504":429}`, attempt.Info.ChannelMeta.StatusCodeMapping)
	require.Equal(t, "frozen-organization", attempt.Info.ChannelMeta.Organization)
	require.True(t, attempt.Info.ChannelMeta.ChannelSetting.ForceFormat)
	require.Equal(t, "http://frozen-proxy.example.test:8080", attempt.Info.ChannelMeta.ChannelSetting.Proxy)
	require.Equal(t, "2025-01-01", attempt.Info.ChannelMeta.ChannelOtherSettings.AzureResponsesVersion)
	require.Equal(t, "https://frozen.example.test", common.GetContextKeyString(attempt.Context, constant.ContextKeyChannelBaseUrl))
	require.Equal(t, "frozen-other", attempt.Context.GetString("channel_other"))
}

func TestBeginTaskHealthAttemptLegacyUsesPersistedEffectiveModelWithoutMapping(t *testing.T) {
	mapping := `{"client-model":"new-provider-model"}`
	channel := &model.Channel{
		Id:           9955,
		Type:         constant.ChannelTypeOpenAI,
		Key:          "legacy-key",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: &mapping,
	}
	task := &model.Task{
		TaskID:    "legacy-task-with-effective-model",
		ChannelId: channel.Id,
		Group:     "default",
		Platform:  constant.TaskPlatform("1"),
		Properties: model.Properties{
			OriginModelName:   "client-model",
			UpstreamModelName: "old-provider-model",
		},
		PrivateData: model.TaskPrivateData{Key: "legacy-key"},
	}

	attempt, err := BeginTaskHealthAttempt(context.Background(), task, channel, "legacy-key", "poll")
	require.NoError(t, err)
	defer attempt.Release()
	require.Equal(t, "old-provider-model", attempt.Info.ChannelMeta.UpstreamModelName)
	require.Equal(t, "old-provider-model", attempt.Info.ChannelHealthRoute.EffectiveUpstreamModel)
	_, mappingPresent := common.GetContextKey(attempt.Context, constant.ContextKeyChannelModelMapping)
	require.False(t, mappingPresent)
}

func TestBeginTaskHealthAttemptLegacyWithoutEffectiveModelFailsClosed(t *testing.T) {
	mapping := `{"client-model":"current-provider-model"}`
	channel := &model.Channel{
		Id:           9956,
		Type:         constant.ChannelTypeOpenAI,
		Key:          "legacy-key",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: &mapping,
	}
	task := &model.Task{
		TaskID:    "legacy-task-without-effective-model",
		ChannelId: channel.Id,
		Group:     "default",
		Platform:  constant.TaskPlatform("1"),
		Properties: model.Properties{
			OriginModelName: "client-model",
		},
		PrivateData: model.TaskPrivateData{Key: "legacy-key"},
	}

	attempt, err := BeginTaskHealthAttempt(context.Background(), task, channel, "legacy-key", "poll")
	require.Nil(t, attempt)
	require.ErrorIs(t, err, ErrTaskRouteMappingMissing)
	// The current channel mapping is deliberately not a compatibility fallback.
	require.NotContains(t, err.Error(), "current-provider-model")
}

func TestBuildTaskRouteSnapshotRejectsBodyParamOverride(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-model",
		UpstreamRequestPath: "/v1/videos",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         9952,
			ChannelBaseUrl:    "https://submit.example.test",
			UpstreamModelName: "provider-model",
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"mode":  "set",
						"path":  "prompt",
						"value": "must-not-be-dropped",
					},
				},
			},
		},
	}

	_, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.Error(t, err)
	require.ErrorContains(t, err, "header-only")
}

func TestResolveTaskRouteScopeVersionDoesNotSilentlyFallbackForInvalidScope(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteDiagnostics, dto.ChannelRouteSelectionDiagnostics{
		Group:               "default",
		EffectiveRouteModel: "provider-model",
		SelectedChannelID:   9942,
		RouteScope:          "invalid-route-scope",
	})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 9942}}

	got, err := resolveTaskRouteScopeVersion(ctx, info, relaycommon.ChannelRouteIdentity{Fingerprint: "fallback"})
	require.Error(t, err)
	require.Empty(t, got)
}

func TestTaskRouteIdentityFromSnapshotRejectsCredentialScopeDrift(t *testing.T) {
	baseInfo := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-model",
		UpstreamRequestPath: "/v1/tasks",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeOpenAI,
			ChannelId:            215,
			ChannelBaseUrl:       "https://submit.example.test",
			ChannelIsMultiKey:    true,
			ChannelMultiKeyIndex: 0,
			UpstreamModelName:    "provider-model",
		},
	}
	multiKeySnapshot, err := BuildTaskRouteSnapshot(nil, baseInfo, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.NoError(t, err)

	singleKeyChannel := &model.Channel{Id: 215, Type: constant.ChannelTypeOpenAI, Key: "key-a"}
	_, err = TaskRouteIdentityFromSnapshot(multiKeySnapshot, "poll", singleKeyChannel)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires multi-key channel")

	singleInfo := *baseInfo
	singleInfo.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelType:       constant.ChannelTypeOpenAI,
		ChannelId:         216,
		ChannelBaseUrl:    "https://submit.example.test",
		ChannelIsMultiKey: false,
		UpstreamModelName: "provider-model",
	}
	singleKeySnapshot, err := BuildTaskRouteSnapshot(nil, &singleInfo, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.NoError(t, err)
	multiKeyChannel := &model.Channel{
		Id:   216,
		Type: constant.ChannelTypeOpenAI,
		Key:  "key-a\nkey-b",
		Keys: []string{"key-a", "key-b"},
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}
	_, err = TaskRouteIdentityFromSnapshot(singleKeySnapshot, "poll", multiKeyChannel)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing credential scope")
}

func newTaskRouteSnapshotForChannelCompatibilityTest(t *testing.T) *model.TaskRouteSnapshot {
	t.Helper()
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-model",
		UpstreamRequestPath: "/v1/videos",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         301,
			ChannelBaseUrl:    "https://submit.example.test",
			UpstreamModelName: "provider-model",
		},
	}
	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.NoError(t, err)
	return snapshot
}

func TestValidateTaskRouteSnapshotChannelCompatibilityAcceptsCurrentChannel(t *testing.T) {
	snapshot := newTaskRouteSnapshotForChannelCompatibilityTest(t)
	channel := &model.Channel{Id: snapshot.ChannelID, Type: snapshot.ChannelType}

	require.NoError(t, ValidateTaskRouteSnapshotChannelCompatibility(snapshot, channel))
}

func TestValidateTaskRouteSnapshotChannelCompatibilityRejectsChannelTypeDrift(t *testing.T) {
	snapshot := newTaskRouteSnapshotForChannelCompatibilityTest(t)
	channel := &model.Channel{Id: snapshot.ChannelID, Type: constant.ChannelTypeGemini}

	err := ValidateTaskRouteSnapshotChannelCompatibility(snapshot, channel)
	require.Error(t, err)
	require.ErrorContains(t, err, "channel type mismatch")
}

func TestValidateTaskRouteSnapshotChannelCompatibilityRejectsAdapterPlatformDrift(t *testing.T) {
	snapshot := newTaskRouteSnapshotForChannelCompatibilityTest(t)
	snapshot.AdapterPlatform = "24"
	snapshot.AdapterIdentity = "task-adapter/v1/1/24"
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	channel := &model.Channel{Id: snapshot.ChannelID, Type: snapshot.ChannelType}

	err := ValidateTaskRouteSnapshotChannelCompatibility(snapshot, channel)
	require.Error(t, err)
	require.ErrorContains(t, err, "adapter platform mismatch")
}

func TestValidateTaskRouteSnapshotChannelCompatibilityRejectsChannelIDDrift(t *testing.T) {
	snapshot := newTaskRouteSnapshotForChannelCompatibilityTest(t)
	channel := &model.Channel{Id: snapshot.ChannelID + 1, Type: snapshot.ChannelType}

	err := ValidateTaskRouteSnapshotChannelCompatibility(snapshot, channel)
	require.Error(t, err)
	require.ErrorContains(t, err, "channel ID mismatch")
}

func TestValidateTaskRouteSnapshotChannelCompatibilityKeepsLegacyCompatibilityPath(t *testing.T) {
	snapshot := newTaskRouteSnapshotForChannelCompatibilityTest(t)
	snapshot.SchemaVersion = taskRouteSnapshotSchemaVersion - 1
	channel := &model.Channel{Id: snapshot.ChannelID + 1, Type: constant.ChannelTypeGemini}

	require.NoError(t, ValidateTaskRouteSnapshotChannelCompatibility(snapshot, channel))
}

func TestTaskRouteIdentityFromSnapshotRejectsMissingPhaseFingerprintForCurrentSchema(t *testing.T) {
	channel := &model.Channel{Id: 209, Type: constant.ChannelTypeOpenAI}
	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:           taskRouteSnapshotSchemaVersion,
		Group:                   "default",
		BaseURL:                 "https://submit.example.test",
		RequestedModel:          "client-model",
		EffectiveUpstreamModel:  "provider-model",
		Protocol:                "openai",
		Operation:               "task.openai.generate",
		PollingOperation:        "task.openai.generate.poll",
		ContentOperation:        "task.openai.generate.content",
		SubmitPath:              "/v1/tasks",
		PollingPath:             "/v1/tasks/fetch",
		ContentPath:             "/v1/videos/content",
		ChannelID:               channel.Id,
		ChannelType:             channel.Type,
		AdapterPlatform:         "1",
		AdapterIdentity:         "task-adapter/v1/1/1",
		ProviderRuntime:         &model.TaskRouteProviderSnapshot{},
		RouteFingerprint:        "placeholder",
		PollingRouteFingerprint: "not-valid",
		ContentRouteFingerprint: "not-valid",
		RouteScopeVersion:       "v1:test",
		ProxyCaptured:           true,
		ProxyCiphertext:         mustEncryptTaskRouteProxyForServiceTest(t, ""),
		ProxyFingerprint:        model.ComputeTaskRouteProxyFingerprint(""),
	}
	addCurrentTaskRouteHeaderPolicyForTest(t, snapshot)
	// Recompute the snapshot digest after removing the phase fingerprint so
	// Validate reaches the intended required-field guard instead of reporting a
	// digest mismatch caused by a stale test fixture.
	snapshot.RouteFingerprint = ""
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	_, err := TaskRouteIdentityFromSnapshot(snapshot, "submit", channel)
	require.Error(t, err)
	require.ErrorContains(t, err, "phase fingerprint is missing")
}

func TestTaskRouteIdentityFromSnapshotUsesPhaseSpecificOperations(t *testing.T) {
	channel := &model.Channel{Id: 202, Type: constant.ChannelTypeOpenAI}
	base := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "client-model",
		EffectiveUpstreamModel: "provider-model",
		FinalProtocol:          "openai",
		Operation:              "task.openai.generate",
		UpstreamPath:           "/v1/tasks",
		ChannelID:              channel.Id,
	}.Normalized()
	poll := base
	poll.Operation = "task.openai.generate.poll"
	poll.UpstreamPath = "/v1/tasks/fetch"
	poll.Fingerprint = ""
	poll = poll.Normalized()
	content := base
	content.Operation = "task.openai.generate.content"
	content.UpstreamPath = "/v1/videos/content"
	content.Fingerprint = ""
	content = content.Normalized()

	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:           taskRouteSnapshotSchemaVersion,
		Group:                   "default",
		BaseURL:                 "https://submit.example.test/",
		RequestedModel:          base.RequestedModel,
		EffectiveUpstreamModel:  base.EffectiveUpstreamModel,
		Protocol:                base.FinalProtocol,
		Operation:               base.Operation,
		PollingOperation:        poll.Operation,
		ContentOperation:        content.Operation,
		SubmitPath:              base.UpstreamPath,
		PollingPath:             poll.UpstreamPath,
		ContentPath:             content.UpstreamPath,
		RouteFingerprint:        base.Fingerprint,
		PollingRouteFingerprint: poll.Fingerprint,
		ContentRouteFingerprint: content.Fingerprint,
		ChannelID:               channel.Id,
		ChannelType:             channel.Type,
		AdapterPlatform:         "1",
		AdapterIdentity:         "task-adapter/v1/1/1",
		ProviderRuntime:         &model.TaskRouteProviderSnapshot{},
		RouteScopeVersion:       "v1:test",
		ProxyCaptured:           true,
		ProxyCiphertext:         mustEncryptTaskRouteProxyForServiceTest(t, ""),
		ProxyFingerprint:        model.ComputeTaskRouteProxyFingerprint(""),
	}
	addCurrentTaskRouteHeaderPolicyForTest(t, snapshot)
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	pollRoute, err := TaskRouteIdentityFromSnapshot(snapshot, "poll", channel)
	require.NoError(t, err)
	require.Equal(t, poll.Operation, pollRoute.Operation)
	require.Equal(t, poll.Fingerprint, pollRoute.Fingerprint)

	contentRoute, err := TaskRouteIdentityFromSnapshot(snapshot, "content", channel)
	require.NoError(t, err)
	require.Equal(t, content.Operation, contentRoute.Operation)
	require.Equal(t, content.Fingerprint, contentRoute.Fingerprint)

	currentBaseURL := "https://current.example.test"
	baseURL, err := TaskRouteSnapshotBaseURL(snapshot, &model.Channel{Id: channel.Id, Type: channel.Type, BaseURL: &currentBaseURL})
	require.NoError(t, err)
	require.Equal(t, "https://submit.example.test", baseURL)
}

func TestTaskRouteSnapshotBaseURLLegacyFallbackAndStrictMissing(t *testing.T) {
	current := "https://current.example.test/"
	channel := &model.Channel{Id: 203, BaseURL: &current}

	baseURL, err := TaskRouteSnapshotBaseURL(&model.TaskRouteSnapshot{SchemaVersion: 1}, channel)
	require.NoError(t, err)
	require.Equal(t, "https://current.example.test", baseURL)

	// Version 2 introduced a persisted base URL. It must not fall back to the
	// mutable channel endpoint once the field is expected to exist.
	_, err = TaskRouteSnapshotBaseURL(&model.TaskRouteSnapshot{SchemaVersion: 3}, channel)
	require.Error(t, err)
	require.ErrorContains(t, err, "base URL is missing")

	// Version 4 first validates the complete authenticated snapshot, which
	// intentionally reports an incomplete identity before using any field.
	_, err = TaskRouteSnapshotBaseURL(&model.TaskRouteSnapshot{SchemaVersion: taskRouteSnapshotSchemaVersion}, channel)
	require.Error(t, err)
	require.ErrorContains(t, err, "required identity fields are incomplete")
}

func TestBeginChannelHealthAttemptWithRouteAndBaseURLBindsSnapshotEndpoint(t *testing.T) {
	currentBaseURL := "https://current.example.test"
	channel := &model.Channel{Id: 204, BaseURL: &currentBaseURL}
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "client-model",
		EffectiveUpstreamModel: "provider-model",
		FinalProtocol:          "openai",
		Operation:              "task.openai.generate.poll",
		UpstreamPath:           "/v1/tasks/fetch",
		ChannelID:              channel.Id,
	}.Normalized()

	attempt, err := BeginChannelHealthAttemptWithRouteAndBaseURL(context.Background(), "default", route, channel, "key", "https://submit.example.test/")
	require.NoError(t, err)
	require.Equal(t, "https://submit.example.test", attempt.Info.ChannelMeta.ChannelBaseUrl)
	require.Equal(t, "https://submit.example.test", common.GetContextKeyString(attempt.Context, constant.ContextKeyChannelBaseUrl))
	attempt.Release()
}

func TestBeginChannelHealthAttemptWithRouteZeroesNonMultiKeyIndex(t *testing.T) {
	baseURL := "https://submit.example.test"
	channel := &model.Channel{Id: 207, BaseURL: &baseURL}
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "client-model",
		EffectiveUpstreamModel: "provider-model",
		FinalProtocol:          "openai",
		Operation:              "task.openai.generate.poll",
		UpstreamPath:           "/v1/tasks/fetch",
		ChannelID:              channel.Id,
		MultiKeyIndex:          7,
	}.Normalized()

	attempt, err := BeginChannelHealthAttemptWithRouteAndBaseURL(context.Background(), "default", route, channel, "key", baseURL)
	require.NoError(t, err)
	require.Equal(t, 0, attempt.Info.ChannelMeta.ChannelMultiKeyIndex)
	attempt.Release()
}

func TestBeginChannelHealthAttemptWithRouteRejectsMalformedPath(t *testing.T) {
	baseURL := "https://submit.example.test"
	channel := &model.Channel{Id: 208, BaseURL: &baseURL}
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "client-model",
		EffectiveUpstreamModel: "provider-model",
		FinalProtocol:          "openai",
		Operation:              "task.openai.generate.poll",
		UpstreamPath:           "/v1/tasks/\x00fetch",
		ChannelID:              channel.Id,
	}.Normalized()

	_, err := BeginChannelHealthAttemptWithRouteAndBaseURL(context.Background(), "default", route, channel, "key", baseURL)
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid task health route path")
}

func TestBuildTaskRouteSnapshotUsesPhaseSpecificFingerprints(t *testing.T) {
	baseURL := "https://submit.example.test/"
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "suno_music",
		RelayMode:           relayconstant.RelayModeVideoFetchByID,
		UpstreamRequestPath: "/suno/generate",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeSunoAPI,
			ChannelId:            205,
			ChannelBaseUrl:       baseURL,
			ChannelIsMultiKey:    true,
			ChannelMultiKeyIndex: 2,
			UpstreamModelName:    "suno-v3",
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatformSuno, constant.SunoActionMusic)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Equal(t, "https://submit.example.test", snapshot.BaseURL)
	require.Equal(t, "suno_music", snapshot.RequestedModel)
	require.Equal(t, "suno-v3", snapshot.EffectiveUpstreamModel)
	require.NotEqual(t, snapshot.Operation, snapshot.PollingOperation)
	require.NotEqual(t, snapshot.Operation, snapshot.ContentOperation)
	require.NotEmpty(t, snapshot.PollingRouteFingerprint)
	require.NotEmpty(t, snapshot.ContentRouteFingerprint)

	channel := &model.Channel{
		Id:   205,
		Type: constant.ChannelTypeSunoAPI,
		Key:  "key-a\nkey-b\nkey-c",
		Keys: []string{"key-a", "key-b", "key-c"},
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}
	poll, err := TaskRouteIdentityFromSnapshot(snapshot, "poll", channel)
	require.NoError(t, err)
	require.Equal(t, snapshot.PollingOperation, poll.Operation)
	require.Equal(t, snapshot.PollingRouteFingerprint, poll.Fingerprint)

	content, err := TaskRouteIdentityFromSnapshot(snapshot, "content", channel)
	require.NoError(t, err)
	require.Equal(t, snapshot.ContentOperation, content.Operation)
	require.Equal(t, snapshot.ContentRouteFingerprint, content.Fingerprint)
}

func TestBuildTaskRouteSnapshotKeepsCanonicalTaskOperation(t *testing.T) {
	baseURL := "https://submit.example.test"
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "suno_music",
		RelayMode:           relayconstant.RelayModeVideoFetchByID,
		UpstreamRequestPath: "/suno/generate",
		ChannelHealthRoute: &relaycommon.ChannelRouteIdentity{
			RequestedModel:         "suno_music",
			EffectiveUpstreamModel: "suno-v3",
			FinalProtocol:          "openai",
			Operation:              "relay.42.17./suno/generate",
			UpstreamPath:           "/suno/generate",
			ChannelID:              212,
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeSunoAPI,
			ChannelId:         212,
			ChannelBaseUrl:    baseURL,
			UpstreamModelName: "suno-v3",
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatformSuno, constant.SunoActionMusic)
	require.NoError(t, err)
	require.Equal(t, "task.suno.music", snapshot.Operation)
	require.Equal(t, "task.suno.music.poll", snapshot.PollingOperation)
	require.Equal(t, "task.suno.music.content", snapshot.ContentOperation)
}

func TestTaskRouteSnapshotProxyVerifiesDecryptedFingerprint(t *testing.T) {
	const proxy = "http://proxy.example.test:8080"
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "suno_music",
		RelayMode:           relayconstant.RelayModeVideoFetchByID,
		UpstreamRequestPath: "/suno/generate",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeSunoAPI,
			ChannelId:         213,
			ChannelBaseUrl:    "https://submit.example.test",
			UpstreamModelName: "suno-v3",
			ChannelSetting:    dto.ChannelSettings{Proxy: proxy},
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatformSuno, constant.SunoActionMusic)
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.ProxyCiphertext)
	snapshot.Proxy = ""

	got, err := TaskRouteSnapshotProxy(snapshot, nil)
	require.NoError(t, err)
	require.Equal(t, proxy, got)
	// Resolving a follow-up route must not mutate the shared task snapshot.
	require.Empty(t, snapshot.Proxy)

	snapshot.Proxy = ""
	snapshot.ProxyFingerprint = model.ComputeTaskRouteProxyFingerprint("http://other.example.test:8080")
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	_, err = TaskRouteSnapshotProxy(snapshot, nil)
	require.ErrorContains(t, err, "proxy fingerprint mismatch")
}

func TestBuildTaskRouteSnapshotUsesPlannedSubmitURLPath(t *testing.T) {
	baseURL := "https://submit.example.test"
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-model",
		RelayMode:           relayconstant.RelayModeVideoFetchByID,
		UpstreamRequestPath: "/legacy/path",
		TaskRelayInfo:       &relaycommon.TaskRelayInfo{SubmitRequestURL: baseURL + "/provider/v2/tasks?api-version=2026-01"},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         211,
			ChannelBaseUrl:    baseURL,
			UpstreamModelName: "provider-model",
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.NoError(t, err)
	require.Equal(t, "/provider/v2/tasks", snapshot.SubmitPath)
}

func TestBuildTaskRouteSnapshotUsesSelectedConcreteAutoGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "auto")
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "premium")

	info := &relaycommon.RelayInfo{
		UsingGroup:      "auto",
		TokenGroup:      "auto",
		OriginModelName: "client-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         221,
			ChannelBaseUrl:    "https://submit.example.test",
			UpstreamModelName: "provider-model",
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(c, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.NoError(t, err)
	require.Equal(t, "premium", snapshot.Group)
}

func TestBuildTaskRouteSnapshotUsesContinuationConcreteGroup(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UsingGroup:      "auto",
		TokenGroup:      "auto",
		OriginModelName: "client-model",
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			OriginRouteGroup: "origin-group",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         222,
			ChannelBaseUrl:    "https://submit.example.test",
			UpstreamModelName: "provider-model",
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.NoError(t, err)
	require.Equal(t, "origin-group", snapshot.Group)
}

func TestBuildTaskRouteSnapshotRejectsUnresolvedAutoGroup(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UsingGroup:      "auto",
		TokenGroup:      "auto",
		UserGroup:       "user-without-this-auto-group",
		OriginModelName: "client-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         223,
			ChannelBaseUrl:    "https://submit.example.test",
			UpstreamModelName: "provider-model",
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.Error(t, err)
	require.Nil(t, snapshot)
	require.Contains(t, err.Error(), "cannot resolve channel")
}

func TestBuildTaskRouteSnapshotRejectsInvalidChannelTypeWithoutPanic(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName:     "task-model",
		RelayMode:           relayconstant.RelayModeVideoFetchByID,
		UpstreamRequestPath: "/v1/tasks",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       -1,
			ChannelId:         206,
			ChannelBaseUrl:    "",
			UpstreamModelName: "provider-model",
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.Error(t, err)
	require.Nil(t, snapshot)
}

func TestBuildTaskRouteSnapshotRejectsEmptyRequestedIdentity(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeVideoFetchByID,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         210,
			UpstreamModelName: "provider-model",
		},
	}
	snapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatform("1"), constant.TaskActionGenerate)
	require.Error(t, err)
	require.Nil(t, snapshot)
}

func TestBuildTaskRouteSnapshotCapturesStaticHeaderPolicy(t *testing.T) {
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "task-route-header-policy-test-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	request, err := http.NewRequest(http.MethodPost, "https://gateway.example.test/v1/videos", nil)
	require.NoError(t, err)
	c.Request = request
	common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileSelection, RuntimeHeaderProfileSelection{
		ChannelID:    214,
		AttemptIndex: 0,
		ProfileID:    "profile-a",
		Headers: map[string]string{
			"X-Profile":     "profile-secret",
			"Authorization": "profile-auth",
		},
	})

	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-video",
		RelayMode:           relayconstant.RelayModeVideoFetchByID,
		UpstreamRequestPath: "/v1/videos",
		RuntimeHeadersOverride: map[string]interface{}{
			"X-Static":  "static-secret",
			"X-Api-Key": "{api_key}",
		},
		UseRuntimeHeadersOverride: true,
		ChannelHealthRoute: &relaycommon.ChannelRouteIdentity{
			RequestedModel:         "client-video",
			EffectiveUpstreamModel: "provider-video",
			FinalProtocol:          "openai",
			Operation:              "relay.video",
			UpstreamPath:           "/v1/videos",
			ChannelID:              214,
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeSunoAPI,
			ChannelId:            214,
			ChannelBaseUrl:       "https://submit.example.test",
			ApiKey:               "api-key-214",
			UpstreamModelName:    "provider-video",
			ChannelOtherSettings: dto.ChannelOtherSettings{HeaderProfileStrategy: &dto.HeaderProfileStrategy{Enabled: true}},
		},
	}

	snapshot, err := BuildTaskRouteSnapshot(c, info, constant.TaskPlatformSuno, constant.SunoActionMusic)
	require.NoError(t, err)
	require.True(t, snapshot.HeaderPolicyCaptured)
	require.NotEmpty(t, snapshot.HeaderPolicyCiphertext)
	require.NotEmpty(t, snapshot.HeaderPolicyFingerprint)

	captured, err := model.DecryptTaskRouteHeaderPolicy(snapshot.HeaderPolicyCiphertext)
	require.NoError(t, err)
	require.Equal(t, "profile-secret", captured["X-Profile"])
	require.Equal(t, "static-secret", captured["X-Static"])
	require.Equal(t, "api-key-214", captured["X-Api-Key"])
	_, unsafeProfile := captured["Authorization"]
	require.False(t, unsafeProfile)

	stored, err := (model.TaskPrivateData{RouteSnapshot: snapshot}).Value()
	require.NoError(t, err)
	storedBytes, ok := stored.([]byte)
	require.True(t, ok)
	require.NotContains(t, string(storedBytes), "profile-secret")
	require.NotContains(t, string(storedBytes), "static-secret")

	info.RuntimeHeadersOverride["X-Static"] = "changed-after-submit"
	requestHeaders, err := BuildTaskRouteSnapshotRequestHeaders(nil, "rotated-key", http.Header{"X-Existing": []string{"keep"}}, snapshot)
	require.NoError(t, err)
	require.Equal(t, "profile-secret", requestHeaders.Get("X-Profile"))
	require.Equal(t, "static-secret", requestHeaders.Get("X-Static"))
	require.Equal(t, "api-key-214", requestHeaders.Get("X-Api-Key"))
	require.Equal(t, "keep", requestHeaders.Get("X-Existing"))
}

func TestBuildTaskRouteSnapshotRequestHeadersReplaysSchema5Policy(t *testing.T) {
	ciphertext, err := model.EncryptTaskRouteHeaderPolicy(map[string]string{
		"X-Captured": "submitted-value",
	})
	require.NoError(t, err)
	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:            model.TaskRouteSnapshotHeaderPolicySchemaVersion,
		HeaderPolicyCaptured:     true,
		HeaderPolicyCiphertext:   ciphertext,
		HeaderPolicyFingerprint:  model.ComputeTaskRouteHeaderPolicyFingerprint(map[string]string{"X-Captured": "submitted-value"}),
		HeaderProfileID:          "",
		HeaderProfileFingerprint: "",
	}

	requestHeaders, err := BuildTaskRouteSnapshotRequestHeaders(nil, "rotated-key", nil, snapshot)
	require.NoError(t, err)
	require.Equal(t, "submitted-value", requestHeaders.Get("X-Captured"))
}

func TestBuildTaskRouteSnapshotRejectsClientDynamicHeaderPolicy(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]interface{}
		passed   map[string]string
		profile  map[string]string
		want     string
	}{
		{
			name:     "wildcard passthrough",
			override: map[string]interface{}{"*": "passthrough"},
			want:     "client-dynamic passthrough rule",
		},
		{
			name:     "regex passthrough",
			override: map[string]interface{}{"re:^x-client-": "passthrough"},
			want:     "client-dynamic passthrough rule",
		},
		{
			name:     "client header placeholder",
			override: map[string]interface{}{"X-Client": "{client_header:X-Request-Dynamic}"},
			want:     "client-dynamic placeholder",
		},
		{
			name:   "runtime passed header",
			passed: map[string]string{"X-Request-Dynamic": "request-secret"},
			want:   "depends on client-dynamic headers",
		},
		{
			name:   "runtime passed header with blank name",
			passed: map[string]string{" \t": "request-secret"},
			want:   "empty client-dynamic header name",
		},
		{
			name:    "profile placeholder",
			profile: map[string]string{"X-Profile": "{client_header:X-Request-Dynamic}"},
			want:    "client-dynamic placeholder",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(nil)
			c.Request = httptest.NewRequest(http.MethodPost, "https://gateway.example.test/v1/videos", nil)
			profileHeaders := map[string]string{"X-Profile": "profile-secret"}
			for name, value := range test.profile {
				profileHeaders[name] = value
			}
			common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileSelection, RuntimeHeaderProfileSelection{
				ChannelID: 214,
				ProfileID: "profile-a",
				Headers:   profileHeaders,
			})
			info := &relaycommon.RelayInfo{
				UsingGroup:                  "default",
				OriginModelName:             "client-video",
				RelayMode:                   relayconstant.RelayModeVideoFetchByID,
				UpstreamRequestPath:         "/v1/videos",
				RuntimeHeadersOverride:      test.override,
				RuntimePassedRequestHeaders: test.passed,
				UseRuntimeHeadersOverride:   true,
				ChannelHealthRoute: &relaycommon.ChannelRouteIdentity{
					RequestedModel:         "client-video",
					EffectiveUpstreamModel: "provider-video",
					FinalProtocol:          "openai",
					Operation:              "relay.video",
					UpstreamPath:           "/v1/videos",
					ChannelID:              214,
				},
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:          constant.ChannelTypeSunoAPI,
					ChannelId:            214,
					ChannelBaseUrl:       "https://submit.example.test",
					ApiKey:               "api-key-214",
					UpstreamModelName:    "provider-video",
					ChannelOtherSettings: dto.ChannelOtherSettings{HeaderProfileStrategy: &dto.HeaderProfileStrategy{Enabled: true}},
				},
			}

			snapshot, err := BuildTaskRouteSnapshot(c, info, constant.TaskPlatformSuno, constant.SunoActionMusic)
			require.Error(t, err)
			require.Nil(t, snapshot)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestBuildTaskRouteSnapshotRequestHeadersRejectsTamperedHeaderPolicy(t *testing.T) {
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "task-route-header-policy-tamper-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })

	encoded, err := model.EncryptTaskRouteHeaderPolicy(map[string]string{"X-Static": "secret"})
	require.NoError(t, err)
	snapshot := &model.TaskRouteSnapshot{
		HeaderPolicyCaptured:    true,
		HeaderPolicyCiphertext:  encoded,
		HeaderPolicyFingerprint: model.ComputeTaskRouteHeaderPolicyFingerprint(map[string]string{"X-Static": "secret"}),
	}
	// The helper validates the complete snapshot before decrypting. Use a
	// minimal current-schema fixture to exercise authenticated ciphertext failure
	// rather than an unrelated required-field error.
	snapshot.SchemaVersion = taskRouteSnapshotSchemaVersion
	snapshot.Group = "default"
	snapshot.BaseURL = "https://submit.example.test"
	snapshot.RequestedModel = "client-model"
	snapshot.EffectiveUpstreamModel = "provider-model"
	snapshot.Protocol = "openai"
	snapshot.Operation = "task.suno.music"
	snapshot.PollingOperation = "task.suno.music.poll"
	snapshot.ContentOperation = "task.suno.music.content"
	snapshot.SubmitPath = "/suno/submit"
	snapshot.PollingPath = "/suno/fetch"
	snapshot.ContentPath = "/v1/videos/content"
	snapshot.ChannelID = 215
	snapshot.ChannelType = constant.ChannelTypeSunoAPI
	snapshot.AdapterPlatform = string(constant.TaskPlatformSuno)
	snapshot.AdapterIdentity = "task-adapter/v1/37/suno"
	snapshot.ProviderRuntime = &model.TaskRouteProviderSnapshot{}
	snapshot.RouteScopeVersion = "v1:test"
	base := relaycommon.ChannelRouteIdentity{RequestedModel: "client-model", EffectiveUpstreamModel: "provider-model", FinalProtocol: "openai", Operation: "task.suno.music", UpstreamPath: "/suno/submit", ChannelID: 215}.Normalized()
	poll := base
	poll.Operation = "task.suno.music.poll"
	poll.UpstreamPath = "/suno/fetch"
	poll.Fingerprint = ""
	poll = poll.Normalized()
	content := base
	content.Operation = "task.suno.music.content"
	content.UpstreamPath = "/v1/videos/content"
	content.Fingerprint = ""
	content = content.Normalized()
	snapshot.RouteFingerprint = base.Fingerprint
	snapshot.PollingRouteFingerprint = poll.Fingerprint
	snapshot.ContentRouteFingerprint = content.Fingerprint
	snapshot.ProxyCaptured = true
	snapshot.ProxyCiphertext = mustEncryptTaskRouteProxyForServiceTest(t, "")
	snapshot.ProxyFingerprint = model.ComputeTaskRouteProxyFingerprint("")
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	decoded := strings.TrimPrefix(encoded, "v1:")
	replacement := "0"
	if strings.HasPrefix(decoded, "0") {
		replacement = "1"
	}
	snapshot.HeaderPolicyCiphertext = "v1:" + replacement + decoded[1:]
	_, err = BuildTaskRouteSnapshotRequestHeaders(nil, "key", nil, snapshot)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt task route header policy")
}

func TestResolveTaskHealthGroupPrefersSubmissionSnapshot(t *testing.T) {
	channel := &model.Channel{Group: "current-group"}
	task := &model.Task{
		Group: "task-row-group",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: &model.TaskRouteSnapshot{Group: "snapshot-group"},
		},
	}

	require.Equal(t, "snapshot-group", ResolveTaskHealthGroup(task, channel))
}

func TestMergeTaskProgressIsMonotonicAndTerminalAware(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		incoming string
		terminal bool
		expected string
	}{
		{name: "stale response", current: "60%", incoming: "30%", expected: "60%"},
		{name: "empty response", current: "40%", incoming: "", expected: "40%"},
		{name: "new progress", current: "20%", incoming: "75%", expected: "75%"},
		{name: "non terminal complete is capped", current: "20%", incoming: "100%", expected: "99%"},
		{name: "terminal success", current: "80%", incoming: "10%", terminal: true, expected: "100%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, MergeTaskProgress(tt.current, tt.incoming, tt.terminal))
		})
	}
}
