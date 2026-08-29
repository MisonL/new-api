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

OpenAI 图像模型的输入图像倍率也必须单独维护。该倍率相对于文本输入倍率计算：

| 模型 | 文本输入 | 图像输入 | `ImageRatio` |
| --- | ---: | ---: | ---: |
| `gpt-image-1-mini` | $2 / 1M | $2.50 / 1M | 1.25 |
| `gpt-image-1` | $5 / 1M | $10 / 1M | 2 |
| `gpt-image-1.5` | $5 / 1M | $8 / 1M | 1.6 |
| `gpt-image-2` | $5 / 1M | $8 / 1M | 1.6 |

来源：OpenAI 官方价格页的 Grouped Pricing Table（2026-08-15 快照）。缓存读取倍率分别为 `0.1`、`0.25`、`0.25`、`0.25`；`gpt-image-1-mini` 的输出倍率为 `4`，`gpt-image-1.5` 为 `2`，`gpt-image-2` 为 `6`。

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

对于全部属于 token 或单次计费且已有官方口径的目录，期望结果为 `0`。按秒、按分辨率或没有可确认官方单价的模型必须先进入显式排除清单；不能为了把结果压到 `0` 而套用相近模型价格。

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

## 2026-08-01 官方价格复核

本轮以供应商官方页面复核以下价格：

| 模型 | 官方输入价格 | 官方缓存命中价格 | 官方输出价格 | 维护方式 |
| --- | ---: | ---: | ---: | --- |
| `kimi-k2.6` | $0.95 / 1M | $0.16 / 1M | $4.00 / 1M | 标准倍率 |
| `gemini-3.1-pro-preview` | $2 / 1M, 200K 以内; $4 / 1M, 超过 200K | $0.20 / 1M, 200K 以内; $0.40 / 1M, 超过 200K | $12 / 1M, 200K 以内; $18 / 1M, 超过 200K | `billing_setting` 阶梯表达式 |
| `minimax-m3` | $0.30 / 1M, 512K 以内; $0.60 / 1M, 超过 512K | $0.06 / 1M, 512K 以内; $0.12 / 1M, 超过 512K | $1.20 / 1M, 512K 以内; $2.40 / 1M, 超过 512K | `billing_setting` 阶梯表达式 |

来源：

- https://platform.kimi.ai/docs/pricing/chat-k26.md
- https://ai.google.dev/gemini-api/docs/pricing
- https://platform.minimax.io/docs/guides/pricing-paygo.md

Kimi K2.6 对应标准倍率为 `ModelRatio=0.475`、`CompletionRatio=4/0.95`、`CacheRatio=0.16/0.95`。Gemini 3.1 Pro Preview 和 MiniMax M3 的长上下文价差不能用固定倍率表示，必须使用 `len` 条件的阶梯表达式。

## 2026-08-13 海外官方价格复核

本轮按海外官方价格页面维护可确认的模型及变体。标准倍率仍按 `ModelRatio=input/2`、`CompletionRatio=output/input`、`CacheRatio=cache_read/input`、`CreateCacheRatio=cache_write/input` 计算。

| 模型 | 输入 | 缓存命中 | 缓存写入 | 输出 | 备注 |
| --- | ---: | ---: | ---: | ---: | --- |
| `glm-5.1`、`glm-5.2` | $1.40 / 1M | $0.26 / 1M | 未公开 | $4.40 / 1M | Z.AI 官方价，含常见名称变体 |
| `MiniMax-M2`、`M2.1`、`M2.5` | $0.30 / 1M | $0.03 / 1M | $0.375 / 1M | $1.20 / 1M | MiniMax Paygo |
| `MiniMax-M2.7` | $0.30 / 1M | $0.06 / 1M | $0.375 / 1M | $1.20 / 1M | MiniMax Paygo |
| `MiniMax-M2.1`、`M2.5` highspeed | $0.60 / 1M | $0.03 / 1M | $0.375 / 1M | $2.40 / 1M | 输入和输出翻倍，缓存价格不变 |
| `MiniMax-M2.7` highspeed | $0.60 / 1M | $0.06 / 1M | $0.375 / 1M | $2.40 / 1M | 输入和输出翻倍，缓存价格不变 |

来源：

- https://docs.z.ai/guides/overview/pricing.md
- https://platform.minimax.io/docs/guides/pricing-paygo.md

`-highspeed` 是独立计费变体，不能静默归一化到普通模型。`firew2oai-local` 等供应商若在渠道配置中有独立价格，优先使用供应商显式配置，不被原厂默认价覆盖。

## 2026-08-13 收尾复核

采样时间：2026-08-13 19:45（Asia/Shanghai）。本次只对现有正式 PostgreSQL、正式 `/api/pricing`、正式 `/api/status` 和现有 3001 隔离容器做有界核验；没有停止、重启或重建正式 `new-api` 容器。

验证命令与结果：

| 命令 | 退出码 | 结果 |
| --- | ---: | --- |
| `go test -count=1 ./setting/ratio_setting ./setting/billing_setting ./model ./pkg/pricingrepair ./bin` | 0 | 全部通过 |
| `git diff --check -- setting/ratio_setting/model_ratio.go setting/ratio_setting/cache_ratio.go setting/ratio_setting/model_name_normalizer.go setting/ratio_setting/model_ratio_test.go docs/operations/pricing-maintenance.md` | 0 | 无空白错误 |
| 目标模型白名单 pricing-repair dry-run（官方/普通渠道） | 0 | `repaired_logs=0` |
| 同一 dry-run，`channel_id=106` Fireworks 自定义 `minimax-m2p5` | 0 | `repaired_logs=4561`，明确未执行 apply |
| 正式 `http://127.0.0.1:13000/api/status` | 0 | `success=true` |
| 隔离 `http://127.0.0.1:3001/api/status` | 0 | `success=true` |

