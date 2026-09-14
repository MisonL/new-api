package ratio_setting

import (
	"strings"
	"time"
)

// FormatMatchingModelName removes routing labels and normalizes known provider
// aliases to the closest configured model. Compact models keep their suffix so
// the existing compact-specific lookup remains effective.
func FormatMatchingModelName(name string) string {
	original := stripModelRouteLabels(strings.TrimSpace(name))
	baseName, compactSuffix := prepareModelName(original)
	if baseName == "" {
		return original
	}

	baseName, known := normalizeKnownModelFamily(baseName)
	if !known {
		return original
	}
	return baseName + compactSuffix
}

// ModelNameMatchesLimit reports whether a request model matches any explicitly
// allowed model-limit key, including provider, date, control and compact aliases.
func ModelNameMatchesLimit(name string, limits map[string]bool) bool {
	if len(limits) == 0 {
		return false
	}
	for _, candidate := range modelNameCandidates(name) {
		candidates := []string{candidate}
		if baseName, compact := CompactBaseModelName(strings.ToLower(candidate)); compact {
			candidates = append(candidates, baseName)
		}
		for _, candidateToCheck := range candidates {
			if limits[candidateToCheck] {
				return true
			}
			for configured, allowed := range limits {
				if allowed && strings.EqualFold(strings.TrimSpace(configured), candidateToCheck) {
					return true
				}
			}
		}
	}
	return false
}

// modelNameCandidates keeps an explicit configured alias ahead of its
// canonical base model. The middle candidate only normalizes spelling and
// casing, so a configured thinking or dated variant can still be selected
// before the base-model fallback.
func modelNameCandidates(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return []string{""}
	}
	candidates := make([]string, 0, 12)
	appendCandidate := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		for _, existing := range candidates {
			if existing == candidate {
				return
			}
		}
		candidates = append(candidates, candidate)
	}

	appendCandidate(name)
	appendCandidate(stripModelRouteLabels(name))
	appendCandidate(normalizeQualifiedModelName(name, false, false))
	appendCandidate(normalizeVariantModelName(name))
	if !isMiniMaxHighspeedVariant(name) && !isKimiCodeHighspeedVariant(name) {
		appendCandidate(normalizeQualifiedModelName(name, true, false))
		appendCandidate(normalizeQualifiedModelName(name, true, true))
		appendCandidate(normalizeControlVariantModelName(name))
	}
	appendCandidate(FormatMatchingModelName(name))
	return candidates
}

func normalizeQualifiedModelName(name string, stripControls, stripDate bool) string {
	name = strings.ToLower(strings.TrimSpace(stripModelRouteLabels(name)))
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, "_", "-")
	if name == "" {
		return name
	}

	prefix, modelPart := splitModelPath(name)
	modelPart, _ = normalizeKnownModelFamilyVariant(modelPart)
	if stripControls {
		modelPart = stripModelControlSuffixes(modelPart)
	}
	if stripDate {
		modelPart = stripTrailingDate(modelPart)
	}
	if stripControls && stripDate {
		if canonical, known := normalizeKnownModelFamily(modelPart); known {
			modelPart = canonical
		}
	}
	return prefix + modelPart
}

func splitModelPath(name string) (string, string) {
	index := strings.LastIndexByte(name, '/')
	if index < 0 {
		return "", name
	}
	return name[:index+1], name[index+1:]
}

func prepareModelName(name string) (string, string) {
	name = strings.ToLower(strings.TrimSpace(name))
	compactSuffix := ""
	if strings.HasSuffix(name, CompactModelSuffix) {
		compactSuffix = CompactModelSuffix
		name = strings.TrimSuffix(name, CompactModelSuffix)
	}
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, "_", "-")
	name = stripKnownProviderPrefix(name)
	return strings.TrimSpace(name), compactSuffix
}

func normalizeVariantModelName(name string) string {
	original := stripModelRouteLabels(strings.TrimSpace(name))
	baseName, compactSuffix := prepareModelName(original)
	if baseName == "" {
		return original
	}
	baseName, known := normalizeKnownModelFamilyVariant(baseName)
	if !known {
		return original
	}
	return baseName + compactSuffix
}

