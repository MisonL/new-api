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

import {
  buildResponsesRoutingTrace,
  getResponsesRoutingFinalPath as getFinalResponsesRoutingPath,
  getResponsesRoutingChannelTrace,
  hasResponsesRoutingChannelTrace,
} from './usageLogsResponsesRoutingTrace.js';

export {
  getResponsesRoutingChannelTrace,
  hasResponsesRoutingChannelTrace,
} from './usageLogsResponsesRoutingTrace.js';

function addStringEvent(events, code, value) {
  const normalized = String(value || '').trim();
  if (normalized) {
    events.push({ code, value: normalized });
  }
}

function positiveSafeInteger(value) {
  const numeric = Number(value);
  if (!Number.isSafeInteger(numeric) || numeric <= 0) {
    return undefined;
  }
  return numeric;
}

function addLocalGatewayBodyReductionEvent(events, other) {
  const source = String(
    other.responses_codex_local_compaction_body_reduction_source || '',
  ).trim();
  const ingressReduced =
    other.responses_codex_local_compaction_ingress_reduced === true;
  const originalBodyBytes = positiveSafeInteger(
    ingressReduced
      ? other.responses_codex_local_compaction_ingress_original_body_bytes
      : other.responses_codex_local_compaction_original_body_bytes,
  );
  const reducedBodyBytes = positiveSafeInteger(
    ingressReduced
      ? other.responses_codex_local_compaction_ingress_reduced_body_bytes
      : other.responses_codex_local_compaction_reduced_body_bytes,
  );
  if (!ingressReduced && (!originalBodyBytes || !reducedBodyBytes)) {
    return;
  }

  events.push({
    code: 'local_gateway_body_reduced',
    source: source || undefined,
    originalBodyBytes,
    reducedBodyBytes,
    inputItems: positiveSafeInteger(
      other.responses_codex_local_compaction_ingress_input_items,
    ),
  });
}

function addInputItemIDNormalizationEvent(events, other) {
  const removedCount = positiveSafeInteger(
    other.responses_input_item_id_normalization_removed_count,
  );
  if (removedCount) {
    events.push({ code: 'input_item_ids_normalized', removedCount });
  }
}

function normalizeStringList(value) {
  if (!Array.isArray(value)) {
    return [];
  }

  const seen = new Set();
  const values = [];
  for (const item of value) {
    const normalized = String(item || '').trim();
    if (!normalized || seen.has(normalized)) {
      continue;
    }
    seen.add(normalized);
    values.push(normalized);
  }
  return values;
}

function addCompactFallbackEvents(events, other) {
  if (other.responses_compact_context_fallback === true) {
    events.push({ code: 'compact_context_fallback' });
  }
  if (other.responses_compact_previous_response_id_fallback === true) {
    events.push({ code: 'compact_previous_response_id_fallback' });
  }
  if (other.responses_compact_summary_model_fallback === true) {
    events.push({ code: 'compact_summary_model_fallback' });
  }
  addStringEvent(
    events,
    'compact_summary_model',
    other.responses_compact_summary_model,
  );
  const summaryModels = normalizeStringList(
    other.responses_compact_summary_models,
  );
  if (summaryModels.length > 0) {
    events.push({
      code: 'compact_summary_models',
      value: summaryModels.join(', '),
    });
  }

  if (other.responses_compact_native_attempted === true) {
    events.push({
      code: 'compact_native_attempt',
      upstreamPath:
        String(other.responses_compact_native_upstream_path || '').trim() ||
        undefined,
      statusCode: positiveSafeInteger(
        other.responses_compact_native_status_code,
      ),
    });
  }
  if (other.responses_compact_auto_fallback === true) {
    events.push({ code: 'compact_auto_fallback' });
  }
  if (other.responses_compact_auto_fallback_window === true) {
    events.push({
      code: 'compact_auto_fallback_window',
      fallbackReason:
        String(other.responses_compact_auto_fallback_reason || '').trim() ||
        undefined,
      retryUntilUnix: positiveSafeInteger(
        other.responses_compact_auto_fallback_retry_until,
      ),
      retryIntervalHours: positiveSafeInteger(
        other.responses_compact_auto_fallback_retry_interval_hours,
      ),
    });
  }

  const client = String(other.responses_compact_client || '').trim();
  const implementation = String(
    other.responses_compact_client_implementation || '',
  ).trim();
  const trigger = String(other.responses_compact_trigger || '').trim();
  const reason = String(other.responses_compact_reason || '').trim();
  const phase = String(other.responses_compact_phase || '').trim();
  const strategy = String(other.responses_compact_strategy || '').trim();
  if (client || implementation || trigger || reason || phase || strategy) {
    events.push({
      code: 'codex_compaction',
      client: client || undefined,
      implementation: implementation || undefined,
      trigger: trigger || undefined,
      reason: reason || undefined,
      phase: phase || undefined,
      strategy: strategy || undefined,
    });
  }
}

