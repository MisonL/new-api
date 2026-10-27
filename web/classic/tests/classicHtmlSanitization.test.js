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

// classic 前端的所有 HTML 注入点都渲染管理员可配置内容（关于、首页、页脚、
// 公告、公告板、FAQ、文档页）或 marked 解析出的 Markdown。DOMPurify 需要
// 真实 DOM，bun test 里没有，所以这里用源码守卫锁住接线：每个
// dangerouslySetInnerHTML 的 __html 都必须经过 sanitizeHtml。
const HTML_SINKS = [
  'src/pages/About/index.jsx',
  'src/pages/Home/index.jsx',
  'src/components/layout/Footer.jsx',
  'src/components/layout/NoticeModal.jsx',
  'src/components/dashboard/AnnouncementsPanel.jsx',
  'src/components/dashboard/FaqPanel.jsx',
  'src/components/settings/OtherSetting.jsx',
  'src/components/common/DocumentRenderer/index.jsx',
  'src/helpers/utils.jsx',
];

// CodeViewer 例外：它的内容先经 escapeHtml/highlightJson 转义再 linkify，
// 不进 DOM 的是已经转义过的文本，不是管理员配置的 HTML。
const PRE_ESCAPED_SINKS = ['src/components/playground/CodeViewer.jsx'];

const read = (relative) =>
  readFileSync(new URL(`../${relative}`, import.meta.url), 'utf8');

describe('classic HTML injection sinks', () => {
  test('every admin-content sink renders through sanitizeHtml', () => {
    for (const file of HTML_SINKS) {
      const source = read(file);
      const sinks = source.match(/dangerouslySetInnerHTML/g) || [];
      expect(sinks.length).toBeGreaterThan(0);
      // __html 可以是 sanitizeHtml(...) 的直接调用，也可以是此前已用
      // sanitizeHtml 处理过的局部变量（例如 marked.parse 的结果先消毒）。
      const direct = source.match(/__html:\s*sanitize(?:Admin)?Html\(/g) || [];
      const indirect = [
        ...source.matchAll(/__html:\s*([A-Za-z_$][\w$]*)/g),
      ].filter(([, name]) =>
        new RegExp(
          `(?:const|let|var)\\s+${name}\\s*=[^;]*sanitizeHtml\\(`,
        ).test(source),
      );
      expect(direct.length + indirect.length).toBe(sinks.length);
    }
  });

  test('no sink renders a marked.parse result directly', () => {
    for (const file of HTML_SINKS) {
      const source = read(file);
      const direct = source.match(/__html:\s*marked\.parse\(/g) || [];
      expect(direct.length).toBe(0);
    }
  });

  test('sanitizer is imported wherever a sink uses it', () => {
    for (const file of HTML_SINKS) {
      const source = read(file);
      if (!source.includes('sanitizeHtml(')) continue;
      expect(source).toMatch(
        /import\s*\{\s*sanitizeHtml\s*\}\s*from\s*'[^']*sanitizeHtml'/,
      );
    }
  });

  test('pre-escaped code viewer is not treated as an admin HTML sink', () => {
    for (const file of PRE_ESCAPED_SINKS) {
      const source = read(file);
      expect(source.includes('escapeHtml(')).toBe(true);
    }
  });
});
