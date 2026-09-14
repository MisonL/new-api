package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func enableChannelHealthForControllerTest(t *testing.T) {
	t.Helper()
	settings := operation_setting.GetGeneralSetting()
	previousEnabled := settings.ChannelHealthEnabled
	settings.ChannelHealthEnabled = true
	t.Cleanup(func() {
		settings.ChannelHealthEnabled = previousEnabled
	})
}

func useChannelHealthRedisForControllerTest(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	enableChannelHealthForControllerTest(t)
	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})
	return server
}

func observeControllerGatewayThreshold(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) {
	for i := 0; i < 3; i++ {
		service.ObserveChannelHealthFailure(c, info, err)
	}
}

func TestGetChannelInitialRequestFallsBackWhenDistributedChannelIsInHealthCooldown(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	initial, fallback := seedRateLimitRetryChannels(t, db)
	useChannelHealthRedisForControllerTest(t)

	cooldownCtx, _ := gin.CreateTestContext(nil)
	observeControllerGatewayThreshold(cooldownCtx, &relaycommon.RelayInfo{
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: initial.Id},
	}, types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway))

	ctx, _ := gin.CreateTestContext(nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, initial, "gpt-5.5"))
	info := &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		OriginModelName: "gpt-5.5",
	}
	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, fallback.Id, channel.Id)
	require.Equal(t, fallback.Id, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, fallback.Id, info.ChannelMeta.ChannelId)
}

func TestGetChannelInitialConcurrencyExhaustionReturns429(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	priority := int64(10)
	weight := uint(1)
	initial := &model.Channel{
		Id:             90511,
		Name:           "initial-concurrency-exhausted",
		Key:            "sk-initial-concurrency-exhausted",
		Type:           constant.ChannelTypeOpenAI,
		Status:         common.ChannelStatusEnabled,
		Group:          "default",
		Models:         "gpt-5.5",
		Priority:       &priority,
		Weight:         &weight,
		MaxConcurrency: 1,
	}
	require.NoError(t, db.Create(initial).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: initial.Id,
		Enabled:   true,
		Priority:  initial.Priority,
		Weight:    *initial.Weight,
	}).Error)
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "false")
	occupied, err := service.AcquireChannelConcurrency(nil, initial)
	require.NoError(t, err)
	require.NotNil(t, occupied)
	t.Cleanup(occupied.Release)

	ctx, _ := gin.CreateTestContext(nil)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, initial, "gpt-5.5"))
	info := &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		OriginModelName: "gpt-5.5",
	}

	channel, apiErr := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeChannelConcurrencyExceeded, apiErr.GetErrorCode())
}

