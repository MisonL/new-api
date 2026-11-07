import type {
  LogOtherData,
  ResponsesRoutingAttempt,
  ResponsesRoutingChannelSkip,
  ResponsesRoutingTrace as ResponsesRoutingChannelTraceData,
} from '../types'
import {
  buildResponsesRoutingTrace,
  getResponsesRoutingFinalPath as getFinalResponsesRoutingPath,
} from './responses-routing-trace'

export type ResponsesRoutingEventCode =
  | 'local_gateway_body_reduced'
  | 'local_413_recovery'
  | 'local_413_recovery_failed'
  | 'input_item_ids_normalized'
  | 'compact_context_fallback'
  | 'compact_previous_response_id_fallback'
  | 'compact_summary_model_fallback'
  | 'compact_summary_model'
  | 'compact_summary_models'
  | 'compact_native_attempt'
  | 'compact_auto_fallback'
  | 'compact_auto_fallback_window'
  | 'codex_compaction'
  | 'stream_options_include_usage_retry'
  | 'stream_options_include_usage_capability_stripped'
  | 'remote_compact_synthetic_fallback'
  | 'remote_compact_first_data_timeout'
  | 'encrypted_context_retry'
  | 'encrypted_context_retry_blocked_after_event'
  | 'namespace_history_retry'
  | 'arguments_history_retry'
  | 'compact_visible_only_fallback'
  | 'compact_route_decision'
  | 'compact_fallback_reason'
  | 'compact_channel_skip'
  | 'channel_skip'
  | 'compact_state_restored'
  | 'compact_model_changed'
  | 'previous_id_action'
  | 'reasoning_context_action'
  | 'chat_compat_ignored_encrypted_include'
  | 'chat_compat_reasoning_summary_mapped'

export interface ResponsesRoutingEvent {
  code: ResponsesRoutingEventCode
  value?: string
  source?: string
  originalBodyBytes?: number
  reducedBodyBytes?: number
  inputItems?: number
  removedCount?: number
  upstreamPath?: string
  statusCode?: number
  fallbackReason?: string
  retryUntilUnix?: number
  retryIntervalHours?: number
  client?: string
  implementation?: string
  trigger?: string
  reason?: string
  phase?: string
  strategy?: string
}

export interface ResponsesRoutingEventDisplay {
  key: ResponsesRoutingEventCode
  label: string
  value: string
}

export type ResponsesRoutingTranslator = (
  key: string,
  options?: Record<string, unknown>
) => string

export type ResponsesRoutingBadgeKind =
  'gateway_compacted' | 'route_recovery' | 'route_failure' | 'request_adapted'

export type ResponsesRoutingResultKind =
  | ResponsesRoutingBadgeKind
  | 'synthetic_compaction'
  | 'native_passthrough'
  | 'capability_observed'

export interface ResponsesRoutingResult {
  kind: ResponsesRoutingResultKind
  actionCount: number
}

export type ResponsesRoutingResultTone =
  'cyan' | 'orange' | 'red' | 'indigo' | 'violet' | 'green' | 'neutral'

export interface ResponsesRoutingResultDisplay {
  label: string
  tone: ResponsesRoutingResultTone
}

export interface ResponsesRoutingChannelTrace {
  candidates: number[]
  preflightSkipped: ResponsesRoutingChannelSkip[]
  attemptedChannels: ResponsesRoutingAttempt[]
  finalChannel?: number
  terminalReason?: string
}

const recoveryEventCodes = new Set<ResponsesRoutingEventCode>([
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
  'namespace_history_retry',
  'arguments_history_retry',
  'compact_visible_only_fallback',
  'compact_fallback_reason',
  'compact_channel_skip',
  'channel_skip',
  'compact_model_changed',
])

function positiveSafeInteger(value: number | undefined): number | undefined {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value <= 0) {
    return undefined
  }
  return value
}

function positiveSafeIntegerValue(value: unknown): number | undefined {
  return typeof value === 'number'
    ? positiveSafeInteger(value)
    : typeof value === 'string' && /^\d+$/.test(value.trim())
      ? positiveSafeInteger(Number(value))
      : undefined
}

function normalizeRoutingString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const normalized = value.trim()
  return normalized ? normalized.slice(0, 128) : undefined
}

