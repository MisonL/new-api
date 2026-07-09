package service

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsBenignCanceledTextStream(t *testing.T) {
	status := relaycommon.NewStreamStatus()
	status.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)

	require.True(t, isBenignCanceledTextStream(&relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: status,
	}))
	require.False(t, isBenignCanceledTextStream(&relaycommon.RelayInfo{
		IsStream:     false,
		StreamStatus: status,
	}))
}

func TestIsBenignCanceledTextStreamRejectsSoftErroredClientGone(t *testing.T) {
	status := relaycommon.NewStreamStatus()
	status.RecordError("upstream warning")
	status.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)

	require.False(t, isBenignCanceledTextStream(&relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: status,
	}))
}

func TestCalculateTextQuotaSummaryUnifiedForClaudeSemantic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         100,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	priceData := types.PriceData{
		ModelRatio:           1,
		CompletionRatio:      2,
		CacheRatio:           0.1,
		CacheCreationRatio:   1.25,
		CacheCreation5mRatio: 1.25,
		CacheCreation1hRatio: 2,
		GroupRatioInfo: types.GroupRatioInfo{
			GroupRatio: 1,
		},
	}

	chatRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}
	messageRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}

	chatSummary := calculateTextQuotaSummary(ctx, chatRelayInfo, usage)
	messageSummary := calculateTextQuotaSummary(ctx, messageRelayInfo, usage)

	require.Equal(t, messageSummary.Quota, chatSummary.Quota)
	require.Equal(t, messageSummary.CacheCreationTokens5m, chatSummary.CacheCreationTokens5m)
	require.Equal(t, messageSummary.CacheCreationTokens1h, chatSummary.CacheCreationTokens1h)
	require.True(t, chatSummary.IsClaudeUsageSemantic)
	require.Equal(t, 1488, chatSummary.Quota)
}

func TestCalculateTextQuotaSummaryUsesSplitClaudeCacheCreationRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      1,
			CacheRatio:           0,
			CacheCreationRatio:   1,
			CacheCreation5mRatio: 2,
			CacheCreation1hRatio: 3,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 0,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 10,
		},
		ClaudeCacheCreation5mTokens: 2,
		ClaudeCacheCreation1hTokens: 3,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 100 + remaining(5)*1 + 2*2 + 3*3 = 118
	require.Equal(t, 118, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesAnthropicUsageSemanticFromUpstreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      2,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         100,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, "anthropic", summary.UsageSemantic)
	require.Equal(t, 1488, summary.Quota)
}

func TestCacheWriteTokensTotal(t *testing.T) {
	t.Run("split cache creation", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens:   50,
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("legacy cache creation", func(t *testing.T) {
		summary := textQuotaSummary{CacheCreationTokens: 50}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("split cache creation without aggregate remainder", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 30, cacheWriteTokensTotal(summary))
	})
}

func TestResponsesCompactLogInfoRecordsNativeAutoMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeAuto,
			},
		},
	}

	logInfo, ok := responsesCompactLogInfo(ctx, info, now)

	require.True(t, ok)
	require.Equal(t, "native", logInfo.Mode)
	require.Equal(t, "auto", logInfo.Setting)
	require.Equal(t, "/v1/responses/compact", logInfo.UpstreamPath)
	require.False(t, logInfo.AutoFallback)
}

func TestResponsesCompactLogInfoRecordsSyntheticDuringActiveAutoFallbackWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:           dto.ResponsesCompactModeAuto,
				ResponsesCompactAutoFallbackAt: now.Unix(),
			},
		},
	}

	logInfo, ok := responsesCompactLogInfo(ctx, info, now)

	require.True(t, ok)
	require.Equal(t, "synthetic_summary", logInfo.Mode)
	require.Equal(t, "auto", logInfo.Setting)
	require.Equal(t, "/v1/responses", logInfo.UpstreamPath)
	require.True(t, logInfo.AutoFallback)
}

func TestResponsesCompactLogInfoRecordsSyntheticForResponsesProxyProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
			},
		},
	}

	logInfo, ok := responsesCompactLogInfo(ctx, info, now)

	require.True(t, ok)
	require.Equal(t, "synthetic_summary", logInfo.Mode)
	require.Equal(t, "auto", logInfo.Setting)
	require.Equal(t, "generic_proxy", logInfo.UpstreamProfile)
	require.Equal(t, "/v1/responses", logInfo.UpstreamPath)
	require.False(t, logInfo.AutoFallback)
}

