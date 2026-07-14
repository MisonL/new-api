# Responses 压缩恢复与工具兼容验证

## 范围

本轮修复聚焦以下问题：

- `remote_compaction_v2` 上游在首个 SSE data 事件前长时间无响应时，允许按渠道配置提前终止原生压缩并进入合成摘要恢复。
- 远程压缩恢复中的合成摘要遇到 500、502、503、504 或 524 时，显式切换配置的摘要备用模型再试一次。
- OpenAI 兼容上游以结构化错误指出 `input[n].encrypted_content` 缺失时，精确移除对应 reasoning 历史项并重试，不删除 message、compaction 或其他输入项。
- default 与 classic 渠道配置页暴露首 data 超时设置，使用日志展示是否触发该恢复路径。

未改变的边界：

- 429 不进入摘要模型回退，仍使用渠道级限流重试策略。
- 含远程 opaque compaction 状态的请求不做本地合成恢复。
- 普通 Responses 请求的 500 或 502 不触发压缩专用回退。
- 所有恢复尝试仍保留错误日志和使用日志标记；最终失败不会被转换为伪成功。

## 代码验证

| 命令 | 结果 |
|---|---|
| `go test ./... -count=1 -timeout=300s` | 通过 |
| `go test -race ./relay/helper ./relay/channel/openai ./relay ./controller -count=1 -timeout=300s` | 通过 |
| `bun test src/features/channels/lib/channel-form.test.ts`，目录 `web/default` | 8 项通过 |
| `bun run lint`，目录 `web/default` | 通过 |
| `bun run build`，目录 `web/default` | 通过，Safari 兼容检查通过 |
| `node --test src/helpers/responsesCompactSettings.test.js`，目录 `web/classic` | 6 项通过 |
| 受影响 classic 文件的 Prettier 与 ESLint | 通过 |
| `bun run build`，目录 `web/classic` | 通过，Safari 兼容检查通过 |

classic 仓库级 `bun run lint` 仍会被本任务范围外的既有文件 `src/components/table/channels/ChannelsColumnDefs.jsx` 格式问题阻断。本轮未修改该文件，受影响文件均已单独通过 Prettier 和 ESLint。

race 首轮发现 `TestRelayStopsAfterOneOutboundUpstreamRateLimitFollowup` 在异步退款完成前清理全局测试数据库。测试现等待钱包额度恢复后再退出；该测试连续 10 次 race 运行通过，完整 controller race 也通过。生产退款语义未修改。

## 隔离环境

隔离 Compose 栈：

- 应用：`new-api-dev-isolated-new-api-1`，端口 `3001`
- PostgreSQL：`new-api-dev-isolated-postgres-1`，端口 `5433`
- Redis：`new-api-dev-isolated-redis-1`，端口 `6380`

当前源码重新构建为 `new-api-local:dev` 并强制重建应用容器。验证时 build-info：

```text
version=v1.1.0
commit=2072d62b45fd3746d8d16e26ff767e2fe37cd7d5-dirty
date=2026-07-14T04:59:15Z
```

容器 health 为 `healthy`，`GET http://127.0.0.1:3001/api/status` 返回成功。

## 真实上游验证

开发渠道 205 和 206 为正式渠道的隔离副本。测试固定渠道并使用 `/v1/responses`，不记录令牌、渠道密钥、完整上游地址或响应正文。

### 工具兼容

最新镜像在渠道 206 上的最终结果：

| 请求格式 | HTTP | 完成态 | function_call |
|---|---:|---|---:|
| 顶层 `tools` | 200 | completed | 1 |
| `input` 中 `additional_tools` | 200 | completed | 1 |

渠道 205 在上游可用窗口也通过了相同两种格式。用户随后确认 205 上游暂时不可用，因此最终工具回归改用 206；205 当前 502 不作为本地转换回归。

### 压缩恢复

渠道 205 配置 `responses_remote_compaction_first_data_timeout_seconds=5` 后，在上游可用窗口完成以下验证：

- 80 条结构化工程上下文连续 5 次全部 HTTP 200，并返回合法 `compaction`。
- 其中 3 次原模型合成摘要在约 30 秒后返回 Cloudflare 502，日志确认切换 `gpt-5.4` 摘要备用模型后最终 200。
- 一次 528,932-token 请求在 14.711 秒内 HTTP 200，并返回合法 `compaction`。

渠道 206 的结果：

- 普通结构化压缩请求 HTTP 200、completed，并返回合法 `compaction`。
- 同一份大上下文正文经项目 `CountTextToken` 计数为 528,932 tokens、2,131,334 字节；请求在 7.754 秒内 HTTP 200，并返回合法 `compaction`。

该结果否定了“超过 272K 必然由 new-api 本地触发 502”的假设。此前复现的 205 失败来自上游 Cloudflare 源站 502；本轮修复增加了首 data 超时、同渠道合成摘要和摘要备用模型三层显式恢复，但当整个上游持续离线时仍会返回真实失败。

## 正式发布状态

尚未发布。正式部署和部署后验证完成前，不将本记录表述为生产已修复。
