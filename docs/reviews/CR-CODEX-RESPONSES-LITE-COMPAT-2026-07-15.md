# Codex Responses Lite 兼容适配验证

## 范围

采样时间：2026-07-15 至 2026-07-16 CST (+08:00)。

本轮处理 Codex 0.144.4 及后续版本在第三方 Responses API 链路上的四类兼容变化：

- Responses Lite 将工具定义放入 `input.additional_tools`，并发送 `reasoning.context=all_turns`。
- Codex 继续发送 `reasoning.encrypted_content` include，并在新版本中始终构造 reasoning 参数。
- Responses 历史 item ID 按 item 类型使用独立前缀。
- 未来新增的 reasoning、stream_options 和顶层请求字段不得在网关 JSON 往返或 Compact 转换中丢失。

本轮没有修改数据库 Schema、密钥、渠道凭证或正式环境配置。

## 上游证据

### openai/codex

- 最新稳定 release 为 `rust-v0.144.4`，发布时间 `2026-07-14T05:08:11Z`。
- 2026-07-16 复核主分支提交为 `38b064c31b1f7464b281006316ec878ed23fea77`。当前源码的 custom tool grammar 仍使用 `type=grammar`、`syntax=lark` 和非空 `definition`，没有发现第二种 grammar 线协议。
- PR #32206 已于 2026-07-10 合并，merge commit 为 `d2d00b6632dc991aa4471db0529773029cae5d68`。源码在所有 Responses 请求中构造 reasoning，并固定 include `reasoning.encrypted_content`。
- 同一源码明确仅在 `use_responses_lite=true` 时设置 `reasoning.context=all_turns`。`multi_agent_version` 不是该字段的直接生成条件。
- Responses Lite 把工具定义放入 `ResponseItem::AdditionalTools`，顶层 `tools` 置空。
- PR #32312 已于 2026-07-11 合并，merge commit 为 `c9d52de5ca52a6b4439e7c0f69b34f6331926bb4`。类型前缀包括 `at`、`msg`、`amsg`、`rs`、`lsh`、`fc`、`tsc`、`fco`、`ctc`、`ctco`、`tso`、`ws`、`ig`、`cmp`。

用户提供的 WSL 文档通过关闭 `use_responses_lite` 绕开第三方接口不支持 `reasoning.context` 的问题。该方法会同时关闭 Responses Lite 的工具承载方式，不是网关侧的完整修复；本仓库改为保留客户端能力，并按渠道显式适配。

### QuantumNous/new-api

采样主分支提交为 `a63364d156cf2a64f1c3d1ee4923d73d5f3222a1`。其 `Reasoning` 已包含 `mode` 和 `context`，但没有保留 reasoning、stream_options 和顶层请求的未来未知字段，也没有本轮完整 item ID 前缀、渠道三态能力与 effective tools 计量处理。

### Wei-Shaw/sub2api

2026-07-16 复核主分支提交为 `eb2b8632ded614bf991d7d36abfa38b513ad8c2d`。

- PR #4082 已合并，merge commit 为 `d774948e09fdb3f2b2e309c94443dc21aa59e8b9`。其核心是合并顶层 tools 与 `input.additional_tools`，用于 Responses 到 Chat Completions 桥接。本仓库原有桥接已具备等价能力，本轮补齐 token 计量和边界校验。
- PR #3951 仍为开放状态。上下文窗口超限映射为 400、`response.failed` 解析、首字节前上游中断重试等相关行为，本仓库已有对应实现和测试。账号亲和性持久化、pricing scheduler 和 Sub2API 账号池错误分类属于不同架构，不直接移植。

## 实现

### JSON 契约

- `Reasoning` 保留 `effort`、`summary`、`mode`、`context` 和未来未知字段。
- `StreamOptions` 保留已知字段和未来未知字段。
- 普通 Responses 与 Compact 相互转换执行深拷贝，避免调用方对象被改写。

提交：`2f20c0339 fix: preserve nested responses options`。

### Item ID

