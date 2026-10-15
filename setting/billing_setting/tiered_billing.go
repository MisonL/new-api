package billing_setting

import (
	"fmt"
	"strings"
	"time"

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

	// DeepSeek V4.1-Flash is the current model behind deepseek-flash and the
	// retired V4 Flash aliases. Its official price is $0.15/$0.003/$0.60 per
	// million input-cache-hit-output tokens off peak, doubled during the
	// published weekday UTC peak windows.
	DeepSeekV4FlashBillingExpr = `((weekday("UTC") >= 1 && weekday("UTC") <= 5) && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))) ? tier("peak", p * 0.30 + c * 1.20 + cr * 0.006) : tier("off_peak", p * 0.15 + c * 0.60 + cr * 0.003)`
	// V4 Flash's pre-V4.1 schedule is retained for timestamp-aware historical
	// repair. The source form uses year() to avoid applying the August change
	// again in later years; the production hot-loaded form below is kept for
	// compatibility with the older image.
	DeepSeekV4FlashLegacyBillingExpr     = `year("UTC") != 2026 || month("UTC") != 8 || day("UTC") > 16 || (day("UTC") == 16 && hour("UTC") >= 16) ? (((weekday("UTC") >= 1 && weekday("UTC") <= 5) && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))) ? tier("peak", p * 0.44 + c * 1.32 + cr * 0.014) : tier("off_peak", p * 0.22 + c * 0.66 + cr * 0.007)) : tier("current", p * 0.14 + c * 0.28 + cr * 0.0028)`
	DeepSeekV4FlashProductionBillingExpr = DeepSeekV4FlashBillingExpr
	DeepSeekV4FlashCurrentBillingExpr    = `tier("current", p * 0.14 + c * 0.28 + cr * 0.0028)`
	DeepSeekV4FlashPreChangeBillingExpr  = DeepSeekV4FlashCurrentBillingExpr
	DeepSeekV4FlashLegacyPeakExpr        = `tier("peak_legacy", p * 0.44 + c * 1.32 + cr * 0.014)`
	DeepSeekV4FlashLegacyOffPeakExpr     = `tier("off_peak_legacy", p * 0.22 + c * 0.66 + cr * 0.007)`
	DeepSeekV4FlashPeakExpr              = `tier("peak", p * 0.30 + c * 1.20 + cr * 0.006)`
	DeepSeekV4FlashOffPeakExpr           = `tier("off_peak", p * 0.15 + c * 0.60 + cr * 0.003)`
	// These month/day-only snapshots were persisted by the previous production
	// image. Keep their exact text so historical repair can recognize them while
	// evaluating the correct fixed tier from the row timestamp.
	DeepSeekV4FlashLegacyMonthBillingExpr   = `month("UTC") != 8 || day("UTC") > 16 || (day("UTC") == 16 && hour("UTC") >= 16) ? (((weekday("UTC") >= 1 && weekday("UTC") <= 5) && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))) ? tier("peak", p * 0.44 + c * 1.32 + cr * 0.014) : tier("off_peak", p * 0.22 + c * 0.66 + cr * 0.007)) : tier("current", p * 0.14 + c * 0.28 + cr * 0.0028)`
	DeepSeekV4FlashLegacyMonthNoWeekdayExpr = `month("UTC") != 8 || day("UTC") > 16 || (day("UTC") == 16 && hour("UTC") >= 16) ? ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10) ? tier("peak", p * 0.44 + c * 1.32 + cr * 0.014) : tier("off_peak", p * 0.22 + c * 0.66 + cr * 0.007)) : tier("current", p * 0.14 + c * 0.28 + cr * 0.0028)`

	// DeepSeek confirmed that V4 Pro remains available after 2026-09-14 and
	// that its billing method is unchanged. Keep the runtime expression on the
	// published Pro peak/off-peak prices; the historical accessor below handles
	// the earlier pre-August pricing period separately.
	DeepSeekV4ProBillingExpr              = `((weekday("UTC") >= 1 && weekday("UTC") <= 5) && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))) ? tier("peak", p * 1.32 + c * 3.96 + cr * 0.044) : tier("off_peak", p * 0.66 + c * 1.98 + cr * 0.022)`
	DeepSeekV4ProLegacyBillingExpr        = `year("UTC") != 2026 || month("UTC") != 8 || day("UTC") > 16 || (day("UTC") == 16 && hour("UTC") >= 16) ? (((weekday("UTC") >= 1 && weekday("UTC") <= 5) && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))) ? tier("peak", p * 1.32 + c * 3.96 + cr * 0.044) : tier("off_peak", p * 0.66 + c * 1.98 + cr * 0.022)) : tier("current", p * 0.435 + c * 0.87 + cr * 0.003625)`
	DeepSeekV4ProProductionBillingExpr    = DeepSeekV4ProBillingExpr
	DeepSeekV4ProCurrentBillingExpr       = `tier("current", p * 0.435 + c * 0.87 + cr * 0.003625)`
	DeepSeekV4ProPreChangeBillingExpr     = DeepSeekV4ProCurrentBillingExpr
	DeepSeekV4ProLegacyPeakExpr           = `tier("peak_legacy", p * 1.32 + c * 3.96 + cr * 0.044)`
	DeepSeekV4ProLegacyOffPeakExpr        = `tier("off_peak_legacy", p * 0.66 + c * 1.98 + cr * 0.022)`
	DeepSeekV4ProLegacyMonthBillingExpr   = `month("UTC") != 8 || day("UTC") > 16 || (day("UTC") == 16 && hour("UTC") >= 16) ? (((weekday("UTC") >= 1 && weekday("UTC") <= 5) && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))) ? tier("peak", p * 1.32 + c * 3.96 + cr * 0.044) : tier("off_peak", p * 0.66 + c * 1.98 + cr * 0.022)) : tier("current", p * 0.435 + c * 0.87 + cr * 0.003625)`
	DeepSeekV4ProLegacyMonthNoWeekdayExpr = `month("UTC") != 8 || day("UTC") > 16 || (day("UTC") == 16 && hour("UTC") >= 16) ? ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10) ? tier("peak", p * 1.32 + c * 3.96 + cr * 0.044) : tier("off_peak", p * 0.66 + c * 1.98 + cr * 0.022)) : tier("current", p * 0.435 + c * 0.87 + cr * 0.003625)`

	// xAI Grok 4.5 uses a 200K context threshold. The current official
	// prices are $2/$0.30/$6 below the threshold and $4/$0.60/$12 above it.
	Grok45BillingExpr = `len < 200000 ? tier("standard", p * 2 + c * 6 + cr * 0.3) : tier("long_context", p * 4 + c * 12 + cr * 0.6)`
	// These exact <= forms were persisted by earlier images. Keep them
	// separate from the current expression so upgradeBillingExpr can recognize
	// and migrate both the old threshold semantics and the old cache price.
	Grok45LegacyThresholdBillingExpr = `len <= 200000 ? tier("standard", p * 2 + c * 6 + cr * 0.3) : tier("long_context", p * 4 + c * 12 + cr * 0.6)`
	Grok45LegacyBillingExpr          = `len <= 200000 ? tier("standard", p * 2 + c * 6 + cr * 0.5) : tier("long_context", p * 4 + c * 12 + cr * 1)`

	// xAI's current language-model table uses a 200K context threshold for
	// Grok 4.3, Grok 4.20, Grok 4.6, and Grok Build 0.1. Coefficients are the
	// published USD prices per one million tokens.
	Grok43BillingExpr      = `len < 200000 ? tier("standard", p * 1.25 + c * 2.5 + cr * 0.2) : tier("long_context", p * 2.5 + c * 5 + cr * 0.4)`
	Grok420BillingExpr     = Grok43BillingExpr
	Grok46BillingExpr      = `len < 200000 ? tier("standard", p * 2 + c * 6 + cr * 0.5) : tier("long_context", p * 4 + c * 12 + cr * 1)`
	GrokBuild01BillingExpr = `len < 200000 ? tier("standard", p * 1 + c * 2 + cr * 0.2) : tier("long_context", p * 2 + c * 4 + cr * 0.4)`

	// OpenAI API Standard list prices. These expressions intentionally do not
	// use the separate ChatGPT Work/Codex Enterprise rate card: that card has
	// a GPT-6 Astra Codex long-context exception and does not charge Codex
	// cache writes, while API pricing publishes explicit long-context and
	// cache-write rates. GPT-6 and GPT-5.6 publish cache-write prices, so their
	// expressions use cc explicitly. GPT-5.5 and GPT-5.4 do not publish a
	// cache-write price and therefore leave that usage in p.
	GPT6AstraBillingExpr = `len <= 272000 ? tier("standard", p * 10 + c * 50 + cr * 1 + cc * 12.5) : tier("long_context", p * 20 + c * 75 + cr * 2 + cc * 25)`
	// OpenAI's non-promotional Standard API list price for gpt-5.6-sol is
	// $5/$0.50/$6.25/$30 per million input/cache-read/cache-write/output
	// tokens. The >272K tier is $10/$1/$12.5/$45. The OpenAI page separately
	// marks a temporary lower price as promotional; this project deliberately
	// keeps the published list price per the pricing-maintenance policy.
	GPT56SolBillingExpr = `len <= 272000 ? tier("standard", p * 5 + c * 30 + cr * 0.5 + cc * 6.25) : tier("long_context", p * 10 + c * 45 + cr * 1 + cc * 12.5)`
	// GPT56SolPromotionBillingExpr is retained solely to recognize historical
	// logs written while OpenAI's 2026-08-21 promotional price was configured.
	GPT56SolPromotionBillingExpr = `len <= 272000 ? tier("standard", p * 4 + c * 20 + cr * 0.4 + cc * 5) : tier("long_context", p * 8 + c * 30 + cr * 0.8 + cc * 10)`
	// Compatibility name for callers that used the pre-promotion expression.
	GPT56SolLegacyBillingExpr = GPT56SolBillingExpr
	GPT56TerraBillingExpr     = `len <= 272000 ? tier("standard", p * 2 + c * 12 + cr * 0.2 + cc * 2.5) : tier("long_context", p * 4 + c * 18 + cr * 0.4 + cc * 5)`
	GPT56LunaBillingExpr      = `len <= 272000 ? tier("standard", p * 0.2 + c * 1.2 + cr * 0.02 + cc * 0.25) : tier("long_context", p * 0.4 + c * 1.8 + cr * 0.04 + cc * 0.5)`
	// GPT-5.6 Cyber (and the Daybreak Red alias) documents the same >272K
	// multiplier rule as the GPT-5.6 family. Its 272K maximum input currently
	// makes the long branch unreachable for accepted requests, but retaining
	// the documented branch keeps the expression future-proof.
	GPT56CyberBillingExpr = `len <= 272000 ? tier("standard", p * 12.5 + c * 75 + cr * 1.25 + cc * 15.625) : tier("long_context", p * 25 + c * 112.5 + cr * 2.5 + cc * 31.25)`
	// GPT-5.5 Cyber has no published cache-write price or long-context tier.
	GPT55CyberBillingExpr = `tier("standard", p * 12.5 + c * 75 + cr * 1.25)`
	GPT55BillingExpr      = `len <= 272000 ? tier("standard", p * 5 + c * 30 + cr * 0.5) : tier("long_context", p * 10 + c * 45 + cr * 1)`
	GPT54BillingExpr      = `len <= 272000 ? tier("standard", p * 2.5 + c * 15 + cr * 0.25) : tier("long_context", p * 5 + c * 22.5 + cr * 0.5)`
	// GPT-5.5 Pro and GPT-5.4 Pro do not offer a cached-input discount. Their
	// published standard prices are $30/$180 and the >272K prices are
	// $60/$270 per million input/output tokens.
	GPT55ProBillingExpr = `len <= 272000 ? tier("standard", p * 30 + c * 180) : tier("long_context", p * 60 + c * 270)`
	GPT54ProBillingExpr = `len <= 272000 ? tier("standard", p * 30 + c * 180) : tier("long_context", p * 60 + c * 270)`

	// SiliconFlow publishes Hunyuan-A13B-Instruct in RMB per one million
	// tokens with a Shanghai-time peak window. Convert the official prices to
	// the gateway's USD expression unit using the fixed project exchange rate.
	HunyuanA13BInstructBillingExpr = `((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 18) ? tier("peak", p * 0.136986301369863 + c * 0.547945205479452) : tier("off_peak", p * 0.10958904109589 + c * 0.438356164383562))`

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

	// MiniMax M3 uses a 512K context threshold. The provider marks the
	// displayed current prices as Permanent 50% off the list prices, so this
	// expression uses the permanently discounted prices. Priority service tier
	// is 1.5x standard and is selected from the request body.
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
		"deepseek-flash":                           BillingModeTieredExpr,
		"deepseek-ai/deepseek-flash":               BillingModeTieredExpr,
		"deepseek-v4.1-flash":                      BillingModeTieredExpr,
		"deepseek-v4.1-flash-0910":                 BillingModeTieredExpr,
		"deepseek-v4-flash":                        BillingModeTieredExpr,
		"deepseek-v4-flash-vision-exp":             BillingModeTieredExpr,
		"deepseek-ai/deepseek-v4-flash-vision-exp": BillingModeTieredExpr,
		"deepseek-v4-pro":                          BillingModeTieredExpr,
		"minimax-m3":                               BillingModeTieredExpr,
		"minimaxai/minimax-m3":                     BillingModeTieredExpr,
		"MiniMax-M3":                               BillingModeTieredExpr,
		"grok-4.5":                                 BillingModeTieredExpr,
		"grok-4.3":                                 BillingModeTieredExpr,
		"grok-4.20":                                BillingModeTieredExpr,
		"grok-4.6":                                 BillingModeTieredExpr,
		"grok-build-0.1":                           BillingModeTieredExpr,
		"grok-code-fast-1":                         BillingModeTieredExpr,
		"gpt-6-astra":                              BillingModeTieredExpr,
		"gpt-5.6":                                  BillingModeTieredExpr,
		"gpt-5.6-sol":                              BillingModeTieredExpr,
		"gpt-5.6-terra":                            BillingModeTieredExpr,
		"gpt-5.6-luna":                             BillingModeTieredExpr,
		"gpt-5.6-cyber":                            BillingModeTieredExpr,
		"gpt-5.5-cyber":                            BillingModeTieredExpr,
		"gpt-daybreak-blue-latest":                 BillingModeTieredExpr,
		"gpt-daybreak-red-latest":                  BillingModeTieredExpr,
		"gpt-5.5":                                  BillingModeTieredExpr,
		"gpt-5.4":                                  BillingModeTieredExpr,
		"gpt-5.4-2026-03-05":                       BillingModeTieredExpr,
		"gpt-5.5-pro":                              BillingModeTieredExpr,
		"gpt-5.4-pro":                              BillingModeTieredExpr,
		"gpt-5.4-pro-2026-03-05":                   BillingModeTieredExpr,
		"tencent/Hunyuan-A13B-Instruct":            BillingModeTieredExpr,
		"tencent/hunyuan-a13b-instruct":            BillingModeTieredExpr,
		"hunyuan-a13b-instruct":                    BillingModeTieredExpr,
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
		"deepseek-flash":                           DeepSeekV4FlashBillingExpr,
		"deepseek-ai/deepseek-flash":               DeepSeekV4FlashBillingExpr,
		"deepseek-v4.1-flash":                      DeepSeekV4FlashBillingExpr,
		"deepseek-v4.1-flash-0910":                 DeepSeekV4FlashBillingExpr,
		"deepseek-v4-flash":                        DeepSeekV4FlashBillingExpr,
		"deepseek-v4-flash-vision-exp":             DeepSeekV4FlashBillingExpr,
		"deepseek-ai/deepseek-v4-flash-vision-exp": DeepSeekV4FlashBillingExpr,
		"deepseek-v4-pro":                          DeepSeekV4ProBillingExpr,
		"minimax-m3":                               MiniMaxM3BillingExpr,
		"minimaxai/minimax-m3":                     MiniMaxM3BillingExpr,
		"MiniMax-M3":                               MiniMaxM3BillingExpr,
		"grok-4.5":                                 Grok45BillingExpr,
		"grok-4.3":                                 Grok43BillingExpr,
		"grok-4.20":                                Grok420BillingExpr,
		"grok-4.6":                                 Grok46BillingExpr,
		"grok-build-0.1":                           GrokBuild01BillingExpr,
		"grok-code-fast-1":                         GrokBuild01BillingExpr,
		"gpt-6-astra":                              GPT6AstraBillingExpr,
		"gpt-5.6":                                  GPT56SolBillingExpr,
		"gpt-5.6-sol":                              GPT56SolBillingExpr,
		"gpt-5.6-terra":                            GPT56TerraBillingExpr,
		"gpt-5.6-luna":                             GPT56LunaBillingExpr,
		"gpt-5.6-cyber":                            GPT56CyberBillingExpr,
		"gpt-5.5-cyber":                            GPT55CyberBillingExpr,
		"gpt-daybreak-blue-latest":                 GPT56SolBillingExpr,
		"gpt-daybreak-red-latest":                  GPT56CyberBillingExpr,
		"gpt-5.5":                                  GPT55BillingExpr,
		"gpt-5.4":                                  GPT54BillingExpr,
		"gpt-5.4-2026-03-05":                       GPT54BillingExpr,
		"gpt-5.5-pro":                              GPT55ProBillingExpr,
		"gpt-5.4-pro":                              GPT54ProBillingExpr,
		"gpt-5.4-pro-2026-03-05":                   GPT54ProBillingExpr,
		"tencent/Hunyuan-A13B-Instruct":            HunyuanA13BInstructBillingExpr,
		"tencent/hunyuan-a13b-instruct":            HunyuanA13BInstructBillingExpr,
		"hunyuan-a13b-instruct":                    HunyuanA13BInstructBillingExpr,
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
		return upgradeBillingExpr(model, expr), true
	}
	if canonical := ratio_setting.FormatMatchingModelName(model); canonical != model {
		expr, ok := billingSetting.BillingExpr[canonical]
		if ok {
			return upgradeBillingExpr(canonical, expr), true
		}
	}
	return "", false
}

