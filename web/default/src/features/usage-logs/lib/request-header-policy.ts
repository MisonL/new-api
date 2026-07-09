import type { LogOtherData, RequestHeaderPolicyInfo } from '../types'

function hasAuditValue(value: unknown): boolean {
  if (value === undefined || value === null) return false
  if (typeof value === 'string') return value.trim() !== ''
  if (Array.isArray(value)) return value.length > 0
  if (typeof value === 'boolean') return value
  if (typeof value === 'object') return Object.keys(value).length > 0
  return true
}

function hasOwnAuditField(source: object, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(source, key)
}

function hasBooleanAuditField(
  source: LogOtherData,
  key: keyof Pick<
    LogOtherData,
    | 'header_profile_applied'
    | 'override_static_user_agent'
    | 'user_agent_applied'
  >
): boolean {
  return (
    hasOwnAuditField(source, key) &&
    source[key] !== undefined &&
    source[key] !== null
  )
}

export function getRequestHeaderPolicy(
  other: LogOtherData | null | undefined
): RequestHeaderPolicyInfo | undefined {
  if (!other) return undefined
  if (hasAuditValue(other.request_header_policy)) {
    return other.request_header_policy
  }
  const fallback: RequestHeaderPolicyInfo = {}
  const mode = other.header_policy_mode || other.request_header_policy_mode
  if (hasAuditValue(mode)) fallback.mode = mode
  if (hasAuditValue(other.header_profile_id)) {
    fallback.header_profile_id = other.header_profile_id
  }
  if (hasAuditValue(other.header_profile_mode)) {
    fallback.header_profile_mode = other.header_profile_mode
  }
  if (hasBooleanAuditField(other, 'header_profile_applied')) {
    fallback.header_profile_applied = other.header_profile_applied
  }
  if (hasAuditValue(other.ua_strategy_mode)) {
    fallback.ua_strategy_mode = other.ua_strategy_mode
  }
  if (hasAuditValue(other.ua_strategy_scope)) {
    fallback.ua_strategy_scope = other.ua_strategy_scope
  }
  if (hasAuditValue(other.selected_user_agent)) {
    fallback.selected_user_agent = other.selected_user_agent
  }
  if (hasAuditValue(other.applied_user_agent)) {
    fallback.applied_user_agent = other.applied_user_agent
  }
  if (hasBooleanAuditField(other, 'override_static_user_agent')) {
    fallback.override_static_user_agent = other.override_static_user_agent
  }
  if (hasBooleanAuditField(other, 'user_agent_applied')) {
    fallback.user_agent_applied = other.user_agent_applied
  }
  if (hasAuditValue(other.applied_header_keys)) {
    fallback.applied_header_keys = other.applied_header_keys
  }
  if (hasAuditValue(other.applied_headers)) {
    fallback.applied_headers = other.applied_headers
  }
  return Object.keys(fallback).length > 0 ? fallback : undefined
}
