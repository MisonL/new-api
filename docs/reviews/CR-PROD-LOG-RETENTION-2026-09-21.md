# CR: 正式环境日志保留策略启用与历史日志清理

- 日期：2026-09-21
- 范围：正式 `postgres` 容器 `/new-api` 库的 `logs` 表、正式 `new-api` 容器的
  `log_retention_setting` 配置
- 状态：已完成

## 背景与授权

用户指令："启用吧，保留3个月的日志；优化一下"。清理目标为正式库中超过 3 个月的
历史日志，同时把保留策略落到配置里，使其此后自动执行。

正式服务 `new-api`（端口 13000）全程**未重建、未重启**：

```text
Started=2026-09-18T08:45:10.621584846Z  Restarts=2  Health=healthy
HTTP /api/status = 200
```

清理前后 `StartedAt` 与 `RestartCount` 一致，证明配置写入与删除都走的是应用自身
接口，没有触碰容器生命周期。

## 执行前的容量基线

采样时间：2026-09-21 18:15 - 18:20 Asia/Shanghai。

```sql
select count(*) from logs;                                  -- 3328315
select count(*) from logs
 where created_at >= extract(epoch from now())::bigint - 90*86400;  -- 2488221
select count(*) from pg_indexes where tablename='logs';     -- 41
select pg_database_size('new-api');                         -- 14226234727 (13 GB)
```

按类型分布与超期行数（cutoff 取 90 天前）：

| type | 名称 | 总数 | 超 90 天 |
| --- | --- | --- | --- |
| 2 | consume | 2,754,908 | 695,680 |
| 3 | manage | 3 | 3 |
| 4 | system | 96 | 59 |
| 5 | error | 573,388 | 144,376 |

`consume` 与 `error` 占绝大多数。**财务类日志即 `topup`（type 1）与 `refund`（type 6）
在库里均为 0 行**，因此本轮没有触碰任何充值或退款凭证。

`logs` 表物理大小为 13 GB（heap 5484 MB + 索引 8052 MB），索引比数据本身还大，
与 `CR-LOG-INDEX-CONSOLIDATION-2026-09-21.md` 记录的问题一致。

## 写入的保留策略

通过 `newapi` skill 的 `api.js` 调用正式 `PUT /api/option/`（`RootAuth`），
逐键写入。每个字段对应独立 option key，符合 `setting/config/config.go`
`LoadFromDB` 的 `<ConfigName>.<field>` 映射口径。

| key | 值 | 说明 |
| --- | --- | --- |
| `log_retention_setting.enabled` | `true` | 启用 |
| `log_retention_setting.run_interval_hours` | `24` | 每天一次 |
| `log_retention_setting.batch_size` | `5000` | 单批上限（`normalizeRetentionBatchSize` 上限） |
| `log_retention_setting.max_batches` | `1000` | 单次上限（`normalizeRetentionMaxBatches` 上限） |
| `log_retention_setting.consume_retention_days` | `90` | |
| `log_retention_setting.error_retention_days` | `90` | |
| `log_retention_setting.system_retention_days` | `90` | |
| `log_retention_setting.manage_retention_days` | `90` | |
| `log_retention_setting.unknown_retention_days` | `90` | |
| `log_retention_setting.server_log_cleanup_enabled` | `true` | |
| `log_retention_setting.server_log_keep_files` | `30` | |
| `log_retention_setting.server_log_keep_days` | `30` | |
| `log_retention_setting.server_log_max_total_size_mb` | `2048` | |

**刻意未设置**的键与理由：

- `topup_retention_days` 与 `refund_retention_days` 保持默认 `0`。`executeLogRetention`
  对 `days <= 0` 的 policy 直接 `continue`，即这两类财务日志永久保留。行情上库内
  当前为 0 行，但策略必须保证未来产生的充值/退款凭证不会被自动删除。
- `request_payload_retention_days` / `response_payload_retention_days` 保持 `0`。
  逐个核对 4 个用户的 `setting`，无人开启 `record_request_content_log` /
  `record_response_content_log`，内容审计目前无数据，设为非 0 只会多跑无意义的
  `other LIKE '%"request_content":%'` 全表扫描。

`log_retention_setting.unknown_retention_days=90` 对应 `LogTypeUnknown` 走
`LogTypeSet=true` 的全类型过滤，仓库内无写入方，预览命中 `matched=0`，保留它只是
为了策略完整。

## 破坏性执行与结果

先跑 `POST /api/log/retention/execute` `{"preview":true}` 确认范围，再跑
`{"preview":false}`。两次 `cutoff` 相差 25 秒，命中数差异来自期间的实时流量
（`consume` 695,687 -> 695,688）。

预览输出：

```text
type=0 unknown   matched=0
type=2 consume   matched=695687
type=3 manage    matched=3
type=4 system    matched=59
type=5 error     matched=144376
server_log_files enabled=true log_dir=/app/logs matched_files=0
```