// normalizeControlVariantModelName removes routing and execution-mode labels
// while retaining the model release/date portion. This lets dated models such
// as claude-opus-4-1-20250805-thinking inherit the configured dated price
// before the broader family fallback is attempted.
func normalizeControlVariantModelName(name string) string {
	original := stripModelRouteLabels(strings.TrimSpace(name))
	baseName, compactSuffix := prepareModelName(original)
	if baseName == "" {
		return original
	}
	baseName, known := normalizeKnownModelFamilyVariant(baseName)
	if !known {
		return original
	}
	baseName = stripModelControlSuffixes(baseName)
	return baseName + compactSuffix
}

func stripModelRouteLabels(name string) string {
	for strings.HasPrefix(name, "[") {
		end := strings.IndexByte(name, ']')
		if end < 0 {
			break
		}
		name = strings.TrimSpace(name[end+1:])
	}
	return name
}

func stripKnownProviderPrefix(name string) string {
	for {
		stripped := name
		for _, prefix := range []string{
			"@cf/",
			"@hf/",
			"pro/",
			"google/",
			"meta/",
			"meta-llama/",
			"openai/",
			"openrouter/",
			"deepseek-ai/",
			"deepseek/",
			"moonshotai/",
			"minimaxai/",
			"minimax/",
			"z-ai/",
			"zai-org/",
			"stepfun-ai/",
			"fireworks-ai/",
			"fireworks/",
			"qwen/",
			"bytedance-seed/",
			"inclusionai/",
			"tencent/",
			"baidu/",
			"siliconflow/",
			"dashscope/",
			"sambanova/",
			"together/",
			"groq/",
			"x-ai/",
			"xai/",
			"grok/",
			"free/",
		} {
			if strings.HasPrefix(stripped, prefix) {
				stripped = strings.TrimPrefix(stripped, prefix)
				break
			}
		}
		if stripped == name {
			return name
		}
		name = stripped
	}
}

func normalizeKnownModelFamilyVariant(name string) (string, bool) {
	switch {
	case strings.HasPrefix(name, "gpt-") || strings.HasPrefix(name, "o1") || strings.HasPrefix(name, "o3") || strings.HasPrefix(name, "o4"):
		return normalizeGPTModelVariant(name), true
	case strings.HasPrefix(name, "claude-"):
		return normalizeClaudeModelVariant(name), true
	case strings.HasPrefix(name, "gemini-"):
		return normalizeGeminiModelVariant(name), true
	case strings.HasPrefix(name, "deepseek-"):
		return normalizeDeepSeekModelVariant(name), true
	case strings.HasPrefix(name, "glm"):
		return normalizeGLMModelVariant(name), true
	case strings.HasPrefix(name, "grok-"):
		return normalizeGrokModelVariant(name), true
	case strings.HasPrefix(name, "kimi-"):
		return normalizeKimiModelVariant(name), true
	case strings.HasPrefix(name, "minimax-"):
		return normalizeMiniMaxModelVariant(name), true
	case strings.HasPrefix(name, "qwen"):
		return normalizeQwenModelVariant(name), true
	case strings.HasPrefix(name, "llama"):
		return normalizeLlamaModelVariant(name), true
	case strings.HasPrefix(name, "mistral-") || strings.HasPrefix(name, "ministral-"):
		return normalizeMistralModelVariant(name), true
	case strings.HasPrefix(name, "command"):
		return normalizeCommandModelVariant(name), true
	case strings.HasPrefix(name, "ernie"):
		return normalizeErnieModelVariant(name), true
	case strings.HasPrefix(name, "gpt-oss-"):
		return normalizeOpenModelVariant(name), true
	case strings.HasPrefix(name, "gemma-") || strings.HasPrefix(name, "medgemma-"):
		return normalizeOpenModelVariant(name), true
	case strings.HasPrefix(name, "agnes-") || strings.HasPrefix(name, "codex-auto-review"):
		return normalizeOpenModelVariant(name), true
	case strings.HasPrefix(name, "step-") || strings.HasPrefix(name, "stepfun-"):
		return normalizeStepModelVariant(name), true
	case strings.HasPrefix(name, "doubao-") || strings.HasPrefix(name, "seed-") || strings.HasPrefix(name, "seedance-") || strings.HasPrefix(name, "seedream-"):
		return normalizeDoubaoModelVariant(name), true
	case strings.HasPrefix(name, "hunyuan"):
		return normalizeOpenModelVariant(name), true
	case strings.HasPrefix(name, "yi-") || strings.HasPrefix(name, "sparkdesk") || strings.HasPrefix(name, "chatglm") || strings.HasPrefix(name, "360gpt"):
		return normalizeOpenModelVariant(name), true
	case strings.HasPrefix(name, "mimo-") || strings.HasPrefix(name, "moonshot-"):
		return normalizeOpenModelVariant(name), true
	default:
		return name, false
	}
}

