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

import { HEADER_PROFILE_PRESETS } from './headerProfile.constants.js';
import {
  CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS,
  CODEX_CLI_HEADER_PASSTHROUGH_HEADERS,
  GEMINI_CLI_HEADER_PASSTHROUGH_HEADERS,
  QWEN_CODE_CLI_HEADER_PASSTHROUGH_HEADERS,
} from '../../../../constants/channel-affinity-template.constants.js';

const STRATEGIES = new Set(['fixed', 'round_robin', 'random']);
const FALLBACK_CATEGORY = 'custom';
const FALLBACK_SCOPE = 'user';

function safeParseJson(text) {
  try {
    return JSON.parse(text);
  } catch (error) {
    return null;
  }
}

function stringifyJson(value) {
  return JSON.stringify(value);
}

function verifyJsonText(text) {
  return safeParseJson(text) !== null;
}

function parseHeadersText(text) {
  return safeParseJson(text);
}

export function buildHeaderProfilePreviewText(headers) {
  if (!headers || typeof headers !== 'object' || Array.isArray(headers)) {
    return '';
  }
  return Object.entries(headers)
    .map(([key, value]) => `${key}: ${value}`)
    .join('\n');
}

export function getHeaderProfileCategoryLabel(t, category) {
  const translate = typeof t === 'function' ? t : (key) => key;
  switch (category) {
    case 'browser':
      return translate('浏览器');
    case 'ai_coding_cli':
      return translate('AI Coding CLI');
    case 'api_sdk':
      return translate('API SDK / 调试');
    default:
      return translate('自定义');
  }
}

function isPlainHeaderObject(headers) {
  return !!headers && typeof headers === 'object' && !Array.isArray(headers);
}

function hasOnlyStringHeaderValues(headers) {
  return Object.values(headers).every((value) => typeof value === 'string');
}

function normalizeSelectedProfileIds(selectedProfileIds = []) {
  if (!Array.isArray(selectedProfileIds)) {
    return [];
  }
  return Array.from(
    new Set(
      selectedProfileIds
        .map((profileId) => String(profileId || '').trim())
        .filter(Boolean),
    ),
  );
}

function isBuiltinPassthroughProfileId(profileId) {
  return (
    profileId === 'codex-cli' ||
    profileId === 'claude-code' ||
    profileId === 'gemini-cli' ||
    profileId === 'qwen-code'
  );
}

function normalizeProfile(profile = {}) {
  const id = String(profile.id || profile.key || '').trim();
  const headers = isPlainHeaderObject(profile.headers) ? profile.headers : {};
  const normalized = {
    id,
    key: id,
    name: String(profile.name || id).trim(),
    category: String(profile.category || profile.group || FALLBACK_CATEGORY),
    scope: String(profile.scope || FALLBACK_SCOPE),
    readonly: profile.readonly === true || profile.readOnly === true,
    description: String(profile.description || '').trim(),
    headers,
    previewText: buildHeaderProfilePreviewText(headers),
  };
  if (
    profile.passthroughRequired === true ||
    profile.passthrough_required === true
  ) {
    normalized.passthroughRequired = true;
  }
  if (isBuiltinPassthroughProfileId(id)) {
    normalized.passthroughRequired = true;
  }
  return normalized;
}

function createMissingProfileItem(profileId) {
  return {
    id: profileId,
    key: profileId,
    name: profileId,
    category: FALLBACK_CATEGORY,
    scope: 'missing',
    readonly: true,
    headers: {},
    previewText: '',
    missing: true,
  };
}

function buildProfileMap(profiles = []) {
  const profileMap = new Map();
  for (const builtinProfile of Object.values(HEADER_PROFILE_PRESETS)) {
    const normalized = normalizeProfile(builtinProfile);
    profileMap.set(normalized.id, normalized);
  }
  for (const profile of profiles) {
    const normalized = normalizeProfile(profile);
    if (normalized.id) {
      profileMap.set(normalized.id, normalized);
    }
  }
  return profileMap;
}

export function normalizeHeaderProfileStrategy(strategy) {
  return STRATEGIES.has(strategy) ? strategy : 'fixed';
}

export function buildProfileItems(profiles = []) {
  return Array.from(buildProfileMap(profiles).values());
}

