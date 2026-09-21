# CR: Docker 资源清理与性能排查

- 日期：2026-09-21
- 范围：Docker 构建缓存/镜像/卷、`.dev-docker` 运行残留、生产 PostgreSQL `logs` 表与索引、日志保留策略
- 状态：清理已执行（仅非生产数据）；生产库与源码改动待确认，未执行

## 采样环境

| 项 | 值 |
| --- | --- |
| 采样时间 | 2026-09-21 10:30 - 12:00 Asia/Shanghai |
| git HEAD | `5cda680770543782d5d0a757b31f4e783443dc03`（本轮未改仓库文件） |
| 正式服务 | `new-api`，镜像 `new-api-local:prod-codex-lite-4527d7666-litefix2`，端口 13000，`running/healthy` |
| 正式容器 | `StartedAt=2026-09-18T08:45:10Z`，`RestartCount=2`，全程未重建、未重启 |
| PostgreSQL | `postgres:15`，容器 `postgres`，库 `new-api`，20 GB |
| Docker VM | 6 CPU / 11.68 GiB（宿主 16 CPU / 64 GB） |

## 一、已执行的清理（不涉及生产库、不涉及生产容器）

| 对象 | 数量 | 释放 | 判据 |
| --- | --- | --- | --- |
| `.dev-docker/runtime/isolated/logs/*.log` | 1309 个 | 61 MB | 文件名时间戳早于 2026-06-21 |
| `.dev-docker/backups/prod-upgrade-*` 等 14 个目录 | 14 个 | 5321 MB | 目录名日期早于 2026-06-21 |
| `.dev-docker/runtime/isolated/*.png` | 11 个 | 5.5 MB | mtime 早于 92 天 |

清理前逐项核对：

- 14 个备份目录均未被 `docs/`、`scripts/` 中任何已跟踪文件引用（`git grep` 结果为空）。
- 备份目录内 `rollback-image-tag.txt` 指向的镜像 `new-api-local:prod-main-rollback-20260527-181904-...`
  经 `docker image inspect` 确认**不存在**，删除备份不损失可回滚目标。
- 生产运行日志 `logs/*.log` 最老为 2026-09-08，**无超过 3 个月的文件**，本轮未删除任何生产日志文件。

`.dev-docker` 由 7.4 GB 降至 2.1 GB。生产容器 `StartedAt` 与 `/api/status` 在清理前后一致。

## 二、未执行、需确认的清理项

### 2.1 生产库 73 张备查表（6751 MB）

`docs/operations/pricing-maintenance.md` 明确把 `pricing_repair_*` 称为「备查表」并记录其行数，
属于历史定价修正的审计证据，不是运行时残留（`git grep` 确认代码零引用）。
按大小排序的前 5 张占 6.7 GB：

| 表 | 行数 | 大小 |
| --- | --- | --- |
| `pricing_repair_20260906_gpt_tiered_logs` | 1,654,628 | 2445 MB |
| `pricing_repair_20260906_gpt56_family_logs` | 1,314,187 | 2306 MB |
| `pricing_repair_20260907_gpt56_alias_logs` | 914,176 | 1650 MB |
| `pricing_repair_logs_20260530_110423` | 179,285 | 295 MB |
| `pricing_repair_logs_20260429_124454` | 27,306 | 41 MB |

其中 `pricing_repair_logs_20260429_124454` 被 `pricing-maintenance.md` 与
`archive/CR-MODEL-RATIO-FALLBACK-SYSTEMIC-RISK-2026-04-21.md` 两处引用。

`option_backups`（1 行，2026-07-01）与 `abilities_backup_before_compact_opt_20260606t111515z`
同样无代码引用。`two_fa_backup_codes` 是应用表（`model/twofa.go`），**不可删**。

### 2.2 生产库 `logs` 早于 2026-06-21 的行

833,662 行（类型 2 消费 689,463 / 类型 5 错误 140,472 / 类型 4 系统 57 / 类型 3 管理 3）。
`logs` 无 `DeletedAt` 字段，删除是物理删除；`quota_data` 是独立聚合表，不从 `logs` 回算，
因此删除历史日志不影响账务与看板。但 `log_retention_setting` 从未配置
（`Enabled` 默认 `false`），删行前需要先决定保留口径。

