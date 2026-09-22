# CR: Claude 渠道 Anthropic 原生认证头补齐

- 日期：2026-09-22
- 范围：`relay/channel/claude/adaptor.go`、`relay/channel/claude/adaptor_auth_header_test.go`
- 状态：代码修复完成，单元测试与真实上游 A/B 通过；生产渠道配置未改动

## 缺陷

正式渠道 188（`mass.qdama.cn`）走 `/v1/messages` 的模型测试返回
`bad response status code 401, body:`（空 body），但同一组 URL / key / body
在 Postman 里返回 200。

根因在出站认证头，不在请求体或协议转换：

- 上游 `mass.qdama.cn` 的 `/v1/messages` **只接受 `Authorization: Bearer`**，
  不接受 `x-api-key`。
- 本仓库 Claude 渠道（type=14）的 `SetupRequestHeader` **只发送 legacy 的
  `x-api-key`**，从不发送 `Authorization`。

```go
// relay/channel/claude/adaptor.go:84（修复前）
func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("x-api-key", info.ApiKey)
	anthropicVersion := c.Request.Header.Get("anthropic-version")
	if anthropicVersion == "" {
		anthropicVersion = "2023-06-01"
	}
	req.Set("anthropic-version", anthropicVersion)
	CommonClaudeHeadersOperation(c, req, info)
	return nil
}
```

Postman 能通，是因为 Postman 的 Authorization 页签填的是 key，实际发出的是
`Authorization: Bearer`；把同一个 key 改填到 `x-api-key` 同样会 401。

### 差分探测证据（对上游直连）

用渠道 188 的真实 key 直连 `https://mass.qdama.cn/v1/messages`，同一份请求体：

| 请求头 | 结果 |
| --- | --- |
| `Authorization: Bearer <key>` | HTTP 200 |
| `x-api-key: <key>` | HTTP 401 |
| 两者同时带，Authorization 正确 | HTTP 200 |
| 两者同时带，Authorization 错误 | HTTP 401 |

同一 key 在 `/v1/chat/completions` 上：`Authorization: Bearer` → 200，
`x-api-key` → 401。即上游按**端点**而非按 key 类型选择认证头，`/v1/messages`
上 `Authorization` 是唯一被接受的凭证。

401 响应头显示 `www-authenticate: Key realm=MSE Gateway`、
`server: istio-envoy`、`content-length: 0`，是网关层拒绝。

### 为什么此前未暴露

渠道 188 于 2026-09-22 11:52:56 被改为 type=14（Anthropic 原生）。日志时间线：

```text
11:52:27  testing channel 188  RelayFormat: openai_responses  → 200
11:52:36  testing channel 188  RelayFormat: openai            → 200
11:52:56  PUT /api/channel/                                   （改为 type=14）
11:53:18  testing channel 188  RelayFormat: claude  endpoint_type=anthropic
11:53:18  channel test bad response: channel_id=188 type=14 status=401
```

该渠道历史上从未有过成功的 `/v1/messages` 调用（09-18 的 188 流量全部是
`/v1/responses`）。走 OpenAI 兼容路径（发 `Authorization: Bearer`）一直正常，
改成本地 Claude 原生路径后才暴露上游认证差异。

## 上游认证事实

Anthropic 官方文档
（`https://platform.claude.com/docs/en/api/overview`，Authentication 表）：

| Header | Value | Required |
| --- | --- | --- |
| `Authorization` | `Bearer <token>`, where `<token>` is your API key or a short-lived access token… | Yes, unless `x-api-key` is set |
| `x-api-key` | Your API key from Console. Legacy fallback for `Authorization`, still supported | No |

`https://platform.claude.com/docs/en/manage-claude/authentication` 另有：

> Send it as `Authorization: Bearer <key>` on direct HTTP requests
> The legacy `x-api-key: YOUR_API_KEY` header is still supported in place of `Authorization`.

即：`Authorization: Bearer` 是正规头，`x-api-key` 是官方标记为 legacy 的兼容写法。

### 本仓库内部的不一致

