package billing_setting

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/require"
)

func TestValidateBillingModeJSON(t *testing.T) {
	require.NoError(t, ValidateBillingModeJSON(`{"gpt-5":"tiered_expr","gpt-4.1":"ratio"}`))
	require.Error(t, ValidateBillingModeJSON(`{"gpt-5":"unknown"}`))
}

func TestValidateBillingExprJSON(t *testing.T) {
	require.NoError(t, ValidateBillingExprJSON(`{"gpt-5":"tier(\"base\", p * 2 + c * 10)"}`))
	require.NoError(t, ValidateBillingExprJSON(`{"deepseek-flash":`+jsonString(DeepSeekV4FlashBillingExpr)+`,"deepseek-v4.1-flash":`+jsonString(DeepSeekV4FlashBillingExpr)+`,"deepseek-v4.1-flash-0910":`+jsonString(DeepSeekV4FlashBillingExpr)+`,"deepseek-v4-flash":`+jsonString(DeepSeekV4FlashBillingExpr)+`,"deepseek-v4-pro":`+jsonString(DeepSeekV4ProBillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"grok-4.5":`+jsonString(Grok45BillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"grok-4.3":`+jsonString(Grok43BillingExpr)+`,"grok-4.20":`+jsonString(Grok420BillingExpr)+`,"grok-4.6":`+jsonString(Grok46BillingExpr)+`,"grok-build-0.1":`+jsonString(GrokBuild01BillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"gpt-6-astra":`+jsonString(GPT6AstraBillingExpr)+`,"gpt-5.6":`+jsonString(GPT56SolBillingExpr)+`,"gpt-5.6-terra":`+jsonString(GPT56TerraBillingExpr)+`,"gpt-5.6-luna":`+jsonString(GPT56LunaBillingExpr)+`,"gpt-5.5":`+jsonString(GPT55BillingExpr)+`,"gpt-5.4":`+jsonString(GPT54BillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"gpt-5.5-pro":`+jsonString(GPT55ProBillingExpr)+`,"gpt-5.4-pro":`+jsonString(GPT54ProBillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"gpt-5.6-cyber":`+jsonString(GPT56CyberBillingExpr)+`,"gpt-5.5-cyber":`+jsonString(GPT55CyberBillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"gemini-2.5-pro":`+jsonString(Gemini25ProBillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"gemini-3.1-pro-preview":"len <= 200000 ? tier(\"standard\", p * 2 + c * 12 + cr * 0.2) : tier(\"long_context\", p * 4 + c * 18 + cr * 0.4)"}`))
	require.NoError(t, ValidateBillingExprJSON(`{"minimax-m3":"len <= 512000 ? tier(\"standard\", p * 0.3 + c * 1.2 + cr * 0.06) : tier(\"long_context\", p * 0.6 + c * 2.4 + cr * 0.12)"}`))
	require.NoError(t, ValidateBillingExprJSON(`{"minimax-m3":`+jsonString(MiniMaxM3BillingExpr)+`}`))
	require.NoError(t, ValidateBillingExprJSON(`{"tencent/hunyuan-a13b-instruct":`+jsonString(HunyuanA13BInstructBillingExpr)+`}`))
	require.Error(t, ValidateBillingExprJSON(`{"gpt-5":"invalid +-+ expr"}`))
	require.Error(t, ValidateBillingExprJSON(`{"gpt-5":""}`))
}

func TestDeepSeekV41FlashAliasesUseCurrentTieredBilling(t *testing.T) {
	for _, name := range []string{"deepseek-v4.1-flash", "deepseek-v4.1-flash-0910"} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, BillingModeTieredExpr, GetBillingMode(name))
			expr, ok := GetBillingExpr(name)
			require.True(t, ok)
			require.Equal(t, DeepSeekV4FlashBillingExpr, expr)
		})
	}
}

func TestHunyuanA13BInstructOfficialPricingUsesShanghaiWindow(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	params := billingexpr.TokenParams{P: 1000, C: 2000, CR: 3000}
	cost, trace, err := billingexpr.RunExpr(HunyuanA13BInstructBillingExpr, params)
	require.NoError(t, err)

	if hour := time.Now().In(loc).Hour(); hour >= 9 && hour < 18 {
		require.Equal(t, "peak", trace.MatchedTier)
		require.InDelta(t, 1000*0.136986301369863+2000*0.547945205479452, cost, 1e-9)
		return
	}
	require.Equal(t, "off_peak", trace.MatchedTier)
	require.InDelta(t, 1000*0.10958904109589+2000*0.438356164383562, cost, 1e-9)
}

func TestHunyuanA13BInstructOfficialAliasesUseTieredBilling(t *testing.T) {
	for _, name := range []string{
		"tencent/Hunyuan-A13B-Instruct",
		"tencent/hunyuan-a13b-instruct",
		"hunyuan-a13b-instruct",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, BillingModeTieredExpr, GetBillingMode(name))
			expr, ok := GetBillingExpr(name)
			require.True(t, ok)
			require.Equal(t, HunyuanA13BInstructBillingExpr, expr)
		})
	}
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
		"MiniMax-M3",
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

func TestOpenAIOfficialListPriceExpressionsSelect272KBoundary(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		standard float64
		long     float64
	}{
		{name: "gpt-6-astra", expr: GPT6AstraBillingExpr, standard: 73500, long: 122000},
		{name: "gpt-5.6-sol", expr: GPT56SolBillingExpr, standard: 41750, long: 68500},
		{name: "gpt-5.6-terra", expr: GPT56TerraBillingExpr, standard: 16700, long: 27400},
		{name: "gpt-5.6-luna", expr: GPT56LunaBillingExpr, standard: 1670, long: 2740},
		{name: "gpt-5.6-cyber", expr: GPT56CyberBillingExpr, standard: 104375, long: 171250},
		{name: "gpt-5.5", expr: GPT55BillingExpr, standard: 35500, long: 56000},
		{name: "gpt-5.4", expr: GPT54BillingExpr, standard: 17750, long: 28000},
		{name: "gpt-5.5-pro", expr: GPT55ProBillingExpr, standard: 210000, long: 330000},
		{name: "gpt-5.4-pro", expr: GPT54ProBillingExpr, standard: 210000, long: 330000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			standard, trace, err := billingexpr.RunExpr(tt.expr, billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000, CC: 1000, Len: 272000})
			require.NoError(t, err)
			require.Equal(t, "standard", trace.MatchedTier)
			require.InDelta(t, tt.standard, standard, 1e-9)

			long, trace, err := billingexpr.RunExpr(tt.expr, billingexpr.TokenParams{P: 1000, C: 1000, CR: 1000, CC: 1000, Len: 272001})
			require.NoError(t, err)
			require.Equal(t, "long_context", trace.MatchedTier)
			require.InDelta(t, tt.long, long, 1e-9)
		})
	}
}

func TestOpenAIOfficialPricingAliasesUseTieredBilling(t *testing.T) {
	cases := map[string]string{
		"gpt-6-astra":                   GPT6AstraBillingExpr,
		"gpt-5.6":                       GPT56SolBillingExpr,
		"gpt-5.6-sol":                   GPT56SolBillingExpr,
		"gpt-5.6-terra":                 GPT56TerraBillingExpr,
		"gpt-5.6-luna":                  GPT56LunaBillingExpr,
		"gpt-5.6-luna-wm":               GPT56LunaBillingExpr,
		"gpt-5.6-luna-pro":              GPT56LunaBillingExpr,
		"gpt-5.6-cyber":                 GPT56CyberBillingExpr,
		"gpt-5.5-cyber":                 GPT55CyberBillingExpr,
		"gpt-daybreak-blue-latest":      GPT56SolBillingExpr,
		"gpt-daybreak-red-latest":       GPT56CyberBillingExpr,
		"gpt-5.5":                       GPT55BillingExpr,
		"gpt-5.4":                       GPT54BillingExpr,
		"openai/gpt-5.4-2026-03-05":     GPT54BillingExpr,
		"gpt-5.5-pro":                   GPT55ProBillingExpr,
		"gpt-5.4-pro":                   GPT54ProBillingExpr,
		"openai/gpt-5.4-pro-2026-03-05": GPT54ProBillingExpr,
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

func TestOpenAIUppercaseModelAliasUsesTieredBilling(t *testing.T) {
	for _, name := range []string{"GPT-5.5", "GPT-5.5(high)", "openai/GPT-5.5"} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, BillingModeTieredExpr, GetBillingMode(name))
			expr, ok := GetBillingExpr(name)
			require.True(t, ok)
			require.Equal(t, GPT55BillingExpr, expr)
		})
	}
}

func TestLegacyGPT56SolExpressionUpgradesByCanonicalAlias(t *testing.T) {
	cases := map[string]string{
		"gpt-5.6":                  GPT56SolBillingExpr,
		"gpt-5.6-luna-wm":          GPT56LunaBillingExpr,
		"gpt-5.6-luna-pro":         GPT56LunaBillingExpr,
		"gpt-daybreak-blue-latest": GPT56SolBillingExpr,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, want, upgradeBillingExpr(name, GPT56SolLegacyBillingExpr))
		})
	}
}

func TestGPT56SolHistoricalExpressionUsesNonPromotionalListPrice(t *testing.T) {
	before := GPT56SolPromotionEffectiveAt.Add(-time.Second).Unix()
	atBoundary := GPT56SolPromotionEffectiveAt.Unix()
	for _, name := range []string{"gpt-5.6", "gpt-5.6-sol", "gpt-5.6-sol-wm", "gpt-daybreak-blue-latest"} {
		t.Run(name, func(t *testing.T) {
			expr, ok := GetBillingExprAt(name, before)
			require.True(t, ok)
			require.Equal(t, GPT56SolBillingExpr, expr)

			expr, ok = GetBillingExprAt(name, atBoundary)
			require.True(t, ok)
			require.Equal(t, GPT56SolBillingExpr, expr)
		})
	}

	for _, createdAt := range []int64{0, atBoundary, atBoundary + 1} {
		expr, ok := GetBillingExprAt("gpt-5.6-luna", createdAt)
		require.True(t, ok)
		require.Equal(t, GPT56LunaBillingExpr, expr)
	}
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
	standard, trace, err := billingexpr.RunExpr(Grok45BillingExpr, billingexpr.TokenParams{P: 1000, C: 1000, Len: 199999, CR: 1000})
	require.NoError(t, err)
	require.Equal(t, "standard", trace.MatchedTier)
	require.InDelta(t, 8300, standard, 1e-9)

	long, trace, err := billingexpr.RunExpr(Grok45BillingExpr, billingexpr.TokenParams{P: 1000, C: 1000, Len: 200000, CR: 1000})
	require.NoError(t, err)
	require.Equal(t, "long_context", trace.MatchedTier)
	require.InDelta(t, 16600, long, 1e-9)
}

func TestGrok45LegacyExpressionUpgradesToOfficialPricing(t *testing.T) {
	originalExpr := billingSetting.BillingExpr
	t.Cleanup(func() {
		billingSetting.BillingExpr = originalExpr
	})
	for _, legacyExpr := range []string{Grok45LegacyBillingExpr, Grok45LegacyThresholdBillingExpr} {
		billingSetting.BillingExpr = map[string]string{"grok-4.5": legacyExpr}
		expr, ok := GetBillingExpr("xai/grok-4.5-latest")
		require.True(t, ok)
		require.Equal(t, Grok45BillingExpr, expr)
	}
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
			standard, trace, err := billingexpr.RunExpr(tt.expr, billingexpr.TokenParams{P: 1000, C: 1000, Len: 199999, CR: 1000})
			require.NoError(t, err)
			require.Equal(t, "standard", trace.MatchedTier)
			require.InDelta(t, tt.standard, standard, 1e-9)

			long, trace, err := billingexpr.RunExpr(tt.expr, billingexpr.TokenParams{P: 1000, C: 1000, Len: 200000, CR: 1000})
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
	require.Equal(t, BillingModeTieredExpr, GetBillingMode("deepseek-flash"))
	require.Equal(t, DeepSeekV4FlashBillingExpr, func() string {
		value, ok := GetBillingExpr("deepseek-flash")
		require.True(t, ok)
		return value
	}())
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

func TestDeepSeekHistoricalExpressionsUseEffectiveBoundaries(t *testing.T) {
	timestamp := func(year int, month time.Month, day, hour int) int64 {
		return time.Date(year, month, day, hour, 0, 0, 0, time.UTC).Unix()
	}
	tests := []struct {
		name  string
		model string
		at    int64
		want  string
	}{
		{name: "flash before legacy change", model: "deepseek-flash", at: timestamp(2026, time.August, 15, 12), want: DeepSeekV4FlashPreChangeBillingExpr},
		{name: "flash legacy peak", model: "deepseek-v4-flash", at: timestamp(2026, time.August, 17, 2), want: DeepSeekV4FlashLegacyPeakExpr},
		{name: "flash new off peak at boundary", model: "deepseek-v4-flash", at: timestamp(2026, time.September, 10, 4), want: DeepSeekV4FlashOffPeakExpr},
		{name: "flash new peak", model: "deepseek-flash", at: timestamp(2026, time.September, 10, 6), want: DeepSeekV4FlashPeakExpr},
		{name: "pro before continued service", model: "deepseek-v4-pro", at: timestamp(2026, time.September, 14, 3), want: DeepSeekV4ProLegacyPeakExpr},
		{name: "pro after continued service", model: "deepseek-v4-pro", at: timestamp(2026, time.September, 14, 4), want: DeepSeekV4ProLegacyOffPeakExpr},
		{name: "pro continued service later peak", model: "deepseek-v4-pro", at: timestamp(2026, time.September, 15, 6), want: DeepSeekV4ProLegacyPeakExpr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, ok := GetBillingExprAt(tt.model, tt.at)
			require.True(t, ok)
			require.Equal(t, tt.want, expr)
		})
	}
}

func TestDeepSeekLegacyExpressionSnapshotsMatchPersistedLengths(t *testing.T) {
	// These lengths are taken from the distinct expr_b64 snapshots in the
	// production logs; keeping exact text recognition prevents silent skips.
	require.Len(t, DeepSeekV4FlashLegacyMonthNoWeekdayExpr, 318)
	require.Len(t, DeepSeekV4ProLegacyMonthNoWeekdayExpr, 321)
	require.Len(t, DeepSeekV4FlashLegacyMonthBillingExpr, 370)
	require.Len(t, DeepSeekV4ProLegacyMonthBillingExpr, 373)
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
