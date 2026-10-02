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

### 未维护价格模型的兼容兜底

本项目要求未及时维护价格的模型仍可调用。正式服务通过 `SelfUseModeEnabled=true` 启用统一兜底：未匹配到 `ModelRatio` 的 token 模型按下列口径计费，其中只有 `ModelRatio` 是固定值，`CompletionRatio` 不是固定值。

| 项 | 兜底值 | 代码位置 |
| --- | --- | --- |
| `ModelRatio` | 固定 `37.5` | `setting/ratio_setting/model_ratio.go` 的 `GetModelRatio` |
| `CompletionRatio` | 按模型名前缀返回，不是固定值：`gpt-5.x` 为 `6`、无点号 `gpt-5*` 为 `8`、`gpt-4o` 为 `4`、`gpt-4`/`gpt-4.5-preview` 为 `2`、`o1`/`o3` 前缀为 `4`、`claude-3`/`claude-sonnet-4`/`claude-opus-4`/`claude-haiku-4` 为 `5`，其余走各自分支 | `setting/ratio_setting/model_ratio.go` 的 `getHardcodedCompletionModelRatio` |
| `CacheRatio` | 固定 `1` | `setting/ratio_setting/cache_ratio.go` 的 `GetCacheRatio` |
| `CreateCacheRatio` | 固定 `1.25` | `setting/ratio_setting/cache_ratio.go` 的 `GetCreateCacheRatio` |

这些是保护性临时倍率，不代表供应商官方价格。由于 `CompletionRatio` 会随模型名变化，核对未维护模型的费用时必须读取实际生效值，不能按固定倍率估算；补齐官方价格后必须覆盖对应模型键，并按实际价格复核历史日志边界。

已明确为免费层的模型必须显式维护为 `0/0/0/0`，依次对应 `ModelRatio`、`CompletionRatio`、`CacheRatio` 和 `CreateCacheRatio`，不能因为启用兜底而使用保护性倍率。按秒、按字符、按图片或按视频时长计费且当前链路缺少对应计量字段的模型，可以继续使用兜底，但必须在审计记录中标明其价格尚未确认。

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

### StepAudio 2.5 ASR 的小时价格换算

StepFun 官方将 `stepaudio-2.5-asr` 按 `¥0.15 / 小时` 计费。当前转写预估逻辑将音频时长归一化为每分钟 1,000 个音频计费 token，因此等价于 `¥2.5 / 1M tokens`，维护为 `ModelRatio=2.5/1000*RMB`、`CompletionRatio=0`、`AudioRatio=1`、`AudioCompletionRatio=0`。标准 `/audio/speech` 的 `stepaudio-2.5-tts` 按 `¥5.8 / 万字符`，现有音频链路可按输入文本单位维护为 `ModelRatio=5.8/10*RMB`、`CompletionRatio=0`、`AudioCompletionRatio=0`；官方另计的音色复刻费用不属于标准合成请求，仍不纳入。

StepFun 当前定价页同时明确列出 `stepaudio-2.5-chat` 的输入/缓存命中/输出价格为 `¥10/¥2/¥25`，`stepaudio-2.5-realtime` 为 `¥10/¥2/¥70`（均按 1M tokens）。这两个模型按标准倍率维护；不能再把整个 `stepaudio-2.5-*` 系列一概标记为未确认价格。

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
| `minimax-m3` | $0.30 / 1M, 512K 以内; $0.60 / 1M, 超过 512K | $0.06 / 1M, 512K 以内; $0.12 / 1M, 超过 512K | $1.20 / 1M, 512K 以内; $2.40 / 1M, 超过 512K | `billing_setting` 阶梯表达式，采用永久降价后的现价 |

来源：

- https://platform.kimi.ai/docs/pricing/chat-k26.md
- https://ai.google.dev/gemini-api/docs/pricing
- https://platform.minimax.io/docs/guides/pricing-paygo.md

Kimi K2.6 对应标准倍率为 `ModelRatio=0.475`、`CompletionRatio=4/0.95`、`CacheRatio=0.16/0.95`。Gemini 3.1 Pro Preview 和 MiniMax M3 的长上下文价差不能用固定倍率表示，必须使用 `len` 条件的阶梯表达式。MiniMax 官方页面明确将当前显示价格标记为 `Permanent 50% off`，因此本项目采用永久折后现价；优先级档为短上下文 `$0.45/$0.09/$1.80`，长上下文 `$0.90/$0.18/$3.60`（输入/缓存命中/输出）。

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
| `qwen3.8-max` | ¥12 / 1M | ¥1.5 / 1M | ¥15 / 1M | ¥36 / 1M | `ModelRatio=12.0/1000*RMB`、`CompletionRatio=36/12`、`CacheRatio=1.5/12`、`CreateCacheRatio=15/12` |
| `qwen3.8-flash`、`qwen-3.8-flash` | ¥0.8 / 1M | ¥0.1 / 1M | ¥1.25 / 1M | ¥2.7 / 1M | `ModelRatio=0.8/1000*RMB`、`CompletionRatio=2.7/0.8`、`CacheRatio=0.1/0.8`、`CreateCacheRatio=1.25/0.8` |
| `glm-5.3` | $1.40 / 1M | $0.26 / 1M | Limited-time Free | $4.40 / 1M | `0.7`、`4.4/1.4`、`0.26/1.4`、`CreateCacheRatio=0` |
| `glm-5.3-flash` | $0.15 / 1M | $0.03 / 1M | Limited-time Free | $0.50 / 1M | `0.15/2`、`0.50/0.15`、`0.03/0.15`、`CreateCacheRatio=0` |
| `step-3.7-flash` | ¥1.35 / 1M | ¥0.27 / 1M | 未公开 | ¥8.10 / 1M | `ModelRatio=1.35/1000*RMB`、`CompletionRatio=8.1/1.35`、`CacheRatio=0.27/1.35` |
| `deepseek-v4-flash-vision-exp` | 非高峰 $0.22 / 1M；峰值 $0.44 / 1M | 非高峰 $0.007 / 1M；峰值 $0.014 / 1M | 未公开 | 非高峰 $0.66 / 1M；峰值 $1.32 / 1M | 与 `deepseek-v4-flash` 共用峰谷 `tiered_expr` |
| `mimo-v2.5` | $0.14 / 1M | $0.0028 / 1M | Limited-time Free | $0.28 / 1M | `0.07`、`2`、`0.0028/0.14`、`CreateCacheRatio=0` |

Qwen 页面同时列出了 Batch File 和显式缓存命中价格，本表只使用普通 API 请求对应的输入、输出、缓存命中和显式缓存创建价格。人民币价格按源码既有 `RMB=500/7.3` 换算到历史倍率口径。`glm-5.3-flash` 按 Z.AI 官方标价 `$0.15/$0.03/$0.50` 维护，不采用当前 50% 限时促销价。GLM-5.3 和 GLM-5.3-Flash 的缓存存储目前标记为 Limited-time Free，官方未公布对应原价，因此显式写入 `CreateCacheRatio=0`，不使用未知价格的默认值。

DeepSeek 官方价格页将 `deepseek-v4-flash-vision-exp` 与 V4 Flash 放在同一价格表，并说明图片按 token 转换后与文本输入合并计费。峰值时段为周一至周五 UTC 01:00-04:00 和 06:00-10:00，其余时段为非高峰。源码表达式使用完整日期和 `weekday`；正式 13000 服务仍运行不支持 `year()` 的旧镜像，因此正式 options 使用等价的 `month/day + weekday` 表达式，未重启容器。

正式活跃目录中的 `step-explore` 已由 StepFun 官方模型页明确说明按 `step-3.7-flash` 同价计费，不能列入未确认项；`stepaudio-2.5-chat`、`stepaudio-2.5-realtime`、`stepaudio-2.5-asr` 和 `stepaudio-2.5-tts` 也已有官方价格并按现有 token/音频计量逻辑维护。当前仍保持未配置的是 `sensenova-u1.5-lite`、`step-router-v1`、按分辨率或视频秒数计费且结算链无法准确表达的模型，以及无可确认独立原价的第三方或自托管路由；不因名称相近继承其他模型价格。`qwen-3.8-27b`、`Qwen3.8-27B` 和 `agnes-2.5-pro-beta` 已在 2026-08-29 全量补齐复核中按官方原价写入。按秒、按字符或按图片计费的模型必须等对应计量字段接入后再维护。

来源：

- https://www.qianwenai.com/models/qwen3.8-max
- https://www.qianwenai.com/models/qwen3.8-flash
- https://docs.z.ai/guides/overview/pricing.md
- https://platform.stepfun.com/docs/zh/guides/pricing/details
- https://api-docs.deepseek.com/quick_start/pricing
- https://mimo.mi.com/static/docs/price/pay-as-you-go.md

## 2026-08-16 xAI 活跃目录最终排除清单

采样时间：2026-08-16（Asia/Shanghai）。重新读取 xAI 当前官方模型目录后，语言模型公开条目为 `grok-4.3`、`grok-4.5`、`grok-4.6`、`grok-4.20` 系列及 `grok-build-0.1`；`grok-4.5-latest`、`grok-build-latest`、`grok-code-fast-1` 等官方别名已按对应基础模型维护。该日期快照中的 Composer、通用 `grok`、`grok-3-mini`、`grok-build` 及其第三方路径别名尚未确认独立 token 单价；Grok 3 Mini 的结论已由 2026-08-30 的 xAI 官方 `llms.txt` 复核和显式价格键补齐记录 supersede。

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

Agnes 官方模型页已发布商业稳定版 `agnes-2.5-pro`，明确区别于基准榜单中的 `agnes-2.5-pro-alpha`。官方按 token 计费：缓存命中 `$0.045 / 1M`、缓存未命中输入 `$0.45 / 1M`、输出 `$0.90 / 1M`。因此两者的标准倍率分别为 `ModelRatio=0.225`、`CompletionRatio=2`、`CacheRatio=0.1`；稳定版和 Alpha 仍使用独立配置键，不能依赖名称归一化或隐式继承。

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

模型变体由 `setting/ratio_setting/model_name_normalizer.go` 归一化到最接近的官方计费键，包含 preview、latest、stable、大小写、厂商路径以及 thinking、nothinking、search 等控制后缀；未被官方价格页确认的专用变体不会因名称相似而静默继承价格。`Gemini 3.1 Flash-Lite` 的音频缓存价格与文本缓存价格不同，但现有统一 `cr` 变量无法按模态拆分，因此表达式按普通缓存价格计费，音频缓存差异属于已记录的精度边界。Embedding 2 的图片、音频和视频 token 在当前 Gemini embedding 响应适配链中没有稳定的独立 usage 字段，不能宣称已经精确结算。

## 2026-08-14 xAI 长上下文模型补充

再次读取 xAI 官方模型表后，确认以下语言模型均有 `longContextThreshold=200000`，此前只有基础倍率，长上下文请求会少计费。本轮补齐 `tiered_expr`：

| 模型组 | 200K 以内（输入/缓存命中/输出） | 超过 200K（输入/缓存命中/输出） |
| --- | ---: | ---: |
| `grok-4.3`、`grok-4.20` | `$1.25/$0.20/$2.50` | `$2.50/$0.40/$5.00` |
| `grok-4.6` | `$2.00/$0.50/$6.00` | `$4.00/$1.00/$12.00` |
| `grok-build-0.1`、官方别名 `grok-code-fast-1` | `$1.00/$0.20/$2.00` | `$2.00/$0.40/$4.00` |

