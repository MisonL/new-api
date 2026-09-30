# 协议转换层重构设计

采样时间：2026-09-30
本地基线：`HEAD` = `d917aae57`（dev 分支，领先 `origin/dev` 37 个提交）
上游基线：`upstream/main` = `789c97019`（2026-09-28）

## 1. 目标

把本地协议转换层从「单文件硬编码直调」重构为「协议层与部署层分离」的架构，采用上游 `relaykit` 独立 module 作为协议层，本地部署关切保留在宿主并收敛到明确的管道位置。

本设计同时修复三个现存缺陷：

1. `grok-4.7` 等非 DeepSeek 模型的 reasoning 被静默丢弃
2. 降级行为不可观测（本地 `ConversionDiagnostic` 零命中）
3. 本地 CI 不执行任何 `go test`，22205 行转换测试无门禁

## 2. 边界规则

分层判据：

> relaykit 拥有「协议要求什么」，宿主拥有「这个部署或上游要求什么」。
> 判定：换个上游还成立吗？成立 → 协议层；不成立 → 部署层。
>
> 否决 1：若成立与否取决于模型名，一律判部署层。
> 否决 2：若实现引用宿主发明的字面量，一律判部署层，或降级为接口注入。
> 操作 3：若同一能力在多个调用点得到不同答案，先拆分，再对每一半单独判定。

该规则由对抗审查修正而来，原始二分版本在 15 项中有 8 项判错。规则的机械对应物是 relaykit 的 `boundary_test.go`（禁止 relaykit 内 import 宿主包与 gin），两者互补：前者管能力归属，后者管依赖方向。

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

relaykit 视为本仓库自有代码，允许修改。补丁限定为三项，全部属协议语义。

### 补丁 1：Claude thinking block 映射

现状：上游两个请求转换器的 content block switch 均无 `thinking` case，且**无 `default` 分支**，未知 block 被 Go switch 静默穿透丢弃。

- `relaykit/relayconvert/internal/claude_messages/to_oai_chat_req.go:181-227`
- `relaykit/relayconvert/internal/claude_messages/to_oai_responses_req.go:165-215`

改为：新增 `case "thinking"`，将 `ClaudeMediaMessage.Thinking` 写入目标协议的 reasoning 字段（Chat 写 `Message.ReasoningContent`，Responses 写 reasoning item）。opaque thinking（`Thinking` 为空但 `Signature` 非空）与 `redacted_thinking` 显式报错，不伪装为文本。

理由：Claude thinking block 到 Chat `reasoning_content` 是纯协议映射，不依赖任何上游特性。上游缺此分支属遗漏。

### 补丁 2：DTO presence 保真

现状：`relaykit/dto` 的可选标量字段缺失显式零值追踪。

- `OpenAIResponsesRequest` 无 `UnmarshalJSON`、无 `Extra`，未知字段在往返中被静默丢弃
- `StreamOptions` 为两个裸 bool，无 `HasIncludeUsageField()` 等 presence 查询
- `ResponsesStreamResponse` 无 `Error`、无 `CallId`、`Code` 为 `string` 而非 `any`

改为：可选标量用带 `omitempty` 的指针类型；`OpenAIResponsesRequest` 补未知字段保留；`StreamOptions` 补 presence 追踪。

理由：AGENTS.md 第 3 节明确要求「客户端 JSON 解析后再转发给上游的可选标量字段，必须用带 `omitempty` 的指针类型保留显式零值」。未知字段丢弃与「禁止静默容错」冲突。

### 补丁 3：reasoning 暴露改为客户端意图门控

现状（本地）：响应方向按**模型名**门控，非 DeepSeek 模型的 reasoning 被整段丢弃。

```go
// service/convert.go:533-535, 调用点 :756（流式）、:1056（非流式）
func shouldExposeReasoningAsClaudeThinking(model string, info *relaycommon.RelayInfo) bool {
    return hasDeepSeekModelName(model, info)
}
```

现状（上游）：无条件暴露，客户端未请求 thinking 时也会收到 thinking block。

改为：按**客户端意图**门控。客户端请求了 thinking（`ClaudeRequest.Thinking` 非空或 `GetEfforts()` 非空）才暴露 reasoning，否则不暴露。

实现：在 `convmeta.Options` 增加宿主注入的钩子，由宿主从 `info.Request` 读取原始请求意图填充。

理由：按模型名门控是错误维度（否决 1）；无条件暴露不符合 Anthropic 行为（未请求 thinking 不应返回 thinking）。按客户端意图门控是协议正确行为，且一并修复 grok 的 reasoning 丢失。

可达性已核验：`relay/common/relay_info.go:252` 持有 `Request dto.Request`，`dto/claude.go:229` 有 `Thinking`、`:445` 有 `GetEfforts()`。

### 明确不打入 relaykit 的能力

以下按边界规则属部署层，保留在宿主，不进入 relaykit：

| 能力 | 判定依据 |
|---|---|
| custom / namespace / tool_search 工具桥接 | 依赖渠道能力快照，部署关切 |
| DeepSeek 空 thinking block 保留 | 模型名门控（否决 1） |
| 工具名契约与别名还原 | 引用宿主常量，且受 `IsOpenAICompatibleResponses` 门控 |
| `context_compaction` 归一化 | 上游版本垫片，上游零命中 |
| 加密内容探测 | 硬编码宿主 marker `newapi.synthetic.compact:`（否决 2） |

## 5. 宿主管道

