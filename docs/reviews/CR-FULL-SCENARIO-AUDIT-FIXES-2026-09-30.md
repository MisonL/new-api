# 全场景审查发现修复记录（2026-09-30）

对应审查：`docs/reviews/CR-FULL-SCENARIO-AUDIT-2026-09-29.md`。
本轮把该审查确认的 A/B/C 三档发现全部落到代码，未提交。

- 采样时间：2026-09-29 19:20 - 19:56（镜像构建时间戳，UTC）
- 运行环境：完全隔离开发环境，容器 `new-api-dev-isolated-new-api-1`，API `127.0.0.1:3001`
- 代码基线：本轮开始时 `HEAD` = `b3c615b3ede7c3876e831bf06062202c2fb24eff`；收尾复核时已被其它会话推进到
  `604c1e6b631e6c896b615d8beb3907c7718c000b`（工作区始终为 dirty，含其它会话的未提交改动）
- 镜像：`new-api-local:dev`，最后一次构建的 `--build-info` 显示
  `commit=604c1e6b631e6c896b615d8beb3907c7718c000b-dirty`（时间戳 2026-09-29T23:26:45Z）
- 生产环境**只读**，未请求 13000 或 3000 端口；未改动生产库

## 一、修复清单

### A 组 — 低风险确定性修复

| # | 问题 | 落点 | 做法 |
| --- | --- | --- | --- |
| A1 | `n>1` 被静默丢弃（chat -> claude） | `relay/channel/claude/relay-claude.go` | `RequestOpenAI2ClaudeMessage` 对 `N>1` 返回 `n>1 is not supported by the Claude Messages API`，与 aws/cohere/baidu/cloudflare 同形。**未动原生 OpenAI 路径**：上游本身只回 1 个 choice，网关无法凭空补出 |
| A2 | `temperature` 无范围校验 | `relay/helper/valid_request.go` | 新增 `validateTemperatureRange`（`[0,2]`，含 NaN），接在文本请求、`GetAndValidateResponsesRequest`、`GetAndValidateResponsesCompactionRequest` 三处 |
| A3 | `ToClaudeError` 输出字面量 `<nil>` | `types/error.go` | `ErrorTypeOpenAIError` 分支的 `fmt.Sprintf("%v", openAIError.Code)` 在 `Code` 为 nil 时回退到 `string(e.errorCode)` |
| A4 | `MaskSensitiveInfo` 把 `token quota` 掩码成 `token ***` | `common/str.go` | 扩 `isAuthSchemeProseWord` allowlist，加入 `quota`、`remain`、`balance`、`limit` |
| A5 | CORS `Allow-Origin: *` 配 `Allow-Credentials: true` | `middleware/cors.go` | 删除 `config.AllowCredentials = true`；`gin-contrib/cors` 在 `AllowAllOrigins` 下硬编码 `*`，两者同真是规范禁止组合 |
| A6 | 429 无 `Retry-After`、无响应体 | `middleware/utils.go` + 5 处限流中间件 | 新增 `abortRateLimited`，写 `Retry-After`（窗口时长，秒）与 JSON 错误体；刻意不走 `abortWithOpenAiMessage` 以免每次拒绝都写错误日志 |
| A7 | SIGTERM 前不 flush，配额丢失 | `main.go` + `model/utils.go` | 导出幂等 `model.FlushBatchUpdates()`，在 `srv.Shutdown(ctx)` 之后调用。生产 `BATCH_UPDATE_ENABLED=true`（已用只读 `docker inspect` 确认），本条有真实影响 |
| A8 | `"Invalid request: Invalid request: EOF"` | `middleware/distributor.go` | 去掉 `getModelFromRequest` 内部两层包装，保留 `Distribute` 那一层 |

### B 组 — 协议与错误体修复

| # | 问题 | 落点 | 做法 |
| --- | --- | --- | --- |
| B1 | `GET /v1/models/{model}` 对可路由模型返回 200 + 假消息 | `controller/model.go` | 判定改为"可路由集合"，未命中返回 404 `invalid_request_error` / `model_not_found` |
| B2 | 未知模型 503 而非 404 | `middleware/distributor.go` | 无可用渠道对客户端是 404 `model_not_found`；保留 `(distributor)` 后缀。**钉渠道的 compact 模式分支保持 503**（那是渠道能力拒绝，不是路由缺失） |
| B3 | SSE 头配裸 JSON 正文 | `controller/relay.go` + `relay/helper/common.go` | 新增 `helper.ResetEventStreamHeaders`，在 `writeRelayErrorResponse` 里 `!c.Writer.Written()` 时显式 `Del` SSE 头。gin `v1.9.1` 的 `render.writeContentType` 只在 header 为空时才写，必须显式删 |
| B4 | `/v1/completions` 404 配内部哨兵 | `service/error.go` + `types/error.go` | 纯文本上游 body 的兜底 message 改用已算好的 `statusMessage` |
| B5 | `/v1/embeddings` 透传上游 SDK 异常串 | `service/error.go` | 透传前对 `oaiError.Type` 做词表归一。**未改 `message` 文本**：`***` 前缀来自 `common.MaskSensitiveInfo` 的域掩码，单独留作已知项 |
| B6 | 错误 `type` 词表不统一 | `types/error.go`、`middleware/utils.go`、`controller/relay.go`、`service/error.go` | 见下节"B6 的完整覆盖" |
| B7 | failover 日志渠道归因错位 | `controller/relay.go` + `controller/relay_responses_routing_trace.go` | 新增尝试级 context key，`responsesRoutingAttributedChannelID` 在无 trace 时优先读它 |
| B8 | 确定性上游 404 被改写成 500 | `controller/relay.go` | `shouldRetry` 对 `isDeterministicModelNotFoundError` 直接 `return false`，从源头消除重试到预算耗尽后丢原始错误的路径 |
| B9 | 错误体泄漏 Go 结构体名 | `common/gin.go` | 新增 `SanitizeJSONDecodeError`，用 `errors.As` 抓 `*json.UnmarshalTypeError` 改写成 `field <name> must be a <type>` |

### B6 的完整覆盖（本轮现场补齐）

原方案只改了 `InitOpenAIError` / `NewOpenAIError` / `ToOpenAIError` 的 `default` 分支，
线上复核发现 `type: "new_api_error"` 仍然到达客户端。定位到四个生产者，本轮全部补齐：

| 生产者 | 原输出 | 现输出 |
| --- | --- | --- |
| `NewErrorWithStatusCode`（经 `ToOpenAIError` default 分支） | `new_api_error` | `standardErrorType(errorCode, statusCode)` |
| `middleware.abortWithOpenAiMessage`（分发、鉴权、限流等大量拒绝路径） | `new_api_error` | 同上 |
| `middleware.abortRateLimited` | `new_api_error` | `rate_limit_error` |
| `controller.RelayNotImplemented` | `new_api_error` | `server_error` |
| `service.ClaudeErrorWrapper`（Anthropic 面） | `new_api_error` | `api_error` |

`code` 字段一律不动，它是本仓库的稳定契约。

### C 组 — 需产品决策

| # | 决策 | 做法 |
| --- | --- | --- |
| C1 | codex 动态头守卫让渠道对非 Codex 客户端整条判死 | 渠道级 opt-out。`dto.ChannelOtherSettings` 新增 `AllowMissingCodexDynamicRequestHeaders *bool`（`omitempty`），`nil`/`false` 保持严格现状，只有显式 `true` 才跳过**路由预检**。请求路径上的运行时透传校验不受影响，Codex 客户端自身元数据无法透传时仍会在那里失败。前端在渠道抽屉的 Override Rules 区加开关 |
| C2 | 同一模型跨渠道时 Responses 面 `usage` 结构漂移 | 只补缺失字段，不改已有数值。非流式在写客户端前补齐 `output_tokens_details` / `input_tokens_details`；`usage` 不是对象时原样返回。**覆盖三条非流式 Responses 路径**：原生透传（`OaiResponsesHandler`）、原生 compact（`OaiResponsesCompactionHandler`）、以及 chat->Responses 桥接的两个 usage 构造点（`buildResponsesUsageFromChat`、`buildResponsesCompatUsage`）。取值优先用上游自己的 chat 拼写（`completion_tokens_details` / `prompt_tokens_details`），缺失时才落零值。流式原生透传在 `sendResponsesStreamData` 写出前做同一归一化（收尾轮补齐） |
| C3 | `/api/channel/` 向任意 admin 明文返回 `settings` | 本轮不改，仅记录 |

## 二、验证命令与结果

### 收尾轮后端全量（2026-09-30 追加）

```
go test ./... -count=1 > /tmp/gotest-full.txt 2>&1; echo "EXIT=$?"
-> EXIT=0
-> ok packages: 63
-> FAIL lines: (无)
-> other non-ok: (无)
```

统计直接取自该文件（`grep -c '^ok'` / `grep -c '^FAIL'` / `grep -Ev '^ok|no test files'`），
不是管道退出码。按临时文件约束，输出文件已在收尾时删除；上述数字是运行时读取的输出。
`/v1/models` 的分组收敛改动落地后重跑了一次，结果相同（`EXIT=0`、63 包、0 FAIL）。

