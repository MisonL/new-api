# Responses logical reasoning effort mapping review

Date: 2026-07-30

## Scope

本审计只覆盖一个原子任务：客户端模型名携带 `-high`、`-medium` 或 `-low` 等
OpenAI reasoning effort 后缀时，经过渠道模型映射后仍将该逻辑 effort 发送给上游。
覆盖原生 Responses、Responses-to-Chat bridge、Responses compact，以及不透明
`encrypted_content` replay 的字节保真边界。

本任务不修改数据库 schema、用户密钥、正式渠道配置或生产服务。正式升级保持冻结，
直到隔离运行时和当前正式启用渠道验证完成。

## Control contract

- Primary setpoint: `gpt-5.6-sol-high` 映射到 `provider/gpt-5.6-sol` 后，上游仍收到
  `reasoning.effort=high`。
- Acceptance: 相关 Go 测试、跨模块回归、`go vet`、race、隔离镜像 build-info、
  `/api/status` 和协议 smoke 均成功。
- Guardrails: opaque replay 不注入顶层 `reasoning`；原始请求只允许替换顶层
  `model`，不得重编码、删除或改写 `encrypted_content` 和未知字段。
- Rollback trigger: 任一 native/bridge/compact 请求丢失 effort、opaque body 出现
  非模型差异、测试或隔离协议 smoke 失败时停止推进，不升级生产。
- Boundary: `relay/common/responses_reasoning_context.go`、
  `relay/responses_handler.go`、`relay/channel/openai/adaptor.go` 及本审计对应测试。

## Root cause

模型映射在请求进入 adaptor 前将逻辑模型名替换成上游模型名。旧逻辑随后只从映射
后的模型名解析 effort 后缀，因此 `gpt-5.6-sol-high` 变成
`provider/gpt-5.6-sol` 后无法恢复 `high`。同一问题同时影响原生 Responses 和
Responses-to-Chat bridge。compact 路径还会在逻辑模型名后追加
`-openai-compact`，解析前必须去除该后缀。

opaque replay 不能采用同一注入逻辑：加密历史必须按原始请求字节转发。它只允许在
顶层 JSON 中替换路由后的模型值，并保持 `info.ReasoningEffort` 为空。

## Changes

- `relay/common/responses_reasoning_context.go` 新增基于
  `RelayInfo.OriginModelName` 的逻辑 effort 解析和不可变请求复制。compact 解析前
  去除 `-openai-compact`。
- `relay/responses_handler.go` 在模型映射后、协议分流前注入逻辑 effort；opaque
  请求明确跳过；有逻辑 effort 的结构化请求强制走带 reasoning 的正文构建路径。
- `relay/channel/openai/adaptor.go` 保留已映射上游模型名，同时保留未映射 suffix
  模型原有的去 suffix 行为。
- 新增 native、compact、bridge、未变更请求和 opaque replay 回归；opaque 用精确
  字节断言验证仅模型值发生变化。
- compact 控制面 E2E 的 HTTP、文本和数据库断言改为显式失败函数，并在失败时输出
  响应体；这消除了 Bash `set -e` 在函数/命令替换边界下可能产生的假阳性。
- E2E 只在隔离数据库中临时扩展 `ModelRatio`、`CompletionRatio`、`CacheRatio` 和
  `CreateCacheRatio`，加入 `gpt-5.6-sol` 与 `gpt-5.6-sol-high` fixture，并在清理时
  比较和恢复完整 options 快照。
- fake upstream capture 仅记录脱敏的 `reasoningEffort` 字段；新增映射 HTTP 用例验证
  `gpt-5.6-sol-high` 到 `provider/gpt-5.6-sol` 的模型映射和 `reasoning.effort=high`。

## Verification

以下命令在分支 `integration/responses-opaque-replay-p0`、工作树
`/Volumes/Work/code/new-api/.worktrees/responses-opaque-replay-integration` 执行，
均以退出码 0 完成：

