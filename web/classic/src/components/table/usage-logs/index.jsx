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

import React, { lazy, Suspense } from 'react';
import CardPro from '../../common/ui/CardPro';
import LogsTable from './UsageLogsTable';
import LogsActions from './UsageLogsActions';
import LogsFilters from './UsageLogsFilters';
import { useLogsData } from '../../../hooks/usage-logs/useUsageLogsData';
import { useIsMobile } from '../../../hooks/common/useIsMobile';
import { createCardProPagination } from '../../../helpers/utils';

const ColumnSelectorModal = lazy(() => import('./modals/ColumnSelectorModal'));
const ChannelAffinityUsageCacheModal = lazy(
  () => import('./modals/ChannelAffinityUsageCacheModal'),
);
const ParamOverrideModal = lazy(() => import('./modals/ParamOverrideModal'));
const PayloadContentModal = lazy(() => import('./modals/PayloadContentModal'));
const EditUserModal = lazy(() => import('../users/modals/EditUserModal'));

const LogsPage = () => {
  const logsData = useLogsData();
  const isMobile = useIsMobile();
  const refreshCurrentPage = () =>
    logsData.refresh(logsData.activePage, { refreshStats: false });

  return (
    <>
      {/* Modals */}
      {logsData.showColumnSelector ? (
        <Suspense fallback={null}>
          <ColumnSelectorModal {...logsData} />
        </Suspense>
      ) : null}
      {logsData.showEditUser ? (
        <Suspense fallback={null}>
          <EditUserModal
            refresh={refreshCurrentPage}
            visible={logsData.showEditUser}
            handleClose={logsData.closeEditUserPanel}
            editingUser={logsData.editingUser}
          />
        </Suspense>
      ) : null}
      {logsData.showChannelAffinityUsageCacheModal ? (
        <Suspense fallback={null}>
          <ChannelAffinityUsageCacheModal {...logsData} />
        </Suspense>
      ) : null}
      {logsData.showParamOverrideModal ? (
        <Suspense fallback={null}>
          <ParamOverrideModal {...logsData} />
        </Suspense>
      ) : null}
      {logsData.showPayloadContentModal ? (
        <Suspense fallback={null}>
          <PayloadContentModal {...logsData} />
        </Suspense>
      ) : null}

      {/* Main Content */}
      <CardPro
        type='type2'
        className='usage-logs-card'
        statsArea={<LogsActions {...logsData} />}
        searchArea={<LogsFilters {...logsData} />}
        paginationArea={createCardProPagination({
          currentPage: logsData.activePage,
          pageSize: logsData.pageSize,
          total: logsData.logCount,
          onPageChange: logsData.handlePageChange,
          onPageSizeChange: logsData.handlePageSizeChange,
          isMobile: isMobile,
          t: logsData.t,
        })}
        t={logsData.t}
      >
        <LogsTable {...logsData} />
      </CardPro>
    </>
  );
};

export default LogsPage;