表达式使用 `len <= 200000`，边界 token 归入标准档。正式库和 3001 仅为活跃目录中的已确认模型名及厂商路径别名写入阶梯配置；`grok-build`、Composer、搜索专用、视频和语音模型不因名称相似而静默继承 token 价。图像模型仍使用已有的按张 `ModelPrice`。

当前 xAI 活跃目录中的 `grok-imagine-video`、`grok-imagine-video-1.5` 及其 preview/path 变体，官方按分辨率和秒数计费：`grok-imagine-video` 的 480p/720p 分别为 `$0.50/s`、`$0.70/s`，`grok-imagine-video-1.5` 的 480p/720p/1080p 分别为 `$0.80/s`、`$1.40/s`、`$2.50/s`。本仓库的 xAI 适配器目前只实现文本/Responses 与图片生成，没有视频任务的时长、分辨率解析和 `OtherRatios` 计费入口。不能把每秒价格直接写入按次 `ModelPrice`，否则会把视频时长计费为单次固定价；这些模型保持显式未配置，等待视频适配器和计费契约一并接入。Composer 和通用 `grok` 仍没有当前官方独立 token 价，继续保持 fail-closed；Grok 3 Mini 已在 2026-08-30 按官方 token 价补齐。

来源：

- https://docs.x.ai/docs/models

## 2026-08-16 可确认价格收尾

采样时间：2026-08-16 05:11（Asia/Shanghai，21:11 UTC）。本轮重新检查正式库、现有 3001 隔离库和 xAI 官方价格页，只补充有明确官方价格且能被当前计费链准确表达的条目。

已补齐：

- 修正 `grok-4-5` 的历史缓存倍率残留值 `0.25` 为官方 `0.30 / 2.00 = 0.15`；`grok-4.5` 本体及阶梯表达式不变。
- 为 xAI 图像模型加入官方日期和路径别名：`grok-imagine-image-2026-03-02`（`$0.02 / image`）、`grok-imagine-image-quality-20260403`、`grok-imagine-image-quality-latest`、`grok-imagine-image-pro`（均 `$0.05 / image`），并同步 `grok/`、`xai/`、`x-ai/` 路径键。
- 为现有 3001 数据库补入 `gpt-5.3` 的官方倍率：`ModelRatio=0.875`、`CompletionRatio=8`、`CacheRatio=0.1`。

正式库当前未配置价格的活跃项仍包括 xAI Composer、通用 Grok 以及视频模型路径；Grok 3 Mini 已有独立官方 token 价并已写入 options。视频官方按秒和分辨率计费，而当前链路没有时长字段，因此仍保持显式未配置，避免错误套用 token 或按次价格。3001 另有本地、自托管、免费路由和音视频任务模型，未发现可用海外原厂 token 单价的条目，保持渠道或免费策略。

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

## 2026-08-29 全量补齐复核

本轮继续按官方原价检查正式活跃目录，补齐此前漏掉但已有官方单价的模型和路由别名：

| 模型 | 官方原价（每 1M tokens） | 维护方式 |
| --- | --- | --- |
| `minimax-m3` | 标准短上下文输入/缓存命中/输出 `$0.30/$0.06/$1.20`；长上下文 `$0.60/$0.12/$2.40`；priority 为标准档 1.5 倍 | `tiered_expr`，采用页面标注的永久 50% 折后现价 |
| `agnes-2.5-pro-beta` | 输入/缓存命中/输出 `$0.10/$0.01/$0.30` | 标准倍率 |
| `qwen3.8-27b` | 输入/缓存命中/输出 `¥3/¥0.6/¥12`；显式缓存创建 `¥3.75` | 标准倍率；同步 `qwen-3.8-27b`、`Qwen3.8-27B` 别名 |
| `gemini-3.6-flash`、`gemini-3.7-flash` 控制后缀 | 标准 `$1.50/$0.15/$7.50` | `high`、`low`、`medium`、`tiered` 以及 `gemini-3.5-flash-extra-low` 归一到对应官方模型 |

