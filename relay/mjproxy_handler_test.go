package relay

import (
	"io"
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
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyResponseSucceededRequiresAcceptedProviderCode(t *testing.T) {
	tests := []struct {
		name     string
		response *dto.MidjourneyResponseWithStatusCode
		want     bool
	}{
		{name: "nil", response: nil, want: false},
		{name: "success", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusOK, Response: dto.MidjourneyResponse{Code: 1}}, want: true},
		{name: "existing", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusOK, Response: dto.MidjourneyResponse{Code: 21}}, want: true},
		{name: "queued", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusOK, Response: dto.MidjourneyResponse{Code: 22}}, want: true},
		{name: "provider error with http 200", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusOK, Response: dto.MidjourneyResponse{Code: 4}}, want: false},
		{name: "http error", response: &dto.MidjourneyResponseWithStatusCode{StatusCode: http.StatusBadGateway, Response: dto.MidjourneyResponse{Code: 1}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, midjourneyResponseSucceeded(tt.response))
		})
	}
}

func TestEnsureMidjourneyHealthSelectionRequiresBoundRoute(t *testing.T) {
	response := ensureMidjourneyHealthSelection(nil, nil, "default", "midjourney")

	require.NotNil(t, response)
	require.Equal(t, 31, response.Code)
	require.Equal(t, "channel_health_route_unavailable", response.Description)
}

func TestMaybeAutoDisableMidjourneyChannelIgnoresMissingChannel(t *testing.T) {
	previous := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = previous })

	require.NotPanics(t, func() {
		maybeAutoDisableMidjourneyChannel(901, nil)
	})
}

func TestRespondMidjourneyImageUpstreamErrorDoesNotExposeBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/mj/image/mj-secret", nil)
	responseBody := `{"error":"upstream-secret","token":"sk-live-secret"}`
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}

	respondMidjourneyImageUpstreamError(c, resp)

	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "upstream-secret")
	require.NotContains(t, recorder.Body.String(), "sk-live-secret")
	require.Contains(t, recorder.Body.String(), "upstream_image_fetch_failed")
}

func TestBindMidjourneyFollowupRouteUsesPersistedSnapshot(t *testing.T) {
	base := "https://submit.example.test/"
	channel := &model.Channel{Id: 812, Type: constant.ChannelTypeMidjourney, Key: "key-a", BaseURL: &base, Status: common.ChannelStatusEnabled, Group: "default"}
	baseRoute := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "mj_imagine",
		EffectiveUpstreamModel: "provider-mj",
		FinalProtocol:          "openai",
		Operation:              "relay.mj.imagine",
		UpstreamPath:           "/mj/submit/imagine",
		ChannelID:              channel.Id,
	}.Normalized()
	task := &model.Midjourney{
		MjId:      "mj-followup",
		Action:    constant.MjActionImagine,
		ChannelId: channel.Id,
		PrivateData: model.TaskPrivateData{Key: "key-a", RouteSnapshot: &model.TaskRouteSnapshot{
			SchemaVersion:           2,
			BaseURL:                 base,
			RequestedModel:          baseRoute.RequestedModel,
			EffectiveUpstreamModel:  baseRoute.EffectiveUpstreamModel,
			Protocol:                baseRoute.FinalProtocol,
			Operation:               baseRoute.Operation,
			SubmitPath:              baseRoute.UpstreamPath,
			PollingPath:             "/mj/task/list-by-condition",
			ContentPath:             "/mj/task/list-by-condition",
			RouteFingerprint:        baseRoute.Fingerprint,
			PollingRouteFingerprint: baseRoute.Fingerprint,
			ContentRouteFingerprint: baseRoute.Fingerprint,
			ChannelID:               channel.Id,
		}},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/change", nil)
	info := &relaycommon.RelayInfo{UsingGroup: "default", RelayMode: relayconstant.RelayModeMidjourneyChange, OriginModelName: "mj_upscale"}
	require.NoError(t, bindMidjourneyFollowupRoute(c, info, channel, task, "mj_upscale", "followup_upscale", "/mj/submit/change"))
	require.Equal(t, base[:len(base)-1], common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl))
	require.Equal(t, "key-a", common.GetContextKeyString(c, constant.ContextKeyChannelKey))
	require.Equal(t, "provider-mj", info.ChannelHealthRoute.EffectiveUpstreamModel)
	require.Contains(t, info.ChannelHealthRoute.Operation, "followup_upscale")
}

