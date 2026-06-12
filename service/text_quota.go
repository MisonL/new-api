package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type textQuotaSummary struct {
	PromptTokens             int
	CompletionTokens         int
	TotalTokens              int
	CacheTokens              int
	CacheCreationTokens      int
	CacheCreationTokens5m    int
	CacheCreationTokens1h    int
	ImageTokens              int
	AudioTokens              int
	ModelName                string
	TokenName                string
	UseTimeSeconds           int64
	CompletionRatio          float64
	CacheRatio               float64
	ImageRatio               float64
	ModelRatio               float64
	GroupRatio               float64
	ModelPrice               float64
	CacheCreationRatio       float64
	CacheCreationRatio5m     float64
	CacheCreationRatio1h     float64
	Quota                    int
	IsClaudeUsageSemantic    bool
	UsageSemantic            string
	WebSearchPrice           float64
	WebSearchCallCount       int
	ClaudeWebSearchPrice     float64
	ClaudeWebSearchCallCount int
	FileSearchPrice          float64
	FileSearchCallCount      int
	AudioInputPrice          float64
	ImageGenerationCallPrice float64
	ToolCallSurchargeQuota   decimal.Decimal
}

func cacheWriteTokensTotal(summary textQuotaSummary) int {
	if summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0 {
		splitCacheWriteTokens := summary.CacheCreationTokens5m + summary.CacheCreationTokens1h
		if summary.CacheCreationTokens > splitCacheWriteTokens {
			return summary.CacheCreationTokens
		}
		return splitCacheWriteTokens
	}
	return summary.CacheCreationTokens
}

func isLegacyClaudeDerivedOpenAIUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) bool {
	if relayInfo == nil || usage == nil {
		return false
	}
	if relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return false
	}
	if usage.UsageSource != "" || usage.UsageSemantic != "" {
		return false
	}
	return usage.ClaudeCacheCreation5mTokens > 0 || usage.ClaudeCacheCreation1hTokens > 0
}

func calculateTextToolCallSurcharge(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary *textQuotaSummary) decimal.Decimal {
	dGroupRatio := decimal.NewFromFloat(summary.GroupRatio)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	var surcharge decimal.Decimal

	if relayInfo.ResponsesUsageInfo != nil {
		if webSearchTool, exists := relayInfo.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool.CallCount > 0 {
			summary.WebSearchCallCount = webSearchTool.CallCount
			summary.WebSearchPrice = operation_setting.GetToolPriceForModel("web_search_preview", summary.ModelName)
			surcharge = surcharge.Add(decimal.NewFromFloat(summary.WebSearchPrice).
				Mul(decimal.NewFromInt(int64(webSearchTool.CallCount))).
				Div(decimal.NewFromInt(1000)).
				Mul(dGroupRatio).
				Mul(dQuotaPerUnit))
		}
	} else if strings.HasSuffix(summary.ModelName, "search-preview") {
		summary.WebSearchCallCount = 1
		summary.WebSearchPrice = operation_setting.GetToolPriceForModel("web_search_preview", summary.ModelName)
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.WebSearchPrice).
			Div(decimal.NewFromInt(1000)).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit))
	}

	summary.ClaudeWebSearchCallCount = ctx.GetInt("claude_web_search_requests")
	if summary.ClaudeWebSearchCallCount > 0 {
		summary.ClaudeWebSearchPrice = operation_setting.GetToolPrice("web_search")
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.ClaudeWebSearchPrice).
			Div(decimal.NewFromInt(1000)).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit).
			Mul(decimal.NewFromInt(int64(summary.ClaudeWebSearchCallCount))))
	}

	if relayInfo.ResponsesUsageInfo != nil {
		if fileSearchTool, exists := relayInfo.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch]; exists && fileSearchTool.CallCount > 0 {
			summary.FileSearchCallCount = fileSearchTool.CallCount
			summary.FileSearchPrice = operation_setting.GetToolPrice("file_search")
			surcharge = surcharge.Add(decimal.NewFromFloat(summary.FileSearchPrice).
				Mul(decimal.NewFromInt(int64(fileSearchTool.CallCount))).
				Div(decimal.NewFromInt(1000)).
				Mul(dGroupRatio).
				Mul(dQuotaPerUnit))
		}
	}

	if ctx.GetBool("image_generation_call") {
		summary.ImageGenerationCallPrice = operation_setting.GetGPTImage1PriceOnceCall(ctx.GetString("image_generation_call_quality"), ctx.GetString("image_generation_call_size"))
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.ImageGenerationCallPrice).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit))
	}

	return surcharge
}