正式服务只读状态为 `running/healthy`，正式容器镜像仍为 `new-api-local:prod-codex-lite-4527d7666`，本轮未改变其重启计数或启动时间。3001 仅重建现有 `new-api-dev-isolated-new-api-1`，build-info 为当前 dirty 工作区构建。

MiniMax 官方价格页只列出 `M2`、`M2.1`、`M2.5`、`M2.7` 及对应的 `highspeed` 变体，没有 `M2-highspeed`。因此删除了孤立的 `minimax-m2-highspeed` 输出倍率，并阻止通用 `-highspeed` 控制后缀将其静默继承普通 `M2` 价格；已确认的 `M2.1/M2.5/M2.7-highspeed` 仍按官方独立价格计算。渠道 106 的 Fireworks 自定义价格保持原样，不纳入原厂价格回算。

当前仍为零倍率的活跃目录项主要是 xAI 未确认别名、视频/图像能力模型，以及 SenseNova 公测免费模型；没有可核实的海外官方 token 单价时未擅自补价。按次模型继续使用 `ModelPrice`，不套用 token 倍率。

## 2026-08-13 DeepSeek V4 官方峰谷价

采样时间：2026-08-13 20:40（Asia/Shanghai）。DeepSeek 官方价格页和更新日志均已显示 V4 新规则，但尚未到生效时间。新规则自 2026-08-16 16:00 UTC 起生效；峰值时段为 01:00-04:00 UTC 和 06:00-10:00 UTC，其余时间为非峰值，非峰值价格为峰值的一半。

| 模型 | 当前价（生效前） | 非峰值（生效后） | 峰值（生效后） |
| --- | --- | --- | --- |
| `deepseek-v4-flash` | 输入未命中 `$0.14`，缓存命中 `$0.0028`，输出 `$0.28` | 输入未命中 `$0.22`，缓存命中 `$0.007`，输出 `$0.66` | 输入未命中 `$0.44`，缓存命中 `$0.014`，输出 `$1.32` |
| `deepseek-v4-pro` | 输入未命中 `$0.435`，缓存命中 `$0.003625`，输出 `$0.87` | 输入未命中 `$0.66`，缓存命中 `$0.022`，输出 `$1.98` | 输入未命中 `$1.32`，缓存命中 `$0.044`，输出 `$3.96` |

维护方式：`billing_setting.billing_mode` 将两个模型设为 `tiered_expr`，正式兼容表达式用 UTC 月日时判断本次生效边界，再按官方峰谷时段选择价格；表达式系数使用美元每百万 token 的真实价格。源码和新开发镜像已提供 `year()`，正式镜像升级后可将边界收紧为完整日期判断。模型变体（如 `deepseek-ai/deepseek-v4-flash`、`-nothinking`）通过现有模型名归一化使用对应基础表达式；未被官方价格页确认的搜索专用变体不自动继承基础模型价格。生效前的历史日志继续按当前官方价，不提前套用新价；生效后的日志由表达式按请求发生时间自动计费。

来源：

- https://api-docs.deepseek.com/quick_start/pricing
- https://api-docs.deepseek.com/updates

正式库复核：截至本次采样，V4 日志最晚为 2026-08-13 12:11:30 UTC，均早于新价生效时间；`deepseek-v4-flash` 和 `deepseek-v4-pro` 的历史快照分别为当前官方输入、缓存命中和输出价格，未发现需要按新规则回算的历史记录。

## 2026-08-13 DeepSeek 版本别名收尾

DeepSeek 官方文档仍要求 API 请求使用 `deepseek-v4-flash` 和 `deepseek-v4-pro`。官网标注的 `DeepSeek-V4-Flash-0731`、`DeepSeek-V4-Pro-0813` 是当前对应版本标识，不是另一个需要单独报价的 API 模型。正式库和现有 3001 隔离库已为以下官方版本、大小写、厂商前缀以及 thinking/non-thinking 别名补齐当前价格、缓存价格和 `tiered_expr` 表达式：本轮数据库实测覆盖为 `24/24`（含 12 个无厂商前缀和 12 个 `deepseek-ai/` 前缀键）。

`deepseek-v4-pro-search`、`deepseek-v4-pro-search-nothinking` 仍保持零倍率和普通计费模式。DeepSeek 价格页没有为搜索专用 Pro 变体公布独立 token 单价，因此本次没有把基础 Pro 价格静默套用到搜索变体。

验证结果：

```text
go test -count=1 ./setting/ratio_setting ./setting/billing_setting ./pkg/billingexpr ./model ./pkg/pricingrepair ./bin
exit=0
正式 /api/status: success=true
正式容器: running/healthy, RestartCount=0, 启动时间未变化
隔离 3001 /api/status: success=true
```

来源：

- https://api-docs.deepseek.com/quick_start/pricing
- https://api-docs.deepseek.com/updates

## 2026-08-13 xAI Grok 4.5 官方价格复核

