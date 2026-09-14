package ratio_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

// from songquanpeng/one-api
const (
	USD2RMB = 7.3 // 暂定 1 USD = 7.3 RMB
	USD     = 500 // $0.002 = 1 -> $1 = 500
	RMB     = USD / USD2RMB
)

// modelRatio
// https://platform.openai.com/docs/models/model-endpoint-compatibility
// https://cloud.baidu.com/doc/WENXINWORKSHOP/s/Blfmc9dlf
// https://openai.com/pricing
// TODO: when a new api is enabled, check the pricing here
// 1 === $0.002 / 1K tokens
// 1 === ￥0.014 / 1k tokens

var defaultModelRatio = map[string]float64{
	//"midjourney":                50,
	"gpt-4-gizmo-*":  15,
	"gpt-4o-gizmo-*": 2.5,
	"gpt-4-all":      15,
	"gpt-4o-all":     15,
	"gpt-4":          15,
	//"gpt-4-0314":                   15, //deprecated
	"gpt-4-0613": 15,
	"gpt-4-32k":  30,
	//"gpt-4-32k-0314":               30, //deprecated
	"gpt-4-32k-0613":                          30,
	"gpt-4-1106-preview":                      5,    // $10 / 1M tokens
	"gpt-4-0125-preview":                      5,    // $10 / 1M tokens
	"gpt-4-turbo-preview":                     5,    // $10 / 1M tokens
	"gpt-4-vision-preview":                    5,    // $10 / 1M tokens
	"gpt-4-1106-vision-preview":               5,    // $10 / 1M tokens
	"chatgpt-4o-latest":                       2.5,  // $5 / 1M tokens
	"gpt-4o":                                  1.25, // $2.5 / 1M tokens
	"gpt-4o-audio-preview":                    1.25, // $2.5 / 1M tokens
	"gpt-4o-audio-preview-2024-10-01":         1.25, // $2.5 / 1M tokens
	"gpt-4o-audio-preview-2024-12-17":         1.25,
	"gpt-4o-audio-preview-2025-06-03":         1.25,
	"gpt-4o-2024-05-13":                       2.5,  // $5 / 1M tokens
	"gpt-4o-2024-08-06":                       1.25, // $2.5 / 1M tokens
	"gpt-4o-2024-11-20":                       1.25, // $2.5 / 1M tokens
	"gpt-4o-realtime-preview":                 2.5,
	"gpt-4o-realtime-preview-2025-06-03":      2.5,
	"gpt-4o-realtime-preview-2024-10-01":      2.5,
	"gpt-4o-realtime-preview-2024-12-17":      2.5,
	"gpt-4o-mini-realtime-preview":            0.3,
	"gpt-4o-mini-realtime-preview-2024-12-17": 0.3,
	"gpt-4.1":                          1.0,  // $2 / 1M tokens
	"gpt-4.1-2025-04-14":               1.0,  // $2 / 1M tokens
	"gpt-4.1-mini":                     0.2,  // $0.4 / 1M tokens
	"gpt-4.1-mini-2025-04-14":          0.2,  // $0.4 / 1M tokens
	"gpt-4.1-nano":                     0.05, // $0.1 / 1M tokens
	"gpt-4.1-nano-2025-04-14":          0.05, // $0.1 / 1M tokens
	"gpt-image-1":                      2.5,  // $5 / 1M tokens
	"chatgpt-image-latest":             2.5,  // $5 / 1M text tokens; same price as gpt-image-1.5
	"o1":                               7.5,  // $15 / 1M tokens
	"o1-2024-12-17":                    7.5,  // $15 / 1M tokens
	"o1-preview":                       7.5,  // $15 / 1M tokens
	"o1-preview-2024-09-12":            7.5,  // $15 / 1M tokens
	"o1-mini":                          0.55, // $1.1 / 1M tokens
	"o1-mini-2024-09-12":               0.55, // $1.1 / 1M tokens
	"o1-pro":                           75.0, // $150 / 1M tokens
	"o1-pro-2025-03-19":                75.0, // $150 / 1M tokens
	"o3-mini":                          0.55,
	"o3-mini-2025-01-31":               0.55,
	"o3-mini-high":                     0.55,
	"o3-mini-2025-01-31-high":          0.55,
	"o3-mini-low":                      0.55,
	"o3-mini-2025-01-31-low":           0.55,
	"o3-mini-medium":                   0.55,
	"o3-mini-2025-01-31-medium":        0.55,
	"o3":                               1.0,  // $2 / 1M tokens
	"o3-2025-04-16":                    1.0,  // $2 / 1M tokens
	"o3-pro":                           10.0, // $20 / 1M tokens
	"o3-pro-2025-06-10":                10.0, // $20 / 1M tokens
	"o3-deep-research":                 5.0,  // $10 / 1M tokens
	"o3-deep-research-2025-06-26":      5.0,  // $10 / 1M tokens
	"o4-mini":                          0.55, // $1.1 / 1M tokens
	"o4-mini-2025-04-16":               0.55, // $1.1 / 1M tokens
	"o4-mini-deep-research":            1.0,  // $2 / 1M tokens
	"o4-mini-deep-research-2025-06-26": 1.0,  // $2 / 1M tokens
	"gpt-4o-mini":                      0.075,
	"gpt-4o-mini-2024-07-18":           0.075,
	"gpt-4-turbo":                      5, // $0.01 / 1K tokens
	"gpt-4-turbo-2024-04-09":           5, // $0.01 / 1K tokens
	"gpt-4.5-preview":                  37.5,
	"gpt-4.5-preview-2025-02-27":       37.5,
	"gpt-5":                            0.625,
	"gpt-5-2025-08-07":                 0.625,
	"gpt-5-chat-latest":                0.625,
	"gpt-5-mini":                       0.125,
	"gpt-5-mini-2025-08-07":            0.125,
	"gpt-5-nano":                       0.025,
	"gpt-5-nano-2025-08-07":            0.025,
	"gpt-6-astra":                      5.0,  // $10 / 1M tokens; standard list price
	"gpt-5.5":                          2.5,  // $5 / 1M tokens
	"GPT-5.5":                          2.5,  // upstream case-preserving alias
	"gpt-5.6":                          2.5,  // alias of gpt-5.6-sol: non-promotional $5 / 1M tokens
	"gpt-5.6-sol":                      2.5,  // non-promotional $5 / 1M tokens; do not use OpenAI's temporary promotion
	"gpt-5.6-terra":                    1.0,  // $2 / 1M tokens
	"gpt-5.6-luna":                     0.1,  // $0.2 / 1M tokens
	"gpt-5.6-cyber":                    6.25, // $12.5 / 1M tokens
	"gpt-5.5-cyber":                    6.25, // $12.5 / 1M tokens
	"gpt-daybreak-blue-latest":         2.5,  // alias of gpt-5.6-sol list pricing
	"gpt-daybreak-red-latest":          6.25, // alias of gpt-5.6-cyber
	"gpt-realtime-2.1":                 2.0,  // $4 / 1M text tokens
	"gpt-realtime-2.1-mini":            0.3,  // $0.6 / 1M text tokens
	"gpt-realtime-2":                   2.0,  // $4 / 1M text tokens
	"gpt-realtime-1.5":                 2.0,  // $4 / 1M text tokens
	"gpt-realtime-mini":                0.3,  // $0.6 / 1M text tokens
	"gpt-realtime-mini-2025-10-06":     0.3,
	"gpt-realtime-mini-2025-12-15":     0.3,
	"gpt-realtime":                     2.0, // $4 / 1M text tokens
	"gpt-realtime-2025-08-28":          2.0,
	"gpt-audio-1.5":                    1.25, // $2.5 / 1M text tokens
	"gpt-audio-mini":                   0.3,  // $0.6 / 1M text tokens
	"gpt-audio-mini-2025-10-06":        0.3,
	"gpt-audio-mini-2025-12-15":        0.3,
	"gpt-audio":                        1.25, // $2.5 / 1M text tokens
	"gpt-audio-2025-08-28":             1.25,
	"gpt-5-codex":                      0.625,
	"gpt-5-codex-mini":                 0.125,
	"gpt-5.1":                          0.625,
	"gpt-5.1-codex":                    0.625,
	"gpt-5.1-codex-max":                0.625,
	"gpt-5.1-codex-mini":               0.125,
	"gpt-5.2":                          0.875,
	"gpt-5.2-codex":                    0.875,
	"gpt-5.2-pro":                      10.5,
	"gpt-5.3-codex":                    0.875,
	"gpt-5.3-codex-spark":              0.875,
	// Channel aliases use the published gpt-5.3-codex price.
	"gpt-5.3":                  0.875,
	"gpt-5.3-spark":            0.875,
	"gpt-5.4":                  1.25,
	"gpt-5.4-mini":             0.375,
	"gpt-5.4-nano":             0.1,
	"gpt-5.4-pro":              15.0,
	"gpt-5.5-pro":              15.0,
	"gpt-5-pro":                7.5,
	"gpt-5.4-openai-compact":   0.625,
	"gpt-5.5-openai-compact":   1.25,
	"gpt-image-1-mini":         1.0, // $2 / 1M tokens
	"gpt-image-1.5":            2.5,
	"gpt-image-2":              2.5,
	"kimi-k3":                  1.5,   // $3 / 1M tokens; https://platform.kimi.ai/docs/pricing/chat-k3
	"Kimi-K3":                  1.5,   // upstream case-preserving alias
	"kimi-k2.6":                0.475, // $0.95 / 1M tokens
	"kimi-k2.7-code":           0.475, // $0.95 / 1M tokens; https://platform.kimi.ai/docs/pricing/chat-k27-code.md
	"kimi-k2.7-code-highspeed": 0.95,  // $1.90 / 1M tokens; https://platform.kimi.ai/docs/pricing/chat-k27-code.md
	"claude-fable-5":           5.0,   // $10 / 1M tokens
	"claude-opus-5":            2.5,   // $5 / 1M tokens
	"claude-opus-5-thinking":   2.5,
	"claude-sonnet-5":          1.0, // $2 / 1M tokens; Anthropic now lists this as the standard price
	"claude-sonnet-5-thinking": 1.0,
	"claude-haiku-4-5":         0.5,
	"claude-sonnet-4":          1.5,
	"claude-sonnet-4-5":        1.5,
	"claude-sonnet-4-6":        1.5,
	"claude-opus-4-5":          2.5,
	"claude-opus-4-8":          2.5,
	"codex-auto-review":        2.5,
	// DeepSeek V4.1-Flash current price: $0.15 / 1M input tokens.
	"deepseek-flash":             0.15 / 2,
	"deepseek-ai/deepseek-flash": 0.15 / 2,
	// DeepSeek V4.1-Flash dated/channel aliases use the official
	// deepseek-flash price object.
	"deepseek-v4.1-flash":           0.15 / 2,
	"deepseek-v4.1-flash-0910":      0.15 / 2,
	"deepseek-v4-flash":             0.15 / 2,
	"deepseek-ai/deepseek-v4-flash": 0.15 / 2,
	// DeepSeek V4 Flash Vision Exp uses the same published token prices as
	// V4 Flash; its peak/off-peak schedule is maintained by billing_setting.
	"deepseek-v4-flash-vision-exp":             0.15 / 2,
	"deepseek-ai/deepseek-v4-flash-vision-exp": 0.15 / 2,
	"deepseek-v4-pro":                          0.2175, // $0.435 / 1M tokens
	"deepseek-ai/deepseek-v4-pro":              0.2175,
	// MiMo V2.5 overseas price: $0.14 / 1M input tokens.
	"mimo-v2.5":              0.07,
	"mimo-v2.5-pro":          0.435 / 2, // $0.435 / 1M tokens; https://mimo.mi.com/static/docs/price/pay-as-you-go.md
	"minimax-m3":             0.15,      // $0.30 / 1M current permanent price; official page marks it Permanent 50% off $0.60 list price
	"minimaxai/minimax-m3":   0.15,
	"minimax-m2":             0.15, // $0.30 / 1M tokens
	"minimax-m2.1":           0.15, // $0.30 / 1M tokens
	"minimax-m2.7":           0.15, // $0.30 / 1M tokens
	"minimax-m2.1-highspeed": 0.3,  // $0.60 / 1M tokens
	"minimax-m2p5-highspeed": 0.3,  // $0.60 / 1M tokens
	"minimax-m2.7-highspeed": 0.3,  // $0.60 / 1M tokens
	// LongCat-2.0 Chat API standard price: ¥5 / 1M input tokens.
	// The official page also lists a limited-time discounted price; this
	// project keeps the standard price and does not use that discount.
	"longcat-2.0": 5.0 / 1000 * RMB, // https://longcat.ai/platform/docs/Pricing/LongCat-2.0.html
	// Qwen3.8 official API prices are published in RMB per 1M tokens.
	"qwen3.8-max":    12.0 / 1000 * RMB,
	"qwen3.8-flash":  0.8 / 1000 * RMB,
	"qwen-3.8-flash": 0.8 / 1000 * RMB,
	"qwen3.8-27b":    3.0 / 1000 * RMB, // ¥3 / 1M tokens; https://www.qianwenai.com/models/qwen3.8-27b
	"qwen-3.8-27b":   3.0 / 1000 * RMB,
	"Qwen3.8-27B":    3.0 / 1000 * RMB,
	// Qwen3.6-35B-A3B official API price: ¥1.8 / 1M input tokens and
	// ¥10.8 / 1M output tokens. No cache price is published.
	"Qwen3.6-35B-A3B": 1.8 / 1000 * RMB,  // https://www.qianwenai.com/models/qwen3.6-35b-a3b
	"qwen3.7-max":     12.0 / 1000 * RMB, // ¥12 / 1M list price; https://www.qianwenai.com/models/qwen3.7-max
	"qwen-3.7-max":    12.0 / 1000 * RMB,
	"qwen3.7-plus":    2.0 / 1000 * RMB, // ¥2 / 1M list price; https://www.qianwenai.com/models/qwen3.7-plus
	"qwen-3.7-plus":   2.0 / 1000 * RMB,
	"agnes-1.5-flash": 0.035, // $0.07 / 1M tokens
	// Agnes currently lists both Flash text models at $0 for input and output.
	// agnes-2.0-flash is deprecated, but its current price remains free.
	"agnes-2.0-flash":                        0,
	"agnes-2.5-flash":                        0,
	"agnes-3.0-flash":                        0,     // current official price is $0 / 1M tokens
	"agnes-2.5-pro":                          0.225, // $0.45 / 1M tokens
	"agnes-2.5-pro-alpha":                    0.225, // $0.45 / 1M tokens
	"agnes-2.5-pro-beta":                     0.05,  // $0.10 / 1M tokens; https://wiki.agnes-ai.com/en/docs/pricing.md
	"gemini-3-flash":                         0.25,
	"gemini-3-flash-preview":                 0.25, // $0.50 / 1M tokens
	"gemini-3.7-flash":                       0.75, // $1.50 / 1M tokens standard price; do not use the 2026 promotion
	"gemini-3.6-flash":                       0.75, // $1.50 / 1M tokens standard price; do not use the 2026 promotion
	"gemini-3.5-flash":                       0.75, // $1.50 / 1M tokens
	"gemini-3.5-flash-lite":                  0.15, // $0.30 / 1M tokens
	"gemini-3-pro":                           1.0,
	"gemini-3-pro-preview":                   1.0,
	"gemini-3.1-pro":                         1.0,
	"gemini-3.1-pro-preview":                 1.0,
	"gemini-3.1-flash-lite":                  0.125,             // $0.25 / 1M tokens
	"gemini-3.1-flash-lite-preview":          0.125,             // $0.25 / 1M tokens
	"gemini-3-pro-image-preview":             1.0,               // $2 / 1M input tokens
	"gemini-3.1-flash-image-preview":         0.25,              // $0.50 / 1M input tokens
	"gemini-3.1-flash-image":                 0.25,              // $0.50 / 1M input tokens
	"gemini-3.1-flash-lite-image":            0.125,             // $0.25 / 1M input tokens
	"gemini-3.1-flash-lite-image-preview":    0.125,             // $0.25 / 1M input tokens
	"gemini-3-pro-image":                     1.0,               // $2 / 1M input tokens
	"gemini-omni-flash-preview":              0.75,              // $1.50 / 1M input tokens
	"gemini-omni-flash":                      0.75,              // official stable alias of the preview model
	"gemini-robotics-er-1.6-preview":         0.5,               // $1.00 / 1M input tokens
	"gemini-robotics-er-2-preview":           1.0,               // $2.00 / 1M input tokens
	"gemini-robotics-er-2-streaming-preview": 1.0,               // $2.00 / 1M input tokens
	"gemini-3.5-live-translate-preview":      1.75,              // $3.50 / 1M audio input tokens
	"gemini-3.1-flash-live-preview":          0.375,             // $0.75 / 1M text input tokens
	"gemini-3.1-flash-tts-preview":           0.5,               // $1.00 / 1M text input tokens
	"gemini-embedding-2":                     0.1,               // $0.20 / 1M text input tokens
	"gemini-embedding-2-preview":             0.1,               // compatibility alias of Embedding 2
	"gemma-4-31b-it":                         0.0,               // Free tier only; paid tier is unavailable
	"gemma-4-26b-a4b-it":                     0.0,               // Free tier only; paid tier is unavailable
	"glm-4.5":                                0.3,               // $0.60 / 1M tokens; https://docs.z.ai/guides/overview/pricing
	"glm-4.5-air":                            0.1,               // $0.20 / 1M tokens; https://docs.z.ai/guides/overview/pricing
	"glm-4.5-flash":                          0.0,               // Official page lists all token prices as Free
	"glm-4.6":                                0.3,               // $0.60 / 1M tokens; https://docs.z.ai/guides/overview/pricing
	"glm-4.7":                                0.3,               // $0.60 / 1M tokens; https://docs.z.ai/guides/overview/pricing
	"glm-4.7-flash":                          0.0,               // Official page lists all token prices as Free
	"glm-5.1":                                0.7,               // $1.40 / 1M tokens; https://docs.z.ai/guides/overview/pricing
	"glm-5.2":                                0.7,               // $1.40 / 1M tokens; https://docs.z.ai/guides/overview/pricing
	"glm-5.3":                                0.7,               // $1.40 / 1M tokens; https://docs.z.ai/guides/overview/pricing.md
	"glm-5.3-flash":                          0.15 / 2,          // $0.15 / 1M list price; ignore the limited-time promotion
	"step-3.7-flash":                         1.35 / 1000 * RMB, // ¥1.35 / 1M tokens; https://platform.stepfun.com/docs/zh/guides/pricing/details
	"step-explore":                           1.35 / 1000 * RMB, // Same price as step-3.7-flash; https://platform.stepfun.com/docs/zh/guides/models/step-explore
	// StepAudio 2.5 ASR is ¥0.15/hour. Transcription requests are normalized
	// to 1,000 audio tokens per minute, so the equivalent input price is
	// ¥2.5/1M tokens. Output transcript tokens are not billed separately.
	"stepaudio-2.5-asr": 2.5 / 1000 * RMB, // https://platform.stepfun.com/docs/zh/guides/pricing/details
	// Standard StepAudio 2.5 TTS is ¥5.8/10K input characters. The existing
	// audio speech path bills the input text units and does not bill audio
	// output when AudioCompletionRatio is zero; voice cloning is a separate
	// per-call charge and is intentionally not represented here.
	"stepaudio-2.5-tts":      5.8 / 10 * RMB,    // https://platform.stepfun.com/docs/zh/guides/pricing/details
	"stepaudio-2.5-chat":     10.0 / 1000 * RMB, // ¥10 / 1M tokens; https://platform.stepfun.com/docs/zh/guides/pricing/details
	"stepaudio-2.5-realtime": 10.0 / 1000 * RMB, // ¥10 / 1M tokens; https://platform.stepfun.com/docs/zh/guides/pricing/details
	// Spark-X2.5 official MaaS standard price: ¥3.2 / 1M input tokens.
	// The provider currently shows a separate discount; this keeps the
	// published original price and its explicit ¥0.48 cache-hit price.
	"spark-x2.5": 3.2 / 1000 * RMB, // https://maas.xfyun.cn/modelSquare/base/2624708664591360
	// xAI's published Grok 3 Mini API price is $0.30/$0.075/$0.50 per
	// million input/cache-hit/output tokens. The fast slug is an official
	// alias of the same pricing object.
	"grok-3-mini":                  0.15,  // $0.30 / 1M tokens; https://docs.x.ai/llms.txt
	"grok-3-mini-fast":             0.15,  // official alias of grok-3-mini
	"grok-4.3":                     0.625, // $1.25 / 1M tokens
	"grok-4.5":                     1.0,   // $2 / 1M tokens
	"grok-4.20":                    0.625, // $1.25 / 1M tokens
	"grok-4.20-0309-non-reasoning": 0.625,
	"grok-4.20-0309-reasoning":     0.625,
	"grok-4.20-multi-agent-0309":   0.625,
	"grok-4.6":                     1.0, // $2 / 1M tokens
	"grok-build-0.1":               0.5, // $1 / 1M tokens
	"grok-code-fast-1":             0.5, // official alias of grok-build-0.1
	// SenseNova Token Plan currently lists these models as free during the
	// public beta. Keep their zero output ratio explicit instead of relying on
	// the generic completion fallback.
	// Source: https://www.sensenova.cn/token-plan
	"sensenova-6.8-flash-lite": 0.0,
	"sensenova-u1-fast":        0.0,
	//"gpt-3.5-turbo-0301":           0.75, //deprecated
	"gpt-3.5-turbo":          0.25,
	"gpt-3.5-turbo-0613":     0.75,
	"gpt-3.5-turbo-16k":      1.5, // $0.003 / 1K tokens
	"gpt-3.5-turbo-16k-0613": 1.5,
	"gpt-3.5-turbo-instruct": 0.75, // $0.0015 / 1K tokens
	"gpt-3.5-turbo-1106":     0.5,  // $0.001 / 1K tokens
	"gpt-3.5-turbo-0125":     0.25,
	"babbage-002":            0.2, // $0.0004 / 1K tokens
	"davinci-002":            1,   // $0.002 / 1K tokens
	"text-ada-001":           0.2,
	"text-babbage-001":       0.25,
	"text-curie-001":         1,
	//"text-davinci-002":               10,
	//"text-davinci-003":               10,
	"text-davinci-edit-001":                     10,
	"code-davinci-edit-001":                     10,
	"whisper-1":                                 15,  // $0.006 / minute -> $0.006 / 150 words -> $0.006 / 200 tokens -> $0.03 / 1k tokens
	"tts-1":                                     7.5, // 1k characters -> $0.015
	"tts-1-1106":                                7.5, // 1k characters -> $0.015
	"tts-1-hd":                                  15,  // 1k characters -> $0.03
	"tts-1-hd-1106":                             15,  // 1k characters -> $0.03
	"davinci":                                   10,
	"curie":                                     10,
	"babbage":                                   10,
	"ada":                                       10,
	"text-embedding-3-small":                    0.01,
	"text-embedding-3-large":                    0.065,
	"text-embedding-ada-002":                    0.05,
	"text-search-ada-doc-001":                   10,
	"text-moderation-stable":                    0.1,
	"text-moderation-latest":                    0.1,
	"claude-3-haiku-20240307":                   0.125, // $0.25 / 1M tokens
	"claude-3-5-haiku-20241022":                 0.5,   // $1 / 1M tokens
	"claude-haiku-4-5-20251001":                 0.5,   // $1 / 1M tokens
	"claude-3-sonnet-20240229":                  1.5,   // $3 / 1M tokens
	"claude-3-5-sonnet-20240620":                1.5,
	"claude-3-5-sonnet-20241022":                1.5,
	"claude-3-7-sonnet-20250219":                1.5,
	"claude-3-7-sonnet-20250219-thinking":       1.5,
	"claude-sonnet-4-20250514":                  1.5,
	"claude-sonnet-4-5-20250929":                1.5,
	"claude-sonnet-4-5-20250929-thinking":       1.5,
	"claude-opus-4-5-20251101":                  2.5,
	"claude-opus-4-5-20251101-thinking":         2.5,
	"claude-opus-4-6":                           2.5,
	"claude-opus-4-6-max":                       2.5,
	"claude-opus-4-6-high":                      2.5,
	"claude-opus-4-6-medium":                    2.5,
	"claude-opus-4-6-low":                       2.5,
	"claude-opus-4-7":                           2.5,
	"claude-opus-4-7-max":                       2.5,
	"claude-opus-4-7-xhigh":                     2.5,
	"claude-opus-4-7-high":                      2.5,
	"claude-opus-4-7-medium":                    2.5,
	"claude-opus-4-7-low":                       2.5,
	"claude-3-opus-20240229":                    7.5, // $15 / 1M tokens
	"claude-opus-4-20250514":                    7.5,
	"claude-opus-4-1-20250805":                  7.5,
	"ERNIE-4.0-8K":                              0.120 * RMB,
	"ERNIE-3.5-8K":                              0.012 * RMB,
	"ERNIE-3.5-8K-0205":                         0.024 * RMB,
	"ERNIE-3.5-8K-1222":                         0.012 * RMB,
	"ERNIE-Bot-8K":                              0.024 * RMB,
	"ERNIE-3.5-4K-0205":                         0.012 * RMB,
	"ERNIE-Speed-8K":                            0.004 * RMB,
	"ERNIE-Speed-128K":                          0.004 * RMB,
	"ERNIE-Lite-8K-0922":                        0.008 * RMB,
	"ERNIE-Lite-8K-0308":                        0.003 * RMB,
	"ERNIE-Tiny-8K":                             0.001 * RMB,
	"BLOOMZ-7B":                                 0.004 * RMB,
	"Embedding-V1":                              0.002 * RMB,
	"bge-large-zh":                              0.002 * RMB,
	"bge-large-en":                              0.002 * RMB,
	"tao-8k":                                    0.002 * RMB,
	"PaLM-2":                                    1,
	"gemini-1.5-pro-latest":                     1.25, // $3.5 / 1M tokens
	"gemini-1.5-flash-latest":                   0.075,
	"gemini-2.0-flash":                          0.05,
	"gemini-2.5-pro-exp-03-25":                  0.625,
	"gemini-2.5-pro-preview-03-25":              0.625,
	"gemini-2.5-pro":                            0.625,
	"gemini-2.5-flash-preview-04-17":            0.075,
	"gemini-2.5-flash-preview-04-17-thinking":   0.075,
	"gemini-2.5-flash-preview-04-17-nothinking": 0.075,
	"gemini-2.5-flash-preview-05-20":            0.075,
	"gemini-2.5-flash-preview-05-20-thinking":   0.075,
	"gemini-2.5-flash-preview-05-20-nothinking": 0.075,
	"gemini-2.5-flash-thinking-*":               0.075, // 用于为后续所有2.5 flash thinking budget 模型设置默认倍率
	"gemini-2.5-pro-thinking-*":                 0.625, // 用于为后续所有2.5 pro thinking budget 模型设置默认倍率
	"gemini-2.5-flash-lite-preview-thinking-*":  0.05,
	"gemini-2.5-flash-lite-preview-06-17":       0.05,
	"gemini-2.5-flash":                          0.15,
	"gemini-robotics-er-1.5-preview":            0.15,
	"gemini-embedding-001":                      0.075,
	"text-embedding-004":                        0.001,
	"chatglm_turbo":                             0.3572,     // ￥0.005 / 1k tokens
	"chatglm_pro":                               0.7143,     // ￥0.01 / 1k tokens
	"chatglm_std":                               0.3572,     // ￥0.005 / 1k tokens
	"chatglm_lite":                              0.1429,     // ￥0.002 / 1k tokens
	"glm-4":                                     7.143,      // ￥0.1 / 1k tokens
	"glm-4v":                                    0.05 * RMB, // ￥0.05 / 1k tokens
	"glm-4-alltools":                            0.1 * RMB,  // ￥0.1 / 1k tokens
	"glm-3-turbo":                               0.3572,
	"glm-4-plus":                                0.05 * RMB,
	"glm-4-0520":                                0.1 * RMB,
	"glm-4-air":                                 0.001 * RMB,
	"glm-4-airx":                                0.01 * RMB,
	"glm-4-long":                                0.001 * RMB,
	"glm-4-flash":                               0,
	"glm-4v-plus":                               0.01 * RMB,
	"qwen-turbo":                                0.8572, // ￥0.012 / 1k tokens
	"qwen-plus":                                 10,     // ￥0.14 / 1k tokens
	"text-embedding-v1":                         0.05,   // ￥0.0007 / 1k tokens
	"SparkDesk-v1.1":                            1.2858, // ￥0.018 / 1k tokens
	"SparkDesk-v2.1":                            1.2858, // ￥0.018 / 1k tokens
	"SparkDesk-v3.1":                            1.2858, // ￥0.018 / 1k tokens
	"SparkDesk-v3.5":                            1.2858, // ￥0.018 / 1k tokens
	"SparkDesk-v4.0":                            1.2858,
	"360GPT_S2_V9":                              0.8572, // ¥0.012 / 1k tokens
	"360gpt-turbo":                              0.0858, // ¥0.0012 / 1k tokens
	"360gpt-turbo-responsibility-8k":            0.8572, // ¥0.012 / 1k tokens
	"360gpt-pro":                                0.8572, // ¥0.012 / 1k tokens
	"360gpt2-pro":                               0.8572, // ¥0.012 / 1k tokens
	"embedding-bert-512-v1":                     0.0715, // ¥0.001 / 1k tokens
	"embedding_s1_v1":                           0.0715, // ¥0.001 / 1k tokens
	"semantic_similarity_s1_v1":                 0.0715, // ¥0.001 / 1k tokens
	"hunyuan":                                   7.143,  // ¥0.1 / 1k tokens  // https://cloud.tencent.com/document/product/1729/97731#e0e6be58-60c8-469f-bdeb-6c264ce3b4d0
	// https://platform.lingyiwanwu.com/docs#-计费单元
	// 已经按照 7.2 来换算美元价格
	"yi-34b-chat-0205":       0.18,
	"yi-34b-chat-200k":       0.864,
	"yi-vl-plus":             0.432,
	"yi-large":               20.0 / 1000 * RMB,
	"yi-medium":              2.5 / 1000 * RMB,
	"yi-vision":              6.0 / 1000 * RMB,
	"yi-medium-200k":         12.0 / 1000 * RMB,
	"yi-spark":               1.0 / 1000 * RMB,
	"yi-large-rag":           25.0 / 1000 * RMB,
	"yi-large-turbo":         12.0 / 1000 * RMB,
	"yi-large-preview":       20.0 / 1000 * RMB,
	"yi-large-rag-preview":   25.0 / 1000 * RMB,
	"command":                0.5,
	"command-nightly":        0.5,
	"command-light":          0.5,
	"command-light-nightly":  0.5,
	"command-r":              0.25,
	"command-r-plus":         1.5,
	"command-r-08-2024":      0.075,
	"command-r-plus-08-2024": 1.25,
	"deepseek-chat":          0.27 / 2,
	"deepseek-coder":         0.27 / 2,
	"deepseek-reasoner":      0.55 / 2, // 0.55 / 1k tokens
	// Perplexity online 模型对搜索额外收费，有需要应自行调整，此处不计入搜索费用
	"llama-3-sonar-small-32k-chat":   0.2 / 1000 * USD,
	"llama-3-sonar-small-32k-online": 0.2 / 1000 * USD,
	"llama-3-sonar-large-32k-chat":   1 / 1000 * USD,
	"llama-3-sonar-large-32k-online": 1 / 1000 * USD,
	// grok
	"grok-3-beta": 1.5,
	// xAI's current grok-3-mini pricing object exposes these beta/latest
	// aliases at the same $0.30/$0.075/$0.50 per-million-token price.
	"grok-3-mini-beta":        0.15,
	"grok-3-mini-latest":      0.15,
	"grok-2":                  1,
	"grok-2-vision":           1,
	"grok-beta":               2.5,
	"grok-vision-beta":        2.5,
	"grok-3-fast-beta":        2.5,
	"grok-3-mini-fast-beta":   0.15,
	"grok-3-mini-fast-latest": 0.15,
	// Fireworks AI
	"deepseek-v3p1":             0.28,  // $0.56 / 1M tokens
	"deepseek-v3p2":             0.28,  // $0.56 / 1M tokens
	"glm-4p7":                   0.3,   // $0.60 / 1M tokens
	"glm-5":                     0.5,   // $1.00 / 1M tokens
	"gpt-oss-20b":               0.035, // $0.07 / 1M tokens
	"gpt-oss-120b":              0.075, // $0.15 / 1M tokens
	"kimi-k2-thinking":          0.3,   // $0.60 / 1M tokens
	"kimi-k2p5":                 0.3,   // $0.60 / 1M tokens
	"llama-v3p3-70b-instruct":   0.45,  // $0.90 / 1M tokens
	"minimax-m2p5":              0.15,  // $0.30 / 1M tokens
	"qwen3-8b":                  0.1,   // $0.20 / 1M tokens
	"qwen3-vl-30b-a3b-instruct": 0.075, // $0.15 / 1M tokens
	"qwen3-vl-30b-a3b-thinking": 0.075, // $0.15 / 1M tokens
	// submodel
	"NousResearch/Hermes-4-405B-FP8":          0.8,
	"Qwen/Qwen3-235B-A22B-Thinking-2507":      0.6,
	"Qwen/Qwen3-Coder-480B-A35B-Instruct-FP8": 0.8,
	"Qwen/Qwen3-235B-A22B-Instruct-2507":      0.3,
	"zai-org/GLM-4.5-FP8":                     0.8,
	"openai/gpt-oss-120b":                     0.5,
	"deepseek-ai/DeepSeek-R1-0528":            0.8,
	"deepseek-ai/DeepSeek-R1":                 0.5 / 2,
	"deepseek-ai/DeepSeek-V3-0324":            0.8,
	"deepseek-ai/DeepSeek-V3.1":               0.8,

	// SiliconFlow official serverless prices (USD per 1M tokens).
	// Qualified keys stay separate from other providers' prices.
	"deepseek-ai/DeepSeek-V3":              0.25 / 2,
	"deepseek-ai/DeepSeek-V3.1-Terminus":   0.27 / 2,
	"deepseek-ai/DeepSeek-V3.2":            0.27 / 2,
	"Qwen/Qwen3.6-35B-A3B":                 0.20 / 2,
	"Qwen/Qwen3.5-397B-A17B":               0.39 / 2,
	"Qwen/Qwen3.5-122B-A10B":               0.26 / 2,
	"Qwen/Qwen3.5-35B-A3B":                 0.24 / 2,
	"Qwen/Qwen3.5-27B":                     0.25 / 2,
	"Qwen/Qwen3.5-9B":                      0.10 / 2,
	"Qwen/Qwen3-VL-32B-Instruct":           0.20 / 2,
	"Qwen/Qwen3-VL-32B-Thinking":           0.20 / 2,
	"Qwen/Qwen3-VL-8B-Instruct":            0.18 / 2,
	"Qwen/Qwen3-VL-30B-A3B-Instruct":       0.29 / 2,
	"Qwen/Qwen3-VL-30B-A3B-Thinking":       0.29 / 2,
	"Qwen/Qwen3-Coder-30B-A3B-Instruct":    0.07 / 2,
	"Qwen/Qwen3-Coder-480B-A35B-Instruct":  0.25 / 2,
	"Qwen/Qwen3-30B-A3B-Instruct-2507":     0.09 / 2,
	"Qwen/Qwen3-30B-A3B-Thinking-2507":     0.09 / 2,
	"Qwen/Qwen3-32B":                       0.14 / 2,
	"Qwen/Qwen3-14B":                       0.07 / 2,
	"Qwen/Qwen3-8B":                        0.06 / 2,
	"Qwen/Qwen2.5-72B-Instruct":            0.59 / 2,
	"Qwen/Qwen2.5-72B-Instruct-128K":       0.59 / 2,
	"Qwen/Qwen2.5-7B-Instruct":             0.05 / 2,
	"Qwen/Qwen3-Embedding-8B":              0.04 / 2,
	"Qwen/Qwen3-Embedding-4B":              0.02 / 2,
	"Qwen/Qwen3-Embedding-0.6B":            0.01 / 2,
	"Qwen/Qwen3-Reranker-8B":               0.04 / 2,
	"Qwen/Qwen3-Reranker-4B":               0.02 / 2,
	"Qwen/Qwen3-Reranker-0.6B":             0.01 / 2,
	"baidu/ERNIE-4.5-300B-A47B":            0.28 / 2,
	"tencent/Hunyuan-A13B-Instruct":        0.14 / 2,
	"tencent/hunyuan-a13b-instruct":        0.14 / 2,
	"hunyuan-a13b-instruct":                0.14 / 2,
	"stepfun-ai/Step-3.5-Flash":            0.10 / 2,
	"inclusionAI/Ling-flash-2.0":           0.14 / 2,
	"ByteDance-Seed/Seed-OSS-36B-Instruct": 0.21 / 2,
}

