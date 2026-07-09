package codex

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

const (
	codexGPT55CompactModel              = "gpt-5.5"
	codexGPT55CompactFallbackModel      = "gpt-5.4"
	codexGPT55CompactContextLimitTokens = 258000
	codexGPT55CompactTargetTokens       = 250000
)

type codexCompactPruneResult struct {
	InitialTokens int
	FinalTokens   int
	RemovedItems  int
}

func applyCodexCompactV2ContextSafeguard(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, error) {
	if !shouldApplyCodexGPT55CompactSafeguard(info, request) {
		return request, nil
	}

	initialTokens := info.GetEstimatePromptTokens()
	if shouldReestimateCodexCompactTokens(c, initialTokens) {
		initialTokens = estimateCodexCompactTokens(request, request.Model)
	}
	if initialTokens <= codexGPT55CompactContextLimitTokens {
		return request, nil
	}

	prunedRequest, pruneResult, err := pruneCodexCompactToolContext(request, initialTokens, codexGPT55CompactTargetTokens)
	if err != nil {
		return request, err
	}
	request = prunedRequest

	if pruneResult.RemovedItems > 0 {
		markCodexCompactContextPruned(c, info, pruneResult)
		info.SetEstimatePromptTokens(pruneResult.FinalTokens)
	}
	if pruneResult.FinalTokens > codexGPT55CompactTargetTokens {
		markCodexCompactModelFallback(c, info, pruneResult.FinalTokens)
		request.Model = codexGPT55CompactFallbackModel
	}
	return request, nil
}

func shouldApplyCodexGPT55CompactSafeguard(info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) bool {
	if info == nil || info.RelayMode != relayconstant.RelayModeResponsesCompact || info.ChannelType != constant.ChannelTypeCodex {
		return false
	}
	if codexCompactCurrentModelIsFallback(info, request) {
		return false
	}
	for _, model := range codexCompactModelCandidates(info, request) {
		if codexCompactModelIsGPT55(model) {
			return true
		}
	}
	return false
}

func shouldReestimateCodexCompactTokens(c *gin.Context, estimatedTokens int) bool {
	if estimatedTokens <= 0 || estimatedTokens > codexGPT55CompactContextLimitTokens {
		return true
	}
	return c != nil && common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactCodexContextPruned)
}

func codexCompactCurrentModelIsFallback(info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) bool {
	if codexCompactModelIsFallback(request.Model) {
		return true
	}
	return info != nil && codexCompactModelIsFallback(info.UpstreamModelName)
}

func codexCompactModelIsFallback(model string) bool {
	model = strings.TrimSpace(model)
	if model == codexGPT55CompactFallbackModel {
		return true
	}
	baseModel, ok := ratio_setting.CompactBaseModelName(model)
	return ok && baseModel == codexGPT55CompactFallbackModel
}

func codexCompactModelIsGPT55(model string) bool {
	if strings.TrimSpace(model) == codexGPT55CompactModel {
		return true
	}
	if baseModel, ok := ratio_setting.CompactBaseModelName(model); ok {
		return baseModel == codexGPT55CompactModel
	}
	return false
}

