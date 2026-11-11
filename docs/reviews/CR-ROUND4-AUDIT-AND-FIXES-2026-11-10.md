# 第 4 轮全维度审查与修复记录（2026-11-10 至 2026-11-11）

## 0. 采样与基线

- 审查基线提交：`e34a97ae6`（dev，`git rev-parse HEAD`）。
- 收尾提交：`533d2cd9f`。
- 隔离开发环境：`new-api-dev-isolated-new-api-1`，端口 `127.0.0.1:3001`，PG `5433`，Redis `6380`。
- 收尾容器镜像 `new-api-local:dev`，`--build-info` 输出
  `commit=fcdc7d79a0e64cc50d337b291be768f7ab251c83-dirty`、`date=2026-11-11T00:04:37Z`。
- 审查与修复期间工作区存在其他会话的未提交改动
  （`web/classic/src/components/table/channels/modals/EditChannelModal.jsx` 已修改、
  `setting/sensitive_test.go` 未跟踪）。这两处未被读取用于结论、未被修改、未被提交。

## 1. 方法

七条只读审查线并行：计费与中继、安全与鉴权、数据层与三库兼容、部署与开发环境、
前端与 i18n、既有修复回归复核、运行时黑盒实测；另有一个 Codex worker 经 Orca
编排执行独立审查（dispatch `ctx_6b21084d9dad`，报告存放于 `/tmp/newapi-audit-e34a97ae6-crJgCQ/report.md`）。
所有 P0/P1 声明在动手前都在源码层复核；本轮推翻了其中两条，并调和了一条相互矛盾的结论。

## 2. 本轮修复（6 个原子提交）

| 提交 | 内容 | 证据 |
| --- | --- | --- |
| `9d89432e2` | 修复 `web/default` format 门禁（导入顺序、断言折行） | `bun run format:check` 由退出码 1 转为 0 |
| `0a6ee9265` | realtime 逐 turn 直扣改用冻结分组倍率，去掉实时覆盖与调试打印 | 新增回归测试：实时倍率 3、冻结 1 时扣费必须是 3000 |
| `3a050a380` | `User.Update(false)` 不再回写陈旧快照中的密码哈希 | 陈旧快照保存资料后新密码仍有效、旧密码失效、世代不变 |
| `dea97b8ec` | 上游已成功但结算失败的任务不再整笔退款 | 超时清扫保留预扣并记录人工核对原因；对照组仍正常退款 |
| `c720e07fe` | OAuth 会话端点补 `no-store` | 路由级测试 + 运行时响应头实测 |
| `fcdc7d79a` | 桌面确认凭据 Cookie 的 `Secure` 识别代理 TLS | 表驱动测试覆盖直连 TLS、forwarded https、逗号链、纯 http |
| `533d2cd9f` | realtime 终态结算沿用冻结的补全与音频倍率 | 实时补全倍率 4、冻结 1 时终态必须按 1 结算 |

### 2.1 format 门禁（`9d89432e2`）

`ec0ad74bd` 引入两处 Prettier 违规，`repo-gates` 的 `web/default format` 步骤会失败。
修复只涉及导入排序与单行断言折行，无语义变化。

### 2.2 realtime 逐 turn 定价（`0a6ee9265`）

`PreWssConsumeQuota` 的注释声明定价一律取 `PriceData` 冻结快照，代码却按
`ContextKeyAutoGroup` 读实时 `GetGroupRatio` 覆盖，并每 turn 输出一行
`log.Printf("final group ratio")`。终态 `PostWssConsumeQuota` 用的是冻结值，
两者不同源。反向验证：修复前同一测试扣 9000（期望 3000）并打印
`final group ratio: 3.000000`，修复后扣 3000 且该行消失。

### 2.3 陈旧快照写回密码（`3a050a380`）

`userUpdateOmittedColumns` 只排除计费列与 `SessionEpoch`，而 `Update` 用调用方
快照做整对象 `Updates`，且 `newUser := *user` 在 `DB.First` 之前复制。常见管理
路径「管理员读到用户（含哈希）→ 用户重置密码 → 管理员保存资料」会把旧哈希写回，
这条路径不递增世代，旧密码重新可登录。修复：把 `Password` 加入 Omit 列表，改密
只走 `updatePassword=true` 分支里的显式单列写入，并与世代递增同事务。补充
`Update(true)` 正向断言，避免修复退化成「任何入口都改不了密码」。

### 2.4 上游成功任务被整笔退款（`dea97b8ec`）

结算失败会把成功终态回退到未完成等待重试，但任务重新进入未完成集合后，超时清扫
与本地失败标记会把它当普通失败并整笔退款：上游已交付的产出变成免费。修复：新增
私有数据标记 `UpstreamSucceeded`，在回退成功终态的同一条语句里置位；五条失败路径
在标记存在时保留预扣并记录人工核对原因——超时清扫（`sweepTimedOutTasks`）、
本地失败标记（`markTaskLocalFailure`）、Suno 构建失败、轮询器在上游报告失败时的
退款分支、realtime 中继的失败退款分支；重新观察到上游成功时由 CAS 清回 false。

