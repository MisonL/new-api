import { useMemo } from 'react'
import { VChart } from '@visactor/react-vchart'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { VCHART_OPTION } from '@/lib/vchart'
import { useTheme } from '@/context/theme-provider'
import type {
  AvailabilityPoint,
  LatencyTimePoint,
} from './model-details-metrics'

function formatHourLabel(iso: string): string {
  const date = new Date(iso)
  const hours = date.getHours()
  return `${String(hours).padStart(2, '0')}:00`
}

function formatDayLabel(date: string): string {
  const parsed = new Date(date)
  if (date.includes('T')) {
    return parsed.toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
    })
  }
  return parsed.toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
  })
}

export function LatencyTrendChart(props: {
  series: LatencyTimePoint[]
  className?: string
}) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()

  const spec = useMemo(() => {
    if (props.series.length === 0) return null
    const data = props.series.map((point) => ({
      time: formatHourLabel(point.timestamp),
      group: point.group,
      ttft: point.ttft_ms,
    }))
    return {
      type: 'line' as const,
      data: [{ id: 'latency', values: data }],
      xField: 'time',
      yField: 'ttft',
      seriesField: 'group',
      smooth: true,
      point: { visible: false },
      legends: { visible: true, orient: 'top', position: 'start' },
      tooltip: {
        mark: {
          title: { value: (d: { time: string }) => d.time },
          content: [
            {
              key: (d: { group: string }) => d.group,
              value: (d: { ttft: number }) => `${Math.round(d.ttft)} ms`,
            },
          ],
        },
      },
      axes: [
        {
          orient: 'bottom',
          label: {
            style: { fill: 'currentColor', fontSize: 10 },
          },
          tick: { visible: false },
        },
        {
          orient: 'left',
          label: {
            formatMethod: (val: number | string) => `${val} ms`,
            style: { fill: 'currentColor', fontSize: 10 },
          },
          grid: { visible: true, style: { lineDash: [3, 3] } },
        },
      ],
    }
  }, [props.series])

  if (props.series.length === 0) {
    return (
      <div
        className={cn(
          'text-muted-foreground flex h-48 items-center justify-center rounded-lg border text-xs',
          props.className
        )}
      >
        {t('No latency data available')}
      </div>
    )
  }

  return (
    <div className={cn('h-64 sm:h-72', props.className)}>
      {spec && (
        <VChart
          key={`latency-${resolvedTheme}`}
          spec={{
            ...spec,
            theme: resolvedTheme === 'dark' ? 'dark' : 'light',
            background: 'transparent',
          }}
          option={VCHART_OPTION}
        />
      )}
    </div>
  )
}

export function AvailabilityBarChart(props: {
  series: AvailabilityPoint[]
  className?: string
}) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()

  const spec = useMemo(() => {
    if (props.series.length === 0) return null

    const data = props.series.map((point) => ({
      date: formatDayLabel(point.date),
      uptime: point.success_rate,
      incidents: point.incidents,
    }))

    return {
      type: 'bar' as const,
      data: [{ id: 'uptime', values: data }],
      xField: 'date',
      yField: 'uptime',
      bar: {
        style: {
          fill: (datum: { uptime: number }) => {
            if (datum.uptime >= 99.9) return '#10b981'
            if (datum.uptime >= 99.0) return '#f59e0b'
            return '#ef4444'
          },
          cornerRadius: 2,
        },
      },
      tooltip: {
        mark: {
          title: { value: (d: { date: string }) => d.date },
          content: [
            {
              key: t('Success rate'),
              value: (d: { uptime: number }) => `${d.uptime.toFixed(2)}%`,
            },
            {
              key: t('Incidents'),
              value: (d: { incidents: number }) => `${d.incidents}`,
            },
          ],
        },
      },
      axes: [
        {
          orient: 'bottom',
          label: {
            style: { fill: 'currentColor', fontSize: 10 },
            autoLimit: true,
          },
          tick: { visible: false },
        },
        {
          orient: 'left',
          min: 95,
          max: 100,
          label: {
            formatMethod: (val: number | string) => `${val}%`,
            style: { fill: 'currentColor', fontSize: 10 },
          },
          grid: { visible: true, style: { lineDash: [3, 3] } },
        },
      ],
    }
  }, [props.series, t])

  if (props.series.length === 0) {
    return (
      <div
        className={cn(
          'text-muted-foreground flex h-48 items-center justify-center rounded-lg border text-xs',
          props.className
        )}
      >
        {t('No availability data available')}
      </div>
    )
  }

  return (
    <div className={cn('h-56 sm:h-64', props.className)}>
      {spec && (
        <VChart
          key={`uptime-${resolvedTheme}`}
          spec={{
            ...spec,
            theme: resolvedTheme === 'dark' ? 'dark' : 'light',
            background: 'transparent',
          }}
          option={VCHART_OPTION}
        />
      )}
    </div>
  )
}
