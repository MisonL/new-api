export type ResponsesRoutingTraceStage =
  'request_preparation' | 'route_selection' | 'upstream_attempt' | 'recovery'

export type ResponsesRoutingTraceTone =
  'adaptation' | 'failure' | 'neutral' | 'recovery'

export interface ResponsesRoutingTraceEvent {
  code: string
  source?: string
  statusCode?: number
}

export interface ResponsesRoutingTraceItem<
  TEvent extends ResponsesRoutingTraceEvent = ResponsesRoutingTraceEvent,
> {
  stage: ResponsesRoutingTraceStage
  event: TEvent
}

export type ResponsesRoutingTraceTranslator = (
  key: string,
  options?: Record<string, unknown>
) => string

const traceStages: readonly ResponsesRoutingTraceStage[] = [
  'request_preparation',
  'route_selection',
  'upstream_attempt',
  'recovery',
]

const requestPreparationCodes = new Set([
  'input_item_ids_normalized',
  'reasoning_context_action',
  'chat_compat_ignored_encrypted_include',
  'chat_compat_reasoning_summary_mapped',
  'stream_options_include_usage_capability_stripped',
])

const upstreamAttemptCodes = new Set([
  'compact_native_attempt',
  'remote_compact_first_data_timeout',
])

const recoveryCodes = new Set([
  'local_413_recovery',
  'local_413_recovery_failed',
  'compact_context_fallback',
  'compact_previous_response_id_fallback',
  'compact_summary_model_fallback',
  'compact_summary_model',
  'compact_summary_models',
  'compact_auto_fallback',
  'compact_auto_fallback_window',
  'stream_options_include_usage_retry',
  'remote_compact_synthetic_fallback',
  'encrypted_context_retry',
  'encrypted_context_retry_blocked_after_event',
  'namespace_history_retry',
  'arguments_history_retry',
  'compact_visible_only_fallback',
  'compact_fallback_reason',
  'compact_state_restored',
  'compact_model_changed',
  'previous_id_action',
])

const recoveryToneCodes = new Set([
  'local_413_recovery',
  'compact_context_fallback',
  'compact_previous_response_id_fallback',
  'compact_summary_model_fallback',
  'compact_auto_fallback',
  'compact_auto_fallback_window',
  'stream_options_include_usage_retry',
  'remote_compact_synthetic_fallback',
  'remote_compact_first_data_timeout',
  'encrypted_context_retry',
  'encrypted_context_retry_blocked_after_event',
  'namespace_history_retry',
  'arguments_history_retry',
  'compact_visible_only_fallback',
  'compact_fallback_reason',
  'compact_state_restored',
  'compact_model_changed',
  'previous_id_action',
])

function getTraceStage(
  event: ResponsesRoutingTraceEvent
): ResponsesRoutingTraceStage {
  if (
    event.code === 'local_gateway_body_reduced' &&
    event.source === 'upstream_413'
  ) {
    return 'recovery'
  }
  if (event.code === 'local_gateway_body_reduced') {
    return 'request_preparation'
  }
  if (requestPreparationCodes.has(event.code)) {
    return 'request_preparation'
  }
  if (upstreamAttemptCodes.has(event.code)) {
    return 'upstream_attempt'
  }
  if (recoveryCodes.has(event.code)) {
    return 'recovery'
  }
  return 'route_selection'
}

export function buildResponsesRoutingTrace<
  TEvent extends ResponsesRoutingTraceEvent,
>(events: readonly TEvent[]): ResponsesRoutingTraceItem<TEvent>[] {
  const trace: ResponsesRoutingTraceItem<TEvent>[] = []
  for (const stage of traceStages) {
    for (const event of events) {
      if (getTraceStage(event) === stage) {
        trace.push({ stage, event })
      }
    }
  }
  return trace
}

function readPath(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const path = value.trim()
  return path || undefined
}

export function getResponsesRoutingFinalPath(
  other: unknown
): string | undefined {
  if (!other || typeof other !== 'object') return undefined
  const routingOther = other as Record<string, unknown>
  return (
    readPath(routingOther.responses_compact_final_upstream_path) ||
    readPath(routingOther.upstream_request_path) ||
    readPath(routingOther.responses_compact_upstream_path)
  )
}

export function getResponsesRoutingTraceStageDisplay(
  stage: ResponsesRoutingTraceStage,
  t: ResponsesRoutingTraceTranslator
): string {
  switch (stage) {
    case 'request_preparation':
      return t('Request Preparation')
    case 'route_selection':
      return t('Route Selection')
    case 'upstream_attempt':
      return t('Upstream Attempt')
    case 'recovery':
      return t('Recovery')
  }
}

export function getResponsesRoutingTraceTone(
  event: ResponsesRoutingTraceEvent
): ResponsesRoutingTraceTone {
  if (
    event.code === 'local_413_recovery_failed' ||
    (event.code === 'compact_native_attempt' &&
      typeof event.statusCode === 'number' &&
      event.statusCode >= 400)
  ) {
    return 'failure'
  }
  if (
    (event.code === 'local_gateway_body_reduced' &&
      event.source === 'upstream_413') ||
    recoveryToneCodes.has(event.code)
  ) {
    return 'recovery'
  }
  if (getTraceStage(event) === 'request_preparation') {
    return 'adaptation'
  }
  return 'neutral'
}

export function shouldIncludeResponsesRoutingColumn(isAdmin: boolean): boolean {
  return isAdmin === true
}
