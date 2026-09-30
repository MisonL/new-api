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

`service/convert.go` 的 13 个导出函数对应的协议映射逻辑，上游均有实现（`relaykit/relayconvert/internal/` 下的 `claude_messages`、`oai_chat`、`oai_responses`、`gemini_chat`、`shared` 五个族）。

其中 `NormalizeCacheCreationSplit` 等辅助函数在 relaykit 中同名存在（`oai_chat/to_claude_messages_resp.go:107`）。

### 3.3 上游缺失 → 需打补丁（2 项）

**补丁 1：Claude thinking block 映射与输入校验对齐**

上游两个请求转换器的 content block switch 均无 `thinking` case。`to_oai_chat_req.go:181`、`to_oai_responses_req.go:165` 两处 switch 本身无 `default`（同文件其他 switch 有，如 `:269`、`:265`），未知 block 被静默穿透丢弃。

改为：

1. 新增 `case "thinking"`，将 `ClaudeMediaMessage.Thinking` 写入目标协议的 reasoning 字段（Chat 写 `Message.ReasoningContent`，Responses 写 reasoning item）。opaque thinking（`Thinking` 空但 `Signature` 非空）与 `redacted_thinking` 显式报错，不伪装为文本
2. 保留非 assistant 的 role 门控：非 assistant 的 `thinking` / `redacted_thinking` 静默跳过。本地有测试锁定（`TestClaudeToOpenAIRequestIgnoresNonAssistantThinkingBlocks`、`…RedactedThinkingBlocks`），上游无此门控
3. 补 `default:` 分支，未知 block 显式报错。本地对应 `service/convert.go:355`

**补丁 2：DTO presence 保真**

上游 `relaykit/dto` 的可选标量字段缺失显式零值追踪。**注意：本地已有这些能力**，补丁 2 的实质是**把本地既有实现带进 relaykit**，而非从零设计：

| 能力 | 本地位置 | 本地生产使用点 |
|---|---|---|
| `OpenAIRequest.UnmarshalJSON`（未知字段保留） | `dto/openai_request.go` | — |
| `HasIncludeUsageField` / `RemoveIncludeUsage` | 同上 | 各 1 |
| `HasIncludeObfuscationField` | 同上 | 1 |

理由：AGENTS.md 第 3 节要求「客户端 JSON 解析后再转发给上游的可选标量字段，必须用带 `omitempty` 的指针类型保留显式零值」。

### 3.4 本地独有 → 逐项处置

见第 4 节。

## 4. 本地独有能力清单与处置

### 4.1 属协议层语义，值得提上游

**待确认，不下结论。** 已核验的本地独有项中，多数属部署关切（4.2）或上游已有更好实现（3.1）。但两项尚未定论：

- `ParseContentStrict` / `ParseToolCallsStrict`（4.4）：严格解析失败即报错，与 AGENTS.md「禁止静默容错」一致。需确认 relaykit 是否已有等价严格性机制；若无，则属协议层语义，应提上游
- think 标签提取族（4.2）：判定为部署关切是因它受模型名门控，但剥离本身是文本协议处理。若把门控与剥离拆开，剥离部分可能属协议层

本清单的完整性有限：穷尽盘点本地独有能力的工作被中断，`service/openaicompat/`、`relay/common/responses_*.go`、本地独有 `dto/` 文件均未逐项核验。**实现阶段需先完成该盘点**，列入第 10 节。

### 4.2 属部署关切，保留在宿主

| 能力 | 位置 | 判定依据 |
|---|---|---|
| 响应方向 reasoning 暴露策略 | `service/convert.go:526`（`shouldExposeReasoningAsClaudeThinking`），调用点 `:749`（流式）、`:1049`（非流式） | 模型名门控；且见第 6.1 节的撤回理由 |
| DeepSeek 空 thinking block 保留 | `service/convert.go:365` | 模型名门控 |
| think 标签提取族 | `service/convert.go:425`、`:475`、`:485`、`:489`、`:554`、`:597`、`:629`，调用点 `:103` | 模型名门控（`hasDeepSeekModelName`） |
| 工具名契约与别名还原 | `dto/tool_name.go` 等 | 引用宿主常量，受 `IsOpenAICompatibleResponses` 门控 |
| `context_compaction` 归一化 | `service/openaicompat/`、`controller/relay.go` | 上游零命中 |
| 加密内容探测 | `relay/common/responses_input_filter.go` | 硬编码宿主 marker `newapi.synthetic.compact:` |
| 本地专有错误契约 | `types/error.go` | 见第 4.3 节 |

