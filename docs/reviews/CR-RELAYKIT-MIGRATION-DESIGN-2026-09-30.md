# CR: 协议转换层收敛到上游 relaykit 的设计与四轮对抗审查

采样时间：2026-09-30
本地基线：`9c4758b53`（dev 分支；记录写作时 HEAD = `a1d5e9aad`）
上游参照：`upstream/main` = `789c97019`（2026-09-28）

## 授权与边界

本记录只做设计与审查，**未修改任何生产代码**。全部改动限于设计文档：

- `docs/superpowers/specs/2026-09-30-relaykit-migration-design.md`

工作区存在其他会话的 500+ 未提交改动，全程未触碰、未回退、未整理。所有上游代码经 `git show upstream/main:<path>` / `git grep ... upstream/main` 读取，未执行 `git checkout`。

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

**保留在宿主**（部署关切）：响应方向 reasoning 暴露策略、DeepSeek 空 thinking block 保留、think 标签提取族、工具名契约、`context_compaction` 归一化、加密内容探测。

**本地专有契约（上游全仓 0 命中）**：`types/error.go` 的 `ErrorCodeUpstreamTransportInterrupted`（25 文件引用）、`IsUpstreamTransportInterruptedError`（9）、`IsOpenAIRequestParameterError`（5）。上游宿主 `types/` 没有 `error.go`。

**本地 DTO 独有方法（13 个，全部在生产使用）**：`ParseContentStrict`(20)、`ParseToolCallsStrict`(16)、`Clone`(13)、`HasCompactionTrigger`(10)、`HasCompactionOutput`(4)、`EffectiveReason`(2)、`SearchToolNameByToolCallId`(1) 等。relaykit 对应类型无这些方法，迁移即编译失败。

**本地独有包**：`service/openaicompat/`、`relay/common/responses_*.go`、`dto/` 若干文件、`*WithError` 系列（4 个函数 9 个生产调用点）。

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

## 未决项（设计文档第 10 节，15 项）

其中列为**阶段 0 前置**的一项：

1. **完成本地独有能力盘点**。`service/openaicompat/`、`relay/common/responses_*.go`、本地独有 `dto/` 文件尚未逐项核验。第四轮负责该盘点的子代理在读完文档后用尽上下文，未产出完整清单。**当前设计文档第 4 节的清单完整性有限**，不能带着它进入阶段 4。

其余主要未决项：think 标签提取族归属、本地专有错误符号归属、`*WithError` 系列形式、响应方向暴露策略具体形态、13 个 DTO 独有方法归属、hosted 工具收敛方式、`types` 别名转发适用范围。

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
