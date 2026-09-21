# CR: 正式 PostgreSQL 参数调优评估

- 日期：2026-09-21
- 范围：正式 `postgres` 容器（postgres:15，`docker-compose.yml` 中 `postgres` 服务）
- 状态：已完成评估，**未修改任何参数**

## 采样环境

| 项 | 值 |
| --- | --- |
| Docker VM | 6 vCPU / 11.68 GiB |
| `postgres` 容器 | 337 - 567 MiB 常驻，1.01% CPU |
| `new-api` 容器 | 456 - 481 MiB 常驻，17.54% CPU |
| 全部容器内存合计 | 约 4.12 GiB / 11.68 GiB |
| 存储 | `/dev/vda1`，`rotational=0`（SSD），21% 使用率 |
| `pg_data` | Docker 卷（默认 local driver，非 bind mount） |
| 连接数 | 总计 14，活跃 1 |
| `logs` 表 | 删除前 3,328,315 行 / 13 GB（heap 5484 MB + 索引 8052 MB） |

全部参数均为 PostgreSQL 15 默认值，没有任何自定义 `postgresql.conf` 调优。

## 核心问题：缓存命中率过低

`pg_stat_database` 累计值：

```text
blks_hit=49060274  blks_read=25007251  ->  DB 级命中率 66.24%
```

按表拆分（`pg_statio_user_tables`）：

| 表 | heap 命中率 | index 命中率 |
| --- | --- | --- |
| `logs` | **17.85%** | 96.26% |
| `quota_data` | 85.92% | 97.70% |

索引命中率健康，**堆表命中率 17.85% 是主要瓶颈**：`shared_buffers` 只有 128 MB，
而 `logs` 堆表 5484 MB，工作集远超缓存。

按窗口实测热数据体量（`sum(pg_column_size)`）：

| 窗口 | 行数 | 近似体量 |
| --- | --- | --- |
| 7 天 | 232,092 | 349 MB |
| 30 天 | 1,088,497 | 1,676 MB |
| 90 天 | 2,488,491 | 3,921 MB |

保留策略落地后稳态工作集约为 90 天 = 3.9 GB（`CR-PROD-LOG-RETENTION-2026-09-21.md`）。

## 问题二：JIT 在本负载下是净损失

`jit=on` 为 PG 15 默认。本次在同一批真实查询上做了 on/off 对照（`set jit=...` +
`explain (analyze)`）：

| 查询 | jit=on | jit=off | 倍数 |
| --- | --- | --- | --- |
| 24 小时内 `type=2` 计数 | 23.2 ms | 7.8 ms | 3.0x |
| 7 天内全部日志计数 | 1019.8 ms | 282.2 ms | 3.6x |
| 按 `model_name` 分组 Top 20（24 小时） | 741.4 ms | 26.8 ms | **27.7x** |

代价来自 `jit_above_cost=100000` 阈值偏低：这些 `count` / `group by` 聚合的估算代价
经常压过阈值而触发编译，但每次执行只跑一次、省不下足够多的解释器开销。计划里的
`Functions: 3` 到 `Functions: 11` 即 JIT 编译产物。

`logs` 是网关的日志表，查询形态高度固定（列表页计数、按类型/用户/时间过滤的聚合），
没有长事务或大排序能摊薄 JIT 编译成本，因此本负载不属于 JIT 的收益场景。

## 问题三：autovacuum 对大表过于迟钝

`logs` 从未被 autovacuum 处理过：

```text
last_autovacuum = (空)   autovacuum_count = 0
```

原因：`autovacuum_vacuum_scale_factor=0.2`，表级阈值为
`50 + 0.2 * n_live_tup`。`logs` 长期在 330 万行量级，阈值约 66 万死元组才触发。
本轮一次性删除 84 万行后，`n_dead_tup` 达到 934,082 才越过阈值，
autovacuum 随即启动并已运行 7 分钟以上。

这说明在常规写入速率下，`logs` 会长期积累死元组而不回收，占用磁盘并抬高顺序扫描成本。

## 建议清单

