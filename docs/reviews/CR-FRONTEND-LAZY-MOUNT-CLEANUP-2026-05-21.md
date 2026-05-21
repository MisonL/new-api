# CR-FRONTEND-LAZY-MOUNT-CLEANUP-2026-05-21

## 范围

采样时间：2026-05-21。

本次收尾覆盖前端弹窗按需挂载、关闭动画保留、表格分页去重触发、usage logs 状态整理和格式门禁清理：

- `web/default`：为 Dialog、Sheet、AlertDialog 类入口接入延迟卸载，减少关闭状态下的常驻弹窗树，同时保留关闭动画期间所需上下文。
- `web/default`：补充 `LazyMount` 状态辅助函数和单元测试，锁定打开立即渲染、关闭延迟卸载的基础契约。
- `web/default`：分页控件避免重复提交当前页或当前 page size，减少无意义 URL 和表格状态更新。
- `web/classic`：整理 usage logs 数据 hook 的请求去重、展开数据缓存和格式化逻辑。
- `web/classic` 与 `web/default`：统一 Prettier 格式，清理既有格式门禁阻塞点。

## 关键结论

- `LazyMount` 默认关闭保留时间为 320ms，用于覆盖 Radix 弹窗退出动画；打开路径仍立即渲染，不等待 timer。
- 对 dashboard drilldown、models 描述、deployments 操作和用户额度弹窗这类关闭期间依赖当前行数据的入口，已使用保留值避免关闭动画期间 props 提前变成 `null` 或 `undefined`。
- `web/default` 新增 `tests/lazy-mount-state.test.ts`，覆盖延迟挂载核心状态判断。
- classic 全量 Prettier 门禁已恢复为通过；此前阻塞点是 `SystemSetting.jsx` 与 `EditTokenModal.jsx` 的格式问题。

## 验证记录

### web/default

```bash
cd web/default && bun test tests/lazy-mount-state.test.ts tests/data-table-pagination.test.ts tests/combobox-state.test.ts tests/protocol-conversion-policy-utils.test.ts
```

结果：通过，22 pass，0 fail。

```bash
cd web/default && node --test ../tests/usageLogsAudit.test.mjs ../tests/usageLogsColumnPreferences.test.mjs ../tests/defaultDashboardDrilldown.test.mjs
```

结果：通过，46 pass，0 fail。

```bash
cd web/default && bun run format:check
cd web/default && bun run lint
cd web/default && bun run typecheck
cd web/default && bun run build
```

结果：全部通过。

### web/classic

```bash
cd web/classic && bun run lint
cd web/classic && bun run eslint
cd web/classic && bun run build
```

结果：全部通过。

### 工作区检查

```bash
git diff --check
```

结果：通过。

## 边界

- 本次改动仅覆盖前端代码、前端测试和审计文档，未修改后端 Go、数据库迁移或渠道配置。
- 本次验证不代表生产 `3000` 栈已变更。
- 本次未执行浏览器人工复验；自动化覆盖了核心状态辅助函数、usage logs 审计/列偏好、dashboard drilldown、lint、typecheck 和 build。