### 后端测试

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service ./middleware ./common ./types ./dto ./relay/channel/openai ./relay/channel/claude -count=1
```

退出码 0：

```
ok  controller 5.317s      ok  model 3.069s              ok  relay/common 0.093s
ok  relay/helper 17.749s   ok  service 16.851s           ok  middleware 2.488s
ok  common 0.243s          ok  types 0.061s              ok  dto 0.107s
ok  relay/channel/openai 1.162s                          ok  relay/channel/claude 0.058s
```

### 前端

```bash
cd web/default && bun run lint
cd web/default && bun run build
cd web/default && bun run typecheck
cd web/default && bun test src/features/channels/lib/channel-form.test.ts
```

收尾轮重跑：`bun run lint`（`eslint .`）无输出、退出码 0；
`bun test src/features/channels/lib/channel-form.test.ts` 13 pass / 0 fail（bun 1.4.0）；
`bun run build` 通过（见 6.8）；
`bun run typecheck`（`tsc -b`）另起进程重跑，本轮**未取得退出码**（进程长时间未结束即被
终止），因此不宣称通过。C1 的前端序列化另有反向验证：把 `channel-form.ts`
里的 `allow_missing_codex_dynamic_request_headers` 写入改回无条件 `delete`，
新增用例立刻失败（`ERR_ASSERTION`）。

### 反向验证（还原修复后测试必须失败）

| 修复 | 反向手段 | 结果 |
| --- | --- | --- |
| A1 | 移除 `N>1` 拒绝 | FAIL |
| A2 | 移除文本面范围校验 | FAIL |
| A2 | 移除 responses 面范围校验 | FAIL |
| A5 | 恢复 `AllowCredentials = true` | FAIL |
| A6 | 退回裸 429 | 编译失败 |
| A7 | `FlushBatchUpdates` 改空实现 | FAIL |
| A8 | `getModelFromRequest` 恢复包装 | FAIL |
| B2 | 恢复无条件 503 | FAIL |
| B4/B5 | 停止上游结构化路径归一 | FAIL |
| B6 | `InitOpenAIError` 写原始 code 到 Type | FAIL |
| B6 | `abortWithOpenAiMessage` 退回字面量 | FAIL |
| C1 | 停用渠道级 opt-out | FAIL（`Should be empty, but was [x-codex-*]`） |
| C2 | 短路归一化 | FAIL（9 个用例） |
| C1 前端 | 序列化改回无条件 `delete` | FAIL（`ERR_ASSERTION`） |
| C2 计费不变量 | 改写后重新 `Unmarshal` 回响应结构 | FAIL（`ReasoningTokens` 期望 10 实得 0） |
| C2 chat 拼写兜底 | 去掉 `fallbackKeys` | FAIL |
| C2 compact 路径 | 短路 compact 的归一化 | FAIL |

### 隔离环境实测（`127.0.0.1:3001`）

镜像 `commit=b3c615b3ede7c3876e831bf06062202c2fb24eff-dirty`。

| 编号 | 探针 | 修复前 | 修复后 |
| --- | --- | --- | --- |
| A2 | `temperature` `-1` / `3` / `100` | 200 通过 | 400 `temperature is invalid` |
| A2 | `temperature` `0` / `2` | 200 | 200（边界不放宽也不收紧） |
| A5 | `OPTIONS /api/status` 带 `Origin` | `Allow-Origin: *` + `Allow-Credentials: true` | 仅 `Allow-Origin: *` |
| A6 | 触发 429 | `Content-Length: 0`，无限流元数据 | `Retry-After: 180` + `{"error":{"code":"","message":"rate limit exceeded, please retry later","type":"rate_limit_error"}}` |
| A8 | 空 body | `Invalid request: Invalid request: EOF` | `Invalid request: EOF`（前缀一次） |
| B1 | `GET /v1/models/no-such-model-xyz` | 200 + "does not exist" | 404 `invalid_request_error` / `model_not_found` |
| B1 | `GET /v1/models/gpt-5.6-luna` | 200 | 200（可路由模型不受影响） |
| B2 | 未知模型 `POST /v1/chat/completions` | 503 `new_api_error` | 404 `not_found_error` |
| B4 | 未知模型 `POST /v1/completions` | 404 `openai_error` / `bad_response_status_code` | 404 `not_found_error` / `model_not_found` |
| B6 | 校验失败（`max_tokens:"abc"`） | `type: new_api_error` | `type: invalid_request_error` |
| B6 | 429 | `type: new_api_error` | `type: rate_limit_error` |
| C1 | 渠道 209 加 opt-out，请求不带 `x-codex-*` | 503 `requires unconditional pass_headers ... x-codex-*` | 200 |
| C2 | 渠道 209 `POST /v1/responses`（非流式） | `usage` 仅 `[input_tokens, output_tokens, total_tokens]` | 补齐 `output_tokens_details` / `input_tokens_details` |

C2 的覆盖缺口是在收尾复查时发现的：首版只改了原生透传一条路径。补齐过程中另外发现
chat->Responses 的两个 usage 构造点（`service/openaicompat/chat_to_responses_response.go`
与 `relay/channel/openai/responses_via_chat.go`）**本来就只补 `input_tokens_details`、
不补 `output_tokens_details`**，即桥接路径的漂移是既有缺陷，与 C2 无关但同属一条不变量。

C2 的反向取证：把归一化短路后重建镜像，同一请求返回的上游原始 `usage` 只有三个键，
两个 details 对象均缺失；恢复修复后重新出现。这是本轮唯一一次对 C2 漂移的**直接观测**。

## 三、对审查报告 `CR-FULL-SCENARIO-AUDIT-2026-09-29.md` 的更正

1. **`1.5` 的 B3 触发条件写宽了**。未知模型（`no-such-model-xyz`）在 `stream:true` 下返回的
   `Content-Type` 是正确的 `application/json`；只有**模型存在、上游对 `/v1/messages` 返回 4xx**
   （`gpt-image-2`）时才复现 SSE 头配 JSON 正文。
2. **`1.11` 的 `temperature` 表述不成立**。`3` 未复现（客户端超时，`HTTP 000`）。
   `-1` 与 `100` 通过为真，范围校验本身仍然需要做。
3. **`1.11` 对 `<nil>` 的归因写错了分支**。产生 `<nil>` 的是 `ErrorTypeOpenAIError` 分支的
   `fmt.Sprintf("%v", openAIError.Code)`（`types/error.go:320`）；`new_api_error` 分支永远产出
   `"new_api_error"`。
4. **`1.5` 引用的 gin 版本应为 `v1.9.1`**（`go.mod:20`），不是 `v1.10.0`。

## 四、行为变更（需要知晓）

- **C2 使非流式 Responses 正文不再与上游逐字节相同**。此前该路径以"逐字节透传"为契约，
  有 6 个测试据此断言；现在 `usage` 对象会被补齐两个 details 字段，其余字节（含缩进与
  未建模字段）仍原样保留。6 个既有测试的期望值已相应更新，另加一个独立拼出的期望字面量
  （不由被测代码生成，避免自证）。
- **C2 首版引入了一个计费回归，本轮已修**。首版在改写正文后重新 `common.Unmarshal` 回
  `responsesResponse`，于是插入的零值 `output_tokens_details` 遮蔽了上游自己的兜底字段：
  `usage` 只带 `completion_tokens_details.reasoning_tokens` 而不带 `output_tokens_details` 时，
  计费走 `responsesUsageOutputDetails` 的兜底分支，重新解码会把 `ReasoningTokens` 从
  实测的 10 覆盖成 0。现在改写后的字节**只发给客户端**，计费继续读上游原始 usage，
  新增 `TestOaiResponsesHandlerKeepsBillingUsageFromUpstreamBody` 锁定该不变量
  （反向验证：重新加回 `Unmarshal` 即失败）。
- **B6 使大量既有错误响应的 `type` 值变化**。`code` 不变，`message` 不变，HTTP 状态码不变。
  全仓测试断言 `"new_api_error"` 与 `"bad_response_status_code"` 各 0 处，
  `web/default` 与 `web/classic` 引用 `new_api_error` 各 0 处，改动面已确认。
- **C1 默认不改变任何行为**：`nil` 与显式 `false` 都保持严格现状，只有显式 `true` 才放宽。

## 五、未能覆盖的边界（不当作已通过）

- **C2 现已覆盖流式原生透传**。`relay/channel/openai/helper.go` 的
  `sendResponsesStreamData` 在写出前对 `response.usage` 做同一归一化，新增
  `TestOaiResponsesStreamHandlerFillsTerminalUsageDetailObjects` 覆盖
  `response.completed` 事件。**仍未覆盖的是流式下的真实上游漂移样本**（见下条）。
- **C2 覆盖了四条路径但只对两条做了端到端断言**：原生透传与原生 compact 有 handler 级用例，
  两个桥接构造点只有函数级用例加一个 `OaiChatToResponsesHandler` 的 handler 级用例；
  流式桥接的 `response.completed.usage` 未单独断言。
- **C2 的"计费不受影响"只在单元测试层面证明**：新增用例覆盖了 `reasoning_tokens` 的兜底分支，
  但未在隔离环境用真实上游复现"只报 `completion_tokens_details` 不报 `output_tokens_details`"
  的响应体，也未核对该场景下 `logs` 落库的 token 明细。
- **C2 的真实漂移源未确证**。审查记录的漂移是渠道 223 与 209 之间的推断；
  本轮在隔离环境实测到 209 确实缺两个 details 对象，但 223 在隔离环境请求失败
  （`do request failed`），未取得 223 的对照样本。生产上 grok 系只有 223 一个渠道，
  所以"生产上是否真的会漂移"仍是 SUSPECTED。
- **A7 只覆盖 SIGTERM/SIGINT，SIGKILL 仍然丢失**。`gopool` 没有 `Wait`
  （`common/gopool.go` 只有 `RelayCtxGo`），无法等待在途 goroutine。默认
  `SHUTDOWN_TIMEOUT_SECONDS=120`，compose stop 宽限 10 秒时仍可能走 `SIGKILL`。
  收尾轮已把这条边界从"推断"变成**实测**，见 6.8。
- ~~**B3 只在单元测试覆盖**。隔离环境未构造"模型存在 + 上游对 `/v1/messages` 返回 4xx + `stream:true`"
  的组合请求，未做线上复现。~~ 收尾轮已用可控上游夹具做出**双向实测**，见 6.10。
- **B5 的 `***` 前缀未修，且已决定不修**。`***.ServiceUnavailableError` 来自
  `common.MaskSensitiveInfo` 对两段点分 token 的域掩码。理由与探针输出见 6.6。
- **C3 未改**，见上表。
- **三库兼容差异未复测**。C1 新增的是 `*bool` + `omitempty` 字段，C2 的改写只依赖
  gjson/sjson 对原始 JSON 字节的操作，两者都不依赖数据库方言；但本轮未在 MySQL/PostgreSQL
  上重跑。
- **前端只做了 lint 与 `channel-form.test.ts` 单元测试**，未走浏览器点击路径，
  未做亮/暗主题与移动端检查。C1 新增的开关在抽屉 Override Rules 区的渲染未做视觉确认。
- **`bun run typecheck` 已取得退出码，此前"卡死"是误判**。收尾轮曾记录为"反复长时间不返回
  （19 分钟墙钟仅 13 秒 CPU）、主线程停在 `uv_fs_read`"，并按未验证处理。末轮实测该结论
  **不成立**：`tsc -b` 会完成，只是慢。详见 6.11。
- **未提交**。工作区仍有其它会话的未提交改动，本轮改动与其混在同一工作区。

## 六、收尾轮：剩余项的调研与处理（2026-09-30 追加）

用户要求把首轮清单里"仍未处理的"逐条调研并处理。以下每条都给出本轮实际运行的命令或
只读查询，以及最终决定。

### 6.1 `/v1/models` 的 `supported_endpoint_types` 未按调用方分组收敛（已修）

`model/pricing.go` 的 `modelSupportEndpointTypes` 全局快照由**所有**启用 ability 构建，
不带分组过滤；`/v1/models` 是分组与令牌作用域的，因此会把只属于其它分组的端点类型
一并告知调用方。新增 `GetModelSupportEndpointTypesForGroups`，按
`abilities.<group> IN (...) AND abilities.model = ?` 反查渠道类型，再经
`common.GetEndpointTypesByChannelType` 求并集；`controller/model.go` 的
`buildOpenAIModel` 在 `len(groups) > 0` 时改用它。

实现过程中发现并修掉自己引入的四个缺陷，每个都有反向验证：

- 查询用 `if err != nil` 判断 `*gorm.DB`，该条件恒为真，结果每次被丢弃并回落到全局快照。
- 查询前未调 `ensureAbilityColumnsInitialized()`，`commonGroupCol` 为空串时 SQL 失败。
  与上一条叠加，使函数在夹具下**恒定返回空**。
- 回落方向本身：查询出错时回落到全局快照，恰好会重新引入这条查找要排除的跨分组端点。
  现改为返回空集合（fail closed）。
- **性能回归**：首版让 `ListModels` 对每个模型各发一次查询（dev 下 64 个模型 / 183 行
  abilities），把原来的内存查表变成了 N 次数据库往返。改为在 `updatePricing` 已有的
  ability 循环里顺手构建 `modelSupportEndpointTypesByGroup`（模型 -> 分组 -> 端点类型），
  查询改成读快照。`/v1/models` 实测 0.015-0.036s（64 个模型）。

改读快照后又暴露出一个测试基础设施问题：定价快照是**进程级**缓存、TTL 一分钟，而
`setupModelListControllerTestDB` 每个用例都新建一个内存库，于是第一个读快照的调用会拿到
**上一个用例**的快照（它的库已经不存在了）。这不是生产缺陷——生产只有一个库——但会让
用例在包内全量运行时失败、单独运行却通过。夹具已加 `model.InvalidatePricingCache()`，
新增的 `model` 侧夹具同样如此；`-shuffle=on` 连跑两轮均通过。

另外，读快照前必须刷新：`GetModelSupportEndpointTypesForGroups` 现在开头就调
`GetPricing()`，否则冷缓存下 `lookupModelMeta` 返回 nil，模型自带的 `endpoints` 声明会被
忽略而错误回落到派生集合。

新索引是在 `updatePricing` 里整体替换的、由多个 goroutine 并发读的 map，因此补了竞态
覆盖。这里有个值得记下的过程问题：`TestPricingCacheReadersRaceWithInvalidate` 原本就在，
我先把它扩到新查询就以为覆盖到了，但**去掉三处锁后它仍然通过**——原测试没装数据库、
夹具也没有 ability 行，`updatePricing` 要么在第一步就返回、要么发布空 map，读者根本碰不到
被填充的 map，等于空转。补上内存库与一行可路由 ability、并把失效循环改成贯穿整个测试期
之后，去掉锁即报 `WARNING: DATA RACE`（`EXIT=1`），恢复后通过；连跑 3 次稳定。

验证：

```
go test ./controller/ -count=1 -run 'TestListModelsScopesEndpointTypesToCallerGroups'
-> ok
go test ./model/ -count=1 -run 'TestGetModelSupportEndpointTypes'
-> ok (含新增的 TestGetModelSupportEndpointTypesForGroupsHonoursDeclaredEndpoints)
go test ./model/ ./controller/ -count=1 -shuffle=on     # 连跑两轮
-> ok github.com/QuantumNous/new-api/model        (两轮)
-> ok github.com/QuantumNous/new-api/controller   (两轮)
go test ./... -count=1; echo "EXIT=$?"
-> EXIT=0, ok=63, fail=0
go test -race ./model/ -count=1 -run TestPricingCacheReadersRaceWithInvalidate   # 连跑 3 次
-> ok (3/3)
```

反向验证（三层）：

1. 把 `Pluck(...).Error` 改回 `Pluck(...)` 后同一用例失败
   （`[]constant.EndpointType{} does not contain "openai"`），恢复后通过。
2. 把分组索引塌缩成单一 `default` 桶（模拟跨分组泄漏）后，
   `TestListModelsScopesEndpointTypesToCallerGroups` 失败
   （`[]constant.EndpointType{"openai","anthropic"} should not contain "anthropic"`）；
   去掉"声明优先"分支后
   `TestGetModelSupportEndpointTypesForGroupsHonoursDeclaredEndpoints` 失败。两者恢复后通过。
2. 隔离环境端到端。dev 库只有 `default` 一个分组、且 22 个渠道全是
   `type=1`，无法直接观测分组收敛，因此临时造了一个可还原夹具：插入
   `type=14`（Anthropic）渠道 99001、把 `probe-scope-model` 的 ability 指向它并置于
   `vip` 分组，另给 `default` 分组的渠道 221 加同模型 ability，再补 `ModelRatio` /
   `GroupRatio` 键。

```
# vip 渠道不在调用方分组内；另有一个模型自带 endpoints 声明
GET /v1/models            (token 9104, group=default)
-> probe-scope-model     supported_endpoint_types = ['openai']
-> probe-declared-model  supported_endpoint_types = ['anthropic']   # 声明优先于派生
GET /api/pricing          (全局快照，不带分组过滤)
-> probe-scope-model supported_endpoint_types = ['anthropic', 'openai']

