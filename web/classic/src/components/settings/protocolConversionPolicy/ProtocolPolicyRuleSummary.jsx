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

import React from 'react';
import { Button, Switch, Tag, Typography } from '@douyinfe/semi-ui';
import {
  getProtocolRuleAttentionKeys,
  getProtocolRuleDirection,
  getEndpointLabel,
  getRuleModelSummary,
  getRuleScopeSummary,
  isChatToResponsesRule,
  isResponsesToChatRule,
  isRuleScopeValid,
} from './utils';
import {
  ENDPOINT_RESPONSES,
  TEMPLATE_TYPE_CHAT_TO_RESPONSES,
} from './constants';

const { Text } = Typography;

export default function ProtocolPolicyRuleSummary({
  directionInvalid,
  index,
  isExpanded,
  passThroughEnabled,
  rule,
  ruleKey,
  t,
  toggleRuleExpanded,
  updateRule,
}) {
  const scopeInvalid = !isRuleScopeValid(rule);
  const customToolBridgeEnabled =
    isResponsesToChatRule(rule) && rule.enable_custom_tool_bridge === true;
  const attentionKeys = getProtocolRuleAttentionKeys(rule, passThroughEnabled);
  const direction = getProtocolRuleDirection(rule);
  const sourceTitle = isChatToResponsesRule(rule)
    ? t('Chat Completions')
    : t('Responses');
  const targetTitle =
    rule.target_endpoint === ENDPOINT_RESPONSES
      ? t('Responses')
      : t('Chat Completions');
  const scopeTitle = rule.all_channels
    ? t('全部渠道')
    : scopeInvalid
      ? t('空范围')
      : t('限定范围');
  const scopeDetail = rule.all_channels
    ? t('此规则可匹配任意渠道。')
    : t('{{channelCount}} 个渠道 ID，{{typeCount}} 个渠道类型', {
        channelCount: rule.channel_ids.length,
        typeCount: rule.channel_types.length,
      });
  const modelTitle =
    rule.model_patterns.length === 0
      ? t('匹配全部非空模型')
      : t('{{count}} 条模型正则', { count: rule.model_patterns.length });

  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        gap: 12,
        flexWrap: 'wrap',
        alignItems: 'center',
        marginBottom: isExpanded ? 14 : 0,
      }}
    >
      <div style={{ flex: '1 1 520px' }}>
        <div
          style={{
            display: 'flex',
            gap: 8,
            flexWrap: 'wrap',
            alignItems: 'center',
            marginBottom: 6,
          }}
        >
          <Tag color='grey'>{t('规则 #{{index}}', { index: index + 1 })}</Tag>
          <Text strong style={{ fontSize: 15 }}>
            {rule.name || t('未命名规则')}
          </Text>
          <Tag color={rule.enabled ? 'green' : 'grey'}>
            {rule.enabled ? t('启用中') : t('已停用')}
          </Tag>
          <Tag color='light-blue'>
            {`${getEndpointLabel(rule.source_endpoint)} -> ${getEndpointLabel(rule.target_endpoint)}`}
          </Tag>
          {directionInvalid ? <Tag color='red'>{t('方向无效')}</Tag> : null}
          {scopeInvalid ? <Tag color='red'>{t('范围未命中')}</Tag> : null}
          {customToolBridgeEnabled ? (
            <Tag color='purple'>{t('自定义工具桥接')}</Tag>
          ) : null}
          {Object.keys(rule.__extra || {}).length > 0 ||
          Object.keys(rule.__options_extra || {}).length > 0 ? (
            <Tag color='orange'>{t('包含高级字段')}</Tag>
          ) : null}
          {attentionKeys.length > 0 ? (
            <Tag color='orange'>
              {t('{{count}} 个需关注项', { count: attentionKeys.length })}
            </Tag>
          ) : null}
        </div>
        <div
          style={{
            display: 'flex',
            gap: 10,
            flexWrap: 'wrap',
          }}
        >
          <Text type='tertiary' size='small'>
            {t('命中范围')}: {getRuleScopeSummary(rule, t)}
          </Text>
          <Text type='tertiary' size='small'>
            {t('模型条件')}: {getRuleModelSummary(rule, t)}
          </Text>
        </div>
        <div
          style={{
            marginTop: 10,
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))',
            gap: 8,
          }}
        >
          <RulePanel
            label={t('入口协议')}
            title={sourceTitle}
            detail={
              direction === TEMPLATE_TYPE_CHAT_TO_RESPONSES
                ? t('客户端使用 Chat Completions。')
                : t('客户端使用 Responses。')
            }
          />
          <RulePanel
            label={t('上游协议')}
            title={targetTitle}
            detail={
              rule.target_endpoint === ENDPOINT_RESPONSES
                ? t('转为 Responses 后访问上游。')
                : t('转为 Chat Completions 后访问上游。')
            }
          />
          <RulePanel
            label={t('运行范围')}
            title={scopeTitle}
            detail={scopeDetail}
          />
          <RulePanel
            label={t('模型边界')}
            title={modelTitle}
            detail={
              rule.model_patterns.length > 0
                ? rule.model_patterns.slice(0, 2).join(', ')
                : t('模型名仍必须为非空。')
            }
          />
          <RulePanel
            label={t('执行提示')}
            title={attentionKeys.length > 0 ? t('需要关注') : t('可参与匹配')}
            detail={
              attentionKeys[0]
                ? t(attentionKeys[0])
                : t('请求方向、渠道和模型全部命中后会执行转换。')
            }
          />
        </div>
      </div>
      <div
        style={{
          display: 'flex',
          gap: 10,
          flexWrap: 'wrap',
          alignItems: 'center',
          justifyContent: 'flex-end',
        }}
      >
        <Button
          size='small'
          type={isExpanded ? 'tertiary' : 'primary'}
          theme={isExpanded ? 'light' : 'solid'}
          onClick={() => toggleRuleExpanded(ruleKey)}
        >
          {isExpanded ? t('收起编辑') : t('编辑规则')}
        </Button>
        <div
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 8,
            padding: '6px 10px',
            borderRadius: 10,
            background: 'var(--semi-color-fill-0)',
            border: '1px solid var(--semi-color-border)',
          }}
        >
          <Text type='tertiary' size='small'>
            {t('规则开关')}
          </Text>
          <Switch
            id={`protocol-rule-enabled-${index}`}
            name={`protocol-rule-enabled-${index}`}
            checked={rule.enabled}
            checkedText={t('开')}
            uncheckedText={t('关')}
            onChange={(checked) => updateRule(index, { enabled: checked })}
          />
        </div>
      </div>
    </div>
  );
}

function RulePanel({ detail, label, title }) {
  return (
    <div
      style={{
        border: '1px solid var(--semi-color-border)',
        borderRadius: 10,
        padding: 10,
        background: 'var(--semi-color-fill-0)',
      }}
    >
      <Text type='tertiary' size='small'>
        {label}
      </Text>
      <div style={{ fontWeight: 600, marginTop: 2 }}>{title}</div>
      <Text type='tertiary' size='small'>
        {detail}
      </Text>
    </div>
  );
}
