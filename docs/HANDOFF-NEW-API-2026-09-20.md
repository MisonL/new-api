# New API 历史任务交接

交接时间：2026-09-20（Asia/Shanghai）  
仓库：`/Volumes/Work/code/new-api`  
当前分支：`dev`

## 1. 交接结论

这个仓库已经完成多轮协议适配、Responses 兼容、计费修正、渠道路由治理、双前端同步和 Docker 运维加固。当前最重要的事实是：

- 交接时刻 `dev` 与 `origin/dev` 均为 `5cda680770543782d5d0a757b31f4e783443dc03`；`origin/main` 为 `51074ac3736288ee664966e0a2109e9580d86fcf`。
- 工作区不是干净候选：当前约 256 个已修改或未跟踪条目（含本文档），包含历史任务的后端、前端、测试、脚本和审计文档。不得使用 `git reset --hard`、`git checkout --` 或批量清理来“整理”它。
- 隔离开发服务 `3001` 已运行当前 dirty 工作区构建，容器 healthy，build-info 为 `5cda6807...-dirty`。
- 正式服务 `13000` 健康，但仍运行独立的旧正式镜像 `new-api-local:prod-codex-lite-4527d7666-litefix2`，build-info 为 `4527d7666...-litefix2`，不是当前 `dev` 工作区制品。
- 当前不能把隔离环境通过、正式 `/api/status` 为 200 或历史发布记录，表述为“当前所有改动已发布并完成真实渠道验收”。

### 状态再采样（2026-09-21）

上表的运行态是交接时刻的快照。此后 `dev` 又前进了若干提交，以下为 2026-09-21
重新采样的结果，命令与交接时刻相同：

```bash
git rev-parse HEAD
git status --short --untracked-files=all | wc -l
docker exec new-api /new-api --build-info
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
```

| 项 | 交接时刻（2026-09-20） | 再采样（2026-09-21） |
| --- | --- | --- |
| `dev` HEAD | `5cda68077` | `6e7eed0d4` |
| 工作区条目数 | 约 256 | 258 |
| 隔离开发容器 build-info | `5cda6807...-dirty` | `5cda6807...-dirty`（未重建，仍为旧构建） |
| 正式服务 | `4527d7666...-litefix2` | 未重建，`StartedAt` 与 `RestartCount` 不变 |

两点边界：

- 隔离开发容器**未随 `dev` 重建**，它运行的仍是 `5cda6807` 的 dirty 构建，
  不包含 2026-09-21 之后合入的提交。要用最新源码验证需重新执行
  `scripts/build-docker-local.sh` 并 force-recreate。
- 正式服务在 2026-09-21 只接受了 `logs` 表的历史数据清理与保留策略配置写入
  （见 `reviews/CR-PROD-LOG-RETENTION-2026-09-21.md`），容器本身未重建、未重启。

## 2. 当前运行态

| 范围 | 实例 | 镜像/版本 | 状态 | 证据 |
| --- | --- | --- | --- | --- |
| 正式 | `new-api`，`0.0.0.0:13000 -> 3000`（局域网可达，刻意配置） | `new-api-local:prod-codex-lite-4527d7666-litefix2`；`v1.1.0`；`4527d7666...-litefix2` | running/healthy，RestartCount=2 | `docker exec new-api /new-api --build-info`；`GET /api/status` HTTP 200 |
| 隔离开发 | `new-api-dev-isolated-new-api-1`，`127.0.0.1:3001 -> 3000` | `new-api-local:dev`；`v1.1.0`；`5cda6807...-dirty` | running/healthy，RestartCount=0 | `docker exec ... /new-api --build-info`；`GET /api/status` HTTP 200 |
| 隔离数据库 | `new-api-dev-isolated-postgres-1` | 端口 `5433` | healthy | Docker 状态 |
| 隔离缓存 | `new-api-dev-isolated-redis-1` | 端口 `6381` | healthy | Docker 状态 |

正式状态接口通过 newapi skill 的 `api.js` 读取，返回 `success=true`、版本 `v1.1.0`。当前正式状态还显示 `self_use_mode_enabled=true`；这会允许未配置官方价格的模型按现有兜底口径继续使用，不能等同于这些模型已经完成官方定价。

本次交接没有重启、重建或写入正式服务、正式 PostgreSQL、Redis、Caddy 或渠道配置。

## 3. 历史任务主线

### 3.1 Responses、Claude 和跨协议转换

已完成并有源码/测试覆盖的主要工作：

