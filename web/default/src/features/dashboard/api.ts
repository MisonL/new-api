import { api } from '@/lib/api'
import { parseDashboardTokenStats } from './lib/token-stats'
import type {
  DashboardLogStats,
  QuotaDataItem,
  UptimeGroupResult,
} from './types'

// ============================================================================
// Dashboard APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Quota & Usage Data
// ----------------------------------------------------------------------------

// Get user quota data within a time range
// Admin users get all users' data by default (matching classic frontend behavior)
export async function getUserQuotaDates(
  params: {
    start_timestamp: number
    end_timestamp: number
    default_time?: string
    username?: string
  },
  isAdmin = false
) {
  const endpoint = isAdmin ? '/api/data' : '/api/data/self'
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    endpoint,
    { params }
  )
  return res.data
}

export async function getDashboardLogStats(
  params: { start_timestamp: number; end_timestamp: number; username?: string },
  isAdmin = false,
  signal?: AbortSignal
) {
  const endpoint = isAdmin ? '/api/log/stat' : '/api/log/self/stat'
  const res = await api.get<{
    success: boolean
    data: DashboardLogStats
  }>(endpoint, { params: { ...params, type: 2 }, signal })
  return parseDashboardTokenStats(res.data)
}

// ----------------------------------------------------------------------------
// System Monitoring
// ----------------------------------------------------------------------------

export async function getUserQuotaDataByUsers(params: {
  start_timestamp: number
  end_timestamp: number
}) {
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    '/api/data/users',
    { params }
  )
  return res.data
}

// Get uptime monitoring status for all services
export async function getUptimeStatus() {
  const res = await api.get<{ success: boolean; data: UptimeGroupResult[] }>(
    '/api/uptime/status'
  )
  return res.data
}
