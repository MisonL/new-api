# model ratio 默认回退风险复核

## Summary

本报告记录模型价格缺失、错误倍率和历史日志金额异常的复核结果。

当前状态更新于 2026-04-29：

- 正式库活跃注册模型均已配置 `ModelRatio` 或 `ModelPrice`，缺价数量为 `0`
- 可按标准倍率公式继续回算的旧价格日志快照数量为 `0`
- `106` 渠道历史错误日志已闭环
- 全库可安全判定的同类历史金额异常已完成批量修正
- 运行期价格维护口径已沉淀到 [pricing-maintenance.md](../operations/pricing-maintenance.md)

本报告原始风险是“模型未显式配置价格时可能落到默认回退值，进而产生错误扣费日志”。当前正式库数据侧已完成清理，但后续新增模型仍必须按价格维护 runbook 做准入复核。

## Control Contract

- Primary Setpoint
  - 进入真实扣费链路的模型必须能命中显式价格配置或按次价格配置。
- Acceptance
  - 活跃注册模型缺价数量为 `0`。
  - 可安全回算的旧倍率日志残留数量为 `0`。
  - 历史日志、用户余额、令牌余额、渠道用量和 `quota_data` 聚合口径一致。
- Guardrails
  - 不误伤真实高价模型，例如合法的 `gpt-4.5-preview`。
  - 不用标准 token 倍率公式修复按次计费、阶梯表达式、工具附加费、图片附加费或订阅链路。
  - 修复前必须创建可回滚备查表或导出快照。
- Boundary
  - `options` 中的 `ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio`、`ModelPrice`
  - 正式库 `logs`、`users`、`tokens`、`channels`、`quota_data`
  - Redis 用户与令牌额度缓存

## Current Findings

### 1. 活跃模型价格覆盖已闭环

2026-04-29 复核结果：

```text
missing_price_active_models = 0
```

该检查同时覆盖：

- `channels.models` 中启用渠道暴露的模型
- `abilities` 中启用的模型路由
- `options.ModelRatio`
- `options.ModelPrice`

### 2. 可安全回算的旧倍率日志已清零

2026-04-29 复核结果：

```text
remaining_repairable_old_snapshot = 0
```

本次只修复满足以下条件的日志：

- `logs.type = 2`
- `other` 为 JSON 对象
- `model_price < 0`
- 未使用 `billing_mode=tiered_expr`
- 不包含 `web_search`、`file_search`、`image_generation_call`、`audio_input_seperate_price`、`image`
- 具备可回算的 `model_ratio`、`completion_ratio`、`cache_ratio`、`group_ratio` 和 token 用量

未满足这些条件的日志不能用标准 token 倍率公式批量修复。

### 3. 历史金额修正结果

本次正式库修正创建了备查表：

```text
pricing_repair_logs_20260429_124454
```

修正汇总：

| 项目 | 数值 |
| --- | ---: |
| 修正消费日志 | 27306 |
| 旧 quota 合计 | 2072608248 |
| 新 quota 合计 | 292275163 |
| 差额 | -1780333085 |

同步更新范围：

- `logs.quota`
- `logs.other` 中的价格快照字段
- `users.quota` 与 `users.used_quota`
- `tokens.remain_quota` 与 `tokens.used_quota`
- `channels.used_quota`
- `quota_data` 小时聚合
- Redis 中受影响的用户与令牌缓存

### 4. 当前仍需关注的非阻断项

近 1 小时错误日志仍有上游失败，主要为 `gpt-5.5` 在部分渠道返回：

```text
502, 503, 520, 522, 524, 525
```

这些错误来自上游或 CDN 链路，不属于本次价格配置和历史金额修复残留。

另外，部分 `unlimited_quota=true` 的令牌可能存在负数 `remain_quota`。这不阻断无限额度令牌使用，但属于展示和数据卫生观察项，不能与用户余额扣费错误混为一类。

## Standard Ratio Formula

标准倍率配置不是直接写美元价格，而是沿用历史 quota 倍率：

```text
ModelRatio = input_usd_per_1M / 2
CompletionRatio = output_usd_per_1M / input_usd_per_1M
CacheRatio = cached_input_usd_per_1M / input_usd_per_1M
CreateCacheRatio = cache_write_usd_per_1M / input_usd_per_1M
```

阶梯计费表达式不同，表达式系数必须直接使用真实美元每百万 token 价格，不做 `/2` 换算。

## Verification Snapshot

关键复核命令：

```bash
docker exec postgres psql -X -v ON_ERROR_STOP=1 -P pager=off -U root -d new-api -AtF $'\t' -c "<SQL>"

docker logs --since 15m new-api 2>&1 | rg -n '\[ERROR\]|\[FATAL\]|panic|invalid character|model_price_error|option sync failed'
```

复核结果：

- 正式服务 `/api/status` 正常。
- `new-api` 容器状态为 healthy。
- 最近应用日志未发现 options 解析或计费配置错误。
- 活跃模型缺价数量为 `0`。
- 可继续回算的旧价格日志数量为 `0`。

## Follow-up Rule

后续新增或同步模型时必须先查官方价格，再按 [pricing-maintenance.md](../operations/pricing-maintenance.md) 完成：

- 价格换算
- options 更新
- 活跃模型缺价检查
- 真实请求日志抽样
- 必要时历史日志修正
