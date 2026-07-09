package common

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSafeElapsedSeconds(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)

	testCases := []struct {
		name     string
		start    time.Time
		end      time.Time
		expected int64
	}{
		{
			name:     "positive",
			start:    base,
			end:      base.Add(5 * time.Second),
			expected: 5,
		},
		{
			name:     "negative clamps to zero",
			start:    base.Add(2 * time.Second),
			end:      base,
			expected: 0,
		},
		{
			name:     "zero time clamps to zero",
			start:    time.Time{},
			end:      base,
			expected: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeElapsedSeconds(tc.start, tc.end)
			if got != tc.expected {
				t.Fatalf("expected %d, got %d", tc.expected, got)
			}
		})
	}
}

func TestRelayInfoFirstResponseLatencyMs(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)

	t.Run("missing first response", func(t *testing.T) {
		info := &RelayInfo{StartTime: base}
		latency, ok := info.FirstResponseLatencyMs()
		if ok || latency != 0 {
			t.Fatalf("expected no valid latency, got latency=%d ok=%v", latency, ok)
		}
	})

	t.Run("valid zero latency", func(t *testing.T) {
		info := &RelayInfo{
			StartTime:         base,
			FirstResponseTime: base,
		}
		latency, ok := info.FirstResponseLatencyMs()
		if !ok || latency != 0 {
			t.Fatalf("expected zero latency, got latency=%d ok=%v", latency, ok)
		}
	})

	t.Run("positive latency", func(t *testing.T) {
		info := &RelayInfo{
			StartTime:         base,
			FirstResponseTime: base.Add(1500 * time.Millisecond),
		}
		latency, ok := info.FirstResponseLatencyMs()
		if !ok || latency != 1500 {
			t.Fatalf("expected latency=1500 ok=true, got latency=%d ok=%v", latency, ok)
		}
	})

	t.Run("future start invalidates negative latency", func(t *testing.T) {
		info := &RelayInfo{
			StartTime:         base.Add(2 * time.Second),
			FirstResponseTime: base,
		}
		latency, ok := info.FirstResponseLatencyMs()
		if ok || latency != 0 {
			t.Fatalf("expected invalid latency for future start time, got latency=%d ok=%v", latency, ok)
		}
	})
}

func TestRelayInfoSetFirstResponseTimeAtIgnoresInvalidEarlyTimestamp(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	info := &RelayInfo{
		StartTime:       base,
		isFirstResponse: true,
	}

	info.SetFirstResponseTimeAt(base.Add(-time.Second))
	if !info.FirstResponseTime.IsZero() {
		t.Fatalf("expected invalid early timestamp to be ignored, got %v", info.FirstResponseTime)
	}
	if !info.isFirstResponse {
		t.Fatal("expected first-response latch to remain open after invalid timestamp")
	}

	valid := base.Add(2 * time.Second)
	info.SetFirstResponseTimeAt(valid)
	if !info.FirstResponseTime.Equal(valid) {
		t.Fatalf("expected valid timestamp %v, got %v", valid, info.FirstResponseTime)
	}
	if info.isFirstResponse {
		t.Fatal("expected first-response latch to close after valid timestamp")
	}
}

func TestRelayInfoUpstreamLatencyMs(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	info := &RelayInfo{}

	info.SetUpstreamRequestStartTimeAt(base)
	info.SetUpstreamHeaderTimeAt(base.Add(120 * time.Millisecond))
	info.SetUpstreamFirstByteTimeAt(base.Add(250 * time.Millisecond))
	info.SetUpstreamEndTimeAt(base.Add(900 * time.Millisecond))

	headerMs, ok := info.UpstreamHeaderLatencyMs()
	require.True(t, ok)
	require.Equal(t, int64(120), headerMs)

	ttfbMs, ok := info.UpstreamFirstByteLatencyMs()
	require.True(t, ok)
	require.Equal(t, int64(250), ttfbMs)

	totalMs, ok := info.UpstreamTotalLatencyMs()
	require.True(t, ok)
	require.Equal(t, int64(900), totalMs)
}

func TestRelayInfoUpstreamLatencyMsIgnoresInvalidTimes(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	info := &RelayInfo{}

	info.SetUpstreamHeaderTimeAt(base)
	require.True(t, info.UpstreamHeaderTime.IsZero())

	info.SetUpstreamRequestStartTimeAt(base)
	info.SetUpstreamEndTimeAt(base.Add(-time.Second))
	require.True(t, info.UpstreamEndTime.IsZero())

	latency, ok := info.UpstreamTotalLatencyMs()
	require.False(t, ok)
	require.Zero(t, latency)
}

