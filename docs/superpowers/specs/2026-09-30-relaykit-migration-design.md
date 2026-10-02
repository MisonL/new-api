# 协议转换层收敛到上游 relaykit

采样时间：2026-09-30
本地基线：`HEAD` = `dbfbc096f`（dev 分支，领先 `origin/dev` 56 个提交）
上游参照：`upstream/main` = `789c97019`（2026-09-28）

行号引用以本文件声明的基线提交为准。工作区有其他会话的未提交改动，工作区行号不采用。

## 1. 定位

**上游已经完成了本仓库正在规划的同一迁移。** `upstream/main` 的 `service/convert.go` 只剩 27 行纯委托，协议转换逻辑全部在独立 module `relaykit` 中，宿主有 172 个文件 import `relaykit/types`。

因此本文件不是一份从本地现状推演的迁移方案，而是一份**差异对照清单**：逐项列出本地与上游完成态的差异，标注每项的处置方式。这样做的理由：

1. 上游是同一迁移的完成态，且有测试与 golden 快照——是最可靠的参照系
2. 从本地现状推演容易得出上游已经证伪的路径（本文件前四稿即如此：曾规划"改 210 个 import 点"，而上游用类型别名做到零调用点变更）
3. 用户诉求是"替换整体架构为上游实现"，对照清单直接服务于该目标

目标保持不变：

- 协议转换层采用上游 `relaykit` 架构
- **兼顾思考等级与 thinking 历史回传**（见第 5 节）
- 本地增加的功能在改造后的基础上重新设计，取最优雅最高效的处置

## 2. 上游完成态

| 项 | 上游事实 | 核验方式 |
|---|---|---|
| 独立 module | `module github.com/QuantumNous/new-api/relaykit`，go 1.25.1，可 `GOWORK=off go test` | `git show upstream/main:relaykit/go.mod` |
| 接入方式 | 根 `go.mod` 加 `require … v0.0.0` + `replace … => ./relaykit` | `git show upstream/main:go.mod` |
| 宿主改造规模 | 172 个宿主文件 import `relaykit/types`；262 个宿主文件 import relaykit 下某包 | `git grep -l` 计数 |
| 宿主残留 | 宿主 `dto/` 剩 8 个文件；宿主 `types/` 剩 4 个文件 | `git ls-tree upstream/main dto/ types/` |
| 常数迁移手法 | **Go 类型别名，零调用点变更**（见下） | `git show upstream/main:constant/endpoint_type.go` |
| 转换逻辑 | `service/convert.go` 27 行纯委托 | `git show upstream/main:service/convert.go \| wc -l` |
| 成熟度 | 34 个测试文件 / 8430 行测试；24 个 golden 快照；仅有 2 处 TODO，无未实现桩 | `git ls-tree`、`golden_test.go` |

**别名机制原文**（`upstream/main:constant/endpoint_type.go`）：

```go
package constant

import "github.com/QuantumNous/new-api/relaykit/types"

// EndpointType moved to types with the conversion kit; aliases keep host
// code compiling unchanged.
type EndpointType = types.EndpointType

const (
	EndpointTypeOpenAI         = types.EndpointTypeOpenAI
	EndpointTypeAnthropic      = types.EndpointTypeAnthropic
	// …
)
```

所以「必须改 N 个 import 点」这类判断必须先查上游是否已用别名规避。凡宿主包是某符号的自然归属（如 `constant` 承载 `EndpointType`），别名转发即可。

注意上游两种手法并用：`constant/endpoint_type.go` 用别名；`relay/channel/openai/adaptor.go` 则**直接改 import** 到 `relaykit/types`。两个包可并存，`relay/common/relay_info.go` 同时有 `"…/relaykit/types"` 与 `hosttypes "…/new-api/types"`。

## 3. 差异对照表

### 3.1 上游更强 → 直接采用

| 能力 | 本地现状 | 上游实现 | 处置 |
|---|---|---|---|
| 思考等级渲染 | `relay/claude_handler.go:55-104` 硬编码 `claude-opus-4-6/4-7` 前缀判断 | `reasoning.RenderClaude` + `claudeCapabilitiesFor`（`relaykit/relayconvert/reasoning/claude.go:35-74`）覆盖 fable-5、mythos-5、Opus 4.5/4.6/4.7/4.8/5、Sonnet 4.6/5，含 `defaultThinking`、`supportsEffort/XHigh/Max`、budget↔effort 互转与冲突诊断 | 采用上游 |
| hosted 工具 | 散落在 `service/convert.go` 与 `relay/common/` | `relaykit/relayconvert/internal/toolconv/`（`decode.go`、`encode.go`、`hosted_values.go`、`model.go`、`response.go`、`response_artifacts.go`），覆盖 10 种 hosted 工具：`KindWebSearch`、`KindCodeExecution`、`KindComputerUse`、`KindFileSearch`、`KindImage`、`KindMCP`、`KindURLContext`、`KindWebFetch`、`KindFunction`、`KindNative` | 采用上游 |
| 降级可观测性 | `ConversionDiagnostic` 本地零命中 | `convdiag` + `types.ConversionDiagnostic`（Code/Path/Message/Severity/From/To） | 新增能力 |
| 转换质量分级 | 无 | `Good`/`Fair`/`Discouraged` + 4×4 转换矩阵 | 新增能力 |
| 测试基线 | CI 不跑 `go test` | 24 个 golden 快照 + `-update` 机制 | 新增能力 |

### 3.2 上游已覆盖 → 直接采用

`service/convert.go` 的 13 个导出函数（`ClaudeToOpenAIRequest`、`IsDeepSeekModelName`、`HasDeepSeekModelName`、`NormalizeCacheCreationSplit`、4 个 `Stream/Response…WithError` 及其 4 个旧包装、`GeminiToOpenAIRequest`）对应的协议映射逻辑，上游均有实现（`relaykit/relayconvert/internal/` 下的 `claude_messages`、`oai_chat`、`oai_responses`、`gemini_chat`、`shared` 五个族）。

其中 `NormalizeCacheCreationSplit` 等辅助函数在 relaykit 中同名存在（`oai_chat/to_claude_messages_resp.go:107`）。

**chat↔responses 兼容层也已覆盖**（4.5.1 详述）：上游把该能力放在 `relaykit/relayconvert/internal/{oai_chat,oai_responses}/`，并在宿主保留 30 行转发 shim `service/openai_chat_responses_compat.go`，其 6 个导出函数中 5 个与本地 `service/openaicompat/` 同名；第 6 个 `ResponsesFinishReasonFromStatus` 函数名本地无对应，但**同能力本地有**（`responsesIncompleteFinishReason`，`responses_to_chat.go:246`，见 4.5.1）。本地该包**大部分能力上游已覆盖**，零覆盖的只有命名空间工具、tool_search、custom tool bridge 门控与 rules 策略扩展。

### 3.3 上游缺失 → 需打补丁（2 项）

**补丁 1：Claude thinking block 映射与输入校验对齐**

上游两个请求转换器的 content block switch 均无 `thinking` case。`internal/claude_messages/to_oai_chat_req.go:181`、`internal/claude_messages/to_oai_responses_req.go:165` 两处 switch 本身无 `default`（同文件其他 switch 有，如 `:269`、`:265`），未知 block 被静默穿透丢弃。

改为：

1. 新增 `case "thinking"`，将 `ClaudeMediaMessage.Thinking` 写入目标协议的 reasoning 字段（Chat 写 `Message.ReasoningContent`，Responses 写 reasoning item）。opaque thinking（`Thinking` 空但 `Signature` 非空）与 `redacted_thinking` 显式报错，不伪装为文本
2. 保留非 assistant 的 role 门控：非 assistant 的 `thinking` / `redacted_thinking` 静默跳过。本地有测试锁定（`TestClaudeToOpenAIRequestIgnoresNonAssistantThinkingBlocks`、`…RedactedThinkingBlocks`），上游无此门控
3. 补 `default:` 分支，未知 block 显式报错。本地对应 `service/convert.go:355`

**补丁 2：DTO presence 保真**

上游 `relaykit/dto` 的可选标量字段缺失显式零值追踪。**注意：本地已有这些能力**，补丁 2 的实质是**把本地既有实现带进 relaykit**，而非从零设计：

| 能力 | 本地位置 | 本地生产使用点 |
|---|---|---|
| `OpenAIResponsesRequest.UnmarshalJSON`（未知字段保留；同一机制另见 `StreamOptions`、`Reasoning`、`OpenAIResponsesCompactionRequest`） | `dto/json_extra_fields.go`（`collectUnknownJSONFields` / `marshalWithExtraJSONFields`）、`dto/openai_request.go` 等 | 4 个类型各自启用 |
| `HasIncludeUsageField` / `RemoveIncludeUsage` | 同上 | 各 1 |
| `HasIncludeObfuscationField` | 同上 | 1 |

理由：AGENTS.md 第 3 节要求「客户端 JSON 解析后再转发给上游的可选标量字段，必须用带 `omitempty` 的指针类型保留显式零值」。

### 3.4 本地独有 → 逐项处置

见第 4 节。

## 4. 本地独有能力清单与处置

**盘点口径**：以下清单同时核对 `HEAD`（`2cf100a1d`）与工作区两套口径，差异处逐项标注。工作区含其他会话的未提交改动（`dto/` 6 个文件、`relay/common/` 4 个文件），本清单不触碰、不回退，仅在计数受影响处注明。未逐项核验的边界见 4.5.5。

### 4.1 属协议层语义，值得提上游

盘点已完成，取证见第 11 节。本地独有项中，多数属部署关切（4.2）、宿主业务域（4.5.3）或上游已有更好实现（3.1）。以下两项确属协议层语义，relaykit 无等价实现：

- **`ParseContentStrict` / `ParseToolCallsStrict`（4.4）**：严格解析失败即报错，与 AGENTS.md「禁止静默容错」一致。relaykit **无 `*Strict` 变体**，且其对应实现**静默容错**：
  - `relaykit/dto/openai_request.go:624` `ParseToolCalls()`：`if err := kitutil.Unmarshal(m.ToolCalls, &toolCalls); err == nil { return toolCalls }` 之后直接 `return toolCalls`——解析失败返回空切片，不报错
  - `relaykit/dto/openai_request.go:686` `ParseContent()`：`arrayContent, ok := m.Content.([]any); if !ok { return contentList }`——类型不匹配时返回空列表，不报错

  这两处正是本地严格版本要修的问题，属协议层语义，应提上游
- **think 标签提取族（4.2）**：判定为部署关切是因它受模型名门控，但剥离本身是文本协议处理。若把门控与剥离拆开，剥离部分属协议层

### 4.2 属部署关切，保留在宿主

| 能力 | 位置 | 判定依据 |
|---|---|---|
| 响应方向 reasoning 暴露策略 | `service/convert.go:526`（`shouldExposeReasoningAsClaudeThinking`），调用点 `:749`（流式）、`:1049`（非流式） | 模型名门控；且见第 6.1 节的撤回理由 |
| DeepSeek 空 thinking block 保留 | `service/convert.go:365` | 模型名门控 |
| think 标签提取族 | `service/convert.go:425`、`:475`、`:485`、`:489`、`:554`、`:597`、`:629`，调用点 `:103` | 模型名门控（`hasDeepSeekModelName`） |
| `context_compaction` 归一化 | `service/openaicompat/`、`controller/relay.go` | 上游零命中 |
| 加密内容探测 | `relay/common/responses_input_filter.go` | 硬编码宿主 marker `newapi.synthetic.compact:` |
| 本地专有错误契约 | `types/error.go` | 见第 4.3 节 |

**已移出本节的项**：工具名契约与别名还原（`dto/tool_name.go` + `relay/common/responses_tool_names.go`）早期列在此处，但它是跨协议工具名映射，上游零覆盖，**属协议层**——归类见 4.5.3，归属决策见第 10 节第 16 项。门控（`IsOpenAICompatibleResponses`）留在宿主，别名生成与响应侧还原本身属协议层。

`RetainEmptyReasoning` 钩子设计（供上表第二项落地）：`convmeta.Options` 增加 `RetainEmptyReasoning func(modelName string) bool`，`nil` 表示不保留（与上游一致）。判定输入均在 relaykit 内部可得——`emptyThinkingBlock` 由补丁 1 新增的 `case "thinking"` 自行计算，`hasTools` 由 relaykit 自行累计——钩子只回答策略。

### 4.3 本地专有契约（上游完全不存在）

本地 `types/error.go` 定义 37 个 `ErrorCode` 常量，上游 `relaykit/types/error.go` 定义 33 个，**上游独有 0 个、本地独有 4 个**：

| 符号 | 生产引用文件数 | 说明 |
|---|---|---|
| `ErrorCodeUpstreamTransportInterrupted` | 14（含测试共 25） | 上游传输中断 |
| `ErrorCodeChannelConcurrencyExceeded` | 3 | 渠道并发超限 |
| `ErrorCodeUpstreamRequestTooLarge` | 3 | 上游请求体过大 |
| `ErrorCodeChannelConcurrencyUnavailable` | 2 | 渠道并发不可用 |

另有 2 个本地独有函数（上游全仓 0 命中）：`IsUpstreamTransportInterruptedError`（生产 8，含测试共 9）、`IsOpenAIRequestParameterError`（5，无测试引用）。

**勘误**：本文档早期版本写「上游宿主 `types/` 没有 `error.go`」。字面为真，但会误导——上游把该文件整体迁到了 `relaykit/types/error.go`，其中 33 个常量与本地同名同值。准确表述是「上游 `error.go` 存在且覆盖本地 37 个中的 33 个，缺 4 个」。同理，早期列的「25 / 9 / 5」是**含测试文件**的口径，非生产引用数。

