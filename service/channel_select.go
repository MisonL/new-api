package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

type RetryParam struct {
	Ctx        *gin.Context
	TokenGroup string
	ModelName  string
	Retry      *int
	// CandidatePreflight is evaluated before route priority and weight. It is
	// request-aware and may return an explicit error for malformed input or an
	// unavailable prerequisite; false only means that this channel is not
	// eligible for the current request.
	CandidatePreflight func(*model.Channel) (bool, error)
	resetNextTry       bool
}

const (
	responsesCompactRouteDecisionNamespaceUnsupportedPreferred         = "namespace_unsupported_preferred"
	responsesCompactRouteDecisionNamespaceUnsupportedPreferredFallback = "namespace_unsupported_preferred_fallback"
)

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.resetNextTry {
		p.resetNextTry = false
		return
	}
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

func (p *RetryParam) ResetRetryNextTry() {
	p.resetNextTry = true
}

// CacheGetRandomSatisfiedChannel tries to get a random channel that satisfies the requirements.
// 尝试获取一个满足要求的随机渠道。
//
// For "auto" tokenGroup with cross-group Retry enabled:
// 对于启用了跨分组重试的 "auto" tokenGroup：
//
//   - Each group will exhaust all its priorities before moving to the next group.
//     每个分组会用完所有优先级后才会切换到下一个分组。
//
//   - Uses ContextKeyAutoGroupIndex to track current group index.
//     使用 ContextKeyAutoGroupIndex 跟踪当前分组索引。
//
//   - Uses ContextKeyAutoGroupRetryIndex to track the global Retry count when current group started.
//     使用 ContextKeyAutoGroupRetryIndex 跟踪当前分组开始时的全局重试次数。
//
//   - priorityRetry = Retry - startRetryIndex, represents the priority level within current group.
//     priorityRetry = Retry - startRetryIndex，表示当前分组内的优先级级别。
//
//   - When GetRandomSatisfiedChannel returns nil (priorities exhausted), moves to next group.
//     当 GetRandomSatisfiedChannel 返回 nil（优先级用完）时，切换到下一个分组。
//
// Example flow (2 groups, each with 2 priorities, RetryTimes=3):
// 示例流程（2个分组，每个有2个优先级，RetryTimes=3）：
//
//	Retry=0: GroupA, priority0 (startRetryIndex=0, priorityRetry=0)
//	         分组A, 优先级0
//
//	Retry=1: GroupA, priority1 (startRetryIndex=0, priorityRetry=1)
//	         分组A, 优先级1
//
//	Retry=2: GroupA exhausted -> GroupB, priority0 (startRetryIndex=2, priorityRetry=0)
//	         分组A用完 -> 分组B, 优先级0
//
//	Retry=3: GroupB, priority1 (startRetryIndex=2, priorityRetry=1)
//	         分组B, 优先级1
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	for {
		channel, selectGroup, err := cacheGetRandomSatisfiedChannel(param)
		if err != nil || channel == nil || param == nil || param.Ctx == nil {
			return channel, selectGroup, err
		}

		decision := EvaluateChannelHealthSelectionForChannelAtBaseRoute(param.Ctx, selectGroup, param.ModelName, channel)
		if decision.Allow {
			if model.IsResponsesCompactionNamespacePreferred(channel, param.ModelName) {
				common.SetContextKey(param.Ctx, constant.ContextKeyResponsesCompactRouteDecision, responsesCompactRouteDecisionNamespaceUnsupportedPreferred)
			} else if common.GetContextKeyString(param.Ctx, constant.ContextKeyResponsesCompactRouteDecision) == responsesCompactRouteDecisionNamespaceUnsupportedPreferred {
				common.SetContextKey(param.Ctx, constant.ContextKeyResponsesCompactRouteDecision, responsesCompactRouteDecisionNamespaceUnsupportedPreferredFallback)
			}
			return channel, selectGroup, nil
		}
		if !ExcludeChannelHealthSelectionForRoute(param.Ctx, decision.Group, decision.ModelName, decision.Route) {
			logger.LogError(param.Ctx, fmt.Sprintf("channel health selection returned an already excluded channel: channel_id=%d", channel.Id))
			return nil, selectGroup, nil
		}
		MarkChannelHealthSelectionRetryableForRoute(param.Ctx, decision.Group, decision.ModelName, decision.Route)
	}
}

func cacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	if param == nil {
		return nil, "", errors.New("channel retry parameters are nil")
	}
	if param.Ctx == nil {
		return nil, param.TokenGroup, errors.New("channel retry context is nil")
	}
	var channel *model.Channel
	var err error
	selectGroup := param.TokenGroup
	userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)
	requestBodySize, requestBodyLimitTTLHours, requestBodyLimitNow := getRequestBodyLimitSelectorParams(param.Ctx)
	selectionDiagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	// Keep one resolver on the Gin request context for the complete retry
	// lifecycle. Recreating it for every selector invocation would allow a
	// channel edit between retries to change the effective upstream model and
	// make health attribution disagree with the relay request that follows.
	routeModelResolver := channelRouteModelResolverForContext(param.Ctx)
	defer bindChannelRouteSelectionDiagnostics(param.Ctx, selectionDiagnostics)

	if common.IsAutoGroup(param.TokenGroup) {
		if len(setting.GetAutoGroups()) == 0 {
			return nil, selectGroup, errors.New("auto groups is not enabled")
		}
		autoGroups := GetUserAutoGroup(userGroup)

		// startGroupIndex: the group index to start searching from
		// startGroupIndex: 开始搜索的分组索引
		startGroupIndex := 0
		crossGroupRetry := common.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)

		if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
			if idx, ok := lastGroupIndex.(int); ok {
				startGroupIndex = idx
			}
		}

		for i := startGroupIndex; i < len(autoGroups); i++ {
			autoGroup := autoGroups[i]
			// Calculate priorityRetry for current group
			// 计算当前分组的 priorityRetry
			priorityRetry := param.GetRetry()
			// If moved to a new group, reset priorityRetry and update startRetryIndex
			// 如果切换到新分组，重置 priorityRetry 并更新 startRetryIndex
			if i > startGroupIndex {
				priorityRetry = 0
			}
			logger.LogDebug(param.Ctx, "Auto selecting group: %s, priorityRetry: %d", autoGroup, priorityRetry)

			excludedChannels, hasHealthExclusions := getChannelSelectionExcludedChannels(param.Ctx, autoGroup, param.ModelName)
			if (priorityRetry > 0 || hasHealthExclusions) && len(excludedChannels) > 0 {
				channel, err = model.GetRandomSatisfiedChannelWithOptions(
					autoGroup,
					param.ModelName,
					0,
					excludedChannels,
					model.SatisfiedChannelSelectionOptions{
						RequestBodySize:       requestBodySize,
						BodyLimitTTLHours:     requestBodyLimitTTLHours,
						Now:                   requestBodyLimitNow,
						ExcludeBeforePriority: true,
						RouteCandidateFilter:  channelHealthRouteCandidateFilter(param.Ctx, autoGroup, param.ModelName),
						CandidatePreflight:    param.CandidatePreflight,
						RouteModelResolver:    routeModelResolver,
						Diagnostics:           selectionDiagnostics,
					},
				)
				if err != nil {
					return nil, autoGroup, err
				}
				if channel != nil {
					common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
					selectGroup = autoGroup
					logger.LogDebug(param.Ctx, "Auto selected untried channel at highest priority in group: %s", autoGroup)
					common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
					break
				}
			}
			channel, err = model.GetRandomSatisfiedChannelWithOptions(
				autoGroup,
				param.ModelName,
				priorityRetry,
				excludedChannels,
				model.SatisfiedChannelSelectionOptions{
					RequestBodySize:       requestBodySize,
					BodyLimitTTLHours:     requestBodyLimitTTLHours,
					Now:                   requestBodyLimitNow,
					ExcludeBeforePriority: requestBodySize > 0,
					RouteCandidateFilter:  channelHealthRouteCandidateFilter(param.Ctx, autoGroup, param.ModelName),
					CandidatePreflight:    param.CandidatePreflight,
					RouteModelResolver:    routeModelResolver,
					Diagnostics:           selectionDiagnostics,
				},
			)
			if err != nil {
				return nil, autoGroup, err
			}
			if channel == nil {
				// Current group has no available channel for this model, try next group
				// 当前分组没有该模型的可用渠道，尝试下一个分组
				logger.LogDebug(param.Ctx, "No available channel in group %s for model %s at priorityRetry %d, trying next group", autoGroup, param.ModelName, priorityRetry)
				// 重置状态以尝试下一个分组
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
				// Reset retry counter so outer loop can continue for next group
				// 重置重试计数器，以便外层循环可以为下一个分组继续
				param.SetRetry(0)
				continue
			}
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
			selectGroup = autoGroup
			logger.LogDebug(param.Ctx, "Auto selected group: %s", autoGroup)

			// Prepare state for next retry
			// 为下一次重试准备状态
			if crossGroupRetry && priorityRetry >= common.RetryTimes {
				// Current group has exhausted all retries, prepare to switch to next group
				// This request still uses current group, but next retry will use next group
				// 当前分组已用完所有重试次数，准备切换到下一个分组
				// 本次请求仍使用当前分组，但下次重试将使用下一个分组
				logger.LogDebug(param.Ctx, "Current group %s retries exhausted (priorityRetry=%d >= RetryTimes=%d), preparing switch to next group for next retry", autoGroup, priorityRetry, common.RetryTimes)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				// Reset retry counter so outer loop can continue for next group
				// 重置重试计数器，以便外层循环可以为下一个分组继续
				param.SetRetry(0)
				param.ResetRetryNextTry()
			} else {
				// Stay in current group, save current state
				// 保持在当前分组，保存当前状态
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
			}
			break
		}
	} else {
		usedChannels, hasHealthExclusions := getChannelSelectionExcludedChannels(param.Ctx, param.TokenGroup, param.ModelName)
		if (param.GetRetry() > 0 || hasHealthExclusions) && len(usedChannels) > 0 {
			channel, err = model.GetRandomSatisfiedChannelWithOptions(
				param.TokenGroup,
				param.ModelName,
				0,
				usedChannels,
				model.SatisfiedChannelSelectionOptions{
					RequestBodySize:       requestBodySize,
					BodyLimitTTLHours:     requestBodyLimitTTLHours,
					Now:                   requestBodyLimitNow,
					ExcludeBeforePriority: true,
					RouteCandidateFilter:  channelHealthRouteCandidateFilter(param.Ctx, param.TokenGroup, param.ModelName),
					CandidatePreflight:    param.CandidatePreflight,
					RouteModelResolver:    routeModelResolver,
					Diagnostics:           selectionDiagnostics,
				},
			)
			if err != nil {
				return nil, param.TokenGroup, err
			}
			if channel != nil {
				return channel, param.TokenGroup, nil
			}
		}
		channel, err = model.GetRandomSatisfiedChannelWithOptions(
			param.TokenGroup,
			param.ModelName,
			param.GetRetry(),
			usedChannels,
			model.SatisfiedChannelSelectionOptions{
				RequestBodySize:       requestBodySize,
				BodyLimitTTLHours:     requestBodyLimitTTLHours,
				Now:                   requestBodyLimitNow,
				ExcludeBeforePriority: requestBodySize > 0,
				CandidateFilter:       nil,
				RouteCandidateFilter:  channelHealthRouteCandidateFilter(param.Ctx, param.TokenGroup, param.ModelName),
				CandidatePreflight:    param.CandidatePreflight,
				RouteModelResolver:    routeModelResolver,
				Diagnostics:           selectionDiagnostics,
			},
		)
		if err != nil {
			return nil, param.TokenGroup, err
		}
	}
	return channel, selectGroup, nil
}

