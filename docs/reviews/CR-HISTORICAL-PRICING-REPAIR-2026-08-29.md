# 历史价格修正复核：2026-08-29

## 结论

正式 PostgreSQL 已完成全部可按已确认官方价格安全回算的历史 `type=2` 钱包日志修正。本轮同时复核 xAI Grok 系列并修正其路径别名历史账单；`glm-5.3-flash` 按 Z.AI 官方原价维护，但正式库没有该模型历史消费日志。

## 范围与结果

修正条件为：`other.model_price < 0`、价格快照完整、非订阅、非 `tiered_expr`、非按次或媒体/搜索附加计费。

| 项目 | 数值 |
| --- | ---: |
| 修正日志 | 6,728 |
| 旧 quota 合计 | 30,576,174 |
| 新 quota 合计 | 30,380,902 |
| quota 差额 | -195,272 |
| `glm-5.3-flash` 历史命中 | 0 |

涉及模型：`minimax-m2p5`、`mimo-v2.5`、`mimo-v2.5-free`、`step-3.7-flash`、`kimi-k2.7-code`、`qwen3.8-max` 和少量 `gpt-oss-20b`。

## 执行与验证

- 初次无边界全量 dry-run 因一次装载约 200 万条日志退出 `137`；修正器增加 `-min-id`、`-max-id` 后按 25 万条区间分片执行。
- 10 个 ID 分片全部 apply 成功；每个非空分片均同步 `logs`、`users`、`tokens`、`channels`、`quota_data`，并清理正式 Redis 用户/令牌缓存。
- 全部 ID 分片再次 dry-run：每片 `repaired_logs=0`。
- 受影响小时聚合复核：`mismatched_buckets=0`。
- 相关测试：`go test -count=1 ./bin ./pkg/pricingrepair ./setting/ratio_setting ./setting/billing_setting`，退出码 `0`。
- `git diff --check`（相关文件），退出码 `0`。
- 正式 `http://127.0.0.1:13000/api/status` 返回 `success=true`；容器 `running/healthy`，`RestartCount=0`。
- 正式 `/api/pricing` 的 `glm-5.3-flash` 为 `ModelRatio=0.075`、`CompletionRatio=3.3333333333333335`、`CacheRatio=0.2`、`CreateCacheRatio=0`。

## 备查与边界

正式库备查表：

- `pricing_repair_20260829_all_logs`：12,151 条
- `pricing_repair_20260829_all_users`：1 条
- `pricing_repair_20260829_all_tokens`：2 条
- `pricing_repair_20260829_all_channels`：6 条
- `pricing_repair_20260829_all_quota_data`：174 条

正式服务在执行期间持续产生实时请求，因此用户/令牌总余额快照的变化还包含并发请求，不能把余额表的总差额直接等同于本次修正差额。两条变更日志引用已删除的渠道 `140`、`172`，无现存渠道行可更新；日志、用户/令牌及小时聚合仍已处理。

`tiered_expr`、订阅、按次、图片、音频、搜索及无可确认 token 单价的日志保持历史快照，避免使用标准倍率公式强行回算。

## 2026-08-29 xAI Grok 官方原价复核与路径别名修正

采样时间：2026-08-29（Asia/Shanghai）。直接读取 xAI 官方模型数据，按普通 API 原价维护，明确排除 Batch 折扣。官方价格单位为 USD / 1M tokens：

| 官方模型 | 200K 以内输入/缓存命中/输出 | 超过 200K 输入/缓存命中/输出 | 官方别名或变体 |
| --- | ---: | ---: | --- |
| `grok-4.3` | `$1.25/$0.20/$2.50` | `$2.50/$0.40/$5.00` | `grok-4.3-latest` |
| `grok-4.20` 系列 | `$1.25/$0.20/$2.50` | `$2.50/$0.40/$5.00` | reasoning、non-reasoning、multi-agent 及 beta/date 变体 |
| `grok-4.5` | `$2.00/$0.30/$6.00` | `$4.00/$0.60/$12.00` | `grok-4.5-latest`、`grok-build-latest` |
| `grok-4.6` | `$2.00/$0.50/$6.00` | `$4.00/$1.00/$12.00` | 无独立别名 |
| `grok-build-0.1` | `$1.00/$0.20/$2.00` | `$2.00/$0.40/$4.00` | `grok-code-fast-1`、`grok-code-fast`、`grok-code-fast-1-0825` |

官方页面同时列出部分模型 20% Batch 折扣；本项目不使用该折扣，`billing_setting` 仅写入上述普通 API 原价阶梯表达式，`len <= 200000` 归入标准档。正式 `http://127.0.0.1:13000/api/pricing` 复核到 10 个 Grok 目录项，其中已确认的 4.3、4.20、4.5、4.6 和 Build 变体均与该表一致；通用 Grok、Grok 3 Mini、Composer、搜索/视频/语音模型没有当前可确认的同口径 token 单价，继续保持未配置。

历史修正器对明确记录为 Grok 4.5 旧 37.5/1/1 fallback 快照的 4 条路径别名执行官方表达式回算：

| 路径别名 | 旧 quota | 新 quota |
| --- | ---: | ---: |
| `xai/grok-4.5` | 12000 | 381 |
| `x-ai/grok-4.5` | 16313 | 726 |
| `grok/grok-4.5` | 13538 | 645 |
| `grok-4-5` | 不变 | 仅归一化价格元数据 |

本组 quota 合计差额为 `-40099`。正式库同步更新 `logs`、`users`、`channels` 和 `quota_data`，并清理用户缓存；二次限定范围 dry-run 为 `repaired_logs=0`，受影响 `quota_data` 与日志逐项一致。另有 76 条 2026-07-09 的 `grok-4.5` 普通日志仍保留当时已记录的 `$0.50` 缓存价格快照，原因是 xAI 当前页面没有给出 `$0.50` 到 `$0.30` 的历史生效时间；另有 33 条旧版/搜索/图像等 Grok 记录没有可确认的 token 单价，均不强行套价。

## 2026-08-29 Gemini 3.6/3.7 标准原价校正

Google 官方价格页将 Gemini 3.6 Flash 和 Gemini 3.7 Flash 的 `$0.75/$0.075/$3.75` 标明为 2026-12-31 前限时价格，标准价格自 2027-01-01 为 `$1.50/$0.15/$7.50`（输入/缓存命中/输出，每 1M tokens）。遵循“不看促销价”的维护口径，源码和正式 PostgreSQL 的 `ModelRatio` 已从 `0.375` 改为 `0.75`，标准表达式改为 `p * 1.5 + c * 7.5 + cr * 0.15`；`CompletionRatio=5`、`CacheRatio=0.1` 不变。正式库和 3001 隔离库均已更新并保留旧值备查表 `pricing_repair_20260829_gemini_standard_options`；正式库历史日志查询未发现这两个模型的消费记录，因此没有历史金额需要回算。

来源：

- https://docs.x.ai/developers/models
- https://docs.x.ai/developers/pricing
- https://ai.google.dev/gemini-api/docs/pricing