需决定这 4 个常量迁入 `relaykit/types` 还是保留宿主。若 relaykit 转换器需据此产生错误，则必须迁入。

另注依赖方向：本地 `types/error.go:11` import 宿主 `common`，上游 `relaykit/types/error.go:10` import `relaykit/relayconvert/kitutil`——即 `common` 的相关 helper 已被抽到 `kitutil`，`types` 迁移实际牵动 `common → kitutil` 替换。逐文件核验本地 `types/`（基线 10 个非测试文件）：仅 `error.go` 与 `rw_map.go` 依赖宿主 `common`，**其余 8 个无宿主依赖**。

### 4.4 本地 DTO 独有方法

**本节只覆盖 3 个文件，不代表 dto 包的全貌。** 按 `接收者.方法名` 机械对比本地 `dto/{openai_request,openai_response,claude}.go` 与上游 `relaykit/dto/` 同名文件的方法集：

| 文件 | 本地 | relaykit | 本地独有 | relaykit 独有 |
|---|---|---|---|---|
| `openai_request.go` | 43 | 31 | **13** | 1 |
| `openai_response.go` | 25（HEAD 22） | 20 | **9**（HEAD 6） | 4 |
| `claude.go` | 31 | 30 | **1** | 0 |
| 三个文件合计 | | | **23**（HEAD 20） | 5 |

**⚠ 全包口径远大于此**：本地 `dto/` 共 **394 个函数**，relaykit/dto **273 个**。单 `dto/channel_settings.go` 本地就有 **61 个函数**（`^func` 口径）、relaykit 同名文件 29 个；按 `func (recv) Name` 提取后求差集，其中 **41 个本地独有方法**（`ResponsesCompactModeOrDefault`、`ResolveResponsesChannelCapability`、`MarkResponsesCompactAutoFallback` 等），逐个 `git grep -cw` 在上游全仓命中数为 0。

本节之所以只列 3 个文件，是因为它们承载**协议转换**语义；其余文件的方法归属见 12.0.2——**它们同样必须处置**，不能因「宿主业务域」而留在原地（`dto` 是一个包，类型迁走则方法无家可归）。完整的方法级差集仍是阶段 2 的开工前置。

**勘误**：本文档早期版本写「13 个方法」。13 只是 `openai_request.go` 单文件的数量，漏算了 `openai_response.go` 与 `claude.go`。早期表格中 7 项计数有 5 项错误，实测对照如下：

| 方法 | 早期声称 | 实测 | |
|---|---|---|---|
| `ParseContentStrict` | 20 | **12** | 错 |
| `ParseToolCallsStrict` | 16 | **11** | 错 |
| `Clone` | 13 | **1** | 错。早期口径把 `http.Request.Clone`、`http.Transport.Clone`、`common.ResponsesRequestMetadata.Clone` 一并计入；dto 的 `Clone` 实际只被 `service/responses_codex_local_compaction_recovery.go:205`（`Reasoning.Clone`）与 `:224`（`StreamOptions.Clone`）调用 |
| `HasCompactionTrigger` | 10 | **9** | 错 |
| `HasCompactionOutput` | 4 | **2** | 错 |
| `EffectiveReason` | 2 | **2** | 对 |
| `SearchToolNameByToolCallId` | 1 | **1** | 对 |

23 个方法的完整清单与生产引用文件数（工作区口径；排除 `_test.go` 与 `dto/` 自身）：

| 文件 | 方法 | 生产引用 |
|---|---|---|
| `openai_request.go` | `Message.ParseContentStrict` | 12 |
| | `Message.ParseToolCallsStrict` | 11 |
| | `OpenAIResponsesRequest.HasCompactionTrigger` | 9 |
| | `OpenAIResponsesRequest.UnmarshalJSON` | 5 |
| | `Reasoning.UnmarshalJSON` | 5 |
| | `StreamOptions.UnmarshalJSON` | 5 |
| | `Reasoning.Clone` / `StreamOptions.Clone` | 1 |
| | `Reasoning.MarshalJSON` / `StreamOptions.MarshalJSON` | 2 |
| | `StreamOptions.HasIncludeUsageField` / `HasIncludeObfuscationField` / `RemoveIncludeUsage` | 各 1 |
| `openai_response.go` | `ResponsesStreamResponse.GetOpenAIError` | 9 |
| | `Usage.EnsureResponsesTokenDetailObjects` | 3 |
| | `IncompleteDetails.EffectiveReason` | 2 |
| | `OpenAIResponsesResponse.HasCompactionOutput` / `GetQuality` / `GetSize` / `HasImageGenerationCall` | 各 2 |
| | `Usage.GetOutputTokenDetails` | 2 |
| | `ChatCompletionsStreamResponse.GetError` | 1 |
| `claude.go` | `ClaudeRequest.SearchToolNameByToolCallId` | 1 |

两点提示：

- **HEAD 与工作区差 3 个方法**。`ResponsesStreamResponse.GetOpenAIError`、`ChatCompletionsStreamResponse.GetError`、`Usage.EnsureResponsesTokenDetailObjects` 只存在于工作区（`dto/openai_response.go` 工作区 25 个方法、HEAD 22 个）。这 3 个属其他会话的未提交改动。迁移评估应以工作区为准，但须与那些会话确认最终形态
- **relaykit 独有 5 个方法**（迁移会丢的能力）：`GeneralOpenAIRequest.MarshalJSON`、`CachedTokenDetails.HasTokens`、`InputTokenDetails.Clone`、`InputTokenDetails.CacheCreationTokensTotal`、`ResponsesOutput.MarshalJSON`。前三个对应 relaykit 的 `dto/usage_merge.go` 与 `dto/reasoning_state.go`（本地无同名文件）——本地的 `Usage.EnsureResponsesTokenDetailObjects` / `GetOutputTokenDetails` 与 relaykit 的 `InputTokenDetails.*` 是**同一能力域的两套实现**，迁移时须做语义对齐，不能简单二选一

其中 `ParseContentStrict` / `ParseToolCallsStrict` 是严格解析（失败即报错），与 AGENTS.md「禁止静默容错」一致；`EffectiveReason` 属推理相关，需单独确认 relaykit 对应机制。

### 4.5 本地独有包与文件

#### 4.5.1 `service/openaicompat/`（9 个非测试文件，基线 3072 行）

上游**无此包**，但**有等价能力**——上游把 chat↔responses 兼容层放在 `relaykit/relayconvert/internal/{oai_chat,oai_responses}/`，并在宿主留了一个 30 行的纯转发 shim `service/openai_chat_responses_compat.go`。它导出 6 个函数，其中 **5 个与本地 `service/openaicompat/` 同名同义**（`ChatCompletionsRequestToResponsesRequest`、`ResponsesRequestToChatCompletionsRequest`、`ChatCompletionsResponseToResponsesResponse`、`ResponsesResponseToChatCompletionsResponse`、`ExtractOutputTextFromResponses`）；第 6 个 `ResponsesFinishReasonFromStatus` 函数名本地无对应，但同能力本地有（`responsesIncompleteFinishReason`，`responses_to_chat.go:246`；本地版大小写不敏感且用 `EffectiveReason()`，比上游更宽容）。**「上游无此包」不等于「上游无此能力」**，逐项对照：

| 本地能力 | 位置 | 上游状态 |
|---|---|---|
| Chat↔Responses 请求/响应互转 | `chat_to_responses.go:207`、`chat_to_responses_response.go:102`、`responses_to_chat_request.go:758`、`responses_to_chat.go:12` | **完全覆盖**：`relaykit/relayconvert/internal/oai_chat/to_oai_responses_{req,resp}.go`、`internal/oai_responses/to_oai_chat_{req,resp}.go`，经 `request_compat.go:60` 与 registry 暴露 |
| 命名空间工具（namespace tools）桥接与还原 | `responses_to_chat_request.go:23`、`responses_input_tools.go`，约 20 个符号（`convertResponsesNamespaceToolToChat`、`extractResponsesToolNamespace`、`flattenResponsesNamespaceToolName`、`parseResponsesNamespaceTools` 等） | **零覆盖**：`git grep -ln namespace upstream/main -- 'relaykit/' 'relaykit/relayconvert/' 'service/' 'relay/'` = **0 文件** |
| tool_search 还原 | `responses_to_chat.go`（`parseResponsesToolSearchOutputTools`、`responsesToolSearchArguments`、`responsesToolSearchProxyChatTool`） | **零覆盖**：上游全仓唯一命中是 `relaykit/dto/claude.go:615` 的 `ToolSearchRequests int`（计费计数字段，非协议概念） |
| custom tool bridge（显式门控） | `EnableCustomToolBridge`，9 个非测试文件 | **部分覆盖**：上游 `custom_tool_call` 出现在 6 个 relaykit 文件（含测试口径；非测试 4）且**不门控**；`EnableCustomToolBridge` / `CustomToolBridge` 在上游 `relaykit/`、`service/` 均 **0 命中**。本地是「默认拒绝、显式开启」，上游是无条件处理 |
| 协议转换规则策略（rules 模式） | `policy.go:40` `FindProtocolConversionRulePolicyWithFilter`、`setting/model_setting/global.go:28` `Rules`、`:65` `ProtocolConversionRule` | **部分覆盖**：上游 `setting/model_setting/global.go:14` 有 `ChatCompletionsToResponsesPolicy` 与 `:22` `IsChannelEnabled`，但**无 `Rules`、无 `ProtocolConversionRule`**（全仓 0 命中）。本地是超集扩展（`HasRules`、`LegacyRule`、`UnmarshalJSON`、`normalizeProtocolEndpoint`） |
| 模型模式匹配语义 | `regex.go` `matchAnyRegex` | **语义不同**：本地空 patterns → `true`（匹配全部）；上游 `service/openai_chat_responses_mode.go:15` `matchAnyModelPattern` 空 patterns → `false`（不匹配任何）。迁移须显式决定取哪个语义 |
| 严格拒绝行为（`Rejects…` 系列：不支持的 role/include/previous_response_id、加密 tool output、嵌套 tool_search、别名冲突、重复命名空间工具名） | `responses_to_chat_request.go`、`chat_to_responses.go` 多处 | **部分覆盖**：上游有 `validateResponsesRequestChatUnsupportedFields`（`internal/oai_responses/to_oai_chat_req.go:119`）与 `ValidateRequestChatUnsupportedFields`（`:139`），但覆盖范围窄于本地 |

消费者（生产，**3 个文件**）：`service/openai_chat_responses_compat.go`（21 处引用）、`service/openai_chat_responses_mode.go`（5 处）、`relay/responses_via_chat.go:104`（调 `…WithState`，需 toolState 往返）。**注意**：早期版本写「仅 2 个文件」是错的，漏了第 3 个；且该包的收敛边界不是「只需处置这几个文件」——见 12.15，按能力切分在函数粒度不成立，该包整体保留。

#### 4.5.2 `relay/common/` 本地独有文件

上游 `relay/common/` 有 17 个文件（含测试；非测试 10），本地 29 个（含测试；非测试 16）。**上游没有任何 `responses_*` 文件**。本地独有并逐项核验（计数为**该文件全部导出符号的并集**，排除 `_test.go` 与定义文件自身）：

| 文件 | 行数 | 能力 | 生产引用文件数（HEAD / 工作区，**该文件全部导出符号的并集**，排除 `_test.go` 与定义文件） | 上游状态 |
|---|---|---|---|---|
| `responses_input_filter.go` | 1034 | 加密内容探测与剥离、`context_compaction` 归一化、input item ID 归一化、从错误文本反解 input 路径、工具形状探测、arguments 对象↔字符串转换 | 11 / 11 | **零覆盖** |
| `responses_tool_names.go` | 536 | 工具名双向归一化与响应侧还原（别名注册表 + gjson/sjson 字节级重写） | **0 / 3** | **零覆盖**（上游 `NormalizeToolName` 全仓 0 命中） |
| `responses_stream_observation.go` | 507 | 每上游尝试的协议级流证据（诊断用，不参与结算） | 15 / 16 | **零覆盖** |
| `task_route.go` | 180 | 无凭证的任务路由意图（Vidu 动作分类等） | 4 / 4 | 宿主业务域 |
| `responses_encrypted_replay.go` | 131 | 不透明加密重放决策/审计分类 + 「不支持」错误构造 | 7 / 7 | **零覆盖** |
| `channel_route.go` | 120 | 渠道健康路由身份指纹 | 8 / 8 | 宿主业务域 |
| `responses_reasoning_context.go` | 118 | 渠道能力门控 `reasoning.context` 转发/剥离、逻辑模型 effort 后缀传递 | 3 / 3 | **零覆盖** |
| `responses_compact.go` | 13 | compaction item 类型谓词 | 6 / 6 | **零覆盖** |
| `request_context.go` | 14 | `GinRequestContext(c)` 取 `*gin.Context` 的 context | 13 / 13 | 宿主工具 |

上游 `relay/common/` 独有的 `conversion_diagnostics.go`、`response_model.go`、`tool_usage.go` 已逐文件读取，**均未吸收上述任何本地能力**——它们分别是转换丢失诊断、响应模型漂移观测、计费工具计数，与本地关切不同。

