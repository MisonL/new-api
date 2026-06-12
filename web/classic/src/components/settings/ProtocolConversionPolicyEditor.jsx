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

import React, { useEffect, useMemo, useState } from 'react';
import { Banner, Card, Empty, TextArea } from '@douyinfe/semi-ui';
import { CHANNEL_OPTIONS } from '../../constants/channel.constants';
import { showError } from '../../helpers';
import { useTranslation } from 'react-i18next';
import ProtocolPolicyHeader from './protocolConversionPolicy/ProtocolPolicyHeader';
import ProtocolPolicyRuleCard from './protocolConversionPolicy/ProtocolPolicyRuleCard';
import {
  CHAT_TO_RESPONSES_TEMPLATE,
  EDIT_MODE_JSON,
  EDIT_MODE_VISUAL,
  POLICY_JSON_EXAMPLE,
  PROTOCOL_FILTER_ALL,
  PROTOCOL_RULE_SCOPE_EMPTY,
  PROTOCOL_RULE_SCOPE_GLOBAL,
  PROTOCOL_RULE_SCOPE_LIMITED,
  PROTOCOL_RULE_STATE_ATTENTION,
  PROTOCOL_RULE_STATE_DISABLED,
  PROTOCOL_RULE_STATE_ENABLED,
  RESPONSES_TO_CHAT_TEMPLATE,
  TEMPLATE_TYPE_BIDIRECTIONAL,
  TEMPLATE_TYPE_CHAT_TO_RESPONSES,
  TEMPLATE_TYPE_RESPONSES_TO_CHAT,
} from './protocolConversionPolicy/constants';
import {
  buildTemplateRule,
  deserializePolicy,
  filterProtocolRules,
  getProtocolPolicyStats,
  getRuleKey,
  isResponsesToChatRule,
  isRuleScopeValid,
  isRuleDirectionValid,
  serializeRules,
  validateRulesForVisualMode,
} from './protocolConversionPolicy/utils';

const DEFAULT_RULE_FILTERS = {
  direction: PROTOCOL_FILTER_ALL,
  state: PROTOCOL_FILTER_ALL,
  scope: PROTOCOL_FILTER_ALL,
  query: '',
};

function buildEditModeOptions(t) {
  return [
    { label: t('可视化配置'), value: EDIT_MODE_VISUAL },
    { label: t('原始 JSON'), value: EDIT_MODE_JSON },
  ];
}

function buildTemplateOptions(t) {
  return [
    { label: t('Chat -> Responses'), value: TEMPLATE_TYPE_CHAT_TO_RESPONSES },
    { label: t('Responses -> Chat'), value: TEMPLATE_TYPE_RESPONSES_TO_CHAT },
    { label: t('双向（两条规则）'), value: TEMPLATE_TYPE_BIDIRECTIONAL },
  ];
}

function appendRulesByTemplates(rules, templates) {
  const nextRules = [...rules];
  const nextRuleKeys = [];
  for (const template of (templates || []).filter(Boolean)) {
    const nextRule = buildTemplateRule(template, nextRules);
    nextRules.push(nextRule);
    nextRuleKeys.push(getRuleKey(nextRule, nextRules.length - 1));
  }
  return { nextRules, nextRuleKeys };
}

