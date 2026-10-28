# 附加审计发现（2026-10-27 至 2026-10-28）

在 CR-AUDIT-FIXES-PACK-2026-10-27 的 20 项修复之后，对认证、OAuth、计费边界、中继
错误路径与限流做的一轮补充审计。发现与验证方式记录在下方各条目；当时的动态复现
全部通过 `/tmp` 下的 overlay 测试完成，未写入仓库。文末"修复记录"是本轮 14 项修复
落地后的提交、测试与复验命令。

采样时间：2026-10-27 夜至 2026-10-28 凌晨，本机 macOS，dev 分支。
基线：`git rev-parse HEAD` 为 66f324e60。

## 结论概览

| 级别 | 数量 | 已动态复现 | 仅代码确认 |
|------|------|-----------|-----------|
| P0 | 1 | 1 | 0 |
| P1 | 4 | 2 | 2 |
| P2 | 9 | 0 | 9 |

动态复现均通过 `/tmp` 下的 overlay 测试完成，未写入仓库。

## P0

### 1. 桌面端 OAuth 登录交接可被未认证攻击者用于接管账号

`controller/oauth_desktop.go` 的 login 模式不要求调用方已登录：任何客户端调用
`GET /api/oauth/desktop/start?provider=<slug>` 都会拿到一对 `state` 与
`handoff_token`。`controller/oauth.go` 的回调在 state 命中桌面端存储时跳过浏览器
会话的 state 校验，并把**回调浏览器**的 IdP 身份解析出的本地用户 id 写入该
handoff（`completeDesktopOAuthRequest`）。随后 `PollDesktopOAuth` 对持有
`handoff_token` 的调用方调用 `setupLoginWithResult` 签发会话。

攻击链：

1. 攻击者无会话发起桌面登录，拿到 `state` 与 `handoff_token`。
2. 攻击者把带自己 `state` 的 IdP 授权链接发给受害者。受害者浏览器若已登录该
   IdP（GitHub 等已授权过应用时无二次确认），会直接回调网关。
3. 回调以受害者的 IdP 身份完成 handoff，写入受害者的本地用户 id。
4. 攻击者用自己的 `handoff_token` 轮询，拿到受害者账号的会话 Cookie。

启用 2FA 的账号会被 `setupLoginWithResult` 的门控挡住（攻击者拿到
`require_2fa`），其余账号可被接管。

验证：overlay 测试 `TestVerifyDesktopOAuthLoginHandoffTakeover`（本次审计编写）
通过。攻击者无 Cookie 启动，受害者浏览器无网关会话完成回调，攻击者轮询返回
`"username":"verify-victim"` 且响应带 1 个 Set-Cookie。

修复方向：把 state 绑定到完成回调的浏览器，例如由桌面端在系统浏览器中打开网关的
start URL（浏览器发起并拿到 HttpOnly Cookie，再跳转 IdP），回调要求该 Cookie；
或改用设备码式的确认流程。仅靠 `handoff_token` 保密无法区分受害者的浏览器与
合法桌面端。

## P1

### 2. 流式扫描器超时返回后，遗留 data handler 继续写 gin.Context

`relay/helper/stream_scanner.go` 的 defer 只等待 `wg.Wait()` 5 秒，超时后仅记录
`timeout waiting for goroutines to exit` 并照常返回。data handler 在
`writeMutex` 内调用 `dataHandler`，后者经 `helper.StringData/ObjectData` 写
`c.Writer`；ping 协程每次 tick 还额外 `gopool.Go` 一个不在 `wg` 内的写协程，超时
后它可能仍持有 `writeMutex`。

`main.go` 明确不设置 `WriteTimeout`（流式响应需要），客户端停读时 TCP 窗口打满，
`c.Writer.Write` 可长时间阻塞。函数返回后 gin 把 Context 放回 `sync.Pool`；
gin v1.12.0 的 `Context.reset` 会把 `c.Writer` 重新绑到新请求的连接。遗留协程之后
继续执行的 `dataHandler` 会把旧流的 SSE 帧写进另一个请求的响应。