### 2.3 未被任何容器引用的镜像（约 2.758 GB 可回收）

`docker system df` 报告 2.758 GB 可回收。其中 `new-api-local:dev` 是隔离环境在用镜像的
**另一份**（容器实际用的是 `00d75010bccd`），属构建产物，删 tag 不影响运行中容器。
其余多为其它项目（qiyu/qdm/petdex 等）的镜像，不在本仓库任务边界内。

构建缓存 14.56 GB、可回收 11.69 GB，全部条目 `LAST USED` 在 20 天内，**无超过 3 个月的条目**，
按「三个月前」口径不在清理范围内。

## 三、性能排查

### 3.1 `logs` 表索引：41 个，8.0 GB

`logs` 总 13 GB = 堆 5466 MB + 索引 8030 MB。按 `pg_stat_user_indexes.idx_scan`：

| 分类 | 数量 | 占用 |
| --- | --- | --- |
| 观测窗口内从未被扫描 | 34 | 7012 MB |
| 前缀冗余（被更长索引完全覆盖） | 10 | 942 MB |
| 并集（去重后真正可回收） | 36 | 7090 MB |
| 扣除 `logs_pkey`（主键约束索引，不可单独 DROP） | 35 | 6959 MB ≈ 6.8 GB |

**观测窗口边界（重要）**：`pg_postmaster_start_time()` = 2026-09-18 08:45 UTC，
窗口 67.1 小时，仅覆盖 `logs` 全表 3.3 M 行中的 103,364 行（3.1%）。
`idx_scan` 是窗口内累计值，因此「从未被扫描」只对该窗口成立；周/月报表、历史定价修正脚本、
按 `request_id` 反查等低频场景未被覆盖。`pg_stat_database.stats_reset` 为 NULL，
说明计数器自数据库创建起未重置。

**受控基准**（隔离库 `new-api-dev`，两表同构，A 组按生产 41 个索引，B 组 8 个索引；
每轮 2000 行、A/B 交错，事务内回滚不留数据）：

| 轮次 | A（41 索引） | B（8 索引） | 倍数 |
| --- | --- | --- | --- |
| R1 | 1613 ms | 122 ms | 13.2x |
| R2 | 1777 ms | 424 ms | 4.2x |
| R3 | 1598 ms | 115 ms | 13.8x |

大样本（20000 行）单轮：A 19153 ms / B 4458 ms。第二、三轮 A 升至 28149-29997 ms，
B 稳定在 7123-7410 ms，但该组数据受宿主负载波动影响，只作参考。

**删除索引必须先改源码**：`model/main.go:443` 在启动时执行 `LOG_DB.AutoMigrate(&Log{})`，
`model/log.go:26-46` 的 gorm tag 声明了其中 33 个索引。只 `DROP INDEX` 会在下次重启时被重建。
不受 AutoMigrate 管理、可直接 DROP 的只有 2 个：
`idx_created_at_id`（182 MB）、`idx_logs_upstream_request_id_created_at`（197 MB）。

### 3.2 生产慢 SQL（`docker logs` 窗口 2026-09-20 15:43 至 2026-09-21 11:45，约 20 小时）

2364 条 `SLOW SQL >= 200ms`，按语句类型：

| 语句 | 次数 |
| --- | --- |
| `INSERT INTO "logs"` | 1178 |
| `UPDATE "users"` | 303 |
| `UPDATE "channels"` | 274 |
| `UPDATE "tokens"` | 208 |
| `SELECT "id" FROM "user_subscriptions"` | 144 |

耗时分布：<300ms 815 / 300-500ms 709 / 500ms-1s 521 / **>1s 319**，
最慢 30841 ms、28133 ms、27876 ms、27080 ms、26255 ms，**全部是 `logs` 的 INSERT**。

单条 INSERT 在 `EXPLAIN (ANALYZE)` 下（事务内回滚）：冷缓存 22.9-26.5 ms，
热缓存 2.1-4.0 ms，`shared hit=858-912`（即约 900 个 8 kB 页 = 7 MB 缓冲区触碰/条）。
`RecordConsumeLog` 走 `LOG_DB.Create(log)` 同步写入，该延迟落在请求收尾路径上。