`RetainEmptyReasoning` 钩子设计（供第 4.2 节第二项落地）：`convmeta.Options` 增加 `RetainEmptyReasoning func(modelName string) bool`，`nil` 表示不保留（与上游一致）。判定输入均在 relaykit 内部可得——`emptyThinkingBlock` 由补丁 1 新增的 `case "thinking"` 自行计算，`hasTools` 由 relaykit 自行累计——钩子只回答策略。

### 4.3 本地专有契约（上游完全不存在）

`types/error.go` 有以下符号，`git grep` upstream/main 全仓 0 命中，上游宿主 `types/` 也没有 `error.go`：

| 符号 | 本地引用文件数 |
|---|---|
| `ErrorCodeUpstreamTransportInterrupted` | 25 |
| `IsUpstreamTransportInterruptedError` | 9 |
| `IsOpenAIRequestParameterError` | 5 |

需决定迁入 `relaykit/types` 还是保留宿主。若 relaykit 转换器需据此产生错误，则必须迁入。

另注依赖方向：本地 `types/error.go:11` import 宿主 `common`，上游 `relaykit/types/error.go:10` import `relaykit/relayconvert/kitutil`——即 `common` 的相关 helper 已被抽到 `kitutil`，`types` 迁移实际牵动 `common → kitutil` 替换。

### 4.4 本地 DTO 独有方法

经方法集对比，本地 DTO 有 13 个 relaykit 没有的方法，**全部在生产代码中使用**：

| DTO 文件 | 方法 | 生产使用点 |
|---|---|---|
| `dto/openai_request.go` | `ParseContentStrict` | 20 |
| | `ParseToolCallsStrict` | 16 |
| | `Clone` | 13 |
| | `HasCompactionTrigger` | 10 |
| | `UnmarshalJSON` / `HasIncludeUsageField` / `HasIncludeObfuscationField` / `RemoveIncludeUsage` | 见 3.3 |
| `dto/openai_response.go` | `HasCompactionOutput` | 4 |
| | `EffectiveReason` | 2 |
| | `GetOutputTokenDetails` / `GetQuality` / `GetSize` / `HasImageGenerationCall` | — |
| `dto/claude.go` | `SearchToolNameByToolCallId` | 1 |

其中 `ParseContentStrict` / `ParseToolCallsStrict` 是严格解析（失败即报错），与 AGENTS.md「禁止静默容错」一致；`EffectiveReason` 属推理相关，需单独确认 relaykit 对应机制。

### 4.5 本地独有包与文件

| 项 | 说明 |
|---|---|
| `service/openaicompat/` | 上游无此包 |
| `relay/common/responses_input_filter.go`、`responses_tool_names.go`、`responses_stream_options_compat.go` | 上游无对应；三个 `responses_tool_names*.go` 当前未被 git 跟踪 |
| `dto/` 独有文件 | `json_extra_fields.go`、`responses_tools.go`、`responses_input_token_meta.go`、`opaque_openai_responses_request.go`、`tool_name.go`、`channel_routing.go`、`responses_routing.go`、`header_policy.go` 等 |
| `*WithError` 系列 | 4 个转换函数有 9 个生产调用点（6 个转换函数合计 14 个），relaykit 无对应签名 |

## 5. 必须保留的两项能力

用户明确要求保留「思考等级」与「thinking 历史回传」。这两项**都是请求方向**，映射如下：

