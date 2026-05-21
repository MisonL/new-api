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
  ENDPOINT_CHAT,
  ENDPOINT_OPTIONS,
  ENDPOINT_RESPONSES,
  SUPPORTED_ENDPOINT_VALUES,
} from './constants';

const RULE_CLIENT_KEY_FIELD = '__client_key';
const RULE_EXTRA_FIELD = '__extra';
const RULE_OPTIONS_EXTRA_FIELD = '__options_extra';

const POLICY_KNOWN_FIELDS = new Set([
  'enabled',
  'all_channels',
  'channel_ids',
  'channel_types',
  'model_patterns',
  'rules',
]);

const RULE_KNOWN_FIELDS = new Set([
  'name',
  'enabled',
  'source_endpoint',
  'target_endpoint',
  'all_channels',
  'channel_ids',
  'channel_types',
  'model_patterns',
  'options',
  RULE_CLIENT_KEY_FIELD,
  RULE_EXTRA_FIELD,
  RULE_OPTIONS_EXTRA_FIELD,
]);

const OPTION_KNOWN_FIELDS = new Set(['enable_custom_tool_bridge']);

let ruleClientKeyCounter = 0;

const nextRuleClientKey = () => `protocol-rule-${ruleClientKeyCounter++}`;

const verifyJSON = (raw) => {
  try {
    JSON.parse(raw);
    return true;
  } catch {
    return false;
  }
};

const createParseError = (message) => new Error(message);

const ruleFieldPath = (fieldName, ruleIndex) =>
  ruleIndex == null ? fieldName : `rules[${ruleIndex}].${fieldName}`;

const normalizeEndpointStrict = (value, fieldName, ruleIndex) => {
  const endpoint = normalizeEndpoint(value);
  const path = ruleFieldPath(fieldName, ruleIndex);
  if (!endpoint) {
    throw createParseError(`${path} is required`);
  }
  if (!SUPPORTED_ENDPOINT_VALUES.has(endpoint)) {
    throw createParseError(`${path} is unsupported`);
  }
  return endpoint;
};

const toPositiveIntegerArray = (value, fieldName, ruleIndex) => {
  if (value === undefined) {
    return [];
  }
  const path = ruleFieldPath(fieldName, ruleIndex);
  if (!Array.isArray(value)) {
    throw createParseError(`${path} must be an array`);
  }
  for (const item of value) {
    if (!Number.isInteger(item) || item <= 0) {
      throw createParseError(`${path} must contain positive integers`);
    }
  }
  return Array.from(new Set(value));
};

const toModelPatternArray = (value, ruleIndex) => {
  if (value === undefined) {
    return [];
  }
  const path = ruleFieldPath('model_patterns', ruleIndex);
  if (!Array.isArray(value)) {
    throw createParseError(`${path} must be an array`);
  }
  const patterns = [];
  for (const item of value) {
    if (typeof item !== 'string') {
      throw createParseError(`${path} must contain strings`);
    }
    const pattern = item.trim();
    if (!pattern) {
      continue;
    }
    try {
      new RegExp(pattern);
    } catch (error) {
      const detail = error instanceof Error ? `: ${error.message}` : '';
      throw createParseError(`${path} contains an invalid regex${detail}`);
    }
    patterns.push(pattern);
  }
  return patterns;
};

const ensureRuleClientKey = (rule) => {
  const currentKey =
    rule &&
    typeof rule === 'object' &&
    typeof rule[RULE_CLIENT_KEY_FIELD] === 'string' &&
    rule[RULE_CLIENT_KEY_FIELD].trim() !== ''
      ? rule[RULE_CLIENT_KEY_FIELD].trim()
      : '';
  return currentKey || nextRuleClientKey();
};

export const normalizeEndpoint = (value) => {
  const endpoint = String(value || '')
    .trim()
    .toLowerCase();
  if (
    endpoint === 'openai' ||
    endpoint === 'chat' ||
    endpoint === 'chat_completions' ||
    endpoint === 'chat-completions' ||
    endpoint === '/v1/chat/completions'
  ) {
    return ENDPOINT_CHAT;
  }
  if (
    endpoint === 'responses' ||
    endpoint === 'response' ||
    endpoint === 'openai-response' ||
    endpoint === 'openai-responses' ||
    endpoint === '/v1/responses'
  ) {
    return ENDPOINT_RESPONSES;
  }
  return endpoint;
};

export const parseTextList = (text) =>
  String(text || '')
    .split('\n')
    .map((item) => item.trim())
    .filter((item) => item.length > 0);

export const parseIntegerList = (text) =>
  Array.from(
    new Set(
      String(text || '')
        .split(/[\s,，、]+/)
        .map((item) => Number(item.trim()))
        .filter((item) => Number.isInteger(item) && item > 0),
    ),
  );

