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

官方页面同时列出部分模型 20% Batch 折扣；本项目不使用该折扣，`billing_setting` 仅写入上述普通 API 原价阶梯表达式，`len <= 200000` 归入标准档。正式 `http://127.0.0.1:13000/api/pricing` 复核到 10 个 Grok 目录项，其中已确认的 4.3、4.20、4.5、4.6 和 Build 变体均与该表一致；截至该次采样，通用 Grok、Grok 3 Mini、Composer、搜索/视频/语音模型尚未确认同口径 token 单价。Grok 3 Mini 的旧结论已由本记录末尾 2026-08-30 的 xAI `llms.txt` 复核 supersede。

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

## 2026-08-29 全量补齐执行收尾

在既有全量历史修正之后，本轮继续把正式活跃目录中已确认官方原价但数据库缺少显式键的模型补齐到正式 PostgreSQL 和 3001 隔离 PostgreSQL：

- MiniMax-M3 及大写/供应商路径别名使用永久折后阶梯 `$0.30/$0.06/$1.20` 和长上下文 `$0.60/$0.12/$2.40`，priority 按 1.5 倍计算；官方页面明确标注 `Permanent 50% off`，不是限时促销。
- Agnes 2.5 Pro Beta 使用 `$0.10/$0.01/$0.30`。
- Qwen3.8-27B 及连字符、大小写别名使用 `¥3/¥0.6/¥12`，显式缓存创建 `¥3.75`；源码同时修正 `qwen3.8-max` 的整数除法字面量，避免 `12 / 1000` 变成零。
- Kimi K2.7 Code 使用 `$0.95/$0.19/$4.00`，highspeed 使用 `$1.90/$0.38/$8.00`。
- Gemini 3.6/3.7 的 preview、latest 和 high/low/medium/tiered 后缀全部改为标准 `$1.50/$0.15/$7.50`，清除旧促销表达式；Gemini 3.5 和 Gemini 3.1 Pro 的已确认控制后缀也显式同步。

生产库选项事务提交前备份为 `/tmp/new-api-pricing-options-before-20260829T154213.tsv`，3001 隔离库备份为 `/tmp/new-api-dev-pricing-options-before-20260829T154222.tsv`。生产针对本组模型的修正器 dry-run 返回 `repaired_logs=0`，因此没有历史金额需要 apply。3001 同范围 dry-run 因隔离 PostgreSQL 长时间磁盘 I/O 未完成，已停止本轮只读进程，未执行任何历史写入；该边界不影响已提交的 options 事务。

生产活跃模型缺价查询已排除本轮补齐条目，剩余 Composer、通用 Grok、SenseNova 公测路由及视频/音频/按秒模型仍无可确认且可由当前 token 计费链精确表达的独立价格，保持 fail-closed；Grok 3 Mini 已在 2026-08-30 补齐。正式服务端口 `13000` 和 3001 端口均可建立 TCP 连接；本轮未重启或重建正式 `new-api` 容器。生产容器在高并发期间的健康探针存在超时记录，属于运行时负载边界，不作为价格同步成功依据。

## 2026-08-29 qwen3.8-max 漏项补修

复核历史日志时发现两条 `qwen3.8-max` 记录仍为 `model_ratio=0`、`quota=0` 的旧快照。按修正器 dry-run 结果执行正式 apply：两条日志各修正为 `quota=83`，合计增加 `166`；两个 `quota_data` 小时聚合各增加 `83`，渠道累计同步增加 `166`，用户缓存已通过正式 Redis 清理。apply 后同范围 dry-run 返回 `repaired_logs=0`。

本组备查表为 `pricing_repair_20260829_qwen38max_logs`（2）、`_users`（1）、`_tokens`（0）、`_channels`（1）和 `_quota_data`（2）。用户表在正式并发请求下同时发生实时变化，故不以用户余额总差额代替本组修正差额。正式容器 `RestartCount=0`，未重启或重建。

## 2026-08-30 全部可确认官方价格收尾

采样时间：2026-08-30（Asia/Shanghai）。本轮以正式服务 `13000`、3001 隔离服务和两套 PostgreSQL 的当前配置为准，复核此前新增模型、历史修正边界及 Grok 系列价格。用户要求按官方原价维护，因此没有采用限时促销、Batch 折扣或第三方渠道自定义价。

### 配置和运行状态

