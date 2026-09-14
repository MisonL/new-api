package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// ChannelHealthAttempt binds one upstream operation to the same immutable
// route identity used by normal relay selection. It is used by task polling
// and result-content fetches, which do not naturally enter the Gin relay path.
// The attempt owns only health state; concurrency leases remain owned by the
// caller and must still be released independently.
type ChannelHealthAttempt struct {
	Context   *gin.Context
	Info      *relaycommon.RelayInfo
	Selection ChannelHealthSelection
	finalized atomic.Bool
}

func setTaskProviderRuntimeContext(c *gin.Context, channelType int, runtime *model.TaskRouteProviderSnapshot) {
	if c == nil {
		return
	}
	if runtime == nil {
		return
	}
	other := strings.TrimSpace(runtime.Other)
	apiVersion := strings.TrimSpace(runtime.ApiVersion)
	if apiVersion == "" {
		apiVersion = other
	}
	switch channelType {
	case constant.ChannelTypeAzure:
		c.Set("api_version", apiVersion)
	case constant.ChannelTypeVertexAi:
		c.Set("region", apiVersion)
	case constant.ChannelTypeXunfei:
		c.Set("api_version", apiVersion)
	case constant.ChannelTypeGemini:
		c.Set("api_version", apiVersion)
	case constant.ChannelTypeAli:
		c.Set("plugin", other)
	case constant.ChannelCloudflare:
		c.Set("api_version", apiVersion)
	case constant.ChannelTypeMokaAI:
		c.Set("api_version", apiVersion)
	case constant.ChannelTypeCoze:
		c.Set("bot_id", other)
	}
}

func taskProviderRuntimeAPIVersion(runtime *model.TaskRouteProviderSnapshot) string {
	if runtime == nil {
		return ""
	}
	if apiVersion := strings.TrimSpace(runtime.ApiVersion); apiVersion != "" {
		return apiVersion
	}
	return strings.TrimSpace(runtime.Other)
}

func taskProviderRuntimeOtherSettingsRaw(runtime *model.TaskRouteProviderSnapshot) string {
	if runtime == nil || !runtime.ResponsesBootstrapRecoveryEnabled {
		return ""
	}
	raw, err := common.Marshal(map[string]bool{"responses_stream_bootstrap_recovery_enabled": true})
	if err != nil {
		return ""
	}
	return string(raw)
}

// TaskRouteProviderOtherSettingsRaw reconstructs only the non-sensitive raw
// setting needed by request-scoped bootstrap recovery checks. It intentionally
// never returns the original channel settings JSON, which may contain custom
// header profile values.
func TaskRouteProviderOtherSettingsRaw(runtime *model.TaskRouteProviderSnapshot) string {
	return taskProviderRuntimeOtherSettingsRaw(runtime)
}

// BeginChannelHealthAttempt creates a request-scoped health context without
// mutating a caller's Gin context. routePath is a stable upstream operation
// path, not a task-specific URL containing an identifier.
func BeginChannelHealthAttempt(
	parent context.Context,
	group string,
	requestedModel string,
	channel *model.Channel,
	routePath string,
	key string,
) (*ChannelHealthAttempt, error) {
	return beginChannelHealthAttempt(parent, group, requestedModel, "", channel, routePath, key)
}

// BeginChannelHealthAttemptWithRoute uses a previously persisted route
// identity verbatim. It is the only entry point follow-up task operations use
// once a TaskRouteSnapshot exists; current channel model_mapping is not read.
func BeginChannelHealthAttemptWithRoute(
	parent context.Context,
	group string,
	route relaycommon.ChannelRouteIdentity,
	channel *model.Channel,
	key string,
) (*ChannelHealthAttempt, error) {
	if channel == nil {
		return nil, fmt.Errorf("invalid channel for health attempt")
	}
	baseURL := strings.TrimSpace(channel.GetBaseURL())
	if baseURL == "" {
		defaultBaseURL, ok := constant.ChannelBaseURL(channel.Type)
		if !ok {
			return nil, fmt.Errorf("invalid channel type for health attempt: %d", channel.Type)
		}
		baseURL = defaultBaseURL
	}
	return BeginChannelHealthAttemptWithRouteAndBaseURL(parent, group, route, channel, key, baseURL)
}