xAI 官方模型表当前给出 `grok-4.5` 及其 `grok-4.5-latest`、`grok-build-latest` 别名的价格：200K tokens 以内输入/缓存命中/输出为 `$2/$0.30/$6`，超过 200K 为 `$4/$0.60/$12`。默认计费使用 `billing_setting.billing_mode=grok-4.5:tiered_expr` 和 `len` 阶梯表达式；`grok-build-0.1`、`grok-code-fast-1` 是独立模型，不继承 Grok 4.5 价格。

来源：

- https://docs.x.ai/docs/models

## 2026-08-28 新模型官方价格复核与正式库热更新

采样时间：2026-08-28（Asia/Shanghai）。本轮重新读取供应商官方模型页和价格页，并以正式活跃目录为范围。只维护有明确官方 token 价格、且当前结算链可以准确表达的模型；未确认价格或按秒、按字符计费的模型继续显式未配置。

| 模型 | 官方输入 | 官方缓存命中 | 官方缓存创建 | 官方输出 | 维护倍率 |
| --- | ---: | ---: | ---: | ---: | --- |
| `qwen3.8-max` | ¥12 / 1M | ¥1.5 / 1M | ¥15 / 1M | ¥36 / 1M | `ModelRatio=12/1000*RMB`、`CompletionRatio=36/12`、`CacheRatio=1.5/12`、`CreateCacheRatio=15/12` |
| `qwen3.8-flash`、`qwen-3.8-flash` | ¥0.8 / 1M | ¥0.1 / 1M | ¥1.25 / 1M | ¥2.7 / 1M | `ModelRatio=0.8/1000*RMB`、`CompletionRatio=2.7/0.8`、`CacheRatio=0.1/0.8`、`CreateCacheRatio=1.25/0.8` |
| `glm-5.3` | $1.40 / 1M | $0.26 / 1M | Limited-time Free | $4.40 / 1M | `0.7`、`4.4/1.4`、`0.26/1.4`、`CreateCacheRatio=0` |
| `glm-5.3-flash` | $0.15 / 1M | $0.03 / 1M | Limited-time Free | $0.50 / 1M | `0.15/2`、`0.50/0.15`、`0.03/0.15`、`CreateCacheRatio=0` |
| `step-3.7-flash` | ¥1.35 / 1M | ¥0.27 / 1M | 未公开 | ¥8.10 / 1M | `ModelRatio=1.35/1000*RMB`、`CompletionRatio=8.1/1.35`、`CacheRatio=0.27/1.35` |
| `deepseek-v4-flash-vision-exp` | 非高峰 $0.22 / 1M；峰值 $0.44 / 1M | 非高峰 $0.007 / 1M；峰值 $0.014 / 1M | 未公开 | 非高峰 $0.66 / 1M；峰值 $1.32 / 1M | 与 `deepseek-v4-flash` 共用峰谷 `tiered_expr` |
| `mimo-v2.5` | $0.14 / 1M | $0.0028 / 1M | Limited-time Free | $0.28 / 1M | `0.07`、`2`、`0.0028/0.14`、`CreateCacheRatio=0` |

Qwen 页面同时列出了 Batch File 和显式缓存命中价格，本表只使用普通 API 请求对应的输入、输出、缓存命中和显式缓存创建价格。人民币价格按源码既有 `RMB=500/7.3` 换算到历史倍率口径。`glm-5.3-flash` 按 Z.AI 官方标价 `$0.15/$0.03/$0.50` 维护，不采用当前 50% 限时促销价。GLM-5.3 和 GLM-5.3-Flash 的缓存存储目前标记为 Limited-time Free，官方未公布对应原价，因此显式写入 `CreateCacheRatio=0`，不使用未知价格的默认值。

DeepSeek 官方价格页将 `deepseek-v4-flash-vision-exp` 与 V4 Flash 放在同一价格表，并说明图片按 token 转换后与文本输入合并计费。峰值时段为周一至周五 UTC 01:00-04:00 和 06:00-10:00，其余时段为非高峰。源码表达式使用完整日期和 `weekday`；正式 13000 服务仍运行不支持 `year()` 的旧镜像，因此正式 options 使用等价的 `month/day + weekday` 表达式，未重启容器。

正式活跃目录中的 `qwen-3.8-27b`、`Qwen3.8-27B`、`agnes-2.5-pro-beta`、`sensenova-u1.5-lite`、`stepaudio-2.5-*`、`step-explore`、`step-router-v1`、`step-image-edit-2` 以及视频、音频和按次模型没有本轮可确认且能被当前 token 结算链准确表达的独立价格，保持未配置；不因名称相近继承其他模型价格。按秒、按小时、按字符或按图片计费的模型必须等对应计量字段接入后再维护。

来源：

- https://www.qianwenai.com/models/qwen3.8-max
- https://www.qianwenai.com/models/qwen3.8-flash
- https://docs.z.ai/guides/overview/pricing.md
- https://platform.stepfun.com/docs/zh/guides/pricing/details
- https://api-docs.deepseek.com/quick_start/pricing
- https://mimo.mi.com/static/docs/price/pay-as-you-go.md

## 2026-08-16 xAI 活跃目录最终排除清单

