# CR: 协议转换层收敛到上游 relaykit 的设计与五轮对抗审查

采样时间：2026-09-30（2026-10-01 补充本地独有能力盘点、第五轮架构审查）
本地基线：`9c4758b53`（dev 分支；记录写作时 HEAD = `a1d5e9aad`；盘点时 HEAD = `2cf100a1d`）
上游参照：`upstream/main` = `789c97019`（2026-09-28）

## 授权与边界

本记录只做设计与审查，**未修改任何生产代码**。全部改动限于设计文档：

- `docs/superpowers/specs/2026-09-30-relaykit-migration-design.md`

工作区存在其他会话的 500+ 未提交改动，全程未触碰、未回退、未整理。所有上游代码经 `git show upstream/main:<path>` / `git grep ... upstream/main` 读取，未执行 `git checkout`。

盘点阶段使用 3 个只读子代理（`service/openaicompat/`、`relay/common/responses_*.go`、本地独有 `dto/` 各一）。其中 2 个在用尽上下文前未产出完整报告，仅返回分析过程；主代理对**每一条**被采纳的结论均独立复现，包括推翻子代理自身的一处断言（其称 dto `Clone` 跨包生产引用为 0，实测有 1 个文件）。

## 任务来源

用户要求：替换协议转换层为上游 `relaykit` 架构，**兼顾思考等级与 thinking 历史回传**，本地增加的功能在改造后的基础上重新设计为最优雅最高效的形式。

用户明确否决了以风险控制为目标的方案（"我要的并不是风险可控，而且让项目实现最优架构，最佳效率"），并在多轮中持续要求"用 agents 审查"。

## 审计方式

四轮对抗性审查，每轮更换角度而非重复：

| 轮次 | 子代理数 | 角度 |
| --- | --- | --- |
| 1 | 1 | 事实核验 + 架构攻击 |
| 2 | 2 | 补丁方案可达性；阶段划分与事实表 |
| 3 | 3 | 架构选择是否最优；宿主调用点方案可行性；增量事实核验 |
| 4 | 2 | 阶段可执行性；本地独有能力完整性 |
| 5 | 2 | 架构攻击（决策可执行性、依赖顺序、验证策略缺口）6 条全部复现成立；事实核验（逐条复现第 12 节断言）大部分通过、6 处需修正 |

主代理对每条代理结论独立复现后才采纳。**四轮中每轮都抓出主代理在上一轮刚引入的错误**：6 → 3 → 2 → 3 处。

## 核心结论

### 1. 上游已完成同一迁移

`upstream/main` 的 `service/convert.go` 只剩 **27 行**纯委托（本地基线 **2229 行**），协议转换逻辑全部在独立 module `relaykit` 中：

- `relaykit/go.mod`：`module github.com/QuantumNous/new-api/relaykit`，go 1.25.1，可 `GOWORK=off go test`
- 根 `go.mod`：`require … v0.0.0` + `replace … => ./relaykit`
- 宿主改造规模：**172 个文件** import `relaykit/types`；262 个文件 import relaykit 下某包
- 宿主残留：`dto/` 剩 8 个文件，`types/` 剩 4 个文件

成熟度证据：34 个测试文件 / 8430 行测试；24 个 golden 快照；仅 2 处 TODO，无未实现桩；上游 CI 以 `GOWORK=off` 独立 vet/build/test relaykit。

因此设计文档已从"迁移方案"重构为**差异对照清单**——上游是完成态，是最可靠的参照系。

### 2. 上游用 Go 类型别名做到零调用点变更

`upstream/main:constant/endpoint_type.go`：

```go
package constant

import "github.com/QuantumNous/new-api/relaykit/types"

// EndpointType moved to types with the conversion kit; aliases keep host
// code compiling unchanged.
type EndpointType = types.EndpointType

const (
	EndpointTypeOpenAI    = types.EndpointTypeOpenAI
	EndpointTypeAnthropic = types.EndpointTypeAnthropic
	// …
)
```

