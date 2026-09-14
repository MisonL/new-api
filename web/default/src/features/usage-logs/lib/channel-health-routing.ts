import type { LogOtherData } from '../types'

export type KnownChannelHealthRoutingAction =
  | 'healthy'
  | 'cooldown'
  | 'half_open_probe'
  | 'half_open_busy'
  | 'half_open_recovered'
  | 'half_open_recovery_superseded'
  | 'half_open_released'
  | 'affinity_bypassed'
  | 'redis_unavailable'

export type ChannelHealthRoutingAction =
  | KnownChannelHealthRoutingAction
  | (string & {})

export interface ChannelHealthRoutingEvent {
  group?: string
  modelName?: string
  channelId: number
  action: ChannelHealthRoutingAction
  statusCode?: number
  errorCode?: string
  routeKey?: string
  effectiveModel?: string
  protocol?: string
  upstreamPath?: string
  multiKeyIndex?: number
}

export interface ChannelHealthRoutingEventDisplay {
  key: ChannelHealthRoutingAction
  label: string
  value: string
}

export type ChannelHealthRoutingTranslator = (
  key: string,
  options?: Record<string, unknown>
) => string

export type ChannelHealthRoutingResultKind =
  | 'healthy'
  | 'gate_unavailable'
  | 'probe_recovery_superseded'
  | 'channel_cooling'
  | 'probe_busy'
  | 'probe_recovered'
  | 'probe_active'
  | 'probe_released'
  | 'affinity_bypassed'
  | 'health_action'

export interface ChannelHealthRoutingResult {
  kind: ChannelHealthRoutingResultKind
  actionCount: number
}

export type ChannelHealthRoutingTone =
  | 'danger'
  | 'warning'
  | 'info'
  | 'success'
  | 'blue'
  | 'neutral'

export interface ChannelHealthRoutingResultDisplay {
  label: string
  tone: ChannelHealthRoutingTone
}

type ChannelHealthRoutingInfo = NonNullable<
  LogOtherData['admin_info']
>['channel_health']

function readTrimmedString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const normalized = value.trim()
  return normalized || undefined
}

function readNonNegativeInteger(value: unknown): number | undefined {
  const normalized =
    typeof value === 'number'
      ? value
      : typeof value === 'string' && value.trim()
        ? Number(value)
        : Number.NaN
  if (!Number.isSafeInteger(normalized) || normalized < 0) return undefined
  return normalized
}

function readPositiveInteger(value: unknown): number | undefined {
  const normalized = readNonNegativeInteger(value)
  return normalized && normalized > 0 ? normalized : undefined
}

function getChannelHealthInfo(
  other: LogOtherData | null | undefined
): ChannelHealthRoutingInfo | undefined {
  return other?.admin_info?.channel_health
}

function eventKey(event: ChannelHealthRoutingEvent): string {
  return [
    event.group ?? '',
    event.modelName ?? '',
    event.channelId,
    event.action,
    event.statusCode ?? '',
    event.errorCode ?? '',
    event.routeKey ?? '',
    event.effectiveModel ?? '',
    event.protocol ?? '',
    event.upstreamPath ?? '',
    event.multiKeyIndex ?? '',
  ].join('|')
}

export function getChannelHealthRoutingEvents(
  other: LogOtherData | null | undefined
): ChannelHealthRoutingEvent[] {
  const info = getChannelHealthInfo(other)
  if (!info) return []

  const events: ChannelHealthRoutingEvent[] = []
  const seen = new Set<string>()
  const rawEvents = Array.isArray(info.events) ? info.events : []

  for (const rawEvent of rawEvents) {
    const action = readTrimmedString(rawEvent?.action)
    if (!action) continue

    const channelId = readNonNegativeInteger(rawEvent?.channel_id)
    if (channelId === undefined && action !== 'redis_unavailable') continue

    const routeKey = readTrimmedString(rawEvent?.route_key)
    const effectiveModel = readTrimmedString(rawEvent?.effective_model)
    const protocol = readTrimmedString(rawEvent?.protocol)
    const upstreamPath = readTrimmedString(rawEvent?.upstream_path)
    const multiKeyIndex = readNonNegativeInteger(rawEvent?.multi_key_index)
    const event: ChannelHealthRoutingEvent = {
      group: readTrimmedString(rawEvent?.group),
      modelName: readTrimmedString(rawEvent?.model_name),
      channelId: channelId ?? 0,
      action,
      statusCode: readPositiveInteger(rawEvent?.status_code),
      errorCode: readTrimmedString(rawEvent?.error_code),
      ...(routeKey ? { routeKey } : {}),
      ...(effectiveModel ? { effectiveModel } : {}),
      ...(protocol ? { protocol } : {}),
      ...(upstreamPath ? { upstreamPath } : {}),
      ...(multiKeyIndex !== undefined ? { multiKeyIndex } : {}),
    }
    const key = eventKey(event)
    if (seen.has(key)) continue
    seen.add(key)
    events.push(event)
  }

  if (
    info.redis_unavailable === true &&
    !events.some((event) => event.action === 'redis_unavailable')
  ) {
    events.push({
      group: undefined,
      modelName: undefined,
      channelId: 0,
      action: 'redis_unavailable',
      statusCode: undefined,
      errorCode: undefined,
    })
  }

  return events
}

