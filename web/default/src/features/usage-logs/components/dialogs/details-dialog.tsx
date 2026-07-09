import {
  Copy,
  Check,
  Route,
  Settings2,
  AlertTriangle,
  Headphones,
  Monitor,
  Cloud,
  Globe,
  ShieldCheck,
  UserCog,
  Info,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import {
  formatLogQuota,
  formatTimestampToDate,
  formatTokens,
  formatUseTime,
} from '@/lib/format'
import { cn } from '@/lib/utils'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import { DynamicPricingBreakdown } from '@/features/pricing/components/dynamic-pricing-breakdown'
import type { UsageLog } from '../../data/schema'
import {
  parseLogOther,
  getParamOverrideActionLabel,
  parseAuditLine,
  decodeBillingExprB64,
  formatRatioDisplay,
  getStreamStatusDisplayInfo,
  getTieredBillingSummary,
  hasAnyCacheTokens,
  isViolationFeeLog,
  getFirstResponseTimeColor,
  getResponseTimeColor,
  formatDurationFromMs,
  isDisplayableDurationMs,
} from '../../lib/format'
import {
  getLogTypeConfig,
  isDisplayableLogType,
  isPerCallBilling,
  isTimingLogType,
} from '../../lib/utils'
import { getRequestHeaderPolicy } from '../../lib/request-header-policy'
import type { LogOtherData } from '../../types'
import { ModelBadge, type ModelBadgeInfoRow } from '../model-badge'

function timingTextColorClass(
  variant: 'success' | 'warning' | 'danger'
): string {
  if (variant === 'success') return 'text-emerald-600'
  if (variant === 'warning') return 'text-amber-600'
  return 'text-rose-600'
}

function DetailRow(props: {
  label: React.ReactNode
  value: React.ReactNode
  mono?: boolean
  muted?: boolean
}) {
  return (
    <div className='grid min-w-0 grid-cols-[5.25rem_minmax(0,1fr)] gap-2 text-sm sm:grid-cols-[7rem_minmax(0,1fr)] sm:gap-3'>
      <span className='text-muted-foreground min-w-0 text-xs'>
        {props.label}
      </span>
      <span
        className={cn(
          'max-w-full min-w-0 text-xs break-all whitespace-pre-wrap sm:break-words',
          props.mono && 'font-mono',
          props.muted && 'text-muted-foreground'
        )}
      >
        {props.value}
      </span>
    </div>
  )
}

function DetailSection(props: {
  icon?: React.ReactNode
  label: string
  variant?: 'default' | 'danger'
  children: React.ReactNode
}) {
  const isDanger = props.variant === 'danger'
  return (
    <div className='min-w-0 space-y-1.5'>
      <Label
        className={cn(
          'flex items-center gap-1.5 text-xs font-semibold',
          isDanger && 'text-red-500'
        )}
      >
        {props.icon}
        {props.label}
      </Label>
      <div
        className={cn(
          'min-w-0 space-y-1 overflow-hidden rounded-md border p-2.5 max-sm:p-2',
          isDanger
            ? 'border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/20'
            : 'bg-muted/30'
        )}
      >
        {props.children}
      </div>
    </div>
  )
}

function getResponsesCompactModeLabel(
  mode: string | undefined,
  t: (key: string, opts?: Record<string, unknown>) => string
): string {
  switch (mode) {
    case 'native':
      return t('Compact Native')
    case 'synthetic_summary':
      return t('Compact Synthetic')
    default:
      return mode ? String(mode) : '-'
  }
}

function formatRequestHeaderPolicyMode(
  mode: string | undefined,
  t: (key: string) => string
): string {
  switch (mode) {
    case 'prefer_channel':
      return t('Channel preferred')
    case 'prefer_tag':
      return t('Tag preferred')
    case 'merge':
      return t('Merge')
    default:
      return mode ? String(mode) : '-'
  }
}

function isUserAgentHeaderKey(key: string | undefined): boolean {
  return (
    String(key || '')
      .trim()
      .toLowerCase() === 'user-agent'
  )
}

function getAppliedUserAgent(other: LogOtherData): string {
  const policy = getRequestHeaderPolicy(other)
  return String(
    policy?.applied_user_agent || policy?.selected_user_agent || ''
  ).trim()
}

function getAppliedHeaderLines(other: LogOtherData): string[] {
  const policy = getRequestHeaderPolicy(other)
  if (!policy) return []
  const entries = Array.isArray(policy.applied_headers)
    ? policy.applied_headers
        .map((entry) => ({
          key: String(entry?.key || '').trim(),
          value: String(entry?.value || '').trim(),
        }))
        .filter((entry) => entry.key && !isUserAgentHeaderKey(entry.key))
    : []
  if (entries.length > 0) {
    return entries.map((entry) =>
      entry.value ? `${entry.key}: ${entry.value}` : entry.key
    )
  }
  if (!Array.isArray(policy.applied_header_keys)) return []
  return policy.applied_header_keys
    .map((key) => String(key || '').trim())
    .filter((key) => key && !isUserAgentHeaderKey(key))
}

function RequestHeaderPolicyDetails(props: { other: LogOtherData }) {
  const { t } = useTranslation()
  const { other } = props
  const policy = getRequestHeaderPolicy(other)
  if (!policy) return null

  const userAgent = getAppliedUserAgent(other)
  const headerLines = getAppliedHeaderLines(other)

  return (
    <DetailSection
      icon={<Settings2 className='size-3.5' aria-hidden='true' />}
      label={t('Request Header Policy')}
    >
      <DetailRow
        label={t('Mode')}
        value={formatRequestHeaderPolicyMode(policy.mode, t)}
        mono
      />
      {policy.header_profile_id && (
        <DetailRow
          label={t('Header Profile')}
          value={policy.header_profile_id}
          mono
        />
      )}
      {policy.header_profile_mode && (
        <DetailRow
          label={t('Profile Mode')}
          value={policy.header_profile_mode}
          mono
        />
      )}
      {typeof policy.header_profile_applied === 'boolean' && (
        <DetailRow
          label={t('Header Profile Applied')}
          value={policy.header_profile_applied ? t('Yes') : t('No')}
          mono
        />
      )}
      {userAgent && (
        <DetailRow label={t('Applied User-Agent')} value={userAgent} mono />
      )}
      {typeof policy.user_agent_applied === 'boolean' && (
        <DetailRow
          label={t('User-Agent Applied')}
          value={policy.user_agent_applied ? t('Yes') : t('No')}
          mono
        />
      )}
      {typeof policy.override_static_user_agent === 'boolean' && (
        <DetailRow
          label={t('Override Static User-Agent')}
          value={policy.override_static_user_agent ? t('Yes') : t('No')}
          mono
        />
      )}
      {headerLines.length > 0 && (
        <DetailRow
          label={t('Applied Headers')}
          value={headerLines.join('\n')}
          mono
        />
      )}
    </DetailSection>
  )
}

function ResponsesCompactDetails(props: {
  other: LogOtherData
  isAdmin: boolean
}) {
  const { t } = useTranslation()
  const { other, isAdmin } = props
  const capability = other.channel_capability_snapshot
  const shouldShow =
    Boolean(other.responses_compact_mode) ||
    other.responses_encrypted_context_retry === true ||
    Boolean(capability)
  if (!shouldShow) return null

  const isAutoFallback = other.responses_compact_auto_fallback === true
  const isAutoFallbackWindow =
    other.responses_compact_auto_fallback_window === true
  const retryUntil = other.responses_compact_auto_fallback_retry_until
    ? formatTimestampToDate(other.responses_compact_auto_fallback_retry_until)
    : ''
  const finalPath =
    other.responses_compact_final_upstream_path ||
    other.responses_compact_upstream_path
  const observedReason =
    other.admin_info?.responses_channel_capability_observed?.reason
  const probeReason =
    other.admin_info?.responses_channel_capability_probe?.reason
  const formatCapability = (value: boolean | undefined) => {
    if (value === true) return t('Supported')
    if (value === false) return t('Unsupported')
    return '-'
  }

  return (
    <DetailSection label={t('Responses Compact capability')}>
      {other.responses_compact_mode && (
        <DetailRow
          label={t('Mode')}
          value={getResponsesCompactModeLabel(other.responses_compact_mode, t)}
        />
      )}
      {other.responses_compact_setting && (
        <DetailRow
          label={t('Configuration')}
          value={other.responses_compact_setting}
          mono
        />
      )}
      {isAutoFallback && (
        <DetailRow
          label={t('Auto Fallback')}
          value={
            <StatusBadge
              label={t('Compact Auto Switched Compatible Mode')}
              variant='yellow'
              size='sm'
              copyable={false}
            />
          }
        />
      )}
      {other.responses_compact_fallback_reason && (
        <DetailRow
          label={t('Auto Fallback Reason')}
          value={other.responses_compact_fallback_reason}
          mono
        />
      )}
      {other.responses_compact_visible_only_fallback && (
        <DetailRow label={t('Visible-only Fallback')} value={t('Yes')} mono />
      )}
      {other.responses_encrypted_context_retry && (
        <DetailRow label={t('Encrypted Context Retry')} value={t('Yes')} mono />
      )}
      {finalPath && (
        <DetailRow label={t('Final Path')} value={finalPath} mono />
      )}
      {capability?.source && (
        <DetailRow
          label={t('Capability Source')}
          value={capability.source}
          mono
        />
      )}
      {capability?.profile && (
        <DetailRow
          label={t('Upstream Profile')}
          value={capability.profile}
          mono
        />
      )}
      {capability?.compact_mode_effective && (
        <DetailRow
          label={t('Effective Compact Mode')}
          value={capability.compact_mode_effective}
          mono
        />
      )}
      {capability && (
        <>
          <DetailRow
            label={t('Responses')}
            value={formatCapability(capability.supports_responses)}
          />
          <DetailRow
            label={t('Responses Compact')}
            value={formatCapability(capability.supports_responses_compact)}
          />
          <DetailRow
            label={t('REST Previous ID')}
            value={formatCapability(
              capability.supports_rest_previous_response_id
            )}
          />
          <DetailRow
            label={t('Compaction Item Passthrough')}
            value={formatCapability(
              capability.supports_compaction_item_passthrough
            )}
          />
          <DetailRow
            label={t('Namespace Tools')}
            value={formatCapability(capability.supports_namespace_tools)}
          />
          <DetailRow
            label={t('Strips Encrypted Reasoning')}
            value={formatCapability(
              capability.strips_responses_encrypted_reasoning
            )}
          />
          {isAdmin && observedReason && (
            <DetailRow
              label={t('Last Observed Failure')}
              value={observedReason}
              mono
            />
          )}
          {isAdmin && probeReason && (
            <DetailRow
              label={t('Last Probe Result')}
              value={probeReason}
              mono
            />
          )}
        </>
      )}
      {isAdmin && isAutoFallbackWindow && (
        <DetailRow
          label={t('Auto Fallback')}
          value={t('Compact Auto Fallback Window Active')}
        />
      )}
      {isAdmin &&
        (other.responses_compact_native_attempted || isAutoFallbackWindow) && (
          <>
            {other.responses_compact_native_upstream_path && (
              <DetailRow
                label={t('Native Path')}
                value={other.responses_compact_native_upstream_path}
                mono
              />
            )}
            {other.responses_compact_native_status_code != null && (
              <DetailRow
                label={t('Native Status')}
                value={String(other.responses_compact_native_status_code)}
                mono
              />
            )}
            {other.responses_compact_auto_fallback_reason && (
              <DetailRow
                label={t('Auto Fallback Reason')}
                value={other.responses_compact_auto_fallback_reason}
              />
            )}
            {other.responses_compact_auto_fallback_retry_interval_hours !=
              null && (
              <DetailRow
                label={t('Retry Interval')}
                value={`${other.responses_compact_auto_fallback_retry_interval_hours} ${t('Hours')}`}
                mono
              />
            )}
            {retryUntil && (
              <DetailRow label={t('Retry After')} value={retryUntil} mono />
            )}
          </>
        )}
    </DetailSection>
  )
}

type TranslateFn = (key: string, opts?: Record<string, unknown>) => string

function getMappedModelName(other: LogOtherData | null): string | undefined {
  const upstreamModel = String(other?.upstream_model_name || '').trim()
  if (!other?.is_model_mapped || !upstreamModel) return undefined
  return upstreamModel
}

function getBaseInputPriceUSD(other: LogOtherData): number {
  return other.model_ratio != null ? other.model_ratio * 2.0 : 0
}

function getEffectiveGroupRatio(other: LogOtherData): {
  ratio?: number
  isUserGroup: boolean
} {
  const userGroupRatio = other.user_group_ratio
  const isUserGroup =
    userGroupRatio != null &&
    Number.isFinite(userGroupRatio) &&
    userGroupRatio !== -1
  return {
    ratio: isUserGroup ? userGroupRatio : other.group_ratio,
    isUserGroup,
  }
}

function getModelPricingSummary(
  other: LogOtherData | null,
  t: TranslateFn
): string | null {
  if (!other) return null

  if (other.billing_mode === 'tiered_expr') {
    const tieredSummary = getTieredBillingSummary(other)
    const tier = tieredSummary?.tier.label || other.matched_tier
    return tier
      ? `${t('Dynamic Pricing')} - ${tier}`
      : `${t('Dynamic Pricing')} - ${t('No matching results')}`
  }

  const priceOpts = { digitsLarge: 4, digitsSmall: 6, abbreviate: false }
  const fmtPrice = (usd: number) => formatBillingCurrencyFromUSD(usd, priceOpts)

  if (isPerCallBilling(other.model_price) && other.model_price != null) {
    return `${t('Per-call')} - ${fmtPrice(other.model_price)}`
  }
  if (other.model_ratio == null) return null

  const inputPrice = getBaseInputPriceUSD(other)
  const segments = [`${t('Input')} ${fmtPrice(inputPrice)}/M`]
  if (other.completion_ratio != null) {
    segments.push(
      `${t('Output')} ${fmtPrice(inputPrice * other.completion_ratio)}/M`
    )
  }
  return `${t('Per-token')} - ${segments.join(' - ')}`
}

function getModelGroupRatioRow(
  other: LogOtherData | null,
  t: TranslateFn
): ModelBadgeInfoRow | null {
  if (!other) return null
  const { ratio, isUserGroup } = getEffectiveGroupRatio(other)
  if (ratio == null || !Number.isFinite(ratio)) return null

  return {
    key: 'group-ratio',
    label: isUserGroup ? t('User Exclusive Ratio') : t('Group Ratio'),
    value: `${formatRatioDisplay(ratio)}x`,
    mono: true,
  }
}

function buildModelInfoRows(params: {
  log: UsageLog
  other: LogOtherData | null
  conversionChain: string[]
  conversionLabel: string
  includeRouting: boolean
  t: TranslateFn
}): ModelBadgeInfoRow[] {
  const { log, other, conversionChain, conversionLabel, includeRouting, t } =
    params
  const actualModel = getMappedModelName(other)
  const rows: ModelBadgeInfoRow[] = [
    {
      key: 'model-mapping',
      label: t('Model Mapping'),
      value: actualModel ? t('Yes') : t('No'),
      muted: !actualModel,
    },
  ]
  const group = String(log.group || other?.group || '').trim()
  if (group) {
    rows.push({
      key: 'group',
      label: t('Group'),
      value: group,
      mono: true,
    })
  }
  if (includeRouting && other?.request_path) {
    rows.push({
      key: 'client-path',
      label: t('Client Path'),
      value: other.request_path,
      mono: true,
    })
  }
  if (includeRouting && other?.upstream_request_path) {
    rows.push({
      key: 'upstream-path',
      label: t('Upstream Path'),
      value: other.upstream_request_path,
      mono: true,
    })
  }
  if (includeRouting && conversionChain.length > 0) {
    rows.push({
      key: 'request-conversion',
      label: t('Request Conversion'),
      value: conversionLabel,
      mono: true,
    })
  }
  const pricingSummary = getModelPricingSummary(other, t)
  if (pricingSummary) {
    rows.push({
      key: 'billing-mode',
      label: t('Billing Mode'),
      value: pricingSummary,
    })
  }
  const groupRatioRow = getModelGroupRatioRow(other, t)
  if (groupRatioRow) rows.push(groupRatioRow)
  return rows
}

function BillingBreakdown(props: {
  log: UsageLog
  other: LogOtherData
  isAdmin: boolean
}) {
  const { t } = useTranslation()
  const { log, other, isAdmin } = props
  const isPerCall = isPerCallBilling(other.model_price)
  const isClaude = other.claude === true
  const isTieredExpr = other.billing_mode === 'tiered_expr'
  const tieredSummary = getTieredBillingSummary(other)

  const rows: Array<{ label: string; value: string }> = []
  const priceOpts = { digitsLarge: 4, digitsSmall: 6, abbreviate: false }
  const fmtPrice = (usd: number) => formatBillingCurrencyFromUSD(usd, priceOpts)
  const baseInputUSD = getBaseInputPriceUSD(other)

  if (isTieredExpr) {
    rows.push({
      label: t('Billing Mode'),
      value: t('Dynamic Pricing'),
    })
    if (tieredSummary) {
      if (tieredSummary.tier.label) {
        rows.push({
          label: t('Matched Tier'),
          value: tieredSummary.tier.label,
        })
      }
      for (const entry of tieredSummary.priceEntries) {
        rows.push({
          label: t(entry.shortLabel),
          value: `${fmtPrice(entry.price)}/M`,
        })
      }
    } else {
      rows.push({
        label: t('Matched Tier'),
        value: t('No matching results'),
      })
    }
  } else if (isPerCall) {
    rows.push({ label: t('Billing Mode'), value: t('Per-call') })
    if (other.model_price != null) {
      rows.push({
        label: t('Model Price'),
        value: fmtPrice(other.model_price),
      })
    }
  } else {
    rows.push({ label: t('Billing Mode'), value: t('Per-token') })
    if (other.model_ratio != null) {
      rows.push({
        label: t('Input'),
        value: `${fmtPrice(baseInputUSD)}/M`,
      })
    }
    if (other.completion_ratio != null && other.model_ratio != null) {
      rows.push({
        label: t('Output'),
        value: `${fmtPrice(baseInputUSD * other.completion_ratio)}/M`,
      })
    }
  }

  const { ratio: effectiveGR, isUserGroup } = getEffectiveGroupRatio(other)
  if (effectiveGR != null && Number.isFinite(effectiveGR)) {
    rows.push({
      label: isUserGroup ? t('User Exclusive Ratio') : t('Group Ratio'),
      value: `${formatRatioDisplay(effectiveGR)}x`,
    })
  }

  if (!isTieredExpr && isClaude && hasAnyCacheTokens(other)) {
    if (other.cache_ratio != null && other.cache_ratio !== 1) {
      rows.push({
        label: t('Cache Read'),
        value: `${fmtPrice(baseInputUSD * other.cache_ratio)}/M`,
      })
    }
    if (
      other.cache_creation_ratio != null &&
      other.cache_creation_ratio !== 1
    ) {
      rows.push({
        label: t('Cache Creation'),
        value: `${fmtPrice(baseInputUSD * other.cache_creation_ratio)}/M`,
      })
    }
    if (
      other.cache_creation_ratio_5m != null &&
      other.cache_creation_ratio_5m !== 0
    ) {
      rows.push({
        label: t('Cache Creation (5m)'),
        value: `${fmtPrice(baseInputUSD * other.cache_creation_ratio_5m)}/M`,
      })
    }
    if (
      other.cache_creation_ratio_1h != null &&
      other.cache_creation_ratio_1h !== 0
    ) {
      rows.push({
        label: t('Cache Creation (1h)'),
        value: `${fmtPrice(baseInputUSD * other.cache_creation_ratio_1h)}/M`,
      })
    }
  }

  if (!isTieredExpr) {
    if (other.audio_ratio != null && other.audio_ratio !== 1) {
      rows.push({
        label: t('Audio input'),
        value: `${fmtPrice(baseInputUSD * other.audio_ratio)}/M`,
      })
    }

    if (
      other.audio_completion_ratio != null &&
      other.audio_completion_ratio !== 1
    ) {
      rows.push({
        label: t('Audio output'),
        value: `${fmtPrice(baseInputUSD * other.audio_completion_ratio)}/M`,
      })
    }

    if (other.image_ratio != null && other.image_ratio !== 1) {
      rows.push({
        label: t('Image input'),
        value: `${fmtPrice(baseInputUSD * other.image_ratio)}/M`,
      })
    }
  }

  if (other.web_search && other.web_search_call_count) {
    rows.push({
      label: t('Web Search'),
      value: `${other.web_search_call_count}x${other.web_search_price ? ` (${fmtPrice(other.web_search_price)})` : ''}`,
    })
  }

  if (other.file_search && other.file_search_call_count) {
    rows.push({
      label: t('File Search'),
      value: `${other.file_search_call_count}x${other.file_search_price ? ` (${fmtPrice(other.file_search_price)})` : ''}`,
    })
  }

  const imageGenerationPrice =
    other.image_generation_call_total_price ?? other.image_generation_call_price
  if (other.image_generation_call || imageGenerationPrice != null) {
    rows.push({
      label: t('Image Generation'),
      value:
        imageGenerationPrice != null ? fmtPrice(imageGenerationPrice) : '-',
    })
  }

  if (other.audio_input_seperate_price && other.audio_input_price) {
    rows.push({
      label: t('Audio Input Price'),
      value: fmtPrice(other.audio_input_price),
    })
  }

  if (isAdmin && other.admin_info) {
    rows.push({
      label: t('Billing Source'),
      value: other.admin_info.local_count_tokens
        ? t('Local Billing')
        : t('Upstream Response'),
    })
  }

  rows.push({
    label: t('Total Cost'),
    value: formatLogQuota(log.quota),
  })

  if (rows.length === 0) return null

  return (
    <DetailSection label={t('Billing Details')}>
      {rows.map((row, idx) => (
        <DetailRow key={idx} label={row.label} value={row.value} mono />
      ))}
    </DetailSection>
  )
}

function TokenBreakdown(props: { log: UsageLog; other: LogOtherData }) {
  const { t } = useTranslation()
  const { log, other } = props

  const promptTokens = log.prompt_tokens || 0
  const completionTokens = log.completion_tokens || 0
  const cacheRead = other.cache_tokens || 0
  const cacheWrite = other.cache_creation_tokens || 0
  const cacheWrite5m = other.cache_creation_tokens_5m || 0
  const cacheWrite1h = other.cache_creation_tokens_1h || 0
  const hasTokens = promptTokens > 0 || completionTokens > 0

  if (!hasTokens) return null

  const rows: Array<{ label: string; value: string }> = []

  rows.push({ label: t('Input Tokens'), value: promptTokens.toLocaleString() })
  rows.push({
    label: t('Output Tokens'),
    value: completionTokens.toLocaleString(),
  })

  if (cacheRead > 0) {
    rows.push({
      label: t('Cache Read'),
      value: cacheRead.toLocaleString(),
    })
  }

  if (cacheWrite > 0 && cacheWrite5m === 0 && cacheWrite1h === 0) {
    rows.push({
      label: t('Cache Write'),
      value: cacheWrite.toLocaleString(),
    })
  }

  if (cacheWrite5m > 0) {
    rows.push({
      label: t('Cache Write (5m)'),
      value: cacheWrite5m.toLocaleString(),
    })
  }

  if (cacheWrite1h > 0) {
    rows.push({
      label: t('Cache Write (1h)'),
      value: cacheWrite1h.toLocaleString(),
    })
  }

  if (other.image && other.image_output) {
    rows.push({
      label: t('Image Tokens'),
      value: other.image_output.toLocaleString(),
    })
  }

  return (
    <DetailSection label={t('Token Breakdown')}>
      {rows.map((row, idx) => (
        <DetailRow key={idx} label={row.label} value={row.value} mono />
      ))}
    </DetailSection>
  )
}

interface DetailsDialogProps {
  log: UsageLog
  isAdmin: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function DetailsDialog(props: DetailsDialogProps) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  const details = props.log.content ?? ''
  const other = parseLogOther(props.log.other)
  const typeConfig = getLogTypeConfig(props.log.type)

  const isViolation = isViolationFeeLog(other)
  const isRefund = props.log.type === 6
  const isConsume = props.log.type === 2
  const isTopup = props.log.type === 1
  const isManage = props.log.type === 3
  const isSubscription = other?.billing_source === 'subscription'
  const isTieredBilling =
    isConsume &&
    !isViolation &&
    other?.billing_mode === 'tiered_expr' &&
    !!other?.expr_b64
  const hasAudioTokens = other?.ws || other?.audio
  const showTiming = isTimingLogType(props.log.type)
  const showAdminIp =
    !!props.log.ip && (showTiming || (props.isAdmin && isTopup))
  const adminInfo = other?.admin_info
  const topupAuditFields =
    isTopup && props.isAdmin && adminInfo
      ? ([
          adminInfo.payment_method && {
            label: t('Order Payment Method'),
            value: adminInfo.payment_method,
          },
          adminInfo.callback_payment_method && {
            label: t('Callback Payment Method'),
            value: adminInfo.callback_payment_method,
          },
          adminInfo.caller_ip && {
            label: t('Callback Caller IP'),
            value: adminInfo.caller_ip,
          },
          adminInfo.server_ip && {
            label: t('Server IP'),
            value: adminInfo.server_ip,
          },
          adminInfo.node_name && {
            label: t('Node Name'),
            value: adminInfo.node_name,
          },
          adminInfo.version && {
            label: t('System Version'),
            value: adminInfo.version,
          },
        ].filter(Boolean) as Array<{ label: string; value: string }>)
      : []
  const showLegacyTopupWarning = isTopup && props.isAdmin && !adminInfo
  const showTopupAuditSection =
    isTopup &&
    props.isAdmin &&
    (topupAuditFields.length > 0 || showLegacyTopupWarning)
  const manageOperator = (() => {
    if (!isManage || !props.isAdmin || !adminInfo) return null
    const username = adminInfo.admin_username
    const id = adminInfo.admin_id
    const hasUsername = username != null && String(username).trim() !== ''
    const hasId = id != null && String(id).trim() !== ''
    if (!hasUsername && !hasId) return null
    if (hasUsername && hasId) return `${username} (ID: ${id})`
    if (hasUsername) return String(username)
    return `ID: ${id}`
  })()

  const conversionChain =
    other && Array.isArray(other.request_conversion)
      ? other.request_conversion.filter(Boolean)
      : []
  const conversionLabel =
    conversionChain.length <= 1
      ? t('Native format')
      : conversionChain.join(' -> ')
  const showConversion =
    props.isAdmin &&
    props.log.type !== 6 &&
    Boolean(
      other?.request_path ||
      other?.upstream_request_path ||
      conversionChain.length > 0
    )

  const useChannel = other?.admin_info?.use_channel
  const channelChain =
    useChannel && useChannel.length > 0 ? useChannel.join(' -> ') : undefined
  const streamStatusDisplay = other?.stream_status
    ? getStreamStatusDisplayInfo(other.stream_status, t)
    : null
  const frtMs = isDisplayableDurationMs(other?.frt) ? other.frt : null
  const upstreamHeaderMs = isDisplayableDurationMs(other?.upstream_header_ms)
    ? other.upstream_header_ms
    : null
  const upstreamTTFBMs = isDisplayableDurationMs(other?.upstream_ttfb_ms)
    ? other.upstream_ttfb_ms
    : null
  const upstreamTotalMs = isDisplayableDurationMs(other?.upstream_total_ms)
    ? other.upstream_total_ms
    : null
  const hasFRT = props.log.is_stream && frtMs != null && frtMs > 0
  const hasUpstreamTiming =
    upstreamHeaderMs != null ||
    upstreamTTFBMs != null ||
    upstreamTotalMs != null
  const actualModel = getMappedModelName(other)
  const conversionCopyText = [
    other?.request_path ? `${t('Client Path')}: ${other.request_path}` : '',
    other?.upstream_request_path
      ? `${t('Upstream Path')}: ${other.upstream_request_path}`
      : '',
    `${t('Request Conversion')}: ${conversionLabel}`,
  ]
    .filter(Boolean)
    .join('\n')
  const modelInfoRows = buildModelInfoRows({
    log: props.log,
    other,
    conversionChain,
    conversionLabel,
    includeRouting: showConversion,
    t,
  })

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        className={cn(
          'min-w-0 overflow-hidden',
          'max-sm:max-h-[calc(100dvh-1.5rem)] max-sm:w-[calc(100vw-1.5rem)] max-sm:max-w-[calc(100vw-1.5rem)] max-sm:p-4',
          isTieredBilling ? 'sm:max-w-4xl lg:max-w-5xl' : 'sm:max-w-lg'
        )}
      >
        <DialogHeader className='max-sm:gap-1'>
          <DialogTitle className='flex items-center gap-2 text-base'>
            {t('Log Details')}
            <StatusBadge
              label={t(typeConfig.label)}
              variant={typeConfig.color as StatusBadgeProps['variant']}
              size='sm'
              copyable={false}
            />
          </DialogTitle>
          <DialogDescription className='sr-only'>
            {t('View the complete details for this log entry')}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className='max-h-[70vh] min-w-0 overflow-hidden pr-2 max-sm:max-h-[calc(100dvh-7rem)] sm:pr-4'>
          <div className='w-full max-w-full min-w-0 space-y-2.5 overflow-hidden py-1 sm:space-y-3'>
            {/* Overview section - key identifiers */}
            <div className='min-w-0 space-y-1'>
              {props.log.request_id && (
                <DetailRow
                  label={t('Request ID')}
                  value={props.log.request_id}
                  mono
                />
              )}

              {props.log.upstream_request_id && (
                <DetailRow
                  label={t('Upstream Request ID')}
                  value={props.log.upstream_request_id}
                  mono
                />
              )}

              {!props.log.upstream_request_id &&
                other?.upstream_request_id &&
                props.isAdmin && (
                  <DetailRow
                    label={t('Upstream Request ID')}
                    value={other.upstream_request_id}
                    mono
                  />
                )}

              {props.isAdmin && props.log.channel > 0 && (
                <DetailRow
                  label={t('Channel')}
                  value={
                    <span>
                      {props.log.channel}
                      {props.log.channel_name && (
                        <span className='text-muted-foreground'>
                          {' '}
                          ({props.log.channel_name})
                        </span>
                      )}
                    </span>
                  }
                  mono
                />
              )}

              {channelChain && props.isAdmin && (
                <DetailRow label={t('Retry Chain')} value={channelChain} mono />
              )}

              {props.log.token_name && (
                <DetailRow
                  label={t('Token')}
                  value={props.log.token_name}
                  mono
                />
              )}

              {(props.log.group || other?.group) && (
                <DetailRow
                  label={t('Group')}
                  value={props.log.group || other?.group || ''}
                  mono
                />
              )}

              {showAdminIp && (
                <DetailRow
                  label={t('IP Address')}
                  value={
                    <span className='flex items-center gap-1'>
                      <Globe
                        className='size-3 text-amber-500'
                        aria-hidden='true'
                      />
                      {props.log.ip}
                    </span>
                  }
                  mono
                />
              )}

              {showTiming &&
                (props.log.use_time > 0 || hasUpstreamTiming || hasFRT) && (
                  <DetailRow
                    label={t('Response Time')}
                    value={
                      <span className='inline-flex flex-wrap items-center gap-x-2 gap-y-1'>
                        {props.log.use_time > 0 && (
                          <span
                            className={cn(
                              'font-medium',
                              timingTextColorClass(
                                getResponseTimeColor(
                                  props.log.use_time,
                                  props.log.completion_tokens
                                )
                              )
                            )}
                          >
                            {formatUseTime(props.log.use_time)}
                          </span>
                        )}
                        {hasFRT && (
                          <span
                            className={cn(
                              'font-medium',
                              timingTextColorClass(
                                getFirstResponseTimeColor(frtMs / 1000)
                              )
                            )}
                          >
                            FRT: {formatDurationFromMs(frtMs)}
                          </span>
                        )}
                        {hasUpstreamTiming && (
                          <span className='text-muted-foreground text-xs'>
                            {upstreamHeaderMs != null && (
                              <span className='mr-2'>
                                {t('Upstream header')}:{' '}
                                {formatDurationFromMs(upstreamHeaderMs)}
                              </span>
                            )}
                            {upstreamTTFBMs != null && (
                              <span className='mr-2'>
                                {t('Upstream first byte')}:{' '}
                                {formatDurationFromMs(upstreamTTFBMs)}
                              </span>
                            )}
                            {upstreamTotalMs != null && (
                              <span>
                                {t('Upstream total')}:{' '}
                                {formatDurationFromMs(upstreamTotalMs)}
                              </span>
                            )}
                          </span>
                        )}
                      </span>
                    }
                  />
                )}
            </div>

            {/* Request conversion (admin only, not for refund) */}
            {showConversion && (
              <DetailSection label={t('Request Conversion')}>
                <div className='relative min-w-0'>
                  <Button
                    variant='ghost'
                    size='sm'
                    className='absolute top-0 right-0 h-5 w-5 p-0'
                    onClick={() => copyToClipboard(conversionCopyText)}
                    title={t('Copy to clipboard')}
                    aria-label={t('Copy to clipboard')}
                  >
                    {copiedText === conversionCopyText ? (
                      <Check className='size-3 text-green-600' />
                    ) : (
                      <Copy className='size-3' />
                    )}
                  </Button>
                  <div className='min-w-0 space-y-1 pr-6'>
                    {other?.request_path && (
                      <DetailRow
                        label={t('Client Path')}
                        value={other.request_path}
                        mono
                      />
                    )}
                    {other?.upstream_request_path && (
                      <DetailRow
                        label={t('Upstream Path')}
                        value={other.upstream_request_path}
                        mono
                      />
                    )}
                    <div className='flex min-w-0 items-center gap-1.5 text-xs'>
                      <Route
                        className='text-muted-foreground size-3'
                        aria-hidden='true'
                      />
                      <span className='min-w-0 break-all sm:break-words'>
                        {conversionLabel}
                      </span>
                    </div>
                  </div>
                </div>
              </DetailSection>
            )}

            {props.isAdmin && other && getRequestHeaderPolicy(other) && (
              <RequestHeaderPolicyDetails other={other} />
            )}

            {(other?.responses_compact_mode ||
              other?.responses_encrypted_context_retry ||
              other?.channel_capability_snapshot) && (
              <ResponsesCompactDetails other={other} isAdmin={props.isAdmin} />
            )}

            {/* Reject reason (admin only) */}
            {props.isAdmin && other?.reject_reason && (
              <DetailSection
                icon={<AlertTriangle className='size-3.5' aria-hidden='true' />}
                label={t('Reject Reason')}
                variant='danger'
              >
                <p className='text-xs break-words'>{other.reject_reason}</p>
              </DetailSection>
            )}

            {/* Violation fee info */}
            {isViolation && other && (
              <DetailSection
                icon={<AlertTriangle className='size-3.5' aria-hidden='true' />}
                label={t('Violation Fee')}
                variant='danger'
              >
                {other.violation_fee_code && (
                  <DetailRow
                    label={t('Violation Code')}
                    value={other.violation_fee_code}
                    mono
                  />
                )}
                {other.violation_fee_marker && (
                  <DetailRow
                    label={t('Violation Marker')}
                    value={other.violation_fee_marker}
                  />
                )}
                <DetailRow
                  label={t('Fee Amount')}
                  value={formatLogQuota(other.fee_quota ?? props.log.quota)}
                  mono
                />
              </DetailSection>
            )}

            {/* Refund details (type=6) */}
            {isRefund && other && (other.task_id || other.reason) && (
              <DetailSection label={t('Refund Details')}>
                {other.task_id && (
                  <DetailRow label={t('Task ID')} value={other.task_id} mono />
                )}
                {other.reason && (
                  <DetailRow label={t('Reason')} value={other.reason} />
                )}
              </DetailSection>
            )}

            {/* Top-up audit info (type=1, admin only) */}
            {showTopupAuditSection && (
              <DetailSection
                icon={<ShieldCheck className='size-3.5' aria-hidden='true' />}
                label={t('Top-up Audit Info')}
              >
                {topupAuditFields.map((field, idx) => (
                  <DetailRow
                    key={idx}
                    label={field.label}
                    value={field.value}
                    mono
                  />
                ))}
                {showLegacyTopupWarning && (
                  <div className='flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-400'>
                    <Info
                      className='mt-0.5 size-3.5 shrink-0'
                      aria-hidden='true'
                    />
                    <span>
                      {t(
                        'This record was written by a pre-upgrade instance and lacks audit info. Upgrade the instance to record server IP, callback IP, payment method and system version.'
                      )}
                    </span>
                  </div>
                )}
              </DetailSection>
            )}

            {/* Manage operator (type=3, admin only) */}
            {manageOperator && (
              <DetailRow
                label={
                  <span className='flex items-center gap-1.5'>
                    <UserCog
                      className='text-muted-foreground size-3.5'
                      aria-hidden='true'
                    />
                    {t('Operator Admin')}
                  </span>
                }
                value={manageOperator}
                mono
              />
            )}

            {/* Audio/WebSocket token breakdown */}
            {hasAudioTokens && other && (
              <DetailSection
                icon={<Headphones className='size-3.5' aria-hidden='true' />}
                label={t('Audio Tokens')}
              >
                {other.audio_input != null && other.audio_input > 0 && (
                  <DetailRow
                    label={t('Audio Input')}
                    value={formatTokens(other.audio_input)}
                    mono
                  />
                )}
                {other.audio_output != null && other.audio_output > 0 && (
                  <DetailRow
                    label={t('Audio Output')}
                    value={formatTokens(other.audio_output)}
                    mono
                  />
                )}
                {other.text_input != null && other.text_input > 0 && (
                  <DetailRow
                    label={t('Text Input')}
                    value={formatTokens(other.text_input)}
                    mono
                  />
                )}
                {other.text_output != null && other.text_output > 0 && (
                  <DetailRow
                    label={t('Text Output')}
                    value={formatTokens(other.text_output)}
                    mono
                  />
                )}
              </DetailSection>
            )}

            {/* Reasoning effort */}
            {other?.reasoning_effort && (
              <DetailRow
                label={t('Reasoning Effort')}
                value={
                  <StatusBadge
                    label={other.reasoning_effort}
                    variant={
                      other.reasoning_effort === 'high'
                        ? 'orange'
                        : other.reasoning_effort === 'medium'
                          ? 'yellow'
                          : 'green'
                    }
                    size='sm'
                    copyable={false}
                  />
                }
              />
            )}

            {/* System prompt override */}
            {other?.is_system_prompt_overwritten && (
              <DetailRow
                label={t('System Prompt')}
                value={
                  <StatusBadge
                    label={t('Overwritten')}
                    variant='orange'
                    size='sm'
                    copyable={false}
                  />
                }
              />
            )}

            {/* Model */}
            {isDisplayableLogType(props.log.type) && props.log.model_name && (
              <DetailSection label={t('Model')}>
                <div className='flex min-w-0 flex-wrap items-center gap-1.5'>
                  <ModelBadge
                    modelName={props.log.model_name}
                    actualModel={actualModel}
                    infoRows={modelInfoRows}
                    className='max-w-full'
                  />
                </div>
              </DetailSection>
            )}

            {/* Token breakdown (for consume/error types with token data) */}
            {isDisplayableLogType(props.log.type) && other && (
              <TokenBreakdown log={props.log} other={other} />
            )}

            {/* Billing breakdown (consume type) */}
            {isConsume && other && !isViolation && (
              <BillingBreakdown
                log={props.log}
                other={other}
                isAdmin={props.isAdmin}
              />
            )}

            {/* Tiered pricing breakdown (when billing_mode is tiered_expr) */}
            {isTieredBilling && other?.expr_b64 && (
              <div className='bg-muted/30 min-w-0 overflow-hidden rounded-md border px-3 max-sm:px-2'>
                <DynamicPricingBreakdown
                  billingExpr={decodeBillingExprB64(other.expr_b64)}
                  matchedTierLabel={other.matched_tier}
                  hideCacheColumns={!hasAnyCacheTokens(other)}
                />
              </div>
            )}

            {/* Admin billing mode indicator for non-consume */}
            {props.isAdmin &&
              !isConsume &&
              props.log.type !== 6 &&
              other?.admin_info && (
                <DetailRow
                  label={t('Billing Source')}
                  value={
                    <span className='flex items-center gap-1'>
                      {other.admin_info.local_count_tokens ? (
                        <Monitor className='size-3 text-blue-500' />
                      ) : (
                        <Cloud className='size-3 text-emerald-500' />
                      )}
                      <span className='text-xs'>
                        {other.admin_info.local_count_tokens
                          ? t('Local Billing')
                          : t('Upstream Response')}
                      </span>
                    </span>
                  }
                />
              )}

            {/* Stream status details (admin only) */}
            {props.isAdmin &&
              other?.stream_status &&
              other.stream_status.status !== 'ok' && (
                <DetailSection label={t('Stream Status')}>
                  <DetailRow
                    label={t('Status')}
                    value={
                      <StatusBadge
                        label={streamStatusDisplay?.label || t('Error')}
                        variant={streamStatusDisplay?.variant || 'red'}
                        size='sm'
                        copyable={false}
                      />
                    }
                  />
                  {other.stream_status.end_reason && (
                    <DetailRow
                      label={t('End reason')}
                      value={other.stream_status.end_reason}
                    />
                  )}
                  {(other.stream_status.error_count ?? 0) > 0 && (
                    <DetailRow
                      label={t('Soft Errors')}
                      value={String(other.stream_status.error_count)}
                    />
                  )}
                  {other.stream_status.end_error && (
                    <DetailRow
                      label={t('End Error')}
                      value={other.stream_status.end_error}
                    />
                  )}
                  {Array.isArray(other.stream_status.errors) &&
                    other.stream_status.errors.length > 0 && (
                      <pre className='bg-background/60 mt-1 max-h-32 overflow-y-auto rounded border p-2 font-mono text-[11px] leading-relaxed break-words whitespace-pre-wrap'>
                        {other.stream_status.errors.join('\n')}
                      </pre>
                    )}
                </DetailSection>
              )}

            {/* Subscription billing details */}
            {isSubscription && other && (
              <DetailSection label={t('Subscription Billing')}>
                {other.subscription_plan_id && (
                  <DetailRow
                    label={t('Plan')}
                    value={`#${other.subscription_plan_id} ${other.subscription_plan_title || ''}`.trim()}
                  />
                )}
                {other.subscription_id && (
                  <DetailRow
                    label={t('Instance')}
                    value={`#${other.subscription_id}`}
                    mono
                  />
                )}
                {other.subscription_pre_consumed != null && (
                  <DetailRow
                    label={t('Pre-consumed')}
                    value={formatLogQuota(other.subscription_pre_consumed)}
                    mono
                  />
                )}
                {other.subscription_post_delta != null &&
                  other.subscription_post_delta !== 0 && (
                    <DetailRow
                      label={t('Post Delta')}
                      value={formatLogQuota(other.subscription_post_delta)}
                      mono
                    />
                  )}
                {other.subscription_consumed != null && (
                  <DetailRow
                    label={t('Final Consumed')}
                    value={formatLogQuota(other.subscription_consumed)}
                    mono
                  />
                )}
                {other.subscription_remain != null && (
                  <DetailRow
                    label={t('Remaining')}
                    value={`${formatLogQuota(other.subscription_remain)}${other.subscription_total != null ? ` / ${formatLogQuota(other.subscription_total)}` : ''}`}
                    mono
                  />
                )}
              </DetailSection>
            )}

            {/* Param override (admin only) */}
            {props.isAdmin &&
              other?.po &&
              Array.isArray(other.po) &&
              other.po.length > 0 && (
                <DetailSection
                  icon={<Settings2 className='size-3.5' aria-hidden='true' />}
                  label={`${t('Param Override')} (${other.po.length})`}
                >
                  {other.po.filter(Boolean).map((line, idx) => {
                    const parsed = parseAuditLine(line)
                    if (!parsed) return null
                    return (
                      <div
                        key={idx}
                        className='bg-background/60 flex min-w-0 flex-col gap-1.5 rounded border p-2 sm:flex-row sm:items-start sm:gap-2'
                      >
                        <StatusBadge
                          variant='neutral'
                          label={getParamOverrideActionLabel(parsed.action, t)}
                          className='shrink-0 font-medium'
                          copyable={false}
                        />
                        <span className='min-w-0 font-mono text-[11px] leading-relaxed break-all sm:break-words'>
                          {parsed.content}
                        </span>
                      </div>
                    )
                  })}
                </DetailSection>
              )}

            {/* Content */}
            {details && (
              <div className='space-y-1.5'>
                <Label className='text-xs font-semibold'>{t('Content')}</Label>
                <div className='bg-muted/30 relative min-w-0 overflow-hidden rounded-md border p-2.5'>
                  <Button
                    variant='ghost'
                    size='sm'
                    className='absolute top-1.5 right-1.5 h-5 w-5 p-0'
                    onClick={() => copyToClipboard(details)}
                    title={t('Copy to clipboard')}
                    aria-label={t('Copy to clipboard')}
                  >
                    {copiedText === details ? (
                      <Check className='size-3 text-green-600' />
                    ) : (
                      <Copy className='size-3' />
                    )}
                  </Button>
                  <p className='min-w-0 pr-6 text-xs leading-relaxed break-all whitespace-pre-wrap sm:break-words'>
                    {details}
                  </p>
                </div>
              </div>
            )}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
