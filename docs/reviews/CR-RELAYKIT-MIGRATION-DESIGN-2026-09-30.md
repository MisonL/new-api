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

**第二十四轮（2026-10-05）——「15 个方法」复现为「13 个要新增」**：阶段 3 写「`RelayInfo` 实现 `convmeta.Meta` 全部 15 个方法」。机械枚举 `convmeta.Meta`（`convmeta/meta.go:18`）：**确为 15 个**。但比对本地 `relay/common/relay_info.go`（HEAD 口径）现有 26 个方法后，**只有 2 个重名**——`GetEstimatePromptTokens`、`AppendRequestConversion`（本地 `:897`、`:844`），**其余 13 个本地完全没有**。所以真实工作量是**新增 13 个方法**，不是「实现 15 个」。

而这 13 个**上游已全部实现**（`upstream/main:relay/common/relay_info.go:825-953`，且 `:823` 就有编译期断言 `var _ convmeta.Meta = (*RelayInfo)(nil)`），阶段 3 实际是**照抄**而非设计。其中 **12 个是纯访问器**，**只有 `ConvOptions` 需新写逻辑**——它 import `model_setting.GetClaudeSettings/GetGeminiSettings`（本地 `setting/model_setting/claude.go:55`、`gemini.go:53` 实测存在），并按 relay 会话缓存 `convOptions` 字段（本地 `RelayInfo` **无**该字段，须新增）。阶段 3 判据已改为「13 个方法逐个落地」。

**教训**：

- **「实现 N 个方法」里的 N 是接口的方法总数，不是工作量**。工作量的正确单位是「**已存在几个**」，须用集合差集算（本地方法名 ∩ 接口方法名）。**凡写「实现 N 个 X」，必须同时给出「其中 M 个已存在、须新增 N−M 个」**，否则阶段判据无法估工作量、也无法在阶段 3 定位「还差哪几个」。
- **如果上游已有实现，先确认它能不能照抄**——本轮的 13 个方法上游全有，阶段 3 从「设计任务」降级为「移植任务」，这两者的风险完全不同。

**第二十五轮（2026-10-05）——「反向新增」与「照抄」是两类任务**：本轮核阶段 4 处置清单第 4、5 项，两项都是**从宿主往 relaykit 反向新增**（前二十四轮核的全是「照抄上游」）。实测四件事：

1. **第 4 项上游无同名方法**。`ParseContentStrict`（`dto/openai_request.go:882`）与 `ParseToolCallsStrict`（`:624`）在 `upstream/main:relaykit/` 里 `git grep` 为空——**确属本地专有**。两方法只用 `common.{GetJsonType,Marshal,Unmarshal,RawMessage}` + stdlib，前三个 `kitutil` 有等价物，**但 `kitutil` 不提供 `RawMessage` 类型别名**（本地 `common/json.go:9` 是 `type RawMessage = json.RawMessage`；`kitutil/json.go` 里 `RawMessage` 只作为 `json.RawMessage` 的参数类型出现）——故迁入时 `[]common.RawMessage` 须改 `[]json.RawMessage`。**原处置项只写「迁入 `relaykit/dto`」，没写这个改名动作。**
2. **第 5 项「门控留宿主」没写落点**。think 标签族实测拆两半：纯函数 `splitThinkTaggedText`(`:554`)、`indexASCIIFold`(`:597`)、`appendVisibleThinkText`(`:629`) **只依赖 `strings`**，可无改动迁入；门控 `shouldExtractThinkTaggedClaudeHistory`(`:489`)、`shouldExposeReasoningAsClaudeThinking`(`:526`) **依赖宿主 `*relaycommon.RelayInfo`**，须留宿主。**但门控的 3 个调用点全在 `service/convert.go` 内**（`:103`/`:749`/`:1049`）——**阶段 6 删该文件主体，调用点随之消失**。故「门控留宿主」必须落到**注入机制**（请求方向 `convmeta.Options` 钩子、响应方向 12.4 的 relaykit 内门控），原处置项未提。
3. **上游 relaykit 无 think 标签提取函数**（`git grep -E "func [a-zA-Z]*[Tt]hink[a-zA-Z]*\(" upstream/main -- 'relaykit/**'` 只命中 Qwen thinking budget 等无关符号），确认本地专有；上游 `to_oai_chat_req.go` 的 think 相关只有 `:82-88` 的 `-thinking` 后缀，与标签族无关。
4. **一个幻影符号**：§5 把 `extractThinkTaggedHistory` 当函数名写，实测它是 `service/convert.go:103` 的**局部变量**（`:=` 赋值，内容为门控函数返回值），真函数是 `shouldExtractThinkTaggedClaudeHistory`。

**顺带一处自纠**：我一度把 `ParseContentStrict`/`ParseToolCallsStrict` 的行号对调——起因是 `sed -n '882p;624p'` 按**文件行序**输出（624 先出），我按参数顺序读成了倒序，于是「修正」反而改错，第二次才改回。**凡一次读多个行号，输出顺序不等于参数顺序**，要么分行打印，要么用 `grep -n` 逐个核。

**教训**：

- **「反向新增」与「照抄」是两类任务，风险点不同**——照抄的风险是抄错，反向新增的风险是**边界与依赖**（会不会 import 宿主、缺的符号怎么办、删除宿主文件后调用点去哪）。**核一类任务不能拿另一类的检查清单。**
- **「留宿主」必须同时说清「放哪个文件、谁调用它、经哪条钩子」**——留宿主的函数若其调用点在被删文件里，它就是不可达的。

**第二十六轮（2026-10-05）——「避免分叉」与「必然分叉」没有和解**：核阶段 5（唯一还没读过的阶段行）。实测发现规格**必然对上游 relaykit 产生本地分叉**——补丁 1、补丁 2、12.4 的门控都要**改写 relaykit 内部的 `.go` 源码**（`internal/claude_messages/to_oai_chat_req.go:181`、`to_oai_responses_req.go:165`、`internal/oai_chat/to_claude_messages_resp.go`），12.4.2 还要给公开包 `convmeta.Options` **新增字段**。而规格里 12.9 写「不采用分支，**避免与 main 产生不可合并分叉**」、12.15.4 写「优先走接线方案，**避免对 relaykit 产生分叉**」——**三处补丁与两处「避免分叉」的基调直接矛盾**，而规格**从未汇总「本迁移改了 relaykit 哪些文件」**。

更关键的是：`relaykit/VERSION`（第 23 轮加的）**只记上游锚点一个字段，回答不了「我们改了什么」**。锚定的意义正是跟进上游时能一眼看出「哪些差异是本地有意为之、哪些是上游漂移」——没有改动清单，锚定只锚住「上游是哪天」。

已补「本迁移对上游 relaykit 的本地改动清单」表（8 条：补丁 1 的 2 个 switch、补丁 2 的 dto presence、12.4 的响应门控两条入口、`convmeta.Options` 新增钩子、think 标签纯函数、两个 `*Strict` 方法、`EffectiveTools()`、错误符号），每条带**核实过的落点行号**，并要求另立 `relaykit/LOCAL_PATCHES.md` 维护。

**顺带让引用门禁真正覆盖新表**：新表里的路径原写成 `relayconvert/...`（无 `relaykit/` 前缀），而 `scripts/relaykit-citation-check.py` 的正则要求 `relaykit/` 前缀——所以**新增的 6 处引用一度完全在门禁之外**（检查器仍报 10/10 绿）。补前缀后覆盖数升到 **16/16**。**这是个假绿**：门禁看起来在跑，实际漏掉了新写的那批引用。**凡新增引用，先确认工具的正则能匹配它。**

**教训**：

- **「避免分叉」与「必然分叉」要和解，不能各说各话**。本迁移必须改上游源码，正确表述是「**基线选 main（不打分支分叉），但接受本地 hunk 分叉，并维护改动清单**」——两件事被混成一句「避免分叉」，导致改动清单从未被写出来。
- **锚定文件要成对**：`VERSION`（上游是谁）+ `LOCAL_PATCHES.md`（我们改了什么）。只有前者，升级上游时无法区分「本地有意」与「上游漂移」。
- **新增引用后立刻重跑门禁，并核对覆盖数是否真的上升**——门禁不报错不等于它在管你（正则前缀不匹配就是静默漏检）。

顺带把阶段 1 的两个隐含前提**核实**（此前是推断）：上游 relaykit **当前没有版本文件**（顶层仅 `README.md`/`go.mod`/`go.sum` + 4 个目录），故新建 `relaykit/VERSION` **不与上游冲突**；`relaykit/relayconvert/boundary_test.go:53` 的过滤条件是「只处理 `.go` 且非 `_test.go`」，且遍历范围是 `kitDirs`（`relayconvert`/`dto`/`types`/`reasonmap`），故**新增 `VERSION` 文件不会触发边界门禁**。

**顺带修正两处自己的数字错**：本地 `makefile` 目标数先写 8（实为 9）；`pr-check.yml` 的构成先写成「只跑 anti-slop」（漏了它其实有且仅有 1 个 `uses:` 步骤、**0 个 `run:`**，故「只跑 action 不跑命令」的说法才准确）。

**第二十七轮（2026-10-05）——门控排在阶段 5，但阶段 4 的判据先红**：核阶段 5 判据能否验证 12.4 要求的语义条件（门控必须在索引分配之前）。**起点是错的**：我原本以为缺口只是「判据没写这条」，实测后发现更硬的问题是**阶段顺序本身自相矛盾**。

事实（均实测）：

1. **阶段 4 判据要求「`TestGolden` 改指 relaykit 后 8 个快照逐字节一致」**，其中 4 个门控快照在非 DeepSeek 侧要求「reasoning 被丢弃」。
2. **上游 relaykit 默认无条件暴露 reasoning**：`ResponseOpenAI2Claude(resp, nil)`（`oai_chat/to_claude_messages_resp.go:449`）签名收 `info` 但**函数体 0 次引用**（`grep -c info` 在 :449-520 只命中签名行本身），`:463` 的 `if reasoningContent != ""` 无任何门控；整个文件 `grep -c "Options\|Retain"` = **0**。`Values.ConvOptions()`（`convmeta/meta.go`）在 `Options` 未设时返回 `&Options{}`（钩子 nil）。
3. **12.4 的门控排在阶段 5**，而阶段 4 就要完成切换。**切换而不门控 → 非 DeepSeek 快照多出 thinking 块 → 阶段 4 判据必红**；豁免条款只认「处置清单第 6 项（9.1/9.2）」，门控不在其中 → **阶段 4 无法通过**。

**决定**：门控随阶段 4 的切换同时落地（新增处置清单第 13 项），阶段 5 只核验其位置语义；阶段 5 判据补上「4 个门控快照逐字节一致」这一可执行条件。

**连带修出四处 12.4 自身的错误**（都是我在同一轮里发现的）：

4. **钩子 nil 语义写反了**。12.4.2 写 `nil = 不保留（与上游一致）`。前半句与后半句不能同真——上游没有这个钩子，其行为是**无条件保留**。按原语义实现，所有不设钩子的调用点（上游自身测试、独立 relaykit 用户）行为从「暴露」翻转成「不暴露」，是**破坏性默认值变更**。正确语义是 `nil = 保留（维持上游默认）`。**这一改同时让上游两条依赖默认暴露的测试保持绿色、无需改动**（`to_claude_messages_resp_test.go:82`、`:125`，该文件 0 处 `Options:`）；按原语义则两条必红。**故这两条测试是 nil 语义实现的验证条件，不是待改对象**——本地改动清单第 9 行据此从「改写」改为「不改动，但须保持绿色」。
5. **单参钩子复现不了三名字门控**。本地 `HasDeepSeekModelName`（`convert.go:537`）综合 `Model`/`OriginModelName`/`UpstreamModelName` **三者**判定，钩子 `func(modelName string) bool` 只收一个。12.4.2 写「加**一次**判断」会丢掉另两个名字 → 公开名非 DeepSeek 但路由到 DeepSeek 上游时 reasoning 被错误抑制，是**行为回归**。解法同上游 `ApplyReasoning` 对 `ShouldPreserveThinkingSuffix` 的连调两次（`shared/claude/reasoning.go:25-26`）：调用点对每个名字各调一次取或。
6. **插入点写错一层**。上游是 `if reasoning != "" { thinking } else { text }` **互斥**结构。只在内层 `if reasoning != ""` 加判断是错的：门控关闭时该分支跳过，`else`（text）也不执行，**同 chunk 的 text 被静默丢弃**。正确做法是在**外层条件之前**把被抑制的 reasoning 归零（`if reasoning != "" && !retain { reasoning = "" }`）。「在索引分配之前」的精确含义是**进入 `reasoning/text` 二选一之前**，不只是 `stopOpenBlocksAndAdvance()` 之前。
7. **同 chunk reasoning+text 是本地能力，上游没有，且未列入 9.1**。上游互斥结构下同 chunk 只发 thinking、丢 text（若还含 tools 则两者都丢）；本地两个独立闭包三者都发，有专门用例 `TestStreamResponseOpenAI2ClaudeEmitsThinkingTextAndToolBlocksFromOneChunk`（`convert_claude_stream_test.go:119`）。**这是能力差异不是严格度差异**——12.16.1 说「阶段 4 的待办是确认既有用例仍通过、失败得其所」，但这条用例失败**不得其所**。已补入 9.1 第 7 条并要求明确取舍。

**顺带**：12.4.3 的「影响」段写门控落点含 `to_claude_messages_stream_resp.go`，但该文件只在 `internal/oai_responses/` 下（`oai_chat/` 无此文件），而 `oai_responses` 一族不接收 `Meta` 且本地不涉及（12.4.2 已述）——已修正。

**引用口径自查**：新增引用时先写成工作区口径（`convert.go:800/830/857`、`:544`），而文档既有引用是 **HEAD 口径**（如 `:794` 正是 `if !allowClaudeThinking`）。工作区当时有**其他会话的 +7 行改动**（在 `:59` 处加 `ReasoningEffort` 透传），使后续行号整体偏移。已全部改回 HEAD 口径（`:793`/`:823`/`:850`、`:537`）。**这正是第 18 轮确立的「HEAD 口径」纪律的又一次应用**。

