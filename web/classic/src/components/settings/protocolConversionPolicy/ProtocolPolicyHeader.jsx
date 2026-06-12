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
  Button,
  Input,
  Radio,
  RadioGroup,
  Select,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { EDIT_MODE_JSON, EDIT_MODE_VISUAL, panelStyle } from './constants';

const { Text } = Typography;

const selectionFieldStyle = {
  display: 'flex',
  flexDirection: 'column',
  gap: 6,
  flex: '1 1 220px',
  minWidth: 180,
};

function RuleStats({
  enabledRuleCount,
  invalidDirectionRuleCount,
  invalidScopeRuleCount,
  passThroughEnabled,
  rules,
  t,
}) {
  return (
    <div style={{ marginTop: 8, display: 'flex', gap: 8, flexWrap: 'wrap' }}>
      <Tag color='light-blue'>
        {t('共 {{total}} 条规则', { total: rules.length })}
      </Tag>
      <Tag color='green'>
        {t('启用 {{enabled}} 条', { enabled: enabledRuleCount })}
      </Tag>
      {invalidDirectionRuleCount > 0 ? (
        <Tag color='red'>
          {t('方向异常 {{count}} 条', { count: invalidDirectionRuleCount })}
        </Tag>
      ) : null}
      {invalidScopeRuleCount > 0 ? (
        <Tag color='red'>
          {t('范围未命中 {{count}} 条', { count: invalidScopeRuleCount })}
        </Tag>
      ) : null}
      {passThroughEnabled ? (
        <Tag color='orange'>{t('全局透传已启用')}</Tag>
      ) : null}
    </div>
  );
}

function ProtocolMetric({ detail, intent = 'default', label, value }) {
  const warningStyle =
    intent === 'warning'
      ? {
          borderColor: 'var(--semi-color-warning)',
          background: 'var(--semi-color-warning-light-default)',
        }
      : {};
  return (
    <div
      style={{
        border: '1px solid var(--semi-color-border)',
        borderRadius: 10,
        padding: 12,
        background: 'var(--semi-color-bg-1)',
        ...warningStyle,
      }}
    >
      <Text type='tertiary' size='small'>
        {label}
      </Text>
      <div style={{ fontSize: 24, fontWeight: 700, lineHeight: 1.2 }}>
        {value}
      </div>
      <Text type='tertiary' size='small'>
        {detail}
      </Text>
    </div>
  );
}

function FilterSelect({ label, onChange, optionList, value }) {
  return (
    <div style={selectionFieldStyle}>
      <Text type='tertiary' size='small'>
        {label}
      </Text>
      <Select
        allowClear={false}
        optionList={optionList}
        size='small'
        value={value}
        onChange={onChange}
      />
    </div>
  );
}

function RuleFilterControls({
  filterOptions,
  filteredRuleCount,
  hasActiveFilters,
  resetRuleFilters,
  ruleFilters,
  setRuleFilters,
  stats,
  t,
}) {
  return (
    <div style={{ marginTop: 14, display: 'flex', flexDirection: 'column' }}>
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))',
          gap: 10,
        }}
      >
        <ProtocolMetric
          label={t('规则库存')}
          value={`${stats.enabled}/${stats.total}`}
          detail={t('{{count}} 条已停用', { count: stats.disabled })}
        />
        <ProtocolMetric
          label={t('Chat 到 Responses')}
          value={stats.chatToResponses}
          detail={t('客户端 Chat 请求')}
        />
        <ProtocolMetric
          label={t('Responses 到 Chat')}
          value={stats.responsesToChat}
          detail={t('仅支持 Chat 的上游')}
        />
        <ProtocolMetric
          label={t('需关注')}
          value={stats.attention}
          detail={t('停用、空范围或全局透传')}
          intent={stats.attention > 0 ? 'warning' : 'default'}
        />
      </div>

      <div
        style={{
          marginTop: 12,
          display: 'flex',
          gap: 10,
          flexWrap: 'wrap',
          alignItems: 'flex-end',
        }}
      >
        <FilterSelect
          label={t('方向')}
          optionList={filterOptions.direction}
          value={ruleFilters.direction}
          onChange={(value) => setRuleFilters({ direction: value })}
        />
        <FilterSelect
          label={t('状态')}
          optionList={filterOptions.state}
          value={ruleFilters.state}
          onChange={(value) => setRuleFilters({ state: value })}
        />
        <FilterSelect
          label={t('范围')}
          optionList={filterOptions.scope}
          value={ruleFilters.scope}
          onChange={(value) => setRuleFilters({ scope: value })}
        />
        <div style={{ ...selectionFieldStyle, flex: '1 1 260px' }}>
          <Text type='tertiary' size='small'>
            {t('搜索')}
          </Text>
          <Input
            size='small'
            value={ruleFilters.query}
            placeholder={t('规则名、渠道或模型')}
            onChange={(value) => setRuleFilters({ query: value })}
          />
        </div>
        <Tag color='light-blue' style={{ height: 28, lineHeight: '28px' }}>
          {t('{{count}} 条可见', { count: filteredRuleCount })}
        </Tag>
        <Button
          size='small'
          type='tertiary'
          disabled={!hasActiveFilters}
          onClick={resetRuleFilters}
        >
          {t('重置筛选')}
        </Button>
      </div>

      <div style={{ marginTop: 8, display: 'flex', gap: 12, flexWrap: 'wrap' }}>
        <Text type='tertiary' size='small'>
          {t('{{count}} 条全部渠道规则', { count: stats.allChannels })}
        </Text>
        <Text type='tertiary' size='small'>
          {t('{{count}} 条限定范围规则', { count: stats.limitedScope })}
        </Text>
        <Text type='tertiary' size='small'>
          {t('{{count}} 条空范围规则', { count: stats.emptyScope })}
        </Text>
      </div>
    </div>
  );
}

