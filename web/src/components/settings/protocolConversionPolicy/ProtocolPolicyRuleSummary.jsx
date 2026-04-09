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
  getEndpointLabel,
  getRuleModelSummary,
  getRuleScopeSummary,
} from './utils';

const { Text } = Typography;

export default function ProtocolPolicyRuleSummary({
  directionInvalid,
  index,
  isExpanded,
  rule,
  ruleKey,
  t,
  toggleRuleExpanded,
  updateRule,
}) {
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
          type={isExpanded ? 'tertiary' : 'secondary'}
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