function hasAction(
  events: readonly ChannelHealthRoutingEvent[],
  action: KnownChannelHealthRoutingAction
): boolean {
  return events.some((event) => event.action === action)
}

export function getChannelHealthRoutingResult(
  other: LogOtherData | null | undefined
): ChannelHealthRoutingResult | null {
  const events = getChannelHealthRoutingEvents(other)
  if (events.length === 0) return null

  const actionCount = events.length
  if (hasAction(events, 'redis_unavailable')) {
    return { kind: 'gate_unavailable', actionCount }
  }
  if (hasAction(events, 'half_open_recovery_superseded')) {
    return { kind: 'probe_recovery_superseded', actionCount }
  }
  if (hasAction(events, 'cooldown')) {
    return { kind: 'channel_cooling', actionCount }
  }
  if (hasAction(events, 'half_open_busy')) {
    return { kind: 'probe_busy', actionCount }
  }
  if (hasAction(events, 'half_open_recovered')) {
    return { kind: 'probe_recovered', actionCount }
  }
  if (hasAction(events, 'half_open_probe')) {
    return { kind: 'probe_active', actionCount }
  }
  if (hasAction(events, 'half_open_released')) {
    return { kind: 'probe_released', actionCount }
  }
  if (hasAction(events, 'affinity_bypassed')) {
    return { kind: 'affinity_bypassed', actionCount }
  }
  if (hasAction(events, 'healthy')) {
    return { kind: 'healthy', actionCount }
  }
  return { kind: 'health_action', actionCount }
}

export function getChannelHealthRoutingResultDisplay(
  result: ChannelHealthRoutingResult,
  t: ChannelHealthRoutingTranslator
): ChannelHealthRoutingResultDisplay {
  switch (result.kind) {
    case 'healthy':
      return { label: t('Health Gate Passed'), tone: 'blue' }
    case 'gate_unavailable':
      return { label: t('Health Gate Unavailable'), tone: 'danger' }
    case 'probe_recovery_superseded':
      return { label: t('Probe Recovery Superseded'), tone: 'danger' }
    case 'channel_cooling':
      return { label: t('Channel Cooling'), tone: 'warning' }
    case 'probe_busy':
      return { label: t('Half-open Probe Busy'), tone: 'warning' }
    case 'probe_recovered':
      return { label: t('Half-open Recovered'), tone: 'success' }
    case 'probe_active':
      return { label: t('Half-open Probe'), tone: 'info' }
    case 'probe_released':
      return { label: t('Half-open Probe Released'), tone: 'neutral' }
    case 'affinity_bypassed':
      return { label: t('Affinity Bypassed'), tone: 'blue' }
    case 'health_action':
      return { label: t('Health Gate Active'), tone: 'neutral' }
  }
}

export function getChannelHealthRoutingResultImpactKey(
  result: ChannelHealthRoutingResult
): string {
  switch (result.kind) {
    case 'gate_unavailable':
      return 'Health filtering was unavailable, so routing continued without temporary health exclusions.'
    case 'probe_recovery_superseded':
      return 'A newer qualifying failure kept the temporary exclusion, so the prior recovery probe did not restore the channel.'
    case 'channel_cooling':
      return 'A qualifying failure started or renewed cooldown for matching requests.'
    case 'probe_busy':
      return 'This request bypassed a channel while another matching request owned its recovery probe.'
    case 'probe_recovered':
      return 'A successful recovery probe cleared the temporary exclusion for matching requests.'
    case 'probe_active':
      return 'This request owns a recovery probe while competing matching requests bypass the channel.'
    case 'probe_released':
      return 'The recovery probe was released without changing the prior failure history.'
    case 'affinity_bypassed':
      return 'Channel affinity was bypassed because the selected channel had an active health exclusion.'
    case 'healthy':
      return 'No active temporary health exclusion affected the selected channel at this decision point.'
    case 'health_action':
      return 'A health-routing action was recorded for this request.'
  }
}

