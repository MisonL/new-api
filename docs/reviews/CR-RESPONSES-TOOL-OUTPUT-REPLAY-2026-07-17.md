# Responses 工具输出误报恢复审计

## 范围

- 采样日期：2026-07-17。
- 目标：处理 OpenAI 兼容上游对合法 Responses 工具输出间歇性返回 `input[n].encrypted_content` 缺失错误的问题。
- 边界：不删除工具调用或输出，不删除 compaction，不切换 Chat，不跨渠道重放，不修改数据库 Schema。

## 根因证据

- 正式渠道 207 的三次失败均指向 `input[79].encrypted_content`。
- 失败请求由 Responses Lite 前缀 2 项、compaction 替换历史 73 项和压缩后历史组成。精确重建后，`input[79]` 是 `function_call_output`，不是 compaction 或 reasoning。
- 该项有非空 `call_id`，`output` 是包含两个 `input_text` 的非空数组；同一轮存在匹配的 `function_call`。
- Codex 0.144.5 源码中 `ResponseItem::Compaction.encrypted_content` 是必填字符串；本机会话中的 compaction 也携带 20324 字节的非空加密内容。
- 开发环境固定渠道 207 重放同一份约 6.1 MB、517 项请求时，上游第一次仍返回相同错误，原样同渠道重放后完成；随后同一请求首发直接完成，证明是上游间歇性误判，而不是 new-api 稳定删除字段。

## 修复

- 既有 reasoning 和嵌套加密内容精确剥离逻辑保持不变。
- 当结构化上游错误精确指向顶层 `input[n].encrypted_content`，且目标项是合法的工具输出时，保留原请求并在同渠道重放一次。
- 合法工具输出必须同时满足：类型属于已知工具输出、`call_id` 非空、`output` 为非空字符串或非空数组。
- compaction、message、空输出、对象输出、缺失 `call_id`、嵌套字段和非结构化错误不进入该恢复路径。
- 日志只记录渠道、模型、索引和 item 类型，不记录 call_id、输出正文或加密内容。

## 自动化验证

```text
go test -count=5 -timeout=120s ./relay/common ./relay -run 'TestValidResponsesToolOutputEncryptedContentTarget|TestExecuteOpenAIResponsesRequest(RetriesValidToolOutputForMissingEncryptedContent|DoesNotDropCompactionForMissingEncryptedContent|RetriesUnpopulatedReasoningForMissingEncryptedContent|DoesNotRetryMessageContentMissingEncryptedContent)'
exit 0

go test -count=1 -timeout=180s ./controller ./model ./relay/common ./relay/helper ./relay/channel/openai ./relay ./service
exit 0

go vet ./controller ./model ./relay/common ./relay/helper ./relay/channel/openai ./relay ./service
exit 0

go test -race -count=1 -timeout=4m ./controller ./relay/common ./relay/channel/openai ./relay
exit 0

go test -count=1 -timeout=12m ./...
exit 0

git diff --check
exit 0
```

## 隔离环境验证

- 3001 容器 build-info 为 `a0178146e202ab824e02c59c0437653243f45a4b`。
- 使用开发 token 和渠道 207 的开发副本固定路由；未使用正式令牌。
- 真实历史请求约 6.1 MB、517 项，目标 `input[79]` 为合法 `function_call_output`。
- 场景一：首请求收到结构化缺字段错误，日志记录 `responses valid tool output replay`，第二次同渠道完成，客户端请求最终为 HTTP 200。
- 场景二：相同请求首发直接完成，没有额外重放。
- 两次验证后容器均为 `running healthy`、restart 0。
- 临时 token、ability 已删除，开发渠道 207 已恢复禁用，临时模型能力已移除。

## 发布门禁

- 发布前备份正式数据库并保留上一版镜像。
- 只重建应用容器，不重建 PostgreSQL 和 Redis。
- 发布后观察 `responses valid tool output replay`、`missing_required_parameter`、502、429、流终止错误、容器健康、重启和资源占用。
- 合法请求重放后仍持续失败、错误率抬升、出现跨渠道重放或服务不健康时触发回滚。

## 相关工具可见性事件

- 另一个 Codex 0.144.5 长会话曾输出“当前会话未提供终端与生成工具访问”。
- 对应正式请求均携带 `additional_tools`，有效集合包含 custom `exec` 和两个 function 工具；模型也在同一回合调用过 `wait`。
- 渠道 117 在相同时间窗内使用 `gpt-5.6-terra` 多次返回 custom tool call，不能判定为渠道不支持 custom 工具。
- 曾短暂尝试把 117 标为 `supports_custom_tools=false`，在用户提供其他正常会话反证后立即使用发布前备份完整恢复；恢复后的 settings 内容 SHA-256 与备份一致。
- 该事件属于 `tool_choice=auto` 下的单轮模型行为退化。网关无法在不理解自然语言任务的情况下安全判定“本轮必须调用工具”，因此不增加正文关键词匹配、自动跨渠道重放或禁用渠道等高副作用策略。
