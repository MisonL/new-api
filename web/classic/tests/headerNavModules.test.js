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
  getHeaderNavModuleAccess,
  parseHeaderNavModules,
} from '../src/helpers/headerNavModules.js';

describe('classic header navigation modules', () => {
  test('provides pricing and rankings defaults', () => {
    const modules = parseHeaderNavModules('');
    expect(modules.pricing).toEqual({ enabled: true, requireAuth: false });
    expect(modules.rankings).toEqual({ enabled: true, requireAuth: false });
  });

  test('normalizes legacy boolean access modules', () => {
    const modules = parseHeaderNavModules(
      JSON.stringify({ pricing: false, rankings: true }),
    );
    expect(modules.pricing).toEqual({ enabled: false, requireAuth: false });
    expect(modules.rankings).toEqual({ enabled: true, requireAuth: false });
  });

  test('returns enabled and authentication policy for route guards', () => {
    const raw = JSON.stringify({
      rankings: { enabled: true, requireAuth: true },
    });
    expect(getHeaderNavModuleAccess(raw, 'rankings')).toEqual({
      enabled: true,
      requireAuth: true,
    });
  });
});
