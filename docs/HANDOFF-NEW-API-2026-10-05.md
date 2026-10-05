# New API 历史会话工作交接

交接时间：2026-10-05（Asia/Shanghai）
仓库：`/Volumes/Work/code/new-api`
当前分支：`dev`
HEAD：`ae2ef2609`
`origin/dev`：`5cda68077`（`dev` 领先 112 个提交，未推送）

## 1. 交接结论

本仓库在 2026-09-20 至 2026-10-05 期间由多个并行 Claude 会话推进，累积 112 个未推送提交（`origin/dev` 之后），全部带 `Co-Authored-By: Claude` trailer（112 个中 110 个显式携带）。按提交者身份分两组：

| 身份 | 提交数 | 说明 |
| --- | --- | --- |
| `1360962086@qq.com`（`MisonL`） | 71 | 含本会话链 |
| `liangmingsen@qdama.cn`（`Mison`） | 41 | 早期会话链 |

两者是同一贡献者的两个 git 身份，不是两批人。112 个提交按类型：`docs` 62、`fix` 34、`test` 5、`feat` 5、`refactor` 3、`perf` 1；按 scope 前三位为 `docs(reviews)` 31、`docs(specs)` 25、`fix(compose)` 6。

当前最重要的事实：

- `dev` 领先 `origin/dev` **112 个提交**，全部未推送。推送前须与用户确认，因为其中混有多个并行会话的工作。
- 工作区有 **503 个未提交条目**（374 个已跟踪改动 + 129 个未跟踪文件；2026-10-05 采样），属于**其他并行会话**的在途工作。不得使用 `git reset --hard`、`git checkout --` 或批量清理来「整理」它。
- 隔离开发服务 `3001` 健康，但运行的是 `de563706d`（2026-10-01）的 dirty 构建，**落后当前 HEAD 38 个提交**。
- 正式服务 `13000` 健康，运行独立旧镜像 `new-api-local:prod-codex-lite-4527d7666-litefix2`（commit `4527d7666`，2026-09-08），**与 `dev` 相距 112 个提交**。
- **不得**把隔离环境通过、正式 `/api/status` 为 200、或历史发布记录，表述为「当前所有改动已发布并完成真实渠道验收」。
- 本交接**没有**重启、重建或写入正式服务、正式 PostgreSQL、Redis 或渠道配置。

## 2. 当前运行态

采样命令与时间：2026-10-05，`docker ps`、`docker exec <c> /new-api --build-info`、`curl -fsS <host>/api/status`。

| 范围 | 实例 | 镜像 / 版本 | 状态 | 证据 |
| --- | --- | --- | --- | --- |
| 正式 | `new-api`，`0.0.0.0:13000 -> 3000`（局域网可达，刻意配置） | `new-api-local:prod-codex-lite-4527d7666-litefix2`；`v1.1.0`；`4527d7666-litefix2`；`date=2026-09-08T05:00:00Z` | Up 15h healthy | `docker exec new-api /new-api --build-info`；`GET /api/status` 200 |
| 隔离开发 | `new-api-dev-isolated-new-api-1`，`127.0.0.1:3001 -> 3000` | `new-api-local:dev`；`v1.1.0`；`de563706d-dirty`；`date=2026-10-01T06:21:48Z` | Up 15h healthy | 同上 |
| 隔离数据库 | `new-api-dev-isolated-postgres-1` | `postgres:15`，`127.0.0.1:5433 -> 5432` | Up 15h healthy | `docker ps` |
| 隔离缓存 | `new-api-dev-isolated-redis-1` | `redis:latest`，`127.0.0.1:6381 -> 6379` | Up 15h healthy | `docker ps` |

隔离容器代码与 HEAD 的关系：

```bash
git merge-base --is-ancestor de563706d HEAD   # YES
git rev-list --count de563706d..HEAD          # 38
```

