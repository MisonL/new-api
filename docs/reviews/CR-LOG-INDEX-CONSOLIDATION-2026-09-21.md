# CR: logs 表索引精简（源码层性能优化）

- 日期：2026-09-21
- 范围：`model/log.go` 索引声明、新增 `model/log_index_cleanup_migration.go` 一次性迁移、`model/main.go` 接入
- 状态：已实现并在隔离环境端到端验证；正式环境未部署

## 问题

`logs` 表有 41 个索引、8030 MB，堆仅 5466 MB（13 GB 总计）。这些索引来自
`7c2b079d8 perf: optimize log and dashboard loading`：它给每个筛选维度两两组合都建了索引，
再加上 `Type` 字段上 8 个组合索引，共 24 个组合索引。

实测这些组合索引没有任何查询能走到。隔离库对全部 23 个真实查询模式跑 `EXPLAIN`，
组合索引一个都没被选中——管理员日志筛选每个维度只传一个值，因此
`(username, token_name, type, created_at, id)` 这种组合索引的前缀部分
`(username, ...)` 已经被 `(username, created_at)` 覆盖，而多值组合场景不存在。

代价落在写入侧。生产 `docker logs` 窗口（2026-09-20 15:43 至 2026-09-21 11:45，约 20 小时）
共 2364 条 `SLOW SQL >= 200ms`，其中 `INSERT INTO "logs"` 占 1178 条；>1s 的 319 条里，
最慢的 5 条全是 `logs` 的 INSERT（30841 / 28133 / 27876 / 27080 / 26255 ms）。
`RecordConsumeLog` 走 `LOG_DB.Create(log)` 同步写入，该延迟落在请求收尾路径上。

## 索引集合的确定方式

不是凭直觉删，而是穷举 `git grep` 出全部 `logs` 查询构造点，再对每条真实查询跑 `EXPLAIN`：

| 查询来源 | 使用的索引 |
| --- | --- |
| `GetAllLogs` 无过滤 | `idx_logs_created_at_id` |
| `GetAllLogs` type+时间 | `idx_logs_type_created_at` |
| `GetAllLogs` 各维度+type+时间 | `idx_logs_<dim>_type_created_at` |
| `GetAllLogs` 维度+时间（无 type） | `idx_logs_<dim>_created_at` |
| `GetLogByTokenId` | `idx_logs_token_id` |
| 看板聚合（username / user_id / 全站） | `idx_logs_username_type_created_at` / `idx_logs_user_type_created_at` / `idx_logs_type_created_at` |
| `sumLogTokenStats` keyset | `logs_pkey` |
| `sumUsedQuota` | `idx_logs_type_created_at` |
| `SumUsedToken` | `idx_logs_username_created_at` |
| 日志联想（`recentSuggestionBase`） | `idx_created_at_type`、`idx_logs_request_id_created_at` |
| 保留策略删除 | `idx_logs_created_at_id` |

结果：41 → 19（+ `logs_pkey` 主键 = DB 中 20 个）。删除的 21 个中，
单列索引（`idx_logs_channel_id`、`idx_logs_group`、`idx_logs_ip`、`idx_logs_model_name`、
`idx_logs_request_id`、`idx_logs_token_name`、`idx_logs_upstream_request_id`、
`idx_logs_user_id`、`idx_logs_username`）全部被对应复合索引的前缀覆盖；
`idx_logs_ip` 是唯一例外——`ip` 列只在日志详情展示，没有任何过滤入口，它的索引服务不了任何查询。

`logs_pkey` 显式排除：它是主键约束索引，`DROP INDEX` 会失败。测试
`TestLegacyLogIndexesExcludePrimaryKey` 守住这一点。

## 实现要点

### 为什么必须改源码而不能只 DROP

`model/main.go:443` 在启动时执行 `LOG_DB.AutoMigrate(&Log{})`。读 gorm v1.25.10 的
`migrator.go` 确认：`AutoMigrate` 对索引只做 `HasIndex` + `CreateIndex`，**从不调用
`DeleteIndex`**。所以从 struct tag 移除索引后它不会被重建；反过来，只 `DROP INDEX`
而不改 tag，下次启动就会被重建。两者必须同时做。

### 一次性迁移

沿用仓库既有的 `migrateChannelPinOrder` 约定（`options` 表 marker，仓库没有版本化迁移表），
新增 `model/log_index_cleanup_migration.go`：

- `legacyLogIndexes` 列出 21 个待删索引。
- 标记键 `migration.log_index_cleanup.20260921` 存在则直接返回，保证幂等。
- 任一次 drop 失败立即返回且**不写标记**，下次启动重试。
- marker 读写走 `DB`，索引删除走 `LOG_DB`：`LOG_SQL_DSN` 非空时两者是不同数据库，
  而 `options` 表只存在于主库。

### 接入位置（此处有一个真实缺陷）