保留在宿主，但收敛到明确的管道位置，消除现状中「协议映射与部署特判交错」的问题（现状 `service/convert.go:368-371` 是协议、紧邻的 `:372-380` 是 DeepSeek 特判）。

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

1. 渠道匹配逻辑收敛：`controller/relay.go:1829-1847` 与 `:1986-2004` 手写了 `IsChannelEnabled` 的等价逻辑，改为复用 `rule.IsChannelEnabled`
2. `ChatCompletionsToResponsesPolicyWarnings` 无生产调用方（仅定义与测试），接入设置 API，使管理员能看到「配了不生效」的告警
3. 前端 `serializeProtocolPolicy` 无条件写 `enabled: true`（`web/default/src/features/system-settings/models/protocol-conversion-policy-utils.ts:750-753`），会把停用配置翻转为启用，修复

## 7. 分阶段落地

每阶段结束时仓库可编译、可运行、测试通过。

| 阶段 | 内容 | 完成判据 |
|---|---|---|
| 0 | `git fetch --unshallow`；建立 CI 门禁（两个 module 分别 `go test`） | CI 能跑通现有测试 |
| 1 | 引入 relaykit module（根 go.mod 加 require + replace）；DTO 拆分：协议 DTO 迁入 relaykit/dto，宿主 DTO 留 dto/ | 两个 module 均可编译；DTO 相关测试通过 |
| 2 | `convmeta.Meta` 接口实现：`RelayInfo` 补齐 15 个方法 | 编译期断言 `var _ convmeta.Meta = (*RelayInfo)(nil)` 通过 |
| 3 | 文本转换切换到 relaykit；打补丁 1、2、3 | 转换测试通过；grok reasoning 可见 |
| 4 | 宿主管道接入 relaykit 分派；规则模型附带修正 | 全量测试通过 |
| 5 | 删除旧代码（`service/convert.go` 主体、`service/openaicompat/` 中被替代部分） | 无残留引用 |

阶段 0 的 unshallow 是前置条件：当前仓库是 shallow clone（`.git/shallow` 截断于 `9724ef1b2`），`git merge-base HEAD upstream/main` 返回空，无法用 diff 驱动迁移。真实共同祖先 `dac55f0f` 存在于对象库但被 graft 遮蔽。

## 8. CI 门禁

现状：`.github/workflows/` 下 7 个 workflow 零 `go test`，`makefile` 无 `test` 目标。

改为：新增 workflow，对根 module 与 relaykit module 分别执行 `go vet ./...`、`go build ./...`、`go test ./...`（`GOWORK=off`），并缓存两个 `go.sum`。

理由：阶段 1 的 DTO 拆分涉及 340 个 import 点，无自动化门禁无法保证每步可运行。

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

- 每阶段运行 `go test ./controller ./model ./relay/common ./relay/helper ./service`（AGENTS.md 第 6 节）
- 阶段 3 需专项验证：构造含 thinking block 的 Claude 多轮工具调用请求，验证 DeepSeek 不再 400
- 阶段 3 需验证 grok 的 reasoning 可见（当前被丢弃）
- 前端改动运行 `bun run lint` 与 `bun run build`
- 验证记录沉淀到 `docs/reviews/CR-*.md`

不得以局部测试通过表述为全链路已验证。

## 11. 未决项

以下项在实现阶段需实测确认，本设计不预设结论：

1. `relaykit/dto` 补丁 2 的改动会否影响上游 golden 快照（`relaykit/relayconvert/testdata/golden/`，24 个 JSON），需实测后决定是否更新快照
2. `convmeta.Options` 新增钩子后，`ConvOptions()` 的缓存与失效语义（上游在 `InitChannelMeta` 末尾置 nil）是否满足本地跨渠道重试场景
3. 本地 `dto` 有 10 个文件 import gin，迁入 relaykit 前需拆分签名（如 `IsStream(c *gin.Context)`），需评估调用点数量
4. `relaykit` 上游仍在演进（`feat/relaykit-tools`、`feat/relaykit-perf`），补丁 1、2 可考虑提上游以减少分叉

## 12. 事实依据

本设计的承重事实均经独立核验（命令与结果见调研记录）：

| 事实 | 核验 |
|---|---|
| 上游 `service/convert.go` 27 行，本地 2236 行 | `git show upstream/main:service/convert.go \| wc -l` |
| 上游无 `service/openaicompat/` | `git ls-tree -r upstream/main -- service/openaicompat/` 空 |
| 两个 Claude 请求转换器 switch 无 `thinking`、无 `default` | 逐 case 提取 |
| 上游 `convmeta.Options` 零 tool bridge 字段 | `grep -c ToolBridge` 返回 0 |
| 上游 responses→chat 不处理 namespace / tool_search | `grep -c` 均返回 0 |
| 上游无 Responses→Chat 全局策略 | `git grep ShouldResponsesUseChatCompletions upstream/main` 空 |
| 本地 `ConversionDiagnostic` 零命中 | `grep -rl` 返回 0 |
| 本地 `dto` 被 340 个文件 import | `grep -rl` 计数 |
| 本地 `dto` 有 10 个文件 import gin | 逐文件核验 |
| 本地 CI 不跑 `go test` | `grep -rn "go test" .github/workflows/` 空 |
| `context_compaction` 上游零命中 | `git grep -c` 空 |
| 三个 `responses_tool_names*.go` 未跟踪 | `git status --short` 显示 `??` |
