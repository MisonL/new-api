package model

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type routeModelCandidate struct {
	model             string
	compactRequest    bool
	compactFallback   bool
	compactAliasModel string
	// effectiveModel is populated by the selector after mapping resolution. It
	// lets compact capability checks consume the same immutable result instead
	// of parsing channel.model_mapping independently.
	effectiveModel         string
	mappingApplied         bool
	mappingSnapshotReady   bool
	compactCapabilityModel string
}

func getGroupModelRouteCandidates(modelName string) []string {
	routeCandidates := getGroupModelRouteCandidateMeta(modelName)
	candidates := make([]string, 0, len(routeCandidates))
	for _, candidate := range routeCandidates {
		candidates = append(candidates, candidate.model)
	}
	return candidates
}

func getGroupModelRouteCandidateMeta(modelName string) []routeModelCandidate {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil
	}
	baseModelName, isCompact := ratio_setting.CompactBaseModelName(modelName)
	candidates := []routeModelCandidate{{
		model:          modelName,
		compactRequest: isCompact,
		compactAliasModel: func() string {
			if isCompact {
				return modelName
			}
			return ""
		}(),
	}}
	if !common.GroupModelRouteHelperEnabled {
		return candidates
	}
	if isCompact {
		candidates = appendRouteCandidate(candidates, baseModelName, true)
		candidates = appendRouteCandidate(candidates, ratio_setting.FormatMatchingModelName(baseModelName), true)
		return candidates
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	candidates = appendRouteCandidate(candidates, normalized, false)
	return candidates
}

func appendRouteCandidate(candidates []routeModelCandidate, modelName string, compactFallback bool) []routeModelCandidate {
	if modelName == "" {
		return candidates
	}
	for _, candidate := range candidates {
		if candidate.model == modelName {
			return candidates
		}
	}
	candidates = append(candidates, routeModelCandidate{
		model:             modelName,
		compactRequest:    compactFallback,
		compactFallback:   compactFallback,
		compactAliasModel: candidates[0].compactAliasModel,
	})
	return candidates
}

func shouldPoolCompactRouteCandidates(candidates []routeModelCandidate) bool {
	// Compact requests intentionally pool the exact compact model and eligible base-model fallbacks
	// so explicit compact abilities do not monopolize compact traffic.
	return len(candidates) > 1 && candidates[0].compactRequest
}

func channelSupportsCompactRouteCandidate(channel *Channel, candidate routeModelCandidate) bool {
	if channel == nil {
		return false
	}
	if !candidate.compactRequest {
		return true
	}
	switch channel.Type {
	case constant.ChannelTypeOpenAI:
		settings, ok := channelResponsesCompactSettings(channel)
		if !ok {
			return false
		}
		if settings.HasDisabledResponsesCompact() {
			return false
		}
		baseModelName := candidate.model
		if compactBaseModelName, isCompact := ratio_setting.CompactBaseModelName(candidate.model); isCompact {
			baseModelName = compactBaseModelName
		}
		if settings.HasDisabledResponsesCompact() {
			return false
		}
		now := time.Now()
		switch settings.EffectiveResponsesCompactModeOrDefaultAt(now) {
		case dto.ResponsesCompactModeNative:
			return channelAllowsNativeCompactFallback(channel, candidate, settings, baseModelName, now)
		case dto.ResponsesCompactModeSynthetic:
			if settings.IsAutoResponsesCompact() && settings.HasActiveResponsesCompactAutoFallback(now) {
				return channelAllowsSyntheticCompactBaseFallback(channel, candidate, baseModelName)
			}
			return channelAllowsSyntheticCompactFallback(channel, candidate, baseModelName)
		case dto.ResponsesCompactModeDisabled:
			return false
		default:
			return false
		}
	case constant.ChannelTypeCodex:
		return true
	case constant.ChannelTypeAzure:
		baseModelName := candidate.model
		if compactBaseModelName, isCompact := ratio_setting.CompactBaseModelName(candidate.model); isCompact {
			baseModelName = compactBaseModelName
		}
		return channelAllowsAzureCompactRoute(channel, candidate, baseModelName)
	default:
		return false
	}
}

func channelResponsesCompactSettings(channel *Channel) (dto.ChannelOtherSettings, bool) {
	settings := dto.ChannelOtherSettings{}
	if channel == nil {
		return settings, false
	}
	if channel.OtherSettings == "" {
		return settings, true
	}
	if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
		return settings, false
	}
	return settings, true
}

func channelPrefersResponsesCompactionWhenNamespaceUnsupported(channel *Channel) bool {
	if channel == nil {
		return false
	}
	settings, ok := channelResponsesCompactSettings(channel)
	if !ok || !settings.PreferForResponsesCompactionWhenNamespaceUnsupportedOrDefault() {
		return false
	}
	return settings.ResolveResponsesNamespaceToolsCapability(channel.Type) == dto.ResponsesToolCapabilityUnsupported
}

