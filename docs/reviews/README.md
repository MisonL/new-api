# 审计记录索引

`docs/reviews/` 存放阶段性审计、验证和收口记录。命名格式为 `CR-<主题>-<YYYY-MM-DD>.md`。

## 目录约定

- `docs/reviews/*.md`：当前仍被引用或仍有效的记录。
- `docs/reviews/archive/*.md`：已完成闭环、不再被现行文档引用的历史记录。内容保留，路径变更。

归档只改变位置，不删改内容。需要追溯早期结论时在 `archive/` 内检索。

## 阅读顺序

按任务类型选择入口，不必顺序通读。

### 接手与状态

| 记录 | 内容 |
| --- | --- |
| [CR-DEV-BRANCH-CHANGE-INVENTORY-2026-09-20.md](CR-DEV-BRANCH-CHANGE-INVENTORY-2026-09-20.md) | dev 分支已提交与未提交改动清单、提交拆分建议 |
| [CR-CURRENT-WORKTREE-CLOSEOUT-2026-09-14.md](CR-CURRENT-WORKTREE-CLOSEOUT-2026-09-14.md) | 工作区收口状态 |
| [CR-DOC-CODE-CONSISTENCY-2026-09-20.md](CR-DOC-CODE-CONSISTENCY-2026-09-20.md) | 文档与代码一致性复核与修复 |
| [CR-DOCKER-CLEANUP-PERF-2026-09-21.md](CR-DOCKER-CLEANUP-PERF-2026-09-21.md) | Docker 资源清理与性能排查（日志/备份/索引/参数） |
| [CR-LOG-INDEX-CONSOLIDATION-2026-09-21.md](CR-LOG-INDEX-CONSOLIDATION-2026-09-21.md) | logs 表索引精简（41→20）与一次性迁移 |
| [CR-LOG-INDEX-PROD-VERIFY-2026-09-23.md](CR-LOG-INDEX-PROD-VERIFY-2026-09-23.md) | logs 索引精简的生产侧核实（33/41 零扫描、`DROP INDEX` 锁代价）；二期候选裁决为空集 |
| [CR-GPT56-SOL-PRICE-DIRECTION-2026-09-24.md](CR-GPT56-SOL-PRICE-DIRECTION-2026-09-24.md) | gpt-5.6-sol 价格方向纠正（`$4/$20` 为当前价）、历史修正生效边界补全、生产侧只读分类核实 |
| [CR-DEEPSEEK-PRO-STALE-ROUTING-2026-09-24.md](CR-DEEPSEEK-PRO-STALE-ROUTING-2026-09-24.md) | deepseek-v4-pro 九个别名残留旧路由表达式（2026-09-14 后切 Flash 价）修复与只读可达性核实 |
| [CR-CODEX-PRICING-REVIEW-2026-09-24.md](CR-CODEX-PRICING-REVIEW-2026-09-24.md) | codex 对本轮计费改动的独立审查与逐条核验；4 条发现复现结论，另发现并修复其无数据库访问而未见到的 LegacyMonth 快照漏识别（隔离库 flash 按 1.4667 倍计价） |
| [CR-MULTI-AGENT-AUDIT-FIXES-2026-09-26.md](CR-MULTI-AGENT-AUDIT-FIXES-2026-09-26.md) | 5 个只读子代理审查的独立复现：修复 SSE 错误帧被计费为成功、MySQL 上路由权重保存回滚、npm codex 缓存永久陈旧、阶梯计费两阶段发布的半成品窗口、额度换算越界与 token 上限未校验、工具名别名缺反向映射、模型列表宣称不可路由内容共 7 条，另在排查中新发现标签级状态写入非原子、渠道删除遗留孤儿 ability、AddAbilities 冲突被静默跳过（第 8、9、10 条）；3 条代理 P1 经复现推翻；1 项确认隐患留待独立决策；另新增渠道状态变更审计能力（第 11 条）；能力漂移存量已在隔离库清零并结案（2026-09-28 用库内 05-25/06-09 两张备份表取证，确认两张快照各自漂移均为 0、06-09 之后 13 次禁用全部成对，漂移是修复前的瞬时半成品而非长期累积）；补做 4 项覆盖边界（真实 MySQL 8.4 上第 8 条原子回滚、真实上游+真实 HTTP 连接下 SSE 错误帧、`POST /api/channel` 真实 handler、两阶段发布两个方向的真实 HTTP 端点）；并定位并修复 `-shuffle` 长期失败的 4 个跨测试状态泄漏根因（共享句柄 `model.DB`、共享 `logs` 表、共享 `billingSetting.BillingExpr` 表），`model` 1..100 / `service` 1..30 / `controller` 1..40 / `setting` 1..60 seed 全部通过 |
| [CR-PROD-LOG-RETENTION-2026-09-21.md](CR-PROD-LOG-RETENTION-2026-09-21.md) | 正式环境日志保留策略启用与历史日志清理 |
| [CR-POSTGRES-TUNING-2026-09-21.md](CR-POSTGRES-TUNING-2026-09-21.md) | 正式 PostgreSQL 参数调优评估（未执行） |
| [CR-DEV-ISOLATED-VERIFY-INDEX.md](CR-DEV-ISOLATED-VERIFY-INDEX.md) | 隔离开发环境验证记录索引（3 次，主次标注） |
| [CR-DEV-LATEST-SOURCE-VERIFY-2026-09-22.md](CR-DEV-LATEST-SOURCE-VERIFY-2026-09-22.md) | 最新源码全量验证；发现 model_mapping 字面量 null 使渠道被静默排除 |
| [CR-MODEL-MAPPING-NULL-2026-09-22.md](CR-MODEL-MAPPING-NULL-2026-09-22.md) | model_mapping 字面量 null 归一化修复；静默失败补诊断 |
| [CR-JSON-HELPER-MIGRATION-2026-09-22.md](CR-JSON-HELPER-MIGRATION-2026-09-22.md) | encoding/json 统一迁移至 common 包装；顺带修两处既有吞错 |
| [CR-UNTRACKED-SOURCE-DEPENDENCY-2026-09-22.md](CR-UNTRACKED-SOURCE-DEPENDENCY-2026-09-22.md) | 31 个未跟踪源文件被已跟踪文件依赖，实证移走即构建失败 |
| [CR-CLAUDE-ANTHROPIC-AUTH-HEADER-2026-09-22.md](CR-CLAUDE-ANTHROPIC-AUTH-HEADER-2026-09-22.md) | Claude 渠道两处认证头构造点补齐 Authorization；确证上游只认 Bearer 导致 401 |
| [CR-FULL-DIMENSION-AUDIT-2026-09-23.md](CR-FULL-DIMENSION-AUDIT-2026-09-23.md) | 全维度审计（8 个只读子代理，部分结论经独立复测推翻重做）；计费、三库、并发、崩溃、日志注入、前端缺陷、i18n 空串回退与 ja 复数后缀 |
| [CR-ADMIN-RATE-LIMIT-EXEMPT-2026-09-29.md](CR-ADMIN-RATE-LIMIT-EXEMPT-2026-09-29.md) | 管理员豁免全局限流选项与后台开关（全局 API/页面/搜索/联想，CriticalRateLimit 与桌面 OAuth 轮询刻意不豁免）；顺带修复 classic 设置页字段被永久过滤丢弃的既有缺陷、补齐 compose 限流变量透传；隔离环境实测 10 组探针含反向不变量；复核三轮各修一处自身缺陷（禁用管理员仍被豁免、前端文案未登记 i18n、非规范布尔值如 `"True"`/`"1"` 被接受却静默失效——真值统一在 `normalizeOptionValueForInMemoryConfig` 归一化，提交 `3a273494a`）；正式环境镜像尚未重建，改动未部署 |
| [ORCHESTRATION-AUDIT-SUMMARY-2026-10-01.md](ORCHESTRATION-AUDIT-SUMMARY-2026-10-01.md) | 编排式复核（Orca `run_7b7371d11c39`，5 个只读 worker 按维度）：4 个完成、1 个（compose 透传）启动即失败无产出；4 个完成者的原始结论均为「逻辑正确 / 无 P0 缺陷」（它们复核的是已含 `02ab2cd0f`、`104a9ae44` 修复后的工作区）；worker 耗时取自 dispatch 时间戳。遗留项后续处理：跨文件限流器测试盲区与 `log_retention_setting` 空白残留已修（`b0f0bfb4e`），布尔选项归一化缺陷已修（`de563706d`），数字字段错配未复现已撤回，default 侧 fr/ja/ru/vi 未译已在 `10c6316d2` 补齐（以该提交树重跑 `i18n:sync` 零改动、六语种 missing 与 untranslated 均为 0） |
| [CR-FULL-SCENARIO-AUDIT-2026-09-29.md](CR-FULL-SCENARIO-AUDIT-2026-09-29.md) | 全场景全维度审查（6 个只读子代理 + 真实 codex/claude/grok/omp 客户端 + 主代理逐条独立复现）：确认 10 条 P1（含 `/v1/responses` 的 codex 动态头守卫使 21 个渠道对无 `x-codex-*` 客户端全判死、SIGKILL 永久丢失配额扣减、`GET /v1/models/{model}` 200 配 "does not exist"、`n>1` 静默忽略、SSE 头配非 SSE 正文等）；推翻或更正 10 条子代理结论（含入站 128 MB 限制确实存在、钉渠道确实生效、断流确实向上游传播、丢失窗口是 5 秒而非 60 秒）；计费算术、认证边界、流式三方言、畸形输入、并发全部验证正常；另出生产升级风险清单（91 个模型受影响、21 次 DROP INDEX 未演练、pin_order 排序语义变更、V2 回填不可省）；记录 6 次早期探针因环境变量覆盖静默失效而落到正式服务（grok-4.6 经渠道 223，2 次完成计费、4 次客户端断开未计费） |
| [CR-FULL-SCENARIO-AUDIT-FIXES-2026-09-30.md](CR-FULL-SCENARIO-AUDIT-FIXES-2026-09-30.md) | 2026-09-29 全场景审查发现的修复落地：A 组 8 条 + B 组 9 条 + C 组 2 条全部实现并反向验证（每条修复还原后回归测试必须失败）；B6 错误词表在现场复核中发现只改了一半，补齐 `NewErrorWithStatusCode` / `abortWithOpenAiMessage` / `abortRateLimited` / `RelayNotImplemented` / `ClaudeErrorWrapper` 五个仍输出 `new_api_error` 的生产者；C1 为渠道级 codex 动态头 opt-out（`*bool` + `omitempty`，默认严格，含前端开关），C2 为非流式 Responses `usage` 缺失 token-detail 对象的补齐（**使该路径正文不再逐字节透传**，6 个既有测试期望值随之更新）；隔离环境逐条实测含 A2/A5/A6/A8/B1/B2/B4/B6/C1/C2，C2 另有短路反向取证（上游原始 `usage` 仅 3 键）；更正审查报告 4 处（B3 触发条件、`temperature=3` 未复现、`<nil>` 归因分支、gin 版本 v1.9.1）；收尾轮另修 `/v1/models` 的 `supported_endpoint_types` 未按调用方分组收敛（含查询 `*gorm.DB` 判空恒真、缺 `ensureAbilityColumnsInitialized`、出错回落方向三个自身缺陷）与令牌分组校验，并把 C2 扩到流式原生透传；实证更正审查 `1.8`（`use_channel` 记的是整条尝试链，非"只记初始渠道"）、确认 `1.11` 的 `tiered_expr` 行 `model_ratio=0` 出自活跃路径并写入运维文档、`***.` 掩码前缀决定不修（理由含探针输出）；本机 AI 工具端点统一到 13000；收尾轮补跑 `bun run build` 通过、A7 的 SIGTERM/SIGKILL 边界实测（前者落库、后者丢失）、并发现原竞态用例因夹具无 ability 而空转（加强后去掉锁即报 DATA RACE）；收尾末轮用可控 400 上游夹具把 B3 从"仅单元测试"补成双向实测（同源码交叉编译两支二进制，摘掉修复即复现 `text/event-stream` + 裸 JSON 正文，带修复即 `application/json`）；`bun run typecheck` 末轮实测通过（`tsc -b --force` 225 秒 EXIT=0，并用注入类型错误反向验证检查真的在工作；此前"卡在 uv_fs_read 不返回"是按一次 `sample` 栈误判，`/Volumes/Work` 实为本地 APFS 而非 SMB）；末轮再用 `mysql:8.4` 一次性实例做 MySQL 冷启动验证，发现 `key` 未加引号的谓词在 MySQL 上是语法错误、导致全新库直接 `FATAL`（8 处漏用既有的方言感知 `commonKeyCol`，已修并加 `model.KeyColumn()` 导出与反向验证），另定位到 gorm 上游的 `uni_tokens_key` 重启缺陷并记录不改；未提交 |

