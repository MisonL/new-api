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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Banner,
  Card,
  Col,
  Empty,
  Row,
  Spin,
  Table,
  Tabs,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import LazyVChart from '../../components/dashboard/LazyVChart';
import { API } from '../../helpers';

const { Text, Title } = Typography;
const VALID_PERIODS = new Set(['today', 'week', 'month', 'year', 'all']);

function formatTokens(value) {
  const numeric = Number(value) || 0;
  if (numeric >= 1000000000) return `${(numeric / 1000000000).toFixed(1)}B`;
  if (numeric >= 1000000) return `${(numeric / 1000000).toFixed(1)}M`;
  if (numeric >= 1000) return `${(numeric / 1000).toFixed(1)}K`;
  return numeric.toLocaleString();
}

function GrowthTag({ value }) {
  const numeric = Number(value) || 0;
  const color = numeric > 0 ? 'green' : numeric < 0 ? 'red' : 'grey';
  return (
    <Tag color={color} size='small'>
      {numeric > 0 ? '+' : ''}
      {numeric.toFixed(1)}%
    </Tag>
  );
}

function buildModelsChart(snapshot) {
  const points = snapshot?.models_history?.points || [];
  if (!points.length) return null;
  return {
    type: 'bar',
    data: [{ id: 'model-history', values: points }],
    xField: 'label',
    yField: 'tokens',
    seriesField: 'model',
    stack: true,
    legends: { visible: false },
    axes: [
      { orient: 'bottom', label: { autoHide: true, autoLimit: true } },
      {
        orient: 'left',
        label: { formatMethod: (value) => formatTokens(value) },
      },
    ],
    tooltip: {
      mark: {
        content: [
          {
            key: (datum) => datum.model,
            value: (datum) => formatTokens(datum.tokens),
          },
        ],
      },
    },
  };
}

function buildVendorChart(snapshot) {
  const points = snapshot?.vendor_share_history?.points || [];
  if (!points.length) return null;
  return {
    type: 'bar',
    data: [{ id: 'vendor-share', values: points }],
    xField: 'label',
    yField: 'share',
    seriesField: 'vendor',
    stack: true,
    legends: { visible: false },
    axes: [
      { orient: 'bottom', label: { autoHide: true, autoLimit: true } },
      {
        orient: 'left',
        min: 0,
        max: 1,
        label: {
          formatMethod: (value) => `${Math.round(Number(value) * 100)}%`,
        },
      },
    ],
    tooltip: {
      mark: {
        content: [
          {
            key: (datum) => datum.vendor,
            value: (datum) => `${(Number(datum.share) * 100).toFixed(1)}%`,
          },
        ],
      },
    },
  };
}

