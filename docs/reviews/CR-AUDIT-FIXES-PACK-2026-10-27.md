# CR-AUDIT-FIXES-PACK-2026-10-27

针对 20 项审计发现的一次集中修复。每项独立提交，先写失败测试再修，修复后在同一轮
运行对应验证命令并读取输出。本轮未触碰生产容器（`new-api`、`postgres`、`redis`
仅只读），未回退其他会话的在途改动
（`web/classic/.../EditChannelModal.jsx` 始终未触碰），未推送。

采样时间：2026-10-27（本机 macOS，dev 分支）。
基线：`git rev-parse HEAD` 起点 0cf8214d1。

## 1. 资金正确性

### 1.1 任务按 token 重算沿用冻结的分组倍率（661c17813）

`RecalculateTaskQuotaByTokens` 用 `GetGroupGroupRatio(group, group)` 查专属倍率，
两个参数都是任务实际使用分组，用户分组没参与；配置了 `group_group_ratio` 的站点
必然查不中并回落到普通分组倍率。预扣按专属倍率、重算按普通倍率，同一笔任务凭空
产生差额。改为优先读提交时冻结的 `BillingContext.GroupRatio`，只有旧行无快照时
才按 `(用户分组, 使用分组)` 现查。

验证：`go test ./service -run TestRecalculateTaskQuotaByTokens`（两条用例：
专属倍率回退路径、冻结快照优先路径），反向验证旧实现下分别为 1000/2000（期望 500）。

### 1.2 差额结算写回任务行（6488eed63）

`RecalculateTaskQuota` 只改内存 `task.Quota`，库里的 `quota` 仍是预扣值；任务列表
（`TaskDto.Quota`）与按行内 quota 计算的后续动作都读库。新增
`model.UpdateTaskQuota`（单列 UPDATE，不整行 Save，避免覆盖轮询快照里过期的
status/progress/private_data），结算成功后落库。

验证：`go test ./service -run TestRecalculateTaskQuotaPersistsSettledQuota`；
反向验证旧实现为 2000（期望 3000）。

### 1.3 终态计费失败回退状态并限次重试（ada4728b0）

异步任务先写终态再计费，资金调整失败只写日志；轮询只查未完成任务，退款/差额结算
从此永不重试。改动：

- `RefundTaskQuota` / `RecalculateTaskQuota` / `RecalculateTaskQuotaByTokens` /
  `settleTaskBillingOnComplete` / `SettleTaskBillingOnComplete` 返回资金调整错误。
  返回值只覆盖"任何资金变动之前"的失败；令牌与日志失败仍只记日志（重试会重复退款）。
- 新增 `RevertTaskTerminalTransition`：以终态为 CAS 守卫把行回退到转移前快照。
  覆盖视频轮询、Suno 响应、本地失败标记、超时清扫、实时拉取五条路径。
- 回退次数存 `PrivateData.BillingRetryCount` 并随回退落库；达到 5 次后保留终态并
  输出错误日志，避免永久性失败（订阅行被删）每轮轮询重复请求上游。

验证：`go test ./service -run 'TestMarkTaskPollingFailure|TestRevertTaskTerminal'`
（四条用例：失败回退、成功保终态、上限停止、计数落库）；反向验证去掉回退后行仍为
FAILURE（期望 IN_PROGRESS）。全量 `./service ./model ./relay ./controller` 通过。

## 2. 稳定性

### 2.1 轮询单请求超时（c6f607b20）

`RELAY_TIMEOUT` 默认 0（正式环境未设置），共享 HTTP 客户端无 Timeout；轮询上下文
只有 WithCancel。上游接受连接后挂起时整个轮询循环永久停在该请求上。给
`FetchTaskWithContext` 加 60s 派生 context（各 adaptor 的请求都由它构造，往返与
响应体读取都受限），响应体包装成 Close 时释放计时器。

验证：`go test ./service -run TestFetchTaskWithContext`（挂起请求、挂起响应体、
正常响应三条）；反向验证旧实现下测试 10s 超时挂死。

### 2.2 实时 WS 心跳与读超时（9633dca32，与 2.3 同一提交）

