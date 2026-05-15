# 协议转换 WebUI 完整化设计方案

## 1. 文档目标

本文记录 `new-api` 协议转换兼容模块的 WebUI 优化设计，目标是把当前依赖 JSON 维护的能力转成管理员可直接操作的可视化配置。

采样时间：2026-05-15。

当前问题来自生产环境一次 Responses 请求失败：

- 请求路径：`POST /v1/responses`
- 模型：`gpt-5.5`
- request id：`202605151408467307002648268d9d6u0qRGL79`
- 实际链路：渠道 `142` 返回 `429 api key 5小时限额已用完`，fallback 到渠道 `117`
- 渠道 `117` 命中 `responses` 到 `chat_completions` 的协议转换规则
- 规则未配置 `options.enable_custom_tool_bridge: true`
- 本地转换层返回 `400 custom tool bridge is not enabled in chat compatibility mode`

这说明后端能力已经具备，但 WebUI 的运维入口不够直观，管理员需要理解和手写底层 JSON 才能正确启用能力。

## 2. 当前代码事实

后端配置结构位于：

- `setting/model_setting/global.go`
- `service/openaicompat/policy.go`
- `relay/responses_handler.go`
- `service/openaicompat/responses_to_chat_request.go`

核心配置项为：

```json
{
  "rules": [
    {
      "name": "responses-to-chat-codex-custom-tools",
      "enabled": true,
      "source_endpoint": "responses",
      "target_endpoint": "chat_completions",
      "all_channels": false,
      "channel_ids": [117],
      "channel_types": [],
      "model_patterns": ["^gpt-5(\\..+)?$"],
      "options": {
        "enable_custom_tool_bridge": true
      }
    }
  ]
}
```

实施前前端状态：

- `web/default` 在全局模型设置页提供 `Policy JSON` 文本框。
- `web/default` 有 `Fill custom tools example` 按钮，可以填入带 `enable_custom_tool_bridge` 的模板。
- `web/default` 没有独立的规则列表、规则表单或自定义工具桥接开关。
- `web/classic` 有协议转换可视化编辑器，但当前可视化序列化不会保留 `options.enable_custom_tool_bridge`。
- `web/classic` 只能通过原始 JSON 模式维护该字段，切回可视化模式存在丢失 `options` 的风险。

后端语义：

- 当 `rules` 非空时，后端按 `rules[]` 逐条匹配，顶层 `enabled` 不会作为总开关参与判断。
- 当 `rules` 为空时，后端才会把顶层字段转成 legacy 规则，此时顶层 `enabled` 才决定 legacy 规则是否可命中。
- `global.pass_through_request_enabled` 或渠道 `pass_through_body_enabled` 开启时，Chat 与 Responses 之间的协议转换会被跳过。
- 当前 `NormalizeChatCompletionsToResponsesPolicyJSON` 会把 JSON 解析到强类型结构再序列化，未知字段会被丢弃。

## 3. 设计原则

- 底层仍保存 `global.chat_completions_to_responses_policy`，不引入新的后端存储结构。
- WebUI 覆盖后端当前支持的全部字段。
- 默认不隐藏失败，不做静默降级。
- 可视化编辑不得丢失未知字段或高级字段；这需要前端和后端保存链路同时支持。
- 旧配置必须可读、可展示、可保存。
- JSON 模式保留为高级入口，但不再作为主要维护方式。
- 管理员完成常见配置时不需要理解底层 JSON。
- 不把当前后端没有的语义包装成 UI 能力。特别是顶层 `enabled` 不能在规则模式下展示为“全局总开关”，除非同步改造后端匹配逻辑。

## 4. 信息架构

入口保持在：

```text
系统设置 / 模型设置 / 全局模型设置 / 协议转换兼容
```

模块拆成三个区域。

### 4.1 顶部概览区

展示和操作：

- 规则模式状态：当前使用规则模式或 legacy 模式
- 已启用规则数
- 总规则数
- 无效规则数
- 包含高级字段的规则数
- 操作按钮：新增规则、导入 JSON、查看 JSON、格式化 JSON

提示信息：

- 协议转换会把请求转成另一种接口格式继续发送。
- 当前支持 `/v1/chat/completions` 和 `/v1/responses` 双向转换。
- 模型正则为空或渠道范围为空时，规则不会命中。
- 自定义工具桥接只适用于 `Responses` 到 `Chat Completions`。
- 全局透传或渠道透传开启时，协议转换不会执行。
- 顶层 `enabled` 只属于 legacy 配置，不是规则模式总开关。

