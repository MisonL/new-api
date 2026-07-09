import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  AlertTriangle,
  HeartPulse,
  Timer,
  TrendingUp,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { GroupBadge } from '@/components/group-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { getPerfMetrics, type PerformanceGroup } from '../api'
import type { PricingModel } from '../types'
import { AvailabilityBarChart, LatencyTrendChart } from './model-details-charts'
import {
  formatLatency,
  formatPercent,
  type AvailabilityPoint,
} from './model-details-metrics'
import { ModelDetailsStatCard } from './model-details-stat-card'

const COMPACT_NUMBER = new Intl.NumberFormat(undefined, {
  notation: 'compact',
  maximumFractionDigits: 1,
})

type PerformanceRow = {
  group: string
  avg_ttft_ms: number
  avg_latency_ms: number
  avg_upstream_header_ms: number
  avg_upstream_ttfb_ms: number
  avg_upstream_total_ms: number
  upstream_header_count: number
  upstream_ttfb_count: number
  upstream_total_count: number
  success_rate: number
  request_count: number
}

function toLatencySeries(groups: PerformanceGroup[]) {
  return groups.flatMap((group) =>
    group.series
      .filter((point) => point.ttft_count > 0 && point.avg_ttft_ms > 0)
      .map((point) => ({
        timestamp: new Date(point.ts * 1000).toISOString(),
        group: group.group,
        ttft_ms: point.avg_ttft_ms,
      }))
  )
}

function toAvailabilitySeries(groups: PerformanceGroup[]): AvailabilityPoint[] {
  const byTs = new Map<number, { count: number; success: number }>()
  for (const group of groups) {
    for (const point of group.series) {
      const current = byTs.get(point.ts) ?? { count: 0, success: 0 }
      current.count += point.count
      current.success += point.success_count
      byTs.set(point.ts, current)
    }
  }
  return Array.from(byTs.entries())
    .sort(([a], [b]) => a - b)
    .map(([ts, value]) => ({
      date: new Date(ts * 1000).toISOString(),
      success_rate:
        value.count > 0
          ? Math.round((value.success / value.count) * 10000) / 100
          : 0,
      incidents: value.success < value.count ? 1 : 0,
    }))
}

function weightedLatency(rows: PerformanceRow[]): number {
  let total = 0
  let count = 0
  for (const row of rows) {
    if (row.avg_latency_ms <= 0 || row.request_count <= 0) continue
    total += row.avg_latency_ms * row.request_count
    count += row.request_count
  }
  return count > 0 ? Math.round(total / count) : 0
}

function bestPositiveLatency(values: number[]): number | null {
  const positiveValues = values.filter((value) => value > 0)
  return positiveValues.length > 0 ? Math.min(...positiveValues) : null
}

function PerformanceDetailsSkeleton() {
  return (
    <div className='flex flex-col gap-4'>
      <div className='grid grid-cols-2 gap-2 lg:grid-cols-4'>
        {Array.from({ length: 4 }).map((_, index) => (
          <Skeleton key={index} className='h-24 rounded-lg' />
        ))}
      </div>
      <Skeleton className='h-48 rounded-lg' />
      <Skeleton className='h-56 rounded-lg' />
      <Skeleton className='h-56 rounded-lg' />
    </div>
  )
}

function UpstreamTimingCell(props: { row: PerformanceRow }) {
  const { t } = useTranslation()
  const items = [
    {
      label: t('Upstream header'),
      value: props.row.avg_upstream_header_ms,
      count: props.row.upstream_header_count,
    },
    {
      label: t('Upstream first byte'),
      value: props.row.avg_upstream_ttfb_ms,
      count: props.row.upstream_ttfb_count,
    },
    {
      label: t('Upstream total'),
      value: props.row.avg_upstream_total_ms,
      count: props.row.upstream_total_count,
    },
  ].filter((item) => item.count > 0)

  if (items.length === 0) {
    return <span className='text-muted-foreground/50'>-</span>
  }

  return (
    <div className='flex flex-col items-end gap-0.5'>
      {items.map((item) => (
        <span
          key={item.label}
          className='text-muted-foreground inline-flex gap-1 font-mono text-xs tabular-nums'
        >
          <span className='text-muted-foreground/60 font-sans'>
            {item.label}
          </span>
          {formatLatency(item.value)}
        </span>
      ))}
    </div>
  )
}

