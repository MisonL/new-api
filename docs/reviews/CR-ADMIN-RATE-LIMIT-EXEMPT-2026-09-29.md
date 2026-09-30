# CR-ADMIN-RATE-LIMIT-EXEMPT-2026-09-29

## 背景

渠道管理页（classic 主题）持续弹出 `错误：请求次数过多，请稍后再试！`，并伴随
`Failed to load status`，后台基本不可用。

根因不是阈值本身，而是分桶粒度：`middleware/rate-limit.go` 的全局限流按
`c.ClientIP()` 分桶。反代未配置可信代理时 gin 的 `SetTrustedProxies(nil)` 会让
`ClientIP()` 忽略转发头，全部客户端塌缩到同一个 bucket（隔离环境实测为
`rateLimit:GA172.18.0.1`）。管理员浏览后台把额度耗尽后，连"改阈值"所需的设置页请求
也被 429，形成死锁。

用户已明确决定不关闭 13000 端口、不在正式环境设置 `TRUSTED_PROXIES`
（`13000暂时不收`），因此本次只处理"放宽阈值 + 管理员豁免"这一条路径。

## 改动

1. 新增选项 `GlobalRateLimitAdminExemptEnabled`（bool，默认 false）。首次启动由环境变量
   `GLOBAL_RATE_LIMIT_ADMIN_EXEMPT` 播种；数据库已有该选项时以库内值为准。
2. 豁免只作用于 `GlobalWebRateLimit`、`GlobalAPIRateLimit`、`SearchRateLimit`、
   `SuggestionRateLimit`。
3. `CriticalRateLimit` 与 `DesktopOAuthPollRateLimit` 刻意不豁免：登录、注册、找回密码、
   支付、查看密钥等入口对管理员仍保留 IP 级保护。
4. 失败关闭：无 session、session 不可读、账号已停用（`status != UserStatusEnabled`）、或角色低于
   `RoleAdminUser`(10) 时照常限流。该中间件是全局中间件，运行在 `UserAuth` 之前，无法依赖用户记录
   已被重新读取，因此以 `setupLogin` 写入 session 的 `role` 与 `status` 为准。
5. 后台开关同时落在 classic（当前生效主题）与 default 两套前端。
6. 顺带修复 classic `SettingsRequestRateLimit.jsx` 的既有缺陷：其 effect 用自身可变的
   `inputs` 过滤 `props.options`，导致首屏 options 中不存在的键被永久丢弃，任何新字段都
   无法渲染。改为模块级字段清单 `REQUEST_RATE_LIMIT_FIELDS`。
7. `docker-compose.yml` 此前完全没有把限流相关环境变量透传给容器，`.env` 中设置这些变量
   是静默无效的。补齐 `GLOBAL_API_RATE_LIMIT`、`GLOBAL_WEB_RATE_LIMIT`（含 DURATION）与
   `GLOBAL_RATE_LIMIT_ADMIN_EXEMPT` 的透传，并在 `.env.example`、
   `deploy/compose/dev-isolated.yml`、`deploy/env/dev-isolated.env.example` 中记录。

提交（`dev`，均未推送）：

- `99f3169c8 feat(rate-limit): let administrators bypass the global rate limits`
- `dc030333d`、`2877e5002`、`a78da04fa`：前端字段与 i18n 补齐
- `77cfbc69d fix(rate-limit): keep disabled accounts out of the admin exemption`
- `797ed0268`、`c160a87b5`、`0b3fc7f88`、`3cd92bc45`：复核记录与 dev 环境示例
- `3a273494a fix(rate-limit): normalize the admin-exempt option before it is persisted`

## 环境

- 隔离开发环境 `deploy/compose/dev-isolated.yml`，容器
  `new-api-dev-isolated-new-api-1`，镜像 `new-api-local:dev`
- `--build-info`：`commit=ef093ab64a0e717347504886eea8b1f2f6c1dadd-dirty`
- 采样时间：2026-09-29
- 限流阈值取隔离环境默认值：`GLOBAL_API_RATE_LIMIT=180`、`GLOBAL_WEB_RATE_LIMIT=60`、
  `CRITICAL_RATE_LIMIT=20`、`SEARCH_RATE_LIMIT=10`、`SUGGESTION_RATE_LIMIT=60`
  （正式环境 `.env` 已改为 600 / 300，见下文"未覆盖边界"）