// BeginChannelHealthAttemptWithRouteAndBaseURL is the snapshot-aware variant
// used by asynchronous follow-up requests. The health identity and the
// endpoint exposed to the adaptor are both bound to the same submission-time
// base URL; the current channel row is used only for credential/status guards.
func BeginChannelHealthAttemptWithRouteAndBaseURL(
	parent context.Context,
	group string,
	route relaycommon.ChannelRouteIdentity,
	channel *model.Channel,
	key string,
	baseURL string,
) (*ChannelHealthAttempt, error) {
	if channel == nil {
		return nil, fmt.Errorf("invalid channel for health attempt")
	}
	providerRuntime := &model.TaskRouteProviderSnapshot{
		Other:                channel.Other,
		StatusCodeMapping:    channel.GetStatusCodeMapping(),
		ChannelSettings:      channel.GetSetting(),
		ChannelOtherSettings: channel.GetOtherSettings(),
	}
	providerRuntime.ResponsesBootstrapRecoveryEnabled = model.IsResponsesBootstrapRecoveryEnabledInOtherSettings(channel.OtherSettings)
	if channel.OpenAIOrganization != nil {
		providerRuntime.OpenAIOrganization = strings.TrimSpace(*channel.OpenAIOrganization)
	}
	return BeginChannelHealthAttemptWithRouteAndBaseURLAndProviderRuntime(
		parent,
		group,
		route,
		channel,
		key,
		baseURL,
		providerRuntime,
		providerRuntime.ChannelSettings.Proxy,
	)
}

// BeginChannelHealthAttemptWithRouteAndBaseURLAndProviderRuntime creates a
// health attempt from an immutable provider runtime snapshot. The channel row
// remains available for credential and status guards, but no provider
// configuration is read from it during context construction.
func BeginChannelHealthAttemptWithRouteAndBaseURLAndProviderRuntime(
	parent context.Context,
	group string,
	route relaycommon.ChannelRouteIdentity,
	channel *model.Channel,
	key string,
	baseURL string,
	providerRuntime *model.TaskRouteProviderSnapshot,
	proxy string,
) (*ChannelHealthAttempt, error) {
	if channel == nil || channel.Id <= 0 {
		return nil, fmt.Errorf("invalid channel for health attempt")
	}
	if providerRuntime == nil {
		return nil, fmt.Errorf("task health provider runtime is missing")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("invalid channel health base URL")
	}
	group = strings.TrimSpace(group)
	if group == "" || common.IsAutoGroup(group) {
		return nil, fmt.Errorf("invalid channel health group")
	}
	route = route.Normalized()
	if route.ChannelID == 0 {
		route.ChannelID = channel.Id
		route.Fingerprint = ""
		route = route.Normalized()
	}
	if route.ChannelID != channel.Id || route.RequestedModel == "" || route.EffectiveUpstreamModel == "" || route.FinalProtocol == "" ||
		(route.UpstreamPath == "/" && channel.Type != constant.ChannelTypeJimeng) {
		return nil, fmt.Errorf("incomplete task health route")
	}
	if parent == nil {
		parent = context.Background()
	}

	request, err := http.NewRequestWithContext(parent, http.MethodGet, route.UpstreamPath, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid task health route path: %w", err)
	}
	c, _ := gin.CreateTestContext(nil)
	c.Request = request
	common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, baseURL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, strings.TrimSpace(key))
	channelSetting := providerRuntime.ChannelSettings
	channelSetting.Proxy = strings.TrimSpace(proxy)
	channelOtherSettings := providerRuntime.ChannelOtherSettings
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channelSetting)
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channelOtherSettings)
	common.SetContextKey(c, constant.ContextKeyChannelOtherSettingRaw, taskProviderRuntimeOtherSettingsRaw(providerRuntime))
	common.SetContextKey(c, constant.ContextKeyChannelStatusCodeMapping, providerRuntime.StatusCodeMapping)
	common.SetContextKey(c, constant.ContextKeyChannelOrganization, providerRuntime.OpenAIOrganization)
	c.Set("channel_other", providerRuntime.Other)
	c.Set("channel_organization", providerRuntime.OpenAIOrganization)
	setTaskProviderRuntimeContext(c, channel.Type, providerRuntime)
	// A persisted task route is immutable. Do not inject the channel's current
	// model mapping into the follow-up context where a future helper could
	// accidentally re-resolve it.
	keyIndex := 0
	if channel.ChannelInfo.IsMultiKey {
		keyIndex = route.MultiKeyIndex
		keys := channel.GetKeys()
		if !route.CredentialScoped {
			keyIndex = -1
			for index, candidate := range keys {
				if strings.TrimSpace(candidate) == strings.TrimSpace(key) {
					keyIndex = index
					break
				}
			}
		}
		if keyIndex < 0 || keyIndex >= len(keys) || strings.TrimSpace(keys[keyIndex]) != strings.TrimSpace(key) {
			return nil, fmt.Errorf("task health route credential does not match channel key index")
		}
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, keyIndex)
	}

	info := &relaycommon.RelayInfo{
		UsingGroup:          group,
		OriginModelName:     route.RequestedModel,
		RelayMode:           relayconstant.RelayModeVideoFetchByID,
		UpstreamRequestPath: route.UpstreamPath,
		ChannelHealthRoute:  &route,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          channel.Type,
			ChannelId:            channel.Id,
			ChannelIsMultiKey:    channel.ChannelInfo.IsMultiKey,
			ChannelMultiKeyIndex: keyIndex,
			ChannelBaseUrl:       baseURL,
			ApiKey:               strings.TrimSpace(key),
			Organization:         providerRuntime.OpenAIOrganization,
			ChannelSetting:       channelSetting,
			ChannelOtherSettings: channelOtherSettings,
			ChannelOther:         providerRuntime.Other,
			StatusCodeMapping:    providerRuntime.StatusCodeMapping,
			ApiVersion:           taskProviderRuntimeAPIVersion(providerRuntime),
			UpstreamModelName:    route.EffectiveUpstreamModel,
		},
	}
	selection := EvaluateChannelHealthSelectionForRoute(c, group, route.RequestedModel, route)
	return &ChannelHealthAttempt{Context: c, Info: info, Selection: selection}, nil
}