**教训**：

- **判据引用了一个「后续阶段才产出」的行为时，先检查产出的阶段早于判据的阶段**。本轮的真问题不是「判据不够细」，而是**判据依赖的行为排在判据之后**——这类矛盾在逐条读判据时看不出来，只有把「判据要求的状态」与「阶段表里的动作顺序」放在一起才暴露。
- **默认值是契约的一部分，方向写反等于破坏性变更**。`nil = 不保留` 与 `nil = 保留` 在「没人设这个钩子」时行为相反；而 relaykit 是**公开模块**，不设钩子的调用方不只是本仓库。
- **「加一次判断」这种措辞要落到代码结构上验证**。本轮三处错误（nil 语义、单参、插入点）都藏在「加一个 if」这五个字里，读起来无懈可击。
- **能力差异与严格度差异要分开处理**。12.16.1 的「失败得其所」对错误路径成立，对**本地刻意支持的渲染能力**不成立——后者失败是回归，不是「上游更宽松」。

**第二十八轮（2026-10-05）——「须实测」的两条边界实测后都不成立，而依据表里写着「已内置」**：核 §12.15.4 被多个判据引用为「已实测」的两条边界（schema 行为对齐、`text/*` MIME 归属）。它们都写着「阶段 4 开工前必须实测比对」而**没给命令**。实测结论：**两条都不成立**，且 12.15.4 的依据表把 `FunctionParametersToInputSchema` 标为「已内置」——**掩盖了一个行为回归**。

事实（逐条读 `upstream/main` 与 HEAD 源码）：

1. **边界 1 不成立：schema 归一化有 4 处差异**（12.15.4.1 表）。本地 `normalizeClaudeInputSchema`（`relay/channel/claude/relay-claude.go:233`）用 `common.Any2Type`（JSON 往返）并在失败时报错；上游 `FunctionParametersToInputSchema`（`relaykit/relayconvert/internal/shared/claude/schema.go:3`）用类型断言、失败**静默**产出 `{"type":"object","properties":{}}`。**D1 是行为回归**：非法 `input_schema` 从「明确报错」变成「静默降级后发给 Claude」。D3（`type` 非 string）同类。
2. **边界 2 不成立：`text/*` 分支仍在用且上游完全没有**（12.15.4.2 表）。上游 `oai_chat/to_claude_messages_req.go` 的 `switch mediaMessage.Type` 只有 `case "text"`（字符串内容）与 `default`，`default` 里 `application/pdf` 前缀→`document`、**其余全部→`image`**——**上游会把 `text/plain` 文件当图片发给 Claude**。
3. **边界 3 是本轮新增的同类差异**：上游**没有 `ContentTypeFile` 这个 case**（本地有独立分支，支持 `file_data` 内联 / `file_id` / `file_url` / 拒绝 `application/octet-stream`），落进 `default` 与 `image_url` 同路；`ToFileSource()` 返回 nil 时**静默 `continue`**（内容被丢弃无错，本地显式报错）；`ContentTypeAudio`/`ContentTypeVideoUrl` 本地显式报错、上游静默当 image；上游**没有 `normalizeClaudeMimeType`**（`ResolveBase64Data` 纯转发给宿主注入的 resolver，自身零归一化）。**M1 是能力缺失，M2/M4/M5/M6 是行为回归，M3 是归一化缺失，M7 是本地自身不一致**（`:122` 用前缀判 pdf、`:197` 用精确相等）。
4. **9.2 表没有 schema 归一化与媒体分派这两条**——已补为第 7、8 条，处置清单第 6 项的计数由「6 条」改为「8 条」。
5. **12.15.4 的退路条款被激活，但它本身写得不对**：原文说「把该函数迁入 relaykit」——而实测发现的不一致部分**本来就是纯函数**（`normalizeClaudeInputSchema` 只依赖 `common.Any2Type` + `fmt`），它们不是「绑定宿主业务」而是「绑定协议转换」，恰恰本就该在 relaykit 里。所以正确处置不是「迁入」（那是现状）而是**改 relaykit 现有实现**。这与「优先走接线方案、避免对 relaykit 产生分叉」**直接冲突**：要保行为一致必须改上游源码。已按「保行为优先于避免分叉」处置，并登记进本地改动清单（新增第 10、11 行）。

**顺带修一处门禁盲区——本轮是第 26 轮那类假绿的复发**：检查器报「18/18 绿」，但文档里**23 处唯一简写引用**（`internal/oai_chat/...`、`oai_chat/...`、`convmeta/...`、`shared/claude/...`）**全部不在门禁内**——旧正则要求 `relaykit/` 前缀，而第 26 轮补的正是那些带前缀的。**第 27 轮我新写的 `:345`、`:247`、`:463` 也在这批盲区里。**

修法：给 `scripts/relaykit-citation-check.py` 加**显式映射表** `SHORT_PACKAGES`（逐个核实过真实目录，不用「试前缀」——试探会把 `shared/claude/` 解析成不存在的 `relayconvert/shared/`），覆盖数 18 → **34/34**。**宿主路径一律不扩展**（`relay/`、`service/`、`controller/`、`common/`、`setting/`、裸 `dto/`、`types/`）——宿主有自己的 `dto/`，文档里的 `dto/...:NN` 指宿主，误扩展会造出假引用；这一条由单测覆盖。测试从 10 个增到 **15 个**（新增「简写路径展开」「宿主路径不展开」「全路径不变」），**先写测试确认 3 个失败再实现**。反向验证：短路径越界 → 退出码 1 报 `out of range`；短路径文件不存在 → 报 `file not found`；还原后退出码 0、文档字节一致。

**又一次踩到「示例文本进入门禁」**：我在文档里写「形如 `internal/oai_chat/foo.go:12`」当示例，检查器立刻把 `foo.go:12` 当成待解析引用并报错（35 处、1 失败）。已改成 `<文件>.go:<行>` 占位。这与第 26 轮是同一类错误的镜像——那次是**真引用漏出门禁**，这次是**假引用混进门禁**。

**教训**：

- **依据表里的「已内置」是最容易藏回归的地方**。它读起来像「能力对齐、只差接线」，于是差异被推到「阶段 4 再验」——而阶段 4 的判据根本不会去看它。**凡写「已内置 / 已有对应机制」，必须当场把两侧实现的输入、输出、错误路径逐项列出**。
- **「已实测」是引用别人结论的词，不是自己做过**。12.15.4 两条边界写着「须实测比对」被多个判据引为「已实测」——实际从未比对过。本轮把这类措辞全部改成有结论的句子。
- **门禁的正则决定什么被检查，不只是决定什么被排除**。新增引用格式时要问「这个正则匹配它吗」，扩门禁时同样要问「我会不会把示例、占位符、宿主路径一起吞进来」——**假绿与假红都是正则的副作用**。
- **「优先 A 方案避免分叉」与「实测证明必须走 B」冲突时，先写下这条冲突**，别让「优先」二字把 B 悄悄压掉。第 26 轮的「必然分叉 vs 避免分叉」是同型的第三次复发。

**第二十九轮（2026-10-05）——「严格校验留宿主调用点」没有落点，且差异被少算成 1 条**：核 §12.11（`reasonmap` 宽松 default）。它写着「严格化应在**宿主调用点**做」，另有三处同义复述（处置清单第 9 项、§10 收尾第 12 条、§12.17 汇总表第 12 行）——**没有一处指明是哪个调用点**。本轮实测后确认：这个落点在阶段 6 之后**不存在**。

事实（逐条读 `HEAD:service/convert.go`、`upstream/main:relaykit/...` 与 `convmeta.Options`）：

1. **严格校验的两个唯一调用点都在阶段 6 要删的文件里**。`stopReasonOpenAI2ClaudeStrict` 定义在 `service/convert.go:1125`，只被 `:738`（`StreamResponseOpenAI2ClaudeWithError`）与 `:1057`（`ResponseOpenAI2ClaudeWithError`）调用；阶段表第 6 行删除 `service/convert.go` 主体（2229 行），处置清单第 2 项在**阶段 4** 就删掉 `*WithError` 系列。
2. **宿主 5 个 Claude 响应方向的调用点全都不做校验**。`relay/channel/openai/relay-openai.go:314`、`openai/helper.go:46,246`、`openai/chat_via_responses.go:280`、`gemini/relay-gemini.go:1784`——每一处都只是 `if conversionErr != nil { return … }` 的转发，校验被**委托**给被删的 `*WithError`。（全仓库 `*WithError` 调用点共 9 个，其余 4 个是 Gemini 方向。）
3. **relaykit 侧没有任何注入点**。`reasonmap.OpenAIFinishReasonToClaudeStopReason`（`relaykit/reasonmap/reasonmap.go:31`）是**纯包级函数**、无 receiver、零钩子，两个调用点（`oai_chat/to_claude_messages_resp.go:502`、`oai_responses/to_claude_messages_resp.go:87`）都在 `internal/` 内；`convmeta.Options` 的**全部字段**（`relaykit/relayconvert/convmeta/options.go`）没有一项涉及 finish_reason/stop_reason。**这与 12.4（有 `Options` 钩子）、12.15.4（有 `MediaResolver`）不同**——那两处的「宿主耦合」都真有注入机制，finish_reason 没有。所以「在宿主调用点做」实际意味着**在 5 个 relay 调用点各写一遍前置校验**，5 份重复逻辑，且顺序与 relaykit 内已完成的转换相反（先转换、才发现非法值，只能丢弃结果）。
4. **差异是 5 条，不是 1 条**。9.1 第 1 条与 §12.11 原文只写「未知值报错 vs 透传」。逐条对 `reasonmap` 后：`content_filter` 两侧同为 `refusal`（**无差异**，旧文隐含「有差异」）；`""` 本地 `end_turn`、上游 `default` 返回空串；`function_call` 本地 `tool_use`、上游无此分支而透传成非法 stop_reason；本地 `ToLower`+`TrimSpace`、上游只 `ToLower`。**后三条都是行为回归，不是错误路径**。
5. **`""` 那条会整字段消失**。`dto.ClaudeResponse.StopReason` 是 `string` + `omitempty`（`dto/claude.go:560`、`relaykit/dto/claude.go:510`）；上游**非流式**路径（`oai_chat/to_claude_messages_resp.go:450-495`）在无 choice 携带 `FinishReason` 时 `stopReason` 停在 `""`，序列化时**省略 `stop_reason`**，本地返回 `end_turn`。流式两侧都有 guard（上游 `:220-222`/`:429-431`），无差异。**两条 golden 基线都测不到**：本地 fixture 只有 `finish_reason: "tool_calls"`（快照 `stop_reason: "tool_use"`），relaykit 24 个 golden 只有 `tool_calls`/`stop`/`null`——无空串样例。
6. **`TestResponseOpenAI2ClaudeRejectsUnknownFinishReason` 会编译失败，不是「失败得其所」**。它（`service/convert_claude_request_test.go:900`）直接调 `ResponseOpenAI2ClaudeWithError`——该函数阶段 4 即删。§12.16 的「确认这些既有用例在切换后仍通过」对它不成立；它钉的行为在阶段 6 后无落点。故它是 12.16.1 里**除第 7 条外的第二个例外**。

修法：§12.11 重写为 12.11.1/12.11.2/12.11.3——列出「无落点」的四条证据、把差异补成 5 条、给出**二选一**（A：给 `reasonmap` 加可选注入钩子，登记进本地改动清单第 12 行；B：宿主 5 处前置校验）。同步改 9.1 第 1 条、处置清单第 9 项、§10 收尾第 12 条、§12.17 第 12 行、阶段表第 4 行判据、12.16.1 第二例外、本地改动清单第 12 行、新增未决项 31。引用数 34 → 40（新增 `reasonmap.go:31`、`to_claude_messages_resp.go:502`、`oai_responses/to_claude_messages_resp.go:87`、`oai_chat/to_claude_messages_resp.go:450/495/220/429`、`dto/claude.go:560`/`relaykit/dto/claude.go:510`），**门禁 40/40 通过**；反向验证：扰动 `reasonmap.go:31` → `:9999` 退出码 1 报 `out of range`，改幻影文件 → 报 `file not found`，还原后退出码 0。

**教训**：

- **「留宿主调用点」是一句没有主语的话**。它省略了「哪个文件、哪个函数、由谁调用」——而这三个正是可执行性的全部。**凡写「在宿主侧做」，必须当场写出文件与函数名，并确认它在所有删除阶段之后仍然存在**。第 25 轮（门控留宿主）是同一个坑的第一次，本轮是第二次。
- **「某处有注入机制」要逐个能力核实，不能从一个能力外推**。12.15.4 的「三类宿主耦合都有注入机制」让「finish_reason 也在宿主做」看起来顺理成章，但 `Options`/`MediaResolver` 是**具体某几项能力**的钩子，不是通用注入点。**套件有没有钩子，取决于那个函数有没有接 `Meta`/`Options`**——`reasonmap` 是纯函数，就没有。
- **差异清单要把「无差异」也写出来**。旧文写「严格校验 vs 宽松 default」，读起来是一条差异；实际是 4 条差异 + 1 条无差异。**不逐条列全，就会把「行为回归」误当成「严格度取舍」而默认放行**。
- **测试的「仍通过」前提是它还能编译**。一个调用待删函数的用例，在删除发生的那一刻就不再是「会失败」而是「编译不过」——「失败得其所」这类措辞要先确认被删符号的引用是否已被改指。

**第三十轮（2026-10-05）——顺着一个聚合计数查下去，牵出工具门禁漏掉的一整个维度**：第 29 轮修完 §12.11 后，本轮核 §12.16.2/12.16.3（基线生命周期）。起因是文档里「9.1/9.2 的 **13 处**本地差异」——第 28 轮给 9.2 加了第 7、8 条（schema 归一化、媒体分派）却漏改聚合处。顺着一个数查下去，共 **8 处错误**，其中 5 处是计数、2 处是事实断言、1 处是死代码。