采样时间：2026-08-16（Asia/Shanghai）。重新读取 xAI 当前官方模型目录后，语言模型公开条目为 `grok-4.3`、`grok-4.5`、`grok-4.6`、`grok-4.20` 系列及 `grok-build-0.1`；`grok-4.5-latest`、`grok-build-latest`、`grok-code-fast-1` 等官方别名已按对应基础模型维护。当前正式活跃目录中的 Composer、通用 `grok`、`grok-3-mini`、`grok-build` 及其第三方路径别名不在官方当前语言模型表中，没有可核实的独立 token 单价，继续保持未配置。

官方视频模型 `grok-imagine-video` 和 `grok-imagine-video-1.5`（含 preview/date 别名）按分辨率和秒数计费：前者为 480p `$0.50/s`、720p `$0.70/s`，后者为 480p `$0.80/s`、720p `$1.40/s`、1080p `$2.50/s`。当前 `relay/channel/xai` 只实现文本、Responses 和图像生成适配，没有视频时长及分辨率计量入口，不能写入固定 `ModelPrice`；待计费契约支持时再接入。

来源：

- https://docs.x.ai/developers/models

## 2026-08-16 `gpt-image-2-all` 变体归一化

采样时间：2026-08-16（Asia/Shanghai）。OpenAI 官方价格页的 Grouped Pricing Table 只列出 `gpt-image-2`，没有 `gpt-image-2-all` 独立价格。当前正式渠道 208 将 `gpt-image-2-all` 作为路由变体提供，因此按最接近的官方基础模型归一到 `gpt-image-2`：文本输入 `$5/1M`、图像输入 `$8/1M`、缓存输入 `$1.25/1M`，输出图像 `$30/1M`。该变体沿用 `ModelRatio=2.5`、`CompletionRatio=6`、`CacheRatio=0.25` 和 `ImageRatio=1.6`，不创建独立的按次 `ModelPrice`。

来源：

- https://platform.openai.com/docs/pricing

## 2026-08-16 SiliconFlow 活跃模型价格复核

采样时间：2026-08-16（Asia/Shanghai）。SiliconFlow 官方价格页 HTML 标记发布时间为 2026-08-12 09:58 UTC，本轮重新读取价格页和对应模型页，按 `api.siliconflow.cn` 渠道的完整模型路径补齐或校正价格。供应商路径使用独立键，避免覆盖 Fireworks、直连 DeepSeek 或其他渠道的同名价格。

已维护的 token 模型价格（美元 / 1M tokens）：

| 模型组 | 输入 | 缓存命中 | 输出 |
| --- | ---: | ---: | ---: |
| `DeepSeek-V3` | 0.25 | 未单列 | 1.00 |
| `DeepSeek-V3.1-Terminus` | 0.27 | 未单列 | 1.00 |
| `DeepSeek-V3.2` | 0.27 | 0.135 | 0.42 |
| `DeepSeek-R1` | 0.50 | 未单列 | 2.18 |
| `Qwen3.6-35B-A3B` | 0.20 | 未单列 | 1.60 |
| `Qwen3.5-9B/27B/35B-A3B/122B-A10B/397B-A17B` | 0.10/0.25/0.24/0.26/0.39 | 未单列 | 0.15/2.00/1.80/2.08/2.34 |
| `Qwen3-VL-8B-Instruct` | 0.18 | 未单列 | 0.68 |
| `Qwen3-VL-30B-A3B-*` | 0.29 | 未单列 | 1.00 |
| `Qwen3-VL-32B-Instruct/Thinking` | 0.20 | 未单列 | 0.60/1.50 |
| `Qwen3-Coder-30B/480B` | 0.07/0.25 | 未单列 | 0.28/1.00 |
| `Qwen3-8B/14B/32B` | 0.06/0.07/0.14 | 未单列 | 0.06/0.28/0.57 |
| `Qwen3-30B-A3B-2507`（Instruct/Thinking） | 0.09 | 未单列 | 0.30 |
| `Qwen2.5-7B/72B-Instruct` | 0.05/0.59 | 未单列 | 0.05/0.59 |
| `Qwen3-Embedding-0.6B/4B/8B` | 0.01/0.02/0.04 | 不适用 | 无输出 token |
| `Qwen3-Reranker-0.6B/4B/8B` | 0.01/0.02/0.04 | 不适用 | 无输出 token |
| `ERNIE-4.5-300B-A47B` | 0.28 | 未单列 | 1.10 |
| `Hunyuan-A13B-Instruct` | 0.14 | 未单列 | 0.57 |
| `Step-3.5-Flash` | 0.10 | 未单列 | 0.30 |
| `Ling-flash-2.0` | 0.14 | 未单列 | 0.57 |
| `Seed-OSS-36B-Instruct` | 0.21 | 未单列 | 0.57 |

按次价格：`Qwen-Image` `$0.02 / image`、`Qwen-Image-Edit` 与 `Qwen-Image-Edit-2509` `$0.04 / image`，`Wan2.2-I2V-A14B` 与 `Wan2.2-T2V-A14B` `$0.29 / video`。这些条目写入 `ModelPrice`，不混入 token 倍率。

官方页面只公开了当前可确认的单位和价格。`CosyVoice2-0.5B`、`IndexTTS-2` 按 UTF-8 字节计费，`TeleSpeechASR`、`SenseVoiceSmall` 等按音频分钟或字符计费，当前结算链没有对应计量字段，因此继续显式未配置。BGE、BCE、PaddleOCR、Kolors、MOSS-TTSD、DeepSeek OCR、定制 LoRA/Pro、Qwen3 Omni/VL 大参数变体和 DeepSeek 搜索变体没有当前可确认的本模型价格，也未使用比较页价格静默继承。

