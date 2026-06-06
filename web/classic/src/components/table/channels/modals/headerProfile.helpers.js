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
  AI_CODING_CLI_DEFAULT_PLATFORM,
  buildAiCodingCliVersionMeta,
  buildVersionedAiCodingCliProfile,
  HEADER_PROFILE_PRESETS,
  NPM_VERSION_LATEST_ALIAS,
  normalizeAiCodingCliPlatform,
} from './headerProfile.constants.js';

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

function isBuiltinAiCodingCliProfileId(profileId) {
  const baseProfileId = getBaseProfileIdFromProfileId(profileId);
  return [
    'codex-cli',
    'codex-desktop',
    'claude-code',
    'gemini-cli',
    'qwen-code',
    'droid',
  ].includes(baseProfileId);
}

function getBaseProfileIdFromProfileId(profileId) {
  const normalizedProfileId = String(profileId || '').trim();
  const separatorIndex = normalizedProfileId.indexOf('@');
  if (
    separatorIndex <= 0 ||
    separatorIndex !== normalizedProfileId.lastIndexOf('@')
  ) {
    return normalizedProfileId;
  }
  return normalizedProfileId.slice(0, separatorIndex).trim();
}

function normalizeVersionMeta(profile = {}) {
  const rawMeta = profile.versionMeta || profile.version_meta;
  if (!rawMeta || typeof rawMeta !== 'object' || Array.isArray(rawMeta)) {
    return null;
  }
  const version = String(rawMeta.version || '').trim();
  const profileId = String(profile.id || profile.key || '').trim();
  const inferredBaseProfileId = getBaseProfileIdFromProfileId(profileId);
  const baseProfileId = String(
    rawMeta.baseProfileId ||
      rawMeta.base_profile_id ||
      inferredBaseProfileId ||
      '',
  ).trim();
  let packageName = String(
    rawMeta.packageName || rawMeta.package_name || '',
  ).trim();
  if (!packageName && baseProfileId) {
    packageName =
      HEADER_PROFILE_PRESETS[baseProfileId]?.versionSource?.packageName || '';
  }
  if (!version || !baseProfileId) {
    return null;
  }
  return {
    baseProfileId,
    packageName,
    source: String(rawMeta.source || '').trim() || 'npm',
    version,
    platform: normalizeAiCodingCliPlatform(rawMeta.platform),
  };
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
  const versionMeta = normalizeVersionMeta(profile);
  if (versionMeta) {
    normalized.versionMeta = versionMeta;
  }
  if (
    profile.versionSource &&
    typeof profile.versionSource === 'object' &&
    !Array.isArray(profile.versionSource)
  ) {
    normalized.versionSource = {
      packageName: String(profile.versionSource.packageName || '').trim(),
      fallbackVersion: String(
        profile.versionSource.fallbackVersion || '',
      ).trim(),
    };
  }
  if (
    !isBuiltinAiCodingCliProfileId(id) &&
    (profile.passthroughRequired === true ||
      profile.passthrough_required === true)
  ) {
    normalized.passthroughRequired = true;
  }
  return normalized;
}