- 标准 item 类型严格要求 `<prefix>_<non-empty-suffix>`。
- 显式空 ID、无下划线 ID 和类型不匹配 ID 只删除 `id`，不删除 item，也不改 `call_id`。
- 未知 item 类型保持不变；遗留 `tool_call` 和 `mcp_tool_call` 保留既有 `fc` 兼容规则。

提交：`9a7f72e54 fix: align responses item id prefixes`。

### reasoning.context 渠道能力

能力状态为 `supported`、`unsupported`、`unknown`：

- `supported` 原样转发。
- `unsupported` 只删除 `reasoning.context`，保留 effort、summary、mode 和未来字段。
- `unknown` 原样转发并记录未验证动作，不做错误驱动静默重试。
- Chat Completions 桥接明确删除协议无法表达的 context，并记录独立动作。
- 包含 context 的请求即使开启 raw pass-through，也强制经过上述能力判断，避免旁路。

渠道 settings JSON 可通过 `responses_capability_registry.supports_reasoning_context` 显式覆盖 profile 默认值。使用日志写入 `responses_reasoning_context_action`、`reasoning_context_capability` 和剥离状态，default 前端日志详情可见。

提交：`e8cf15b04 fix: adapt responses reasoning context capabilities`。

### Effective tools 计量

- token 计量统一使用顶层 tools 与 `input.additional_tools` 的有效集合。
- 每个工具定义写入一次 CombineText，并设置 ToolsCount。
- 普通 Responses 和 Compact 在请求边界显式拒绝 malformed `additional_tools`，不在计量阶段吞错。

提交：`c3b9c31d9 fix: count responses lite tools`。

### Lark grammar 与定向 Chat bridge

- Responses 到 Chat Completions 的 custom tool bridge 接受 Codex 当前唯一已知的 `grammar/lark/definition` 结构。
- grammar definition 作为工具约束进入 Chat function 描述，参数固定为严格的 `{input:string}`；回程恢复为 `custom_tool_call`，不伪装成普通 function call。
- 未知 grammar syntax、缺失 definition 和无法表达的结构继续显式失败。
- `tool_bridge_types` 可把条件桥接限制为 `function`、`custom`、`namespace`、`tool_search` 中的指定类型；未设置时保持旧配置的全类型行为。

提交：`3aef56ec2 fix: bridge codex lark grammar tools to chat`。

### 工具能力路由

- 渠道 capability snapshot 新增 `supports_function_tools`，与 custom、namespace、tool_search 能力分别管理。
- 请求同时检查顶层 tools、`input.additional_tools`、工具历史 item 和 `tool_choice`，避免只看顶层 tools 而漏判 Responses Lite。
- 显式不支持的工具请求返回可重试 503，自动路由先耗尽同优先级其他渠道，再进入下一优先级；固定渠道请求保持显式失败。
- 普通 Responses 和 Compact 不受工具 capability 影响，仍优先走原生 Responses。

提交：`50787348d fix: route responses tools by channel capability`。

### 同优先级重试

- 重试会排除本请求已经尝试过的渠道，在当前优先级仍有候选时不会提前跌到低优先级。
- 该行为同时覆盖上游 5xx 和 capability 产生的显式可重试错误。

提交：`803211365 fix: exhaust same-priority channels before retry fallback`。

### 多优先级排除后的重试选择

- 真实全量池暴露出进一步缺口：当多个较高优先级渠道已因 capability 跳过或上游错误被排除，重试仍按全量优先级索引，可能在仍有未尝试渠道时返回 `get_channel_failed`。
- 选路器新增显式的“先应用排除集合，再选择最高剩余优先级”模式，只用于 retry 的 untried-channel 分支；普通首选和按优先级下降语义不变。
- 回归覆盖 default/auto group、内存缓存/数据库四种组合，并保留原同优先级和 request-body-limit 测试。

提交：`19d9f7c42 fix: select highest remaining channel on retry`。

## 验证

