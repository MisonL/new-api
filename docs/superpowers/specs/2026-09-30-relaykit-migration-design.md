# 协议转换层重构设计

采样时间：2026-09-30
本地基线：`HEAD` = `9c4758b53`（dev 分支）
上游基线：`upstream/main` = `789c97019`（2026-09-28）

行号引用一律以本文件声明的基线提交为准。工作区存在其他会话的未提交改动，工作区行号与提交行号不一致，不采用。

## 1. 目标

把本地协议转换层从「单文件硬编码直调」重构为「协议层与部署层分离」的架构，采用上游 `relaykit` 独立 module 作为协议层，本地部署关切保留在宿主并收敛到明确的管道位置。

本设计同时修复三个现存缺陷：

1. `grok-4.7` 等非 DeepSeek 模型的 reasoning 被静默丢弃
2. 降级行为不可观测（本地 `ConversionDiagnostic` 零命中）
3. 本地 CI 不执行任何 `go test`，转换测试无门禁

第 3 项无可复现的测试行数口径（见第 12 节），只陈述事实：CI 无 `go test` 门禁。

## 2. 边界规则

分层判据：

> relaykit 拥有「协议要求什么」，宿主拥有「这个部署或上游要求什么」。
> 判定：换个上游还成立吗？成立 → 协议层；不成立 → 部署层。
>
> 否决 1：若成立与否取决于模型名，一律判部署层。
> 否决 2：若实现引用宿主发明的字面量，一律判部署层，或降级为接口注入。
> 操作 3：若同一能力在多个调用点得到不同答案，先拆分，再对每一半单独判定。

该规则由对抗审查修正而来，原始二分版本在 15 项中有 8 项判错。

relaykit 的 `boundary_test.go` 是**部分**机械对应物，不是完整实现：它的 `kitDirs` 为 `relayconvert`、`dto`、`types`、`reasonmap`（不含 `convmeta`），只检查 import 前缀是否为宿主 module 或 gin，**不检查能力归属**。边界规则中「换个上游还成立吗」这一层判据没有机械保证，靠人工审查。

## 3. 架构分层

```
relaykit（协议层，独立 module，可 GOWORK=off go test）
  dto/           协议 DTO
  relayconvert/  注册表 + 分派 + 质量等级 + 4x4 矩阵
  reasoning/     Intent 抽象 + 能力渲染 + 冲突诊断
  convdiag/      诊断收集
  convmeta/      Meta 接口 + Options 钩子
        ▲ 依赖方向：宿主 -> relaykit（单向）

宿主
  出站管道      responses_input_filter / item id 规范化 / compaction 归一化
  转换后管道    tool bridge / tool name 别名与还原 / stream_options 兼容
  能力层        ResponsesChannelCapabilityRegistry + 观察值 TTL
  策略层        ProtocolConversionRule（端点对）
  路由层        渠道预检 / skip reason / synthetic compact
  host dto/     header_profile / channel_health / responses_routing 等
```

依赖方向单向：宿主 import relaykit，relaykit 不 import 宿主。由 `relaykit/relayconvert/boundary_test.go` 机械保证。

## 4. relaykit 补丁

relaykit 视为本仓库自有代码，允许修改。补丁限定为**两项**，均属协议语义（第三项原设计的撤回见下）。

### 补丁 1：Claude thinking block 映射与输入校验对齐

现状：上游两个请求转换器的 content block switch 均无 `thinking` case。`to_oai_chat_req.go:181` 与 `to_oai_responses_req.go:165` 两处 switch 本身无 `default` 分支（同文件其他 switch 有 default，例如 `to_oai_chat_req.go:269`、`to_oai_responses_req.go:265`），未知 block 被 Go switch 静默穿透丢弃。

- `relaykit/relayconvert/internal/claude_messages/to_oai_chat_req.go:181-227`
- `relaykit/relayconvert/internal/claude_messages/to_oai_responses_req.go:165-215`

改为：

