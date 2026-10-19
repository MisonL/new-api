# CR-AUDIT-ROUND-2026-10-19：代理层阻断、登录跳转、流式响应体与方言兼容

本轮对四条只读审计线索（SSRF/文件 IO、计费、relay 并发、前端 XSS、SQL/DB 兼容）
逐条复现后修复。原则是：只有本人复现成功的才改，未复现或已被推翻的条目只记录。

基线：`520f6b264`（dev），本轮提交 `1c02b5c7b`、`32cfa60b0`、`905ae9657`、
`b0c9efc5b`、`6e8c4abfe`。

## 已复现并修复

### 1. 只读代理未阻断 GET 副作用接口（`32cfa60b0`）

`deploy/nginx/frontend-readonly.conf.template` 原先只拒绝非安全方法加五条认证
路径。两份前端 guard（`web/default/src/lib/readonly-guard.ts`、
`web/classic/src/helpers/readonlyGuard.js`）各列了 17 条，代理层缺 12 条
GET 副作用接口，`curl http://127.0.0.1:3300/api/user/token` 可绕过前端直接
落到正式后端。AGENTS.md 第 7 节要求的"双层阻断"不成立。

修复：模板补齐全部路径（含可选尾斜杠），新增
`scripts/readonly-proxy-contract.test.mjs` 比对两份 guard 与代理正则，并接入
`.github/workflows/repo-gates.yml` 的 operational-contracts job。

验证（真实 nginx 1.29-alpine 容器，渲染模板后逐路径 curl）：

| 请求 | 结果 |
| --- | --- |
| `/api/user/token`、`/api/user/token/`、`/api/user%2Ftoken`、`/api//user/token` | 403 |
| `/api/channel/test`、`/api/channel/test/42`、`/api/channel/update_balance/42`、`/api/channel/fetch_models/42`、`/api/channel/42/codex/usage` | 403 |
| `/api/user/aff`、`/api/subscription/epay/return`、`/api/oauth/github`、`/api/verification`、`/api/reset_password`、`/api/user/epay/notify` | 403 |
| `/api/status`、`/api/user/self`、`/api/user/groups`、`/api/channel/models`、`/api/channel/42`、`/api/channel/42/codex/usage/history`、`/api/channel/test-models`、`/api/subscription/plans`、`/api/verification/resend`、`/api/user/tokens` | 502（穿透到不存在的上游） |
| `POST /api/user/token` | 405 |

`node --test scripts/readonly-proxy-contract.test.mjs scripts/dockerfile-contract.test.mjs`：24/24 通过。

### 2. 登录后跳转开放重定向 + VITE 变量从未注入（`905ae9657`）

- `web/default/src/routes/oauth/$provider.tsx` 把 `?redirect=` 原样交给
  `window.location.replace()`；`/sign-in`、`/(auth)/oauth`、`useAuthRedirect`、
  `redirectTo2FA` 同样未校验。新增 `normalizeLoginRedirectTarget`（只接受站内
  绝对路径，拒绝 `//host`、`/\host`、绝对 URL 与认证页自身），五处统一收敛。
- `rsbuild.config.ts` 只调用 `loadEnv`，从未把结果交给 `source.define`。打包后
  `import.meta.env.VITE_REACT_APP_READONLY_MODE` 为 undefined，只读守卫的
  `IS_READONLY_FRONTEND` 被常量折叠为 `false`，整层前端阻断被 DCE 删除。
  补 `source.define = env.publicVars`，并把 publicVars 纳入 `buildCache.cacheDigest`。

验证：

- `bun test tests/redirect-target.test.ts`：7 通过。
- `bun test tests/client-env-injection.test.ts`：修复前 2 失败，修复后 3 通过。
- 带 `VITE_REACT_APP_READONLY_MODE=true` 构建，产物含守卫文案（1 个 chunk）；
  不带该变量构建，产物不含（树摇生效）。
- `bun run lint`、`bun run typecheck` 退出码 0。

### 3. 四个流式处理器在多条退出路径漏关上游响应体（`b0c9efc5b`）

`cohereStreamHandler` 与 `cozeChatStreamHandler` 没有任何关闭点；
`zhipuStreamHandler`、`palmStreamHandler` 的关闭语句位于 `c.Stream` 之后，
写盘 panic 会跳过。未关闭的上游响应体让连接无法归还，上游读取协程挂到超时。

修复：四个函数入口 `defer service.CloseResponseBodyGracefully(resp)`；
zhipu 去掉原先的重复关闭，保证每个请求只关一次。

验证：每个包新增回归测试，先在未修复代码上确认 `closed=0`，修复后通过：
`go test ./relay/channel/cohere ./relay/channel/coze ./relay/channel/zhipu ./relay/channel/palm`。

### 4. MySQL 修正未接线，SumUsedToken 方言不兼容（`6e8c4abfe`）

- `model/main.go` 的 `chooseDB` 用裸 `mysql.Open(dsn)`，`model/mysql_dialector.go`
  的 uniqueIndex 修正只有测试调用，生产 MySQL 的 AutoMigrate 仍会尝试对不存在
  的约束发 `DROP FOREIGN KEY`（MySQL Error 1091）。改为统一经
  `mysqlOpenDialector`，新增 `TestChooseDBUsesTheMySQLCorrection` 断言该入口
  返回的 migrator 会跳过 uniqueIndex 列的约束协调。