function MoversCard({ title, rows, direction, t }) {
  return (
    <Card title={title} className='h-full'>
      {rows.length === 0 ? (
        <Empty description={t('暂无排行变化')} />
      ) : (
        <div className='flex flex-col gap-3'>
          {rows.map((row) => (
            <div
              key={row.model_name}
              className='flex items-center justify-between gap-3'
            >
              <div className='min-w-0'>
                <Text strong ellipsis={{ showTooltip: true }}>
                  {row.model_name}
                </Text>
                <div>
                  <Text type='tertiary' size='small'>
                    #{row.current_rank} / {row.vendor}
                  </Text>
                </div>
              </div>
              <Tag color={direction === 'up' ? 'green' : 'red'}>
                {direction === 'up' ? '+' : '-'}
                {Math.abs(row.rank_delta)}
              </Tag>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

export default function Rankings() {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedPeriod = searchParams.get('period') || 'week';
  const period = VALID_PERIODS.has(requestedPeriod) ? requestedPeriod : 'week';
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [snapshot, setSnapshot] = useState(null);
  const requestIdRef = useRef(0);

  useEffect(() => {
    const requestId = ++requestIdRef.current;
    setLoading(true);
    setError('');
    API.get('/api/rankings', { params: { period } })
      .then((response) => {
        if (requestId !== requestIdRef.current) return;
        if (!response?.data?.success || !response.data.data) {
          throw new Error(response?.data?.message || t('排行榜数据加载失败'));
        }
        setSnapshot(response.data.data);
      })
      .catch((requestError) => {
        if (requestId !== requestIdRef.current) return;
        setSnapshot(null);
        setError(requestError?.message || t('排行榜数据加载失败'));
      })
      .finally(() => {
        if (requestId === requestIdRef.current) setLoading(false);
      });
  }, [period, t]);

  const modelChart = useMemo(() => buildModelsChart(snapshot), [snapshot]);
  const vendorChart = useMemo(() => buildVendorChart(snapshot), [snapshot]);

  const modelColumns = [
    { title: t('排名'), dataIndex: 'rank', width: 72 },
    { title: t('模型'), dataIndex: 'model_name' },
    { title: t('供应商'), dataIndex: 'vendor', width: 150 },
    {
      title: t('Token 数量'),
      dataIndex: 'total_tokens',
      width: 140,
      render: (value) => formatTokens(value),
    },
    {
      title: t('占比'),
      dataIndex: 'share',
      width: 100,
      render: (value) => `${(Number(value) * 100).toFixed(1)}%`,
    },
    {
      title: t('增长'),
      dataIndex: 'growth_pct',
      width: 100,
      render: (value) => <GrowthTag value={value} />,
    },
  ];
  const vendorColumns = [
    { title: t('排名'), dataIndex: 'rank', width: 72 },
    { title: t('供应商'), dataIndex: 'vendor' },
    {
      title: t('Token 数量'),
      dataIndex: 'total_tokens',
      render: (value) => formatTokens(value),
    },
    {
      title: t('占比'),
      dataIndex: 'share',
      render: (value) => `${(Number(value) * 100).toFixed(1)}%`,
    },
    { title: t('模型数'), dataIndex: 'models_count' },
    { title: t('热门模型'), dataIndex: 'top_model' },
  ];

  const periods = [
    ['today', t('今日')],
    ['week', t('本周')],
    ['month', t('本月')],
    ['year', t('今年')],
    ['all', t('全部时间')],
  ];

  return (
    <div className='mx-auto w-full max-w-screen-xl px-3 py-6 sm:px-6'>
      <div className='mb-5'>
        <Title heading={2}>{t('排行榜')}</Title>
        <Text type='tertiary'>
          {t('基于平台实时用量统计模型和供应商热度。')}
        </Text>
      </div>

      <Tabs
        type='line'
        activeKey={period}
        onChange={(nextPeriod) => setSearchParams({ period: nextPeriod })}
        className='mb-5'
      >
        {periods.map(([key, label]) => (
          <Tabs.TabPane key={key} itemKey={key} tab={label} />
        ))}
      </Tabs>

      {error ? (
        <Banner type='danger' description={error} className='mb-4' />
      ) : null}
      <Spin spinning={loading} size='large'>
        {!loading && !snapshot ? (
          <Empty description={t('暂无排行榜数据')} />
        ) : snapshot ? (
          <div className='flex flex-col gap-5'>
            <Row gutter={[16, 16]}>
              <Col span={24} lg={12}>
                <Card title={t('模型 Token 趋势')} className='h-full'>
                  <div className='h-72'>
                    {modelChart ? (
                      <LazyVChart spec={modelChart} />
                    ) : (
                      <Empty description={t('暂无历史趋势数据')} />
                    )}
                  </div>
                </Card>
              </Col>
              <Col span={24} lg={12}>
                <Card title={t('供应商市场份额')} className='h-full'>
                  <div className='h-72'>
                    {vendorChart ? (
                      <LazyVChart spec={vendorChart} />
                    ) : (
                      <Empty description={t('暂无历史趋势数据')} />
                    )}
                  </div>
                </Card>
              </Col>
            </Row>

            <Card title={t('热门模型')}>
              <Table
                columns={modelColumns}
                dataSource={snapshot.models || []}
                rowKey='model_name'
                pagination={false}
                scroll={{ x: 760 }}
              />
            </Card>

            <Card title={t('供应商排行')}>
              <Table
                columns={vendorColumns}
                dataSource={snapshot.vendors || []}
                rowKey='vendor'
                pagination={false}
                scroll={{ x: 720 }}
              />
            </Card>

            <Row gutter={[16, 16]}>
              <Col span={24} lg={12}>
                <MoversCard
                  title={t('上升最快')}
                  rows={snapshot.top_movers || []}
                  direction='up'
                  t={t}
                />
              </Col>
              <Col span={24} lg={12}>
                <MoversCard
                  title={t('下降最快')}
                  rows={snapshot.top_droppers || []}
                  direction='down'
                  t={t}
                />
              </Col>
            </Row>
          </div>
        ) : null}
      </Spin>
    </div>
  );
}