// GPT56SolPromotionEffectiveAt is retained as an audit marker for the
// temporary OpenAI price published on 2026-08-21. It is not used to select a
// lower price: both new requests and historical repairs use the project's
// configured non-promotional list price.
var GPT56SolPromotionEffectiveAt = time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC)

var DeepSeekV4FlashEffectiveAt = time.Date(2026, time.September, 10, 4, 0, 0, 0, time.UTC)
var DeepSeekV4LegacyEffectiveAt = time.Date(2026, time.August, 16, 16, 0, 0, 0, time.UTC)

// GetBillingExprAt returns a fixed expression effective for a model at a log
// timestamp. Runtime billing should use GetBillingExpr; this accessor is for
// timestamp-aware historical repair so old rows are not evaluated against the
// current wall clock or rewritten with a newer price without an explicit
// effective-date rule.
func GetBillingExprAt(model string, createdAt int64) (string, bool) {
	canonical := ratio_setting.FormatMatchingModelName(model)
	if createdAt > 0 {
		created := time.Unix(createdAt, 0).UTC()
		switch canonical {
		case "deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp":
			if created.Before(DeepSeekV4LegacyEffectiveAt) {
				return DeepSeekV4FlashPreChangeBillingExpr, true
			}
			if created.Before(DeepSeekV4FlashEffectiveAt) {
				if deepSeekPeakAt(created) {
					return DeepSeekV4FlashLegacyPeakExpr, true
				}
				return DeepSeekV4FlashLegacyOffPeakExpr, true
			}
			if deepSeekPeakAt(created) {
				return DeepSeekV4FlashPeakExpr, true
			}
			return DeepSeekV4FlashOffPeakExpr, true
		case "deepseek-v4-pro":
			if created.Before(DeepSeekV4LegacyEffectiveAt) {
				return DeepSeekV4ProPreChangeBillingExpr, true
			}
			if deepSeekPeakAt(created) {
				return DeepSeekV4ProLegacyPeakExpr, true
			}
			return DeepSeekV4ProLegacyOffPeakExpr, true
		}
	}
	return GetBillingExpr(model)
}