- 管理员会话与普通用户会话通过独立工具按 `main.go` 的 cookie store 参数签发
  （`SESSION_SECRET` + `SameSite=Strict`），未改数据库、未改任何密码。
- 每组探针前删除对应 Redis bucket（`rateLimit:GA*`、`rateLimit:SR:user:*`、
  `rateLimit:SG:user:*`、`rateLimit:CT*`），避免前一组污染后一组。

## 结果

| # | 场景 | 请求数 | 结果 |
| --- | --- | --- | --- |
| 1 | 管理员 + 豁免开启，`/api/user/self` | 250 | 250x200 |
| 2 | 匿名，`/api/user/self` | 250 | 180x401 + 70x429 |
| 3 | 管理员 + 豁免开启，`/api/log/self/search`（限 10） | 30 | 30x200 |
| 4 | 普通用户，`/api/log/self/search`（限 10） | 15 | 10x200 + 5x429 |
| 5 | 管理员 + 豁免开启，`/api/log/self/suggestions`（限 60） | 80 | 80x200 |
| 6 | 普通用户，`/api/log/self/suggestions`（限 60） | 70 | 60x200 + 10x429 |
| 7 | 管理员 + 豁免开启，`/api/ratio_config`（Critical，限 20） | 25 | 20x403 + 5x429 |
| 8 | 管理员，豁免关闭后 `/api/user/self` | 250 | 180x200 + 70x429 |
| 9 | 管理员，豁免重新开启后 `/api/user/self` | 250 | 250x200 |
| 10 | 写入非法值 `yes-please` | 1 | `success:false`，库内值保持 `true` |

第 7 行是本次最关键的反向不变量：CriticalRateLimit 对管理员不豁免。403 来自
`/api/ratio_config` 自身（该接口路由未挂 `UserAuth`），429 出现在第 21 次，说明限流确实
按 20 生效。

第 8、9 行通过后台 `PUT /api/option/` 真实切换开关，同时覆盖了"库值覆盖播种值"与
"开关在运行期立即生效"两个方向。

上表是首轮在隔离环境取得的实测结果。复核阶段补出 `status` 校验后，用重建的镜像
（`commit=797ed02686b7c740d5a1d1250741bd366717bae2-dirty`）重跑了全部探针，并追加了禁用账号
与无 `status` 会话两组反向用例：

| # | 场景 | 请求数 | 结果 |
| --- | --- | --- | --- |
| 11 | 管理员会话但 `status=disabled`，`/api/user/self` | 250 | 180x200 + 70x429 |
| 12 | 管理员会话但无 `status`，`/api/user/self` | 250 | 180x200 + 70x429 |

第 11、12 行是复核修复的直接实证：修复前 `status=disabled` 的会话返回 250x200（仍豁免），
修复后回落到与普通限流一致的 180/70 分布。其余 10 行结果与首轮完全一致，`status` 校验未
影响任何启用状态账号的行为。

## 代码级验证

| 命令 | 结果 |
| --- | --- |
| `gofmt -l middleware/ common/ model/ setting/operation_setting/` | 无输出 |
| `go build ./...` | exit 0 |
| `go test ./middleware/ -count=1` | ok，2.930s（含 8 个新增豁免用例） |
| `go test ./controller ./model ./relay/common ./relay/helper ./service -count=1` | 全部 ok |
| `cd web/default && bun run lint` | 通过 |
| `cd web/default && bun run typecheck` | 通过 |
| `cd web/default && bun run build` | 通过，Safari 兼容检查通过 |
| `cd web/classic && bun x prettier --check src/pages/Setting/RateLimit/SettingsRequestRateLimit.jsx` | 通过 |
| `cd web/classic && bun run eslint` | 通过 |
| `cd web/classic && bun run i18n:lint` | `未发现超出基线的 i18n lint 新问题` |
| `cd web/classic && bun run build` | 通过，Safari 兼容检查通过 |

`go test ./middleware/` 现含 10 个豁免用例，其中 8 个为首轮用例，2 个为复核阶段新增的
反向用例（被禁用账号、无 `status` 会话）。变异测试：把 `adminRateLimitExempt` 强制返回 false
后全部失败；单独移除 `status` 校验后，新增的两个用例失败而其余通过。因此它们约束的是
真实实现，不是同义反复。

界面侧确认：`/console/setting?tab=ratelimit` 下 `管理员豁免全局限流` 开关渲染为
`checked: true`，与库内值一致。