- Claude Messages 到 OpenAI Chat/Responses 的 thinking、`reasoning_content`、工具调用历史和 DeepSeek 兼容转换。
- `thinking.signature`、`redacted_thinking`、opaque encrypted context 等无法安全表达的内容采用显式失败，避免静默丢失历史上下文。
- Responses 原生流终态按官方 `response.completed`、`response.incomplete`、`response.failed` 和顶层 `error` 处理；`response.done` 只保留在 Chat 兼容适配路径，不作为原生 Responses 成功依据。
- SSE 前导事件、ping、流用量、非流式读取重试、413 压缩恢复、远程/本地 compact、opaque replay、工具输出重放和路由观测已加入相应保护及回归测试。
- Responses 工具名统一归一化，覆盖声明、选择、namespace、加载工具、MCP 和历史工具项；别名冲突显式失败，发生改名时关闭 raw pass-through。
- `reasoning_effort`、模型别名、协议转换方向、Chat 兼容终态和请求体重试均已补充边界测试。

状态：源码和隔离环境验证完成；正式环境曾有受控升级记录，但当前正式镜像不是当前工作区制品。真实上游长会话、Claude Code、Codex CLI 和所有渠道的最新生产行为仍需在发布窗口按 request id 复验。

关键证据：

- [CR-CLAUDE-REASONING-CONVERSION-DEV-VERIFY-2026-08-31.md](reviews/CR-CLAUDE-REASONING-CONVERSION-DEV-VERIFY-2026-08-31.md)
- [CR-RESPONSES-CHAT-COMPAT-RECHECK-2026-09-05.md](reviews/CR-RESPONSES-CHAT-COMPAT-RECHECK-2026-09-05.md)
- [CR-RESPONSES-OFFICIAL-PROTOCOL-2026-09-15.md](reviews/CR-RESPONSES-OFFICIAL-PROTOCOL-2026-09-15.md)
- [CR-RESPONSES-TOOL-NAME-NORMALIZATION-2026-09-17.md](reviews/CR-RESPONSES-TOOL-NAME-NORMALIZATION-2026-09-17.md)
- [CR-GPT56-RESPONSES-PROD-UPGRADE-2026-07-12.md](reviews/archive/CR-GPT56-RESPONSES-PROD-UPGRADE-2026-07-12.md)

### 3.2 渠道路由、502、429 和健康治理

已完成的实现包括：

- 统一请求模型、映射后上游模型、协议、上游路径、多 key 身份和 `route_key`，避免模型映射后健康状态落到错误路由。
- Redis cooldown、half-open probe、probe token 续租/释放、运行态索引和 TTL 语义；Redis 不可用时 fail-open，但记录管理员可见审计，不伪造健康结果。
- 流式请求只有正常终态且没有非良性 `EndError` 才恢复渠道；429 按 `Retry-After` 或配置回退冷却。
- 路由权重、优先级、渠道 pin、并发和自动切换的 Default/Classic 管理界面与回归测试。

历史真实渠道门禁曾确认：部分 Terra 渠道通过，部分渠道存在 `503`、`429`、超大 compact `502` 或能力策略拒绝。历史报告明确没有把失败静默当作成功。

状态：隔离代码与路由测试已完成；正式渠道 35、201、216 以及历史上出现过 72、150 等渠道的最新真实 request id、上游响应、Redis 状态和计费结果仍需单独采集。不能用 `13000 /api/status=200` 关闭这些问题。

关键证据：

- [CR-CHANNEL-HEALTH-ROUTING-2026-08-25.md](reviews/CR-CHANNEL-HEALTH-ROUTING-2026-08-25.md)
- [CR-CODEX-PARALLEL-REAL-CHANNELS-2026-09-08.md](reviews/CR-CODEX-PARALLEL-REAL-CHANNELS-2026-09-08.md)
- [CR-GPT56-TERRA-REAL-CHANNEL-GATE-2026-07-23.md](reviews/archive/CR-GPT56-TERRA-REAL-CHANNEL-GATE-2026-07-23.md)
- [CR-TASK-ROUTING-504-RECHECK-2026-09-02.md](reviews/CR-TASK-ROUTING-504-RECHECK-2026-09-02.md)
- [CR-RESPONSES-ROUTING-OBSERVABILITY-2026-07-24.md](reviews/archive/CR-RESPONSES-ROUTING-OBSERVABILITY-2026-07-24.md)

### 3.3 官方价格、计费和历史数据