func TestResponsesCompactLogInfoDoesNotMarkAutoFallbackForProxyProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)
	ctx.Set("responses_compact_auto_fallback_attempted", true)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:           dto.ResponsesCompactModeAuto,
				ResponsesUpstreamProfile:       dto.ResponsesUpstreamProfileGenericProxy,
				ResponsesCompactAutoFallbackAt: now.Unix(),
			},
		},
	}

	logInfo, ok := responsesCompactLogInfo(ctx, info, now)

	require.True(t, ok)
	require.Equal(t, "synthetic_summary", logInfo.Mode)
	require.Equal(t, "auto", logInfo.Setting)
	require.Equal(t, "/v1/responses", logInfo.UpstreamPath)
	require.False(t, logInfo.AutoFallback)
}

func TestResponsesCompactLogInfoRecordsDisabledBeforeProxyProfileFallbacks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)
	ctx.Set("responses_compact_auto_fallback_attempted", true)
	ctx.Set("responses_compact_context_fallback_attempted", true)
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactSummaryModelFallbackAttempted, true)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeDisabled,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
			},
		},
	}

	logInfo, ok := responsesCompactLogInfo(ctx, info, now)

	require.True(t, ok)
	require.Equal(t, "disabled", logInfo.Mode)
	require.Equal(t, "disabled", logInfo.Setting)
	require.Equal(t, "/v1/responses/compact", logInfo.UpstreamPath)
	require.False(t, logInfo.AutoFallback)
	// These flags record attempted request paths; disabled only prevents them from changing the effective compact mode.
	require.True(t, logInfo.ContextFallback)
	require.True(t, logInfo.SummaryModelRetry)
}

func TestAppendResponsesCompactLogInfoWritesContentAndOther(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
	}
	other := map[string]interface{}{"existing": true}

	content, annotatedOther := appendResponsesCompactLogInfo(ctx, info, []string{"模型倍率 1.00"}, other, now)

	require.Equal(t, []string{
		"模型倍率 1.00",
		"Responses Compact mode=synthetic_summary setting=synthetic_summary path=/v1/responses",
	}, content)
	require.Equal(t, "synthetic_summary", annotatedOther["responses_compact_mode"])
	require.Equal(t, "synthetic_summary", annotatedOther["responses_compact_setting"])
	require.Equal(t, "/v1/responses", annotatedOther["responses_compact_upstream_path"])
	require.Equal(t, true, annotatedOther["existing"])
}

func TestAppendResponsesCompactLogInfoWritesUpstreamProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
			},
		},
	}

	content, other := appendResponsesCompactLogInfo(ctx, info, nil, nil, now)

	require.Len(t, content, 1)
	require.Contains(t, content[0], "upstream_profile=sub2api_http")
	require.Equal(t, "sub2api_http", other["responses_upstream_profile"])
	require.Equal(t, "native", other["responses_compact_mode"])
	require.Equal(t, "/v1/responses/compact", other["responses_compact_upstream_path"])
}

func TestAppendResponsesCompactLogInfoRecordsContextAndSummaryModelFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)
	ctx.Set("responses_compact_context_fallback_attempted", true)
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactSummaryModelFallbackAttempted, true)
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactSummaryModel, "gpt-5.4")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactSummaryModels, []string{"gpt-5.4"})
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
	}

	content, annotatedOther := appendResponsesCompactLogInfo(ctx, info, nil, nil, now)

	require.Equal(t, []string{
		"Responses Compact mode=synthetic_summary setting=synthetic_summary path=/v1/responses context_fallback=true summary_model_fallback=true summary_model=gpt-5.4 summary_models=gpt-5.4",
	}, content)
	require.Equal(t, true, annotatedOther["responses_compact_context_fallback"])
	require.Equal(t, true, annotatedOther["responses_compact_summary_model_fallback"])
	require.Equal(t, "gpt-5.4", annotatedOther["responses_compact_summary_model"])
	require.Equal(t, []string{"gpt-5.4"}, annotatedOther["responses_compact_summary_models"])
}