// preferResponsesCompactionChannels returns the preferred compact candidates
// when at least one eligible channel explicitly lacks namespace support. Once
// every preferred channel is excluded, only ordinary candidates remain so the
// retry priority is evaluated from the ordinary pool instead of an exhausted
// preferred priority.
func preferResponsesCompactionChannels(channels []*Channel, compactRequest bool, excluded map[int]struct{}) []*Channel {
	if !compactRequest || len(channels) == 0 {
		return channels
	}
	preferred := make([]*Channel, 0, len(channels))
	fallback := make([]*Channel, 0, len(channels))
	for _, channel := range channels {
		if channel == nil || !channelPrefersResponsesCompactionWhenNamespaceUnsupported(channel) {
			fallback = append(fallback, channel)
			continue
		}
		if _, skip := excluded[channel.Id]; skip {
			continue
		}
		preferred = append(preferred, channel)
	}
	if len(preferred) > 0 {
		return preferred
	}
	if len(fallback) == 0 {
		return channels
	}
	return fallback
}

func preferResponsesCompactionChannelsWithWeights(channels []*Channel, weights []uint, compactRequest bool, excluded map[int]struct{}) ([]*Channel, []uint) {
	if !compactRequest || len(channels) == 0 {
		return channels, weights
	}
	preferredChannels := make([]*Channel, 0, len(channels))
	preferredWeights := make([]uint, 0, len(weights))
	fallbackChannels := make([]*Channel, 0, len(channels))
	fallbackWeights := make([]uint, 0, len(weights))
	for i, channel := range channels {
		weight := uint(0)
		if i < len(weights) {
			weight = weights[i]
		}
		if channel == nil || !channelPrefersResponsesCompactionWhenNamespaceUnsupported(channel) {
			fallbackChannels = append(fallbackChannels, channel)
			fallbackWeights = append(fallbackWeights, weight)
			continue
		}
		if _, skip := excluded[channel.Id]; skip {
			continue
		}
		preferredChannels = append(preferredChannels, channel)
		preferredWeights = append(preferredWeights, weight)
	}
	if len(preferredChannels) > 0 {
		return preferredChannels, preferredWeights
	}
	if len(fallbackChannels) == 0 {
		return channels, weights
	}
	return fallbackChannels, fallbackWeights
}

// IsResponsesCompactionNamespacePreferred reports whether a selected channel
// matches the compact routing preference for an unsupported namespace.
func IsResponsesCompactionNamespacePreferred(channel *Channel, modelName string) bool {
	if channel == nil {
		return false
	}
	_, isCompact := ratio_setting.CompactBaseModelName(strings.TrimSpace(modelName))
	return isCompact && channelPrefersResponsesCompactionWhenNamespaceUnsupported(channel)
}

func channelAllowsNativeCompactFallback(channel *Channel, candidate routeModelCandidate, settings dto.ChannelOtherSettings, baseModelName string, now time.Time) bool {
	baseModelName = strings.TrimSpace(baseModelName)
	if baseModelName == "" {
		return false
	}
	compactModelName := ratio_setting.WithCompactModelSuffix(baseModelName)
	if modelListContains(channel.GetModels(), compactModelName) {
		return true
	}

	compactSignals := compactModelSignals(settings)
	for _, mappedBaseModelName := range compactMappedBaseModelCandidatesForCandidate(channel, candidate, baseModelName, compactModelName) {
		if compactRouteTargetAllowed(channel, compactSignals, mappedBaseModelName) {
			return true
		}
	}
	// Without compactSignals, allow base-model fallback only before upstream model checks start.
	if len(compactSignals) == 0 {
		if settings.AllowAutoNativeCompactBaseFallbackAt(now) && modelListContains(channel.GetModels(), baseModelName) {
			return true
		}
		return !settings.UpstreamModelUpdateCheckEnabled || settings.UpstreamModelUpdateLastCheckTime == 0
	}
	_, ok := compactSignals[compactModelName]
	return ok
}

func channelAllowsSyntheticCompactFallback(channel *Channel, candidate routeModelCandidate, baseModelName string) bool {
	baseModelName = strings.TrimSpace(baseModelName)
	if baseModelName == "" {
		return false
	}
	compactModelName := ratio_setting.WithCompactModelSuffix(baseModelName)
	if modelListContains(channel.GetModels(), compactModelName) || modelListContains(channel.GetModels(), baseModelName) {
		return true
	}
	for _, mappedBaseModelName := range compactMappedBaseModelCandidatesForCandidate(channel, candidate, baseModelName, compactModelName) {
		if modelListContains(channel.GetModels(), mappedBaseModelName) ||
			modelListContains(channel.GetModels(), ratio_setting.WithCompactModelSuffix(mappedBaseModelName)) {
			return true
		}
	}
	return false
}

