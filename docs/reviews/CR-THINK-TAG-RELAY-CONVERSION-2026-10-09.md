# CR-THINK-TAG-RELAY-CONVERSION-2026-10-09

修复：上游把思考内容塞进 assistant `content` 字符串里的 DeepSeek 风格思考标签，
经 new-api 转换后原样出现在回复正文中，Claude Code 不把它识别为思考块。

方法：先复现与定位（读代码 + 探针实测），再按四种客户端协议分别接线，最后用
单元测试与端到端转换测试固定行为。本轮未提交，未改动其他会话在途改动的语义，
未触碰生产容器与生产数据库。

## 1. 问题与根因

症状：Claude Code 经 new-api（渠道 220）收到的回复正文里带思考标签。

根因（已在上一轮逐条实测确认）：

- Claude 路径由 `service/convert.go` 的 `streamResponseOpenAI2Claude` 负责把上游
  OpenAI 帧转成 Anthropic SSE，它无法向 `content` 注入标签；响应路径此前没有任何
  标签剥离动作。
- 标签只能来自上游渠道自己放在 `content` 里的文本，new-api 原样透传为
  `text_delta`，因此客户端把标签当成正文渲染。
- 渠道设置 `thinking_to_content` 只作用于 `RelayFormatOpenAI`，与 Claude 客户端
  无关，因此它不是本症状的来源（也不足以修复它）。
- 非 DeepSeek 命名的模型，`reasoning_content` 会被名字门控丢弃；这解释了为什么
  同样的标签在部分模型上表现为"正文里的标签"，在另一些模型上是"思考被吞掉"。

## 2. 设计约束（实现与测试均按此钉住）

- 只有**完整的一对**开闭标签才改写输出。单独一个开标签是普通英文
  （"I am thinking about it"），必须原样通过，否则会误改正常回复。
- 思考内容一律不丢弃：抽取出的文本进入客户端协议各自的 reasoning 字段
  （Claude 的 thinking 块、OpenAI 的 `reasoning_content`、Gemini 的 thought part、
  Responses 的 reasoning 事件）。
- 标签对之外的可见文本**逐字节**保持，不做 trim；流式路径上被暂存的尾字节在流
  结束时必须原样吐回。
- 渠道显式开启 `thinking_to_content`（且客户端是 OpenAI 格式）时跳过抽取：该设置
  的存在意义就是把思考放进正文标签，抽取等于撤销运营方的显式选择。
- 源码中的标签常量用 Go 转义书写（`\x3cthink\x3e`、`\x3c/think\x3e`，以及 V3 形式的
  U+FF5C/U+2581 变体），文件里不出现字面尖括号，避免编辑/对比工具吞掉它们。

## 3. 改动清单

新增（未跟踪，提交前需 `git add`）：

- `relay/common/think_tag.go`：标签常量、`SplitThinkTaggedContent`、
  `ThinkTagStreamSplitter`（跨帧重组）、`ExtractThinkTagsFromChatChunk`、
  `ExtractThinkTagsFromMessage`、`ExtractThinkTagsFromTextResponse`、
  `ThinkTagsExtracted`、`FinishThinkTagStream`、`FlushThinkTagStream`。
- `relay/common/think_tag_test.go`：非流式、逐 rune 流式等价、分块边界、未闭合回吐、
  前缀暂存、raw JSON 帧抽取、跨帧重组、终止帧回吐与幂等、消息级抽取与去重、
  无损性质（字符多重集不变）。
- `service/think_tag_claude_test.go`：Claude 流式/非流式把标签变成 thinking 块的
  回归测试，以及"没有抽取就没有 thinking 块"的因果测试。
- `relay/channel/openai/think_tag_stream_test.go`：`HandleStreamFormat` 的接线测试
  （Claude 客户端抽取生效；`thinking_to_content` 时不抽取、不分配状态）。

修改（均在在途文件上做加法，未触碰他人 hunk 语义）：

- `relay/common/relay_info.go`：`RelayInfo` 增加惰性字段 `ThinkTagSplitter`。
- `relay/channel/openai/helper.go`：`HandleStreamFormat` 在分派前抽取；
  `HandleFinalResponseWithError` 的 Claude 与 Gemini 分支在解析前调用
  `FinishThinkTagStream`（终止帧不经过 `HandleStreamFormat`）。
- `relay/channel/openai/relay-openai.go`：`OaiStreamHandler` 在扫描结束后、最后一次
  响应检查前 `FinishThinkTagStream`，使 OpenAI 重发、Claude/Gemini 终止转换看到
  同一份完整帧；`OpenaiHandler` 非流式抽取，并在被改写时强制重新序列化。
