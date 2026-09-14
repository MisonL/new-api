import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  getResponsesRoutingEventDisplay,
  getResponsesRoutingChannelTrace,
  hasResponsesRoutingChannelTrace,
  getResponsesRoutingTrace,
} from '../lib/responses-routing'
import {
  getResponsesRoutingTraceStageDisplay,
  getResponsesRoutingTraceTone,
  type ResponsesRoutingTraceStage,
  type ResponsesRoutingTraceTone,
} from '../lib/responses-routing-trace'
import type { LogOtherData } from '../types'

function formatChannelLabel(
  channelId: number,
  t: (key: string, options?: Record<string, unknown>) => string
): string {
  return t('Channel #{{channel}}', { channel: channelId })
}

function formatChannelAttempt(
  attempt: {
    channel_id: number
    status_code?: number
    error_code?: string
    error_category?: string
    outcome?: string
  },
  t: (key: string, options?: Record<string, unknown>) => string
): string {
  const details: string[] = []
  if (attempt.outcome === 'success') {
    details.push(t('Success'))
  } else if (attempt.outcome === 'error') {
    details.push(t('Error'))
  } else {
    details.push(t('Unknown'))
  }
  if (attempt.status_code !== undefined) {
    details.push(t('Status: {{status}}', { status: attempt.status_code }))
  }
  const error = [attempt.error_code, attempt.error_category]
    .filter(Boolean)
    .join(' / ')
  if (error) details.push(t('Error: {{error}}', { error }))
  return `${formatChannelLabel(attempt.channel_id, t)}; ${details.join('; ')}`
}

const traceStages: readonly ResponsesRoutingTraceStage[] = [
  'request_preparation',
  'route_selection',
  'upstream_attempt',
  'recovery',
]

const toneClasses: Record<
  ResponsesRoutingTraceTone,
  { dot: string; label: string; value: string }
> = {
  adaptation: {
    dot: 'bg-blue-500 dark:bg-blue-400',
    label: 'text-blue-700 dark:text-blue-300',
    value: 'text-blue-800 dark:text-blue-200',
  },
  failure: {
    dot: 'bg-red-500 dark:bg-red-400',
    label: 'text-red-700 dark:text-red-300',
    value: 'text-red-800 dark:text-red-200',
  },
  neutral: {
    dot: 'bg-slate-400 dark:bg-slate-500',
    label: 'text-slate-700 dark:text-slate-300',
    value: 'text-foreground/85',
  },
  recovery: {
    dot: 'bg-amber-500 dark:bg-amber-400',
    label: 'text-amber-700 dark:text-amber-300',
    value: 'text-amber-800 dark:text-amber-200',
  },
}

function hasCodeLikeValue(code: string): boolean {
  return new Set([
    'channel_skip',
    'compact_channel_skip',
    'compact_route_decision',
    'compact_summary_model',
    'compact_summary_models',
    'previous_id_action',
    'reasoning_context_action',
  ]).has(code)
}

interface ResponsesRoutingTraceProps {
  other: LogOtherData
  compact?: boolean
  className?: string
}

