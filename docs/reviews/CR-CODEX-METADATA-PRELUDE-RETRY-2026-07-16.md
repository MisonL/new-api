# Codex 元数据 Prelude 断流重试审计

## 范围

- 采样日期：2026-07-16
- 真实复现：隔离环境 3001，渠道 205，模型 `gpt-5.6-sol`，namespace 场景。
- 请求 ID：`202607161410019653201188268d9d6I8Jix6K9`
- 边界：只修复首个语义事件前的 Codex 元数据，不改变正文已经写出后的流终止策略。

## 现象

- 上游先返回 `codex.rate_limits` 与 `codex.response.metadata`，随后连接失败。
- new-api 将两个元数据事件立即写给客户端，HTTP 200 因此提前提交。
- 服务端随后识别 `upstream_connection_error` 并记录 502，但客户端只看到两个元数据事件，没有 `response.completed`、`response.failed` 或 `response.error`。
- smoke 结果为 `SSE response contained no terminal event`。

## 根因

- `isResponsesStreamPreludeEvent` 只识别 `response.created`、`response.in_progress`、`response.queued`。
- Codex 专用的 rate limit 与 response metadata 不是模型语义输出，但未被归类为 prelude。
- 通用断流判断同时只接受 `ReceivedResponseCount == 0`。即使事件仍缓存在 new-api 内部，收到两个元数据事件也会阻止可重试错误上抛。

## 修复

- 将 `codex.rate_limits`、`codex.response.metadata` 纳入 Responses prelude 缓冲。
- 断流是否可重试同时参考 `responses_stream_prelude_retry_safe`，以实际是否写出客户端为准。
- 若后续出现有效 Responses 事件，缓冲的 Codex 元数据仍按原顺序转发。
- 若元数据后直接断流，保持 writer 未写，向 controller 返回 `upstream_transport_interrupted` 502，由渠道重试层处理。
- smoke 脚本新增 `--custom-tool-name` 与 `--tool-choice-mode`，用于区分工具名特例和 forced/auto tool choice，不改变默认行为。

## 验证

```text
go test -count=1 -timeout=60s ./controller ./model ./relay/common ./relay/helper ./relay/channel/openai ./relay ./service
exit 0

go vet ./controller ./model ./relay/common ./relay/helper ./relay/channel/openai ./relay ./service
exit 0

go test -count=1 -timeout=10m ./...
exit 0

go test -race -count=1 -timeout=3m ./relay/channel/openai ./relay ./controller
exit 0

node --check scripts/codex-responses-lite-smoke.mjs
exit 0

git diff --check
exit 0
```

新增回归覆盖：

- Codex 元数据后断流时 writer 保持未写并返回可重试 502。
- Codex 元数据后正常完成时，元数据与 `response.completed` 全部按序转发。
- controller 已有 `upstream_transport_interrupted` 跨渠道重试断言。

附加稳定性复跑：

```text
go test -count=5 -timeout=60s ./relay/channel/openai -run 'TestOaiResponsesStreamHandler(KeepsCodexMetadataPreludeRetryable|FlushesCodexMetadataPreludeOnCompleted|ReturnsRetryableErrorBeforeFirstWrite)|TestRetryableUpstreamStreamInterruptedError'
exit 0

go test -count=5 -timeout=60s ./controller -run 'Test.*(UpstreamTransport|Retry.*Interrupted|RelayRetry)'
exit 0
```

`go test -count=3 -timeout=90s ./controller` 额外复跑返回 1。失败集中在既有测试进程全局状态未复位：Header Profile 预期版本 `0.142.4` 被前序测试残留为 `1.2.3`，CAS 回调地址被残留为 `127.0.0.1:13000`。这些失败与本次 Responses 断流代码无调用耦合，`-count=1` 整包回归及本次相关 controller 重试测试均通过；测试隔离问题保留为独立风险，不在本原子任务中修改。

## L2 结果

- 最终隔离镜像与 3001 容器 build-info 均为提交 `e6e0c31fd4d6f06f71a13f44afc6bbfd9daf8a0d`，`/api/status` 成功，容器保持 healthy 且重启次数为 0。
- 指定开发渠道 205 重放 namespace 场景时，上游仍在 `codex.rate_limits`、`codex.response.metadata` 后发送 `upstream_connection_error`。请求 `202607161436272682653898268d9d6dAnT7KeP` 对客户端返回明确 HTTP 502，不再出现 HTTP 200 且无终止事件。
- 自动路由 namespace 请求依次尝试 `169->199->150->180->205`，最终渠道 205 的同类断流仍以 HTTP 502 收口；前序已失败渠道没有因 SSE prelude 提交 200 而阻断重试链。
- 指定渠道 205 的 grammar custom tool 在 `tool_choice=forced` 与 `tool_choice=auto` 下均返回 HTTP 200、`response.completed` 和正确工具名。
- 原始 SSE 抽取显示同一 custom 请求的 `response.output_item.added/done` 为 `custom_tool_call`，终止 `response.completed.output` 为 `function_call`。这是渠道上游自身的前后类型差异，new-api 原样转发，没有在终止事件中伪造 custom 类型。
- 本机 Codex CLI 0.144.5 通过一次性 3001 provider 和指定渠道 205 完成两轮原生 Responses：首轮实际执行一次 shell 命令并得到 `CODEX_TOOL_EXEC_OK`，第二轮携带 `custom_tool_call` 与 `custom_tool_call_output` 历史并正常完成。服务端两次均为 HTTP 200、`stream_status=done`。
- 测试结束后已将开发渠道 205 恢复为原始 `status=2`。正式数据库和正式容器未修改。

## 发布边界

- 本项 L2 已通过：首个语义事件前的 Codex metadata 断流现在能返回明确 502，并保留渠道重试资格。
- 渠道 205 的 namespace 请求仍由上游返回连接错误，这属于渠道能力或上游运行状态，不得表述为 namespace 已通过。
- 正式升级仍须和本轮其余 Codex Responses 兼容门禁一起执行，发布前重新备份正式数据库，并确认正式转换策略保持关闭。
