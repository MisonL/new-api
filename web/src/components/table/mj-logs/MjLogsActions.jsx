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
import { Skeleton, Tag, Typography } from '@douyinfe/semi-ui';
import { useMinimumLoadingTime } from '../../../hooks/common/useMinimumLoadingTime';
import { IconEyeOpened } from '@douyinfe/semi-icons';
import CompactModeToggle from '../../common/ui/CompactModeToggle';

const { Text } = Typography;

const MjLogsActions = ({
  loading,
  showBanner,
  isAdminUser,
  compactMode,
  setCompactMode,
  t,
}) => {
  const showSkeleton = useMinimumLoadingTime(loading);
  const description =
    isAdminUser && showBanner
      ? t('当前未开启 Midjourney 回调，部分任务可能无法自动回填结果。')
      : t('查看绘图任务进度、结果地址与失败原因');

  const placeholder = (
    <div className='logs-inline-summary'>
      <div className='logs-inline-summary-icon logs-inline-summary-icon-purple'>
        <IconEyeOpened />
      </div>
      <div className='flex-1 min-w-0'>
        <Skeleton.Title style={{ width: 220, height: 18, borderRadius: 8 }} />
        <Skeleton.Paragraph rows={1} style={{ width: 300, marginTop: 10 }} />
      </div>
    </div>
  );

  return (
    <div className='aux-logs-actions flex flex-col md:flex-row justify-between items-start md:items-center gap-3 w-full'>
      <Skeleton loading={showSkeleton} active placeholder={placeholder}>
        <div className='logs-inline-summary'>
          <div className='logs-inline-summary-icon logs-inline-summary-icon-purple'>
            <IconEyeOpened />
          </div>
          <div className='logs-inline-summary-content'>
            <div className='logs-inline-summary-header'>
              <Text strong>{t('Midjourney 任务记录')}</Text>
              <Tag
                color={isAdminUser && showBanner ? 'red' : 'violet'}
                size='small'
                className='logs-inline-summary-tag'
              >
                {isAdminUser && showBanner ? t('需回调') : t('绘图')}
              </Tag>
            </div>
            <Text size='small' type='tertiary'>
              {description}
            </Text>
          </div>
        </div>
      </Skeleton>

      <div className='usage-logs-actions-toolbar flex items-center gap-2'>
        <CompactModeToggle
          compactMode={compactMode}
          setCompactMode={setCompactMode}
          t={t}
        />
      </div>
    </div>
  );
};

export default MjLogsActions;