1. 新增 `case "thinking"`，将 `ClaudeMediaMessage.Thinking` 写入目标协议的 reasoning 字段（Chat 写 `Message.ReasoningContent`，Responses 写 reasoning item）。opaque thinking（`Thinking` 为空但 `Signature` 非空）与 `redacted_thinking` 显式报错，不伪装为文本。
2. 保留非 assistant 角色的 role 门控：非 assistant 的 `thinking` / `redacted_thinking` 静默跳过而非报错。本地有测试锁定该行为（`service/convert_claude_request_test.go` 的 `TestClaudeToOpenAIRequestIgnoresNonAssistantThinkingBlocks`、`TestClaudeToOpenAIRequestIgnoresNonAssistantRedactedThinkingBlocks`），上游 switch 无 role 门控，需补上。
3. 为两个 switch 补 `default:` 分支，未知 block 显式报错。本地对应 `service/convert.go:355`（`Claude content block type %q ... is not supported`）。

理由：Claude thinking block 到 Chat `reasoning_content` 是纯协议映射，不依赖任何上游特性。上游缺此分支属遗漏。

### 补丁 2：DTO presence 保真

现状：`relaykit/dto` 的可选标量字段缺失显式零值追踪。

- `OpenAIResponsesRequest` 无 `UnmarshalJSON`、无 `Extra`，未知字段在往返中被静默丢弃
- `StreamOptions` 为两个裸 bool，无 `HasIncludeUsageField()` 等 presence 查询
- 响应方向：本地 `ResponsesStreamResponse`（`dto/openai_response.go:450`）有 `Error *types.OpenAIError`、`Text`、`Arguments`、`CallId`、`Item` 等字段；relaykit 对应类型的 `CallId` 位于 `ResponsesOutput` 内而非事件顶层。本地**没有** `Code` 字段（此前 spec 误写「`Code` 为 `string` 而非 `any`」——本地与该字段无关，`Code any` 仅出现在 `dto/error.go:14` 的注释里）

改为：可选标量用带 `omitempty` 的指针类型；`OpenAIResponsesRequest` 补未知字段保留；`StreamOptions` 补 presence 追踪；对齐 `ResponsesStreamResponse` 字段布局。

理由：AGENTS.md 第 3 节明确要求「客户端 JSON 解析后再转发给上游的可选标量字段，必须用带 `omitempty` 的指针类型保留显式零值」。未知字段丢弃与「禁止静默容错」冲突。

### 原补丁 3（已撤回）：reasoning 暴露策略

**结论：不打 relaykit 补丁。** 响应方向的暴露策略落在宿主 5 个调用点（见下）。撤回理由经独立复现：

1. 门控依据不成立。「客户端未请求 thinking 就不应返回 thinking」不是协议不变量。relaykit 自身的 `claudeCapabilitiesFor`（`relaykit/relayconvert/reasoning/claude.go:35-74`）为 fable-5、mythos-5、opus-5、sonnet-5 置 `defaultThinking = true`——上游代码里就有「这些模型默认思考」的证据。把某一代的默认行为写成「协议正确行为」是推断而非事实。
2. 客户端意图在响应阶段取不到正确值。`relay/claude_handler.go:34` 对 Claude 请求做 `common.DeepCopy`，此后 `-thinking` 后缀与 effort 后缀适配器写入的 `request.Thinking`（`:58`、`:79`、`:91`）全部作用于拷贝；`info.Request` 在 `genBaseRelayInfo`（`relay/common/relay_info.go:749`）一次性赋值，此后从不重新指向（`grep -c "info.Request = " relay/claude_handler.go` = 0）。客户端发 `claude-opus-4-7-high` 时适配器让上游真的思考了，而 `info.Request.Thinking` 仍是 nil，门控会误判为「未请求」并丢弃上游真实返回的 thinking——与被修复的缺陷同构。
3. **`convmeta.Options` 是请求阶段机制，不适合做响应门控。** `git grep OptionsOf upstream/main -- relaykit/relayconvert/internal/` 的全部 9 处命中都在 `*_req.go`（请求方向），响应方向零使用。把新钩子挂到 `Options` 会开创上游没有的先例。
4. relaykit 内不修改签名即不可达：Responses→Claude 非流式（`to_claude_messages_resp.go:15`）与流式（`to_claude_messages_stream_resp.go:82`）**没有 `Meta` 参数**。**修正此前 spec 的过强表述**：`response_registry.go:22-30` 的全部四个分派函数类型签名都带 `info convmeta.Meta`，且 `:1004` 的流式包装实际调用了 `info.EnsureClaudeConvertInfo()`——所以 Meta 在 registry 层**可达**，缺的是向内部转换器的透传。正确表述是「需改签名透传」，不是「不可达」。这不改变撤回结论：即使透传了 `Meta`，客户端意图数据通路仍不通（第 2 条），且 `Options` 不适合该用途（第 3 条）。

