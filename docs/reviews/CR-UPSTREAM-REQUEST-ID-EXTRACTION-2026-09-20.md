# CR: 上游 request id 提取的正则误捕与不确定遍历

- 日期：2026-09-20
- 范围：`service/error.go`
- 状态：已修复并验证

## 问题 1：phrase 正则未要求分隔符，把散文单词当成 ID

`upstreamRequestIDPhrasePattern` 原为：

```
(?i)\brequest\s+id\s+([A-Za-z0-9][A-Za-z0-9._:-]{7,127})
```

捕获组前只要求空白，不要求 `:` 或 `=`。兄弟 pattern `upstreamRequestIDTextPattern`
要求 `\s*[:=]\s*`，两者不一致。

复现（修复前）：

| body 中的 message | 捕获结果 |
|---|---|
| `please check your request id parameter setting` | `parameter` |
| `invalid request id value supplied` | `value`（同类误捕） |

即任何紧跟 "request id" 的 8 字符以上单词都会被当成上游请求 ID 写入日志。

修复：与兄弟 pattern 对齐，要求分隔符：

```
(?i)\brequest\s+id\s*[:=]\s*([A-Za-z0-9][A-Za-z0-9._:-]{7,127})
```

实测该改动不影响合法形式：`Request ID: 1a2b3c4d-...` 仍被捕获（此前由 text pattern 捕获），
`request_id: text-body-456` 同样仍被捕获。

## 问题 2：嵌套对象遍历依赖 map 随机顺序

`requestIDFromJSONFields` 用 `for key, raw := range fields` 直接遍历 Go map。当错误体
含多个 request-id 键或多个嵌套对象时，返回哪个 ID 取决于当次 map 迭代顺序。

复现（修复前）：同一 body 连续 60 次，得到 3 种不同结果：

```
{"alpha":{"request_id":"aaaaaaaa"},"beta":{"request_id":"bbbbbbbb"},"gamma":{"request_id":"cccccccc"}}
distinct=3 map[aaaaaaaa:47 bbbbbbbb:6 cccccccc:7]
```

修复：先把键排序再按序遍历，两个循环共用同一份排序键列表。修复后 60 次全部得到
`aaaaaaaa`（字典序最小的键）。

## 验证

| 命令 | 结果 |
|---|---|
| `go test ./service/ -count=1` | ok |
| `go test ./controller ./model ./relay/common ./relay/helper ./service -count=1` | 全部 ok |
| `go build ./...` | exit 0 |
| `gofmt -l`（改动文件） | 无输出 |

新增回归测试（`service/error_test.go`）：

- `TestRelayErrorHandlerWithGinContextRejectsRequestIDProse`：4 条散文 message 均不得被捕获
- `TestRelayErrorHandlerWithGinContextCapturesRequestIDPhraseWithSeparator`：带分隔符的真实短语仍被捕获
- `TestCaptureUpstreamRequestIDIsDeterministicAcrossCalls`：40 次调用结果一致，且为字典序最小键

变异验证（逐个撤掉修复，测试必须失败）：

| 撤掉 | 结果 |
|---|---|
| phrase 正则的分隔符要求 | FAIL |
| 排序遍历（还原 range map） | FAIL（captured ID must not depend on map iteration order） |

## 边界

- 修复只改变提取结果的确定性与精确度，不改变优先级顺序（结构化 JSON 字段优先于文本
  正则，header 优先于 body 由既有测试覆盖）。
- 既有 request id 测试全部保持通过。