// channelRouteModelResolverForContext returns a request-scoped resolver that
// memoizes each channel's first observed, validated model_mapping snapshot.
// The resolver is deliberately kept outside channel-scoped reset logic because
// it represents the immutable routing decision for the whole request, not the
// currently installed channel context.
func channelRouteModelResolverForContext(ctx *gin.Context) model.RouteModelResolver {
	if ctx == nil {
		return newChannelRouteModelResolver()
	}
	if resolver, ok := common.GetContextKeyType[model.RouteModelResolver](ctx, constant.ContextKeyChannelRouteModelResolver); ok && resolver != nil {
		return resolver
	}
	resolver := newChannelRouteModelResolver(ctx)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteModelResolver, resolver)
	return resolver
}

// ResolveChannelModelMappingForContext resolves a channel's model mapping
// through the request-scoped snapshot used by the selector. When the caller
// only has the lightweight channel identity assembled by the distributor,
// the already-installed context mapping is used as the channel snapshot.
// This keeps specific-channel, affinity, health, and relay paths on one
// immutable mapping decision without exposing the resolver implementation.
func ResolveChannelModelMappingForContext(ctx *gin.Context, channel *model.Channel, requestedModel string, compact bool) (string, bool, error) {
	if channel == nil {
		return "", false, nil
	}
	resolver := channelRouteModelResolverForContext(ctx)
	if !hasProvisionalChannelMapping(ctx, channel) {
		return resolver(channel, requestedModel, compact)
	}
	return withProvisionalChannelMapping(ctx, func() (string, bool, error) {
		return resolver(channel, requestedModel, compact)
	})
}

