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
import { Button, Space, Tooltip, Typography } from '@douyinfe/semi-ui';
import { HEADER_OVERRIDE_USER_AGENT_PRESET_GROUPS } from '../../../../helpers/headerOverrideUserAgent';

const { Text } = Typography;

export default function HeaderOverrideUserAgentPresets({
  t,
  onSelect,
  showTitle = true,
  compact = false,
  activeValues = [],
  disabled = false,
  showGroupHint = true,
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
          className={`rounded-lg border ${
            compact ? 'px-3 py-2' : 'px-3 py-2.5'
          }`}
          style={{
            borderColor: 'var(--semi-color-border)',
            backgroundColor: 'var(--semi-color-fill-0)',
            boxShadow: 'none',
          }}
        >
          <Text strong size='small'>
            {t(group.label)}
          </Text>
          {showGroupHint && (
            <div className='mt-0.5'>
              <Text type='tertiary' size='small'>
                {t('点击快速加入到当前 UA 列表')}
              </Text>
            </div>
          )}
          <Space
            wrap
            spacing={compact ? 4 : 6}
            className={
              compact
                ? showGroupHint
                  ? 'mt-1.5'
                  : 'mt-1'
                : 'mt-2'
            }
          >
            {group.items.map((item) => (
              <Tooltip key={item.id} content={item.ua} position='top'>
                <Button
                  type={activeSet.has(item.ua) ? 'primary' : 'tertiary'}
                  theme={activeSet.has(item.ua) ? 'light' : 'outline'}
                  size='small'
                  className={`transition-all duration-200 ${
                    disabled ? 'cursor-not-allowed opacity-60' : ''
                  }`}
                  style={{
                    borderRadius: 9999,
                    fontWeight: activeSet.has(item.ua) ? 600 : 400,
                  }}
                  disabled={disabled}
                  onClick={() => {
                    if (disabled) {
                      return;
                    }
                    onSelect(item);
                  }}
                >
                  {item.label}
                </Button>
              </Tooltip>
            ))}
          </Space>
        </div>
      ))}
    </div>
  );
}
