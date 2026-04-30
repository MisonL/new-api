package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func getGroupModelRouteCandidates(modelName string) []string {
	if modelName == "" {
		return nil
	}
	candidates := []string{modelName}
	if !common.GroupModelRouteHelperEnabled {
		return candidates
	}
	if baseName, isCompact := ratio_setting.CompactBaseModelName(modelName); isCompact {
		candidates = appendRouteCandidate(candidates, baseName)
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	candidates = appendRouteCandidate(candidates, normalized)
	return candidates
}

func appendRouteCandidate(candidates []string, modelName string) []string {
	if modelName == "" {
		return candidates
	}
	for _, candidate := range candidates {
		if candidate == modelName {
			return candidates
		}
	}
	candidates = append(candidates, modelName)
	return candidates
}
