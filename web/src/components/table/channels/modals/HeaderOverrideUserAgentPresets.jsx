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
import { Space, Tag, Typography } from '@douyinfe/semi-ui';
import { HEADER_OVERRIDE_USER_AGENT_PRESET_GROUPS } from '../../../../helpers/headerOverrideUserAgent';

const { Text } = Typography;

export default function HeaderOverrideUserAgentPresets({
  t,
  onSelect,
  showTitle = true,
  compact = false,
  activeValues = [],
  disabled = false,
}) {
  const activeSet = new Set(activeValues || []);
  return (
    <div className={`flex flex-col ${compact ? 'gap-2' : 'gap-3'}`}>
      {showTitle && (
        <Text type='tertiary' size='small'>
          {t('UA 预置模板')}
        </Text>
      )}
      {HEADER_OVERRIDE_USER_AGENT_PRESET_GROUPS.map((group) => (
        <div
          key={group.key}
          className={`rounded-lg border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-0)] ${
            compact ? 'px-3 py-2' : 'px-3 py-2.5'
          }`}
          style={{
            boxShadow: 'none',
          }}
        >
          <Text type='tertiary' size='small'>
            {t(group.label)}
          </Text>
          <Space
            wrap
            spacing={compact ? 4 : 6}
            className={compact ? 'mt-1.5' : 'mt-2'}
          >
            {group.items.map((item) => (
              <Tag
                key={item.id}
                color='grey'
                size='small'
                shape='circle'
                className={`select-none transition-colors duration-200 ${
                  disabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'
                }`}
                style={{
                  backgroundColor: activeSet.has(item.ua)
                    ? 'var(--semi-color-primary-light-default)'
                    : 'var(--semi-color-bg-1)',
                  border: activeSet.has(item.ua)
                    ? '1px solid var(--semi-color-primary)'
                    : '1px solid var(--semi-color-border)',
                  color: activeSet.has(item.ua)
                    ? 'var(--semi-color-primary)'
                    : undefined,
                }}
                onClick={() => {
                  if (disabled) {
                    return;
                  }
                  onSelect(item);
                }}
              >
                {item.label}
              </Tag>
            ))}
          </Space>
        </div>
      ))}
    </div>
  );
}
