/**
 * Type definitions for usage logs
 */
import type { UsageLog } from './data/schema'

// ============================================================================
// Log Category Types
// ============================================================================

/**
 * Log category for different log types
 */
export type LogCategory = 'common' | 'drawing' | 'task'

// ============================================================================
// Filter Types
// ============================================================================

/**
 * Common filters (shared across all log types)
 */
export interface CommonFilters {
  startTime?: Date
  endTime?: Date
  channel?: string
}

/**
 * Common logs specific filters
 */
export interface CommonLogFilters extends CommonFilters {
  model?: string
  token?: string
  group?: string
  username?: string
  requestId?: string
  upstreamRequestId?: string
}

/**
 * Drawing logs specific filters
 */
export interface DrawingLogFilters extends CommonFilters {
  mjId?: string
}

/**
 * Task logs specific filters
 */
export interface TaskLogFilters extends CommonFilters {
  taskId?: string
}

/**
 * Union type for all log filters
 */
export type LogFilters = CommonLogFilters | DrawingLogFilters | TaskLogFilters

// ============================================================================
// Common Logs Additional Types
// ============================================================================

/**
 * Parsed data from the 'other' field in usage logs
 */
export interface ChannelAffinityInfo {
  rule_name?: string
  selected_group?: string
  key_source?: string
  key_path?: string
  key_key?: string
  key_hint?: string
  key_fp?: string
  using_group?: string
}

export interface ResponsesRoutingChannelSkip {
  channel_id: number
  reason: string
}

export interface ResponsesRoutingAttempt {
  channel_id: number
  status_code?: number
  error_code?: string
  error_category?: string
  outcome?: string
}

export interface ResponsesRoutingTrace {
  candidates?: number[]
  preflight_skipped?: ResponsesRoutingChannelSkip[]
  attempted_channels?: ResponsesRoutingAttempt[]
  final_channel?: number
  terminal_reason?: string
}

export interface ResponsesRoutingUserSummary {
  outcome?: string
  automatic_reroute?: boolean
  attempt_count?: number
  preflight_skip_count?: number
  reasons?: string[]
}

export interface ChannelHealthRoutingEvent {
  group?: string
  model_name?: string
  channel_id?: number
  action?: string
  status_code?: number
  error_code?: string
  route_key?: string
  effective_model?: string
  protocol?: string
  upstream_path?: string
  multi_key_index?: number
}

export interface ChannelHealthRoutingInfo {
  events?: ChannelHealthRoutingEvent[]
  redis_unavailable?: boolean
}

