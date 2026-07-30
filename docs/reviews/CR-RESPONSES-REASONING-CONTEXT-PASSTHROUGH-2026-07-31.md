# CR-RESPONSES-REASONING-CONTEXT-PASSTHROUGH-2026-07-31

## Summary

采样时间：2026-07-31T04:52:04+0800。

本记录覆盖一个 Responses 原生透传原子任务。目标是在不破坏 opaque encrypted replay、压缩控制面、模型映射、参数覆盖和 Codex item ID 规则的前提下，让支持或未知能力的原生 Responses 渠道保留 `reasoning.context`、`reasoning.mode`、未知 reasoning 字段、未知 `stream_options` 字段和未来顶层字段。

修复前，普通非 opaque 请求只要包含 `reasoning.context` 就必然进入 DTO 序列化路径。该路径虽然能表达已知字段，但对不需要转换的原生渠道施加了不必要的全量重写风险。修复后，满足原生透传条件的请求只替换顶层 `model`；需要语义处理的请求仍显式进入既有结构化路径，不使用隐藏回退或静默字段丢弃。

## Scope

- 工作树：`/Volumes/Work/code/new-api/.worktrees/responses-opaque-replay-integration`
- 分支：`integration/responses-opaque-replay-p0`
- 提交前基线：`36f5b9c33`
- 修改模块：`relay/responses_handler.go`、`relay/responses_via_chat_test.go`、`relay/common/override.go`、`relay/common/responses_reasoning_context.go`、OpenAI Responses adaptor、usage log 字段和对应回归测试。
- 未修改：数据库 schema、正式服务、正式数据库、正式 Redis、渠道配置、用户密钥和 `.gate-tools/`。

## Contract

- opaque encrypted replay 继续使用独立字节保真路径；本任务不会对其执行 item ID 归一化或普通请求的 reasoning-context 策略。
- 对开启请求体透传、无协议桥接且无其他结构化需求的普通 `/v1/responses` 与原生 `/v1/responses/compact` 请求，仅顶层 `model` 可因模型映射被替换。嵌套和未知字段的原始字节保持不变。
- `reasoning.context` 能力为 `supported` 或 `unknown` 时保留；明确为 `unsupported` 时仅删除该字段，并以 `responses_reasoning_context_action=stripped_unsupported_capability` 记录动作。
- 带 OpenAI 推理后缀的模型，例如 `gpt-5.6-sol-high`，始终进入结构化路径，转换为基础模型和 `reasoning.effort`。该规则与是否携带 `reasoning.context` 无关。
- 非法受约束 item ID、空或需归一化的 `context_compaction`、synthetic compact、正文参数覆盖和能力剥离均明确进入结构化路径。`call_id` 不变。
- 仅 header 的 `param_override` 可在不重写 body 的前提下应用；任何正文、条件或格式不兼容的覆盖都走既有结构化覆盖语义。
- usage log 只记录被删除的 item ID 数量 `responses_input_item_ids_removed`，不记录请求正文、密钥或 encrypted content。

## Review

人工 diff 审查覆盖 raw/structured 分流、模型映射、推理后缀、参数覆盖、压缩控制面、previous response 恢复、item ID 归一化和 usage-log 写入点。

CodeRabbit 对初始未提交 diff 的审阅完成，返回两项 trivial 覆盖缺口：

- `responses_input_item_ids_removed=0` 时不应写入 usage log；已补断言。
- 不含 `reasoning.context` 的模型推理后缀也必须走结构化适配；已补实现及 `/v1/responses`、`/v1/responses/compact` 回归。

新增 compact 覆盖后再次请求 CodeRabbit 被公共仓库免费配额限制拒绝，提示 24 分钟后恢复；未把该限制表述为审阅通过。

重启后再次调用 CodeRabbit 仍被公共仓库免费配额限制拒绝，返回等待 6 分钟。只读 Agy 审阅进程异常退出，未产生 finding。两项外部审阅不可用均不作为审阅通过证据；本记录保留其失败边界。

`relay/responses_via_chat_test.go` 中 namespace 工具与历史 item ID 的端点回归由同一任务会话在 2026-07-31T04:32:15+0800 写入，已通过三次定向运行。它验证 Responses 到 Chat 桥接会保留 `call_id`、移除无效历史 item `id`，并还原 namespace 工具输出，因此纳入本原子任务范围。

`relay/responses_handler_test.go` 是既有的端点级回归矩阵，已超过测试文件长度提醒阈值。本任务新增的 native compact 覆盖拆到 `relay/responses_compact_request_body_test.go`；其余直接 `/v1/responses` 测试保留在同一入口的既有矩阵中。为避免与协议修复无关的大规模测试搬迁，本次不拆分旧矩阵，豁免仅限本原子任务。

## L0 Verification

以下命令均退出码 `0`：

```bash
go test ./relay \
  -run 'TestBuildOpenAIResponses(Compact)?RequestBody' \
  -count=3 -timeout=180s

go test ./relay \
  -run '^TestResponsesViaChatBridgesNamespaceToolsWithoutForwardingHistoryItemIDs$' \
  -count=3 -timeout=120s

go test ./relay ./relay/channel/openai ./relay/common ./service \
  -count=1 -timeout=240s

git diff --check
```

定向矩阵覆盖：支持、未知和明确不支持的 reasoning-context 能力；模型映射；模型推理后缀；显式 reasoning effort；未知顶层、reasoning 和 stream-options 字段；全量 Codex item ID 前缀；非法 ID 与 `call_id`；空 context compaction；header-only 和正文参数覆盖；以及普通/compact 原生 Responses 请求。

## L1 Verification

以下命令均退出码 `0`：

```bash
go test ./relay ./relay/channel/openai ./relay/common \
  -count=1 -timeout=240s

go test ./controller ./model ./relay/helper ./service \
  -count=1 -timeout=240s

go test -race ./relay ./relay/channel/openai ./relay/common \
  -count=1 -timeout=300s

go vet ./controller ./model ./relay ./relay/channel/openai \
  ./relay/common ./relay/helper ./service

go test ./relay/... -count=1 -timeout=300s

go test ./service/openaicompat -count=1 -timeout=180s
```

未报告 test failure、data race 或 vet finding。

## L2 Gate

本记录创建时尚未执行本次提交 SHA 的隔离 Docker 重建、`3001` HTTP smoke、`compact-control-plane-e2e.sh` 或真实上游渠道验收。因此本原子任务的 L0/L1 通过不等同于开发运行时、真实渠道或正式服务验证。

正式升级前必须完成：提交后以可追溯镜像重建隔离 `3001`，核对 build-info 和 `/api/status`，执行控制面 E2E 并确认 PostgreSQL/Redis 快照恢复和锁释放，然后重新获取当前正式环境启用的 GPT 渠道做真实 Responses/compact 协议验证。正式服务当前未修改。
