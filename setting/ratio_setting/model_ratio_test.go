package ratio_setting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetConfiguredModelRatioFireworksModels(t *testing.T) {
	InitRatioSettings()

	tests := []struct {
		name      string
		wantRatio float64
		wantComp  float64
		wantCache *float64
	}{
		{name: "qwen3-vl-30b-a3b-thinking", wantRatio: 0.075, wantComp: 4, wantCache: floatPtr(0.07 / 0.15)},
		{name: "qwen3-vl-30b-a3b-instruct", wantRatio: 0.075, wantComp: 4, wantCache: nil},
		{name: "qwen3-8b", wantRatio: 0.1, wantComp: 1, wantCache: floatPtr(0.5)},
		{name: "minimax-m2p5", wantRatio: 0.15, wantComp: 4, wantCache: floatPtr(0.1)},
		{name: "llama-v3p3-70b-instruct", wantRatio: 0.45, wantComp: 1, wantCache: floatPtr(0.5)},
		{name: "kimi-k2p5", wantRatio: 0.3, wantComp: 5, wantCache: floatPtr(1.0 / 6.0)},
		{name: "gpt-oss-20b", wantRatio: 0.035, wantComp: 30.0 / 7.0, wantCache: floatPtr(0.5)},
		{name: "gpt-oss-120b", wantRatio: 0.075, wantComp: 4, wantCache: floatPtr(1.0 / 15.0)},
		{name: "glm-5", wantRatio: 0.5, wantComp: 3.2, wantCache: floatPtr(0.2)},
		{name: "glm-4p7", wantRatio: 0.3, wantComp: 11.0 / 3.0, wantCache: floatPtr(0.5)},
		{name: "deepseek-v3p2", wantRatio: 0.28, wantComp: 3, wantCache: floatPtr(0.5)},
		{name: "deepseek-v3p1", wantRatio: 0.28, wantComp: 3, wantCache: floatPtr(0.5)},
		{name: "deepseek-flash", wantRatio: 0.15 / 2, wantComp: 4, wantCache: floatPtr(0.003 / 0.15)},
		{name: "kimi-k2-thinking", wantRatio: 0.3, wantComp: 25.0 / 6.0, wantCache: floatPtr(0.25)},
		{name: "kimi-k2.6", wantRatio: 0.475, wantComp: 4.0 / 0.95, wantCache: floatPtr(0.16 / 0.95)},
		{name: "qwen3.8-max", wantRatio: 12.0 / 1000 * RMB, wantComp: 36.0 / 12.0, wantCache: floatPtr(1.5 / 12.0)},
		{name: "qwen3.8-27b", wantRatio: 3.0 / 1000 * RMB, wantComp: 12.0 / 3.0, wantCache: floatPtr(0.6 / 3.0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.name, matchName)
			require.InDelta(t, tt.wantRatio, ratio, 1e-9)
			require.InDelta(t, tt.wantComp, GetCompletionRatio(tt.name), 1e-9)

			cacheRatio, hasCache := GetCacheRatio(tt.name)
			if tt.wantCache == nil {
				require.False(t, hasCache)
				return
			}
			require.True(t, hasCache)
			require.InDelta(t, *tt.wantCache, cacheRatio, 1e-9)
		})
	}
}

func TestSiliconFlowQualifiedPricingUsesCurrentOfficialRates(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	cases := []struct {
		name   string
		input  float64
		output float64
		cache  float64
	}{
		{name: "deepseek-ai/DeepSeek-R1", input: 0.5, output: 2.18, cache: 1},
		{name: "deepseek-ai/DeepSeek-V3", input: 0.25, output: 1, cache: 1},
		{name: "deepseek-ai/DeepSeek-V3.1-Terminus", input: 0.27, output: 1, cache: 1},
		{name: "deepseek-ai/DeepSeek-V3.2", input: 0.27, output: 0.42, cache: 0.135 / 0.27},
		{name: "Qwen/Qwen3.6-35B-A3B", input: 0.2, output: 1.6, cache: 1},
		{name: "Qwen/Qwen3.5-397B-A17B", input: 0.39, output: 2.34, cache: 1},
		{name: "Qwen/Qwen3.5-122B-A10B", input: 0.26, output: 2.08, cache: 1},
		{name: "Qwen/Qwen3.5-35B-A3B", input: 0.24, output: 1.8, cache: 1},
		{name: "Qwen/Qwen3.5-27B", input: 0.25, output: 2, cache: 1},
		{name: "Qwen/Qwen3.5-9B", input: 0.1, output: 0.15, cache: 1},
		{name: "Qwen/Qwen3-VL-32B-Instruct", input: 0.2, output: 0.6, cache: 1},
		{name: "Qwen/Qwen3-VL-32B-Thinking", input: 0.2, output: 1.5, cache: 1},
		{name: "Qwen/Qwen3-VL-8B-Instruct", input: 0.18, output: 0.68, cache: 1},
		{name: "Qwen/Qwen3-VL-30B-A3B-Instruct", input: 0.29, output: 1, cache: 1},
		{name: "Qwen/Qwen3-VL-30B-A3B-Thinking", input: 0.29, output: 1, cache: 1},
		{name: "Qwen/Qwen3-Coder-30B-A3B-Instruct", input: 0.07, output: 0.28, cache: 1},
		{name: "Qwen/Qwen3-Coder-480B-A35B-Instruct", input: 0.25, output: 1, cache: 1},
		{name: "Qwen/Qwen3-30B-A3B-Instruct-2507", input: 0.09, output: 0.3, cache: 1},
		{name: "Qwen/Qwen3-30B-A3B-Thinking-2507", input: 0.09, output: 0.3, cache: 1},
		{name: "Qwen/Qwen3-32B", input: 0.14, output: 0.57, cache: 1},
		{name: "Qwen/Qwen3-14B", input: 0.07, output: 0.28, cache: 1},
		{name: "Qwen/Qwen3-8B", input: 0.06, output: 0.06, cache: 1},
		{name: "Qwen/Qwen2.5-72B-Instruct", input: 0.59, output: 0.59, cache: 1},
		{name: "Qwen/Qwen2.5-7B-Instruct", input: 0.05, output: 0.05, cache: 1},
		{name: "Qwen/Qwen3-Embedding-8B", input: 0.04, output: 0, cache: 1},
		{name: "Qwen/Qwen3-Embedding-0.6B", input: 0.01, output: 0, cache: 1},
		{name: "Qwen/Qwen3-Reranker-8B", input: 0.04, output: 0, cache: 1},
		{name: "baidu/ERNIE-4.5-300B-A47B", input: 0.28, output: 1.1, cache: 1},
		{name: "tencent/Hunyuan-A13B-Instruct", input: 0.14, output: 0.57, cache: 1},
		{name: "stepfun-ai/Step-3.5-Flash", input: 0.1, output: 0.3, cache: 1},
		{name: "inclusionAI/Ling-flash-2.0", input: 0.14, output: 0.57, cache: 1},
		{name: "ByteDance-Seed/Seed-OSS-36B-Instruct", input: 0.21, output: 0.57, cache: 1},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.name, matchName)
			require.InDelta(t, tt.input/2, ratio, 1e-9)
			wantCompletion := tt.output / tt.input
			if tt.output == 0 {
				wantCompletion = 1
			}
			require.InDelta(t, wantCompletion, GetCompletionRatio(tt.name), 1e-9)
			cacheRatio, hasCache := GetCacheRatio(tt.name)
			require.True(t, hasCache)
			require.InDelta(t, tt.cache, cacheRatio, 1e-9)
		})
	}

	for _, tt := range []struct {
		name  string
		price float64
	}{
		{name: "Qwen/Qwen-Image", price: 0.02},
		{name: "Qwen/Qwen-Image-Edit", price: 0.04},
		{name: "Qwen/Qwen-Image-Edit-2509", price: 0.04},
		{name: "Wan-AI/Wan2.2-I2V-A14B", price: 0.29},
		{name: "Wan-AI/Wan2.2-T2V-A14B", price: 0.29},
	} {
		t.Run(tt.name, func(t *testing.T) {
			price, ok := GetModelPrice(tt.name, false)
			require.True(t, ok)
			require.InDelta(t, tt.price, price, 1e-9)
		})
	}

	for _, name := range []string{
		"FunAudioLLM/CosyVoice2-0.5B",
		"IndexTeam/IndexTTS-2",
		"TeleAI/TeleSpeechASR",
		"grok-imagine-video",
	} {
		t.Run("unsupported-unit/"+name, func(t *testing.T) {
			_, ok := GetModelPrice(name, false)
			require.False(t, ok)
		})
	}
}

func TestKimiK26VariantPricingUsesOfficialBaseModel(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	for _, name := range []string{
		"kimi-k2.6",
		"kimi-k2-6",
		"kimi-k2.6-thinking",
		"moonshotai/kimi-k2.6",
		"moonshotai/Kimi-K2-6-thinking",
	} {
		t.Run(name, func(t *testing.T) {
			modelRatio, ok, matchName := GetConfiguredModelRatio(name)
			require.True(t, ok)
			require.Equal(t, "kimi-k2.6", matchName)
			require.InDelta(t, 0.475, modelRatio, 1e-9)
			require.InDelta(t, 4.0/0.95, GetCompletionRatio(name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(name)
			require.True(t, hasCacheRatio)
			require.InDelta(t, 0.16/0.95, cacheRatio, 1e-9)
		})
	}
}

func TestKimiK27CodeOfficialPricingVariants(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	cases := []struct {
		name       string
		matchName  string
		modelRatio float64
		completion float64
		cacheRatio float64
	}{
		{name: "kimi-k2.7-code", matchName: "kimi-k2.7-code", modelRatio: 0.475, completion: 4.0 / 0.95, cacheRatio: 0.19 / 0.95},
		{name: "kimi-k2-7-code-thinking", matchName: "kimi-k2.7-code", modelRatio: 0.475, completion: 4.0 / 0.95, cacheRatio: 0.19 / 0.95},
		{name: "moonshotai/Kimi-K2-7-Code-free", matchName: "kimi-k2.7-code", modelRatio: 0.475, completion: 4.0 / 0.95, cacheRatio: 0.19 / 0.95},
		{name: "kimi-k2.7-code-highspeed", matchName: "kimi-k2.7-code-highspeed", modelRatio: 0.95, completion: 8.0 / 1.90, cacheRatio: 0.38 / 1.90},
		{name: "kimi-k2-7-code-highspeed-thinking", matchName: "kimi-k2.7-code-highspeed", modelRatio: 0.95, completion: 8.0 / 1.90, cacheRatio: 0.38 / 1.90},
		{name: "moonshotai/Kimi-K2-7-Code-HighSpeed", matchName: "kimi-k2.7-code-highspeed", modelRatio: 0.95, completion: 8.0 / 1.90, cacheRatio: 0.38 / 1.90},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.matchName, matchName)
			require.InDelta(t, tt.modelRatio, ratio, 1e-9)
			require.InDelta(t, tt.completion, GetCompletionRatio(tt.name), 1e-9)
			cacheRatio, hasCacheRatio := GetCacheRatio(tt.name)
			require.True(t, hasCacheRatio)
			require.InDelta(t, tt.cacheRatio, cacheRatio, 1e-9)
		})
	}
}

func TestGPT56SolWMVariantUsesBasePricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	modelRatio, ok, matchName := GetConfiguredModelRatio("gpt-5.6-sol-wm")
	require.True(t, ok)
	require.Equal(t, "gpt-5.6-sol", matchName)
	require.InDelta(t, 2.5, modelRatio, 1e-9)
	require.InDelta(t, 6.0, GetCompletionRatio("gpt-5.6-sol-wm"), 1e-9)

	cacheRatio, hasCacheRatio := GetCacheRatio("gpt-5.6-sol-wm")
	require.True(t, hasCacheRatio)
	require.InDelta(t, 0.1, cacheRatio, 1e-9)

	createCacheRatio, hasCreateCacheRatio := GetCreateCacheRatio("gpt-5.6-sol-wm")
	require.True(t, hasCreateCacheRatio)
	require.InDelta(t, 1.25, createCacheRatio, 1e-9)
}

func TestGemma4OfficialModelsAreExplicitlyFree(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	cases := []struct {
		name      string
		matchName string
	}{
		{name: "gemma-4-31b-it", matchName: "gemma-4-31b-it"},
		{name: "google/gemma-4-31b-it", matchName: "gemma-4-31b-it"},
		{name: "gemma-4-26b-a4b-it", matchName: "gemma-4-26b-a4b-it"},
		{name: "google/gemma-4-26b-a4b-it-free", matchName: "gemma-4-26b-a4b-it"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			modelRatio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.matchName, matchName)
			require.Zero(t, modelRatio)
			require.InDelta(t, 1.0, GetCompletionRatio(tt.name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(tt.name)
			require.True(t, hasCacheRatio)
			require.Zero(t, cacheRatio)
		})
	}
}

func TestGrokImagineAliasesUseClosestOfficialImagePrice(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	aliases := map[string]float64{
		"grok-imagine":                           0.02,
		"grok-imagine-1":                         0.02,
		"grok-imagine-edit":                      0.02,
		"grok-imagine-image-2026-03-02":          0.02,
		"grok-imagine-image-quality-20260403":    0.05,
		"grok-imagine-image-quality-latest":      0.05,
		"grok-imagine-image-pro":                 0.05,
		"grok/grok-imagine":                      0.02,
		"xai/grok-imagine-1":                     0.02,
		"x-ai/grok-imagine-edit":                 0.02,
		"xai/grok-imagine-image-2026-03-02":      0.02,
		"x-ai/grok-imagine-image-quality-latest": 0.05,
	}
	for name, want := range aliases {
		t.Run(name, func(t *testing.T) {
			modelPrice, ok := GetModelPrice(name, false)
			require.True(t, ok)
			require.InDelta(t, want, modelPrice, 1e-9)
		})
	}

	for _, name := range []string{"grok-imagine-video", "grok-imagine-video-1.5"} {
		t.Run(name, func(t *testing.T) {
			_, ok := GetModelPrice(name, false)
			require.False(t, ok)
		})
	}
}

func TestGLM51And52OfficialPricingRatios(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	for _, name := range []string{"glm-5.1", "glm-5-1", "glm-5.2", "glm-5-2", "zai-org/GLM-5.1", "z-ai/glm-5.2-thinking"} {
		t.Run(name, func(t *testing.T) {
			ratio, ok, _ := GetConfiguredModelRatio(name)
			require.True(t, ok)
			require.InDelta(t, 0.7, ratio, 1e-9)
			require.InDelta(t, 4.4/1.4, GetCompletionRatio(name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(name)
			require.True(t, hasCacheRatio)
			require.InDelta(t, 0.26/1.4, cacheRatio, 1e-9)
		})
	}
}

// glm-5.3-flashx is priced alongside the rest of the glm-5.3 family, so it
// needs a cache entry too: GetCacheRatio falls back to 1.0 when the key is
// absent, which would bill a cache hit at the full input price.
func TestGLM53FlashxCacheRatioIsPriced(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	const name = "glm-5.3-flashx"

	ratio, ok, _ := GetConfiguredModelRatio(name)
	require.True(t, ok, "glm-5.3-flashx must have a model ratio")
	require.InDelta(t, 0.37/2, ratio, 1e-9)

	cacheRatio, hasCacheRatio := GetCacheRatio(name)
	require.True(t, hasCacheRatio, "glm-5.3-flashx must have a cache ratio, not the 1.0 fallback")
	require.InDelta(t, 0.075/0.37, cacheRatio, 1e-9)
}

func TestMiniMaxOfficialPricingVariants(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	cases := []struct {
		name        string
		modelRatio  float64
		cacheRatio  float64
		createRatio float64
	}{
		{name: "minimax-m2", modelRatio: 0.15, cacheRatio: 0.03 / 0.30, createRatio: 0.375 / 0.30},
		{name: "minimax-m2.1", modelRatio: 0.15, cacheRatio: 0.03 / 0.30, createRatio: 0.375 / 0.30},
		{name: "minimax-m2.5", modelRatio: 0.15, cacheRatio: 0.03 / 0.30, createRatio: 0.375 / 0.30},
		{name: "minimax-m2.7", modelRatio: 0.15, cacheRatio: 0.06 / 0.30, createRatio: 0.375 / 0.30},
		{name: "MiniMax-M2.1-highspeed", modelRatio: 0.3, cacheRatio: 0.03 / 0.60, createRatio: 0.375 / 0.60},
		{name: "MiniMax-M2.5-highspeed", modelRatio: 0.3, cacheRatio: 0.03 / 0.60, createRatio: 0.375 / 0.60},
		{name: "MiniMax-M2.7-highspeed", modelRatio: 0.3, cacheRatio: 0.06 / 0.60, createRatio: 0.375 / 0.60},
		{name: "minimaxai/MiniMax-M2.7-highspeed", modelRatio: 0.3, cacheRatio: 0.06 / 0.60, createRatio: 0.375 / 0.60},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok, _ := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.InDelta(t, tt.modelRatio, ratio, 1e-9)
			require.InDelta(t, 4.0, GetCompletionRatio(tt.name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(tt.name)
			require.True(t, hasCacheRatio)
			require.InDelta(t, tt.cacheRatio, cacheRatio, 1e-9)

			createRatio, hasCreateRatio := GetCreateCacheRatio(tt.name)
			require.True(t, hasCreateRatio)
			require.InDelta(t, tt.createRatio, createRatio, 1e-9)
		})
	}
}

func TestMiniMaxM3UsesPermanentPrice(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	for _, name := range []string{"minimax-m3", "MiniMax-M3", "minimaxai/minimax-m3"} {
		t.Run(name, func(t *testing.T) {
			ratio, ok, _ := GetConfiguredModelRatio(name)
			require.True(t, ok)
			require.InDelta(t, 0.15, ratio, 1e-9)
			require.InDelta(t, 4.0, GetCompletionRatio(name), 1e-9)
			cache, hasCache := GetCacheRatio(name)
			require.True(t, hasCache)
			require.InDelta(t, 0.2, cache, 1e-9)
		})
	}
}

func TestMiniMaxHighspeedRemainsDistinctFromNormalPricing(t *testing.T) {
	require.Equal(t, "minimax-m2.7-highspeed", FormatMatchingModelName("MiniMax-M2.7-highspeed"))
	require.Equal(t, "minimax-m2.7", FormatMatchingModelName("MiniMax-M2.7"))
	_, configured, _ := GetConfiguredModelRatio("MiniMax-M2-highspeed")
	require.False(t, configured)
}

func TestModelNameMatchesLimitUsesPricingCandidates(t *testing.T) {
	limits := map[string]bool{
		"gpt-5.6-sol":                   true,
		"gpt-5.5":                       true,
		"deepseek-ai/deepseek-v4-flash": true,
		"Kimi-K3":                       true,
	}

	require.True(t, ModelNameMatchesLimit("gpt-5.6-sol-openai-compact", limits))
	require.True(t, ModelNameMatchesLimit("deepseek-ai/DeepSeek-V4-Flash-search", limits))
	require.True(t, ModelNameMatchesLimit("moonshotai/kimi-k3-thinking", limits))
	require.True(t, ModelNameMatchesLimit("gpt-5.5-20250807", limits))
	require.False(t, ModelNameMatchesLimit("gpt-5.2-pro", limits))
	require.False(t, ModelNameMatchesLimit("unknown-model", limits))
	require.False(t, ModelNameMatchesLimit("kimi-k3-pro", limits))
	require.False(t, ModelNameMatchesLimit("grok-4.3-experimental", limits))
	require.False(t, ModelNameMatchesLimit("gpt-5.5-12345678", limits))
	require.False(t, ModelNameMatchesLimit("claude-sonnet-4-6-12345678", limits))
}

func TestGetConfiguredModelRatioMissingModel(t *testing.T) {
	InitRatioSettings()

	ratio, ok, matchName := GetConfiguredModelRatio("missing-model-for-pricing-test")
	require.False(t, ok)
	require.Zero(t, ratio)
	require.Equal(t, "missing-model-for-pricing-test", matchName)
}

func TestAgnes25ProUsesOfficialStablePricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	ratio, ok, matchName := GetConfiguredModelRatio("agnes-2.5-pro")
	require.True(t, ok)
	require.InDelta(t, 0.225, ratio, 1e-9)
	require.Equal(t, "agnes-2.5-pro", matchName)

	require.InDelta(t, 2, GetCompletionRatio("agnes-2.5-pro"), 1e-9)
	cacheRatio, hasCacheRatio := GetCacheRatio("agnes-2.5-pro")
	require.True(t, hasCacheRatio)
	require.InDelta(t, 0.1, cacheRatio, 1e-9)

	alphaRatio, alphaConfigured, alphaMatchName := GetConfiguredModelRatio("agnes-2.5-pro-alpha")
	require.True(t, alphaConfigured)
	require.Equal(t, "agnes-2.5-pro-alpha", alphaMatchName)
	require.InDelta(t, 0.225, alphaRatio, 1e-9)
}

func TestAgnes25ProBetaUsesOfficialListPricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	ratio, ok, matchName := GetConfiguredModelRatio("agnes-2.5-pro-beta")
	require.True(t, ok)
	require.Equal(t, "agnes-2.5-pro-beta", matchName)
	require.InDelta(t, 0.05, ratio, 1e-9)
	require.InDelta(t, 3, GetCompletionRatio("agnes-2.5-pro-beta"), 1e-9)

	cache, hasCache := GetCacheRatio("agnes-2.5-pro-beta")
	require.True(t, hasCache)
	require.InDelta(t, 0.1, cache, 1e-9)
}

