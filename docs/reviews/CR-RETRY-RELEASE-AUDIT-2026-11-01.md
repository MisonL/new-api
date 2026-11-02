# CR-RETRY-RELEASE-AUDIT-2026-11-01

对 2026-10-31 至 2026-11-01 重试链路改造批次的全维度审查，同时覆盖最新源码与
隔离开发环境运行态。审查由五个并行只读 agent 分头执行（重试链路端到端、启动迁移
与三库兼容、变更影响面与遗留引用、开发服务运行态、前端双端一致性），每条结论由
我方独立复核后才收入本文件。

采样时间：2026-11-01（本机 macOS，dev 分支）。
源码基线：`git rev-parse HEAD` = `50dce919113fbb0c3eb1e532673e6f02e502fc10`。
本批次提交：`66e68350f`、`59ff7e9d9`、`baaf3b32c`、`d76fa9e0b`、`9906fceb2`、`50dce9191`。

本轮未修改仓库代码，未触碰生产容器（`new-api`、`postgres`、`redis` 仅只读查询），
未回退其他会话的在途改动，未推送。

## 1. 结论摘要

- 重试链路本身：未发现 P0/P1。闸门顺序、状态码映射、渠道亲和 bypass 与渠道健康
  分类、skip-retry 生产点四处均已逐行复核，无逻辑空洞。
- 启动迁移：`quota_data` 的启动顺序缺陷已在本批次修复（`50dce9191`）并三库实测。
  审查另发现 `Model` / `Vendor` 同型隐患（见第 3 节 F1），已由 `afd76c02e` 移出
  大 AutoMigrate 列表并补 MySQL 回归用例。
- 生产环境升级路径：生产二进制 `4527d7666`（构建于 2026-09-08）尚未包含本批次
  全部改动；F1 的阻断路径已实测，MySQL 部署升级前需带 `afd76c02e`。
- 隔离开发环境：容器健康、迁移已落库；审查当日 `RetryTimes = 0` 使重试链路空转
  （第 3 节 F3），已改回 3，2026-11-02 复核为 `RetryTimes = 3`。

## 2. 已核查且未发现问题的项

### 2.1 重试决策链（controller/relay.go）

`shouldRetry`（`controller/relay.go:2060`-`2138`）在删除 `IsAlwaysSkipRetryCode`
之后无逻辑空洞：

- `isUnsafeReadResponseBodyRetry`（`:2141`）只拦 `ErrorCodeReadResponseBodyFailed`，
  `bad_response_body` 由通用闸门 `c.Writer.Written() && !ResponsesStreamPreludeRetrySafe`
  （`:2080`）兜底。非流式解析失败点（`relay/channel/xai/text.go:87-102`、
  `relay/channel/baidu/relay-baidu.go:162-204`、`relay/common_handler/rerank.go:36,71`）
  都发生在任何 `c.Writer` 写入之前，重放安全。
- 限流观测读 `OriginalStatusCode`（`:2159-2161`），状态码映射不会丢掉 429 的限流语义。
- `RequestRetry = "exclude"` 全库仅两处（`service/channel_health.go:1201` 远程 compact
  首包超时、`:1226` 凭据不可用）。`gateway_timeout`（`:1214`）与传输中断保持默认
  `retry`，与 `shouldRetryDespiteChannelAffinity`（`:2264`-`:2274`）的六个 bypass
  条件一一对应，无正反不一致。
- `ErrOptionWithSkipRetry` 的 channel 级生产点（`middleware/distributor.go:822,865`）
  落在 `IsChannelError`（`:2108`）之后、`IsSkipRetryError`（`:2111`）之前，语义正确：
  这类本地错误换渠道即可解决，不应因 skip 标记终止整个请求。
- `shouldRetryTaskRelay`（`:4043`-`:4091`）非 2xx 全放行后，`NoRefund` 与 `LocalError`
  两道守卫仍拦在 4xx 之前，调用方只有 `:3778` 一处，无遗漏。

验证：`go test ./controller/ -run 'Retry' -count=1` 通过；`go test ./service/ -run
'Channel|Health' -count=1` 通过。

### 2.2 出厂值一致性

后端 `setting/operation_setting/status_code_ranges.go:39-43` 出厂规则为
`{100-199},{300-599}`，规范串 `100-199,300-599`。前端四处硬编码与之全部一致：
`web/default/src/features/system-settings/integrations/index.tsx:25`、
`web/classic/src/pages/Setting/Operation/SettingsMonitoring.jsx:40`、
`web/classic/src/components/settings/OperationSetting.jsx:88`，以及
`web/default/.../monitoring-settings-section.tsx` 的表单链路。
`deploy/`、`desktop/` 下无 `AutomaticRetryStatusCodes` 出现。

