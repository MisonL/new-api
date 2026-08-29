package billing_setting

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/require"
)

func TestValidateBillingModeJSON(t *testing.T) {
	require.NoError(t, ValidateBillingModeJSON(`{"gpt-5":"tiered_expr","gpt-4.1":"ratio"}`))
	require.Error(t, ValidateBillingModeJSON(`{"gpt-5":"unknown"}`))
}

func TestValidateBillingExprJSON(t *testing.T) {
	require.NoError(t, ValidateBillingExprJSON(`{"gpt-5":"tier(\"base\", p * 2 + c * 10)"}`))
	require.NoError(t, ValidateBillingExprJSON(`{"deepseek-v4-flash":`+jsonString(DeepSeekV4FlashBillingExpr)+`,"deepseek-v4-pro":`+jsonString(DeepSeekV4ProBillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"grok-4.5":`+jsonString(Grok45BillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"grok-4.3":`+jsonString(Grok43BillingExpr)+`,"grok-4.20":`+jsonString(Grok420BillingExpr)+`,"grok-4.6":`+jsonString(Grok46BillingExpr)+`,"grok-build-0.1":`+jsonString(GrokBuild01BillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"gemini-2.5-pro":`+jsonString(Gemini25ProBillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"gemini-3.1-pro-preview":"len <= 200000 ? tier(\"standard\", p * 2 + c * 12 + cr * 0.2) : tier(\"long_context\", p * 4 + c * 18 + cr * 0.4)"}`))
	require.NoError(t, ValidateBillingExprJSON(`{"minimax-m3":"len <= 512000 ? tier(\"standard\", p * 0.3 + c * 1.2 + cr * 0.06) : tier(\"long_context\", p * 0.6 + c * 2.4 + cr * 0.12)"}`))
	require.NoError(t, ValidateBillingExprJSON(`{"minimax-m3":`+jsonString(MiniMaxM3BillingExpr)+`}`))
	require.Error(t, ValidateBillingExprJSON(`{"gpt-5":"invalid +-+ expr"}`))
	require.Error(t, ValidateBillingExprJSON(`{"gpt-5":""}`))
}

func TestMiniMaxM3OfficialPricingSelectsContextAndServiceTier(t *testing.T) {
	tests := []struct {
		name        string
		len         int64
		serviceTier string
		matchedTier string
		want        float64
	}{
		{name: "standard short", len: 512000, matchedTier: "standard", want: 1560},
		{name: "standard long", len: 512001, matchedTier: "long_context", want: 3120},
		{name: "priority short", len: 512000, serviceTier: "priority", matchedTier: "priority_standard", want: 2340},
		{name: "priority long", len: 512001, serviceTier: "priority", matchedTier: "priority_long_context", want: 4680},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := billingexpr.RequestInput{}
			if tt.serviceTier != "" {
				request.Body = []byte(`{"service_tier":"` + tt.serviceTier + `"}`)
			}
			cost, trace, err := billingexpr.RunExprWithRequest(MiniMaxM3BillingExpr, billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000, Len: float64(tt.len)}, request)
			require.NoError(t, err)
			require.Equal(t, tt.matchedTier, trace.MatchedTier)
			require.InDelta(t, tt.want, cost, 1e-9)
		})
	}
}

func TestMiniMaxM3OfficialAliasesUseTieredBilling(t *testing.T) {
	for _, name := range []string{
		"minimax-m3",
		"MiniMax-M3-thinking",
		"minimaxai/minimax-m3",
		"minimaxai/MiniMax-M3-free",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, BillingModeTieredExpr, GetBillingMode(name))
			expr, ok := GetBillingExpr(name)
			require.True(t, ok)
			require.Equal(t, MiniMaxM3BillingExpr, expr)
		})
	}
}

func TestMiniMaxM3LegacyExpressionUpgradesToPriorityPricing(t *testing.T) {
	originalExpr := billingSetting.BillingExpr
	billingSetting.BillingExpr = map[string]string{"minimax-m3": MiniMaxM3LegacyBillingExpr}
	t.Cleanup(func() { billingSetting.BillingExpr = originalExpr })

	expr, ok := GetBillingExpr("minimaxai/minimax-m3")
	require.True(t, ok)
	require.Equal(t, MiniMaxM3BillingExpr, expr)
}