本仓库会向 Anthropic 原生 `/v1/messages` 发起请求的适配器共 8 个，认证头如下
（`grep` 各适配器 `SetupRequestHeader` 的 `Set("Authorization"` / `Set("x-api-key"`）：

| 适配器 | 认证头（修复前） |
| --- | --- |
| `relay/channel/claude/adaptor.go:85` | `x-api-key` |
| `relay/channel/ali/adaptor.go` | `Authorization: Bearer` |
| `relay/channel/aws/adaptor.go` | `Authorization: Bearer`（SigV4 派生凭证） |
| `relay/channel/deepseek/adaptor.go` | `Authorization: Bearer` |
| `relay/channel/minimax/adaptor.go` | `Authorization: Bearer` |
| `relay/channel/moonshot/adaptor.go` | `Authorization: Bearer` |
| `relay/channel/volcengine/adaptor.go` | `Authorization: Bearer` |
| `relay/channel/zhipu_4v/adaptor.go` | `Authorization: Bearer` |

即 7 个发 `Authorization: Bearer`，`claude` 是唯一只发 legacy 头的孤例。

（`relay/channel/openai/adaptor.go` 也含 `/v1/messages` 字样，但它把 Claude
请求转换成 chat/completions 后发往 OpenAI 兼容端点，不走 Anthropic 原生协议，
其 `Authorization: Bearer` 用于 OpenAI 语义，不属本表。）

### 上游同问题的处置状态

- 上游 issue **#3420**「X-Api-Key参数导致的Claude401问题」（2026-03-24 开，
  2026-06-16 关）：报的正是出站方向，标签 `bug` + `needs reproduction`，
  **无对应修复 commit**。维护者回复说的是入站方向（new-api 作为服务端接受
  客户端用 x-api-key 调用），不是本问题。
- 上游 PR **#2436** 修的也是入站：让 `TokenAuth()` 对 `/v1/models` 也认 `x-api-key`。
- 上游 issue **#5491**（vLLM 0.23 的 Anthropic 端点只认 Bearer）是同一问题的
  另一实例，维护者 `seefs001` 的处方是逐渠道
  `header_override: {"Authorization": "Bearer {api_key}"}`，并说明
  「vllm 的 Claude 端点和 Anthropic 官方的鉴权方式不一致」。
- 上游至今（`upstream/main` `9310231b3`）未改 `relay/channel/claude/adaptor.go`
  的认证头；其 `newapi` 适配器发双头，但那是为「转发给另一个 new-api」设计的
  （对端入站中间件会把 `x-api-key` 归一成 `Authorization`），不是给真实上游用的模式。

## 修复

`relay/channel/claude/adaptor.go` 的 `SetupRequestHeader` 同时发送两个头：

```go
	// The Messages API documents x-api-key as a legacy fallback for
	// Authorization: Bearer. Third-party Anthropic-compatible gateways often
	// implement only the bearer form, so emit both. Both values are derived
	// from the same channel key, so the authenticated principal cannot depend
	// on which header an upstream reads.
	req.Set("x-api-key", info.ApiKey)
	req.Set("Authorization", "Bearer "+info.ApiKey)
```

设计要点：

- **同源同值**。两个头都由 `info.ApiKey` 在同一函数内派生，不可能出现
  「Bearer 指向 A、x-api-key 指向 B」的凭证错配。这是本方案与「双头」风险
  讨论的分界点：真正的风险是两个头携带不同凭证，而非头本身重复。
- **`header_override` 仍可覆盖**。`DoApiRequest` 在 `SetupRequestHeader`
  之后调用 `processHeaderOverride` + `applyHeaderOverrideToRequest`
  （`relay/channel/api_request.go:700`，注释「确保用户设置优先级最高」），
  后者用 `req.Header.Set` 写入，因此运维配置的 `header_override` 仍然胜出。
  已有 `header_override: {"Authorization": "Bearer {api_key}"}` 的渠道不受影响。
- **客户端传入的 `Authorization` 会被覆盖**。`req.Set` 无条件写入，客户端
  无法借 `Authorization` 头把自己伪装成别的凭证。已加测试锁定。

## 验证

### 静态检查与单元测试