func hasProvisionalChannelMapping(ctx *gin.Context, channel *model.Channel) bool {
	if ctx == nil || channel == nil || !channelHealthContextMatchesChannel(ctx, channel) {
		return false
	}
	if strings.TrimSpace(common.GetContextKeyString(ctx, constant.ContextKeyChannelModelMapping)) == "" {
		return false
	}
	rawMapping := strings.TrimSpace(channel.GetModelMapping())
	if rawMapping == "" {
		return true
	}
	// Treat syntactically valid semantic-empty objects (for example "{}") as
	// an absent channel mapping. This lets the distributor's authoritative
	// request snapshot supplement a lightweight/cache channel without letting a
	// malformed persisted mapping be silently replaced.
	prepared, err := model.PrepareChannelModelMappingForRouting(rawMapping)
	return err == nil && prepared.IsEmpty()
}

func channelModelMappingIsSemanticallyEmpty(rawMapping string) bool {
	prepared, err := model.PrepareChannelModelMappingForRouting(strings.TrimSpace(rawMapping))
	return err == nil && prepared.IsEmpty()
}

func withProvisionalChannelMapping(ctx *gin.Context, fn func() (string, bool, error)) (string, bool, error) {
	if fn == nil {
		return "", false, errors.New("provisional mapping callback is nil")
	}
	if ctx == nil {
		return fn()
	}
	previous, existed := common.GetContextKey(ctx, constant.ContextKeyChannelRouteModelMappingProvisional)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteModelMappingProvisional, true)
	defer func() {
		if existed {
			common.SetContextKey(ctx, constant.ContextKeyChannelRouteModelMappingProvisional, previous)
		} else {
			common.SetContextKey(ctx, constant.ContextKeyChannelRouteModelMappingProvisional, false)
		}
	}()
	return fn()
}

func withProvisionalChannelMappingBool(ctx *gin.Context, fn func() bool) bool {
	if fn == nil {
		return false
	}
	if ctx == nil {
		return fn()
	}
	previous, existed := common.GetContextKey(ctx, constant.ContextKeyChannelRouteModelMappingProvisional)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteModelMappingProvisional, true)
	defer func() {
		if existed {
			common.SetContextKey(ctx, constant.ContextKeyChannelRouteModelMappingProvisional, previous)
		} else {
			common.SetContextKey(ctx, constant.ContextKeyChannelRouteModelMappingProvisional, false)
		}
	}()
	return fn()
}

