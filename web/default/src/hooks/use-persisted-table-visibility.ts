import { useEffect, useState, type Dispatch, type SetStateAction } from 'react'
import type { VisibilityState } from '@tanstack/react-table'

/**
 * Merge persisted visibility over the caller's defaults.
 *
 * A required column (a column definition carrying `enableHiding: false`, such
 * as a row-actions column) must stay visible. TanStack's `getIsVisible` reads
 * only the visibility state and does not consult `getCanHide`, so a persisted
 * `{ actions: false }` would hide a required column that the toggle menu cannot
 * even offer back. `requiredVisible` names those columns so a stale or
 * hand-edited entry cannot make a required column disappear.
 */
export function mergeVisibility(
  defaults: VisibilityState,
  persisted: unknown,
  requiredVisible: readonly string[] = []
): VisibilityState {
  if (!persisted || typeof persisted !== 'object' || Array.isArray(persisted)) {
    return defaults
  }
  const merged: VisibilityState = {
    ...defaults,
    ...(persisted as VisibilityState),
  }
  for (const key of requiredVisible) {
    merged[key] = true
  }
  return merged
}

function readVisibility(
  storageKey: string,
  defaults: VisibilityState,
  requiredVisible: readonly string[]
) {
  if (typeof window === 'undefined') return defaults

  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return defaults
    return mergeVisibility(
      defaults,
      JSON.parse(raw) as unknown,
      requiredVisible
    )
  } catch {
    return defaults
  }
}

/**
 * Persist a table's column visibility without allowing required columns to
 * disappear. Pass every column id whose definition sets `enableHiding: false`
 * through `requiredVisible`.
 */
export function usePersistedTableVisibility(
  tableKey: string,
  defaults: VisibilityState = {},
  requiredVisible: readonly string[] = []
): [VisibilityState, Dispatch<SetStateAction<VisibilityState>>] {
  const storageKey = `table-column-visibility:${tableKey}`
  // Join to a stable primitive so the initializer and the storage effect do not
  // re-run on every render when the caller passes an inline array.
  const requiredKey = requiredVisible.join(',')
  const [visibility, setVisibility] = useState<VisibilityState>(() =>
    readVisibility(
      storageKey,
      defaults,
      requiredKey ? requiredKey.split(',') : []
    )
  )

  useEffect(() => {
    try {
      window.localStorage.setItem(storageKey, JSON.stringify(visibility))
    } catch {
      // Storage can be unavailable in privacy-restricted browser contexts.
    }
  }, [storageKey, visibility])

  return [visibility, setVisibility]
}