现状（本地）：响应方向按**模型名**门控，非 DeepSeek 模型的 reasoning 被整段丢弃。

```go
// service/convert.go:526（定义）, 调用点 :749（流式）、:1049（非流式）
func shouldExposeReasoningAsClaudeThinking(model string, info *relaycommon.RelayInfo) bool {
    return hasDeepSeekModelName(model, info)
}
```

该门控由 `2e986a65f` 引入，是**新增的抑制**（此前无条件暴露），且无任何测试锁定其行为。

现状（上游）：无条件暴露。

改为：**响应方向的暴露策略留在宿主，不进 relaykit。**

- relaykit 保持无条件暴露（其现有行为，纯协议语义，不打补丁）
- 宿主在 5 个响应方向调用点按部署策略处理：`relay/channel/openai/helper.go:45`、`:245`、`relay/channel/openai/relay-openai.go:335`、`relay/channel/openai/chat_via_responses.go:280`、`relay/channel/gemini/relay-gemini.go:1787`
- 这 5 个调用点**都已持有 `info`**，有完整会话上下文，是唯一同时满足「拿得到决策输入」和「不污染 relaykit 协议层」的位置
- 默认行为对齐上游（暴露），修复 grok；现有 DeepSeek-only 行为降级为宿主可配置策略

理由：响应方向门控是**部署关切**（哪些上游的 reasoning 值得暴露给 Claude 客户端），不是协议语义。`Options` 是请求阶段机制（9 处使用全在 `*_req.go`），不适合承载它；在 relaykit 内部实现则需改转换器签名且仍取不到客户端意图。

### 明确不打入 relaykit 的能力

以下按边界规则属部署层，保留在宿主，不进入 relaykit：

| 能力 | 判定依据 |
|---|---|
| 响应方向 reasoning 暴露策略 | 部署关切；`Options` 为请求阶段机制，响应方向零先例 |
| think 标签提取族（见下） | 模型名门控（否决 1） |
| DeepSeek 空 thinking block 保留 | 模型名门控（否决 1），以 `RetainEmptyReasoning` 钩子注入 |
| custom / namespace / tool_search 工具桥接 | 依赖渠道能力快照，部署关切 |
| 工具名契约与别名还原 | 引用宿主常量，且受 `IsOpenAICompatibleResponses` 门控 |
| `context_compaction` 归一化 | 上游版本垫片，上游零命中 |
| 加密内容探测 | 硬编码宿主 marker `newapi.synthetic.compact:`（否决 2） |

### 补充：think 标签提取族（此前 spec 遗漏）

本地有一族 relaykit **完全没有**的能力，用于把 assistant 历史文本里的 `<think>...</think>` 拆成可见文本 + reasoning：

| 函数 | 位置 |
|---|---|
| `shouldExtractThinkTaggedClaudeHistory` | `service/convert.go:489`（门控，由 `hasDeepSeekModelName` 判定） |
| `splitThinkTaggedText` | `:554` |
| `indexASCIIFold` | `:597` |
| `appendVisibleThinkText` | `:629` |
| `appendReasoningText` / `appendTrimmedReasoningText` | `:475` / `:485` |
| `needsVisibleTextBoundarySpace` | `:425` |

调用点 `:103`（`extractThinkTaggedHistory := shouldExtractThinkTaggedClaudeHistory(...)`）。`git grep 'think>' upstream/main -- relaykit/` 零命中。本地有 8+ 处测试锁定（`service/convert_claude_request_test.go` 的 `:488`、`:511`、`:558`、`:581`、`:607`、`:652`、`:696`、`:754`）。

**这是「thinking 历史回传」的组成部分**，不是可选项。按边界规则属部署层（模型名门控），与 `RetainEmptyReasoning` 同族。实现阶段需决定：搬入 relaykit 成为带钩子的能力，或在宿主预处理阶段完成。本设计倾向前者（与补丁 1 的 thinking 处理共生），但需在实现时确认——列入第 11 节未决项。

## 5. 宿主管道

保留在宿主，但收敛到明确的管道位置，消除现状中「协议映射与部署特判交错」的问题。现状 `service/convert.go` 的 content block switch 内协议映射与 DeepSeek 特判交错，且 `emptyThinkingBlock`（`:198` 声明、`:244` 设置、`:365` 读取）跨越 switch 边界传递状态。