export function ResponsesRoutingTrace({
  other,
  compact = false,
  className,
}: ResponsesRoutingTraceProps) {
  const { t } = useTranslation()
  const trace = getResponsesRoutingTrace(other)
  const channelTrace = getResponsesRoutingChannelTrace(other)
  const showChannelTrace = hasResponsesRoutingChannelTrace(channelTrace)
  const traceByStage = traceStages
    .map((stage) => ({
      stage,
      items: trace.filter((item) => item.stage === stage),
    }))
    .filter((group) => group.items.length > 0)

  return (
    <div className={cn('min-w-0 space-y-2.5', className)}>
      <div className='flex min-w-0 flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5'>
        <span className='text-foreground text-xs font-semibold'>
          {t('Execution Trace')}
        </span>
        <span className='text-muted-foreground text-[11px] leading-4'>
          {t('Grouped by processing stage, not timestamp order')}
        </span>
      </div>

      {traceByStage.length === 0 ? (
        <p className='text-muted-foreground text-xs leading-5'>
          {t('No recorded routing action')}
        </p>
      ) : (
        <div className={cn('space-y-3', compact && 'space-y-2.5')}>
          {traceByStage.map((group) => (
            <section key={group.stage} className='min-w-0'>
              <h4 className='text-muted-foreground mb-1 text-[11px] leading-4 font-semibold'>
                {getResponsesRoutingTraceStageDisplay(group.stage, t)}
              </h4>
              <div className='border-border/70 ml-1 space-y-2 border-l pl-3'>
                {group.items.map((item, index) => {
                  const display = getResponsesRoutingEventDisplay(item.event, t)
                  const tone =
                    toneClasses[getResponsesRoutingTraceTone(item.event)]
                  return (
                    <div
                      key={`${group.stage}:${display.key}:${index}`}
                      className='relative min-w-0'
                    >
                      <span
                        aria-hidden='true'
                        className={cn(
                          'ring-popover absolute top-1.5 -left-[1.08rem] size-2 rounded-full ring-2',
                          tone.dot
                        )}
                      />
                      <div className='flex min-w-0 flex-col gap-0.5'>
                        <span
                          className={cn(
                            'min-w-0 text-[11px] leading-4 font-medium',
                            tone.label
                          )}
                        >
                          {display.label}
                        </span>
                        <span
                          className={cn(
                            'min-w-0 text-xs leading-5 break-words',
                            tone.value,
                            hasCodeLikeValue(item.event.code) && 'font-mono'
                          )}
                        >
                          {display.value}
                        </span>
                      </div>
                    </div>
                  )
                })}
              </div>
            </section>
          ))}
        </div>
      )}

      {showChannelTrace && channelTrace && (
        <section className='border-border/70 min-w-0 space-y-2 border-t pt-3'>
          <h4 className='text-muted-foreground text-[11px] leading-4 font-semibold'>
            {t('Route Selection')}
          </h4>
          <div className='border-border/70 ml-1 space-y-2 border-l pl-3'>
            {channelTrace.candidates.length > 0 && (
              <div className='relative min-w-0'>
                <span className='text-muted-foreground block text-[11px] leading-4 font-medium'>
                  {t('Channel IDs')}
                </span>
                <span className='text-foreground/85 block font-mono text-xs leading-5 break-words'>
                  {channelTrace.candidates
                    .map((channelId) => formatChannelLabel(channelId, t))
                    .join(' -> ')}
                </span>
              </div>
            )}
            {channelTrace.preflightSkipped.map((skip) => (
              <div
                className='relative min-w-0'
                key={`skip:${skip.channel_id}:${skip.reason}`}
              >
                <span className='text-muted-foreground block text-[11px] leading-4 font-medium'>
                  {t('Channel Skip')}
                </span>
                <span className='text-foreground/85 block font-mono text-xs leading-5 break-words'>
                  {formatChannelLabel(skip.channel_id, t)}; {skip.reason}
                </span>
              </div>
            ))}
            {channelTrace.attemptedChannels.map((attempt, index) => (
              <div
                className='relative min-w-0'
                key={`attempt:${attempt.channel_id}:${index}`}
              >
                <span className='text-muted-foreground block text-[11px] leading-4 font-medium'>
                  {t('Upstream Attempt')}
                </span>
                <span className='text-foreground/85 block font-mono text-xs leading-5 break-words'>
                  {formatChannelAttempt(attempt, t)}
                </span>
              </div>
            ))}
            {channelTrace.finalChannel !== undefined && (
              <div className='relative min-w-0'>
                <span className='text-muted-foreground block text-[11px] leading-4 font-medium'>
                  {t('Channel')}
                </span>
                <span className='text-foreground/85 block font-mono text-xs leading-5 break-words'>
                  {formatChannelLabel(channelTrace.finalChannel, t)}
                </span>
              </div>
            )}
            {channelTrace.terminalReason && (
              <div className='relative min-w-0'>
                <span className='text-muted-foreground block text-[11px] leading-4 font-medium'>
                  {t('Reason')}
                </span>
                <span className='text-foreground/85 block font-mono text-xs leading-5 break-words'>
                  {channelTrace.terminalReason}
                </span>
              </div>
            )}
          </div>
        </section>
      )}
    </div>
  )
}
