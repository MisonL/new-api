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

const traceStages = [
  'request_preparation',
  'route_selection',
  'upstream_attempt',
  'recovery',
];

const requestPreparationCodes = new Set([
  'input_item_ids_normalized',
  'reasoning_context_action',
  'chat_compat_ignored_encrypted_include',
  'chat_compat_reasoning_summary_mapped',
  'stream_options_include_usage_capability_stripped',
]);

const upstreamAttemptCodes = new Set([
  'compact_native_attempt',
  'remote_compact_first_data_timeout',
]);

const recoveryCodes = new Set([
  'local_413_recovery',
  'local_413_recovery_failed',
  'compact_context_fallback',
  'compact_previous_response_id_fallback',
  'compact_summary_model_fallback',
  'compact_summary_model',
  'compact_summary_models',
  'compact_auto_fallback',
  'compact_auto_fallback_window',
  'stream_options_include_usage_retry',
  'remote_compact_synthetic_fallback',
  'encrypted_context_retry',
  'encrypted_context_retry_blocked_after_event',
  'namespace_history_retry',
  'arguments_history_retry',
  'compact_visible_only_fallback',
  'compact_fallback_reason',
  'compact_state_restored',
  'compact_model_changed',
  'previous_id_action',
]);

const recoveryToneCodes = new Set([
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
  'encrypted_context_retry_blocked_after_event',
  'namespace_history_retry',
  'arguments_history_retry',
  'compact_visible_only_fallback',
  'compact_fallback_reason',
  'compact_state_restored',
  'compact_model_changed',
  'previous_id_action',
]);

function getTraceStage(event) {
  if (
    event.code === 'local_gateway_body_reduced' &&
    event.source === 'upstream_413'
  ) {
    return 'recovery';
  }
  if (event.code === 'local_gateway_body_reduced') {
    return 'request_preparation';
  }
  if (requestPreparationCodes.has(event.code)) {
    return 'request_preparation';
  }
  if (upstreamAttemptCodes.has(event.code)) {
    return 'upstream_attempt';
  }
  if (recoveryCodes.has(event.code)) {
    return 'recovery';
  }
  return 'route_selection';
}

export function buildResponsesRoutingTrace(events) {
  const trace = [];
  for (const stage of traceStages) {
    for (const event of events) {
      if (getTraceStage(event) === stage) {
        trace.push({ stage, event });
      }
    }
  }
  return trace;
}

function positiveRoutingInteger(value) {
  const number =
    typeof value === 'number'
      ? value
      : typeof value === 'string' && /^\d+$/.test(value.trim())
        ? Number(value)
        : NaN;
  return Number.isSafeInteger(number) && number > 0 ? number : undefined;
}

function normalizedRoutingString(value) {
  if (typeof value !== 'string') {
    return undefined;
  }
  const normalized = value.trim();
  return normalized ? normalized.slice(0, 128) : undefined;
}

function normalizedRoutingChannelList(value) {
  if (!Array.isArray(value)) {
    return [];
  }
  const seen = new Set();
  const channels = [];
  for (const item of value.slice(0, 256)) {
    const channel = positiveRoutingInteger(item);
    if (channel === undefined || seen.has(channel)) {
      continue;
    }
    seen.add(channel);
    channels.push(channel);
  }
  return channels;
}

function normalizedRoutingChannelSkips(value) {
  if (!Array.isArray(value)) {
    return [];
  }
  const seen = new Set();
  const skips = [];
  for (const item of value.slice(0, 256)) {
    if (!item || typeof item !== 'object' || Array.isArray(item)) {
      continue;
    }
    const channelId = positiveRoutingInteger(item.channel_id);
    const reason = normalizedRoutingString(item.reason);
    if (channelId === undefined || !reason) {
      continue;
    }
    const key = `${channelId}:${reason}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    skips.push({ channel_id: channelId, reason });
  }
  return skips;
}

function normalizedRoutingAttempts(value) {
  if (!Array.isArray(value)) {
    return [];
  }
  const attempts = [];
  for (const item of value.slice(0, 256)) {
    if (!item || typeof item !== 'object' || Array.isArray(item)) {
      continue;
    }
    const channelId = positiveRoutingInteger(item.channel_id);
    if (channelId === undefined) {
      continue;
    }
    attempts.push({
      channel_id: channelId,
      status_code: positiveRoutingInteger(item.status_code),
      error_code: normalizedRoutingString(item.error_code),
      error_category: normalizedRoutingString(item.error_category),
      outcome: normalizedRoutingString(item.outcome),
    });
  }
  return attempts;
}

export function getResponsesRoutingChannelTrace(other) {
  const raw = other?.responses_routing_trace;
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    return null;
  }
  const trace = {
    candidates: normalizedRoutingChannelList(raw.candidates),
    preflightSkipped: normalizedRoutingChannelSkips(raw.preflight_skipped),
    attemptedChannels: normalizedRoutingAttempts(raw.attempted_channels),
    finalChannel: positiveRoutingInteger(raw.final_channel),
    terminalReason: normalizedRoutingString(raw.terminal_reason),
  };
  if (
    trace.candidates.length === 0 &&
    trace.preflightSkipped.length === 0 &&
    trace.attemptedChannels.length === 0 &&
    trace.finalChannel === undefined &&
    !trace.terminalReason
  ) {
    return null;
  }
  return trace;
}

export function hasResponsesRoutingChannelTrace(trace) {
  if (!trace) {
    return false;
  }
  if (trace.preflightSkipped.length > 0 || trace.attemptedChannels.length > 1) {
    return true;
  }
  if (trace.candidates.length > 1) {
    return true;
  }
  return Boolean(trace.terminalReason && trace.terminalReason !== 'success');
}

function readPath(value) {
  if (typeof value !== 'string') {
    return undefined;
  }
  const path = value.trim();
  return path || undefined;
}

export function getResponsesRoutingFinalPath(other) {
  if (!other || typeof other !== 'object') {
    return undefined;
  }
  return (
    readPath(other.responses_compact_final_upstream_path) ||
    readPath(other.upstream_request_path) ||
    readPath(other.responses_compact_upstream_path)
  );
}

export function getResponsesRoutingTraceStageDisplay(stage, t) {
  switch (stage) {
    case 'request_preparation':
      return t('请求准备');
    case 'route_selection':
      return t('路由选择');
    case 'upstream_attempt':
      return t('上游尝试');
    case 'recovery':
      return t('恢复处理');
    default:
      return stage;
  }
}

export function getResponsesRoutingTraceTone(event) {
  if (
    event.code === 'local_413_recovery_failed' ||
    (event.code === 'compact_native_attempt' &&
      typeof event.statusCode === 'number' &&
      event.statusCode >= 400)
  ) {
    return 'failure';
  }
  if (
    (event.code === 'local_gateway_body_reduced' &&
      event.source === 'upstream_413') ||
    recoveryToneCodes.has(event.code)
  ) {
    return 'recovery';
  }
  if (getTraceStage(event) === 'request_preparation') {
    return 'adaptation';
  }
  return 'neutral';
}

export function shouldIncludeResponsesRoutingColumn(isAdminUser) {
  return isAdminUser === true;
}
