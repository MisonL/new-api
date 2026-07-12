# Responses remote compaction 收口审计

## 审计范围

本轮复核上一个 Codex 会话中记录的 Responses remote compaction、SSE 错误、兼容重试和隔离环境验证问题，并对当前代码补充边界修复。

## 已发现并修复

1. `response.completed` 明确返回 `output: []` 时，必须覆盖此前收到的 `response.output_item.done`，不能沿用旧 compaction 项伪造成功。
2. `encrypted_content` 必须是可解析且去除空白后非空的字符串；对象、数字、布尔值、`null` 和空白字符串均拒绝。
3. `response.completed.response.output` 是非空时的最终快照；此前的普通 output item 不得阻止最终快照生效。
4. remote compaction 流必须收到带 `response` 对象的 `response.completed`。缺少终止事件、只收到 `[DONE]`、连接 EOF 或 completed 缺少 response 对象时，返回 502，且不向客户端发送缓存事件。
5. remote compaction 流在校验完成前最多缓存 8 MiB，超过上限显式失败，不静默降级。

## 外部状态边界

- 渠道 72、205 历史上的 502、524，以及渠道 150 的 503，属于上游可用性或账户状态问题，不是本地 Responses 语义修复失败。
- 渠道 150 的 `gpt-5.6*` 已按生产审计结论保持禁用，未在本轮重新启用。
- `grok-4.5` 未启用时返回 503 是显式配置结果，不存在隐藏模型回退。
- 本轮只更新代码和隔离开发环境，未升级正式服务；正式服务仍需按独立发布流程执行。

## 验证记录

以下命令均退出码为 0：

```text
go test ./relay/channel/openai ./relay -run 'Test(OaiResponsesStreamHandlerRejectsRemoteCompactionWithoutCompletedEvent|OaiResponsesStreamHandlerRejectsRemoteCompactionCompletedWithoutResponse|OaiResponsesStreamHandlerRejectsExplicitEmptyCompletedOutputSnapshot|OaiResponsesStreamHandlerUsesCompletedOutputAsRemoteCompactionSnapshot|OaiResponsesStreamHandlerAcceptsRemoteCompactionFromCompletedResponse|RemoteCompactionStreamStateEnforcesBufferLimit|ValidateRemoteResponsesCompactionOutputRequiresExactlyOneCompactionItem)$' -count=50 -timeout=180s
go test ./... -count=1 -timeout=300s
go test -race ./relay/channel/openai ./relay -count=1 -timeout=240s
go vet ./...
git diff --check
```

验证覆盖显式空最终快照、缺失 completed、completed 缺少 response、混合输出、非法 `encrypted_content`、重复 compaction 和缓存上限。真实上游对 malformed 流的触发仍依赖上游返回对应畸形事件，因此本轮以离线语义测试和现有隔离环境成功请求共同作为证据，不将上游不可用误报为已覆盖。
