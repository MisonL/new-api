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

import React, { useMemo } from 'react';
import {
  Banner,
  Descriptions,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  formatPerformanceLatency,
  formatPerformancePercent,
  useModelPerformance,
} from '../../../../hooks/model-pricing/useModelPerformance';

const { Text } = Typography;

export default function ModelPerformanceDetails({ modelName, visible, t }) {
  const state = useModelPerformance(modelName, visible);
  const groups = state.data?.groups || [];
  const totals = useMemo(() => {
    const requestCount = groups.reduce(
      (sum, item) => sum + (item.request_count || 0),
      0,
    );
    const successCount = groups.reduce(
      (sum, item) => sum + (item.success_count || 0),
      0,
    );
    const latencyTotal = groups.reduce(
      (sum, item) =>
        sum + (item.avg_latency_ms || 0) * (item.request_count || 0),
      0,
    );
    return {
      requestCount,
      successRate: requestCount ? (successCount / requestCount) * 100 : 0,
      avgLatency: requestCount ? latencyTotal / requestCount : 0,
    };
  }, [groups]);

  if (!modelName) return null;
  if (state.loading)
    return <Spin spinning style={{ width: '100%', padding: 24 }} />;
  if (state.error) return <Banner type='warning' description={state.error} />;
  if (!groups.length) {
    return <Text type='tertiary'>{t('最近 24 小时暂无模型性能数据')}</Text>;
  }

  const columns = [
    { title: t('分组'), dataIndex: 'group', key: 'group' },
    {
      title: t('平均 TTFT'),
      dataIndex: 'avg_ttft_ms',
      key: 'ttft',
      render: (value) => formatPerformanceLatency(value),
    },
    {
      title: t('平均延迟'),
      dataIndex: 'avg_latency_ms',
      key: 'latency',
      render: (value) => formatPerformanceLatency(value),
    },
    {
      title: t('上游首字节'),
      dataIndex: 'avg_upstream_ttfb_ms',
      key: 'upstream-ttfb',
      render: (value, record) =>
        record.upstream_ttfb_count ? formatPerformanceLatency(value) : '-',
    },
    {
      title: t('上游总耗时'),
      dataIndex: 'avg_upstream_total_ms',
      key: 'upstream-total',
      render: (value, record) =>
        record.upstream_total_count ? formatPerformanceLatency(value) : '-',
    },
    {
      title: t('成功率'),
      dataIndex: 'success_rate',
      key: 'success',
      render: (value) => (
        <Tag color={value < 99 ? 'red' : 'teal'}>
          {formatPerformancePercent(value)}
        </Tag>
      ),
    },
    { title: t('请求数'), dataIndex: 'request_count', key: 'requests' },
  ];

  return (
    <div className='flex flex-col gap-3'>
      <Descriptions
        data={[
          {
            key: t('平均延迟'),
            value: formatPerformanceLatency(totals.avgLatency),
          },
          {
            key: t('成功率'),
            value: formatPerformancePercent(totals.successRate),
          },
          { key: t('请求数'), value: totals.requestCount },
        ]}
      />
      <Text strong>{t('按分组性能')}</Text>
      <Table
        columns={columns}
        dataSource={groups}
        pagination={false}
        size='small'
        rowKey='group'
      />
    </div>
  );
}
