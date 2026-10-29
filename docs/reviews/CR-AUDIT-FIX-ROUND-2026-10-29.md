# CR-AUDIT-FIX-ROUND-2026-10-29

本轮针对第二轮审计报告的 12 项发现逐项修复。修复按原子任务拆分为 13 个提交
（`5e9a8659e` 至 `97defe3db`），每项都先写失败用例、确认 RED、实现、反向验证、
再单独提交。

审计报告的严重度排序是 1、2、3 优先，其次是 4、5、6；本轮按该顺序执行，最后
处理代码确认类（7 至 12）。

## 1. 逐项修复

### 1.1 钱包并发透支（5e9a8659e）

准入检查"读余额 -> Go 里比较 -> SQL 相对扣减"三步非原子，`quota = quota - ?`
没有 `WHERE quota >= ?` 守卫。探针实测：余额 1000、预扣 400、10 并发，7 个请求
通过准入（应为 2 个），最终余额 -1800。

- `model/user.go` 新增 `DecreaseUserQuotaGuarded`，在 SQL 内做阈值判定，
  `RowsAffected == 0` 时返回余额不足错误。
- `WalletFunding.PreConsume` 与 `BillingSession` 的补充预扣改走该入口。
- 新增 `model/user_quota_guard_test.go`（并发扣减不越过余额）与
  `service/wallet_preconsume_guard_test.go`（预扣准入与实际扣减一致）。

### 1.2 令牌缓存残缺哈希被当作有效令牌（b3f1deee0）

`RedisHIncrBy` 在 key 过期后会重建一个只含 `RemainQuota` 的哈希（TTL 60s）。
令牌缓存读取没有用户缓存那样的完整性校验，命中残缺哈希时拿到 `Id=0 Status=0`，
`ValidateUserToken` 判为无效，该令牌后续请求在最多 60 秒内全部 401。

- `model/token_cache.go` 读取后校验关键字段完整性，残缺时回源数据库并重建缓存。
- 新增 `model/token_cache_integrity_test.go`。

### 1.3 配置热更新数据竞争（d8572f81f）

管理端保存设置走 `ConfigManager.Update` 在写锁内逐字段重写结构体，而 17 个裸
指针 getter 读时不加锁。`-race` 复现三处：`ThinkingModelBlacklist`（slice）、
`SafetySettings`（map）、`SensitiveWords`（slice）。

- `setting/config/config.go`：切片一律解码到新值再整体替换发布。`encoding/json`
  风格的解码会复用非 nil 切片的底层数组并原地覆盖元素，持有旧切片头的读者会
  读到撕裂数据；替换发布使旧对象在替换后不再被改写。
- `setting/model_setting/global.go`、`gemini.go`、`qwen.go`、
  `setting/system_setting/fetch_setting.go`：新增锁内快照读取入口
  （`readGlobalSettings` / `GlobalSettingsSnapshot` / `GeminiSettingsSnapshot` /
  `GetFetchSettingSnapshot`），替换全部裸指针 getter。
- 30 个文件，新增 4 个竞态回归用例。

反向验证：只回退读者侧（改回裸指针）触发 4 处 `WARNING: DATA RACE`；只回退
写者侧（切片原地解码）同样触发。证明两部分都是必需的。

### 1.4 payload 审计包装使写截止时间失效（17a3fa2f2）

用户开启"记录响应内容日志"后 `c.Writer` 被换成 `*payloadAuditResponseWriter`，
它没有 `Unwrap`，`http.ResponseController.SetWriteDeadline` 返回
`feature not supported`，而调用点用 `_ =` 吞掉了错误。

- `common/payload_audit.go` 补 `Unwrap() http.ResponseWriter`。
- 新增 `common/payload_audit_write_deadline_test.go`。

### 1.5 cohere / zhipu / palm 流式生产者协程泄漏（60a434687）

生产者 goroutine 向无缓冲 `dataChan`/`stopChan` 发送，没有 `select ctx.Done()`
逃生。客户端中途断开时 `c.Stream` 直接返回且不排空通道，生产者永久阻塞。

- 三个渠道统一加 `done chan struct{}`（handler 内 `defer close(done)`），
  生产者所有发送改为 `select { case ch <- v: case <-done: }`。