func TestOfficialLongContextExpressionsSelectPrices(t *testing.T) {
	gemini := `len <= 200000 ? tier("standard", p * 2 + c * 12 + cr * 0.2) : tier("long_context", p * 4 + c * 18 + cr * 0.4)`
	minimax := `len <= 512000 ? tier("standard", p * 0.3 + c * 1.2 + cr * 0.06) : tier("long_context", p * 0.6 + c * 2.4 + cr * 0.12)`

	standard, trace, err := billingexpr.RunExpr(gemini, billingexpr.TokenParams{P: 1000, C: 1000, Len: 200000, CR: 1000})
	require.NoError(t, err)
	require.Equal(t, "standard", trace.MatchedTier)
	require.InDelta(t, 14200, standard, 1e-9)

	long, trace, err := billingexpr.RunExpr(gemini, billingexpr.TokenParams{P: 1000, C: 1000, Len: 200001, CR: 1000})
	require.NoError(t, err)
	require.Equal(t, "long_context", trace.MatchedTier)
	require.InDelta(t, 22400, long, 1e-9)

	standard, trace, err = billingexpr.RunExpr(minimax, billingexpr.TokenParams{P: 1000, C: 1000, Len: 512000, CR: 1000})
	require.NoError(t, err)
	require.Equal(t, "standard", trace.MatchedTier)
	require.InDelta(t, 1560, standard, 1e-9)

	long, trace, err = billingexpr.RunExpr(minimax, billingexpr.TokenParams{P: 1000, C: 1000, Len: 512001, CR: 1000})
	require.NoError(t, err)
	require.Equal(t, "long_context", trace.MatchedTier)
	require.InDelta(t, 3120, long, 1e-9)
}

func TestGeminiOfficialExpressionsSelectPublishedPrices(t *testing.T) {
	tests := []struct {
		name   string
		expr   string
		params billingexpr.TokenParams
		want   float64
	}{
		{name: "3.7 flash standard price", expr: Gemini37FlashBillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000}, want: 9150},
		{name: "3.6 flash standard price", expr: Gemini36FlashBillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000}, want: 9150},
		{name: "3.5 flash", expr: Gemini35FlashBillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000}, want: 10650},
		{name: "3.5 flash lite", expr: Gemini35FlashLiteBillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000}, want: 2830},
		{name: "3.1 flash lite audio", expr: Gemini31FlashLiteBillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000, AI: 1000}, want: 2275},
		{name: "omni flash image output", expr: GeminiOmniFlashBillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000, ImgO: 1000}, want: 28000},
		{name: "live", expr: Gemini31FlashLiveBillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000, Img: 1000, AI: 1000, AO: 1000}, want: 21250},
		{name: "live translate", expr: Gemini35LiveTranslateBillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000}, want: 24500},
		{name: "tts", expr: Gemini31FlashTTSBillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000}, want: 21000},
		{name: "robotics er 1.6", expr: GeminiRoboticsER16BillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000, AI: 1000}, want: 8000},
		{name: "robotics er 2", expr: GeminiRoboticsER2BillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000}, want: 12200},
		{name: "robotics er 2 streaming", expr: GeminiRoboticsER2StreamingBillingExpr, params: billingexpr.TokenParams{P: 1000, C: 1000}, want: 12000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, trace, err := billingexpr.RunExpr(tt.expr, tt.params)
			require.NoError(t, err)
			require.Equal(t, "standard", trace.MatchedTier)
			require.InDelta(t, tt.want, cost, 1e-9)
		})
	}
}

func TestGemini31ProPreviewExpressionUsesContextBoundary(t *testing.T) {
	standard, trace, err := billingexpr.RunExpr(Gemini31ProPreviewBillingExpr, billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000, Len: 200000})
	require.NoError(t, err)
	require.Equal(t, "standard", trace.MatchedTier)
	require.InDelta(t, 14200, standard, 1e-9)

	long, trace, err := billingexpr.RunExpr(Gemini31ProPreviewBillingExpr, billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000, Len: 200001})
	require.NoError(t, err)
	require.Equal(t, "long_context", trace.MatchedTier)
	require.InDelta(t, 22400, long, 1e-9)
}