### 4.2 规则列表区

每条规则以列表项或紧凑卡片展示，不使用嵌套卡片。

字段：

- 规则名称
- 启用状态
- 转换方向
- 渠道范围摘要
- 模型正则摘要
- 自定义工具桥接状态
- 高级字段标记
- 编辑、复制、删除

状态展示：

- 启用：规则参与匹配。
- 停用：规则保留但不参与匹配。
- 不会命中：缺少渠道范围或模型正则。
- 方向无效：源协议和目标协议相同。
- 透传会跳过：命中规则存在，但全局或渠道透传开启时不会执行转换。
- 包含高级字段：存在 WebUI 暂不识别但需要保留的字段。

### 4.3 规则编辑抽屉

字段分组如下。

基础配置：

- 规则名称
- 启用规则
- 源协议
- 目标协议

命中范围：

- 作用于全部渠道
- 指定渠道 ID，支持输入 `117,142`
- 指定渠道类型，使用渠道类型多选
- 模型正则，一行一个

高级选项：

- Responses 自定义工具桥接
  - 对应 `options.enable_custom_tool_bridge`
  - 仅当方向为 `responses` 到 `chat_completions` 时可开启
  - 其他方向显示为禁用，并解释不适用原因

命中预览：

- 输入渠道 ID、渠道类型和模型名后，实时判断当前规则是否命中。
- 示例：`channel_id=117`、`channel_type=1`、`model=gpt-5.5`。
- 预览必须同时展示全局透传和渠道透传的影响。若透传开启，结果显示为“规则匹配，但运行时会跳过转换”。

底部操作：

- 保存
- 取消
- 删除当前规则

## 5. 字段映射

| WebUI 字段 | JSON 字段 | 说明 |
| --- | --- | --- |
| Legacy 启用状态 | `enabled` | 仅在 `rules` 为空的 legacy 模式下生效。规则模式下不得展示为总开关。 |
| Legacy 作用于全部渠道 | `all_channels` | 仅用于读取和迁移旧格式。 |
| Legacy 指定渠道 ID | `channel_ids` | 仅用于读取和迁移旧格式。 |
| Legacy 指定渠道类型 | `channel_types` | 仅用于读取和迁移旧格式。 |
| Legacy 模型正则 | `model_patterns` | 仅用于读取和迁移旧格式。 |
| 规则名称 | `rules[].name` | 必填，默认 `responses-to-chat` 或 `chat-to-responses`。 |
| 启用规则 | `rules[].enabled` | 默认开启。 |
| 源协议 | `rules[].source_endpoint` | 枚举：`chat_completions`、`responses`。 |
| 目标协议 | `rules[].target_endpoint` | 枚举：`chat_completions`、`responses`。 |
| 作用于全部渠道 | `rules[].all_channels` | 开启后忽略渠道 ID 和渠道类型。 |
| 指定渠道 ID | `rules[].channel_ids` | 正整数数组。 |
| 指定渠道类型 | `rules[].channel_types` | 正整数数组。 |
| 模型正则 | `rules[].model_patterns` | 字符串数组。 |
| Responses 自定义工具桥接 | `rules[].options.enable_custom_tool_bridge` | 仅适用于 `responses` 到 `chat_completions`。 |

## 6. 兼容与保留策略

### 6.1 旧格式兼容

旧格式示例：

```json
{
  "enabled": true,
  "all_channels": false,
  "channel_ids": [117],
  "model_patterns": ["^gpt-5.*$"]
}
```

WebUI 读取时转换为一条 legacy 规则展示：

```json
{
  "name": "legacy-chat-to-responses",
  "enabled": true,
  "source_endpoint": "chat_completions",
  "target_endpoint": "responses",
  "all_channels": false,
  "channel_ids": [117],
  "model_patterns": ["^gpt-5.*$"]
}
```

保存时统一写成 `rules` 格式，减少双形态维护成本。

实施决策：

- 读取旧格式时必须展示“Legacy 配置，将在保存后升级为 rules 格式”。
- 只要管理员点击保存，就强制写回 `rules` 格式。
- 升级后不再保留顶层 legacy 范围字段，避免同一配置存在两套可读但语义不同的状态。
- 若需要保留旧格式原文，用“查看 JSON”中的保存前快照提供复制，而不是让运行配置继续双写。