export interface LogOtherData {
  admin_info?: {
    is_multi_key?: boolean
    multi_key_index?: number
    use_channel?: string[]
    local_count_tokens?: boolean
    channel_affinity?: ChannelAffinityInfo
    // Top-up audit fields (type=1, admin only)
    payment_method?: string
    callback_payment_method?: string
    caller_ip?: string
    server_ip?: string
    version?: string
    node_name?: string
    // Manage audit fields (type=3, admin only)
    admin_username?: string
    admin_id?: number | string
    responses_channel_capability_observed?: ResponsesCapabilityObservation
    responses_channel_capability_probe?: ResponsesCapabilityObservation
    responses_channel_tool_capability_observed?: ResponsesToolCapabilityObservation
    channel_health?: ChannelHealthRoutingInfo
  }
  request_path?: string
  upstream_request_path?: string
  upstream_request_id?: string
  request_conversion?: string[]
  ws?: boolean
  audio?: boolean
  audio_input?: number
  audio_output?: number
  text_input?: number
  text_output?: number
  cache_tokens?: number
  cache_write_tokens?: number
  cache_creation_tokens?: number
  cache_creation_tokens_5m?: number
  cache_creation_tokens_1h?: number
  input_tokens_total?: number
  usage_semantic?: string
  claude?: boolean
  model_ratio?: number
  completion_ratio?: number
  model_price?: number
  group_ratio?: number
  user_group_ratio?: number
  cache_ratio?: number
  cache_creation_ratio?: number
  cache_creation_ratio_5m?: number
  cache_creation_ratio_1h?: number
  is_model_mapped?: boolean
  upstream_model_name?: string
  audio_ratio?: number
  audio_completion_ratio?: number
  frt?: number
  upstream_header_ms?: number
  upstream_ttfb_ms?: number
  upstream_total_ms?: number
  // Tiered (expression-based) billing fields, set by backend when
  // billing_mode === 'tiered_expr'. expr_b64 is the base64-encoded billing
  // expression and matched_tier is the label of the tier that fired.
  billing_mode?: string
  expr_b64?: string
  matched_tier?: string
  reasoning_effort?: string
  image?: boolean
  image_ratio?: number
  image_output?: number
  web_search?: boolean
  web_search_call_count?: number
  web_search_price?: number
  file_search?: boolean
  file_search_call_count?: number
  file_search_price?: number
  audio_input_seperate_price?: boolean
  audio_input_token_count?: number
  audio_input_price?: number
  image_generation_call?: boolean
  image_generation_call_price?: number
  image_generation_call_total_price?: number
  is_system_prompt_overwritten?: boolean
  po?: string[]
  billing_source?: string
  responses_compact_mode?: string
  responses_compact_setting?: string
  responses_compact_upstream_path?: string
  responses_compact_auto_fallback?: boolean
  responses_compact_auto_fallback_window?: boolean
  responses_compact_context_fallback?: boolean
  responses_compact_previous_response_id_fallback?: boolean
  responses_compact_summary_model_fallback?: boolean
  responses_compact_summary_model?: string
  responses_compact_summary_models?: string[]
  responses_compact_native_attempted?: boolean
  responses_compact_native_upstream_path?: string
  responses_compact_native_status_code?: number
  responses_compact_auto_fallback_reason?: string
  responses_compact_auto_fallback_retry_until?: number
  responses_compact_auto_fallback_retry_interval_hours?: number
  responses_compact_final_upstream_path?: string
  responses_compact_client?: string
  responses_compact_client_implementation?: string
  responses_compact_trigger?: string
  responses_compact_reason?: string
  responses_compact_phase?: string
  responses_compact_strategy?: string
  responses_compact_route_decision?: string
  responses_compact_channel_skip?: string
  responses_routing_channel_skips?: ResponsesRoutingChannelSkip[]
  responses_routing_trace?: ResponsesRoutingTrace
  responses_routing_summary?: ResponsesRoutingUserSummary
  responses_compact_state_restored?: boolean
  responses_compact_model_changed?: boolean
  responses_remote_compact_first_data_timeout?: boolean
  responses_remote_compact_synthetic_fallback?: boolean
  channel_capability_snapshot?: ChannelCapabilitySnapshot
  responses_compact_fallback_reason?: string
  responses_compact_visible_only_fallback?: boolean
  responses_encrypted_context_retry?: boolean
  responses_encrypted_context_retry_blocked_after_event?: string
  responses_namespace_history_retry?: boolean
  responses_arguments_history_retry?: boolean
  responses_stream_options_include_usage_retry?: boolean
  responses_stream_options_include_usage_capability_stripped?: boolean
  responses_previous_id_action?: string
  responses_reasoning_context_action?: string
  responses_chat_compat_ignored_encrypted_include?: boolean
  responses_chat_compat_reasoning_summary_mapped?: boolean
  responses_codex_local_compaction_413_recovery?: boolean
  responses_codex_local_compaction_413_recovery_failed?: boolean
  responses_codex_local_compaction_body_reduction_source?: string
  responses_codex_local_compaction_ingress_reduced?: boolean
  responses_codex_local_compaction_ingress_original_body_bytes?: number
  responses_codex_local_compaction_ingress_reduced_body_bytes?: number
  responses_codex_local_compaction_ingress_input_items?: number
  responses_codex_local_compaction_original_body_bytes?: number
  responses_codex_local_compaction_reduced_body_bytes?: number
  responses_input_item_id_normalization_removed_count?: number
  request_header_policy?: RequestHeaderPolicyInfo
  request_header_policy_mode?: string
  header_policy_mode?: string
  header_profile_id?: string
  header_profile_mode?: string
  header_profile_applied?: boolean
  ua_strategy_mode?: string
  ua_strategy_scope?: string
  selected_user_agent?: string
  applied_user_agent?: string
  override_static_user_agent?: boolean
  user_agent_applied?: boolean
  applied_header_keys?: string[]
  applied_headers?: Array<{ key?: string; value?: string }>
  group?: string
  stream_status?: {
    status?: string
    end_reason?: string
    error_count?: number
    end_error?: string
    errors?: string[]
  }
  // Violation fee fields
  violation_fee?: boolean
  violation_fee_code?: string
  violation_fee_marker?: string
  fee_quota?: number
  // Reject / intercept reason (admin)
  reject_reason?: string
  // Task-related fields (for refund logs, type=6)
  is_task?: boolean
  task_id?: string
  reason?: string
  // Subscription billing fields
  subscription_plan_id?: string
  subscription_plan_title?: string
  subscription_id?: string
  subscription_pre_consumed?: number
  subscription_post_delta?: number
  subscription_consumed?: number
  subscription_remain?: number
  subscription_total?: number
}

export interface RequestHeaderPolicyInfo {
  mode?: string
  applied_header_keys?: string[]
  applied_headers?: Array<{ key?: string; value?: string }>
  header_profile_id?: string
  header_profile_mode?: string
  header_profile_applied?: boolean
  ua_strategy_mode?: string
  ua_strategy_scope?: string
  selected_user_agent?: string
  applied_user_agent?: string
  override_static_user_agent?: boolean
  user_agent_applied?: boolean
}

