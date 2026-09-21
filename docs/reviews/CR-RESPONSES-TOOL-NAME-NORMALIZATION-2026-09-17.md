# Responses 工具名校正故障复核

采样时间：2026-09-17（Asia/Shanghai）

## 故障证据

截图中的 OpenAI Responses 错误为 `HTTP 400`，参数为 `input[25].name`，上游要求工具名匹配 `^[a-zA-Z0-9_-]+$`。此前正式日志在 channel 213 和 channel 171 记录了同类错误。

## 根因

OpenAI-compatible Responses 的原生出站路径在满足 byte-preserving 条件时会直接复用客户端请求体。历史 `function_call`、`custom_tool_call`、`tool_search_call`、MCP 工具调用以及遗留工具调用中的 `name` 没有经过 `dto.NormalizeToolName`，因此含有点号、斜线或空格的 MCP 工具名会原样送到严格校验的上游。

## 修复内容

- `relay/common/responses_tool_names.go` 为顶层工具、namespace 嵌套工具、`additional_tools`/`tool_search_output` 加载工具、`tool_choice` 和工具历史项建立请求级别的稳定别名表。
- 别名表检测不同原始名称归一化后的冲突并显式失败，避免工具调用静默指向错误声明。
- `relay/responses_handler.go` 在 OpenAI/Azure Responses 原生出站前执行校正；工具名改变时关闭 raw pass-through，保留其它未知字段。
- 含 `encrypted_content` 的 opaque replay 仍在校正前按原始字节重放，避免破坏上游状态。

## 验证证据

```text
go test -count=1 ./relay/common ./relay/channel/openai ./relay       exit 0
go test -count=1 ./controller ./model ./relay/common ./relay/helper ./service  exit 0
go test -count=1 ./...                                           exit 0
git diff --check -- relay/common/responses_tool_names.go relay/common/responses_tool_names_test.go relay/responses_handler.go relay/responses_handler_test.go  exit 0
scripts/build-docker-local.sh new-api-local:dev                    exit 0
```

回归测试覆盖声明、选择、namespace 和加载工具、遗留及 MCP 历史项、别名冲突、合法名称不变、raw pass-through 关闭，以及 opaque replay 原样保留。隔离镜像重建后：

- 容器：`new-api-dev-isolated-new-api-1`
- 端口：`127.0.0.1:3001`
- build-info：`v1.1.0`，commit `5cda680770543782d5d0a757b31f4e783443dc03-dirty`
- 容器状态：healthy，restart=0
- `/api/healthz`：HTTP 200
- `/api/status`：HTTP 200

本轮复核（2026-09-18 Asia/Shanghai）再次执行隔离应用容器强制重建；容器创建时间为 `2026-09-17T16:20:03Z`，镜像为 `new-api-local:dev`，build-info 与当前 `HEAD`（`5cda680770543782d5d0a757b31f4e783443dc03`）一致并带 dirty 标记，健康状态仍为 healthy、restart=0、OOM=false。

正式容器最近 8 小时日志采样未出现新的 `Invalid 'input[n].name'` 匹配，但仍有上游 502/429。该采样不等于真实请求通过。

## 未覆盖边界

本轮未向正式 `13000` 发起请求，未重启、部署或写入正式数据库和 Redis，也未完成真实上游模型请求验收。正式渠道 171/213 的修复效果仍需在允许变更窗口内用真实请求和 request id 对照上游响应确认。