var defaultModelPrice = map[string]float64{
	// Agnes currently lists these image/video models as free. Keep the entries
	// explicit so the fixed-price path reports the current $0 price.
	"agnes-image-2.0-flash": 0,
	"agnes-image-2.1-flash": 0,
	"agnes-image-2.5-flash": 0,
	"agnes-video-v2.0":      0,
	// Agnes Video 2.5 list and current price is $0.025 per output second.
	"agnes-video-2.5": 0.025,
	// Agnes currently lists Video 2.5 Flash as free for the current offer.
	"agnes-video-2.5-flash":               0,
	"grok-imagine-image":                  0.02,
	"grok-imagine-image-2026-03-02":       0.02,
	"grok-imagine-image-quality":          0.05,
	"grok-imagine-image-quality-20260403": 0.05,
	"grok-imagine-image-quality-latest":   0.05,
	"grok-imagine-image-pro":              0.05,
	"grok-imagine-image-2.0":              0.04,
	"grok-imagine":                        0.02,
	"grok-imagine-1":                      0.02,
	"grok-imagine-edit":                   0.02,
	// StepFun lists Step Image Edit 2 at ¥0.02 per generated image. ModelPrice
	// uses the internal USD quota unit, so convert from RMB using the project
	// exchange-rate constant instead of treating ¥0.02 as $0.02.
	"step-image-edit-2": 0.02 / USD2RMB,
	// SiliconFlow image and video prices are per generated item.
	"Qwen/Qwen-Image":                0.02,
	"Qwen/Qwen-Image-Edit":           0.04,
	"Qwen/Qwen-Image-Edit-2509":      0.04,
	"Wan-AI/Wan2.2-I2V-A14B":         0.29,
	"Wan-AI/Wan2.2-T2V-A14B":         0.29,
	"suno_music":                     0.1,
	"suno_lyrics":                    0.01,
	"dall-e-3":                       0.04,
	"imagen-3.0-generate-002":        0.03,
	"black-forest-labs/flux-1.1-pro": 0.04,
	"gpt-4-gizmo-*":                  0.1,
	"mj_video":                       0.8,
	"mj_imagine":                     0.1,
	"mj_edits":                       0.1,
	"mj_variation":                   0.1,
	"mj_reroll":                      0.1,
	"mj_blend":                       0.1,
	"mj_modal":                       0.1,
	"mj_zoom":                        0.1,
	"mj_shorten":                     0.1,
	"mj_high_variation":              0.1,
	"mj_low_variation":               0.1,
	"mj_pan":                         0.1,
	"mj_inpaint":                     0,
	"mj_custom_zoom":                 0,
	"mj_describe":                    0.05,
	"mj_upscale":                     0.05,
	"swap_face":                      0.05,
	"mj_upload":                      0.05,
	// OpenAI lists Sora 2 at $0.10/s (720p) and Sora 2 Pro at
	// $0.30/s (720p). Resolution-specific Sora 2 Pro prices are applied by
	// the task adaptor through the size billing ratio.
	"sora-2":                        0.1,
	"sora-2-pro":                    0.3,
	"gpt-4o-mini-tts":               0.3,
	"veo-3.0-generate-001":          0.4,
	"veo-3.0-fast-generate-001":     0.1,
	"veo-3.1-generate-preview":      0.4,
	"veo-3.1-fast-generate-preview": 0.1,
	"veo-3.1-lite-generate-preview": 0.05,
}

