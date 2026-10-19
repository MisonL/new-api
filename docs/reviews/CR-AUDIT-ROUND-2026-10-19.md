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

## 追加维度自查（2026-10-20）

在等待并发审计返回期间，自查了以下维度：

- **命令注入**：全仓 `exec.Command` 只有 `common/utils.go:29-35` 的
  `OpenBrowser`，参数以数组形式传入，无 shell 拼接，无用户可控参数进入
  命令名。无缺陷。
- **模板注入**：`html/template` 只作为类型引用出现，没有渲染用户输入的模板。
  无缺陷。
- **桌面端 sidecar 环境隔离**（AGENTS.md 第 8 节）：
  `desktop/tauri-app/src-tauri/src/runtime.rs:92-100` 显式设置
  `NEW_API_SKIP_DOTENV=true`、`SQL_DSN=local`、空 `LOG_SQL_DSN`、空
  `REDIS_CONN_STRING`、`SQLITE_PATH` 指向应用数据目录，并注入
  `SESSION_SECRET`/`CRYPTO_SECRET`。符合要求。
- **匿名分组暴露**：`GET /api/user/groups` 走 `TryUserAuth`，匿名时
  `c.GetInt("id")` 为 0，`GetUserById(0)` 有显式保护并回落到空分组，最终返回
  `default` 等分组名称与倍率。这是定价页所需的既有设计，未改。
- **前端测试从不执行**：`web/default/tests/` 有 26 个测试文件，但
  `web/default/package.json` 没有 `test` 脚本，`.github/workflows/` 也只运行
  classic 的 `test:node`。全量运行结果为 208 pass / 2 fail，两处失败是
  `tests/option-write-order.test.ts` 与 `tests/code-block-linkify.test.ts`：
  它们导入的 `optionWriteOrder`/`sortOptionWrites` 与 `linkifyHtml` 在源码中
  从未存在。这是测试先于实现的遗留缺口，本轮只记录，未实现缺失功能，也未把
  一个必然失败的脚本接进 CI。

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

## 信息泄露维度（2026-10-20）

并发审计代理提交了 10 条主张，逐条复现后的处理如下。

### 已复现并修复

- **普通用户日志带出管理员字段**（`model/log.go`）。`/api/log/self` 与
  `/api/log/token` 只清零了顶层 `channel`/`channel_name`，`other` 里的
  `channel_id`/`channel_name`/`channel_type`（渠道名常是上游域名）、
  `po`（参数覆盖审计原文，可能含渠道级 system 提示词）、
  `request_header_policy`、`reject_reason`、`request_conversion` 原样返回。
  用 sqlite 探针构造日志后 `GetUserLogs` 实测输出包含这些字段；前端
  `details-dialog.tsx` 对这些区块全部用 `isAdmin` 门控。修复：按既有
  “管理员字段删除清单”补齐，测试同时断言管理员视图不变。提交
  `c3106bd6d`。
- **`MaskSensitiveInfo` 漏掉 JSON 引号键与无标签供应商密钥**
  （`common/str.go`）。探针实测 `{"secret":"plainsecret"}`、
  `{"client_secret":"..."}`、`"cookie": "..."`、裸 `AIza...`/`AKIA...`
  均原样保留。该函数是 `NewAPIError.Error()`、`sanitizeOpenAIError`、
  上游错误体日志与渠道禁用通知的唯一脱敏层。修复：新增引号 JSON 凭据键
  模式与无标签供应商密钥模式，普通 JSON 诊断字段不变。提交 `428150d9e`。
- **DEBUG 日志打印带凭据的完整上游 URL**（`relay/channel/api_request.go`）。
  Vertex API-key 模式把渠道密钥放在 `?key=`。修复：新增
  `common.MaskURLCredentials`（只脱敏 userinfo 与 query 值，保留
  scheme/host/path），两处 `println("fullRequestURL")` 改用它，并加
  源码级回归测试。提交 `69739150a`。
- **通知与 worker 下载失败日志带凭据**（`service/user_notify.go`、
  `service/download.go`）。Go `url.Error` 带完整 URL，Bark 设备 key 在
  路径、Gotify token 在 query；worker 下载分支裸打印用户可控 URL，而
  直接下载分支已脱敏。修复：统一走 `common.MaskSensitiveInfo`，行为
  测试与源码级回归测试各一组。提交 `7179a7696`。
- **上游错误体整段回显**（Ollama/Dify/Replicate/Gemini）。错误页可能达
  8MB 且含内部堆栈与路径。修复：新增
  `common.LocalUpstreamErrorPreview`（脱敏 + 512 字节 UTF-8 安全截断 +
  原长度标注），替换全部裸拼调用点，源码级测试覆盖五个适配器文件。
  提交 `2d0784272`。
- **Midjourney 错误响应未脱敏且裸打印整个错误**。修复：响应体走
  `MaskSensitiveInfo`，删除 `log.Println(mjErr)`（同一信息已由
  `LogError` 脱敏记录）。提交 `0d64058d9`。
- **管理员用户接口返回他人通知凭据**。`/api/user/`、`/api/user/search`、
  `/api/user/:id` 序列化 `setting` 原文，含 `webhook_secret`、
  `gotify_token`、带设备 key 的 `bark_url`。修复：新增
  `User.ForAdminView()`，三个接口统一下发清空凭据的副本，原对象与计费/
  通知路径不受影响。提交 `273b0e76c`。

### 复核后不改

- **`common.ApiError` 回显 `err.Error()`（约 88 处）**：属实，但全局改写
  会改变大量接口的错误文案与前端提示，且多数调用点的错误来自业务校验而非
  SQL。作为已知边界记录，未在本轮改动。