func channelAllowsSyntheticCompactBaseFallback(channel *Channel, candidate routeModelCandidate, baseModelName string) bool {
	baseModelName = strings.TrimSpace(baseModelName)
	if baseModelName == "" {
		return false
	}
	compactModelName := ratio_setting.WithCompactModelSuffix(baseModelName)
	if modelListContains(channel.GetModels(), baseModelName) {
		return true
	}
	for _, mappedBaseModelName := range compactMappedBaseModelCandidatesForCandidate(channel, candidate, baseModelName, compactModelName) {
		if modelListContains(channel.GetModels(), mappedBaseModelName) {
			return true
		}
	}
	return false
}

func channelAllowsAzureCompactRoute(channel *Channel, candidate routeModelCandidate, baseModelName string) bool {
	baseModelName = strings.TrimSpace(baseModelName)
	if baseModelName == "" {
		return false
	}
	compactModelName := ratio_setting.WithCompactModelSuffix(baseModelName)
	if modelListContains(channel.GetModels(), compactModelName) ||
		modelListContains(channel.GetModels(), baseModelName) {
		return true
	}
	for _, mappedBaseModelName := range compactMappedBaseModelCandidatesForCandidate(channel, candidate, baseModelName, compactModelName) {
		if modelListContains(channel.GetModels(), mappedBaseModelName) ||
			modelListContains(channel.GetModels(), ratio_setting.WithCompactModelSuffix(mappedBaseModelName)) {
			return true
		}
	}
	return false
}

func compactRouteTargetAllowed(channel *Channel, compactSignals map[string]struct{}, baseModelName string) bool {
	baseModelName = strings.TrimSpace(baseModelName)
	if baseModelName == "" {
		return false
	}
	compactModelName := ratio_setting.WithCompactModelSuffix(baseModelName)
	if len(compactSignals) > 0 {
		_, ok := compactSignals[compactModelName]
		return ok
	}
	return modelListContains(channel.GetModels(), compactModelName) ||
		modelListContains(channel.GetModels(), baseModelName)
}

// CompactMappedBaseModelCandidates returns mapping-derived compact targets for
// callers that already hold a validated snapshot. It is intentionally a small
// public adapter for route diagnostics and tests; normal selectors should pass
// the resolved effective model through routeModelCandidate instead.
func CompactMappedBaseModelCandidates(mapping *ChannelModelMapping, baseModelName string, compactModelName string) []string {
	if mapping == nil {
		return nil
	}
	if mappedModel, _, err := mapping.Resolve(compactModelName, true); err == nil && strings.TrimSpace(mappedModel) != "" && mappedModel != compactModelName {
		return appendCompactMappedCandidate(nil, mappedModel)
	}
	if mappedModel, _, err := mapping.Resolve(baseModelName, false); err == nil && strings.TrimSpace(mappedModel) != "" && mappedModel != baseModelName {
		return appendCompactMappedCandidate(nil, mappedModel)
	}
	return nil
}

func compactMappedBaseModelCandidatesForCandidate(channel *Channel, candidate routeModelCandidate, baseModelName string, compactModelName string) []string {
	if strings.TrimSpace(candidate.compactCapabilityModel) != "" {
		return appendCompactMappedCandidate(nil, candidate.compactCapabilityModel)
	}
	if candidate.mappingApplied && strings.TrimSpace(candidate.effectiveModel) != "" {
		return appendCompactMappedCandidate(nil, candidate.effectiveModel)
	}
	if candidate.mappingSnapshotReady {
		// The caller already resolved and validated this channel's mapping through
		// its request-scoped snapshot. An empty result means there is no mapping
		// target for this candidate; never parse channel.model_mapping again.
		return nil
	}
	// Unprepared candidates are not eligible for a compact mapping target. All
	// production selector paths call prepareResolvedRouteCandidate first, and
	// refusing to parse here prevents a hidden second mapping implementation.
	return nil
}

func appendCompactMappedCandidate(candidates []string, modelName string) []string {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return candidates
	}
	if baseModelName, isCompact := ratio_setting.CompactBaseModelName(modelName); isCompact {
		modelName = baseModelName
	}
	for _, candidate := range candidates {
		if candidate == modelName {
			return candidates
		}
	}
	return append(candidates, modelName)
}

func compactModelSignals(settings dto.ChannelOtherSettings) map[string]struct{} {
	signals := make(map[string]struct{})
	for _, modelName := range settings.UpstreamModelUpdateLastDetectedModels {
		modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
		if _, isCompact := ratio_setting.CompactBaseModelName(modelName); isCompact {
			signals[modelName] = struct{}{}
		}
	}
	return signals
}

func modelListContains(models []string, target string) bool {
	for _, modelName := range models {
		if strings.TrimSpace(modelName) == target {
			return true
		}
	}
	return false
}