# 把 Anthropic 渠道改到 default 分组后重建
GET /v1/models
-> probe-scope-model     supported_endpoint_types = ['openai', 'anthropic']
-> probe-declared-model  supported_endpoint_types = ['anthropic']   # 声明仍然优先
```

夹具已删除（`channels.id=99001`、对应 `abilities`、`models` 行、`ModelRatio` 的两个
`probe-*` 键），重建后 `/v1/models` 不再含 `probe-*`、模型总数回到 64。这是本轮对
分组收敛与"声明优先"两条行为的**直接观测**，不再只是单元测试推断。

### 6.2 令牌分组校验（已修）

`controller/token_group_validation.go` 新增 `validateTokenGroup`，接入 `AddToken` 与
`UpdateToken`（`status_only` 分支跳过，因为该分支不改动已存分组）。此前非管理员可以把
令牌分组写成任意字符串（审计记录的 `../../etc/passwd`），落库原样保存、使用时才被
`middleware.TokenAuth` 拒绝。现在创建/更新时就要求分组在
`service.GetUserUsableGroups(userGroup)` 内且 `ratio_setting.ContainsGroupRatio` 为真，
`auto` 分组放行。三条函数级用例（可用分组放行、空串与 `auto` 放行、
`../../etc/passwd` 拒绝）加两条 handler 级用例（`TestAddTokenRejectsUnusableGroup`
断言**未落库**、`TestAddTokenAcceptsUsableGroup` 断言 `vip` 仍可用）。

需要说明的边界：本轮**未做**端到端实测。dev 库只有 `default` 一个分组，
`UserUsableGroups` 里的 `vip` 等键在 `abilities` 中没有对应渠道，构造"非管理员用户提交
跨分组"的完整 HTTP 路径需要同时改用户分组与 `UserUsableGroups` 配置；该用例的判定逻辑
（`GetUserUsableGroups` + `ContainsGroupRatio`）已由函数级用例覆盖，但**写路径的端到端
行为未在隔离环境观测**。

### 6.3 本轮重建镜像与 build-info 比对

```
scripts/build-docker-local.sh new-api-local:dev
-> built image=new-api-local:dev version=v1.1.0 commit=02c20de3facc...-dirty date=2026-09-30T04:04:36Z
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env \
  up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
