# CR-RESPONSES-ROUTING-OBSERVABILITY-2026-07-24

## Summary

采样时间：2026-07-24 18:04:49 +0800。

本轮验证的目标是让管理员能够从使用日志直接判断 Responses 请求的路由、压缩和恢复行为，而不是只能依赖后端日志推测。实现覆盖 default 与 classic 两套前端，并将能力快照、上游路径和路由诊断限制在管理员视图。

结论：

- default 管理员日志表格增加 `Responses Routing` 列。每条有诊断信息的日志显示最终结果和记录动作数；悬浮后显示按处理阶段归类的执行轨迹、最终路径和动作摘要。
- default 日志详情显示 `Responses Routing` 区域，包括最终结果、最终路径、执行轨迹和能力依据。页面不展示请求正文、密钥或加密推理内容。
- classic 使用日志增加 `Responses 路由` 列；展开行后显示相同的最终路径、执行轨迹和能力依据。
- 普通用户日志不包含该列，也不返回上游路径、能力快照或 `responses_*` 路由诊断字段。
- default 与 classic 的桌面和 390 x 844 小屏验证均通过。小屏直接显示路由结果和动作数，不依赖横向表格列或悬浮操作。

## Environment

- Repository: `<repository-root>`
- Local HEAD: `b36cd3e2367ccbb2e8ca4caea13324057acee9a6`
- `origin/main`: `51074ac3736288ee664966e0a2109e9580d86fcf`
- Isolated app container: `new-api-dev-isolated-new-api-1`
- Isolated app build info: `b36cd3e2367ccbb2e8ca4caea13324057acee9a6-dirty`
- Isolated backend: `http://127.0.0.1:3001`
- default frontend development server used for validation: `http://127.0.0.1:5176` (stopped after validation)
- Production service: not modified

## Browser Evidence

使用隔离数据库中的临时账号和三条复制的既有 Responses 诊断样本进行验收。样本包含：

- `synthetic_summary` 压缩完成，最终上游路径 `/v1/responses`。
- 压缩状态恢复、模型切换和 `previous_response_id` 清理。
- 因原生压缩能力不兼容而跳过渠道的 503 错误。

普通用户验证：

- 访问 default `http://127.0.0.1:5176/usage-logs/common`。
- 页面能显示三条自身日志。
- 页面没有 `Responses Routing` 列、最终路径、能力依据或执行轨迹。
- 浏览器控制台没有与路由日志相关的 warning 或 error。

管理员验证：

- default 桌面表格显示 `Responses 路由`、`路由恢复 4` 和 `合成压缩 0`。
- default 徽标悬浮后显示最终结果、`/v1/responses`、按请求准备/路由选择/上游尝试/恢复归类的轨迹及动作数。
- default 详情弹窗显示 `Responses 路由`、最终路径、执行轨迹和能力依据。
- default 390 x 844 小屏把路由徽标放在日志卡片顶部，未发生重叠或文字裁剪。
- classic 桌面表格显示 `Responses 路由` 列；展开 `路由恢复 4` 行后显示完整诊断详情。
- classic 390 x 844 小屏把 `Responses 路由` 显示为日志卡片的显式字段，包含动作数。

## Automated Verification

```bash
cd web/default && bun test ./tests/usage-logs-routing.test.ts
cd web/classic && bun test tests/usageLogsResponsesCompact.test.js
go test ./model -run 'TestGetUserLogsRedactsResponsesRoutingDiagnostics|TestRecordLogWithAdminInfoKeepsAuditForAdminsOnly' -count=1
git diff --check -- model/log.go model/log_admin_audit_test.go web/default/src/features/usage-logs web/default/tests/usage-logs-routing.test.ts web/default/src/i18n web/classic/src/components/table/usage-logs web/classic/src/helpers/usageLogsResponsesCompact.js web/classic/src/helpers/usageLogsResponsesRoutingTrace.js web/classic/tests/usageLogsResponsesCompact.test.js
cd web/default && bun run lint
cd web/default && bun run build
cd web/classic && bun run build
```

结果：

- default 路由诊断测试：12 pass，0 fail。
- classic 路由诊断测试：15 pass，0 fail。
- 管理员信息与普通用户脱敏测试：通过。
- scoped `git diff --check`：通过。
- default lint：退出码 0；报告 5 条现存的 unused-disable warning，均不在本轮路由日志文件中。
- default build：通过，Safari compatibility check 通过。
- classic build：通过，Safari compatibility check 通过。
- 隔离 `/api/status`：通过。

## Privacy Boundary

- `model.formatUserLogs` 删除 `admin_info`、`upstream_request_path`、能力快照和除 `responses_compact_mode` 之外的 `responses_*` 字段。
- 管理员界面只展示结构化状态、能力布尔值、上游路径和脱敏的输入/体积统计；不展示请求正文、密钥、Cookie 或 encrypted reasoning 内容。
- 浏览器验收使用的临时用户和复制日志只存在于隔离 PostgreSQL，验收后已删除；复核结果为 `routing_qa_20260724_users=0` 和 `routing_qa_20260724_logs=0`。

## Release Boundary

- 本记录证明 default/classic 在隔离开发环境中的展示、权限隔离和构建结果。
- 本记录不证明正式 `13000` 服务已升级，也不替代真实上游 Responses、压缩或工具调用的正式环境门禁。
- 正式升级仍需遵循 Codex Responses Lite 总目标中的完整开发门禁、镜像可追溯和生产观察要求。
