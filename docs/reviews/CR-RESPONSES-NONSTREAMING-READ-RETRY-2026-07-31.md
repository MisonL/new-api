# CR-RESPONSES-NONSTREAMING-READ-RETRY-2026-07-31

## Summary

采样时间：2026-07-31T03:06:00+0800。

本记录覆盖一个 Responses 可靠性原子任务：非流式上游响应已返回头部、但读取 body 时中断的错误分类与跨渠道重试边界。

修复前，Responses、native/synthetic compact 和两个 Responses/Chat 兼容桥会把全部 `io.ReadAll` 错误统一包装为 `500/read_response_body_failed`。全局 500 重试策略可能将未知读取失败跨渠道重放；而已写出下游字节时，缺少 prelude 安全标记也可能继续重试。

修复后，只有可识别的上游传输中断才映射为 `502/upstream_transport_interrupted`，默认仅在下游尚未写出、请求仍存活时允许切换。下游已写出字节时，只有显式 `responses_stream_prelude_retry_safe=true` 的安全 prelude 才允许例外切换。客户端取消、deadline、响应体限制和未知 reader failure 均保留显式 `500/read_response_body_failed`，带 `skipRetry`，不会自动禁用渠道或跨渠道重放。

## Scope

- 工作树：`/Volumes/Work/code/new-api/.worktrees/responses-opaque-replay-integration`
- 分支：`integration/responses-opaque-replay-p0`
- 基线提交：`cf92ceac2e4c4e117651dd2274234fd73b2e9f43`
- 修改范围：Responses 非流式响应读取、relay 重试边界、HTTP/2 传输异常识别和回归测试。
- 未修改：数据库 schema、正式服务、正式数据库、正式 Redis、渠道配置、用户密钥。

## Contract

- `io.ErrUnexpectedEOF`、HTTP/2 GOAWAY、HTTP/2 stream reset 和 connection reset 视为可识别上游传输中断。
- 默认只有请求上下文仍存活且下游尚未写出时，传输中断可以进入现有 failover；下游已写出时，必须显式设置 `responses_stream_prelude_retry_safe=true`。
- opaque `encrypted_content` replay 仍绝对禁止跨渠道切换。
- 一旦下游已写出，默认禁止重试；只有显式设置 `responses_stream_prelude_retry_safe=true` 的 prelude 可以例外。
- 未知 body read 错误不能作为“上游暂时不可用”的证据，不重试、不自动禁用渠道，也不伪造成功。

## Reproduction

新增失败测试后、实现前执行：

```bash
go test ./controller ./relay/channel/openai \
  -run 'Test(ResponsesNonStreamingHandlersClassifyResponseBodyReadErrors|ShouldRetryReadResponseBodyFailureRequiresSafeReplay|ShouldRetryRequiresExplicitResponsesPreludeSafetyAfterWrite)$' \
  -count=1 -timeout=120s
```

结果：退出码 `1`。失败证明原实现把 `io.ErrUnexpectedEOF` 仍归类为 `read_response_body_failed`，取消、deadline、响应体超限和未知 reader failure 也未携带 `skipRetry`；默认已写出 retry 规则同样未收紧。

## L0 Verification

```bash
gofmt -d controller/relay.go controller/relay_retry_test.go \
  relay/channel/openai/chat_via_responses.go \
  relay/channel/openai/relay_responses.go \
  relay/channel/openai/relay_responses_compact.go \
  relay/channel/openai/relay_responses_synthetic_compact.go \
  relay/channel/openai/responses_via_chat.go \
  relay/channel/openai/responses_response_body_read.go \
  relay/channel/openai/responses_response_body_read_test.go \
  types/error.go types/error_test.go

go test ./controller ./relay/channel/openai ./types -count=1 -timeout=120s
git diff --check
```

结果：全部退出码 `0`；`gofmt -d` 无输出。

测试矩阵覆盖六个非流式入口：原生 Responses、native compact、synthetic compact、synthetic remote compact、Chat-to-Responses 和 Responses-to-Chat。每个入口验证 `unexpected EOF`、typed HTTP/2 stream reset、typed HTTP/2 GOAWAY、取消、deadline、`http.MaxBytesError` 和未知 reader failure 的错误码、HTTP 状态、`skipRetry` 和零下游输出。额外单测验证 nil Gin context 或缺失 `Request` 时，即使错误为 `unexpected EOF` 也必须保持 `skipRetry`。

## L1 Verification

```bash
go test ./common ./dto ./middleware ./router ./relay/constant \
  ./relay/common ./relay/helper ./relay -count=1 -timeout=120s

go test ./model ./service -count=1 -timeout=120s

go test ./controller \
  -run 'TestRelay(RetriesResponsesAfterUnreadableUpstreamBodyBeforeDownstreamWrite|DoesNotRetryOpaqueResponsesAfterUnreadableUpstreamBody|DoesNotRetryUnreadableUpstreamBodyAfterClientCancellation)$' \
  -count=1 -timeout=120s
```

