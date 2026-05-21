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

import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Table,
  Card,
  Skeleton,
  Pagination,
  Empty,
  Button,
  Collapsible,
} from '@douyinfe/semi-ui';
import { IconChevronDown, IconChevronUp } from '@douyinfe/semi-icons';
import PropTypes from 'prop-types';
import { useIsMobile } from '../../../hooks/common/useIsMobile';
import { useMinimumLoadingTime } from '../../../hooks/common/useMinimumLoadingTime';

/**
 * CardTable 响应式表格组件
 *
 * 在桌面端渲染 Semi-UI 的 Table 组件，在移动端则将每一行数据渲染成 Card 形式。
 * 该组件与 Table 组件的大部分 API 保持一致，只需将原 Table 换成 CardTable 即可。
 */
const CardTable = ({
  columns = [],
  dataSource = [],
  loading = false,
  rowKey = 'key',
  hidePagination = false,
  ...tableProps
}) => {
  const isMobile = useIsMobile();
  const { t } = useTranslation();

  const showSkeleton = useMinimumLoadingTime(loading);
  const pagination = normalizePagination(tableProps.pagination);

  const getRowKey = (record, index) => {
    if (typeof rowKey === 'function') return rowKey(record);
    return record[rowKey] !== undefined ? record[rowKey] : index;
  };

  if (!isMobile) {
    const normalizedTableProps = { ...tableProps, pagination };
    const finalTableProps = hidePagination
      ? { ...tableProps, pagination: false }
      : normalizedTableProps;
    const horizontalScrollWidth = finalTableProps?.scroll?.x;
    const hasHorizontalScroll = Boolean(horizontalScrollWidth);
    const hasFixedColumns = columns.some((column) => Boolean(column?.fixed));
    const {
      className: desktopTableClassName,
      style: desktopTableStyle,
      id: desktopTableId,
      ...tableOnlyProps
    } = finalTableProps;
    const tableNode = (
      <Table
        columns={columns}
        dataSource={dataSource}
        loading={loading}
        rowKey={rowKey}
        id={desktopTableId}
        className={desktopTableClassName}
        style={desktopTableStyle}
        {...(hasHorizontalScroll ? tableOnlyProps : finalTableProps)}
      />
    );

    if (!hasHorizontalScroll) {
      return tableNode;
    }

    return (
      <DesktopTableScrollProxy
        syncKey={`${columns.length}:${dataSource.length}:${loading}:${hasFixedColumns}`}
      >
        {tableNode}
      </DesktopTableScrollProxy>
    );
  }

  if (showSkeleton) {
    const visibleCols = columns.filter((col) => {
      if (tableProps?.visibleColumns && col.key) {
        return tableProps.visibleColumns[col.key];
      }
      return true;
    });

    const renderSkeletonCard = (key) => {
      const placeholder = (
        <div className='p-2'>
          {visibleCols.map((col, idx) => {
            if (!col.title) {
              return (
                <div key={idx} className='mt-2 flex justify-end'>
                  <Skeleton.Title style={{ width: 100, height: 24 }} />
                </div>
              );
            }

            return (
              <div
                key={idx}
                className='flex justify-between items-center py-1 border-b last:border-b-0 border-dashed'
                style={{ borderColor: 'var(--semi-color-border)' }}
              >
                <Skeleton.Title style={{ width: 80, height: 14 }} />
                <Skeleton.Title
                  style={{
                    width: `${50 + (idx % 3) * 10}%`,
                    maxWidth: 180,
                    height: 14,
                  }}
                />
              </div>
            );
          })}
        </div>
      );

      return (
        <Card key={key} className='!rounded-2xl shadow-sm'>
          <Skeleton loading={true} active placeholder={placeholder}></Skeleton>
        </Card>
      );
    };

    return (
      <div className='flex flex-col gap-2'>
        {[1, 2, 3].map((i) => renderSkeletonCard(i))}
      </div>
    );
  }

  const isEmpty = !showSkeleton && (!dataSource || dataSource.length === 0);

  const MobileRowCard = ({ record, index }) => {
    const [showDetails, setShowDetails] = useState(false);
    const [hasOpenedDetails, setHasOpenedDetails] = useState(false);
    const rowKeyVal = getRowKey(record, index);

    const hasDetails =
      tableProps.expandedRowRender &&
      (!tableProps.rowExpandable || tableProps.rowExpandable(record));

    return (
      <Card key={rowKeyVal} className='!rounded-2xl shadow-sm'>
        {columns.map((col, colIdx) => {
          if (
            tableProps?.visibleColumns &&
            !tableProps.visibleColumns[col.key]
          ) {
            return null;
          }

          const title = col.title;
          const cellContent = col.render
            ? col.render(record[col.dataIndex], record, index)
            : record[col.dataIndex];

          if (!title) {
            return (
              <div key={col.key || colIdx} className='mt-2 flex justify-end'>
                {cellContent}
              </div>
            );
          }

          return (
            <div
              key={col.key || colIdx}
              className='flex justify-between items-start py-1 border-b last:border-b-0 border-dashed'
              style={{ borderColor: 'var(--semi-color-border)' }}
            >
              <span className='font-medium text-gray-600 mr-2 whitespace-nowrap select-none'>
                {title}
              </span>
              <div className='flex-1 break-all flex justify-end items-center gap-1'>
                {cellContent !== undefined && cellContent !== null
                  ? cellContent
                  : '-'}
              </div>
            </div>
          );
        })}

        {hasDetails && (
          <>
            <Button
              theme='borderless'
              size='small'
              className='w-full flex justify-center mt-2'
              icon={showDetails ? <IconChevronUp /> : <IconChevronDown />}
              onClick={(e) => {
                e.stopPropagation();
                setHasOpenedDetails(true);
                setShowDetails(!showDetails);
              }}
            >
              {showDetails ? t('收起') : t('详情')}
            </Button>
            <Collapsible isOpen={showDetails} keepDOM>
              <div className='pt-2'>
                {hasOpenedDetails
                  ? tableProps.expandedRowRender(record, index)
                  : null}
              </div>
            </Collapsible>
          </>
        )}
      </Card>
    );
  };

  if (isEmpty) {
    if (tableProps.empty) return tableProps.empty;
    return (
      <div className='flex justify-center p-4'>
        <Empty description='No Data' />
      </div>
    );
  }

  return (
    <div className='flex flex-col gap-2'>
      {dataSource.map((record, index) => (
        <MobileRowCard
          key={getRowKey(record, index)}
          record={record}
          index={index}
        />
      ))}
      {!hidePagination && pagination && dataSource.length > 0 && (
        <div className='mt-2 flex justify-center'>
          <Pagination {...pagination} />
        </div>
      )}
    </div>
  );
};

