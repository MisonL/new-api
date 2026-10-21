# CR-AUDIT-FIXES-2026-10-21：计费、方言兼容与 OAuth 回调修复

本轮承接 2026-10-19 的多 agent 审查清单，逐项独立复现后修复。原则不变：
只有本人复现成功的才改；SQLite 单测覆盖不到的方言语义，一律在一次性
MySQL / PostgreSQL 库上实测。

基线：`ee979703a`（dev）。本轮提交：`268b793b9`、`1feee25d2`、`3656eceef`、
`2b4b822ea`、`e248ec175`、`f28c9811f`、`11f5b5f19`、`fceb66471`、
`36bed2263`、`d7327cb86`、`eb28d1b5f`。

## 1. DeepSeek thinking 放宽门控只看映射后上游模型名（`268b793b9`）

`ee979703a` 用 `HasDeepSeekModelName(claudeRequest.Model, info)` 决定是否放宽
thinking 块校验。该函数除 `claudeRequest.Model` 外还看 `OriginModelName`，
于是「公共名是 deepseek 别名、实际转发给真实 Anthropic 模型」时，真实的
opaque signature 被静默丢弃，回传后上游必然 400。门控改为
`IsDeepSeekModelName(claudeRequest.Model)`，只看已映射的上游名，维持
fail-closed。

验证：overlay 探针独立复现（修复前期望 error 得到 nil，修复后符合预期）。

## 2. realtime 每轮用量并入 sumUsage 后立即清空（`1feee25d2`）

`ResponseDone` 的两个分支里，`usage`/`localUsage` 在 `preConsumeUsage` 之后
才清空，且清空语句位于错误返回之后。`preConsumeUsage` 已经把本轮用量累加进
`sumUsage`；一旦 `PreWssConsumeQuota` 出错，本轮用量仍留在局部变量里，函数
尾部的兜底 `preConsumeUsage` 会把同一轮再加一次，`sumUsage` 翻倍并按翻倍值
终态结算。把清空移到错误判断之前。

## 3. 订阅预扣幂等回退在 PostgreSQL 上真正生效（`3656eceef`）

并发同 `requestId` 时唯一键冲突，原实现在事务内查询已提交记录做幂等回退。
PostgreSQL 在语句失败后把整个事务标记为 aborted，事务内任何后续语句返回
25P02，COMMIT 也变成 ROLLBACK。

实测（真实 PostgreSQL，`newapi_test_pg1`）：修复前回调返回 nil 仍报
`commit unexpectedly resulted in rollback`。

改按方言分派：

- PostgreSQL 用 `ON CONFLICT DO NOTHING`，语句本身成功、事务保持健康，
  `RowsAffected == 0` 时走事务外连接读取胜者记录；
- MySQL / SQLite 保留报错回退（重复键不中止事务）。不改用
  `ON DUPLICATE KEY UPDATE id=id`：DSN 带 `clientFoundRows=true` 时冲突会报
  `RowsAffected = 1`，被误判为插入成功并重复累加订阅额度。

新增真实 PG 与 MySQL 门控测试，注入并发重复记录并断言返回值取自已提交
记录、失败事务的额度累加随事务回滚。两者均通过。

## 4. tiered 计费保留聚合 cache creation（`2b4b822ea`）

Anthropic 的 `cache_creation_input_tokens` 是聚合值，`cache_creation` 里的
`ephemeral_5m/1h` 只是拆分。`BuildTieredTokenParams` 在 anthropic 语义下直接
拿 5m 拆分字段当 `cc`：上游不返回 `cache_creation` 对象时拆分字段为 0，
整段缓存写入以 `CC = 0` 计费；拆分只覆盖部分聚合值时，未归类部分同样丢失。
ratio 路径（`service/text_quota.go`）已按 remaining 补齐，tiered 路径缺失。

改为 `cc = 聚合值 - 1h 拆分`（未归类部分按通用缓存写入价），聚合值缺失时
退回拆分之和；`Len` 同步使用聚合值。`bin/repair_channel_quota_logs.go` 里的
同构逻辑一并修正。

三个新测试在修复前分别失败（`CC = 0`、`CC = 1000` 应为 1500），修复后通过；
完整拆分用例修复前后都通过，确认既有行为不变。

## 5. 订阅超额结算落账，realtime 逐轮定价改用冻结快照（`e248ec175`）

### 5.1 订阅超额

订阅额度准入由预扣把关（`remain < amount` 即拒绝），超额只可能来自
「实际用量 > 预估」的结算差额。结算发生在请求已执行完之后，原实现把超额
当错误返回，整笔差额被丢弃，这次请求等于免费。