export function getChannelHealthRoutingEventImpactKey(
  event: ChannelHealthRoutingEvent
): string {
  switch (event.action) {
    case 'cooldown':
      return 'A qualifying failure started or renewed cooldown for matching requests.'
    case 'half_open_probe':
      return 'This request owns the recovery probe. Competing matching requests bypass the channel until it finishes.'
    case 'half_open_busy':
      return 'This request bypassed the channel because another request owns its recovery probe.'
    case 'half_open_recovered':
      return 'The recovery probe succeeded and cleared the temporary exclusion for matching requests.'
    case 'half_open_recovery_superseded':
      return 'A newer qualifying failure kept the temporary exclusion, so this probe did not restore the channel.'
    case 'half_open_released':
      return 'This request released the recovery probe without changing the prior failure history.'
    case 'affinity_bypassed':
      return 'Channel affinity was bypassed because the selected channel had an active health exclusion.'
    case 'healthy':
      return 'No active temporary health exclusion affected this channel at selection time.'
    case 'redis_unavailable':
      return 'Health filtering was unavailable, so routing continued without temporary health exclusions.'
    default:
      return 'This health-routing action has no defined request impact.'
  }
}

function formatEventContext(
  event: ChannelHealthRoutingEvent,
  t: ChannelHealthRoutingTranslator
): string {
  if (event.action === 'redis_unavailable') {
    return t('Redis unavailable; routing continued')
  }

  if (event.action === 'healthy') {
    const details = [
      t('Channel #{{channel}}', { channel: event.channelId }),
      t('No active cooldown or probe state'),
    ]
    if (event.group && event.modelName) {
      details.push(
        t('Routing scope: {{group}} / {{model}}', {
          group: event.group,
          model: event.modelName,
        })
      )
    }
    return details.join('; ')
  }

  const details: string[] = []
  if (event.channelId > 0) {
    details.push(t('Channel #{{channel}}', { channel: event.channelId }))
  }
  if (event.group && event.modelName) {
    details.push(
      t('Routing scope: {{group}} / {{model}}', {
        group: event.group,
        model: event.modelName,
      })
    )
  }
  if (event.statusCode !== undefined) {
    details.push(t('Status: {{status}}', { status: event.statusCode }))
  }
  if (event.errorCode) {
    details.push(t('Error: {{error}}', { error: event.errorCode }))
  }
  const route = [
    event.effectiveModel ? `model=${event.effectiveModel}` : '',
    event.protocol ? `protocol=${event.protocol}` : '',
    event.upstreamPath ? `path=${event.upstreamPath}` : '',
    Number.isInteger(event.multiKeyIndex) ? `key=${event.multiKeyIndex}` : '',
    event.routeKey ? `route=${event.routeKey.slice(0, 12)}` : '',
  ].filter(Boolean)
  if (route.length > 0) details.push(route.join(' | '))
  return details.length > 0 ? details.join('; ') : t('No additional details')
}

export function getChannelHealthRoutingEventDisplay(
  event: ChannelHealthRoutingEvent,
  t: ChannelHealthRoutingTranslator
): ChannelHealthRoutingEventDisplay {
  const value = formatEventContext(event, t)
  switch (event.action) {
    case 'healthy':
      return { key: event.action, label: t('Health Gate Passed'), value }
    case 'cooldown':
      return { key: event.action, label: t('Channel Cooling'), value }
    case 'half_open_probe':
      return { key: event.action, label: t('Half-open Probe'), value }
    case 'half_open_busy':
      return { key: event.action, label: t('Half-open Probe Busy'), value }
    case 'half_open_recovered':
      return { key: event.action, label: t('Half-open Recovered'), value }
    case 'half_open_recovery_superseded':
      return {
        key: event.action,
        label: t('Probe Recovery Superseded'),
        value,
      }
    case 'half_open_released':
      return {
        key: event.action,
        label: t('Half-open Probe Released'),
        value,
      }
    case 'affinity_bypassed':
      return { key: event.action, label: t('Affinity Bypassed'), value }
    case 'redis_unavailable':
      return {
        key: event.action,
        label: t('Health Gate Unavailable'),
        value,
      }
    default:
      return {
        key: event.action,
        label: t('Unknown health action: {{action}}', {
          action: event.action,
        }),
        value,
      }
  }
}

export function getChannelHealthRoutingEventTone(
  event: ChannelHealthRoutingEvent
): ChannelHealthRoutingTone {
  switch (event.action) {
    case 'healthy':
      return 'blue'
    case 'redis_unavailable':
    case 'half_open_recovery_superseded':
      return 'danger'
    case 'cooldown':
    case 'half_open_busy':
      return 'warning'
    case 'half_open_recovered':
      return 'success'
    case 'half_open_probe':
      return 'info'
    case 'affinity_bypassed':
      return 'blue'
    default:
      return 'neutral'
  }
}

export function shouldIncludeChannelHealthRoutingColumn(
  isAdmin: boolean
): boolean {
  return isAdmin === true
}
