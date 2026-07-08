# CR-DEV-FULL-VERIFY-2026-07-08

## Scope

- 验证时间：2026-07-08 14:52:24 CST
- 复检补充时间：2026-07-08 15:31:38 CST
- 正式环境基线：`f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty`
- 初始验证源码：`2e986a65f32d7c75c064a9d01eed9a9ae3b47361-dirty`
- 复检后提交：`f04b83cb82bb183796d9ede0ce2c2a92493df87e`
- 差异范围：`git diff --stat f19d3437c80f8b64ec04d2fa453058f2a2a36530..HEAD`
- 边界：正式环境带 `-dirty`，Git 只能精确覆盖正式环境提交基线到当前 HEAD 的已提交差异，无法还原正式环境未提交改动。

## Deployment Verification

执行：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

结果：

- Docker 构建通过，default/classic 前端和 Go 二进制均完成构建。
- 3001 隔离开发容器重建后达到 `running healthy`。
- 容器内 build-info：
  - version: `v1.1.0`
  - commit: `2e986a65f32d7c75c064a9d01eed9a9ae3b47361`
  - source: `https://github.com/MisonL/new-api`
- `/api/status` 可访问。

## Automated Tests

后端：

```bash
go test -count=1 -timeout=300s ./controller ./model ./relay/common ./relay/helper ./service ./router ./relay/channel/codex ./relay/channel/openai ./relay
```

结果：全部通过。

default 前端目标测试：

```bash
cd web/default
bun test tests/usage-logs-format.test.ts tests/cc-switch-url.test.ts tests/channel-balance.test.ts tests/channel-header-profile-strategy.test.ts tests/channel-priority.test.ts tests/request-header-policy.test.ts tests/protocol-conversion-policy-utils.test.ts
```

结果：90 pass, 0 fail。

classic 前端目标测试：

```bash
cd web/classic
bun test tests/usageLogsResponsesCompact.test.js tests/protocolConversionPolicyUtils.test.js tests/channelBalance.test.js ../tests/usageLogsAudit.test.mjs ../tests/headerOverridePolicy.test.mjs
```

结果：35 pass, 0 fail。

codex-bridge：

```bash
cd tools/codex-bridge
npm test
```

结果：115 pass, 0 fail。

说明：直接用 `bun test` 跑 codex-bridge 会因 Bun 的 `node:test` mock API 不兼容导致 9 个 mock 场景失败；按 package script 的 `node --test` 入口复跑通过。

本地前端构建与类型检查：

```bash
cd web/default && bun run typecheck
cd web/default && bun run build
cd web/classic && bun run build
```

结果：

- default typecheck 通过。
- default build 通过，Safari compatibility check passed。
- classic build 通过，Safari compatibility check passed。

## Compact Control Plane E2E

执行：

```bash
scripts/compact-control-plane-e2e.sh
```

结果：通过，结束后数据库状态恢复，开发容器重启。
说明：新增 native continue 后第一次重跑在 synthetic compact 生成本地 instance id 时遇到一次上下文超时；脚本已恢复数据库并重启容器，随后重跑完整 e2e 通过。

覆盖用例：

- `native_newapi_compact`: `/v1/responses/compact` 返回 compact 输出，HTTP 200。
- `synthetic_newapi_compact_and_continue`: synthetic compact 生成 marker，继续请求 HTTP 200。
- `stale_local_marker_visible_only`: 过期本地 marker 仍保留可见输入继续，HTTP 200。
- `generic_openai_rejects_remote_opaque_compaction`: generic OpenAI profile 拒绝 remote opaque compaction，HTTP 503。
- `sub2api_http_rejects_rest_previous_id`: sub2api HTTP profile 拒绝 REST `previous_response_id`，HTTP 400。
- `model_switch_synthetic_restore`: 模型切换后 synthetic restore HTTP 200。
- `real_codex_history_synthetic_restore`: Codex history fixture compact 与继续均 HTTP 200。

复检后加固：

- `generic_openai_rejects_remote_opaque_compaction` 从非 200 断言收紧为 exact HTTP 503。
- `sub2api_http_rejects_rest_previous_id` 从非 200 断言收紧为 exact HTTP 400。
- `native_newapi_compact` 补充 continue 闭环：先执行 `/v1/responses/compact`，再用返回的 native `id` 调 `/v1/responses`，断言 fake upstream 收到同一个 `previous_response_id`。
- 在包含本轮未提交修正的 `2e986a65...-dirty` 镜像上重新执行完整 e2e，结果通过；新增 native continue、exact 503、exact 400 均在该轮通过。

## Real Channel Verification

临时创建开发库用户和 API token，仅在 shell 变量和 0600 临时文件中使用，结束自动删除。

真实渠道 API：

- `/v1/models`: HTTP 200，包含 `gpt-5.5` 和 `gpt-5.5-openai-compact`，模型数量 31。
- `/v1/responses`: HTTP 200，返回真实 `resp_...`，包含预期输出 `real-channel-ok`。
- `/v1/responses/compact`: HTTP 200，返回 compact 输出。

真实 Codex CLI coding 任务：