func normalizeKnownModelFamily(name string) (string, bool) {
	switch {
	case strings.HasPrefix(name, "gpt-") || strings.HasPrefix(name, "o1") || strings.HasPrefix(name, "o3") || strings.HasPrefix(name, "o4"):
		return normalizeGPTModel(name), true
	case strings.HasPrefix(name, "claude-"):
		return normalizeClaudeModel(name), true
	case strings.HasPrefix(name, "gemini-"):
		return normalizeGeminiModel(name), true
	case strings.HasPrefix(name, "deepseek-"):
		return normalizeDeepSeekModel(name), true
	case strings.HasPrefix(name, "glm"):
		return normalizeGLMModel(name), true
	case strings.HasPrefix(name, "grok-"):
		return normalizeGrokModel(name), true
	case strings.HasPrefix(name, "kimi-"):
		return normalizeKimiModel(name), true
	case strings.HasPrefix(name, "minimax-"):
		return normalizeMiniMaxModel(name), true
	case strings.HasPrefix(name, "qwen"):
		return normalizeQwenModel(name), true
	case strings.HasPrefix(name, "llama"):
		return normalizeGenericModel(normalizeLlamaModelVariant(name)), true
	case strings.HasPrefix(name, "gpt-oss-"):
		return stripRoutingVariantSuffix(name), true
	case strings.HasPrefix(name, "gemma-") || strings.HasPrefix(name, "medgemma-") || strings.HasPrefix(name, "hunyuan"):
		return normalizeGenericModel(name), true
	case strings.HasPrefix(name, "agnes-") || strings.HasPrefix(name, "codex-auto-review"):
		return stripRoutingVariantSuffix(name), true
	case strings.HasPrefix(name, "mistral-") || strings.HasPrefix(name, "ministral-"):
		return normalizeGenericModel(normalizeMistralModelVariant(name)), true
	case strings.HasPrefix(name, "step-"):
		return normalizeGenericModel(normalizeStepModelVariant(name)), true
	case strings.HasPrefix(name, "doubao-") || strings.HasPrefix(name, "seed-") || strings.HasPrefix(name, "seedance-") || strings.HasPrefix(name, "seedream-"):
		return normalizeGenericModel(normalizeDoubaoModelVariant(name)), true
	case strings.HasPrefix(name, "command") || strings.HasPrefix(name, "ernie") || strings.HasPrefix(name, "yi-") || strings.HasPrefix(name, "sparkdesk") || strings.HasPrefix(name, "chatglm") || strings.HasPrefix(name, "360gpt") || strings.HasPrefix(name, "mimo-") || strings.HasPrefix(name, "moonshot-"):
		return normalizeGenericModel(name), true
	default:
		return name, false
	}
}

func normalizeGPTModelVariant(name string) string {
	name = replaceModelPrefixToken(name, "gpt-5-5", "gpt-5.5")
	name = replaceModelPrefixToken(name, "gpt-5-6", "gpt-5.6")
	name = replaceModelPrefixToken(name, "gpt-4-5", "gpt-4.5")
	name = replaceModelPrefixToken(name, "gpt-4-1", "gpt-4.1")
	return name
}

func normalizeClaudeModelVariant(name string) string {
	for _, version := range []string{"3.5", "3.7", "4.1", "4.5", "4.6", "4.7", "4.8"} {
		name = strings.ReplaceAll(name, "-"+version, "-"+strings.ReplaceAll(version, ".", "-"))
	}
	return name
}

func normalizeGeminiModelVariant(name string) string {
	for _, version := range []string{"2.5", "3.1", "3.5", "3.6", "3.7"} {
		name = strings.Replace(name, "gemini-"+strings.ReplaceAll(version, ".", "-")+"-", "gemini-"+version+"-", 1)
	}
	return name
}