已完成或已有正式库执行记录的工作：

- GPT-5.6 系列、Codex、Terra、Luna 的倍率、缓存读取/写入字段和 Responses 计费路径。
- DeepSeek V4.1-Flash、V4 Pro 的 peak/off-peak 阶梯、生效时间边界、旧表达式快照和历史日志回算。
- xAI Grok 4.3、4.20、4.5、4.6、Grok Build 变体按普通 API 原价阶梯维护，排除 Batch 折扣；Grok 4.5 旧路径别名历史金额已修正。
- GPT Image 2.5 只维护官方精确变体 `gpt-image-2.5-sunburst` 和 `gpt-image-2.5-flare`，未虚构通用 `gpt-image-2.5`；标准输入、缓存和图像输出倍率已加入源码测试。
- Gemma 4 免费模型的输出倍率由错误的 `1` 修正为 `0`，正式价格选项回读为 `0/0/0/0`。
- MiniMax、Agnes、Qwen、Kimi、Step、Gemini 等已确认官方原价模型和别名已按“不采用促销/Batch 折扣”的口径补齐。
- 历史 `type=2` 日志按官方价格安全回算；已结束小时的 `quota_data` 按原始日志重建并完成对账，避免把活动小时异步刷新差异误判为历史错误。
- 价格修正器支持分片扫描、旧价格快照、tiered expression、模型路径别名和事务备查表；已多次验证二次 dry-run 为零。

仍需注意：

- 未确认官方 token 单价或无法由当前计费链准确表达的 Composer、部分通用路由、视频/音频/搜索/按秒模型不能强行套用标准倍率。
- 正式服务当前启用了统一自用模式，未定价模型可能继续使用历史兜底倍率；这不是官方价格。新官方价格确认后必须更新 options、源码默认值，并按生效时间决定是否回算历史。
- 活动小时的 `quota_data` 与实时日志可能暂时不同，复核必须先固定 `logs.id` 或关闭小时边界，再把活动小时单独报告。

关键证据：

- [CR-HISTORICAL-PRICING-REPAIR-2026-09-12.md](reviews/CR-HISTORICAL-PRICING-REPAIR-2026-09-12.md)
- [CR-HISTORICAL-PRICING-REPAIR-2026-08-29.md](reviews/CR-HISTORICAL-PRICING-REPAIR-2026-08-29.md)
- [CR-DEEPSEEK-PRICING-REPAIR-2026-09-11.md](reviews/CR-DEEPSEEK-PRICING-REPAIR-2026-09-11.md)
- [CR-GPT-IMAGE-25-PRICING-2026-09-11.md](reviews/CR-GPT-IMAGE-25-PRICING-2026-09-11.md)
- [CR-PRICING-UNSET-FALLBACK-GEMMA-2026-09-19.md](reviews/CR-PRICING-UNSET-FALLBACK-GEMMA-2026-09-19.md)
- [pricing-maintenance.md](operations/pricing-maintenance.md)

### 3.4 Classic 与 Default 双前端

已完成的对齐范围：

- 控制台路由、旧 Classic 路径重写、Dashboard、Chat、Playground、Chat2Link、Token、日志、任务、绘图日志。
- 钱包、充值、兑换码、订阅、用户、渠道、模型、模型部署和系统设置。
- 使用日志列可见性/顺序、渠道模型测试运行配置、Header Profile、参数覆盖、代理、模型映射、响应协议和渠道健康运行态。
- Playground 调试查看器、SSE 事件、图片粘贴、消息重试和多语言。
- Default 微信绑定：二维码、验证码、真实 POST 绑定接口、绑定结果展示和六种语言文案。

`web/classic` 的 `ModelsList.jsx` 当前没有页面引用，是历史死代码，不能据此认定 Default 缺少可用模型页面。

状态：源码、测试、构建和隔离容器验证完成；正式 `13000` 未加载本轮 Default 前端制品，微信真实扫码链路未在启用微信配置的环境中完成业务验收。

关键证据：