var defaultAudioRatio = map[string]float64{
	"stepaudio-2.5-asr":                       1,
	"stepaudio-2.5-tts":                       1,
	"gpt-4o-audio-preview":                    16,
	"gpt-4o-audio-preview-2025-06-03":         16,
	"gpt-4o-audio-preview-2024-12-17":         16,
	"gpt-4o-mini-audio-preview":               66.67,
	"gpt-4o-mini-audio-preview-2024-12-17":    66.67,
	"gpt-4o-realtime-preview":                 8,
	"gpt-4o-realtime-preview-2025-06-03":      8,
	"gpt-4o-realtime-preview-2024-12-17":      8,
	"gpt-4o-mini-realtime-preview":            16.67,
	"gpt-4o-mini-realtime-preview-2024-12-17": 16.67,
	"gpt-4o-mini-tts":                         25,
	"gpt-realtime-2.1":                        8,
	"gpt-realtime-2.1-mini":                   10.0 / 0.6,
	"gpt-realtime-2":                          8,
	"gpt-realtime-1.5":                        8,
	"gpt-realtime-mini":                       10.0 / 0.6,
	"gpt-realtime-mini-2025-10-06":            10.0 / 0.6,
	"gpt-realtime-mini-2025-12-15":            10.0 / 0.6,
	"gpt-realtime":                            8,
	"gpt-realtime-2025-08-28":                 8,
	"gpt-audio-1.5":                           32.0 / 2.5,
	"gpt-audio-mini":                          10.0 / 0.6,
	"gpt-audio-mini-2025-10-06":               10.0 / 0.6,
	"gpt-audio-mini-2025-12-15":               10.0 / 0.6,
	"gpt-audio":                               32.0 / 2.5,
	"gpt-audio-2025-08-28":                    32.0 / 2.5,
	"gemini-3.1-flash-lite":                   2,
	"gemini-3.1-flash-lite-preview":           2,
	"gemini-3.1-flash-live-preview":           4,
	"gemini-3.5-live-translate-preview":       1,
	"gemini-3.1-flash-tts-preview":            1,
	"gemini-robotics-er-1.6-preview":          2,
	"gemini-robotics-er-2-preview":            1,
	"gemini-robotics-er-2-streaming-preview":  1,
}

