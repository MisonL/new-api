# CR: Claude output_config.effort 透传打断 Ollama 渠道

- 日期：2026-09-20
- 范围：`relay/channel/ollama/relay-ollama.go`
- 状态：已修复并验证

## 问题

`service/convert.go` 的 `ClaudeToOpenAIRequest` 在非 OpenRouter 分支新增了一段逻辑，
把 Claude Messages 的 `output_config.effort` 写入 `openAIRequest.ReasoningEffort`。
Ollama 渠道的 Claude 请求转换链是：

```
ollama.Adaptor.ConvertClaudeRequest
  -> openai.Adaptor.ConvertClaudeRequest
  -> service.ClaudeToOpenAIRequest        # 在此写入 ReasoningEffort
  -> openAIChatToOllamaChat               # 校验并拒绝非空 ReasoningEffort
```

`validateOllamaChatRequest`（`relay-ollama.go:240`，HEAD 既有代码）对非空
`ReasoningEffort` 直接报错：

```
reasoning_effort, verbosity, and reasoning are not supported by Ollama Chat; use think
```

因此任何带 `output_config.effort` 的 Claude 请求经 Ollama 渠道都会 400。
该错误信息本身指明了正确承载字段：`think`。

## 复现（修复前）

```
ReasoningEffort after convert = "high"
RESULT: ollama conversion FAILED: reasoning_effort, verbosity, and reasoning are not supported by Ollama Chat; use think
```

## 修复

在 `openAIChatToOllamaChat` 与 `openAIToGenerate` 的校验之前加入
`applyOllamaReasoningEffort`，把 effort 映射到 Ollama 原生 `think`：

- `low` / `medium` / `high` -> `think` 为对应字符串
- `none` -> `think` 为 `false`
- 其他取值 -> 保持 `reasoning_effort` 不变，交给校验显式报错（不静默丢弃）
- 调用方已显式设置 `think` -> `think` 优先，但仍清空 `reasoning_effort`，
  否则请求会因该字段被判失败

映射的取值集合与两处既有先例一致：`relay/channel/claude/relay-claude.go:416-434`
的 effort 分支同样只接受 low/medium/high，`docs/openapi/relay.json` 的
`reasoning.effort` 枚举亦为 `["low","medium","high"]`。

## 验证

| 命令 | 结果 |
|---|---|
| `go test ./relay/channel/ollama/ -count=1` | ok |
| `go test ./relay/... -count=1` | 37 包 ok，0 FAIL |
| `go test ./controller ./model ./relay/common ./relay/helper ./service -count=1` | 全部 ok |
| `go build ./...` | exit 0 |
| `gofmt -l`（改动文件） | 无输出 |

新增回归测试（`protocol_conversion_test.go`）：

- `TestClaudeEffortMapsToOllamaThink`：low/medium/high/none 四例映射正确
- `TestClaudeEffortDoesNotOverrideExplicitThink`：显式 think 优先且请求仍可通过校验
- `TestClaudeEffortUnknownLevelStillRejected`：无对应取值的 level 仍显式报错

变异验证（逐个撤掉修复，测试必须失败）：

| 撤掉 | 结果 |
|---|---|
| 整个 `applyOllamaReasoningEffort` 调用 | FAIL |
| unknown level 改为静默清空 | FAIL |

## 边界

- 逐个核查了同样复用 `openai.Adaptor.ConvertClaudeRequest` 的其余渠道
  （ali、baidu_v2、deepseek、gemini、minimax、moonshot、perplexity、siliconflow、
  volcengine、zhipu_4v），均透传或容忍 `ReasoningEffort`，不受影响。
- AWS Converse 曾作为候选受害者，实测不成立：`convertClaudeToConverseInput` 在
  转换前先执行 `rejectUnsupportedClaudeConverseFields`，该函数拒绝非空
  `OutputConfig`，请求在 Claude 层即 fail-closed 并给出明确信息。
- 使用 `RejectUnsupportedTextRequestFields` 的文本协议渠道（baidu、cohere、xunfei、
  coze、zhipu、cloudflare、tencent、palm）其 `ConvertClaudeRequest` 均直接返回
  not implemented，Claude 请求不可达。
- 未连接真实 Ollama 上游；本记录覆盖的是请求转换与校验语义，未覆盖真实上游行为。
