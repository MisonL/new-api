package codex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestRestoresSyntheticCompactState(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		UserId:      10,
		TokenId:     20,
		UsingGroup:  "default",
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	scope := service.SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}
	compactResp, _, err := service.BuildSyntheticCompactResponse(context.Background(), scope, "gpt-5.5", dto.OpenAIResponsesResponse{
		CreatedAt: 1710000000,
		Model:     "gpt-5.5",
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "Stored codex switch summary."},
				},
			},
		},
	})
	require.NoError(t, err)
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: compactResp.ID,
		Input:              common.RawMessage(`"continue"`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Empty(t, convertedReq.PreviousResponseID)
	require.Contains(t, string(convertedReq.Input), "Stored codex switch summary.")
	require.JSONEq(t, `false`, string(convertedReq.Store))
}

func TestConvertOpenAIResponsesCompactRestoresSyntheticCompactState(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		UserId:          10,
		TokenId:         20,
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "gpt-5.5",
		},
	}
	scope := service.SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}
	compactResp, _, err := service.BuildSyntheticCompactResponse(context.Background(), scope, "gpt-5.5", dto.OpenAIResponsesResponse{
		CreatedAt: 1710000000,
		Model:     "gpt-5.5",
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "Stored codex compact summary."},
				},
			},
		},
	})
	require.NoError(t, err)
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: compactResp.ID,
		Input:              common.RawMessage(`"continue after compact"`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Empty(t, convertedReq.PreviousResponseID)
	require.Contains(t, string(convertedReq.Input), "Stored codex compact summary.")
	require.Contains(t, string(convertedReq.Input), "continue after compact")
	require.Equal(t, "cleared_by_synthetic_restore", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactStateRestored))
}

func TestConvertOpenAIResponsesRequestPropagatesSyntheticCompactErrors(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		UserId:      10,
		TokenId:     20,
		UsingGroup:  "default",
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_newapi_synthcmp_missing",
		Input:              common.RawMessage(`[]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.Nil(t, converted)
	require.ErrorIs(t, err, service.ErrSyntheticCompactStateNotFound)
	require.Equal(t, "missing_local_synthetic_state", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
}

func TestConvertOpenAIResponsesRequestContinuesMissingSyntheticStateWithVisibleInput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		UserId:      10,
		TokenId:     20,
		UsingGroup:  "default",
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_newapi_synthcmp_missing",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_missing"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue with visible context"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Empty(t, convertedReq.PreviousResponseID)
	require.Contains(t, string(convertedReq.Input), "continue with visible context")
	require.NotContains(t, string(convertedReq.Input), "newapi.synthetic.compact")
	require.JSONEq(t, `false`, string(convertedReq.Store))
	require.Equal(t, "stale_local_synthetic_state_visible_only", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactVisibleOnlyFallbackAttempted))
}

func codexLargeCompactTestText() string {
	return strings.Repeat("token ", codexGPT55CompactContextLimitTokens/2+2048)
}

func codexHugeCompactTestText() string {
	return strings.Repeat("token ", codexGPT55CompactContextLimitTokens+2048)
}

func TestConvertOpenAIResponsesCompactGPT55PrunesOldToolContext(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "gpt-5.5",
		},
	}
	info.SetEstimatePromptTokens(codexGPT55CompactContextLimitTokens + 1)
	largePayload := codexLargeCompactTestText()
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_old","name":"read_file","arguments":"large old tool input %s"},
			{"type":"custom_tool_call_output","call_id":"call_old","output":"large old tool output %s"},
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"keep visible context"}]}
		]`, largePayload, largePayload)),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "gpt-5.5", convertedReq.Model)
	require.NotContains(t, string(convertedReq.Input), "large old tool")
	require.Contains(t, string(convertedReq.Input), `"type":"compaction"`)
	require.Contains(t, string(convertedReq.Input), "keep visible context")
	require.False(t, c.GetBool("responses_compact_context_fallback_attempted"))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned))
	require.Equal(t, "codex_compact_tool_context_pruned", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactRouteDecision))
	require.Equal(t, "codex_gpt55_context_limit", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactFallbackReason))
	require.Less(t, info.GetEstimatePromptTokens(), codexGPT55CompactTargetTokens)
}

