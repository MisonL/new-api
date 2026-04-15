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
import { IconEyeOpened } from '@douyinfe/semi-icons';
import CompactModeToggle from '../../common/ui/CompactModeToggle';

const { Text } = Typography;

const TaskLogsActions = ({ compactMode, setCompactMode, t }) => {
  return (
    <div className='aux-logs-actions flex flex-col md:flex-row justify-between items-start md:items-center gap-3 w-full'>
      <div className='aux-logs-summary-card'>
        <div className='aux-logs-summary-icon aux-logs-summary-icon-orange'>
          <IconEyeOpened />
        </div>
        <div className='aux-logs-summary-content'>
          <Text strong>{t('任务记录')}</Text>
          <Text size='small' type='tertiary'>
            {t('查看异步任务状态、回调结果与失败信息')}
          </Text>
        </div>
        <Tag color='orange' className='aux-logs-summary-tag !rounded-xl'>
          {t('任务')}
        </Tag>
      </div>

      <div className='usage-logs-actions-toolbar flex items-center gap-2'>
        <CompactModeToggle
          compactMode={compactMode}
          setCompactMode={setCompactMode}
          t={t}
          className='usage-logs-action-button'
        />
      </div>
    </div>
  );
};

export default TaskLogsActions;