上游两种手法并用：`constant/` 用别名转发；`relay/channel/openai/adaptor.go` 等直接改 import。两个包可并存——`relay/common/relay_info.go` 同时有 `"…/relaykit/types"` 与 `hosttypes "…/new-api/types"`。

**影响**：设计文档曾规划"阶段 1 必须更新 210 个 import 点"，据此把 DTO 收敛描述为"规模最大的部分"。210 是**上界**，实际改写量须按符号逐个判断。

### 3. 上游更强的地方比预想的多

| 能力 | 本地 | 上游 |
| --- | --- | --- |
| 思考等级渲染 | `relay/claude_handler.go:55-104` 硬编码 `claude-opus-4-6/4-7` 前缀 | `reasoning.RenderClaude` + `claudeCapabilitiesFor` 覆盖 fable-5、mythos-5、Opus 4.5/4.6/4.7/4.8/5、Sonnet 4.6/5，含 `defaultThinking`、`supportsEffort/XHigh/Max`、budget↔effort 互转 |
| hosted 工具 | 散落在 `service/convert.go` 与 `relay/common/` | `toolconv/` 覆盖 10 种：`KindWebSearch`、`KindCodeExecution`、`KindComputerUse`、`KindFileSearch`、`KindImage`、`KindMCP`、`KindURLContext`、`KindWebFetch`、`KindFunction`、`KindNative` |
| 降级可观测性 | `ConversionDiagnostic` 本地零命中 | `convdiag` + `types.ConversionDiagnostic` |
| 转换质量分级 | 无 | `Good`/`Fair`/`Discouraged` + 4×4 矩阵 |
| 测试基线 | CI 不跑 `go test` | 24 个 golden 快照 + `-update` 机制 |

用户要求的"思考等级"在收敛后是**净增强**，不是妥协。

### 4. 待打的上游补丁只有 2 项

**补丁 1：Claude thinking block 映射与输入校验对齐**

上游两个请求转换器的 content block switch 均无 `thinking` case（`to_oai_chat_req.go:181`、`to_oai_responses_req.go:165`），且这两处 switch 无 `default`，未知 block 静默丢弃。需补：`case "thinking"`、非 assistant role 门控（本地有测试锁定）、`default:` 报错分支。

**补丁 2：DTO presence 保真**

实质是**把本地既有能力带进 relaykit**，非从零设计。本地已有 `UnmarshalJSON`、`HasIncludeUsageField`、`HasRemoveIncludeUsage`、`HasIncludeObfuscationField` 并用于生产。

### 5. 本地独有能力的处置

**保留在宿主**（部署关切）：响应方向 reasoning 暴露策略、DeepSeek 空 thinking block 保留、think 标签提取族、`context_compaction` 归一化、加密内容探测。

**本地专有契约**：本地 `types/error.go` 37 个 `ErrorCode` 中，上游 `relaykit/types/error.go`（33 个）缺 4 个：`ErrorCodeUpstreamTransportInterrupted`（生产 14 文件）、`ErrorCodeChannelConcurrencyExceeded`（3）、`ErrorCodeUpstreamRequestTooLarge`（3）、`ErrorCodeChannelConcurrencyUnavailable`（2）。另有 2 个本地独有函数 `IsUpstreamTransportInterruptedError`（生产 8）、`IsOpenAIRequestParameterError`（5）。

**本地 DTO 独有方法（23 个，HEAD 口径 20 个）**：`ParseContentStrict`(12)、`ParseToolCallsStrict`(11)、`HasCompactionTrigger`(9)、`ResponsesStreamResponse.GetOpenAIError`(9)、`EffectiveReason`(2)、`SearchToolNameByToolCallId`(1) 等。relaykit 对应类型无这些方法，迁移即编译失败。反向 relaykit 独有 5 个方法。

**本地独有包**：`service/openaicompat/`、`relay/common/responses_*.go`、`dto/` 若干文件。**`*WithError` 系列不是能力缺口**——上游转换器本就返回 error，4 个无 error 旧包装的生产调用点为 0。

### 5.1 盘点阶段的修正（2026-10-01）

