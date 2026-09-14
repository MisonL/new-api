package service

import (
	"context"
	"fmt"
	"net/textproto"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	modelsetting "github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
)

const taskRouteSnapshotSchemaVersion = model.TaskRouteSnapshotSchemaVersion

const taskRouteAdapterIdentityPrefix = "task-adapter/v1/"

const taskRouteSnapshotCompatibilityLogGap = 5 * time.Minute

// TaskRouteSnapshotCompatibilityMetrics records explicit compatibility-path
// usage. Schema-5 rows have an authenticated header policy but predate the
// provider-runtime freeze; schema-1..4 rows predate phase-specific routing.
// These counters make the remaining mutable-channel compatibility boundary
// visible without rejecting existing tasks solely because they are old.
type TaskRouteSnapshotCompatibilityMetrics struct {
	Schema5Replays uint64 `json:"schema5_replays"`
	LegacyReplays  uint64 `json:"legacy_replays"`
}

var taskRouteSnapshotCompatibilityMetrics struct {
	schema5Replays atomic.Uint64
	legacyReplays  atomic.Uint64
}

var taskRouteSnapshotCompatibilityLogAt atomic.Int64

func TaskRouteSnapshotCompatibilityMetricsSnapshot() TaskRouteSnapshotCompatibilityMetrics {
	return TaskRouteSnapshotCompatibilityMetrics{
		Schema5Replays: taskRouteSnapshotCompatibilityMetrics.schema5Replays.Load(),
		LegacyReplays:  taskRouteSnapshotCompatibilityMetrics.legacyReplays.Load(),
	}
}