func TestAppendResponsesCompactLogInfoRecordsPreviousResponseIDFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)
	ctx.Set("responses_compact_previous_response_id_fallback_attempted", true)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
			},
		},
	}

	content, annotatedOther := appendResponsesCompactLogInfo(ctx, info, nil, nil, now)

	require.Equal(t, []string{
		"Responses Compact mode=synthetic_summary setting=auto path=/v1/responses upstream_profile=sub2api_http previous_response_id_fallback=true",
	}, content)
	require.Equal(t, "synthetic_summary", annotatedOther["responses_compact_mode"])
	require.Equal(t, "/v1/responses", annotatedOther["responses_compact_upstream_path"])
	require.Equal(t, true, annotatedOther["responses_compact_previous_response_id_fallback"])
}

func TestAppendResponsesCompactLogInfoRecordsContextFallbackAfterModeRestored(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)
	ctx.Set("responses_compact_context_fallback_attempted", true)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeNative,
			},
		},
	}

	content, annotatedOther := appendResponsesCompactLogInfo(ctx, info, nil, nil, now)

	require.Equal(t, []string{
		"Responses Compact mode=synthetic_summary setting=native path=/v1/responses context_fallback=true",
	}, content)
	require.Equal(t, "synthetic_summary", annotatedOther["responses_compact_mode"])
	require.Equal(t, "native", annotatedOther["responses_compact_setting"])
	require.Equal(t, "/v1/responses", annotatedOther["responses_compact_upstream_path"])
	require.Equal(t, true, annotatedOther["responses_compact_context_fallback"])
}

func TestAppendResponsesCompactLogInfoRecordsCodexContextPrunedAsContextFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactCodexContextPruned, true)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeCodex,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeNative,
			},
		},
	}

	content, annotatedOther := appendResponsesCompactLogInfo(ctx, info, nil, nil, now)

	require.Equal(t, []string{
		"Responses Compact mode=native setting=native path=/v1/responses/compact context_fallback=true",
	}, content)
	require.Equal(t, true, annotatedOther["responses_compact_context_fallback"])
}

func TestAppendResponsesCompactLogInfoUsesRelayInfoForErrorLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeAuto,
			},
		},
	}

	content, annotatedOther := AppendResponsesCompactLogInfo(ctx, info, []string{"status_code=500"}, nil, now)

	require.Equal(t, []string{
		"status_code=500",
		"Responses Compact mode=native setting=auto path=/v1/responses/compact",
	}, content)
	require.Equal(t, "native", annotatedOther["responses_compact_mode"])
	require.Equal(t, "auto", annotatedOther["responses_compact_setting"])
	require.Equal(t, "/v1/responses/compact", annotatedOther["responses_compact_upstream_path"])
}

func TestResponsesCompactLogInfoCanUseContextForErrorLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)
	ctx.Set("relay_mode", relayconstant.RelayModeResponsesCompact)
	ctx.Set("channel_type", constant.ChannelTypeOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeAuto,
	})
	ctx.Set("responses_compact_auto_fallback_attempted", true)

	logInfo, ok := responsesCompactLogInfo(ctx, nil, now)

	require.True(t, ok)
	require.Equal(t, "synthetic_summary", logInfo.Mode)
	require.Equal(t, "auto", logInfo.Setting)
	require.Equal(t, "/v1/responses", logInfo.UpstreamPath)
	require.True(t, logInfo.AutoFallback)
}