func normalizeDeepSeekModelVariant(name string) string {
	for _, version := range []string{"v3.1", "v3.2", "v4.0"} {
		name = strings.Replace(name, "deepseek-"+strings.ReplaceAll(version, ".", "-")+"-", "deepseek-"+version+"-", 1)
	}
	switch name {
	case "deepseek-v4.1-flash", "deepseek-v4.1-flash-0910":
		// DeepSeek's 2026-09-10 release uses deepseek-flash as the
		// official API name. Keep dated/channel aliases on the same
		// pricing and billing path as the current V4.1 Flash model.
		return "deepseek-flash"
	case "deepseek-v4-flash-0731":
		return "deepseek-v4-flash"
	case "deepseek-v4-pro-0813":
		return "deepseek-v4-pro"
	default:
		return name
	}
}

func normalizeGLMModelVariant(name string) string {
	if name == "glm5" {
		return "glm-5"
	}
	if strings.HasPrefix(name, "glm5.") {
		return "glm-" + strings.TrimPrefix(name, "glm")
	}
	if strings.HasPrefix(name, "glm4.") {
		return "glm-" + strings.TrimPrefix(name, "glm")
	}
	if name == "glm4p7" {
		return "glm-4p7"
	}
	name = strings.Replace(name, "glm-5-2", "glm-5.2", 1)
	name = strings.Replace(name, "glm-5-1", "glm-5.1", 1)
	name = strings.Replace(name, "glm-4-5", "glm-4.5", 1)
	name = strings.Replace(name, "glm-4-6", "glm-4.6", 1)
	name = strings.Replace(name, "glm-4-7", "glm-4.7", 1)
	return name
}

func normalizeGrokModelVariant(name string) string {
	name = replaceModelPrefixToken(name, "grok-4-5", "grok-4.5")
	name = replaceModelPrefixToken(name, "grok-4-1", "grok-4.1")
	name = replaceModelPrefixToken(name, "grok-4-3", "grok-4.3")
	name = replaceModelPrefixToken(name, "grok-4-20", "grok-4.20")
	return replaceModelPrefixToken(name, "grok-420", "grok-4.20")
}

func normalizeKimiModelVariant(name string) string {
	name = replaceModelPrefixToken(name, "kimi-k2-5", "kimi-k2.5")
	name = replaceModelPrefixToken(name, "kimi-k2-6", "kimi-k2.6")
	return replaceModelPrefixToken(name, "kimi-k2-7", "kimi-k2.7")
}

func normalizeMiniMaxModelVariant(name string) string {
	name = replaceModelPrefixToken(name, "minimax-m2.5", "minimax-m2p5")
	name = replaceModelPrefixToken(name, "minimax-m2-5", "minimax-m2p5")
	return replaceModelPrefixToken(name, "minimax-m2-1", "minimax-m2.1")
}

func normalizeQwenModelVariant(name string) string {
	name = replaceModelPrefixToken(name, "qwen3-5", "qwen3.5")
	name = replaceModelPrefixToken(name, "qwen3-6", "qwen3.6")
	return replaceModelPrefixToken(name, "qwen3-7", "qwen3.7")
}

func normalizeLlamaModelVariant(name string) string {
	return replaceModelPrefixToken(name, "llama-3", "llama3")
}

func normalizeMistralModelVariant(name string) string {
	return replaceModelPrefixToken(name, "mistral-3-", "mistral-3.")
}

func normalizeCommandModelVariant(name string) string {
	return name
}

func normalizeErnieModelVariant(name string) string {
	return name
}

func normalizeOpenModelVariant(name string) string {
	return name
}

func normalizeStepModelVariant(name string) string {
	name = replaceModelPrefixToken(name, "step-3-5", "step-3.5")
	return replaceModelPrefixToken(name, "step-3-7", "step-3.7")
}

func normalizeDoubaoModelVariant(name string) string {
	name = replaceModelPrefixToken(name, "doubao-seed-1-6", "doubao-seed-1.6")
	name = replaceModelPrefixToken(name, "seed-1-6", "seed-1.6")
	return replaceModelPrefixToken(name, "seedance-1-0", "seedance-1.0")
}

// replaceModelPrefixToken changes a model token only when it is the complete
// model prefix. This prevents gpt-4-1 from corrupting gpt-4-1106-preview.
func replaceModelPrefixToken(name, from, to string) string {
	if name == from {
		return to
	}
	if strings.HasSuffix(from, "-") && strings.HasPrefix(name, from) {
		return to + strings.TrimPrefix(name, from)
	}
	if strings.HasPrefix(name, from+"-") {
		return to + strings.TrimPrefix(name, from)
	}
	return name
}