func TestConvertOpenAIResponsesCompactGPT55PrunesToolCallGroups(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "gpt-5.5",
		},
	}
	info.SetEstimatePromptTokens(codexGPT55CompactContextLimitTokens + 1)
	largePayload := codexLargeCompactTestText()
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_old","name":"read_file","arguments":"large old tool input %s"},
			{"type":"function_call_output","call_id":"call_old","output":"large old tool output %s"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"keep assistant message"}]},
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"keep visible context"}]}
		]`, largePayload, largePayload)),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.NotContains(t, string(convertedReq.Input), `"call_id":"call_old"`)
	require.NotContains(t, string(convertedReq.Input), "large old tool output")
	require.Contains(t, string(convertedReq.Input), "keep assistant message")
	require.Contains(t, string(convertedReq.Input), "keep visible context")
	require.Equal(t, "codex_compact_tool_context_pruned", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactRouteDecision))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned))
}

func TestConvertOpenAIResponsesCompactGPT55PrunesToolContextWhenUpstreamModelMissing(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeCodex,
		},
	}
	info.SetEstimatePromptTokens(codexGPT55CompactContextLimitTokens + 1)
	largePayload := codexLargeCompactTestText()
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5-openai-compact",
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_old","name":"read_file","arguments":"large old tool input %s"},
			{"type":"function_call_output","call_id":"call_old","output":"large old tool output %s"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"keep visible context"}]}
		]`, largePayload, largePayload)),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "gpt-5.5-openai-compact", convertedReq.Model)
	require.NotContains(t, string(convertedReq.Input), "large old tool")
	require.Contains(t, string(convertedReq.Input), "keep visible context")
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned))
}

func TestConvertOpenAIResponsesCompactGPT55ReestimatesHighCachedEstimateBeforeFallback(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "gpt-5.5",
		},
	}
	info.SetEstimatePromptTokens(codexGPT55CompactContextLimitTokens + 1)
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"small visible context"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "gpt-5.5", convertedReq.Model)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactSummaryModelFallbackAttempted))
}

func TestConvertOpenAIResponsesCompactGPT55PrunesEmptyCallIDToolItemsByBudget(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "gpt-5.5",
		},
	}
	info.SetEstimatePromptTokens(codexGPT55CompactContextLimitTokens + 1)
	largePayload := codexLargeCompactTestText()
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","name":"read_file","arguments":"large first tool input %s"},
			{"type":"function_call_output","output":"large second tool output %s"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"keep visible context"}]}
		]`, largePayload, largePayload)),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.NotContains(t, string(convertedReq.Input), "large first tool input")
	require.Contains(t, string(convertedReq.Input), "large second tool output")
	require.Contains(t, string(convertedReq.Input), "keep visible context")
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned))
}

func TestRemoveOldestCodexCompactToolItemsKeepsDistinctEmptyCallIDItems(t *testing.T) {
	items := []common.RawMessage{
		common.RawMessage(`{"type":"function_call","name":"read_file","arguments":"remove first"}`),
		common.RawMessage(`{"type":"function_call_output","output":"keep second"}`),
		common.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_text","text":"keep visible"}]}`),
	}

	nextItems, removed := removeOldestCodexCompactToolItems(items, "gpt-5.5", 1, 0)

	require.Equal(t, 1, removed)
	require.Len(t, nextItems, 2)
	encodedItems, err := common.Marshal(nextItems)
	require.NoError(t, err)
	require.NotContains(t, string(encodedItems), "remove first")
	require.Contains(t, string(encodedItems), "keep second")
}

