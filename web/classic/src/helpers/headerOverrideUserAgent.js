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

export function normalizeHeaderTemplateContent(rawValue, options = {}) {
  const allowEmpty = options.allowEmpty !== false;
  const trimmed = typeof rawValue === 'string' ? rawValue.trim() : '';

  if (!trimmed) {
    if (allowEmpty) {
      return {
        ok: true,
        value: '',
      };
    }
    return {
      ok: false,
      message: '模板内容不能为空！',
    };
  }

  let parsed;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return {
      ok: false,
      message: '请求头覆盖必须是合法的 JSON 格式！',
    };
  }

  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    return {
      ok: false,
      message: '请求头覆盖必须是 JSON 对象！',
    };
  }

  for (const [key, value] of Object.entries(parsed)) {
    if (typeof key !== 'string' || key.trim() === '') {
      return {
        ok: false,
        message: '请求头名称不能为空！',
      };
    }
    if (
      typeof value !== 'string' &&
      typeof value !== 'number' &&
      typeof value !== 'boolean'
    ) {
      return {
        ok: false,
        message: `请求头值类型不受支持: ${key}`,
      };
    }
  }

  return {
    ok: true,
    value: JSON.stringify(parsed, null, 2),
  };
}