export function toggleSelectedProfile({
  strategy,
  selectedProfileIds = [],
  selectedProfileKeys = [],
  profileId,
  profileKey,
}) {
  const normalizedStrategy = normalizeHeaderProfileStrategy(strategy);
  const currentProfileId = profileId || profileKey;
  const currentSelectedProfileIds = normalizeSelectedProfileIds(
    selectedProfileIds.length > 0 ? selectedProfileIds : selectedProfileKeys,
  );
  if (!currentProfileId) {
    return [...currentSelectedProfileIds];
  }

  if (normalizedStrategy === 'fixed') {
    return [currentProfileId];
  }

  const nextKeys = [...currentSelectedProfileIds];
  const existingIndex = nextKeys.indexOf(currentProfileId);
  if (existingIndex >= 0) {
    nextKeys.splice(existingIndex, 1);
    return nextKeys;
  }

  nextKeys.push(currentProfileId);
  return nextKeys;
}

export function buildSelectedProfileItems(
  selectedProfileIds = [],
  profiles = [],
  snapshotProfiles = [],
) {
  const profileMap = buildProfileMap([...snapshotProfiles, ...profiles]);
  return normalizeSelectedProfileIds(selectedProfileIds).map((profileId) => {
    if (profileMap.has(profileId)) {
      return profileMap.get(profileId);
    }
    return createMissingProfileItem(profileId);
  });
}

export function reorderSelectedProfileIds(
  selectedProfileIds = [],
  sourceId,
  targetId,
  position = 'before',
) {
  if (!sourceId || !targetId || sourceId === targetId) {
    return [...selectedProfileIds];
  }
  const nextIds = [...selectedProfileIds];
  const sourceIndex = nextIds.indexOf(sourceId);
  const targetIndex = nextIds.indexOf(targetId);
  if (sourceIndex < 0 || targetIndex < 0) {
    return nextIds;
  }

  const [moved] = nextIds.splice(sourceIndex, 1);
  const adjustedTargetIndex =
    sourceIndex < targetIndex ? targetIndex - 1 : targetIndex;
  const insertIndex =
    position === 'after' ? adjustedTargetIndex + 1 : adjustedTargetIndex;
  nextIds.splice(insertIndex, 0, moved);
  return nextIds;
}

export function getHeaderProfileStrategyFromSettings(settingsText) {
  if (typeof settingsText !== 'string' || settingsText.trim() === '') {
    return null;
  }
  const parsedSettings = safeParseJson(settingsText);
  if (!isPlainHeaderObject(parsedSettings)) {
    return null;
  }
  const rawStrategy = parsedSettings.header_profile_strategy;
  if (!isPlainHeaderObject(rawStrategy)) {
    return null;
  }
  return {
    enabled: rawStrategy.enabled === true,
    mode: normalizeHeaderProfileStrategy(rawStrategy.mode),
    selectedProfileIds: normalizeSelectedProfileIds(
      rawStrategy.selected_profile_ids || rawStrategy.selectedProfileIds,
    ),
    profiles: Array.isArray(rawStrategy.profiles)
      ? rawStrategy.profiles
          .map(normalizeProfile)
          .filter((profile) => profile.id)
      : [],
  };
}

export function buildHeaderProfileStrategySettings(settingsText, strategy) {
  const parsedSettings = safeParseJson(settingsText);
  const nextSettings = isPlainHeaderObject(parsedSettings)
    ? parsedSettings
    : {};

  if (!strategy || typeof strategy !== 'object') {
    delete nextSettings.header_profile_strategy;
    return JSON.stringify(nextSettings);
  }

  nextSettings.header_profile_strategy = {
    enabled: strategy.enabled === true,
    mode: normalizeHeaderProfileStrategy(strategy.mode),
    selected_profile_ids: normalizeSelectedProfileIds(
      strategy.selectedProfileIds || strategy.selected_profile_ids,
    ),
    profiles: Array.isArray(strategy.profiles)
      ? strategy.profiles
          .map(normalizeProfile)
          .filter((profile) => profile.id)
          .map((profile) => {
            const snapshot = {
              id: profile.id,
              name: profile.name,
              category: profile.category,
              scope: profile.scope,
              readonly: profile.readonly,
              description: profile.description,
              headers: profile.headers,
            };
            if (profile.passthroughRequired === true) {
              snapshot.passthrough_required = true;
            }
            return snapshot;
          })
      : [],
  };
  return JSON.stringify(nextSettings);
}