// BeginChannelHealthAttemptWithEffectiveModel binds an asynchronous operation
// to the effective model persisted when the task was submitted. This prevents
// a later channel model_mapping edit from changing the route identity of an
// already-running task. When no persisted effective model exists it behaves
// exactly like BeginChannelHealthAttempt and resolves the current mapping.
func BeginChannelHealthAttemptWithEffectiveModel(
	parent context.Context,
	group string,
	requestedModel string,
	effectiveModel string,
	channel *model.Channel,
	routePath string,
	key string,
) (*ChannelHealthAttempt, error) {
	return beginChannelHealthAttempt(parent, group, requestedModel, effectiveModel, channel, routePath, key)
}

func beginChannelHealthAttempt(
	parent context.Context,
	group string,
	requestedModel string,
	effectiveModelOverride string,
	channel *model.Channel,
	routePath string,
	key string,
) (*ChannelHealthAttempt, error) {
	if channel == nil || channel.Id <= 0 {
		return nil, fmt.Errorf("invalid channel for health attempt")
	}
	group = strings.TrimSpace(group)
	requestedModel = strings.TrimSpace(requestedModel)
	if group == "" || common.IsAutoGroup(group) {
		return nil, fmt.Errorf("invalid channel health group")
	}
	if requestedModel == "" {
		return nil, fmt.Errorf("invalid channel health model")
	}
	routePath = relaycommon.NormalizeRoutePath(routePath)
	if routePath == "/" && channel.Type != constant.ChannelTypeJimeng {
		return nil, fmt.Errorf("invalid channel health route path")
	}
	if parent == nil {
		parent = context.Background()
	}

	request, err := http.NewRequestWithContext(parent, http.MethodGet, routePath, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid channel health route path: %w", err)
	}
	c, _ := gin.CreateTestContext(nil)
	c.Request = request
	common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())
	common.SetContextKey(c, constant.ContextKeyChannelKey, strings.TrimSpace(key))
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channel.GetSetting())
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channel.GetOtherSettings())
	// A caller-supplied effective model is already the persisted route identity.
	// Do not even materialize the current model_mapping into the follow-up
	// context in that case; a later adaptor/helper must not be able to mistake
	// mutable channel configuration for the historical mapping.
	if strings.TrimSpace(effectiveModelOverride) == "" {
		common.SetContextKey(c, constant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	}
	if channel.ChannelInfo.IsMultiKey {
		keyIndex := -1
		for index, candidate := range channel.GetKeys() {
			if strings.TrimSpace(candidate) == strings.TrimSpace(key) {
				keyIndex = index
				break
			}
		}
		if keyIndex < 0 {
			return nil, fmt.Errorf("health attempt credential is not present in channel %d", channel.Id)
		}
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, keyIndex)
	}

	var route relaycommon.ChannelRouteIdentity
	if strings.TrimSpace(effectiveModelOverride) != "" {
		// This entry point is used only when the caller has an immutable
		// provider-facing model (for example a legacy task's persisted
		// Properties.UpstreamModelName). Construct the route directly so a
		// malformed or changed channel model_mapping cannot be consulted even
		// once as a compatibility fallback.
		route = relaycommon.ChannelRouteIdentity{
			RequestedModel:         requestedModel,
			EffectiveUpstreamModel: strings.TrimSpace(effectiveModelOverride),
			FinalProtocol:          channelHealthRouteProtocol(c, channel.Type, relayconstant.RelayModeVideoFetchByID),
			Operation:              channelHealthOperation(channel.Type, relayconstant.RelayModeVideoFetchByID, routePath),
			UpstreamPath:           routePath,
			ChannelID:              channel.Id,
		}.Normalized()
	} else {
		var routeErr error
		route, routeErr = BuildChannelHealthRouteForChannel(c, group, requestedModel, channel)
		if routeErr != nil {
			return nil, routeErr
		}
	}
	// Compact model settings may intentionally rewrite the normal relay path;
	// task polling and content fetches have their own stable upstream operation
	// shape and must not inherit that rewrite.
	route.UpstreamPath = routePath
	route.Operation = channelHealthOperation(channel.Type, relayconstant.RelayModeVideoFetchByID, routePath)
	route.Fingerprint = ""
	route = route.Normalized()
	info := &relaycommon.RelayInfo{
		UsingGroup:          group,
		OriginModelName:     requestedModel,
		RelayMode:           relayconstant.RelayModeVideoFetchByID,
		UpstreamRequestPath: routePath,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          channel.Type,
			ChannelId:            channel.Id,
			ChannelIsMultiKey:    channel.ChannelInfo.IsMultiKey,
			ChannelMultiKeyIndex: common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex),
			ChannelBaseUrl:       channel.GetBaseURL(),
			ApiKey:               strings.TrimSpace(key),
			ChannelSetting:       channel.GetSetting(),
			ChannelOtherSettings: channel.GetOtherSettings(),
			UpstreamModelName:    route.EffectiveUpstreamModel,
		},
	}
	info.ChannelHealthRoute = &route
	selection := EvaluateChannelHealthSelectionForRoute(c, group, requestedModel, route)
	return &ChannelHealthAttempt{Context: c, Info: info, Selection: selection}, nil
}

