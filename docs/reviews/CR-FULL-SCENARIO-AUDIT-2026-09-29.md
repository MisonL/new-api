# CR: 全场景全维度审查（真实 CLI 驱动 + 升级风险）

- 日期：2026-09-29
- 分支：`dev`。审查主体在源码 `ef093ab64`（容器 build `ef093ab64-dirty`，
  date=2026-09-29T06:07:07Z）上完成；收尾时另一个会话把容器重建为
  `797ed0268-dirty`（date=2026-09-29T08:44:19Z），主代理在新镜像上**逐条重跑了
  全部 P1 复现**，结论一致（见下）
- 被测：隔离开发环境 `new-api-dev-isolated-new-api-1`，`http://127.0.0.1:3001`
- 对照：正式环境 `new-api`（端口 13000，build `4527d7666-litefix2`，源码提交日期 2026-07-17），**只读**
- 状态：审查完成，未做代码修复，未提交

### 换镜像后的复现复核

容器被外部会话重建为 `797ed0268-dirty` 后，主代理在新镜像上重跑了全部 P1 的复现命令
（`ef093ab64..797ed0268` 的 diff 只涉及限流豁免与 i18n，未触碰任何被标记的代码路径，
`git diff --stat` 对 `relay/common/override.go`、`controller/relay.go`、`controller/model.go`、
`relay/helper/price.go`、`relay/channel/api_request.go`、`relay/helper/common.go` 均为空）：

| 条目 | 新镜像上的结果 |
| --- | --- |
| 1.1 `/v1/responses` 无 `x-codex-*` | 503 `... requires unconditional pass_headers ... x-codex-*`，一致 |
| 1.2 硬杀丢配额 | 3 个 200 后 `used=18` / `logs sum=27`，SIGKILL 后 +0/+30/+60s 恒为 18，差值 9 永不收敛，一致 |
| 1.3 `GET /v1/models/gpt-5.5` | 200 + `"The model 'gpt-5.5' does not exist"`，一致 |
| 1.4 `POST /v1/completions` | 404 + `openai_error` / `bad_response_status_code`，一致 |
| 1.5 SSE 头配裸 JSON | 400 + `Content-Type: text/event-stream` + 正文首字节 `{`，一致 |
| 1.6 grok usage 漂移 | pin 223 有 `cost_in_usd_ticks` / `num_sources_used`，pin 209 没有，一致 |
| 1.7 `n=3` | choices 长度 1，indices `[0]`，一致 |
| 1.10 `/v1/embeddings` | 503 + `***.ServiceUnavailableError: ... OpenAIException - no available server`，一致 |

1.8（failover 归因错位）与 1.9（404 被改写成 500）需要构造特定上游失败，
本次未在新镜像上重跑；它们的证据来自隔离环境上同一份代码路径的复现，
且该路径在 `ef093ab64..797ed0268` 之间未被改动。

## 授权与边界

用户指令："现在使用agents对最新源码和新部署的服务端情况进行全场景全维度审查，允许真实调用API渠道
（可使用从正式服务拷贝的渠道配置），建议用真实codex、claude code、grok build、omp来实际调用测试
看效果，注意先把工具都升级到最新版本哦"。

- 目标全程只访问 `127.0.0.1:3001`。正式库只读
  （`docker exec postgres psql -U root -d new-api` 只跑 SELECT）；正式容器未重建、未重启。
- 隔离库的临时改动已在收尾时全部还原，见"环境还原"一节。
- 凭据、渠道 key、上游主机名不写入本文件。

### 已发生的越界：6 次请求确实到达了正式服务

**如实记录，不掩饰。** 审查早期为了把 CLI 指向隔离环境，试过用环境变量覆盖
（claude 的 `ANTHROPIC_BASE_URL`、grok 的 `GROK_CONFIG_PATH`），两次都**静默无效** ——
工具照常返回了预期字符串，请求却落到了本机 `~/.claude/settings.json` 与
`~/.grok/config.toml` 里配置的正式地址（3000，即 caddy -> 13000）。
之后改用"先指向本地 sink 端口、确认 sink 收到请求再切真实目标"的验证法，
并直接改配置文件（改前留备份），此后再无越界。

主代理在正式库（只读）里定位到了这 6 条请求：

| 时间（CST） | 模型 | 渠道 | 结果 |
| --- | --- | --- | --- |
| 2026-09-29 09:44:29 | grok-4.6 | 223 | 完成，prompt 26467 / completion 231 |
| 2026-09-29 09:44:56 | grok-4.6 | 223 | 客户端已断开，未计费 |
| 2026-09-29 09:46:32 | grok-4.6 | 223 | 客户端已断开，未计费 |
| 2026-09-29 10:00:04 | grok-4.6 | 223 | 完成，prompt 26364 / completion 381 |
| 2026-09-29 10:00:29 | grok-4.6 | 223 | 客户端已断开，未计费 |
| 2026-09-29 10:00:50 | grok-4.6 | 223 | 客户端已断开，未计费 |

