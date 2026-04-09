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
import {
  Banner,
  Button,
  Col,
  Input,
  Row,
  Select,
  Switch,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { ENDPOINT_OPTIONS, panelStyle } from './constants';
import { parseIntegerList, parseTextList, stringifyIntegerList } from './utils';

const { Text } = Typography;

function BasicPanel({ directionInvalid, index, rule, t, updateRule }) {
  return (
    <div style={panelStyle}>
      <div style={{ marginBottom: 12 }}>
        <Text strong>{t('基础配置')}</Text>
      </div>
      <Row gutter={16}>
        <Col xs={24} md={8}>
          <div style={{ marginBottom: 12 }}>
            <Text strong>{t('规则名称')}</Text>
            <Input
              name={`protocol-rule-name-${index}`}
              value={rule.name}
              placeholder={t('例如：responses-to-chat')}
              onChange={(nextValue) => updateRule(index, { name: nextValue })}
            />
          </div>
        </Col>
        <Col xs={24} md={8}>
          <div style={{ marginBottom: 12 }}>
            <Text strong>{t('源协议')}</Text>
            <Select
              name={`protocol-rule-source-endpoint-${index}`}
              allowClear={false}
              optionList={ENDPOINT_OPTIONS}
              value={rule.source_endpoint}
              onChange={(nextValue) =>
                updateRule(index, { source_endpoint: nextValue })
              }
            />
          </div>
        </Col>
        <Col xs={24} md={8}>
          <div style={{ marginBottom: 12 }}>
            <Text strong>{t('目标协议')}</Text>
            <Select
              name={`protocol-rule-target-endpoint-${index}`}
              allowClear={false}
              optionList={ENDPOINT_OPTIONS}
              value={rule.target_endpoint}
              onChange={(nextValue) =>
                updateRule(index, { target_endpoint: nextValue })
              }
            />
          </div>
        </Col>
      </Row>
      {directionInvalid ? (
        <Banner
          type='warning'
          description={t(
            '源协议和目标协议不能相同。请修改为 Chat -> Responses 或 Responses -> Chat。',
          )}
        />
      ) : null}
    </div>
  );
}

function ScopePanel({ channelTypeOptions, index, rule, t, updateRule }) {
  return (
    <div style={panelStyle}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          gap: 12,
          flexWrap: 'wrap',
          alignItems: 'center',
          marginBottom: 12,
        }}
      >
        <div>
          <Text strong>{t('命中范围')}</Text>
          <div>
            <Text type='tertiary' size='small'>
              {t(
                '先匹配方向，再匹配渠道范围和模型正则；全部命中后才会执行协议转换。',
              )}
            </Text>
          </div>
        </div>
        <div style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
          <Text>{t('作用于全部渠道')}</Text>
          <Switch
            id={`protocol-rule-all-channels-${index}`}
            name={`protocol-rule-all-channels-${index}`}
            checked={rule.all_channels}
            checkedText={t('是')}
            uncheckedText={t('否')}
            onChange={(checked) => updateRule(index, { all_channels: checked })}
          />
        </div>
      </div>

      <Row gutter={16}>
        <Col xs={24} md={12}>
          <div style={{ marginBottom: 12 }}>
            <Text strong>{t('指定渠道 ID')}</Text>
            <Input
              name={`protocol-rule-channel-ids-${index}`}
              disabled={rule.all_channels}
              value={stringifyIntegerList(rule.channel_ids)}
              placeholder={t('多个 ID 用逗号分隔，例如：35,36,37')}
              onChange={(nextValue) =>
                updateRule(index, { channel_ids: parseIntegerList(nextValue) })
              }
            />
          </div>
        </Col>
        <Col xs={24} md={12}>
          <div style={{ marginBottom: 12 }}>
            <Text strong>{t('指定渠道类型')}</Text>
            <Select
              name={`protocol-rule-channel-types-${index}`}
              multiple
              disabled={rule.all_channels}
              optionList={channelTypeOptions}
              value={rule.channel_types}
              placeholder={t('可选，用于按渠道类型批量命中')}
              onChange={(nextValue) =>
                updateRule(index, { channel_types: nextValue || [] })
              }
            />
          </div>
        </Col>
      </Row>

      <div>
        <Text strong>{t('模型正则')}</Text>
        <div>
          <Text type='tertiary' size='small'>
            {t('每行一个正则；留空表示不命中。')}
          </Text>
        </div>
        <TextArea
          name={`protocol-rule-model-patterns-${index}`}
          value={(rule.model_patterns || []).join('\n')}
          rows={3}
          placeholder={'^gpt-5.*$\n^gpt-4o.*$'}
          onChange={(nextValue) =>
            updateRule(index, { model_patterns: parseTextList(nextValue) })
          }
        />
      </div>
    </div>
  );
}

export default function ProtocolPolicyRuleBody({
  channelTypeOptions,
  directionInvalid,
  index,
  removeRule,
  rule,
  t,
  updateRule,
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <BasicPanel
        directionInvalid={directionInvalid}
        index={index}
        rule={rule}
        t={t}
        updateRule={updateRule}
      />
      <ScopePanel
        channelTypeOptions={channelTypeOptions}
        index={index}
        rule={rule}
        t={t}
        updateRule={updateRule}
      />
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Button
          type='danger'
          theme='borderless'
          onClick={() => removeRule(index)}
        >
          {t('删除当前规则')}
        </Button>
      </div>
    </div>
  );
}