第 4 节本应「阶段 0 前置」的本地独有能力盘点，在本次补充完成。盘点同时推翻了本节上方若干原有数字：

| 原表述 | 实测 |
| --- | --- |
| `ErrorCodeUpstreamTransportInterrupted` 25 文件 | 25 是**含测试**口径；生产 14。且「上游宿主 `types/` 没有 `error.go`」字面为真但误导——上游迁到了 `relaykit/types/error.go`，33 个常量与本地同名同值 |
| `IsUpstreamTransportInterruptedError` 9 | 含测试 9，生产 8 |
| 本地 DTO 独有方法 13 个 | **23 个**（工作区）/ 20 个（HEAD）。13 只是 `openai_request.go` 单文件数 |
| `ParseContentStrict` 20 / `ParseToolCallsStrict` 16 / `Clone` 13 / `HasCompactionTrigger` 10 / `HasCompactionOutput` 4 | 实测 12 / 11 / **1** / 9 / 2。`Clone` 的 13 把 `http.Request.Clone`、`http.Transport.Clone`、`common.ResponsesRequestMetadata.Clone` 一并计入 |
| 「本地 DTO 独有方法全部在生产使用」 | 不成立：`IsValidToolName`、`ToolNameMaxLength` 生产引用为 0 |
| 「`service/openaicompat/` 上游无此包」（不加限定） | 包确无，但**能力大部分有**——上游把 chat↔responses 兼容层放在 `relaykit/relayconvert/internal/{oai_chat,oai_responses}/`，并在宿主留了同名函数的 27 行转发 shim。零覆盖的只有命名空间工具、tool_search、custom tool bridge 门控与 rules 策略扩展 |
| 「`*WithError` 系列 relaykit 无对应签名」 | 上游 `ResponseConverterFunc` 本就返回 `(any, *dto.Usage, error)`；`WithError` 是本地命名遗留 |
| `relay/common/responses_stream_options_compat.go` | 该文件**不存在**，系误记 |
| 「工具名契约与别名还原属部署关切」 | 它是跨协议工具名映射，上游零覆盖，**属协议层** |

**新发现的高风险项**：`relay/common/responses_tool_names.go` 及其 2 个测试文件**未被 git 跟踪**，且其 3 个调用点（`relay/responses_handler.go:1123`、`relay/channel/openai/relay_responses.go:912`/`:1296`、`relay/common/relay_info.go`）**也全部位于未提交改动中**——在 HEAD 上该能力完全不存在。与 `CR-UNTRACKED-SOURCE-DEPENDENCY-2026-09-22.md` 同型但更严重：能力本体与全部调用点都未提交。

**未覆盖边界**（不得据此下结论）：`relay/channel/{claude,gemini,openai,aws}` 各通道内自有的转换辅助函数（仅核验了它们对 `service/convert.go` 的 6 个调用点）；`service/` 根目录其余本地专有文件；`controller/` 层协议相关逻辑；上游 3 个 relaykit 演进分支；`responses_input_filter.go` 第 646–1034 行的逐行细节。

## 被推翻的结论

以下是审查中经复现**判定为错误**的断言。部分由子代理提出，部分由主代理自己提出。列出以备用：

### 主代理自己引入、被下一轮抓出的错误