事实（逐条实测）：

1. **聚合计数 13 → 15**：9.1 表 **7 行**、9.2 表 **8 行**（机械数行，非印象）。第 27 轮加 9.1 第 7 条后 13 是对的，第 28 轮加 9.2 两行时只改了处置清单第 6 项，四处聚合处未同步。
2. **「6 个调用点」是 6 个函数、8 个调用点**：`ResponseOpenAI2ClaudeWithError` 与 `StreamResponseOpenAI2ClaudeWithError` 各被调两次（门控两侧各跑一遍）。`grep -c` 实测 8。处置清单第 12 项、§9.3 命令块注释、§12.16.2 决定 1 三处都写成 6。
3. **`mustUnmarshalFixture` 不是「同签名」**：12.16.3 的表头写「与我方同名同签名」，逐行 diff 后 4 行确实同签名，但这一行**不同**——上游 `(raw string, out any)`、本地 `(t *testing.T, raw string, v any)`。同名即冲突（结论不变），但它决定了阶段 6 合并时本地 **8 处调用都得去掉首参**，原表没写。
4. **「与 15 处差异无一重叠」不成立**：至少 3 处重叠——`ChatCompletionsStreamResponseChoice.Delta`（输入侧，流式 fixture 逐 chunk 带 `delta`）、`GeminiChatRequest.GenerationConfig`（输入侧，Gemini fixture 用的就是该类型）、`ToolCallRequest.Function`（**输出侧**，`request/claude_to_claude`… 实为 `request/claude_to_openai` 与 `request/gemini_to_openai` 两个快照都输出 `tools`）。**结论仍成立，但理由不是「无重叠」而是「重叠的几处都是 struct 值空操作」**——`Delta`/`GenerationConfig`/`Function` 三者在 12.0.6 已判为 `omitempty` 无效的空操作。另两处真实变化（`IncompleteDetails.Reason` 属 `OpenAIResponsesResponse`、本基线不走 responses 方向；`ClaudeServerToolUse.WebSearchRequests` 属 `ClaudeUsage`、快照实测无 `server_tool_use` 键）确实无重叠。
5. **12.0.8 的「全部 15 处采用 relaykit」与工具实测不符**：跑 `scripts/relaykit-dto-compat.sh`（`--strict` 通过）得 **tag=5、type=6**，即真正需逐项决定的是 **11 处**，不是 15。且原文自列的三类 5+6+15=26 与句首「15 处」自相矛盾。
6. **12.0.6 的「等价拼写 15 处」实为 21**：工具的「等价拼写」是**独立第三组**（`main.go:166-171`：类型不同且 `spellingsEquivalent` 为真时进 `tagEquivalents`，否则才进 `typeSet`），实测 **21 项 = 20 个字段 + `NewNotify` 的 1 个形参**，涉及 **7 个类型**（`GeminiModel` 13、`DifferenceItem` 2、其余各 1）。原文档自己的枚举逐个相加就是 21，与句首的 15 矛盾——**不需要外部工具就能发现**。
7. **门禁漏掉一整个维度**：`--strict` 的期望数组只有 5 项（fieldSet/tag/type/embed/sig），而工具**打印 6 组**——等价拼写组从未进 gate。这就是「15」能连续十几轮无人察觉的原因。已把 `wantEquiv = 21` 加入 gate 并**反向验证**（改成 20 → 退出码 1 报 `dimension 5 changed`，还原后恢复）。另修「类别 A 的 8 个类型」（实为 7 类型 + 1 函数）与「类别 C 3 个类型共 4 个字段」（实为 4 类型 / 5 字段）。
8. **两个死代码 fixture**：`fixtureOpenAIChatRequest`（`:121`）与 `fixtureClaudeResponse`（`:181`）在全 `service/` 包内**零引用**——文档说「本基线的 5 个 fixture」实际只有 3 个被引用。**声明数与引用数不是一回事。**

修法：8 处全部逐条改正并在正文标注「第 30 轮修正」及原值；`tools/relaykit-dto-compat/main.go` 的 `--strict` 增加 `wantEquiv` 维度。门禁复核：`scripts/relaykit-citation-check.py` 退出码 0（40/40 引用解析）、其单测 15 个通过、`relaykit-dto-compat.sh --strict` 通过、该工具单测 `ok relaykit-dto-compat`。

**顺带澄清一条既有诊断**：IDE 报 `main.go:50` 的 `//go:embed all:web/classic/dist` 找不到文件——**与本轮无关**，`main.go` 未被本轮改动（`git status` 为空），且 `HEAD:main.go` 同样 embed 该路径；`web/classic/dist` 不存在是因为前端未构建。**看到 embed 诊断先 `git status` 确认归属，别当成自己改坏的。**

**教训**：

- **门禁打印了某个维度，不等于它在管这个维度**。`--strict` 的期望数组长度 5 vs 打印 6 组，差额就是静默漂移区。**加门禁维度时先数「打印了几组」与「gate 了几组」**。
- **一个数字错了，往往同族的数字都错**。本轮从 `15` 出发查出 5 处计数错误（12.0.6 的 21=6+15、12.0.8 的 15、类别 A 的 8 个类型、类别 C 的 3 类型/4 字段）。**改一个计数时把同段落所有计数一起复核**。
- **文档自己给出的枚举就是它的校验和**。12.0.6 写「涉及 …（13 个字段）…」而句首写「15 处」，把枚举相加即得 21。**凡「N 处」后跟具名清单，当场相加。**
- **「函数个数」与「调用点个数」是两个量**。写「N 个调用点」用 `grep -c` 数实际出现次数，不要数函数名。
- **「同签名」要逐行 diff，不能按表头整列推定**。同名即冲突（结论不变），但签名不同会改变合并动作。
- **全称否定（「无一重叠」）错了而结论对，最容易被放过**——它读起来像已核过。**逐项找反例，并确认结论的理由是否也需要换。**
- **声明数 ≠ 引用数**。文档说「5 个 fixture」时，实际被引用的可能只有 3 个。

**第三十一轮（2026-10-05）——文档自标「一次性抽查、不是门禁」的那句话，正是缺口所在**：核 §12.16.2 里「4 个输出类型共有字段同序」这一段。它自己写着「但这是一次性抽查，不是门禁」，并建议「可复用 `tools/relaykit-dto-compat/` 扩一个『顺序』维度」——**建议从未执行，而抽查的结论是错的**。

事实（逐条实测）：

1. **「4 个输出类型」里有一个不是输出类型**：8 个快照真正 marshal 的顶层类型是 **3 个**——`GeneralOpenAIRequest`（2 个 request 快照）、`ClaudeResponse`、`GeminiChatResponse`（6 个 response/stream 快照）。`OpenAITextResponse` 是 response 方向的**输入**类型，被误列进「输出类型」。
2. **真正需要同序的不止顶层类型**：`json.MarshalIndent` 递归输出嵌套结构，所以凡从输出根可达的类型都要同序。按「从 3 个输出根沿字段类型递归」重算：本地闭包 **28 个类型**、relaykit 闭包 **36 个**（kit 多出 `Usage`、`BillingUsage`、`CachedTokenDetails`、`InputTokenDetails`、`OutputTokenDetails`、`ReasoningConversionState`、`GeminiPartialArg`、`GeminiGroundingMetadata`），两侧共有 **28 个**。
3. **共有 28 个里有 2 个顺序不同**：`ClaudeMediaMessage`（本地 `Thinking, Signature, Data, Delta`；kit `Thinking, Data, Signature, Delta`）与 `FunctionRequest`（本地 `Parameters, Strict, Arguments`；kit `Parameters, Arguments, Strict`）。原文的「全部一致」不成立。
4. **本基线确实不受影响，但理由是另一条**：那两对字段在 8 个快照里**从未同时非空**——`signature`/`data` 在全部 8 个快照中出现 **0 次**；`strict` 在 fixture 与快照里均 0 次，且 `tools[].function` 实际输出的键是 `["description","name","parameters"]`（`arguments` 只出现在 `messages[].tool_calls[].function`，那是 `FunctionResponse`，两侧同序）。**这是运气不是保证**：fixture 一旦产出带 `signature` 的 thinking 块或带 `strict` 的工具定义，同一份快照就会纯因序列化顺序而红。
5. **包级另有 2 个顺序差异类型**，不在本基线可达闭包内：`OpenAIResponsesCompactionRequest`、`ResponsesStreamResponse`（responses 方向）。
6. **文档自报的维度数也不准**：12.0.6 写「覆盖四个维度」并把「嵌入字段与函数签名」并作一项，而工具一直打印 **5 组**（字段名/tag/类型/嵌入/签名）；§9.3 标题写「四维比较」，基线列表还漏了 `equiv`。

修法（本轮把建议变成门禁）：

- `tools/relaykit-dto-compat/main.go` 新增**第五维 `共有字段声明顺序不同`**（`orderDiff`）。`scan()` 原先只返回 `map[string]fieldInfo`——**map 丢失声明顺序**，这正是顺序维度长期缺席的结构性原因；改为额外返回 `orders map[string][]string`。**嵌入字段也计入顺序**（它在序列化输出里同样占位）。
- 新增 `sharedOrderDiff()`：只比**两侧共有**字段的相对顺序，单边字段跳过（那是 `fieldSetDiff` 的职责，重复计数会把「增删一个字段」也报成顺序问题）。
- `--strict` 加入 `wantOrder = 4`，**反向验证**：改成 3 → 退出码 1 报 `dimension 4 changed: got 4 want 3`，还原后恢复。
- 新增 3 条单测：顺序不同要报、同序不报、单边字段不算顺序差异（后者同时断言它仍落在 `fieldSetDiff`）。
- 文档侧：重写 §12.16.2 该段（列出 2 个顺序差异类型、说明本基线为何仍安全、并指明「先核字段顺序再谈回归」）、§9.3 改为六维基线 `fieldSet=33 tag=5 type=6 embed=0 order=4 sig=12 equiv=21`、12.0.6 维度数改为五个、阶段 4 判据补一条。

门禁复核：`relaykit-citation-check.py` 退出码 0（40/40）、其单测 15 个通过；`relaykit-dto-compat.sh --strict` 通过（7 项计数）、工具单测 10 个通过（原 7 + 新增 3）。

**教训**：

- **文档里「这是一次性抽查，不是门禁」的句子，就是下一个缺口的位置**——它自己知道结论不可靠、也知道该怎么做（扩一个维度），却停在建议上。**凡写「不是门禁」并给出「可扩成门禁」的方案，当场把方案做掉**。
- **「N 个类型」要先确认这 N 个是不是同一个集合**。原文把输入类型混进输出类型，于是「4 个」本身就不成立；而真正要核的是**可达闭包**（递归），不是顶层。
- **序列化顺序的检查必须递归**。顶层同序不代表嵌套同序——2 个顺序差异类型都在第二层。
- **map 是有损容器，凡是「顺序/位置」相关的维度都不能用它存**。工具的 `map[string]fieldInfo` 从第 15 轮起就在，顺序维度因此一直不可能实现。
- **「本次不受影响」与「两侧一致」是两个结论**。本轮 2 处顺序差异都不影响基线，但原因是「那两对字段恰好为空」（偶然），不是「两侧一致」（事实）。**把偶然写成事实，下次 fixture 一改就翻车。**

**第三十二轮（2026-10-05）——被问到「规划够完善了吗」，答「还差 3 处」，查完发现第 4 处**：这一轮不是按「找错」的角度，而是**按用户提问逐条核「还剩什么没 owner」**。结论：§12.18 的 31 项里只剩 2 项未关闭、全文无 TBD/TODO 占位符、三条门禁都能重跑——**主体规划可执行**。但逐条核「已知未知项」时找到 3 个缺口，补的过程中又找到第 4 个。

事实：

1. **12.5 的 3 个「需实测」方法没有 owner**：分类列写的是「需实测」而非 A/B/C，而**阶段 2 判据只说「全部宿主独有方法给出出路」、处置清单第 6 项只覆盖 9.1/9.2 的语义差异，都不指向这 3 行**。这与第 25 轮「门控留宿主但调用点全在被删文件里」同型——**一个已知未知项没有 owner，就会滑到开工当天**。已新增「阶段 2 的前置」整节 + 阶段 2 判据追加一条 + §12.18 新增第 32 项。
2. **其中 1 个方法在 `HEAD` 上根本不存在**：`Usage.EnsureResponsesTokenDetailObjects` 在 `HEAD` 的 `dto/` 里 0 命中，只存在于工作区（`dto/openai_response.go:305`），4 个调用点也全是工作区新代码。文档第 214 行**早就记过**「HEAD 与工作区差 3 个方法」，但 **12.5 表没有引用那条提示**——分类对象本身尚未进入版本控制。已在新前置节里写明，并点出另外 2 个（`ResponsesStreamResponse.GetOpenAIError`、`ChatCompletionsStreamResponse.GetError`）属同一集合。
3. **12.5 的「生产引用」列口径与它自己声明的不一致**：表头写「工作区口径；排除 `_test.go` 与 `dto/` 自身」，但**这 2 行反而是唯一按真实调用量的**（`Reasoning.Clone`/`StreamOptions.Clone` 各写 1，唯一引用在 `service/responses_codex_local_compaction_recovery.go:205` 与 `:224`，同一文件故文件数 1）。**整列 23 行里 21 行是「名字匹配的文件数」、2 行是「调用口径」，两种口径混在同一列。**
4. **第 4 处（补 1–3 时顺带发现）**：`-lw` 这个名字匹配口径还会把**仅声明字段而无调用**的地方算进来——`common/responses_request_metadata.go:118` 的 `HasCompactionTrigger bool` 字段声明、`common/responses_request_metadata_shape.go:163` 的 `metadata.HasCompactionTrigger = shape.hasCompactionTrigger` 赋值，两处都不是 `dto` 那个方法的调用，却各计 1。**这解释了为什么勘误表里 `HasCompactionTrigger` 从 10 改成 9 之后仍然偏高**：真实调用点是 10 个文件、7 个非测试文件，而 9 是「名字匹配排除 dto/」的产物。

