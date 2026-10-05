# CR: 协议转换层收敛到上游 relaykit 的设计与十七轮对抗审查

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

五轮对抗性审查，每轮更换角度而非重复（第五轮的事实核验部分在架构攻击之后才完成，其 6 处修正见下）；其后又对设计文档做过 16 次独立复核（见「设计文档提交记录」的末 16 行）：第 6–8 轮转向此前未查区块的事实核验（第 8 轮零发现），第 9–10 轮转向**执行层**，补齐阶段 6 矛盾、relaykit 源码获取、`IsStream` 影响面、回滚止损点，以及阶段表逐条走查找出的 6 处引用面问题；第 11 轮转向**阶段间的数据流**，发现 `boundary_test.go` 是阶段 2 的隐性硬门禁；第 12 轮为全维度复核；第 13 轮把文档自认的「未决/未核验」逐条做成结论，发现 459 行遗漏转换器；第 14 轮补测字段 tag 与 Go 类型两个维度，推翻上一轮刚写的「17 个纯增益」；第 15 轮把四维比较工具化，工具又抓出第 14 轮两处分类错误；第 16 轮把三个「待定」逐项取证后关闭，未决项由 6 项降至 3 项；第 17 轮决定 tag/类型差异 15 处全部采用 relaykit，未决项降至 2 项。

| 轮次 | 子代理数 | 角度 |
| --- | --- | --- |
| 1 | 1 | 事实核验 + 架构攻击 |
| 2 | 2 | 补丁方案可达性；阶段划分与事实表 |
| 3 | 3 | 架构选择是否最优；宿主调用点方案可行性；增量事实核验 |
| 4 | 2 | 阶段可执行性；本地独有能力完整性 |
| 5 | 2 | 架构攻击（决策可执行性、依赖顺序、验证策略缺口）6 条全部复现成立；事实核验（逐条复现第 12 节断言）大部分通过、6 处需修正 |

主代理对每条代理结论独立复现后才采纳。**每一轮都抓出主代理在上一轮刚引入的错误**：6 → 3 → 2 → 3 处（第 1–4 轮），第 5 轮另修正 6 处事实错误，第 6 轮再修正 3 处（含 1 处主代理自己引入的），第 7 轮再修正 5 处，第 8 轮零发现，第 9 轮补执行层 4 处缺口（其中 2 处为主代理当轮自己引入并自查修正），第 10 轮再找出 6 处阶段表引用面问题，第 11 轮再找出 1 处跨阶段数据流缺口（`boundary_test.go`），第 12 轮（全维度复核）推翻第 7 轮写入的一处计数（内建转换器 8→12），第 13 轮关闭 3 项未决并新发现 1 处 459 行的遗漏转换路径，第 14 轮在「已完成」的结论上补出 13 处此前未比的 tag/类型差异，第 15 轮把它固化成带 `--strict` 门禁的可重跑脚本，第 16 轮关闭 27/28/29 三项未决，第 17 轮关闭第 30 项。

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
| 思考等级渲染 | `relay/claude_handler.go:55-104` 硬编码 `claude-opus-4-6/4-7` 前缀 | `reasoning.RenderClaude` + `claudeCapabilitiesFor`（`:35-78`）覆盖 10 个模型前缀：fable-5、mythos-5、mythos-preview、Opus 4.5/4.6/4.7/4.8/5、Sonnet 4.6/5，含 `defaultThinking`、`supportsEffort/XHigh/Max`、budget↔effort 互转 |
| hosted 工具 | 散落在 `service/convert.go` 与 `relay/common/` | `toolconv/` 覆盖 10 种：`KindWebSearch`、`KindCodeExecution`、`KindComputerUse`、`KindFileSearch`、`KindImage`、`KindMCP`、`KindURLContext`、`KindWebFetch`、`KindFunction`、`KindNative` |
| 降级可观测性 | `ConversionDiagnostic` 本地零命中 | `convdiag` + `types.ConversionDiagnostic` |
| 转换质量分级 | 无 | `Good`/`Fair`/`Discouraged` 三级；4 个文本格式间 12 个内建转换器（覆盖全部 12 个有序对，即 6 双向对，无单向）各自标注 quality |
| 测试基线 | CI 不跑 `go test` | 24 个 golden 快照 + `-update` 机制 |

用户要求的"思考等级"在收敛后是**净增强**，不是妥协。

### 4. 待打的上游补丁只有 2 项

**补丁 1：Claude thinking block 映射与输入校验对齐**

上游两个请求转换器的 content block switch 均无 `thinking` case（`to_oai_chat_req.go:181`、`to_oai_responses_req.go:165`），且这两处 switch 无 `default`，未知 block 静默丢弃。需补：`case "thinking"`、非 assistant role 门控（本地有测试锁定）、`default:` 报错分支。

**补丁 2：DTO presence 保真**

实质是**把本地既有能力带进 relaykit**，非从零设计。本地已有 `UnmarshalJSON`（`StreamOptions`、`OpenAIResponsesRequest`、`Reasoning`、`OpenAIResponsesCompactionRequest` 四个类型）、`HasIncludeUsageField`、`RemoveIncludeUsage`、`HasIncludeObfuscationField` 并用于生产。

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
| 「`service/openaicompat/` 上游无此包」（不加限定） | 包确无，但**能力大部分有**——上游把 chat↔responses 兼容层放在 `relaykit/relayconvert/internal/{oai_chat,oai_responses}/`，并在宿主留了 30 行转发 shim（6 个函数，5 个与本地同名，第 6 个对应本地同能力的 `responsesIncompleteFinishReason`）。零覆盖的只有命名空间工具、tool_search、custom tool bridge 门控与 rules 策略扩展 |
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
| 「4 个文本格式间的 8 个内建转换器（6 双向对 + 2 单向）」（第七轮引入，第十二轮推翻） | 实为 12 个（覆盖全部 12 个有序对，6 双向对、0 单向）；只数了 `Converter*` 命名，漏了 4 个 `requestConverter*` 条目 |

### 主代理坚持多轮的错误结论

**「`dac55f0f` 不是 `HEAD` 与 `upstream/main` 的共同祖先」**——该断言从文档第一稿存活到第四稿，经历两轮专门的事实核验仍未被发现。实际：

```text
git cat-file -p 9724ef1b2   →  parent aa7d0d39a4
git merge-base HEAD aa7d0d39a4  →  dac55f0f
```

`dac55f0f` **是**真实共同祖先。`git merge-base HEAD upstream/main` 返回空纯属浅克隆（`.git/shallow` 截断于 `9724ef1b2`）遮蔽。原先用「两历史 `comm -12` = 0」作为证据是无效的——浅克隆下该结果恒真、无鉴别力。

### 被撤回的设计

**「按客户端意图门控 reasoning 暴露」**（原补丁 3）。三条理由：

1. 依据不成立。relaykit 自身的 `claudeCapabilitiesFor` 为 fable-5、mythos-5、mythos-preview、opus-5、sonnet-5 置 `defaultThinking = true`——"未请求 thinking 就不应返回 thinking"不是协议不变量
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
| AWS Converse | `relay/channel/aws/converse.go:748`、`:1110` | **否** |

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

## 未决项（设计文档第 10 节 20 项 + 12.18 新增 6 项 = 26 项）

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
| 2 | 「23 个 DTO 独有方法」只数了 3 个文件 | 本地 dto **394 函数** vs relaykit **273**；`channel_settings.go` 单文件本地 61/relaykit 29（`^func` 口径），**42 个本地独有方法**（主代理复现后把代理给的 32 修正为 41，第六轮再修正为 42；早期 41 漏算 1 个，见下）。引用范围：33 个跨包生产、6 个仅 dto 包内、2 个仅测试 | §4.4、§4.5.3、§12.5 |
| 3 | `dto` 与宿主包耦合未做 import 图 | 本地 **12 个非测试 dto 文件 import `common`**、2 个 `constant`、1 个 `logger`（含测试口径 21/3/1）；relaykit/dto 只 import `types`(17) 与 `kitutil`(12) | §12.0 的依赖方向 |
| 4 | `openaicompat` 无法按能力切分 | `convertResponsesToolsToChat`（`responses_to_chat_request.go:455`）签名同时收 `namespaceSelection`+`toolState`，体内 namespace 16/toolState 5/ToolSearch 5/Custom 4/门控 3 交织；上游 shim 无 state 变体；消费者还有第 3 个 `relay/responses_via_chat.go:104` | §12.15、§4.5.1 |
| 5 | 流式 thinking 门控是有状态的 | 本地 `convert.go:749` 算策略、`:794` 在分配索引前 `return`（闭包提前返回）；relaykit `to_claude_messages_resp.go:151` 在 `stopOpenBlocksAndAdvance` 内无条件 `state.Index++`，`:342` 起无条件建块。`state` 私有，后置删块产生**索引空洞** | §12.4「后置过滤」、§6.1 结论 |
| 5b | 12.4 补丁落点的可达性被高估（本记录后续更新时补记） | `oai_chat/to_claude_messages_resp.go:111`/`:449` **已接收 `info convmeta.Meta`**，且 `Meta` 已含 `ConvOptions()`；本地只有 Chat→Claude 两条路径，无 Responses→Claude。故门控补丁**零透传成本**，原文按「需向内部转换器透传」描述是错的 |
| 6 | AWS 路径套用策略会引入回归 | `aws/converse.go:748` **无条件**转 thinking，全文件 0 处调用 `shouldExposeReasoningAsClaudeThinking`（该策略只对 DeepSeek 为真） | §12.4 的落点描述 |

**另发现（主代理自查，非代理提出）**：主代理自己把 `IsValidToolName` / `ToolNameMaxLength` 当作「dto 独有方法」列入撤回清单，实为**包级符号**，不该出现在方法集比对里。

**验证策略缺口（代理提出，主代理确认）**：24 个 golden 快照是**上游自己的基线**，验证的是「上游行为未变」而非「本地行为未退化」；第 9.1/9.2 节的 12 处本地差异它一处都测不到。原「唯一的机械回归网」表述不成立。已在 §12.16 加入「补建本地 golden 基线」的决定。

**修正范围**：§4.4、§4.5.1、§4.5.3、§4.5.5、§6.1、第 7 节阶段 2、§12.0（重写为 12.0.1–12.0.3）、§12.4（重写为 12.4.1–12.4.3）、§12.5、§12.6、§12.15（重写为 12.15.1–12.15.3）、§12.16、§12.17、新增 §12.18（5 项新未决）、第 11 节（新增 15 条取证）、撤回清单（新增 10 条）。

**方法学教训**：本轮 4 条被推翻的决策有共同形态——**只看了局部就下全局结论**（3 个文件 → 「23 个方法」；2 个消费者 → 「爆炸半径极小」；单个 chunk → 「无状态」）。这与前四轮的教训一致：把 `types` 别名零改动的成功经验外推到了性质完全不同的 `dto`（常量 vs 类型）。

## 第五轮审查之二：事实核验（2026-10-01）

同一轮的第二位代理逐条复现第 12 节断言。结论：**大部分通过，6 处需修正**。

### 确认通过（代理逐条复现，与文档一致）

方法集差集（13+9+1=23 / HEAD 20 / relaykit 独有 5）、12.5 分类（A18/B2/待实测3）、4.3 错误码（37 vs 33，本地独有 4、上游独有 0，引用数 14/3/3/2 与 8/5）、12.10（`relay_info.go:311` 确为 `info.convOptions = nil`）、12.16（golden 24 个全 .json）、12.8（空模式语义双向确认）、12.9 日期与祖先判断、4.5.2 未跟踪文件（3 个 `??`，3 个调用点 HEAD 上均不存在）、4.5.1 全部零命中（`namespace` 四作用域各 0、`tool_search`/`EnableCustomToolBridge`/`ProtocolConversionRule`/`synthetic.compact`/`encrypted_content`/`NormalizeToolName`/`context_compaction` 全仓 0）、第 6/8/9 节全部行号、12.4.1（`convert.go:749`/`:794` 与 relaykit `:151`/`:342`）、12.4.3（AWS 无条件、策略即 `hasDeepSeekModelName`）、12.15.1（函数签名与符号分布）、第 2 节全部数字、12.0.1（281/39/327、394/273、61/29）。