### 协议与中继

| 记录 | 内容 |
| --- | --- |
| [CR-RESPONSES-OFFICIAL-PROTOCOL-2026-09-15.md](CR-RESPONSES-OFFICIAL-PROTOCOL-2026-09-15.md) | Responses 官方流协议复核 |
| [CR-RESPONSES-STREAM-CODE-TYPE-2026-09-20.md](CR-RESPONSES-STREAM-CODE-TYPE-2026-09-20.md) | Responses 流事件顶层 code 类型 |
| [CR-RESPONSES-TOOL-NAME-NORMALIZATION-2026-09-17.md](CR-RESPONSES-TOOL-NAME-NORMALIZATION-2026-09-17.md) | 工具名归一化 |
| [CR-RESPONSES-TOOL-NAME-REFERENCE-2026-09-20.md](CR-RESPONSES-TOOL-NAME-REFERENCE-2026-09-20.md) | 工具名别名在引用位置被误判为冲突 |
| [CR-UPSTREAM-REQUEST-ID-EXTRACTION-2026-09-20.md](CR-UPSTREAM-REQUEST-ID-EXTRACTION-2026-09-20.md) | 上游 request id 提取的正则误捕与不确定遍历 |
| [CR-RESPONSES-CHAT-COMPAT-RECHECK-2026-09-05.md](CR-RESPONSES-CHAT-COMPAT-RECHECK-2026-09-05.md) | Responses 与 Chat 兼容路径 |
| [CR-CLAUDE-REASONING-CONVERSION-DEV-VERIFY-2026-08-31.md](CR-CLAUDE-REASONING-CONVERSION-DEV-VERIFY-2026-08-31.md) | Claude reasoning 转换 |
| [CR-CLAUDE-EFFORT-OLLAMA-CONVERSION-2026-09-20.md](CR-CLAUDE-EFFORT-OLLAMA-CONVERSION-2026-09-20.md) | effort 透传打断 Ollama 渠道 |
| [CR-PROTOCOL-CONVERSION-RECHECK-2026-09-01.md](CR-PROTOCOL-CONVERSION-RECHECK-2026-09-01.md) | 协议转换复核 |
| [CR-CODEX-SUB2API-FINGERPRINT-2026-08-18.md](CR-CODEX-SUB2API-FINGERPRINT-2026-08-18.md) | Codex 指纹比对 |
| [CR-CODEX-PARALLEL-REAL-CHANNELS-2026-09-08.md](CR-CODEX-PARALLEL-REAL-CHANNELS-2026-09-08.md) | 真实渠道并行验证 |
| [CR-RELAYKIT-MIGRATION-DESIGN-2026-09-30.md](CR-RELAYKIT-MIGRATION-DESIGN-2026-09-30.md) | 协议转换层收敛到上游 relaykit 的设计与十一轮对抗审查；确认上游已完成同一迁移（172 宿主文件 import `relaykit/types`、类型别名做到零调用点变更、`toolconv/` 覆盖 10 种 hosted 工具）；撤回"按客户端意图门控 reasoning 暴露"；记录 15 条主代理自身被推翻的错误断言（含存活四稿的"`dac55f0f` 非共同祖先"）；设计文档在 `docs/superpowers/specs/2026-09-30-relaykit-migration-design.md`，未改生产代码 |
| [CR-AI-TOOLS-SEARCH-PLAN-2026-10-10.md](CR-AI-TOOLS-SEARCH-PLAN-2026-10-10.md) | Claude Code / Codex / Kimi / Grok / omp / dsh 内置联网搜索支持的调研与核实；4 路并行只读调研 + 主代理逐条独立复现，对交接单与前序初稿给出 10 条修正（C1-C10），含"上游 `/v1/alpha/search` 是纯渠道透传而非自带搜索后端"与"dsh 326 个断链成立"；交付 `docs/architecture/ai-tools-search-support-plan-v2.md`，未改生产代码 |