func composeTieredTextQuota(relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, tieredQuota int, tieredResult *billingexpr.TieredResult) int {
	if summary.ToolCallSurchargeQuota.IsZero() {
		return tieredQuota
	}

	if tieredResult != nil {
		if snap := relayInfo.TieredBillingSnapshot; snap != nil {
			return int(decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).
				Mul(decimal.NewFromFloat(snap.GroupRatio)).
				Add(summary.ToolCallSurchargeQuota).
				Round(0).
				IntPart())
		}
	}

	return tieredQuota + int(summary.ToolCallSurchargeQuota.Round(0).IntPart())
}

func calculateTextQuotaSummary(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage) textQuotaSummary {
	summary := textQuotaSummary{
		ModelName:            relayInfo.OriginModelName,
		TokenName:            ctx.GetString("token_name"),
		UseTimeSeconds:       time.Now().Unix() - relayInfo.StartTime.Unix(),
		CompletionRatio:      relayInfo.PriceData.CompletionRatio,
		CacheRatio:           relayInfo.PriceData.CacheRatio,
		ImageRatio:           relayInfo.PriceData.ImageRatio,
		ModelRatio:           relayInfo.PriceData.ModelRatio,
		GroupRatio:           relayInfo.PriceData.GroupRatioInfo.GroupRatio,
		ModelPrice:           relayInfo.PriceData.ModelPrice,
		CacheCreationRatio:   relayInfo.PriceData.CacheCreationRatio,
		CacheCreationRatio5m: relayInfo.PriceData.CacheCreation5mRatio,
		CacheCreationRatio1h: relayInfo.PriceData.CacheCreation1hRatio,
		UsageSemantic:        usageSemanticFromUsage(relayInfo, usage),
	}
	summary.IsClaudeUsageSemantic = summary.UsageSemantic == "anthropic"

	if usage == nil {
		usage = &dto.Usage{
			PromptTokens:     relayInfo.GetEstimatePromptTokens(),
			CompletionTokens: 0,
			TotalTokens:      relayInfo.GetEstimatePromptTokens(),
		}
	}

	summary.PromptTokens = usage.PromptTokens
	summary.CompletionTokens = usage.CompletionTokens
	summary.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	summary.CacheTokens = usage.PromptTokensDetails.CachedTokens
	summary.CacheCreationTokens = usage.PromptTokensDetails.CachedCreationTokens
	summary.CacheCreationTokens5m = usage.ClaudeCacheCreation5mTokens
	summary.CacheCreationTokens1h = usage.ClaudeCacheCreation1hTokens
	summary.ImageTokens = usage.PromptTokensDetails.ImageTokens
	summary.AudioTokens = usage.PromptTokensDetails.AudioTokens
	legacyClaudeDerived := isLegacyClaudeDerivedOpenAIUsage(relayInfo, usage)
	isOpenRouterClaudeBilling := relayInfo.ChannelMeta != nil &&
		relayInfo.ChannelType == constant.ChannelTypeOpenRouter &&
		summary.IsClaudeUsageSemantic

	if isOpenRouterClaudeBilling {
		summary.PromptTokens -= summary.CacheTokens
		isUsingCustomSettings := relayInfo.PriceData.UsePrice || hasCustomModelRatio(summary.ModelName, relayInfo.PriceData.ModelRatio)
		if summary.CacheCreationTokens == 0 && relayInfo.PriceData.CacheCreationRatio != 1 && usage.Cost != 0 && !isUsingCustomSettings {
			maybeCacheCreationTokens := CalcOpenRouterCacheCreateTokens(*usage, relayInfo.PriceData)
			if maybeCacheCreationTokens >= 0 && summary.PromptTokens >= maybeCacheCreationTokens {
				summary.CacheCreationTokens = maybeCacheCreationTokens
			}
		}
		summary.PromptTokens -= summary.CacheCreationTokens
	}

	dPromptTokens := decimal.NewFromInt(int64(summary.PromptTokens))
	dCacheTokens := decimal.NewFromInt(int64(summary.CacheTokens))
	dImageTokens := decimal.NewFromInt(int64(summary.ImageTokens))
	dAudioTokens := decimal.NewFromInt(int64(summary.AudioTokens))
	dCompletionTokens := decimal.NewFromInt(int64(summary.CompletionTokens))
	dCachedCreationTokens := decimal.NewFromInt(int64(summary.CacheCreationTokens))
	dCompletionRatio := decimal.NewFromFloat(summary.CompletionRatio)
	dCacheRatio := decimal.NewFromFloat(summary.CacheRatio)
	dImageRatio := decimal.NewFromFloat(summary.ImageRatio)
	dModelRatio := decimal.NewFromFloat(summary.ModelRatio)
	dGroupRatio := decimal.NewFromFloat(summary.GroupRatio)
	dModelPrice := decimal.NewFromFloat(summary.ModelPrice)
	dCacheCreationRatio := decimal.NewFromFloat(summary.CacheCreationRatio)
	dCacheCreationRatio5m := decimal.NewFromFloat(summary.CacheCreationRatio5m)
	dCacheCreationRatio1h := decimal.NewFromFloat(summary.CacheCreationRatio1h)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	ratio := dModelRatio.Mul(dGroupRatio)
	summary.ToolCallSurchargeQuota = calculateTextToolCallSurcharge(ctx, relayInfo, &summary)

	var audioInputQuota decimal.Decimal
	if !relayInfo.PriceData.UsePrice {
		baseTokens := dPromptTokens

		var cachedTokensWithRatio decimal.Decimal
		if !dCacheTokens.IsZero() {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCacheTokens)
			}
			cachedTokensWithRatio = dCacheTokens.Mul(dCacheRatio)
		}

		var cachedCreationTokensWithRatio decimal.Decimal
		hasSplitCacheCreationTokens := summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0
		if !dCachedCreationTokens.IsZero() || hasSplitCacheCreationTokens {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCachedCreationTokens)
				cachedCreationTokensWithRatio = dCachedCreationTokens.Mul(dCacheCreationRatio)
			} else {
				remaining := summary.CacheCreationTokens - summary.CacheCreationTokens5m - summary.CacheCreationTokens1h
				if remaining < 0 {
					remaining = 0
				}
				cachedCreationTokensWithRatio = decimal.NewFromInt(int64(remaining)).Mul(dCacheCreationRatio)
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens5m)).Mul(dCacheCreationRatio5m))
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens1h)).Mul(dCacheCreationRatio1h))
			}
		}

		var imageTokensWithRatio decimal.Decimal
		if !dImageTokens.IsZero() {
			baseTokens = baseTokens.Sub(dImageTokens)
			imageTokensWithRatio = dImageTokens.Mul(dImageRatio)
		}

		if !dAudioTokens.IsZero() {
			summary.AudioInputPrice = operation_setting.GetGeminiInputAudioPricePerMillionTokens(summary.ModelName)
			if summary.AudioInputPrice > 0 {
				baseTokens = baseTokens.Sub(dAudioTokens)
				audioInputQuota = decimal.NewFromFloat(summary.AudioInputPrice).
					Div(decimal.NewFromInt(1000000)).Mul(dAudioTokens).Mul(dGroupRatio).Mul(dQuotaPerUnit)
			}
		}

		promptQuota := baseTokens.Add(cachedTokensWithRatio).Add(imageTokensWithRatio).Add(cachedCreationTokensWithRatio)
		completionQuota := dCompletionTokens.Mul(dCompletionRatio)
		quotaCalculateDecimal := promptQuota.Add(completionQuota).Mul(ratio)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(audioInputQuota)

		if len(relayInfo.PriceData.OtherRatios) > 0 {
			for _, otherRatio := range relayInfo.PriceData.OtherRatios {
				quotaCalculateDecimal = quotaCalculateDecimal.Mul(decimal.NewFromFloat(otherRatio))
			}
		}

		if !ratio.IsZero() && quotaCalculateDecimal.LessThanOrEqual(decimal.Zero) {
			quotaCalculateDecimal = decimal.NewFromInt(1)
		}
		summary.Quota = int(quotaCalculateDecimal.Round(0).IntPart())
	} else {
		quotaCalculateDecimal := dModelPrice.Mul(dQuotaPerUnit).Mul(dGroupRatio)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(audioInputQuota)
		if len(relayInfo.PriceData.OtherRatios) > 0 {
			for _, otherRatio := range relayInfo.PriceData.OtherRatios {
				quotaCalculateDecimal = quotaCalculateDecimal.Mul(decimal.NewFromFloat(otherRatio))
			}
		}
		summary.Quota = int(quotaCalculateDecimal.Round(0).IntPart())
	}

	if summary.TotalTokens == 0 {
		summary.Quota = 0
	} else if !ratio.IsZero() && summary.Quota == 0 {
		summary.Quota = 1
	}

	return summary
}

func usageSemanticFromUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) string {
	if usage != nil && usage.UsageSemantic != "" {
		return usage.UsageSemantic
	}
	if relayInfo != nil && relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return "anthropic"
	}
	return "openai"
}

type responsesCompactLogDetails struct {
	Mode                string
	Setting             string
	UpstreamProfile     string
	UpstreamPath        string
	AutoFallback        bool
	AutoFallbackWindow  bool
	ContextFallback     bool
	PreviousIDFallback  bool
	VisibleOnlyFallback bool
	SummaryModel        string
	SummaryModels       []string
	SummaryModelRetry   bool
	NativeFallback      *ResponsesCompactNativeFallbackLog
	FallbackReason      string
	RetryUntilUnix      int64
	RetryIntervalHrs    int
}

type ResponsesCompactNativeFallbackLog struct {
	Attempted        bool
	ChannelID        int
	UpstreamPath     string
	StatusCode       int
	Reason           string
	RetryUntilUnix   int64
	RetryIntervalHrs int
}

type ResponsesCompactFallbackAttemptKind string

const (
	ResponsesCompactFallbackAttemptAuto         ResponsesCompactFallbackAttemptKind = "auto"
	ResponsesCompactFallbackAttemptContext      ResponsesCompactFallbackAttemptKind = "context"
	ResponsesCompactFallbackAttemptPreviousID   ResponsesCompactFallbackAttemptKind = "previous_id"
	ResponsesCompactFallbackAttemptVisibleOnly  ResponsesCompactFallbackAttemptKind = "visible_only"
	ResponsesCompactFallbackAttemptSummaryModel ResponsesCompactFallbackAttemptKind = "summary_model"
)