func TestBindMidjourneyFollowupRouteIgnoresCurrentHeaderPolicyForCurrentSnapshot(t *testing.T) {
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "midjourney-followup-route-test-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })

	base := "https://submit.example.test/"
	channel := &model.Channel{
		Id:            814,
		Type:          constant.ChannelTypeMidjourney,
		Key:           "key-a",
		BaseURL:       &base,
		Status:        common.ChannelStatusEnabled,
		Group:         "default",
		ModelMapping:  common.GetPointer(`{"mj_imagine":"provider-current"}`),
		OtherSettings: `{"header_policy_mode":"invalid"}`,
	}

	baseRoute := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "mj_imagine",
		EffectiveUpstreamModel: "provider-frozen",
		FinalProtocol:          "openai",
		Operation:              "task.midjourney.imagine",
		UpstreamPath:           "/mj/submit/imagine",
		ChannelID:              channel.Id,
	}.Normalized()
	pollRoute := baseRoute
	pollRoute.Operation = baseRoute.Operation + ".poll"
	pollRoute.UpstreamPath = "/mj/task/list-by-condition"
	pollRoute.Fingerprint = ""
	pollRoute = pollRoute.Normalized()
	contentRoute := baseRoute
	contentRoute.Operation = baseRoute.Operation + ".content"
	contentRoute.UpstreamPath = "/mj/task/list-by-condition"
	contentRoute.Fingerprint = ""
	contentRoute = contentRoute.Normalized()
	headerPolicy, err := model.EncryptTaskRouteHeaderPolicy(map[string]string{"X-Snapshot-Header": "frozen"})
	require.NoError(t, err)
	proxyCiphertext, err := model.EncryptTaskRouteProxy("")
	require.NoError(t, err)
	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:           model.TaskRouteSnapshotSchemaVersion,
		Group:                   "default",
		BaseURL:                 base,
		RequestedModel:          baseRoute.RequestedModel,
		EffectiveUpstreamModel:  baseRoute.EffectiveUpstreamModel,
		Protocol:                baseRoute.FinalProtocol,
		Operation:               baseRoute.Operation,
		PollingOperation:        pollRoute.Operation,
		ContentOperation:        contentRoute.Operation,
		SubmitPath:              baseRoute.UpstreamPath,
		PollingPath:             pollRoute.UpstreamPath,
		ContentPath:             contentRoute.UpstreamPath,
		RouteFingerprint:        baseRoute.Fingerprint,
		PollingRouteFingerprint: pollRoute.Fingerprint,
		ContentRouteFingerprint: contentRoute.Fingerprint,
		RouteScopeVersion:       "v1:test",
		ChannelID:               channel.Id,
		ChannelType:             channel.Type,
		AdapterPlatform:         string(constant.TaskPlatformMidjourney),
		AdapterIdentity:         "task-adapter/v1/2/mj",
		ProviderRuntime:         &model.TaskRouteProviderSnapshot{},
		HeaderPolicyCaptured:    true,
		HeaderPolicyCiphertext:  headerPolicy,
		HeaderPolicyFingerprint: model.ComputeTaskRouteHeaderPolicyFingerprint(map[string]string{"X-Snapshot-Header": "frozen"}),
		ProxyCaptured:           true,
		ProxyCiphertext:         proxyCiphertext,
		ProxyFingerprint:        model.ComputeTaskRouteProxyFingerprint(""),
	}
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	task := &model.Midjourney{
		MjId:        "mj-followup-current-policy-drift",
		Action:      constant.MjActionImagine,
		ChannelId:   channel.Id,
		PrivateData: model.TaskPrivateData{Key: "key-a", RouteSnapshot: snapshot},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/change", nil)
	info := &relaycommon.RelayInfo{UsingGroup: "default", RelayMode: relayconstant.RelayModeMidjourneyChange, OriginModelName: "mj_upscale"}

	require.NoError(t, bindMidjourneyFollowupRoute(c, info, channel, task, "mj_upscale", "followup_upscale", "/mj/submit/change"))
	require.Equal(t, "frozen", common.GetContextKeyStringMap(c, constant.ContextKeyChannelHeaderOverride)["X-Snapshot-Header"])
	otherSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	require.True(t, ok)
	require.Nil(t, otherSettings.HeaderProfileStrategy)
	require.Empty(t, common.GetContextKeyString(c, constant.ContextKeyChannelModelMapping))
	require.Equal(t, "provider-frozen", info.ChannelHealthRoute.EffectiveUpstreamModel)
	serviceRoute, err := service.TaskRouteIdentityFromSnapshot(snapshot, "submit", channel)
	require.NoError(t, err)
	require.Equal(t, serviceRoute.EffectiveUpstreamModel, info.ChannelHealthRoute.EffectiveUpstreamModel)
}