| 命令 | 退出码 | 结果 |
| --- | ---: | --- |
| `go test ./controller ./model ./relay/common ./relay/helper ./service -count=1` | 0 | 主后端门禁通过 |
| `go test ./dto ./relay/channel/openai ./relay ./service/openaicompat -count=1` | 0 | DTO、Responses、OpenAI 适配和 Chat 桥接通过 |
| `bunx eslint src/features/channels/types.ts src/features/usage-logs/types.ts src/features/usage-logs/components/dialogs/details-dialog.tsx` | 0 | 本轮前端文件通过 |
| `bun run build` | 0 | default 前端构建通过，Safari 兼容重写和检查通过 |
| `bun run i18n:sync` | 0 | 六语言缺失数为 0；只保留本轮新增的两个翻译键 |
| `git diff --check` | 0 | 无空白错误 |
| `go test -count=1 -timeout 120s ./controller ./model ./relay/common ./relay/helper ./service ./setting/model_setting` | 0 | 2026-07-16 重跑，7 个相关包通过 |
| `node --check scripts/codex-responses-lite-smoke.mjs` | 0 | smoke 脚本语法通过 |

全局 `bun run typecheck` 仍被未修改的 `src/components/ui/markdown.tsx` 重复 React 类型问题阻断。全局 `bun run lint` 仍被未修改的 `src/components/long-text.tsx` 和 `src/features/system-settings/models/tool-price-settings.tsx` effect 内同步 setState 阻断。本轮修改文件的定向 ESLint 与生产构建均通过，未将既有问题表述为本轮通过。

## 2026-08-04 原子修复复核

本轮补齐普通 raw pass-through 路径的 Responses item ID 归一化缺口。此前该路径直接复用原始请求体，会绕过 adaptor 中已有的 `NormalizeResponsesInputItemIDs`；现在普通请求与 opaque encrypted replay 都只在 `input` item 层执行 ID 归一化，保留顶层未知字段、合法 ID、`call_id` 和 `encrypted_content`，并写入移除计数与系统日志。opaque replay 仍不解析或改写加密内容本身，也继续跳过 Codex 重写和 synthetic compact 分支。

| 命令 | 退出码 | 结果 |
| --- | ---: | --- |
| `go test -count=1 -run '^TestBuildOpenAIResponsesRequestBodyNormalizesItemIDsOnRawPassThrough$' ./relay`（修复前） | 1 | 失败测试确认 raw pass-through 保留非法 ID |
| `go test -count=1 -run '^TestBuildOpenAIResponsesRequestBodyNormalizesItemIDsOnRawPassThrough$' ./relay`（修复后） | 0 | 非法 ID 移除、合法 ID/`call_id`/未知顶层字段保留 |
| `go test -count=1 -timeout 120s ./relay/common ./relay/channel/codex ./relay/channel/openai ./relay` | 0 | item ID、Responses、Codex/OpenAI adaptor 与 handler 回归通过 |
| `go test -count=1 -timeout 120s ./controller ./model ./relay/helper ./service` | 0 | AGENTS 指定 L1 回归通过 |
| `git diff --check` | 0 | 无空白错误 |

## 2026-08-06 opaque replay ID normalization

- 官方 `openai/codex` `rust-v0.145.0` 的 `ResponseItem::id_prefix()` 仍只声明 `at`、`msg`、`amsg`、`rs`、`lsh`、`fc`、`tsc`、`fco`、`ctc`、`ctco`、`tso`、`ws`、`ig`、`cmp`；本仓库现有 18 类映射（包含历史别名）未凭未知 token fixture 臆造新前缀。
- Codex 与 OpenAI adaptor 现在先执行 `NormalizeResponsesInputItemIDs`，再对 opaque replay 原样返回其余请求字段，避免非法客户端生成 ID 绕过过滤，同时不触发 Codex 字段重写或 synthetic compact。
- 真实 opaque raw-body pass-through 现在只重写 `input`，保留 `encrypted_content`、未知 item 字段和顶层未知字段；模型映射、reasoning capability 处理仍沿用原路径。
- Compact ingress 到 Responses request 的转换现在深拷贝所有顶层 `json.RawMessage`、reasoning/stream_options 的未知字段和显式 `null` raw 状态，避免调用方修改 compact DTO 后污染已转换请求。
- Opaque raw replay 不再绕过 `stream_options.include_usage` 能力剥离；显式不支持的渠道只移除该字段，保留 `include_obfuscation`、未知 stream option 与加密 input。