function normalizeRoutingChannelList(value: unknown): number[] {
  if (!Array.isArray(value)) return []
  const seen = new Set<number>()
  const channels: number[] = []
  for (const item of value.slice(0, 256)) {
    const channel = positiveSafeIntegerValue(item)
    if (channel === undefined || seen.has(channel)) continue
    seen.add(channel)
    channels.push(channel)
  }
  return channels
}

function normalizeRoutingChannelSkips(
  value: unknown
): ResponsesRoutingChannelSkip[] {
  if (!Array.isArray(value)) return []
  const seen = new Set<string>()
  const skips: ResponsesRoutingChannelSkip[] = []
  for (const item of value.slice(0, 256)) {
    if (!item || typeof item !== 'object' || Array.isArray(item)) continue
    const raw = item as Record<string, unknown>
    const channelId = positiveSafeIntegerValue(raw.channel_id)
    const reason = normalizeRoutingString(raw.reason)
    if (channelId === undefined || !reason) continue
    const key = `${channelId}:${reason}`
    if (seen.has(key)) continue
    seen.add(key)
    skips.push({ channel_id: channelId, reason })
  }
  return skips
}

function normalizeRoutingAttempts(value: unknown): ResponsesRoutingAttempt[] {
  if (!Array.isArray(value)) return []
  const attempts: ResponsesRoutingAttempt[] = []
  for (const item of value.slice(0, 256)) {
    if (!item || typeof item !== 'object' || Array.isArray(item)) continue
    const raw = item as Record<string, unknown>
    const channelId = positiveSafeIntegerValue(raw.channel_id)
    if (channelId === undefined) continue
    attempts.push({
      channel_id: channelId,
      status_code: positiveSafeIntegerValue(raw.status_code),
      error_code: normalizeRoutingString(raw.error_code),
      error_category: normalizeRoutingString(raw.error_category),
      outcome: normalizeRoutingString(raw.outcome),
    })
  }
  return attempts
}

export function getResponsesRoutingChannelTrace(
  other: LogOtherData | null | undefined
): ResponsesRoutingChannelTrace | null {
  const raw = other?.responses_routing_trace
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null
  const source = raw as ResponsesRoutingChannelTraceData
  const trace: ResponsesRoutingChannelTrace = {
    candidates: normalizeRoutingChannelList(source.candidates),
    preflightSkipped: normalizeRoutingChannelSkips(source.preflight_skipped),
    attemptedChannels: normalizeRoutingAttempts(source.attempted_channels),
    finalChannel: positiveSafeIntegerValue(source.final_channel),
    terminalReason: normalizeRoutingString(source.terminal_reason),
  }
  if (
    trace.candidates.length === 0 &&
    trace.preflightSkipped.length === 0 &&
    trace.attemptedChannels.length === 0 &&
    trace.finalChannel === undefined &&
    !trace.terminalReason
  ) {
    return null
  }
  return trace
}

export function hasResponsesRoutingChannelTrace(
  trace: ResponsesRoutingChannelTrace | null
): boolean {
  if (!trace) return false
  if (trace.preflightSkipped.length > 0 || trace.attemptedChannels.length > 1) {
    return true
  }
  if (trace.candidates.length > 1) return true
  return Boolean(trace.terminalReason && trace.terminalReason !== 'success')
}

function responsesRoutingChannelTraceActionCount(
  trace: ResponsesRoutingChannelTrace | null
): number {
  if (!trace) return 0
  return trace.preflightSkipped.length + trace.attemptedChannels.length
}

function addLocalGatewayBodyReductionEvent(
  events: ResponsesRoutingEvent[],
  other: LogOtherData
) {
  const source =
    other.responses_codex_local_compaction_body_reduction_source?.trim()
  const ingressReduced =
    other.responses_codex_local_compaction_ingress_reduced === true
  const originalBodyBytes = positiveSafeInteger(
    ingressReduced
      ? other.responses_codex_local_compaction_ingress_original_body_bytes
      : other.responses_codex_local_compaction_original_body_bytes
  )
  const reducedBodyBytes = positiveSafeInteger(
    ingressReduced
      ? other.responses_codex_local_compaction_ingress_reduced_body_bytes
      : other.responses_codex_local_compaction_reduced_body_bytes
  )
  if (
    !ingressReduced &&
    (originalBodyBytes === undefined || reducedBodyBytes === undefined)
  ) {
    return
  }

  events.push({
    code: 'local_gateway_body_reduced',
    source: source || undefined,
    originalBodyBytes,
    reducedBodyBytes,
    inputItems: positiveSafeInteger(
      other.responses_codex_local_compaction_ingress_input_items
    ),
  })
}

