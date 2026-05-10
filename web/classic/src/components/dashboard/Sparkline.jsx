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

import React from 'react';

const CHART_WIDTH = 96;
const CHART_HEIGHT = 40;
const CHART_PADDING = 4;

const toFiniteNumber = (value) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

const buildPolylinePoints = (data) => {
  const values = data.map(toFiniteNumber);
  if (values.length === 0) {
    return '';
  }

  const min = Math.min(...values);
  const max = Math.max(...values);
  const valueRange = max - min || 1;
  const xStep =
    values.length > 1
      ? (CHART_WIDTH - CHART_PADDING * 2) / (values.length - 1)
      : 0;

  return values
    .map((value, index) => {
      const x = CHART_PADDING + index * xStep;
      const y =
        CHART_HEIGHT -
        CHART_PADDING -
        ((value - min) / valueRange) * (CHART_HEIGHT - CHART_PADDING * 2);
      return `${x.toFixed(2)},${y.toFixed(2)}`;
    })
    .join(' ');
};

const Sparkline = ({ data = [], color = '#06b6d4' }) => {
  const points = buildPolylinePoints(data);
  if (!points) {
    return null;
  }

  return (
    <svg
      aria-hidden='true'
      className='h-10 w-24'
      focusable='false'
      viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`}
    >
      <polyline
        fill='none'
        points={points}
        stroke={color}
        strokeLinecap='round'
        strokeLinejoin='round'
        strokeWidth='2'
      />
    </svg>
  );
};

export default Sparkline;
