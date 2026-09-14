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

function readTrimmedString(value) {
  if (typeof value !== 'string') {
    return undefined;
  }
  const normalized = value.trim();
  return normalized || undefined;
}

function readNonNegativeInteger(value) {
  const normalized =
    typeof value === 'number'
      ? value
      : typeof value === 'string' && value.trim()
        ? Number(value)
        : Number.NaN;
  if (!Number.isSafeInteger(normalized) || normalized < 0) {
    return undefined;
  }
  return normalized;
}

function readPositiveInteger(value) {
  const normalized = readNonNegativeInteger(value);
  return normalized && normalized > 0 ? normalized : undefined;
}

function getChannelHealthInfo(other) {
  if (!other || typeof other !== 'object') {
    return undefined;
  }
  const info = other.admin_info?.channel_health;
  return info && typeof info === 'object' ? info : undefined;
}

function eventKey(event) {
  return [
    event.group ?? '',
    event.modelName ?? '',
    event.channelId,
    event.action,
    event.statusCode ?? '',
    event.errorCode ?? '',
    event.routeKey ?? '',
    event.effectiveModel ?? '',
    event.protocol ?? '',
    event.upstreamPath ?? '',
    event.multiKeyIndex ?? '',
  ].join('|');
}

export function getChannelHealthRoutingEvents(other) {
  const info = getChannelHealthInfo(other);
  if (!info) {
    return [];
  }

  const events = [];
  const seen = new Set();
  const rawEvents = Array.isArray(info.events) ? info.events : [];
  for (const rawEvent of rawEvents) {
    const action = readTrimmedString(rawEvent?.action);
    if (!action) {
      continue;
    }

    const channelId = readNonNegativeInteger(rawEvent?.channel_id);
    if (channelId === undefined && action !== 'redis_unavailable') {
      continue;
    }

    const routeKey = readTrimmedString(rawEvent?.route_key);
    const effectiveModel = readTrimmedString(rawEvent?.effective_model);
    const protocol = readTrimmedString(rawEvent?.protocol);
    const upstreamPath = readTrimmedString(rawEvent?.upstream_path);
    const multiKeyIndex = readNonNegativeInteger(rawEvent?.multi_key_index);
    const event = {
      group: readTrimmedString(rawEvent?.group),
      modelName: readTrimmedString(rawEvent?.model_name),
      channelId: channelId ?? 0,
      action,
      statusCode: readPositiveInteger(rawEvent?.status_code),
      errorCode: readTrimmedString(rawEvent?.error_code),
      ...(routeKey ? { routeKey } : {}),
      ...(effectiveModel ? { effectiveModel } : {}),
      ...(protocol ? { protocol } : {}),
      ...(upstreamPath ? { upstreamPath } : {}),
      ...(multiKeyIndex !== undefined ? { multiKeyIndex } : {}),
    };
    const key = eventKey(event);
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    events.push(event);
  }

  if (
    info.redis_unavailable === true &&
    !events.some((event) => event.action === 'redis_unavailable')
  ) {
    events.push({
      group: undefined,
      modelName: undefined,
      channelId: 0,
      action: 'redis_unavailable',
      statusCode: undefined,
      errorCode: undefined,
    });
  }

  return events;
}

function hasAction(events, action) {
  return events.some((event) => event.action === action);
}

export function getChannelHealthRoutingResult(other) {
  const events = getChannelHealthRoutingEvents(other);
  if (events.length === 0) {
    return null;
  }

  const actionCount = events.length;
  if (hasAction(events, 'redis_unavailable')) {
    return { kind: 'gate_unavailable', actionCount };
  }
  if (hasAction(events, 'half_open_recovery_superseded')) {
    return { kind: 'probe_recovery_superseded', actionCount };
  }
  if (hasAction(events, 'cooldown')) {
    return { kind: 'channel_cooling', actionCount };
  }
  if (hasAction(events, 'half_open_busy')) {
    return { kind: 'probe_busy', actionCount };
  }
  if (hasAction(events, 'half_open_recovered')) {
    return { kind: 'probe_recovered', actionCount };
  }
  if (hasAction(events, 'half_open_probe')) {
    return { kind: 'probe_active', actionCount };
  }
  if (hasAction(events, 'half_open_released')) {
    return { kind: 'probe_released', actionCount };
  }
  if (hasAction(events, 'affinity_bypassed')) {
    return { kind: 'affinity_bypassed', actionCount };
  }
  if (hasAction(events, 'healthy')) {
    return { kind: 'healthy', actionCount };
  }
  return { kind: 'health_action', actionCount };
}

export function getChannelHealthRoutingResultDisplay(result, t) {
  switch (result.kind) {
    case 'healthy':
      return { label: t('健康门控通过'), color: 'blue' };
    case 'gate_unavailable':
      return { label: t('健康门控不可用'), color: 'red' };
    case 'probe_recovery_superseded':
      return { label: t('半开恢复已被覆盖'), color: 'red' };
    case 'channel_cooling':
      return { label: t('渠道冷却'), color: 'orange' };
    case 'probe_busy':
      return { label: t('半开探测忙'), color: 'orange' };
    case 'probe_recovered':
      return { label: t('半开恢复成功'), color: 'green' };
    case 'probe_active':
      return { label: t('半开探测'), color: 'cyan' };
    case 'probe_released':
      return { label: t('半开探测已释放'), color: 'grey' };
    case 'affinity_bypassed':
      return { label: t('亲和性已绕过'), color: 'blue' };
    default:
      return { label: t('健康门控已执行'), color: 'grey' };
  }
}