type ResponsesCompactFallbackAttemptLog struct {
	ChannelID           int
	AutoFallback        bool
	ContextFallback     bool
	PreviousIDFallback  bool
	VisibleOnlyFallback bool
	SummaryModelRetry   bool
	SummaryModels       []string
}

const (
	responsesCompactErrorLogKey           = "responses_compact_error_log"
	responsesCompactFallbackAttemptLogKey = "responses_compact_fallback_attempt_log"
)

func MarkResponsesCompactErrorLog(ctx *gin.Context, enabled bool) {
	if ctx == nil {
		return
	}
	if enabled {
		ctx.Set(responsesCompactErrorLogKey, true)
		return
	}
	if ctx.Keys != nil {
		delete(ctx.Keys, responsesCompactErrorLogKey)
	}
}

func MarkResponsesCompactNativeFallback(
	ctx *gin.Context,
	relayInfo *relaycommon.RelayInfo,
	statusCode int,
	reason string,
	now time.Time,
) {
	if ctx == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	reason = sanitizeResponsesCompactLogValue(reason, 240)
	retryInterval := dto.DefaultResponsesCompactAutoFallbackRetryIntervalHours
	channelID := 0
	if relayInfo != nil && relayInfo.ChannelMeta != nil {
		retryInterval = relayInfo.ChannelOtherSettings.ResponsesCompactAutoFallbackRetryIntervalHoursOrDefault()
		channelID = relayInfo.ChannelMeta.ChannelId
	}
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactNativeFallback, ResponsesCompactNativeFallbackLog{
		Attempted:        true,
		ChannelID:        channelID,
		UpstreamPath:     "/v1/responses/compact",
		StatusCode:       statusCode,
		Reason:           reason,
		RetryUntilUnix:   now.UTC().Add(time.Duration(retryInterval) * time.Hour).Unix(),
		RetryIntervalHrs: retryInterval,
	})
}

func MarkResponsesCompactFallbackAttempt(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, kind ResponsesCompactFallbackAttemptKind, summaryModels []string) {
	if ctx == nil || relayInfo == nil || relayInfo.ChannelMeta == nil {
		return
	}
	channelID := relayInfo.ChannelMeta.ChannelId
	attemptLog := ResponsesCompactFallbackAttemptLog{ChannelID: channelID}
	if value, ok := ctx.Get(responsesCompactFallbackAttemptLogKey); ok {
		if existing, ok := value.(ResponsesCompactFallbackAttemptLog); ok && existing.ChannelID == channelID {
			attemptLog = existing
		}
	}
	switch kind {
	case ResponsesCompactFallbackAttemptAuto:
		attemptLog.AutoFallback = true
	case ResponsesCompactFallbackAttemptContext:
		attemptLog.ContextFallback = true
	case ResponsesCompactFallbackAttemptPreviousID:
		attemptLog.PreviousIDFallback = true
	case ResponsesCompactFallbackAttemptVisibleOnly:
		attemptLog.VisibleOnlyFallback = true
	case ResponsesCompactFallbackAttemptSummaryModel:
		attemptLog.SummaryModelRetry = true
		attemptLog.SummaryModels = append([]string(nil), summaryModels...)
	}
	ctx.Set(responsesCompactFallbackAttemptLogKey, attemptLog)
}

