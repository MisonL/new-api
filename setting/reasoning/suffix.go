package reasoning

import (
	"strings"

	"github.com/samber/lo"
)

var EffortSuffixes = []string{"-max", "-xhigh", "-high", "-medium", "-low", "-minimal"}

var OpenAIEffortSuffixes = []string{"-high", "-minimal", "-low", "-medium", "-none", "-xhigh"}

// Keep the richer public suffix vocabulary used by the gateway, and normalize
// it to the effort values accepted by the DeepSeek API. DeepSeek maps
// minimal/low to low, medium/high/xhigh to high, and max/ultra to max.
var DeepSeekV4EffortSuffixes = []string{"-minimal", "-ultra", "-xhigh", "-medium", "-none", "-high", "-low", "-max"}

// TrimEffortSuffix -> modelName level(low) exists
func TrimEffortSuffix(modelName string) (string, string, bool) {
	return TrimEffortSuffixWithSuffixes(modelName, EffortSuffixes)
}

func TrimEffortSuffixWithSuffixes(modelName string, suffixes []string) (string, string, bool) {
	suffix, found := lo.Find(suffixes, func(s string) bool {
		return strings.HasSuffix(modelName, s)
	})
	if !found {
		return modelName, "", false
	}
	return strings.TrimSuffix(modelName, suffix), strings.TrimPrefix(suffix, "-"), true
}

func ParseOpenAIReasoningEffortFromModelSuffix(modelName string) (string, string) {
	baseModel, effort, ok := TrimEffortSuffixWithSuffixes(modelName, OpenAIEffortSuffixes)
	if !ok {
		return "", modelName
	}
	return effort, baseModel
}

func ParseDeepSeekV4ThinkingSuffix(modelName string) (baseModel string, thinkingType string, effort string, ok bool) {
	baseModel, suffix, ok := TrimEffortSuffixWithSuffixes(modelName, DeepSeekV4EffortSuffixes)
	if !ok {
		return modelName, "", "", false
	}
	normalizedBaseModel := strings.ToLower(baseModel)
	normalizedModel := strings.TrimPrefix(normalizedBaseModel, "deepseek-ai/")
	if !strings.HasPrefix(normalizedModel, "deepseek-v4-") && normalizedModel != "deepseek-flash" {
		return modelName, "", "", false
	}
	switch suffix {
	case "none":
		return baseModel, "disabled", "", true
	case "minimal", "low":
		return baseModel, "enabled", "low", true
	case "medium", "high", "xhigh":
		return baseModel, "enabled", "high", true
	case "max", "ultra":
		return baseModel, "enabled", "max", true
	default:
		return modelName, "", "", false
	}
}
