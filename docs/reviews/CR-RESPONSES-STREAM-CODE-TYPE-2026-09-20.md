# CR: Responses 流事件顶层 code 非字符串时整条事件解析失败

- 日期：2026-09-20
- 范围：`dto/openai_response.go`、`dto/openai_response_test.go`
- 状态：已修复并验证

## 问题

`dto.ResponsesStreamResponse` 承载 `/v1/responses` 的流式事件，同一个结构体既要解析
`response.failed` / `response.error` 的嵌套错误，也要解析协议标准的顶层 `error` 事件
（code/message/param 在事件顶层）。两处的 `code` 类型不一致：

| 位置 | 字段 | 类型 |
| --- | --- | --- |
| 事件顶层 | `ResponsesStreamResponse.Code` | `string`（修复前） |
| 嵌套错误 | `types.OpenAIError.Code` | `any` |

`types.OpenAIError` 用 `any` 是有意为之：`WithOpenAIError` 明确处理非字符串 code

```go
code, ok := openAIError.Code.(string)
if !ok {
    if openAIError.Code != nil {
        code = fmt.Sprintf("%v", openAIError.Code)
    }
}
```

而 `ResponsesStreamResponse.Code` 是 `string`。上游若在顶层 `error` 事件里给出数字或对象
code，整条事件解析失败。

复现（`dto` 包内探针，`common.UnmarshalJsonStr` 到 `ResponsesStreamResponse`）：

| 输入 | 修复前 |
| --- | --- |
| `{"type":"error","code":"rate_limit_exceeded","message":"slow down"}` | 解析成功 |
| `{"type":"error","code":123,"message":"numeric code"}` | **失败**：`json: cannot unmarshal number into Go struct field ResponsesStreamResponse.code of type string` |
| `{"type":"error","code":null,"message":"null code"}` | 解析成功 |
| `{"type":"error","code":{"nested":true},"message":"object code"}` | **失败**：`json: cannot unmarshal object into Go struct field ResponsesStreamResponse.code of type string` |
| `{"type":"response.failed","response":{"error":{"code":429,"message":"boom"}}}` | 解析成功（嵌套路径本就是 `any`） |

即同一个非字符串 code，放在嵌套错误里可以接受，放在顶层就整条事件失败，两处行为不一致。

## 影响范围（已核实，未夸大）

两个解析点都会受影响：

- `relay/channel/openai/relay_responses.go:1256`（Responses 原生流）
- `relay/channel/openai/chat_via_responses.go:692`（Chat 经 Responses 上游）

两者失败后都调用 `sr.Error(err)`。查 `relay/helper/stream_result.go`：

```go
// Error records a soft error. The stream continues processing.
func (r *StreamResult) Error(err error) {
```

`Error()` 只记录软错误、不置 `stopped`，因此**流不会因此中止**：该条事件被丢弃，流继续到
EOF 或超时，最终在 `stream_scanner.go:211` 因 `HasErrors()` 为真而记一条 error 级日志。

后果是错误事件的内容（code 与 message）不会到达客户端，客户端拿到的是"流结束但没有错误
事件"。`code` 非字符串时必然触发。

另有一处会直接吞掉整条错误事件：`relay/channel/openai/relay_responses_error.go` 的
`responsesSSETerminalError` 在解析失败时 `return nil`，即把该事件当作"没有错误"。

## 修复

1. 字段类型放宽为 `any`，与 `types.OpenAIError.Code` 对齐。

```go
Code           any              `json:"code,omitempty"`
```

2. 判空语义随类型调整。原来用 `s.Code != ""`，字段变成 `any` 后不再成立，改为
   `!isEmptyResponsesStreamCode(s.Code)`，并新增该辅助函数，保持与嵌套路径一致的
   "空值视为不存在"语义：

```go
func isEmptyResponsesStreamCode(code any) bool {
	if code == nil {
		return true
	}
	if text, ok := code.(string); ok {
		return text == ""
	}
	return false
}
```

JSON null、空字符串、字段缺失一律视为无 code（与修复前 `string` 零值的表现一致），
数字、对象等真实值则保留并透传进 `openAIError.Code`。

## 验证

命令均在本轮运行并读取输出。

| 命令 | 结果 |
|---|---|
| `go test ./dto/ -count=1` | ok |
| `go test ./relay/... ./dto/... -count=1` | 38 包 ok，0 FAIL |
| `go test ./controller ./model ./relay/common ./relay/helper ./service -count=1` | 全部 ok |
| `go build ./...` | exit 0 |
| `gofmt -l dto/` | 无输出 |

新增回归测试（追加至 `dto/openai_response_test.go`）：

- `TestResponsesStreamResponseUnmarshalsNonStringTopLevelCode`（string / number / object 三个子用例，
  断言解析成功且 `GetOpenAIError().Code` 保留原值）
- `TestResponsesStreamResponseGetOpenAIErrorTreatsNullAndEmptyCodeAsAbsent`（null / 空串 / 缺失，
  断言 `Code` 为 nil）
- `TestResponsesStreamResponseGetOpenAIErrorKeepsCodeOnlyEvent`（只有 code 无 message 时仍产出错误对象）

该文件既有 2 个 `GetOpenAIError` 用例（以字符串字面量构造，`any` 下仍可编译）保持通过。

变异验证（逐个撤掉修复，测试必须失败）：

| 撤掉 | 结果 |
|---|---|
| 字段类型改回 `string` | FAIL（number / object 子用例 + code-only 用例） |
| `isEmptyResponsesStreamCode` 恒返回 false | FAIL（null / 空串子用例，另连带既有用例 `...UsesNestedFieldsWhenTopLevelIsAbsent`） |

第二个变异连带打破既有用例，反证该判空语义确为已有契约的一部分，不是本次新引入的行为。

## 边界

- **未连接真实上游**。覆盖的是反序列化语义与错误提取语义，未覆盖真实上游发出非字符串
  顶层 code 的端到端流。
- 未做浏览器或 SSE 端到端验证。
- 该结构体在转发路径上**不会被重新序列化**：`relay/helper/common.go` 的
  `ResponseChunkData` 直接透传上游原始 `data` 字符串（`sendResponsesStreamData` 同样），
  因此字段类型变化不影响转发出去的字节。序列化只发生在测试里。
- 由此带来一个已核实但当前无实际影响的差异：`omitempty` 对 `any` 只在值为 `nil` 时省略，
  故 `Code` 为空字符串时若被序列化会输出 `"code":""`（修复前省略）。实测：
  `{Code:nil}` -> `{"type":"x"}`，`{Code:""}` -> `{"type":"x","code":""}`。
  由于无线上路径序列化该结构体，仅为记录。
- 未处理 `responsesSSETerminalError` 解析失败时静默 `return nil` 的分支；该分支在字段放宽后
  不再被非字符串 code 触发，其余解析失败原因（如上游发出非 JSON data 行）仍会走该分支。
