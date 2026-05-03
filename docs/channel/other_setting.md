# 渠道 Other Settings 说明

`Other Settings` 对应渠道 `settings` JSON 字段，实际结构见后端 `dto.ChannelOtherSettings`。

该字段适合承载：

- 某类渠道的附加开关
- 上游兼容性参数
- 请求头运行时策略
- 模型更新检查状态

不建议把无结构的临时配置长期堆进这里；新增字段前应先确认三库兼容、前后端序列化行为和运行时读取链路。

## 组模型路由辅助匹配

`GROUP_MODEL_ROUTE_HELPER_ENABLED` 是全局环境变量，不属于渠道 `settings` 字段，但会影响渠道能力匹配。

默认值为 `true`。启用后，渠道选路会先尝试请求里的原始模型名，再尝试 `FormatMatchingModelName()` 生成的归一化模型名。例如请求 `gpt-4o-gizmo-special` 时，可以继续命中配置为 `gpt-4o-gizmo-*` 的渠道能力。

如果显式设置为 `false`，渠道选路只按精确模型名匹配。该模式适合需要严格隔离模型名称、避免通配归一化扩大匹配范围的部署。

## 常用基础字段

```json
{
  "force_format": true,
  "thinking_to_content": true,
  "proxy": "socks5://127.0.0.1:1080",
  "pass_through_body_enabled": false,
  "system_prompt": "You are a helpful assistant.",
  "system_prompt_override": true
}
```

字段说明：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `force_format` | `bool` | 强制格式化返回为 OpenAI 风格响应 |
| `thinking_to_content` | `bool` | 将 `reasoning_content` 拼接为 `<think>` 内容返回 |
| `proxy` | `string` | 上游请求代理地址 |
| `pass_through_body_enabled` | `bool` | 允许更原始地透传请求体，是否可用取决于具体渠道 |
| `system_prompt` | `string` | 渠道级系统提示词 |
| `system_prompt_override` | `bool` | 是否强制覆盖请求中的系统提示词 |

## 请求头策略字段

这部分是当前仓库里最容易混淆的区域，当前推荐口径是：

1. `header_override`
   - 独立渠道字段，不在 `settings` 里。
   - 仅作为旧版静态请求头 JSON 兼容字段。
   - 新配置建议显式导入或保存为 `Header Profile` 后再选择。

2. `header_profile_strategy`
   - 位于 `settings` 中。
   - 控制渠道选择哪些 `Header Profile` 资产以及选择模式。
   - `User-Agent` 只是 `Header Profile.headers` 中的普通字段。
   - 常规渠道应优先使用该字段管理完整请求头。

3. `ua_strategy`
   - 位于 `settings` 中。
   - 历史兼容字段，保留后端读取和校验。
   - 新的渠道表单不再提供独立 UA 池入口。
   - 如需多组 User-Agent 轮询或随机，使用多个只包含或包含不同 `User-Agent` 的 `Header Profile`。

示例：

```json
{
  "header_profile_strategy": {
    "enabled": true,
    "mode": "round_robin",
    "selected_profile_ids": [
      "chrome-macos",
      "codex-cli"
    ]
  }
}
```

字段说明：

| 字段 | 类型 | 允许值 | 说明 |
| --- | --- | --- | --- |
| `header_profile_strategy.enabled` | `bool` | `true` / `false` | 是否启用 Header Profile 选择 |
| `header_profile_strategy.mode` | `string` | `fixed` / `round_robin` / `random` | Profile 选择模式 |
| `header_profile_strategy.selected_profile_ids` | `string[]` | 非空字符串数组 | 选中的 Profile ID 列表 |
| `header_policy_mode` | `string` | `system_default` / `prefer_channel` / `prefer_tag` / `merge` | 历史兼容：渠道级与标签级旧请求头策略的优先级模式 |
| `override_header_user_agent` | `bool` | `true` / `false` | 历史兼容：是否用 `ua_strategy` 结果覆盖静态 `header_override.User-Agent` |
| `ua_strategy.enabled` | `bool` | `true` / `false` | 历史兼容：是否启用 UA 运行时策略 |
| `ua_strategy.mode` | `string` | `round_robin` / `random` | 历史兼容：UA 池选择模式 |
| `ua_strategy.user_agents` | `string[]` | 非空字符串数组 | 历史兼容：UA 候选池 |