func normalizeGPTModel(name string) string {
	if strings.HasPrefix(name, "gpt-4-gizmo") {
		return "gpt-4-gizmo-*"
	}
	if strings.HasPrefix(name, "gpt-4o-gizmo") {
		return "gpt-4o-gizmo-*"
	}
	name = normalizeGPTModelVariant(name)
	name = stripSuffixes(name, "-cpa", "-lokiwang", "-chat-latest", "-xhigh", "-high", "-low", "-medium", "(xhigh)", "(high)", "(medium)", "(low)")
	name = stripTrailingDate(name)

	if strings.HasPrefix(name, "gpt-5.6-luna") || strings.HasPrefix(name, "gpt-5.6-sol") || strings.HasPrefix(name, "gpt-5.6-terra") {
		return stripSuffixes(name, "-pro", "-thinking", "-wm")
	}
	if strings.HasPrefix(name, "gpt-image-2-") {
		variant := strings.TrimPrefix(name, "gpt-image-2-")
		switch variant {
		case "2k", "4k", "pro", "vip", "all":
			return "gpt-image-2"
		}
	}
	if strings.HasPrefix(name, "gpt-image-2.5-") {
		variant := strings.TrimPrefix(name, "gpt-image-2.5-")
		switch variant {
		case "sunburst", "flare":
			// OpenAI's current image-2.5 variants use the same token prices
			// and image-input ratio as gpt-image-2.
			return "gpt-image-2"
		}
	}
	return name
}

func normalizeClaudeModel(name string) string {
	name = normalizeClaudeModelVariant(name)
	name = stripSuffixes(name, "-thinking-medium", "-thinking", "-xhigh", "-high", "-medium", "-low", "-max")
	name = stripTrailingDate(name)
	if strings.HasPrefix(name, "claude-sonnet-4-8") {
		return strings.Replace(name, "claude-sonnet-4-8", "claude-sonnet-4-6", 1)
	}
	return name
}

func normalizeGeminiModel(name string) string {
	name = normalizeGeminiModelVariant(name)
	// Google exposes preview/latest aliases for several priced endpoints.
	switch name {
	case "gemini-3.7-flash-preview", "gemini-3.7-flash-latest":
		return "gemini-3.7-flash"
	case "gemini-3.6-flash-preview", "gemini-3.6-flash-latest":
		return "gemini-3.6-flash"
	case "gemini-3.5-flash-preview", "gemini-3.5-flash-latest":
		return "gemini-3.5-flash"
	case "gemini-3.5-flash-lite-preview", "gemini-3.5-flash-lite-latest":
		return "gemini-3.5-flash-lite"
	case "gemini-3.1-pro-latest":
		return "gemini-3.1-pro-preview"
	case "gemini-3.1-flash-live", "gemini-3.1-flash-live-latest":
		return "gemini-3.1-flash-live-preview"
	case "gemini-3.1-flash-tts", "gemini-3.1-flash-tts-latest":
		return "gemini-3.1-flash-tts-preview"
	case "gemini-3.5-live-translate", "gemini-3.5-live-translate-latest":
		return "gemini-3.5-live-translate-preview"
	case "gemini-robotics-er-1.6":
		return "gemini-robotics-er-1.6-preview"
	case "gemini-robotics-er-2":
		return "gemini-robotics-er-2-preview"
	case "gemini-robotics-er-2-streaming":
		return "gemini-robotics-er-2-streaming-preview"
	}
	if name == "gemini-embedding-2-preview" {
		return "gemini-embedding-2"
	}
	if name == "gemini-3.1-pro" {
		return "gemini-3.1-pro-preview"
	}
	if name == "gemini-omni-flash" {
		return "gemini-omni-flash-preview"
	}
	if name == "gemini-3-pro-image" {
		return "gemini-3-pro-image-preview"
	}
	if name == "gemini-3.1-flash-image" {
		return "gemini-3.1-flash-image-preview"
	}
	if strings.Contains(name, "-image") || strings.Contains(name, "-audio") || strings.Contains(name, "-tts") || strings.Contains(name, "embedding") || strings.Contains(name, "computer-use") || strings.Contains(name, "robotics") {
		return name
	}
	// These two active aliases are routing controls for the paid Gemini 2.5
	// Flash endpoint, not separate price tiers. Keep them on the published base
	// price instead of the legacy thinking-budget wildcard.
	if name == "gemini-2.5-flash-thinking" || name == "gemini-2.5-flash-nothinking" {
		return "gemini-2.5-flash"
	}
	// Third-party channels expose Gemini reasoning/quality controls as routing
	// suffixes. Google publishes one token price for the underlying endpoint,
	// so these controls must reuse the same pricing key.
	if hasModelTokenPrefix(name, "gemini-3.7-flash") || hasModelTokenPrefix(name, "gemini-3.6-flash") || hasModelTokenPrefix(name, "gemini-3.5-flash") {
		name = stripSuffixes(name, "-extra-low", "-tiered", "-high", "-medium", "-low")
	}
	if hasModelTokenPrefix(name, "gemini-3.1-pro") || hasModelTokenPrefix(name, "gemini-3-pro") {
		name = stripSuffixes(name, "-high", "-medium", "-low")
	}
	if strings.HasPrefix(name, "gemini-2.5-flash-lite") && strings.Contains(name, "-thinking") {
		return "gemini-2.5-flash-lite-preview-thinking-*"
	}
	if strings.HasPrefix(name, "gemini-2.5-flash") && strings.Contains(name, "-thinking") {
		return "gemini-2.5-flash-thinking-*"
	}
	if strings.HasPrefix(name, "gemini-2.5-pro") && strings.Contains(name, "-thinking") {
		return "gemini-2.5-pro-thinking-*"
	}
	name = stripSuffixes(name, "-customtools", "-search-maxthinking", "-searching", "-maxthinking", "-thinking-512", "-thinking-128", "-thinking", "-search", "-1m")
	if name == "gemini-3-pro" {
		return "gemini-3-pro-preview"
	}
	if name == "gemini-3.1-flash-lite" {
		return "gemini-3.1-flash-lite-preview"
	}
	return name
}

