# CR-RESPONSES-OPAQUE-REPLAY-INTEGRATION-2026-07-29

## Summary

采样时间：2026-07-30T12:17:35Z。

本记录覆盖 Responses opaque replay 集成中的一个原子任务：统一 `/v1/responses/compact` 与 `/responses/compact` 的原生压缩入口、路由识别、选渠别名、认证顺序和结算路径。

结论：当前隔离开发环境已运行本工作树构建的镜像。两个 compact URL 均在认证后进入强制磁盘 spool；经过 identity、gzip、Brotli 和 Zstd 的请求会保留解压后的原始字节。未认证请求在认证前不读取 body。正式服务未修改。

## Scope

- 工作树：`/Volumes/Work/code/new-api/.worktrees/responses-opaque-replay-integration`
- 分支：`integration/responses-opaque-replay-p0`
- Git HEAD：`74d83854586da22f40c6a818c74311f2c478df6c`
- `origin/main`：`51074ac3736288ee664966e0a2109e9580d86fcf`
- 构建镜像：`new-api-local:dev`
- 隔离后端：`http://127.0.0.1:3001`
- 隔离容器：`new-api-dev-isolated-new-api-1`

主工作树仅保留未跟踪的 `.gate-tools/` 生成物，未纳入提交。为避免将生成物误写入镜像元数据，最终镜像从临时干净 worktree 构建，build-info 为 `74d83854586da22f40c6a818c74311f2c478df6c`，不带 `-dirty`。这表示容器运行的是该提交，不表示已完成正式环境验收或可直接升级生产。

## Changes Under Test

- `common.IsResponsesCompactPath` 精确识别两个原生 compact URL。
- `Path2RelayMode` 和选渠逻辑将两个 URL 统一为 `RelayModeResponsesCompact` 与 `-openai-compact` 模型别名。
- `CodexLocalResponsesCompactionIngress` 对 local Codex `/v1/responses` 及两个 direct compact URL 在认证后执行解压、强制磁盘落盘和 metadata 扫描。
- `settleResponsesCompactUsage` 在消费前补齐 compact usage、重新计算 compact 模型价格，并在返回前恢复原始 `RelayInfo` 定价状态。
- opaque replay 入口不走结构化兼容重写或跨渠道重放；上游 body 只允许替换顶层 `model`。

## L0 Verification

命令：

```bash
go test ./common ./dto ./middleware ./router ./relay/constant ./relay/common ./relay/channel/openai ./relay -count=1
git diff --check
```

结果：退出码 `0`。

额外定向验证覆盖：

- 两个 direct compact URL 在 identity、gzip、Brotli、Zstd 下的磁盘 spool、metadata 和字节保真。
- 两个 direct compact URL 的未认证请求体零读取。
- local Codex compact 的未认证请求体零读取。
- compact usage 归一化、定价重算和 `PriceData` 恢复。

## L1 Verification

命令：

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service -count=1
```

结果：退出码 `0`。

## L2 Verification

执行：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml \
  --env-file /Volumes/Work/code/new-api/deploy/env/dev-isolated.env \
  up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS --connect-timeout 2 --max-time 5 http://127.0.0.1:3001/api/status
```

结果：

- Docker build 退出码 `0`。
- 容器重建后状态为 `running healthy`。
- `/new-api --build-info` 的 commit 为 `74d83854586da22f40c6a818c74311f2c478df6c`，不带 `-dirty`。
- `/api/status` 返回 `success:true`。
- 容器最近 120 行启动日志未匹配 `panic`、`fatal`、监听失败或迁移失败。

无凭证协议 smoke：

```bash
curl -sS -o /dev/null -w '%{http_code}\n' \
  --connect-timeout 2 --max-time 5 \
  -X POST http://127.0.0.1:3001/v1/responses/compact \
  -H 'Content-Type: application/json' \
  --data-binary '{"model":"gpt-5.6-sol","input":[{"type":"compaction","encrypted_content":"opaque-state"}]}'

curl -sS -o /dev/null -w '%{http_code}\n' \
  --connect-timeout 2 --max-time 5 \
  -X POST http://127.0.0.1:3001/responses/compact \
  -H 'Content-Type: application/json' \
  --data-binary '{"model":"gpt-5.6-sol","input":[{"type":"compaction","encrypted_content":"opaque-state"}]}'
```

