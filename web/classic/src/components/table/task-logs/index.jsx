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
import { Layout } from '@douyinfe/semi-ui';
import CardPro from '../../common/ui/CardPro';
import TaskLogsTable from './TaskLogsTable';
import TaskLogsActions from './TaskLogsActions';
import TaskLogsFilters from './TaskLogsFilters';
import { useTaskLogsData } from '../../../hooks/task-logs/useTaskLogsData';
import { useIsMobile } from '../../../hooks/common/useIsMobile';
import { createCardProPagination } from '../../../helpers/utils';

const ColumnSelectorModal = lazy(() => import('./modals/ColumnSelectorModal'));
const ContentModal = lazy(() => import('./modals/ContentModal'));
const AudioPreviewModal = lazy(() => import('./modals/AudioPreviewModal'));
const EditUserModal = lazy(() => import('../users/modals/EditUserModal'));

const TaskLogsPage = () => {
  const taskLogsData = useTaskLogsData();
  const isMobile = useIsMobile();
  const refreshCurrentPage = () =>
    taskLogsData.refresh(taskLogsData.activePage);

  return (
    <>
      {/* Modals */}
      {taskLogsData.showColumnSelector ? (
        <Suspense fallback={null}>
          <ColumnSelectorModal {...taskLogsData} />
        </Suspense>
      ) : null}
      {taskLogsData.isModalOpen ? (
        <Suspense fallback={null}>
          <ContentModal {...taskLogsData} isVideo={false} />
        </Suspense>
      ) : null}
      {/* 新增：视频预览弹窗 */}
      {taskLogsData.isVideoModalOpen ? (
        <Suspense fallback={null}>
          <ContentModal
            isModalOpen={taskLogsData.isVideoModalOpen}
            setIsModalOpen={taskLogsData.setIsVideoModalOpen}
            modalContent={taskLogsData.videoUrl}
            isVideo={true}
          />
        </Suspense>
      ) : null}
      {taskLogsData.isAudioModalOpen ? (
        <Suspense fallback={null}>
          <AudioPreviewModal
            isModalOpen={taskLogsData.isAudioModalOpen}
            setIsModalOpen={taskLogsData.setIsAudioModalOpen}
            audioClips={taskLogsData.audioClips}
          />
        </Suspense>
      ) : null}
      {taskLogsData.showEditUser ? (
        <Suspense fallback={null}>
          <EditUserModal
            refresh={refreshCurrentPage}
            visible={taskLogsData.showEditUser}
            handleClose={taskLogsData.closeEditUserPanel}
            editingUser={taskLogsData.editingUser}
          />
        </Suspense>
      ) : null}

      <Layout>
        <CardPro
          type='type2'
          statsArea={<TaskLogsActions {...taskLogsData} />}
          searchArea={<TaskLogsFilters {...taskLogsData} />}
          paginationArea={createCardProPagination({
            currentPage: taskLogsData.activePage,
            pageSize: taskLogsData.pageSize,
            total: taskLogsData.logCount,
            onPageChange: taskLogsData.handlePageChange,
            onPageSizeChange: taskLogsData.handlePageSizeChange,
            isMobile: isMobile,
            t: taskLogsData.t,
          })}
          t={taskLogsData.t}
        >
          <TaskLogsTable {...taskLogsData} />
        </CardPro>
      </Layout>
    </>
  );
};

export default TaskLogsPage;
