# Responses 远程压缩 502 分析与修复

## 结论

本轮 502 不是单一原因：

- 生产日志中 `/v1/responses` 的 502 主要来自上游渠道返回 `502 Upstream request failed`、`502 Upstream service temporarily unavailable`，并伴随 HTTP/2 `INTERNAL_ERROR`。这类错误不能由本地压缩校验修复。
- 远程压缩链路存在一个确定的本地兼容缺陷：上游事件流已经发送合法 `response.output_item.done` compaction，但 `response.completed.response.output` 为空或只包含 message 时，旧实现会用终态 output 覆盖事件流收集结果，最终错误返回 `expected exactly one compaction output, got 0`。

## 证据

采样时间：2026-07-13，生产容器 `new-api`，日志文件 `logs/oneapi-20260713085102.log`。

- 该实例承载持续的 Responses 长连接；13 分钟窗口内完成 217 次 `/v1/responses` 和 68 次 `/v1/messages`。
- 已观察到普通 Responses 请求在渠道 160、204、205、180 上依次返回 502，完整重试链为 `160->204->205->180`，最终错误仍来自上游。
- 历史 `/v1/responses/compact` 请求存在成功和失败混合，失败主要是 500/524；当前代码的远程 v2 严格校验会将空或缺失 compaction 映射为 502。

## 修复

`remoteCompactionStreamState` 现在按以下规则合并输出：

1. 终态 output 已包含 compaction 时，以终态为权威快照。
2. 终态 output 为空时，保留事件流中的 output item。
3. 终态 output 非空但没有 compaction 时，仅补入事件流中已观察到的 compaction item。
4. 事件流和终态都没有合法 compaction，仍返回 502；重复 item 不重复追加。

该修复与 `Wei-Shaw/sub2api` 主线 2026-07-10 合入的 `000f6dc6`（终态 output 非空但缺 compaction 时从 raw SSE 事件补入）及 2026-07-11 合入的 `84bb7d07`（保留 remote_compaction_v2 原生 Responses 流）语义一致。原版 `QuantumNous/new-api` 当前 `upstream/main=7c28993f6` 没有对应的远程压缩修复；其最近相关提交 `92d3c9d18` 仅涉及缓存 token 计费和 `prompt_cache_key` 转发。

## 验证

执行：

```text
go test ./relay/channel/openai -run 'Test(OaiResponsesStreamHandler|RemoteCompactionStreamState)' -count=1 -timeout=180s
go test ./relay/channel/openai ./relay -run 'Responses|Compaction|Compact' -count=1 -timeout=240s
git diff --check
```

结果：全部通过。测试覆盖终态空 output、终态 message 缺 compaction、终态合法 compaction、缺失 response.completed、非法加密内容和重复 compaction。

## 未改变的边界

- 没有把普通上游 502 转换为成功，也没有增加静默重试。
- 没有降低远程压缩输出的合法性校验；合法 compaction 仍必须恰好一个且含非空 `encrypted_content`。
- 尚未宣称生产镜像已包含本修复；需在提交后按隔离环境规则重新构建并验证。

## 2026-07-13 隔离环境真实验证

提交 `0326a30ac` 已构建到隔离容器 `new-api-dev-isolated-new-api-1`，build-info 与 Git HEAD 一致，容器 `healthy`，`/api/status` 返回成功。

使用隔离环境现有有效令牌，对 `POST /v1/responses` 连续发送 3 次 `stream=true`、`x-codex-beta-features: remote_compaction_v2` 和 `compaction_trigger` 请求：

```text
run=1 http=200 bytes=3380 item_types=compaction_summary,message
run=2 http=200 bytes=3204 item_types=compaction_summary,message
run=3 http=200 bytes=3508 item_types=compaction_summary,message
```

三次请求均在 2.9 到 4.1 秒内完成，容器日志对应三次 `/v1/responses` 均为 200，未出现 502。该结果证明修复后的本地流状态合并和远程压缩 v2 链路在当前隔离渠道上可用；不代表所有生产上游渠道都已恢复，生产日志中独立存在的上游 502 仍需按渠道和上游可用性单独处理。
