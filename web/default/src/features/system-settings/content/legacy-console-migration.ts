import type { SystemOption } from '../types'

export const LEGACY_CONSOLE_SETTING_KEYS = [
  'ApiInfo',
  'Announcements',
  'FAQ',
  'UptimeKumaUrl',
  'UptimeKumaSlug',
] as const

export type LegacyConsoleSettingKey =
  (typeof LEGACY_CONSOLE_SETTING_KEYS)[number]

export type LegacyConsoleSettingValues = Partial<
  Record<LegacyConsoleSettingKey, string>
>

/**
 * Keep the detection rule aligned with the backend migration endpoint: any
 * non-empty legacy value is actionable, including a partially configured
 * Uptime Kuma pair that the backend will clear without importing.
 */
export function getLegacyConsoleSettingValues(
  options: readonly SystemOption[] | undefined
): LegacyConsoleSettingValues {
  const optionMap = new Map(
    (options ?? []).map((option) => [option.key, option.value])
  )
  const legacyValues: LegacyConsoleSettingValues = {}

  for (const key of LEGACY_CONSOLE_SETTING_KEYS) {
    const value = optionMap.get(key)
    if (value !== undefined && value !== '') {
      legacyValues[key] = value
    }
  }

  return legacyValues
}

export function hasLegacyConsoleSettings(
  options: readonly SystemOption[] | undefined
): boolean {
  return Object.keys(getLegacyConsoleSettingValues(options)).length > 0
}
