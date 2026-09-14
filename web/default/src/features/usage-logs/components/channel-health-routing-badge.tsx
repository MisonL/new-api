import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  getChannelHealthRoutingResult,
  getChannelHealthRoutingResultDisplay,
  getChannelHealthRoutingResultImpactKey,
  type ChannelHealthRoutingTone,
  shouldIncludeChannelHealthRoutingColumn,
} from '../lib/channel-health-routing'
import type { LogOtherData } from '../types'
import { ChannelHealthRoutingEvents } from './channel-health-routing-details'

interface ChannelHealthRoutingBadgeProps {
  other: LogOtherData | null
  isAdmin: boolean
}

const toneClasses: Record<
  ChannelHealthRoutingTone,
  { badge: string; value: string }
> = {
  danger: {
    badge:
      'border-rose-200/55 bg-rose-50/45 text-rose-700/90 dark:border-rose-900/45 dark:bg-rose-950/20 dark:text-rose-300/90',
    value: 'font-semibold text-rose-700 dark:text-rose-300',
  },
  warning: {
    badge:
      'border-amber-200/55 bg-amber-50/45 text-amber-700/90 dark:border-amber-900/45 dark:bg-amber-950/20 dark:text-amber-300/90',
    value: 'font-semibold text-amber-700 dark:text-amber-300',
  },
  info: {
    badge:
      'border-cyan-200/55 bg-cyan-50/45 text-cyan-700/90 dark:border-cyan-900/45 dark:bg-cyan-950/20 dark:text-cyan-300/90',
    value: 'font-semibold text-cyan-700 dark:text-cyan-300',
  },
  success: {
    badge:
      'border-emerald-200/55 bg-emerald-50/45 text-emerald-700/90 dark:border-emerald-900/45 dark:bg-emerald-950/20 dark:text-emerald-300/90',
    value: 'font-semibold text-emerald-700 dark:text-emerald-300',
  },
  blue: {
    badge:
      'border-blue-200/55 bg-blue-50/45 text-blue-700/90 dark:border-blue-900/45 dark:bg-blue-950/20 dark:text-blue-300/90',
    value: 'font-semibold text-blue-700 dark:text-blue-300',
  },
  neutral: {
    badge:
      'border-slate-200/65 bg-slate-50/60 text-slate-600 dark:border-slate-700/70 dark:bg-slate-900/30 dark:text-slate-300',
    value: 'font-semibold text-slate-700 dark:text-slate-300',
  },
}

export function ChannelHealthRoutingBadge({
  other,
  isAdmin,
}: ChannelHealthRoutingBadgeProps) {
  const { t } = useTranslation()
  const result = getChannelHealthRoutingResult(other)
  if (!shouldIncludeChannelHealthRoutingColumn(isAdmin) || !other || !result) {
    return null
  }

  const display = getChannelHealthRoutingResultDisplay(result, t)
  const tone = toneClasses[display.tone]
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            tabIndex={0}
            className={cn(
              'focus-visible:ring-ring inline-flex max-w-[11rem] items-center gap-1 rounded-md border px-1.5 py-0.5 text-xs font-medium focus-visible:ring-1 focus-visible:outline-none',
              tone.badge
            )}
          >
            <span className='truncate'>{display.label}</span>
            <span
              className='shrink-0 border-l border-current/30 pl-1 font-mono tabular-nums'
              title={t('Recorded actions: {{count}}', {
                count: result.actionCount,
              })}
            >
              {result.actionCount}
            </span>
          </span>
        </TooltipTrigger>
        <TooltipContent
          side='top'
          sideOffset={4}
          className='border-border bg-popover text-popover-foreground z-[100] w-[min(24rem,calc(100vw-2rem))] p-0 shadow-lg'
          arrowClassName='bg-popover fill-popover'
        >
          <div className='max-h-[min(24rem,calc(100vh-6rem))] overflow-auto p-3 text-xs leading-5'>
            <div className='flex min-w-0 flex-col gap-3'>
              <div className='flex min-w-0 items-center justify-between gap-2'>
                <span
                  className={cn(
                    'inline-flex rounded-md border px-1.5 py-0.5 font-semibold',
                    tone.badge
                  )}
                >
                  {t('Channel Health')}
                </span>
                <span className='text-muted-foreground font-mono text-[11px] tabular-nums'>
                  {t('Recorded Actions')}: {result.actionCount}
                </span>
              </div>
              <div className='grid min-w-0 grid-cols-2 gap-x-3 gap-y-2'>
                <div className='flex min-w-0 flex-col gap-0.5'>
                  <span className='text-muted-foreground text-[11px] leading-4 whitespace-nowrap'>
                    {t('Health routing decision')}
                  </span>
                  <span className={tone.value}>{display.label}</span>
                </div>
                <div className='flex min-w-0 flex-col gap-0.5'>
                  <span className='text-muted-foreground text-[11px] leading-4 whitespace-nowrap'>
                    {t('Recorded Actions')}
                  </span>
                  <span className='font-mono tabular-nums'>
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
              <ChannelHealthRoutingEvents other={other} compact />
            </div>
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
