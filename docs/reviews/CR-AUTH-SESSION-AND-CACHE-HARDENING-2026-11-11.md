# CR-AUTH-SESSION-AND-CACHE-HARDENING-2026-11-11

## 0. 采样与基线

- 分支：`dev`（`git rev-parse --abbrev-ref HEAD`），相对 `origin/dev` ahead 843。
- 本轮未提交；所有改动留在工作区，命令与结果见第 4 节。
- 工作区同时存在其他会话的改动：`web/classic/src/components/table/channels/modals/EditChannelModal.jsx`、
  `setting/sensitive_test.go`。这两处未被读取用于结论、未被修改、未被提交。

## 1. 范围

本轮处理两类**会话凭据与缓存一致性**缺陷，并对其余已知项给出明确裁决。

## 2. 修复项

### 2.1 会话签发端点的 `no-store` 缺口（`router/api-router.go`）

`/api/verify` 成功后会调用 `setSecureVerificationSession` 写会话
（`controller/secure_verification.go:128`），但路由上原本只有 `UserAuth` 与
`CriticalRateLimit`，没有 `DisableCache`。Passkey 的 6 个端点通过
`SaveSessionData` / `PopSessionData`（`service/passkey/session.go:25`）改写会话，
`login/finish`、`verify/finish` 还会 `setupLogin` 签发会话，同样缺少该中间件。

修复：

- `POST /api/verify` 增加 `middleware.DisableCache()`，并放在 `UserAuth()` **之前**。
  顺序很关键：第一版把它放在 `UserAuth` 之后，鉴权失败时 `Abort()` 先返回 401，
  `no-store` 根本没写进响应，测试直接暴露（`"" does not contain "no-store"`）。
- `POST /api/user/passkey/login/begin|finish` 增加 `middleware.DisableCache()`。
- 4 个已登录态端点（`register/begin|finish`、`verify/begin|finish`）从 `selfRoute`
  拆到独立子组 `passkeySelfRoute`，中间件顺序为
  `DisableCache` → `UserAuth`。留在 `selfRoute` 里时组级 `UserAuth` 先执行，
  401 路径永远拿不到该头。

### 2.2 用户会话世代在缓存里只增不减（`common/redis.go`、`model/user_cache.go`）

`updateUserCache` 原本整对象 HSET，只把 `Quota` 排除在外。`SessionEpoch` 参与
`stored != current` 判定（`middleware/auth.go:78`），而整值回写可能写入调用方更早
读到的快照，把已递增的世代**调低**：改密码后旧 Cookie 的 `stored` 恰好等于被调低的
`current`，会话在缓存 TTL 内重新通过。同一次错误也会把正常新会话判成已吊销。

修复：新增 `common.RedisHSetFieldIfGreater`（Lua 内比较，字段缺失或新值更大才写），
`updateUserCache` 把 `SessionEpoch` 移出整对象回写、改走该函数。世代在数据库里只由
`bumpUserSessionEpochTx` 递增，因此「只增不减」即可封闭这条竞态，且不需要版本键。

`Quota` 的处理保持不变（`HSETNX` 建基数），因为它的正确语义是「增量维护、只建一次」。

## 3. 明确不修的三项与裁决理由

| 项 | 裁决 | 理由 |
| --- | --- | --- |
| 任务终态 CAS 与结算之间的崩溃窗口 | 保持现状 | 当前顺序是「先落终态，再结算，失败则回退」。崩溃只会**漏收**（上游交付但未扣款），不会**双扣**。若改成先结算后落终态，崩溃后任务仍是非终态、下一轮会再结算，直接变成双扣——比现状更严重。要真正修好必须把结算入参随终态 CAS 原子落库，再加幂等补偿扫描，属于数据模型改动；在无法用真实 PG/MySQL 做崩溃注入验证前不动。 |
| `/api/*` 的 CORS | 保持现状 | `/api` 的鉴权主体是会话 Cookie（另有 `New-Api-User` 头），而 `middleware.CORS()` 是 `AllowAllOrigins=true` + `AllowCredentials=false`（`middleware/cors.go:11-18`）。跨源请求带不上 Cookie，因此给 `/api` 加这层 CORS 不会让任何合法的浏览器流程变得可用；bearer 客户端是服务端到服务端，本就不经过 CORS。放开只会放宽面而不产生价值。 |
| Tauri sidecar 环境隔离 | 已修（见 `CR-ROUND4` 之后的 `runtime.rs` 改动） | `runtime.rs` 已在显式 `.env(...)` 前加 `.env_clear().envs(passthrough)`，透传白名单由 `sidecar_passthrough_env_keys()` 定义并有单测锁定。实机启动行为本机无法验证，已在该条目注明。 |

## 4. 验证记录

以下命令均在本轮实际运行并读取输出。