-> commit=02c20de3facc1024a6ab9c4a374dc3013edd98d6-dirty date=2026-09-30T04:04:36Z
git rev-parse HEAD
-> 02c20de3facc1024a6ab9c4a374dc3013edd98d6
```

镜像提交与工作区 HEAD 一致（`-dirty` 反映未提交改动，符合本轮"不提交"的约束）。
`/api/status` 返回 200。

在**这个新镜像**上重跑了首轮的核心探针，确认改动确实进入运行时（不是只过了单元测试）：

| 编号 | 探针 | 结果 |
| --- | --- | --- |
| A2 | `temperature:-1` | 400 `temperature is invalid`，`type: invalid_request_error` |
| A5 | `OPTIONS /api/status` 带 `Origin` | 204，只有 `Access-Control-Allow-Origin: *`，无 `Allow-Credentials` |
| B2 | `POST /v1/chat/completions` 未知模型 | 404 `not_found_error` / `model_not_found`，`(distributor)` 后缀保留；`logs` 无新行 |
| B6 | `max_tokens:"abc"` | 400 `field max_tokens must be a uint, got string`，`type: invalid_request_error` |
| C2 | 渠道 209 `POST /v1/responses`（非流式） | `usage` = `['input_tokens','input_tokens_details','output_tokens','output_tokens_details','total_tokens']` |

C2 这条需要先给渠道 209 临时加 `allow_missing_codex_dynamic_request_headers`（否则
非 Codex 客户端在路由预检就被拒），测完已删除该键并重建容器确认
（`settings LIKE '%allow_missing_codex_dynamic_request_headers%'` = `f`）。

### 6.4 审计 `1.8` 的 `use_channel` 归因说法**不成立**（更正，非缺陷）

审计原文称"`admin_info.use_channel=["72"]` 也只记录初始选中渠道"。本轮用只读 SQL 核对
隔离库全部历史行后确认该说法错误：`use_channel` 记录的是**整条尝试链**。

```
SELECT other::json->'admin_info'->>'use_channel' AS uc, count(*)
FROM logs WHERE other LIKE '%use_channel%' GROUP BY 1 ORDER BY 2 DESC;
-> ["106"]|83045  ["1"]|68682  ...  ["2","1"]|2810  ["106","106"]|1694
```

多元素值的行正是发生 failover 的请求，且**成功**行（`type=2`）的 `channel_id` 等于链上
最后一个渠道，与 `use_channel` 末位一致（如 `["225","76"]` / `channel_id=76`、
`["223","209"]` / `channel_id=209`）。审计引用的那一行是 `preflight_skipped` 全链被拒、
最终 `channel_unavailable` 的**失败**请求，其 `channel_id=72` 是终端错误归属，
与 `use_channel` 的语义不同，不构成归因错位。

本轮另做受控复现（临时把隔离库 `RetryTimes` 设为 3，测完立即还原为 0 并重启确认）：

```
POST /v1/responses {"model":"grok-4.6","input":"say hi"} -> 503
落库：id=430575 type=5 channel_id=223 use_channel=["223"] error_code=channel:model_mapped_error
trace.preflight_skipped=[223,150,178,209] terminal_reason=channel_unavailable
```

结论：`channel_id` 取的是候选/首次尝试渠道，错误文案点名的是链上最后一个被拒渠道
（`#209`）。这是"终端 skip 场景下错误正文与 `channel_id` 指向不同渠道"，与审计 1.8 描述的
"failover 成功后只持久化失败的那次尝试"不是同一件事。按"每次只处理一个原子任务、
无关问题只记录"的约束，本轮记录不改。

### 6.5 审计 `1.11` 的 `model_ratio` 说法**成立**，且成因在活跃路径（已文档化）

审计说 `tiered_expr` 行的 `other.model_ratio` 写 0，并把它归到修复工具。本轮只读核对
隔离库后确认**活跃计费路径同样写 0**：

```
SELECT id, model_name, other::json->>'billing_mode', other::json->>'model_ratio'
FROM logs WHERE other LIKE '%tiered_expr%' ORDER BY id DESC LIMIT 4;
-> 430568 gpt-5.6-luna tiered_expr 0
-> 430567 gpt-5.6-luna tiered_expr 0
```

而同一时刻 `/api/pricing` 返回 `gpt-5.6-luna model_ratio=0.1`。成因是
`relay/helper/price.go` 的 `modelPriceHelperTiered` 只构造
`FreeModel` / `GroupRatioInfo` / `QuotaToPreConsume` 三个字段，其余留零值，
`calculateTextQuotaSummary` 随后照抄 `relayInfo.PriceData.ModelRatio`。

未改代码：前端 `details-dialog.tsx` 与 `common-logs-columns.tsx` 都在读取
`model_ratio` 之前先按 `billing_mode === 'tiered_expr'` 分支，用户可见面无影响；
改 `PriceData` 的构造反而会让活跃路径与修正工具（后者显式把这四个字段置 0）口径不一致。
处理方式是把它写进 `docs/operations/pricing-maintenance.md` 的"必须跳过或单独设计修复
逻辑的场景"，明确 `tiered_expr` 行的四个价格字段是快照占位而非配置倍率，并给出
`gpt-5.6-luna` 的对照实例。

### 6.6 B5 的 `***.` 掩码前缀：保持现状（记录不改）

`common.MaskSensitiveInfo` 的域掩码会把两段点分标识的前段替换成 `***`。本轮用独立探针
确认实际输出：

```
litellm.ServiceUnavailableError -> ***.ServiceUnavailableError
openai.APIError                 -> ***.APIError
anthropic.RateLimitError        -> ***.RateLimitError
```

即 SDK 类名与"真实主机名"被同一条正则命中。可想到的收窄办法（例如要求末段全小写才视为
TLD）会把 `example.COM` 这类写法一并放过，属于**降低掩码强度**；掩码是安全向的，
漏掩一个主机名的代价高于把 `litellm.` 显示成 `***.` 的可读性损失。且该字符串正是要发给
**客户端**的错误正文，隐藏上游实现细节本身是期望行为。维持首轮"不动掩码正则"的决定。

### 6.7 A7 的 SIGTERM/SIGKILL 边界实测（本轮新增）

A7 此前只有单元测试（`model/batch_flush_test.go`），边界是推断的。本轮在隔离环境做了
端到端实测。方法：`BATCH_UPDATE_ENABLED=true`、`BATCH_UPDATE_INTERVAL` 默认 5 秒，
发一次请求后**立即**查库——若计数器未变，说明增量确实还在内存里（已实测确认这一前提）；
再在同一条件下去掉进程，重启后查库。

```
# 前提：批量窗口存在
发请求后立即查库   -> used_quota|request_count 与发请求前相同
8 秒后再查         -> +102|+1（5 秒 tick 落库）

# SIGTERM（docker stop，实测 0.48s 返回，远早于 5s tick）
pending -> 停 -> 起 -> 计数已更新
日志：received signal: terminated, shutting down... / server exited
-> FLUSHED

# SIGKILL（docker kill -s KILL）
pending -> 杀 -> 起 -> 计数仍是旧值，等过 7 秒（>1 个 tick）后复查仍是旧值
-> LOST
```

SIGKILL 那次要排除"tick 恰好先落库"的竞态：把发请求、查库、kill 放在同一次 shell 调用里，
并在 kill 前断言计数未变；同时容器 `RestartPolicy=always`，被 SIGKILL 后会自行重启，
但实测这次没有自动恢复（`Exited (137)`），手动 `docker start` 后 6 秒恢复。

### 6.8 前端 `bun run build` 通过

AGENTS.md 第 6 节要求前端改动跑 `bun run lint && bun run build`，此前只跑过 lint 与
单元测试。本轮补跑：

```
cd web/default && bun run build
-> rsbuild v2.0.1 构建成功
-> Safari compatibility rewrite updated 4 file(s)
-> Safari compatibility check passed for dist
-> Total: 19091.4 kB / 4326.3 kB
```

产物在 `web/default/dist`（构建产物，不入库）。

### 6.8.1 前端全量单元测试（2026-10-02 补记）

§6.8 当时只跑了 C1 相关的单个用例。本轮补跑**全部**前端测试：

```
cd web/default && bun test
-> 251 pass / 0 fail，951 expect() calls，34 files（bun 1.4.0）
```

`web/default` 无 vitest/jest 依赖，测试由 bun 内置 runner 执行（`bun test`，仓库无
`test` npm script）。此前报告的"前端测试通过"仅指 `channel-form.test.ts` 的 13 个用例，
本节把它扩到全量 251 个。`bun run lint` 同时复核通过（仅 browserslist 数据陈旧提示，非错误）。

### 6.9 生产升级清单 U1-U4 不在代码修复范围

`21 次 DROP INDEX` 演练、V2 路由回填、`options` 快照等属于生产运维动作，需要单独窗口
与回滚预案，本轮不做，也不在只读边界内执行。

### 6.10 B3 的线上复现与反向验证（本轮新增）

首轮只做了单元测试，边界记录为"未做线上复现"。本轮构造可控上游夹具补齐，并做了
**反向验证**：把修复代码摘掉后重新编译，同一请求必须复现原始缺陷。

夹具（`/tmp/upstream400.mjs`，临时文件，已删除）：Node HTTP 服务监听 `127.0.0.1:18099`，
对任意请求返回 `400` + `{"error":{"type":"invalid_request_error","message":"..."}}`。
容器内经 `host.docker.internal:18099` 可达（已用容器内 `wget` 确认收到 400）。

被触发路径：`relay/channel/api_request.go:986` 的 `doRequestWithClient` 在 `info.IsStream`
时**先于上游调用**执行 `helper.SetEventStreamHeaders(c)`（流式保活 ping 需要），上游随后返回
4xx 时 SSE 头已经落在 writer 上，`writeRelayErrorResponse` 再写 JSON 正文。

触发条件（与首轮更正后的描述一致）：**模型必须存在且可路由**，失败必须发生在上游调用阶段。
隔离环境 22 个渠道原先全是 `type=1`，无法自然触发，故临时把渠道 223 指向夹具：

```
UPDATE channels SET type=14, base_url='http://host.docker.internal:18099', priority=99
WHERE id=223;   -- 测后已还原为 type=1 / https://nameyn.shop / priority=16
```

请求（`grok-4.6` 走渠道 223）：

```
POST /v1/messages
{"model":"grok-4.6","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}
```

**反向验证的两支二进制**用同一份源码、同一组 ldflags 交叉编译（`CGO_ENABLED=0 GOOS=linux
GOARCH=amd64`），唯一差别是 `writeRelayErrorResponse` 里那段重置代码：

