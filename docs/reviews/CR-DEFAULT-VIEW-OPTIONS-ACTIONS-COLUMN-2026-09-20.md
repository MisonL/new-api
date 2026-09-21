# CR: Default 视图选项放宽过滤后暴露行操作列

- 日期：2026-09-20
- 范围：`web/default/src/features/{users,keys,redemption-codes}/**/*-columns.tsx`
- 状态：已修复并验证

## 问题

`web/default/src/components/data-table/view-options.tsx` 有一处 dev 分支未提交改动，
把列可见性候选的过滤条件放宽了：

```diff
-          .filter(
-            (column) =>
-              typeof column.accessorFn !== 'undefined' && column.getCanHide()
-          )
+          .filter((column) => column.getCanHide())
```

原条件要求列必须有 `accessorFn` 才出现在"切换列"菜单里。TanStack Table 的
`getCanHide()` 只取决于 `enableHiding !== false`，独立于 `accessorFn`；因此去掉
`accessorFn` 判断后，所有没有写 `enableHiding: false`、且本身不是数据访问列的列
（典型是行操作列）都会新出现在菜单中。

"操作"列是行操作入口，隐藏后该行无法执行任何操作，不应可隐藏。仓库既有约定正是
用 `enableHiding: false` 标记这类功能必需列：

| 文件 | 行 | 状态 |
| --- | --- | --- |
| `usage-logs/components/columns/common-logs-columns.tsx` | 1472 | 已有 `enableHiding: false` |
| `models/components/models-columns.tsx` | 594 | 已有 |
| `models/components/deployments-columns.tsx` | 282 | 已有 |
| `models/components/dialogs/upstream-conflict-dialog.tsx` | 231 | 已有 |
| `system-settings/models/model-ratio-visual-editor.tsx` | 528 | 已有 |
| `channels/components/channels-columns.tsx` | 1243 | 已有 |

放宽后在菜单里新暴露且未加守卫的操作列有 3 个。`common-logs-columns.tsx` 的操作列
在 1409 行定义、1472 行设置守卫，间隔 63 行，未受影响。

## 判定依据：哪些表格真的会渲染该菜单

`DataTableViewOptions` 由 `DataTableToolbar` 渲染。实际使用它的表格为 8 个：
`models-table`、`deployments-table`、`model-ratio-visual-editor`、`users-table`、
`api-keys-table`、`redemptions-table`、`channels-table`、`usage-logs-table`。

以下两处虽有 `id: 'actions'` 列，但不渲染该菜单，本次未改动：

- `subscriptions/components/subscriptions-columns.tsx`：其表格未使用 `DataTableToolbar`。
- `channels/components/dialogs/channel-test-dialog.tsx`：未使用。

两者的列定义同样未加 `enableHiding: false`，属于既有状态，不在本次修复范围。

## 修复

给受影响的 3 个操作列补 `enableHiding: false`，与仓库既有约定一致：

| 文件 | 行 |
| --- | --- |
| `features/users/components/users-columns.tsx` | 357 |
| `features/keys/components/api-keys-columns.tsx` | 307 |
| `features/redemption-codes/components/redemptions-columns.tsx` | 248 |

改动合计 3 行新增，未触及其它列定义，也未回退 `view-options.tsx` 中那处未提交改动
（该改动属于 dev 分支既有工作，不是本次任务范围）。

## 验证

命令均在本轮运行并读取输出。

| 命令 | 结果 |
|---|---|
| `cd web/default && bun run typecheck` | exit 0 |
| `cd web/default && bun run lint` | exit 0 |
| `cd web/default && bun test` | 220 pass, 0 fail |
| `cd web/default && bun run build` | exit 0，`Safari compatibility check passed for dist` |

修复后守卫状态复核：`grep -rl "id: 'actions'" src/` 命中的 11 处里，9 处
（含本次 3 处）为 `enableHiding: false`；剩余 2 处为上文列出的两张不渲染菜单的表格。
`common-logs-columns.tsx` 的操作列守卫位于块尾，确认存在。

## 边界

- 仓库当前没有覆盖 `view-options.tsx`、`enableHiding` 或列可见性的测试
  （已确认 `src/` 下无相关 `*.test.*`）。本修复只有静态与构建验证，
  **未做浏览器交互验证**：即未实际打开菜单确认操作列不再出现。
- `view-options.tsx` 那处过滤放宽本身未改动，其是否应恢复 `accessorFn` 判断需单独评估
  （恢复即回归原行为，会让非数据列全部不再可选）。
- 未覆盖隐藏操作列后表格布局、移动端 `mobileBadge` / `mobileHidden` 行为。
