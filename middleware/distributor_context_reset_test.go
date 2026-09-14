package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetupContextForSelectedChannelClearsPreviousProviderContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("api_version", "2024-01-01")
	ctx.Set("region", "us-central1")
	ctx.Set("plugin", "old-plugin")
	ctx.Set("bot_id", "old-bot")
	ctx.Set("channel_organization", "old-organization")
	ctx.Set("model_mapping", `{"old-model":"old-provider"}`)
	ctx.Set("request_model", "old-request-model")
	common.SetContextKey(ctx, constant.ContextKeyChannelOrganization, "old-organization")
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"old-model":"old-provider"}`)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteScope, "old-route-scope")
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteWeightBPS, uint(4200))
	common.SetContextKey(ctx, constant.ContextKeyChannelRoutePriority, int64(7))
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteDiagnostics, dto.ChannelRouteSelectionDiagnostics{
		Group:               "default",
		RequestedModel:      "fresh-model",
		EffectiveRouteModel: "fresh-provider-model",
		RouteScope:          "old-route-scope",
		RoutePriority:       7,
		RouteWeightBPS:      4200,
	})

	channel := &model.Channel{
		Id:     902,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "fresh-openai",
		Key:    "fresh-key",
		Status: common.ChannelStatusEnabled,
		Models: "fresh-model",
		Group:  "default",
	}
	setupErr := SetupContextForSelectedChannel(ctx, channel, "fresh-model")
	require.Nil(t, setupErr)

	require.Empty(t, ctx.GetString("api_version"))
	require.Empty(t, ctx.GetString("region"))
	require.Empty(t, ctx.GetString("plugin"))
	require.Empty(t, ctx.GetString("bot_id"))
	require.Empty(t, ctx.GetString("channel_organization"))
	require.Empty(t, ctx.GetString("model_mapping"))
	require.Empty(t, ctx.GetString("request_model"))
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyChannelOrganization))
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyChannelModelMapping))
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyChannelRouteScope))
	routeWeightBPS, ok := common.GetContextKeyType[uint](ctx, constant.ContextKeyChannelRouteWeightBPS)
	require.True(t, ok)
	require.Equal(t, uint(0), routeWeightBPS)
	require.Equal(t, int64(0), common.GetContextKeyInt64(ctx, constant.ContextKeyChannelRoutePriority))
	routeDiagnostics, ok := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](ctx, constant.ContextKeyChannelRouteDiagnostics)
	require.True(t, ok)
	require.Empty(t, routeDiagnostics.EffectiveRouteModel)
	require.Equal(t, channel.Id, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
	require.Equal(t, channel.Type, common.GetContextKeyInt(ctx, constant.ContextKeyChannelType))
}

func TestSetupContextForSelectedChannelPreservesMatchingRouteDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	channel := &model.Channel{
		Id:     903,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "same-route-channel",
		Key:    "same-route-key",
		Status: common.ChannelStatusEnabled,
		Models: "fresh-model",
		Group:  "default",
	}
	diagnostics := dto.ChannelRouteSelectionDiagnostics{
		Group:                  "default",
		RequestedModel:         "fresh-model",
		SelectedCandidateModel: "fresh-model",
		EffectiveRouteModel:    "provider/fresh-model",
		SelectedChannelID:      channel.Id,
		RouteScope:             "default/provider/fresh-model/10",
		RoutePriority:          10,
		RouteWeightBPS:         10000,
	}
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteDiagnostics, diagnostics)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteScope, diagnostics.RouteScope)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteWeightBPS, diagnostics.RouteWeightBPS)
	common.SetContextKey(ctx, constant.ContextKeyChannelRoutePriority, diagnostics.RoutePriority)

	require.Nil(t, SetupContextForSelectedChannel(ctx, channel, "fresh-model"))

	got, ok := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](ctx, constant.ContextKeyChannelRouteDiagnostics)
	require.True(t, ok)
	require.Equal(t, diagnostics, got)
	require.Equal(t, diagnostics.RouteScope, common.GetContextKeyString(ctx, constant.ContextKeyChannelRouteScope))
	weight, ok := common.GetContextKeyType[uint](ctx, constant.ContextKeyChannelRouteWeightBPS)
	require.True(t, ok)
	require.Equal(t, diagnostics.RouteWeightBPS, weight)
	require.Equal(t, diagnostics.RoutePriority, common.GetContextKeyInt64(ctx, constant.ContextKeyChannelRoutePriority))
}

func TestSetupContextForSelectedChannelWithEffectiveModelUsesPersistedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	mapping := `{"origin-model":"new-provider-model"}`
	channel := &model.Channel{
		Id:           904,
		Type:         constant.ChannelTypeOpenAI,
		Name:         "snapshot-channel",
		Key:          "snapshot-key",
		Status:       common.ChannelStatusEnabled,
		Models:       "origin-model",
		ModelMapping: &mapping,
		Group:        "default",
	}

	require.Nil(t, SetupContextForSelectedChannelWithEffectiveModel(ctx, channel, "origin-model", "old-provider-model"))
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyChannelModelMapping))
	route, err := service.BuildChannelHealthRouteForChannel(ctx, "default", "origin-model", channel)
	require.NoError(t, err)
	require.Equal(t, "old-provider-model", route.EffectiveUpstreamModel)
}

func newCurrentTaskRouteSnapshotForMiddlewareTest(t *testing.T, channelID int, multiKeyIndex int) *model.TaskRouteSnapshot {
	t.Helper()
	base := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "client-model",
		EffectiveUpstreamModel: "provider-model",
		FinalProtocol:          "openai",
		Operation:              "task.openai.generate",
		UpstreamPath:           "/v1/tasks",
		ChannelID:              channelID,
		MultiKeyIndex:          multiKeyIndex,
		CredentialScoped:       true,
	}.Normalized()
	poll := base
	poll.Operation = base.Operation + ".poll"
	poll.UpstreamPath = "/v1/tasks/status"
	poll.Fingerprint = ""
	poll = poll.Normalized()
	content := base
	content.Operation = base.Operation + ".content"
	content.UpstreamPath = "/v1/tasks/content"
	content.Fingerprint = ""
	content = content.Normalized()
	headerPolicy, err := model.EncryptTaskRouteHeaderPolicy(nil)
	require.NoError(t, err)
	proxyCiphertext, err := model.EncryptTaskRouteProxy("")
	require.NoError(t, err)
	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:           model.TaskRouteSnapshotSchemaVersion,
		Group:                   "default",
		BaseURL:                 "https://snapshot.example.test",
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
		RouteScopeVersion:       "v1:test",
		ChannelID:               channelID,
		ChannelType:             constant.ChannelTypeOpenAI,
		AdapterPlatform:         "1",
		AdapterIdentity:         "task-adapter/v1/1/1",
		ProviderRuntime:         &model.TaskRouteProviderSnapshot{},
		MultiKeyIndex:           multiKeyIndex,
		CredentialScoped:        true,
		HeaderPolicyCaptured:    true,
		HeaderPolicyCiphertext:  headerPolicy,
		HeaderPolicyFingerprint: model.ComputeTaskRouteHeaderPolicyFingerprint(nil),
		ProxyCaptured:           true,
		ProxyCiphertext:         proxyCiphertext,
		ProxyFingerprint:        model.ComputeTaskRouteProxyFingerprint(""),
	}
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	return snapshot
}

func TestSetupContextForSelectedChannelWithRouteSnapshotUsesPersistedMultiKeyIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/status", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	baseURL := "https://current.example.test"
	channel := &model.Channel{
		Id:      905,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "snapshot-multi-key",
		Key:     "key-a\nkey-b",
		Keys:    []string{"key-a", "key-b"},
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:           true,
			MultiKeyPollingIndex: 0,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusEnabled,
				1: common.ChannelStatusEnabled,
			},
		},
	}
	snapshot := newCurrentTaskRouteSnapshotForMiddlewareTest(t, channel.Id, 1)

	require.Nil(t, SetupContextForSelectedChannelWithRouteSnapshot(ctx, channel, snapshot.RequestedModel, snapshot))
	require.Equal(t, "key-b", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	require.Equal(t, 1, common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))
	require.Equal(t, "https://snapshot.example.test", common.GetContextKeyString(ctx, constant.ContextKeyChannelBaseUrl))
	require.Equal(t, 0, channel.ChannelInfo.MultiKeyPollingIndex)
}

func TestSetupContextForSelectedChannelWithRouteSnapshotUsesPersistedSingleKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/status", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	channel := &model.Channel{
		Id:     9051,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "snapshot-single-key",
		Key:    "original-key",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: false,
		},
	}
	snapshot := newCurrentTaskRouteSnapshotForMiddlewareTest(t, channel.Id, 0)
	snapshot.CredentialScoped = false
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	require.Nil(t, SetupContextForSelectedChannelWithRouteSnapshotAndCredential(ctx, channel, snapshot.RequestedModel, snapshot, "original-key"))
	require.Equal(t, "original-key", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	require.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey))

	channel.Key = "rotated-key"
	ctx, _ = gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/status", nil)
	setupErr := SetupContextForSelectedChannelWithRouteSnapshotAndCredential(ctx, channel, snapshot.RequestedModel, snapshot, "original-key")
	require.Error(t, setupErr)
	require.Equal(t, types.ErrorCodeChannelNoAvailableKey, setupErr.GetErrorCode())
	require.Contains(t, setupErr.Error(), "no longer available")
}

func TestSetupContextForSelectedChannelWithRouteSnapshotUsesProviderRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/status", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	channel := &model.Channel{
		Id:     909,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "live-channel",
		Key:    "key-a",
		Status: common.ChannelStatusEnabled,
		Other:  "live-other",
		Keys:   []string{"key-a"},
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: false,
		},
	}
	channel.SetSetting(dto.ChannelSettings{ForceFormat: false, Proxy: "http://live-proxy.example.test:8080"})
	channel.SetOtherSettings(dto.ChannelOtherSettings{AzureResponsesVersion: "live-version"})
	liveStatusMapping := `{"500":503}`
	channel.StatusCodeMapping = &liveStatusMapping
	liveOrganization := "live-organization"
	channel.OpenAIOrganization = &liveOrganization

	snapshot := newCurrentTaskRouteSnapshotForMiddlewareTest(t, channel.Id, 0)
	snapshot.CredentialScoped = false
	snapshot.ProviderRuntime = &model.TaskRouteProviderSnapshot{
		Other:                             "snapshot-other",
		StatusCodeMapping:                 `{"504":429}`,
		OpenAIOrganization:                "snapshot-organization",
		ChannelSettings:                   dto.ChannelSettings{ForceFormat: true},
		ChannelOtherSettings:              dto.ChannelOtherSettings{AzureResponsesVersion: "snapshot-version"},
		ResponsesBootstrapRecoveryEnabled: true,
	}
	snapshot.ProxyCiphertext, _ = model.EncryptTaskRouteProxy("http://snapshot-proxy.example.test:8080")
	snapshot.ProxyFingerprint = model.ComputeTaskRouteProxyFingerprint("http://snapshot-proxy.example.test:8080")
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	require.Nil(t, SetupContextForSelectedChannelWithRouteSnapshot(ctx, channel, snapshot.RequestedModel, snapshot))
	settings, ok := common.GetContextKeyType[dto.ChannelSettings](ctx, constant.ContextKeyChannelSetting)
	require.True(t, ok)
	require.True(t, settings.ForceFormat)
	require.Equal(t, "http://snapshot-proxy.example.test:8080", settings.Proxy)
	otherSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](ctx, constant.ContextKeyChannelOtherSetting)
	require.True(t, ok)
	require.Equal(t, "snapshot-version", otherSettings.AzureResponsesVersion)
	require.Equal(t, "snapshot-other", ctx.GetString("channel_other"))
	require.Equal(t, "snapshot-organization", common.GetContextKeyString(ctx, constant.ContextKeyChannelOrganization))
	require.Equal(t, `{"504":429}`, common.GetContextKeyString(ctx, constant.ContextKeyChannelStatusCodeMapping))
	require.True(t, model.IsResponsesBootstrapRecoveryEnabledInOtherSettings(common.GetContextKeyString(ctx, constant.ContextKeyChannelOtherSettingRaw)))
}

func TestSetupContextForSelectedChannelWithSchema5SnapshotReplaysCapturedHeaderPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/status", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	channel := &model.Channel{
		Id:     9091,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "schema5-channel",
		Key:    "schema5-key",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: false,
		},
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{HeaderProfileStrategy: &dto.HeaderProfileStrategy{
		Enabled:            true,
		Mode:               dto.HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"live-profile"},
		Profiles:           []dto.HeaderProfile{{ID: "live-profile", Headers: map[string]string{"X-Captured": "live-value"}}},
	}})

	snapshot := newCurrentTaskRouteSnapshotForMiddlewareTest(t, channel.Id, 0)
	snapshot.SchemaVersion = model.TaskRouteSnapshotHeaderPolicySchemaVersion
	snapshot.ProviderRuntime = nil
	snapshot.CredentialScoped = false
	headerPolicy, err := model.EncryptTaskRouteHeaderPolicy(map[string]string{"X-Captured": "submitted-value"})
	require.NoError(t, err)
	snapshot.HeaderPolicyCiphertext = headerPolicy
	snapshot.HeaderPolicyFingerprint = model.ComputeTaskRouteHeaderPolicyFingerprint(map[string]string{"X-Captured": "submitted-value"})
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	require.Nil(t, SetupContextForSelectedChannelWithRouteSnapshot(ctx, channel, snapshot.RequestedModel, snapshot))
	require.Equal(t, "submitted-value", common.GetContextKeyStringMap(ctx, constant.ContextKeyChannelHeaderOverride)["X-Captured"])
	audit, ok := common.GetContextKeyType[service.RuntimeHeaderPolicyAudit](ctx, constant.ContextKeyChannelHeaderPolicyAudit)
	require.True(t, ok)
	require.Equal(t, "task_snapshot", audit.HeaderPolicyMode)
}

func TestSetupContextForSelectedChannelWithRouteSnapshotRejectsCredentialDrift(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/status", nil)
	channel := &model.Channel{
		Id:     906,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "key-a\nkey-b",
		Keys:   []string{"key-a", "key-b"},
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusEnabled,
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}
	snapshot := newCurrentTaskRouteSnapshotForMiddlewareTest(t, channel.Id, 1)

	setupErr := SetupContextForSelectedChannelWithRouteSnapshot(ctx, channel, snapshot.RequestedModel, snapshot)
	require.Error(t, setupErr)
	require.Equal(t, types.ErrorCodeChannelNoAvailableKey, setupErr.GetErrorCode())
	require.Contains(t, setupErr.Error(), "disabled")
}

func TestSetupContextForSelectedChannelWithRouteSnapshotRejectsChannelIdentityDrift(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/status", nil)

	channel := &model.Channel{
		Id:     9061,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "key-a",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: false,
		},
	}
	snapshot := newCurrentTaskRouteSnapshotForMiddlewareTest(t, channel.Id, 0)
	snapshot.CredentialScoped = false
	snapshot.MultiKeyIndex = 0
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	// Reusing the row ID for another provider must fail before any channel
	// context is installed. Keep the snapshot integrity digest valid so this
	// exercises the identity guard rather than the generic snapshot validator.
	snapshot.ChannelType = constant.ChannelTypeSora
	snapshot.AdapterPlatform = "55"
	snapshot.AdapterIdentity = "task-adapter/v1/55/55"
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	setupErr := SetupContextForSelectedChannelWithRouteSnapshot(ctx, channel, snapshot.RequestedModel, snapshot)
	require.Error(t, setupErr)
	require.Equal(t, types.ErrorCodeGetChannelFailed, setupErr.GetErrorCode())
	require.Contains(t, setupErr.Error(), "channel type mismatch")
	require.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
}

func TestSetupContextForSelectedChannelWithRouteSnapshotRejectsHeaderPolicyFingerprintDrift(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/status", nil)

	channel := &model.Channel{
		Id:     907,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "key-a\nkey-b",
		Keys:   []string{"key-a", "key-b"},
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusEnabled,
				1: common.ChannelStatusEnabled,
			},
		},
	}
	snapshot := newCurrentTaskRouteSnapshotForMiddlewareTest(t, channel.Id, 1)
	replacement, err := model.EncryptTaskRouteHeaderPolicy(map[string]string{"X-Injected": "replacement"})
	require.NoError(t, err)
	// Ciphertext is intentionally excluded from SnapshotFingerprint because its
	// nonce is randomized. The middleware must therefore authenticate the
	// decrypted policy against HeaderPolicyFingerprint before installing it.
	snapshot.HeaderPolicyCiphertext = replacement
	require.Equal(t, snapshot.SnapshotFingerprint, snapshot.ComputeFingerprint())

	setupErr := SetupContextForSelectedChannelWithRouteSnapshot(ctx, channel, snapshot.RequestedModel, snapshot)
	require.Error(t, setupErr)
	require.Equal(t, types.ErrorCodeChannelHeaderOverrideInvalid, setupErr.GetErrorCode())
	require.Contains(t, setupErr.Error(), "header policy fingerprint mismatch")
}

func TestSetupContextForSelectedChannelWithRouteSnapshotIgnoresLiveParamOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/status", nil)

	liveOverride := `{"operations":[{"mode":"set_header","path":"X-Live-Header","value":"must-not-be-read"}]}`
	channel := &model.Channel{
		Id:            908,
		Type:          constant.ChannelTypeOpenAI,
		Name:          "snapshot-param-override",
		Key:           "snapshot-key",
		ParamOverride: &liveOverride,
		Status:        common.ChannelStatusEnabled,
	}
	snapshot := newCurrentTaskRouteSnapshotForMiddlewareTest(t, channel.Id, 0)
	snapshot.CredentialScoped = false
	snapshot.MultiKeyIndex = 0
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	require.Nil(t, SetupContextForSelectedChannelWithRouteSnapshot(ctx, channel, snapshot.RequestedModel, snapshot))
	require.Empty(t, common.GetContextKeyStringMap(ctx, constant.ContextKeyChannelParamOverride))
	require.Empty(t, common.GetContextKeyStringMap(ctx, constant.ContextKeyChannelHeaderOverride))
}

func TestAbortWithSelectedChannelSetupErrorReturnsExplicitStatusAndRedactsCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "sk-test-secret")

	abortWithSelectedChannelSetupError(ctx, types.NewErrorWithStatusCode(
		errors.New("invalid header profile for sk-test-secret"),
		types.ErrorCodeChannelHeaderOverrideInvalid,
		http.StatusBadRequest,
	))

	require.True(t, ctx.IsAborted())
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), string(types.ErrorCodeChannelHeaderOverrideInvalid))
	require.NotContains(t, recorder.Body.String(), "sk-test-secret")
}

func TestDistributeSkipsChannelSetupForUnselectedTaskFetches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "midjourney task fetch", method: http.MethodGet, path: "/mj/task/mj-task/fetch"},
		{name: "midjourney image seed", method: http.MethodGet, path: "/mj/task/mj-task/image-seed"},
		{name: "suno fetch", method: http.MethodPost, path: "/suno/fetch"},
		{name: "suno fetch by id", method: http.MethodGet, path: "/suno/fetch/suno-task"},
		{name: "video fetch", method: http.MethodGet, path: "/v1/videos/video-task"},
		{name: "legacy video fetch", method: http.MethodGet, path: "/v1/video/generations/video-task"},
		{name: "video remix", method: http.MethodPost, path: "/v1/videos/video-task/remix"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router := gin.New()
			router.Any("/*path", Distribute(), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(tt.method, tt.path, nil)
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}

func TestDistributeRejectsMissingPinnedChannelForUnselectedTaskFetches(t *testing.T) {
	setupDistributorBootstrapTestDB(t)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		// A missing explicit pin must fail even for a no-select follow-up.
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, "999999")
		c.Next()
	})
	router.Any("/*path", Distribute(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/videos/video-task", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestDistributeRejectsDisabledPinnedChannelForUnselectedTaskFetches(t *testing.T) {
	db := setupDistributorBootstrapTestDB(t)
	_, err := seedDistributorBootstrapChannel(db, 1201, "video-model")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, "1201")
		c.Next()
	})
	router.Any("/*path", Distribute(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/videos/video-task", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestDistributeInitializesValidPinnedChannelForUnselectedTaskFetches(t *testing.T) {
	db := setupDistributorBootstrapTestDB(t)
	channel := &model.Channel{
		Id:     1202,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Name:   "valid-pinned-task-follow-up",
		Models: "video-model",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, "1202")
		c.Next()
	})
	router.Any("/*path", Distribute(), func(c *gin.Context) {
		require.Equal(t, channel.Id, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
		require.Equal(t, "test-key", common.GetContextKeyString(c, constant.ContextKeyChannelKey))
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/videos/video-task", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestDistributeTokenModelLimitAllowsModelLessTaskQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.Any("/*path", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"gpt-5.5": true})
		c.Next()
	}, Distribute(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/videos/video-task", nil)
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestDistributeTokenModelLimitRejectsDeniedSubmitModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.Any("/*path", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"gpt-5.5": true})
		c.Next()
	}, Distribute(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"claude-opus-4","messages":[]}`))
	req.Header.Set("Content-Type", gin.MIMEJSON)
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}
