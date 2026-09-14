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

export function getChannelHealthRuntimeStateDisplay(state, t) {
  switch (state) {
    case 'cooldown':
      return { label: t('渠道冷却中'), color: 'orange' };
    case 'half_open_probe':
      return { label: t('半开探测进行中'), color: 'blue' };
    case 'awaiting_half_open_probe':
      return { label: t('等待半开探测'), color: 'grey' };
    default:
      return { label: t('未知运行状态'), color: 'grey' };
  }
}

export function getChannelHealthRuntimeStateBehavior(state, t) {
  switch (state) {
    case 'cooldown':
      return {
        impact: t('冷却期间，相同分组和模型的请求会绕过该渠道。'),
        nextStep: t('冷却结束后，下一个匹配请求可以获取恢复探测。'),
      };
    case 'half_open_probe':
      return {
        impact: t('一个匹配请求正在探测恢复，其他匹配请求继续绕过该渠道。'),
        nextStep: t('探测成功会清除临时绕过；合格失败会重新进入冷却。'),
      };
    case 'awaiting_half_open_probe':
      return {
        impact: t('冷却已结束，该渠道正在等待恢复探测。'),
        nextStep: t('下一个匹配请求可以获取唯一的恢复探测。'),
      };
    default:
      return {
        impact: t('当前运行状态没有定义路由绕过行为。'),
        nextStep: t('当前运行状态没有可用的后续转换信息。'),
      };
  }
}

const runtimeStatePriority = {
  cooldown: 3,
  half_open_probe: 2,
  awaiting_half_open_probe: 1,
};

const ACTIVE_RUNTIME_REFRESH_MS = 1000;
const IDLE_RUNTIME_REFRESH_MS = 5000;

export function getChannelHealthRuntimeEntriesForChannel(runtime, channelId) {
  if (
    !runtime?.enabled ||
    !runtime.redis_available ||
    !Number.isInteger(channelId)
  ) {
    return [];
  }

  const entries = Array.isArray(runtime.entries) ? runtime.entries : [];
  return entries.filter((entry) => entry?.channel_id === channelId);
}

export function getChannelHealthRuntimeEntrySearchCriteria(entry) {
  const channelId = Number(entry?.channel_id);
  if (!Number.isSafeInteger(channelId) || channelId <= 0) {
    return null;
  }

  return {
    searchKeyword: String(channelId),
    searchGroup: '',
    searchModel: '',
  };
}

export function getChannelHealthRuntimeDominantEntry(entries) {
  return entries.reduce((current, entry) => {
    if (!current) return entry;
    const currentPriority = runtimeStatePriority[current.state] ?? 0;
    const entryPriority = runtimeStatePriority[entry.state] ?? 0;
    return entryPriority > currentPriority ? entry : current;
  }, null);
}

export function getChannelHealthRuntimeChannelStateDisplay(entries, t) {
  const mostRestrictiveEntry = getChannelHealthRuntimeDominantEntry(entries);

  return mostRestrictiveEntry
    ? getChannelHealthRuntimeStateDisplay(mostRestrictiveEntry.state, t)
    : null;
}

function readTrimmedString(value) {
  if (typeof value !== 'string') return undefined;
  const normalized = value.trim();
  return normalized || undefined;
}

function readPositiveInteger(value) {
  const normalized =
    typeof value === 'number'
      ? value
      : typeof value === 'string' && value.trim()
        ? Number(value)
        : Number.NaN;
  return Number.isSafeInteger(normalized) && normalized > 0
    ? normalized
    : undefined;
}

function readNonNegativeInteger(value) {
  const normalized = readNonNegativeIntegerValue(value);
  return Number.isSafeInteger(normalized) && normalized >= 0
    ? normalized
    : undefined;
}

function readNonNegativeIntegerValue(value) {
  return typeof value === 'number'
    ? value
    : typeof value === 'string' && value.trim()
      ? Number(value)
      : Number.NaN;
}

export function getChannelHealthRuntimeEntryKey(entry) {
  return JSON.stringify([
    entry.group,
    entry.model_name,
    entry.channel_id,
    entry.state,
    entry.route_key ?? '',
    entry.effective_model ?? '',
    entry.protocol ?? '',
    entry.upstream_path ?? '',
    entry.multi_key_index ?? '',
  ]);
}

