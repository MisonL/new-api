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

import { useCallback, useRef, useState } from 'react';
import {
  buildDashboardDrilldown,
  createDashboardChartAreaClickGuard,
  getDashboardChartAreaDrilldownTarget,
  getDashboardDimensionDrilldownTarget,
  getDashboardDrilldownTarget,
  getDashboardLegendDrilldownTarget,
} from '../../helpers/dashboardDrilldown';

export const useDashboardDrilldown = ({
  quotaData,
  dataExportDefaultTime,
  specLine,
  t,
}) => {
  const [drilldownDetail, setDrilldownDetail] = useState(null);
  const areaClickGuardRef = useRef(createDashboardChartAreaClickGuard());

  const openDrilldown = useCallback(
    (target) => {
      if (!target) {
        return false;
      }
      const detail = buildDashboardDrilldown({
        quotaData,
        targetTime: target.time,
        targetTimes: target.times,
        granularity: dataExportDefaultTime,
        models: target.models,
        t,
      });
      if (!detail || detail.rows.length === 0) {
        return false;
      }
      setDrilldownDetail(detail);
      return true;
    },
    [dataExportDefaultTime, quotaData, t],
  );

  const handleQuotaBarClick = useCallback(
    (event) => {
      const target = getDashboardDrilldownTarget({
        datum: event?.datum || event?.item?.getDatum?.(),
        otherLabel: t('其他'),
      });
      areaClickGuardRef.current.markChartClickHandled(
        openDrilldown(target) ? target : null,
      );
    },
    [openDrilldown, t],
  );

  const handleQuotaDimensionClick = useCallback(
    (event) => {
      const target = getDashboardDimensionDrilldownTarget({
        dimensionInfo: event?.dimensionInfo,
        otherLabel: t('其他'),
      });
      areaClickGuardRef.current.markChartClickHandled(
        openDrilldown(target) ? target : null,
      );
    },
    [openDrilldown, t],
  );

  const handleQuotaChartAreaClick = useCallback(
    (event) => {
      if (!areaClickGuardRef.current.shouldHandleAreaClick()) {
        return;
      }
      openDrilldown(
        getDashboardChartAreaDrilldownTarget({
          clientX: event.clientX,
          rect: event.currentTarget.getBoundingClientRect(),
          chartValues: specLine?.data?.[0]?.values,
        }),
      );
    },
    [openDrilldown, specLine],
  );

  const handleQuotaLegendClick = useCallback(
    (event) => {
      const target = getDashboardLegendDrilldownTarget({
        event,
        chartValues: specLine?.data?.[0]?.values,
        otherLabel: t('其他'),
      });
      areaClickGuardRef.current.markChartClickHandled(
        openDrilldown(target) ? target : null,
      );
    },
    [openDrilldown, specLine, t],
  );

  return {
    drilldownDetail,
    closeDrilldown: () => setDrilldownDetail(null),
    handleQuotaBarClick,
    handleQuotaDimensionClick,
    handleQuotaChartAreaClick,
    handleQuotaLegendClick,
  };
};