| 二进制 | 响应头 | 结果 |
| --- | --- | --- |
| 无修复（摘掉 `if !c.Writer.Written() { helper.ResetEventStreamHeaders(c) }`） | `Content-Type: text/event-stream`、`Transfer-Encoding: chunked`、`Cache-Control: no-cache`、`X-Accel-Buffering: no` | **复现**：SSE 头配裸 JSON 正文（`{"error":...}`） |
| 有修复（当前源码） | `Content-Type: application/json; charset=utf-8`、`Content-Length: 151` | **修复**：SSE 头全部消失，正文为 JSON |

两支的 400 状态码与错误正文一致，只有传输层头不同，说明改动确实只作用于"头已设置但正文未写出"
这一分支。单元测试侧同样做了反向验证：摘掉那段代码后
`TestWriteRelayErrorResponseResetsUnwrittenStreamHeaders` 失败
（`expected: "application/json; charset=utf-8"` / `actual: "text/event-stream"`），
还原后 `go test ./controller -run TestWriteRelayErrorResponse` 通过。

交叉编译的二进制能启动，前提是镜像构建阶段会注入前端发布元数据
（`router/frontend_release.go` 校验 `web/default/dist/new-api-release.json`）；缺失时进程
以 `FATAL ... frontend release metadata missing` 退出。本轮用仓库自带脚本
`scripts/write-frontend-release-metadata.sh` 生成，测后已删除该产物（`dist` 不入库）。

还原情况：渠道 223 配置已还原；容器内 `/new-api` 已用 `docker cp` 换回原始二进制并用
sha256 比对一致（`b6a04062...71f14`）；夹具进程与临时文件全部清理；`go vet ./controller` 退出码 0。

### 6.11 `bun run typecheck` 实测通过（更正 6.9 之外的"未验证"结论）

首轮把 `tsc -b` 记为"长时间不返回"，末轮重测推翻了它。

```
cd web/default && tsc -b --force
-> EXIT=0，耗时 225 秒

# 检查确实在工作（反向验证）
在 src/main.tsx 尾部注入 const __probe: number = "not a number";
-> src/main.tsx(157,7): error TS2322: Type 'string' is not assignable to type 'number'.
-> src/main.tsx(157,7): error TS6133: '__probe' is declared but its value is never read.
-> EXIT=2
注入内容已还原（diff 与备份一致）
```

两次判断错在哪：

1. **`uv_fs_read` 不是永久阻塞**。`sample` 抓到的栈只是采样瞬间正在读文件的普通 I/O。
   工作区 `node_modules` 有 6.3G，冷启动要跑 7 分钟以上，看上去像卡死。
2. **归因到 `/Volumes` 挂载也不成立**。`mount` 显示 `/Volumes/Work` 是本地 APFS
   （`/dev/disk3s1 on /Volumes/Work (apfs, local, journaled)`），不是 SMB 挂载。

两点操作要点，写进本轮记录以免再犯：

- **`tsc -b` 不带 `--force` 时，warm cache 下秒回 `EXIT=0` 且不做任何检查**，
  不能当通过证据；要拿它当证据必须 `--force`。
- 跑它要给 5-10 分钟，并且用后台 + 轮询退出码，不要用短的 `sleep` 链去"等"。

### 6.12 MySQL 上 `key` 未加引号导致**全新库无法启动**（已修，含反向验证）

用 `mysql:8.4` 起了一个一次性实例（`127.0.0.1:13306`），把仓库里既有的 MySQL 门控用例跑起来，
顺带用真实二进制对 MySQL 做了一次冷启动，发现一条**启动级**缺陷。

**现象**：全新 MySQL 库启动直接 `FATAL`。

```
[FATAL] failed to initialize database: Error 1064 (42000): You have an error in your SQL
syntax; ... near 'key = ? ORDER BY `options`.`key` LIMIT ?' at line 1
```

**根因**：`key` 是 MySQL 保留字，未加引号的 `"key = ?"` 谓词在 MySQL 上是语法错误；
PostgreSQL 和 SQLite 接受这种写法，所以只有 MySQL 会炸。仓库**已经有**方言感知的
`commonKeyCol`（`model/main.go` 的 `initCol`，PostgreSQL 用 `"key"`，其余用 `` `key` ``），
`model/token.go`、`model/channel.go` 都在用；但有 8 处漏了：

| 位置 | 处数 |
| --- | --- |
| `model/main.go`（`migrateChannelPinOrder` / `migrateResponsesCompactModeAuto*`） | 3 |
| `model/log_index_cleanup_migration.go`（`OptionExists`） | 1 |
| `service/npm_cli_version_cache.go` | 1 |
| `service/responses_synthetic_compact_instance.go` | 3 |

`service` 包用不了未导出的 `commonKeyCol`，因此新增导出 `model.KeyColumn()`
（空值时自行 `initCol`，对未初始化调用安全）。

**反向验证**（同一份源码，只还原那一行）：

| 二进制 | 全新 MySQL 启动 |
| --- | --- |
| 还原 `model/main.go:479` 为 `"key = ?"` | `FATAL ... Error 1064 ... near 'key = ?'` |
| 当前源码（`commonKeyCol+" = ?"`） | `ready in 1743 ms` |

另加回归用例 `model/option_test.go` 的 `TestKeyColumnIsDialectQuoted`，断言两种方言下
引号形态正确。

**顺带跑通的 MySQL 门控用例**（此前从未在本机跑过，`TEST_MYSQL_DSN` 未设置时一律 skip）：

```
model:      TestOnConflictDoNothingRowsAffectedOnMySQL              PASS
            TestAddAbilitiesRejectsOrphanConflictOnMySQL            PASS
            TestUpdateRouteScopeAbilityAcceptsUnchangedValuesOnMySQL PASS
            TestDisableChannelByTagRollsBackOnMySQL                 PASS
controller: TestTokenMigrationFromChar48ToVarchar128MySQL           FAIL（见下）
```

**仍未修：`uni_tokens_key`（gorm 上游缺陷，非本仓库代码）**

上面那条修好后，MySQL **重启**（第二次 `AutoMigrate`）仍然失败：

```
Error 1091 (42000): Can't DROP 'uni_tokens_key'; check that column/key exists
```

机制已定位到 gorm 内部，不是本仓库可改的代码：

- `model.Token.Key` 的 tag 是 `type:varchar(128);uniqueIndex`，于是
  `field.UniqueIndex="idx_tokens_key"` 而 **`field.Unique=false`**；
- MySQL 把该列报成 `COLUMN_KEY=UNI`，驱动据此给出 **`columnType.Unique()=true`**；
- gorm `MigrateColumnUnique`（`gorm@v1.25.10/migrator/migrator.go:599`）在
  `unique && !field.Unique` 时调 `DropConstraint`，约束名由
  `NamingStrategy.UniqueName("tokens","key")` 生成为 **`uni_tokens_key`**；
- MySQL 驱动的 `DropConstraint` 发的是 `ALTER TABLE ... DROP FOREIGN KEY`，
  而该名字根本不是外键，于是报 1091。

即 `uniqueIndex` 与 MySQL 的 `UNI` 无法区分，gorm 自己那条注释（`:604`
"we don't care about UniqueIndex"）说明它已知这个问题但未处理 `UniqueIndex` 字段。
`gorm@v1.31.2` 的同一函数**逐字未变**，升级不解决。已确认这是**既有**缺陷，
与本轮改动无关：用改动前的二进制对同一个已迁移库启动，报同一条错。

影响面：生产 `SQL_DSN` 是 PostgreSQL（`docker inspect` 确认），PostgreSQL 与 SQLite
路径都不受影响，所以这是 MySQL 部署的潜在问题，不是当前生产故障。**已在后续轮修掉，见 6.13。**

### 6.13 MySQL 重启失败（`uni_tokens_key`）已修：自定义 dialector 纠正 gorm

6.12 记录的那条"记录不改"，在用户要求"全部优化好"后做掉了。

**修法**：新增 `model/mysql_dialector.go`，包装驱动自己的 dialector，只覆写一个方法：
`MigrateColumnUnique` 在 `field.UniqueIndex != "" && !field.Unique`（唯一性来自唯一**索引**、
而非唯一**约束**）时直接返回 nil —— 这种情况下根本没有约束可供 gorm 去 add/drop。
判据抽成 `skipsUniqueIndexReconciliation(field)` 便于单测。真正的 `unique` tag 与普通列
照旧走驱动实现。`chooseDB` 改用 `model.MySQLDialector(dsn)`。

**一个踩到的坑**：第一版把 gorm 的**接口**嵌进去（`gorm.Dialector` / `gorm.Migrator`），
构建通过但启动即 panic：

```
panic: interface conversion: model.mysqlMigrator is not migrator.BuildIndexOptionsInterface: missing method BuildIndexOptions
```

gorm 在 `CreateTable` 里对 migrator 做**可选能力**的类型断言
（`tx.Migrator().(BuildIndexOptionsInterface)`），接口包装会把这些方法丢掉。
改成嵌入**驱动的具体类型**（`*mysql.Dialector`、`mysql.Migrator`）后正常。
这一条已固化成用例 `TestMySQLDialectorKeepsOptionalMigratorCapabilities`。

**反向验证**（同一份源码，只摘掉那一个判据）：

| 二进制 | 全新 MySQL 启动 | 重启（第二次 AutoMigrate） |
| --- | --- | --- |
| 摘掉判据 | `ready in 2088 ms` | `FATAL ... Error 1091: Can't DROP 'uni_tokens_key'` |
| 当前源码 | `ready in 4802 ms` | `ready in 1874 ms` |

唯一索引在修复后完好保留（`tokens` 上仍是 `idx_tokens_key`，`COLUMN_KEY=UNI`、
`varchar(128)`），说明只是不再误删一个不存在的约束，没有削弱约束语义。

**MySQL 门控用例现在全绿**（此前 `TestTokenMigrationFromChar48ToVarchar128MySQL` 一直失败）：