全部是 `grok-4.6`，全部走渠道 223，全部是审查探针（请求体是
`Reply with exactly: GROK_PROBE_OK` 一类的短提示，但上下文被上游计到 2.6 万 token）。
两次完成的请求实际产生了费用；4 次因客户端被超时杀掉而记"未收到完整计费信息"。

关于 claude 的两次探针：**未在正式库留下任何行**。按主代理当时的记录，
claude 探针用的是模型 `deepseek-v4-flash`，而正式库在 2026-09-29 09:15-09:55
这个窗口里 `token_id=5` 一条记录都没有（整个 09:15-09:55 区间为空），
09:00-12:00 区间内也没有任何 `deepseek*` 行。所以那两次 claude 探针
**没有到达正式服务**（最可能的解释是 claude CLI 侧的启动/鉴权失败，
或被本机 3001 之外的路径拒绝）。这是从日志反推的结论，不是当时观测到的，
标注为 SUSPECTED。

主代理在事后发现这一情况时立即停止了用环境变量覆盖的尝试，改用 sink 验证法，
并删除了测试机上全部带凭据的配置文件。**后续所有结论都来自隔离环境。**

## 审计方式

6 个只读子代理并行，各自出结论文件；**所有高严重度结论由主代理独立复现或推翻**，
过程与更正都保留在结论文件里。另由主代理独立完成升级风险分析（子代理未覆盖）。

| 维度 | 结论文件 | 行数 |
| --- | --- | --- |
| 协议一致性（OpenAI / Anthropic / Responses / Gemini 面） | `/tmp/audit/findings-protocol.md` | 693 |
| 真实 CLI 客户端（codex / claude / grok / omp） | `/tmp/audit/findings-cli.md` | 572 |
| 计费与阶梯表达式 | `/tmp/audit/findings-billing.md` | 360 |
| 健壮性与重启韧性 | `/tmp/audit/findings-robustness.md` | 527 |
| 路由、失败转移、渠道选择 | `/tmp/audit/findings-routing.md` | 447 |
| 认证、授权、密钥处理 | `/tmp/audit/findings-security.md` | 634 |
| 升级风险（主代理） | `/tmp/audit/findings-upgrade.md` | 258 |

（这些文件是本轮的原始工作材料，位于 `/tmp/audit/`，不随仓库保留；本记录已把
每一条可复现结论的请求、响应、源码位置和命令都写进来，可以脱离这些文件独立复核。）

真实客户端在测试机 10.0.91.3 上运行，经 SSH 反向隧道访问本机 3001：codex 0.158.0、
claude 2.1.284、grok 1.0.41、omp 18.4.2。测试机本身的配置全部指向 `127.0.0.1:3001`
（隧道出口），测试机上不存在指向正式环境的配置；但见上一节，
审查早期本机两个 CLI 因环境变量覆盖静默失效而打到过正式地址。

## 一、确认为真缺陷（主代理已独立复现）

### 1.1 P1 — `/v1/responses` 上 21 个 codex 系渠道全部对无 `x-codex-*` 客户端判死

`controller/relay.go:1236` 在候选预检阶段调用 `relay/common/override.go:636`
`MissingCodexDynamicRequestHeaders`。触发条件只有两条：请求是 `/v1/responses`
（或 `/compact`），且候选渠道启用了 codex 系 `header_profile_strategy`，
而**入站请求没有任何非空 `x-codex-*` 头**。

关键点：`override.go:678-683` 的兜底 `if !hasCodexDynamicHeader { missing = append(missing, "x-codex-*") }`
是**无条件**的。`pass_headers` 只能满足"入站已有的动态头必须被透传"这一半，
无法凭空满足"必须至少有一个 `x-codex-*`"这一半。所以受影响的不是"没配 pass_headers 的渠道"，
而是**全部 21 个 codex 系渠道**。

隔离环境逐头实测（同一请求，只改请求头）：

| 入站请求头 | 结果 |
| --- | --- |
| （无） | 503 `... requires unconditional pass_headers ... x-codex-*` |
| `Session-Id` / `Session_id` / `x-client-request-id` / `Originator` | 503，仍报 `x-codex-*` |
| `x-codex-foo`（不在透传清单） | 503，报 `x-codex-foo` |
| `x-codex-window-id` | 200 |

作用域收窄：`controller/relay.go:1212` 开头即 `RelayMode != Responses/ResponsesCompact -> return false`，
所以 `/v1/chat/completions` 与 `/v1/messages` 完全不受影响。