来源：

- https://www.siliconflow.com/pricing
- https://www.siliconflow.com/models
- 具体模型页由官方价格页链接的 `/models/<slug>` 页面提供；`Qwen3-Reranker-4B` 页面标记 Deprecated，维护的是页面 RSC 中仍可见的历史 `$0.02 / 1M tokens`，不将其扩展到 VL Reranker。

## 2026-08-15 正式库变体价格与历史日志修正落地

采样时间：2026-08-15 20:10（Asia/Shanghai）。本轮只在正式 PostgreSQL 和现有 3001 隔离 PostgreSQL 中合并明确的模型键，没有覆盖整张 options 表，也没有改动 Fireworks 渠道的独立价格。

已写入或校正：`gpt-5.6-sol-wm`、`moonshotai/kimi-k2.6`、`openai/gpt-oss-20b`、`gpt-oss-20b`、`google/gemma-4-31b-it`、`google/gemma-4-26b-a4b-it`，以及 `grok-imagine`、`grok-imagine-1`、`grok-imagine-edit` 的 `ModelPrice=0.02` 路径别名。`gpt-oss-20b` 基础键的缓存倍率最终校正为 `0.5`，正式和 3001 的 `/api/pricing` 热加载复核均已生效。

历史日志修正器使用 `channel_id=0` 加完整模型白名单，dry-run 和 apply 均只命中 4 条日志：

| 日志 ID | 模型 | 旧 quota | 新 quota |
| ---: | --- | ---: | ---: |
| 314105、447391 | `google/gemma-4-31b-it` | 900、1125 | 0、0 |
| 447666 | `moonshotai/kimi-k2.6` | 1050 | 36 |
| 2068248 | `gpt-5.6-sol-wm` | 178313 | 3248 |

合计旧 `181388`，新 `3284`，差额 `-178104`。事务同步更新了 `logs`、`users`、`tokens`、`channels` 和对应 `quota_data` 小时聚合，并清理了受影响的 Redis 用户/令牌缓存。修正后 4 条日志和 4 条小时聚合分别为 `0、0、36、3248`。

验证结果：

```text
go test -count=1 -timeout=180s ./setting/ratio_setting ./pkg/pricingrepair ./bin: exit=0
git diff --check（目标源码、修正器和本记录）: exit=0
正式 http://127.0.0.1:13000/api/status: success=true
隔离 http://127.0.0.1:3001/api/status: success=true
正式/隔离 /api/pricing: gpt-oss-20b CacheRatio=0.5，Kimi K2.6 和 GPT-5.6 Sol 倍率正确
正式容器: running/healthy，RestartCount=1，启动时间未变化
修正器临时二进制: 已从宿主机和正式容器移除
```

`gpt-oss-20b` 旧缓存倍率的历史日志未批量回算：Fireworks 页面没有提供明确的价格生效时间，当前保持历史结算不变。xAI 视频模型仍不写固定 `ModelPrice`，因为官方按秒/分辨率计费而当前链路没有时长字段。

## 2026-08-15 变体价格收尾复核

本轮重新读取供应商海外官方页面，并只补齐能够明确映射到官方基础模型的活跃变体：

| 模型或变体 | 官方价格 | 维护方式 |
| --- | --- | --- |
| `kimi-k2.6` | 输入 `$0.95`、缓存命中 `$0.16`、输出 `$4.00` / 1M tokens | 标准倍率；`moonshotai/` 路径别名显式写入数据库 |
| `gpt-oss-20b`（Fireworks Serverless） | 输入 `$0.07`、缓存命中 `$0.035`、输出 `$0.30` / 1M tokens | `CacheRatio=0.5`；旧的 `4/7` 已修正 |
| `gemma-4-31b-it`、`gemma-4-26b-a4b-it` | 免费层输入、输出和缓存均免费；付费层为 `Not available` | 显式零倍率，不能按其他 Gemma 或 Gemini 模型继承价格 |
| `grok-imagine`、`grok-imagine-1`、`grok-imagine-edit` | 按最接近的 `grok-imagine-image` 基础输出价 `$0.02` / image | `ModelPrice=0.02`；路径别名同步写入 |

`gpt-5.6-sol-wm` 是渠道路由后缀，按同一渠道已确认的 `gpt-5.6-sol` 价格归一化；归一化只在 GPT-5.6 专属分支处理 `-wm`，避免影响其他 GPT 型号。xAI 官方图片页还列出 `grok-imagine-image-2.0`（起始 `$0.04` / image）和 `grok-imagine-image-quality`（起始 `$0.05` / image），继续按现有独立 `ModelPrice` 维护。

本轮不为 `grok-imagine-video`、`grok-imagine-video-1.5` 及 preview/path 变体写固定单价。官方按秒和分辨率计费，而当前任务结算链没有视频时长字段；写入固定 `ModelPrice` 会把整段视频错误计成一次调用。待视频适配器提供时长和分辨率计费字段后再接入。

来源：

- https://platform.kimi.ai/docs/pricing/chat-k26
- https://fireworks.ai/models/fireworks/gpt-oss-20b
- https://ai.google.dev/gemini-api/docs/pricing
- https://ai.google.dev/gemma/docs/core/gemma_on_gemini_api
- https://docs.x.ai/developers/pricing
- https://docs.x.ai/developers/models/grok-imagine-image