```
model:      TestOnConflictDoNothingRowsAffectedOnMySQL               PASS
            TestAddAbilitiesRejectsOrphanConflictOnMySQL             PASS
            TestUpdateRouteScopeAbilityAcceptsUnchangedValuesOnMySQL PASS
            TestDisableChannelByTagRollsBackOnMySQL                  PASS
            TestMySQLDialectorKeepsOptionalMigratorCapabilities      PASS
            TestMySQLDialectorDelegatesToTheDriver                   PASS
controller: TestTokenMigrationFromChar48ToVarchar128MySQL            PASS
EXIT=0
```

`controller/token_test.go` 里那处 `mysql.Open` 也改成 `model.MySQLDialector`：用例必须走
服务端真正使用的 migrator，否则测的是一个生产上不存在的组合。反向验证过这一点 ——
换回 stock dialector，该用例立刻回到 `Error 1091` 失败。

全量 `go test ./... -count=1`：`EXIT=0`、63 包 ok、0 FAIL。

## 七、本机 AI 工具端点统一（与代码修复分开）

按用户指令统一到 13000（生产容器端口映射）：

| 工具 | 文件 | 改动 |
| --- | --- | --- |
| grok | `~/.grok/config.toml` | `models_base_url` `127.0.0.1:3000` -> `127.0.0.1:13000` |
| omp | `~/.omp/agent/models.yml` | 3 处 `baseUrl` 同上 |
| claude | `~/.claude/settings.json` | 已是 13000，未改动（该文件不得修改） |
| codex | `~/.codex/config.toml` | 已是 13000，未改动 |

`3000` 是 caddy 反代到 `13000` 的同一生产容器，两者等价；统一到 13000 是为了绕开中间层。

## 八、代码审查轮（2026-10-01 追加）

用户要求对本轮全部改动做代码审查。派发 4 个并行 Agent 深度复核：

| Agent | 审查范围 | 结果 |
| --- | --- | --- |
| Worker 1 | 整体架构与错误处理逻辑 | 当时记为**进行中**；事后查证为 **03:11:18 `succeeded`**，但正文已不可取回（见 §8.2 更正） |
| Worker 2 | Token 组验证逻辑 (`controller/token_group_validation.go`、`controller/token.go`) | **通过**，无问题 |
| Worker 3 | 测试覆盖与 fixture 一致性 | **发现 1 个问题**：`controller/model_list_test.go` cleanup 缺失 `InvalidatePricingCache()` |
| Worker 4 | Relay 错误归因逻辑 (`controller/relay.go`) | **通过**，无问题 |

### 8.1 Worker 3 发现的问题：测试 cleanup 不完整

**问题**：`controller/model_list_test.go:65-77` 的 `setupModelListControllerTestDB` cleanup 函数恢复了全局变量和数据库连接，但**没有调用 `model.InvalidatePricingCache()`**。

**风险**：定价快照是进程级缓存，TTL 1 分钟。若测试顺序打乱，下一个测试可能在 TTL 未过期时读到**已关闭数据库**的快照指针，导致 panic 或不确定行为。

**对比**：`model/pricing_endpoint_types_test.go` 的 `setupPricingEndpointTypesTestDB` 在 cleanup 中正确调用了 `InvalidatePricingCache()`（第 48 行）。

**修复**：

```diff
 t.Cleanup(func() {
     common.UsingSQLite = previousUsingSQLite
     common.UsingMySQL = previousUsingMySQL
     common.UsingPostgreSQL = previousUsingPostgreSQL
     common.RedisEnabled = previousRedisEnabled
     model.DB = previousDB
     model.LOG_DB = previousLogDB
+    model.InvalidatePricingCache()

     sqlDB, err := db.DB()
     if err == nil {
         _ = sqlDB.Close()
     }
 })
```

**验证**：

```bash
$ go test ./controller -run TestListModels -shuffle=on -count=3
ok      github.com/QuantumNous/new-api/controller       0.139s
```

随机测试顺序下连续 3 次通过，修复有效。

### 8.2 Worker 1 状态

Worker 1 自 02:14:46Z 最后心跳后进入深度思考(Simmering)状态，截至 02:23 UTC 已持续 31+ 分钟，消耗 112.6k tokens。终端显示最后执行"Running 2 shell commands"后回到空闲 shell 提示符，但 Worker 仍在解读测试结果。

**异常判断**: 正常代码审查即使深度复核也不应超过 30 分钟。考虑到:
- Worker 2/3/4 都在合理时间内完成(1-3 分钟)
- Worker 3 已发现唯一实质问题(测试 cleanup)，已修复并验证通过
- Worker 1 已消耗 3 倍于其他 Worker 的 tokens
- 最后心跳距今 8+ 分钟无新输出

**结论**: Worker 1 可能陷入过度思考或等待状态。基于已完成的三个 Worker 的审查结果(1 个真实问题，已修复)，本轮代码审查实质上已完成。

> **更正（2026-10-02）**：上述"陷入过度思考"的判断**未被后续事实支持**。
> 该轮次对应的是 `task_9efb0cb51519` / dispatch `ctx_62dd083f8ae9`，Orca 记录显示它
> **02:40:22 派发、03:11:18 `succeeded`**（`worker-show` 实测）——即在写下这段"可能卡住"的判断
> **之后**又运行了约 48 分钟并正常交付。`worker-read` 现已返回
> `worker_identity_changed`（进程身份已变），无法再取回其正文，因此它具体报告了什么内容
> **不可考**；但"没有产出"这个结论是错的。
>
> 更需要留意的是它对 §8.3 的影响：§8.3 据此写下"发现问题数 1、待修复 0"，而紧接着的
> 第九节（第二轮复核）又独立找出 3 条真实缺陷（A4 的两条文案、A6 的 Redis 路径、P2 的
> `GetUsage`）。**以"某个 worker 迟迟不返回"推断"审查已完成"，在本轮被证伪了两次**——
> 一次是 worker 实际成功了，一次是结论本身漏掉了 3 条。§8.3 的"待修复 0"应以第九节为准。

### 8.3 代码审查总结

**发现问题数**: 1 个  
**已修复**: 1 个  
**待修复**: 0 个

> 本节的"待修复 0"**已被第九节取代**：第二轮复核另发现并修复 3 条（A4/A6/P2），
> 详见 §9 与 `CR-FULL-SCENARIO-AUDIT-FIXES-SUMMARY.md`。

唯一发现的问题是测试 cleanup 不完整(`controller/model_list_test.go` 缺失 `InvalidatePricingCache()`)，已修复并通过 `-shuffle=on -count=3` 验证。

其余审查项(整体架构、Token 组验证、Relay 错误归因)均通过，无发现问题。

## 九、第二轮审查：结论的独立复现与修正（2026-10-01 追加）

第二轮派发的复核 Agent 给出 4 条结论（A4、A6/P3、P1、P2）。按"子代理结论必须独立复现"的
规则逐条重跑，**其中 2 条被证实但归因错误，1 条证实，1 条为既有死代码**。

### 9.1 A4：掩码误伤，**结论成立但 Agent 举的例子不存在**

Agent 称 `"token balance: 50"` 被掩成 `"token *** 50"`。实测确实如此，但**全仓不存在任何
产生 `token balance` 的代码**，该例子是构造出来的。

重新扫描全仓 `"..."` 字面量里 `token <word>` 的真实形态后，发现**另有 4 条真实错误文案会被掩码器
改写**。但"掩码器会改写"与"客户端真的看到改写后的文案"是两件事，逐条查了调用链：

| 真实文案 | 来源 | 掩码器输出 | 是否经过掩码器 |
| --- | --- | --- | --- |
| `token model access configuration is invalid` | `middleware/distributor.go:130`、`controller/claude_count_tokens.go:65` | `token *** access configuration is invalid` | **是**（两条链：`relay.go:3590` → `TaskErrorWrapperLocal` → `TaskErrorWrapper`；`writeClaudeCountTokensError` → `ToClaudeError`） |
| `token id is required` | `service/responses_synthetic_compact.go:1068` | `token *** is required` | **是**（`NewOpenAIError` → `ToOpenAIError` → `sanitizeOpenAIError`） |
| `token endpoint is required` | `model/custom_oauth_provider.go:364` | `token *** is required` | 否（走 `common.ApiError` → `c.JSON(err.Error())`，不经掩码器） |
| `Responses opaque replay body requires token authentication` | `middleware/gzip.go:144` | `...requires token ***` | 否（走 `abortWithOpenAiMessage` → `c.JSON(message)`，不经掩码器） |

即：**2 条在真实掩码路径上**（已确认 `TaskErrorWrapper` 与 `sanitizeOpenAIError` 都调用
`MaskSensitiveInfo`），另 2 条当前不经过掩码器——它们的错误串直接 `c.JSON`。后两条仍纳入白名单
是为了口径一致，但**不能声称它们当前正被误伤**。

**修复**：`common/str.go` 的 `isAuthSchemeProseWord` 白名单补入 `model`、`id`、`endpoint`、
`authentication`。该函数是对整个捕获词的**精确匹配**，所以以这些词开头的凭证（如
`token model-abcdef...`）仍会被掩码。

**反向验证**（`common/mask_prose_test.go` 新增 `TestMaskSensitiveInfoKeepsTokenConceptProse`）：
还原白名单后 4 条用例全部 FAIL，输出与上表"掩码器输出"列一致。

### 9.2 A6/P3：429 的 `Retry-After`，**结论成立，且缺口在生产路径上**

`abortRateLimited` 的调用点是 **6 处**（`rate-limit.go` 4 处 + `model-rate-limit.go` 2 处），
不是审查报告写的 7 处。

真正的问题在**另一条路径**：`middleware/email-verification-rate-limit.go` 有两个限流器，
本会话此前只给内存路径加了 `Retry-After`。而生产 `REDIS_CONN_STRING` 已设置
（`docker inspect new-api` 确认），走的是 **Redis 路径**，该路径此前**没有任何 `Retry-After`**，
客户端只能靠解析中文文案猜等待时间。

