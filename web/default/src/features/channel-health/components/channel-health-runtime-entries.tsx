import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import {
  getChannelHealthRuntimeChannelSearch,
  getChannelHealthRuntimeEntryKey,
  getChannelHealthRuntimeRouteSummary,
  getChannelHealthRuntimeStateBehavior,
  getChannelHealthRuntimeStateDisplay,
  type ChannelHealthRuntimeEntry,
} from '@/features/system-settings/general/channel-health-runtime'

function formatRemainingSeconds(seconds: number | undefined): string {
  if (!seconds || seconds < 1) return '-'
  return `${seconds}s`
}

function formatLastQualifyingFailure(entry: ChannelHealthRuntimeEntry): string {
  if (!entry.status_code && !entry.error_code) return '-'
  return [entry.status_code, entry.error_code].filter(Boolean).join(' / ')
}

function ChannelHealthRuntimeChannelLink(props: {
  entry: ChannelHealthRuntimeEntry
}) {
  const { t } = useTranslation()
  const search = getChannelHealthRuntimeChannelSearch(props.entry)
  const label = `#${props.entry.channel_id}`

  if (!search) {
    return <span>{label}</span>
  }

  return (
    <Link
      to='/channels'
      search={(current) => ({ ...current, ...search })}
      className='text-primary hover:text-primary/80 underline decoration-current/40 decoration-1 underline-offset-2 transition-colors hover:decoration-current'
      aria-label={t('Channel #{{channel}}', {
        channel: props.entry.channel_id,
      })}
    >
      {label}
    </Link>
  )
}

function RuntimeEntryField(props: {
  label: string
  value: string
  className?: string
  monospace?: boolean
}) {
  return (
    <div className={props.className}>
      <dt className='text-muted-foreground text-[11px] leading-4'>
        {props.label}
      </dt>
      <dd
        className={`mt-0.5 min-w-0 text-xs leading-5 break-words ${
          props.monospace ? 'font-mono' : ''
        }`}
      >
        {props.value}
      </dd>
    </div>
  )
}

function ChannelHealthRuntimeMobileEntry(props: {
  entry: ChannelHealthRuntimeEntry
}) {
  const { t } = useTranslation()
  const { entry } = props
  const display = getChannelHealthRuntimeStateDisplay(entry.state)
  const behavior = getChannelHealthRuntimeStateBehavior(entry.state)
  const remaining =
    entry.state === 'cooldown'
      ? formatRemainingSeconds(entry.cooldown_remaining_seconds)
      : formatRemainingSeconds(entry.probe_lease_remaining_seconds)

  return (
    <article className='bg-muted/20 min-w-0 space-y-3 rounded-md border p-3'>
      <div className='flex min-w-0 items-start justify-between gap-3'>
        <div className='min-w-0'>
          <p className='text-muted-foreground text-[11px] leading-4'>
            {t('Channel')}
          </p>
          <p className='font-mono text-sm font-semibold tabular-nums'>
            <ChannelHealthRuntimeChannelLink entry={entry} />
          </p>
        </div>
        <StatusBadge
          label={t(display.labelKey)}
          variant={display.variant}
          size='sm'
          copyable={false}
        />
      </div>
      <dl className='grid min-w-0 grid-cols-2 gap-x-3 gap-y-2'>
        <RuntimeEntryField label={t('Group')} value={entry.group} />
        <RuntimeEntryField label={t('Remaining')} value={remaining} monospace />
        <RuntimeEntryField
          label={t('Model')}
          value={entry.model_name}
          className='col-span-2'
          monospace
        />
        <RuntimeEntryField
          label={t('Route')}
          value={getChannelHealthRuntimeRouteSummary(entry)}
          className='col-span-2'
          monospace
        />
        <RuntimeEntryField
          label={t('Last qualifying failure')}
          value={formatLastQualifyingFailure(entry)}
          className='col-span-2'
          monospace
        />
        <RuntimeEntryField
          label={t('Routing impact')}
          value={t(behavior.impactKey)}
          className='col-span-2'
        />
        <RuntimeEntryField
          label={t('Next transition')}
          value={t(behavior.nextStepKey)}
          className='col-span-2'
        />
      </dl>
    </article>
  )
}

export function ChannelHealthRuntimeEntries(props: {
  entries: ChannelHealthRuntimeEntry[]
}) {
  const { t } = useTranslation()

  return (
    <>
      <div className='space-y-2 md:hidden'>
        {props.entries.map((entry) => (
          <ChannelHealthRuntimeMobileEntry
            key={getChannelHealthRuntimeEntryKey(entry)}
            entry={entry}
          />
        ))}
      </div>
      <div className='hidden overflow-x-auto rounded-md border md:block'>
        <table className='w-full min-w-[680px] text-sm'>
          <thead className='bg-muted/40 text-muted-foreground text-left text-xs'>
            <tr>
              <th className='px-3 py-2 font-medium'>{t('Channel')}</th>
              <th className='px-3 py-2 font-medium'>{t('Group')}</th>
              <th className='px-3 py-2 font-medium'>{t('Model')}</th>
              <th className='px-3 py-2 font-medium'>{t('Route')}</th>
              <th className='px-3 py-2 font-medium'>{t('State')}</th>
              <th className='px-3 py-2 font-medium'>{t('Remaining')}</th>
              <th className='px-3 py-2 font-medium'>
                {t('Last qualifying failure')}
              </th>
              <th className='px-3 py-2 font-medium'>{t('Routing impact')}</th>
              <th className='px-3 py-2 font-medium'>{t('Next transition')}</th>
            </tr>
          </thead>
          <tbody>
            {props.entries.map((entry) => {
              const display = getChannelHealthRuntimeStateDisplay(entry.state)
              const behavior = getChannelHealthRuntimeStateBehavior(entry.state)
              const remaining =
                entry.state === 'cooldown'
                  ? formatRemainingSeconds(entry.cooldown_remaining_seconds)
                  : formatRemainingSeconds(entry.probe_lease_remaining_seconds)
              return (
                <tr
                  key={getChannelHealthRuntimeEntryKey(entry)}
                  className='border-t'
                >
                  <td className='px-3 py-2 font-mono tabular-nums'>
                    <ChannelHealthRuntimeChannelLink entry={entry} />
                  </td>
                  <td className='px-3 py-2'>{entry.group}</td>
                  <td className='px-3 py-2 font-mono text-xs'>
                    {entry.model_name}
                  </td>
                  <td className='px-3 py-2 font-mono text-xs'>
                    {getChannelHealthRuntimeRouteSummary(entry)}
                  </td>
                  <td className='px-3 py-2'>
                    <StatusBadge
                      label={t(display.labelKey)}
                      variant={display.variant}
                      copyable={false}
                    />
                  </td>
                  <td className='px-3 py-2 font-mono tabular-nums'>
                    {remaining}
                  </td>
                  <td className='px-3 py-2 font-mono text-xs'>
                    {formatLastQualifyingFailure(entry)}
                  </td>
                  <td className='px-3 py-2 text-xs leading-5'>
                    {t(behavior.impactKey)}
                  </td>
                  <td className='px-3 py-2 text-xs leading-5'>
                    {t(behavior.nextStepKey)}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </>
  )
}
