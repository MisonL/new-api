package ratio_setting

import "strings"

const CompactModelSuffix = "-openai-compact"
const CompactWildcardModelKey = "*" + CompactModelSuffix

func WithCompactModelSuffix(modelName string) string {
	if strings.HasSuffix(modelName, CompactModelSuffix) {
		return modelName
	}
	return modelName + CompactModelSuffix
}

func CompactBaseModelName(modelName string) (string, bool) {
	if !strings.HasSuffix(modelName, CompactModelSuffix) {
		return modelName, false
	}
	return strings.TrimSuffix(modelName, CompactModelSuffix), true
}
