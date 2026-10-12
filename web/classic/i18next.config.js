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

import { defineConfig } from 'i18next-cli';

/** @type {import('i18next-cli').I18nextToolkitConfig} */
export default defineConfig({
  locales: ['zh-CN', 'zh-TW', 'en', 'fr', 'ru', 'ja', 'vi'],
  extract: {
    input: ['src/**/*.{js,jsx,ts,tsx}'],
    ignore: ['src/i18n/**/*'],
    output: 'src/i18n/locales/{{language}}.json',
    ignoredAttributes: [
      'accept',
      'align',
      'aria-label',
      'autoComplete',
      'className',
      'clipRule',
      'color',
      'crossOrigin',
      'dropdownClassName',
      'data-index',
      'data-name',
      'data-testid',
      'data-type',
      'defaultActiveKey',
      'direction',
      'editorType',
      'field',
      'fill',
      'fillRule',
      'height',
      'hoverStyle',
      'htmlType',
      'id',
      'itemKey',
      'key',
      'keyPrefix',
      'layout',
      'margin',
      'maxHeight',
      'mode',
      'name',
      'overflow',
      'placement',
      'placeholder',
      'position',
      'rel',
      'role',
      'rowKey',
      'searchPosition',
      'selectedStyle',
      'shape',
      'size',
      'spacing',
      'style',
      'theme',
      'trigger',
      'uploadTrigger',
      'validateStatus',
      'value',
      'viewBox',
      'width',
      'aria-hidden',
    ],
    // i18next-cli 默认只识别 t(...) 与 *.t(...)。仓内两处给 t 起了防御性别名
    // （CodexUsageModal.jsx 的 tt、headerProfile.helpers.js 与 dashboardDrilldown.js
    // 的 translate，形如 typeof t === 'function' ? t : (v) => v），不在默认集合里时
    // 提取器会静默漏掉这些调用点的键。
    functions: ['t', '*.t', 'tt', 'translate'],
    sort: true,
    disablePlurals: false,
    removeUnusedKeys: false,
    nsSeparator: false,
    keySeparator: false,
    mergeNamespaces: true,
  },
});