### 3.3 PostgreSQL 参数（均为默认值，未按负载调优）

| 参数 | 当前值 | 来源 |
| --- | --- | --- |
| `shared_buffers` | 16384 × 8 kB = 128 MB | 配置文件 |
| `effective_cache_size` | 524288 × 8 kB = 4 GB | 默认 |
| `work_mem` | 4 MB | 默认 |
| `maintenance_work_mem` | 64 MB | 默认 |
| `random_page_cost` | 4 | 默认 |
| `effective_io_concurrency` | 1 | 默认 |
| `autovacuum_max_workers` | 3 | 默认 |

`logs` 堆 5466 MB 而 `shared_buffers` 仅 128 MB，`logs` 堆缓存命中率 28.81%
（heap_blks_hit 2665428 / heap_blks_read 6586710），说明每次查询都大量落盘读。
`logs` 上 `last_autovacuum` = 2026-09-20 01:42，`n_dead_tup` = 93,940。
`logs` 已有表级 reloptions：`autovacuum_vacuum_scale_factor=0.05`、
`autovacuum_vacuum_threshold=5000`、`autovacuum_analyze_scale_factor=0.02`、
`autovacuum_analyze_threshold=5000`。

### 3.4 日志保留策略从未启用

`setting/log_retention_setting/config.go` 的 `Enabled` 默认 `false`，
生产库 `options` 中不存在任何 `log_retention_setting.*` 键，因此
`controller.StartLogRetentionTask()` 的主循环始终跳过执行。
`logs` 现有 3,318,036 行、跨 2026-03-19 至 2026-09-21，无自动回收。

### 3.5 连接池上限与服务端不匹配

`model/main.go:218` / `:257` 默认 `SQL_MAX_OPEN_CONNS=1000`，
而 PostgreSQL `max_connections=100`（`superuser_reserved_connections=3`）。
生产与隔离容器均未显式设置该变量。当前实际连接数 3（1 active），尚未触发，
但并发上升时应用侧会等到服务端拒绝才失败。

### 3.6 存储与检查点

- Docker VM 顺序写 434 / 347 MB/s，顺序读 6.7 GB/s，单块 fsync 5-12 ms。
- `checkpoints_timed=804`、`checkpoints_req=1`、`checkpoint_write_time` 累计 26358 s
  = 运行 66.2 小时中的 11.1%，`checkpoint_sync_time` 累计 65 s。

### 3.7 宿主负载

采样期间宿主 load average 11.73 → 36.10（16 核），`CPU usage: 42.77% user, 53.69% sys,
3.52% idle`。`Virtualization.framework` 虚拟机进程占 193-548%，Docker VM 仅获 6 CPU。
生产 `new-api` 容器实测 CPU 时间增量约 190% 单核。
**遗留忙循环进程按用户要求不在本轮范围，未做任何处理。**

## 四、未覆盖边界

- 未执行任何生产库 DDL/DML（唯一例外见下）。未删除任何 `logs` 行、任何备查表、任何索引。
- 索引可回收量基于 67.1 小时观测窗口，未覆盖周/月报表与历史修正脚本的查询模式。
- 受控基准在隔离库执行，与生产的数据分布、并发、磁盘状态不同，倍数不可外推到生产。
- `SLOW SQL` 只出现在容器 `docker logs`，未写入 `--log-dir` 文件；窗口仅 20 小时
  （`max-size=10m` × `max-file=3`）。
- 未做图片、视频、任务类渠道的性能排查。
- 宿主遗留忙循环进程未排查（用户指定由其它 Agent 处理）。

## 五、本轮对生产库的一次写入及其回滚

为测量单条 INSERT 成本，曾向生产 `logs` 直接插入 1 行探针
（`request_id='perf-probe-req'`），随后按 `request_id` 删除，
`SELECT count(*) ... WHERE request_id LIKE 'perf-%'` 与 `'probe-%'` 均返回 0。
其余测量均包裹在 `BEGIN ... ROLLBACK` 中，未留痕。