// MarkSuccess completes a probe only after the upstream operation produced a
// valid response. For non-probe healthy attempts this is a no-op.
func (attempt *ChannelHealthAttempt) MarkSuccess() {
	if attempt == nil || attempt.finalized.Swap(true) {
		return
	}
	if attempt.Selection.Action == ChannelHealthSelectionHalfOpenProbe {
		MarkChannelHealthProbeSuccess(attempt.Context, attempt.Info)
	}
}

// ObserveFailure converts a task/content upstream error into the common health
// classifier. Local validation errors should not call this method.
func (attempt *ChannelHealthAttempt) ObserveFailure(err error, statusCode int, errorCode types.ErrorCode) {
	if attempt == nil {
		return
	}
	if err == nil {
		attempt.Release()
		return
	}
	var apiErr *types.NewAPIError
	if errors.As(err, &apiErr) && apiErr != nil {
		attempt.ObserveAPIError(apiErr)
		return
	}
	apiErr = types.NewErrorWithStatusCode(err, errorCode, statusCode)
	attempt.ObserveAPIError(apiErr)
}

// ObserveAPIError records an already classified upstream error without
// rebuilding it. RelayErrorHandler carries the original status, retry-after
// hint, structured error and retry policy; preserving those fields keeps
// health cooldown and request retry semantics identical to the normal relay.
func (attempt *ChannelHealthAttempt) ObserveAPIError(apiErr *types.NewAPIError) {
	if attempt == nil {
		return
	}
	if apiErr == nil {
		// Preserve the legacy nil-error cleanup path. Release owns the same
		// finalized gate and safely arbitrates with a concurrent observation.
		attempt.Release()
		return
	}
	if attempt.finalized.Swap(true) {
		return
	}
	decision := ClassifyChannelHealthFailure(attempt.Context, attempt.Info, apiErr)
	if decision.HealthAction == "ignore" {
		if attempt.Selection.Action == ChannelHealthSelectionHalfOpenProbe {
			ReleaseChannelHealthProbe(attempt.Context, attempt.Info)
		}
	} else {
		ObserveChannelHealthFailure(attempt.Context, attempt.Info, apiErr)
	}
}