| 管道 | 内容 | 位置 |
|---|---|---|
| 出站前 | input item id 规范化、compaction 归一化、加密内容剔除 | `relay/common/responses_input_filter.go` |
| 转换后 | tool name 别名注册与还原、`stream_options` 兼容 | `relay/common/responses_tool_names.go`、`relay/responses_stream_options_compat.go` |
| 能力层 | 渠道能力注册表、观察值 TTL、fail-closed 语义 | `dto/channel_settings.go`、`model/channel_responses_tool_capability.go` |
| 策略层 | `ProtocolConversionRule` 端点对匹配 | `setting/model_setting/global.go`、`service/openaicompat/policy.go` |
| 路由层 | 渠道预检、skip reason、synthetic compact | `controller/relay.go`、`service/responses_synthetic_compact*.go` |

## 6. 规则模型：保留端点对

不引入「转换器 ID 选择策略」。理由：

1. 在只有 `chat_completions` / `responses` 两个端点的当下，「2 选 1 方向」比「8 选 1 ID（其中 6 个会报错）」更直观
2. 上游自身也不是纯 ID：`AdvancedCustomRoute` 用 `IncomingPath` + `Models` + `Converter`，匹配条件仍是端点加模型
3. 转换器 ID 方案无法表达 `only_when_tool_bridge_required` 的请求体乘渠道能力判定
4. 方向校验会从「保存即拒绝」退化为「运行时才报错」

附带修正三项（审查发现，与重构同批完成）：

1. 消除重复：`controller/relay.go` 的 `responsesLocalCompactionTriggerRuleRequiresChannelSkip` 与 `responsesCompactionRuleRequiresChannelSkip` 两个函数体**逐字相同**（仅函数名不同，diff 无其他差异），收敛为单一实现加两个调用名。

   **注意：这不是向 `rule.IsChannelEnabled` 收敛。** 该函数体与库函数语义不同：`AllChannels` 分支返回能力门控结果（`!SupportsCompactionItemPassthrough`）而库函数返回 true；还多一层 `HasResponsesProxyCompatibilityProfile()` 前置检查；且不检查 `rule.Enabled`。它表达的是「该渠道能否透传 compaction item」这一部署关切，不是通用的渠道启用判定。只消除两个函数之间的重复，不改语义。
2. `ChatCompletionsToResponsesPolicyWarnings` 无生产调用方（仅 `setting/model_setting/global.go:392` 定义与 `global_test.go:623` 测试），接入设置 API，使管理员能看到「配了不生效」的告警
3. 前端 `serializeProtocolPolicy` 无条件写 `enabled: true`（`web/default/src/features/system-settings/models/protocol-conversion-policy-utils.ts:750`），会把停用配置翻转为启用，修复

## 7. 分阶段落地

每阶段结束时仓库可编译、可运行、测试通过。

阶段顺序由依赖关系决定，不是由「先易后难」决定。三处依赖是硬约束：

- `types` 是 `dto` 的前置：**210 个文件** import 宿主 `types`，relaykit 的 DTO 必须 import `relaykit/types`。上游把 6 个文件移入 `relaykit/types`（`channel_error.go`、`error.go`、`file_data.go`、`file_source.go`、`relay_format.go`、`request_meta.go`），宿主 `types` 只剩 `price_data.go`、`rw_map.go`、`set.go`、`task_artifact.go`。

  这不是纯搬迁：本地 `types/error.go` 有上游**完全不存在**的符号（`ErrorCodeUpstreamTransportInterrupted` 被 25 个文件引用、`IsUpstreamTransportInterruptedError` 9 个、`IsOpenAIRequestParameterError` 5 个）。这些是本地错误处理契约，需迁入 `relaykit/types` 或保留在宿主，且要决定 relaykit 是否应知道它们。

- `ClaudeConvertInfo` 别名是 `Meta` 接口实现的前置，且**不是改个类型名那么简单**：本地 `relay/common/relay_info.go` 的 `ClaudeConvertInfo` 是独立结构体，字段（`ToolCallStarted map[int]bool`、`ToolCallIDs`、`ToolCallNames`、`ToolCallPendingArgs`）与 `convmeta.ClaudeConvertInfo`（`ToolCalls []*ClaudeStreamToolCall`、`ToolCallByIndex`、`ToolCallByID`）**字段名与类型都不同**。`service/convert.go` 引用本地字段 **22 处**（`ToolCallStarted` 6 + `ToolCallIDs` 5 + `ToolCallNames` 5 + `ToolCallPendingArgs` 6）。改为别名后这 22 处全部编译失败，必须同步改写——这是阶段 3 的实际工作量。