function requiredPassthroughHeadersForProfile(profile) {
  switch (profile?.id) {
    case 'codex-cli':
      return CODEX_CLI_HEADER_PASSTHROUGH_HEADERS;
    case 'claude-code':
      return CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS;
    case 'gemini-cli':
      return GEMINI_CLI_HEADER_PASSTHROUGH_HEADERS;
    case 'qwen-code':
      return QWEN_CODE_CLI_HEADER_PASSTHROUGH_HEADERS;
    default:
      return Object.keys(profile?.headers || {}).sort();
  }
}

function collectRequiredPassthroughHeaders(selectedProfiles = []) {
  const headers = new Set();
  selectedProfiles.forEach((profile) => {
    if (profile?.passthroughRequired !== true) {
      return;
    }
    requiredPassthroughHeadersForProfile(profile).forEach((header) => {
      if (header) {
        headers.add(header);
      }
    });
  });
  return Array.from(headers);
}

function normalizeHeaderName(header) {
  return String(header || '').trim();
}

function normalizeHeaderNameKey(header) {
  return normalizeHeaderName(header).toLowerCase();
}

function normalizePassHeaderValue(value) {
  if (value == null) {
    return [];
  }
  if (Array.isArray(value)) {
    return value.map(normalizeHeaderName).filter(Boolean);
  }
  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (!trimmed) {
      return [];
    }
    if (trimmed.startsWith('[') || trimmed.startsWith('{')) {
      const parsed = safeParseJson(trimmed);
      if (parsed !== null) {
        return normalizePassHeaderValue(parsed);
      }
    }
    return value
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
  }
  if (value?.headers !== undefined) {
    return normalizePassHeaderValue(value.headers);
  }
  if (value?.names !== undefined) {
    return normalizePassHeaderValue(value.names);
  }
  if (value?.header !== undefined) {
    return normalizePassHeaderValue([value.header]);
  }
  return [];
}

function buildMergedPassHeaders(existingHeaders = [], requiredHeaders = []) {
  const seen = new Set();
  const mergedHeaders = [];
  const addHeader = (header) => {
    const normalized = normalizeHeaderName(header);
    if (!normalized) {
      return;
    }
    const key = normalizeHeaderNameKey(normalized);
    if (seen.has(key)) {
      return;
    }
    seen.add(key);
    mergedHeaders.push(normalized);
  };

  existingHeaders.forEach(addHeader);
  requiredHeaders.forEach(addHeader);
  return mergedHeaders;
}

function hasConditions(operation) {
  return (
    Array.isArray(operation?.conditions) && operation.conditions.length > 0
  );
}

function findUnconditionalPassHeaderOperation(operations = []) {
  return operations.find(
    (operation) =>
      operation?.mode === 'pass_headers' &&
      !hasConditions(operation) &&
      !operation?.when,
  );
}

function mergePassHeadersIntoOperations(operations = [], requiredHeaders = []) {
  if (requiredHeaders.length === 0) {
    return operations;
  }
  const passHeaderOperation = findUnconditionalPassHeaderOperation(operations);
  if (!passHeaderOperation) {
    return [
      {
        mode: 'pass_headers',
        value: [...requiredHeaders],
        keep_origin: true,
      },
      ...operations,
    ];
  }

  const mergedHeaders = buildMergedPassHeaders(
    normalizePassHeaderValue(passHeaderOperation.value),
    requiredHeaders,
  );
  return operations.map((operation) =>
    operation === passHeaderOperation
      ? {
          ...operation,
          value: mergedHeaders,
          keep_origin: operation.keep_origin !== false,
        }
      : operation,
  );
}

export function buildParamOverrideWithRequiredPassHeaders(
  paramOverrideText,
  requiredHeaders = [],
) {
  if (requiredHeaders.length === 0) {
    return paramOverrideText;
  }
  const rawText =
    typeof paramOverrideText === 'string' ? paramOverrideText.trim() : '';
  if (!rawText) {
    const nextParamOverride = {
      operations: mergePassHeadersIntoOperations([], requiredHeaders),
    };
    return stringifyJson(nextParamOverride);
  }
  const parsed = safeParseJson(rawText);
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return paramOverrideText;
  }
  const nextParamOverride = parsed;
  const operations = Array.isArray(nextParamOverride.operations)
    ? nextParamOverride.operations
    : [];
  nextParamOverride.operations = mergePassHeadersIntoOperations(
    operations,
    requiredHeaders,
  );
  return stringifyJson(nextParamOverride);
}

