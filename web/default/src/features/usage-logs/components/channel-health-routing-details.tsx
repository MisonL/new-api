import { HeartPulse } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Label } from '@/components/ui/label'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import {
  getChannelHealthRoutingEventDisplay,
  getChannelHealthRoutingEventImpactKey,
  getChannelHealthRoutingEventTone,
  getChannelHealthRoutingEvents,
  getChannelHealthRoutingResult,
  getChannelHealthRoutingResultDisplay,
  getChannelHealthRoutingResultImpactKey,
  type ChannelHealthRoutingTone,
} from '../lib/channel-health-routing'
import type { LogOtherData } from '../types'

const eventBorderClasses: Record<ChannelHealthRoutingTone, string> = {
  danger: 'border-l-rose-500',
  warning: 'border-l-amber-500',
  info: 'border-l-cyan-500',
  success: 'border-l-emerald-500',
  blue: 'border-l-blue-500',
  neutral: 'border-l-slate-400',
}

function getStatusVariant(
  tone: ChannelHealthRoutingTone
): StatusBadgeProps['variant'] {
  return tone
}

export function ChannelHealthRoutingEvents(props: {
  other: LogOtherData
  compact?: boolean
}) {
  const { t } = useTranslation()
  const events = getChannelHealthRoutingEvents(props.other)
  if (events.length === 0) return null

  return (
    <section
      className={cn(
        'border-border/70 min-w-0 space-y-2.5 border-t pt-3',
        props.compact && 'space-y-2'
      )}
    >
      <div className='flex min-w-0 flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5'>
        <h4 className='text-foreground text-xs font-semibold'>
          {t('Channel Health Events')}
        </h4>
        <span className='text-muted-foreground text-[11px] leading-4'>
          {t('Recorded during this request')}
        </span>
      </div>
      <div className='min-w-0 space-y-2'>
        {events.map((event, index) => {
          const display = getChannelHealthRoutingEventDisplay(event, t)
          const tone = getChannelHealthRoutingEventTone(event)
          return (
            <div
              key={[
                event.channelId,
                event.action,
                event.statusCode ?? '',
                event.errorCode ?? '',
                event.routeKey ?? '',
                event.effectiveModel ?? '',
                event.protocol ?? '',
                event.upstreamPath ?? '',
                event.multiKeyIndex ?? '',
                index,
              ].join(':')}
              className={cn(
                'bg-muted/20 min-w-0 space-y-1 border-l-2 py-1.5 pr-2 pl-2.5',
                eventBorderClasses[tone]
              )}
            >
              <StatusBadge
                label={display.label}
                variant={getStatusVariant(tone)}
                size='sm'
                copyable={false}
              />
              <p className='text-foreground/85 min-w-0 text-xs leading-5 break-words'>
                {display.value}
              </p>
              <div className='min-w-0 space-y-0.5 text-xs leading-5'>
                <span className='text-muted-foreground block text-[11px] leading-4'>
                  {t('Routing impact')}
                </span>
                <p className='text-foreground/85 break-words'>
                  {t(getChannelHealthRoutingEventImpactKey(event))}
                </p>
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}

export function ChannelHealthRoutingDetails(props: { other: LogOtherData }) {
  const { t } = useTranslation()
  const result = getChannelHealthRoutingResult(props.other)
  if (!result) return null

  const display = getChannelHealthRoutingResultDisplay(result, t)
  return (
    <section className='min-w-0 space-y-1.5'>
      <Label className='flex items-center gap-1.5 text-xs font-semibold'>
        <HeartPulse className='size-3.5' aria-hidden='true' />
        {t('Channel Health Routing')}
      </Label>
      <div className='bg-muted/30 min-w-0 space-y-3 overflow-hidden rounded-md border p-2.5 max-sm:p-2'>
        <div className='grid min-w-0 gap-x-4 gap-y-2 sm:grid-cols-2'>
          <div className='min-w-0 space-y-0.5'>
            <span className='text-muted-foreground block text-[11px] leading-4'>
              {t('Health routing decision')}
            </span>
            <StatusBadge
              label={display.label}
              variant={getStatusVariant(display.tone)}
              size='sm'
              copyable={false}
            />
          </div>
          <div className='min-w-0 space-y-0.5'>
            <span className='text-muted-foreground block text-[11px] leading-4'>
              {t('Recorded Actions')}
            </span>
            <span className='block font-mono text-xs leading-5 tabular-nums'>
              {result.actionCount}
            </span>
          </div>
        </div>
        <div className='min-w-0 space-y-0.5'>
          <span className='text-muted-foreground block text-[11px] leading-4'>
            {t('Routing impact')}
          </span>
          <p className='text-foreground/85 text-xs leading-5 break-words'>
            {t(getChannelHealthRoutingResultImpactKey(result))}
          </p>
        </div>
        {result.kind === 'healthy' && (
          <p className='text-muted-foreground text-[11px] leading-4'>
            {t(
              'Health gate decisions do not indicate whether the upstream request succeeded.'
            )}
          </p>
        )}
        <ChannelHealthRoutingEvents other={props.other} />
      </div>
    </section>
  )
}
