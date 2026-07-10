# Responses namespace 复核记录

## 范围

- 复核提交 `ca8232ba3` 之后的 Responses namespace 兼容逻辑。
- 修复 SSE 错误事件先下发再触发重试的风险。
- 修复 namespace 历史清理过宽的问题，只移除上游点名的 `input[index].namespace`。
- 补充直接单元测试，覆盖参数解析、索引越界、目标项缺少 namespace 和正常移除路径。

## 代码结论

- `response.error` 和 `error` SSE 事件在识别为上游错误后不会写入客户端响应体，调用方仍可拿到结构化 `unknown_parameter` 错误并进入重试判断。
- namespace 重试仅在错误参数匹配 `input[index].namespace` 时触发。
- 重试请求只删除对应输入项的 `namespace` 字段，其他历史项保持不变。
- 本轮未修改正式环境。

## 外部审查

- CodeRabbit 首轮审查返回 1 条低优先级建议：补充 `relay/common/responses_input_filter.go` 的直接单元测试。
- 该建议已采纳，并补充到 `relay/common/responses_input_filter_test.go`。
- CodeRabbit 二次审查长时间只返回心跳状态，未返回最终完成状态；已中断二次进程，避免遗留后台任务。

## 验证命令

```bash
gofmt -w relay/common/responses_input_filter_test.go relay/common/responses_input_filter.go relay/channel/openai/relay_responses.go relay/channel/openai/relay_responses_test.go relay/responses_handler.go relay/responses_handler_test.go
git diff --check
go test ./relay/common -run 'TestResponsesNamespaceHistoryIndex|TestStripNamespaceFromResponsesInputHistory'
go test ./relay/channel/openai ./relay -run 'TestOaiResponsesStreamHandlerPreservesUpstreamParameterError|TestExecuteOpenAIResponsesRequestRetriesRejectedNamespaceHistory'
go test ./controller ./relay/common ./relay/channel/openai ./relay ./service ./setting/ratio_setting
go test ./...
go vet ./...
go test -race ./relay/common ./relay/channel/openai ./relay ./relay/helper
coderabbit review --agent -t uncommitted
scripts/build-docker-local.sh new-api-local:dev
```

## 验证结果

- `git diff --check`：通过。
- `go test ./relay/common -run 'TestResponsesNamespaceHistoryIndex|TestStripNamespaceFromResponsesInputHistory'`：通过。
- `go test ./relay/channel/openai ./relay -run 'TestOaiResponsesStreamHandlerPreservesUpstreamParameterError|TestExecuteOpenAIResponsesRequestRetriesRejectedNamespaceHistory'`：通过。
- `go test ./controller ./relay/common ./relay/channel/openai ./relay ./service ./setting/ratio_setting`：通过。
- `go test ./...`：通过。
- `go vet ./...`：通过。
- `go test -race ./relay/common ./relay/channel/openai ./relay ./relay/helper`：通过。
- `coderabbit review --agent -t uncommitted`：首轮低优先级建议已处理；二次审查未在等待窗口内返回最终结果。
- `scripts/build-docker-local.sh new-api-local:dev`：通过，生成 `new-api-local:dev`，构建标识为 `ca8232ba372054dac0d855ce1e1433df6549381b-dirty`，其中 `dirty` 表示提交前仍有未提交改动。

## 边界

- 本记录覆盖本地代码、测试、静态检查、竞态测试和隔离开发镜像构建。
- 提交后仍需强制重建隔离 `3001` 服务，并用 `/new-api --build-info` 与 `/api/status` 复验运行态。