即隔离容器是当前源码的**祖先**，未包含 2026-10-01 之后的 38 个提交。要用最新源码验证需重建：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
```

**边界**：容器 `healthy` 只证明服务健康，不证明运行代码已同步到最新 Git 提交。正式服务的 `StartedAt` 与 `RestartCount` 在本交接期间未变化，即未重启。

## 3. 历史任务主线

### 3.1 协议转换层收敛到上游 relaykit（最大工作块）

设计文档：`docs/superpowers/specs/2026-09-30-relaykit-migration-design.md`（1496 行）。审计记录：`docs/reviews/CR-RELAYKIT-MIGRATION-DESIGN-2026-09-30.md`。

核心结论：上游 `QuantumNous/new-api` 的 `main` 分支**已经完成**同类迁移（`relaykit/` 子 module，宿主 172 个文件 import `relaykit/types`）。本仓库不是要发明该迁移，而是把本地独有的协议转换能力并入 relaykit 的既有形态。

设计文档经 **19 轮**对抗式复核，第 10 节的 30 个未决项中 **29 个已关闭**，仅剩第 24 项（`openaicompat` tool 循环重构）明确划为「独立重构，不在本次迁移范围」（12.15.3）；另 12.18 表的 10 项亦全部关闭。关键决定：

- `types` 拆分：6 个文件迁入 `relaykit/types`，宿主保留 4 个；常量经 Go 类型别名转发，其余改 import 路径。
- `dto` 迁入 `relaykit/dto`：**不可用别名**——136 个同名结构体中 33 个字段集不同（双向差异）。上游的做法是重写 281 个 import，不是别名。
- `dto` 迁入后宿主 import 必须清零，`relaykit/relayconvert/boundary_test.go` 是阶段 2 的**硬门禁**（`allowedViolations` 为空，约定「只减不增」）。
- 通道级 Claude 入向转换器（`relay-claude.go:255` 起 459 行，服务 claude/aws/vertex 三通道）**收敛到 relaykit**，阶段 4 接线钩子后删除本地实现。三类宿主耦合在 relaykit 侧均有注入机制：思考等级走 `convmeta.Options` 钩子、媒体解析走 `relayconvert.MediaResolver`、schema 归一化内置 `FunctionParametersToInputSchema`。
- `service/openaicompat/` **整体保留**——按能力切分在函数粒度不成立（`convertResponsesToolsToChat` 内交织 5 种工具语义）。
- 响应方向的 thinking 暴露必须在**索引分配之前**在 relaykit 内门控；后置过滤会破坏 SSE 索引，不可行。
- 15 处 tag / Go 类型差异**全部采用 relaykit、零处打补丁**。其中 tag 5 处有 3 处是空操作（Go 的 `omitempty` 对 struct 值无效）。

已落地的配套产物：

- `cf29ddb6f`：可重跑的「四维 dto 兼容性检查器」（字段名、JSON tag、Go 类型、嵌入字段），把复核规则变成带严格门禁的工具，而非一次性人工比对。
- `ae2ef2609`：本地 golden 基线 `service/convert_golden_test.go`，8 个快照覆盖请求 / 响应 / 流三矩阵，且把 thinking 门控的**两侧**（DeepSeek 暴露、非 DeepSeek 丢弃）都钉住。反向验证：扰动任一快照即失败。**注意**：第一版夹具只用非 DeepSeek 模型名，全绿却没有覆盖迁移要保护的 thinking 路径，已修正。

迁移分 6 个阶段，尚未开始执行（阶段 0 前置：unshallow `.git/shallow`、提交未跟踪文件、取得并锚定 relaykit 源码版本）。

### 3.2 Anthropic 认证头

7 个提交：`be1979283`、`85f652257`、`d32f60d6a`、`6aab23e87`、`f13b6d022`、`d93183595`、`998740f20`。

- Claude 渠道与取模型接口同时发送 `Authorization: Bearer` 与 `x-api-key`（同一 token，保证上游读哪个头都认证为同一主体）。
- `f13b6d022` 把凭据头从「合并」改为「替换」，避免逗号拼接出无效凭据。
- `d93183595` 引入凭据对同步，**自身引入了一个 P0**：当请求未携带 Anthropic 凭据对时（如 Vertex 的 Google token），会把 `Authorization` 覆盖成配置值。`998740f20` 修复——仅当凭据对实际存在时才同步。
- 修复已反向验证：还原 `d93183595` 使 4 个测试按正确原因失败。

### 3.3 定价与计费

- `5af49fd51`、`6794b63a5`、`ef093ab64`：gpt-6-sol / gpt-6-luna / grok-4.7 及多厂商新模型官方定价。
- `bd25f920d`：修正 Doubao Seed 2.1 源倍率（原值高 500 倍）。
- `de72e05f2`：`setting/ratio_setting/model_ratio_new_models_test.go`，把新模型定价钉死（14 个子测试，从厂商公布价按仓库自身换算推导）。独立文件，避免与另一会话正在编辑的 `model_ratio_test.go` 冲突。
- 未定价模型是 **fail-closed 特性，不是缺陷**：`GetModelRatio` 对未配置名返回 `37.5` 且 `ok = SelfUseModeEnabled`；`relay/helper/price.go:96-103` 在用户未开启 `AcceptUnsetRatioModel` 时直接拒绝。**不要**通过写入兜底倍率来「修复」它。
- 37.5 兜底名的逐家官方复核结论见 `docs/operations/pricing-maintenance.md`（`### 剩余兜底名的逐家官方复核（2026-10-03）` 与 `## 2026-10-04 大小写变体别名补键与兜底限定回算`）。17 个名字经官方页确认无价，**不补价是有意结论**；另 2 个（`MiMo-V2.6-Flash`、`DeepSeek-V4.1-Flash`）是大小写变体，已补显式键并回算。