两个连接此前只有读大小上限。客户端拔网线/上游静默挂死时两个 `ReadMessage` 永久
阻塞，连接、计费会话、上游并发租约都不释放。新增
`helper.StartRealtimeKeepalive`（读超时 + pong 顺延 + 定期 ping，ping 走
WriteControl 与数据写并发安全），两侧启用；成功读到的消息也顺延超时。

验证：`go test ./relay/helper -run TestRealtimeKeepalive`（静默对端被断开、
有回应对端保持连接）；反向验证关闭心跳后静默对端测试失败。

### 2.3 实时用量跨协程加锁（9633dca32）

两个读协程共享 `usage` / `localUsage` / `sumUsage`：客户端方向累加 `localUsage`，
上游方向在 turn 结束时累加 `usage`、结算并清空 `localUsage`，函数尾部再读两个
对象兜底结算。无同步时清空赋值可能被另一协程累加覆盖（漏计）或尾部读到未清空
旧值（重复计费）。所有访问纳入同一互斥量；`CountTokenRealtime` 读的
`info.IsFirstRequest` / `info.RealtimeTools` 也被上游方向写，一并纳入临界区。

验证：`go test -race ./relay/channel/openai`。

### 2.4 删除无人消费的 sendChan/receiveChan（b4f12a33f）

两个 100 容量的 `[]byte` 通道只在写端 select/default 投递，没有读取方；转发是直接
WssString 写对端。它们让每条 16MB 帧在被转发后仍被通道引用。

验证：`go test ./relay/channel/openai`。

### 2.5 Hailuo 查询失败不再被覆盖回进行中（dae87abdf）

`parseTaskResultWithContext` 先用 base_resp 判定失败，随后无条件执行状态字符串
switch；失败时状态串通常是空值或不可识别值，落进 default 分支改回 IN_PROGRESS，
若状态串还写着 Success 更会被改成 SUCCESS 并为取视频再发一次上游请求。base_resp
非成功时直接返回失败结果。

验证：`go test ./relay/channel/task/hailuo -run TestParseTaskResultKeepsFailureOnBaseRespError`
（空/处理中/不可识别/Success 四种状态串）；反向验证旧实现 5 个子用例失败。

## 3. 数据一致性

### 3.1 未完成查询只按状态筛选（53588ebf4）

`GetAllUnFinishSyncTasksWithError`、`GetTimedOutUnfinishedTasksWithError`、
`TaskCountUnfinishedWithError` 都带 `progress != '100%'`。provider 在终态前把进度
报到 100%、或 legacy 行 status 已终态但 progress 停在中间值时，这两种行都会从轮询
与超时清扫里永久消失。Midjourney 侧早已按状态判断并注释了同一理由。改为只按
status 判断，并更新既有测试（progress=100% + status=NULL 的行现在必须被选中）。

验证：`go test ./model -run TestUnfinishedTaskQueries`；`./model ./service` 全量通过。

### 3.2 轮询游标推进（174fec423）

每轮只取 `TASK_QUERY_LIMIT` 条且固定 `Order("id")`。一批卡在非终态的行占满窗口时，
id 更大的新任务永远排不进这一批。新增 `GetUnfinishSyncTasksAfterIDWithError`，
主循环维护进程内游标：每轮从上一轮最后一条之后继续，取不满窗口就回到开头。

验证：`go test ./model -run TestUnfinishSyncTasksAfterIDCoversBeyondLimit`。

### 3.3 models/vendors 名称唯一性（82dcfcf93）

唯一索引是 composite `(name, deleted_at)`；SQL 里 NULL 互不相等，未删除行不冲突，
名称唯一性只靠 controller 先查后插的竞态窗口。模型标签改为
`where:deleted_at IS NULL`：PG/SQLite 得到部分唯一索引；MySQL 沿用
prefill_groups 的生成列方案（`active_model_name` / `active_vendor_name`，软删除后
坍缩为 NULL）。

验证：SQLite `go test ./model -run TestLiveModelNameIsUnique|TestLiveVendorNameIsUnique`
（旧实现返回 nil 错误）；MySQL 8.4 实测（`NEW_API_TEST_MYSQL_DSN` 指向
`na-audit-mysql2:13400` 一次性库）迁移 DDL、同名拒绝、软删除后可重建三项通过。