- `dto.Request.IsStream` 签名迁移是 `dto` 迁移的前置，且**不可机械替换**：本地为 `IsStream(c *gin.Context)`，relaykit 为 `IsStream(c *http.Request)`；`dto/gemini.go:112` 的实现用了 `c.Query("alt")`，`*http.Request` 无此方法。`dto/claude.go:391` 忽略参数可机械迁移。

| 阶段 | 内容 | 完成判据 |
|---|---|---|
| 0 | `git fetch --unshallow upstream`；建立 CI 门禁（两个 module 分别 `go test`） | CI 能跑通现有测试 |
| 1 | 引入 relaykit module（根 go.mod 加 require + replace）；`types` 拆分：6 个文件迁入 `relaykit/types`，宿主保留 4 个；处理本地独有错误符号；更新 210 个 import 点（`RelayFormat` 子集 51 个） | 两个 module 均可编译 |
| 2 | `dto` 拆分：协议 DTO 迁入 `relaykit/dto`，宿主 DTO 留 `dto/`（上游宿主仅剩 8 个文件）；迁移 `IsStream` 签名（`GeminiChatRequest` 需改为从 `*http.Request` 读取 query 与 path）；补未知字段保留 | 两个 module 均可编译；DTO 相关测试通过 |
| 3 | `ClaudeConvertInfo` 改为 `convmeta.ClaudeConvertInfo` 类型别名，同步改写 `service/convert.go` 的 22 处字段引用；`convmeta.Meta` 接口实现：`RelayInfo` 补齐 15 个方法 | 编译期断言 `var _ convmeta.Meta = (*RelayInfo)(nil)` 通过 |
| 4 | 文本转换切换到 relaykit；打补丁 1、2；对齐第 10 节列出的语义差异；`*WithError` 系列（本地 39 个生产调用点，relaykit 无对应）需在宿主包装或放弃 | 转换测试通过；grok reasoning 可见 |
| 5 | 宿主管道接入 relaykit 分派；规则模型附带修正；响应方向暴露策略落到 5 个宿主调用点 | 全量测试通过 |
| 6 | 删除旧代码（`service/convert.go` 主体、`service/openaicompat/` 中被替代部分） | 无残留引用 |

阶段 0 的 unshallow 是前置条件：当前仓库是 shallow clone（`.git/shallow` 截断于 `9724ef1b2`），`git merge-base HEAD upstream/main` 返回空，三点 diff 不可用。

两点 diff（`git diff upstream/main HEAD`）当前**可以**工作，但它比较的是两棵树的最终状态，无法回答「本地相对共同祖先改了什么」。迁移需要后者来判断哪些本地改动是增强、哪些是上游已覆盖，所以 unshallow 仍是必要前置。

需注意 unshallow 的目标是 `upstream` 而非默认的 `origin`：`origin/main` 停在 `51074ac37`（2026-07-10），与 `upstream/main` 不是同一历史。当前本地历史与 `upstream/main` 的提交交集为 0，`9724ef1b2` 在 `upstream/main` 的祖先链上但不在 `HEAD` 的祖先链上。

## 8. CI 门禁

现状：`.github/workflows/` 下 7 个 workflow 零 `go test`，`makefile` 无 `test` 目标。

改为：新增 workflow，对根 module 与 relaykit module 分别执行 `go vet ./...`、`go build ./...`、`go test ./...`（`GOWORK=off`），并缓存两个 `go.sum`。

理由：阶段 1、2 的 DTO 拆分涉及 210 个 `types` import 点与 327 个 `dto` import 点，无自动化门禁无法保证每步可运行。这是本设计**规模最大的部分**，远超「转换层重构」的字面预期——`service/convert.go` 只有 2229 行，而 DTO 收敛的爆炸半径是其两个数量级。

## 9. 配置迁移

存量配置位于 `options` 表 `global.chat_completions_to_responses_policy` 键。

实测存量形态：

- 生产环境：`{"all_channels":false,"enabled":false}`（纯 legacy）
- 隔离开发环境：含 `rules[]` 与端点对

规则 schema 不变，因此**存量配置零迁移**。

