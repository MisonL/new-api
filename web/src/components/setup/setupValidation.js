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

export const ADMIN_USERNAME_MAX_LENGTH = 20;
export const ADMIN_PASSWORD_MIN_LENGTH = 8;

function getUnicodeCharacterCount(value) {
  return Array.from(value || '').length;
}

export function normalizeSetupFormValues(values) {
  return {
    ...values,
    username: typeof values.username === 'string' ? values.username.trim() : '',
  };
}

export function validateAdminSetupValues(values) {
  const normalized = normalizeSetupFormValues(values);

  if (!normalized.username) {
    return { key: '请输入管理员用户名' };
  }
  if (getUnicodeCharacterCount(normalized.username) > ADMIN_USERNAME_MAX_LENGTH) {
    return {
      key: '用户名长度不能超过{{max}}个字符',
      params: { max: ADMIN_USERNAME_MAX_LENGTH },
    };
  }
  if (
    !normalized.password ||
    normalized.password.length < ADMIN_PASSWORD_MIN_LENGTH
  ) {
    return { key: '密码长度至少为8个字符' };
  }
  if (normalized.password !== normalized.confirmPassword) {
    return { key: '两次输入的密码不一致' };
  }

  return null;
}