两个请求均返回 `401`，证明未携带凭证时不会进入上游或选渠路径。

控制面 E2E：

```bash
COMPACT_E2E_ENV_FILE=/Volumes/Work/code/new-api/deploy/env/dev-isolated.env \
COMPACT_E2E_COMPOSE_PROJECT_NAME=new-api-dev-isolated \
scripts/compact-control-plane-e2e.sh
```

结果：退出码 `0`。脚本仅对隔离 PostgreSQL 写入临时测试用户、令牌、四个测试渠道和两项 option，并在 `EXIT` trap 中恢复它们后重启隔离服务。覆盖并通过：

- 原生 `/v1/responses/compact` 转发及 `previous_response_id` 后续请求。
- `stream_options.include_usage` 失败重试后的渠道能力学习与下次预剥离。
- remote compaction 上游 `413` 的 synthetic compact 恢复，且不写入永久 `request_body_limit`。
- synthetic compact、过期本地 marker、模型切换和真实 Codex history fixture 的恢复。
- generic OpenAI profile 对 opaque compaction 的显式 `503` 拒绝。
- native 和 local-Codex compaction ingress 的 opaque body 仅替换顶层模型、字节保真且不跨渠道重试。
- sub2api HTTP profile 对不兼容 `previous_response_id` 的显式 `400`。

该运行首次暴露 E2E 辅助脚本缺陷：`psql_query` 未转发调用方的 psql 参数，使新增的 model-mapping snapshot 无法替换 `:channel`。修复为转发 `"$@"` 后，从已恢复的隔离数据库完整重跑通过。运行结束后独立确认容器启动日志无 panic、fatal、监听或迁移失败，`/api/status` 返回 `200` 与 `success:true`。

本轮还验证了完整控制面脚本在服务重启期间的生命周期处理。此前使用 Docker 默认 10 秒停止窗口时，慢 SQL 或计费请求尚未结束，Compose 出现 `tried to kill container, but did not receive an exit event`，容器最终以 `137` 结束；该失败发生在断言之后的重启阶段，不是业务断言失败。当前 `main.go` 使用 `http.Server.Shutdown`，默认优雅退出窗口为 120 秒，脚本使用 `docker compose restart -t 120` 并以 180 秒总时限等待健康检查。本轮所有重启均正常完成，未再出现 `137`。这只解决开发 E2E 的生命周期稳定性，不证明生产部署的停止窗口或外部编排已采用相同配置。

并发复核首次运行 `go test -race ./controller ./middleware ./relay ./relay/channel/openai ./relay/common ./service -count=1 -timeout=180s` 时，发现 `PostTextConsumeQuota` 的异步指标记录在测试清理之后读取 `common.RedisEnabled`，与恢复全局状态发生 data race。修复后，文本和音频结算路径均在提交 gopool 任务前调用 `perfmetrics.CaptureRelaySample`，将 request-scoped sample 和 Redis 依赖快照为不可变闭包；后台任务不再读取可变全局依赖。定向三例 unreadable-body retry 的 race 测试与完整上述 race 命令均以退出码 `0` 重跑通过。

最终提交后的门禁结果：`go test ./... -count=1 -timeout=180s`、`go vet ./...`、默认前端 `bun run lint`、`bun run typecheck`、`bun run build`、classic 两组定向测试、L0、L1、完整 E2E 和完整 race 回归均退出码 `0`。最终隔离容器使用 `74d83854586da22f40c6a818c74311f2c478df6c` 启动，状态为 `running healthy`，`/api/status` 返回 `success:true`。

观察到一项与本 E2E 无因果关系、但需要后续单独审计的启动配置警告：`failed to update option map: invalid http status code rules: []`。本记录不将其视为已解决。

## Boundary

- 本记录不包含真实渠道、用户密钥或正式环境请求。
- 未验证带真实授权的上游 compaction 响应，因此不将 L0-L2 结果表述为生产渠道验收。
- 正式服务、正式数据库和正式 Redis 未修改。
- 后续生产升级仍需完成完整 Goal 的剩余 DTO、工具、reasoning-context、可观测性和前端门禁，并在升级窗口内执行备份、真实请求验收和升级后监控。