## 复核阶段发现并修复的三个问题

首轮提交后做了多轮全面复核，每轮都发现并修复了一处自身缺陷：

1. **被禁用的管理员仍然豁免**。`adminRateLimitExempt` 只读 session 的 `role`，未读 `status`。该中间件
   是全局中间件，运行在 `UserAuth` 之前，而 `UserAuth` 才会从库里重新读取用户并拒绝已停用账号，
   因此在被拒绝之前，一个属于"已停用管理员"的 session 仍会跳过全局限流。已补 `status` 校验。
   实证：`role=admin` + `status=disabled` 的会话此前返回 200，现在返回 429；无 `status` 的会话
   同样按失败关闭处理。移除该校验会让两个新增用例失败（变异测试）。
2. **新增的前端文案未登记到 i18n**。`web/default` 的两个英文串未写入 base locale `en.json`，
   导致项目脚本 `bun run i18n:sync` 直接 `exit 1`（`Found 2 source translation key(s) missing`）；
   `web/classic` 的四个中文串未登记，全部语言都回退到中文原文，而该区块的兄弟键在 classic 的
   7 个语言里均有译文。两处均已按各前端既有惯例补齐，并按文件原有的排序位置插入。
3. **新选项的非规范布尔值被接受却静默失效**（第三轮复核发现，提交 `3a273494a`）。
   `UpdateOption` 的写入值来自 `normalizeOptionValueForInMemoryConfig` 的返回值，而
   `validateOptionValue` 只返回 error，它经 `NormalizeRateLimitAdminExempt` 算出的规范值被丢弃，
   同时 `normalizeOptionValueForInMemoryConfig` 当初漏了该键的 `case`。结果是
   `strconv.ParseBool` 认得的写法（`"True"`、`"TRUE"`、`" true "`、`"1"`、`"t"`、`"T"`）全部
   `success:true`，却把原始串原样写进库，运行期再按 `value == "true"` 严格比较而读成 false，
   即"配置保存成功但实际未生效"的伪成功路径。
   修复前隔离环境实测（每轮先清 `rateLimit:GA172.18.0.1`）：

   | 写入 | 接口返回 | DB 值 | 200 次探测中的 429 | 实际效果 |
   | --- | --- | --- | --- | --- |
   | `true` | success | `true` | 0 | 生效 |
   | `True` | success | `True` | 20 | 未生效 |
   | `TRUE` | success | `TRUE` | 20 | 未生效 |
   | ` true ` | success | ` true ` | 20 | 未生效 |
   | `1` | success | `1` | 20 | 未生效 |
   | `t` / `T` | success | `t` / `T` | 20 | 未生效 |

   修复后同一组探针（dev 镜像 `commit=3cd92bc4527caa5d935bc4de4d58fd82415e816f-dirty`）：

   ```
   "true"    DB=true   429=0   OK      "false"   DB=false  429=20  OK
   "True"    DB=true   429=0   OK      "FALSE"   DB=false  429=20  OK
   "TRUE"    DB=true   429=0   OK      "0"       DB=false  429=20  OK
   " true "  DB=true   429=0   OK
   "1"       DB=true   429=0   OK
   "t"       DB=true   429=0   OK
   "T"       DB=true   429=0   OK
   ```

   非法值仍被拒绝且不落库（每轮先清桶，避免上一组探测把 IP 打进限流而误读为 429）：
   `broken`、`yes`、空串、`2`、`Truex` 均返回
   `{"message":"strconv.ParseBool: parsing \"...\": invalid syntax","success":false}`，
   库内保持 `false`。回归测试 `TestUpdateOptionNormalizesAdminRateLimitExemptBeforePersist`
   覆盖 7 个接受写法与 4 个拒绝写法；删掉 `normalizeOptionValueForInMemoryConfig` 里的该
   `case` 后测试在 `raw="True" must be stored as the canonical form` 处失败（变异测试）。

补充验证：`web/default` 的 `i18n:sync` 由失败转为 `exit 0`（928 files / 4674 keys / 0 missing）；
`web/classic` 的 `i18n:sync` 报 `All locales are already in sync`，缺失键计数由 28 降为 24，
其余 24 个属既有缺口。上述两处 i18n 修复均只暂存本任务新增的行，未触碰同一文件里其他会话的未提交改动。

## 优化轮（2026-09-30）