### 3.4 限流管理员豁免

24 个提交（`99f3169c8` 起至 `32776b881`）。功能：允许管理员绕过全局限流（全局 API / 页面 / 搜索 / 联想），`CriticalRateLimit` 与桌面 OAuth 轮询**刻意不豁免**。

过程中修掉的自身缺陷：

- `77cfbc69d`：被禁用的管理员账号仍被豁免。
- `a78da04fa`、`2877e5002`：新增文案未登记 i18n。
- `3a273494a`、`de563706d`：非规范布尔值（`"True"`、`"1"`）被接受却静默失效——真值统一在 `normalizeOptionValueInMemoryConfig` 归一化。
- `604c1e6b6`：会话中间件缺失时改为 fail-closed。
- `b3c615b3e`：classic 设置页每个保存按钮限定到自己的区段。

### 3.5 布尔选项归一化与配置健壮性

- `b0f0bfb4e`、`de563706d`、`31cf18d72`：日志保留与全部布尔选项在持久化前归一化；补齐由此暴露的守卫缺口。
- `104a9ae44`：尚未加载的选项在限流页保持可编辑（修复 classic 设置页字段被永久过滤丢弃的既有缺陷）。
- `502f26cec`：`controller/option_guard_test.go` 快照 22 个 `common.*` 布尔全局，阻止守卫测试把一个请求放行后设置的值泄漏到同进程后续测试。

### 3.6 日志索引与 PostgreSQL

- `eade672f6`：`logs` 表声明索引从 41 个（含大量历史遗留的重复前缀索引，写入放大明显）收敛到 19 个（含主键）。
- `e6b7a0779`：在 PostgreSQL 上用 `CONCURRENTLY` 删除遗留索引。
- `b3ffba8a0`：`model/log_index_cleanup_migration.go` 的 `HasIndex` 接口改为返回 `(bool, error)`。原实现用 gorm 的 `HasIndex`，它**吞掉错误**（PG/SQLite 覆写把错误放进 `RunWithValue` 返回值，而 `HasIndex` 签名是 `bool`，没有错误通道）——索引查找失败会被读成「索引已不存在」，于是跳过删除却仍写入完成标记，遗留索引永久搁浅。现改为按方言直接计数查询。
- `64e07ec1e`：`key` 在 MySQL 上是保留字，`Option` 表的谓词必须用 `model.KeyColumn()` 加引号，否则全新库启动即 `FATAL`。同类修复见 `mysql-key-column-needs-dialect-quoting` 记忆。
- `1f85662ae`：恢复支付与兑换路径上的行锁。