**为什么只补注口径、不改数字**：改口径会连带改动 23 行的取值与 A/B/C 分类的观感，而分类本身不依赖这个数（第 32 轮实测确认 23 行的 A/B/C 结论不因口径而变）。**把口径写清比强行统一到一个新数字更有用**——后者会让「与勘误表不一致」变成「与上一轮不一致」，而那一层差异已被 214 行的提示覆盖。

门禁复核：引用检查 40/40 退出码 0、其单测 15 个通过；`dto-compat --strict` 通过（7 项计数）、工具单测 10 个通过。

**顺带修一处会漂的行号引用**：新增段落里写「第 208 行已记过」，而那条提示实际在第 214 行（本文档已增行）。**文档内部的「见第 N 行」也会随编辑漂移**，与宿主文件行号同类——已改为当前实际行号。

**教训**：

- **「已知未知项有没有 owner」是一个独立于「事实对不对」的检查维度**。前 31 轮都在查「写得对不对」，这一轮查「谁负责做」，立刻找到 3 个缺口。**每个写着「需实测 / 待定 / 须确认」的条目，都必须能指出它属于哪个阶段的哪一步**；指不出来的，就是没有 owner。
- **同一条记录的引用处可能没读到**。第 214 行明明记了「3 个方法只在工作区」，12.5 表却照旧按工作区给分类并写「需实测」——**记录存在 ≠ 被消费**。凡在 A 节写下约束，要在 B 节确认它被引用；这是「审计记录与规格一起改」的第三种形态（前两种是行号漂移与提交表）。
- **一列数字底下可能有两种口径**。只对一行的时候看不出；**逐行用同一条命令复现，不符的行往往不是数字错，是口径不同**。
- **「名字匹配」会把声明、赋值、注释里的同名符号都算进去**。统计「谁引用了 X」时必须区分「调用 `X()`」与「提到 `X`」——前者才是引用点。

**第三十三轮（2026-10-05）——「单点可逆」是一句无人验证的断言，且它要求的形态与动作清单相反**：第 32 轮把「owner」这个维度查完，本轮换一个从未查过的维度：**回滚承诺**。§8.1 写着阶段 4 必须「单点可逆」——「切换动作收敛为一个可回退的开关或一次集中的函数体替换，而不是散落在 6 条响应路径里的多点改写。若做不到单点可逆，则阶段 4 不能合并」。逐条核后：

1. **该约束与动作清单相反**。12.3 决定**删除** 4 个 `*WithError` 与 4 个无 error 旧包装、**不保留包装层**；12.16.2 决定 1 要求把 8 个调用点**逐个改写**（Claude 方向 5 个生产调用点分布在 4 个文件：`relay-openai.go:314`、`openai/helper.go:46,246`、`openai/chat_via_responses.go:280`、`gemini/relay-gemini.go:1784`；另 3 个在 `convert_golden_test.go`）。**「删除包装层 + 逐点改写」本身就是多点**，与 §8.1 要求的单点相反。两条都写着、都没被指定为唯一形态，**回滚路径因此实际不存在**。
2. **修法**：§8.1 增一张「形态 × 阶段 4 × 阶段 6 × 是否满足」的对照表，给出两种可行形态（**薄封装**：6 个同名转发函数、调用点零改动，阶段 6 再删封装并改写；**直接改写**：与 12.3 一致，但回滚须 revert 4 个生产文件），并明确**必须在阶段 4 开工前二选一并写回 §8.1**。阶段 4 判据与处置清单同步：新增第 14 项、判据补「未定案则单点可逆不可验证」。§12.18 新增未决项 33。
3. **顺带修正一处计数**：4.5.5 表写「与 relaykit 重复的 4 个」，但那张表 **4 行里有 2 行各含两个函数**（`ResponseClaude2OpenAI` + `ResponseClaude2OpenAIWithError`；`responseGeminiChat2OpenAI` + `responseGeminiChat2OpenAIWithError`），**函数总数是 6**。阶段 6 判据「4 个通道级转换器已删除且无残留引用」用的是**行数口径**——照它验收会漏掉 2 个函数。已把判据改为**逐个列出 6 个函数名**，不再依赖这个数。
4. **补一处悬空指针**：§8 表第 6 条（AWS Converse）写「需单独处理」，全文无指针指向它的处置结论。**实际早已处置**（12.4.3 决定维持现状：该路径无条件暴露 thinking，套用 DeepSeek-only 门控会新抑制 Bedrock reasoning，是行为回归）。已在该行补上结论与两条禁止（阶段 4 不得施加门控、阶段 6 不得连带删除其 thinking 生成逻辑）。

门禁复核：引用检查 40/40 退出码 0、其单测 15 个通过；`dto-compat --strict` 通过（7 项计数）、工具单测 10 个通过。

**教训**：

- **回滚承诺要和动作清单放在一起读**。§8.1 的「单点可逆」写在「止损点」一节，动作清单写在阶段表与 §12；两者相隔数百行，各自读都自洽，**并排读才看出一个要求单点、一个描述多点**。**凡写「必须 X 可逆 / 必须单点」，把实现 X 的动作逐条列出来数一数**。
- **「4 行」不等于「4 个」**。一张表的行与它列举的对象数可以不等（一行含两个函数名）。**用行数当计数的判据，会在验收时漏掉行内的其余对象**——判据要么逐个列名，要么写清口径。
- **「需单独处理」后面必须跟一个指针**。这类措辞读起来像待办，但可能早已处置——不写指针，后续读者要么重做一遍，要么当成未决项挂起。**写「需 X」时当场给出「已在 Y 处置」或「未处置，owner 是 Z」。**

**第三十四轮（2026-10-06）——性能/缓存维度第一次进入审查，查出的是「已处理」这句断言在本地落点为空，且它牵出整条思考等级输入链缺席**：前三十三轮从未查过运行时/性能维度。本轮从「`ConvOptions()` 每 chunk 调用是否重复构造快照」出发，逐项核对后得到 3 个发现，全部指向同一份「阶段 3 照抄清单」的缺项：

1. **§12.10「无需改动，上游已正确处理」只对上游成立**。上游 `relay/common/relay_info.go:311` 在 `InitChannelMeta` 置 `info.convOptions = nil`；本地 `InitChannelMeta`（`relay/common/relay_info.go:364`）没有这一行，也没有 `convOptions` 字段（阶段 3 才加）。阶段 3 的照抄区间 `:823-953` 既不含字段声明（`:201-202`）也不含重置行（`:311`）；判据「首次调用建快照、同一 relay 会话复用」**不重置也满足**，抓不到。本地重置点现成：换渠道重试循环 `controller/relay.go:1016` 每次调用 `InitChannelMeta`（另有 16 个 handler 入口调用点）。已把 §12.10 改为「上游语义采用、本地落地两处」，阶段 3 动作/判据、§10 第 10 项、§12.17 第 10 行同步；判据改为「换渠道重试后 `ConvOptions()` 反映新渠道（`OpenRouterDialect`/`ToolLossPolicy` 不读到上一渠道）」。性能边界同时写清：缓存命中=一次指针判空、重置每渠道一次、不得实现成每 chunk 新建快照（12.4.4 的门控每 chunk 调用它）。
2. **照抄区间比接口多一个方法、比本地多三个字段依赖**。`:823-953` 段里实际有 14 个方法，其中 `GetBillingModelName`（`:832`）**不在 `convmeta.Meta` 接口里**、relaykit 零调用、本地无 `BillingModelName` 字段与写入方（上游写入方在 `relay/helper/price.go:73-77`）——照抄它需要凭空加一个无人读写的字段；13 个接口方法里 `ReasoningState()` 读 `ReasoningConversion`（上游 `:123`）、`ConvOptions()` 读 `convOptions`（`:201-202`），**两个字段本地都没有**，「12 个是纯访问器、只有 `ConvOptions` 需新写逻辑」随之修正为「11 个纯访问器 + 2 个各需一个新字段」。阶段 3 行已重写（含「`GetBillingModelName` 不照抄」的显式决定）。
3. **最大发现：思考等级的宿主填充器全链路缺席（未决项 34，新 §12.19）**。relaykit README 明示转换器**不再解释** `-thinking`/effort 尾缀，后缀意图必须由宿主入口写成 `dto.ReasoningConversionState` 并经 `ReasoningState()` 传入；relaykit 内部 4 处 `ReasoningStateOf(info)` 消费点（`shared/claude/reasoning.go:24`、`shared/gemini/request.go:107`、`claude_messages/to_oai_responses_req.go:96`、`gemini_chat/to_oai_chat_req.go:56`）+ 原生路径 `ApplyClaudeThinkingModel`（`request_compat.go:49`，由 `relay/channel/claude/adaptor.go:39` 调用）全靠它。上游入口是 `relay/helper/reasoning_suffix.go`（232 行，`ApplyReasoningModelSuffix` 置 `info.ReasoningConversion` 于 `:71`，非测试调用 6 处、5 个文件）+ `model_modifier.go`（278 行）——**本地两个文件都不存在**，全文（含处置清单）零提及。本地现有后缀处理全是内联的：`relay-claude.go:430-478` 随阶段 4 收敛删除，`claude_handler.go:55-104` 的换装（§3.1「采用上游」）**在阶段表里同样零落点**（全文仅 §3.1/§5/§6.1 提及，均为能力盘点、无动作）。不装填充器 → `ReasoningState()` 恒 nil → 跨协议路径（oai→claude 等）的思考等级静默丢失——正是第 5 节承诺保留的能力。且 8 个 golden 快照的模型名零后缀用例（`claude-test`/`gpt-test`/`gemini-test`/`deepseek-v4`），**现有验证网抓不到该回归**。已新增 §12.19（A 移植上游 / B 本地解析器薄填充器，阶段 4 开工前二选一）、处置清单第 15 项、阶段 4 判据一条、12.16.1 盲区补记、§5 承载链注记、§12.17 第 26 行、未决项 34。

门禁复核：引用检查 45/45 退出码 0（新增 5 处；对 /tmp 副本反向验证 2 处，扰动即 exit 1）；其单测 15 个通过；`dto-compat --strict` 通过（7 项计数）。

**教训**：

- **「上游已处理」不等于「本地已落地」**。判据里的「按上游语义」经常被读成「照抄即可」，但上游的处理可能位于本地根本不存在的文件/字段/调用点上。**看到「上游已处理」，当场在本地仓库里找出对应落点并指明它属于哪个阶段动作**——找不到就是缺口，不是「无需改动」。
- **「可整体照抄 X-Y 行」的区间承诺必须逐符号验证**。区间里可能多出接口不需要的方法（照抄要凭空加字段）、少掉方法依赖的字段声明与调用点重置行。**照抄清单要按「方法 + 字段 + 重置行」三类分别列，不能用一个行号区间代替**。
- **能力清单里的「收敛后承载」必须带一条可验证的输入链**。第 5 节写「思考等级 → `RenderClaude` + suffix 解析」，读者会以为承载方自己会解析后缀；实测承载方明确声明「不解释尾缀」，输入必须由宿主另一处代码填充——**写「承载方是谁」时，把「输入从哪来、由谁填充、在哪个阶段接线」一起写**，否则能力承诺在阶段表里没有任何动作与之对应。

**第三十五轮（2026-10-06）——阶段 4/6 的宿主侧终态从未与上游完成态对照：上游「保留旧名」，文档「全删全改写」**：第 34 轮查完「上游已处理 ≠ 本地已落地」，本轮换一个角度：**「本地要删的这套东西，上游迁移完成后到底长什么样？」**——12.3 与 §8.1 讨论了三轮「删不删包装层」，却从未打开上游的终态文件。逐文件实测 `upstream/main` 后：

1. **上游终态是「保留旧名」**：`service/convert.go` 没有消失，而是 **27 行、5 个同名 facade**（`NormalizeCacheCreationSplit` + 4 个非 WithError 包装，旧签名不变、一行转发 `relayconvert.*`）；`ClaudeToOpenAIRequest`/`GeminiToOpenAIRequest` 保为同名 shim（`service/request_converter.go:50`/`:62`）；通道级 4 个函数同样保为一行 facade（`relay-claude.go:37`/`:41`、`relay-gemini.go:139`/`:143`）；`relaykit/relayconvert/response_compat.go` 导出约 40 个同名包装供 facade 转发。真正被删的只有 `*WithError` ×4 与 `IsDeepSeekModelName`/`HasDeepSeekModelName`（前者在 `service/` 下 0 命中、后者全仓 0 命中），它们的调用点改指 `service.ConvertResponse`/`ConvertStreamResponse` 包装（39 处、16 个文件）+ `RelayInfo.RecordConversionDiagnostics`（新文件 `relay/common/conversion_diagnostics.go`）。
2. **测试面因此差一个量级**：按文档字面形态（全删）有 **4 个文件、82 处调用**编译不过（`convert_claude_request_test.go` 35、`convert_claude_stream_test.go` 14、`convert_test.go` 25、`convert_golden_test.go` 8；阶段 4 断 38、阶段 6 断 44），而**只有 `convert_golden_test.go` 有处置动作认领**；按上游形态只有 19 处要动。**12.16 决定清单第 2 项写「确认这些既有用例在切换后仍通过——切换会让它们失败得其所」**，该断言隐含了「函数还在」这一未写出的前提；按文档自己的计划它们是**编译失败**，不是「失败得其所」。
3. **两处自相矛盾被同批修正**：12.3 写「4 个测试文件需改写调用」（对）而 12.16.1 写「两处例外」（错）——同一批对象、两个结论，六轮无人对照；§8.1「薄封装」列出的 6 个同名转发函数**含 `*WithError` 名字**，而上游恰恰删掉了这些名字，与上游实际保留的 5+2 个不同。
4. **4.5.5 表的映射列指向宿主不可达路径**：三个通道级转换器的「relaykit 对应」写的是 `claude_messages/to_oai_chat_resp.go:34` 等 **internal 路径**，违反 12.16.2 自己写的可达性规则；公开同名包装就在 `response_compat.go:50`/`:100`/`:140`/`:144`。
5. **§3.1 的「降级可观测性 新增能力」只在包装层形态下成立**：diagnostics 的产生在 relaykit 内部，落地靠 `RelayInfo.RecordConversionDiagnostics`（本地没有，且不在 `convmeta.Meta` 里、经类型断言调用）——直接调包级函数则**静默丢弃**。