## 2026-08-15 Agnes 2.5 Pro 稳定版价格补齐

Agnes 官方模型页已发布商业稳定版 `agnes-2.5-pro`，明确区别于基准榜单中的 `agnes-2.5-pro-alpha`。官方按 token 计费：缓存命中 `$0.0038 / 1M`、缓存未命中输入 `$0.45 / 1M`、输出 `$0.90 / 1M`。因此两者的标准倍率分别为 `ModelRatio=0.225`、`CompletionRatio=2`、`CacheRatio=0.0038/0.45`；稳定版和 Alpha 仍使用独立配置键，不能依赖名称归一化或隐式继承。

正式库渠道 211 已启用 `agnes-2.5-pro`，但截至本次采样没有该模型的成功消费日志，因此无需历史日志回算；正式库与现有 3001 隔离库均已写入三项标准倍率。图片和视频模型继续使用各自的 `ModelPrice` 按次计费，不套用本次 token 价格。

来源：

- https://agnes-ai.com/doc/agnes-25-pro.md

## 2026-08-14 Gemini 官方模型与价格复核

采样时间：2026-08-14（Asia/Shanghai）。本轮重新读取 Google AI for Developers 官方模型页和价格页，按海外 Gemini Developer API 标准付费档维护已接入模型及其 preview、stable、latest、厂商路径和控制后缀变体。固定 token 价格使用标准倍率；上下文长度或多模态价格不同的模型使用 `billing_setting.billing_expr`；Veo 使用 `ModelPrice` 的 720p 每秒基准，并由任务计费按分辨率调整。

| 模型组 | 官方输入价格 | 官方缓存命中 | 官方输出价格 | 维护方式 |
| --- | ---: | ---: | ---: | --- |
| `gemini-3.7-flash`、`gemini-3.6-flash` | $1.50 / 1M | $0.15 / 1M | $7.50 / 1M | 标准倍率；排除官方 2026 年底前限时优惠 |
| `gemini-3.5-flash` | $1.50 / 1M | $0.15 / 1M | $9.00 / 1M | 标准倍率 |
| `gemini-3.5-flash-lite` | $0.30 / 1M | $0.03 / 1M | $2.50 / 1M | 标准倍率 |
| `gemini-3.1-flash-lite` | 文本/图像/视频 $0.25 / 1M；音频 $0.50 / 1M | $0.025 / $0.05 / 1M | $1.50 / 1M | `tiered_expr`；统一缓存变量按普通缓存价格，音频缓存拆分边界见下文 |
| `gemini-omni-flash-preview` | $1.50 / 1M | 未单列 | 文本 $9.00 / 1M；视频 $17.50 / 1M | `tiered_expr`，图像/视频输出使用 `img_o` |
| `gemini-3.1-pro-preview` | $2 / 1M（<=200K）；$4 / 1M（>200K） | $0.20 / $0.40 / 1M | $12 / $18 / 1M | `len` 阶梯表达式 |
| `gemini-3.1-flash-live-preview` | 文本 $0.75、图像/视频 $1、音频 $3 / 1M | 未单列 | 文本 $4.50、音频 $12 / 1M | `tiered_expr` |
| `gemini-3.5-live-translate-preview` | 音频 $3.50 / 1M | 未单列 | 音频 $21 / 1M | `tiered_expr` |
| `gemini-3.1-flash-image` | $0.50 / 1M | 未单列 | 文本 $3、图像 $60 / 1M | `tiered_expr` |
| `gemini-3.1-flash-lite-image` | $0.25 / 1M | 未单列 | 文本 $1.50、图像 $30 / 1M | `tiered_expr` |
| `gemini-3-pro-image` | $2 / 1M | 未单列 | 文本 $12、图像 $120 / 1M | `tiered_expr` |
| `gemini-3.1-flash-tts-preview` | 文本 $1 / 1M | 未单列 | 音频 $20 / 1M | `tiered_expr` |
| `gemini-robotics-er-1.6-preview` | 普通 $1、音频 $2 / 1M | 未单列 | $5 / 1M | `tiered_expr` |
| `gemini-robotics-er-2-preview`、`-streaming-preview` | $2 / 1M | ER 2 $0.20 / 1M | $10 / 1M | `tiered_expr` |
| `gemini-embedding-2` | 文本 $0.20、图像 $0.45、音频 $6.50、视频 $12 / 1M | 未单列 | 不适用 | 当前链路只稳定读取文本 token |
| `veo-3.1` Standard / Fast / Lite | 720p $0.40 / $0.10 / $0.05 每秒 | 不适用 | 按视频生成计费 | `ModelPrice` 基准；1080p 为 $0.40 / $0.12 / $0.08，4K 为 $0.60 / $0.30，Lite 不支持 4K |

来源：

- https://ai.google.dev/gemini-api/docs/models
- https://ai.google.dev/gemini-api/docs/pricing

模型变体由 `setting/ratio_setting/model_name_normalizer.go` 归一化到最接近的官方计费键，包含 preview、latest、stable、大小写、厂商路径以及 thinking、nothinking、search 等控制后缀；未被官方价格页确认的专用变体不会因名称相似而静默继承价格。`Gemini 3.1 Flash-Lite` 的音频缓存价格与文本缓存价格不同，但现有统一 `cr` 变量无法按模态拆分，因此表达式按普通缓存价格计费，音频缓存差异属于已记录的精度边界。Embedding 2 的图片、音频和视频 token 在当前 Gemini embedding 响应适配链中没有稳定的独立 usage 字段，不能宣称已经精确结算。