### 3.7 代理信任与部署

- `65f4299f5`：`auto` 模式从容器自身接口推导受信代理。
- `ead9e4b9f`：在任何部署形态下都信任 loopback。
- `f97a7c3a7`：`TRUSTED_PROXIES=auto` 按**条目**识别，而非整串匹配。此前 `auto,` 会被当成字面量 `"auto"` 传给 gin，`prepareTrustedCIDRs` 抛 `ParseError`，`main.go:183` 的 `FatalLog` 直接终止进程。同时拒绝 `auto` 与显式条目混用。
- `48c569077`、`2b05f2a41`：compose 默认 `TRUSTED_PROXIES` 置空，健康检查探测配置的 PORT。
- `6e0f9b0f6`、`229ff5764`、`fad2e9e36`、`4b68dd87a`、`31ac41f2d`、`a678db05c`：补齐 compose 变量透传（限流变量、`STREAMING_TIMEOUT`、`SQLITE_PATH`、`CRYPTO_SECRET`），删除消费方已消失的 `GEMINI_VISION_MAX_IMAGE_NUM`。

### 3.8 i18n

- `d871b6d73`、`10c6316d2`、`3a8e680a3`、`6a3a8f854`：fr / ja / ru / vi 四语种补齐，使 `i18n:sync` 报零改动、六语种 missing 与 untranslated 均为 0。
- `83a134885`：Init 之前返回消息键而非 panic（`i18n.NewLocalizer(nil, ...)` 的每次查找都解引用 nil bundle）。
- `4842d62ad`：删除 `GetLocalizer` 的过时首行注释。
- `2877e5002`、`a78da04fa`：限流页新增字符串在 default 与 classic 两侧登记。

### 3.9 构建修复

- `aa0258248`：补上 `main.go` 已调用但缺失的 `FlushBatchUpdates` 定义。

### 3.10 审计记录与文档索引

`docs(reviews)` 31 个提交，维护 `docs/reviews/` 的 63 份记录 + 76 份归档，以及 `docs/reviews/README.md` 索引。`e9c93b0a5` 把 12 份「索引已链接但文件未跟踪」的审计记录纳入版本控制。`8f86fb1d0` 归档 2026-07 及更早记录并建立主题索引。

## 4. 最近验证结果

均在隔离 worktree（`HEAD` + 本会话改动）中运行，未污染共享工作区。

| 命令 | 结果 |
| --- | --- |
| `go test ./setting/model_setting/... ./common/... ./setting/ratio_setting/...` | ok |
| `go test ./model/ -run 'TestLogIndexCleanup\|TestGormExecutorHasIndex\|TestLegacyLogIndexes' -count=1` | ok |
| `go test ./controller/ -run 'TestUpdateOptionOAuthGuards...\|TestUpdateOptionGuardsCoverEveryProvider' -count=1` | ok |
| `go test ./setting/ratio_setting/ -run TestRecentVendorModelPricingIsPinned -count=1` | ok |
| `bun run build`（web/default，末轮实测） | 通过 |
| `bun run typecheck`（`tsc -b --force`） | 通过，约 225 秒，EXIT=0 |

**未覆盖的边界**：

- 隔离容器是 `de563706d`，未随 HEAD 重建，**上表全部是源码级验证，不是运行中服务的行为验证**。
- 真实数据库列类型 / SQL 类型转换 / 三库兼容差异：MySQL 冷启动曾用一次性 `mysql:8.4` 实例验证过，SQLite 与 PostgreSQL 在隔离环境验证过；**未覆盖**在正式库上执行迁移。
- 协议转换的 golden 基线只钉住 `convert.go` 的本地行为，**未**在 relaykit 上运行过（迁移尚未开始）。