需注意三个现存脏状态（非本次引入，但影响迁移正确性）：

1. `enabled` 双语义：rules 模式下是总闸，legacy 模式下是单条开关；`UnmarshalJSON` 在有 rules 且无 `enabled` 键时自动置 true
2. 前端会把 `enabled:false` 翻成 `true`（见第 6 节修正 3）
3. `tool_bridge_types` 在前端 UI 不可见，仅能通过原始 JSON 维护

## 10. 验证策略

### 10.1 阶段 4 的语义对齐清单

本地 `service/convert.go` 的 13 个导出函数与 relaykit 的同名等价物**不是逐一等价**。切换前必须对下列差异逐项决定「保留、放弃或改写」，并同步调整锁定的测试。这是阶段 4 的显式交付项，不是实现细节。

| # | 差异 | 本地行为 | relaykit 行为 | 影响 |
|---|---|---|---|---|
| 1 | finish_reason 严格校验 | `stopReasonOpenAI2ClaudeStrict`（`service/convert.go:1125`）对未知值返回错误 | `reasonmap.OpenAIFinishReasonToClaudeStopReason` 的 `default: return finishReason` 原样透传 | 本地「拒绝未知 finish_reason」的测试会失败 |
| 2 | 多 choice 拒绝 | `ResponseOpenAI2ClaudeWithError` 在 `len(Choices) > 1` 时报错 | 遍历所有 choice 并拼接进同一 Claude response | 语义变化，需明确取舍 |
| 3 | `Message.ThinkingContent` | `dto/openai_request.go:407` 有该字段，`GetReasoningContent()` 三级回退 `ReasoningContent → ThinkingContent → Reasoning` | relaykit `Message` 无 `ThinkingContent`，两级回退 | 本地 5 个生产点依赖（`relay/channel/openai/adaptor.go:238`、`relay/channel/claude/relay-claude.go:506`、`relay/channel/gemini/relay-gemini.go:587`、`relay/channel/ollama/relay-ollama.go:203` 等） |
| 4 | `ClaudeConvertInfo` 字段 | 见第 7 节依赖约束 | 字段名与类型均不同 | 阶段 3 的类型别名迁移 |
| 5 | `ResponsesStreamResponse` 形状 | `dto/openai_response.go:450` 有 `Error *types.OpenAIError`、`Text`、`Arguments`、`CallId`、`Item` | `CallId` 在 `ResponsesOutput` 内而非事件顶层 | 补丁 2 覆盖 |

### 10.2 阶段 4 的输入校验对齐

本地对 Claude 请求做了 relaykit 没有的加固。阶段 4 切换会静默丢失，必须搬入 relaykit 或明确放弃：

| 校验 | 本地 | relaykit 现状 |
|---|---|---|
| 未知 content block | `service/convert.go:355` 显式报错 | 无 default，静默丢弃（补丁 1 第 3 项修复） |
| image 缺 source（Chat 方向） | `:252` 显式报错 | `to_oai_chat_req.go:189` 直接解引用 `mediaMsg.Source`，**nil pointer panic** |
| image 缺 source（Responses 方向） | `:252` 显式报错 | 有守卫：`claudeSourceURL`（`to_oai_responses_req.go:315`）对 nil 返回 `""`，安全 |
| image source.type 校验 | `:282` 分派 base64/url，其他报错 | 无分派，一律按 base64 拼接 |
| 非 assistant thinking | `:234`、`:246` 静默跳过 | 无 role 门控（补丁 1 第 2 项修复） |
| tool_use 缺 id/name | `:287` 显式报错 | 无校验 |

image 缺 source 的 panic 只存在于 Chat 方向（`to_oai_chat_req.go:189-190`），可复现：一个 `{"type":"image"}` 无 `source` 的请求在该路径下会被 `gin.CustomRecovery` 变成 500，而本地返回带定位信息的 400。Responses 方向不受影响。

### 10.3 验证命令

- 每阶段运行 `go test ./controller ./model ./relay/common ./relay/helper ./service`（AGENTS.md 第 6 节）
- 阶段 4 需专项验证：构造含 thinking block 的 Claude 多轮工具调用请求，验证 DeepSeek 不再 400
- 阶段 4 需验证 grok 的 reasoning 可见（当前被丢弃）
- 前端改动运行 `bun run lint` 与 `bun run build`
- 验证记录沉淀到 `docs/reviews/CR-*.md`

