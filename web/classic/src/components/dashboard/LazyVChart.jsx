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

import React, { lazy, Suspense, useEffect } from 'react';
import { Skeleton } from '@douyinfe/semi-ui';

const VChart = lazy(() =>
  import('@visactor/react-vchart').then((module) => ({
    default: module.VChart,
  })),
);
let chartThemeInitialized = false;

const initChartTheme = () =>
  import('@visactor/vchart-semi-theme').then((module) => {
    if (chartThemeInitialized) {
      return;
    }
    module.initVChartSemiTheme({
      isWatchingThemeSwitch: true,
    });
    chartThemeInitialized = true;
  });

const scheduleIdle = (callback) => {
  if (typeof window !== 'undefined' && window.requestIdleCallback) {
    return window.requestIdleCallback(callback, { timeout: 2000 });
  }
  return window.setTimeout(callback, 1);
};

const cancelIdle = (id) => {
  if (typeof window !== 'undefined' && window.cancelIdleCallback) {
    window.cancelIdleCallback(id);
    return;
  }
  window.clearTimeout(id);
};

export const warmDashboardChartEngine = () => {
  const idleId = scheduleIdle(() => {
    Promise.all([import('@visactor/react-vchart'), initChartTheme()]);
  });
  return () => cancelIdle(idleId);
};

const LazyVChart = ({ fallbackClassName = 'h-full w-full', ...props }) => {
  useEffect(() => {
    initChartTheme();
  }, []);

  return (
    <Suspense
      fallback={
        <div className={fallbackClassName}>
          <Skeleton
            active
            loading
            placeholder={<Skeleton.Paragraph rows={4} />}
          />
        </div>
      }
    >
      <VChart {...props} />
    </Suspense>
  );
};

export const DashboardChartWarmup = () => {
  useEffect(() => warmDashboardChartEngine(), []);
  return null;
};

export default LazyVChart;
