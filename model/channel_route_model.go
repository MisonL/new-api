package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type routeModelCandidate struct {
	model           string
	compactFallback bool
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
	if modelName == "" {
		return nil
	}
	candidates := []routeModelCandidate{{model: modelName}}
	if !common.GroupModelRouteHelperEnabled {
		return candidates
	}
	if baseModelName, isCompact := ratio_setting.CompactBaseModelName(modelName); isCompact {
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
		model:           modelName,
		compactFallback: compactFallback,
	})
	return candidates
}

func channelSupportsCompactRouteCandidate(channel *Channel, candidate routeModelCandidate) bool {
	if channel == nil || !candidate.compactFallback {
		return true
	}
	switch channel.Type {
	case constant.ChannelTypeOpenAI:
		settings, ok := channelNativeResponsesCompactSettings(channel)
		return ok && channelAllowsNativeCompactFallback(channel, settings, candidate.model)
	case constant.ChannelTypeCodex:
		return true
	default:
		return false
	}
}

func channelNativeResponsesCompactSettings(channel *Channel) (dto.ChannelOtherSettings, bool) {
	settings := dto.ChannelOtherSettings{}
	if channel == nil || channel.OtherSettings == "" {
		return settings, false
	}
	if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
		return settings, false
	}
	return settings, settings.HasNativeResponsesCompact()
}

func channelAllowsNativeCompactFallback(channel *Channel, settings dto.ChannelOtherSettings, baseModelName string) bool {
	baseModelName = strings.TrimSpace(baseModelName)
	if baseModelName == "" {
		return false
	}
	compactModelName := ratio_setting.WithCompactModelSuffix(baseModelName)
	if modelListContains(channel.GetModels(), compactModelName) {
		return true
	}

	compactSignals := compactModelSignals(settings)
	// Without compactSignals, allow base-model fallback only before upstream model checks start.
	if len(compactSignals) == 0 {
		return !settings.UpstreamModelUpdateCheckEnabled || settings.UpstreamModelUpdateLastCheckTime == 0
	}
	_, ok := compactSignals[compactModelName]
	return ok
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