func recordTaskRouteSnapshotCompatibilityReplay(ctx context.Context, task *model.Task, schemaVersion int, phase string) {
	if schemaVersion >= taskRouteSnapshotSchemaVersion || schemaVersion < 1 {
		return
	}
	if schemaVersion == model.TaskRouteSnapshotHeaderPolicySchemaVersion {
		taskRouteSnapshotCompatibilityMetrics.schema5Replays.Add(1)
	} else {
		taskRouteSnapshotCompatibilityMetrics.legacyReplays.Add(1)
	}
	now := time.Now().UnixNano()
	last := taskRouteSnapshotCompatibilityLogAt.Load()
	if last != 0 && now-last < taskRouteSnapshotCompatibilityLogGap.Nanoseconds() {
		return
	}
	if !taskRouteSnapshotCompatibilityLogAt.CompareAndSwap(last, now) {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	taskID := ""
	channelID := 0
	if task != nil {
		taskID = strings.TrimSpace(task.TaskID)
		channelID = task.ChannelId
	}
	logger.LogWarn(ctx, fmt.Sprintf("task route snapshot compatibility replay: schema=%d phase=%s task_id=%s channel_id=%d", schemaVersion, strings.TrimSpace(phase), taskID, channelID))
}

// TaskPollingRoutePath returns a stable provider/action operation path. It is
// deliberately a path shape, not the task-specific URL, so health state is not
// merged across providers or Kling operations.
func TaskPollingRoutePath(channelType int, action string) string {
	action = strings.TrimSpace(strings.ToLower(action))
	switch channelType {
	case constant.ChannelTypeGemini:
		return "/v1beta/operations"
	case constant.ChannelTypeVertexAi:
		return "/v1/projects:fetchPredictOperation"
	case constant.ChannelTypeAgnes:
		return "/agnesapi"
	case constant.ChannelTypeAli:
		return "/api/v1/tasks"
	case constant.ChannelTypeDoubaoVideo, constant.ChannelTypeVolcEngine:
		return "/api/v3/contents/generations/tasks"
	case constant.ChannelTypeMiniMax:
		return "/v1/query/video_generation"
	case constant.ChannelTypeJimeng:
		return "/jimeng"
	case constant.ChannelTypeKling:
		if action == strings.ToLower(constant.TaskActionGenerate) {
			return "/v1/videos/image2video"
		}
		return "/v1/videos/text2video"
	case constant.ChannelTypeVidu:
		return "/ent/v2/tasks"
	case constant.ChannelTypeSora, constant.ChannelTypeOpenAI:
		return "/v1/videos"
	case constant.ChannelTypeSunoAPI:
		return "/suno/fetch"
	case constant.ChannelTypeMidjourney, constant.ChannelTypeMidjourneyPlus:
		return "/mj/task/list-by-condition"
	default:
		return "/v1/tasks/fetch"
	}
}

// TaskPollingRoutePathForModel captures provider paths whose API version is
// selected from the effective model (currently Gemini). The model-dependent
// path is persisted so changing global model settings cannot redirect an
// already-submitted asynchronous task.
func TaskPollingRoutePathForModel(channelType int, effectiveModel string, action string) string {
	if channelType == constant.ChannelTypeGemini {
		version := strings.TrimSpace(modelsetting.GetGeminiVersionSetting(strings.TrimSpace(effectiveModel)))
		if version == "" {
			version = "v1beta"
		}
		return "/" + strings.Trim(version, "/") + "/operations"
	}
	return TaskPollingRoutePath(channelType, action)
}

// TaskContentRoutePath returns the stable operation used to obtain completed
// content. Gemini and Vertex first fetch an operation payload; other providers
// use their content endpoint or a stored signed URL.
func TaskContentRoutePath(channelType int, action string) string {
	return TaskContentRoutePathForModel(channelType, "", action)
}

// TaskContentRoutePathForModel captures provider paths whose API version is
// selected from the effective model. Gemini uses the same version for the
// completed-operation fetch as it does for polling; keeping both paths in the
// snapshot prevents a later global version change from splitting one task
// across two API versions.
func TaskContentRoutePathForModel(channelType int, effectiveModel string, action string) string {
	if channelType == constant.ChannelTypeGemini {
		version := strings.TrimSpace(modelsetting.GetGeminiVersionSetting(strings.TrimSpace(effectiveModel)))
		if version == "" {
			version = "v1beta"
		}
		return "/" + strings.Trim(version, "/") + "/operations"
	}
	switch channelType {
	case constant.ChannelTypeVertexAi:
		return "/v1/projects:fetchPredictOperation"
	case constant.ChannelTypeMiniMax:
		// Hailuo/MiniMax resolves the completed file through its dedicated
		// retrieve endpoint inside ParseTaskResult. Keep the persisted content
		// route identical to that request so health attribution and snapshot
		// fingerprints describe the actual follow-up phase.
		return "/v1/files/retrieve"
	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
		return "/v1/videos/content"
	default:
		return "/v1/videos/content"
	}
}

// TaskRouteOperation is a stable provider/action identity kept alongside the
// paths. The action is part of the identity even when a provider shares one
// HTTP endpoint for several operations.
func TaskRouteOperation(channelType int, platform constant.TaskPlatform, action string) string {
	provider := strings.TrimSpace(strings.ToLower(string(platform)))
	if provider == "" {
		provider = fmt.Sprintf("channel-%d", channelType)
	}
	action = strings.TrimSpace(strings.ToLower(action))
	if action == "" {
		action = "unknown"
	}
	return "task." + provider + "." + action
}

// BuildTaskRouteSnapshot freezes all non-secret routing inputs needed by later
// task polling and content retrieval. Credentials are never persisted in
// plaintext; the selected profile identity/version and encrypted static header
// policy are retained so replay can reject policy drift.
func BuildTaskRouteSnapshot(c *gin.Context, info *relaycommon.RelayInfo, platform constant.TaskPlatform, action string) (*model.TaskRouteSnapshot, error) {
	if info == nil || info.ChannelMeta == nil || info.ChannelMeta.ChannelId <= 0 {
		return nil, fmt.Errorf("task route snapshot channel metadata is incomplete")
	}
	channelType := info.ChannelMeta.ChannelType
	if !constant.IsValidChannelType(channelType) {
		return nil, fmt.Errorf("task route snapshot channel type is invalid: %d", channelType)
	}
	platform = constant.TaskPlatform(strings.TrimSpace(string(platform)))
	if platform == "" {
		return nil, fmt.Errorf("task route snapshot adapter platform is missing")
	}
	if expected, ok := taskAdapterPlatformForChannelType(channelType); ok && platform != expected {
		return nil, fmt.Errorf("task route snapshot adapter platform mismatch: channel_type=%d expected=%q got=%q", channelType, expected, platform)
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return nil, fmt.Errorf("task route snapshot action is missing")
	}
	requestedModel := strings.TrimSpace(info.OriginModelName)
	effectiveModel := strings.TrimSpace(info.UpstreamModelName)
	if effectiveModel == "" {
		effectiveModel = strings.TrimSpace(info.ChannelMeta.UpstreamModelName)
	}
	if effectiveModel == "" {
		effectiveModel = requestedModel
	}
	protocol := ""
	if info.ChannelHealthRoute != nil {
		route := info.ChannelHealthRoute.Normalized()
		protocol = strings.TrimSpace(route.FinalProtocol)
	}
	if protocol == "" {
		protocol = channelHealthRouteProtocol(c, info.ChannelMeta.ChannelType, info.RelayMode)
	}
	if requestedModel == "" || effectiveModel == "" || protocol == "" {
		return nil, fmt.Errorf("task route snapshot model or protocol identity is incomplete")
	}

	rawSubmitPath := strings.TrimSpace(info.UpstreamRequestPath)
	submitPath := relaycommon.NormalizeRoutePath(rawSubmitPath)
	submitPathExplicit := rawSubmitPath != ""
	if info.TaskRelayInfo != nil && strings.TrimSpace(info.TaskRelayInfo.SubmitRequestURL) != "" {
		submitURL, err := url.Parse(strings.TrimSpace(info.TaskRelayInfo.SubmitRequestURL))
		if err != nil || submitURL.Path == "" {
			if err == nil {
				err = fmt.Errorf("submit URL path is empty")
			}
			return nil, fmt.Errorf("task route snapshot submit URL is invalid: %w", err)
		}
		submitPath = relaycommon.NormalizeRoutePath(submitURL.Path)
		submitPathExplicit = true
	}
	if !submitPathExplicit && info.ChannelHealthRoute != nil && strings.TrimSpace(info.ChannelHealthRoute.UpstreamPath) != "" {
		submitPath = relaycommon.NormalizeRoutePath(info.ChannelHealthRoute.UpstreamPath)
		submitPathExplicit = true
	}
	if !submitPathExplicit || (submitPath == "/" && info.ChannelMeta.ChannelType != constant.ChannelTypeJimeng) {
		// The fallback still needs to describe the effective provider model. For
		// Gemini the API version is model-specific, so using the legacy fixed
		// /v1beta path here could make the persisted submit route disagree with
		// the polling route when model mapping selects a v1 model.
		submitPath = TaskPollingRoutePathForModel(info.ChannelMeta.ChannelType, effectiveModel, action)
	}
	baseURL := strings.TrimSpace(info.ChannelMeta.ChannelBaseUrl)
	if baseURL == "" {
		defaultBaseURL, ok := constant.ChannelBaseURL(info.ChannelMeta.ChannelType)
		if !ok {
			return nil, fmt.Errorf("task route snapshot channel type is invalid: %d", info.ChannelMeta.ChannelType)
		}
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("task route snapshot base URL is missing")
	}
	// Task follow-ups need a provider/action identity that is independent from
	// the generic relay operation bound during model mapping. ModelMappedHelper
	// intentionally installs a `relay.*` route for the normal request path;
	// reusing that value here would merge (for example) Suno MUSIC and LYRICS
	// tasks that share one fetch endpoint into the same health bucket. The task
	// platform and action are the canonical identity for this asynchronous
	// lifecycle, while the persisted path and fingerprint still distinguish
	// provider endpoint variants.
	submitOperation := TaskRouteOperation(info.ChannelMeta.ChannelType, platform, action)
	pollingOperation := taskRoutePhaseOperation(submitOperation, "poll")
	contentOperation := taskRoutePhaseOperation(submitOperation, "content")

	base := relaycommon.ChannelRouteIdentity{
		RequestedModel:         requestedModel,
		EffectiveUpstreamModel: effectiveModel,
		FinalProtocol:          protocol,
		Operation:              submitOperation,
		UpstreamPath:           submitPath,
		ChannelID:              info.ChannelMeta.ChannelId,
		MultiKeyIndex:          info.ChannelMeta.ChannelMultiKeyIndex,
		CredentialScoped:       info.ChannelMeta.ChannelIsMultiKey,
	}.Normalized()
	pollingPath := TaskPollingRoutePathForModel(info.ChannelMeta.ChannelType, effectiveModel, action)
	// Jimeng has two wire endpoints: the native access-key form uses the root
	// path, while the new-api bearer-key form uses the /jimeng prefix. The
	// submit URL is the authoritative source for this distinction; preserving
	// it prevents an old-key task from being polled through the wrong endpoint
	// after the channel is edited.
	if info.ChannelMeta.ChannelType == constant.ChannelTypeJimeng && submitPath == "/" {
		pollingPath = "/"
	}
	polling := base
	polling.Operation = pollingOperation
	polling.UpstreamPath = pollingPath
	polling.Fingerprint = ""
	polling = polling.Normalized()
	contentPath := TaskContentRoutePathForModel(info.ChannelMeta.ChannelType, effectiveModel, action)
	content := base
	content.Operation = contentOperation
	content.UpstreamPath = contentPath
	content.Fingerprint = ""
	content = content.Normalized()

	profileID, profileVersion, profileFingerprint, profileErr := taskHeaderProfileSnapshot(
		c,
		info.ChannelMeta.ChannelId,
		info.ChannelMeta.ChannelOtherSettings.HeaderProfileStrategy,
		info.RetryIndex,
	)
	if profileErr != nil {
		return nil, fmt.Errorf("task route snapshot header profile is incomplete: %w", profileErr)
	}
	if err := prepareTaskRouteSnapshotParamOverride(info); err != nil {
		return nil, fmt.Errorf("task route snapshot param override is incomplete: %w", err)
	}
	headerPolicy, headerPolicyErr := taskRouteSnapshotHeaderPolicy(info, c, profileID)
	if headerPolicyErr != nil {
		return nil, fmt.Errorf("task route snapshot header policy is incomplete: %w", headerPolicyErr)
	}
	headerPolicyFingerprint := model.ComputeTaskRouteHeaderPolicyFingerprint(headerPolicy)
	headerPolicyCiphertext, headerPolicyErr := model.EncryptTaskRouteHeaderPolicy(headerPolicy)
	if headerPolicyErr != nil {
		return nil, fmt.Errorf("encrypt task route header policy: %w", headerPolicyErr)
	}
	scopeVersion, scopeErr := resolveTaskRouteScopeVersion(c, info, base)
	if scopeErr != nil {
		return nil, fmt.Errorf("task route snapshot scope version is unavailable: %w", scopeErr)
	}
	group, groupErr := resolveTaskRouteSnapshotGroup(c, info)
	if groupErr != nil {
		return nil, groupErr
	}
	providerRuntime, providerRuntimeErr := buildTaskRouteProviderSnapshot(c, info)
	if providerRuntimeErr != nil {
		return nil, fmt.Errorf("task route snapshot provider runtime is incomplete: %w", providerRuntimeErr)
	}
	proxy := strings.TrimSpace(info.ChannelMeta.ChannelSetting.Proxy)
	if proxy == "" && c != nil {
		if settings, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting); ok {
			proxy = strings.TrimSpace(settings.Proxy)
		}
	}
	proxyCiphertext, err := model.EncryptTaskRouteProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("encrypt task route proxy: %w", err)
	}
	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:            taskRouteSnapshotSchemaVersion,
		Group:                    group,
		BaseURL:                  baseURL,
		RequestedModel:           requestedModel,
		EffectiveUpstreamModel:   effectiveModel,
		Protocol:                 protocol,
		Operation:                submitOperation,
		PollingOperation:         pollingOperation,
		ContentOperation:         contentOperation,
		SubmitPath:               submitPath,
		PollingPath:              pollingPath,
		ContentPath:              contentPath,
		RouteFingerprint:         base.Fingerprint,
		PollingRouteFingerprint:  polling.Fingerprint,
		ContentRouteFingerprint:  content.Fingerprint,
		RouteScopeVersion:        scopeVersion,
		ChannelID:                info.ChannelMeta.ChannelId,
		ChannelType:              channelType,
		AdapterPlatform:          string(platform),
		AdapterIdentity:          taskRouteAdapterIdentity(channelType, platform),
		MultiKeyIndex:            info.ChannelMeta.ChannelMultiKeyIndex,
		CredentialScoped:         info.ChannelMeta.ChannelIsMultiKey,
		HeaderProfileID:          profileID,
		HeaderProfileVersion:     profileVersion,
		HeaderProfileFingerprint: profileFingerprint,
		HeaderPolicyCaptured:     true,
		HeaderPolicyCiphertext:   headerPolicyCiphertext,
		HeaderPolicyFingerprint:  headerPolicyFingerprint,
		ProxyCaptured:            true,
		ProxyCiphertext:          proxyCiphertext,
		ProxyFingerprint:         model.ComputeTaskRouteProxyFingerprint(proxy),
		Proxy:                    proxy,
		ProviderRuntime:          providerRuntime,
	}
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	return snapshot, nil
}

