# 渠道健康与通用模型路由优化复核

## 结论

采样时间：2026-08-26，Asia/Shanghai。

本轮已完成通用渠道路由健康治理实现，并在隔离环境完成代码、测试、构建和 HTTP 健康核对。实现目标是让渠道选择、模型映射、协议转换、上游路径和多 Key 身份使用同一条可追踪的路由身份，避免健康状态或重试过程把请求错误地当成另一条路由。

正式 `13000` 未重启、未发布。正式渠道 201 的真实 504、request id、上游响应和生产 Redis 状态本轮没有重新采集，因此不能把隔离验证表述为生产验收。

## 实现范围

- `model.ResolveChannelModelMapping` 统一解析普通模型和 Responses compact 模型映射，检测映射环，并保留客户端请求模型作为不可变路由模型。
- `service.BuildChannelHealthRouteForChannel` 和 `relay/common.ChannelRouteIdentity` 统一记录 `route_key`、请求模型、最终上游模型、协议、上游路径和 `multi_key_index`。映射与健康选择使用同一份有效上游模型。
- 半开探针续租按 probe token 管理。释放、成功、失败、Redis 异常和请求结束都会清理本地 token 状态并停止对应续租；同一个 Gin context 释放后重新获取 probe 会创建独立续租。
- Redis 健康读取和 half-open probe reserve 使用一次 Lua check-and-acquire；cooldown、history、probe、runtime index 和 metadata 的 TTL/语义保持不变。base route 与 credential route 使用独立的 Redis 超时上下文。
- Redis 不可用继续 fail-open 放行，但增加进程级限频审计日志；请求级 admin 信息仍标记 `redis_unavailable`，不伪造健康结果。
- 流式渠道只有在正常结束且没有非良性 `EndError` 或软错误时恢复。Codex folded SSE 的 `handler_stop` 且无错误仍视为正常结束。
- Default 和 Classic 的渠道健康运行态、请求路由日志和事件去重均使用上述路由字段，并把 React 唯一 key 扩展到路由维度，避免多模型、多协议、多路径和多 Key 冲突。
- 两套前端 429 文案与后端一致：按 `Retry-After` 冷却，没有该头时使用配置的回退时长。Redis 不可用时说明为跳过健康筛选并记录管理员日志。

## 验证记录

以下命令均在本轮重新执行，退出码为 0，除特别说明外没有失败项：

| 范围 | 命令 | 结果 |
| --- | --- | --- |
| Go 单元与集成 | `go test -count=1 ./service ./controller ./relay/common ./relay/helper ./router` | 通过；service、controller、relay/common、relay/helper、router 均通过 |
| Go race | `go test -race -count=1 ./service ./controller` | 通过 |
| Go 静态检查 | `go vet ./service ./controller ./relay/common ./relay/helper ./router` | 通过 |
| Default 目标测试 | `bun test tests/channel-health-runtime.test.ts tests/usage-logs-routing.test.ts` | 34/34 通过 |
| Classic 目标测试 | `bun test tests/channelHealthRuntime.test.js tests/usageLogsResponsesCompact.test.js` | 33/33 通过 |
| Default 类型检查 | `bun run typecheck` | 通过 |
| Default lint | `bun run lint` | 通过 |
| Default 构建 | `bun run build` | 通过 |
| Classic lint | `bun run lint` | 通过 |
| Classic Node 测试 | `bun run test:node` | 137/137 通过 |
| Classic 构建 | `bun run build` | 通过；Safari compatibility check 通过 |

隔离 Docker 使用当前工作区重新构建：

```text
scripts/build-docker-local.sh new-api-local:dev                         exit 0
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env \
  up -d --no-deps --force-recreate new-api                                 exit 0
docker inspect ...                                                         running healthy
/new-api --build-info                                                      v1.1.0, b42e5f54b2fdb6d0f2d1cd44e4e1cd31af15c1aa-dirty
curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status  HTTP 200
```

重建后第一次立即探活时容器仍为 `starting`，得到一次 `curl 52 Empty reply`；等待到 `running healthy` 后再次执行带超时的探活返回 HTTP 200。这是启动时序，不作为代码失败或通过依据。

## 正式环境边界

本轮只读核对结果：

```text
docker inspect new-api -f '{{.Config.Image}}'
new-api-local:prod-codex-lite-4527d7666

docker exec new-api /new-api --build-info
version=v1.1.0
commit=4527d7666a37f9151a57f08ca0bc5e8e08b8fb46
date=2026-07-17T03:41:30Z

curl --connect-timeout 3 --max-time 10 http://127.0.0.1:13000/api/status
HTTP 200
```

因此正式容器仍不是当前 `b42e5f54...-dirty` 隔离构建，不能宣称 13000 已运行本轮修复。没有执行正式重启、镜像替换、数据库变更或发布操作。

## 未覆盖边界

- 没有重新向正式渠道 201 发起真实请求，未取得新的 504 request id、上游响应、映射后的实际请求模型或生产 Redis 证据。
- 没有运行全量 `go test ./...`；本轮覆盖的是本功能涉及的 service、controller、relay/common、relay/helper、router 及对应前端目标测试。
- 隔离环境的 HTTP 200 只证明当前 dirty checkout 可启动和状态接口可用，不等同于真实上游渠道、计费、长流、生产流量或正式发布验收。

## 工作区说明

工作区在任务开始前已有大量 dirty/untracked 修改。本轮没有使用回退或清理命令，也没有把无关修改归因于本任务；最终范围复核以 `git status --short --branch`、`git diff --name-only` 和相关文件审查为准。