func TestRelayInfoUpstreamRequestStartResetsPreviousAttemptTiming(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	info := &RelayInfo{}

	info.SetUpstreamRequestStartTimeAt(base)
	info.SetUpstreamHeaderTimeAt(base.Add(120 * time.Millisecond))
	info.SetUpstreamFirstByteTimeAt(base.Add(250 * time.Millisecond))
	info.SetUpstreamEndTimeAt(base.Add(900 * time.Millisecond))

	nextStart := base.Add(2 * time.Second)
	info.SetUpstreamRequestStartTimeAt(nextStart)

	require.True(t, info.UpstreamHeaderTime.IsZero())
	require.True(t, info.UpstreamFirstByteTime.IsZero())
	require.True(t, info.UpstreamEndTime.IsZero())
	require.Equal(t, nextStart, info.UpstreamRequestStart)
}

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoSyntheticCompactScopeUsesOriginModel(t *testing.T) {
	info := &RelayInfo{
		UserId:          10,
		TokenId:         20,
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &ChannelMeta{
			ChannelId:   163,
			ChannelType: 1,
		},
	}

	scope := info.SyntheticCompactScope()

	require.Equal(t, "gpt-5.5-openai-compact", scope.Model)
	require.Equal(t, "default", scope.Group)
	require.Equal(t, 163, scope.ChannelID)
}

func TestInitChannelMetaDoesNotEnableStreamOptionsForAgnes(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rootcommon.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeAgnes)

	info := &RelayInfo{}
	info.InitChannelMeta(ctx)

	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, constant.ChannelTypeAgnes, info.ChannelType)
	require.False(t, info.SupportStreamOptions)
}

func TestInitChannelMetaSynchronizesChannelOtherSettings(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	rootcommon.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	rootcommon.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
	})

	info := &RelayInfo{}
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	info.InitChannelMeta(ctx)

	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, dto.ResponsesCompactModeSynthetic, info.ChannelMeta.ChannelOtherSettings.ResponsesCompactMode)
	require.Equal(t, dto.ResponsesCompactModeSynthetic, info.ChannelOtherSettings.ResponsesCompactMode)
	require.True(t, IsSyntheticOpenAICompatibleResponsesCompact(info))
}

func TestGenRelayInfoResponsesCompactionInitializesConversionChain(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	request := &dto.OpenAIResponsesCompactionRequest{
		Model: "gpt-5.4-mini",
		Input: json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]}]`),
	}

	info, err := GenRelayInfo(ctx, types.RelayFormatOpenAIResponsesCompaction, request, nil)

	require.NoError(t, err)
	require.Equal(t, []types.RelayFormat{types.RelayFormatOpenAIResponsesCompaction}, info.RequestConversionChain)
	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponsesCompaction), info.GetFinalRequestRelayFormat())
}

func TestGenRelayInfoResponsesCapturesImageGenerationToolMetadata(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Tools: json.RawMessage(`[{"type":"image_generation","quality":"high","size":"1536x1024"}]`),
	}

	info := GenRelayInfoResponses(ctx, request)

	require.NotNil(t, info.ResponsesUsageInfo)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, "image_generation", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].ToolName)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Quality)
	require.Equal(t, "1536x1024", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Size)
}

func TestGenRelayInfoResponsesCapturesWebSearchPreviewToolMetadata(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Tools: json.RawMessage(`[{"type":"web_search_preview","search_context_size":"high"}]`),
	}

	info := GenRelayInfoResponses(ctx, request)

	require.NotNil(t, info.ResponsesUsageInfo)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, "web_search_preview", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].ToolName)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
}

func TestGenRelayInfoResponsesDefaultsWebSearchPreviewContextSize(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Tools: json.RawMessage(`[{"type":"web_search_preview"}]`),
	}

	info := GenRelayInfoResponses(ctx, request)

	require.NotNil(t, info.ResponsesUsageInfo)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, "web_search_preview", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].ToolName)
	require.Equal(t, "medium", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
	require.True(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].DefaultSearchSize)
}

func TestGenRelayInfoResponsesCanonicalizesVersionedWebSearchPreviewTool(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Tools: json.RawMessage(`[{"type":"web_search_preview_2025_03_11","search_context_size":"high"}]`),
	}

	info := GenRelayInfoResponses(ctx, request)

	require.NotNil(t, info.ResponsesUsageInfo)
	require.Len(t, info.ResponsesUsageInfo.BuiltInTools, 1)
	require.Equal(t, "web_search_preview", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].ToolName)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
	require.False(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].DefaultSearchSize)
}

func TestGenRelayInfoResponsesIgnoresOrdinaryToolDefinitions(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Tools: json.RawMessage(`[
			{"type":"function","name":"read_file"},
			{"type":"custom","name":"shell"},
			{"type":"tool_search"},
			{"type":"namespace","name":"codex_app","tools":[{"type":"function","name":"read_thread"}]},
			{"type":"web_search","search_context_size":"low"}
		]`),
	}

	info := GenRelayInfoResponses(ctx, request)

	require.NotNil(t, info.ResponsesUsageInfo)
	require.Len(t, info.ResponsesUsageInfo.BuiltInTools, 1)
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, dto.BuildInToolWebSearchPreview)
	require.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, "function")
	require.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, "custom")
	require.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, "tool_search")
	require.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, "namespace")
	require.Equal(t, "low", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
}

func TestGenRelayInfoResponsesCapturesFileSearchDefinition(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Tools: json.RawMessage(`[{"type":"file_search"}]`),
	}

	info := GenRelayInfoResponses(ctx, request)

	require.NotNil(t, info.ResponsesUsageInfo)
	require.Len(t, info.ResponsesUsageInfo.BuiltInTools, 1)
	require.Equal(t, dto.BuildInToolFileSearch, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].ToolName)
	require.Equal(t, 0, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}
