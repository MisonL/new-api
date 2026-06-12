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

export const RESPONSES_COMPACT_MODE_AUTO = 'auto';
export const RESPONSES_COMPACT_MODE_NATIVE = 'native';
export const RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY = 'synthetic_summary';
export const RESPONSES_COMPACT_MODE_DEFAULT = RESPONSES_COMPACT_MODE_AUTO;
export const RESPONSES_COMPACT_CONTEXT_FALLBACK_DEFAULT = true;
export const RESPONSES_COMPACT_SUMMARY_MODEL_FALLBACK_DEFAULT = true;
export const RESPONSES_COMPACT_SUMMARY_FALLBACK_MODELS_DEFAULT = ['gpt-5.4'];
export const RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_DEFAULT = 3;
export const RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_MIN = 1;
export const RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_MAX = 168;
export const RESPONSES_UPSTREAM_PROFILE_DEFAULT = '';
export const RESPONSES_UPSTREAM_PROFILE_SELECT_DEFAULT = '__default__';
export const RESPONSES_UPSTREAM_PROFILE_OFFICIAL_OPENAI = 'official_openai';
export const RESPONSES_UPSTREAM_PROFILE_OFFICIAL_NEWAPI = 'official_newapi';
export const RESPONSES_UPSTREAM_PROFILE_SAME_CLUSTER_NEWAPI =
  'same_cluster_newapi';
export const RESPONSES_UPSTREAM_PROFILE_TRUSTED_NEWAPI = 'trusted_newapi';
export const RESPONSES_UPSTREAM_PROFILE_SUB2API_HTTP = 'sub2api_http';
export const RESPONSES_UPSTREAM_PROFILE_SUB2API_WSV2 = 'sub2api_wsv2';
export const RESPONSES_UPSTREAM_PROFILE_GENERIC_PROXY = 'generic_proxy';
export const RESPONSES_UPSTREAM_PROFILE_CHAT_ONLY_PROXY = 'chat_only_proxy';

export const RESPONSES_COMPACT_MODE_OPTIONS = [
  {
    label: '自动：优先原生，失败回退模拟摘要',
    value: RESPONSES_COMPACT_MODE_AUTO,
  },
  {
    label: '原生 /v1/responses/compact',
    value: RESPONSES_COMPACT_MODE_NATIVE,
  },
  {
    label: '模拟摘要兼容',
    value: RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
  },
];

export const RESPONSES_UPSTREAM_PROFILE_OPTIONS = [
  {
    label: '默认行为',
    value: RESPONSES_UPSTREAM_PROFILE_SELECT_DEFAULT,
  },
  {
    label: '官方 OpenAI',
    value: RESPONSES_UPSTREAM_PROFILE_OFFICIAL_OPENAI,
  },
  {
    label: '可信 New API',
    value: RESPONSES_UPSTREAM_PROFILE_TRUSTED_NEWAPI,
  },
  {
    label: '官方 New API',
    value: RESPONSES_UPSTREAM_PROFILE_OFFICIAL_NEWAPI,
  },
  {
    label: '同集群 New API',
    value: RESPONSES_UPSTREAM_PROFILE_SAME_CLUSTER_NEWAPI,
  },
  {
    label: 'Sub2API HTTP',
    value: RESPONSES_UPSTREAM_PROFILE_SUB2API_HTTP,
  },
  {
    label: 'Sub2API WSv2',
    value: RESPONSES_UPSTREAM_PROFILE_SUB2API_WSV2,
  },
  {
    label: '通用代理',
    value: RESPONSES_UPSTREAM_PROFILE_GENERIC_PROXY,
  },
  {
    label: '仅 Chat 代理',
    value: RESPONSES_UPSTREAM_PROFILE_CHAT_ONLY_PROXY,
  },
];

export function normalizeResponsesCompactMode(mode) {
  if (mode === RESPONSES_COMPACT_MODE_AUTO) {
    return RESPONSES_COMPACT_MODE_AUTO;
  }
  if (mode === RESPONSES_COMPACT_MODE_NATIVE) {
    return RESPONSES_COMPACT_MODE_NATIVE;
  }
  if (mode === RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY) {
    return RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY;
  }
  if (mode === 'convert') {
    return RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY;
  }
  if (mode === 'disabled' || mode === 'unsupported') {
    // Legacy disabled/unsupported meant no synthetic conversion; keep the native endpoint path.
    return RESPONSES_COMPACT_MODE_NATIVE;
  }
  return RESPONSES_COMPACT_MODE_DEFAULT;
}