以下为**建议项，本轮均未执行**。修改 `shared_buffers` 等需要重启；其余可用
`ALTER SYSTEM SET` + `SELECT pg_reload_conf()` 在线生效。

### 需要重启（postmaster 级）

| 参数 | 当前 | 建议 | 理由 |
| --- | --- | --- | --- |
| `shared_buffers` | 128 MB | `2GB` | 90 天工作集 3.9 GB，2 GB 可覆盖 30-60 天热数据；VM 有 11.68 GiB、全部容器合计仅 4.12 GiB，余量充足 |
| `max_connections` | 100 | `200` | 与 `SQL_MAX_OPEN_CONNS` 默认 1000 不匹配，当前虽只用 14，但突发扩容时 100 是硬顶 |

### 可在线生效

| 参数 | 当前 | 建议 | 理由 |
| --- | --- | --- | --- |
| `jit` | on | `off` | 实测净损失 3-28x，本负载无收益场景 |
| `random_page_cost` | 4 | `1.1` | 存储为 SSD（`rotational=0`），4 是机械盘默认值，会误导规划器避开索引扫描 |
| `effective_io_concurrency` | 1 | `200` | 同上，SSD 可并发提交 IO |
| `effective_cache_size` | 4GB | `6GB` | 允许规划器正确评估 OS page cache 可用量 |
| `work_mem` | 4 MB | `16MB` | 当前已有 8 次临时文件落盘、累计 269 MB；列表页排序与聚合受益 |
| `maintenance_work_mem` | 64 MB | `256MB` | 加速 autovacuum 与后续可能的 `REINDEX` |
| `autovacuum_vacuum_scale_factor` | 0.2 | `0.02`（表级） | 对 `logs` 单独设，阈值从 66 万降到约 5 万死元组 |
| `autovacuum_vacuum_cost_limit` | -1（实际 200） | `1000` | 加速大表回收；当前 `maxwritten_clean=614` 说明写限速在拖慢 autovacuum |
| `default_statistics_target` | 100 | `200` | 改善多列过滤（如 `type + created_at`）的选择率估算 |
| `log_min_duration_statement` | -1 | `1000`（ms） | 当前完全无慢查询记录，1 秒阈值可用于发现回归 |
| `track_io_timing` | off | `on` | 无此开关时 `pg_stat_statements` 的 IO 耗时统计不可用 |

### 表级 autovacuum（推荐优先做，不需重启）

```sql
ALTER TABLE logs SET (autovacuum_vacuum_scale_factor = 0.02,
                      autovacuum_analyze_scale_factor = 0.01);
ALTER TABLE logs SET (autovacuum_vacuum_cost_limit = 1000);
```

这两条只影响 `logs`，风险最低，且直接解决上面第三个问题。

### 可选：pg_stat_statements

镜像内已带 `pg_stat_statements` 扩展文件，但 `shared_preload_libraries` 为空，
需要重启并加到预加载列表才能用。启用后可以按总耗时排序定位真实瓶颈，
取代当前依赖手工采样的做法。建议与 `shared_buffers` 调整在同一次重启中完成。

## 未覆盖与边界

- **全部建议均为评估结论，未在任何环境执行**。没有 `ALTER SYSTEM`、没有重启、
  没有改 `docker-compose.yml`。因此"调优后提升多少"没有实测数据。
- 上表的收益方向基于本轮的实测采样（JIT 对照、命中率、死元组）与通用 PG 经验，
  不是压测结论。落地前应在隔离环境按 `deploy/compose/dev-isolated.yml` 复现验证。
- `shared_buffers=2GB` 是按"总内存 11.68 GiB、其他容器合计 4.12 GiB"推算的，
  若后续在同一 VM 增加重负载容器，需要重新评估。
- 未测 `max_parallel_workers_per_gather=2` 是否适合 6 vCPU；本轮未做并行度扫描。
- 索引精简（41 -> 20 个声明）尚未部署到正式环境，部署后 `logs` 索引将显著缩小，
  届时工作集与 `shared_buffers` 的匹配关系会改善，建议先部署索引精简再定
  `shared_buffers` 的最终值。
