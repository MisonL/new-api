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
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if normalized != "" && normalized != modelName {
		candidates = append(candidates, normalized)
	}
	return candidates
}