## 2026-08-14 xAI 长上下文模型补充

再次读取 xAI 官方模型表后，确认以下语言模型均有 `longContextThreshold=200000`，此前只有基础倍率，长上下文请求会少计费。本轮补齐 `tiered_expr`：

| 模型组 | 200K 以内（输入/缓存命中/输出） | 超过 200K（输入/缓存命中/输出） |
| --- | ---: | ---: |
| `grok-4.3`、`grok-4.20` | `$1.25/$0.20/$2.50` | `$2.50/$0.40/$5.00` |
| `grok-4.6` | `$2.00/$0.50/$6.00` | `$4.00/$1.00/$12.00` |
| `grok-build-0.1`、官方别名 `grok-code-fast-1` | `$1.00/$0.20/$2.00` | `$2.00/$0.40/$4.00` |

表达式使用 `len <= 200000`，边界 token 归入标准档。正式库和 3001 仅为活跃目录中的已确认模型名及厂商路径别名写入阶梯配置；`grok-build`、Composer、搜索专用、视频和语音模型不因名称相似而静默继承 token 价。图像模型仍使用已有的按张 `ModelPrice`。

当前 xAI 活跃目录中的 `grok-imagine-video`、`grok-imagine-video-1.5` 及其 preview/path 变体，官方按分辨率和秒数计费：`grok-imagine-video` 的 480p/720p 分别为 `$0.50/s`、`$0.70/s`，`grok-imagine-video-1.5` 的 480p/720p/1080p 分别为 `$0.80/s`、`$1.40/s`、`$2.50/s`。本仓库的 xAI 适配器目前只实现文本/Responses 与图片生成，没有视频任务的时长、分辨率解析和 `OtherRatios` 计费入口。不能把每秒价格直接写入按次 `ModelPrice`，否则会把视频时长计费为单次固定价；这些模型保持显式未配置，等待视频适配器和计费契约一并接入。Composer、通用 `grok` 和 `grok-3-mini` 也没有当前官方独立 token 价，继续保持 fail-closed。

来源：

- https://docs.x.ai/docs/models

## 2026-08-16 可确认价格收尾

采样时间：2026-08-16 05:11（Asia/Shanghai，21:11 UTC）。本轮重新检查正式库、现有 3001 隔离库和 xAI 官方价格页，只补充有明确官方价格且能被当前计费链准确表达的条目。

已补齐：

- 修正 `grok-4-5` 的历史缓存倍率残留值 `0.25` 为官方 `0.30 / 2.00 = 0.15`；`grok-4.5` 本体及阶梯表达式不变。
- 为 xAI 图像模型加入官方日期和路径别名：`grok-imagine-image-2026-03-02`（`$0.02 / image`）、`grok-imagine-image-quality-20260403`、`grok-imagine-image-quality-latest`、`grok-imagine-image-pro`（均 `$0.05 / image`），并同步 `grok/`、`xai/`、`x-ai/` 路径键。
- 为现有 3001 数据库补入 `gpt-5.3` 的官方倍率：`ModelRatio=0.875`、`CompletionRatio=8`、`CacheRatio=0.1`。

正式库当前未配置价格的活跃项仍只有 xAI Composer、通用 Grok、Grok 3 Mini 以及视频模型路径。Composer、通用 Grok 和 Grok 3 Mini 没有可确认的独立官方 token 单价；视频官方按秒和分辨率计费，而当前链路没有时长字段，因此均保持显式未配置，避免错误套用 token 或按次价格。3001 另有本地、自托管、免费路由和音视频任务模型，未发现可用海外原厂 token 单价的条目，保持渠道或免费策略。

DeepSeek V4 新峰谷价截至本次采样尚未到 `2026-08-16 16:00 UTC` 生效时间，本轮未提前改写当前价或历史日志。

针对本轮变更执行的历史日志 dry-run 命中 `77` 条：`76` 条 `grok-4.5` 和 `1` 条 `grok-4-5` 的旧 ratio 快照，全部发生在 2026-07-09。旧快照使用 `CacheRatio=0.25`，而当前官方价为 `0.30 / 2.00 = 0.15`；xAI 官方页面没有公布这次缓存价调整的生效时间，因此本轮不 apply，保留历史账单的当时价格口径。该 dry-run 没有写入 `logs`、用户、令牌、渠道或 `quota_data`。

## 2026-08-14 DeepSeek 生效边界收紧

再次采样 DeepSeek 官方价格页和 V4 Pro 发布公告，规则未变：新价于 2026-08-16 16:00 UTC 生效，峰值时段为 01:00-04:00、06:00-10:00 UTC，其余时段为峰值价格的一半。源码表达式已改用 `year("UTC")`、`month("UTC")`、`day("UTC")` 和 `hour("UTC")` 共同判断完整的 2026-08-16 边界，避免后续年份在 8 月重复落入生效前分支。现有 3001 隔离库已同步该完整日期表达式；正式库继续保留旧正式二进制可解析的月日兼容表达式，行为在本次生效日前后一致，正式服务未停止、未重启、未重建。