### 2.5 OAuth 会话端点 no-store（`c720e07fe`）

`/api/oauth/state` 会种会话 Cookie、`/api/oauth/desktop/poll` 在授权完成时签发会话、
`/api/oauth/:provider` 回调写登录态，但都没有 `DisableCache`。补齐后
`DisableCache` 位于限流之前，被拒绝的响应同样带 `no-store`。反向验证：临时移除
poll 路由的 `DisableCache`，路由测试即在 poll 处失败，恢复后通过。

需要更正当时提交信息里"移除任一 DisableCache 测试即失败"的说法：该测试只钉住了
`state` 与 `desktop/poll` 两个路由，移除 `desktop/start`、`desktop/confirm`
（GET/POST）或 `/oauth/:provider` 的 `DisableCache` 并不会让它失败。第 5 轮把
覆盖扩到微信 / Telegram 的登录与绑定端点（提交 `8e87f30a6`）。

### 2.6 桌面确认凭据 Cookie 的 Secure（`fcdc7d79a`）

确认凭据 Cookie 只看 `c.Request.TLS`，而同一页面的 `desktopOAuthRequestOrigin`
已信任 `X-Forwarded-Proto`。TLS 在反向代理终止时 Go 看到明文请求：来源校验认为
客户端在 https，凭据却没有 `Secure`。修复：把 HTTPS 判定导出为
`middleware.RequestIsHTTPS`，会话 Cookie 与确认凭据共用同一规则。

### 2.7 realtime 终态结算的倍率口径（`533d2cd9f`）

`calculateAudioQuota` 每次回查 `ratio_setting` 取补全/音频倍率，而模型倍率与分组
倍率取自 `PriceData` 冻结快照：连接期间管理员改倍率，同一请求的逐 turn 直扣与
终态结算会按两套价格计算，日志里打印的倍率与实际计费也会分叉。

把三个倍率纳入 `QuotaInfo`，逐 turn 预扣、终态结算、音频结算三个构造点统一从
`PriceData` 取值；只有冻结值为 0 的按次计费与分档表达式路径才回查实时配置，这两
条路径分别走 `UsePrice` 分支或由表达式结算覆盖，行为不变。反向验证：修复前同一
测试扣 3000（期望 1500）并打印按实时倍率 4 计算的金额，修复后扣 1500。

## 3. 复核后推翻的结论

- **`.app.tar.gz` 产物缺失（部署线 P1 候选）不成立。**
  `git show v1.1.0:.github/workflows/tauri-release.yml | grep app.tar.gz` 无输出，
  说明该硬依赖是在 tag `v1.1.0` 之后（`a22f02cb1` 2026-04-19 17:40:42 +0800、
  `30679f5fb` 20:01:40 +0800）才加入的；而 run `24628614421` 于
  2026-04-19T12:02:15Z（晚于 `30679f5fb` 35 秒）启动，四个 job 全部 success，
  其中 Upload job 正是执行 `pick_one '*.app.tar.gz'` 的那一步。当前
  `bundle.targets: "all"`、无 `createUpdaterArtifacts` 的配置确实会产出该文件。
- **`READONLY_UPSTREAM` 默认 3000 与生产 13000 不匹配不构成缺陷。**
  该值位于 `deploy/env/frontend-readonly.env.example`，是需复制后由使用者填写的示例。
- **R02（已结算后又退款）调和结论。** 计费线与 Codex 的结论相互矛盾：前者正确指出
  「结算成功后不可能再退款」（终态不可逆），但漏掉「结算失败后回退、再被超时清扫
  退全款」这条路径。该路径已按 2.4 修复。

## 4. 已确认但未修复（附原因）

结构性或需要产品决策，第 4 轮未改动（第 5 轮状态见条目注记）：

- 任务终态 CAS 与结算之间的崩溃窗口：终态行不会再被任何轮询选中，崩溃即永久漏收/漏退。
  需要新增持久化待结算标记与幂等补偿扫描，且结算依赖 provider 结果，属于设计要求。
- 价格快照不完整：`TaskBillingContext` 未保存 `CompletionRatio` 等参数，异步任务终态
  重算读实时配置；realtime 终态 `calculateAudioQuota` 同样回查实时音频/补全倍率。
  第 5 轮已修复（`533d2cd9f` 引入冻结快照，`a9152aa6f` 改为显式 `RatiosFrozen` 标记，
  `a3660828e` 在 `TaskBillingContext` 增加 `CompletionRatio` 与 `Frozen`）。