### 渠道与路由

| 记录 | 内容 |
| --- | --- |
| [CR-CHANNEL-HEALTH-ROUTING-2026-08-25.md](CR-CHANNEL-HEALTH-ROUTING-2026-08-25.md) | 渠道健康与路由治理 |
| [CR-DEV-ROUTING-CONCURRENCY-2026-08-27.md](CR-DEV-ROUTING-CONCURRENCY-2026-08-27.md) | 路由并发 |
| [CR-TASK-ROUTING-504-RECHECK-2026-09-02.md](CR-TASK-ROUTING-504-RECHECK-2026-09-02.md) | 任务路由 504 复核 |

### 计费与价格

| 记录 | 内容 |
| --- | --- |
| [CR-HISTORICAL-PRICING-REPAIR-INDEX.md](CR-HISTORICAL-PRICING-REPAIR-INDEX.md) | 历史价格修正记录索引（2 次） |
| [CR-DEEPSEEK-PRICING-REPAIR-2026-09-11.md](CR-DEEPSEEK-PRICING-REPAIR-2026-09-11.md) | DeepSeek 价格 |
| [CR-GPT-IMAGE-25-PRICING-2026-09-11.md](CR-GPT-IMAGE-25-PRICING-2026-09-11.md) | GPT Image 2.5 价格 |
| [CR-PRICING-UNSET-FALLBACK-GEMMA-2026-09-19.md](CR-PRICING-UNSET-FALLBACK-GEMMA-2026-09-19.md) | 未定价模型兜底与 Gemma |
| [CR-CACHE-STATS-CLOSEOUT-2026-09-07.md](CR-CACHE-STATS-CLOSEOUT-2026-09-07.md) | 缓存统计 |
| [CR-LOG-CACHE-TOKEN-STATS-2026-09-01.md](CR-LOG-CACHE-TOKEN-STATS-2026-09-01.md) | 日志缓存 token 统计 |