| `go test -count=1 -timeout 120s ./relay/common ./relay/channel/codex ./relay/channel/openai ./relay` | 0 | opaque replay、item ID、Responses handler 与 adaptor 回归通过 |
| `gh api 'repos/openai/codex/contents/codex-rs/protocol/src/models.rs?ref=rust-v0.145.0'` | 0 | 官方 0.145 item ID 前缀矩阵核验完成 |
| `go test -count=1 -timeout 120s ./dto` | 0 | DTO、compact nested round-trip 与深拷贝回归通过 |
| `go test -count=1 -timeout 120s ./relay -run 'TestBuildOpenAIResponsesRequestBodyApplies(StreamOptionsCapabilityToOpaqueReplay\|ReasoningContextPolicyToOpaqueReplay)'` | 0 | opaque reasoning/context 与 stream options capability 回归通过 |

本轮未执行正式环境重建、升级或渠道配置变更；隔离 Docker 镜像和完整 L2 协议 smoke 仍需在最终 HEAD 汇总门禁阶段重建验证。

## 隔离开发环境验证

### 版本与边界

- 隔离候选镜像的 build-info 与候选源码一致，`/api/status` 成功。具体 image digest 和 revision 保存在私有验证记录。
- 测试只使用隔离渠道夹具和受控测试凭证；凭证不输出到终端或文件，不复制生产数据库或渠道配置到公开工件。
- 启用渠道集合和临时可用性属于运行环境数据，保存在私有验证记录，不作为公开发布门禁的输入。

### 固定渠道覆盖

| 场景 | 结果 |
| --- | --- |
| 原生 Responses basic、function、compact 和 continuation | 完成，保持原生路径和合法 `call_id`。 |
| grammar、namespace 和 tool-search 的定向 Chat bridge | 完成，保留 `custom_tool_call`、namespace 和两阶段工具语义。 |
| 明确不支持的工具能力 | 返回可分类错误并记录能力动作，不伪造成功。 |
| 上游确定性 403、瞬时 502 或能力缺失 | 归类为上游或能力状态，不作为协议通过证据。 |

最小有效策略是：function 保持原生；custom、namespace、tool_search 仅在对应请求出现时转 Chat bridge；普通 Responses 和 Compact 不转换。

### 自动路由与优先级复现

smoke 脚本的 `--channel auto` 覆盖 basic、grammar、function、tool_search 与 compact continuation。能力不兼容候选会被显式跳过，健康同优先级候选优先完成请求；只有同优先级穷尽后才进入下一优先级。相关的 `request_conversion`、`channel_capability_snapshot`、`request_path` 与 `upstream_request_path` 均与预期一致。

测试结束后，临时渠道、ability、日志和路由规则均已清理，无残留。真实运行渠道的标识、优先级、流量计数和错误样本保存在私有验证记录中。

## 生产前基线

生产前只在私有运维审计中保留时间窗口、渠道成功/失败计数、上游状态码和流终止情况。公开报告只保留其用途：这些数据用于与升级后的同口径采样比较，不能替代升级后验证。

## 2026-08-06 上游增量复核与本轮修复

本轮通过 `gh` 复核了 QuantumNous/new-api `upstream/main`（`0ab02020603d22e5613bc4cf46bfab06f8567769`，2026-08-01）以及 Wei-Shaw/sub2api 当前主线和相关 PR。上游实现与本仓库 relay 架构已明显分叉，因此只吸收经过本地调用链验证的语义，不覆盖整文件。

### 已吸收的语义

- sub2api PR #5089 已合并（merge commit `d4cada3b6bd1098b11593102de0d1aab535aa016`）：Chat Completions 流在 HTTP 200 中携带 `rate_limit_exceeded` 时，首个语义输出前必须按 429 处理，才可进入渠道重试。本轮在 `responses_via_chat.go` 补齐该行为，并保留已提交输出后的 HTTP 状态不可逆约束。
- sub2api PR #4595 已合并（merge commit `1b4029b05061706a1a32acff5ff6dfcbb590173e`）：HTTP 桥接在上游传输中断时不能伪造完成结果。本轮复用本地 Responses 断流分类，在 Chat 兼容流缺少 `[DONE]` 时返回 `upstream_transport_interrupted`/502。