```text
go test ./relay ./relay/channel/openai -run 'Test(BuildOpenAIResponsesRequestBodyPreservesMappedLogicalEffortContract|ResponsesHelperMapsLogicalEffortBeforeResponsesToChat|ResponsesHelperDoesNotInjectLogicalEffortForOpaqueReplay|ResponsesToChatCarriesMappedLogicalReasoningEffort|ConvertOpenAIResponsesRequestPreservesMappedLogicalReasoningEffort)' -count=1 -timeout=60s
go test ./relay/common -count=1 -timeout=120s
go test ./relay -count=1 -timeout=180s
go test ./relay/channel/openai -count=1 -timeout=180s
go test ./controller ./model ./relay/common ./relay/helper ./service -count=1 -timeout=300s
go vet ./...
go test -race ./relay ./relay/channel/openai -count=1 -timeout=300s
gofmt -w relay/channel/openai/adaptor.go relay/channel/openai/adaptor_responses_compaction_test.go relay/common/responses_reasoning_context.go relay/common/responses_reasoning_effort_test.go relay/responses_handler.go relay/responses_handler_test.go relay/responses_reasoning_effort_mapping_test.go
git diff --check
```

测试证据覆盖：

- 原生 Responses 映射后保留 `high`；compact 映射后保留 `low`。
- Responses-to-Chat bridge 上游收到映射模型和 `reasoning_effort=high`。
- 既有 reasoning 字段与未知嵌套字段在逻辑 effort 复制时保留。
- opaque replay 不增加顶层 reasoning，且上游 body 等于仅替换顶层 model 后的原始
  body 字节。
- `go vet` 和 race 未发现诊断或数据竞争。

## L2 隔离运行时验证

补充采样时间：2026-07-31 08:07:53 +0800。使用隔离项目
`new-api-dev-effort-dev-isolated`，API `3001`，PostgreSQL 用户/数据库为
`effort/new_api_effort`，Redis 为同一 Compose project。容器 build-info 与候选代码
一致：`3c1d4ec63c711d17bd68d3fcd9a707fb4b45d764`；容器为 `healthy`，`GET /api/status`
返回 `success:true`。

在不读取或写入正式配置的前提下，执行当前 worktree 的控制面 E2E（两次完整运行，
第二次用包装命令显式打印退出码）：

```bash
COMPACT_E2E_BASE_URL=http://127.0.0.1:3001 \
COMPACT_E2E_UPSTREAM_HOST_URL=http://127.0.0.1:18081 \
COMPACT_E2E_UPSTREAM_CONTAINER_URL=http://host.docker.internal:18081 \
COMPACT_E2E_POSTGRES_CONTAINER=new-api-dev-effort-dev-isolated-postgres-1 \
COMPACT_E2E_POSTGRES_USER=effort \
COMPACT_E2E_POSTGRES_DB=new_api_effort \
COMPACT_E2E_APP_CONTAINER=new-api-dev-effort-dev-isolated-new-api-1 \
COMPACT_E2E_REDIS_CONTAINER=new-api-dev-effort-dev-isolated-redis-1 \
COMPACT_E2E_COMPOSE_FILE=/private/tmp/new-api-responses-effort-3c1d4ec63/deploy/compose/dev-isolated.yml \
COMPACT_E2E_ENV_FILE=/private/tmp/new-api-effort-dev.env \
COMPACT_E2E_COMPOSE_PROJECT_NAME=new-api-dev-effort-dev-isolated \
scripts/compact-control-plane-e2e.sh
```

结果：两次均完成 native compact continuation、stream-options 能力学习、远程 413
恢复、synthetic/stale、generic opaque 503、native/local Codex opaque 字节保真且不
重试、Sub2API previous-response-id 拒绝、模型切换和 Codex history fixture；新增
`mapped_logical_reasoning_effort` 也通过，fake upstream 实际收到
`model=provider/gpt-5.6-sol`、`reasoning.effort=high`。第二次包装运行明确输出
`e2e_exit_code=0`。两次收尾均有：

```text
database_snapshot_match=true
redis_snapshot_match=true
compact_e2e_lock_released=true
e2e_done=true
```

独立检查确认 E2E 锁和 fake upstream 均已清理，隔离容器仍为 `healthy`。

## Gate boundary and residual risk

本任务已完成当前候选提交的 L0/L1 和隔离 L2；fake upstream 仍只证明本地请求构造、
路由和 capture 语义，不等同于真实上游质量、正式渠道可用性或生产验收。正式升级前
仍必须重新读取正式环境当前启用的 GPT 渠道，在开发环境执行真实上游 Responses/compact
验证并观察 502/429、工具、压缩和 reasoning-context 指标；在这些门禁完成前不授权
生产升级。

下一原子任务是吸收 sub2api PR #5021 的非流式响应体读取错误分类；它必须单独设计、
测试、审计和提交，不能与本任务混合。
