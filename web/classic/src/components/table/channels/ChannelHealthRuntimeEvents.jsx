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
import { Tag, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  getChannelHealthRuntimeEventDisplay,
  getChannelHealthRuntimeEvents,
} from '../../../helpers/channelHealthRuntime';

const { Text } = Typography;

function formatEventDetails(event, t) {
  const details = [
    t('渠道 #{{channel}}', { channel: event.channel_id }),
    event.group,
    event.model_name,
  ];
  if (event.status_code) {
    details.push(t('状态码：{{status}}', { status: event.status_code }));
  }
  if (event.error_code) {
    details.push(t('错误：{{error}}', { error: event.error_code }));
  }
  const route = [
    event.effective_model ? `model=${event.effective_model}` : '',
    event.protocol ? `protocol=${event.protocol}` : '',
    event.upstream_path ? `path=${event.upstream_path}` : '',
    Number.isInteger(event.multi_key_index)
      ? `key=${event.multi_key_index}`
      : '',
    event.route_key ? `route=${event.route_key.slice(0, 12)}` : '',
  ].filter(Boolean);
  if (route.length > 0) details.push(route.join(' | '));
  return details.join(' | ');
}

export default function ChannelHealthRuntimeEvents({ runtime }) {
  const { t } = useTranslation();
  const events = getChannelHealthRuntimeEvents(runtime);
  if (events.length === 0) return null;

  return (
    <div
      className='space-y-2 border-t pt-3'
      style={{ borderColor: 'var(--semi-color-border)' }}
    >
      <div className='flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1'>
        <Text strong size='small'>
          {t('最近事件')}
        </Text>
        <Text type='tertiary' size='small'>
          {t('时间')}
        </Text>
      </div>
      <div className='space-y-2'>
        {events.map((event, index) => {
          const display = getChannelHealthRuntimeEventDisplay(event, t);
          return (
            <div
              key={[
                event.occurred_at,
                event.channel_id,
                event.action,
                event.status_code ?? '',
                event.error_code ?? '',
                event.route_key ?? '',
                event.effective_model ?? '',
                event.protocol ?? '',
                event.upstream_path ?? '',
                event.multi_key_index ?? '',
                index,
              ].join(':')}
              className='min-w-0 border-l-2 py-1.5 pl-3'
              style={{ borderColor: 'var(--semi-color-border)' }}
            >
              <div className='flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-1'>
                <Tag color={display.color} type='light' size='small'>
                  {display.label}
                </Tag>
                <Text type='tertiary' code size='small'>
                  {new Date(event.occurred_at * 1000).toLocaleString()}
                </Text>
              </div>
              <Text
                type='tertiary'
                code
                size='small'
                className='mt-1 block break-words'
              >
                {formatEventDetails(event, t)}
              </Text>
            </div>
          );
        })}
      </div>
    </div>
  );
}
