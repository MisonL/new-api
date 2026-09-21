# Responses 流终止错误审计

## 范围

- 采样日期：2026-07-17。
- 目标：Responses 和 Responses Compact 在已向客户端写出语义事件后遇到上游显式错误或连接中断时，必须以 SSE `event: error` 明确终止。
- 边界：不改变协议转换策略、渠道能力配置、数据库 Schema、计费公式和非 Responses 流行为。

## 根因

- 控制器原先在 HTTP writer 已提交后仍尝试调用 JSON 错误响应，客户端只能看到 HTTP 200 和不完整正文。
- 第一轮修复让控制器在已提交 Responses SSE 后写入 `event: error`，但上游在正文后直接断开时，Responses handler 为避免跨渠道重放而返回 `nil`，错误没有继续传播到控制器。
- 上游在正文后显式返回 429、502 或 `response.failed` 时，统一重试入口仍可能切换渠道，存在把第二个上游响应拼入同一 SSE 的风险。

## 修复

- Responses handler 在语义 payload 后发生传输中断时返回 `upstream_transport_interrupted`，并设置 `skipRetry`。
- 首个语义事件前的断流保持可重试；只有心跳或缓冲 prelude 时仍允许恢复。
- 统一重试入口检测 Responses prelude 状态。writer 已提交且 prelude 标记为不安全后，禁止任何跨渠道重试。
- 控制器对正文后错误统一写入标准 SSE `event: error`，不再追加 JSON 响应。

## L0 和 L1 验证

```text
go test -count=5 -timeout=90s ./controller -run 'TestShouldRetry(StopsAfterResponsesPayloadWasWritten|AllowsResponsesPreludeRecovery)|TestWriteRelayErrorResponseEmitsResponsesErrorAfterPayload'
exit 0

go test -count=5 -timeout=90s ./relay/channel/openai -run 'TestOaiResponsesStreamHandler(ReturnsTerminalErrorAfterPayload|ReturnsRetryableErrorBeforeFirstWrite|KeepsCodexMetadataPreludeRetryable|FlushesCodexMetadataPreludeOnCompleted)'
exit 0

go test -count=1 -timeout=120s ./controller ./model ./relay/common ./relay/helper ./relay/channel/openai ./relay ./service
exit 0

go vet ./controller ./model ./relay/common ./relay/helper ./relay/channel/openai ./relay ./service
exit 0

go test -race -count=1 -timeout=3m ./controller ./relay/channel/openai ./relay
exit 0

go test -count=1 -timeout=10m ./...
exit 0

git diff --check
exit 0
```

## L2 隔离环境验证

- `new-api-local:dev` 和 3001 容器 build-info 均为提交 `d0abcb9585540e9030ad54bb8e35aa0e250b2774`。
- `/api/status` 成功，容器状态为 `running healthy 0`。
- 开发渠道 205 的 `gpt-5.6-sol` function tool 请求返回 HTTP 200、`response.completed` 和 `extra_lookup` 调用。
- 临时 fake upstream 先发送 `response.created` 和 `response.output_text.delta`，随后主动断开连接。3001 对客户端返回 HTTP 200，保留 delta，追加 1 个 `event: error`，且没有 `response.completed`。
- 临时渠道 920101、ability 和令牌 920001 已删除；开发渠道 205 已恢复 `status=2`。

## 发布门禁

- 正式发布只重建应用容器，不重建 PostgreSQL 和 Redis。
- 发布前保留现有数据库备份和上一版镜像。
- 发布后观察 429、502、524、570、`write responses stream error failed`、无终止事件、工具调用、压缩请求、CPU、内存和容器重启次数。
- 服务不健康、工具丢失、压缩回归、无终止事件持续出现或错误率明显抬升时触发回滚。