func TestAppendResponsesCompactLogInfoRecordsAutoFallbackNativeAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 8, 0, 0, 0, time.UTC)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:                           dto.ResponsesCompactModeAuto,
				ResponsesCompactAutoFallbackRetryIntervalHours: 6,
			},
		},
	}
	ctx.Set("responses_compact_auto_fallback_attempted", true)
	MarkResponsesCompactNativeFallback(
		ctx,
		info,
		502,
		"status_code=502, Upstream service temporarily unavailable\nextra whitespace",
		now,
	)

	content, annotatedOther := AppendResponsesCompactLogInfo(ctx, info, nil, nil, now)

	require.Equal(t, []string{
		"Responses Compact mode=synthetic_summary setting=auto path=/v1/responses auto_fallback=true native_path=/v1/responses/compact native_status=502 fallback_reason=status_code=502, Upstream service temporarily unavailable extra whitespace",
	}, content)
	require.Equal(t, "synthetic_summary", annotatedOther["responses_compact_mode"])
	require.Equal(t, "/v1/responses", annotatedOther["responses_compact_final_upstream_path"])
	require.Equal(t, true, annotatedOther["responses_compact_auto_fallback"])
	require.Equal(t, true, annotatedOther["responses_compact_native_attempted"])
	require.Equal(t, "/v1/responses/compact", annotatedOther["responses_compact_native_upstream_path"])
	require.Equal(t, 502, annotatedOther["responses_compact_native_status_code"])
	require.Equal(t, "status_code=502, Upstream service temporarily unavailable extra whitespace", annotatedOther["responses_compact_auto_fallback_reason"])
	require.Equal(t, int64(1779804000), annotatedOther["responses_compact_auto_fallback_retry_until"])
	require.Equal(t, 6, annotatedOther["responses_compact_auto_fallback_retry_interval_hours"])
}

func TestAppendResponsesCompactLogInfoSkipsStaleNativeAttemptOnLaterSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 8, 0, 0, 0, time.UTC)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeAuto,
			},
		},
	}
	MarkResponsesCompactNativeFallback(ctx, info, 503, "status_code=503, first channel unavailable", now)

	content, annotatedOther := AppendResponsesCompactLogInfo(ctx, info, nil, nil, now)

	require.Equal(t, []string{
		"Responses Compact mode=native setting=auto path=/v1/responses/compact",
	}, content)
	_, hasNativeAttempt := annotatedOther["responses_compact_native_attempted"]
	require.False(t, hasNativeAttempt)
	_, hasFallbackReason := annotatedOther["responses_compact_auto_fallback_reason"]
	require.False(t, hasFallbackReason)
}

func TestAppendResponsesCompactLogInfoRecordsNativeAttemptForErrorLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 8, 0, 0, 0, time.UTC)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:                           dto.ResponsesCompactModeAuto,
				ResponsesCompactAutoFallbackRetryIntervalHours: 4,
			},
		},
	}
	MarkResponsesCompactNativeFallback(ctx, info, 503, "status_code=503, first channel unavailable", now)
	MarkResponsesCompactErrorLog(ctx, true)

	content, annotatedOther := AppendResponsesCompactLogInfo(ctx, info, nil, nil, now)

	require.Equal(t, []string{
		"Responses Compact mode=native setting=auto path=/v1/responses/compact native_path=/v1/responses/compact native_status=503 fallback_reason=status_code=503, first channel unavailable",
	}, content)
	require.Equal(t, true, annotatedOther["responses_compact_native_attempted"])
	require.Equal(t, "/v1/responses/compact", annotatedOther["responses_compact_native_upstream_path"])
	require.Equal(t, 503, annotatedOther["responses_compact_native_status_code"])
	require.Equal(t, "status_code=503, first channel unavailable", annotatedOther["responses_compact_auto_fallback_reason"])
	require.Equal(t, int64(1779796800), annotatedOther["responses_compact_auto_fallback_retry_until"])
	require.Equal(t, 4, annotatedOther["responses_compact_auto_fallback_retry_interval_hours"])

	MarkResponsesCompactErrorLog(ctx, false)
	content, annotatedOther = AppendResponsesCompactLogInfo(ctx, info, nil, nil, now)
	require.Equal(t, []string{
		"Responses Compact mode=native setting=auto path=/v1/responses/compact",
	}, content)
	_, hasNativeAttempt := annotatedOther["responses_compact_native_attempted"]
	require.False(t, hasNativeAttempt)
}