保存时的关键校验：

- `header_profile_strategy.mode=fixed` 时必须且只能选择 1 个 Profile。
- `header_profile_strategy.mode=round_robin/random` 时至少要选择 1 个 Profile。
- `header_profile_strategy` 中标记为 `passthrough_required` 的 AI Coding CLI 预置 Profile 依赖真实客户端动态头；WebUI 选择这类模板时会自动在 `param_override.operations` 中合并对应 `pass_headers` 规则，直接写配置时也必须同时提供完整 `pass_headers`。
- 非法 `header_policy_mode` 会在保存阶段直接拒绝。
- 非法 `ua_strategy.mode`、空 UA 池、启用策略但无合法 UA，都会在保存阶段直接拒绝。

当前内置 `Header Profile`：

| Profile ID | 名称 | 固定请求头快照 | 是否自动补 `pass_headers` |
| --- | --- | --- | --- |
| `chrome-macos` | Chrome macOS | 浏览器常见导航请求头 | 否 |
| `codex-cli` | Codex CLI | `User-Agent: codex-tui/0.128.0 ...`、`Originator: codex-tui` | 是 |
| `claude-code` | Claude Code | `User-Agent: claude-cli/2.1.126 (external, sdk-cli)` | 是 |
| `gemini-cli` | Gemini CLI | `User-Agent: GeminiCLI/0.40.1/gemini-3.1-pro-preview ...` | 是 |
| `qwen-code` | Qwen Code | `User-Agent: QwenCode/0.15.6 (darwin; x64)` | 是 |
| `droid` | Droid CLI | `User-Agent: factory-cli/0.115.0` | 是 |
| `postman-runtime` | Postman Runtime | Postman Runtime 调试请求头 | 否 |

`codex-cli` 的固定快照代表交互式 TUI 场景。`codex exec` 的 non-interactive 请求会使用 `codex_exec`，不能作为 `Codex CLI` 内置 Profile 模板。

## 请求头模板与透传规则

模板内容本身不保存在 `settings`，但会影响 `header_override` 的合法性。

当前合法规则：

- 必须是 JSON 对象
- key 不能为空
- value 只支持字符串、数字、布尔值
- key 支持三类写法：
  - 标准请求头名，例如 `User-Agent`
  - `*`
  - `re:<pattern>` 或 `regex:<pattern>`

示例：

```json
{
  "User-Agent": "Mozilla/5.0",
  "X-App": "new-api",
  "re:^x-openai-": "passthrough"
}
```

注意：

- 非法正则现在会在保存时直接失败，不再延迟到请求真正转发时才报错。
- 规范化后重复的 key 也会被拒绝，例如大小写不同但归一化后相同的请求头名。

### `pass_headers` 与 CLI 客户端

`pass_headers` 不生成固定请求头，只从当前客户端请求里读取同名请求头并写入上游请求。它用于保留 Codex CLI、Claude Code、Gemini CLI 等真实客户端携带的动态元数据。

Codex CLI 透传模板当前包含：

```json
[
  "Originator",
  "Session_id",
  "User-Agent",
  "X-Codex-Beta-Features",
  "X-Codex-Turn-Metadata",
  "X-Codex-Window-Id",
  "X-Client-Request-Id"
]
```

Claude CLI 透传模板当前包含：

