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

const modalSource = readFileSync(
  new URL(
    '../src/components/table/channels/modals/EditChannelModal.jsx',
    import.meta.url,
  ),
  'utf8',
);
const localeNames = ['en', 'fr', 'ja', 'ru', 'vi', 'zh-CN', 'zh-TW'];
const sourceKey =
  '启用 Codex Responses 流式推理截断续写折叠。默认关闭，仅作用于 Codex SSE，不作用于 WebSocket 或 compact。';

describe('classic Codex folded SSE channel setting', () => {
  test('guards display, advanced auto-open, read and save by Codex type', () => {
    expect(modalSource).toContain('{inputs.type === 57 && (');
    expect(modalSource).toContain(
      '(data.type === 57 && data.codex_folded_sse_enabled)',
    );
    expect(modalSource).toContain('data.type === 57 &&');
    expect(modalSource).toContain(
      'parsedSettings.codex_folded_sse_enabled === true',
    );
    expect(modalSource).toContain('if (localInputs.type === 57) {');
    expect(modalSource).toContain('delete settings.codex_folded_sse_enabled;');
  });

  test('ships the folded SSE label and description in every loaded locale', () => {
    for (const locale of localeNames) {
      const { translation } = JSON.parse(
        readFileSync(
          new URL(`../src/i18n/locales/${locale}.json`, import.meta.url),
          'utf8',
        ),
      );
      expect(typeof translation['Codex folded SSE continuation']).toBe(
        'string',
      );
      expect(translation['Codex folded SSE continuation'].trim()).not.toBe('');
      expect(typeof translation[sourceKey]).toBe('string');
      expect(translation[sourceKey].trim()).not.toBe('');
    }
  });
});