复核通过后又做了一轮不改变行为的整理，并顺手修掉两处由此暴露的问题。

1. **豁免包裹闭包去重**（`e75076340`）。四个豁免限流器各自重复了同一段闭包，判断"哪些限流器
   豁免管理员"必须同时读四个函数。抽出 `exemptableLimiter` 作为唯一入口，四个调用点收敛为
   `return exemptableLimiter(limiter)`。`CriticalRateLimit` 刻意不包裹，因此登录、注册、
   找回密码、支付入口的 IP 级保护不受影响。行为不变：既有 10 个豁免用例（含禁用账号、无
   `status` 两个反向用例）全部通过。
2. **静态资源短路移出包裹层**（`f689e51e4`）。`exemptableLimiter` 会先读 session 再决定是否
   放行，把整个 `GlobalWebRateLimit` 函数体包进去，会让每个静态资源请求都多付一次 session
   读取。静态资源判断因此留在包裹层之外，保持原有的短路顺序。
3. **`userRedisRateLimiter` 删除**（并入 `e75076340`）。其函数体与 `redisRateLimiterWithKey`
   除两行注释外逐字节相同；`userRateLimitFactory` 改为直接调用后者。逐字节比对方式：
   `python3` 取两函数体做 `==` 比较，差异只有那两行注释。
4. **classic 设置页两个保存按钮各自只保存自己的区块**（`b3c615b3e`）。新增第二个
   `Form.Section` 后，两个按钮仍调用同一个 `onSubmit`，diff 覆盖全部字段：点"保存模型速率限制"
   会连带写入管理员豁免开关；模型区块的分组 JSON 非法时，"保存全局限流设置"也会一起报
   `部分保存失败`。改为按区块传入字段清单过滤 diff。

重建镜像后的复测（`commit=b3c615b3ede7c3876e831bf06062202c2fb24eff-dirty`，
built 2026-09-29T19:56:55Z，`health=healthy`）：

| # | 场景 | 请求数 | 结果 |
| --- | --- | --- | --- |
| 13 | 管理员 + 豁免开启，`/api/user/self` | 250 | 250x200 |
| 14 | 匿名，`/api/user/self` | 250 | 180x401 + 70x429 |
| 15 | 管理员，豁免关闭，`/api/user/self` | 250 | 180x200 + 70x429 |
| 16 | 管理员 + 豁免开启，`/api/log/self/search`（限 10） | 30 | 30x200 |
| 17 | 管理员 + 豁免开启，`/api/ratio_config`（Critical，限 20） | 25 | 20x403 + 5x429 |
| 18 | 匿名，真实静态资源 `/assets/index-Dh7qDsjK.js`（GW 限 60） | 100 | 100x200，无 429 |
| 19 | 匿名，`/console`（非静态，GW 限 60） | 100 | 60x200 + 40x429 |

第 17 行是反向不变量复测：CriticalRateLimit 对管理员仍按 20 生效。第 18、19 行是本次
静态资源短路调整的直接实证：同一限流桶下，静态资源 100 次无 429，非静态路径按 60 生效。
第 16 行的搜索限流为每用户分桶（`rateLimit:SR:user:1`），与 IP 桶无关。

代码门禁：`gofmt -l` 无输出；`go vet ./middleware/ ./model/ ./setting/operation_setting/` 无输出；
`go test ./middleware/ ./model/ ./setting/operation_setting/` 全部 ok；
`web/classic` 的 `bun x prettier --check`、`bun run eslint`、`bun run i18n:lint`、
`bun run build` 全部通过（Safari 兼容检查通过）。

提交：`e75076340`、`f689e51e4`、`b3c615b3e`。

## 加固轮（2026-09-30，提交 `604c1e6b6`）

继续排查后修掉一处健壮性缺口，并把文档声明的不变量变成测试。

1. **未注册 session 中间件时 `sessions.Default(c)` 会 panic**。`sessions.Default` 内部是
   `c.MustGet(DefaultKey).(Session)`，key 不存在时直接 panic。`main.go` 只有一个 engine，
   且 `server.Use(sessions.Sessions(...))`（`main.go:215`）在 `router.SetRouter(...)`
   （`main.go:221`）之前执行，所以生产不可达；但 `GlobalAPIRateLimit` 等是导出函数，任何
   忘记挂 session 中间件的窄 engine 都会让请求以 panic 结束而不是照常限流。已改为先检查 key，
   缺失即按"session 不可读"失败关闭。

   探针（`gin.New()` 不挂 session 中间件，豁免开启，`GlobalAPIRateLimit()` 限 2）：

   | 版本 | 结果 |
   | --- | --- |
   | 加固前 | `panic: Key "github.com/gin-contrib/sessions" does not exist` |
   | 加固后 | 第 3 次请求 429，无 panic |

   回归测试 `TestAdminExemptionFailsClosedWithoutSessionMiddleware`；移除守卫后该用例
   `panic: Key ... does not exist` 并使整个包失败（变异测试）。