// IsChannelEnabledForGroupModelForContext checks a concrete channel through
// the same request-scoped mapping snapshot used by the main selector. This is
// required by affinity and specific-channel paths, which otherwise could
// observe a different model_mapping after a channel edit during an in-flight
// request.
func IsChannelEnabledForGroupModelForContext(ctx *gin.Context, group string, modelName string, channelID int) bool {
	resolver := channelRouteModelResolverForContext(ctx)
	if channel, err := model.CacheGetChannel(channelID); err == nil && hasProvisionalChannelMapping(ctx, channel) {
		return withProvisionalChannelMappingBool(ctx, func() bool {
			return model.IsChannelEnabledForGroupModelWithResolver(group, modelName, channelID, resolver)
		})
	}
	return model.IsChannelEnabledForGroupModelWithResolver(group, modelName, channelID, resolver)
}

// IsChannelEnabledForAnyGroupModelForContext is the auto-group counterpart of
// IsChannelEnabledForGroupModelForContext.
func IsChannelEnabledForAnyGroupModelForContext(ctx *gin.Context, groups []string, modelName string, channelID int) bool {
	resolver := channelRouteModelResolverForContext(ctx)
	if channel, err := model.CacheGetChannel(channelID); err == nil && hasProvisionalChannelMapping(ctx, channel) {
		return withProvisionalChannelMappingBool(ctx, func() bool {
			return model.IsChannelEnabledForAnyGroupModelWithResolver(groups, modelName, channelID, resolver)
		})
	}
	return model.IsChannelEnabledForAnyGroupModelWithResolver(groups, modelName, channelID, resolver)
}

func bindChannelRouteSelectionDiagnostics(ctx *gin.Context, diagnostics *dto.ChannelRouteSelectionDiagnostics) {
	if ctx == nil || diagnostics == nil {
		return
	}
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteDiagnostics, *diagnostics)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteScope, diagnostics.RouteScope)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteWeightBPS, diagnostics.RouteWeightBPS)
	common.SetContextKey(ctx, constant.ContextKeyChannelRoutePriority, diagnostics.RoutePriority)
}

type channelRouteModelMemoKey struct {
	channelID      int
	rawMapping     string
	requestedModel string
	compact        bool
}

type channelRouteModelMemoEntry struct {
	snapshot      *model.ChannelModelMapping
	err           error
	rawMapping    string
	semanticEmpty bool
}