function addChannelSkipEvents(events, channelSkips) {
  if (!Array.isArray(channelSkips)) {
    return false;
  }

  const seen = new Set();
  let added = false;
  for (const channelSkip of channelSkips) {
    const channelId = Number(channelSkip?.channel_id);
    const reason = String(channelSkip?.reason || '').trim();
    if (!Number.isInteger(channelId) || channelId <= 0 || !reason) {
      continue;
    }

    const value = `#${channelId}: ${reason}`;
    if (seen.has(value)) {
      continue;
    }
    seen.add(value);
    events.push({ code: 'channel_skip', value });
    added = true;
  }
  return added;
}

export function getResponsesRoutingEvents(other) {
  if (!other || typeof other !== 'object') {
    return [];
  }

  const events = [];
  addLocalGatewayBodyReductionEvent(events, other);
  if (other.responses_codex_local_compaction_413_recovery_failed === true) {
    events.push({ code: 'local_413_recovery_failed' });
  } else if (other.responses_codex_local_compaction_413_recovery === true) {
    events.push({ code: 'local_413_recovery' });
  }
  addInputItemIDNormalizationEvent(events, other);
  addCompactFallbackEvents(events, other);
  if (other.responses_stream_options_include_usage_retry === true) {
    events.push({ code: 'stream_options_include_usage_retry' });
  }
  if (
    other.responses_stream_options_include_usage_capability_stripped === true
  ) {
    events.push({ code: 'stream_options_include_usage_capability_stripped' });
  }
  if (other.responses_remote_compact_synthetic_fallback === true) {
    events.push({ code: 'remote_compact_synthetic_fallback' });
  }
  if (other.responses_remote_compact_first_data_timeout === true) {
    events.push({ code: 'remote_compact_first_data_timeout' });
  }
  if (other.responses_encrypted_context_retry === true) {
    events.push({ code: 'encrypted_context_retry' });
  }
  addStringEvent(
    events,
    'encrypted_context_retry_blocked_after_event',
    other.responses_encrypted_context_retry_blocked_after_event,
  );
  if (other.responses_namespace_history_retry === true) {
    events.push({ code: 'namespace_history_retry' });
  }
  if (other.responses_arguments_history_retry === true) {
    events.push({ code: 'arguments_history_retry' });
  }
  if (other.responses_compact_visible_only_fallback === true) {
    events.push({ code: 'compact_visible_only_fallback' });
  }
  addStringEvent(
    events,
    'compact_route_decision',
    other.responses_compact_route_decision,
  );
  addStringEvent(
    events,
    'compact_fallback_reason',
    other.responses_compact_fallback_reason,
  );
  if (!addChannelSkipEvents(events, other.responses_routing_channel_skips)) {
    addStringEvent(
      events,
      'compact_channel_skip',
      other.responses_compact_channel_skip,
    );
  }
  if (other.responses_compact_state_restored === true) {
    events.push({ code: 'compact_state_restored' });
  }
  if (other.responses_compact_model_changed === true) {
    events.push({ code: 'compact_model_changed' });
  }
  addStringEvent(
    events,
    'previous_id_action',
    other.responses_previous_id_action,
  );
  addStringEvent(
    events,
    'reasoning_context_action',
    other.responses_reasoning_context_action,
  );
  if (other.responses_chat_compat_ignored_encrypted_include === true) {
    events.push({ code: 'chat_compat_ignored_encrypted_include' });
  }
  if (other.responses_chat_compat_reasoning_summary_mapped === true) {
    events.push({ code: 'chat_compat_reasoning_summary_mapped' });
  }
  return events;
}

export function getResponsesRoutingTrace(other) {
  return buildResponsesRoutingTrace(getResponsesRoutingEvents(other));
}