2. **把"哪些限流器豁免"从注释变成测试**。`TestExemptableLimiterCoversExactlyTheDocumentedLimiters`
   对 8 个限流器逐一在"豁免开启 + 管理员会话"下探测：豁免的 4 个（`GlobalWebRateLimit`、
   `GlobalAPIRateLimit`、`SearchRateLimit`、`SuggestionRateLimit`）必须返回 200，不豁免的
   4 个（`CriticalRateLimit`、`DownloadRateLimit`、`DesktopOAuthPollRateLimit`、
   `UploadRateLimit`）必须返回 429。判定依据是"是否被 `exemptableLimiter` 包裹"这一唯一事实，
   因此新增限流器若忘记决策会被测出。
   变异测试：解开 `GlobalAPIRateLimit` 的包裹 → 该用例在
   `GlobalAPIRateLimit should exempt administrators, got 429` 失败；给 `CriticalRateLimit`
   加上包裹 → 该用例与 `TestCriticalRateLimitNeverExemptsAdmin` 同时失败。

3. **同类缺陷排查（结论：不存在第二处）**。把 `validateOptionValue` 与
   `normalizeOptionValueForInMemoryConfig` 的 `case` 集合做差集，逐个核对运行期消费方：

   | 键 | 消费方 | 空白值是否失效 |
   | --- | --- | --- |
   | `RequestHeaderPolicyDefaultMode` | `parseRuntimeHeaderPolicyMode` | 否，`TrimSpace` 后比较 |
   | `RequestHeaderPolicyAuxiliaryRequestsEnabled` | `getRequestHeaderPolicyAuxiliaryRequestsEnabled` | 否，已归一化为 `FormatBool`，消费方再 `ParseBool(TrimSpace(...))` |
   | `billing_setting.billing_mode` / `billing_expr` / `tool_price_setting.prices` | 各自 JSON 解析 | 否，JSON 解析对空白不敏感 |

   即第 3 条缺陷（接受却静默失效）在本仓库只有管理员豁免那一处，已闭环。

4. **排除一条错误怀疑**。一度怀疑写入 `"  merge  "` 会让 `RequestHeaderPolicyDefaultMode`
   运行期失效，实测隔离环境：接口返回 `success:true`、库内 `  merge`，但消费方 `TrimSpace`
   后正常解析，**不是缺陷**，未改动。探针后已删除该行，恢复库内原本不存在该键的状态。

## 最终镜像复测（2026-09-30）

镜像 `commit=604c1e6b631e6c896b615d8beb3907c7718c000b-dirty`（built 2026-09-29T23:24:15Z，
`health=healthy`），隔离环境复测：

| 场景 | 请求数 | 结果 |
| --- | --- | --- |
| 管理员 + 豁免开，`/api/user/self` | 250 | 250x200 |
| 匿名，`/api/user/self` | 250 | 180x401 + 70x429 |
| 管理员 + 豁免开，`/api/ratio_config`（Critical，限 20） | 25 | 20x403 + 5x429 |

Redis 键名与去重前一致：搜索/联想分别落在 `rateLimit:SR:user:1`、`rateLimit:SG:user:1`
（按用户分桶），Critical 落在 `rateLimit:CT172.18.0.1`（按 IP 分桶），`userRedisRateLimiter`
的删除未改变任何键名。

测试稳定性：`go test ./middleware/ -shuffle=11..20` 全部 ok；限流相关用例
`-shuffle=1..10` 全部 ok。新增的两个用例（豁免集合、缺 session 中间件）未引入跨测试状态泄漏。

## 浏览器端交互验证（2026-09-30）

在 classic 主题（dev 环境 `theme=classic`）的 `/console/setting?tab=ratelimit` 上，用页面内
XHR 拦截记录实际发出的 `PUT /api/option/`，验证两个保存按钮的作用域修复：