**⚠ 整个工具名归一化能力尚未提交**：`relay/common/responses_tool_names.go` 及其两个测试文件未被 git 跟踪（`git status --short` 显示 `??`），**且它的 3 个调用点也全部位于未提交的改动中**——`relay/responses_handler.go:1123`（`NormalizeResponsesToolNamesWithRestorer`）、`relay/channel/openai/relay_responses.go:912` 与 `:1296`（`RestoreRawJSON`）、`relay/common/relay_info.go` 的 `ResponsesToolNameRestorer` 字段。**以上 4 处行号是工作区口径**（该能力在基线上不存在，故无基线行号可给）。这解释了上表「HEAD 0 / 工作区 3」的差异：**在 HEAD 上该能力完全不存在**。

这与 `docs/reviews/CR-UNTRACKED-SOURCE-DEPENDENCY-2026-09-22.md` 记录的问题同型——未跟踪源文件被已跟踪文件依赖，移走即构建失败。此处更严重：**能力本体与全部调用点都未提交**。迁移前必须先纳入版本控制，且须与拥有这些改动的会话确认最终形态（见第 10 节第 19 项）。

另注：本文档早期版本列出的 `relay/common/responses_stream_options_compat.go` **不存在**（`ls` 返回 No such file or directory），系误记。本地实际的 6 个非测试 `responses_*.go` 为上表所列。

#### 4.5.3 本地独有 `dto/` 文件（11 个非测试）

按是否属协议转换层归类：

| 文件 | 行数 | 归类 | 关键符号生产引用数 | 上游状态 |
|---|---|---|---|---|
| `responses_tools.go` | 111 | **协议层** | `EffectiveTools` 8 | **零覆盖**，须迁入 |
| `tool_name.go` | 58 | **协议层** | `NormalizeToolName` 5（`IsValidToolName`、`ToolNameMaxLength` 均为 0） | **零覆盖** |
| `opaque_openai_responses_request.go` | 64 | **协议层** | `OpaqueOpenAIResponsesRequest` 6 | **零覆盖** |
| `responses_input_token_meta.go` | 462 | **协议层** | 27 个方法**全未导出**，仅 dto 包内使用；跨包 0 | **零覆盖** |
| `json_extra_fields.go` | 61 | **协议层** | 4 个符号**全未导出**，供 `openai_request.go`、`openai_responses_compaction_request.go` 使用 | **零覆盖** |
| `header_profile.go` | 397 | 宿主业务域 | `HeaderProfileStrategy` 13 | 无需上游等价 |
| `header_profile_version.go` | 294 | 宿主业务域 | `ValidateHeaderProfileVersionMeta` 1 | 无需上游等价 |
| `channel_health.go` | 93 | 宿主业务域 | 各 1–3 | 无需上游等价 |
| `channel_routing.go` | 59 | 宿主业务域 | `ChannelRouteSelectionDiagnostics` 9 | 无需上游等价 |
| `responses_routing.go` | 48 | 宿主业务域（边界） | `ResponsesRoutingTrace` 2 | 无需上游等价 |
| `header_policy.go` | 16 | 宿主业务域 | `HeaderPolicyMode` 8、`UserAgentStrategy` 6 | 无需上游等价 |

归类依据：6 个宿主域文件**零协议类型 import**，被 `controller/`、`middleware/distributor.go`、`service/channel_header_*` 等宿主路径消费。

**⚠ 关键纠正：本表只列了「本地独有文件」，不覆盖「同名但字段不同」的文件。** 早期版本据此推断「宿主域文件不应被收敛波及」——**该推断在 Go 里不成立**。`dto` 是一个包，`channel_settings.go`、`user_settings.go`、`openai_request.go` 等同名文件承载了大量宿主独有内容，它们**同样在迁移范围内**：

| 同名文件 | 本地独有字段/方法 | 说明 |
|---|---|---|
| `dto/channel_settings.go` | **20 个本地独有字段**（基线口径，工作区 21；`HeaderProfileStrategy`、`ResponsesCompactMode*`、`UserAgentStrategy` 等）+ **41 个本地独有方法** | 上游同名文件只有 4 个协议相关字段；41 个方法上游全仓 0 命中 |
| `dto/user_settings.go` | 3 个字段（`HeaderProfiles`、`RecordRequestContentLog`、`RecordResponseContentLog`） | 上游无 |
| `dto/openai_request.go` | `Reasoning.Extra`、`StreamOptions.Extra`、`Message.ThinkingContent`、`OpenAIResponsesRequest.Extra` | 补丁 2 的 presence 机制 + SenseNova 支持 |
| `dto/openai_response.go` | `ResponsesOutput.{Namespace,EncryptedContent,Execution,Input}`、`ChatCompletionsStreamResponse.Error`、`IncompleteDetails.Reasoning`、`ResponsesStreamResponse.{CallId,Error}`、`ToolCallResponse.Custom`、`ResponsesOutputContent.Refusal` 等 | 命名空间工具、加密内容、流式错误 |
| `dto/openai_responses_compaction_request.go` | 20 个字段 | 上游只有 `PromptCacheOptions` |

**全包实测**：本地 dto **394 个函数** vs relaykit **273 个**（`^func` 口径）；**33 个同名类型字段集不同且双向**（基线/HEAD 口径，工作区 34；完整清单见 12.0.1）。因此「宿主业务域留原地」不是可选方案——类型迁走则方法无家可归（12.0.2）。

两点提示：

- `responses_input_token_meta.go` 虽 462 行、27 个方法，但**全未导出且仅 dto 包内使用**——迁移成本高、外部影响面为零，是典型的「高体量低耦合」项
- `responses_tools.go` 只有 111 行却服务 8 个生产文件，**迁移风险最高**

#### 4.5.4 `*WithError` 系列

**这是迁移留下的形式差异，不是能力缺口。** 上游 relaykit 的转换器**本来就返回 error**——`ResponseConverterFunc func(c context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error)`（`response_registry.go:22`）。本地因历史原因保留了双 API：`FooWithError` 为主，`Foo` 为丢弃 error 的旧包装。

实测：

- `service/convert.go` 的 4 个 `*WithError` 转换函数有 **9 个外部生产调用点**（`relay/channel/openai/helper.go:46,63,246,267`、`relay-openai.go:314,324`、`chat_via_responses.go:280,286`、`relay/channel/gemini/relay-gemini.go:1784`）
- 对应的 4 个**无 error 版本**（`StreamResponseOpenAI2Claude:721`、`ResponseOpenAI2Claude:1035`、`ResponseOpenAI2Gemini:1826`、`StreamResponseOpenAI2Gemini:1997`）**生产调用点为 0**，仅测试使用

收敛后 `WithError` 后缀可直接消失，无需宿主包装层——旧包装函数可一并删除。

#### 4.5.5 未逐项核验项

本次盘点覆盖 `service/openaicompat/`（9 个非测试文件全文读取）、`relay/common/` 全部本地独有文件、11 个本地独有 `dto/` 文件、`types/` 与 `constant/` 的集合差集、以及 `service/convert.go` 的 `*WithError` 家族。以下**仍未逐项核验**，不得据此下结论：

- **`relay/channel/{claude,gemini,openai,aws}` 各自通道内的转换辅助函数**。本次只核验了它们对 `service/convert.go` 的调用点（即第 8 节的 6 条路径），未逐文件清点通道内自有的转换逻辑。已知 `relay/channel/aws/converse.go:713` 的 `converseOutputToClaudeResponse` 独立生成 thinking 块、绕过 `service/convert.go`，同类情况的完整清单未做
- **`service/` 根目录下的其余本地专有文件**（如 `service/responses_*.go` 系列）。本次未清点
- **`controller/` 层的协议相关逻辑**（`controller/relay.go`、`controller/channel-test.go:957` 等）。已知 `controller/channel-test.go:957` 有 `info.Request = chatReq` 反例，但未做完整清点
- **上游 relaykit 的 3 个演进分支**：`feat/relaykit-tools`、`feat/relaykit-perf`、`fix/relayconvert-tool-result-media-and-reasoning-parts`。未逐一核验，可能已包含本文档列为「待打补丁」的改动（见第 10 节第 15 项）
- **`relay/common/responses_input_filter.go` 第 646–1034 行**：只按符号 grep 核验了导出能力，未逐行读取其 ID 前缀契约与错误反解逻辑的细节
- **`dto/` 的完整方法级差集与 import 图**：早期版本只比对了 3 个文件（4.4）。实测本地 dto 共 **394 个函数**、relaykit/dto **273 个**；本地 **12 个非测试 dto 文件 import 宿主 `common`**、2 个 import `constant`、1 个 import `logger`（含测试口径为 21/3/1），而 relaykit/dto 只 import `types` 与 `kitutil`。**完整差集与依赖图仍是阶段 2 的开工前置**（见 12.0.2、12.0.3）
- **33 个字段不兼容类型的逐项对齐方案**（基线口径，工作区 34）：12.0.1 已列出清单，但每个字段「迁入 relaykit / 降为宿主自由函数 / 放弃」的取舍未逐项给出

## 5. 必须保留的两项能力

用户明确要求保留「思考等级」与「thinking 历史回传」。这两项**都是请求方向**，映射如下：

| 需求 | 方向 | 现状 | 收敛后承载 |
|---|---|---|---|
| 思考等级 | 请求 | `relay/claude_handler.go:55-104` 手写后缀适配 + `request.Thinking`/`OutputConfig` 组装 | `reasoning.RenderClaude` + suffix 解析（上游实现更完整，见 3.1） |
| thinking 历史回传 | 请求 | `service/convert.go:103` 起的 `extractThinkTaggedHistory` 分支 + `case "thinking"` 提取 + DeepSeek 空块保留 | 补丁 1 的 `case "thinking"` + `RetainEmptyReasoning` 钩子 + think 标签族（归属待定，见第 10 节第 2 项） |

**响应方向**的 reasoning 暴露策略是第三个独立关切，与上述两项无关，且**不能**按客户端意图门控——理由见第 6.1 节。

## 6. 已撤回的设计及其理由

保留此节用于避免重蹈覆辙。

### 6.1 撤回「按客户端意图门控 reasoning 暴露」

**结论（2026-10-01 修订）：门控依据是宿主策略而非客户端意图，落点在 relaykit 内部的索引分配之前，不在宿主调用点。**

**⚠ 本节原结论「响应方向不打 relaykit 补丁，策略落在宿主调用点」已被 12.4 推翻。** 原结论依赖「后置过滤可行」这一前提，实测该前提不成立：relaykit 的 `state.Index` 是私有流式状态，宿主删块会产生**索引空洞**，被 Claude 客户端拒绝。详见 12.4.1。

**以下三条论据仍然成立**（它们证明的是「不能按客户端意图门控」，与落点无关）：

1. 依据不成立。「客户端未请求 thinking 就不应返回 thinking」不是协议不变量。relaykit 自身的 `claudeCapabilitiesFor` 为 fable-5、mythos-5、opus-5、sonnet-5 置 `defaultThinking = true`——上游代码里就有「这些模型默认思考」的证据。
2. 客户端意图在响应阶段取不到正确值。`relay/claude_handler.go:34` 对 Claude 请求做 `common.DeepCopy`，此后后缀适配器写入的 `request.Thinking`（`:58`、`:79`、`:91`）全部作用于拷贝。客户端发 `claude-opus-4-7-high` 时适配器让上游真的思考了，而 `info.Request.Thinking` 仍是 nil，门控会误判为「未请求」并丢弃上游真实返回的 thinking——与被修复的缺陷同构。

   `info.Request` 在 `genBaseRelayInfo`（本地基线 `relay/common/relay_info.go:670` 定义、`:701` 赋值；上游 `:562` 定义、`:595` 赋值）一次性赋值。`grep -c "info.Request = " relay/claude_handler.go` = 0，但全仓有一个反例：`controller/channel-test.go:957` 的 `info.Request = chatReq`（渠道测试合成请求）。准确表述是「正常 relay 路径下不反映适配器改写后的意图」。
3. **只有 Responses→Claude 这一族转换器不可达 `Meta`**：`internal/oai_responses/to_claude_messages_resp.go:15` 与 `internal/oai_responses/to_claude_messages_stream_resp.go:82` 没有 `Meta` 参数（后者由 `ResponsesToClaudeStreamState` 承载，该结构不持有 `info`）。**注意**：Chat→Claude 的两条（`internal/oai_chat/to_claude_messages_resp.go:111`、`:449`）**已经接收 `info convmeta.Meta`**，不缺透传。本地只有 Chat→Claude 两条响应路径（`service/convert.go:721`、`:1035`），**没有** Responses→Claude，所以 12.4 的补丁**不需要**动 `oai_responses/` 一族——这一点在 12.4.2 已按可达性分侧写明。

**曾用过的错误论据，不得再用**：「`convmeta.Options` 是请求阶段机制、响应方向零先例」。实测 `response_registry.go:500` 的 `toolconv.AttachHostedResponse(..., convmeta.OptionsOf(info))` 就是响应方向读取 `Options` 的实例；且 `internal/` 下 9 处 `OptionsOf` 命中中，`shared/claude/reasoning.go:21` 与 `shared/gemini/request.go:85` 不在 `*_req.go`。**该反例恰恰支持 12.4 的新方案**——响应方向用 `Options` 钩子已有先例。

### 6.2 撤回「阶段 1 必须更新 210 个 import 点」

见第 2 节的别名机制。210 是上界，实际改写量需按符号逐个判断。

### 6.3 撤回「hosted 工具桥接是本地独有」

上游 `toolconv/` 在 `main` 上已存在（见 3.1）。

## 7. 收敛路径

每阶段结束时仓库可编译、可运行、测试通过。