func TestQwen38ModelOfficialPricingVariants(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	for _, name := range []string{"qwen3.8-27b", "qwen-3.8-27b", "Qwen3.8-27B"} {
		t.Run(name, func(t *testing.T) {
			ratio, ok, _ := GetConfiguredModelRatio(name)
			require.True(t, ok)
			require.InDelta(t, 3.0/1000*RMB, ratio, 1e-9)
			require.InDelta(t, 4.0, GetCompletionRatio(name), 1e-9)
			cache, hasCache := GetCacheRatio(name)
			require.True(t, hasCache)
			require.InDelta(t, 0.6/3.0, cache, 1e-9)
			create, hasCreate := GetCreateCacheRatio(name)
			require.True(t, hasCreate)
			require.InDelta(t, 3.75/3.0, create, 1e-9)
		})
	}
}

func TestLongCat20UsesOfficialStandardChatPricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	ratio, ok, matchName := GetConfiguredModelRatio("longcat-2.0")
	require.True(t, ok)
	require.Equal(t, "longcat-2.0", matchName)
	require.InDelta(t, 5.0/1000*RMB, ratio, 1e-9)
	require.InDelta(t, 4.0, GetCompletionRatio("longcat-2.0"), 1e-9)

	cache, hasCache := GetCacheRatio("longcat-2.0")
	require.True(t, hasCache)
	require.InDelta(t, 0.10/5.0, cache, 1e-9)

	// LongCat publishes cached input but no cache-creation price. Keep the
	// creation ratio absent instead of inferring one.
	_, hasCreate := GetCreateCacheRatio("longcat-2.0")
	require.False(t, hasCreate)
}

func TestQwen36OfficialAlibabaPricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	ratio, ok, matchName := GetConfiguredModelRatio("Qwen3.6-35B-A3B")
	require.True(t, ok)
	require.Equal(t, "Qwen3.6-35B-A3B", matchName)
	require.InDelta(t, 1.8/1000*RMB, ratio, 1e-9)
	require.InDelta(t, 10.8/1.8, GetCompletionRatio("Qwen3.6-35B-A3B"), 1e-9)
	_, hasCache := GetCacheRatio("Qwen3.6-35B-A3B")
	require.False(t, hasCache)
	_, hasCreate := GetCreateCacheRatio("Qwen3.6-35B-A3B")
	require.False(t, hasCreate)
}

func TestSparkX25OfficialOriginalPricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	ratio, ok, matchName := GetConfiguredModelRatio("spark-x2.5")
	require.True(t, ok)
	require.Equal(t, "spark-x2.5", matchName)
	require.InDelta(t, 3.2/1000*RMB, ratio, 1e-9)
	require.InDelta(t, 12.0/3.2, GetCompletionRatio("spark-x2.5"), 1e-9)
	cache, hasCache := GetCacheRatio("spark-x2.5")
	require.True(t, hasCache)
	require.InDelta(t, 0.48/3.2, cache, 1e-9)
	_, hasCreate := GetCreateCacheRatio("spark-x2.5")
	require.False(t, hasCreate)
}

func TestAdditionalOfficialPricingVariants(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	cases := []struct {
		name             string
		modelRatio       float64
		completionRatio  float64
		cacheRatio       float64
		createCacheRatio *float64
	}{
		{name: "mimo-v2.5-pro", modelRatio: 0.435 / 2, completionRatio: 0.87 / 0.435, cacheRatio: 0.0036 / 0.435},
		{name: "qwen3.7-max", modelRatio: 12.0 / 1000 * RMB, completionRatio: 36.0 / 12.0, cacheRatio: 2.4 / 12.0, createCacheRatio: floatPtr(15.0 / 12.0)},
		{name: "qwen3.7-plus", modelRatio: 2.0 / 1000 * RMB, completionRatio: 8.0 / 2.0, cacheRatio: 0.4 / 2.0, createCacheRatio: floatPtr(2.5 / 2.0)},
		{name: "step-explore", modelRatio: 1.35 / 1000 * RMB, completionRatio: 8.1 / 1.35, cacheRatio: 0.27 / 1.35},
		{name: "stepaudio-2.5-chat", modelRatio: 10.0 / 1000 * RMB, completionRatio: 25.0 / 10.0, cacheRatio: 2.0 / 10.0},
		{name: "stepaudio-2.5-realtime", modelRatio: 10.0 / 1000 * RMB, completionRatio: 70.0 / 10.0, cacheRatio: 2.0 / 10.0},
		{name: "glm-4.5", modelRatio: 0.3, completionRatio: 2.2 / 0.6, cacheRatio: 0.11 / 0.6, createCacheRatio: floatPtr(0)},
		{name: "glm-4.5-air", modelRatio: 0.1, completionRatio: 1.1 / 0.2, cacheRatio: 0.03 / 0.2, createCacheRatio: floatPtr(0)},
		{name: "glm-4.6", modelRatio: 0.3, completionRatio: 2.2 / 0.6, cacheRatio: 0.11 / 0.6, createCacheRatio: floatPtr(0)},
		{name: "glm-4.7", modelRatio: 0.3, completionRatio: 2.2 / 0.6, cacheRatio: 0.11 / 0.6, createCacheRatio: floatPtr(0)},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok, _ := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.InDelta(t, tt.modelRatio, ratio, 1e-9)
			require.InDelta(t, tt.completionRatio, GetCompletionRatio(tt.name), 1e-9)
			cache, hasCache := GetCacheRatio(tt.name)
			require.True(t, hasCache)
			require.InDelta(t, tt.cacheRatio, cache, 1e-9)
			create, hasCreate := GetCreateCacheRatio(tt.name)
			require.Equal(t, tt.createCacheRatio != nil, hasCreate)
			if tt.createCacheRatio != nil {
				require.InDelta(t, *tt.createCacheRatio, create, 1e-9)
			}
		})
	}

	for _, name := range []string{"glm-4.5-flash", "glm-4.7-flash"} {
		t.Run(name, func(t *testing.T) {
			ratio, ok, _ := GetConfiguredModelRatio(name)
			require.True(t, ok)
			require.Zero(t, ratio)
			require.Zero(t, GetCompletionRatio(name))
			cache, hasCache := GetCacheRatio(name)
			require.True(t, hasCache)
			require.Zero(t, cache)
		})
	}
}

func TestStepAudioASRUsesOfficialHourlyPricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	ratio, ok, matchName := GetConfiguredModelRatio("stepaudio-2.5-asr")
	require.True(t, ok)
	require.Equal(t, "stepaudio-2.5-asr", matchName)
	require.InDelta(t, 2.5/1000*RMB, ratio, 1e-9)
	require.Zero(t, GetCompletionRatio("stepaudio-2.5-asr"))

	audioRatio, hasAudioRatio := GetAudioRatio("stepaudio-2.5-asr"), ContainsAudioRatio("stepaudio-2.5-asr")
	require.True(t, hasAudioRatio)
	require.InDelta(t, 1, audioRatio, 1e-9)
	audioCompletionRatio, hasAudioCompletionRatio := GetAudioCompletionRatio("stepaudio-2.5-asr"), ContainsAudioCompletionRatio("stepaudio-2.5-asr")
	require.True(t, hasAudioCompletionRatio)
	require.Zero(t, audioCompletionRatio)
}

func TestStepAudioTTSUsesOfficialCharacterPricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	ratio, ok, matchName := GetConfiguredModelRatio("stepaudio-2.5-tts")
	require.True(t, ok)
	require.Equal(t, "stepaudio-2.5-tts", matchName)
	require.InDelta(t, 5.8/10*RMB, ratio, 1e-9)
	require.Zero(t, GetCompletionRatio("stepaudio-2.5-tts"))

	audioRatio, hasAudioRatio := GetAudioRatio("stepaudio-2.5-tts"), ContainsAudioRatio("stepaudio-2.5-tts")
	require.True(t, hasAudioRatio)
	require.InDelta(t, 1, audioRatio, 1e-9)
	audioCompletionRatio, hasAudioCompletionRatio := GetAudioCompletionRatio("stepaudio-2.5-tts"), ContainsAudioCompletionRatio("stepaudio-2.5-tts")
	require.True(t, hasAudioCompletionRatio)
	require.Zero(t, audioCompletionRatio)
}