| 操作 | 发出的 PUT | 提示 | 库内全局开关 |
| --- | --- | --- | --- |
| 只切全局开关，点"保存模型速率限制" | 无（0 条） | `你似乎并没有修改什么` | 不变（`true`） |
| 只切模型开关，点"保存全局限流设置" | 无（0 条） | `你似乎并没有修改什么` | 不变（`true`） |
| 只切全局开关，点"保存全局限流设置" | 1 条，`GlobalRateLimitAdminExemptEnabled` | `保存成功` | 按开关变更 |
| 连续 3 轮"切全局开关 + 保存全局限流设置" | 每轮恰好 1 条 | `保存成功` | 逐轮交替 |

第一、二行是本次修复的直接实证：修复前两个按钮 diff 全部字段，同样操作会写入
`GlobalRateLimitAdminExemptEnabled`。第三、四行确认修复没有把本区块的保存也一起挡掉。

页面 DOM 顺序为"模型开关在前、全局开关在后"（`启用用户模型请求速率限制` 为 index 0，
`管理员豁免全局限流` 为 index 1），验证时按 index 定位。

过程中一次误读已排除：同一页面会话里多次调用 `page.evaluate` 安装 XHR 拦截器会逐层包裹
`XMLHttpRequest.prototype`，使同一次请求被重复记录（曾观察到 3 条相同 PUT）。改为单次安装后，
每轮保存恒为 1 条。这是插桩方式的问题，不是应用行为。

## 复核中排除的候选（2026-09-30）

以下三处排查后判定**不是缺陷**，均未改动：

1. **classic `zh.json` 缺少本次新增的三个中文串**。该文件有 2814 条、缺失率 45.2%，
   但 `src/i18n/i18n.js` 的 `resources` 只 import 7 个语言（en、zh-CN、zh-TW、fr、ru、ja、vi），
   `language.js` 又把 `zh` 归一化到 `zh-CN`，全仓库无任何文件引用 `zh.json`——它是未接入的孤立
   文件，其 2311 个缺口是既有状态。不为其补键，避免让一个死文件看起来是被维护的。
2. **classic `en.json` 缺 `已清理 {{count}} 个日志文件，释放 {{size}}_other`**。该键在 HEAD 中存在，
   当前缺失来自另一会话未提交的 `en.json` 改动（146 增 13 删），本任务未触碰该文件。
3. **写入 `"  merge  "` 到 `RequestHeaderPolicyDefaultMode`**。消费方 `parseRuntimeHeaderPolicyMode`
   会 `TrimSpace` 后比较，运行期正常；已实测确认并删除探针写入的行。

## 第四轮：顺序依赖与启动降级路径（2026-09-30）

给豁免用例加快照恢复后跑 `-shuffle`，在 seed=41 触发了一个与限流无关的 panic，追下去发现两处问题。

1. **`i18n.Translate` 在 `Init()` 之前会 nil 解引用 panic**（提交 `83a134885`）。
   `main.go:380` 的 `i18n.Init()` 失败时只记日志继续（注释写着 `i18n is not critical`），
   但 `GetLocalizer` 会用 nil 的 `bundle` 调 `i18n.NewLocalizer`，产出的 localizer 每次查表都
   解引用 nil，`Translate` 里"找不到就返回 key"的兜底来不及生效。结果是启动期 i18n 加载失败后，
   每个 `i18n.T` 调用点（`middleware/distributor.go` 有 20 余处）在构造第一个错误响应时 panic。
   已改为 `GetLocalizer` 在无 bundle 时返回 nil、`Translate` 回退为 key。

   探针（`Translate` 在 `Init` 之前调用）：

   | 版本 | 结果 |
   | --- | --- |
   | 修复前 | `panic: runtime error: invalid memory address or nil pointer dereference` |
   | 修复后 | 返回 key `"some.missing.key"` |

   回归测试 `TestTranslateFallsBackToKeyWhenBundleIsMissing` 自行构造"无 bundle"状态，
   而不是依赖 `Init` 尚未被调用——`initOnce` 使 `Init` 第二次调用成为空操作，依赖包内执行顺序的
   测试本身就会在 `-shuffle` 下失败。移除 nil 守卫后该测试 panic。

