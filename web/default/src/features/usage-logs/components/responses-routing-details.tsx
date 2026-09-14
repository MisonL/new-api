import { Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Label } from '@/components/ui/label'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import {
  getResponsesRoutingFinalPath,
  getResponsesRoutingResult,
  getResponsesRoutingResultDisplay,
  hasResponsesRoutingDetails,
  type ResponsesRoutingResultTone,
} from '../lib/responses-routing'
import type {
  ChannelCapabilitySnapshot,
  LogOtherData,
  ResponsesRoutingUserSummary,
} from '../types'
import { ResponsesRoutingTrace } from './responses-routing-trace'

type CapabilityState = 'supported' | 'unknown' | 'unsupported'

function getResultVariant(
  tone: ResponsesRoutingResultTone
): StatusBadgeProps['variant'] {
  return tone
}

function getCompactModeLabel(
  mode: string | undefined,
  t: (key: string) => string
): string {
  switch (mode) {
    case 'native':
      return t('Compact Native')
    case 'synthetic_summary':
      return t('Compact Synthetic')
    default:
      return mode || t('Unknown')
  }
}

function getBooleanCapabilityState(
  value: boolean | undefined
): CapabilityState {
  if (value === true) return 'supported'
  if (value === false) return 'unsupported'
  return 'unknown'
}

function getReasoningContextCapabilityState(
  capability: ChannelCapabilitySnapshot
): CapabilityState {
  if (capability.reasoning_context_capability === 'supported')
    return 'supported'
  if (capability.reasoning_context_capability === 'unsupported')
    return 'unsupported'
  if (capability.reasoning_context_capability === 'unknown') return 'unknown'
  return getBooleanCapabilityState(capability.supports_reasoning_context)
}

function formatCapabilityState(
  state: CapabilityState,
  t: (key: string) => string
): string {
  switch (state) {
    case 'supported':
      return t('Supported')
    case 'unsupported':
      return t('Unsupported')
    case 'unknown':
      return t('Unknown')
  }
}

function formatBooleanState(
  value: boolean | undefined,
  t: (key: string) => string
): string {
  if (value === true) return t('Yes')
  if (value === false) return t('No')
  return t('Unknown')
}

function getCapabilityStateClass(state: CapabilityState): string {
  switch (state) {
    case 'supported':
      return 'text-emerald-700 dark:text-emerald-300'
    case 'unsupported':
      return 'text-red-700 dark:text-red-300'
    case 'unknown':
      return 'text-muted-foreground'
  }
}

function RoutingDetailItem(props: {
  label: string
  value: React.ReactNode
  className?: string
  mono?: boolean
  wide?: boolean
}) {
  return (
    <div className={cn('min-w-0 space-y-0.5', props.wide && 'sm:col-span-2')}>
      <span className='text-muted-foreground block text-[11px] leading-4'>
        {props.label}
      </span>
      <span
        className={cn(
          'block min-w-0 text-xs leading-5 break-words',
          props.mono && 'font-mono',
          props.className
        )}
      >
        {props.value}
      </span>
    </div>
  )
}

function CapabilityEvidence(props: {
  capability: ChannelCapabilitySnapshot | undefined
  observedReason: string | undefined
  probeReason: string | undefined
}) {
  const { t } = useTranslation()
  const { capability } = props
  const capabilityEntries: Array<[string, CapabilityState]> = capability
    ? [
        [
          t('Responses'),
          getBooleanCapabilityState(capability.supports_responses),
        ],
        [
          t('Responses Compact'),
          getBooleanCapabilityState(capability.supports_responses_compact),
        ],
        [
          t('REST Previous ID'),
          getBooleanCapabilityState(
            capability.supports_rest_previous_response_id
          ),
        ],
        [
          t('Compaction Item Passthrough'),
          getBooleanCapabilityState(
            capability.supports_compaction_item_passthrough
          ),
        ],
        [
          t('Custom Tools'),
          getBooleanCapabilityState(capability.supports_custom_tools),
        ],
        [
          t('Namespace Tools'),
          getBooleanCapabilityState(capability.supports_namespace_tools),
        ],
        [
          t('Tool Search'),
          getBooleanCapabilityState(capability.supports_tool_search),
        ],
        [
          t('Stream Usage Option'),
          getBooleanCapabilityState(
            capability.supports_stream_options_include_usage
          ),
        ],
        [
          t('Reasoning Context'),
          getReasoningContextCapabilityState(capability),
        ],
      ]
    : []

  return (
    <section className='border-border/70 min-w-0 space-y-2.5 border-t pt-3'>
      <div className='flex min-w-0 flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5'>
        <h4 className='text-foreground text-xs font-semibold'>
          {t('Capability Evidence')}
        </h4>
        {!capability && (
          <span className='text-muted-foreground text-[11px] leading-4'>
            {t('No capability snapshot recorded')}
          </span>
        )}
      </div>

      {capability && (
        <div className='min-w-0 space-y-3'>
          {(capability.source ||
            capability.profile ||
            capability.compact_mode_effective) && (
            <div className='grid min-w-0 gap-x-4 gap-y-2 sm:grid-cols-2'>
              {capability.source && (
                <RoutingDetailItem
                  label={t('Capability Source')}
                  value={capability.source}
                  mono
                />
              )}
              {capability.profile && (
                <RoutingDetailItem
                  label={t('Upstream Profile')}
                  value={capability.profile}
                  mono
                />
              )}
              {capability.compact_mode_effective && (
                <RoutingDetailItem
                  label={t('Effective Compact Mode')}
                  value={capability.compact_mode_effective}
                  mono
                />
              )}
            </div>
          )}

          <div className='grid min-w-0 gap-x-4 gap-y-2 sm:grid-cols-2'>
            {capabilityEntries.map(([label, state]) => (
              <RoutingDetailItem
                key={label}
                label={label}
                value={formatCapabilityState(state, t)}
                className={getCapabilityStateClass(state)}
              />
            ))}
            <RoutingDetailItem
              label={t('Strips Reasoning Context')}
              value={formatBooleanState(
                capability.strips_responses_reasoning_context,
                t
              )}
            />
            <RoutingDetailItem
              label={t('Strips Encrypted Reasoning')}
              value={formatBooleanState(
                capability.strips_responses_encrypted_reasoning,
                t
              )}
            />
          </div>

          {(props.observedReason || props.probeReason) && (
            <div className='grid min-w-0 gap-x-4 gap-y-2 border-t border-dashed pt-2 sm:grid-cols-2'>
              {props.observedReason && (
                <RoutingDetailItem
                  label={t('Last Observed Failure')}
                  value={props.observedReason}
                  mono
                  wide={!props.probeReason}
                />
              )}
              {props.probeReason && (
                <RoutingDetailItem
                  label={t('Last Probe Result')}
                  value={props.probeReason}
                  mono
                  wide={!props.observedReason}
                />
              )}
            </div>
          )}
        </div>
      )}
    </section>
  )
}

