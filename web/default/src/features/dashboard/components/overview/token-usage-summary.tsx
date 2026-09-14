import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Database, Download, Upload, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { formatNumber } from '@/lib/format'
import { computeTimeRange } from '@/lib/time'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { getDashboardLogStats } from '../../api'

interface SummaryItem {
  label: string
  value: string
  icon: LucideIcon
}

export function TokenUsageSummary() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = Boolean(user?.role && user.role >= 10)
  const [timeRange, setTimeRange] = useState(() => computeTimeRange(1))
  const {
    data: stats,
    isPending: loading,
    isError,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ['dashboard-token-stats', user?.id, isAdmin, timeRange],
    queryFn: ({ signal }) => getDashboardLogStats(timeRange, isAdmin, signal),
    enabled: Boolean(user?.id),
    staleTime: 60_000,
  })

  const items: SummaryItem[] = [
    {
      label: t('Input Tokens'),
      value: formatNumber(stats?.input_tokens ?? 0),
      icon: Upload,
    },
    {
      label: t('Cache Read'),
      value: formatNumber(stats?.cache_read_tokens ?? 0),
      icon: Database,
    },
    {
      label: t('Cache Write'),
      value: formatNumber(stats?.cache_write_tokens ?? 0),
      icon: Database,
    },
    {
      label: t('Output Tokens'),
      value: formatNumber(stats?.output_tokens ?? 0),
      icon: Download,
    },
    {
      label: t('Hit Rate'),
      value:
        stats?.cache_rate == null
          ? 'N/A'
          : `${(stats.cache_rate * 100).toFixed(2)}%`,
      icon: Database,
    },
  ]

  return (
    <div className='overflow-hidden rounded-lg border' aria-busy={isFetching}>
      <div className='flex flex-wrap items-center justify-between gap-2 border-b px-3 py-3 sm:px-5'>
        <h2 className='text-sm font-medium'>{t('Statistical tokens')}</h2>
        <span className='text-muted-foreground text-xs'>
          {new Date(timeRange.start_timestamp * 1000).toLocaleString()} -{' '}
          {new Date(timeRange.end_timestamp * 1000).toLocaleString()}
        </span>
        <Button
          variant='outline'
          size='sm'
          disabled={isFetching}
          onClick={() => {
            const next = computeTimeRange(1)
            if (next.end_timestamp === timeRange.end_timestamp) void refetch()
            else setTimeRange(next)
          }}
        >
          {t('Refresh Stats')}
        </Button>
      </div>
      {isError && (
        <p role='alert' className='text-destructive px-3 py-2 text-sm sm:px-5'>
          {t('Request failed')}
        </p>
      )}
      <div className='divide-border/60 grid grid-cols-2 divide-x sm:grid-cols-3 lg:grid-cols-5'>
        {items.map((item) => (
          <div key={item.label} className='px-3 py-3 sm:px-5 sm:py-4'>
            <div className='text-muted-foreground flex items-center gap-2 text-xs font-medium tracking-wider uppercase'>
              <item.icon
                aria-hidden='true'
                className='text-muted-foreground/60 size-3.5 shrink-0'
              />
              <span className='min-w-0 break-words'>{item.label}</span>
            </div>
            {loading ? (
              <Skeleton className='mt-2 h-7 w-24' />
            ) : (
              <div
                aria-live='polite'
                className='text-foreground mt-1.5 font-mono text-lg font-bold break-all tabular-nums sm:text-2xl'
              >
                {isError ? '--' : item.value}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
