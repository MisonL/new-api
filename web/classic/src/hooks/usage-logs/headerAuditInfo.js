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

export function formatRequestHeaderPolicyMode(mode, t) {
  switch (mode) {
    case 'prefer_channel':
      return t('渠道优先');
    case 'prefer_tag':
      return t('标签优先');
    case 'merge':
      return t('合并');
    default:
      return String(mode || '').trim();
  }
}

export function normalizeHeaderKeyList(value) {
  if (!Array.isArray(value)) {
    return [];
  }

  const seen = new Set();
  const keys = [];
  value.forEach((item) => {
    const key = String(item || '').trim();
    if (!key) {
      return;
    }
    const normalized = key.toLowerCase();
    if (seen.has(normalized)) {
      return;
    }
    seen.add(normalized);
    keys.push(key);
  });
  return keys;
}

export function isUserAgentHeaderKey(key) {
  return (
    String(key || '')
      .trim()
      .toLowerCase() === 'user-agent'
  );
}

export function getAppliedUserAgent(policy) {
  return String(
    policy?.applied_user_agent || policy?.selected_user_agent || '',
  ).trim();
}

export function getAppliedHeaderKeys(policy) {
  return normalizeHeaderKeyList(policy?.applied_header_keys);
}

export function normalizeHeaderEntryList(value) {
  if (!Array.isArray(value)) {
    return [];
  }

  const seen = new Set();
  const entries = [];
  value.forEach((item) => {
    const key = String(item?.key || '').trim();
    if (!key) {
      return;
    }
    const normalized = key.toLowerCase();
    if (seen.has(normalized)) {
      return;
    }
    seen.add(normalized);
    entries.push({
      key,
      value: String(item?.value || '').trim(),
    });
  });
  return entries;
}

export function getAppliedHeaderEntries(policy) {
  const entries = normalizeHeaderEntryList(policy?.applied_headers);
  if (entries.length) {
    return entries;
  }
  return getAppliedHeaderKeys(policy).map((key) => ({ key, value: '' }));
}

const legacyRequestHeaderPolicyKeys = [
  'header_profile_id',
  'header_profile_mode',
  'header_profile_applied',
  'ua_strategy_mode',
  'ua_strategy_scope',
  'selected_user_agent',
  'applied_user_agent',
  'override_static_user_agent',
  'user_agent_applied',
  'applied_header_keys',
  'applied_headers',
];

const legacyRequestHeaderPolicyBooleanKeys = new Set([
  'header_profile_applied',
  'override_static_user_agent',
  'user_agent_applied',
]);

function hasAuditValue(value) {
  if (value === undefined || value === null) {
    return false;
  }
  if (typeof value === 'string') {
    return value.trim() !== '';
  }
  if (Array.isArray(value)) {
    return value.length > 0;
  }
  if (typeof value === 'boolean') {
    return value;
  }
  if (typeof value === 'object') {
    return Object.keys(value).length > 0;
  }
  return true;
}

function hasOwnAuditField(source, key) {
  return Object.prototype.hasOwnProperty.call(source, key);
}

function hasLegacyAuditValue(source, key) {
  if (legacyRequestHeaderPolicyBooleanKeys.has(key)) {
    return (
      hasOwnAuditField(source, key) &&
      source[key] !== undefined &&
      source[key] !== null
    );
  }
  return hasAuditValue(source[key]);
}

export function normalizeRequestHeaderPolicyFromLogOther(other) {
  const policy = other?.request_header_policy;
  if (policy && typeof policy === 'object' && hasAuditValue(policy)) {
    return policy;
  }
  if (!other || typeof other !== 'object') {
    return null;
  }

  const fallback = {};
  const mode = other.header_policy_mode || other.request_header_policy_mode;
  if (hasAuditValue(mode)) {
    fallback.mode = mode;
  }
  legacyRequestHeaderPolicyKeys.forEach((key) => {
    const value = other[key];
    if (hasLegacyAuditValue(other, key)) {
      fallback[key] = value;
    }
  });

  return Object.keys(fallback).length ? fallback : null;
}

export function buildRequestHeaderAuditLines(
  policy,
  scope = 'all',
  t = (v) => v,
) {
  if (!policy || typeof policy !== 'object') {
    return [];
  }

  const normalizedScope = ['all', 'user-agent', 'headers'].includes(scope)
    ? scope
    : 'all';
  const includeUserAgent =
    normalizedScope === 'all' || normalizedScope === 'user-agent';
  const includeHeaders =
    normalizedScope === 'all' || normalizedScope === 'headers';
  const modeLabel = formatRequestHeaderPolicyMode(policy.mode, t);
  const profileId = String(policy.header_profile_id || '').trim();
  const userAgent = getAppliedUserAgent(policy);
  const headerEntries = getAppliedHeaderEntries(policy).filter(
    ({ key }) => !isUserAgentHeaderKey(key),
  );
  const headerKeys = headerEntries.map(({ key }) => key);
  const headerValue = headerEntries.some(({ value }) => value)
    ? headerEntries
        .map(({ key, value }) => (value ? `${key}: ${value}` : key))
        .join('\n')
    : headerKeys.join(', ');

  return [
    modeLabel
      ? { key: 'mode', label: t('请求头策略'), value: modeLabel }
      : null,
    profileId
      ? {
          key: 'profile',
          label: t('请求头模板'),
          value: profileId,
        }
      : null,
    includeUserAgent && userAgent
      ? {
          key: 'user-agent',
          label: t('已选 UA'),
          value: userAgent,
          copyMessage: t('已复制 UA'),
        }
      : null,
    includeHeaders && headerKeys.length
      ? {
          key: 'headers',
          label: t('应用请求头'),
          value: headerValue,
          items: headerEntries,
          copyMessage: t('已复制请求头'),
        }
      : null,
  ].filter(Boolean);
}