首次接入写在 `migrateLOGDB` 内，但 `migrateLOGDB` 只在 `LOG_SQL_DSN` 非空时被调用
（`InitLogDB` 的 `LOG_SQL_DSN == ""` 分支直接 `return`）。正式与隔离环境都没有设置
`LOG_SQL_DSN`，因此迁移**一次都没执行**——隔离容器重建后日志中只有
`database migration started: main`，没有 `log index cleanup finished`，索引数仍是 41。

改为在 `InitLogDB` 中调用（此时 `LOG_DB` 已赋值），并保留 `migrateLOGDB` 内的一处
以覆盖 `LOG_SQL_DSN` 非空部署。`LOG_DB` 在 `migrateDB` 执行时仍是 `nil`，
所以不能在 `migrateDB` 里调用——这是第一次尝试的位置错误。

## 验证

### 单元测试（`model/log_index_cleanup_migration_test.go`）

沿用仓库 `fake executor` 约定，9 个用例全部通过：

```bash
go test ./model/ -run "TestLogIndexCleanup|TestLegacyLogIndexes" -count=1
# ok github.com/QuantumNous/new-api/model
```

覆盖：全部索引被删一次、marker 存在时幂等、缺失索引跳过、`logs` 表不存在时跳过、
drop 失败即中止且不写标记、marker 读失败、marker 写失败、
`logs_pkey` 不在列表、以及
`TestLegacyLogIndexesAreNotDeclaredOnModel`——该用例断言列表里每个索引都已从 struct 移除，
否则 AutoMigrate 会重建、迁移被撤销。

**变异验证**（改坏实现，确认测试能抓到）：

| 变异 | 结果 |
| --- | --- |
| marker 存在时仍执行删除 | `TestLogIndexCleanupIsIdempotentWhenMarkerPresent` FAIL |
| drop 失败仍继续写 marker | `TestLogIndexCleanupStopsOnDropError` FAIL |
| 把 `logs_pkey` 加入列表 | `TestLegacyLogIndexesExcludePrimaryKey` FAIL |
| 不再写 marker | `DropsEveryLegacyIndexOnce` + `FailsWhenMarkerWriteFails` FAIL |

### 隔离环境端到端

镜像 `new-api-local:dev`，`commit=5cda680770543782d5d0a757b31f4e783443dc03-dirty`
与 `git HEAD` 一致。重建容器后容器日志：

```text
[SYS] log index cleanup finished: dropped_indexes=21
```

- 迁移前 `logs` 41 个索引、无 marker；迁移后 20 个（19 声明 + `logs_pkey`）、marker 存在。
- DB 索引集合与 GORM 从 struct 解析出的集合逐项一致（`comm` 对比，差异仅为 `logs_pkey`）。
- **幂等**：再次 `--force-recreate`，日志无 `cleanup finished`，索引数仍为 20。

### 查询性能（隔离库，41 索引 vs 20 索引，同一份数据）

对全部 23 个真实查询模式跑 `EXPLAIN (ANALYZE)`：

| | 规划时间合计 | 执行时间合计 |
| --- | --- | --- |
| 41 索引 | 66 ms | 80 ms |
| 20 索引 | 17 ms | 22 ms |

逐条对比无任何一项变慢。最常用的「日志列表无过滤」（`A1`）规划时间 48.67 ms → 11.49 ms，
执行 0.131 ms → 0.123 ms。日志联想三项执行时间 19.0/36.5/19.5 ms → 7.0/5.9/5.3 ms。
全部 23 项仍然走索引，**无一项退化为顺序扫描**。

## 未覆盖边界

- **未在生产执行**。正式环境 `logs` 仍为 41 个索引，容器未重启（`StartedAt=2026-09-18T08:45:10Z`，
  `RestartCount=2`），13000 端口 HTTP 200。部署后首次启动会执行 21 次 `DROP INDEX`，
  期间持有表锁；`logs` 3.3M 行、索引合计 8 GB，实际耗时未在生产验证。
- **索引可回收量的实测来自隔离库 411,644 行**，与生产 3,318,548 行的数据分布不同。
  查询性能对比在同一份数据上做，结论可靠；但「省下多少 MB」未在生产量级验证。
- **写入侧收益未直接测量**。生产 INSERT 慢查询是观测证据，本次未在生产复测
  插入延迟；隔离库的受控基准（41 索引 vs 8 索引，2000 行交错）曾显示 13.2x / 4.2x / 13.8x，
  但该组数据受宿主负载波动影响，只作参考。
- **多值组合筛选未覆盖**。前端每个维度只传一个值，因此未为多值场景保留组合索引；
  若未来支持多选筛选，需要重新评估。
- 未做图片、视频、任务类渠道的性能排查。
- 生产构建期间 `proxy.golang.org` 经 TUN 解析到 `198.18.2.188`（fake-IP），
  Docker VM 无法路由，本次以 `--build-arg GOPROXY=https://goproxy.cn,direct` 绕过；
  仓库 `Dockerfile` 默认值未改。
