# CR-RESPONSES-COMPACT-RECHECK-2026-05-24

## 范围

本记录覆盖 Responses Compact 三态重构后的复检。当前目标状态为：

- `responses_compact_mode` 支持 `convert`、`native`、`disabled`。
- OpenAI 渠道默认 `convert`，将 `/v1/responses/compact` 上游路径转换为 `/v1/responses`。
- `convert` 保留 `previous_response_id`，移除不能安全转发到普通 Responses 的 compact 输入项。
- `native` 保持 `/v1/responses/compact` 原生直通。
- `disabled` 显式拒绝 compact 请求。

## 已修复问题

- 转换体过滤绕过：当全局或渠道开启请求体透传时，compact `convert` 会强制进入转换分支，但后续禁用字段过滤仍把透传开关当作跳过条件。已改为只在本次真实请求体透传时跳过过滤，转换体继续过滤 `service_tier`、`safety_identifier`、`stream_options.include_obfuscation` 等默认禁用字段。
- 动态 i18n 缺口：渠道列表的 `Compact Convert`、`Compact Disabled`、`Compact disabled` 通过动态 key 调用 `t()`，不会被同步脚本从静态调用中发现。已补齐 `en`、`zh`、`fr`、`ja`、`ru`、`vi`，并增加测试锁定这些运行时 key。
- compact 模式兼容契约缺口：后端仅验证了 `native` round-trip，未锁定空值默认 `convert`、旧值 `unsupported` 映射 `disabled`、未知值回退 `convert`。已补齐 `model.ChannelOtherSettings` 契约测试。
- 前端 compact 模式归一化分散：空配置、旧值 `unsupported`、未知值和异常 JSON 的归一化逻辑分散在解析分支和调用侧默认值里。已收口为统一归一化函数，并补测试锁定 WebUI 展示和保存口径。
- CodeRabbit minor 文案问题：`OpenAI Response Compaction` 与 `Responses Compact` 混用、后端错误常量缺少句号、法语 `compact` 复数不一致、动态翻译测试仅用 truthy 断言。已统一为 `OpenAI Responses Compact`，修正文案和测试断言。

## 跳过的审查意见

- CodeRabbit 建议恢复 `RemoveDisabledFields` 对全局 pass-through 的无条件短路。跳过原因：本轮修复的核心问题正是 compact `convert` 强制进入转换分支后不应再按全局透传跳过字段过滤；恢复会重新允许转换体透传 `service_tier`、`safety_identifier`、`stream_options.include_obfuscation` 等默认禁用字段。
- CodeRabbit 建议 `ShouldConvertResponsesRequest` 在 `ShouldStripCodexEncryptedContext` 为 true 时返回 false。跳过原因：剥离 Codex encrypted context 必须进入转换分支，否则无法在转发前移除不支持或敏感上下文；现有测试覆盖该语义。具体分支关系：当 `ShouldStripCodexEncryptedContext(info) == true` 时，`ShouldConvertResponsesRequest(info)` 返回 true 并进入 conversion branch 执行 stripping；只有 `ShouldConvertResponsesRequest(info) == false` 且不需要 encrypted-context stripping 时，Codex adaptor native compact path 才保持原生 compact 转发。
- CodeRabbit 建议新增 `ResponsesCompactModeUnset` 和迁移。跳过原因：当前设计明确是 `convert`、`native`、`disabled` 三态；空值默认 `convert`，旧 `unsupported` 映射 `disabled`。新增第四态会扩大本次兼容面并改变已验证语义。
- CodeRabbit 建议在 DTO 默认分支记录未知 compact mode。跳过原因：`dto.ChannelOtherSettings` 当前没有渠道上下文，直接在 DTO 层记录会缺少定位信息且可能产生重复日志；本轮已用后端和前端契约测试锁定未知值回退 `convert`，配置诊断可作为后续独立任务处理。
- CodeRabbit 建议把历史 `unsupported` 改为默认 `convert`。跳过原因：本轮兼容策略明确将旧 `unsupported` 映射为 `disabled`，避免已有旧配置在升级后被静默启用 compact conversion。

## 当前边界

