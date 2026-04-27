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
import { Button, Skeleton, Space, Tag } from '@douyinfe/semi-ui';
import { renderQuota } from '../../../helpers';
import CompactModeToggle from '../../common/ui/CompactModeToggle';
import { useMinimumLoadingTime } from '../../../hooks/common/useMinimumLoadingTime';

const LogStatButton = ({ loadingStat, handleEyeClick, t, label }) => (
  <Button
    type='tertiary'
    theme='light'
    size='small'
    loading={loadingStat}
    onClick={handleEyeClick}
  >
    {label || t('查看统计')}
  </Button>
);

const LogStatsContent = ({ stat, loadingStat, handleEyeClick, t }) => (
  <Space className='usage-logs-stats-list' wrap>
    <Tag color='blue' className='usage-logs-stat-tag'>
      {t('消耗额度')}: {renderQuota(stat.quota)}
    </Tag>
    <Tag color='pink' className='usage-logs-stat-tag'>
      {t('RPM')}: {stat.rpm}
    </Tag>
    <Tag
      color='white'
      className='usage-logs-stat-tag usage-logs-stat-tag-neutral'
    >
      {t('TPM')}: {stat.tpm}
    </Tag>
    <LogStatButton
      loadingStat={loadingStat}
      handleEyeClick={handleEyeClick}
      t={t}
      label={t('刷新统计')}
    />
  </Space>
);

const LogsActionsToolbar = ({
  batchDeleteLogs,
  compactMode,
  setCompactMode,
  t,
}) => (
  <div className='usage-logs-actions-toolbar flex items-center gap-2'>
    <Button type='danger' theme='light' onClick={batchDeleteLogs} size='small'>
      {t('批量删除日志')}
    </Button>
    <CompactModeToggle
      compactMode={compactMode}
      setCompactMode={setCompactMode}
      t={t}
    />
  </div>
);

const LogsActions = ({
  stat,
  loadingStat,
  showStat,
  compactMode,
  setCompactMode,
  batchDeleteLogs,
  handleEyeClick,
  t,
}) => {
  const showSkeleton = useMinimumLoadingTime(loadingStat);
  const needSkeleton = showStat && showSkeleton;

  const placeholder = (
    <Space>
      <Skeleton.Title style={{ width: 108, height: 21, borderRadius: 6 }} />
      <Skeleton.Title style={{ width: 65, height: 21, borderRadius: 6 }} />
      <Skeleton.Title style={{ width: 64, height: 21, borderRadius: 6 }} />
    </Space>
  );

  return (
    <div className='usage-logs-actions flex flex-col md:flex-row justify-between items-start md:items-center gap-2 w-full'>
      {showStat ? (
        <Skeleton loading={needSkeleton} active placeholder={placeholder}>
          <LogStatsContent
            stat={stat}
            loadingStat={loadingStat}
            handleEyeClick={handleEyeClick}
            t={t}
          />
        </Skeleton>
      ) : (
        <LogStatButton
          loadingStat={loadingStat}
          handleEyeClick={handleEyeClick}
          t={t}
        />
      )}

      <LogsActionsToolbar
        batchDeleteLogs={batchDeleteLogs}
        compactMode={compactMode}
        setCompactMode={setCompactMode}
        t={t}
      />
    </div>
  );
};

export default LogsActions;