func TestConvertOpenAIResponsesCompactGPT55ReestimatesAfterPriorContextFallback(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactCodexContextPruned, true)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "gpt-5.5",
		},
	}
	info.SetEstimatePromptTokens(codexGPT55CompactTargetTokens - 1)
	largePayload := strings.Repeat("token ", codexGPT55CompactContextLimitTokens+1024)
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_old","name":"read_file","arguments":"%s"},
			{"type":"function_call_output","call_id":"call_old","output":"%s"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"keep visible context"}]}
		]`, largePayload, largePayload)),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.NotContains(t, string(convertedReq.Input), "large old tool")
	require.Contains(t, string(convertedReq.Input), "keep visible context")
	require.Equal(t, "codex_compact_tool_context_pruned", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactRouteDecision))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned))
}

func TestConvertOpenAIResponsesCompactGPT55FallsBackToGPT54WhenToolPruneCannotFit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "gpt-5.5",
		},
	}
	info.SetEstimatePromptTokens(codexGPT55CompactContextLimitTokens + 1)
	largePayload := codexHugeCompactTestText()
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"visible context without tool items %s"}]}
		]`, largePayload)),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "gpt-5.4", convertedReq.Model)
	require.Equal(t, "gpt-5.4", info.UpstreamModelName)
	require.Equal(t, "gpt-5.4", info.ChannelMeta.UpstreamModelName)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned))
	require.False(t, c.GetBool("responses_compact_context_fallback_attempted"))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactSummaryModelFallbackAttempted))
	require.Equal(t, "gpt-5.4", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactSummaryModel))
	require.Equal(t, "codex_compact_model_fallback", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactRouteDecision))
}

func TestConvertOpenAIResponsesCompactSafeguardSkipsNonGPT55(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.4-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "gpt-5.4",
		},
	}
	info.SetEstimatePromptTokens(codexGPT55CompactContextLimitTokens + 1)
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.4",
		Input: common.RawMessage(`[
			{"type":"function_call","call_id":"call_old","name":"read_file","arguments":"must stay"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"keep visible context"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "gpt-5.4", convertedReq.Model)
	require.Contains(t, string(convertedReq.Input), "must stay")
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned))
	require.False(t, c.GetBool("responses_compact_context_fallback_attempted"))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactSummaryModelFallbackAttempted))
}

func TestConvertOpenAIResponsesCompactSafeguardSkipsAlreadyFallbackModel(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "gpt-5.5",
		},
	}
	info.SetEstimatePromptTokens(codexGPT55CompactContextLimitTokens + 1)
	largePayload := codexLargeCompactTestText()
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.4",
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"function_call","call_id":"call_old","name":"read_file","arguments":"large old tool input %s"},
			{"type":"function_call_output","call_id":"call_old","output":"large old tool output %s"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"keep visible context"}]}
		]`, largePayload, largePayload)),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "gpt-5.4", convertedReq.Model)
	require.Contains(t, string(convertedReq.Input), "large old tool input")
	require.Equal(t, "gpt-5.5", info.ChannelMeta.UpstreamModelName)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactSummaryModelFallbackAttempted))
}

func TestConvertOpenAIResponsesRequestPrependsSystemPromptToStringInstructions(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				SystemPrompt:         "configured system",
				SystemPromptOverride: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:        "gpt-5.5",
		Input:        common.RawMessage(`"continue"`),
		Instructions: common.RawMessage(`"original task"`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, `"configured system\noriginal task"`, string(convertedReq.Instructions))
}

func TestConvertOpenAIResponsesRequestKeepsStructuredInstructionsOnSystemPromptOverride(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				SystemPrompt:         "configured system",
				SystemPromptOverride: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`"continue"`),
		Instructions: common.RawMessage(`{
			"role":"developer",
			"content":[{"type":"input_text","text":"preserve structured task"}]
		}`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, string(req.Instructions), string(convertedReq.Instructions))
}
