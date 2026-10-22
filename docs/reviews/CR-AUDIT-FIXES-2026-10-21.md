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

## 第二轮：多 agent 调研后的补充修复

第一轮修复完成后，按测试隔离、数据库 schema、后端 service/controller、
前端、协议转换五个方向各派一个只读调研 agent，结论逐条独立复核后才动手。

### 已修复

| 提交 | 内容 | 复核方式 |
|---|---|---|
| `9bd09cc16` | `Type` 恢复六个 `*_type_created_at` 的 priority:2 声明 | 解析 19 个索引，修复前有 6 对列序列相同 |
| `c335c9d83` | `UpdateChannelStatus` 的整行 Save 排除 `used_quota`/`balance` | gorm 回调在保存前写入并发计费，回退即失败 |
| `11912c2fc` | `ensureLogIndexesMatchModel`：AutoMigrate 前按名字+列顺序+indisvalid 对齐 logs 索引，PG 上 CONCURRENTLY | PG 门控 5 例 + 反向验证（预建两列退化索引，断言重建为三列） |
| `d8625c5c9` | 上述功能的 SQLite / MySQL 分支门控测试 | 真实 SQLite 内存库与一次性 MySQL 库 |
| `d7e2b48f4` | 流式 OpenAI→Claude 的 `message_delta` stop_reason 走严格映射 | 改前实测输出 `stop_reason":"function_call"`（不在 Anthropic 枚举） |
| `28077bc8b` | model 包测试隔离：DSN 唯一化、`DB` 恢复、truncateTables 两端清理 | 修复前 `-count=2` 137 个 `--- FAIL`；修复后 `-count=3`、`-count=2 -shuffle=on`、5 个随机 shuffle 全绿 |
| `4bb585c9d` | 订阅超额状态下退款不再被拒 | 探针复现：超额后 refund 报 `used exceeds total`，失败请求预扣退不回 |
| `0d35b8304` | 两个前端 `bun test` 接入 repo-gates | 本地 275 + 218 用例；CI 此前零覆盖 |
| `023e32463` | 桌面 OAuth 提示补 fr/ja/ru/vi 译文 | 逐文件单行插入，JSON 转义未被改写 |

### 调研中推翻或判定不修的结论

- **MySQL `CreateIndex` 会 panic**（schema agent）：推翻。`mysql.Migrator` 内嵌
  `migrator.Migrator`，`BuildIndexOptions` 定义在 gorm 基础类型上；真实 MySQL
  8.4 上 `TestEnsureLogIndexesMatchModelOnMySQL` 执行了 CREATE INDEX 并通过。
- **仅 `user_update_billing_scope_test.go` 漏恢复 `DB`**（测试隔离 agent）：
  机制判断不完整。该文件确为触发点之一，但 `log_stat_test.go` 三个用例只清理
  开头不清理结尾，是另一个独立且可固定 seed 复现的污染源。
- **`tool_choice:"any"` 应转 `required`**：不改。上游实际行为未实测，改成
  `required` 可能把一个被忽略的值变成必然报错的值。
- **Claude 入站 tools 在 OpenAI adaptor 层被清空**：不改。仅静态读到的形状，
  未在运行时证实。
- **quota_data / ability 冗余索引、passkey/twofa 软删墓碑占唯一键**：不改。
  前两者是标准前缀索引与低优先级维护债；后者当前所有删除路径已 `Unscoped()`
  硬删，改造 MySQL 生成列方案的收益低于风险。
- **只读守卫不覆盖 SSE/WebSocket**：不改。两侧 `new WebSocket` /
  `new EventSource` / `XMLHttpRequest` 全为零命中，无可触发路径，记为潜在缺口。

### 验证记录

```bash
go build ./... && go test ./... -count=1                       # 退出码 0
go test ./model -count=3                                       # 退出码 0
go test ./model -count=2 -shuffle=on                           # 退出码 0
NEW_API_TEST_MYSQL_DSN=<newapi_test_i/j>  go test ./model -run MySQL -count=1
NEW_API_POSTGRES_TEST_DSN=<newapi_test_pg1/2> go test ./model -run 'Postgres|PostgreSQL' -count=1
cd web/default && bun test && bun run lint && bun run build    # 275 通过，构建通过
cd web/classic && bun test && bun run build                    # 218 通过，构建通过
```