export function getResponsesRoutingFinalPath(other) {
  return getFinalResponsesRoutingPath(other);
}

const recoveryEventCodes = new Set([
  'local_413_recovery',
  'compact_context_fallback',
  'compact_previous_response_id_fallback',
  'compact_summary_model_fallback',
  'compact_auto_fallback',
  'compact_auto_fallback_window',
  'stream_options_include_usage_retry',
  'remote_compact_synthetic_fallback',
  'remote_compact_first_data_timeout',
  'encrypted_context_retry',
  'namespace_history_retry',
  'arguments_history_retry',
  'compact_visible_only_fallback',
  'compact_fallback_reason',
  'compact_channel_skip',
  'channel_skip',
  'compact_model_changed',
]);

export function getResponsesRoutingBadgeKind(other) {
  const events = getResponsesRoutingEvents(other);
  const channelTrace = getResponsesRoutingChannelTrace(other);
  if (events.length === 0 && !hasResponsesRoutingChannelTrace(channelTrace)) {
    return null;
  }
  if (
    channelTrace &&
    hasResponsesRoutingChannelTrace(channelTrace) &&
    channelTrace.terminalReason &&
    channelTrace.terminalReason !== 'success' &&
    !channelTrace.attemptedChannels.some(
      (attempt) => attempt.outcome === 'success',
    )
  ) {
    return 'route_failure';
  }
  if (
    channelTrace &&
    hasResponsesRoutingChannelTrace(channelTrace) &&
    (channelTrace.preflightSkipped.length > 0 ||
      channelTrace.attemptedChannels.length > 1 ||
      channelTrace.candidates.length > 1)
  ) {
    return 'route_recovery';
  }
  if (events.some((event) => event.code === 'local_413_recovery_failed')) {
    return 'route_failure';
  }
  if (events.some((event) => recoveryEventCodes.has(event.code))) {
    return 'route_recovery';
  }
  if (events.some((event) => event.code === 'local_gateway_body_reduced')) {
    return 'gateway_compacted';
  }
  return 'request_adapted';
}

export function getResponsesRoutingResult(other) {
  const events = getResponsesRoutingEvents(other);
  const channelTrace = getResponsesRoutingChannelTrace(other);
  const channelTraceIsMeaningful =
    hasResponsesRoutingChannelTrace(channelTrace);
  const hasDetails =
    events.length > 0 ||
    Boolean(other?.responses_compact_mode) ||
    Boolean(other?.channel_capability_snapshot) ||
    channelTraceIsMeaningful;
  if (!hasDetails) {
    return null;
  }

  const channelTraceActionCount = channelTrace
    ? channelTrace.preflightSkipped.length +
      channelTrace.attemptedChannels.length
    : 0;
  const actionCount = events.length + channelTraceActionCount;
  if (
    channelTraceIsMeaningful &&
    channelTrace?.terminalReason &&
    channelTrace.terminalReason !== 'success' &&
    !channelTrace.attemptedChannels.some(
      (attempt) => attempt.outcome === 'success',
    )
  ) {
    return { kind: 'route_failure', actionCount };
  }
  if (
    channelTraceIsMeaningful &&
    (channelTrace.preflightSkipped.length > 0 ||
      channelTrace.attemptedChannels.length > 1 ||
      channelTrace.candidates.length > 1)
  ) {
    return { kind: 'route_recovery', actionCount };
  }
  if (events.some((event) => event.code === 'local_413_recovery_failed')) {
    return { kind: 'route_failure', actionCount };
  }
  if (events.some((event) => recoveryEventCodes.has(event.code))) {
    return { kind: 'route_recovery', actionCount };
  }
  if (events.some((event) => event.code === 'local_gateway_body_reduced')) {
    return { kind: 'gateway_compacted', actionCount };
  }
  if (actionCount > 0) {
    return { kind: 'request_adapted', actionCount };
  }

  switch (other?.responses_compact_mode) {
    case 'synthetic_summary':
      return { kind: 'synthetic_compaction', actionCount };
    case 'native':
      return { kind: 'native_passthrough', actionCount };
    default:
      return { kind: 'capability_observed', actionCount };
  }
}