func sanitizeResponsesCompactLogValue(value string, limit int) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func responsesCompactLogInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, now time.Time) (responsesCompactLogDetails, bool) {
	if relayInfo == nil || relayInfo.RelayMode != relayconstant.RelayModeResponsesCompact {
		if ctx == nil || ctx.GetInt("relay_mode") != relayconstant.RelayModeResponsesCompact {
			return responsesCompactLogDetails{}, false
		}
	}
	if now.IsZero() {
		now = time.Now()
	}
	settings := dto.ChannelOtherSettings{}
	channelType := 0
	if relayInfo != nil && relayInfo.ChannelMeta != nil {
		settings = relayInfo.ChannelMeta.ChannelOtherSettings
		channelType = relayInfo.ChannelMeta.ChannelType
	}
	if ctx != nil {
		if channelType == 0 {
			channelType = ctx.GetInt("channel_type")
		}
		if ctxSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](ctx, constant.ContextKeyChannelOtherSetting); ok {
			settings = ctxSettings
		}
	}

	setting := settings.NormalizedResponsesCompactModeSetting()
	upstreamProfile := string(settings.NormalizedResponsesUpstreamProfile())
	effective := settings.EffectiveResponsesCompactModeOrDefaultAt(now)
	disabled := settings.HasDisabledResponsesCompact()
	proxyCompatibilityProfile := settings.HasResponsesProxyCompatibilityProfile()
	autoFallbackWindow := !proxyCompatibilityProfile && setting == dto.ResponsesCompactModeAuto && settings.HasActiveResponsesCompactAutoFallback(now)
	autoFallback := autoFallbackWindow
	if ctx != nil && ctx.GetBool("responses_compact_auto_fallback_attempted") && !disabled && !proxyCompatibilityProfile {
		setting = dto.ResponsesCompactModeAuto
		effective = dto.ResponsesCompactModeSynthetic
		autoFallback = true
		autoFallbackWindow = false
	}
	contextFallback := false
	previousIDFallback := false
	visibleOnlyFallback := false
	summaryModelRetry := false
	summaryModel := ""
	if ctx != nil {
		contextFallback = ctx.GetBool("responses_compact_context_fallback_attempted")
		previousIDFallback = ctx.GetBool("responses_compact_previous_response_id_fallback_attempted")
		visibleOnlyFallback = ctx.GetBool("responses_compact_visible_only_fallback_attempted")
		summaryModelRetry = ctx.GetBool("responses_compact_summary_model_fallback_attempted")
		summaryModel = common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactSummaryModel)
	}
	summaryModels := []string(nil)
	if ctx != nil {
		summaryModels = common.GetContextKeyStringSlice(ctx, constant.ContextKeyResponsesCompactSummaryModels)
	}
	if ctx != nil && ctx.GetBool(responsesCompactErrorLogKey) {
		if value, ok := ctx.Get(responsesCompactFallbackAttemptLogKey); ok {
			if attemptLog, ok := value.(ResponsesCompactFallbackAttemptLog); ok && fallbackAttemptMatchesRelayInfo(attemptLog, relayInfo) {
				if attemptLog.AutoFallback && !disabled && !proxyCompatibilityProfile {
					setting = dto.ResponsesCompactModeAuto
					autoFallback = true
					autoFallbackWindow = false
				}
				contextFallback = contextFallback || attemptLog.ContextFallback
				previousIDFallback = previousIDFallback || attemptLog.PreviousIDFallback
				visibleOnlyFallback = visibleOnlyFallback || attemptLog.VisibleOnlyFallback
				summaryModelRetry = summaryModelRetry || attemptLog.SummaryModelRetry
				if len(summaryModels) == 0 && len(attemptLog.SummaryModels) > 0 {
					summaryModels = append([]string(nil), attemptLog.SummaryModels...)
				}
			}
		}
	}
	if (autoFallback || contextFallback || previousIDFallback || visibleOnlyFallback || summaryModelRetry) && !disabled {
		effective = dto.ResponsesCompactModeSynthetic
	}

	mode := string(dto.ResponsesCompactModeNative)
	upstreamPath := "/v1/responses/compact"
	if effective == dto.ResponsesCompactModeDisabled {
		mode = string(dto.ResponsesCompactModeDisabled)
	} else if channelType == constant.ChannelTypeOpenAI && effective == dto.ResponsesCompactModeSynthetic {
		mode = string(dto.ResponsesCompactModeSynthetic)
		upstreamPath = "/v1/responses"
	}

	return responsesCompactLogDetails{
		Mode:                mode,
		Setting:             string(setting),
		UpstreamProfile:     upstreamProfile,
		UpstreamPath:        upstreamPath,
		AutoFallback:        autoFallback,
		AutoFallbackWindow:  autoFallbackWindow,
		ContextFallback:     contextFallback,
		PreviousIDFallback:  previousIDFallback,
		VisibleOnlyFallback: visibleOnlyFallback,
		SummaryModel:        summaryModel,
		SummaryModels:       summaryModels,
		SummaryModelRetry:   summaryModelRetry,
		FallbackReason:      sanitizeResponsesCompactLogValue(settings.ResponsesCompactAutoFallbackReason, 240),
		RetryUntilUnix:      responsesCompactAutoFallbackRetryUntil(settings, now),
		RetryIntervalHrs:    settings.ResponsesCompactAutoFallbackRetryIntervalHoursOrDefault(),
	}, true
}