### 本轮未覆盖

- CI 新增两步只在本地跑通，未等一次真实 PR 运行（无法在本机构造 GitHub Actions）。
- `repo-gates` 的 classic 单测用 `bun test` 直接调用；若将来 classic 的
  `bun test` 需要额外环境变量，CI 会暴露出来，本地绿色不代表 CI 绿色。

## 第三轮：controller 与 openaicompat 穷尽扫描

第二轮结束后继续查 `controller/`（96 文件）与 `service/openaicompat/`
（协议转换最密的一层）。这一轮的判断标准是"仓库内可自证的契约"，不再
等上游实测。

### 已修复

| 提交 | 内容 |
|---|---|
| `e549557f0` | Claude `tool_choice:{"type":"any"}` 映射为 OpenAI `required`；`disable_parallel_tool_use` 映射为 `parallel_tool_calls:false` |
| `5ed329ebf` | 补齐 `service/tiered_settle_test.go` 的 gofmt 对齐 |

`any` → `required` 这条第二轮列为"待上游实测，不改"，本轮改变结论的依据是
仓库内证据而非猜测：`relay/channel/claude/relay-claude.go:1433` 的
`mapToolChoice` 已经把 OpenAI 的 `required` 映射成 Claude 的 `any`，正向
映射应与反向一致；而 `any` 不在 OpenAI `tool_choice` 的封闭枚举中，原样
透传的两种可能落点（400 或退化成 auto）都丢失"必须调用工具"的语义。
`disable_parallel_tool_use` 原本在 auto/any/tool 三个分支都被静默丢弃。

两条测试先红后绿：改前分别得到 `"any"` 与 `nil`。

### 已核查并判定正确（含判据）

`service/openaicompat/`：

- 请求方向对无法忠实表达的字段是拒绝而非静默丢：`previous_response_id`、
  `conversation`、`context_management`、`truncation`、`max_tool_calls`、
  `prompt`、`preset`、`reasoning.context/mode/扩展` 全部显式报错。
- `include` 只放行 `reasoning.encrypted_content`，其余值报错。
- 响应方向 `validateResponsesResponseForChat` 对 `failed` 状态与未知输出
  类型报错；内建工具调用与压缩输出按注释有意省略而保留文本。
- `tool_choice` 对象形态会校验引用的函数确实在 tools 中
  （`validateChatToolChoice`），`required` 在无工具时报错
  （`normalizeChatToolChoiceForTools`）。
- 工具名扁平化可能让 `namespace=a,name=b__c` 与 `namespace=a__b,name=c`
  压成同一个名字；`toolState.register` 在"同名且定义不同"时报错退出，
  不静默串号。
- 流式与聚合共用 `responsesStatusFromChatFinishReason`，`length` 与
  `content_filter` 的终态一致。

`controller/`：

- 支付回调（订阅 Epay notify/return、Stripe、Creem、Waffo）签名校验后
  才进 `CompleteSubscriptionOrder`；后者 `forUpdate` 锁行，success 幂等
  返回、非 pending 拒绝。
- 渠道密钥读取：`/api/channel/:id/key` 需 `RootAuth` + 限流 + 安全验证，
  并写系统日志；其余列表与详情一律 `Omit("key")`。
- 管理端订阅操作统一经 `adminCanActOnSubscriptionOwner` 角色比对。
- self 系列端点全部从会话取 `c.GetInt("id")`，不接受外部传入的用户 ID。
- `User.Update` 显式 `Omit(billingColumns...)`，六个计费列不被编辑写回。
- `ManageUser` 的 delete 分支提前返回，不把已软删用户写回 Redis。
- `main.go` 显式 `SetTrustedProxies`，默认为空即完全不信任转发头。

### 本轮未覆盖