## 5. 当前未闭环事项

按优先级排列，均非「已完成」：

1. **112 个提交未推送**。推送前须与用户确认；其中混有多个并行会话的提交，且工作区仍有 503 项其他会话的在途改动。
2. **relaykit 迁移未开工**。设计已完成、未决项已全关闭，但阶段 0 的三个前置（unshallow、提交未跟踪文件、锚定 relaykit 源码版本）都还没做。
3. **隔离开发环境落后 38 个提交**。需重建才能用最新源码做行为验证。
4. **正式服务落后 112 个提交**。用户明确指示「先不升级正式服务」。`docs/operations/new-api-production-release-gates.md` 定义了门禁；升级前须核对 91 个受影响模型、21 次 `DROP INDEX` 未演练、`pin_order` 排序语义变更、V2 回填不可省（见 `CR-FULL-SCENARIO-AUDIT-2026-09-29.md`）。
5. **生产的大小写归一化能力只能实测**。`strings` / 源码 diff / 自写复刻三种静态判据对 `Qwen3-Embedding-8B` 全部判错（它有原始计费 0.02，而正式库只有小写键）。要判定一批模型名的生产解析结果，只有两条路：用正式计费日志反查，或打开 `ExposeRatioEnabled` 后调 `GET /api/ratio_config`（属配置写入，须先经用户同意）。
6. **18 个可路由但正式库无精确键、差异不止大小写的名字，已决定不建键**。理由：它们在 `abilities` 里 `enabled` 全为 `f`（当前不可路由）；与 `gpt-5.2-pro` 先例不同类（那对是成对存在，这批只有无日期版父键，补带日期的键会把滚动语义钉死）；`[free]deepseek-v4-pro-0813` 解析到付费价却写着 free，语义未知时建键可能制造错误。未来启用这批渠道时，正确动作是先按官方价逐个取价再建键。
7. **`service/openaicompat/` 的 tool 循环重构**：让基础分支可独立转调 relaykit 的唯一路径；独立重构，不在本次迁移范围。

## 6. 建议接手顺序

1. `git status --short --branch` 与 `git log --oneline origin/dev..dev`，确认 503 项未提交改动的归属边界。
2. 读 `docs/superpowers/specs/2026-09-30-relaykit-migration-design.md` 的第 7 节（收敛路径）、第 10 节（未决项）、12.16（验证策略）。
3. 若要验证最新源码：重建隔离环境（命令见第 2 节），再跑 `go test ./controller ./model ./relay/common ./relay/helper ./service`。
4. 若要推进 relaykit 迁移：从阶段 0 的三个前置开始。
5. 若要发布：先读 `docs/operations/new-api-production-release-gates.md`，且必须与用户确认。

## 7. 边界与不得触碰

- **正式服务不升级**（用户明确指示）。只读源码与隔离开发环境。
- **不管理开发环境管理员密码**（用户明确指示）。
- 工作区 503 项未提交改动属其他并行会话。不得提交、回退、覆盖或整理。
- 提交前用 `git diff --cached --name-only` 与 `git diff --name-only` 确认只含自己任务的原子范围。**注意**：`git commit` 会扫入其他会话已 staged 的文件，提交前必须检查暂存区。
- 代码中不使用 Emoji 或装饰性 Unicode；代码注释不用中文（i18n 翻译值用中文）。
- 业务代码不直接调用 `encoding/json`，统一走 `common.Marshal` / `Unmarshal` / `UnmarshalJsonStr` / `DecodeJson` / `GetJsonType`。
- 三库（SQLite / MySQL / PostgreSQL）兼容；`key` 列必须用 `model.KeyColumn()` 加引号。
- 不得为了「先跑通」添加隐藏回退、静默容错、默认降级或伪成功路径；不得用 mock 或吞异常制造通过结果。
- 不得把局部测试通过表述成全链路已验证。

## 8. 文档索引

