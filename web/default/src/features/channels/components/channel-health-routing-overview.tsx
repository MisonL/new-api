import { Link } from '@tanstack/react-router'
import { Activity, ExternalLink, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { StatusBadge } from '@/components/status-badge'
import { ChannelHealthRuntimeEntries } from '@/features/channel-health/components/channel-health-runtime-entries'
import { ChannelHealthRuntimeEvents } from '@/features/channel-health/components/channel-health-runtime-events'
import {
  getChannelHealthRuntimeEvents,
  getChannelHealthRuntimePresentation,
  summarizeChannelHealthRuntime,
} from '@/features/system-settings/general/channel-health-runtime'
import { useChannels } from './channels-provider'

export function ChannelHealthRoutingOverview() {
  const { t } = useTranslation()
  const {
    canViewChannelHealthRuntime,
    canConfigureChannelHealthRuntime,
    channelHealthRuntime,
  } = useChannels()
  const {
    runtime,
    runtimeUnavailable,
    runtimeUpdatedAt,
    runtimeLoading,
    refresh,
  } = channelHealthRuntime

  if (!canViewChannelHealthRuntime) return null

  const presentation = getChannelHealthRuntimePresentation(
    runtime,
    runtimeUnavailable
  )
  const summary = runtime ? summarizeChannelHealthRuntime(runtime) : null
  const activeEntries = runtime?.entries ?? []
  const recentEvents = getChannelHealthRuntimeEvents(runtime)
  const handleRefresh = async () => {
    try {
      await refresh()
    } catch {
      toast.error(t('Failed to load channel health runtime'))
    }
  }

  return (
    <section className='space-y-3 border-y py-3 sm:py-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='flex min-w-0 items-center gap-2'>
          <Activity
            className='text-muted-foreground size-4 shrink-0'
            aria-hidden='true'
          />
          <div className='min-w-0'>
            <h2 className='text-sm font-semibold'>
              {t('Channel health routing')}
            </h2>
            <p className='text-muted-foreground text-xs leading-5'>
              {t('Runtime scope is channel, group, and model.')}
            </p>
          </div>
        </div>
        <StatusBadge
          label={t(presentation.labelKey)}
          variant={presentation.variant}
          copyable={false}
        />
      </div>

      {runtimeUnavailable ? (
        <p className='text-destructive text-sm'>
          {t(
            'The current health-routing state cannot be confirmed. No stale runtime data is shown.'
          )}
        </p>
      ) : null}

      {runtime && summary ? (
        <div className='grid divide-y overflow-hidden rounded-md border sm:grid-cols-3 sm:divide-x sm:divide-y-0'>
          <div className='p-3'>
            <p className='text-xs text-amber-700 dark:text-amber-400'>
              {t('Cooling channels')}
            </p>
            <p className='mt-1 font-mono text-lg text-amber-700 tabular-nums dark:text-amber-400'>
              {summary.cooling}
            </p>
          </div>
          <div className='p-3'>
            <p className='text-xs text-sky-700 dark:text-sky-400'>
              {t('Active probes')}
            </p>
            <p className='mt-1 font-mono text-lg text-sky-700 tabular-nums dark:text-sky-400'>
              {summary.probing}
            </p>
          </div>
          <div className='p-3'>
            <p className='text-xs text-teal-700 dark:text-teal-400'>
              {t('Awaiting probe')}
            </p>
            <p className='mt-1 font-mono text-lg text-teal-700 tabular-nums dark:text-teal-400'>
              {summary.awaitingProbe}
            </p>
          </div>
        </div>
      ) : null}

      {runtime ? (
        <div className='grid gap-2 rounded-md border p-3 text-xs sm:grid-cols-4'>
          <div>
            <span className='text-muted-foreground'>
              {t('Concurrency acquires')}
            </span>
            <div className='font-mono tabular-nums'>
              {runtime.concurrency?.acquire_success ?? 0}
            </div>
          </div>
          <div>
            <span className='text-muted-foreground'>
              {t('Saturated events')}
            </span>
            <div className='font-mono tabular-nums'>
              {runtime.concurrency?.saturated_events ?? 0}
            </div>
          </div>
          <div>
            <span className='text-muted-foreground'>{t('Strict 429')}</span>
            <div className='font-mono tabular-nums'>
              {runtime.concurrency?.strict_429 ?? 0}
            </div>
          </div>
          <div>
            <span className='text-muted-foreground'>{t('Local leases')}</span>
            <div className='font-mono tabular-nums'>
              {runtime.concurrency?.current_leases_local ?? 0}
            </div>
          </div>
        </div>
      ) : null}

      {runtime?.enabled &&
      runtime.redis_available &&
      activeEntries.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No active channel exclusions.')}
        </p>
      ) : null}

      {activeEntries.length > 0 ? (
        <ChannelHealthRuntimeEntries entries={activeEntries} />
      ) : null}

      <ChannelHealthRuntimeEvents events={recentEvents} />

      <div className='flex flex-wrap items-center gap-2'>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void handleRefresh()}
          disabled={runtimeLoading}
        >
          <RefreshCw
            className={`mr-1 size-3.5 ${runtimeLoading ? 'animate-spin' : ''}`}
          />
          {t('Refresh runtime state')}
        </Button>
        {canConfigureChannelHealthRuntime ? (
          <Button variant='outline' size='sm' asChild>
            <Link
              to='/system-settings/general/$section'
              params={{ section: 'channel-health' }}
            >
              {t('Configure health routing')}
            </Link>
          </Button>
        ) : null}
        <Button variant='outline' size='sm' asChild>
          <Link to='/usage-logs/$section' params={{ section: 'common' }}>
            <ExternalLink className='mr-1 size-3.5' />
            {t('View request routing logs')}
          </Link>
        </Button>
      </div>

      {runtime && runtimeUpdatedAt ? (
        <p className='text-muted-foreground text-xs'>
          {t('Last refreshed: {{time}}', {
            time: runtimeUpdatedAt.toLocaleTimeString(),
          })}
        </p>
      ) : null}
    </section>
  )
}