| 命令 | 退出码 | 摘要 |
| --- | --- | --- |
| `go build ./...` | 0 | 无输出 |
| `go vet ./common ./model ./router ./middleware` | 0 | 无输出 |
| `go test ./common ./model ./router ./middleware -count=1` | 0 | 四包全 ok |
| `go test ./controller ./model ./relay/common ./relay/helper ./service ./middleware -count=1` | 0 | 六包全 ok |
| `go test ./router -count=1` | 0 | 含新增的会话端点 `no-store` 断言 |
| `gofmt -l router/` | - | 无输出 |

前端与桌面端（本轮未改动，沿用上一轮结论）：

| 命令 | 退出码 |
| --- | --- |
| `cd web/default && bun run typecheck` | 0 |
| `cd web/default && bun run lint` | 0（0 error / 22 warning） |
| `cd web/default && bun test src/features/auth/lib/custom-oauth-action.test.ts` | 0（10 pass） |
| `cd web/default && bun run build` | 0 |
| `cd desktop/tauri-app/src-tauri && cargo test --test runtime_support` | 0（31 passed） |

### 4.1 反向验证（确认测试会因正确的原因失败）

不只看绿灯，还扰动修复点确认断言真的在起作用：

| 扰动 | 结果 |
| --- | --- |
| 去掉 `/api/verify` 的 `DisableCache` | `FAIL`：`POST /api/verify 会下发或消费会话，必须带 no-store（实际码 401）` |
| passkey 子组改回 `UserAuth()` 在前 | `FAIL`：`POST /api/user/passkey/register/begin 会下发或消费会话，必须带 no-store（实际码 401）` |
| `SessionEpoch` 放回整对象回写、去掉 `IfGreater` | `FAIL`：`expected: "3" actual: "1"`，即旧快照把世代从 3 调回 1 |

三次扰动后均已还原，相关测试重新通过。

### 4.2 后续自查发现并修复的回归

- 自定义 OAuth 登录的 toast 文案是新增的 `t('...')` 字面量，但 6 个语言文件都没登记。
  `bun run i18n:coverage` 退出码 1，报 `en/fr/ja/ru/vi/zh 各缺少 2 个代码中使用的键`。
  `scripts/sync-i18n.mjs` 只对齐语言文件之间、**不扫描源码**（源码扫描在
  `i18nDefaultCoverageGate.mjs`），所以新增文案必须显式写进语言文件。已按各语言包的
  字母序插入 `{{n}}` 与 `{{value}}` 之间并保持原始格式补齐；补齐后
  `i18n:coverage`、`i18n:complete` 与 classic 侧两个 i18n 门禁全部通过。

### 4.3 本轮补跑的其它门禁

| 命令 | 退出码 | 摘要 |
| --- | --- | --- |
| `go vet ./...` | 0 | 全量，无输出 |
| `cd web/default && bun run format:check` | 0 | `All matched files use Prettier code style!` |
| `cd web/default && bun run test` | 0 | 313 pass / 0 fail（44 个文件） |
| `cd web/default && bun run i18n:coverage` | 0 | 3795 个字面量键 × 6 个语言文件 |
| `cd web/default && bun run i18n:complete` | 0 | 两个前端均无未翻译多词文案 |
| `cd web/classic && node ../scripts/i18nCoverageGate.mjs` | 0 | 7 个语言文件键齐全 |
| `cd web/classic && node ../scripts/i18nLintGate.mjs` | 0 | 62 条 == 基线，新增 0 条 |
| `cd web/default && bun audit --production` | 0 | 593 个依赖，无漏洞 |
| `cd web/classic && bun audit --production` | 0 | 661 个依赖，无漏洞 |

### 4.4 偶发与跳过

- `TestManageUserOverrideInvalidatesQuotaCache` 在本轮一次 `./controller` 定向运行中失败
  （`override 后缓存不得继续持有旧额度`）。单跑 3 次、`-count=30`、以及随后 3 次整包
  `go test ./controller -count=1` 均通过。该用例的偶发性质在第 4 轮记录第 5 节已有记载，
  根因是异步回写在键被删除后重建，与本轮改动无关（本轮只改 `SessionEpoch` 写入语义）。
- 未运行 `go test ./...` 全量（改为 `go vet ./...` 与关键包 `-count=1`）、未做真实
  MySQL/PostgreSQL 并发、真实 Redis 竞争、崩溃注入与真实浏览器 passkey/OAuth 往返。

## 5. 边界声明

- 2.1 的结论来自路由中间件顺序与测试断言，未在真实浏览器上跑通 passkey/verify 全流程。
- 2.2 的结论来自 miniredis 上的 Lua 行为与单测，未在真实 Redis 集群上验证 Lua 脚本
  在 `EVALSHA` 缓存淘汰后的重放路径。
- 第 3 节三项的裁决基于代码路径与实际部署形态（会话 Cookie 鉴权、Redis 可选、
  异步任务崩溃概率），未做生产事故复盘。