func TestAppendResponsesCompactLogInfoSkipsNativeAttemptFromDifferentChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 8, 0, 0, 0, time.UTC)
	firstChannel := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelId:   183,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeAuto,
			},
		},
	}
	nextChannel := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelId:   199,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeAuto,
			},
		},
	}
	MarkResponsesCompactNativeFallback(ctx, firstChannel, 503, "status_code=503, first channel unavailable", now)
	MarkResponsesCompactErrorLog(ctx, true)

	content, annotatedOther := AppendResponsesCompactLogInfo(ctx, nextChannel, nil, nil, now)

	require.Equal(t, []string{
		"Responses Compact mode=native setting=auto path=/v1/responses/compact",
	}, content)
	_, hasNativeAttempt := annotatedOther["responses_compact_native_attempted"]
	require.False(t, hasNativeAttempt)
	_, hasNativeChannel := annotatedOther["responses_compact_native_channel_id"]
	require.False(t, hasNativeChannel)
}

func TestAppendResponsesCompactLogInfoRecordsAutoFallbackWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 8, 0, 0, 0, time.UTC)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:                           dto.ResponsesCompactModeAuto,
				ResponsesCompactAutoFallbackAt:                 now.Add(-time.Hour).Unix(),
				ResponsesCompactAutoFallbackReason:             "status_code=503, upstream unavailable\nwith whitespace",
				ResponsesCompactAutoFallbackRetryIntervalHours: 3,
			},
		},
	}

	content, annotatedOther := AppendResponsesCompactLogInfo(ctx, info, nil, nil, now)

	require.Equal(t, []string{
		"Responses Compact mode=synthetic_summary setting=auto path=/v1/responses auto_fallback=true auto_fallback_window=true fallback_reason=status_code=503, upstream unavailable with whitespace",
	}, content)
	require.Equal(t, "synthetic_summary", annotatedOther["responses_compact_mode"])
	require.Equal(t, "auto", annotatedOther["responses_compact_setting"])
	require.Equal(t, "/v1/responses", annotatedOther["responses_compact_final_upstream_path"])
	require.Equal(t, true, annotatedOther["responses_compact_auto_fallback"])
	require.Equal(t, true, annotatedOther["responses_compact_auto_fallback_window"])
	require.Equal(t, "status_code=503, upstream unavailable with whitespace", annotatedOther["responses_compact_auto_fallback_reason"])
	require.Equal(t, now.Add(2*time.Hour).Unix(), annotatedOther["responses_compact_auto_fallback_retry_until"])
	require.Equal(t, 3, annotatedOther["responses_compact_auto_fallback_retry_interval_hours"])
	_, hasNativeAttempt := annotatedOther["responses_compact_native_attempted"]
	require.False(t, hasNativeAttempt)
}

func TestAppendResponsesCompactLogInfoRecordsLegacyAutoFallbackDateWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	now := time.Date(2026, time.May, 26, 8, 0, 0, 0, time.UTC)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:             dto.ResponsesCompactModeAuto,
				ResponsesCompactAutoFallbackDate: dto.ResponsesCompactAutoFallbackDate(now),
			},
		},
	}

	_, annotatedOther := AppendResponsesCompactLogInfo(ctx, info, nil, nil, now)

	require.Equal(t, true, annotatedOther["responses_compact_auto_fallback_window"])
	require.Equal(t, time.Date(2026, time.May, 27, 0, 0, 0, 0, time.UTC).Unix(), annotatedOther["responses_compact_auto_fallback_retry_until"])
	require.Equal(t, 3, annotatedOther["responses_compact_auto_fallback_retry_interval_hours"])
}

func TestResponsesCompactAutoFallbackRetryUntilRejectsInvalidLegacyDate(t *testing.T) {
	now := time.Date(2026, time.February, 1, 8, 0, 0, 0, time.UTC)
	settings := dto.ChannelOtherSettings{
		ResponsesCompactAutoFallbackDate: 20260230,
	}

	require.Zero(t, responsesCompactAutoFallbackRetryUntil(settings, now))
}

func TestCalculateTextQuotaSummaryHandlesLegacyClaudeDerivedOpenAIUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      5,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo:       types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     62,
		CompletionTokens: 95,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3544,
		},
		ClaudeCacheCreation5mTokens: 586,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 62 + 3544*0.1 + 586*1.25 + 95*5 = 1624.9 => 1624
	require.Equal(t, 1624, summary.Quota)
}