**循环依赖**：阶段 0→6 未见硬环。`relaykit/types/error.go` import `kitutil`，而 kitutil 不 import relaykit/types。

### 需修正（主代理逐条复现后确认）

| # | 问题 | 实测 | 处置 |
| --- | --- | --- | --- |
| 1 | 第 7 节阶段表与第 12 节决策矛盾 | 阶段 4 写「`*WithError` 在宿主包装」、阶段 5 写「落到 6 条调用点」，与 12.3「删除」、12.4「relaykit 内门控」相反 | 已同步阶段表 |
| 2 | 12.9「main 已含最终形态」缺逐项取证 | 6 个提交**全部**不在 main（`--is-ancestor` 全 exit=1）。原结论仅凭「非祖先」+「toolconv 10 个 Kind」——**不充分** | 已补逐字节/符号集比对（toolconv 5 SAME + 2 处 main 侧功能新增；`reasoning/claude.go` 双向独有导出符号 0；relaykit 整体 main 净增 `3910+/746−`）。结论仍成立，但取证是后补的 |
| 3 | 4.5.1「3661 行」无出处 | 实测基线 **3072 行**（含测试 5280；工作区因其他会话改动为 3071）。高估 19% | 已改为 3072 |
| 4 | 12.0.3 的 dto import 计数口径矛盾 | 非测试是 **12/2/1**；21/3/1 是含测试口径，与文档自述「排除 `_test.go`」冲突 | 已改为非测试口径并注明含测试值 |
| 5 | `HasCompactionTrigger` 写 9 | 提及口径确为 **9**（代理说 10 是把它算进了 `dto/` 自身）；调用口径 7 | 数字正确，已注明口径 |
| 6 | 12.9「−3910 行」未限定作用域 | 该数是 **relaykit 子目录**口径；全仓为 `1251 files changed, +35412/−151592` | 已注明 |

另修正 2 处口径表述：4.5.2 的文件数标注「含测试/非测试」；12.6 的「18 个生产点」无可复现口径，改为定性表述并列为新未决项 26。

### 新增未决项

| # | 项 |
| --- | --- |
| 26 | 命名空间字段的生产引用精确计数 |

## 第六轮复核：此前未查区块（2026-10-02）

前五轮集中在第 3、12 节与第 4 节盘点。本轮换角度，系统核此前未被任何一轮覆盖的区块：§4.1、§4.2、§4.3、§4.5.2、§5、§6、§7、§9、§11、§12.7–§12.17。**结论：绝大多数断言复现一致，另有 3 处需修正**，其中 1 处是主代理在第五轮自己引入的。

### 复现一致（本轮实测）

- **§4.2 的 12 个 `service/convert.go` 行号**（`:526`、`:749`、`:1049`、`:365`、`:425`、`:475`、`:485`、`:489`、`:554`、`:597`、`:629`、`:103`）在基线 `HEAD` 上逐行命中。注意**工作区版本已被其他会话改动，行号全部漂移**——核验必须走 `git show HEAD:service/convert.go`
- **§4.3 错误码**：AST 提取本地 37 / 上游 33，本地独有 4（`UpstreamTransportInterrupted`、`ChannelConcurrencyExceeded`、`UpstreamRequestTooLarge`、`ChannelConcurrencyUnavailable`），上游独有 0；引用数 14/3/3/2 与 8/5 均复现（口径含定义文件 `types/error.go` 自身）
- **§4.5.2**：`responses_input_token_meta.go` 462 行、27 个方法、导出 0 个；`responses_tools.go` 111 行，`EffectiveTools` 有 9 个生产引用文件，扣除 `dto/` 自身（§4.4 声明的口径）后为 8
- **§5 / §6.1**：`relay/claude_handler.go:34` 是 `common.DeepCopy`，后缀适配块 `:55-104`，`:58`/`:79`/`:91` 三处 `request.Thinking`；文件内 `info.Request = ` 计数为 0，全仓唯一非测试反例是 `controller/channel-test.go:957`；`genBaseRelayInfo` 本地 `:670` 定义/`:701` 赋值、上游 `:562`/`:595`；`oai_responses` 两个响应转换器确实无 `Meta` 参数，`oai_chat` 的 `:111`/`:449` 确实接收 `convmeta.Meta`
- **§7 / §12.15**：`convertResponsesToolsToChat` 函数体 115 行、循环 93 行，符号计数 `namespace` 16 / `toolState` 5 / `ToolSearch` 5 / `Custom` 4 / `EnableCustomToolBridge` 3；上游 `ResponsesRequestToChatCompletionsRequest` 签名无 namespace/toolState；第 3 个消费者 `relay/responses_via_chat.go:104` 与 4 处 `toolState.Resolve` 均存在
- **§9.1 / §9.2**：两表分别为 6 条与 7 条；§9.2 的 6 个 `service/convert.go` 行号（`:355`、`:252`、`:282`、`:234`、`:246`、`:287`）逐行命中；上游 `internal/claude_messages/to_oai_chat_req.go:189` 的 `case "image"` 在 `:190` 直接解引用 `mediaMsg.Source`（nil panic），而 `to_oai_responses_req.go:315` 的 `claudeSourceURL` 有 nil 守卫——与文档一致
- **§12.7–§12.14、§12.16、§12.17**：`matchAnyRegex`（本地 `service/openaicompat/regex.go:10`）与 `matchAnyModelPattern`（上游 `service/openai_chat_responses_mode.go:15`）并存；上游 `relay_info.go:311` 的 `info.convOptions = nil` 及其原文注释；本地 `stopReasonOpenAI2ClaudeStrict`（`service/convert.go:1125`）；`case "thinking"` 只在「文本空且签名非空」时报错；`setting/model_setting/global.go:31` 的 `UnmarshalJSON` 存在性判定；上游 24 个 golden 快照（4/8/12 分目录、无 error/reject 场景）；`ImageGenerationCallCounter.Observe` 的四别名去重；本地 `HasImageGenerationCall` 的 4 个调用点；§12.16 的「12 处」= 9.1 的 6 条 + 9.2 的 6 条（9.2 第 3 行上游有守卫，不计）
- **§11 事实表**：上游 `service/convert.go` 27 行、本地 2229 行；`openai_chat_responses_compat.go` 30 行 6 个导出函数（第 6 个确为 `ResponsesFinishReasonFromStatus`）；本地 10 个非测试 dto 文件 import gin；makefile 无 `test` 目标；上游 `relay/responses_request.go` 87 行；`convmeta.Meta` 15 个方法（含 `ConvOptions()`）；`relaykit/types` 宿主 import **172 文件**（不含 `relaykit/` 自身的 49 个；含则为 221）；relaykit 下某包 262；上游 relaykit 34 个测试文件 / 8430 行 / 2 处 TODO
- **§12.0.x**：281 / 39 / 327（本地基线；工作区 340）；394 / 273（`^func`）；61 / 29；12 / 2 / 1（非测试）与 21 / 3 / 1（含测试）；`ChannelOtherSettings` 本地独有字段基线 20 / 工作区 21

### 需修正（本轮，3 处）

| # | 问题 | 实测 | 处置 |
| --- | --- | --- | --- |
| 1 | `channel_settings.go` 本地独有方法写 **41**（§4.4、§4.5.3、§12.0.2、§11 两行、§12.17 汇总，共 6 处） | AST 按 `接收者.方法名` 差集为 **42**（`A=43 B=15`，双向均含未导出方法）。41 是第四轮从代理的 32 修正时的漏算，此后各轮均照抄 | 6 处全部改为 42 |
| 2 | 「42 个方法上游全仓 0 命中」的**计数方法**不成立 | 纯名字口径 `git grep -cw IsNative` 在上游有 3 处（`AdvancedCustomRoute.IsNative`、`relay/responses_websocket.go` 等），属**同名异接收者串味**。带接收者口径下 42/42 确为 0 | 三处改为「按 `接收者.方法名` 口径」并注明纯名字口径的误命中 |
| 3 | §12.9 `billing_usage.go` 行写「diff 方向为 main 净增」 | 该文件 numstat 是 `25+/38−`，**净减 13 行**；同行的 `usage_merge.go` `52+/7−` 才是净增。且原表述「分支独有导出符号 0、main 独有 0」对 `usage_merge.go` 不成立——main 独有导出符号 `MergeClaudeUsageNonZero` | 拆开两文件分别表述；`billing_usage.go` 改为「纯函数体改写，函数集双向完全相同，导出符号 18/18」；`usage_merge.go` 改为「main 独有导出符号 1 个（`MergeClaudeUsageNonZero`，即分支未导出实现的导出化）」

**方法学教训（第六轮）**：本轮 3 处错误**没有一处是数字本身抄错**——第 1 处是**差集漏算 1 个**（两侧都含未导出方法，机械对比不会漏，漏的是人工转录），第 2 处是**计数方法未随断言一起声明**（「全仓 0 命中」在名字口径下为假、接收者口径下为真，文档只写了结论没写口径），第 3 处是**同一单元格里的两个对象被合并描述**（两个文件的 diff 方向相反，却共用一句「main 净增」）。这与前五轮的教训同源：**每个数字都要在其对象上、以其口径独立复现**，且**一个单元格只放一个可证伪的断言**。

## 第七轮复核：§3、§8、§12.4 与审计记录自身（2026-10-02）

转向仍未系统核过的区块：§3.1、§3.2、§4.5.3、§4.5.5、§6.1、§7、§8、§12.4.1–§12.4.3、§12.5、§12.14，以及**审计记录自身的能力表**。先复验第六轮的三处改动（`billing_usage.go` 方法集 4/4、导出符号 18/18；`usage_merge.go` main 独有 `MergeClaudeUsageNonZero`）——全部成立。

### 复现一致（本轮实测）

- **§8 的 8 个行号**（六条路径 + AWS 的 `:748`/`:1110`/`:713`）在基线 `HEAD` 上逐行命中
- **§12.4.1 的代码片段**与 relaykit 原文逐行一致（`:342` 起、`:346-357` 建块、`:359-362` delta）
- **§12.4.3**：`relay/channel/aws/converse.go` 全文件 `shouldExposeReasoningAsClaudeThinking` 调用数为 **0**；`shouldExposeReasoningAsClaudeThinking` 实测就是 `hasDeepSeekModelName`
- **§12.14**：3 个未跟踪文件（`responses_tool_names.go` + 2 个测试）状态为 `??`；其生产调用点 `relay/responses_handler.go:1123`、`relay/channel/openai/relay_responses.go:912`/`:1296`、`relay/common/relay_info.go:250` 的 `ResponsesToolNameRestorer` 字段**在 HEAD 上全部不存在**——「该能力在 HEAD 完全不存在」成立
- **§3.2**：`service/convert.go` 恰为 **13 个导出函数**，与列出的 13 个名字逐一对应；`NormalizeCacheCreationSplit` 在 relaykit 有 4 处定义（`oai_chat/to_claude_messages_resp.go:107`、`shared/claude/cache.go:3`、`response_compat.go:34`、宿主 `service/convert.go:9`）
- **§4.5.3**：11 个本地独有 dto 文件（排除上游宿主 `dto/` 的 `midjourney.go`/`suno.go`/`task.go`/`video.go` 后正好 11）；表内 11 个行数与 9 个符号引用数全部复现（口径：排除 `_test.go`、定义文件、`dto/` 包内）
- **§4.5.5**：`responses_input_filter.go` 共 1034 行，`646–1034` 范围成立；`aws/converse.go:713` 精确命中
- **§12.5**：表内 16 个生产引用数全部精确复现（12/11/9/9/3/2/2/2/2/2/2/1/1/1/1/1）；分类 **A 18 / B 2 / 待实测 3 = 23**；`HasImageGenerationCall` 的 4 个调用点；relaykit 独有 5 个方法逐项一致
- **§4.4**：「HEAD 与工作区差 3 个方法」实测为 `ChatCompletionsStreamResponse.GetError`、`ResponsesStreamResponse.GetOpenAIError`、`Usage.EnsureResponsesTokenDetailObjects`，与文档列出的三个完全一致
- **§7**：本地 `ClaudeConvertInfo` 的 `ToolCallStarted map[int]bool` 等字段在 `service/convert.go` 的引用数为 6/5/5/6 = 22

