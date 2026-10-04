# CR: logs 索引精简的生产侧核实与二期候选裁决

- 日期：2026-09-23
- 范围：正式库 `new-api`（13000 容器所用 PostgreSQL 15.17）的 `logs` 表索引实测；`model/log_index_cleanup_migration.go` 删除清单与生产实际的比对；二期清理候选的可行性裁决
- 状态：核实完成。**一期删除清单与生产精确匹配，判定安全；二期候选为空集，不实施**
- 前置记录：[CR-LOG-INDEX-CONSOLIDATION-2026-09-21.md](CR-LOG-INDEX-CONSOLIDATION-2026-09-21.md)（源码层实现与隔离环境验证）

## 本文档的定位

前一份记录从**源码层**论证了 41 → 19 个索引声明的精简，并在隔离库验证了性能。它把
「未在生产执行」「生产量级未验证」列为未覆盖边界。本文补齐这一段：在**正式库上只读取证**，
核实删除清单是否精确命中、`DROP INDEX` 的真实锁代价、以及是否存在值得做的二期清理。

全程只读：`SELECT` / `EXPLAIN` / `pg_stat_user_indexes`，未执行任何 `UPDATE`/`DELETE`/`DROP`/`CREATE`，
正式容器未重建、未重启。

## 生产基线

```bash
docker exec postgres psql -U root -d new-api -t -c "SELECT count(*) FROM logs;"
docker exec postgres psql -U root -d new-api -t -c "SELECT pg_size_pretty(pg_total_relation_size('logs')), pg_size_pretty(pg_relation_size('logs'));"
docker exec postgres psql -U root -d new-api -t -c "SELECT count(*) FROM pg_indexes WHERE tablename='logs';"
```

```text
行数          2,565,445
总占用        13 GB（堆 5484 MB，索引合计 8053 MB）
索引数        41（含 logs_pkey）
PostgreSQL    15.17 (Debian 15.17-1.pgdg13+1)
```

`LOG_SQL_DSN` 未设置，故 `LOG_DB = DB`，标记库与目标库同库。迁移标记
`migration.log_index_cleanup.20260921` 在 `options` 表中**不存在**，证明一期迁移**尚未在生产执行**。

```bash
docker exec postgres psql -U root -d new-api -t -c "SELECT key, value FROM options WHERE key LIKE 'migration.%' ORDER BY key;"
```

```text
 migration.responses_compact_mode_auto.20260526              | done
 migration.responses_compact_mode_auto_all_channels.20260610 | done
```

## 删除清单与生产实际的精确比对

把 `model/log_index_cleanup_migration.go` 的 21 个名字逐个对生产库求证：

```bash
docker exec postgres psql -U root -d new-api -t -c "SELECT indexname FROM pg_indexes WHERE tablename='logs' ORDER BY indexname;"
```

**21/21 全部存在**，无一名单项落空。反查「生产有但清单没有」得到 20 个，与
`Log` struct 当前的 19 个声明 + `logs_pkey` 精确对应。清单与生产状态**双向匹配**。

### 关键证据：33/41 索引从未被 planner 选中

```bash
docker exec postgres psql -U root -d new-api -t -c "
SELECT indexrelname, idx_scan, pg_size_pretty(pg_relation_size(indexrelid))
FROM pg_stat_user_indexes WHERE relname='logs' ORDER BY idx_scan NULLS LAST;"
```

41 个索引中 **33 个 `idx_scan = 0`**，被删的 21 个全部落在这个零扫描集合内。有扫描记录的仅 8 个：

| 索引 | idx_scan | 体积 |
| --- | --- | --- |
| `logs_pkey` | 834 | 131 MB |
| `idx_logs_created_at_id` | 210 | 182 MB |
| `idx_created_at_type` | 38 | 161 MB |
| `idx_logs_type_created_at` | 14 | 154 MB |
| `idx_logs_username` | 10 | 34 MB |
| `idx_logs_token_name` | 3 | 34 MB |
| `idx_logs_user_type_created_at` | 3 | 193 MB |
| `idx_logs_model_created_at` | 3 | 201 MB |