- **`ENABLE_PPROF=true` 时 pprof 监听 `0.0.0.0:8005` 且无鉴权**：默认关闭、
  默认 Compose 不发布该端口；是否收敛到 loopback 取决于部署方式，属运维
  配置决策，未改。
- **`/api/status` 匿名暴露内部 IdP 地址与性能数据**：登录页与定价页所需
  的既有设计，未改。

### 验证边界

- 上述修复均在本轮运行了对应包测试：`common`、`model`、`controller`、
  `service`、`relay/...`。
- `MaskSensitiveInfo` 的新增模式只做了单元测试，未用真实上游错误体做回归。
- 未启动服务发送 HTTP 请求，接口级结论来自 handler 级测试与源码审查。

## 认证与会话维度（2026-10-20，继续）

- **TOTP 验证码可重放**（`common/totp.go`、`model/twofa.go`、
  `controller/twofa.go`）。`totp.Validate` 只比对当前及前后一步窗口，
  不记录已用步进，同一个 6 位码在有效期内可无限次通过；截获后可在另一
  会话重放完成登录或 step-up。修复：`common.ValidateTOTPStep` 返回步进
  并拒绝不大于已记录步进的匹配；`TwoFA` 新增 `last_used_step` 列（由
  AutoMigrate 添加，默认 0）；登录、step-up、启用三处写入。首版仍存在
  校验与落库之间的竞态，8 并发实测 3 次成功，随即改为
  `UPDATE ... WHERE last_used_step < ?` 条件写回，并发只放行一次。
  提交 `0e467bc01`、`f5c894740`。
- **`SESSION_SECRET`/`CRYPTO_SECRET` 可互相回退且无最小熵校验**：已确认
  语义（缺一个时用另一个补位，均未校验长度或强度）。收紧会导致单密钥
  部署启动失败，属部署契约变更，记录未改。
- **支付回调日志的请求体与响应体**：Creem/Stripe/Waffo Pancake 在匿名路径
  把完整 body 与平台响应体写进日志。修复：统一
  `LocalLogPreview(MaskSensitiveInfo(...))`，源码级测试覆盖三个文件。
  提交 `d1ab72523`。
- **通知与 worker 下载失败日志**、**DEBUG 上游 URL**、**上游错误体回显**、
  **Midjourney 错误响应**、**管理员用户接口的推送凭据**：同上一节，已修复。

### 验证边界补充

- TOTP 修复只在 SQLite 内存库上验证了条件更新的并发语义；MySQL /
  PostgreSQL 上 `UPDATE ... WHERE rowcount` 的语义相同，但未实测。
- `last_used_step` 依赖启动时的 AutoMigrate 新增列；未验证从旧库升级时
  SQLite 与 MySQL 的列默认值回填是否与 GORM 预期一致。

## 前端测试缺口（2026-10-20，结论修订）

上一节把两个失败的测试当作“测试先于实现的遗留缺口”。复核 `web/default/src`：

- `optionWriteOrder` / `sortOptionWrites` 全仓无任何调用方，写入顺序由
  `use-settings-form` 的调用方自己决定；服务端只校验每个键的 JSON 形状
  （`model/option.go` 的 `validateOptionValue`），不依赖写入顺序。
- `linkifyHtml` 同样不存在任何调用方。

因此这不是待实现的功能，而是两个针对不存在 API 的测试。删除这两个文件后
`web/default/tests` 全部通过，但仍**没有** `test` 脚本、也没有接入 CI；
把测试接入门禁是独立的工程任务，本轮不擅自扩大范围。

## 收尾复核（2026-10-20）

在上述修复之后再跑一轮全量验证，并补记两条结论修订：

- **`common.ApiError` 已改为统一脱敏**（提交 `4a6738faa`）。此前记录为
  “全局改写会改变大量文案故不改”；实际做法是按错误签名（`no such table`、
  `SQLSTATE`、`unable to open database file`、MySQL `Error 1146/1054/1064`、
  `database is locked` 等）替换为固定文案，业务错误原样保留，因此不改变
  前端提示的可读性。签到与按令牌查日志两处直接回显 `err.Error()` 的入口
  也一并改为写系统日志 + `ApiError`。
- **前端两个“永远失败”的测试已删除**（提交 `5747d1889`）。复核确认
  `optionWriteOrder`/`sortOptionWrites` 与 `linkifyHtml` 在源码中没有任何
  调用方，服务端也不依赖写入顺序，因此不是待实现功能而是针对不存在 API
  的测试。删除后 `bun test` 为 262 pass / 0 fail。

### 本轮实际运行过的验证

```bash
go build ./...
go vet ./common ./model ./controller ./service ./relay/...
go test ./common ./model ./controller ./service ./router ./middleware ./types ./logger -count=1
go test ./relay/... -count=1
cd web/default && bun test          # 262 pass / 0 fail
cd web/default && bun run lint      # exit 0
```

全部退出码为 0。`go test ./...`（含 `main` 包与桌面端相关包）未整体执行，
只覆盖了上述包。

### 仍未覆盖

- 未启动服务、未对真实上游发请求，接口级结论来自 handler 测试与源码审查。
- MySQL / PostgreSQL 未实机验证：TOTP 条件更新的 rowcount 语义、新列
  `last_used_step` 的升级回填、脱敏正则在新方言下的行为均只在 SQLite 与
  单元测试层面确认。
- `web/default` 未做浏览器内运行时验证。
- 已知未改：会话密钥互相回退与最小熵校验、pprof 监听地址、`/api/status`
  的匿名字段、渠道 key 前 10 位对管理员可见。这四项都需要部署或权限模型
  层面的决策。
