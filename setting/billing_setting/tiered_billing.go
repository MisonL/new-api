package billing_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/samber/lo"
)

const (
	BillingModeRatio      = "ratio"
	BillingModeTieredExpr = "tiered_expr"
	BillingModeField      = "billing_mode"
	BillingExprField      = "billing_expr"

	// DeepSeek V4 keeps the published pre-change price until 2026-08-16
	// 16:00 UTC, then switches to the announced UTC peak/off-peak schedule.
	// Peak pricing applies Monday through Friday only.
	// The source expression includes the year to avoid repeating the change
	// every August. The currently running production binary keeps the older
	// month/day-compatible expression until its normal image upgrade.
	DeepSeekV4FlashBillingExpr = `year("UTC") != 2026 || month("UTC") != 8 || day("UTC") > 16 || (day("UTC") == 16 && hour("UTC") >= 16) ? (((weekday("UTC") >= 1 && weekday("UTC") <= 5) && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))) ? tier("peak", p * 0.44 + c * 1.32 + cr * 0.014) : tier("off_peak", p * 0.22 + c * 0.66 + cr * 0.007)) : tier("current", p * 0.14 + c * 0.28 + cr * 0.0028)`
	DeepSeekV4ProBillingExpr   = `year("UTC") != 2026 || month("UTC") != 8 || day("UTC") > 16 || (day("UTC") == 16 && hour("UTC") >= 16) ? (((weekday("UTC") >= 1 && weekday("UTC") <= 5) && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))) ? tier("peak", p * 1.32 + c * 3.96 + cr * 0.044) : tier("off_peak", p * 0.66 + c * 1.98 + cr * 0.022)) : tier("current", p * 0.435 + c * 0.87 + cr * 0.003625)`
	// The production image predates year(), so this month/day-compatible form
	// is used for its hot-loaded options until the image is upgraded.
	DeepSeekV4FlashProductionBillingExpr = `month("UTC") != 8 || day("UTC") > 16 || (day("UTC") == 16 && hour("UTC") >= 16) ? (((weekday("UTC") >= 1 && weekday("UTC") <= 5) && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))) ? tier("peak", p * 0.44 + c * 1.32 + cr * 0.014) : tier("off_peak", p * 0.22 + c * 0.66 + cr * 0.007)) : tier("current", p * 0.14 + c * 0.28 + cr * 0.0028)`
	DeepSeekV4ProProductionBillingExpr   = `month("UTC") != 8 || day("UTC") > 16 || (day("UTC") == 16 && hour("UTC") >= 16) ? (((weekday("UTC") >= 1 && weekday("UTC") <= 5) && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))) ? tier("peak", p * 1.32 + c * 3.96 + cr * 0.044) : tier("off_peak", p * 0.66 + c * 1.98 + cr * 0.022)) : tier("current", p * 0.435 + c * 0.87 + cr * 0.003625)`
	// These flat expressions are accepted by the currently running
	// production binary. A new binary upgrades them to the time-aware form.
	DeepSeekV4FlashCurrentBillingExpr = `tier("current", p * 0.14 + c * 0.28 + cr * 0.0028)`
	DeepSeekV4ProCurrentBillingExpr   = `tier("current", p * 0.435 + c * 0.87 + cr * 0.003625)`

	// xAI Grok 4.5 uses a 200K context threshold. The current official
	// prices are $2/$0.30/$6 below the threshold and $4/$0.60/$12 above it.
	Grok45BillingExpr       = `len <= 200000 ? tier("standard", p * 2 + c * 6 + cr * 0.3) : tier("long_context", p * 4 + c * 12 + cr * 0.6)`
	Grok45LegacyBillingExpr = `len <= 200000 ? tier("standard", p * 2 + c * 6 + cr * 0.5) : tier("long_context", p * 4 + c * 12 + cr * 1)`

	// xAI's current language-model table uses a 200K context threshold for
	// Grok 4.3, Grok 4.20, Grok 4.6, and Grok Build 0.1. Coefficients are the
	// published USD prices per one million tokens.
	Grok43BillingExpr      = `len <= 200000 ? tier("standard", p * 1.25 + c * 2.5 + cr * 0.2) : tier("long_context", p * 2.5 + c * 5 + cr * 0.4)`
	Grok420BillingExpr     = Grok43BillingExpr
	Grok46BillingExpr      = `len <= 200000 ? tier("standard", p * 2 + c * 6 + cr * 0.5) : tier("long_context", p * 4 + c * 12 + cr * 1)`
	GrokBuild01BillingExpr = `len <= 200000 ? tier("standard", p * 1 + c * 2 + cr * 0.2) : tier("long_context", p * 2 + c * 4 + cr * 0.4)`

	// Gemini 2.5 Pro uses a 200K context threshold. The official paid-tier
	// prices are $1.25/$0.125/$10 below the threshold and
	// $2.50/$0.25/$15 above it for input/cache-read/output.
	Gemini25ProBillingExpr = `len <= 200000 ? tier("standard", p * 1.25 + c * 10 + cr * 0.125) : tier("long_context", p * 2.5 + c * 15 + cr * 0.25)`

	// Gemini standard paid-tier prices are expressed in USD per one million
	// tokens. The source page labels the lower Gemini 3.6/3.7 prices as a
	// time-limited 2026 promotion; use the published standard price instead.
	Gemini37FlashBillingExpr              = `tier("standard", p * 1.5 + c * 7.5 + cr * 0.15)`
	Gemini36FlashBillingExpr              = Gemini37FlashBillingExpr
	Gemini35FlashBillingExpr              = `tier("standard", p * 1.5 + c * 9 + cr * 0.15)`
	Gemini35FlashLiteBillingExpr          = `tier("standard", p * 0.3 + c * 2.5 + cr * 0.03)`
	Gemini31FlashLiteBillingExpr          = `tier("standard", p * 0.25 + c * 1.5 + cr * 0.025 + ai * 0.5)`
	GeminiOmniFlashBillingExpr            = `tier("standard", p * 1.5 + c * 9 + img_o * 17.5)`
	Gemini31ProPreviewBillingExpr         = `len <= 200000 ? tier("standard", p * 2 + c * 12 + cr * 0.2) : tier("long_context", p * 4 + c * 18 + cr * 0.4)`
	Gemini31FlashLiveBillingExpr          = `tier("standard", p * 0.75 + c * 4.5 + img * 1 + ai * 3 + ao * 12)`
	Gemini35LiveTranslateBillingExpr      = `tier("standard", p * 3.5 + c * 21)`
	Gemini31FlashImageBillingExpr         = `tier("standard", p * 0.5 + c * 3 + img_o * 60)`
	Gemini31FlashLiteImageBillingExpr     = `tier("standard", p * 0.25 + c * 1.5 + img_o * 30)`
	Gemini3ProImageBillingExpr            = `tier("standard", p * 2 + c * 12 + img_o * 120)`
	Gemini31FlashTTSBillingExpr           = `tier("standard", p * 1 + c * 20)`
	GeminiRoboticsER16BillingExpr         = `tier("standard", p * 1 + c * 5 + ai * 2)`
	GeminiRoboticsER2BillingExpr          = `tier("standard", p * 2 + c * 10 + cr * 0.2)`
	GeminiRoboticsER2StreamingBillingExpr = `tier("standard", p * 2 + c * 10)`

	// MiniMax M3 uses a 512K context threshold. Priority service tier is 1.5x
	// the standard price and is selected from the request body.
	MiniMaxM3BillingExpr       = `len <= 512000 ? (param("service_tier") == "priority" ? tier("priority_standard", p * 0.45 + c * 1.8 + cr * 0.09) : tier("standard", p * 0.3 + c * 1.2 + cr * 0.06)) : (param("service_tier") == "priority" ? tier("priority_long_context", p * 0.9 + c * 3.6 + cr * 0.18) : tier("long_context", p * 0.6 + c * 2.4 + cr * 0.12))`
	MiniMaxM3LegacyBillingExpr = `len <= 512000 ? tier("standard", p * 0.3 + c * 1.2 + cr * 0.06) : tier("long_context", p * 0.6 + c * 2.4 + cr * 0.12)`
)

