import type { StatusBadgeProps } from '@/components/status-badge'

export type ChannelHealthRuntimeState =
  | 'cooldown'
  | 'half_open_probe'
  | 'awaiting_half_open_probe'
  | (string & {})

export type ChannelHealthRuntimeEntry = {
  group: string
  model_name: string
  channel_id: number
  state: ChannelHealthRuntimeState
  cooldown_remaining_seconds?: number
  probe_lease_remaining_seconds?: number
  status_code?: number
  error_code?: string
  route_key?: string
  effective_model?: string
  protocol?: string
  upstream_path?: string
  multi_key_index?: number
}

export type ChannelHealthRuntimeEventAction =
  | 'cooldown'
  | 'half_open_probe'
  | 'half_open_busy'
  | 'half_open_recovered'
  | 'half_open_recovery_superseded'
  | 'half_open_released'
  | 'healthy'
  | 'redis_unavailable'
  | (string & {})

export type ChannelHealthRuntimeEvent = {
  occurred_at: number
  group: string
  model_name: string
  channel_id: number
  action: ChannelHealthRuntimeEventAction
  status_code?: number
  error_code?: string
  route_key?: string
  effective_model?: string
  protocol?: string
  upstream_path?: string
  multi_key_index?: number
}

export type ChannelHealthRuntimeStatus = {
  enabled: boolean
  redis_available: boolean
  cooldown_seconds: number
  probe_lease_seconds: number
  concurrency?: {
    acquire_success: number
    acquire_rejected: number
    acquire_unavailable: number
    reselect_skipped: number
    strict_429: number
    redis_unavailable: number
    renew_failure: number
    release_failure: number
    saturated_events: number
    current_leases_local: number
  }
  entries: ChannelHealthRuntimeEntry[]
  recent_events: ChannelHealthRuntimeEvent[]
}

export type ChannelHealthRuntimeChannelSearch = {
  page: number
  filter: string
  status: string[]
  type: string[]
  group: string[]
  model: string
  runtimeChannelFocus: number
}

type RuntimeSummaryState = 'disabled' | 'redis_unavailable' | 'active'

type RuntimePresentationState = RuntimeSummaryState | 'loading' | 'unavailable'

export type ChannelHealthRuntimeSummary = {
  state: RuntimeSummaryState
  cooling: number
  probing: number
  awaitingProbe: number
}

export type ChannelHealthRuntimePresentation = {
  state: RuntimePresentationState
  labelKey: string
  variant: StatusBadgeProps['variant']
}

export type ChannelHealthRuntimeStateBehavior = {
  impactKey: string
  nextStepKey: string
}

const ACTIVE_RUNTIME_REFRESH_MS = 1000
const IDLE_RUNTIME_REFRESH_MS = 5000

export function getChannelHealthRuntimeStateDisplay(
  state: ChannelHealthRuntimeState
): { labelKey: string; variant: StatusBadgeProps['variant'] } {
  switch (state) {
    case 'cooldown':
      return { labelKey: 'Cooling down', variant: 'warning' }
    case 'half_open_probe':
      return { labelKey: 'Half-open probe active', variant: 'info' }
    case 'awaiting_half_open_probe':
      return { labelKey: 'Awaiting half-open probe', variant: 'neutral' }
    default:
      return { labelKey: 'Unknown runtime state', variant: 'neutral' }
  }
}

export function getChannelHealthRuntimeStateBehavior(
  state: ChannelHealthRuntimeState
): ChannelHealthRuntimeStateBehavior {
  switch (state) {
    case 'cooldown':
      return {
        impactKey:
          'Matching group and model requests bypass this channel while cooldown remains.',
        nextStepKey:
          'When cooldown expires, the next matching request may acquire a recovery probe.',
      }
    case 'half_open_probe':
      return {
        impactKey:
          'One matching request is probing this channel while competing requests bypass it.',
        nextStepKey:
          'A successful probe clears the temporary exclusion; a qualifying failure restarts cooldown.',
      }
    case 'awaiting_half_open_probe':
      return {
        impactKey:
          'Cooldown has ended and this channel is waiting for a recovery probe.',
        nextStepKey:
          'The next matching request may acquire the single recovery probe.',
      }
    default:
      return {
        impactKey:
          'The current runtime state does not define a routing exclusion.',
        nextStepKey:
          'The next transition is not available for this runtime state.',
      }
  }
}

const channelHealthRuntimeStatePriority: Record<string, number> = {
  cooldown: 3,
  half_open_probe: 2,
  awaiting_half_open_probe: 1,
}