func normalizeDeepSeekModel(name string) string {
	name = normalizeDeepSeekModelVariant(name)
	if isDeepSeekV4ProSearchVariant(name) {
		return name
	}
	name = stripSuffixes(name, "-search-nothinking", "-search", "-nothinking", "-minimal", "-low", "-medium", "-high", "-xhigh", "-max", "-ultra", "-none", "-free", "-thinking", "-cpa")
	name = normalizeDeepSeekModelVariant(name)
	name = stripTrailingDate(name)
	return name
}

func normalizeGLMModel(name string) string {
	name = normalizeGLMModelVariant(name)
	name = stripSuffixes(name, "-chat", "-thinking", "-low", "-high", "-medium", "-lokiwang", "-cpa")
	name = stripTrailingDate(name)
	return name
}

func normalizeGrokModel(name string) string {
	name = normalizeGrokModelVariant(name)
	if name == "grok-build-latest" {
		return "grok-4.5"
	}
	if name == "grok-latest" {
		return "grok-4.3"
	}
	if isGrokIsolatedVariant(name) {
		return name
	}
	// Only the documented slug and its numbered alias are equivalent. Do not
	// collapse arbitrary suffixes (for example "grok-code-fast-experimental")
	// into the priced model, otherwise model-limit checks become a family
	// allow-list instead of an explicit model allow-list.
	if name == "grok-code-fast" || name == "grok-code-fast-1" {
		return "grok-code-fast-1"
	}
	name = stripSuffixes(name, "-agents", "-reasoning", "-non-reasoning", "-fast", "-latest")
	name = stripTrailingDate(name)
	if name == "grok-4.20" {
		return "grok-4.20"
	}
	if name == "grok-4.20-multi-agent" {
		return "grok-4.20"
	}
	if name == "grok-4.6" {
		return "grok-4.6"
	}
	if name == "grok-build-0.1" {
		return "grok-build-0.1"
	}
	if name == "grok-4.3" {
		return "grok-4.3"
	}
	return name
}

func normalizeKimiModel(name string) string {
	name = normalizeKimiModelVariant(name)
	name = stripSuffixes(name, "-thinking", "-reasoning", "-high", "-medium", "-low", "-max", "-free", "-cpa", "-lokiwang", "-turbo", "-preview")
	name = stripTrailingDate(name)
	// The suffixes above are the only supported Kimi controls. Keep unknown
	// variants distinct so token model limits cannot be bypassed by inventing a
	// new model suffix that happens to share a family prefix.
	if name == "kimi-k3" {
		return "kimi-k3"
	}
	if name == "kimi-k2.6" {
		return "kimi-k2.6"
	}
	if name == "kimi-k2.7-code-highspeed" {
		return "kimi-k2.7-code-highspeed"
	}
	if name == "kimi-k2.7-code" {
		return "kimi-k2.7-code"
	}
	if name == "kimi-k2.5" {
		return "kimi-k2p5"
	}
	if name == "kimi-k2p5" {
		return "kimi-k2p5"
	}
	return name
}