func deepSeekPeakAt(t time.Time) bool {
	weekday := t.UTC().Weekday()
	if weekday < time.Monday || weekday > time.Friday {
		return false
	}
	hour := t.UTC().Hour()
	return (hour >= 1 && hour < 4) || (hour >= 6 && hour < 10)
}

func upgradeBillingExpr(model, expr string) string {
	switch ratio_setting.FormatMatchingModelName(model) {
	case "deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp":
		if expr == DeepSeekV4FlashCurrentBillingExpr || expr == DeepSeekV4FlashProductionBillingExpr || expr == DeepSeekV4FlashLegacyBillingExpr {
			return DeepSeekV4FlashBillingExpr
		}
	case "deepseek-v4-pro":
		if expr == DeepSeekV4ProCurrentBillingExpr || expr == DeepSeekV4ProProductionBillingExpr || expr == DeepSeekV4ProLegacyBillingExpr {
			return DeepSeekV4ProBillingExpr
		}
	case "grok-4.5":
		if expr == Grok45LegacyBillingExpr || expr == Grok45LegacyThresholdBillingExpr {
			return Grok45BillingExpr
		}
	case "minimax-m3":
		if expr == MiniMaxM3LegacyBillingExpr {
			return MiniMaxM3BillingExpr
		}
	case "gpt-5.6-luna":
		if expr == GPT56SolLegacyBillingExpr {
			return GPT56LunaBillingExpr
		}
	case "gpt-5.6", "gpt-5.6-sol", "gpt-daybreak-blue-latest":
		if expr == GPT56SolLegacyBillingExpr || expr == GPT56SolPromotionBillingExpr {
			return GPT56SolBillingExpr
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
	// 向量含 c > p 组合：可产出负值的表达式（如 (p-c)*k）在保存时即被拒绝，
	// 而不是等真实请求 c>p 时结算出负额度（delta<0 会走返还路径）。
	vectors := []billingexpr.TokenParams{
		{P: 0, C: 0, Len: 0},
		{P: 1000, C: 1000, Len: 1000},
		{P: 100000, C: 100000, Len: 100000},
		{P: 1000000, C: 1000000, Len: 1000000},
		{P: 1000000, C: 2000000, Len: 1000000},
		{P: 0, C: 1000000, Len: 0},
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