// newChannelRouteModelResolver prepares each distinct persisted mapping once
// per selection call and reuses the immutable snapshot for all candidate and
// health checks. The memo is intentionally request-scoped so a later channel
// edit cannot mutate an in-flight routing decision.
func newChannelRouteModelResolver(contexts ...*gin.Context) model.RouteModelResolver {
	// Resolvers attached to a Gin request pin even an explicitly empty mapping.
	// The no-context form remains source-compatible for low-level callers and
	// tests that intentionally exercise the legacy empty-snapshot upgrade path.
	requestContext := (*gin.Context)(nil)
	if len(contexts) > 0 {
		requestContext = contexts[0]
	}
	strictEmptySnapshot := requestContext != nil
	// A non-empty mapping observed for a channel is immutable for the request.
	// A validated empty snapshot may be replaced when a later channel copy
	// carries a different raw value. This covers the distributor's lightweight
	// channel (empty mapping) followed by the authoritative context snapshot,
	// including semantically empty values such as "{}" or whitespace. Once a
	// non-empty snapshot has been observed, later edits cannot change routing.
	snapshotsByChannel := make(map[int]channelRouteModelMemoEntry)
	snapshotsByRawMapping := make(map[string]channelRouteModelMemoEntry)
	results := make(map[channelRouteModelMemoKey]channelRouteModelResolution)
	var mu sync.Mutex
	return func(channel *model.Channel, requestedModel string, compact bool) (string, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if channel == nil {
			return "", false, nil
		}
		provisional := requestContext != nil && common.GetContextKeyBool(requestContext, constant.ContextKeyChannelRouteModelMappingProvisional)
		channelMappingEmpty := strings.TrimSpace(channel.GetModelMapping()) == "" || channelModelMappingIsSemanticallyEmpty(channel.GetModelMapping())
		if provisional && channelMappingEmpty && channelHealthContextMatchesChannel(requestContext, channel) {
			if rawMapping := strings.TrimSpace(common.GetContextKeyString(requestContext, constant.ContextKeyChannelModelMapping)); rawMapping != "" {
				copyOfChannel := *channel
				copyOfChannel.ModelMapping = common.GetPointer(rawMapping)
				channel = &copyOfChannel
			}
		}
		rawMapping := channel.GetModelMapping()
		channelID := channel.Id
		var entry channelRouteModelMemoEntry
		if channelID > 0 {
			var ok bool
			entry, ok = snapshotsByChannel[channelID]
			if !ok || (entry.semanticEmpty && rawMapping != entry.rawMapping && (!strictEmptySnapshot || provisional)) {
				entry.snapshot, entry.err = model.PrepareChannelModelMappingForRouting(rawMapping)
				entry.rawMapping = rawMapping
				entry.semanticEmpty = entry.err == nil && entry.snapshot.IsEmpty()
				snapshotsByChannel[channelID] = entry
				if ok {
					for key := range results {
						if key.channelID == channelID {
							delete(results, key)
						}
					}
				}
			}
		} else {
			var ok bool
			entry, ok = snapshotsByRawMapping[rawMapping]
			if !ok {
				entry.snapshot, entry.err = model.PrepareChannelModelMappingForRouting(rawMapping)
				entry.rawMapping = rawMapping
				entry.semanticEmpty = entry.err == nil && entry.snapshot.IsEmpty()
				snapshotsByRawMapping[rawMapping] = entry
			}
		}
		key := channelRouteModelMemoKey{channelID: channelID, rawMapping: entry.rawMapping, requestedModel: strings.TrimSpace(requestedModel), compact: compact}
		if result, ok := results[key]; ok {
			return result.effectiveModel, result.mapped, result.err
		}
		if entry.err != nil {
			result := channelRouteModelResolution{err: entry.err}
			results[key] = result
			return "", false, entry.err
		}
		effectiveModel, mapped, err := entry.snapshot.Resolve(requestedModel, compact)
		if err == nil {
			// Provider/action-specific wire identities are part of the same
			// request-scoped route decision as model_mapping. Applying the rule
			// before candidate health/priority selection prevents a Vidu
			// reference-image request from being selected and snapshotted under
			// an alias that the adaptor later replaces with viduq2.
			if requestContext != nil {
				if intent, ok := common.GetContextKeyType[relaycommon.TaskRouteIntent](requestContext, constant.ContextKeyTaskRouteIntent); ok {
					wireModel := relaycommon.ResolveTaskWireModel(channel.Type, intent.Action, effectiveModel)
					mapped = mapped || wireModel != strings.TrimSpace(requestedModel)
					effectiveModel = wireModel
				}
			}
		}
		result := channelRouteModelResolution{effectiveModel: effectiveModel, mapped: mapped, err: err}
		results[key] = result
		return effectiveModel, mapped, err
	}
}

type channelRouteModelResolution struct {
	effectiveModel string
	mapped         bool
	err            error
}