// buildTaskRouteProviderSnapshot captures the provider-specific values that
// are normally copied from a mutable Channel row into request context. The
// task snapshot owns these values for every later phase; credentials, proxy
// URLs, and resolved profile headers use their dedicated authenticated
// fields and are deliberately not duplicated here.
func buildTaskRouteProviderSnapshot(c *gin.Context, info *relaycommon.RelayInfo) (*model.TaskRouteProviderSnapshot, error) {
	if info == nil || info.ChannelMeta == nil {
		return nil, fmt.Errorf("channel metadata is missing")
	}
	meta := info.ChannelMeta
	channelOtherSettings, err := cloneTaskRouteChannelOtherSettings(meta.ChannelOtherSettings)
	if err != nil {
		return nil, fmt.Errorf("clone channel other settings: %w", err)
	}
	runtime := &model.TaskRouteProviderSnapshot{
		Other:                strings.TrimSpace(meta.ChannelOther),
		ApiVersion:           strings.TrimSpace(meta.ApiVersion),
		StatusCodeMapping:    strings.TrimSpace(meta.StatusCodeMapping),
		OpenAIOrganization:   strings.TrimSpace(meta.Organization),
		ChannelSettings:      meta.ChannelSetting,
		ChannelOtherSettings: channelOtherSettings,
	}
	rawOtherSettings := ""
	if c != nil {
		if runtime.Other == "" {
			runtime.Other = strings.TrimSpace(c.GetString("channel_other"))
		}
		if runtime.StatusCodeMapping == "" {
			runtime.StatusCodeMapping = strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyChannelStatusCodeMapping))
		}
		if runtime.OpenAIOrganization == "" {
			runtime.OpenAIOrganization = strings.TrimSpace(c.GetString("channel_organization"))
			if runtime.OpenAIOrganization == "" {
				runtime.OpenAIOrganization = strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyChannelOrganization))
			}
		}
		if runtime.ApiVersion == "" {
			runtime.ApiVersion = strings.TrimSpace(c.GetString("api_version"))
			if runtime.ApiVersion == "" {
				runtime.ApiVersion = strings.TrimSpace(c.GetString("region"))
			}
		}
		if settings, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting); ok {
			runtime.ChannelSettings = settings
		}
		if settings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting); ok {
			runtime.ChannelOtherSettings, err = cloneTaskRouteChannelOtherSettings(settings)
			if err != nil {
				return nil, fmt.Errorf("clone context channel other settings: %w", err)
			}
		}
		rawOtherSettings = common.GetContextKeyString(c, constant.ContextKeyChannelOtherSettingRaw)
	}
	runtime.ResponsesBootstrapRecoveryEnabled = model.IsResponsesBootstrapRecoveryEnabledInOtherSettings(rawOtherSettings)
	// Proxy and HeaderProfileStrategy are represented by the authenticated
	// proxy/header-policy fields on TaskRouteSnapshot. Keeping them empty here
	// prevents accidental plaintext persistence and prevents a replay helper
	// from resolving mutable profile state a second time.
	runtime.ChannelSettings.Proxy = ""
	runtime.ChannelOtherSettings.HeaderProfileStrategy = nil
	return runtime, nil
}

// cloneTaskRouteChannelOtherSettings prevents the persisted runtime snapshot
// from sharing slices or pointer fields with a mutable request/channel object.
// A value copy alone is insufficient for Responses capability registries,
// request-body limits, and fallback-model lists.
func cloneTaskRouteChannelOtherSettings(settings dto.ChannelOtherSettings) (dto.ChannelOtherSettings, error) {
	encoded, err := common.Marshal(settings)
	if err != nil {
		return dto.ChannelOtherSettings{}, err
	}
	var cloned dto.ChannelOtherSettings
	if err := common.Unmarshal(encoded, &cloned); err != nil {
		return dto.ChannelOtherSettings{}, err
	}
	return cloned, nil
}