function addInputItemIDNormalizationEvent(
  events: ResponsesRoutingEvent[],
  other: LogOtherData
) {
  const removedCount = positiveSafeInteger(
    other.responses_input_item_id_normalization_removed_count
  )
  if (removedCount) {
    events.push({ code: 'input_item_ids_normalized', removedCount })
  }
}

function addStringEvent(
  events: ResponsesRoutingEvent[],
  code: ResponsesRoutingEventCode,
  value: string | undefined
) {
  const normalized = value?.trim()
  if (normalized) events.push({ code, value: normalized })
}

function normalizeStringList(value: string[] | undefined): string[] {
  if (!Array.isArray(value)) return []

  const seen = new Set<string>()
  const values: string[] = []
  for (const item of value) {
    const normalized = item?.trim()
    if (!normalized || seen.has(normalized)) continue
    seen.add(normalized)
    values.push(normalized)
  }
  return values
}

function addCompactFallbackEvents(
  events: ResponsesRoutingEvent[],
  other: LogOtherData
) {
  if (other.responses_compact_context_fallback === true) {
    events.push({ code: 'compact_context_fallback' })
  }
  if (other.responses_compact_previous_response_id_fallback === true) {
    events.push({ code: 'compact_previous_response_id_fallback' })
  }
  if (other.responses_compact_summary_model_fallback === true) {
    events.push({ code: 'compact_summary_model_fallback' })
  }
  addStringEvent(
    events,
    'compact_summary_model',
    other.responses_compact_summary_model
  )
  const summaryModels = normalizeStringList(
    other.responses_compact_summary_models
  )
  if (summaryModels.length > 0) {
    events.push({
      code: 'compact_summary_models',
      value: summaryModels.join(', '),
    })
  }

  if (other.responses_compact_native_attempted === true) {
    events.push({
      code: 'compact_native_attempt',
      upstreamPath:
        other.responses_compact_native_upstream_path?.trim() || undefined,
      statusCode: positiveSafeInteger(
        other.responses_compact_native_status_code
      ),
    })
  }
  if (other.responses_compact_auto_fallback === true) {
    events.push({ code: 'compact_auto_fallback' })
  }
  if (other.responses_compact_auto_fallback_window === true) {
    events.push({
      code: 'compact_auto_fallback_window',
      fallbackReason:
        other.responses_compact_auto_fallback_reason?.trim() || undefined,
      retryUntilUnix: positiveSafeInteger(
        other.responses_compact_auto_fallback_retry_until
      ),
      retryIntervalHours: positiveSafeInteger(
        other.responses_compact_auto_fallback_retry_interval_hours
      ),
    })
  }

  const client = other.responses_compact_client?.trim()
  const implementation = other.responses_compact_client_implementation?.trim()
  const trigger = other.responses_compact_trigger?.trim()
  const reason = other.responses_compact_reason?.trim()
  const phase = other.responses_compact_phase?.trim()
  const strategy = other.responses_compact_strategy?.trim()
  if (client || implementation || trigger || reason || phase || strategy) {
    events.push({
      code: 'codex_compaction',
      client,
      implementation,
      trigger,
      reason,
      phase,
      strategy,
    })
  }
}

function addChannelSkipEvents(
  events: ResponsesRoutingEvent[],
  channelSkips: LogOtherData['responses_routing_channel_skips']
): boolean {
  if (!Array.isArray(channelSkips)) return false

  const seen = new Set<string>()
  let added = false
  for (const channelSkip of channelSkips) {
    const channelId = Number(channelSkip?.channel_id)
    const reason = channelSkip?.reason?.trim()
    if (!Number.isInteger(channelId) || channelId <= 0 || !reason) continue

    const value = `#${channelId}: ${reason}`
    if (seen.has(value)) continue
    seen.add(value)
    events.push({ code: 'channel_skip', value })
    added = true
  }
  return added
}