| 阶段 | 内容 | 完成判据 |
|---|---|---|
| 0 | `git fetch --unshallow upstream`；建立 CI 门禁（两个 module 分别 `go test`，`GOWORK=off`）；~~完成本地独有能力盘点~~（已完成，见第 4 节）；**把 `relay/common/responses_tool_names.go` 及 2 个测试文件纳入版本控制**（第 10 节第 19 项） | CI 能跑通现有测试；盘点清单已产出；未跟踪文件已提交 |
| 1 | 引入 relaykit module（根 `go.mod` 加 require + replace）；`types` 拆分：6 个文件迁入 `relaykit/types`，宿主保留 4 个；按符号逐个决定别名转发或改 import 路径；处置 4.3 的本地专有符号 | 两个 module 均可编译 |
| 2 | **（最大工作量块）** `dto` 迁移：**327 个 import 点改写** + **33 个类型字段差异逐项对齐**（基线口径，工作区 34）+ 全部宿主独有方法（全包 394 vs 273 函数）给出出路。开工前置：完整的**方法级差集 + import 图分析**（12.0.2、12.0.3）。另含 `IsStream` 签名迁移、补丁 2（DTO presence） | 两个 module 均可编译；DTO 测试通过；**327 个 import 点全部验证** |
| 3 | `ClaudeConvertInfo` 改为 `convmeta.ClaudeConvertInfo` 别名，同步改写 `service/convert.go` 的 22 处字段引用；`RelayInfo` 实现 `convmeta.Meta` 全部 15 个方法 | 编译期断言 `var _ convmeta.Meta = (*RelayInfo)(nil)` 通过 |
| 4 | 协议映射切换到 relaykit；打补丁 1；对齐第 4 节的处置结论；**`*WithError` 系列直接删除**（12.3，不保留包装层） | 转换测试通过；grok reasoning 可见 |
| 5 | 宿主管道接入 relaykit 分派；**响应方向暴露策略在 relaykit 内门控**（12.4，`convmeta.Options` 钩子，必须在索引分配之前——后置过滤不可行。落点 `oai_chat/to_claude_messages_resp.go` 已接收 `info`，无需透传改造）；规则模型附带修正 | 全量测试通过 |
| 6 | 删除旧代码（`service/convert.go` 主体、`service/openaicompat/` 中被替代部分） | 无残留引用 |

三处硬依赖：

- `types` 是 `dto` 的前置（relaykit 的 DTO 必须 import `relaykit/types`）；本地 `types/error.go` import 宿主 `common`，上游改 import `kitutil`，所以 `types` 迁移牵动 `common → kitutil` 替换
- `ClaudeConvertInfo` 别名是 `Meta` 实现的前置。本地字段（`ToolCallStarted map[int]bool`、`ToolCallIDs`、`ToolCallNames`、`ToolCallPendingArgs`）与 `convmeta.ClaudeConvertInfo`（`ToolCalls []*ClaudeStreamToolCall`、`ToolCallByIndex`、`ToolCallByID`）**字段名与类型都不同**，`service/convert.go` 引用本地字段 22 处（6+5+5+6），改别名后全部编译失败
- `dto.Request.IsStream` 签名迁移**不可机械替换**：本地 `IsStream(c *gin.Context)`，relaykit `IsStream(c *http.Request)`；`dto/gemini.go:111` 用了 `c.Query("alt")`，`*http.Request` 无此方法

阶段 0 的 unshallow：当前 `.git/shallow` 截断于 `9724ef1b2`，导致 `git merge-base HEAD upstream/main` 返回空、三点 diff 不可用。但**真实共同祖先存在**——`9724ef1b2` 的提交对象记录 `parent aa7d0d39a4`，而 `merge-base(HEAD, aa7d0d39a4)` = `dac55f0f`。空结果纯属浅克隆遮蔽。两点 diff（`git diff upstream/main HEAD`）当前可用，但它比较两棵树终态，无法回答「本地相对共同祖先改了什么」，所以 unshallow 仍是必要前置。

unshallow 的目标是 `upstream` 而非默认 `origin`：`origin/main` 停在 `51074ac37`（2026-07-10）。

## 8. 响应方向的 6 条路径

响应方向把上游响应转成 `dto.ClaudeResponse` 的路径：

| 路径 | 基线位置 | 经 `service/convert.go` |
|---|---|---|
| OpenAI 渠道流式 | `relay/channel/openai/helper.go:46` | 是 |
| OpenAI 渠道流式末帧 | `relay/channel/openai/helper.go:246` | 是 |
| OpenAI 渠道非流式 | `relay/channel/openai/relay-openai.go:314` | 是 |
| Chat 经 Responses | `relay/channel/openai/chat_via_responses.go:280` | 是 |
| Gemini 渠道 | `relay/channel/gemini/relay-gemini.go:1784` | 是 |
| AWS Converse | `relay/channel/aws/converse.go:748`（非流式）、`:1110`（流式 `thinking_delta`） | **否**，`converseOutputToClaudeResponse`（`:713`）直接从 `ConverseOutput` 构造 |

前 5 条都持有 `info`，是首选落点。第 6 条有独立于 `service/convert.go` 的 thinking 生成逻辑，需单独处理。

## 9. 验证策略

### 9.1 阶段 4 的语义对齐清单

本地与 relaykit **不是逐一等价**，切换前须逐项决定「保留、放弃或改写」，并同步调整锁定的测试：

| # | 差异 | 本地 | relaykit |
|---|---|---|---|
| 1 | finish_reason 严格校验 | `stopReasonOpenAI2ClaudeStrict`（`service/convert.go:1125`）对未知值返回错误 | `reasonmap.OpenAIFinishReasonToClaudeStopReason` 的 `default: return finishReason` 原样透传 |
| 2 | 多 choice | `ResponseOpenAI2ClaudeWithError` 在 `len(Choices) > 1` 时报错 | 遍历所有 choice 并拼接 |
| 3 | `Message.ThinkingContent` | `dto/openai_request.go:407` 有该字段，`GetReasoningContent()` 三级回退 | relaykit `Message` 无 `ThinkingContent`，两级回退 |
| 4 | `ClaudeConvertInfo` 字段 | 见第 7 节的三处硬依赖 | 字段名与类型均不同 |
| 5 | `ResponsesStreamResponse` 形状 | `dto/openai_response.go:450` 有 `Error`、`Text`、`Arguments`、`CallId`、`Item` | `CallId` 在 `ResponsesOutput` 内而非事件顶层 |
| 6 | 本地 DTO 独有方法 | 见 4.4（23 个，HEAD 20 个） | 无对应；反向有 5 个 relaykit 独有方法 |

### 9.2 阶段 4 的输入校验对齐

本地对 Claude 请求做了 relaykit 没有的加固，切换会静默丢失：

| 校验 | 本地 | relaykit |
|---|---|---|
| 未知 content block | `service/convert.go:355` 显式报错 | 无 default，静默丢弃（补丁 1 第 3 项修复） |
| image 缺 source（Chat 方向） | `:252` 显式报错 | `internal/claude_messages/to_oai_chat_req.go:189` 的 `case "image"` 在 `:190` 直接解引用 `mediaMsg.Source`，**nil pointer panic** |
| image 缺 source（Responses 方向） | `:252` 显式报错 | 有守卫：`claudeSourceURL`（`internal/claude_messages/to_oai_responses_req.go:315`）对 nil 返回 `""` |
| image source.type 校验 | `:282` 分派 base64/url，其他报错 | 无分派，一律按 base64 拼接 |
| 非 assistant thinking | `:234`、`:246` 静默跳过 | 无 role 门控（补丁 1 第 2 项修复） |
| tool_use 缺 id/name | `:287` 显式报错 | 无校验 |

### 9.3 命令

- 每阶段运行 `go test ./controller ./model ./relay/common ./relay/helper ./service`（AGENTS.md 第 6 节）
- 阶段 4 专项：构造含 thinking block 的 Claude 多轮工具调用请求，验证 DeepSeek 不再 400；验证 grok 的 reasoning 可见（当前被丢弃）
- 前端改动运行 `bun run lint` 与 `bun run build`
- 验证记录沉淀到 `docs/reviews/CR-*.md`

不得以局部测试通过表述为全链路已验证。

## 10. 未决项

1. ~~**完成本地独有能力盘点**（4.1）~~ **已完成**，见 4.1–4.5.5 与第 11 节取证。剩余未覆盖边界已明确列在 4.5.5，不影响阶段 4 启动
2. ~~**think 标签提取族归属**（4.2）~~ → **12.1**：剥离迁入 relaykit 成为 Claude 历史回传能力（带钩子），门控留宿主
3. ~~**本地专有错误符号归属**（4.3）~~ → **12.2**：4 个 `ErrorCode` 常量与 2 个判定函数全部迁入 `relaykit/types`
4. ~~**`*WithError` 系列形式**（4.5.4）~~ → **12.3**：直接删除，9 个调用点改为 error 返回接口
5. ~~**响应方向暴露策略的具体形态**~~ → **12.4**：**relaykit 内门控**（`convmeta.Options` 钩子），必须在索引分配之前。后置过滤会破坏 SSE 索引，不可行
6. ~~**本地 DTO 独有方法归属**（4.4）~~ → **12.5**：A 类 18 个迁入 relaykit/dto，B 类 2 个降为宿主自由函数，3 个需实测（与 `InputTokenDetails` / `ImageGenerationCallCounter` 对齐）
7. ~~**本地 hosted 工具与上游 `toolconv` 的收敛**~~ → **无差异**：上游 10 种 `Kind`（`function`/`web_search`/`file_search`/`web_fetch`/`code_execution`/`computer_use`/`url_context`/`mcp`/`image_generation`/`native`）是本地超集。本地非测试代码出现的 hosted 工具 type 只有 3 种（`web_search`、`file_search`、`image_generation`），差集为空。直接采用
8. ~~**`types` 别名转发的适用范围**~~ → **12.2**：仅 `endpoint_type.go`、`finish_reason.go` 两类常量用别名；其余改 import
9. ~~`relaykit/dto` 补丁 2 是否影响上游 golden 快照~~ → **12.16**：需实测，24 个 JSON 快照逐一核对
10. ~~`ConvOptions()` 缓存与失效语义~~ → **12.10**：无需改动，上游 `relay/common/relay_info.go:311`（`InitChannelMeta` 起于 `:245`）已在其中显式置 `nil`
11. ~~钩子闭包捕获语义~~ → **12.10**：收窄为「钩子只答策略、不捕获状态」，调用点传入 `modelName`
12. ~~`reasonmap` 的宽松 default 是否需严格模式~~ → **12.11**：保留宽松 default；严格化在宿主调用点做，不污染套件
13. ~~thinking block 的 `signature`~~ → **12.12**：维持现状（请求接受无签名块、响应不生成签名），标注为已知未验证项，列入阶段 4 验证清单
14. ~~`enabled` 双语义~~ → **12.13**：保留并文档化（`UnmarshalJSON` 用存在性判断，语义确定）
15. ~~上游 `feat/relaykit-tools` 分支是否应作为补丁基线~~ → **12.9**：不采用，两分支均早于 main（2026-09-01/03 vs 09-28），且核心提交已作为 PR #7137 并入 main
16. ~~**命名空间工具与 tool_search 的归属**（4.5.1）~~ → **12.6**：整体保留在宿主（上游零覆盖，属本地协议扩展）
17. ~~**custom tool bridge 门控语义取舍**（4.5.1）~~ → **12.7**：保留本地 fail-closed（默认拒绝、显式开启）
18. ~~**`matchAnyRegex` 空模式语义**（4.5.1）~~ → **12.8**：采用本地语义（空 patterns = 匹配全部）并文档化
19. ~~**未跟踪文件纳入版本控制**（4.5.2）~~ → **12.14**：阶段 0 门禁，与会话确认后提交
20. ~~**`service/openaicompat/` 的收敛边界**（4.5.1）~~ → **12.15**：**整体保留**。按能力切分在函数粒度不成立（`convertResponsesToolsToChat` 内交织 5 种工具语义）

## 11. 事实依据

行号以第 1 节声明的本地基线 `dbfbc096f` 为准（该基线与 `9c4758b53` 之间只改了代理/环境文件，行号一致）。上游行号以 `upstream/main` 为准。

