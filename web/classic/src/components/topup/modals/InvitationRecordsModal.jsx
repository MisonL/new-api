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

import React, { useEffect, useMemo, useState } from 'react';
import {
  Modal,
  Table,
  Empty,
  Input,
  Select,
  Pagination,
  ButtonGroup,
  Button,
  Tag,
  Typography,
  Toast,
} from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { IconSearch } from '@douyinfe/semi-icons';
import { VChart } from '@visactor/react-vchart';
import {
  BarChart2,
  LayoutGrid,
  List,
  UserPlus,
  WalletCards,
} from 'lucide-react';
import { API, timestamp2string } from '../../../helpers';
import { useIsMobile } from '../../../hooks/common/useIsMobile';

const { Text } = Typography;

const RECORD_TYPE_OPTIONS = [
  { value: '', labelKey: '全部类型' },
  { value: 'register', labelKey: '注册奖励' },
  { value: 'topup_rebate', labelKey: '充值返利' },
];

const SOURCE_OPTIONS = [
  { value: '', labelKey: '全部来源' },
  { value: 'register', labelKey: '注册' },
  { value: 'stripe', labelKey: 'Stripe' },
  { value: 'creem', labelKey: 'Creem' },
  { value: 'waffo', labelKey: 'Waffo' },
  { value: 'admin', labelKey: '管理员补单' },
];