新增 `SettleUserSubscriptionDelta`：超额落账，剩余额度变负，由下一次预扣的
准入检查拦截；中途 Reserve 与退款仍用严格拒绝的
`PostConsumeUserSubscriptionDelta`。realtime 会话、订阅资金来源、异步任务
差额结算三处终态路径改用宽松变体。

### 5.2 realtime 逐轮直扣的三处定价缺陷

- 开头 `if relayInfo.UsePrice { return nil }` 是全仓无赋值点的死分支，
  按次计费模型在逐 turn 阶段永远被跳过；
- 第 110 行忽略 `GetModelRatio` 的 success，未配置倍率的模型被 37.5 兜底
  倍率扣费；
- 钱包余额检查对订阅计费同样生效，订阅用户钱包为 0 时每轮报错。

改为统一读取 `relayInfo.PriceData` 冻结快照（与终态结算同源），并让钱包
检查只对钱包计费生效。

三个新测试在修复前分别失败（按次价格模型扣 1500 而非 10000、未定价模型
扣 56250 而非 3000、订阅用户报错），修复后通过。

## 6. MySQL 上 prefill 组名部分唯一索引退化（`f28c9811f`）

`PrefillGroup.Name` 用 `uniqueIndex:uk_prefill_name,where:deleted_at IS NULL`
声明部分唯一索引。只有实现 `BuildIndexOptions` 的方言才把 where 写进 DDL；
GORM 的 MySQL 驱动没有该接口，条件被静默丢弃。

真实 MySQL 8.4 上 `SHOW CREATE TABLE` 确认：

```sql
UNIQUE KEY `uk_prefill_name` (`name`)
```

后果：软删除一个组后同名组永远无法重建（墓碑行仍占唯一键），而
`IsPrefillGroupNameDuplicated` 只统计未删除行、会放行到数据库报错。

MySQL 不支持部分索引，用生成列做等价实现：新增 `active_name`（软删除后为
NULL 的 STORED 生成列），把 `uk_prefill_name` 移到该列上；MySQL 唯一索引把
NULL 视为互不相等，等价于「仅活动行唯一」。迁移按方言分派；MySQL 分支不再
二次 AutoMigrate（表已存在时 gorm 会去删幻影约束 `uni_prefill_groups_name`
并返回 1091），改为按列清单补齐。

真实 MySQL 修复后的 DDL：

```sql
`active_name` varchar(64) GENERATED ALWAYS AS (if((`deleted_at` is null),`name`,NULL)) STORED,
UNIQUE KEY `uk_prefill_name` (`active_name`),
```

端到端测试：软删除后同名重建成功、索引列确为 `active_name`。

## 7. 桌面端 login 回调不再向系统浏览器签发会话（`11f5b5f19`）

桌面端 OAuth 的回调跑在系统浏览器里，桌面应用自己的会话由 poll 在桌面
webview 内签发。`HandleOAuth` 对桌面请求完全跳过浏览器会话绑定检查，成功
后又调用 `setupLogin` 给该浏览器签发会话。

state 只与状态存储比对、不绑定任何浏览器，第三方页面拿自己发起的 state
拼出回调 URL 就能让受害者浏览器登录进攻击者账号（登录 CSRF），而这把浏览器
会话对桌面端没有任何用途。

改为桌面 login 回调只完成 handoff 并返回 `desktop_handoff` 标记，由两个前端
的 OAuth 回调页提示返回桌面应用。2FA 门控不受影响：poll 走同一个
`setupLoginWithResult` 收口，桌面端仍会收到 `require_2fa`。普通 Web 登录
路径不变，有测试钉住（断言回调必须签发会话 cookie 且用户落库）。

## 8. 前端审查项（`fceb66471`、`36bed2263`、`d7327cb86`、`eb28d1b5f`）

### 8.1 format:check（`fceb66471`）

`use-auth-redirect.ts` 与 `sign-in.tsx` 是 `905ae9657` 之后 prettier 规则
漂移，格式化后 `bunx prettier --check .` 全绿。

### 8.2 bun audit（`36bed2263`）

`web/default` 报 8 个、`web/classic` 报 6 个漏洞，可升级项全部来自传递依赖：

| 包 | 修复前 | 修复后 | 公告 |
| --- | --- | --- | --- |
| follow-redirects | 1.16.0 | 1.16.1 | GHSA-q3jh-wrc6-jgq8、GHSA-8r9p-f939-6h3c |
| @modelcontextprotocol/sdk | 1.31.0 | 1.32.1 | GHSA-22jm-h49p-29qw 等四项 |
| hono | 4.13.7 | 4.13.13 | GHSA-5r4p-p66f-jhc7 |
| vite（classic） | 6.4.3 | 6.4.5 | launch-editor 三项 |