价格维护的现行口径见 `docs/operations/pricing-maintenance.md`，本目录记录只作为过程证据。

### 前端

| 记录 | 内容 |
| --- | --- |
| [CR-DEFAULT-CLASSIC-PARITY-INDEX.md](CR-DEFAULT-CLASSIC-PARITY-INDEX.md) | Default 与 Classic 对齐记录索引（总览 + 3 个分项） |
| [CR-DEFAULT-LEGACY-CONSOLE-MIGRATION-2026-09-18.md](CR-DEFAULT-LEGACY-CONSOLE-MIGRATION-2026-09-18.md) | 旧控制台路由迁移 |
| [CR-DEFAULT-I18N-SOURCE-SCAN-2026-09-18.md](CR-DEFAULT-I18N-SOURCE-SCAN-2026-09-18.md) | i18n 源码扫描 |
| [CR-I18N-SCANNER-TEMPLATE-LITERALS-2026-09-20.md](CR-I18N-SCANNER-TEMPLATE-LITERALS-2026-09-20.md) | i18n 扫描漏掉模板字面量内的 t() |
| [CR-CLASSIC-RANKINGS-I18N-2026-09-20.md](CR-CLASSIC-RANKINGS-I18N-2026-09-20.md) | Classic 排行榜界面 i18n 键补齐 |
| [CR-DEFAULT-VIEW-OPTIONS-ACTIONS-COLUMN-2026-09-20.md](CR-DEFAULT-VIEW-OPTIONS-ACTIONS-COLUMN-2026-09-20.md) | Default 视图选项暴露行操作列 |
| [CR-DEFAULT-PERSISTED-VISIBILITY-REQUIRED-COLUMN-2026-09-20.md](CR-DEFAULT-PERSISTED-VISIBILITY-REQUIRED-COLUMN-2026-09-20.md) | 隐藏列持久化会永久隐藏必需列 |
| [CR-CHANNELS-BATCH-TOGGLE-DEFAULT-2026-09-20.md](CR-CHANNELS-BATCH-TOGGLE-DEFAULT-2026-09-20.md) | 渠道批量操作开关默认值 |