function formatUserRoutingOutcome(
  outcome: string | undefined,
  t: (key: string) => string
): string {
  switch (outcome) {
    case 'success':
      return t('Success')
    case 'rate_limited':
      return t('Rate Limited')
    case 'authentication_failed':
      return t('Authentication')
    case 'channel_unavailable':
      return t('Error')
    case 'error':
      return t('Error')
    default:
      return t('Unknown')
  }
}

export function ResponsesRoutingUserSummaryDetails(props: {
  summary: ResponsesRoutingUserSummary
}) {
  const { t } = useTranslation()
  const { summary } = props
  const reasons = Array.isArray(summary.reasons)
    ? summary.reasons.filter((reason) => String(reason).trim())
    : []

  return (
    <section className='min-w-0 space-y-1.5'>
      <Label className='flex items-center gap-1.5 text-xs font-semibold'>
        <Route className='size-3.5' aria-hidden='true' />
        {t('Responses Routing')}
      </Label>
      <div className='bg-muted/30 min-w-0 space-y-1.5 overflow-hidden rounded-md border p-2.5 max-sm:p-2'>
        <RoutingDetailItem
          label={t('Final Result')}
          value={formatUserRoutingOutcome(summary.outcome, t)}
        />
        <RoutingDetailItem
          label={t('Route Decision')}
          value={summary.automatic_reroute ? t('Yes') : t('No')}
        />
        <RoutingDetailItem
          label={t('Recorded Actions')}
          value={String(summary.attempt_count ?? 0)}
          mono
        />
        <RoutingDetailItem
          label={t('Channel Skip')}
          value={String(summary.preflight_skip_count ?? 0)}
          mono
        />
        {reasons.length > 0 && (
          <RoutingDetailItem
            label={t('Reason')}
            value={reasons.join(', ')}
            mono
            wide
          />
        )}
      </div>
    </section>
  )
}

export function ResponsesRoutingDetails(props: { other: LogOtherData }) {
  const { t } = useTranslation()
  const { other } = props
  const routingResult = getResponsesRoutingResult(other)
  if (!hasResponsesRoutingDetails(other) || !routingResult) return null

  const resultDisplay = getResponsesRoutingResultDisplay(routingResult, t)
  const finalPath = getResponsesRoutingFinalPath(other)
  const capability = other.channel_capability_snapshot
  const observedReason =
    other.admin_info?.responses_channel_capability_observed?.reason
  const probeReason =
    other.admin_info?.responses_channel_capability_probe?.reason

  return (
    <section className='min-w-0 space-y-1.5'>
      <Label className='flex items-center gap-1.5 text-xs font-semibold'>
        <Route className='size-3.5' aria-hidden='true' />
        {t('Responses Routing')}
      </Label>
      <div className='bg-muted/30 min-w-0 space-y-3 overflow-hidden rounded-md border p-2.5 max-sm:p-2'>
        <div className='grid min-w-0 gap-x-4 gap-y-2 sm:grid-cols-2'>
          <RoutingDetailItem
            label={t('Final Result')}
            value={
              <StatusBadge
                label={resultDisplay.label}
                variant={getResultVariant(resultDisplay.tone)}
                size='sm'
                copyable={false}
              />
            }
          />
          <RoutingDetailItem
            label={t('Recorded Actions')}
            value={String(routingResult.actionCount)}
            mono
          />
          <RoutingDetailItem
            label={t('Final Path')}
            value={finalPath || t('Not recorded')}
            className={!finalPath ? 'text-muted-foreground' : undefined}
            mono={Boolean(finalPath)}
            wide
          />
          {other.responses_compact_mode && (
            <RoutingDetailItem
              label={t('Mode')}
              value={getCompactModeLabel(other.responses_compact_mode, t)}
            />
          )}
          {other.responses_compact_setting && (
            <RoutingDetailItem
              label={t('Configuration')}
              value={other.responses_compact_setting}
              mono
            />
          )}
        </div>

        <ResponsesRoutingTrace
          other={other}
          className='border-border/70 border-t pt-3'
        />

        <CapabilityEvidence
          capability={capability}
          observedReason={observedReason}
          probeReason={probeReason}
        />
      </div>
    </section>
  )
}
