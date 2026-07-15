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

## 隔离开发环境验证

### 版本与边界

- `new-api-local:dev` 镜像提交：`50787348d1075ec3ba0b1413ae00be4263c622a7`。
- 3001 隔离容器 build-info 与该提交一致，`/api/status` 成功。
- 使用正式渠道配置的开发库临时副本完成测试，凭证只通过 PostgreSQL 管道复制，未输出到终端或文件。
- 2026-07-16 正式库实时启用且包含 `gpt-5.6-sol` 的渠道为 1、150、201、206、117；按已知状态排除暂不可用的 150。204 和 155 已不在当前启用范围，本轮不再作为发布门禁。

### 固定渠道矩阵

| 开发临时渠道 | 来源 | 场景 | 结果 |
| ---: | ---: | --- | --- |
| 99201 | 1 | basic | 200 completed，原生 `/v1/responses` |
| 99201 | 1 | compact + continue | 200，生成 compaction 并成功续接 |
| 99201 | 1 | grammar | 200，Chat bridge 回程为 `custom_tool_call apply_patch` |
| 99201 | 1 | namespace | 200，回程保留 namespace |
| 99201 | 1 | function/tool_search | capability 显式 503；日志分别记录 function/tool_search unsupported |
| 99206 | 206 | basic/function | 200，保持原生 Responses；function 返回 `extra_lookup` |
| 99206 | 206 | grammar 原生基线 | 200，但错误返回普通 `function_call`，证明 custom 语义丢失 |
| 99206 | 206 | tool_search 原生基线 | 200，但仅返回 message，没有 `tool_search_call` |
| 99206 | 206 | grammar 定向 bridge | 200，恢复 `custom_tool_call apply_patch` |
| 99206 | 206 | namespace 定向 bridge | 200，保留 namespace |
| 99206 | 206 | tool_search 定向 bridge | 连续 3 次通过发现、加载、调用两阶段流程，最终调用 `deferred_lookup` |
| 99206 | 206 | compact + continue | 200，生成 compaction 并成功续接 |
| 99206 | 206 | controls/continuation/ids/web-search | 全部 200；工具续接 call_id 保留，ID 场景完成 |
| 99211 | 201 | basic | 上游 403 `API Key 所属分组已停用`，属于渠道运行状态，不作为协议通过证据 |
| 99217 | 117 | basic | 200；后续 function 两次被上游 Cloudflare 502 打断，属于当前上游抖动 |

206 的最小有效策略为：function 原生；custom、namespace、tool_search 仅在对应请求出现时转 Chat bridge；普通 Responses 和 Compact 不转换。

### 自动路由矩阵

smoke 脚本新增 `--channel auto`，不附加固定渠道后缀。开发自动池只保留当前可用的 1、206、117 副本，201 因确定性 403 不参与自动路由验收。

| 场景 | 结果 | 实际路径 |
| --- | --- | --- |
| basic | 200 completed | 99201 原生 `/v1/responses` |
| grammar | 200 `custom_tool_call apply_patch` | 99201 定向 `/v1/chat/completions` |
| function | 200 `function_call extra_lookup` | 99201 capability 跳过，99206 原生 `/v1/responses` |
| tool_search | 200，发现并调用 `deferred_lookup` | 99201 capability 跳过，99206 定向 Chat bridge，两阶段均成功 |
| compact + continue | 200，compaction 和续接均成功 | 99201 Responses/Compact 路径 |

使用日志的 `request_conversion`、`channel_capability_snapshot`、`request_path` 和 `upstream_request_path` 与上表一致。测试结束后删除 41 条临时日志、3 条 ability 和 4 个临时渠道，并验证临时渠道、ability、日志和规则计数均为 0。

### 正式优先级全量池复现

第二组临时池按正式 ability 顺序复现 1 -> 150 -> 201/206 -> 117：

- 150 的 function 原生调用成功，但 tool_search 原生返回 reasoning 和 message，没有 `tool_search_call`，证明会静默丢失工具搜索语义。
- 仅为 150 设置 `supports_tool_search=false` 后，请求能跳过 1 和 150；修复前在 201 的确定性 403 后提前返回 500，重试链为 `99301->99350->99311`，没有到达同优先级 206。
- 应用 `19d9f7c42` 后，同一 tool_search 的发现和加载两个请求均继续尝试剩余渠道。两次都观察到 201 返回 403、206 返回 Cloudflare 502，随后 117 成功；最终 200 并调用 `deferred_lookup`。
- 该结果证明 capability 跳过、同优先级剩余渠道、下一优先级和真实上游错误可在同一请求内正确组合。

正式最小能力配置因此包括：渠道 1 的 function/custom/namespace/tool_search 均为 false，并只桥接 custom/namespace；渠道 150 只声明 tool_search=false；渠道 206 声明 function=true、custom/namespace/tool_search=false，并只桥接 custom/namespace/tool_search；渠道 117 保留既有 Responses 到 Chat 规则。

## 生产前基线

2026-07-16 00:40 CST 前一小时，正式 `gpt-5.6-sol` 日志中渠道 1 为 1586 次成功、3 次错误；错误为 2 次 504 和 1 次上游流在 `response.completed` 前结束，429 为 0。此数据只作为升级前对照，不代表升级后结论。

## 发布边界

本文已记录代码、静态测试和隔离 Docker 真实上游验证，尚未升级正式环境。进入正式发布前仍必须完成：

- 从包含本文档和 smoke 脚本的最终 HEAD 重建 `new-api-local:dev` 和 3001 隔离栈。
- 比对 HEAD、origin/main 与容器 build-info。
- 对最终镜像重跑最小 3001 回归，确认文档提交没有改变构建可追溯性。
- 隔离验证通过后制作正式镜像、执行新备份、只重建应用服务，并观察错误率、429、502、工具调用和压缩日志。