func TestRelayTaskInitialConcurrencyExhaustionReturns429(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	priority := int64(10)
	weight := uint(1)
	initial := &model.Channel{
		Id:             90512,
		Name:           "task-initial-concurrency-exhausted",
		Key:            "sk-task-initial-concurrency-exhausted",
		Type:           constant.ChannelTypeOpenAI,
		Status:         common.ChannelStatusEnabled,
		Group:          "default",
		Models:         "sora-2",
		Priority:       &priority,
		Weight:         &weight,
		MaxConcurrency: 1,
	}
	require.NoError(t, db.Create(initial).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "sora-2",
		ChannelId: initial.Id,
		Enabled:   true,
		Priority:  initial.Priority,
		Weight:    *initial.Weight,
	}).Error)
	t.Setenv("CHANNEL_CONCURRENCY_STRICT", "false")
	occupied, err := service.AcquireChannelConcurrency(nil, initial)
	require.NoError(t, err)
	require.NotNil(t, occupied)
	t.Cleanup(occupied.Release)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/videos",
		strings.NewReader(`{"model":"sora-2","prompt":"test"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("relay_mode", relayconstant.RelayModeVideoSubmit)
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "sora-2")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserId, 1)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, initial, "sora-2"))

	RelayTask(ctx)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	var response dto.TaskError
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, string(types.ErrorCodeChannelConcurrencyExceeded), response.Code)
}

func TestGetChannelInitialAffinityMultiKeySkipsCooledCredential(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	useChannelHealthRedisForControllerTest(t)

	priority := int64(10)
	weight := uint(1)
	preferred := &model.Channel{
		Id:       206,
		Name:     "affinity-multi-key",
		Key:      "key-a\nkey-b",
		Type:     constant.ChannelTypeOpenAI,
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &priority,
		Weight:   &weight,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyStatusList:   map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled},
			MultiKeyPollingIndex: 0,
		},
	}
	require.NoError(t, db.Create(preferred).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: preferred.Id,
		Enabled:   true,
		Priority:  preferred.Priority,
		Weight:    *preferred.Weight,
	}).Error)

	failureCtx, _ := gin.CreateTestContext(nil)
	failureCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(failureCtx, constant.ContextKeyChannelId, preferred.Id)
	common.SetContextKey(failureCtx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(failureCtx, constant.ContextKeyChannelMultiKeyIndex, 0)
	keyZeroRoute, err := service.BuildChannelHealthRouteForChannel(failureCtx, "default", "gpt-5.5", preferred)
	require.NoError(t, err)
	keyZeroRoute.CredentialScoped = true
	keyZeroRoute.Fingerprint = ""
	keyZeroRoute = keyZeroRoute.Normalized()
	service.ObserveChannelHealthFailure(failureCtx, &relaycommon.RelayInfo{
		UsingGroup:         "default",
		OriginModelName:    "gpt-5.5",
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelId: preferred.Id, ChannelIsMultiKey: true, ChannelMultiKeyIndex: 0},
		ChannelHealthRoute: &keyZeroRoute,
	}, types.NewErrorWithStatusCode(errors.New("auth_not_found: no auth available"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized))

	affinitySetting := operation_setting.GetChannelAffinitySetting()
	originalRules := append([]operation_setting.ChannelAffinityRule(nil), affinitySetting.Rules...)
	affinitySetting.Rules = []operation_setting.ChannelAffinityRule{{
		Name:              "controller-initial-affinity-multi-key",
		ModelRegex:        []string{"^gpt-.*$"},
		PathRegex:         []string{"/v1/responses"},
		KeySources:        []operation_setting.ChannelAffinityKeySource{{Type: "gjson", Path: "prompt_cache_key"}},
		IncludeUsingGroup: true,
		IncludeModelName:  true,
		IncludeRuleName:   true,
	}}
	t.Cleanup(func() { affinitySetting.Rules = originalRules })
	t.Cleanup(func() { _, _ = service.ClearChannelAffinityCacheByRuleName("controller-initial-affinity-multi-key") })

	requestBody := `{"model":"gpt-5.5","prompt_cache_key":"controller-affinity-key"}`
	recordCtx, _ := gin.CreateTestContext(nil)
	recordCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(requestBody))
	common.SetContextKey(recordCtx, constant.ContextKeyUsingGroup, "default")
	_, found := service.GetPreferredChannelByAffinity(recordCtx, "gpt-5.5", "default")
	require.False(t, found)
	recordCtx.Set("channel_id", preferred.Id)
	service.RecordChannelAffinity(recordCtx, preferred.Id)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(requestBody))
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	preferredID, found := service.GetPreferredChannelByAffinity(ctx, "gpt-5.5", "default")
	require.True(t, found)
	require.Equal(t, preferred.Id, preferredID)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, preferred, "gpt-5.5"))

	info := &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		OriginModelName: "gpt-5.5",
	}
	channel, channelErr := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	if channelErr != nil {
		t.Fatalf("getChannel: %v", channelErr)
	}
	require.NotNil(t, channel)
	require.Equal(t, preferred.Id, channel.Id)
	require.Equal(t, 1, common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))
}

func TestGetChannelSpecificRequestBypassesHealthCooldown(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	initial, _ := seedRateLimitRetryChannels(t, db)
	useChannelHealthRedisForControllerTest(t)

	cooldownCtx, _ := gin.CreateTestContext(nil)
	service.ObserveChannelHealthFailure(cooldownCtx, &relaycommon.RelayInfo{
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: initial.Id},
	}, types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway))

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("specific_channel_id", "206")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, initial, "gpt-5.5"))
	info := &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		OriginModelName: "gpt-5.5",
	}
	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, initial.Id, channel.Id)
	require.Nil(t, info.ChannelMeta)
}

func TestGetChannelReleasesHalfOpenProbeWhenResponsesPrecheckReturnsError(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)
	native.Status = common.ChannelStatusManuallyDisabled
	require.NoError(t, db.Save(native).Error)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", native.Id).Update("enabled", false).Error)
	server := useChannelHealthRedisForControllerTest(t)

	failureCtx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(failureCtx, constant.ContextKeyUsingGroup, "default")
	service.ObserveChannelHealthFailure(failureCtx, &relaycommon.RelayInfo{
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: chatOnly.Id},
	}, types.NewOpenAIError(errors.New("upstream gateway timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusGatewayTimeout))
	server.FastForward(service.DefaultChannelHealthCooldown)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicy(chatOnly.Id)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := malformedRemoteCompactionRelayInfo()
	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Contains(t, err.Error(), "missing encrypted_content")
	competingCtx, _ := gin.CreateTestContext(nil)
	decision := service.EvaluateChannelHealthSelection(competingCtx, "default", "gpt-5.5", chatOnly.Id)
	require.True(t, decision.Allow)
	require.Equal(t, service.ChannelHealthSelectionHalfOpenProbe, decision.Action)
}

func TestProcessChannelErrorMovesGatewayFailureIntoHealthCooldown(t *testing.T) {
	useChannelHealthRedisForControllerTest(t)
	previousErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		constant.ErrorLogEnabled = previousErrorLogEnabled
	})

	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	for i := 0; i < 3; i++ {
		processChannelError(
			ctx,
			info,
			types.ChannelError{ChannelId: 206, AutoBan: false},
			types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
		)
	}

	selectionCtx, _ := gin.CreateTestContext(nil)
	decision := service.EvaluateChannelHealthSelection(selectionCtx, "default", "gpt-5.5", 206)
	require.False(t, decision.Allow)
	require.Equal(t, service.ChannelHealthSelectionCooldown, decision.Action)
}

func TestFinalizeRelayChannelHealthProbeClearsSuccessfulProbe(t *testing.T) {
	server := useChannelHealthRedisForControllerTest(t)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	observeControllerGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(service.DefaultChannelHealthCooldown)

	probeCtx, _ := gin.CreateTestContext(nil)
	probe := service.EvaluateChannelHealthSelection(probeCtx, "default", "gpt-5.5", 206)
	require.True(t, probe.Allow)
	require.Equal(t, service.ChannelHealthSelectionHalfOpenProbe, probe.Action)
	info.ChannelHealthRoute = &probe.Route

	finalizeRelayChannelHealthProbe(probeCtx, info, true)

	selectionCtx, _ := gin.CreateTestContext(nil)
	selection := service.EvaluateChannelHealthSelection(selectionCtx, "default", "gpt-5.5", 206)
	require.True(t, selection.Allow)
	require.Equal(t, service.ChannelHealthSelectionHealthy, selection.Action)
}

func TestFinalizeRelayChannelHealthProbeClearsAfterProtocolConversion(t *testing.T) {
	server := useChannelHealthRedisForControllerTest(t)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	observeControllerGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(service.DefaultChannelHealthCooldown)

	probeCtx, _ := gin.CreateTestContext(nil)
	probe := service.EvaluateChannelHealthSelection(probeCtx, "default", "gpt-5.5", 206)
	require.True(t, probe.Allow)
	require.Equal(t, service.ChannelHealthSelectionHalfOpenProbe, probe.Action)

	convertedRoute := probe.Route
	convertedRoute.UpstreamPath = "/v1/chat/completions"
	convertedRoute.FinalProtocol = "openai"
	convertedRoute.Fingerprint = ""
	convertedRoute = convertedRoute.Normalized()
	info.ChannelHealthRoute = &convertedRoute
	finalizeRelayChannelHealthProbe(probeCtx, info, true)

	selectionCtx, _ := gin.CreateTestContext(nil)
	selection := service.EvaluateChannelHealthSelection(selectionCtx, "default", "gpt-5.5", 206)
	require.True(t, selection.Allow)
	require.Equal(t, service.ChannelHealthSelectionHealthy, selection.Action)
}

func TestHalfOpenProbeFailureAfterProtocolConversionImmediatelyCoolsSelectedRoute(t *testing.T) {
	server := useChannelHealthRedisForControllerTest(t)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	observeControllerGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(service.DefaultChannelHealthCooldown)

	probeCtx, _ := gin.CreateTestContext(nil)
	probe := service.EvaluateChannelHealthSelection(probeCtx, "default", "gpt-5.5", 206)
	require.True(t, probe.Allow)
	require.Equal(t, service.ChannelHealthSelectionHalfOpenProbe, probe.Action)

	convertedRoute := probe.Route
	convertedRoute.UpstreamPath = "/v1/chat/completions"
	convertedRoute.FinalProtocol = "openai"
	convertedRoute.Fingerprint = ""
	convertedRoute = convertedRoute.Normalized()
	info.ChannelHealthRoute = &convertedRoute
	service.ObserveChannelHealthFailure(
		probeCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)

	selectionCtx, _ := gin.CreateTestContext(nil)
	selection := service.EvaluateChannelHealthSelection(selectionCtx, "default", "gpt-5.5", 206)
	require.False(t, selection.Allow)
	require.Equal(t, service.ChannelHealthSelectionCooldown, selection.Action)
}

func TestFinalizeRelayChannelHealthProbeReleasesInconclusiveProbe(t *testing.T) {
	server := useChannelHealthRedisForControllerTest(t)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	observeControllerGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(service.DefaultChannelHealthCooldown)

	probeCtx, _ := gin.CreateTestContext(nil)
	probe := service.EvaluateChannelHealthSelection(probeCtx, "default", "gpt-5.5", 206)
	require.True(t, probe.Allow)
	require.Equal(t, service.ChannelHealthSelectionHalfOpenProbe, probe.Action)

	finalizeRelayChannelHealthProbe(probeCtx, info, false)

	nextCtx, _ := gin.CreateTestContext(nil)
	next := service.EvaluateChannelHealthSelection(nextCtx, "default", "gpt-5.5", 206)
	require.True(t, next.Allow)
	require.Equal(t, service.ChannelHealthSelectionHalfOpenProbe, next.Action)
}

func TestFinalizeRelayChannelHealthProbeReleasesFailedStreamProbeEvenWithNilRelayError(t *testing.T) {
	server := useChannelHealthRedisForControllerTest(t)
	info := &relaycommon.RelayInfo{
		IsStream:        true,
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
		StreamStatus:    relaycommon.NewStreamStatus(),
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	observeControllerGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(service.DefaultChannelHealthCooldown)

	probeCtx, _ := gin.CreateTestContext(nil)
	probe := service.EvaluateChannelHealthSelection(probeCtx, "default", "gpt-5.5", 206)
	require.True(t, probe.Allow)
	require.Equal(t, service.ChannelHealthSelectionHalfOpenProbe, probe.Action)

	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, errors.New("upstream failed after partial usage"))
	finalizeRelayChannelHealthProbe(probeCtx, info, true)

	nextCtx, _ := gin.CreateTestContext(nil)
	next := service.EvaluateChannelHealthSelection(nextCtx, "default", "gpt-5.5", 206)
	require.True(t, next.Allow)
	require.Equal(t, service.ChannelHealthSelectionHalfOpenProbe, next.Action)
}

func TestRelayRequestProducedNormalEndRequiresStreamStatus(t *testing.T) {
	require.True(t, relayRequestProducedNormalEnd(&relaycommon.RelayInfo{}))
	require.False(t, relayRequestProducedNormalEnd(&relaycommon.RelayInfo{IsStream: true}))
	status := relaycommon.NewStreamStatus()
	status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	require.True(t, relayRequestProducedNormalEnd(&relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: status,
	}))
}

func TestRelayRequestProducedNormalEndRejectsStreamErrors(t *testing.T) {
	status := relaycommon.NewStreamStatus()
	status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errors.New("fatal handler stop"))
	require.False(t, relayRequestProducedNormalEnd(&relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: status,
	}))

	status = relaycommon.NewStreamStatus()
	status.RecordError("malformed upstream event")
	status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	require.False(t, relayRequestProducedNormalEnd(&relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: status,
	}))
}