export function getResponsesRoutingEvents(
  other: LogOtherData | null | undefined
): ResponsesRoutingEvent[] {
  if (!other) return []

  const events: ResponsesRoutingEvent[] = []
  addLocalGatewayBodyReductionEvent(events, other)
  if (other.responses_codex_local_compaction_413_recovery_failed === true) {
    events.push({ code: 'local_413_recovery_failed' })
  } else if (other.responses_codex_local_compaction_413_recovery === true) {
    events.push({ code: 'local_413_recovery' })
  }
  addInputItemIDNormalizationEvent(events, other)
  addCompactFallbackEvents(events, other)
  if (other.responses_stream_options_include_usage_retry === true) {
    events.push({ code: 'stream_options_include_usage_retry' })
  }
  if (
    other.responses_stream_options_include_usage_capability_stripped === true
  ) {
    events.push({ code: 'stream_options_include_usage_capability_stripped' })
  }
  if (other.responses_remote_compact_synthetic_fallback === true) {
    events.push({ code: 'remote_compact_synthetic_fallback' })
  }
  if (other.responses_remote_compact_first_data_timeout === true) {
    events.push({ code: 'remote_compact_first_data_timeout' })
  }
  if (other.responses_encrypted_context_retry === true) {
    events.push({ code: 'encrypted_context_retry' })
  }
  addStringEvent(
    events,
    'encrypted_context_retry_blocked_after_event',
    other.responses_encrypted_context_retry_blocked_after_event
  )
  if (other.responses_namespace_history_retry === true) {
    events.push({ code: 'namespace_history_retry' })
  }
  if (other.responses_arguments_history_retry === true) {
    events.push({ code: 'arguments_history_retry' })
  }
  if (other.responses_compact_visible_only_fallback === true) {
    events.push({ code: 'compact_visible_only_fallback' })
  }
  addStringEvent(
    events,
    'compact_route_decision',
    other.responses_compact_route_decision
  )
  addStringEvent(
    events,
    'compact_fallback_reason',
    other.responses_compact_fallback_reason
  )
  if (!addChannelSkipEvents(events, other.responses_routing_channel_skips)) {
    addStringEvent(
      events,
      'compact_channel_skip',
      other.responses_compact_channel_skip
    )
  }
  if (other.responses_compact_state_restored === true) {
    events.push({ code: 'compact_state_restored' })
  }
  if (other.responses_compact_model_changed === true) {
    events.push({ code: 'compact_model_changed' })
  }
  addStringEvent(
    events,
    'previous_id_action',
    other.responses_previous_id_action
  )
  addStringEvent(
    events,
    'reasoning_context_action',
    other.responses_reasoning_context_action
  )
  if (other.responses_chat_compat_ignored_encrypted_include === true) {
    events.push({ code: 'chat_compat_ignored_encrypted_include' })
  }
  if (other.responses_chat_compat_reasoning_summary_mapped === true) {
    events.push({ code: 'chat_compat_reasoning_summary_mapped' })
  }
  return events
}

export function getResponsesRoutingTrace(
  other: LogOtherData | null | undefined
) {
  return buildResponsesRoutingTrace(getResponsesRoutingEvents(other))
}

export function getResponsesRoutingFinalPath(
  other: LogOtherData | null | undefined
) {
  return getFinalResponsesRoutingPath(other)
}

export function hasResponsesRoutingEvents(
  other: LogOtherData | null | undefined
): boolean {
  return getResponsesRoutingEvents(other).length > 0
}

export function getResponsesRoutingBadgeKind(
  other: LogOtherData | null | undefined
): ResponsesRoutingBadgeKind | null {
  const events = getResponsesRoutingEvents(other)
  const channelTrace = getResponsesRoutingChannelTrace(other)
  if (events.length === 0 && !hasResponsesRoutingChannelTrace(channelTrace)) {
    return null
  }
  if (
    channelTrace &&
    hasResponsesRoutingChannelTrace(channelTrace) &&
    channelTrace.terminalReason &&
    channelTrace.terminalReason !== 'success' &&
    !channelTrace.attemptedChannels.some(
      (attempt) => attempt.outcome === 'success'
    )
  ) {
    return 'route_failure'
  }
  if (
    channelTrace &&
    hasResponsesRoutingChannelTrace(channelTrace) &&
    (channelTrace.preflightSkipped.length > 0 ||
      channelTrace.attemptedChannels.length > 1 ||
      channelTrace.candidates.length > 1)
  ) {
    return 'route_recovery'
  }
  if (events.some((event) => event.code === 'local_413_recovery_failed')) {
    return 'route_failure'
  }
  if (events.some((event) => recoveryEventCodes.has(event.code))) {
    return 'route_recovery'
  }
  if (events.some((event) => event.code === 'local_gateway_body_reduced')) {
    return 'gateway_compacted'
  }
  return 'request_adapted'
}