- palm 额外问题：读协程与 handler 并发 `Close` 响应体，`-race` 捕获后改为
  handler 独占关闭。
- 新增 3 个生产者退出用例。

### 1.6 编辑令牌回滚并发消耗（2c21fecfe）

`UpdateToken` 读出新行后仍用请求体的 `RemainQuota` 覆盖，`Token.Update` 显式
Select 了 `remain_quota`，前端每次保存都发这个字段。

- `model/token.go`：`Token.Update` 只更新配置列，结算列（`remain_quota`/
  `used_quota`）不再参与；写缓存前回读结算列。
- 新增 `UpdateWithExplicitRemainQuota`，仅在请求显式携带 `remain_quota` 时使用。
- 前端 `web/default` 与 `web/classic` 的令牌编辑表单不再回传该字段。
- 新增 `controller/token_update_quota_test.go`。

### 1.7 multipart 文件名注入（578cc3b0e）

文件名来自客户端 multipart 头，`fmt.Sprintf` 直接拼进 `Content-Disposition`，
`CreatePart` 不做转义。探针确认 RFC 2231 `filename*=UTF-8''a%0D%0A...` 百分号
解码出的 CRLF 会原样进入发往上游的 part 头。

- `common/multipart_filename.go` 新增 `SanitizeMultipartFilename` /
  `SanitizeMultipartFieldName`，剥离全部控制字符（含 CR/LF/TAB）。
- openai（图片编辑）、replicate、sora 三处调用点接入。
- 新增 `relay/channel/openai/image_edit_filename_injection_test.go`。

### 1.8 上游错误体无上限读取（adfd92ec4）

`RelayErrorHandler` 是所有非 2xx 的统一出口，`io.ReadAll` 无上限。

- 读取改为 `io.LimitReader`，上限 1 MiB。
- 新增 `service/error_body_limit_test.go`（64 MiB 输入只读不到 4 MiB）。

### 1.9 SSRF 缺口（334b51143、97defe3db）

- `POST /api/channel/fetch_models` 的 Gemini/Ollama 分支没有打
  `common.WithSSRFValidation`，同函数通用分支已打。改为两个分支都接收 context
  并从请求 context 派生，调用点统一标记。新增两个分支的拒绝用例。
- `GET /api/uptime/status` 是匿名路由，每个请求按分组配置同步出站（每组两次），
  无缓存也无并发合并。改为按分组配置 JSON 作 key 缓存 30 秒，并发请求经
  singleflight 共用同一次抓取；抓取不再绑定触发它的请求 context，避免任一
  客户端断开取消其他人等待的结果。新增 4 个缓存用例。

### 1.10 AWS Bedrock 与文件下载忽略请求上下文（13ed6daec）

- `newAwsInvokeContext` 由 `context.Background()` 派生改为从
  `c.Request.Context()` 派生，`RELAY_TIMEOUT` 作为额外截止时间覆盖其上；
  无超时配置时也保留父级取消。5 处调用点全部接入。
- `DoDownloadRequest` 使用带有限超时的 context（未配置 `RELAY_TIMEOUT` 时回退
  5 分钟），并把 cancel 绑定到响应体 `Close`，避免函数返回时掐断调用方读取。
- 新增 `relay/channel/aws/invoke_context_test.go` 与
  `service/download_timeout_test.go`。

### 1.11 结算失败只记日志（dc5b8e246）

结算发生在响应写完之后：HTTP 状态已不可改，而 `used_quota` 与渠道用量已经
累加、消费日志也已按全额写入。`SettleBilling` 失败此前只打一行 LogError，
差额永久丢失且无法从日志里筛出受影响的请求。

- `service/billing.go` 新增 `MarkSettleFailure`：把错误写进消费日志的
  `other.settle_error`，错误日志带上 userId/channelId/tokenId/actualQuota。
- 文本、WSS、音频三处结算调用点统一走该入口。
- 不重试结算：`IncreaseUserQuota` 是非幂等的 `quota += N`，重试可能重复退款。
- 新增 `service/settle_failure_log_test.go`。

### 1.12 额度换算整数溢出（99dbaeb7a）

