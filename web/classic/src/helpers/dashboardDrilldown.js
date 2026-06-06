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
  if (!Array.isArray(models)) {
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

const formatRangeLabel = (times) => {
  if (!Array.isArray(times) || times.length === 0) {
    return '';
  }
  if (times.length === 1) {
    return times[0];
  }
  return `${times[0]} - ${times[times.length - 1]}`;
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

const findDrilldownDatum = (datum, otherLabel) => {
  if (Array.isArray(datum)) {
    // Dimension events include every series at a time bucket; prefer the scoped Other datum.
    const scopedOtherDatum = datum
      .map((item) => findDrilldownDatum(item, otherLabel))
      .find(
        (item) =>
          item &&
          item.Model === otherLabel &&
          Array.isArray(item.CollapsedModels),
      );
    if (scopedOtherDatum) {
      return scopedOtherDatum;
    }

    for (const item of datum) {
      const matched = findDrilldownDatum(item, otherLabel);
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
  const matchedDatum = findDrilldownDatum(datum, otherLabel);
  if (!matchedDatum) {
    return null;
  }

  if (
    matchedDatum.Model === otherLabel &&
    Array.isArray(matchedDatum.CollapsedModels)
  ) {
    return {
      time: matchedDatum.Time,
      models: matchedDatum.CollapsedModels.filter(
        (model) => typeof model === 'string' && model,
      ),
    };
  }

  return {
    time: matchedDatum.Time,
    models: null,
  };
};

const extractLegendModel = (event) => {
  const candidates = [
    event?.event?.detail?.data?.id,
    event?.event?.detail?.data?.label,
    event?.event?.detail?.data?.value,
    event?.detail?.data?.id,
    event?.detail?.data?.label,
    event?.detail?.data?.value,
    event?.data?.id,
    event?.data?.label,
    event?.data?.value,
    event?.datum?.Model,
    event?.Model,
    event?.value,
  ];
  const matched = candidates.find(
    (value) => typeof value === 'string' && value !== '',
  );
  return matched || '';
};

const getCollapsedModelsForChartRange = (chartValues, otherLabel) => {
  if (!Array.isArray(chartValues)) {
    return [];
  }

  const models = [];
  const seen = new Set();
  chartValues.forEach((item) => {
    if (
      !item ||
      item.Model !== otherLabel ||
      !Array.isArray(item.CollapsedModels)
    ) {
      return;
    }
    item.CollapsedModels.forEach((model) => {
      if (typeof model !== 'string' || model === '' || seen.has(model)) {
        return;
      }
      seen.add(model);
      models.push(model);
    });
  });
  return models;
};

export const getDashboardLegendDrilldownTarget = ({
  event,
  chartValues,
  otherLabel,
}) => {
  const model = extractLegendModel(event);
  const times = getUniqueChartTimes(chartValues);
  if (!model || times.length === 0) {
    return null;
  }

  return {
    time: formatRangeLabel(times),
    times,
    models:
      model === otherLabel
        ? getCollapsedModelsForChartRange(chartValues, otherLabel)
        : [model],
  };
};

export const getDashboardDimensionDrilldownTarget = ({
  dimensionInfo,
  otherLabel,
}) => {
  const firstDimension = Array.isArray(dimensionInfo) ? dimensionInfo[0] : null;
  if (!firstDimension) {
    return null;
  }

  const dimensionDatum = Array.isArray(firstDimension.data)
    ? firstDimension.data.map((item) => item?.datum)
    : null;
  const datumTarget = getDashboardDrilldownTarget({
    datum: dimensionDatum,
    otherLabel,
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
    markChartClickHandled(target) {
      chartClickHandled = arguments.length === 0 ? true : Boolean(target?.time);
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

const getDatumRecord = ({ datum, item }) => {
  const itemDatum = item?.getDatum?.();
  const candidate = datum || itemDatum;
  if (Array.isArray(candidate)) {
    return candidate.find(
      (entry) => entry && typeof entry === 'object' && entry.type != null,
    );
  }
  if (!candidate || typeof candidate !== 'object') {
    return null;
  }
  return candidate;
};

export const getDashboardDistributionLogRow = ({ datum, item, rows }) => {
  if (!Array.isArray(rows)) {
    return null;
  }
  const record = getDatumRecord({ datum, item });
  if (!record || record.type == null) {
    return null;
  }
  const model = String(record.type);
  return rows.find((row) => row?.model === model) || null;
};

export const buildDashboardDrilldown = ({
  quotaData,
  targetTime,
  targetTimes,
  granularity,
  models,
  t,
}) => {
  const translate = typeof t === 'function' ? t : (text) => text;
  const targetTimeSet = Array.isArray(targetTimes)
    ? new Set(targetTimes.filter((time) => typeof time === 'string' && time))
    : null;
  if ((!targetTime && !targetTimeSet) || !Array.isArray(quotaData)) {
    return null;
  }

  const modelFilter = normalizeModelFilter(models);
  const showYear = isDashboardDataCrossYear(
    quotaData.map((item) => item.created_at),
  );
  const modelMap = new Map();
  let logRangeStart = 0;
  let logRangeEnd = 0;

  quotaData.forEach((item) => {
    const timeKey = formatDashboardTimeBucket(
      item.created_at,
      granularity,
      showYear,
    );
    if (targetTimeSet ? !targetTimeSet.has(timeKey) : timeKey !== targetTime) {
      return;
    }

    const startTimestamp = toDashboardFiniteNumber(item.created_at);
    if (startTimestamp > 0) {
      const endTimestamp =
        startTimestamp + getBucketDurationSeconds(granularity) - 1;
      logRangeStart =
        logRangeStart === 0
          ? startTimestamp
          : Math.min(logRangeStart, startTimestamp);
      logRangeEnd = Math.max(logRangeEnd, endTimestamp);
    }

    const rawModelName = item.model_name || '';
    const model = rawModelName || translate('未知模型');
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
      logModelName: rawModelName,
      quota: prev.quota + toDashboardFiniteNumber(item.quota),
      count: prev.count + toDashboardFiniteNumber(item.count),
      tokens: prev.tokens + toDashboardFiniteNumber(item.token_used),
    });
  });

  const rows = Array.from(modelMap.values())
    .filter((item) => item.quota > 0 || item.count > 0 || item.tokens > 0)
    .sort((a, b) => b.quota - a.quota || b.count - a.count);
  const totals = rows.reduce(
    (sum, item) => ({
      quota: sum.quota + item.quota,
      count: sum.count + item.count,
      tokens: sum.tokens + item.tokens,
    }),
    { quota: 0, count: 0, tokens: 0 },
  );
  const detailRows = rows.map((item) => ({
    ...item,
    ratio: totals.quota > 0 ? item.quota / totals.quota : 0,
  }));

  return {
    time: targetTime || formatRangeLabel(Array.from(targetTimeSet || [])),
    startTimestamp: logRangeStart,
    endTimestamp: logRangeEnd,
    rows: detailRows,
    distribution: detailRows
      .filter((item) => item.quota > 0)
      .map((item) => ({
        type: item.model,
        value: item.quota,
      })),
    totalQuota: totals.quota,
    totalCount: totals.count,
    totalTokens: totals.tokens,
  };
};