验证：overlay 测试 `TestVerifyScannerReturnsWhileHandlerStillWriting`（本次审计
编写）显示 `StreamScannerHandler` 在 5.00s 返回时 data handler 只完成了 1 次调用，
返回 5 秒后完成到 3 次，日志出现 `timeout waiting for goroutines to exit`。
gin 的池化与 `reset` 语义在 `gin@v1.12.0/gin.go:667-674`、
`context.go:103-105` 核实。

对照：`relay/channel/api_request.go` 的 `startPingKeepAlive` 在停止时会
`pendingPings.Wait()`，保证返回后不再有人写 `c.Writer`；扫描器这条路径没有等价
保证。

### 3. OpenAI 流内 error 帧的状态码被 HandlerStop 分支遮蔽

`relay/channel/openai/relay-openai.go` 中，data handler 对 error 帧调用
`sr.Stop`，而 `StreamStatus.SetEndReason` 是 `sync.Once` 语义（先到先得），必然
先记下 `StreamEndReasonHandlerStop`。函数返回后的第一个判断
`EndReason == HandlerStop && EndError != nil` 直接返回
`ErrorCodeBadResponseBody` + 502，早于后面的 `streamFrameError` 分支，于是
`frameErrorStatusCode` 的 429（rate_limit_exceeded）与 402（insufficient_quota）
映射永远走不到。

`ErrorCodeBadResponseBody` 在 `alwaysSkipRetryCodes` 中，渠道限流观测也看不到
429。现有测试 `relay_openai_stream_error_frame_test.go` 的 fixture 总在末尾追加
`data: [DONE]`，扫描器先设 `Done`，测试才碰巧走到映射分支；真实上游的 error 帧是
最后一帧，后面没有 `[DONE]`。

验证：overlay 测试 `TestVerifyErrorFrameWithoutDone`（本次审计编写，body 只有
内容帧与 error 帧）返回 `status=502`；同一 fixture 加回 `[DONE]` 即返回 429。

### 4. 微信与 Telegram 登录没有浏览器绑定（登录 CSRF）

`controller/wechat.go` 的 `WeChatAuth` 只读取 `code` 并直接 `setupLogin`，没有
state 或 nonce 概念；`controller/telegram.go` 的 `TelegramLogin` 校验签名 payload
后直接登录，一次性消费只防重放，不防把未消费的 payload 转送给受害者。两者都注册
为公开 GET（`router/api-router.go` 的 `/oauth/wechat`、`/oauth/telegram/login`）。

攻击者用自己的微信或 Telegram 身份拿到 code/payload 后，把回调 URL 发给受害者；
受害者浏览器访问即被登录进攻击者的账号，之后在该账号内创建的令牌、充值与数据都
归攻击者所有。对照 `/api/oauth/:provider` 的 state 机制，这两条路径没有等价保护。

验证：代码路径确认；未连接真实微信/Telegram 服务做端到端复现。

### 5. 统一 OAuth bind 路径不消费 oauth_state

`setupLoginWithResult` 在会话签发时集中调用 `consumeOAuthState`，所以登录路径没有
重放窗口。但 `controller/oauth.go` 的 `handleOAuthBindWithUser`（`/api/oauth/:provider`
命中已登录会话时走这里）成功绑定后不调用 `consumeOAuthState`；而 GitHub、Discord、
OIDC、LinuxDO 与 custom_oauth 各自的绑定路径都会消费。于是统一路径下，一次成功
绑定后 state 仍留在 30 天会话里。

若 state 通过 referrer、共享终端或代理日志泄漏，攻击者可在受害者浏览器用同一
state 与自己的 IdP code 再发起一次绑定，把自己的 IdP 身份挂到受害者账户，随后用
该 IdP 登录接管。前置条件是 state 泄漏，但代码注释明确把 state 定义为单次 nonce，
这条路径违反了该不变量。

验证：代码路径确认（`rg consumeOAuthState` 的调用点不含该函数）。

## P2

### 6. 实时 WS 两个读协程未 join，结算与遗留协程存在竞争

