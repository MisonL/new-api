# Codex Responses Lite 兼容适配验证

## 范围

采样时间：2026-07-15 CST (+08:00)。

本轮处理 Codex 0.144.4 及后续版本在第三方 Responses API 链路上的四类兼容变化：

- Responses Lite 将工具定义放入 `input.additional_tools`，并发送 `reasoning.context=all_turns`。
- Codex 继续发送 `reasoning.encrypted_content` include，并在新版本中始终构造 reasoning 参数。
- Responses 历史 item ID 按 item 类型使用独立前缀。
- 未来新增的 reasoning、stream_options 和顶层请求字段不得在网关 JSON 往返或 Compact 转换中丢失。

本轮没有修改数据库 Schema、密钥、渠道凭证或正式环境配置。

## 上游证据

### openai/codex

- 最新稳定 release 为 `rust-v0.144.4`，发布时间 `2026-07-14T05:08:11Z`。
- PR #32206 已于 2026-07-10 合并，merge commit 为 `d2d00b6632dc991aa4471db0529773029cae5d68`。源码在所有 Responses 请求中构造 reasoning，并固定 include `reasoning.encrypted_content`。
- 同一源码明确仅在 `use_responses_lite=true` 时设置 `reasoning.context=all_turns`。`multi_agent_version` 不是该字段的直接生成条件。
- Responses Lite 把工具定义放入 `ResponseItem::AdditionalTools`，顶层 `tools` 置空。
- PR #32312 已于 2026-07-11 合并，merge commit 为 `c9d52de5ca52a6b4439e7c0f69b34f6331926bb4`。类型前缀包括 `at`、`msg`、`amsg`、`rs`、`lsh`、`fc`、`tsc`、`fco`、`ctc`、`ctco`、`tso`、`ws`、`ig`、`cmp`。

用户提供的 WSL 文档通过关闭 `use_responses_lite` 绕开第三方接口不支持 `reasoning.context` 的问题。该方法会同时关闭 Responses Lite 的工具承载方式，不是网关侧的完整修复；本仓库改为保留客户端能力，并按渠道显式适配。

### QuantumNous/new-api

采样主分支提交为 `a63364d156cf2a64f1c3d1ee4923d73d5f3222a1`。其 `Reasoning` 已包含 `mode` 和 `context`，但没有保留 reasoning、stream_options 和顶层请求的未来未知字段，也没有本轮完整 item ID 前缀、渠道三态能力与 effective tools 计量处理。

### Wei-Shaw/sub2api

采样主分支提交为 `accb918eca222e35f0648aeb58535868825af723`。

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

## 验证

| 命令 | 退出码 | 结果 |
| --- | ---: | --- |
| `go test ./controller ./model ./relay/common ./relay/helper ./service -count=1` | 0 | 主后端门禁通过 |
| `go test ./dto ./relay/channel/openai ./relay ./service/openaicompat -count=1` | 0 | DTO、Responses、OpenAI 适配和 Chat 桥接通过 |
| `bunx eslint src/features/channels/types.ts src/features/usage-logs/types.ts src/features/usage-logs/components/dialogs/details-dialog.tsx` | 0 | 本轮前端文件通过 |
| `bun run build` | 0 | default 前端构建通过，Safari 兼容重写和检查通过 |
| `bun run i18n:sync` | 0 | 六语言缺失数为 0；只保留本轮新增的两个翻译键 |
| `git diff --check` | 0 | 无空白错误 |

全局 `bun run typecheck` 仍被未修改的 `src/components/ui/markdown.tsx` 重复 React 类型问题阻断。全局 `bun run lint` 仍被未修改的 `src/components/long-text.tsx` 和 `src/features/system-settings/models/tool-price-settings.tsx` effect 内同步 setState 阻断。本轮修改文件的定向 ESLint 与生产构建均通过，未将既有问题表述为本轮通过。

## 发布边界

本文记录代码与静态测试状态，不代表隔离 Docker 或正式环境已升级。进入正式发布前仍必须完成：

- 使用当前 HEAD 重建 `new-api-local:dev` 和 3001 隔离栈。
- 比对 HEAD、origin/main 与容器 build-info。
- 在隔离环境验证普通 Responses、Responses Lite additional_tools、reasoning.context 三态、工具调用、多轮工具回传、Compact 和非法 item ID。
- 隔离验证通过后制作正式镜像、执行新备份、只重建应用服务，并观察错误率、429、502、工具调用和压缩日志。