// BillingSetting is managed by config.GlobalConfig.Register.
// DB keys: billing_setting.billing_mode, billing_setting.billing_expr
type BillingSetting struct {
	BillingMode map[string]string `json:"billing_mode"`
	BillingExpr map[string]string `json:"billing_expr"`
}

var billingSetting = BillingSetting{
	BillingMode: map[string]string{
		"deepseek-v4-flash":                        BillingModeTieredExpr,
		"deepseek-v4-flash-vision-exp":             BillingModeTieredExpr,
		"deepseek-ai/deepseek-v4-flash-vision-exp": BillingModeTieredExpr,
		"deepseek-v4-pro":                          BillingModeTieredExpr,
		"minimax-m3":                               BillingModeTieredExpr,
		"minimaxai/minimax-m3":                     BillingModeTieredExpr,
		"grok-4.5":                                 BillingModeTieredExpr,
		"grok-4.3":                                 BillingModeTieredExpr,
		"grok-4.20":                                BillingModeTieredExpr,
		"grok-4.6":                                 BillingModeTieredExpr,
		"grok-build-0.1":                           BillingModeTieredExpr,
		"grok-code-fast-1":                         BillingModeTieredExpr,
		"gemini-2.5-pro":                           BillingModeTieredExpr,
		"gemini-2.5-pro-exp-03-25":                 BillingModeTieredExpr,
		"gemini-2.5-pro-preview-03-25":             BillingModeTieredExpr,
		"gemini-2.5-pro-thinking-*":                BillingModeTieredExpr,
		"gemini-3.7-flash":                         BillingModeTieredExpr,
		"gemini-3.6-flash":                         BillingModeTieredExpr,
		"gemini-3.5-flash":                         BillingModeTieredExpr,
		"gemini-3.5-flash-lite":                    BillingModeTieredExpr,
		"gemini-3.1-flash-lite-preview":            BillingModeTieredExpr,
		"gemini-omni-flash-preview":                BillingModeTieredExpr,
		"gemini-3.1-pro-preview":                   BillingModeTieredExpr,
		"gemini-3.1-flash-live-preview":            BillingModeTieredExpr,
		"gemini-3.5-live-translate-preview":        BillingModeTieredExpr,
		"gemini-3.1-flash-image-preview":           BillingModeTieredExpr,
		"gemini-3.1-flash-image":                   BillingModeTieredExpr,
		"gemini-3.1-flash-lite-image":              BillingModeTieredExpr,
		"gemini-3.1-flash-lite-image-preview":      BillingModeTieredExpr,
		"gemini-3-pro-image-preview":               BillingModeTieredExpr,
		"gemini-3-pro-image":                       BillingModeTieredExpr,
		"gemini-3.1-flash-tts-preview":             BillingModeTieredExpr,
		"gemini-robotics-er-1.6-preview":           BillingModeTieredExpr,
		"gemini-robotics-er-2-preview":             BillingModeTieredExpr,
		"gemini-robotics-er-2-streaming-preview":   BillingModeTieredExpr,
	},
	BillingExpr: map[string]string{
		"deepseek-v4-flash":                        DeepSeekV4FlashBillingExpr,
		"deepseek-v4-flash-vision-exp":             DeepSeekV4FlashBillingExpr,
		"deepseek-ai/deepseek-v4-flash-vision-exp": DeepSeekV4FlashBillingExpr,
		"deepseek-v4-pro":                          DeepSeekV4ProBillingExpr,
		"minimax-m3":                               MiniMaxM3BillingExpr,
		"minimaxai/minimax-m3":                     MiniMaxM3BillingExpr,
		"grok-4.5":                                 Grok45BillingExpr,
		"grok-4.3":                                 Grok43BillingExpr,
		"grok-4.20":                                Grok420BillingExpr,
		"grok-4.6":                                 Grok46BillingExpr,
		"grok-build-0.1":                           GrokBuild01BillingExpr,
		"grok-code-fast-1":                         GrokBuild01BillingExpr,
		"gemini-2.5-pro":                           Gemini25ProBillingExpr,
		"gemini-2.5-pro-exp-03-25":                 Gemini25ProBillingExpr,
		"gemini-2.5-pro-preview-03-25":             Gemini25ProBillingExpr,
		"gemini-2.5-pro-thinking-*":                Gemini25ProBillingExpr,
		"gemini-3.7-flash":                         Gemini37FlashBillingExpr,
		"gemini-3.6-flash":                         Gemini36FlashBillingExpr,
		"gemini-3.5-flash":                         Gemini35FlashBillingExpr,
		"gemini-3.5-flash-lite":                    Gemini35FlashLiteBillingExpr,
		"gemini-3.1-flash-lite-preview":            Gemini31FlashLiteBillingExpr,
		"gemini-omni-flash-preview":                GeminiOmniFlashBillingExpr,
		"gemini-3.1-pro-preview":                   Gemini31ProPreviewBillingExpr,
		"gemini-3.1-flash-live-preview":            Gemini31FlashLiveBillingExpr,
		"gemini-3.5-live-translate-preview":        Gemini35LiveTranslateBillingExpr,
		"gemini-3.1-flash-image-preview":           Gemini31FlashImageBillingExpr,
		"gemini-3.1-flash-image":                   Gemini31FlashImageBillingExpr,
		"gemini-3.1-flash-lite-image":              Gemini31FlashLiteImageBillingExpr,
		"gemini-3.1-flash-lite-image-preview":      Gemini31FlashLiteImageBillingExpr,
		"gemini-3-pro-image-preview":               Gemini3ProImageBillingExpr,
		"gemini-3-pro-image":                       Gemini3ProImageBillingExpr,
		"gemini-3.1-flash-tts-preview":             Gemini31FlashTTSBillingExpr,
		"gemini-robotics-er-1.6-preview":           GeminiRoboticsER16BillingExpr,
		"gemini-robotics-er-2-preview":             GeminiRoboticsER2BillingExpr,
		"gemini-robotics-er-2-streaming-preview":   GeminiRoboticsER2StreamingBillingExpr,
	},
}