```bash
go build ./...
go vet ./relay/channel/claude/
go test ./relay/channel/claude/ -v -run TestSetupRequestHeader
go test ./relay/... ./controller/ ./service/
```

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | exit 0 |
| `go vet ./relay/channel/claude/` | exit 0 |
| `go test ./relay/channel/claude/` | ok（含 5 个新增测试） |
| `go test ./relay/... ./controller/ ./service/` | 全部 ok，无失败 |

新增 `relay/channel/claude/adaptor_auth_header_test.go`，5 个测试：

| 测试 | 断言 |
| --- | --- |
| `TestSetupRequestHeaderSendsBothAnthropicAuthHeaders` | 两个头都发，值正确，`anthropic-version` 默认 `2023-06-01` |
| `TestSetupRequestHeaderAnthropicAuthHeadersAgree` | `Authorization` 恰好一个值且等于 `Bearer ` + `x-api-key` |
| `TestSetupRequestHeaderKeepsExplicitAnthropicVersion` | 调用方传入的 `anthropic-version` 仍优先 |
| `TestSetupRequestHeaderOverridesCallerSuppliedAuthorization` | 调用方传入的 `Authorization` 被渠道 key 覆盖 |
| `TestSetupRequestHeaderHandlesEmptyChannelKey` | 空 key 下两个头仍存在且不产生非法头值 |

反向验证：删掉 `req.Set("Authorization", ...)` 一行后，5 个测试全部 FAIL；
恢复后全部 PASS。

### 官方 API 认证头行为探测

无可用官方凭证，改用「格式合法但无效」的 key，靠报错信息判断服务端实际
读取哪个头（不同认证分支返回不同文案）：

| 发出的头 | 实际返回 |
| --- | --- |
| `x-api-key` = 格式合法 | `API key is invalid.`，`request_id: null` |
| `Authorization: Bearer` = 同值 | `invalid x-api-key`，`request_id: req_011CfJ1NK3UPb7N7bddCHAqk` |
| **两者同值** | **`API key is invalid.`，`request_id: null`** |
| `x-api-key` 合法 + `Authorization` 垃圾 | `API key is invalid.`，`request_id: null` |
| `x-api-key` 垃圾 + `Authorization` 合法 | `invalid x-api-key`，`request_id: req_011CfJ1NQLiW2A5B3JgsDcWe` |
| 不带认证头 | `x-api-key header is required` |

结论：`x-api-key` 存在时服务端总是读它（第 4、5 行证明），`Authorization` 被忽略；
两者同值时的响应与只发 `x-api-key` **逐字节相同（含 `request_id: null`）**，
说明走同一条代码路径，不存在「双凭证冲突」的独立拒绝分支。

### 隔离环境端到端（实际出站头抓包）

用本地回显服务器接收请求并记录真实出站头。渠道为临时 type=14，
**未配置任何 `header_override`**：

```text
path: /v1/messages
  Anthropic-Version: 2023-06-01
  Authorization: Bearer PROBE_CHANNEL_KEY
  Content-Type: application/json
  User-Agent: claude-cli/2.1.278 (external, sdk-cli)
  X-Api-Key: PROBE_CHANNEL_KEY
```

修复前同一配置的出站头只有 `X-Api-Key`，无 `Authorization`。

### 真实上游 A/B

同一临时渠道、同一 key、无 `header_override`，指向 `https://mass.qdama.cn`，
仅切换镜像：

| 镜像 | 模型测试结果 |
| --- | --- |
| 修复前（`new-api-local:dev`，仅 `x-api-key`） | `bad response status code 401, body:` |
| 修复后（`new-api-local:dev`，双头） | `bad response status code 429, body: quota exceeded` |

429 是**认证已通过**的证据，不是失败：直连上游同一 key 走
`Authorization: Bearer` 得 429 `quota exceeded`（已认证、额度用尽），
走 `x-api-key` 得 401。401 → 429 的变化即认证从拒绝转为通过。

### 版本对齐

```bash
scripts/build-docker-local.sh new-api-local:dev
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
```

```text
commit=dfe176c64e3ec288631f0c242f014f293f652dad-dirty
```