var defaultAudioCompletionRatio = map[string]float64{
	"stepaudio-2.5-asr":                       0,
	"stepaudio-2.5-tts":                       0,
	"gpt-4o-audio-preview":                    2,
	"gpt-4o-audio-preview-2025-06-03":         2,
	"gpt-4o-audio-preview-2024-12-17":         2,
	"gpt-4o-mini-audio-preview":               2,
	"gpt-4o-mini-audio-preview-2024-12-17":    2,
	"gpt-4o-realtime-preview":                 2,
	"gpt-4o-realtime-preview-2025-06-03":      2,
	"gpt-4o-realtime-preview-2024-12-17":      2,
	"gpt-4o-mini-realtime-preview":            2,
	"gpt-4o-mini-realtime-preview-2024-12-17": 2,
	"gpt-realtime-2.1":                        2,
	"gpt-realtime-2.1-mini":                   2,
	"gpt-realtime-2":                          2,
	"gpt-realtime-1.5":                        2,
	"gpt-realtime-mini":                       2,
	"gpt-realtime-mini-2025-10-06":            2,
	"gpt-realtime-mini-2025-12-15":            2,
	"gpt-realtime":                            2,
	"gpt-realtime-2025-08-28":                 2,
	"gpt-audio-1.5":                           2,
	"gpt-audio-mini":                          2,
	"gpt-audio-mini-2025-10-06":               2,
	"gpt-audio-mini-2025-12-15":               2,
	"gpt-audio":                               2,
	"gpt-audio-2025-08-28":                    2,
	"gpt-4o-realtime":                         2,
	"gpt-4o-mini-realtime":                    2,
	"gpt-4o-mini-tts":                         1,
	"gemini-3.1-flash-live-preview":           4,
	"gemini-3.5-live-translate-preview":       6,
	"gemini-3.1-flash-tts-preview":            20,
	"tts-1":                                   0,
	"tts-1-hd":                                0,
	"tts-1-1106":                              0,
	"tts-1-hd-1106":                           0,
}