不得以局部测试通过表述为全链路已验证。

## 11. 未决项

以下项在实现阶段需实测确认，本设计不预设结论：

1. `relaykit/dto` 补丁 2 的改动会否影响上游 golden 快照（`relaykit/relayconvert/testdata/golden/`，24 个 JSON），需实测后决定是否更新快照
2. `convmeta.Options` 新增钩子后，`ConvOptions()` 的缓存与失效语义（上游在 `InitChannelMeta` 末尾置 nil）是否满足本地跨渠道重试场景
3. `dto.Request.IsStream` 签名迁移的调用点数量与 `GeminiChatRequest` 的改写方式（需从 `*http.Request` 读 query 与 path，不能机械替换）
4. `relaykit` 上游仍在演进（`feat/relaykit-tools`、`feat/relaykit-perf`），补丁 1、2 可考虑提上游以减少分叉
5. 阶段 4 的 5 项语义差异（第 10.1 节）与 5 项输入校验（第 10.2 节）的逐项取舍，需在切换前定案
6. 钩子闭包捕获语义：`Options` 由 `ConvOptions()` 缓存，宿主注入的钩子若闭包捕获 `info`，跨渠道重试时读到的是哪个 `UpstreamModelName`，需实测
7. `reasonmap` 的宽松 default 是否需要在 relaykit 侧增加严格模式，或由宿主包装
8. thinking block 的 `signature`：relaykit 响应方向产出的 thinking block 只有 `{Type, Thinking}`，不含 `signature`（golden 快照确认）。本地现状同样不生成。请求方向本地接受「有文本、无签名」的 thinking 块（`service/convert.go:233-244` 仅对「空文本+有签名」报错，`:237-239` 接受有文本的块），所以往返可用。但 Anthropic 对 preserved thinking 是否校验签名，以及客户端是否接受无签名 block，未在本地验证，属未评估的风险面
9. `enabled` 双语义在 rules 模式下的行为（第 9 节脏状态 1）是否需要在迁移中一并规整
10. 转换测试的统计口径：本设计不引用具体行数（无可复现口径），CI 门禁按 package 覆盖而非按行数
11. **think 标签提取族归属**（第 4 节补充小节）：`splitThinkTaggedText` 等 6 个函数是 relaykit 零覆盖的本地增强，被 `hasDeepSeekModelName` 门控，属「thinking 历史回传」的组成部分。需决定：搬入 relaykit 成为带钩子的能力，还是在宿主预处理阶段完成。本设计倾向前者，但需实现时确认
12. **本地 `types/error.go` 独有符号的归属**：`ErrorCodeUpstreamTransportInterrupted` 等被生产代码广泛引用，需决定迁入 `relaykit/types` 还是保留宿主。若 relaykit 转换器需要据此产生错误，则必须迁入
13. **`*WithError` 系列的处理方式**：本地 39 个生产调用点依赖 `ClaudeToOpenAIRequest` 等函数的 error 返回，relaykit 无对应签名。需决定宿主包装层的形式
14. **响应方向暴露策略的具体形态**：落在 5 个宿主调用点时，是过滤转换结果、还是在调用前决定跳过 thinking 块，需在实现时定案（流式场景下后者更简单）

第 11 项原为「补丁 3 按客户端意图门控」，已撤回，理由见第 4 节补丁 3。

## 12. 事实依据

本设计的承重事实均经独立核验。行号以本文件声明的基线 `9c4758b53` 为准。