function getDefaultVersionMeta(profile) {
  if (!profile || profile.versionMeta) {
    return profile?.versionMeta || null;
  }
  return buildAiCodingCliVersionMeta(
    profile,
    NPM_VERSION_LATEST_ALIAS,
    AI_CODING_CLI_DEFAULT_PLATFORM,
  );
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

function buildProfileMap(profiles = [], options = {}) {
  const profileMap = new Map();
  const includeVersionedAliases = options.includeVersionedAliases === true;
  for (const builtinProfile of Object.values(HEADER_PROFILE_PRESETS)) {
    const normalized = normalizeProfile(builtinProfile);
    profileMap.set(normalized.id, normalized);
    if (!includeVersionedAliases) {
      continue;
    }
    const latestProfile = buildVersionedAiCodingCliProfile(
      builtinProfile,
      NPM_VERSION_LATEST_ALIAS,
    );
    if (latestProfile !== builtinProfile) {
      const normalizedLatest = normalizeProfile(latestProfile);
      profileMap.set(normalizedLatest.id, normalizedLatest);
    }
  }
  for (const profile of profiles) {
    const normalized = normalizeProfile(profile);
    if (normalized.id) {
      profileMap.set(normalized.id, normalized);
    }
  }
  return profileMap;
}

function resolveBuiltinVersionedProfile(profileId) {
  const normalizedProfileId = String(profileId || '').trim();
  const separatorIndex = normalizedProfileId.indexOf('@');
  if (
    separatorIndex <= 0 ||
    separatorIndex !== normalizedProfileId.lastIndexOf('@')
  ) {
    return null;
  }
  const baseProfileId = normalizedProfileId.slice(0, separatorIndex).trim();
  const version = normalizedProfileId.slice(separatorIndex + 1).trim();
  const baseProfile = HEADER_PROFILE_PRESETS[baseProfileId];
  if (!baseProfile || !version) {
    return null;
  }
  const versionedProfile = buildVersionedAiCodingCliProfile(
    baseProfile,
    version,
  );
  const normalized = normalizeProfile(versionedProfile);
  return normalized.id === normalizedProfileId ? normalized : null;
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
    if (currentSelectedProfileIds.includes(currentProfileId)) {
      return [];
    }
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
  const profileMap = buildProfileMap([...snapshotProfiles, ...profiles], {
    includeVersionedAliases: true,
  });
  return normalizeSelectedProfileIds(selectedProfileIds).map((profileId) => {
    if (profileMap.has(profileId)) {
      return profileMap.get(profileId);
    }
    const builtinVersionedProfile = resolveBuiltinVersionedProfile(profileId);
    if (builtinVersionedProfile) {
      return builtinVersionedProfile;
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

function getVersionBaseProfileId(profile = {}) {
  const versionMeta = profile.versionMeta || profile.version_meta;
  return String(
    versionMeta?.baseProfileId || versionMeta?.base_profile_id || '',
  ).trim();
}

function isVersionedProfileIdForBase(profileId, baseProfileId) {
  return (
    typeof profileId === 'string' &&
    profileId.startsWith(`${baseProfileId}@`) &&
    profileId.length > baseProfileId.length + 1
  );
}

export function removeEquivalentVersionedProfileIds(
  selectedProfileIds = [],
  selectedProfiles = [],
  nextProfileId,
  nextProfile = {},
) {
  const currentProfileIds = normalizeSelectedProfileIds(selectedProfileIds);
  const versionBaseProfileId = getVersionBaseProfileId(nextProfile);
  if (!versionBaseProfileId || currentProfileIds.includes(nextProfileId)) {
    return currentProfileIds;
  }
  return currentProfileIds.filter((profileId) => {
    if (profileId === versionBaseProfileId) {
      return false;
    }
    const selectedProfile = selectedProfiles.find(
      (profile) => profile.id === profileId,
    );
    if (getVersionBaseProfileId(selectedProfile) === versionBaseProfileId) {
      return false;
    }
    return !isVersionedProfileIdForBase(profileId, versionBaseProfileId);
  });
}

export function replaceSelectedProfilePreservingOrder({
  selectedProfileIds = [],
  selectedProfiles = [],
  profileId,
  profile = {},
}) {
  const nextProfileId = String(profileId || '').trim();
  const currentProfileIds = normalizeSelectedProfileIds(selectedProfileIds);
  if (!nextProfileId) {
    return currentProfileIds;
  }

  const versionBaseProfileId =
    getVersionBaseProfileId(profile) || profile.id || nextProfileId;
  const selectedProfileById = new Map(
    selectedProfiles.filter((item) => item?.id).map((item) => [item.id, item]),
  );
  const nextProfileIds = [];
  let replaced = false;
  currentProfileIds.forEach((currentProfileId) => {
    const selectedProfile = selectedProfileById.get(currentProfileId);
    const sameProfile = currentProfileId === nextProfileId;
    const sameVersionBase =
      versionBaseProfileId &&
      (currentProfileId === versionBaseProfileId ||
        getVersionBaseProfileId(selectedProfile) === versionBaseProfileId ||
        isVersionedProfileIdForBase(currentProfileId, versionBaseProfileId));
    if (sameProfile || sameVersionBase) {
      if (!replaced) {
        nextProfileIds.push(nextProfileId);
        replaced = true;
      }
      return;
    }
    nextProfileIds.push(currentProfileId);
  });

  if (!replaced) {
    nextProfileIds.push(nextProfileId);
  }
  return nextProfileIds;
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

export function disableEmptyHeaderProfileStrategy(strategy) {
  if (!strategy || typeof strategy !== 'object') {
    return strategy || null;
  }
  const selectedProfileIds = normalizeSelectedProfileIds(
    strategy.selectedProfileIds || strategy.selected_profile_ids,
  );
  if (strategy.enabled !== true || selectedProfileIds.length > 0) {
    return {
      ...strategy,
      selectedProfileIds,
    };
  }
  return {
    ...strategy,
    enabled: false,
    selectedProfileIds: [],
    profiles: [],
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
            const versionMeta = getDefaultVersionMeta(profile);
            if (versionMeta) {
              snapshot.version_meta = {
                base_profile_id: versionMeta.baseProfileId,
                package_name: versionMeta.packageName,
                source: versionMeta.source,
                version: versionMeta.version,
                platform: normalizeAiCodingCliPlatform(versionMeta.platform),
              };
            }
            return snapshot;
          })
      : [],
  };
  return JSON.stringify(nextSettings);
}

function requiredPassthroughHeadersForProfile(profile) {
  return Object.keys(profile?.headers || {}).sort();
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
  const strategyProfiles = Array.isArray(strategy?.profiles)
    ? strategy.profiles
    : [];
  const selectedProfiles = buildSelectedProfileItems(
    selectedProfileIds,
    headerProfiles,
    [...snapshotProfiles, ...strategyProfiles],
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