**修复**：Redis 路径补 `Retry-After`（值取 `TTL` 实际剩余秒数，失败时回落到窗口长度）。
响应体保持 `{"success":false,"message":...}` 不变——这是 `/api/` 会话接口的既有形状，
前端 `web/default/src/features/auth/api.ts:114` 依赖它，不属于 429 协议面。

**验证**：`middleware/email_verification_rate_limit_test.go` 新增两个用例（miniredis 驱动
Redis 路径 + 内存路径），断言 429 + `Retry-After: 30` + body 形状。**反向验证**：去掉
`Retry-After` 后 Redis 用例 FAIL（`actual: ""`）。

### 9.3 P2：`GetUsage` 仍输出 `new_api_error`，**结论成立，且是 B6 的覆盖遗漏**

`controller/billing.go:91` 的 `GetUsage`（路由 `router/dashboard.go:20-21`）在本会话
B6 统一词表时**被漏掉了**——第 46-54 行的"四个生产者，本轮全部补齐"表格没有它。
该端点用 HTTP 200 + 错误体回答（对齐 OpenAI billing API），所以状态码无法提供词表。

**修复**：改用 `types.StandardizeErrorType(..., http.StatusInternalServerError)`，
输出 `server_error`。前端 `web/` 下对 `dashboard/billing/*` 与 `new_api_error` 均 **0 处引用**，
改动面无消费方。

**验证**：`controller/billing_error_type_test.go` 新增用例。**反向验证**：还原字面量后
FAIL（`"type":"new_api_error"` 不含 `"type":"server_error"`）。

### 9.4 P1：`abortWithMidjourneyMessage` 无调用者，**证实，但不改**

`middleware/utils.go:87` 的该函数全仓 0 个调用点，是死代码。`git show origin/main` 确认
它**在主线就存在**，不是本会话引入。按"每次只处理一个原子任务、无关问题只记录"的规则，
本轮不删，仅记录。

### 9.5 本轮验证

```bash
go build ./...                                                       # EXIT=0
go test ./controller ./model ./relay/common ./relay/helper \
        ./service ./middleware ./common ./types -count=1             # 8 包全 ok
```

三条修复各自的回归用例均有反向验证（还原修复后必须 FAIL，已逐条执行并读取输出）。

**未覆盖边界**：A6 的 Redis 用例用 miniredis，覆盖语义但未覆盖真实 Redis 版本差异。

### 9.6 Orca 资源收尾

按用户"用完了的记得确认后关闭掉"的要求，逐项确认后关闭：

| 资源 | 处理 | 依据 |
| --- | --- | --- |
| 三个 worktree（`mysql-key-review`、`mysql-key-review-2`、`review-mysql-key-fix`） | 已删除，分支一并删除 | 三个分支对 `dev` 的独有提交数均为 **0**；逐文件 sha256 比对确认改动已全部在主工作区（`review-mysql-key-fix` 的 `option_test.go` 是主工作区版本的**子集**，删除前已备份核对） |
| 三个 worktree 终端 | `terminal close --worktree <path> --all` | 关闭前逐条读屏确认：一个 agent 已上报 `worker_done` 并停在空提示符，两个是 `claude: command not found` 失败后残留的裸 shell |
| `run_47467b455d99` 的 4 个 worker | 3 个已 released；`ctx_62dd083f8ae9` 为 `user_owned`（`retainedReason: user_takeover`），`worker-release` 按设计返回 no-op，`nextAction` 为 `none` | 所有权已交给用户，非异常 |
| `run_d19ba6c27a6d` 的 2 个 worker | `ctx_73c0733545a8` 同上（`user_owned`）；`ctx_18633b7ffa12` 三次 `worker-release` 均返回 `release_unknown` | 见下 |

`ctx_18633b7ffa12` 的 `release_unknown` 无法收敛：该 worker 在 `agent_readiness` 阶段失败
（就是 `claude: command not found` 那次），它的 worktree 与终端都已随上面的清理消失，但 Orca 无法
确认进程已停。按 `references/recovery-and-cleanup.md`，"绝不 substitute `terminal close`"，所以
按 receipt 的 recovery 用新 request ID 重试了 3 次，仍返回同一结果；再试 `worker-stop` 与
`worker-abandon`，两者都报 `alreadySettled: true, processAction: none`（`worker-abandon` 另报
`stale: true`）。独立核对：该终端记录为 `connected:false / orphaned:true /
exitCause.kind="operator_close"`，且全机无任何进程的 cwd 或命令行指向已删路径。**结论**：进程
实际已停，残留的是 Orca 的记账行，不影响运行；已在 `worker-list` 中如实标注为
`release_unknown`，不粉饰为已释放。
### 9.7 复查自身测试的健壮性（2026-10-02 追加）

§9.1-9.4 的验证只跑了 `-count=1`，没有检查新用例在 `-shuffle` / 重复运行下是否稳定。
补做后**发现我自己的一个用例有缺陷，并已修复**：

**`TestEmailVerificationMemoryRateLimitSetsRetryAfter` 在 `-count>1` 下必失败**。
`inMemoryRateLimiter` 是包级单例，`Request()` 每调用一次就记一条，而该用例原本用
`inMemoryRateLimiter.Init(...)` 就地初始化，等于把本轮 3 次请求的计数留在了共享实例里，
第二轮运行第一次请求就撞上 429。改为按仓库既有模式**换入新实例**并在 cleanup 换回
（`rate_limit_429_response_test.go:62-64`、`rate_limit_test.go:19-25` 都是这个写法）：

```bash
go test ./middleware -run 'TestEmailVerification' -shuffle=on -count=5   # 修复前 FAIL，修复后 ok
```

**另发现一个与本次改动无关的既有缺陷（记录不改）**：`middleware/rate-limit_test.go` 的
`TestDesktopOAuthPollRateLimitUsesHandoffTokenKey` 与 `TestDesktopOAuthPollRateLimitRejectsExcessRequests`
同样不重置共享 `inMemoryRateLimiter`，在 `-count>1` 下第二次运行即失败：

```bash
go test ./middleware -run 'TestDesktopOAuthPollRateLimit' -count=3
# rate-limit_test.go:46: expected token-a request 1 to pass, got 429
```

**已确认非本次改动引入**：把 `middleware/rate-limit.go` 还原到 `HEAD` 后该失败照旧复现；
再把本次新增的 7 个测试文件全部移出包后也照旧复现。按"无关问题只记录"的规则不在本轮修改。
注意这两个用例各自加了 `desktopOAuthRateLimitTestMu` 互斥锁，说明作者已知共享状态问题，
但锁只防并发、不防跨轮次累积。

### 9.8 按"能优化的都优化好"扩大修复范围（2026-10-02 追加）

用户明确要求把能优化的都修掉，因此 §9.7 中"记录不改"的既有缺陷本轮一并修复，并沿
"跨轮次状态泄漏"这一条线索把全仓扫了一遍。共修 3 处，全部有反向验证。

#### 9.8.1 `openTestDB` 未关闭连接，55 个 `model` 测试在 `-count>=2` 下失败（已修）

`model/tag_request_header_policy_test.go:20` 的 DSN 是
`file:<t.Name()>?mode=memory&cache=shared`，按测试名固定命名，且**从不关闭连接**。
`cache=shared` 的内存库存活到最后一个连接关闭为止，所以第二次运行时第一个 pass 的行还在：
插入撞 `UNIQUE constraint failed: channels.id`，断言读到上一轮的状态。

```bash
go test ./model -shuffle=on -count=2   # 修复前：55 个用例 FAIL
```

修法是在 `openTestDB` 加 `t.Cleanup` 关闭底层 `sql.DB`，每个 pass 结束时销毁该内存库。
修复后 `-count=3` 全绿。**该缺陷非本会话引入**：`openTestDB` 出自 `a5e224627`，
失败用例定义在多个未改动的已提交文件里。

#### 9.8.2 `HybridCache` 在构造时捕获 Redis 客户端，替换后仍写旧连接（已修）

`service/channel_affinity.go` 的 affinity 缓存由 `sync.Once` 每进程构建一次，
配置里 `Redis: common.RDB` **在构造时捕获客户端**，而 `RedisEnabled` 却是每次调用求值的
闭包（`common.RedisEnabled && common.RDB != nil`）。两者在客户端被替换后立刻不一致：
闸门说"能用 Redis"，操作却写向已关闭的旧连接。

用独立探针取到直接证据（探针已删）：

```
same cache instance across runs: true
SetWithTTL against swapped redis error: redis: client is closed
Get after write: value=0 found=false err: redis: client is closed
```

后果是 `RecordChannelAffinity` 静默失败，下一轮 `GetPreferredChannelByAffinity` 查不到：

```bash
go test ./controller -run TestGetChannelInitialAffinityMultiKeySkipsCooledCredential -count=2
# 修复前 FAIL: channel_health_integration_test.go:284 Should be true
```

修法在 `pkg/cachex`：新增 `HybridCacheConfig.RedisFunc func() *redis.Client`，
与 `RedisEnabled` 一样**每次调用求值**；`redisOn()` 与四处操作点统一走新的 `client()`
解析器。四个调用点（`service/channel_affinity.go` ×2、`model/subscription.go` ×2）改为
传 `RedisFunc: func() *redis.Client { return common.RDB }`。

**生产可达性**：`common.RDB` 只在 `main.go:373` 的 `common.InitRedisClient()` 赋值一次，
所以线上不会中途替换，本缺陷实际只在测试中触发。仍按源码修，因为"闸门动态、操作静态"
本身是 `HybridCache` 的内部不一致，留着会让任何未来的重连或换库踩同一个坑。

**反向验证（端到端）**：把两个调用点还原成 `Redis: common.RDB` 后，
`-count=2` 立刻复现原来的 `Should be true` 失败；恢复即通过。
另在 `pkg/cachex` 加了 `TestHybridCacheRedisFuncFollowsAReplacedClient` 固化该不变量。