结果：全部退出码 `0`。

端到端回归使用真实 `httptest` 上游声明过大的 `Content-Length` 并提前断开 body，验证可见 Responses 在未写出时从第一个渠道切到第二个渠道成功；opaque replay 和客户端取消各自只命中第一个渠道，并恢复预扣额度。

额外稳定性复核：上述三项 Relay E2E 以 `-count=3` 重复执行退出码 `0`；下列 race 检查退出码 `0`，未报告 data race：

```bash
go test ./controller \
  -run 'TestRelay(RetriesResponsesAfterUnreadableUpstreamBodyBeforeDownstreamWrite|DoesNotRetryOpaqueResponsesAfterUnreadableUpstreamBody|DoesNotRetryUnreadableUpstreamBodyAfterClientCancellation)$' \
  -count=3 -timeout=120s

go test -race ./controller ./relay/channel/openai \
  -run 'Test(ResponsesNonStreamingHandlersClassifyResponseBodyReadErrors|ShouldRetryReadResponseBodyFailureRequiresSafeReplay|ShouldRetryRequiresExplicitResponsesPreludeSafetyAfterWrite|ShouldRetryAllowsResponsesPreludeRecovery|RelayRetriesResponsesAfterUnreadableUpstreamBodyBeforeDownstreamWrite|RelayDoesNotRetryOpaqueResponsesAfterUnreadableUpstreamBody|RelayDoesNotRetryUnreadableUpstreamBodyAfterClientCancellation)$' \
  -count=1 -timeout=180s
```

## L2 Verification

候选运行时代码提交 `188bd27ff2cf85356bd18dea098cb38c161bf3df` 使用临时干净 worktree 构建，避免用户工作树中的 `.gate-tools/` 使镜像变为 `-dirty`：

```bash
git worktree add --detach /tmp/new-api-responses-read-retry-188bd27ff 188bd27ff
scripts/build-docker-local.sh new-api-local:188bd27ff-clean

DEV_NEW_API_IMAGE=new-api-local:188bd27ff-clean \
docker compose --project-name new-api-dev-isolated \
  -f /Volumes/Work/code/new-api/deploy/compose/dev-isolated.yml \
  --env-file /Volumes/Work/code/new-api/deploy/env/dev-isolated.env \
  up -d --no-deps --force-recreate new-api
```

Docker build 与隔离容器重建均退出码 `0`。OCI revision、`/new-api --build-info` 和候选提交均为 `188bd27ff2cf85356bd18dea098cb38c161bf3df`；容器状态为 `running healthy`，`GET /api/status` 返回 `success:true`，未认证 `POST /v1/responses` 返回预期 `401`，最近 120 行启动日志未匹配 panic、fatal、监听失败或迁移失败。

控制面 E2E：

```bash
DEV_NEW_API_IMAGE=new-api-local:188bd27ff-clean \
COMPACT_E2E_ENV_FILE=/Volumes/Work/code/new-api/deploy/env/dev-isolated.env \
COMPACT_E2E_COMPOSE_FILE=/Volumes/Work/code/new-api/deploy/compose/dev-isolated.yml \
COMPACT_E2E_COMPOSE_PROJECT_NAME=new-api-dev-isolated \
COMPACT_E2E_CURL_MAX_TIME=180 \
scripts/compact-control-plane-e2e.sh
```

结果：退出码 `0`。覆盖并通过 native compact 和后续 `previous_response_id`、`stream_options.include_usage` 重试恢复、远端 `413` synthetic compact 恢复、synthetic compact、stale marker、generic OpenAI opaque 拒绝、native/local Codex opaque replay 字节保真且不跨渠道重试、Sub2API REST `previous_response_id` 拒绝、模型切换和真实 Codex history fixture。

收尾日志确认两次 `database_snapshot_match=true`、两次 `redis_snapshot_match=true`，最终输出 `compact_e2e_lock_released=true` 和 `e2e_done=true`。独立检查确认 `/tmp/newapi-compact-e2e.lock` 不存在、无 fake upstream 进程，隔离容器仍为 `healthy` 且 `/api/status` 仍返回 `success:true`。

## Boundary

- 本轮不依赖真实上游凭证；离线和隔离 E2E 证明的是本地分类、重试和恢复语义，不等同于所有正式渠道已验收。
- `Wei-Shaw/sub2api#5021` 截至采样仍为开放 PR，仅作为错误边界的外部参考，未被表述为已合并行为。
- 本轮不通过关闭 Responses Lite、切 Chat、删除转换策略或剔除渠道规避故障。
