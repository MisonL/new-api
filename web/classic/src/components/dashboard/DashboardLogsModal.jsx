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

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { Button, Form, Modal, Radio, Space, Table } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { API, showError } from '../../helpers';
import {
  DASHBOARD_LOG_PAGE_SIZE,
  DASHBOARD_LOG_TYPES,
  buildDashboardLogInitialFilters,
  normalizeDashboardLogFilters,
} from '../../helpers/dashboardLogs';
import { getDashboardLogColumns } from './dashboardLogColumns';

const DashboardLogsModal = ({
  visible,
  scope,
  fallbackRange,
  isAdminUser,
  onClose,
  t,
}) => {
  const [formApi, setFormApi] = useState(null);
  const [logs, setLogs] = useState([]);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(DASHBOARD_LOG_PAGE_SIZE);
  const [total, setTotal] = useState(0);
  const requestSeqRef = useRef(0);
  const autoLoadKeyRef = useRef('');

  const initialFilters = useMemo(
    () => buildDashboardLogInitialFilters(scope, fallbackRange),
    [fallbackRange, scope],
  );
  const initialFiltersKey = useMemo(
    () => JSON.stringify(normalizeDashboardLogFilters(initialFilters)),
    [initialFilters],
  );

  const columns = useMemo(
    () => getDashboardLogColumns({ isAdminUser, t }),
    [isAdminUser, t],
  );

  const loadLogs = useCallback(
    async (nextPage, nextPageSize, filterValues) => {
      if (!formApi && !filterValues) {
        return;
      }
      const filters = normalizeDashboardLogFilters(
        filterValues || formApi.getValues(),
      );
      const requestSeq = requestSeqRef.current + 1;
      requestSeqRef.current = requestSeq;
      setLoading(true);
      try {
        const endpoint = isAdminUser ? '/api/log/' : '/api/log/self/';
        const res = await API.get(endpoint, {
          params: {
            p: nextPage,
            page_size: nextPageSize,
            ...filters,
          },
          skipErrorHandler: true,
          disableDuplicate: true,
        });
        const { success, message, data } = res.data;
        if (!success) {
          showError(message);
          return;
        }
        if (requestSeq !== requestSeqRef.current) {
          return;
        }
        setLogs(data?.items || []);
        setPage(data?.page || nextPage);
        setPageSize(data?.page_size || nextPageSize);
        setTotal(data?.total || 0);
      } catch (error) {
        showError(error?.message || t('查询日志失败'));
      } finally {
        if (requestSeq === requestSeqRef.current) {
          setLoading(false);
        }
      }
    },
    [formApi, isAdminUser, t],
  );

  useEffect(() => {
    if (!visible) {
      autoLoadKeyRef.current = '';
      return;
    }
    if (!visible || !formApi) {
      return;
    }
    formApi.setValues(initialFilters);
    if (autoLoadKeyRef.current === initialFiltersKey) {
      return;
    }
    autoLoadKeyRef.current = initialFiltersKey;
    setLogs([]);
    setTotal(0);
    setPage(1);
    setPageSize(DASHBOARD_LOG_PAGE_SIZE);
    loadLogs(1, DASHBOARD_LOG_PAGE_SIZE, initialFilters);
  }, [formApi, initialFilters, initialFiltersKey, loadLogs, visible]);

  const handleSearch = () => {
    setPage(1);
    loadLogs(1, pageSize);
  };

  const handleReset = () => {
    const nextFilters = buildDashboardLogInitialFilters(scope, fallbackRange);
    formApi?.setValues(nextFilters);
    setPage(1);
    loadLogs(1, pageSize, nextFilters);
  };

  return (
    <Modal
      title={scope?.title || t('相关日志')}
      visible={visible}
      onCancel={onClose}
      footer={null}
      width={1180}
      closeOnEsc
    >
      <div className='flex flex-col gap-4'>
        <Form
          layout='vertical'
          initValues={initialFilters}
          getFormApi={setFormApi}
          onSubmit={handleSearch}
          autoComplete='off'
        >
          <div className='rounded-lg border border-semi-color-border bg-semi-color-fill-0 p-3'>
            <div className='grid grid-cols-1 gap-x-3 gap-y-2 md:grid-cols-2 xl:grid-cols-4'>
              <div className='md:col-span-2'>
                <Form.DatePicker
                  field='dateRange'
                  label={t('时间范围')}
                  type='dateTimeRange'
                  className='w-full'
                  placeholder={[t('开始时间'), t('结束时间')]}
                  size='small'
                />
              </div>
              <Form.Input
                field='model_name'
                label={t('模型名称')}
                prefix={<IconSearch />}
                placeholder={t('模型名称')}
                size='small'
              />
              <Form.Input
                field='token_name'
                label={t('令牌名称')}
                prefix={<IconSearch />}
                placeholder={t('令牌名称')}
                size='small'
              />
              <Form.Input
                field='group'
                label={t('分组')}
                prefix={<IconSearch />}
                placeholder={t('分组')}
                size='small'
              />
              <Form.Input
                field='request_id'
                label={t('请求 ID')}
                prefix={<IconSearch />}
                placeholder={t('请求 ID')}
                size='small'
              />
              {isAdminUser ? (
                <>
                  <Form.Input
                    field='channel'
                    label={t('渠道 ID')}
                    prefix={<IconSearch />}
                    placeholder={t('渠道 ID')}
                    size='small'
                  />
                  <Form.Input
                    field='username'
                    label={t('用户名称')}
                    prefix={<IconSearch />}
                    placeholder={t('用户名称')}
                    size='small'
                  />
                </>
              ) : null}
            </div>
            <Form.RadioGroup
              field='logType'
              label={t('日志类型')}
              type='button'
              className='mt-3'
            >
              {DASHBOARD_LOG_TYPES.map((item) => (
                <Radio key={item.value} value={item.value}>
                  {t(item.label)}
                </Radio>
              ))}
            </Form.RadioGroup>
            <div className='mt-3 flex justify-end'>
              <Space>
                <Button
                  type='primary'
                  htmlType='submit'
                  loading={loading}
                  size='small'
                  icon={<IconSearch />}
                >
                  {t('查询')}
                </Button>
                <Button
                  type='tertiary'
                  onClick={() => loadLogs(page, pageSize)}
                  loading={loading}
                  size='small'
                  icon={<IconRefresh />}
                >
                  {t('刷新')}
                </Button>
                <Button type='tertiary' onClick={handleReset} size='small'>
                  {t('重置')}
                </Button>
              </Space>
            </div>
          </div>
        </Form>
        <div className='overflow-hidden rounded-lg border border-semi-color-border bg-semi-color-bg-0'>
          <Table
            columns={columns}
            dataSource={logs}
            rowKey='id'
            loading={loading}
            size='small'
            scroll={{ x: 'max-content', y: 420 }}
            pagination={{
              currentPage: page,
              pageSize,
              total,
              pageSizeOptions: [10, 20, 50, 100],
              showSizeChanger: true,
              onPageChange: (nextPage) => loadLogs(nextPage, pageSize),
              onPageSizeChange: (nextPageSize) => {
                setPageSize(nextPageSize);
                loadLogs(1, nextPageSize);
              },
            }}
          />
        </div>
      </div>
    </Modal>
  );
};

export default DashboardLogsModal;
