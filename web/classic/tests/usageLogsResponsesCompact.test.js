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

import { shouldRenderResponsesCompactTag } from '../src/helpers/usageLogsResponsesCompact.js';

describe('classic usage logs responses compact tag', () => {
  test('does not render a generic Responses tag for capability snapshots only', () => {
    expect(
      shouldRenderResponsesCompactTag({
        channel_capability_snapshot: {
          compact_mode_effective: 'native',
          source: 'explicit_settings',
        },
      }),
    ).toBe(false);
  });

  test('renders for actual compact records', () => {
    expect(
      shouldRenderResponsesCompactTag({
        responses_compact_mode: 'native',
        channel_capability_snapshot: {
          compact_mode_effective: 'native',
        },
      }),
    ).toBe(true);
  });

  test('renders encrypted context retry diagnostics', () => {
    expect(
      shouldRenderResponsesCompactTag({
        responses_encrypted_context_retry: true,
      }),
    ).toBe(true);
  });

  test('returns false for empty input', () => {
    expect(shouldRenderResponsesCompactTag(undefined)).toBe(false);
    expect(shouldRenderResponsesCompactTag(null)).toBe(false);
    expect(shouldRenderResponsesCompactTag({})).toBe(false);
  });

  test('requires literal true for encrypted context retry diagnostics', () => {
    expect(
      shouldRenderResponsesCompactTag({
        responses_encrypted_context_retry: 'true',
      }),
    ).toBe(false);
    expect(
      shouldRenderResponsesCompactTag({
        responses_encrypted_context_retry: 1,
      }),
    ).toBe(false);
  });
});
