# 协议转换层收敛到上游 relaykit

采样时间：2026-09-30
本地基线：`dbfbc096f`（写作时即 `HEAD`；全文「基线」均指它。dev 分支，写作时领先 `origin/dev` 56 个提交）
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
| 思考等级渲染 | `relay/claude_handler.go:55-104` 硬编码 `claude-opus-4-6/4-7` 前缀判断 | `reasoning.RenderClaude` + `claudeCapabilitiesFor`（`relaykit/relayconvert/reasoning/claude.go:35-78`）覆盖 10 个模型前缀：`claude-fable-5`、`claude-mythos-5`、`claude-mythos-preview`、`claude-opus-4-5/4-6/4-7/4-8/5`、`claude-sonnet-4-6/5`，含 `defaultThinking`、`supportsEffort/XHigh/Max`、budget↔effort 互转与冲突诊断 | 采用上游 |
| hosted 工具 | 散落在 `service/convert.go` 与 `relay/common/` | `relaykit/relayconvert/internal/toolconv/`（`decode.go`、`encode.go`、`hosted_values.go`、`model.go`、`response.go`、`response_artifacts.go`），覆盖 10 种 hosted 工具：`KindWebSearch`、`KindCodeExecution`、`KindComputerUse`、`KindFileSearch`、`KindImage`、`KindMCP`、`KindURLContext`、`KindWebFetch`、`KindFunction`、`KindNative` | 采用上游 |
| 降级可观测性 | `ConversionDiagnostic` 本地零命中 | `convdiag` + `types.ConversionDiagnostic`（Code/Path/Message/Severity/From/To） | 新增能力 |
| 转换质量分级 | 无 | `Good`/`Fair`/`Discouraged` 三级 + 4 个文本格式（Claude / Gemini / OpenAI / OpenAIResponses）间的 12 个内建转换器（`builtinTextConverters`，覆盖 4 格式的全部 12 个有序对，即 6 个双向对，无单向）各自标注 quality | 新增能力 |
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

**盘点口径**：以下清单同时核对 `HEAD`（`2cf100a1d`，**§4 盘点时点的 HEAD；与头部声明的基线 `dbfbc096f` 是不同提交——第 39 轮注明，此前两者同名易混**）与工作区两套口径，差异处逐项标注。工作区含其他会话的未提交改动（`dto/` 6 个文件、`relay/common/` 4 个文件），本清单不触碰、不回退，仅在计数受影响处注明。未逐项核验的边界见 4.5.5。

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

**注意本迁移共新增 2 个钩子，方向不同、不可混为一谈**：本节的是 `RetainEmptyReasoning`（**请求**方向，对应本地 `service/convert.go:365` 的 DeepSeek 空 thinking block 保留，在 `ClaudeToOpenAIRequest` 内）；12.4.2 的是 `RetainReasoningInClaudeResponse`（**响应**方向，对应本地 `service/convert.go:749/:1049` 的 `shouldExposeReasoningAsClaudeThinking`）。两者签名相同（`func(modelName string) bool`），语义与落点均不同。

### 4.3 本地专有契约（上游完全不存在）

本地 `types/error.go` 定义 37 个 `ErrorCode` 常量，上游 `relaykit/types/error.go` 定义 33 个，**上游独有 0 个、本地独有 4 个**：

| 符号 | 生产引用文件数 | 说明 |
|---|---|---|
| `ErrorCodeUpstreamTransportInterrupted` | 14（含测试共 25） | 上游传输中断 |
| `ErrorCodeChannelConcurrencyExceeded` | 3 | 渠道并发超限 |
| `ErrorCodeUpstreamRequestTooLarge` | 3 | 上游请求体过大 |
| `ErrorCodeChannelConcurrencyUnavailable` | 2 | 渠道并发不可用 |

另有 2 个本地独有函数（上游全仓 0 命中）：`IsUpstreamTransportInterruptedError`（生产 8，含测试共 9）、`IsOpenAIRequestParameterError`（5，无测试引用）。

**勘误**：本文档早期版本写「上游宿主 `types/` 没有 `error.go`」。字面为真，但会误导——上游把该文件整体迁到了 `relaykit/types/error.go`，其中 33 个常量与本地同名同值。准确表述是「上游 `error.go` 存在且覆盖本地 37 个中的 33 个，缺 4 个」。同理，早期列的「25 / 9 / 5」是**含测试文件**的口径，非生产引用数。**第 39 轮补注口径**：本节表格的「生产引用文件数」按 `git grep -lw <符号> -- '*.go' ':(exclude)*_test.go'` 计，**含定义文件 `types/error.go` 自身**（如 14 = 13 个引用文件 + 定义文件；`IsOpenAIRequestParameterError` 的 5 = 4 + 1）；§4.4 的表则**排除定义文件自身**，两表口径不同，此前未注明。

**已决定**（见 12.2、§12.17 第 3 项）：这 4 个常量与 2 个判定函数**全部迁入 `relaykit/types`**，不保留宿主副本。理由是若留在宿主会出现两个 `ErrorCode` 类型，所有跨边界的 `switch` 与比较都要转换。

另注依赖方向：本地 `types/error.go:11` import 宿主 `common`，上游 `relaykit/types/error.go:10` import `relaykit/relayconvert/kitutil`——即 `common` 的相关 helper 已被抽到 `kitutil`，`types` 迁移实际牵动 `common → kitutil` 替换。逐文件核验本地 `types/`（基线 10 个非测试文件）：仅 `error.go` 与 `rw_map.go` 依赖宿主 `common`，**其余 8 个无宿主依赖**。

### 4.4 本地 DTO 独有方法

**本节只覆盖 3 个文件，不代表 dto 包的全貌。** 按 `接收者.方法名` 机械对比本地 `dto/{openai_request,openai_response,claude}.go` 与上游 `relaykit/dto/` 同名文件的方法集：

| 文件 | 本地 | relaykit | 本地独有 | relaykit 独有 |
|---|---|---|---|---|
| `openai_request.go` | 43 | 31 | **13** | 1 |
| `openai_response.go` | 25（HEAD 22） | 20 | **9**（HEAD 6） | 4 |
| `claude.go` | 31 | 30 | **1** | 0 |
| 三个文件合计 | | | **23**（HEAD 20） | 5 |

**⚠ 全包口径远大于此**：本地 `dto/` 共 **394 个函数**，relaykit/dto **273 个**（`^func` 口径、**含测试**；非测试为 306 / 215，见 12.0.5）。单 `dto/channel_settings.go` 本地就有 **61 个函数**（`^func` 口径）、relaykit 同名文件 29 个；按 `func (recv) Name` 提取后求差集，其中 **42 个本地独有方法**（`ResponsesCompactModeOrDefault`、`ResolveResponsesChannelCapability`、`MarkResponsesCompactAutoFallback` 等），逐个按 `接收者.方法名` 口径在上游全仓命中数为 0。

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

**口径（第 32 轮补注，此前未写明；勘误表只讲了「早期口径错在哪」，没讲「现在这一列是什么」）**：该列是**「提到该方法名的文件数」**，命令为 `git grep -lw <方法名> -- '*.go' ':(exclude)*_test.go' ':(exclude)dto/*'`——**按名字匹配，不限定接收者、也不区分调用与声明**。故它会把同名但不同类型的方法算进来，也会把**仅声明字段而无调用**的文件算进来（如 `common/responses_request_metadata.go:118` 的 `HasCompactionTrigger bool` 字段声明、`common/responses_request_metadata_shape.go:163` 的 `metadata.HasCompactionTrigger = shape.hasCompactionTrigger` 赋值——这两处都不是 `dto` 那个方法的调用，却各计 1）。

第 32 轮实测：按此口径 23 行中 **21 行可复现**；唯二例外是 `Reasoning.Clone` 与 `StreamOptions.Clone`（doc 各写 1，按名字匹配为 4）——**这两行反而是唯一按「真实调用」量的**（勘误表已说明：只被 `service/responses_codex_local_compaction_recovery.go:205` 与 `:224` 调用，同一文件，故文件数为 1）。**即整列 23 行里有 2 行是调用口径、21 行是名字口径**，两种口径混在同一列。

**这一列只用于判断「迁走后会删掉多少引用点」，不用于判断「引用是否在宿主业务侧」**——后者看理由列的文字。**本次只补注口径、不改数字**：改口径会连带改动 23 行的取值与分类依据的观感，而分类本身（A/B/C）不依赖这个数。（12.6 的命名空间计数用的是另一条命令 `git grep -lE '<正则>' HEAD -- '*.go' | grep -v _test.go`，**含 `dto/`、按正则匹配**，与本列口径不同，不受此处影响。）

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
- **relaykit 独有 5 个方法（仅上述三个文件口径）**：`GeneralOpenAIRequest.MarshalJSON`（`relaykit/dto/openai_request.go:136`）、`CachedTokenDetails.HasTokens`（`relaykit/dto/openai_response.go:284`）、`InputTokenDetails.Clone`（`:290`）、`InputTokenDetails.CacheCreationTokensTotal`（`:316`）、`ResponsesOutput.MarshalJSON`（`:397`）——**四个在 `openai_response.go`、一个在 `openai_request.go`**（**第 42 轮修正**：原文写「前三个对应 relaykit 的 `dto/usage_merge.go` 与 `dto/reasoning_state.go`」，实测无一在这两个文件里；本地无同名文件的是整个 `dto/usage_merge.go`/`dto/reasoning_state.go`，不是这几个方法）。它们与本地的 `Usage.EnsureResponsesTokenDetailObjects` / `GetOutputTokenDetails` **共享 `Usage` 的 token-detail 数据域、但能力互不重叠、迁移后必须并存**（relaykit 侧是缓存写计费与存在性感知深拷贝，本地侧是两种拼写的桥接与回退读取），**不存在二选一**（**第 42 轮修正**：原文写「是同一能力域的两套实现，迁移时须做语义对齐，不能简单二选一」，前半勉强成立、后半「两套实现」会误导读者以为需取舍）。**注意：全包口径是 31 个 relaykit 独有方法，不是 5 个**（5 个只覆盖这三个文件），见 12.0.5 第二节

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
| 模型模式匹配语义 | `regex.go` `matchAnyRegex` | **语义不同**：本地空 patterns → `true`（匹配全部）；上游 `upstream/main:service/openai_chat_responses_mode.go:15` `matchAnyModelPattern` 空 patterns → `false`（不匹配任何）。迁移须显式决定取哪个语义 |
| 严格拒绝行为（`Rejects…` 系列：不支持的 role/include/previous_response_id、加密 tool output、嵌套 tool_search、别名冲突、重复命名空间工具名） | `responses_to_chat_request.go`、`chat_to_responses.go` 多处 | **部分覆盖**：上游有 `validateResponsesRequestChatUnsupportedFields`（`internal/oai_responses/to_oai_chat_req.go:119`）与 `ValidateRequestChatUnsupportedFields`（`:139`），但覆盖范围窄于本地 |

消费者（生产，**3 个文件**）：`service/openai_chat_responses_compat.go`（21 处引用）、`service/openai_chat_responses_mode.go`（5 处）、`relay/responses_via_chat.go:104`（调 `…WithState`，需 toolState 往返）。**注意**：早期版本写「仅 2 个文件」是错的，漏了第 3 个；且该包的收敛边界不是「只需处置这几个文件」——见 12.15，按能力切分在函数粒度不成立，该包整体保留。

#### 4.5.2 `relay/common/` 本地独有文件

上游 `relay/common/` 有 17 个文件（含测试；非测试 10），本地 30 个（含测试；非测试 16；**第 39 轮修正**：原文写「29 个」，在基线 `dbfbc096f`（26/15）、§4 盘点 HEAD `2cf100a1d`（26/15）与修正时 HEAD `3a903bb47`（30/16）三个口径下均不可复现）。**上游没有任何 `responses_*` 文件**。本地独有并逐项核验（计数为**该文件全部导出符号的并集**，排除 `_test.go` 与定义文件自身）：

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
| `dto/channel_settings.go` | **20 个本地独有字段**（基线口径，工作区 21；`HeaderProfileStrategy`、`ResponsesCompactMode*`、`UserAgentStrategy` 等）+ **42 个本地独有方法** | 上游同名文件 `ChannelOtherSettings` 21 个字段中只有 `ToolLossPolicy`、`AllowIncludeObfuscation` 等少数与协议转换相关，其余为宿主管辖的渠道开关；42 个方法按 `接收者.方法名` 口径上游全仓 0 命中 |
| `dto/user_settings.go` | 3 个字段（`HeaderProfiles`、`RecordRequestContentLog`、`RecordResponseContentLog`） | 上游无 |
| `dto/openai_request.go` | `Reasoning.Extra`、`StreamOptions.Extra`、`Message.ThinkingContent`、`OpenAIResponsesRequest.Extra` | 补丁 2 的 presence 机制 + SenseNova 支持 |
| `dto/openai_response.go` | `ResponsesOutput.{Namespace,EncryptedContent,Execution,Input}`、`ChatCompletionsStreamResponse.Error`、`IncompleteDetails.Reasoning`、`ResponsesStreamResponse.{CallId,Error}`、`ToolCallResponse.Custom`、`ResponsesOutputContent.Refusal` 等 | 命名空间工具、加密内容、流式错误 |
| `dto/openai_responses_compaction_request.go` | 20 个字段 | 上游只有 `PromptCacheOptions` |

**全包实测**：本地 dto **394 个函数** vs relaykit **273 个**（`^func` 口径、含测试；非测试 306 / 215）；**33 个同名类型字段集不同且双向**（基线/HEAD 口径，工作区 34；完整清单见 12.0.1）。因此「宿主业务域留原地」不是可选方案——类型迁走则方法无家可归（12.0.2）。

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

本次盘点覆盖 `service/openaicompat/`（9 个非测试文件全文读取）、`relay/common/` 全部本地独有文件、11 个本地独有 `dto/` 文件、`types/` 与 `constant/` 的集合差集、以及 `service/convert.go` 的 `*WithError` 家族。以下项目在第 12 轮已补齐（见各条后的「已核验」），其余**仍未核验的不得据此下结论**：

- ~~**`relay/channel/{claude,gemini,openai,aws}` 各自通道内的转换辅助函数**~~ **已核验（第 12 轮）**，并推翻原有认知——见下「通道级转换器清单」。
- **`service/` 根目录下的其余本地专有文件**（如 `service/responses_*.go` 系列）。**第 12 轮已做定向核验**：`service/` 根目录 35 个非测试文件 import 宿主 dto；其中协议转换相关的（排除 `responses_*` 与 `convert.go`）只有 `error.go`、`log_info_generate.go`、`task_polling.go`、`usage_helpr.go`、`openai_chat_responses_compat.go` 5 个，均为「读取/构造 dto 以做记录或兼容」，不含独立协议转换语义。`responses_*.go` 系列 11 个文件承载 responses 压缩/恢复的本地扩展，属 §12.15 已定的「保留」范畴
- **`controller/` 层的协议相关逻辑**（`controller/relay.go`、`controller/channel-test.go:957` 等）。**第 12 轮已清点**：8 个文件引用协议 dto（`channel-test.go`、`channel.go`、`model.go`、`relay.go`、`relay_responses_routing_trace.go`、`relay_responses_tool_observation.go`、`responses_native_tool_identity.go`、`swag_video.go`），均为「构造测试请求 / 记录路由轨迹 / 原生工具身份」，不承载协议转换。`channel-test.go:957` 的 `info.Request = chatReq` 是自建请求，随 dto 迁移改 import 即可
- **上游 relaykit 的 3 个演进分支**：**第 12 轮已核验**——`feat/relaykit-tools`（1251 文件）与 `feat/relaykit-perf`（1248 文件）虽改动了补丁 1 的目标文件，但其 `to_oai_chat_req.go` 的 content block switch **仍无 `thinking` case**（实测 case 列表止于 `text`/`input_text`、`image`、`tool_use`、`tool_result`），**补丁 1 未被分支覆盖**，第 10 节第 15 项「不采用分支」的结论成立；`fix/relayconvert-tool-result-media-and-reasoning-parts`（18 文件）不触碰这两个文件
- **`relay/common/responses_input_filter.go` 第 646–1034 行**：**第 12 轮已读取**——其 ID 前缀契约由 `responsesInputItemIDPrefix`（`:151`）定义，列出 14 个 item type 的前缀（`msg`/`rs`/`fc`/`fco`/`ctc`/…），并含本地专有 marker `responsesSyntheticCompactMarkerPrefix = "newapi.synthetic.compact:"`（`:22`）。该能力**在 relaykit 中无任何对应**，是纯本地 responses 扩展，随 §12.15 保留在宿主
- ~~**`dto/` 的完整方法级差集与 import 图**~~ **已完成（第 12 轮），见 12.0.5**
- ~~**33 个字段不兼容类型的逐项对齐方案**~~ **已完成（第 12 轮），见 12.0.5**

#### 通道级转换器清单（第 12 轮新增）

`relay/channel/*/` 内共有 **45 个**协议转换函数（**第 48 轮订正：这是口径内的数；口径外还有 1 个 `CovertOpenAI2Gemini`，纳入后为 46 个**——见下「口径本身的漏项」），分布在 **20 个通道包**。**计数口径（第 37 轮重定，此前写「30 个 / 17 个包」且无命令可复现，作废；第 48 轮补漏，见下）**——函数名含 `<字母>2<字母>` 且含 `request`/`response`（不分大小写）：

```bash
git grep -hE '^func [A-Za-z0-9_]*[A-Za-z]2[A-Za-z][A-Za-z0-9_]*\(' dbfbc096f -- 'relay/channel/' ':!*_test.go' \
  | sed -E 's/^func ([A-Za-z0-9_]+)\(.*/\1/' | grep -icE 'request|response'   # 45
```

包数用同一命令把 `-h` 换 `-l`、末尾接 `| sed -E 's/^[^:]+://' | xargs -n1 dirname | sort -u | wc -l` = 20。按归属分两类：

| 类别 | 数量 | 归属 | 例 |
|---|---|---|---|
| 通道私有协议 | 36 | **保留**——relaykit 无对应，是各上游厂商的私有格式 | `requestOpenAI2Baidu`、`requestOpenAI2Cohere`、`requestOpenAI2PaLM`、`requestOpenAI2Tencent`、`requestOpenAI2Xunfei`、`requestOpenAI2Zhipu`、`requestOpenAI2Dify`、`convertCf2CompletionsRequest` 等 |
| 与 relaykit 重复 | 10（口径内 9 + 口径外 1；**第 48 轮 9 → 10**，补入口径外的 `CovertOpenAI2Gemini`） | **需在阶段 4/6 处置**（见下） | 见下表 |

**第 48 轮——口径本身的漏项（本条是口径缺陷，不是清单偶发遗漏）**：上面的过滤条件（名含 `<字母>2<字母>` **且** 名含 `request`/`response`）会**静默漏掉名字不合规的转换器**。反向枚举全量 `<字母>2<字母>` 函数（去掉 `request|response` 过滤）得 **52 个唯一名**，减去含 `request|response` 的 **44 个唯一名**（对应上表 45 行——`requestOpenAI2Zhipu` 在多个包重复出现，故行数比唯一名多 1），得 **8 个被口径排除的名字**：`apiVersion2domain`、`applyCodexCompactV2ContextSafeguard`、`CovertOpenAI2Gemini`、`oaiFormEdit2AliImageEdit`、`oaiFormEdit2WanxImageEdit`、`stopReasonClaude2OpenAI`、`stopReasonClaude2OpenAIStrict`、`stopReasonCohere2OpenAI`：

```bash
# 全量 <字母>2<字母> 唯一名（52）
git grep -hE '^func [A-Za-z0-9_]*[A-Za-z]2[A-Za-z][A-Za-z0-9_]*\(' dbfbc096f -- 'relay/channel/' ':!*_test.go' \
  | sed -E 's/^func ([A-Za-z0-9_]+)\(.*/\1/' | sort -u | wc -l    # 52
# 再 grep -icE 'request|response' 得 44（唯一名口径），两者相差 8 个被排除名
```

**这 8 个里只有 1 个是危险的**：`CovertOpenAI2Gemini`（注意拼写是 `Covert` 而非 `Convert`）——名字里既无 `request` 也无 `response`，故被口径漏掉，**但它是 relaykit 完整替换的转换器**（568 行 oai→gemini 请求转换，详见下节）。其余 7 个逐一核过、**均无须在此清单登记**：`stopReasonClaude2OpenAI`/`stopReasonClaude2OpenAIStrict` 是 stop_reason 严格函数，已由 9.2.2 与处置清单第 17 项覆盖；`apiVersion2domain`、`stopReasonCohere2OpenAI` 在上游**存在同名同职**（`git grep -c 'func <name>' upstream/main` 各命中 1 个文件，属通道私有助手、relaykit 也保留，两侧一致）；`applyCodexCompactV2ContextSafeguard`、`oaiFormEdit2AliImageEdit`、`oaiFormEdit2WanxImageEdit` 上游零命中（纯本地私有助手，不在 relaykit 替换面内）。**结论：45 这个数字对「relaykit 会替换哪些通道级转换器」而言少算了 1 个**——加 `CovertOpenAI2Gemini` 后，被 relaykit 替换的通道级转换器是 **10 个函数**（9 + 1），它们分属 Gemini 请求/响应两个方向。**本口径缺陷的教训与 §12.20 反复出现的那条同型**：按名字特征做过滤的清单，会对「名字不合规的成员」静默失明；凡声称「已穷举」就必须给反向枚举命令（这里是从 52 反推 8）。

**与 relaykit 重复的是 10 个函数（第 37 轮修正：此前写「4 组 6 个」；第 48 轮再从口径外补入 `CovertOpenAI2Gemini`，故 9 → 10）**：下表 **6 行**，其中第 1、6 行各 1 个函数、第 2–5 行各含 2 个（非 WithError + WithError 变体）。**阶段 6 判据按函数名逐个核，不用行数或组数口径。**

| 本地 | 行数 | relaykit 对应 | 调用方 |
|---|---|---|---|
| `relay/channel/claude/relay-claude.go:255 RequestOpenAI2ClaudeMessage` | 459 | `relayconvert.OpenAIChatRequestToClaudeMessages`（`request_compat.go:20`） | `relay/channel/claude/adaptor.go:100`，**另被 `relay/channel/aws/adaptor.go:173` 与 `relay/channel/vertex/adaptor.go:302` 跨包复用** |
| `relay/channel/claude/relay-claude.go:714 StreamResponseClaude2OpenAI` + `:719 StreamResponseClaude2OpenAIWithError` | — | **`relayconvert.StreamResponseClaude2OpenAI`（`response_compat.go:50`）——公开同名、宿主可达**；实现体在 `claude_messages/to_oai_chat_resp.go:34`（internal，宿主不可 import，**第 35 轮修正本列**：原指向 internal 路径，违反 12.16.2 自己的可达性规则）。上游把本地同名函数保为一行 facade（`upstream/main:relay/channel/claude/relay-claude.go:37`），**WithError 变体上游已删**（`git grep -c 'func StreamResponseClaude2OpenAIWithError' upstream/main -- 'relay/'` = 0） | `relay/channel/claude/relay-claude.go:1167`（生产）、`relay_claude_test.go:689,697`（测试） |
| `relay/channel/claude/relay-claude.go:831 ResponseClaude2OpenAI` + `:836 ResponseClaude2OpenAIWithError` | — | **`relayconvert.ResponseClaude2OpenAI`（`response_compat.go:100`，公开同名）**；上游 facade 在 `upstream/main:relay/channel/claude/relay-claude.go:41`。**WithError 变体上游已删**——错误路径须经包装层（12.20）或按 12.11 的裁定处理 | 生产：`relay-claude.go:1273`、`relay/channel/aws/converse.go:696`；测试：`relay_claude_test.go:465,511`（非 WithError） |
| `relay/channel/gemini/relay-gemini.go:1276/1281 responseGeminiChat2OpenAI{,WithError}` | — | **`relayconvert.ResponseGeminiChat2OpenAI`（`response_compat.go:140`）**；上游 facade 在 `upstream/main:relay/channel/gemini/relay-gemini.go:139`（把 `c *gin.Context` 适配成 `id`/`created` 两个参数）。WithError 变体上游已删 | 生产：`relay-gemini.go:1768`；测试：`gemini/protocol_conversion_test.go:38` |
| `relay/channel/gemini/relay-gemini.go:1381/1430 streamResponseGeminiChat2OpenAI{,WithError}` | — | **`relayconvert.StreamResponseGeminiChat2OpenAI`（`response_compat.go:144`）**；上游 facade 在 `upstream/main:relay/channel/gemini/relay-gemini.go:143`。WithError 变体上游已删 | 生产：`relay-gemini.go:1605`；测试：`gemini/protocol_conversion_test.go:50,329,330`（非 WithError） |
| **`relay/channel/gemini/relay-gemini.go:201 CovertOpenAI2Gemini`（第 48 轮补入——被上条口径漏掉）** | 568 | **`relayconvert.OpenAIChatRequestToGeminiGenerateContent`（`internal/oai_chat/to_gemini_chat_req.go:19`，internal，宿主经 `service.ConvertRequest(c, info, types.RelayFormatGemini, request)` 到达）**——上游**已完全删除** `CovertOpenAI2Gemini` 这个名字（`git grep -c CovertOpenAI2Gemini upstream/main -- '*.go'` = 0）。**阶段 6 判据按函数名核：本函数属「真删」（与 `RequestOpenAI2ClaudeMessage` 同型，不能保 facade）** | 生产：`relay/channel/gemini/adaptor.go:186`、`relay/channel/vertex/adaptor.go:311`（上游 `upstream/main:relay/channel/gemini/adaptor.go:174`、`upstream/main:relay/channel/vertex/adaptor.go:294` 均改为 `service.ConvertRequest(c, info, types.RelayFormatGemini, request)` 再断言 `result.Value.(*dto.GeminiChatRequest)`）；测试：`gemini/protocol_conversion_test.go` 11 处（`:82,102,128,157,187,206,225,240,258,279,298`） |

**第 6 行（`CovertOpenAI2Gemini`）的严格性差异见 9.2.4**：它是 oai→gemini **请求**方向的转换器，本地 23 个 `fmt.Errorf` 错误点（基线 `:264` 的 stop 超过 5 条报错、`:406` tools type、`:410` 函数名必填、`:413` 重名、`:445` parameters 必须为 JSON object、`:503`/`:534` part type、`:574` role、`:604`/`:608` tool_calls type 与函数名、`:711` 无可用文件源、`:720` mime type 等），而 relaykit 的替代者 `OpenAIChatRequestToGeminiGenerateContent`（379 行）只保留 `extra_body`/`image_config` 一族与 mime type 校验，**其余全部静默化**（stop 超过 5 条改为截断、tools 无类型/重名/JSON-object 校验、role 原样透传、tool_calls 只校验 args 是否可 unmarshal）。差异逐条见 **9.2.5**（oai→gemini 请求方向）；处置归**处置清单第 18 项**（Gemini 两个请求方向的入口转换器严格性，与 9.2.4 合为 20 个裁定点 + 1 行无差异确认），**不并入第 9 项**——第 9 项只覆盖 9.1 第 8 条的 oai→gemini 响应方向 finish_reason 单字段语义。

**为什么这是缺口**：§8 只列了 6 条**出向**（上游响应 → `dto.ClaudeResponse`）路径，把 `RequestOpenAI2ClaudeMessage` 这条 **459 行的入向转换**整条漏掉了；而它同时服务 3 个通道（claude、aws、vertex），是本地最大的单个转换函数，仅次于 `service/convert.go` 的 2229 行。阶段 6 只删 `service/convert.go`，**不删这些通道级转换器**，于是迁移后会存在「relaykit 转换器 + 本地 459 行转换器」两套并行实现——正是 §12.15 竭力避免的「双实现」。

**本地独有文件 `relay/channel/aws/converse.go`（第 37 轮补入盘点）**：1206 行（基线口径；HEAD 1217），承载 AWS Bedrock Converse API 的双向转换（`convertClaudeToConverseInput:106`、`convertOpenAIToConverseInput:141`、`converseOutputToClaudeResponse:713` 等 **32 个顶层函数**（基线口径；HEAD 33）——第 38 轮修正，初稿的「33」是 HEAD 口径混入）。**上游从未有此文件**——`git log upstream/main -- relay/channel/aws/converse.go` 为空，上游 `relay/channel/aws/` 只有 `adaptor.go`/`constants.go`/`dto.go`/`relay-aws.go`/`relay_aws_test.go`，且全上游 `Converse` 标识符只出现在 `upstream/main:relay/channel/aws/adaptor.go:103` 的一个 URL 字符串里（上游 ApiKey 模式直接委托 `claude.Adaptor.DoResponse`）。**处置：整文件保留**（12.4.3 已定其 thinking 暴露不受门控），但它与本次迁移有 3 个交互点，须在阶段 4/6 认领：`:113` 调 `service.ClaudeToOpenAIRequest`（同名 shim 保留，零改动）；`:696` 调 `claude.ResponseClaude2OpenAIWithError`（**该函数上游已删**，是通道级 WithError 的 5 个生产调用点之一，见 12.20.1）；`:412` 调 `service.GetBase64Data`（媒体解析，随 MediaResolver 接线不变）。**此前任何盘点（§4.5 各小节、12.20.1 的 C 表）都未出现该文件**——本段是其首次盘点，但**不是唯一落点**：12.20.1 的 C 表已补入 `aws/converse.go:696`（第 37 轮），4.5.5 末段与 §12.11.3 亦各有一处（**第 38 轮修正**：原文写「唯一落点」，与 12.20.1 的 C 表冲突）。**第 39 轮修正**：末句「§12.11.3 亦各有一处」不成立——§12.11.3 小节全文无 `converse`／`aws`／`:696` 命中；`converse.go` 的落点除本段外是 4.5.5 的表格行（`ResponseClaude2OpenAI` 行的「生产」列）、12.15.4 第 4 点与 12.20.1 的生产侧表。

**本地专有能力盘点**（决定它能否被 relaykit 版替换）：该函数用到 `model_setting.GetClaudeSettings()`、`model_setting.ShouldPreserveThinkingSuffix()`、`buildClaudeMediaMessageFromFile/Source`、`normalizeClaudeInputSchema`——**relaykit 的 `to_claude_messages_req.go` 与 `shared/claude` 均不引用任何宿主 `model_setting`**。但这不是「不能替换」——relaykit 侧有对应的**注入机制**（`convmeta.Options` 钩子、`MediaResolver`、内置 schema 归一化）。**处置决定见 12.15.4：收敛到 relaykit，宿主只留钩子接线。**

## 5. 必须保留的两项能力

用户明确要求保留「思考等级」与「thinking 历史回传」。这两项**都是请求方向**，映射如下：

| 需求 | 方向 | 现状 | 收敛后承载 |
|---|---|---|---|
| 思考等级 | 请求 | `relay/claude_handler.go:55-104` 手写后缀适配 + `request.Thinking`/`OutputConfig` 组装 | `reasoning.RenderClaude` + suffix 解析（上游实现更完整，见 3.1）。**注意承载链（第 34 轮）**：relaykit 不解释模型名尾缀，后缀意图必须由宿主入口写入 `info.ReasoningConversion` 再经 `ReasoningState()` 传入——**第 44 轮定案为 A：移植上游填充器（12.19），换装 `upstream/main:relay/claude_handler.go:41` + `upstream/main:relay/channel/claude/adaptor.go:39` 随填充器同批在阶段 4 落地** |
| thinking 历史回传 | 请求 | `service/convert.go:103` 起的 `extractThinkTaggedHistory` 分支 + `case "thinking"` 提取 + DeepSeek 空块保留 | 补丁 1 的 `case "thinking"` + think 标签族（剥离迁入 relaykit、门控留宿主，见 12.1）；DeepSeek 空块保留由 `RetainEmptyReasoning` 钩子承载（§4.2） |

**响应方向**的 reasoning 暴露策略是第三个独立关切，与上述两项无关，且**不能**按客户端意图门控——理由见第 6.1 节。

## 6. 已撤回的设计及其理由

保留此节用于避免重蹈覆辙。

### 6.1 撤回「按客户端意图门控 reasoning 暴露」

**结论（2026-10-01 修订）：门控依据是宿主策略而非客户端意图，落点在 relaykit 内部的索引分配之前，不在宿主调用点。**

**⚠ 本节原结论「响应方向不打 relaykit 补丁，策略落在宿主调用点」已被 12.4 推翻。** 原结论依赖「后置过滤可行」这一前提，实测该前提不成立：relaykit 的 `state.Index` 是私有流式状态，宿主删块会产生**索引空洞**，被 Claude 客户端拒绝。详见 12.4.1。

**以下三条论据仍然成立**（它们证明的是「不能按客户端意图门控」，与落点无关）：

1. 依据不成立。「客户端未请求 thinking 就不应返回 thinking」不是协议不变量。relaykit 自身的 `claudeCapabilitiesFor` 为 fable-5、mythos-5、mythos-preview、opus-5、sonnet-5 置 `defaultThinking = true`——上游代码里就有「这些模型默认思考」的证据。
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
| 1 | **取得 relaykit 源码**（见下「阶段 1 的前置」）；引入 relaykit module（根 `go.mod` 加 require + replace）；`types` 拆分：6 个文件迁入 `relaykit/types`，宿主保留 4 个；按符号逐个决定别名转发或改 import 路径；处置 4.3 的本地专有符号；**新建 `relaykit/LOCAL_PATCHES.md` 并登记全部改动清单行（第 44 轮升为阶段 1 交付物——见下「维护要求」段，它回答「我们改了什么」而不是「上游是哪天」，越晚建、漏登越多，且第 44 轮定案后本地分叉已扩到 A1–A6 + 共享助手 + DeepSeek 例外，必须从阶段 1 起随改动逐条登记）** | 两个 module 均可编译；`relaykit/` 的版本已锚定并记录；**`relaykit/LOCAL_PATCHES.md` 已存在且含改动清单全部行（第 46 轮修正：原写「全部 12 行」是静态数字，与「随改动逐条登记」的动态需求不匹配——实际要登记的是 12（表内）+ 3（第 46 轮新增第 13–15 行：共享助手、填充器 A1–A6、`HasChannelMeta`）+ 6（A1–A6 若逐条登记）+ 1（`IsDeepSeekModelName` 保留）≈ 22 行；判据改为「已存在且含改动清单全部行，第 46 轮新增的第 13–15 行已登记」）** |
| 2 | **（最大工作量块）** `dto` 迁移：**327 个 import 点改写**（**基线口径，含测试**：基线 `dbfbc096f` 实测 205 非测试 + 122 测试 = 327；工作区已漂到 341）+ **33 个类型字段差异逐项对齐**（基线口径，工作区 34）+ **同名字段的 JSON tag / Go 类型差异逐项对齐**（12.0.8：11 处（tag 5 + type 6）全部采用 relaykit，改 8 处 `IncludeThoughts` 赋值点 + `Text`/`Arguments` 读写点 + 4 处 import）+ 全部宿主独有方法（全包 394 vs 273 函数，含测试口径）给出出路；**清空 `dto` 对 `common`/`constant`/`logger`/gin 的 import**（否则 `relaykit/relayconvert/boundary_test.go` 红）。**另须改 `service/convert_golden_test.go` 的 import**——它是第 18 轮新增、**不在上述 327 的基线口径内**，却 import 宿主 `dto`/`constant`/`types`，`dto` 一迁即编译失败（见下「阶段 2 的前置：基线测试的 import 不在 327 内」）。开工前置：完整的**方法级差集 + import 图分析**（12.0.2、12.0.3）。另含 `IsStream` 签名迁移、补丁 2（DTO presence） | 两个 module 均可编译；DTO 测试通过；**`boundary_test.go` 通过**；**12.5 的 3 个原「需实测」方法已定案为 A**（第 42 轮；`Usage.EnsureResponsesTokenDetailObjects`、`Usage.GetOutputTokenDetails`、`OpenAIResponsesResponse.HasImageGenerationCall`）——见「阶段 2 的前置」，**带着「需实测」开工即判据不可判**；**327 个 import 点全部验证**；**11 处 tag/类型差异逐项有决定（12.0.8）**；**`convert_golden_test.go` 的 import 已随之迁移且 `TestGolden` 仍绿** |
| 3 | `ClaudeConvertInfo` 改为 `convmeta.ClaudeConvertInfo` 别名，同步改写 `service/convert.go` 的 22 处字段引用；**`RelayInfo` 补齐 `convmeta.Meta` 的缺失方法**——接口共 15 个（已机械核验），本地 `RelayInfo` 现有 26 个方法中**只有 2 个重名**（`GetEstimatePromptTokens`、`AppendRequestConversion`），**须新增 13 个**（第 46 轮复核：接口 15 − 本地已有 2 = 13；原文「13 个」正确）（实现对应上游 `upstream/main:relay/common/relay_info.go` 的 `GetOriginModelName`（`:825`）至 `ConvOptions`（`:922-953`），上游已有编译期断言 `:823`）。**照抄不是纯文本搬运（第 34 轮修正；第 46 轮复核确认）**：13 个里 **11 个是纯访问器**（只读本地已有字段），另 2 个各依赖一个本地没有的字段——`ReasoningState()` 读 `ReasoningConversion`、`ConvOptions()` 读 `convOptions`。**第 46 轮复核（防误报）**：两次用错接收者 grep 模式曾得出「交集 0、须新增 15」，第三次修正模式后实测——接口 15 个方法中本地 `RelayInfo` 已实现 **2 个**（`AppendRequestConversion`、`GetEstimatePromptTokens`），**须新增 13 个**；`HasChannelMeta` **在这 13 个之内**（非新增的第 14 个）。故原文「13 个」正确，第 46 轮子代理「漏了 `HasChannelMeta`、应为 14」是误报——`ReasoningState()` 读 `ReasoningConversion`（上游声明在 `:123`）、`ConvOptions()` 读 `convOptions`（`:201-202`；另 import `model_setting.GetClaudeSettings/GetGeminiSettings`，本地已存在：`setting/model_setting/claude.go:43`、`setting/model_setting/gemini.go:53`）；**且本地 `InitChannelMeta` 须补上游 `:311` 的重置行 `info.convOptions = nil`**（见 12.10）。**同一范围内的 `GetBillingModelName`（`:832`）不照抄**——它不在 `convmeta.Meta` 接口里，本地无 `BillingModelName` 字段也无写入方（上游写入方在 `relay/helper/price.go:73-77`，不在本迁移范围），relaykit 侧零调用 | 编译期断言 `var _ convmeta.Meta = (*RelayInfo)(nil)` 通过；**13 个方法逐个落地**（非「实现 15 个」，也不含 `GetBillingModelName`；**第 46 轮复核：13 = 接口 15 − 本地已有 2**）；`ReasoningConversion`、`convOptions` 两个字段已声明；**本地 `InitChannelMeta` 置 `info.convOptions = nil`——换渠道重试后 `ConvOptions()` 反映新渠道**（`OpenRouterDialect`/`ToolLossPolicy` 不得读到上一渠道的值）；`ConvOptions` 的缓存语义按上游（首次调用建快照、同一 relay 会话复用） |
| 4 | 协议映射切换到 relaykit（**单点可逆**，见 §8.1）；**接线 `relayconvert.SetMediaResolver`（指向 `service.GetBase64Data`）与 `convmeta.Options` 钩子（指向 `model_setting`）**，随后通道级转换器改调 relaykit（12.15.4）；打补丁 1；**打补丁 3 并落地 12.4 的响应方向门控**（`convmeta.Options` 新增 `RetainReasoningInClaudeResponse` + 宿主接线 + `oai_chat/to_claude_messages_resp.go` 两条入口加判断，处置清单 13）——**门控必须与本阶段的切换同时落地**：relaykit 默认无条件暴露 reasoning，切换而不门控即等于把非 DeepSeek 的 reasoning 泄漏给客户端（见 12.4.4）；**按下方「阶段 4 的处置清单」逐项落地**；**`*WithError` 系列随本阶段切换一并删除**（12.3；**第 44 轮随未决项 33 定案 C**：不留转发函数、不等阶段 6，第 14 项 A 形态不再适用；同时按 C1 把 4 个非 WithError 旧包装**保留为 facade**）；**把 `convert_golden_test.go` 的调用点改指 relaykit**（处置清单 12）；**落地 finish_reason 严格语义**（处置清单 9，**第 44 轮定案为 B**——共享助手前置校验，见 12.11.3；**第 47 轮扩为两个方向**：Claude 方向 5 处 + Gemini 方向 4 处，见 9.1 第 8 条与 12.11.2b） | **`TestGolden` 改指 relaykit 后 8 个快照逐字节一致，或每一处差异都能对应到处置清单第 6 项为 9.1/9.2/9.2.1/9.2.3（**第 46 轮：9.2.2 已移出第 6 项、独立为第 17 项**）逐条产出的「保留/放弃/改写」决定**（**第 38 轮修正**：原文只写 9.1/9.2，而第 6 项在第 37/38 轮已扩到 9.2.1/9.2.3——其中 9.2.1 第 9 条与 9.2.3 第 8 条**会产生快照可见的输出变化**，其余为错误路径，由专项单测而非 golden 覆盖）（12.16.2；该决定清单是**阶段 4 的产出**，不是既有物——判据引用的是它，不是预先存在的清单）；**请求方向快照的差异豁免范围（第 46 轮补）**：`request/claude_to_openai`、`request/gemini_to_openai` 若因**补丁 1**（第 1 项，加 `case "thinking"`）或**填充器 A**（第 15 项，写 `info.ReasoningConversion` + 裁 `UpstreamModelName`）而变，**其豁免范围 = 处置清单第 1 项与第 15 项的产出决定，第 6 项不覆盖请求方向**——原判据只认第 6 项（覆盖 9.1/9.2/9.2.1/9.2.3；9.2.2 归第 17 项），请求方向的差异既不在第 6 项、又不在「4 个门控快照」的豁免集合里，**判据无法裁决此类差异是回归还是预期**。**另注前提（第 46 轮补）**：12.16.3 的两条「待阶段 4 实测的边界」（`EnsureClaudeConvertInfo()` 返回状态初始值是否等价于本地 `goldenRelayInfo` 手工设的 `ClaudeConvertInfo{LastMessagesType: LastMessageTypeNone}`；relaykit 侧签名未经编译验证）**未实测前本条不可判**。**4 个门控相关快照（`response/openai_to_claude`、`stream/openai_to_claude`、`response/openai_to_claude_deepseek`、`stream/openai_to_claude_deepseek`）不适用上述豁免**——第 6 项覆盖 9.1/9.2/9.2.1/9.2.3（第 37/38 轮扩；9.2.2 归第 17 项），门控（12.4）不在其中，它们逐字节一致**直接证明门控已随切换落地**；grok reasoning 可见；处置清单每项已确认；**切换形态已按处置清单第 14 项定案为 C（第 44 轮）并可验证**（按 12.20 落点逐项核，回滚＝删 4 个新文件 + revert 5 个既有文件 + revert 生产改写 18 处 + revert 测试侧 50 处（**第 49 轮订正**：C7 的 `log_info_generate.go` 与 C4 的 `relay_info.go` 是既有文件，见 12.20.1 末段）；A 薄封装与 B 直接改写的判据分支不再适用）——**未定案则「单点可逆」不可验证**（§8.1，现已定案）；**12.15.4 的两条边界已实测且均不成立**（12.15.4.1/12.15.4.2：schema 归一化 4 处差异、媒体分派 7 处差异）——须按处置清单第 10 项连同上游实现一并改，不能只接线；**finish_reason 严格语义的 b/c/d 三条有新测试**（12.11.3——`TestResponseOpenAI2ClaudeRejectsUnknownFinishReason` 只覆盖未知值一条，两条 golden 基线都测不到其余三条）；**Gemini 方向的未知值报错有新测试**（第 47 轮补——`openAIFinishReasonToGemini` 全仓零测试，且 8 个 golden 快照的 `finish_reason` 只有 `tool_calls` 一个非空值、恰好落在「两侧同值」那一格，见 12.16.1 第 47 轮补）；**快照差异先排除「字段顺序」再谈回归**——`ClaudeMediaMessage`（`Signature`/`Data`）与 `FunctionRequest`（`Strict`/`Arguments`）两侧声明顺序不同（12.16.2，第 31 轮实测），本基线因那两对字段在快照里恰好都为空而未受影响，但改动 fixture 使其非空即会纯因序列化顺序而红；**思考等级后缀填充器已接线**（处置清单第 15 项、12.19）——带后缀的跨协议请求（oai→claude 至少一条）渲染出正确 thinking 与裁剪后的上游模型名，且不带后缀的请求与现状逐字节一致 |
| 5 | 宿主管道接入 relaykit 分派（**第 37 轮修正阶段归属**：本行只做**核验**，不落地——包装层 `request_converter.go`/`response_converter.go`、`RelayInfo.RecordConversionDiagnostics`、流式 `ResponseStreamState`（上游 6 处构造点）属**阶段 4**（处置清单第 16 项、12.20.1，随未决项 33 定案；它们是阶段 4 切换的组成部分：门控钩子依赖阶段 3 的 `convOptions` 接线、diagnostics 与流式状态在切换时一并接上）。第 35 轮补注把这些**落地**动作写进本行、与 12.20.1 的「阶段 4 一次落地」冲突，第 37 轮 Codex 复核抓出后归位到 12.20；本行保留 `matchAnyRegex` 文档化与门控位置核验两项**；**`matchAnyRegex` 空模式语义显式文档化**（12.8，采用本地语义「空 patterns = 匹配全部」）。**响应方向暴露门控已在阶段 4 落地**（12.4.4 已说明为何不能留到本阶段），本阶段只需**核验其位置语义**：门控必须在**索引分配之前**（`stopOpenBlocksAndAdvance()` 推进 `state.Index` 之前），后置过滤不可行——见 12.4.1 | 全量测试通过；空 patterns 语义已写入配置文档；**门控位置已核验**：`stream/openai_to_claude` 快照（非 DeepSeek + 输入含 `reasoning_content`）的输出为 text 在 index 0、tool 在 index 1——若门控后置于索引分配，reasoning 会先占 index 0、text 顺移到 index 1，该快照必红（12.4.4） |
| 6 | 删除旧代码（**第 44 轮随未决项 33 定案 C 后的动作面已收窄，本句为 C 形态下的实际动作**）：`service/convert.go` **不整体删除**——C1 已在阶段 4 把它改写为 27 行 facade（含 `NormalizeCacheCreationSplit` + 4 个非 WithError 包装），随阶段 4 落地的还有共享助手（处置清单第 9 项）与被删的 4 个 `*WithError` 函数体（C1 改写时一并消失），**本阶段对 `service/convert.go` 只剩「确认无残留主体函数体」**；真正删除的本地实现是 4.5.5 表列的 10 个通道级转换器中的 6 个真删部分（`RequestOpenAI2ClaudeMessage` 459 行主体、`CovertOpenAI2Gemini` 568 行主体、4 个 `*WithError`）与 4 个通道级转换器文件里被 facade 化后的旧函数体（第 48 轮把 `CovertOpenAI2Gemini` 列入真删：上游已删除该名字）（12.15.4 已定收敛到 relaykit，本阶段删除本地实现主体）。**第 46 轮修正 `*WithError` 变体的删除时点**：4 个通道级 `*WithError` 变体的**删除**发生在阶段 4（随 C6 的 facade 化一并完成），本阶段删的是**本地实现主体**（`RequestOpenAI2ClaudeMessage` 459 行与 facade 化后的旧函数体）——原表把「`relay-gemini.go` 两侧的 `*WithError` 变体」也写成本阶段删除，与阶段 4 的 C6 重复。**`service/openaicompat/` 不删**——12.15.3 已决定该包整体保留、行数不减少，它与 relaykit 的基础转换重复但不可切分（12.15.1）。**`relay/channel/aws/converse.go` 不删**（本地独有、上游零覆盖，见 4.5.5 末段），仅其 `:696` 的 `ResponseClaude2OpenAIWithError` 调用须改指——**第 44 轮定案 C 后已由处置清单第 16 项归入阶段 4 落地**（**第 46 轮修正：删除原「形态 A 下随本阶段删封装一并改指，形态 B 下的归属随未决项 33 定案明确」的分支描述——第 44 轮已定案 C，该分支不再适用**）。**`convert_golden_test.go` 与 `service/testdata/golden/` 不得随主体一起删除**：阶段 4 已把它改指 relaykit，本阶段须把**本地独有部分**（DeepSeek 门控 fixture 与两条 `*_deepseek` 用例）并入上游已有的 `relaykit/relayconvert/golden_test.go`，复用其 helper，**不得整体移入**（同包同名符号会编译失败，见 12.16.3）。**终态形态见 12.20 的上游实测（第 35 轮）**——上游保留 `service/convert.go`（27 行 facade）与请求/响应包装层，并保留通道级同名 facade；**第 44 轮随未决项 33 定案为 C，本行动作与判据按 C 执行**（下述「形态 C 下」判据） | **`service/convert.go` 无残留「非 facade 主体函数体」**（第 46 轮修正范围：原写「无残留引用」未界定——6 个真删名中 4 个 `*WithError` 的生产调用点已在阶段 4 改指，真正随本阶段删除只剩函数定义与残存测试引用；**判据须写死为：6 个真删名（`RequestOpenAI2ClaudeMessage` + `CovertOpenAI2Gemini` + 4 个 `*WithError`）全仓 `git grep` 零命中；4 个 facade 名逐文件读函数体确认只有一行 `return relayconvert.Xxx(...)`**）；`service/openaicompat/` 的 3 个消费者（4.5.1）行为不变；**4.5.5 表列的 10 个通道级转换器逐个核到终态**（**第 38 轮修正：本判据此前只写「全部删除」，与 12.20.1 的 C6 直接矛盾**——C6 要求 4 个非 WithError 函数**保留为一行 facade**）：**第 44 轮定案 C，判据取 C 分支**：6 个真删（`RequestOpenAI2ClaudeMessage` + `CovertOpenAI2Gemini` + 4 个 `*WithError`，第 48 轮补入 `CovertOpenAI2Gemini`）、4 个保为 facade（`StreamResponseClaude2OpenAI`、`ResponseClaude2OpenAI`、`responseGeminiChat2OpenAI`、`streamResponseGeminiChat2OpenAI`）且各自只有一行 `return relayconvert.Xxx(...)`（**「9 个全部删除」的 A/B 形态分支不再适用**，见 4.5.5 表头注、12.20.1 C6）；**golden 基线已在 `relaykit/` 下运行且快照与阶段 4 末态一致** |

四处硬依赖：

- `types` 是 `dto` 的前置（relaykit 的 DTO 必须 import `relaykit/types`）；本地 `types/error.go` import 宿主 `common`，上游改 import `kitutil`，所以 `types` 迁移牵动 `common → kitutil` 替换
- **`dto` 迁入 `relaykit/dto` 后不得再 import 任何宿主包或 gin——这是上游 `relaykit/relayconvert/boundary_test.go` 会断言失败的硬约束**（见下「阶段 2 的前置：`boundary_test.go` 是阶段 2 的硬门禁」）。`types` 那一处 `common → kitutil` 只是该约束在 `types` 上的体现；`dto` 自身还有 12 个 `common`、2 个 `constant`、1 个 `logger` import 必须同批清除，其中 3 个符号 `kitutil` 未提供。**这必须列入阶段 2 的动作，不能等测试红了再补。**
- `ClaudeConvertInfo` 别名是 `Meta` 实现的前置。本地字段（`ToolCallStarted map[int]bool`、`ToolCallIDs`、`ToolCallNames`、`ToolCallPendingArgs`）与 `convmeta.ClaudeConvertInfo`（`ToolCalls []*ClaudeStreamToolCall`、`ToolCallByIndex`、`ToolCallByID`）**字段名与类型都不同**，`service/convert.go` 引用本地字段 22 处（6+5+5+6），改别名后全部编译失败
- `dto.Request.IsStream` 签名迁移**不可机械替换**：本地 `IsStream(c *gin.Context)`，relaykit `IsStream(c *http.Request)`；`dto/gemini.go:112` 用了 `c.Query("alt")`，`*http.Request` 无此方法。影响面见下「阶段 2 的前置：`IsStream` 迁移的实际范围」

**阶段 4 的处置清单**（早期版本写「对齐第 4 节的处置结论」，但第 4 节是**能力盘点**、按归属分类，不含动作与阶段；此处收拢散在 §12 各节的可执行项，供阶段 4 逐项勾选）：

**执行次序（第 44 轮补，第 46 轮修正倒置；**第 50 轮把「17 项」订正为「18 项」**——第 46 轮写「17」时清单尚未含第 17 项之外的第 18 项，第 48 轮新增第 18 项后本句未同步；现表实为 18 项）**：下列 **18 项**按依赖分成四批，**同批内可并行、跨批必须先后**——批 0 是「阶段 2 的活」（第 4、11 项，**不属于阶段 4**，此处列出仅为厘清它们不在本清单的动作面内）；批 1 是「骨架」（先立类型/字段/包装层，否则后续无法编译）；批 2 是「注入与切换」（补丁、钩子接线与调用点改指，**必须同批**）；批 3 是「判定」（逐条裁定语义差异，需要批 2 的产物可跑）。**第 50 轮补——批次与项号的对应关系**：四批共引用 17 个项号（批 0: 4,11；批 1: 9,16；批 2: 1,2,9,10,12,13,15,16,17,18；批 3: 3,5,6,7,8,18），**未进任何批次的是第 14 项**——它是「阶段 4 切换形态」的**决策项**（第 44 轮定案 C），不含独立编辑动作，其动作面已折进第 16 项的 C1–C6，故不在批次表内出现是正确的、不是遗漏。

**第 46 轮修正的三条倒置**（原表有真实顺序错误，不是「颠倒即编译报错」那么安全）：

1. **第 4、11 项不是阶段 4 的工作**——第 11 项 `EffectiveTools()` 迁入在 12.0.7（`relay/common` 一节）明写「**阶段 2 的动作**」，改动清单第 7 行阶段列也是「2」；第 4 项 `ParseContentStrict`/`ParseToolCallsStrict` 判为 A 类（12.5），随阶段 2 的 dto 整包迁移落地。原表把它们放进阶段 4 的批 1，比真实需要晚两个阶段——**dto 整包在阶段 2 迁入后，阶段 2 的 327 个 import 点改写必须能找到这两个方法，否则阶段 2 构建即失败**。故第 46 轮把它们移出阶段 4 清单。
2. **C1 不能先于门控（第 13 项）**——C1 把 `service/convert.go` 的 facade 函数体改写成 `return relayconvert.Xxx(...)`，**这本身就是「协议映射切换到 relaykit」**；而 relaykit 默认无条件暴露 reasoning（12.4.4 第 1 点：「阶段 4 一旦切换而不门控，非 DeepSeek 快照必然多出 thinking 块」）。故 C1 与第 13 项门控**必须同批**，否则非 DeepSeek 的 reasoning 在门控落地前**泄漏给客户端**——这是真实的对外行为泄漏，不是「编译期暴露」。原表把 C1（批 1）与门控（批 2）分列，且第 415 行末句「不会静默」的断言**对这条不成立**，第 46 轮删除该断言。
3. **第 9 项（共享助手）的迁移源是 C1 之前的 `service/convert.go`**——第 9 项要把 `stopReasonOpenAI2ClaudeStrict` 从 `service/convert.go` 迁入宿主共享助手，而 C1 会把该文件的 5 个函数体改写为 facade（**该函数不在上游 27 行的 5 个 facade 里**，会被删）。若 C1 先于第 9 项执行，迁移源已不存在，只能凭 git 历史手工重写——**这不产生编译错误，若重写有细微偏差即静默丢语义**（该函数含 `""`/`TrimSpace`/`function_call` 三条分支）。故第 9 项与 C1 **同批**，且第 9 项的迁移源钉死为「C1 之前的 `service/convert.go:1125`」。

| 批次 | 项 | 依赖说明 |
|---|---|---|
| **批 0 阶段 2 的活（不在阶段 4 动作面）** | 第 4、11 项 | 见上第 1 条。**本清单其余批次不引用这两项** |
| 批 1 骨架 | 第 16 项的 C3/C4 + 第 9 项（仅迁出） | C3（`response_converter.go`）是**纯新增文件**；**C4 是「新增 `conversion_diagnostics.go` + 改既有 `relay/common/relay_info.go` 补 3 个字段」两半**（第 49 轮订正：原写「C4 是纯新增文件」不成立——`relay_info.go` 基线存在，且不加这 3 个未导出字段 C4 照抄即编译失败，见 C4 行）。两者都不依赖后续项，可最先做。**第 50 轮补——批 1 内 C4 先于 C3**：C3（`response_converter.go`）的 4 个包装体都调 `info.RecordConversionDiagnostics`，该方法与 3 个未导出字段由 C4 提供——故批 1 内须 **C4（含 `relay_info.go` 3 字段 + `conversion_diagnostics.go`）先落地、C3 后落地**，否则 C3 单独先写即 `info.RecordConversionDiagnostics undefined`。批 2 的 C2（同样调 `RecordConversionDiagnostics`）依赖 C4 先于它，因 C4 在批 1、C2 在批 2，天然满足。**第 49 轮两处订正**：①**C2（`request_converter.go`）移出本批、归批 2**——原写「C2/C3/C4 是纯新增文件」不成立：基线 `service/convert.go:18`/`:1248` 与 C2 的 2 个 shim **同包（`package service`）同名**，C2 单独落地而 C1 未删旧声明即 `ClaudeToOpenAIRequest redeclared` 编译失败，故 C2 必须与 C1 同批（证据见处置清单第 18 项第 1 条与 C2 行）；②**第 9 项本行只管「迁出」**——原写「第 9 项与 C1 同批（执行次序表批 1）」而批 2 项列不含第 9 项：第 9 项的迁出（共享助手进批 1，见上第 3 条）与接入（9 个调用点前置校验随 C1 批 2 生效）是两半，批 1 只做迁出。**第 47 轮补**：同一批还须迁出 `openAIFinishReasonToGemini`（9.1 第 8 条）——它同样不在 5 个 facade 里，且 C1 会删掉整个非 facade 主体；两个函数迁入**同一个**共享助手文件，避免出现两个同职责文件 |
| 批 2 注入与切换（**必须同批**） | 第 1、2、10、12、13、15、16、18 项的 C1/C2/C5/C6/**C7/C8**、9（接入）、17 项 | 这一批是**一个原子动作**：C1 改写 facade（**含删 `convert.go` 内 `ClaudeToOpenAIRequest`/`GeminiToOpenAIRequest` 两个旧实现声明**——第 49 轮补，否则 C2 新增的同名 shim 冲突）+ C2 新增 `request_converter.go`（**第 49 轮移入**：与 C1 必须同批，见批 1 依据）+ 第 9 项的接入（**第 49 轮补**：迁出在批 1、9 个调用点的前置校验随 C1 同批生效）+ 第 18 项的落点（**第 49 轮补**：9.2.4 的 shim 前置校验与 9.2.5 的独立 guard 均须在切换同批就位）+ C5 流式状态构造（**第 49 轮指针**：上游 6 处构造点里 2 处的本地文件 `relay/channel/{claude,gemini}/relay_responses.go` 基线不存在、本地该路径显式不支持——**纳入/排除须在本批开工前裁定**，见 C5 行）+ C6 通道级 facade 与 adaptor 改指（第 16 项）+ 第 13 项门控 + 第 10 项 `SetMediaResolver`/`convmeta.Options` 钩子 + 第 1 项补丁 1 + 第 2 项删 `*WithError` + 第 12 项改指 golden + 第 15 项填充器与换装 + 第 17 项反方向共享助手 + **C7/C8（第 50 轮补入本批——此前整张批次表只列 C1–C6，C7/C8 无归属）**：C8（`relay/convert_request_error.go`）的消费者全在批 2（第 15 项填充器与 C6 改写的 handler 路径），且它调 `info.RecordConversionDiagnostics` 依赖 C4（批 1）先落地；C7（`service/log_info_generate.go` 的诊断落盘）同样消费 C4 的 `ConversionDiagnostics()`，故二者随批 2 落地最稳（批 1 结束后 C4 已在，C7/C8 不早于 C4 即可；此处与其它批 2 项一次落地，避免 C4 已落、C7/C8 未落时诊断只写不读的中间态）。**C1 与第 13 项必须同批**（见上第 2 条）；**第 12 项改指 golden 后 golden 立即走 relaykit 路径，故也必须在第 13 项之后**；**第 15 项内部序：A1–A6（移植）+ 填充器接线 → 换装（`claude_handler` + `adaptor.go`），换装依赖填充器先写入 `info.ReasoningConversion`**（12.19）；**第 10 项与 C6 改的是同一批 adaptor 调用点**（`relay/channel/claude/adaptor.go:100` 等三处），须串行合并、不得重复认领。**同批内「可并行」指逻辑无依赖，实际编辑点有重叠（同一个 `convmeta.Options` struct、同一个 `ConvOptions()` 构建函数、同一批 adaptor 调用点），须串行合并** |
| 批 3 判定 | 第 3、5、6、7、8、18 项 | 第 6 项是最大判定项（9.1 的 8（**第 47 轮新增第 8 条**）+ 9.2 的 8 + 9.2.1 的 9 + 9.2.3 的 8 条条件 = **33 条条件**；**9.2.3 第 5 条拆为 5a/5b，故裁定点为 34 个**——全文「条」指条件数）；**第 17 项（9.2.2 的 6 条）已在批 2 落地形态、其 a–e 裁定随本批一并做**（见第 17 项与 9.2.2）；**第 48 轮补：第 18 项（9.2.4 的 10 条 + 9.2.5 的 11 条，其中 1 行为无差异确认 = 20 个裁定点）同样在批 3 判定**——它是 Gemini 两个请求方向的入口转换器严格性，与第 17 项同型（独立于第 6 项，不撑大 33 条口径）；第 3/5/7/8 项是确认或搬迁核对，可在批 2 产物上并行核。**注意第 6 项的裁定同时是批 1/批 2 测试改写的前置**：通道级 6 处（`relay_claude_test.go:689,697`、`tool_call_id_test.go` 4 处）钉的是被 relaykit 丢弃的本地严格性，其处置随第 6 项对应行裁定——批 1/批 2 的测试改写须等对应行裁定（详见第 16 项与 9.2.1/9.2.3） |

| # | 动作 | 依据 |
|---|---|---|
| 1 | 打补丁 1：两个请求转换器的 content block switch 增加 `case "thinking"` 与非 assistant role 门控，并补 `default:` 显式报错 | 3.3 |
| 2 | **`*WithError` ×4 删除**（与上游一致，**删除时机已随未决项 33 第 44 轮定案为 C：随阶段 4 切换一并删**——A 形态（先以同名转发函数存活至阶段 6）不再适用，见第 14 项）；**4 个无 error 旧包装已定案：保留为 facade**（阶段 4 把 5 个函数体改写为转发 `relayconvert.*`，调用点零改动），见 12.20、12.20.1 的 C1。原文本「与 4 个无 error 旧包装一并删除」已按 12.3（第 35 轮修正）撤回 | 12.3、12.20 |
| 3 | 4 个本地专有 `ErrorCode` 与 2 个判定函数：搬迁动作在**阶段 1**（随 `types` 拆分），阶段 4 只做**确认**——宿主侧无残留引用 | 12.2、4.3 |
| 4 | **（阶段 2 动作，非阶段 4——第 46 轮归位）** `ParseContentStrict`（`dto/openai_request.go:882`）/ `ParseToolCallsStrict`（`:624`）迁入 `relaykit/dto`。**上游 relaykit/dto 无同名方法**（实测 `git grep` 空）——这是**本地专有的反向新增**，不是照抄。**两个方法只用 `common.{GetJsonType,Marshal,Unmarshal,RawMessage}` + stdlib**，其中前三个 `kitutil` 有等价物（`kitutil/json.go` 的 `GetJsonType`/`Marshal`/`Unmarshal`），但 **`kitutil` 不提供 `RawMessage` 类型别名**（本地 `common.RawMessage = json.RawMessage`，`common/json.go:9`；`kitutil/json.go` 里 `RawMessage` 只作为 `json.RawMessage` 的参数类型出现）。故 `ParseContentStrict` 里的 `[]common.RawMessage` 须改为 `[]json.RawMessage`。**不得 import 宿主 `common`**（`boundary_test.go` 会红） | 12.1、12.5（A 类） |
| 5 | think 标签剥离族迁入 relaykit（纯函数），门控留宿主。**（第 46 轮归位：本项是搬迁+接线、不是裁定，应归批 2 注入而非批 3 判定——原表把它放进批 4 判定）****具体拆分（实测，HEAD 口径）**：纯函数是 `splitThinkTaggedText`（`service/convert.go:554`）、`indexASCIIFold`（`:597`）、`appendVisibleThinkText`（`:629`），**只依赖 `strings`**，可无改动迁入 relaykit；门控是 `shouldExtractThinkTaggedClaudeHistory`（`:489`）与 `shouldExposeReasoningAsClaudeThinking`（`:526`），**依赖宿主 `*relaycommon.RelayInfo`**，须留宿主。**但门控的 3 个调用点全在 `service/convert.go` 内**（`:103`、`:749`、`:1049`）——**阶段 6 删该文件主体，调用点随之消失**。故「门控留宿主」必须落到**注入机制**上（阶段 4 接线）：请求方向的历史回传门控走 `convmeta.Options` 钩子、响应方向的 reasoning 暴露门控走 12.4 的 relaykit 内门控。**本节此前只写「门控留宿主」，没说门控函数放哪个文件、由谁调用、经哪条钩子**——阶段 4 开工前须定（与第 6.1 节「不能按客户端意图门控」的结论配套） | 12.1、4.3、12.4、6.1 |
| 6 | 逐项核对 9.1 的 8 条语义差异与 9.2 的 8 条输入校验差异，每项确认「保留 / 放弃 / 改写」（9.1 第 7 条为第 27 轮新增：同 chunk reasoning+text 的能力差异；**第 47 轮新增第 8 条：oai→gemini 的 finish_reason 严格语义**；9.2 第 7、8 条为第 28 轮新增：schema 归一化与媒体分派）。**第 37 轮追加**：9.2.1 的 9 条通道级响应错误路径（含 1 条 panic、1 条 `signature_delta` 输出变化），同法逐条裁定（**第 46 轮修正：9.2.2 的 6 条反方向 stop_reason 差异已移出本项、独立为处置清单第 17 项**——原表同时把 9.2.2 写进第 6 项与第 17 项，两处矛盾，且并存时会重复计数 40+6=46。第 17 项与第 9 项同构、一正一反两个方向）；**9.2.3（第 38 轮追加）**：Gemini 方向另有 8 条同类差异（**第 38 轮：第 5 条已拆为 5a/5b，裁定点 9 个**；含 1 条 nil 解引用 panic、未知 `finishReason` 被静默改写成 `content_filter`、**合成 tool call ID 由确定性变随机 UUID**——后者被 `relay/channel/gemini/protocol_conversion_test.go:318` 钉住），同法裁定。**两条 panic（9.2.1 第 5 条、9.2.3 第 1 条）优先**；**两条 panic 的裁定点是 relaykit 内部守卫**（`to_oai_chat_resp.go` 的 nil response 与 `PartialJson` 解引用）——选「保留」即产生新本地改动清单行（阶段 1 起登记），选「放弃」须显式记录为已知 panic 风险，**两者均不得默认** | 9.1、9.2、9.2.1、9.2.3 |
| 7 | 确认 hosted 工具改用上游 `toolconv`（本地 3 种子集，差集为空） | 12.17 #7、3.1 |
| 8 | custom tool bridge 保持本地 fail-closed（默认拒绝、显式开启） | 12.7 |
| 9 | finish_reason 严格语义：**第 44 轮定案为 B**（原三选一 A1/A2/B，第 29 轮重写、第 37 轮修正——原「选项 A」的布尔钩子无法产生 error，见 12.11.3）——**B**：把 `stopReasonOpenAI2ClaudeStrict`（基线 `service/convert.go:1125`，覆盖 `""`/`TrimSpace`/`function_call` 三条）从被删主体**迁入宿主共享助手**（**第 46 轮钉死迁移源与时机**：该函数不在上游 27 行的 5 个 facade 里，C1 改写 `service/convert.go` 时会连同其余非 facade 函数一起消失——故**本项的迁出部分必须在 C1 改写 facade 之前执行**（执行次序表批 1）；**第 49 轮订正**：原写「与 C1 同批（执行次序表批 1）」但接入落在批 2（见批 2 项列）——迁出在批 1、接入随 C1 在批 2，一项跨两批已标注；迁移源钉死为「C1 之前的 `service/convert.go:1125`」），在宿主 5 个 Claude 响应方向的 `*WithError` 调用点（`relay-openai.go:314`、`openai/helper.go:46,246`、`chat_via_responses.go:280`、`gemini/relay-gemini.go:1784`）**调 relaykit 之前**各调一次：先用该函数校验（未知值即 `return error`），再把 `Choices[].FinishReason` 归一到 OpenAI 词汇表（`""`→`stop`、`function_call`→`tool_calls`、`TrimSpace`+`ToLower`），b/c/d 经 relaykit 的 `reasonmap` 映射后与本地严格输出逐字一致。**A1/A2 均已排除**（A1 是 A2 超集、A2 退化为 B，见 12.11.3 第 44 轮）。**不得**沿用「留宿主调用点」的原表述——原调用点全在阶段 6 删除的 `service/convert.go` 内（12.11.1）。**流式侧（`helper.go` 2 处）只校验未知值、不得把空串改写成非空**（末块判定依赖 `FinishReason != nil && *FinishReason != ""`）。须为 `""`/`TrimSpace`/`function_call` 三条各补测试（12.11.3）**第 47 轮补：本项只覆盖 OpenAI→Claude 方向（5 个调用点），而 9.1 第 8 条的 oai→gemini 方向有 4 个同型调用点（`relay-openai.go:324`、`openai/helper.go:63,267`、`chat_via_responses.go:286` 的 `*GeminiWithError`）此前无人认领**——它们同样随第 2 项的 `*WithError` 删除而失去落点，且 `openAIFinishReasonToGemini`（基线 `service/convert.go:1915`）**不在 C1 保留的 5 个 facade 里**，C1 改写 `service/convert.go` 时会被一并删除。**处置形态与本项同构**：把该函数迁入**同一个宿主共享助手**（与 `stopReasonOpenAI2ClaudeStrict` 同批迁出，见执行次序表批 1），在 4 个 Gemini 响应方向调用点调 relaykit 前各调一次——未知值即 `return error`，`""`/`stop`/`tool_calls`/`function_call` 归一到 `STOP`、`length`→`MAX_TOKENS`、`content_filter`→`SAFETY`。**与 Claude 方向的两点差异**：(a) 归一目标词汇表是 Gemini 而非 OpenAI；(b) **不得**做 `""`→`stop` 之类的非空改写——Gemini 侧 `STOP` 是合法终态，空串在本地也映射 `STOP`，故归一表与本地严格函数逐字一致、无「空串改非空」风险。**须为未知值一条补测试**（现状：`openAIFinishReasonToGemini` 全仓零测试——对 `git show dbfbc096f:service/convert.go` 逐行核，仅得定义行 `:1915` 与其**函数体内 2 处调用**（`:1854` 非流式、`:2149` 流式）；上述 4 处是**生产侧调用点**，它们调的是 `*GeminiWithError` 包装、**不直接调本函数**，两个口径不可混算） | 12.11、12.11.1、12.11.3、9.1 第 8 条 |
| 10 | 接线 `relayconvert.SetMediaResolver(service.GetBase64Data 包装)` 与 `convmeta.Options` 钩子（`model_setting` 适配）；通道级转换器改调 relaykit。**第 28 轮已实测两条边界，均不成立**（12.15.4.1/12.15.4.2）：schema 归一化有 4 处差异、媒体分派有 7 处差异（含能力缺失与行为回归）。故本项**不是纯接线**——须把 schema 归一化（改 relaykit 的 `FunctionParametersToInputSchema` 或留宿主 pre-step）与媒体分派（`text/*`→text 块、`ContentTypeFile` 独立分支、不支持的 MIME 报错、`ToFileSource()==nil` 报错）一并处置，否则 9.2 新增的第 7、8 条即为回归 | 12.15.4、12.15.4.1、12.15.4.2 |
| 11 | **（阶段 2 动作，非阶段 4——第 46 轮归位）** `dto` 的 `EffectiveTools()` 及缓存字段迁入 `relaykit/dto`，13 个调用点改 import（12.0.7 明写「阶段 2 的动作」；改动清单第 7 行阶段列亦为「2」） | 12.0.7 |
| 12 | **把 `service/convert_golden_test.go` 的 8 个调用点改指 `relayconvert` 包级入口**（**第 30 轮修正：8 个调用点分布在 6 个不同函数上**——`ClaudeToOpenAIRequest`、`GeminiToOpenAIRequest`、`ResponseOpenAI2ClaudeWithError`×2、`ResponseOpenAI2GeminiWithError`、`StreamResponseOpenAI2ClaudeWithError`×2、`StreamResponseOpenAI2GeminiWithError`；早期版本写「6 个调用点」是把**函数个数**当成了**调用点个数**）（换函数名、`info` 由 `*relaycommon.RelayInfo` 换成 `convmeta.Meta`、删去 `*WithError` 变体），**快照内容保持不变**；随后逐条解释差异。**改指范围随形态**（**第 40 轮修正**：B 下 8 处全改；C 下仅 6 处 `*WithError` 改指、2 处请求方向经同名 shim 零改动，见 12.16.2/12.20.1；A 下阶段 6 再改）。**不得 import `relayconvert/internal/...`**（Go 的 `internal` 规则禁止） | 12.16.2 |
| 13 | **落地 12.4 的响应方向门控（补丁 3）**：`convmeta.Options` 加 `RetainReasoningInClaudeResponse` 字段（12.4.2，**nil = 保留上游默认**）、宿主 `ConvOptions()` 接线（阶段 3 的 `convOptions` 快照里填该钩子，指向 `model_setting` 适配）、`oai_chat/to_claude_messages_resp.go` 流式与非流式两条入口在**进入 `reasoning/text` 二选一之前**归零被抑制的 reasoning。**必须在本阶段完成，不能留到阶段 5**——本阶段的判据要求 4 个门控快照逐字节一致，而 relaykit 默认无条件暴露，不门控即失败（12.4.4）。**验证条件：上游两条依赖默认暴露的测试（`to_claude_messages_resp_test.go:82`、`:125`）仍绿**（本地改动清单第 9 行） | 12.4、12.4.4 |
| 14 | **阶段 4 切换形态：第 44 轮已定案为 C（照抄上游终态）**（原三选一：A 薄封装 6 个同名转发函数 / B 直接改写 8 处调用点 / C 照抄上游终态，§8.1 的「单点可逆」落地）——A、B 不再适用，仅留作选型对照。**C 的动作面**：`service/convert.go` 5 个 facade 改函数体 + 新增 2 个包装文件 + `conversion_diagnostics.go` + 流式 6 处构造 `NewResponseStreamState`（逐项见 12.20.1 的 C1–C6 与处置清单第 16 项）；测试侧 `service/` 4 文件必改 17 处（全为 `*WithError`；`IsDeepSeekModelName` 2 处调用点零改动、函数保留为宿主差异）、通道级 4 文件另必改 33 处（HEAD 41 处中 16×`RequestOpenAI2ClaudeMessage` + 11×`CovertOpenAI2Gemini` + 6×`*WithError`，8 处 facade 零改动；**第 48 轮把 22 → 33**，补 `CovertOpenAI2Gemini` 的 11 处）——C 的测试侧改动面 = 17+33 = 50 处；回滚见 12.20.1 末段（删 4 新文件 + revert 5 个既有文件 + revert 生产改写 18 处 + revert 测试侧 50 处；**第 49 轮订正**：C7 改既有 `log_info_generate.go`、C4 改既有 `relay_info.go`，二者不宜算新文件）。**判据（C 行）**：按 12.20 的落点逐项核（facade 保名、包装层 + `RecordConversionDiagnostics` 就位、82 处测试调用在 C 形态下全部有出路（第 44 轮定案 C 后的真实口径：50 处必改——`service/` 17 处 + 通道级 33 处，其余 65 处零改动，见 12.20.1 的测试侧两张表；「所选形态」措辞已随定案撤回，82 是全删形态下的旧数）） | §8.1、12.3、12.16.2、12.20 |
| 15 | **安装思考等级的后缀填充器（第 34 轮新增，未决项 34；第 44 轮定案为 A）**：宿主入口把模型名后缀解析成 `dto.ReasoningConversionState` 写入 `info.ReasoningConversion`，并把 `UpstreamModelName` 裁成无后缀基础名。上游实现是 `relay/helper/reasoning_suffix.go` 的 `ApplyReasoningModelSuffix`（置 `info.ReasoningConversion` 于 `:71`，非测试调用 6 处、5 个文件），本地**不存在该文件**；**移植上游（A1–A5 五项范围见 12.19 的表）**，本地词表是上游严格子集（无 `-thinking`/budget/`@modifier`、OpenAI 词表缺 `-max`），DeepSeek 富词表保持本地实现不被上游窄词表覆盖（12.19）。**不装即回归**：本阶段换入的 relaykit 转换器在 4 处消费 `ReasoningStateOf(info)`，且 relaykit README 明示转换器不再解释 `-thinking`/effort 尾缀。验证条件（**第 44 轮定案 A 后扩充为四条**）：①带后缀的跨协议请求（oai→claude 至少一条）在快照或单测中渲染出正确 thinking 与裁剪后的模型名；②不带后缀的请求与现状逐字节一致；③**`gpt-*-max` 现在被裁**（A 新增能力面）；④**非 GPT/o 系列名字不再被裁尾缀**（家族白名单生效）。**A6 例外**：`ParseDeepSeekV4ThinkingSuffix` 保持本地实现、不得被上游窄词表（仅 `-none`/`-max`）覆盖（见 12.19 A6 行）——现有 8 个快照零后缀用例，抓不到该回归 | 12.19 |
| 16 | **未决项 33 已于第 44 轮定案为 C——本项为阶段 4（阶段 4）的实际动作，按 12.20.1 的连带清单落地（第 36 轮新增，阶段归属第 37 轮钉死——与第 13 项同批，阶段 5 行只核验不落地）**：C1 保留 `service/convert.go` 为 5 个 facade（不删文件，与处置清单第 2 项的删除范围差异已由 12.3 修正）、C2/C3 新增两个包装文件（含 `SetMediaResolver` 两个字段）、C4 新增 `conversion_diagnostics.go` + `RelayInfo.RecordConversionDiagnostics`、C5 流式 6 处构造 `NewResponseStreamState`、C6 通道级 4 函数保为 facade（阶段 6 判据随之改为「保留」）；测试侧：`service/` 4 文件只改 `*WithError` 的 17 处（其中 golden 6 处已由第 12 项认领，另 3 文件 11 处随本项增补）；**通道级 4 文件另有 41 处（HEAD 口径，基线 33 处）——第 37 轮补入、第 48 轮补 `CovertOpenAI2Gemini` 的 11 处**：`RequestOpenAI2ClaudeMessage` 16 处必改（无 facade 可依）、`CovertOpenAI2Gemini` 11 处必改（上游已删该函数，同样无 facade，见 9.2.5）、4 个非 WithError facade 8 处零改动、`*WithError` 6 处必改且其中 6 处（`relay_claude_test.go:689,697` + `tool_call_id_test.go` 4 处）钉住的是被 relaykit 丢弃的本地严格性，须随对应小节裁定——`relay_claude_test.go:689,697` 对应 **9.2.1 第 1、4 条**，`tool_call_id_test.go` 4 处对应 **9.2.3 第 8 条**（**第 38 轮修正**：原文把 6 处一并归给 9.2.1，但 `tool_call_id_test.go` 钉的是 Gemini 合成 tool call ID 的跨 chunk 唯一性，与 Claude 侧 9.2.1 无关）。**本项适用性（第 44 轮）**：定案为 C → **生效**（A/B 形态下原「不适用」的条款已不适用） | 12.20.1、9.2.1、未决项 33 |
| 17 | **9.2.2 反方向 stop_reason 严格校验的独立处置（第 44 轮新增——第 6 项已过载，本项单列）**：9.2.2 的 6 条差异（a 未知值报错 / b `""`→`"stop"` / c `TrimSpace` / d `model_context_window_exceeded`→`"length"` / e `pause_turn` 的 stop/length 分歧 / f 无差异）**已从第 6 项移出、独立处置**（第 46 轮修正：原第 6 项同时把 9.2.2 纳入、本项又说不在第 6 项范围内，两处矛盾；现第 6 项只管 9.1/9.2/9.2.1/9.2.3 共 33 条条件（9.2.3 拆条后 34 个裁定点），本项独立管 9.2.2 的 6 条，不重复计数）。**第 44 轮定案 B 后，本方向的处置形态与第 9 项同构**：把 `stopReasonClaude2OpenAIStrict`（基线 `relay/channel/claude/relay-claude.go:41`）迁出随阶段 4/6 收敛的通道文件、放入宿主共享助手，在调 relaykit 前校验并归一到 Claude 词汇表（`""`→`end_turn`、`model_context_window_exceeded`→`length`、`TrimSpace`+`ToLower`），未知值即 `return error`。**e 的 stop/length 分歧须显式裁定**，不能默认采用 relaykit 的 `length`——`pause_turn` 在本地是 `stop`，改变它会让客户端把「可续跑的 agent 轮次」误判为「被截断」。**现状**：无任何测试覆盖 `stopReasonClaude2OpenAIStrict`（`git grep` 全仓仅定义行 + 两个调用点 `:806`/`:889`），须为 a–d 各补一条 | 9.2.2、12.11.3 |
| 18 | **9.2.4/9.2.5 Gemini 请求方向入口转换器的独立处置（第 48 轮新增——与第 17 项同型：第 6 项已过载，且 9.2.4/9.2.5 是新补的两节，归入第 6 项会把「9.1/9.2/9.2.1/9.2.3 共 33 条」的既有口径撑大）**：本项管 **9.2.4 的 10 条（gemini→oai 请求）+ 9.2.5 的 11 条（oai→gemini 请求）共 21 条差异**（9.2.4 表第 11 行不存在——该表恰好 10 行，第 11 行是 9.2.5 表的「无差异」记录行；**实际保留/放弃裁定点是 20 个**，另 1 行是无差异确认行）。**处置形态与 9.2.4/9.2.5 的性质绑定**：
- **9.2.4（gemini→oai 请求，`service/convert.go:1248`）：本方向随 C2 的同名 shim 承接，shim 不删**——上游保留同名 shim（`upstream/main:service/request_converter.go:62`）。**承接路径有一处编译前置 + 一处时序错位（第 48/49 轮补）**：①编译前置——基线 `convert.go:18`/`:1248` 与 C2 的 shim **同包同名**（`package service`），C2 单独落地即 `redeclared` 编译失败，**C2 必须与 C1 同批、且 C1 须删掉这两个旧声明**（第 49 轮补，原第 48 轮只写了「C2/C1 同批」未写「删旧声明」这个编译前置）；②时序——C2 新增 `service/request_converter.go` 的 shim 与 C1 改写 `service/convert.go` 是**同批**（批 2），但本地旧实现体（`:1248-1599`）与两个严格助手在阶段 6 才删——**在阶段 4 与阶段 6 之间，shim 与旧实现体并存**；阶段 6 删旧实现体后，shim 是唯一入口。**因此本方向的严格函数有明确存活路径**：`validateGeminiRequestPartForOpenAI`（`:1600`）与 `convertGeminiRoleToOpenAI`（`:1652`）**须在 C2 shim 落地时（批 2）由 shim 显式调用**——不能等到阶段 6，因为 shim 落地那一刻旧实现体就不再被生产路径走到（shim 已接管 `relay/channel/openai/adaptor.go:47` 的调用），严格校验若只留在旧实现体里即已死代码化。**实际落点**：`service/request_converter.go` 的 `GeminiToOpenAIRequest` shim 入口加前置校验，调用 `validateGeminiRequestPartForOpenAI` + `convertGeminiRoleToOpenAI`，未知值返回 error；本地 `convert_test.go` 钉住的 6 个 Rejects\* 语义须逐条裁定（10 个测试中另 4 个是正向渲染用例）。**第 50 轮补——严格助手的落点前提**：上述「shim 显式调用 `validateGeminiRequestPartForOpenAI` + `convertGeminiRoleToOpenAI`」有个**此前未写的编译前提**——这两个助手位于 `service/convert.go` 的非 facade 体内，**C1 改写会把它们一并删掉**（它们不在上游 27 行的 5 个 facade 里），而第 13 项的共享助手迁出目前只列了 `stopReasonOpenAI2ClaudeStrict`/`openAIFinishReasonToGemini`**未含这两个**；故若裁定「保留严格性」，这两个助手**必须随第 13 项在 C1 之前迁入同一共享助手文件**（否则 C2 的 shim 调用它们即 `undefined` 编译失败）——与 C2/C1 同包同名冲突同类，是本次裁定的**前置条件**，不是裁定后的实现细节。若裁定「照抄上游放弃严格性」，则这两个助手随 C1 删除、shim 不调，无此前提。
- **9.2.5（oai→gemini 请求，`relay-gemini.go:201`）**：本方向随 C6 由 `service.ConvertRequest(RelayFormatGemini)` 承接（上游已删 `CovertOpenAI2Gemini` 名字，见 12.20.1 生产表新行）。**严格函数随 568 行主体整体删除**，要么在迁移前迁出入参校验逻辑（stop≤5、tools type/name/dup/JSON-object、role 白名单、part type 白名单），要么逐条裁定放弃。**与 9.1 第 8 条（finish_reason）不同**：那条是单字段输出语义，本方向是整段入参校验（11 条差异，覆盖 tools/messages/system/role/generationConfig），**不能并入第 9 项的 finish_reason 共享助手**，须独立落点（建议宿主 `relay/channel/gemini/gemini_request_guard.go` 或随 C6 的包装层前置）。**批 3 判定：9.2.4 与 9.2.5 的 20 个裁定点全部逐条裁定「保留（宿主前置校验）/ 放弃（记行为变更并改写测试）」，不得默认静默化**（第 11 行是「无差异」确认行，不产生裁定）——尤其 9.2.4 第 2 条（role 静默重解释 user）与 9.2.5 第 9 条（role 直接透传）是本地明确拒绝的语义 | 9.2.4、9.2.5、12.20.1 |

**不在阶段 4 范围内**：向**上游**提 `ParseContentStrict`/`ParseToolCallsStrict` 的 PR（4.1「值得提上游」）——那是对上游仓库的贡献，不阻塞本迁移，也不属于本仓库的任何阶段。

**阶段 1 的前置：relaykit 源码的取得与版本锚定**

本仓库当前**没有** `relaykit/` 目录，`HEAD` 上也不存在；上游的 relaykit 是在提交 `86ac0f774`（`refactor: extract protocol conversion layer into standalone relaykit module (#6369)`，2026-07-27，368 文件）引入的，该提交**不是本地 `HEAD` 的祖先**。因此 relaykit 源码必须显式取得，不能靠本地历史恢复。

**取得方式**：整体拷贝 `upstream/main:relaykit/` 到仓库根的 `relaykit/`。依据（已实测）：

- **relaykit 自包含**：全目录对宿主零 import——`git grep '^\s*[a-z]*\s*"github.com/QuantumNous/new-api/' upstream/main -- 'relaykit/**/*.go'` 排除 `relaykit/` 自身后为空。（`relayconvert/boundary_test.go` 里的 `modulePrefix`/`forbiddenPrefixes`/`allowedViolations` 是**以测试形式强制该约束**的常量，不是反向依赖；`allowedViolations` 为空，且文件约定「只减不增」。阶段 1 拷贝它即等于拷贝了一个阶段 2 必须满足的硬门禁，详见「阶段 2 的前置：`boundary_test.go` 是阶段 2 的硬门禁」。）
- 规模：157 个文件（96 个非测试 `.go`）、35198 行，含自己的 `relaykit/go.mod` 与 `relaykit/go.sum`。
- 上游接入方式照抄即可：根 `go.mod` 加 `require github.com/QuantumNous/new-api/relaykit v0.0.0` + `replace ... => ./relaykit`（`upstream/main:go.mod:176,179`）。

**版本锚定（必须做，否则本文档全部行号失效）**：本文档所有 `relaykit/...` 行号均以 `upstream/main` = **`789c97019`**（2026-09-28）为准。拷贝后必须把该 commit 记入 `relaykit/VERSION` 或等价文件。

**行号引用的机械核对（已补工具，原先只有一句「须重新核对」）**：**第 44 轮新增/改写的引用（`service/convert.go:1125`、`relay/channel/claude/relay-claude.go:41`、`setting/model_setting/global.go:208`、`upstream/main:relay/channel/claude/adaptor.go:39`、`upstream/main:relay/claude_handler.go:41`）已逐条人工核对内容正确**（门禁只校验「行存在、非空白」，不断言内容——见下「不断言该行内容」段）；后续若 HEAD 漂移导致这些行号失效，门禁会抓红，但**内容是否仍正确须人工复核**。本文档 relaykit 行引用现为 **82 处唯一**（**第 53 轮**：73→82——短横线范围引用展开端点，`relaykit/relayconvert/convmeta/meta.go:18-54`、`relaykit/relayconvert/reasoning/claude.go:35-78`、`relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go:342-365` 等 9 个终点行进门禁；**第 52 轮**：69→73——斜杠行号列表逐项进门禁（C1 行的 `relaykit/relayconvert/response_compat.go` :34/:38/:42/:128/:132 五个 facade 锚点此前只核 :34）；**第 50 轮**：+1——C5 行补入 `relaykit/relayconvert/request_compat.go:28` 的 `OpenAIChatRequestToGeminiGenerateContent` 公开再导出锚点）（**第 48 轮**：新增 3 处——`relaykit/relayconvert/internal/gemini_chat/to_oai_chat_req.go:18` 的 `GeminiGenerateContentRequestToOpenAIChat` 与 `relaykit/relayconvert/internal/oai_chat/to_gemini_chat_req.go:19` 的 `OpenAIChatRequestToGeminiGenerateContent` 与同文件 `:157-167` 的生成参数段；**第 47 轮**：新增 `relaykit/relayconvert/internal/oai_chat/to_gemini_chat_resp.go:518` 的 `geminiFinishReason`；**第 46 轮**：第 44 轮为 62，本轮补入 2 处 relaykit 全路径（`relaykit/relayconvert/convmeta/meta.go` 的 `HasChannelMeta`/`Values.ReasoningConversion`）；第 44 轮：第 43 轮为 61，12.19 补入 1 处；第 43 轮：第 42 轮为 56——本轮给 5 处歧义裸引用补 relaykit 全路径；第 38 轮为 51——本轮 12.5/4.4 补入 5 处 relaykit/dto 全路径引用，见 12.5 与本节「固有边界」段）（第 28 轮时 34 处：18 全路径 + 23 简写 − 7 重叠；此后 29–35 轮陆续新增，本段计数此前未随批次更新——**第 36 轮修正**）。除 relaykit 外，第 36 轮起**宿主路径也进门禁**：裸宿主引用 **150 处**（**第 53 轮**：145→150——短横线范围终点进门禁，`relay/claude_handler.go:55-104`、`relay/helper/price.go:73-77`、`dto/openai_request.go:1283-1307`、`service/convert.go:233-250`、`relay/channel/claude/relay-claude.go:366-414` 五个终点行；**第 52 轮**：斜杠行号列表逐项进门禁，每列表由 1 条变逐行）（**第 49 轮**：+1——C5 行补入 `relay/channel/gemini/adaptor.go:243`；**第 50 轮** +3——第 13/18 项补入 `service/convert.go:1600`，C5 行补入 `service/error.go:25`、`relay/responses_handler.go:1692` 的两处本地改动落点）（在本地基线 `dbfbc096f` 解析；**第 48 轮**：新增 4 处——`service/convert.go:1248`/`:1600`/`:1652`（`GeminiToOpenAIRequest` 与两个助手）与 `relay/channel/gemini/relay-gemini.go:201`（`CovertOpenAI2Gemini`）；**第 47 轮**：新增 2 处 `service/convert.go`（`:1125` 共享助手迁移源与 `:1915` `openAIFinishReasonToGemini`）；**第 46 轮**：第 44 轮为 111，本轮补入 `relaykit/relayconvert/convmeta/meta.go` 的 `HasChannelMeta` 锚点（宿主侧引用）；**第 44 轮**：第 43 轮为 108，补入 `service/convert.go:1125`、`relay/channel/claude/relay-claude.go:41`、`setting/model_setting/global.go:208` 三处；**第 43 轮**：第 42 轮为 107，本轮修正 `GetClaudeSettings` 锚点（`:55` 实为 `:43`）、补 gemini 设置文件锚点与 2 处 adaptor 全路径，净 +1；**第 38 轮修正**：原文写 103，与本节下一段的 107/107 及门禁实际输出均不一致）+ `upstream/main:<宿主路径>:NN` 全路径 **47 处**（**第 53 轮**：44→47——短横线范围终点进门禁，`upstream/main:relay/common/relay_info.go:201-202`、`upstream/main:relay/common/relay_info.go:221-223`、`upstream/main:relay/channel/claude/relay-claude.go:129-134` 三个终点行；**第 49 轮订正**：原文写「+1——`789c97019:relay/common/relay_info.go:204-206` 钉点」，**归因错误**——`789c97019:` 是 sha 前缀形态、当时不在任何组内，43→44 的新增对实为同轮补入的 `upstream/main:relay/common/relay_info.go:221-223`（起行 221）；该 sha 形态到**第 53 轮**才随 sha 锚点组进门禁）（**第 48 轮**：新增 3 处——`upstream/main:relay/channel/gemini/adaptor.go:174`、`upstream/main:relay/channel/vertex/adaptor.go:294`、`upstream/main:service/request_converter.go:62`；**第 46 轮**：第 44 轮为 35，本轮把 12.19 的 `BaseModelName` 消费者 4 处加 `upstream/main:` 前缀（`upstream/main:relay/helper/model_mapped.go:35`、`upstream/main:relay/helper/price.go:313`、`upstream/main:model/model_pricing_conversion.go:220`、`upstream/main:setting/ratio_setting/model_ratio.go:712`）；**第 44 轮**：第 43 轮为 34，把 12.19 新增的 `upstream/main:relay/channel/claude/adaptor.go:39` 计入；**第 43 轮**：第 42 轮为 27，本轮把 7 处上游语义的裸引用加 `upstream/main:` 前缀（其中 2 处与已有全路径同对，去重后净 +7）；第 40 轮为 24，本轮 12.19 补入 3 处；在钉死的 `789c97019` 解析——即本文档的锚定版本，不用会漂移的 `upstream/main` 默认值）。**第 37 轮更新此三处计数**（36 轮为 98/21）。原先只写「后续需要跟进上游时重新核对本文档的行号引用」，**没给任何可执行手段**——而上游 relaykit 仍在演进（见「边界与未验证项」），锚定一漂移，全部引用静默失效。现已固化为：

```bash
scripts/relaykit-citation-check.py            # 校验，漂移时退出码 1
scripts/relaykit-citation-check.py --ref <ref>  # 换锚点复核
python3 scripts/relaykit-citation-check.test.py # 54 个单测（第 43 轮 +9：裸 X.go:NN 组；第 41 轮 +1：docs/ 引用不得 fail-open；第 52 轮 +8：斜杠行号列表逐行展开；第 53 轮 +11：短横线范围端点 + sha 钉点组）
```

它从本文档抽出全部引用，逐条在指定 ref 下检查**文件存在、行号在范围内、该行非空白**，只抓机械失效（文件没了、行越界、指向空行），**不断言该行内容**——引用指向的符号变了仍需人工复核。**实测（第 53 轮重测）五组全绿：** **82/82**（`ref=789c97019`，即本文档锚定版本；**第 53 轮** 73→82——短横线范围终点展开（9 个 relaykit 范围的终点行，`relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go:342-365`、`relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go:450-495`、`relaykit/relayconvert/convmeta/meta.go:18-54`、`relaykit/relayconvert/reasoning/claude.go:35-78`、`upstream/main:relaykit/relayconvert/golden_test.go:323-325` 等）；**第 52 轮** 69→73——斜杠行号列表逐项展开；**第 50 轮** 68→69——`request_compat.go:28` 再导出锚点；第 47 轮为 65、第 44 轮为 62、第 43 轮为 61、第 42 轮为 56）、**宿主 150/150**（`baseline=dbfbc096f`）（**第 53 轮** 145→150——5 个宿主短横线范围终点；**第 49 轮** 118→119；**第 50 轮** 119→124；**第 52 轮** 124→145——斜杠行号列表逐项展开（+21 = `service/convert_test.go` 十行列表 +9、`relay/channel/gemini/relay-gemini.go` 八行列表 +7、`relay/channel/deepseek/adaptor.go` 三行列表 +2、`relay/channel/openai/responses_via_chat.go:951/955`、`relay/helper/valid_request.go:128/183`、`service/openaicompat/chat_to_responses_response.go:51/53` 三个两行列表各 +1；各列表本体见 9.2.4、12.0.8、12.6、12.15.4 等处））（第 47 轮为 114、第 44 轮为 111、第 43 轮为 108、第 42 轮为 107）、**upstream 全路径 47/47**（**第 53 轮** 44→47——3 个 upstream 短横线范围终点；**第 49 轮** 43→44——`upstream/main:relay/common/relay_info.go:221-223`，**第 53 轮订正归因**：原文写「+1——`789c97019:…:204-206`」，该 sha 形态当时不在组内，见 §11 上段）（第 47 轮为 40、第 44 轮为 35、第 43 轮为 34、第 42 轮为 27、第 40 轮为 24；第 42 轮 12.19 补入 3 处 `upstream/main:setting/...`、`relay/common/...` 引用，第 43 轮再补 7 处 `upstream/main:` 前缀，第 44 轮补 1 处 adaptor 全路径，第 46 轮补 4 处 `BaseModelName` 消费者）、**裸引用 67/67（未解析 3）**（**第 53 轮**：`convert_golden_test.go:255-258` 范围展开为两个端点、均无对应文件，未解析 2→3；**第 49 轮** 60→61：C5 行新补入 1 处 `dbfbc096f:` 前缀的基线引用）、**sha 引用 9/9**（**第 53 轮新增组**：`<sha>:<path>:NN` 显式钉点形态——`789c97019:…`（上游钉点）与 `dbfbc096f:…`（基线）共 9 对唯一引用，此前四组提取器的负向后顾都把前导 `:` 挡住、零覆盖；`HOST_CITATION` 上方注释还声称该形态「covered by their sha form」，实际并无该提取器。每组在引用自己写明的 sha 上核对；本文档 sha 引用 **9 处**——3 处 `789c97019:relay/common/relay_info.go:204-206`（范围展开为 :204/:206 两端点）+ `789c97019:service/request_converter.go:62`、`:14`、`789c97019:relay/channel/claude/adaptor.go:122`、`:138`、`789c97019:relay/channel/gemini/adaptor.go:229` + `dbfbc096f:relay/channel/claude/relay-claude.go:1134`、`dbfbc096f:relay/channel/openai/responses_via_chat.go:138`）（第 43 轮新增组，见下方「第二类边界」段；第 44 轮把 2 处裸 adaptor 引用补全路径，57→55；第 45 轮优化又新增 1 处裸引用，55→56；**第 47 轮把 4 处「`service/convert_golden_test.go:NNN`」裸引用改为不带行号的字面量引用，56→60——该文件在基线 `dbfbc096f` 上不存在、建于 `HEAD`，裸引用会落到 `unresolved`**）。**注意：不带 `--ref` 的默认运行已因 `upstream/main` 漂移而变红**——第 40 轮实测 `upstream/main` 已前进到 `04c597bf6`（PR #7698，2026-10-08 16:07），默认 ref 下得到 `50/51`（`convmeta/meta.go:58`、`upstream/main:relay/channel/claude/relay-claude.go:37`、`:195` 三处行内容变空白）与 `upstream-full 22/24`、退出码 1；`--ref 789c97019` 恢复全绿。这正是「必须钉 ref、不能依赖移动的 `upstream/main`」的现场证据——本段旧文写「`ref=upstream/main` 51/51」，该表述自 `upstream/main` 移动起不再成立（**第 40 轮修正**）。已做**反向验证**（五次扰动全部退出码 1）：relaykit 全路径 `:99999` → `out of range`；改文件名 → `file not found`；简写路径 `:99999`；宿主 `<宿主文件>:99999` → `out of range at dbfbc096f`；**去掉一个 `upstream/main:` 前缀 → 报「not found at baseline but found at upstream/main: add an upstream/main: prefix」**。还原后退出码 0、文档字节一致。

**简写路径的支持（第 28 轮补充）**：文档大量用省略 `relaykit/relayconvert/` 前缀的写法，形如 `internal/oai_chat/<文件>.go:<行>`、`oai_chat/<文件>.go:<行>`、`convmeta/<文件>.go:<行>`、`shared/claude/<文件>.go:<行>`。旧版正则要求 `relaykit/` 前缀，**这 23 处唯一简写引用全部在门禁之外**，而检查器仍报 18/18 绿——与第 26 轮的假绿同类（那次是新表缺前缀）。现按**显式映射表**（`SHORT_PACKAGES`，逐个核实过真实目录）扩展：`internal`/`oai_chat`/`oai_responses`/`claude_messages`/`gemini_chat`/`shared`/`media`/`convdiag` → `relaykit/relayconvert/internal/...`；`convmeta`/`kitutil` → `relaykit/relayconvert/...`；`reasonmap` → `relaykit/reasonmap/...`。**宿主路径（`relay/`、`service/`、`controller/`、`common/`、`setting/`、`aws/`、`vertex/` 与裸 `dto/`、`types/`）一律不扩展**——宿主有自己的 `dto/`、`types/`，文档里的 `dto/...:NN` 指宿主，误扩展会造出假引用。**3 个单测**覆盖了「宿主路径不得被扩展」这一条（`test_does_not_expand_host_paths`、`test_does_not_extract_upstream_prefixed_host_paths`、`test_does_not_extract_sha_prefixed_host_paths`）——**第 38 轮修正**：原文写「15 个单测」，那是当时该文件的总用例数、且并非都测这一条；此处改为按用例名指认（总用例数由 §9.3 的门禁自述检查覆盖）。**注意本段自身不能用真引用作示例**——写成 `foo.go:12` 会被检查器当成待解析的引用（本轮就踩过一次），故示例一律用 `<文件>`/`<行>` 占位。

**固有边界（不得声称已覆盖）**：文档还有大量**裸 `:NNN`** 引用（如 `:749`、`:1049`、`:345`、`:247`、`:463`），它们不带路径，**无法机械归属**到某个文件（同一节里可能同时指上游与本地），故**不在门禁内**。凡关键行号，应写成带路径的形式以进入门禁。

**第二类边界——已闭合（第 43 轮，42 轮遗留的覆盖缺口）：裸「`X.go:NN`」（有文件名、无目录前缀）进门禁。** 第 42 轮实测全文 **249 个唯一 `.go:NN` 引用中 79 个不被四类提取器命中**（`relaykit/...` 全路径、简写包根路径、宿主目录前缀、`upstream/main:` 前后缀），当时记为「覆盖缺口、不声称已覆盖」。第 43 轮在 `relaykit-citation-check.py` 新增**第四组**：按 basename 对两侧文件树做**唯一后缀匹配**——只在 `relaykit/` 唯一命中的在钉死 ref 核对、只在宿主基线唯一命中的在 `dbfbc096f` 核对；**两侧同名或任一侧多名 = 歧义，直接失败**（要求写全路径，不猜测）；两侧都无 = 计入 `unresolved` 并逐条列出（不判失败——占位符与工作区文件落在这里）。核对结果：裸引用 ok **67**（**第 49 轮** 60→61；**第 50 轮** 61→62——C5 行补入 `internal/oai_chat/to_gemini_chat_req.go:19`；**第 52 轮** 62→67——斜杠行号列表逐项展开，裸组 +5：`chat_to_responses_response.go:193/222/243/259` 与 `golden_test.go:290/319/337` 各由每列表 1 条变逐行（IncludeThoughts 的八行列表是宿主全路径引用，在宿主组展开，不落裸组））（**第 47 轮**：把 4 处指向工作区文件 `service/convert_golden_test.go` 的裸引用改为不带行号的字面量引用——该文件两 ref 均不存在，裸引用只会落 `unresolved`；**第 44 轮**：第 43 轮为 57，本轮把 2 处裸 adaptor 引用补全为 `upstream/main:relay/channel/claude/adaptor.go:39`，歧义消除；**第 45 轮优化新增 1 处裸引用，55→56**）、未解析 **3** 处——`foo.go`（本节自己声明的占位示例）、`convert_golden_test.go:255` 与 `:258`（第 18 轮新增的工作区文件，两 ref 均不存在；**第 53 轮**：`convert_golden_test.go:255-258` 范围展开为两个端点，2→3）。42 轮点名的 4 处两侧同名（`gemini.go`、`openai_request.go`、`pricing.go`、`reasoning/claude.go`）由**补齐全路径**解决（歧义对门禁是失败，不是可绕过项）。同轮顺手修掉 7 处「上游 `X.go:NN`」写成裸形——它们按裸规则会落到宿主基线，其中 claude 通道的 `:349` 在基线是**空行**（门禁当场抓红，内容在上游同名文件第 349 行），全部改加 `upstream/main:` 前缀后归入全路径组。**反向验证**：去掉任一 `upstream/main:` 前缀 → `FAIL bare … is blank at dbfbc096f`、退出码 1；把占位符改名成新路径 → `unresolved` 2→3、selfcheck 报「§11 裸引用未解析数：文档写 2，实际 3」、退出码 1；还原后全绿、文档字节一致。

**实测补充**（阶段 1 执行时需知）：上游 relaykit **当前没有版本文件**（顶层仅 `README.md`、`go.mod`、`go.sum` 与 4 个目录），故新建 `relaykit/VERSION` 不与上游冲突；且 `relaykit/relayconvert/boundary_test.go` 只遍历 `kitDirs`（`relayconvert`/`dto`/`types`/`reasonmap`）下的 `.go` 文件，**新增 `relaykit/VERSION` 不会触发边界门禁**（已读 `boundary_test.go:53` 的过滤条件确认）。

**本迁移对上游 relaykit 的本地改动清单（必须维护，否则「避免分叉」无从谈起）**：本迁移**必然对上游 relaykit 产生本地分叉**——补丁 1、补丁 2 与 12.4 的门控都要**改写 relaykit 内部的 `.go` 源码**，而非仅在宿主侧接线。这与 12.9「不以上游分支为补丁基线，避免与 main 产生不可合并分叉」的取舍**方向不同**（那一条讲的是「基线选哪个分支」，本条讲的是「本地必然有改动」）。规格此前**散落三处提到补丁，但没有一处汇总改动落在哪些文件**，而 `relaykit/VERSION` 锚定的意义正是：跟进上游时要能一眼看出「哪些差异是本地有意为之、哪些是上游漂移」。

| # | 改动 | 落点文件（上游 `789c97019`） | 阶段 |
|---|---|---|---|
| 1 | content block switch 加 `case "thinking"`、非 assistant role 门控、补 `default:` 显式报错 | `relaykit/relayconvert/internal/claude_messages/to_oai_chat_req.go:181`、`relaykit/relayconvert/internal/claude_messages/to_oai_responses_req.go:165` | 4（补丁 1） |
| 2 | DTO presence 保真（把本地既有实现带进 relaykit） | `relaykit/dto/*`（见 3.3 补丁 2 表） | 2 |
| 3 | 响应方向 reasoning 暴露门控（**在索引分配之前**，见 12.4.1） | `relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go:111`（流式）、`relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go:449`（非流式）两条入口，已接收 `info convmeta.Meta`，零透传成本） | 4（见 12.4.4） |
| 4 | `convmeta.Options` 新增钩子字段 | `relaykit/relayconvert/convmeta/options.go:9`（`type Options struct`）；现有同类字段 `relaykit/relayconvert/convmeta/options.go:29`（`PreserveThinkingSuffix`）、`:33`（`PreserveEffortTail`）、`:52`（`DefaultMaxTokens`）、`:72`（`SupportsImagine`）、`:75`（`SafetySetting`） | 4（两个钩子 `RetainEmptyReasoning` 与 `RetainReasoningInClaudeResponse` 同批加字段；请求方向随补丁 1 接线、响应方向随 12.4 门控接线，见 12.4.4） |
| 5 | think 标签剥离纯函数迁入 | 新增（宿主 `service/convert.go:554`/`:597`/`:629` 原样移入） | 4 |
| 6 | `ParseContentStrict`/`ParseToolCallsStrict` 迁入 | `relaykit/dto/openai_request.go`（新增；上游无同名方法） | 4 |
| 7 | `EffectiveTools()` 及缓存字段 | `relaykit/dto`（12.0.7） | 2 |
| 8 | `ClaudeConvertInfo` 等宿主专有错误符号 | `relaykit/types`（12.2） | 1 |
| 9 | **不改动，但须保持绿色：上游两条依赖「无条件暴露 reasoning」的测试** | `relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp_test.go:82`（`TestResponseOpenAI2ClaudePreservesReasoningBeforeText`）、`:125`（`TestStreamResponseOpenAI2ClaudeClosesTextThinkingAndToolBlocks`）：两条都构造 `convmeta.Values` 而**不设 `Options`**（该文件 0 处 `Options:`）。**在正确的 nil 语义（`nil = 保留`）下它们无需改动即保持绿色**；它们的作用是**约束 nil 语义**——若误实现为 `nil = 不保留`，这两条必红（12.4.4 第 2 点）。阶段 4 须把「这两条仍绿」作为 nil 语义实现的验证条件 | 4 |
| 10 | **schema 归一化改回本地的严格语义**（第 28 轮新增） | `relaykit/relayconvert/internal/shared/claude/schema.go:3` `FunctionParametersToInputSchema`——须补 error 返回（非 map / 不可序列化时报错）与 `type` 非 string 校验；否则非法 `input_schema` 从「报错」变成「静默降级为 `{"type":"object","properties":{}}`」（行为回归）。差异清单见 12.15.4.1 | 4 |
| 11 | **媒体分派补齐**（第 28 轮新增） | `relaykit/relayconvert/internal/oai_chat/to_claude_messages_req.go`（switch 在 `:255`，`default` 分支的 MIME 判定在 `:277`）——须补：`ContentTypeFile` 独立 case、`text/*`→text 块、不支持的 MIME 显式报错、`ToFileSource()==nil` 报错（现为静默 `continue`）、MIME 归一化（`ResolveBase64Data` 自身零归一化）。差异清单见 12.15.4.2 | 4 |
| 12 | **finish_reason 严格语义注入（第 29 轮新增；第 37 轮修正：原「仅选项 A」不成立；第 44 轮定案为 B——本行不适用）** | **第 44 轮定案为 B（宿主共享助手前置校验，见 12.11.3），本行所述的 relaykit 内部改动不适用**——`relaykit/reasonmap/reasonmap.go:31` `OpenAIFinishReasonToClaudeStopReason` 保持返回裸 `string`、无 error 通道，relaykit 零改动。**共享助手是宿主文件、不进 relaykit**（第 46 轮补：与第 1583 行呼应），`relaykit/VERSION` 跟进时零差异。**保留 A1 方案作对照**（第 46 轮修正「原文」措辞）：若选 A1，则须「给 `reasonmap` 加返回 error 的严格变体 `OpenAIFinishReasonToClaudeStopReasonStrict` + 改 `oai_chat` 的 `StreamResponseOpenAI2Claude`/`FinalizeStreamResponseOpenAI2Claude`/`ResponseOpenAI2Claude` 3 个函数签名及其调用点」，b/c/d 与未知值报错可恢复，另须新增一行登记签名变更；**A2/B 下本行不适用**——校验由宿主共享助手完成。B 的落地落点在阶段 4 处置清单第 9 项。差异与影响面见 12.11.2、12.11.3 | 4 |
| 13 | **（第 46 轮补）共享助手：`stopReasonOpenAI2ClaudeStrict` + `openAIFinishReasonToGemini` 迁出（第 47 轮扩为两个函数）** | 宿主新文件（建议 `service/finish_reason_strict.go` 或并入 `service/response_converter.go`，**阶段 4 批 1 定**）——把基线 `service/convert.go:1125` 的严格函数（Claude 方向）与 `service/convert.go:1915` 的 `openAIFinishReasonToGemini`（**Gemini 方向，第 47 轮补，9.1 第 8 条**）**原样迁入同一文件**，各配一个归一化助手（`NormalizeOpenAIFinishReasonsForClaude` / `…ForGemini`），**必须在 C1 改写 facade 之前执行**（见执行次序表批 1 与 C1 行第 46 轮修正）——两个函数都不在 C1 保留的 5 个 facade 里，C1 会一并删掉；**第 50 轮补——若处置清单第 18 项裁定保留 gemini→oai 请求方向的严格性，则 `validateGeminiRequestPartForOpenAI`（基线 `service/convert.go:1600`）与 `convertGeminiRoleToOpenAI`（`:1652`）也是同一类「C1 会删、但批 2 的 shim 需要」的函数，必须在 C1 之前一并迁入该共享助手文件**（它们同样不在 5 个 facade 内，唯一调用点在 `GeminiToOpenAIRequest` 体内，见 9.2.4）；按上游照抄则这两个助手在上游侧不存在、shim 不做校验（`789c97019:service/request_converter.go:62` 的 shim 只 `ConvertRequest` + 断言），故**保留=本地差异、放弃=照抄上游**，属第 18 项的裁定内容，此处只钉「保留则须迁出否则编译失败」 | 4 |
| 14 | **（第 46 轮补）填充器 A1–A6 移植（宿主侧差异，不改 relaykit）** | `relay/helper/reasoning_suffix.go`（232 行）+ `relay/helper/model_modifier.go`（278 行）+ `setting/reasoning.ParseLegacyModelSuffix`（加性补，含私有助手 `splitModelNamespace`）+ `model_setting.ShouldPreserveEffortTail` + `convmeta.Options.PreserveEffortTail` 接线 + DeepSeek 富词表保持本地实现（A6 例外）——**A1–A6 是宿主侧文件，不进 relaykit**，但须登记到 `LOCAL_PATCHES.md` 以说明与上游的差异 | 4 |
| 15 | **（第 46 轮补，同日复核撤销）`HasChannelMeta` 已在阶段 3 的 13 个方法之内** | `convmeta.Meta` 接口方法（`relaykit/relayconvert/convmeta/meta.go:23`）——第 46 轮初判「阶段 3 漏了 `HasChannelMeta`、应为 14 个」，**复核后推翻**：`git show dbfbc096f:relay/common/relay_info.go` 的 26 个方法中，接口 15 个方法只命中 2 个（`AppendRequestConversion`、`GetEstimatePromptTokens`），故须新增 **13 个**，`HasChannelMeta` 正在其中。阶段 3 行原文无需改动。**本行保留为「已核查、无需动作」记录**（原初判的误报已在阶段 3 行注明） | 3 |

**维护要求（第 44 轮：`relaykit/LOCAL_PATCHES.md` 由「建议」升为阶段 1 交付物）**：`relaykit/VERSION` 只记上游锚点一个字段，**不足以识别本地改动**。须有一份「本地改动记录」——最简形式是 `relaykit/LOCAL_PATCHES.md`，逐条记上表的落点与目的，**并记改动前后的 diff 或至少改动涉及的代码块（函数名/case 标签等）**（第 46 轮补：只记「落点 + 目的」无法定位冲突——知道「改了某个 `to_oai_chat_req.go` 的 switch」并不能帮助判断上游的同一行是否也改了；要定位冲突须知道改动的具体内容）；**第 44 轮定案后本地分叉已扩到 A1–A6（填充器移植 + DeepSeek 例外）+ 共享助手（严格函数迁出）+ 门控字段 + schema/media 两个上游改动，必须从阶段 1 起随改动逐条登记，否则跟进上游时「哪些 hunk 会冲突」无从定位**。跟进上游升级时，先用它定位「哪些 hunk 会冲突」，再跑 `scripts/relaykit-citation-check.py --ref <新锚点>`（行号）与 `KIT_REF=<新锚点> scripts/relaykit-dto-compat.sh --strict`（结构）复核（**第 40 轮**：两命令都须带锚点参数，用会漂移的 `upstream/main` 默认值核对钉死声明必然报错）。**没有这份记录，锚定就只能锚住「上游是哪天」，回答不了「我们改了什么」。**

**依赖影响**：relaykit 的 `require` 会经 MVS 抬高根 module 的两个依赖版本——`github.com/samber/lo` `v1.52.0 → v1.53.0`（本地 58 个文件在用）、`github.com/tidwall/gjson` `v1.18.0 → v1.19.0`（本地 14 个文件在用）。`google/uuid`、`stretchr/testify`、`tidwall/sjson` 版本相同；`golang.org/x/text` 本地（v0.41.0）高于 relaykit 要求（v0.35.0），不受影响。阶段 1 完成后须跑全量测试确认这两个次版本升级无行为回归。

**`go.work`**：上游仓库既无 `go.work` 也无 `go.work.sum`。本地与 CI 一律用 `GOWORK=off` 分别构建/测试两个 module，**不引入 `go.work`**——它会让两个 module 共享同一依赖解析结果，掩盖 relaykit 自身的依赖闭包问题。上游的 `makefile` `test` 目标（`upstream/main:makefile:43-49`）就是 `GOWORK=off go test` 两次（根 module 排除 relaykit 子目录、再 `cd relaykit` 单独测），可作模板。

**阶段 2 的前置：`IsStream` 迁移的实际范围**

§7 硬依赖 3 只举了 `dto/gemini.go:112` 一例，实测影响面远大于此（已逐项核验）：

- **12 个方法**（11 个具体类型 + 1 个接口方法）签名从 `*gin.Context` 改为 `*http.Request`，分布在 **8 个文件**：`dto/audio.go:44`、`dto/embedding.go:48`、`dto/gemini.go:111,518,546`、`dto/openai_image.go:167`、`dto/openai_request.go:207,1253`、`dto/openai_responses_compaction_request.go:135`、`dto/request_common.go:10,22`、`dto/rerank.go:21`
- **`dto/request_common.go:10` 是接口方法**，签名一改，全部实现者同时编译失败——不能逐个改
- **10 个非测试 `dto/` 文件 import gin**（上列 8 个 + `dto/claude.go`、`dto/opaque_openai_responses_request.go`）；迁移后 relaykit 内不得再有 gin 依赖，这些 import 必须清除
- **5 个生产调用点**：`relay/common/relay_info.go:690`、`relay/responses_handler.go:669,755`、`service/responses_bootstrap_recovery.go:192` 手上都只有 `*gin.Context`，需要一个 `c.Request` 的取用路径；`relay/channel/claude/relay-claude.go:346` 传的是 `IsStream(nil)`——该处接收者是 `dto.GeneralOpenAIRequest`，其实现为 `lo.FromPtrOr(r.Stream, false)`，**不触碰 `c`**，所以传 `nil` 是安全的既有用法。**唯一解引用 `c` 的实现是 `dto/gemini.go:111`（`c.Query("alt")` 与 `c.Request.URL.Path`）**，迁移后这两处都要改从 `*http.Request` 取值

**阶段 2 的前置：`boundary_test.go` 是阶段 2 的硬门禁**

上游 relaykit 自带 `relaykit/relayconvert/boundary_test.go`，它**以测试形式强制**「将要迁入 relaykit 的包不得 import 宿主」这条边界。阶段 1 整体拷贝 `upstream/main:relaykit/` 时该测试一并进入本仓库，从阶段 1 起就被 §9.3 的 `cd relaykit && GOWORK=off go test ./...` 执行。实测该测试：

- 遍历 `kitDirs = {"relayconvert", "dto", "types", "reasonmap"}` 下所有非测试 `.go`，逐个解析 import
- `forbiddenPrefixes = {"github.com/QuantumNous/new-api/", "github.com/gin-gonic/gin"}`——命中即 `t.Errorf`，只有 `relaykit/` 自身前缀与 `allowedViolations` 可豁免
- **`allowedViolations` 是空 map**，且文件注释明写「**shrink this list, never grow it**」（「All initial violations have been cleared」）。**因此豁免不是退路**——把宿主 import 加进该表即违反文件自身的契约
- 它以「含 `go.mod` 的最近祖先」为根（`repoRoot`），即 `relaykit/` 自身；`kitDirs` 是相对 `relaykit/` 的路径，与 `relaykit/dto/` 直接对应

**后果**：阶段 2 若把本地协议 `dto` 迁入 `relaykit/dto` 而不先清掉宿主 import，阶段 2 的完成判据「DTO 测试通过」**必然为红**，且失败信息会把每一条宿主 import 逐行列出。本仓库现状（`HEAD`，非测试 `.go`）：

| 被 dto import 的宿主包 | 文件数 | 符号 |
|---|---|---|
| `common` | 12 | `Marshal`、`Unmarshal`、`GetJsonType`、`Interface2String`、`JsonRawMessageToString`、`Any2Type`、`RawMessage`、`JsonDelim`、`NewJsonDecoderUseNumber`、`DecodeJsonFromDecoder`、`ResponsesRequestMetadata` |
| `constant` | 2 | `ChannelTypeCodex`、`ChannelTypeOpenAI`、`ChannelTypeAzure`、`EndpointType` |
| `logger` | 1 | `LogError` |
| gin | 10 | （见上一节「`IsStream` 迁移的实际范围」） |

**清除路径与缺口**：`relaykit/relayconvert/kitutil` 提供 `Marshal`、`Unmarshal`、`GetJsonType`、`Interface2String`、`JsonRawMessageToString`、`Any2Type`、`RawMessage`、`LogError`——`common`、`logger` 的大部分是**直接换包名**即可。但实测以下 **3 个符号 `kitutil` 未提供**，`dto` 又确实在用，必须逐项给出路（这是阶段 2 开工前必须补齐的分析，当前文档尚未给出）：

- `common.JsonDelim`、`common.NewJsonDecoderUseNumber`、`common.DecodeJsonFromDecoder`——`dto/openai_request.go:1283-1307`（`UnmarshalJSON` 的流式 decoder 路径，即补丁 2 的机制之一）在用；`kitutil` 无对应导出
- `common.ResponsesRequestMetadata`——`dto/opaque_openai_responses_request.go` 在用；`kitutil` 无对应导出
- `constant.ChannelType*` / `constant.EndpointType`——`constant` 是宿主包且**不在** `kitDirs` 内，其类型本身可迁 `relaykit/types`，但 `dto` 对 `constant` 的 import 无论如何都要断掉（`dto/pricing.go:11` 的 `[]constant.EndpointType` 上游已改为 `[]types.EndpointType`，可照抄）

对照上游做法：上游 `relaykit/dto` 的 35 个文件对宿主与 gin 零 import，靠的正是把这些符号在迁入前全部归位（或改写实现），而非加豁免。因此**阶段 2 必须新增一条动作：清空 `dto` 的全部 `common` / `constant` / `logger` / gin import（前者换 `kitutil`，`constant.EndpointType` 换 `types.EndpointType`，其余 3 个 `kitutil` 未提供的符号另行归位），并把 `boundary_test.go` 通过作为阶段 2 的完成判据之一**——它比「两个 module 均可编译」更严，因为 import 不合规时编译仍可能通过，只有该测试会拦下。

**阶段 2 的前置：基线测试的 import 不在 327 内**

第 18 轮新建的 `service/convert_golden_test.go` import 宿主 `dto`、`constant`、`types`。**它不在「327 个 import 点」的基线口径内**——实测 `dbfbc096f` 的 327 = **205 个非测试文件 + 122 个测试文件**，而该文件在基线上**不存在**（`git cat-file -e dbfbc096f:service/convert_golden_test.go` 报 does not exist）。工作区实测已漂到 **341**（含测试）。

因此阶段 2 迁走 `dto` 后，这个测试会**编译失败**，而阶段 2 的判据「327 个 import 点全部验证」**恰好以那个旧数字为界，覆盖不到它**——判据会绿，而测试是红的。这与第十一轮 `boundary_test.go`、第十九轮基线生命周期属同一类：**判据的覆盖范围必须包含新引入的产物**。

**阶段 2 须一并处理**：

1. 把 `convert_golden_test.go` 的 3 个 import 改到 `relaykit/dto`、`relaykit/types`（实际用到 4 个：`ChannelTypeAnthropic`、`ChannelTypeOpenAI`、`ChannelTypeGemini`、`ChannelTypeDeepSeek`；随 12.0.4 的归属一并处理）。
2. **8 个快照内容应保持不变**：本轮已核 12.0.8 判为「空操作」的那 3 处 tag 差异（`Delta`、`Function`、`GenerationConfig`）在本仓库对应字段确为 **struct 值**——`dto/openai_response.go:86` 的 `Delta` 是 `ChatCompletionsStreamResponseChoiceDelta`（值类型，非指针）、`dto/openai_request.go:240` 的 `Function` 是 `FunctionRequest` 值类型。故 `omitempty` 对其无效，改 tag 不改变输出。**若阶段 2 后快照发生漂移，那不是 tag 差异造成的，须查字段差异对齐或实现改动。**
3. 阶段 2 判据追加「`TestGolden` 仍绿」。

**口径提醒**：`327` 与工作区的 `341` 差 14 个文件——其中含本轮新增的 `service/convert_golden_test.go`，也有其他会话的在途文件（如 `service/convert_effort_test.go`），**不全是本轮新增**。**凡引用「327」处都是基线口径（含测试）**，工作区数字须另行标注，不要混用。

**阶段 2 的前置：12.5 的 3 个「需实测」方法必须在本阶段开工前定分类（第 32 轮补充；第 42 轮已定案关闭）**

**第 42 轮结论：三项均判 A（迁入 `relaykit/dto`），本前置已关闭。** 取证见 12.5 表与该节末段：三方法 relaykit 全仓零命中，方法体只引用 relaykit/dto 也有的字段，迁入不新增 `boundary_test.go` 违规；`EnsureResponsesTokenDetailObjects` 的最近邻 `UsageFromChatUsage` 有三处语义差、`HasImageGenerationCall` 原引的 `ImageGenerationCallCounter.Observe` 是上游**宿主**的有状态计数器（不同能力、且本地无该文件）。下表与末段保留为**取证记录**（第 32 轮的「待比对」措辞已被 12.5 的定案取代）。

12.5 的方法归属表里曾有 **3 行不是 A/B/C 而是「需实测」**，它们**此前没有任何阶段或处置项认领**——阶段 2 判据只写「全部宿主独有方法给出出路」，处置清单第 6 项只覆盖 9.1/9.2/9.2.1/9.2.3 的语义差异（9.2.2 归第 17 项）（**第 40 轮修正**：原文写「只覆盖 9.1/9.2」，第 37/38 轮已扩到 9.2.1–9.2.3），都不指向这 3 行。这与第 25 轮「门控留宿主但调用点在被删文件里」同型：**一个已知未知项，没有 owner，就会一路滑到开工当天**。

| 方法 | 待比对的 relaykit 侧 | 第 42 轮定案 |
|---|---|---|
| `Usage.EnsureResponsesTokenDetailObjects` | `relayconvert.UsageFromChatUsage`（`relaykit/relayconvert/internal/oai_chat/to_oai_responses_resp.go:146`）是最近邻，但三处语义差（非零才设 `InputTokensDetails`、不设 `OutputTokensDetails`、新建而非就地填充） | **A（迁入）**，条件结论（见下） |
| `Usage.GetOutputTokenDetails` | relaykit 无等价物（全仓零命中） | **A（迁入）** |
| `OpenAIResponsesResponse.HasImageGenerationCall` | 原引上游**宿主** `relay/common/tool_usage.go` 的 `ImageGenerationCallCounter.Observe`——有状态计费去重计数器，与协议层无状态谓词不同能力，且本地无该文件 | **A（迁入）** |

**定案依据**：逐项读两侧实现比对语义（读 relaykit 源码 + 本地方法体 + 调用点），三项均无 relaykit 等价物，写回 12.5 表。阶段 2 判据「全部宿主独有方法给出出路」随之可判。

**其中 1 个方法在 `HEAD` 上不存在（第 32 轮实测）**：`Usage.EnsureResponsesTokenDetailObjects` 在 `HEAD` 的 `dto/` 里**没有定义**（`git grep` 命中 0），只存在于工作区（`dto/openai_response.go:305`，工作区口径），其 4 个调用点（`service/responses_synthetic_compact.go:1102,1116`、`service/openaicompat/chat_to_responses_response.go:43`、`relay/channel/openai/responses_via_chat.go:63`——**均为工作区口径**，`HEAD` 上这 4 行是别的代码）在 `HEAD` 上同样为 0。这是其他会话的未提交改动。第 214 行已记过「HEAD 与工作区差 3 个方法」，但 **12.5 表没有引用那条提示**，其「生产引用 3」是从工作区数的。**故这 3 行里有 1 行的分类对象本身尚未进入版本控制**——迁移前须与该会话确认最终形态，否则阶段 2 会对着一个不存在的符号做分类。

**另 2 个工作区独有方法同样只在工作区存在**：`ResponsesStreamResponse.GetOpenAIError`（12.5 表中列为 A 类，9 处引用）与 `ChatCompletionsStreamResponse.GetError`（列为 A 类，1 处引用），二者在 `HEAD` 上均为 0 定义。**这 3 个方法构成同一个待确认集合**，不是 3 个独立问题。

**阶段 0 的 unshallow：当前 `.git/shallow` 截断于 `9724ef1b2`，导致 `git merge-base HEAD upstream/main` 返回空、三点 diff 不可用。但**真实共同祖先存在**——`9724ef1b2` 的提交对象记录 `parent aa7d0d39a4`，而 `merge-base(HEAD, aa7d0d39a4)` = `dac55f0f`。空结果纯属浅克隆遮蔽。两点 diff（`git diff upstream/main HEAD`）当前可用，但它比较两棵树终态，无法回答「本地相对共同祖先改了什么」，所以 unshallow 仍是必要前置。

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

前 5 条都持有 `info`，是首选落点。第 6 条（AWS Converse）有独立于 `service/convert.go` 的 thinking 生成逻辑，**需单独处理——该条已在 12.4.3 处置完毕**：它**无条件**暴露 thinking（`:748`、`:1110` 直接构造 `{Type:"thinking"}`，全文件 0 处调用 `shouldExposeReasoningAsClaudeThinking`），**套用 DeepSeek-only 门控会新抑制 Bedrock reasoning，是行为回归**，故决定**维持现状**。**阶段 4 不得对它施加门控**，阶段 6 删除 `service/convert.go` 主体时也不得连带删除它的 thinking 生成逻辑。

**本节只覆盖出向（上游响应 → `dto.ClaudeResponse`）。** 入向（客户端请求 → 上游格式）另有一条**通道级**路径不在本表内，且不被 `service/convert.go` 承载——见 4.5.5「通道级转换器清单」：`relay/channel/claude/relay-claude.go:255 RequestOpenAI2ClaudeMessage`（459 行）由 claude / aws / vertex **三个通道**共用，与 relaykit 的 `OpenAIChatRequestToClaudeMessages` 重复。第 12 轮补记；**第 16 轮已定收敛方案，见 12.15.4**。

## 8.1 回滚与止损点

本文档前 7 节讲「怎么做成」，本节讲「做不成时怎么办」。这是 6 个阶段里唯一涉及生产行为切换（阶段 4/5）的迁移，必须有中途止损点。

**分层止损**（前两层是本迁移特有的，第三层复用既有门禁）：

| 层 | 触发 | 动作 |
|---|---|---|
| **阶段内** | 任一阶段的完成判据不满足 | 该阶段**不合并**。阶段 1–3 纯属「新旧并存 + 编译期切换」，不改变任何运行时行为，失败即丢弃分支重来，代价可控 |
| **阶段间** | 阶段 4 或 5 合并后发现回归 | **阶段 4 与 5 必须各自独立成一次合并**，不得压成一次。回归发生在阶段 4 就回滚到阶段 3 的末态——此时 `service/convert.go` 主体仍在，协议映射可切回本地实现，不需要 revert 阶段 2 的 327 个 import |
| **发布** | 阶段 6 完成后上线 | 复用 `docs/operations/new-api-production-release-gates.md` 的门禁 E（灰度）与门禁 F（回滚）。本迁移**不新增发布流程**，也不改变该文档的停止条件 |

**关键设计约束**：阶段 4「协议映射切换到 relaykit」必须是**单点可逆**的。即，切换动作收敛为一个可回退的开关或一次集中的函数体替换，而不是散落在 6 条响应路径（第 8 节）里的多点改写。若做不到单点可逆，则阶段 4 不能合并。

**第 33 轮实测：这条约束与 12.3 / 12.16.2 的动作序列存在张力，须在阶段 4 开工前定一次切换形态。** 12.3 决定**删除** 4 个 `*WithError` 与 4 个无 error 旧包装、不保留包装层；12.16.2 决定 1 要求把 **8 个调用点**（6 个函数；**其中 Claude 响应方向的 5 个生产调用点**分布在 **4 个文件**：`relay-openai.go:314`、`openai/helper.go:46,246`、`openai/chat_via_responses.go:280`、`gemini/relay-gemini.go:1784`——**回滚要 revert 这 4 个文件**；另 3 个在 `convert_golden_test.go`）**逐个改写**为 relaykit 入口。**「删除包装层 + 逐点改写」本身是多点**，与本节要求的「单点」相反。两者只能同时成立于一种形态：**阶段 4 先落一个宿主侧薄封装（同名同签名的转发函数，内部 `return relayconvert.Xxx(...)`），8 个调用点一行不改；确认全链路绿之后再于阶段 6 删除该封装并把调用点改写。** 本节据此把「单点可逆」的实现形态定死：

| 形态 | 阶段 4 | 阶段 6 | 是否满足本节 |
|---|---|---|---|
| **薄封装（推荐，须写入阶段 4 动作）** | 新增 `service` 内 6 个同名转发函数，调用点零改动 | 删封装 + 改写 8 处调用点 | 是——阶段 4 的回滚只需删掉 6 个新函数 |
| 直接改写调用点（12.3/12.16.2 的字面表述） | 8 处逐个改 | 无事可做 | **否**——回滚要同时 revert 4 个生产文件 |
| **照抄上游终态（第 35 轮新增；第 44 轮定案；详见 12.20/12.20.1）** | `service/convert.go` 5 个 facade 改函数体 + 新增 request/response 两个包装文件 + `conversion_diagnostics.go` + `convert_request_error.go` + `log_info_generate.go` 诊断落盘 + 流式 6 处构造 `ResponseStreamState`；调用点按 12.20.1 分族处理 | 无（facade 与包装层即终态，不删） | 是——回滚 = revert 5 个既有文件 + 删 4 个新文件 + revert 生产改写 18 处 + revert 测试侧 50 处（完整清单见 12.20.1 末段；**第 49 轮订正**：原写「revert 2 个既有文件 + 删 5 个新文件」，把 C7 的既有 `log_info_generate.go` 算成新文件） |

**本节不强制薄封装**（它与 12.3「不保留包装层」的意图相左，且多一层间接），但**必须在阶段 4 开工前选定唯一形态并写进处置清单**——现状是两条都写着、都没被指定为唯一形态，回滚路径因此实际不存在。**阶段 4 判据须增一条**：若选薄封装，判据为「调用点零改动且全链路绿」；若选直接改写，判据须包含「revert 该 4 个生产文件后 `go build ./...` 与全量测试仍绿」——**否则「单点可逆」只是一句无人验证的断言**。

**第 44 轮定案：选定形态 C（照抄上游终态）。** 上表「薄封装」行与「直接改写」行自此**不再适用**，仅留作选型对照。三行选型对照中 C 在「回滚点最集中」（revert 5 个既有文件 + 删 4 个新文件 + revert 生产改写 18 处 + revert 测试侧 50 处；**第 49 轮订正**：C7 的 `log_info_generate.go` 与 C4 的 `relay_info.go` 是既有文件）、「与上游零分叉」（`relaykit/VERSION` 跟进时宿主形态一致）、「测试侧改动面最小」（50 处 vs 全删形态 82 处）三个判据上同时占优——薄封装只在「阶段 4 回滚」一个维度占优，且它多一层间接、阶段 6 还要再改写一次（终态与上游不同，跟进成本是持续性的）。定案后 §8.1 的「单点可逆」按 C 的具体形态执行：**阶段 4 的切换 = `service/convert.go` 单文件的函数体替换（**注意：不是纯 27 行**，还含本地保留的 `IsDeepSeekModelName`/`HasDeepSeekModelName`，且 `stopReasonOpenAI2ClaudeStrict` 须在改写前迁出，见 C1 行第 46 轮修正）+ 4 个新增文件 + 5 个既有文件 + 生产改写 18 处**（清单见 12.20.1 末段），回滚 = 删 4 个新文件 + revert 5 个既有文件 + revert 生产改写 18 处 + revert 测试侧 50 处（完整清单见 12.20.1 末段「第 46 轮补」；**第 49 轮订正**：原写「删 5 个新文件 + revert 2 个既有文件」）。阶段 4 判据按上表 C 行执行，处置清单第 14 项已改为「C 已定案」。

**第 35 轮实测补充（见 12.20）：形态选择扩为三选一，新增选项 C——照抄上游终态。** 上游 `main` 的实际形态是：`service/convert.go` 保留 **27 行、5 个同名 facade**（`NormalizeCacheCreationSplit` + 4 个非 WithError 包装，旧签名不变、一行转发 `relayconvert.*`）；`ClaudeToOpenAIRequest`/`GeminiToOpenAIRequest` 保留为 shim（`upstream/main:service/request_converter.go:50`/`:62`）；`*WithError` 删除、9 个生产调用点改指 `service.ConvertResponse`/`ConvertStreamResponse` 包装；通道级 4 个函数同样保为一行 facade（`upstream/main:relay/channel/claude/relay-claude.go:37`/`:41`、`upstream/main:relay/channel/gemini/relay-gemini.go:139`/`:143`）。**推荐 C**，理由三条：与上游零分叉（`relaykit/VERSION` 跟进时宿主形态一致）、调用点改动面最小（测试侧 `service/` 4 文件必改 17 处（全为 `*WithError`；`IsDeepSeekModelName` 2 处调用点零改动），而非全删形态下同样 4 个文件的 82 处；**通道级 4 个测试文件在 C 下另必改 33 处（HEAD 口径 41 处中 16+11+6，8 处经 facade 零改动；**第 48 轮 22 → 33**）**——C 的测试侧改动面 = 17+33 = 50 处，`service/` 与通道级两组不可混加（处置清单第 16 项同段的「基线 33 处」指通道级 4 文件在基线口径的调用点总数（33，2 个文件））。**第 40 轮修正**：原文写「19 处」，把 2 处零改动调用算进必改、且漏了通道级 22 处。详见处置清单第 16 项）、生产侧请求方向 5 处零改动）、回滚点最集中（阶段 4 的切换 = `service/convert.go` 单文件的函数体替换 + 两个新包装文件，revert 即回滚；**完整回滚清单见 12.20.1 末段**——生产侧还有 8 处改指须一并 revert，第 40 轮修正）。**注意上表「薄封装」行里的 6 个同名转发函数与上游实际保留的 5+2 个不同**——那 6 个含 `*WithError` 名字，而上游恰恰删掉了这些名字；选 C 时以 12.20 的表为准。C 的代价是 `service/convert.go` 文件保留（27 行），与 12.3 原标题「不保留包装层」的字面冲突已在 12.3（第 35 轮修正）里处理。

**为什么阶段 2 的回滚代价可以接受**：阶段 2 只改 import 路径与类型对齐，`dto` 包的类型在迁移前后**语义一致**（33 个字段差异在阶段 2 内逐项对齐完毕，见 12.0.1）。这意味着阶段 2 的产物是「同一个包换了个位置」，出问题可以整体 revert，不会留下半迁移状态。

**已知不可逆项**：无。本迁移不涉及数据库 schema 变更、不涉及数据回填、不改变持久化格式——`relaykit` 只承载协议转换的纯函数与状态机。因此回滚不需要独立的数据恢复方案（与门禁 F 提到的「Schema 或数据变更」不同）。

**明确不做的事**：不引入双跑（新旧转换器同时执行并比对输出）。理由：转换是纯函数，双跑的比对结果等价于 golden 基线测试（12.16 已要求补建本地基线），却要在生产路径上付出双倍 CPU 与一份差异日志的维护成本。用测试覆盖等价性，不用生产双跑。

## 9. 验证策略

### 9.1 阶段 4 的语义对齐清单

**本清单只覆盖行为语义；结构语义见 12.0.6 与 12.0.8**——同名字段的 JSON tag 差异（5 处，其中 3 处因 struct 值而 `omitempty` 无效、2 处为真实变化）与 Go 类型差异（6 处，3 处编译期破坏）。**11 处**（tag 5 + 类型 6；**第 30 轮修正**，原文写 15）**全部采用 relaykit**，逐项决定见 12.0.8。

本地与 relaykit **不是逐一等价**，切换前须逐项决定「保留、放弃或改写」，并同步调整锁定的测试：

| # | 差异 | 本地 | relaykit |
|---|---|---|---|
| 1 | finish_reason 严格语义（**第 29 轮修正为 5 条**） | `stopReasonOpenAI2ClaudeStrict`（`service/convert.go:1125`）对未知值报错；另含 `""`→`end_turn`、`ToLower`+`TrimSpace` 归一、`function_call`→`tool_use` 三条 | `reasonmap.OpenAIFinishReasonToClaudeStopReason`（`relaykit/reasonmap/reasonmap.go:31`）的 `default: return finishReason` 原样透传：未知值透传、`""` 透传成空、**无 `TrimSpace`**、**无 `function_call` 分支**。`content_filter` 两侧同为 `refusal`，无差异。逐条差异与 `stop_reason` 被 `omitempty` 整字段省略的影响见 12.11.2；落地选项见 12.11.3 |
| 2 | 多 choice | `ResponseOpenAI2ClaudeWithError` 在 `len(Choices) > 1` 时报错 | 遍历所有 choice 并拼接 |
| 3 | `Message.ThinkingContent` | `dto/openai_request.go:407` 有该字段，`GetReasoningContent()` 三级回退 | relaykit `Message` 无 `ThinkingContent`，两级回退 |
| 4 | `ClaudeConvertInfo` 字段 | 见第 7 节的四处硬依赖 | 字段名与类型均不同 |
| 5 | `ResponsesStreamResponse` 形状 | `dto/openai_response.go:450` 有 `Error`、`Text`、`Arguments`、`CallId`、`Item` | `CallId` 在 `ResponsesOutput` 内而非事件顶层 |
| 6 | 本地 DTO 独有方法 | 见 4.4（23 个，HEAD 20 个） | 无对应；反向有 5 个 relaykit 独有方法（**三文件口径**；全包 31 个，见 12.0.5） |
| 7 | **同一 chunk 内 reasoning + text（+ tools）** | 三个独立闭包（`appendReasoningDelta` `service/convert.go:793`、`appendTextDelta` `:823`、`appendToolCalls` `:850`，**HEAD 口径**）各自判空、**三者都发**；专门用例 `TestStreamResponseOpenAI2ClaudeEmitsThinkingTextAndToolBlocksFromOneChunk`（`convert_claude_stream_test.go:119`）钉住 | `if reasoning != ""` / `else` **互斥**（`oai_chat/to_claude_messages_resp.go:345`）——同 chunk 有 reasoning 时 **text 被丢弃**；若该 chunk 还含 `tool_calls`，则 `:247` 的 tools 分支优先，reasoning 与 text **都**被丢弃。上游无对应用例 |
| 8 | **oai→gemini 的 finish_reason 严格语义（第 47 轮新增）** | `openAIFinishReasonToGemini`（基线 `service/convert.go:1915`）：`TrimSpace` 后 `""`/`stop`/`tool_calls`/`function_call`→`STOP`、`length`→`MAX_TOKENS`、`content_filter`→`SAFETY`、**未知值报错** | `geminiFinishReason`（`relaykit/relayconvert/internal/oai_chat/to_gemini_chat_resp.go:518`）：`TrimSpace` 后 `length`→`MAX_TOKENS`、`content_filter`→`SAFETY`、**`default`（含未知值、含 `function_call`）→`STOP` 静默改写**。**性质：严格性丢失 + 语义变化**（未知值从报错变成「正常结束」）。**relaykit 侧有两条独立路径**：非流式 `ResponseOpenAI2Gemini` 经 `geminiFinishReason`（`:300`），流式 `StreamResponseOpenAI2Gemini` 内联一个 5 分支 switch（`:176-186`，`stop`/`length`/`content_filter`/`tool_calls` 显式 + `default`→`STOP`）——**两条路径的映射结果一致，但代码是两份**，改一处不会同步另一处。**此前整个 §9 无落点**——9.1 第 1 条只讲 Claude 方向、9.2.3 只讲 gemini→oai 反方向；而 12.20.1 生产表首行的 9 处 `*WithError` 中有 **4 处正是本方向**（`relay-openai.go:324`、`openai/helper.go:63,267`、`chat_via_responses.go:286` 的 `*GeminiWithError`），此前无人认领 |

**第 8 条（第 47 轮补）与第 1 条同型、与第 7 条不同型**：第 1 条是 Claude 方向的 finish_reason 严格校验，第 8 条是 Gemini 方向——**两套本地严格函数、两个 relaykit 宽松映射、两组生产调用点**，但差异性质相同（未知值报错 vs 静默改写）。第 8 条**不是能力差异**（第 7 条才是），故不适用「保留 = 打补丁」那条路径；它的处置与第 1 条共用同一形态（迁入严格函数 + 宿主前置校验），见 12.11.2b 与 12.11.3 第 47 轮补。**两条合起来才是「finish_reason 严格语义」这一维的完整差异面**——此前文档只有第 1 条。

**第 48 轮补——§9 的差异表已覆盖全部有本地转换器的方向**：本表（§9.1）只列**响应**方向；**Gemini 相关的两个请求方向此前无落点**（第 47 轮补了 oai→gemini 响应方向，仍未补请求方向）。第 48 轮新增 **9.2.4（gemini→oai 请求）**与 **9.2.5（oai→gemini 请求）**两节补足——**§9 的差异表现在覆盖了全部含本地实现的转换方向**——此前 4 个方向有差异表（claude→oai 请求/响应、oai→claude 响应、gemini→oai 响应），第 47 轮加 oai→gemini 响应，第 48 轮再加 gemini→oai 请求与 oai→gemini 请求，**凡本仓库有对应本地转换器的方向均有表**（`ClaudeToOpenAIRequest`、`GeminiToOpenAIRequest`、`CovertOpenAI2Gemini`、`ResponseOpenAI2Claude{,WithError}`、`ResponseOpenAI2Gemini{,WithError}`、`StreamResponseOpenAI2Claude{,WithError}`、`StreamResponseOpenAI2Gemini{,WithError}`、通道级 4 facade 与 4 个 `*WithError` 各方向）。**9.2.4/9.2.5 归处置清单第 18 项**（不并入第 6 项的 33 条口径，理由见第 18 项）。

**第 7 条是能力差异，不是严格度差异**：12.16.1 说「阶段 4 的待办不是补用例而是确认既有用例仍通过，切换会让它们失败得其所」——但 `TestStreamResponseOpenAI2ClaudeEmitsThinkingTextAndToolBlocksFromOneChunk` 的失败**不得其所**，它钉的是本地刻意支持的渲染能力被上游丢弃。阶段 4 须对它明确取舍：保留 = 需再打补丁（把上游的互斥 `if/else` 改为顺序执行）；放弃 = 记入行为变更并改写用例。**不得默认放行。**

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
| **工具 input_schema 归一化**（第 28 轮新增） | `relay-claude.go:233` `normalizeClaudeInputSchema`：非 map / 不可 JSON 序列化**报错**；`type` 非 string**报错** | `shared/claude/schema.go:3` `FunctionParametersToInputSchema`：类型断言失败**静默**产出 `{"type":"object","properties":{}}`；`type` 非 string 原样透传。详见 12.15.4.1 |
| **媒体内容块分派**（第 28 轮新增） | 本地分 `text/*`→text 块、`image/*`→image、`application/pdf`→document、其他**报错**；`ContentTypeFile` 有独立分支；`ToFileSource()==nil` **报错** | 上游只分 pdf 前缀→document、**其余全部→image**（含 `text/*`）；无 `ContentTypeFile` 分支；`ToFileSource()==nil` **静默 `continue`**。详见 12.15.4.2 |

### 9.2.1 通道级响应方向（Claude→OpenAI）的错误路径（第 37 轮新增）

9.1/9.2 此前只覆盖 `service/convert.go` 的映射层，**漏掉了 `relay/channel/claude/relay-claude.go` 内通道级响应转换器自己的 8 条错误路径**（它们随 4.5.5 的通道级转换器一起被替换，是独立的一套严格性）。本地 `StreamResponseClaude2OpenAIWithError`（基线 `:719`，函数体 `:719-822`）与 `ResponseClaude2OpenAIWithError`（`:836`，函数体 `:836-905`）逐条对 `relaykit/relayconvert/internal/claude_messages/to_oai_chat_resp.go:34`（`StreamResponseClaude2OpenAI`）与 `relaykit/relayconvert/internal/claude_messages/to_oai_chat_resp.go:218`（`ResponseClaude2OpenAI`）：

| # | 条件 | 本地（基线行号） | relaykit | 性质 |
|---|---|---|---|---|
| 1 | 流式事件 type 未知 | `:820` 报错 `stream event type %q is not supported` | `:112` 的 `else { return nil }`——**静默丢弃**（`:110` 是 `message_stop` 的正常丢弃） | 严格性丢失（`TestStreamResponseClaude2OpenAIRejectsUnknownEvent` 钉住） |
| 2 | content block type 未知 | `:768` 报错 | `:73` 的 `else { return nil }` 静默丢弃 | 同上 |
| 3 | delta type 未知 | `:799` 报错 | `:87-98` 的 switch 无 default——静默忽略 | 同上 |
| 4 | tool_use 块缺 id 或 name | `:751` 报错 | `:64` 直接取 `ContentBlock.Id`/`Name`，**无校验**（产出空 id/name 的 tool_call） | 严格性丢失（`TestStreamResponseClaude2OpenAIRejectsIncompleteToolBlock`） |
| 5 | `input_json_delta` 缺 `PartialJson` | `:779` 报错 | `:84` 直接解引用 `*claudeResponse.Delta.PartialJson`——**nil pointer panic**（非流式的 `ConvertChunk` 在 `:188` 有守卫，流式 facade 没有） | 本地报错 → 上游 panic |
| 6 | `content_block_start` 缺 content_block | `:771` 报错 | `:73` 静默丢弃 | 严格性丢失 |
| 7 | `text_delta` 缺 text | `:796` 报错 | 无该分支（`text_delta` 落进 `:87-98` 的无 default switch） | 严格性丢失 |
| 8 | `redacted_thinking` 不可表示为 reasoning_content | `:766`/`:874` 报错 | **无 `redacted_thinking` 概念**——落进未知 type 的静默路径 | 严格性丢失 |
| 9 | `signature_delta` | `:788-791` **返回 `nil, nil`**（不产出任何 chunk） | `:87-88` 产出 `ReasoningContent = "\n"`——**向客户端注入一个换行** | **行为变化**（本地丢弃 vs 上游注入换行）；本地**专门用例 `TestStreamResponseClaude2OpenAIIgnoresOpaqueSignatureDelta`**（`relay/channel/claude/relay_claude_test.go:676`，断言 `require.Nil(t, response)`）钉在本地语义上——**第 38 轮修正：初稿把它错写成 `HandlesMissingPartialJSON`（那是第 5 条的用例）与 `RejectsIncompleteToolBlock`（第 4 条）** |

**为何单列**：这 9 条与 9.1/9.2 的 15 条**不重叠**（后者的行号全在 `service/convert.go` 与 `relay-claude.go` 的请求侧），且它们**全部是错误路径或边缘路径**，golden 基线一条都测不到。第 9 条尤其危险——它不是「严格性丢失」而是**输出内容变化**（`signature_delta` 在真实 Claude 流里每次 thinking 块结束都会出现），且本地已有测试钉住相反语义，切换后该测试会红且**不得其所**。阶段 4 须为这 9 条逐条决定「保留（打补丁）/ 放弃（记行为变更并改写用例）」。**第 5 条须优先处理**：panic 是可用性事故，不是行为差异。

### 9.2.2 反方向 stop_reason 严格校验同样丢失（第 37 轮新增）

9.1 第 1 条只写了 **OpenAI→Claude** 方向的 `finish_reason` 严格校验（`stopReasonOpenAI2ClaudeStrict`）。**反方向 Claude→OpenAI 有一套同型但独立的严格校验，此前完全未被盘点**：

| 本地 | relaykit | 差异 |
|---|---|---|
| `stopReasonClaude2OpenAIStrict`（基线 `relay/channel/claude/relay-claude.go:41`，两个调用点 `:806`（流式）、`:889`（非流式）） | `StopReasonClaudeToOpenAI`（`relaykit/relayconvert/internal/claude_messages/to_oai_chat_resp.go:30`，转调 `relaykit/reasonmap/reasonmap.go:9` 的 `ClaudeStopReasonToOpenAIFinishReason`，调用点 `relaykit/relayconvert/internal/claude_messages/to_oai_chat_resp.go:104`、`relaykit/relayconvert/internal/claude_messages/to_oai_chat_resp.go:270`） | 逐条见下 |

| # | 输入 | 本地严格函数 | relaykit | 性质 |
|---|---|---|---|---|
| a | 未知值 | **报错** `Claude stop_reason %q is not supported` | `default: return stopReason` 原样透传 | 严格度差异 |
| b | `""` | `"stop"` | `default` → `""`（`omitempty` 可能整字段省略） | 行为回归 |
| c | `" END_TURN "`（首尾空白） | `ToLower`+`TrimSpace` 后匹配 | 仅 `ToLower`，**无 `TrimSpace`** → 落 `default` 透传 | 行为回归（边界输入） |
| d | `model_context_window_exceeded` | `"length"`（`constant.FinishReasonLength`） | **无该分支** → `default` 透传原串 | 行为回归（合法值被透传成非法 finish_reason） |
| e | `pause_turn` | `"stop"` | `"length"` | **行为差异（值不同）**：本地注释说「Chat Completions 无对应生命周期，stop 最不丢失」；上游注释说「Responses 无 pause_turn，视为 incomplete」。**两侧结论相反**（stop vs length），是真实分歧，**须显式裁定** |
| f | `end_turn`/`stop_sequence`/`max_tokens`/`tool_use`/`refusal` | 经 `reasonmap` 映射 | 同 | 无差异 |

**为何单列**：与 9.1 第 1 条是**两个方向的两套函数**（`stopReasonOpenAI2ClaudeStrict` vs `stopReasonClaude2OpenAIStrict`），本迁移两套都换入 relaykit，故两套都会丢严格性。阶段 4 须为 a–d 各决定取舍并补测试（现状：**无任何测试覆盖 `stopReasonClaude2OpenAIStrict`**，`git grep` 全仓仅定义行 + 两个调用点）。**e 的 stop/length 分歧须显式裁定**，不能默认采用 relaykit 的 `length`——`pause_turn` 在本地是 `stop`，改变它会让客户端把「可续跑的 agent 轮次」误判为「被截断」。**第 44 轮修正**：原文写「若采用 12.11.3 的 A1（改 `oai_chat` 3 个函数签名），本方向可一并纳入同一钩子机制」——**A1 已于第 44 轮被排除（12.11.3 定案为 B），本方向不再有「同一钩子」可搭**。B 只覆盖 OpenAI→Claude 方向（5 个 Claude 响应方向调用点）；**本反方向须独立处理**——同样形态：把 `stopReasonClaude2OpenAIStrict`（基线 `relay/channel/claude/relay-claude.go:41`）迁出随阶段 4/6 收敛的通道文件、放入宿主共享助手，在调 relaykit 前校验并归一到 Claude 词汇表。a–d 与 e 的裁定归处置清单第 17 项（**第 46 轮修正：原写「仍在第 6 项内」与第 17 项矛盾——9.2.2 的 6 条已独立为第 17 项，第 6 项只管 9.1/9.2/9.2.1/9.2.3 共 33 条条件**），随批 3 判定一并做。

### 9.2.3 Gemini 方向的通道级错误路径与 ID 语义（第 38 轮新增）

9.2.1 只盘了 **Claude** 方向的通道级响应错误路径。**Gemini 方向有结构相同的第二套，此前完全未盘**。本地 `responseGeminiChat2OpenAIWithError`（基线 `relay/channel/gemini/relay-gemini.go:1281`，非流式）与 `streamResponseGeminiChat2OpenAIWithError`（`relay/channel/gemini/relay-gemini.go:1430`，流式）对 `relaykit/relayconvert/internal/gemini_chat/to_oai_chat_resp.go:81`（`ResponseGeminiChat2OpenAI`）与 `:188`（`StreamResponseGeminiChat2OpenAI`）：

| # | 条件 | 本地（基线行号） | relaykit | 性质 |
|---|---|---|---|---|
| 1 | 响应为 nil | `:1283`/`:1432` 报错 | `:86`（非流式 `len(response.Candidates)`）/`:189`（流式同）——**nil 解引用 panic** | 本地报错 → 上游 panic |
| 2 | 未知 `finishReason` | `geminiFinishReasonToOpenAI`（`:1188`）`:1198` **报错**，两处调用点 `:1366`（非流式）、`:1453`（流式） | 非流式 `:174-175`、流式 `:227-228` 的 `default` **一律归为 `FinishReasonContentFilter`**——静默改写而非报错 | 严格性丢失 + **语义变化**（未知值变成「内容被过滤」） |
| 3 | `part` 含多个内容字段 / thought 与非文本混排 / 无受支持内容 / `fileData` / `functionResponse` | `validateGeminiResponsePartForOpenAI`（`:1386`）的 5 条报错 `:1410`/`:1413`/`:1419`/`:1422`/`:1425`，调用点 `:1308`（非流式）、`:1460`（流式） | **无对应校验**：`fileData` 与 `functionResponse` 落进 `else` 分支被静默忽略；多字段 part 按首个命中分支处理 | 严格性丢失 |
| 4 | `inlineData` 缺 `mimeType`/`data` | `:1313` 报错 | 无校验（`strings.HasPrefix(part.InlineData.MimeType, "image")` 对空串为 false） | 严格性丢失 |
| 5a | `inlineData` 的 MIME 非 image（**非流式**） | `:1322` **报错** `cannot be represented by OpenAI Chat Completions` | `:128` 的 `else` **降级为文本** `[media](data:<mime>;base64,...)` | **行为变化**（报错 vs 产出可读文本）；这是 relaykit 的**有意设计**，非缺陷 |
| 5b | `inlineData` 的 MIME 非 image（**流式**） | `:1468` **报错**（同 5a） | **无 `else` 分支**——该 part 被**静默丢弃**，chunk 里既不产出 image 也不产出 `[media]` | **行为变化**（报错 vs 静默丢失内容）；与 5a 不是同一处置，须分别裁定（**第 38 轮拆分**：原文只写 `:128` 的 `[media]` 降级，未区分流式侧无此分支） |
| 6 | `functionCall` 缺 name | `getResponseToolCallWithError`（`:1147`）`:1152` 报错 `name is required` | `geminiResponseToolCall`（`:722`）**不校验 name**（`:723-726` 只对 `Marshal` 失败返回 nil） | 严格性丢失 |
| 7 | `functionCall` arguments 非 JSON object / 编码失败 | `:1161`/`:1164` 报错 | `:724-726` `Marshal` 失败即 `return nil`（调用方 `:136`/`:243` 静默跳过该 part） | 严格性丢失 |
| 8 | **合成 tool call ID 的确定性** | 确定性：`call_gemini_<candidate>_<ordinal>`（基线 `:1326`（非流式）、`:1472`（流式）），**同一输入两次调用得到同一 ID** | `:728-729` **随机 UUID** `call_<uuid>`——同一输入两次调用得到不同 ID | **行为变化**，且本地有专门用例钉住（见下） |

**第 8 条须优先裁定**：`relay/channel/gemini/protocol_conversion_test.go:318`（`TestStreamResponseGeminiChat2OpenAIUsesStableGeneratedToolCallIDs`）断言「同一输入两次调用 ID 相同」，而 relaykit 的 UUID 方案**必然让它失败**；这不是「严格性取舍」而是**本地刻意保证的幂等性被丢弃**（客户端重放/重试时工具调用 ID 漂移）。本地另有两个用例（`relay/channel/gemini/tool_call_id_test.go`，基线**不存在**，由 `4dee3b71b` 引入、**上游无此修复**）钉「不同 chunk 的调用不得共用 ID」「同 chunk 内两个调用 ID 必须不同」「上游已给的 ID 原样透传」——relaykit 的 UUID 方案恰好也满足这三条，**但破坏确定性那一条**。

**为何单列**：9.2.1 与本节是**两个通道、两套独立的错误路径**（Claude 侧 9 条：8 条错误路径 + 1 条输出变化；Gemini 侧 8 条条件——**第 38 轮把第 5 条拆为 5a/5b**，因为流式侧无 `else` 分支、与非流式不是同一处置，故**实际裁定点为 9 个**，全文「8 条」均指条件数），同属 4.5.5 那批通道级转换器被替换时丢失的严格性（第 48 轮口径：9 个函数 → 10 个，见 4.5.5 表头）。§9.2.1 的 Claude 侧与本节 Gemini 侧**都有 nil 解引用 panic（各 1 条）**，两处都须在阶段 4 优先处理。阶段 4 须为这 8 条（第 5 条拆分后为 9 个裁定点）逐条决定「保留（打补丁）/ 放弃（记行为变更并改写用例）」。

### 9.2.4 Gemini 请求方向的入口转换器严格性（第 48 轮新增）

**本节补的是 §9 的方向空洞**：§9 此前有 6 张差异表、覆盖 4 个转换方向（claude→oai 请求/响应、oai→claude 请求+响应、gemini→oai 响应），**两条涉及 Gemini 的请求方向一直没有落点**。第 47 轮补了 oai→gemini 的**响应**方向（9.1 第 8 条、12.11.2b），但仍缺两条**请求**方向。本节的 gemini→oai 请求方向与 9.2.5 的 oai→gemini 请求方向合起来把它补全（补后 §9 共 8 张表、凡有本地转换器的方向均有表）。

**gemini→oai 请求方向**：本地 `GeminiToOpenAIRequest`（基线 `service/convert.go:1248`）及其两个私有助手 `validateGeminiRequestPartForOpenAI`（`:1600`）、`convertGeminiRoleToOpenAI`（`:1652`）对 `relaykit/relayconvert/internal/gemini_chat/to_oai_chat_req.go:18` 的 `GeminiGenerateContentRequestToOpenAIChat`（377 行）。本地 `GeminiToOpenAIRequest` 函数体内含 **35 个 `fmt.Errorf` 错误点**（第 48 轮实测，函数体 `:1248-1599`；其中 `:1250-1346` 段 11 条、`:1347-1599` 段 24 条），上游该文件**除 reasoning 一族（`reasoning.AsClientError`）外，函数体内 `fmt.Errorf`/`errors.New` 计数为 0**——即本方向的输入校验被整体丢弃：

| # | 条件 | 本地（基线行号） | relaykit | 性质 |
|---|---|---|---|---|
| 1 | `geminiRequest == nil` / `info == nil` | `:1250`/`:1253` 报错 | 入口无此校验 | 严格性丢失（nil 相关） |
| 2 | 未知 `content.Role` | `convertGeminiRoleToOpenAI`（`:1652`）**报错** `role %q is not supported by OpenAI Chat Completions`（`:1665`）；空串**显式**归 `user` 且注释声明「never reinterpret an unknown role as user」 | `convertGeminiRoleToOpenAI`（`:356`）`default: return "user"`——**未知 role 静默重解释为 user**（且 `switch` 未 `TrimSpace`，`" model "` 落 `default` 也变 user） | 严格性丢失 + **静默重解释**（本地注释明写不得如此） |
| 3 | 单个 part 含多个内容字段 / `thought` 与非文本混排 / `thoughtSignature`/`mediaResolution`/`videoMetadata` 非空 / `functionResponse` 的 `willContinue`/`scheduling`/`parts` 非空 | `validateGeminiRequestPartForOpenAI`（`:1600`）的 **8 条报错**（`:1624` multiple content fields、`:1627` thought 混排、`:1630`/`:1633`/`:1636` 三个「cannot be represented」、`:1640`/`:1643`/`:1646` functionResponse 三个元数据字段），调用点 `:1347`（`if err := validate…`） | **无对应校验**：part 按 `if/else-if` 链取首个命中分支，多余字段被忽略；`thought`/`thoughtSignature` 概念不在此文件（`thought` 文本走 reasoning 提取，签名不校验） | 严格性丢失 |
| 4 | `functionCall.ID` 重复 | `:1362` 报错 `id %q is duplicated` | `newGeminiFunctionCallHistory`（`:289`）把已有 ID 收进 `reservedIDs`、`add` 时对**空 ID** 生成 `call_<n>` 兜底，**重复 ID 不报错**（按名匹配容忍乱序） | 严格性丢失（重复 ID 从报错变为容忍） |
| 5 | `functionCall` 缺 name / `functionResponse` 缺 name | `:1366`/`:1409` 报错 | `add`（`:309`）用 `call.FunctionName`（可空）；`match`（`:318`）按 `ID`/`name` 关联，**不校验 name 是否为空** | 严格性丢失 |
| 6 | `functionResponse` 引用未知 `functionCall` ID / name 不匹配 | `:1398`/`:1401`/`:1414` 报错 | `match`（`:318`）找不到时 `newFallbackID` 兜底或按名匹配，**不报错** | 严格性丢失 |
| 7 | 参数越界：`responseModality`（`:1262`）、`speechConfig`（`:1267`）、`imageConfig`（`:1270`）、`responseMimeType`（`:1274`）、`responseJsonSchema` 非 JSON object（`:1292`）等 | 各自报错 | **无**（这些 `GenerationConfig` 字段在此转换器内不被消费，落静默忽略） | 严格性丢失 |
| 8 | 生成参数归一的收窄：`topK` 非负整数（`:1483`）、`stopSequences` ≤ 4（`:1494`）、每项非空（`:1498`）、`candidateCount` 正整数（`:1505`） | 越界报错 | 无校验：`TopK` 原样转 `int`（`relaykit/relayconvert/internal/gemini_chat/to_oai_chat_req.go:157-158`）、`StopSequences` **截断到 4**（同文件 `:163-164`，`min(len, 4)`）、`CandidateCount` 原样映射（同文件 `:166-167`） | 严格性丢失 + **静默截断**（同 9.2.5 的 stop 截断同型） |
| 9 | server-side tool 不可表示（`:1525`）、`functionDeclarations` 解析失败（`:1530`）、函数声明缺 name（`:1534`） | 报错 | tools 段（`:174`）对 `FunctionDeclarations == nil` 直接 `continue`、解析失败仅 `LogSystemError` 后 `continue`、函数名可空 | 严格性丢失（解析失败从报错变为**记日志后静默跳过**） |
| 10 | `systemInstruction.Role` 不支持（`:1563`）、part 不支持（`:1568`）、part 无内容（`:1574`） | 报错 | `SystemInstructions` 段（`:200`）直接取 `extractTextFromGeminiParts` 拼成 system message，**不校验 role、不校验 part 类型** | 严格性丢失 |

**本地有 10 个 `GeminiToOpenAIRequest` 测试（`service/convert_test.go:33/81/102/133/152/383/413/444/453/464`），其中 6 个是 Rejects\*（`:133` 未知 functionResponse ID、`:152` name 不匹配、`:383` malformed tools、`:444` 非 object response schema、`:453` 不可表示 tool config、`:464` functionResponse 元数据）——这 6 条钉的正是本地严格性，切换后必然失败。上游对该转换器（`GeminiGenerateContentRequestToOpenAIChat`）零测试**（`git grep -ln … upstream/main -- '*_test.go'` = 0）。**故本方向的 10 条差异须逐条裁定，且第 2 条（role 静默重解释）优先级最高**——它不是「丢失一条报错」，而是把本地明写「不得如此」的行为改成如此，且**上游同名函数里的注释与本地注释直接冲突**。

### 9.2.5 oai→gemini 请求方向的入口转换器严格性（第 48 轮新增）

**oai→gemini 请求方向**：本地 `CovertOpenAI2Gemini`（基线 `relay/channel/gemini/relay-gemini.go:201`，568 行，**23 个 `fmt.Errorf` 错误点**——第 48 轮实测；初稿写「31」是把整文件计数误当函数体计数，实际函数体 `:201-768` 内为 23）对 `relaykit/relayconvert/internal/oai_chat/to_gemini_chat_req.go:19` 的 `OpenAIChatRequestToGeminiGenerateContent`（379 行，15 个 error 点，其中 8 个是 `extra_body` 一族、1 个 mime type、1 个 `image_config` marshal、1 个函数参数 args、1 个 function response ID marshal）：

| # | 条件 | 本地（基线 `relay-gemini.go` 的**绝对行号**） | relaykit（`to_gemini_chat_req.go`） | 性质 |
|---|---|---|---|---|
| 1 | `stop` 序列超过 5 条 | `:264` **报错** `OpenAI stop supports at most 5 sequences for Gemini, got %d` | `:52-53` **静默截断** `if len(stopSequences) > 5 { stopSequences = stopSequences[:5] }` | 严格性丢失 + 静默截断 |
| 2 | `tools[].type` 不支持 | `:406` 报错 `type %q is not supported by Gemini` | tools 段（`:159-178`）直接取 `tool.Function`，**不校验 type** | 严格性丢失 |
| 3 | `tools[].function.name` 为空 | `:410` 报错 | 函数声明无 name 校验 | 严格性丢失 |
| 4 | `tools[].function.name` 重复 | `:413` 报错 `name %q is duplicated` | 无重名校验 | 严格性丢失 |
| 5 | `tools[].function.parameters` 非 JSON object | `:445` 报错 `parameters must be a JSON object`（`:442` 另有编码失败报错） | `:170` 只做 `CleanFunctionParameters` 清理，**不做 JSON-object 校验** | 严格性丢失 |
| 6 | `systemInstruction` part type 不支持 | `:503` 报错 | system 消息构造（system/developer role 分支 `:200`，`SystemInstructions` 赋值 `:369`）无 part type 校验 | 严格性丢失 |
| 7 | tool response 缺 function name | `:525` 报错 `function name is required for tool call %q` | 无此校验 | 严格性丢失 |
| 8 | `functionResponse` part type 不支持 | `:534` 报错 | 无 part type 校验 | 严格性丢失 |
| 9 | `message.role` 不支持 | `:574` 报错 `role %q is not supported by Gemini` | `:249` 直接把 `message.Role` 透传进 `GeminiChatContent.Role`，**无 role 校验** | 严格性丢失 |
| 10 | `tool_calls[].type` 不支持 / function name 为空 | `:604`/`:608` 报错 | `:253-273` 只对 `call.Function.Arguments` 做 `Unmarshal` 校验（失败报 `:258`），**不校验 type 或 name** | 严格性丢失 |
| 11 | content part 无可用文件源（`:711`）/ mime type 不支持（`:720`） | 两处报错 | **保留**——`:339`/`:343` 同型报错 | 无差异（**这两条与 9.2 第 8 条媒体分派相关，另见 12.15.4.2**） |

**本方向测试面**：本地 `CovertOpenAI2Gemini` 在 `relay/channel/gemini/protocol_conversion_test.go` 有 **11 处调用**，其中 6 处是 Rejects\* 严格性钉板——`RejectsUnresolvableToolResponseName`（`:205`，钉第 7 条）、`RejectsUnknownMessageRole`（`:224`，钉第 9 条）、`RejectsMalformedContentArray`（`:239`）、`RejectsNonTextSystemContent`（`:257`，钉第 6 条）、`RejectsUnsupportedToolType`（`:278`，钉第 2 条）、`RejectsNonTextToolResponseContent`（`:297`，钉第 8 条）——切换后这 6 条必然失败。**上游对 `OpenAIChatRequestToGeminiGenerateContent` 只有 1 个测试**（`upstream/main:relaykit/relayconvert/safety_settings_test.go` 的 `TestOpenAIToGeminiSafetySettings`，测的是 safety settings 透传，与本地这 11 处覆盖的 tools/role/part type/schema 无关）；无 `to_gemini_chat_req_test.go`（只有 `to_gemini_chat_resp_test.go`）。**故本地 11 处钉的严格性在上游侧无一对应**。

**本方向与 9.1 第 8 条（oai→gemini 响应方向 finish_reason）同属「oai→gemini 请求/响应两条方向」**，二者合起来才构成「oai→gemini 这一协议对的完整严格性差异面」。处置上：**第 1 条（stop 截断）与 9.2.4 第 8 条（stopSequences 截断）是同一类静默截断**；本方向 11 条差异**归处置清单第 18 项**（第 48 轮新增，与 9.2.4 的 10 条合为 20 个裁定点独立处置）——**不并入第 9 项**：第 9 项处理的 finish_reason 是响应方向的单字段输出语义，本方向是请求方向的整段入参校验（覆盖 tools/messages/system/role/generationConfig），两者的校验对象与落点都不同，合并会造成「一个处置项管两个方向、两种动作」的过载（第 6 项正是这样过载后才拆出第 17、18 项的）。

### 9.3 命令

**阶段 0（迁移前，单 module）**：运行 AGENTS.md 第 6 节的 `go test ./controller ./model ./relay/common ./relay/helper ./service`。

**阶段 1 起（双 module）**：上述单 module 命令**不再覆盖全部代码**——`types`/`dto` 的测试已随类型迁入 `relaykit/`，根 module 的 `./...` 也不再包含它们。必须分别运行（模板见上游 `upstream/main:makefile:43-49`）：

```bash
# 根 module（排除 relaykit 子目录）
GOWORK=off go test $(GOWORK=off go list -e ./... | grep -vxF "$(GOWORK=off go list -m)")
# relaykit module
cd relaykit && GOWORK=off go test ./...
```

relaykit module 这一条**包含边界门禁**：`relaykit/relayconvert/boundary_test.go` 会断言 `relayconvert`/`dto`/`types`/`reasonmap` 不得 import 宿主包或 gin（`allowedViolations` 为空且约定「只减不增」）。阶段 1 拷贝后该测试即开始生效；阶段 2 迁 `dto` 时必须让它保持绿色，见「阶段 2 的前置：`boundary_test.go` 是阶段 2 的硬门禁」。

`GOWORK=off` 不可省略（见「阶段 1 的前置」：本仓库不引入 `go.work`）。

**阶段 4 专项**：构造含 thinking block 的 Claude 多轮工具调用请求，验证 DeepSeek 不再 400；验证 grok 的 reasoning 可见（当前被丢弃）；**验证响应方向门控已随切换落地**——非 DeepSeek 模型的 reasoning 不产出 thinking 块、且门控在索引分配之前（判据即 4 个门控快照逐字节一致，见 12.4.4 第 1、4 点）；**逐项确认 9.1 第 7 条（同 chunk reasoning+text）与 9.2 第 7、8 条（schema 归一化、媒体分派）的取舍**——后两条的差异清单已由第 28 轮实测给出（12.15.4.1/12.15.4.2），不再需要「开工前比对」，而是**照清单改上游实现**；验证方式是构造非法 `input_schema`（应报错而非静默降级）与 `text/plain` 文件（应转 text 块而非 image）。**第 47 轮补——9.1 第 8 条（oai→gemini 的 finish_reason 严格语义）的专项验证**：构造 `finish_reason` 为未知值（如 `unknown_reason`）的 OpenAI 响应，经 Gemini 响应方向转换后**必须报错**（本地 `openAIFinishReasonToGemini` 的行为），**不得**产出 `stopReason: "STOP"`；再构造 `length` 与 `content_filter` 各一条，断言分别产出 `MAX_TOKENS` 与 `SAFETY`（这两条两侧同值，属回归保护）。**该组用例在阶段 4 之前不存在**（`openAIFinishReasonToGemini` 全仓零测试），且**不得**用 golden 快照承载——错误路径不产出可快照的正常输出（同 12.16.1 对 9.1/9.2 的口径）。**第 48 轮补——9.2.4/9.2.5（Gemini 两个请求方向的入口转换器严格性）的专项验证**：9.2.4（gemini→oai 请求）构造 `content.Role` 为未知值（如 `"system2"`）的 Gemini 请求，经 oai 方向请求转换后**必须报错**，**不得**被静默重解释为 `user`；再构造单 part 含多字段、`thoughtSignature` 非空各一条，断言报错。9.2.5（oai→gemini 请求）构造 `stop` 为 6 条的 OpenAI 请求，经 gemini 方向请求转换后**必须报错**（本地 `CovertOpenAI2Gemini` 的行为），**不得**被静默截断为 5 条；再构造 `tools[].type` 为未知值、`tools[].function.name` 重复、`message.role` 为未知值各一条，断言报错。**这两组用例同样不得用 golden 快照承载**（错误路径），且**须为 9.2.4/9.2.5 各自的「保留」分支补一条正向回归**（合法输入在本地方案下输出不变）。

**本地 golden 基线（见 12.16；阶段 4 与阶段 6 的命令不同）**：

```bash
# 阶段 4 之前：钉住本地实现的行为（测试文件在 service/，调用本地函数）
go test ./service -run TestGolden -count=1

# 阶段 4 之内/之后：测试文件**仍留在 service/**，只把 8 个调用点（分布在 6 个函数上）改指 relayconvert 包级入口
# （12.16.2 决定 1；快照不改）。因此命令**不变**——仍是上面那条。
# 差异必须对应处置清单第 6 项为 9.1/9.2/9.2.1/9.2.3（9.2.2 归第 17 项）产出的决定，否则视为回归（**第 38 轮修正**）。

# 阶段 6：测试与快照并入 relaykit/relayconvert/ 之后才换到这条
cd relaykit && GOWORK=off go test ./relayconvert -run TestGolden -count=1
```

**注意两条命令的分界**：`cd relaykit` 那条**只在阶段 6 之后有效**。阶段 4 只改调用点、不搬文件，所以在阶段 4 用 `cd relaykit` 会**找不到测试**（文件还在 `service/`）。§12.16.2 决定 1 与阶段 6 行的分工即此。

`-run TestGolden` 是**前缀匹配**，在阶段 4 命中本地三个测试（`TestGoldenRequestConversion`/`TestGoldenResponseConversion`/`TestGoldenStreamConversion`）；在阶段 6 并入后命中上游三个矩阵测试（`TestGoldenRequestConversionMatrix`/`TestGoldenResponseConversionMatrix`/`TestGoldenStreamConversionMatrix`，`golden_test.go:290/319/337`）与并入的 DeepSeek 用例。

**`-update` 只用于重录基线，不得用于吸收阶段 4 的差异**——那会把「行为回归」直接写成新的正确值，正是 12.16 批评上游 `-update` 机制的原话。

**前端改动**：`cd web/default && bun run lint && bun run build`（AGENTS.md 第 4 节）。

**dto 兼容性七组比较**（阶段 2 前必跑，上游 relaykit 每次变动后重跑；工具实际打印 7 组、`--strict` gate 7 个计数——「七组」以工具输出为准，组数与分节名见下）：

```bash
KIT_REF=789c97019 scripts/relaykit-dto-compat.sh --strict   # relaykit 侧须钉文档锚点；不钉时默认取会漂移的 upstream/main（第 40 轮实测已漂到 04c597bf6，计数变 (35,6,7,…)、strict 全红）
# 计数: fieldSet=33 tag=5 type=6 embed=0 order=4 sig=12 equiv=21
# （2026-10-06 基线；order 与 equiv 是第 30/31 轮补入 gate 的两个维度）
```

**设计文档行号引用的完整性**（锚定漂移后重跑，见「阶段 1 的前置：relaykit 源码的取得与版本锚定」）：

```bash
scripts/relaykit-citation-check.py --ref 789c97019   # 全部引用可解析时退出码 0（relaykit 82 + 宿主 150 + upstream 全路径 47，裸引用 ok=67/未解析=3，sha ok=9/failed=0，第 53 轮；不带 --ref 时用会漂移的 upstream/main，见「阶段 1 的前置」的版本锚定段；docs/ 路径只有「两处都不存在」才跳过——上游存在时必须要求 upstream/main: 前缀，第 41 轮修掉 fail-open）
python3 scripts/relaykit-citation-check.test.py  # 54 个单测（第 43 轮 +9；第 52 轮 +8：斜杠行号列表逐行展开；第 53 轮 +11：短横线范围端点 + sha 钉点组）
```

**C1–C8 动作行的符号/文件机械核对**（第 50 轮新增，专门核「照抄上游终态」那条主线的可编译性——C1–C8 八行引用的每个 `pkg.Symbol` 与带锚点文件引用逐条在锚点上验证）：

```bash
python3 scripts/relaykit-c-symbols-check.py            # C 行符号核对: 符号 10 个 + 带锚点文件引用 17 个
python3 scripts/relaykit-c-symbols-check.test.py       # 12 个单测（含反向验证：改坏符号/行号/删 C 行须变红）
```

**文档结构完整性**（第 38 轮新增，与上一条正交：上一条查「引用能否解析」，本条查「文档自身结构是否自洽」）：

```bash
python3 scripts/relaykit-doc-structure-check.py        # 两份文档均 0 问题时退出码 0
python3 scripts/relaykit-doc-structure-check.test.py   # 25 个单测（第 41 轮 +5：§ 任意段数、第三文档引用、版本号不误报、D 主表缺失）
```

**门禁自述计数**（第 38 轮新增：把「门禁改了、描述没改」变成门禁——这一盲区此前复发三次：第 36 轮的 34/15、第 38 轮的 §9.3 47/98/21、以及 `relaykit-citation-check.py` docstring 里长期滞留的 「13 citations across 9 files」）：

```bash
python3 scripts/relaykit-gate-selfcheck.py            # 实跑前四条门禁，与本文档的自述计数逐处比对（内部按本文档锚点 789c97019 钉 citation 的 --ref 与 dto 的 KIT_REF，见脚本 UPSTREAM_PIN；citation 四行的 `failed` 必须存在且为 0（第 43 轮起含裸引用组；其 `unresolved` 数由第 11 节声明并逐条比对）；设计文档必须携带 EXPECTED_CLAIM_KINDS 声明的整类自述，删空一类即失败——第 41 轮补入的两条 fail-closed）
python3 scripts/relaykit-gate-selfcheck.test.py       # 42 个单测（第 40 轮 +1：钉死 ref 句式；第 41 轮 +4：failed 字段、尾注句式、类别 fail-closed；第 43 轮 +4：裸组解析与声明）
```

它只检查**本文档的当前口径声明**；CR 记录里的计数是历史记录（「第 37 轮为 50/103/24」这类），记的是「当时为真」，不随门禁演进失效，故不在射程内。

四类检查：**A** 表格行序号严格递增（含重复编号——`[1..12,13,13,15,16]` 是 sorted 的，只判非排序会漏）；**B** 每个数据行的单元格数等于表头列数（未转义的 `|` 会撑破单元格，渲染时多出一列）；**C** 交叉引用的章节号存在（`defined` 取被检查文档的**并集**，故 CR 引用设计文档章节号不误报；`SECTION_ALLOWLIST` 收 `5.6`/`4.6`/`4.7`/`4.8` 这类与模型名同形的串；带「§」的引用检查**任意段数**——旧版只捕三段，`§N.M.K.L` 这类四段编号的尾段会被数字断开、整个不匹配（示例一律用占位符，真编号会被本规则判为未定义引用）；裸点号数只在顶层号属章节头集合时才检查，因为 `go 1.25.1`、`v1.52.0` 等模块版本与章节号同形，第 41 轮实测放宽会误报 12 个版本号）；**D** 「未决项 N」的 N 必须在第 10 节的编号集合内（本轮补入 31–34 前，该项会报「主表只定义到 30」）；**主表小节缺失直接失败**——否则「删掉第 10 节」会让所有未决项引用静默失去校验（第 41 轮 Codex 复现的 D 假阴性）。

**这两条门禁的失效模式不同**：引用检查锚定 `upstream/main` 与基线，上游一动就红；结构检查只读本文档自身，**改动本文档的任何一次编辑都可能让它变红**——两处表格行序、两处未转义 `|`、一处缺列、一处主表缺项都是它抓出来的，而 38 轮事实核验一轮都没碰到过这个维度。

**验证记录**：沉淀到 `docs/reviews/CR-*.md`，含命令、退出码、通过或失败摘要。

不得以局部测试通过表述为全链路已验证。

**CI 门禁（阶段 0 建立）**：上游的 `.github/workflows/ci.yml` 已给出可照抄的形态——对两个 module 各跑一遍 `go vet` + `go build`，再 `make test`（`:43-58`）。本仓库当前 **7 个 workflow 中没有一个跑 `go test`**（实测：`pr-check.yml` 只跑 `peakoss/anti-slop`，`tauri-build.yml` 只在 PR 上构建桌面端，`release.yml` 是 `workflow_dispatch` + `go build` 出包，其余 3 个 docker/tauri 的 `push` 字段是 docker 的 `push: true` 而非事件触发器），阶段 0 需补齐。

**照抄上游有三处不够，须在本仓库补齐**（实测，非推测）：

1. **挂载点**：上游 `ci.yml` 触发于 `pull_request`；本仓库唯一在 PR 上触发且与代码质量相关的是 `pr-check.yml`（`pull_request_target`）。阶段 0 的门禁应挂到 `pull_request`（**不是** `pull_request_target`——后者在 base 分支上下文运行，无法检出 PR 的代码来测试）。**规格此前只说「建立 CI 门禁」，没写挂到哪个 workflow、用哪个事件**，这是阶段 0 开工前必须定的。
2. **`make test` 目标本仓库不存在**：本地 `makefile` 有 `all`/`build-frontend`/`build-frontend-classic`/`build-all-frontends`/`start-backend`/`dev-api`/`dev-web`/`dev-web-classic`/`dev` 共 9 个目标，**没有 `test`**。上游的 `make test`（`makefile:43-49`，两个 module 分别 `GOWORK=off go test`）须照抄进来。（注：工作区的 `Makefile` 是 macOS 大小写不敏感文件系统的同名别名，50 行与 `makefile` 相同，不被 git 跟踪。）
3. **上游 `make test` 不含 `-count=1` 与 `-shuffle`**：实测其命令为 `GOWORK=off go test $$root_packages` 与 `cd relaykit && GOWORK=off go test ./...`，**两个 flag 都没有**。而本仓库有已知的跨运行状态泄漏（命名 in-memory DB 未关闭、包级 limiter 单例、`sync.Once` 缓存捕获 Redis client——`-count=1` 绿会隐藏失败）。**故照抄上游会让 CI 继承这个盲区**；本仓库的 `test` 目标须显式加 `-count=1`，并在至少一条路径上加 `-shuffle=on`。

**`TestGolden` 须进入 CI 门禁**：它是阶段 2/4/6 三个判据的共同依赖（阶段 2「`TestGolden` 仍绿」、阶段 4「8 个快照逐字节一致」、阶段 6「golden 基线已在 `relaykit/` 下运行」）。**规格此前从未把 `TestGolden` 写进任何 CI 表述**——若它不在 CI 里，上述三个判据就只在本机手工执行时才有效，PR 上无人拦截。阶段 0 的 `test` 目标须确保它被 `go test ./service`（阶段 2 前）与 `cd relaykit && go test ./relayconvert`（阶段 6 后）覆盖到。

## 10. 未决项

1. ~~**完成本地独有能力盘点**（4.1）~~ **已完成**，见 4.1–4.5.5 与第 11 节取证。剩余未覆盖边界已明确列在 4.5.5，不影响阶段 4 启动
2. ~~**think 标签提取族归属**（4.2）~~ → **12.1**：剥离迁入 relaykit 成为 Claude 历史回传能力（带钩子），门控留宿主
3. ~~**本地专有错误符号归属**（4.3）~~ → **12.2**：4 个 `ErrorCode` 常量与 2 个判定函数全部迁入 `relaykit/types`
4. ~~**`*WithError` 系列形式**（4.5.4）~~ → **12.3**（**第 35 轮修正范围**）：直接删除（与上游一致，上游 `service/` 下已删、0 命中），9 个生产调用点改指包装层；**「4 个非 WithError 旧包装一并删除」已撤回**——上游把它们保为永久 facade（`upstream/main:service/convert.go` 27 行，见 12.20），**第 44 轮随未决项 33 定案为 C：保留为 facade**
5. ~~**响应方向暴露策略的具体形态**~~ → **12.4**：**relaykit 内门控**（`convmeta.Options` 钩子），必须在索引分配之前。后置过滤会破坏 SSE 索引，不可行
6. ~~**本地 DTO 独有方法归属**（4.4）~~ → **12.5（第 42 轮定案）**：A 类 **21** 个迁入 relaykit/dto（含原 3 个「需实测」，均判 A），B 类 2 个降为宿主自由函数，待实测 0 个
7. ~~**本地 hosted 工具与上游 `toolconv` 的收敛**~~ → **无差异**：上游 10 种 `Kind`（`function`/`web_search`/`file_search`/`web_fetch`/`code_execution`/`computer_use`/`url_context`/`mcp`/`image_generation`/`native`）是本地超集。本地非测试代码出现的 hosted 工具 type 只有 3 种（`web_search`、`file_search`、`image_generation`），差集为空。直接采用
8. ~~**`types` 别名转发的适用范围**~~ → **12.2**：仅 `endpoint_type.go`、`finish_reason.go` 两类常量用别名；其余改 import
9. ~~`relaykit/dto` 补丁 2 是否影响上游 golden 快照~~ → **12.16**：需实测，24 个 JSON 快照逐一核对
10. ~~`ConvOptions()` 缓存与失效语义~~ → **12.10**（**第 34 轮修正**）：上游 `upstream/main:relay/common/relay_info.go:311`（`InitChannelMeta` 起于 `:245`）已显式置 `nil`，但**本地 `InitChannelMeta`（`relay/common/relay_info.go:358`）没有该行、`convOptions` 字段也不存在**——阶段 3 须同时落地字段与重置行，判据见 12.10
11. ~~钩子闭包捕获语义~~ → **12.10**：收窄为「钩子只答策略、不捕获状态」，调用点传入 `modelName`
12. ~~`reasonmap` 的宽松 default 是否需严格模式~~ → **12.11**：保留宽松 default；**但「严格化在宿主调用点做」经第 29 轮实测无落点**（原调用点全在阶段 6 删除的 `service/convert.go` 内），改为 12.11.3 的 A1/A2/B 三选一
13. ~~thinking block 的 `signature`~~ → **12.12**：维持现状（请求接受无签名块、响应不生成签名），标注为已知未验证项，列入阶段 4 验证清单
14. ~~`enabled` 双语义~~ → **12.13**：保留并文档化（`UnmarshalJSON` 用存在性判断，语义确定）
15. ~~上游 `feat/relaykit-tools` 分支是否应作为补丁基线~~ → **12.9**：不采用，两分支均早于 main（2026-09-01/03 vs 09-28），且核心提交已作为 PR #7137 并入 main
16. ~~**命名空间工具与 tool_search 的归属**（4.5.1）~~ → **12.6**：整体保留在宿主（上游零覆盖，属本地协议扩展）
17. ~~**custom tool bridge 门控语义取舍**（4.5.1）~~ → **12.7**：保留本地 fail-closed（默认拒绝、显式开启）
18. ~~**`matchAnyRegex` 空模式语义**（4.5.1）~~ → **12.8**：采用本地语义（空 patterns = 匹配全部）并文档化
19. ~~**未跟踪文件纳入版本控制**（4.5.2）~~ → **12.14**：阶段 0 门禁，与会话确认后提交
20. ~~**`service/openaicompat/` 的收敛边界**（4.5.1）~~ → **12.15**：**整体保留**。按能力切分在函数粒度不成立（`convertResponsesToolsToChat` 内交织 5 种工具语义）
21. ~~**`dto` 迁入后对宿主包/gin 的 import 如何处理**~~ → **12.0.4**：阶段 2 必须清零，`boundary_test.go` 是硬门禁。3 个 `kitutil` 未提供的符号（`JsonDelim`/`NewJsonDecoderUseNumber`/`DecodeJsonFromDecoder`）与 1 个类型（`ResponsesRequestMetadata`）的归位方式（改写 vs 新增到 relaykit）**待阶段 2 开工前实测确定**，优先改写
22. ~~**`dto` import 图是否成环**~~ → **12.0.5 第六节已关闭**：`constant` 不 import relaykit，`types`/`kitutil` 不 import dto，成环不成立
23. ~~**33 个字段不兼容类型的逐项对齐**~~ → **12.0.5 第五节已关闭**：136 个同名结构体中 33 个字段集不同，但按「取 relaykit 后本地是否丢字段」二分后，**只需动作的 16 个**，清单与处置已逐项给出；另 17 个是纯增益
24. **`openaicompat` tool 循环重构**：让基础分支可独立转调 relaykit 的唯一路径；独立重构，不在本次迁移范围（12.15.3）
25. ~~**本地行为的 golden 基线**~~ **已关闭**（2026-10-04）：见 12.16——`service/convert_golden_test.go` 已建于 `HEAD`，覆盖请求/响应/流三个矩阵共 8 个快照（含 DeepSeek 与非 DeepSeek 两侧的 thinking 门控），并实测「扰动任一快照即测试失败」
26. ~~**命名空间字段的生产引用精确计数**~~ **已关闭**（2026-10-03）：见 12.6——11 个非测试文件、跨 6 个顶层目录（8 个 Go 包；**第 40 轮修正**：原文写「跨 6 个包」，与 12.6 的目录/包双口径不符），已给出「口径 + 命令 + 结果」
27. ~~**`dto` 宿主 import 归零后 3 个符号的归位方式**~~ → **12.0.7 已关闭**（2026-10-04）：三个符号是 `encoding/json` 的薄包装，直接换 stdlib（relaykit/dto 本就 import 它）；`ResponsesRequestMetadata` 判为宿主类型（488 行文件 + 650 行扫描器依赖），随 `dto/opaque_openai_responses_request.go` 保留在宿主 `dto`
28. ~~**通道级转换器的归属（第 12 轮新增）**~~ → **12.15.4 已关闭**（2026-10-04）：**收敛到 relaykit**，本地只留薄适配。三类宿主耦合 relaykit 侧均有注入机制——思考等级走 `convmeta.Options` 钩子（`sharedclaude.ApplyReasoning`）、媒体解析走 `relayconvert.MediaResolver`（签名与 `service.GetBase64Data` 一致）、schema 归一化对应 `FunctionParametersToInputSchema`。阶段 4 注册钩子后删除 459 行本地实现。**第 28 轮实测了两条边界，均不成立**（12.15.4.1/12.15.4.2），故收敛不是纯接线：须一并改上游的 schema 归一化与媒体分派（本地改动清单第 10、11 行）
29. ~~**`dto` 独有缓存字段的承载方式（第 12 轮新增）**~~ → **12.0.7 已关闭**（2026-10-04）：把 `EffectiveTools()` 连缓存字段一起**迁入 `relaykit/dto`**——未导出字段在**自己的包**内声明即合法（上游 relaykit 已有同例，如 `Reasoning.effortRaw`），不需要宿主侧旁路。13 个调用点改 import 即可
30. ~~**字段名相同但 tag / Go 类型不同的 10 个类型（第 14 轮新增）**~~ → **12.0.8 已关闭**（2026-10-04；**第 30 轮修正计数**）：**11 处**（tag 5 + 类型 6，原文写 15）**全部采用 relaykit、零处打补丁**。tag 5 处中 3 处因字段是 struct 值而 `omitempty` 无效（空操作）、2 处风险已逐点排除；类型 6 处中 `CreatedAt` 为纯增益（`MarshalJSON` 与 int 相同）、`IncludeThoughts`/`Text`/`Arguments` 须改读写点、`SupportedEndpointTypes` 改 import

31. **`finish_reason` 严格语义的落点（第 29 轮新增，第 37 轮修正为三选，第 44 轮定案）**：**已定案为 B**（宿主侧共享助手的归一化前置校验，第 44 轮，见 12.11.3）——**A1** 给 `reasonmap` 加返回 error 的严格变体 + 改 `oai_chat` 3 个函数签名（**已排除**：A1 是 A2 的超集，见 12.11.3 第 42 轮实测）；**A2** 布尔钩子只选分支、报错由宿主前置校验（**已排除**：退化为 B 且多一个只为选分支而存在的钩子）；**B** 宿主 5 处调用点经**一个共享助手**前置校验（第 44 轮定案）。原「选项 A」的布尔钩子**不产生 error**（`reasonmap.OpenAIFinishReasonToClaudeStopReason` 与 `oai_chat` 3 个调用函数都无 error 返回通道，第 37 轮实测）；原表述「留宿主调用点」无落点——两个原调用点都在阶段 6 删除的 `service/convert.go` 内（12.11.1）。**第 44 轮新增证据**：宿主已存在覆盖 a/b/c/d 的现成严格函数 `stopReasonOpenAI2ClaudeStrict`（基线 `service/convert.go:1125`，返回 `(string, error)`，含 `""`/`TrimSpace`/`function_call` 三条分支），但它在 12.3 下随 `service/convert.go` 主体删除——故 B 不是「新写校验」而是「把该函数的存活位置从被删文件迁到宿主共享助手」，见 12.11.3 第 44 轮实现形态。**第 47 轮补**：本项标题的「`finish_reason` 严格语义」此前只指 Claude 方向；**Gemini 方向（9.1 第 8 条）同型、同批、同形态**——第二个严格函数 `openAIFinishReasonToGemini` 与第二个归一化助手，落点见 12.11.2b 与 12.11.3 第 47 轮补。**本项并未因扩为两个方向而改变定案**（仍是 B），但「宿主 5 处调用点」自此应读作「宿主 5 + 4 = 9 处调用点、2 个助手」。
32. ~~**12.5 的 3 个「需实测」方法的分类（第 32 轮新增）**~~ **已关闭**（第 42 轮）：三方法均判 **A（迁入 `relaykit/dto`）**，见 12.5 与「阶段 2 的前置」。三者在 relaykit 全仓零命中、方法体零宿主依赖；`EnsureResponsesTokenDetailObjects` 的 A 为**条件结论**（该对象在 `HEAD`/基线上不存在，只存在于工作区——其他会话的未提交改动，迁入前须确认其最终形态）
33. **阶段 4 的切换形态（第 33 轮新增；第 35 轮扩为三选一，第 44 轮定案）**：**已定案为 C（照抄上游终态）**（第 44 轮，见 §8.1、12.20、12.20.1）——**A 薄封装** / **B 直接改写调用点** / **C 照抄上游终态（已选定）**。§8.1 要求阶段 4「单点可逆」，C 的切换 = `service/convert.go` 单文件的函数体替换，回滚最集中（完整回滚清单见 12.20.1 末段）。定案即 12.20.1 的连带清单生效、处置清单第 16 项进入阶段 4 动作面
34. **思考等级后缀填充器缺失（第 34 轮新增，第 44 轮定案）**：**已定案为 A（移植上游填充器）**（第 44 轮，见 12.19）——移植上游 `relay/helper/reasoning_suffix.go`（+ `model_modifier.go` 符号，加性补 `ParseLegacyModelSuffix`/`ShouldPreserveEffortTail`/`PreserveEffortTail`）。判据：本地现有后缀词表是上游**可表达面的子集**——本地 `setting/reasoning` 无 `-thinking`/`-nothinking`/budget/`@modifier` 任何解析能力，选项 B（本地等价填充器）意味着**在宿主重写上游已有的解析语义**，正是本文档反对的「在宿主复制套件逻辑」。relaykit 不解释 `-thinking`/effort 尾缀，4 个消费点全靠 `ReasoningState()`；不装即静默丢失跨协议路径的思考等级，且 8 个 golden 快照零后缀用例、抓不到该回归

**编号 31–34 的完整说明见 12.18**（该表同时保留第 12 轮新增的 21–30 条目及其关闭状态）；本节第 10 节**已无「仍未决」项**——31（B）、33（C）、34（A）于第 44 轮定案，32 已于第 42 轮关闭。
## 11. 事实依据

行号以第 1 节声明的本地基线 `dbfbc096f` 为准（该基线与 `9c4758b53` 之间只改了代理/环境文件，行号一致）。上游行号以**钉死的 `upstream/main` = `789c97019`** 为准（见「阶段 1 的前置」的版本锚定；不用会漂移的 `upstream/main` 默认值——**第 40 轮修正**：原文只写「以 `upstream/main` 为准」）。

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
| relaykit 内建 Claude 模型代能力表 | `relaykit/relayconvert/reasoning/claude.go:35-78`（10 个模型前缀） |
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
| `service/convert.go` 4 个 `*WithError` 有 9 个外部生产调用点 | `git grep -nE "(^\|[^A-Za-z0-9_])<fn>\(" -- '*.go'` 排除定义行与 `service/convert.go` 内部包装 |
| 4 个无 error 旧包装的生产调用点为 0 | 同法核验 `StreamResponseOpenAI2Claude` / `ResponseOpenAI2Claude` / `ResponseOpenAI2Gemini` / `StreamResponseOpenAI2Gemini`，命中仅 `_test.go`。**范围限定**：这 4 个是 `service/convert.go` 内的包装；**通道级的 4 个同名函数另有 5 个生产调用点**（`relay-claude.go:1167,1273`、`relay-gemini.go:1605,1768`、`aws/converse.go:696`，第 37 轮补入），不在本行口径内 |
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
| 上游只有 2 个 `constant/` 文件做转发 | 逐文件核验 `upstream/main:constant/*.go` 的 `relaykit` import：仅 `endpoint_type.go`（类型别名 + 常量别名）、`finish_reason.go`（变量别名，无 `type` 声明） |
| `relay/common/responses_tool_names.go` 未跟踪但在生产路径 | `git status --short` 显示 `??`；调用点 `relay/responses_handler.go:1123`、`relay/channel/openai/relay_responses.go:912,1296` |
| 本地 `types/` 仅 2 个文件依赖宿主 `common` | 逐文件 grep `github.com/QuantumNous/new-api/[a-z]+`：`error.go`、`rw_map.go` |
| 上游 `dto` **零别名转发**，直接改 import | 上游宿主 `dto/` 8 个文件均不 import relaykit、无 `type X =` 行；`git grep -l 'new-api/relaykit/dto' upstream/main -- '*.go'` = **281**，宿主 dto = 39 |
| 本地 327 个文件 import 宿主 `dto` | `git grep -l '"github.com/QuantumNous/new-api/dto"' HEAD -- '*.go' \| wc -l` |
| 33 个同名类型字段集不同（双向） | `go/ast` 解析 22 个同名文件的 136 个同名导出类型的字段名集合求差集；基线/HEAD 均 33（工作区 34）。本地独有 `Reasoning.Extra`、`StreamOptions.Extra`、`Message.ThinkingContent`、`ResponsesOutput.Namespace`、`ChannelOtherSettings` 20 字段等；relaykit 独有 `GeneralOpenAIRequest` 20 字段、`ResponsesOutput` 15 字段等 |
| Go 别名**继承方法集**但**不能新增方法** | 最小程序实测：`type A = base.T` 后 `a.Method()` 正常；跨包给别名加方法报 `cannot define new methods on non-local type Thing` |
| 本地 dto 394 个函数 vs relaykit 273 | `git grep -hE '^func ' HEAD -- 'dto/*.go' \| wc -l` / 同法查 `relaykit/dto/` |
| `channel_settings.go` 本地 61 函数 / relaykit 29 | 单文件 `grep -cE '^func '` 得 61/29（含包级函数）；42 个本地独有**方法**由 `func (recv) Name` 提取后求差集，按 `接收者.方法名` 口径上游全仓 0 命中（纯名字口径下 `IsNative` 会误命中上游 `AdvancedCustomRoute.IsNative`，须带接收者） |
| 本地 12 个**非测试** dto 文件 import 宿主 `common` | 逐文件 `git show HEAD:<f> \| grep`：非测试 `common` 12、`constant` 2、`logger` 1（含测试 21/3/1）；relaykit/dto 只 import `types`(17) 与 `kitutil`(12) |
| `convertResponsesToolsToChat` 交织 5 种工具 | `responses_to_chat_request.go:455` 签名含 `namespaceSelection` + `toolState`；函数体内 namespace 16 / toolState 5 / ToolSearch 5 / Custom 4 / EnableCustomToolBridge 3 |
| 本地 thinking 门控在索引分配之前 | `service/convert.go:749` 算 `allowClaudeThinking`，`:794` `if !allowClaudeThinking { return }`；relaykit `internal/oai_chat/to_claude_messages_resp.go:137`（`state.Index++` 在 `:151`）无条件推进索引，`:342` 起无条件建块 |
| AWS Converse 无条件暴露 thinking | `relay/channel/aws/converse.go:748` 直接构造 `{Type:"thinking"}`；全文件 0 处调用 `shouldExposeReasoningAsClaudeThinking` |
| `shouldExposeReasoningAsClaudeThinking` 只对 DeepSeek 为真 | `service/convert.go:526` 即 `hasDeepSeekModelName(model, info)` |
| `openaicompat` 第 3 个消费者 | `relay/responses_via_chat.go:104` 调 `service.ResponsesRequestToChatCompletionsRequestWithState` |
| 上游两分支早于 main 且已并入 | `feat/relaykit-tools`=`bbe20f2e1`(2026-09-01)、`feat/relaykit-perf`=`55bba35f2`(2026-09-03)，均早于 main(2026-09-28)；核心提交内容已作为 PR #7137 `0ed497f06` 并入（`git merge-base --is-ancestor 0ed497f06 upstream/main` 通过；两分支 toolconv 均 10 个 `Kind*`） |
| `ConvOptions` 缓存跨渠道重试：上游已处理，本地未处理（第 34 轮修正） | 上游 `upstream/main:relay/common/relay_info.go:311` 在 `InitChannelMeta` 置 `info.convOptions = nil`；本地 `InitChannelMeta`（`relay/common/relay_info.go:358`）无此行且无该字段（`git grep convOptions HEAD -- relay/common/` 空）。本地换渠道重试调用 `InitChannelMeta`：`controller/relay.go` 渠道重试循环内的 `info.InitChannelMeta(c)`（基线口径 `:969`；**不写 HEAD 行号**——第 35 轮发现该行 24 小时内已从 `:1016` 漂到 `:1028`） |
| 阶段 3 照抄的 13 个方法依赖 3 个本地没有的字段（第 34 轮实测） | `git grep -n 'func (info \*RelayInfo)' upstream/main -- relay/common/relay_info.go`（823-953 段）+ 逐字段 grep：`ReasoningState()` 读 `ReasoningConversion`（上游 `:123`）、`ConvOptions()` 读 `convOptions`（`:201-202`）——本地 `git show HEAD:relay/common/relay_info.go` 对两名字零命中；同段 `GetBillingModelName`（`:832`）读 `BillingModelName`（`:107`）但不在 `convmeta.Meta` 接口、relaykit 零调用（`git grep GetBillingModelName upstream/main -- relaykit/` 空） |
| relaykit 后缀意图的 4 个消费点（第 34 轮实测） | `git grep -n 'ReasoningStateOf' upstream/main -- relaykit/relayconvert/internal/`：`shared/claude/reasoning.go:24`、`shared/gemini/request.go:107`、`claude_messages/to_oai_responses_req.go:96`、`gemini_chat/to_oai_chat_req.go:56`；README 明示转换器不再解释 `-thinking`/effort 尾缀，宿主须自行 `Parse*` 并写 `dto.ReasoningConversionState` |
| 本地无后缀填充器入口（第 34 轮实测） | `relay/helper/reasoning_suffix.go` 与 `relay/helper/model_modifier.go` 在本地 `HEAD` 均不存在（`git ls-tree HEAD relay/helper/` 无此二文件）；上游 `ApplyReasoningModelSuffix` 非测试调用 6 处：`upstream/main:relay/claude_handler.go:41`、`upstream/main:relay/compatible_handler.go:46`、`upstream/main:relay/gemini_handler.go:40`、`upstream/main:relay/gemini_handler.go:201`、`upstream/main:relay/responses_request.go:43`、`upstream/main:controller/channel-test.go:262` |
| golden 基线零后缀用例（第 34 轮实测） | `git show HEAD:service/convert_golden_test.go \| grep -oE '"(claude\|gpt\|gemini\|deepseek)[a-z0-9.-]*"' \| sort -u` 仅得 `claude-test`/`deepseek-v4`/`gemini-test`/`gpt-test` 四个，无任何 `-thinking`/`-high` 尾缀 |
| 上游宿主终态是「保留旧名」而非全删（第 35 轮实测） | `upstream/main:service/convert.go` 27 行、5 个同名 facade（函数行 `:9`/`:13`/`:17`/`:21`/`:25`，旧签名不变、一行转发）；`upstream/main:service/request_converter.go:50`/`:62` 保留 `ClaudeToOpenAIRequest`/`GeminiToOpenAIRequest` 两个同名 shim；`upstream/main:relay/channel/claude/relay-claude.go:37`/`:41` 与 `upstream/main:relay/channel/gemini/relay-gemini.go:139`/`:143` 为一行 facade；`*WithError` 转换函数在 `upstream/main:service/` 下 0 命中（全仓仅一条无关测试名 `TestStreamStatus_SetEndReason_WithError` 含此字样）、`IsDeepSeekModelName`/`HasDeepSeekModelName` 全仓 0 命中 |
| relaykit 公开兼容面（第 35 轮实测） | `relaykit/relayconvert/response_compat.go` 导出约 40 个同名包装，含 facade 的 5 个目标（`relaykit/relayconvert/response_compat.go:34`、`:38`、`:42`、`:128`、`:132`）与 4.5.5 的 4 个通道级目标（`:50`、`:100`、`:140`、`:144`） |
| 宿主包装层调用规模（第 35 轮实测；**第 51 轮校准口径**） | 上游**钉点** `789c97019` 的 7 族包装（`ConvertRequest` / `ConvertRequestByID` / `ConvertRequestVia` / `ConvertResponse` / `ConvertStreamResponse` / `ConvertStreamResponseChunk` / `FinalizeStreamResponse` 七族）非测试调用点 **39 处、16 个文件**（advancedcustom/ali/aws/claude/gemini/openai/vertex 及 relay 顶层）。**第 51 轮补三点**：①口径须写全 7 族——只数 `ConvertRequest`/`Response`/`StreamResponse` 三族会得 33 处/15 文件（漏 `FinalizeStreamResponse` 的 6 处）；②**其中 8 处本地无对应文件**：`advancedcustom/adaptor.go` 3 处（该通道本地不存在）+ `claude/relay_responses.go` 2 处 + `gemini/relay_responses.go` 3 处（两文件基线与 HEAD 均不存在，见 C5 行），**本地适用面 = 39−8 = 31 处/13 个文件**；③`ConvertRequestVia` 在钉点**全仓零生产调用**（只有 relaykit 自测），`ConvertRequestByID` 的唯一生产调用方就是本地不存在的 advancedcustom |
| 测试面 82 处调用点（第 35 轮实测） | 4 个文件：`convert_claude_request_test.go` 35、`convert_claude_stream_test.go` 14、`convert_test.go` 25、`convert_golden_test.go` 8（`git show HEAD:<f> \| grep -oE` 同包无前缀计数）；按本文档字面形态阶段 4 断 38、阶段 6 断 44 |
| 上游测试文件形态（第 35 轮实测） | 本地两个大测试文件上游不存在；上游 `service/convert_test.go` 仅 114 行、只测 5 个 facade |
| `matchAnyRegex` 空模式语义差异 | 本地 `service/openaicompat/regex.go`：`len(patterns)==0 → true`；上游 `upstream/main:service/openai_chat_responses_mode.go:15`：`len(patterns)==0 \|\| model=="" → false` |
| `service/openaicompat/` 共 3072 行（9 个非测试文件，基线口径） | 逐文件 `wc -l` 求和；含测试 5280。工作区因其他会话改了 `chat_to_responses_response.go` 为 3071 |
| 两分支 6 个提交均非 main 祖先 | `git merge-base --is-ancestor <c> upstream/main` 对 `55bba35f2`/`bbe20f2e1`/`c82c03ecd`/`60e15b53a`/`b45639116`/`a9a0e2272` 全部 exit=1 |
| 两分支内容已被 main 覆盖（逐字节/符号集） | `toolconv/` 7 文件 5 SAME、2 处为 main 侧**功能新增**（`decodeOpenAIChatPseudoHostedTool` 等）；`reasoning/claude.go` 导出符号双向为 0；`billing_usage.go`/`usage_merge.go` 分支独有导出符号 0；`relaykit/` 整体 main 净增 `3910+/746−` |
| 非测试 dto 文件的宿主 import 分布 | 逐文件 `git show HEAD:<f> \| grep`：`common` 12、`constant` 2、`logger` 1（含测试 21/3/1） |
| `HasCompactionTrigger` 生产引用 9（提及口径） | `git grep -lw` 排除 `_test.go` 与 `dto/`；按调用 `.HasCompactionTrigger()` 计为 7 |
| 136 个同名类型中 33 个字段不兼容 | `go/ast` 解析 `type X struct` 字段名集合求差集；基线 `dbfbc096f` 与 `HEAD` 均为 33，工作区 34（`ChatCompletionsStreamResponse` 被其他会话改动） |
| `ChannelOtherSettings` 本地独有 20 个字段 | 同上，AST 差集；基线/HEAD 均 20，工作区 21 |
| `GeneralOpenAIRequest` relaykit 独有 20 个字段 | 同上，AST 差集；三口径均 20 |
| `channel_settings.go` 42 个本地独有方法上游全仓 0 命中 | 逐个 `git grep -n '<recv>) <m>' upstream/main -- '*.go'`（带接收者），42/42 命中数为 0；不带接收者时 `IsNative` 误命中 3 处 |
| 24 个 golden 快照 | `git ls-tree -r upstream/main relaykit/ \| grep -i golden \| grep -v '\.go$' \| wc -l` = 24（第 25 个是 `golden_test.go` 本身） |
| relaykit `state.Index++` 的确切位置 | `internal/oai_chat/to_claude_messages_resp.go:137` 定义 `stopOpenBlocksAndAdvance`，`:151` 在 `default:` 分支执行 `state.Index++` |
| 上游 `info.Request` 赋值位置 | 上游 `upstream/main:relay/common/relay_info.go:562` 定义 `genBaseRelayInfo`、`:595` 赋值；本地基线 `:670` 定义、`:701` 赋值 |

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
- 「`dto/channel_settings.go` 属宿主业务域、不应被波及」：它是 dto 文件，类型迁走则 42 个本地独有方法无家可归
- 「`service/openaicompat/` 消费者仅 2 个文件」：漏了 `relay/responses_via_chat.go:104`（调 `…WithState`）
- 「流式下逐 chunk 剔除 thinking 是无状态操作」：relaykit 的 `state.Index` 是私有流式状态，删块产生索引空洞
- 「AWS Converse 用同一 helper」：该路径当前**无条件**暴露 thinking，套用 DeepSeek-only 策略会新抑制 Bedrock reasoning
- 「`openaicompat` 按能力切分，基础转换转调 relaykit」：`convertResponsesToolsToChat` 一个函数内交织 5 种工具语义，函数粒度不可切
- 「24 个 golden 快照是唯一的机械回归网」：它是上游自己的基线，测不到 9.1/9.2 的 16 处本地差异（第 27 轮把 9.1 的第 7 条「同 chunk reasoning+text」计入；**第 30 轮把 9.2 的第 7、8 条计入；第 47 轮把 9.1 的第 8 条「oai→gemini 的 finish_reason 严格语义」计入，故 8 + 8 = 16**——第 28 轮加 9.2 两行时漏改此处的聚合计数；**第 48 轮再补 9.2.4 的 10 条 + 9.2.5 的 11 条（Gemini 两个请求方向），§9 的差异表随之从 4 个方向扩到凡有本地转换器的方向都有表——但 9.2.4/9.2.5 归于处置清单第 18 项、不并入本句的 16 处聚合**）
- 「响应方向不打 relaykit 补丁」（第 6.1 节结论）：已被 12.4 推翻——门控必须在索引分配之前，只能在 relaykit 内

## 12. 决策

第 10 节 20 项未决的处置结论。每条给出决定、依据与影响。凡标注「需实测」者，实施时必须先取证再落地。

### 12.0 决定全局归属的硬约束

本节是 12.1–12.15 的共同前提。**本文档早期版本在此处给出的两条论断均已被实测推翻**，修正如下。

#### 12.0.1 别名不能承载 dto 迁移（推翻早期「调用点不变」）

早期版本写「迁入 `relaykit/dto`（上游化）：方法体随类型走，**调用点不变**」。**这是错的**，两个独立事实：

**事实一：上游对 `dto` 根本没用别名。** 上游只有 `constant/endpoint_type.go`、`constant/finish_reason.go` 两处做转发，且**手法不同**：前者 `type EndpointType = types.EndpointType` 是**类型别名**加常量别名，后者只有 `var FinishReasonStop = types.FinishReasonStop` 一族**变量别名**（该文件无任何 `type` 声明）。宿主 `dto/` 剩的 8 个文件是**独立包**——零别名转发、零 relaykit import。上游是直接改 import：

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

- `dto/channel_settings.go` 被早期版本归为「宿主业务域、零协议类型 import、不应被波及」——但它是 dto 文件，类型必须迁走，其 **42 个本地独有方法**（`ResponsesCompactModeOrDefault`、`ResolveResponsesChannelCapability`、`MarkResponsesCompactAutoFallback` 等，按 `接收者.方法名` 口径上游全仓 0 命中）随之无家可归
- 全 `dto` 包本地 **394 个函数**、relaykit **273 个**（`^func` 口径、**含测试**；非测试口径为 306 / 215，见 12.0.5）。4.4 的「23 个方法」只比对了 3 个文件，**严重低估**

#### 12.0.3 依赖方向

`dto` 不只是 `types` 的消费者。实测本地 **12 个非测试 dto 文件 import 宿主 `common`**、2 个 import `constant`、1 个 import `logger`（含测试口径为 21/3/1）；relaykit/dto 只 import `relaykit/types` 与 `relaykit/relayconvert/kitutil`。**完整的 import 图分析仍是未完成项**（见 4.5.5），阶段 2 开工前必须补齐——否则 `dto/channel_settings.go` 的 `constant.ChannelTypeCodex` 可能形成 `dto → constant → relaykit/types → dto` 的环。

**这条约束决定了下面每一项的取舍，也解释了为何不能「先把 dto 搬过去再说」——搬的瞬间 33 个类型差异（基线口径，工作区 34）与全部宿主独有方法同时编译失败，必须同时给出出路。**

#### 12.0.4 `boundary_test.go` 决定 `dto` 的宿主 import 必须归零（第 12 轮新增）

上游 relaykit 用 `relayconvert/boundary_test.go` **以测试形式**锁死「未来迁入 relaykit 的包不得 import 宿主或 gin」。`kitDirs` 含 `dto`，`forbiddenPrefixes` 含宿主 module 前缀与 `github.com/gin-gonic/gin`，`allowedViolations` 为空且约定「只减不增」。**这不是一条背景事实，而是阶段 2 的验收条件**——阶段 2 的完成判据必须包含「`cd relaykit && GOWORK=off go test ./...` 通过」，而不只是「两个 module 均可编译」（import 不合规时编译可以过，只有该测试会拦）。

**决定**：阶段 2 在迁入 `dto` 的同一批动作里，把 `dto` 对宿主包的 import 全部清零，**不新增 `allowedViolations` 条目**（那会违反文件契约，见「阶段 2 的前置」）。归位方式：

| 现状 | 归位方式 |
|---|---|
| `common` 的 `Marshal`/`Unmarshal`/`GetJsonType`/`Interface2String`/`JsonRawMessageToString`/`Any2Type`/`RawMessage`（12 文件） | 改 import `relaykit/relayconvert/kitutil`，同签名直接替换 |
| `logger.LogError`（1 文件，`dto/gemini.go`） | 改 `kitutil.LogError` |
| `constant.EndpointType`（`dto/pricing.go:11`） | 改 `types.EndpointType`（上游 `relaykit/dto/pricing.go:11` 即如此） |
| `common.JsonDelim`、`common.NewJsonDecoderUseNumber`、`common.DecodeJsonFromDecoder`（`dto/openai_request.go:1283-1307`） | **`kitutil` 无对应导出**——需在 relaykit 内新增这三个（`kitutil/json.go` 的 `Codec` 接口已提供 `Unmarshal`，可承载等价实现），或改写该 `UnmarshalJSON` 走 `kitutil` 既有原语 |
| `common.ResponsesRequestMetadata`（`dto/opaque_openai_responses_request.go`） | **`kitutil` 无对应导出**——需迁入 `relaykit/types` 或在 relaykit 内新增，该字段是本地协议扩展（opaque request 的元数据） |
| `constant.ChannelType*`（`dto/channel_settings.go`） | 上游 relaykit `dto/channel_settings.go` 不含这些代码路径；本地是随 `ChannelOtherSettings` 的本地独有方法一起进来的，**其归位与本文件的方法归属（4.4）绑定**：删则随之删除，留则需在 relaykit 内给出不依赖宿主 `constant` 的判定 |

**风险提示**：以上 3 个新符号（`JsonDelim`/`NewJsonDecoderUseNumber`/`DecodeJsonFromDecoder`）与 1 个类型（`ResponsesRequestMetadata`）若迁入 relaykit，即为**对上游 relaykit 的本地分叉**。这与 12.9「不以上游 `feat/relaykit-tools` 分支为基线，避免与 main 产生不可合并分叉」的取舍同源，须一并评估：**优先用 `kitutil` 既有原语改写，只有确无等价时才新增**。新增项必须记入 `relaykit/VERSION`（阶段 1 建立的锚定文件），以便跟进上游时识别。

#### 12.0.5 `dto` 完整差集、字段对齐与 import 图（第 12 轮完成，关闭原未决项 21/22/23）

本节关闭第 10 节原第 21、22、23 项。全部数据由 `go/ast` 抽取（脚本口径见每条），**基线为 `HEAD`**；`git grep`-style 计数一律标注口径。

**一、函数规模与口径（`^func` 与 AST 双口径，均已复现）**

| 口径 | 本地 dto | relaykit/dto |
|---|---|---|
| `^func` 含测试 | **394** | **273** |
| `^func` 非测试 | **306** | **215** |
| AST 唯一键非测试（方法 + 包级函数） | **301**（方法 229 + 函数 72） | **210**（方法 159 + 函数 51） |

**修正 12.0.2 的口径标签**：12.0.2 写「本地 394 个函数、relaykit 273 个（`^func` 口径，含包级函数）」，**漏标「含测试」**——394/273 只在含测试口径下成立，非测试口径是 306/215。与 relaykit 对比时两口径不可混用。

**二、方法级差集（`接收者.方法名` 口径，非测试，AST 抽取）**

本地 229 个方法、relaykit 159 个；**本地独有 101 个、relaykit 独有 31 个**。本地独有的 101 个按文件归组：

| 文件 | 本地独有方法数 | 性质 |
|---|---|---|
| `channel_settings.go` | 42 | `ChannelOtherSettings` 的 Responses 能力解析（本地专有契约）。与 4.4/12.0.2 的 42 一致 |
| `responses_input_token_meta.go` | 27 | 本地独有类型 `responsesInputTokenMeta` 的 `append*` 族（token 计数元数据） |
| `openai_request.go` | 13 | `Message.ParseContentStrict/ParseToolCallsStrict`、`Reasoning.*`、`StreamOptions.*`、`OpenAIResponsesRequest.{UnmarshalJSON,HasCompactionTrigger}` |
| `openai_response.go` | 6 | `Usage.GetOutputTokenDetails`、`OpenAIResponsesResponse.Get*/Has*` 等 |
| `opaque_openai_responses_request.go` | 6 | 本地独有类型 `OpaqueOpenAIResponsesRequest` 的全部方法 |
| `openai_responses_compaction_request.go` | 3 | 补丁 2（presence 保真） |
| `responses_tools.go` | 2 | `effectiveToolsUncached` 等 |
| `task.go` / `claude.go` | 各 1 | `TaskResponse.IsSuccess`、`ClaudeRequest.SearchToolNameByToolCallId` |

**relaykit 独有 31 个方法**——**修正 §4.4**：§4.4 末尾写「relaykit 独有 5 个方法」，那是**仅 `openai_request`/`openai_response`/`claude` 三个文件**的口径。全包口径是 **31 个**，含 `AdvancedCustomConfig.*`（8）、`BillingUsage.*`（5）、`ChannelSettings.*`（3）、`GeminiChatResponse.*`（2）、`ImageRequest.*`（2）、`InputTokenDetails.*`（2）、`OpenAIResponsesRequest.MarshalJSON`、`ResponsesOutput.MarshalJSON`、`CachedTokenDetails.HasTokens`、`GeneralOpenAIRequest.MarshalJSON` 等。§9.1 第 6 行的「反向有 5 个」同错，一并改为 31。

**三、包级函数差集（非测试，AST 抽取）**

本地 72、relaykit 51；**本地独有 65、relaykit 独有 44**。本地独有的 65 个集中在 4 个文件：`channel_settings.go`（18）、`header_profile_version.go`（12）、`header_profile.go`（11）、`json_extra_fields.go`（4）——即**整个 `header_profile*` 与 `json_extra_fields` 家族是本地新功能**，不是被拆散的旧代码。relaykit 独有 44 个集中在 `billing_usage.go`（16）、`channel_settings.go`（14）、`usage_merge.go`（8）、`openai_request.go`（5）。

**四、文件级归属**

relaykit/dto 有、本地 dto 无：`alpha_search_request.go`、`billing_usage.go`、`legacy_dalle_image.go`、`reasoning_state.go`、`usage_merge.go`。本地有、relaykit 无：`channel_health.go`、`channel_routing.go`、`header_policy.go`、`header_profile.go`、`header_profile_version.go`、`json_extra_fields.go`、`midjourney.go`、`opaque_openai_responses_request.go`、`responses_input_token_meta.go`、`responses_routing.go`、`responses_tools.go`、`suno.go`、`task.go`、`tool_name.go`、`video.go`。

**归属决定（对齐上游做法）**：`midjourney.go`、`suno.go`、`task.go`、`video.go` 与上游宿主 dto 的 8 个保留文件同属**非协议域**，留在宿主 `dto`（迁 `relaykit/dto` 会无谓拉高 relaykit 的职责面）；其余 11 个本地独有文件承载协议扩展（header profile、responses 工具/token 元数据、opaque request），**随协议 dto 迁入 `relaykit/dto`**。文本注释与测试同步迁移。

**五、字段级对齐：136 个同名结构体，33 个字段集不同**

按「取 relaykit 后本地是否丢字段」二分，得出**需要动作的只有 16 个**，另 17 个取 relaykit 即为纯增益。第 10 节原第 23 项据此关闭。

**Group 1——本地有 relaykit 缺失的字段，必须处置（16 个）**

| 类型 | 本地独有字段数 | 处置 |
|---|---|---|
| `ChannelOtherSettings` | 20 | **保留**：`HeaderProfileStrategy`、`UserAgentStrategy`、`RequestBodyLimit`、`ResponsesCompaction*` 等是持久化到 DB 的渠道配置，字段名不可变 |
| `OpenAIResponsesCompactionRequest` | 20 | **保留**：本地整体扩展了该请求类型（`Preset`、`ContextManagement`、`EnableThinking`…） |
| `UserSetting` | 3 | **保留**：`HeaderProfiles`、`Record{Request,Response}ContentLog`（本地功能） |
| `OpenAIResponsesRequest` | 5 | **须改造**：`Extra`（补丁 2）可迁；**`effectiveToolsCache*` 4 个是未导出字段，不能加到 relaykit 类型**——Go 未导出字段属于声明包，宿主无法读写，须改为 relaykit 内的缓存方法或宿主侧 `map[*Request]…` |
| `Reasoning` | 5 | `Extra`（补丁 2）迁入；`effortRaw/effortSet/summaryRaw/summarySet` 为 presence 追踪，随类型迁入 relaykit 后在包内可见 |
| `StreamOptions` | 5 | 同上：`Extra` + 4 个 presence 字段（`includeUsageSet`/`includeObfuscationSet`/`includeUsageRaw`/`includeObfuscationRaw`；**第 39 轮修正**：原文写「3 个 presence 字段」，漏数了 `includeObfuscationRaw`） |
| `ResponsesOutput` | 4 | `Namespace`（命名空间工具，12.6 保留在宿主）；`EncryptedContent`/`Execution`/`Input` 需逐项确认上游是否另有承载 |
| `Message` / `ChatCompletionsStreamResponseChoiceDelta` | 各 1 | `ThinkingContent`——SenseNova/DeepSeek 的 thinking 历史回传，属用户明确要求保留的能力 |
| `MediaInput` | 2 | `AudioData`/`AudioFormat` |
| `MessageFile` | 2 | `FileUrl`/`MimeType` |
| `ResponsesStreamResponse` | 2 | `CallId`/`Error`——§9.1 第 5 行已记录与 relaykit 形状不同 |
| `ImageRequest` | 2 | `ExtraBody`/`ReturnBase64` |
| `IncompleteDetails` / `ResponsesOutputContent` / `ToolCallResponse` | 各 1 | `Reasoning` / `Refusal` / `Custom` |

**Group 2——只有 relaykit 有额外字段（17 个）**。**注意**：这 17 个是**按字段名集合**判定的；其中 `ClaudeServerToolUse` 另有 JSON tag 差异（`WebSearchRequests`，见 12.0.6），**不能一律视为「无需动作」**。仅按字段名口径时：`GeneralOpenAIRequest`(20)、`ClaudeMediaMessage`(6)、`ChannelSettings`(5)、`GeminiChatTool`(5)、`ClaudeWebSearchTool`(4)、`ClaudeServerToolUse`(3)、`ClaudeRequest`(2)、`FunctionCall`(2)、`InputTokenDetails`(2)、`ClaudeMessage`、`ClaudeUsage`、`FunctionCallingConfig`、`GeminiChatCandidate`、`GeminiChatResponse`、`GeminiUsageMetadata`、`Tool`、`Usage`（各 1）。

**六、import 图：成环风险不存在（关闭原第 22 项）**

§12.0.3 曾担心 `dto → constant → relaykit/types → dto` 成环。实测**不成立**：

- `constant` 包**不 import 任何 relaykit 包**（`git grep 'new-api/relaykit' HEAD -- 'constant/*.go'` 为空）——它是叶子，形不成环
- 本地 `dto` 当前**不 import relaykit**（0 文件）；relaykit/dto 只 import `relaykit/types` 与 `relaykit/relayconvert/kitutil`
- `relaykit/types` 与 `kitutil` **都不 import relaykit/dto**（各 0 文件）

因此 `dto → types`、`dto → kitutil` 两条新边都不会闭合成环。**但 `constant.ChannelTypeCodex` 等符号在 `relaykit/types` 中不存在**（实测 `ChannelTypeCodex`/`ChannelTypeOpenAI`/`ChannelTypeAzure` 在 relaykit/types 命中 0）——这不是环的问题，是归属问题，见 12.0.4 与未决项 27。

#### 12.0.6 字段名相同不等于兼容：tag 与 Go 类型两个维度（第 14 轮新增）

12.0.5 的差集只比**字段名集合**。字段名相同但 **JSON tag** 或 **Go 类型**不同，同样是破坏性的——前者静默改变线上/库中数据，后者直接编译失败。本轮补测两个维度（AST 抽取，非测试，136 个同名结构体）：

**一、JSON tag 差异：5 处（此前完全未知）**

| 类型.字段 | 本地 | relaykit | 字段类型 | 影响 |
|---|---|---|---|---|
| `ChatCompletionsStreamResponseChoice.Delta` | `delta,omitempty` | `delta` | **struct 值** | **空操作**：`omitempty` 对 struct 值无效，两侧都输出 `"delta":{...}` |
| `GeminiChatRequest.GenerationConfig` | `generationConfig,omitempty` | `generationConfig` | **struct 值** | **空操作**，同上 |
| `ToolCallRequest.Function` | `function,omitempty` | `function` | **struct 值** | **空操作**，同上 |
| `IncompleteDetails.Reason` | `reason,omitempty` | `reason` | `string` | **真实变化**：本地空串省略，relaykit 输出 `"reason":""` |
| `ClaudeServerToolUse.WebSearchRequests` | `web_search_requests` | `web_search_requests,omitempty` | `int` | **真实变化**：relaykit 在 0 时省略该键 |

**`omitempty` 只对 false/0/nil/空串/空集合生效，对 struct 值无效**——所以前 3 行两侧输出完全一致。两处真实变化的风险判定与逐项结论见 12.0.8。

**二、Go 类型差异：真差异 6 处，另有等价拼写 21 处（**第 30 轮修正**：原文写「21 处，其中真差异 6 处、等价拼写 15 处」，三处数字全错——等价拼写组实测 **21** 项，不是 15）

口径：工具的「类型不同」维度报 **6 处**（`[]constant.EndpointType` 被单列，因为它不是拼写等价）。**等价拼写是工具单列的一组，不并入类型维度**（`tools/relaykit-dto-compat/main.go` 的 `build()` 里两处 `spellingsEquivalent` 分支：类型或签名文本不同且拼写等价时进 `tagEquivalents`，否则分别进 `typeSet` / `sigDiff`；此处按符号而非行号引用——该文件是本地新增、不在任何基线提交里，行号引用会随工具演进漂移且不被 citation 门禁覆盖）——实测该组 **21 项 = 20 个字段 + `NewNotify` 的 1 个形参**，涉及 **7 个类型 + 1 个函数**。两组合计 **27 处**。

**类别 A——纯写法差异，无语义变化（21 项 = 20 个字段 + 1 个形参；**第 30 轮修正**，原文写「15 处」，与紧随其后的枚举相加不符）**：`interface{}` 与 `any` 是同一类型的两种拼写。逐类型计数（工具实测）：`GeminiModel` 13、`DifferenceItem` 2、`GeminiFunctionResponse` 1、`Notify` 1、`RealtimeSession` 1、`ResponsesOutputContent` 1、`Tool.InputSchema` 1 = **20 个字段**，加 `NewNotify` 的形参 1 = **21 项**。**无需动作**，但会在 diff 里产生噪音，评审时不要误判为不兼容——本工具把它们单列一组正是为此。

**类别 B——类型换了但语义等价，改的是引用而非形状（1 个）**：`OpenAIModels.SupportedEndpointTypes` 由 `[]constant.EndpointType` 变为 `[]types.EndpointType`。两者是同一底层类型（12.0.1 已证 `constant.EndpointType` 是 `types.EndpointType` 的别名），**JSON 形状不变**，但**仍需改 import**（`constant` 不能出现在 relaykit 内，见 12.0.4）。工具的「类型不同」维度把它计入 6 处，是对的——它不是拼写等价。

**类别 C——真正的语义变化，必须处置（5 个字段 / 4 个类型；第 40 轮修正**：原表头写「4 个字段 / 3 个类型」，与下表 5 行字段（`Text`/`Arguments`/`IncludeThoughts`/两个 `CreatedAt`）及同节计数口径行的「4 个类型共 5 个字段」均不符**）**：

| 类型.字段 | 本地 | relaykit | 为何是语义变化 |
|---|---|---|---|
| `ResponsesStreamResponse.Text`、`.Arguments` | `string` | `*string` | `omitempty` 对 `string` 与 `*string` 语义不同：本地空串被省略，relaykit 只有 `nil` 才省略、空串会输出 `"text":""`。**且 `string`→`*string` 是编译期破坏**，所有读写点必须改 |
| `GeminiThinkingConfig.IncludeThoughts` | `bool` | `*bool` | 同上，且这是**思考等级**链路的一环。本地 `relay/channel/gemini/relay-gemini.go` 有 **8 处**直接赋值/读取（`:148`、`:167`、`:171`、`:192` 字面量 `true`；`:302`/`:305` 赋 `true`/`false`；`:315` 赋变量；`:339` 读取），改为指针后每处都要改成 `common.GetPointer[...]` 或解引用 |
| `OpenAIResponsesResponse.CreatedAt`、`OpenAIResponsesCompactionResponse.CreatedAt` | `int` | `IntValue` | `IntValue`（`relaykit/dto/values.go:32`）自定义了 `UnmarshalJSON`，**接受 int / float / string 三种形态**；本地 `int` 只接受数字。取 relaykit 版是**能力增强**，但需确认本地没有依赖「字符串形态应当报错」的行为 |

**计数口径（第 15 轮用工具复核；第 30 轮修正三处数字）**：类型维度 = **真差异 6**（类别 B 1 + 类别 C **4 个类型共 5 个字段**——`ResponsesStreamResponse` 的 `Text`/`Arguments` 各算一个字段）；**等价拼写 21 项是工具的独立组，不并入类型维度**（原文写「等价拼写 15 + 真差异 6」把两者相加，实为并列关系且 15 应为 21）。签名维度 = 12（12 个 `IsStream`；`NewNotify` 的 `interface{}`/`any` 已归入等价拼写，不计入签名差异）。**上一轮曾把这两个数写成「类型真差异 3 处」「签名差异 13 处」，前者漏了 `OpenAIModels`、后者把等价拼写算作了差异**，见第 15 轮说明。

**结论**：12.0.5 第五节「17 个取 relaykit 即纯增益」的表述**不完整**——那 17 个是**按字段名集合**判定的；按 tag 与类型两个维度复核后，其中 `ClaudeServerToolUse` 另有 tag 差异（`WebSearchRequests`，见 12.0.6），须逐项确认。**第 30 轮修正**：原文写「其中 10 个另有 tag 或类型差异」——那 10 个是**全部 136 个同名结构体**口径（tag 组 5 个类型 + type 组 5 个类型，两组无交集），与 Group 2 的交集只有 `ClaudeServerToolUse` 1 个；把全量口径的数字安到 Group 2 上，会让读者以为 17 个里有 10 个要处置。**第 30 轮修正**：紧接的「真正的『零动作』只有类别 A 的 **8 个类型**」——实测是 **7 个类型 + 1 个函数**（`DifferenceItem`、`GeminiFunctionResponse`、`GeminiModel`、`Notify`、`RealtimeSession`、`ResponsesOutputContent`、`Tool.InputSchema`，加 `NewNotify` 的形参），不是 8 个类型。

**阶段 4 的语义对齐清单（§9.1）与阶段 2 的完成判据都应纳入这两个维度**：字段名集合相同只是第一层，tag 与类型是第二、三层。已列为未决项 30。

**三、补齐另外两个可能的盲维度（同轮，结论为「无新差异」）**

字段名差集还有一个固有盲区：`ast.Field.Names` 对**匿名嵌入字段**为空，因此嵌入字段在字段名差集与类型差集里**都是隐形的**。另补两个维度：

- **嵌入（匿名）字段**：本地 7 个、relaykit 7 个，**逐类型对比零差异**（都在 `embedding.go`、`openai_response.go`，均为 `Usage`/`Message` 的 JSON 嵌入）
- **函数/方法签名**（此前只比了名字，没比参数与返回值）：135 个同名函数中 **13 处签名文本不同**——12 个是 `IsStream` 的 `*gin.Context → *http.Request`（与 §7「阶段 2 的前置」记录的 12 个方法完全一致，互为印证），1 个是 `NewNotify` 的 `interface{} → any`（等价写法，工具归入等价拼写组，**故签名真差异为 12**）。**无未知项**

至此，dto 的兼容性核查覆盖 **7 组**（以 `tools/relaykit-dto-compat/` 的实际输出为准）：字段名（12.0.5）、JSON tag、Go 类型（本节）、嵌入字段、**共有字段的声明顺序**（第 31 轮新增，见下）、函数/方法签名，以及等价拼写组（单列、不计为差异但同样进 `--strict` gate）。**第 31 轮修正**：原文写「四个维度」并把「嵌入字段与函数签名」并作一项，随后又写「实际工具一直报 5 组……共 6 组」——两次都漏了等价拼写组（它自初版起就打印，第 30 轮起也进 gate）。组数以工具打印的 `###` 分节数为准。

**四、工具化（第 15 轮新增；第 31 轮补声明顺序组）**

本节与 12.0.5 的全部数字，已固化为可重跑的工具：

- `tools/relaykit-dto-compat/`（独立 Go module，含单元测试，不进根 module 的 `./...` 构建）
- `scripts/relaykit-dto-compat.sh`（从 git 拉两侧源码——本地取 `HEAD`、relaykit 侧默认 `KIT_REF=upstream/main`；**核对本表基线须显式 `KIT_REF=789c97019`**，否则上游一漂移即假红——第 40 轮实测——再调用上述工具）

```bash
KIT_REF=789c97019 scripts/relaykit-dto-compat.sh            # 人读报告（钉文档锚点）
KIT_REF=789c97019 scripts/relaykit-dto-compat.sh --markdown # 供粘进审查记录
KIT_REF=789c97019 scripts/relaykit-dto-compat.sh --strict   # 计数漂移即非零退出（基线即本节记录值）
```

**这解决了本文档最大的一个结构性弱点**：此前每一次「上游 relaykit 演进后需重新核对」都得靠人肉复核，而 12.0.6 已证明人肉复核会漏维度。现在比较是一条命令；`--strict` 把当前基线（fieldSet=33、tag=5、type=6、embed=0、**order=4**、sig=12、**equiv=21**）变成门禁，**上游一漂移就报警**，而不是等到阶段 2 编译失败才发现。**第 31 轮修正两处**：一是原文的基线列表漏了 `equiv`（等价拼写 21，第 30 轮才纳入门禁）；二是**「打印了几组」与「gate 了几组」曾经不等**——工具打印 6 组而 `--strict` 只 gate 5 项，等价拼写组因此长期在门外。基线是**时间点快照**，本地 `dto` 或锚定的 relaykit 提交变动时须有意重录——这正是门禁的意义。

工具同时修掉了本节上一轮的两处计数错误（见下）。

#### 12.0.7 三个符号与缓存字段的归位（第 16 轮决定，关闭未决项 27、29）

**未决项 27 的第一半——`JsonDelim` / `NewJsonDecoderUseNumber` / `DecodeJsonFromDecoder`：关闭，直接换 stdlib。**

实测 `common/json.go:11,32,38`：这三个符号是 `encoding/json` 的**薄包装**——`type JsonDelim = json.Delim`、`NewJsonDecoderUseNumber` 就是 `json.NewDecoder` + `UseNumber()`、`DecodeJsonFromDecoder` 就是 `decoder.Decode(v)`。而 relaykit/dto **本就 import `encoding/json`**（`relaykit/dto/openai_request.go` 等）。因此 `dto/openai_request.go:1283-1307` 的三处用法改为：

```go
decoder := json.NewDecoder(bytes.NewReader(input))
decoder.UseNumber()
token, err := decoder.Token()
delimiter, ok := token.(json.Delim)
err = decoder.Decode(&item)
```

**不新增到 relaykit、不分叉**——只是补 `bytes`、`io` 两个 stdlib import。这条同时说明：把 `common` 的 stdlib 包装当作「宿主能力」是误判，它们没有宿主语义。

**未决项 27 的第二半——`ResponsesRequestMetadata`：关闭，判为宿主类型，迁入 `relaykit/types` 代价过高且无必要。**

实测它不是一个类型，而是一个 **488 行的文件**（`common/responses_request_metadata.go`：`ResponsesRequestMetadata`、`ResponsesRequestShapeMetadata`、`ResponsesJSONValueKind`、`ScanResponsesRequestMetadata` 及一组扫描辅助），**并依赖另一个 650 行的扫描器 `common/json_top_level_replay.go`**（`jsonTopLevelScanner`、`LocateTopLevelJSONString`、`SizedReader`、`TopLevelJSONStringReplacementReader`）。后者还被 `relay/responses_handler.go` 单独使用（不是 dto 专属）。把它整体迁入 relaykit，等于把 ~1100 行本地 responses 解析基础设施拉进套件，远超「为让 `dto` 通过边界测试」所需。

**归位方式**：`dto/opaque_openai_responses_request.go`（64 行）**不迁入 `relaykit/dto`，保留在宿主 `dto`**，与 `midjourney.go`/`suno.go`/`task.go`/`video.go` 同类。理由：`OpaqueOpenAIResponsesRequest` 是本地独有类型，实现 `dto.Request` 接口（含 `IsStream`），其未导出字段是 `common.ResponsesRequestMetadata`；宿主 `dto` 保留 `dto.Request` 接口定义（供宿主类型实现），因此不需要 relaykit/dto 依赖宿主。

**副作用（可接受）**：本地协议请求类型将**分居两包**——协议类型在 `relaykit/dto`，`OpaqueOpenAIResponsesRequest` 在宿主 `dto`。两者通过 relaykit 的 `dto.Request` 接口（`IsStream(*http.Request)`）协作。**阶段 2 完成判据须包含：`dto/opaque_openai_responses_request.go` 仍在宿主、`boundary_test.go` 通过**。

**未决项 29——`OpenAIResponsesRequest.effectiveToolsCache*` 四个未导出字段：关闭，改为 relaykit 内的缓存方法。**

`dto/responses_tools.go`（111 行）的 `EffectiveTools()` 用 `Tools`+`Input` 拼接的签名做缓存键，四个字段（`effectiveToolsCache{,Err,Ready,Signature}`）都是未导出。Go 里未导出字段属于声明包，**不能声明在 relaykit 的类型上**。决定：

- **把 `EffectiveTools()` 迁入 `relaykit/dto`**，缓存字段作为 relaykit 类型 `OpenAIResponsesRequest` 的未导出字段——**在 relaykit 包内声明即合法**（relaykit 是独立 module，字段属于它自己的包）
- 这正是上游做法：relaykit/dto 已有未导出字段（如 `Reasoning.effortRaw`）
- `effectiveToolsUncached` 与 `parseResponsesToolsField` 随之迁入（它们只依赖 `kitutil` 的 `GetJsonType`/`Unmarshal`/`RawMessage`）
- 13 个调用点（`controller/responses_native_tool_identity.go:193`、`dto/openai_request.go:1234`、`relay/common/relay_info.go:598`、`relay/common/responses_input_filter.go` 四处、`relay/helper/valid_request.go:128/183`、`service/openai_chat_responses_compat.go:165`、`service/openaicompat/responses_input_tools.go:274`、`service/openaicompat/responses_to_chat_request.go:28`、`service/responses_request_shape.go:277`）**改 import 即可**，调用形式不变（方法仍在 `*OpenAIResponsesRequest` 上）

**为什么这解决了问题**：原表述「取 relaykit 类型后须改为方法级缓存或宿主侧旁路」暗示要在宿主侧做旁路（如 `map[*Request]entry`），那是脆弱的。真正的问题只是「字段不能声明在**别的包**的类型上」——把方法连字段一起放进 relaykit，问题消失。

**阶段 2 的动作**：迁 `EffectiveTools` 及其两个辅助函数到 `relaykit/dto`，13 个调用点改 import。**无缓存语义变化**（同包内，逻辑不变）。

#### 12.0.8 tag 与类型差异的逐项决定（第 17 轮，关闭未决项 30）

**决定：全部 11 处采用 relaykit 侧，零处打补丁**（**第 30 轮修正**：原文写「15 处」，与本节自列的三类 5 + 6 + 15 = 26 对不上；真正需要逐项决定的是 **tag 5 + 类型 6 = 11 处**，等价拼写 21 项零动作、不进入该计数）。但理由分三类，不能一律说「无影响」。

**类别 I——tag 差异 5 处，其中 3 处是空操作、2 处是真实行为变化**

关键判据：`encoding/json` 的 `omitempty` 只对 **false / 0 / nil 指针或接口 / 长度 0 的 string / array / map / slice** 生效，**对 struct 值不生效**。据此：

| 类型.字段 | 本地 | relaykit | 字段类型 | 判定 |
|---|---|---|---|---|
| `ChatCompletionsStreamResponseChoice.Delta` | `delta,omitempty` | `delta` | `ChatCompletionsStreamResponseChoiceDelta`（**struct 值**） | **空操作**——struct 值永不被 `omitempty` 省略，两侧输出都是 `"delta":{...}` |
| `ToolCallRequest.Function` | `function,omitempty` | `function` | `FunctionRequest`（**struct 值**） | **空操作** |
| `GeminiChatRequest.GenerationConfig` | `generationConfig,omitempty` | `generationConfig` | `GeminiChatGenerationConfig`（**struct 值**） | **空操作** |
| `IncompleteDetails.Reason` | `reason,omitempty` | `reason` | `string` | **真实变化**——本地空串省略，relaykit 输出 `"reason":""` |
| `ClaudeServerToolUse.WebSearchRequests` | `web_search_requests` | `web_search_requests,omitempty` | `int` | **真实变化**——relaykit 在 0 时省略该键 |

两处真实变化的**风险与结论**：
- `IncompleteDetails` 只在两个构造点产生（`relay/channel/openai/responses_via_chat.go:951/955`、`service/openaicompat/chat_to_responses_response.go:51/53`），**两处都设非空 Reason**（`"max_output_tokens"` / `"content_filter"`），故实际输出不变。采用 relaykit，**无需动作**
- `ClaudeServerToolUse` 本地只有 1 个字段（relaykit 有 4 个），生产代码**从不读取**该结构（只在 dto 内声明），省略行为无影响。采用 relaykit

**类别 II——Go 类型差异 6 处**

| 类型.字段 | 本地 | relaykit | 判定 |
|---|---|---|---|
| `ChatCompletionsStreamResponseChoice`… 无关 | — | — | — |
| `ResponsesStreamResponse.Text`、`.Arguments` | `string` | `*string` | **采用**。本地构造点**不显式写空串**（已逐点核实），故「空串 vs nil」的 `omitempty` 差异不触发；`*string` 只是把「显式空串」与「未设置」分开。**须改所有读写点**（编译期破坏） |
| `GeminiThinkingConfig.IncludeThoughts` | `bool` | `*bool` | **采用**。relaykit 用 nil 表示「客户端未指定」，语义比 `bool` 更精确，且**正是思考等级链路**所需（`shared/gemini/request.go:168` 的 `if native.IncludeThoughts == nil` 依赖它）。本地 8 处赋值点（`relay/channel/gemini/relay-gemini.go:148/167/171/192/302/305/315/339`）改 `kitutil.GetPointer(...)` 或解引用 |
| `OpenAIModels.SupportedEndpointTypes` | `[]constant.EndpointType` | `[]types.EndpointType` | **采用**（别名等价，JSON 形状不变）；改 import 即可，与 12.0.4 同因 |
| `OpenAIResponsesResponse.CreatedAt`、`OpenAIResponsesCompactionResponse.CreatedAt` | `int` | `IntValue` | **采用，纯增益**。`IntValue.MarshalJSON` 输出 `int(i)`，**与本地 `int` 完全相同**；`UnmarshalJSON` 额外接受 float 与 **string**。是超集，无行为回退 |

**类别 III——等价拼写 21 项**（**第 30 轮修正**，原文写 15）：`interface{}`/`any`，采用 relaykit，零动作。

**结论**：未决项 30 关闭。阶段 2 的实际工作量是**改 8 处 `IncludeThoughts` 赋值点 + 所有 `Text`/`Arguments` 读写点 + 4 处 import**；tag 差异中 3 处无需动作、2 处风险已排除。

**这一节同时修正了 12.0.6 的一个过度警告**：那里写「tag 差异会**静默改变序列化输出**」，对 3 个 struct 值字段不成立——`omitempty` 对 struct 值无效，那是 Go 的既定语义。**凡「tag 不同即输出不同」的判断，必须先看字段类型**。

### 12.1 协议层语义 → 提上游（第 10 节第 1 项已完成的产出）

| 项 | 决定 | 依据 |
|---|---|---|
| `ParseContentStrict` / `ParseToolCallsStrict` | **迁入 `relaykit/dto`**，并向上游提 PR | relaykit 的 `ParseToolCalls()`（`relaykit/dto/openai_request.go:624`）静默吞掉 unmarshal 错误、`ParseContent()`（`:686`）类型不匹配返回空列表。AGENTS.md 第 3 节「禁止静默容错」。这是修上游的缺陷，不是加本地特性 |
| think 标签剥离（`splitThinkTaggedText` 等） | **迁入 relaykit**，作为 Claude 历史回传能力；**门控留在宿主** | 剥离是文本协议处理（`<think>` 标签解析），门控是部署策略。拆分后 relaykit 提供无门控的纯函数，宿主用 `convmeta.Options` 钩子决定是否调用——与既有 `PreserveThinkingSuffix func(modelName string) bool` 同型 |

### 12.2 本地专有错误符号 → 迁入 `relaykit/types`

**决定**：4 个 `ErrorCode` 常量与 2 个判定函数全部迁入 `relaykit/types`。

**依据**：上游 `relaykit/types/error.go` 已经承载宿主侧语义的码——`ErrorCodeDoRequestFailed`、`ErrorCodeGetChannelFailed`、`ErrorCodeChannelNoAvailableKey`、`ErrorCodeChannelResponseTimeExceeded`。转换套件的错误词汇表本就不是纯转换语义。若把 4 个码留在宿主，会出现两个 `ErrorCode` 类型，所有跨边界的 `switch` 与比较都要转换，代价远大于收益。

**影响**：`relay/channel/api_request.go`、`relay/channel/openai/responses_response_body_read.go`、`service/channel.go` 等 15 个非测试文件的 import 路径改到 `relaykit/types`（排除 `types/error.go` 定义处；多数本来就要改）。

### 12.3 `*WithError` 系列 → 删除，不保留包装层（第 35 轮按上游终态修正范围）

**决定**：迁移时直接删除 4 个 `*WithError` 函数（与上游一致，上游 `service/` 下已删、0 命中），9 个生产调用点改为调用 relaykit 的 error 返回接口（上游形态下即 `service.ConvertResponse` / `ConvertStreamResponse` 包装，见 12.20）。**「4 个无 error 旧包装一并删除」这半句撤回**——上游终态把这 4 个函数**保留为永久 facade**（`upstream/main:service/convert.go` 27 行，5 个同名函数一行转发 `relayconvert.*`，旧签名不变）；本地删还是留，**第 44 轮随未决项 33 定案为 C（照抄上游终态）：保留为 facade**，见 12.20。

**依据**：上游 `ResponseConverterFunc`（`response_registry.go:22`）签名本就是 `(any, *dto.Usage, error)`；`WithError` 是本地历史命名。4 个无 error 包装的生产调用点为 **0**（仅测试引用）——但「无生产调用点」不等于「可删除」：上游保留它们的目的是**让旧调用点零改动**。

**影响（第 35 轮精确化；第 44 轮定案）**：受影响的测试调用点共 **4 个文件、82 处**（12.16.1 逐文件清点）：`*WithError` 的 17 处随本决定必改（**第 48 轮：通道级另有 `CovertOpenAI2Gemini` 的 11 处必改，见 12.20.1 通道级测试面**）；4 个非 WithError 包装的 21 处**是否要改取决于未决项 33**——**第 44 轮定案为 C：照抄上游终态则零改动**。原表述「4 个测试文件需改写调用」与 12.16.1 的「确认这些既有用例在切换后仍通过」相互矛盾，两处已同批修正。无生产代码语义变化。

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

`state` 是 `info.EnsureClaudeConvertInfo()` 返回的 `*convmeta.ClaudeConvertInfo`（`convmeta/meta.go:58`），**其字段全部导出**（`Index int`、`LastMessagesType string`、`ToolCallBaseIndex int` 等），且经 `convmeta.Meta` 接口对宿主可达——所以宿主**确实能读到** `state.Index`。但「读得到」不等于「删得掉」：索引由 relaykit 在**产出事件时**按含 thinking 的顺序分配完毕，宿主拿到的是已生成的 `[]*dto.ClaudeResponse`，删块后

- 后续块的索引**无法重排**（relaykit 已按含 thinking 的顺序分配完毕）
- 产出**索引空洞**：对未 `content_block_start` 的 index 发 delta

Anthropic SSE 状态机要求 `start → delta* → stop` 逐 index 闭合，索引空洞会被客户端拒绝。

#### 12.4.2 决定

**在 relaykit 内门控，用 `convmeta.Options` 钩子**——与既有的 `PreserveThinkingSuffix func(modelName string) bool` 同型：

```go
// convmeta.Options 新增
RetainReasoningInClaudeResponse func(modelName string) bool   // nil = 保留（维持上游默认）
```

**注意 nil 语义是「保留」而非「不保留」**——与 `PreserveThinkingSuffix`（`nil = never preserve`）**方向相反**，不要照抄。理由：上游本无此钩子，其默认行为是**无条件暴露** reasoning；若 `nil = 不保留`，则所有不设钩子的调用点（上游自身测试、独立 relaykit 用户）行为会从「暴露」翻转成「不暴露」，是破坏性默认值变更。宿主 new-api 显式把钩子设为 DeepSeek 判定，独立使用者不设即维持上游行为。**且钩子是单参的，本地门控需要三个模型名**（`HasDeepSeekModelName` 综合 `Model`/`OriginModelName`/`UpstreamModelName`）——由 relaykit 在调用点对每个名字各调一次取或，见 12.4.4 第 3 点。这两处早期都写错了，修正依据见 12.4.4。

本钩子是**响应**方向；本迁移另有一个**请求**方向的钩子 `RetainEmptyReasoning`（§4.2，对应 DeepSeek 空 thinking block 保留）。两者签名相同、语义与落点不同，实现时不要合并。**两个钩子共用的唯一准则不是某个字面值，而是「不设钩子时行为等于上游」**——各自落到哪一侧由上游默认行为决定：响应方向上游默认**暴露**，故 `nil = 保留`；请求方向上游默认**不保留**空块（实测 `claude_messages/to_oai_chat_req.go` 的 switch 只有 `text`/`image`/`tool_use`/`tool_result` 四个 case，无 `case "thinking"`，空块无从保留），故 §4.2 的 `nil = 不保留（与上游一致）` 成立、不改。**两者字面值相反但准则一致**，不能为了「看起来统一」而改成同一个值。

**可达性分两侧，成本不同**（已实测）：

- **Chat→Claude 流式**（`internal/oai_chat/to_claude_messages_resp.go:111` `StreamResponseOpenAI2Claude`）：**已接收 `info convmeta.Meta`**，且 `Meta` 已含 `ConvOptions() *Options`（`convmeta/meta.go:18` 接口起、`:53` 该方法）。在 `:342` 取出 `reasoning` 之后、进入 `reasoning/text` 二选一之前把被抑制的 reasoning 归零（**不是**在内层 `if reasoning != ""` 建块处加判断——那样会连同一 chunk 的 text 一起丢），并对三个模型名各调一次钩子取或，**无需新增透传参数**。精确插入点与理由见 12.4.4 第 3、4 点
- **Chat→Claude 非流式**（同文件 `:449` `ResponseOpenAI2Claude`）：同样已接收 `info convmeta.Meta`，同样零透传成本。门控落在 `:463` 的 `if reasoningContent != ""` 之前
- **Responses→Claude 非流式/流式**（`internal/oai_responses/to_claude_messages_resp.go:15`、`internal/oai_responses/to_claude_messages_stream_resp.go:82`）：**不接收 `Meta`**（前者只收 `*dto.OpenAIResponsesResponse`，后者的 `ResponsesToClaudeStreamState` 不持有 `info`）。若这两条路径也需要门控，才需要 6.1 第 3 点所述的透传改造

钩子只回答策略、不捕获请求状态（见 12.10）。**这条推翻了第 6.1 节「响应方向不打 relaykit 补丁」的结论**——该结论的前提是「后置过滤可行」，现已被推翻。6.1 关于「不能按客户端意图门控」的三条论据仍然成立（客户端意图在响应阶段取不到正确值），但结论要改为：**门控依据是宿主策略，不是客户端意图，且落点在 relaykit 内部的索引分配之前**。

#### 12.4.3 AWS Converse 路径不得套用 DeepSeek 策略

**早期版本写「AWS 路径用同一 helper」，会引入回归。** 实测：

- `relay/channel/aws/converse.go:748` **无条件**把 Bedrock `ContentBlockMemberReasoningContent` 转成 `{Type: "thinking"}` 块
- 全文件 **0 处**调用 `shouldExposeReasoningAsClaudeThinking`

而该策略实测就是 `hasDeepSeekModelName`（`service/convert.go:526`）——**只对 DeepSeek 返回 true**。套用后 Bedrock Claude 的 reasoning 会被**新抑制**，这是行为回归。

且 AWS 流式（`:1110`）在 switch 内联发 `thinking_delta`，不存在「自有生成逻辑之后」的单一过滤点。

**决定**：AWS 路径**维持现状**（无条件暴露），除非有独立理由要求门控。若将来需要，必须为其单独定义策略函数，不得复用 DeepSeek 判定。

**影响**：`convmeta.Options` 新增 1 个钩子；relaykit 的 `oai_chat/to_claude_messages_resp.go` **两条入口**（`:111` 流式、`:449` 非流式）各加一处索引分配前的门控；宿主 `ConvOptions()` 填充该钩子。第 6.1 节需同步修订。**注意**：`to_claude_messages_stream_resp.go` 只存在于 `internal/oai_responses/`（`oai_chat/` 下没有该文件，流式入口就在 `to_claude_messages_resp.go` 内），而 `oai_responses` 一族不接收 `Meta` 且本地不涉及该路径（12.4.2），故**不是**本门控的落点。

#### 12.4.4 门控的落地时机、钩子契约与插入点（2026-10-05 补充，第 27 轮）

**本节修正 12.4 的五处自相矛盾或遗漏，均经命令实测。**

**(1) 门控必须排在阶段 4，不能留到阶段 5。** 早期阶段表把 12.4 的门控排在阶段 5，但阶段 4 的判据是「`TestGolden` 改指 relaykit 后 8 个快照逐字节一致」，其中 4 个门控快照（`response/openai_to_claude`、`stream/openai_to_claude`、`response/openai_to_claude_deepseek`、`stream/openai_to_claude_deepseek`）在**非 DeepSeek** 侧要求「reasoning 被丢弃」。而 relaykit 默认**无条件暴露** reasoning——实测 `ResponseOpenAI2Claude(resp, nil)`（`oai_chat/to_claude_messages_resp.go:449`）在 `info == nil` 时照样产出 `{Type: "thinking"}` 块；`Values.ConvOptions()`（`convmeta/meta.go`）在 `Options` 未设时返回 `&Options{}`（钩子为 nil）。因此阶段 4 一旦切换而不门控，非 DeepSeek 快照必然多出 thinking 块 → 判据必红；且豁免条款只认「处置清单第 6 项（9.1/9.2/9.2.1/9.2.3；9.2.2 归第 17 项；**第 40 轮修正**：原文写 9.1/9.2，第 37/38 轮已扩）」，门控不在其中 → **阶段 4 无法通过**。故门控随阶段 4 的切换同时落地（处置清单 13），阶段 5 只核验其位置语义。

**(2) 钩子的 nil 语义写反了。** 12.4.2 的注释是 `nil = 不保留（与上游一致）`。**前半句与后半句不能同真**：上游没有这个钩子，其行为是**无条件保留**。若按 `nil = 不保留` 实现，则所有不设钩子的调用点（上游自己的测试、独立使用 relaykit 的用户）行为从「暴露」翻转为「不暴露」，是**破坏性默认值变更**。正确语义是 **`nil = 保留（维持上游默认）`**——宿主（new-api）显式把钩子设为 `hasDeepSeekModelName`，独立使用者不设则保持上游行为。这一改同时让上游两条依赖默认暴露的测试（`to_claude_messages_resp_test.go:82`、`:125`，该文件 0 处 `Options:`）**保持绿色、无需改动**；若按原语义实现，这两条必红（本地改动清单第 9 行）。**故这两条测试是 nil 语义实现的验证条件，不是待改对象。**

**决定性反证（同一输入、相反断言）**：本地 golden `response/openai_to_claude` 的 fixture 模型名是 `gpt-test` 且带 `reasoning_content`，快照里 thinking **0 次**（门控关闭）；上游 `to_claude_messages_resp_test.go:82` 用**同一个** `gpt-test`、同样带 reasoning，却断言 `resp.Content[0].Type == "thinking"`。**两者对同一输入断言相反，只有「宿主配置门控」能同时满足**——这正是 nil 语义必须是「保留」的原因：上游测试不设钩子（nil），要得到 thinking；本地宿主显式设钩子（非 DeepSeek 返回 false），要丢弃 thinking。

**(3) 单参钩子复现不了本地的三名字门控。** 本地 `shouldExposeReasoningAsClaudeThinking` → `HasDeepSeekModelName(model, info)` 综合 `openAIResponse.Model`、`info.OriginModelName`、`info.UpstreamModelName` **三者**判定（`service/convert.go:537`，**HEAD 口径**），注释原文：「Routing may rewrite the public model name, so all available names must participate in the capability decision」。而钩子签名 `func(modelName string) bool` 只收一个名字。12.4.2 写「加**一次**判断」会丢掉另外两个名字——当公开模型名非 DeepSeek、却被路由到 DeepSeek 上游时（`UpstreamModelName` 命中），reasoning 会被错误抑制，是**行为回归**。解法与上游 `ApplyReasoning` 对 `ShouldPreserveThinkingSuffix` 的用法同型（`shared/claude/reasoning.go:25-26` 连调两次）：**relaykit 在调用点对每个可用名字各调一次并取或**——

```go
opts := info.ConvOptions()
retain := opts.RetainReasoningInClaudeResponse(openAIResponse.Model) ||
    opts.RetainReasoningInClaudeResponse(info.GetOriginModelName()) ||
    (info.HasChannelMeta() && opts.RetainReasoningInClaudeResponse(info.GetUpstreamModelName()))
```

这样钩子保持「只答策略、不捕获请求状态」（12.10），三名字判定由调用点完成。

**(4) 插入点必须在索引分配之前，且不能只守内层分支。** 上游结构是：

```go
reasoning := chosenChoice.Delta.GetReasoningContent()
textContent := chosenChoice.Delta.GetContentString()
if reasoning != "" || textContent != "" {
    if reasoning != "" { /* thinking 块，内含 stopOpenBlocksAndAdvance() */ }
    else               { /* text 块 */ }
} else { isEmpty = true }
```

只在内层 `if reasoning != ""` 加判断是错的：门控关闭时该分支被跳过，但 `else`（text）也不会执行，**同一 chunk 里的 text 被静默丢弃**。正确做法是在**外层条件之前**把被抑制的 reasoning 归零：

```go
reasoning := chosenChoice.Delta.GetReasoningContent()
if reasoning != "" && !retain { reasoning = "" }
```

归零后，门控关闭的行为与「reasoning 不存在」完全一致——与本地两个独立闭包（`appendReasoningDelta` 早返回 `:801`、`appendTextDelta` 独立执行 `:830`）等价。**这是「门控在索引分配之前」的精确含义**：不只是 `stopOpenBlocksAndAdvance()` 之前，而是**进入 `reasoning/text` 二选一之前**。

**(5) 同 chunk 的 reasoning+text（+tools）是本地能力，上游没有，且未列入 9.1。** 上游的 `if reasoning != ""` / `else` 是**互斥**的：同一 chunk 同时含 reasoning 与 text 时**只发 thinking、丢弃 text**；若该 chunk 还含 `tool_calls`，则 `if len(chosenChoice.Delta.ToolCalls) > 0` 分支优先，reasoning 与 text **都**被丢弃。本地则用两个独立闭包，三者都发——本地有专门用例 `TestStreamResponseOpenAI2ClaudeEmitsThinkingTextAndToolBlocksFromOneChunk`（`service/convert_claude_stream_test.go:119`）钉住该行为。**这是能力差异，不是严格度差异**：12.16.1 说「阶段 4 的待办不是补用例而是确认既有用例仍通过，切换会让它们失败得其所」——但这条用例的失败**不得其所**，它是本地刻意支持的渲染能力被上游丢弃。故须把该差异**补入 9.1 清单**并明确取舍（保留 = 需再打补丁，放弃 = 记入行为变更），不能当作「严格度丢失」默认放行。

**待阶段 4 实测的边界**：以上 (2)(3)(4) 的实现细节读自 `upstream/main` 源码（签名级与结构级证据），**未经编译验证**——relaykit 目录在阶段 1 才拷入本仓库。钩子改名为 `RetainReasoningInClaudeResponse` 后的实际字段名、`ConvOptions()` 快照的填充时机，须到阶段 4 确认。

### 12.5 DTO 独有方法归属

**⚠ 本表只覆盖 4.4 的 23 个方法（3 个文件），不是 dto 包的全貌。** 全包是本地 394 个函数 vs relaykit 273 个（`^func` 口径、含测试；非测试 306 / 215），另有 33 个类型字段不兼容（12.0.1，工作区口径 34）。完整清单见 12.0.5。

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
| `Usage.EnsureResponsesTokenDetailObjects` | 3（工作区） | **A** | relaykit 无等价物（全仓零命中）。最近邻 `relayconvert.UsageFromChatUsage`（`relaykit/relayconvert/internal/oai_chat/to_oai_responses_resp.go:146`）有三处语义差：只在 4 项 detail 任一非零时才设 `InputTokensDetails`（本地无条件建对象）、**完全不设 `OutputTokensDetails`**（只设 `CompletionTokenDetails`）、且**新建** `*Usage` 而非就地填充。纯协议层拼写桥接（Responses 客户端读 `output_tokens_details`），零宿主依赖。**该对象在 `HEAD`/`dbfbc096f` 均 0 命中，只在工作区**——见下 |
| `IncompleteDetails.EffectiveReason` | 2 | **A** | 推理语义，relaykit 有 `reasoning_state.go` 可承接 |
| `OpenAIResponsesResponse.HasCompactionOutput` | 2 | **B** | 同 `HasCompactionTrigger` |
| `OpenAIResponsesResponse.GetQuality` | 2 | **A** | 图像响应字段访问器 |
| `OpenAIResponsesResponse.GetSize` | 2 | **A** | 同上 |
| `OpenAIResponsesResponse.HasImageGenerationCall` | 2 文件 / 4 调用点 | **A** | relaykit 无谓词（只有常量 `ResponsesOutputTypeImageGenerationCall`，`relaykit/dto/openai_response.go:13`）。与同接收者、同调用点、已判 A 的 `GetQuality`/`GetSize` 同构。零宿主依赖。**原文引的 `ImageGenerationCallCounter.Observe` 不构成「更强覆盖」**：它是**上游宿主**（`upstream/main:relay/common/tool_usage.go`，本地不存在该文件）的**有状态**计费去重计数器（返回 void、入参含 outputIndex），与协议层**无状态**谓词不同能力 |
| `Usage.GetOutputTokenDetails` | 2 | **A** | relaykit 无等价物（全仓零命中；`OutputTokensDetails` 在 relaykit 内只有字段声明 `relaykit/dto/openai_response.go:242`，无任何带回退的读取路径）。纯协议层访问器（`OutputTokensDetails` 缺失时回退到 `CompletionTokenDetails`），零宿主依赖 |
| `Reasoning.MarshalJSON` | 2 | **A** | presence 保真 |
| `StreamOptions.MarshalJSON` | 2 | **A** | presence 保真 |
| `Reasoning.Clone` | 1 | **A** | 值拷贝，无策略 |
| `StreamOptions.Clone` | 1 | **A** | 同上 |
| `StreamOptions.HasIncludeUsageField` | 1 | **A** | presence 保真 |
| `StreamOptions.HasIncludeObfuscationField` | 1 | **A** | presence 保真 |
| `StreamOptions.RemoveIncludeUsage` | 1 | **A** | presence 保真 |
| `ChatCompletionsStreamResponse.GetError` | 1 | **A** | 协议响应字段访问器（工作区口径） |
| `ClaudeRequest.SearchToolNameByToolCallId` | 1 | **A** | 工具名契约，属协议层 |

**A 类 21 个、B 类 2 个、待实测 0 个（第 42 轮定案，见下）。** 另有 2 个（`IsValidToolName`、`ToolNameMaxLength`）生产引用为 0，**C 类删除**或随 `tool_name.go` 整体迁入——倾向后者，因为 `NormalizeToolName` 与它们同文件且必须迁入。

**3 个原「需实测」方法已定案为 A（第 42 轮）**：`Usage.EnsureResponsesTokenDetailObjects`、`Usage.GetOutputTokenDetails`、`OpenAIResponsesResponse.HasImageGenerationCall`。三者 relaykit 全仓零命中（`git grep -c <方法名> 789c97019 -- 'relaykit/**/*.go'` 均为 0），且方法体只引用 relaykit/dto 也有的类型字段——迁入不新增任何 `boundary_test.go` 违规。**其中 `Usage.EnsureResponsesTokenDetailObjects` 在 `HEAD`/基线均不存在**（只存在于工作区，是其他会话的未提交改动），其 A 分类是**条件结论**：执行迁入前须与该会话确认该方法的最终形态（若被删除或改名，本行分类随之作废）。原「阶段 2 的前置」中的 3 行判据据此关闭。

**relaykit 独有 5 个方法**（**三文件口径**；全包 31 个，见 12.0.5，迁移会丢）：`GeneralOpenAIRequest.MarshalJSON`（`relaykit/dto/openai_request.go:136`）、`CachedTokenDetails.HasTokens`（`:284`）、`InputTokenDetails.Clone`（`:290`）、`InputTokenDetails.CacheCreationTokensTotal`（`:316`）、`ResponsesOutput.MarshalJSON`（`:397`）——**四个在 `relaykit/dto/openai_response.go`，一个在 `openai_request.go`**（**第 42 轮修正**：原文写「前三个对应 relaykit 的 `dto/usage_merge.go` 与 `dto/reasoning_state.go`」，实测这 5 个方法无一在 `usage_merge.go`/`reasoning_state.go`；前者是 `Usage` 合并，后者是 `ReasoningConversionState` 类型声明）。它们与本地的 `Usage.*` 方法**共享 `Usage` 的 token-detail 数据域、但能力互不重叠，迁移后两组必须并存**：relaykit 侧是缓存写计费（`CacheCreationTokensTotal`）与存在性感知深拷贝（`Clone`/`HasTokens`），本地侧是 Responses/Chat 两种拼写的桥接与回退读取。二者**不存在取舍**（**第 42 轮修正**：原文写「同一能力域的两套实现……须做语义对齐，不能简单二选一」，会误导读者以为要二选一）。本地方法在 relaykit 侧的最近邻是 `relayconvert.UsageFromChatUsage`（仅输入半边），不是 `InputTokenDetails.*`。

### 12.6 命名空间工具与 tool_search → 保留在宿主

**决定**：`service/openaicompat/` 的命名空间工具与 tool_search 能力**整体保留在宿主**，不迁 relaykit。

**依据**：

- 上游四作用域（`relaykit/`、`relaykit/relayconvert/`、`service/`、`relay/`）**零命中** `namespace`；`tool_search` 唯一命中是计费字段。这是本地独有的协议扩展，不是上游遗漏
- 二者都依赖 `ChatCompletionsToResponsesPolicy` 的 rules 模式（12.8），该策略本身是宿主配置体系的一部分
- 上游把「是否转换」的决策明确留在宿主——其 `service/openai_chat_responses_mode.go` 注释原文：*"Chat→Responses upgrade policy is host routing logic (it decides **whether** to convert, reading host settings), so it lives here, not in relayconvert."*

**影响**：`service/openaicompat/` **整体保留**（见 12.15——按能力切分在函数粒度不成立）。命名空间能力所依赖的字段（`ResponsesOutput.Namespace`、`ChannelOtherSettings` 的命名空间相关字段）**必须留在宿主可达的位置**。

**精确引用面（口径必须随结论给出）**：以命名空间**工具**相关的全部符号为口径——

```bash
PAT='NamespaceToolCall|SupportsNamespaceTools|NamespaceToolsCapability|ResolveResponsesNamespaceTools|ExplicitlyDisablesResponsesNamespaceTools|responsesNamespaceToolSelection|responsesToolTypeNamespace|NamespaceTools'
git grep -lE "$PAT" HEAD -- '*.go' | grep -v '_test.go'
```

得 **11 个非测试文件，跨 6 个顶层目录（按 Go 包为 8 个——`relay/channel/openai`、`relay/common`、`service/openaicompat` 各算一个；**第 39 轮补注口径**）**：`constant/context_key.go`、`controller/{relay.go,relay_responses_tool_observation.go}`、`dto/channel_settings.go`、`model/{channel_responses_tool_capability.go,channel_route_model.go}`、`relay/{channel/openai/responses_tool_capability.go,common/responses_input_filter.go}`、`service/{log_info_generate.go,openai_chat_responses_compat.go,openaicompat/responses_to_chat_request.go}`。

**注意口径陷阱**：早期版本写「横跨 4 个包」（`dto`/`controller`/`model`/`service/openaicompat`），该数字**无法用任何单一命令复现**——它既漏了 `constant`、`relay/common` 与 `relay/channel/openai`，也漏了 `service/log_info_generate.go`。窄口径（只搜 `NamespaceToolCall` 等 4 个符号）得 8 个文件 / 6 个顶层目录，宽口径（上列 8 个符号）得 11 个文件 / 6 个顶层目录；无论哪个口径都不是 4 个包。本项即 12.18 原第 26 项，现以「口径 + 命令 + 结果」三者齐全的形式关闭。

若 `dto` 类型整体迁入 relaykit，则这些字段要么一并迁入（使 relaykit 承载宿主协议扩展），要么由宿主维护影子结构——**这是阶段 2 必须一并决策的耦合点**，见 12.0.2。

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
| `dto/billing_usage.go`、`dto/usage_merge.go` | `billing_usage.go`：函数集（AST `recv.name` 口径）**双向完全相同**，diff `25+/38−` 是纯函数体改写（六处内联条件收敛为 `mergeInputTokenDetails`、手工夹紧改为 `max()`、`canonicalClaudeUsage` 由「零/非零」判定改为按非 nil 判定、`cloneOpenAIUsage` 改用 `.Clone()`），导出符号 18/18 一致。`usage_merge.go`：`52+/7−` 为净增；分支独有导出符号 0 个，**main 独有导出符号 1 个**（`MergeClaudeUsageNonZero`，即分支上未导出的 `mergeClaudeUsageNonZero` 导出后供 `relayconvert/internal/oai_chat` 跨包调用）；另 main 多一个**非导出的提取函数** `normalizeGeminiModality`（把分支里内联的 `strings.ToUpper(strings.TrimSpace(...))` 提成函数，非新逻辑）。两文件 main 侧**无能力删除** |
| `relaykit/` 整体 | 分支相对 main 是 `68 files changed, 3910 insertions(+), 746 deletions(-)`——**main 净增** |
| `ClaudeToChatStreamState` / `ChatToGeminiStreamState` | 类型定义在 main 与分支同名同语义（`relaykit/relayconvert/internal/claude_messages/to_oai_chat_resp.go:126`、`to_gemini_chat_resp.go:13`）；文件命中数 main 与 `feat/relaykit-perf` 各为 6/6，`feat/relaykit-tools` 为 4/2（该分支较旧） |
| `toolconv` 的 `Kind*` 常量 | 两边均 **10 个** |

**结论**：main 已包含这些分支的最终形态，且在这些区域**更完整**。以 main 为基线即可，无需 cherry-pick。

**⚠ 方法学注记**：早期版本仅凭「分支不是 main 祖先」+「toolconv 有 10 个 Kind」就下了「main 已含最终形态」的结论——**这是不充分的**。6 个提交均不在 main 中，必须逐一证明其内容已被覆盖。上表的逐字节/符号集比对是后补的取证。

### 12.10 `ConvOptions` 缓存与跨渠道重试（第 10 节第 10、11 项；第 34 轮修正）

**决定**：上游语义采用；**但「无需改动」只对上游成立——本地必须落地两处**，都排进阶段 3：

1. **`convOptions` 字段**（在阶段 3 的照抄范围之外）：上游声明在 `upstream/main:relay/common/relay_info.go:201-202`，本地无此字段。
2. **`InitChannelMeta` 里的重置行**：上游 `upstream/main:relay/common/relay_info.go:311` 置 `info.convOptions = nil`，注释原文：*"Channel identity feeds the converter options snapshot (e.g. OpenRouterDialect); drop the cache so a cross-channel retry rebuilds it."* 本地 `InitChannelMeta`（`relay/common/relay_info.go:358`）没有这一行，也不可能有——字段本身尚不存在。本地换渠道重试每次都会调用它（`controller/relay.go` 渠道重试循环内的 `info.InitChannelMeta(c)`，基线口径 `:969`；其余 16 个 handler 入口也调用），重置点现成，缺的只是这一行。

**为什么原判据抓不到**：原文判据是「首次调用建快照、同一 relay 会话复用」——**不重置也满足**。第 34 轮把阶段 3 判据改为可验证条件：**换渠道重试后 `ConvOptions()` 反映新渠道**（`OpenRouterDialect`、`ToolLossPolicy` 不得读到上一渠道的值）。

**性能边界（第 34 轮补记）**：`ConvOptions()` 在 12.4.4 的门控里**每 chunk 被调用**（流式 `StreamResponseOpenAI2Claude` 在 `reasoning/text` 二选一之前）。缓存命中时它只是一次指针判空；未命中才重建快照（`GetClaudeSettings`/`GetGeminiSettings` + 构造 `convmeta.Options`）。重置发生在 `InitChannelMeta`（每渠道尝试一次），不在 chunk 循环里，所以稳态是「每渠道一次重建 + 每 chunk 一次判空」。**不得**为了「简单」把 `ConvOptions()` 实现成每次新建快照——那是每 chunk 一次分配；也不得省掉重置行——那是跨渠道读到上一渠道的 `OpenRouterDialect`/`ToolLossPolicy`。

**第 11 项（钩子闭包捕获哪个 `UpstreamModelName`）随之收窄**：钩子签名是 `func(modelName string) bool`，**由 relaykit 在调用点传入当前模型名**，不捕获 `info`。因此不存在「闭包捕获了哪个名字」的问题——调用方决定。本地新增钩子必须遵循同一约定：**只回答策略，不捕获请求状态**。

### 12.11 `reasonmap` 宽松 default（第 10 节第 12 项）

**决定**：**保留上游宽松 default**（不改 `reasonmap` 的 default 分支）。但「严格校验留宿主调用点」这句在**阶段 6 之后无落点**——第 29 轮实测如下，落点须按 12.11.3 三选一重新指定（A1 改签名 / A2 钩子选分支+宿主校验 / B 纯宿主校验），不能指望既有调用点。

**依据**：`reasonmap.OpenAIFinishReasonToClaudeStopReason`（`relaykit/reasonmap/reasonmap.go:31`）的 `default: return finishReason` 与本地 `stopReasonOpenAI2ClaudeStrict`（`service/convert.go:1125`）行为不同。relaykit 作为通用转换套件，对未知 finish_reason 透传是合理默认——它不知道上游会发明什么新值；套件本身不该拒绝。这一点不变。

**第 47 轮补——本节只覆盖 Claude 方向，relaykit 还有第二套 finish_reason 映射**：`reasonmap` 是 OpenAI→Claude 的映射表；**Gemini 方向的映射在 `oai_chat` 包内自成一套**（`geminiFinishReason`，`relaykit/relayconvert/internal/oai_chat/to_gemini_chat_resp.go:518`），**不经 `reasonmap`**，其 `default: return "STOP"` 比 `reasonmap` 的透传更激进——**未知值被改写成「正常结束」**。故「保留上游宽松 default」这条决定对 Gemini 方向**同样适用**（套件本身不该拒绝），但**严格校验的落点也必须一并指定**，不能只指定 Claude 方向的 5 处。差异逐条见 12.11.2b，落点见 12.11.3 第 47 轮补与处置清单第 9 项。

#### 12.11.1 「宿主调用点」在阶段 6 后不存在（第 29 轮实测）

早期版本写「严格化应在**宿主调用点**做」，但**从未指明哪个调用点**，且它默认的前提是错的：

| 事实 | 证据 |
|---|---|
| 本地严格校验的**两个唯一调用点**都在 `service/convert.go` 内 | `:738`（`StreamResponseOpenAI2ClaudeWithError`）、`:1057`（`ResponseOpenAI2ClaudeWithError`）；`stopReasonOpenAI2ClaudeStrict` 定义在 `:1125` |
| 这两个函数**正是阶段 6 要删除的主体** | 阶段表第 6 行：删 `service/convert.go` 主体（2229 行）；12.3：`*WithError` 系列直接删除 |
| 宿主 **5 个 Claude 响应方向的 `*WithError` 调用点**没有任何一处做 finish_reason 校验（**第 47 轮注：这是 Claude 方向的 5 处；Gemini 方向另有 4 处同样无校验**——`relay-openai.go:324`、`openai/helper.go:63,267`、`chat_via_responses.go:286`，两侧合计 9 处，见 9.1 第 8 条） | `relay/channel/openai/relay-openai.go:314`、`openai/helper.go:46,246`、`openai/chat_via_responses.go:280`、`gemini/relay-gemini.go:1784`——全是 `if conversionErr != nil { return … }` 的转发，校验被**委托**给被删的 `*WithError`（全仓库 `*WithError` 调用点共 9 个，其余 4 个是 Gemini 方向） |
| relaykit **无 finish_reason 钩子可注入** | `convmeta.Options` 全部字段（`relaykit/relayconvert/convmeta/options.go`）无一项涉及 finish_reason/stop_reason；`reasonmap.OpenAIFinishReasonToClaudeStopReason` 是**纯包级函数**、无 receiver、零钩子；两个调用点（`oai_chat/to_claude_messages_resp.go:502`、`oai_responses/to_claude_messages_resp.go:87`）都在 `internal/` 内 |

**结论**：把校验「加回宿主调用点」意味着在 5 个 relay 调用点各写一遍「先校验 `simpleResponse.Choices[].FinishReason`、再调 relaykit」，是**5 份重复的校验**、且与 relaykit 内已完成的转换**顺序颠倒**（先转换、后发现非法值，只能丢弃结果）。**这与「避免在宿主复制套件逻辑」相悖。** §12.11 必须改为下列二者之一。

#### 12.11.2 差异不止「未知值报错」一条（第 29 轮实测）

早期版本把 9.1 第 1 条归结为「未知值报错 vs 透传」一条。实测**共 5 条**，逐条对 `reasonmap.OpenAIFinishReasonToClaudeStopReason`：

| # | finish_reason | 本地严格函数 | relaykit | 差异性质 |
|---|---|---|---|---|
| a | 未知值（如 `unknown_reason`） | 报错 | 原样透传 | 严格度差异（9.1 已记） |
| b | `""` | `"end_turn"` | `default` → `finishReason` = `""` | **行为回归（非错误路径）** |
| c | `" stop "` / `"STOP "`（含首尾空白） | `ToLower`+`TrimSpace` 归一后匹配 | 仅 `ToLower`，**无 `TrimSpace`** → 落 `default` 透传带空白的原串 | 行为回归（边界输入） |
| d | `function_call` | `"tool_use"` | `default` → `"function_call"` | 行为回归（合法值被透传成非法 stop_reason） |
| e | `content_filter` | `"refusal"` | `types.FinishReasonContentFilter` = `"content_filter"` → `"refusal"` | **无差异**（前版本误以为有；两分支同值） |

**本轮（第 42 轮）实测修正——b/c/d 三条回归都只在非流式路径。** 上表把 b/c/d 记作「行为回归」，但本地严格函数在**流式**路径只作校验、**丢弃返回值**：`StreamResponseOpenAI2ClaudeWithError`（`service/convert.go:730`）先用 `stopReasonOpenAI2ClaudeStrict` 校验（非法即 `return nil, err`），实际输出却走宽松的 `streamResponseOpenAI2Claude`——其 `StopReason` 取 `stopReasonOpenAI2Claude`（`:955`、`:1025`），即 relaykit 同款裸透传。故流式输出的 b/c/d 与 relaykit **逐字一致**（`""`/`" stop "`/`function_call` 都原样透传，其中 `""` 另有 `:231-233`、`:975-977` 的 `end_turn` 守卫，两侧同值）。**真正两方向都差的只有 a**（未知值报错——流式经校验也会 `return nil, err`，relaykit 则透传）。**实现含义**：恢复 b/c/d 只需改**非流式**路径；测试也应落在非流式（流式侧只有 a 需覆盖）。

**b 的影响面（已实测）**：`dto.ClaudeResponse.StopReason` 是 `string` + `omitempty`（`dto/claude.go:560`、`relaykit/dto/claude.go:510`）。上游**非流式**路径（`oai_chat/to_claude_messages_resp.go:450-495`）在没有任何 choice 携带 `FinishReason`（Go 零值 `""`）时，`stopReason` 停在 `""`，序列化时**整个省略 `stop_reason` 字段**；本地经 `stopReasonOpenAI2ClaudeStrict` 的 `case ""` 返回 `"end_turn"`。**流式无差异**——上游流式另有显式 guard（`oai_chat/to_claude_messages_resp.go:220-222`、`:429-431`），同样落到 `end_turn`；本地两条路径都走同一个 `case ""`。**golden fixture 覆盖不到 b**：本地 golden 的输入 fixture 只有 `finish_reason: "tool_calls"`（快照里对应 `stop_reason: "tool_use"`），relaykit 24 个 golden 只有 `tool_calls`/`stop`/`null`——**两侧都无空串样例**。这正是「一个通过的快照基线不等于有效的基线」的又一实例（同 12.16.1 的 must-fix）。

#### 12.11.2b oai→gemini 方向的 finish_reason 差异（第 47 轮新增）

9.1 第 8 条是本方向的差异登记处，但**差异的逐条清单此前不存在**——12.11.2 只做了 OpenAI→Claude 方向的 5 条（a–e）。本方向逐条对 `geminiFinishReason`（`relaykit/relayconvert/internal/oai_chat/to_gemini_chat_resp.go:518`）：

| # | finish_reason | 本地严格函数 `openAIFinishReasonToGemini`（基线 `service/convert.go:1915`） | relaykit `geminiFinishReason` | 差异性质 |
|---|---|---|---|---|
| a | 未知值（如 `unknown_reason`） | **报错** | `default` → `"STOP"` | **严格度差异 + 语义变化**（未知值从「报错」变成「正常结束」） |
| b | `""` | `"STOP"` | `default` → `"STOP"` | **无差异**（两侧同值，但 relaykit 是「落 default 恰好同值」，不是显式分支） |
| c | `" stop "` / `"STOP "`（含首尾空白） | `TrimSpace` 后匹配 → `"STOP"` | `TrimSpace` 后匹配 → `"STOP"` | **无差异**（两侧都有 `TrimSpace`，与 12.11.2 的 c 不同——那条 relaykit 缺 `TrimSpace`） |
| d | `function_call` | `"STOP"` | `default` → `"STOP"` | **无差异**（同 b，落 default 恰好同值） |
| e | `length` | `"MAX_TOKENS"` | `"MAX_TOKENS"` | 无差异 |
| f | `content_filter` | `"SAFETY"` | `"SAFETY"` | 无差异 |
| g | `stop` / `tool_calls` | `"STOP"` | `default` → `"STOP"` | 无差异（同 b/d） |

**两条路径的映射结果一致（已逐分支比对），但代码是两份**——非流式走 `geminiFinishReason`（`:300`），流式内联 switch（`:176-186`）。**定案 B（宿主前置校验）对两条路径都生效**——校验在调 relaykit 之前，与 relaykit 内部走哪条路径无关，故**不需要两份校验**；两条路径的分叉风险只在「若有人改为在 relaykit 内部打补丁」时出现（改一处不会同步另一处）。本项定案为 B，故该风险不适用，但**须在阶段 4 逐一确认两个 Gemini 响应方向入口（非流式与流式）都插入了前置校验**——只插一个会让另一条路径静默退回未知值→`STOP`。

**结论：本方向只有 a 一条是真差异**，且性质比 12.11.2 的 a 更重——12.11.2 的 a 是「未知值透传成非法 stop_reason」（客户端拿到非法值），本方向的 a 是「未知值被改写成 `STOP`」，**客户端会把它读成「模型正常结束」**，可能触发重试/续跑逻辑误判。b/d/g 三条虽「无差异」，但属于**巧合同值**：relaykit 侧没有对应分支，一旦上游给 `function_call` 加了显式映射（例如映射成 `TOOL_CALL`——Gemini 协议里并不存在该值），差异会**无预警地出现**。故处置上仍按 9.1 第 8 条 + 12.11.3 第 47 轮补的「迁入严格函数 + 宿主前置校验」执行，**不因「只有 a 一条」而降级为「只补一条测试」**。

**与 12.11.2 的三点不同（须写清，避免读者按 12.11.2 的口径推断本方向）**：(1) **relaykit 侧有 `TrimSpace`**，故不存在 12.11.2 的 c 那条差异；(2) **归一目标词汇表是 Gemini**（`STOP`/`MAX_TOKENS`/`SAFETY`）而非 OpenAI（`stop`/`length`/`content_filter`），两侧词汇表不同意味着**不能复用** `NormalizeOpenAIFinishReasonsForClaude`，须第二个助手；(3) **无「空串改非空」风险**——本地严格函数本身就把 `""` 映射为 `STOP`，故 12.11.3 给 Claude 方向加的那条「流式侧只校验未知值、不得改写空串」的限制**不适用于本方向**。(4) **relaykit 侧是两条路径两份代码**（非流式 `geminiFinishReason` `:300`、流式内联 switch `:176-186`），映射结果一致但**改一处不会同步另一处**——故阶段 4 须逐一确认**两个入口都插入了宿主前置校验**，不能只插非流式那一个。

#### 12.11.3 决定

**第 37 轮关键修正——「选项 A」按原样不可实现。** `reasonmap.OpenAIFinishReasonToClaudeStopReason(finishReason string) string`（`relaykit/reasonmap/reasonmap.go:31`）**没有 error 返回通道**；它由 `oai_chat` 内的本地助手 `stopReasonOpenAI2Claude`（`:501`）调用，该助手再被 3 个同样**无 error 返回**的函数调用——`StreamResponseOpenAI2Claude`（`:111`，返回 `[]*dto.ClaudeResponse`）、`FinalizeStreamResponseOpenAI2Claude`（`:420`）、`ResponseOpenAI2Claude`（`:449`，返回 `*dto.ClaudeResponse`）；4 个调用点 `:220`、`:406`、`:429`、`:459` 全在这三个函数体内。**一个布尔钩子只能决定「走严格分支还是宽松分支」，无法把「未知值」变成 `error` 向上传播**——错误无处可去。`oai_responses` 一侧的唯一调用点所在函数 `ResponsesResponseToClaudeMessagesResponse`（`oai_responses/to_claude_messages_resp.go:15`，reasonmap 调用在 `:87`）**有** error 返回，但那只覆盖 responses 方向，覆盖不到本次迁移涉及的 chat 方向。**故选项 A 拆为 A1/A2：**

- **选项 A1（改签名）**：给 `reasonmap` 增加一个**返回 error 的严格变体**（如 `OpenAIFinishReasonToClaudeStopReasonStrict(string) (string, error)`），并把 `oai_chat` 的 3 个函数签名改为带 `error` 返回（`StreamResponseOpenAI2Claude` / `FinalizeStreamResponseOpenAI2Claude` / `ResponseOpenAI2Claude`），宿主在阶段 3 接线本地严格语义（含 b/c/d 三条 + 未知值报错）。**代价**：改动面比原「选项 A」大——3 个函数签名 + 其全部调用点（含 `response_registry.go:1055`、`:1071`）都要跟着改，且 `ResponseOpenAI2Claude`/`StreamResponseOpenAI2Claude` 是 `response_compat.go:38`/`:42` 的公开兼容面目标，签名变化会波及宿主调用点。这是**真正的分叉**，须登记进本地改动清单并评估上游跟进成本。
- **选项 A2（不改套件，宿主前置校验）**：**原选项 A 的可实现版本**——`convmeta.Options` 新增 `StrictFinishReason func(modelName string) bool` 之类的**布尔钩子只用于选择分支**（走严格映射还是宽松透传），**而「未知值报错」由宿主在调用 relaykit 之前完成**：即在宿主入口先对 `Choices[].FinishReason` 做一次 `stopReasonOpenAI2ClaudeStrict` 校验，非法即 `return error`，合法才调 relaykit。**这实际上退化为选项 B 的形态**（宿主前置校验），只是校验函数复用本地实现、且用钩子告知 relaykit 该走哪条分支。**若团队不接受 3 个函数签名变更，A2 是唯一可行路径。**
- **选项 B（不改套件，宿主 5 + 4 处前置校验）**（**第 47 轮修正**：原文写「宿主 5 处」，只覆盖 Claude 方向；Gemini 方向的 4 处须一并纳入，否则 9.1 第 8 条无落点——两个方向各经自己的共享助手）：在**宿主 5 个 Claude 响应方向的 `*WithError` 调用点各自前置校验** `Choices[].FinishReason`，非法即 `return error`（在调 relaykit **之前**）。代价：5 + 4 = 9 份重复校验、逻辑分散、且要保证与 relaykit 的 b/c/d 语义逐字一致（Gemini 方向另要保证与 `geminiFinishReason` 的映射逐字一致）。**与 A1 并列为待定两选项之一**——第 42 轮已核：5 份重复校验可用 1 个共享助手消除、宿主不复制套件逻辑（见下），故其代价不构成排除理由；**第 47 轮注：Gemini 方向的 4 份同样可用第二个共享助手消除，不改变这条判据**。

**为什么 A1 与 A2 的取舍不能只看「改动小」**：A1 让严格语义成为 relaykit 的一部分（一次实现、上游可跟进），代价是签名分叉；A2 保持 relaykit 签名不变，代价是宿主多一次前置遍历 + 一个只为选分支存在的钩子（**钩子本身不产生错误，这一点必须写清楚，否则读者会以为接线钩子就完成了严格化**）。

**本轮（第 42 轮）实测补充——两个影响选型的已核实事实**：(1) **A1 在改动面上是 A2 的超集**。A1 若要与 §12.11 已定的「保留上游宽松 default（套件本身不该拒绝未知值）」并存，relaykit 的 chat 转换器就不能对未知值无条件报错，于是仍须引入 A2 的「选分支」钩子——即 A1 = A2 + `response_compat.go:38`/`:42` 两个公开包装的签名变更，不是「更干净的单点」。(2) **§12.11.1 对 B 的两条反对都可规避**：「5 份重复校验」用 1 个共享助手即可消除；「顺序颠倒」只对**事后**实现成立——B 的正确形态是在调 relaykit **之前**把 `Choices[].FinishReason` 归一到 OpenAI 词汇表（`""`→`stop`、`function_call`→`tool_calls`、`TrimSpace`+`ToLower`），未知值即 `return error`，OpenAI→Claude 的映射仍全由 relaykit 完成，宿主没有复制套件逻辑。**B 的注意点**：流式末块判定依赖 `FinishReason != nil && *FinishReason != ""`，故流式侧只校验未知值、**不得**把空串改写成非空（否则未结束块被误判为结束块）——这与上条「b/c/d 只在非流式」一致，即流式侧只有 a 需要处理。

**第 44 轮定案：选 B。** 判据三条：(1) **A1 不是「更干净的单点」**——第 42 轮已核 A1 = A2 + 两个公开包装签名变更（`response_compat.go:38`/`:42`），而 A2 已退化为 B 的形态；选 A1 等于同时接受「宿主前置校验」与「relaykit 公开签名分叉」两笔成本。(2) **B 的校验函数在宿主已有现成实现**：`stopReasonOpenAI2ClaudeStrict`（基线 `service/convert.go:1125`）已覆盖 a（未知值报错）/b（`""`→`end_turn`）/c（`TrimSpace`）/d（`function_call`→`tool_use`）四条的全部语义，且已被 `:738`/`:1057` 两个调用点验证过行为。**但该函数随 12.3 与 `service/convert.go` 主体一并删除**——B 的实现形态因此是「**把它迁出被删文件，放进宿主共享助手**」，不是新写校验逻辑。(3) **B 保持 relaykit 全部签名与上游一致**，`relaykit/VERSION` 跟进时这一维零冲突；A1 的分叉（即使理论上可提上游）在跟进时是持续的合并成本。

**B 的实现形态（第 44 轮，写死到可执行；第 47 轮补第三件事）**：新增宿主文件（建议 `service/finish_reason_strict.go` 或并入 `service/response_converter.go`，阶段 4 定）承载三件事：

- **`stopReasonOpenAI2ClaudeStrict` 原样迁入**（含 `case ""`/`TrimSpace`/`function_call` 三条分支，语义逐字不变）；
- **一个共享归一化助手**（如 `NormalizeOpenAIFinishReasonsForClaude`），在调 relaykit **之前**遍历 `Choices[].FinishReason`：先用 `stopReasonOpenAI2ClaudeStrict` 校验（未知值即 `return error`），再把值**归一到 OpenAI 词汇表**（`""`→`stop`、`function_call`→`tool_calls`、`TrimSpace`+`ToLower`）——这样 b/c/d 三条经 relaykit 的 `reasonmap` 映射后与本地严格函数输出**逐字一致**（`stop`→`end_turn`、`tool_calls`→`tool_use`），宿主不复制 OpenAI→Claude 的映射表本身。

**第 47 轮补——同一文件还须承载第三件事（9.1 第 8 条方向）**：`openAIFinishReasonToGemini`（基线 `service/convert.go:1915`）**与 `stopReasonOpenAI2ClaudeStrict` 同批迁入同一个文件**（执行次序表批 1，与 C1 同批），并配**第二个共享归一化助手**（如 `NormalizeOpenAIFinishReasonsForGemini`）供 4 个 Gemini 响应方向调用点（`relay/channel/openai/relay-openai.go:324`、`openai/helper.go:63,267`、`openai/chat_via_responses.go:286`）在调 relaykit 前调用。**归一目标词汇表不同**：`""`/`stop`/`tool_calls`/`function_call`→`STOP`、`length`→`MAX_TOKENS`、`content_filter`→`SAFETY`，未知值即 `return error`。**与 Claude 方向的关键差异**：Gemini 侧**不存在「空串改非空」风险**——本地严格函数本身就把 `""` 映射为 `STOP`，故归一表与本地输出逐字一致，**不需要** Claude 方向那条「流式侧只校验未知值、不得改写空串」的限制。**测试**：`openAIFinishReasonToGemini` 全仓零测试（基线 `service/convert.go` 内仅定义行 `:1915` 与函数体内 2 处调用 `:1854`/`:2149`；4 个生产调用点调的是包装、不直接调它），须至少补未知值报错一条；`length`/`content_filter`/`""`/`function_call` 四条映射各一条更稳妥。**此前整个 §9 与 12.11 全节无本方向的任何落点**——本段是其首次登记。

**落点与调用次数**：宿主 5 个 Claude 响应方向的生产调用点（`relay/channel/openai/relay-openai.go:314`、`openai/helper.go:46,246`、`openai/chat_via_responses.go:280`、`gemini/relay-gemini.go:1784`）各调一次共享助手——**这是 5 次调用、1 份逻辑**，不是 12.11.1 所说的「5 份重复校验」。**流式侧（`openai/helper.go` 2 处）只做未知值校验、不得改写空串**——流式末块判定依赖 `FinishReason != nil && *FinishReason != ""`，把 `""` 改写成 `stop` 会让未结束块被误判为结束块（第 42 轮已核）。这与「b/c/d 只在非流式」一致。**第 47 轮补——Gemini 方向另有 4 个调用点**（`relay/channel/openai/relay-openai.go:324`、`openai/helper.go:63,267`、`openai/chat_via_responses.go:286`）调**第二个**共享助手 `NormalizeOpenAIFinishReasonsForGemini`，**4 次调用、1 份逻辑**；两个助手同文件、同批迁出（处置清单第 9 项、执行次序表批 1）。**Gemini 方向无「不得改写空串」限制**——本地严格函数本身就把 `""` 映射为 `STOP`，故其流式侧（`openai/helper.go:63`）可做完整归一，与 Claude 侧的流式限制不对称（原因见 12.11.2b 第 (3) 点）。

**与形态 C 的次序**：形态 C（12.20.1）把 9 个 `*WithError` 生产调用点改指 `service.ConvertResponse`/`ConvertStreamResponse` 包装——共享助手的调用插在这些包装调用**之前**，两种定案不冲突；助手本身是宿主文件，不进 relaykit，`relaykit/VERSION` 跟进时零差异。

**无论选哪个，都必须新建测试**：`TestResponseOpenAI2ClaudeRejectsUnknownFinishReason`（`service/convert_claude_request_test.go:900`）只覆盖 a；**b/c/d 三条无任何测试**，且两条 golden 基线都测不到（见 12.11.2）。阶段 4 须为 b/c/d 各补一条，否则「切换后行为未退化」在 finish_reason 这一维上**无证据**。

**登记（B 定案后）**：A1 未被选择——本地改动清单第 12 行（`reasonmap` 严格变体 + `oai_chat` 3 函数签名）**不适用**，保留原文作对照、在该行标注定案结果（第 44 轮已改）；`stopReasonOpenAI2ClaudeStrict` 的迁出与共享助手落入阶段 4 处置清单第 9 项（已改写）。**9.2.2（反方向）不受本定案自动化解**：A1 原可让反方向「一并纳入同一钩子机制」，B 没有该钩子——9.2.2 的 a–e 裁定归处置清单第 17 项独立进行（**第 46 轮修正：原写「仍在第 6 项内」与第 17 项矛盾**），`pause_turn` 的 stop/length 分歧不得默认采用 relaykit 的 `length`。

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

### 12.15.4 通道级转换器收敛到 relaykit（第 16 轮决定，关闭未决项 28）

**决定**：`relay/channel/{claude,aws,vertex}` 共用的 `RequestOpenAI2ClaudeMessage`（459 行）及其同族转换器**收敛到 relaykit 的对应入口**，本地只保留一层薄适配（`convmeta.Options` / `MediaResolver` 注入 + 调用）。**不是「保留双实现」，也不是「原样不动」。**

**依据（逐条实测）**：该函数的三类宿主耦合，relaykit 侧**都已有对应的注入机制**，因此不需要把 459 行搬进 relaykit，只需要在宿主侧接线：

| 本地能力 | relaykit 对应机制 | 证据 |
|---|---|---|
| 思考等级适配器（`model_setting.GetClaudeSettings()`、`ShouldPreserveThinkingSuffix`、budget percentage） | `sharedclaude.ApplyReasoning(ctx, req, info, source, crossProtocol)`（`internal/shared/claude/reasoning.go:16`）**完全经 `convmeta.Options` 取策略**：`opts.ShouldPreserveThinkingSuffix(...)`（`:25`、`:26`）、`opts.Claude.ThinkingAdapterBudgetTokensPercentage`（`:108`）、`reasoning.RenderClaude`。relaykit 内**零 `model_setting` 引用** | 这正是 §5 要保留的「思考等级」，relaykit 的实现比本地更完整 |
| 文件/媒体解析（`buildClaudeMediaMessageFromSource` 调 `service.GetBase64Data`） | `relayconvert.MediaResolver`（`relayconvert/media.go`，`SetMediaResolver`）——接口为 `GetBase64Data func(context.Context, types.FileSource, ...string) (string, string, error)`，**与 `service.GetBase64Data` 签名完全一致**；relaykit 的 `relaykit/relayconvert/internal/oai_chat/to_claude_messages_req.go:268` 内部做 MIME→`ClaudeMediaMessage` 映射（pdf→`document`，其余→`image`） | 宿主注册一次 resolver 即可 |
| 输入 schema 归一化（`normalizeClaudeInputSchema`） | `sharedclaude.FunctionParametersToInputSchema`（`relaykit/relayconvert/internal/shared/claude/schema.go:3`） | **同名不同语义**，**不是等价替换**——实测有 4 处差异，见下方「边界 1 的实测结论」。早期版本在此写「已内置」而把差异推到阶段 4 再说，等于把一个行为回归藏进验证项 |

**接线方式**：阶段 4 注册 `relayconvert.SetMediaResolver(...)`（指向 `service.GetBase64Data`）与 `convmeta.Options` 钩子（指向 `model_setting`）。**第 50 轮订正两处**：①**注册位置**——上游放在 `service/request_converter.go` 的 `init()`（`789c97019:service/request_converter.go:14`，包初始化、进程启动即生效），**不是「relay 初始化路径」**；原写「在宿主的 relay 初始化路径注册」会误导实现者把注册推迟到某条 relay 请求路径，而那已是转换器被调用之后（`SetMediaResolver` 是进程级全局，须在首次转换前就位）。②**adaptor 改调目标**——`relay/channel/claude/adaptor.go:100`、`relay/channel/aws/adaptor.go:173`、`relay/channel/vertex/adaptor.go:302` **改为调 `service.ConvertRequest(c, info, types.RelayFormatClaude, request)`**（与 C6 行、12.20.1 生产表一致；`789c97019:relay/channel/claude/adaptor.go:122`），**不是 `relayconvert.OpenAIChatRequestToClaudeMessages`**——该函数虽在 `request_compat.go:20` 公开可 import，但直调会绕过 `RecordConversionDiagnostics`，且与全文其它三处（C6、12.20.1、第 16 项）的落点矛盾。**459 行本地实现随之删除**（阶段 6 一并处理），不是保留。

**边界（2026-10-05 已实测，原文写「阶段 4 开工前必须实测比对」——现在有结论了，且两条都成立，故本决定的退路条款被激活）**：

1. ~~**`normalizeClaudeInputSchema` 的行为必须与 `FunctionParametersToInputSchema` 逐项对齐**~~ → **已实测：不成立，有 4 处实质差异**（见下表）。**这不是「细节差异」，其中 D1 是行为回归**（非法 schema 从「明确报错」变成「静默降级为 `{"type":"object","properties":{}}` 发给上游」）。**必须补进 relaykit 或保留为宿主 pre-step**，不能只接线。
2. ~~**媒体 MIME 支持面**（`text/*` 分支是否仍在用）~~ → **已实测：仍在用，且必须保留**。本地 `text/*` 分支在两处（`relay-claude.go:184` 的 `buildClaudeMediaMessageFromSource`、`:103` 的 `buildClaudeMediaMessageFromFile`），**上游 `oai_chat/to_claude_messages_req.go` 完全没有**——它的 `switch mediaMessage.Type` 只有 `case "text"`（字符串内容）与 `default`，`default` 里不分 MIME 类型：一律 `application/pdf` 前缀→`document`、**其余全部→`image`**，并把 resolver 返回的原始 `mimeType` 原样写进 `Source.MediaType`。即**上游会把一个 `text/plain` 文件当 image 块发给 Claude**。同时上游**没有** `normalizeClaudeMimeType`（去 `;` 参数 + 小写 + trim）——`relaymedia.ResolveBase64Data`（`internal/media/media.go:28`）只转发给宿主注入的 resolver，自身零归一化。
3. **本地独有的第三条**（12.15.4 未列，本轮发现）：本地对 `ContentTypeFile` 有**独立分支**（`relay-claude.go:639`）走 `buildClaudeMediaMessageFromFile`（支持 `file_data` 内联、`file_id`/`file_url` 间接、`application/octet-stream` 拒绝）；上游**没有 `ContentTypeFile` 这个 case**，它落进 `default` 与 `image_url` 走同一条路。此外上游在 `mediaMessage.ToFileSource()` 返回 nil 时**静默 `continue`**（内容被丢弃无错），本地则显式报错。**且 `ContentTypeInputAudio`/`ContentTypeVideoUrl` 本地显式报错（`relay-claude.go:666` 的 `case dto.ContentTypeInputAudio, dto.ContentTypeVideoUrl:`，**第 39 轮修正**：原文写 `ContentTypeAudio`，本地无该常量，源码为 `dto.ContentTypeInputAudio`），上游同样静默当 image。**
4. `ResponseClaude2OpenAI` / `StreamResponseClaude2OpenAI`（出向）同样收敛；`aws/converse.go:696` 的调用改为 relaykit 入口

#### 12.15.4.1 边界 1 的实测差异（`normalizeClaudeInputSchema` vs `FunctionParametersToInputSchema`）

两侧签名都收 `parameters any`，但行为不同（本地 `relay/channel/claude/relay-claude.go:233`、上游 `relaykit/relayconvert/internal/shared/claude/schema.go:3`）：

| # | 输入 | 本地 | 上游 | 性质 |
|---|---|---|---|---|
| D1 | 非 map 或不可 JSON 序列化 | `common.Any2Type[map[string]interface{}]`（`common/utils.go`，JSON 往返）失败即**报错**，带 `tool %s parameters must be a JSON object` 上下文 | `parameters.(map[string]any)` 类型断言失败 → `params` 为 nil → 静默产出 `{"type":"object","properties":{}}` | **行为回归**：非法 schema 从报错变成静默降级，错误请求被当作「无参数工具」发给 Claude |
| D2 | 缺 `properties` | 不补（保持缺省） | 补 `map[string]any{}` | 输出差异：Claude 收到的 `input_schema` 多一个空 `properties` |
| D3 | `type` 存在但非 string | **报错**（`parameters.type must be a string`） | 保留原值（仅在 `== nil` 时补 `"object"`），非 string 会原样透传 | **行为回归**：非法 `type` 从报错变成透传 |
| D4 | 输入是「可 JSON 往返但非 map 断言成功」的类型 | `Any2Type` 走 JSON 往返，`json.Marshal`/`Unmarshal` 到 `map` 能成功的情形比类型断言宽（如 `json.RawMessage` 存的 map） | 类型断言直接失败 | 边缘；取决于 `Parameters` 的实际动态类型 |

**注意 D1 与 9.2 的关系**：9.2 表原有 6 条输入校验（未知 content block、image 缺 source ×2、image source.type、非 assistant thinking、tool_use 缺字段），**没有 schema 归一化这一条**。本轮已把它与「媒体内容块分派」一并补入 9.2（现共 8 条），见 9.2 表第 7、8 行。

**结论**：边界 1 **不成立** → 触发 12.15.4 的退路条款。但退路「把该函数迁入 relaykit」不适用于 schema 归一化（它是**纯函数**、无宿主依赖），所以正确处置是**改 relaykit 的 `FunctionParametersToInputSchema` 使其行为与本地一致**（补 error 返回 + `type` 校验），或保留为宿主 pre-step 后再调 relaykit。**这使 12.15.4 的「优先走接线方案，避免对 relaykit 产生分叉」与实测结论冲突**——要保行为一致就必须改上游源码，理由与 12.4 的门控补丁同类（见本地改动清单）。

#### 12.15.4.2 边界 2/3 的实测差异（媒体分派）

| # | 场景 | 本地 | 上游 | 性质 |
|---|---|---|---|---|
| M1 | `ContentTypeFile`（`type: "file"`） | 独立分支（`relay-claude.go:639`）→ `buildClaudeMediaMessageFromFile`（定义 `:69`）：支持 `file_data` 内联 base64、`file_id`/`file_url` 间接、`text/*`→text 块、`application/pdf` 前缀→document、`image/*` 前缀→image、`application/octet-stream` 拒绝 | **无此 case**，落进 `default` 与 `image_url` 同路 | **能力缺失**：file 内容块支持丢失 |
| M2 | `text/*` MIME | 转成 `{Type:"text", Text:<解码内容>}`（`:184` 的 Source 版与 `:103` 的 File 版） | 一律当 `image`，`Source.MediaType` 写原始 `text/plain` | **行为回归**：把文本当图片发给 Claude，上游大概率 400 |
| M3 | MIME 归一化 | `normalizeClaudeMimeType`（`:165`，去 `;` 后缀 + 小写 + trim）+ `buildClaudeMediaMessageFromSource` 内再做一次 `ToLower/TrimSpace` 与**空值报错** | 无归一化，`ResolveBase64Data` 纯转发 | **行为回归**：`IMAGE/PNG; charset=x` 之类在两侧分派结果不同；上游 mimeType 为空也不报错，直接写进 `Source.MediaType` |
| M4 | 不支持的 MIME（如 `application/zip`） | `Claude does not support MIME type %q` 显式报错 | 静默当 `image` | **行为回归** |
| M5 | `ToFileSource()` 返回 nil | 显式报错路径 | `continue`（内容被丢弃，无错） | **行为回归**（静默丢内容） |
| M6 | `ContentTypeInputAudio` / `ContentTypeVideoUrl`（**第 39 轮修正**：原文写 `ContentTypeAudio`，本地无该常量；报错点 `relay-claude.go:666`） | `Claude content type %q is not supported` 显式报错 | 静默当 `image` | **行为回归** |
| M7 | pdf 判定 | `buildClaudeMediaMessageFromFile` 用 `HasPrefix("application/pdf")`（`:122`）；`buildClaudeMediaMessageFromSource` 用**精确相等** `mimeType == "application/pdf"`（`:197`）——**两处不一致** | `HasPrefix("application/pdf")`（`oai_chat/to_claude_messages_req.go:277`） | 本地自身不一致（`:122` 前缀 vs `:197` 精确），迁到上游等于二选一 |

**结论**：边界 2 **不成立**（`text/*` 分支仍在用且必须保留），边界 3 是本轮**新增**的同类差异。**M1 是能力缺失**（file 块），M2/M4/M5/M6 是**行为回归**，M3 是**归一化缺失**，M7 是**本地自身不一致**须择一。

**同族 3 个**（`StreamResponseClaude2OpenAI`、`ResponseClaude2OpenAI{,WithError}`、`responseGeminiChat2OpenAI{,WithError}`）按同一模式处理。

**第 28 轮实测结论：边界 1 与 2 均不成立，退路条款已被激活**——但原退路写得不对，须修正：

- **原退路**：「把该函数**迁入 relaykit**」。**这条不适用**：实测发现的不一致部分是**纯函数**（`normalizeClaudeInputSchema` 只依赖 `common.Any2Type` + `fmt`；`buildClaudeMediaMessageFromFile/Source` 只依赖 `service.GetBase64Data`（可注入）+ stdlib），它们不是「绑定宿主业务」而是**绑定协议转换**——恰恰**本就该在 relaykit 里**。所以正确处置不是「迁入 relaykit」（那正是现状），而是**改 relaykit 现有的实现使其与本地一致**，即给 `FunctionParametersToInputSchema` 补 error 返回与 `type` 校验、给 `oai_chat/to_claude_messages_req.go` 的 `default` 分支补 MIME 分派与 `ContentTypeFile` case。
- **「优先走接线方案，避免对 relaykit 产生分叉」与实测结论冲突**：要保行为一致**必须**改上游源码。理由与 12.4 的门控补丁同类——**保行为优先于避免分叉**。这两条本地改动须登记进「本地改动清单」（第 28 轮新增两行），否则跟进上游时会误判为漂移。
- **退出 12.15.4 决定的另一种可能**（比原退路更省事）：**把 `RequestOpenAI2ClaudeMessage` 及其媒体/schema 辅助函数整体保留在宿主**，只在 3 个 adaptor 处**不切换**。但这与 12.15.4 的初衷（消除双实现）冲突，且 §8/§12.15 的 459 行双实现问题回到原处。**故不推荐**，列出以免阶段 4 走投无路。

### 12.16 验证策略：24 个上游快照不足以保证本地不退化（第 10 节第 9 项）

**决定**：**需实测，且必须补建本地的 golden 基线。**

**问题**：`relaykit/relayconvert/testdata/golden/{request,response,stream}/` 的 **24 个 JSON**（已实测）是**上游自己的基线**——它验证的是「上游行为未变」，**不是「本地行为未退化」**。

具体缺陷：

- 打补丁 1/2 后快照会因「presence 保真生效」而 fail，上游的 `-update` 机制会把差异**直接吸收为新的正确值**——无法区分「保真生效」与「行为回归」
- 第 9.1、9.2 节列出的 **16 处本地差异**（9.1 的 8 条 + 9.2 的 8 条；第 27 轮把 9.1 的第 7 条计入，第 30 轮把 9.2 的第 7、8 条计入，第 47 轮把 9.1 的第 8 条计入——**第 28 轮加 9.2 两行时漏改了此处的聚合计数**；**第 48 轮补：9.2.4 的 10 条 + 9.2.5 的 11 条另计、归处置清单第 18 项**）——其中 **13 处是「本地更强/更严行为」**（9.2 全部 8 条 + 9.1 第 1、2、3、7、8 条），另 **3 处是结构差异**（9.1 第 4、5、6 条：字段名/形状/方法集），**这 24 个快照一处都测不到**
- 早期版本称其为「唯一的机械回归网」——该表述不成立

**决定**：

1. **补建本地的 golden 基线**：用当前 `HEAD` 跑同一批 fixture 生成快照，作为「本地行为」的对照。这是「本地没退化」这一核心目标的**唯一**验证手段
2. **补建针对 9.1/9.2 的 16 处差异的专项用例**（9.1 的 8 条 + 9.2 的 8 条；第 27 轮把 9.1 的第 7 条计入，第 30 轮把 9.2 的第 7、8 条计入，第 47 轮把 9.1 的第 8 条计入；**第 37/38 轮追加 9.2.1 的 9 条、9.2.2 的 6 条、9.2.3 的 8 条，共 23 条；第 48 轮再追加 9.2.4 的 10 条 + 9.2.5 的 11 条，共 21 条**——它们**全部是错误路径或边缘路径**，golden 基线同样测不到，须由专项单测覆盖），每处至少一个：输入侧（未知 block、image 缺 source、非 assistant thinking、tool_use 缺字段、schema 归一化、媒体分派）与输出侧（多 choice、`finish_reason` 严格、命名空间往返、AWS 流式索引、Bedrock 非 DeepSeek thinking、**同 chunk reasoning+text 都发**）。**现状（2026-10-04 实测，2026-10-05 补第 7 条）**：这些用例**大部分已存在**——`service/convert_claude_request_test.go`（1118 行）与 `service/convert_claude_stream_test.go`（330 行）已逐条覆盖（如 `TestClaudeToOpenAIRequestIgnoresNonAssistantThinkingBlocks`、`TestResponseOpenAI2ClaudeRejectsUnknownFinishReason`、`TestClaudeToOpenAIRequestRejectsNonTextSystemBlock`、`TestStreamResponseOpenAI2ClaudeDoesNotExposeReasoningForNonDeepSeekModel`，以及第 7 条的 `TestStreamResponseOpenAI2ClaudeEmitsThinkingTextAndToolBlocksFromOneChunk`）；两文件均为 git 跟踪且在 `HEAD` 上通过。**阶段 4 的待办不是「补用例」而是「确认这些既有用例在切换后仍通过」**——但这句话有一个此前未写出的前提（**第 35 轮修正**）：**「仍通过」只在 facade/shim 形态下成立**。按本文档字面计划（全删 13 个函数、调用点逐个改写），这 4 个测试文件里有 **82 处调用会直接编译失败**——**不是「失败得其所」，是编译不过**；上游实际形态（保留旧名，见 12.20）下才只剩 `*WithError` 的 17 处与 `IsDeepSeekModelName` 的 2 处要动。**故本项与未决项 33 的形态选择绑定：第 44 轮已定案为 C（照抄上游终态），「确认仍通过」可执行**（facade/shim 保名，`*WithError` 17 处与 `IsDeepSeekModelName` 2 处才需动）。逐文件清点（第 35 轮实测，同包调用无 `service.` 前缀，按 `git show HEAD:<file> | grep -oE` 计数）：

| 测试文件 | 总调用 | 阶段 4 断（`*WithError`×4 与旧包装×4 删除） | 阶段 6 断（`ClaudeToOpenAIRequest` 等主体删除） |
|---|---|---|---|
| `service/convert_claude_request_test.go`（1118 行） | 35 | 7（6×`ResponseOpenAI2Claude` + 1×`ResponseOpenAI2ClaudeWithError`） | 28（`ClaudeToOpenAIRequest`） |
| `service/convert_claude_stream_test.go`（330 行） | 14 | 14（13×`StreamResponseOpenAI2Claude` + 1×`StreamResponseOpenAI2ClaudeWithError`） | 0 |
| `service/convert_test.go`（499 行） | 25 | 11（1×`ResponseOpenAI2Gemini` + 1×`ResponseOpenAI2GeminiWithError` + 1×`StreamResponseOpenAI2Gemini` + 8×`StreamResponseOpenAI2GeminiWithError`） | 14（2×`ClaudeToOpenAIRequest` + 10×`GeminiToOpenAIRequest` + 2×`IsDeepSeekModelName`） |
| `service/convert_golden_test.go` | 8 | 6（`*WithError`） | 2（请求方向）——已由处置清单 12 认领 |

合计 **82 处**（阶段 4 断 38、阶段 6 断 44）。**处置清单 12 只认领 `convert_golden_test.go` 的 8 处；其余三个文件的 11 处 `*WithError` 调用只在形态 C 下被认领（处置清单第 16 项「另 3 文件 11 处随本项增补」，第 36/37 轮补入）——形态 A 下它们零改动（第 14 项 A），形态 B 下无处置项逐条认领（随第 2 项的 `*WithError` 删除一并改写）**（**第 40 轮修正**：原文写「其余三个文件在任何处置清单中零出现」，自第 36 轮第 16 项补入起已不成立）。 **行为层的例外有两处**：第 7 条（同 chunk reasoning+text）与 **finish_reason 严格校验**（**第 47 轮注：后者现覆盖两个方向**——Claude 方向 9.1 第 1 条 + Gemini 方向 9.1 第 8 条，两条都有「切换即失去落点」的同一问题，见 12.11.3）。第 7 条的用例会失败但**不得其所**（本地能力被上游丢弃，见 9.1 表下注），须明确取舍而非默认放行。**finish_reason 一条（第 29 轮新增）更特殊**：`TestResponseOpenAI2ClaudeRejectsUnknownFinishReason` 直接调用 `ResponseOpenAI2ClaudeWithError`——**该函数在阶段 4 即被删除**（处置清单第 2 项、12.3），故它**不会「失败得其所」，而是编译失败**；它钉的行为（未知 finish_reason 报错）在阶段 6 后**无落点**（12.11.1）。阶段 4 须先按 12.11.3 定下 A1/A2/B 三选一（**第 40 轮修正**：原文写「A/B 选项」，第 37 轮起已拆为 A1/A2/B），再决定该用例是改指新落点还是删除，并补 b/c/d 三条新用例（12.11.2）
3. 第 9.3 节的专项清单需扩充，覆盖 12.4.1（索引空洞）、12.4.3（AWS 回归）、12.6（命名空间往返）、12.8（`matchAnyRegex` 语义）、12.7（custom bridge fail-closed）
4. 12.12 的 thinking `signature` 与本节均属「需实测」，须给出可执行命令而非仅标注

**依据**：AGENTS.md 第 6 节「不得把局部测试通过表述成全链路已验证」。若以「快照全绿」作为完成判据，实际会得到**上游行为未变 + 本地行为未知**的结论。

#### 12.16.1 已建成的本地基线（2026-10-04 关闭第 10 节第 25 项）

**产物**：`service/convert_golden_test.go` + `service/testdata/golden/`（8 个 JSON 快照）。

| 矩阵 | 快照 | 覆盖 |
|---|---|---|
| request | `claude_to_openai`、`gemini_to_openai` | 含 tool_use / tool_result / functionCall 的多轮工具调用 |
| response | `openai_to_claude`、`openai_to_claude_deepseek`、`openai_to_gemini` | reasoning 生成 + tool_calls + usage 明细 |
| stream | `openai_to_claude`、`openai_to_claude_deepseek`、`openai_to_gemini` | 逐 chunk 的 SSE 事件序列，含工具参数分片 |

命令：

```bash
go test ./service -run TestGolden -count=1           # 校验
go test ./service -run TestGolden -count=1 -update   # 重录
```

**must-fix（建基线时发现）**：第一版 fixture 全用非 DeepSeek 模型名，导致**thinking 门控整条路径未被任何快照覆盖**——而这恰是本次迁移要保住的第 5 节能力。真实行为是 `shouldExposeReasoningAsClaudeThinking`（`service/convert.go:526`）**仅对 DeepSeek 模型名**暴露 reasoning（门控依据是 `IsDeepSeekModelName`，`:505`）。因此补了 DeepSeek 侧的 response 与 stream 两个快照：非 DeepSeek 快照里 reasoning 被丢弃、DeepSeek 快照里出现 `content_block_start`（`type: thinking`，index 0）并把后续 text/tool 块顺移到 index 1/2。**两侧同时钉住**，门控翻成 always-on 或 always-off 都会被检出。

**门控依赖基线模型名的口径**：`HasDeepSeekModelName`（`:537`）综合 `openAIResponse.Model`、`info.OriginModelName`、`info.UpstreamModelName` 三者判定，故 fixture 的模型名即门控开关，改动 fixture 必须重录快照。

**反向验证（已实测）**：分别扰动 `response/openai_to_claude_deepseek`（thinking→text）、`stream/openai_to_claude_deepseek`（index 2→3）、`request/claude_to_openai`（删 tool_call_id），三次均 `FAIL` 且报「drifted from golden snapshot …」，恢复后 `PASS`。另跑 `-count=3 -shuffle=on` 保持通过，确认无跨运行状态泄漏。

**基线口径（已实测）**：生成时工作区的 `service/convert.go` 有**其他会话的未提交改动**（在 `ClaudeToOpenAIRequest` 的 `else` 分支加 `ReasoningEffort` 透传）。为避免快照编码未提交行为，把 `convert.go` 换成 `HEAD` 版本重跑同一批快照，**结果一致通过**——该改动读 `claudeRequest.OutputConfig`（fixture 未设），对快照为 no-op。故本基线等价于 `HEAD` 口径，快照中不含未提交行为。（生成后已还原工作区文件。）

**第 47 轮补——9.1 第 8 条（oai→gemini 的 finish_reason 严格语义）在本基线上同样无覆盖，且原因与第 7 条不同**：第 7 条是「fixture 的 reasoning 与 text 落在不同 chunk」，本条是**两侧 fixture 的 `finish_reason` 都只有 `tool_calls` 一个非空值**（`convert_golden_test.go` 的非流式 fixture 与流式末块，字面量均为 `"finish_reason":"tool_calls"`；**该文件在基线 `dbfbc096f` 上不存在、建于 `HEAD`**，故不给行号——与 12.20.1 生产表同一条口径提醒，引用时以字面量为准），而 `tool_calls` 在本地严格函数与 relaykit 的 `geminiFinishReason` 里**都映射为 `STOP`**（见 12.11.2b 的 d/g 行）——**该 fixture 恰好落在「无差异」的那一格**，故 8 个快照逐字节一致**不能**证明本方向未退化。这与 12.16.1 末尾「模型名零后缀」那条盲区同型：**基线只钉已覆盖的输入，未覆盖的输入要靠专项单测**。阶段 4 须补一条 `finish_reason` 为未知值（如 `unknown_reason`）的 Gemini 响应方向用例，断言**报错**而非 `STOP`——该用例在阶段 4 之前不存在，且**不得**用 golden 快照承载（错误路径不产出可快照的正常输出，同 12.16.1 的口径）。

**边界（未覆盖，须在阶段 4 单列）**：本基线只钉「当前 HEAD 的本地输出字节」。9.1/9.2 的 16 处差异（9.1 的 8 条 + 9.2 的 8 条；第 27 轮把 9.1 的第 7 条计入，第 30 轮把 9.2 的第 7、8 条计入，第 47 轮把 9.1 的第 8 条计入；未知 block 报错、image 缺 source 报错、多 choice 报错、`finish_reason` 严格校验、非 assistant thinking 门控、tool_use 缺 id/name 报错、schema 归一化、媒体分派等；**第 37/38 轮追加的 9.2.1/9.2.2/9.2.3 共 23 条同属此类；第 48 轮追加的 9.2.4/9.2.5 共 21 条亦同属此类**）**多数是错误路径**，不产出「可快照的正常输出」，因此由既有**专用单测**覆盖（`service/convert_claude_request_test.go`、`service/convert_claude_stream_test.go`，本次已核两文件为 git 跟踪且在 `HEAD` 上通过），**不在** golden 基线的职责内。golden 基线的职责是「正常路径的字节级不漂移」。**唯一例外是第 7 条**（同 chunk reasoning+text 都发）——它是正常路径，但**本 golden 也未覆盖**：本地流式 fixture 的 `reasoning_content` 与 `content` 落在**不同 chunk**（`convert_golden_test.go:255-258`，同属 `HEAD` 口径——该文件在基线 `dbfbc096f` 上不存在），故不触及该分支；它只由单测 `TestStreamResponseOpenAI2ClaudeEmitsThinkingTextAndToolBlocksFromOneChunk` 钉住。阶段 4 须按 9.1 表下注明确取舍。**另一处盲区（第 34 轮补记）**：8 个快照的模型名零后缀用例（仅 `claude-test`/`gpt-test`/`gemini-test`/`deepseek-v4`），`-thinking`/effort 尾缀→思考等级的路径不在任何快照里；该路径在阶段 4 换入 relaykit 后依赖宿主填充器（12.19），须随阶段 4 补一条带后缀的请求快照。

#### 12.16.2 基线的生命周期：阶段 4 必须改指 relaykit，阶段 6 必须迁移（2026-10-05 补充）

**问题（第 19 轮审查发现，且是本轮自己引入的）**：12.16.1 建成的基线调用 6 个**本地**函数，它们全部定义在 `service/convert.go`：

| 基线调用的本地函数 | 定义位置 |
|---|---|
| `ClaudeToOpenAIRequest` | `service/convert.go:18` |
| `GeminiToOpenAIRequest` | `service/convert.go:1248` |
| `ResponseOpenAI2ClaudeWithError` | `service/convert.go:1040` |
| `ResponseOpenAI2GeminiWithError` | `service/convert.go:1831` |
| `StreamResponseOpenAI2ClaudeWithError` | `service/convert.go:730` |
| `StreamResponseOpenAI2GeminiWithError` | `service/convert.go:2101` |

而阶段 6 明确**删除 `service/convert.go` 主体**。两侧的入口签名不同（已从 `upstream/main` 读出，属**签名级证据、非编译验证**）：

| 方向 | 本地入口（`service/convert.go`，HEAD 口径） | relaykit 公开入口（`relayconvert` 包级，`upstream/main`） |
|---|---|---|
| Claude 请求 | `ClaudeToOpenAIRequest(req dto.ClaudeRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error)` | `ClaudeMessagesRequestToOpenAIChat(req dto.ClaudeRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error)`（`request_compat.go:16`） |
| Gemini 请求 | `GeminiToOpenAIRequest(req *dto.GeminiChatRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error)` | `GeminiGenerateContentRequestToOpenAIChat(req *dto.GeminiChatRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error)`（`request_compat.go:24`） |
| Claude 响应 | `ResponseOpenAI2ClaudeWithError(resp *dto.OpenAITextResponse, info *relaycommon.RelayInfo) (*dto.ClaudeResponse, error)` | `ResponseOpenAI2Claude(resp *dto.OpenAITextResponse, info convmeta.Meta) *dto.ClaudeResponse`（`response_compat.go:38`） |
| Gemini 响应 | `ResponseOpenAI2GeminiWithError(resp *dto.OpenAITextResponse, info *relaycommon.RelayInfo) (*dto.GeminiChatResponse, error)` | `ResponseOpenAI2Gemini(resp *dto.OpenAITextResponse, info convmeta.Meta) *dto.GeminiChatResponse`（`response_compat.go:128`） |
| Claude 流 | `StreamResponseOpenAI2ClaudeWithError(chunk *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) ([]*dto.ClaudeResponse, error)` | `StreamResponseOpenAI2Claude(chunk *dto.ChatCompletionsStreamResponse, info convmeta.Meta) []*dto.ClaudeResponse`（`response_compat.go:42`） |
| Gemini 流 | `StreamResponseOpenAI2GeminiWithError(chunk *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) (*dto.GeminiChatResponse, error)` | `StreamResponseOpenAI2Gemini(chunk *dto.ChatCompletionsStreamResponse, info convmeta.Meta) *dto.GeminiChatResponse`（`response_compat.go:132`） |

**入口可达性（必须区分，否则会写出无法编译的迁移）**：relaykit 的真实实现在 `relayconvert/internal/{claude_messages,gemini_chat,oai_chat}/` 下，**`internal/` 包宿主无法 import**。宿主可达的只有 `relayconvert` 包顶层的 `request_compat.go` / `response_compat.go` 两个转发文件。**故上表「relaykit 入口」一列指的是 `relayconvert.` 的包级函数，不是 `internal/` 里的同名实现。** 这与第 77 行已记录的「上游在宿主保留 30 行转发 shim」是同一模式：上游自己也是通过 `relayconvert.` 顶层转发调用 `internal/` 实现的。

差异分四类：**函数名**（`ClaudeToOpenAIRequest` → `ClaudeMessagesRequestToOpenAIChat`；`GeminiToOpenAIRequest` → `GeminiGenerateContentRequestToOpenAIChat`）、**`info` 由具体指针变为接口 `convmeta.Meta`**、**`*WithError` 变体不存在**（12.3 已决定删除，relaykit 侧只有无 error 版本）、**请求方向的 `ctx` 差异并不存在**——`request_compat.go` 的两个请求入口（`:16`、`:24`）内部用 `context.Background()`，**签名里没有 `ctx`**（只有 `OpenAIChatRequestToClaudeMessages`、`OpenAIChatRequestToGeminiGenerateContent` 等反向入口才收 `c context.Context`）。因此请求方向改指时**不需要补 `ctx`**，只需换名与换 `info` 类型。

**若不处理，两种结局都错**：要么阶段 6 连测试一起删掉（第 25 项白做，基线在最需要它的时刻消失），要么测试悬空指向已删除的函数、阶段 6 编译失败后被人草率删除。这与第十一轮发现的 `boundary_test.go` 是同一类错误——**一个阶段的产物对后续阶段有隐含前提，而前提没被写出来**；区别在于那个前提由上游代码带入，这一个由本地基线自带。

**决定**：

1. **阶段 4 把 8 个调用点（6 个不同函数）改指 relaykit**（处置清单第 12 项）：换函数名、`info` 换成 `convmeta.Meta`、删去 `*WithError`。**（第 35 轮注：本节按「直接改写」形态书写；若按推荐形态 C 照抄上游终态（12.20），这 8 处里只有 6 处 `*WithError` 需要改指，2 处请求方向经同名 shim 零改动。）** **快照内容一律不改**——同一批 fixture 的输出在改指后应当逐字节相同，这正是 12.16 所说「本地没退化」的判据。

   **调用点清单（第 30 轮实测，`service/convert_golden_test.go`）**：`ClaudeToOpenAIRequest`(`:204`)、`GeminiToOpenAIRequest`(`:213`)、`ResponseOpenAI2ClaudeWithError`(`:224`、`:238`)、`ResponseOpenAI2GeminiWithError`(`:247`)、`StreamResponseOpenAI2ClaudeWithError`(`:271`、`:291`)、`StreamResponseOpenAI2GeminiWithError`(`:307`)——**共 8 处，分布在 6 个函数上**。两个函数各被调用两次，是因为门控两侧各跑一遍（非 DeepSeek 与 DeepSeek）。**改指时这 8 处都要改，漏掉任一处即编译失败**。

   **「逐字节一致」的前提是两侧结构体字段顺序相同**（`json.MarshalIndent` 按声明顺序输出，顺序不同则语义等价也会失败）。

   **第 31 轮实测推翻本段原结论。** 原文写「已机械核验 fixture 涉及的 4 个输出类型，共有字段的相对顺序两侧全部一致」——**两处错**：其一，**4 个类型里有 1 个不是输出类型**（`OpenAITextResponse` 是 response 方向的**输入**）；其二，真正需要同序的不止顶层类型，而是**从 3 个输出根可达的全部嵌套类型**。按「从 `GeneralOpenAIRequest` / `ClaudeResponse` / `GeminiChatResponse` 出发、沿字段类型递归」重算，本地闭包 **28 个类型**、relaykit 闭包 **36 个**（kit 多出 `Usage`、`BillingUsage`、`CachedTokenDetails`、`InputTokenDetails`、`OutputTokenDetails`、`ReasoningConversionState`、`GeminiPartialArg`、`GeminiGroundingMetadata` 8 个），**两侧共有 28 个，其中 2 个字段顺序不同**：

   | 类型 | 本地顺序 | relaykit 顺序 | 是否影响本基线 |
   |---|---|---|---|
   | `ClaudeMediaMessage`（`dto/claude.go`） | …`Thinking`, **`Signature`**, **`Data`**, `Delta`… | …`Thinking`, **`Data`**, **`Signature`**, `Delta`… | **否**——两者都是 `string` + `omitempty`，快照里 `signature`/`data` 均**未出现**（8 个快照实测 0 次），只有同时非空才影响字节序 |
   | `FunctionRequest`（`dto/openai_request.go`） | …`Parameters`, **`Strict`**, **`Arguments`** | …`Parameters`, **`Arguments`**, **`Strict`** | **否**——快照里 `tools[].function` 实际输出的键是 `["description","name","parameters"]`（`arguments` 只出现在 `messages[].tool_calls[].function`，那是 `FunctionResponse`，两侧顺序一致）；`strict` 在 fixture 与快照里均为 0 次 |

   **另 2 个顺序差异类型不在本基线可达闭包内**：`OpenAIResponsesCompactionRequest`、`ResponsesStreamResponse`（属 responses 方向，本基线不走）。

   **结论不变但理由要换**：本基线 8 个快照**不会因字段顺序而漂移**——不是因为「所有类型同序」，而是因为**被交换的那对字段在快照里恰好都不非空**。这是**运气，不是保证**：本地若产出带 `signature` 的 thinking 块或带 `strict` 的 tool 定义，同一份快照就会因纯序列化顺序而红。**阶段 4 改指后若快照出现「内容相同、顺序不同」的差异，先核字段顺序再谈回归**（这仍是本段原来的正确部分）。

   **已工具化，不再靠人肉**：第 31 轮给 `tools/relaykit-dto-compat/` 加了**声明顺序组 `共有字段声明顺序不同`**（`orderDiff`，嵌入字段也计入顺序），当前包级基线 **order=4**，已进 `--strict` 门禁并经**反转验证**（改成 3 → 退出码 1 报 `order changed: got 3 want 4`；第 38 轮把消息从 `dimension N changed` 改为按组名报，组名即 `reportGroups` 表里的 `key`）。三条新增单测覆盖「顺序不同要报」「同序不报」「单边字段不算顺序差异（那是 name 维度，避免重复计数）」。

   **另一个前置**：本基线声明了 5 个 fixture 常量，但**只有 3 个真被引用**——`fixtureOpenAIChatRequest`（`:121`）与 `fixtureClaudeResponse`（`:181`）在全 `service/` 包内零引用，是死代码（**第 30 轮实测**：两者各自只出现在自己的声明行）。实际用到的输入类型是 `dto.ChatCompletionsStreamResponse`、`dto.ClaudeRequest`、`dto.GeminiChatRequest`、`dto.OpenAITextResponse` 与 `dto.UserSetting`，输出侧是 `GeneralOpenAIRequest`、`ClaudeResponse`、`GeminiChatResponse`。**第 30 轮修正**：原文写「与 12.0.8 那 15 处差异涉及的字段**无一重叠**」（引文中的 15 处为该轮前的旧口径，12.0.8 已修正为 11 处）——**该断言不成立**。逐条核后的实际情况是**有重叠，且输出侧也重叠**：

   | 重叠的差异 | 宿主类型 | 与 fixture 的关系 |
   |---|---|---|
   | `ChatCompletionsStreamResponseChoice.Delta`（tag） | `ChatCompletionsStreamResponse.Choices[].Delta` | **输入侧**，流式 fixture 逐 chunk 都带 `delta` |
   | `GeminiChatRequest.GenerationConfig`（tag） | `GeminiChatRequest.GenerationConfig` | **输入侧**，Gemini 请求 fixture 用的就是该类型（尽管该 fixture 未设 `generationConfig`，`omitempty` 对 struct 值本就无效） |
   | `ToolCallRequest.Function`（tag） | `GeneralOpenAIRequest.Tools[].Function` | **输出侧**，`request/claude_to_openai` 与 `request/gemini_to_openai` 两个快照都输出 `tools` |

   **但结论仍成立**，理由与原文给的**不是同一条**：这 3 处重叠的 tag 差异**全部是 struct 值的空操作**（12.0.6 已核：两侧都输出 `"delta":{...}`／`"generationConfig":{...}`／`"function":{...}`），因此阶段 2 的对齐动作**不应**改变这 8 个快照。**不能用「无重叠」当理由**——那是不成立的事实；正确理由是「重叠的那几处都是空操作」。12.0.6 列的另两处**真实变化**（`IncompleteDetails.Reason`、`ClaudeServerToolUse.WebSearchRequests`）**确实与本基线无重叠**：前者属 `OpenAIResponsesResponse`（本基线不走 responses 方向），后者属 `ClaudeUsage.ServerToolUse`（`dto/claude.go:628`，而快照里实测无 `server_tool_use`／`web_search_requests` 键）。故若阶段 2 后快照漂移，**不要归因于这 11 处差异**——但理由是「重叠的几处为空操作」，不是「无重叠」。
2. **差异必须逐条解释，不得用 `-update` 吸收**：若改指后快照有差异，每一处都要能对应到**处置清单第 6 项**为 9.1/9.2/9.2.1/9.2.3（9.2.2 归第 17 项）逐条产出的「保留/放弃/改写」决定（**第 38 轮修正**，原文只写 9.1/9.2）（该清单在阶段 4 内产出，见第 6 项），否则视为回归。**禁止**用 `go test -update` 把差异直接写成新的正确值——那正是 12.16 批评上游 `-update` 机制的原话（「无法区分『保真生效』与『行为回归』」）。
3. **阶段 6 不是「整体移入」，而是「把 fixture 与门控用例并入上游已有的 `golden_test.go`」**（详见下节 12.16.3：直接移入会因包级符号重名而编译失败）。
4. §9.3 增列该基线的校验命令。

#### 12.16.3 上游已有一份同名 golden 测试，且它恰好漏掉本地要保的那条路径（2026-10-05 补充）

上节决定 3 的原始表述是「阶段 6 把测试与快照整体移入 `relaykit/relayconvert/`」。**该表述会编译失败**，实测证据：

`upstream/main:relaykit/relayconvert/golden_test.go`（`package relayconvert`，同一包）已声明以下**与我方同名**的包级符号（**第 30 轮修正**：其中 `mustUnmarshalFixture` 是**同名不同签名**，其余 4 个同名同签名——同名即冲突，签名不同不改变冲突结论）：

| 符号 | 上游 `golden_test.go` | 本地 `convert_golden_test.go` |
|---|---|---|
| `updateGolden` | `:25` `var updateGolden = flag.Bool("update", ...)` | `:32` 同 |
| `goldenDir` | `:42` `const goldenDir = "testdata/golden"` | `:34` 同 |
| `marshalGolden` | `:55` `func marshalGolden(t *testing.T, value any) []byte` | `:47` 同签名 |
| `checkGolden` | `:62` `func checkGolden(t *testing.T, name string, got []byte)` | `:54` 同签名 |
| `mustUnmarshalFixture` | `:460` `func mustUnmarshalFixture(raw string, out any)`（**无 `*testing.T`**，失败即 `panic`） | `:112` `func mustUnmarshalFixture(t *testing.T, raw string, v any)`（**签名不同**：多一个 `t`，用 `require.NoError` 而非 `panic`）。**合并时须删本地这一个、改用上游的**，而本地 **8 处调用**都传了 `t`（`:202`、`:211`、`:222`、`:236`、`:245`、`:270`、`:290`、`:306`），须逐个去掉首参 |
| `TestMain` | `:29` | 无（本地不需要） |

两文件的 `normalizeVolatile`/`normalizeGolden` 也是**同一套正则**（`[0-9a-f]{32}` → `<uuid>`、`("created(_at)?"\s*:\s*)\d{9,}` → `${1}0`）。因此同包共存必然 `redeclared in this block`。**修正后的做法**：阶段 6 只把**本地独有**的部分并入上游文件——DeepSeek 门控 fixture 与两条 `*_deepseek` 用例——复用上游既有的 `goldenInfo()`/`checkGolden()`/`checkStreamEventsGolden()`，不重复声明任何 helper。

**更重要的是：上游那份测试恰好漏掉本地要保的那条路径。** 上游 24 个快照里，response 矩阵**显式排除** `openai_to_claude`：

```go
// upstream/main:relaykit/relayconvert/golden_test.go:323-325
if from == to || to == types.RelayFormatGemini ||
    (from == types.RelayFormatOpenAI && to == types.RelayFormatClaude) {
    continue
}
```

`response/` 目录实测确实没有 `openai_to_claude.golden.json`（只有 `claude_to_openai`、`gemini_to_claude`、`openai_responses_to_claude` 等 8 个）。而**本地 thinking 门控正落在这条被排除的路径上**（`ResponseOpenAI2Claude`，见 12.16.1）。

stream 矩阵**包含** `openai_to_claude`，但实测该快照里 `thinking` 出现 **0 次**，且上游 fixture 的模型名是 `stream-model`、`goldenInfo()` 不设 DeepSeek 模型名，**chunk fixture 里完全没有 `reasoning_content`**。因此上游 24 个快照**没有一个覆盖 thinking 暴露路径**。

这从**上游侧独立印证**了 12.16.1 在本地侧发现的问题，并把 12.16 原有的论断精确化：原文写「24 个快照一处都测不到 9.1/9.2 的 12 处差异」——现在可以指出**最要害的那一处（thinking 门控）是被上游矩阵的 `continue` 条件主动排除的**，不是偶然遗漏。这也解释了为什么必须补本地基线，以及为什么补出来的 DeepSeek 侧快照在上游找不到对应物：**它填补的正是上游刻意留白的格子**。

**待阶段 6 实测的边界**：合并后上游 `TestMain`（`:29`）会接管 `flag.Parse()`，本地文件的 `-update` 行为随之改变（同一 flag，语义不变）。

**快照文件名冲突（实测，非推测）**：本地 8 个快照与上游 24 个快照的**交集是两个**——`stream/openai_to_claude.golden.json` 与 `stream/openai_to_gemini.golden.json`，且**两者内容都不同**（上游 3489 / 4050 字节，本地 1536 / 4243 字节）。这两个名字**必须改名**（例如加 `_local` 后缀）才能并入，否则合并即**静默覆盖上游快照**——而上游那两个快照正是 `TestGoldenStreamConversionMatrix` 的基线。两条 `*_deepseek` 快照（`response/openai_to_claude_deepseek`、`stream/openai_to_claude_deepseek`）**不与上游重名**，是纯新增。**注意方向**：冲突源是**不带后缀**的两条，不是带 `_deepseek` 的两条——上一版本此处写反了。

**改名之外还有一层 fixture 混淆**：上游 `goldenInfo()` 用 `UpstreamModelName: "upstream-model"`、流式状态用 `Model: "stream-model"`，而上游快照的 payload 与本地同名快照**高度相似**：`response/claude_to_openai` 两侧 `model` 都是 `claude-test`；`stream/openai_to_claude` 上游首事件是 `message_start`（含 `"model": "gpt-test"`），**本地那条没有 `message_start`**（首事件直接是 `content_block_start`）。即两套 fixture 在同名快照上产出**结构相似但内容不同**的 JSON，且**文件名与部分字段值不足以区分两者**。合并时若只按文件名判断「已有基线，跳过」，会**拿上游基线去比本地输出**，得到的是结构性假失败（事件序列起点都不同）。**改名是前提，且不能靠字段值判断两个快照是否同源。**

**待阶段 4 实测的边界**（不得写成既成事实）：

- 上表 relaykit 侧签名读自 `upstream/main` 的 `request_compat.go` / `response_compat.go`，**未经编译验证**——relaykit 目录在阶段 1 才拷入本仓库，`convmeta.Meta` 的构造方式（`RelayInfo` 在阶段 3 才实现该接口）要到阶段 4 才能确认。
- 两个**请求方向**入口（`ClaudeMessagesRequestToOpenAIChat`、`GeminiGenerateContentRequestToOpenAIChat`）签名**不含 `ctx`**——它们在内部用 `context.Background()`，因此宿主无法向转换过程传递请求上下文或取消信号。本地现有实现同样不接收 `ctx`（只接收 `info`），故**行为等价**；但若阶段 4 需要传递 trace/diagnostic 上下文，须另找路径（`request_compat.go` 的反向入口才收 `c context.Context`）。
- **流式路径的状态承载两侧同构，改指时不需要构造新对象**：本地 `streamResponseOpenAI2Claude` 读 `info.ClaudeConvertInfo`（`Done`、`Index`、`LastMessagesType`、`ToolCall*`），relaykit 版读 `info.EnsureClaudeConvertInfo()` 返回的同类状态。两侧都把状态挂在 `info` 上，**签名里都没有独立的 `state` 参数**。因此改指 `StreamResponseOpenAI2Claude` / `StreamResponseOpenAI2Gemini` 时，只需把 `goldenRelayInfo` 返回的 `*relaycommon.RelayInfo` 作为 `convmeta.Meta` 传入即可——但**前提是该 `RelayInfo` 的 `EnsureClaudeConvertInfo()` 返回的状态初始值与本地 `goldenRelayInfo` 里手工设的 `ClaudeConvertInfo{LastMessagesType: LastMessageTypeNone}` 等价**，否则首个 chunk 的 `Done`/`Index` 起点不同、快照会整体偏移。这一条**待阶段 4 实测**（阶段 3 才把 `ClaudeConvertInfo` 改为 `convmeta` 别名）。
- 本地 `goldenRelayInfo` 返回 `*relaycommon.RelayInfo`，改指后需换成 `convmeta.Meta`；阶段 3 已让 `RelayInfo` 实现该接口（编译期断言 `var _ convmeta.Meta = (*RelayInfo)(nil)`），故 `goldenRelayInfo` 的返回值可直接作为 `Meta` 传入，无需改 fixture。

### 12.17 决策汇总

**本表用压缩编号（1–26），与第 10 节的原始编号（1–34）不同**——已关闭的条目在此合并，编号不一一对应。**跨表引用一律用第 10 节的原始编号**（即 12.18/12.19/处置清单里写的「未决项 33」指第 10 节第 33 项）。

逐行映射（已核对）：本表 1–21 = 第 10 节 1–21；本表 22 合并了第 10 节的 22（`dto` import 图）与 23（33 个字段不兼容类型）；本表 23 = 第 10 节 28；本表 24 = 第 10 节 29；本表 25 = 第 10 节 30；本表 26 = 第 10 节 34。

**第 10 节有、本表无独立行的七项**：24（`openaicompat` tool 循环重构，明确不在本次范围，见 12.15.3）、25（本地行为 golden 基线，已关闭，见 12.16）、26（命名空间字段生产引用计数，已关闭，见 12.6）、27（`dto` 宿主 import 归零后 3 个符号的归位，已关闭，见 12.0.7；本表第 21 行只覆盖「归零」本身）、31（`finish_reason` 严格语义落点，已并入本表第 12 行，第 44 轮定案为 B）、32（12.5 的 3 个待实测方法，属阶段 2 前置，见该节）、33（切换形态，第 44 轮定案为 C，见 12.20 与 §8.1）。**这七项在本表无行不等于无主——每项都给了上列落点。**

| # | 未决项 | 决定 |
|---|---|---|
| 1 | 本地独有能力盘点 | **已完成**（第 4 节） |
| 2 | think 标签族归属 | 剥离迁 relaykit，门控留宿主 |
| 3 | 本地专有错误符号 | 迁入 `relaykit/types` |
| 4 | `*WithError` 系列 | 删除 |
| 5 | 响应方向暴露策略形态 | **relaykit 内门控**（`convmeta.Options` 钩子），在索引分配之前（精确地说：进入 `reasoning/text` 二选一之前）。后置过滤不可行，见 12.4。落点为 `oai_chat/to_claude_messages_resp.go` 两条入口（已可达 `Meta`，零透传成本）。**钩子 nil 语义为「保留上游默认」、单参钩子由调用点对三个模型名各调一次取或、落地时机在阶段 4 而非阶段 5**——三处修正见 12.4.4 |
| 6 | DTO 方法归属 | 4.4 的 23 个：A 21 / B 2 / 待实测 0（第 42 轮定案，3 个原「需实测」均判 A）。**全包 394 vs 273 函数（含测试；非测试 306 / 215）**，完整差集见 12.0.5 |
| 7 | hosted 工具收敛 | 采用上游 `toolconv`（10 种 `Kind`），本地非测试只有 3 种且是其子集，差集为空 |
| 8 | `types` 别名转发范围 | 仅 `endpoint_type.go`、`finish_reason.go` 两类常量；其余改 import |
| 9 | 补丁 2 对 golden 快照 | 需实测，逐条核对 |
| 10 | `ConvOptions` 缓存语义 | 上游语义采用；**本地须落地两处**（第 34 轮修正）：阶段 3 声明 `convOptions` 字段 + 本地 `InitChannelMeta` 补 `info.convOptions = nil`（本地 `relay/common/relay_info.go:358` 无此行）。判据已改为「换渠道重试后快照反映新渠道」，见 12.10 |
| 11 | 钩子闭包捕获 | 收窄：钩子只答策略、不捕获状态 |
| 12 | `reasonmap` 严格模式 | 保留宽松 default；严格语义**第 44 轮定案为 B**（宿主 5 处调用点经 1 个共享助手前置校验，复用从被删 `service/convert.go` 迁出的 `stopReasonOpenAI2ClaudeStrict`；A1 为 A2 超集、A2 退化为 B，均排除，见 12.11.3） |
| 13 | thinking `signature` | 维持现状，列入运行时验证 |
| 14 | `enabled` 双语义 | 保留并文档化 |
| 15 | 上游 relaykit 分支 | 不采用，main 已含最终形态 |
| 16 | 命名空间/tool_search 归属 | 保留宿主；其字段横跨 6 个顶层目录（8 个 Go 包）、11 个非测试文件，阶段 2 须一并决策（12.6） |
| 17 | custom tool bridge 门控 | 保留本地 fail-closed |
| 18 | `matchAnyRegex` 空模式 | 采用本地语义并文档化 |
| 19 | 未跟踪文件 | 阶段 0 门禁，先提交 |
| 20 | `openaicompat` 收敛边界 | **整体保留**。按能力切分在函数粒度不成立（12.15） |
| 21 | `dto` 宿主 import 归零 | **阶段 2 必须清零**，`boundary_test.go` 是硬门禁，不得加 `allowedViolations`（12.0.4）。3 个符号换 stdlib、`ResponsesRequestMetadata` 随 opaque request 留宿主（12.0.7） |
| 22 | `dto` 方法/函数/字段差集与 import 图 | **已完成**（12.0.5）：方法本地独有 101、relaykit 独有 31；函数 65 / 44；136 个同名结构体 33 个不同，其中 16 个需动作（另 17 个按字段名口径为增益，其中 `ClaudeServerToolUse` 另有 tag 差异，见 12.0.6；**第 39 轮修正**：原文写「其中 10 个」，那是全部 136 个的 tag/type 口径，与这 17 个的交集只有 1 个）；import 图**无环** |
| 23 | 通道级 Claude 入向转换器 | **已决定**（12.15.4）：收敛到 relaykit，宿主只留钩子接线，459 行本地实现删除（原未决项 28） |
| 24 | `dto` 未导出缓存字段承载 | **已决定**（12.0.7）：`EffectiveTools()` 连字段迁入 `relaykit/dto`，同包内未导出字段合法（原未决项 29） |
| 25 | 字段 tag / Go 类型差异 | **已决定**（12.0.8）：**11 处**（tag 5 + 类型 6；第 30 轮修正，原文写 15）全部采用 relaykit、零打补丁。tag 5 处中 3 处空操作（struct 值 `omitempty` 无效）、2 处风险排除；类型 6 处含 1 处纯增益、3 处改读写点、2 处改 import |
| 26 | 思考等级后缀填充器（第 34 轮新增） | **第 44 轮定案为 A**（12.19、未决项 34）：移植上游 `relay/helper/reasoning_suffix.go` + `model_modifier.go`（加性补 `ParseLegacyModelSuffix`/`ShouldPreserveEffortTail`/`PreserveEffortTail`）；本地词表是上游严格子集（无 `-thinking`/budget/`@modifier`、OpenAI 词表缺 `-max`），B 等于在宿主重写上游语义。DeepSeek 富词表保持本地实现，不得被上游窄词表覆盖 |
| 27 | **Gemini 请求方向入口转换器的严格性（第 48 轮新增）** | **待批 3 判定**（9.2.4、9.2.5、处置清单第 18 项）：两条 Gemini 请求方向（gemini→oai 的 `GeminiToOpenAIRequest`、oai→gemini 的 `CovertOpenAI2Gemini`）的 21 条差异（20 个严格校验点 + 1 行无差异确认）在 relaykit 侧全部静默化，归处置清单第 18 项逐条裁定。**发现路径**：4.5.5 的计数口径（名含 `<字母>2<字母>` 且含 `request`/`response`）漏掉了 `CovertOpenAI2Gemini`（名字拼写 `Covert`、且无 request/response 字样），由反向枚举（52 全量 − 44 含 request/response = 8 排除名）暴露 |

### 12.18 本轮审查新增的未决项

| # | 项 | 说明 |
|---|---|---|
| 21 | ~~`dto` 完整方法级差集~~ | **已关闭**（2026-10-04）：见 12.0.5——非测试口径本地方法 229 / relaykit 159，本地独有 101、relaykit 独有 31；包级函数本地 72 / relaykit 51，独有 65 / 44。已按文件归组给出性质。同时修正 12.0.2 的「394 vs 273」漏标**含测试**口径（非测试为 306 / 215） |
| 22 | ~~`dto` import 图分析~~ | **已关闭**（2026-10-04）：见 12.0.5 第六节——`constant` 是叶子（0 个 relaykit import），`types`/`kitutil` 均不 import dto，**成环不成立**。真正的问题是 `constant.ChannelType*` 在 `relaykit/types` 中不存在，属归属问题（12.0.4） |
| 23 | ~~33 个字段不兼容类型的逐项对齐~~ | **已关闭**（2026-10-04）：见 12.0.5 第五节——33 个中 **16 个需动作**（含 `ChannelOtherSettings` 20 / `OpenAIResponsesCompactionRequest` 20 / `UserSetting` 3 等保留项，`Message.ThinkingContent` 等保留项），**17 个取 relaykit 即纯增益** |
| 24 | `openaicompat` tool 循环重构 | 让基础分支可独立转调 relaykit 的唯一路径；独立重构，不在本次范围（12.15.3） |
| 25 | ~~本地行为的 golden 基线~~ | **已关闭**（2026-10-04）：见 12.16——`service/convert_golden_test.go` 建于 `HEAD`，8 个快照覆盖请求/响应/流三矩阵，且把 thinking 门控的两侧（DeepSeek 暴露 / 非 DeepSeek 丢弃）都钉住；反向验证：扰动任一快照即失败 |
| 26 | ~~命名空间字段的生产引用精确计数~~ | **已关闭**（2026-10-03）：见 12.6，已给出「口径 + 命令 + 结果」——11 个非测试文件、跨 6 个顶层目录（8 个 Go 包）。早期写的「4 个包」无法用任何单一命令复现 |
| 27 | ~~`dto` 宿主 import 归零后 3 个符号的归位方式~~ | **已关闭**（2026-10-04）：见 12.0.7——三个符号是 `encoding/json` 薄包装，换 stdlib；`ResponsesRequestMetadata` 判为宿主类型，随 `dto/opaque_openai_responses_request.go` 留在宿主 `dto` |
| 28 | ~~通道级 Claude 入向转换器的归属（第 12 轮新增）~~ | **已关闭**（2026-10-04；第 28 轮补注）：见 12.15.4——收敛到 relaykit。三类宿主耦合 relaykit 侧均有注入机制（`convmeta.Options` 钩子 / `MediaResolver` / `FunctionParametersToInputSchema`），阶段 4 接线后删除 459 行本地实现。**但第 28 轮实测两条边界均不成立**（12.15.4.1 的 4 处 schema 差异、12.15.4.2 的 7 处媒体分派差异），故收敛须连同改上游实现（本地改动清单第 10、11 行），不是纯接线 |
| 29 | ~~`dto` 未导出缓存字段的承载方式（第 12 轮新增）~~ | **已关闭**（2026-10-04）：见 12.0.7——`EffectiveTools()` 连缓存字段迁入 `relaykit/dto`，未导出字段在自己包内声明合法（上游已有 `Reasoning.effortRaw` 同例），无需旁路。13 个调用点改 import |
| 30 | ~~字段名相同但 tag / Go 类型不同（第 14 轮新增）~~ | **已关闭**（2026-10-04；**第 30 轮修正计数**）：见 12.0.8——**11 处**（tag 5 + 类型 6，原文写 15）全部采用 relaykit。tag 3 处空操作（struct 值）、2 处风险排除；类型 `CreatedAt` 纯增益、`IncludeThoughts`(8 处)+`Text`/`Arguments` 改读写点、`SupportedEndpointTypes` 改 import |
| 31 | **finish_reason 严格语义的落点（第 29 轮新增，第 37 轮修正为三选，第 44 轮定案）** | **已定案为 B**（第 44 轮，见 12.11.3）：宿主 5 处 Claude 响应方向调用点经 **1 个共享助手**前置校验——把 `stopReasonOpenAI2ClaudeStrict`（基线 `service/convert.go:1125`，覆盖 a/b/c/d）从被删主体迁入宿主助手，在调 relaykit 前校验 + 归一到 OpenAI 词汇表（`""`→`stop`、`function_call`→`tool_calls`、`TrimSpace`+`ToLower`）。**A1 排除**（是 A2 的超集，含 `response_compat.go:38`/`:42` 两处公开签名分叉）、**A2 排除**（退化为 B + 一个只为选分支的钩子）。原「选项 A」的布尔钩子不产生 error（`reasonmap.OpenAIFinishReasonToClaudeStopReason` 与 `oai_chat` 3 个调用它的函数都无 error 返回通道，第 37 轮实测）。另须为 b/c/d 三条差异各补测试（12.11.2）；流式侧只校验未知值、不得改写空串 |
| 32 | ~~**12.5 的 3 个「需实测」方法的分类（第 32 轮新增）**~~ | **已关闭**（第 42 轮）：见 12.5 与「阶段 2 的前置」——`Usage.EnsureResponsesTokenDetailObjects`、`Usage.GetOutputTokenDetails`、`OpenAIResponsesResponse.HasImageGenerationCall` 三者在 relaykit 全仓零命中、方法体零宿主依赖，均判 **A（迁入 `relaykit/dto`）**。**`EnsureResponsesTokenDetailObjects` 的 A 为条件结论**（该对象在 `HEAD`/基线上不存在，只存在于工作区——其他会话的未提交改动，迁入前须确认最终形态）。原引的 `ImageGenerationCallCounter.Observe` 判为不同能力（上游**宿主**的有状态计费计数器，且本地无该文件） |
| 33 | **阶段 4 的切换形态（第 33 轮新增；第 35 轮扩为三选一，第 44 轮定案）** | **已定案为 C（照抄上游终态）**（第 44 轮，见 §8.1、12.20、12.20.1）：**A 薄封装**（6 个同名转发函数，含 `*WithError` 名字，与上游实际保留的 5+2 个不同）与 **B 直接改写调用点** 均不采用。上游 `main` 保留 5 个 facade + 2 个 shim + 包装层，调用点改动面最小（测试侧 `service/` 4 文件必改 17 处、通道级 4 文件另必改 33 处，而非全删形态下 `service/` 4 文件的 82 处）。§8.1 要求阶段 4「单点可逆」，C 的切换 = `service/convert.go` 单文件的函数体替换，回滚最集中（完整回滚清单见 12.20.1 末段：删 4 新文件 + revert 5 个既有文件 + revert 生产改写 18 处 + revert 测试侧 50 处；**第 49 轮订正**：C7 的 `log_info_generate.go` 与 C4 的 `relay_info.go` 是既有文件） |
| 34 | **思考等级后缀填充器缺失（第 34 轮新增，第 44 轮定案）** | **已定案为 A（移植上游填充器）**（第 44 轮，见 12.19）：移植上游 `relay/helper/reasoning_suffix.go` + `model_modifier.go`（加性补 `setting/reasoning.ParseLegacyModelSuffix`、`model_setting.ShouldPreserveEffortTail`、`convmeta.Options.PreserveEffortTail` 接线）。relaykit 的 4 处 `ReasoningStateOf(info)` 消费点 + `ApplyClaudeThinkingModel` 依赖 `info.ReasoningConversion`，relaykit README 明示转换器不再解释 `-thinking`/effort 尾缀——后缀意图必须由宿主入口填充。**本地词表是上游严格子集**（无 `-thinking`/budget/`@modifier`，OpenAI 词表缺 `-max`），故 A 而非 B。**DeepSeek 富词表（`ParseDeepSeekV4ThinkingSuffix`）保持本地实现，不得被上游窄词表覆盖**（本地有 4 个生产调用点）。不装填充器即静默丢失跨协议路径的思考等级，8 个 golden 快照零后缀用例、抓不到该回归 |

### 12.19 思考等级后缀的宿主填充器（第 34 轮新增，未决项 34）

**问题**：relaykit 侧**不解释模型名尾缀**。README 原文：*"`ThinkingAdapterEnabled` 只控制是否把已解析的推理意图渲染到 Claude / Gemini 请求上。`-thinking` / `-nothinking` / effort 尾缀等命名约定不再由转换器自动解释。若你的入口仍使用这些模型名后缀，请在调用转换前自行调用 `relayconvert/reasoning` 的 `Parse*` 帮助函数，把结果写成 `dto.ReasoningConversionState`，并通过 `convmeta.Meta.ReasoningState()`（`convmeta.Values.ReasoningConversion`）传入。同时把发给上游的模型名裁成无后缀基础名。"*

**`convmeta.Values.ReasoningConversion` 的接线（第 46 轮补）**：relaykit README 描述的填充路径是「把结果写成 `dto.ReasoningConversionState`，并通过 `convmeta.Meta.ReasoningState()`（`convmeta.Values.ReasoningConversion`）传入」。实测 `relaykit/relayconvert/convmeta/meta.go:100` 确有 `Values.ReasoningConversion` 字段、`:161` 的 `Values.ReasoningState()` 读它；但**阶段 3 只声明了 `RelayInfo.ReasoningConversion` 字段（`relay/common/relay_info.go`）、无 `Values` 路径的接线计划**。两者是不同机制（一个是 `Meta.ReasoningState()` 直读、一个是 `ConvOptions()` 快照内取值）。**处置**：填充器 A 移植后，`ApplyReasoningModelSuffix` 写 `info.ReasoningConversion`，宿主在 `ConvOptions()` 构建函数里把该值拷进 `Values.ReasoningConversion`（或让 `RelayInfo.ReasoningState()` 直接返回 `info.ReasoningConversion`，绕开 `Values`）——**阶段 4 批 2 须二选一并落进第 15 项的接线动作**。

**消费侧（已实测）**：relaykit 内部 4 处读取 `ReasoningStateOf(info)`——`relaykit/relayconvert/internal/shared/claude/reasoning.go:24`（oai→claude 与 oai_responses→claude 两条请求转换经 `sharedclaude.ApplyReasoning`）、`relaykit/relayconvert/internal/shared/gemini/request.go:107`、`relaykit/relayconvert/internal/claude_messages/to_oai_responses_req.go:96`、`relaykit/relayconvert/internal/gemini_chat/to_oai_chat_req.go:56`。原生 Claude 路径的渲染入口 `relayconvert.ApplyClaudeThinkingModel`（`relaykit/relayconvert/request_compat.go:49`）同样经 `sharedclaude.ApplyReasoning` 读它，由 `upstream/main:relay/channel/claude/adaptor.go:39` 调用（该行为上游终态；本地基线 `relay/channel/claude/adaptor.go` 尚无此调用）。

**生产侧（已实测）**：上游宿主在入口层填充——`relay/helper/reasoning_suffix.go`（232 行）的 `ApplyReasoningModelSuffix` 置 `info.ReasoningConversion`（`:71`）并裁剪 `UpstreamModelName`，非测试调用点 6 处、分布于 5 个文件：`upstream/main:relay/claude_handler.go:41`、`upstream/main:relay/compatible_handler.go:46`、`upstream/main:relay/gemini_handler.go:40`、`upstream/main:relay/gemini_handler.go:201`、`upstream/main:relay/responses_request.go:43`、`upstream/main:controller/channel-test.go:262`。**本地两个上游文件都不存在**（`relay/helper/` 下无 `reasoning_suffix.go`、无 `model_modifier.go`），也没有任何写 `info.ReasoningConversion` 的代码——字段本身要阶段 3 才引入。

**为什么这是缺口而不是「本地本来就有」**：本地现有后缀处理全部是内联的，且随阶段 4/6 删除或收敛：

- `relay/channel/claude/relay-claude.go:366-414`（HEAD 口径曾是 `:430-478`，第 36 轮重锚）（`RequestOpenAI2ClaudeMessage` 内：`TrimEffortSuffix` 与 `-thinking` 两段）——**第 39 轮修正**：原文写「`TrimEffortSuffix` / `-thinking` / `ReasoningEffort` 三段」，但第三段 `ReasoningEffort` 在 `:416-435`，不在 `:366-414` 内（`git show dbfbc096f:relay/channel/claude/relay-claude.go` 实测）。该函数是阶段 4 收敛到 `relayconvert.OpenAIChatRequestToClaudeMessages` 的 459 行之一（4.5.5 表）；
**`claude_handler` 换装的落点（第 44 轮补，配合填充器 A）**：上游形态实测——`relay/claude_handler.go:41` 只做一行 `helper.ApplyReasoningModelSuffix(c, info, request)`（这就是上文所说「本地零落点」的换装），真正的 thinking 渲染在**通道适配器** `upstream/main:relay/channel/claude/adaptor.go:39` 的 `relayconvert.ApplyClaudeThinkingModel(request, info)`（`ConvertClaudeRequest` 内；本地基线无此调用，见 12.19 前述「本地不存在该文件」同理）。**填充器（处置清单第 15 项）与换装（本项）是两件事但强绑定**：`ApplyClaudeThinkingModel` 只从 `ReasoningState()` 取意图，故换装前必须先有填充器写入 `info.ReasoningConversion`。**第 44 轮定案**：填充器随 A 移植（12.19）；换装（`upstream/main:relay/claude_handler.go:41` 的 `ApplyReasoningModelSuffix` + `upstream/main:relay/channel/claude/adaptor.go:39` 的 `ApplyClaudeThinkingModel`）**随填充器同批在阶段 4 落地**——否则 §5「思考等级」的承载链在原生 Claude 路径上仍由本地硬编码承载，与 §3.1「采用上游」的声明不符。落点补入阶段 4 处置清单（与第 15 项同批）。

**不装填充器的后果**：`ReasoningState()` 恒 nil → `IntentFromState(nil)` 为空意图 → 上述全部消费点的后缀分支成死代码。客户端发 `claude-opus-4-7-high`、`gpt-5.6-high`、`-thinking` 这类模型名时，跨协议路径（oai→claude 等）上的思考等级渲染**静默丢失**，且上游模型名不再被裁成基础名。这正是第 5 节承诺保留的「思考等级」，不能接受。

**现有验证网的盲区（已实测）**：8 个 golden 快照的 fixture 模型名只有 `claude-test`/`gpt-test`/`gemini-test`/`deepseek-v4` 四个，**零后缀用例**——这条回归路径不在任何现有快照里。阶段 4 须为带后缀的请求补一条快照或单测，与处置清单第 15 项配套。

**第 44 轮定案：选 A（移植上游填充器）。** 判据是**词表与解析能力的包含关系**（第 44 轮实测，逐条命令在下）：

| 能力 | 上游 `relaykit/relayconvert/reasoning` + 宿主 `setting/reasoning` | 本地 `setting/reasoning`（基线） |
|---|---|---|
| effort 尾缀（`-max`/`-xhigh`/`-high`/`-medium`/`-low`/`-minimal`） | 有 | 有 |
| OpenAI `-none` | `OpenAIEffortSuffixes` 含 `-none` | `OpenAIEffortSuffixes` 含 `-none`（词表相同） |
| OpenAI `-max` | `OpenAIEffortSuffixes` **含 `-max`**（7 值） | **缺 `-max`**（6 值：`-high`/`-minimal`/`-low`/`-medium`/`-none`/`-xhigh`）——`gpt-*-max` 在本地不被裁 |
| `-thinking` / `-nothinking` 别名 | 有（`ParseClaudeModelSuffix`/`ParseGeminiModelSuffix`/`ParseLegacyModelSuffix`） | **零**（全仓无任何解析） |
| `-thinking-<budget>` 数字预算 | 有（`parseProviderModelSuffix`） | **零** |
| `@key:value` 修饰符（`@thinking:adaptive`、`@effort:high`） | 有（`ParseModelModifiers`/`ParseThinkingModifier`） | **零** |
| 家族白名单（只对 `gpt-*`/`o[1-9]*`/`claude-*`/`gemini-*` 生效，未知 OpenAI 兼容名不动） | 有（`legacyOpenAIModelPattern` 等三个 regexp） | **零**（本地 `ParseOpenAIReasoningEffortFromModelSuffix` 对任何以尾缀结尾的名字都裁，无家族判定） |
| mapped-model 覆盖 / pass-through 短路 | 有（`ApplyReasoningModelSuffix`） | **零** |
| `ShouldPreserveEffortTail` 逃生舱（`gpt-5.1-codex-max` 保持不裁） | 有 | **零** |
| DeepSeek 富词表（`-minimal`/`-low`/`-medium`/`-high`/`-xhigh`/`-max`/`-ultra` 归一化到 `low`/`high`/`max`） | **归一为 `-none`/`-max` 两值** | **保留富词表**（见下） |

**结论**：本地能力是上游能力面的**严格子集**——除 DeepSeek 富词表一处外，本地连 `-thinking`/budget/`@modifier` 都不解析，且 OpenAI 词表还缺 `-max`。选 B（本地等价物）意味着**在宿主从头写这些解析**，即本文档反复反对的「在宿主复制套件逻辑」；且上游这些语义（含 `@modifier`、家族白名单）本地**无从对表**（本地根本没实现，谈不上「与本地现状严格一致」——原文该措辞只在 DeepSeek 一维成立）。**故 A 是唯一与「避免分叉、不复制套件逻辑」两条原则皆不冲突的选项。** A 的两处增益须在阶段 4 显式记录为行为变化（不静默）：`-max` 开始被裁（`gpt-*-max` → effort=max）、家族白名单生效（非 GPT/o 系列的名字不再被裁尾缀）。

**A 的移植范围（第 44 轮据上游实测钉死，取代原文「两文件 510 行」的粗略估计）**：

| # | 落点 | 上游来源（`789c97019`） | 动作 |
|---|---|---|---|
| A1 | `relay/helper/reasoning_suffix.go` | `relay/helper/reasoning_suffix.go`（232 行） | 整文件移植 |
| A2 | `relay/helper/model_modifier.go` | `relay/helper/model_modifier.go`（278 行） | 整文件移植（`modelModifierDiagnostic`/`extractTemperature`/`extractTopP` 等） |
| A3 | `setting/reasoning` 加 `ParseLegacyModelSuffix` | `upstream/main:setting/reasoning/suffix.go:35` | **加性补齐**（本地该文件被 9 个文件 import，只能加、不能整体替换——原文第 42 轮已警示） |
| A4 | `model_setting.ShouldPreserveEffortTail` | `upstream/main:setting/model_setting/global.go:167` | 本地无（本地只有 `ShouldPreserveThinkingSuffix`，`setting/model_setting/global.go:208`），加性补齐 |
| A5 | `convmeta.Options.PreserveEffortTail` 接线 | `upstream/main:relay/common/relay_info.go:944` | 随 A4 一并接线 |
| A6 | **保持本地实现，不得被上游窄词表覆盖（第 44 轮例外项）** | 本地 `setting/reasoning/suffix.go` 的 `ParseDeepSeekV4ThinkingSuffix`（上游 relaykit 版 `relaykit/relayconvert/reasoning/suffix.go:269` 词表仅 `-none`/`-max`） | **移植 A 不得顺手 re-export**——上游 `setting/reasoning/suffix.go` 顶部把三类词表全量 `= kitreasoning.Xxx` 转出，照抄即**静默窄化**本地富词表（7 值归一化 + `deepseek-ai/` 前缀 + `deepseek-flash`），本地有 4 个生产调用点（`relay/channel/deepseek/adaptor.go:111/138/230`、`relay/channel/openai/adaptor.go:104`）且 `suffix_test.go` 钉住。此行是 A1–A5 表中**唯一「不移植」的项**，必须与 A1–A5 同批执行时显式跳过，并记入本地改动清单（宿主侧差异，不是 relaykit 分叉） |

**A3 的依赖面（第 44 轮实测）**：上游 `ParseLegacyModelSuffix` 直接调用 **relaykit 公开包** `relaykit/relayconvert/reasoning` 的 `ParseClaudeModelSuffix`/`ParseGeminiModelSuffix`/`ParseEffort`（`upstream/main:setting/reasoning/suffix.go` 顶部 `kitreasoning` 别名），并直接调用本文件的私有助手 `splitModelNamespace`（`upstream/main:setting/reasoning/suffix.go:90`，**本地无，须随 A3 一并补齐**）。**第 46 轮修正依赖方向**：原文写「依赖 `BaseModelName`/`CanonicalBillingModelNames`」是**写反了**——实测 `789c97019:setting/reasoning/suffix.go` 中 `ParseLegacyModelSuffix`（`:35-70`）对这两个名字零引用，实际是 `BaseModelName`（`:76`）与 `CanonicalBillingModelNames`（`:109`）**调用** `ParseLegacyModelSuffix`（`:84`、`:118`）。**`BaseModelName` 的处置**：它是上游 4 处消费者的依赖（`upstream/main:relay/helper/model_mapped.go:35`、`upstream/main:relay/helper/price.go:313`、`upstream/main:model/model_pricing_conversion.go:220`、`upstream/main:setting/ratio_setting/model_ratio.go:712`），基线 `relay/helper/model_mapped.go` 不含该调用——若「照抄上游终态」延伸到 `model_mapped.go`，`BaseModelName` 就是 A1–A6 未列却必需的符号，**阶段 4 须决定是否随 A 一并移植**（本地无对应需求，倾向不移植、只补 `ParseLegacyModelSuffix`）。**因此 A3 的本地位移不是「照抄 35–70 行」**——`ParseLegacyModelSuffix` 的 claude/gemini 分支依赖 relaykit 的 `ParseClaudeModelSuffix`/`ParseGeminiModelSuffix`，而 relaykit 在阶段 1 才拷入、阶段 3 才接线 `convmeta`；故 A3 的落地时机**只能在阶段 3 之后**（`setting/reasoning` 是叶子包、不得反向 import `relaykit` 之外的宿主 relay 包，但 import `relaykit/relayconvert/reasoning` 是允许的——它与本迁移「宿主 import relaykit」的方向一致）。**A3 的 OpenAI 分支**（默认 case）只依赖本地已有的 `ParseOpenAIReasoningEffortFromModelSuffix` + relaykit `ParseEffort`，与 A4 的 `ShouldPreserveEffortTail` 参数强绑定。

**本地 `ParseDeepSeekV4ThinkingSuffix` 与上游同名函数不是同一实现（第 44 轮实测，`A 也必须处理`）**：上游 relaykit 版（`relaykit/relayconvert/reasoning/suffix.go:269`）词表只有 `-none`/`-max` 两值；本地版（基线 `setting/reasoning/suffix.go`）保留富词表（`-minimal`/`-low`/`-medium`/`-high`/`-xhigh`/`-max`/`-ultra`）且归一化到 `low`/`high`/`max`，并额外接受 `deepseek-ai/` 前缀与 `deepseek-flash`。**移植 A 时不得让上游窄词表覆盖本地**——本地该函数有 4 个生产调用点（`relay/channel/deepseek/adaptor.go:111/138/230`、`relay/channel/openai/adaptor.go:104`）且 `suffix_test.go` 钉住富词表。**处置**：A 只补 claude/gemini/budget 缺失面，`ParseDeepSeekV4ThinkingSuffix` 保持本地实现（加性补齐的边界之一）；这与上游把三类词表整体 re-export 到 relaykit 的形态不同，须记入本地改动清单（宿主侧差异，不是 relaykit 分叉）。

**决定（原二选一，第 44 轮已定案为 A）**：

- **A（第 44 轮定案采用）**：移植上游 `relay/helper/reasoning_suffix.go` 与其依赖的 `relay/helper/model_modifier.go` 符号（`modelModifierDiagnostic`、`extractTemperature`、`extractTopP` 等，两文件共 510 行）为宿主文件，在 5 个入口接线。收益：与上游行为逐字节对齐，`-thinking`/`-nothinking`/effort 尾缀的统一语义、mapped-model 覆盖、pass-through 短路全部现成；代价：两个上游文件连同其测试一起移植，且它们依赖 `relaykit/relayconvert/reasoning` 的 `Parse*`（relaykit 公开包，宿主可 import）。**移植范围与依赖面已在上表（A1–A5）与两段实测中钉死。**
- **B（本地等价物，第 44 轮排除）**：用本地 `setting/reasoning/suffix.go` 的解析器写一个薄填充器，只覆盖本地实际使用的后缀词表（`TrimEffortSuffix`、`ParseOpenAIReasoningEffortFromModelSuffix`、`ParseDeepSeekV4ThinkingSuffix`），在入口调用并写 `info.ReasoningConversion` + 裁 `UpstreamModelName`。**排除理由**：本地词表是上游的严格子集（上表），B 等于在宿主重写 `-thinking`/budget/`@modifier`/家族白名单等上游已有语义——即「在宿主复制套件逻辑」，且这些语义本地**无从对表**。原文「词表与本地现状严格一致」只在 DeepSeek 一维成立。

**本轮（第 42 轮）实测修正——A 的移植面不止 510 行。** 上游 `relay/helper/reasoning_suffix.go` 还依赖上游**宿主**的另两个符号：`setting/reasoning.ParseLegacyModelSuffix`（`upstream/main:setting/reasoning/suffix.go:35`；该文件上游 233 行，本地仅 63 行且**无此法**——`git show dbfbc096f:setting/reasoning/suffix.go | grep -c ParseLegacyModelSuffix` = 0）与 `model_setting.ShouldPreserveEffortTail`（`upstream/main:setting/model_setting/global.go:167`，本地无；上游在 `upstream/main:relay/common/relay_info.go:944` 把它接进 `convmeta.Options.PreserveEffortTail`）。故 A 的实际范围是：`relay/helper/` 两文件 + `setting/reasoning` 的 `ParseLegacyModelSuffix` + `model_setting.ShouldPreserveEffortTail` + `PreserveEffortTail` 接线。**注意：不得用上游版本整体替换本地 `setting/reasoning/suffix.go`**——本地该文件被 9 个文件 import（含 `relay/channel/deepseek/adaptor.go`、`relay/channel/gemini/adaptor.go`），其中的 `TrimEffortSuffix` 等在阶段 4/6 之后仍有存活调用点，只能做加性补齐。

**判据（第 44 轮定案 A 后，在原文两条上追加两条行为变化断言）**：

- **带后缀的跨协议请求（oai→claude 至少一条）在阶段 4 快照或单测中渲染出正确的 thinking 与裁剪后的上游模型名**；
- **不带后缀的请求行为与现状逐字节一致**（pass-through 短路不得误伤）；
- **`gpt-*-max` 类名字现在被裁**（原文 OpenAI 词表缺 `-max`，A 定案后 relaykit 词表含 `-max`）：须补一条单测断言 `gpt-5.1-max` → base `gpt-5.1` + effort `max`，**否则上游能力面的这处增益会以「未验证即生效」的形式上线**；
- **非 GPT/o 系列名字不再被裁尾缀**（上游家族白名单生效）：须补一条单测断言某非 `gpt-*`/`o[1-9]*`/`claude-*`/`gemini-*` 的名字带 `-high` 尾缀时**保持原样不裁**——本地的旧行为是对任何名字都裁，A 定案后行为收窄，若不显式断言，「旧用例由红转绿」会掩盖这次语义变化。

### 12.20 上游宿主侧终态实测：facade / shim / wrapper 层（第 35 轮新增）

**问题**：12.3 与 §8.1 都在讨论「删不删包装层」，但从未对照**上游迁移完成后的宿主实际形态**。本轮逐文件实测 `upstream/main`，结论：**上游的终态是「保留旧名」——`service/convert.go` 没有消失，而是变成 27 行的 facade；调用点绝大多数零改动**。本迁移的字面计划（全删 13 个函数 + 改写全部调用点）与之相反，且此前无人把两者放在一起比较过。

**上游终态（全部实测于 `upstream/main`）**：

| 层 | 内容 | 证据 |
|---|---|---|
| `service/convert.go`（27 行） | **5 个同名 facade**：`NormalizeCacheCreationSplit`、`StreamResponseOpenAI2Claude`、`ResponseOpenAI2Claude`、`ResponseOpenAI2Gemini`、`StreamResponseOpenAI2Gemini`——**保留旧的 `*relaycommon.RelayInfo` 签名**，函数体是一行 `return relayconvert.Xxx(...)` | `upstream/main:service/convert.go` 全文 27 行；5 个 `func` 位于 `:9`、`:13`、`:17`、`:21`、`:25` |
| `service/request_converter.go` | **2 个同名 shim**：`ClaudeToOpenAIRequest`、`GeminiToOpenAIRequest`（经 `ConvertRequest` 转发）+ 3 个 `ConvertRequest*` 包装（记录 diagnostics）+ `init()` 注册 `SetMediaResolver`（**两个字段都接线**：`GetBase64Data` 与 `DecodeBase64FileData`） | `upstream/main:service/request_converter.go:50`、`:62`（两个 shim）、`:14`（`init`）、`:26`/`:34`/`:42`（3 个包装） |
| `service/response_converter.go` | 4 个响应包装：`ConvertResponse`、`ConvertStreamResponse`、`ConvertStreamResponseChunk`、`FinalizeStreamResponse`——都做 `info.RecordConversionDiagnostics` | `upstream/main:service/response_converter.go` 全文 |
| `relay/common/conversion_diagnostics.go` | `RelayInfo.RecordConversionDiagnostics`（可选接口，relaykit 经类型断言调用；**不属于 `convmeta.Meta`**） | `upstream/main:relay/common/conversion_diagnostics.go:25` |
| relaykit 公开兼容面 | `relaykit/relayconvert/response_compat.go` **导出约 40 个与宿主旧函数同名的包装**——含 5 个 facade 的目标（`NormalizeCacheCreationSplit:34`、`ResponseOpenAI2Claude:38`、`StreamResponseOpenAI2Claude:42`、`ResponseOpenAI2Gemini:128`、`StreamResponseOpenAI2Gemini:132`），以及 4.5.5 表里 4 个通道级转换器的同名目标（`StreamResponseClaude2OpenAI:50`、`ResponseClaude2OpenAI:100`、`ResponseGeminiChat2OpenAI:140`、`StreamResponseGeminiChat2OpenAI:144`） | `upstream/main:relaykit/relayconvert/response_compat.go` 逐行 |
| 通道级 facade | `upstream/main:relay/channel/claude/relay-claude.go:37`/`:41` 与 `upstream/main:relay/channel/gemini/relay-gemini.go:139`/`:143` 均为一行转发 `relayconvert.*` | 同上 |
| `*WithError` ×4 | **已删除**（`service/` 下 0 命中；全仓唯一含此字样的是一条无关测试名 `TestStreamStatus_SetEndReason_WithError`）；9 个生产调用点改为 `service.ConvertResponse` / `ConvertStreamResponse`（带 target 与 error） | `git grep "WithError" upstream/main -- 'service/*.go'` 空；`upstream/main:relay/channel/openai/helper.go:48` 等 |
| `IsDeepSeekModelName` / `HasDeepSeekModelName` | **已删除**（全仓 0 命中） | `git grep "func IsDeepSeekModelName\|func HasDeepSeekModelName" upstream/main -- '*.go'` 空 |
| 包装层调用规模 | `service.Convert*` 包装的非测试调用点 **39 处、16 个文件**（advancedcustom/ali/aws/claude/gemini/openai/vertex/relay 顶层；**第 51 轮校准：这是钉点 `789c97019` 口径**，其中 8 处本地无对应文件、本地适用 31 处/13 文件，见上表同行） | `git grep -nE "service\.(ConvertRequest\|ConvertRequestByID\|ConvertRequestVia\|ConvertResponse\|ConvertStreamResponse\|ConvertStreamResponseChunk\|FinalizeStreamResponse)\(" 789c97019 -- '*.go' \| grep -v _test \| wc -l`（**第 51 轮修正方法行**：原文写 `upstream/main` 且模式是省略号，按它复现得 41/17 而非 39/16——数字本身是钉点口径，方法行指错了 ref；上游 `main` 此后又新增 2 处） |
| 测试文件 | 本地的 `convert_claude_request_test.go`、`convert_claude_stream_test.go` **上游不存在**；上游的 `service/convert_test.go` 只剩 114 行，测的就是这 5 个 facade（`TestStreamResponseConverterFacades` 等） | `git ls-tree upstream/main service/`；`convert_test.go` 规模与用例名 |

**对本迁移的三点含义**：

1. **调用点规模相差一个量级**。按本文档的字面计划（全删），本地测试侧有 **4 个文件、82 处调用**会断（见 12.16.1 的逐文件清点；38 处在阶段 4、44 处在阶段 6；**第 46 轮注：该 82 处为 A/B 全删形态口径；C 形态下真实口径为 50 处必改——`service/` 17 处 + 通道级 33 处，其余 65 处零改动，见 12.20.1 末段与处置清单第 14 项**）；按上游形态，**在这 4 个 `service/` 测试文件内**是 **`*WithError` 的 17 处必须改指**，另 **`IsDeepSeekModelName` 的 2 处调用点零改动**（函数保留为宿主差异，见 12.20.1 测试表），其余 63 处经 facade/shim 零改动继续编译（**第 40 轮修正**：原文的「只有 `*WithError` 的 17 处 + `IsDeepSeekModelName` 的 2 处需要动」把「保留函数」与「改调用点」两种动作混算，且未标口径——通道级 4 个测试文件在 C 下另必改 33 处（`RequestOpenAI2ClaudeMessage` 16 + `CovertOpenAI2Gemini` 11 + `*WithError` 6，HEAD 口径；第 48 轮把 22 → 33），不在此 82 处的父集内，见处置清单第 16 项）。生产侧同理：`ClaudeToOpenAIRequest`/`GeminiToOpenAIRequest` 的 5 个生产调用点、4 个非 WithError 包装的 0 个生产调用点，按上游形态都是零改动。
2. **「单点可逆」在上游形态下有具体实现**：阶段 4 的切换 = 替换 `service/convert.go` 一个文件内的 5 个函数体（+ 新增 request/response 两个包装文件），回滚 = 恢复该文件；`relay-claude.go`/`relay-gemini.go` 的 4 个通道级 facade 同理。这正是 §8.1 想要的「一次集中的函数体替换」，比「薄封装」更贴近上游、也更好回滚。
3. **§3.1 声称的「降级可观测性 新增能力」在上游形态下才成立**：diagnostics 的产生在 relaykit 内部，落地要靠 `RelayInfo.RecordConversionDiagnostics`（宿主新文件）+ 包装层调用。按本文档字面计划（直接调 relayconvert 包级函数），diagnostics 会被**静默丢弃**（relaykit 经类型断言调用，宿主没实现该方法就什么都不发生）。若要兑现该能力，阶段 3/4 须加：`relay/common/conversion_diagnostics.go` 等价物 + 包装层（或至少给 `RelayInfo` 加该方法并让调用点经包装层）。

**未决项 33 的形态选项据此扩为三个**（A 薄封装 6 个转发函数 / B 直接改写 / **C 照抄上游终态**——5 个 facade + 2 个 shim + 包装层 + diagnostics），**第 44 轮定案为 C**：与上游零分叉、改动面最小、回滚点最集中。C 的唯一代价是 `service/convert.go` 文件保留（27 行）——与 12.3 原标题「不保留包装层」字面冲突，已在 12.3（第 35 轮修正）里处理。

### 12.20.1 形态 C 的连带动作清单（第 36 轮补齐，服务未决项 33）

12.20 只写了「C 的落点逐项核」，没把选 C 后必须先做（或明确不做）的动作变成可勾选清单——按本文档自己的标准（每个待定必须能指出属于哪个阶段的哪一步），补齐如下。**第 44 轮：未决项 33 已定案为 C，本表生效**——阶段 4 处置清单第 16 项援引本文，以下动作即为阶段 4 的实际动作面（A/B 形态下的「本表不生效」条款已不适用）。

**生产侧（按形态 C；行号一律为基线 `dbfbc096f` 口径；**第 44 轮注：本表含若干 HEAD 口径行号（如 `relay-gemini.go:1784` 的 `:1848`），它们只对某个瞬时 commit 成立、随时漂移，且无门禁保护——C 定案后这些引用会被阶段 4 实际使用，漂移风险比之前更实；开工时按当时 HEAD 重数，行号只作定位参考、以函数名为准**——HEAD 口径无门禁保护、会随任何提交漂移，本文档只在括注里给 HEAD 值。通道级 4 个 `*WithError` 的 HEAD 行号为 `relay-claude.go:1252`/`:1366`、`relay-gemini.go:1669`/`:1832`（**实测于 `a0491b310`**）、`aws/converse.go:696`（依次为 `StreamResponseClaude2OpenAIWithError` / `ResponseClaude2OpenAIWithError` / `streamResponseGeminiChat2OpenAIWithError` / `responseGeminiChat2OpenAIWithError` / `claude.ResponseClaude2OpenAIWithError`），与基线 `1167`/`:1273`/`:1605`/`:1768`/`:696` 不同；**这些值只对 `a0491b310` 成立**——同一轮内 gemini 两行号就已从 `1666`/`1829` 漂到 `1669`/`:1832`，故一律以函数名为准、行号只作定位参考**）**：

| 调用点族 | 数量 | 形态 C 下的处置 | 依据 |
|---|---|---|---|
| `*WithError` 生产调用点（`relay/channel/openai/relay-openai.go:314,324`、`relay/channel/openai/helper.go:46,63,246,267`、`relay/channel/openai/chat_via_responses.go:280,286`、`relay/channel/gemini/relay-gemini.go:1784`（**实测于 `a0491b310` 为 `:1848`**；`:1819` 是第 36 轮的值、`:1845` 是第 38 轮的值，**第 38c 轮实测两者都已漂**——该文件在第 36 轮后被另一会话的提交 `222876349` 改动，此后又漂了 3 行。**故本表一律以函数名 `service.ResponseOpenAI2ClaudeWithError` 为准，行号只作定位参考**。**此类「HEAD 口径」标注无门禁保护，只锚定基线口径的引用才是可复核的**）） | 9（4 个文件） | **改指** `service.ConvertResponse`/`ConvertStreamResponse` 包装（上游形态；`openai/helper.go` 同文件 4 处，故按文件回滚仍是 4 个文件）。**另有 4 处包装体内部调用不计入本行**（**第 40 轮补注**）：`service/convert.go` 的 4 个非 WithError 包装各自一行转发同名 `*WithError`（基线 `:722`/`:1036`/`:1827`/`:1998`），随 C1 的 facade 改写自然消失**第 47 轮补——这 9 处按方向分两组，严格性助手不共用**：`relay-openai.go:314`、`openai/helper.go:46,246`、`chat_via_responses.go:280`、`relay-gemini.go:1784` 是 **Claude 方向**（调 `*ClaudeWithError`），由处置清单第 9 项的 `NormalizeOpenAIFinishReasonsForClaude` 前置校验；`relay-openai.go:324`、`openai/helper.go:63,267`、`chat_via_responses.go:286` 是 **Gemini 方向**（调 `*GeminiWithError`），由第 47 轮新增的 `NormalizeOpenAIFinishReasonsForGemini` 前置校验（9.1 第 8 条、12.11.3）。**两组各 5 与 4 处、共 9 处**——本行此前只按「文件」分组，未按方向分组，故 Gemini 方向的 4 处一直挂在「同一族、同一处置」下，掩盖了它们依赖的是另一个本地严格函数 `openAIFinishReasonToGemini`（基线 `service/convert.go:1915`，C1 一并删除）这一事实 | 12.3、12.20 |
| **通道级 4 个 `*WithError` 的生产调用点**（第 37 轮补入，此前 12.20.1 完全未列；**第 40 轮修正计数**：原写「5（3 个文件）」，漏了基线 `relay-claude.go:715`/`:832`、`relay-gemini.go:1277`/`:1382` 四处——实测 `git show dbfbc096f:relay/channel/claude/relay-claude.go \| grep -n 'WithError('` 可见 `:715`/`:832` 位于 4 个非 WithError 包装的**函数体内**（如 `StreamResponseClaude2OpenAI` 一行转发 `…WithError`），这四处是**包装体内部调用**，形态 C 下随包装层改写自然处理、不计独立生产调用点；`relay-gemini.go:1277`/`:1382` 同型。故「必改生产调用点」仍为下 9+5=14 处中的通道级 5 处，本行数量不变、但须说明包装体内 4 处的归属）：`relay-claude.go:1167`（`StreamResponseClaude2OpenAIWithError`）、`relay-claude.go:1273`（`ResponseClaude2OpenAIWithError`）、`relay-gemini.go:1605`（`streamResponseGeminiChat2OpenAIWithError`）、`relay-gemini.go:1768`（`responseGeminiChat2OpenAIWithError`）、`aws/converse.go:696`（`claude.ResponseClaude2OpenAIWithError`，跨包） | 5（3 个文件） | **逐处按上游形态改**（逐处实测）：**`:1167`** 上游不是调无 error facade，而是 `claudeToChatStreamState(info)` + `state.ConvertChunk(&claudeResponse)`（`upstream/main:relay/channel/claude/relay-claude.go:129-134`，`ConvertChunk` **有** error 返回），错误路径保留；**`:1273`** → `ResponseClaude2OpenAI(&claudeResponse)` 无 error、删 `if conversionErr` 分支（上游 `upstream/main:relay/channel/claude/relay-claude.go:349`）；**`:1605`** → `streamResponseGeminiChat2OpenAI(geminiResponse)` 删 `convertErr`（上游 `upstream/main:relay/channel/gemini/relay-gemini.go:276`）；**`:1768`** → `responseGeminiChat2OpenAI(c, ...)` 删 `conversionErr`（上游 `upstream/main:relay/channel/gemini/relay-gemini.go:426`）；**`converse.go:696`** 上游无对应文件，按 `:1273` 同型处理——调无 error facade、删 err 分支。**本地报错而 relaykit 侧无对应报错**——`StreamResponseClaude2OpenAI`/`ResponseClaude2OpenAI` 对未知 type 一律 `return nil` 静默跳过（见 9.2.1 第 1、2、8 条），其余条目则静默忽略（第 3、7 条）、无校验产出空 tool_call（第 4 条）、panic（第 5 条）或静默丢弃（第 6 条），故删 err 分支不是机械改写，**须逐条决定 9.2.1 的 9 条是否放弃**（**第 39 轮修正**：原文写「四类本地错误…（见 9.2.1 第 1、2、4 条）」，四类枚举与所引条目均不准确——第 4 条不是 `return nil` 路径，且本地报错的实为 9 条中的 8 条，不能以「四类」概括） | 4.5.5、9.2.1、12.20 |
| `ClaudeToOpenAIRequest`/`GeminiToOpenAIRequest` 生产调用（`relay/channel/ali/adaptor.go:88`、`relay/channel/aws/converse.go:113`、`relay/channel/openai/adaptor.go:47,65`、`relay/claude_handler.go:134`） | 5 | **零改动**（同名 shim 保留） | 12.20 |
| **`RequestOpenAI2ClaudeMessage` 的生产调用点**（第 37 轮补入）：`relay/channel/claude/adaptor.go:100`、`relay/channel/aws/adaptor.go:173`、`relay/channel/vertex/adaptor.go:302` | 3（3 个文件） | **必改，且不能靠同名 facade**——上游**已删除**该函数（`git grep -c RequestOpenAI2ClaudeMessage upstream/main -- 'relay/'` = 0），上游三个 adaptor 一律改调 `service.ConvertRequest(c, info, types.RelayFormatClaude, request)` 再断言 `result.Value.(*dto.ClaudeRequest)`（`upstream/main:relay/channel/claude/adaptor.go:122`、`upstream/main:relay/channel/aws/adaptor.go:130`、`upstream/main:relay/channel/vertex/adaptor.go:281`）。**故 4.5.5 表首行的「relaykit 对应 = `relayconvert.OpenAIChatRequestToClaudeMessages`」（`request_compat.go:20` 公开再导出）只是底层转换函数，**上游 adaptor 不直调它**（直调绕过 `RecordConversionDiagnostics`），实际落点是 `service.ConvertRequest`** | 4.5.5、12.20 |
| 4 个非 WithError 旧包装的生产调用 | 0 | 不涉及（本就只有测试引用） | 12.3 |
| **`CovertOpenAI2Gemini` 的生产调用点**（第 48 轮补入，此前 12.20.1 完全未列——它是 4.5.5 口径外的第 10 个被替换转换器，见 4.5.5 表头注）：`relay/channel/gemini/adaptor.go:186`、`relay/channel/vertex/adaptor.go:311` | 2（2 个文件） | **必改，且不能靠同名 facade**——上游**已删除**该函数（`git grep -c CovertOpenAI2Gemini upstream/main -- '*.go'` = 0），两个 adaptor 一律改调 `service.ConvertRequest(c, info, types.RelayFormatGemini, request)` 再断言 `result.Value.(*dto.GeminiChatRequest)`（`upstream/main:relay/channel/gemini/adaptor.go:174`、`upstream/main:relay/channel/vertex/adaptor.go:294`）。**与 `RequestOpenAI2ClaudeMessage` 行同型**：4.5.5 表第 6 行的「relaykit 对应 = `relayconvert.OpenAIChatRequestToGeminiGenerateContent`」只是底层转换函数（**第 50 轮订正**：该名字在 `relaykit/relayconvert/request_compat.go:28` **有公开再导出、宿主可 import**，同名的 `internal/oai_chat/to_gemini_chat_req.go:19` 才是 internal；原写「internal，宿主不可 import」不准确），但**上游 adaptor 不直调它**——直调会绕过 `RecordConversionDiagnostics`，故实际落点是 `service.ConvertRequest`（经包装层记录诊断）。**本行是回滚清单「生产改写 16 处」升为 18 处的原因**（见 12.20.1 末段） | 4.5.5、9.2.5、12.20 |

**测试侧（第 35 轮清点 82 处；C 形态下 50 处必改，见下）按形态 C 的三分**：

| 分类 | 数量 | 形态 C 下的处置 |
|---|---|---|
| `*WithError` 调用（`convert_claude_request_test` 1、`convert_claude_stream_test` 1、`convert_test` 9、`convert_golden_test` 6） | 17 | **必改**——改指 relaykit 无 error 入口或包装层；其中 golden 的 6 处已由处置清单 12 认领，**另 3 个文件的 11 处此前无认领，C 定案后随处置清单增补** |
| 4 个非 WithError 旧包装的调用（`convert_claude_request_test` 6、`convert_claude_stream_test` 13、`convert_test` 2） | 21 | **零改动**（facade 保名保签名） |
| 请求方向调用（`ClaudeToOpenAIRequest` 31 = 28+2+1、`GeminiToOpenAIRequest` 11 = 10+1，含 golden 的 2 处） | 42 | **零改动**（同名 shim） |
| `IsDeepSeekModelName` 调用（`convert_test` 2） | 2 | **保留**——与上游终态的**必要差异**：上游已删这两个函数，但本地要用 `hasDeepSeekModelName` 作 12.4 的门控钩子值（12.4.4），函数必须活到阶段 6 之后。**须记入本地改动清单**（宿主侧差异，不是 relaykit 分叉） |

合计（**仅 `service/` 4 文件这一面**）：C 形态下必改 **17 处**（全在 `*WithError`），零改动 65 处，其中 2 处是与上游终态的有意差异。**通道级 4 文件是另一面**（见下段「通道级测试面」，第 48 轮实测基线 33 / HEAD 41、必改 33），两面的必改数相加才是本节开头的 **50 处**（`service/` 17 + 通道级 33）——**上一句的「52」与这里的「17」不可直接比较，口径不同**。

**通道级测试面（第 37 轮补入，此前整表未列）**：`relay/channel/` 下的测试文件直接调用 4.5.5 表列的通道级转换器（第 48 轮口径 10 个），与上表**是两套互不重叠的测试面**（上表全在 `service/`）。基线口径共 **33 处、2 个文件**（**第 48 轮修正：原文写「22 处」漏了 `protocol_conversion_test.go` 对 `CovertOpenAI2Gemini` 的 11 处**）：`relay/channel/claude/relay_claude_test.go` **18 处**（`RequestOpenAI2ClaudeMessage` 12、`StreamResponseClaude2OpenAI` 2、`StreamResponseClaude2OpenAIWithError` 2、`ResponseClaude2OpenAI` 2）、`relay/channel/gemini/protocol_conversion_test.go` **15 处**（`responseGeminiChat2OpenAI` 1、`streamResponseGeminiChat2OpenAI` 3、**`CovertOpenAI2Gemini` 11**——第 48 轮补入，此前整表漏列）。**HEAD 口径已增至 41 处、4 个文件**（新增 `relay/channel/claude/claude_response_format_test.go` 的 4 处 `RequestOpenAI2ClaudeMessage`、`relay/channel/gemini/tool_call_id_test.go` 的 4 处 `streamResponseGeminiChat2OpenAIWithError`；后两个文件在基线 `dbfbc096f` 上**不存在**，是之后新增的，故阶段 4 开工时须按当时的 HEAD 重数）。**形态 C 下的处置**：`RequestOpenAI2ClaudeMessage` 的 16 处**必改**（上游已删该函数，无 facade 可依，见上表）；**新增第 48 轮：`CovertOpenAI2Gemini` 的 11 处**（基线口径，`dbfbc096f` 已存在）**必改**——上游已删该函数（`git grep -c CovertOpenAI2Gemini upstream/main` = 0），其中 6 处 `Rejects*`（`relay/channel/gemini/protocol_conversion_test.go` 内，见 9.2.5 的行号表，钉 9.2.5 第 7/9/6/2/8 条）钉的是本地严格性、改写即放弃；另 5 处是正向渲染用例，改指 relaykit 后须与本地输出逐字比对；4 个非 WithError facade 的 8 处**零改动**；`*WithError` 的 6 处**必改**——其中 `relay/channel/claude/relay_claude_test.go:689`（`RejectsUnknownEvent`）与 `relay/channel/claude/relay_claude_test.go:697`（`RejectsIncompleteToolBlock`）**钉的是 9.2.1 第 1、4 条那两类被 relaykit 丢弃的错误**，改写即等于放弃本地严格性，须随 9.2.1 的裁定一并处理；`relay/channel/gemini/tool_call_id_test.go` 的 4 处**与 9.2.1 无关**（第 38 轮修正——第 37 轮误写成「钉 9.2.1 第 5 条的 `PartialJson` 守卫」）：它们钉的是 **Gemini 合成 tool call ID 的跨 chunk 唯一性**，对应 **9.2.3 第 8 条**。**上游 `relay/channel/claude/relay_claude_test.go` 已不含这些用例**（402 行，`RequestOpenAI2ClaudeMessage` 引用数 0）——**第 38 轮修正**：原文接着说「上游把这 18 处覆盖整体丢弃」，**该断言过强**。上游改调 `relayconvert.OpenAIChatRequestToClaudeMessages` 后，**新写了 2 个用例**（`TestOpenAIChatRequestToClaudeMessages_ClaudeOpus48HighUsesAdaptiveThinking`、`…ThinkingUsesAdaptiveHighEffort`，`relay_claude_test.go:350`/`:377`），覆盖**思考等级适配**一条；而本地那 12 个用例覆盖的 12 个行为（未知 file content、显式 MIME、assistant reasoning 与 tool_calls 共存、空 tool_calls、reasoning 不与相邻文本合并、PDF、text 文件转文本、原始 tool schema、developer→system、非法 stop_sequence、非法 tool arguments、未知 tool_choice）**在上游侧一个都没有对应**。**正确表述是「上游只承接了 1 条（思考等级），其余 11 条无对应」**，不是「整体丢弃」——两者的动作不同：前者要求阶段 4 逐条决定这 11 条是移植还是放弃，后者读起来像「不必处理」。**第 48 轮补**：上游对 `CovertOpenAI2Gemini` 的替代者（`OpenAIChatRequestToGeminiGenerateContent`）在 `upstream/main` 全仓仅 1 个测试命中（`relaykit/relayconvert/safety_settings_test.go`，测 safety settings 透传），**与本地这 11 处覆盖的 tools/role/part type/schema 无一对应**——故本表 `protocol_conversion_test.go` 那 11 处**上游实质一条都没承接**（与 `relay_claude_test.go` 那 18 处「承接了 1 条（思考等级）」不同）。

**阶段新增文件（选 C 时，阶段 4 一次落地）**：

| # | 动作 | 上游照抄源 | 验证条件 |
|---|---|---|---|
| C1 | `service/convert.go` 保留并把 5 个函数体改写为 `return relayconvert.Xxx(...)`（**旧签名不变**）；不按处置清单第 2 项删除该文件（12.3 第 35 轮已修正删除范围）。**第 46 轮修正「27 行」的互斥**：上游 `service/convert.go` 的 27 行只含 5 个 facade，**不含** `IsDeepSeekModelName`/`HasDeepSeekModelName`（12.4 门控钩子，须保留为宿主差异）与 `stopReasonOpenAI2ClaudeStrict`（处置清单第 9 项的迁移源，**必须在 C1 改写前迁出**，见执行次序表批 1）。故 C1 的实际产物是「5 个 facade + 本地保留的 `IsDeepSeekModelName`/`HasDeepSeekModelName`」——**不是纯 27 行**，全文对 C1 的「27 行」断言（§2、§8.1、12.20、12.20.1）须按此口径理解。**第 50 轮补——C1 还须删除两个旧声明**：C1 改写函数体的同一动作里，**必须删掉基线 `service/convert.go:18` 的 `ClaudeToOpenAIRequest` 与 `:1248` 的 `GeminiToOpenAIRequest` 两个实现声明**（它们不在上游 27 行的 5 个 facade 里，上游把这两个同名 shim 放在 `service/request_converter.go`，即 C2）——不删则与 C2 新增的同包同名 shim `redeclared` 编译失败（见 C2 行、批 2 依据、处置清单第 18 项）。**这也是 C1 动作面此前漏写的一项**：C1 行原文只列「5 个 facade 改写 + 保留两个 deepseek 函数 + `stopReasonOpenAI2ClaudeStrict` 迁出」，未列这 2 个请求方向声明的删除。**第 50 轮补——C1 facade 的编译前提（阶段 3 的 Meta 实现）**：C1 的 5 个 facade 体是 `return relayconvert.Xxx(..., info)`，而 relaykit 侧这 5 个函数的形参是 `info convmeta.Meta`（`relaykit/relayconvert/response_compat.go:34/38/42/128/132`），本地 facade 的形参是 `info *relaycommon.RelayInfo`——**能编译只因 `*RelayInfo` 实现了 `convmeta.Meta`**（阶段 3 行第 3 项新增的那 13 个方法 + 编译期断言 `var _ convmeta.Meta = (*RelayInfo)(nil)`）。故 **C1 的编译前提是阶段 3 完成**；这是阶段表「阶段 3 先于阶段 4」的隐含依赖，C1 行此前未点明，实现者若跳阶段 3 直接做 C1 会在 `cannot use info (variable of type *relaycommon.RelayInfo) as convmeta.Meta` 处失败 | `upstream/main:service/convert.go`（27 行，**仅指 5 个 facade 部分**） | 5 个 facade 的既有测试调用（21 处中的 `convert_test`/`convert_claude_*` 部分）继续编译且绿；**`IsDeepSeekModelName`/`HasDeepSeekModelName` 保留且 12.4 门控可用** |
| C2 | 新增 `service/request_converter.go`（2 个同名 shim + 3 个 `ConvertRequest*` 包装 + `init()` 注册 `SetMediaResolver` **两个字段**） | `upstream/main:service/request_converter.go` | 签名逐行一致；`DecodeBase64FileData` 必须一并注册（12.20 第 3 点）。**第 49 轮补编译前置**：基线 `service/convert.go:18`/`:1248` 已声明 `ClaudeToOpenAIRequest`/`GeminiToOpenAIRequest`（`package service`），**C2 必须与 C1 同批且 C1 须同时删掉这两个旧声明**——否则同包同名 `redeclared` 编译失败；执行次序表批 2 已把 C2 移入 |
| C3 | 新增 `service/response_converter.go`（4 个包装，均调 `info.RecordConversionDiagnostics`） | `upstream/main:service/response_converter.go` | 同上 |
| C4 | 新增 `relay/common/conversion_diagnostics.go`，`RelayInfo` 实现 `RecordConversionDiagnostics`（**可选接口、不在 `convmeta.Meta`**，relaykit 经类型断言调用）。**第 46 轮补：上游该文件定义 3 个方法**（`RecordConversionDiagnostics`/`ConversionDiagnostics`/`ConversionDiagnosticsTruncated`），且 `RecordConversionDiagnostics` 读 `info` 上的 3 个未导出字段（`conversionDiagnostics []types.ConversionDiagnostic`、`conversionDiagnosticKeys map[conversionDiagnosticKey]struct{}`、`conversionDiagnosticsTruncated bool`，**第 49 轮双锚点**：`789c97019:relay/common/relay_info.go:204-206` 与 `upstream/main:relay/common/relay_info.go:221-223`——两 ref 该文件有 44 增/15 删的漂移，钉上游 `main` 会指错行，钉点用 204-206、内容用 `upstream/main`）——**本地 `RelayInfo` 三个字段全缺**（`git grep conversionDiagnostic dbfbc096f -- relay/common/relay_info.go` = 0，第 49 轮实测），**须一并补齐**，否则 C4 照抄即编译失败。**第 49 轮归类订正**：补 3 字段改的是**既有**文件 `relay/common/relay_info.go`（基线/HEAD 均存在），C4 的动作 = 新增 `conversion_diagnostics.go` + 改既有 `relay_info.go` 两个落点，回滚时后者归「revert 既有文件」、不是删新文件 | `upstream/main:relay/common/conversion_diagnostics.go` | 断言生效的验证：切流式/非流式后诊断出现在 `info` 上（或按上游同名测试）；**3 个字段已声明** |
| C5 | 流式路径构造 `relayconvert.NewResponseStreamState`（上游 6 处构造点，全部实测、全部 `upstream/main:` 口径：`upstream/main:relay/channel/claude/relay-claude.go:195`、`upstream/main:relay/channel/claude/relay_responses.go:22`、`upstream/main:relay/channel/gemini/relay_responses.go:84`、`upstream/main:relay/channel/openai/helper.go:89`、`upstream/main:relay/channel/openai/chat_via_responses.go:181`、`upstream/main:relay/channel/openai/responses_via_chat.go:73`）。**第 49 轮本地适用性实测：6 处里 2 处本地无对应文件、4 处是改既有函数**——① `relay/channel/claude/relay_responses.go`、`relay/channel/gemini/relay_responses.go` **基线与 HEAD 均不存在**（`git cat-file -e` 均失败）；它们承载的是 `RelayFormatOpenAIResponses` 请求打到 claude/gemini 渠道的新能力，而本地基线该路径**显式不支持**：基线 `relay/channel/claude/adaptor.go:113`、`relay/channel/gemini/adaptor.go:243` 的 `ConvertOpenAIResponsesRequest` 直接返回 `ErrProtocolConversionUnsupported`（**第 49 轮门禁**：该 gemini adaptor 引用短路径歧义——仓里还有 `relay/channel/task/gemini/adaptor.go`，故此处写全路径）（上游已删该错误符号）。照抄这 2 个文件会**连带启用本地基线不支持的 responses→claude/gemini 流式路径**，超出 C 的动作面（连带 adaptor 的 `DoResponse` 分支改动）——**须先裁定这是刻意并入还是明确排除**，排除则 C5 记 4 处、批 2 依据同步。**第 50 轮补——同一分歧还牵动 3 处此前未列的既有文件改动与 1 个符号删除**：①`relay/channel/claude/adaptor.go:112` 与 `relay/channel/gemini/adaptor.go:242` 的 `ConvertOpenAIResponsesRequest` **本地 = 直接返回 `ErrProtocolConversionUnsupported`，上游 = 真转换**（`service.ConvertRequest(c, info, types.RelayFormatClaude/ Gemini, &request)` + 断言 `result.Value.(*dto.ClaudeRequest/*dto.GeminiChatRequest)`，`789c97019:relay/channel/claude/adaptor.go:138`、`789c97019:relay/channel/gemini/adaptor.go:229`）——这 2 个函数是**既有文件的方法体改写**，不在 12.20.1 生产改写 18 处清单内（那张表只列 `ConvertOpenAIRequest` 下的 `RequestOpenAI2ClaudeMessage` 等）；②`service/error.go:25` 的 `ErrProtocolConversionUnsupported` **上游已删**（`git grep 789c97019 -- service/error.go` = 0），本地 3 个使用点（两个 adaptor 方法体 + `relay/responses_handler.go:1692` 的 `errors.Is` 分支 + 1 处测试）**须随上游一并处理**——该符号既不在 12.2 的 4 个本地独有 `ErrorCode`（那是 `types/error.go` 的常量）内，也未被任何处置项认领；③`relay/responses_handler.go:1692` 的 `errors.Is(err, service.ErrProtocolConversionUnsupported)` 分支**无上游对应**——本地该文件 1748 行、上游同路径文件仅 111 行（`789c97019:relay/responses_handler.go` 只含 `ResponsesHelper` 一个函数），两者**同路径但内容不同**，本地是大幅扩展后的版本（第 50 轮实测），故上游删掉 `ErrProtocolConversionUnsupported` 后本地这个 `errors.Is` 分支须由本迁移自行决定去留（随 C5 的纳入/排除裁定），不能指望「照抄上游」自动解决。**结论**：C5 的「纳入/排除」裁定**必须同时覆盖这 2 个 adaptor 方法体 + 1 个错误符号 + 1 个 handler 分支**，否则「排除 responses 路径」时符号仍被 3 处引用（删不掉）、「纳入」时 3 处未改（编译失败）——这正是第 49 轮 C5 缺口的下游连锁，此前只记了 2 个文件、漏了这 3 处既有改动 + 1 符号删除；② 另 4 处对应本地**既有**文件（`relay-claude.go`/`helper.go`/`chat_via_responses.go`/`responses_via_chat.go` 基线均存在）的函数体改写，归入回滚「既有文件 revert」——**本地既有实现与上游构造点的映射**：`relay-claude.go` 本地 `HandleStreamResponseData`（`dbfbc096f:relay/channel/claude/relay-claude.go:1134`）只有 Claude/OpenAI 两分支（上游 `:152` 起多出 `RelayFormatGemini` 分支），本地 `HandleStreamFinalResponse` 无 Gemini 分支；`responses_via_chat.go` 本地 `OaiChatToResponsesStreamHandler`（`dbfbc096f:relay/channel/openai/responses_via_chat.go:138`）是三行兼容层包装，无构造点 | 同上游各处 | 流式 golden 仍绿；**第 49 轮补：6 处构造点的本地落点逐处列出**（含 2 个基线不存在文件的「纳入/排除」裁定），否则「6 处」只是上游坐标 |
| C6 | 通道级 **4 个非 WithError 函数**改保为一行 facade（`relay-claude.go` 的 `StreamResponseClaude2OpenAI`/`ResponseClaude2OpenAI`、`relay-gemini.go` 的 `responseGeminiChat2OpenAI`/`streamResponseGeminiChat2OpenAI`），`*WithError` 变体删除；**`RequestOpenAI2ClaudeMessage` 不能保 facade——上游已删该函数**，3 个 adaptor 调用点（`relay/channel/claude/adaptor.go:100`、`relay/channel/aws/adaptor.go:173`、`relay/channel/vertex/adaptor.go:302`）改调 `service.ConvertRequest`；**第 48 轮补：`CovertOpenAI2Gemini` 同样不能保 facade——上游已删该名字**，另 2 个 adaptor 调用点（`relay/channel/gemini/adaptor.go:186`、`relay/channel/vertex/adaptor.go:311`）改调 `service.ConvertRequest(c, info, types.RelayFormatGemini, request)`（见生产侧表） | `upstream/main:relay/channel/{claude/relay-claude.go,gemini/relay-gemini.go,claude/adaptor.go,aws/adaptor.go,vertex/adaptor.go}` | 4.5.5 表 **6 行 10 个函数**（第 48 轮补入 `CovertOpenAI2Gemini`）的「阶段 6 删除」判据随之改写：4 个保 facade、6 个真删（`RequestOpenAI2ClaudeMessage` + `CovertOpenAI2Gemini` + 4 个 `*WithError`）——**选 C 时须改阶段 6 判据，否则判据与形态矛盾**。**第 46 轮补测试侧前置**：C6 要删的 6 处通道级测试调用中，`relay_claude_test.go:689`（`RejectsUnknownEvent`）、`:697`（`RejectsIncompleteToolBlock`）钉的是 **9.2.1 第 1、4 条**被 relaykit 丢弃的错误，`tool_call_id_test.go` 4 处钉的是 **9.2.3 第 8 条**——**这些测试的处置依赖批 3 第 6 项的对应行裁定**（执行次序表批 3），故 C6 在裁定前**须保留 `*WithError` 函数体**，或把对应行的裁定提前到批 1 |
| C7 | **（第 46 轮补）诊断落盘消费者**：`service/log_info_generate.go` 的 `AppendRelayLogAdminInfo` 读 `relayInfo.ConversionDiagnostics()`/`ConversionDiagnosticsTruncated()` 并写 `other.SetAdmin("conversion_diagnostics", …)`——**基线对该文件零命中**（`git grep ConversionDiagnostic dbfbc096f -- 'service/*.go' relay/*.go` = 0），即基线**没有任何诊断消费者**。**第 49 轮归类订正**：`service/log_info_generate.go` 是**既有**文件（基线存在、798 行，第 49 轮实测 `git cat-file -e dbfbc096f:service/log_info_generate.go` 通过），C7 是在其中插入读取逻辑，**不是新增文件**——原表述把它列进「5 个新增文件」不成立，回滚应 revert 该文件、不是删文件。§3.1 声称的「降级可观测性 新增能力」**依赖此项**：C1–C6 落地后诊断只被写入 `info`、永不被读取，§3.1 不成立 | `upstream/main:service/log_info_generate.go`（`:79-82`） | 诊断出现在日志的 `other` 字段（**C4 的验证条件「诊断出现在 `info` 上」只验字段、不验落盘，恰好绕过本缺口**） |
| C8 | **（第 46 轮补）请求转换失败错误构造**：新增 `relay/convert_request_error.go` 的 `newConvertRequestFailedError`——上游 6 个 `ApplyReasoningModelSuffix` 调用点**全部** `return newConvertRequestFailedError(c, info, err)`，该文件是上游专有（基线 `relay/` 下无），依赖 `types.ConversionLossError` 与 `kitreasoning.IsClientError`。**填充器接线（第 15 项）与 C6 改写的 handler 路径直接需要它**，全文原无一处提及 | `upstream/main:relay/convert_request_error.go` | 6 个 `ApplyReasoningModelSuffix` 调用点编译通过且错误类型符合预期 |

**C 的回滚点**：阶段 4 的切换集中在 **5 个既有文件**（`service/convert.go`[C1 函数体] + `service/log_info_generate.go`[C7 诊断落盘] + `relay/common/relay_info.go`[C4 三个字段] + `relay/channel/claude/relay-claude.go` 与 `relay/channel/gemini/relay-gemini.go`[C6 各 2 行 facade]，第 49 轮实测五个文件基线均存在）与 **4 个新增文件**（`request_converter.go`、`response_converter.go`、`conversion_diagnostics.go`、`convert_request_error.go`——**第 46 轮补 C8；第 49 轮归类订正**：C4 的 `conversion_diagnostics.go` 是新文件，但 C4 同时改既有 `relay_info.go`；原列 5 个把 C7 的既有文件 `log_info_generate.go` 误算成新文件；另加宿主侧共享助手文件与填充器 2 文件，见下「第 46 轮补」）——回滚 = 删 4 新文件 + revert 5 个既有文件 + revert **生产改写 18 处**（`service.*WithError` 改指 8 处 / 4 个文件，加通道级 `*WithError` 5 处 / 3 个文件，加 `RequestOpenAI2ClaudeMessage` 生产改指 3 处 / 3 个文件，加 `CovertOpenAI2Gemini` 生产改指 2 处 / 2 个文件；**第 40 轮修正**：原文只写「`*WithError` 的 9 处生产改写（4 个文件）」，漏了通道级行的 5 处与请求方向行的 3 处，与本节上方生产表 8+5+3+0+2 的逐行合计不闭合——按生产表自己的计数为 16 处（8+5+3+0）；**第 48 轮补 `CovertOpenAI2Gemini` 的 2 处，故 16 → 18**（文件数 8+5+3+0+2 逐行合计 12、去重后 10——`vertex/adaptor.go` 已在 `RequestOpenAI2ClaudeMessage` 行的 3 个文件内，`relay-gemini.go` 跨两族））。**第 46 轮补：回滚清单的完整动作面**（原清单只列生产侧，**按现清单执行回滚会编译失败**）：

- **生产侧**：删 4 个新文件（C2/C3/C4/C8——**第 49 轮订正**：C7 非新文件，见下）+ revert 5 个既有文件（`service/convert.go`[C1] + `service/log_info_generate.go`[C7] + `relay/common/relay_info.go`[C4 三字段] + `relay/channel/claude/relay-claude.go` 与 `relay/channel/gemini/relay-gemini.go`[C6]）+ revert 生产改写 18 处（10 个文件，第 48 轮 16→18）。
- **测试侧**：revert 测试侧 50 处（`service/` 4 文件 17 处 + 通道级 4 文件 33 处；第 48 轮 39 → 50）——**原清单完全未列**。不 revert 则编译失败：`*WithError` 函数已恢复，但测试里改指了 `ConvertResponse`/`ConvertStreamResponse`，而包装层随 3 新文件删除后这些符号不存在。
- **宿主新增文件**：删除共享助手文件（`stopReasonOpenAI2ClaudeStrict` 迁入点，处置清单第 9 项；**第 47 轮补：同一文件还承载 `openAIFinishReasonToGemini` 与第二个归一化助手 `NormalizeOpenAIFinishReasonsForGemini`，9.1 第 8 条**——删该文件即同时回滚两个方向的严格语义）+ 填充器 2 个文件（`relay/helper/reasoning_suffix.go`、`model_modifier.go`，处置清单第 15 项 A1/A2）——**原清单完全未列**。不删除则残留死代码或引用已不存在的 relaykit 符号。
- **`converse.go` 的归属**：`relay/channel/aws/converse.go:696` 的 `claude.ResponseClaude2OpenAIWithError` 是 16 处生产改写之一，revert 它只需改回一行，**不需 revert 整个文件**（该文件不在「5 个既有文件」内，属生产改写 18 处之一）。

与 §8.1「单点可逆」的关系：facade 层本身单点（一个文件的函数体），生产改写项（12.3 的 `*WithError` 固定删除 + 12.20.1 生产表的改指点）是随形态落地的固定清单，不随形态在 A/B/C 间消失、只随形态变化落点文件数。**第 46 轮修正「单点」的表述**：C 形态的「单点可逆」是**两层**的——协议映射层单点（facade 函数体替换），调用点层多点（18 处生产改写 + 50 处测试改写；第 48 轮口径）但它们是**固定清单**、随形态一次落地，不是「散落在 6 条响应路径里的多点改写」。§8.1 反对的是后者，C 形态满足前者。
