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

import globals from 'globals';
import header from './header-plugin-compat.mjs';
import reactHooks from 'eslint-plugin-react-hooks';

// 从 .eslintrc.cjs 平移而来的 flat config，规则集保持一致：
// 仅启用 header/header 与 no-multiple-empty-lines，react-hooks 只注册不启用。
// eslint 10 起不再读取 .eslintrc/.eslintignore。
export default [
  {
    ignores: ['dist/', 'coverage/'],
  },
  {
    files: ['**/*.{js,jsx}'],
    languageOptions: {
      ecmaVersion: 2020,
      sourceType: 'module',
      parserOptions: { ecmaFeatures: { jsx: true } },
      globals: {
        ...globals.browser,
        ...globals.es2021,
        ...globals.node,
      },
    },
    plugins: {
      header,
      'react-hooks': reactHooks,
    },
    rules: {
      'header/header': [
        2,
        'block',
        [
          '',
          'Copyright (C) 2025 QuantumNous',
          '',
          'This program is free software: you can redistribute it and/or modify',
          'it under the terms of the GNU Affero General Public License as',
          'published by the Free Software Foundation, either version 3 of the',
          'License, or (at your option) any later version.',
          '',
          'This program is distributed in the hope that it will be useful,',
          'but WITHOUT ANY WARRANTY; without even the implied warranty of',
          'MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the',
          'GNU Affero General Public License for more details.',
          '',
          'You should have received a copy of the GNU Affero General Public License',
          'along with this program. If not, see <https://www.gnu.org/licenses/>.',
          '',
          'For commercial licensing, please contact support@quantumnous.com',
          '',
        ],
      ],
      'no-multiple-empty-lines': ['error', { max: 1 }],
    },
  },
  {
    // Node 测试文件是 ESM 且使用顶层 await；src 为浏览器兼容性锁定
    // ecmaVersion 2020，无法解析顶层 await，这里对 tests 放开。
    files: ['tests/**/*.js'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
    },
  },
];