### 6.2 未知字段保留

解析规则时保留未知字段：

- 顶层未知字段保存在 policy extra 区。
- 规则未知字段保存在 rule extra 区。
- `options` 下未知字段保存在 options extra 区。

序列化时把 extra 合并回原位置。

这样可以避免 WebUI 可视化编辑器覆盖未来后端新增字段。

后端保存链路要求：

- 实施前强类型 normalize 会丢未知字段，因此本方案实施时必须同步改造后端规范化逻辑。
- 可选实现是先解析为 `map[string]json.RawMessage`，再解析已知字段做校验和规范化，最后把未知字段合并回原层级。
- 顶层、`rules[]`、`rules[].options` 三个层级都必须覆盖。
- 若后端未完成未知字段保留，前端不得宣称“可视化编辑不丢高级字段”。

### 6.3 classic 修复要求

`web/classic` 实施前可视化编辑器至少需要做到：

- 读取规则时保留 `options`。
- 保存规则时保留 `options.enable_custom_tool_bridge`。
- 可视化模式如果暂不提供完整高级选项，也不能丢字段。
- 删除规则必须二次确认。

该修复必须在 `web/default` 规则管理器发布前完成，并作为同一发布门禁验收。否则管理员在 `web/default` 配好自定义工具桥接后，只要再通过 `web/classic` 保存一次，就可能破坏生产配置。

## 7. 校验规则

阻断保存的校验：

- 源协议和目标协议不能相同。
- 渠道 ID 必须是正整数。
- 渠道类型必须是正整数。
- 模型正则必须能被 Go `regexp` 编译。
- `enable_custom_tool_bridge` 只能在 `responses` 到 `chat_completions` 时开启。

允许保存但必须告警的状态：

- 渠道范围为空，且 `all_channels` 不是 true。规则不会命中。
- 模型正则为空。规则不会命中。
- 规则停用。规则保留但不参与匹配。
- 命中规则存在，但全局或渠道透传开启，运行时会跳过转换。

破坏性操作校验：

- 删除规则需要二次确认。

阻断保存的校验失败时不保存，显示具体规则名称和字段。告警状态允许保存，但必须在规则列表、规则编辑抽屉和保存前确认中明确显示。

前后端职责：

- 后端硬校验：协议值、源目标不能相同、渠道 ID 和渠道类型必须是正整数、`enable_custom_tool_bridge` 只能用于 `responses` 到 `chat_completions`。
- 后端软校验或告警：模型正则为空、渠道范围为空、规则停用、透传导致转换跳过。
- 正则可编译必须以后端 Go `regexp` 为准。前端可以用 JavaScript `RegExp` 做即时提示，但不能把 JS 校验结果作为最终依据。
- JSON 模式、classic、API 保存都必须走同一套后端校验与规范化。

## 8. 推荐交互流程

### 8.1 修复渠道 117 的自定义工具桥接

管理员操作：

1. 打开协议转换兼容模块。
2. 点击新增规则。
3. 方向选择 `Responses` 到 `Chat Completions`。
4. 渠道 ID 填写 `117`。
5. 模型正则填写 `^gpt-5(\\..+)?$`。
6. 开启 Responses 自定义工具桥接。
7. 保存。

生成配置：

```json
{
  "rules": [
    {
      "name": "responses-to-chat-codex-custom-tools",
      "enabled": true,
      "source_endpoint": "responses",
      "target_endpoint": "chat_completions",
      "all_channels": false,
      "channel_ids": [117],
      "model_patterns": ["^gpt-5(\\..+)?$"],
      "options": {
        "enable_custom_tool_bridge": true
      }
    }
  ]
}
```

### 8.2 新增普通 Chat 到 Responses 转换

管理员操作：

1. 点击新增规则。
2. 方向选择 `Chat Completions` 到 `Responses`。
3. 选择渠道类型或填写渠道 ID。
4. 填写模型正则。
5. 保存。

该方向不显示自定义工具桥接开关。

## 9. 实施阶段

本轮实现已一次性交付阶段一到阶段三；以下阶段拆分保留为设计和验收口径。

### 阶段一：配置保存链路与 classic 防丢字段

范围：