// TaskRouteSnapshotProviderRuntime returns a defensive copy of the immutable
// provider runtime captured in a current-schema task snapshot. It is shared by
// middleware and asynchronous health attempts so every follow-up installs the
// same provider configuration without consulting the live channel row.
func TaskRouteSnapshotProviderRuntime(snapshot *model.TaskRouteSnapshot) (*model.TaskRouteProviderSnapshot, error) {
	if snapshot == nil || snapshot.SchemaVersion < taskRouteSnapshotSchemaVersion {
		return nil, fmt.Errorf("task route snapshot provider runtime is unavailable for legacy schema")
	}
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	if snapshot.ProviderRuntime == nil {
		return nil, fmt.Errorf("task route snapshot provider runtime is missing")
	}
	runtime := *snapshot.ProviderRuntime
	channelOtherSettings, err := cloneTaskRouteChannelOtherSettings(runtime.ChannelOtherSettings)
	if err != nil {
		return nil, fmt.Errorf("clone task route provider channel other settings: %w", err)
	}
	runtime.ChannelOtherSettings = channelOtherSettings
	// Validate already rejects these fields, but clear them on the returned
	// copy as an additional boundary against accidental future schema changes.
	runtime.ChannelSettings.Proxy = ""
	runtime.ChannelOtherSettings.HeaderProfileStrategy = nil
	return &runtime, nil
}

// prepareTaskRouteSnapshotParamOverride validates and evaluates the selected
// channel's param_override before an asynchronous task is submitted. Task
// bodies are built before the snapshot is created, so applying a body
// operation here would be too late; such operations must fail explicitly
// instead of being silently omitted. Header-only operations are evaluated once
// against the submission context and their resulting static headers are
// captured by taskRouteSnapshotHeaderPolicy. Client-dynamic passthrough values
// are rejected because a later poll/content request has no trustworthy client
// request from which to reconstruct them.
func prepareTaskRouteSnapshotParamOverride(info *relaycommon.RelayInfo) error {
	if info == nil || info.ChannelMeta == nil || len(info.ChannelMeta.ParamOverride) == 0 {
		return nil
	}
	if err := relaycommon.ValidateBytePreservingParamOverride(info.ChannelMeta.ParamOverride); err != nil {
		return fmt.Errorf("async task param override must be header-only: %w", err)
	}
	if err := relaycommon.ApplyBytePreservingHeaderParamOverrideWithRelayInfo(info); err != nil {
		return fmt.Errorf("evaluate async task header-only param override: %w", err)
	}
	if len(info.RuntimePassedRequestHeaders) == 0 {
		return nil
	}
	names := make([]string, 0, len(info.RuntimePassedRequestHeaders))
	for name := range info.RuntimePassedRequestHeaders {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			return fmt.Errorf("async task param override contains an empty client-dynamic header name")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Errorf("async task param override depends on client-dynamic headers: %s", strings.Join(names, ", "))
}

// resolveTaskRouteScopeVersion records the administrative route-scope version
// when the selector exposed a concrete scope. Route-scope versions are the
// member hashes used by the CAS-protected route-scope APIs; using the selected
// route fingerprint here would make the snapshot field look like a scope
// version while omitting membership and capacity changes. Compatibility paths
// without a scope ID retain a namespaced route-identity fallback.
func resolveTaskRouteScopeVersion(c *gin.Context, info *relaycommon.RelayInfo, fallback relaycommon.ChannelRouteIdentity) (string, error) {
	scopeID := ""
	var diagnostics dto.ChannelRouteSelectionDiagnostics
	if c != nil {
		if value, ok := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](c, constant.ContextKeyChannelRouteDiagnostics); ok {
			diagnostics = value
			scopeID = strings.TrimSpace(value.RouteScope)
		}
		if scopeID == "" {
			scopeID = strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyChannelRouteScope))
		}
	}
	if scopeID == "" && info != nil && info.ChannelMeta != nil && strings.TrimSpace(diagnostics.EffectiveRouteModel) != "" && diagnostics.RoutePriority >= 0 {
		group := strings.TrimSpace(diagnostics.Group)
		if group == "" {
			group = strings.TrimSpace(info.UsingGroup)
		}
		if group != "" && !strings.EqualFold(group, "auto") {
			scopeID = model.EncodeRouteScopeID(group, diagnostics.EffectiveRouteModel, diagnostics.RoutePriority)
		}
	}
	if scopeID != "" {
		if model.DB == nil {
			return "", fmt.Errorf("route scope %q cannot be resolved without database", scopeID)
		}
		scope, err := model.GetChannelRouteScope(scopeID)
		if err != nil {
			return "", fmt.Errorf("resolve route scope %q: %w", scopeID, err)
		}
		if scope == nil || strings.TrimSpace(scope.Version) == "" {
			return "", fmt.Errorf("route scope %q has no version", scopeID)
		}
		return strings.TrimSpace(scope.Version), nil
	}
	if fingerprint := strings.TrimSpace(fallback.Fingerprint); fingerprint != "" {
		return "v1:" + fingerprint, nil
	}
	return "", nil
}

// resolveTaskRouteSnapshotGroup returns the concrete group that actually
// selected the channel. An asynchronous task must never persist "auto" or
// silently turn an unresolved group into "default", because the group is part
// of the health and routing scope used by every later poll/content request.
func resolveTaskRouteSnapshotGroup(c *gin.Context, info *relaycommon.RelayInfo) (string, error) {
	if info == nil || info.ChannelMeta == nil || info.ChannelMeta.ChannelId <= 0 {
		return "", fmt.Errorf("task route snapshot group cannot be resolved without channel metadata")
	}
	concrete := func(value string) string {
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, "auto") {
			return ""
		}
		return value
	}

	// A continuation/remix carries the original task's concrete scope. It must
	// win over any auto-group state left by the new request's initial selector.
	if info.TaskRelayInfo != nil {
		if group := concrete(info.OriginRouteGroup); group != "" {
			return group, nil
		}
	}
	if c != nil {
		if group := concrete(common.GetContextKeyString(c, constant.ContextKeyAutoGroup)); group != "" {
			return group, nil
		}
	}
	if c != nil {
		if diagnostics, ok := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](c, constant.ContextKeyChannelRouteDiagnostics); ok {
			if group := concrete(diagnostics.Group); group != "" {
				return group, nil
			}
		}
	}

	requestedGroup := strings.TrimSpace(info.UsingGroup)
	if requestedGroup == "" {
		requestedGroup = strings.TrimSpace(info.TokenGroup)
	}
	if requestedGroup == "" && c != nil {
		requestedGroup = strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
	}
	if requestedGroup == "" && c != nil {
		requestedGroup = strings.TrimSpace(c.GetString("group"))
	}
	if group := concrete(requestedGroup); group != "" {
		return group, nil
	}
	if !strings.EqualFold(requestedGroup, "auto") {
		return "", fmt.Errorf("task route snapshot concrete group is missing")
	}

	userGroup := strings.TrimSpace(info.UserGroup)
	if userGroup == "" && c != nil {
		userGroup = strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyUserGroup))
	}
	autoGroups := GetUserAutoGroup(userGroup)
	if len(autoGroups) == 0 {
		return "", fmt.Errorf("task route snapshot auto group has no user-available concrete groups")
	}
	if model.DB == nil {
		return "", fmt.Errorf("task route snapshot auto group cannot be resolved without channel capability state")
	}

	matches := make([]string, 0, len(autoGroups))
	requestedModel := strings.TrimSpace(info.OriginModelName)
	for _, candidate := range autoGroups {
		candidate = concrete(candidate)
		if candidate == "" {
			continue
		}
		var enabled bool
		if c != nil {
			enabled = IsChannelEnabledForGroupModelForContext(c, candidate, requestedModel, info.ChannelMeta.ChannelId)
		} else {
			enabled = model.IsChannelEnabledForGroupModel(candidate, requestedModel, info.ChannelMeta.ChannelId)
		}
		if enabled {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("task route snapshot auto group cannot resolve channel %d for model %q", info.ChannelMeta.ChannelId, requestedModel)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("task route snapshot auto group is ambiguous for channel %d and model %q: %s", info.ChannelMeta.ChannelId, requestedModel, strings.Join(matches, ","))
	}
}