export function getResponsesRoutingResultDisplay(result, t) {
  switch (result.kind) {
    case 'gateway_compacted':
      return { label: t('网关已压缩'), color: 'cyan' };
    case 'route_recovery':
      return { label: t('路由恢复'), color: 'orange' };
    case 'route_failure':
      return { label: t('路由恢复失败'), color: 'red' };
    case 'request_adapted':
      return { label: t('请求已适配'), color: 'blue' };
    case 'synthetic_compaction':
      return { label: t('合成压缩'), color: 'violet' };
    case 'native_passthrough':
      return { label: t('原生透传'), color: 'green' };
    case 'capability_observed':
      return { label: t('仅能力观测'), color: 'grey' };
    default:
      return { label: '', color: 'grey' };
  }
}

export function formatResponsesRoutingBytes(bytes) {
  const units = ['B', 'KiB', 'MiB', 'GiB'];
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  return unitIndex === 0
    ? `${value} ${units[unitIndex]}`
    : `${value.toFixed(1)} ${units[unitIndex]}`;
}

function formatResponsesRoutingTimestamp(unix) {
  if (!unix) {
    return undefined;
  }
  const timestamp = new Date(unix * 1000);
  if (Number.isNaN(timestamp.getTime())) {
    return undefined;
  }
  return timestamp
    .toISOString()
    .replace('T', ' ')
    .replace(/\.\d{3}Z$/, ' UTC');
}

function formatNativeCompactionAttempt(event, t) {
  const details = [];
  if (event.statusCode !== undefined) {
    details.push(t('状态码：{{status}}', { status: event.statusCode }));
  }
  if (event.upstreamPath) {
    details.push(t('路径：{{path}}', { path: event.upstreamPath }));
  }
  return details.length > 0 ? details.join(t('；')) : t('已尝试');
}

function formatAutoFallbackWindow(event, t) {
  const details = [];
  if (event.fallbackReason) {
    details.push(t('回退原因：{{reason}}', { reason: event.fallbackReason }));
  }
  const retryUntil = formatResponsesRoutingTimestamp(event.retryUntilUnix);
  if (event.retryIntervalHours !== undefined || retryUntil) {
    details.push(
      t('重试窗口：{{hours}} 小时，至 {{time}}', {
        hours: event.retryIntervalHours ?? '-',
        time: retryUntil ?? '-',
      }),
    );
  }
  return details.length > 0 ? details.join(t('；')) : t('已启用');
}

function formatCodexCompaction(event, t) {
  const details = [];
  if (event.implementation) {
    details.push(t('实现：{{value}}', { value: event.implementation }));
  }
  if (event.trigger) {
    details.push(t('触发：{{value}}', { value: event.trigger }));
  }
  if (event.reason) {
    details.push(t('原因：{{value}}', { value: event.reason }));
  }
  if (event.phase) {
    details.push(t('阶段：{{value}}', { value: event.phase }));
  }
  if (event.strategy) {
    details.push(t('策略：{{value}}', { value: event.strategy }));
  }
  return details.length > 0
    ? details.join(t('；'))
    : event.client || t('已观测');
}

function getGatewayBodyReductionSourceLabel(source, t) {
  switch (source) {
    case 'ingress_preemptive_size':
    case 'preemptive_size':
      return t('预防性大小保护');
    case 'upstream_413':
      return t('上游 413 恢复');
    default:
      return undefined;
  }
}

function getGatewayBodyReductionValue(event, t) {
  const parts = [];
  const source = getGatewayBodyReductionSourceLabel(event.source, t);
  if (source) {
    parts.push(source);
  }
  if (
    event.originalBodyBytes !== undefined &&
    event.reducedBodyBytes !== undefined
  ) {
    parts.push(
      t('请求大小：{{before}} -> {{after}}', {
        before: formatResponsesRoutingBytes(event.originalBodyBytes),
        after: formatResponsesRoutingBytes(event.reducedBodyBytes),
      }),
    );
  } else {
    parts.push(t('请求大小已缩减'));
  }
  if (event.inputItems !== undefined) {
    parts.push(t('输入项：{{count}}', { count: event.inputItems }));
  }
  const details = parts.slice(source ? 1 : 0).join(t('；'));
  return source ? t('{{source}}：{{details}}', { source, details }) : details;
}

