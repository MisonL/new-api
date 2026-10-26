package constant

type ContextKey string

const (
	ContextKeyResponsesStreamOptionsIncludeUsageRetry           ContextKey = "responses_stream_options_include_usage_retry"
	ContextKeyResponsesStreamOptionsIncludeUsageRetryChannelID  ContextKey = "responses_stream_options_include_usage_retry_channel_id"
	ContextKeyResponsesStreamOptionsCapabilityStripped          ContextKey = "responses_stream_options_include_usage_capability_stripped"
	ContextKeyResponsesStreamOptionsCapabilityStrippedChannelID ContextKey = "responses_stream_options_include_usage_capability_stripped_channel_id"
)

const (
	ContextKeyTokenCountMeta  ContextKey = "token_count_meta"
	ContextKeyPromptTokens    ContextKey = "prompt_tokens"
	ContextKeyEstimatedTokens ContextKey = "estimated_tokens"

	ContextKeyOriginalModel                                 ContextKey = "original_model"
	ContextKeyRequestStartTime                              ContextKey = "request_start_time"
	ContextKeyResponsesBootstrapRecoveryState               ContextKey = "responses_bootstrap_recovery_state"
	ContextKeyResponsesCompactSummaryModel                  ContextKey = "responses_compact_summary_model"
	ContextKeyResponsesCompactSummaryModelFallbackAttempted ContextKey = "responses_compact_summary_model_fallback_attempted"
	ContextKeyResponsesCompactSummaryModels                 ContextKey = "responses_compact_summary_models"
	ContextKeyResponsesCompactVisibleOnly                   ContextKey = "responses_compact_visible_only"
	ContextKeyResponsesCompactCodexContextPruned            ContextKey = "responses_compact_codex_context_pruned"
	ContextKeyResponsesCompactNativeFallback                ContextKey = "responses_compact_native_fallback"
	ContextKeyResponsesCompactStateLookup                   ContextKey = "responses_compact_state_lookup"
	ContextKeyResponsesCompactStateScopeResult              ContextKey = "responses_compact_state_scope_result"
	ContextKeyResponsesCompactMarkerKind                    ContextKey = "responses_compact_marker_kind"
	ContextKeyResponsesCompactRouteDecision                 ContextKey = "responses_compact_route_decision"
	ContextKeyResponsesCompactFallbackReason                ContextKey = "responses_compact_fallback_reason"
	ContextKeyResponsesCompactChannelSkip                   ContextKey = "responses_compact_channel_skip"
	ContextKeyResponsesRoutingChannelSkips                  ContextKey = "responses_routing_channel_skips"
	ContextKeyResponsesRoutingTrace                         ContextKey = "responses_routing_trace"
	ContextKeyResponsesPreflightCache                       ContextKey = "responses_preflight_cache"
	// ContextKeyResponsesCandidatePreflightCache stores request-aware channel
	// preflight results. The cache key includes the retry/attempt index so a
	// round-robin Header Profile selection is never reused across attempts.
	ContextKeyResponsesCandidatePreflightCache                      ContextKey = "responses_candidate_preflight_cache"
	ContextKeyResponsesOpaqueEncryptedReplay                        ContextKey = "responses_opaque_encrypted_replay"
	ContextKeyResponsesEncryptedReasoningReplayAction               ContextKey = "responses_encrypted_reasoning_replay_action"
	ContextKeyResponsesEncryptedReasoningReplayCapability           ContextKey = "responses_encrypted_reasoning_replay_capability"
	ContextKeyResponsesEncryptedReasoningReplayRetryAction          ContextKey = "responses_encrypted_reasoning_replay_retry_action"
	ContextKeyResponsesCompactStateRestored                         ContextKey = "responses_compact_state_restored"
	ContextKeyResponsesCompactModelChanged                          ContextKey = "responses_compact_model_changed"
	ContextKeyResponsesCompactStateHash                             ContextKey = "responses_compact_state_hash"
	ContextKeyResponsesCompactVisibleOnlyFallbackAttempted          ContextKey = "responses_compact_visible_only_fallback_attempted"
	ContextKeyResponsesPreviousIDAction                             ContextKey = "responses_previous_id_action"
	ContextKeyResponsesEncryptedContextRetry                        ContextKey = "responses_encrypted_context_retry"
	ContextKeyResponsesNamespaceHistoryRetry                        ContextKey = "responses_namespace_history_retry"
	ContextKeyResponsesArgumentsHistoryRetry                        ContextKey = "responses_arguments_history_retry"
	ContextKeyResponsesReasoningContextAction                       ContextKey = "responses_reasoning_context_action"
	ContextKeyResponsesStreamPreludeRetrySafe                       ContextKey = "responses_stream_prelude_retry_safe"
	ContextKeyClaudeStreamPreludeSent                               ContextKey = "claude_stream_prelude_sent"
	ContextKeyResponsesChatCompatIgnoredEncryptedInclude            ContextKey = "responses_chat_compat_ignored_encrypted_include"
	ContextKeyResponsesChatCompatReasoningSummaryMapped             ContextKey = "responses_chat_compat_reasoning_summary_mapped"
	ContextKeyResponsesCompactionOutput                             ContextKey = "responses_compaction_output"
	ContextKeyResponsesObservedCustomToolCall                       ContextKey = "responses_observed_custom_tool_call"
	ContextKeyResponsesObservedNamespaceToolCall                    ContextKey = "responses_observed_namespace_tool_call"
	ContextKeyResponsesObservedToolSearchCall                       ContextKey = "responses_observed_tool_search_call"
	ContextKeyResponsesToolCapabilityObservationSource              ContextKey = "responses_tool_capability_observation_source"
	ContextKeyResponsesToolCapabilityObservationBridged             ContextKey = "responses_tool_capability_observation_bridged"
	ContextKeyResponsesRemoteCompactSyntheticFallback               ContextKey = "responses_remote_compact_synthetic_fallback"
	ContextKeyResponsesRemoteCompactFirstDataTimeout                ContextKey = "responses_remote_compact_first_data_timeout"
	ContextKeyResponsesCodexLocalCompaction413Recovery              ContextKey = "responses_codex_local_compaction_413_recovery"
	ContextKeyResponsesCodexLocalCompaction413RecoveryFailed        ContextKey = "responses_codex_local_compaction_413_recovery_failed"
	ContextKeyResponsesCodexLocalCompactionBodyReductionSource      ContextKey = "responses_codex_local_compaction_body_reduction_source"
	ContextKeyResponsesCodexLocalCompactionIngressReduced           ContextKey = "responses_codex_local_compaction_ingress_reduced"
	ContextKeyResponsesCodexLocalCompactionIngressOriginalBodyBytes ContextKey = "responses_codex_local_compaction_ingress_original_body_bytes"
	ContextKeyResponsesCodexLocalCompactionIngressReducedBodyBytes  ContextKey = "responses_codex_local_compaction_ingress_reduced_body_bytes"
	ContextKeyResponsesCodexLocalCompactionIngressInputItems        ContextKey = "responses_codex_local_compaction_ingress_input_items"
	ContextKeyResponsesCodexLocalCompactionOriginalBodyBytes        ContextKey = "responses_codex_local_compaction_original_body_bytes"
	ContextKeyResponsesCodexLocalCompactionReducedBodyBytes         ContextKey = "responses_codex_local_compaction_reduced_body_bytes"
	ContextKeyResponsesInputItemIDNormalizationRemovedCount         ContextKey = "responses_input_item_id_normalization_removed_count"
	// ContextKeyResponsesInputItemIDsRemoved is kept as a compatibility alias
	// for older relay tests and integrations. Both names intentionally share
	// the same context value.
	ContextKeyResponsesInputItemIDsRemoved  ContextKey = ContextKeyResponsesInputItemIDNormalizationRemovedCount
	ContextKeyResponsesRequestMetadata      ContextKey = "responses_request_metadata"
	ContextKeyResponsesProxyBilledUsage     ContextKey = "responses_proxy_billed_usage"
	ContextKeyResponsesErrorEvent           ContextKey = "responses_error_event"
	ContextKeyResponsesStreamSequenceNumber ContextKey = "responses_stream_sequence_number"
	ContextKeyUpstreamRateLimitRetryState   ContextKey = "upstream_rate_limit_retry_state"
	ContextKeyChannelHealthRoutingState     ContextKey = "channel_health_routing_state"
	ContextKeyChannelHealthUnavailable      ContextKey = "channel_health_unavailable"
	ContextKeyChannelHealthRoutes           ContextKey = "channel_health_routes"

	/* token related keys */
	ContextKeyTokenUnlimited         ContextKey = "token_unlimited_quota"
	ContextKeyTokenKey               ContextKey = "token_key"
	ContextKeyTokenId                ContextKey = "token_id"
	ContextKeyTokenGroup             ContextKey = "token_group"
	ContextKeyTokenSpecificChannelId ContextKey = "specific_channel_id"
	ContextKeyTokenModelLimitEnabled ContextKey = "token_model_limit_enabled"
	ContextKeyTokenModelLimit        ContextKey = "token_model_limit"
	ContextKeyTokenCrossGroupRetry   ContextKey = "token_cross_group_retry"

	/* channel related keys */
	ContextKeyChannelId                ContextKey = "channel_id"
	ContextKeyChannelName              ContextKey = "channel_name"
	ContextKeyChannelCreateTime        ContextKey = "channel_create_time"
	ContextKeyChannelBaseUrl           ContextKey = "base_url"
	ContextKeyChannelType              ContextKey = "channel_type"
	ContextKeyChannelSetting           ContextKey = "channel_setting"
	ContextKeyChannelOtherSetting      ContextKey = "channel_other_setting"
	ContextKeyChannelOtherSettingRaw   ContextKey = "channel_other_setting_raw"
	ContextKeyChannelParamOverride     ContextKey = "param_override"
	ContextKeyChannelHeaderOverride    ContextKey = "header_override"
	ContextKeyChannelHeaderPolicyAudit ContextKey = "channel_header_policy_audit"
	// ContextKeyChannelHeaderProfileSelection stores the request-scoped profile
	// chosen for the current channel attempt. Preflight and the actual outbound
	// request must consume this same selection rather than advancing the
	// round-robin cursor independently.
	ContextKeyChannelHeaderProfileSelection ContextKey = "channel_header_profile_selection"
	// ContextKeyChannelHeaderProfileSelections stores the request-scoped profile
	// chosen for each candidate channel during preflight. Responses capability
	// checks may inspect several channels before the selector commits one; a
	// single current-selection slot would otherwise advance the selected
	// channel's round-robin cursor a second time during setup.
	ContextKeyChannelHeaderProfileSelections ContextKey = "channel_header_profile_selections"
	// ContextKeyChannelHeaderProfileAttempt identifies the retry/attempt for the
	// request-scoped header profile selection. It is intentionally not cleared
	// when a channel's fields are reset between candidates.
	ContextKeyChannelHeaderProfileAttempt ContextKey = "channel_header_profile_attempt"
	ContextKeyChannelOrganization         ContextKey = "channel_organization"
	ContextKeyChannelAutoBan              ContextKey = "auto_ban"
	ContextKeyChannelModelMapping         ContextKey = "model_mapping"
	ContextKeyChannelStatusCodeMapping    ContextKey = "status_code_mapping"
	ContextKeyChannelIsMultiKey           ContextKey = "channel_is_multi_key"
	ContextKeyChannelMultiKeyIndex        ContextKey = "channel_multi_key_index"
	ContextKeyChannelKey                  ContextKey = "channel_key"
	ContextKeyChannelMaxConcurrency       ContextKey = "channel_max_concurrency"
	// ContextKeySelectedChannelSnapshot stores the complete channel snapshot
	// selected for the current request. Relay's first selection reuses it instead
	// of reconstructing a credential-less shell from channel_id/type/name.
	ContextKeySelectedChannelSnapshot    ContextKey = "selected_channel_snapshot"
	ContextKeyChannelConcurrencyLease    ContextKey = "channel_concurrency_lease"
	ContextKeyChannelConcurrencyExcluded ContextKey = "excluded_due_to_concurrency"
	// ContextKeyChannelConcurrencyRootRequest keeps the original request
	// wrapper for the lifetime of one Gin request. Channel-concurrency leases
	// replace c.Request with cancellable attempt wrappers; retries must always
	// derive from this root instead of making the next lease a child of the
	// previous lease's context.
	ContextKeyChannelConcurrencyRootRequest ContextKey = "channel_concurrency_root_request"
	ContextKeyChannelRouteScope             ContextKey = "channel_route_scope"
	ContextKeyChannelRouteWeightBPS         ContextKey = "channel_route_weight_bps"
	ContextKeyChannelRoutePriority          ContextKey = "channel_route_priority"
	ContextKeyChannelRouteDiagnostics       ContextKey = "channel_route_diagnostics"
	// ContextKeyTaskRouteIntent stores the request-level asynchronous task
	// operation inferred before channel selection. Provider-specific final wire
	// model rules (for example Vidu reference-image generation) consume this
	// typed intent so selection, health and submission share one identity.
	ContextKeyTaskRouteIntent ContextKey = "task_route_intent"
	// ContextKeyChannelRouteModelResolver stores the request-scoped immutable
	// model-mapping resolver used by selection, health checks, and relay
	// dispatch. It is intentionally not cleared when a retry swaps channel
	// context: the resolver memoizes each channel's first observed mapping for
	// the lifetime of the request.
	ContextKeyChannelRouteModelResolver ContextKey = "channel_route_model_resolver"
	// ContextKeyChannelRouteModelMappingProvisional is a short-lived marker used
	// only when a lightweight channel object is being completed from the mapping
	// already installed on the current request. It prevents an ordinary channel
	// edit from replacing a settled empty mapping snapshot mid-request.
	ContextKeyChannelRouteModelMappingProvisional ContextKey = "channel_route_model_mapping_provisional"

	ContextKeyAutoGroup           ContextKey = "auto_group"
	ContextKeyAutoGroupIndex      ContextKey = "auto_group_index"
	ContextKeyAutoGroupRetryIndex ContextKey = "auto_group_retry_index"

	/* user related keys */
	ContextKeyUserId      ContextKey = "id"
	ContextKeyUserSetting ContextKey = "user_setting"
	ContextKeyUserQuota   ContextKey = "user_quota"
	ContextKeyUserStatus  ContextKey = "user_status"
	ContextKeyUserEmail   ContextKey = "user_email"
	ContextKeyUserGroup   ContextKey = "user_group"
	ContextKeyUsingGroup  ContextKey = "group"
	ContextKeyUserName    ContextKey = "username"

	ContextKeyLocalCountTokens ContextKey = "local_count_tokens"

	ContextKeySystemPromptOverride ContextKey = "system_prompt_override"

	// ContextKeyFileSourcesToCleanup stores file sources that need cleanup when request ends
	ContextKeyFileSourcesToCleanup ContextKey = "file_sources_to_cleanup"

	// ContextKeyAdminRejectReason stores an admin-only reject/block reason extracted from upstream responses.
	// It is not returned to end users, but can be persisted into consume/error logs for debugging.
	ContextKeyAdminRejectReason ContextKey = "admin_reject_reason"

	// ContextKeyLanguage stores the user's language preference for i18n
	ContextKeyLanguage ContextKey = "language"

	// Payload audit context keys store user-authorized request/response previews for logs.
	ContextKeyLogRequestPayload  ContextKey = "log_request_payload"
	ContextKeyLogResponsePayload ContextKey = "log_response_payload"
	ContextKeyIsStream           ContextKey = "is_stream"
	ContextKeyRequestBodySize    ContextKey = "request_body_size"

	// ContextKeyRelayStopChan carries the relay stream's stop channel from
	// relay/helper into the gopool panic handler through the request context.
	// context.WithValue matches on the dynamic type, so both sides must use
	// this shared typed key: a bare string on either side makes the handler
	// silently fail to find the channel. The handler is defense in depth —
	// relay goroutines recover their own panics (relay/helper/stream_scanner.go),
	// so gopool only sees one if that recover is removed.
	ContextKeyRelayStopChan ContextKey = "relay_stop_chan"
)

const (
	ContextKeyResponsesStreamFirstCommittedEvent              ContextKey = "responses_stream_first_committed_event"
	ContextKeyResponsesEncryptedContextRetryBlockedAfterEvent ContextKey = "responses_encrypted_context_retry_blocked_after_event"
)