| 需求 | 方向 | 现状 | 收敛后承载 |
|---|---|---|---|
| 思考等级 | 请求 | `relay/claude_handler.go:55-104` 手写后缀适配 + `request.Thinking`/`OutputConfig` 组装 | `reasoning.RenderClaude` + suffix 解析（上游实现更完整，见 3.1） |
| thinking 历史回传 | 请求 | `service/convert.go:103` 起的 `extractThinkTaggedHistory` 分支 + `case "thinking"` 提取 + DeepSeek 空块保留 | 补丁 1 的 `case "thinking"` + `RetainEmptyReasoning` 钩子 + think 标签族（归属待定，见第 10 节第 1 项） |

**响应方向**的 reasoning 暴露策略是第三个独立关切，与上述两项无关，且**不能**按客户端意图门控——理由见第 6.1 节。

## 6. 已撤回的设计及其理由

保留此节用于避免重蹈覆辙。

### 6.1 撤回「按客户端意图门控 reasoning 暴露」

**结论：响应方向不打 relaykit 补丁，策略落在宿主调用点。**

1. 依据不成立。「客户端未请求 thinking 就不应返回 thinking」不是协议不变量。relaykit 自身的 `claudeCapabilitiesFor` 为 fable-5、mythos-5、opus-5、sonnet-5 置 `defaultThinking = true`——上游代码里就有「这些模型默认思考」的证据。
2. 客户端意图在响应阶段取不到正确值。`relay/claude_handler.go:34` 对 Claude 请求做 `common.DeepCopy`，此后后缀适配器写入的 `request.Thinking`（`:58`、`:79`、`:91`）全部作用于拷贝。客户端发 `claude-opus-4-7-high` 时适配器让上游真的思考了，而 `info.Request.Thinking` 仍是 nil，门控会误判为「未请求」并丢弃上游真实返回的 thinking——与被修复的缺陷同构。

   `info.Request` 在 `genBaseRelayInfo`（`relay/common/relay_info.go:701`，函数定义于 `:670`）一次性赋值。`grep -c "info.Request = " relay/claude_handler.go` = 0，但全仓有一个反例：`controller/channel-test.go:957` 的 `info.Request = chatReq`（渠道测试合成请求）。准确表述是「正常 relay 路径下不反映适配器改写后的意图」。
3. relaykit 内不修改签名即不可达：Responses→Claude 非流式（`to_claude_messages_resp.go:15`）与流式（`to_claude_messages_stream_resp.go:82`）没有 `Meta` 参数。registry 层可达 `Meta`（`response_registry.go:22`、`:24`、`:28`、`:30` 带 `info`；`:26` 的 `ResponseStreamStateFactory` 不带），缺的是向内部转换器透传。

**曾用过的错误论据，不得再用**：「`convmeta.Options` 是请求阶段机制、响应方向零先例」。实测 `response_registry.go:500` 的 `toolconv.AttachHostedResponse(..., convmeta.OptionsOf(info))` 就是响应方向读取 `Options` 的实例；且 `internal/` 下 9 处 `OptionsOf` 命中中，`shared/claude/reasoning.go:21` 与 `shared/gemini/request.go:85` 不在 `*_req.go`。

### 6.2 撤回「阶段 1 必须更新 210 个 import 点」

见第 2 节的别名机制。210 是上界，实际改写量需按符号逐个判断。

### 6.3 撤回「hosted 工具桥接是本地独有」

上游 `toolconv/` 在 `main` 上已存在（见 3.1）。

## 7. 收敛路径

每阶段结束时仓库可编译、可运行、测试通过。

