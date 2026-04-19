import test from 'node:test';
import assert from 'node:assert/strict';

import {
  getManageOperatorEntryDescriptor,
  getTopupAuditEntryDescriptors,
  shouldShowLogIp,
} from '../src/hooks/usage-logs/logAuditInfo.js';

const t = (value) => value;

test('充值日志审计字段仅对管理员返回并保留关键字段顺序', () => {
  const entries = getTopupAuditEntryDescriptors({
    isAdminUser: true,
    logType: 1,
    adminInfo: {
      payment_method: 'stripe',
      callback_payment_method: 'stripe',
      caller_ip: '198.51.100.20',
      server_ip: '10.0.0.12',
      node_name: 'node-a',
      version: 'v1.0.0',
    },
    t,
  });

  assert.deepEqual(entries, [
    { key: '订单支付方式', value: 'stripe', warning: false },
    { key: '回调支付方式', value: 'stripe', warning: false },
    { key: '回调调用者IP', value: '198.51.100.20', warning: false },
    { key: '服务器IP', value: '10.0.0.12', warning: false },
    { key: '节点名称', value: 'node-a', warning: false },
    { key: '系统版本', value: 'v1.0.0', warning: false },
  ]);
});

test('旧版充值日志对管理员返回升级警告', () => {
  const entries = getTopupAuditEntryDescriptors({
    isAdminUser: true,
    logType: 1,
    adminInfo: null,
    t,
  });

  assert.equal(entries.length, 1);
  assert.equal(entries[0].key, '审计信息');
  assert.equal(entries[0].warning, true);
  assert.match(entries[0].value, /旧版本实例写入/);
});

test('普通用户看不到充值审计字段', () => {
  const entries = getTopupAuditEntryDescriptors({
    isAdminUser: false,
    logType: 1,
    adminInfo: {
      payment_method: 'stripe',
    },
    t,
  });

  assert.deepEqual(entries, []);
});

test('管理日志操作人展示优先使用用户名与ID组合', () => {
  assert.deepEqual(
    getManageOperatorEntryDescriptor({
      isAdminUser: true,
      logType: 3,
      adminInfo: {
        admin_username: 'root-admin',
        admin_id: 7,
      },
      t,
    }),
    {
      key: '操作管理员',
      value: 'root-admin (ID: 7)',
    },
  );

  assert.deepEqual(
    getManageOperatorEntryDescriptor({
      isAdminUser: true,
      logType: 3,
      adminInfo: {
        admin_id: 9,
      },
      t,
    }),
    {
      key: '操作管理员',
      value: 'ID: 9',
    },
  );
});

test('IP 列展示逻辑允许管理员查看充值日志IP，普通用户不可见', () => {
  assert.equal(
    shouldShowLogIp({
      isAdminUser: true,
      recordType: 1,
      ip: '198.51.100.9',
    }),
    true,
  );
  assert.equal(
    shouldShowLogIp({
      isAdminUser: false,
      recordType: 1,
      ip: '198.51.100.9',
    }),
    false,
  );
  assert.equal(
    shouldShowLogIp({
      isAdminUser: false,
      recordType: 2,
      ip: '198.51.100.9',
    }),
    true,
  );
  assert.equal(
    shouldShowLogIp({
      isAdminUser: true,
      recordType: 5,
      ip: '',
    }),
    false,
  );
});