function getDesktopTableBody(container) {
  return container?.querySelector('.semi-table-body') || null;
}

function createTableScrollSync(tableBody, updateScrollLeft) {
  const syncFromTable = () => updateScrollLeft(tableBody.scrollLeft);

  tableBody.addEventListener('scroll', syncFromTable, { passive: true });

  return () => {
    tableBody.removeEventListener('scroll', syncFromTable);
  };
}

function createTableMetricObserver(elements, updateMetrics) {
  const resizeObserver = new ResizeObserver(updateMetrics);
  const mutationObserver = new MutationObserver(updateMetrics);
  elements.filter(Boolean).forEach((element) => {
    resizeObserver.observe(element);
  });
  const tableBody = elements[0];
  mutationObserver.observe(tableBody, { childList: true, subtree: true });
  const animationFrame = requestAnimationFrame(updateMetrics);

  return () => {
    cancelAnimationFrame(animationFrame);
    resizeObserver.disconnect();
    mutationObserver.disconnect();
  };
}

function useDesktopTableScrollMetrics(containerRef, syncKey) {
  const [metrics, setMetrics] = useState({
    clientWidth: 0,
    scrollWidth: 0,
    scrollLeft: 0,
    trackWidth: 0,
  });

  useEffect(() => {
    const container = containerRef.current;
    const tableBody = getDesktopTableBody(container);
    if (!tableBody) return undefined;
    const cardBody = container?.closest('.semi-card-body');
    const track = container?.querySelector('.card-table-scrollbar');

    const updateMetrics = () => {
      if (container && cardBody && track) {
        const cardBodyRect = cardBody.getBoundingClientRect();
        const containerRect = container.getBoundingClientRect();
        const trackStyle = getComputedStyle(track);
        const trackOuterHeight =
          track.offsetHeight + parseFloat(trackStyle.marginTop || '0');
        const availableHeight =
          cardBody.clientHeight -
          Math.max(containerRect.top - cardBodyRect.top, 0) -
          trackOuterHeight;
        if (availableHeight > 0) {
          container.style.setProperty(
            '--card-table-body-max-height',
            `${Math.floor(availableHeight)}px`,
          );
        } else {
          container.style.removeProperty('--card-table-body-max-height');
        }
      }
      setMetrics({
        clientWidth: tableBody.clientWidth,
        scrollWidth: tableBody.scrollWidth,
        scrollLeft: tableBody.scrollLeft,
        trackWidth: Math.max(
          (track?.clientWidth || tableBody.clientWidth) - 8,
          0,
        ),
      });
    };
    const updateScrollLeft = (scrollLeft) => {
      setMetrics((current) => ({ ...current, scrollLeft }));
    };
    const cleanupSync = createTableScrollSync(tableBody, updateScrollLeft);
    const cleanupObserver = createTableMetricObserver(
      [tableBody, container, cardBody, track],
      updateMetrics,
    );

    return () => {
      cleanupObserver();
      cleanupSync();
      container?.style.removeProperty('--card-table-body-max-height');
    };
  }, [syncKey]);

  return metrics;
}