export function normalizeResponsesUpstreamProfile(profile) {
  if (profile === RESPONSES_UPSTREAM_PROFILE_OFFICIAL_OPENAI) {
    return RESPONSES_UPSTREAM_PROFILE_OFFICIAL_OPENAI;
  }
  if (profile === RESPONSES_UPSTREAM_PROFILE_OFFICIAL_NEWAPI) {
    return RESPONSES_UPSTREAM_PROFILE_OFFICIAL_NEWAPI;
  }
  if (profile === RESPONSES_UPSTREAM_PROFILE_SAME_CLUSTER_NEWAPI) {
    return RESPONSES_UPSTREAM_PROFILE_SAME_CLUSTER_NEWAPI;
  }
  if (profile === RESPONSES_UPSTREAM_PROFILE_TRUSTED_NEWAPI) {
    return RESPONSES_UPSTREAM_PROFILE_TRUSTED_NEWAPI;
  }
  if (profile === RESPONSES_UPSTREAM_PROFILE_SUB2API_HTTP) {
    return RESPONSES_UPSTREAM_PROFILE_SUB2API_HTTP;
  }
  if (profile === RESPONSES_UPSTREAM_PROFILE_SUB2API_WSV2) {
    return RESPONSES_UPSTREAM_PROFILE_SUB2API_WSV2;
  }
  if (profile === RESPONSES_UPSTREAM_PROFILE_GENERIC_PROXY) {
    return RESPONSES_UPSTREAM_PROFILE_GENERIC_PROXY;
  }
  if (profile === RESPONSES_UPSTREAM_PROFILE_CHAT_ONLY_PROXY) {
    return RESPONSES_UPSTREAM_PROFILE_CHAT_ONLY_PROXY;
  }
  return RESPONSES_UPSTREAM_PROFILE_DEFAULT;
}

export function hasResponsesProxyCompatibilityProfile(profile) {
  const normalized = normalizeResponsesUpstreamProfile(profile);
  return (
    normalized === RESPONSES_UPSTREAM_PROFILE_GENERIC_PROXY ||
    normalized === RESPONSES_UPSTREAM_PROFILE_CHAT_ONLY_PROXY
  );
}

export function getResponsesCompactModeFromSettings(settings = {}) {
  const mode = normalizeResponsesCompactMode(settings.responses_compact_mode);
  if (
    hasResponsesProxyCompatibilityProfile(settings.responses_upstream_profile)
  ) {
    return RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY;
  }
  return mode;
}

export function normalizeResponsesCompactSummaryFallbackModels(models) {
  const rawModels = Array.isArray(models)
    ? models
    : String(models || '').split(',');
  const seen = new Set();
  const normalized = [];
  for (const model of rawModels) {
    const value = String(model || '').trim();
    if (!value || seen.has(value)) {
      continue;
    }
    seen.add(value);
    normalized.push(value);
  }
  return normalized.length > 0
    ? normalized
    : [...RESPONSES_COMPACT_SUMMARY_FALLBACK_MODELS_DEFAULT];
}

export function normalizeResponsesCompactAutoFallbackRetryIntervalHours(hours) {
  const parsed = Number(hours);
  if (!Number.isFinite(parsed) || parsed === 0) {
    return RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_DEFAULT;
  }
  const rounded = Math.trunc(parsed);
  if (rounded < RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_MIN) {
    return RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_MIN;
  }
  if (rounded > RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_MAX) {
    return RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_MAX;
  }
  return rounded;
}

export function buildResponsesCompactSettings(
  channelType,
  mode,
  contextFallback = RESPONSES_COMPACT_CONTEXT_FALLBACK_DEFAULT,
  summaryModelFallback = RESPONSES_COMPACT_SUMMARY_MODEL_FALLBACK_DEFAULT,
  summaryFallbackModels = RESPONSES_COMPACT_SUMMARY_FALLBACK_MODELS_DEFAULT,
  autoFallbackRetryIntervalHours = RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_DEFAULT,
  upstreamProfile = RESPONSES_UPSTREAM_PROFILE_DEFAULT,
) {
  if (channelType !== 1) {
    return {};
  }
  const settings = {
    responses_compact_mode: normalizeResponsesCompactMode(mode),
    responses_compact_auto_fallback_retry_interval_hours:
      normalizeResponsesCompactAutoFallbackRetryIntervalHours(
        autoFallbackRetryIntervalHours,
      ),
    responses_compact_context_fallback: contextFallback !== false,
    responses_compact_summary_model_fallback: summaryModelFallback !== false,
    responses_compact_summary_fallback_models:
      normalizeResponsesCompactSummaryFallbackModels(summaryFallbackModels),
  };
  const normalizedProfile = normalizeResponsesUpstreamProfile(upstreamProfile);
  if (normalizedProfile) {
    settings.responses_upstream_profile = normalizedProfile;
  }
  return settings;
}

export function clearResponsesCompactSettings(settings) {
  if (!settings || typeof settings !== 'object') {
    return;
  }
  delete settings.responses_compact_mode;
  delete settings.responses_compact_auto_fallback_date;
  delete settings.responses_compact_auto_fallback_at;
  delete settings.responses_compact_auto_fallback_reason;
  delete settings.responses_compact_auto_fallback_retry_interval_hours;
  delete settings.responses_compact_context_fallback;
  delete settings.responses_compact_summary_model_fallback;
  delete settings.responses_compact_summary_fallback_models;
  delete settings.responses_upstream_profile;
}

export function resetResponsesCompactAutoFallbackOnModeChange(
  settings,
  mode,
  previousModeOverride,
) {
  if (!settings || typeof settings !== 'object') {
    return false;
  }
  const nextMode = normalizeResponsesCompactMode(mode);
  const previousMode = normalizeResponsesCompactMode(
    previousModeOverride ?? settings?.responses_compact_mode,
  );
  if (nextMode !== previousMode) {
    delete settings.responses_compact_auto_fallback_date;
    delete settings.responses_compact_auto_fallback_at;
    delete settings.responses_compact_auto_fallback_reason;
    return true;
  }
  return false;
}