export function applyHeaderProfileStrategyToChannelInputs({
  inputs = {},
  strategy,
  headerProfiles = [],
  snapshotProfiles = [],
}) {
  const selectedProfileIds = strategy?.selectedProfileIds || [];
  const selectedProfiles = buildSelectedProfileItems(
    selectedProfileIds,
    headerProfiles,
    snapshotProfiles,
  ).filter((profile) => !profile.missing);
  const settings = buildHeaderProfileStrategySettings(
    inputs.settings,
    strategy ? { ...strategy, profiles: selectedProfiles } : null,
  );
  const requiredHeaders =
    strategy?.enabled === true
      ? collectRequiredPassthroughHeaders(selectedProfiles)
      : [];
  return {
    settings,
    param_override: buildParamOverrideWithRequiredPassHeaders(
      inputs.param_override,
      requiredHeaders,
    ),
  };
}

const HIDDEN_SUBMIT_STATE_FIELDS = [
  'settings',
  'param_override',
  'header_override',
  'status_code_mapping',
];

function isEmptyHiddenSubmitValue(value) {
  if (typeof value !== 'string') {
    return false;
  }
  const trimmed = value.trim();
  return trimmed === '' || trimmed === '{}';
}

export function mergeChannelSubmitFormValues(formValues = {}, inputs = {}) {
  const merged = { ...inputs, ...formValues };

  HIDDEN_SUBMIT_STATE_FIELDS.forEach((field) => {
    const inputValue = inputs?.[field];
    if (inputValue === undefined || inputValue === null) {
      return;
    }

    const formValue = formValues?.[field];
    const formValueMissing =
      formValue === undefined ||
      formValue === null ||
      isEmptyHiddenSubmitValue(formValue);

    if (formValueMissing) {
      merged[field] = inputValue;
    }
  });

  return merged;
}

export function createLegacyHeaderProfileDraft(headerOverrideText) {
  const rawText =
    typeof headerOverrideText === 'string' ? headerOverrideText.trim() : '';
  if (!rawText || !verifyJsonText(rawText)) {
    return null;
  }

  const parsedHeaders = JSON.parse(rawText);
  if (
    !parsedHeaders ||
    typeof parsedHeaders !== 'object' ||
    Array.isArray(parsedHeaders)
  ) {
    return null;
  }

  return {
    mode: 'custom',
    category: 'custom',
    name: 'Legacy Header Override',
    headersText: JSON.stringify(parsedHeaders, null, 2),
    source: 'legacy_header_override',
  };
}

export function validateHeaderProfileDraft(draft = {}, options = {}) {
  const errors = {};
  const name = typeof draft.name === 'string' ? draft.name.trim() : '';
  const headersText =
    typeof draft.headersText === 'string' ? draft.headersText.trim() : '';
  const currentProfileId = String(
    options.currentProfileId || draft.id || '',
  ).trim();
  const existingProfiles = Array.isArray(options.profiles)
    ? options.profiles
    : [];

  if (!name) {
    errors.name = '名称不能为空';
  } else if (
    existingProfiles.some((profile) => {
      const normalized = normalizeProfile(profile);
      return normalized.id !== currentProfileId && normalized.name === name;
    })
  ) {
    errors.name = '名称已存在';
  }

  if (!headersText) {
    errors.headersText = 'Headers JSON 不能为空';
    return {
      isValid: false,
      errors,
      parsedHeaders: null,
    };
  }

  if (!verifyJsonText(headersText)) {
    errors.headersText = 'Headers JSON 必须是合法 JSON';
    return {
      isValid: false,
      errors,
      parsedHeaders: null,
    };
  }

  const parsedHeaders = parseHeadersText(headersText);
  if (
    !isPlainHeaderObject(parsedHeaders) ||
    Object.keys(parsedHeaders).length === 0
  ) {
    errors.headersText = 'Headers JSON 必须是非空对象';
  } else if (!hasOnlyStringHeaderValues(parsedHeaders)) {
    errors.headersText = 'Headers JSON 的值必须全部是字符串';
  }

  return {
    isValid: Object.keys(errors).length === 0,
    errors,
    parsedHeaders: errors.headersText ? null : parsedHeaders,
  };
}
