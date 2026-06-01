# CR-RESPONSES-COMPACT-RECHECK-2026-05-24

## Scope

本记录覆盖 Responses Compact 三态重构后的复检。当前目标状态为：

- `responses_compact_mode` 支持 `convert`、`native`、`disabled`。
- OpenAI 渠道默认 `convert`，将 `/v1/responses/compact` 上游路径转换为 `/v1/responses`。
- `convert` 保留 `previous_response_id`，移除不能安全转发到普通 Responses 的 compact 输入项。
- `native` 保持 `/v1/responses/compact` 原生直通。
- `disabled` 显式拒绝 compact 请求。

## Findings Fixed

- 转换体过滤绕过：当全局或渠道开启请求体透传时，compact `convert` 会强制进入转换分支，但后续禁用字段过滤仍把透传开关当作跳过条件。已改为只在本次真实请求体透传时跳过过滤，转换体继续过滤 `service_tier`、`safety_identifier`、`stream_options.include_obfuscation` 等默认禁用字段。
- 动态 i18n 缺口：渠道列表的 `Compact Convert`、`Compact Disabled`、`Compact disabled` 通过动态 key 调用 `t()`，不会被同步脚本从静态调用中发现。已补齐 `en`、`zh`、`fr`、`ja`、`ru`、`vi`，并增加测试锁定这些运行时 key。
- compact 模式兼容契约缺口：后端仅验证了 `native` round-trip，未锁定空值默认 `convert`、旧值 `unsupported` 映射 `disabled`、未知值回退 `convert`。已补齐 `model.ChannelOtherSettings` 契约测试。
- 前端 compact 模式归一化分散：空配置、旧值 `unsupported`、未知值和异常 JSON 的归一化逻辑分散在解析分支和调用侧默认值里。已收口为统一归一化函数，并补测试锁定 WebUI 展示和保存口径。
- CodeRabbit minor 文案问题：`OpenAI Response Compaction` 与 `Responses Compact` 混用、后端错误常量缺少句号、法语 `compact` 复数不一致、动态翻译测试仅用 truthy 断言。已统一为 `OpenAI Responses Compact`，修正文案和测试断言。

## Review Findings Skipped

- CodeRabbit 建议恢复 `RemoveDisabledFields` 对全局 pass-through 的无条件短路。跳过原因：本轮修复的核心问题正是 compact `convert` 强制进入转换分支后不应再按全局透传跳过字段过滤；恢复会重新允许转换体透传 `service_tier`、`safety_identifier`、`stream_options.include_obfuscation` 等默认禁用字段。
- CodeRabbit 建议 `ShouldConvertResponsesRequest` 在 `ShouldStripCodexEncryptedContext` 为 true 时返回 false。跳过原因：剥离 Codex encrypted context 必须进入转换分支，否则无法在转发前移除不支持或敏感上下文；现有测试覆盖该语义。具体分支关系：当 `ShouldStripCodexEncryptedContext(info) == true` 时，`ShouldConvertResponsesRequest(info)` 返回 true 并进入 conversion branch 执行 stripping；只有 `ShouldConvertResponsesRequest(info) == false` 且不需要 encrypted-context stripping 时，Codex adaptor native compact path 才保持原生 compact 转发。
- CodeRabbit 建议新增 `ResponsesCompactModeUnset` 和迁移。跳过原因：当前设计明确是 `convert`、`native`、`disabled` 三态；空值默认 `convert`，旧 `unsupported` 映射 `disabled`。新增第四态会扩大本次兼容面并改变已验证语义。
- CodeRabbit 建议在 DTO 默认分支记录未知 compact mode。跳过原因：`dto.ChannelOtherSettings` 当前没有渠道上下文，直接在 DTO 层记录会缺少定位信息且可能产生重复日志；本轮已用后端和前端契约测试锁定未知值回退 `convert`，配置诊断可作为后续独立任务处理。
- CodeRabbit 建议把历史 `unsupported` 改为默认 `convert`。跳过原因：本轮兼容策略明确将旧 `unsupported` 映射为 `disabled`，避免已有旧配置在升级后被静默启用 compact conversion。

## Current Boundaries