func taskRoutePhaseOperation(operation string, phase string) string {
	operation = strings.TrimSpace(operation)
	phase = strings.TrimSpace(strings.ToLower(phase))
	if operation == "" || phase == "" {
		return operation
	}
	return operation + "." + phase
}

// TaskRouteSnapshotBaseURL returns the endpoint captured at submission time.
// Rows created before schema version 2 do not have this field; they retain the
// legacy compatibility behavior and use the current channel endpoint.
func TaskRouteSnapshotBaseURL(snapshot *model.TaskRouteSnapshot, channel *model.Channel) (string, error) {
	if snapshot != nil {
		if err := snapshot.Validate(); err != nil {
			return "", err
		}
		if baseURL := strings.TrimRight(strings.TrimSpace(snapshot.BaseURL), "/"); baseURL != "" {
			return baseURL, nil
		}
		if snapshot.SchemaVersion >= 2 {
			return "", fmt.Errorf("task route snapshot base URL is missing")
		}
	}
	if channel == nil {
		return "", fmt.Errorf("task route channel is nil")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	if baseURL == "" {
		defaultBaseURL, ok := constant.ChannelBaseURL(channel.Type)
		if !ok {
			return "", fmt.Errorf("task route channel type is invalid: %d", channel.Type)
		}
		baseURL = strings.TrimRight(strings.TrimSpace(defaultBaseURL), "/")
	}
	if baseURL == "" {
		return "", fmt.Errorf("task route channel base URL is empty")
	}
	return baseURL, nil
}

// TaskRouteSnapshotProxy returns the proxy captured at task submission. A
// schema-v3 snapshot always owns the proxy value, including an explicitly empty
// value; older rows use the current channel setting as a compatibility path.
func TaskRouteSnapshotProxy(snapshot *model.TaskRouteSnapshot, channel *model.Channel) (string, error) {
	if snapshot != nil && snapshot.ProxyCaptured {
		// Work on a value copy. Follow-up polling/content requests can run
		// concurrently for the same task; decrypting into the persisted pointer
		// would introduce a data race and make validation depend on call order.
		resolved := *snapshot
		if err := resolved.Validate(); err != nil {
			return "", err
		}
		if resolved.Proxy == "" && strings.TrimSpace(resolved.ProxyCiphertext) != "" {
			proxy, err := model.DecryptTaskRouteProxy(resolved.ProxyCiphertext)
			if err != nil {
				return "", fmt.Errorf("decrypt task route proxy: %w", err)
			}
			resolved.Proxy = proxy
		}
		// Validate once more after materializing the plaintext. The first
		// validation covers the persisted snapshot digest; this explicit second
		// check also verifies the authenticated proxy fingerprint against the
		// value that will actually be used for the follow-up request.
		if model.ComputeTaskRouteProxyFingerprint(resolved.Proxy) != strings.TrimSpace(resolved.ProxyFingerprint) {
			return "", fmt.Errorf("task route snapshot proxy fingerprint mismatch")
		}
		return strings.TrimSpace(resolved.Proxy), nil
	}
	if snapshot != nil && snapshot.SchemaVersion >= taskRouteSnapshotSchemaVersion {
		return "", fmt.Errorf("task route snapshot proxy is missing")
	}
	if channel == nil {
		return "", fmt.Errorf("task route channel is nil")
	}
	return strings.TrimSpace(channel.GetSetting().Proxy), nil
}

// TaskRouteSnapshotChannelType returns the provider type frozen at submission.
// New snapshots reject a channel row that was repurposed under the same ID;
// legacy snapshots retain the pre-snapshot compatibility behavior.
func TaskRouteSnapshotChannelType(snapshot *model.TaskRouteSnapshot, channel *model.Channel) (int, error) {
	if snapshot == nil || snapshot.SchemaVersion < taskRouteSnapshotSchemaVersion {
		if channel == nil {
			return 0, fmt.Errorf("task route channel is nil")
		}
		return channel.Type, nil
	}
	if err := snapshot.Validate(); err != nil {
		return 0, err
	}
	if !constant.IsValidChannelType(snapshot.ChannelType) {
		return 0, fmt.Errorf("task route snapshot channel type is missing")
	}
	if channel != nil && channel.Type != snapshot.ChannelType {
		return 0, fmt.Errorf("task route snapshot channel type mismatch: snapshot=%d current=%d", snapshot.ChannelType, channel.Type)
	}
	return snapshot.ChannelType, nil
}

// TaskRouteSnapshotAdapterPlatform returns the exact task adaptor registry key
// captured at submission. The fallback is only for legacy rows.
func TaskRouteSnapshotAdapterPlatform(snapshot *model.TaskRouteSnapshot, fallback constant.TaskPlatform) (constant.TaskPlatform, error) {
	if snapshot == nil || snapshot.SchemaVersion < taskRouteSnapshotSchemaVersion {
		return fallback, nil
	}
	if err := snapshot.Validate(); err != nil {
		return "", err
	}
	if !constant.IsValidChannelType(snapshot.ChannelType) {
		return "", fmt.Errorf("task route snapshot channel type is invalid: %d", snapshot.ChannelType)
	}
	platform := constant.TaskPlatform(strings.TrimSpace(snapshot.AdapterPlatform))
	if platform == "" {
		return "", fmt.Errorf("task route snapshot adapter platform is missing")
	}
	identityChannelType, identityPlatform, err := parseTaskRouteAdapterIdentity(snapshot.AdapterIdentity)
	if err != nil {
		return "", err
	}
	if identityChannelType != snapshot.ChannelType {
		return "", fmt.Errorf("task route snapshot adapter identity channel type mismatch: snapshot=%d identity=%d", snapshot.ChannelType, identityChannelType)
	}
	if identityPlatform != platform {
		return "", fmt.Errorf("task route snapshot adapter identity platform mismatch: snapshot=%q identity=%q", platform, identityPlatform)
	}
	if expected, ok := taskAdapterPlatformForChannelType(snapshot.ChannelType); ok && platform != expected {
		return "", fmt.Errorf("task route snapshot adapter platform mismatch: channel_type=%d expected=%q got=%q", snapshot.ChannelType, expected, platform)
	}
	return platform, nil
}

// ValidateTaskRouteSnapshotChannelCompatibility verifies that a current-schema
// task snapshot still belongs to the channel row selected for a continuation.
// A channel ID is not a sufficient identity: administrators can reuse that ID
// after changing its provider type or task adapter. Legacy snapshots retain
// their explicit compatibility path and are validated by the phase-specific
// helpers when they are replayed.
func ValidateTaskRouteSnapshotChannelCompatibility(snapshot *model.TaskRouteSnapshot, channel *model.Channel) error {
	if snapshot == nil || snapshot.SchemaVersion < taskRouteSnapshotSchemaVersion {
		return nil
	}
	if channel == nil {
		return fmt.Errorf("task route snapshot channel is nil")
	}
	if snapshot.ChannelID != channel.Id {
		return fmt.Errorf("task route snapshot channel ID mismatch: snapshot=%d current=%d", snapshot.ChannelID, channel.Id)
	}
	if _, err := TaskRouteSnapshotChannelType(snapshot, channel); err != nil {
		return err
	}
	if _, err := TaskRouteSnapshotAdapterPlatform(snapshot, ""); err != nil {
		return err
	}
	return nil
}

// taskAdapterPlatformForChannelType returns the canonical registry key for
// task adaptors. Most task providers use their numeric channel type; Suno and
// Midjourney use their named platform keys. The boolean is kept so a future
// channel type can extend the registry without making this snapshot schema
// reject a valid non-numeric adaptor key.
func taskAdapterPlatformForChannelType(channelType int) (constant.TaskPlatform, bool) {
	if !constant.IsValidChannelType(channelType) {
		return "", false
	}
	switch channelType {
	case constant.ChannelTypeSunoAPI:
		return constant.TaskPlatformSuno, true
	case constant.ChannelTypeMidjourney, constant.ChannelTypeMidjourneyPlus:
		return constant.TaskPlatformMidjourney, true
	case constant.ChannelTypeVidu:
		return constant.TaskPlatformVidu, true
	default:
		return constant.TaskPlatform(strconv.Itoa(channelType)), true
	}
}

func taskRouteAdapterIdentity(channelType int, platform constant.TaskPlatform) string {
	return fmt.Sprintf("%s%d/%s", taskRouteAdapterIdentityPrefix, channelType, strings.TrimSpace(string(platform)))
}

func parseTaskRouteAdapterIdentity(identity string) (int, constant.TaskPlatform, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return 0, "", fmt.Errorf("task route snapshot adapter identity is missing")
	}
	if !strings.HasPrefix(identity, taskRouteAdapterIdentityPrefix) {
		return 0, "", fmt.Errorf("task route snapshot adapter identity has unsupported format")
	}
	parts := strings.Split(strings.TrimPrefix(identity, taskRouteAdapterIdentityPrefix), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return 0, "", fmt.Errorf("task route snapshot adapter identity has invalid components")
	}
	channelType, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || !constant.IsValidChannelType(channelType) {
		return 0, "", fmt.Errorf("task route snapshot adapter identity channel type is invalid")
	}
	platform := constant.TaskPlatform(strings.TrimSpace(parts[1]))
	return channelType, platform, nil
}

