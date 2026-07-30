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

## Gate boundary and residual risk

当前只完成离线 L0/L1。尚未完成本任务要求的隔离 Docker 镜像重建、`3001` build-info
和 `/api/status` 核对、无凭证 Responses/compact/tool smoke，以及从正式库重新读取
当前启用 GPT 渠道并复制到开发环境的真实上游验证。因此本审计不授权生产升级。

下一原子任务是吸收 sub2api PR #5021 的非流式响应体读取错误分类；它必须单独设计、
测试、审计和提交，不能与本任务混合。