func TestStepImageEditUsesRMBPerCallPrice(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	price, ok := GetModelPrice("step-image-edit-2", false)
	require.True(t, ok)
	require.InDelta(t, 0.02/USD2RMB, price, 1e-12)
}

func TestGemini25FlashControlAliasesUseBasePricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	for _, name := range []string{"gemini-2.5-flash-thinking", "gemini-2.5-flash-nothinking"} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, "gemini-2.5-flash", FormatMatchingModelName(name))
			ratio, ok, matchName := GetConfiguredModelRatio(name)
			require.True(t, ok)
			require.Equal(t, "gemini-2.5-flash", matchName)
			require.InDelta(t, 0.15, ratio, 1e-9)
			require.InDelta(t, 2.5/0.3, GetCompletionRatio(name), 1e-9)
			cache, hasCache := GetCacheRatio(name)
			require.True(t, hasCache)
			require.InDelta(t, 0.1, cache, 1e-9)
		})
	}
}

func TestGetCompletionRatioInfoGPT55AndLaterUsesUnlockedOutputMultiplier(t *testing.T) {
	InitRatioSettings()

	tests := []string{
		"gpt-5.5",
		"gpt-5.6",
		"gpt-5.6-sol",
		"gpt-5.6-terra",
		"gpt-5.6-luna",
	}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			info := GetCompletionRatioInfo(name)

			want := 6.0
			if name == "gpt-5.6" || name == "gpt-5.6-sol" {
				want = 6.0
			}
			require.InDelta(t, want, info.Ratio, 1e-9)
			require.False(t, info.Locked)
		})
	}
}

func TestLockedCompletionRatioIgnoresUnqualifiedOverrides(t *testing.T) {
	resetPricingMapsForTest(t)
	completionRatioMap.AddAll(map[string]float64{
		"gpt-4o-2024-05-13": 99,
		"gpt-5.4":           99,
		"o1":                99,
		"claude-opus-4-6":   99,
	})

	tests := []struct {
		name string
		want float64
		lock bool
	}{
		{name: "gpt-4o-2024-05-13", want: 3, lock: true},
		{name: "gpt-5.4", want: 6, lock: true},
		{name: "o1", want: 4, lock: true},
		{name: "claude-opus-4-6", want: 5, lock: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := GetCompletionRatioInfo(tt.name)
			require.InDelta(t, tt.want, info.Ratio, 1e-9)
			require.Equal(t, tt.lock, info.Locked)
		})
	}
}

func TestQualifiedCompletionRatioCanOverrideLockedBase(t *testing.T) {
	resetPricingMapsForTest(t)
	completionRatioMap.Set("openai/gpt-5.4", 99)

	info := GetCompletionRatioInfo("openai/gpt-5.4")
	require.InDelta(t, 99, info.Ratio, 1e-9)
	require.False(t, info.Locked)
}

func TestGPT56OfficialPricingRatios(t *testing.T) {
	InitRatioSettings()
	tests := []struct {
		name            string
		modelRatio      float64
		completionRatio float64
	}{
		{name: "gpt-5.6", modelRatio: 2.5, completionRatio: 6},
		{name: "gpt-5.6-sol", modelRatio: 2.5, completionRatio: 6},
		{name: "gpt-5.6-terra", modelRatio: 1.0, completionRatio: 6},
		{name: "gpt-5.6-luna", modelRatio: 0.1, completionRatio: 6},
		{name: "gpt-6-astra", modelRatio: 5.0, completionRatio: 5},
		{name: "gpt-5.6-cyber", modelRatio: 6.25, completionRatio: 6},
		{name: "gpt-daybreak-blue-latest", modelRatio: 2.5, completionRatio: 6},
		{name: "gpt-daybreak-red-latest", modelRatio: 6.25, completionRatio: 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modelRatio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.name, matchName)
			require.InDelta(t, tt.modelRatio, modelRatio, 1e-9)
			require.InDelta(t, tt.completionRatio, GetCompletionRatio(tt.name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(tt.name)
			require.True(t, hasCacheRatio)
			require.InDelta(t, 0.1, cacheRatio, 1e-9)

			createCacheRatio, hasCreateCacheRatio := GetCreateCacheRatio(tt.name)
			require.True(t, hasCreateCacheRatio)
			require.InDelta(t, 1.25, createCacheRatio, 1e-9)
		})
	}
}

func TestOpenAIAudioOutputRatiosUseOfficialTwoToOneMultiplier(t *testing.T) {
	InitRatioSettings()
	for _, name := range []string{
		"gpt-4o-audio-preview",
		"gpt-4o-realtime-preview",
		"gpt-4o-mini-audio-preview",
		"gpt-4o-mini-realtime-preview",
	} {
		t.Run(name, func(t *testing.T) {
			ratio, ok := GetAudioCompletionRatio(name), ContainsAudioCompletionRatio(name)
			require.True(t, ok)
			require.InDelta(t, 2.0, ratio, 1e-9)
		})
	}
}

func TestKimiK3VariantPricingUsesOfficialBaseModel(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	for _, name := range []string{
		"kimi-k3",
		"Kimi-K3",
		"kimi-k3-free",
		"kimi-k3-thinking",
		"kimi-k3:free",
		"kimi-k3_high",
		"moonshotai/kimi-k3-free",
		"moonshotai/Kimi-K3-Max",
	} {
		t.Run(name, func(t *testing.T) {
			modelRatio, ok, matchName := GetConfiguredModelRatio(name)
			require.True(t, ok)
			wantMatchName := "kimi-k3"
			if name == "Kimi-K3" {
				wantMatchName = "Kimi-K3"
			}
			require.Equal(t, wantMatchName, matchName)
			require.InDelta(t, 1.5, modelRatio, 1e-9)
			require.InDelta(t, 5.0, GetCompletionRatio(name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(name)
			require.True(t, hasCacheRatio)
			require.InDelta(t, 0.1, cacheRatio, 1e-9)
		})
	}
}

func TestOpenAIImageModelPricingAndImageInputRatios(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	cases := []struct {
		name       string
		modelRatio float64
		completion float64
		cacheRatio float64
		imageRatio float64
		matchName  string
	}{
		{name: "gpt-image-1-mini", modelRatio: 1, completion: 4, cacheRatio: 0.1, imageRatio: 1.25, matchName: "gpt-image-1-mini"},
		{name: "gpt-image-1", modelRatio: 2.5, completion: 8, cacheRatio: 0.25, imageRatio: 2, matchName: "gpt-image-1"},
		{name: "gpt-image-1.5", modelRatio: 2.5, completion: 2, cacheRatio: 0.25, imageRatio: 1.6, matchName: "gpt-image-1.5"},
		{name: "gpt-image-2", modelRatio: 2.5, completion: 6, cacheRatio: 0.25, imageRatio: 1.6, matchName: "gpt-image-2"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.matchName, matchName)
			require.InDelta(t, tt.modelRatio, ratio, 1e-9)
			require.InDelta(t, tt.completion, GetCompletionRatio(tt.name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(tt.name)
			require.True(t, hasCacheRatio)
			require.InDelta(t, tt.cacheRatio, cacheRatio, 1e-9)

			imageRatio, hasImageRatio := GetImageRatio(tt.name)
			require.True(t, hasImageRatio)
			require.InDelta(t, tt.imageRatio, imageRatio, 1e-9)
		})
	}
}

func TestOpenAIImageModelVariantsUseOfficialBasePricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	cases := []struct {
		name       string
		base       string
		imageRatio float64
	}{
		{name: "openai/gpt-image-1-mini-2025-12-16", base: "gpt-image-1-mini", imageRatio: 1.25},
		{name: "gpt-image-1.5-2025-12-16", base: "gpt-image-1.5", imageRatio: 1.6},
		{name: "gpt-image-2-2026-04-21", base: "gpt-image-2", imageRatio: 1.6},
		{name: "gpt-image-2-2k", base: "gpt-image-2", imageRatio: 1.6},
		{name: "gpt-image-2-pro", base: "gpt-image-2", imageRatio: 1.6},
		{name: "gpt-image-2-all", base: "gpt-image-2", imageRatio: 1.6},
		{name: "gpt-image-2.5-sunburst", base: "gpt-image-2", imageRatio: 1.6},
		{name: "gpt-image-2.5-flare", base: "gpt-image-2", imageRatio: 1.6},
		{name: "openai/gpt-image-2.5-sunburst", base: "gpt-image-2", imageRatio: 1.6},
		{name: "openai/gpt-image-2.5-flare", base: "gpt-image-2", imageRatio: 1.6},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.base, matchName)

			cacheRatio, hasCacheRatio := GetCacheRatio(tt.name)
			require.True(t, hasCacheRatio)
			wantCacheRatio := 0.25
			if tt.base == "gpt-image-1-mini" {
				wantCacheRatio = 0.1
			}
			require.InDelta(t, wantCacheRatio, cacheRatio, 1e-9)

			imageRatio, hasImageRatio := GetImageRatio(tt.name)
			require.True(t, hasImageRatio)
			require.InDelta(t, tt.imageRatio, imageRatio, 1e-9)
			wantCompletion := 6.0
			switch tt.base {
			case "gpt-image-1-mini":
				wantCompletion = 4
			case "gpt-image-1":
				wantCompletion = 8
			case "gpt-image-1.5":
				wantCompletion = 2
			}
			require.InDelta(t, wantCompletion, GetCompletionRatio(tt.name), 1e-9)
		})
	}
}

func TestPricingLookupIsCaseInsensitive(t *testing.T) {
	resetPricingMapsForTest(t)
	modelRatioMap.Set("ERNIE-4.0-8K", 0.876)
	completionRatioMap.Set("ERNIE-4.0-8K", 2)

	ratio, ok, matchName := GetConfiguredModelRatio("ernie-4.0-8k")
	require.True(t, ok)
	require.Equal(t, "ERNIE-4.0-8K", matchName)
	require.InDelta(t, 0.876, ratio, 1e-9)
	require.InDelta(t, 2, GetCompletionRatio("ErNiE-4.0-8k"), 1e-9)
}

