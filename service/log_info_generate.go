package service

import (
	"encoding/base64"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func appendRequestPath(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if other == nil {
		return
	}
	if relayInfo != nil {
		if upstreamPath := strings.TrimSpace(relayInfo.UpstreamRequestPath); upstreamPath != "" {
			other["upstream_request_path"] = upstreamPath
		}
	}
	if ctx != nil && ctx.Request != nil && ctx.Request.URL != nil {
		if path := ctx.Request.URL.Path; path != "" {
			other["request_path"] = path
			return
		}
	}
	if relayInfo != nil && relayInfo.RequestURLPath != "" {
		path := relayInfo.RequestURLPath
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}
		other["request_path"] = path
	}
}

func GenerateTextOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelRatio, groupRatio, completionRatio float64,
	cacheTokens int, cacheRatio float64, modelPrice float64, userGroupRatio float64) map[string]interface{} {
	other := make(map[string]interface{})
	other["model_ratio"] = modelRatio
	other["group_ratio"] = groupRatio
	other["completion_ratio"] = completionRatio
	other["cache_tokens"] = cacheTokens
	other["cache_ratio"] = cacheRatio
	other["model_price"] = modelPrice
	other["user_group_ratio"] = userGroupRatio
	if frtMs, ok := relayInfo.FirstResponseLatencyMs(); ok {
		other["frt"] = float64(frtMs)
	}
	if relayInfo.ReasoningEffort != "" {
		other["reasoning_effort"] = relayInfo.ReasoningEffort
	}
	if relayInfo.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = relayInfo.UpstreamModelName
	}

	isSystemPromptOverwritten := common.GetContextKeyBool(ctx, constant.ContextKeySystemPromptOverride)
	if isSystemPromptOverwritten {
		other["is_system_prompt_overwritten"] = true
	}

	adminInfo := make(map[string]interface{})
	adminInfo["use_channel"] = ctx.GetStringSlice("use_channel")
	isMultiKey := common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey)
	if isMultiKey {
		adminInfo["is_multi_key"] = true
		adminInfo["multi_key_index"] = common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex)
	}

	isLocalCountTokens := common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens)
	if isLocalCountTokens {
		adminInfo["local_count_tokens"] = isLocalCountTokens
	}

	AppendChannelAffinityAdminInfo(ctx, adminInfo)
	AppendUpstreamRateLimitRetryAdminInfo(ctx, adminInfo)

	other["admin_info"] = adminInfo
	appendRequestPath(ctx, relayInfo, other)
	appendRequestConversionChain(relayInfo, other)
	appendFinalRequestFormat(relayInfo, other)
	appendResponsesRelayInfo(ctx, relayInfo, other)
	appendResponsesRequestShape(ctx, relayInfo, other)
	appendBillingInfo(relayInfo, other)
	appendParamOverrideInfo(relayInfo, other)
	AppendRequestHeaderPolicyInfo(ctx, other)
	appendStreamStatus(relayInfo, other)
	appendUpstreamMetadata(relayInfo, other)
	return other
}

func appendResponsesRelayInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if other == nil {
		return
	}
	settings := dto.ChannelOtherSettings{}
	if relayInfo != nil && relayInfo.ChannelMeta != nil {
		settings = relayInfo.ChannelOtherSettings
	}
	if ctx != nil {
		if ctxSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](ctx, constant.ContextKeyChannelOtherSetting); ok {
			settings = ctxSettings
		}
		if action := common.GetContextKeyString(ctx, constant.ContextKeyResponsesPreviousIDAction); action != "" {
			other["responses_previous_id_action"] = action
		}
		if lookup := common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactStateLookup); lookup != "" {
			other["responses_compact_state_lookup"] = lookup
		}
		if scopeResult := common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactStateScopeResult); scopeResult != "" {
			other["responses_compact_state_scope_result"] = scopeResult
		}
		if markerKind := common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactMarkerKind); markerKind != "" {
			other["responses_compact_marker_kind"] = markerKind
		}
		if routeDecision := common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactRouteDecision); routeDecision != "" {
			other["responses_compact_route_decision"] = routeDecision
		}
		if fallbackReason := common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactFallbackReason); fallbackReason != "" {
			other["responses_compact_fallback_reason"] = fallbackReason
		}
		if common.GetContextKeyBool(ctx, constant.ContextKeyResponsesEncryptedContextRetry) {
			other["responses_encrypted_context_retry"] = true
		}
		if common.GetContextKeyBool(ctx, constant.ContextKeyResponsesNamespaceHistoryRetry) {
			other["responses_namespace_history_retry"] = true
		}
		if common.GetContextKeyBool(ctx, constant.ContextKeyResponsesArgumentsHistoryRetry) {
			other["responses_arguments_history_retry"] = true
		}
		if relayInfo.ChannelMeta != nil && common.GetContextKeyBool(ctx, constant.ContextKeyResponsesStreamOptionsIncludeUsageRetry) &&
			common.GetContextKeyInt(ctx, constant.ContextKeyResponsesStreamOptionsIncludeUsageRetryChannelID) == relayInfo.ChannelMeta.ChannelId {
			other["responses_stream_options_include_usage_retry"] = true
		}
		if relayInfo.ChannelMeta != nil && common.GetContextKeyBool(ctx, constant.ContextKeyResponsesStreamOptionsCapabilityStripped) &&
			common.GetContextKeyInt(ctx, constant.ContextKeyResponsesStreamOptionsCapabilityStrippedChannelID) == relayInfo.ChannelMeta.ChannelId {
			other["responses_stream_options_include_usage_capability_stripped"] = true
		}
		if action := common.GetContextKeyString(ctx, constant.ContextKeyResponsesReasoningContextAction); action != "" {
			other["responses_reasoning_context_action"] = action
		}
		if removedCount := common.GetContextKeyInt(ctx, constant.ContextKeyResponsesInputItemIDsRemoved); removedCount > 0 {
			other["responses_input_item_ids_removed"] = removedCount
		}
		if common.GetContextKeyBool(ctx, constant.ContextKeyResponsesChatCompatIgnoredEncryptedInclude) {
			other["responses_chat_compat_ignored_encrypted_include"] = true
		}
		if common.GetContextKeyBool(ctx, constant.ContextKeyResponsesChatCompatReasoningSummaryMapped) {
			other["responses_chat_compat_reasoning_summary_mapped"] = true
		}
		if channelSkip := common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactChannelSkip); channelSkip != "" {
			other["responses_compact_channel_skip"] = channelSkip
		}
		if channelSkips, ok := common.GetContextKeyType[[]dto.ResponsesRoutingChannelSkip](ctx, constant.ContextKeyResponsesRoutingChannelSkips); ok && len(channelSkips) > 0 {
			other["responses_routing_channel_skips"] = append([]dto.ResponsesRoutingChannelSkip(nil), channelSkips...)
		}
		if action := common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayAction); action != "" {
			other["responses_encrypted_reasoning_replay_action"] = action
		}
		if capability := common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayCapability); capability != "" {
			other["responses_encrypted_reasoning_replay_capability"] = capability
		}
		if retryAction := common.GetContextKeyString(ctx, constant.ContextKeyResponsesEncryptedReasoningReplayRetryAction); retryAction != "" {
			other["responses_encrypted_reasoning_replay_retry_action"] = retryAction
		}
		if common.GetContextKeyBool(ctx, constant.ContextKeyResponsesCompactStateRestored) {
			other["responses_compact_state_restored"] = true
		}
		if common.GetContextKeyBool(ctx, constant.ContextKeyResponsesCompactModelChanged) {
			other["responses_compact_model_changed"] = true
		}
		if stateHash := common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactStateHash); stateHash != "" {
			other["responses_compact_state_hash"] = stateHash
		}
		if common.GetContextKeyBool(ctx, constant.ContextKeyResponsesCompactVisibleOnlyFallbackAttempted) {
			other["responses_compact_visible_only_fallback"] = true
		}
		if common.GetContextKeyBool(ctx, constant.ContextKeyResponsesRemoteCompactSyntheticFallback) {
			other["responses_remote_compact_synthetic_fallback"] = true
		}
		if common.GetContextKeyBool(ctx, constant.ContextKeyResponsesRemoteCompactFirstDataTimeout) {
			other["responses_remote_compact_first_data_timeout"] = true
		}
	}
	if snapshot := ResponsesChannelCapabilitySnapshot(relayInfo, settings); len(snapshot) > 0 {
		other["channel_capability_snapshot"] = snapshot
	}
	appendResponsesChannelCapabilityAdminInfo(relayInfo, settings, other)
	if profile := settings.NormalizedResponsesUpstreamProfile(); profile != "" {
		other["responses_upstream_profile"] = string(profile)
	}
	appendCodexCompactionInfo(ctx, relayInfo, other)
}

func AppendResponsesRelayInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	appendResponsesRelayInfo(ctx, relayInfo, other)
}

func ResponsesChannelCapabilitySnapshot(relayInfo *relaycommon.RelayInfo, settings dto.ChannelOtherSettings) map[string]interface{} {
	if relayInfo == nil || relayInfo.ChannelMeta == nil {
		return nil
	}
	snapshotInfo := settings.ResolveResponsesChannelCapability(relayInfo.ChannelType)
	snapshot := map[string]interface{}{
		"source":                                 snapshotInfo.Source,
		"compact_mode_setting":                   string(snapshotInfo.CompactModeSetting),
		"compact_mode_effective":                 string(snapshotInfo.CompactModeEffective),
		"supports_responses":                     snapshotInfo.SupportsResponses,
		"supports_responses_compact":             snapshotInfo.SupportsResponsesCompact,
		"supports_chat":                          snapshotInfo.SupportsChat,
		"supports_rest_previous_response_id":     snapshotInfo.SupportsRestPreviousResponseID,
		"compaction_item_passthrough_capability": string(snapshotInfo.CompactionItemPassthroughCapability),
		"supports_namespace_tools":               snapshotInfo.SupportsNamespaceTools,
		"supports_tool_search":                   snapshotInfo.SupportsToolSearch,
		"reasoning_context_capability":           string(snapshotInfo.ReasoningContextCapability),
		"encrypted_reasoning_replay_capability":  string(snapshotInfo.EncryptedReasoningReplayCapability),
		"strips_responses_reasoning_context":     snapshotInfo.StripsResponsesReasoningContext,
		"strips_responses_encrypted_reasoning":   snapshotInfo.StripsResponsesEncryptedReasoning,
	}
	if snapshotInfo.SupportsCustomTools != nil {
		snapshot["supports_custom_tools"] = *snapshotInfo.SupportsCustomTools
	}
	if snapshotInfo.SupportsCompactionItemPassthrough != nil {
		snapshot["supports_compaction_item_passthrough"] = *snapshotInfo.SupportsCompactionItemPassthrough
	}
	if snapshotInfo.SupportsFunctionTools != nil {
		snapshot["supports_function_tools"] = *snapshotInfo.SupportsFunctionTools
	}
	if snapshotInfo.SupportsReasoningContext != nil {
		snapshot["supports_reasoning_context"] = *snapshotInfo.SupportsReasoningContext
	}
	if snapshotInfo.SupportsStreamOptionsIncludeUsage != nil {
		snapshot["supports_stream_options_include_usage"] = *snapshotInfo.SupportsStreamOptionsIncludeUsage
	}
	if snapshotInfo.SupportsEncryptedReasoningReplay != nil {
		snapshot["supports_encrypted_reasoning_replay"] = *snapshotInfo.SupportsEncryptedReasoningReplay
	}
	if snapshotInfo.Profile != "" {
		snapshot["profile"] = string(snapshotInfo.Profile)
	}
	return snapshot
}

func appendResponsesChannelCapabilityAdminInfo(relayInfo *relaycommon.RelayInfo, settings dto.ChannelOtherSettings, other map[string]interface{}) {
	if relayInfo == nil || relayInfo.ChannelMeta == nil || other == nil {
		return
	}
	snapshotInfo := settings.ResolveResponsesChannelCapability(relayInfo.ChannelType)
	if snapshotInfo.Observed != nil {
		adminInfo := getOrCreateAdminInfo(other)
		adminInfo["responses_channel_capability_observed"] = snapshotInfo.Observed
	}
	if snapshotInfo.Probe != nil {
		adminInfo := getOrCreateAdminInfo(other)
		adminInfo["responses_channel_capability_probe"] = snapshotInfo.Probe
	}
	if snapshotInfo.ObservedTools != nil {
		adminInfo := getOrCreateAdminInfo(other)
		adminInfo["responses_channel_tool_capability_observed"] = snapshotInfo.ObservedTools
	}
}

