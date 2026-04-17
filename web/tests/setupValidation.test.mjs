import test from 'node:test';
import assert from 'node:assert/strict';

import {
  ADMIN_USERNAME_MAX_LENGTH,
  normalizeSetupFormValues,
  validateAdminSetupValues,
} from '../src/components/setup/setupValidation.js';

test('normalizeSetupFormValues trims username only', () => {
  assert.deepEqual(
    normalizeSetupFormValues({
      username: '  deskadmin  ',
      password: 'DesktopTest123!',
      confirmPassword: 'DesktopTest123!',
    }),
    {
      username: 'deskadmin',
      password: 'DesktopTest123!',
      confirmPassword: 'DesktopTest123!',
    },
  );
});

test('validateAdminSetupValues accepts max length username', () => {
  assert.equal(
    validateAdminSetupValues({
      username: 'a'.repeat(ADMIN_USERNAME_MAX_LENGTH),
      password: 'DesktopTest123!',
      confirmPassword: 'DesktopTest123!',
    }),
    null,
  );
});

test('validateAdminSetupValues rejects username longer than max length', () => {
  assert.deepEqual(
    validateAdminSetupValues({
      username: 'a'.repeat(ADMIN_USERNAME_MAX_LENGTH + 1),
      password: 'DesktopTest123!',
      confirmPassword: 'DesktopTest123!',
    }),
    {
      key: '用户名长度不能超过{{max}}个字符',
      params: { max: ADMIN_USERNAME_MAX_LENGTH },
    },
  );
});

test('validateAdminSetupValues rejects short password and mismatched passwords', () => {
  assert.deepEqual(
    validateAdminSetupValues({
      username: 'deskadmin',
      password: 'short',
      confirmPassword: 'short',
    }),
    {
      key: '密码长度至少为8个字符',
    },
  );

  assert.deepEqual(
    validateAdminSetupValues({
      username: 'deskadmin',
      password: 'DesktopTest123!',
      confirmPassword: 'DesktopTest123',
    }),
    {
      key: '两次输入的密码不一致',
    },
  );
});