2. **`TestDistributeTokenModelLimitRejectsDeniedSubmitModel` 依赖 i18n 已被初始化**。
   该用例不调用 `i18n.Init()`，靠其他用例先初始化才能通过。这是**既有缺陷**：在干净 HEAD
   worktree（不含本任务任何文件）上扫 seed 1..60，seed 29、33 即触发。修掉第 1 条后 seed 29、33、
   41 全部恢复通过。

3. **豁免用例把 enable 标志恢复成硬编码 `true` 而非原值**（提交 `6a7305749`）。
   把某个限流器关掉的用例会把该状态泄漏给下一个用例；由于 Go 在单进程内跑包内全部用例，
   这只在 `-shuffle` 下显现。已改为在 `withAdminExempt` 内快照全部可变限流设置并在 cleanup 恢复，
   替换掉 11 个逐用例的硬编码 cleanup。

   探针：先记录某标志，跑一个会改写它的豁免用例，再比对——快照恢复时一致；移除 `restoreGlobals()`
   后报 `leak: before=false after=true`。

`-shuffle` 扫描结果：seed 1..40 全部通过（修复前 seed 41 失败）；干净 HEAD 的 seed 29、33 在
i18n 修复后同样通过。

## 未覆盖边界

- 正式环境容器 `new-api` 运行的是 2026-09-08 构建的镜像。该镜像的二进制**认识阈值变量**
  （`GLOBAL_API_RATE_LIMIT`、`GLOBAL_API_RATE_LIMIT_DURATION`、`GLOBAL_WEB_RATE_LIMIT`、
  `GLOBAL_WEB_RATE_LIMIT_DURATION`、各自的 `_ENABLE`，`strings` 均可命中），但**不认识**
  `GLOBAL_RATE_LIMIT_ADMIN_EXEMPT` 与 `GlobalRateLimitAdminExemptEnabled`（计数为 0）。
  因此两件事的部署状态不同：把 600 / 300 写入 `.env` 并重建容器即可生效（无需换镜像），
  豁免开关则必须重新构建镜像。
  实测方式：用同一镜像起一次性容器，注入 `GLOBAL_API_RATE_LIMIT=5` 后请求 12 次
  `/api/status`，得到 4x200 + 8x429，证明旧镜像确实按该变量生效。
- 正式环境的 600 / 300 阈值未在正式环境实测，仅通过 `docker compose config` 确认变量解析为
  `GLOBAL_API_RATE_LIMIT: "600"`、`GLOBAL_WEB_RATE_LIMIT: "300"`。
- 正式容器的 compose 标签引用了 `/tmp/new-api-prod-lite-override.yml`，该文件已不存在
  （`/tmp` 被清理）。`docker compose` 只要带上这个 `-f` 参数就会直接报错退出，因此对正式环境
  执行任何 compose 操作前必须先决定如何处理这个覆盖文件。仅用 `docker-compose.yml` 可以正常
  解析，且解析出的 image 与当前运行容器一致。该问题与本任务无关，为既有状态，此处仅记录。
- 分桶塌缩问题本身未解决：只要反代不可信且 `TRUSTED_PROXIES` 未设置，所有客户端仍共享
  一个 bucket。管理员豁免只保护管理员自身，普通用户之间的相互挤占依旧存在。
- 三库兼容：本改动不涉及数据库迁移与方言 SQL，PostgreSQL 上的选项读写已在隔离环境实测；
  MySQL / SQLite 未单独验证。
- `[sessions] ERROR!` 日志噪声：豁免开启时，携带畸形 session cookie 的请求会多打一行该库日志
  （实测 10 次畸形 Cookie 新增 10 行；无 Cookie 或合法 Cookie 为 0 行）。该行为不是本次改动引入：
  豁免关闭（等同改动前）时 `/api/pricing` 同样每请求一行，`/api/status` 在豁免关闭时为 0 行、
  开启后为 10 行，差异只是豁免使全局中间件也开始读取 session。属 `gin-contrib/sessions` 的库行为，
  本任务不修改第三方依赖。
- `go test ./middleware/` 期间曾出现一个失败用例
  `TestDistributeSpecificChannelRejectsDisabledResponsesCompactMode`（期望 503，实得 404）。
  该失败与本任务无关：在 HEAD 的干净 worktree 上通过，仅把另一会话当时未提交的
  `middleware/distributor.go` 复制进去即复现。该会话随后于 02:13 更新了该文件，用例恢复通过；
  最终一轮 `go test ./middleware/` 在 seed 11..20 的 `-shuffle` 下全部通过，无失败用例。