func getOrCreateAdminInfo(other map[string]interface{}) map[string]interface{} {
	adminInfo, _ := other["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = map[string]interface{}{}
		other["admin_info"] = adminInfo
	}
	return adminInfo
}

type codexTurnMetadataHeader struct {
	RequestKind string                         `json:"request_kind"`
	Compaction  *codexCompactionMetadataHeader `json:"compaction"`
}

type codexCompactionMetadataHeader struct {
	Trigger        string `json:"trigger"`
	Reason         string `json:"reason"`
	Implementation string `json:"implementation"`
	Phase          string `json:"phase"`
	Strategy       string `json:"strategy"`
}

func appendCodexCompactionInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if other == nil {
		return
	}

	headerInfo, ok := parseCodexCompactionHeader(ctx)
	hasTrigger := relayInfoHasCompactionTrigger(relayInfo)
	hasOutput := ctx != nil && common.GetContextKeyBool(ctx, constant.ContextKeyResponsesCompactionOutput)
	if !ok && !hasTrigger && !hasOutput {
		return
	}

	if ok {
		other["responses_compact_client"] = "codex"
		if headerInfo.Implementation != "" {
			other["responses_compact_client_implementation"] = headerInfo.Implementation
		}
		if headerInfo.Trigger != "" {
			other["responses_compact_trigger"] = headerInfo.Trigger
		}
		if headerInfo.Reason != "" {
			other["responses_compact_reason"] = headerInfo.Reason
		}
		if headerInfo.Phase != "" {
			other["responses_compact_phase"] = headerInfo.Phase
		}
		if headerInfo.Strategy != "" {
			other["responses_compact_strategy"] = headerInfo.Strategy
		}
	}

	if _, exists := other["responses_compact_mode"]; !exists && !isLegacyResponsesCompactRelay(relayInfo, ctx) {
		other["responses_compact_mode"] = codexCompactionMode(headerInfo.Implementation, hasTrigger, hasOutput)
		other["responses_compact_upstream_path"] = requestPathForLog(ctx, relayInfo)
	}
}

func parseCodexCompactionHeader(ctx *gin.Context) (codexCompactionMetadataHeader, bool) {
	if ctx == nil || ctx.Request == nil {
		return codexCompactionMetadataHeader{}, false
	}
	rawHeader := strings.TrimSpace(ctx.Request.Header.Get("x-codex-turn-metadata"))
	if rawHeader == "" {
		return codexCompactionMetadataHeader{}, false
	}

	var metadata codexTurnMetadataHeader
	if err := common.Unmarshal([]byte(rawHeader), &metadata); err != nil {
		return codexCompactionMetadataHeader{}, false
	}
	if metadata.RequestKind != "compaction" || metadata.Compaction == nil {
		return codexCompactionMetadataHeader{}, false
	}
	return *metadata.Compaction, true
}

func relayInfoHasCompactionTrigger(relayInfo *relaycommon.RelayInfo) bool {
	if relayInfo == nil {
		return false
	}
	request, ok := relayInfo.Request.(*dto.OpenAIResponsesRequest)
	return ok && request.HasCompactionTrigger()
}

func isLegacyResponsesCompactRelay(relayInfo *relaycommon.RelayInfo, ctx *gin.Context) bool {
	if relayInfo != nil && relayInfo.RelayMode == relayconstant.RelayModeResponsesCompact {
		return true
	}
	return ctx != nil && ctx.GetInt("relay_mode") == relayconstant.RelayModeResponsesCompact
}

func codexCompactionMode(implementation string, hasTrigger bool, hasOutput bool) string {
	switch implementation {
	case "responses_compaction_v2":
		return "remote_v2"
	case "responses_compact":
		return "native"
	case "responses":
		return "codex_local"
	}
	if hasTrigger {
		return "remote_v2"
	}
	if hasOutput {
		return "remote_v2"
	}
	return "codex_local"
}