- WebUI 和后端 compact 三态设置范围保持在 OpenAI 渠道类型 `1`；Codex 渠道继续使用专用 adaptor 原生 compact 路径。
- 旧值 `unsupported` 读取时映射为 `disabled`，避免旧渠道被静默启用 compact。
- Azure 不支持 compact；前端空配置仍会归一化为 `convert`，但该设置只在 OpenAI 渠道类型 `1` 生效。
- `RemoveDisabledFields` 第三个参数表示当前请求体是否为未转换的 raw pass-through body。已核对 call-site：Responses handler 仅 raw pass-through 分支传 true，转换分支传 false；`compatible_handler`、`chat_completions_via_responses`、`responses_via_chat`、`claude_handler` 均处理转换后的请求体，因此传 false。
- 本轮未执行 WebUI 浏览器手工创建/编辑渠道验证，也未用真实 API key 做 `/v1/responses/compact` 端到端请求；相关行为由单元测试、类型检查、构建和隔离 Docker 健康验证覆盖，不能表述为真实渠道手工验证已完成。

## 验证

2026-05-24 执行的命令：

```bash
go test ./relay/common ./relay ./relay/channel/openai
go test ./controller ./model ./relay ./relay/common ./relay/helper ./relay/channel/openai ./service
cd web/default && bun run i18n:sync
cd web/default && bun test tests/channel-responses-compact.test.ts tests/channel-priority.test.ts
cd web/default && bun run typecheck
cd web/default && bun run lint
cd web/default && bun run build
git diff --check
```

结果：

- Go 相关包集合测试通过。
- 前端 compact 设置和优先级测试通过。
- i18n 同步报告显示所有 locale 均为 `missing=0`、`extras=0`、`untranslated=0`。
- 前端 typecheck、lint 和 build 通过。
- `git diff --check` 通过。

2026-05-24 兼容性契约修复后的补充复检：

- `go test ./model ./relay/common ./relay ./relay/channel/openai ./controller` 通过。
- `go test ./controller ./model ./relay ./relay/common ./relay/helper ./relay/channel/openai ./service` 通过。
- `cd web/default && bun test tests/channel-responses-compact.test.ts` 通过，共 9 个测试。
- `cd web/default && bun test tests/channel-responses-compact.test.ts tests/channel-priority.test.ts` 通过，共 13 个测试。
- `cd web/default && bun run i18n:sync` 通过；`_sync-report.json` 显示所有 locale 均为 `missingCount=0`、`extrasCount=0`、`untranslatedCount=0`。
- `cd web/default && bun run typecheck` 退出码为 0。
- `cd web/default && bun run lint` 退出码为 0。
- `cd web/default && bun run build` 退出码为 0。

2026-05-24 隔离开发 Docker 验证：

- 部署前已确认范围隔离：`3001` 隔离栈与正式入口相互独立。正式镜像身份和容器生命周期值只保存在私有部署记录中。
- `scripts/build-docker-local.sh new-api-local:dev` 退出码为 0，并构建出 build-info 与候选源码记录一致的隔离候选。
- `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` 只重建隔离应用服务。
- 隔离应用容器 `/new-api --build-info` 与隔离候选记录一致。
- `curl -fsS http://127.0.0.1:3001/api/status` 返回 `success: true`。
- Docker 健康检查达到 `healthy`。
- 隔离验证期间，单独确认的正式容器保持不变。
- `git diff --check` 退出码为 0。

CodeRabbit 复检：

- `coderabbit review --agent -t uncommitted` 完成并报告 8 个问题。
- 已修复 4 个仍有效的次要/琐碎问题：术语一致性、前后端错误消息标点和措辞、法语复数一致性，以及 i18n 非空断言。
- 另外 4 个问题按“跳过的审查意见”一节所列原因跳过。

2026-05-24 CodeRabbit 复检后的验证：

- `go test ./controller ./model ./relay ./relay/common ./relay/helper ./relay/channel/openai ./service` 通过。
- `cd web/default && bun test tests/channel-responses-compact.test.ts tests/channel-priority.test.ts` 通过，共 13 个测试、69 个断言。
- `cd web/default && bun run i18n:sync` 退出码为 0；`_sync-report.json` 显示所有 locale 均为 `missingCount=0`、`extrasCount=0`、`untranslatedCount=0`。
- `cd web/default && bun run typecheck` 退出码为 0。
- `cd web/default && bun run lint` 退出码为 0。
- `cd web/default && bun run build` 退出码为 0。