### 需修正（本轮，5 处）

| # | 问题 | 实测 | 处置 |
| --- | --- | --- | --- |
| 1 | §12.4.1 称 `state` 是「relaykit 的**私有内部状态**（`ResponseStreamState` 的字段不导出）」 | `state` 是 `info.EnsureClaudeConvertInfo()` 返回的 `*convmeta.ClaudeConvertInfo`（`convmeta/meta.go:58`），**字段全部导出**且经 `convmeta.Meta` 对宿主可达。真正私有的 `ResponseStreamState` 是另一个类型（`response_registry.go`），不是这里讨论的对象 | **结论对、论据错**：索引空洞成立，但理由改为「宿主读得到 `state.Index`，却无法重排已分配的索引」 |
| 2 | §3.1 称 `claudeCapabilitiesFor` 覆盖 9 个模型（漏 `claude-mythos-preview`），行号写 `:35-74` | 实测 **10 个模型前缀**，函数结束于 `:78` | 两处改为 10 个前缀 + `:35-78`；§11 同行号同步 |
| 3 | §3.1 称转换质量分级含「4×4 转换矩阵」 | 实际是 4 个文本格式间的 **12 个内建转换器**（`builtinTextConverters`，覆盖全部 12 个有序对，即 6 个双向对，无单向），非 16 格矩阵 | 改为可复现表述；审计记录第 83 行同步 |
| 4 | §12.0.1「事实一」称 `constant/finish_reason.go` 用 `type X = types.X` | 该文件用的是 **`var FinishReasonStop = types.FinishReasonStop`**（变量别名），**无任何 `type` 声明**；只有 `endpoint_type.go` 用类型别名 | 改为「两处手法不同」；§11 第 502 行同步 |
| 5 | §7 写 `dto/gemini.go:111` 用 `c.Query("alt")` | 实测 `:112`（两个基线口径一致） | 改为 `:112` |

**方法学教训（第七轮）**：本轮 5 处里，**第 1 处是唯一实质性的**——它属于「**结论正确但论据被替换**」：索引空洞的结论没错，但支撑它的那句「宿主拿不到 state」是错的，而这句话恰好**削弱**了论证（若宿主真拿不到 state，反而说明后置过滤连读都读不到，论证会更强）。第 2–5 处是**枚举不全**（漏一个模型）与**行号/机制名称错**（`type` 别名 vs `var` 别名、4×4 vs 8 个转换器）。这与前几轮合起来指向同一条：**凡「覆盖 N 个」的枚举，必须逐个列出并计数**；凡「用 X 机制」的断言，必须打开文件看第一行关键字。

## 第八轮复核：§3.3 补丁规格与跨节一致性（2026-10-03）

**结论：零修正。** 这是八轮里第一次整轮未发现错误。

本轮先复验第七轮的三处改动（`claudeCapabilitiesFor` `:35-78` 与 10 个模型、`finish_reason.go` 无 `type` 声明、`gemini.go:112`）——全部成立。随后转向**八轮里唯一没有系统核过的区块：§3.3 的两个补丁规格**，并做了一次跨节数字一致性扫描。

复现一致：补丁 1 的两个 switch（`to_oai_chat_req.go:181` 4 个 case、`to_oai_responses_req.go:165` 5 个 case）均**无 `thinking` case、无 `default`**，文档引用的「同文件其他 switch 有 default」也对（`:269`/`:265`）；补丁 1 第 2 项的两个测试名在 `service/convert_claude_request_test.go:221/257` 存在；补丁 2 的 4 个 `UnmarshalJSON` 与 `json_extra_fields.go` 的 4 个未导出函数；`PreserveThinkingSuffix` 钩子（`convmeta/options.go:29`，宿主 `relay_info.go:943` 填充，`Options` 内另有 2 个同型钩子）；§12.0.1 的**基线 33 / 工作区 34**（差额确为 `ChatCompletionsStreamResponse`）与全部本地独有/relaykit 独有字段清单（`ChannelOtherSettings` 20、`OpenAIResponsesCompactionRequest` 20、`GeneralOpenAIRequest` 20、`ResponsesOutput` 15、`OpenAIResponsesRequest` 13 等）；§12.8 的空 patterns 语义差异（本地 `true`、上游 `false`，其余逐行同构）。跨节一致性：`327`（9 处）、`281`（3 处）、`136`（4 处）、`172`（3 处）语境全部一致，`210`（5 处）全在「已撤回」语境中。

**本轮的性质**：零修正有两种解释——文档确实收敛，或反复查同一批区块已饱和。为区分，本轮特意选了 §3.3 与跨节一致性这两个此前从未查过的角度，仍零错误，支持前一种解释。

## 第九轮：执行层补完（2026-10-03）

前八轮全部针对**事实层**（文档说的与代码是否相符）。本轮转向**执行层**——即「照着这份文档能否做完」。结论：事实层已收敛，执行层有 4 处缺口，全部补齐。

| # | 缺口 | 补法 |
| --- | --- | --- |
| 1 | **阶段 6 与 §12.15.3 直接矛盾**：阶段 6 写「删除 `service/openaicompat/` 中被替代部分」，而 §12.15.3/§12.17 #20/§10 #20 三处都说该包「整体保留、行数不减少」 | 阶段 6 改为「**仅**删除 `service/convert.go` 主体；`openaicompat/` 不删」，并注明理由与完成判据 |
| 2 | **阶段 1 缺最关键的步骤：relaykit 源码从哪来**。仓库当前无 `relaykit/`，`HEAD` 也没有；上游是在 `86ac0f774` 引入的，该提交不是本地 `HEAD` 的祖先，无法靠本地历史恢复 | 新增「阶段 1 的前置」：整体拷贝 `upstream/main:relaykit/`（157 文件 / 35198 行，**零宿主 import**，可整体搬）；给出上游接入方式（`go.mod:176,179`）；**要求把 `upstream/main` = `789c97019` 锚定**，否则全文行号失效；列出依赖影响（`samber/lo` v1.52→v1.53、`tidwall/gjson` v1.18→v1.19 经 MVS 抬高） |
| 3 | **`go.work` 未提及**；`IsStream` 迁移影响面被低估（只举了 1 个理由） | 明确「不引入 `go.work`」及理由；新增「阶段 2 的前置」，列出 `IsStream` 的 12 个定义 / 8 个文件 / 5 个调用点 / 10 个 gin import |
| 4 | **无任何回滚与止损点** | 新增 §8.1：三层止损（阶段内 / 阶段间 / 发布），要求阶段 4 与 5 各自独立成一次合并、阶段 4 必须单点可逆；声明本迁移**无不可逆项**（不涉 schema 与数据）；明确**不做双跑**及其理由；发布层复用既有的 `docs/operations/new-api-production-release-gates.md` 门禁 E/F，不另起一套 |

**本轮自查抓到 2 处自己引入的错误**（写完后逐条复核时发现，已修正）：

- 我写「本地 `IsStream(nil)` 会 panic（`c.Query` 解引用 nil）」——**错**。`relay-claude.go:346` 的接收者是 `dto.GeneralOpenAIRequest`，其实现是 `lo.FromPtrOr(r.Stream, false)`，不触碰 `c`；唯一解引用 `c` 的是 `dto/gemini.go:111`。修正后还发现上游 relaykit 的对应实现有 `if c == nil { return false }` 守卫，nil 语义明确。
- 我写「上游 `makefile` 的 `test` 目标即如此，见 9.3」——**错引用**，§9.3 无此内容；改为直接指向 `upstream/main:makefile:43-49`。

**同时关闭了原第 26 项未决项**（命名空间字段的精确计数）。关闭方式不是换一个数，而是给出「口径 + 命令 + 结果」：以命名空间工具相关的 8 个符号为口径，得 **11 个非测试文件、跨 6 个包**。早期写的「4 个包」无法用任何单一命令复现（窄口径得 8 文件、宽口径得 11 文件，都不是 4 个包）——这正好说明该项当初被列为未决是对的：**没有口径的数字不该保留**。

**方法学教训（第九轮）**：前八轮把「事实正确」做到了极致，却始终没有回答「能不能执行」。**事实层的完备不等于执行层的完备**——阶段 6 的矛盾在八轮里一直存在（它是一句措辞与一个决策的对立，不是任何数字或行号的错误），没有任何一轮的事实核验会碰到它。**评估一份计划，要分「它说的是否为真」与「照着做能否做完」两个问题**；前者靠复现，后者靠把每一阶段当作要执行的动作走一遍。

## 第十轮：阶段表的可执行性逐条走查（2026-10-03）

第九轮补了 4 处执行层缺口后，本轮**把 §7 的每一阶段当作要执行的动作逐条走一遍**，检查三件事：每步的前置是否已具备、阶段之间是否自相矛盾、验证命令在迁移后是否仍然有效。又找出 **6 处**：

| # | 问题 | 性质 | 处置 |
| --- | --- | --- | --- |
| 1 | **§9.3 的验证命令在阶段 1 之后失效**。原文写「每阶段运行 `go test ./controller ./model ./relay/common ./relay/helper ./service`」——这是单 module 命令，但阶段 1/2 会把 `types`/`dto` 迁进 `relaykit/`，根 module 的 `./...` 不再包含它们，relaykit 侧的测试完全测不到。**§7 阶段 0 自己就要求「两个 module 分别 go test」，两处冲突** | 命令失效（会导致「测过了」的假象） | §9.3 按阶段拆分：阶段 0 用单 module 命令；阶段 1 起给出双 module 模板（照抄 `upstream/main:makefile:43-49`），并注明 `GOWORK=off` 不可省 |
| 2 | **阶段 4 引用的「第 4 节的处置结论」不存在**。§4 是**能力盘点**（有什么、归哪类），按 §4.1/§4.2/§4.3/§4.4/§4.5.x 分类，不含动作与阶段归属；§4.1 甚至写的是「应提上游」（对上游仓库的贡献，不属本迁移任何阶段） | 悬空引用 | 新增「阶段 4 的处置清单」表（9 项，逐项标依据），并显式声明「向上游提 PR 不在阶段 4 范围内」 |
| 3 | **阶段 5 的「规则模型附带修正」全文无定义**——只此一处出现，无依据、无验收标准 | 幽灵条目 | 明确为 §12.8 的 `matchAnyRegex` 空模式语义文档化，并补入完成判据 |
| 4 | **§4.3 停留在「需决定」**：原文写「需决定这 4 个常量迁入 `relaykit/types` 还是保留宿主」，而 §12.2 早已决定「全部迁入」，§12.17 #3 也记了 | 决策未回流到盘点节 | §4.3 改为「已决定」并指向 12.2 |
| 5 | **§12.17 第 16 项残留「其字段跨 4 个包」**——上一轮刚在 §12.6 把该数改为「11 个文件、跨 6 个包」，此处未同步 | 跨节同步遗漏（我自己上一轮改的） | 同步为 6 个包 |
| 6 | **§5 表格写 think 标签族「归属待定，见第 10 节第 2 项」**，但第 10 节第 2 项已划掉并给出决定（§12.1） | 过期状态 | 改为「剥离迁入 relaykit、门控留宿主，见 12.1」 |

**另修正一处易误读**：本迁移共新增 **2 个**钩子——`RetainEmptyReasoning`（请求方向，§4.2）与 `RetainReasoningInClaudeResponse`（响应方向，§12.4.2），签名相同（`func(modelName string) bool`）但语义与落点不同。原文两处各自独立描述，无一处点明它们是两个钩子，实现时极易合并。已在两处互相加注。