- 使用独立临时 `CODEX_HOME`，通过 `tools/codex-bridge` 写入 new-api provider 配置。
- 临时仓库中 `sum.js` 初始实现错误，`node test.js` 先失败。
- 执行：

```bash
codex exec --json --model gpt-5.5 --cd <temp-work-dir> --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox --output-last-message <temp-file> 'Fix the failing JavaScript test by editing the implementation. Run npm test before finishing. Keep the change minimal.'
```

结果：

- `codex_exec_rc=0`
- 最终 `node test.js` 输出 `test passed`
- Codex JSONL 事件 31 行，事件类型包括 `thread.started`、`turn.started`、`item.started`、`item.updated`、`item.completed`、`turn.completed`
- 临时 token 在开发库中记录到真实调用日志和 quota 使用。
- 清理后 `users.id=920001` 与 `tokens.id=920001` 均为 0。

## Npm CLI Version Diagnostics

使用临时管理员用户 `role=10` 验证管理端接口，结束后清理用户。

```bash
GET /api/channel/npm_version_options/diagnostics
GET /api/channel/npm_version_options?package=%40openai%2Fcodex
```

结果：

- diagnostics: HTTP 200, success=true, `summary.package_count=5`, `summary.recorded_count=5`, `summary.missing_count=0`, `summary.last_error_count=0`。
- options: HTTP 200, success=true, source=recorded, latest_version=0.143.0, options=6。
- `latest` 选项存在，`resolvedVersion=0.143.0`，可用于前端选择。

## External Review Follow-up

按用户要求使用本机 CLI 做只读复检：

```bash
claude -p --no-session-persistence --dangerously-skip-permissions --output-format text '<review prompt>'
omp -p --no-session --auto-approve --cwd /Volumes/Work/code/new-api '<review prompt>'
```

Claude 结论：

- 未发现 P0。
- 指出验证报告原先遗漏 `./relay/channel` 等改动包测试。
- 指出 compact v2 safeguard 和 native compact continue 深度可以继续加强。

OMP 结论：

- 未发现 P0。
- 指出 compact e2e 报告写了 503/400，但脚本原先只断言非 200。
- 指出真实渠道验证缺少可复核的脱敏证据摘要。
- 指出 compact `stream:true` 契约不清：请求转换保留 stream 字段，但 handler 按非流式 JSON 响应处理。
- 指出 npm diagnostics 报告字段名应写成 `summary.last_error_count`。

已采纳处理：

- 补跑遗漏 Go 包测试：

```bash
go test -count=1 -timeout=300s ./common ./constant ./dto ./middleware ./pkg/perf_metrics ./setting/model_setting ./types ./relay/channel
```

结果：通过；`constant` 无测试文件。

- 补跑 compact/compaction/safeguard 专项：

```bash
go test -count=1 -timeout=300s ./relay/channel/codex ./relay/channel/openai ./relay ./service -run 'Compact|Compaction|compact|compaction|Safeguard|Synthetic|Native|previous_response_id|Context'
```

结果：通过。

- compact request 转普通 Responses request 时不再携带 `stream` 和 `stream_options`，避免上游返回 SSE 后被 compact handler 按 JSON 解析。
- 更新 DTO 测试，锁定 compact 转发为非流式契约：

```bash
go test -count=1 -timeout=300s ./dto ./relay/channel/openai ./relay/channel/codex ./relay ./service
```

结果：通过。

- 重建 `new-api-local:dev` 镜像并替换 3001 开发容器，容器 build-info 为 `2e986a65f32d7c75c064a9d01eed9a9ae3b47361-dirty`。
- 在新容器上重新执行：

```bash
COMPACT_E2E_CURL_MAX_TIME=180 scripts/compact-control-plane-e2e.sh
```

结果：通过，结束后数据库状态恢复，开发容器重启。

- 在 `2e986a65...-dirty` 新容器上重新创建临时 token 做真实渠道复测：
  - `/v1/responses`: HTTP 200，返回真实 `resp_09c65ab0ff86aeb1016a4dfea36c90819888c87bb9eac63744`，包含 `dirty-real-channel-ok`。
  - `/v1/responses/compact` 携带 `stream:true` 和 `stream_options.include_usage=true`: HTTP 200，返回真实 `resp_04af35b72f60f9b0016a4dfeab107081999d34d81e38996de7`，包含 compaction 输出。
  - 临时 token 产生 2 条日志，结束后清理临时用户和 token。

## Final State

- 初始复检结束时 `git status --short --branch`：包含本报告、compact e2e 状态码断言和 compact stream 契约修正。
- 初始复检结束时开发容器 build-info 为当前工作区镜像 `2e986a65f32d7c75c064a9d01eed9a9ae3b47361-dirty`。
- 后续升级准备阶段已将上述修正提交为 `f04b83cb82bb183796d9ede0ce2c2a92493df87e`，并在该干净提交镜像上重跑关键 Go 测试和 `COMPACT_E2E_CURL_MAX_TIME=180 scripts/compact-control-plane-e2e.sh`，结果通过。
- 如本报告后续再次修改并提交，最终候选镜像仍必须按最新 commit 重新构建并验证 build-info。
- 未发现本轮验证阻断问题。