### 2.3 遗留引用与 i18n

- `IsAlwaysSkipRetryCode` / `alwaysSkipRetryCodes`：全仓库仅剩一处历史文档引用
  （`docs/reviews/ADDITIONAL-AUDIT-2026-10-27.md:87`，记录当时代码，无需改动）。
- 过时文案（"504/524 默认不重试"类）：`README`、`AGENTS.md`、`docs/operations`、
  `docs/architecture`、`deploy/`、`desktop/`、`scripts/` 零命中。
- classic 7 语言 JSON 语法有效；`高危状态码重试风险告知与免责声明Markdown` 在
  7 个语言语义一致，无语言残留旧说法；被删孤儿 key
  `支持填写单个状态码或范围（含首尾），使用逗号分隔；504 和 524 始终不重试，不受此处配置影响`
  在 7 个语言与全 `src` 下零引用。

### 2.4 隔离开发环境运行态

- 三容器 `running/healthy`，`RestartCount=0`；app 容器内 `nc -zv postgres 5432`
  与 `redis 6379` 均通，`psql SELECT 1` 返回 1，`redis-cli PING` 返回 PONG。
- 当前启动段无 error/panic/fail；`16:20:06` 那次 `uk_qdt_bucket` 23505 失败早于
  `50dce9191` 镜像，已修复。
- 迁移落库：`migration.automatic_retry_status_codes.20261101 = done`；
  `AutomaticRetryStatusCodes` 行不存在（运行时走代码出厂值，预期行为）；
  `quota_data` 3181 行、重复桶 0、`uk_qdt_bucket` 存在。
- HTTP 冒烟：`GET /api/status` 200，`GET /` 200，`/api/uptime/status` 200。
- compose 标签（`project`/`service`/`working_dir`/`config_files`）三容器齐全；
  挂载分别指向 `.dev-docker/runtime/isolated/{data,logs,postgres}`；端口仅
  3001/5433/6381 且均绑 `127.0.0.1`。

## 3. 发现与处置

本节记录审查当日列出的 F1-F7。前七条在当日审查后由 `afd76c02e` 修复，F3 属数据面，
已在隔离环境改回；逐条状态见各小节标题后的「已修复」标注。

### F1（P1，非本批次引入，已修复）Model / Vendor 曾在启动大 AutoMigrate 列表中

- 位置：`model/main.go:462-463`（`&Model{}`、`&Vendor{}`）与 `:487`、`:490`
  （专用迁移）。
- 机制：`Model` 的 `uk_model_name`（`model/model_meta.go:26`）与 `Vendor` 的
  `uk_vendor_name`（`model/vendor_meta.go:17`）带 `where:deleted_at IS NULL`。
  大 AutoMigrate 先于专用迁移执行，gorm 按标签直接建索引：
  - MySQL 驱动没有 `BuildIndexOptions`，`where` 被静默丢弃，建出的是
    `model_name` 上的普通唯一索引。只要存在"软删除 + 同名活动行"，建索引即
    `Error 1062`，`runDBMigration` 返回错误、进程 `FatalLog` 退出。
  - PostgreSQL / SQLite 会建出真正的部分唯一索引；只要存在两行同名活动行，
    同样 `SQLSTATE 23505` 退出。
- 我方实测（一次性 MySQL 8.4 库，忠实复现 82dcfcf93 之前的复合唯一索引结构）：
  `db.AutoMigrate(&model.Model{})` → `Error 1062 (23000): Duplicate entry
  'dup-name' for key 'models.uk_model_name'`；`Vendor` 同型 →
  `'dup-vendor' for key 'vendors.uk_vendor_name'`。
  一次性 PG 15 schema（复合索引 + 一行软删一行活动）→ 通过；改为两行活动同名 →
  `ERROR: could not create unique index "uk_model_name" (SQLSTATE 23505)`。
- 影响面：生产为 PostgreSQL 15，实测该升级路径不阻断（活动重名 0 行；24 组
  "软删除 + 同名活动行"在部分索引下只算一行）。因此生产升级安全。风险落在
  MySQL 部署：任何有"删除后重建同名模型/供应商"历史的实例，升级到含
  `82dcfcf93` 的版本会在启动时直接退出。
- 修法与 `50dce9191` 同型：把 `Model`、`Vendor` 移出大 AutoMigrate 列表，由
  `migrateModelMetaUniqueIndex` / `migrateVendorMetaUniqueIndex` 独占（这两个
  迁移已有 `!HasTable` → AutoMigrate 分支，具备独占条件）。
