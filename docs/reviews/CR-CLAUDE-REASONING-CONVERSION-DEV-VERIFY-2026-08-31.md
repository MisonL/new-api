# CR-CLAUDE-REASONING-CONVERSION-DEV-VERIFY-2026-08-31

## Summary

采样时间：2026-08-31T20:39:48+0800。

本轮复核 Claude Code 使用的 Anthropic Messages 请求经协议转换为 OpenAI Chat/Responses 后，历史 `reasoning_content` 是否会丢失。复核覆盖普通 thinking、空 thinking、`signature`、`redacted_thinking`、工具调用、DeepSeek 模型别名、转换错误状态码和请求体重放。

## Changes reviewed

- `service/convert.go`
  - 仅从 assistant 消息提取可表达的 thinking 文本为 `reasoning_content`。
  - 非 assistant 的 thinking 与 `redacted_thinking` 不进入推理字段。
  - 带 opaque signature 或 `redacted_thinking` 的状态无法安全映射时显式失败，不静默丢失。
  - DeepSeek 模型识别覆盖 provider、slash、dot、colon 等限定别名。
  - DeepSeek 工具调用历史保留显式空 `reasoning_content` 字段。
  - OpenAI 返回显式空 `reasoning_content` 且包含工具调用时，转换回 Claude 保留空 thinking block。
- `relay/claude_handler.go`
  - Claude 请求转换失败返回 HTTP 400，并跳过重试或换渠道，避免客户端协议错误被伪装成 500。
  - 保留请求体大小和可重读 reader，支持后续重试链路使用同一请求体。

## Verification

以下命令均退出码为 0：

```bash
gofmt -w service/convert.go service/convert_claude_request_test.go service/convert_test.go
git diff --check

go test -p 1 ./service ./service/openaicompat ./relay \
  ./relay/channel/claude ./relay/channel/openai ./relay/channel/deepseek \
  ./relay/common ./relay/helper ./controller ./model \
  -count=1 -timeout 180s

go vet -p 1 ./service ./service/openaicompat ./relay \
  ./relay/channel/claude ./relay/channel/openai ./relay/channel/deepseek \
  ./relay/common ./relay/helper ./controller ./model

scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml \
  --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
```

测试结果：

- `service`：通过，20.968s。
- `service/openaicompat`：通过，0.042s。
- `relay`：通过，17.677s。
- `relay/channel/claude`：通过，0.060s。
- `relay/channel/openai`：通过，1.141s。
- `relay/channel/deepseek`：无测试文件，编译通过。
- `relay/common`：通过，0.064s。
- `relay/helper`：通过，14.315s。
- `controller`：通过，6.990s。
- `model`：通过，4.266s。
- `go vet`：无 finding。
- Docker 默认前端、classic 前端和 Go 二进制构建均通过，镜像为 `new-api-local:dev`。

隔离开发环境运行核对：

```text
container: new-api-dev-isolated-new-api-1
state: running healthy
build-info: version=v1.1.0
commit=6f064edb0ca606f7e9caca2bd72163643717e394-dirty
port: 127.0.0.1:3001/tcp reachable
/api/status: HTTP 200, success=true
```

## Production read-only check

正式服务未重启、未发布、未修改配置。只读核对结果：

```text
container: new-api running healthy
image: new-api-local:prod-codex-lite-4527d7666
port: 127.0.0.1:13000/tcp reachable
/api/status: HTTP 200
```

最近日志采样未发现本轮 400 或 reasoning conversion error。35 号渠道当前可见错误为上游 `429 inference tpm exhausted`，随后按现有策略重试到 36 号渠道；该错误属于上游限流，不是协议转换失败。

## Boundaries

- Anthropic `thinking.signature` 和 `redacted_thinking.data` 是 opaque 状态，OpenAI `reasoning_content` 没有等价承载；本实现按 fail-closed 规则返回 400，不伪造或静默丢弃。
- 本轮执行了离线语义测试、相关包集成编译、隔离 Docker 启动和 HTTP smoke；没有为了验证而向真实上游发送新的带副作用请求。正式日志中的请求是既有运行流量，仅用于只读观察。
- 工作区存在大量既有 dirty 修改，未执行清理、回退或覆盖；镜像 build-info 的 `-dirty` 反映该状态。
