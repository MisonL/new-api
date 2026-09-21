# CR: 隐藏列持久化会永久隐藏必需的行操作列

- 日期：2026-09-20
- 范围：`web/default/src/hooks/use-persisted-table-visibility.ts`、`web/default/src/features/{users,keys,redemption-codes}/**/*-table.tsx`
- 状态：已修复并验证

## 问题

承接 `CR-DEFAULT-VIEW-OPTIONS-ACTIONS-COLUMN-2026-09-20.md`。那处修复给 3 个行操作列补了
`enableHiding: false`，但只挡住了"从菜单里隐藏"这一条路径，没有挡住"已经隐藏过"的状态。

`use-persisted-table-visibility.ts` 从 localStorage 读取列可见性后直接合并：

```ts
return { ...defaults, ...(parsed as VisibilityState) }
```

而 TanStack Table 的 `getIsVisible` 只读 `columnVisibility` 状态，**不查 `getCanHide`**：

```js
column.getIsVisible = () => {
  const childColumns = column.columns;
  return childColumns.length
    ? childColumns.some(c => c.getIsVisible())
    : table.getState().columnVisibility?.[column.id] ?? true;
};
column.getCanHide = () =>
  (column.columnDef.enableHiding ?? true) && (table.options.enableHiding ?? true);
```

只有 `toggleVisibility` 会检查 `getCanHide`：

```js
column.toggleVisibility = value => {
  if (column.getCanHide()) { ... }
};
```

因此在 `view-options.tsx` 那处未提交改动把操作列放进"切换列"菜单之后，用户只要隐藏过一次
操作列，`{"actions": false}` 就会被写入 localStorage。此后：

- 菜单里操作列消失（`getCanHide()` 为 false，不再被渲染）；
- 表格里操作列也不显示（`getIsVisible()` 读到的持久化值优先）；
- 用户没有任何入口把它恢复。

即"隐藏一次，永久丢失行操作"，且无法自助恢复。

## 修复

1. 抽出纯函数 `mergeVisibility(defaults, persisted, requiredVisible)`，对必需列强制可见：

```ts
const merged: VisibilityState = { ...defaults, ...(persisted as VisibilityState) }
for (const key of requiredVisible) {
  merged[key] = true
}
return merged
```

该函数同时保留既有的畸形输入处理（`null`、字符串、数组一律回退到 defaults）。

2. `usePersistedTableVisibility` 增加第三个参数 `requiredVisible`。为免调用方传内联数组导致
   每次渲染重算，用 `join(',')` 归一到稳定原始值。

3. 三张会渲染该菜单的表格传入 `['actions']`：`users-table`、`api-keys-table`、
   `redemptions-table`。

## 验证

命令均在本轮运行并读取输出。

| 命令 | 结果 |
|---|---|
| `cd web/default && bun test src/hooks/use-persisted-table-visibility.test.ts` | 5 pass, 0 fail |
| `cd web/default && bun test` | 225 pass, 0 fail |
| `cd web/default && bun run typecheck` | exit 0 |
| `cd web/default && bun run lint` | exit 0 |
| `cd web/default && bun run build` | exit 0，`Safari compatibility check passed for dist` |
| `bun x prettier --check`（两个新文件） | All matched files use Prettier code style |

新增测试 `src/hooks/use-persisted-table-visibility.test.ts`（沿用仓库既有 `node:test` +
`node:assert/strict` 纯函数测试约定，非 DOM 测试）：

- 持久化值合并到 defaults
- 同名列以持久化值为准
- **必需列在被持久化隐藏时仍可见**（本次缺陷的回归用例）
- 必需列在两个输入都不存在时仍可见
- 畸形持久化载荷（`null` / 字符串 / 数组）回退到 defaults

变异验证：撤掉 `requiredVisible` 的强制赋值（即恢复修复前行为），
`bun test` 报 2 个失败（`required columns stay visible...` 与
`required columns are visible even when absent...`），确认测试确实覆盖该缺陷。

## 浏览器验证

- 通过 `web/scripts/browser-cross-verify.mjs` 对 `http://127.0.0.1:5176/`（`web/default`
  开发服务器，代理到隔离 `3001` 后端）做真实渲染检查：
  chromium / firefox / webkit × desktop / mobile 共 6 组，全部 `hasConsoleText=true`、
  无渲染错误、`consoleErrors=0`、`pageErrors=0`、`failedRequests=0`。
- 未能完成"登录后在真实表格里注入 stale visibility 并观察操作列"这一步：隔离开发库里
  管理员账号密码未知，本次没有尝试猜测口令。因此该缺陷的**浏览器端到端确认未完成**，
  覆盖到的是纯函数语义 + 构建 + 无错渲染。

## 边界

- `mergeVisibility` 的 `requiredVisible` 需要调用方显式传入列 id。新增必需列时若忘记传参，
  该列仍可被持久化隐藏；本修复没有做成"从列定义自动推导"，因为该 hook 只持有可见性状态、
  拿不到列定义。
- 只处理了 `actions` 一个必需列 id。其它 `enableHiding: false` 的列（如各表的 `select`
  列）不在"切换列"菜单里，但仍可能被旧持久化数据隐藏；本次未一并纳入，避免扩大范围。
- 未清理用户已写入的 `{"actions": false}` 持久化数据：修复后读取时会被覆盖为 `true`，
  但 localStorage 中的旧值保留到下一次写入。
