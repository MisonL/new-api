/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

import {
  getResponsesRoutingBadgeKind,
  getResponsesRoutingChannelTrace,
  getResponsesRoutingEventDisplay,
  getResponsesRoutingEvents,
  getResponsesRoutingFinalPath,
  getResponsesRoutingResult,
  getResponsesRoutingResultDisplay,
  getResponsesRoutingTrace,
  hasResponsesRoutingChannelTrace,
  shouldRenderResponsesCompactTag,
} from '../src/helpers/usageLogsResponsesCompact.js';
import { shouldIncludeResponsesRoutingColumn } from '../src/helpers/usageLogsResponsesRoutingTrace.js';
import {
  getChannelHealthRoutingEventDisplay,
  getChannelHealthRoutingEvents,
  getChannelHealthRoutingResult,
  shouldIncludeChannelHealthRoutingColumn,
} from '../src/helpers/usageLogsChannelHealthRouting.js';

function translate(key, options = {}) {
  return key.replace(/\{\{(\w+)\}\}/g, (_, name) =>
    String(options[name] ?? ''),
  );
}

function getResponsesRoutingTranslationKeys() {
  const files = [
    new URL(
      '../src/components/table/usage-logs/ResponsesRoutingTrace.jsx',
      import.meta.url,
    ),
    new URL('../src/helpers/usageLogsResponsesCompact.js', import.meta.url),
    new URL(
      '../src/helpers/usageLogsResponsesRoutingTrace.js',
      import.meta.url,
    ),
  ];
  const keys = new Set();

  for (const file of files) {
    const source = readFileSync(file, 'utf8');
    for (const match of source.matchAll(/\bt\(\s*['"]([^'"\n]+)['"]/g)) {
      keys.add(match[1]);
    }
  }

  return [...keys];
}

function getMissingResponsesRoutingTranslations() {
  const keys = getResponsesRoutingTranslationKeys();
  const locales = ['zh-CN', 'zh-TW', 'en', 'fr', 'ru', 'ja', 'vi'];
  const missing = [];

  for (const locale of locales) {
    const file = new URL(`../src/i18n/locales/${locale}.json`, import.meta.url);
    const { translation } = JSON.parse(readFileSync(file, 'utf8'));
    for (const key of keys) {
      const variants = [
        key,
        `${key}_zero`,
        `${key}_one`,
        `${key}_two`,
        `${key}_few`,
        `${key}_many`,
        `${key}_other`,
      ];
      const existingVariants = variants.filter((variant) =>
        Object.prototype.hasOwnProperty.call(translation, variant),
      );
      const effectiveVariants =
        existingVariants.length > 0 ? existingVariants : [key];
      const hasBlankVariant = effectiveVariants.some(
        (variant) =>
          typeof translation[variant] !== 'string' ||
          translation[variant].trim() === '',
      );
      if (hasBlankVariant) {
        missing.push(`${locale}:${key}`);
      }
    }
  }

  return missing;
}

function getChannelHealthRoutingTranslationKeys() {
  const files = [
    new URL(
      '../src/components/table/usage-logs/ChannelHealthRoutingTrace.jsx',
      import.meta.url,
    ),
    new URL('../src/helpers/usageLogsChannelHealthRouting.js', import.meta.url),
  ];
  const keys = new Set();

  for (const file of files) {
    const source = readFileSync(file, 'utf8');
    for (const match of source.matchAll(/\bt\(\s*['"]([^'"\n]+)['"]/g)) {
      keys.add(match[1]);
    }
  }

  return [...keys];
}

function getMissingChannelHealthRoutingTranslations() {
  const keys = getChannelHealthRoutingTranslationKeys();
  const locales = ['zh-CN', 'zh-TW', 'en', 'fr', 'ru', 'ja', 'vi'];
  const missing = [];

  for (const locale of locales) {
    const file = new URL(`../src/i18n/locales/${locale}.json`, import.meta.url);
    const { translation } = JSON.parse(readFileSync(file, 'utf8'));
    for (const key of keys) {
      const variants = [
        key,
        `${key}_zero`,
        `${key}_one`,
        `${key}_two`,
        `${key}_few`,
        `${key}_many`,
        `${key}_other`,
      ];
      const existingVariants = variants.filter((variant) =>
        Object.prototype.hasOwnProperty.call(translation, variant),
      );
      const effectiveVariants =
        existingVariants.length > 0 ? existingVariants : [key];
      const hasBlankVariant = effectiveVariants.some(
        (variant) =>
          typeof translation[variant] !== 'string' ||
          translation[variant].trim() === '',
      );
      if (hasBlankVariant) {
        missing.push(`${locale}:${key}`);
      }
    }
  }

  return missing;
}

describe('classic usage logs responses compact tag', () => {
  test('localizes every Responses routing diagnostic in each runtime locale', () => {
    expect(getMissingResponsesRoutingTranslations()).toEqual([]);
  });

  test('limits Responses routing diagnostics to administrators', () => {
    expect(shouldIncludeResponsesRoutingColumn(true)).toBe(true);
    expect(shouldIncludeResponsesRoutingColumn(false)).toBe(false);
  });

  test('localizes every channel health routing diagnostic in each runtime locale', () => {
    expect(getMissingChannelHealthRoutingTranslations()).toEqual([]);
  });

  test('limits channel health routing diagnostics to administrators', () => {
    expect(shouldIncludeChannelHealthRoutingColumn(true)).toBe(true);
    expect(shouldIncludeChannelHealthRoutingColumn(false)).toBe(false);
  });

  test('renders channel health cooldown and redis fail-open without changing route semantics', () => {
    const other = {
      admin_info: {
        channel_health: {
          redis_unavailable: true,
          events: [
            {
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
    };

    const events = getChannelHealthRoutingEvents(other);
    expect(events).toEqual([
      {
        channelId: 160,
        action: 'cooldown',
        statusCode: 502,
        errorCode: 'bad_response_status_code',
      },
      {
        channelId: 0,
        action: 'redis_unavailable',
        statusCode: undefined,
        errorCode: undefined,
      },
    ]);
    expect(getChannelHealthRoutingResult(other)).toEqual({
      kind: 'gate_unavailable',
      actionCount: 2,
    });
    expect(getChannelHealthRoutingEventDisplay(events[0], translate)).toEqual({
      label: '渠道冷却',
      value: '渠道 #160；状态码：502；错误：bad_response_status_code',
    });
    expect(getChannelHealthRoutingEventDisplay(events[1], translate)).toEqual({
      label: '健康门控不可用',
      value: 'Redis 不可用，路由继续执行',
    });
  });

  test('deduplicates channel health redis unavailable markers from flag and event', () => {
    expect(
      getChannelHealthRoutingEvents({
        admin_info: {
          channel_health: {
            redis_unavailable: true,
            events: [],
          },
        },
      }),
    ).toEqual([
      {
        channelId: 0,
        action: 'redis_unavailable',
        statusCode: undefined,
        errorCode: undefined,
      },
    ]);
  });

  test('shows a healthy gate pass without claiming that the upstream call succeeded', () => {
    const other = {
      admin_info: {
        channel_health: {
          events: [{ channel_id: 206, action: 'healthy' }],
        },
      },
    };

    const events = getChannelHealthRoutingEvents(other);
    expect(events).toEqual([
      {
        channelId: 206,
        action: 'healthy',
        statusCode: undefined,
        errorCode: undefined,
      },
    ]);
    expect(getChannelHealthRoutingResult(other)).toEqual({
      kind: 'healthy',
      actionCount: 1,
    });
    expect(getChannelHealthRoutingEventDisplay(events[0], translate)).toEqual({
      label: '健康门控通过',
      value: '渠道 #206；当前没有活动冷却或探测状态',
    });
  });

  test('does not render a generic Responses tag for capability snapshots only', () => {
    expect(
      shouldRenderResponsesCompactTag({
        channel_capability_snapshot: {
          compact_mode_effective: 'native',
          source: 'explicit_settings',
        },
      }),
    ).toBe(false);
  });

  test('renders for actual compact records', () => {
    expect(
      shouldRenderResponsesCompactTag({
        responses_compact_mode: 'native',
        channel_capability_snapshot: {
          compact_mode_effective: 'native',
        },
      }),
    ).toBe(true);
  });

  test('keeps routing diagnostics out of the compact status tag', () => {
    expect(
      shouldRenderResponsesCompactTag({
        responses_encrypted_context_retry: true,
      }),
    ).toBe(false);
  });

  test('returns false for empty input', () => {
    expect(shouldRenderResponsesCompactTag(undefined)).toBe(false);
    expect(shouldRenderResponsesCompactTag(null)).toBe(false);
    expect(shouldRenderResponsesCompactTag({})).toBe(false);
  });

  test('requires literal true for encrypted context retry diagnostics', () => {
    expect(
      shouldRenderResponsesCompactTag({
        responses_encrypted_context_retry: 'true',
      }),
    ).toBe(false);
    expect(
      shouldRenderResponsesCompactTag({
        responses_encrypted_context_retry: 1,
      }),
    ).toBe(false);
  });

  test('lists every supported Responses routing recovery action', () => {
    expect(
      getResponsesRoutingEvents({
        responses_stream_options_include_usage_retry: true,
        responses_stream_options_include_usage_capability_stripped: true,
        responses_remote_compact_synthetic_fallback: true,
        responses_remote_compact_first_data_timeout: true,
        responses_encrypted_context_retry: true,
        responses_encrypted_context_retry_blocked_after_event:
          'response.output_item.added',
        responses_namespace_history_retry: true,
        responses_arguments_history_retry: true,
        responses_compact_visible_only_fallback: true,
        responses_compact_route_decision: 'remote_v2_synthetic_recovery',
        responses_compact_fallback_reason: '413 upstream payload limit',
        responses_routing_channel_skips: [
          { channel_id: 2, reason: 'unsupported_custom_tools' },
        ],
        responses_compact_state_restored: true,
        responses_compact_model_changed: true,
        responses_previous_id_action: 'cleared_by_synthetic_restore',
        responses_reasoning_context_action: 'stripped_unsupported',
        responses_chat_compat_ignored_encrypted_include: true,
        responses_chat_compat_reasoning_summary_mapped: true,
      }),
    ).toEqual([
      { code: 'stream_options_include_usage_retry' },
      { code: 'stream_options_include_usage_capability_stripped' },
      { code: 'remote_compact_synthetic_fallback' },
      { code: 'remote_compact_first_data_timeout' },
      { code: 'encrypted_context_retry' },
      {
        code: 'encrypted_context_retry_blocked_after_event',
        value: 'response.output_item.added',
      },
      { code: 'namespace_history_retry' },
      { code: 'arguments_history_retry' },
      { code: 'compact_visible_only_fallback' },
      {
        code: 'compact_route_decision',
        value: 'remote_v2_synthetic_recovery',
      },
      {
        code: 'compact_fallback_reason',
        value: '413 upstream payload limit',
      },
      { code: 'channel_skip', value: '#2: unsupported_custom_tools' },
      { code: 'compact_state_restored' },
      { code: 'compact_model_changed' },
      {
        code: 'previous_id_action',
        value: 'cleared_by_synthetic_restore',
      },
      { code: 'reasoning_context_action', value: 'stripped_unsupported' },
      { code: 'chat_compat_ignored_encrypted_include' },
      { code: 'chat_compat_reasoning_summary_mapped' },
    ]);
  });

  test('exposes local gateway compaction and input ID normalization as safe structured events', () => {
    const other = {
      responses_codex_local_compaction_ingress_reduced: true,
      responses_codex_local_compaction_body_reduction_source:
        'ingress_preemptive_size',
      responses_codex_local_compaction_ingress_original_body_bytes: 34603607,
      responses_codex_local_compaction_ingress_reduced_body_bytes: 1292,
      responses_codex_local_compaction_ingress_input_items: 4,
      responses_input_item_id_normalization_removed_count: 2,
    };

    expect(getResponsesRoutingEvents(other)).toEqual([
      {
        code: 'local_gateway_body_reduced',
        source: 'ingress_preemptive_size',
        originalBodyBytes: 34603607,
        reducedBodyBytes: 1292,
        inputItems: 4,
      },
      { code: 'input_item_ids_normalized', removedCount: 2 },
    ]);
    expect(getResponsesRoutingBadgeKind(other)).toBe('gateway_compacted');
    expect(
      getResponsesRoutingEventDisplay(
        getResponsesRoutingEvents(other)[0],
        translate,
      ),
    ).toEqual({
      label: '网关已压缩',
      value: '预防性大小保护：请求大小：33.0 MiB -> 1.3 KiB；输入项：4',
    });
  });

  test('shows upstream 413 recovery with sanitized body reduction metrics', () => {
    const other = {
      responses_codex_local_compaction_413_recovery: true,
      responses_codex_local_compaction_body_reduction_source: 'upstream_413',
      responses_codex_local_compaction_original_body_bytes: 34603607,
      responses_codex_local_compaction_reduced_body_bytes: 1292,
    };

    expect(getResponsesRoutingEvents(other)).toEqual([
      {
        code: 'local_gateway_body_reduced',
        source: 'upstream_413',
        originalBodyBytes: 34603607,
        reducedBodyBytes: 1292,
        inputItems: undefined,
      },
      { code: 'local_413_recovery' },
    ]);
    expect(getResponsesRoutingBadgeKind(other)).toBe('route_recovery');
  });

  test('distinguishes a failed local 413 recovery from a successful retry', () => {
    expect(
      getResponsesRoutingEvents({
        responses_codex_local_compaction_413_recovery: true,
        responses_codex_local_compaction_413_recovery_failed: true,
      }),
    ).toEqual([{ code: 'local_413_recovery_failed' }]);
    expect(
      getResponsesRoutingBadgeKind({
        responses_codex_local_compaction_413_recovery: true,
        responses_codex_local_compaction_413_recovery_failed: true,
      }),
    ).toBe('route_failure');
  });

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
    };

    const events = getResponsesRoutingEvents(other);
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
    ]);
    expect(getResponsesRoutingResult(other)).toEqual({
      kind: 'route_recovery',
      actionCount: 8,
    });
    expect(getResponsesRoutingEventDisplay(events[0], translate)).toEqual({
      label: '压缩上下文回退',
      value: '已应用',
    });
    expect(getResponsesRoutingEventDisplay(events[5], translate)).toEqual({
      label: '原生压缩尝试',
      value: '状态码：413；路径：/v1/responses/compact',
    });
    expect(getResponsesRoutingEventDisplay(events[7], translate)).toEqual({
      label: 'Codex 压缩',
      value:
        '实现：responses_compaction_v2；触发：token_budget；原因：context_limit；阶段：preflight；策略：summary',
    });
  });

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
    };

    expect(
      getResponsesRoutingTrace(other).map((item) => [
        item.stage,
        item.event.code,
      ]),
    ).toEqual([
      ['request_preparation', 'local_gateway_body_reduced'],
      ['request_preparation', 'reasoning_context_action'],
      ['route_selection', 'compact_route_decision'],
      ['route_selection', 'channel_skip'],
      ['upstream_attempt', 'compact_native_attempt'],
      ['recovery', 'encrypted_context_retry'],
      ['recovery', 'compact_state_restored'],
    ]);
    expect(getResponsesRoutingFinalPath(other)).toBe('/v1/responses');
    expect(
      getResponsesRoutingFinalPath({
        responses_compact_final_upstream_path: ' ',
        upstream_request_path: ' /v1/responses ',
      }),
    ).toBe('/v1/responses');
    expect(getResponsesRoutingFinalPath({})).toBeUndefined();
  });

  test('summarizes the final routing result without inventing unrecorded actions', () => {
    expect(getResponsesRoutingResult(undefined)).toBeNull();
    expect(getResponsesRoutingResult({})).toBeNull();

    expect(
      getResponsesRoutingResult({
        channel_capability_snapshot: {
          compact_mode_effective: 'native',
        },
      }),
    ).toEqual({ kind: 'capability_observed', actionCount: 0 });

    expect(
      getResponsesRoutingResult({ responses_compact_mode: 'native' }),
    ).toEqual({ kind: 'native_passthrough', actionCount: 0 });

    expect(
      getResponsesRoutingResult({
        responses_compact_mode: 'synthetic_summary',
      }),
    ).toEqual({ kind: 'synthetic_compaction', actionCount: 0 });

    expect(
      getResponsesRoutingResult({
        responses_reasoning_context_action: 'stripped_unsupported',
      }),
    ).toEqual({ kind: 'request_adapted', actionCount: 1 });

    expect(
      getResponsesRoutingResult({
        responses_codex_local_compaction_413_recovery: true,
        responses_codex_local_compaction_413_recovery_failed: true,
      }),
    ).toEqual({ kind: 'route_failure', actionCount: 1 });
  });

  test('uses the admin channel trace for an automatic channel reroute', () => {
    const other = {
      responses_routing_trace: {
        candidates: [117, 205, 205],
        preflight_skipped: [
          { channel_id: 117, reason: 'unsupported_namespace_tools' },
        ],
        attempted_channels: [{ channel_id: 205, outcome: 'success' }],
        final_channel: 205,
        terminal_reason: 'success',
      },
    };
    const trace = getResponsesRoutingChannelTrace(other);
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
    });
    expect(hasResponsesRoutingChannelTrace(trace)).toBe(true);
    expect(getResponsesRoutingBadgeKind(other)).toBe('route_recovery');
    expect(getResponsesRoutingResult(other)).toEqual({
      kind: 'route_recovery',
      actionCount: 2,
    });
  });

  test('does not surface an ordinary single-channel success as a reroute', () => {
    const other = {
      responses_routing_trace: {
        candidates: [205],
        attempted_channels: [{ channel_id: 205, outcome: 'success' }],
        final_channel: 205,
        terminal_reason: 'success',
      },
    };
    expect(getResponsesRoutingChannelTrace(other)).not.toBeNull();
    expect(getResponsesRoutingResult(other)).toBeNull();
  });

  test('maps every final routing result to a distinct admin-facing state', () => {
    expect(
      getResponsesRoutingResultDisplay(
        { kind: 'gateway_compacted', actionCount: 1 },
        translate,
      ),
    ).toEqual({ label: '网关已压缩', color: 'cyan' });
    expect(
      getResponsesRoutingResultDisplay(
        { kind: 'route_recovery', actionCount: 1 },
        translate,
      ),
    ).toEqual({ label: '路由恢复', color: 'orange' });
    expect(
      getResponsesRoutingResultDisplay(
        { kind: 'route_failure', actionCount: 1 },
        translate,
      ),
    ).toEqual({ label: '路由恢复失败', color: 'red' });
    expect(
      getResponsesRoutingResultDisplay(
        { kind: 'request_adapted', actionCount: 1 },
        translate,
      ),
    ).toEqual({ label: '请求已适配', color: 'blue' });
    expect(
      getResponsesRoutingResultDisplay(
        { kind: 'synthetic_compaction', actionCount: 0 },
        translate,
      ),
    ).toEqual({ label: '合成压缩', color: 'violet' });
    expect(
      getResponsesRoutingResultDisplay(
        { kind: 'native_passthrough', actionCount: 0 },
        translate,
      ),
    ).toEqual({ label: '原生透传', color: 'green' });
    expect(
      getResponsesRoutingResultDisplay(
        { kind: 'capability_observed', actionCount: 0 },
        translate,
      ),
    ).toEqual({ label: '仅能力观测', color: 'grey' });
  });
});
