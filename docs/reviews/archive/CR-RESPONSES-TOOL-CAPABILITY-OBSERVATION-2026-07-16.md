# Responses 工具能力正向观测审计

## 范围

- 采样日期：2026-07-16
- 目标：从成功的原生 Responses 输出中记录 custom、namespace、tool_search 的正向能力证据。
- 边界：不修改数据库 schema，不启用协议转换，不修改正式渠道配置，不把缺少工具调用推断为不支持。

## 根因证据

- 正式渠道 160 在 2026-07-16 17:02:43 被手工写入 `supports_custom_tools=false`、`supports_namespace_tools=false`、`supports_tool_search=false`。
- 2026-07-16 17:03:15，渠道 160 完成的真实 Codex Responses 请求生成了 `custom_tool_call exec`，并被客户端正常执行。该证据直接否定 `supports_custom_tools=false`。
- 原实现只记录部分失败响应的 capability observation，不记录成功输出实际出现的工具类型。手工错误结论因此无法形成可审计冲突证据。
- 当前渠道 160 的隔离环境 basic 探测返回上游 429 `Concurrency limit exceeded for account`。429 不能用于判断协议能力。

## 实现

- relay 在完整输出或 `response.output_item.done` 中观测以下原生类型：
  - `custom_tool_call`
  - 带非空 `namespace` 的 `function_call` 或 `custom_tool_call`
  - `tool_search_call`
- controller 只在 Responses 请求成功结束后提交观测。失败流、未完成流和仅出现 added 事件的流不会提升能力。
- model 使用事务和行锁合并单调正向证据，并以 300 秒窗口去抖。
- 观测只将未知能力补齐为 `true`，不覆盖显式 `false`。发生证据冲突时保留显式配置并写告警。
- 观测证据写入渠道 `settings.responses_capability_registry.observed_tools`，不新增数据库列。
- usage log 的管理员信息增加 `responses_channel_tool_capability_observed`，不记录请求正文、工具参数或凭证。

## 验证

### L0/L1 已通过

```text
go test -count=1 -timeout=60s ./controller ./model ./relay/common ./relay/helper ./relay/channel/openai ./service
exit 0

go vet ./controller ./model ./relay/common ./relay/helper ./relay/channel/openai ./service
exit 0

go test -count=1 -timeout=10m ./...
exit 0

go test -race -count=1 -timeout=3m ./model ./relay/channel/openai ./controller ./service
exit 0

git diff --check
exit 0
```

覆盖行为：

- custom、namespace、tool_search 输出识别。
- namespace function call 识别。
- 未知能力正向提升。
- 显式 false 不被覆盖。
- 多次观测合并。
- 损坏 settings JSON 显式失败且不清空原数据。
- 管理员 usage log 输出正向观测证据。

### L2 已执行

- 使用提交 `e6e0c31fd4d6f06f71a13f44afc6bbfd9daf8a0d` 重建 `new-api-local:dev` 并强制重建 3001；HEAD、容器 build-info 与 `/api/status` 一致。
- 开发渠道 205 的 custom grammar 请求在 forced 与 auto tool choice 下均成功，输出正确工具名。原始 SSE 的 `response.output_item.done` 明确为 `custom_tool_call`，因此 custom 正向观测有直接事件证据。
- 同一上游的 `response.completed.output` 将该项写成 `function_call`。观测来源是中间 `custom_tool_call`，不是把普通终止 `function_call` 误记为 custom。
- 本机 Codex CLI 0.144.5 经 3001 指定渠道 205 实际执行 shell 工具成功，并在下一轮携带 `custom_tool_call`、`custom_tool_call_output` 历史继续完成。两次服务端日志均为 HTTP 200、`stream_status=done`。
- 渠道 205 的 registry 只记录 `custom_tool_call=true`，没有写入 namespace 或 tool_search 证据。namespace 请求在 metadata 后收到上游 `upstream_connection_error`，由新断流逻辑明确返回 HTTP 502。
- 开发渠道 160 的错误 false 已移除，但当前上游持续返回 429 并发限制；429 未被用于推断能力。tool_search 也没有新的真实成功证据。
- 测试结束后开发渠道 205 已恢复原始 `status=2`。正式数据库和正式容器未修改。

## 发布门禁

- custom 正向观测链路的 L2 已通过；namespace 与 tool_search 仍必须保持未知，不能因 custom 成功一并放开。
- 正式变更前重新备份数据库，并再次确认转换策略保持关闭。
- 正式渠道 160 只能移除缺乏依据的 custom false；namespace 与 tool_search 需继续等待各自真实成功证据。
- 升级后观察 502、524、429、压缩、工具调用和 capability conflict 日志。
