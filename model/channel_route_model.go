package model

import (
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
		return channelHasNativeResponsesCompact(channel)
	case constant.ChannelTypeCodex:
		return true
	default:
		return false
	}
}

func channelHasNativeResponsesCompact(channel *Channel) bool {
	if channel == nil || channel.OtherSettings == "" {
		return false
	}
	settings := dto.ChannelOtherSettings{}
	if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
		return false
	}
	return settings.HasNativeResponsesCompact()
}
