import { useTranslation } from 'react-i18next'
import { formatTimestampToDate } from '@/lib/format'
import { StatusBadge } from '@/components/status-badge'
import {
  getChannelHealthRuntimeEventDisplay,
  type ChannelHealthRuntimeEvent,
} from '@/features/system-settings/general/channel-health-runtime'

function formatEventDetails(
  event: ChannelHealthRuntimeEvent,
  t: (key: string, options?: Record<string, unknown>) => string
): string {
  const details = [
    t('Channel #{{channel}}', { channel: event.channel_id }),
    event.group,
    event.model_name,
  ]
  if (event.status_code) {
    details.push(t('Status: {{status}}', { status: event.status_code }))
  }
  if (event.error_code) {
    details.push(t('Error: {{error}}', { error: event.error_code }))
  }
  const route = [
    event.effective_model ? `model=${event.effective_model}` : '',
    event.protocol ? `protocol=${event.protocol}` : '',
    event.upstream_path ? `path=${event.upstream_path}` : '',
    Number.isInteger(event.multi_key_index)
      ? `key=${event.multi_key_index}`
      : '',
    event.route_key ? `route=${event.route_key.slice(0, 12)}` : '',
  ].filter(Boolean)
  if (route.length > 0) details.push(route.join(' | '))
  return details.join(' | ')
}

export function ChannelHealthRuntimeEvents(props: {
  events: ChannelHealthRuntimeEvent[]
}) {
  const { t } = useTranslation()
  if (props.events.length === 0) return null

  return (
    <section className='border-border/70 min-w-0 space-y-2.5 border-t pt-3'>
      <div className='flex min-w-0 flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5'>
        <h3 className='text-foreground text-xs font-semibold'>
          {t('Channel Health Events')}
        </h3>
        <span className='text-muted-foreground text-[11px] leading-4'>
          {t('Timeline')}
        </span>
      </div>
      <ol className='min-w-0 space-y-2'>
        {props.events.map((event, index) => {
          const display = getChannelHealthRuntimeEventDisplay(event)
          return (
            <li
              key={[
                event.occurred_at,
                event.channel_id,
                event.action,
                event.status_code ?? '',
                event.error_code ?? '',
                event.route_key ?? '',
                event.effective_model ?? '',
                event.protocol ?? '',
                event.upstream_path ?? '',
                event.multi_key_index ?? '',
                index,
              ].join(':')}
              className='border-border/70 min-w-0 border-l-2 py-1.5 pl-3'
            >
              <div className='flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-1'>
                <StatusBadge
                  label={t(display.labelKey)}
                  variant={display.variant}
                  size='sm'
                  copyable={false}
                />
                <time className='text-muted-foreground font-mono text-[11px] tabular-nums'>
                  {formatTimestampToDate(event.occurred_at)}
                </time>
              </div>
              <p className='text-muted-foreground mt-1 min-w-0 font-mono text-[11px] leading-5 break-words'>
                {formatEventDetails(event, t)}
              </p>
            </li>
          )
        })}
      </ol>
    </section>
  )
}