| 错误断言 | 实际 |
| --- | --- |
| 「`convmeta.Options` 是请求阶段机制、响应方向零先例」 | `response_registry.go:500` 的 `toolconv.AttachHostedResponse(..., convmeta.OptionsOf(info))` 即响应方向实例；`internal/` 下 9 处命中中 2 处在 `shared/` 而非 `*_req.go` |
| 「`info.Request` 从不被重新指向」 | `controller/channel-test.go:957` 的 `info.Request = chatReq` 是反例（渠道测试合成请求） |
| 「本地 `ResponsesStreamResponse` 的 `Code` 为 `any`」 | 本地**没有** `Code` 字段，是把 relaykit 字段名与本地类型名拼接 |
| 「relaykit 响应方向两个转换器都用 `_` 丢弃 Meta」 | 只有非流式（`:920`）如此；流式（`:1004`）实际调用 `info.EnsureClaudeConvertInfo()` |
| 「hosted 工具桥接是本地独有」 | 上游 `toolconv/` 在 main 上已存在，覆盖 10 种工具 |
| 「阶段 1 必须更新 210 个 import 点」 | 上游用类型别名做到零调用点变更 |
| 「`*WithError` 系列 39 个生产调用点」 | 无可复现口径；实测 4 个函数 9 个，6 个函数 14 个 |
| 「本地 DTO 有 13 个独有方法」（2026-10-01 盘点推翻） | 13 只是 `openai_request.go` 单文件数，实测 23（工作区）/ 20（HEAD） |
| 「`Clone` 有 13 个生产使用点」（同上） | 把 `http.Request.Clone`、`http.Transport.Clone`、`common.ResponsesRequestMetadata.Clone` 一并计入；dto 的 `Clone` 实际只 1 个生产文件 |
| 「上游宿主 `types/` 没有 `error.go`」（同上） | 字面为真但误导——上游迁到 `relaykit/types/error.go`，33 个常量与本地同名同值 |
| 「`*WithError` 系列 relaykit 无对应签名」（同上） | 上游 `ResponseConverterFunc` 本就返回 error；`WithError` 是本地命名遗留，旧包装生产调用点为 0 |
| 「`service/openaicompat/` 上游无此包」（同上，未加限定） | 包确无但能力大部分有——上游在 `relaykit/relayconvert/internal/{oai_chat,oai_responses}/`，宿主留了同名函数转发 shim |
| 「`relay/common/responses_stream_options_compat.go`」（同上） | 该文件不存在，系误记 |
| 「工具名契约与别名还原属部署关切」（同上） | 跨协议工具名映射，上游零覆盖，属协议层 |

### 主代理坚持多轮的错误结论

**「`dac55f0f` 不是 `HEAD` 与 `upstream/main` 的共同祖先」**——该断言从文档第一稿存活到第四稿，经历两轮专门的事实核验仍未被发现。实际：

```text
git cat-file -p 9724ef1b2   →  parent aa7d0d39a4
git merge-base HEAD aa7d0d39a4  →  dac55f0f
```

`dac55f0f` **是**真实共同祖先。`git merge-base HEAD upstream/main` 返回空纯属浅克隆（`.git/shallow` 截断于 `9724ef1b2`）遮蔽。原先用「两历史 `comm -12` = 0」作为证据是无效的——浅克隆下该结果恒真、无鉴别力。

### 被撤回的设计

**「按客户端意图门控 reasoning 暴露」**（原补丁 3）。三条理由：

1. 依据不成立。relaykit 自身的 `claudeCapabilitiesFor` 为 fable-5、mythos-5、opus-5、sonnet-5 置 `defaultThinking = true`——"未请求 thinking 就不应返回 thinking"不是协议不变量
2. 客户端意图在响应阶段取不到正确值。`relay/claude_handler.go:34` 对 Claude 请求做 `common.DeepCopy`，后缀适配器写入的 `request.Thinking`（`:58`、`:79`、`:91`）全部作用于拷贝。客户端发 `claude-opus-4-7-high` 时适配器让上游真的思考了，而 `info.Request.Thinking` 仍是 nil
3. relaykit 内不修改签名即不可达：Responses→Claude 非流式（`to_claude_messages_resp.go:15`）与流式（`to_claude_messages_stream_resp.go:82`）没有 `Meta` 参数

改为：响应方向不打 relaykit 补丁，策略落在宿主调用点。

## 响应方向的 6 条路径

此前文档只列 5 条，第 6 条（AWS Converse）是审查中发现的：

| 路径 | 基线位置 | 经 `service/convert.go` |
| --- | --- | --- |
| OpenAI 渠道流式 | `relay/channel/openai/helper.go:46` | 是 |
| OpenAI 渠道流式末帧 | `relay/channel/openai/helper.go:246` | 是 |
| OpenAI 渠道非流式 | `relay/channel/openai/relay-openai.go:314` | 是 |
| Chat 经 Responses | `relay/channel/openai/chat_via_responses.go:280` | 是 |
| Gemini 渠道 | `relay/channel/gemini/relay-gemini.go:1784` | 是 |
| AWS Converse | `relay/channel/aws/converse.go:748`、`:1112` | **否** |