- 注：`PrefillGroup` 不在大列表（由 `migratePrefillGroupTable` 自建），
  `Channel`/`Token`/`User`/`Ability`/`Log`/`PerfMetric`/`Checkin`/
  `UserOAuthBinding` 等其余带唯一索引的模型，其索引随建表或列添加一并引入，
  没有"先建索引、后处理数据"的重叠，不属同型隐患。

### F2（P2，本批次引入，已修复）第三个 legacy 串无出厂出处

- 位置：`model/main.go:574`（`100-199,300-503,505-523,525-599`）与
  `model/retry_status_codes_migration_test.go:41`。
- 机制：该串在本仓库全部 ref、全部 commit 对象、stash 以及 upstream 中都不存在，
  仅由 `9906fceb2` 自身引入。把它列入迁移匹配集，意味着某个管理员恰好自定义成
  这一串时会被静默改写。
- 处置建议：从匹配集删除该条（只保留有两处出厂出处的两条：
  `9a7fa9d13` 的 `100-199,300-399,401-407,...` 与 `a5deb1bcf` 起的
  `100-199,300-399,400-407,...`）。风险概率低，但匹配一条无出处的字符串没有依据。

### F3（P2，数据面，已处置）隔离开发环境 `RetryTimes = 0`

- 位置：dev 库 `options.RetryTimes = 0`（`docker exec ... psql` 只读查询）。
- 机制：`updateOptionMap`（`model/option.go:550-551`）用库值覆盖代码默认 3 下的
  `common.RetryTimes`；`shouldRetry` 在 `retryTimes <= 0` 直接返回 false
  （`controller/relay.go:2114`）。该实例实际不会有任何渠道 failover，
  与"自动重试"目标相反。
- 生产不受影响：生产 `RetryTimes = 3`（只读查询）。
- 处置：把该选项改回 >= 1（设置页保存，或 `PUT /api/option/`）。本轮为只读审查，
  未改动。

### F4（P2，本批次引入，已加固）迁移标记检查在事务之外

- 位置：`model/main.go:581-586`（先读标记）与 `:588`（事务内写标记）。
- 机制：两个 master 同时启动时会双双通过检查，后者 `Save` 主键冲突导致
  `runDBMigration` 报错退出。与仓库既有迁移（`migrateChannelPinOrder`、
  `migrateResponsesCompactModeAuto`）为同一模式，属既有惯例而非新问题类别。
- 处置建议：可改为在事务内 `First` 再写，或对主键冲突做幂等忽略。

### F5（P2，非本批次引入，已修复）`migrateQuotaDataUniqueIndexWithExecutor` 参数与执行器不一致

- 位置：`model/quota_data_migration.go:29`（`dialect` 形参未被使用）、`:43`
  （用全局 `DB` 而非 `executor`）。
- 机制：生产路径传 `DB`，行为正确；但测试替身路径会查到错误的库。属可读性/
  可测性缺陷，无生产影响。

### F6（P2，非本批次引入，已修复）default 前端风险弹窗未与 classic 同步

- 位置：`web/default/src/features/channels/components/dialogs/status-code-risk-dialog.tsx`
  对 `web/classic/src/components/table/channels/modals/StatusCodeRiskGuardModal.jsx`。
- 差异：classic 用长 Markdown 风险告知 + 4 条强确认项 + 禁止粘贴；default 只有
  一句短 disclaimer + 4 条通用勾选。两侧机制（`NON_REDIRECTABLE = {504,524}`
  守卫渠道状态码重定向）功能等价，仅确认强度不同。非事实性错误。

### F7（P2，非本批次引入，已修复）classic `i18n:lint` 门为红

- 命令：`cd web/classic && bun run i18n:lint`，审查当日退出码 1。
  输出：`i18n lint 当前共 62 条，基线 307 条，新增 6 条，已解决 251 条`。
- 现状（2026-11-02 复跑）：基线已由 `afd76c02e` 重置为 62 条，退出码 0。
- 新增 6 条均为硬编码字符串：`CardTable.jsx:643`、`NotificationSettings.jsx:690,693`、
  `EditTagModal.jsx:809`、`TokensColumnDefs.jsx:154,183`。
- 与本次改动无关：这 4 个文件的最近提交分别为 `8427a98ae`、`7497b8e4c`、
  `e39e0e64f`、`e8020a10f`/`a42b39760`，均不在本批次内。

