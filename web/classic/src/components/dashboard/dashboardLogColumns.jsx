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
import { renderNumber, renderQuota, timestamp2string } from '../../helpers';

export const getDashboardLogColumns = ({ isAdminUser, t }) => [
  {
    title: t('时间'),
    dataIndex: 'created_at',
    key: 'created_at',
    width: 170,
    render: (value) => timestamp2string(value),
  },
  ...(isAdminUser
    ? [
        {
          title: t('用户'),
          dataIndex: 'username',
          key: 'username',
          width: 120,
        },
      ]
    : []),
  {
    title: t('模型'),
    dataIndex: 'model_name',
    key: 'model_name',
    width: 180,
    render: (value) => (
      <Typography.Text
        className='block max-w-full'
        ellipsis={{ showTooltip: true }}
      >
        {value || '-'}
      </Typography.Text>
    ),
  },
  {
    title: t('令牌'),
    dataIndex: 'token_name',
    key: 'token_name',
    width: 140,
    render: (value) => value || '-',
  },
  {
    title: t('渠道'),
    dataIndex: 'channel',
    key: 'channel',
    width: 90,
    render: (_, record) =>
      record.channel ? (
        <Tag color='blue' shape='circle'>
          {record.channel}
        </Tag>
      ) : (
        '-'
      ),
  },
  {
    title: t('输入'),
    dataIndex: 'prompt_tokens',
    key: 'prompt_tokens',
    width: 90,
    render: (value) => renderNumber(value || 0),
  },
  {
    title: t('输出'),
    dataIndex: 'completion_tokens',
    key: 'completion_tokens',
    width: 90,
    render: (value) => renderNumber(value || 0),
  },
  {
    title: t('花费'),
    dataIndex: 'quota',
    key: 'quota',
    width: 120,
    render: (value) => renderQuota(value || 0, 6),
  },
  {
    title: t('请求 ID'),
    dataIndex: 'request_id',
    key: 'request_id',
    width: 320,
    render: (value) => (
      <Typography.Text
        className='block max-w-full'
        ellipsis={{ showTooltip: true }}
      >
        {value || '-'}
      </Typography.Text>
    ),
  },
];