| 事实 | 核验 |
|---|---|
| 上游 `service/convert.go` 27 行 | `git show upstream/main:service/convert.go \| wc -l` |
| 上游无 `service/openaicompat/` | `git ls-tree -r upstream/main -- service/openaicompat/` 空 |
| 两个 Claude 请求转换器 content block switch 无 `thinking`、无 `default` | `internal/claude_messages/to_oai_chat_req.go:181`、`internal/claude_messages/to_oai_responses_req.go:165` 逐 case 提取 |
| 上游 relaykit 无字面量 `namespace` / `tool_search` | `git grep -c` 均无命中。**注意**：不代表无 hosted 工具能力（`toolconv/` 覆盖 10 种） |
| 上游无 Responses→Chat 全局策略 | `git grep ShouldResponsesUseChatCompletions upstream/main` 空 |
| 本地 `ConversionDiagnostic` 零命中 | `grep -rl` 返回 0 |
| 本地 `dto` 被 327 个文件 import（基线） | `git grep -l` 计数。工作区为 340 |
| 本地 `dto` 有 10 个非测试文件 import gin | 逐文件核验 |
| 本地 CI 不跑 `go test` | `.github/workflows/` 命中 0；`makefile` 无 `test` 目标 |
| `context_compaction` 上游零命中 | `git grep -c` 无命中 |
| 三个 `responses_tool_names*.go` 未跟踪 | `git status --short` 显示 `??` |
| `convmeta.Meta` 15 个方法无客户端请求访问器 | `relaykit/relayconvert/convmeta/meta.go:18-54` 逐方法核验 |
| registry 分派函数类型带 `info convmeta.Meta` | `response_registry.go` 的 `:22`、`:24`、`:28`、`:30` 带；`:26` 的不带 |
| `convmeta.Options` 响应方向也有使用 | `response_registry.go:500` |
| `types` 拆分爆炸半径 210 个 import 点 | `git grep -l '"…/new-api/types"' 9c4758b53 -- '*.go' \| wc -l`（含测试口径；工作区 212） |
| 上游宿主 172 个文件 import `relaykit/types` | `git grep -l` 计数 |
| 上游宿主 24 个文件仍 import 宿主 `types` | 同上 |
| 本地 `ClaudeConvertInfo` map 字段被引用 22 处 | `ToolCallStarted` 6 + `ToolCallIDs` 5 + `ToolCallNames` 5 + `ToolCallPendingArgs` 6 |
| relaykit 无 `*WithError` 系列 | `git grep WithError upstream/main -- relaykit/` 空 |
| 上游 `toolconv/` 覆盖 10 种 hosted 工具 | `toolconv/model.go` 的 `Kind*` 常量 |
| relaykit 内建 Claude 模型代能力表 | `reasoning/claude.go:35-74` |
| `dac55f0f` 是真实共同祖先（被浅克隆遮蔽） | `git cat-file -p 9724ef1b2` 记录 `parent aa7d0d39a4`；`git merge-base HEAD aa7d0d39a4` = `dac55f0f` |

第 4 节盘点（2026-10-01）新增取证：

| 事实 | 核验 |
|---|---|
| 上游 `relaykit/types/error.go` 存在，33 个 `ErrorCode` | `git show upstream/main:relaykit/types/error.go`；与本地 37 个取差集得本地独有 4、上游独有 0 |
| 上游无 `ParseContentStrict` / `ParseToolCallsStrict` | `git grep -ln ParseContentStrict upstream/main -- '*.go'` = 0；`ParseToolCallsStrict` = 0（`ParseContent` 18 文件、`ParseToolCalls` 15 文件均指宽松版） |
| relaykit `ParseToolCalls` 静默吞解析错误 | `relaykit/dto/openai_request.go:624`：`if err := kitutil.Unmarshal(...); err == nil { return toolCalls }` 后直接 `return toolCalls` |
| relaykit `ParseContent` 类型不匹配返回空列表 | `relaykit/dto/openai_request.go:686`：`arrayContent, ok := m.Content.([]any); if !ok { return contentList }` |
| 本地 DTO 独有方法 23 个（HEAD 20 个） | 按 `接收者.方法名` 对比本地 `dto/` 与 `relaykit/dto/` 同名文件：13 + 9 + 1 = 23（工作区）/ 13 + 6 + 1 = 20（HEAD） |
| dto `Clone` 生产调用点只有 1 个文件 | `git grep -n '\.Clone()' -- 'dto/*.go'` 与 `service/responses_codex_local_compaction_recovery.go:205,224` |
| `service/convert.go` 4 个 `*WithError` 有 9 个外部生产调用点 | `git grep -nE "(^|[^A-Za-z0-9_])<fn>\(" -- '*.go'` 排除定义行与 `service/convert.go` 内部包装 |
| 4 个无 error 旧包装的生产调用点为 0 | 同法核验 `StreamResponseOpenAI2Claude` / `ResponseOpenAI2Claude` / `ResponseOpenAI2Gemini` / `StreamResponseOpenAI2Gemini`，命中仅 `_test.go` |
| 上游转换器本就返回 error | `relaykit/relayconvert/response_registry.go:22` `ResponseConverterFunc … (any, *dto.Usage, error)` |
| 上游无 `namespace` 支持 | `git grep -ln namespace upstream/main -- 'relaykit/' 'relaykit/relayconvert/' 'service/' 'relay/'` = **0 文件**（四作用域分别 0） |
| 上游无 `tool_search` 协议概念 | 全仓唯一命中 `relaykit/dto/claude.go:615` `ToolSearchRequests int`（计费字段） |
| 上游无 `EnableCustomToolBridge` | `git grep -ln EnableCustomToolBridge upstream/main -- 'relaykit/' 'service/'` = 0；`custom_tool_call` 在 relaykit 6 文件但不门控 |
| 上游无 `ProtocolConversionRule` | `git grep -ln ProtocolConversionRule upstream/main -- '*.go'` = **0 文件**；`ChatCompletionsToResponsesPolicy` 存在于 `setting/model_setting/global.go:14` 但无 `Rules` |
| 上游无 `synthetic.compact` / `encrypted_content` / `NormalizeToolName` | 三次 `git grep -ln … upstream/main -- '*.go'` 均 0 文件 |
| 上游宿主 `service/openai_chat_responses_compat.go` 是 30 行纯转发 | `git show upstream/main:service/openai_chat_responses_compat.go \| wc -l` = 30。导出 6 个函数，5 个与本地同名，第 6 个 `ResponsesFinishReasonFromStatus` 对应本地 `responsesIncompleteFinishReason`（同能力、异名），全部转调 `relayconvert` |
| 上游 `PrepareResponsesRequest` 无本地 4 项请求增强 | `git show upstream/main:relay/responses_request.go`（87 行）全文读取，仅 `common.SupportsResponsesCompact` 门控 |
| 上游 `relay/common/` 五个候选文件未吸收本地能力 | 逐文件读取 `conversion_diagnostics.go`、`response_model.go`、`request_conversion.go`、`outbound_body.go`、`tool_usage.go` |
| 上游有更强的 image generation 去重 | `relay/common/tool_usage.go` `ImageGenerationCallCounter.Observe` 按 id/callId/index/result-sha256 四别名去重 |
| 上游只有 2 个 `constant/` 文件用别名转发 | 逐文件核验 `upstream/main:constant/*.go` 的 `relaykit` import：仅 `endpoint_type.go`、`finish_reason.go` |
| `relay/common/responses_tool_names.go` 未跟踪但在生产路径 | `git status --short` 显示 `??`；调用点 `relay/responses_handler.go:1123`、`relay/channel/openai/relay_responses.go:912,1296` |
| 本地 `types/` 仅 2 个文件依赖宿主 `common` | 逐文件 grep `github.com/QuantumNous/new-api/[a-z]+`：`error.go`、`rw_map.go` |
| 上游 `dto` **零别名转发**，直接改 import | 上游宿主 `dto/` 8 个文件均不 import relaykit、无 `type X =` 行；`git grep -l 'new-api/relaykit/dto' upstream/main -- '*.go'` = **281**，宿主 dto = 39 |
| 本地 327 个文件 import 宿主 `dto` | `git grep -l '"github.com/QuantumNous/new-api/dto"' HEAD -- '*.go' \| wc -l` |
| 33 个同名类型字段集不同（双向） | `go/ast` 解析 22 个同名文件的 136 个同名导出类型的字段名集合求差集；基线/HEAD 均 33（工作区 34）。本地独有 `Reasoning.Extra`、`StreamOptions.Extra`、`Message.ThinkingContent`、`ResponsesOutput.Namespace`、`ChannelOtherSettings` 20 字段等；relaykit 独有 `GeneralOpenAIRequest` 20 字段、`ResponsesOutput` 15 字段等 |
| Go 别名**继承方法集**但**不能新增方法** | 最小程序实测：`type A = base.T` 后 `a.Method()` 正常；跨包给别名加方法报 `cannot define new methods on non-local type Thing` |
| 本地 dto 394 个函数 vs relaykit 273 | `git grep -hE '^func ' HEAD -- 'dto/*.go' \| wc -l` / 同法查 `relaykit/dto/` |
| `channel_settings.go` 本地 61 函数 / relaykit 29 | 单文件 `grep -cE '^func '` 得 61/29（含包级函数）；41 个本地独有**方法**由 `func (recv) Name` 提取后求差集，上游全仓 0 命中 |
| 本地 12 个**非测试** dto 文件 import 宿主 `common` | 逐文件 `git show HEAD:<f> \| grep`：非测试 `common` 12、`constant` 2、`logger` 1（含测试 21/3/1）；relaykit/dto 只 import `types`(17) 与 `kitutil`(12) |
| `convertResponsesToolsToChat` 交织 5 种工具 | `responses_to_chat_request.go:455` 签名含 `namespaceSelection` + `toolState`；函数体内 namespace 16 / toolState 5 / ToolSearch 5 / Custom 4 / EnableCustomToolBridge 3 |
| 本地 thinking 门控在索引分配之前 | `service/convert.go:749` 算 `allowClaudeThinking`，`:794` `if !allowClaudeThinking { return }`；relaykit `internal/oai_chat/to_claude_messages_resp.go:137`（`state.Index++` 在 `:151`）无条件推进索引，`:342` 起无条件建块 |
| AWS Converse 无条件暴露 thinking | `relay/channel/aws/converse.go:748` 直接构造 `{Type:"thinking"}`；全文件 0 处调用 `shouldExposeReasoningAsClaudeThinking` |
| `shouldExposeReasoningAsClaudeThinking` 只对 DeepSeek 为真 | `service/convert.go:526` 即 `hasDeepSeekModelName(model, info)` |
| `openaicompat` 第 3 个消费者 | `relay/responses_via_chat.go:104` 调 `service.ResponsesRequestToChatCompletionsRequestWithState` |
| 上游两分支早于 main 且已并入 | `feat/relaykit-tools`=`bbe20f2e1`(2026-09-01)、`feat/relaykit-perf`=`55bba35f2`(2026-09-03)，均早于 main(2026-09-28)；核心提交内容已作为 PR #7137 `0ed497f06` 并入（`git merge-base --is-ancestor 0ed497f06 upstream/main` 通过；两分支 toolconv 均 10 个 `Kind*`） |
| `ConvOptions` 缓存跨渠道重试已处理 | 上游 `relay/common/relay_info.go:311` 在 `InitChannelMeta` 置 `info.convOptions = nil`，注释明示为跨渠道重试重建 |
| `matchAnyRegex` 空模式语义差异 | 本地 `service/openaicompat/regex.go`：`len(patterns)==0 → true`；上游 `openai_chat_responses_mode.go:15`：`len(patterns)==0 \|\| model=="" → false` |
| `service/openaicompat/` 共 3072 行（9 个非测试文件，基线口径） | 逐文件 `wc -l` 求和；含测试 5280。工作区因其他会话改了 `chat_to_responses_response.go` 为 3071 |
| 两分支 6 个提交均非 main 祖先 | `git merge-base --is-ancestor <c> upstream/main` 对 `55bba35f2`/`bbe20f2e1`/`c82c03ecd`/`60e15b53a`/`b45639116`/`a9a0e2272` 全部 exit=1 |
| 两分支内容已被 main 覆盖（逐字节/符号集） | `toolconv/` 7 文件 5 SAME、2 处为 main 侧**功能新增**（`decodeOpenAIChatPseudoHostedTool` 等）；`reasoning/claude.go` 导出符号双向为 0；`billing_usage.go`/`usage_merge.go` 分支独有导出符号 0；`relaykit/` 整体 main 净增 `3910+/746−` |
| 非测试 dto 文件的宿主 import 分布 | 逐文件 `git show HEAD:<f> \| grep`：`common` 12、`constant` 2、`logger` 1（含测试 21/3/1） |
| `HasCompactionTrigger` 生产引用 9（提及口径） | `git grep -lw` 排除 `_test.go` 与 `dto/`；按调用 `.HasCompactionTrigger()` 计为 7 |
| 136 个同名类型中 33 个字段不兼容 | `go/ast` 解析 `type X struct` 字段名集合求差集；基线 `dbfbc096f` 与 `HEAD` 均为 33，工作区 34（`ChatCompletionsStreamResponse` 被其他会话改动） |
| `ChannelOtherSettings` 本地独有 20 个字段 | 同上，AST 差集；基线/HEAD 均 20，工作区 21 |
| `GeneralOpenAIRequest` relaykit 独有 20 个字段 | 同上，AST 差集；三口径均 20 |
| `channel_settings.go` 41 个本地独有方法上游全仓 0 命中 | 逐个 `git grep -cw <m> upstream/main -- '*.go'`，41/41 命中数为 0 |
| 24 个 golden 快照 | `git ls-tree -r upstream/main relaykit/ | grep -i golden | grep -v '\.go$' | wc -l` = 24（第 25 个是 `golden_test.go` 本身） |
| relaykit `state.Index++` 的确切位置 | `internal/oai_chat/to_claude_messages_resp.go:137` 定义 `stopOpenBlocksAndAdvance`，`:151` 在 `default:` 分支执行 `state.Index++` |
| 上游 `info.Request` 赋值位置 | 上游 `relay/common/relay_info.go:562` 定义 `genBaseRelayInfo`、`:595` 赋值；本地基线 `:670` 定义、`:701` 赋值 |

已撤回、不得再用的断言：

