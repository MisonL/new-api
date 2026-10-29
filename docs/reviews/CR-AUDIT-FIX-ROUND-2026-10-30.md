# CR-AUDIT-FIX-ROUND-2026-10-30

在第二轮审计的 12 项修完之后，针对"已修缺陷类是否还在别处复现"再做了一轮
排查，发现 3 类残留，本轮逐项修复，拆成 3 个原子提交（`b3aa04f76`、
`56a62c098`、`4b6837cc2`），每项都先写失败用例、确认 RED、实现、反向验证、
再单独提交。

## 1. 逐项修复

### 1.1 任务计费倍率换算越界（b3aa04f76）

第二轮只覆盖了文本、音频、预扣费与任务 token 重算，漏了任务提交路径：
`relay/relay_task.go:744` 的 `int(float64(Quota) * ra)` 和 `:968`
`recalcQuotaFromRatios` 的 `int(result)`。

倍率来自 `adaptor.EstimateBilling`，其中 sora 的 `seconds` 直接取自客户端
请求（`relay/channel/task/sora/adaptor.go:111`，`strconv.Atoi` 解析，无上限）。
乘积越界回绕成 MinInt64 后，负额度的预扣被 `PreConsume` 的 `amount <= 0`
直接放行，最终落到退款方向。

- 抽出 `applyOtherRatiosToQuota`，两处统一走
  `common.SafeQuotaIntFromFloat64`；除数为极小值时同样饱和。
- 新增 `relay/task_quota_overflow_test.go`（4 例，含正常倍率不变）。
- 反向验证：三处换回裸转换后，三条用例均报
  `"-9223372036854775808" is not positive`。

可达性说明：机制已由用例固定，但"客户端传入极大 seconds 能走到该换算"是
代码推断，未构造真实上游请求验证。

### 1.2 剩余 32 处上游响应体无界读取（56a62c098）

此前一批提交给 14 处封了顶，但 `relay/` 下仍有 32 处直接
`io.ReadAll(resp.Body)`：非流式 JSON、TTS 音频、内联媒体、rerank、
embedding、模型列表。上游返回超大响应时每个并发请求各持一份完整响应体。

- `common/io.go` 新增三档上限：`UpstreamSmallResponseLimit`（8 MiB，模型列表/
  状态查询）、`UpstreamJSONResponseLimit`（64 MiB，非流式 JSON）、
  `UpstreamMediaResponseLimit`（128 MiB，含内联 base64 媒体）。数值与既有
  封顶（claude/xai/responses 64 MiB、gemini native 128 MiB）一致。
- 32 处调用点全部改为 `common.ReadAllWithLimit`，超限返回显式错误而不是
  静默截断。
- 新增 `relay/upstream_body_read_guard_test.go`：遍历 relay 树断言不再出现
  `io.ReadAll(resp.Body)` 这类写法。任务提交路径排除在外——`relay_task.go:855`
  在每个 task adaptor 读取之前已装好 `boundedTaskSubmitResponseBody`（1 MiB
  显式错误）。
- 新增 `relay/channel/gemini/fetch_models_body_limit_test.go`：用分块写入
  模拟超大模型列表响应，断言返回 `exceeds` 错误。
- 反向验证：守卫用例在修复前列出 45 处（含 3 处 `req.Body` 与 1 处注释，
  收窄正则后为 32 处）；gemini 用例在改回 `io.ReadAll` 后报
  `解析响应失败: invalid character 'a'` 而不是 `exceeds`。

### 1.3 用量统计写入失败可对账（4b6837cc2）

`updateUserUsedQuotaAndRequestCount` 与 `updateChannelUsedQuota` 失败时只打
一行 SysLog 且函数不返回错误。`used_quota`/`request_count` 是 SQL 相对增量，
写入失败后没有可回放的来源，失败既不知道是哪个请求丢的增量，也无法对账。

- `model.UpdateUserUsedQuotaAndRequestCount` / `UpdateChannelUsedQuota` 改为
  返回 error，日志带上 userId/channelId/delta。
- 新增 `service.RecordUsageStats`（合并两处错误）与
  `service.MarkUsageStatsFailure`（写进消费日志 `other.usage_stats_error`，
  错误日志带身份字段）。
- 七处调用点接入：文本、WSS、音频、任务提交日志、任务重算、MJ 两处、
  违规扣费。
- 新增 `model/usage_stats_write_test.go`（成功/失败各路径）与
  `service/usage_stats_failure_log_test.go`（用没有 users/channels 表的库
  触发失败，断言消费日志行含 `usage_stats_error`）。
- 反向验证：去掉 other 写入后，服务层用例报
  `does not contain "usage_stats_error"`。

## 2. 已评估但不改

前一轮报告中列出的其余候选经复核不成立或已由既有机制覆盖：

- `service/image.go` 的 `DecodeUrlImageData` 整段读取无上限，但全仓库没有
  调用点，是死代码。
- `relay/channel/task/{ali,vidu,suno,vertex}` 的 `FetchTask` 用
  `context.Background()`，但轮询链路全部走 `FetchTaskWithContext`
  （`service/task_polling.go:1240`、`:1572`），非 context 版本在接口之外没有
  调用者。
- `relay/mjproxy_handler.go`、`relay/responses_handler.go`、
  `service/text_quota.go` 等处的 `context.Background()` 都是 ctx 为 nil 时的
  兜底，不是替换真实请求 context。
- 匿名路由逐个核对：`/uptime/status` 已加缓存；其余为纯配置读取或内存快照。

## 3. 验证记录

以下命令均在本轮执行并读取输出。

```text
go build ./...                                                       exit 0
go vet ./common ./service ./controller ./model ./relay/... ./setting/...
                                                                     exit 0
go test ./common ./service ./controller ./model ./relay/... ./setting/... -count=1
                                                                     exit 0，52 个包 ok，无 FAIL
go test -race -count=1 ./service ./model ./relay ./relay/channel/gemini
                                                                     exit 0，4 个包 ok，无 DATA RACE
```

## 4. 未覆盖边界

- 1.2 只对 gemini 模型列表做了端到端超限用例；其余 31 处的封顶由守卫用例
  与共享 helper 的既有单测覆盖，未逐端点构造超大响应。
- 1.3 的失败注入用的是缺表的独立 SQLite；真实环境里的失败形态（连接中断、
  锁超时）语义一致，但未在 MySQL/PostgreSQL 上复现。
- 1.1 的可达性依赖客户端传入异常大的任务参数，未接真实上游验证。