| 主题 | 路径 |
| --- | --- |
| 审计记录总索引 | `docs/reviews/README.md` |
| relaykit 迁移设计 | `docs/superpowers/specs/2026-09-30-relaykit-migration-design.md` |
| relaykit 迁移设计审计 | `docs/reviews/CR-RELAYKIT-MIGRATION-DESIGN-2026-09-30.md` |
| 全场景全维度审查（2026-09-29） | `docs/reviews/CR-FULL-SCENARIO-AUDIT-2026-09-29.md` |
| 全场景审查修复落地 | `docs/reviews/CR-FULL-SCENARIO-AUDIT-FIXES-2026-09-30.md` |
| 计费维护与兜底名复核 | `docs/operations/pricing-maintenance.md` |
| 生产发布门禁 | `docs/operations/new-api-production-release-gates.md` |
| 上一份交接（2026-09-20） | `docs/HANDOFF-NEW-API-2026-09-20.md` |
| 架构方案 | `docs/architecture/enterprise-auth-three-phase-plan.md` |

## 9. 跨会话记忆索引

位于 `/Users/mison/.claude/projects/-Volumes-Work-code-new-api/memory/`，索引为 `MEMORY.md`。与本次交接直接相关的条目：

| 记忆 | 要点 |
| --- | --- |
| `concurrent-claude-sessions-same-repo` | 同仓多会话：构建会瞬时失败，构建缓存争用，`git commit` 会扫入其他会话的暂存文件 |
| `new-api-session-handoff-2026-09-23` | 上一份交接快照 |
| `new-api-prod-binary-predates-alias-normalizer` | 正式二进制早于别名归一化器，`z-ai/glm-5.3` 等仍按 37.5 兜底，`/api/pricing` 看不出差距 |
| `new-api-case-normalization-prod-is-proven-not-inferable` | 生产的大小写归一化只能实测；三种静态判据全部判错 |
| `new-api-37.5-fallback-names-are-vendor-verified-unpriced` | 37.5 兜底名两类：17 个确认无价 / 2 个大小写变体已补键 |
| `unpriced-models-are-a-feature-not-a-pricing-bug` | 未定价模型是 fail-closed 特性，不是计费缺陷 |
| `mysql-key-column-needs-dialect-quoting` | MySQL 上 `key` 是保留字，用 `model.KeyColumn()`；包装 gorm dialector 必须嵌入驱动的具体类型 |
| `relaykit-dto-cannot-be-aliased` | 33 / 136 个同名类型字段双向不同；上游重写 281 个 import 而非别名 |
| `relaykit-migration-dto-scale-and-streaming` | dto 是 281 import / 394 函数（非 23 方法）；宿主侧流过滤会破坏 SSE 索引 |
| `relaykit-migration-spec-review-rounds` | 每一轮复核都抓到我刚引入的错误，包括我亲自验证过的结论 |
| `numeric-claims-need-the-counting-method-stated` | 数值断言必须给出计数口径；「已完成」「纯增益」这类封口断言需要枚举比较维度 |
| `reverse-verification-must-fail-for-the-right-reason` | 回归测试必须在还原修复后按正确原因失败 |
| `shuffle-count-reveals-cross-run-test-leaks` | `-count=1` 全绿会掩盖 57 个跨运行泄漏失败 |
| `verify-db-migrations-against-live-pg-stats` | 用 `pg_stat_user_indexes` + 真实数据分布判断索引/迁移安全性 |
| `never-run-unbounded-recursive-scan-on-this-machine` | 不要跑无界递归扫描（`find /`、`ls /Volumes/Work/**`） |
| `subagent-audit-claims-need-independent-verification` | 子代理审计结论必须独立复现；曾 7 个代理中 4 个的 P0/P1 被推翻 |
| `orca-release-unknown-is-not-a-failure-to-force` | `release_unknown` 是设计行为，不得强制替代 |
| `tsc-typecheck-eventually-completes` | `tsc -b` 约 225 秒，是慢不是卡死 |