- 「本地 `service/convert.go` 2236 行」：工作区口径，基线 2229
- 「22205 行转换测试」：无可复现口径
- 「`dto` 被 340 个文件 import」：工作区口径，基线 327
- 「18 个 dto 文件 import 宿主 types」：工作区口径，基线 17
- 「`dac55f0f` 不是共同祖先」：错，它是真实共同祖先，只被浅克隆遮蔽
- 「无法用 diff 驱动迁移」：两点 diff 可用，仅三点 diff 不可用
- 「两个 `*RequiresChannelSkip` 等价于 `IsChannelEnabled`」：两者语义不同，且这两个函数是**互相**重复
- 「无条件暴露不符合 Anthropic 行为」：推断，且随模型代变化，不是协议不变量
- 「本地 `ResponsesStreamResponse` 的 `Code` 为 `any`」：本地无 `Code` 字段
- 「136 个同名类型中 34 个字段不兼容」：34 是工作区口径，基线/HEAD 为 33
- 「`ChannelOtherSettings` 21 个字段」「`GeneralOpenAIRequest` 21 个」：均为工作区口径，基线各为 20
- 「`channel_settings.go` 40 个宿主独有方法」：41（且「函数」与「方法」两个口径被混写）
- 「`service/convert.go:801` `if !allowClaudeThinking { continue }`」：实际是 `:794` 且为 `return`（闭包提前返回），非循环 `continue`
- 「上游 shim 是 27 行」：27 是上游 `service/convert.go` 的行数，被误植到 shim；shim 实测 30 行
- 「shim 的 6 个函数名与本地完全相同」：只有 5 个同名，第 6 个 `ResponsesFinishReasonFromStatus` 本地全仓无对应
- 「`types` import 工作区 216」：实测 212（含测试口径）
- 「`EnableCustomToolBridge` 8 个文件」：9 个非测试文件
- 「上游 10 种 hosted 工具且多 `apply_patch`、`local_shell`」：`apply_patch`/`local_shell` 是 Responses **input item 类型**，不是 `toolconv` 的 `Kind`。上游 Kind 恰好 10 个，本地非测试只出现 3 种 hosted 工具 type，是其子集
- 「上游 shim 第 6 个函数本地无对应」：函数**名**无对应，**能力**有（`responsesIncompleteFinishReason`，`responses_to_chat.go:246`）
- 「relaykit `internal/oai_chat/to_claude_messages_resp.go:342-365` 无条件 `state.Index++`」：`state.Index++` 在 `:151` 的 `stopOpenBlocksAndAdvance` 内，`:342` 起是调用点
- 「`convmeta.Options` 是请求阶段机制、响应方向零先例」：`response_registry.go:500` 即反例
- 「`info.Request` 从不被重新指向」：`controller/channel-test.go:957` 是反例
- 「relaykit 的 image 处理直接解引用会 panic」：只有 Chat 方向如此
- 「`*WithError` 系列 39 个生产调用点」：无可复现口径；实测 4 个函数 9 个，6 个函数 14 个
- 「relaykit 响应方向两个转换器都用 `_` 丢弃 Meta」：只有非流式如此
- 「响应方向门控在 relaykit 内不可实现」：registry 层 `Meta` 可达；`oai_chat` 的两个转换器（`:111`、`:449`）**本就直接接收 `info convmeta.Meta`**，零透传成本。只有 `oai_responses` 一族不接收，且本地不涉及该路径（见 12.4.2）
- 「hosted 工具桥接是本地独有」：上游 `toolconv/` 已存在
- 「阶段 1 必须更新 210 个 import 点」：上游用类型别名做到零调用点变更
- 「两个 Claude 转换器 switch 无 `default`」（未限定作用域）：只有 content block switch 无，同文件其他 switch 有
- 「上游宿主 `types/` 没有 `error.go`」：字面为真但误导——上游该文件迁到了 `relaykit/types/error.go`，33 个常量与本地同名同值
- 「本地 DTO 有 13 个独有方法」：13 只是 `openai_request.go` 单文件数，实测 23（工作区）/ 20（HEAD）
- 「`Clone` 有 13 个生产使用点」：把 `http.Request.Clone`、`http.Transport.Clone`、`common.ResponsesRequestMetadata.Clone` 一并计入；dto 的 `Clone` 实际只 1 个生产文件
- 「`ParseContentStrict` 20 / `ParseToolCallsStrict` 16 / `HasCompactionTrigger` 10 / `HasCompactionOutput` 4」：实测 12 / 11 / 9 / 2
- 「`*WithError` 系列 4 个函数 9 个生产调用点，relaykit 无对应签名」：前半对，后半错——上游转换器本就返回 error，`WithError` 是本地命名遗留
- 「`relay/common/responses_stream_options_compat.go`」：该文件不存在，系误记
- 「`service/openaicompat/` 上游无此包」（不加限定）：包确无，但**能力大部分有**——上游把 chat↔responses 兼容层放在 `relaykit/relayconvert/internal/{oai_chat,oai_responses}/`，并在宿主留了同名函数的转发 shim
- 「本地 DTO 独有方法全部在生产代码中使用」：23 个中 `IsValidToolName`、`ToolNameMaxLength` 生产引用为 0；`ResponsesStreamResponse.GetOpenAIError` 等 3 个在 HEAD 口径下为 0
- 「`IsValidToolName` / `ToolNameMaxLength` 是 dto 独有方法」：它们是**包级符号**（`const` / `func`），不是方法，不该出现在方法集比对里
- 「迁入 `relaykit/dto`，调用点不变」：上游对 dto **零别名转发**，是直接改 import（281 文件）。本地对应 327 个文件
- 「本地 DTO 有 23 个独有方法」（作为全包结论）：23 只是 3 个文件的数。全包本地 394 函数 vs relaykit 273
- 「`dto/channel_settings.go` 属宿主业务域、不应被波及」：它是 dto 文件，类型迁走则 41 个本地独有方法无家可归
- 「`service/openaicompat/` 消费者仅 2 个文件」：漏了 `relay/responses_via_chat.go:104`（调 `…WithState`）
- 「流式下逐 chunk 剔除 thinking 是无状态操作」：relaykit 的 `state.Index` 是私有流式状态，删块产生索引空洞
- 「AWS Converse 用同一 helper」：该路径当前**无条件**暴露 thinking，套用 DeepSeek-only 策略会新抑制 Bedrock reasoning
- 「`openaicompat` 按能力切分，基础转换转调 relaykit」：`convertResponsesToolsToChat` 一个函数内交织 5 种工具语义，函数粒度不可切
- 「24 个 golden 快照是唯一的机械回归网」：它是上游自己的基线，测不到 9.1/9.2 的 12 处本地差异
- 「响应方向不打 relaykit 补丁」（第 6.1 节结论）：已被 12.4 推翻——门控必须在索引分配之前，只能在 relaykit 内

## 12. 决策

第 10 节 20 项未决的处置结论。每条给出决定、依据与影响。凡标注「需实测」者，实施时必须先取证再落地。

### 12.0 决定全局归属的硬约束

本节是 12.1–12.15 的共同前提。**本文档早期版本在此处给出的两条论断均已被实测推翻**，修正如下。

#### 12.0.1 别名不能承载 dto 迁移（推翻早期「调用点不变」）

早期版本写「迁入 `relaykit/dto`（上游化）：方法体随类型走，**调用点不变**」。**这是错的**，两个独立事实：

**事实一：上游对 `dto` 根本没用别名。** 上游只有 `constant/endpoint_type.go`、`constant/finish_reason.go` 两处用 `type X = types.X`。宿主 `dto/` 剩的 8 个文件是**独立包**——零别名转发、零 relaykit import。上游是直接改 import：

```bash
git grep -l 'new-api/relaykit/dto' upstream/main -- '*.go' | wc -l   # 281
git grep -l '"github.com/QuantumNous/new-api/dto"' upstream/main -- '*.go' | wc -l  # 39
git grep -l '"github.com/QuantumNous/new-api/dto"' HEAD -- '*.go' | wc -l            # 327（本地）
```

**事实二：字段级不兼容使别名在技术上也不成立。** 逐字段比对 22 个同名文件的 **136 个同名导出类型**，**33 个字段集不同，且双向**（基线 `dbfbc096f` 与 `HEAD` 口径；工作区因其他会话改动为 34）：

- 本地独有（采用 relaykit 类型即编译失败）：`Reasoning.Extra`、`StreamOptions.Extra`（补丁 2 的未知字段保真机制）、`Message.ThinkingContent`（SenseNova/DeepSeek）、`ResponsesOutput.Namespace`（命名空间工具）、`ChannelOtherSettings` 20 个字段、`OpenAIResponsesCompactionRequest` 20 个字段、`UserSetting` 3 个
- relaykit 独有：`GeneralOpenAIRequest` 20 个、`ResponsesOutput` 15 个、`OpenAIResponsesRequest` 13 个、`ClaudeRequest.{ChatTemplateKwargs,Safeguards}`、`Usage.BillingUsage`、`ClaudeWebSearchTool` 4 个

**Go 别名的两条语义（已用最小程序实测）**：别名**完整继承方法集**（`type A = base.T` 后 `a.Method()` 正常工作）；但**不能新增方法**（`cannot define new methods on non-local type`）。

**结论**：`dto` 迁移是 **327 个文件的 import 改写 + 33 个类型差异的逐项对齐**（基线口径，工作区 34），不是零改动。这是本次迁移**最大的工作量块**，必须在阶段 2 单列并给出规模评估。

#### 12.0.2 类型必须整体迁移，不能只迁「协议层」部分

早期版本假设可按「协议层 / 宿主业务域」切分 dto 文件，把宿主业务域留在原地。**这在 Go 里不成立**：`dto` 是一个包，类型定义在哪就属于哪；同文件的方法无法跟随部分类型迁移。实测后果：

- `dto/channel_settings.go` 被早期版本归为「宿主业务域、零协议类型 import、不应被波及」——但它是 dto 文件，类型必须迁走，其 **41 个本地独有方法**（`ResponsesCompactModeOrDefault`、`ResolveResponsesChannelCapability`、`MarkResponsesCompactAutoFallback` 等，上游全仓 0 命中）随之无家可归
- 全 `dto` 包本地 **394 个函数**、relaykit **273 个**（`^func` 口径，含包级函数）。4.4 的「23 个方法」只比对了 3 个文件，**严重低估**

#### 12.0.3 依赖方向

`dto` 不只是 `types` 的消费者。实测本地 **12 个非测试 dto 文件 import 宿主 `common`**、2 个 import `constant`、1 个 import `logger`（含测试口径为 21/3/1）；relaykit/dto 只 import `relaykit/types` 与 `relaykit/relayconvert/kitutil`。**完整的 import 图分析仍是未完成项**（见 4.5.5），阶段 2 开工前必须补齐——否则 `dto/channel_settings.go` 的 `constant.ChannelTypeCodex` 可能形成 `dto → constant → relaykit/types → dto` 的环。

**这条约束决定了下面每一项的取舍，也解释了为何不能「先把 dto 搬过去再说」——搬的瞬间 33 个类型差异（基线口径，工作区 34）与全部宿主独有方法同时编译失败，必须同时给出出路。**

### 12.1 协议层语义 → 提上游（第 10 节第 1 项已完成的产出）

| 项 | 决定 | 依据 |
|---|---|---|
| `ParseContentStrict` / `ParseToolCallsStrict` | **迁入 `relaykit/dto`**，并向上游提 PR | relaykit 的 `ParseToolCalls()`（`openai_request.go:624`）静默吞掉 unmarshal 错误、`ParseContent()`（`:686`）类型不匹配返回空列表。AGENTS.md 第 3 节「禁止静默容错」。这是修上游的缺陷，不是加本地特性 |
| think 标签剥离（`splitThinkTaggedText` 等） | **迁入 relaykit**，作为 Claude 历史回传能力；**门控留在宿主** | 剥离是文本协议处理（`<think>` 标签解析），门控是部署策略。拆分后 relaykit 提供无门控的纯函数，宿主用 `convmeta.Options` 钩子决定是否调用——与既有 `PreserveThinkingSuffix func(modelName string) bool` 同型 |

### 12.2 本地专有错误符号 → 迁入 `relaykit/types`

**决定**：4 个 `ErrorCode` 常量与 2 个判定函数全部迁入 `relaykit/types`。

**依据**：上游 `relaykit/types/error.go` 已经承载宿主侧语义的码——`ErrorCodeDoRequestFailed`、`ErrorCodeGetChannelFailed`、`ErrorCodeChannelNoAvailableKey`、`ErrorCodeChannelResponseTimeExceeded`。转换套件的错误词汇表本就不是纯转换语义。若把 4 个码留在宿主，会出现两个 `ErrorCode` 类型，所有跨边界的 `switch` 与比较都要转换，代价远大于收益。

**影响**：`relay/channel/api_request.go`、`relay/channel/openai/responses_response_body_read.go`、`service/channel.go` 等 15 个非测试文件的 import 路径改到 `relaykit/types`（排除 `types/error.go` 定义处；多数本来就要改）。

### 12.3 `*WithError` 系列 → 删除，不保留包装层

**决定**：迁移时直接删除 4 个 `*WithError` 函数与 4 个无 error 旧包装，9 个生产调用点改为调用 relaykit 的 error 返回接口。

**依据**：上游 `ResponseConverterFunc`（`response_registry.go:22`）签名本就是 `(any, *dto.Usage, error)`；`WithError` 是本地历史命名。4 个无 error 包装的生产调用点为 **0**（仅测试引用），删除无生产影响。

**影响**：4 个测试文件需改写调用；无生产代码语义变化。

### 12.4 响应方向 reasoning 暴露策略 → 门控必须在索引分配之前

**早期版本的「宿主调用点后置过滤」方案已被实测推翻**，见下。

#### 12.4.1 为什么后置过滤不可行

早期版本断言「流式下逐 chunk 剔除是**无状态**操作」。**这是错的**——thinking 的门控与 SSE 索引分配是**同一状态机**的两面：