`relay/channel/openai/relay-openai.go` 的两个读协程由 `gopool.Go` 启动，函数在
`clientClosed`/`targetClosed`/`errChan`/`c.Done()` 任一命中后汇总 `sumUsage` 并
返回，不等待另一侧协程。`relay/websocket.go` 随后调用
`PostWssConsumeQuota`，后者读取 usage 时不持 `usageMu`；遗留协程仍会在
`usageMu` 下执行 `addRealtimeUsage`/`preConsumeUsage` 并写 `c`。

后果：`sumUsage` 数据竞争；结算后到达的 turn 调用 `Billing.Reserve` 时
`BillingSession.Settle` 已置 `settled`，该 turn 不再结算（漏计），或在无
BillingSession 时直接扣费（重复扣）。心跳把窗口限制在约 90 秒内，但窗口真实存在。

验证：代码路径确认；窗口较窄，未动态复现。

### 7. Gemini 空候选/被拦截：客户端收到错误，网关按成功计费

`relay/channel/gemini/relay-gemini.go` 的 `GeminiChatHandler` 在
`len(Candidates) == 0` 时向客户端写出 400/500 错误 JSON（含
`ContextKeyAdminRejectReason`），随后 `return &usage, nil`。usage 由
`buildUsageFromGeminiMetadata` 用 `info.GetEstimatePromptTokens()` 兜底，计费大于
0；`relay/gemini_handler.go` 与 `relay/compatible_handler.go` 只要 error 为 nil 就
调用 `PostTextConsumeQuota`，不检查响应是否已按错误写出。`reject_reason` 只写日志，
不参与退款或渠道失败计数。

即：客户端看到失败，用户被扣费，渠道未记失败，也不会重试。若"拦截也计费"是有意
设计，至少与客户端可见状态不一致，需要显式决定。

验证：代码路径确认。

### 8. TokenOrUserAuth 只信任会话内的 status

`middleware/auth.go` 的 `TokenOrUserAuth` 会话分支只比较 `session.Get("status")`，
不回读数据库；`UserAuth` 走的 `authHelper` 每次回读并刷新。`router/video-router.go`
的 `GET /v1/videos/:task_id/content` 使用 `TokenOrUserAuth`，管理员禁用用户后，该
用户在自己的会话 Cookie 有效期内（默认 30 天）仍可下载自己的视频。

`controller.VideoProxy` 通过 `model.GetByTaskId(userID, taskID)` 按用户过滤，只暴露
本人数据，所以定为 P2；但它违反了"禁用即时生效"的其它路径一致性。

验证：代码路径确认。

### 9. Telegram 登录与 2FA 完成登录不检查用户禁用状态

`TelegramLogin` 与 `Verify2FALogin` 在调用 `setupLogin` 前不检查
`user.Status != UserStatusEnabled`；GitHub、OIDC、Discord、LinuxDO、微信、统一
OAuth 等路径都检查。`setupLoginWithResult` 本身不做状态检查，因此被禁用账号仍能
拿到 `status=disabled` 的会话。当前 `UserAuth`/`TokenOrUserAuth` 会拒绝使用，实际
影响有限；但任何未来只校验"会话存在"的入口会继承这个缺口。

验证：代码路径确认。

### 10. DEBUG=true 时日志泄漏 IdP 令牌与用户推送凭据

`oauth/generic.go` 在 `DEBUG=true` 时把自定义 OAuth token 端点响应体前 500 字节
（含 `access_token`/`refresh_token`/`id_token`）与 userinfo 响应体写入日志；
`common/redis.go` 的 `RedisHSetObj`/`RedisHSetField` 在 `DebugEnabled` 时打印完整
用户缓存对象与字段值（`UserBase.Setting` 含 `webhook_secret`/`gotify_token`）。
默认 `DEBUG=false`，但排障时开启即持续写入 `oneapi-*.log`。

验证：代码路径确认。

### 11. 限流参数为 0 时 Redis 与内存路径语义不一致

`middleware/rate-limit.go` 的 `redisRateLimiterWithKey` 在 `maxRequestNum == 0` 时
`listLength < 0` 为假，进入 else 分支，空列表 `LIndex(-1)` 返回空串，`time.Parse`
失败直接 500；`common/rate-limit.go` 的 `InMemoryRateLimiter.Request` 在
`maxRequestNum == 0` 时首次放行、第二次起恒为 429。`middleware/model-rate-limit.go`
则明确把 0 解释为"不限制"。