func normalizeMiniMaxModel(name string) string {
	name = normalizeMiniMaxModelVariant(name)
	name = stripSuffixes(name, "-thinking", "-lokiwang", "-cpa", "-free")
	name = stripTrailingDate(name)
	return name
}

func isMiniMaxHighspeedVariant(name string) bool {
	name = strings.ToLower(strings.TrimSpace(stripModelRouteLabels(name)))
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, ":", "-")
	_, modelPart := splitModelPath(name)
	return strings.HasPrefix(modelPart, "minimax-m2") && strings.HasSuffix(modelPart, "-highspeed")
}

func isKimiCodeHighspeedVariant(name string) bool {
	name = strings.ToLower(strings.TrimSpace(stripModelRouteLabels(name)))
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, ":", "-")
	_, modelPart := splitModelPath(name)
	modelPart = normalizeKimiModelVariant(modelPart)
	return strings.HasPrefix(modelPart, "kimi-k2.7-code") && strings.Contains(modelPart, "-highspeed")
}

func normalizeQwenModel(name string) string {
	name = normalizeQwenModelVariant(name)
	name = stripRoutingVariantSuffix(name)
	name = stripTrailingDate(name)
	return name
}

func stripRoutingVariantSuffix(name string) string {
	return stripSuffixes(name, "-free", "-cpa", "-lokiwang", "-nothinking")
}

func stripModelControlSuffixes(name string) string {
	for {
		previous := name
		name = stripSuffixes(name,
			"-search-maxthinking",
			"-thinking-512",
			"-thinking-128",
			"-thinking-medium",
			"-maxthinking",
			"-non-reasoning",
			"-reasoning",
			"-agents",
			"-thinking",
			"-nothinking",
			"-highspeed",
			"-xhigh",
			"-high",
			"-medium",
			"-low",
			"-max",
			"-turbo",
			"-free",
			"-cpa",
			"-lokiwang",
			"(xhigh)",
			"(high)",
			"(medium)",
			"(low)",
		)
		if name == previous {
			return name
		}
	}
}

func normalizeGenericModel(name string) string {
	name = stripRoutingVariantSuffix(name)
	return stripTrailingDate(name)
}

func stripSuffixes(name string, suffixes ...string) string {
	for {
		longest := ""
		for _, suffix := range suffixes {
			if len(suffix) > len(longest) && strings.HasSuffix(name, suffix) {
				longest = suffix
			}
		}
		if longest == "" {
			return name
		}
		name = strings.TrimSuffix(name, longest)
	}
}

func hasModelTokenPrefix(name, prefix string) bool {
	return name == prefix || strings.HasPrefix(name, prefix+"-")
}

func isDeepSeekV4ProSearchVariant(name string) bool {
	return hasModelTokenPrefix(name, "deepseek-v4-pro") && strings.Contains(name, "-search")
}

func isGrokIsolatedVariant(name string) bool {
	for _, marker := range []string{"-search", "-deepsearch", "-imagine", "-image", "-video"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func stripTrailingDate(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) == 0 {
		return name
	}
	last := parts[len(parts)-1]
	if isValidCompactDate(last) {
		return strings.Join(parts[:len(parts)-1], "-")
	}
	if len(parts) >= 4 {
		year, month, day := parts[len(parts)-3], parts[len(parts)-2], parts[len(parts)-1]
		if isValidSeparatedDate(year, month, day) {
			return strings.Join(parts[:len(parts)-3], "-")
		}
	}
	return name
}

// isValidCompactDate limits YYYYMMDD normalization to real calendar dates.
// Treating every eight-digit suffix as a date would let arbitrary model names
// inherit a base model's pricing and token-limit authorization.
func isValidCompactDate(value string) bool {
	if len(value) != 8 || !allDigits(value) {
		return false
	}
	_, err := time.Parse("20060102", value)
	return err == nil
}

func isValidSeparatedDate(year, month, day string) bool {
	if len(year) != 4 || len(month) != 2 || len(day) != 2 ||
		!allDigits(year) || !allDigits(month) || !allDigits(day) {
		return false
	}
	_, err := time.Parse("2006-01-02", year+"-"+month+"-"+day)
	return err == nil
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