**本地现状**（`service/convert.go`）：
```go
:749  allowClaudeThinking := shouldExposeReasoningAsClaudeThinking(openAIResponse.Model, info)
:794  if !allowClaudeThinking { return }     // 闭包在分配索引之前提前返回
```
本地从不分配 thinking 的 `content_block_start`，因此**不存在**需要补 `content_block_stop` 的索引。

**relaykit 现状**（`relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go:342-362`）：无条件发出 `content_block_start` + `thinking_delta`，索引由 `stopOpenBlocksAndAdvance` 分配：

```go
// :137 定义；:150-152 的 default 分支执行 state.Index++
stopOpenBlocksAndAdvance := func() {
    if state.LastMessagesType == convmeta.LastMessageTypeNone {
        return
    }
    appendStopOpenBlocks()
    switch state.LastMessagesType {
    case convmeta.LastMessageTypeTools:
        state.Index = state.ToolCallBaseIndex + len(state.ToolCalls)
        …
    default:
        state.Index++                        // 索引在此推进
    }
    state.LastMessagesType = convmeta.LastMessageTypeNone
}

// :342-362 调用点：reasoning 非空即无条件建块
if reasoning != "" {
    if state.LastMessagesType != convmeta.LastMessageTypeThinking {
        stopOpenBlocksAndAdvance()           // 已推进 state.Index
        idx := state.Index
        claudeResponses = append(claudeResponses, &dto.ClaudeResponse{Index: &idx, Type: "content_block_start", …})
    }
    state.LastMessagesType = convmeta.LastMessageTypeThinking
    claudeResponse.Delta = &dto.ClaudeMediaMessage{Type: "thinking_delta", Thinking: &reasoning}
}
```

`state` 是 relaykit 的**私有内部状态**（`ResponseStreamState` 的字段不导出），宿主拿到的是已生成的 `[]*dto.ClaudeResponse`。后置删除 thinking 块会导致：

- 后续块的索引**无法重排**（relaykit 已按含 thinking 的顺序分配完毕）
- 产出**索引空洞**：对未 `content_block_start` 的 index 发 delta

Anthropic SSE 状态机要求 `start → delta* → stop` 逐 index 闭合，索引空洞会被客户端拒绝。

#### 12.4.2 决定

**在 relaykit 内门控，用 `convmeta.Options` 钩子**——与既有的 `PreserveThinkingSuffix func(modelName string) bool` 同型：

```go
// convmeta.Options 新增
RetainReasoningInClaudeResponse func(modelName string) bool   // nil = 不保留（与上游一致）
```

**可达性分两侧，成本不同**（已实测）：

- **Chat→Claude 流式**（`internal/oai_chat/to_claude_messages_resp.go:111` `StreamResponseOpenAI2Claude`）：**已接收 `info convmeta.Meta`**，且 `Meta` 已含 `ConvOptions() *Options`（`convmeta/meta.go:18`）。只需在 `:342` 建块前加一次 `info.ConvOptions().RetainReasoningInClaudeResponse(...)` 判断，**无需新增透传参数**
- **Chat→Claude 非流式**（同文件 `:449` `ResponseOpenAI2Claude`）：同样已接收 `info convmeta.Meta`，同样零透传成本
- **Responses→Claude 非流式/流式**（`internal/oai_responses/to_claude_messages_resp.go:15`、`internal/oai_responses/to_claude_messages_stream_resp.go:82`）：**不接收 `Meta`**（前者只收 `*dto.OpenAIResponsesResponse`，后者的 `ResponsesToClaudeStreamState` 不持有 `info`）。若这两条路径也需要门控，才需要 6.1 第 3 点所述的透传改造

钩子只回答策略、不捕获请求状态（见 12.10）。**这条推翻了第 6.1 节「响应方向不打 relaykit 补丁」的结论**——该结论的前提是「后置过滤可行」，现已被推翻。6.1 关于「不能按客户端意图门控」的三条论据仍然成立（客户端意图在响应阶段取不到正确值），但结论要改为：**门控依据是宿主策略，不是客户端意图，且落点在 relaykit 内部的索引分配之前**。

#### 12.4.3 AWS Converse 路径不得套用 DeepSeek 策略

**早期版本写「AWS 路径用同一 helper」，会引入回归。** 实测：

- `relay/channel/aws/converse.go:748` **无条件**把 Bedrock `ContentBlockMemberReasoningContent` 转成 `{Type: "thinking"}` 块
- 全文件 **0 处**调用 `shouldExposeReasoningAsClaudeThinking`

而该策略实测就是 `hasDeepSeekModelName`（`service/convert.go:526`）——**只对 DeepSeek 返回 true**。套用后 Bedrock Claude 的 reasoning 会被**新抑制**，这是行为回归。

且 AWS 流式（`:1110`）在 switch 内联发 `thinking_delta`，不存在「自有生成逻辑之后」的单一过滤点。

**决定**：AWS 路径**维持现状**（无条件暴露），除非有独立理由要求门控。若将来需要，必须为其单独定义策略函数，不得复用 DeepSeek 判定。

**影响**：`convmeta.Options` 新增 1 个钩子；relaykit 的 `to_claude_messages_resp.go` 与 `to_claude_messages_stream_resp.go` 各加一处索引分配前的门控；宿主 `ConvOptions()` 填充该钩子。第 6.1 节需同步修订。

### 12.5 DTO 独有方法归属

**⚠ 本表只覆盖 4.4 的 23 个方法（3 个文件），不是 dto 包的全貌。** 全包是本地 394 个函数 vs relaykit 273 个（`^func` 口径），另有 33 个类型字段不兼容（12.0.1，工作区口径 34）。完整清单是阶段 2 的开工前置。

按 12.0 的硬约束分类。**A 类**迁入 `relaykit/dto`（上游化），**B 类**降为宿主自由函数，**C 类**删除。**注意**：A 类的「迁入」意味着连带 327 个 import 点的改写，不是零成本。

| 方法 | 生产引用 | 分类 | 理由 |
|---|---|---|---|
| `Message.ParseContentStrict` | 12 | **A** | 协议层严格解析，见 12.1 |
| `Message.ParseToolCallsStrict` | 11 | **A** | 同上 |
| `OpenAIResponsesRequest.HasCompactionTrigger` | 9 | **B** | compaction 是宿主特性；类型迁走后写成 `hostdto.HasCompactionTrigger(r)` |
| `ResponsesStreamResponse.GetOpenAIError` | 9 | **A** | 协议响应字段访问器 |
| `OpenAIResponsesRequest.UnmarshalJSON` | 5 | **A** | presence 保真，即 3.3 的补丁 2 |
| `Reasoning.UnmarshalJSON` | 5 | **A** | 同上 |
| `StreamOptions.UnmarshalJSON` | 5 | **A** | 同上 |
| `Usage.EnsureResponsesTokenDetailObjects` | 3 | **需实测** | 与 relaykit `InputTokenDetails.*`（`usage_merge.go`）可能同域。先比对语义再定 A 或 C |
| `IncompleteDetails.EffectiveReason` | 2 | **A** | 推理语义，relaykit 有 `reasoning_state.go` 可承接 |
| `OpenAIResponsesResponse.HasCompactionOutput` | 2 | **B** | 同 `HasCompactionTrigger` |
| `OpenAIResponsesResponse.GetQuality` | 2 | **A** | 图像响应字段访问器 |
| `OpenAIResponsesResponse.GetSize` | 2 | **A** | 同上 |
| `OpenAIResponsesResponse.HasImageGenerationCall` | 2 | **需实测** | 上游 `relay/common/tool_usage.go` 的 `ImageGenerationCallCounter.Observe` 是同域的**更强**实现（四别名去重）。须先确认本地这 4 个调用点是否可改用计数器的语义；不可则 A |
| `Usage.GetOutputTokenDetails` | 2 | **需实测** | 同 `EnsureResponsesTokenDetailObjects` |
| `Reasoning.MarshalJSON` | 2 | **A** | presence 保真 |
| `StreamOptions.MarshalJSON` | 2 | **A** | presence 保真 |
| `Reasoning.Clone` | 1 | **A** | 值拷贝，无策略 |
| `StreamOptions.Clone` | 1 | **A** | 同上 |
| `StreamOptions.HasIncludeUsageField` | 1 | **A** | presence 保真 |
| `StreamOptions.HasIncludeObfuscationField` | 1 | **A** | presence 保真 |
| `StreamOptions.RemoveIncludeUsage` | 1 | **A** | presence 保真 |
| `ChatCompletionsStreamResponse.GetError` | 1 | **A** | 协议响应字段访问器（工作区口径） |
| `ClaudeRequest.SearchToolNameByToolCallId` | 1 | **A** | 工具名契约，属协议层 |

**A 类 18 个、B 类 2 个、待实测 3 个。** 另有 2 个（`IsValidToolName`、`ToolNameMaxLength`）生产引用为 0，**C 类删除**或随 `tool_name.go` 整体迁入——倾向后者，因为 `NormalizeToolName` 与它们同文件且必须迁入。

**relaykit 独有 5 个方法**（迁移会丢）：`GeneralOpenAIRequest.MarshalJSON`、`CachedTokenDetails.HasTokens`、`InputTokenDetails.Clone`、`InputTokenDetails.CacheCreationTokensTotal`、`ResponsesOutput.MarshalJSON`。前三者对应 relaykit 的 `usage_merge.go` / `reasoning_state.go`——本地 `Usage.*` 与 relaykit `InputTokenDetails.*` 是**同一能力域的两套实现**，对齐方式见 12.5 的「需实测」三项。

### 12.6 命名空间工具与 tool_search → 保留在宿主

**决定**：`service/openaicompat/` 的命名空间工具与 tool_search 能力**整体保留在宿主**，不迁 relaykit。

**依据**：

- 上游四作用域（`relaykit/`、`relaykit/relayconvert/`、`service/`、`relay/`）**零命中** `namespace`；`tool_search` 唯一命中是计费字段。这是本地独有的协议扩展，不是上游遗漏
- 二者都依赖 `ChatCompletionsToResponsesPolicy` 的 rules 模式（12.8），该策略本身是宿主配置体系的一部分
- 上游把「是否转换」的决策明确留在宿主——其 `service/openai_chat_responses_mode.go` 注释原文：*"Chat→Responses upgrade policy is host routing logic (it decides **whether** to convert, reading host settings), so it lives here, not in relayconvert."*

**影响**：`service/openaicompat/` **整体保留**（见 12.15——按能力切分在函数粒度不成立）。命名空间能力所依赖的字段（`ResponsesOutput.Namespace`、`ChannelOtherSettings` 的命名空间相关字段）**必须留在宿主可达的位置**：实测这些字段横跨 `dto`、`controller`、`model`、`service/openaicompat` 四个包，生产引用点分散在 `controller/responses_native_tool_identity.go`、`model/channel_responses_tool_capability.go`、`controller/relay_responses_tool_observation.go` 等处（**精确计数待补，见 12.18 第 26 项**）。若 `dto` 类型整体迁入 relaykit，则这些字段要么一并迁入（使 relaykit 承载宿主协议扩展），要么由宿主维护影子结构——**这是阶段 2 必须一并决策的耦合点**，见 12.0.2。

### 12.7 custom tool bridge 门控 → 保留本地语义

**决定**：保留「默认拒绝、显式开启」（`EnableCustomToolBridge`）。

**依据**：AGENTS.md 第 1 节「禁止隐藏回退、静默容错、默认降级」。上游无条件处理 custom tool 会把无法保真的调用静默放行；本地默认拒绝是 fail-closed，符合仓库约束。这是有意的行为差异，不是待修的缺口。

### 12.8 `matchAnyRegex` 空模式语义 → 采用本地语义，并显式文档化

**决定**：保留本地语义（空 `model_patterns` = 匹配全部），并在配置文档与 UI 中写明。

**依据**：`AllChannels`/`ChannelIDs` 与 `ModelPatterns` 是两级过滤。本地语义下「选中渠道但不填模型模式」= 该渠道全模型启用，符合直觉；上游语义下同样的配置**静默不生效**（匹配空集），是更危险的默认值——用户以为开了实际没开。AGENTS.md 禁止静默失效。

**影响**：`service/openaicompat/regex.go` 的 `matchAnyRegex` 保留；上游的 `matchAnyModelPattern` 若被引入，须替换为本语义。

### 12.9 上游 relaykit 分支 → 不作为补丁基线（第 10 节第 15 项）

**决定**：`feat/relaykit-tools`、`feat/relaykit-perf` **均不采用**。

**依据**（已实测）：

- 两分支日期为 2026-09-01 / 2026-09-03，**早于** `upstream/main` 的 2026-09-28——是主线的**前身**，不是后续
- 两分支均**不是** `upstream/main` 的祖先：`git merge-base --is-ancestor <c> upstream/main` 对 6 个提交（`55bba35f2`、`bbe20f2e1`、`c82c03ecd`、`60e15b53a`、`b45639116`、`a9a0e2272`）**全部 exit=1**
- `feat/relaykit-tools` 相对 main 是 **−3910 行**（**限定 relaykit 子目录口径**；全仓为 `1251 files changed, 35412 insertions(+), 151592 deletions(-)`），形态是「尚未完成的重构」
- 其核心提交 `a9a0e2272`（`preserve hosted tools across conversions`）的内容**已作为 PR #7137（`0ed497f06`）并入 main**，且 `0ed497f06` 是 main 的祖先

**「main 已含最终形态」的完整取证**（这是本结论的核心，不能只靠祖先判断——6 个提交都不在 main 里，必须证明其内容已被别的形式覆盖）：