var modelPriceMap = types.NewRWMap[string, float64]()
var modelRatioMap = types.NewRWMap[string, float64]()
var completionRatioMap = types.NewRWMap[string, float64]()

var defaultCompletionRatio = map[string]float64{
	"agnes-1.5-flash":                          0.15 / 0.07,
	"agnes-2.0-flash":                          0,
	"agnes-2.5-flash":                          0,
	"agnes-3.0-flash":                          0,
	"agnes-2.5-pro":                            2,
	"agnes-2.5-pro-alpha":                      2,
	"agnes-2.5-pro-beta":                       3,
	"gpt-6-astra":                              5, // $50 / $10 output/input
	"gpt-5.6":                                  6, // gpt-5.6-sol list output/input ratio: $30 / $5
	"gpt-5.6-sol":                              6, // list output/input ratio: $30 / $5
	"gpt-5.6-terra":                            6, // $12 / $2 output/input
	"gpt-5.6-luna":                             6, // $1.2 / $0.2 output/input
	"gpt-5.6-cyber":                            6, // $75 / $12.5 output/input
	"gpt-5.5-cyber":                            6, // $75 / $12.5 output/input
	"gpt-daybreak-blue-latest":                 6, // alias of gpt-5.6-sol list pricing
	"gpt-daybreak-red-latest":                  6, // alias of gpt-5.6-cyber
	"gpt-realtime-2.1":                         6, // $24 / $4 text output/input
	"gpt-realtime-2.1-mini":                    4, // $2.4 / $0.6 text output/input
	"gpt-realtime-2":                           6, // $24 / $4 text output/input
	"gpt-realtime-1.5":                         4, // $16 / $4 text output/input
	"gpt-realtime-mini":                        4, // $2.4 / $0.6 text output/input
	"gpt-realtime-mini-2025-10-06":             4,
	"gpt-realtime-mini-2025-12-15":             4,
	"gpt-realtime":                             4, // $16 / $4 text output/input
	"gpt-realtime-2025-08-28":                  4,
	"gpt-audio-1.5":                            4, // $10 / $2.5 text output/input
	"gpt-audio-mini":                           4, // $2.4 / $0.6 text output/input
	"gpt-audio-mini-2025-10-06":                4,
	"gpt-audio-mini-2025-12-15":                4,
	"gpt-audio":                                4, // $10 / $2.5 text output/input
	"gpt-audio-2025-08-28":                     4,
	"claude-fable-5":                           5,
	"claude-opus-5":                            5,
	"claude-opus-5-thinking":                   5,
	"claude-sonnet-5":                          5,
	"claude-sonnet-5-thinking":                 5,
	"kimi-k3":                                  5,
	"Kimi-K3":                                  5,
	"kimi-k2.6":                                4.0 / 0.95,
	"kimi-k2.7-code":                           4.0 / 0.95,
	"kimi-k2.7-code-highspeed":                 8.0 / 1.90,
	"deepseek-flash":                           4,
	"deepseek-ai/deepseek-flash":               4,
	"deepseek-v4.1-flash":                      4,
	"deepseek-v4.1-flash-0910":                 4,
	"deepseek-v4-flash":                        4,
	"deepseek-ai/deepseek-v4-flash":            4,
	"deepseek-v4-flash-vision-exp":             4,
	"deepseek-ai/deepseek-v4-flash-vision-exp": 4,
	"deepseek-v4-pro":                          2,
	"deepseek-ai/deepseek-v4-pro":              2,
	"mimo-v2.5":                                0.28 / 0.14,
	"mimo-v2.5-pro":                            0.87 / 0.435,
	"qwen3.7-max":                              36.0 / 12.0,
	"qwen-3.7-max":                             36.0 / 12.0,
	"qwen3.7-plus":                             8.0 / 2.0,
	"qwen-3.7-plus":                            8.0 / 2.0,
	"qwen3.8-27b":                              12.0 / 3.0,
	"qwen-3.8-27b":                             12.0 / 3.0,
	"Qwen3.8-27B":                              12.0 / 3.0,
	"Qwen3.6-35B-A3B":                          10.8 / 1.8,
	"longcat-2.0":                              20.0 / 5.0,
	"qwen3.8-max":                              36.0 / 12.0,
	"qwen3.8-flash":                            2.7 / 0.8,
	"qwen-3.8-flash":                           2.7 / 0.8,
	"step-3.7-flash":                           8.1 / 1.35,
	"step-explore":                             8.1 / 1.35,
	"stepaudio-2.5-asr":                        0,
	"stepaudio-2.5-tts":                        0,
	"stepaudio-2.5-chat":                       25.0 / 10.0,
	"stepaudio-2.5-realtime":                   70.0 / 10.0,
	"spark-x2.5":                               12.0 / 3.2,
	"gemini-3-flash":                           6,
	"gemini-3-flash-preview":                   6,
	"gemini-3.7-flash":                         5,
	"gemini-3.6-flash":                         5,
	"gemini-3.5-flash":                         6,
	"gemini-3.5-flash-lite":                    2.5 / 0.3,
	"gemini-3-pro":                             6,
	"gemini-3.1-flash-lite-preview":            6,
	"gemini-3.1-pro":                           6,
	"gemini-3.1-pro-preview":                   6,
	"gemini-omni-flash-preview":                6,
	"gemini-3.1-flash-live-preview":            6,
	"gemini-3.5-live-translate-preview":        6,
	"gemini-3.1-flash-image-preview":           6,
	"gemini-3.1-flash-lite-image":              6,
	"gemini-3.1-flash-lite-image-preview":      6,
	"gemini-3-pro-image-preview":               6,
	"gemini-3.1-flash-tts-preview":             20,
	"gemini-robotics-er-1.6-preview":           5,
	"gemini-robotics-er-2-preview":             5,
	"gemini-robotics-er-2-streaming-preview":   5,
	"gemma-4-31b-it":                           1,
	"gemma-4-26b-a4b-it":                       1,
	"grok-3-mini":                              5.0 / 3.0,
	"grok-3-mini-beta":                         5.0 / 3.0,
	"grok-3-mini-latest":                       5.0 / 3.0,
	"grok-3-mini-fast":                         5.0 / 3.0,
	"grok-3-mini-fast-beta":                    5.0 / 3.0,
	"grok-3-mini-fast-latest":                  5.0 / 3.0,
	"grok-4.3":                                 2,
	"grok-4.5":                                 3,
	"grok-4.20":                                2,
	"grok-4.20-0309-non-reasoning":             2,
	"grok-4.20-0309-reasoning":                 2,
	"grok-4.20-multi-agent-0309":               2,
	"grok-4.6":                                 3,
	"grok-build-0.1":                           2,
	"grok-code-fast-1":                         2,
	"sensenova-6.8-flash-lite":                 0,
	"sensenova-u1-fast":                        0,
	"gpt-5-codex":                              8,
	"gpt-5-codex-mini":                         8,
	"gpt-5.1":                                  8,
	"gpt-5.1-codex":                            8,
	"gpt-5.1-codex-max":                        8,
	"gpt-5.1-codex-mini":                       8,
	"gpt-5.2":                                  8,
	"gpt-5.2-codex":                            8,
	"gpt-5.2-pro":                              8,
	"gpt-5.3-codex":                            8,
	"gpt-5.3-codex-spark":                      8,
	"gpt-5.3":                                  8,
	"gpt-5.3-spark":                            8,
	"gpt-5.4":                                  6,
	"gpt-5.4-mini":                             6,
	"gpt-5.4-nano":                             6.25,
	"gpt-5.4-pro":                              6,
	"gpt-5.5":                                  6,
	"GPT-5.5":                                  6,
	// codex-auto-review is a project-local alias whose production pricing
	// snapshot matches the GPT-5.6 standard tier.
	"codex-auto-review":         6,
	"gpt-5.5-pro":               6,
	"gpt-5-pro":                 8,
	"minimax-m3":                4,
	"minimaxai/minimax-m3":      4,
	"minimax-m2":                4,
	"minimax-m2.1":              4,
	"minimax-m2.7":              4,
	"minimax-m2.1-highspeed":    4,
	"minimax-m2p5":              4,
	"minimax-m2p5-highspeed":    4,
	"minimax-m2.7-highspeed":    4,
	"deepseek-v3p1":             3,
	"deepseek-v3p2":             3,
	"glm-4p7":                   11.0 / 3.0,
	"glm-5":                     3.2,
	"glm-5.1":                   4.4 / 1.4,
	"glm-5.2":                   4.4 / 1.4,
	"glm-5.3":                   4.4 / 1.4,
	"glm-5.3-flash":             0.50 / 0.15,
	"glm-4.5":                   2.20 / 0.60,
	"glm-4.5-air":               1.10 / 0.20,
	"glm-4.5-flash":             0,
	"glm-4.6":                   2.20 / 0.60,
	"glm-4.7":                   2.20 / 0.60,
	"glm-4.7-flash":             0,
	"gpt-4-gizmo-*":             2,
	"gpt-4o-gizmo-*":            3,
	"gpt-4-all":                 2,
	"gpt-image-1-mini":          4,
	"gpt-image-1":               8,
	"gpt-image-1.5":             2,
	"gpt-image-2":               6,
	"chatgpt-image-latest":      2,
	"gpt-oss-20b":               30.0 / 7.0,
	"gpt-oss-120b":              4,
	"kimi-k2-thinking":          25.0 / 6.0,
	"kimi-k2p5":                 5,
	"llama-v3p3-70b-instruct":   1,
	"qwen3-8b":                  1,
	"qwen3-vl-30b-a3b-instruct": 4,
	"qwen3-vl-30b-a3b-thinking": 4,

	// SiliconFlow official output prices for qualified model paths.
	"deepseek-ai/DeepSeek-R1":              2.18 / 0.5,
	"deepseek-ai/DeepSeek-V3":              1.0 / 0.25,
	"deepseek-ai/DeepSeek-V3.1-Terminus":   1.0 / 0.27,
	"deepseek-ai/DeepSeek-V3.2":            0.42 / 0.27,
	"Qwen/Qwen3.6-35B-A3B":                 1.60 / 0.20,
	"Qwen/Qwen3.5-397B-A17B":               2.34 / 0.39,
	"Qwen/Qwen3.5-122B-A10B":               2.08 / 0.26,
	"Qwen/Qwen3.5-35B-A3B":                 1.80 / 0.24,
	"Qwen/Qwen3.5-27B":                     2.00 / 0.25,
	"Qwen/Qwen3.5-9B":                      0.15 / 0.10,
	"Qwen/Qwen3-VL-32B-Instruct":           0.60 / 0.20,
	"Qwen/Qwen3-VL-32B-Thinking":           1.50 / 0.20,
	"Qwen/Qwen3-VL-8B-Instruct":            0.68 / 0.18,
	"Qwen/Qwen3-VL-30B-A3B-Instruct":       1.00 / 0.29,
	"Qwen/Qwen3-VL-30B-A3B-Thinking":       1.00 / 0.29,
	"Qwen/Qwen3-Coder-30B-A3B-Instruct":    0.28 / 0.07,
	"Qwen/Qwen3-Coder-480B-A35B-Instruct":  1.00 / 0.25,
	"Qwen/Qwen3-30B-A3B-Instruct-2507":     0.30 / 0.09,
	"Qwen/Qwen3-30B-A3B-Thinking-2507":     0.30 / 0.09,
	"Qwen/Qwen3-32B":                       0.57 / 0.14,
	"Qwen/Qwen3-14B":                       0.28 / 0.07,
	"Qwen/Qwen3-8B":                        0.06 / 0.06,
	"Qwen/Qwen2.5-72B-Instruct":            0.59 / 0.59,
	"Qwen/Qwen2.5-72B-Instruct-128K":       0.59 / 0.59,
	"Qwen/Qwen2.5-7B-Instruct":             0.05 / 0.05,
	"Qwen/Qwen3-Embedding-8B":              1,
	"Qwen/Qwen3-Embedding-4B":              1,
	"Qwen/Qwen3-Embedding-0.6B":            1,
	"Qwen/Qwen3-Reranker-8B":               1,
	"Qwen/Qwen3-Reranker-4B":               1,
	"Qwen/Qwen3-Reranker-0.6B":             1,
	"baidu/ERNIE-4.5-300B-A47B":            1.10 / 0.28,
	"tencent/Hunyuan-A13B-Instruct":        0.57 / 0.14,
	"tencent/hunyuan-a13b-instruct":        0.57 / 0.14,
	"hunyuan-a13b-instruct":                0.57 / 0.14,
	"stepfun-ai/Step-3.5-Flash":            0.30 / 0.10,
	"inclusionAI/Ling-flash-2.0":           0.57 / 0.14,
	"ByteDance-Seed/Seed-OSS-36B-Instruct": 0.57 / 0.21,
}