`controller/` 中 `channel-test.go`、`channel_upstream_update.go`、
`ratio_sync.go`、`model_sync.go`、`custom_oauth.go`、`twofa.go`、
`passkey.go`、`deployment.go`、`video_proxy.go` 的剩余分支未逐行读。

`any` → `required` 的映射未在真实 OpenAI 兼容渠道上做端到端请求验证；
本结论的依据是仓库内正反向映射一致性与 OpenAI 的封闭枚举，不是上游响应。

### 第三轮验证记录

```bash
go build ./... && go test ./... -count=1        # 退出码 0
go test ./model -count=2 -shuffle=on            # 退出码 0
NEW_API_TEST_MYSQL_DSN=<newapi_test_j>  go test ./model -run 'MySQL' -count=1
NEW_API_POSTGRES_TEST_DSN=<newapi_test_pg2> go test ./model -run 'Postgres|PostgreSQL' -count=1
```

三项门控均退出码 0。`TestChannelHealthConcurrentHalfOpenSelectionHasSingleProbe`
在包级并发跑时出现过一次失败，单独重跑 3 次通过、`./service` 单包通过；
未定位到根因，不归因于本轮改动。

## 第四轮：测试隔离与并发凭证

### 已修复

| 提交 | 内容 |
|---|---|
| `28077bc8b` | 消除 `model` 包的跨测试污染（详见下节） |
| `61db9879c` | 备用码并发消费竞态 |

### 测试隔离：一条悬案的根因

前几轮反复出现"`go test ./model` 偶发 `FAIL` 但没有 `--- FAIL:` 行"的现象。
定位结果：这不是 flake，而是**跨测试污染**，且 `-count=1` 时污染发生在全部
测试跑完之后，所以标准跑法看不见。

两个独立污染源：

- `model/user_update_billing_scope_test.go` 用 `previousDB` 换过 `DB` 但没
  注册 `t.Cleanup`，后续测试继续用被替换过的库句柄。
- `model/log_stat_test.go` 的三个用例只在测试**开头** `DELETE FROM logs`，
  不在结尾清理。第三个用例的日志被后续测试读到。
- `model/tag_request_header_policy_test.go` 的 `openTestDB` 用固定 DSN，
  `-count=N` 时同名共享内存库跨轮次复用。

修法：`openTestDB` 的 DSN 加单调序号；`truncateTables` 改为开始与结束各清
一次；两个漏 cleanup 的测试补上。

证据：修复前 `go test ./model -count=2` 有 137 个 `--- FAIL`；修复后
`-count=3`、`-count=2 -shuffle=on` 与 5 个随机 shuffle 全部退出码 0。

一次失败尝试记录在此以免重蹈：曾在 `openTestDB` 里加
`t.Cleanup(func(){ sqlDB.Close() })`，导致 153 个 `sql: database is closed`。
原因是 TestMain 的 `:memory:` 库被替换后连接池被关掉。唯一序号方案不触碰
连接生命周期。

### 备用码并发消费

`ValidateBackupCode` 先 `WHERE user_id = ? AND is_used = false` 查出候选、
逐条校验哈希、再 `Save` 标记已用。查询与更新之间没有原子性，并发提交同一
个备用码时每个请求都能通过校验。

实测：8 个并发提交同一备用码，改前 `succeeded = 8`，期望 1。

改为条件写 `WHERE id = ? AND is_used = false` + `Updates`，以
`RowsAffected == 0` 判定"已被别的请求消费"，返回 `false, nil`（与其他
失败路径一致，计一次失败尝试）。调用方只有
`model/twofa.go:291`（`ValidateBackupCodeAndUpdateUsage`），影响面就是
同一备用码的并发提交。这与 TOTP 路径已确立的 `last_used_step` 条件写
契约对齐。

测试 `TestValidateBackupCodeConcurrentSameCodeAllowsOne` 并入
`model/twofa_replay_test.go`，与 TOTP 的同名并发测试放在一起。

### 第四轮验证记录