func taskRouteSnapshotOperation(snapshot *model.TaskRouteSnapshot, phase string) string {
	if snapshot == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "poll", "polling", "status":
		if operation := strings.TrimSpace(snapshot.PollingOperation); operation != "" {
			return operation
		}
	case "content", "download":
		if operation := strings.TrimSpace(snapshot.ContentOperation); operation != "" {
			return operation
		}
	}
	return strings.TrimSpace(snapshot.Operation)
}

func taskHeaderProfileSnapshot(c *gin.Context, channelID int, strategy *dto.HeaderProfileStrategy, attemptIndex int) (string, string, string, error) {
	if strategy == nil || !strategy.Enabled {
		return "", "", "", nil
	}
	selection, err := PrepareChannelRuntimeHeaderProfileSelection(c, channelID, strategy, attemptIndex)
	if err != nil {
		return "", "", "", err
	}
	profileID := strings.TrimSpace(selection.ProfileID)
	if profileID == "" {
		return "", "", "", fmt.Errorf("selected profile identity is missing")
	}
	profileVersion := ""
	if _, version, ok := strings.Cut(profileID, "@"); ok {
		profileVersion = strings.TrimSpace(version)
	} else if profile, exists := dto.ResolveHeaderProfile(profileID, strategy.Profiles); exists && profile.VersionMeta != nil {
		profileVersion = strings.TrimSpace(profile.VersionMeta.Version)
	}
	profileFingerprint := model.ComputeTaskRouteHeaderProfileFingerprint(selection.Headers)
	if profileFingerprint == "" {
		return "", "", "", fmt.Errorf("selected profile content is empty")
	}
	return profileID, profileVersion, profileFingerprint, nil
}

// taskRouteSnapshotHeaderPolicy freezes the static headers that were effective
// for the submission attempt. Profile values are merged first and explicit
// channel/tag/param overrides win, matching relay/channel's request builder.
// Values derived from the incoming client request cannot be replayed safely:
// asynchronous polling and content requests have no trustworthy client header
// context. Reject them explicitly instead of silently creating a task whose
// later phases use a different request identity.
func taskRouteSnapshotHeaderPolicy(info *relaycommon.RelayInfo, c *gin.Context, profileID string) (map[string]string, error) {
	result := make(map[string]string)
	if info == nil {
		return result, nil
	}
	// RuntimePassedRequestHeaders records values copied from the submission
	// request. They are intentionally not persisted in a task snapshot because
	// they may contain request-scoped credentials or session metadata. This
	// guard also covers values produced outside param_override, so no caller can
	// bypass the explicit rejection in prepareTaskRouteSnapshotParamOverride.
	if len(info.RuntimePassedRequestHeaders) > 0 {
		names := make([]string, 0, len(info.RuntimePassedRequestHeaders))
		for name := range info.RuntimePassedRequestHeaders {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				return nil, fmt.Errorf("task route snapshot header policy contains an empty client-dynamic header name")
			}
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) > 0 {
			return nil, fmt.Errorf("task route snapshot header policy depends on client-dynamic headers: %s", strings.Join(names, ", "))
		}
	}

	profileHeaders := map[string]string{}
	if c != nil && info.ChannelMeta != nil {
		strategy := info.ChannelMeta.ChannelOtherSettings.HeaderProfileStrategy
		if strategy != nil && strategy.Enabled {
			selection, ok := GetChannelRuntimeHeaderProfileSelection(c)
			if ok && strings.TrimSpace(selection.ProfileID) == strings.TrimSpace(profileID) {
				profileHeaders = selection.Headers
			}
		}
	}
	for name, value := range profileHeaders {
		if err := addTaskRouteSnapshotStaticHeader(result, name, value, true); err != nil {
			return nil, err
		}
	}

	dynamicNames := make(map[string]struct{})
	for name := range info.RuntimePassedRequestHeaders {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			return nil, fmt.Errorf("task route snapshot header policy contains an empty client-dynamic header name")
		}
		dynamicNames[name] = struct{}{}
	}
	normalizedOverrides := make(map[string]string)
	headerOverrideSource, err := relaycommon.GetEffectiveHeaderOverrideWithError(info)
	if err != nil {
		return nil, fmt.Errorf("header override is invalid: %w", err)
	}
	for name, rawValue := range headerOverrideSource {
		if isTaskRouteSnapshotHeaderPassthroughRule(name) {
			return nil, fmt.Errorf("task route snapshot header policy contains client-dynamic passthrough rule: %s", strings.TrimSpace(name))
		}
		canonicalName := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
		if canonicalName == "" {
			continue
		}
		if _, dynamic := dynamicNames[strings.ToLower(canonicalName)]; dynamic {
			return nil, fmt.Errorf("task route snapshot header policy depends on client-dynamic header: %s", canonicalName)
		}
		value, ok := rawValue.(string)
		if !ok {
			return nil, fmt.Errorf("header override %q is not a string", name)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		value = strings.ReplaceAll(value, "{api_key}", taskRouteSnapshotAPIKey(info))
		if value == "" {
			continue
		}
		if err := addTaskRouteSnapshotStaticHeader(normalizedOverrides, canonicalName, value, false); err != nil {
			return nil, err
		}
	}
	for name, value := range normalizedOverrides {
		// Explicit channel/tag overrides intentionally win over the selected
		// profile, but collisions within the override map were rejected above.
		result[name] = value
	}
	return result, nil
}