- [CR-DEFAULT-CLASSIC-FULL-PARITY-2026-09-20.md](reviews/CR-DEFAULT-CLASSIC-FULL-PARITY-2026-09-20.md)
- [CR-DEFAULT-CLASSIC-PARITY-COLUMN-ORDER-2026-09-19.md](reviews/CR-DEFAULT-CLASSIC-PARITY-COLUMN-ORDER-2026-09-19.md)
- [CR-DEFAULT-CLASSIC-PARITY-MODEL-TEST-RUNTIME-2026-09-19.md](reviews/CR-DEFAULT-CLASSIC-PARITY-MODEL-TEST-RUNTIME-2026-09-19.md)
- [CR-DEFAULT-CLASSIC-PARITY-PLAYGROUND-2026-09-19.md](reviews/CR-DEFAULT-CLASSIC-PARITY-PLAYGROUND-2026-09-19.md)
- [CR-DEFAULT-LEGACY-CONSOLE-MIGRATION-2026-09-18.md](reviews/CR-DEFAULT-LEGACY-CONSOLE-MIGRATION-2026-09-18.md)

### 3.5 部署、macOS Docker 和安全治理

已完成：

- Docker build-info、OCI traceability、Compose project 标签和正式/隔离端口边界。
- macOS Docker TUN monitor/recovery/watchdog/log-retention 脚本的特殊字符转义、快照新鲜度、Caddy 重启阈值、dirty worktree guard 和安装原子替换。
- GORM 参数化、SSRF/上游 URL 校验、敏感日志字段隔离、token/quota 操作审计、Passkey/OAuth 绑定保护和支付/充值边界加固。

状态：脚本测试和隔离验证有记录；真实 launchd、Caddy 切换、正式 Compose apply、签名发布和回滚仍需按维护窗口执行。

关键证据：

- [CR-CURRENT-WORKTREE-CLOSEOUT-2026-09-14.md](reviews/CR-CURRENT-WORKTREE-CLOSEOUT-2026-09-14.md)
- [CR-DEPLOY-BUILD-TRACEABILITY-2026-05-01.md](reviews/archive/CR-DEPLOY-BUILD-TRACEABILITY-2026-05-01.md)
- [CR-PROD-UPGRADE-READINESS-2026-07-08.md](reviews/archive/CR-PROD-UPGRADE-READINESS-2026-07-08.md)
- [CR-MACOS-DOCKER-TUN-HARDENING-2026-08-23.md](reviews/CR-MACOS-DOCKER-TUN-HARDENING-2026-08-23.md)

## 4. 最近验证结果

交接前完成的前端与隔离环境验证：

| 检查 | 结果 |
| --- | --- |
| `cd web/default && bun run i18n:sync` | 通过；925 个源码文件、4632 个翻译键、0 个缺失键 |
| `cd web/default && bun test` | 220 pass、0 fail、866 expect calls |
| `cd web/default && bun run typecheck` / `lint` | 退出码 0 |
| `cd web/default && bun run build` | 通过；Rsbuild、Safari compatibility rewrite/check 通过 |
| `cd web/classic && bun run test:node` | 137 pass、0 fail |
| 隔离 Docker 重建 | `new-api-local:dev` 构建并强制重建成功，healthy |
| `3001/api/status` / `13000/api/status` | HTTP 200（`13000` 为只读探针） |
| `git diff --check` | 通过 |

Go 侧 `build` / `vet` / `test` 的门禁结果与命令见
[CR-DEV-BRANCH-CHANGE-INVENTORY-2026-09-20.md](reviews/CR-DEV-BRANCH-CHANGE-INVENTORY-2026-09-20.md)。

注意：上述证据不能替代最终拆分后候选的全量 Go 门禁；正式提交或发布前必须对候选重新执行。

## 5. 当前未闭环事项

按优先级排列：

1. **候选制品未冻结**：约 256 个 dirty/untracked 条目混合了多个历史任务。接手者必须先按主题拆分、审查 `git diff --name-only`，再形成一个无 `-dirty` 的候选 commit 和镜像。
2. **正式服务未加载当前代码**：`13000` 仍是旧正式镜像。任何“修复已在正式生效”的结论都需要重新读取正式 build-info 证明。
3. **真实渠道错误未全部关闭**：对 35、201、216 以及历史失败渠道，需要用 request id 对照 new-api 出站日志、上游后台日志、HTTP 状态、响应体、路由模型、计费快照和 Redis 健康事件。上游 429、502、503、524 不能只靠重试计为成功。
4. **未定价模型风险**：重新查询当前 active model 目录和官方价目，区分永久降价、限时促销、Batch 价格、缓存读写价和无法确认的媒体/按次价格；对未确认价格继续 fail-closed 或显式标记，不把 `37.5` 兜底写成官方价格。
5. **历史日志持续增长**：每次修正先固定 `logs.max(id)` 和已关闭小时，运行分片 dry-run，备份受影响日志/用户/token/channel/quota_data，再 apply、清缓存、二次 dry-run 和聚合对账。
6. **微信业务验收未完成**：需要在已配置 `wechat_login` 和二维码的环境中真实扫码、获取验证码、调用绑定 POST、确认账户列表和失败状态；当前 `13000` 状态中 `wechat_login=false`，不能在正式端验收该流程。
7. **正式发布门禁未执行**：需要生产 Compose 环境变量非空校验、PostgreSQL 备份、候选镜像回滚标签、`13000` 端口映射和 Compose 标签复核；未得到明确发布授权前保持只读。