**修法**：新增 §12.20（上游终态实测表 + 三点含义）；12.3 撤回「4 个非 WithError 旧包装一并删除」半句；12.16 第 2 项重写（含逐文件清点表）；§8.1 形态选择扩为三选一（**新增 C＝照抄上游终态，推荐**）；4.5.5 映射列改为公开包装并注明 WithError 变体已删；§10 第 4 项、§11 五条新事实、阶段 6 行终态指针、§12.18 第 33 项同步。**收尾补（同轮自查）**：处置清单第 2、14 项仍复述旧决定（活清单不得复述——正是本轮教训 3 说的那种残留），已同步为三选一 + 12.20 指针；12.16.2 决定 1 加形态注记（直接改写形态下 8 处全改，形态 C 下只有 6 处）；阶段 5 行补 12.20 指针（「宿主管道接入 relaykit 分派」的具体内容）。门禁：引用 45→47（两处新增均入门禁，1 处经 /tmp 副本反向验证）、`dto-compat --strict` 通过。

**教训**：

- **「上游已经做过同一场迁移」时，先打开上游的终态，再设计自己的形态**。§8.1 的 A/B 两形态都是本地发明，从未与「上游怎么收的尾」对照——而答案就在 `upstream/main` 的文件树里（27 行的 convert.go）。**凡迁移类设计，先 diff 上游的完成态与本地计划态**，差异列表本身就是最高产的审查入口。
- **「X 在切换后仍通过」这类断言，必须写出它的使能条件**。「仍通过」隐含「函数还在、签名没变」；形态一换（全删），同一句话从「通过」变成「编译不过」。**凡写「无需新建 / 仍通过 / 失败得其所」，先问：它在哪种形态下为真？该形态是唯一被指定的形态吗？**
- **同一批对象的结论分散在多处时，矛盾可以存活很多轮**。12.3 的「4 个文件需改写」与 12.16.1 的「两处例外」互相矛盾六轮无人发现——**修法是逐文件枚举成唯一清单，其他位置只引用不复述**。
- **「同名保留」是上游迁移的默认策略，值得当模板**：一行 facade 让全部旧调用点零改动，把迁移面缩到最小、回滚点缩到一个文件。设计本地迁移时**默认先问「哪些名字可以原样保留」**，而不是默认「全删+全改写」。

**第三十六轮（2026-10-07）——把宿主路径补进门禁：三组分类实测抓出 1 个真错行号与 6 处漏前缀，过程中门禁被自己刚写的反向验证示例击中**：第 23、28 两轮各修过一次门禁盲区（全路径、简写路径），本轮按同一方法查**第三类引用**——宿主路径。实测（两口径交叉分类，全部裸宿主引用 107 处）：

1. **三者皆不可解析（真错）1 处**：`service/convert.go:533` 应为 `:526`——这是第 18 轮建 golden 基线时写入的段落，行号错了 18 轮没人能抓，因为宿主路径根本不在门禁内。同段另有两个旧错行号一并修：`:512`→`:505`（`IsDeepSeekModelName`）、`:544`→`:537`（`HasDeepSeekModelName`，该行还带裸 `:544` 无路径写法）。
2. **本地基线不可解析、但上游可解析（漏 `upstream/main:` 前缀）6 处**：`service/openai_chat_responses_mode.go:15`（两处，其中一处与本地同名文件并存——**靠巧合解析**，ref 身份是错的）、`relay/responses_request.go:43`、`service/request_converter.go:50`（`:62` 为共享行段，一并加前缀）、`relay_info.go:562`（行首有「上游」字样但无 ref 前缀）、以及 §11/§12.19 各一处的 `relay/claude_handler.go:41` 等上游 6 调用点清单（清单里其余 4 个恰好在本地也存在，**部分正确掩盖整体错误**）。
3. **两口径行内容分歧 15 处**——逐条对照文档语义后：13 处文档确按基线口径书写（如 4.5.5 表的 `:255`/`:714`/`:831`/`:1276` 在基线精确命中函数定义，HEAD 已漂），**2 处是第 34/35 轮我按 HEAD 口径写入的混入**：本地 `InitChannelMeta` `:364`→基线 `:358`（4 处）、`relay-claude.go:430-478`→基线 `:366-414`（1 处，`TrimEffortSuffix` 基线行实测在 `:366`）。顺带确认一处符号漂移：`genBaseRelayInfo` 在 HEAD 已改名 `GenRelayInfoOpenAI`（`:670` 基线引用仍对，但基线正在老化）。
4. **门禁落地**：检查器新增两类——裸宿主引用 **98 处**在 `--baseline dbfbc096f` 解析（文件不存在但上游存在 → 报「add an upstream/main: prefix」；`docs/` 路径两处皆无 → skip），`upstream/main:<path>:NN` 全路径 **15 处**在上游解析（relaykit 路径去重避免与 CITATION 重计）；单测 15→25（宿主提取、前缀排除、sha 前缀排除、缺前缀报错、doc skip、upstream 全路径各一组）；**反向验证 5 次扰动全部退出码 1**（relaykit `:99999`、改文件名、简写 `:99999`、宿主 `:99999`、去掉一个 `upstream/main:` 前缀）。
5. **反向验证刚写完，门禁就红了**——我在设计文档里记反向验证用了一个真引用 `service/convert.go:99999`，当场被新宿主门禁击中（`failed=1`）。改成 `<宿主文件>:99999` 占位符后恢复绿。**这是「示例进入门禁」第三次复发**（第 26、28 轮各一次），前两次都在 relaykit 侧，这次在我自己为**避免**复发而写的文档段落里。
6. **顺带清掉三处门禁自述的过期计数**：检查器描述段「34 处」（实为 47，该段落自第 28 轮起未随批次更新）、「15 个单测」（25）、§9.3「当前 16 处」（三组合计 160）。
7. **缺口 2 补齐**：新增 §12.20.1（形态 C 的连带动作清单）——生产侧三分（9 处 `*WithError` 必改 / 5 处同名 shim 零改动 / 非 WithError 生产 0 处）、测试侧四分（**17 处必改 / 63 处零改动 / 2 处是有意与上游终态不同**）、C1–C6 新增文件与回滚点，处置清单新增第 16 项。枚举时发现一条此前无人记录的隐藏依赖：**`IsDeepSeekModelName` 的 2 个测试调用方要求该函数活到阶段 6 之后**（12.4 的门控钩子值要用它），而上游终态把它删了——这是**与上游终态的必要差异**，须记入宿主侧改动清单。同时发现选 C 必须改阶段 6 判据（通道级 4 函数从「删除」变「保留为 facade」），已写进 C6。

**同轮自查（写完 §12.20.1 后逐字复核，抓到 4 处）**：①C5 把上游 6 个 `NewResponseStreamState` 构造点写成「`relay-gemini.go` 响应两处」+ 5 个具体路径 = **7 个描述 6 个对象**，且其中 gemini 的两处实际在 `relay_responses.go`（我写成了 `relay-gemini.go`）——改为 6 个逐条实测的路径。②生产表把 `gemini/relay-gemini.go:1784`+`:1819` 列成两处，实为**同一处在两个口径下的行号**（第 36 轮刚建立的基线/HEAD 对照表里就有这条），改为「`:1784`（HEAD 口径 `:1819`，同一处）」。③请求方向测试数 42 的拆分写成「30 + 10 + 2」，实测是 **31（28+2+1）+ 11（10+1）**，含 golden 的 2 处——写拆分时把 golden 的贡献重复计了一次。④**改完 ①②后重跑门禁，新写的 6 个上游站点里只有 2 个被报错**——另 4 个（`relay-claude.go`、`helper.go`、`chat_via_responses.go`、`responses_via_chat.go`）在本地恰好同名存在、按**本地基线口径解析通过**，于是「3 个正确 + 1 个错路径 + 2 个报错」的混合清单看起来只是「2 处漏前缀」。这正是上一轮刚写下的教训 92（「部分正确」的清单最难抓）**在同一份文档的下一轮、由我自己复发**；6 处全部加 `upstream/main:` 前缀后门禁转绿（upstream-full 15→21）。

**教训**：

- **每修一类引用的门禁，先把「还能长成什么样引用」的全集枚举出来**——23 轮修全路径、28 轮修简写，两轮都是被假绿教育后补的；宿主路径是第三类，本轮换方法**主动枚举**（正则问「什么长成 `path:NN`」），一次抓出 7 处。剩下的已知边界：**无目录前缀的裸文件名（`<file>.go:NN`）与裸 `:NNN`** 不在门禁内，已写进固有边界段。
- **反向验证的示例本身也是引用**。第三次复发说明这条不靠记忆、要靠占位符习惯：**写门禁文档时只允许 `<path>`/`<行>` 形式的示例**，本轮已把规则与复发次数一起写进检查器描述段。
- **「部分正确」的清单最难抓**：上游 6 调用点里 4 个在本地恰好也有同名文件，清单因此在「检查是否可解析」的意义上几乎全对——只有逐条问「这行在**声明的 ref** 上是什么」才暴露。**ref 身份（前缀）比可解析性更根本。**
- **计数段落是门禁的自盲区**：检查器描述段里的「34 处」「15 个单测」滞后了 8 轮，因为**它们描述的是门禁本身，而门禁不检查文档对自己的描述**。工具自述也列入每次改动的同步清单。

**第三十七轮（2026-10-07）——让外部模型（Codex）独立复核：它的 2 个 P1 全部成立、1 个 P2 成立但被低估，而复核过程本身又牵出 3 处比 P1 更硬的缺口**：本轮不是自查，是把整份规格连同「只用 `git show <ref>:<path>`、不得改动工作区」的约束交给 Codex 独立走查，再由我逐条复现它的每条结论。**外部复核的价值不在「多一双眼睛」，而在它不受我前 36 轮的思路惯性约束**——它一上来就查了三个我从未作为整体查过的对象。

Codex 的报告与我的逐条复现：

1. **P1-1（成立）阶段归属冲突**：阶段 5 行把包装层 / `RecordConversionDiagnostics` / 流式 `ResponseStreamState` 写成自己的落地内容，而 §12.20.1 与处置清单第 16 项把它们归给阶段 4。**同一批产物被两个阶段同时认领，且阶段 5 的判据还要求「核验」它们**——若真按阶段 5 落地，阶段 4 的判据（4 个门控快照逐字节一致）就已经隐含要求过它们，届时会出现「阶段 4 未落地但判据已通过」的假绿。已把阶段 5 行改为**只核验不落地**，并给处置清单第 16 项加注「阶段归属第 37 轮钉死」。
2. **P1-2（成立，且比 Codex 说的更受限）无法产生 error**：Codex 指出 `reasonmap.OpenAIFinishReasonToClaudeStopReason` 无 error 通道。**我复现后把范围扩大**：不仅该函数返回裸 `string`，调用它的 `oai_chat` 本地助手 `stopReasonOpenAI2Claude`（`:501`）与 3 个上游函数（`StreamResponseOpenAI2Claude:111`、`FinalizeStreamResponseOpenAI2Claude:420`、`ResponseOpenAI2Claude:449`）**全部无 error 返回**，4 个调用点（`:220`/`:406`/`:429`/`:459`）都在它们体内。**故 12.11.3 的「选项 A（布尔钩子）」从设计上就不可能实现**——布尔只能选分支，错误无处可去。已拆为 **A1**（改 3 个函数签名，真分叉）/ **A2**（钩子只选分支、报错由宿主前置校验，实为 B 的变体）/ **B**（纯宿主 5 处前置校验），同步处置清单 9、§12.18 第 31 行、§10 收尾 12、本地改动清单第 12 行。
3. **P2（成立，且实际是 9 个不是 6 个）**：4.5.5 写「与 relaykit 重复的 4 组（6 个函数）」。**逐名核对后是 9 个**——漏了 `StreamResponseClaude2OpenAIWithError`、`streamResponseGeminiChat2OpenAI`、`streamResponseGeminiChat2OpenAIWithError`。同时**该表的「30 个 / 17 个通道包」用任何单一命令都复现不出来**（我试了 8 种正则，得到 26/16、27/17、39/18、42/18、45/20、49/20 等），已换成一个**写出命令**的口径：函数名含 `<字母>2<字母>` 且含 `request`/`response`（不分大小写）= **45 个 / 20 个包**。阶段 6 判据的名单 6→9。
4. **Codex 自己的「确认无误」项里有一处抽查是错的**：它用 `git show dbfbc096f:service/convert.go | sed -n '240,270p'` 核对 4.5.5 第 1 行，报「Claude 入向函数在 `service/convert.go:255`」——**`:255` 是 `relay/channel/claude/relay-claude.go` 的行号，`service/convert.go:255` 是 `sourceType := strings.ToLower(...)`**。**外部复核的「确认无误」与「发现问题」需要同等的复现义务**（同 [[subagent-audit-claims-need-independent-verification]]），本轮我逐条复现了它的每个 P 级结论，它的「确认无误」项也抽检了 6 条，仅此 1 处有误。

**复核过程中我自己牵出的 3 处缺口（均未被 Codex 提及，且都比它的 P1 更硬）**：