- 正式 PostgreSQL `postgres/new-api` 与 3001 隔离 PostgreSQL `new-api-dev` 均已提交 Agnes Video 2.5/2.5 Flash、Hunyuan-A13B-Instruct、MiniMax M3、Qwen3.8-27B、Kimi K2.7 Code、StepAudio 2.5、Gemini 3.6/3.7 及已确认 Grok 4.3/4.20/4.5/4.6/Build 变体的官方原价配置。正式库和隔离库的 options 变更均保留了事务前 TSV 备份。
- `http://127.0.0.1:13000/api/status` 和 `http://127.0.0.1:3001/api/status` 均返回 HTTP 200、`success=true`；两个 `new-api` 容器当前为 `running/healthy`，`RestartCount=0`。正式容器未重启，隔离容器使用当前 dirty 工作区重建。
- 正式 `13000/api/pricing` 已返回上述新增或校正模型及其 `ModelRatio`、缓存/输出倍率或 `tiered_expr`；3001 的 `/api/pricing` 也可正常读取，当前隔离库按自身 47 个活跃渠道展示模型，未配置到隔离渠道的生产模型不会在该响应数组中出现，但两套 `options` 数据库已核对同步。正式容器镜像仍为 `new-api-local:prod-codex-lite-4527d7666`；3001 隔离镜像 `new-api-local:dev` 的 build-info 为 `v1.1.0`、commit `6f064edb0ca606f7e9caca2bd72163643717e394-dirty`。

### 历史数据

- 既有全量普通 token 日志修正为 6,728 条；`qwen3.8-max` 漏项另修正 2 条，每条 `quota=0` 改为 `83`，合计增加 `166`。上述修正均同步 `logs`、用户/令牌余额、渠道累计、`quota_data` 和 Redis 缓存，并在 apply 后以限定范围 dry-run 复核为 `repaired_logs=0`。
- Agnes Video 2.5/2.5 Flash、Hunyuan-A13B-Instruct、MiniMax M3、Qwen3.8-27B、Kimi K2.7 Code、StepAudio 2.5 和本轮新增的标准 Gemini/Grok 价格键没有历史有效消费日志需要追加回算；因此没有为了制造差额而写入历史账单。
- 历史中的 `tiered_expr`、订阅、按次、媒体/搜索附加计费、第三方自定义价以及缺少可确认官方 token 单价的记录仍保留原快照。不能用普通倍率公式替代这些不同计量口径。

### 活跃目录缺价边界

此前快照中的正式活跃目录共 152 个模型键、35 个未配置项；截至 2026-08-30 本轮复核，正式目录为 95 个模型键、9 个未配置项，3001 隔离目录为 199 个模型键、34 个未配置项。剩余项目主要是第三方/自托管路由、自动路由、未公开独立价格的 Grok Composer、搜索或代理别名、仅有免费/公测或渠道相关口径的 SenseNova 模型，以及按图片、音频、秒数或本地推理计费的模型（Flux 等）。Grok 3 Mini、StepAudio ASR 和标准 StepAudio TTS 已按官方原价补齐；剩余模型没有当前 token 结算链可表达的、可确认的官方原价，继续 fail-closed；不套用相近模型，不把缺价数量压到零作为验收标准。

## 2026-08-30 Grok 3 Mini 官方价格补齐与历史修正

采样时间：2026-08-30（Asia/Shanghai）。xAI 官方 `https://docs.x.ai/llms.txt` 明确记录 `grok-3-mini` 的普通 API 原价为输入 `$0.30`、缓存命中 `$0.075`、输出 `$0.50` / 1M tokens；`grok-3-mini-fast` 是同一官方价格对象的官方别名。本项目按用户要求使用普通 API 原价，不使用 Batch 或促销价。

源码新增两个显式价格键并校正 beta/latest 别名：`ModelRatio=0.15`、`CompletionRatio=5/3`、`CacheRatio=0.25`。同一价格对象的别名包括 `grok-3-mini-latest`、`grok-3-mini-beta`、`grok-3-mini-fast`、`grok-3-mini-fast-latest` 和 `grok-3-mini-fast-beta`。正式 PostgreSQL 和 3001 隔离 PostgreSQL 均事务合并了这三项 options，写入前备份表为 `pricing_repair_20260830_grok3mini_options`，别名校正另保留 `pricing_repair_20260830_grok3mini_alias_options`。

正式日志 `447511` 的旧快照为 `grok-3-mini`、prompt `13`、completion `15`、quota `1050`、`model_ratio=37.5`、`completion_ratio=1`、`cache_ratio=1`。pricing-repair dry-run 计算新 quota `6`，apply 后同步了 `logs`、用户、令牌、小时 `quota_data`，并清理了 2 个 Redis 缓存键；限定范围二次 dry-run 为 `repaired_logs=0`。修正后日志快照为 `model_ratio=0.15`、`completion_ratio=1.6666666666666667`、`cache_ratio=0.25`，小时聚合 quota 为 `6`。