- 用户缓存的异步回写竞态（同一根因的两种表现）：`updateUserCache` 用整对象 HSET，
  只对 `Quota` 用 `HSETNX` 建基数。其一，回退路径写的 `session_epoch` 可能是旧值，
  已吊销会话在一个 TTL 内重新通过；其二，缓存键被失效删除后，更早读到旧行的回写
  会重建该键并把旧额度写回（`TestManageUserOverrideInvalidatesQuotaCache` 在负载下
  暴露此路径）。正确修法需要在 Redis 侧做比较写入（版本键 + Lua/CAS），并把「权威写」
  与「回写」区分开；只改一半会让吊销失效或把正常用户踢下线，本轮未改。
- `taskAdjustTokenQuota` 失败只记日志，资金与令牌账本可能分叉；正确处理需要幂等补偿。
- `/api/*` 的 CORS：`SetApiRouter` 在 `router.Use(middleware.CORS())` 之前执行，
  预飞经 NoRoute 返回 204 与 `Allow-Origin`，真实响应却没有该头。是否允许 `/api/*`
  跨源属于产品与安全决策，未擅自放开。
- Tauri sidecar 未 `env_clear`，宿主 `NODE_TYPE`/`UPDATE_TASK`/`TRUSTED_PROXIES`/
  代理变量会继承；本环境无法运行桌面端验证，未改。
- 生产 compose 未设置 `LOG_RETENTION_DAYS`，应用日志默认不清理；保留策略属于运维决策。
- 自定义 OAuth 绑定入口在 `web/default` 指向回调端点且不带 `state`，点击得到 403；
  需要按 provider 类型实现起始流程，未做浏览器实测，未改。
- 侧栏聊天预设使用列表接口返回的脱敏 key 拼链接，需改走取真实 key 的接口。

## 5. 验证记录

以下命令均在本轮实际运行并读取输出。

| 命令 | 退出码 | 摘要 |
| --- | --- | --- |
| `cd web/default && bun run format:check` | 0 | `All matched files use Prettier code style!`（修复前为 1，报两个文件） |
| `cd web/default && bun run lint` | 0 | 0 error / 22 warning（与基线一致） |
| `cd web/default && bun test src/lib/sanitize-html.test.ts` | 0 | 10 pass / 0 fail |
| `go test ./service -run TestPreWssConsumeQuota -count=1` | 0 | 5 个用例通过，含新增的冻结倍率用例 |
| `go test ./model -run 'TestUserUpdate.*' -count=1` | 0 | 陈旧快照与显式改密两个用例通过 |
| `go test ./service -run 'TestSweepTimedOutTasks|TestRevertTaskTerminalTransitionMarksUpstreamSucceeded' -count=1` | 0 | 跳过退款、对照退款、回退置位三个用例通过 |
| `go test ./router -count=1` | 0 | 含新增 OAuth no-store 路由测试 |
| `go test ./middleware ./controller -count=1` | 0 | 通过 |
| `go vet ./...` | 0 | 无输出 |
| `go test ./... -count=1` | 0 | 全部包通过 |

运行时验证（容器 `new-api-dev-isolated-new-api-1`，镜像 `commit=fcdc7d79a...`；该镜像早于 `533d2cd9f`）：

- `/api/healthz` 首次探测即 200，启动日志 `database migration finished: main in 1.291s`、
  `ready in 1425 ms`，无 panic。
- `/api/oauth/state` 返回 `Cache-Control: no-store, no-cache, must-revalidate, private, max-age=0`。
- `POST /api/user/login` 同样带 `no-store`（回归确认）。
- `GET /api/oauth/desktop/confirm?state=probe` 无有效凭据时返回 404 且带 `Cache-Control: no-store`。

跳过与偶发：

- `TestManageUserOverrideInvalidatesQuotaCache` 在全量 `go test ./...` 中失败过一次，
  单跑、`-count=20`、以及两次 controller 整包运行均通过。排查后确认不是构建争用：
  `updateUserCache` 在整对象 HSET 中跳过 `Quota`，随后用 `RedisHSetFieldIfAbsent`
  建基数；若 override 路径删除了缓存键，一个更早读到旧行的异步回写会重建该键并把
  旧额度写回。这与第 4 节列出的 `session_epoch` 回写竞态同一类，已单列为已知问题，
  本轮未修（需要版本化缓存或 Redis 比较写入，见第 4 节）。
- 未运行前端 `bun run build` / `bun run typecheck`：本轮前端改动仅为格式化与注释级，
  且 `format:check`、`lint`、单测均已通过。
- 未做真实 MySQL/PostgreSQL 并发、真实 Redis 竞争、真实崩溃注入、真实上游与浏览器
  OAuth 往返、桌面端 sidecar 启动验证。

## 6. 边界声明

- 本轮修复均基于 SQLite 测试库与本地隔离容器验证，不代表三库全量验收。
- `go test ./...` 的输出里包含 `web/*/node_modules/**` 下的 Go 包（无测试文件），不属于本项目代码。
- 修复后的镜像标签带 `-dirty`（工作区存在其他会话的未提交改动），不能据此推断镜像内容
  与提交时的树完全一致。