func TestCalculateTextQuotaSummarySeparatesOpenRouterCacheReadFromPromptBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "openai/gpt-4.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 2432,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// OpenRouter OpenAI-format display keeps prompt_tokens as total input,
	// but billing still separates normal input from cache read tokens.
	// quota = (2604 - 2432) + 2432*0.1 + 383 = 798.2 => 798
	require.Equal(t, 2604, summary.PromptTokens)
	require.Equal(t, 798, summary.Quota)
}

func TestCalculateTextQuotaSummarySeparatesOpenRouterCacheCreationFromPromptBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "openai/gpt-4.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 100,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// prompt_tokens is still logged as total input, but cache creation is billed separately.
	// quota = (2604 - 100) + 100*1.25 + 383 = 3012
	require.Equal(t, 2604, summary.PromptTokens)
	require.Equal(t, 3012, summary.Quota)
}

func TestCalculateTextQuotaSummaryKeepsPrePRClaudeOpenRouterBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "anthropic/claude-3.7-sonnet",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 2432,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// Pre-PR PostClaudeConsumeQuota behavior for OpenRouter:
	// prompt = 2604 - 2432 = 172
	// quota = 172 + 2432*0.1 + 383 = 798.2 => 798
	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, 172, summary.PromptTokens)
	require.Equal(t, 798, summary.Quota)
}

func TestComposeTieredTextQuotaKeepsToolCallSurcharges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("image_generation_call", true)
	ctx.Set("image_generation_call_quality", "low")
	ctx.Set("image_generation_call_size", "1024x1024")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: &relaycommon.BuildInToolInfo{
					CallCount: 1,
				},
				dto.BuildInToolFileSearch: &relaycommon.BuildInToolInfo{
					CallCount: 2,
				},
			},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	quota := composeTieredTextQuota(relayInfo, summary, 1000, &billingexpr.TieredResult{
		ActualQuotaBeforeGroup: 1000,
		ActualQuotaAfterGroup:  1000,
	})

	require.Equal(t, int64(13000), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.Equal(t, 14000, quota)
}

func TestCalculateTextQuotaSummaryUsesResponsesImageGenerationCallCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("image_generation_call", true)
	ctx.Set("image_generation_call_quality", "low")
	ctx.Set("image_generation_call_size", "1024x1024")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {
					CallCount: 2,
					Quality:   "low",
					Size:      "1024x1024",
				},
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.Equal(t, 2, summary.ImageGenerationCallCount)
	require.Equal(t, operation_setting.GetGPTImage1PriceOnceCall("low", "1024x1024"), summary.ImageGenerationCallPrice)
	require.Equal(t, operation_setting.GetGPTImage1PriceOnceCall("low", "1024x1024")*2, summary.ImageGenerationTotal)
	require.Equal(t, int64(11000), summary.ToolCallSurchargeQuota.Round(0).IntPart())
}

func TestCalculateTextQuotaSummaryKeepsToolSurchargeWhenTotalTokensZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {
					CallCount: 1,
					Quality:   "low",
					Size:      "1024x1024",
				},
			},
		},
		StartTime: time.Now(),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, &dto.Usage{})

	require.Zero(t, summary.TotalTokens)
	require.Equal(t, 1, summary.ImageGenerationCallCount)
	require.True(t, summary.hasObservedNonTokenUsage())
	require.Greater(t, summary.ToolCallSurchargeQuota.Round(0).IntPart(), int64(0))
	require.Greater(t, summary.Quota, 0)
}

func TestAppendImageGenerationCallOtherInfoKeepsZeroPriceCalls(t *testing.T) {
	other := map[string]interface{}{}

	appendImageGenerationCallOtherInfo(other, textQuotaSummary{
		ImageGenerationCallCount: 1,
	})

	require.Equal(t, true, other["image_generation_call"])
	require.Equal(t, 1, other["image_generation_call_count"])
	require.Equal(t, float64(0), other["image_generation_call_price"])
	require.Equal(t, float64(0), other["image_generation_call_total_price"])
}

