type DashboardDatum = Record<string, unknown>
type TimeGranularity = 'hour' | 'day' | 'week'

interface QuotaDataItem {
  model_name?: string
  created_at: number
  token_used?: number
  count?: number
  quota?: number
}

export interface DashboardDrilldownTarget {
  time: string
  times?: string[]
  models: string[] | null
}

export interface DashboardDrilldownRow {
  model: string
  quota: number
  count: number
  tokens: number
  ratio: number
}

export interface DashboardDrilldownDetail {
  time: string
  rows: DashboardDrilldownRow[]
  totalQuota: number
  totalCount: number
  totalTokens: number
}

export function getDashboardDrilldownTarget(options: {
  datum: unknown
  otherLabel?: string
}): DashboardDrilldownTarget | null {
  const matchedDatum = findDashboardDrilldownDatum(
    options.datum,
    options.otherLabel
  )
  if (!matchedDatum) return null

  if (
    matchedDatum.Model === options.otherLabel &&
    Array.isArray(matchedDatum.CollapsedModels)
  ) {
    return {
      time: String(matchedDatum.Time),
      models: matchedDatum.CollapsedModels.filter(
        (model): model is string => typeof model === 'string' && model !== ''
      ),
    }
  }

  return {
    time: String(matchedDatum.Time),
    models: null,
  }
}

export function getDashboardDimensionDrilldownTarget(options: {
  dimensionInfo: unknown
  otherLabel?: string
}): DashboardDrilldownTarget | null {
  const dimensionInfo = Array.isArray(options.dimensionInfo)
    ? options.dimensionInfo
    : []
  const firstDimension = dimensionInfo[0] as
    | { data?: Array<{ datum?: unknown }>; value?: unknown }
    | undefined
  if (!firstDimension) return null

  const datumTarget = getDashboardDrilldownTarget({
    datum: Array.isArray(firstDimension.data)
      ? firstDimension.data.map((item) => item?.datum)
      : null,
    otherLabel: options.otherLabel,
  })
  if (datumTarget) return datumTarget

  if (firstDimension.value == null) return null
  return {
    time: String(firstDimension.value),
    models: null,
  }
}

export function getDashboardChartAreaDrilldownTarget(options: {
  clientX: number
  rect: Pick<DOMRect, 'left' | 'width'> | null
  chartValues: unknown
}): DashboardDrilldownTarget | null {
  const { clientX, rect } = options
  if (
    !rect ||
    !Number.isFinite(clientX) ||
    !Number.isFinite(rect.left) ||
    !Number.isFinite(rect.width) ||
    rect.width <= 0
  ) {
    return null
  }

  const times = getUniqueChartTimes(options.chartValues)
  if (times.length === 0) return null

  const ratio = Math.min(Math.max((clientX - rect.left) / rect.width, 0), 1)
  const index = Math.min(Math.floor(ratio * times.length), times.length - 1)
  return {
    time: times[index],
    models: null,
  }
}

export function getDashboardLegendDrilldownTarget(options: {
  event: unknown
  chartValues: unknown
  otherLabel?: string
}): DashboardDrilldownTarget | null {
  const model = extractLegendModel(options.event)
  const times = getUniqueChartTimes(options.chartValues)
  if (!model || times.length === 0) return null

  return {
    time: formatRangeLabel(times),
    times,
    models:
      model === options.otherLabel
        ? getCollapsedModelsForChartRange(
            options.chartValues,
            options.otherLabel
          )
        : [model],
  }
}

export function createDashboardChartAreaClickGuard() {
  let chartClickHandled = false

  return {
    markChartClickHandled: (target: DashboardDrilldownTarget | null) => {
      chartClickHandled = Boolean(target?.time)
    },
    shouldHandleAreaClick: () => {
      if (!chartClickHandled) return true
      chartClickHandled = false
      return false
    },
  }
}

export function buildDashboardDrilldown(options: {
  data: QuotaDataItem[]
  targetTime: string
  targetTimes?: string[]
  granularity: TimeGranularity
  models?: string[] | null
  unknownLabel?: string
}): DashboardDrilldownDetail | null {
  const targetTimeSet = Array.isArray(options.targetTimes)
    ? new Set(
        options.targetTimes.filter(
          (time): time is string => typeof time === 'string' && time !== ''
        )
      )
    : null
  if ((!options.targetTime && !targetTimeSet) || !Array.isArray(options.data)) {
    return null
  }

  const modelFilter = Array.isArray(options.models)
    ? new Set(options.models)
    : null
  const modelMap = new Map<
    string,
    {
      model: string
      quota: number
      count: number
      tokens: number
    }
  >()

  for (const item of options.data) {
    const timeKey = formatDashboardTime(
      parseDashboardTimestamp(item.created_at),
      options.granularity
    )
    if (
      targetTimeSet
        ? !targetTimeSet.has(timeKey)
        : timeKey !== options.targetTime
    ) {
      continue
    }

    const model = item.model_name || options.unknownLabel || 'Unknown'
    if (modelFilter && !modelFilter.has(model)) continue

    const previous = modelMap.get(model) || {
      model,
      quota: 0,
      count: 0,
      tokens: 0,
    }
    modelMap.set(model, {
      model,
      quota: previous.quota + toFiniteNumber(item.quota),
      count: previous.count + toFiniteNumber(item.count),
      tokens: previous.tokens + toFiniteNumber(item.token_used),
    })
  }

  const rows = Array.from(modelMap.values())
    .filter((item) => item.quota > 0 || item.count > 0 || item.tokens > 0)
    .sort((a, b) => b.quota - a.quota || b.count - a.count)
  const totals = rows.reduce(
    (sum, item) => ({
      quota: sum.quota + item.quota,
      count: sum.count + item.count,
      tokens: sum.tokens + item.tokens,
    }),
    { quota: 0, count: 0, tokens: 0 }
  )

  return {
    time:
      options.targetTime || formatRangeLabel(Array.from(targetTimeSet || [])),
    rows: rows.map((item) => ({
      ...item,
      ratio: totals.quota > 0 ? item.quota / totals.quota : 0,
    })),
    totalQuota: totals.quota,
    totalCount: totals.count,
    totalTokens: totals.tokens,
  }
}