生产暴露面（只读，2026-09-29 快照）：可路由模型 97 个，其中 **91 个**的全部候选渠道都是 codex 系，
只有 6 个（`deepseek-v4.1-flash`、`deepseek-v4-flash`、`deepseek-v4-pro`、`glm-5.3`、
`glm-5.3-flash`、`kimi-k3`）因为恰好在渠道 76（claude-code profile）上有能力而幸免。
91 个中近 7 天在 `/v1/responses` 上有真实流量的有 5 个：`gpt-6-luna` 4296 次、
`gpt-5.6-luna` 755 次、`gpt-5.6-terra` 179 次、`gpt-6-sol` 11 次、`grok-4.6` 6 次。

生产当前主流量是真实 Codex 客户端（带 `x-codex-*`），所以**升级不会打断现状**；
受影响的是非 Codex 客户端（OpenAI SDK / curl / 第三方工具）走 `/v1/responses` 的场景。

### 1.2 P1 — 硬杀（SIGKILL）永久丢失配额扣减，但日志行已落库

主代理独立复现（隔离环境，独立 token，真实渠道）：

```
基线                             remain=50000000 used=0     logs 0 行 sum=0
连发 3 个 200 请求后立即查库      remain=49999991 used=9     logs 3 行 sum=27
docker kill -s KILL + docker start
+0s / +25s / +50s / +75s          remain=49999991 used=9     logs 3 行 sum=27
```

差值恒为 18，永不收敛。3 个请求都返回 200 并被记入 `logs`，但配额从未扣减。

机制（静态核对）：`model/token.go` / `model/user.go` 的扣减走 `addNewRecord` 写进程内存 map；
`model/utils.go:38-44` 的 `InitBatchUpdater` 是 `batchUpdate()` 的唯一调用者（ticker）；
`main.go:154-158` 在 `BATCH_UPDATE_ENABLED=true` 时启动该 ticker；
`main.go:253-262` 收到 SIGTERM 后只做 `srv.Shutdown`，**不 flush**。
Redis 不持有配额 delta 键，所以没有任何可回放来源；启动路径也不做
`tokens.used_quota` 与 `logs` 的对账。

暴露窗口：由 `BATCH_UPDATE_INTERVAL` 决定，默认 **5 秒**（`common/init.go:119`），
不是子代理初版写的 60 秒（`SYNC_FREQUENCY=60` 只驱动 `SyncChannelCache` 与 `SyncOptions`，
`main.go:103,107`，与配额无关）。风险量级下调一个数量级，但"永久、静默、
只能靠 logs 表看出"这三个性质不变。

现实触发方式：`main.go` 的优雅退出窗口默认 120 秒，而 compose 默认 stop 宽限 10 秒，
慢请求下 `docker compose stop/restart` 可能走到 SIGKILL 路径。

### 1.3 P1 — `GET /v1/models/{model}` 对已存在且可路由的模型返回 200 + "does not exist"

```
GET /v1/models/gpt-5.5   ->  HTTP 200
{"error":{"message":"The model 'gpt-5.5' does not exist","type":"invalid_request_error",
          "param":"model","code":"model_not_found"}}
```

`controller/model.go:365` `RetrieveModel` 查的是静态目录 `openAIModelsMap`
（由各 adaptor 的 `GetModelList()` 拼出），而不是网关实际可路由的模型集合。
`gpt-5.5` / `gpt-5.6-sol` / `grok-4.6` 都命中这个分支；`deepseek-v4-flash` /
`gpt-4o` / `text-embedding-3-small` 因为恰好在静态目录里而正常返回模型对象。
后者中有两个根本不在 `GET /v1/models` 的 64 条里 —— 也就是说这个端点回答的是
"静态目录里有没有"，既不是"网关有没有"，也不是"你能不能调"。

双错：状态码 200 配错误体（OpenAI SDK 会当成成功并给出全零模型对象），
且消息本身是假的。这是本轮最容易被 SDK 踩到的一条。

### 1.4 P1 — `/v1/completions` 返回 404 配内部错误体

```
POST /v1/completions  ->  HTTP 404
{"error":{"message":"openai_error","type":"bad_response_status_code",
          "param":"","code":"bad_response_status_code"}}
```

路由是注册的（`router/relay-router.go:106`），所以不是"端点不存在"，
而是失败在 relay 内部。`message` 是内部哨兵串，`type`/`code` 是网关自己的状态码分类，
调用方拿不到任何可行动信息。同一分类串还泄漏到 `/v1/messages` 的流式错误（见 1.5）。

### 1.5 P1 — SSE 头配非 SSE 正文（`/v1/messages`，主代理给出确定性复现）

