# GPT-5.6 Responses 历史输入修复记录

## 背景

正式服务请求 `gpt-5.6` 时出现两类上游错误：

- `Unknown parameter: 'input[index].namespace'`
- `Invalid type for 'input[index].arguments': expected an object, but got a string instead.`

第一类问题来自 Responses 历史输入中的 `namespace` 被当前上游拒绝。第二类问题来自 Responses 历史输入中的 `function_call.arguments` 仍以字符串形式转发，而当前上游在该路径要求对象。生产截图里还存在 `code:null`、`param:""` 的错误包装形态，索引只出现在 `message` 中。

## 修复内容

- 新增 `input[index].arguments` 参数识别。
- `namespace` 与 `arguments` 均支持从标准 `param` 字段或错误 `message` 中识别 `input[index]`。
- 当上游明确返回 `400`，且错误内容匹配 `Unknown parameter: 'input[index].namespace'` 时，执行一次显式重试并移除目标历史输入项的 `namespace`。
- 当上游明确返回 `400`，且错误内容匹配 `input[index].arguments` 的字符串对象类型错误时，执行一次显式重试。
- 重试前将历史输入中所有可安全解析为 JSON 对象的字符串 `arguments` 转为对象。
- 若目标索引无法解析为对象，不伪造默认成功，返回原始上游错误。
- 新增 `responses_arguments_history_retry` 日志标记，便于环境确认是否命中修复。

## 验证命令

```bash
gofmt -w constant/context_key.go relay/common/responses_input_filter.go relay/common/responses_input_filter_test.go relay/responses_handler.go relay/responses_handler_test.go service/log_info_generate.go
git diff --check
go test ./relay/common -run 'TestResponses(Namespace|Arguments)HistoryIndex|TestNormalizeStringObjectArgumentsFromResponsesInputHistory'
go test ./relay -run 'TestExecuteOpenAIResponsesRequestRetriesRejectedNamespaceHistory|TestExecuteOpenAIResponsesRequestRetriesRejectedNamespaceHistoryFromMessageOnlyError|TestExecuteOpenAIResponsesRequestRetriesRejectedStringArgumentsHistory|TestExecuteOpenAIResponsesRequestRetriesRejectedStringArgumentsHistoryFromMessageOnlyError|TestExecuteOpenAIResponsesRequestDoesNotRetryUnnormalizableArgumentsHistory'
go test ./controller ./relay/common ./relay/channel/openai ./relay ./service ./setting/ratio_setting
go test ./...
go vet ./...
go test -race ./relay/common ./relay ./relay/channel/openai ./relay/helper
```

## 验证结果

- `git diff --check`：通过。
- `go test ./relay/common -run 'TestResponses(Namespace|Arguments)HistoryIndex|TestNormalizeStringObjectArgumentsFromResponsesInputHistory'`：通过。
- `go test ./relay -run 'TestExecuteOpenAIResponsesRequestRetriesRejectedNamespaceHistory|TestExecuteOpenAIResponsesRequestRetriesRejectedNamespaceHistoryFromMessageOnlyError|TestExecuteOpenAIResponsesRequestRetriesRejectedStringArgumentsHistory|TestExecuteOpenAIResponsesRequestRetriesRejectedStringArgumentsHistoryFromMessageOnlyError|TestExecuteOpenAIResponsesRequestDoesNotRetryUnnormalizableArgumentsHistory'`：通过。
- `go test ./controller ./relay/common ./relay/channel/openai ./relay ./service ./setting/ratio_setting`：通过。
- `go test ./...`：通过。
- `go vet ./...`：通过。
- `go test -race ./relay/common ./relay ./relay/channel/openai ./relay/helper`：通过。

## 边界

- 本修复不会主动改写所有请求；只有上游明确拒绝 `input[index].arguments` 字符串类型时才重试一次。
- 只能解析为 JSON 对象的字符串会被转换；数组、普通文本、无效 JSON 字符串不会伪造为对象。
- `namespace` 只移除上游指明的目标历史输入项，不批量删除其他项。
- 本记录为开发环境代码和测试结果；按要求未升级正式服务。正式服务当前仍需在后续明确允许后再检查容器 build-info 与 `/api/status`。