func requestPathForLog(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) string {
	if ctx != nil && ctx.Request != nil && ctx.Request.URL != nil && ctx.Request.URL.Path != "" {
		return ctx.Request.URL.Path
	}
	if relayInfo == nil || relayInfo.RequestURLPath == "" {
		return "/v1/responses"
	}
	path := relayInfo.RequestURLPath
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}
	if path == "" {
		return "/v1/responses"
	}
	return path
}

func appendUpstreamMetadata(relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil {
		return
	}
	if relayInfo.UpstreamRequestId != "" {
		other["upstream_request_id"] = relayInfo.UpstreamRequestId
	}
	if relayInfo.UpstreamUsageMetadata != "" {
		other["upstream_usage_metadata"] = relayInfo.UpstreamUsageMetadata
	}
	if headerMs, ok := relayInfo.UpstreamHeaderLatencyMs(); ok {
		other["upstream_header_ms"] = float64(headerMs)
	}
	if ttfbMs, ok := relayInfo.UpstreamFirstByteLatencyMs(); ok {
		other["upstream_ttfb_ms"] = float64(ttfbMs)
	}
	if totalMs, ok := relayInfo.UpstreamTotalLatencyMs(); ok {
		other["upstream_total_ms"] = float64(totalMs)
	}
}

func appendParamOverrideInfo(relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil || len(relayInfo.ParamOverrideAudit) == 0 {
		return
	}
	other["po"] = relayInfo.ParamOverrideAudit
}

func appendStreamStatus(relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil || !relayInfo.IsStream || relayInfo.StreamStatus == nil {
		return
	}
	ss := relayInfo.StreamStatus
	status := classifyStreamStatus(relayInfo, ss)
	streamInfo := map[string]interface{}{
		"status":     status,
		"end_reason": string(ss.EndReason),
	}
	if ss.EndError != nil {
		streamInfo["end_error"] = ss.EndError.Error()
	}
	if ss.ErrorCount > 0 {
		streamInfo["error_count"] = ss.ErrorCount
		messages := make([]string, 0, len(ss.Errors))
		for _, e := range ss.Errors {
			messages = append(messages, e.Message)
		}
		streamInfo["errors"] = messages
	}
	other["stream_status"] = streamInfo
}

func AppendRequestHeaderPolicyInfo(ctx *gin.Context, other map[string]interface{}) {
	if ctx == nil || other == nil {
		return
	}

	audit, ok := common.GetContextKeyType[RuntimeHeaderPolicyAudit](ctx, constant.ContextKeyChannelHeaderPolicyAudit)
	if !ok {
		return
	}

	info := map[string]interface{}{
		"mode": audit.HeaderPolicyMode,
	}
	if len(audit.AppliedHeaderKeys) > 0 {
		info["applied_header_keys"] = audit.AppliedHeaderKeys
	}
	if appliedHeaders := sanitizeAppliedHeaderAuditEntries(audit.AppliedHeaders); len(appliedHeaders) > 0 {
		info["applied_headers"] = appliedHeaders
	}
	if audit.HeaderProfileID != "" {
		info["header_profile_id"] = audit.HeaderProfileID
	}
	if audit.HeaderProfileMode != "" {
		info["header_profile_mode"] = audit.HeaderProfileMode
	}
	if audit.HeaderProfileApplied {
		info["header_profile_applied"] = true
	}
	if audit.UserAgentStrategyMode != "" {
		info["ua_strategy_mode"] = audit.UserAgentStrategyMode
	}
	if audit.UserAgentStrategyScope != "" {
		info["ua_strategy_scope"] = audit.UserAgentStrategyScope
	}
	if audit.SelectedUserAgent != "" {
		info["selected_user_agent"] = audit.SelectedUserAgent
	}
	if audit.AppliedUserAgent != "" {
		info["applied_user_agent"] = audit.AppliedUserAgent
	}
	if audit.OverrideStaticUserAgent {
		info["override_static_user_agent"] = true
	}
	info["user_agent_applied"] = audit.UserAgentApplied
	other["request_header_policy"] = info
}

