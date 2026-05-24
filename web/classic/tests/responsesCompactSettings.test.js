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
  RESPONSES_COMPACT_MODE_DEFAULT,
  RESPONSES_COMPACT_MODE_CONVERT,
  RESPONSES_COMPACT_MODE_DISABLED,
  RESPONSES_COMPACT_MODE_NATIVE,
  buildResponsesCompactSettings,
  normalizeResponsesCompactMode,
} from '../src/helpers/responsesCompactSettings.js';

describe('classic responses compact settings', () => {
  test('defaults missing compact mode to native', () => {
    expect(RESPONSES_COMPACT_MODE_DEFAULT).toBe(RESPONSES_COMPACT_MODE_NATIVE);
    expect(normalizeResponsesCompactMode(undefined)).toBe(
      RESPONSES_COMPACT_MODE_NATIVE,
    );
    expect(normalizeResponsesCompactMode('')).toBe(
      RESPONSES_COMPACT_MODE_NATIVE,
    );
    expect(normalizeResponsesCompactMode('unexpected')).toBe(
      RESPONSES_COMPACT_MODE_CONVERT,
    );
  });

  test('keeps legacy unsupported mode disabled', () => {
    expect(normalizeResponsesCompactMode('unsupported')).toBe(
      RESPONSES_COMPACT_MODE_DISABLED,
    );
    expect(normalizeResponsesCompactMode(RESPONSES_COMPACT_MODE_DISABLED)).toBe(
      RESPONSES_COMPACT_MODE_DISABLED,
    );
  });

  test('stores compact mode only for OpenAI channels', () => {
    expect(buildResponsesCompactSettings(1, undefined)).toEqual({
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
    });
    expect(
      buildResponsesCompactSettings(14, RESPONSES_COMPACT_MODE_NATIVE),
    ).toEqual({});
  });
});