func TestGemini25ProOfficialExpressionUsesContextBoundary(t *testing.T) {
	standard, trace, err := billingexpr.RunExpr(Gemini25ProBillingExpr, billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000, Len: 200000})
	require.NoError(t, err)
	require.Equal(t, "standard", trace.MatchedTier)
	require.InDelta(t, 11375, standard, 1e-9)

	long, trace, err := billingexpr.RunExpr(Gemini25ProBillingExpr, billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000, Len: 200001})
	require.NoError(t, err)
	require.Equal(t, "long_context", trace.MatchedTier)
	require.InDelta(t, 17750, long, 1e-9)
}

func TestGemini25ProAliasesUseTieredBilling(t *testing.T) {
	for _, name := range []string{
		"gemini-2.5-pro",
		"gemini-2.5-pro-exp-03-25",
		"gemini-2.5-pro-preview-03-25",
		"gemini-2.5-pro-thinking-512",
		"google/gemini-2.5-pro-thinking-512",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, BillingModeTieredExpr, GetBillingMode(name))
			expr, ok := GetBillingExpr(name)
			require.True(t, ok)
			require.Equal(t, Gemini25ProBillingExpr, expr)
		})
	}
}

func TestGrok45OfficialLongContextExpressionSelectsPrices(t *testing.T) {
	standard, trace, err := billingexpr.RunExpr(Grok45BillingExpr, billingexpr.TokenParams{P: 1000, C: 1000, Len: 200000, CR: 1000})
	require.NoError(t, err)
	require.Equal(t, "standard", trace.MatchedTier)
	require.InDelta(t, 8300, standard, 1e-9)

	long, trace, err := billingexpr.RunExpr(Grok45BillingExpr, billingexpr.TokenParams{P: 1000, C: 1000, Len: 200001, CR: 1000})
	require.NoError(t, err)
	require.Equal(t, "long_context", trace.MatchedTier)
	require.InDelta(t, 16600, long, 1e-9)
}

func TestGrok45LegacyExpressionUpgradesToOfficialPricing(t *testing.T) {
	originalExpr := billingSetting.BillingExpr
	billingSetting.BillingExpr = map[string]string{"grok-4.5": Grok45LegacyBillingExpr}
	t.Cleanup(func() {
		billingSetting.BillingExpr = originalExpr
	})
	expr, ok := GetBillingExpr("xai/grok-4.5-latest")
	require.True(t, ok)
	require.Equal(t, Grok45BillingExpr, expr)
}

func TestGrok45OfficialAliasesUseTieredBilling(t *testing.T) {
	for _, name := range []string{
		"grok-4.5",
		"grok-4.5-latest",
		"grok-build-latest",
		"grok/grok-4.5",
		"grok/grok-4.5-latest",
		"grok/grok-build-latest",
		"xai/grok-4.5",
		"xai/grok-4.5-latest",
		"xai/grok-build-latest",
		"x-ai/grok-4.5",
		"x-ai/grok-4.5-latest",
		"x-ai/grok-build-latest",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, BillingModeTieredExpr, GetBillingMode(name))
			expr, ok := GetBillingExpr(name)
			require.True(t, ok)
			require.Equal(t, Grok45BillingExpr, expr)
		})
	}
}

func TestGrokOfficialLongContextExpressionsSelectPrices(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		standard float64
		long     float64
	}{
		{name: "grok-4.3", expr: Grok43BillingExpr, standard: 3950, long: 7900},
		{name: "grok-4.20", expr: Grok420BillingExpr, standard: 3950, long: 7900},
		{name: "grok-4.6", expr: Grok46BillingExpr, standard: 8500, long: 17000},
		{name: "grok-build-0.1", expr: GrokBuild01BillingExpr, standard: 3200, long: 6400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			standard, trace, err := billingexpr.RunExpr(tt.expr, billingexpr.TokenParams{P: 1000, C: 1000, Len: 200000, CR: 1000})
			require.NoError(t, err)
			require.Equal(t, "standard", trace.MatchedTier)
			require.InDelta(t, tt.standard, standard, 1e-9)

			long, trace, err := billingexpr.RunExpr(tt.expr, billingexpr.TokenParams{P: 1000, C: 1000, Len: 200001, CR: 1000})
			require.NoError(t, err)
			require.Equal(t, "long_context", trace.MatchedTier)
			require.InDelta(t, tt.long, long, 1e-9)
		})
	}
}