func TestSupplierSpecificVariantOverridesGenericBase(t *testing.T) {
	resetPricingMapsForTest(t)
	modelRatioMap.AddAll(map[string]float64{
		"kimi-k3":                       1.5,
		"moonshotai/kimi-k3":            2.25,
		"deepseek-v4-flash":             0.07,
		"deepseek-ai/deepseek-v4-flash": 0.11,
	})
	completionRatioMap.AddAll(map[string]float64{
		"kimi-k3":                       5,
		"moonshotai/kimi-k3":            4,
		"deepseek-v4-flash":             2,
		"deepseek-ai/deepseek-v4-flash": 3,
	})

	ratio, ok, matchName := GetConfiguredModelRatio("moonshotai/Kimi-K3-free")
	require.True(t, ok)
	require.Equal(t, "moonshotai/kimi-k3", matchName)
	require.InDelta(t, 2.25, ratio, 1e-9)
	require.InDelta(t, 4, GetCompletionRatio("moonshotai/Kimi-K3-free"), 1e-9)

	ratio, ok, matchName = GetConfiguredModelRatio("deepseek-ai/DeepSeek-V4-Flash-search")
	require.True(t, ok)
	require.Equal(t, "deepseek-ai/deepseek-v4-flash", matchName)
	require.InDelta(t, 0.11, ratio, 1e-9)
	require.InDelta(t, 3, GetCompletionRatio("deepseek-ai/DeepSeek-V4-Flash-search"), 1e-9)
}

func TestKnownModelVariantsUseClosestConfiguredPricing(t *testing.T) {
	resetPricingMapsForTest(t)
	modelRatioMap.AddAll(map[string]float64{
		"gemini-3.1-pro":                 1,
		"gemini-3-pro-preview":           1,
		"gemini-3-pro-image-preview":     1,
		"gemini-3.1-flash-image-preview": 0.25,
		"glm-5.2":                        0.5479452054794521,
		"gpt-5.3-codex-spark":            0.875,
		"gpt-5.4-mini":                   0.375,
		"gpt-5.5":                        2.5,
		"gpt-5.6-luna":                   0.1,
		"gpt-5.6-sol":                    2.5,
		"gpt-5.6-terra":                  1,
		"grok-4.5":                       1,
	})
	completionRatioMap.AddAll(map[string]float64{
		"gemini-3.1-pro":                 6,
		"gemini-3-pro-preview":           6,
		"gemini-3-pro-image-preview":     6,
		"gemini-3.1-flash-image-preview": 6,
		"glm-5.2":                        3.5,
		"gpt-5.3-codex-spark":            8,
		"gpt-5.4-mini":                   6,
		"gpt-5.5":                        6,
		"gpt-5.6-luna":                   6,
		"gpt-5.6-sol":                    6,
		"gpt-5.6-terra":                  6,
		"grok-4.5":                       3,
	})
	cacheRatioMap.AddAll(map[string]float64{
		"gemini-3.1-pro":       0.1,
		"gemini-3-pro-preview": 0.1,
		"glm-5.2":              0.25,
		"gpt-5.3-codex-spark":  0.1,
		"gpt-5.4-mini":         0.1,
		"gpt-5.5":              0.1,
		"gpt-5.6-luna":         0.1,
		"gpt-5.6-sol":          0.1,
		"gpt-5.6-terra":        0.1,
		"grok-4.5":             0.15,
	})
	createCacheRatioMap.AddAll(map[string]float64{
		"gpt-5.6-luna":  0.1,
		"gpt-5.6-sol":   0.1,
		"gpt-5.6-terra": 0.1,
	})

	tests := []struct {
		name       string
		normalized string
		matchName  string
		ratio      float64
		completion float64
		cache      float64
	}{
		{name: "gemini-3-1-pro", normalized: "gemini-3.1-pro-preview", matchName: "gemini-3.1-pro", ratio: 1, completion: 6, cache: 0.1},
		{name: "gemini-3-pro", normalized: "gemini-3-pro-preview", matchName: "gemini-3-pro-preview", ratio: 1, completion: 6, cache: 0.1},
		{name: "glm-5-2", normalized: "glm-5.2", matchName: "glm-5.2", ratio: 0.5479452054794521, completion: 3.5, cache: 0.25},
		{name: "gpt-5.3-codex-spark-openai-compact", normalized: "gpt-5.3-codex-spark-openai-compact", matchName: "gpt-5.3-codex-spark", ratio: 0.875, completion: 8, cache: 0.1},
		{name: "gpt-5.4-mini-openai-compact", normalized: "gpt-5.4-mini-openai-compact", matchName: "gpt-5.4-mini", ratio: 0.375, completion: 6, cache: 0.1},
		{name: "gpt-5-5", normalized: "gpt-5.5", matchName: "gpt-5.5", ratio: 2.5, completion: 6, cache: 0.1},
		{name: "gpt-5-6-luna", normalized: "gpt-5.6-luna", matchName: "gpt-5.6-luna", ratio: 0.1, completion: 6, cache: 0.1},
		{name: "gpt-5.6-luna-openai-compact", normalized: "gpt-5.6-luna-openai-compact", matchName: "gpt-5.6-luna", ratio: 0.1, completion: 6, cache: 0.1},
		{name: "gpt-5.6-luna-pro", normalized: "gpt-5.6-luna", matchName: "gpt-5.6-luna", ratio: 0.1, completion: 6, cache: 0.1},
		{name: "gpt-5.6-luna-pro-openai-compact", normalized: "gpt-5.6-luna-openai-compact", matchName: "gpt-5.6-luna", ratio: 0.1, completion: 6, cache: 0.1},
		{name: "gpt-5.6-sol-openai-compact", normalized: "gpt-5.6-sol-openai-compact", matchName: "gpt-5.6-sol", ratio: 2.5, completion: 6, cache: 0.1},
		{name: "gpt-5.6-sol-pro", normalized: "gpt-5.6-sol", matchName: "gpt-5.6-sol", ratio: 2.5, completion: 6, cache: 0.1},
		{name: "gpt-5.6-sol-pro-openai-compact", normalized: "gpt-5.6-sol-openai-compact", matchName: "gpt-5.6-sol", ratio: 2.5, completion: 6, cache: 0.1},
		{name: "gpt-5-6-terra", normalized: "gpt-5.6-terra", matchName: "gpt-5.6-terra", ratio: 1, completion: 6, cache: 0.1},
		{name: "gpt-5.6-terra-openai-compact", normalized: "gpt-5.6-terra-openai-compact", matchName: "gpt-5.6-terra", ratio: 1, completion: 6, cache: 0.1},
		{name: "gpt-5.6-terra-pro", normalized: "gpt-5.6-terra", matchName: "gpt-5.6-terra", ratio: 1, completion: 6, cache: 0.1},
		{name: "gpt-5.6-terra-pro-openai-compact", normalized: "gpt-5.6-terra-openai-compact", matchName: "gpt-5.6-terra", ratio: 1, completion: 6, cache: 0.1},
		{name: "grok-4-5", normalized: "grok-4.5", matchName: "grok-4.5", ratio: 1, completion: 3, cache: 0.15},
		{name: "gemini-3-pro-image", normalized: "gemini-3-pro-image-preview", matchName: "gemini-3-pro-image-preview", ratio: 1, completion: 6, cache: 0},
		{name: "gemini-3.1-flash-image", normalized: "gemini-3.1-flash-image-preview", matchName: "gemini-3.1-flash-image-preview", ratio: 0.25, completion: 6, cache: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.normalized, FormatMatchingModelName(tt.name))

			modelRatio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.matchName, matchName)
			require.InDelta(t, tt.ratio, modelRatio, 1e-9)
			require.InDelta(t, tt.completion, GetCompletionRatio(tt.name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(tt.name)
			if tt.cache == 0 {
				require.False(t, hasCacheRatio)
			} else {
				require.True(t, hasCacheRatio)
				require.InDelta(t, tt.cache, cacheRatio, 1e-9)
			}
		})
	}
}

func TestAllVendorVariantsUseClosestConfiguredBase(t *testing.T) {
	resetPricingMapsForTest(t)
	modelRatioMap.AddAll(map[string]float64{
		"gpt-5.5":            1,
		"claude-opus-4-6":    2,
		"gemini-3.1-pro":     3,
		"deepseek-v4-flash":  4,
		"glm-5.2":            5,
		"grok-4.5":           6,
		"kimi-k3":            7,
		"minimax-m2p5":       8,
		"qwen3.5-flash":      9,
		"llama3-8b-instruct": 10,
		"mistral-3.large":    11,
		"ERNIE-4.0-8K":       12,
		"step-3.5-flash":     13,
		"doubao-seed-1.6":    14,
		"gemma-4-31b-it":     15,
		"gpt-oss-120b":       16,
		"hunyuan":            17,
		"agnes-2.5-flash":    18,
	})

	tests := []struct {
		name      string
		matchName string
		wantRatio float64
	}{
		{name: "gpt-5-5-high", matchName: "gpt-5.5", wantRatio: 1},
		{name: "claude-opus-4.6-thinking", matchName: "claude-opus-4-6", wantRatio: 2},
		{name: "gemini-3-1-pro-thinking", matchName: "gemini-3.1-pro", wantRatio: 3},
		{name: "deepseek-ai/deepseek-v4-flash-search-nothinking", matchName: "deepseek-v4-flash", wantRatio: 4},
		{name: "glm-5-2-thinking", matchName: "glm-5.2", wantRatio: 5},
		{name: "grok-4-5-reasoning", matchName: "grok-4.5", wantRatio: 6},
		{name: "moonshotai/kimi-k3_high", matchName: "kimi-k3", wantRatio: 7},
		{name: "minimax-m2-5-thinking", matchName: "minimax-m2p5", wantRatio: 8},
		{name: "Qwen/qwen3-5-flash-thinking", matchName: "qwen3.5-flash", wantRatio: 9},
		{name: "llama-3-8b-instruct-free", matchName: "llama3-8b-instruct", wantRatio: 10},
		{name: "mistral-3-large", matchName: "mistral-3.large", wantRatio: 11},
		{name: "ernie-4.0-8k", matchName: "ERNIE-4.0-8K", wantRatio: 12},
		{name: "step-3-5-flash", matchName: "step-3.5-flash", wantRatio: 13},
		{name: "doubao-seed-1-6-thinking", matchName: "doubao-seed-1.6", wantRatio: 14},
		{name: "gemma-4-31b-it-free", matchName: "gemma-4-31b-it", wantRatio: 15},
		{name: "openai/gpt-oss-120b-free", matchName: "gpt-oss-120b", wantRatio: 16},
		{name: "hunyuan-thinking", matchName: "hunyuan", wantRatio: 17},
		{name: "agnes-2.5-flash-thinking", matchName: "agnes-2.5-flash", wantRatio: 18},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.matchName, matchName)
			require.InDelta(t, tt.wantRatio, ratio, 1e-9)
		})
	}
}

