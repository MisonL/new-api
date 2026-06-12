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
  Popconfirm,
  Row,
  Select,
  Switch,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import {
  ENDPOINT_CHAT,
  ENDPOINT_RESPONSES,
  TEMPLATE_TYPE_CHAT_TO_RESPONSES,
  TEMPLATE_TYPE_RESPONSES_TO_CHAT,
  panelStyle,
} from './constants';
import {
  getProtocolPreviewResult,
  getProtocolRuleDirection,
  isResponsesToChatRule,
  isRuleScopeValid,
  parseIntegerList,
  parseTextList,
  stringifyIntegerList,
} from './utils';

const { Text } = Typography;

function IntegerListInput({ disabled, name, onChange, placeholder, value }) {
  const committedValue = stringifyIntegerList(value);
  const [draft, setDraft] = React.useState({
    source: committedValue,
    value: committedValue,
  });
  const inputValue =
    draft.source === committedValue ? draft.value : committedValue;

  React.useEffect(() => {
    setDraft((current) =>
      current.source === committedValue
        ? current
        : { source: committedValue, value: committedValue },
    );
  }, [committedValue]);

  const updateDraft = (nextValue) => {
    const nextList = parseIntegerList(nextValue);
    const normalizedValue = stringifyIntegerList(nextList);
    setDraft({
      source: normalizedValue,
      value: nextValue,
    });
    onChange(nextList);
  };

  const commitDraft = () => {
    const nextList = parseIntegerList(inputValue);
    const nextValue = stringifyIntegerList(nextList);
    setDraft({
      source: nextValue,
      value: nextValue,
    });
    onChange(nextList);
  };

  return (
    <Input
      name={name}
      disabled={disabled}
      value={inputValue}
      placeholder={placeholder}
      onBlur={commitDraft}
      onChange={updateDraft}
      onEnterPress={commitDraft}
    />
  );
}