func TestCalculateTextQuotaSummaryChargesMixedResponsesImageGenerationSpecs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {
					CallCount: 2,
					Quality:   "low",
					Size:      "1024x1024",
					ImageCalls: map[string]int{
						relaycommon.ImageGenerationCallKey("low", "1024x1024"):  1,
						relaycommon.ImageGenerationCallKey("high", "1536x1024"): 1,
					},
				},
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	lowPrice := operation_setting.GetGPTImage1PriceOnceCall("low", "1024x1024")
	highPrice := operation_setting.GetGPTImage1PriceOnceCall("high", "1536x1024")

	require.Equal(t, 2, summary.ImageGenerationCallCount)
	require.Equal(t, lowPrice, summary.ImageGenerationCallPrice)
	require.Equal(t, lowPrice+highPrice, summary.ImageGenerationTotal)
	require.Equal(t, []imageGenerationCallDetail{
		{Quality: "high", Size: "1536x1024", Count: 1, Price: highPrice},
		{Quality: "low", Size: "1024x1024", Count: 1, Price: lowPrice},
	}, summary.ImageGenerationDetails)
	require.Equal(t, int64(130500), summary.ToolCallSurchargeQuota.Round(0).IntPart())
}

func TestCalculateTextQuotaSummaryBackfillsMissingResponsesImageGenerationDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {
					CallCount: 2,
					Quality:   "low",
					Size:      "1024x1024",
					ImageCalls: map[string]int{
						relaycommon.ImageGenerationCallKey("high", "1536x1024"): 1,
					},
				},
			},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	lowPrice := operation_setting.GetGPTImage1PriceOnceCall("low", "1024x1024")

	require.Equal(t, 2, summary.ImageGenerationCallCount)
	require.Equal(t, lowPrice*2, summary.ImageGenerationTotal)
	require.Equal(t, []imageGenerationCallDetail{
		{Quality: "low", Size: "1024x1024", Count: 2, Price: lowPrice},
	}, summary.ImageGenerationDetails)
}

func TestCalculateTextQuotaSummaryCapsExcessResponsesImageGenerationDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {
					CallCount: 1,
					Quality:   "low",
					Size:      "1024x1024",
					ImageCalls: map[string]int{
						relaycommon.ImageGenerationCallKey("high", "1536x1024"): 1,
						relaycommon.ImageGenerationCallKey("low", "1024x1024"):  1,
					},
				},
			},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	lowPrice := operation_setting.GetGPTImage1PriceOnceCall("low", "1024x1024")

	require.Equal(t, 1, summary.ImageGenerationCallCount)
	require.Equal(t, lowPrice, summary.ImageGenerationTotal)
	require.Equal(t, []imageGenerationCallDetail{
		{Quality: "low", Size: "1024x1024", Count: 1, Price: lowPrice},
	}, summary.ImageGenerationDetails)
}

func TestCalculateTextQuotaSummaryFallsBackForInvalidResponsesImageGenerationDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {
					CallCount: 1,
					Quality:   "low",
					Size:      "1024x1024",
					ImageCalls: map[string]int{
						relaycommon.ImageGenerationCallKey("high", "1536x1024"): 2,
						relaycommon.ImageGenerationCallKey("low", "1024x1024"):  -1,
					},
				},
			},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	lowPrice := operation_setting.GetGPTImage1PriceOnceCall("low", "1024x1024")

	require.Equal(t, 1, summary.ImageGenerationCallCount)
	require.Equal(t, lowPrice, summary.ImageGenerationTotal)
	require.Equal(t, []imageGenerationCallDetail{
		{Quality: "low", Size: "1024x1024", Count: 1, Price: lowPrice},
	}, summary.ImageGenerationDetails)
}

func TestComposeTieredTextQuotaFallbackKeepsToolCallSurcharges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("claude_web_search_requests", 2)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1.25},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1.25,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	quota := composeTieredTextQuota(relayInfo, summary, 1250, nil)

	require.Equal(t, int64(12500), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.Equal(t, 13750, quota)
}

func TestComposeTieredTextQuotaErrorFallbackUsesPreConsumedQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("claude_web_search_requests", 2)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1.25},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1.25,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// tieredResult=nil simulates a settlement error where TryTieredSettle
	// falls back to FinalPreConsumedQuota (2000), which differs from
	// EstimatedQuotaBeforeGroup * GroupRatio (1250).
	preConsumedFallback := 2000
	quota := composeTieredTextQuota(relayInfo, summary, preConsumedFallback, nil)

	require.Equal(t, int64(12500), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.Equal(t, 14500, quota)
}