- `model/log.go` 的 `SumUsedToken` 使用 `ifnull`，在隔离 PostgreSQL 上实测
  `ERROR: function ifnull(numeric, integer) does not exist (SQLSTATE 42883)`。
  该函数丢弃 Scan 错误，因此失败静默返回 0。改为 `COALESCE`，并把查询构造
  拆成 `sumUsedTokenQuery`，让测试能断言 SQL 错误而非被吞掉的 0。

验证：

- `go test ./controller ./model ./relay/common ./relay/helper ./service`：全部通过。
- 反向验证：还原 `mysql.Open` 后 wiring 测试失败；还原 `ifnull` 后 PG 测试报
  SQLSTATE 42883。
- PostgreSQL 为隔离实例 `127.0.0.1:5433`（`new-api-dev`），未触碰正式库。

### 5. FetchModels 绕过 SSRF 校验（`f03a56540`）

`controller/channel.go` 的 `FetchModels` 通用分支用裸 `&http.Client{}` 请求
请求体中的 `base_url`。共享客户端的 SSRF 校验是逐请求 opt-in 的
（`common.WithSSRFValidation`），裸客户端既没打标记也不受 `RelayTimeout`
约束，因此该接口可以把任意 URL 的响应体取回并返回给调用方。

复现：打开 SSRF 保护、`DomainFilterMode` 设为白名单且列表为空，让
`base_url` 指向本机 `httptest` 服务器。修复前返回
`{"data":["leaked-model"],"success":true}`；修复后传输层拒绝该目的地。

修复：改用 `service.GetHttpClient()` 并显式 `WithSSRFValidation`。
反向验证：去掉该标记后测试重新失败。Gemini 分支走 `FetchGeminiModels`，
原本就使用共享客户端，不在本缺陷内。

## 已复现但判定为不修

- `common.InMemoryRateLimiter.Request` 的零容量路径：探针测试传入
  `maxRequestNum = 0`，两次调用分别返回 `true`、`false`，未 panic；
  `CheckModelRequestRateLimitGroup` 也拒绝成功计数小于 1 的分组配置，默认
  全局总量为 0 时整段检查被跳过。原审计描述的 panic 不可达。
- `relay/channel/api_request.go:sendPingData` 超时后内层 goroutine 持有
  `writeMutex`：属实，但内层 goroutine 正卡在写操作上，任何"超时后交还锁"
  的改法都会让两次写并发，破坏 SSE 写入串行化。已撤回该改动，需要单独的
  写超时 + 连接放弃设计，不在本轮范围。

## 未复现 / 已被推翻

- `DROP INDEX CONCURRENTLY` 经 `PrepareStmt: true` 会被 prepare（SQL 审计第 3 条）：
  用生产 `newGormConfig()` 连接真实 PostgreSQL 执行，`err=<nil>`，索引确实被删除。
  gorm 的 `PreparedStmtDB.ExecContext` 虽走 `prepare()`，PostgreSQL 驱动在此
  路径上没有报错，原结论不成立。
- `SumUsedToken` "没有调用方所以是潜伏缺陷"：确认无调用方，但仍改为 COALESCE，
  避免恢复调用时在 PG 上失败。

## 越权与 IDOR 审计结论（已逐条复核）

审计报告提出四条，逐条核对后的结论：

- "管理员可重置同级管理员的 2FA / Passkey / 绑定"：**不成立**。
  `controller/twofa.go:521`、`controller/passkey.go:355`、`controller/user.go:676`
  三处守卫都是 `myRole <= targetUser.Role && myRole != RoleRootUser` 即拒绝，
  与 `controller/user.go:632` 的 `UpdateUser` 使用同一规则，同级无法互相操作。
- "`GetUserModels` 读取裸 `:id` 参数可越权"：**不成立**。该 handler 挂在
  `/api/user/models`（`router/api-router.go:103`），gin 优先匹配静态路由，
  `Param("id")` 为空，`Atoi` 失败后回落到会话 id。用 gin 最小复现验证：
  `/api/user/models` 命中静态路由，`/api/user/42/models` 返回 404。
- "`TryUserAuth` 是伪鉴权，可能被复用成真实越权"：**当前不可达**。三个调用点
  中 `GetPricing`、`GetUserGroups` 只读，`GetUserGroup(0)` 失败后回落到空分组；
  `perf metrics` 不读 id。作为模式约束记录，不改代码。
- "管理员可见渠道 key 前 10 位（`controller/channel.go:2089-2091`），而完整
  key 要 root"：**属实，属权限模型内暴露**。`/api/channel/multi_key/manage`
  挂 `AdminAuth`。未改，因为收紧会改变现有管理员可用功能，需产品决策。

## 本轮未覆盖

- MySQL 实例未启动：MySQL 侧结论（Error 1091、dialector 行为）来自驱动源码与
  既有单测，未实测。
- `web/default` 全量测试仍有两处既有失败，与本轮无关且在基线
  `520f6b264` 上同样失败：
  - `tests/option-write-order.test.ts`：`optionWriteOrder` 未从
    `src/features/system-settings/models/utils.ts` 导出。
  - `tests/code-block-linkify.test.ts`：`linkifyHtml` 未从
    `src/components/ai-elements/code-block.tsx` 导出。
- 前端未做浏览器内运行时验证，只做了构建产物检查与单元测试。
- 计费、authz/IDOR 两条审计线索的其余条目尚未逐条复现。
