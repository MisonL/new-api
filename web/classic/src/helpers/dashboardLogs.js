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

export const DASHBOARD_LOG_TYPES = [
  { value: 0, label: '全部' },
  { value: 1, label: '充值' },
  { value: 2, label: '消费' },
  { value: 3, label: '管理' },
  { value: 4, label: '系统' },
  { value: 5, label: '错误' },
  { value: 6, label: '退款' },
];

export const DASHBOARD_LOG_PAGE_SIZE = 10;

const toSafeTimestamp = (value) => {
  const timestamp = Number(value);
  return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : 0;
};

const padTimePart = (value) => String(value).padStart(2, '0');

export const formatDashboardLogTimestamp = (timestamp) => {
  const date = new Date(toSafeTimestamp(timestamp) * 1000);
  const year = date.getFullYear();
  const month = padTimePart(date.getMonth() + 1);
  const day = padTimePart(date.getDate());
  const hour = padTimePart(date.getHours());
  const minute = padTimePart(date.getMinutes());
  const second = padTimePart(date.getSeconds());
  return `${year}-${month}-${day} ${hour}:${minute}:${second}`;
};

export const buildDashboardLogInitialFilters = (scope, fallbackRange) => {
  const startTimestamp = toSafeTimestamp(
    scope?.startTimestamp || fallbackRange.startTimestamp,
  );
  const endTimestamp = toSafeTimestamp(
    scope?.endTimestamp || fallbackRange.endTimestamp,
  );
  return {
    logType: scope?.logType ?? 2,
    username: scope?.username || '',
    token_name: scope?.token_name || '',
    model_name: scope?.model_name || '',
    channel: scope?.channel || '',
    group: scope?.group || '',
    request_id: scope?.request_id || '',
    dateRange: [
      formatDashboardLogTimestamp(startTimestamp),
      formatDashboardLogTimestamp(endTimestamp),
    ],
  };
};

const toUnixSeconds = (value) => {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return 0;
  }
  return Math.floor(timestamp / 1000);
};

export const normalizeDashboardLogFilters = (values) => {
  const dateRange = Array.isArray(values.dateRange) ? values.dateRange : [];
  return {
    type: Number(values.logType || 0),
    username: values.username || '',
    token_name: values.token_name || '',
    model_name: values.model_name || '',
    channel: values.channel || '',
    group: values.group || '',
    request_id: values.request_id || '',
    start_timestamp: toUnixSeconds(dateRange[0]),
    end_timestamp: toUnixSeconds(dateRange[1]),
  };
};