### 已核对但未直接移植的变更

- sub2api PR #2800 的账号-模型维度 404 冷却，以及开放 PR #5019 的 400/404 `model_not_found` 失败后语义保持，依赖 Sub2API 账号池和模型限流持久化。本仓库目前只有渠道-模型 Redis 健康状态，不能直接复制其账号状态机；本轮不把模型错误静默改成渠道禁用。
- sub2api PR #5181 仍开放，内容是向官方 Codex 上游发送 zstd 压缩请求。它不等同于本项目入口的请求体解压边界；本仓库继续在认证前使用有界解压、认证后对本地 Codex compact 使用更大但固定的上限，不在未验证上游支持时增加出站压缩。
- QuantumNous/new-api 的 #6446、#6523、#6580、#6082 仍为开放 PR，分别涉及 SSE 429、流错误/渠道切换、自适应排除重试与限流冷却、缺少 `Content-Type` 的 SSE 探测。当前代码已有相应的 Responses 原生错误、断流和能力路由基础；未将其未合并的整套控制器改动混入本轮。

### 本轮新增回归

| 命令 | 退出码 | 结果 |
| --- | ---: | --- |
| `go test -count=1 ./relay/channel/openai -run 'TestOaiChatToResponses(StreamHandlerReturnsRateLimitBeforeOutput\\|StreamHandlerKeepsCommittedRateLimitTerminal\\|HandlerMapsRateLimitErrorBody\\|StreamHandlerRejectsTruncatedUpstreamStream)'` | 0 | Chat bridge 错误帧、状态映射和断流语义通过 |
| `go test -count=1 ./relay/channel/openai` | 0 | OpenAI Responses/Chat bridge 全包通过 |
| `go test -timeout=60s ./controller ./model ./relay/common ./relay/helper ./service` | 0 | AGENTS 指定 L1 通过 |

残余风险仍是正式环境和真实上游的时间窗口验证：本轮只修改开发代码和审计文档，未重建或升级正式服务。隔离 3001 的最终 HEAD 重建、build-info 对齐、真实协议 smoke 和多次观察仍是发布门禁。

## 2026-08-07 最终隔离真实渠道 gate

### 运行边界

- 通过只读生产 PostgreSQL repeatable-read snapshot 读取当前启用且匹配 `gpt-5.6-sol` 的渠道，候选集合为 `114,115,116,117,119,120,130,141,160,168,180,205,207,208,223`。
- gate 命令为 `CODEX_REAL_GATE_APPLY_CODEX_PASSTHROUGH=1 bash scripts/codex-real-channel-compaction-gate.sh`；该开关只在隔离数据库临时副本中写入 Codex 动态 `pass_headers` 测试策略，生产数据库保持只读。
- 每个渠道执行 Codex CLI 0.146 coding smoke、1 KiB 基础 Compact 和 33 MiB 工具 schema Compact。压缩成功判定为 HTTP 200 且终态 `response.completed`；超大请求同时记录入口收敛后的字节数。
- 两次 gate 的 cleanup 均为 `passed`，并重新创建隔离 Redis、验证隔离应用网络和 `/api/status`。隔离 PostgreSQL 中 `codex_real_gate_%` 临时表、测试用户、测试 token 和测试日志计数均为 0。

### 结果分类