MiniMax M3 价格来源为 [MiniMax Pay as You Go](https://platform.minimax.io/docs/guides/pricing-paygo.md)。页面同时展示划线前价格和当前价格，并明确标注 `Permanent 50% off`；本项目采用当前永久折后价格，不把它误判为限时促销。Agnes Beta 价格来源为 [Agnes 模型价格页](https://wiki.agnes-ai.com/en/docs/pricing.md)，Qwen3.8-27B 价格来源为 [Qwen 官方模型页](https://www.qianwenai.com/models/qwen3.8-27b)。Qwen 页面另列显式缓存命中价格 `¥0.3/M`，当前 usage 结构只有统一缓存读取字段，故 `CacheRatio` 按普通缓存命中 `¥0.6/M` 维护，显式缓存命中差异属于已记录的计量边界。

`agnes-2.5-pro-beta`、Qwen3.8-27B 和 MiniMax M3 的正式历史日志在本轮复核中没有需要按新单价回算的有效消费记录；若后续出现旧价格快照，必须先按日志中的计费模式和 usage 字段做有界 dry-run，再同步 `logs`、余额表、渠道累计和 `quota_data`。

当前仍保持未配置的正式活跃目录项，是官方没有可确认独立 token 原价或当前链路无法精确计量的模型：xAI Composer、通用 Grok、视频/音频/按秒模型、SenseNova 公测模型以及第三方自托管路由。Grok 3 Mini 已在 2026-08-30 按官方原价补齐；不能为了消除剩余缺价列表而套用相近模型价格。

本轮生产和 3001 隔离库均已在事务内合并上述模型键，生产库选项变更前备份为 `/tmp/new-api-pricing-options-before-20260829T154213.tsv`，3001 选项变更前备份为 `/tmp/new-api-dev-pricing-options-before-20260829T154222.tsv`。生产库对新增和校正模型执行修正器 dry-run，结果为 `repaired_logs=0`；3001 dry-run 因隔离 PostgreSQL 长时间磁盘 I/O 未完成，未执行 apply，也未将超时视为通过。正式活跃目录缺价查询已不再包含 Agnes Beta、Qwen3.8-27B、MiniMax-M3、Kimi K2.7 Code 或 Gemini 3.6/3.7 控制后缀；剩余缺价项仍按上面的官方价格和计量边界排除。

随后对漏出原全量范围的 `qwen3.8-max` 历史记录做了单独有界修正：2 条日志由 `quota=0` 改为每条 `83`，同步 2 个小时聚合、用户和渠道累计，并通过正式容器 Redis 清理用户缓存。该组 apply 后 dry-run 为 `repaired_logs=0`；备查表为 `pricing_repair_20260829_qwen38max_logs`（2 条）、`pricing_repair_20260829_qwen38max_users`（1 条）、`pricing_repair_20260829_qwen38max_tokens`（0 条）、`pricing_repair_20260829_qwen38max_channels`（1 条）和 `pricing_repair_20260829_qwen38max_quota_data`（2 条）。用户余额在正式服务持续请求期间还包含实时流量变化，不能只用余额前后差额推导本次修正差额。

## 2026-08-30 Grok 3 Mini 官方价格补齐与历史修正

采样时间：2026-08-30（Asia/Shanghai）。本轮重新读取 xAI 官方模型数据 `https://docs.x.ai/llms.txt`，确认 `grok-3-mini` 的普通 API 原价为输入 `$0.30`、缓存命中 `$0.075`、输出 `$0.50` / 1M tokens；`grok-3-mini-fast` 是同一官方价格对象的官方别名。本项目不使用 Batch 或其他促销价。

标准倍率为：

| 模型 | `ModelRatio` | `CompletionRatio` | `CacheRatio` |
| --- | ---: | ---: | ---: |
| `grok-3-mini` | `0.15` | `5/3` | `0.25` |
| `grok-3-mini-fast` | `0.15` | `5/3` | `0.25` |

同一官方价格对象的别名还包括 `grok-3-mini-latest`、`grok-3-mini-beta`、`grok-3-mini-fast-latest` 和 `grok-3-mini-fast-beta`；本轮已将旧的 `grok-3-mini-fast-beta` 输入倍率 `0.30` 校正为 `0.15`，并补齐全部别名的缓存与输出倍率。

源码已加入两个显式键并校正 beta/latest 别名，正式 PostgreSQL `postgres/new-api` 和 3001 隔离 PostgreSQL `new-api-dev` 均在事务内合并三项 options；写入前备份表为 `pricing_repair_20260830_grok3mini_options`，别名校正另保留 `pricing_repair_20260830_grok3mini_alias_options`。正式服务 `13000` 的历史日志 `447511` 原为 `grok-3-mini`、prompt `13`、completion `15`、旧 quota `1050`，旧快照为 `37.5/1/1`；按官方倍率回算为 quota `6`，差额 `-1044`。修正器在事务内同步 `logs`、用户、令牌和 `quota_data`，并通过正式容器 Redis 清理 2 个受影响缓存键。

修正后限定范围 dry-run 返回 `repaired_logs=0`，日志快照已为 `model_ratio=0.15`、`completion_ratio=1.6666666666666667`、`cache_ratio=0.25`、`model_price=-1`，对应小时聚合 quota 为 `6`。两套数据库 options 查询均返回相同的三项倍率。正式 `http://127.0.0.1:13000/api/status` 和隔离 `http://127.0.0.1:3001/api/status` 均返回 HTTP 200、`success=true`。

本轮不为 Composer、通用 `grok`、视频、音频、按秒、自动路由或第三方自托管模型臆造价格；这些模型仍按无法确认官方原价或当前计量链无法表达处理。

来源：

- https://docs.x.ai/llms.txt
- https://docs.x.ai/developers/models
- https://docs.x.ai/developers/pricing

## 2026-09-05 新模型官方原价生产维护

本节使用 OpenAI API Standard 口径。用户提供的 Enterprise Token Rate Card 属于 ChatGPT Work/Codex 合同口径，不能直接覆盖 API 网关价格；该卡对 GPT-6 Astra 另有 Codex 例外：超过 272K 输入不加长上下文倍率，且 Codex 不收缓存写入费。当前 New API 没有 Enterprise 合同或产品面元数据来按请求切换该口径，因此 GPT-6 的 API 阶梯仍按官方 API 模型页维护。

采样时间：2026-09-05（Asia/Shanghai）。本轮按供应商官方原价复核新模型，排除 GPT-5.6 Sol 当前页面展示的促销价、Batch/Flex 折扣和 Fast 模式加价。OpenAI 的 272K 以上输入上下文按官方规则对整次请求使用 2 倍输入及缓存价格、1.5 倍输出价格；GPT-6 Astra、GPT-5.6 系列公开缓存创建价为未命中输入价的 1.25 倍，GPT-5.5 和 GPT-5.4 未公开缓存创建价，表达式不单独拆分该字段。

| 模型 | 官方原价（输入/缓存命中/缓存创建/输出，美元每 1M tokens） | 维护方式 |
| --- | --- | --- |
| `gpt-6-astra` | `$10/$1/$12.5/$50`；超过 272K 为 `$20/$2/$25/$75` | `tiered_expr` |
| `gpt-5.6-sol`、`gpt-5.6` | `$5/$0.5/$6.25/$30`；超过 272K 为 `$10/$1/$12.5/$45` | `tiered_expr` |
| `gpt-5.6-terra` | `$2/$0.2/$2.5/$12`；超过 272K 为 `$4/$0.4/$5/$18` | `tiered_expr` |
| `gpt-5.6-luna` | `$0.2/$0.02/$0.25/$1.2`；超过 272K 为 `$0.4/$0.04/$0.5/$1.8` | `tiered_expr` |
| `gpt-5.5` | `$5/$0.5/未公开/$30`；超过 272K 为 `$10/$1/未公开/$45` | `tiered_expr` |
| `gpt-5.4`、`gpt-5.4-2026-03-05` | `$2.5/$0.25/未公开/$15`；超过 272K 为 `$5/$0.5/未公开/$22.5` | `tiered_expr` |
| `kimi-k3`、`Kimi-K3` | `$3/$0.30/不适用/$15` | 标准倍率 |
| `agnes-2.5-pro`、`agnes-2.5-pro-alpha` | `$0.45/$0.045/不适用/$0.90` | 标准倍率，缓存倍率 `0.1` |

正式服务 `http://127.0.0.1:13000` 已通过 `newapi` skill 的 `api.js` 合并更新 `ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio`、`billing_setting.billing_mode` 和 `billing_setting.billing_expr` 六个 option，保留其他模型及渠道自定义键。生产写入前快照为 `/tmp/new-api-options-before-20260905T123524.json`；写入后快照为 `/tmp/new-api-options-after-20260905T124523.json` 和 `/tmp/new-api-pricing-after-20260905T124523.json`。线上复核结果：`gpt-6-astra` 和 `Kimi-K3` 不再为零价，Agnes Pro/Alpha 缓存倍率为 `0.1`，所有 GPT 目标模型均返回 `tiered_expr` 和 272K 阶梯表达式。

生产容器 `new-api` 写入前后均为 `healthy`，启动时间 `2026-09-03T10:00:35.998112309Z`，`RestartCount=0`；本轮没有重启或重建正式服务。没有对历史消费日志做回算，未将本轮配置修正误报为历史金额修正。

来源：

- https://developers.openai.com/api/docs/pricing
- https://developers.openai.com/api/docs/models/gpt-6-astra.md
- https://help.openai.com/en/articles/20001415-chatgpt-rate-card-enterprise-token-based-pricing
- https://platform.kimi.ai/docs/pricing/chat-k3.md
- https://wiki.agnes-ai.com/en/docs/pricing.md

## 2026-09-05 永久降价与限时促销区分复核

本轮补充复核发现，不能把所有当前低价都按促销价排除，也不能把所有划线前价格都当作应收价格。判定以供应商官方页面对价格性质的明确文字为准：

| 官方标记 | 维护口径 | 本项目处理 |
| --- | --- | --- |
| MiniMax M3 `Permanent 50% off` | 永久降价后的当前价格 | 使用 `$0.30/$0.06/$1.20`，长上下文 `$0.60/$0.12/$2.40`；priority 为标准档 1.5 倍 |
| OpenAI gpt-5.6-sol `promotional pricing ... through November 21, 2026` | 有明确结束时间的限时促销 | 不使用当前 `$4/$0.40/$5/$20` 促销展示，按标准 `$5/$0.50/$6.25/$30` 维护 |
| OpenAI July 30 gpt-5.6-luna/Luna price update | 官方变更未给出结束日期，按当前标准价维护 | Terra 使用 `$2/$0.20/$2.50/$12`，Luna 使用 `$0.20/$0.02/$0.25/$1.20`；不回退到降价前价格 |
| Google Gemini 3.6/3.7 Flash `through December 31, 2026` | 有明确结束时间的限时价格 | 不使用 `$0.75/$0.075/$3.75`，按 2027 起标准 `$1.50/$0.15/$7.50` 维护 |
| Z.AI GLM-5.3 Flash `promotion ends ... September 9, 2026` | 有明确结束时间的限时促销 | 不使用当前 50% 折后价，按 `$0.15/$0.03/$0.50` 维护 |
| Agnes Video 2.5 Flash 当前免费 `for a limited time` | 限时免费 | 2026-09-07 当前生效价按 `$0` 维护；此前仅使用 list price 的结论已被当前生效价复核覆盖 |

MiniMax 官方原文明确同时显示划线前价格和当前价格，并标注 `Permanent 50% off`；这与 OpenAI、Google、Z.AI、Agnes 页面上的结束日期或 `limited-time` 文案不同。正式服务在本轮复核前仍保留 M3 的折前 `ModelRatio=0.3` 和折前阶梯表达式，已通过 `newapi` skill 的 `api.js` 只合并修正以下四个 M3 键：`MiniMax-M3`、`minimax-m3`、`minimaxai/MiniMax-M3`、`minimaxai/minimax-m3`。修正后为 `ModelRatio=0.15`、`CompletionRatio=4`、`CacheRatio=0.2`，并使用永久折后阶梯表达式；未改动 `CreateCacheRatio`，因为官方页面没有公开 M3 缓存写入价格。

生产写入前目标选项快照为 `/tmp/new-api-options-before-20260905T214956.json`。`http://127.0.0.1:13000/api/pricing` 已返回上述四个别名的 `model_ratio=0.15`、`completion_ratio=4`、`cache_ratio=0.2`、`billing_mode=tiered_expr` 和永久折后表达式；`/api/status` 返回 HTTP 200、`success=true`。正式 PostgreSQL 中 `type=2` 的 M3 历史日志数量为 `0`，因此没有历史账单需要回算或同步余额表。

来源：

- https://platform.minimax.io/docs/guides/pricing-paygo.md
- https://developers.openai.com/api/docs/pricing.md
- https://ai.google.dev/gemini-api/docs/pricing
- https://docs.z.ai/guides/overview/pricing.md
- https://wiki.agnes-ai.com/en/docs/pricing.md

## 2026-09-06 gpt-5.6-sol/Terra 历史修正收尾

本轮补上一次全模型修正遗漏的 gpt-5.6-sol/Terra 普通 token 日志。生产快照边界为 `logs.id <= 2852418`（开始检查时最大日志 ID 为 `2854043`），不追溯边界之后的实时请求。正式容器内的修正器先 dry-run，再在事务中更新 `logs`、`users`、`tokens`、`channels` 和受影响小时的 `quota_data`，提交后清理正式 Redis 的用户/令牌缓存。

| 模型 | 修正日志 | 旧 quota | 新 quota | 差额 |
| --- | ---: | ---: | ---: | ---: |
| `gpt-5.6-sol` | 114,615 | 10,325,775,004 | 12,151,406,828 | +1,825,631,824 |
| `gpt-5.6-terra` | 300,324 | 10,355,985,304 | 11,237,975,218 | +881,989,914 |
| 合计 | 414,939 | 20,681,760,308 | 23,389,382,046 | +2,707,621,738 |

`gpt-5.6-sol-wm`、`gpt-5.6-terra-thinking` 以及已正确计费的行仅发生 0 quota 变更或被修正器判定为 no-op；修正器最终报告的计划行数为 414,941，其中两行的金额差额为 0。apply 返回 `repaired_logs=414941`、Redis 清理 `keys=2`；同一模型列表和边界的 post-dry-run 返回 `repaired_logs=0`。受影响备份表为 `pricing_repair_20260906_gpt56_family_logs`（1,314,187 行）、`_users`（1 行）、`_tokens`（4 行）、`_channels`（54 行）和 `_quota_data`（2,118 行）。备份包含整个 GPT-5.6 家族，便于区分已修正、no-op 和排除项。

以下记录没有强行套用阶梯表达式：`gpt-5.6-luna` 的 676 行、`gpt-5.6-terra` 的 32 行和 `gpt-5.6-sol` 的 1 行带有 `image_generation_call`，其 quota 已包含按张图片费用；`gpt-5.6-luna-openai-compact`、`gpt-5.6-sol-openai-compact` 是独立 compact 价格维度。它们按规程保留历史价格快照，避免重复或错误计费。`quota_data` 对备份家族的聚合复核不一致数为 `0`。

生产 `new-api` 容器在 apply 前后未重启，`13000` 服务仍可用。检查期间新增日志 ID 大于快照边界的请求属于实时流量，不纳入本轮历史修正；用户、令牌和渠道累计值与修正期间实时请求的差异应分开解释。

## 2026-09-06 全面复核与内部别名历史修正

采样时间：2026-09-06（Asia/Shanghai）。本轮重新读取 OpenAI 官方 Markdown 价格页和 GPT-6 Astra、GPT-5.6 Luna 模型页，并通过 `newapi` skill 的 `api.js` 复核正式 `13000` 的 `/api/status` 和 `/api/pricing`。GPT-6 Astra 的标准 API 价格为 `$10/$1/$12.5/$50`，超过 272K 为 `$20/$2/$25/$75`；GPT-5.6 Luna 为 `$0.2/$0.02/$0.25/$1.2`，超过 272K 为 `$0.4/$0.04/$0.5/$1.8`。正式配置表达式与官方值一致；该模型按上面的官方价格维护。

本轮发现历史修正器的源码默认值缺少项目内部别名 `codex-auto-review` 的输出和缓存倍率，导致独立工具把已按正式配置计费的最新日志误报为待修正。源码已补齐该内部别名的 `ModelRatio=2.5`、`CompletionRatio=6`、`CacheRatio=0.1`，并增加回归测试；该值是项目正式配置中与 GPT-5.5/5.6 标准档一致的内部计费口径，不代表供应商另有一个名为 `codex-auto-review` 的官方模型。

正式历史日志中 10 条 `codex-auto-review` 记录的旧快照为 `2.5/1/1`，均为 272K 以下普通 token 请求，按正式内部别名口径由旧 quota 合计 `383,662` 修正为 `232,552`，差额 `-151,110`。修正器事务同步了 `logs`、用户、令牌、渠道和 `quota_data`，并清理了 3 个正式 Redis 用户/令牌缓存键；同一模型范围二次 dry-run 为 `repaired_logs=0`。备查审计表为 `pricing_repair_20260906_codex_repair_audit`，记录 10 条日志的旧、新 quota 和差额。

另有 `glm-4.6-cpa` 1 条零 token、零 quota 日志，仅将旧价格快照规范化为 `glm-4.6` 官方倍率 `0.3/3.6666666666666665/0.18333333333333332`，金额差额为 `0`；备查表为 `pricing_repair_20260906_glm46cpa_logs`，限定范围二次 dry-run 为 `repaired_logs=0`。

截至 2026-09-06，正式 `/api/pricing` 曾返回 12 个零倍率且无按次价格的目录项，其中包括当时尚未写入 `ModelPrice` 的 `agnes-image-2.5-flash`。该历史快照已由 2026-09-07 的 Agnes 当前生效价纠正覆盖；当前 Agnes 图片 Flash 条目已显式维护为 `ModelPrice=0`。其余项目分别属于路由别名、第三方或自托管模型、未确认独立 token 原价，或按分辨率、参考图、视频秒数计费而当前 token/按次结算链无法准确表达。

验证结果：正式 `13000` 与隔离 `3001` 的 `nc`、HTTP 健康检查均通过；正式和隔离容器均为 `healthy` 且 `RestartCount=0`。正式镜像仍是 `new-api-local:prod-codex-lite-4527d7666`，不是当前 dirty 工作区；隔离环境为当前工作区构建。相关 Go 测试、修正器 dry-run、正式价格接口和 `git diff --check` 均通过。历史修正器临时诊断二进制已从正式容器和主机临时目录移除。

最终复核补充：首次全量 `go test -count=1 ./...` 暴露 `TestLegacyPooledCacheRouteFilterKeepsMappedCandidateIdentity` 的全局缓存测试隔离问题；仅在该测试建立和恢复 `legacyRouteCandidateCache`，未改变路由业务逻辑。修正后 `go test -count=1 ./model` 和全量 `go test -count=1 ./...` 均以退出码 `0` 完成；正式 `/api/pricing` 价格契约检查覆盖 `77` 个目录项、`14` 个明确缺价项，返回 `pricing_contract_ok`。本轮未重启或重建正式服务，未新增历史账单写入。

## 2026-09-06 GPT 全系列正式目录复核

采样时间：2026-09-06（Asia/Shanghai）。本轮重新读取 OpenAI 官方 Standard API 价格页、模型页和 GPT Image 定价表，并通过 `newapi` skill 的 `api.js` 只读回读正式 `http://127.0.0.1:13000`。正式 `/api/pricing` 的 GPT 前缀目录共 20 个条目，逐项核对 `ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio` 以及 `billing_setting.billing_expr`；其中 8 个目录键对应 7 组 272K 阶梯价格，`gpt-5.4-2026-03-05` 与 `gpt-5.4` 共用一组表达式。结果为 `gpt_pricing_contract_ok=true`、`active_gpt_entries=20`、`checked_entries=20`、`checked_tiered_directory_entries=8`、`checked_tiered_price_groups=7`。

正式目录与官方原价（每 1M tokens，美元）如下；同一行中的多个键是官方快照或项目目录别名，价格相同：

| 正式目录键 | Standard 原价 | 维护方式 |
| --- | --- | --- |
| `gpt-6-astra` | 输入 `$10`、缓存命中 `$1`、缓存创建 `$12.5`、输出 `$50`；超过 272K 为 `$20/$2/$25/$75` | `tiered_expr` |
| `gpt-5.6-sol`、`gpt-5.6` | 输入 `$5`、缓存命中 `$0.5`、缓存创建 `$6.25`、输出 `$30`；超过 272K 为 `$10/$1/$12.5/$45` | `tiered_expr`；排除页面中有结束日期的促销价 |
| `gpt-5.6-terra` | `$2/$0.2/$2.5/$12`；超过 272K 为 `$4/$0.4/$5/$18` | `tiered_expr` |
| `gpt-5.6-luna` | `$0.2/$0.02/$0.25/$1.2`；超过 272K 为 `$0.4/$0.04/$0.5/$1.8` | `tiered_expr` |
| `gpt-5.5` | 输入 `$5`、缓存命中 `$0.5`、输出 `$30`；超过 272K 为 `$10/$1/$45`；官方未公开缓存创建价 | `tiered_expr` |
| `gpt-5.4`、`gpt-5.4-2026-03-05` | 输入 `$2.5`、缓存命中 `$0.25`、输出 `$15`；超过 272K 为 `$5/$0.5/$22.5`；官方未公开缓存创建价 | `tiered_expr` |
| `gpt-5.4-mini` | 输入 `$0.75`、缓存命中 `$0.075`、输出 `$4.5` | 标准倍率 |
| `gpt-5.3-codex-spark` | 按官方 `gpt-5.3-codex` 价格：输入 `$1.75`、缓存命中 `$0.175`、输出 `$14` | 标准倍率；Spark 是项目目录键，不臆造独立供应商价 |
| `gpt-5.2`、`gpt-5.2-2025-12-11`、`gpt-5.2-chat-latest` | 输入 `$1.75`、缓存命中 `$0.175`、输出 `$14` | 标准倍率 |
| `gpt-5.2-pro`、`gpt-5.2-pro-2025-12-11` | 输入 `$21`、输出 `$168`；官方未公开缓存价 | 标准倍率 |
| `gpt-4o-audio-preview` | 文本输入/输出 `$2.5/$10`；音频输入/输出 `$40/$80` | 按文本与音频 usage 分支计费 |
| `gpt-4o-realtime-preview` | 文本输入/缓存命中/输出 `$5/$2.5/$20`；音频输入/缓存命中/输出 `$40/$2.5/$80` | 按文本与音频 usage 分支计费 |
| `gpt-image-1` | 文本输入/缓存 `$5/$1.25`；图像输入/缓存/输出 `$10/$2.5/$40`；另按官方质量和尺寸收取图像生成单价 | token 与图像生成分开维护 |
| `gpt-image-1.5` | 文本输入/缓存/输出 `$5/$1.25/$10`；图像输入/缓存/输出 `$8/$2/$32`；另按官方质量和尺寸收取图像生成单价 | token 与图像生成分开维护 |
| `gpt-image-2` | 文本输入/缓存 `$5/$1.25`；图像输入/缓存/输出 `$8/$2/$30` | token 与图像生成分开维护 |

本段是此前的历史复核记录；其中对当前 `$4/$0.40/$5/$20` 条目的促销排除结论已被本文件末尾的“当前生效价纠正”覆盖，不再作为当前配置依据。GPT Image 的图像生成按质量、尺寸和输出数量计费，不能用单一 token 倍率替代；正式链路已有独立图像费用处理，本轮只核对其 token 价格和倍率映射。

来源：

- https://developers.openai.com/api/docs/pricing.md
- https://developers.openai.com/api/docs/models/gpt-6-astra.md
- https://developers.openai.com/api/docs/models/gpt-5.6-luna.md
- https://developers.openai.com/api/docs/models/gpt-5.5.md
- https://developers.openai.com/api/docs/models/gpt-5.4.md
- https://developers.openai.com/api/docs/models/gpt-5.4-mini.md
- https://developers.openai.com/api/docs/models/gpt-5.3-codex.md
- https://developers.openai.com/api/docs/models/gpt-5.2.md
- https://developers.openai.com/api/docs/models/gpt-5.2-pro.md
- https://developers.openai.com/api/docs/models/gpt-4o-audio-preview.md
- https://developers.openai.com/api/docs/models/gpt-4o-realtime-preview.md
- https://developers.openai.com/api/docs/models/gpt-image-1.md
- https://developers.openai.com/api/docs/models/gpt-image-1.5.md
- https://developers.openai.com/api/docs/pricing.md#image-generation

## 2026-09-06 当前生效价纠正

重新读取官方 Standard 价格页后，以当前生效价为准：页面当前 `$4/$0.40/$5/$20` 的模型条目，超过 272K 为 `$8/$0.80/$10/$30`。官方说明该价格至少持续至 2026-11-21，因此当前服务不能继续按 `$5/$0.50/$6.25/$30` 的旧快照计费。上一节的 `$5` 表格和“排除促销价”文字属于过时记录，不能作为当前配置依据。

官方 Standard 价格表中当前 `$4/$0.40/$5/$20` 的条目（含 272K 以上 `$8/$0.80/$10/$30` 阶梯）已按页面列示的模型名称维护；另一个 `$0.20/$0.02/$0.25/$1.20` 条目保持原值。同一轮复核确认 `gpt-5-pro` 当前输入价为 `$15 / 1M`，对应 `ModelRatio=7.5`；正式选项原有 `6.75` 已修正。四个 OpenAI 音频预览模型及其日期别名的官方音频输出/输入比为 `2`，正式 `AudioCompletionRatio` 已补齐。历史账单只在有明确生效边界时回算；没有边界的旧日志不凭当前页猜测追溯。

## 2026-09-07 当前生效价重新核对与线上纠正

采样时间：2026-09-07（Asia/Shanghai）。本轮重新抓取 OpenAI 官方 Standard API 价格页、Changelog 和模型页，未采用 Batch、Flex 或 Fast 模式价格。官方当前 Standard 价格如下（每 1M tokens，顺序为输入/缓存命中/缓存创建/输出）：

| 模型 | 272K 以下 | 超过 272K |
| --- | --- | --- |
| `gpt-6-astra` | `$10/$1/$12.5/$50` | `$20/$2/$25/$75` |
| `gpt-5.6-sol`、`gpt-5.6` | `$4/$0.40/$5/$20` | `$8/$0.80/$10/$30` |
| `gpt-5.6-terra` | `$2/$0.20/$2.50/$12` | `$4/$0.40/$5/$18` |
| `gpt-5.6-luna` | `$0.20/$0.02/$0.25/$1.20` | `$0.40/$0.04/$0.50/$1.80` |

Sol 的 `$4/$0.40/$5/$20` 是当前页面生效价；Changelog 说明该价格至少持续至 2026-11-21。它虽然被官方称为 promotional pricing，但在该有效期内应按当前价格结算，不能继续使用已失效的 `$5/$0.50/$6.25/$30` 快照。Terra 和 Luna 的 2026-07-30 降价没有结束日期，按当前标准价维护。

正式服务 `http://127.0.0.1:13000` 复核时发现 `gpt-5.6-luna` 及其 `-wm`、`-pro`、`-openai-compact` 变体被误写成 Sol 的 `ModelRatio=2`、`CompletionRatio=5` 和 Sol 阶梯表达式。本轮只合并修正这组 Luna 选项：`ModelRatio=0.1`、`CompletionRatio=6`、`CacheRatio=0.1`、`CreateCacheRatio=1.25`，并把 Luna 表达式恢复为 `$0.20/$0.02/$0.25/$1.20` 与 `$0.40/$0.04/$0.50/$1.80` 阶梯；其他模型及无关选项保持不变。

线上历史日志按实际存在的模型名做有界 dry-run，覆盖 `gpt-5.6`、Luna 及其已知变体、`gpt-6-astra`。修正器先 dry-run 命中 85 条普通 token 日志：`gpt-5.6` 10 条（quota `296100` 到 `371635`），Luna 75 条（quota `4454598` 到 `224216`）；事务同步 `logs`、`users`、`tokens`、`channels` 和 `quota_data`，随后清理 3 个 Redis 缓存键。复核结果为 `repaired_logs=0`。带图片费用和 compact 独立计费维度的记录没有强行套用 token 阶梯。

正式服务状态仍为 `running/healthy`，重启次数为 `0`；`13000` TCP 和 `/api/status` 均通过。修正前备查表包括既有 `pricing_repair_20260907_gpt56_alias_*` 以及本轮增量 `pricing_repair_20260907_gpt56_inc_*` 表。

来源：

- https://developers.openai.com/api/docs/pricing.md
- https://developers.openai.com/api/docs/changelog.md
- https://developers.openai.com/api/docs/models/gpt-6-astra.md
- https://developers.openai.com/api/docs/models/gpt-5.6-luna.md

## 2026-09-07 Agnes 当前生效价纠正

本轮实时读取 Agnes 官方价格页后，按页面的 `Current price` 维护新请求，不再沿用此前仅取 list price 的旧结论：

| 模型 | 当前生效价 | 维护方式 |
| --- | ---: | --- |
| `agnes-2.0-flash`、`agnes-2.5-flash` | 输入/输出 `$0 / 1M tokens` | `ModelRatio=0`、`CompletionRatio=0`、`CacheRatio=0` |
| `agnes-image-2.0-flash`、`agnes-image-2.1-flash`、`agnes-image-2.5-flash` | `$0` | `ModelPrice=0` |
| `agnes-video-v2.0` | `$0 / second` | `ModelPrice=0` |
| `agnes-video-2.5` | 720P `$0.025/s`；1080P/1K `$0.040/s`；2K `$0.055/s` | 保留现有视频时长和分辨率计量 |
| `agnes-video-2.5-flash` | 当前 `$0 / second`（页面标注 limited time） | `ModelPrice=0` |

页面没有为 Flash 文本模型公布独立的缓存创建价格，因此没有新增 `CreateCacheRatio=0`；缓存命中随当前免费输入价显式维护为 `CacheRatio=0`。Agnes 页面同时注明当前 offer 可能随公告或账户账单变化，本次只修正当前生效的新请求价格。页面没有给出这些免费 offer 的明确起始时间，历史 Agnes 日志只做有界 dry-run，不凭当前页直接回写既有账单。

历史 dry-run 使用当前源码修正器、`channel_id=0` 和 Agnes 模型白名单，命中 1 条 `agnes-2.0-flash` 普通 token 日志（旧 quota `4`，按当前免费价新 quota `0`）；由于官方未公布免费 offer 的生效起始时间，本次未执行 `--apply`，没有改写任何历史日志或余额汇总。

来源：

- https://wiki.agnes-ai.com/en/docs/pricing.md

Agnes 官方模型索引同时列出新模型 `agnes-3.0-flash`，但其官方模型页明确写明 `Pricing will be announced separately`，当前没有可确认的 token 单价。因此该活跃目录项继续保持未配置，不能把 2.5 Flash 的免费价或其他模型价格静默套用到 3.0 Flash。

来源：

- https://wiki.agnes-ai.com/en/docs/agnes-30-flash.md

## 2026-09-09 新发布模型复核与 LongCat-2.0 价格维护

采样时间：2026-09-09（Asia/Shanghai）。本轮通过正式服务 `http://127.0.0.1:13000` 的管理接口复核 active channel 目录和 `/api/pricing`，并重新读取供应商官方模型页和价格页。正式目录共 61 个活跃模型；其中 `longcat-2.0` 已由正式渠道 227 提供，但此前没有有效 token 价格。

LongCat 官方模型 API 文档确认模型 ID 为 `longcat-2.0`。官方 [LongCat-2.0 Models & Pricing](https://longcat.ai/platform/docs/Pricing/LongCat-2.0.html) 的 Chat 版标准价为每 1M tokens：未命中输入 `¥5`、缓存命中输入 `¥0.10`、输出 `¥20`。同一页面另列 `¥2/¥0.04/¥8` 的 `limited-time` 折扣，本项目按标准原价维护，不采用该限时价格。

按项目历史倍率口径，已加入源码默认配置和正式 options：

| 模型 | `ModelRatio` | `CompletionRatio` | `CacheRatio` | `CreateCacheRatio` |
| --- | ---: | ---: | ---: | ---: |
| `longcat-2.0` | `5/1000*RMB`（约 `0.3424657534`） | `20/5 = 4` | `0.10/5 = 0.02` | 未配置 |

官方页面没有公布缓存创建单价，因此不推导或写入 `CreateCacheRatio`。本轮只更新 `longcat-2.0` 三个 token 价格键，未改动其他模型、渠道或按秒/按分辨率计费条目。

同一轮目录扫描发现 Google 官方模型页新增 `gemini-3.8-flash`，但它尚未出现在正式 active channel 目录；其当前 `$0.75/$0.075/$3.75` 价格明确标注仅至 2026-12-31，属于限时价格，不能按本项目“不采用促销价”的口径写入正式服务。`agnes-3.0-flash` 仍标注 `Pricing will be announced separately`；`Qwen3.8-Max-0902` 是 `qwen3.8-max` 的页面版本标识，不是新的 API ID。其余正式缺价项（Composer、路由模型、SenseNova U1.5 Lite、视频/按秒模型、未限定供应商的 Embedding）仍缺乏可确认标准 token 原价或当前计量链无法准确表达，保持 fail-closed。

验证结果：`go test -count=1 ./setting/ratio_setting`、`go test -count=1 ./pkg/pricingrepair ./bin` 和相关后端回归 `go test -count=1 ./controller ./model ./relay/common ./relay/helper ./service` 均以退出码 0 通过，`git diff --check` 通过。正式 options 写入前后差异仅为三个目标键各新增 `longcat-2.0`，`CreateCacheRatio` 未变化；`/api/status` 返回 `success=true`，`/api/pricing` 返回上述倍率。正式容器仍为 `running/healthy`、`RestartCount=0`，本轮未重启。正式数据库查询到 `longcat-2.0` 的历史普通 token 日志数量为 0。历史账单只有在确定官方价格生效时间后才可回算；LongCat 页面未给出明确生效起始时间，本轮不直接改写历史日志或余额汇总。

来源：

- https://longcat.ai/platform/docs/api/models
- https://longcat.ai/platform/docs/Pricing/LongCat-2.0.html
- https://ai.google.dev/gemini-api/docs/pricing
- https://ai.google.dev/gemini-api/docs/models

## 2026-09-10 Agnes 3.0 Flash 当前生效价纠正

采样时间：2026-09-10（Asia/Shanghai）。重新读取 Agnes 官方价格页和 `agnes-3.0-flash` 模型页后，修正 2026-09-07/09-09 记录中的过时结论。官方当前价格表明确列出 `agnes-3.0-flash` 的缓存输入、普通输入和输出当前价格均为 `$0 / 1M tokens`；模型页同时给出其 list price 为 `$0.005/$0.05/$0.15`，但结算应以页面的 `Current price` 为准。

本项目维护为：`ModelRatio=0`、`CompletionRatio=0`、`CacheRatio=0`。官方没有公布缓存创建价格，因此不新增 `CreateCacheRatio`，也不把 list price 当作当前生效价。正式 `http://127.0.0.1:13000` 当前 `/api/pricing` 已返回该模型的 `0/0/0`，本轮只补齐源码默认倍率和回归测试，未重启正式容器或覆盖其他选项。

官方页面没有给出免费当前价的明确起始时间，因此本轮不对 `agnes-3.0-flash` 历史账单执行 apply；历史日志如需修正，必须先取得明确生效边界，再按规程 dry-run 并同步余额、渠道累计和 `quota_data`。

来源：

- https://wiki.agnes-ai.com/en/docs/pricing.md
- https://wiki.agnes-ai.com/en/docs/agnes-30-flash.md

## 2026-09-11 Qwen3.6-35B-A3B 与 Spark-X2.5 当前原价维护

采样时间：2026-09-11（Asia/Shanghai）。本轮只写入能从供应商官方页面确认、且能映射到现有 token 计量链的模型；不采用页面上的限时折扣或其他促销价。

| 模型 | 官方当前原价 | 维护方式 |
| --- | --- | --- |
| `Qwen3.6-35B-A3B` | 输入 `¥1.8 / 1M tokens`，输出 `¥10.8 / 1M tokens` | `ModelRatio=1.8/1000*RMB`、`CompletionRatio=6`；官方未列缓存价，不配置缓存倍率 |
| `spark-x2.5` | 输入 `¥3.2 / 1M tokens`，缓存命中 `¥0.48 / 1M tokens`，输出 `¥12 / 1M tokens` | `ModelRatio=3.2/1000*RMB`、`CompletionRatio=3.75`、`CacheRatio=0.15`；不配置缓存创建倍率 |

`Qwen3.6-35B-A3B` 使用无供应商前缀的正式目录键；源码中已有的 `Qwen/Qwen3.6-35B-A3B` 是 SiliconFlow 独立价格，保持不变，不能混用。讯飞页面同时显示 Spark-X2.5 的 5 折价格，本项目按用户要求使用页面标示的原价，不把折扣价写入配置。

`step-router-v1` 的 StepFun 官方页面明确说明按实际命中的 `deepseek-v4-pro` 或 `step-3.7-flash` 引擎计费。由于当前网关没有可靠的命中引擎计量字段，不能安全映射为单一倍率，继续保持未配置。`Qwen3-Embedding-8B` 尚未找到阿里/Qwen 官方公开 API 单价，继续保持未配置；不能套用 SiliconFlow 的 `Qwen/Qwen3-Embedding-8B` 价格。SenseNova 的当前页面为“公测期完全免费/限时放量”，不属于可确认的原价，继续保持现有显式零值或未配置状态。

本轮通过 `newapi` skill 的 `api.js` 合并正式 `13000` 服务的 `ModelRatio`、`CompletionRatio` 和 `CacheRatio`，只新增上述两个正式目录键，未新增 `CreateCacheRatio`，未改写历史消费日志或余额汇总。历史回算需要明确的官方生效边界，本轮页面未提供该边界，因此不执行。

来源：

- https://www.qianwenai.com/models/qwen3.6-35b-a3b
- https://maas.xfyun.cn/modelSquare/base/2624708664591360
- https://platform.stepfun.com/docs/zh/guides/models/step-router

同一轮 active channel 交叉核对发现正式渠道使用大小写别名 `GPT-5.5`。该名称是 `gpt-5.5` 的大小写变体，不是独立模型；官方 Standard 原价为输入 `$5`、缓存命中 `$0.50`、输出 `$30`（超过 272K 时为 `$10/$1/$45`）。正式选项已补齐别名的 `ModelRatio=2.5`、`CompletionRatio=6`、`CacheRatio=0.1`；不新增缓存创建价，因为官方未公开该项。历史日志未执行回算，避免把别名修正误作供应商价格变更。

## 2026-09-10 Agnes 3.0 Flash 正式 options 显式值收尾

随后对正式 `13000` 的 `options` 做了有界回读，发现 `ModelRatio`、`CompletionRatio` 和 `CacheRatio` 三个线上 JSON 映射当时尚未显式包含 `agnes-3.0-flash`；价格接口因此依赖缺省零值，虽然结果为零但不可审计。已通过 `newapi` skill 的 `api.js` 在正式服务合并写入这三个键，保留其他模型映射，不新增官方未公布的 `CreateCacheRatio`。

写入前快照为 `/tmp/new-api-options-before-20260910-agnes30.json`，写入后三项目标键均为 `0`；正式 `/api/pricing` 已显式返回 `agnes-3.0-flash` 的 `model_ratio=0`、`completion_ratio=0`、`cache_ratio=0`。本次只修正当前配置，没有历史账单回算或余额汇总改写。

## 2026-09-10 xAI Grok 当前价格复核

重新读取 xAI 官方模型数据后，当前语言模型价格为：`grok-4.5` 及其 `grok-4.5-latest`、`grok-build-latest` 别名在 200K tokens 以内输入/缓存命中/输出为 `$2/$0.30/$6`，超过 200K 为 `$4/$0.60/$12`；`grok-4.6` 为 `$2/$0.50/$6`，超过 200K 为 `$4/$1/$12`。正式 `/api/pricing` 对应倍率分别为 `1/3/0.15`、`1/3/0.25`，并使用 `len < 200000` 的同值阶梯表达式，当前没有价格偏差。

本次只做线上价格契约复核，没有改写 Grok options。Grok 4.20 及其别名当前不在正式 active channel；视频模型仍按秒和分辨率计费，当前链路没有可安全映射的固定 token 或按次价格，继续保持显式未配置。

来源：

- https://docs.x.ai/developers/pricing
- https://docs.x.ai/docs/models

## 2026-09-11 OpenAI GPT Image 2.5 官方价格维护

采样时间：2026-09-11（Asia/Shanghai）。OpenAI 官方当前没有可直接调用的通用模型 ID `gpt-image-2.5`，而是发布了两个精确模型：`gpt-image-2.5-sunburst` 和 `gpt-image-2.5-flare`。两者的官方标准 token 价格相同：图像输入 `$8`、缓存图像输入 `$2`、图像输出 `$30`、文本输入 `$5`、缓存文本输入 `$1.25`，均按每 1M tokens 计费；文本输出不适用。

这组价格与现有 `gpt-image-2` 完全一致，因此按本项目倍率口径维护为：

| 模型 | `ModelRatio` | `CompletionRatio` | `CacheRatio` | `ImageRatio` |
| --- | ---: | ---: | ---: | ---: |
| `gpt-image-2.5-sunburst` | `2.5` | `6` | `0.25` | `1.6` |
| `gpt-image-2.5-flare` | `2.5` | `6` | `0.25` | `1.6` |

不创建 `ModelPrice` 按次价格，也不创建 `CreateCacheRatio`；官方价格表没有公布缓存写入单价。源码模型名归一化已将两个精确变体映射到 `gpt-image-2` 的同价计费路径，但不会创建未被官方公布的通用 `gpt-image-2.5` 别名。

本轮正式服务 `13000` 的 active channel 当前没有这两个模型，因此 `/api/pricing` 不会展示它们；正式 options 已预置四个倍率字段，待渠道启用精确模型名后可直接计费。没有历史消费日志，未执行历史账单回算。

验证结果：OpenAI 官方模型页和图像生成指南均确认两个精确模型 ID；官方价格页确认上述输入、缓存和输出价格。源码 `go test -count=1 ./setting/ratio_setting` 通过；正式 `13000` 只读复核确认当前没有 `gpt-image-2.5` 相关 active channel。

来源：

- https://developers.openai.com/api/docs/pricing
- https://developers.openai.com/api/docs/models/gpt-image-2.5-sunburst
- https://developers.openai.com/api/docs/models/gpt-image-2.5-flare
- https://developers.openai.com/api/docs/guides/image-generation

## 2026-09-11 正式目录遗漏复核与 DeepSeek V4.1/SenseNova 线上补齐

采样时间：2026-09-11（Asia/Shanghai）。本轮重新读取正式 `http://127.0.0.1:13000` 的 `/api/user/models`、`/api/pricing` 和 `/api/channel?page=1&page_size=100`，以 active channel 实际模型名为边界。用户模型目录和价格接口均为 74 项；筛出标准 token 模式下倍率为零或价格字段不完整的条目后，逐项对照供应商官方模型页和价格页。除下述项目外，没有发现可以安全映射到当前 token/按次结算链的新增官方价格遗漏。

DeepSeek 官方当前页面已将 API 模型名切换为 `deepseek-flash`，模型版本为 V4.1-Flash；`deepseek-v4-flash` 和 `deepseek-v4-flash-vision-exp` 旧名仍接受请求并按 Flash 价格计费。当前非高峰输入/缓存命中/输出为 `$0.15/$0.003/$0.60` / 1M tokens，工作日 UTC 01:00-04:00、06:00-10:00 峰值时段为 `$0.30/$0.006/$1.20`。正式渠道 215 使用的 `deepseek-v4.1-flash-0910` 原先仍为 `0/0` 且无缓存字段，本轮已补为：

| 模型 | `ModelRatio` | `CompletionRatio` | `CacheRatio` | 计费模式 |
| --- | ---: | ---: | ---: | --- |
| `deepseek-v4.1-flash-0910` | `0.075` | `4` | `0.02` | `tiered_expr`，按 DeepSeek 峰谷时段 |

同时补齐 `deepseek-v4.1-flash` 别名和 SenseNova 路径别名的线上映射。SenseNova 官方 [Token Plan](https://www.sensenova.cn/token-plan) 当前明确列出 `SenseNova 6.8 Flash Lite` 和 `SenseNova U1 Fast` 为公测期免费；正式 options 现显式维护 `ModelRatio=0`、`CompletionRatio=0`、`CacheRatio=0`，避免原先 `CompletionRatio=1` 或缺省缓存值造成元数据不一致。该页面未将 `sensenova-6.7-flash-lite` 或 `sensenova-u1.5-lite` 列入当前免费模型，二者继续保持未配置，不套用 6.8/U1 价格。

本轮正式线上更新只涉及 `ModelRatio`、`CompletionRatio`、`CacheRatio`、`billing_setting.billing_mode` 和 `billing_setting.billing_expr` 五个 option，均通过 `newapi` skill 的 `api.js` 完成；未新增 DeepSeek 缓存创建价，也未改写历史消费日志。由于 DeepSeek V4.1 页面未提供精确的历史生效时间，本轮不对旧日志执行回算。

更新后正式 `/api/pricing` 目标值为：`deepseek-v4.1-flash-0910=0.075/4/0.02/tiered_expr`、`sensenova-6.8-flash-lite=0/0/0`、`sensenova-u1-fast=0/0/0`；`/api/status` 返回 `success=true`、版本 `v1.1.0`。剩余 25 个零倍率或无按次价格条目分别属于按秒/按分辨率计费的视频模型、未公布独立 token 原价的 Composer/路由/Embedding/自托管模型，或 SenseNova 未列入当前 Token Plan 的模型；当前结算链无法安全表达的项目继续 fail-closed。

验证命令：

```text
go test -count=1 ./setting/ratio_setting ./setting/billing_setting: exit=0
git diff --check -- 目标源码与本记录: exit=0
```

来源：

- https://api-docs.deepseek.com/quick_start/pricing
- https://api-docs.deepseek.com/updates
- https://www.sensenova.cn/token-plan

## 2026-09-11 SenseNova 6.7 fail-closed 收尾与 Grok 别名复核

本轮再次回读正式 `13000` 的 options 和 `/api/pricing`，发现 `sensenova-6.7-flash-lite` 仍有错误的 `ModelRatio=0`、`CompletionRatio=1` 映射。SenseNova 当前 Token Plan 只列出 `SenseNova 6.8 Flash Lite` 和 `SenseNova U1 Fast` 的公测免费项，没有列出 6.7；因此不能把 6.7 当作免费模型，也不能把 6.8 的当前状态套用到 6.7。

已通过 `newapi` skill 的 `api.js` 从正式 `ModelRatio`、`CompletionRatio` 和 `CacheRatio` 三个 JSON option 中删除 `sensenova-6.7-flash-lite`，其他键保持不变。删除后 `/api/pricing` 对该 active 目录项只返回缺省零值，不再带有伪造的 `CompletionRatio=1`，源码默认映射和回归测试也已同步删除并增加 fail-closed 覆盖。该模型在正式日志中有 5 条历史记录；当前修正器 dry-run 返回 `repaired_logs=0`，由于官方没有可确认的生效时间或 token 原价，本轮不改写历史账单、余额或聚合表。

同时重新读取 xAI 官方模型页，确认 `grok-build-latest` 明确位于 `grok-4.5` 的 aliases 列表，其当前价格仍为 200K 以内 `$2/$0.30/$6`、超过 200K `$4/$0.60/$12`。`grok-build-0.1` 是独立模型，价格为 `$1/$0.20/$2`（长上下文 `$2/$0.40/$4`）；因此正式 `grok-build-latest` 的 `$1/3/0.15` 标准倍率继承和 Grok 4.5 阶梯表达式均保持不变，不能错误改到 Build 0.1。

验证：`go test -count=1 ./setting/ratio_setting ./setting/billing_setting ./bin ./pkg/pricingrepair` 退出码为 0；正式 `/api/status` 返回 `success=true`、版本 `v1.1.0`；`13000` TCP 检查通过。

来源：

- https://www.sensenova.cn/token-plan
- https://docs.x.ai/developers/models

## 2026-09-11 正式价格目录最终核对

采样时间：2026-09-11 21:19（Asia/Shanghai）。正式 `13000` 当前有 32 个启用渠道，启用渠道模型去重后为 75 项；`/api/pricing` 返回 75 项，启用渠道模型与价格目录的精确集合差异为 0。目录中 19 项使用 `tiered_expr`，22 项使用标准 token 倍率，4 项使用已确认的按次价格，5 项为按次模型的显式零价，另有 25 项标准 token 倍率和按次价格均为零。

复核中发现并已修正一个正式配置遗漏：启用渠道 199、169 使用的 `GPT-5.5` 大写别名原先只有 `ModelRatio=2.5`、`CompletionRatio=6`、`CacheRatio=0.1`，缺少阶梯计费键。现已在正式 `billing_setting.billing_mode` 和 `billing_setting.billing_expr` 中显式补入 `GPT-5.5`，与小写 `gpt-5.5` 共用官方输入 `$5/$0.50/$30`、超过 272K 为 `$10/$1/$45` 的表达式。写入前后 options 仅各新增该一个别名键，其他价格键未变化；`/api/pricing` 已同时返回大小写两个键的 `tiered_expr`。

剩余 25 项零倍率目录均已逐项确认属于免费或未公布独立原价的模型、动态路由、视频/按秒计费模型，或当前结算链无法准确表达的项目，未将相近模型价格静默继承。当时记录将 `Kun` 归因为已停用渠道 168；该状态已过时，后续复核确认正式渠道 168、225 均为启用状态且都提供 `Kun`。`Kun` 仍没有可确认的官方独立原价，因此继续不补价。

验证结果：`go test -count=1 ./setting/ratio_setting ./setting/billing_setting ./pkg/pricingrepair ./bin` 退出码为 0；`git diff --check` 通过；正式 `/api/status` 返回 `success=true`、版本 `v1.1.0`；`13000` TCP 检查通过；正式容器 `running/healthy`、`RestartCount=0`，未重启。此次别名补齐只影响后续请求的计费模式，历史 `gpt-5.5` 日志已使用小写模型名和阶梯快照，未发现需要额外回算的历史金额。

## 2026-09-12 DeepSeek V4 Pro 规则修正与正式目录收尾

采样时间：2026-09-12（Asia/Shanghai）。重新读取 DeepSeek 官方价格页和更新日志，确认 `deepseek-v4-pro` 对应 `DeepSeek-V4-Pro-0813`，2026-09-14 之后仍继续提供 API，计费方式保持不变；不能因服务日期变化而切换到 Flash 价格。正式服务当前 `deepseek-v4-pro`、`DeepSeek-V4-Pro` 和 `deepseek-v4-pro-0813` 均使用同一官方峰谷价：工作日 UTC 01:00-04:00、06:00-10:00 为输入/缓存命中/输出 `$1.32/$0.044/$3.96`，其余时间为 `$0.66/$0.022/$1.98` / 1M tokens。

源码已将 Pro 的运行时表达式固定为上述峰谷价格，并让时间戳感知的历史修正逻辑在 2026-08-16 后继续使用 Pro 峰谷价；2026-08-16 之前的日志仍保留旧价分支。正式 `/api/pricing` 当前三个 Pro 键返回 `ModelRatio=0.2175`、`CompletionRatio=2`、`CacheRatio=0.008333333333333333` 和 `billing_mode=tiered_expr`。本轮未将 Pro 误改为 Flash，也没有新增缓存创建价。

同一轮对正式目录重新分组核对：75 项中 19 项使用 `tiered_expr`，23 项使用标准 token 倍率，4 项使用已确认的按次非零价格，5 项为按次显式零价，24 项为尚无可安全写入当前结算链的 token 零价。剩余 24 项逐项属于当前公测免费、没有可确认独立 token 原价、动态路由/自托管模型，或按秒/分辨率计费而现有链路缺少必要计量字段；没有发现可以安全新增的官方价格项。Kun 的当前启用渠道状态已按上文更正，但仍保持 fail-closed。

验证结果：`go test -count=1 ./setting/billing_setting ./setting/ratio_setting ./pkg/pricingrepair ./bin` 退出码为 0；正式 `api.js GET /api/status` 返回 `success=true`、版本 `v1.1.0`；正式 `api.js GET /api/user/models` 和 `GET /api/pricing` 均返回 75 项；正式容器 `new-api` 当前为 `running/healthy`，`13000` 端口可用，`RestartCount=1`。本轮没有执行重启；该计数对应 05:50:22 UTC PostgreSQL fast shutdown、05:50:42 UTC 恢复期间的一次数据库启动竞态，`new-api` 首次连接收到 `57P03 database system is starting up` 后在 05:50:45 UTC 正常启动。

来源：

- https://api-docs.deepseek.com/quick_start/pricing
- https://api-docs.deepseek.com/updates

补充核查：Kimi 官方 `https://platform.kimi.ai/docs/llms.txt`、Model List 和 Model Inference Pricing 当前只列 `kimi-k3`、`kimi-k2.7-code`（含 highspeed）和 `kimi-k2.6`。`chat-k28.md` 与 `chat-k2.8.md` 虽返回 HTTP 200，但正文是通用 Quickstart 回退内容，未出现 K2.8 模型或价格表；因此没有 K2.8 的官方发布证据，本项目不新增该模型或猜测价格。

## 2026-09-23 GPT-6 Sol/Luna 与 Grok 4.7 官方价格维护

采样时间：2026-09-23（Asia/Shanghai）。本轮以供应商官方模型页和价格页为唯一价格来源，不使用任何第三方中转站（relay）暴露的 `ratio_config`、`/api/pricing` 或聚合站数据；这类站点的取值包含其自身加价和自建模型名，不能作为原厂价格依据。

本轮重新枚举正式活跃目录的未定价项（`channels.status=1` 的 `models` 与 `abilities.enabled=true` 取并集，再与 `ModelRatio`、`ModelPrice` 匹配），得到 11 项：`5.6-luna`、`composer-2.5`、`deepseek-v4.1-flash-expires-on-0910`、`gpt-6-luna`、`gpt-6-sol`、`gpt-reserve`、`grok-4.7`、`grok-composer-2.5-fast`、`grok-imagine-video`、`grok-imagine-video-1.5`、`kimi-k3-1`。逐项核对官方来源后，只有三个模型存在可确认的官方 token 价格，本轮据此写入。

写入前状态：`SelfUseModeEnabled=true`，未配置模型会落到 37.5 的保护性兜底倍率。该兜底显著高于这三个模型的官方原价，因此补价同时消除了超额计费风险。

| 模型 | 官方输入 | 官方缓存命中 | 官方缓存创建 | 官方输出 | 维护倍率 |
| --- | --- | --- | --- | --- | --- |
| `gpt-6-luna` | $0.1 / 1M | $0.01 / 1M | $0.125 / 1M | $0.5 / 1M | `ModelRatio=0.05`、`CompletionRatio=5`、`CacheRatio=0.1`、`CreateCacheRatio=1.25` |
| `gpt-6-sol` | $2 / 1M | $0.2 / 1M | $2.5 / 1M | $10 / 1M | `ModelRatio=1`、`CompletionRatio=5`、`CacheRatio=0.1`、`CreateCacheRatio=1.25` |
| `grok-4.7` | $2 / 1M | $0.5 / 1M | 未公布 | $6 / 1M | `ModelRatio=1`、`CompletionRatio=3`、`CacheRatio=0.25` |

`gpt-6-luna` 与 `gpt-6-sol` 官方模型页均标注：输入超过 272K tokens 的请求整体按 2 倍输入/缓存价、1.5 倍输出价计费。`grok-4.7` 官方价格页标注：prompt 达到 200K tokens 的请求整体按 `$4/$1/$12` 计费。三个模型因此都使用 `tiered_expr`，表达式沿用本仓库既有的 `gpt-6-astra`（272K）和 `grok-4.6`（200K）模板，阶梯条件使用 `len` 而非 `p`，避免缓存命中压低 `p` 造成误判档位。`grok-4.7` 官方未公布缓存创建价，表达式与倍率均不写入 `cc` 项。

正式库写入结果：`ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio`、`billing_setting.billing_expr`、`billing_setting.billing_mode` 六个 option 只做新增键合并，未覆盖任何既有条目。写入后正式 `/api/pricing` 返回 `gpt-6-luna` 为 `0.05/5/0.1/1.25`、`gpt-6-sol` 为 `1/5/0.1/1.25`、`grok-4.7` 为 `1/3/0.25`（无缓存创建价）。三个表达式均通过 `billingexpr.CompileFromCache` 编译校验。

写入过程中发现并纠正一次自身失误：首次写入复用了上一轮生成的 `after` 快照，该快照仍带有把 `kimi-k3-1` 当作 `kimi-k3` 别名、把 `5.6-luna` 当作 `gpt-5.6-luna` 别名、把 `deepseek-v4.1-flash-expires-on-0910` 当作 DeepSeek 别名的三个条目。这三个条目已随首次写入进入正式库，随后按写入前快照完整回滚（四个 key 与备份逐键 md5 一致后）再以干净集合重写。当前正式库已确认不含这三个键。

以下项目本轮保持未配置，均有明确依据：

- `kimi-k3-1`：Kimi 官方价格页（`chat.md`）、模型列表、API 参考、`llms.txt` 和 Platform Changelog 当前只有 `kimi-k3`（$3/$0.3/$15 每 1M）及 `kimi-k2.7-code`、`kimi-k2.6`；2026 年 9 月的 Changelog 条目只有 Web Search API 和计费方式变更，7 月条目为 Kimi K3 上线。没有任何官方来源发布 `kimi-k3.1` 或 `kimi-k3-1`。无法确认官方原价，不补价。
- `5.6-luna`：OpenAI 官方模型列表中不存在该 ID，官方对应模型为 `gpt-5.6-luna`（$0.2/$0.02/$1.2，已按官方价维护）。该键为渠道侧自建名，不静默继承 `gpt-5.6-luna` 价格。
- `deepseek-v4.1-flash-expires-on-0910`：DeepSeek 官方要求使用 `deepseek-flash` 作为模型名；`deepseek-v4-flash` 与 `deepseek-v4-flash-vision-exp` 已退役并按 Flash 价计费。官方没有 `-expires-on-` 形式的 ID，该后缀为渠道侧自建，不继承。
- `gpt-reserve`：OpenAI 官方模型页返回 404，不存在该官方模型，不补价。
- `composer-2.5`、`grok-composer-2.5-fast`：xAI 官方价格页的语言模型表中没有 Composer 条目，无可确认的官方独立 token 原价，继续保持 fail-closed。
- `grok-imagine-video`、`grok-imagine-video-1.5`：官方按秒计费（$0.050/s、$0.080/s），而 `relay/channel/xai` 没有视频时长与分辨率计量入口，写入按次 `ModelPrice` 会把时长计费错算为单次固定价。与 2026-08-16 起的既有结论一致，保持显式未配置。

同时复核了原厂侧的近期发布：Google 官方 Changelog 显示 2026-09-02 `gemini-3.8-flash` GA、2026-09-15 `gemini-3.8-live` 与 `gemini-3.8-live-extended-thinking` GA、2026-09-03 `lyria-3.5` GA，但这些模型均不在正式活跃目录中；`gemini-3.8-flash` 的官方价格标注为限时价（至 2026-12-31），按本项目"不采用促销价"的口径仍不写入。Z.AI 官方定价页当前最新为 GLM-5.3/GLM-5.3-Flash/GLM-5.3-FlashX 和 GLM-5.2，与已维护值一致，9 月无新增模型。DeepSeek 官方 News 索引当前最新为 2026-09-10 的 `DeepSeek-V4.1-Flash`，其价格已由既有 `tiered_expr` 峰谷表达式按官方价维护。OpenAI 官方模型列表当前新增项为 `gpt-6-luna`、`gpt-6-sol` 和已有的 `gpt-6-astra`，无其它未维护的语言模型。

本轮不做历史日志回算：`kimi-k3-1`、`5.6-luna`、`deepseek-v4.1-flash-expires-on-0910` 曾在首次写入时短暂进入正式库，需确认该窗口内没有产生计费日志。近 14 天正式库消费日志中，这三个模型名的记录数均为 0；11 个未定价项中只有 `grok-composer-2.5-fast` 有 1 条记录。历史修正仍按用户决定另行处理。

验证结果：`go test ./setting/ratio_setting/` 退出码为 0；`go build ./setting/... ./pkg/billingexpr/` 通过；`gofmt -l` 对改动文件无输出；正式 `/api/status` 返回 `success=true`、版本 `v1.1.0`；正式 `/api/pricing` 已返回上述三个模型的新值，服务日志显示 `syncing options from database` 正常周期同步，未重启容器。写入前快照为 `/tmp/opt-<key>-before-20260923T122636.json`，写入后为 `/tmp/opt-<key>-clean-20260923T122636.json`，`billing_expr` 写入前快照为 `/tmp/opt-billing_expr-before-20260923T122636.json`。

源码侧已在 `setting/ratio_setting/model_ratio.go` 与 `setting/ratio_setting/cache_ratio.go` 补齐这三个模型的基础倍率默认值，使源码默认与正式库一致；分档表达式仅存在于正式库 `billing_setting.billing_expr`，源码默认值不含表达式。

来源：

- https://developers.openai.com/api/docs/models/gpt-6-luna.md
- https://developers.openai.com/api/docs/models/gpt-6-sol.md
- https://developers.openai.com/api/docs/models.md
- https://docs.x.ai/developers/pricing.md
- https://docs.x.ai/developers/release-notes.md
- https://platform.kimi.ai/docs/pricing/chat.md
- https://platform.kimi.ai/docs/platform-changelog.md
- https://platform.kimi.ai/docs/llms.txt
- https://api-docs.deepseek.com/quick_start/pricing
- https://docs.z.ai/guides/overview/pricing
- https://ai.google.dev/gemini-api/docs/changelog

## 2026-09-23 多厂商新模型官方价格补齐（第二轮）

采样时间：2026-09-23（Asia/Shanghai）。本轮把枚举范围从"活跃目录"扩大到"全部渠道（含禁用状态）+ abilities + 近 30 天消费日志"，共得到 323 个模型名，其中 83 个无价格。用真实消费排序后确认 13 个模型在有流量却按 37.5 的保护性兜底倍率计费。价格来源全部为供应商官方模型页或价格页，未使用任何第三方中转站数据。

### 已写入的官方价格

| 模型 | 官方输入/缓存命中/输出 | 维护倍率 |
| --- | --- | --- |
| `claude-fable-5-1` | $10 / $0.25 / $50（缓存写 $12.50） | `5.0` / `5` / `0.025` / `1.25` |
| `claude-opus-5-5` | $4 / $0.20 / $20（缓存写 $5） | `2.0` / `5` / `0.05` / `1.25` |
| `gemini-3.8-flash` | $1.50 / $0.15 / $7.50 标准价 | `0.75` / `5` / `0.1` |
| `glm-5.3-flashx` | $0.37 / $0.075 / $1.25 | `0.185` / `3.378378` / `0.202703` |
| `mimo-v2.6-flash` | $0.14 / $0.0028 / $0.28 | `0.07` / `2` / `0.02` |
| `mimo-v2.6-pro` | $0.435 / $0.0036 / $0.87 | `0.2175` / `2` / `0.008275862` |
| `mimo-v2.6-pro-ultraspeed` | $4.35 / $0.036 / $8.7 | `2.175` / `2` / `0.008275862` |
| `doubao-seed-2.1-pro` | ¥6 / ¥1.2 / ¥30 | `0.410958904` / `5` / `0.2` |
| `doubao-seed-2.1-turbo` | ¥3 / ¥0.6 / ¥15 | `0.205479452` / `5` / `0.2` |
| `doubao-seed-2.1-lite` | ¥0.8 / ¥0.16 / ¥2.7 | `0.054794521` / `3.375` / `0.2` |
| `step-5-preview` | ¥7 / ¥0.35 / ¥20 | `0.479452055` / `2.857142857` / `0.05` |
| `step-3.5-flash` | ¥0.7 / ¥0.14 / ¥2.1 | `0.047945205` / `3` / `0.2` |
| `qwen3-embedding-8b` / `-4b` / `-0.6b` | SiliconFlow ¥0.28 / ¥0.14 / ¥0.07 | `0.02` / `0.01` / `0.005`，`CompletionRatio=1`、`CacheRatio=1` |

`gemini-3.8-flash` 按仓库既有口径采用标准价，不采用 Google 标注的 2026 年底前限时价（与 `gemini-3.7-flash`、`gemini-3.6-flash` 处理一致）。人民币价格按 `元/1000*RMB`（`RMB=500/7.3`）折算，该公式已用正式库中 `step-3.7-flash`（¥1.35 → 0.09246575342465753）和 `qwen3.8-max`（¥12 → 0.8219178082191781）两个锚点反推确认。

### 一并修复的归一化缺口

`normalizeDoubaoModelVariant` 只处理 `doubao-seed-1-6` → `doubao-seed-1.6` 和 `seed-1-6` → `seed-1.6`，缺少 2.x 规则。上游把 Doubao Seed 2.1 写成 `doubao-seed-2-1-pro`（连字符），而官方 ID 是 `doubao-seed-2.1-pro`（点号），导致该写法无法解析到已配置价格。已补充 `doubao-seed-2-1` → `doubao-seed-2.1` 和 `seed-2-1` → `seed-2.1`，两种写法现在解析到同一价格。

### 无前缀向量键补齐

上游与消费日志使用无前缀写法 `Qwen3-Embedding-8B`，归一化会将其映射为 `qwen3-embedding-8b`，但此前该键只存在于正式库、源码默认值中没有对应项，源码构建后请求会落到 37.5 兜底。已在源码补齐无前缀键，与带前缀键同价。

### 修正的两处自身误改

本轮出现过两次基于名称相似推断归属的错误，均由既有测试拦截并回滚：

1. `stepfun-ai/Step-3.5-Flash` 带有 `stepfun-ai/` 前缀，一度被当作阶跃官方模型的别名而改为阶跃官方价。该键实际属于 SiliconFlow 托管定价，`TestSiliconFlowQualifiedPricingUsesCurrentOfficialRates` 明确锁定其值为 `0.05` / `3` / `1`。源码与正式库均已回滚。
2. `Qwen/Qwen3-Embedding-8B` 等带前缀键的值 `0.02` / `0.01` / `0.005` 一度被按精确公式改写。这些是仓库已锁定的 SiliconFlow 口径，测试同样拦截。已回滚。

两处的教训一致：带厂商路径前缀的键可能指向第三方托管平台而非原厂，改动前必须先查测试锁定值与该键的既有语义。

### 保持未配置的模型

- `kimi-k3-1`：Kimi 官方全部来源（定价页、模型列表、API 参考、`llms.txt`、Platform Changelog、国际站与美国站）均只有 `kimi-k3`（$3/$0.3/$15），无 k3.1 或 `-1` 变体。
- `5.6-luna`、`deepseek-v4.1-flash-expires-on-0910`、`gpt-reserve`、`composer-2.5`、`grok-composer-2.5-fast`：渠道侧自建名，无对应官方 ID（`gpt-reserve` 官方页返回 404）。
- `hy3`、`hy4`、`hy4-preview`：腾讯官方模型列表中不存在该命名，官方只用 `hunyuan-*`。腾讯官方 token 后付费价已查明（`hunyuan-a13b` ¥0.5/¥2、`hunyuan-role-latest` ¥2.4/¥9.6、`hunyuan-turbos-vision` ¥3/¥9 等），但无法确定 `hy3`/`hy4` 的对应关系，不静默套用。
- `step-router-v1`：阶跃官方明确说明该模型按请求实际命中的底层引擎计费（`deepseek-v4-pro` 或 `step-3.7-flash`），本身没有独立 token 单价。
- `sensenova-*`：商汤平台定价页与 SenseCore 文档页当前均不可达（404）。
- `spark-x2.5`：讯飞星火官方价格页当前返回 403/404/500。
- `grok-imagine-video`、`grok-imagine-video-1.5`：xAI 官方按秒计费（$0.050/s、$0.080/s），而 `relay/channel/xai` 没有视频时长与分辨率计量入口，写入按次 `ModelPrice` 会把时长计费错算为单次固定价。
- `Kun`、`qdama-standard`、`auto`、`nvidia/nemotron-*` 及未列出的第三方或自托管路由：无官方独立 token 原价。

### 小米 MiMo 版本状态

MiMo 官方价格页已明确标注 `mimo-v2.5` 与 `mimo-v2.5-pro` 将于 2026-10-21 弃用，当前在售版本为 `mimo-v2.6-pro`、`mimo-v2.6-flash`、`mimo-v2.6-pro-ultraspeed`。正式库此前只有 v2.5 系列（渠道、`abilities`、消费日志中均无 v2.6），本轮已补齐 v2.6 三个模型。v2.5 系列的现有价格保持不动，待官方正式弃用后再评估。

### 正式库写入范围

本轮修改 `ModelRatio`、`CompletionRatio`、`CacheRatio` 三个 option，以及此前一轮的 `CreateCacheRatio`、`billing_setting.billing_expr`、`billing_setting.billing_mode`。全部采用只新增键的合并写入，未覆盖任何既有条目。写入使用带 `BEGIN`/`COMMIT` 的、以 `$json$...$json$` 美元引号包裹的 SQL 文件经 `docker exec -i postgres psql` 执行；此前尝试的 `psql -v` 变量替换方式在超大 JSON 值上会失败且不报错，已弃用。

### 验证结果

`go build ./setting/...` 通过；`go test ./setting/ratio_setting/` 通过；`gofmt -l` 对改动文件无输出；正式 `/api/pricing` 返回已写入模型的新值；正式 `/api/status` 返回 `success=true`、版本 `v1.1.0`。源码默认值与正式库逐键一致。提交 `6794b63a5`（本轮）与 `5af49fd51`（GPT-6 Sol/Luna 与 Grok 4.7）均只包含本任务的改动，其他会话在工作区的未提交改动未被带入。

### 尚未执行

历史日志回算未在本轮进行。`Qwen3-Embedding-8B`（3 条）、`gemini-3.8-flash`（3 条）、`hy4-preview`（1 条）、`step-router-v1`（1 条）等按兜底倍率计费的历史记录，以及更早 13 个模型的历史日志，需要按本文档"历史日志修正规则"单独设计一轮修正。

来源：

- https://platform.claude.com/docs/en/about-claude/models/overview.md
- https://platform.claude.com/docs/en/about-claude/pricing.md
- https://ai.google.dev/gemini-api/docs/pricing.md
- https://docs.z.ai/guides/overview/pricing.md
- https://mimo.mi.com/static/docs/price/pay-as-you-go.md
- https://www.volcengine.com/docs/82379/1099320
- https://platform.stepfun.com/docs/zh/pricing/details
- https://siliconflow.cn/pricing
- https://cloud.tencent.com/document/product/1729/97731

## 2026-09-24 doubao-seed-2.1 源码默认倍率 500 倍修正

采样时间：2026-09-24（Asia/Shanghai）。本轮在源码默认倍率与正式库 `ModelRatio` 的全量对照中发现三处算术错误。

### 缺陷

`setting/ratio_setting/model_ratio.go` 中三个 Doubao Seed 2.1 条目使用了错误的换算因子：

```text
修正前： "doubao-seed-2.1-pro":   6.0 / 2 * RMB
修正后： "doubao-seed-2.1-pro":   6.0 / 1000 * RMB
```

官方单价为人民币每 1M tokens，正确倍率是 `(单价 / USD2RMB) / 2`。本文件通行的写法是 `单价 / 1000 * RMB`，两者恒等：

```text
¥6  / 1000 * RMB = 0.4109589041095891
(6 / 7.3) / 2    = 0.4109589041095891
```

`/ 2 * RMB` 少了 1000 这个量纲因子，结果偏高 500 倍。全文件 32 处人民币条目中只有这 3 处用了 `/ 2 * RMB`（另一处 `/ 10 * RMB` 是 `stepaudio-2.5-tts` 按「每 10K 输入字符」计费的独立单位，有注释说明，不属此类）。

### 三个键的对照

| 键 | 官方价 | 错误值 | 正确值 |
| --- | --- | --- | --- |
| `doubao-seed-2.1-pro` | ¥6 / 1M | `205.479452` | `0.410958904` |
| `doubao-seed-2.1-turbo` | ¥3 / 1M | `102.739726` | `0.205479452` |
| `doubao-seed-2.1-lite` | ¥0.8 / 1M | `27.3972603` | `0.054794521` |

本文件 2026-09-23 一节的价格表（`doubao-seed-2.1-pro` 为 `0.410958904` / `5` / `0.2`）与正式库现值都取正确值，只有源码默认倍率写错，属于同一批改动中的笔误。

### 影响面

- **正式库不受影响**：三个键在正式 `ModelRatio` 行中均存在且取正确值。由于 `types.LoadFromJsonStringWithCallback` 是整体替换（`m.data = make(...)`），有该行的部署不会读到源码默认值。
- **全新部署受影响**：`ModelRatio` 行为空时由 `InitRatioSettings()` 从源码默认值播种，此时 `doubao-seed-2.1-pro` 按 `ratio=205.479452` 计费，即 `$410.96/1M` 而非 `$0.82/1M`。
- **隔离开发库受影响**：`new-api-dev` 的 `ModelRatio` 行不含任何 `doubao` 键，实测该模型解析为 `ratio=37.5, ok=false`，走「未定价模型」兜底而非 500 倍值。

### 修复与验证

- 三个条目改为 `/ 1000 * RMB`。
- 新增回归测试 `TestDoubaoSeed21RatiosMatchDocumentedPrices`，断言三个键的倍率等于官方人民币价按 `(单价 / USD2RMB) / 2` 推出的值，并直接校验 `/ 1000 * RMB` 与 `(单价 / USD2RMB) / 2` 恒等。已验证把任一条目改回 `/ 2 * RMB` 后测试失败。

```text
go test -count=1 ./setting/ratio_setting/    ok
go build ./...                               exit 0
gofmt -l（全部已跟踪 Go 文件）                无输出
```

### 未处理

同轮对照还发现 4 处源码默认倍率与正式库不一致（`gemini-2.5-pro` `0.625` vs `1`、`kimi-k2-thinking` `0.3` vs `0.2`、`qwen3-vl-30b-a3b-instruct` / `-thinking` `0.075` vs `0.1`），以及 2 处源码为 `0.5`、正式库为 `0`（`llama-3-sonar-large-32k-chat` / `-online`）。这些不是算术错误，需要各自的官方价格依据才能判定方向；本轮已核对 Fireworks 价格页，该页已不再列出 `kimi-k2-thinking`、`kimi-k2p5`、`glm-4p7`、`glm-5` 等条目，无法据此判定，**未改动**。这些键在正式库均无启用路由。同轮还对全部 8 张倍率表（`ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio`、`ModelPrice`、`ImageRatio`、`AudioRatio`、`AudioCompletionRatio`）做了同样对照，除上述 6 处外其余差异均为 float64 往返噪声（相对偏差小于 1e-12）；`CacheRatio` 另有 2 处、`CompletionRatio` 另有 2 处差异，键与 `ModelRatio` 的 6 处部分重叠（`kimi-k2-thinking`、`qwen3-vl-30b-a3b-thinking`）。

可达性核实：6 个键在正式库 `abilities` 中启用数均为 0；日志中仅 `qwen3-vl-30b-a3b-instruct` / `-thinking` 各 1 条（id `949361`、`949362`，2026-07-04 的管理端探测，`quota=0`、token 全 0，无计费影响）。