export const stringifyIntegerList = (values) =>
  Array.isArray(values) ? values.join(', ') : '';

const pickExtraFields = (value, knownFields) => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {};
  }
  return Object.fromEntries(
    Object.entries(value).filter(([key]) => !knownFields.has(key)),
  );
};

export const isRuleScopeValid = (rule) =>
  rule?.all_channels === true ||
  (Array.isArray(rule?.channel_ids) && rule.channel_ids.length > 0) ||
  (Array.isArray(rule?.channel_types) && rule.channel_types.length > 0);

export const sanitizeRule = (rule, fallbackName, ruleIndex) => {
  if (!rule || typeof rule !== 'object' || Array.isArray(rule)) {
    throw createParseError(`rules[${ruleIndex}] must be an object`);
  }
  if (rule.options !== undefined) {
    if (
      !rule.options ||
      typeof rule.options !== 'object' ||
      Array.isArray(rule.options)
    ) {
      throw createParseError(`rules[${ruleIndex}].options must be an object`);
    }
  }
  const sourceEndpoint = normalizeEndpointStrict(
    rule.source_endpoint,
    'source_endpoint',
    ruleIndex,
  );
  const targetEndpoint = normalizeEndpointStrict(
    rule.target_endpoint,
    'target_endpoint',
    ruleIndex,
  );
  if (sourceEndpoint === targetEndpoint) {
    throw createParseError(
      `rules[${ruleIndex}].source_endpoint and target_endpoint must be different`,
    );
  }

  const enableCustomToolBridge =
    rule?.options?.enable_custom_tool_bridge === true ||
    rule?.enable_custom_tool_bridge === true;
  if (
    enableCustomToolBridge &&
    (sourceEndpoint !== ENDPOINT_RESPONSES || targetEndpoint !== ENDPOINT_CHAT)
  ) {
    throw createParseError(
      `rules[${ruleIndex}].enable_custom_tool_bridge only supports Responses to Chat Completions`,
    );
  }

  return {
    [RULE_CLIENT_KEY_FIELD]: ensureRuleClientKey(rule),
    name: String(rule?.name || fallbackName || '').trim() || 'rule',
    enabled: rule?.enabled !== false,
    source_endpoint: sourceEndpoint,
    target_endpoint: targetEndpoint,
    all_channels: rule?.all_channels !== false,
    channel_ids: toPositiveIntegerArray(
      rule?.channel_ids,
      'channel_ids',
      ruleIndex,
    ),
    channel_types: toPositiveIntegerArray(
      rule?.channel_types,
      'channel_types',
      ruleIndex,
    ),
    model_patterns: toModelPatternArray(rule?.model_patterns, ruleIndex),
    enable_custom_tool_bridge: enableCustomToolBridge,
    [RULE_EXTRA_FIELD]: {
      ...pickExtraFields(rule?.[RULE_EXTRA_FIELD], new Set()),
      ...pickExtraFields(rule, RULE_KNOWN_FIELDS),
    },
    [RULE_OPTIONS_EXTRA_FIELD]: {
      ...pickExtraFields(rule?.[RULE_OPTIONS_EXTRA_FIELD], new Set()),
      ...pickExtraFields(rule?.options, OPTION_KNOWN_FIELDS),
    },
  };
};

export const extractPolicyExtra = (policy) =>
  pickExtraFields(policy, POLICY_KNOWN_FIELDS);

export const extractRulesFromPolicy = (policy) => {
  if (!policy || typeof policy !== 'object' || Array.isArray(policy)) {
    return [];
  }
  if (Array.isArray(policy.rules)) {
    return policy.rules.map((rule, index) =>
      sanitizeRule(rule, `rule-${index + 1}`, index),
    );
  }

  const hasLegacyContent =
    policy.enabled !== undefined ||
    policy.all_channels !== undefined ||
    (Array.isArray(policy.channel_ids) && policy.channel_ids.length > 0) ||
    (Array.isArray(policy.channel_types) && policy.channel_types.length > 0) ||
    (Array.isArray(policy.model_patterns) && policy.model_patterns.length > 0);

  if (!hasLegacyContent) {
    return [];
  }

  return [
    sanitizeRule(
      {
        name: 'legacy-chat-to-responses',
        enabled: policy.enabled !== false,
        source_endpoint: ENDPOINT_CHAT,
        target_endpoint: ENDPOINT_RESPONSES,
        all_channels: policy.all_channels !== false,
        channel_ids: policy.channel_ids || [],
        channel_types: policy.channel_types || [],
        model_patterns: policy.model_patterns || [],
      },
      'legacy-chat-to-responses',
      0,
    ),
  ];
};