func codexCompactModelCandidates(info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) []string {
	candidates := []string{request.Model}
	if info != nil {
		candidates = append(candidates, info.UpstreamModelName, info.OriginModelName)
		if info.ChannelMeta != nil {
			candidates = append(candidates, info.ChannelMeta.UpstreamModelName)
		}
	}
	out := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func pruneCodexCompactToolContext(request dto.OpenAIResponsesRequest, initialTokens int, targetTokens int) (dto.OpenAIResponsesRequest, codexCompactPruneResult, error) {
	result := codexCompactPruneResult{
		InitialTokens: initialTokens,
		FinalTokens:   initialTokens,
	}
	if initialTokens <= targetTokens || common.GetJsonType(request.Input) != "array" {
		return request, result, nil
	}

	var items []common.RawMessage
	if err := common.Unmarshal(request.Input, &items); err != nil {
		return request, result, nil
	}

	for result.FinalTokens > targetTokens {
		nextItems, removed := removeOldestCodexCompactToolItems(items, request.Model, result.FinalTokens, targetTokens)
		if removed == 0 {
			break
		}
		var err error
		request.Input, err = common.Marshal(nextItems)
		if err != nil {
			return request, result, fmt.Errorf("marshal pruned codex compact input: %w", err)
		}
		result.RemovedItems += removed
		result.FinalTokens = estimateCodexCompactTokens(request, request.Model)
		items = nextItems
	}
	return request, result, nil
}

func removeOldestCodexCompactToolItems(items []common.RawMessage, model string, remainingTokenEstimate int, targetTokens int) ([]common.RawMessage, int) {
	selectedGroups := make(map[string]struct{})
	out := make([]common.RawMessage, 0, len(items))
	removed := 0
	for index, rawItem := range items {
		remove := false
		if isCodexCompactToolContextItem(rawItem) {
			groupKey := codexCompactItemGroupKey(rawItem, index)
			_, alreadySelected := selectedGroups[groupKey]
			remove = remainingTokenEstimate > targetTokens || alreadySelected
			if remove {
				selectedGroups[groupKey] = struct{}{}
			}
		}
		if remove {
			removed++
			remainingTokenEstimate -= max(1, service.EstimateTokenByModel(model, string(rawItem)))
			continue
		}
		out = append(out, rawItem)
	}
	return out, removed
}

func isCodexCompactToolContextItem(rawItem common.RawMessage) bool {
	if common.GetJsonType(rawItem) != "object" {
		return false
	}
	var item map[string]common.RawMessage
	if err := common.Unmarshal(rawItem, &item); err != nil {
		return false
	}
	itemType := strings.TrimSpace(rawStringField(item["type"]))
	if itemType == "" || relaycommon.IsResponsesCompactionItemType(itemType) || relaycommon.IsResponsesCompactionTriggerItemType(itemType) {
		return false
	}
	if itemType == "message" {
		return false
	}
	return isCodexToolLikeItemType(itemType)
}

func isCodexToolLikeItemType(itemType string) bool {
	switch itemType {
	case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
		return true
	}
	return false
}

func codexCompactItemCallID(rawItem common.RawMessage) string {
	if common.GetJsonType(rawItem) != "object" {
		return ""
	}
	var item map[string]common.RawMessage
	if err := common.Unmarshal(rawItem, &item); err != nil {
		return ""
	}
	return strings.TrimSpace(rawStringField(item["call_id"]))
}

func codexCompactItemGroupKey(rawItem common.RawMessage, index int) string {
	if callID := codexCompactItemCallID(rawItem); callID != "" {
		return "call_id:" + callID
	}
	return fmt.Sprintf("item_index:%d", index)
}

func estimateCodexCompactTokens(request dto.OpenAIResponsesRequest, model string) int {
	parts := make([]string, 0, 6)
	appendRaw := func(raw common.RawMessage) {
		if trimmed := strings.TrimSpace(string(raw)); trimmed != "" && trimmed != "null" {
			parts = append(parts, trimmed)
		}
	}
	appendRaw(request.Instructions)
	appendRaw(request.Input)
	appendRaw(request.Tools)
	appendRaw(request.ParallelToolCalls)
	if request.Reasoning != nil {
		if raw, err := common.Marshal(request.Reasoning); err == nil {
			appendRaw(raw)
		}
	}
	appendRaw(request.Text)
	return service.EstimateTokenByModel(model, strings.Join(parts, "\n"))
}

func markCodexCompactContextPruned(c *gin.Context, info *relaycommon.RelayInfo, result codexCompactPruneResult) {
	service.MarkResponsesCompactFallbackAttempt(c, info, service.ResponsesCompactFallbackAttemptContext, nil)
	if c == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeyResponsesCompactCodexContextPruned, true)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactRouteDecision, "codex_compact_tool_context_pruned")
	common.SetContextKey(c, constant.ContextKeyResponsesCompactFallbackReason, "codex_gpt55_context_limit")
	common.SysLog(fmt.Sprintf(
		"codex compact context pruned: model=%s initial_tokens=%d final_tokens=%d removed_tool_items=%d",
		codexGPT55CompactModel,
		result.InitialTokens,
		result.FinalTokens,
		result.RemovedItems,
	))
}

func markCodexCompactModelFallback(c *gin.Context, info *relaycommon.RelayInfo, finalTokens int) {
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModelFallbackAttempted, true)
		common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, codexGPT55CompactFallbackModel)
		common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModels, []string{codexGPT55CompactFallbackModel})
		common.SetContextKey(c, constant.ContextKeyResponsesCompactRouteDecision, "codex_compact_model_fallback")
		common.SetContextKey(c, constant.ContextKeyResponsesCompactFallbackReason, "codex_gpt55_context_limit")
		service.MarkResponsesCompactFallbackAttempt(c, info, service.ResponsesCompactFallbackAttemptSummaryModel, []string{codexGPT55CompactFallbackModel})
	}
	if info != nil {
		info.UpstreamModelName = codexGPT55CompactFallbackModel
		if info.ChannelMeta != nil {
			info.ChannelMeta.UpstreamModelName = codexGPT55CompactFallbackModel
		}
	}
	common.SysLog(fmt.Sprintf(
		"codex compact model fallback: from=%s to=%s estimated_tokens=%d target_tokens=%d",
		codexGPT55CompactModel,
		codexGPT55CompactFallbackModel,
		finalTokens,
		codexGPT55CompactTargetTokens,
	))
}

func rawStringField(raw common.RawMessage) string {
	if len(raw) == 0 || common.GetJsonType(raw) != "string" {
		return ""
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}