正式 `http://127.0.0.1:13000/api/status` 与隔离 `http://127.0.0.1:3001/api/status` 均返回 HTTP 200、`success=true`。本轮没有为 Composer、通用 `grok`、视频、音频、按秒、自动路由或第三方自托管模型套用相近价格。

## 2026-08-30 StepAudio ASR 可表达价格补齐

StepFun 官方价格页明确 `stepaudio-2.5-asr` 为 `¥0.15/小时`。项目的音频转写预估已按每分钟 1,000 个音频计费 token 归一化，因此换算为 `¥2.5/1M tokens`，源码和正式/隔离 PostgreSQL 均补齐 `ModelRatio=2.5/1000*RMB`、`CompletionRatio=0`、`AudioRatio=1`、`AudioCompletionRatio=0`。标准 `/audio/speech` 的 `stepaudio-2.5-tts` 按官方 `¥5.8/万字符` 补齐为 `ModelRatio=5.8/10*RMB`、`CompletionRatio=0`、`AudioRatio=1`、`AudioCompletionRatio=0`；官方另计的音色复刻按次费用不纳入标准合成请求。正式库和隔离库均没有这两个模型历史日志，未执行历史账单回算。

来源：

- https://docs.x.ai/llms.txt
- https://docs.x.ai/developers/models
- https://docs.x.ai/developers/pricing

### 官方来源

- Agnes：<https://wiki.agnes-ai.com/en/docs/agnes-video-25.md>、<https://wiki.agnes-ai.com/en/docs/agnes-video-25-flash.md>、<https://wiki.agnes-ai.com/en/docs/pricing.md>
- SiliconFlow：<https://www.siliconflow.cn/pricing>
- StepFun：<https://platform.stepfun.com/docs/zh/guides/pricing/details>
- xAI：<https://docs.x.ai/developers/models>、<https://docs.x.ai/developers/pricing>
- Kimi K3/K2.7 Code：<https://platform.kimi.ai/docs/pricing/chat-k3.md>、<https://platform.kimi.ai/docs/pricing/chat-k27-code.md>
- Z.AI：<https://docs.z.ai/guides/overview/pricing.md>
- Google Gemini：<https://ai.google.dev/gemini-api/docs/pricing>
- Qwen3.8-27B：<https://www.qianwenai.com/models/qwen3.8-27b>

上述链接为官方模型或价格页；维护值按普通 API 原价，不含促销价、Batch 折扣或无法确认生效时间的历史价格变更。

## 2026-09-05 永久降价判定纠正

此前按“原价维护”执行时，正式服务仍有一组 MiniMax M3 选项停留在折前值。重新读取 [MiniMax Pay as You Go](https://platform.minimax.io/docs/guides/pricing-paygo.md) 后确认，页面对 M3 当前价格明确标注 `Permanent 50% off`，不是限时促销：标准档为 `$0.30/$0.06/$1.20`，长上下文为 `$0.60/$0.12/$2.40`，priority 为标准档 1.5 倍。因此本项目应使用永久降价后的当前价格，而不是划线前 `$0.60/$0.12/$2.40`。

本轮通过 `newapi` skill 的 `api.js` 修正正式服务四个 M3 键（`MiniMax-M3`、`minimax-m3`、`minimaxai/MiniMax-M3`、`minimaxai/minimax-m3`）：`ModelRatio` 从 `0.30` 改为 `0.15`，并把 `billing_setting.billing_expr` 改为永久折后短/长上下文及 priority 表达式；`CompletionRatio=4`、`CacheRatio=0.2` 保持不变。写入前目标选项快照为 `/tmp/new-api-options-before-20260905T214956.json`。

同日对其他低价标记重新分类：OpenAI gpt-5.6-luna、Google Gemini 3.6/3.7 Flash、Z.AI GLM-5.3 Flash 和 Agnes Video 2.5 Flash 的官方页面分别写有结束日期或 `limited-time`，仍按标准 list price 维护；xAI Batch/Flex 仍是独立处理模式，不并入普通 API 价格。正式 M3 `type=2` 历史日志查询为 `0` 条，因此本次没有账单回算。

验证证据：

- `go test -count=1 ./setting/ratio_setting ./setting/billing_setting ./pkg/pricingrepair ./bin`：退出码 `0`。
- `nc -z -w 2 127.0.0.1 13000`：退出码 `0`。
- `curl --connect-timeout 5 --max-time 10 http://127.0.0.1:13000/api/status`：HTTP 成功，`success=true`。
- 正式 `api/pricing`：四个 M3 别名均返回 `model_ratio=0.15`、`completion_ratio=4`、`cache_ratio=0.2`、`billing_mode=tiered_expr`。