function SectionHeader(props: {
  icon: React.ComponentType<{ className?: string }>
  title: string
  description?: string
  accent?: React.ReactNode
}) {
  const Icon = props.icon
  return (
    <div className='mb-2 flex flex-wrap items-center justify-between gap-2'>
      <div className='flex min-w-0 items-center gap-2'>
        <Icon className='text-muted-foreground/70 size-3.5 shrink-0' />
        <div className='min-w-0'>
          <div className='text-foreground text-sm font-semibold'>
            {props.title}
          </div>
          {props.description && (
            <p className='text-muted-foreground/80 text-xs'>
              {props.description}
            </p>
          )}
        </div>
      </div>
      {props.accent && (
        <div className='shrink-0 text-xs font-medium'>{props.accent}</div>
      )}
    </div>
  )
}

export function ModelDetailsPerformance(props: { model: PricingModel }) {
  const { t } = useTranslation()
  const metricsQuery = useQuery({
    queryKey: ['perf-metrics', props.model.model_name],
    queryFn: () => getPerfMetrics(props.model.model_name, 24),
    staleTime: 60 * 1000,
  })
  const groups = useMemo(
    () => metricsQuery.data?.data.groups ?? [],
    [metricsQuery.data?.data.groups]
  )
  const rows = useMemo<PerformanceRow[]>(
    () =>
      groups.map((group) => ({
        group: group.group,
        avg_ttft_ms: group.avg_ttft_ms,
        avg_latency_ms: group.avg_latency_ms,
        avg_upstream_header_ms: group.avg_upstream_header_ms,
        avg_upstream_ttfb_ms: group.avg_upstream_ttfb_ms,
        avg_upstream_total_ms: group.avg_upstream_total_ms,
        upstream_header_count: group.upstream_header_count,
        upstream_ttfb_count: group.upstream_ttfb_count,
        upstream_total_count: group.upstream_total_count,
        success_rate: group.success_rate,
        request_count: group.request_count,
      })),
    [groups]
  )
  const latencySeries = useMemo(() => toLatencySeries(groups), [groups])
  const availabilitySeries = useMemo(
    () => toAvailabilitySeries(groups),
    [groups]
  )

  if (metricsQuery.isLoading) {
    return <PerformanceDetailsSkeleton />
  }

  if (metricsQuery.isError) {
    return (
      <div className='text-muted-foreground flex flex-col items-center gap-3 rounded-lg border p-6 text-center text-sm'>
        <span>{t('Failed to load')}</span>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => void metricsQuery.refetch()}
        >
          {t('Retry')}
        </Button>
      </div>
    )
  }

  if (rows.length === 0) {
    return (
      <div className='text-muted-foreground rounded-lg border p-6 text-center text-sm'>
        {t('Performance data is not yet available for this model.')}
      </div>
    )
  }

  const bestTtft = bestPositiveLatency(rows.map((row) => row.avg_ttft_ms))
  const totalRequests = rows.reduce((sum, row) => sum + row.request_count, 0)
  const totalSuccess = groups.reduce(
    (sum, group) => sum + group.success_count,
    0
  )
  const successRate =
    totalRequests > 0 ? (totalSuccess / totalRequests) * 100 : 0
  const incidentCount = availabilitySeries.reduce(
    (sum, point) => sum + point.incidents,
    0
  )
  const hasUpstreamTiming = rows.some(
    (row) =>
      row.upstream_header_count > 0 ||
      row.upstream_ttfb_count > 0 ||
      row.upstream_total_count > 0
  )
  const intent =
    successRate >= 99.9 ? 'success' : successRate >= 99 ? 'default' : 'warning'
  const headerCellClass =
    'text-muted-foreground py-2 text-[10px] font-medium tracking-wider uppercase'

  return (
    <div className='flex flex-col gap-4'>
      <div className='grid grid-cols-2 gap-2 lg:grid-cols-4'>
        <ModelDetailsStatCard
          icon={Timer}
          label={t('Best TTFT')}
          value={bestTtft == null ? '-' : formatLatency(bestTtft)}
          hint={t('Lowest average first-token latency')}
        />
        <ModelDetailsStatCard
          icon={Timer}
          label={t('Average latency')}
          value={formatLatency(weightedLatency(rows))}
          hint={t('Across all groups')}
        />
        <ModelDetailsStatCard
          icon={HeartPulse}
          label={t('Success rate')}
          value={formatPercent(successRate)}
          hint={
            incidentCount > 0
              ? t('{{count}} incidents in the last 24 hours', {
                  count: incidentCount,
                })
              : t('No incidents in the last 24 hours')
          }
          intent={intent}
        />
        <ModelDetailsStatCard
          icon={TrendingUp}
          label={t('Requests (24h)')}
          value={COMPACT_NUMBER.format(totalRequests)}
          hint={t('Aggregated across enabled groups')}
        />
      </div>

      <section>
        <SectionHeader
          icon={Activity}
          title={t('Per-group performance')}
          description={t(
            'Average latency, TTFT, upstream timing, and success rate by group'
          )}
        />
        <div className='overflow-x-auto rounded-lg border'>
          <Table className='text-sm'>
            <TableHeader>
              <TableRow className='hover:bg-transparent'>
                <TableHead className={headerCellClass}>{t('Group')}</TableHead>
                <TableHead className={`${headerCellClass} text-right`}>
                  {t('Average TTFT')}
                </TableHead>
                <TableHead className={`${headerCellClass} text-right`}>
                  {t('Average latency')}
                </TableHead>
                {hasUpstreamTiming && (
                  <TableHead className={`${headerCellClass} text-right`}>
                    {t('Upstream timing')}
                  </TableHead>
                )}
                <TableHead className={`${headerCellClass} text-right`}>
                  {t('Success rate')}
                </TableHead>
                <TableHead className={`${headerCellClass} text-right`}>
                  {t('Request Count')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.group}>
                  <TableCell className='py-2.5'>
                    <GroupBadge group={row.group} size='sm' />
                  </TableCell>
                  <TableCell className='py-2.5 text-right font-mono'>
                    {formatLatency(row.avg_ttft_ms)}
                  </TableCell>
                  <TableCell className='text-muted-foreground py-2.5 text-right font-mono'>
                    {formatLatency(row.avg_latency_ms)}
                  </TableCell>
                  {hasUpstreamTiming && (
                    <TableCell className='py-2.5 text-right'>
                      <UpstreamTimingCell row={row} />
                    </TableCell>
                  )}
                  <TableCell className='text-muted-foreground py-2.5 text-right font-mono'>
                    {formatPercent(row.success_rate)}
                  </TableCell>
                  <TableCell className='text-muted-foreground py-2.5 text-right font-mono'>
                    {COMPACT_NUMBER.format(row.request_count)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </section>

      <section>
        <SectionHeader
          icon={Timer}
          title={t('Latency trend (last 24h)')}
          description={t('Average time-to-first-token (TTFT) by group')}
        />
        <LatencyTrendChart series={latencySeries} />
      </section>

      <section>
        <SectionHeader
          icon={HeartPulse}
          title={t('Availability (last 24h)')}
          description={t('Request success rate sampled over the last 24 hours')}
          accent={
            incidentCount > 0 ? (
              <span className='inline-flex items-center gap-1 text-amber-600 dark:text-amber-400'>
                <AlertTriangle className='size-3.5' />
                {t('{{count}} incidents', { count: incidentCount })}
              </span>
            ) : null
          }
        />
        <AvailabilityBarChart series={availabilitySeries} />
      </section>
    </div>
  )
}