### 部署与运维

| 记录 | 内容 |
| --- | --- |
| [CR-MACOS-DOCKER-TUN-HARDENING-2026-08-23.md](CR-MACOS-DOCKER-TUN-HARDENING-2026-08-23.md) | macOS Docker TUN 加固 |

### 安全与认证

| 记录 | 内容 |
| --- | --- |
| [CR-SSRF-OLLAMA-RECHECK-2026-09-09.md](CR-SSRF-OLLAMA-RECHECK-2026-09-09.md) | SSRF 复核 |

### 发布记录

| 记录 | 内容 |
| --- | --- |

### 归档记录

2026-07 及更早的记录在 [`archive/`](archive/)。内容保留，仅路径变更。按主题查：

| 主题 | 归档记录 |
| --- | --- |
| 接手与版本 | `CR-DEPLOY-BUILD-TRACEABILITY-2026-05-01`、`CR-PROD-UPGRADE-READINESS-2026-07-08` |
| 隔离环境验证 | `CR-DEV-ISOLATED-VERIFY-2026-05-07`、`CR-DEV-ISOLATED-VERIFY-2026-05-11`、`CR-UPSTREAM-UI-INTAKE-PHASE-1-2026-05-11` |
| 协议与中继 | `CR-PROTOCOL-CONVERSION-CUSTOM-TOOLS-2026-05-13`、`CR-RESPONSES-ROUTING-OBSERVABILITY-2026-07-24` |
| 渠道与路由 | `CR-GPT56-TERRA-REAL-CHANNEL-GATE-2026-07-23` |
| 部署与运维 | `CR-MACOS-DOCKER-TUN-LAN-2026-05-22`、`CR-STORAGE-ACCELERATION-2026-06-12` |
| 安全与认证 | `CR-MAIN-PAYMENT-SECURITY-GAP-2026-04-19`、`CR-HEADER-PROFILE-STRATEGY-2026-04-21`、`CR-DESKTOP-OAUTH-HANDOFF-STORE-2026-04-17` |
| 发布记录 | `CR-GPT56-RESPONSES-PROD-UPGRADE-2026-07-12` |

archive 目录共有 76 份记录，上表只列出 2026-09-21 从根目录移入的 14 份。
完整清单直接查 `archive/` 目录。

## 新增记录

写入前确认：

- 文件名符合 `CR-<主题>-<YYYY-MM-DD>.md`，主题用大写英文与连字符。
- 正文包含采样时间、范围、验证命令与退出码、未覆盖边界。
- 只记录本轮实际运行过的命令与读到的输出；未验证的内容标注为未覆盖。
- 文档间用相对路径互链；归档目录内引用根目录文件需加一层 `../`。