- WebUI 和后端 compact 三态设置范围保持在 OpenAI 渠道类型 `1`；Codex 渠道继续使用专用 adaptor 原生 compact 路径。
- 旧值 `unsupported` 读取时映射为 `disabled`，避免旧渠道被静默启用 compact。
- Azure 不支持 compact；前端空配置仍会归一化为 `convert`，但该设置只在 OpenAI 渠道类型 `1` 生效。
- `RemoveDisabledFields` 第三个参数表示当前请求体是否为未转换的 raw pass-through body。已核对 call-site：Responses handler 仅 raw pass-through 分支传 true，转换分支传 false；`compatible_handler`、`chat_completions_via_responses`、`responses_via_chat`、`claude_handler` 均处理转换后的请求体，因此传 false。
- 本轮未执行 WebUI 浏览器手工创建/编辑渠道验证，也未用真实 API key 做 `/v1/responses/compact` 端到端请求；相关行为由单元测试、类型检查、构建和隔离 Docker 健康验证覆盖，不能表述为真实渠道手工验证已完成。

## Verification

Commands run on 2026-05-24:

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

Results:

- Go related package sets passed.
- Frontend compact settings and priority tests passed.
- i18n sync report shows all locales with `missing=0`, `extras=0`, `untranslated=0`.
- Frontend typecheck, lint, and build passed.
- `git diff --check` passed.

Additional recheck on 2026-05-24 after compatibility-contract fixes:

- `go test ./model ./relay/common ./relay ./relay/channel/openai ./controller` passed.
- `go test ./controller ./model ./relay ./relay/common ./relay/helper ./relay/channel/openai ./service` passed.
- `cd web/default && bun test tests/channel-responses-compact.test.ts` passed with 9 tests.
- `cd web/default && bun test tests/channel-responses-compact.test.ts tests/channel-priority.test.ts` passed with 13 tests.
- `cd web/default && bun run i18n:sync` passed; `_sync-report.json` shows all locales with `missingCount=0`, `extrasCount=0`, `untranslatedCount=0`.
- `cd web/default && bun run typecheck` exited 0.
- `cd web/default && bun run lint` exited 0.
- `cd web/default && bun run build` exited 0.

Isolated dev Docker validation on 2026-05-24:

- Confirmed target separation before deploy: production `new-api` is `new-api-local:prod-main` on port `3000`; isolated dev is `new-api-dev-isolated-new-api-1` using `new-api-local:dev` on port `3001`.
- `scripts/build-docker-local.sh new-api-local:dev` exited 0 and built `new-api-local:dev` with build commit `48ca2dd77b0cd55935ec5b970152c60e02448c92-dirty`.
- `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` recreated only `new-api-dev-isolated-new-api-1`.
- `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` reported version `v1.1.0`, commit `48ca2dd77b0cd55935ec5b970152c60e02448c92-dirty`, date `2026-05-24T01:23:05Z`.
- `curl -fsS http://127.0.0.1:3001/api/status` returned `success: true`.
- Docker health check reached `healthy`.
- Production `new-api` remained on `new-api-local:prod-main`, build commit `8c0e6d05c085196507da8da56ac4c31028786b6f`, started `2026-05-21T16:10:45.789793804Z`.
- `git diff --check` exited 0.

CodeRabbit recheck:

- `coderabbit review --agent -t uncommitted` completed with 8 findings.
- Fixed 4 still-valid minor/trivial findings: terminology consistency, backend/frontend error message punctuation and wording, French plural agreement, and explicit non-empty i18n assertions.
- Skipped 4 findings for the reasons listed in "Review Findings Skipped".

Post-CodeRabbit validation on 2026-05-24:

- `go test ./controller ./model ./relay ./relay/common ./relay/helper ./relay/channel/openai ./service` passed.
- `cd web/default && bun test tests/channel-responses-compact.test.ts tests/channel-priority.test.ts` passed with 13 tests and 69 assertions.
- `cd web/default && bun run i18n:sync` exited 0; `_sync-report.json` shows all locales with `missingCount=0`, `extrasCount=0`, `untranslatedCount=0`.
- `cd web/default && bun run typecheck` exited 0.
- `cd web/default && bun run lint` exited 0.
- `cd web/default && bun run build` exited 0.

Final follow-up validation on 2026-05-24 after CodeRabbit rounds:

- `coderabbit review --agent -t uncommitted` completed multiple rechecks. Still-valid issues fixed: structured compact diagnostics, frontend non-object settings guard, localized form/test diagnostics, mode-specific mapping help text, locale wording, non-default compact advanced-state detection, and type-safe test settings construction. Skipped design-conflicting findings remain documented above.
- `go test ./controller ./model ./relay ./relay/common ./relay/helper ./relay/channel/openai ./service` exited 0.
- `cd web/default && bun test tests/channel-responses-compact.test.ts tests/channel-priority.test.ts` passed with 16 tests and 111 assertions.
- `cd web/default && bun run i18n:sync` exited 0; `_sync-report.json` shows all locales with `missingCount=0`, `extrasCount=0`, `untranslatedCount=0`.
- `cd web/default && bun run typecheck` exited 0.
- `cd web/default && bun run lint` exited 0.
- `cd web/default && bun run build` exited 0.
- `git diff --check` exited 0.

Final isolated dev Docker validation on 2026-05-24:

- Confirmed target separation before deploy: production `new-api` runs `new-api-local:prod-main` on port `3000`; isolated dev `new-api-dev-isolated-new-api-1` runs `new-api-local:dev` on port `3001`.
- `scripts/build-docker-local.sh new-api-local:dev` exited 0 and built `new-api-local:dev` with build commit `48ca2dd77b0cd55935ec5b970152c60e02448c92-dirty`, date `2026-05-24T02:50:23Z`.
- `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` recreated only `new-api-dev-isolated-new-api-1`.
- `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` reported version `v1.1.0`, commit `48ca2dd77b0cd55935ec5b970152c60e02448c92-dirty`, date `2026-05-24T02:50:23Z`.
- `curl -fsS http://127.0.0.1:3001/api/status` returned `success: true`.
- Docker health check for `new-api-dev-isolated-new-api-1` reached `healthy`.
- Production `new-api` remained on `new-api-local:prod-main`, build commit `8c0e6d05c085196507da8da56ac4c31028786b6f`, started `2026-05-21T16:10:45.789793804Z`.

Claude and Gemini recheck on 2026-05-24:

- `claude --print --output-format text --no-session-persistence --tools ""` reviewed the uncommitted diff and reported no blocking findings. Two still-valid low-risk cleanup items were fixed: redundant compact badge fallback in `channels-columns.tsx`, and defensive nil receiver handling in `HasDisabledResponsesCompact`.
- `gemini --approval-mode plan --output-format text --prompt ...` reviewed the same uncommitted diff and reported no blocking findings. Gemini confirmed the three-state compact logic, disabled-field filtering chain, frontend normalization, and i18n coverage are consistent.
- `go test ./model ./controller ./relay/common ./relay ./relay/channel/openai` exited 0 after the cleanup fixes.
- `go test ./controller ./model ./relay ./relay/common ./relay/helper ./relay/channel/openai ./service` exited 0.
- `cd web/default && bun test tests/channel-responses-compact.test.ts` passed with 12 tests and 107 assertions.
- `cd web/default && bun test tests/channel-responses-compact.test.ts tests/channel-priority.test.ts` passed with 16 tests and 111 assertions.
- `cd web/default && bun run typecheck` exited 0.
- `cd web/default && bun run i18n:sync` exited 0; `_sync-report.json` shows all locales with `missingCount=0`, `extrasCount=0`, `untranslatedCount=0`.
- `cd web/default && bun run lint` exited 0.
- `cd web/default && bun run build` exited 0.
- `git diff --check` exited 0.
- `scripts/build-docker-local.sh new-api-local:dev` exited 0 and built `new-api-local:dev` with build commit `48ca2dd77b0cd55935ec5b970152c60e02448c92-dirty`, date `2026-05-24T03:39:28Z`.
- `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` recreated only `new-api-dev-isolated-new-api-1`.
- `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` reported version `v1.1.0`, commit `48ca2dd77b0cd55935ec5b970152c60e02448c92-dirty`, date `2026-05-24T03:39:28Z`.
- `curl -fsS http://127.0.0.1:3001/api/status` returned `success: true`.
- Docker health check for `new-api-dev-isolated-new-api-1` reached `healthy`.
- Production `new-api` remained on `new-api-local:prod-main`, started `2026-05-21T16:10:45.789793804Z`.