| 渠道 | CLI coding smoke | 基础/超大 Compact | 结论 |
| ---: | --- | --- | --- |
| 114, 115, 116, 130, 168 | 通过 | 通过，HTTP 200 / `response.completed` | 本地协议和压缩路径通过 |
| 160 | 通过 | 通过，HTTP 200 / `response.completed` | 本地协议和压缩路径通过 |
| 205 | 通过 | 通过，HTTP 200 / `response.completed` | 本地协议和压缩路径通过；33 MiB 输入收敛为 1290 bytes |
| 119, 120 | `server_is_overloaded` | 通过，HTTP 200 / `response.completed` | 上游过载，非网关协议失败 |
| 141, 180 | `upstream_transport_interrupted` | 通过，HTTP 200 / `response.completed` | 上游传输中断，非压缩控制面失败 |
| 117 | 503 `channel:model_mapped_error`，`unsupported_namespace_tools` | 通过，HTTP 200 / `response.completed` | 渠道能力明确不支持 namespace，路由应跳过或返回可解释能力错误 |
| 208, 223 | 503 `channel:model_mapped_error`，`unsupported_encrypted_reasoning_replay` | 通过，HTTP 200 / `response.completed` | 渠道能力明确不支持 encrypted reasoning replay，不能伪造 CLI 成功 |
| 207 | 403 `insufficient_user_quota` | 403，未进入压缩成功态 | 上游额度失败，不能归因于网关 413/502 |

### 动态请求头缺口

未注入测试 `pass_headers` 的首轮 gate 对所有候选在上游请求前返回 `channel:header_override_invalid`。更新前正式渠道只配置静态 `codex-cli@latest` 身份和 `prune_objects`，缺少 `Session_id`、`Session-Id`、`Thread-Id`、`X-Codex-*`、`X-Client-Request-Id` 等真实 Codex 运行时头的透传。隔离 gate 注入动态 `pass_headers` 后，160 以及其余可用渠道的协议请求可正常到达上游，证明这是正式配置门禁缺口，不是通过固定值伪造请求头即可解决的问题。

因此在完成正式镜像发布前，119/120、141/180、207 的结果需要按上游过载、传输中断和额度分别告警；117/208/223 需要按能力快照参与自动路由，不能通过静默剔除或全局关闭 Responses Lite 规避。动态 `pass_headers` 已在当前仍启用的正式渠道配置并通过隔离复验，剩余阻断是 dirty worktree 无法获得正式镜像晋级资格。

### 本轮证据

| 命令/证据 | 退出码 | 结果 |
| --- | ---: | --- |
| `CODEX_REAL_GATE_APPLY_CODEX_PASSTHROUGH=1 bash scripts/codex-real-channel-compaction-gate.sh`（首轮候选） | 0（cleanup） | 114/115/116/119/120/130/141/168/180/205/207/208/223 完成分类，压缩场景均按上表记录 |
| 同一 gate 排除首轮已测渠道，仅测试 `117,160` | 0（cleanup） | 160 全部通过；117 仅 namespace 能力不支持，压缩全部通过 |
| `bash -n scripts/codex-real-channel-compaction-gate.sh` | 0 | gate 脚本语法通过 |
| `node --test scripts/codex-real-channel-compaction-gate.test.mjs` | 0 | gate 合同测试通过 |
| `scripts/go-test-all.sh -count=1 -timeout=120s` | 0 | 排除前端生成目录和嵌套 worktree 后枚举 88 个 Go 包，全部通过 |
| `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` | 0 | 当前隔离容器为 `5d12e728b8ff610136882973cac21fd382e7972d-dirty` |
| `curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status` | 0 | 隔离应用 HTTP 200 |

以上验证未修改、未重建、未升级正式服务；正式发布门禁仍要求在完成动态请求头配置后重新执行真实 gate，并保留正式镜像锚点、备份和升级后 413/429/502/工具调用观察证据。

## 2026-08-07 正式配置门禁与发布阻断

### 正式渠道配置

- 在生产 PostgreSQL 原子事务中，仅对当时仍启用且匹配 `gpt-5.6-sol` 的 13 个渠道 `114,115,116,117,119,120,130,141,160,168,180,205,223` 追加无条件 `pass_headers` 和 `X-Client-Request-Id -> Session_id` `copy_header` 操作。
- 原有 `prune_objects` 操作全部保留；180 原本没有参数覆盖，因此只新增上述两项。207、208 在配置前已为 `status=2, auto_ban=1`，本轮没有重新启用、没有写入参数覆盖。
- 事务校验结果为 `updated_channels=13`、`pass_headers_ops=13`、`copy_header_ops=13`；失败的首次 15 渠道校验已回滚，所有目标行在重试前的参数覆盖哈希未改变。