export function getChannelHealthRoutingResultImpact(result, t) {
  switch (result.kind) {
    case 'gate_unavailable':
      return t('健康筛选不可用，路由未应用临时健康绕过。');
    case 'probe_recovery_superseded':
      return t('较新的合格失败保留了临时绕过，本次恢复探测没有恢复该渠道。');
    case 'channel_cooling':
      return t('合格失败已开始或续期冷却，影响相同分组和模型的后续请求。');
    case 'probe_busy':
      return t('另一个请求正在执行恢复探测，本请求绕过了该渠道。');
    case 'probe_recovered':
      return t('恢复探测成功，已清除相同分组和模型的临时绕过。');
    case 'probe_active':
      return t('本请求持有恢复探测，其他匹配请求会绕过该渠道。');
    case 'probe_released':
      return t('恢复探测已释放，之前的失败历史没有改变。');
    case 'affinity_bypassed':
      return t('由于所选渠道有活动健康绕过，渠道亲和性被绕过。');
    case 'healthy':
      return t('该选择点没有活动的临时健康绕过影响此渠道。');
    default:
      return t('本请求记录了健康路由动作。');
  }
}

export function getChannelHealthRoutingEventImpact(event, t) {
  switch (event.action) {
    case 'cooldown':
      return t('合格失败已开始或续期冷却，影响相同分组和模型的后续请求。');
    case 'half_open_probe':
      return t('本请求持有恢复探测，其他匹配请求会绕过该渠道。');
    case 'half_open_busy':
      return t('另一个请求正在执行恢复探测，本请求绕过了该渠道。');
    case 'half_open_recovered':
      return t('恢复探测成功，已清除相同分组和模型的临时绕过。');
    case 'half_open_recovery_superseded':
      return t('较新的合格失败保留了临时绕过，本次恢复探测没有恢复该渠道。');
    case 'half_open_released':
      return t('恢复探测已释放，之前的失败历史没有改变。');
    case 'affinity_bypassed':
      return t('由于所选渠道有活动健康绕过，渠道亲和性被绕过。');
    case 'healthy':
      return t('该选择点没有活动的临时健康绕过影响此渠道。');
    case 'redis_unavailable':
      return t('健康筛选不可用，路由未应用临时健康绕过。');
    default:
      return t('本请求记录了健康路由动作。');
  }
}

function formatEventContext(event, t) {
  if (event.action === 'redis_unavailable') {
    return t('Redis 不可用，路由继续执行');
  }

  if (event.action === 'healthy') {
    const details = [
      t('渠道 #{{channel}}', { channel: event.channelId }),
      t('当前没有活动冷却或探测状态'),
    ];
    if (event.group && event.modelName) {
      details.push(
        t('路由范围：{{group}} / {{model}}', {
          group: event.group,
          model: event.modelName,
        }),
      );
    }
    return details.join('；');
  }

  const details = [];
  if (event.channelId > 0) {
    details.push(t('渠道 #{{channel}}', { channel: event.channelId }));
  }
  if (event.group && event.modelName) {
    details.push(
      t('路由范围：{{group}} / {{model}}', {
        group: event.group,
        model: event.modelName,
      }),
    );
  }
  if (event.statusCode !== undefined) {
    details.push(t('状态码：{{status}}', { status: event.statusCode }));
  }
  if (event.errorCode) {
    details.push(t('错误：{{error}}', { error: event.errorCode }));
  }
  const route = [
    event.effectiveModel ? `model=${event.effectiveModel}` : '',
    event.protocol ? `protocol=${event.protocol}` : '',
    event.upstreamPath ? `path=${event.upstreamPath}` : '',
    Number.isInteger(event.multiKeyIndex) ? `key=${event.multiKeyIndex}` : '',
    event.routeKey ? `route=${event.routeKey.slice(0, 12)}` : '',
  ].filter(Boolean);
  if (route.length > 0) details.push(route.join(' | '));
  return details.length > 0 ? details.join('；') : t('无额外详情');
}

export function getChannelHealthRoutingEventDisplay(event, t) {
  const value = formatEventContext(event, t);
  switch (event.action) {
    case 'healthy':
      return { label: t('健康门控通过'), value };
    case 'cooldown':
      return { label: t('渠道冷却'), value };
    case 'half_open_probe':
      return { label: t('半开探测'), value };
    case 'half_open_busy':
      return { label: t('半开探测忙'), value };
    case 'half_open_recovered':
      return { label: t('半开恢复成功'), value };
    case 'half_open_recovery_superseded':
      return { label: t('半开恢复已被覆盖'), value };
    case 'half_open_released':
      return { label: t('半开探测已释放'), value };
    case 'affinity_bypassed':
      return { label: t('亲和性已绕过'), value };
    case 'redis_unavailable':
      return { label: t('健康门控不可用'), value };
    default:
      return {
        label: t('未知健康动作：{{action}}', { action: event.action }),
        value,
      };
  }
}

export function getChannelHealthRoutingEventTone(event) {
  switch (event.action) {
    case 'healthy':
      return 'bypassed';
    case 'redis_unavailable':
    case 'half_open_recovery_superseded':
      return 'failure';
    case 'cooldown':
    case 'half_open_busy':
      return 'warning';
    case 'half_open_recovered':
      return 'success';
    case 'half_open_probe':
      return 'info';
    case 'affinity_bypassed':
      return 'bypassed';
    default:
      return 'neutral';
  }
}

export function shouldIncludeChannelHealthRoutingColumn(isAdminUser) {
  return isAdminUser === true;
}