### 3.4 quota_data 小时桶唯一索引（fae32df39）

`SaveQuotaDataCache` 是"先查再插"：两个进程同时刷同一桶都查不到行、都 INSERT。
此后 `increaseQuotaData` 的 UPDATE 同时命中两行，看板翻倍。增加唯一索引
`uk_qdt_bucket`（user_id, username, model_name, created_at），刷新改为
ON CONFLICT / ON DUPLICATE KEY UPDATE 在 SQL 层累加；迁移先合并存量重复行
（count/quota/token_used 求和）再建索引。

验证：SQLite 四条用例（唯一约束、重复合并、缓存 upsert、增量可加）；MySQL 8.4
实测 upsert 语义。生产库只读查询：当前 0 组重复（13585 行）。

### 3.5 任务消费日志计入看板聚合（3a45de8f5）

任务计费日志写进 logs（type=2），却从不更新 quota_data；而
`bin/repair_channel_quota_logs.go` 按 type=2 重建 quota_data。任务消费在看板长期
缺失，跑一次修复脚本又会整批补上。`RecordTaskBillingLog` 在 `LogTypeConsume` 时
按与 `RecordConsumeLog` 相同的异步路径写聚合；退款不进入聚合（修复脚本同样只按
type=2 重建）。

验证：`go test ./model -run TestRecordTaskBillingLog`（消费计入、退款不计入）。

### 3.6 SQLite 签到改事务（ebede6393）

SQLite 路径是"插入记录 → 加额度 → 失败则删除记录"的手动回滚，`DB.Delete` 的返回值
被丢弃。删除失败时记录留下、额度没到账，`HasCheckedInToday` 只查记录，用户当天
再也签不了到。插入与加额度改用同一事务，额度更新走 tx 而非全局 DB。

验证：`go test ./model -run TestUserCheckin`（用触发器阻断 DELETE，旧实现记录残留）。

## 4. 前端安全

### 4.1 classic 全部 HTML 注入点接 DOMPurify（bf263e493）

classic 有十个 `dangerouslySetInnerHTML`（关于、首页、页脚、公告弹窗、公告板、FAQ、
文档渲染、版本更新说明、toast、Markdown 渲染结果），渲染管理员可配置内容或 marked
解析结果（marked 原样保留内联 HTML）。生产环境加载的正是 classic，DOMPurify 早在
依赖里却从未被引用。新增 `helpers/sanitizeHtml.js`：`<template>` 惰性解析（脚本
不执行、`<style>` 不被解析器丢进 head）+ DOMPurify 默认白名单，禁表单控件与
srcdoc，保留 target。DocumentRenderer 原有同名局部函数改名 `extractHtmlPayload`。

验证：
- `bun test tests/classicHtmlSanitization.test.js`：十个注入点的 `__html` 全部
  经过 sanitizeHtml，不允许 `__html: marked.parse(...)` 直出（CodeViewer 例外：
  内容先转义再 linkify）。反向验证：去掉 About 的 sanitizeHtml 后测试失败。
- 真实浏览器（ego-browser + Chromium，本地静态页加载 DOMPurify 3.4.17）：script、
  onerror、javascript: URL、iframe、form、svg onload 全部剥离，注入后 xss 计数
  保持 0；`<style>`、class、style、target 正常保留。
- `bun run lint`、`bun run build` 通过。

## 5. 已知边界

- 单请求超时（2.1）用派生 context 覆盖往返与响应体读取；若上游在 TLS 握手阶段
  挂起，同样由该 deadline 结束（adaptor 用同一 ctx 构造请求）。未覆盖：DNS 解析
  阶段（Go resolver 不读请求 ctx 的超时）。
- WS 心跳（2.2）依赖对端回应 ping；对不回应 ping 的客户端，90s 内无任何数据帧
  即断开，这是有意行为。
- MySQL 相关验证（3.3、3.4）在一次性库 `na-audit-mysql2` 上进行，未覆盖正式 MySQL。
- 前端源码守卫（4.1）是静态检查，运行时行为由浏览器实测覆盖；bun 环境无 DOM，
  无法在单测里直接跑 DOMPurify。
- 生产容器与数据库本轮仅只读查询（`SELECT`、`docker inspect`），未重启、未改配置。