运维把 `GLOBAL_API_RATE_LIMIT`、`GLOBAL_WEB_RATE_LIMIT` 或 `CRITICAL_RATE_LIMIT`
设为 0 期望关闭限流时，Redis 模式下所有请求 500，内存模式下第二个请求起 429。
默认值非 0，触发需要显式配置。

验证：代码路径确认。

### 12. 模型限流内存版把失败请求计入"成功请求"配额

`middleware/model-rate-limit.go` 的内存版用 `successKey+"_check"` 的临时键做预检，
该键记录所有请求；Redis 版用成功计数列表。于是内存模式下，连续失败的请求也会
耗尽"仅成功请求"的配额，造成与 Redis 模式不一致的误 429。

验证：代码路径确认。

### 13. 错误路径响应体未关闭

`relay/common_handler/rerank.go` 的 `ReadAllWithLimit` 失败直接返回，body 未关闭
（响应体超上限时上游正常但过大，连接无法回收）；`relay/channel/gemini/relay-gemini.go`
的 `GeminiChatHandler`/`GeminiImageHandler` 在 `io.ReadAll` 出错时返回，成功路径才
Close；`relay/channel/coze/adaptor.go` 的非流式 `DoRequest` 读完 create-chat 响应体
后成功与失败路径都不 Close。同型低危点还有 baidu、tencent、zhipu、jimeng、minimax、
mokaai、siliconflow、ali/rerank、coze 的若干读错误分支。

后果是连接与文件描述符泄漏，不影响响应内容。

验证：代码路径确认。

### 14. zhipu 非法 key 格式时完整密钥写入系统日志

`relay/channel/zhipu/relay-zhipu.go` 在 `strings.Split(apikey, ".")` 长度不为 2 时
执行 `common.SysLog("invalid zhipu key: " + apikey)`，把完整密钥打进系统日志。
该 key 对 zhipu 的 JWT 格式无效，但可能是用户误粘的其它厂商有效密钥。
`common.MaskSensitiveInfo` 已存在，可直接复用。

验证：代码路径确认。

## 已复核且未发现问题的区域

- `middleware/auth.go` 的 `authHelper`：会话每次回读用户身份并校验禁用与角色，
  `New-Api-User` 必须与会话用户一致。
- `TokenAuth`：令牌状态、过期、额度、用户状态、分组可用性检查齐全。
- Web 端 OAuth（GitHub/Discord/OIDC/LinuxDO）登录路径：state 校验且
  `setupLoginWithResult` 集中消费。
- CAS ticket 重放保护、JWT direct（拒绝 `alg=none`、强制 `exp`、issuer/audience
  校验）、trusted header（state + 对端 CIDR）。
- Passkey 登录/注册状态检查、克隆告警、step-up 门控；2FA 的 `last_used_step`
  条件写、失败锁定、备用码单次消费。
- SSRF：webhook/Bark/Gotify、视频代理、MJ 图片、文件下载均走共享 transport 并
  标记 `common.WithSSRFValidation`。
- 会话 Cookie：`HttpOnly`、`SameSite=Strict`、TLS 时 `Secure`。
- 计费重试：预扣费只发生一次，重试复用同一 `BillingSession`，最终只 `Settle`
  一次；向客户端写出内容后不再重试。
- 限流键：各限流器使用不同 mark + `ClientIP` 或 user id，不存在共用桶；反代部署
  需正确设置 `TRUSTED_PROXIES`（既有文档已记录）。

## 验证方法与边界

- 动态复现：`go test -overlay` 注入 `/tmp/newapi-verify/` 下的测试文件，覆盖
  P0（桌面端接管）、P1 第 2 条（扫描器超时返回）与第 3 条（error 帧 502）。
- 其余条目为代码路径确认，未连接真实 IdP、微信/Telegram 服务或真实 Redis 集群。
- 未覆盖：真实上游 SSE 行为、生产 Redis 多实例下的桌面 OAuth 交接、Tauri 桌面端
  的实际启动方式（若桌面端本就由系统浏览器发起 start，P0 的触发条件需要重新评估）。