export function getChannelHealthRuntimeEntriesForChannel(
  runtime: ChannelHealthRuntimeStatus | null | undefined,
  channelID: number
): ChannelHealthRuntimeEntry[] {
  if (
    !runtime?.enabled ||
    !runtime.redis_available ||
    !Number.isInteger(channelID)
  ) {
    return []
  }

  return (Array.isArray(runtime.entries) ? runtime.entries : []).filter(
    (entry) => entry.channel_id === channelID
  )
}

// Channel IDs remain stable even when an operator changes a channel's groups
// or models after a health event was recorded. Locate by ID and clear stale
// table filters so the affected channel remains reachable from runtime state.
export function getChannelHealthRuntimeChannelSearch(
  entry: ChannelHealthRuntimeEntry
): ChannelHealthRuntimeChannelSearch | null {
  if (!Number.isSafeInteger(entry.channel_id) || entry.channel_id <= 0) {
    return null
  }

  return {
    page: 1,
    filter: String(entry.channel_id),
    status: [],
    type: [],
    group: [],
    model: '',
    runtimeChannelFocus: entry.channel_id,
  }
}

export function getChannelHealthRuntimeDominantEntry(
  entries: ChannelHealthRuntimeEntry[]
): ChannelHealthRuntimeEntry | null {
  return entries.reduce<ChannelHealthRuntimeEntry | null>((current, entry) => {
    if (!current) return entry
    const currentPriority =
      channelHealthRuntimeStatePriority[current.state] ?? 0
    const entryPriority = channelHealthRuntimeStatePriority[entry.state] ?? 0
    return entryPriority > currentPriority ? entry : current
  }, null)
}

export function getChannelHealthRuntimeEntryKey(
  entry: ChannelHealthRuntimeEntry
): string {
  return JSON.stringify([
    entry.group,
    entry.model_name,
    entry.channel_id,
    entry.state,
    entry.route_key ?? '',
    entry.effective_model ?? '',
    entry.protocol ?? '',
    entry.upstream_path ?? '',
    entry.multi_key_index ?? '',
  ])
}

export function getChannelHealthRuntimeRouteSummary(
  entry: ChannelHealthRuntimeEntry
): string {
  const details = [
    entry.effective_model ? `model=${entry.effective_model}` : '',
    entry.protocol ? `protocol=${entry.protocol}` : '',
    entry.upstream_path ? `path=${entry.upstream_path}` : '',
    Number.isInteger(entry.multi_key_index)
      ? `key=${entry.multi_key_index}`
      : '',
    entry.route_key ? `route=${entry.route_key.slice(0, 12)}` : '',
  ].filter(Boolean)
  return details.length > 0 ? details.join(' | ') : '-'
}

export function getChannelHealthRuntimeChannelPresentation(
  entries: ChannelHealthRuntimeEntry[]
): { labelKey: string; variant: StatusBadgeProps['variant'] } | null {
  const mostRestrictiveEntry = getChannelHealthRuntimeDominantEntry(entries)

  return mostRestrictiveEntry
    ? getChannelHealthRuntimeStateDisplay(mostRestrictiveEntry.state)
    : null
}

function readTrimmedString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const normalized = value.trim()
  return normalized || undefined
}

function readPositiveInteger(value: unknown): number | undefined {
  const normalized =
    typeof value === 'number'
      ? value
      : typeof value === 'string' && value.trim()
        ? Number(value)
        : Number.NaN
  return Number.isSafeInteger(normalized) && normalized > 0
    ? normalized
    : undefined
}

function readNonNegativeInteger(value: unknown): number | undefined {
  const normalized =
    typeof value === 'number'
      ? value
      : typeof value === 'string' && value.trim()
        ? Number(value)
        : Number.NaN
  return Number.isSafeInteger(normalized) && normalized >= 0
    ? normalized
    : undefined
}

