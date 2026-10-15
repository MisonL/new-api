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

const translationKeys = [
  '清理 Codex 加密上下文',
  '转发 Responses 请求前移除加密推理上下文。',
  '渠道请求体大小限制',
  '请求体大小限制（字节）',
  '设为 0 删除渠道限制；启用全局防护后，上游 413 响应可以自动更新该值。',
];

describe('classic channel advanced settings parity', () => {
  test('reads, displays, serializes and removes the new channel settings', () => {
    expect(modalSource).toContain('strip_codex_encrypted_context: false');
    expect(modalSource).toContain(
      'parsedSettings.strip_codex_encrypted_context === true',
    );
    expect(modalSource).toContain("field='strip_codex_encrypted_context'");
    expect(modalSource).toContain('settings.strip_codex_encrypted_context =');
    expect(modalSource).toContain(
      'delete settings.strip_codex_encrypted_context;',
    );
    expect(modalSource).toContain('request_body_limit_max_bytes: 0');
    expect(modalSource).toContain('parsedSettings.request_body_limit');
    expect(modalSource).toContain('settings.request_body_limit = {');
    expect(modalSource).toContain('delete settings.request_body_limit;');
    expect(modalSource).toContain("field='request_body_limit_max_bytes'");
  });

  test('ships labels and descriptions in every loaded locale', () => {
    for (const locale of localeNames) {
      const { translation } = JSON.parse(
        readFileSync(
          new URL(`../src/i18n/locales/${locale}.json`, import.meta.url),
          'utf8',
        ),
      );
      for (const key of translationKeys) {
        expect(typeof translation[key]).toBe('string');
        expect(translation[key].trim()).not.toBe('');
      }
    }
  });
});