export interface ChannelCapabilitySnapshot {
  source?: string
  profile?: string
  compact_mode_setting?: string
  compact_mode_effective?: string
  supports_responses?: boolean
  supports_responses_compact?: boolean
  supports_chat?: boolean
  supports_rest_previous_response_id?: boolean
  supports_compaction_item_passthrough?: boolean
  supports_custom_tools?: boolean
  supports_namespace_tools?: boolean
  supports_tool_search?: boolean
  namespace_tools_capability?: 'supported' | 'unsupported' | 'unknown'
  tool_search_capability?: 'supported' | 'unsupported' | 'unknown'
  reasoning_context_capability?: 'supported' | 'unsupported' | 'unknown'
  supports_reasoning_context?: boolean
  supports_stream_options_include_usage?: boolean
  strips_responses_reasoning_context?: boolean
  strips_responses_encrypted_reasoning?: boolean
}

export interface ResponsesCapabilityObservation {
  observed_at?: number
  status_code?: number
  error_code?: string
  reason?: string
}

export interface ResponsesToolCapabilityObservation {
  observed_at?: number
  expires_at?: number
  source?: string
  model?: string
  profile?: string
  transport?: string
  custom_tool_call?: boolean
  namespace_tool_call?: boolean
  tool_search_call?: boolean
}

/**
 * Log statistics data
 */
export interface LogStatistics {
  quota: number
  rpm: number
  tpm: number
  input_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  output_tokens: number
  cache_rate: number | null
}

// ============================================================================
// Drawing Logs (Midjourney) Types
// ============================================================================

export interface MidjourneyLog {
  id: number
  user_id: number
  channel_id: number
  code: number
  mj_id: string
  action: string // IMAGINE, UPSCALE, VARIATION, etc. (backend field name)
  submit_time: number // milliseconds
  finish_time?: number // milliseconds
  start_time?: number // milliseconds
  fail_reason?: string
  progress: string
  prompt: string
  prompt_en?: string
  description?: string
  buttons?: string
  properties?: string
  image_url?: string
  status: string // NOT_START, SUBMITTED, IN_PROGRESS, SUCCESS, FAILURE, MODAL
  other?: string
  created_at?: number
  updated_at?: number
}

// ============================================================================
// Task Logs Types
// ============================================================================

export interface TaskLog {
  id: number
  user_id: number
  username?: string
  platform: string // suno, kling, runway, etc.
  task_id: string
  action: string // MUSIC, LYRICS, GENERATE, TEXT_GENERATE, etc.
  channel_id: number
  submit_time: number // seconds
  finish_time?: number // seconds
  progress?: string
  progress_message_en?: string
  data?: string // JSON string
  fail_reason?: string
  status: string // NOT_START, SUBMITTED, IN_PROGRESS, SUCCESS, FAILURE, QUEUED, UNKNOWN
  other?: string
  created_at?: number
  updated_at?: number
}

// ============================================================================
// Common Log Types
// ============================================================================

export interface GetLogsParams {
  p?: number
  page_size?: number
  type?: number
  username?: string
  token_name?: string
  model_name?: string
  start_timestamp?: number
  end_timestamp?: number
  channel?: number
  group?: string
  request_id?: string
  upstream_request_id?: string
}

export interface GetLogsResponse {
  success: boolean
  message?: string
  data?: {
    items: UsageLog[] | MidjourneyLog[] | TaskLog[]
    total: number
    page: number
    page_size: number
  }
}

export interface GetLogStatsParams {
  type?: number
  username?: string
  token_name?: string
  model_name?: string
  start_timestamp?: number
  end_timestamp?: number
  channel?: number
  group?: string
  request_id?: string
  upstream_request_id?: string
}

export interface GetLogStatsResponse {
  success: boolean
  message?: string
  data?: LogStatistics
}

// ============================================================================
// Drawing Log Types
// ============================================================================

export interface GetMidjourneyLogsParams {
  p?: number
  page_size?: number
  channel_id?: string
  mj_id?: string
  start_timestamp?: number
  end_timestamp?: number
}

// ============================================================================
// Task Log Types
// ============================================================================

export interface GetTaskLogsParams {
  p?: number
  page_size?: number
  channel_id?: string
  task_id?: string
  start_timestamp?: number
  end_timestamp?: number
}

// ============================================================================
// Fetch Logs Configuration
// ============================================================================

/**
 * Configuration for fetching logs by category
 */
export interface FetchLogsConfig {
  logCategory: LogCategory
  isAdmin: boolean
  page: number
  pageSize: number
  searchParams: Record<string, unknown>
  columnFilters: Array<{ id: string; value: unknown }>
}

// ============================================================================
// User Info Types
// ============================================================================

export interface UserInfo {
  id: number
  username: string
  display_name?: string
  quota: number
  used_quota: number
  request_count: number
  group?: string
  aff_code?: string
  aff_count?: number
  aff_quota?: number
  remark?: string
}
