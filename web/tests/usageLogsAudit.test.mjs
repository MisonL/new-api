import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  getManageOperatorEntryDescriptor,
  getTopupAuditEntryDescriptors,
  shouldShowLogIp,
} from "../classic/src/hooks/usage-logs/logAuditInfo.js";
import { buildRequestHeaderAuditLines } from "../classic/src/hooks/usage-logs/headerAuditInfo.js";

const t = (value) => value;
const testDir = path.dirname(fileURLToPath(import.meta.url));

test("充值日志审计字段仅对管理员返回并保留关键字段顺序", () => {
  const entries = getTopupAuditEntryDescriptors({
    isAdminUser: true,
    logType: 1,
    adminInfo: {
      payment_method: "stripe",
      callback_payment_method: "stripe",
      caller_ip: "198.51.100.20",
      server_ip: "10.0.0.12",
      node_name: "node-a",
      version: "v1.0.0",
    },
    t,
  });

  assert.deepEqual(entries, [
    { key: "订单支付方式", value: "stripe", warning: false },
    { key: "回调支付方式", value: "stripe", warning: false },
    { key: "回调调用者IP", value: "198.51.100.20", warning: false },
    { key: "服务器IP", value: "10.0.0.12", warning: false },
    { key: "节点名称", value: "node-a", warning: false },
    { key: "系统版本", value: "v1.0.0", warning: false },
  ]);
});

test("旧版充值日志对管理员返回升级警告", () => {
  const entries = getTopupAuditEntryDescriptors({
    isAdminUser: true,
    logType: 1,
    adminInfo: null,
    t,
  });

  assert.equal(entries.length, 1);
  assert.equal(entries[0].key, "审计信息");
  assert.equal(entries[0].warning, true);
  assert.match(entries[0].value, /旧版本实例写入/);
});

test("普通用户看不到充值审计字段", () => {
  const entries = getTopupAuditEntryDescriptors({
    isAdminUser: false,
    logType: 1,
    adminInfo: {
      payment_method: "stripe",
    },
    t,
  });

  assert.deepEqual(entries, []);
});

test("管理日志操作人展示优先使用用户名与ID组合", () => {
  assert.deepEqual(
    getManageOperatorEntryDescriptor({
      isAdminUser: true,
      logType: 3,
      adminInfo: {
        admin_username: "root-admin",
        admin_id: 7,
      },
      t,
    }),
    {
      key: "操作管理员",
      value: "root-admin (ID: 7)",
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
      key: "操作管理员",
      value: "ID: 9",
    },
  );
});

test("IP 列展示逻辑允许管理员查看充值日志IP，普通用户不可见", () => {
  assert.equal(
    shouldShowLogIp({
      isAdminUser: true,
      recordType: 1,
      ip: "198.51.100.9",
    }),
    true,
  );
  assert.equal(
    shouldShowLogIp({
      isAdminUser: false,
      recordType: 1,
      ip: "198.51.100.9",
    }),
    false,
  );
  assert.equal(
    shouldShowLogIp({
      isAdminUser: false,
      recordType: 2,
      ip: "198.51.100.9",
    }),
    true,
  );
  assert.equal(
    shouldShowLogIp({
      isAdminUser: true,
      recordType: 5,
      ip: "",
    }),
    false,
  );
});

test("请求头审计气泡按列范围展示不同内容", () => {
  const policy = {
    mode: "merge",
    header_profile_id: "codex-cli",
    applied_user_agent: "codex-tui/0.128.0",
    applied_header_keys: ["User-Agent", "originator", "x-codex-window-id"],
  };

  assert.deepEqual(
    buildRequestHeaderAuditLines(policy, "user-agent", t).map(
      ({ key, value }) => [key, value],
    ),
    [
      ["mode", "合并"],
      ["profile", "codex-cli"],
      ["user-agent", "codex-tui/0.128.0"],
    ],
  );

  assert.deepEqual(
    buildRequestHeaderAuditLines(policy, "headers", t).map(({ key, value }) => [
      key,
      value,
    ]),
    [
      ["mode", "合并"],
      ["profile", "codex-cli"],
      ["headers", "originator, x-codex-window-id"],
    ],
  );
});