function DirectionOption({
  active,
  description,
  onClick,
  source,
  t,
  target,
  title,
}) {
  return (
    <button
      type='button'
      onClick={onClick}
      style={{
        width: '100%',
        textAlign: 'left',
        border: active
          ? '1px solid var(--semi-color-primary)'
          : '1px solid var(--semi-color-border)',
        borderRadius: 10,
        padding: 12,
        background: active
          ? 'var(--semi-color-primary-light-default)'
          : 'var(--semi-color-bg-1)',
        cursor: 'pointer',
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
        <Text strong>{title}</Text>
        <Text type={active ? 'primary' : 'tertiary'} size='small'>
          {active ? t('当前方向') : t('选择')}
        </Text>
      </div>
      <div style={{ marginTop: 6 }}>
        <Text size='small'>{source}</Text>
        <Text type='tertiary' size='small'>
          {' -> '}
        </Text>
        <Text size='small'>{target}</Text>
      </div>
      <div style={{ marginTop: 6 }}>
        <Text type='tertiary' size='small'>
          {description}
        </Text>
      </div>
    </button>
  );
}

function BasicPanel({ directionInvalid, index, rule, t, updateRule }) {
  const direction = getProtocolRuleDirection(rule);
  const setDirection = (nextDirection) => {
    const nextIsResponsesToChat =
      nextDirection === TEMPLATE_TYPE_RESPONSES_TO_CHAT;
    updateRule(index, {
      source_endpoint: nextIsResponsesToChat
        ? ENDPOINT_RESPONSES
        : ENDPOINT_CHAT,
      target_endpoint: nextIsResponsesToChat
        ? ENDPOINT_CHAT
        : ENDPOINT_RESPONSES,
      enable_custom_tool_bridge: nextIsResponsesToChat
        ? rule.enable_custom_tool_bridge
        : false,
    });
  };

  return (
    <div style={panelStyle}>
      <div style={{ marginBottom: 12 }}>
        <Text strong>{t('基础配置与协议方向')}</Text>
      </div>
      <Row gutter={16}>
        <Col xs={24} md={24}>
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
        <Col xs={24} md={12}>
          <DirectionOption
            active={direction === TEMPLATE_TYPE_CHAT_TO_RESPONSES}
            title={t('Chat -> Responses')}
            source={t('客户端 Chat Completions')}
            target={t('上游 Responses')}
            description={t(
              '客户端发起 Chat Completions 请求，上游按 Responses 接口接收。',
            )}
            t={t}
            onClick={() => setDirection(TEMPLATE_TYPE_CHAT_TO_RESPONSES)}
          />
        </Col>
        <Col xs={24} md={12}>
          <DirectionOption
            active={direction === TEMPLATE_TYPE_RESPONSES_TO_CHAT}
            title={t('Responses -> Chat')}
            source={t('客户端 Responses')}
            target={t('上游 Chat Completions')}
            description={t(
              '客户端发起 Responses 请求，上游按 Chat Completions 接口接收。',
            )}
            t={t}
            onClick={() => setDirection(TEMPLATE_TYPE_RESPONSES_TO_CHAT)}
          />
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
            <IntegerListInput
              name={`protocol-rule-channel-ids-${index}`}
              disabled={rule.all_channels}
              value={rule.channel_ids}
              placeholder={t('多个 ID 用逗号分隔，例如：35,36,37')}
              onChange={(nextList) =>
                updateRule(index, { channel_ids: nextList })
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
            {t('每行一个正则；留空表示匹配全部非空模型。')}
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
      {!isRuleScopeValid(rule) ? (
        <Banner
          type='warning'
          style={{ marginTop: 12 }}
          description={t(
            '当前未指定任何渠道范围，这条规则不会命中。请勾选“作用于全部渠道”或至少指定一个渠道 ID / 渠道类型。',
          )}
        />
      ) : null}
    </div>
  );
}

function AdvancedPanel({ index, rule, t, updateRule }) {
  const bridgeSupported = isResponsesToChatRule(rule);
  return (
    <div style={panelStyle}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          gap: 12,
          flexWrap: 'wrap',
          alignItems: 'center',
        }}
      >
        <div>
          <Text strong>{t('高级选项')}</Text>
          <div>
            <Text type='tertiary' size='small'>
              {bridgeSupported
                ? t('Responses 自定义工具会桥接到 Chat Completions 工具调用。')
                : t('自定义工具桥接仅适用于 Responses -> Chat Completions。')}
            </Text>
          </div>
        </div>
        <div style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
          <Text>{t('自定义工具桥接')}</Text>
          <Switch
            id={`protocol-rule-custom-tool-bridge-${index}`}
            name={`protocol-rule-custom-tool-bridge-${index}`}
            checked={Boolean(bridgeSupported && rule.enable_custom_tool_bridge)}
            checkedText={t('开')}
            disabled={!bridgeSupported}
            uncheckedText={t('关')}
            onChange={(checked) =>
              updateRule(index, { enable_custom_tool_bridge: checked })
            }
          />
        </div>
      </div>
    </div>
  );
}

function HitPreviewPanel({ passThroughEnabled, rule, t }) {
  const [preview, setPreview] = React.useState({
    channelId: '',
    channelType: '',
    model: '',
  });
  const result = getProtocolPreviewResult(rule, preview, passThroughEnabled);
  const updatePreview = (patch) =>
    setPreview((current) => ({ ...current, ...patch }));

  return (
    <div style={panelStyle}>
      <div style={{ marginBottom: 12 }}>
        <Text strong>{t('命中预览')}</Text>
        <div>
          <Text type='tertiary' size='small'>
            {t('用一个样例渠道和模型验证当前规则是否会参与协议转换。')}
          </Text>
        </div>
      </div>
      <Row gutter={16}>
        <Col xs={24} md={8}>
          <div style={{ marginBottom: 12 }}>
            <Text strong>{t('样例渠道 ID')}</Text>
            <Input
              name='protocol-preview-channel-id'
              value={preview.channelId}
              placeholder='145'
              onChange={(value) => updatePreview({ channelId: value })}
            />
          </div>
        </Col>
        <Col xs={24} md={8}>
          <div style={{ marginBottom: 12 }}>
            <Text strong>{t('样例渠道类型')}</Text>
            <Input
              name='protocol-preview-channel-type'
              value={preview.channelType}
              placeholder='1'
              onChange={(value) => updatePreview({ channelType: value })}
            />
          </div>
        </Col>
        <Col xs={24} md={8}>
          <div style={{ marginBottom: 12 }}>
            <Text strong>{t('样例模型')}</Text>
            <Input
              name='protocol-preview-model'
              value={preview.model}
              placeholder='gpt-5'
              onChange={(value) => updatePreview({ model: value })}
            />
          </div>
        </Col>
      </Row>
      <Banner
        type={result.matched ? 'success' : 'warning'}
        description={t(result.reason)}
      />
    </div>
  );
}

export default function ProtocolPolicyRuleBody({
  channelTypeOptions,
  directionInvalid,
  index,
  passThroughEnabled,
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
      <AdvancedPanel index={index} rule={rule} t={t} updateRule={updateRule} />
      <HitPreviewPanel
        passThroughEnabled={passThroughEnabled}
        rule={rule}
        t={t}
      />
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Popconfirm
          content={t('删除后需要重新保存配置才会生效，确定删除这条规则吗？')}
          okText={t('删除')}
          cancelText={t('取消')}
          onConfirm={() => removeRule(index)}
        >
          <Button type='danger' theme='borderless'>
            {t('删除当前规则')}
          </Button>
        </Popconfirm>
      </div>
    </div>
  );
}