5. **`relay/channel/aws/converse.go`（1206 行）从未进入任何盘点**：这是本地独有的 Bedrock Converse 双向转换（33 个顶层函数），**上游从未有此文件**（`git log upstream/main -- relay/channel/aws/converse.go` 为空，上游 `relay/channel/aws/` 只有 5 个文件），上游全仓 `Converse` 标识符只出现在一个 URL 字符串里。它与本迁移有 3 个交互点（`:113` 调 `service.ClaudeToOpenAIRequest`、`:696` 调 `claude.ResponseClaude2OpenAIWithError`、`:412` 调 `service.GetBase64Data`），**但 §4.5 各小节、§12.20.1 的 C 表、阶段 6 的删除清单全都没提到它**。已补入 4.5.5 末段与阶段 6 行。
6. **通道级的 5 个生产调用点 + 6 个测试调用点同样未被任何清单认领**：`relay-claude.go:1167,1273`、`relay-gemini.go:1605,1768`、`aws/converse.go:696` 调用 4 个通道级 `*WithError` 函数，**而 12.20.1 的生产表只列了 `service/convert.go` 的 9 处**。更关键的是**这 5 处的「删掉 err 分支」不是机械改写**：上游对 `:1273`/`:1605`/`:1768` 用无 error facade，对 `:1167` 却用 `state.ConvertChunk`（**有** error 返回）——**同一批调用点，上游的处理方式并不一致**，逐处实测后写入。测试侧同理：`relay/channel/claude/relay_claude_test.go`（18 处）、`relay/channel/gemini/protocol_conversion_test.go`（4 处）与 12.16.1 的「4 文件 82 处」是**两套互不重叠的测试面**，且 HEAD 已增至 30 处（新增两个基线不存在的文件）。
7. **通道级响应转换器自己的一套严格性（9 条）与反方向 stop_reason 严格校验（6 条）都无盘点**：12.16.1 与 9.1/9.2 只覆盖 `service/convert.go` 的映射层，**而 4.5.5 那 9 个函数被替换时会丢掉另一套独立的错误路径**——新增 §9.2.1（9 条，含 1 条 **nil pointer panic**：上游流式 facade 在 `:84` 直接解引用 `*Delta.PartialJson`，而本地 `:779` 有守卫、非流式 `ConvertChunk:188` 也有守卫）与 §9.2.2（6 条反方向差异，含 `pause_turn` 本地 `stop` vs 上游 `length` 的**语义分歧**）。**§9.2.1 第 9 条不是「严格性丢失」而是输出内容变化**：`signature_delta` 本地返回 `nil,nil` 丢弃，上游 `:87-88` 产出 `ReasoningContent="\n"`——而真实 Claude 流每个 thinking 块结束都会发 `signature_delta`，本地测试正钉在相反语义上。

**门禁与反向验证**：新增引用后 `relaykit-citation-check.py` 三组全绿（**50/50** relaykit、**103/103** 宿主、**24/24** upstream 全路径；第 36 轮为 47/98/21）；单测 25 个通过；`relaykit-dto-compat.sh --strict` 通过（`fieldSet=33 tag=5 type=6 embed=0 order=4 sig=12 equiv=21`）。**反向验证 5 次扰动全部退出码 1**（本轮新增的两类锚点各验一次：relaykit `to_oai_chat_resp.go:99999`、`reasonmap.go:99999`；宿主 `relay_claude_test.go:99999`；upstream 全路径 `claude/adaptor.go:99999`；以及 §9.2.1/§9.2.2 最初写成裸 `:NN` 简写时**不被门禁覆盖**——当场改成全路径后进入门禁）。顺带同步检查器自述段的 3 处计数（47→50、98→103、21→24）。

**教训**：

- **外部复核的「确认无误」与「发现问题」有同等的复现义务**。本轮它的 3 个 P 级结论全部成立（P2 还被低估），但它的 1 条「确认无误」抽查指向了错误的文件——**不能只复现它的失败项、把它的通过项当既成事实**。
- **「无 error 通道」类结论必须逐签名核到函数体**。Codex 报的是最外层函数无 error 返回；真正的约束是**从报错点到最近的 error 返回点之间的整条调用链**——本轮因此把「选项 A 不可实现」的范围从 1 个函数扩到 4 个函数。
- **同一批产物的阶段归属冲突，用「可逆性」判而不只是「谁先谁后」**：阶段 4 是唯一被要求单点可逆的切换点，凡与该切换耦合的产物（包装层、diagnostics、流式状态）都必须在阶段 4 落地——否则阶段 5 单独落地会让阶段 4 的判据在「产物尚不存在」时通过。
- **一个文件从未出现在任何盘点里，往往不是「无关」而是「没人查过」**。`converse.go` 1206 行、33 个函数、上游零覆盖，却因为「不在 §4.5 的分类框架里」而 36 轮无人提及——**分类框架本身会制造盲区**（同 12.5 的「需实测」无 owner）。
- **同一批调用点里，上游的处理方式可能不一致**。`:1167` 用带 error 的 `ConvertChunk`，`:1273`/`:1605`/`:1768` 用无 error 的 facade——**不能按「同一族函数」批量推断改写方式，必须逐处打开上游对应位置**。
- **替换一套函数时，它自带的错误路径要单独盘点，不能只盘「正常路径的语义差异」**。9.1/9.2 盘了 15 条语义差异，却漏掉了被替换函数自己的 9 条错误路径与 6 条反方向差异——**「语义对齐清单」的覆盖面应按被删函数的全集反推，而不是按输出类型反推**。

**第三十八轮（2026-10-07）——自查第 37 轮刚写入的内容，抓到 3 处自己引入的错误；顺着第 37 轮的「被替换函数的错误路径要单独盘点」这条教训，把同一盘点做完，发现 Gemini 方向整套未盘**：第 37 轮的教训写「按被删函数的全集反推」，但那一轮**只对 Claude 方向做了**——4.5.5 的 9 个函数里有一半属 Gemini，其错误路径一条没盘。本轮先自查后补全。

**自查第 37 轮（3 处，全部当场复现）**：

1. **`relay-gemini.go:1819` 已过期**。第 37 轮写「`relay/channel/gemini/relay-gemini.go:1784`（HEAD 口径 `:1819`，同一处）」。基线口径 `:1784` 正确，但 HEAD 现为 **`:1845`**——该文件在第 36 轮后被另一会话的提交 `222876349` 改动，整体下移 26 行。**这类「HEAD 口径」标注不受任何门禁保护**（门禁只锚定基线与 `upstream/main`），是系统性腐坏点；已改正并加注说明。
2. **`tool_call_id_test.go` 的交叉引用指错了对象**。第 37 轮写它「钉 9.2.1 第 5 条的 `PartialJson` 守卫」。实测该文件钉的是 **Gemini 合成 tool call ID 的跨 chunk 唯一性**，与 Claude 的 `PartialJson` 守卫毫无关系——我把「另一个通道的同名测试文件」当成了同一件事。已改指 9.2.3 第 8 条。
3. **9.2.3 初稿的行号混了两个口径**。写表时 `call_gemini_...` 用了工作区行号（`:1369`/`:1525`），基线实为 `:1326`/`:1472`；`geminiFinishReasonToOpenAI` 的调用点写 `:1368`/`:1455`，基线实为 `:1366`/`:1453`。**这是第 51 条教训（行号口径必须一致）在同一份文档里的又一次复发**——而且是在我刚刚写下该教训的那一轮的下一轮。

**第 37 轮教训的直接续作——Gemini 方向的通道级错误路径（新增 §9.2.3，8 条）**：9.2.1 只盘 Claude 方向。Gemini 方向本地 `responseGeminiChat2OpenAIWithError`（基线 `:1281`）与 `streamResponseGeminiChat2OpenAIWithError`（`:1430`）对 relaykit `to_oai_chat_resp.go:81`/`:188` 的差异：

- **第 1 条是第二个 nil 解引用 panic**（本地 `:1283`/`:1432` 报错；上游 `:86`/`:189` 直接 `len(response.Candidates)`）——**两个通道各有一条 panic**，均须在阶段 4 优先处理。
- **第 2 条是语义改写而非单纯放宽**：未知 `finishReason` 本地报错（`geminiFinishReasonToOpenAI:1198`），上游 `default` 一律归为 `ContentFilter`——未知值会以「内容被过滤」的形式返回给客户端。
- **第 8 条是行为变化且本地有用例钉住**：本地合成 ID 是**确定性的** `call_gemini_<candidate>_<ordinal>`（`:1326`/`:1472`），relaykit 用**随机 UUID**（`:728-729`）。`protocol_conversion_test.go:318` 断言「同一输入两次调用 ID 相同」，relaykit 方案**必然让它失败**；这不是严格性取舍，而是**本地刻意保证的幂等性被丢弃**（客户端重放/重试时工具调用 ID 会漂移）。上游无此修复（本地另两个用例 `tool_call_id_test.go` 由 `4dee3b71b` 引入、`git merge-base --is-ancestor 4dee3b71b upstream/main` 为假）。
- 第 3–7 条（多字段 part / 无受支持内容 / `fileData` / `functionResponse` / `inlineData` 缺字段 / `functionCall` 缺 name）本地各有报错，上游静默忽略或按首个命中分支处理。

**门禁**：9.2.3 新增引用后 `relaykit-citation-check.py` 三组全绿（**51/51** relaykit、**105/105** 宿主、**24/24** upstream 全路径；第 37 轮为 50/103/24），自述计数已同批更新（教训 93）。`--strict` dto 门禁不变。

**教训**：

- **一条教训写下来，要先问「我是不是只做了一半」**。第 37 轮写下「被替换函数的错误路径要单独盘点」，同轮只盘了 Claude 一半（9 个函数里 4 个属 Gemini）——**盘点类结论必须用「被盘对象的全集」校验覆盖率**，写「已盘 N 条」时把分母也写出来（本轮补：Claude 侧 9 条 + Gemini 侧 8 条）。
- **「HEAD 口径」是文档里唯一不受门禁保护的行号类别**。基线与 `upstream/main` 都有门禁锚定，HEAD 会随任何人的提交漂移——另一会话在我写下 `:1819` 之后提交了 `222876349`，该标注当场过期。**凡写 HEAD 口径行号，同句注明「该值无门禁保护」，或干脆改写成基线/上游口径**。
- **跨通道的同名测试文件最容易张冠李戴**。`relay_claude_test.go` 与 `tool_call_id_test.go` 都测「转换器的错误/边界行为」，我按名字相似把它们归给了同一个 §9.2.1 条目——**引用测试文件前打开它，读它断言的是什么**，不要按文件名推断。
- **行号口径混用会在「刚写下该教训」的下一轮复发**（第 51 条 → 本轮第 3 处）。**光记教训不够，要把它变成写表时的动作**：写任何 `:NN` 之前，先跑一条命令打印该行内容，而不是凭上一次 grep 的记忆。
- **一个 panic 是事故，两个 panic 说明这类问题成对出现**——两个通道的转换器都直接解引用上游响应结构，本地都加了守卫。**「本地加了守卫」这种加固在替换时是系统性丢失的**，不是个别疏漏；盘点时按「本地所有 nil 检查」反推比按通道反推更可靠。

**第三十八轮续：让 Codex 独立复核（第二次），同时自己把全部断言逐行复现**。本轮把第 37/38 轮新增的 §9.2.1/9.2.2/9.2.3 三张表连同计数、判据一起交给 Codex 独立走查（只读、只用 `git show <ref>:<path>`），**我并行做同一批核查**——外部结论与自查互为对照。

**我这侧逐行复现的结果**：§9.2.1 的 9 行、§9.2.2 的 6 行、§9.2.3 的 8 行，**本地侧与 relaykit 侧的行号全部逐行命中**（用 `git show dbfbc096f:<path> | sed -n '<line>p'` 与 `git show upstream/main:<path> | sed -n '<line>p'` 各打一遍并核对内容）；§4.5.5 的 45/20（=36+9）命令复现一致；§12.20.1 生产表五数量（9/5/5/3/0）逐个复现；§12.11.3 的推断链（`reasonmap:31` 裸 string → `stopReasonOpenAI2Claude:501` → 3 个无 error 函数 `:111`/`:420`/`:449` → 4 个调用点 `:220`/`:406`/`:429`/`:459`）完整；converse.go 基线 1206 行、32 函数、3 交互点（`:113`/`:412`/`:696`）；通道级测试面基线 22 处/2 文件、HEAD 30 处/4 文件。

**自查在本轮又抓到 3 处（第 4 处是第 37 轮遗留，已在上文记）**：

1. **`converse.go` 的「33 个顶层函数」是 HEAD 口径混入**——基线实测 32（`git show dbfbc096f:... | grep -c '^func '`），33 是 HEAD 值。**这是「行号口径混用」从行号扩散到了计数**：我第 37 轮补写这段时用了工作区/HEAD 的数，却在同句写了「1206 行（基线口径）」。已改成双口径标注。
2. **§9.2.1 第 9 条的测试归属张冠李戴**（第 37 轮引入）——它写「`HandlesMissingPartialJSON` 与 `RejectsIncompleteToolBlock` 都钉在本地语义上」，但那两个用例分别钉第 5 条（缺 `partial_json`）与第 4 条（不完整 tool block）；钉 `signature_delta` 的是 **`TestStreamResponseClaude2OpenAIIgnoresOpaqueSignatureDelta`**（`relay_claude_test.go:676`，断言 `require.Nil`）。**同一张表里三个用例被混作一团**。
3. **§9.3 的门禁示例输出计数过期**（「relaykit 47 + 宿主 98 + upstream 全路径 21」）——第 36 轮写的数，第 37/38 轮各加引用后未同步。**这是教训 93（门禁自述计数是自盲区）的第二次复发**，且这次漏的是 §9.3 而非检查器描述段。

**另发现一处判据覆盖面缺口（比上面三处更实质）**：阶段 4 行判据与 §12.16.2 决定 2 都写「每一处差异都要能对应到**处置清单第 6 项为 9.1/9.2** 逐条产出的决定」，而第 6 项在第 37/38 轮已扩到 9.2.1/9.2.2/9.2.3——**判据的覆盖面没跟着扩，新增的 23 条差异因此不在任何完成判据的射程内**。已同步四处（阶段 4 判据、§12.16.2 决定 2、§9.3 的判据注释、§12.16.2 的边界段与 12.16 决定 2）。**其中 9.2.1 第 9 条与 9.2.3 第 8 条会产生快照可见的输出变化**，须在判据里与「错误路径由专项单测覆盖」区分开。

**教训**：