export default function ProtocolConversionPolicyEditor({
  value,
  onChange,
  passThroughEnabled = false,
}) {
  const { t } = useTranslation();
  const [editMode, setEditMode] = useState(EDIT_MODE_VISUAL);
  const [rules, setRules] = useState([]);
  const [policyExtra, setPolicyExtra] = useState({});
  const [expandedRuleKeys, setExpandedRuleKeys] = useState([]);
  const [newRuleTemplateType, setNewRuleTemplateType] = useState(
    TEMPLATE_TYPE_CHAT_TO_RESPONSES,
  );
  const [ruleFilters, setRuleFilters] = useState(DEFAULT_RULE_FILTERS);

  useEffect(() => {
    const parsedPolicy = deserializePolicy(value);
    if (parsedPolicy) {
      const nextSerialized = serializeRules(
        parsedPolicy.rules,
        parsedPolicy.policyExtra,
      );
      const currentSerialized = serializeRules(rules, policyExtra);
      if (nextSerialized === currentSerialized) {
        return;
      }
      setRules(parsedPolicy.rules);
      setPolicyExtra(parsedPolicy.policyExtra);
    }
  }, [value, rules, policyExtra]);

  useEffect(() => {
    if (rules.length === 0) {
      setExpandedRuleKeys([]);
      return;
    }
    setExpandedRuleKeys((prev) => {
      const validKeys = new Set(
        rules.map((rule, index) => getRuleKey(rule, index)),
      );
      return prev.filter((key) => validKeys.has(key));
    });
  }, [rules]);

  const channelTypeOptions = useMemo(
    () =>
      CHANNEL_OPTIONS.map((item) => ({ label: item.label, value: item.value })),
    [],
  );
  const editModeOptions = useMemo(() => buildEditModeOptions(t), [t]);
  const newRuleTemplateOptions = useMemo(() => buildTemplateOptions(t), [t]);
  const ruleKeys = useMemo(
    () => rules.map((rule, index) => getRuleKey(rule, index)),
    [rules],
  );
  const enabledRuleCount = useMemo(
    () => rules.filter((rule) => rule.enabled).length,
    [rules],
  );
  const invalidDirectionRuleCount = useMemo(
    () => rules.filter((rule) => !isRuleDirectionValid(rule)).length,
    [rules],
  );
  const invalidScopeRuleCount = useMemo(
    () => rules.filter((rule) => !isRuleScopeValid(rule)).length,
    [rules],
  );
  const isAllRulesExpanded = useMemo(
    () => ruleKeys.length > 0 && expandedRuleKeys.length === ruleKeys.length,
    [expandedRuleKeys.length, ruleKeys.length],
  );
  const stats = useMemo(
    () => getProtocolPolicyStats(rules, passThroughEnabled),
    [rules, passThroughEnabled],
  );
  const filteredRules = useMemo(
    () => filterProtocolRules(rules, ruleFilters, passThroughEnabled),
    [rules, ruleFilters, passThroughEnabled],
  );
  const hasActiveFilters =
    ruleFilters.direction !== PROTOCOL_FILTER_ALL ||
    ruleFilters.state !== PROTOCOL_FILTER_ALL ||
    ruleFilters.scope !== PROTOCOL_FILTER_ALL ||
    ruleFilters.query.trim() !== '';

  const applyRules = (nextRules, nextPolicyExtra = policyExtra) => {
    setRules(nextRules);
    setPolicyExtra(nextPolicyExtra);
    onChange(serializeRules(nextRules, nextPolicyExtra));
  };

  const switchToVisualMode = () => {
    const parsedPolicy = deserializePolicy(value);
    if (parsedPolicy === null) {
      showError(t('JSON 配置不合法，无法切换到可视化模式'));
      return;
    }
    if (!validateRulesForVisualMode(parsedPolicy.rules)) {
      showError(t('当前 JSON 中存在暂不支持的协议值，请先在 JSON 模式下修正'));
      return;
    }
    setRules(parsedPolicy.rules);
    setPolicyExtra(parsedPolicy.policyExtra);
    setEditMode(EDIT_MODE_VISUAL);
    onChange(serializeRules(parsedPolicy.rules, parsedPolicy.policyExtra));
  };

  const addRuleByTemplateType = () => {
    const templates =
      newRuleTemplateType === TEMPLATE_TYPE_BIDIRECTIONAL
        ? [CHAT_TO_RESPONSES_TEMPLATE, RESPONSES_TO_CHAT_TEMPLATE]
        : [
            newRuleTemplateType === TEMPLATE_TYPE_RESPONSES_TO_CHAT
              ? RESPONSES_TO_CHAT_TEMPLATE
              : CHAT_TO_RESPONSES_TEMPLATE,
          ];
    const { nextRuleKeys, nextRules } = appendRulesByTemplates(
      rules,
      templates,
    );
    applyRules(nextRules);
    setExpandedRuleKeys((prev) =>
      Array.from(new Set([...prev, ...nextRuleKeys])),
    );
  };

  const updateRule = (index, patch) =>
    applyRules(
      rules.map((rule, currentIndex) => {
        if (currentIndex !== index) return rule;
        const nextRule = { ...rule, ...patch };
        if (!isResponsesToChatRule(nextRule)) {
          nextRule.enable_custom_tool_bridge = false;
        }
        return nextRule;
      }),
    );

  const removeRule = (targetIndex) => {
    const targetRuleKey = getRuleKey(rules[targetIndex], targetIndex);
    applyRules(rules.filter((_, currentIndex) => currentIndex !== targetIndex));
    setExpandedRuleKeys((prev) => prev.filter((key) => key !== targetRuleKey));
  };

  const headerProps = {
    addRuleByTemplateType,
    editMode,
    editModeOptions,
    enabledRuleCount,
    formatJsonValue: () => onChange(serializeRules(rules, policyExtra)),
    handleEditModeChange: (nextValue) =>
      nextValue === EDIT_MODE_VISUAL
        ? switchToVisualMode()
        : (setEditMode(EDIT_MODE_JSON),
          onChange(serializeRules(rules, policyExtra))),
    invalidDirectionRuleCount,
    invalidScopeRuleCount,
    isAllRulesExpanded,
    newRuleTemplateOptions,
    newRuleTemplateType,
    ruleFilters,
    rules,
    stats,
    filteredRuleCount: filteredRules.length,
    hasActiveFilters,
    passThroughEnabled,
    resetRuleFilters: () => setRuleFilters(DEFAULT_RULE_FILTERS),
    setRuleFilters: (patch) =>
      setRuleFilters((current) => ({ ...current, ...patch })),
    setNewRuleTemplateType,
    t,
    filterOptions: {
      direction: [
        { label: t('全部方向'), value: PROTOCOL_FILTER_ALL },
        {
          label: t('Chat -> Responses'),
          value: TEMPLATE_TYPE_CHAT_TO_RESPONSES,
        },
        {
          label: t('Responses -> Chat'),
          value: TEMPLATE_TYPE_RESPONSES_TO_CHAT,
        },
      ],
      state: [
        { label: t('全部状态'), value: PROTOCOL_FILTER_ALL },
        { label: t('启用中'), value: PROTOCOL_RULE_STATE_ENABLED },
        { label: t('已停用'), value: PROTOCOL_RULE_STATE_DISABLED },
        { label: t('需关注'), value: PROTOCOL_RULE_STATE_ATTENTION },
      ],
      scope: [
        { label: t('全部范围'), value: PROTOCOL_FILTER_ALL },
        { label: t('全部渠道'), value: PROTOCOL_RULE_SCOPE_GLOBAL },
        { label: t('限定范围'), value: PROTOCOL_RULE_SCOPE_LIMITED },
        { label: t('空范围'), value: PROTOCOL_RULE_SCOPE_EMPTY },
      ],
    },
    toggleExpandStateForAllRules: () =>
      setExpandedRuleKeys(isAllRulesExpanded ? [] : ruleKeys),
  };

  return (
    <div>
      <Banner
        type='info'
        description={t(
          '协议转换会把请求转到另一种接口格式继续发送。当前仅支持 /v1/chat/completions 与 /v1/responses 双向转换；模型正则为空时匹配全部模型，渠道范围为空时不会命中。',
        )}
      />
      <ProtocolPolicyHeader {...headerProps} />
      {editMode === EDIT_MODE_VISUAL && invalidDirectionRuleCount > 0 ? (
        <Banner
          type='warning'
          style={{ marginBottom: 12 }}
          description={t(
            '存在源协议与目标协议相同的规则，这类规则不会产生实际转换效果，请检查后再保存。',
          )}
        />
      ) : null}
      {editMode === EDIT_MODE_VISUAL && invalidScopeRuleCount > 0 ? (
        <Banner
          type='warning'
          style={{ marginBottom: 12 }}
          description={t(
            '存在未指定渠道范围的规则：既未勾选“作用于全部渠道”，也没有填写渠道 ID 或渠道类型，这类规则不会命中。',
          )}
        />
      ) : null}
      {editMode === EDIT_MODE_JSON ? (
        <TextArea
          name='protocol-conversion-policy-json'
          value={value}
          rows={14}
          placeholder={POLICY_JSON_EXAMPLE}
          onChange={(nextValue) => onChange(nextValue)}
        />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {rules.length === 0 ? (
            <Card>
              <Empty
                description={t('当前还没有协议转换规则')}
                style={{ padding: 24 }}
              />
            </Card>
          ) : null}
          {rules.length > 0 && filteredRules.length === 0 ? (
            <Card>
              <Empty
                description={t('当前筛选条件下没有匹配规则')}
                style={{ padding: 24 }}
              />
            </Card>
          ) : null}
          {filteredRules.map(({ rule, index }) => (
            <ProtocolPolicyRuleCard
              key={getRuleKey(rule, index)}
              channelTypeOptions={channelTypeOptions}
              index={index}
              isExpanded={expandedRuleKeys.includes(getRuleKey(rule, index))}
              passThroughEnabled={passThroughEnabled}
              removeRule={removeRule}
              rule={rule}
              ruleKey={getRuleKey(rule, index)}
              t={t}
              toggleRuleExpanded={(ruleKey) =>
                setExpandedRuleKeys((prev) =>
                  prev.includes(ruleKey)
                    ? prev.filter((key) => key !== ruleKey)
                    : [...prev, ruleKey],
                )
              }
              updateRule={updateRule}
            />
          ))}
        </div>
      )}
    </div>
  );
}