// InitRatioSettings initializes all model related settings maps
func InitRatioSettings() {
	modelPriceMap.AddAll(defaultModelPrice)
	modelRatioMap.AddAll(defaultModelRatio)
	completionRatioMap.AddAll(defaultCompletionRatio)
	cacheRatioMap.AddAll(defaultCacheRatio)
	createCacheRatioMap.AddAll(defaultCreateCacheRatio)
	imageRatioMap.AddAll(defaultImageRatio)
	audioRatioMap.AddAll(defaultAudioRatio)
	audioCompletionRatioMap.AddAll(defaultAudioCompletionRatio)
}

func GetModelPriceMap() map[string]float64 {
	return modelPriceMap.ReadAll()
}

func ModelPrice2JSONString() string {
	return modelPriceMap.MarshalJSONString()
}

func UpdateModelPriceByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(modelPriceMap, jsonStr, InvalidateExposedDataCache)
}

func getCompactAwareMapValue(source *types.RWMap[string, float64], name string) (float64, bool, string) {
	for _, candidate := range modelNameCandidates(name) {
		if value, matchName, ok := getModelMapValue(source, candidate); ok {
			return value, true, matchName
		}
		baseName, isCompact := CompactBaseModelName(strings.ToLower(candidate))
		if !isCompact {
			continue
		}
		if value, matchName, ok := getModelMapValue(source, CompactWildcardModelKey); ok {
			return value, true, matchName
		}
		if value, matchName, ok := getModelMapValue(source, baseName); ok {
			return value, true, matchName
		}
	}
	return 0, false, FormatMatchingModelName(name)
}