func sanitizeAppliedHeaderAuditEntries(entries []AppliedHeaderAuditEntry) []AppliedHeaderAuditEntry {
	if len(entries) == 0 {
		return nil
	}
	sanitized := make([]AppliedHeaderAuditEntry, 0, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			continue
		}
		sanitized = append(sanitized, AppliedHeaderAuditEntry{
			Key:   key,
			Value: RedactHeaderAuditValue(key, entry.Value),
		})
	}
	return sanitized
}

func classifyStreamStatus(relayInfo *relaycommon.RelayInfo, ss *relaycommon.StreamStatus) string {
	if ss == nil {
		return "ok"
	}
	if ss.IsCanceled() {
		return "canceled"
	}
	if ss.EndReason == relaycommon.StreamEndReasonUpstreamInterrupted &&
		relayInfo != nil &&
		!ss.HasErrors() &&
		relayInfo.ReceivedResponseCount > 0 {
		return "partial"
	}
	if !ss.IsNormalEnd() || ss.HasErrors() {
		return "error"
	}
	return "ok"
}

func appendBillingInfo(relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil {
		return
	}
	// billing_source: "wallet" or "subscription"
	if relayInfo.BillingSource != "" {
		other["billing_source"] = relayInfo.BillingSource
	}
	if relayInfo.UserSetting.BillingPreference != "" {
		other["billing_preference"] = relayInfo.UserSetting.BillingPreference
	}
	if relayInfo.BillingSource == "subscription" {
		if relayInfo.SubscriptionId != 0 {
			other["subscription_id"] = relayInfo.SubscriptionId
		}
		if relayInfo.SubscriptionPreConsumed > 0 {
			other["subscription_pre_consumed"] = relayInfo.SubscriptionPreConsumed
		}
		// post_delta: settlement delta applied after actual usage is known (can be negative for refund)
		if relayInfo.SubscriptionPostDelta != 0 {
			other["subscription_post_delta"] = relayInfo.SubscriptionPostDelta
		}
		if relayInfo.SubscriptionPlanId != 0 {
			other["subscription_plan_id"] = relayInfo.SubscriptionPlanId
		}
		if relayInfo.SubscriptionPlanTitle != "" {
			other["subscription_plan_title"] = relayInfo.SubscriptionPlanTitle
		}
		// Compute "this request" subscription consumed + remaining
		consumed := relayInfo.SubscriptionPreConsumed + relayInfo.SubscriptionPostDelta
		usedFinal := relayInfo.SubscriptionAmountUsedAfterPreConsume + relayInfo.SubscriptionPostDelta
		if consumed < 0 {
			consumed = 0
		}
		if usedFinal < 0 {
			usedFinal = 0
		}
		if relayInfo.SubscriptionAmountTotal > 0 {
			remain := relayInfo.SubscriptionAmountTotal - usedFinal
			if remain < 0 {
				remain = 0
			}
			other["subscription_total"] = relayInfo.SubscriptionAmountTotal
			other["subscription_used"] = usedFinal
			other["subscription_remain"] = remain
		}
		if consumed > 0 {
			other["subscription_consumed"] = consumed
		}
		// Wallet quota is not deducted when billed from subscription.
		other["wallet_quota_deducted"] = 0
	}
}

func appendRequestConversionChain(relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil {
		return
	}
	if len(relayInfo.RequestConversionChain) == 0 {
		return
	}
	chain := make([]string, 0, len(relayInfo.RequestConversionChain))
	for _, f := range relayInfo.RequestConversionChain {
		switch f {
		case types.RelayFormatOpenAI:
			chain = append(chain, "OpenAI Compatible")
		case types.RelayFormatClaude:
			chain = append(chain, "Claude Messages")
		case types.RelayFormatGemini:
			chain = append(chain, "Google Gemini")
		case types.RelayFormatOpenAIResponses:
			chain = append(chain, "OpenAI Responses")
		case types.RelayFormatOpenAIResponsesCompaction:
			chain = append(chain, "OpenAI Responses Compact")
		default:
			chain = append(chain, string(f))
		}
	}
	if len(chain) == 0 {
		return
	}
	other["request_conversion"] = chain
}