// Release abandons a reserved probe when the operation exits without a valid
// upstream result or a classified health failure.
func (attempt *ChannelHealthAttempt) Release() {
	if attempt == nil || attempt.finalized.Swap(true) {
		return
	}
	if attempt.Selection.Action == ChannelHealthSelectionHalfOpenProbe {
		ReleaseChannelHealthProbe(attempt.Context, attempt.Info)
	}
}

// ResolveTaskHealthGroup returns the concrete group saved at task submission;
// auto is never used as a health scope key.
func ResolveTaskHealthGroup(task *model.Task, channel *model.Channel) string {
	if task != nil {
		// The submission snapshot is the authoritative route identity for every
		// asynchronous follow-up. The mutable task row and current channel
		// configuration are compatibility fallbacks only; consulting either first
		// can put a poll into a different health bucket after an edit.
		if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil {
			if group := strings.TrimSpace(snapshot.Group); group != "" && !common.IsAutoGroup(group) {
				return group
			}
		}
		if group := strings.TrimSpace(task.Group); group != "" && !common.IsAutoGroup(group) {
			return group
		}
	}
	if channel != nil {
		groups := channel.GetGroups()
		if len(groups) > 0 && !common.IsAutoGroup(groups[0]) {
			return strings.TrimSpace(groups[0])
		}
	}
	return "default"
}

// TaskHealthRoutePath returns a stable path shape for asynchronous status
// operations. It intentionally excludes task IDs so all tasks share a health
// scope while still remaining distinct from normal text routes.
func TaskHealthRoutePath(channelType int) string {
	return TaskHealthRoutePathForModel(channelType, "")
}

// TaskHealthRoutePathForModel keeps Gemini's API version aligned with the
// effective model chosen at submission time. Legacy callers that do not have a
// model continue to resolve to the default v1beta path.
func TaskHealthRoutePathForModel(channelType int, effectiveModel string) string {
	if channelType == constant.ChannelTypeGemini {
		return TaskPollingRoutePathForModel(channelType, effectiveModel, "")
	}
	switch channelType {
	case constant.ChannelTypeVertexAi:
		return "/v1/projects:fetchPredictOperation"
	default:
		return "/v1/tasks/fetch"
	}
}

// VideoContentHealthRoutePath returns a stable route shape for completed-video
// downloads. The concrete URL may contain a task identifier or a provider
// signed URL; health state must remain keyed by the operation shape instead of
// those volatile values.
func VideoContentHealthRoutePath(channelType int) string {
	return VideoContentHealthRoutePathForModel(channelType, "")
}

func VideoContentHealthRoutePathForModel(channelType int, effectiveModel string) string {
	if channelType == constant.ChannelTypeGemini {
		return TaskContentRoutePathForModel(channelType, effectiveModel, "")
	}
	switch channelType {
	case constant.ChannelTypeVertexAi:
		return "/v1/projects:fetchPredictOperation"
	case constant.ChannelTypeMiniMax:
		// Hailuo resolves the completed file through this endpoint rather than
		// the generic OpenAI video-content route.
		return "/v1/files/retrieve"
	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
		return "/v1/videos/content"
	default:
		return "/v1/videos/content"
	}
}