`converseOutputToClaudeResponse`（`:713`）直接从 `ConverseOutput` 构造 `dto.ClaudeResponse` 并生成 thinking 块，绕过 `service/convert.go`。

## 核验命令与结果

以下均为只读命令，在基线 `9c4758b53` / `upstream/main` 上执行：

```bash
git show upstream/main:service/convert.go | wc -l            # 27
git show 9c4758b53:service/convert.go | wc -l                # 2229
git grep -l '"github.com/QuantumNous/new-api/types"' 9c4758b53 -- '*.go' | wc -l          # 210
git grep -l '"github.com/QuantumNous/new-api/relaykit/types"' upstream/main -- '*.go' | grep -vc '^upstream/main:relaykit/'  # 172
git grep -l '"github.com/QuantumNous/new-api/types"' upstream/main -- '*.go' | grep -vc '^upstream/main:relaykit/'          # 24
git ls-tree --name-only upstream/main dto/ | wc -l           # 8
git ls-tree --name-only upstream/main types/                 # 4 个文件
git ls-tree --name-only upstream/main relaykit/types/        # 8 个文件
git grep WithError upstream/main -- relaykit/                # 空
git grep -c "tool_search" upstream/main -- relaykit/relayconvert/   # 空
```

`convmeta.Meta` 15 个方法逐条核验无客户端请求访问器（`relaykit/relayconvert/convmeta/meta.go:18-54`）。

## 未决项（设计文档第 10 节，20 项）

原列为**阶段 0 前置**的一项已于 2026-10-01 完成：

1. ~~**完成本地独有能力盘点**~~。**已完成**，见设计文档 4.1–4.5.5 与第 11 节取证，本节 5.1 记录修正。剩余未覆盖边界已在设计文档 4.5.5 明确列出，不阻塞阶段 4。

本次盘点新增 5 项未决（设计文档第 10 节第 16–20 项）：

16. 命名空间工具与 tool_search 的归属（上游零覆盖，本地约 20 + 3 个符号）
17. custom tool bridge 门控语义取舍（本地默认拒绝 vs 上游无条件）
18. `matchAnyRegex` 空模式语义（本地匹配全部 vs 上游不匹配任何）
19. **未跟踪文件纳入版本控制**：`relay/common/responses_tool_names.go` 及 2 个测试文件，其调用点也未提交
20. `service/openaicompat/` 的收敛边界（上游覆盖部分与本地扩展部分如何切分）

其余主要未决项：think 标签提取族归属、本地专有错误符号归属（4 个常量）、`*WithError` 系列形式（倾向直接删除）、响应方向暴露策略具体形态、**23 个** DTO 独有方法归属、hosted 工具收敛方式、`types` 别名转发适用范围。

## 第五轮审查（2026-10-01）：架构攻击

第一轮审查代理因 API 503 中断后重新发起，两个角度：**事实核验**与**架构攻击**。架构攻击提出 6 条，主代理逐条独立复现，**6 条全部成立**——其中 4 条推翻了主代理在同一会话中刚写下的决策。