- **口径混用不止发生在行号，也发生在计数**。第 51 条教训只说了行号，本轮 `converse.go` 的函数数是同一个病：**同一句话里「1206 行（基线口径）」与「33 个函数（实为 HEAD）」并存**。凡在「基线口径」句子里写数量，每个数都要单独跑基线命令。
- **一张表里引用多个测试用例时，逐个打开它断言什么**。「钉在本地语义上」这类概括句最容易把相邻条目的用例串到一起——本轮第 9 条同时错引了第 4、5 条的用例。
- **判据的覆盖面必须与它所引用的清单同批扩展**。处置清单第 6 项从「9.1/9.2」扩到五个小节，而引用它的**四处判据文本**（阶段 4 行、§12.16.2 决定 2、§9.3 注释、12.16 决定 2）有三处没动——**扩展一个清单时，要 grep 出所有引用它的判据**，否则新增项在完成判据之外。
- **门禁自述计数的自盲区会从检查器描述段扩散到文档正文**。教训 93 记的是「检查器描述段」，本轮漏的是 §9.3 的命令注释——**同一个盲区的第二处**。修法不变：改动引用后，grep 全部计数位置（`grep -n 'relaykit [0-9]*'`、`grep -n '/[0-9]*/（'` 等）。

**第三十八轮之三：Codex 复核报告落地（9 项，全部独立复现后修正）**。Codex 走查（19016 行记录，约 2 小时，只读）给出 9 项：4 个 P1、5 个 P2、无 P0。**我逐项独立复现，9 项全部成立**，已全部修正。

**P1 四项**：

1. **阶段 6 判据与形态 C 直接矛盾**——判据写「9 个通道级转换器已全部删除」，而 §12.20.1 的 C6 要求 4 个非 WithError 函数**保留为一行 facade**，只有 5 个真删。已改为按未决项 33 的形态分读（A/B 全删、C 删 5 保 4）。
2. **§8.1 形态表仍是 A/B 两行**，而阶段 4 处置清单第 14 项已是三选一、§12.20 已推荐 C——**表与它自己的引用不一致**。已补 C 行（含回滚面「revert `service/convert.go` 一个文件 + 删 3 个新文件」）。
3. **§12.20.1 表头声称「全部 `HEAD` 口径实测」，但通道级 4 个行号是基线值**——HEAD 实测 `relay-claude.go:1252`/`:1366`、`relay-gemini.go:1669`/`:1832`、`aws/converse.go:696`，文档写的是基线 `1167`/`:1273`/`:1605`/`:1768`。已把表头改为「行号一律基线口径」，HEAD 值只作括注。
4. **Gemini 合成 ID 测试仍归在 §9.2.1**——处置清单第 16 项把 `tool_call_id_test.go` 4 处一并算作「9.2.1 被丢弃的本地严格性」，但它们钉的是 **9.2.3 第 8 条**（Gemini 合成 tool call ID 的跨 chunk 唯一性）。已按小节拆开归属。

**P2 五项**：A1 影响面漏列 `response_registry.go:1063`（三处调用点应为 `:1055`/`:1063`/`:1071`）；§9.2.3 第 5 条未区分非流式（`:128` 有 `[media]` 降级）与流式（`:231-245` 只有 image 分支、非 image 被静默丢弃）；引用计数自述 103 与 107 矛盾（第 448 行写 103，第 456/747 行写 107）；阶段 4 判据残留「判据覆盖面未随之扩大」这句已过期的自相矛盾文字；`converse.go` 段写「本段为唯一落点」而 §12.20.1 的 C 表已列 `:696`。

**修正过程中的新发现（我自己引入、当场抓到）**：我给 §12.20.1 补 HEAD 行号时写的是 `1666`/`1829`，**当场复核发现实测已是 `1669`/`1832`**——同一轮之内 gemini 文件又漂了 3 行；同一张表里 `relay-gemini.go:1845`（第 38 轮的值）也已漂到 `:1848`。**这正是发现 #3 的同一个病**：HEAD 口径行号没有任何门禁保护，任何会话的提交都会让它静默失效。已在两处都改成「行号只作定位参考、一律以函数名为准」，并把实测 commit 写进括注。

**教训**：

- **「表头声明的口径」与「表内实际用的口径」是两个可以各自出错的独立事实**。发现 #3 不是行号算错，而是**行号对、表头错**——我用基线行号配了 HEAD 表头。核对引用时，先读表头声明的是哪个 ref，再核行号属于哪个 ref，**两个动作不能合并成「行号对不对」**。
- **同一份文档里对同一批测试用例可以有两种正确归属，取决于「钉住什么」而非「写在哪个文件」**。`tool_call_id_test.go` 在处置清单 16 里被归给 9.2.1，在 §12.20.1 通道级测试面段里被正确归给 9.2.3——**同一个文件里两处归属相反，而两处都能自圆其说**（一处按「它调的是 `*WithError`」、一处按「它断言的是 Gemini ID」）。判据是**断言的内容**，不是调用的函数。
- **无门禁保护的口径会在「同一轮之内」失效**。我在本轮补的 HEAD 行号，在本轮复核时就已漂了 3 行。**任何写进文档的 HEAD 口径数字，都必须与「以符号名为准」的兜底句同时出现**，否则它从写下的一刻起就在倒计时。

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
| （本轮·第二十四轮） | 复现「`RelayInfo` 实现 `convmeta.Meta` 全部 15 个方法」：**接口确为 15 个**（机械枚举），但本地 `RelayInfo` 现有 26 个方法中**只有 2 个重名**（`GetEstimatePromptTokens`、`AppendRequestConversion`），**须新增 13 个**。上游已全部实现于 `relay_info.go:825-953`（含断言 `:823`），可整体照抄；12 个是纯访问器，**仅 `ConvOptions` 需新写逻辑**（import `model_setting`，本地 `claude.go:55`/`gemini.go:53` 已有，且本地**无** `convOptions` 缓存字段）。阶段 3 判据由「实现 15 个」改为「13 个逐个落地」 |
| （本轮·第二十五轮） | 核阶段 4 处置清单第 4、5 项（**「反向新增」类**任务）：第 4 项上游**无同名方法**，且 `kitutil` **不提供 `RawMessage` 类型别名** → 须把 `[]common.RawMessage` 改 `[]json.RawMessage`；第 5 项「门控留宿主」**没写落点**——门控 3 个调用点全在 `convert.go`（阶段 6 删），必须落到注入机制。另修正一个**幻影符号**（`extractThinkTaggedHistory` 实为局部变量） |
| （本轮·第二十六轮） | 核阶段 5：**发现「必然分叉」与「避免分叉」的矛盾**——三处补丁都要改写 relaykit 内部源码 + 给 `convmeta.Options` 新增字段，而 12.9/12.15.4 两处写「避免分叉」，且规格**从未汇总改了哪些文件**。补 8 条「本地改动清单」表（核实过的行号），并要求另立 `relaykit/LOCAL_PATCHES.md`。**顺带修一处假绿**：新表路径原缺 `relaykit/` 前缀，6 处新引用一度在门禁之外（检查器仍报 10/10），补前缀后升到 16/16 |
| （本轮·第二十七轮） | 核阶段 5 判据能否验证 12.4 的语义条件，**发现更硬的问题：阶段顺序自相矛盾**——门控排在阶段 5，但阶段 4 判据「8 个快照逐字节一致」已要求门控生效，而上游 relaykit **默认无条件暴露 reasoning**（实测 `ResponseOpenAI2Claude(resp, nil)` 函数体 0 次引用 `info`、`:463` 无门控、整文件 0 处 `Options`）→ 阶段 4 必红且豁免条款覆盖不到。决定：门控移入阶段 4（处置清单新增第 13 项），阶段 5 只核验位置语义。**连带修出 12.4 自身四处错误**：钩子 nil 语义写反（`nil = 不保留` 应为 `nil = 保留`，否则破坏上游默认）、单参钩子复现不了三名字门控（须调用点各调一次取或）、插入点写错一层（须在外层二选一之前归零，否则同 chunk text 被丢）、同 chunk reasoning+text 是本地能力却未列入 9.1（已补第 7 条）。另修正 12.4.3 的落点文件名错误（`to_claude_messages_stream_resp.go` 不在 `oai_chat/` 下）。新增引用一度写成工作区口径（`:800/830/857`、`:544`），已按 HEAD 口径改正（`:793/823/850`、`:537`） |
| （本轮·第二十八轮） | 核 §12.15.4 被多个判据引为「已实测」的两条边界——**实测后两条都不成立，且依据表把 schema 归一化标为「已内置」，掩盖了一个行为回归**。边界 1（schema）：本地 `normalizeClaudeInputSchema` 报错语义 vs 上游 `FunctionParametersToInputSchema` 静默降级，4 处差异（新增 §12.15.4.1）。边界 2（媒体）：上游 `switch` 只有 `case "text"` 与 `default`，`default` 里非 pdf 一律当 image——**`text/*` 文件被当图片发给 Claude**；另发现上游**无 `ContentTypeFile` case**、`ToFileSource()==nil` 静默丢内容、无 MIME 归一化、本地自身 pdf 判定不一致（`:122` 前缀 vs `:197` 精确），共 7 处差异（新增 §12.15.4.2）。9.2 补第 7、8 条（6→8）。退路条款被激活但原文写错（差异部分是纯函数、本就该在 relaykit，须**改上游实现**而非「迁入」），与「避免对 relaykit 产生分叉」冲突——按「保行为优先」处置，本地改动清单新增第 10、11 行。**顺带修门禁盲区（第 26 轮假绿复发）**：检查器报 18/18 绿，而 **23 处唯一简写引用**（`internal/...`、`oai_chat/...`、`convmeta/...`、`shared/claude/...`）全在门禁外——旧正则要求 `relaykit/` 前缀；加显式映射表 `SHORT_PACKAGES` 后覆盖 18→**34/34**，单测 10→15（先写测试确认 3 个失败再实现），反向验证短路径越界/文件不存在均能检出。**又踩一次「示例文本进入门禁」**：文档里写 `internal/oai_chat/foo.go:12` 当示例被检查器当真引用，已改占位符 |

