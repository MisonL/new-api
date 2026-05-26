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
  RESPONSES_COMPACT_MODE_DEFAULT,
  RESPONSES_COMPACT_MODE_NATIVE,
  RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
  buildResponsesCompactSettings,
  normalizeResponsesCompactMode,
} from '../src/helpers/responsesCompactSettings.js';

const currentDir = dirname(fileURLToPath(import.meta.url));

describe('classic responses compact settings', () => {
  test('defaults missing compact mode to native', () => {
    expect(RESPONSES_COMPACT_MODE_DEFAULT).toBe(RESPONSES_COMPACT_MODE_NATIVE);
    expect(normalizeResponsesCompactMode(undefined)).toBe(
      RESPONSES_COMPACT_MODE_NATIVE,
    );
    expect(normalizeResponsesCompactMode('')).toBe(
      RESPONSES_COMPACT_MODE_NATIVE,
    );
    expect(normalizeResponsesCompactMode('convert')).toBe(
      RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
    );
    for (const mode of ['auto', 'disabled', 'unsupported', 'unexpected']) {
      expect(normalizeResponsesCompactMode(mode)).toBe(
        RESPONSES_COMPACT_MODE_NATIVE,
      );
    }
  });

  test('keeps synthetic compact mode explicit', () => {
    expect(
      normalizeResponsesCompactMode(RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY),
    ).toBe(RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY);
  });

  test('stores compact mode only for OpenAI channels', () => {
    expect(buildResponsesCompactSettings(1, undefined)).toEqual({
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
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
