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
  Button,
  Empty,
  Modal,
  TabPane,
  Table,
  Tabs,
  Typography,
} from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import { renderNumber, renderQuota } from '../../helpers';

const { Text } = Typography;

const DashboardDrilldownModal = ({
  detail,
  modelColors,
  chartConfig,
  onClose,
  onOpenLogs,
  t,
}) => {
  const columns = useMemo(
    () => [
      {
        title: t('模型'),
        dataIndex: 'model',
        key: 'model',
        render: (value) => (
          <Text ellipsis={{ showTooltip: true }} className='max-w-[260px]'>
            {value}
          </Text>
        ),
      },
      {
        title: t('消耗'),
        dataIndex: 'quota',
        key: 'quota',
        sorter: (a, b) => a.quota - b.quota,
        render: (value) => renderQuota(value || 0, 4),
      },
      {
        title: t('占比'),
        dataIndex: 'ratio',
        key: 'ratio',
        sorter: (a, b) => a.ratio - b.ratio,
        render: (value) => `${((value || 0) * 100).toFixed(2)}%`,
      },
      {
        title: t('调用次数'),
        dataIndex: 'count',
        key: 'count',
        sorter: (a, b) => a.count - b.count,
        render: (value) => renderNumber(value || 0),
      },
      {
        title: t('Token 数'),
        dataIndex: 'tokens',
        key: 'tokens',
        sorter: (a, b) => a.tokens - b.tokens,
        render: (value) => renderNumber(value || 0),
      },
    ],
    [t],
  );

  const spec = useMemo(
    () => ({
      type: 'pie',
      data: [
        { id: 'dashboardDrilldownData', values: detail?.distribution || [] },
      ],
      outerRadius: 0.78,
      innerRadius: 0.5,
      padAngle: 0.6,
      valueField: 'value',
      categoryField: 'type',
      legends: { visible: true, orient: 'right' },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum) => datum.type,
              value: (datum) => renderQuota(datum.value || 0, 4),
            },
          ],
        },
      },
      color: { specified: modelColors || {} },
    }),
    [detail?.distribution, modelColors],
  );

  return (
    <Modal
      title={detail ? `${t('消耗明细')} · ${detail.time}` : t('消耗明细')}
      visible={!!detail}
      onCancel={onClose}
      footer={null}
      width={960}
    >
      {detail ? (
        <div className='flex flex-col gap-4'>
          <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
            <SummaryItem
              label={t('总消耗')}
              value={renderQuota(detail.totalQuota, 4)}
            />
            <SummaryItem
              label={t('调用次数')}
              value={renderNumber(detail.totalCount)}
            />
            <SummaryItem
              label={t('Token 数')}
              value={renderNumber(detail.totalTokens)}
            />
          </div>
          <Tabs
            type='line'
            defaultActiveKey='detail'
            keepDOM={false}
            tabBarExtraContent={
              <Button
                size='small'
                type='tertiary'
                theme='light'
                onClick={onOpenLogs}
              >
                {t('查看日志')}
              </Button>
            }
          >
            <TabPane tab={t('明细')} itemKey='detail'>
              <Table
                columns={columns}
                dataSource={detail.rows}
                rowKey='model'
                pagination={
                  detail.rows.length > 8
                    ? { pageSize: 8, showSizeChanger: false }
                    : false
                }
                size='small'
              />
            </TabPane>
            <TabPane tab={t('分布')} itemKey='distribution'>
              <div className='h-[360px] rounded-lg border border-semi-color-border bg-semi-color-bg-1 p-3'>
                {detail.distribution.length > 0 ? (
                  <VChart spec={spec} option={chartConfig} />
                ) : (
                  <Empty title={t('暂无数据')} />
                )}
              </div>
            </TabPane>
          </Tabs>
        </div>
      ) : null}
    </Modal>
  );
};

const SummaryItem = ({ label, value }) => (
  <div className='rounded-lg border border-semi-color-border bg-semi-color-bg-1 p-3'>
    <Text type='tertiary' size='small'>
      {label}
    </Text>
    <div className='mt-1 text-base font-semibold'>{value}</div>
  </div>
);

export default DashboardDrilldownModal;
