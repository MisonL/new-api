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

const toFiniteNumber = (value) => {
  const numberValue = Number(value);
  return Number.isFinite(numberValue) ? numberValue : 0;
};

export const isDashboardDataCrossYear = (timestamps) => {
  if (!Array.isArray(timestamps) || timestamps.length === 0) {
    return false;
  }
  const years = new Set(
    timestamps.map((timestamp) =>
      new Date(toFiniteNumber(timestamp) * 1000).getFullYear(),
    ),
  );
  return years.size > 1;
};

const padTimePart = (value) => String(value).padStart(2, '0');

export const formatDashboardTimeBucket = (
  timestamp,
  granularity = 'hour',
  showYear = false,
) => {
  const date = new Date(toFiniteNumber(timestamp) * 1000);
  const year = date.getFullYear();
  const month = padTimePart(date.getMonth() + 1);
  const day = padTimePart(date.getDate());
  const hour = padTimePart(date.getHours());

  let text = showYear ? `${year}-${month}-${day}` : `${month}-${day}`;
  if (granularity === 'hour') {
    text += ` ${hour}:00`;
  } else if (granularity === 'week') {
    const nextWeek = new Date(date.getTime() + 6 * 24 * 60 * 60 * 1000);
    const nextWeekYear = nextWeek.getFullYear();
    const nextMonth = padTimePart(nextWeek.getMonth() + 1);
    const nextDay = padTimePart(nextWeek.getDate());
    const nextText = showYear
      ? `${nextWeekYear}-${nextMonth}-${nextDay}`
      : `${nextMonth}-${nextDay}`;
    text += ` - ${nextText}`;
  }
  return text;
};

export const toDashboardFiniteNumber = toFiniteNumber;