**方法学教训（第十轮）**：第 1 处与第 2 处都属于**「阶段表与其它节不一致」**这一类，与第九轮找到的阶段 6 矛盾同型——它们都不是「某句话是错的」，而是「两个各自合理的地方放在一起就冲突」。**逐条走查每个阶段时，必须同时检查它的三个引用面**：(a) 它引用的依据节是否存在且内容匹配（第 2 处）、(b) 它给的验证命令在**该阶段之后**是否仍然有效（第 1 处）、(c) 它提到的每个条目是否有定义（第 3 处）。只读阶段表本身看不出这些问题。

第 5 处则说明**跨节同步是个反复出错的点**：我在第九轮改了 §12.6 的数，却漏了 §12.17 汇总表里的同一个数——**改一个数时要 grep 全文，而不是只改当前节**。

## 第十一轮：跨阶段数据流走查——`boundary_test.go` 是阶段 2 的隐性硬门禁（2026-10-03）

第九、十轮走的是「每一阶段自身是否可执行」。本轮走**阶段之间的数据流**：前一阶段的产物进入下一阶段时，是否仍以假设的形态存在；以及**每个阶段的完成判据在迁移后是否真的能通过**。找出 **1 处执行层硬缺口 + 3 处已被文档覆盖的确认项**。

**核心发现：阶段 1 拷贝 relaykit 时，连带拷入了一个阶段 2 会失败的测试。**

`relaykit/relayconvert/boundary_test.go` 以测试形式锁死「将要迁入 relaykit 的包不得 import 宿主或 gin」：

```go
var kitDirs = []string{"relayconvert", "dto", "types", "reasonmap"}
var forbiddenPrefixes = []string{"github.com/QuantumNous/new-api/", "github.com/gin-gonic/gin"}
var allowedViolations = map[string]bool{}   // 空，注释写 "All initial violations have been cleared"
```

文件注释明写「**shrink this list, never grow it**」。该测试以含 `go.mod` 的最近祖先为根（即 `relaykit/`），`kitDirs` 直接对应 `relaykit/dto` 等目录，命中即 `t.Errorf`。

**为什么这是阶段 2 的问题**：阶段 2 把本地协议 `dto` 迁入 `relaykit/dto`。而本地 `dto`（`HEAD`，非测试 `.go`）有 **12 个文件 import 宿主 `common`、2 个 import `constant`、1 个 import `logger`，另 10 个 import gin**。迁入即命中该测试。**阶段 2 的完成判据写的是「两个 module 均可编译；DTO 测试通过」——但 import 不合规时编译可以过，只有 `cd relaykit && GOWORK=off go test ./...` 会拦下**，而阶段 2 的行里从未提到这个测试。

| 被 dto import 的宿主包 | 文件数 | 符号 | 归位 |
| --- | --- | --- | --- |
| `common` | 12 | `Marshal`/`Unmarshal`/`GetJsonType`/`Interface2String`/`JsonRawMessageToString`/`Any2Type`/`RawMessage` | 改 import `relaykit/relayconvert/kitutil`，同签名直接替换 |
| `common` | 1（`openai_request.go`） | `JsonDelim`、`NewJsonDecoderUseNumber`、`DecodeJsonFromDecoder` | **`kitutil` 无对应导出**——须改写或新增到 relaykit |
| `common` | 1（`opaque_openai_responses_request.go`） | `ResponsesRequestMetadata` | **`kitutil` 无对应导出**——须迁 `relaykit/types` 或新增 |
| `constant` | 2 | `EndpointType`（`pricing.go:11`） | 改 `types.EndpointType`（上游即如此） |
| `constant` | 1（`channel_settings.go`） | `ChannelTypeCodex`/`ChannelTypeOpenAI`/`ChannelTypeAzure` | 随本地独有方法归属一并决策（4.4） |
| `logger` | 1 | `LogError` | 改 `kitutil.LogError` |
| gin | 10 | — | 见「阶段 2 的前置：`IsStream` 迁移的实际范围」 |

**补法**：新增 §7 硬依赖第 2 条（`dto` 不得 import 宿主/gin，`boundary_test.go` 为硬门禁）；阶段 2 的行补「清空 `dto` 对 `common`/`constant`/`logger`/gin 的 import」与完成判据「`boundary_test.go` 通过」；新增「阶段 2 的前置：`boundary_test.go` 是阶段 2 的硬门禁」整段（含上表与「不加 `allowedViolations`」的理由）；新增 §12.0.4 记录该决定；§9.3 注明 relaykit 的 `go test ./...` 含边界门禁；§11 的 boundary 提及处补 `forbiddenPrefixes`/`allowedViolations` 与「阶段 2 必须满足」；新增第 10 节第 21 项与 §12.17 #21。

**同时确认的 3 处跨阶段数据流（走查后判定已覆盖，无需改动）**：

1. **阶段 1 → 2**：`types` 拆分后本地独有符号靠别名转发（`constant/endpoint_type.go` 的手法），`dto` 改 import 即可，链路成立——文档已写。
2. **阶段 2 → 3**：`service/convert.go` 对 `dto.*` 有 **37 个不同类型**引用；`dto` 迁走后改 import 路径即可（阶段 2 的 327 个 import 点含它）。`ClaudeConvertInfo.` 在 `service/convert.go` **共 59 处**，其中本地独有 4 字段（`ToolCallStarted`/`ToolCallIDs`/`ToolCallNames`/`ToolCallPendingArgs`）为 **22 处**——阶段 3 写的「22 处字段引用（6+5+5+6）」经逐行复现**完全准确**（分字段 6/5/5/6）。
3. **阶段 3 → 4**：`RelayInfo` 在阶段 3 实现 `convmeta.Meta` 后，阶段 4 的补丁 1 落在 relaykit 的转换器内（`internal/claude_messages/to_oai_chat_req.go:181`、`to_oai_responses_req.go:165`），不依赖宿主侧改动，顺序成立。

**方法学教训（第十一轮）**：前三轮分别查了事实层、阶段自身可执行性、引用面；本轮查**阶段间的契约**——一个阶段的产物是否满足下一阶段**没说出口**的前提。这次的形式是：阶段 1 的产物（拷贝 relaykit）**自带一个测试**，而该测试恰好对阶段 2 施加了阶段 2 从未声明的约束。**走查阶段时不能只看阶段表列出的动作，还要看每个动作的产物里是否藏着对后续阶段的隐含断言**——`boundary_test.go` 就是「上游代码里写死的验收标准」。同类风险面：relaykit 内任何 `*_test.go`、golden 快照、`go.mod` 的 `require` 都在拷贝瞬间成为本地契约。

## 第十二轮：全维度复核发现的计数错误（2026-10-03）

对本会话全部提交做全维度复核时，用一个只读 agent 独立复现 `builtinTextConverters` 的规模，推翻了第七轮（`c926fb0e9`）写入的数字：

| 错误断言 | 实际 |
| --- | --- |
| 「4 个文本格式间的 **8 个**内建转换器（6 个双向对 + 2 个单向）」 | `builtinTextConverters` 顶层 **12 个**元素，覆盖 4 格式的全部 12 个有序对，即 **6 个双向对、0 个单向** |

**复现命令**（`upstream/main` 已在本地）：

```bash
git show upstream/main:relaykit/relayconvert/text_converter_registry.go \
  | awk '/var builtinTextConverters/,/^}$/' | grep -cE '^\t\tID:'
# 12
```

12 个 ID 中 8 个以 `Converter*` 命名、4 个以 `requestConverter*` 命名。第七轮只数了前者，漏了后 4 个复合 `StepConverters` 条目，并据此臆造了「2 个单向」——实际每个有序对都有反向条目，无一单向。**本轮修正 §3.1 表格（设计文档第 68 行）、审计记录第 83 行与第 322 行。**

**方法学教训（第十二轮）**：这是一个**「计数口径未写明」**型的错误，与第九、十轮的性质不同——那两轮是「两处合理表述互相冲突」，这一轮是「一个数字对应多个可数的子集，选了错的子集且没写口径」。`builtinTextConverters` 里同时存在 `Converter*` 与 `requestConverter*` 两类命名，只按命名前缀 `grep` 会系统性漏掉后者。**凡计数结论，命令必须能唯一地圈定全集**：本轮的正确写法是按 `^\t\tID:` 的缩进层级取顶层元素，而非按命名模式。

## 第十三轮：把「未核验」变成结论——关闭 21/22/23，发现 459 行遗漏路径（2026-10-04）

第十二轮结束时，第 10 节仍有 7 项未决（21–27），§4.5.5 仍有 6 条「未逐项核验」的边界。本轮把这 13 项**逐条做成结论**（不是继续标为未决），全部数据用 `go/ast` 抽取、基线 `HEAD`。

**新发现一：`relay/channel/claude/relay-claude.go:255 RequestOpenAI2ClaudeMessage`——459 行，被 3 个通道共用，第 8 节整条遗漏。**

§8「响应方向的 6 条路径」只列**出向**（上游响应 → `dto.ClaudeResponse`）。入向侧存在一条**通道级**转换器，不写在 `service/convert.go` 里，因此既不在 §8，也没进阶段 6 的删除范围：

| | 本地 | relaykit |
| --- | --- | --- |
| 函数 | `RequestOpenAI2ClaudeMessage`（`relay-claude.go:255`，**459 行**） | `relayconvert.OpenAIChatRequestToClaudeMessages`（`request_compat.go:20`） |
| 调用方 | `relay/channel/claude/adaptor.go:100`、**`relay/channel/aws/adaptor.go:173`**、**`relay/channel/vertex/adaptor.go:302`** | 上游自身 |

459 行是本地第二大的转换函数（仅次于 `service/convert.go` 的 2229 行）。阶段 6 只删 `service/convert.go`，**不删通道级转换器**，因此迁移后会留下「relaykit 转换器 + 本地 459 行」两套并行实现——正是 §12.15 竭力避免的双实现。**且它不能简单替换**：实测它绑定了三类 relaykit 侧零对应的宿主能力——`model_setting.GetClaudeSettings()` / `ShouldPreserveThinkingSuffix()`（思考等级适配器）、`buildClaudeMediaMessageFromFile/Source`（文件附件构造）、`normalizeClaudeInputSchema`（输入 schema 归一化）。列为未决项 28。

同族另 3 个：`StreamResponseClaude2OpenAI`（`relay-claude.go:714`）、`ResponseClaude2OpenAI` / `WithError`（`:831`/`:836`，被 `aws/converse.go:696` 调用）、`responseGeminiChat2OpenAI` / `WithError`（`relay-gemini.go:1276`/`:1281`）。

顺带完成 §4.5.5 那条「未逐文件清点通道内自有转换逻辑」的欠账：`relay/channel/*/` 共 **30 个** `Request*2*`/`Response*2*` 转换函数、分布 17 个通道包，其中 **26 个是上游厂商私有协议（relaykit 无对应，保留）**，**4 个与 relaykit 重复**（上表）。

**新发现二：§4.4 的「relaykit 独有 5 个方法」是全包 31 个的局部口径。**

按 `接收者.方法名` 对本地 `dto/` 与 `relaykit/dto/` 全包求差集（AST，非测试）：本地方法 229、relaykit 159，**本地独有 101、relaykit 独有 31**。§4.4 末尾与 §9.1 第 6 行写的「5 个」只覆盖 `openai_request`/`openai_response`/`claude` 三个文件。已两处修正并加注口径。

**新发现三：12.0.2 的「394 vs 273 个函数」漏标「含测试」口径。**

逐文件复核：`^func` **含测试** = 394 / 273（文中数字），**非测试** = 306 / 215，AST 唯一键非测试 = 301 / 210。原句只写「`^func` 口径，含包级函数」，未说含测试，与 relaykit 对比时会让读者用错基数。已补全口径并给出三种计数。

**关闭的项（21/22/23）**：