修复后两个前端各剩 1 项：`braces@3.0.3` 是 npm 最新版，公告覆盖 `<=3.0.3`
（暂无修复版本），且只经 dev 工具链（chokidar / fast-glob / micromatch）。

验证：两边 `bun install` 后 `bun audit` 各剩该项；`bun run build` 均通过。

### 8.3 模型映射编辑器丢焦点（`d7327cb86`）

编辑器把每次按键的结果通过 `onChange` 交给父级，父级再作为 `value` 传回。
原实现的 `useEffect([value])` 不区分「自己刚发出的回声」和「外部变更」，
每按键都重新解析成行，行 id 由 `Date.now()` 生成必然变化，React 按新 key
卸载重挂 `<Input>`，用户每敲一个字符就丢一次焦点。

抽出 `model-mapping-rows` 纯函数：解析/序列化、单调递增的行 id、以及判断
传入值是否就是本组件刚发出的那份。组件用 `lastEmittedRef` 跳过自己的回声。

顺带修正 `JSON.stringify(obj, 2)` 少传 `null` 导致第二个参数被当作 replacer
的问题（typecheck 报错）。8 个单测覆盖解析、非法 JSON 保持、空键跳过与回声判定。

### 8.4 OAuth 回调判定（`eb28d1b5f`）

`GET /api/oauth/:provider` 的成功响应有三种语义，原前端只处理了一种：

- 绑定：后端返回 `{action:"bind"}`，`message` 是本地化文案。前端比较
  `message === 'bind'` 永远不成立，绑定被当成登录：`{action:"bind"}` 写进
  auth store 与 localStorage，弹窗不关、`OAUTH_BIND_STORAGE_KEY` 不写、
  绑定列表不刷新，还提示「登录成功」。
- 二次验证：开启 2FA 的账号走 OAuth 登录时后端返回 `{require_2fa:true}` 且
  不签发会话。前端把它当用户对象写入 store 并跳 dashboard，随后 `getSelf`
  失败被弹回登录页，用户永远到不了 `/otp`。
- 桌面 handoff：见第 7 节。

抽出 `resolveOAuthCallbackOutcome` 按 data 字段判定四种形态；登录分支要求
载荷真的带 `id` 与 `username`，否则回落到 `finalizeLogin`。绑定成功落点从
路由 id `/_authenticated/profile/` 改为真实路径 `/profile`，否则修复判定后
会落到 NotFound。二次验证交给 `redirectTo2FA`，其内部已对 redirect 做站内
校验。5 个单测覆盖四种形态。classic 前端的回调判定本已正确，未改。

## 验证记录

### Go

```bash
go build ./...
go test ./... -count=1
```

结果：退出码 0，全包通过。

方言门控测试（一次性库，不复用隔离开发库的数据）：

```bash
NEW_API_POSTGRES_TEST_DSN=<newapi_test_pg1> \
  go test ./model -run 'TestPreConsumeUserSubscriptionIdempotentFallbackOnPostgres|TestPrefillGroup|TestInsertChannelSynchronizes' -count=1
NEW_API_TEST_MYSQL_DSN=<newapi_test_d/e> \
  go test ./model -run 'TestPreConsumeUserSubscriptionIdempotentFallbackOnMySQL|TestPrefillGroupNameReusableAfterSoftDeleteOnMySQL' -count=1
```

结果：均通过。

### 前端

```bash
cd web/default && bun run typecheck && bun test && bun run build && bunx prettier --check .
cd web/classic && bun run build && bunx prettier --check .
```

结果：typecheck 退出码 0；`bun test` 275 通过 / 0 失败（38 个文件）；
两个前端构建通过；format:check 全绿。

## 边界与未覆盖

- 桌面 OAuth 登录 CSRF 的修复只验证了 HTTP 层（回调不签发会话、handoff
  正常完成、Web 登录不受影响），未在真实 Tauri 应用中做端到端点击验证。
- MySQL 生成列方案依赖 MySQL 5.7+ 的 STORED 生成列与「唯一索引把 NULL 视为
  互不相等」语义；实测环境是 MySQL 8.4，未在 5.7 上复验。
- 订阅超额落账改变了 `amount_used` 可能大于 `amount_total` 的语义，已有
  日志与前端展示按「剩余 = total - used」计算，会显示负数；这是有意为之的
  真实反映，未额外做 UI 提示。
- 前端依赖只剩 `braces@3.0.3`，上游尚无修复版本，本轮无法消除。
