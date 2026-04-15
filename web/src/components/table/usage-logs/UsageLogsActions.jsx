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
  Divider,
  Space,
  Skeleton,
  Typography,
} from '@douyinfe/semi-ui';
import { renderQuota } from '../../../helpers';
import CompactModeToggle from '../../common/ui/CompactModeToggle';
import { useMinimumLoadingTime } from '../../../hooks/common/useMinimumLoadingTime';

const { Text } = Typography;

const LogsActions = ({
  stat,
  loadingStat,
  showStat,
  compactMode,
  setCompactMode,
  batchDeleteLogs,
  t,
}) => {
  const showSkeleton = useMinimumLoadingTime(loadingStat);
  const needSkeleton = !showStat || showSkeleton;

  const placeholder = (
    <Space>
      <Skeleton.Title style={{ width: 108, height: 21, borderRadius: 6 }} />
      <Skeleton.Title style={{ width: 65, height: 21, borderRadius: 6 }} />
      <Skeleton.Title style={{ width: 64, height: 21, borderRadius: 6 }} />
    </Space>
  );

  return (
    <div className='usage-logs-actions flex flex-col md:flex-row justify-between items-start md:items-center gap-3 w-full'>
      <Skeleton loading={needSkeleton} active placeholder={placeholder}>
        <Space
          className='usage-logs-stats-list'
          wrap
          split={<Divider layout='vertical' margin='8px' />}
        >
          <div className='usage-logs-stat-item'>
            <Text size='small' type='tertiary'>
              {t('消耗额度')}
            </Text>
            <Text strong className='usage-logs-stat-value'>
              {renderQuota(stat.quota)}
            </Text>
          </div>
          <div className='usage-logs-stat-item'>
            <Text size='small' type='tertiary'>
              RPM
            </Text>
            <Text strong className='usage-logs-stat-value'>
              {stat.rpm}
            </Text>
          </div>
          <div className='usage-logs-stat-item'>
            <Text size='small' type='tertiary'>
              TPM
            </Text>
            <Text strong className='usage-logs-stat-value'>
              {stat.tpm}
            </Text>
          </div>
        </Space>
      </Skeleton>

      <div className='usage-logs-actions-toolbar flex items-center gap-2'>
        <Button
          type='danger'
          theme='light'
          onClick={batchDeleteLogs}
          className='usage-logs-action-button'
        >
          {t('批量删除日志')}
        </Button>
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

export default LogsActions;