func TestGPT55ParenthesizedControlVariantsUseBasePricing(t *testing.T) {
	resetPricingMapsForTest(t)
	modelRatioMap.Set("gpt-5.5", 2.5)
	completionRatioMap.Set("gpt-5.5", 6)
	cacheRatioMap.Set("gpt-5.5", 0.1)

	for _, name := range []string{
		"gpt-5.5(high)",
		"gpt-5.5(low)",
		"gpt-5.5(medium)",
		"gpt-5.5(xhigh)",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, "gpt-5.5", FormatMatchingModelName(name))

			ratio, ok, matchName := GetConfiguredModelRatio(name)
			require.True(t, ok)
			require.Equal(t, "gpt-5.5", matchName)
			require.InDelta(t, 2.5, ratio, 1e-9)
			require.InDelta(t, 6.0, GetCompletionRatio(name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(name)
			require.True(t, hasCacheRatio)
			require.InDelta(t, 0.1, cacheRatio, 1e-9)
		})
	}
}

func TestVariantPricingDoesNotCrossCapabilityOrSearchBoundary(t *testing.T) {
	resetPricingMapsForTest(t)
	modelRatioMap.AddAll(map[string]float64{
		"gpt-5.2":              1,
		"deepseek-v4-pro":      2,
		"gemini-3-pro-preview": 3,
		"grok-4.5":             4,
		"qwen3.5":              5,
	})

	for _, name := range []string{
		"gpt-5.2-pro",
		"deepseek-v4-pro-search",
		"deepseek-v4-pro-search-nothinking",
		"gemini-3-pro-image",
		"grok-4-5-search",
		"grok-4.5-image",
		"qwen3.5-search",
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, _ := GetConfiguredModelRatio(name)
			require.False(t, ok)
		})
	}
}

func TestDeepSeekV4OfficialVersionAliasesUseBasePricing(t *testing.T) {
	resetPricingMapsForTest(t)
	modelRatioMap.AddAll(map[string]float64{
		"deepseek-v4-flash": 0.07,
		"deepseek-v4-pro":   0.2175,
	})
	completionRatioMap.AddAll(map[string]float64{
		"deepseek-v4-flash": 2,
		"deepseek-v4-pro":   2,
	})
	cacheRatioMap.AddAll(map[string]float64{
		"deepseek-v4-flash": 0.0028 / 0.14,
		"deepseek-v4-pro":   0.003625 / 0.435,
	})

	cases := []struct {
		name       string
		matchName  string
		modelRatio float64
		cacheRatio float64
	}{
		{name: "deepseek-v4-flash-0731", matchName: "deepseek-v4-flash", modelRatio: 0.07, cacheRatio: 0.0028 / 0.14},
		{name: "DeepSeek-V4-Flash-0731", matchName: "deepseek-v4-flash", modelRatio: 0.07, cacheRatio: 0.0028 / 0.14},
		{name: "deepseek-ai/DeepSeek-V4-Pro-0813", matchName: "deepseek-v4-pro", modelRatio: 0.2175, cacheRatio: 0.003625 / 0.435},
		{name: "deepseek-v4-flash-0731-nothinking", matchName: "deepseek-v4-flash", modelRatio: 0.07, cacheRatio: 0.0028 / 0.14},
		{name: "deepseek-v4-pro-0813-nothinking", matchName: "deepseek-v4-pro", modelRatio: 0.2175, cacheRatio: 0.003625 / 0.435},
		{name: "deepseek-v4-pro-thinking", matchName: "deepseek-v4-pro", modelRatio: 0.2175, cacheRatio: 0.003625 / 0.435},
		{name: "deepseek-v4-pro-nothinking", matchName: "deepseek-v4-pro", modelRatio: 0.2175, cacheRatio: 0.003625 / 0.435},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.matchName, matchName)
			require.InDelta(t, tt.modelRatio, ratio, 1e-9)
			require.InDelta(t, 2.0, GetCompletionRatio(tt.name), 1e-9)
			cacheRatio, hasCacheRatio := GetCacheRatio(tt.name)
			require.True(t, hasCacheRatio)
			require.InDelta(t, tt.cacheRatio, cacheRatio, 1e-9)
		})
	}

	for _, name := range []string{
		"deepseek-v4-pro-search",
		"deepseek-v4-pro-search-nothinking",
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, _ := GetConfiguredModelRatio(name)
			require.False(t, ok)
		})
	}
}

func TestModelVariantNormalizationRespectsTokenBoundaries(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "gpt-4-1106-preview", want: "gpt-4-1106-preview"},
		{name: "gpt-4-1-2025-04-14", want: "gpt-4.1-2025-04-14"},
		{name: "grok-4-1-fast-non-reasoning", want: "grok-4.1-fast-non-reasoning"},
		{name: "mistral-3-large", want: "mistral-3.large"},
		{name: "step-3-5-flash", want: "step-3.5-flash"},
		{name: "qwen3-7-max", want: "qwen3.7-max"},
		{name: "qwen3-7-plus", want: "qwen3.7-plus"},
		{name: "doubao-seed-1-6-thinking", want: "doubao-seed-1.6-thinking"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch {
			case strings.HasPrefix(tt.name, "gpt-"):
				require.Equal(t, tt.want, normalizeGPTModelVariant(tt.name))
			case strings.HasPrefix(tt.name, "grok-"):
				require.Equal(t, tt.want, normalizeGrokModelVariant(tt.name))
			case strings.HasPrefix(tt.name, "mistral-"):
				require.Equal(t, tt.want, normalizeMistralModelVariant(tt.name))
			case strings.HasPrefix(tt.name, "step-"):
				require.Equal(t, tt.want, normalizeStepModelVariant(tt.name))
			case strings.HasPrefix(tt.name, "qwen"):
				require.Equal(t, tt.want, normalizeQwenModelVariant(tt.name))
			default:
				require.Equal(t, tt.want, normalizeDoubaoModelVariant(tt.name))
			}
		})
	}

	publicCases := []struct {
		name string
		want string
	}{
		{name: "llama-3-8b-instruct-free", want: "llama3-8b-instruct"},
		{name: "mistral-3-large", want: "mistral-3.large"},
		{name: "step-3-5-flash", want: "step-3.5-flash"},
		{name: "qwen3-7-max", want: "qwen3.7-max"},
		{name: "qwen3-7-plus", want: "qwen3.7-plus"},
		{name: "doubao-seed-1-6-thinking", want: "doubao-seed-1.6-thinking"},
	}
	for _, tt := range publicCases {
		t.Run("public/"+tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, FormatMatchingModelName(tt.name))
		})
	}
}

func TestGrokAliasesAndQualifiedProviderNamesUseOfficialBasePricing(t *testing.T) {
	resetPricingMapsForTest(t)
	modelRatioMap.AddAll(map[string]float64{
		"grok-4.3":       0.625,
		"grok-4.5":       1,
		"grok-4.20":      0.625,
		"grok-build-0.1": 0.5,
	})
	completionRatioMap.AddAll(map[string]float64{
		"grok-4.3":       2,
		"grok-4.5":       3,
		"grok-4.20":      2,
		"grok-build-0.1": 2,
	})
	cacheRatioMap.AddAll(map[string]float64{
		"grok-4.3":       0.16,
		"grok-4.5":       0.15,
		"grok-4.20":      0.16,
		"grok-build-0.1": 0.2,
	})

	cases := []struct {
		name       string
		matchName  string
		ratio      float64
		completion float64
		cache      float64
	}{
		{name: "grok-build-latest", matchName: "grok-4.5", ratio: 1, completion: 3, cache: 0.15},
		{name: "grok-latest", matchName: "grok-4.3", ratio: 0.625, completion: 2, cache: 0.16},
		{name: "xai/grok-4.20-reasoning", matchName: "grok-4.20", ratio: 0.625, completion: 2, cache: 0.16},
		{name: "x-ai/grok-build-0.1", matchName: "grok-build-0.1", ratio: 0.5, completion: 2, cache: 0.2},
		{name: "grok/grok-4.3-latest", matchName: "grok-4.3", ratio: 0.625, completion: 2, cache: 0.16},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.matchName, matchName)
			require.InDelta(t, tt.ratio, ratio, 1e-9)
			require.InDelta(t, tt.completion, GetCompletionRatio(tt.name), 1e-9)
			cache, hasCache := GetCacheRatio(tt.name)
			require.True(t, hasCache)
			require.InDelta(t, tt.cache, cache, 1e-9)
		})
	}

	for _, name := range []string{"grok-build", "grok"} {
		t.Run("unconfirmed/"+name, func(t *testing.T) {
			_, ok, _ := GetConfiguredModelRatio(name)
			require.False(t, ok)
		})
	}
}

func TestGrok3MiniOfficialPricingAndFastAlias(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	for _, name := range []string{
		"grok-3-mini",
		"grok-3-mini-latest",
		"grok-3-mini-beta",
		"grok-3-mini-fast",
		"grok-3-mini-fast-latest",
		"grok-3-mini-fast-beta",
	} {
		t.Run(name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(name)
			require.True(t, ok)
			require.Equal(t, name, matchName)
			require.InDelta(t, 0.15, ratio, 1e-9) // $0.30 / 1M input tokens
			require.InDelta(t, 5.0/3.0, GetCompletionRatio(name), 1e-9)
			cache, hasCache := GetCacheRatio(name)
			require.True(t, hasCache)
			require.InDelta(t, 0.25, cache, 1e-9) // $0.075 / $0.30 cached input
		})
	}
}