| 核验 | 结果 |
|---|---|
| `toolconv/` 7 个文件逐字节比对 | 5 个 SAME；`decode.go`、`encode.go` 有差异，方向是 **main 更新**——且是**功能新增**，不是清理：main 的 `decode.go` 多了 `decodeOpenAIChatPseudoHostedTool`（把 Gemini 的 `googleSearch`/`codeExecution`/`urlContext` 伪工具名识别为 hosted 工具），`encode.go` 的 `KindCodeExecution`/`KindURLContext` 分支多了 `definition.NativeType` 判断。分支上**没有**这些。同两文件另含 `function := function` 遮蔽删除与 `interface{}` → `any`，那部分是清理 |
| `reasoning/claude.go` 导出符号集 | 分支独有 **0** 个，main 独有 **0** 个——导出符号集完全相同 |
| `dto/billing_usage.go`、`dto/usage_merge.go` | 分支独有**导出**符号 0 个；diff 方向为 main 净增（`usage_merge.go` `52+/7−`、`billing_usage.go` `25+/38−`；后者的净变化来自函数体改写，非删能力） |
| `relaykit/` 整体 | 分支相对 main 是 `68 files changed, 3910 insertions(+), 746 deletions(-)`——**main 净增** |
| `ClaudeToChatStreamState` / `ChatToGeminiStreamState` | 类型定义在 main 与分支同名同语义（`to_oai_chat_resp.go:126`、`to_gemini_chat_resp.go:13`）；文件命中数 main 与 `feat/relaykit-perf` 各为 6/6，`feat/relaykit-tools` 为 4/2（该分支较旧） |
| `toolconv` 的 `Kind*` 常量 | 两边均 **10 个** |

**结论**：main 已包含这些分支的最终形态，且在这些区域**更完整**。以 main 为基线即可，无需 cherry-pick。

**⚠ 方法学注记**：早期版本仅凭「分支不是 main 祖先」+「toolconv 有 10 个 Kind」就下了「main 已含最终形态」的结论——**这是不充分的**。6 个提交均不在 main 中，必须逐一证明其内容已被覆盖。上表的逐字节/符号集比对是后补的取证。

### 12.10 `ConvOptions` 缓存与跨渠道重试（第 10 节第 10、11 项）

**决定**：**无需改动**，上游已正确处理。

**依据**（已实测）：上游 `relay/common/relay_info.go:311` 在 `InitChannelMeta` 里显式置 `info.convOptions = nil`，注释原文：*"Channel identity feeds the converter options snapshot (e.g. OpenRouterDialect); drop the cache so a cross-channel retry rebuilds it."* 每次换渠道都重建快照，`OpenRouterDialect`、`ToolLossPolicy` 等渠道相关字段不会读到上一个渠道的值。

**第 11 项（钩子闭包捕获哪个 `UpstreamModelName`）随之收窄**：钩子签名是 `func(modelName string) bool`，**由 relaykit 在调用点传入当前模型名**，不捕获 `info`。因此不存在「闭包捕获了哪个名字」的问题——调用方决定。本地新增钩子必须遵循同一约定：**只回答策略，不捕获请求状态**。

### 12.11 `reasonmap` 宽松 default（第 10 节第 12 项）

**决定**：**保留上游宽松 default**，不改严格模式。

**依据**：`reasonmap.OpenAIFinishReasonToClaudeStopReason` 的 `default: return finishReason` 与本地 `stopReasonOpenAI2ClaudeStrict`（`service/convert.go:1125`）行为不同。但 finish_reason 的严格校验属宿主关切（本地该严格函数服务 Claude 响应方向），应在**宿主调用点**做。relaykit 作为通用转换套件，对未知 finish_reason 透传是合理默认——它不知道上游会发明什么新值。严格化应在宿主调用点做，不污染套件。

### 12.12 thinking block 的 `signature`（第 10 节第 13 项）

**决定**：**维持现状**（请求方向接受「有文本、无签名」的块；响应方向不生成签名），标注为**已知未验证项**。

**依据**（已实测）：本地 `service/convert.go:233-250` 的 `case "thinking"` 只在「文本为空且签名非空」时报错（`claudeOpaqueThinkingConversionError`），有文本的块直接接受并剥离签名。这与 relaykit 产出的 `{Type, Thinking}`（无签名）兼容，往返可用。

**未验证**：Anthropic 是否校验签名、第三方客户端是否接受无签名 thinking 块。**这是运行时可观测项，不是代码可判定项**——须在隔离环境用真实 Claude 客户端验证，列入阶段 4 验证清单（第 9.3 节）。

### 12.13 `enabled` 双语义（第 10 节第 14 项）

**决定**：**保留并文档化**。`rules` 非空且未显式给 `enabled` 时，`UnmarshalJSON`（`setting/model_setting/global.go:31`）自动置 `Enabled = true`。

**依据**：这是为「只写 rules 不写 enabled」的配置提供向后兼容。风险是「显式 `enabled: false` + 非空 rules」与「省略 enabled + 非空 rules」行为不同。由于 `UnmarshalJSON` 用 `fields["enabled"]` 的存在性判断，两者已可区分，语义是确定的、非歧义的。保留现状，在配置文档写明。

### 12.14 未跟踪文件（第 10 节第 19 项）

**决定**：**迁移前必须先纳入版本控制**，且须与拥有这些改动的会话确认最终形态。

**依据**：`relay/common/responses_tool_names.go` 及 2 个测试文件未跟踪，其 3 个调用点也全部未提交——在 HEAD 上该能力完全不存在。与 `CR-UNTRACKED-SOURCE-DEPENDENCY-2026-09-22.md` 同型但更严重。列为阶段 0 门禁。

### 12.15 `service/openaicompat/` 收敛边界（第 10 节第 20 项）

**早期版本的「按能力切分、基础转换转调 relaykit」在函数粒度上不成立**，实测如下。

#### 12.15.1 为什么不能按能力切开

`convertResponsesToolsToChat`（`responses_to_chat_request.go:455`）的签名同时接收 namespace 选择与 toolState：

```go
func convertResponsesToolsToChat(raw []byte, options ResponsesChatCompatibilityOptions,
    namespaceSelection *responsesNamespaceToolSelection,
    toolState *ResponsesChatCompatibilityToolState) ([]dto.ToolCallRequest, error)
```

函数体（115 行，其中 `for index, rawTool := range rawTools` 循环占 93 行）内符号分布：`namespace` 16 处、`toolState` 5 处、`ToolSearch` 5 处、`Custom` 4 处（不含 `EnableCustomToolBridge`）、`EnableCustomToolBridge` 3 处——**五种工具语义交织在同一循环**，共享 `toolState.register` 与 `namespaceSelection` 过滤。

响应侧同样耦合：`chat_to_responses_response.go:193/222/243/259` 依赖 `toolState.Resolve` 做工具名还原。

而 relaykit 的 `ResponsesRequestToChatCompletionsRequest` 签名**不接受** namespace filter 或 toolState，上游 shim 也**没有 state 变体**。若「基础转换转调 relaykit、namespace 保留本地」，request 侧的工具注册与 response 侧的还原会**断裂**——要么保留整份本地实现，要么 namespace 还原静默失效（违反 AGENTS.md 禁止静默降级）。

#### 12.15.2 消费者不止 2 个

早期版本写「消费者仅 2 个文件」。实测还有第 3 个：`relay/responses_via_chat.go:104` 经 shim 调用 `service.ResponsesRequestToChatCompletionsRequestWithState`——**正是需要 toolState 往返的那条链**。

#### 12.15.3 决定

**`service/openaicompat/` 整体保留**，不按能力切分。

**依据**：

- 12.15.1 的耦合使切分在函数粒度不可行；强行切分只会得到「一半用 relaykit、一半用本地」的双实现，且状态不通
- 该包的本地扩展（namespace、tool_search、custom bridge 门控、rules 策略）上游零覆盖（12.6–12.8），本就必须保留
- 其基础转换虽与 relaykit 重复，但重复的代价远小于切分后状态断裂的代价

**若将来要真正收敛**，唯一可行路径是先把 `responses_to_chat_request.go` 的 tool 循环重构为**可注入的 tool-transformer 链**，让基础分支可独立转调 relaykit。这是一次独立重构，不在本次迁移范围内。列为第 10 节新增未决项。

**影响**：`service/openaicompat/` 行数不减少；`service/convert.go` 的主体替换（阶段 4）不受影响。

### 12.16 验证策略：24 个上游快照不足以保证本地不退化（第 10 节第 9 项）

**决定**：**需实测，且必须补建本地的 golden 基线。**

**问题**：`relaykit/relayconvert/testdata/golden/{request,response,stream}/` 的 **24 个 JSON**（已实测）是**上游自己的基线**——它验证的是「上游行为未变」，**不是「本地行为未退化」**。

具体缺陷：

- 打补丁 1/2 后快照会因「presence 保真生效」而 fail，上游的 `-update` 机制会把差异**直接吸收为新的正确值**——无法区分「保真生效」与「行为回归」
- 第 9.1、9.2 节列出的 **12 处本地更强/更严行为**（未知 block 报错、image 缺 source 报错、多 choice 报错、`finish_reason` 严格校验、非 assistant thinking 门控、tool_use 缺 id/name 报错…），这 24 个快照**一处都测不到**
- 早期版本称其为「唯一的机械回归网」——该表述不成立

**决定**：

1. **补建本地的 golden 基线**：用当前 `HEAD` 跑同一批 fixture 生成快照，作为「本地行为」的对照。这是「本地没退化」这一核心目标的**唯一**验证手段
2. **补建针对 9.1/9.2 的 12 处差异的专项用例**，每处至少一个：输入侧（未知 block、image 缺 source、非 assistant thinking、tool_use 缺字段）与输出侧（多 choice、`finish_reason` 严格、命名空间往返、AWS 流式索引、Bedrock 非 DeepSeek thinking）
3. 第 9.3 节的专项清单需扩充，覆盖 12.4.1（索引空洞）、12.4.3（AWS 回归）、12.6（命名空间往返）、12.8（`matchAnyRegex` 语义）、12.7（custom bridge fail-closed）
4. 12.12 的 thinking `signature` 与本节均属「需实测」，须给出可执行命令而非仅标注

**依据**：AGENTS.md 第 6 节「不得把局部测试通过表述成全链路已验证」。若以「快照全绿」作为完成判据，实际会得到**上游行为未变 + 本地行为未知**的结论。

### 12.17 决策汇总

| # | 未决项 | 决定 |
|---|---|---|
| 1 | 本地独有能力盘点 | **已完成**（第 4 节） |
| 2 | think 标签族归属 | 剥离迁 relaykit，门控留宿主 |
| 3 | 本地专有错误符号 | 迁入 `relaykit/types` |
| 4 | `*WithError` 系列 | 删除 |
| 5 | 响应方向暴露策略形态 | **relaykit 内门控**（`convmeta.Options` 钩子），在索引分配之前。后置过滤不可行，见 12.4。落点为 `oai_chat/to_claude_messages_resp.go`（已可达 `Meta`，零透传成本） |
| 6 | DTO 方法归属 | 4.4 的 23 个：A 18 / B 2 / 待实测 3。**全包 394 vs 273 函数**，完整差集是阶段 2 前置 |
| 7 | hosted 工具收敛 | 采用上游 `toolconv`（10 种 `Kind`），本地非测试只有 3 种且是其子集，差集为空 |
| 8 | `types` 别名转发范围 | 仅 `endpoint_type.go`、`finish_reason.go` 两类常量；其余改 import |
| 9 | 补丁 2 对 golden 快照 | 需实测，逐条核对 |
| 10 | `ConvOptions` 缓存语义 | 无需改动，上游 `:311` 已处理 |
| 11 | 钩子闭包捕获 | 收窄：钩子只答策略、不捕获状态 |
| 12 | `reasonmap` 严格模式 | 保留宽松 default，严格化留宿主 |
| 13 | thinking `signature` | 维持现状，列入运行时验证 |
| 14 | `enabled` 双语义 | 保留并文档化 |
| 15 | 上游 relaykit 分支 | 不采用，main 已含最终形态 |
| 16 | 命名空间/tool_search 归属 | 保留宿主；其字段跨 4 个包，阶段 2 须一并决策（12.6） |
| 17 | custom tool bridge 门控 | 保留本地 fail-closed |
| 18 | `matchAnyRegex` 空模式 | 采用本地语义并文档化 |
| 19 | 未跟踪文件 | 阶段 0 门禁，先提交 |
| 20 | `openaicompat` 收敛边界 | **整体保留**。按能力切分在函数粒度不成立（12.15） |

### 12.18 本轮审查新增的未决项

| # | 项 | 说明 |
|---|---|---|
| 21 | `dto` 完整方法级差集 | 全包 394 vs 273 函数，须逐项给出出路。阶段 2 前置（12.0.2） |
| 22 | `dto` import 图分析 | 12 个非测试 dto 文件 import `common`、2 个 import `constant`，须确认无 `dto → constant → relaykit/types → dto` 环（12.0.3） |
| 23 | 33 个字段不兼容类型的逐项对齐（基线口径，工作区 34） | 12.0.1 已列清单，取舍未逐项给出 |
| 24 | `openaicompat` tool 循环重构 | 让基础分支可独立转调 relaykit 的唯一路径；独立重构，不在本次范围（12.15.3） |
| 25 | 本地行为的 golden 基线 | 24 个快照是上游自己的基线，测不到 9.1/9.2 的 12 处本地差异。须用当前 HEAD 生成本地基线（12.16） |
| 26 | 命名空间字段的生产引用精确计数 | 12.6 早期写「18 个生产点」无可复现口径，已改为定性表述 |
