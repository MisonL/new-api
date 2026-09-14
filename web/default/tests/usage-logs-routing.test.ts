import { describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import {
  getResponsesRoutingBadgeKind,
  getResponsesRoutingChannelTrace,
  getResponsesRoutingEventDisplay,
  getResponsesRoutingEvents,
  getResponsesRoutingFinalPath,
  getResponsesRoutingResult,
  getResponsesRoutingTrace,
  hasResponsesRoutingChannelTrace,
} from '../src/features/usage-logs/lib/responses-routing'
import { shouldIncludeResponsesRoutingColumn } from '../src/features/usage-logs/lib/responses-routing-trace'
import {
  getChannelHealthRoutingEventDisplay,
  getChannelHealthRoutingEventImpactKey,
  getChannelHealthRoutingEvents,
  getChannelHealthRoutingResult,
  getChannelHealthRoutingResultImpactKey,
  shouldIncludeChannelHealthRoutingColumn,
} from '../src/features/usage-logs/lib/channel-health-routing'

function interpolateTranslation(
  key: string,
  options?: Record<string, unknown>
): string {
  return key.replace(/\{\{(\w+)\}\}/g, (_, name) =>
    String(options?.[name] ?? '')
  )
}

function getResponsesRoutingTranslationKeys(): string[] {
  const files = [
    '../src/features/usage-logs/components/responses-routing-badge.tsx',
    '../src/features/usage-logs/components/responses-routing-details.tsx',
    '../src/features/usage-logs/components/responses-routing-trace.tsx',
    '../src/features/usage-logs/lib/responses-routing.ts',
    '../src/features/usage-logs/lib/responses-routing-trace.ts',
  ]
  const keys = new Set<string>()

  for (const file of files) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8')
    for (const match of source.matchAll(/\bt\(\s*['"]([^'"\n]+)['"]/g)) {
      keys.add(match[1])
    }
  }

  return [...keys]
}

function getMissingResponsesRoutingTranslations(): string[] {
  const keys = getResponsesRoutingTranslationKeys()
  const locales = ['en', 'zh', 'fr', 'ja', 'ru', 'vi']
  const missing: string[] = []

  for (const locale of locales) {
    const file = new URL(`../src/i18n/locales/${locale}.json`, import.meta.url)
    const { translation } = JSON.parse(readFileSync(file, 'utf8')) as {
      translation: Record<string, string | undefined>
    }

    for (const key of keys) {
      const variants = [
        key,
        `${key}_zero`,
        `${key}_one`,
        `${key}_two`,
        `${key}_few`,
        `${key}_many`,
        `${key}_other`,
      ]
      const existingVariants = variants.filter((variant) =>
        Object.prototype.hasOwnProperty.call(translation, variant)
      )
      const effectiveVariants =
        existingVariants.length > 0 ? existingVariants : [key]
      const hasBlankVariant = effectiveVariants.some(
        (variant) =>
          typeof translation[variant] !== 'string' ||
          translation[variant]?.trim() === ''
      )
      if (hasBlankVariant) {
        missing.push(`${locale}:${key}`)
      }
    }
  }

  return missing
}

function getChannelHealthRoutingTranslationKeys(): string[] {
  const files = [
    '../src/features/usage-logs/components/channel-health-routing-badge.tsx',
    '../src/features/usage-logs/components/channel-health-routing-details.tsx',
    '../src/features/usage-logs/lib/channel-health-routing.ts',
  ]
  const keys = new Set<string>()

  for (const file of files) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8')
    for (const match of source.matchAll(/\bt\(\s*['"]([^'"\n]+)['"]/g)) {
      keys.add(match[1])
    }
  }

  return [
    ...keys,
    'Health routing decision',
    'Routing impact',
    'Routing scope: {{group}} / {{model}}',
    'A qualifying failure started or renewed cooldown for matching requests.',
    'This request owns the recovery probe. Competing matching requests bypass the channel until it finishes.',
    'This request bypassed the channel because another request owns its recovery probe.',
    'The recovery probe succeeded and cleared the temporary exclusion for matching requests.',
    'A newer qualifying failure kept the temporary exclusion, so this probe did not restore the channel.',
    'This request released the recovery probe without changing the prior failure history.',
    'Channel affinity was bypassed because the selected channel had an active health exclusion.',
    'No active temporary health exclusion affected this channel at selection time.',
    'Health filtering was unavailable, so routing continued without temporary health exclusions.',
    'This health-routing action has no defined request impact.',
    'A newer qualifying failure kept the temporary exclusion, so the prior recovery probe did not restore the channel.',
    'This request bypassed a channel while another matching request owned its recovery probe.',
    'A successful recovery probe cleared the temporary exclusion for matching requests.',
    'This request owns a recovery probe while competing matching requests bypass the channel.',
    'The recovery probe was released without changing the prior failure history.',
    'No active temporary health exclusion affected the selected channel at this decision point.',
    'A health-routing action was recorded for this request.',
  ]
}

function getMissingChannelHealthRoutingTranslations(): string[] {
  const keys = getChannelHealthRoutingTranslationKeys()
  const locales = ['en', 'zh', 'fr', 'ja', 'ru', 'vi']
  const missing: string[] = []

  for (const locale of locales) {
    const file = new URL(`../src/i18n/locales/${locale}.json`, import.meta.url)
    const { translation } = JSON.parse(readFileSync(file, 'utf8')) as {
      translation: Record<string, string | undefined>
    }

    for (const key of keys) {
      const variants = [
        key,
        `${key}_zero`,
        `${key}_one`,
        `${key}_two`,
        `${key}_few`,
        `${key}_many`,
        `${key}_other`,
      ]
      const existingVariants = variants.filter((variant) =>
        Object.prototype.hasOwnProperty.call(translation, variant)
      )
      const effectiveVariants =
        existingVariants.length > 0 ? existingVariants : [key]
      const hasBlankVariant = effectiveVariants.some(
        (variant) =>
          typeof translation[variant] !== 'string' ||
          translation[variant]?.trim() === ''
      )
      if (hasBlankVariant) {
        missing.push(`${locale}:${key}`)
      }
    }
  }

  return missing
}

describe('usage log Responses routing events', () => {
  test('localizes every Responses routing diagnostic in each runtime locale', () => {
    expect(getMissingResponsesRoutingTranslations()).toEqual([])
  })

  test('limits Responses routing diagnostics to administrators', () => {
    expect(shouldIncludeResponsesRoutingColumn(true)).toBe(true)
    expect(shouldIncludeResponsesRoutingColumn(false)).toBe(false)
  })

  test('localizes every channel health routing diagnostic in each runtime locale', () => {
    expect(getMissingChannelHealthRoutingTranslations()).toEqual([])
  })

  test('limits channel health routing diagnostics to administrators', () => {
    expect(shouldIncludeChannelHealthRoutingColumn(true)).toBe(true)
    expect(shouldIncludeChannelHealthRoutingColumn(false)).toBe(false)
  })

  test('renders channel health cooldown and redis fail-open without changing route semantics', () => {
    const other = {
      admin_info: {
        channel_health: {
          redis_unavailable: true,
          events: [
            {
              group: 'coding',
              model_name: 'gpt-5.6-sol',
              channel_id: 160,
              action: 'cooldown',
              status_code: 502,
              error_code: 'bad_response_status_code',
            },
            {
              channel_id: 0,
              action: 'redis_unavailable',
            },
          ],
        },
      },
    }

    const events = getChannelHealthRoutingEvents(other)
    expect(events).toEqual([
      {
        group: 'coding',
        modelName: 'gpt-5.6-sol',
        channelId: 160,
        action: 'cooldown',
        statusCode: 502,
        errorCode: 'bad_response_status_code',
      },
      {
        group: undefined,
        modelName: undefined,
        channelId: 0,
        action: 'redis_unavailable',
        statusCode: undefined,
        errorCode: undefined,
      },
    ])
    expect(getChannelHealthRoutingResult(other)).toEqual({
      kind: 'gate_unavailable',
      actionCount: 2,
    })
    expect(
      getChannelHealthRoutingEventDisplay(events[0], interpolateTranslation)
    ).toEqual({
      key: 'cooldown',
      label: 'Channel Cooling',
      value:
        'Channel #160; Routing scope: coding / gpt-5.6-sol; Status: 502; Error: bad_response_status_code',
    })
    expect(
      getChannelHealthRoutingEventDisplay(events[1], interpolateTranslation)
    ).toEqual({
      key: 'redis_unavailable',
      label: 'Health Gate Unavailable',
      value: 'Redis unavailable; routing continued',
    })
  })

  test('deduplicates channel health redis unavailable markers from flag and event', () => {
    expect(
      getChannelHealthRoutingEvents({
        admin_info: {
          channel_health: {
            redis_unavailable: true,
            events: [],
          },
        },
      })
    ).toEqual([
      {
        group: undefined,
        modelName: undefined,
        channelId: 0,
        action: 'redis_unavailable',
        statusCode: undefined,
        errorCode: undefined,
      },
    ])
  })

  test('shows a healthy gate pass without claiming that the upstream call succeeded', () => {
    const other = {
      admin_info: {
        channel_health: {
          events: [
            {
              group: 'coding',
              model_name: 'gpt-5.6-sol',
              channel_id: 206,
              action: 'healthy',
            },
          ],
        },
      },
    }

    const events = getChannelHealthRoutingEvents(other)
    expect(events).toEqual([
      {
        group: 'coding',
        modelName: 'gpt-5.6-sol',
        channelId: 206,
        action: 'healthy',
        statusCode: undefined,
        errorCode: undefined,
      },
    ])
    expect(getChannelHealthRoutingResult(other)).toEqual({
      kind: 'healthy',
      actionCount: 1,
    })
    expect(
      getChannelHealthRoutingEventDisplay(events[0], interpolateTranslation)
    ).toEqual({
      key: 'healthy',
      label: 'Health Gate Passed',
      value:
        'Channel #206; No active cooldown or probe state; Routing scope: coding / gpt-5.6-sol',
    })
  })

  test('makes the request-level routing impact explicit for cooldown, probes, and fail-open', () => {
    expect(
      getChannelHealthRoutingEventImpactKey({
        channelId: 206,
        action: 'cooldown',
        statusCode: 502,
      })
    ).toBe(
      'A qualifying failure started or renewed cooldown for matching requests.'
    )
    expect(
      getChannelHealthRoutingEventImpactKey({
        channelId: 206,
        action: 'half_open_busy',
      })
    ).toBe(
      'This request bypassed the channel because another request owns its recovery probe.'
    )
    expect(
      getChannelHealthRoutingResultImpactKey({
        kind: 'gate_unavailable',
        actionCount: 1,
      })
    ).toBe(
      'Health filtering was unavailable, so routing continued without temporary health exclusions.'
    )
  })

  test('renders structured channel preflight skips without duplicating legacy compact skips', () => {
    expect(
      getResponsesRoutingEvents({
        responses_compact_channel_skip: 'channel_skipped_unsupported_compaction',
        responses_routing_channel_skips: [
          { channel_id: 2, reason: 'unsupported_custom_tools' },
          { channel_id: 2, reason: 'unsupported_custom_tools' },
          { channel_id: 168, reason: 'unsupported_namespace_tools' },
        ],
      })
    ).toEqual([
      { code: 'channel_skip', value: '#2: unsupported_custom_tools' },
      { code: 'channel_skip', value: '#168: unsupported_namespace_tools' },
    ])
  })

  test('keeps the legacy compaction skip visible for existing logs', () => {
    expect(
      getResponsesRoutingEvents({
        responses_compact_channel_skip: 'channel_skipped_unsupported_compaction',
      })
    ).toEqual([
      {
        code: 'compact_channel_skip',
        value: 'channel_skipped_unsupported_compaction',
      },
    ])
  })

  test('shows the first committed event when encrypted context retry is blocked', () => {
    expect(
      getResponsesRoutingEvents({
        responses_encrypted_context_retry_blocked_after_event:
          'response.output_item.added',
      })
    ).toEqual([
      {
        code: 'encrypted_context_retry_blocked_after_event',
        value: 'response.output_item.added',
      },
    ])
  })

  test('makes gateway compaction and input ID normalization visible without request content', () => {
    const other = {
      responses_codex_local_compaction_ingress_reduced: true,
      responses_codex_local_compaction_body_reduction_source:
        'ingress_preemptive_size',
      responses_codex_local_compaction_ingress_original_body_bytes: 34603607,
      responses_codex_local_compaction_ingress_reduced_body_bytes: 1292,
      responses_codex_local_compaction_ingress_input_items: 4,
      responses_input_item_id_normalization_removed_count: 2,
    }

    const events = getResponsesRoutingEvents(other)
    expect(events).toEqual([
      {
        code: 'local_gateway_body_reduced',
        source: 'ingress_preemptive_size',
        originalBodyBytes: 34603607,
        reducedBodyBytes: 1292,
        inputItems: 4,
      },
      { code: 'input_item_ids_normalized', removedCount: 2 },
    ])
    expect(getResponsesRoutingBadgeKind(other)).toBe('gateway_compacted')
    expect(
      getResponsesRoutingEventDisplay(events[0], interpolateTranslation)
    ).toEqual({
      key: 'local_gateway_body_reduced',
      label: 'Gateway Compacted',
      value:
        'Preemptive size protection: Request size: 33.0 MiB -> 1.3 KiB; Input items: 4',
    })
  })

  test('prioritizes route recovery over a normal gateway compaction marker', () => {
    expect(
      getResponsesRoutingBadgeKind({
        responses_codex_local_compaction_ingress_reduced: true,
        responses_codex_local_compaction_413_recovery: true,
      })
    ).toBe('route_recovery')
  })

  test('shows upstream 413 recovery with sanitized body reduction metrics', () => {
    const other = {
      responses_codex_local_compaction_413_recovery: true,
      responses_codex_local_compaction_body_reduction_source: 'upstream_413',
      responses_codex_local_compaction_original_body_bytes: 34603607,
      responses_codex_local_compaction_reduced_body_bytes: 1292,
    }

    const events = getResponsesRoutingEvents(other)
    expect(events).toEqual([
      {
        code: 'local_gateway_body_reduced',
        source: 'upstream_413',
        originalBodyBytes: 34603607,
        reducedBodyBytes: 1292,
        inputItems: undefined,
      },
      { code: 'local_413_recovery' },
    ])
    expect(getResponsesRoutingBadgeKind(other)).toBe('route_recovery')
    expect(
      getResponsesRoutingEventDisplay(events[0], interpolateTranslation)
    ).toEqual({
      key: 'local_gateway_body_reduced',
      label: 'Gateway Compacted',
      value: 'Upstream 413 recovery: Request size: 33.0 MiB -> 1.3 KiB',
    })
  })

  test('distinguishes a failed local 413 recovery from a successful retry', () => {
    const events = getResponsesRoutingEvents({
      responses_codex_local_compaction_413_recovery: true,
      responses_codex_local_compaction_413_recovery_failed: true,
    })

    expect(events).toEqual([{ code: 'local_413_recovery_failed' }])
    expect(
      getResponsesRoutingBadgeKind({
        responses_codex_local_compaction_413_recovery: true,
        responses_codex_local_compaction_413_recovery_failed: true,
      })
    ).toBe('route_failure')
    expect(
      getResponsesRoutingEventDisplay(events[0], interpolateTranslation)
    ).toEqual({
      key: 'local_413_recovery_failed',
      label: 'Local 413 Recovery',
      value: 'Recovery failed after compaction',
    })
  })

  test('makes compact fallback decisions and Codex compaction metadata traceable', () => {
    const other = {
      responses_compact_context_fallback: true,
      responses_compact_previous_response_id_fallback: true,
      responses_compact_summary_model_fallback: true,
      responses_compact_summary_model: 'gpt-5.6-sol',
      responses_compact_summary_models: ['gpt-5.6-sol', 'gpt-5.6-terra'],
      responses_compact_native_attempted: true,
      responses_compact_native_upstream_path: '/v1/responses/compact',
      responses_compact_native_status_code: 413,
      responses_compact_auto_fallback_window: true,
      responses_compact_auto_fallback_reason: 'upstream payload limit',
      responses_compact_auto_fallback_retry_interval_hours: 6,
      responses_compact_auto_fallback_retry_until: 1784816400,
      responses_compact_client: 'codex',
      responses_compact_client_implementation: 'responses_compaction_v2',
      responses_compact_trigger: 'token_budget',
      responses_compact_reason: 'context_limit',
      responses_compact_phase: 'preflight',
      responses_compact_strategy: 'summary',
    }

    const events = getResponsesRoutingEvents(other)
    expect(events).toEqual([
      { code: 'compact_context_fallback' },
      { code: 'compact_previous_response_id_fallback' },
      { code: 'compact_summary_model_fallback' },
      { code: 'compact_summary_model', value: 'gpt-5.6-sol' },
      {
        code: 'compact_summary_models',
        value: 'gpt-5.6-sol, gpt-5.6-terra',
      },
      {
        code: 'compact_native_attempt',
        upstreamPath: '/v1/responses/compact',
        statusCode: 413,
      },
      {
        code: 'compact_auto_fallback_window',
        fallbackReason: 'upstream payload limit',
        retryUntilUnix: 1784816400,
        retryIntervalHours: 6,
      },
      {
        code: 'codex_compaction',
        client: 'codex',
        implementation: 'responses_compaction_v2',
        trigger: 'token_budget',
        reason: 'context_limit',
        phase: 'preflight',
        strategy: 'summary',
      },
    ])
    expect(getResponsesRoutingResult(other)).toEqual({
      kind: 'route_recovery',
      actionCount: 8,
    })
    expect(
      getResponsesRoutingEventDisplay(events[0], interpolateTranslation)
    ).toEqual({
      key: 'compact_context_fallback',
      label: 'Compaction Context Fallback',
      value: 'Applied',
    })
    expect(
      getResponsesRoutingEventDisplay(events[5], interpolateTranslation)
    ).toEqual({
      key: 'compact_native_attempt',
      label: 'Native Compaction Attempt',
      value: 'Status: 413; Path: /v1/responses/compact',
    })
    expect(
      getResponsesRoutingEventDisplay(events[7], interpolateTranslation)
    ).toEqual({
      key: 'codex_compaction',
      label: 'Codex Compaction',
      value:
        'Implementation: responses_compaction_v2; Trigger: token_budget; Reason: context_limit; Phase: preflight; Strategy: summary',
    })
  })

  test('orders the execution trace by gateway processing stage and keeps the final path evidence-based', () => {
    const other = {
      responses_codex_local_compaction_ingress_reduced: true,
      responses_codex_local_compaction_body_reduction_source:
        'ingress_preemptive_size',
      responses_codex_local_compaction_ingress_original_body_bytes: 2048,
      responses_codex_local_compaction_ingress_reduced_body_bytes: 1024,
      responses_reasoning_context_action: 'stripped_unsupported',
      responses_compact_route_decision: 'synthetic_summary',
      responses_routing_channel_skips: [
        { channel_id: 2, reason: 'unsupported_custom_tools' },
      ],
      responses_compact_native_attempted: true,
      responses_compact_native_upstream_path: '/v1/responses/compact',
      responses_compact_native_status_code: 502,
      responses_encrypted_context_retry: true,
      responses_compact_state_restored: true,
      responses_compact_final_upstream_path: '/v1/responses',
      upstream_request_path: '/v1/unused-fallback',
    }

    expect(
      getResponsesRoutingTrace(other).map((item) => [
        item.stage,
        item.event.code,
      ])
    ).toEqual([
      ['request_preparation', 'local_gateway_body_reduced'],
      ['request_preparation', 'reasoning_context_action'],
      ['route_selection', 'compact_route_decision'],
      ['route_selection', 'channel_skip'],
      ['upstream_attempt', 'compact_native_attempt'],
      ['recovery', 'encrypted_context_retry'],
      ['recovery', 'compact_state_restored'],
    ])
    expect(getResponsesRoutingFinalPath(other)).toBe('/v1/responses')
    expect(
      getResponsesRoutingFinalPath({
        responses_compact_final_upstream_path: ' ',
        upstream_request_path: ' /v1/responses ',
      })
    ).toBe('/v1/responses')
    expect(getResponsesRoutingFinalPath({})).toBeUndefined()
  })

  test('summarizes the final routing result without inventing unrecorded actions', () => {
    expect(getResponsesRoutingResult(undefined)).toBeNull()
    expect(getResponsesRoutingResult({})).toBeNull()

    expect(
      getResponsesRoutingResult({
        channel_capability_snapshot: {
          compact_mode_effective: 'native',
        },
      })
    ).toEqual({ kind: 'capability_observed', actionCount: 0 })

    expect(
      getResponsesRoutingResult({ responses_compact_mode: 'native' })
    ).toEqual({ kind: 'native_passthrough', actionCount: 0 })

    expect(
      getResponsesRoutingResult({ responses_compact_mode: 'synthetic_summary' })
    ).toEqual({ kind: 'synthetic_compaction', actionCount: 0 })

    expect(
      getResponsesRoutingResult({
        responses_reasoning_context_action: 'stripped_unsupported',
      })
    ).toEqual({ kind: 'request_adapted', actionCount: 1 })

    expect(
      getResponsesRoutingResult({
        responses_codex_local_compaction_413_recovery: true,
        responses_codex_local_compaction_413_recovery_failed: true,
      })
    ).toEqual({ kind: 'route_failure', actionCount: 1 })
  })

  test('uses the admin channel trace for an automatic channel reroute', () => {
    const other = {
      responses_routing_trace: {
        candidates: [117, 205, 205],
        preflight_skipped: [
          { channel_id: 117, reason: 'unsupported_namespace_tools' },
        ],
        attempted_channels: [
          {
            channel_id: 205,
            outcome: 'success',
          },
        ],
        final_channel: 205,
        terminal_reason: 'success',
      },
    }

    const trace = getResponsesRoutingChannelTrace(other)
    expect(trace).toEqual({
      candidates: [117, 205],
      preflightSkipped: [
        { channel_id: 117, reason: 'unsupported_namespace_tools' },
      ],
      attemptedChannels: [
        {
          channel_id: 205,
          outcome: 'success',
          status_code: undefined,
          error_code: undefined,
          error_category: undefined,
        },
      ],
      finalChannel: 205,
      terminalReason: 'success',
    })
    expect(hasResponsesRoutingChannelTrace(trace)).toBe(true)
    expect(getResponsesRoutingBadgeKind(other)).toBe('route_recovery')
    expect(getResponsesRoutingResult(other)).toEqual({
      kind: 'route_recovery',
      actionCount: 2,
    })
  })

  test('does not surface an ordinary single-channel success as a reroute', () => {
    const other = {
      responses_routing_trace: {
        candidates: [205],
        attempted_channels: [{ channel_id: 205, outcome: 'success' }],
        final_channel: 205,
        terminal_reason: 'success',
      },
    }
    expect(getResponsesRoutingChannelTrace(other)).not.toBeNull()
    expect(getResponsesRoutingResult(other)).toBeNull()
  })
})