Go 的越界 float->int 转换与 `decimal.IntPart()` 在 amd64/arm64 上都得到
MinInt64。结算路径把负额度当作退款（`IncreaseUserQuota`），一个异常大的倍率
或价格换算就会静默变成"向用户加钱"。

- `common/quota_math.go` 新增 `SafeQuotaIntFromFloat64` /
  `SafeQuotaIntFromDecimal`：NaN 视为 0，越界饱和到 int64 上下限并告警。
- 替换预扣费、音频计费、文本计费（含阶梯加价）、任务 token 重算中的裸转换。
- 新增 `common/quota_math_test.go`、`service/quota_overflow_test.go`、
  `relay/helper/price_overflow_test.go`。

## 2. 反向验证

每项都做过回退验证，且失败原因与修复目标一致：

| 项 | 回退方式 | 回退后结果 |
| --- | --- | --- |
| 1.3 配置竞争 | 读者改回裸指针 | 4 处 `WARNING: DATA RACE` |
| 1.3 配置竞争 | 写者改回切片原地解码 | `WARNING: DATA RACE` |
| 1.5 流式生产者 | 去掉 `done` 逃生 | 生产者协程仍在栈上 |
| 1.10 AWS ctx | 改回 Background 派生 | `Expected error with "context canceled" ... got nil` |
| 1.10 下载超时 | 改回无截止时间 context | 测试挂起不返回（无截止时间的表现） |
| 1.12 越界饱和 | 调用点改回裸转换 | `"-9223372036854775808" is not positive` |
| 1.11 结算失败 | 去掉 `other` 写入 | 日志 `other` 不含 `settle_error` |
| 1.9 uptime 缓存 | `Get` 改为直接抓取 | 重复读取与并发合并两条用例失败 |

其中 1.12 的 `relay/helper` 用例在首次编写时只测了换算函数本身，回退调用点后
仍然通过，属于无效回归测试。已改写为驱动 `ModelPriceHelper` 的价格分支，回退
后按预期失败。

## 3. 已评估但不改

- `controller/ratio_sync.go` 与 `controller/custom_oauth.go` 的裸 `http.Client`：
  目标 URL 来自管理员配置或 root 请求体，不是攻击者可控输入。对其施加
  `fetch_setting` 的 SSRF 边界会破坏自建内网上游（内网 Ollama、企业 IdP），
  与 `CR-FULL-DIMENSION-AUDIT-2026-09-23.md` 记录的既有决定冲突。两处都已有
  超时上限（`req.Timeout` 与 20 秒），不存在无界挂起。
- `fetch_models` 的 Gemini/Ollama 分支是另一回事：同一个函数里通用分支已经
  打了标记，这两个分支是遗漏而非策略选择，因此本轮补齐。

## 4. 验证记录

以下命令均在本轮执行并读取输出。

```text
go build ./...                                              exit 0
go vet <16 个改动包>                                        exit 0
go test ./common ./service ./controller ./model ./relay/... ./setting/... -count=1
                                                           exit 0，52 个包 ok，无 FAIL
go test -race ./setting/... ./common ./controller ./relay/channel/{cohere,zhipu,palm} ./service
                                                           exit 0，15 个包 ok，无 DATA RACE
```

前端改动（1.6）在对应提交前执行：

```text
cd web/default && bun run typecheck                          exit 0
cd web/default && bun run lint                               exit 0（0 error，22 个既有 warning）
```

## 5. 未覆盖边界

- 1.1 的并发用例在 SQLite 上验证；MySQL/PostgreSQL 的 `RowsAffected` 语义一致，
  但本轮未接真实 MySQL/PG 实例复跑。
- 1.5 只对 cohere 做过动态探针复现，zhipu/palm 为同构代码，验证方式是对齐后
  的用例通过。
- 1.12 的端到端可达性依赖异常倍率或异常上游用量，属机制修复；正常倍率下的
  计费结果未变（既有计费测试全部通过）。
- 1.9 的 SSRF 拒绝用例使用 loopback `httptest` 服务模拟被拦截目标，未在真实
  网络环境验证可达性。
- 1.11 只保证失败可被发现与对账，未做自动补扣；补扣属于独立的运维流程。