## 4. 验证命令与结果

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go build ./...` | 0 | 通过 |
| `go vet ./controller/ ./model/ ./setting/operation_setting/` | 0 | 通过 |
| `go test -count=1 ./...` | 0 | 68 个包全部 ok |
| `go test -race -count=1 ./setting/operation_setting/` | 0 | 通过 |
| `go test ./model/ -run 'Migrate\|QuotaData' -count=1` | 0 | 通过 |
| `NEW_API_TEST_MYSQL_DSN=... go test ./model/ -run TestMigrateRetryStatusCodes -count=1` | 0 | 通过（一次性库，跑完删除） |
| `NEW_API_POSTGRES_TEST_DSN=... go test ./model/ -run TestMigrateRetryStatusCodes -count=1` | 0 | 通过（临时 schema，跑完删除） |
| `cd web/classic && bun run lint` | 0 | `All matched files use Prettier code style!` |
| `cd web/classic && bun run build` | 0 | 通过 |
| `cd web/default && bun run lint` | 0 | 0 errors / 22 warnings（与本批次无关） |
| `cd web/default && bun run typecheck` | 0 | 通过（`tsc -b`，约 81s） |
| `cd web/classic && bun run i18n:lint` | 1（当日）/ 0（2026-11-02 复跑） | 当日 6 条新增硬编码；`afd76c02e` 重置基线后转绿，见 F7 |

反向验证（本轮复核）：

- 把 `&QuotaData{}` 放回大 AutoMigrate 列表 →
  `TestMigrateDBMergesQuotaDataDuplicatesBeforeUniqueIndex` 以
  `UNIQUE constraint failed: quota_data...` 失败；移出后通过。
- 在 `migrateRetryStatusCodesLegacyDefault` 中把 `KeyColumn()` 换成未加引号的
  `"key"` → MySQL 上 `Error 1064 (42000) ... near 'key = ? LIMIT ?'`。
- 去掉迁移中的 `Update` 语句 → `TestMigrateRetryStatusCodesUpgradesStoredLegacyDefaults`
  以"值未升级"失败。
- 把 `bad_response_body` 的重试硬挡加回 `shouldRetry` →
  `TestShouldRetryBadResponseBodyBeforeAnyDownstreamWrite` 失败；移除后通过。

## 5. 边界说明

- 未在真实 PostgreSQL/MySQL 上验证 F1 的**生产数据形态**（生产为 PG，仅做了
  只读统计与一次性 schema 复现）；MySQL 的结论来自一次性库复现，非生产实例。
- 未在真实浏览器中打开两个前端的人工动作验证；前端结论来自静态阅读与
  lint/typecheck/build。
- `web/default` 的 `typecheck` 在 agent 阶段未执行，由本文作者补跑（退出码 0）。
- 生产容器始终只读：本轮对生产库仅执行 SELECT，未重启、未改配置。
- 隔离开发环境的一次性测试库（MySQL `newapi_test_*`）与临时 schema 已全部删除；
  `na-audit-mysql2` 容器已 `stop` 回原状态；构建过程产生的临时目录已清理。

## 6. 复审（2026-11-02）

逐条对照代码与实跑结果复核 F1-F7 与验证表，结论如下。

| 项 | 复核方法 | 结果 |
| --- | --- | --- |
| F1 | `awk` 提取 `autoMigrateModels` 列表 | `&Model{}`、`&Vendor{}`、`&QuotaData{}`、`&PrefillGroup{}` 均不在列表内，由专用迁移独占 |
| F2 | `grep retryStatusCodesLegacyDefaults` | 仅剩两条有出厂出处的串，第三个串已删除 |
| F3 | 隔离库 `SELECT key,value FROM options` | `RetryTimes = 3` |
| F4 | 读 `migrateRetryStatusCodesLegacyDefault` | 标记写入已改为 `ON CONFLICT DO NOTHING`，重复执行不报错 |
| F5 | 读 `quota_data_migration.go` | 形参改为 `executor`，扫描与合并都走 `executor.DB()` |
| F6 | 读 `status-code-risk-dialog.tsx` | 已渲染 Markdown 风险告知 |
| F7 | `cd web/classic && bun run i18n:lint` | 退出码 0，`当前共 62 条，基线 62 条，新增 0 条` |

复跑命令与结果：

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go test ./controller ./model ./relay/common ./relay/helper ./service` | 0 | 五个包全部 ok |
| `gofmt -l model/` | 0 | 无输出 |

本轮另发现 `afd76c02e` 遗留两处格式问题（`model/main.go` 数组末尾多余空行、
`model/meta_unique_mysql_f1_test.go` 缺少文件末尾换行），已由 `15a314da0`
修正，无行为变化。

边界：生产容器本轮仍只读，未重启、未改配置；上述结论只覆盖源码与隔离环境，
不表示生产已运行这些修复。生产二进制 `4527d7666` 构建于 2026-09-08，落后于
本地 `dev` 676 个提交。
