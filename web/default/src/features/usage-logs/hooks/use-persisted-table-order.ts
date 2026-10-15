import {
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type SetStateAction,
} from 'react'
import { normalizeColumnOrder } from '../lib/column-preferences'

function readStoredOrder(
  storageKey: string,
  defaults: readonly string[],
  legacyStorageKeys: readonly string[],
  mapLegacyOrder?: (value: unknown) => unknown
): string[] {
  if (typeof window === 'undefined') return [...defaults]

  for (const key of [storageKey, ...legacyStorageKeys]) {
    try {
      const raw = window.localStorage.getItem(key)
      if (!raw) continue
      const parsed: unknown = JSON.parse(raw)
      const candidate = key === storageKey ? parsed : mapLegacyOrder?.(parsed)
      const normalized = normalizeColumnOrder(candidate, defaults)
      if (normalized.length > 0) return normalized
    } catch {
      // Ignore malformed or unavailable browser storage and use defaults.
    }
  }

  return [...defaults]
}

interface PersistedTableOrderOptions {
  legacyStorageKeys?: readonly string[]
  mapLegacyOrder?: (value: unknown) => unknown
}

export function usePersistedTableOrder(
  tableKey: string,
  defaultOrder: readonly string[],
  options: PersistedTableOrderOptions = {}
): [string[], Dispatch<SetStateAction<string[]>>] {
  const storageKey = `table-column-order:${tableKey}`
  const defaultOrderKey = defaultOrder.join('\u0000')
  const stableDefaults = useMemo(
    () => (defaultOrderKey ? defaultOrderKey.split('\u0000') : []),
    [defaultOrderKey]
  )
  const legacyStorageKeysKey = (options.legacyStorageKeys ?? []).join('\u0000')
  const stableLegacyStorageKeys = useMemo(
    () => (legacyStorageKeysKey ? legacyStorageKeysKey.split('\u0000') : []),
    [legacyStorageKeysKey]
  )
  const mapLegacyOrder = options.mapLegacyOrder
  const [storedOrder, setStoredOrder] = useState<string[]>(() =>
    readStoredOrder(
      storageKey,
      stableDefaults,
      stableLegacyStorageKeys,
      mapLegacyOrder
    )
  )
  const normalizedOrder = useMemo(
    () => normalizeColumnOrder(storedOrder, stableDefaults),
    [storedOrder, stableDefaults]
  )

  useEffect(() => {
    setStoredOrder(
      readStoredOrder(
        storageKey,
        stableDefaults,
        stableLegacyStorageKeys,
        mapLegacyOrder
      )
    )
  }, [mapLegacyOrder, stableDefaults, stableLegacyStorageKeys, storageKey])

  useEffect(() => {
    try {
      window.localStorage.setItem(storageKey, JSON.stringify(normalizedOrder))
    } catch {
      // Storage can be unavailable in privacy-restricted browser contexts.
    }
  }, [normalizedOrder, storageKey])

  return [normalizedOrder, setStoredOrder]
}