| # | 攻击 | 复现证据 | 推翻的决策 |
| --- | --- | --- | --- |
| 1 | `dto` 迁移规模被系统性低估 | 上游 **281** 个文件 import `relaykit/dto`（宿主 dto 327→39）；本地对应 **327** 个。第 6.2 节用整节撤回 `types` 的 210 import 点，对 dto 只字未提 | §12.0「调用点不变」 |
| 2 | 「23 个 DTO 独有方法」只数了 3 个文件 | 本地 dto **394 函数** vs relaykit **273**；`channel_settings.go` 单文件本地 61/relaykit 29，**40 个宿主独有方法**（主代理复现后把代理给的 32 修正为 40），全部有生产引用、上游 0 命中 | §4.4、§4.5.3、§12.5 |
| 3 | `dto` 与宿主包耦合未做 import 图 | 本地 **21 个 dto 文件 import `common`**、3 个 `constant`、1 个 `logger`；relaykit/dto 只 import `types`(17) 与 `kitutil`(12) | §12.0 的依赖方向 |
| 4 | `openaicompat` 无法按能力切分 | `convertResponsesToolsToChat`（`responses_to_chat_request.go:455`）签名同时收 `namespaceSelection`+`toolState`，体内 namespace 16/toolState 5/ToolSearch 5/Custom 4/门控 3 交织；上游 shim 无 state 变体；消费者还有第 3 个 `relay/responses_via_chat.go:104` | §12.15、§4.5.1 |
| 5 | 流式 thinking 门控是有状态的 | 本地 `convert.go:756` 算策略、`:801` 在分配索引前 `continue`；relaykit `to_claude_messages_resp.go:342-365` 无条件 `state.Index++`。`state` 私有，后置删块产生**索引空洞** | §12.4「后置过滤」、§6.1 结论 |
| 6 | AWS 路径套用策略会引入回归 | `aws/converse.go:746` **无条件**转 thinking，全文件 0 处调用 `shouldExposeReasoningAsClaudeThinking`（该策略只对 DeepSeek 为真） | §12.4 的落点描述 |

**另发现（主代理自查，非代理提出）**：主代理自己把 `IsValidToolName` / `ToolNameMaxLength` 当作「dto 独有方法」列入撤回清单，实为**包级符号**，不该出现在方法集比对里。

**验证策略缺口（代理提出，主代理确认）**：24 个 golden 快照是**上游自己的基线**，验证的是「上游行为未变」而非「本地行为未退化」；第 9.1/9.2 节的 12 处本地差异它一处都测不到。原「唯一的机械回归网」表述不成立。已在 §12.16 加入「补建本地 golden 基线」的决定。

**修正范围**：§4.4、§4.5.1、§4.5.3、§4.5.5、§6.1、第 7 节阶段 2、§12.0（重写为 12.0.1–12.0.3）、§12.4（重写为 12.4.1–12.4.3）、§12.5、§12.6、§12.15（重写为 12.15.1–12.15.3）、§12.16、§12.17、新增 §12.18（5 项新未决）、第 11 节（新增 15 条取证）、撤回清单（新增 10 条）。

**方法学教训**：本轮 4 条被推翻的决策有共同形态——**只看了局部就下全局结论**（3 个文件 → 「23 个方法」；2 个消费者 → 「爆炸半径极小」；单个 chunk → 「无状态」）。这与前四轮的教训一致：把 `types` 别名零改动的成功经验外推到了性质完全不同的 `dto`（常量 vs 类型）。

## 第五轮审查之二：事实核验（2026-10-01）

同一轮的第二位代理逐条复现第 12 节断言。结论：**大部分通过，6 处需修正**。

### 确认通过（代理逐条复现，与文档一致）

方法集差集（13+9+1=23 / HEAD 20 / relaykit 独有 5）、12.5 分类（A18/B2/待实测3）、4.3 错误码（37 vs 33，本地独有 4、上游独有 0，引用数 14/3/3/2 与 8/5）、12.10（`relay_info.go:311` 确为 `info.convOptions = nil`）、12.16（golden 24 个全 .json）、12.8（空模式语义双向确认）、12.9 日期与祖先判断、4.5.2 未跟踪文件（3 个 `??`，3 个调用点 HEAD 上均不存在）、4.5.1 全部零命中（`namespace` 四作用域各 0、`tool_search`/`EnableCustomToolBridge`/`ProtocolConversionRule`/`synthetic.compact`/`encrypted_content`/`NormalizeToolName`/`context_compaction` 全仓 0）、第 6/8/9 节全部行号、12.4.1（`convert.go:756`/`:801` 与 relaykit `:348/366`）、12.4.3（AWS 无条件、策略即 `hasDeepSeekModelName`）、12.15.1（函数签名与符号分布）、第 2 节全部数字、12.0.1（281/39/327、394/273、61/29）。