export function getResponsesRoutingEventDisplay(event, t) {
  switch (event.code) {
    case 'local_gateway_body_reduced':
      return {
        label: t('网关已压缩'),
        value: getGatewayBodyReductionValue(event, t),
      };
    case 'local_413_recovery':
      return {
        label: t('本地 413 恢复'),
        value: t('压缩后已重试'),
      };
    case 'local_413_recovery_failed':
      return {
        label: t('本地 413 恢复'),
        value: t('压缩后恢复失败'),
      };
    case 'input_item_ids_normalized':
      return {
        label: t('输入项 ID 已归一化'),
        value: t('已移除无效 ID：{{count}}', {
          count: event.removedCount || 0,
        }),
      };
    case 'compact_context_fallback':
      return {
        label: t('压缩上下文回退'),
        value: t('已应用'),
      };
    case 'compact_previous_response_id_fallback':
      return {
        label: t('前序响应回退'),
        value: t('已应用'),
      };
    case 'compact_summary_model_fallback':
      return {
        label: t('摘要模型回退'),
        value: t('已应用'),
      };
    case 'compact_summary_model':
      return {
        label: t('摘要模型'),
        value: event.value || '-',
      };
    case 'compact_summary_models':
      return {
        label: t('摘要模型列表'),
        value: event.value || '-',
      };
    case 'compact_native_attempt':
      return {
        label: t('原生压缩尝试'),
        value: formatNativeCompactionAttempt(event, t),
      };
    case 'compact_auto_fallback':
      return {
        label: t('自动回退已启用'),
        value: t('已应用'),
      };
    case 'compact_auto_fallback_window':
      return {
        label: t('自动回退窗口'),
        value: formatAutoFallbackWindow(event, t),
      };
    case 'codex_compaction':
      return {
        label: t('Codex 压缩'),
        value: formatCodexCompaction(event, t),
      };
    case 'stream_options_include_usage_retry':
      return {
        label: t('流用量重试'),
        value: t('已恢复'),
      };
    case 'stream_options_include_usage_capability_stripped':
      return {
        label: t('流用量已省略'),
        value: t('已学习上游能力'),
      };
    case 'remote_compact_synthetic_fallback':
      return {
        label: t('远程压缩恢复'),
        value: t('合成摘要'),
      };
    case 'remote_compact_first_data_timeout':
      return {
        label: t('远程压缩首个事件超时'),
        value: t('已观测'),
      };
    case 'encrypted_context_retry':
      return {
        label: t('加密上下文重试'),
        value: t('已重试'),
      };
    case 'encrypted_context_retry_blocked_after_event':
      return {
        label: t('加密上下文重试受阻'),
        value: event.value || '-',
      };
    case 'namespace_history_retry':
      return {
        label: t('命名空间历史重试'),
        value: t('已重试'),
      };
    case 'arguments_history_retry':
      return {
        label: t('工具参数历史重试'),
        value: t('已重试'),
      };
    case 'compact_visible_only_fallback':
      return {
        label: t('仅可见内容回退'),
        value: t('已启用'),
      };
    case 'compact_route_decision':
      return {
        label: t('路由决策'),
        value: event.value || '-',
      };
    case 'compact_fallback_reason':
      return {
        label: t('回退原因'),
        value: event.value || '-',
      };
    case 'compact_channel_skip':
    case 'channel_skip':
      return {
        label: t('跳过渠道'),
        value: event.value || '-',
      };
    case 'compact_state_restored':
      return {
        label: t('压缩状态已恢复'),
        value: t('是'),
      };
    case 'compact_model_changed':
      return {
        label: t('压缩模型已切换'),
        value: t('是'),
      };
    case 'previous_id_action':
      return {
        label: t('前序响应处理'),
        value: event.value || '-',
      };
    case 'reasoning_context_action':
      return {
        label: t('推理上下文处理'),
        value: event.value || '-',
      };
    case 'chat_compat_ignored_encrypted_include':
      return {
        label: t('Chat 兼容处理'),
        value: t('已忽略加密 include'),
      };
    case 'chat_compat_reasoning_summary_mapped':
      return {
        label: t('Chat 兼容处理'),
        value: t('已映射推理摘要'),
      };
    default:
      return {
        label: event.code,
        value: event.value || '-',
      };
  }
}

export function shouldRenderResponsesCompactTag(other) {
  return Boolean(other?.responses_compact_mode);
}
