import { useTranslation } from 'react-i18next'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { StatusBadge } from '@/components/status-badge'
import {
  getChannelHealthRuntimeDominantEntry,
  getChannelHealthRuntimeEntryKey,
  getChannelHealthRuntimeChannelPresentation,
  getChannelHealthRuntimeEntriesForChannel,
  getChannelHealthRuntimeRouteSummary,
  getChannelHealthRuntimeStateBehavior,
  getChannelHealthRuntimeStateDisplay,
  type ChannelHealthRuntimeEntry,
  type ChannelHealthRuntimeStatus,
} from '@/features/system-settings/general/channel-health-runtime'

function formatRemainingSeconds(entry: ChannelHealthRuntimeEntry): string {
  const seconds =
    entry.state === 'cooldown'
      ? entry.cooldown_remaining_seconds
      : entry.probe_lease_remaining_seconds
  return seconds && seconds > 0 ? `${seconds}s` : '-'
}

function formatLastQualifyingFailure(entry: ChannelHealthRuntimeEntry): string {
  return (
    [entry.status_code, entry.error_code].filter(Boolean).join(' / ') || '-'
  )
}

export function ChannelHealthRuntimeChannelState(props: {
  channelId: number
  runtime: ChannelHealthRuntimeStatus | null
}) {
  const { t } = useTranslation()
  const entries = getChannelHealthRuntimeEntriesForChannel(
    props.runtime,
    props.channelId
  )
  const presentation = getChannelHealthRuntimeChannelPresentation(entries)

  if (!presentation) return null

  const dominantEntry = getChannelHealthRuntimeDominantEntry(entries)
  const behavior = dominantEntry
    ? getChannelHealthRuntimeStateBehavior(dominantEntry.state)
    : null

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type='button'
          className='focus-visible:ring-ring inline-flex min-h-7 items-center rounded-sm px-1.5 focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-hidden'
          aria-label={`${t('Channel health routing')}: ${t(
            presentation.labelKey
          )}`}
          onClick={(event) => event.stopPropagation()}
        >
          <StatusBadge
            label={t(presentation.labelKey)}
            variant={presentation.variant}
            copyable={false}
          />
        </button>
      </PopoverTrigger>
      <PopoverContent
        align='start'
        className='w-[min(24rem,calc(100vw-2rem))] space-y-3 p-3'
      >
        <div className='space-y-1'>
          <h3 className='text-sm font-semibold'>
            {t('Channel health routing')}
          </h3>
          <p className='text-muted-foreground text-xs leading-5'>
            {t('Runtime scope is channel, group, and model.')}
          </p>
          {behavior ? (
            <dl className='text-muted-foreground grid gap-1 text-xs leading-5'>
              <div>
                <dt className='font-medium'>{t('Routing impact')}</dt>
                <dd>{t(behavior.impactKey)}</dd>
              </div>
              <div>
                <dt className='font-medium'>{t('Next transition')}</dt>
                <dd>{t(behavior.nextStepKey)}</dd>
              </div>
            </dl>
          ) : null}
        </div>

        <div className='max-h-80 space-y-2 overflow-y-auto pr-1'>
          {entries.map((entry) => {
            const display = getChannelHealthRuntimeStateDisplay(entry.state)
            const entryBehavior = getChannelHealthRuntimeStateBehavior(
              entry.state
            )
            return (
              <div
                key={getChannelHealthRuntimeEntryKey(entry)}
                className='bg-muted/30 min-w-0 space-y-2 rounded-md border p-2.5'
              >
                <StatusBadge
                  label={t(display.labelKey)}
                  variant={display.variant}
                  copyable={false}
                />
                <dl className='grid min-w-0 grid-cols-2 gap-x-3 gap-y-1.5 text-xs'>
                  <div className='min-w-0'>
                    <dt className='text-muted-foreground'>{t('Group')}</dt>
                    <dd className='mt-0.5 break-words'>{entry.group}</dd>
                  </div>
                  <div className='min-w-0'>
                    <dt className='text-muted-foreground'>{t('Remaining')}</dt>
                    <dd className='mt-0.5 font-mono tabular-nums'>
                      {formatRemainingSeconds(entry)}
                    </dd>
                  </div>
                  <div className='col-span-2 min-w-0'>
                    <dt className='text-muted-foreground'>{t('Model')}</dt>
                    <dd className='mt-0.5 font-mono text-[11px] break-words'>
                      {entry.model_name}
                    </dd>
                  </div>
                  <div className='col-span-2 min-w-0'>
                    <dt className='text-muted-foreground'>{t('Route')}</dt>
                    <dd className='mt-0.5 font-mono text-[11px] break-words'>
                      {getChannelHealthRuntimeRouteSummary(entry)}
                    </dd>
                  </div>
                  <div className='col-span-2 min-w-0'>
                    <dt className='text-muted-foreground'>
                      {t('Routing impact')}
                    </dt>
                    <dd className='mt-0.5 text-[11px] leading-4'>
                      {t(entryBehavior.impactKey)}
                    </dd>
                  </div>
                  <div className='col-span-2 min-w-0'>
                    <dt className='text-muted-foreground'>
                      {t('Next transition')}
                    </dt>
                    <dd className='mt-0.5 text-[11px] leading-4'>
                      {t(entryBehavior.nextStepKey)}
                    </dd>
                  </div>
                  <div className='col-span-2 min-w-0'>
                    <dt className='text-muted-foreground'>
                      {t('Last qualifying failure')}
                    </dt>
                    <dd className='mt-0.5 font-mono text-[11px] break-words'>
                      {formatLastQualifyingFailure(entry)}
                    </dd>
                  </div>
                </dl>
              </div>
            )
          })}
        </div>
      </PopoverContent>
    </Popover>
  )
}