func TestGemini367UseOfficialStandardPriceInsteadOfPromotion(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	for _, name := range []string{"gemini-3.6-flash", "gemini-3.7-flash"} {
		t.Run(name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(name)
			require.True(t, ok)
			require.Equal(t, name, matchName)
			require.InDelta(t, 0.75, ratio, 1e-9)                 // $1.50 / 1M standard input price
			require.InDelta(t, 5, GetCompletionRatio(name), 1e-9) // $7.50 / $1.50
			cache, hasCache := GetCacheRatio(name)
			require.True(t, hasCache)
			require.InDelta(t, 0.1, cache, 1e-9) // $0.15 / $1.50
		})
	}
}

func TestGeminiPricingControlAliasesUseOfficialBasePricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	cases := map[string]string{
		"gemini-3.6-flash-high":      "gemini-3.6-flash",
		"gemini-3.6-flash-low":       "gemini-3.6-flash",
		"gemini-3.6-flash-medium":    "gemini-3.6-flash",
		"gemini-3.6-flash-tiered":    "gemini-3.6-flash",
		"gemini-3.7-flash-high":      "gemini-3.7-flash",
		"gemini-3.7-flash-low":       "gemini-3.7-flash",
		"gemini-3.7-flash-medium":    "gemini-3.7-flash",
		"gemini-3.7-flash-tiered":    "gemini-3.7-flash",
		"gemini-3.5-flash-extra-low": "gemini-3.5-flash",
		"gemini-3.5-flash-high":      "gemini-3.5-flash",
		"gemini-3.1-pro-high":        "gemini-3.1-pro",
		"gemini-3.1-pro-preview-low": "gemini-3.1-pro-preview",
		"gemini-3-pro-high":          "gemini-3-pro",
		"gemini-3-pro-low":           "gemini-3-pro",
	}
	for name, wantMatch := range cases {
		t.Run(name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(name)
			require.True(t, ok)
			require.Equal(t, wantMatch, matchName)
			require.Greater(t, ratio, 0.0)
			require.Greater(t, GetCompletionRatio(name), 0.0)
		})
	}
}

func TestClaudeDottedVariantsUseClosestConfiguredPricing(t *testing.T) {
	resetPricingMapsForTest(t)
	modelRatioMap.AddAll(map[string]float64{
		"claude-haiku-4-5":           0.5,
		"claude-haiku-4-5-thinking":  0.5,
		"claude-opus-4-5":            2.5,
		"claude-opus-4-5-thinking":   2.5,
		"claude-opus-4-6":            2.5,
		"claude-opus-4-6-thinking":   2.5,
		"claude-opus-4-7":            2.5,
		"claude-opus-4-7-thinking":   2.5,
		"claude-opus-4-8":            2.5,
		"claude-opus-4-8-thinking":   2.5,
		"claude-sonnet-4-5":          1.5,
		"claude-sonnet-4-5-thinking": 1.5,
		"claude-sonnet-4-6":          1.5,
		"claude-sonnet-4-6-thinking": 1.5,
	})
	completionRatioMap.AddAll(map[string]float64{
		"claude-haiku-4-5":           5,
		"claude-haiku-4-5-thinking":  5,
		"claude-opus-4-5":            5,
		"claude-opus-4-5-thinking":   5,
		"claude-opus-4-6":            5,
		"claude-opus-4-6-thinking":   5,
		"claude-opus-4-7":            5,
		"claude-opus-4-7-thinking":   5,
		"claude-opus-4-8":            5,
		"claude-opus-4-8-thinking":   5,
		"claude-sonnet-4-5":          5,
		"claude-sonnet-4-5-thinking": 5,
		"claude-sonnet-4-6":          5,
		"claude-sonnet-4-6-thinking": 5,
	})
	cacheRatioMap.AddAll(map[string]float64{
		"claude-haiku-4-5":           0.1,
		"claude-haiku-4-5-thinking":  0.1,
		"claude-opus-4-5":            0.1,
		"claude-opus-4-5-thinking":   0.1,
		"claude-opus-4-6":            0.1,
		"claude-opus-4-6-thinking":   0.1,
		"claude-opus-4-7":            0.1,
		"claude-opus-4-7-thinking":   0.1,
		"claude-opus-4-8":            0.1,
		"claude-opus-4-8-thinking":   0.1,
		"claude-sonnet-4-5":          0.1,
		"claude-sonnet-4-5-thinking": 0.1,
		"claude-sonnet-4-6":          0.1,
		"claude-sonnet-4-6-thinking": 0.1,
	})
	createCacheRatioMap.AddAll(map[string]float64{
		"claude-haiku-4-5":           1.25,
		"claude-haiku-4-5-thinking":  1.25,
		"claude-opus-4-5":            1.25,
		"claude-opus-4-5-thinking":   1.25,
		"claude-opus-4-6":            1.25,
		"claude-opus-4-6-thinking":   1.25,
		"claude-opus-4-7":            1.25,
		"claude-opus-4-7-thinking":   1.25,
		"claude-opus-4-8":            1.25,
		"claude-opus-4-8-thinking":   1.25,
		"claude-sonnet-4-5":          1.25,
		"claude-sonnet-4-5-thinking": 1.25,
		"claude-sonnet-4-6":          1.25,
		"claude-sonnet-4-6-thinking": 1.25,
	})

	tests := []struct {
		name       string
		normalized string
		matchName  string
		ratio      float64
	}{
		{name: "claude-haiku-4.5", normalized: "claude-haiku-4-5", matchName: "claude-haiku-4-5", ratio: 0.5},
		{name: "claude-haiku-4.5-thinking", normalized: "claude-haiku-4-5", matchName: "claude-haiku-4-5-thinking", ratio: 0.5},
		{name: "claude-opus-4.5", normalized: "claude-opus-4-5", matchName: "claude-opus-4-5", ratio: 2.5},
		{name: "claude-opus-4.5-thinking", normalized: "claude-opus-4-5", matchName: "claude-opus-4-5-thinking", ratio: 2.5},
		{name: "claude-opus-4.6", normalized: "claude-opus-4-6", matchName: "claude-opus-4-6", ratio: 2.5},
		{name: "claude-opus-4.6-thinking", normalized: "claude-opus-4-6", matchName: "claude-opus-4-6-thinking", ratio: 2.5},
		{name: "claude-opus-4.7", normalized: "claude-opus-4-7", matchName: "claude-opus-4-7", ratio: 2.5},
		{name: "claude-opus-4.7-thinking", normalized: "claude-opus-4-7", matchName: "claude-opus-4-7-thinking", ratio: 2.5},
		{name: "claude-opus-4.8", normalized: "claude-opus-4-8", matchName: "claude-opus-4-8", ratio: 2.5},
		{name: "claude-opus-4.8-thinking", normalized: "claude-opus-4-8", matchName: "claude-opus-4-8-thinking", ratio: 2.5},
		{name: "claude-sonnet-4.5", normalized: "claude-sonnet-4-5", matchName: "claude-sonnet-4-5", ratio: 1.5},
		{name: "claude-sonnet-4.5-thinking", normalized: "claude-sonnet-4-5", matchName: "claude-sonnet-4-5-thinking", ratio: 1.5},
		{name: "claude-sonnet-4.6", normalized: "claude-sonnet-4-6", matchName: "claude-sonnet-4-6", ratio: 1.5},
		{name: "claude-sonnet-4.6-thinking", normalized: "claude-sonnet-4-6", matchName: "claude-sonnet-4-6-thinking", ratio: 1.5},
		{name: "claude-sonnet-4-8", normalized: "claude-sonnet-4-6", matchName: "claude-sonnet-4-6", ratio: 1.5},
		{name: "claude-sonnet-4.8", normalized: "claude-sonnet-4-6", matchName: "claude-sonnet-4-6", ratio: 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.normalized, FormatMatchingModelName(tt.name))

			modelRatio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.matchName, matchName)
			require.InDelta(t, tt.ratio, modelRatio, 1e-9)
			require.InDelta(t, 5.0, GetCompletionRatio(tt.name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(tt.name)
			require.True(t, hasCacheRatio)
			require.InDelta(t, 0.1, cacheRatio, 1e-9)

			createCacheRatio, hasCreateCacheRatio := GetCreateCacheRatio(tt.name)
			require.True(t, hasCreateCacheRatio)
			require.InDelta(t, 1.25, createCacheRatio, 1e-9)
		})
	}
}

func TestNewModelDefaultPricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	modelCases := []struct {
		name             string
		modelRatio       float64
		completionRatio  float64
		cacheRatio       *float64
		createCacheRatio *float64
	}{
		{name: "claude-fable-5", modelRatio: 5, completionRatio: 5, cacheRatio: floatPtr(0.1), createCacheRatio: floatPtr(1.25)},
		{name: "claude-haiku-4-5", modelRatio: 0.5, completionRatio: 5, cacheRatio: floatPtr(0.1), createCacheRatio: floatPtr(1.25)},
		{name: "claude-sonnet-4-6", modelRatio: 1.5, completionRatio: 5, cacheRatio: floatPtr(0.1), createCacheRatio: floatPtr(1.25)},
		{name: "claude-opus-5", modelRatio: 2.5, completionRatio: 5, cacheRatio: floatPtr(0.1), createCacheRatio: floatPtr(1.25)},
		{name: "claude-sonnet-5", modelRatio: 1, completionRatio: 5, cacheRatio: floatPtr(0.1), createCacheRatio: floatPtr(1.25)},
		{name: "GPT-5.5", modelRatio: 2.5, completionRatio: 6, cacheRatio: floatPtr(0.1)},
		{name: "deepseek-flash", modelRatio: 0.15 / 2, completionRatio: 4, cacheRatio: floatPtr(0.003 / 0.15)},
		{name: "deepseek-ai/deepseek-flash", modelRatio: 0.15 / 2, completionRatio: 4, cacheRatio: floatPtr(0.003 / 0.15)},
		{name: "deepseek-v4.1-flash", modelRatio: 0.15 / 2, completionRatio: 4, cacheRatio: floatPtr(0.003 / 0.15)},
		{name: "deepseek-v4.1-flash-0910", modelRatio: 0.15 / 2, completionRatio: 4, cacheRatio: floatPtr(0.003 / 0.15)},
		{name: "deepseek-v4-flash", modelRatio: 0.15 / 2, completionRatio: 4, cacheRatio: floatPtr(0.003 / 0.15)},
		{name: "deepseek-ai/deepseek-v4-flash", modelRatio: 0.15 / 2, completionRatio: 4, cacheRatio: floatPtr(0.003 / 0.15)},
		{name: "deepseek-v4-pro", modelRatio: 0.2175, completionRatio: 2, cacheRatio: floatPtr(0.003625 / 0.435)},
		{name: "sensenova-6.8-flash-lite", modelRatio: 0, completionRatio: 0, cacheRatio: floatPtr(0)},
		{name: "sensenova-u1-fast", modelRatio: 0, completionRatio: 0, cacheRatio: floatPtr(0)},
		{name: "agnes-2.0-flash", modelRatio: 0, completionRatio: 0, cacheRatio: floatPtr(0)},
		{name: "agnes-2.5-flash", modelRatio: 0, completionRatio: 0, cacheRatio: floatPtr(0)},
		{name: "agnes-3.0-flash", modelRatio: 0, completionRatio: 0, cacheRatio: floatPtr(0)},
		{name: "agnes-2.5-pro", modelRatio: 0.225, completionRatio: 2, cacheRatio: floatPtr(0.1)},
		{name: "agnes-2.5-pro-alpha", modelRatio: 0.225, completionRatio: 2, cacheRatio: floatPtr(0.1)},
		{name: "gemini-2.5-flash", modelRatio: 0.15, completionRatio: 2.5 / 0.3, cacheRatio: floatPtr(0.1)},
		{name: "gemini-2.5-pro", modelRatio: 0.625, completionRatio: 8, cacheRatio: floatPtr(0.1)},
		{name: "gemini-3-flash", modelRatio: 0.25, completionRatio: 6, cacheRatio: floatPtr(0.1)},
		{name: "gemini-3.1-pro", modelRatio: 1, completionRatio: 6, cacheRatio: floatPtr(0.1)},
		{name: "gpt-5.4-openai-compact", modelRatio: 0.625, completionRatio: 6, cacheRatio: floatPtr(0.104)},
		{name: "codex-auto-review", modelRatio: 2.5, completionRatio: 6, cacheRatio: floatPtr(0.1)},
	}

	for _, tt := range modelCases {
		t.Run(tt.name, func(t *testing.T) {
			modelRatio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.name, matchName)
			require.InDelta(t, tt.modelRatio, modelRatio, 1e-9)
			require.InDelta(t, tt.completionRatio, GetCompletionRatio(tt.name), 1e-9)

			cacheRatio, hasCacheRatio := GetCacheRatio(tt.name)
			require.Equal(t, tt.cacheRatio != nil, hasCacheRatio)
			if tt.cacheRatio != nil {
				require.InDelta(t, *tt.cacheRatio, cacheRatio, 1e-9)
			}

			createCacheRatio, hasCreateCacheRatio := GetCreateCacheRatio(tt.name)
			require.Equal(t, tt.createCacheRatio != nil, hasCreateCacheRatio)
			if tt.createCacheRatio != nil {
				require.InDelta(t, *tt.createCacheRatio, createCacheRatio, 1e-9)
			}
		})
	}

	for _, name := range []string{
		"agnes-image-2.0-flash",
		"agnes-image-2.1-flash",
		"agnes-image-2.5-flash",
		"agnes-video-v2.0",
		"agnes-video-2.5-flash",
	} {
		t.Run(name, func(t *testing.T) {
			modelPrice, ok := GetModelPrice(name, false)
			require.True(t, ok)
			require.Zero(t, modelPrice)
		})
	}
}

func TestAgnes30FlashUsesCurrentOfficialFreePricing(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	ratio, configured, matchName := GetConfiguredModelRatio("agnes-3.0-flash")
	require.True(t, configured)
	require.Equal(t, "agnes-3.0-flash", matchName)
	require.Zero(t, ratio)
	require.Zero(t, GetCompletionRatio("agnes-3.0-flash"))

	cacheRatio, hasCacheRatio := GetCacheRatio("agnes-3.0-flash")
	require.True(t, hasCacheRatio)
	require.Zero(t, cacheRatio)

	// Agnes publishes no cache-creation price for this model; do not infer one.
	_, hasCreateCacheRatio := GetCreateCacheRatio("agnes-3.0-flash")
	require.False(t, hasCreateCacheRatio)
}

func TestSenseNova67UnlistedModelFailsClosed(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	_, configured, _ := GetConfiguredModelRatio("sensenova-6.7-flash-lite")
	require.False(t, configured)
	_, hasCacheRatio := GetCacheRatio("sensenova-6.7-flash-lite")
	require.False(t, hasCacheRatio)
	_, hasCreateCacheRatio := GetCreateCacheRatio("sensenova-6.7-flash-lite")
	require.False(t, hasCreateCacheRatio)
}

func TestGetCompletionRatioGPT55DatedVariant(t *testing.T) {
	got := GetCompletionRatio("gpt-5.5-2026-04-24")

	require.InDelta(t, 6.0, got, 1e-9)
}

func TestCompactModelPricingInheritsBaseModelSettings(t *testing.T) {
	resetPricingMapsForTest(t)

	modelRatioMap.Set("compact-base", 2.5)
	completionRatioMap.Set("compact-base", 4)
	cacheRatioMap.Set("compact-base", 0.25)
	createCacheRatioMap.Set("compact-base", 1.5)
	modelPriceMap.Set("compact-base", 0.02)

	ratio, ok, matchName := GetConfiguredModelRatio("compact-base-openai-compact")
	require.True(t, ok)
	require.Equal(t, "compact-base", matchName)
	require.InDelta(t, 2.5, ratio, 1e-9)

	completionInfo := GetCompletionRatioInfo("compact-base-openai-compact")
	require.False(t, completionInfo.Locked)
	require.InDelta(t, 4.0, completionInfo.Ratio, 1e-9)
	require.InDelta(t, 4.0, GetCompletionRatio("compact-base-openai-compact"), 1e-9)

	cacheRatio, hasCacheRatio := GetCacheRatio("compact-base-openai-compact")
	require.True(t, hasCacheRatio)
	require.InDelta(t, 0.25, cacheRatio, 1e-9)

	createCacheRatio, hasCreateCacheRatio := GetCreateCacheRatio("compact-base-openai-compact")
	require.True(t, hasCreateCacheRatio)
	require.InDelta(t, 1.5, createCacheRatio, 1e-9)

	modelPrice, hasModelPrice := GetModelPrice("compact-base-openai-compact", false)
	require.True(t, hasModelPrice)
	require.InDelta(t, 0.02, modelPrice, 1e-9)
}

func TestCompactModelPricingPrefersExplicitAndWildcardSettings(t *testing.T) {
	resetPricingMapsForTest(t)

	modelRatioMap.Set("compact-base", 2)
	modelRatioMap.Set(CompactWildcardModelKey, 3)
	modelRatioMap.Set("compact-base-openai-compact", 4)

	ratio, ok, matchName := GetConfiguredModelRatio("compact-base-openai-compact")
	require.True(t, ok)
	require.Equal(t, "compact-base-openai-compact", matchName)
	require.InDelta(t, 4.0, ratio, 1e-9)

	modelRatioMap.Clear()
	modelRatioMap.Set("compact-base", 2)
	modelRatioMap.Set(CompactWildcardModelKey, 3)

	ratio, ok, matchName = GetConfiguredModelRatio("compact-base-openai-compact")
	require.True(t, ok)
	require.Equal(t, CompactWildcardModelKey, matchName)
	require.InDelta(t, 3.0, ratio, 1e-9)
}

func TestCurrentOpenAIImageAndSoraPrices(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	for _, tt := range []struct {
		name  string
		price float64
	}{
		{name: "sora-2", price: 0.1},
		{name: "sora-2-pro", price: 0.3},
	} {
		price, ok := GetModelPrice(tt.name, false)
		require.True(t, ok)
		require.InDelta(t, tt.price, price, 1e-9)
	}

	ratio, ok, matchName := GetConfiguredModelRatio("chatgpt-image-latest")
	require.True(t, ok)
	require.Equal(t, "chatgpt-image-latest", matchName)
	require.InDelta(t, 2.5, ratio, 1e-9)
	require.InDelta(t, 2, GetCompletionRatio("chatgpt-image-latest"), 1e-9)
	cacheRatio, ok := GetCacheRatio("chatgpt-image-latest")
	require.True(t, ok)
	require.InDelta(t, 0.25, cacheRatio, 1e-9)
	imageRatio, ok := GetImageRatio("chatgpt-image-latest")
	require.True(t, ok)
	require.InDelta(t, 1.6, imageRatio, 1e-9)
}

func resetPricingMapsForTest(t *testing.T) {
	t.Helper()
	modelPriceMap.Clear()
	modelRatioMap.Clear()
	completionRatioMap.Clear()
	cacheRatioMap.Clear()
	createCacheRatioMap.Clear()
	imageRatioMap.Clear()
	audioRatioMap.Clear()
	audioCompletionRatioMap.Clear()
	t.Cleanup(func() {
		modelPriceMap.Clear()
		modelRatioMap.Clear()
		completionRatioMap.Clear()
		cacheRatioMap.Clear()
		createCacheRatioMap.Clear()
		imageRatioMap.Clear()
		audioRatioMap.Clear()
		audioCompletionRatioMap.Clear()
		InitRatioSettings()
	})
}

func floatPtr(v float64) *float64 {
	return &v
}
