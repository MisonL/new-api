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

import {
  formatDashboardTimeBucket,
  isDashboardDataCrossYear,
  toDashboardFiniteNumber,
} from './dashboardTimeBucket.js';

const normalizeModelFilter = (models) => {
  if (!Array.isArray(models) || models.length === 0) {
    return null;
  }
  return new Set(models.filter((model) => typeof model === 'string' && model));
};

const getUniqueChartTimes = (chartValues) => {
  if (!Array.isArray(chartValues)) {
    return [];
  }
  const times = [];
  const seen = new Set();
  chartValues.forEach((item) => {
    if (!item || item.Time == null || seen.has(item.Time)) {
      return;
    }
    seen.add(item.Time);
    times.push(String(item.Time));
  });
  return times;
};

const getBucketDurationSeconds = (granularity) => {
  if (granularity === 'week') {
    return 7 * 24 * 60 * 60;
  }
  if (granularity === 'day') {
    return 24 * 60 * 60;
  }
  return 60 * 60;
};

export const getDashboardBucketLogRange = ({
  quotaData,
  targetTime,
  granularity,
}) => {
  if (!targetTime || !Array.isArray(quotaData)) {
    return {
      startTimestamp: 0,
      endTimestamp: 0,
    };
  }

  const showYear = isDashboardDataCrossYear(
    quotaData.map((item) => item.created_at),
  );
  const match = quotaData.find(
    (item) =>
      formatDashboardTimeBucket(item.created_at, granularity, showYear) ===
      targetTime,
  );
  if (!match) {
    return {
      startTimestamp: 0,
      endTimestamp: 0,
    };
  }

  const startTimestamp = toDashboardFiniteNumber(match.created_at);
  return {
    startTimestamp,
    endTimestamp: startTimestamp + getBucketDurationSeconds(granularity) - 1,
  };
};

const findDrilldownDatum = (datum) => {
  if (Array.isArray(datum)) {
    for (const item of datum) {
      const matched = findDrilldownDatum(item);
      if (matched) {
        return matched;
      }
    }
    return null;
  }
  if (!datum || typeof datum !== 'object' || !datum.Time) {
    return null;
  }
  return datum;
};

export const getDashboardDrilldownTarget = ({ datum, otherLabel }) => {
  const matchedDatum = findDrilldownDatum(datum);
  if (!matchedDatum) {
    return null;
  }

  if (
    matchedDatum.Model === otherLabel &&
    Array.isArray(matchedDatum.CollapsedModels) &&
    matchedDatum.CollapsedModels.length > 0
  ) {
    return {
      time: matchedDatum.Time,
      models: matchedDatum.CollapsedModels,
    };
  }

  return {
    time: matchedDatum.Time,
    models: null,
  };
};

export const getDashboardDimensionDrilldownTarget = ({ dimensionInfo }) => {
  const firstDimension = Array.isArray(dimensionInfo) ? dimensionInfo[0] : null;
  if (!firstDimension) {
    return null;
  }

  const dimensionDatum = Array.isArray(firstDimension.data)
    ? firstDimension.data.map((item) => item?.datum)
    : null;
  const datumTarget = getDashboardDrilldownTarget({
    datum: dimensionDatum,
  });
  if (datumTarget) {
    return datumTarget;
  }

  if (firstDimension.value == null) {
    return null;
  }
  return {
    time: String(firstDimension.value),
    models: null,
  };
};

export const getDashboardChartAreaDrilldownTarget = ({
  clientX,
  rect,
  chartValues,
}) => {
  if (
    !rect ||
    !Number.isFinite(clientX) ||
    !Number.isFinite(rect.left) ||
    !Number.isFinite(rect.width) ||
    rect.width <= 0
  ) {
    return null;
  }

  const times = getUniqueChartTimes(chartValues);
  if (times.length === 0) {
    return null;
  }

  const ratio = Math.min(Math.max((clientX - rect.left) / rect.width, 0), 1);
  const index = Math.min(Math.floor(ratio * times.length), times.length - 1);
  return {
    time: times[index],
    models: null,
  };
};

export const createDashboardChartAreaClickGuard = () => {
  let chartClickHandled = false;

  return {
    markChartClickHandled: () => {
      chartClickHandled = true;
    },
    shouldHandleAreaClick: () => {
      if (!chartClickHandled) {
        return true;
      }
      chartClickHandled = false;
      return false;
    },
  };
};

export const buildDashboardDrilldown = ({
  quotaData,
  targetTime,
  granularity,
  models,
  t,
}) => {
  const translate = typeof t === 'function' ? t : (text) => text;
  if (!targetTime || !Array.isArray(quotaData)) {
    return null;
  }

  const modelFilter = normalizeModelFilter(models);
  const showYear = isDashboardDataCrossYear(
    quotaData.map((item) => item.created_at),
  );
  const modelMap = new Map();

  quotaData.forEach((item) => {
    const timeKey = formatDashboardTimeBucket(
      item.created_at,
      granularity,
      showYear,
    );
    if (timeKey !== targetTime) {
      return;
    }

    const model = item.model_name || translate('未知模型');
    if (modelFilter && !modelFilter.has(model)) {
      return;
    }

    const prev = modelMap.get(model) || {
      model,
      quota: 0,
      count: 0,
      tokens: 0,
    };
    modelMap.set(model, {
      model,
      quota: prev.quota + toDashboardFiniteNumber(item.quota),
      count: prev.count + toDashboardFiniteNumber(item.count),
      tokens: prev.tokens + toDashboardFiniteNumber(item.token_used),
    });
  });

  const rows = Array.from(modelMap.values())
    .filter((item) => item.quota > 0 || item.count > 0 || item.tokens > 0)
    .sort((a, b) => b.quota - a.quota || b.count - a.count);
  const totalQuota = rows.reduce((sum, item) => sum + item.quota, 0);
  const totalCount = rows.reduce((sum, item) => sum + item.count, 0);
  const totalTokens = rows.reduce((sum, item) => sum + item.tokens, 0);
  const detailRows = rows.map((item) => ({
    ...item,
    ratio: totalQuota > 0 ? item.quota / totalQuota : 0,
  }));

  const logRange = getDashboardBucketLogRange({
    quotaData,
    targetTime,
    granularity,
  });

  return {
    time: targetTime,
    startTimestamp: logRange.startTimestamp,
    endTimestamp: logRange.endTimestamp,
    rows: detailRows,
    distribution: detailRows
      .filter((item) => item.quota > 0)
      .map((item) => ({
        type: item.model,
        value: item.quota,
      })),
    totalQuota,
    totalCount,
    totalTokens,
  };
};