export function getChannelHealthRuntimeRouteSummary(entry) {
  const details = [
    entry.effective_model ? `model=${entry.effective_model}` : '',
    entry.protocol ? `protocol=${entry.protocol}` : '',
    entry.upstream_path ? `path=${entry.upstream_path}` : '',
    Number.isInteger(entry.multi_key_index)
      ? `key=${entry.multi_key_index}`
      : '',
    entry.route_key ? `route=${entry.route_key.slice(0, 12)}` : '',
  ].filter(Boolean);
  return details.length > 0 ? details.join(' | ') : '-';
}

export function getChannelHealthRuntimeEvents(runtime) {
  const rawEvents = Array.isArray(runtime?.recent_events)
    ? runtime.recent_events
    : [];

  return rawEvents.flatMap((event) => {
    const occurredAt = readPositiveInteger(event?.occurred_at);
    const channelId = readPositiveInteger(event?.channel_id);
    const group = readTrimmedString(event?.group);
    const modelName = readTrimmedString(event?.model_name);
    const action = readTrimmedString(event?.action);
    if (!occurredAt || !channelId || !group || !modelName || !action) {
      return [];
    }

    const statusCode = readPositiveInteger(event?.status_code);
    const errorCode = readTrimmedString(event?.error_code);
    const routeKey = readTrimmedString(event?.route_key);
    const effectiveModel = readTrimmedString(event?.effective_model);
    const protocol = readTrimmedString(event?.protocol);
    const upstreamPath = readTrimmedString(event?.upstream_path);
    const multiKeyIndex = readNonNegativeInteger(event?.multi_key_index);
    return [
      {
        occurred_at: occurredAt,
        group,
        model_name: modelName,
        channel_id: channelId,
        action,
        ...(statusCode ? { status_code: statusCode } : {}),
        ...(errorCode ? { error_code: errorCode } : {}),
        ...(routeKey ? { route_key: routeKey } : {}),
        ...(effectiveModel ? { effective_model: effectiveModel } : {}),
        ...(protocol ? { protocol } : {}),
        ...(upstreamPath ? { upstream_path: upstreamPath } : {}),
        ...(multiKeyIndex !== undefined
          ? { multi_key_index: multiKeyIndex }
          : {}),
      },
    ];
  });
}

export function getChannelHealthRuntimeEventDisplay(event, t) {
  switch (event.action) {
    case 'cooldown':
      return { label: t('渠道冷却'), color: 'orange' };
    case 'half_open_probe':
      return { label: t('半开探测'), color: 'cyan' };
    case 'half_open_busy':
      return { label: t('半开探测忙'), color: 'orange' };
    case 'half_open_recovered':
      return { label: t('半开恢复成功'), color: 'green' };
    case 'half_open_recovery_superseded':
      return { label: t('半开恢复已被覆盖'), color: 'red' };
    case 'half_open_released':
      return { label: t('半开探测已释放'), color: 'grey' };
    case 'healthy':
      return { label: t('健康门控通过'), color: 'blue' };
    case 'redis_unavailable':
      return { label: t('健康门控不可用'), color: 'red' };
    default:
      return { label: t('健康门控已执行'), color: 'grey' };
  }
}

export function getChannelHealthRuntimeRefreshDelay(runtime) {
  const entries = Array.isArray(runtime?.entries) ? runtime.entries : [];
  const hasTimeSensitiveState = entries.some(
    (entry) =>
      entry?.state === 'cooldown' || entry?.state === 'half_open_probe',
  );
  return runtime?.enabled && runtime.redis_available && hasTimeSensitiveState
    ? ACTIVE_RUNTIME_REFRESH_MS
    : IDLE_RUNTIME_REFRESH_MS;
}

export function summarizeChannelHealthRuntime(runtime) {
  const entries = Array.isArray(runtime?.entries) ? runtime.entries : [];
  const cooling = entries.filter((entry) => entry?.state === 'cooldown').length;
  const probing = entries.filter(
    (entry) => entry?.state === 'half_open_probe',
  ).length;
  const awaitingProbe = entries.filter(
    (entry) => entry?.state === 'awaiting_half_open_probe',
  ).length;

  if (!runtime?.enabled) {
    return { state: 'disabled', cooling, probing, awaitingProbe };
  }
  if (!runtime.redis_available) {
    return { state: 'redis_unavailable', cooling, probing, awaitingProbe };
  }
  return { state: 'active', cooling, probing, awaitingProbe };
}