func init() {
	config.GlobalConfig.Register("billing_setting", &billingSetting)
}

// ---------------------------------------------------------------------------
// Read accessors (hot path, must be fast)
// ---------------------------------------------------------------------------

func GetBillingMode(model string) string {
	if mode, ok := billingSetting.BillingMode[model]; ok {
		return mode
	}
	if canonical := ratio_setting.FormatMatchingModelName(model); canonical != model {
		if mode, ok := billingSetting.BillingMode[canonical]; ok {
			return mode
		}
	}
	return BillingModeRatio
}

func GetBillingExpr(model string) (string, bool) {
	if expr, ok := billingSetting.BillingExpr[model]; ok {
		return upgradeDeepSeekV4BillingExpr(model, expr), true
	}
	if canonical := ratio_setting.FormatMatchingModelName(model); canonical != model {
		expr, ok := billingSetting.BillingExpr[canonical]
		if ok {
			return upgradeDeepSeekV4BillingExpr(canonical, expr), true
		}
	}
	return "", false
}

func upgradeDeepSeekV4BillingExpr(model, expr string) string {
	switch ratio_setting.FormatMatchingModelName(model) {
	case "deepseek-v4-flash", "deepseek-v4-flash-vision-exp":
		if expr == DeepSeekV4FlashCurrentBillingExpr || expr == DeepSeekV4FlashProductionBillingExpr {
			return DeepSeekV4FlashBillingExpr
		}
	case "deepseek-v4-pro":
		if expr == DeepSeekV4ProCurrentBillingExpr || expr == DeepSeekV4ProProductionBillingExpr {
			return DeepSeekV4ProBillingExpr
		}
	case "grok-4.5":
		if expr == Grok45LegacyBillingExpr {
			return Grok45BillingExpr
		}
	case "minimax-m3":
		if expr == MiniMaxM3LegacyBillingExpr {
			return MiniMaxM3BillingExpr
		}
	}
	return expr
}