func taskRouteSnapshotAPIKey(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if strings.TrimSpace(info.ApiKey) != "" {
		return info.ApiKey
	}
	if info.ChannelMeta != nil {
		return info.ChannelMeta.ApiKey
	}
	return ""
}

func addTaskRouteSnapshotStaticHeader(target map[string]string, name string, value string, skipUnsafe bool) error {
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" || value == "" {
		return nil
	}
	if strings.Contains(value, "{client_header:") {
		return fmt.Errorf("header policy contains client-dynamic placeholder for %s", name)
	}
	if !dto.IsValidHeaderFieldName(name) {
		return fmt.Errorf("header policy contains invalid header name: %s", name)
	}
	name = textproto.CanonicalMIMEHeaderKey(name)
	if skipUnsafe && isRuntimeProfileUnsafeHeader(name) {
		return nil
	}
	for existingName, existingValue := range target {
		if !strings.EqualFold(existingName, name) {
			continue
		}
		if existingValue == value {
			return nil
		}
		return fmt.Errorf("header policy contains duplicate header names: %s", name)
	}
	target[name] = value
	return nil
}

func isTaskRouteSnapshotHeaderPassthroughRule(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "*" || strings.HasPrefix(name, "re:") || strings.HasPrefix(name, "regex:")
}

// TaskRouteSnapshotHeaderProfileRef returns the stable profile reference to
// use for follow-up requests. When only a base profile ID was persisted, append
// its captured version so a later "latest" resolution cannot silently change
// the task's client identity. Empty means no profile was selected at submit.
func TaskRouteSnapshotHeaderProfileRef(snapshot *model.TaskRouteSnapshot) string {
	if snapshot == nil {
		return ""
	}
	profileID := strings.TrimSpace(snapshot.HeaderProfileID)
	if profileID == "" {
		return ""
	}
	if snapshot.HeaderProfileVersion != "" && !strings.Contains(profileID, "@") {
		return profileID + "@" + strings.TrimSpace(snapshot.HeaderProfileVersion)
	}
	return profileID
}

func taskRouteSnapshotLegacyAction(snapshot *model.TaskRouteSnapshot) string {
	if snapshot == nil {
		return ""
	}
	operation := strings.TrimSpace(snapshot.Operation)
	if operation == "" {
		return ""
	}
	parts := strings.Split(operation, ".")
	if len(parts) == 0 {
		return ""
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "poll" || last == "content" || last == "submit" {
		if len(parts) < 2 {
			return ""
		}
		last = strings.TrimSpace(parts[len(parts)-2])
	}
	return last
}

// taskRouteSnapshotPhaseFallbackPath supplies the compatibility path for
// schema-1/2 rows that predate phase-specific path fields. New schema-v3 rows
// are deliberately strict and never consult the current channel mapping.
func taskRouteSnapshotPhaseFallbackPath(snapshot *model.TaskRouteSnapshot, phase string, channel *model.Channel) string {
	if snapshot == nil || channel == nil || snapshot.SchemaVersion >= taskRouteSnapshotSchemaVersion {
		return ""
	}
	action := taskRouteSnapshotLegacyAction(snapshot)
	effectiveModel := strings.TrimSpace(snapshot.EffectiveUpstreamModel)
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "poll", "polling", "status":
		return TaskPollingRoutePathForModel(channel.Type, effectiveModel, action)
	case "content", "download":
		return TaskContentRoutePathForModel(channel.Type, effectiveModel, action)
	default:
		if path := relaycommon.NormalizeRoutePath(snapshot.SubmitPath); path != "/" {
			return path
		}
		return TaskPollingRoutePathForModel(channel.Type, effectiveModel, action)
	}
}

func taskRouteSnapshotPhaseFallbackOperation(snapshot *model.TaskRouteSnapshot, phase string, channel *model.Channel) string {
	if snapshot == nil || channel == nil || snapshot.SchemaVersion >= taskRouteSnapshotSchemaVersion {
		return ""
	}
	operation := TaskRouteOperation(channel.Type, constant.TaskPlatform(strings.TrimSpace(snapshot.AdapterPlatform)), taskRouteSnapshotLegacyAction(snapshot))
	if strings.TrimSpace(snapshot.AdapterPlatform) == "" {
		operation = TaskRouteOperation(channel.Type, "", taskRouteSnapshotLegacyAction(snapshot))
	}
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "poll", "polling", "status":
		return taskRoutePhaseOperation(operation, "poll")
	case "content", "download":
		return taskRoutePhaseOperation(operation, "content")
	default:
		return operation
	}
}