function findDashboardDrilldownDatum(
  datum: unknown,
  otherLabel?: string
): DashboardDatum | null {
  if (Array.isArray(datum)) {
    // Dimension events include every series at a time bucket; prefer the scoped Other datum.
    const scopedOtherDatum = datum
      .map((item) => findDashboardDrilldownDatum(item, otherLabel))
      .find(
        (item) =>
          item &&
          item.Model === otherLabel &&
          Array.isArray(item.CollapsedModels)
      )
    if (scopedOtherDatum) return scopedOtherDatum

    for (const item of datum) {
      const matched = findDashboardDrilldownDatum(item, otherLabel)
      if (matched) return matched
    }
    return null
  }
  if (!datum || typeof datum !== 'object') return null

  const record = datum as DashboardDatum
  return record.Time ? record : null
}

function getUniqueChartTimes(chartValues: unknown): string[] {
  if (!Array.isArray(chartValues)) return []
  const times: string[] = []
  const seen = new Set<string>()
  for (const item of chartValues) {
    if (!item || typeof item !== 'object') continue
    const time = (item as DashboardDatum).Time
    if (time == null || seen.has(String(time))) continue
    seen.add(String(time))
    times.push(String(time))
  }
  return times
}

function formatRangeLabel(times: string[]): string {
  if (times.length === 0) return ''
  if (times.length === 1) return times[0]
  return `${times[0]} - ${times[times.length - 1]}`
}

function extractLegendModel(event: unknown): string {
  const candidates = [
    getPathValue(event, ['event', 'detail', 'data', 'id']),
    getPathValue(event, ['event', 'detail', 'data', 'label']),
    getPathValue(event, ['event', 'detail', 'data', 'value']),
    getPathValue(event, ['detail', 'data', 'id']),
    getPathValue(event, ['detail', 'data', 'label']),
    getPathValue(event, ['detail', 'data', 'value']),
    getPathValue(event, ['data', 'id']),
    getPathValue(event, ['data', 'label']),
    getPathValue(event, ['data', 'value']),
    getPathValue(event, ['datum', 'Model']),
    getPathValue(event, ['Model']),
    getPathValue(event, ['value']),
  ]
  const matched = candidates.find(
    (value) => typeof value === 'string' && value !== ''
  )
  return typeof matched === 'string' ? matched : ''
}

function getCollapsedModelsForChartRange(
  chartValues: unknown,
  otherLabel?: string
): string[] {
  if (!Array.isArray(chartValues)) return []

  const models: string[] = []
  const seen = new Set<string>()
  for (const item of chartValues) {
    if (!item || typeof item !== 'object') continue
    const record = item as DashboardDatum
    if (record.Model !== otherLabel || !Array.isArray(record.CollapsedModels)) {
      continue
    }
    for (const model of record.CollapsedModels) {
      if (typeof model !== 'string' || model === '' || seen.has(model)) {
        continue
      }
      seen.add(model)
      models.push(model)
    }
  }
  return models
}

function getPathValue(source: unknown, path: string[]): unknown {
  let current = source
  for (const key of path) {
    if (!current || typeof current !== 'object') return undefined
    current = (current as Record<string, unknown>)[key]
  }
  return current
}

function formatDashboardTime(
  timestamp: number,
  granularity: TimeGranularity
): string {
  if (!Number.isFinite(timestamp) || timestamp < 0) {
    throw new Error('Invalid timestamp')
  }

  const date = new Date(timestamp * 1000)
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const base = `${month}-${day}`
  if (granularity === 'hour') {
    return `${base} ${String(date.getHours()).padStart(2, '0')}:00`
  }
  if (granularity === 'week') {
    const weekEnd = new Date(date)
    weekEnd.setDate(date.getDate() + 6)
    return `${base} - ${String(weekEnd.getMonth() + 1).padStart(2, '0')}-${String(weekEnd.getDate()).padStart(2, '0')}`
  }
  return base
}

function parseDashboardTimestamp(value: unknown): number {
  if (value == null) {
    throw new Error('Invalid timestamp')
  }

  const timestamp = Number(value)
  if (!Number.isFinite(timestamp) || timestamp < 0) {
    throw new Error('Invalid timestamp')
  }
  return timestamp
}

function toFiniteNumber(value: unknown): number {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}