func channelHealthCandidateFilter(ctx *gin.Context, group string, modelName string, resolver model.RouteModelResolver) func(*model.Channel) bool {
	if ctx == nil {
		return nil
	}
	_, compactRequest := ratio_setting.CompactBaseModelName(strings.TrimSpace(modelName))
	if resolver == nil {
		resolver = channelRouteModelResolverForContext(ctx)
	}
	return func(channel *model.Channel) bool {
		if channel == nil {
			return false
		}
		effectiveModel, _, err := resolver(channel, modelName, compactRequest)
		if err != nil {
			// A resolver error means the candidate's route identity is not
			// trustworthy. Health must not fail open for an unknown resolver
			// failure, otherwise a malformed or unavailable route can be selected
			// and later surface as a misleading upstream 504.
			logger.LogWarn(ctx, fmt.Sprintf("channel model mapping rejected during selection: channel_id=%d model=%s error=%s", channel.Id, strings.TrimSpace(modelName), common.MaskSensitiveInfo(err.Error())))
			return false
		}
		return !IsChannelHealthSelectionExcludedForRouteWithEffectiveModel(ctx, group, modelName, channel, effectiveModel)
	}
}

// channelHealthRouteCandidateFilter evaluates health state against the
// effective model attached to the concrete route candidate. This is required
// for compact/normalized pools where one physical channel can represent more
// than one mapped upstream route.
func channelHealthRouteCandidateFilter(ctx *gin.Context, group string, modelName string) model.RouteCandidateFilter {
	if ctx == nil {
		return nil
	}
	// A channel skipped because its concurrency lease could not be acquired has
	// not sent an upstream request. Keep this request-local capacity exclusion
	// separate from health state and use_channel; it is an eligibility filter
	// for the current route candidate only.
	concurrencyExclusions := ChannelConcurrencyExcludedChannels(ctx)
	return func(channel *model.Channel, _ string, effectiveModel string) bool {
		if channel == nil || strings.TrimSpace(effectiveModel) == "" {
			return false
		}
		if _, excluded := concurrencyExclusions[channel.Id]; excluded {
			return false
		}
		return !IsChannelHealthSelectionExcludedForRouteWithEffectiveModel(ctx, group, modelName, channel, effectiveModel)
	}
}

func getChannelSelectionExcludedChannels(ctx *gin.Context, group string, modelName string) (map[int]struct{}, bool) {
	usedChannels := getUsedChannelSet(ctx)
	for channelID := range channelHealthSelectionRetryableChannels(ctx, group, modelName) {
		delete(usedChannels, channelID)
	}
	healthExclusions := channelHealthSelectionExclusions(ctx, group, modelName)
	if len(healthExclusions) == 0 {
		return usedChannels, false
	}
	// Health exclusions and upstream-attempt history share the selector's
	// retry/priority contract. Concurrency exclusions do not: they are
	// evaluated by channelHealthRouteCandidateFilter before a route can be
	// selected and must not make this function report a health exclusion or
	// advance priority on their behalf.
	merged := make(map[int]struct{}, len(usedChannels)+len(healthExclusions))
	for channelID := range usedChannels {
		merged[channelID] = struct{}{}
	}
	for channelID := range healthExclusions {
		merged[channelID] = struct{}{}
	}
	return merged, true
}

func getUsedChannelSet(ctx *gin.Context) map[int]struct{} {
	if ctx == nil {
		return nil
	}
	usedChannels := ctx.GetStringSlice("use_channel")
	if len(usedChannels) == 0 {
		return nil
	}
	excluded := make(map[int]struct{}, len(usedChannels))
	for _, raw := range usedChannels {
		channelId, err := strconv.Atoi(raw)
		if err != nil || channelId <= 0 {
			continue
		}
		excluded[channelId] = struct{}{}
	}
	return excluded
}

func getRequestBodyLimitSelectorParams(ctx *gin.Context) (int64, int, time.Time) {
	if ctx == nil {
		return 0, 0, time.Time{}
	}
	policy := model_setting.GetRequestBodyLimitPolicy()
	if !policy.Enabled {
		return 0, 0, time.Time{}
	}
	requestBodySize := common.GetContextKeyInt64(ctx, constant.ContextKeyRequestBodySize)
	if requestBodySize <= 0 {
		return 0, 0, time.Time{}
	}
	return requestBodySize, policy.TTLHours, time.Now().UTC()
}
