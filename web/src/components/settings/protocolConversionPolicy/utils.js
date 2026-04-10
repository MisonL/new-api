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
import { verifyJSON } from '../../../helpers';

const RULE_CLIENT_KEY_FIELD = '__client_key';

let ruleClientKeyCounter = 0;

const nextRuleClientKey = () => `protocol-rule-${ruleClientKeyCounter++}`;

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
        .split(',')
        .map((item) => Number(item.trim()))
        .filter((item) => Number.isInteger(item) && item > 0),
    ),
  );

export const stringifyIntegerList = (values) =>
  Array.isArray(values) ? values.join(', ') : '';

export const isRuleScopeValid = (rule) =>
  rule?.all_channels === true ||
  (Array.isArray(rule?.channel_ids) && rule.channel_ids.length > 0) ||
  (Array.isArray(rule?.channel_types) && rule.channel_types.length > 0);

export const sanitizeRule = (rule, fallbackName) => {
  const channelTypes = Array.isArray(rule?.channel_types)
    ? rule.channel_types
        .map((item) => Number(item))
        .filter((item) => Number.isInteger(item) && item > 0)
    : [];

  return {
    [RULE_CLIENT_KEY_FIELD]: ensureRuleClientKey(rule),
    name: String(rule?.name || fallbackName || '').trim() || 'rule',
    enabled: rule?.enabled !== false,
    source_endpoint: normalizeEndpoint(rule?.source_endpoint),
    target_endpoint: normalizeEndpoint(rule?.target_endpoint),
    all_channels: rule?.all_channels !== false,
    channel_ids: Array.isArray(rule?.channel_ids)
      ? rule.channel_ids
          .map((item) => Number(item))
          .filter((item) => Number.isInteger(item) && item > 0)
      : [],
    channel_types: channelTypes,
    model_patterns: Array.isArray(rule?.model_patterns)
      ? rule.model_patterns
          .map((item) => String(item || '').trim())
          .filter((item) => item.length > 0)
      : [],
  };
};

export const extractRulesFromPolicy = (policy) => {
  if (!policy || typeof policy !== 'object' || Array.isArray(policy)) {
    return [];
  }
  if (Array.isArray(policy.rules) && policy.rules.length > 0) {
    return policy.rules.map((rule, index) =>
      sanitizeRule(rule, `rule-${index + 1}`),
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
    ),
  ];
};

export const serializeRules = (rules) => {
  const cleanedRules = (rules || []).map((rule) => {
    const payload = {
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
    return payload;
  });

  if (cleanedRules.length === 0) {
    return '{}';
  }
  return JSON.stringify({ rules: cleanedRules }, null, 2);
};

export const deserializeRules = (rawValue) => {
  const raw = String(rawValue || '').trim();
  if (!raw) {
    return [];
  }
  if (!verifyJSON(raw)) {
    return null;
  }
  try {
    return extractRulesFromPolicy(JSON.parse(raw));
  } catch {
    return null;
  }
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
    return t('未配置，不会命中');
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