| 阶段 | 内容 | 完成判据 |
|---|---|---|
| 0 | `git fetch --unshallow upstream`；建立 CI 门禁（两个 module 分别 `go test`，`GOWORK=off`）；**完成本地独有能力盘点**（第 10 节第 1 项） | CI 能跑通现有测试；盘点清单产出 |
| 1 | 引入 relaykit module（根 `go.mod` 加 require + replace）；`types` 拆分：6 个文件迁入 `relaykit/types`，宿主保留 4 个；按符号逐个决定别名转发或改 import 路径；处置 4.3 的本地专有符号 | 两个 module 均可编译 |
| 2 | `dto` 拆分（上游宿主剩 8 个文件）；迁移 `IsStream` 签名；补丁 2（DTO presence）；处置 4.4 的 13 个独有方法 | 两个 module 均可编译；DTO 测试通过 |
| 3 | `ClaudeConvertInfo` 改为 `convmeta.ClaudeConvertInfo` 别名，同步改写 `service/convert.go` 的 22 处字段引用；`RelayInfo` 实现 `convmeta.Meta` 全部 15 个方法 | 编译期断言 `var _ convmeta.Meta = (*RelayInfo)(nil)` 通过 |
| 4 | 协议映射切换到 relaykit；打补丁 1；对齐第 4 节的处置结论；`*WithError` 系列在宿主包装 | 转换测试通过；grok reasoning 可见 |
| 5 | 宿主管道接入 relaykit 分派；响应方向暴露策略落到 6 条调用点；规则模型附带修正 | 全量测试通过 |
| 6 | 删除旧代码（`service/convert.go` 主体、`service/openaicompat/` 中被替代部分） | 无残留引用 |

三处硬依赖：

- `types` 是 `dto` 的前置（relaykit 的 DTO 必须 import `relaykit/types`）；本地 `types/error.go` import 宿主 `common`，上游改 import `kitutil`，所以 `types` 迁移牵动 `common → kitutil` 替换
- `ClaudeConvertInfo` 别名是 `Meta` 实现的前置。本地字段（`ToolCallStarted map[int]bool`、`ToolCallIDs`、`ToolCallNames`、`ToolCallPendingArgs`）与 `convmeta.ClaudeConvertInfo`（`ToolCalls []*ClaudeStreamToolCall`、`ToolCallByIndex`、`ToolCallByID`）**字段名与类型都不同**，`service/convert.go` 引用本地字段 22 处（6+5+5+6），改别名后全部编译失败
- `dto.Request.IsStream` 签名迁移**不可机械替换**：本地 `IsStream(c *gin.Context)`，relaykit `IsStream(c *http.Request)`；`dto/gemini.go:112` 用了 `c.Query("alt")`，`*http.Request` 无此方法

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
| AWS Converse | `relay/channel/aws/converse.go:748`（非流式）、`:1112`（流式 `thinking_delta`） | **否**，`converseOutputToClaudeResponse`（`:713`）直接从 `ConverseOutput` 构造 |

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
| 6 | 本地 DTO 独有方法 | 见 4.4 | 无对应 |

### 9.2 阶段 4 的输入校验对齐

本地对 Claude 请求做了 relaykit 没有的加固，切换会静默丢失：

| 校验 | 本地 | relaykit |
|---|---|---|
| 未知 content block | `service/convert.go:355` 显式报错 | 无 default，静默丢弃（补丁 1 第 3 项修复） |
| image 缺 source（Chat 方向） | `:252` 显式报错 | `to_oai_chat_req.go:189` 的 `case "image"` 在 `:190` 直接解引用 `mediaMsg.Source`，**nil pointer panic** |
| image 缺 source（Responses 方向） | `:252` 显式报错 | 有守卫：`claudeSourceURL`（`to_oai_responses_req.go:315`）对 nil 返回 `""` |
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

