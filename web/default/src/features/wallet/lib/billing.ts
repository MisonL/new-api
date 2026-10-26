import { formatTimestampToDate } from '@/lib/format'
import type { StatusBadgeProps } from '@/components/status-badge'
import type { TopupStatus } from '../types'

// ============================================================================
// Billing Utility Functions
// ============================================================================

interface StatusConfig {
  variant: StatusBadgeProps['variant']
  // i18n 键；组件里用 t(labelKey) 渲染，否则非英语界面会一直显示英文。
  labelKey: string
}

/**
 * Status badge configuration
 */
export const STATUS_CONFIG: Record<TopupStatus, StatusConfig> = {
  success: {
    variant: 'success',
    labelKey: 'Success',
  },
  pending: {
    variant: 'warning',
    labelKey: 'Pending',
  },
  expired: {
    variant: 'danger',
    labelKey: 'Expired',
  },
}

/**
 * Get status badge configuration
 */
export function getStatusConfig(status: TopupStatus): StatusConfig {
  return STATUS_CONFIG[status] || STATUS_CONFIG.pending
}

/**
 * Payment method display names
 */
export const PAYMENT_METHOD_NAMES: Record<string, string> = {
  stripe: 'Stripe',
  alipay: 'Alipay',
  wxpay: 'WeChat Pay',
  waffo: 'Waffo',
}

/**
 * Get payment method display name
 */
export function getPaymentMethodName(method: string): string {
  return PAYMENT_METHOD_NAMES[method] || method
}

/**
 * Format timestamp to readable date string
 */
export function formatTimestamp(timestamp: number): string {
  return formatTimestampToDate(timestamp)
}
