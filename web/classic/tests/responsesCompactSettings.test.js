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
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  RESPONSES_COMPACT_MODE_AUTO,
  RESPONSES_COMPACT_MODE_DEFAULT,
  RESPONSES_COMPACT_MODE_NATIVE,
  RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
  buildResponsesCompactSettings,
  clearResponsesCompactSettings,
  normalizeResponsesCompactMode,
  resetResponsesCompactAutoFallbackOnModeChange,
} from '../src/helpers/responsesCompactSettings.js';

const currentDir = dirname(fileURLToPath(import.meta.url));

describe('classic responses compact settings', () => {
  test('defaults missing compact mode to auto', () => {
    expect(RESPONSES_COMPACT_MODE_DEFAULT).toBe(RESPONSES_COMPACT_MODE_AUTO);
    expect(normalizeResponsesCompactMode(undefined)).toBe(
      RESPONSES_COMPACT_MODE_AUTO,
    );
    expect(normalizeResponsesCompactMode('')).toBe(RESPONSES_COMPACT_MODE_AUTO);
    expect(normalizeResponsesCompactMode('convert')).toBe(
      RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
    );
    expect(normalizeResponsesCompactMode('auto')).toBe(
      RESPONSES_COMPACT_MODE_AUTO,
    );
    for (const mode of ['disabled', 'unsupported']) {
      expect(normalizeResponsesCompactMode(mode)).toBe(
        RESPONSES_COMPACT_MODE_NATIVE,
      );
    }
    expect(normalizeResponsesCompactMode('unexpected')).toBe(
      RESPONSES_COMPACT_MODE_AUTO,
    );
  });

  test('keeps synthetic compact mode explicit', () => {
    expect(
      normalizeResponsesCompactMode(RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY),
    ).toBe(RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY);
  });

  test('stores compact mode only for OpenAI channels', () => {
    expect(buildResponsesCompactSettings(1, undefined)).toEqual({
      responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
    });
    expect(buildResponsesCompactSettings(1, RESPONSES_COMPACT_MODE_AUTO)).toEqual({
      responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
    });
    expect(
      buildResponsesCompactSettings(
        1,
        RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
      ),
    ).toEqual({
      responses_compact_mode: RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
    });
    expect(buildResponsesCompactSettings(1, 'convert')).toEqual({
      responses_compact_mode: RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
    });
    expect(
      buildResponsesCompactSettings(14, RESPONSES_COMPACT_MODE_NATIVE),
    ).toEqual({});
  });

  test('resets auto fallback state only when compact mode changes', () => {
    const settings = {
      responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
      responses_compact_auto_fallback_date: 20260526,
      responses_compact_auto_fallback_reason: 'status_code=404',
    };
    expect(
      resetResponsesCompactAutoFallbackOnModeChange(
        settings,
        RESPONSES_COMPACT_MODE_AUTO,
      ),
    ).toBe(false);
    expect(settings.responses_compact_auto_fallback_date).toBe(20260526);

    expect(
      resetResponsesCompactAutoFallbackOnModeChange(
        settings,
        RESPONSES_COMPACT_MODE_NATIVE,
      ),
    ).toBe(true);
    expect(settings.responses_compact_auto_fallback_date).toBeUndefined();
    expect(settings.responses_compact_auto_fallback_reason).toBeUndefined();

    expect(
      resetResponsesCompactAutoFallbackOnModeChange(
        null,
        RESPONSES_COMPACT_MODE_NATIVE,
      ),
    ).toBe(false);
  });

  test('resets auto fallback state using initial compact mode override', () => {
    const settings = {
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
      responses_compact_auto_fallback_date: 20260526,
      responses_compact_auto_fallback_reason: 'status_code=404',
    };

    expect(
      resetResponsesCompactAutoFallbackOnModeChange(
        settings,
        RESPONSES_COMPACT_MODE_NATIVE,
        RESPONSES_COMPACT_MODE_AUTO,
      ),
    ).toBe(true);

    expect(settings.responses_compact_auto_fallback_date).toBeUndefined();
    expect(settings.responses_compact_auto_fallback_reason).toBeUndefined();
  });

  test('clears all compact metadata for non OpenAI channels', () => {
    const settings = {
      responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
      responses_compact_auto_fallback_date: 20260526,
      responses_compact_auto_fallback_reason: 'status_code=404',
    };

    clearResponsesCompactSettings(settings);

    expect(settings.responses_compact_mode).toBeUndefined();
    expect(settings.responses_compact_auto_fallback_date).toBeUndefined();
    expect(settings.responses_compact_auto_fallback_reason).toBeUndefined();
  });

  test('renders a single compact field label in channel advanced settings', () => {
    const source = readFileSync(
      resolve(
        currentDir,
        '../src/components/table/channels/modals/EditChannelModal.jsx',
      ),
      'utf8',
    );

    expect(source.match(/t\('Responses Compact 能力'\)/g) ?? []).toHaveLength(
      1,
    );
  });
});