// getModelMapValue keeps exact configured keys preferred while allowing model
// names from providers that differ only in casing to reuse the same pricing.
func getModelMapValue(source *types.RWMap[string, float64], candidate string) (float64, string, bool) {
	if value, ok := source.Get(candidate); ok {
		return value, candidate, true
	}
	for configuredName, value := range source.ReadAll() {
		if strings.EqualFold(configuredName, candidate) {
			return value, configuredName, true
		}
	}
	return 0, "", false
}

// GetModelPrice 返回模型的价格，如果模型不存在则返回-1，false
func GetModelPrice(name string, printErr bool) (float64, bool) {
	if price, ok, _ := getCompactAwareMapValue(modelPriceMap, name); ok {
		return price, true
	}

	if printErr {
		common.SysError("model price not found: " + name)
	}
	return -1, false
}

func UpdateModelRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(modelRatioMap, jsonStr, InvalidateExposedDataCache)
}

// 处理带有思考预算的模型名称，方便统一定价
func handleThinkingBudgetModel(name, prefix, wildcard string) string {
	if strings.HasPrefix(name, prefix) && strings.Contains(name, "-thinking-") {
		return wildcard
	}
	return name
}

func GetModelRatio(name string) (float64, bool, string) {
	ratio, ok, matchName := getCompactAwareMapValue(modelRatioMap, name)
	if !ok {
		return 37.5, operation_setting.SelfUseModeEnabled, FormatMatchingModelName(name)
	}
	return ratio, true, matchName
}

func GetConfiguredModelRatio(name string) (float64, bool, string) {
	if ratio, ok, matchName := getCompactAwareMapValue(modelRatioMap, name); ok {
		return ratio, true, matchName
	}

	return 0, false, FormatMatchingModelName(name)
}

