import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  getResponsesRoutingFinalPath,
  getResponsesRoutingResult,
  getResponsesRoutingResultDisplay,
  type ResponsesRoutingResultTone,
} from '../lib/responses-routing'
import { shouldIncludeResponsesRoutingColumn } from '../lib/responses-routing-trace'
import type { LogOtherData } from '../types'
import { ResponsesRoutingTrace } from './responses-routing-trace'

interface ResponsesRoutingBadgeProps {
  other: LogOtherData | null
  isAdmin: boolean
}

const toneClasses: Record<
  ResponsesRoutingResultTone,
  { badge: string; value: string }
> = {
  cyan: {
    badge:
      'border-cyan-200/55 bg-cyan-50/45 text-cyan-700/90 dark:border-cyan-900/45 dark:bg-cyan-950/20 dark:text-cyan-300/90',
    value: 'font-semibold text-cyan-700 dark:text-cyan-300',
  },
  orange: {
    badge:
      'border-orange-200/55 bg-orange-50/45 text-orange-700/90 dark:border-orange-900/45 dark:bg-orange-950/20 dark:text-orange-300/90',
    value: 'font-semibold text-orange-700 dark:text-orange-300',
  },
  red: {
    badge:
      'border-red-200/55 bg-red-50/45 text-red-700/90 dark:border-red-900/45 dark:bg-red-950/20 dark:text-red-300/90',
    value: 'font-semibold text-red-700 dark:text-red-300',
  },
  indigo: {
    badge:
      'border-indigo-200/55 bg-indigo-50/45 text-indigo-700/90 dark:border-indigo-900/45 dark:bg-indigo-950/20 dark:text-indigo-300/90',
    value: 'font-semibold text-indigo-700 dark:text-indigo-300',
  },
  violet: {
    badge:
      'border-violet-200/55 bg-violet-50/45 text-violet-700/90 dark:border-violet-900/45 dark:bg-violet-950/20 dark:text-violet-300/90',
    value: 'font-semibold text-violet-700 dark:text-violet-300',
  },
  green: {
    badge:
      'border-green-200/55 bg-green-50/45 text-green-700/90 dark:border-green-900/45 dark:bg-green-950/20 dark:text-green-300/90',
    value: 'font-semibold text-green-700 dark:text-green-300',
  },
  neutral: {
    badge:
      'border-slate-200/65 bg-slate-50/60 text-slate-600 dark:border-slate-700/70 dark:bg-slate-900/30 dark:text-slate-300',
    value: 'font-semibold text-slate-700 dark:text-slate-300',
  },
}

export function ResponsesRoutingBadge({
  other,
  isAdmin,
}: ResponsesRoutingBadgeProps) {
  const { t } = useTranslation()
  const result = getResponsesRoutingResult(other)
  if (!shouldIncludeResponsesRoutingColumn(isAdmin) || !other || !result) {
    return null
  }

  const resultDisplay = getResponsesRoutingResultDisplay(result, t)
  const finalPath = getResponsesRoutingFinalPath(other)
  const tone = toneClasses[resultDisplay.tone]

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
            <span className='truncate'>{resultDisplay.label}</span>
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
                  {t('Responses Routing')}
                </span>
                <span className='text-muted-foreground font-mono text-[11px] tabular-nums'>
                  {t('Recorded Actions')}: {result.actionCount}
                </span>
              </div>
              <div className='grid min-w-0 grid-cols-2 gap-x-3 gap-y-2'>
                <div className='flex min-w-0 flex-col gap-0.5'>
                  <span className='text-muted-foreground text-[11px] leading-4 whitespace-nowrap'>
                    {t('Final Result')}
                  </span>
                  <span className={tone.value}>{resultDisplay.label}</span>
                </div>
                <div className='flex min-w-0 flex-col gap-0.5'>
                  <span className='text-muted-foreground text-[11px] leading-4 whitespace-nowrap'>
                    {t('Recorded Actions')}
                  </span>
                  <span className='font-mono tabular-nums'>
                    {result.actionCount}
                  </span>
                </div>
                {finalPath && (
                  <div className='col-span-2 flex min-w-0 flex-col gap-0.5'>
                    <span className='text-muted-foreground text-[11px] leading-4'>
                      {t('Final Path')}
                    </span>
                    <code className='text-foreground/85 min-w-0 font-mono text-xs break-all'>
                      {finalPath}
                    </code>
                  </div>
                )}
              </div>
              <ResponsesRoutingTrace other={other} compact />
            </div>
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