func TestBindMidjourneyFollowupRouteSchema5ReplaysCapturedHeaderPolicy(t *testing.T) {
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "midjourney-followup-schema5-header-policy-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })

	base := "https://submit.example.test/"
	channel := &model.Channel{
		Id:            815,
		Type:          constant.ChannelTypeMidjourney,
		Key:           "key-a",
		BaseURL:       &base,
		Status:        common.ChannelStatusEnabled,
		Group:         "default",
		OtherSettings: `{"header_profile_strategy":{"enabled":true,"mode":"fixed","selected_profile_ids":["captured-profile"],"profiles":[{"id":"captured-profile","headers":{"X-Snapshot-Header":"mutated","X-Current-Only":"live"}}]}}`,
	}
	baseRoute := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "mj_imagine",
		EffectiveUpstreamModel: "provider-frozen",
		FinalProtocol:          "openai",
		Operation:              "task.midjourney.imagine",
		UpstreamPath:           "/mj/submit/imagine",
		ChannelID:              channel.Id,
	}.Normalized()
	headerPolicy := map[string]string{"X-Snapshot-Header": "frozen"}
	headerCiphertext, err := model.EncryptTaskRouteHeaderPolicy(headerPolicy)
	require.NoError(t, err)
	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:           model.TaskRouteSnapshotHeaderPolicySchemaVersion,
		Group:                   "default",
		BaseURL:                 base,
		RequestedModel:          baseRoute.RequestedModel,
		EffectiveUpstreamModel:  baseRoute.EffectiveUpstreamModel,
		Protocol:                baseRoute.FinalProtocol,
		Operation:               baseRoute.Operation,
		PollingOperation:        baseRoute.Operation + ".poll",
		ContentOperation:        baseRoute.Operation + ".content",
		SubmitPath:              baseRoute.UpstreamPath,
		PollingPath:             "/mj/task/list-by-condition",
		ContentPath:             "/mj/task/list-by-condition",
		RouteFingerprint:        baseRoute.Fingerprint,
		PollingRouteFingerprint: baseRoute.Fingerprint,
		ContentRouteFingerprint: baseRoute.Fingerprint,
		ChannelID:               channel.Id,
		HeaderProfileID:         "captured-profile",
		HeaderProfileFingerprint: model.ComputeTaskRouteHeaderProfileFingerprint(map[string]string{
			"X-Snapshot-Header": "original",
		}),
		HeaderPolicyCaptured:    true,
		HeaderPolicyCiphertext:  headerCiphertext,
		HeaderPolicyFingerprint: model.ComputeTaskRouteHeaderPolicyFingerprint(headerPolicy),
	}
	task := &model.Midjourney{
		MjId:      "mj-followup-schema5-header-policy",
		Action:    constant.MjActionImagine,
		ChannelId: channel.Id,
		PrivateData: model.TaskPrivateData{
			Key:           "key-a",
			RouteSnapshot: snapshot,
		},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/change", strings.NewReader(`{"prompt":"test"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		UsingGroup:      "default",
		RelayMode:       relayconstant.RelayModeMidjourneyChange,
		OriginModelName: "mj_upscale",
	}

	require.NoError(t, bindMidjourneyFollowupRoute(c, info, channel, task, "mj_upscale", "followup_upscale", "/mj/submit/change"))
	otherSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	require.True(t, ok)
	require.Nil(t, otherSettings.HeaderProfileStrategy)
	require.Equal(t, "frozen", common.GetContextKeyStringMap(c, constant.ContextKeyChannelHeaderOverride)["X-Snapshot-Header"])

	replayedHeaders, err := service.BuildTaskRouteSnapshotRequestHeaders(channel, "key-a", nil, snapshot)
	require.NoError(t, err)
	require.Equal(t, "frozen", replayedHeaders.Get("X-Snapshot-Header"))
	require.Empty(t, replayedHeaders.Get("X-Current-Only"))
}