| 原未决项 | 结论 |
| --- | --- |
| 21 `dto` 完整方法级差集 | **已给出**：方法 101 / 31、包级函数 65 / 44，按文件归组标注性质；文件级归属也定了（`midjourney`/`suno`/`task`/`video` 留宿主，其余 11 个本地独有文件随协议迁入） |
| 22 import 图是否成环 | **不成环**：`constant` 包 0 个 relaykit import（叶子），`relaykit/types`、`kitutil` 均不 import dto。§12.0.3 的担心可撤销。真正的遗留是 `constant.ChannelType*` 在 `relaykit/types` 不存在（归属问题，非环） |
| 23 33 个字段不兼容类型 | **已二分**：136 个同名结构体中 33 个字段集不同，按「取 relaykit 后本地是否丢字段」分为**需动作 16 个（含保留项）** 与**纯增益 17 个**，需动作项逐条给了处置 |

顺带核出一条新的执行层约束并入未决项 29：`OpenAIResponsesRequest.effectiveToolsCache{,Err,Ready,Signature}` 是**未导出字段**，Go 里未导出字段属于声明包，**不能声明在 relaykit 的类型上**，取 relaykit 类型后必须改为方法级缓存或宿主侧旁路。

**§4.5.5 其余 4 条的核验结果**（全部由「未核验」转为「已核验」）：`service/` 根目录协议相关文件只有 5 个非 responses 文件且均为记录/兼容用途；`controller/` 8 个文件为构造测试请求/路由轨迹，不承载转换；relaykit 三演进分支的 `to_oai_chat_req.go` **仍无 `thinking` case，补丁 1 未被覆盖**（第 10 节第 15 项结论成立）；`responses_input_filter.go` 的 ID 前缀契约（14 个 item type）与本地专有 marker `newapi.synthetic.compact:` relaykit 零对应。

**方法学教训（第十三轮）**：前十二轮一直在「发现文档哪里说错了」。这一轮换成了**「把文档自己承认不知道的东西查出来」**——未决项与「未核验」清单是文档最诚实的部分，也是最容易被反复引用而不被消化的部分。十三轮里**最大的发现（459 行遗漏路径）恰恰出自 §4.5.5 第一条**——作者自己标注了「未逐文件清点通道内自有的转换逻辑」，清单在那儿挂了五轮没人动。**凡文档里有「未核验/待定/未清点」的句子，那就是下一个缺口的位置**；一份计划的可执行性上限，等于它未决项中最关键那一条的分量。

## 第十四轮：字段名相同不等于兼容——tag 与 Go 类型两个维度（2026-10-04）

第十三轮做了字段集的完整差集，据此判定「136 个同名结构体中 33 个不同，其中 16 个需动作、17 个取 relaykit 即纯增益」。本轮问了一个**此前十三轮从未问过**的问题：**差集比的是字段名集合，那字段名相同、但 JSON tag 或 Go 类型不同的呢？** 结论：有，而且「17 个纯增益」这个判断**不成立**。

**一、JSON tag 差异 5 处**（AST 抽取，非测试）：

| 类型.字段 | 本地 | relaykit | 为什么是问题 |
|---|---|---|---|
| `ChatCompletionsStreamResponseChoice.Delta` | `delta,omitempty` | `delta` | 空结构体时，本地省略键、relaykit 输出 `"delta":{}` |
| `GeminiChatRequest.GenerationConfig` | `generationConfig,omitempty` | `generationConfig` | 入向：空配置本地不发、relaykit 会发零值对象给 Gemini |
| `ToolCallRequest.Function` | `function,omitempty` | `function` | 出向键是否存在 |
| `IncompleteDetails.Reason` | `reason,omitempty` | `reason` | 出向 `"reason":""` 是否出现 |
| `ClaudeServerToolUse.WebSearchRequests` | `web_search_requests` | `web_search_requests,omitempty` | relaykit 在 0 时省略（另 3 个兄弟字段同样带 `omitempty`） |

这些**不改字段名、不改类型，编译完全通过，只是序列化输出变了**——字段名差集永远看不到它们。

**二、Go 类型差异 12 个类型，分三类**：