- 改造后端 `NormalizeChatCompletionsToResponsesPolicyJSON`，保留未知字段。
- 增加后端校验，覆盖协议方向、正整数范围和自定义工具桥接方向。
- 修复 `web/classic` 可视化模式丢失 `options` 的问题。
- `web/classic` 删除规则增加二次确认。

验收：

- classic 可视化编辑后不丢 `options.enable_custom_tool_bridge`。
- classic 原始 JSON 和可视化模式切换不会破坏当前规则。
- 含未知顶层字段、规则字段、options 字段的 JSON 经过保存链路后不丢字段。

### 阶段二：default 前端规则管理器

前置条件：

- 阶段一已完成并通过验收。
- 同一发布包必须包含阶段一产物，避免 default 已可配置但 classic 仍会丢字段。

范围：

- 新增 `web/default` 协议转换规则编辑组件。
- 用可视化列表和编辑抽屉替换主入口 JSON 文本框。
- 保留查看 JSON 和导入 JSON。
- 实现 `options.enable_custom_tool_bridge` 开关。
- 实现旧格式读取，并在保存时强制升级为 `rules` 格式。
- 增加渠道选择辅助：支持按渠道 ID、名称、模型搜索，选择后自动填入渠道 ID。

验收：

- 管理员不手写 JSON 可以配置渠道 `117` 的自定义工具桥接。
- 保存结果包含 `options.enable_custom_tool_bridge: true`。
- 旧 JSON 能正常回显。
- 旧 JSON 保存后明确升级为 `rules` 格式。

### 阶段三：命中预览与校验体验

范围：

- 增加正则校验、渠道 ID 校验、命中范围校验。
- 增加命中预览。
- 在命中预览中展示全局透传和渠道透传影响。
- 增加恢复到上次保存的操作，降低误改复杂配置的成本。

验收：

- 无效规则不能静默保存为看似成功。
- 不会命中的规则允许保存，但必须明确标记为告警状态。
- 命中预览结果与后端匹配逻辑一致。
- 正则最终校验以后端 Go `regexp` 结果为准。

## 10. 测试计划

前端：

```bash
cd web/default
bun run lint
bun run build
```

必要时补充：

```bash
cd web/default
bun run typecheck
bun run i18n:sync
```

后端相关回归：

```bash
go test ./setting/... ./service/... ./relay/...
```

浏览器验证：

- 亮色主题。
- 暗色主题。
- 桌面宽度。
- 移动宽度。
- 新增规则。
- 编辑规则。
- 复制规则。
- 删除规则。
- 导入 JSON。
- 查看 JSON。
- 保存后刷新页面回显。

配置 round-trip 验证：

- legacy JSON 导入后展示为 legacy 规则，保存后升级为 `rules` 格式，刷新后仍可回显。
- rules JSON 导入后进入可视化模式，修改规则名称并保存，刷新后语义不变。
- JSON 中包含顶层未知字段，保存后字段和值保持不变。
- JSON 中包含 `rules[].unknown_field`，保存后字段和值保持不变。
- JSON 中包含 `rules[].options.some_future_field`，可视化修改其他字段并保存后，该字段和值保持不变。
- JSON 模式和可视化模式来回切换，不丢 `options.enable_custom_tool_bridge`。
- default 与 classic 分别保存同一份配置，不丢 `options` 和未知字段。

真实保存链路验证：

- 通过后台接口保存配置，而不是只测前端本地序列化。
- 保存后重新读取 `global.chat_completions_to_responses_policy`，确认后端 normalize 后的 JSON 符合预期。
- 运行包含未知字段保留的后端单元测试，防止只在前端通过。

## 11. 完成标准

- WebUI 覆盖协议转换模块全部已知功能。
- JSON 只作为高级导入和排障入口。
- 管理员可通过表单完成 `responses` 到 `chat_completions` 的自定义工具桥接配置。
- `web/default` 和 `web/classic` 不再在可视化编辑中丢失 `options.enable_custom_tool_bridge`。
- 后端匹配语义不为 WebUI 改变；但保存链路必须支持未知字段保留和统一校验。
- UI 不再把顶层 `enabled` 表述为规则模式总开关。
- classic 防丢字段与 default 规则管理器同一发布完成。
- 命中预览明确展示 passthrough 跳过转换的情况。
- 相关验证命令通过，并在实施完成后补充到 `docs/reviews/`。
