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

export default function HeaderOverrideUserAgentPresets({ t, onSelect }) {
  return (
    <div className='flex flex-col gap-2'>
      <Text type='tertiary' size='small'>
        {t('UA 预置模板')}
      </Text>
      {HEADER_OVERRIDE_USER_AGENT_PRESET_GROUPS.map((group) => (
        <div key={group.key} className='flex flex-col gap-1'>
          <Text type='tertiary' size='small'>
            {t(group.label)}
          </Text>
          <Space wrap spacing={6}>
            {group.items.map((item) => (
              <Tag
                key={item.id}
                color='grey'
                size='small'
                className='cursor-pointer select-none'
                onClick={() => onSelect(item)}
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