```bash
go build ./...                                  # 退出码 0
go test ./... -count=1                          # 退出码 0
go test ./model -count=2 -shuffle=on            # 退出码 0
go test ./model -run 'TestValidateBackupCode|TestValidateTOTP' -count=3
```


## 第五轮：controller 剩余文件的穷尽扫描

第四轮之后按 AGENTS.md §9 的索引继续扫 `controller/` 中尚未逐行读过的九个
文件：`channel-test.go`、`channel_upstream_update.go`、`ratio_sync.go`、
`model_sync.go`、`custom_oauth.go`、`passkey.go`、`deployment.go`、
`video_proxy.go`、`twofa.go`（另有 `channel-billing.go` 作为 `GetResponseBody`
的被调方一并核查）。

### 已修复

| 提交 | 内容 | 级别 |
|---|---|---|
| `56718a365` | 视频代理重试时保留 SSRF 校验标记 | P0 |
| `a80d06c84` | 2FA 失败计数与状态更新改增量写，消除快照回滚 | P1 |
| `c61953908` | Passkey 注册对已有凭证要求 step-up | P1 |
| `43c250eef` | 渠道测试错误响应脱敏；巡检不再因缺 context 中断 | P1 / P2 |
| `c914a8657` | 自定义 OAuth slug 冲突检查改用归一化值 | P2 |
| `372f2339e` | 模型同步的 locale 回退与上游拒绝的伪成功 | P2 |
| `3fbd397e7` | 同步创建的模型保留官方同步资格；覆盖失败不再静默 | P2 |
| `605b4ba6c` | 倍率同步过滤 NaN/Inf 与负值，避免响应变空体 | P2 |
| `4ebfd3338` | 上游模型同步加 30s 超时，避免巡检永久卡死 | P2 |
| `c93544082` | 编辑自定义 OAuth 提供商不再清空未改动的 client_secret | P2 |

### 各条的判据与反向验证

`video_proxy.go`：`doVideoProxyRequest` 的每次尝试用调用方请求上下文派生
`attemptContext`，再 `req.Clone(attemptContext)`。Clone 只复制请求头与 body，
不复制 context，于是调用方打的 `common.WithSSRFValidation` 标记被整体替换，
共享 transport 的第一道门（`service/http_client.go:119`）随之失效。三条测试：
标记保留、未标记请求不被误加标记、真实 transport 下回环监听端零命中。去掉
修复后第三条以 "loopback listener must never be reached" 失败。

`twofa.go`：失败路径与备用码成功路径都用 `DB.Save(t)` 整行写回，`t` 是调用方
早先加载的快照。gorm 的 Save 写所有字段含零值，因此并发时会把别人刚推进的
`last_used_step` 覆盖回旧值（已消费的 TOTP 码重新可用），把刚触发的
`locked_until` 抹成 nil（锁定被解除），并让多个并发失败请求互相覆盖计数
（探针实测 8 次并发只记到 3 次，永远到不了 `MaxFailAttempts`）。四条测试先红
后绿，原实现下得到 `last_used_step=0`（期望 1000）、`locked_until=nil`、
并发计数 3（期望 8）。

`passkey.go`：`UpsertPasskeyCredential` 先按 user_id 硬删再插入，即"注册"等于
"替换"。删除路径在未开 2FA 时要求 passkey step-up，注册路径却直接放行，
使会话失陷可升级为长期免密登录（改密码也无效）。门禁对齐删除路径，只有
首次绑定才直接放行。四条测试覆盖首次绑定、替换被拒、step-up 后放行、开了
2FA 时拒绝；原实现下"替换被拒"一条失败。

`channel-test.go`：本地错误 `result.localErr.Error()` 裸回显，而该错误可能来自
URL 解析失败，Go 的错误文本原样嵌入完整 URL（已用探针确认
`http.NewRequest` 的错误含 `?key=AIzaSECRETKEY...`），Vertex 的 api_key 模式与
Baidu 的 access_token 都把密钥放在 URL 里，因此普通管理员可借测试接口读出
root 管理的渠道密钥。同级分支用 `newAPIError.Error()`（内部已脱敏）。另一处：
启用/禁用分支解引用可能为 nil 的 `result.context`（早退路径不填），nil
`*gin.Context` 解引用会 panic 并中断整个巡检循环（gin 1.9.1 的 `Context.Get`
在 nil 接收者上取 RLock，已用探针确认）。

