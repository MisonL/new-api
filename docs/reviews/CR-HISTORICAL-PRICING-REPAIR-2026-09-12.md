# 正式库历史价格金额复核与聚合修正

采样时间：2026-09-12 22:00-22:42（Asia/Shanghai）。目标为正式 PostgreSQL `new-api`、正式 `new-api` 容器和 `13000` 端口。整个过程未停止、重启或重建正式服务。

## 历史日志复核

- 以 `logs.id <= 3029198` 固定本轮历史边界；修正器按当前源码中的标准倍率、DeepSeek、Grok、OpenAI 和 Gemini 阶梯计费规则执行全量 dry-run。
- 全量 dry-run：`repaired_logs=0`，实际修正计划只有 CSV 表头，没有新的日志金额或价格快照需要写入。
- 随后新增日志 `3029199..3029290` 的尾部 dry-run：`repaired_logs=0`。
- 终态继续扫描新增日志 `3029291..3029307`：`repaired_logs=0`；该范围之后的日志属于新的实时写入边界。
- `model_price=-1` 仍存在于部分标准 token 日志，但这些日志按当前官方倍率计算后 quota 和价格快照已经一致；该值是 token 计费路径的历史哨兵，不作为单独的金额错误。
- 最终数据库统计显示，历史 `type=2` 日志中共有 `131970` 条保留该哨兵；当前修正器全量 dry-run 未命中需要改写的行，不能把该数量当作未修正金额。
- 既有本轮备查表保持不变：`pricing_repair_20260912_logs` 137 行、`_users` 1 行、`_tokens` 1 行、`_channels` 6 行、`_quota_data` 56 行。
- 终态重新读取最新边界为 `logs.max(id)=3029516`，以当前活跃小时 `1789221600` 为截止再次执行全量历史 dry-run，计划仍只有 CSV 表头（`repaired_logs=0`）。

## `quota_data` 修正

对已结束小时（初始边界 `created_at < 1789218000`，即 2026-09-12 13:00 UTC）按原始 `logs.type=2` 重算，发现 14 个聚合键的 `count`、`token_used` 或 `quota` 不一致。已完成：

- 备份表：`pricing_repair_20260912_quota_data_reaudit`，14 行。
- 只删除并重建这 14 个键，未修改 `logs`、`users`、`tokens` 或 `channels`。
- 修正后全量历史对账：`count_mismatch=0`、`token_mismatch=0`、`quota_mismatch=0`、`duplicate_keys=0`、`quota_delta=0`，共 11,277 个聚合键。

跨过 14:00 UTC 整点后重新对账，新的历史边界 `created_at < 1789221600` 仍为上述五项全 0（11,278 个聚合键）。对账按 `left(logs.model_name, 64)` 比较，因为 `quota_data.model_name` 是 `varchar(64)`；不截断原始模型名会把同一超长模型名错误拆成 4 个差异键，但其总 `count/token_used/quota` 差额均为 0，不是金额问题。当前活跃小时（14:00 UTC）采样为 1 个聚合键、日志与看板 quota 暂差 `34152`，这是正式服务持续写入和异步刷新造成的活动边界，不属于已结束历史数据；本轮未强制重建该桶。

## 验证

| 检查 | 结果 |
| --- | --- |
| `go test -count=1 ./setting/billing_setting ./setting/ratio_setting ./pkg/pricingrepair ./bin` | 通过，退出码 0 |
| Linux 修正器静态构建 | 通过，退出码 0 |
| `git diff --check`（修正器文件） | 通过，退出码 0 |
| `nc -z -w 3 127.0.0.1 13000` | 通过 |
| 正式 `GET /api/status`（`newapi` skill `api.js`） | HTTP 200，`success=true` |
| 正式容器状态 | `running/healthy`；本轮未改变其重启计数 |

正式服务仍在产生实时日志，因此后续复核应重新读取最新 `max(logs.id)`，并继续把当前活跃小时与已关闭历史小时分开判断。

## 2026-09-13 全量日志复核

