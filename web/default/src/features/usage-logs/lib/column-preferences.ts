export type ColumnOrderDirection = 'up' | 'down'
export type ColumnDropPosition = 'before' | 'after'

function uniqueStrings(values: readonly string[]): string[] {
  return values.filter(
    (value, index) => value.length > 0 && values.indexOf(value) === index
  )
}

export function normalizeColumnOrder(
  savedOrder: unknown,
  defaultOrder: readonly string[]
): string[] {
  const defaults = uniqueStrings(defaultOrder)
  if (defaults.length === 0) return []

  const defaultKeys = new Set(defaults)
  const saved = Array.isArray(savedOrder)
    ? savedOrder.filter((value): value is string => typeof value === 'string')
    : []
  const ordered = saved.filter(
    (key, index) => defaultKeys.has(key) && saved.indexOf(key) === index
  )

  return [...ordered, ...defaults.filter((key) => !ordered.includes(key))]
}

export function moveColumnKey(
  columnOrder: readonly string[],
  columnKey: string,
  direction: ColumnOrderDirection,
  movableKeys: readonly string[]
): string[] {
  const nextOrder = [...columnOrder]
  const movableSet = new Set(movableKeys)
  if (!movableSet.has(columnKey)) return nextOrder

  const movableOrder = nextOrder.filter((key) => movableSet.has(key))
  const currentIndex = movableOrder.indexOf(columnKey)
  const offset = direction === 'up' ? -1 : 1
  const targetIndex = currentIndex + offset
  if (
    currentIndex < 0 ||
    targetIndex < 0 ||
    targetIndex >= movableOrder.length
  ) {
    return nextOrder
  }

  const targetKey = movableOrder[targetIndex]
  const sourcePosition = nextOrder.indexOf(columnKey)
  const targetPosition = nextOrder.indexOf(targetKey)
  nextOrder[sourcePosition] = targetKey
  nextOrder[targetPosition] = columnKey
  return nextOrder
}

export function reorderColumnKey(
  columnOrder: readonly string[],
  sourceKey: string,
  targetKey: string,
  position: ColumnDropPosition,
  movableKeys: readonly string[]
): string[] {
  if (sourceKey === targetKey) return [...columnOrder]

  const movableSet = new Set(movableKeys)
  if (!movableSet.has(sourceKey) || !movableSet.has(targetKey)) {
    return [...columnOrder]
  }

  const movableOrder = columnOrder.filter((key) => movableSet.has(key))
  const sourceIndex = movableOrder.indexOf(sourceKey)
  const targetIndex = movableOrder.indexOf(targetKey)
  if (sourceIndex < 0 || targetIndex < 0) return [...columnOrder]

  const nextMovableOrder = movableOrder.filter((key) => key !== sourceKey)
  const adjustedTargetIndex = nextMovableOrder.indexOf(targetKey)
  const insertIndex =
    position === 'after' ? adjustedTargetIndex + 1 : adjustedTargetIndex
  nextMovableOrder.splice(insertIndex, 0, sourceKey)

  let movableIndex = 0
  return columnOrder.map((key) => {
    if (!movableSet.has(key)) return key
    const nextKey = nextMovableOrder[movableIndex]
    movableIndex += 1
    return nextKey
  })
}

export function mapLegacyUsageLogColumnKey(key: string): string | null {
  const mappings: Record<string, string> = {
    time: 'created_at',
    channel: 'channel',
    username: 'user',
    token: 'token_name',
    model: 'model_name',
    use_time: 'use_time',
    routing_result: 'responses_routing',
    channel_health: 'channel_health',
    prompt: 'prompt_tokens',
    completion: 'prompt_tokens',
    cost: 'quota',
    details: 'content',
  }
  return mappings[key] ?? null
}

export function mapLegacyUsageLogColumnOrder(savedOrder: unknown): string[] {
  if (!Array.isArray(savedOrder)) return []
  return savedOrder
    .filter((value): value is string => typeof value === 'string')
    .map(mapLegacyUsageLogColumnKey)
    .filter((value): value is string => value !== null)
}