```json
[
  "X-Claude-Code-Session-Id",
  "X-Stainless-Arch",
  "X-Stainless-Lang",
  "X-Stainless-Os",
  "X-Stainless-Package-Version",
  "X-Stainless-Retry-Count",
  "X-Stainless-Runtime",
  "X-Stainless-Runtime-Version",
  "X-Stainless-Timeout",
  "User-Agent",
  "X-App",
  "Anthropic-Beta",
  "Anthropic-Dangerous-Direct-Browser-Access",
  "Anthropic-Version"
]
```

Gemini CLI 透传模板当前包含：

```json
[
  "User-Agent",
  "X-Goog-Api-Client"
]
```

Qwen Code 透传模板当前包含：

```json
[
  "User-Agent",
  "X-Stainless-Arch",
  "X-Stainless-Lang",
  "X-Stainless-Os",
  "X-Stainless-Package-Version",
  "X-Stainless-Retry-Count",
  "X-Stainless-Runtime",
  "X-Stainless-Runtime-Version"
]
```

Droid CLI 透传模板当前包含：

```json
[
  "User-Agent",
  "X-Stainless-Arch",
  "X-Stainless-Lang",
  "X-Stainless-Os",
  "X-Stainless-Package-Version",
  "X-Stainless-Retry-Count",
  "X-Stainless-Runtime",
  "X-Stainless-Runtime-Version"
]
```

OpenCode 当前不作为内置 `Header Profile` 提供。本机 live capture 中，模型 upstream 请求头来自 AI SDK provider 运行时，而不是 OpenCode 自身固定 UA；不要把 OpenCode CLI 默认参数当作上游请求头模板。

如果客户端原始请求里没有这些头，`pass_headers` 不会伪造值；这类缺失应通过真实客户端调用链路修复，而不是用固定 UA 模板替代。

## 模型更新检查相关字段

这组字段用于“上游模型自动检查/自动同步”能力：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `upstream_model_update_check_enabled` | `bool` | 是否启用上游模型更新检查 |
| `upstream_model_update_auto_sync_enabled` | `bool` | 是否自动同步上游模型更新 |
| `upstream_model_update_last_check_time` | `int64` | 上次检查时间戳 |
| `upstream_model_update_last_detected_models` | `string[]` | 最近检测到可新增的模型 |
| `upstream_model_update_last_removed_models` | `string[]` | 最近检测到可移除的模型 |
| `upstream_model_update_ignored_models` | `string[]` | 忽略列表 |

## 渠道专属字段

下列字段只在对应渠道类型下有意义：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `azure_responses_version` | `string` | Azure Responses API 版本 |
| `vertex_key_type` | `string` | `json` 或 `api_key` |
| `aws_key_type` | `string` | `ak_sk` 或 `api_key` |
| `openrouter_enterprise` | `bool` | OpenRouter Enterprise 标志 |
| `claude_beta_query` | `bool` | Claude 渠道是否追加 `?beta=true` |
| `allow_service_tier` | `bool` | 是否允许 `service_tier` 透传 |
| `allow_inference_geo` | `bool` | 是否允许 `inference_geo` 透传 |
| `allow_speed` | `bool` | 是否允许 `speed` 透传 |
| `allow_safety_identifier` | `bool` | 是否允许 `safety_identifier` 透传 |
| `disable_store` | `bool` | 是否禁用 `store` 透传 |
| `allow_include_obfuscation` | `bool` | 是否允许 `stream_options.include_obfuscation` 透传 |

## 使用建议

- 先优先使用已有表单入口，不要手工拼接未知 JSON。
- 涉及请求头能力时，先区分你要改的是：
  - `header_profile_strategy`
  - 旧版静态 `header_override`
  - 标签级请求头策略
  - 历史兼容 `ua_strategy`
- 任何需要进入真实转发链路的配置，都必须在隔离环境做真实请求验证，不能只看表单保存成功。
- 渠道模型测试窗口的运行配置说明见 [渠道模型测试运行配置说明](./model_test_runtime_config.md)。