func appendFinalRequestFormat(relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil {
		return
	}
	if relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		// claude indicates the final upstream request format is Claude Messages.
		// Frontend log rendering uses this to keep the original Claude input display.
		other["claude"] = true
	}
}

func GenerateWssOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.RealtimeUsage, modelRatio, groupRatio, completionRatio, audioRatio, audioCompletionRatio, modelPrice, userGroupRatio float64) map[string]interface{} {
	info := GenerateTextOtherInfo(ctx, relayInfo, modelRatio, groupRatio, completionRatio, 0, 0.0, modelPrice, userGroupRatio)
	info["ws"] = true
	info["audio_input"] = usage.InputTokenDetails.AudioTokens
	info["audio_output"] = usage.OutputTokenDetails.AudioTokens
	info["text_input"] = usage.InputTokenDetails.TextTokens
	info["text_output"] = usage.OutputTokenDetails.TextTokens
	info["audio_ratio"] = audioRatio
	info["audio_completion_ratio"] = audioCompletionRatio
	return info
}

func GenerateAudioOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, modelRatio, groupRatio, completionRatio, audioRatio, audioCompletionRatio, modelPrice, userGroupRatio float64) map[string]interface{} {
	info := GenerateTextOtherInfo(ctx, relayInfo, modelRatio, groupRatio, completionRatio, 0, 0.0, modelPrice, userGroupRatio)
	info["audio"] = true
	info["audio_input"] = usage.PromptTokensDetails.AudioTokens
	info["audio_output"] = usage.CompletionTokenDetails.AudioTokens
	info["text_input"] = usage.PromptTokensDetails.TextTokens
	info["text_output"] = usage.CompletionTokenDetails.TextTokens
	info["audio_ratio"] = audioRatio
	info["audio_completion_ratio"] = audioCompletionRatio
	return info
}

func GenerateClaudeOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelRatio, groupRatio, completionRatio float64,
	cacheTokens int, cacheRatio float64,
	cacheCreationTokens int, cacheCreationRatio float64,
	cacheCreationTokens5m int, cacheCreationRatio5m float64,
	cacheCreationTokens1h int, cacheCreationRatio1h float64,
	modelPrice float64, userGroupRatio float64) map[string]interface{} {
	info := GenerateTextOtherInfo(ctx, relayInfo, modelRatio, groupRatio, completionRatio, cacheTokens, cacheRatio, modelPrice, userGroupRatio)
	info["claude"] = true
	info["cache_creation_tokens"] = cacheCreationTokens
	info["cache_creation_ratio"] = cacheCreationRatio
	if cacheCreationTokens5m != 0 {
		info["cache_creation_tokens_5m"] = cacheCreationTokens5m
		info["cache_creation_ratio_5m"] = cacheCreationRatio5m
	}
	if cacheCreationTokens1h != 0 {
		info["cache_creation_tokens_1h"] = cacheCreationTokens1h
		info["cache_creation_ratio_1h"] = cacheCreationRatio1h
	}
	return info
}

func GenerateMjOtherInfo(relayInfo *relaycommon.RelayInfo, priceData types.PriceData) map[string]interface{} {
	other := make(map[string]interface{})
	other["model_price"] = priceData.ModelPrice
	other["group_ratio"] = priceData.GroupRatioInfo.GroupRatio
	if priceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = priceData.GroupRatioInfo.GroupSpecialRatio
	}
	appendRequestPath(nil, relayInfo, other)
	return other
}

// InjectTieredBillingInfo overlays tiered billing fields onto an existing
// module-specific other map. Call this after GenerateTextOtherInfo /
// GenerateClaudeOtherInfo / etc. when the request used tiered_expr billing.
func InjectTieredBillingInfo(other map[string]interface{}, relayInfo *relaycommon.RelayInfo, result *billingexpr.TieredResult) {
	if relayInfo == nil || other == nil {
		return
	}
	snap := relayInfo.TieredBillingSnapshot
	if snap == nil {
		return
	}
	other["billing_mode"] = "tiered_expr"
	other["expr_b64"] = base64.StdEncoding.EncodeToString([]byte(snap.ExprString))
	if result != nil {
		other["matched_tier"] = result.MatchedTier
	}
}
