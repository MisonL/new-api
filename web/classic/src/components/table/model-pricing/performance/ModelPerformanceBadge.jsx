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
import { Tag, Tooltip } from '@douyinfe/semi-ui';
import {
  formatPerformanceLatency,
  formatPerformancePercent,
  formatPerformanceThroughput,
} from '../../../../hooks/model-pricing/useModelPerformance';

export default function ModelPerformanceBadge({ performance, t }) {
  if (!performance) return null;
  return (
    <Tooltip content={t('最近 24 小时模型性能摘要')}>
      <div
        className='flex flex-wrap items-center gap-1 text-xs'
        onClick={(event) => event.stopPropagation()}
      >
        <Tag color='blue'>
          {t('延迟')} {formatPerformanceLatency(performance.avg_latency_ms)}
        </Tag>
        <Tag color='green'>
          {t('吞吐')} {formatPerformanceThroughput(performance.avg_tps)}
        </Tag>
        <Tag color={performance.success_rate < 99 ? 'red' : 'teal'}>
          {t('成功率')} {formatPerformancePercent(performance.success_rate)}
        </Tag>
      </div>
    </Tooltip>
  );
}
