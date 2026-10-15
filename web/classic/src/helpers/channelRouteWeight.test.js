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

import { describe, expect, test } from 'bun:test';
import {
  adjustRouteWeights,
  formatRouteWeightPercent,
  parseRouteWeightPercent,
  ROUTE_WEIGHT_TOTAL_BPS,
} from './channelRouteWeight.js';

describe('classic channel route weights', () => {
  test('parses and formats percentage values without floating point drift', () => {
    expect(parseRouteWeightPercent('12.34')).toBe(1234);
    expect(parseRouteWeightPercent('100.01')).toBeNull();
    expect(formatRouteWeightPercent(1234)).toBe('12.34');
  });

  test('redistributes unlocked weights while preserving the total', () => {
    const result = adjustRouteWeights(
      [5000, 3000, 2000],
      0,
      7000,
      'equal_adjust',
      [false, true, false],
    );
    expect(result).toEqual([7000, 3000, 0]);
    expect(result.reduce((sum, value) => sum + value, 0)).toBe(
      ROUTE_WEIGHT_TOTAL_BPS,
    );
  });

  test('rejects edits that would require changing locked rows', () => {
    expect(
      adjustRouteWeights([5000, 5000], 0, 6000, 'equal_adjust', [false, true]),
    ).toBeNull();
  });
});