### 使用正式配置导入隔离 gate

关闭隔离测试覆盖开关后，gate 从生产只读 snapshot 导入上述 13 个渠道，结果不再出现 `channel:header_override_invalid`：

| 渠道 | CLI | Compact 结果 | 归类 |
| ---: | --- | --- | --- |
| 114, 115, 168 | 通过 | 基础和 33 MiB 超大请求均 HTTP 200 / `response.completed` | 动态头和压缩路径通过 |
| 141, 205 | 通过 | 分别出现上游过载/传输中断的 502，其他压缩请求通过 | 上游瞬态失败，非网关头校验 |
| 116, 119, 120, 130, 180 | 上游 `upstream_transport_interrupted` 或 `server_is_overloaded` | 压缩请求大多通过；120 基础请求一次 `server_is_overloaded` 502 | 上游状态需按错误码观察 |
| 160 | 上游 `do_request_failed` 500 | 基础和超大请求均 HTTP 200 / `response.completed` | 上游 CLI 请求失败，压缩控制面通过 |
| 117 | 503 `unsupported_namespace_tools` | 基础和超大请求均通过 | 渠道能力不支持 namespace，路由需解释/跳过 |
| 223 | 503 `unsupported_encrypted_reasoning_replay` | 基础和超大请求均通过 | 渠道能力不支持 encrypted reasoning replay |

隔离 cleanup 仍为 `passed`，Redis 重新创建、应用网络和 `/api/status` 均验证成功。上述 502/500/能力错误均有结构化观测，不作为压缩修复失败或静默成功。

### 正式镜像发布状态

- 正式服务当前仍运行 `new-api-local:prod-codex-lite-4527d7666`，build-info 为提交 `4527d7666a37f9151a57f08ca0bc5e8e08b8fb46`，容器 healthy、重启次数为 0，`127.0.0.1:13000/api/healthz` HTTP 200。
- 最新隔离候选 `new-api-local:dev` 已从当前工作树重建，build-info 为 `5d12e728b8ff610136882973cac21fd382e7972d-dirty`，所有相关 Go 测试、前端 Docker 构建和真实渠道 gate 已完成。
- 由于工作树仍包含既有大量未提交改动，生产发布规程的 dirty-worktree guard 不允许把该镜像标记或晋级为正式候选。本轮没有重建/替换正式应用容器，也没有修改 PostgreSQL 之外的生产依赖。

因此当前状态是“正式动态请求头配置已生效，代码镜像尚未升级”。在工作树获得可审计的干净提交、正式镜像 digest 和独立备份前，不能宣称 Responses Lite 代码修复已在生产运行。

### 回滚边界

- 应用回滚无需动作：本轮没有替换正式容器，旧镜像 `prod-codex-lite-4527d7666` 仍在运行。
- 渠道配置回滚只删除本轮追加的无条件 `pass_headers` 和精确的 `copy_header(X-Client-Request-Id -> Session_id)` 两项，保留每个渠道原有的其他 `operations`；必须在事务中校验目标渠道和操作计数后执行，不能整体覆盖 `param_override`。
- 207、208 的 `status=2, auto_ban=1` 不属于本轮配置变更，回滚时不得自动恢复；重新启用需另行完成上游可用性和能力 gate。

## 发布边界

本文已记录代码、静态测试和隔离 Docker 真实上游验证，尚未升级正式环境。进入正式发布前仍必须完成：

- 从包含本文档和 smoke 脚本的最终 HEAD 重建 `new-api-local:dev` 和 3001 隔离栈。
- 比对 HEAD、origin/main 与容器 build-info。
- 对最终镜像重跑最小 3001 回归，确认文档提交没有改变构建可追溯性。
- 隔离验证通过后制作正式镜像、执行新备份、只重建应用服务，并观察错误率、429、502、工具调用和压缩日志。