func DefaultModelRatio2JSONString() string {
	jsonBytes, err := common.Marshal(defaultModelRatio)
	if err != nil {
		common.SysError("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func GetDefaultModelRatioMap() map[string]float64 {
	return defaultModelRatio
}

func GetDefaultModelPriceMap() map[string]float64 {
	return defaultModelPrice
}

func CompletionRatio2JSONString() string {
	return completionRatioMap.MarshalJSONString()
}

func UpdateCompletionRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(completionRatioMap, jsonStr, InvalidateExposedDataCache)
}

func GetCompletionRatio(name string) float64 {
	return getCompletionRatioInfo(name).Ratio
}

type CompletionRatioInfo struct {
	Ratio  float64 `json:"ratio"`
	Locked bool    `json:"locked"`
}

func GetCompletionRatioInfo(name string) CompletionRatioInfo {
	return getCompletionRatioInfo(name)
}

func getCompletionRatioInfo(name string) CompletionRatioInfo {
	// Keep the upstream behavior for explicitly qualified provider aliases: a
	// configured path such as "provider/model" may intentionally override the
	// generic model price. Unqualified official models still honor hard locks.
	if ratio, ok := getQualifiedCompletionRatio(name); ok {
		return CompletionRatioInfo{Ratio: ratio}
	}

	hardcodedName := normalizeControlVariantModelName(name)
	hardcodedRatio, locked := getHardcodedCompletionModelRatio(hardcodedName)
	if locked {
		return CompletionRatioInfo{Ratio: hardcodedRatio, Locked: true}
	}
	if ratio, ok, _ := getCompactAwareMapValue(completionRatioMap, name); ok {
		return CompletionRatioInfo{Ratio: ratio}
	}
	return CompletionRatioInfo{Ratio: hardcodedRatio}
}

func getQualifiedCompletionRatio(name string) (float64, bool) {
	for _, candidate := range modelNameCandidates(name) {
		if !strings.Contains(candidate, "/") {
			continue
		}
		if ratio, _, ok := getModelMapValue(completionRatioMap, candidate); ok {
			return ratio, true
		}
	}
	return 0, false
}

func getHardcodedCompletionModelRatio(name string) (float64, bool) {

	isReservedModel := strings.HasSuffix(name, "-all") || strings.HasSuffix(name, "-gizmo-*")
	if isReservedModel {
		return 2, false
	}

	if strings.HasPrefix(name, "gpt-") {
		if strings.HasPrefix(name, "gpt-4o") {
			if name == "gpt-4o-2024-05-13" {
				return 3, true
			}
			if strings.HasPrefix(name, "gpt-4o-mini-tts") {
				return 20, false
			}
			return 4, false
		}
		// gpt-5 匹配
		if strings.HasPrefix(name, "gpt-5") {
			if !strings.Contains(name, ".") {
				return 8, true
			}
			if strings.HasPrefix(name, "gpt-5.4") {
				if strings.HasPrefix(name, "gpt-5.4-nano") {
					return 6.25, true
				}
				return 6, true
			}
			// gpt-5.5 and later models are unlocked
			return 6, false
		}
		// gpt-4.5-preview匹配
		if strings.HasPrefix(name, "gpt-4.5-preview") {
			return 2, true
		}
		if strings.HasPrefix(name, "gpt-4-turbo") || strings.HasSuffix(name, "gpt-4-1106") || strings.HasSuffix(name, "gpt-4-1105") {
			return 3, true
		}
		// 没有特殊标记的 gpt-4 模型默认倍率为 2
		return 2, false
	}
	if strings.HasPrefix(name, "o1") || strings.HasPrefix(name, "o3") {
		return 4, true
	}
	if name == "chatgpt-4o-latest" {
		return 3, true
	}

	if strings.Contains(name, "claude-3") {
		return 5, true
	} else if strings.Contains(name, "claude-sonnet-4") || strings.Contains(name, "claude-opus-4") || strings.Contains(name, "claude-haiku-4") {
		return 5, true
	}

	if strings.HasPrefix(name, "gpt-3.5") {
		if name == "gpt-3.5-turbo" || strings.HasSuffix(name, "0125") {
			// https://openai.com/blog/new-embedding-models-and-api-updates
			// Updated GPT-3.5 Turbo model and lower pricing
			return 3, true
		}
		if strings.HasSuffix(name, "1106") {
			return 2, true
		}
		return 4.0 / 3.0, true
	}
	if strings.HasPrefix(name, "mistral-") {
		return 3, true
	}
	if strings.HasPrefix(name, "gemini-") {
		if strings.HasPrefix(name, "gemini-1.5") {
			return 4, true
		} else if strings.HasPrefix(name, "gemini-2.0") {
			return 4, true
		} else if strings.HasPrefix(name, "gemini-2.5-pro") { // 移除preview来增加兼容性，这里假设正式版的倍率和preview一致
			return 8, false
		} else if strings.HasPrefix(name, "gemini-2.5-flash") { // 处理不同的flash模型倍率
			if strings.HasPrefix(name, "gemini-2.5-flash-preview") {
				if strings.HasSuffix(name, "-nothinking") {
					return 4, false
				}
				return 3.5 / 0.15, false
			}
			if strings.HasPrefix(name, "gemini-2.5-flash-lite") {
				return 4, false
			}
			return 2.5 / 0.3, false
		} else if strings.HasPrefix(name, "gemini-robotics-er-1.5") {
			return 2.5 / 0.3, false
		} else if strings.HasPrefix(name, "gemini-3-pro") {
			if strings.HasPrefix(name, "gemini-3-pro-image") {
				return 60, false
			}
			return 6, false
		}
		return 4, false
	}
	if strings.HasPrefix(name, "command") {
		switch name {
		case "command-r":
			return 3, true
		case "command-r-plus":
			return 5, true
		case "command-r-08-2024":
			return 4, true
		case "command-r-plus-08-2024":
			return 4, true
		default:
			return 4, false
		}
	}
	// hint 只给官方上4倍率，由于开源模型供应商自行定价，不对其进行补全倍率进行强制对齐
	if strings.HasPrefix(name, "ERNIE-Speed-") {
		return 2, true
	} else if strings.HasPrefix(name, "ERNIE-Lite-") {
		return 2, true
	} else if strings.HasPrefix(name, "ERNIE-Character") {
		return 2, true
	} else if strings.HasPrefix(name, "ERNIE-Functions") {
		return 2, true
	}
	switch name {
	case "llama2-70b-4096":
		return 0.8 / 0.64, true
	case "llama3-8b-8192":
		return 2, true
	case "llama3-70b-8192":
		return 0.79 / 0.59, true
	}
	return 1, false
}

func GetAudioRatio(name string) float64 {
	if ratio, ok, _ := getCompactAwareMapValue(audioRatioMap, name); ok {
		return ratio
	}
	return 1
}

func GetAudioCompletionRatio(name string) float64 {
	if ratio, ok, _ := getCompactAwareMapValue(audioCompletionRatioMap, name); ok {
		return ratio
	}
	return 1
}

func ContainsAudioRatio(name string) bool {
	_, ok, _ := getCompactAwareMapValue(audioRatioMap, name)
	return ok
}

func ContainsAudioCompletionRatio(name string) bool {
	_, ok, _ := getCompactAwareMapValue(audioCompletionRatioMap, name)
	return ok
}

func ModelRatio2JSONString() string {
	return modelRatioMap.MarshalJSONString()
}

var defaultImageRatio = map[string]float64{
	"gpt-image-1-mini":             1.25, // $2.50 image input / $2 text input
	"gpt-image-1":                  2.0,  // $10 image input / $5 text input
	"gpt-image-1.5":                1.6,  // $8 image input / $5 text input
	"gpt-image-2":                  1.6,  // $8 image input / $5 text input
	"chatgpt-image-latest":         1.6,  // $8 image input / $5 text input
	"gpt-realtime-2.1":             1.25, // $5 image input / $4 text input
	"gpt-realtime-2.1-mini":        0.8 / 0.6,
	"gpt-realtime-2":               1.25,
	"gpt-realtime-1.5":             1.25,
	"gpt-realtime-mini":            0.8 / 0.6,
	"gpt-realtime-mini-2025-10-06": 0.8 / 0.6,
	"gpt-realtime-mini-2025-12-15": 0.8 / 0.6,
	"gpt-realtime":                 1.25,
	"gpt-realtime-2025-08-28":      1.25,
}
var imageRatioMap = types.NewRWMap[string, float64]()
var audioRatioMap = types.NewRWMap[string, float64]()
var audioCompletionRatioMap = types.NewRWMap[string, float64]()

func ImageRatio2JSONString() string {
	return imageRatioMap.MarshalJSONString()
}

func UpdateImageRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(imageRatioMap, jsonStr)
}

func GetImageRatio(name string) (float64, bool) {
	ratio, ok, _ := getCompactAwareMapValue(imageRatioMap, name)
	if !ok {
		return 1, false // Default to 1 if not found
	}
	return ratio, true
}

func AudioRatio2JSONString() string {
	return audioRatioMap.MarshalJSONString()
}

func UpdateAudioRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(audioRatioMap, jsonStr, InvalidateExposedDataCache)
}

func AudioCompletionRatio2JSONString() string {
	return audioCompletionRatioMap.MarshalJSONString()
}

func UpdateAudioCompletionRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(audioCompletionRatioMap, jsonStr, InvalidateExposedDataCache)
}

func GetModelRatioCopy() map[string]float64 {
	return modelRatioMap.ReadAll()
}

func GetModelPriceCopy() map[string]float64 {
	return modelPriceMap.ReadAll()
}

func GetCompletionRatioCopy() map[string]float64 {
	return completionRatioMap.ReadAll()
}

func GetImageRatioCopy() map[string]float64 {
	return imageRatioMap.ReadAll()
}

func GetAudioRatioCopy() map[string]float64 {
	return audioRatioMap.ReadAll()
}

func GetAudioCompletionRatioCopy() map[string]float64 {
	return audioCompletionRatioMap.ReadAll()
}

// result: 倍率or价格， usePrice， exist
func GetModelRatioOrPrice(model string) (float64, bool, bool) { // price or ratio
	price, usePrice := GetModelPrice(model, false)
	if usePrice {
		return price, true, true
	}
	modelRatio, success, _ := GetModelRatio(model)
	if success {
		return modelRatio, false, true
	}
	return 37.5, false, false
}