- 生产容器与数据库本轮仅只读查询，未重启、未改配置。

## 修复记录（2026-10-28）

14 项发现全部修复并逐项提交，每项均为先写失败测试、确认 RED、再改实现，
并对关键项做反向验证（还原修复前形态必须失败）。

| 条目 | 提交 | 测试与验证 |
|------|------|-----------|
| 1 桌面端 OAuth 接管 | 99a185fb8 | 两阶段确认（确认码 + HttpOnly 浏览器令牌 + 同源校验 + 常量时间比较 + 失败上限）；overlay 攻击链复现转正为仓库测试 |
| 2 扫描器遗留协程 | 763e1fdbc | 返回前设置写截止并等待协程退出；`stream_scanner_lifecycle_test.go` |
| 3 error 帧状态码 | ff39fe629 | 映射分支前移到 HandlerStop 之前；`TestOaiStreamHandlerMapsTerminalErrorFrameWithoutDone` |
| 4 微信/Telegram 登录 CSRF | 3cb202431 | 两条登录路径加浏览器绑定 state 校验；`browser_bound_login_state_test.go`（含真实 HMAC 构造） |
| 5 bind 路径消费 state | 552773b48 | 成功绑定后 `consumeOAuthState`；`oauth_bind_state_test.go` |
| 6 实时 WS 读协程未 join | 555949c8c | 返回前停心跳、补发关闭帧、关闭两侧连接并 WaitGroup 等待；`realtime_handler_join_test.go`（-race 通过） |
| 7 Gemini 空候选计费 | a535e9618 | 改为返回 nil usage + 错误，走统一错误响应与退款；`gemini_empty_candidates_test.go` |
| 8 TokenOrUserAuth 会话状态 | 4cc7b4ee6 | 会话分支回读数据库校验状态；`token_or_user_auth_status_test.go` |
| 9 禁用账号签发会话 | 61b274280 | `setupLoginWithResult` 统一拒绝禁用账号；`setup_login_status_test.go` |
| 10 DEBUG 日志泄漏 | a1e405784 | OAuth 响应体与 Redis 缓存对象/字段值统一 `MaskSensitiveInfo`；捕获真实日志断言凭据不出现 |
| 11 限流 0 语义 | 746f6a434 | 两条限流路径在参数为 0 时返回放行；`rate_limit_zero_test.go`（内存 + miniredis） |
| 12 模型限流成功配额 | 2f0e95a6b | 新增只读 `Allow` 预检，失败请求不消耗成功配额；`model_rate_limit_success_semantics_test.go` |
| 13 错误路径响应体未关闭 | 4a2ab15cf、849a9192d | 首轮修 rerank/Gemini/coze；二轮把 relay/channel 全树 33 处"读后显式关闭"改为入口 defer（新增 33 处 defer、移除 29 处旧关闭，另 4 处原本就没有关闭），新增 AST 守卫 `body_close_guard_test.go` 与 baidu 运行时用例 |
| 14 zhipu 密钥入日志 | ce600dc95 | 长度 + 脱敏替代完整密钥；同包测试 |

本轮验证命令与结果（2026-10-28，dev 分支）：

```
go build ./...                                             # exit 0
go test -count=1 ./controller ./model ./relay/common ./relay/helper ./service
                                                           # 全部 ok，exit 0
go test -count=1 ./relay/... ./middleware ./common ./oauth # 全部 ok，exit 0
go test -race -run TestOpenaiRealtimeHandlerJoinsReaderGoroutines ./relay/channel/openai
                                                           # ok
```

边界：

- 前端只改 classic 与 default 的 OAuth 回调处理，未做浏览器端到端回归；桌面端
  两阶段确认的真实 Tauri 交互未验证。
- 微信/Telegram 的 state 校验只在测试内用真实 HMAC 构造验证，未连接真实服务。
- 实时 WS 的 join 用 httptest WebSocket 对端验证，未接入真实上游实时服务。
- 第 13 条的静态守卫只覆盖 `relay/channel` 树内直接 `ReadAll(resp.Body)` 的函数；
  其他目录（如 `relay/common_handler`）由各自的运行时用例覆盖。