`ratio_sync.go`：`strconv.ParseFloat` 对 `"NaN"`/`"Infinity"` 返回 nil error，
而 NaN 与任何值比较都是 false，能绕过"负值跳过"与"prompt<=0 跳过"两道过滤；
NaN 进入候选倍率后 `json.Marshal` 报错，gin 已发过状态头，整个 `/fetch` 响应
只剩空体，所有渠道结果一起丢失。type1 原始 map 与 type2 的数字同样无校验。
三处统一用 models.dev 分支已有的 `isValidNonNegativeCost`。

`model_sync.go`：`strings.ToLower` 后再与含大写的常量 `zh-CN`/`zh-TW` 比较，
中文 locale 静默回退英文；envelope 的 `Success` 从不被读取，上游 HTTP 200 +
`success:false` 被当作空列表继续同步并回报成功；`Model.Insert` 会把零值写回，
同步创建的模型落成 `sync_official=0`，而 0 的语义是"不参与官方同步"，之后永久
不再被更新；覆盖事务错误被 `_ =` 丢弃且计数在提交前自增。

`channel_upstream_update.go`：`GetResponseBody` 用共享客户端，在
`RELAY_TIMEOUT=0`（默认）时无 Timeout，transport 也无 ResponseHeaderTimeout；
后台巡检用 `CompareAndSwap` 做互斥，卡住后所有周期被跳过。抽出
`GetResponseBodyWithTimeout`（其余 12 个余额查询调用点不受影响），模型拉取
用 30s。测试用只接受连接不写字节的本地监听端验证。

`custom_oauth.go`：slug 冲突检查用未归一化输入，而落库与 registry 键都是
小写，`"GitHub"` 这类变体可覆盖内置 provider，删除时还会把内置项一并从注册表
删掉。前端表单把服务端不回显的 `client_secret` 以空串回填并原样提交，
编辑任意字段都会清空已配置的 secret。

### 判定为正确、不修的条目

- `deployment.go` 只操作 io.net 远程 API，无本地文件读写、无 `os/exec`；
  `container_id` 未做 `url.PathEscape` 可改写上游路径与查询参数，但接口在
  `AdminAuth()` 下且用的是同一把服务端密钥，属管理员自伤，风险低于修复引入
  的兼容成本，记录不修。
- `custom_oauth.go` 的 `AutoMergeByEmail` 依赖 IdP 是否真的验证邮箱，代码侧
  从不读 `email_verified`。这是信任模型选择（默认关闭），不是无条件缺陷。
- `rate-limit.go` 的 `CriticalRateLimit` 对 2FA 登录是 20 次/20 分钟按 IP，
  只能减缓暴力破解；并发计数修复后 `MaxFailAttempts` 才真正生效。
- 渠道 base_url 被当作管理员可信配置，渠道测试与上游同步都不 opt-in SSRF
  校验；不存在非管理员写入 base_url 的路径，因此不构成越权 SSRF 绕过。
- 兑换码 `Redeem` 用 `forUpdate` 行锁（SQLite 上退化为普通查询，但 SQLite 的
  写事务本身串行）；8 并发探针实测 `success=1, finalQuota=500`，3 轮一致。

### 第五轮验证记录

```bash
go build ./...                                  # 退出码 0
go test ./... -count=1                          # 退出码 0，66 个包 ok
go test ./model -count=2 -shuffle=on            # 退出码 0
go test ./controller -count=1                   # 退出码 0
NEW_API_TEST_MYSQL_DSN=<newapi_test_k>   go test ./model -run 'MySQL' -count=1
NEW_API_POSTGRES_TEST_DSN=<newapi_test_pg3> go test ./model -run 'Postgres|PostgreSQL' -count=1
cd web/default && bun run lint                  # 退出码 0
```