export function hasResponsesRoutingDetails(
  other: LogOtherData | null | undefined
): boolean {
  const channelTrace = getResponsesRoutingChannelTrace(other)
  return Boolean(
    other?.responses_compact_mode ||
    other?.channel_capability_snapshot ||
    hasResponsesRoutingEvents(other) ||
    hasResponsesRoutingChannelTrace(channelTrace)
  )
}

export function getResponsesRoutingResult(
  other: LogOtherData | null | undefined
): ResponsesRoutingResult | null {
  if (!hasResponsesRoutingDetails(other)) return null

  const events = getResponsesRoutingEvents(other)
  const channelTrace = getResponsesRoutingChannelTrace(other)
  const channelTraceIsMeaningful = hasResponsesRoutingChannelTrace(channelTrace)
  const actionCount =
    events.length + responsesRoutingChannelTraceActionCount(channelTrace)
  if (
    channelTraceIsMeaningful &&
    channelTrace?.terminalReason &&
    channelTrace.terminalReason !== 'success' &&
    !channelTrace.attemptedChannels.some(
      (attempt) => attempt.outcome === 'success'
    )
  ) {
    return { kind: 'route_failure', actionCount }
  }
  if (
    channelTrace &&
    channelTraceIsMeaningful &&
    (channelTrace.preflightSkipped.length > 0 ||
      channelTrace.attemptedChannels.length > 1 ||
      channelTrace.candidates.length > 1)
  ) {
    return { kind: 'route_recovery', actionCount }
  }
  if (events.some((event) => event.code === 'local_413_recovery_failed')) {
    return { kind: 'route_failure', actionCount }
  }
  if (events.some((event) => recoveryEventCodes.has(event.code))) {
    return { kind: 'route_recovery', actionCount }
  }
  if (events.some((event) => event.code === 'local_gateway_body_reduced')) {
    return { kind: 'gateway_compacted', actionCount }
  }
  if (actionCount > 0) {
    return { kind: 'request_adapted', actionCount }
  }

  switch (other?.responses_compact_mode) {
    case 'synthetic_summary':
      return { kind: 'synthetic_compaction', actionCount }
    case 'native':
      return { kind: 'native_passthrough', actionCount }
    default:
      return { kind: 'capability_observed', actionCount }
  }
}

export function getResponsesRoutingResultDisplay(
  result: ResponsesRoutingResult,
  t: ResponsesRoutingTranslator
): ResponsesRoutingResultDisplay {
  switch (result.kind) {
    case 'gateway_compacted':
      return { label: t('Gateway Compacted'), tone: 'cyan' }
    case 'route_recovery':
      return { label: t('Route Recovery'), tone: 'orange' }
    case 'route_failure':
      return { label: t('Route Recovery Failed'), tone: 'red' }
    case 'request_adapted':
      return { label: t('Request Adapted'), tone: 'indigo' }
    case 'synthetic_compaction':
      return { label: t('Synthetic Compaction'), tone: 'violet' }
    case 'native_passthrough':
      return { label: t('Native Passthrough'), tone: 'green' }
    case 'capability_observed':
      return { label: t('Capability Observed'), tone: 'neutral' }
  }
}

export function formatResponsesRoutingBytes(bytes: number): string {
  const units = ['B', 'KiB', 'MiB', 'GiB']
  let value = bytes
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex++
  }
  return unitIndex === 0
    ? `${value} ${units[unitIndex]}`
    : `${value.toFixed(1)} ${units[unitIndex]}`
}

function formatResponsesRoutingTimestamp(
  unix: number | undefined
): string | undefined {
  if (!unix) return undefined
  const timestamp = new Date(unix * 1000)
  if (Number.isNaN(timestamp.getTime())) return undefined
  return timestamp
    .toISOString()
    .replace('T', ' ')
    .replace(/\.\d{3}Z$/, ' UTC')
}