**循环依赖**：阶段 0→6 未见硬环。`relaykit/types/error.go` import `kitutil`，而 kitutil 不 import relaykit/types。

### 需修正（主代理逐条复现后确认）

| # | 问题 | 实测 | 处置 |
| --- | --- | --- | --- |
| 1 | 第 7 节阶段表与第 12 节决策矛盾 | 阶段 4 写「`*WithError` 在宿主包装」、阶段 5 写「落到 6 条调用点」，与 12.3「删除」、12.4「relaykit 内门控」相反 | 已同步阶段表 |
| 2 | 12.9「main 已含最终形态」缺逐项取证 | 6 个提交**全部**不在 main（`--is-ancestor` 全 exit=1）。原结论仅凭「非祖先」+「toolconv 10 个 Kind」——**不充分** | 已补逐字节/符号集比对（toolconv 5 SAME + 2 处 main 侧清理；`reasoning/claude.go` 双向独有符号 0；relaykit 整体 main 净增 4899 行）。结论仍成立，但取证是后补的 |
| 3 | 4.5.1「3661 行」无出处 | 实测 **3071 行**（含测试 5280）。高估 19% | 已改为 3071 |
| 4 | 12.0.3 的 dto import 计数口径矛盾 | 非测试是 **12/2/1**；21/3/1 是含测试口径，与文档自述「排除 `_test.go`」冲突 | 已改为非测试口径并注明含测试值 |
| 5 | `HasCompactionTrigger` 写 9 | 提及口径确为 **9**（代理说 10 是把它算进了 `dto/` 自身）；调用口径 7 | 数字正确，已注明口径 |
| 6 | 12.9「−3910 行」未限定作用域 | 该数是 **relaykit 子目录**口径；全仓为 `1251 files changed, +35412/−151592` | 已注明 |

另修正 2 处口径表述：4.5.2 的文件数标注「含测试/非测试」；12.6 的「18 个生产点」无可复现口径，改为定性表述并列为新未决项 26。

### 新增未决项

| # | 项 |
| --- | --- |
| 26 | 命名空间字段的生产引用精确计数 |

## 边界与未验证项

- 本记录及设计文档**未修改任何生产代码**，未运行 Go 编译或测试。所有"编译失败"结论均为符号存在性推导，未经 `go build` 复现
- 未实测 `service/convert.go` 的转换测试在阶段 4 是否失败（需真实运行 `go test ./service`）
- 「适配器写入拷贝后 `info.Request.Thinking` 仍为 nil」是代码阅读推断，与文档同源，非独立运行时复现
- thinking block 的 `signature`：relaykit 响应方向产出的 thinking block 只有 `{Type, Thinking}`，不含 `signature`；本地现状同样不生成。Anthropic 是否校验签名、客户端是否接受无签名 block，未验证
- 上游 relaykit 仍在演进，存在 `feat/relaykit-tools`、`feat/relaykit-perf`、`fix/relayconvert-tool-result-media-and-reasoning-parts` 等分支，未逐一核验
- 未执行阶段 0 要求的 `git fetch --unshallow upstream`（属修改仓库状态的操作）

## 设计文档提交记录

| 提交 | 内容 |
| --- | --- |
| `81979fa54` | 初稿 |
| `df919efdb` | 第一轮修正：撤回原补丁 3、补丁 1 扩充、阶段重排 |
| `d8beee22e` | 第二轮：补丁 3 改为宿主调用点、补丁 3→2、think 标签族 |
| `c046c93cc` | 第三轮：2 处实质性错误 + 行号/计数修正 + AWS Converse 路径 |
| `dbfbc096f` | 第四轮：上游已完成迁移、别名机制、hosted 工具 |
| `53af20dc5` | 重构为上游对照清单 |
| `a1d5e9aad` | 修正交叉引用编号 |

文档当前 348 行，位于 `docs/superpowers/specs/2026-09-30-relaykit-migration-design.md`。