function ModeControls({
  editMode,
  editModeOptions,
  formatJsonValue,
  handleEditModeChange,
  t,
}) {
  return (
    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
      <div style={selectionFieldStyle}>
        <Text strong size='small'>
          {t('编辑模式')}
        </Text>
        <RadioGroup
          aria-label={t('编辑模式')}
          direction='horizontal'
          name='protocol-policy-edit-mode'
          value={editMode}
          onChange={(event) => handleEditModeChange(event.target.value)}
        >
          {editModeOptions.map((option) => (
            <Radio key={option.value} value={option.value}>
              {option.label}
            </Radio>
          ))}
        </RadioGroup>
      </div>
      {editMode === EDIT_MODE_JSON ? (
        <Button type='secondary' onClick={formatJsonValue}>
          {t('格式化 JSON')}
        </Button>
      ) : null}
    </div>
  );
}

function VisualControls({
  addRuleByTemplateType,
  isAllRulesExpanded,
  newRuleTemplateOptions,
  newRuleTemplateType,
  rules,
  setNewRuleTemplateType,
  t,
  toggleExpandStateForAllRules,
}) {
  if (rules.length < 0) {
    return null;
  }
  return (
    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
      <div style={selectionFieldStyle}>
        <Text strong size='small'>
          {t('新增模板')}
        </Text>
        <RadioGroup
          aria-label={t('新增模板')}
          direction='horizontal'
          name='protocol-policy-rule-template'
          value={newRuleTemplateType}
          onChange={(event) =>
            setNewRuleTemplateType(event.target.value || newRuleTemplateType)
          }
        >
          {newRuleTemplateOptions.map((option) => (
            <Radio key={option.value} value={option.value}>
              {option.label}
            </Radio>
          ))}
        </RadioGroup>
      </div>
      <Button type='primary' theme='solid' onClick={addRuleByTemplateType}>
        {t('新增规则')}
      </Button>
      {rules.length > 1 ? (
        <Button type='tertiary' onClick={toggleExpandStateForAllRules}>
          {isAllRulesExpanded ? t('全部收起') : t('全部展开')}
        </Button>
      ) : null}
    </div>
  );
}

function ModeTip({ editMode, t }) {
  const text =
    editMode === EDIT_MODE_VISUAL
      ? t(
          '先在可视化模式维护规则，确认命中范围与模型条件后，再按需切到 JSON 做高级编辑。',
        )
      : t(
          'JSON 模式用于高级配置；如果写入了暂不支持的协议值，可视化模式会拒绝切换并提示修正。',
        );
  return (
    <Text type='tertiary' size='small'>
      {text}
    </Text>
  );
}

export default function ProtocolPolicyHeader(props) {
  const {
    editMode,
    enabledRuleCount,
    invalidDirectionRuleCount,
    invalidScopeRuleCount,
    rules,
    t,
  } = props;

  return (
    <div style={{ ...panelStyle, marginTop: 12, marginBottom: 12 }}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          gap: 12,
          flexWrap: 'wrap',
          alignItems: 'flex-start',
        }}
      >
        <div style={{ flex: '1 1 280px' }}>
          <Text strong>{t('规则列表')}</Text>
          <div>
            <Text type='tertiary' size='small'>
              {t(
                '首屏仅展示每条规则的方向、命中范围和模型条件；展开后再编辑详细配置。',
              )}
            </Text>
          </div>
          <RuleStats
            enabledRuleCount={enabledRuleCount}
            invalidDirectionRuleCount={invalidDirectionRuleCount}
            invalidScopeRuleCount={invalidScopeRuleCount}
            passThroughEnabled={props.passThroughEnabled}
            rules={rules}
            t={t}
          />
        </div>

        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 10,
            flex: '1 1 420px',
            alignItems: 'stretch',
            maxWidth: 560,
          }}
        >
          <ModeControls {...props} />
          {editMode === EDIT_MODE_VISUAL ? <VisualControls {...props} /> : null}
          <ModeTip editMode={editMode} t={t} />
        </div>
      </div>
      {editMode === EDIT_MODE_VISUAL ? <RuleFilterControls {...props} /> : null}
    </div>
  );
}
