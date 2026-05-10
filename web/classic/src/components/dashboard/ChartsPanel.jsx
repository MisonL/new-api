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

import React, { useMemo, useState } from 'react';
import { Card } from '@douyinfe/semi-ui';
import { parseDashboardTimestamp } from '../../helpers/dashboard';
import { useDashboardDrilldown } from '../../hooks/dashboard/useDashboardDrilldown';
import ChartsPanelHeader from './ChartsPanelHeader';
import ChartsRangeToolbar from './ChartsRangeToolbar';
import DashboardDrilldownModal from './DashboardDrilldownModal';
import DashboardLogsModal from './DashboardLogsModal';
import LazyVChart, { DashboardChartWarmup } from './LazyVChart';

// ChartsPanel renders the dashboard charts together with time-range controls.
const ChartsPanel = ({
  activeChartTab,
  setActiveChartTab,
  spec_line,
  spec_model_line,
  spec_pie,
  spec_rank_bar,
  spec_user_rank,
  spec_user_trend,
  quotaData,
  modelColors,
  dashboardInputs,
  isAdminUser,
  CARD_PROPS,
  CHART_CONFIG,
  FLEX_CENTER_GAP2,
  hasApiInfoPanel,
  customRangeDraft,
  timeOptions,
  dataExportDefaultTime,
  activeRangePreset,
  quickRangeOptions,
  handleRangePresetChange,
  handleCustomRangeChange,
  handleCustomRangeConfirm,
  loading,
  t,
}) => {
  const [logScope, setLogScope] = useState(null);
  const customRangeValue = [
    customRangeDraft.start_timestamp,
    customRangeDraft.end_timestamp,
  ];
  const hasCompleteCustomRange = customRangeValue.every(Boolean);
  const customRangeStart = parseDashboardTimestamp(customRangeValue[0]);
  const customRangeEnd = parseDashboardTimestamp(customRangeValue[1]);
  const isCustomRangeOrderValid =
    !hasCompleteCustomRange ||
    (Number.isFinite(customRangeStart) &&
      Number.isFinite(customRangeEnd) &&
      customRangeStart < customRangeEnd);
  const activeGranularityLabel =
    timeOptions.find((option) => option.value === dataExportDefaultTime)
      ?.label || dataExportDefaultTime;
  const {
    drilldownDetail,
    closeDrilldown,
    handleQuotaBarClick,
    handleQuotaDimensionClick,
    handleQuotaChartAreaClick,
  } = useDashboardDrilldown({
    quotaData,
    dataExportDefaultTime,
    specLine: spec_line,
    t,
  });
  const fallbackLogRange = useMemo(
    () => ({
      startTimestamp: Math.floor(customRangeStart / 1000) || 0,
      endTimestamp: Math.floor(customRangeEnd / 1000) || 0,
    }),
    [customRangeEnd, customRangeStart],
  );
  const buildBaseLogScope = () => ({
    title: t('相关日志'),
    fast_page: true,
    compact: true,
    startTimestamp: fallbackLogRange.startTimestamp,
    endTimestamp: fallbackLogRange.endTimestamp,
    username: dashboardInputs?.username || '',
    token_name: dashboardInputs?.token_name || '',
    model_name: dashboardInputs?.model_name || '',
    channel: dashboardInputs?.channel || '',
    group: dashboardInputs?.group || '',
    request_id: dashboardInputs?.request_id || '',
    logType: 2,
  });
  const openDashboardLogs = () => {
    setLogScope(buildBaseLogScope());
  };
  const openDrilldownLogs = () => {
    if (!drilldownDetail) {
      return;
    }
    setLogScope({
      ...buildBaseLogScope(),
      title: `${t('相关日志')} · ${drilldownDetail.time}`,
      startTimestamp: drilldownDetail.startTimestamp,
      endTimestamp: drilldownDetail.endTimestamp,
    });
  };
  const openDrilldownRowLogs = (row) => {
    if (!drilldownDetail || !row?.model) {
      return;
    }
    const isEmptyModel = row.logModelName === '';
    setLogScope({
      ...buildBaseLogScope(),
      title: `${t('相关日志')} - ${drilldownDetail.time} - ${row.model}`,
      startTimestamp: drilldownDetail.startTimestamp,
      endTimestamp: drilldownDetail.endTimestamp,
      model_name: isEmptyModel ? '' : row.logModelName || row.model,
      model_name_empty: isEmptyModel,
      model_name_empty_label: row.model,
    });
  };
  const closeLogs = () => setLogScope(null);

  return (
    <>
      <DashboardChartWarmup />
      <Card
        {...CARD_PROPS}
        className={`!rounded-2xl ${hasApiInfoPanel ? 'lg:col-span-3' : ''}`}
        title={
          <ChartsPanelHeader
            activeChartTab={activeChartTab}
            setActiveChartTab={setActiveChartTab}
            isAdminUser={isAdminUser}
            flexCenterGap2={FLEX_CENTER_GAP2}
            onOpenLogs={openDashboardLogs}
            t={t}
          />
        }
        bodyStyle={{ padding: 0 }}
      >
        <ChartsRangeToolbar
          activeRangePreset={activeRangePreset}
          activeGranularityLabel={activeGranularityLabel}
          customRangeDraft={customRangeDraft}
          customRangeValue={customRangeValue}
          hasCompleteCustomRange={hasCompleteCustomRange}
          isCustomRangeOrderValid={isCustomRangeOrderValid}
          loading={loading}
          quickRangeOptions={quickRangeOptions}
          timeOptions={timeOptions}
          handleRangePresetChange={handleRangePresetChange}
          handleCustomRangeChange={handleCustomRangeChange}
          handleCustomRangeConfirm={handleCustomRangeConfirm}
          t={t}
        />
        <div className='h-96 p-2 pt-0'>
          {activeChartTab === '1' && (
            <div
              className='h-full cursor-pointer'
              onClick={handleQuotaChartAreaClick}
            >
              <LazyVChart
                spec={spec_line}
                option={CHART_CONFIG}
                onClick={handleQuotaBarClick}
                onPointerTap={handleQuotaBarClick}
                onDimensionClick={handleQuotaDimensionClick}
              />
            </div>
          )}
          {activeChartTab === '2' && (
            <LazyVChart spec={spec_model_line} option={CHART_CONFIG} />
          )}
          {activeChartTab === '3' && (
            <LazyVChart spec={spec_pie} option={CHART_CONFIG} />
          )}
          {activeChartTab === '4' && (
            <LazyVChart spec={spec_rank_bar} option={CHART_CONFIG} />
          )}
          {activeChartTab === '5' && isAdminUser && (
            <LazyVChart spec={spec_user_rank} option={CHART_CONFIG} />
          )}
          {activeChartTab === '6' && isAdminUser && (
            <LazyVChart spec={spec_user_trend} option={CHART_CONFIG} />
          )}
        </div>
      </Card>
      <DashboardDrilldownModal
        detail={drilldownDetail}
        modelColors={modelColors}
        chartConfig={CHART_CONFIG}
        onClose={closeDrilldown}
        onOpenLogs={openDrilldownLogs}
        onOpenRowLogs={openDrilldownRowLogs}
        t={t}
      />
      <DashboardLogsModal
        visible={!!logScope}
        scope={logScope}
        fallbackRange={fallbackLogRange}
        isAdminUser={isAdminUser}
        onClose={closeLogs}
        t={t}
      />
    </>
  );
};

export default ChartsPanel;