本次不修正历史日志：截至采样时新价尚未生效，已有 V4 日志均按官网当前价记录；提前按未来峰谷价回算会改变已结算金额。

来源：

- https://api-docs.deepseek.com/quick_start/pricing
- https://api-docs.deepseek.com/news/news260813

本轮将源码默认 `grok-4.5` 缓存倍率从 `0.25` 修正为 `0.15`（`$0.30/$2`），并把正式库原 `$0.50/$2` 的阶梯表达式更新为 `$0.30/$2`；正式库和 3001 的新请求均由热加载配置使用新表达式。xAI 当前页面没有给出 `$0.50` 到 `$0.30` 的历史生效时间，因此未按当前价批量回算无法确定边界的旧普通日志；其中失败日志 token 为零，不产生修正金额。

本轮进一步从 xAI 官方模型数据确认 `longContextThreshold=200000`，并确认 `grok-4.5-latest`、`grok-build-latest` 是 `grok-4.5` 的官方别名。正式库和 3001 隔离库已将以下带厂商路径的等价别名显式写入 `billing_setting.billing_mode` 与 `billing_setting.billing_expr`，使旧版正式二进制也能直接命中阶梯计费：`grok-4.5-latest`、`grok-build-latest`、`grok/grok-4.5`、`grok/grok-4.5-latest`、`grok/grok-build-latest`、`xai/grok-4.5`、`xai/grok-4.5-latest`、`xai/grok-build-latest`、`x-ai/grok-4.5`、`x-ai/grok-4.5-latest`、`x-ai/grok-build-latest`。`grok-4.5` 的 200,000 token 边界按官方数据使用 `len <= 200000`，不把 `grok-build-0.1` 或 `grok-code-fast-1` 静默归并到 Grok 4.5。

## 2026-08-29 原价口径复核

本轮再次核对供应商官方页面，统一采用普通 API 标准原价，不采用 Batch、限时或其他促销价。xAI Grok 价格和历史路径别名修正证据见 `docs/reviews/CR-HISTORICAL-PRICING-REPAIR-2026-08-29.md`。另发现 Gemini 3.6/3.7 Flash 的现有 `$0.75/$0.075/$3.75` 是 Google 官方标注的 2026 年底前限时价，已改为标准 `$1.50/$0.15/$7.50`（输入/缓存命中/输出，每 1M tokens），并同步正式库和 3001 隔离库；正式库无这两个模型历史消费日志。

本次收尾又发现旧数据库未显式覆盖的 xAI 官方等价键，已在正式库和现有 3001 隔离库事务内补齐：`grok-latest`、`grok/grok-latest`、`xai/grok-latest`、`x-ai/grok-latest` 使用 `grok-4.3` 的表达式；`grok/grok-build-0.1`、`xai/grok-build-0.1`、`x-ai/grok-build-0.1` 使用 `grok-build-0.1` 的表达式。当前活跃目录中仍显示为零价的项目是未确认官方 token 价的 Composer、视频/搜索专用模型、通用 `grok` 别名和 SenseNova 公测免费模型；Agnes 2.5 Pro 的价格已在 2026-08-15 收尾补齐。

来源：

- https://docs.x.ai/docs/models

## 2026-08-29 全量历史普通 token 日志修正

采样时间：2026-08-29（Asia/Shanghai）。本轮按当前已确认的官方价格，对正式库全部 `type=2`、普通 token 钱包日志执行分片 dry-run 和 apply；分片按 `logs.id` 的 25 万条区间进行，避免一次装载约 200 万条日志导致内存耗尽。

修正范围遵循本规程的安全条件：`other.model_price < 0`、存在完整价格快照、非订阅、非 `tiered_expr`、非按次/媒体/搜索附加计费。`glm-5.3-flash` 当前原价配置已生效，但正式库没有该模型历史消费日志，命中 `0` 条。

| 项目 | 数值 |
| --- | ---: |
| 修正日志 | 6,728 |
| 旧 quota 合计 | 30,576,174 |
| 新 quota 合计 | 30,380,902 |
| quota 差额 | -195,272 |

涉及模型：`minimax-m2p5`、`mimo-v2.5`、`mimo-v2.5-free`、`step-3.7-flash`、`kimi-k2.7-code`、`qwen3.8-max` 和少量 `gpt-oss-20b`。每个非空分片均在事务内同步 `logs`、`users`、`tokens`、`channels` 和受影响的 `quota_data`，并通过正式服务容器的 Redis 配置清理用户/令牌缓存。

正式库备查表：`pricing_repair_20260829_all_logs`（12,151 条）、`pricing_repair_20260829_all_users`（1 条）、`pricing_repair_20260829_all_tokens`（2 条）、`pricing_repair_20260829_all_channels`（6 条）、`pricing_repair_20260829_all_quota_data`（174 条）。修正后全部 ID 分片 dry-run 均为 `repaired_logs=0`，受影响小时聚合复核为 `0` 个不一致。正式服务持续运行期间产生的实时请求不属于本次历史 ID 范围，余额总差额需与实时请求分开解释。

本轮同时为修正器增加 `-min-id` 和 `-max-id` 分片边界，并改为比较规范化 JSON，避免仅因历史 `other` 键顺序变化而产生无意义写入。`tiered_expr`、订阅、按次、图片、音频、搜索及无可确认 token 单价的历史记录仍保持原快照，不能用标准倍率公式强行回算。