预览命中合计 840,125 行，与执行前独立 SQL 计数
`select count(*) from logs where created_at <= 1782209888 and type in (2,3,4,5)`
返回的 840,125 完全一致。

正式执行返回：

```text
type=2 consume  deleted=695688
type=3 manage   deleted=3
type=4 system   deleted=59
type=5 error    deleted=144376
started_at=1789985913 finished_at=1789986015   (耗时 102 秒)
```

合计删除 840,126 行。`server_log_files.matched_files=0`：`/app/logs` 下 12 个文件中
最旧的 `oneapi-20260908130735.log` 为 2026-09-08，距今 13 天，未到 30 天阈值；
`max_total_size_mb=2048` 也未触发（总计 352.1 MB）。

删除后复核：

```sql
select count(*) from logs where created_at <= 1782209913;   -- 0
select type, count(*), min/max(created_at) from logs group by type;
```

```text
type=2 consume  2059344  2026-06-23 10:20:53+00 .. 2026-09-21 10:20:34+00
type=4 system        37  2026-06-29 01:58:55+00 .. 2026-09-03 05:02:42+00
type=5 error     429013  2026-06-23 10:41:17+00 .. 2026-09-21 10:17:42+00
```

超期残留为 0，最旧记录落在 2026-06-23，即删除时刻的 90 天前，边界正确。
总行数 3,328,315 -> 2,488,397。

## 空间回收（删除后未立即释放）

删除完成时 `pg_total_relation_size('logs')` 仍为 13 GB，因为死元组尚未回收：

```text
n_live_tup=2485573  n_dead_tup=934082
活跃进程: autovacuum: VACUUM ANALYZE public.logs (已运行 7 分 14 秒)
```

`logs` 表从未被 autovacuum 处理过（`last_autovacuum` 为空、`autovacuum_count=0`），
本轮 84 万行的删除才第一次把死元组推到阈值以上。按 dead tuple 约 130 MB 估算，
（索引 8054 MB 中相当一部分属于重复前缀索引，不在本轮回收范围内，
其精简见 `CR-LOG-INDEX-CONSOLIDATION-2026-09-21.md`）。

autovacuum 于 2026-09-21 10:26:55 UTC 完成一轮，`n_dead_tup` 从 934,082 降至
360,593，`autovacuum_count=1`，页空间已标记为可重用。

物理文件大小在回收后仍为 heap 5484 MB + 索引 8054 MB = 13 GB，
`pg_database_size('new-api')` 同样维持 13 GB。这是设计内行为，不是异常：
普通 `VACUUM` 只把页标记为可重用供后续写入复用，不会把文件归还操作系统。
要让物理文件真正缩小必须走 `VACUUM FULL` 或 `REINDEX`，两者都需要
`ACCESS EXCLUSIVE` 锁，本轮未做（见下方边界）。

因此**本次清理的实际收益是"止血"而非"缩容"**：在保留策略生效前，表以每月约
28 万行的速度增长；现在写入会优先复用已释放的页，增长速度显著放缓。

## 未覆盖与边界

- **未执行 `VACUUM FULL` 或 `REINDEX`**。两者都需要 `ACCESS EXCLUSIVE` 锁，
  会在正式服务持续接流时阻塞 `logs` 表的读写。本轮未做。
- **删除前未做表级备份**。用户明确授权删除（"要删"）；财务类日志（topup/refund）
  本轮为 0 行且策略上永久保留，因此不存在充值/退款凭证丢失。被删除的是
  consume/error/system/manage 四类运行时日志。
- **`unknown` 类型（type 0）无数据**，其策略分支只靠预览的 `matched=0` 验证。
- **`server_log_cleanup` 的删除路径本轮未真实触发**，因为没有任何文件同时满足
  "不在保留的最近 30 个之内"与"超过 30 天"；只验证到 `matched_files=0` 与
  `log_dir` 解析正确。文件选择逻辑由 `controller/log_retention_test.go` 的
  两个用例覆盖（跳过活跃文件、年龄与大小合并选择）。
- 本轮未改动源码，仅写入正式库配置与删除正式库数据行。
- 正式 `logs` 表的 41 个索引未变，索引精简仍是待部署改动，见
  `CR-LOG-INDEX-CONSOLIDATION-2026-09-21.md`。

## 后续

保留策略启用后，正式容器每天自动执行一次。首次由
`StartLogRetentionTask` 的 `logRetentionInitialDelay = 5 * time.Minute` 决定：
**下次重启后 5 分钟**会跑第一次，之后按 `run_interval_hours` 循环。当前不重启，
因此下一次自动执行发生在未来某次正常发布之后；在此之前如需再清理，可手动调用
`POST /api/log/retention/execute`。
