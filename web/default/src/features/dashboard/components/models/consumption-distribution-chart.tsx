import { useEffect, useMemo, useRef, useState } from 'react'
import { VChart } from '@visactor/react-vchart'
import { AreaChart, BarChart3, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { TimeGranularity } from '@/lib/time'
import { VCHART_OPTION } from '@/lib/vchart'
import { useTheme } from '@/context/theme-provider'
import {
  CONSUMPTION_DISTRIBUTION_CHART_OPTIONS,
  DEFAULT_TIME_GRANULARITY,
} from '@/features/dashboard/constants'
import { processChartData } from '@/features/dashboard/lib'
import {
  buildDashboardDrilldown,
  createDashboardChartAreaClickGuard,
  getDashboardChartAreaDrilldownTarget,
  getDashboardDimensionDrilldownTarget,
  getDashboardDrilldownTarget,
  type DashboardDrilldownDetail,
} from '@/features/dashboard/lib/drilldown'
import type {
  ConsumptionDistributionChartType,
  QuotaDataItem,
} from '@/features/dashboard/types'
import { DashboardDrilldownDialog } from './dashboard-drilldown-dialog'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

interface ConsumptionDistributionChartProps {
  data: QuotaDataItem[]
  loading?: boolean
  timeGranularity?: TimeGranularity
  defaultChartType?: ConsumptionDistributionChartType
}

const CHART_TYPE_ICONS: Record<
  ConsumptionDistributionChartType,
  typeof BarChart3
> = {
  bar: BarChart3,
  area: AreaChart,
}

export function ConsumptionDistributionChart(
  props: ConsumptionDistributionChartProps
) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const [chartType, setChartType] = useState<ConsumptionDistributionChartType>(
    props.defaultChartType ?? 'bar'
  )
  const [themeReady, setThemeReady] = useState(false)
  const [drilldownDetail, setDrilldownDetail] =
    useState<DashboardDrilldownDetail | null>(null)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)
  const areaClickGuardRef = useRef(createDashboardChartAreaClickGuard())
  const timeGranularity = props.timeGranularity ?? DEFAULT_TIME_GRANULARITY

  useEffect(() => {
    if (props.defaultChartType) setChartType(props.defaultChartType)
  }, [props.defaultChartType])

  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)

      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (m) => m.ThemeManager
        )
      }

      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }

    updateTheme()
  }, [resolvedTheme])

  const chartData = useMemo(
    () =>
      processChartData(
        props.loading ? [] : props.data,
        timeGranularity,
        t,
        resolvedTheme
      ),
    [props.data, props.loading, resolvedTheme, timeGranularity, t]
  )
  const spec = chartType === 'bar' ? chartData.spec_line : chartData.spec_area
  const chartValues = spec?.data?.[0]?.values
  const openDrilldown = (
    target: ReturnType<typeof getDashboardDrilldownTarget>
  ) => {
    if (!target) return

    const detail = buildDashboardDrilldown({
      data: props.data,
      targetTime: target.time,
      granularity: timeGranularity,
      models: target.models,
      unknownLabel: t('Unknown'),
    })
    if (!detail || detail.rows.length === 0) return

    setDrilldownDetail(detail)
  }
  const handleChartClick = (event: {
    datum?: unknown
    item?: { getDatum?: () => unknown }
  }) => {
    const target = getDashboardDrilldownTarget({
      datum: event?.datum || event?.item?.getDatum?.(),
      otherLabel: t('Other'),
    })
    areaClickGuardRef.current.markChartClickHandled(target)
    openDrilldown(target)
  }
  const handleDimensionClick = (event: { dimensionInfo?: unknown }) => {
    const target = getDashboardDimensionDrilldownTarget({
      dimensionInfo: event?.dimensionInfo,
      otherLabel: t('Other'),
    })
    areaClickGuardRef.current.markChartClickHandled(target)
    openDrilldown(target)
  }
  const handleAreaClick = (event: React.MouseEvent<HTMLDivElement>) => {
    if (!areaClickGuardRef.current.shouldHandleAreaClick()) return
    openDrilldown(
      getDashboardChartAreaDrilldownTarget({
        clientX: event.clientX,
        rect: event.currentTarget.getBoundingClientRect(),
        chartValues,
      })
    )
  }

  return (
    <>
      <div className='overflow-hidden rounded-lg border'>
        <div className='flex w-full flex-col gap-1.5 border-b px-3 py-2 sm:gap-3 sm:px-5 sm:py-3 lg:flex-row lg:items-center lg:justify-between'>
          <div className='flex items-center gap-2'>
            <WalletCards className='text-muted-foreground/60 size-4' />
            <div className='text-sm font-semibold'>
              {t('Quota Distribution')}
            </div>
            <span className='text-muted-foreground text-xs'>
              {t('Total:')} {chartData.totalQuotaDisplay}
            </span>
          </div>

          <div className='bg-muted/60 inline-flex h-7 w-full overflow-x-auto rounded-md border p-0.5 sm:h-8 sm:w-auto'>
            {CONSUMPTION_DISTRIBUTION_CHART_OPTIONS.map((item) => {
              const Icon = CHART_TYPE_ICONS[item.value]
              return (
                <button
                  key={item.value}
                  type='button'
                  onClick={() => setChartType(item.value)}
                  className={`inline-flex shrink-0 items-center gap-1.5 rounded-[5px] px-3 text-xs font-medium transition-colors ${
                    chartType === item.value
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  <Icon className='size-3.5' />
                  {t(item.labelKey)}
                </button>
              )
            })}
          </div>
        </div>

        <div
          className='h-[300px] cursor-pointer p-1.5 sm:h-96 sm:p-2'
          onClick={handleAreaClick}
        >
          {themeReady && spec && (
            <VChart
              key={`${chartType}-${resolvedTheme}`}
              spec={{
                ...spec,
                theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                background: 'transparent',
              }}
              option={VCHART_OPTION}
              onClick={handleChartClick}
              onPointerTap={handleChartClick}
              onDimensionClick={handleDimensionClick}
            />
          )}
        </div>
      </div>
      <DashboardDrilldownDialog
        detail={drilldownDetail}
        open={Boolean(drilldownDetail)}
        onOpenChange={(open) => {
          if (!open) setDrilldownDetail(null)
        }}
      />
    </>
  )
}