func responsesCompactAutoFallbackRetryUntil(settings dto.ChannelOtherSettings, now time.Time) int64 {
	if settings.ResponsesCompactAutoFallbackAt > 0 {
		interval := time.Duration(settings.ResponsesCompactAutoFallbackRetryIntervalHoursOrDefault()) * time.Hour
		return time.Unix(settings.ResponsesCompactAutoFallbackAt, 0).UTC().Add(interval).Unix()
	}
	if settings.ResponsesCompactAutoFallbackDate > 0 {
		year := settings.ResponsesCompactAutoFallbackDate / 10000
		month := time.Month((settings.ResponsesCompactAutoFallbackDate / 100) % 100)
		day := settings.ResponsesCompactAutoFallbackDate % 100
		if year > 0 && month >= time.January && month <= time.December && day > 0 && day <= 31 {
			fallbackDate := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
			if fallbackDate.Year() == year && fallbackDate.Month() == month && fallbackDate.Day() == day {
				return fallbackDate.AddDate(0, 0, 1).Unix()
			}
		}
	}
	return 0
}

func shouldAppendResponsesCompactNativeFallback(ctx *gin.Context) bool {
	return ctx != nil &&
		(ctx.GetBool("responses_compact_auto_fallback_attempted") || ctx.GetBool(responsesCompactErrorLogKey))
}

func nativeFallbackMatchesRelayInfo(nativeFallback ResponsesCompactNativeFallbackLog, relayInfo *relaycommon.RelayInfo) bool {
	if nativeFallback.ChannelID == 0 || relayInfo == nil || relayInfo.ChannelMeta == nil {
		return true
	}
	return nativeFallback.ChannelID == relayInfo.ChannelMeta.ChannelId
}

func fallbackAttemptMatchesRelayInfo(attemptLog ResponsesCompactFallbackAttemptLog, relayInfo *relaycommon.RelayInfo) bool {
	if attemptLog.ChannelID == 0 || relayInfo == nil || relayInfo.ChannelMeta == nil {
		return true
	}
	return attemptLog.ChannelID == relayInfo.ChannelMeta.ChannelId
}

func appendResponsesCompactLogInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, extraContent []string, other map[string]interface{}, now time.Time) ([]string, map[string]interface{}) {
	logInfo, ok := responsesCompactLogInfo(ctx, relayInfo, now)
	if !ok {
		return extraContent, other
	}
	if other == nil {
		other = make(map[string]interface{})
	}
	other["responses_compact_mode"] = logInfo.Mode
	other["responses_compact_setting"] = logInfo.Setting
	if logInfo.UpstreamProfile != "" {
		other["responses_upstream_profile"] = logInfo.UpstreamProfile
	}
	other["responses_compact_upstream_path"] = logInfo.UpstreamPath
	other["responses_compact_final_upstream_path"] = logInfo.UpstreamPath
	if logInfo.AutoFallback {
		other["responses_compact_auto_fallback"] = true
	}
	if logInfo.AutoFallbackWindow {
		other["responses_compact_auto_fallback_window"] = true
		if logInfo.FallbackReason != "" {
			other["responses_compact_auto_fallback_reason"] = logInfo.FallbackReason
		}
		if logInfo.RetryUntilUnix > 0 {
			other["responses_compact_auto_fallback_retry_until"] = logInfo.RetryUntilUnix
		}
		if logInfo.RetryIntervalHrs > 0 {
			other["responses_compact_auto_fallback_retry_interval_hours"] = logInfo.RetryIntervalHrs
		}
	}
	if logInfo.ContextFallback {
		other["responses_compact_context_fallback"] = true
	}
	if logInfo.PreviousIDFallback {
		other["responses_compact_previous_response_id_fallback"] = true
	}
	if logInfo.VisibleOnlyFallback {
		other["responses_compact_visible_only_fallback"] = true
	}
	if logInfo.SummaryModelRetry {
		other["responses_compact_summary_model_fallback"] = true
	}
	if logInfo.SummaryModel != "" {
		other["responses_compact_summary_model"] = logInfo.SummaryModel
	}
	if len(logInfo.SummaryModels) > 0 {
		other["responses_compact_summary_models"] = logInfo.SummaryModels
	}
	if shouldAppendResponsesCompactNativeFallback(ctx) {
		if nativeFallback, ok := common.GetContextKeyType[ResponsesCompactNativeFallbackLog](ctx, constant.ContextKeyResponsesCompactNativeFallback); ok && nativeFallback.Attempted && nativeFallbackMatchesRelayInfo(nativeFallback, relayInfo) {
			nativeFallback.Reason = sanitizeResponsesCompactLogValue(nativeFallback.Reason, 240)
			logInfo.NativeFallback = &nativeFallback
			other["responses_compact_native_attempted"] = true
			if nativeFallback.ChannelID > 0 {
				other["responses_compact_native_channel_id"] = nativeFallback.ChannelID
			}
			if nativeFallback.UpstreamPath != "" {
				other["responses_compact_native_upstream_path"] = nativeFallback.UpstreamPath
			}
			if nativeFallback.StatusCode > 0 {
				other["responses_compact_native_status_code"] = nativeFallback.StatusCode
			}
			if nativeFallback.Reason != "" {
				other["responses_compact_auto_fallback_reason"] = nativeFallback.Reason
			}
			if nativeFallback.RetryUntilUnix > 0 {
				other["responses_compact_auto_fallback_retry_until"] = nativeFallback.RetryUntilUnix
			}
			if nativeFallback.RetryIntervalHrs > 0 {
				other["responses_compact_auto_fallback_retry_interval_hours"] = nativeFallback.RetryIntervalHrs
			}
		}
	}
	content := fmt.Sprintf("Responses Compact mode=%s setting=%s path=%s", logInfo.Mode, logInfo.Setting, logInfo.UpstreamPath)
	if logInfo.UpstreamProfile != "" {
		content += fmt.Sprintf(" upstream_profile=%s", logInfo.UpstreamProfile)
	}
	if logInfo.AutoFallback {
		content += " auto_fallback=true"
	}
	if logInfo.AutoFallbackWindow {
		content += " auto_fallback_window=true"
		if logInfo.FallbackReason != "" {
			content += fmt.Sprintf(" fallback_reason=%s", logInfo.FallbackReason)
		}
	}
	if logInfo.NativeFallback != nil {
		if logInfo.NativeFallback.UpstreamPath != "" {
			content += fmt.Sprintf(" native_path=%s", logInfo.NativeFallback.UpstreamPath)
		}
		if logInfo.NativeFallback.StatusCode > 0 {
			content += fmt.Sprintf(" native_status=%d", logInfo.NativeFallback.StatusCode)
		}
		if logInfo.NativeFallback.Reason != "" {
			content += fmt.Sprintf(" fallback_reason=%s", logInfo.NativeFallback.Reason)
		}
	}
	if logInfo.ContextFallback {
		content += " context_fallback=true"
	}
	if logInfo.PreviousIDFallback {
		content += " previous_response_id_fallback=true"
	}
	if logInfo.VisibleOnlyFallback {
		content += " visible_only_fallback=true"
	}
	if logInfo.SummaryModelRetry {
		content += " summary_model_fallback=true"
	}
	if logInfo.SummaryModel != "" {
		content += fmt.Sprintf(" summary_model=%s", logInfo.SummaryModel)
	}
	if len(logInfo.SummaryModels) > 0 {
		content += fmt.Sprintf(" summary_models=%s", strings.Join(logInfo.SummaryModels, ","))
	}
	if ctx != nil {
		logger.LogInfo(ctx, content)
	}
	return append(extraContent, content), other
}

func AppendResponsesCompactLogInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, extraContent []string, other map[string]interface{}, now time.Time) ([]string, map[string]interface{}) {
	return appendResponsesCompactLogInfo(ctx, relayInfo, extraContent, other, now)
}

func PostTextConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent []string) {
	originUsage := usage
	if usage == nil {
		extraContent = append(extraContent, "上游无计费信息")
	}
	if originUsage != nil {
		ObserveChannelAffinityUsageCacheByRelayFormat(ctx, usage, relayInfo.GetFinalRequestRelayFormat())
	}

	adminRejectReason := common.GetContextKeyString(ctx, constant.ContextKeyAdminRejectReason)
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	var tieredResult *billingexpr.TieredResult
	tieredBillingApplied := false
	if originUsage != nil {
		var tieredUsedVars map[string]bool
		if snap := relayInfo.TieredBillingSnapshot; snap != nil {
			tieredUsedVars = billingexpr.UsedVars(snap.ExprString)
		}
		tieredOk, tieredQuota, tieredRes := TryTieredSettle(relayInfo, BuildTieredTokenParams(usage, summary.IsClaudeUsageSemantic, tieredUsedVars))
		if tieredOk {
			tieredBillingApplied = true
			tieredResult = tieredRes
			summary.Quota = composeTieredTextQuota(relayInfo, summary, tieredQuota, tieredRes)
		}
	}

	if summary.WebSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Web Search 调用 %d 次，调用花费 %s", summary.WebSearchCallCount, decimal.NewFromFloat(summary.WebSearchPrice).Mul(decimal.NewFromInt(int64(summary.WebSearchCallCount))).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.ClaudeWebSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Claude Web Search 调用 %d 次，调用花费 %s", summary.ClaudeWebSearchCallCount, decimal.NewFromFloat(summary.ClaudeWebSearchPrice).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Mul(decimal.NewFromInt(int64(summary.ClaudeWebSearchCallCount))).String()))
	}
	if summary.FileSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("File Search 调用 %d 次，调用花费 %s", summary.FileSearchCallCount, decimal.NewFromFloat(summary.FileSearchPrice).Mul(decimal.NewFromInt(int64(summary.FileSearchCallCount))).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Audio Input 花费 %s", decimal.NewFromFloat(summary.AudioInputPrice).Div(decimal.NewFromInt(1000000)).Mul(decimal.NewFromInt(int64(summary.AudioTokens))).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.ImageGenerationCallPrice > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Image Generation Call 花费 %s", decimal.NewFromFloat(summary.ImageGenerationCallPrice).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}

	if summary.TotalTokens == 0 {
		extraContent = append(extraContent, "上游没有返回计费信息，无法扣费（可能是上游超时）")
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, summary.ModelName, relayInfo.FinalPreConsumedQuota))
	} else {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, summary.Quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, summary.Quota)
	}

	if err := SettleBilling(ctx, relayInfo, summary.Quota); err != nil {
		logger.LogError(ctx, "error settling billing: "+err.Error())
	}

	logModel := summary.ModelName
	if strings.HasPrefix(logModel, "gpt-4-gizmo") {
		logModel = "gpt-4-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}
	if strings.HasPrefix(logModel, "gpt-4o-gizmo") {
		logModel = "gpt-4o-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}

	var other map[string]interface{}
	if summary.IsClaudeUsageSemantic {
		other = GenerateClaudeOtherInfo(ctx, relayInfo,
			summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio,
			summary.CacheTokens, summary.CacheRatio,
			summary.CacheCreationTokens, summary.CacheCreationRatio,
			summary.CacheCreationTokens5m, summary.CacheCreationRatio5m,
			summary.CacheCreationTokens1h, summary.CacheCreationRatio1h,
			summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
		other["usage_semantic"] = "anthropic"
	} else {
		other = GenerateTextOtherInfo(ctx, relayInfo, summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio, summary.CacheTokens, summary.CacheRatio, summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	}
	if adminRejectReason != "" {
		other["reject_reason"] = adminRejectReason
	}
	if summary.ImageTokens != 0 {
		other["image"] = true
		other["image_ratio"] = summary.ImageRatio
		other["image_output"] = summary.ImageTokens
	}
	if summary.WebSearchCallCount > 0 {
		other["web_search"] = true
		other["web_search_call_count"] = summary.WebSearchCallCount
		other["web_search_price"] = summary.WebSearchPrice
	} else if summary.ClaudeWebSearchCallCount > 0 {
		other["web_search"] = true
		other["web_search_call_count"] = summary.ClaudeWebSearchCallCount
		other["web_search_price"] = summary.ClaudeWebSearchPrice
	}
	if summary.FileSearchCallCount > 0 {
		other["file_search"] = true
		other["file_search_call_count"] = summary.FileSearchCallCount
		other["file_search_price"] = summary.FileSearchPrice
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		other["audio_input_seperate_price"] = true
		other["audio_input_token_count"] = summary.AudioTokens
		other["audio_input_price"] = summary.AudioInputPrice
	}
	if summary.ImageGenerationCallPrice > 0 {
		other["image_generation_call"] = true
		other["image_generation_call_price"] = summary.ImageGenerationCallPrice
	}
	if summary.CacheCreationTokens > 0 {
		other["cache_creation_tokens"] = summary.CacheCreationTokens
		other["cache_creation_ratio"] = summary.CacheCreationRatio
	}
	if summary.CacheCreationTokens5m > 0 {
		other["cache_creation_tokens_5m"] = summary.CacheCreationTokens5m
		other["cache_creation_ratio_5m"] = summary.CacheCreationRatio5m
	}
	if summary.CacheCreationTokens1h > 0 {
		other["cache_creation_tokens_1h"] = summary.CacheCreationTokens1h
		other["cache_creation_ratio_1h"] = summary.CacheCreationRatio1h
	}
	cacheWriteTokens := cacheWriteTokensTotal(summary)
	if cacheWriteTokens > 0 {
		// cache_write_tokens: normalized cache creation total for UI display.
		// If split 5m/1h values are present, this is their sum; otherwise it falls back
		// to cache_creation_tokens.
		other["cache_write_tokens"] = cacheWriteTokens
	}
	if relayInfo.GetFinalRequestRelayFormat() != types.RelayFormatClaude && usage != nil && usage.UsageSource != "" && usage.InputTokens > 0 {
		// input_tokens_total: explicit normalized total input used by the usage log UI.
		// Only write this field when upstream/current conversion has already provided a
		// reliable total input value and tagged the usage source. Do not infer it from
		// prompt/cache fields here, otherwise old upstream payloads may be double-counted.
		other["input_tokens_total"] = usage.InputTokens
	}
	if tieredBillingApplied {
		InjectTieredBillingInfo(other, relayInfo, tieredResult)
	}
	extraContent, other = appendResponsesCompactLogInfo(ctx, relayInfo, extraContent, other, time.Now())
	logContent := strings.Join(extraContent, ", ")

	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     summary.PromptTokens,
		CompletionTokens: summary.CompletionTokens,
		ModelName:        logModel,
		TokenName:        summary.TokenName,
		Quota:            summary.Quota,
		Content:          logContent,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(summary.UseTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, int64(summary.CompletionTokens))
	})
}