- **类别 A（8 个，无需动作）**：`interface{}` 与 `any` 是同一类型的两种拼写。`DifferenceItem`、`GeminiFunctionResponse`、`GeminiModel`（13 个字段）、`Notify`、`RealtimeSession`、`ResponsesOutputContent`、`Tool.InputSchema`。会在 diff 里产生噪音，评审时别误判为不兼容。
- **类别 B（1 个，语义等价）**：`OpenAIModels.SupportedEndpointTypes` 由 `[]constant.EndpointType` 变 `[]types.EndpointType`——同一底层类型（12.0.1 已证是别名），JSON 形状不变。但它再次确认 `constant` 不能出现在 relaykit 内（12.0.4）。
- **类别 C（3 个，必须处置）**：
  - `ResponsesStreamResponse.Text`、`.Arguments`：`string` → `*string`。`omitempty` 语义不同（空串本地省略、relaykit 保留），且**编译期破坏**
  - `GeminiThinkingConfig.IncludeThoughts`：`bool` → `*bool`。**这是思考等级链路的一环**，本地 `relay/channel/gemini/relay-gemini.go` 有 **8 处**直接赋值/读取（`:148`/`:167`/`:171`/`:192` 字面量 `true`，`:302`/`:305` 赋 `true`/`false`，`:315` 赋变量，`:339` 读取），改指针后每处都要改
  - `OpenAIResponsesResponse.CreatedAt`、`OpenAIResponsesCompactionResponse.CreatedAt`：`int` → `IntValue`（自定义 `UnmarshalJSON`，接受 int/float/**string** 三种形态）。取 relaykit 是能力增强，但需确认本地没有依赖「字符串形态报错」

**为什么这一轮值得单独做**：第十三轮我在同一份文档里刚写下「17 个纯增益」，第十四轮就在同一批对象上找到了 10 个另有差异的类型。**这不是数字抄错，是「比较维度少了一层」**——我比了字段名的集合，没比集合里每个元素的 tag 和类型。**「集合相同」是最容易让人停止检查的结论**，因为它看起来是个完整的答案。已把三个维度（名 / tag / 类型）都写进 12.0.6，并把它同时挂到 §9.1 语义对齐清单与阶段 2 判据上。

**三、补齐另外两个盲维度（结论：无新差异）**：字段名差集本身有盲区——`ast.Field.Names` 对**匿名嵌入字段**为空，所以嵌入字段在名差集和类型差集里都隐形。补测：嵌入字段本地 7 / relaykit 7，逐类型零差异；函数与方法**签名**（此前只比名字）135 个同名里 13 个不同——12 个是 `IsStream` 的 `gin.Context→http.Request`（与 §7 阶段 2 前置记录的 12 个方法完全吻合，互为印证）、1 个是 `NewNotify` 的 `interface{}→any`。**无未知项**。

**方法学教训（第十四轮）**：前十三轮找的是「文档写错了什么」，第十三轮找的是「文档说不知道什么」，这一轮找的是「文档**自以为知道**什么」。**「已完成」「已关闭」「纯增益」「无需动作」这类收束性结论是风险最高的位置**——它们终止了后续检查。凡写出一个「已完成」，就该立刻问：**这个「完成」是按哪个维度衡量的？还有哪些维度没比？** 本轮的具体形式是：字段名差集 ✓、字段 tag 差集 ✗、字段类型差集 ✗。

## 第十五轮：把四维比较做成工具——工具当场抓出上一轮的两处计数错（2026-10-04）

第十四轮的结论是「比较维度会漏」，靠人肉每次换角度只能发现、不能防止。本轮把四个维度固化成可重跑的工具，并立刻用它复核第十四轮刚写下的数字。

**新增**：`tools/relaykit-dto-compat/`（独立 Go module，7 个单元测试）＋ `scripts/relaykit-dto-compat.sh`（从 git 拉两侧源码再调用）。

**工具当场抓出第十四轮的两处计数错误**：

| 上一轮写的 | 工具实测 | 性质 |
| --- | --- | --- |
| Go 类型真差异 **3 处** | **6 处** | 漏了 `OpenAIModels.SupportedEndpointTypes`（`[]constant.EndpointType` → `[]types.EndpointType`）。它不是 `interface{}`/`any` 拼写等价，工具正确地把它计入类型差异，而我把它归进了「等价」的类别 B 而没有计入那 3 处 |
| 签名差异 **13 处** | **12 处** | 13 里含 `NewNotify` 的 `interface{}` → `any`，那是**拼写等价**，不该算作签名差异。工具把它单列进「等价拼写」组 |

两处都不是数字抄错，而是**分类边界划错**：什么叫「真差异」、什么叫「等价拼写」，我上轮是靠印象划的，工具要求把规则写成代码。规则一旦写成代码，`[]constant.EndpointType`（语义等价但**引用不同**，仍需改 import）与 `interface{}`/`any`（完全同一类型）就不再混在一起。

**工具在编写过程中又暴露一个自己的 bug**：嵌入字段维度最初写成「遍历本地侧、查 kit 侧有无同名条目」，导致**一侧有嵌入字段、另一侧完全没有**时不报警（另一侧在 map 里没有条目，被 `continue` 跳过）。单元测试 `TestEmbeddedFieldDifferenceIsDetected` 当场捕获，改为遍历并集。**这条正是第十四轮列出的盲维度本身**——如果只写工具不写测试，会把同一类盲区原样复制进工具里，而且更难发现。

**验证**（本轮实跑）：
- `cd tools/relaykit-dto-compat && go vet ./... && go test ./...` → `ok relaykit-dto-compat`
- `scripts/relaykit-dto-compat.sh --strict` → `strict: counts match the recorded expectation`，exit 0
- **反转验证**：把 `wantTag` 由 5 改成 99 → exit 1 并报 `dimension 1 changed: got 5 want 99`；改回 → exit 0。**门禁确实会因计数漂移而失败，不是恒真**
- `go build ./...`（根 module）→ exit 0；`go list ./tools/...` → `matched no packages`，工具模块被正确排除在根 module 之外

**方法学教训（第十五轮）**：第十四轮的教训是「一次性的核查发现不了下一条」，本轮的解法是**把核查本身变成可重跑的东西**。人肉复核的三个结构性缺陷——会漏维度、结论无法复算、上游演进后要重来一遍——都由工具一次性消掉。**能写成代码的核查规则，就不该只留在一次性的报告里**；反过来，把规则写成代码时**必须配测试**，否则规则里的分类边界（什么叫「等价」）和实现盲区会一起固化，而且比人肉更难察觉——本轮工具自己的嵌入字段 bug 就是例证。

## 第十六轮：把三个「待定」变成决定（2026-10-04）

前几轮把未决项从 7 项压到 6 项，但剩下三项（27 三个符号归位、28 通道级转换器归属、29 未导出缓存字段）都标着「阶段 2/4 开工前必须确定」。本轮逐项取证，三项全部关闭。

**未决项 27 上半：三个符号是 `encoding/json` 的薄包装，直接换 stdlib。** 打开 `common/json.go` 看第一行就够——`type JsonDelim = json.Delim`、`NewJsonDecoderUseNumber` = `json.NewDecoder` + `UseNumber()`、`DecodeJsonFromDecoder` = `decoder.Decode(v)`。而 relaykit/dto **本就 import `encoding/json`**。此前把它们当作「`kitutil` 未提供的宿主能力」是误判：它们没有宿主语义。改写只需补 `bytes`/`io` 两个 stdlib import。**教训：凡「X 未提供 Y」的结论，先打开 Y 的定义看它是不是 stdlib 包装。**

**未决项 27 下半：`ResponsesRequestMetadata` 不是「一个类型」，是 488 行文件 + 650 行扫描器依赖。** 前几轮一直按「一个类型」估它，实测它所在的 `common/responses_request_metadata.go` 有 488 行，还依赖 `common/json_top_level_replay.go` 的 650 行扫描器，而那个扫描器**同时被 `relay/responses_handler.go` 使用**（不是 dto 专属）。整体迁入 relaykit 等于把 ~1100 行本地 responses 解析基础设施拉进套件。决定：**`dto/opaque_openai_responses_request.go`（64 行，本地独有类型）保留在宿主 `dto`**，与 `midjourney`/`suno`/`task`/`video` 同类；副作用是本地协议类型分居两包，通过 relaykit 的 `dto.Request` 接口协作，可接受。

**未决项 28：收敛到 relaykit，不是「保留双实现」也不是「原样不动」。** 上几轮把该函数的三类宿主耦合（`model_setting`、`buildClaudeMediaMessageFromFile/Source`、`normalizeClaudeInputSchema`）当作「relaykit 零对应」，因而只敢判「需决定」。本轮打开 relaykit 侧，发现**三类都有注入机制**：

| 本地能力 | relaykit 机制 |
| --- | --- |
| 思考等级适配器 | `sharedclaude.ApplyReasoning` **完全经 `convmeta.Options` 取策略**（`ShouldPreserveThinkingSuffix`、`Claude.ThinkingAdapterBudgetTokensPercentage`），relaykit 内零 `model_setting` 引用 |
| 媒体/文件解析 | `relayconvert.MediaResolver`，其 `GetBase64Data` 字段**与 `service.GetBase64Data` 签名完全一致**；MIME→`ClaudeMediaMessage` 映射已在 relaykit 内 |
| schema 归一化 | `sharedclaude.FunctionParametersToInputSchema`（内置） |

于是决定：阶段 4 注册 `SetMediaResolver` 与 `Options` 钩子，三个通道改调 relaykit，**459 行本地实现删除**。留了两条必须实测的边界（`normalizeClaudeInputSchema` 行为是否逐项对齐、本地 `text/*` MIME 分支是否仍需要），并给出退路（若边界不成立则迁入 relaykit 而非保留）。**教训：判「上游没有对应能力」之前，先搜上游有没有「注入点」——钩子式设计里，能力在宿主、机制在套件。**

**未决项 29：问题问错了。** 原表述是「4 个未导出字段不能声明在 relaykit 的类型上，须改为方法级缓存或宿主侧旁路」。前半句对，后半句是错误推论——未导出字段**在自己包里声明完全合法**，上游 relaykit 本就有（`Reasoning.effortRaw`）。把 `EffectiveTools()` 连缓存字段一起迁入 `relaykit/dto` 即可，13 个调用点改 import，**不需要任何旁路**。**教训：「A 不能做 X」不等于「必须用 B 绕开」——先问 X 的约束条件是什么，往往换个位置约束就消失了。**

**验证**：本轮结论全部由读码得出（`git show` / `git grep`），未改生产代码。三项关闭后，第 10 节未决项由 6 项降至 3 项（24 openaicompat 重构、25 golden 基线、30 tag/类型差异）。

## 第十七轮：tag 与类型差异逐项决定——15 处全部采用 relaykit（2026-10-04）

第十四轮发现 5 处 tag 差异与 6 处类型差异，第十五轮用工具确认计数，本轮逐项决定「采用 relaykit / 打补丁保留本地」。**结论：15 处全部采用 relaykit，零处打补丁。** 但更重要的是过程中推翻了第十四轮对 tag 差异的判断。

**第十四轮的过度警告**：那一轮把 5 处 tag 差异一律描述为「**静默改变序列化输出**」。本轮打开字段类型后发现，**3 处是空操作**：

`encoding/json` 的 `omitempty` 只对 false / 0 / nil 指针或接口 / 长度 0 的 string / array / map / slice 生效，**对 struct 值不生效**。`ChatCompletionsStreamResponseChoice.Delta`、`ToolCallRequest.Function`、`GeminiChatRequest.GenerationConfig` 三者都是 **struct 值**，所以两侧都输出 `"delta":{...}` / `"function":{...}` / `"generationConfig":{...}`——**tag 不同但输出完全相同**。

只有 `IncompleteDetails.Reason`（string）与 `ClaudeServerToolUse.WebSearchRequests`（int）是真实行为变化，而两处风险都能排除：
- `IncompleteDetails` 只在两个构造点产生，**两处都设非空 Reason**，故实际输出不变
- `ClaudeServerToolUse` 生产代码从不读取，省略行为无影响

**类型差异 6 处的处置**：
- `OpenAIResponsesResponse.CreatedAt` / `OpenAIResponsesCompactionResponse.CreatedAt`（`int` → `IntValue`）：**纯增益**。`IntValue.MarshalJSON` 输出 `int(i)`，与本地 `int` **完全相同**；`UnmarshalJSON` 额外接受 float 与 string，是超集
- `ResponsesStreamResponse.Text` / `.Arguments`（`string` → `*string`）：采用。本地构造点**不显式写空串**（逐点核实），故「空串 vs nil」的 `omitempty` 差异不触发；须改读写点
- `GeminiThinkingConfig.IncludeThoughts`（`bool` → `*bool`）：采用。relaykit 用 nil 表示「客户端未指定」，**正是思考等级链路所需**（`shared/gemini/request.go:168` 的 `if native.IncludeThoughts == nil` 依赖它）；本地 8 处赋值点改 `kitutil.GetPointer(...)` 或解引用
- `OpenAIModels.SupportedEndpointTypes`（`[]constant.EndpointType` → `[]types.EndpointType`）：采用，改 import

**实际工作量**：改 8 处 `IncludeThoughts` 赋值点 + 所有 `Text`/`Arguments` 读写点 + 4 处 import。tag 差异中 3 处无需动作。

**方法学教训（第十七轮）**：第十四、十五、十六轮连续三轮都在「tag 差异」这个题目上工作，但直到本轮**打开字段类型**才判准。前两轮把「tag 文本不同」直接等同于「序列化输出不同」——**这是个未经检验的推理**。`omitempty` 的语义依赖字段类型，是 Go 的既定行为，但只有把类型摆到 tag 旁边才看得出来。**「A 的属性不同」不等于「A 的行为不同」；属性到行为之间隔着一条要单独验证的推理链**——本轮的形式是：tag 差异（属性）→ `omitempty` 是否生效（取决于类型）→ 输出是否不同（行为）。跳过中间一环，就会把 3 处空操作误报成 3 处风险。

## 第十八轮：补建本地 golden 基线——建基线时发现第一版漏掉了整个 thinking 门控（2026-10-04）

第 10 节第 25 项要求「用当前 `HEAD` 生成本地 golden 基线」。本轮把它建成，并首次**实际运行 `go test ./service`**（此前十七轮全部是静态阅读，审计记录此前明确标注「未运行 Go 编译或测试」）。

**产物**：`service/convert_golden_test.go` + `service/testdata/golden/`（8 个 JSON 快照，覆盖请求/响应/流三个矩阵）。校验命令 `go test ./service -run TestGolden -count=1`，重录加 `-update`。

**建基线时发现的 must-fix**：第一版 fixture 全用非 DeepSeek 模型名（`gpt-test` / `claude-test` / `gemini-test`）。跑出来的第一版快照**全部通过**，但都是**没有 thinking block 的**。核对 `shouldExposeReasoningAsClaudeThinking`（`service/convert.go:533`）后确认：reasoning 暴露**仅对 DeepSeek 模型名**生效（门控依据 `IsDeepSeekModelName`，`:512`）。也就是说，第一版基线把**本次迁移存在的理由**——第 5 节「必须保留 thinking 历史回传」的能力——整个排除在覆盖之外。补 DeepSeek 侧两个快照后：非 DeepSeek 快照里 reasoning 被丢弃、DeepSeek 快照里出现 `content_block_start{type:thinking, index:0}` 且后续块顺移到 index 1/2。**只有两侧都在，门控翻成 always-on 或 always-off 才会被检出。**

这与第八节（12.4.1）的索引空洞是同一类风险：thinking 一出现，后面所有块的索引都要顺移；基线必须同时钉住「有/无 thinking」两种索引布局。

**门控的模型名口径**：`HasDeepSeekModelName`（`:544`）综合响应 `Model`、`info.OriginModelName`、`info.UpstreamModelName` 三者判定。故 fixture 的模型名**就是**门控开关，改 fixture 必须重录快照。

**基线口径的实测（工作区有他人未提交改动）**：生成时工作区的 `service/convert.go` 有**其他会话的未提交改动**（`ClaudeToOpenAIRequest` 的 `else` 分支加 `ReasoningEffort` 透传）。若快照编码了未提交行为，提交它就是错的。验证方式：把 `convert.go` 换成 `HEAD` 版本（2229 行 vs 工作区 2236 行）重跑同一批快照，**结果一致通过**——该改动读 `claudeRequest.OutputConfig`（fixture 未设 `output_config`，`GetEfforts` 返回 `""`），对快照为 no-op。故基线等价于 `HEAD` 口径。验证后已还原工作区文件（`git diff --stat service/convert.go` 恢复为 `7 insertions`）。

**反向验证（已实测）**：扰动 `response/openai_to_claude_deepseek`（thinking→text）、`stream/openai_to_claude_deepseek`（index 2→3）、`request/claude_to_openai`（删 tool_call_id），三次均 `FAIL` 且报「drifted from golden snapshot …」；恢复后 `PASS`。`-count=3 -shuffle=on` 保持通过，无跨运行泄漏。`gofmt` 已修正（`types` 与 `relaycommon` 的 import 顺序）。

**baseline 的职责边界（重要）**：本基线只钉「正常路径的字节级不漂移」。9.1/9.2 的 12 处差异**多数是错误路径**（未知 block 报错、image 缺 source 报错、多 choice 报错、`finish_reason` 严格校验、tool_use 缺 id/name 报错），不产出「可快照的正常输出」，因此由既有**专用单测**覆盖（`service/convert_claude_request_test.go`、`service/convert_claude_stream_test.go`；本次已核两文件为 git 跟踪、在 `HEAD` 上通过），**不在** golden 基线职责内。不能因为「golden 基线建好了」就宣称 9.1/9.2 的 12 处差异已被覆盖——那是另一批测试的职责。

**方法学教训（第十八轮）**：十七轮都在读代码，本轮第一次**跑**测试。「第一版快照全绿」本身是个陷阱——绿只证明 fixture 的输入能跑通，不证明 fixture 覆盖了要保护的能力。**基线通过 ≠ 基线有效**；有效性的判据是「把被测能力改坏，基线会不会红」。本轮形式：fixture 选了非 DeepSeek 模型（输入）→ 门控不触发（分支）→ 快照里没有 thinking（输出），三环里第一环选错，后两环再对也没用。检验基线有效性的唯一手段是**反向验证**（扰动后必须失败），而不是正向跑绿。

## 第十九轮：把「已完成的产物」当成新起点——基线的生命周期是本轮自己造的缺口（2026-10-05）

第十八轮把第 25 项关掉、写完「已关闭」就收工了。本轮问了一个前十八轮都成立、但第十八轮的产物**恰好使其失效**的问题：**这份基线在阶段 4/6 之后还在吗？** 答案是「不在，且没人负责」。

**缺口（第十八轮自己引入的）**：`service/convert_golden_test.go` 调用 6 个**本地**函数，实测全部定义在 `service/convert.go`（HEAD 口径 `:18`、`:1248`、`:1040`、`:1831`、`:730`、`:2101`）——而**阶段 6 明确删除该文件主体**。若不处理，两种结局都错：要么阶段 6 连测试一起删（第 25 项白做，基线在最需要它的时刻消失），要么测试悬空指向已删除的函数、编译失败后被人草率删除。这与第十一轮的 `boundary_test.go` 是**同一类错误**：一个阶段的产物对后续阶段有隐含前提，而前提没被写出来。区别在于那个前提由上游代码带入，这一个由本地基线自带——**上一轮刚写完「已关闭」，就给下一个阶段造了一个未声明的依赖**。

**修的过程本身又错了三次，全部由取证推翻**（这是本轮最值得记的部分）：

1. **入口可达性错了**。我先写 relaykit 侧入口是 `claude_messages.ClaudeMessagesRequestToOpenAIChat(ctx, req, info)` 这类形式。实测 `relayconvert/internal/{claude_messages,gemini_chat,oai_chat}/` 是 **`internal/` 包，Go 语言规则禁止宿主 import**；宿主可达的只有 `relayconvert` 包顶层的 `request_compat.go` / `response_compat.go`。**写迁移方案时必须先问「这个包宿主能 import 吗」，而不是看到函数存在就写进表格。**
2. **`ctx` 参数是臆造的**。我断言「请求方向改指要补 `ctx`」。实测 `request_compat.go:16`/`:24` 内部用 `context.Background()`，**签名里根本没有 `ctx`**；只有反向入口（`OpenAIChatRequestToClaudeMessages` 等）收 `c context.Context`。**「两个签名长得像」不等于「参数一样」。**
3. **阶段 6「整体移入 `relaykit/relayconvert/`」会编译失败**。上游 `relaykit/relayconvert/golden_test.go`（同包 `package relayconvert`）已声明**同名同签名**的 `updateGolden`（`:25`）、`goldenDir`（`:42`）、`marshalGolden`（`:55`）、`checkGolden`（`:62`）、`mustUnmarshalFixture`（`:460`）、`TestMain`（`:29`），两文件的归一化正则也逐字相同 → 同包共存必然 `redeclared in this block`。**改为「只把本地独有的部分并入上游文件，复用其 helper」。**

**四项修正**（已落入设计文档）：阶段 4 完成判据由含糊的「转换测试通过」改为「`TestGolden` 改指 relaykit 后 8 个快照逐字节一致，或每处差异逐条对应 9.1/9.2 的已决定项」；处置清单加第 12 项（改指调用点，**快照内容保持不变**，且**禁止用 `-update` 吸收差异**——那正是 12.16 批评上游的原话）；阶段 6 明确「本地独有部分并入上游 `golden_test.go`，不得整体移入」；§9.3 增列两个 module 的校验命令。

**顺带的独立印证（比 12.16 原有的论断更精确）**：上游 24 个快照**恰好漏掉**本地要保的那条路径。上游 response 矩阵**显式排除** `openai_to_claude`：

```go
// upstream/main:relaykit/relayconvert/golden_test.go:323-325
if from == to || to == types.RelayFormatGemini ||
    (from == types.RelayFormatOpenAI && to == types.RelayFormatClaude) {
    continue
}
```

实测 `response/` 目录 8 个快照里确实没有 `openai_to_claude.golden.json`。而**本地 thinking 门控正落在这条被排除的路径上**。stream 矩阵虽包含 `openai_to_claude`，但实测该快照 `thinking` 出现 **0 次**——上游 fixture 用 `stream-model` 且 chunk 里完全没有 `reasoning_content`。**故上游 24 个快照没有一个覆盖 thinking 暴露路径**。这把 12.16 的「24 个快照一处都测不到」从断言升级为**可指认的代码位置**：不是偶然遗漏，是被 `continue` 主动排除；而本地补出的 DeepSeek 快照填补的正是上游刻意留白的格子。

**同时修正了自己引入的行号口径错**：`service/convert.go` 有他会话的 7 行未提交改动，我一度按工作区写了 `:1255`/`:1047`/`:736`，按 HEAD 应为 `:1248`/`:1040`/`:730`。

**写完判据后再核，又发现本轮自己引入的两处错**（同一个「写完不复核」的毛病，本轮内重犯）：

1. **判据是循环引用**。我写的阶段 4 判据要求「每一处差异逐条对应 9.1/9.2 的**已决定项**」——但处置清单第 6 项恰恰写着「9.1 的 6 条与 9.2 的 6 条，每项确认『保留/放弃/改写』」是**阶段 4 才做的动作**。也就是说，判据引用了一个**此刻不存在、且由本阶段自己产出**的清单。修正为「对应处置清单**第 6 项产出**的决定」，并注明该清单是阶段 4 的产出而非既有物。**「判据引用某个清单」时必须确认该清单在本阶段开始前已存在**——否则判据无法在阶段入口处被判定。
2. **快照文件名冲突的方向写反了**。我在 §12.16.3 写「两条 `*_deepseek` 快照的路径不覆盖上游既有文件名」，暗示 DeepSeek 两条是冲突源。实测本地 8 个快照与上游 24 个的**交集是两个不带后缀的**：`stream/openai_to_claude.golden.json` 与 `stream/openai_to_gemini.golden.json`，且**内容都不同**（上游 3489 / 4050 字节，本地 1536 / 4243 字节）。**这两条才是必须改名的**，否则阶段 6 合并即静默覆盖上游 `TestGoldenStreamConversionMatrix` 的基线；`*_deepseek` 两条**不与上游重名，是纯新增**。修正并注明「上一版本此处写反了」。

**方法学教训（第十九轮）**：

- **「已完成 / 已关闭」是风险最高的措辞，因为它终止了后续检查**——第十八轮刚写下这个措辞，本轮立刻发现该产物有个没人负责的下半场。凡关闭一项时产出的**代码、测试、快照、脚本**，都要多问一句「它在后续阶段还在吗，谁维护它」。
- **写「X 迁到 Y」时，先问 Y 的包宿主能否 import**，再问参数是否一致。前者零成本，后者要读源码，两者都不能靠函数名相似推断。
- **接手任何「搬到另一个模块/包」的方案前，先 grep 目标包是否已有同名测试文件与同名 helper**。上游往往已经把同类测试写好了——本轮的发现不是「要新建测试」，而是「上游已有一份，且它刻意没测我们要保的那条路径」。
- **判据、清单、决定之间要查是否存在循环引用**：若 A 判据要求「对应 B 清单」，而 B 清单是 A 所在阶段的产出，则 A 无法在阶段入口判定。**写任何判据后，立刻问「它引用的东西在阶段开始时存在吗」。**
- **比较两组文件名的交集时，把交集和差集都算出来，并核内容是否相同**——「文件名冲突」与「内容冲突」是两件事；本轮两次都差点只凭印象判断方向（先写反、后靠 `comm` + `diff` 才定准）。
- **命令块要标注它属于哪个阶段**：§9.3 我一度把「`cd relaykit && ... -run TestGolden`」写成「阶段 4 之后」的命令——但按 12.16.2，阶段 4 只改调用点、**测试文件仍在 `service/`**，搬进 relaykit 是阶段 6 的事。阶段 4 用 `cd relaykit` 会**找不到测试**。**凡写命令，先问「这条命令在它标注的阶段能不能跑起来」**（文件在不在、包存不存在）。
- **同名对象的字段值相近时会互相串味**：本轮我把上游 `response/claude_to_openai` 的 `model` 说成 `gpt-test`（实为 `claude-test`，`gpt-test` 是 stream 那条的），又把「两侧 model 相同」写成通用结论（本地 stream 快照根本没有 `model` 字段——它没有 `message_start` 事件）。**核对同名对象时逐个取值，不要用一个对象的值概括另一个；写「两侧相同」前先把两侧都打印出来。**

**第二十轮（2026-10-05）——判据的数字看不见新引入的文件**：第十八轮新建的 `service/convert_golden_test.go` import 宿主 `dto`/`constant`/`types`，而阶段 2 要把 `dto` 迁进 relaykit。实测**它不在「327 个 import 点」里**——`dbfbc096f` 的 327 = 205 非测试 + 122 测试，而该文件在基线上**根本不存在**（`git cat-file -e dbfbc096f:service/convert_golden_test.go` 报 does not exist），工作区已漂到 341。所以阶段 2 一迁 `dto`，这个测试**编译失败**，而阶段 2 判据「327 个 import 点全部验证」**恰好以那个旧数字为界，覆盖不到它**——判据会绿而测试是红的。

**教训**：

- **判据里的数字是「快照」，新引入的产物不在其中**。凡「N 个 X 全部验证」型判据，必须问「这 N 是哪个时点数的？此后我又新增了几个 X？」。本例中的 `N` 是基线 `dbfbc096f` 的，而基线测试是基线**之后**才有的。**这与第十一轮（拷贝进来的上游测试变成硬门禁）、第十九轮（基线测试的生命周期无归属）是同一个结构：某样东西的存在/变化没有被任何判据覆盖。**
- **判据的覆盖范围要用「集合」而非「数字」表述**。把「327 个 import 点全部验证」写成「**所有** import 宿主 `dto` 的文件都改完（含测试文件，且以开工时的清单为准）」就不会漏——**数字会过期，集合的界定方式不会**。
- **顺带核实了一件此前只是断言的事**：12.0.8 判 `Delta`/`Function`/`GenerationConfig` 三处 tag 差异为「空操作」（struct 值使 `omitempty` 失效），本轮打开字段确认——`dto/openai_response.go:86` 的 `Delta` 确为 `ChatCompletionsStreamResponseChoiceDelta` **值类型**、`dto/openai_request.go:240` 的 `Function` 确为 `FunctionRequest` **值类型**。故阶段 2 迁 `dto` 后**基线快照不应因这 3 处 tag 而漂移**；若漂移则另有原因，须查字段对齐或实现改动，而不是归因到 tag。**「空操作」这个结论第一次被独立复现。**

**第二十一轮（2026-10-05）——走判据间的依赖，找到「逐字节一致」的隐藏前提**：上一轮建议的方向是「判据与判据之间是否互相矛盾」，本轮走了一遍六个阶段的判据依赖链。**没有发现判据互相矛盾**，但发现阶段 4 的「8 个快照逐字节一致」有一条**未写出的前提**：

- **`json.MarshalIndent` 按结构体声明顺序输出**。若两侧 `dto` 类型的字段顺序不同，「逐字节一致」会在**语义完全等价时也失败**——判据本身不可达，而不是发现了回归。两侧 `dto` 恰有 33 个类型字段双向不同（12.0.1），顺序不同的可能性真实存在。本轮**机械核验**（正则抽 `struct` 体字段名序列）了 fixture 涉及的 4 个输出类型：`OpenAITextResponse`(7/7)、`ClaudeResponse`(12/12)、`GeminiChatResponse`(共有 3/3)、`GeneralOpenAIRequest`(共有 61/61) **共有字段相对顺序全部一致**。**但这是一次性抽查**——已写入文档，并注明「阶段 4 若出现『内容相同、顺序不同』的差异，先核字段顺序再谈回归」，且可扩 `tools/relaykit-dto-compat/` 加一个「顺序」维度机械判定。
- **顺带核实基线快照与 12.0.8 的 15 处差异无重叠**：本基线的 5 个 fixture 只用 `dto.ChatCompletionsStreamResponse`、`dto.ClaudeRequest`、`dto.GeminiChatRequest`、`dto.OpenAITextResponse`、`dto.UserSetting`，与 12.0.8 涉及的字段（`Delta`/`Function`/`GenerationConfig`/`IncludeThoughts`/`Text`/`Arguments`/`CreatedAt`/`SupportedEndpointTypes`）**无一重叠**。故阶段 2 的对齐**不应**改变这 8 个快照；若漂移，原因在别处。**这是「阶段 2 判据要求 `TestGolden` 仍绿」这条能否成立的关键前提**——它成立，判据才有意义。

**教训**：

- **「逐字节/逐字段一致」型判据，先确认两侧的可比性是同构的**——序列化顺序、零值与缺失的表示、浮点格式、时间格式，任何一项不同都会让判据在语义等价时失败。**写「应逐字节相同」之前，先问「两侧的序列化规则是否相同」。**
- **一次走判据依赖链没找到矛盾，本身就是有价值的结论**（说明六个阶段的判据在顺序上自洽），但它**不等于判据已完备**——本轮的收获全在「判据的**前提**」而非「判据的矛盾」上。**「没矛盾」与「没缺口」是两回事**，与第八轮「零发现不等于完备」同源。
- **判据要能自动执行，才等于判据**——阶段 0 的 CI 门禁若不含 `TestGolden`，阶段 2/4/6 三个判据就只在本机手工跑时有效，PR 上无人拦截。**每写一个判据，顺手问「它在 CI 里吗」。**

**第二十二轮（2026-10-05）——照抄上游的 CI 门禁有三处不够**：阶段 0 要「建立 CI 门禁」，§9.3 说照抄上游 `ci.yml:43-58`（两个 module 各 `go vet`+`go build`，再 `make test`）。实测三处照抄即错：

1. **挂载点没说**。上游 `ci.yml` 触发于 `pull_request`；本仓库唯一在 PR 上触发的质量类 workflow 是 `pr-check.yml`，它用 **`pull_request_target`**——在 base 分支上下文运行，**无法检出 PR 代码来测试**。规格此前只说「建立 CI 门禁」，没写挂哪个 workflow、用哪个事件。
2. **`make test` 目标本地不存在**。本地 `makefile` 实测 9 个目标（`all`/`build-frontend`/`build-frontend-classic`/`build-all-frontends`/`start-backend`/`dev-api`/`dev-web`/`dev-web-classic`/`dev`），**无 `test`**。（顺带确认工作区的 `Makefile` 是 macOS 大小写不敏感文件系统的同名别名，与 `makefile` 内容完全相同、不被 git 跟踪。）
3. **上游 `make test` 不含 `-count=1` 与 `-shuffle`**。实测命令是 `GOWORK=off go test $$root_packages` 与 `cd relaykit && GOWORK=off go test ./...`，两个 flag 都没有。而本仓库有已知的跨运行状态泄漏（命名 in-memory DB 未关闭、包级 limiter 单例、`sync.Once` 捕获 Redis client）——**`-count=1` 绿会隐藏失败**。照抄上游等于把这个盲区带进 CI。

**更要紧的是第 4 点，本轮才意识到**：`TestGolden` 是阶段 2/4/6 三个判据的**共同依赖**，而**规格此前从未把它写进任何 CI 表述**（实测全文 `TestGolden` 无一处出现在 CI/门禁语境）。若它不在 CI 里，那三个判据只在本机手工执行时才成立。已写入 §9.3。

**第二十三轮（2026-10-05）——「须重新核对」不是可执行指令，给它一个命令**：阶段 1 要求「把 commit 记入 `relaykit/VERSION`」，并说后续跟进上游时**「重新核对本文档的行号引用」**。全文实测 **10 处唯一 `relaykit/<path>.go:<line>` 引用**（13 处出现、9 个文件），当前锚点下 **10/10 全部可解析**——但**没有任何工具保证它继续准确**。这与第十五轮把四维比较工具化是同一条理由：**能写成代码的核查规则不要留在文档里等人手工执行**。

新增 `scripts/relaykit-citation-check.py`（按仓库既有 `scripts/*.py` + `*.test.py` 模式，用 `importlib` 加载 + `unittest`）：从本文档抽出全部引用，逐条在指定 ref 下检查**文件存在、行号在范围内、该行非空白**，只抓机械失效、**不断言该行内容**（引用指向的符号变了仍需人工复核）。**10 个单测**，含「引用不存在的文件」「行越界」「指向空行」「混合通过与失败各自报出」四类。**反向验证**：把 `values.go:32` 改成 `:99999` → 退出码 1 且报 `out of range`；改成 `nonexistent.go:32` → 退出码 1 且报 `file not found`；还原后退出码 0，文档 `git diff --numstat` 为空。

顺带把阶段 1 的两个隐含前提**核实**（此前是推断）：上游 relaykit **当前没有版本文件**（顶层仅 `README.md`/`go.mod`/`go.sum` + 4 个目录），故新建 `relaykit/VERSION` **不与上游冲突**；`relaykit/relayconvert/boundary_test.go:53` 的过滤条件是「只处理 `.go` 且非 `_test.go`」，且遍历范围是 `kitDirs`（`relayconvert`/`dto`/`types`/`reasonmap`），故**新增 `VERSION` 文件不会触发边界门禁**。

**顺带修正两处自己的数字错**：本地 `makefile` 目标数先写 8（实为 9）；`pr-check.yml` 的构成先写成「只跑 anti-slop」（漏了它其实有且仅有 1 个 `uses:` 步骤、**0 个 `run:`**，故「只跑 action 不跑命令」的说法才准确）。

## 边界与未验证项

- 本记录及设计文档**未修改任何生产代码**。第十八轮起已实际运行 `go test ./service -run TestGolden` 与全包 `go test ./service`（见该轮），但「编译失败」类结论仍未逐条 `go build` 复现
- 第十九轮给出的 relaykit 入口签名、`internal/` 不可达、上游 `golden_test.go` 的符号冲突与矩阵排除条件，均读自 `upstream/main` 的源码（**签名级证据**），**未经编译验证**——relaykit 目录在阶段 1 才拷入本仓库。`convmeta.Meta` 的构造方式、改指后快照是否逐字节一致，须到阶段 4 实测
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
| `b32a308e9` | 补全本地独有能力盘点（本节 5.1 记录的修正） |
| `047d0ca09` | 第五轮架构审查的修正（新增 §12.18 的 5 项未决） |
| `320f43b5b` | 第五轮事实核验的 6 处修正（新增未决项 26，§12.18 增至 6 项） |
| `eb286da9d` | 独立复核：行号、计数与门控可达性（8 处修正） |
| `39a0c970c` | 独立复核：5 个计数与 1 个口径标签 |
| `4181c5b62` | 独立复核：上游 shim 规模与 5 个过期数字 |
| `eec2dd4b1` | 独立复核：修正上一轮自己引入的一处能力断言 |
| `c4d21ce49` | 独立复核：同步审计记录、修正分支比对取证（`relaykit/` 68 文件 `3910+/746−`、toolconv 差异为功能新增） |
| `7b8d478d7` | 第六轮：`channel_settings.go` 方法数 41→42、补计数口径、拆分 §12.9 单元格 |
| `c926fb0e9` | 第七轮：`state` 的论据纠正、`claudeCapabilitiesFor` 10 个模型 / `:35-78`、内建转换器计数（当轮误写为 8，第十二轮改为 12）、`var` 别名、`gemini.go:112` |
| `fffa65533` | 第九轮：执行层补完——阶段 6 矛盾、relaykit 源码获取与版本锚定、`IsStream` 影响面、§8.1 回滚止损点；关闭原第 26 项 |
| （本轮·第十一轮） | 跨阶段数据流走查：发现 `boundary_test.go` 是阶段 2 的隐性硬门禁（`dto` 的 24 处宿主/gin import 必须清零），补 §7 硬依赖第 2 条、阶段 2 判据、「阶段 2 的前置」整段、§12.0.4、§9.3 注、§11 注、第 10 节第 21 项、§12.17 #21 |
| （本轮·第十二轮） | 全维度复核推翻第七轮的转换器计数（8→12），修正 §3.1、审计记录两处与本文档汇总 |
| （本轮·第十三轮） | 关闭未决项 21/22/23（dto 完整差集、import 图无环、字段对齐 16 需动作/17 纯增益）；修正「relaykit 独有 5 个方法」为全包 31 个、补 394/273 的「含测试」口径；**新发现 `relay/channel/claude/relay-claude.go:255` 459 行入向转换器被 3 通道共用且第 8 节遗漏**（未决项 28）；§4.5.5 六条未核验边界全部转为结论 |
| （本轮·第十四轮） | 补测字段 **JSON tag** 与 **Go 类型** 两个维度（13 轮从未查过）：tag 差异 5 处（静默改变序列化）、类型真差异 3 处（编译期破坏，`IncludeThoughts` 一项 8 处赋值点）、`interface{}`/`any` 拼写差异 8 处；**推翻上一轮自己写的「17 个纯增益」**，新增 §12.0.6 与未决项 30 |
| （本轮·第十五轮） | 把四维比较工具化：`tools/relaykit-dto-compat/`（7 个测试）+ `scripts/relaykit-dto-compat.sh`（含 `--strict` 门禁，经反转验证）。**工具当场抓出上一轮两处分类错误**：类型真差异 3→6、签名差异 13→12（后者含 1 处 `interface{}`/`any` 等价拼写）。编写中工具的嵌入字段维度自身有 bug，由单元测试捕获 |
| （本轮·第十六轮） | 关闭未决项 27/28/29：三个符号换 stdlib（是 `encoding/json` 薄包装，此前误判为宿主能力）、`ResponsesRequestMetadata` 判为宿主类型（488 行文件 + 650 行扫描器依赖）、通道级转换器**收敛到 relaykit**（三类宿主耦合 relaykit 侧均有注入机制：`Options` 钩子 / `MediaResolver` / 内置 schema）、`EffectiveTools()` 连缓存字段迁入 relaykit。未决项 6→3 |
| （本轮·第十七轮） | 关闭未决项 30：15 处 tag/类型差异**全部采用 relaykit**。**推翻第十四轮对 tag 差异的判断**——5 处中 3 处因字段是 struct 值而 `omitempty` 无效，是空操作而非「静默改变输出」；另 2 处风险逐点排除。类型 6 处含 1 处纯增益（`IntValue.MarshalJSON` 与 int 相同）、3 处改读写点、2 处改 import。未决项 3→2 |
| （本轮·第十八轮） | 关闭未决项 25：建成 `service/convert_golden_test.go` + 8 个快照的本地基线（首次实际运行 `go test`）。**建基线时发现第一版 fixture 全用非 DeepSeek 模型名，导致 thinking 门控整条路径未被覆盖**——补 DeepSeek 侧两个快照，把门控两侧都钉住。实测基线与 `HEAD` 等价（工作区他人未提交改动对快照为 no-op）。反向验证三处扰动均失败。未决项 2→1（第 24 项 `openaicompat` tool 循环重构仍开放，范围外） |
| （本轮·第十九轮） | 发现并修补**第十八轮自己引入的缺口**——基线的生命周期无归属（其 6 个被调函数定义在阶段 6 要删的 `service/convert.go`）。修正过程又推翻自己三次：`internal/` 包宿主不可 import、请求入口无 `ctx`、阶段 6「整体移入」会因同包同名符号而编译失败。四项修正落入阶段 4 判据 / 处置清单 12 / 阶段 6 / §9.3；新增 §12.16.2、§12.16.3。**独立印证上游 24 快照刻意排除 `openai_to_claude`（`golden_test.go:323-325` 的 `continue`），正是本地 thinking 门控所在路径** |
| （本轮·第二十轮） | 发现阶段 2 判据「327 个 import 点全部验证」**覆盖不到第 18 轮新建的基线测试**——327 是基线 `dbfbc096f` 口径（205 非测试 + 122 测试），而 `convert_golden_test.go` 在基线上不存在；阶段 2 迁 `dto` 后它会编译失败，判据却仍绿。补阶段 2 判据与前置节。**顺带独立复现了 12.0.8 的「3 处 tag 差异是空操作」**（`Delta`/`Function` 确为 struct 值） |
| （本轮·第二十一轮） | 走六个阶段判据的依赖链：**无矛盾**，但发现阶段 4「8 个快照逐字节一致」有未写前提——`json.MarshalIndent` 按声明顺序输出，两侧字段顺序不同则语义等价也失败。机械核验 4 个输出类型共有字段全部同序；并确认基线 fixture 与 12.0.8 的 15 处差异无重叠 |
| （本轮·第二十二轮） | 核阶段 0 的 CI 门禁：照抄上游 `ci.yml` **三处不够**（挂载点未定、本地无 `make test` 目标、上游 `make test` 不含 `-count=1`/`-shuffle` 会继承已知跨运行泄漏盲区）；**且 `TestGolden` 从未进入任何 CI 表述**，而它是阶段 2/4/6 判据的共同依赖。已写入 §9.3 |
| （本轮·第二十三轮） | 核阶段 1 的版本锚定可执行性：原文只写「须重新核对行号引用」而**无任何可执行手段**。实测全文 10 处唯一 `relaykit/<path>.go:<line>` 引用、**当前 10/10 全部可解析**，但无工具保证。新增 `scripts/relaykit-citation-check.py` + 10 个单测（按仓库既有 `scripts/*.py` + `*.test.py` 模式），经**反向验证**（`:99999` → 退出码 1 报 out of range；`nonexistent.go` → 报 file not found；还原后退出码 0）。另实测：上游 relaykit 无版本文件、且 `boundary_test.go:53` 只遍历 `kitDirs` 下的 `.go`，故新建 `relaykit/VERSION` 既不冲突也不触发边界门禁 |

文档当前 1554 行，位于 `docs/superpowers/specs/2026-09-30-relaykit-migration-design.md`。