`-dirty` 来自未提交的工作区改动（含本次修复），`commit` 与 `git HEAD` 一致。
正式环境全程未重建、未重启、未写入。

## 发现的第二处同类缺陷（本轮不修，另开任务）

影响面排查时发现：`relay/channel/claude/adaptor.go` 的 `SetupRequestHeader`
**不是**唯一构造 Claude 出站认证头的地方。

`controller/channel-billing.go:150` 有独立实现：

```go
// GetClaudeAuthHeader get claude auth header
func GetClaudeAuthHeader(token string) http.Header {
	h := http.Header{}
	h.Add("x-api-key", token)
	h.Add("anthropic-version", "2023-06-01")
	return h
}
```

它由 `controller/channel.go:206 buildFetchModelsHeaders` 在
`channel.Type == constant.ChannelTypeAnthropic` 时选用，服务于
`GET /api/channel/fetch_models/:id`（「获取模型列表」按钮，
`router/api-router.go:271` → `controller.FetchUpstreamModels`）。该路径不经过
adaptor，因此**本轮修复对它是零影响**。

### 实证（隔离环境回显服务器抓包）

临时 type=14 渠道指向本地回显服务器，调用
`GET /api/channel/fetch_models/<id>`，抓到的真实出站头：

```text
GET /v1/models
  Anthropic-Version: 2023-06-01
  X-Api-Key: PROBE_KEY
  User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) ...
```

**只有 `X-Api-Key`，无 `Authorization`** —— 与 adaptor 修复前的形态一致。

### 影响与建议

对只接受 `Authorization: Bearer` 的第三方 Anthropic 兼容网关（如
`mass.qdama.cn`、vLLM 0.23），渠道模型测试已修复，但「获取模型列表」仍会
因 401 失败。`GetClaudeAuthHeader` 当前无任何测试覆盖
（`grep GetClaudeAuthHeader --include='*_test.go'` 零命中）。

建议后续单独任务处理，方案与本次一致：同时发送两个头，同源同值；或直接复用
`GetAuthHeader` 的 `Authorization: Bearer` 再补 `x-api-key`。需同时补单测。

本轮按 `AGENTS.md` §1「每次只处理一个原子任务」只提交 adaptor 修复，不扩大范围。

## 未覆盖与边界

- **未在真实官方 Anthropic 账号跑通 200**。手头无可用凭证
  （`ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` 均未设置，`ant` 未安装）。
  本文证明的是：官方 API 接受双头、且 `x-api-key` 优先（用无效 key 的报错路径
  证明），以及双头能通过第三方网关认证。**「官方账号下双头返回 200」是从
  「响应逐字节相同、同一代码路径」推出的，不是实测。**
- **`x-api-key` 优先这一行为未写入任何官方文档**。它是从报错信息反推的，
  可能无预告变更。因此代码注释只写文档明确支持的表述（legacy fallback），
  未把反推出的优先级当作契约。
- **未覆盖 `web/classic` 前端**。本改动无前端面；`web/default` 中
  `x-api-key` / `Authorization` 的既有出现均在示例文案、透传模板和
  param_override 编辑器内，与本改动无关。
- **生态惯例存在分歧**。本轮调研的 15 个网关中 14 个对 Anthropic 原生端点
  只发一个认证头；需要 Bearer 时是「切换」而非「追加」（LiteLLM 用
  `use_bearer_for_custom_base`）。本仓库刻意选择「双头」，理由是它对任何
  只认 Bearer 的上游自动生效、无需运维逐渠道配置，且实测在官方 API 上无害。
  若未来出现同时拒绝两个头的上游，需重新评估是否改为渠道级开关。
- **未新增渠道级认证头开关**。若上游同时拒绝双头，届时再单独设计。
- **未改动正式渠道 188 的配置**。用户已将其改回 type=1（OpenAI 兼容路径），
  实测正常中继 `deepseek-v4-flash`；本改动对它是零影响。
- **渠道 key 仅经 shell 变量传递**，未写入本报告或仓库。
- 隔离环境探针渠道（id 9232、9233、9234）已删除，回显服务器已停止，临时文件与
  `new-api-local:dev-apitest` 镜像已清理。