| （本轮·第三十三轮） | 换查「回滚承诺」。§8.1 要求阶段 4「单点可逆」（收敛为一个开关或一次集中的函数体替换），而 12.3 决定**删除**包装层、12.16.2 要求 **8 个调用点逐个改写**（5 个生产调用点分布在 4 个文件）——**多点与单点相反**，两条都写着、都未被指定为唯一形态，**回滚路径实际不存在**。§8.1 增「形态 × 阶段 4 × 阶段 6 × 是否满足」对照表 + 处置清单第 14 项 + 阶段 4 判据 + 未决项 33。顺带：4.5.5 表写「重复的 4 个」而 **4 行里 2 行各含 2 个函数（共 6 个）**，阶段 6 判据「4 个已删除」是行数口径、照它验收会漏 2 个函数 → 判据改为逐个列 6 个函数名；§8 表第 6 条（AWS Converse）「需单独处理」全文无指针，实际已在 12.4.3 处置（维持现状）→ 补上结论与两条禁止 |
| （本轮·第三十四轮） | 换查性能/运行时维度（前 33 轮从未查过）：①§12.10「无需改动」只对上游成立——本地 `InitChannelMeta`（`relay/common/relay_info.go:364`）无 `convOptions` 字段与重置行，判据「首次调用建快照」不重置也满足；重置点现成（`controller/relay.go:1016` 重试循环内）。②阶段 3 照抄区间 `:823-953` 实含 14 个方法，`GetBillingModelName` 不在接口内（照抄需凭空加字段）；13 个接口方法里 `ReasoningState()`/`ConvOptions()` 各依赖一个本地没有的字段（`ReasoningConversion` `:123`、`convOptions` `:201-202`）→「12 个纯访问器」改「11 + 2 需新字段」。③**思考等级宿主填充器全链路缺席**：relaykit README 明示不解释尾缀，4 处 `ReasoningStateOf` 消费点 + `ApplyClaudeThinkingModel` 全靠 `info.ReasoningConversion`；上游入口 `relay/helper/reasoning_suffix.go`（232 行）+ `model_modifier.go`（278 行）本地都不存在、全文零提及；本地内联处理（`relay-claude.go:430-478` 阶段 4 删、`claude_handler.go:55-104` 换装零落点）；8 个 golden 快照零后缀用例抓不到回归。新增 §12.19 + 处置清单 15 + 阶段 4 判据 + 未决项 34；引用 40→45，门禁 45/45 |
| （本轮·第三十五轮） | 换查「上游迁移完成后的宿主终态」：**上游保留旧名，文档全删全改写**。实测 `upstream/main`：`service/convert.go` 27 行 5 个同名 facade（旧签名不变）、`request_converter.go:50/:62` 两个同名 shim、`relay-claude.go:37/41` 与 `relay-gemini.go:139/143` 通道级 facade、`response_compat.go` 约 40 个同名包装、`conversion_diagnostics.go` + `RecordConversionDiagnostics`；真正被删的只有 `*WithError`×4 与 `IsDeepSeekModelName`/`HasDeepSeekModelName`。由此：①测试面 82 处调用（4 文件：35/14/25/8；阶段 4 断 38、阶段 6 断 44）按字面形态**编译失败**，12.16 的「失败得其所」不成立且只有 golden 一个文件有认领；②12.3「4 个文件需改写」（对）与 12.16.1「两处例外」（错）矛盾六轮；③§8.1 薄封装的 6 个函数含 `*WithError` 名，与上游保留的 5+2 不同；④4.5.5 映射列指向 internal（宿主不可达），公开同名包装在 `response_compat.go:50/100/140/144`；⑤§3.1 的 diagnostics 能力只在包装层形态下成立。新增 §12.20；12.3 撤回半句；§8.1 扩为三选一（C＝照抄上游终态，推荐）；12.16 第 2 项重写（含逐文件表）；4.5.5 映射列修正；§10/§11/阶段 6/未决项 33 同步。引用 45→47，门禁 47/47 |
| （本轮·第三十六轮） | 把宿主路径补进门禁（第 23 轮补全路径、第 28 轮补简写后的第三类）：①三者皆不可解析 1 处——`service/convert.go:533` 应为 `:526`（另修 `:512`→`:505`、`:544`→`:537`）；②基线不可解析但上游可解析 6 处（漏 `upstream/main:` 前缀）——`service/openai_chat_responses_mode.go:15` 两处（本地有同名文件，靠巧合解析）、`relay/responses_request.go:43`、`service/request_converter.go:50`、`relay_info.go:562`、上游 6 调用点清单 2 处；③两口径分歧 15 处逐条裁决：13 处文档确按基线书写，2 处是 34/35 轮按 HEAD 混入（`InitChannelMeta` `:364`→`:358` 4 处、`relay-claude.go:430-478`→`:366-414`）；④门禁落地：裸宿主 98 处在基线解析、`upstream/main:` 全路径 15 处在上游解析，单测 15→25，反向验证 5 次扰动全 exit 1；⑤反向验证示例 `service/convert.go:99999` 当场被新门禁击中（示例进引用第三次复发），改占位符；⑥清掉门禁自述的 3 处过期计数（34→47 处、15→25 单测、16→160 合计）；⑦新增 §12.20.1（形态 C 连带清单：必改 17 / 零改动 63 / 有意差异 2；C1–C6 与回滚点）+ 处置清单第 16 项，并发现 `IsDeepSeekModelName` 须活过阶段 6（与上游终态的必要差异）及选 C 须改阶段 6 判据 |
| （本轮·第三十二轮） | 按「还剩什么没 owner」逐条核，答「主体可执行，差 3 处」——补的过程中找到第 4 处。①12.5 的 3 个「需实测」方法（`Usage.EnsureResponsesTokenDetailObjects`、`Usage.GetOutputTokenDetails`、`OpenAIResponsesResponse.HasImageGenerationCall`）**无任何阶段或处置项认领**，而阶段 2 判据要求「全部宿主独有方法给出出路」→ 新增「阶段 2 的前置」整节 + 判据追加一条 + 未决项 32。②其中 `EnsureResponsesTokenDetailObjects` **在 `HEAD` 上不存在**（工作区独有），文档第 214 行早记过「HEAD 与工作区差 3 个方法」但 12.5 表未引用它——分类对象尚未进入版本控制。③12.5「生产引用」列声明口径是「工作区；排除 `_test.go` 与 `dto/`」，实测 23 行里 **21 行是「名字匹配的文件数」、2 行（Clone）是调用口径**，两种口径混在一列。④顺带发现该名字匹配口径把 `common/responses_request_metadata.go:118` 的字段声明与 `:163` 的赋值也计入，解释了勘误表里 `HasCompactionTrigger` 改后仍偏高。只补注口径不改数字（分类不依赖该数）。另修一处文档内部会漂的行号引用（「第 208 行」实为 214 行） |
| （本轮·第三十一轮） | 核 §12.16.2「4 个输出类型共有字段同序」。**文档自标「一次性抽查、不是门禁」并建议扩维度——建议从未执行，抽查结论也是错的**。三处错：①「4 个输出类型」里有 1 个是**输入**类型（`OpenAITextResponse`），真正 marshal 的顶层类型是 3 个；②需同序的是**可达闭包**（递归），实测本地 28 / kit 36 / 共有 28 个类型，其中 **2 个顺序不同**（`ClaudeMediaMessage` 的 `Signature`/`Data`、`FunctionRequest` 的 `Strict`/`Arguments`）；③「全部一致」不成立。本基线仍安全，但理由是「那两对字段在快照里恰好都为空」（偶然），不是「两侧一致」。修法：给工具加**第五维 orderDiff**——根因是 `scan()` 用 `map[string]fieldInfo` 存字段，**map 丢失声明顺序**，顺序维度从第 15 轮起就不可能实现；改返回 `orders map[string][]string`（嵌入字段也计入），新增 `sharedOrderDiff`（只比共有字段，避免与 fieldSetDiff 重复计数），`--strict` 加 `wantOrder=4` 并反向验证（改 3 → 退出码 1）。新增 3 条单测（10 个通过）。文档侧重写该段、§9.3 改六维基线、12.0.6 维度数改五、阶段 4 判据补一条 |
| （本轮·第三十轮） | 核 §12.16.2/12.16.3，从「9.1/9.2 的 13 处」这一个聚合计数查出 **8 处错误**：计数 5 处（13→15；「6 个调用点」实为 6 函数/8 调用点；12.0.8 的「全部 15 处」实为 tag 5 + 类型 6 = 11；12.0.6 的「等价拼写 15 处」实为 21；类别 C「3 类型 4 字段」实为 4 类型 5 字段）、事实 2 处（`mustUnmarshalFixture` 并非「同签名」——上游 `(raw string, out any)` vs 本地 `(t, raw, v)`，决定阶段 6 合并须改本地 8 处调用；「与 15 处差异无一重叠」不成立，至少 3 处重叠，结论仍成立但理由应改为「重叠的几处是 struct 值空操作」）、死代码 1 处（2 个 fixture 常量零引用）。**根因是工具门禁漏掉一整个维度**：`--strict` 期望数组 5 项而工具打印 6 组，等价拼写组从未进 gate——已加 `wantEquiv=21` 并反向验证（改 20 → 退出码 1）。另核实 IDE 报的 `main.go:50` embed 诊断与本轮无关（HEAD 原样、前端未构建） |
| （本轮·第二十九轮） | 核 §12.11（`reasonmap` 宽松 default）：原文写「严格化在宿主调用点做」，**实测该落点在阶段 6 后不存在**——严格校验的两个唯一调用点（`service/convert.go:738`、`:1057`）都在阶段 6 删除的文件内，宿主 5 个 Claude 方向调用点全部只做错误转发，relaykit 侧 `reasonmap.OpenAIFinishReasonToClaudeStopReason` 是纯包级函数、零钩子（与 12.4 的 `Options` 钩子、12.15.4 的 `MediaResolver` 不同）。且差异被少算：实际 **5 条**（未知值报错、`""`、`TrimSpace`、`function_call`、`content_filter` 无差异），其中 3 条是行为回归；`""` 那条因 `stop_reason` 带 `omitempty` 会整字段消失，且两条 golden 基线都测不到。`TestResponseOpenAI2ClaudeRejectsUnknownFinishReason` 调用的函数阶段 4 即删，是「编译失败」而非「失败得其所」——12.16.1 的第二个例外。§12.11 重写为 12.11.1/12.11.2/12.11.3（二选一：A 加钩子 / B 宿主 5 处前置校验），同步 9.1 第 1 条、处置清单 9、§10 收尾 12、§12.17 第 12 行、阶段表第 4 行判据、本地改动清单第 12 行、未决项 31。引用 34→40，门禁 40/40，反向验证通过 |

| （本轮·第三十七轮） | 让外部模型（Codex）独立复核，再逐条复现其结论：**P1×2 与 P2 全部成立**——①阶段 5 行与 §12.20.1/处置清单 16 争抢同一批产物（包装层、diagnostics、流式状态）→ 阶段 5 改「只核验不落地」；②12.11.3「选项 A 布尔钩子」**不可实现**（`reasonmap` + `oai_chat` 3 个函数全无 error 返回）→ 拆 A1（改 3 个签名）/ A2（钩子选分支+宿主校验）/ B；③4.5.5「4 组 6 个」实为 **9 个**（漏 3 个），且「30 个 / 17 包」无命令可复现 → 换为可复现口径 **45 个 / 20 包**，阶段 6 名单 6→9。**它自己的 1 条「确认无误」抽查指错了文件**（把 `relay-claude.go:255` 说成 `service/convert.go:255`）→ 外部复核的通过项也须复现。**复核中另牵出 3 处更硬的缺口**：④`relay/channel/aws/converse.go`（1206 行、33 函数、上游零覆盖）从未进入任何盘点；⑤通道级 5 个生产 + 6 个测试调用点未被认领，且**上游对这批调用点处理不一致**（`:1167` 用带 error 的 `ConvertChunk`，另 3 处用无 error facade）；⑥被替换的 9 个函数自带的**另一套严格性**无盘点 → 新增 §9.2.1（9 条，含 1 条 nil pointer panic、1 条 `signature_delta` 输出变化）与 §9.2.2（6 条反方向 stop_reason，含 `pause_turn` 的 stop/length 语义分歧）。引用 47→50（宿主 98→103、upstream 全路径 21→24），门禁三组全绿 + 反向验证 5 次扰动全 exit 1 |
| （本轮·第三十八轮） | 自查第 37 轮刚写入的内容 + 把「被替换函数的错误路径要单独盘点」这条教训做完整。**自查抓到 3 处自己引入的错**：①`relay-gemini.go:1819` 已过期（另一会话提交 `222876349` 使 HEAD 现为 `:1845`）——**HEAD 口径行号是文档里唯一不受门禁保护的一类**；②`tool_call_id_test.go` 被误指为「钉 9.2.1 的 PartialJson 守卫」，实为钉 Gemini 合成 ID 的跨 chunk 唯一性（跨通道同名测试张冠李戴）；③§9.2.3 初稿的行号混了工作区口径（`:1369`/`:1525` 应为基线 `:1326`/`:1472`；`:1368`/`:1455` 应为 `:1366`/`:1453`）——**第 51 条教训在写下它的下一轮复发**。**续作**：第 37 轮只盘了 Claude 一半（9 个函数里 4 个属 Gemini）→ 新增 **§9.2.3**，Gemini 侧 8 条差异，含**第二个 nil 解引用 panic**、未知 `finishReason` 被静默改写为 `ContentFilter`、以及**合成 tool call ID 由确定性变随机 UUID**（`protocol_conversion_test.go:318` 钉住，relaykit 方案必然使其失败）。门禁 50/103/24 → **51/107/24**，自述计数同批更新 |
| （本轮·第三十八轮续） | 让 Codex 独立复核（第二次），同时自己把全部断言逐行复现。**我这侧**：§9.2.1 的 9 行、§9.2.2 的 6 行、§9.2.3 的 8 行本地与 relaykit 行号**全部逐行命中**；§4.5.5 的 45/20（=36+9）命令复现一致；§12.20.1 生产表五数量（9/5/5/3/0）逐个复现；§12.11.3 推断链完整；converse.go 基线 1206 行/32 函数/3 交互点；通道级测试面基线 22 处/2 文件、HEAD 30 处/4 文件。**自查又抓到 3 处**：①`converse.go` 的「33 个顶层函数」是 HEAD 口径混入（基线 32）——**口径混用从行号扩散到计数**；②§9.2.1 第 9 条把钉 `signature_delta` 的 `TestStreamResponseClaude2OpenAIIgnoresOpaqueSignatureDelta`（`relay_claude_test.go:676`）错写成第 4、5 条的两个用例；③§9.3 门禁示例输出计数过期（47/98/21）——**教训 93 的第二处**。**第 4 处（第 37 轮遗留的过强断言）**：§12.20.1 写「上游 `relay_claude_test.go` 402 行、无 `RequestOpenAI2ClaudeMessage` 引用，即上游把本地这 18 处覆盖**整体丢弃**」。402 行与「0 引用」属实，但**结论过强**：上游改调 `relayconvert.OpenAIChatRequestToClaudeMessages` 后新写了 2 个用例（`relay_claude_test.go:350`/`:377`，覆盖思考等级适配一条），而本地 12 个用例覆盖的 12 个行为里**只有 1 条有对应**。「整体丢弃」读起来像「不必处理」，正确表述是「只承接 1 条、其余 11 条无对应」，**动作完全不同**——前者要求阶段 4 逐条决定移植或放弃。已改正。**另发现判据覆盖面缺口**：处置清单第 6 项已扩到 9.2.1/9.2.2/9.2.3，而引用它的**四处判据文本**（阶段 4 行、§12.16.2 决定 2、§9.3 注释、12.16 决定 2）仍写「9.1/9.2」——**新增的 23 条差异不在任何完成判据的射程内**，已同批扩展 |
| （本轮·第三十八轮·Codex 复核落地） | 让 Codex 独立复核（19016 行记录、约 2 小时、只读），得 9 项（P1×4、P2×5、无 P0），**我逐项独立复现，9 项全部成立**，已全部修正。**P1**：①阶段 6 判据「9 个通道级转换器已全部删除」与 §12.20.1 的 C6「4 个保为一行 facade」直接矛盾 → 按未决项 33 的形态分读；②§8.1 形态表仍只有 A/B 两行，而处置清单第 14 项已是三选一、§12.20 已推荐 C → 补 C 行（含回滚面）；③§12.20.1 表头声称「全部 `HEAD` 口径实测」，但通道级 4 个行号是基线值（HEAD 实测 `relay-claude.go:1252`/`:1366`、`relay-gemini.go:1669`/`:1832`、`aws/converse.go:696`）→ 表头改为「行号一律基线口径」，HEAD 值只作括注；④处置清单第 16 项把 `tool_call_id_test.go` 4 处一并归给 9.2.1，实为 **9.2.3 第 8 条** → 按小节拆开归属。**P2**：A1 影响面补 `response_registry.go:1063`（三处调用点 `:1055`/`:1063`/`:1071`）；§9.2.3 第 5 条拆为 5a（非流式 `:128` 有 `[media]` 降级）/5b（流式 `:231-245` 只有 image 分支、非 image 被静默丢弃）；引用计数自述 103→107；删阶段 4 判据里已过期的「判据覆盖面未随之扩大」；`converse.go` 段「唯一落点」改为「首次盘点、非唯一」。**修正中我自己引入又当场抓到**：补 HEAD 行号时写 `1666`/`1829`，复核发现实测已是 `1669`/`1832`（同一轮内又漂 3 行），同表 `relay-gemini.go:1845` 也已漂到 `:1848`——**发现 #3 的同一个病**，已在两处改为「以函数名为准、行号只作定位参考」并写明实测 commit `a0491b310`。门禁三组全绿（51/107/24 + 25 单测 + dto strict 33/5/6/0/4/12/21） |

文档当前 1954 行，位于 `docs/superpowers/specs/2026-09-30-relaykit-migration-design.md`。