// TaskRouteIdentityFromSnapshot materializes the exact health identity for a
// follow-up phase without consulting the channel's current model mapping.
func TaskRouteIdentityFromSnapshot(snapshot *model.TaskRouteSnapshot, phase string, channel *model.Channel) (relaycommon.ChannelRouteIdentity, error) {
	if snapshot == nil {
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("task route snapshot is missing")
	}
	if channel == nil || channel.Id <= 0 {
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("task route snapshot channel is invalid")
	}
	if err := snapshot.Validate(); err != nil {
		return relaycommon.ChannelRouteIdentity{}, err
	}
	if snapshot.SchemaVersion >= taskRouteSnapshotSchemaVersion && snapshot.ChannelID <= 0 {
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("task route snapshot channel ID is missing")
	}
	if snapshot.ChannelID > 0 && snapshot.ChannelID != channel.Id {
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("task route snapshot channel mismatch: snapshot=%d current=%d", snapshot.ChannelID, channel.Id)
	}
	if _, err := TaskRouteSnapshotChannelType(snapshot, channel); err != nil {
		return relaycommon.ChannelRouteIdentity{}, err
	}
	if _, err := TaskRouteSnapshotAdapterPlatform(snapshot, ""); err != nil {
		return relaycommon.ChannelRouteIdentity{}, err
	}
	if err := validateTaskRouteCredentialScope(snapshot, channel); err != nil {
		return relaycommon.ChannelRouteIdentity{}, err
	}
	path, fingerprint := snapshot.SubmitPath, snapshot.RouteFingerprint
	operation := taskRouteSnapshotOperation(snapshot, phase)
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "poll", "polling", "status":
		path, fingerprint = snapshot.PollingPath, snapshot.PollingRouteFingerprint
	case "content", "download":
		path, fingerprint = snapshot.ContentPath, snapshot.ContentRouteFingerprint
	case "submit", "submission", "":
	default:
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("unknown task route snapshot phase %q", phase)
	}
	path = relaycommon.NormalizeRoutePath(path)
	if path == "/" {
		path = taskRouteSnapshotPhaseFallbackPath(snapshot, phase, channel)
	}
	if path == "/" && snapshot.ChannelType != constant.ChannelTypeJimeng {
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("task route snapshot path is missing for phase %s", phase)
	}
	if strings.TrimSpace(operation) == "" {
		operation = taskRouteSnapshotPhaseFallbackOperation(snapshot, phase, channel)
	}
	if snapshot.SchemaVersion >= taskRouteSnapshotSchemaVersion && strings.TrimSpace(operation) == "" {
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("task route snapshot operation is missing for phase %s", phase)
	}
	persistedFingerprint := strings.TrimSpace(fingerprint)
	if persistedFingerprint == "" && snapshot.SchemaVersion >= taskRouteSnapshotSchemaVersion {
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("task route snapshot fingerprint is missing for phase %s", phase)
	}
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         strings.TrimSpace(snapshot.RequestedModel),
		EffectiveUpstreamModel: strings.TrimSpace(snapshot.EffectiveUpstreamModel),
		FinalProtocol:          strings.TrimSpace(snapshot.Protocol),
		Operation:              operation,
		UpstreamPath:           path,
		ChannelID:              channel.Id,
		MultiKeyIndex:          snapshot.MultiKeyIndex,
		CredentialScoped:       snapshot.CredentialScoped,
		// Recompute the fingerprint from the persisted route fields. Passing the
		// persisted value into Normalized would make it compare equal to itself
		// and would fail to detect a tampered or stale snapshot.
		Fingerprint: "",
	}.Normalized()
	if route.RequestedModel == "" || route.EffectiveUpstreamModel == "" || route.FinalProtocol == "" {
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("task route snapshot identity is incomplete")
	}
	if persistedFingerprint != "" && route.Fingerprint != persistedFingerprint {
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("task route snapshot fingerprint mismatch")
	}
	return route, nil
}

// validateTaskRouteCredentialScope prevents a current-schema task route from
// being replayed against a channel whose multi-key mode no longer matches the
// submission. A scoped snapshot is tied to one concrete key index; a current
// multi-key channel must not accept an unscoped snapshot and a single-key
// channel must not silently downgrade a scoped route. The concrete key value
// is checked by ResolveTaskCredential at each request boundary.
func validateTaskRouteCredentialScope(snapshot *model.TaskRouteSnapshot, channel *model.Channel) error {
	if snapshot == nil || snapshot.SchemaVersion < model.TaskRouteSnapshotSchemaVersion {
		return nil
	}
	if channel == nil {
		return fmt.Errorf("task route snapshot channel is nil")
	}
	if snapshot.CredentialScoped != channel.ChannelInfo.IsMultiKey {
		if snapshot.CredentialScoped {
			return fmt.Errorf("task route snapshot credential scope requires multi-key channel %d", channel.Id)
		}
		return fmt.Errorf("task route snapshot is missing credential scope for multi-key channel %d", channel.Id)
	}
	if !snapshot.CredentialScoped {
		return nil
	}
	keys := channel.GetKeys()
	if snapshot.MultiKeyIndex < 0 || snapshot.MultiKeyIndex >= len(keys) {
		return fmt.Errorf("task route snapshot credential index %d is no longer available for channel %d", snapshot.MultiKeyIndex, channel.Id)
	}
	if !isTaskPollingKeyEnabled(channel, snapshot.MultiKeyIndex) {
		return fmt.Errorf("task route snapshot credential is disabled for channel %d (key index %d)", channel.Id, snapshot.MultiKeyIndex)
	}
	return nil
}

// BeginTaskHealthAttempt prefers the persisted snapshot and explicitly falls
// back to the legacy mapping path only for rows created before snapshots were
// introduced.
func BeginTaskHealthAttempt(parent context.Context, task *model.Task, channel *model.Channel, key string, phase string) (*ChannelHealthAttempt, error) {
	if task == nil || channel == nil {
		return nil, fmt.Errorf("task or channel is nil")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("task health credential is empty")
	}
	resolvedKey, credentialErr := ResolveTaskCredential(channel, task.PrivateData.RouteSnapshot, key)
	if credentialErr != nil {
		return nil, credentialErr
	}
	key = resolvedKey
	group := ResolveTaskHealthGroup(task, channel)
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && snapshot.SchemaVersion >= 1 {
		recordTaskRouteSnapshotCompatibilityReplay(parent, task, snapshot.SchemaVersion, phase)
		route, err := TaskRouteIdentityFromSnapshot(snapshot, phase, channel)
		if err != nil {
			return nil, err
		}
		baseURL, err := TaskRouteSnapshotBaseURL(snapshot, channel)
		if err != nil {
			return nil, err
		}
		if snapshot.SchemaVersion >= taskRouteSnapshotSchemaVersion {
			providerRuntime, runtimeErr := TaskRouteSnapshotProviderRuntime(snapshot)
			if runtimeErr != nil {
				return nil, runtimeErr
			}
			proxy, proxyErr := TaskRouteSnapshotProxy(snapshot, channel)
			if proxyErr != nil {
				return nil, proxyErr
			}
			return BeginChannelHealthAttemptWithRouteAndBaseURLAndProviderRuntime(
				parent,
				group,
				route,
				channel,
				key,
				baseURL,
				providerRuntime,
				proxy,
			)
		}
		return BeginChannelHealthAttemptWithRouteAndBaseURL(parent, group, route, channel, key, baseURL)
	}
	requestedModel := ResolveTaskOriginModelName(task)
	effectiveModel := ResolveTaskPersistedEffectiveModelName(task)
	if strings.TrimSpace(effectiveModel) == "" {
		return nil, fmt.Errorf("%w: task %s", ErrTaskRouteMappingMissing, strings.TrimSpace(task.TaskID))
	}
	path := TaskHealthRoutePathForModel(channel.Type, effectiveModel)
	if strings.EqualFold(strings.TrimSpace(phase), "content") {
		path = TaskContentRoutePathForModel(channel.Type, effectiveModel, task.Action)
	}
	return BeginChannelHealthAttemptWithEffectiveModel(parent, group, requestedModel, effectiveModel, channel, path, key)
}
