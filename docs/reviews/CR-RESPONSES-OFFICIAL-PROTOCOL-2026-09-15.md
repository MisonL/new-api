# Responses API 官方流协议复核

采样时间：2026-09-16（Asia/Shanghai）

## 官方依据

- [Responses streaming events](https://developers.openai.com/api/reference/resources/responses/streaming-events)
- [Streaming API responses](https://developers.openai.com/api/docs/guides/streaming-responses)

官方事件定义要求：

- `response.completed` 表示响应完成，事件包含 `response` 对象。
- `response.incomplete` 表示响应不完整，事件包含 `response` 对象。
- `response.failed` 表示响应失败，事件包含失败的 `response` 对象。
- 顶层 `error` 事件使用 `code`、`message`、`param` 和必需的 `sequence_number`。
- Responses 流不使用 `response.done` 作为成功终止依据；`[DONE]` 也不是成功判定。

## 本次复核结果

- 原生 `/v1/responses` 流仅接受 `response.completed` 或 `response.incomplete` 作为成功结算终止事件。
- 两类终止事件缺少 `response` 对象时返回 502，不向客户端继续转发，也不进入成功结算。
- `response.done` 在原生 Responses 路径明确按非标准事件返回 502。
- 顶层 `error`、`response.failed` 和兼容上游的嵌套错误统一提取为 OpenAI 错误；错误优先于完成状态。
- Chat 兼容转换路径保留其既有 `response.done` 兼容行为，避免把兼容适配器误当成原生 Responses 协议。
- 观测日志只把 `response.completed` / `response.incomplete` 标记为完成确认；`[DONE]` 仅记录收到结束标记。
- usage 在事件间合并，重试 attempt 保留，敏感字段和日志大小受限。
- 代理生成的 Responses `error` 事件使用当前流的下一个连续 `sequence_number`；若上游没有提供序号，则从 1 开始。

## 验证证据

```text
go test -count=1 ./...                                             exit 0
go test -race -count=1 ./relay/common ./relay/channel/openai ./relay/helper ./controller  exit 0
python3 scripts/compose-healthcheck-contract.py                   exit 0
bash scripts/macos-docker-tun-monitor.test.sh                      exit 0
bash scripts/macos-docker-tun-recovery-watchdog.test.sh            exit 0
bash scripts/install-macos-docker-tun-monitor.test.sh              exit 0
bash scripts/install-macos-docker-tun-recovery.test.sh              exit 0
gofmt -d <受影响 Go 文件>                                            exit 0
git diff --check                                                   exit 0
```

全仓测试最终完整通过；长时间无输出期间没有重复启动或提前终止同一测试进程。

上述命令均直接在本机 macOS 工作区执行。Docker 仅用于隔离的被测
`new-api` 服务，不是本任务的执行环境。

隔离环境：

- 镜像：`new-api-local:dev`
- 容器：`new-api-dev-isolated-new-api-1`
- 端口：`127.0.0.1:3001`
- build-info：`v1.1.0`, commit `5cda680770543782d5d0a757b31f4e783443dc03-dirty`
- `/api/status`：HTTP 200，容器 healthy

正式环境只读探针：

- 容器：`new-api`
- 端口：`127.0.0.1:13000->3000`
- TCP 连接成功，`/api/status` 最终 HTTP 200，容器 healthy
- 本轮未重启、未部署、未写正式数据库或 Redis

## 未覆盖边界

本轮未向真实上游发起模型推理请求，也未修改正式服务。正式日志仍需按 request id 对照上游事件，尤其关注长连接超时、`stream_read_error` 和 goroutine 退出超时；这些运行时现象不能仅凭协议单元测试判定为已消失。