2026-05-24 CodeRabbit 多轮审查后的最终跟进验证：

- `coderabbit review --agent -t uncommitted` 完成多轮复检。已修复仍有效的问题包括：结构化 compact 诊断、前端非对象设置防护、本地化表单和测试诊断、按模式区分的映射帮助文本、区域语言措辞、非默认 compact 高级状态检测，以及类型安全的测试设置构造。与设计冲突而跳过的问题仍记录在上文。
- `go test ./controller ./model ./relay ./relay/common ./relay/helper ./relay/channel/openai ./service` 退出码为 0。
- `cd web/default && bun test tests/channel-responses-compact.test.ts tests/channel-priority.test.ts` 通过，共 16 个测试、111 个断言。
- `cd web/default && bun run i18n:sync` 退出码为 0；`_sync-report.json` 显示所有 locale 均为 `missingCount=0`、`extrasCount=0`、`untranslatedCount=0`。
- `cd web/default && bun run typecheck` 退出码为 0。
- `cd web/default && bun run lint` 退出码为 0。
- `cd web/default && bun run build` 退出码为 0。
- `git diff --check` 退出码为 0。

2026-05-24 最终隔离开发 Docker 验证：

- 已确认 `3001` 隔离栈与正式入口保持分离；正式运行身份保留在私有部署证据中。
- `scripts/build-docker-local.sh new-api-local:dev` 退出码为 0，并生成 build-info 一致的隔离候选。
- 隔离 Compose 命令只重建隔离应用服务。
- 隔离应用容器 `/new-api --build-info` 与隔离候选记录一致。
- `curl -fsS http://127.0.0.1:3001/api/status` 返回 `success: true`。
- 隔离应用容器的 Docker 健康检查达到 `healthy`。
- 隔离验证期间，单独确认的正式容器保持不变。

2026-05-24 Claude 和 Gemini 复检：

- `claude --print --output-format text --no-session-persistence --tools ""` 审查了未提交 diff，未报告阻断性问题。已修复两个仍有效的低风险清理项：`channels-columns.tsx` 中重复的 compact 徽标回退，以及 `HasDisabledResponsesCompact` 中的 nil 接收者防护。
- `gemini --approval-mode plan --output-format text --prompt ...` 审查了同一份未提交 diff，未报告阻断性问题。Gemini 确认三态 compact 逻辑、禁用字段过滤链、前端归一化和 i18n 覆盖保持一致。
- `go test ./model ./controller ./relay/common ./relay ./relay/channel/openai` 在清理修复后退出码为 0。
- `go test ./controller ./model ./relay ./relay/common ./relay/helper ./relay/channel/openai ./service` 退出码为 0。
- `cd web/default && bun test tests/channel-responses-compact.test.ts` 通过，共 12 个测试、107 个断言。
- `cd web/default && bun test tests/channel-responses-compact.test.ts tests/channel-priority.test.ts` 通过，共 16 个测试、111 个断言。
- `cd web/default && bun run typecheck` 退出码为 0。
- `cd web/default && bun run i18n:sync` 退出码为 0；`_sync-report.json` 显示所有 locale 均为 `missingCount=0`、`extrasCount=0`、`untranslatedCount=0`。
- `cd web/default && bun run lint` 退出码为 0。
- `cd web/default && bun run build` 退出码为 0。
- `git diff --check` 退出码为 0。
- `scripts/build-docker-local.sh new-api-local:dev` 退出码为 0，并生成 build-info 一致的隔离候选。
- 隔离 Compose 命令只重建隔离应用服务。
- 隔离应用容器 `/new-api --build-info` 与隔离候选记录一致。
- `curl -fsS http://127.0.0.1:3001/api/status` 返回 `success: true`。
- 隔离应用容器的 Docker 健康检查达到 `healthy`。
- 隔离验证期间，单独确认的正式容器保持不变。
