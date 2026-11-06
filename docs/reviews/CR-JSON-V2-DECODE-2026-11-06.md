# JSON v2 解码引擎接入（2026-11-06）

## 结论

`common.Unmarshal`、`common.UnmarshalJsonStr`、`common.DecodeJson` 的解码实现
从 `encoding/json`（v1）切换到 `encoding/json/v2`，覆盖全仓 600 余处业务解码
调用（含 `common.Any2Type` 与 common 内的 JSON 辅助函数）。编码（`common.Marshal`）
保持 v1：v2 编码会改变线上字节且没有性能收益，理由见下文。

实测（darwin/amd64，Go 1.27.3，16 线程，`-benchtime=3s -count=7` 取中位数）：

| 场景 | v1 | v2（本次接入） | 变化 | 分配 |
| --- | --- | --- | --- | --- |
| OpenAI chat 请求体（约 600B，解码为 dto.GeneralOpenAIRequest） | 21.1 us | 19.0 us | -10.0% | 50 -> 50 |
| 流式 usage 帧（约 200B，解码为 dto.Usage） | 3.11 us | 2.53 us | -18.7% | 1 -> 1 |
| 非流式大响应（约 40KB，解码为 dto.TextResponse） | 91.3 us | 62.7 us | -31.3% | 5 -> 5 |
| 大响应透传（约 40KB，解码为 map[string]any） | 97.4 us | 74.9 us | -23.1% | 46 -> 46 |

数字随机器与负载变化（同机重复运行波动约 5-10%），结论以同机 v1/v2 对比为准。
基准常驻在 `common/json_bench_test.go`，可按
`go test ./common -run '^$' -bench 'BenchmarkUnmarshal' -benchmem` 复跑。

## 方案取舍

评估过三条路径，只有第三条可行：

1. 全仓加 `GOEXPERIMENT=jsonv2`，让 v1 API 内部改走 v2 引擎并保留 v1 语义。
   实测反而更慢：chat 请求 18.0us -> 20.4us（+13%），40KB 响应 80us -> 91us
   （+14%）。v1 兼容层在 v2 引擎上的选项开销抵消了收益，因此 Dockerfile 不引入
   该实验开关。
2. 直接用 v2 默认语义。最快（chat -25%，大响应 -33%），但默认拒绝重复键、
   字段名大小写敏感、拒绝非法 UTF-8，会改变网关接受请求与解析上游响应的边界。
3. 直接调用 v2 并只开启必要的 v1 兼容选项（本次采用）：保留重复键后者生效、
   大小写不敏感匹配、非法 UTF-8 替换为 U+FFFD，其余走 v2 快速路径。

最终选项（`common/json.go` 的 `jsonV2UnmarshalOptions`）：

- `jsontext.AllowInvalidUTF8(true)`：上游内容带非法 UTF-8 时继续解码，不整条报错。
- `jsonv2.MatchCaseInsensitiveNames(true)`：客户端发 `Model` 仍命中 `json:"model"`。
- `jsontext.AllowDuplicateNames(true)`：重复键后者生效，与 v1 一致。

## 有意保留与有意放弃的差异

保留（与 v1 一致）：重复键处理、字段名大小写、非法 UTF-8、未知字段忽略、
尾随垃圾报错、`json.RawMessage`/`json.Number` 解码、请求体单值解码语义
（`DecodeJson` 只消费第一个值，顶层值之后的内容不消费也不报错，空输入返回
`io.EOF`）。

放弃（依赖 v1 独有语义的场景，repo 内已确认未使用或影响可接受）：

- 非零目标值的 v1 合并语义（JSON null 保留原标量、切片逐元素合并）。
  常规解码目标都是零值，回读覆盖写场景未发现依赖。
- 宽松 RFC3339 时间解析。DTO 中无 `time.Time` JSON 字段。
- 定长数组任意长度、`,string` 标签、字节数组旧格式。全仓检索无使用。
- v1 错误类型与文案。`SanitizeJSONDecodeError` 已同时支持 v2 的
  `*jsonv2.SemanticError`，继续按 JSON 字段路径输出不含 Go 类型名的消息。

## 编码（Marshal）为什么不变

v2 默认编码的线上字节与 v1 不同，直接切换会改变 API 响应：

- nil 切片编码为 `[]` 而不是 `null`，nil map 编码为 `{}` 而不是 `null`。
- 不再转义 `<`、`>`、`&` 为 `<` 等。
- 默认 `omitempty` 语义不同（例如 `float64` 零值仍会输出 `"score":0`）。

用 `DefaultOptionsV1` 对齐输出则没有性能收益（实测与 v1 持平或略慢）。
因此 `common.Marshal` 继续使用 v1，`common.ValidJson` 同理。

`DecodeJsonUseNumber`、`NewJsonDecoderUseNumber`、`DecodeJsonFromDecoder`
保持 v1：v2 没有 `UseNumber` 等价选项（已在 Go 1.27.3 GOROOT 内确认），
这些调用点是多值流与保精度解析，语义优先。

## 修改点

- `common/json.go`：`Unmarshal`/`UnmarshalJsonStr`/`DecodeJson` 改走 v2；
  `SanitizeJSONDecodeError` 增加 `*jsonv2.SemanticError` 分支与 RFC 6901
  指针转字段路径；新增 `jsonV2UnmarshalOptions`。
- `common/utils.go`：`Any2Type` 的编解码改走 wrapper（解码走 v2）。
- `common/str.go`：`MapToJsonStr`、`StrToJsonArray`、`IsJsonArray`、
  `IsJsonObject`、`GetJsonString` 改走 wrapper。
- `common/json_v2_codec_test.go`：新增引擎探针、v1 宽容语义回归、
  RawMessage/Number 回归、DecodeJson 流式语义回归、v2 错误清洗回归。
- `common/json_decode_error_test.go`：原始错误断言改为引擎无关的 `Go ` 前缀。
- `common/json_bench_test.go`：常驻 v1/v2 对比基准。

## 验证

- `go test ./... -count=1`：退出码 0，68 个包通过，无 FAIL。
- `go test ./common -count=1`：通过。
- 基准数据见上表。

## 未覆盖边界

- 未连接真实 MySQL/PostgreSQL/Redis，未做多实例联调。
- 未对真实上游渠道做端到端压测；解码收益以本机基准为准。
- v1 到 v2 的合并语义差异仅通过全量测试覆盖，未逐个调用点静态审计。
- `encoding/json` 的 `GOEXPERIMENT=jsonv2` 构建开关未启用，构建配置无改动。