function formatNativeCompactionAttempt(
  event: ResponsesRoutingEvent,
  t: ResponsesRoutingTranslator
): string {
  const details: string[] = []
  if (event.statusCode !== undefined) {
    details.push(t('Status: {{status}}', { status: event.statusCode }))
  }
  if (event.upstreamPath) {
    details.push(t('Path: {{path}}', { path: event.upstreamPath }))
  }
  return details.length > 0 ? details.join('; ') : t('Attempted')
}

function formatAutoFallbackWindow(
  event: ResponsesRoutingEvent,
  t: ResponsesRoutingTranslator
): string {
  const details: string[] = []
  if (event.fallbackReason) {
    details.push(
      t('Fallback Reason: {{reason}}', { reason: event.fallbackReason })
    )
  }
  const retryUntil = formatResponsesRoutingTimestamp(event.retryUntilUnix)
  if (event.retryIntervalHours !== undefined || retryUntil) {
    details.push(
      t('Retry Window: {{hours}}h until {{time}}', {
        hours: event.retryIntervalHours ?? '-',
        time: retryUntil ?? '-',
      })
    )
  }
  return details.length > 0 ? details.join('; ') : t('Active')
}

function formatCodexCompaction(
  event: ResponsesRoutingEvent,
  t: ResponsesRoutingTranslator
): string {
  const details: string[] = []
  if (event.implementation) {
    details.push(
      t('Implementation: {{value}}', { value: event.implementation })
    )
  }
  if (event.trigger) {
    details.push(t('Trigger: {{value}}', { value: event.trigger }))
  }
  if (event.reason) {
    details.push(t('Reason: {{value}}', { value: event.reason }))
  }
  if (event.phase) {
    details.push(t('Phase: {{value}}', { value: event.phase }))
  }
  if (event.strategy) {
    details.push(t('Strategy: {{value}}', { value: event.strategy }))
  }
  return details.length > 0 ? details.join('; ') : event.client || t('Observed')
}

function getGatewayBodyReductionSourceLabel(
  source: string | undefined,
  t: ResponsesRoutingTranslator
): string | undefined {
  switch (source) {
    case 'ingress_preemptive_size':
    case 'preemptive_size':
      return t('Preemptive size protection')
    case 'upstream_413':
      return t('Upstream 413 recovery')
    default:
      return undefined
  }
}

function getGatewayBodyReductionValue(
  event: ResponsesRoutingEvent,
  t: ResponsesRoutingTranslator
): string {
  const parts: string[] = []
  const source = getGatewayBodyReductionSourceLabel(event.source, t)
  if (source) parts.push(source)
  if (
    event.originalBodyBytes !== undefined &&
    event.reducedBodyBytes !== undefined
  ) {
    parts.push(
      t('Request size: {{before}} -> {{after}}', {
        before: formatResponsesRoutingBytes(event.originalBodyBytes),
        after: formatResponsesRoutingBytes(event.reducedBodyBytes),
      })
    )
  } else {
    parts.push(t('Request size reduced'))
  }
  if (event.inputItems !== undefined) {
    parts.push(t('Input items: {{count}}', { count: event.inputItems }))
  }
  const details = parts.slice(source ? 1 : 0).join('; ')
  return source ? `${source}: ${details}` : details
}