const InvitationRecordsModal = ({ visible, onCancel, t, renderQuota }) => {
  const [loading, setLoading] = useState(false);
  const [records, setRecords] = useState([]);
  const [inviteeStats, setInviteeStats] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [keyword, setKeyword] = useState('');
  const [recordType, setRecordType] = useState('');
  const [source, setSource] = useState('');
  const [viewMode, setViewMode] = useState('table');
  const isMobile = useIsMobile();
  const effectiveViewMode =
    isMobile && viewMode === 'table' ? 'cards' : viewMode;

  const buildQuery = (currentPage, currentPageSize) => {
    const params = new URLSearchParams({
      p: String(currentPage),
      page_size: String(currentPageSize),
    });
    if (keyword) {
      params.set('keyword', keyword);
    }
    if (recordType) {
      params.set('record_type', recordType);
    }
    if (source) {
      params.set('source', source);
    }
    return params.toString();
  };

  const loadRecords = async (currentPage, currentPageSize) => {
    setLoading(true);
    try {
      const res = await API.get(
        `/api/user/invitation_records?${buildQuery(currentPage, currentPageSize)}`,
      );
      const { success, message, data } = res.data;
      if (success) {
        setRecords(data.items || []);
        setInviteeStats(data.invitee_stats || []);
        setTotal(data.total || 0);
      } else {
        Toast.error({ content: message || t('加载失败') });
      }
    } catch (error) {
      Toast.error({ content: t('加载邀请记录失败') });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (visible) {
      loadRecords(page, pageSize);
    }
  }, [visible, page, pageSize, keyword, recordType, source]);

  const resetToFirstPage = (setter) => (value) => {
    setter(value || '');
    setPage(1);
  };

  const renderRecordType = (type) => {
    if (type === 'topup_rebate') {
      return (
        <Tag color='green' shape='circle' size='small'>
          {t('充值返利')}
        </Tag>
      );
    }
    return (
      <Tag color='blue' shape='circle' size='small'>
        {t('注册奖励')}
      </Tag>
    );
  };

  const renderSource = (value) => {
    const option = SOURCE_OPTIONS.find((item) => item.value === value);
    return option ? t(option.labelKey) : value || '-';
  };

  const renderTime = (value) => {
    return value > 0 ? timestamp2string(value) : '-';
  };

  const columns = useMemo(
    () => [
      {
        title: t('被邀请用户'),
        dataIndex: 'invitee_username',
        key: 'invitee_username',
        render: (text, record) => (
          <div className='flex flex-col'>
            <Text>{text || '-'}</Text>
            <Text type='tertiary' size='small'>
              {t('ID')}: {record.invitee_id}
            </Text>
          </div>
        ),
      },
      {
        title: t('类型'),
        dataIndex: 'record_type',
        key: 'record_type',
        render: renderRecordType,
      },
      {
        title: t('来源'),
        dataIndex: 'source',
        key: 'source',
        render: renderSource,
      },
      {
        title: t('充值额度'),
        dataIndex: 'topup_quota',
        key: 'topup_quota',
        render: (value) => (value > 0 ? renderQuota(value) : '-'),
      },
      {
        title: t('收益金额'),
        dataIndex: 'reward_quota',
        key: 'reward_quota',
        render: (value) => renderQuota(value || 0),
      },
      {
        title: t('返利比例'),
        dataIndex: 'rebate_rate',
        key: 'rebate_rate',
        render: (value, record) =>
          record.record_type === 'topup_rebate'
            ? `${Number(value || 0)}%`
            : '-',
      },
      {
        title: t('时间'),
        dataIndex: 'created_time',
        key: 'created_time',
        render: renderTime,
      },
    ],
    [t, renderQuota],
  );

  const summary = useMemo(() => {
    return records.reduce(
      (acc, record) => {
        acc.reward += Number(record.reward_quota || 0);
        if (record.record_type === 'register') {
          acc.register += 1;
        }
        if (record.record_type === 'topup_rebate') {
          acc.rebate += 1;
        }
        return acc;
      },
      { reward: 0, register: 0, rebate: 0 },
    );
  }, [records]);

  const normalizedInviteeStats = useMemo(
    () =>
      inviteeStats.map((item) => ({
        key: item.invitee_id || item.invitee_username,
        name: item.invitee_username || `${t('ID')}: ${item.invitee_id || '-'}`,
        reward: Number(item.reward_quota || 0),
        topup: Number(item.topup_quota || 0),
        registerCount: Number(item.register_count || 0),
        rebateCount: Number(item.rebate_count || 0),
        latestTime: Number(item.latest_time || 0),
      })),
    [inviteeStats, t],
  );

  const inviteeChartSpec = useMemo(
    () => ({
      type: 'bar',
      data: [
        {
          id: 'inviteeValueData',
          values: normalizedInviteeStats,
        },
      ],
      direction: 'horizontal',
      xField: 'reward',
      yField: 'name',
      seriesField: 'name',
      legends: { visible: false },
      bar: {
        state: {
          hover: {
            stroke: '#2563eb',
            lineWidth: 1,
          },
        },
      },
      label: {
        visible: true,
        position: 'outside',
        formatMethod: (_, datum) => renderQuota(datum.reward || 0),
      },
      axes: [
        {
          orient: 'left',
          type: 'band',
          label: {
            visible: true,
            formatMethod: (value) =>
              String(value).length > 14
                ? `${String(value).slice(0, 14)}...`
                : value,
          },
        },
        {
          orient: 'bottom',
          type: 'linear',
          label: {
            formatMethod: (value) => renderQuota(Number(value || 0), 2),
          },
        },
      ],
      tooltip: {
        mark: {
          content: [
            {
              key: t('收益金额'),
              value: (datum) => renderQuota(datum.reward || 0),
            },
            {
              key: t('充值额度'),
              value: (datum) => renderQuota(datum.topup || 0),
            },
            {
              key: t('充值返利记录'),
              value: (datum) => datum.rebateCount,
            },
            {
              key: t('注册奖励记录'),
              value: (datum) => datum.registerCount,
            },
          ],
        },
      },
      color: {
        type: 'ordinal',
        range: ['#1d4ed8', '#2563eb', '#3b82f6', '#60a5fa'],
      },
    }),
    [normalizedInviteeStats, renderQuota, t],
  );

  const renderCards = () => (
    <div className='grid grid-cols-1 md:grid-cols-2 gap-3'>
      {records.map((record) => (
        <div
          key={record.id}
          className='rounded-xl border border-semi-color-border p-4 bg-semi-color-bg-1'
        >
          <div className='flex items-start justify-between gap-3'>
            <div>
              <div className='font-medium'>
                {record.invitee_username || '-'}
              </div>
              <Text type='tertiary' size='small'>
                {t('ID')}: {record.invitee_id}
              </Text>
            </div>
            {renderRecordType(record.record_type)}
          </div>
          <div className='grid grid-cols-2 gap-3 mt-4 text-sm'>
            <div>
              <Text type='tertiary'>{t('收益金额')}</Text>
              <div className='font-medium mt-1'>
                {renderQuota(record.reward_quota || 0)}
              </div>
            </div>
            <div>
              <Text type='tertiary'>{t('返利比例')}</Text>
              <div className='font-medium mt-1'>
                {record.record_type === 'topup_rebate'
                  ? `${Number(record.rebate_rate || 0)}%`
                  : '-'}
              </div>
            </div>
            <div>
              <Text type='tertiary'>{t('来源')}</Text>
              <div className='font-medium mt-1'>
                {renderSource(record.source)}
              </div>
            </div>
            <div>
              <Text type='tertiary'>{t('时间')}</Text>
              <div className='font-medium mt-1'>
                {renderTime(record.created_time)}
              </div>
            </div>
          </div>
        </div>
      ))}
    </div>
  );

  const renderPagination = () => (
    <div className='flex justify-center pt-2'>
      <Pagination
        currentPage={page}
        pageSize={pageSize}
        total={total}
        showSizeChanger
        pageSizeOpts={[10, 20, 50, 100]}
        onPageChange={setPage}
        onPageSizeChange={(value) => {
          setPageSize(value);
          setPage(1);
        }}
        size={isMobile ? 'small' : 'default'}
      />
    </div>
  );

  const renderChart = () => {
    if (normalizedInviteeStats.length === 0) {
      return (
        <Empty
          image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
          darkModeImage={
            <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
          }
          description={t('当前筛选条件暂无可统计收益')}
          style={{ padding: 30 }}
        />
      );
    }

    return (
      <div className='grid grid-cols-1 xl:grid-cols-[minmax(0,1fr)_280px] gap-3'>
        <div className='rounded-xl border border-semi-color-border bg-semi-color-bg-1 p-3'>
          <div className='mb-3 flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between'>
            <Text strong>{t('高价值被邀请用户')}</Text>
            <Text type='tertiary' size='small'>
              {t('当前筛选条件')} ·{' '}
              {t('前 {{count}} 名', {
                count: normalizedInviteeStats.length,
              })}
            </Text>
          </div>
          <div className='h-[320px] min-w-0'>
            <VChart
              spec={inviteeChartSpec}
              option={{ mode: 'desktop-browser' }}
            />
          </div>
        </div>
        <div className='rounded-xl border border-semi-color-border bg-semi-color-bg-1 p-3'>
          <Text strong>{t('收益排行')}</Text>
          <div className='mt-3 space-y-3'>
            {normalizedInviteeStats.slice(0, 5).map((item, index) => (
              <div key={item.key} className='flex items-start gap-3'>
                <div className='flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-semi-color-fill-0 text-xs font-medium text-semi-color-tertiary'>
                  {index + 1}
                </div>
                <div className='min-w-0 flex-1'>
                  <div className='truncate font-medium'>{item.name}</div>
                  <Text type='tertiary' size='small'>
                    {t('充值返利记录')}: {item.rebateCount} ·{' '}
                    {t('注册奖励记录')}: {item.registerCount}
                  </Text>
                </div>
                <div className='shrink-0 font-medium'>
                  {renderQuota(item.reward)}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  };

  return (
    <Modal
      title={t('邀请记录')}
      visible={visible}
      onCancel={onCancel}
      footer={null}
      size={isMobile ? 'full-width' : 'large'}
      className='invitation-records-modal'
    >
      <div className='space-y-4'>
        <div className='grid grid-cols-1 md:grid-cols-3 gap-3'>
          <div className='rounded-xl border border-semi-color-border p-3 bg-semi-color-bg-1'>
            <div className='flex items-center gap-2 text-semi-color-tertiary text-xs'>
              <WalletCards size={14} />
              {t('本页收益')}
            </div>
            <div className='font-medium mt-2'>
              {renderQuota(summary.reward)}
            </div>
          </div>
          <div className='rounded-xl border border-semi-color-border p-3 bg-semi-color-bg-1'>
            <div className='flex items-center gap-2 text-semi-color-tertiary text-xs'>
              <UserPlus size={14} />
              {t('注册奖励记录')}
            </div>
            <div className='font-medium mt-2'>{summary.register}</div>
          </div>
          <div className='rounded-xl border border-semi-color-border p-3 bg-semi-color-bg-1'>
            <div className='flex items-center gap-2 text-semi-color-tertiary text-xs'>
              <BarChart2 size={14} />
              {t('充值返利记录')}
            </div>
            <div className='font-medium mt-2'>{summary.rebate}</div>
          </div>
        </div>

        <div className='flex flex-col lg:flex-row gap-2 lg:items-center lg:justify-between'>
          <div className='grid grid-cols-1 md:grid-cols-3 gap-2 flex-1'>
            <Input
              prefix={<IconSearch />}
              placeholder={t('搜索用户名、邮箱或用户 ID')}
              value={keyword}
              onChange={resetToFirstPage(setKeyword)}
              showClear
              name='components-topup-modals-invitationrecordsmodal-input-1'
            />
            <Select
              value={recordType}
              onChange={resetToFirstPage(setRecordType)}
              optionList={RECORD_TYPE_OPTIONS.map((item) => ({
                value: item.value,
                label: t(item.labelKey),
              }))}
            />
            <Select
              value={source}
              onChange={resetToFirstPage(setSource)}
              optionList={SOURCE_OPTIONS.map((item) => ({
                value: item.value,
                label: t(item.labelKey),
              }))}
            />
          </div>
          <div className='overflow-x-auto pb-1 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden'>
            <ButtonGroup className='min-w-max'>
              {!isMobile && (
                <Button
                  type={viewMode === 'table' ? 'primary' : 'tertiary'}
                  icon={<List size={14} />}
                  onClick={() => setViewMode('table')}
                >
                  {t('列表视图')}
                </Button>
              )}
              <Button
                type={effectiveViewMode === 'cards' ? 'primary' : 'tertiary'}
                icon={<LayoutGrid size={14} />}
                onClick={() => setViewMode('cards')}
              >
                {t('卡片视图')}
              </Button>
              <Button
                type={effectiveViewMode === 'chart' ? 'primary' : 'tertiary'}
                icon={<BarChart2 size={14} />}
                onClick={() => setViewMode('chart')}
              >
                {t('图表视图')}
              </Button>
            </ButtonGroup>
          </div>
        </div>

        {effectiveViewMode === 'chart' ? (
          <>
            {renderChart()}
            {total > 0 && renderPagination()}
          </>
        ) : effectiveViewMode === 'cards' ? (
          records.length > 0 ? (
            <>
              {renderCards()}
              {renderPagination()}
            </>
          ) : (
            <Empty
              image={
                <IllustrationNoResult style={{ width: 150, height: 150 }} />
              }
              darkModeImage={
                <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
              }
              description={t('暂无邀请记录')}
              style={{ padding: 30 }}
            />
          )
        ) : (
          <Table
            columns={columns}
            dataSource={records}
            loading={loading}
            rowKey='id'
            pagination={{
              currentPage: page,
              pageSize,
              total,
              showSizeChanger: true,
              pageSizeOpts: [10, 20, 50, 100],
              onPageChange: setPage,
              onPageSizeChange: (value) => {
                setPageSize(value);
                setPage(1);
              },
            }}
            size='small'
            empty={
              <Empty
                image={
                  <IllustrationNoResult style={{ width: 150, height: 150 }} />
                }
                darkModeImage={
                  <IllustrationNoResultDark
                    style={{ width: 150, height: 150 }}
                  />
                }
                description={t('暂无邀请记录')}
                style={{ padding: 30 }}
              />
            }
          />
        )}
      </div>
    </Modal>
  );
};

export default InvitationRecordsModal;
