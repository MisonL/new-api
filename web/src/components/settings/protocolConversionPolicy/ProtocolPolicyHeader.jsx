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
import { Button, Select, Tag, Typography } from '@douyinfe/semi-ui';
import { EDIT_MODE_JSON, EDIT_MODE_VISUAL, panelStyle } from './constants';

const { Text } = Typography;

function RuleStats({ enabledRuleCount, invalidDirectionRuleCount, rules, t }) {
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
      <div style={{ flex: '1 1 220px', minWidth: 180 }}>
        <Select
          optionList={editModeOptions}
          value={editMode}
          onChange={handleEditModeChange}
          insetLabel={t('编辑模式')}
        />
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
      <div style={{ flex: '1 1 220px', minWidth: 180 }}>
        <Select
          optionList={newRuleTemplateOptions}
          value={newRuleTemplateType}
          onChange={(nextValue) =>
            setNewRuleTemplateType(nextValue || newRuleTemplateType)
          }
          insetLabel={t('新增模板')}
        />
      </div>
      <Button type='secondary' onClick={addRuleByTemplateType}>
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
  const { editMode, enabledRuleCount, invalidDirectionRuleCount, rules, t } =
    props;

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
    </div>
  );
}