- `relay/channel/openai/responses_via_chat.go`：每帧抽取；流结束时把暂存字节经
  reasoning / output_text 事件吐出。
- `relay/channel/openai/chat_via_responses.go`：流结束、回退用量估算之前吐出暂存字节。
- `service/convert.go`：`streamResponseOpenAI2Claude` 与 `ResponseOpenAI2ClaudeWithError`
  放宽名字门控（`ThinkTagsExtracted` / 抽取命中即暴露 thinking），
  `ResponseOpenAI2GeminiWithError` 与 `StreamResponseOpenAI2GeminiWithError` 在抽取命中
  时补一个 thought part。

删除：`service/think_tag_split.go`、`service/think_tag_split_test.go`（实现迁到
`relay/common`，因为流式状态字段必须与 `RelayInfo` 同包，而 `service` 依赖 `relay/common`）。

## 4. 四种客户端协议覆盖

| 客户端 | 入口 | 抽取点 | 终止帧回吐 |
| --- | --- | --- | --- |
| Claude `/v1/messages` | `HandleStreamFormat` → `handleClaudeFormat` | 分派前 | `OaiStreamHandler` + `HandleFinalResponseWithError` |
| OpenAI `/v1/chat/completions` | `HandleStreamFormat` → `sendStreamData` | 分派前 | 同上（`sendStreamData` 前） |
| Gemini `:generateContent` | `HandleStreamFormat` → `handleGeminiFormat`；Gemini 渠道 `handleStream` | 分派前 | `HandleFinalResponseWithError` 的 Gemini 分支 |
| Responses `/v1/responses`（Codex） | `OaiChatToResponses*`（`responses_via_chat.go`） | 每帧解析前 | 扫描结束后经 reasoning/output_text 事件 |

非流式：Claude/Gemini 客户端由 `service` 的两个转换函数内部抽取；OpenAI 客户端在
`OpenaiHandler` 抽取后重新序列化；Responses 客户端在 `OaiChatToResponsesHandler` 抽取。

## 5. 验证（本轮实际运行的命令与结果）

- `go build ./relay/... ./service/... ./controller/...` → 退出码 0，无输出。
- `go test ./relay/common/ ./relay/channel/openai/ ./relay/channel/gemini/ -count=1`
  → 三个包全部 `ok`。
- `go test ./service/ -count=1` → `FAIL`，唯一失败为
  `TestSSRFGuardedTransportDoesNotPinHTTPProxyEndpoint`（见第 6 节，与本改动无关）。
- `go test ./controller ./model -count=1` → 两个包 `ok`。

边界说明：以上为包级单元与转换级测试，未在真实渠道 220 上跑端到端请求（开发库中
不存在该渠道，且本轮未连接真实上游）。"局部测试通过"不等于"全链路已验证"。

## 6. 既有失败（与本改动无关，已独立复现）

`TestSSRFGuardedTransportDoesNotPinHTTPProxyEndpoint` 断言经过 SSRF 保护的
传输仍把拨号地址交给代理端点，实际 `dialAddress` 为空。该失败与本改动无关，依据：

- 被测代码 `service/http_client.go` 与测试文件 `service/http_client_ssrf_test.go`
  相对 HEAD 无任何改动（`git diff HEAD` 为空）。
- `service/` 与 `common/`、`setting/` 下 SSRF/Fetch 相关文件均不在工作区改动列表中。
- 在 `/tmp/na-head` 用 `git worktree add --detach <HEAD>` 检出已提交版本单独运行该
  测试，同样 `FAIL`（`expected "proxy.example:8080"`，`actual ""`），证明它在未应用
  本轮改动时即失败，属既有问题。

## 7. 未覆盖的边界（明确列出，不当作已修复）

- 上游为**原生 Responses API**、客户端也是 Responses 的路径（`relay_responses.go`）
  未接入抽取。原生 Responses 上游不会产生 DeepSeek 风格正文标签，故本轮不做。
- Gemini 渠道 + OpenAI 客户端的**非流式**响应由 Gemini 适配器自行序列化，未接入抽取。
- 上游把 `content` 作为结构化数组（而非字符串）返回时不做改写，避免把消息 JSON 形状
  从数组改成字符串；此时标签不会被抽取。
- 同一帧内上游同时在 `reasoning_content` 给出同样思考时按"包含即跳过"去重；跨帧的
  重复（上游在两处交错发送同一段文本）无法识别。

## 8. 提交状态

未提交。`docs/reviews/README.md` 由其他会话在途修改，本文件暂未加入索引。