export function getChannelHealthRuntimeEvents(
  runtime: ChannelHealthRuntimeStatus | null | undefined
): ChannelHealthRuntimeEvent[] {
  const events = Array.isArray(runtime?.recent_events)
    ? runtime.recent_events
    : []

  return events.flatMap((event) => {
    const occurredAt = readPositiveInteger(event?.occurred_at)
    const channelID = readPositiveInteger(event?.channel_id)
    const group = readTrimmedString(event?.group)
    const modelName = readTrimmedString(event?.model_name)
    const action = readTrimmedString(event?.action)
    if (!occurredAt || !channelID || !group || !modelName || !action) {
      return []
    }

    const statusCode = readPositiveInteger(event?.status_code)
    const errorCode = readTrimmedString(event?.error_code)
    const routeKey = readTrimmedString(event?.route_key)
    const effectiveModel = readTrimmedString(event?.effective_model)
    const protocol = readTrimmedString(event?.protocol)
    const upstreamPath = readTrimmedString(event?.upstream_path)
    const multiKeyIndex = readNonNegativeInteger(event?.multi_key_index)
    return [
      {
        occurred_at: occurredAt,
        group,
        model_name: modelName,
        channel_id: channelID,
        action,
        ...(statusCode ? { status_code: statusCode } : {}),
        ...(errorCode ? { error_code: errorCode } : {}),
        ...(routeKey ? { route_key: routeKey } : {}),
        ...(effectiveModel ? { effective_model: effectiveModel } : {}),
        ...(protocol ? { protocol } : {}),
        ...(upstreamPath ? { upstream_path: upstreamPath } : {}),
        ...(multiKeyIndex !== undefined
          ? { multi_key_index: multiKeyIndex }
          : {}),
      },
    ]
  })
}

export function getChannelHealthRuntimeEventDisplay(
  event: ChannelHealthRuntimeEvent
): { labelKey: string; variant: StatusBadgeProps['variant'] } {
  switch (event.action) {
    case 'cooldown':
      return { labelKey: 'Channel Cooling', variant: 'warning' }
    case 'half_open_probe':
      return { labelKey: 'Half-open Probe', variant: 'info' }
    case 'half_open_busy':
      return { labelKey: 'Half-open Probe Busy', variant: 'warning' }
    case 'half_open_recovered':
      return { labelKey: 'Half-open Recovered', variant: 'success' }
    case 'half_open_recovery_superseded':
      return { labelKey: 'Probe Recovery Superseded', variant: 'danger' }
    case 'half_open_released':
      return { labelKey: 'Half-open Probe Released', variant: 'neutral' }
    case 'healthy':
      return { labelKey: 'Health Gate Passed', variant: 'blue' }
    case 'redis_unavailable':
      return { labelKey: 'Health Gate Unavailable', variant: 'danger' }
    default:
      return { labelKey: 'Health Gate Active', variant: 'neutral' }
  }
}

export function getChannelHealthRuntimeRefreshDelay(
  runtime: ChannelHealthRuntimeStatus | null | undefined
): number {
  const entries = Array.isArray(runtime?.entries) ? runtime.entries : []
  const hasTimeSensitiveState = entries.some(
    (entry) => entry?.state === 'cooldown' || entry?.state === 'half_open_probe'
  )
  return runtime?.enabled && runtime.redis_available && hasTimeSensitiveState
    ? ACTIVE_RUNTIME_REFRESH_MS
    : IDLE_RUNTIME_REFRESH_MS
}

export function summarizeChannelHealthRuntime(
  status: ChannelHealthRuntimeStatus
): ChannelHealthRuntimeSummary {
  const entries = Array.isArray(status.entries) ? status.entries : []
  const cooling = entries.filter((entry) => entry.state === 'cooldown').length
  const probing = entries.filter(
    (entry) => entry.state === 'half_open_probe'
  ).length
  const awaitingProbe = entries.filter(
    (entry) => entry.state === 'awaiting_half_open_probe'
  ).length

  if (!status.enabled) {
    return { state: 'disabled', cooling, probing, awaitingProbe }
  }
  if (!status.redis_available) {
    return { state: 'redis_unavailable', cooling, probing, awaitingProbe }
  }
  return { state: 'active', cooling, probing, awaitingProbe }
}

export function getChannelHealthRuntimePresentation(
  runtime: ChannelHealthRuntimeStatus | null,
  unavailable: boolean
): ChannelHealthRuntimePresentation {
  if (unavailable) {
    return {
      state: 'unavailable',
      labelKey: 'Runtime status unavailable',
      variant: 'danger',
    }
  }
  if (!runtime) {
    return {
      state: 'loading',
      labelKey: 'Loading runtime state',
      variant: 'neutral',
    }
  }

  const summary = summarizeChannelHealthRuntime(runtime)
  switch (summary.state) {
    case 'redis_unavailable':
      return {
        state: summary.state,
        labelKey: 'Redis unavailable: routing fails open',
        variant: 'danger',
      }
    case 'active':
      return {
        state: summary.state,
        labelKey: 'Health routing active',
        variant: 'success',
      }
    default:
      return {
        state: summary.state,
        labelKey: 'Health routing disabled',
        variant: 'neutral',
      }
  }
}