## 6. 建议接手顺序

```text
1. 保存本交接文档和当前 git status/diff 快照
2. 按 Responses、pricing、routing、frontend、ops 五组拆分 dirty worktree
3. 对最终候选执行 go test/vet、Default/Classic test/lint/typecheck/build
4. 构建无 dirty 标记的候选镜像，并在 3001 重建、验证 build-info 和健康接口
5. 重新查询正式 13000 的镜像、价格选项、active models 和最近错误样本
6. 仅在授权维护窗口执行正式备份、Compose 发布和真实渠道门禁
7. 发布后按 request id、上游日志、计费和 quota_data 做闭环对账
```

## 7. 正式发布与回滚门禁

发布前必须满足：

- 工作区已拆分并形成干净候选 commit；镜像 build-info revision 等于候选 commit，不带 `-dirty`。
- 已完成隔离 `3001` 测试，且候选镜像、容器和 build-info 三者一致。
- 已备份正式 PostgreSQL，并记录 options、模型价格、渠道能力和 active model 快照。
- `SESSION_SECRET`、`CRYPTO_SECRET`、`SQL_DSN`、`REDIS_CONN_STRING` 等正式变量已在受控 shell 中非空；不得把值写入日志或文档。
- 使用正式 Compose project 和配置文件，明确保留 `0.0.0.0:13000:3000`（局域网访问为刻意需求）、真实数据目录和日志目录。
- 正式 `new-api` 容器的 Compose 配置来自 `docker-compose.yml` **加**一个已丢失的 `/tmp/new-api-prod-lite-override.yml`。该 override 只用于把镜像 tag 覆盖为 `-litefix2`；`.env` 的 `NEW_API_IMAGE` 已同步修正为 `new-api-local:prod-codex-lite-4527d7666-litefix2`，因此当前单文件渲染与运行容器逐项一致（image、container_name、restart、command、端口、两个 bind 挂载）。发布前必须重新确认这一点：若 `.env` 的 `NEW_API_IMAGE` 仍是基础 tag，`docker compose up -d` 会把正式服务静默降级到 2026-07-17 的 `4527d7666` 镜像。

发布后必须读取：

```bash
docker exec new-api /new-api --build-info
curl --connect-timeout 3 --max-time 10 -fsS http://127.0.0.1:13000/api/status
docker inspect new-api --format '{{json .Config.Labels}}'
docker inspect new-api --format '{{json .NetworkSettings.Ports}}'
docker logs --tail=200 new-api
```

回滚使用发布前保留的旧镜像标签和同一 Compose project；如涉及数据库迁移，先停止写入再按备份恢复，不能只替换容器而假定数据自动回滚。

## 8. 文档索引

`docs/reviews/` 的完整索引见 [docs/reviews/README.md](reviews/README.md)，
其中按接手状态、协议、渠道、计费、前端、部署、安全分类列出当前有效记录。
2026-07 及更早的历史记录已移入 [docs/reviews/archive/](reviews/archive/)，内容保留。

本任务相关的直接入口：

1. [CR-DEV-BRANCH-CHANGE-INVENTORY-2026-09-20.md](reviews/CR-DEV-BRANCH-CHANGE-INVENTORY-2026-09-20.md)：改动清单与提交拆分建议。
2. [CR-DOC-CODE-CONSISTENCY-2026-09-20.md](reviews/CR-DOC-CODE-CONSISTENCY-2026-09-20.md)：文档与代码一致性修复。
3. [CR-CURRENT-WORKTREE-CLOSEOUT-2026-09-14.md](reviews/CR-CURRENT-WORKTREE-CLOSEOUT-2026-09-14.md)：工作区收口状态。

本交接文件不包含 API token、数据库密码、Redis 连接串、Cookie 或其他凭证。New API 管理操作应继续通过 `/Users/mison/.agents/skills/newapi` 提供的脚本执行，避免直接读取或打印敏感配置。