#### 9.8.3 `middleware/rate-limit_test.go` 的两个 desktop OAuth 用例（已修）

§9.7 记录的那两个用例本轮一并修掉：新增 `useFreshInMemoryRateLimiter(t)` 助手，
按仓库既有模式换入新 limiter 实例。修复后 `-count=3` 通过。

#### 9.8.4 本轮验证

```bash
go build ./...                                              # EXIT=0
go test ./controller ./model ./relay/common ./relay/helper \
        ./service ./middleware ./common ./types ./pkg/cachex \
        -shuffle=on -count=2                                # 9 包全 ok
go test -race ./middleware ./common -count=1                # ok
```

三处修复各有反向验证，已逐条执行并读取输出。

**未覆盖边界**：`-count=3` 只对 `model`、`middleware`、`controller`、`pkg/cachex` 跑过，
`relay/helper` 与 `service` 只跑到 `-count=2`（单次耗时 35s / 37s，再翻倍代价偏高）。
其余同类 `sync.Once` 缓存（`model/subscription.go` 已改）与从不关闭的内存库
（`model/main_logger_test.go` 四处）经查不会因重复运行失败——前者本轮已修，后者
`AutoMigrate` 幂等且不断言行数，属潜在脆弱但当前非缺陷，未改。

### 9.9 复查上节修复本身（2026-10-02 追加）

对上节四处改动做一次自查，补上前一轮留下的覆盖缺口，并修掉自查中发现的一个自造问题。

**补跑上节标为"未覆盖"的两项**：

```bash
go test ./relay/helper ./service -shuffle=on -count=3      # 均 ok（上节只到 -count=2）
go test -race ./relay/helper ./relay/common ./types -count=1   # 均 ok（上节未覆盖这三个包）
go vet ./pkg/cachex ./service ./model ./controller ./middleware # EXIT=0
go test ./model -run 'TestNewGormLogger|TestRunDBMigration' -count=5  # ok
```

最后一条是核实上节"`main_logger_test.go` 不关闭连接但无害"这一判断——`-count=5` 通过，
判断成立。全仓再扫一遍，除该文件外所有使用命名内存库的测试文件都有 `Close()`。

**修掉一个自造问题**：上节给 `HybridCache.scanKeys` 加的 `if client == nil { return nil, nil }`
守卫是**不可达的死代码**——它的三个调用点（`Keys`、`Purge`、`DeleteByPrefix`）都先过了
`redisOn()`，而 `redisOn()` 已保证 `client()` 非 nil。更要紧的是它 `return nil, nil`
是一条**静默成功**路径，正违反 AGENTS.md §0"禁止静默容错、伪成功路径"。已删除该守卫并
在函数上注明前置条件。

**未覆盖边界**：`model/subscription.go` 的两个订阅缓存调用点当时没有任何测试覆盖
（`-run 'TestSubscription'` 无匹配用例），所以那两处改动只有"全包 `-count=3` 通过"这一
间接证据，没有针对性的反向验证。`HybridCache` 的不变量本身由
`pkg/cachex/hybrid_cache_redis_func_test.go` 固化。
**该边界已在 §9.12 补齐**（新增 `model/subscription_plan_cache_redis_test.go`，含反向验证）。

### 9.10 把 `-count=2` 推到全仓，又查出 1 处（2026-10-02 追加）

§9.8 只扫了 9 个包。本节把 `-count=2` 推到**全仓**（`go test ./...`），
在之前完全没扫过的 `router` 包里又查出一个同类泄漏：

**`router/api_router_test.go` 的 `TestChannelNpmVersionOptionRoutesUseAdminAuthAndCriticalRateLimit`
在 `-count>=2` 下失败**。该用例把 `CriticalRateLimitNum` 设为 1 并挂载真实 API 路由，
第一次请求就把限额用掉；`inMemoryRateLimiter` 是包级单例，那条记录在整个一小时窗口内都在，
于是第二轮第一次 GET 直接 429：

```bash
go test ./router -run TestChannelNpmVersionOptionRoutesUseAdminAuthAndCriticalRateLimit -count=2
# 修复前 FAIL: api_router_test.go:53 expected 200, actual 429
```

**修法**：`router` 包够不到 `middleware` 的未导出变量，因此在 `middleware/rate-limit.go`
新增导出钩子 `ResetInMemoryRateLimiterForTest()`（与仓库既有的
`service.ResetNpmCLIVersionCacheForTest` 同形，注释写明为何必须导出），在
`setupNpmVersionAPIRouterTest` 里调用。

**反向验证**：移除该调用后 `-count=2` 立刻复现 `expected 200, actual 429`；恢复即通过。

**全仓验证**：

```bash
go test ./... -shuffle=on -count=2      # 0 个 FAIL
go test -race ./router ./middleware -count=1   # ok
```

**该缺陷非本会话引入**：`router/api_router_test.go` 与 `router/` 下所有文件本轮之前均未被修改
（`git status` 为空）。


### 9.11 把验证推到全仓，并修掉上节修复自身的一处缺陷（2026-10-02 追加）

#### 9.11.1 全仓维度验证（此前只覆盖部分包）

```bash
go vet ./...                              # 无输出
go test ./... -shuffle=on -count=3        # 0 个 FAIL（上节只到 -count=2）
go test -race ./... -count=1              # 0 个 DATA RACE，0 个 FAIL（此前 -race 只覆盖 9 个包）
```

三项全绿，未再发现新的跨轮次泄漏。`router/healthz_test.go` 同样修改了
`GlobalApiRateLimitNum` 且不重置，但它在 `-count=3` 下稳定通过：它把 `Num` 设为 0，
`memoryRateLimiter` 的 `maxRequestNum=0` 使每次请求都超限（该用例正是断言 healthz 绕过限流），
不依赖累计计数，所以不受跨轮次状态影响。已核实，非缺陷。

#### 9.11.2 修掉上节修复自身的一处缺陷：客户端被解析两次

上节把操作点写成 `if client := c.client(); c.redisOn() {`。**这在一行里解析了两次客户端**
——`c.client()` 一次，`redisOn()` 内部又一次。当 `RedisFunc` 每次返回不同客户端时，
闸门可能批准 A 而操作用 B，**正是上节要修的那类"闸门与操作不一致"**。上节引入的
`TestHybridCacheRedisFuncFollowsAReplacedClient` 用的是稳定闭包，所以没暴露它。

**修法**：新增 `redisClient() (*redis.Client, bool)`，一次解析同时给出客户端与"是否启用"；
四个操作点（`Get`/`SetWithTTL`/`DeleteMany`，以及 `Keys`/`Purge`/`DeleteByPrefix` 经
`scanKeys`）统一改用它，`scanKeys` 改为接收已解析的客户端。`Capacity()`/`Algorithm()`
保留 `redisOn()`：它们是纯判断、无后续操作，重复解析不影响结果。

**新增回归用例** `TestHybridCacheResolvesTheClientOncePerOperation`：用计数闭包断言
`SetWithTTL` 与 `Get` 各只解析客户端一次。**反向验证**：把该点改回 `c.client()` 与
`redisOn()` 分开调用后，用例立即 FAIL（`resolutions == 2`）；恢复即通过。

这一条说明上节的改动本身也需要复查——新写的代码与既有代码一样会引入缺陷，而且新代码
更容易因为"刚写完、看着对"而漏检。

### 9.12 补上 `model/subscription.go` 两处缓存改动的针对性覆盖（2026-10-02 追加）

§9.8 把 `HybridCache` 的 `RedisFunc` 机制落到 4 个调用点，其中 `model/subscription.go`
的**两个订阅缓存**此前没有任何测试覆盖（`go test ./model -run 'TestSubscription'` 无匹配
用例），所以那两处改动当时只有"全包 `-count=3` 通过"这一间接证据，缺少针对性的反向验证。

新增 `model/subscription_plan_cache_redis_test.go`，用 miniredis 换入客户端，再换第二次，
断言重新读取后的缓存**写进了当前客户端**：

```go
fullKey := getSubscriptionPlanCache().FullKey(subscriptionPlanCacheKey(plan.Id))
stored, err := current.Get(context.Background(), fullKey).Result()
require.NoError(t, err,
    "the plan must be cached in the current Redis client, not a replaced one")
require.Contains(t, stored, plan.Title)
```

#### 9.12.1 该用例的第一版是**无效的**（反向验证时才发现）

第一版只断言 `getSubscriptionPlanByIdTx(nil, plan.Id)` 返回的 plan 正确。反向验证时把
`model/subscription.go` 的两处 `RedisFunc` 改回 `Redis: common.RDB`（并删掉 redis import），
用例**仍然通过**——因为 `getSubscriptionPlanByIdTx` 在缓存未命中时会回落查库，无论缓存
写没写进去都返回同一个 plan。**这个用例即使缺陷复现也不会失败，等于零覆盖。**

改为直接对缓存内容断言（上面的 `current.Get(ctx, fullKey)`）后重做反向验证：
还原 `Redis: common.RDB` 时用例**立即 FAIL**（`the plan must be cached in the current
Redis client, not a replaced one`），恢复 `RedisFunc` 后通过。这才是有效的反向验证。

#### 9.12.2 教训

**"反向验证通过"本身也要看失败原因**。第一版用例的反向验证是"通过"的，但通过的原因是
断言点选在了有回落路径的返回值上，而不是被测机制本身。§9.8 的 `HybridCache` 用例之所以
有效，是因为它断言的是 `RedisFunc` 的求值次数/客户端身份；本条的第一版断言的是业务函数
返回值——那正是回退路径会掩盖差异的地方。**断言点必须落在修复所改变的那一层状态上。**

至此 §9.8 记录的 4 个 `RedisFunc` 调用点（`service/channel_affinity.go` 2 处、
`model/subscription.go` 2 处）**都有针对性的反向验证**，不再依赖"全包通过"这一间接证据。
