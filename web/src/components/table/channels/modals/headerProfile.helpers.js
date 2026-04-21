import { HEADER_PROFILE_PRESETS } from './headerProfile.constants.js';

const STRATEGIES = new Set(['fixed', 'round_robin', 'random']);

function verifyJsonText(text) {
  try {
    JSON.parse(text);
    return true;
  } catch (error) {
    return false;
  }
}

function parseHeadersText(text) {
  if (!verifyJsonText(text)) {
    return null;
  }
  return JSON.parse(text);
}

function buildPreviewText(headers) {
  return Object.entries(headers)
    .map(([key, value]) => `${key}: ${value}`)
    .join('\n');
}

function isPlainHeaderObject(headers) {
  return !!headers && typeof headers === 'object' && !Array.isArray(headers);
}

function hasOnlyStringHeaderValues(headers) {
  return Object.values(headers).every((value) => typeof value === 'string');
}

export function normalizeHeaderProfileStrategy(strategy) {
  return STRATEGIES.has(strategy) ? strategy : 'fixed';
}

export function toggleSelectedProfile({
  strategy,
  selectedProfileKeys = [],
  profileKey,
}) {
  const normalizedStrategy = normalizeHeaderProfileStrategy(strategy);
  if (!profileKey) {
    return [...selectedProfileKeys];
  }

  if (normalizedStrategy === 'fixed') {
    return [profileKey];
  }

  const nextKeys = [...selectedProfileKeys];
  const existingIndex = nextKeys.indexOf(profileKey);
  if (existingIndex >= 0) {
    nextKeys.splice(existingIndex, 1);
    return nextKeys;
  }

  nextKeys.push(profileKey);
  return nextKeys;
}

export function buildSelectedProfileItems(selectedProfileKeys = []) {
  return selectedProfileKeys
    .map((profileKey) => HEADER_PROFILE_PRESETS[profileKey])
    .filter(Boolean)
    .map((profile) => ({
      key: profile.key,
      name: profile.name,
      group: profile.group,
      headers: profile.headers,
      previewText: buildPreviewText(profile.headers),
    }));
}

export function createLegacyHeaderProfileDraft(headerOverrideText) {
  const rawText = typeof headerOverrideText === 'string' ? headerOverrideText.trim() : '';
  if (!rawText || !verifyJsonText(rawText)) {
    return null;
  }

  const parsedHeaders = JSON.parse(rawText);
  if (!parsedHeaders || typeof parsedHeaders !== 'object' || Array.isArray(parsedHeaders)) {
    return null;
  }

  return {
    mode: 'custom',
    name: 'Legacy Header Override',
    headersText: JSON.stringify(parsedHeaders, null, 2),
    source: 'legacy_header_override',
  };
}

export function validateHeaderProfileDraft(draft = {}) {
  const errors = {};
  const name = typeof draft.name === 'string' ? draft.name.trim() : '';
  const headersText =
    typeof draft.headersText === 'string' ? draft.headersText.trim() : '';

  if (!name) {
    errors.name = '名称不能为空';
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
  if (!isPlainHeaderObject(parsedHeaders) || Object.keys(parsedHeaders).length === 0) {
    errors.headersText = 'Headers JSON 必须是非空对象';
  } else if (!hasOnlyStringHeaderValues(parsedHeaders)) {
    errors.headersText = 'Headers JSON 的值必须全部是字符串';
  }

  return {
    isValid: Object.keys(errors).length === 0,
    errors,
    parsedHeaders,
  };
}