test("请求头审计气泡优先展示应用请求头键值", () => {
  const policy = {
    mode: "merge",
    header_profile_id: "codex-cli",
    applied_header_keys: ["originator", "x-codex-window-id"],
    applied_headers: [
      { key: "originator", value: "codex-tui" },
      { key: "x-codex-window-id", value: "window-123" },
    ],
  };

  const headersLine = buildRequestHeaderAuditLines(policy, "headers", t).find(
    ({ key }) => key === "headers",
  );

  assert.equal(
    headersLine.value,
    "originator: codex-tui\nx-codex-window-id: window-123",
  );
  assert.deepEqual(headersLine.items, [
    { key: "originator", value: "codex-tui" },
    { key: "x-codex-window-id", value: "window-123" },
  ]);
});

test("请求头审计气泡兼容旧日志应用请求头键列表", () => {
  const policy = {
    mode: "merge",
    applied_header_keys: ["originator", "x-codex-window-id"],
  };

  const headersLine = buildRequestHeaderAuditLines(policy, "headers", t).find(
    ({ key }) => key === "headers",
  );

  assert.equal(headersLine.value, "originator, x-codex-window-id");
  assert.deepEqual(headersLine.items, [
    { key: "originator", value: "" },
    { key: "x-codex-window-id", value: "" },
  ]);
});

test("请求头审计气泡过滤空键并按大小写去重键值", () => {
  const policy = {
    applied_headers: [
      { key: "", value: "empty" },
      { key: "Originator", value: "codex-tui" },
      { key: "originator", value: "duplicate" },
      { key: "x-codex-window-id", value: "window-123" },
    ],
  };

  const headersLine = buildRequestHeaderAuditLines(policy, "headers", t).find(
    ({ key }) => key === "headers",
  );

  assert.equal(
    headersLine.value,
    "Originator: codex-tui\nx-codex-window-id: window-123",
  );
  assert.deepEqual(headersLine.items, [
    { key: "Originator", value: "codex-tui" },
    { key: "x-codex-window-id", value: "window-123" },
  ]);
});

test("渠道信息气泡外层宽度与内容宽度保持一致", () => {
  const columnSource = fs.readFileSync(
    path.join(
      testDir,
      "../classic/src/components/table/usage-logs/UsageLogsColumnDefs.jsx",
    ),
    "utf8",
  );
  const cssSource = fs.readFileSync(
    path.join(testDir, "../classic/src/index.css"),
    "utf8",
  );

  assert.match(
    columnSource,
    /<Tooltip[\s\S]{0,240}className=["']usage-log-channel-tooltip-popover["']/,
  );
  assert.match(
    cssSource,
    /\.usage-log-channel-tooltip-popover\.semi-tooltip-wrapper\s*\{[\s\S]*?width:\s*min\(420px,\s*calc\(100vw\s*-\s*32px\)\)/,
  );
  assert.match(
    cssSource,
    /\.usage-log-channel-tooltip-popover\s+\.semi-tooltip-content\s*\{[\s\S]*?width:\s*100%/,
  );
  assert.match(
    cssSource,
    /\.usage-log-channel-tooltip\s*\{[\s\S]*?width:\s*100%/,
  );
});

test("请求头审计气泡安全处理空请求头和空策略", () => {
  assert.deepEqual(
    buildRequestHeaderAuditLines(
      {
        mode: "merge",
        header_profile_id: "test",
        applied_header_keys: [],
      },
      "headers",
      t,
    ).map(({ key, value }) => [key, value]),
    [
      ["mode", "合并"],
      ["profile", "test"],
    ],
  );

  assert.deepEqual(buildRequestHeaderAuditLines(null, "headers", t), []);
  assert.deepEqual(buildRequestHeaderAuditLines(undefined, "headers", t), []);
});

test("请求头审计气泡保留未知策略模式原值", () => {
  assert.deepEqual(
    buildRequestHeaderAuditLines(
      {
        mode: "override",
        applied_header_keys: ["originator"],
      },
      "headers",
      t,
    ).map(({ key, value }) => [key, value]),
    [
      ["mode", "override"],
      ["headers", "originator"],
    ],
  );
});