func TestGrokOfficialVariantsUseTieredBilling(t *testing.T) {
	cases := map[string]string{
		"grok-4.3-latest":                   Grok43BillingExpr,
		"grok-latest":                       Grok43BillingExpr,
		"grok/grok-latest":                  Grok43BillingExpr,
		"xai/grok-latest":                   Grok43BillingExpr,
		"x-ai/grok-latest":                  Grok43BillingExpr,
		"xai/grok-4.20-reasoning":           Grok420BillingExpr,
		"grok/grok-4.20-multi-agent-latest": Grok420BillingExpr,
		"x-ai/grok-4.6":                     Grok46BillingExpr,
		"grok/grok-build-0.1":               GrokBuild01BillingExpr,
		"xai/grok-build-0.1":                GrokBuild01BillingExpr,
		"x-ai/grok-build-0.1":               GrokBuild01BillingExpr,
		"x-ai/grok-code-fast-1":             GrokBuild01BillingExpr,
	}
	for name, wantExpr := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, BillingModeTieredExpr, GetBillingMode(name))
			expr, ok := GetBillingExpr(name)
			require.True(t, ok)
			require.Equal(t, wantExpr, expr)
		})
	}
}

func TestDeepSeekV4PeakPricingExpressionValidates(t *testing.T) {
	require.Equal(t, BillingModeTieredExpr, GetBillingMode("deepseek-ai/deepseek-v4-flash"))
	require.Equal(t, DeepSeekV4FlashBillingExpr, func() string {
		value, ok := GetBillingExpr("deepseek-ai/deepseek-v4-flash")
		require.True(t, ok)
		return value
	}())
	require.Equal(t, DeepSeekV4ProBillingExpr, func() string {
		value, ok := GetBillingExpr("deepseek-v4-pro-nothinking")
		require.True(t, ok)
		return value
	}())
}

func TestDeepSeekV4BillingAliasesUseThePublishedExpressions(t *testing.T) {
	for _, name := range []string{
		"deepseek-v4-flash-0731",
		"DeepSeek-V4-Flash-0731",
		"deepseek-v4-flash-vision-exp",
		"deepseek-ai/deepseek-v4-flash-vision-exp",
		"deepseek-ai/DeepSeek-V4-Pro-0813",
		"deepseek-v4-pro-thinking",
		"deepseek-v4-pro-nothinking",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, BillingModeTieredExpr, GetBillingMode(name))
			expr, ok := GetBillingExpr(name)
			require.True(t, ok)
			if strings.Contains(strings.ToLower(name), "flash") {
				require.Equal(t, DeepSeekV4FlashBillingExpr, expr)
				return
			}
			require.Equal(t, DeepSeekV4ProBillingExpr, expr)
		})
	}

	for _, name := range []string{"deepseek-v4-pro-search", "deepseek-v4-pro-search-nothinking"} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, BillingModeRatio, GetBillingMode(name))
			_, ok := GetBillingExpr(name)
			require.False(t, ok)
		})
	}
}

func TestDeepSeekV4LegacyProductionExpressionUpgrades(t *testing.T) {
	billingSetting.BillingExpr = map[string]string{
		"deepseek-v4-flash": DeepSeekV4FlashCurrentBillingExpr,
		"deepseek-v4-pro":   DeepSeekV4ProCurrentBillingExpr,
	}
	t.Cleanup(func() {
		billingSetting.BillingExpr = map[string]string{
			"deepseek-v4-flash": DeepSeekV4FlashBillingExpr,
			"deepseek-v4-pro":   DeepSeekV4ProBillingExpr,
		}
	})
	flash, ok := GetBillingExpr("deepseek-ai/deepseek-v4-flash")
	require.True(t, ok)
	require.Equal(t, DeepSeekV4FlashBillingExpr, flash)
	pro, ok := GetBillingExpr("deepseek-v4-pro-nothinking")
	require.True(t, ok)
	require.Equal(t, DeepSeekV4ProBillingExpr, pro)

	billingSetting.BillingExpr = map[string]string{
		"deepseek-v4-flash-vision-exp": DeepSeekV4FlashProductionBillingExpr,
	}
	vision, ok := GetBillingExpr("deepseek-ai/deepseek-v4-flash-vision-exp")
	require.True(t, ok)
	require.Equal(t, DeepSeekV4FlashBillingExpr, vision)
}

func jsonString(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}