export const serializeRules = (rules, policyExtra = {}) => {
  const cleanedRules = (rules || []).map((rule) => {
    const payload = {
      ...(rule[RULE_EXTRA_FIELD] || {}),
      name: rule.name,
      enabled: rule.enabled !== false,
      source_endpoint: normalizeEndpoint(rule.source_endpoint),
      target_endpoint: normalizeEndpoint(rule.target_endpoint),
      all_channels: rule.all_channels !== false,
    };
    if (Array.isArray(rule.channel_ids) && rule.channel_ids.length > 0) {
      payload.channel_ids = rule.channel_ids;
    }
    if (Array.isArray(rule.channel_types) && rule.channel_types.length > 0) {
      payload.channel_types = rule.channel_types;
    }
    if (Array.isArray(rule.model_patterns) && rule.model_patterns.length > 0) {
      payload.model_patterns = rule.model_patterns;
    }
    const options = {
      ...(rule[RULE_OPTIONS_EXTRA_FIELD] || {}),
    };
    if (
      rule.enable_custom_tool_bridge === true &&
      isResponsesToChatRule(rule)
    ) {
      options.enable_custom_tool_bridge = true;
    } else {
      delete options.enable_custom_tool_bridge;
    }
    if (Object.keys(options).length > 0) {
      payload.options = options;
    }
    return payload;
  });

  if (cleanedRules.length === 0) {
    return JSON.stringify(policyExtra || {}, null, 2);
  }
  return JSON.stringify(
    { ...(policyExtra || {}), rules: cleanedRules },
    null,
    2,
  );
};

export const deserializePolicy = (rawValue) => {
  const raw = String(rawValue || '').trim();
  if (!raw) {
    return { policyExtra: {}, rules: [] };
  }
  if (!verifyJSON(raw)) {
    return null;
  }
  try {
    const policy = JSON.parse(raw);
    return {
      policyExtra: extractPolicyExtra(policy),
      rules: extractRulesFromPolicy(policy),
    };
  } catch {
    return null;
  }
};

export const deserializeRules = (rawValue) => {
  const parsed = deserializePolicy(rawValue);
  return parsed ? parsed.rules : parsed;
};

export const validateRulesForVisualMode = (rules) =>
  (rules || []).every(
    (rule) =>
      SUPPORTED_ENDPOINT_VALUES.has(rule.source_endpoint) &&
      SUPPORTED_ENDPOINT_VALUES.has(rule.target_endpoint),
  );

export const buildTemplateRule = (template, currentRules) => {
  const existingNames = new Set((currentRules || []).map((rule) => rule.name));
  let nextName = template.name;
  let index = 2;
  while (existingNames.has(nextName)) {
    nextName = `${template.name}-${index}`;
    index += 1;
  }
  return sanitizeRule(
    {
      ...template,
      name: nextName,
    },
    nextName,
  );
};

export const getRuleKey = (rule, index) =>
  ensureRuleClientKey(rule) || `rule-${index}`;

export const getEndpointLabel = (value) =>
  ENDPOINT_OPTIONS.find((item) => item.value === value)?.label || value;

export const getRuleScopeSummary = (rule, t) => {
  if (rule.all_channels) {
    return t('全部渠道');
  }
  const parts = [];
  if (Array.isArray(rule.channel_types) && rule.channel_types.length > 0) {
    parts.push(t('{{count}} 个渠道类型', { count: rule.channel_types.length }));
  }
  if (Array.isArray(rule.channel_ids) && rule.channel_ids.length > 0) {
    parts.push(t('{{count}} 个渠道 ID', { count: rule.channel_ids.length }));
  }
  return parts.length === 0 ? t('未指定渠道范围，不会命中') : parts.join(' / ');
};

export const getRuleModelSummary = (rule, t) => {
  if (!Array.isArray(rule.model_patterns) || rule.model_patterns.length === 0) {
    return t('全部模型');
  }
  if (rule.model_patterns.length === 1) {
    return rule.model_patterns[0];
  }
  return t('{{count}} 条模型正则', { count: rule.model_patterns.length });
};

export const isRuleDirectionValid = (rule) =>
  SUPPORTED_ENDPOINT_VALUES.has(rule.source_endpoint) &&
  SUPPORTED_ENDPOINT_VALUES.has(rule.target_endpoint) &&
  rule.source_endpoint !== rule.target_endpoint;

export const isResponsesToChatRule = (rule) =>
  normalizeEndpoint(rule?.source_endpoint) === ENDPOINT_RESPONSES &&
  normalizeEndpoint(rule?.target_endpoint) === ENDPOINT_CHAT;
