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
import { StatusContext } from '../../context/Status';
import {
  DASHBOARD_LOG_PAGE_SIZE,
  DASHBOARD_LOG_TYPES,
  buildDashboardLogInitialFilters,
  normalizeDashboardLogFilters,
} from '../../helpers/dashboardLogs';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import FilterAutoComplete from '../common/ui/FilterAutoComplete';
import { getDashboardLogColumns } from './dashboardLogColumns';

const DashboardLogsModal = ({
  visible,
  scope,
  fallbackRange,
  isAdminUser,
  onClose,
  t,
}) => {
  const [statusState] = React.useContext(StatusContext);
  const autocompleteEnabled = statusState?.status
    ? (statusState.status.log_filter_autocomplete_enabled ?? true)
    : false;
  const isMobile = useIsMobile();
  const tableScrollY = isMobile
    ? 'clamp(180px, calc(100dvh - 600px), 280px)'
    : 'clamp(240px, calc(100dvh - 480px), 360px)';
  const [formApi, setFormApi] = useState(null);
  const [logs, setLogs] = useState([]);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(DASHBOARD_LOG_PAGE_SIZE);
  const [total, setTotal] = useState(0);
  const fastPage = scope?.fast_page === true;
  const requestSeqRef = useRef(0);
  const autoLoadKeyRef = useRef('');
  const autoLoadTimerRef = useRef(null);

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
  const modalViewportGap = 'clamp(32px, 6vw, 96px)';
  const modalBodyStyle = {
    padding: '10px 14px 14px',
    maxHeight: `calc(100dvh - ${modalViewportGap} - 64px)`,
    overflow: 'hidden',
  };
  const suggestionEndpoint = isAdminUser
    ? '/api/log/suggestions'
    : '/api/log/self/suggestions';
  const buildSuggestionParams = () =>
    normalizeDashboardLogFilters(
      formApi ? formApi.getValues() : initialFilters,
    );

  const cancelScheduledAutoLoad = useCallback(() => {
    if (autoLoadTimerRef.current == null) {
      return;
    }
    window.clearTimeout(autoLoadTimerRef.current);
    autoLoadTimerRef.current = null;
  }, []);

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
      cancelScheduledAutoLoad();
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
    cancelScheduledAutoLoad();
    autoLoadTimerRef.current = window.setTimeout(() => {
      autoLoadTimerRef.current = null;
      loadLogs(1, DASHBOARD_LOG_PAGE_SIZE, initialFilters);
    }, 0);
    return cancelScheduledAutoLoad;
  }, [
    cancelScheduledAutoLoad,
    formApi,
    initialFilters,
    initialFiltersKey,
    loadLogs,
    visible,
  ]);

  const handleSearch = () => {
    cancelScheduledAutoLoad();
    setPage(1);
    loadLogs(1, pageSize);
  };

  const handleReset = () => {
    cancelScheduledAutoLoad();
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
      width={`min(1180px, calc(100vw - ${modalViewportGap}))`}
      centered
      bodyStyle={modalBodyStyle}
      closeOnEsc
    >
      <div className='flex max-h-[calc(100dvh-clamp(144px,14vw,200px))] min-h-0 flex-col gap-2'>
        <Form
          layout='vertical'
          initValues={initialFilters}
          getFormApi={setFormApi}
          onSubmit={handleSearch}
          autoComplete='off'
        >
          <div className='max-h-[34dvh] overflow-y-auto rounded-lg border border-semi-color-border bg-semi-color-fill-0 p-2 md:max-h-none md:overflow-visible'>
            <div className='grid grid-cols-1 gap-x-2 gap-y-1 md:grid-cols-4 xl:grid-cols-9'>
              <div className='md:col-span-2 xl:col-span-3'>
                <Form.DatePicker
                  field='dateRange'
                  type='dateTimeRange'
                  className='w-full'
                  placeholder={[t('开始时间'), t('结束时间')]}
                  showClear
                  pure
                  size='small'
                />
              </div>
              <FilterAutoComplete
                field='model_name'
                endpoint={suggestionEndpoint}
                buildParams={buildSuggestionParams}
                enableSuggestions={autocompleteEnabled}
                prefix={<IconSearch />}
                placeholder={t('模型名称')}
              />
              <FilterAutoComplete
                field='token_name'
                endpoint={suggestionEndpoint}
                buildParams={buildSuggestionParams}
                enableSuggestions={autocompleteEnabled}
                prefix={<IconSearch />}
                placeholder={t('令牌名称')}
              />
              <FilterAutoComplete
                field='group'
                endpoint={suggestionEndpoint}
                buildParams={buildSuggestionParams}
                enableSuggestions={autocompleteEnabled}
                prefix={<IconSearch />}
                placeholder={t('分组')}
              />
              <FilterAutoComplete
                field='request_id'
                endpoint={suggestionEndpoint}
                buildParams={buildSuggestionParams}
                enableSuggestions={autocompleteEnabled}
                prefix={<IconSearch />}
                placeholder={t('请求 ID')}
                minLength={1}
              />
              {isAdminUser ? (
                <>
                  <FilterAutoComplete
                    field='channel'
                    endpoint={suggestionEndpoint}
                    buildParams={buildSuggestionParams}
                    enableSuggestions={autocompleteEnabled}
                    prefix={<IconSearch />}
                    placeholder={t('渠道 ID')}
                    minLength={1}
                  />
                  <FilterAutoComplete
                    field='username'
                    endpoint={suggestionEndpoint}
                    buildParams={buildSuggestionParams}
                    enableSuggestions={autocompleteEnabled}
                    prefix={<IconSearch />}
                    placeholder={t('用户名称')}
                    minLength={1}
                  />
                </>
              ) : null}
            </div>
            <div className='mt-1.5 flex flex-col gap-2 lg:flex-row lg:items-end lg:justify-between'>
              <Form.RadioGroup
                field='logType'
                label={t('日志类型')}
                type='button'
                className='m-0 min-w-0 flex-1'
              >
                {DASHBOARD_LOG_TYPES.map((item) => (
                  <Radio key={item.value} value={item.value}>
                    {t(item.label)}
                  </Radio>
                ))}
              </Form.RadioGroup>
              <div className='flex justify-end lg:shrink-0'>
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
                    htmlType='button'
                    onClick={() => {
                      cancelScheduledAutoLoad();
                      loadLogs(page, pageSize);
                    }}
                    loading={loading}
                    size='small'
                    icon={<IconRefresh />}
                  >
                    {t('刷新')}
                  </Button>
                  <Button
                    type='tertiary'
                    htmlType='button'
                    onClick={handleReset}
                    size='small'
                  >
                    {t('重置')}
                  </Button>
                </Space>
              </div>
            </div>
          </div>
        </Form>
        <div className='min-h-0 flex-1 overflow-hidden rounded-lg border border-semi-color-border bg-semi-color-bg-0'>
          <Table
            columns={columns}
            dataSource={logs}
            rowKey='id'
            loading={loading}
            size='small'
            scroll={{
              x: 'max-content',
              y: tableScrollY,
            }}
            pagination={{
              currentPage: page,
              pageSize,
              total,
              pageSizeOptions: [10, 20, 50, 100],
              showSizeChanger: !isMobile,
              showTotal: fastPage ? false : undefined,
              showQuickJumper: true,
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
