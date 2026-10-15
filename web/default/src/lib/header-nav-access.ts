import { getStatus } from '@/lib/api'

export type HeaderNavAccessModule = 'pricing' | 'rankings'

export type HeaderNavModuleAccess = {
  enabled: boolean
  requireAuth: boolean
}

const DEFAULT_ACCESS: HeaderNavModuleAccess = {
  enabled: true,
  requireAuth: false,
}

export function parseHeaderNavModuleAccess(
  raw: unknown,
  module: HeaderNavAccessModule
): HeaderNavModuleAccess {
  if (typeof raw !== 'string' || !raw.trim()) return { ...DEFAULT_ACCESS }

  try {
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return { ...DEFAULT_ACCESS }
    }
    const value = (parsed as Record<string, unknown>)[module]
    if (typeof value === 'boolean') {
      return { enabled: value, requireAuth: false }
    }
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      return { ...DEFAULT_ACCESS }
    }
    const record = value as Record<string, unknown>
    return {
      enabled:
        typeof record.enabled === 'boolean'
          ? record.enabled
          : DEFAULT_ACCESS.enabled,
      requireAuth:
        typeof record.requireAuth === 'boolean'
          ? record.requireAuth
          : DEFAULT_ACCESS.requireAuth,
    }
  } catch {
    return { ...DEFAULT_ACCESS }
  }
}

function getCachedHeaderNavModules(): unknown {
  try {
    if (typeof window === 'undefined') return undefined
    const raw = window.localStorage.getItem('status')
    if (!raw) return undefined
    const status = JSON.parse(raw) as Record<string, unknown>
    return status.HeaderNavModules
  } catch {
    return undefined
  }
}

export async function getHeaderNavModuleAccess(
  module: HeaderNavAccessModule
): Promise<HeaderNavModuleAccess> {
  try {
    const status = await getStatus()
    return parseHeaderNavModuleAccess(status?.HeaderNavModules, module)
  } catch {
    return parseHeaderNavModuleAccess(getCachedHeaderNavModules(), module)
  }
}