func TestResolveMidjourneyFollowupKeyRejectsCredentialScopeDrift(t *testing.T) {
	base := "https://submit.example.test"
	channel := &model.Channel{
		Id:      813,
		Type:    constant.ChannelTypeMidjourney,
		Key:     "key-a",
		BaseURL: &base,
		Status:  common.ChannelStatusEnabled,
		Group:   "default",
	}
	task := &model.Midjourney{
		MjId:      "mj-scope-drift",
		ChannelId: channel.Id,
		PrivateData: model.TaskPrivateData{
			Key: "key-a",
			RouteSnapshot: &model.TaskRouteSnapshot{
				SchemaVersion:    model.TaskRouteSnapshotSchemaVersion,
				CredentialScoped: true,
				MultiKeyIndex:    0,
			},
		},
	}

	key, err := resolveMidjourneyFollowupKey(channel, task)
	require.Error(t, err)
	require.Empty(t, key)
	require.Contains(t, err.Error(), "requires multi-key channel")
}

func TestCanonicalMidjourneyRequestTargetSupportsModePrefixAndRejectsUnregisteredPaths(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "plain submit", raw: "/mj/submit/change", want: "/mj/submit/change"},
		{name: "mode prefix", raw: "/fast/mj/submit/change?x=1", want: "/mj/submit/change?x=1"},
		{name: "legacy mode prefix", raw: "/mj-fast/mj/submit/change", want: "/mj/submit/change"},
		{name: "task fetch", raw: "/mj/task/task-1/fetch", want: "/mj/task/task-1/fetch"},
		{name: "task image seed", raw: "/slow/mj/task/task-1/image-seed", want: "/mj/task/task-1/image-seed"},
		{name: "trailing slash", raw: "/fast/mj/submit/change/", want: "/mj/submit/change"},
		{name: "task trailing slash", raw: "/mj/task/task-1/fetch/", want: "/mj/task/task-1/fetch"},
		{name: "unknown action", raw: "/mj/submit/unknown", wantErr: "not registered"},
		{name: "empty task id", raw: "/mj/task//fetch", wantErr: "not registered"},
		{name: "traversal", raw: "/mj/task/../fetch", wantErr: "traversal"},
		{name: "absolute URL", raw: "https://evil.example/mj/submit/change", wantErr: "relative"},
		{name: "encoded separator", raw: "/mj/submit%2fchange", wantErr: "encoded separator"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := canonicalMidjourneyRequestTarget(tt.raw)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCanonicalMidjourneyHealthPathUsesStableTaskIdentity(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "/mj/submit/change", want: "/mj/submit/change"},
		{raw: "/mj/task/task-1/fetch", want: "/mj/task/fetch"},
		{raw: "/mj/task/task-1/image-seed", want: "/mj/task/image-seed"},
		{raw: "/mj/task/fetch", want: "/mj/task/fetch"},
		{raw: "/mj/task/image-seed", want: "/mj/task/image-seed"},
	}
	for _, tt := range tests {
		got, err := canonicalMidjourneyHealthPath(tt.raw)
		require.NoError(t, err, tt.raw)
		require.Equal(t, tt.want, got, tt.raw)
	}
	for _, raw := range []string{"https://evil.example/mj/submit/change", "/mj/task/../fetch"} {
		_, err := canonicalMidjourneyHealthPath(raw)
		require.Error(t, err, raw)
	}
	got, err := canonicalMidjourneyHealthPath("/mj/submit/change?bad=1")
	require.NoError(t, err)
	require.Equal(t, "/mj/submit/change", got)
}