const DesktopTableScrollProxy = ({ children, syncKey }) => {
  const containerRef = useRef(null);
  const trackRef = useRef(null);
  const dragStateRef = useRef(null);
  const metrics = useDesktopTableScrollMetrics(containerRef, syncKey);
  const showScrollbar = metrics.scrollWidth > metrics.clientWidth + 1;
  const scrollableWidth = Math.max(
    metrics.scrollWidth - metrics.clientWidth,
    0,
  );
  const thumbWidth =
    showScrollbar && metrics.trackWidth > 0
      ? Math.max(
          44,
          (metrics.clientWidth / metrics.scrollWidth) * metrics.trackWidth,
        )
      : metrics.trackWidth;
  const maxThumbLeft = Math.max(metrics.trackWidth - thumbWidth, 0);
  const thumbLeft =
    showScrollbar && scrollableWidth > 0
      ? Math.min(
          (metrics.scrollLeft / scrollableWidth) * maxThumbLeft,
          maxThumbLeft,
        )
      : 0;

  const setTableScrollLeft = useCallback((nextScrollLeft) => {
    const tableBody = getDesktopTableBody(containerRef.current);
    if (!tableBody) return;
    tableBody.scrollLeft = Math.max(
      0,
      Math.min(nextScrollLeft, tableBody.scrollWidth - tableBody.clientWidth),
    );
  }, []);

  const getTableScrollLeft = useCallback(() => {
    const tableBody = getDesktopTableBody(containerRef.current);
    return tableBody?.scrollLeft ?? metrics.scrollLeft;
  }, [metrics.scrollLeft]);

  const scrollToPointer = useCallback(
    (clientX) => {
      const track = trackRef.current;
      if (!track || !showScrollbar || maxThumbLeft <= 0) return;
      const rect = track.getBoundingClientRect();
      const trackWidth = Math.max(rect.width - 8, 0);
      const maxThumbPixelLeft = trackWidth - thumbWidth;
      if (maxThumbPixelLeft <= 0) return;
      const nextThumbLeft = Math.max(
        0,
        Math.min(clientX - rect.left - 4 - thumbWidth / 2, maxThumbPixelLeft),
      );
      setTableScrollLeft((nextThumbLeft / maxThumbPixelLeft) * scrollableWidth);
    },
    [
      maxThumbLeft,
      scrollableWidth,
      setTableScrollLeft,
      showScrollbar,
      thumbWidth,
    ],
  );

  const handleTrackPointerDown = useCallback(
    (event) => {
      scrollToPointer(event.clientX);
    },
    [scrollToPointer],
  );

  const handleThumbPointerDown = useCallback(
    (event) => {
      if (!showScrollbar) return;
      event.preventDefault();
      event.stopPropagation();
      const track = trackRef.current;
      if (!track) return;
      dragStateRef.current = {
        startX: event.clientX,
        startScrollLeft: getTableScrollLeft(),
        trackWidth: Math.max(track.getBoundingClientRect().width - 8, 0),
      };
      event.currentTarget.setPointerCapture?.(event.pointerId);
    },
    [getTableScrollLeft, showScrollbar],
  );

  const handleThumbPointerMove = useCallback(
    (event) => {
      const dragState = dragStateRef.current;
      if (!dragState || !showScrollbar || maxThumbLeft <= 0) return;
      const maxThumbPixelLeft = dragState.trackWidth - thumbWidth;
      if (maxThumbPixelLeft <= 0) return;
      const deltaX = event.clientX - dragState.startX;
      setTableScrollLeft(
        dragState.startScrollLeft +
          (deltaX / maxThumbPixelLeft) * scrollableWidth,
      );
    },
    [
      maxThumbLeft,
      scrollableWidth,
      setTableScrollLeft,
      showScrollbar,
      thumbWidth,
    ],
  );

  const handleThumbPointerEnd = useCallback((event) => {
    dragStateRef.current = null;
    if (event.currentTarget.hasPointerCapture?.(event.pointerId)) {
      event.currentTarget.releasePointerCapture?.(event.pointerId);
    }
  }, []);

  const handleScrollbarKeyDown = useCallback(
    (event) => {
      if (!showScrollbar) return;
      const currentScrollLeft = getTableScrollLeft();
      const smallStep = Math.max(metrics.clientWidth * 0.1, 40);
      const pageStep = Math.max(metrics.clientWidth * 0.8, 40);
      let nextScrollLeft = currentScrollLeft;

      switch (event.key) {
        case 'ArrowLeft':
          nextScrollLeft -= smallStep;
          break;
        case 'ArrowRight':
          nextScrollLeft += smallStep;
          break;
        case 'PageUp':
          nextScrollLeft -= pageStep;
          break;
        case 'PageDown':
          nextScrollLeft += pageStep;
          break;
        case 'Home':
          nextScrollLeft = 0;
          break;
        case 'End':
          nextScrollLeft = scrollableWidth;
          break;
        default:
          return;
      }

      event.preventDefault();
      setTableScrollLeft(nextScrollLeft);
    },
    [
      getTableScrollLeft,
      metrics.clientWidth,
      scrollableWidth,
      setTableScrollLeft,
      showScrollbar,
    ],
  );

  return (
    <div className='card-table-scroll-shell' ref={containerRef}>
      {children}
      <div
        className='card-table-scrollbar'
        style={{ display: showScrollbar ? undefined : 'none' }}
        ref={trackRef}
        onPointerDown={handleTrackPointerDown}
        onKeyDown={handleScrollbarKeyDown}
        role='scrollbar'
        aria-label='Horizontal table scroll'
        tabIndex={showScrollbar ? 0 : -1}
        aria-orientation='horizontal'
        aria-valuemin={0}
        aria-valuemax={Math.round(scrollableWidth)}
        aria-valuenow={Math.round(metrics.scrollLeft)}
      >
        <div
          className='card-table-scrollbar-thumb'
          style={{
            left: `${thumbLeft}px`,
            width: `${thumbWidth}px`,
          }}
          onPointerDown={handleThumbPointerDown}
          onPointerMove={handleThumbPointerMove}
          onPointerUp={handleThumbPointerEnd}
          onPointerCancel={handleThumbPointerEnd}
        />
      </div>
    </div>
  );
};

DesktopTableScrollProxy.propTypes = {
  children: PropTypes.node,
  syncKey: PropTypes.string,
};

CardTable.propTypes = {
  columns: PropTypes.array.isRequired,
  dataSource: PropTypes.array,
  loading: PropTypes.bool,
  rowKey: PropTypes.oneOfType([PropTypes.string, PropTypes.func]),
  hidePagination: PropTypes.bool,
};

function normalizePagination(pagination) {
  if (!pagination || pagination === false) return pagination;
  if (pagination === true) return { showQuickJumper: true };
  return { ...pagination, showQuickJumper: true };
}

export default CardTable;