采样时间：2026-09-13 00:45-01:19（Asia/Shanghai）。本轮只读复核，没有执行 `--apply`、数据库写入、服务重启或容器重建。

### 计费修正器覆盖

- 固定快照边界为 `logs.id <= 3034677`，type=2 日志数为 `2498160`；正式库在扫描期间仍会产生新日志，边界之后属于后续实时写入。
- 通过连续 ID 分片覆盖 `1..3033702`、`3033703..3034287`、`3034288..3034677`：主历史范围 31 个 10 万 ID 左右分片，另加 2 个尾部增量分片，共 33 次 dry-run，全部 `repaired_logs=0`。
- 分片扫描解决了无界一次性加载历史日志的内存限制问题；此前无界尝试返回 `exit 137`，没有产生数据写入。
- 价格快照字段检查：type=2 日志没有缺失或非法格式的 `model_price`、`model_ratio`、`completion_ratio`，没有低于合法哨兵的值；`model_price=-1` 仍是标准 token 计费历史哨兵。
- tiered 日志共 `2365822` 条，`expr_b64`、`matched_tier` 均无缺失或空值，tiered 日志的模型/完成倍率字段均为 0。另有 `21427` 条 token 计数为 0 的 tiered 日志，其 quota 全部为 0，符合上游未返回计费信息的记录语义。

### 日志结构与引用完整性

- 全表 `other` 复核：空字符串 99 条，非法 JSON 也是 99 条，非对象 JSON 为 0；空值全部来自 type=3/4 管理或系统日志，type=2 没有空模型或非法 JSON。
- type=2 的 quota、prompt token、completion token 均无负数；模型名为空 0 条；未来时间戳 0 条。
- type=2 的用户引用无孤立记录；孤立 token 仅 6 条，均为历史 `*-smoke-temp` 临时 token；孤立渠道 204692 条全部是旧历史渠道，最晚 `created_at=1782350094`，没有当前活跃时段的孤立渠道日志。
- 直接钱包路径 `token_id=0` 共 3099 条，均有用户和渠道引用，不属于异常 token 丢失。

### 历史聚合对账

- 以当前活跃小时 `1789232400` 为边界，对关闭小时的原始 type=2 日志按 `left(model_name, 64)` 重算：日志聚合与 `quota_data` 均为 11289 个键。
- 缺失键、孤立键、count/token/quota mismatch、三项总差额、重复键均为 0；关闭历史金额无需再修正。
- 活跃小时当时只有 1 个键，采样到 `quota_delta=92830`，属于正式服务持续写入与异步聚合刷新中的实时差异，不作为历史错误强制重建。

### 余额字段边界

只读比较发现 users/tokens/channels 的 `used_quota` 与当前保留日志总和存在差异（用户 4 条中 1 条、token 10 条中 2 条、渠道 78 条中 76 条）。该字段还受日志保留、人工重置、退款/预扣结算和异步批量更新影响，不能据此反写历史日志；本轮没有擅自改动余额。用户和渠道余额无负值，7 个负 token 余额均带 `unlimited_quota=true`，是无限额度哨兵。

### 本轮运行验证

| 检查 | 结果 |
| --- | --- |
| Linux amd64 临时修正器构建 | 通过；容器内 `-h` 可执行 |
| 连续 ID 分片 dry-run | 主分片 31 片、尾部增量 2 片，共 33 片，全部 `repaired_logs=0` |
| 最新尾部 dry-run | `3034288..3034677`，`repaired_logs=0` |
| 正式 `GET /api/status`（`newapi` skill `api.js`） | HTTP 200，`success=true`，版本 `v1.1.0` |
| 正式容器和端口 | `new-api` `running/healthy`；`nc -z -w 3 127.0.0.1 13000` 通过 |

结论：截至上述固定边界，所有已覆盖的 type=2 历史日志没有发现需要重新写入的价格金额；关闭小时聚合完全一致。正式服务仍在实时写入，边界之后的新日志需在下一次复核中单独纳入。