export function getResponsesRoutingEventDisplay(
  event: ResponsesRoutingEvent,
  t: ResponsesRoutingTranslator
): ResponsesRoutingEventDisplay {
  switch (event.code) {
    case 'local_gateway_body_reduced':
      return {
        key: event.code,
        label: t('Gateway Compacted'),
        value: getGatewayBodyReductionValue(event, t),
      }
    case 'local_413_recovery':
      return {
        key: event.code,
        label: t('Local 413 Recovery'),
        value: t('Retried after compaction'),
      }
    case 'local_413_recovery_failed':
      return {
        key: event.code,
        label: t('Local 413 Recovery'),
        value: t('Recovery failed after compaction'),
      }
    case 'input_item_ids_normalized':
      return {
        key: event.code,
        label: t('Input Item IDs Normalized'),
        value: t('Removed invalid item IDs: {{count}}', {
          count: event.removedCount ?? 0,
        }),
      }
    case 'compact_context_fallback':
      return {
        key: event.code,
        label: t('Compaction Context Fallback'),
        value: t('Applied'),
      }
    case 'compact_previous_response_id_fallback':
      return {
        key: event.code,
        label: t('Previous Response Fallback'),
        value: t('Applied'),
      }
    case 'compact_summary_model_fallback':
      return {
        key: event.code,
        label: t('Summary Model Fallback'),
        value: t('Applied'),
      }
    case 'compact_summary_model':
      return {
        key: event.code,
        label: t('Summary Model'),
        value: event.value || '-',
      }
    case 'compact_summary_models':
      return {
        key: event.code,
        label: t('Summary Models'),
        value: event.value || '-',
      }
    case 'compact_native_attempt':
      return {
        key: event.code,
        label: t('Native Compaction Attempt'),
        value: formatNativeCompactionAttempt(event, t),
      }
    case 'compact_auto_fallback':
      return {
        key: event.code,
        label: t('Auto Fallback Activated'),
        value: t('Applied'),
      }
    case 'compact_auto_fallback_window':
      return {
        key: event.code,
        label: t('Auto Fallback Window'),
        value: formatAutoFallbackWindow(event, t),
      }
    case 'codex_compaction':
      return {
        key: event.code,
        label: t('Codex Compaction'),
        value: formatCodexCompaction(event, t),
      }
    case 'stream_options_include_usage_retry':
      return {
        key: event.code,
        label: t('Stream Usage Retry'),
        value: t('Recovered'),
      }
    case 'stream_options_include_usage_capability_stripped':
      return {
        key: event.code,
        label: t('Stream Usage Omitted'),
        value: t('Learned Upstream Capability'),
      }
    case 'remote_compact_synthetic_fallback':
      return {
        key: event.code,
        label: t('Remote Compaction Recovery'),
        value: t('Synthetic Summary'),
      }
    case 'remote_compact_first_data_timeout':
      return {
        key: event.code,
        label: t('Remote Compaction First Data Event Timeout'),
        value: t('Observed'),
      }
    case 'encrypted_context_retry':
      return {
        key: event.code,
        label: t('Encrypted Context Retry'),
        value: t('Retried'),
      }
    case 'encrypted_context_retry_blocked_after_event':
      return {
        key: event.code,
        label: t('Encrypted Context Retry Blocked'),
        value: event.value || '-',
      }
    case 'namespace_history_retry':
      return {
        key: event.code,
        label: t('Namespace History Retry'),
        value: t('Retried'),
      }
    case 'arguments_history_retry':
      return {
        key: event.code,
        label: t('Tool Arguments History Retry'),
        value: t('Retried'),
      }
    case 'compact_visible_only_fallback':
      return {
        key: event.code,
        label: t('Visible-only Fallback'),
        value: t('Active'),
      }
    case 'compact_route_decision':
      return {
        key: event.code,
        label: t('Route Decision'),
        value: event.value || '-',
      }
    case 'compact_fallback_reason':
      return {
        key: event.code,
        label: t('Fallback Reason'),
        value: event.value || '-',
      }
    case 'compact_channel_skip':
      return {
        key: event.code,
        label: t('Channel Skip'),
        value: event.value || '-',
      }
    case 'channel_skip':
      return {
        key: event.code,
        label: t('Channel Skip'),
        value: event.value || '-',
      }
    case 'compact_state_restored':
      return {
        key: event.code,
        label: t('Compaction State Restored'),
        value: t('Yes'),
      }
    case 'compact_model_changed':
      return {
        key: event.code,
        label: t('Compaction Model Changed'),
        value: t('Yes'),
      }
    case 'previous_id_action':
      return {
        key: event.code,
        label: t('Previous Response Action'),
        value: event.value || '-',
      }
    case 'reasoning_context_action':
      return {
        key: event.code,
        label: t('Reasoning Context Action'),
        value: event.value || '-',
      }
    case 'chat_compat_ignored_encrypted_include':
      return {
        key: event.code,
        label: t('Chat Compatibility'),
        value: t('Ignored Encrypted Include'),
      }
    case 'chat_compat_reasoning_summary_mapped':
      return {
        key: event.code,
        label: t('Chat Compatibility'),
        value: t('Mapped Reasoning Summary'),
      }
  }
}
