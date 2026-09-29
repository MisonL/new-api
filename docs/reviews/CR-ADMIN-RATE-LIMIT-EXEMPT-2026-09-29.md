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

提交：`99f3169c8 feat(rate-limit): let administrators bypass the global rate limits`

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

上表是首轮在隔离环境取得的实测结果。复核阶段补出的 `status` 校验不改变表中任何一行：
该表所有管理员探针使用的会话都对应启用状态账号。禁用账号的行为由 `middleware` 单测覆盖
（见下节），未在隔离环境重复实测。

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

`go test ./middleware/` 现含 10 个豁免用例（新增被禁用账号与无 status 会话两个反向用例）。
新增的 10 个 `middleware` 用例已用变异测试验证其真实性：把 `adminRateLimitExempt` 强制
返回 false 后全部失败，恢复后全部通过。因此它们约束的是真实实现，不是同义反复。

界面侧确认：`/console/setting?tab=ratelimit` 下 `管理员豁免全局限流` 开关渲染为
`checked: true`，与库内值一致。

## 复核阶段发现并修复的两个问题

首轮提交后做了一次全面复核，发现并修复了两处自身缺陷：

1. **被禁用的管理员仍然豁免**。`adminRateLimitExempt` 只读 session 的 `role`，未读 `status`。该中间件
   是全局中间件，运行在 `UserAuth` 之前，而 `UserAuth` 才会从库里重新读取用户并拒绝已停用账号，
   因此在被拒绝之前，一个属于"已停用管理员"的 session 仍会跳过全局限流。已补 `status` 校验。
   实证：`role=admin` + `status=disabled` 的会话此前返回 200，现在返回 429；无 `status` 的会话
   同样按失败关闭处理。移除该校验会让两个新增用例失败（变异测试）。
2. **新增的前端文案未登记到 i18n**。`web/default` 的两个英文串未写入 base locale `en.json`，
   导致项目脚本 `bun run i18n:sync` 直接 `exit 1`（`Found 2 source translation key(s) missing`）；
   `web/classic` 的四个中文串未登记，全部语言都回退到中文原文，而该区块的兄弟键在 classic 的
   7 个语言里均有译文。两处均已按各前端既有惯例补齐，并按文件原有的排序位置插入。

补充验证：`web/default` 的 `i18n:sync` 由失败转为 `exit 0`（928 files / 4674 keys / 0 missing）；
`web/classic` 的 `i18n:sync` 报 `All locales are already in sync`，缺失键计数由 28 降为 24，
其余 24 个属既有缺口。两处修复均只暂存本任务新增的行，未触碰同一文件里其他会话的未提交改动。

## 未覆盖边界

- 正式环境容器 `new-api` 运行的是 2026-09-08 构建的镜像，其二进制不含
  `GLOBAL_RATE_LIMIT_ADMIN_EXEMPT` 与 `GlobalRateLimitAdminExemptEnabled`（`strings`
  计数为 0）。本次改动尚未部署到正式环境；需要重新构建镜像并重建容器后，`.env` 中的
  600 / 300 与豁免开关才会生效。
- 正式环境的 600 / 300 阈值本身未在正式环境实测，仅通过 `docker compose config` 确认
  变量解析为 `GLOBAL_API_RATE_LIMIT: "600"`、`GLOBAL_WEB_RATE_LIMIT: "300"`。
- 分桶塌缩问题本身未解决：只要反代不可信且 `TRUSTED_PROXIES` 未设置，所有客户端仍共享
  一个 bucket。管理员豁免只保护管理员自身，普通用户之间的相互挤占依旧存在。
- 三库兼容：本改动不涉及数据库迁移与方言 SQL，PostgreSQL 上的选项读写已在隔离环境实测；
  MySQL / SQLite 未单独验证。