其余 33 个，包含全部被删的 21 个，均为 0。被删索引中体积最大的是
`idx_logs_username_token_group_type_created_id`（386 MB）与
`idx_logs_model_group_type_created_id`（377 MB）。

**口径提醒**：`idx_scan` 自数据库上次统计重置起算。本次取样时 PostgreSQL 容器已运行
2 小时，**该窗口内**确为 0。这不能外推为「历史从未使用」；但结合下节的
`EXPLAIN` 与数据分布证据，可以确定在当前生产数据形态下这些索引不会被选中。

### 数据分布解释了为什么组合索引不可用

```bash
docker exec postgres psql -U root -d new-api -t -c "
SELECT attname, n_distinct, correlation FROM pg_stats
WHERE tablename='logs' AND attname IN ('username','token_name','type','created_at','model_name','channel_id','group');"
docker exec postgres psql -U root -d new-api -t -c "SELECT count(DISTINCT username) FROM logs;"
```

```text
 username    n_distinct=1     correlation=1.0
 group       n_distinct=1     correlation=1.0
 token_name  n_distinct=5     correlation=0.9965
 type        n_distinct=2     correlation=0.6532
 model_name  n_distinct=25    correlation=0.4806
 channel_id  n_distinct=70    correlation=0.0442
 created_at  correlation=0.1679

 count(DISTINCT username) = 1     （mison 占 2,564,494 / 2,564,477）
```

`username` 与 `group` 各只有 **1 个取值**，`token_name` 只有 5 个。等值条件命中率高到索引无意义，
planner 必然绕开。这从分布层面解释了 `idx_scan = 0`。

### EXPLAIN 实测：组合索引不被选中

对一个典型的管理员多维度筛选（`username` + `token_name` + `type` + 时间区间）：

```bash
docker exec postgres psql -U root -d new-api -t -c "
EXPLAIN (COSTS OFF) SELECT id FROM logs
WHERE username='testuser' AND token_name='t1' AND type=2
  AND created_at >= 1758000000 AND created_at <= 1758600000
ORDER BY created_at DESC, id DESC LIMIT 21;"
```

```text
 Limit
   ->  Sort
         Sort Key: created_at DESC, id DESC
         ->  Index Scan using idx_logs_username on logs
               Index Cond: (username = 'testuser'::text)
               Filter: ((created_at >= ...) AND (created_at <= ...) AND (token_name = 't1') AND (type = 2))
```

即使在**索引尚未删除**的当前生产库上，planner 也选择单列 `idx_logs_username` 而非
组合索引 `idx_logs_username_token_type_created_id`。加 `SET enable_seqscan=off` 后计划不变，
进一步排除「因成本估算偏向顺序扫描」的可能。

这从执行计划层面直接证实：被删的组合索引**在当前生产数据上就是死索引**，删除无性能代价。

## `DROP INDEX` 的锁代价（未在生产验证的风险项）

gorm 的 postgres 驱动生成裸 DDL：

```go
// gorm.io/driver/postgres@v1.6.0/migrator.go:185
return m.DB.Exec("DROP INDEX ?", clause.Column{Name: name}).Error
```

**不带 `CONCURRENTLY`**。在 PostgreSQL 15 上，普通 `DROP INDEX` 对 `logs` 取 `ACCESS EXCLUSIVE` 锁，
会短暂排空该表全部读写。

- 迁移在 `InitLogDB()` 内执行，位于服务开始接受请求之前，理论窗口是启动阶段
- `DROP INDEX` 主要是元数据操作，但生产 `logs` 索引合计 8053 MB，21 次删除的**实际耗时未在生产验证**
- 该迁移**从未在生产执行**，也无同等数据量的演练记录

**这是本文唯一未能消除的风险**：删除清单的正确性已充分证实，但「21 次 `DROP INDEX` 在
256 万行 / 8 GB 索引上的实际锁持有时长」仍是未验证项。建议正式部署前在同等数据量的库上演练一次。

## 二期清理候选的裁决：空集

初步候选是「生产 `idx_scan = 0`，但不在**一期**删除清单里」的索引——即一期之后仍会残留的死索引。
经四轮筛选，候选集为空。

### 第 1 轮：剔除有扫描记录的

`idx_logs_model_created_at` 的 `idx_scan = 3`，不是死索引，剔除。

