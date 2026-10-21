# DeepSeek thinking 回传兼容（跨协议渠道轮询）

## Summary

采样时间：2026-10-21 上午（Asia/Shanghai）。线上渠道 160（agentrouter.org，Anthropic 协议）在 2026-10-20 22:44 出现
`status_code=400, The content[].thinking in the thinking mode must be passed back to the API`。
本轮定位该错误的上游规则，并在网关侧补齐跨协议转换的兼容处理。

## 上游规则（实测）

直连 agentrouter.org 的 `/v1/messages`（model=deepseek-v4-flash）验证：

| 场景 | 结果 |
|---|---|
| 最后一条 assistant 消息含 `tool_use`、缺 `thinking` 块（请求带或不带 thinking 参数都一样） | 400，与线上日志文案一致 |
| 同上，带 thinking 块（文本非空或为空、带或不带 signature、UUID 假签名均可） | 200 |
| 更早轮次的 tool_use 缺 thinking | 200（只校验最后一条 assistant 消息） |
| 纯文本多轮缺 thinking | 200 |

OpenAI 协议端点（`/v1/chat/completions`）同样要求回传：assistant 消息含 tool_calls 但缺
`reasoning_content` → 400；`reasoning_content` 为空串 → 400；非空 → 200。

客户端行为：Claude Code 2.1.304 会把上游返回的 thinking 块（包括空文本 + UUID signature）原样回传，
旧轮次也保留。网关的 Anthropic 透传路径不丢块（隔离网关 + 回显上游逐字节比对验证）。

## 根因

用户的会话在 18 个渠道间轮询（Anthropic 协议渠道与 OpenAI 协议渠道混合）。由 OpenAI 协议渠道服务的
工具轮次，响应转换只在 `reasoning_content` 非空时生成 thinking 块；上游没给推理时，Claude Code 的
历史里就是裸 `tool_use`。下一跳打到 DeepSeek 的 Anthropic 端点即 400。

另外，agentrouter 返回的空 thinking 块带 UUID signature；dev 分支的 fail-closed 规则会把它判为 opaque
状态并整条 400（生产二进制 4527d7666 无此规则，是静默丢弃，升级后会成为新故障）。

## Changes

- `relay/channel/claude/thinking_echo.go`（新增）
  - `ensureDeepSeekThinkingEcho`：最后一条 assistant 消息含 `tool_use` 且无 thinking 块时，
    补一个空 thinking 块（无 signature）。上游实测接受该形态；不伪造推理文本。
  - 只按上游模型名（`IsDeepSeekModelName`）生效；真实 Anthropic 模型的 signature 是强制的，
    注入无签名块会被拒绝，因此不做处理。
- `relay/channel/claude/adaptor.go`
  - `ConvertClaudeRequest` 调用上述函数。该适配器只服务 APITypeAnthropic 渠道（含 DeepSeek 官方
    渠道包装器），OpenAI 协议渠道不经过这里。
- `relay/channel/claude/relay-claude.go`
  - `RequestOpenAI2ClaudeMessage`：OpenAI 协议客户端（Codex 等）的 assistant 消息带 tool_calls 但无
    reasoning 时，同样补空 thinking 块（DeepSeek 模型限定）。覆盖 OpenAI→Claude 的转换方向。
- `service/convert.go`
  - `ClaudeToOpenAIRequest`：DeepSeek 模型的「空 thinking + signature」按空块处理（保留空
    `reasoning_content`），不再 fail-closed。其他模型维持 fail-closed：真实 signature 可能是
    opaque 状态，OpenAI `reasoning_content` 无法承载。

## Verification

以下命令退出码均为 0：

```bash
go build ./...
go vet ./relay/channel/claude ./service
go test ./controller ./model ./relay/common ./relay/helper ./service \
  ./relay/channel/claude ./relay/channel/deepseek -count=1
go test ./service ./relay/... -count=1
```

端到端验证（隔离环境 + 本地回显上游）：

- 镜像 `new-api-local:dev`，commit=db2b41b8-dirty，隔离容器重建后 healthy。
- 通过隔离网关发送「裸 tool_use 历史 + DeepSeek 模型 + Anthropic 渠道」请求，回显上游收到的
  assistant 消息为 `['thinking', 'tool_use']`，thinking 文本为空、无 signature，与设计一致。
- 网关返回 200（回显上游的固定响应）。

新增测试：

- `relay/channel/claude/adaptor_thinking_echo_test.go`：DeepSeek 裸 tool_use 注入；已有 thinking 块
  （含 signature）原样保留；非 DeepSeek 模型不注入；纯文本轮次不注入。
- `relay/channel/claude/relay_claude_test.go`：OpenAI→Claude 方向 DeepSeek 注入与非 DeepSeek 不注入。
- `service/convert_deepseek_signed_thinking_test.go`：DeepSeek 空 thinking + signature 保留空
  reasoning_content；非 DeepSeek 模型仍报 opaque 错误。
- `service/convert_claude_request_test.go`：原 opaque 拒绝测试改用非 DeepSeek 模型，覆盖范围不变。

## Boundaries

- 上游行为以 agentrouter.org 实测为准；DeepSeek 官方端点与其他中转是否完全一致未逐个验证。
- OpenAI 协议端点对空 `reasoning_content` 返回 400（实测）。因此 OpenAI 协议方向的裸 tool_use 历史
  无法用空值满足，本轮只解决 Anthropic 协议方向与转换方向的注入；OpenAI 协议渠道的同类历史仍会由
  上游拒绝（这是上游规则，网关不伪造推理文本）。
- `pass_through_body_enabled` 或全局透传开启时不做注入（请求体原样转发）。
- 生产未重启、未发布；本轮只证明源码与测试层行为。