> **更正（2026-09-30）**：触发条件原先写得过宽。未知模型
> （`no-such-model-xyz`）在 `stream:true` 下返回的 `Content-Type` 是**正确的**
> `application/json`，因为请求在 distributor 层就被拒绝，没有走到
> `SetEventStreamHeaders`。本条只在**模型存在、且上游对 `/v1/messages` 返回 4xx**
> 时复现（下例的 `gpt-image-2`）。详见
> `CR-FULL-SCENARIO-AUDIT-FIXES-2026-09-30.md` 第三节。

子代理原始记录依赖上游偶发 502，主代理找到确定性触发：

```
POST /v1/messages  {"model":"gpt-image-2","max_tokens":8,"stream":true,
                    "messages":[{"role":"user","content":"hi"}]}
-> HTTP/1.1 400 Bad Request
   Content-Type: text/event-stream
   Transfer-Encoding: chunked
   正文首字节是 `{`，无任何 SSE 帧：
   {"error":{"type":"<nil>","message":"This model is not supported on the Chat Completions endpoint ..."},"type":"error"}
```

同一个请求 `stream:false` 时返回 `application/json; charset=utf-8`。

机制：`relay/channel/api_request.go:986` 在 `info.IsStream` 时先调
`helper.SetEventStreamHeaders(c)` 写死 `text/event-stream`；失败后
`controller/relay.go:490-492` 的 Claude 分支只做 `c.JSON(status, ...)`；
而 `gin@v1.9.1 render/render.go:36` 的 `writeContentType` 只在 header 为空时才写，
已存在的 `text/event-stream` 不会被覆盖。

顺带暴露两个形态问题：`error.type` 渲染成字面量 `<nil>`，以及同一对象里
同时出现 `"type":"<nil>"` 与顶层 `"type":"error"`。

> **更正（2026-09-30）**：`<nil>` 的归因原先指错了分支。产生它的是
> `ErrorTypeOpenAIError` 分支的 `fmt.Sprintf("%v", openAIError.Code)`
> （`types/error.go:320`，`OpenAIError.Code` 是 `any`，上游 body 无 `code` 字段时为 nil）；
> `new_api_error` 分支永远产出字面量 `"new_api_error"`，不可能产出 `<nil>`。

### 1.6 P1 — 同一模型在会话内切换渠道时上游 `usage` 结构不同，网关原样转发

子代理报 grok 1.0.41 在多轮里崩于 `serialization error: missing field 'output_tokens_details'`，
但渠道归因只是推断。主代理用 `Authorization: Bearer <key>-<channelId>` 钉渠道后证实：

```
POST /v1/responses  {"model":"grok-4.6", ...}     （非流式）
  ch=223  usage keys = [context_details, cost_in_usd_ticks, input_tokens,
                        input_tokens_details, num_server_side_tools_used,
                        num_sources_used, output_tokens, output_tokens_details, total_tokens]
  ch=209  usage keys = [input_tokens, input_tokens_details, output_tokens, total_tokens]
```

流式同样：ch=209 的 `response.completed.usage` 只有 4 个基础字段。
`/v1/chat/completions` 面不会暴露这个问题（网关把 usage 归一化成标准 OpenAI 形状），
漂移只在 Responses 面出现 —— 这也解释了为什么报错出现在 Responses 客户端上。

生产暴露面（只读）：启用渠道中含 `pass_headers` 的只有 7 个
（115/116/119/120/130/141 与 223）。Responses 面的守卫要求"渠道必须透传入站的 `x-codex-*`"，
所以漂移只可能发生在两个都配了 pass_headers 的渠道之间。grok 系只有 223 一个，
**生产上 grok 不可能漂移**；而 `gpt-5.4*` / `gpt-5.5` / `gpt-5.6*` / `gpt-6*`
由那 6 个 `mynav.website-*` 渠道共同承载，**条件已具备**。这 6 个渠道上游的实际
usage 结构未取得（需真实请求），所以"是否真的会漂移"仍是 SUSPECTED。

### 1.7 P1 — `n > 1` 被静默忽略

```
POST /v1/chat/completions  {..., "n":3, "max_tokens":8}
-> HTTP 200，choices 长度 1，indices=[0]，completion_tokens=6
```

复现 2 次（`gpt-5.5`），子代理另在 `kimi-k3` 上复现。`dto/openai_request.go:53` 已经解析了
`N *int`，但没有使用。同一仓库的 Responses 兼容路径
（`service/openaicompat/chat_to_responses.go:218`）对 `n>1` 是显式硬失败
（`"n>1 is not supported in responses compatibility mode"`），两条路径口径不一致。

### 1.8 P1 — failover 成功后只持久化失败的那次尝试

`logs` 里记的是 503 与失败渠道，而客户端实际收到 200。
主代理独立复现的归因错位：

```
[ERR]  ... | relay error: status_code=503, responses opaque replay param override unsupported: channel #141 ...
[INFO] ... | record error log: userId=1, channelId=72, modelName=gpt-5.6-sol ...
```

排障时按 `logs.channel_id` 会追到 72，而真正报错的是 141。
`admin_info.use_channel=["72"]` 也只记录初始选中渠道。这与 `RetryTimes` 无关，是独立缺陷。

### 1.9 P1 — 有 retry 预算时确定性上游 404 被改写成 500

`controller/relay.go` 的 retry 收尾把最后一次失败重新归类为 `get_channel_failed` / 500，
丢掉了上游原始的确定性 404。子代理在 `RetryTimes=3` 下复现。

### 1.10 P1 — `/v1/embeddings` 透传上游 SDK 异常

```
POST /v1/embeddings  {"model":"agnes-image-2.0-flash","input":"hello"}
-> HTTP 503
{"error":{"message":"***.ServiceUnavailableError: ServiceUnavailableError: OpenAIException - no available server",
          "type":"internal_server_error","param":"","code":"503"}}
```

`code` 把 HTTP 状态码当字符串回显，`type` 又是第三种词表。
主代理复现一致。

### 1.11 P2 若干（已复现，非阻断）

- **错误体词表不统一**：同一网关同时出现 `new_api_error`、`bad_response_status_code`、
  `internal_server_error`、`<nil>`，没有一个是 OpenAI 的标准集合。
- **Go 类型泄漏**：`"json: cannot unmarshal string into Go struct field GeneralOpenAIRequest.max_tokens of type uint"`
  把内部结构体名发给调用方；同时 `common.MaskSensitiveInfo` 把 `token quota is not enough`
  改写成 `token *** is not enough`（`common/str.go:34` 的 `maskAuthTokenPattern` 把
  `token quota` / `token remain` 当成凭证）。主代理用独立 Go 程序验证了后者：
  `"token quota is not enough, token remain quota: ..."` -> `"token *** is not enough, token *** quota: ..."`，
  而 `"token usage exceeded"` / `"this token is not valid"` 不受影响（在 allowlist 里）。
- **未知模型返回 503 而非 404**，且预派发拒绝不写 `logs` 行（主代理复现：`logs` 行数 0）。
- **`temperature` 不做范围校验**：`-1` / `3` / `100` 全部 200 通过（主代理复现）。
  > **更正（2026-09-30）**：`3` 未复现 —— 隔离环境 `curl` 返回 `HTTP 000`（客户端超时），
  > 不是 200。`-1` 与 `100` 通过为真，所以范围校验本身仍然要做；
  > `-1` / `3` / `100` 原先全部记为 200 通过，数字是错的。
- **429 无 `Retry-After`、无 `X-RateLimit-*`、无响应体**：主代理在 `GET /api/status` 上
  压到第 179 次触发 429，响应 `Content-Length: 0`，无任何限流元数据；
  `middleware/rate-limit.go` 中 `Retry-After` 出现 0 次。
- **`GET /api/channel/` 向任意 admin 返回上游 `base_url` 与 `header_profile_strategy`
  的完整 headers**。渠道 key 本身被 `RootAuth` + `SecureVerificationRequired` 正确挡住；
  泄漏的是 `settings` JSON blob。当前快照里的 header 值都不是密钥（客户端身份伪装头），
  但用户自定义 profile 若含鉴权头会一并明文返回。
- **`group` 字符串在创建时不校验**：非管理员可写入 `../../etc/passwd`，落库原样保存，
  使用时被 `middleware.TokenAuth` 拒绝（主代理复现 `403 无权访问 ../../etc/passwd 分组`），
  即惰性但无穿越、无提权。
- **CORS `Allow-Origin: *` 配 `Allow-Credentials: true`**：Fetch 规范禁止的组合，
  `AllowCredentials` 是死配置；带凭据的浏览器跨域调用会被浏览器拒绝。
- **`/v1/models` 的 `supported_endpoint_types`**：`agnes-image-2.0/2.1/2.5-flash` 都标了
  `image-generation`；2.0 已实测可生成图片，2.1/2.5 未实测。反向问题是
  `gpt-image-2` 在列表里但 `/v1/images/generations` 因分组开关返回 403，列表未体现这一点。
- **`logs` 的 `other.model_ratio` / `completion_ratio` 在 tiered_expr 行上写 0**：
  `bin/repair_channel_quota_logs.go` 会按 `billing_mode` 分支，未造成错误修复；
  但任何其它消费方直接套 `docs/operations/pricing-maintenance.md` 的公式会算错。

## 二、经主代理复现后**推翻或更正**的子代理结论

| 子代理结论 | 主代理结论 |
| --- | --- |
| 路由 F5b：钉渠道 `Bearer <key>-<id>` 不能覆盖 route scope | **REJECTED**。逐渠道钉死复现：`gpt-5.6-luna` pin=114 -> 落 114、pin=221 -> 落 221（3 次里 2 次成功，1 次 curl 超时）、pin=1/pin=72（无该模型能力）-> 404 且落库 channel_id 就是 1/72；`grok-4.6` pin=209 -> 209、pin=178 -> 178。包括"被钉渠道没有该模型能力"的负例都稳定遵守。 |
| 路由 F3：`/v1/responses` 对 63/64 模型 503，P0 | **更正为 21 个渠道 / 生产 91 个模型，P0 -> P1**。`override.go:678-683` 的兜底是无条件的，`pass_headers` 救不了；且原统计按 `/v1/models` 宣称集合算，应按 abilities 全量算。 |
| 路由 F1：`RetryTimes=0` 是"默认契约"，生产无 failover，P1 | **更正为 P2**。dev `RetryTimes=0`，**生产 `RetryTimes=3`**。主代理在隔离环境把 RetryTimes 临时改成 3 后复现了梯队递进（改前 6/6 全落 ch225 全 403，改后 3/3 成功落 ch76）。已还原为 0 并复核 dev=0 / prod=3。 |
| 路由 F2：梯队内无加权分流，P1 | **降为 P2**。这是配置形态（每个梯队只给一个渠道 10000 bps），不是代码缺陷。 |
| 协议 F-18：客户端断流不取消上游、用户为废弃请求付费，P1 | **更正为 P2**。7 次独立 kill 复现显示取消确实向上游传播，`end_reason=client_gone`，按已产出 token 结算。 |
| 健壮性 F10：入站无请求体上限，100 MB 被接受，P2 | **REJECTED**。限制存在且生效：`middleware/gzip.go:239-241` 包 `http.MaxBytesReader`，`common/gin.go:47-57` -> `constant.MaxRequestBodyMB` 默认 128。140 MiB 请求返回 400 `request body exceeds 128 MB`。 |
| 健壮性 F11d：丢失窗口 ≤60 秒 | **更正为 ≤5 秒**（`BATCH_UPDATE_INTERVAL` 默认 5；`SYNC_FREQUENCY=60` 与配额无关）。 |
| CLI C-3：P0 | **降为 P1**。只在需要"第二次模型调用携带 `reasoning.encrypted_content`"的轮次上触发；单次模型调用即可完成的任务成功。 |
| CLI C-9：该 400 只出现在 CLI stderr | **更正**。400 会打到容器 stdout 与 CLI，但**不落 `logs` 表**（`controller/relay.go:214` 的提前返回发生在渠道选择之前）。主代理用 Codex turn 元数据 + `x-codex-window-id` 复现：`logs` 行数 0，stdout 有 `[ERR] ... status_code=400, tiered expression billing is unsupported for opaque encrypted Responses replay`。 |
| CLI C-5/C-8：渠道归因 SUSPECTED | **升级为 CONFIRMED**，见 1.6。 |

## 三、验证为正常的部分（负结论，同样重要）

每条都标注证据来源：**主代理**＝本轮由主代理亲自跑出；**子代理**＝只有子代理的
执行记录，主代理只核对了它的证据形式（原始字节、落库行、命令输出），未重跑。

- **计费算术**［子代理，主代理另在 4 条 CLI 请求上独立复算一致］：10 个独立请求上按表达式重算的 quota 与落库值**逐条精确相等**，
  覆盖 flat、长度边界、时间窗（peak/off_peak 分支真的在切）、cache read、
  零补全、跨阶梯、退款、预扣费/结算一致。无 P0/P1 计费缺陷。
- **认证与授权**［子代理；主代理独立复现了 `New-Api-User` 缺失 401、`GET /api/channel/` 无 admin 头 401，以及伪造 group 的 relay 拒绝］：`New-Api-User` 强制（缺失 401、不匹配 401、正确 200）；
  token 模型范围限制；禁用/过期 token 拒绝；跨用户隔离；渠道 key 不下发；
  分组限制无静默回退 —— 全部按文档成立。
- **畸形输入**［子代理］：13 例全部干净 400，无 500、无 panic。
- **上游异常**［子代理］：HTML / 纯文本 / 垃圾 / 空体一律被重新包装成合法 JSON 错误，无 HTML 或 panic 泄漏。
- **并发**［子代理］：20 并发无 500，20 行日志，配额增量精确，无重试放大。
- **流式协议三方言**［主代理］：`/v1/chat/completions` 以 `data: [DONE]` 收尾、
  `/v1/responses` 以 `response.completed` 收尾、`/v1/messages` 以 `message_stop` 收尾，
  逐个按原始字节核对，全部正确。
- **工具调用**［子代理］：非流式与流式的 `tool_calls` 形状与增量 `arguments` 按 `index` 重组都符合 OpenAI 契约。
- **模型列表**［主代理部分复现］：`GET /v1/models` 宣称集合与库内可路由集合精确一致（扣除 `mock-*` 与计费配置过滤后）；主代理独立核对了 64 条宣称集合的成员资格，另发现 `GET /v1/models/{model}` 走的是静态目录而非这个集合（见 1.3）。
- **真实 CLI**［子代理执行，主代理核对了请求 id 与落库行］：codex 0.158.0 单轮、claude 2.1.284 单轮与工具调用多轮、omp 18.4.2
  单轮与工具调用多轮全部通过；四个 CLI 都产生了 `is_stream=t` 的真实流式行。

## 四、升级风险（生产 `4527d7666` -> 工作区 `ef093ab64`）

差距：52 个提交、824 文件、+148965/-16435 行；`4527d7666` 是 HEAD 的祖先。

| # | 风险 | 严重度 | 状态 |
| --- | --- | --- | --- |
| U1 | 升级后 `/v1/responses` 上 91 个模型对无 `x-codex-*` 客户端 503（21 个 codex 渠道全被判死） | P1 | CONFIRMED |
| U2 | 启动期 21 次 `DROP INDEX` 在生产量级未演练 | 已知残留风险 | CONFIRMED（既有记录 `CR-LOG-INDEX-PROD-VERIFY-2026-09-23.md:135-152` 已列为唯一未消除风险） |
| U3 | 渠道列表默认排序改为 `pin_order`（`migrateChannelPinOrder`，marker `migration.channel_pin_order.20260829`） | P1 | CONFIRMED |
| U3b | 新增 `abilities` 4 列（带索引）+ `channels.max_concurrency` + `midjourneys.private_data` | P2 | CONFIRMED |
| U4 | V2 路由字段不会自动回填；生产 915 行 abilities 全部 `route_priority_configured=false`，且无 V2 marker | P1 | CONFIRMED |
| U5 | 升级不会意外启用自动禁用或并发闸门 | 已验证安全 | CONFIRMED |

U1 详见 1.1。U2 本次只读量化：生产 `logs` 2665009 行 / 13 GB / 41 个索引，
待删的 21 个索引合计约 4.3 GB；`comm -12` 双向核对 21/21 命中删除清单，
HEAD 声明的 19 个索引名生产全部已存在，所以净效果只删不建。
gorm postgres 驱动的 `DropIndex` 是裸 `DROP INDEX`（取 ACCESS EXCLUSIVE 锁），
而仓库既有的 `scripts/storage-log-index-maintenance.sh` 走的是
`DROP INDEX CONCURRENTLY IF EXISTS` —— 启动期迁移不享受这个安全性。

U3：`model/main.go:460-494` 把 `priority` 复制进 `pin_order`（只对 `priority>0` 生效）。
生产 78 行渠道 `priority` 全部 > 0，所以升级后 78 行全被写入，第一版列表顺序不变；
但此后 `priority` 与 `pin_order` 成为两个独立字段，默认排序子句也换成
`COALESCE(pin_order,0) DESC, COALESCE(priority,0) DESC, id DESC`。
生产 `CHANNEL_ROUTING_V2_ENABLED=false`，`IsChannelRoutingV2StrictWrites()` 默认取它的值，
所以严格写守卫默认关闭，`priority` 仍可写、只是不再影响列表排序。**管理端语义变更，
升级说明必须写明**，否则管理员会以为"改了 priority 但顺序没动"是 bug。

U4：`model/channel_route_selector.go:50-56` 的 `buildRouteCandidateCacheV2` 会跳过所有
`configured=false` 的行，候选集为空 -> 全模型不可路由。回填入口是显式的
`GET /api/channel-route-scopes/migration/preview`（dry-run）与
`POST /api/channel-route-scopes/migration/apply`（写库 + InitChannelCache + 审计日志；
遇到任何冲突整事务回滚并返回 409，是正确的 fail-closed）。**若计划开启 V2，必须先 apply。**

### 升级前建议清单

1. **[U1]** 先定口径：接受"非 Codex 客户端在 `/v1/responses` 上不可用"，
   还是改守卫逻辑 / 给这些渠道换 profile。当前 91 个模型会全渠道失效。
   补 `pass_headers` **不能**解决 —— 兜底是无条件的。
2. **[U2]** 先在同等数据量库上演练 21 次 `DROP INDEX`，或先手工
   `CONCURRENTLY` 删除并写入 marker。
3. **[U4]** 若计划开 `CHANNEL_ROUTING_V2_ENABLED`，先跑
   `POST /api/channel-route-scopes/migration/apply`（先 `GET .../preview` 看报告）。
4. **[U3]** 在升级说明中写明渠道列表默认排序改为 `pin_order`，
   并确认管理员对 `priority` 排序的依赖已迁移到 `/api/channel/pin_orders`。
5. **[U3b]** 升级后确认 `channels.max_concurrency` 全为 0
   （`service/channel_concurrency.go:315-317` 中 0 表示不限制，闸门不会因升级而激活）。

## 五、环境还原

隔离库的临时改动已全部还原，逐项复核：

| 项 | 处理 | 复核 |
| --- | --- | --- |
| 14 个渠道被补的 `pass_headers`（1/72/114/150/171/178/204/209/211/215/218/219/221/225） | 用正式库对应渠道的 `param_override` 覆盖回去 | 逐渠道语义 diff（operations 的 mode 序列）与生产完全一致 |
| 渠道 209/223 的 `header_profile_strategy.enabled` 临时置 false | 用正式库的 `settings` 覆盖回去 | 两者均为 `true`，与生产一致 |
| `options.RetryTimes` 临时改 3 | 改回 0 | dev=0，prod=3 |
| 20 个审查用 token（`audit-*` / `billing-audit-tmp-*`） | 删除 | 剩余 16 个 token 全部是审查前既有 |
| Redis 限流桶 `rateLimit:GA*` / `rateLimit:GW*` | 删除 | 瞬时计数器，会自然回填 |
| 140 MiB 请求体测试文件 | 删除 | `/tmp/audit/big*` 已清 |

测试机 10.0.91.3 的审查痕迹已全部清除：反向隧道已停（无残留 ssh 进程）、
`~/.audit-runtime`（2.1 GB，含四个 CLI）已删、审查期间创建的 `~/.codex` /
`~/.claude` / `~/.grok` / `~/.omp` / `~/.npmrc` / `~/.claude.json` / `~/.npm` 已删
（逐个用 `find ! -newermt 2026-09-29` 确认过这些目录内没有任何早于本轮的既有文件）、
`~/.zsh_history` 已删（本轮所有 ssh 命令都经它落盘）。清除后全盘 grep 中继令牌
与 `127.0.0.1:3001` 均为空。

本机个人配置已还原：

| 文件 | 处理 | 复核 |
| --- | --- | --- |
| `~/.grok/config.toml` | 从它自己的 `config.toml.bak_audit` 还原 | `models_base_url` 回到 `http://127.0.0.1:3000/v1` |
| `~/.omp/agent/models.yml` | 3 处 `baseUrl` 由 3001 改回 3000 | `grep baseUrl` 三处全部是 3000 |
| `~/.claude/settings.json` | **从未修改** | mtime 仍是 2026-09-21，内容里无 3000/3001 |

`~/.omp/agent/models.yml` 没有留独立备份（当轮的 `/tmp/audit/models.yml.orig`
已随临时目录清理而不在），但还原是**可核验的**：改回 3000 后文件为 3306 字节，
与审查开始前 `ls -la` 记录的 pre-audit 大小（3306，mtime Sep 14 17:40）一致；
把当前文件与审查期间指向 3001 的那份做逐行 diff，差异**只有端口**
（那份副本在 `/tmp/audit/models.yml.audit-3001`，`/tmp` 会被系统清理，不保证长期可查）。

其他需知情项：

- 隔离库存在 `migration.channel_pin_order.20260829` 与 `migration.log_index_cleanup.20260921`
  两个 marker，是容器重启时启动迁移正常写入的，不是审查副作用。
- `users` 表有 user 73 `billingaudit01`（role 1），是计费子代理在本轮创建的
  测试用户，其 token 已删、只剩 17 行历史日志。**未删除**，因为删用户会连带影响
  日志归属，交由用户决定。
- 测试机 10.0.91.3 上被删掉的四个 CLI 与 omp/bun 运行时是审查期间安装的，
  该机器原本没有这些工具；如需保留需重装。

仓库工作区：`git status --short` 461 条，全部是审查开始前就存在的其它会话改动，
本轮**没有修改任何仓库源文件**（本文件除外）。

## 六、未能覆盖的边界（明确说明，不当作已通过）

- **前端 `web/default` 与 `web/classic` 的真实点击路径**未走浏览器验证，
  只做了代码阅读。AGENTS.md §4 要求的亮/暗主题与移动端检查未执行。
- **三库兼容差异**未在本轮复测（既有 `CR-FULL-DIMENSION-AUDIT-2026-09-23.md`
  已有 MySQL 8.4 实测记录，本轮未重跑）。
- **`ModelRequestRateLimit`** 未触发：隔离环境该开关为 false 且无相关 option 行，
  其 429 路径仅靠代码阅读。
- **OAuth / CAS / passkey / 2FA / Trusted Header** 认证路径未测试。
- **C-8 在生产上的实际发生性**：那 6 个 `mynav.website-*` 渠道上游的 usage 结构未取得，
  所以"生产上 gpt-* 系是否真的会漂移"是 SUSPECTED，只能确定条件已具备。
- **`DROP INDEX` 的生产耗时**未实测（U2 本身就是这样一条未覆盖边界）。