func ValidateBillingModeJSON(raw string) error {
	modeMap := make(map[string]string)
	if err := common.UnmarshalJsonStr(raw, &modeMap); err != nil {
		return fmt.Errorf("billing mode 配置格式无效: %w", err)
	}
	for modelName, mode := range modeMap {
		switch mode {
		case BillingModeRatio, BillingModeTieredExpr:
		default:
			return fmt.Errorf("模型 %s 的 billing_mode 无效: %s", modelName, mode)
		}
	}
	return nil
}

func ValidateBillingExprJSON(raw string) error {
	exprMap := make(map[string]string)
	if err := common.UnmarshalJsonStr(raw, &exprMap); err != nil {
		return fmt.Errorf("billing expr 配置格式无效: %w", err)
	}
	for modelName, exprStr := range exprMap {
		if strings.TrimSpace(exprStr) == "" {
			return fmt.Errorf("模型 %s 的 billing_expr 不能为空", modelName)
		}
		if err := smokeTestExpr(exprStr); err != nil {
			return fmt.Errorf("模型 %s 的 billing_expr 无效: %w", modelName, err)
		}
	}
	return nil
}

func GetBillingModeCopy() map[string]string {
	return lo.Assign(billingSetting.BillingMode)
}

func GetBillingExprCopy() map[string]string {
	return lo.Assign(billingSetting.BillingExpr)
}

func GetPricingSyncData(base map[string]any) map[string]any {
	extra := make(map[string]any, 2)
	if modes := GetBillingModeCopy(); len(modes) > 0 {
		extra[BillingModeField] = modes
	}
	if exprs := GetBillingExprCopy(); len(exprs) > 0 {
		extra[BillingExprField] = exprs
	}
	return lo.Assign(base, extra)
}

// ---------------------------------------------------------------------------
// Smoke test (called externally for validation before save)
// ---------------------------------------------------------------------------

func SmokeTestExpr(exprStr string) error {
	return smokeTestExpr(exprStr)
}

func smokeTestExpr(exprStr string) error {
	vectors := []billingexpr.TokenParams{
		{P: 0, C: 0, Len: 0},
		{P: 1000, C: 1000, Len: 1000},
		{P: 100000, C: 100000, Len: 100000},
		{P: 1000000, C: 1000000, Len: 1000000},
	}
	requests := []billingexpr.RequestInput{
		{},
		{
			Headers: map[string]string{
				"anthropic-beta": "fast-mode-2026-02-01",
			},
			Body: []byte(`{"service_tier":"fast","stream_options":{"include_usage":true},"messages":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]}`),
		},
	}

	for _, v := range vectors {
		for _, request := range requests {
			result, _, err := billingexpr.RunExprWithRequest(exprStr, v, request)
			if err != nil {
				return fmt.Errorf("vector {p=%g, c=%g}: run failed: %w", v.P, v.C, err)
			}
			if result < 0 {
				return fmt.Errorf("vector {p=%g, c=%g}: result %f < 0", v.P, v.C, result)
			}
		}
	}
	return nil
}
