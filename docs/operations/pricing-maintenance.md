# 模型价格维护与历史日志修正规程

## 适用范围

本文用于维护模型价格配置、复核正式服务价格异常，以及在价格配置错误后修正历史消费日志金额。

涉及阶梯计费、工具定价、预扣费或结算链路前，必须先阅读 [expr.md](../../pkg/billingexpr/expr.md)。

## 价格配置口径

### 标准 token 倍率

`ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio` 使用历史倍率口径，不直接写完整美元价格：

```text
ModelRatio = input_usd_per_1M / 2
CompletionRatio = output_usd_per_1M / input_usd_per_1M
CacheRatio = cached_input_usd_per_1M / input_usd_per_1M
CreateCacheRatio = cache_write_usd_per_1M / input_usd_per_1M
```

示例：模型输入价格为 `$1.25 / 1M tokens`，输出价格为 `$10 / 1M tokens`，缓存读取为 `$0.125 / 1M tokens`：

```json
{
  "ModelRatio": 0.625,
  "CompletionRatio": 8,
  "CacheRatio": 0.1
}
```

### 阶梯计费表达式

`ModelBillingExpr` 的表达式系数直接使用供应商真实的美元每百万 token 价格，不做 `/2` 换算。

示例：

```text
tier("base", p * 1.25 + c * 10 + cr * 0.125)
```

### 按次价格

按次价格使用 `ModelPrice`，不能混入标准 token 倍率修复流程。

## 当前重点模型价格

以下是 2026-04-29 已在正式库复核通过的重点模型口径：

| 模型 | ModelRatio | CompletionRatio | CacheRatio |
| --- | ---: | ---: | ---: |
| `gpt-5.5` | 2.5 | 6 | 0.1 |
| `gpt-5.5-openai-compact` | 1.25 | 6 | 0.1 |
| `gpt-5.4` | 1.25 | 6 | 0.1 |
| `gpt-5.4-openai-compact` | 0.625 | 6 | 0.104 |
| `gpt-5.4-mini` | 0.375 | 6 | 0.1 |
| `gpt-5.3-codex` | 0.875 | 8 | 0.1 |
| `gpt-5.2` | 0.875 | 8 | 0.1 |
| `gpt-5.2-codex` | 0.875 | 8 | 0.1 |
| `gpt-5.1` | 0.625 | 8 | 0.1 |
| `gpt-5.1-codex` | 0.625 | 8 | 0.1 |
| `gpt-5.1-codex-mini` | 0.125 | 8 | 0.1 |
| `gpt-5` | 0.625 | 8 | 0.1 |
| `gpt-5-codex` | 0.625 | 8 | 0.1 |
| `gpt-5-codex-mini` | 0.125 | 8 | 0.1 |
| `o4-mini` | 0.55 | 4 | 0.25 |
| `gpt-image-2` | 2.5 | 6 | 0.25 |

价格来源必须以供应商官方价格页或模型页为准。无法确认官方价格时，不得批量补价。

## 正式库更新流程

1. 导出当前配置快照。

```bash
docker exec postgres psql -X -v ON_ERROR_STOP=1 -P pager=off -U root -d new-api -AtF $'\t' \
  -c "select key, value from options where key in ('ModelRatio','CompletionRatio','CacheRatio','CreateCacheRatio','ModelPrice') order by key;" \
  > /tmp/new-api-pricing-options-before-$(date +%Y%m%dT%H%M%S).tsv
```

2. 只更新可确认的模型，不覆盖整张表中的无关条目。

3. 更新后等待运行进程同步 options，或观察日志中的同步记录。

4. 复核活跃模型是否仍有缺价。

```sql
WITH active_models AS (
  SELECT DISTINCT trim(m) model
  FROM channels c CROSS JOIN LATERAL regexp_split_to_table(coalesce(c.models,''), ',') m
  WHERE c.status = 1 AND trim(m) <> ''
  UNION
  SELECT DISTINCT model FROM abilities WHERE enabled = true
), mr AS (
  SELECT value::jsonb j FROM options WHERE key = 'ModelRatio'
), mp AS (
  SELECT value::jsonb j FROM options WHERE key = 'ModelPrice'
)
SELECT count(*)
FROM active_models a, mr, mp
WHERE NOT (mr.j ? a.model) AND NOT (mp.j ? a.model);
```

期望结果为 `0`。

## 历史日志修正规则

历史日志只能在满足以下条件时使用标准倍率公式批量回算：

- `logs.type = 2`
- `other` 是 JSON 对象
- `model_price < 0`
- 未使用 `billing_mode=tiered_expr`
- 不包含 `web_search`
- 不包含 `file_search`
- 不包含 `image_generation_call`
- 不包含 `audio_input_seperate_price`
- 不包含 `image`
- 存在可用的 token 用量和价格快照字段

必须跳过或单独设计修复逻辑的场景：

- 订阅计费日志
- 阶梯计费表达式日志
- 按次价格模型
- 图片、音频、Web Search、File Search 等附加计费日志
- 无法确认模型官方价格的日志

## 标准倍率回算公式

普通 OpenAI 风格 token 日志的回算公式：

```text
new_quota = round(
  (
    (prompt_tokens - cache_tokens - cache_creation_tokens)
    + cache_tokens * new_cache_ratio
    + cache_creation_tokens * cache_creation_ratio
    + completion_tokens * new_completion_ratio
  )
  * new_model_ratio
  * group_ratio
)
```

如果 `prompt_tokens + completion_tokens = 0`，则 `new_quota = 0`。

## 历史修正必须同步的表

修正历史日志时，不能只改 `logs`。必须同步以下数据：

| 表 | 字段 |
| --- | --- |
| `logs` | `quota`、`other` 中的价格快照 |
| `users` | `quota`、`used_quota` |
| `tokens` | `remain_quota`、`used_quota` |
| `channels` | `used_quota` |
| `quota_data` | 小时聚合 `quota` |

修正后还必须处理 Redis 缓存：

- 清理或对齐受影响的 `user:<id>`。
- 清理或对齐受影响的 `token:<hmac_key>`。
- 如果正式服务有实时请求，缓存可能会在修正期间被重新写入；最终以数据库为准重新对齐一次。

## 2026-04-29 正式库修正记录

本次修正创建备查表：

```text
pricing_repair_logs_20260429_124454
```

修正结果：

| 项目 | 数值 |
| --- | ---: |
| 修正消费日志 | 27306 |
| 旧 quota 合计 | 2072608248 |
| 新 quota 合计 | 292275163 |
| 差额 | -1780333085 |

修正后复核结果：

```text
missing_price_active_models = 0
remaining_repairable_old_snapshot = 0
```

## 巡检注意事项

- 近实时错误日志中出现 `502`、`503`、`520`、`522`、`524`、`525` 时，优先判断上游或 CDN 链路，不要直接归因到价格配置。
- `unlimited_quota=true` 的令牌可能存在负数 `remain_quota`，这通常不阻断使用，但会影响展示直观性。
- 新增模型后，如果模型测试可用但消费日志价格异常，先检查最终请求模型名、模型映射和 `other` 中的价格快照。