1. **完成本地独有能力盘点**（4.1）：`service/openaicompat/`、`relay/common/responses_*.go`、本地独有 `dto/` 文件尚未逐项核验，清单完整性有限。这是阶段 4 的前置
2. **think 标签提取族归属**（4.2）：6 个函数是 relaykit 零覆盖的本地增强，属「thinking 历史回传」。搬入 relaykit 成为带钩子的能力，还是留在宿主预处理阶段；若拆分门控与剥离，剥离部分可能属协议层
3. **本地专有错误符号归属**（4.3）：迁入 `relaykit/types` 还是保留宿主
4. **`*WithError` 系列形式**（4.5）：4 个函数 9 个生产调用点，宿主包装层怎么写
5. **响应方向暴露策略的具体形态**：落在 6 条路径时是过滤转换结果，还是调用前决定跳过 thinking 块（流式下后者更简单）
6. **本地 DTO 独有方法归属**（4.4）：13 个方法逐个决定提上游、宿主包装或放弃
7. **本地 hosted 工具与上游 `toolconv` 的收敛**：比对差异后决定收敛方式
8. **`types` 别名转发的适用范围**：逐个判断哪些符号可照上游 `constant` 办理
9. `relaykit/dto` 补丁 2 是否影响上游 golden 快照（`relaykit/relayconvert/testdata/golden/`，24 个 JSON）
10. `convmeta.Options` 新增钩子后，`ConvOptions()` 的缓存与失效语义是否满足跨渠道重试
11. 钩子闭包捕获语义：`Options` 由 `ConvOptions()` 缓存，跨渠道重试时读到哪个 `UpstreamModelName`
12. `reasonmap` 的宽松 default 是否需严格模式
13. thinking block 的 `signature`：relaykit 响应方向产出的 thinking block 只有 `{Type, Thinking}`，不含 `signature`；本地现状同样不生成。请求方向本地接受「有文本、无签名」的块（`service/convert.go:233-244` 仅对「空文本+有签名」报错），故往返可用。但 Anthropic 是否校验签名、客户端是否接受无签名 block，未验证
14. `enabled` 双语义在 rules 模式下的行为是否需一并规整
15. 上游 `feat/relaykit-tools` 分支（`a9a0e2272 feat(relaykit): preserve hosted tools across conversions`）是否应作为补丁基线

## 11. 事实依据

行号以基线 `dbfbc096f` 声明的本地基线 `9c4758b53` 为准。

| 事实 | 核验 |
|---|---|
| 上游 `service/convert.go` 27 行 | `git show upstream/main:service/convert.go \| wc -l` |
| 上游无 `service/openaicompat/` | `git ls-tree -r upstream/main -- service/openaicompat/` 空 |
| 两个 Claude 请求转换器 content block switch 无 `thinking`、无 `default` | `to_oai_chat_req.go:181`、`to_oai_responses_req.go:165` 逐 case 提取 |
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
| `types` 拆分爆炸半径 210 个 import 点 | `git grep -l '"…/new-api/types"' 9c4758b53 -- '*.go' \| wc -l`（工作区 216） |
| 上游宿主 172 个文件 import `relaykit/types` | `git grep -l` 计数 |
| 上游宿主 24 个文件仍 import 宿主 `types` | 同上 |
| 本地 `ClaudeConvertInfo` map 字段被引用 22 处 | `ToolCallStarted` 6 + `ToolCallIDs` 5 + `ToolCallNames` 5 + `ToolCallPendingArgs` 6 |
| relaykit 无 `*WithError` 系列 | `git grep WithError upstream/main -- relaykit/` 空 |
| 上游 `toolconv/` 覆盖 10 种 hosted 工具 | `toolconv/model.go` 的 `Kind*` 常量 |
| relaykit 内建 Claude 模型代能力表 | `reasoning/claude.go:35-74` |
| `dac55f0f` 是真实共同祖先（被浅克隆遮蔽） | `git cat-file -p 9724ef1b2` 记录 `parent aa7d0d39a4`；`git merge-base HEAD aa7d0d39a4` = `dac55f0f` |

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
- 「`convmeta.Options` 是请求阶段机制、响应方向零先例」：`response_registry.go:500` 即反例
- 「`info.Request` 从不被重新指向」：`controller/channel-test.go:957` 是反例
- 「relaykit 的 image 处理直接解引用会 panic」：只有 Chat 方向如此
- 「`*WithError` 系列 39 个生产调用点」：无可复现口径；实测 4 个函数 9 个，6 个函数 14 个
- 「relaykit 响应方向两个转换器都用 `_` 丢弃 Meta」：只有非流式如此
- 「补丁 3 在 relaykit 内不可实现」：registry 层 `Meta` 可达，缺的是向内部转换器透传
- 「hosted 工具桥接是本地独有」：上游 `toolconv/` 已存在
- 「阶段 1 必须更新 210 个 import 点」：上游用类型别名做到零调用点变更
- 「两个 Claude 转换器 switch 无 `default`」（未限定作用域）：只有 content block switch 无，同文件其他 switch 有