| 事实 | 核验 |
|---|---|
| 上游 `service/convert.go` 27 行 | `git show upstream/main:service/convert.go \| wc -l` |
| 上游无 `service/openaicompat/` | `git ls-tree -r upstream/main -- service/openaicompat/` 空 |
| 两个 Claude 请求转换器 switch 无 `thinking`、无 `default` | `to_oai_chat_req.go:181`、`to_oai_responses_req.go:165` 逐 case 提取 |
| 上游 `convmeta.Options` 零 tool bridge 字段 | `git grep -c ToolBridge upstream/main -- relaykit/relayconvert/convmeta/` 无命中 |
| 上游 responses→chat 不处理 namespace / tool_search | `git grep -c` 均无命中 |
| 上游无 Responses→Chat 全局策略 | `git grep ShouldResponsesUseChatCompletions upstream/main` 空 |
| 本地 `ConversionDiagnostic` 零命中 | `grep -rl` 返回 0 |
| 本地 `dto` 被 327 个文件 import（基线口径） | `git grep -l '"github.com/QuantumNous/new-api/dto"' 9c4758b53 -- '*.go' \| wc -l`。工作区为 340——本表统一采用基线口径 |
| 本地 `dto` 有 10 个非测试文件 import gin | 逐文件核验 |
| 本地 CI 不跑 `go test` | `grep -rn "go test" .github/workflows/` 命中 0；`makefile` 无 `test` 目标 |
| `context_compaction` 上游零命中 | `git grep -c` 无命中 |
| 三个 `responses_tool_names*.go` 未跟踪 | `git status --short` 显示 `??` |
| `convmeta.Meta` 15 个方法无客户端请求访问器 | `relaykit/relayconvert/convmeta/meta.go:18-54` 逐方法核验 |
| registry 层全部 4 个分派函数类型都带 `info convmeta.Meta` | `response_registry.go:22-30` |
| `convmeta.Options` 上游仅用于请求方向 | `git grep OptionsOf upstream/main -- relaykit/relayconvert/internal/` 全部 9 处命中都在 `*_req.go` |
| `info.Request` 一次性赋值且从不重新指向 | `relay/common/relay_info.go:749`；`grep -c "info.Request = " relay/claude_handler.go` = 0 |
| 两个 `*RequiresChannelSkip` 函数体逐字相同 | `diff` 仅函数名不同 |
| `types` 拆分：上游 6 文件入 `relaykit/types`，宿主剩 4 | `git ls-tree` 两侧对比 |
| `dac55f0f` 非共同祖先（浅克隆下鉴别力有限） | `git merge-base --is-ancestor dac55f0f upstream/main` rc=1；`comm -12` 两历史 = 0。**注意**：`.git/shallow` 截断上游历史，该结果是弱推论；决定性证据是 `git merge-base HEAD upstream/main` 返回空 + 三点 diff `fatal: no merge base` |
| 本地 `types/error.go` 有上游不存在的符号 | `ErrorCodeUpstreamTransportInterrupted`（本地 25 文件引用）、`IsUpstreamTransportInterruptedError`（9）、`IsOpenAIRequestParameterError`（5）；`git grep` upstream/main 均 0 命中 |
| 本地 `ClaudeConvertInfo` 的 map 字段被 `convert.go` 引用 22 处 | `ToolCallStarted` 6 + `ToolCallIDs` 5 + `ToolCallNames` 5 + `ToolCallPendingArgs` 6 |
| relaykit 无 `*WithError` 系列函数 | `git grep WithError upstream/main -- relaykit/` 空；本地 39 个生产调用点 |
| relaykit 内建 Claude 模型代能力表 | `relaykit/relayconvert/reasoning/claude.go:35-74` 覆盖 fable-5、mythos-5、Opus 4.5/4.6/4.7/4.8/5、Sonnet 4.6/5 |

已撤回的断言（原 spec 有、经复核不成立）：

- 「本地 `service/convert.go` 2236 行」：那是工作区行数（含未提交改动），基线提交为 2229 行
- 「22205 行转换测试」：无可复现口径
- 「`dto` 被 340 个文件 import」：那是工作区口径，基线为 327
- 「18 个 dto 文件 import 宿主 types」：那是工作区口径，基线为 17
- 「真实共同祖先 `dac55f0f`」：见上表，不是共同祖先
- 「无法用 diff 驱动迁移」：两点 diff 可用，仅三点 diff 不可用
- 「两个 `*RequiresChannelSkip` 等价于 `IsChannelEnabled`」：两者语义不同，且这两个函数是**互相**重复
- 「无条件暴露不符合 Anthropic 行为」：这是推断，且随模型代变化，不是协议不变量
- 「本地 `ResponsesStreamResponse` 的 `Code` 为 `any`」：**本地根本没有 `Code` 字段**，此前把 relaykit 的字段名与本地类型名拼接了
- 「relaykit 响应方向两个转换器都用 `_` 丢弃 Meta」：只有非流式用 `_`，流式实际使用了 `info`
- 「relaykit 的 image 处理直接解引用会 panic」：只有 Chat 方向如此，Responses 方向有 nil 守卫
- 「补丁 3 在 relaykit 内不可实现」：表述过强，registry 层 `Meta` 可达，缺的是向内部转换器的透传