### 第 2 轮：剔除一期已覆盖的

`idx_logs_upstream_request_id_created_at` 虽符合「零扫描且不在我最初的候选里」，
但它**已在一期清单第 39 行**，一期即会删除，不属于二期。

### 第 3 轮：修正统计方法（本文档的一个自我纠错）

前几轮用 `grep -c "<index_name>"` 统计索引是否仍在 `Log` struct 中声明。这是**子串匹配**：
`grep -c "idx_logs_username"` 会同时命中 `idx_logs_username_created_at` 与
`idx_logs_username_type_created_at`，导致「声明次数」虚高，一度得出「9 个候选全部仍被声明」的错误结论。

改用精确匹配后：

```bash
sed -n "/^type Log struct/,/^}/p" model/log.go | grep -o 'index:[a-z_0-9]*' | sed 's/index://' | sort -u | grep -cx "<index_name>"
```

### 第 4 轮：按 `AutoMigrate` 语义分类

`AutoMigrate` **只创建** struct 上仍声明的索引，**不删除**已移除标签的索引。由此：

| 类别 | 索引 | 判定 |
| --- | --- | --- |
| 已移除声明 | `idx_logs_upstream_request_id_created_at`（198 MB） | 可安全删，但**已在一期清单** |
| 仍在声明（9 个） | `idx_logs_request_id_created_at`（375 MB）、`idx_logs_upstream_request_created_at_id`（270 MB）、`idx_logs_channel_created_at`（171 MB）、`idx_logs_token_created_at`（164 MB）、`idx_logs_group_created_at`（162 MB）、`idx_logs_username_created_at`（158 MB）、`idx_logs_user_created_at`（157 MB）、`index_username_model_name`（45 MB）、`idx_logs_token_id`（34 MB） | **不可直接删**：删后下次启动被 `AutoMigrate` 重建 |

**结论：候选集为空。** 凡「零扫描 + 已退役声明」的索引，一期已全部覆盖；凡「零扫描但仍在声明」的，
删除不会持久生效。

### 为什么不实施二期

一期之所以有效，正是因为它删的 21 个索引都在提交 `eade672f6` 里**从 struct 移除了标签**
（声明 41 → 19），所以 `AutoMigrate` 不会重建。

二期的 9 个残留索引性质不同：它们不是「设计上无用」，而是「在当前生产数据分布下未被选中」。
生产 `username` 只有 1 个取值，一旦换到多用户环境，`idx_logs_request_id_created_at`（375 MB）这类
很可能被 planner 使用。要落地二期必须「移除 struct 声明 + 一次性删除」双改动，收益约 1.5 GB，
代价是改动 `Log` 结构声明并可能影响其他环境的查询计划——不划算。

## 验证命令与退出码

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | exit 0 |
| 生产 `SELECT count(*) FROM logs` | 2,565,445（只读） |
| 生产 `pg_indexes WHERE tablename='logs'` | 41 行（只读） |
| 生产 `pg_stat_user_indexes` | 33/41 为 `idx_scan=0`（只读） |
| 生产 `EXPLAIN (COSTS OFF)` | 走 `idx_logs_username`，不走组合索引（只读） |
| 清单 21 项逐条比对生产 | 21/21 存在 |
| 容器状态 | `new-api` healthy，`/api/healthz` HTTP 200，全程未重建 |

## 未覆盖边界

- **`DROP INDEX` 在生产量级上的实际耗时与锁持有时长未验证**。这是唯一残留风险。
- **`idx_scan` 的观测窗口只有本次取样的 2 小时**（容器上次启动至今）。不能据此断言历史从未使用；
  结论依赖的是 `EXPLAIN` 与数据分布这两项与时间无关的证据。
- **9 个「仍在声明且零扫描」的索引未处理**，其中 `idx_logs_request_id_created_at` 达 375 MB。
  判定为暂不处理，理由见上；若未来生产用户数增长，应重新评估。
- **未在多用户形态的数据集上验证**。生产 `username` 只有 1 个取值是极端情况，
  本记录关于「组合索引无用」的结论绑定在这个数据分布上。
- 未评估 `logs` 表本身的膨胀与 `VACUUM`/分区策略，超出本次范围。
