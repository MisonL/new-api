# 上游变更选择性吸纳方案（2026-05-30）

## 结论

本轮上游变更不能直接 `merge` 或 `rebase` 到本项目。必须采用选择性吸纳：

- 先冻结当前未提交改动，不在 dirty worktree 上处理上游。
- 只把确定收益明确、影响范围可控的修复分批移植。
- 对支付、订阅、前端大改、桌面栈替换、部署链路替换单独评审。
- 对会删除本项目自有能力的上游路径明确拒绝整包吸纳。

## 当前事实快照

采样时间：2026-05-30 13:14:43 +0800

| 项目 | 值 |
| --- | --- |
| 当前分支 | `codex/protocol-conversion-webui` |
| 当前 HEAD | `06f7fa532` |
| `origin/main` | `13bba9b91` |
| `upstream/main` | `158802708` |
| merge-base | `dac55f0fdeb1` |
| 分歧数量 | 本项目 ahead 403，上游 ahead 115 |
| 上游差异规模 | 1765 files changed, 87338 insertions, 121495 deletions |
| 上游新增文件数 | 135 |
| 上游删除文件数 | 362 |
| 静态 merge 冲突路径数 | 140 |

当前工作区存在未提交改动，覆盖 Header Profile、请求头模板、i18n、浏览器兼容、前端配置等路径。任何上游吸纳前必须先提交、暂存或用独立 worktree 保护这些改动。

## 直接合并风险

静态 `git merge-tree HEAD upstream/main` 显示冲突集中在以下区域：

- 后端核心：`controller/channel.go`、`controller/log.go`、`controller/relay.go`、`model/channel.go`、`model/log.go`、`relay/responses_handler.go`
- Header Profile 与渠道运行时：`service/channel_affinity_template_test.go`、`web/default/src/features/channels/*`
- Dashboard 与 usage logs：`web/default/src/features/dashboard/*`、`web/default/src/features/usage-logs/*`
- i18n：`web/default/src/i18n/locales/*`、`web/classic/src/i18n/locales/*`
- 布局、主题、pricing、rankings、subscriptions、wallet 等 default UI 大面
- Electron 与 Tauri、部署模板、README、多语言文档

直接合并会把“稳定性修复”“产品功能扩展”“UI 架构重构”“部署/桌面栈替换”混在同一次变更中，无法形成可审计回滚点。

## 必须保留的本项目能力

以下路径或能力属于本项目自有链路，上游 diff 中出现删除或替换时不能整包接受：

- 隔离开发环境：`deploy/compose/dev-isolated.yml`、`deploy/env/dev-isolated.env.example`
- 只读前端联调：`deploy/compose/frontend-readonly-proxy.yml`、`deploy/env/frontend-readonly.env.example`、`deploy/nginx/frontend-readonly.conf.template`
- Tauri 桌面端：`desktop/tauri-app/`
- Header Profile 与请求头策略：`dto/header_profile.go`、`dto/header_policy.go`、`service/channel_header_policy_runtime.go`、`service/header_policy.go`
- Header Profile 后端接口和测试：`controller/channel_header_profile_*`、`controller/user_header_profile*`、`controller/user_header_template*`
- Header Profile 前端：classic 与 default 中的 Header Profile Library、模板、策略配置、测试
- responses/compact 兼容与恢复能力：`relay/responses_handler.go`、`service/responses_bootstrap_recovery.go`、`relay/channel/openai/adaptor_responses_compaction_test.go`
- 定价维护文档和历史修复路径：`docs/operations/pricing-maintenance.md`、`pkg/pricingrepair/*`
- 已沉淀审计记录：`docs/reviews/*`
- 本项目现有 Web 回归测试：`web/tests/*`

这些路径如果和上游修复冲突，处理原则是“保留本项目能力，手工移植上游语义”，不是接受上游删除。

## 分批吸纳策略

### Batch 0：准备批次

目标：建立可回滚的吸纳工作面。

动作：

- 处理当前 dirty worktree：优先把现有未提交改动按当前任务单独提交；若暂不提交，创建 patch 或独立 worktree。
- 新建吸纳分支，例如 `codex/upstream-intake-20260530`。
- 生成本文件引用的基线命令输出，并在每批提交说明中记录来源上游 commit。
- 每批只处理一个领域，不跨越后端、前端、支付、桌面、部署边界。

禁止：

- 禁止在当前 dirty worktree 上直接 `merge upstream/main`。
- 禁止直接 `git checkout upstream/main -- .`。
- 禁止把上游删除作为“清理”接受。
- 禁止触碰正式 `3000` 服务。

验证：

- `git status --short --branch`
- `git diff --name-only`
- `git merge-tree HEAD upstream/main` 仅用于只读冲突评估

### Batch 1：低风险后端与 relay 修复

优先级最高，建议先做。每个条目都应按当前代码手工移植或 cherry-pick 后再解冲突。

| 上游提交 | 内容 | 建议 |
| --- | --- | --- |
| `74985fa87` | token log 精确过滤 | 吸纳，重点看 `model/log.go` |
| `1d3203736` | usage log 精确过滤，显式通配才模糊匹配 | 吸纳，避免破坏本地 usage log 字段 |
| `465c5edab` | Gemini 到 Claude `tool_use` 兼容修复 | 吸纳，补 relay 适配测试 |
| `ff06067a1` | Claude 并发工具调用索引修复 | 吸纳，重点查 `relay/channel/claude/relay-claude.go` |
| `2a528d46c` | image quality 参数处理修复 | 吸纳，检查 OpenAI image edit/generation 路径 |
| `128802818` | 上游错误日志截断 | 吸纳，但要保留本项目日志审计字段和错误可观测性 |
| `ebbe31553` | 自动禁用 multi-key channel 后清理缓存 | 吸纳，检查 channel cache 失效语义 |
| `ae6a03364` | 请求 metadata 提取与 disabled field 过滤性能优化 | 谨慎吸纳，涉及 middleware 和 override |
| `006e80165` | model `owned_by` 从 active channels 解析 | 吸纳，补 controller/model 层测试 |
| `38a3314b` | OpenAI image edit reference fields 保留 | 吸纳，和当前 dto/openai_image 口径比对 |

验证门槛：

- `go test ./controller ./model ./relay/common ./relay/helper ./service`
- 触及具体渠道时补跑对应包，例如 `go test ./relay/channel/claude ./relay/channel/gemini ./relay/channel/openai`
- `git diff --check`

回滚条件：

- 任一渠道请求体转换测试失败。
- 日志字段丢失或被截断到无法定位请求。
- 破坏本项目 Header Profile 或 param override 运行时。

当前执行记录：

- 已提交 `603d016cf`：手工吸纳 `ae6a03364` 中 disabled field 解析短路部分；请求 metadata 提取剩余部分仍待复核。
- 已提交 `42de48f13`：手工吸纳 `006e80165`，model `owned_by` 从 active channels 解析。
- 已提交 `e6a8e6214`：手工吸纳 `132d7b9f9`、`2d968c3ea`，channel list group filter 生效。
- 已提交 `ac32b4404`：结合 `74985fa87`、`1d3203736`、`554defe4f`、`b9bc6f0e2` 处理日志过滤，保留显式通配语义。
- 已提交 `698da0236`、`550d506aa`：手工吸纳 `465c5edab`、`ff06067a1`，修复 Gemini/Claude 工具流兼容和 Claude 并发工具索引。
- 已提交 `6455f3ceb`：手工吸纳 `2a528d46c`，保留 image quality 日志。
- 已提交 `a566ce68d`：手工吸纳 `aa56667b8` 的 upstream request id 追踪，但按本项目语义调整为独立 `logs.upstream_request_id` 字段、usage logs 筛选和详情展示，并通过请求头复制过滤避免上游 `X-Oneapi-Request-Id` 覆盖本地 request id。
- 已提交 `a566ce68d`：手工吸纳 `128802818` 的超长上游错误日志截断，但只截断本地运行日志输出；数据库日志内容、`showBodyWhenFail` 和结构化上游错误消息保持完整，避免降低审计和排障能力。
- 已提交 `a566ce68d`：修正 usage log 统计接口，使 request id 和 upstream request id 筛选同时影响列表与统计。
- 当前未提交子批次：手工吸纳 `ebbe31553` 中 multi-key key 匹配、实际可用 key 判断和重新启用恢复语义；保留本项目事务写库后 `InitChannelCache()` 的 cache 刷新方式。
- 已等价跳过 `38a3314b9`：当前代码已保留 OpenAI image edit JSON 请求、`images`、`mask`、`input_fidelity` 字段，并有 `relay/channel/openai/image_edit_test.go` 和 `relay/helper/openai_image_request_test.go` 覆盖。
- 已等价跳过 `5b86ce0d7`：当前 `model/utils.go` 已通过 `collectUserBatchDeltas` 和 `updateUserBatchDelta` 合并用户 quota、used_quota、request_count 更新，并有 `model/batch_update_test.go` 覆盖。
- 待继续复核：`fddf54ccc` 大 base64/body 生命周期优化、`ae6a03364` 请求 metadata 提取剩余部分。

当前未提交子批次验证记录：

- `go test ./model -run 'Test(GetLogsCanFilterUpstreamRequestId|RecordConsumeLogCopiesUpstreamRequestIdFromOther|SumUsedQuota)' -count=1 -v`：通过。
- `go test ./controller ./model ./service ./relay/common ./relay/channel/openai ./relay/channel/claude ./relay/channel/gemini -count=1`：通过。
- `git diff --check`：通过。
- `cd web/default && bun run lint`：通过。
- `cd web/default && bun run build`：通过。
- `go test ./model -run 'TestUpdateMultiKeyStatus|TestUpdateChannelStatusRefreshesMemoryCacheAfterEnable|TestBatchUpdateMergesUserCounters' -count=1 -v`：通过。
- `go test ./relay/channel/openai ./relay/helper -run 'Test(ConvertImageEditJSONRequestPreservesBody|DoImageEditJSONRequestUsesJSONRequestPath|GetAndValidOpenAIImageEditJSONPreservesReferenceFields)' -count=1 -v`：通过。

### Batch 2：日志、计费、模型与管理功能修复

这一批涉及管理后台和计费展示，影响比 Batch 1 大，但仍可拆成小提交。

| 上游提交 | 内容 | 建议 |
| --- | --- | --- |
| `349d5429` | API key 分页搜索响应处理 | 吸纳，前端 keys 页面验证 |
| `30025aeb` | channel test 使用真实 user id | 吸纳，确认本项目后台测试接口鉴权约束 |
| `6f11d198` | model pricing 展示漂移修复 | 谨慎吸纳，必须对照本项目定价维护文档 |
| `7fe896d2` | ratio 展示使用 `getUserGroups` | 谨慎吸纳，涉及 group ratio 语义 |
| `58ba867d` | channel test failure details UX | 延后到 UI 小批次，避免混入后端 |
| `8f9ee9b` | 允许清空 channel remark | 吸纳，表单空字符串和 null 语义要明确 |
| `132d7b9`、`2d968c3` | channel list group filter 修复 | 吸纳前先确认本项目 channel 查询已无等价修复 |
| `aa56667b` | upstream request id 追踪并防止响应头覆盖 | 独立小专题，涉及日志 schema 和 header 安全 |

验证门槛：

- `go test ./controller ./model ./service`
- 定价相关必须阅读 `docs/operations/pricing-maintenance.md`
- 前端触及时运行 `cd web/default && bun run lint && bun run build`
- 如触及 classic，补跑 `cd web/classic && bun run build`

回滚条件：

- 历史日志筛选结果语义变化未被测试覆盖。
- 计费展示或 ratio 计算无法解释为等价或明确修复。
- 后台接口需要的 `New-Api-User` 头或 session 语义被绕开。

### Batch 3：responses、compact 与协议转换专项

上游当前 diff 对本项目 responses/compact 路径主要表现为删除本地能力，不能整包吸纳。

裁决：

- 不接受上游删除 `service/responses_bootstrap_recovery.go`、`relay/channel/openai/adaptor_responses_compaction_test.go`、`relay/responses_handler_test.go` 等路径。
- `relay/responses_handler.go` 只能逐行审查有无独立 bugfix。
- 与 synthetic compact、context fallback、summary model fallback、原生 fallback 相关逻辑，以本项目当前实现为主。

验证门槛：

- `go test ./relay ./relay/channel/openai ./service`
- 覆盖 `/v1/responses`、`/v1/responses/compact`、stream 与 non-stream。
- 使用 3001 隔离开发环境做真实请求 smoke，不使用正式 3000。

回滚条件：

- compact state 无法恢复。
- native compact 失败后未按本项目策略进入 synthetic。
- fallback 请求上下文或模型回退行为与当前设计不一致。

### Batch 4：支付、订阅与合规功能

这些属于产品能力扩展，不应混入稳定性修复。

| 上游提交 | 内容 | 建议 |
| --- | --- | --- |
| `0526a226` | 付费功能合规确认 | 独立专题 |
| `19f1821` | Waffo Pancake 订阅支付完整集成 | 独立专题 |
| `f2c7647` | Waffo 订阅合规和产品 ID | 依赖 Waffo 专题 |
| `0354c38` | Waffo webhook 修复 | 依赖 Waffo 专题 |
| `c91ba0c` | Waffo 设置保存流程收口 | 依赖 Waffo 专题 |
| `6b6c990` | subscription 支持余额购买 | 独立专题 |
| `1588027` | subscription balance redemption toggle | 依赖余额购买专题 |

专题准入条件：

- 明确定义是否启用对应支付网关。
- 评估数据库字段迁移和三库兼容。
- 覆盖支付成功、失败、重复 webhook、签名错误、订单状态幂等。
- 前端同时验证 default 和 classic 当前加载路径。

验证门槛：

- `go test ./controller ./model ./service`
- 支付服务相关 package 测试
- `cd web/default && bun run lint && bun run build`
- `cd web/classic && bun run build`
- 隔离开发环境 3001 管理后台配置 smoke

回滚条件：

- 任一支付路径在未配置时不显式失败。
- webhook 幂等或签名校验没有测试。
- 新增付费入口绕过合规确认。

### Batch 5：default UI 大改

上游 default UI 已进入大范围 Base UI、主题、表格工具栏、渠道编辑器、系统设置重构。不能整包吸纳。

可拆分方向：

- 小修复：表单首个错误 focus、重复 toast、按钮 submit、pagination 文案、dark mode 可读性。
- 中等改动：usage logs 移动端工具栏、channel test failure details、system settings number input 修复。
- 大改动：Base UI migration、channel create/edit drawer rebuild、system settings drill-in sidebar、rankings dashboard、pricing detail 重构。

准入条件：

- 每次只选一个页面或组件群。
- 先写 UI 合约或最小测试，再手工移植。
- 必须保留 Header Profile、param override、channel affinity、usage log audit 等本项目能力。
- 浏览器验证亮色、暗色、桌面、移动端。

验证门槛：

- `cd web/default && bun run lint && bun run build`
- 相关 `web/tests/*.mjs` 或新增测试
- Vite dev server 代理 3001 做浏览器检查

回滚条件：

- 表格筛选或列配置丢失。
- Header Profile 配置入口缺失。
- 文案/i18n key 缺失。
- 移动端或暗色主题明显破坏。

### Batch 6：classic UI

classic 仍可能被系统配置加载。上游 classic 改动不能被忽略，也不能用 default 验证替代。

策略：

- 只吸纳与当前 classic 实际入口相关的小修。
- 不接受删除 Header Profile classic UI。
- 不接受删除 usage logs 审计信息展示。
- 支付订阅相关 classic 改动必须跟支付专题走。

验证门槛：

- `cd web/classic && bun run build`
- 与 default 同步检查 i18n JSON 可解析。

### Batch 7：桌面与部署

上游新增 `electron/` 并删除 `desktop/tauri-app/`，同时删除本项目隔离开发和只读联调 Compose。当前裁决为不吸纳整包变更。

保留：

- Tauri sidecar 隔离工作目录和环境变量语义。
- `NEW_API_SKIP_DOTENV=true` 桌面隔离。
- 3001 dev isolated stack。
- frontend readonly proxy。

仅可吸纳：

- 明确不影响当前部署链路的 `.dockerignore`、license 文件、构建元数据小修。
- 任何 Dockerfile 改动必须确认不破坏本项目 build-info 和 dev/prod traceability。

验证门槛：

- `scripts/build-docker-local.sh new-api-local:dev`
- `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api`
- `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info`
- `curl -fsS http://127.0.0.1:3001/api/status`

回滚条件：

- build-info 缺失或和 Git commit 不一致。
- dev/prod 端口和 compose project 隔离被破坏。
- 正式服务 3000 被误操作。

### Batch 8：文档、license、issue template

可低风险选择性吸纳：

- `NOTICE`
- `THIRD-PARTY-LICENSES.md`
- 多语言 README 中与本项目不冲突的通用介绍
- `docs/translation-glossary*.md`
- 英文 issue template

必须保留：

- 本项目 README 中的独立演进、部署、header profile、compact、pricing 说明。
- `docs/reviews/*` 审计资料。
- `docs/operations/pricing-maintenance.md`。

验证门槛：

- 文档链接存在性检查。
- 不引用不存在截图、Demo 或上游不适用于本项目的部署方式。

## 推荐执行顺序

1. 保护当前 dirty worktree。
2. 创建上游吸纳工作分支或 worktree。
3. 实施 Batch 1，小提交，每个提交只覆盖一个上游语义簇。
4. 跑后端最小测试和 `git diff --check`。
5. 实施 Batch 2 中确认无等价实现的小修。
6. 对 Batch 3 只做只读复核，除非发现明确 bugfix。
7. 对 Batch 4、Batch 5、Batch 7 创建独立专题计划，不混入本轮。
8. 产出每批验证记录到 `docs/reviews/`。
9. 所有批次完成后，才考虑推送。
10. 仅在用户明确授权后，才进入正式服务升级流程。

## 上游提交裁决索引

`origin/main..upstream/main` 共 115 个上游 ahead 提交，其中 111 个为非 merge 提交，4 个为 merge commit。实施只按 111 个非 merge 提交追踪；merge commit 不单独实施。

| Commit | 标题 | 裁决 |
| --- | --- | --- |
| `8b2b03d27` | feat(web/default): unified UI overhaul - Base UI migration, theme presets, rankings dashboard, and table toolbar refactor (#4633) | Batch 5，UI 专题，不整包覆盖 |
| `9acf5feca` | feat: collect model performance metrics (#4635) | Batch 5，UI/性能指标专题 |
| `0f9f094a4` | feat(default): reorganize system settings pricing UI | Batch 5，UI 专题 |
| `f8cf9c57c` | feat(default): add real rankings data | Batch 5，rankings 专题 |
| `dc8deb0c2` | fix: enable channel table server-side sorting (#4600) | Batch 2，评估吸纳 |
| `446a8420f` | fix(default): correct subscription payment display | Batch 4，支付/订阅专题 |
| `dede1e296` | fix(default): improve billing settings forms | Batch 4，支付/订阅专题 |
| `ee190b604` | docs(security): add bulk reporting policy with block warning | Batch 8，文档选择性吸纳 |
| `5c793d799` | refactor: move top_up_link from status API to topup info API | 单独复核，涉及状态 API 兼容 |
| `38a3314b9` | fix: preserve OpenAI image edit reference fields (#4646) | Batch 1，优先吸纳 |
| `d98f0e8ac` | fix: migrate select to Base UI items API (#4655) | Batch 5，UI 专题 |
| `e8cfb546f` | feat(default): add model performance badges | Batch 5，UI/性能指标专题 |
| `a7d019e3a` | feat(default): redesign dashboard overview | Batch 5，dashboard UI 专题 |
| `abc255dd6` | fix(default): keep SectionPageLayout description slot hidden | Batch 5，已在历史 UI intake 中处理过，复核后决定是否跳过 |
| `415d21d07` | refactor(layout): rename workspace switcher to system brand | Batch 5，已在历史 UI intake 中处理过，复核后决定是否跳过 |
| `a7475a1e6` | fix(web): align UI and charts with theme tokens and presets | Batch 5，已在历史 UI intake 中部分处理，复核剩余 |
| `faa0f1425` | fix: qualify column names in PerfMetric upsert to avoid ambiguity | Batch 2，后端 SQL 小修优先评估 |
| `c19d5aa66` | feat: Add model performance metrics to dashboard | Batch 5，性能指标专题 |
| `948780e3f` | fix(theme): align UI controls with global radius tokens | Batch 5，主题专题 |
| `560ba57c8` | feat: add DeepChat deeplink support (#4668) | Batch 8，文档/能力入口选择性评估 |
| `d146e45e2` | chore(web/default): add reusable copyright header tooling | Batch 8，license/版权工具选择性评估 |
| `543cc64ea` | feat(licenses): add LICENSE, NOTICE, and THIRD-PARTY-LICENSES files to Docker images | Batch 8，license 优先吸纳但保留本项目说明 |
| `5fa103fa5` | fix: exclude THIRD-PARTY-LICENSES.md from .dockerignore for Docker build (#4739) | Batch 8，配合 license 吸纳 |
| `ba474393f` | fix(default): resolve v1 frontend issue regressions | Batch 5，UI 专题 |
| `19fc384e6` | feat(performance): update performance metrics handling and UI components | Batch 5，性能指标专题 |
| `03d537328` | fix(default): improve performance health panel layout | Batch 5，性能指标 UI 专题 |
| `3057f04a1` | fix(wallet): read topup gateway flags from topupInfo instead of status (#4599) | Batch 2，钱包/状态 API 兼容复核 |
| `7fe896d2f` | fix: use getUserGroups for ratio display to respect GroupGroupRatio (#4772) | Batch 2，ratio 语义复核 |
| `2b89989f6` | fix(default): support DropdownMenuItem onSelect (#4787) | Batch 5，UI 小修 |
| `fde2cac9d` | fix(web/default): guard playground messages against legacy classic shape (#4650) | Batch 5，playground UI 小修 |
| `469d3747a` | fix: defaut ui triage (#4802) | Batch 5，UI 小修集合，拆分吸纳 |
| `3856b9d2c` | chore(deps): bump axios from 1.15.0 to 1.15.2 in /web/classic (#4634) | Batch 7，依赖专题 |
| `428e3d91f` | chore: refresh related resources | Batch 8，仓库资源选择性复核 |
| `aa56667b8` | feat: track upstream request ID and prevent response header override | Batch 1，优先吸纳但独立小提交 |
| `0526a2264` | feat: require compliance confirmation for paid features | Batch 4，支付合规专题 |
| `3e588b4d4` | chore(deps-dev): bump ip-address from 10.1.0 to 10.2.0 in /electron (#4811) | Batch 7，Electron 相关，当前不替换 Tauri |
| `51b5cbe1b` | fix: prevent combobox from over-filtering options on focus (#4829) | Batch 5，UI 小修 |
| `18282e610` | chore(deps): update axios from 1.15.0 to 1.15.2 | Batch 7，依赖专题 |
| `3caa6e467` | fix(web/default): batch fix new UI issues #4880 #4893 #4817 #4877 #4898 | Batch 5，UI 小修集合，拆分吸纳 |
| `8f9ee9ba8` | fix: allow clearing channel remark (#4886) | Batch 1，优先吸纳 |
| `554defe4f` | fix: correct usage logs filtering (#4883) | Batch 2，需结合后续 revert 复核 |
| `8a10dedb7` | fix(web): handle unlimited API key quota validation (#4881) | Batch 2，keys UI/表单复核 |
| `6f8668e4c` | fix: enforce header nav access control for public modules (#4889) | 单独复核，涉及公开导航权限 |
| `132d7b9f9` | fix: GetAllChannels ignores group filter parameter (#4847) | Batch 1，优先吸纳前确认是否已等价修复 |
| `2d968c3ea` | fix: apply group filter to channel list queries (#4885) | Batch 1，优先吸纳前确认是否已等价修复 |
| `68830e609` | feat: support request_header key source (#4903) | 单独复核，和本项目 Header Profile 架构强相关 |
| `f69ceb696` | fix: 修复新 UI 语言与文案显示问题 (#4876) | Batch 5，i18n/UI 专题 |
| `5dd0d3bcb` | fix: add analytics placeholder (#4928) | Batch 5，UI 小修，低优先级 |
| `0936e2504` | perf: avoid eager formatting in debug log calls (#4929) | 单独复核，可能作为后端性能小修 |
| `ee9736bbc` | fix: add type="submit" to forgot password form button (#4910) | Batch 5，auth UI 小修 |
| `04b4483d7` | fix(web): normalize model detail tabs layout (#4938) | Batch 5，models UI 小修 |
| `8ae095c3b` | fix user create and delete handling (#4818) | 单独复核，用户管理路径 |
| `b397c58ba` | fix(auth): expose register_enabled in /api/status and gate sign-up link (#4871) | 单独复核，状态 API 与注册入口 |
| `fc08c133e` | fix(web/default): update pagination button labels in ModelCardGrid (#4675) | Batch 5，pricing UI 小修 |
| `cb9270ed2` | fix(auth): localize reset password confirmation (#4769) | 单独复核，auth i18n 小修 |
| `8db32213e` | fix(web/default/wallet): make recharge preset selection visible in dark mode (#4897) | Batch 5，wallet UI 小修 |
| `c78573ce0` | fix(web/default): api-info color dot shows wrong color due to semantic token mismatch (#4824) | Batch 5，theme/UI 小修 |
| `032993ed4` | fix: check save result in handleSaveAll and add slate to validColors (#4823) | Batch 5，settings/theme 小修 |
| `0cd9a3a06` | fix(auth): use aff_code field name in registration payload (#4945) (#4965) | 单独复核，注册 payload 兼容 |
| `5e88f97ac` | fix(data-table): make faceted filter popover width adaptive (#4905) (#4966) | Batch 5，data table UI 小修 |
| `146dd77b8` | fix(keys): call submit handler directly to avoid stale form linkage (#4858) (#4967) | Batch 5，keys UI 小修 |
| `0d4b25795` | fix: expose param override audits for sensitive message fields (#4974) | Batch 2，日志审计专题，需保留本项目 param override |
| `2d1ca1538` | fix: respect dashboard content visibility settings (#4975) | Batch 2，dashboard 设置语义复核 |
| `20d3e7373` | fix: filter perf metrics summary by active groups (#4976) | Batch 2，性能指标后端复核 |
| `58ba867dd` | fix: improve channel test failure details UX (#4988) | Batch 2 或 Batch 5，依赖 channel test UI 当前结构 |
| `6f11d1987` | fix: normalize model pricing display drift (#4985) | Batch 2，定价展示复核 |
| `006e80165` | fix: resolve model owned_by from active channels (#4416) | Batch 1，优先吸纳 |
| `ae6a03364` | perf: optimize request metadata extraction and disabled field filtering (#5009) | Batch 1，谨慎吸纳 |
| `e13d67345` | fix: update default frontend hardcoded route links (#5016) | Batch 5，UI 路由小修 |
| `8e5e89bb5` | 修复 切换新版前端Turnstile 开启后注册页未显示验证的问题 (#5011) | Batch 5，auth UI 小修 |
| `19f1821fc` | Feature Request: Waffo Pancake gateway full integration with subscription support and admin catalog binding flow (#4935) | Batch 4，Waffo 专题 |
| `f2c7647ec` | fix: enforce Waffo subscription compliance and product ID update (#5038) | Batch 4，Waffo 专题 |
| `b9bc6f0e2` | Revert "fix: correct usage logs filtering (#4883)" | 单独复核，和 `554defe4f`、`1d3203736` 成组判断 |
| `fddf54ccc` | perf: reduce heap residency for large base64 relay requests | Batch 1，优先吸纳但需覆盖大 payload 流程 |
| `ebbe31553` | fix(channel): evict auto-disabled multi-key channels from cache (#4983) | Batch 1，优先吸纳 |
| `0354c38be` | BugFix: fix webhook process (#5047) | Batch 4，Waffo 专题 |
| `49bc3a117` | fix(payment): hide classic Waffo Pancake settings (#5085) | Batch 4，Waffo/classic 专题 |
| `92a095944` | refactor(web/default): adopt drill-in sidebar pattern for System Settings | Batch 5，system settings UI 专题 |
| `b08febaa3` | refactor: system settings UI for consistent, compact layouts | Batch 5，system settings UI 专题 |
| `88437a186` | chore(deps): Upgrade default frontend dependencies | Batch 7，依赖专题 |
| `b302be30e` | fix: v1 interface feedback regressions | Batch 5，UI 小修集合，拆分吸纳 |
| `583da4529` | refactor(ui): Improve usage log filter responsiveness and mobile UX | Batch 5，usage logs UI 专题 |
| `2a528d46c` | fix(relay): correct image quality parameter handling (#5103) | Batch 1，优先吸纳 |
| `128802818` | fix: truncate oversized upstream error logs (#5083) | Batch 1，优先吸纳 |
| `51ca897cf` | refactor(home): redesign hero section to dual-column layout with compliant copywriting | Batch 5，home UI 专题，低优先级 |
| `ff06067a1` | fix: 移除 fcIdx -1 偏移，修复并发工具调用撞键问题 (#5095) | Batch 1，优先吸纳 |
| `465c5edab` | fix:gemini to claude tool_use err (#5041) | Batch 1，优先吸纳 |
| `349d5429c` | fix: handle paginated API key search response (#5014) | Batch 2，keys UI/API 小修 |
| `3d850d38b` | refactor(channels): rebuild channel create/edit drawer with modular sections and improved form UX | Batch 5，channels UI 专题，不能整包覆盖 |
| `336088264` | refactor(channels): rebuild channel editor UX with modular sections and Base UI multi-select | Batch 5，channels UI 专题，不能整包覆盖 |
| `a64f26d1d` | feat(web/default): add Anthropic theme preset and configurable serif typography | Batch 5，theme 专题 |
| `ad224ecf5` | fix: prevent duplicate channel action toasts (#5015) | Batch 5，channels UI 小修 |
| `bc8110ce3` | refactor(badge): restore status-badge sizes and classic color scheme | Batch 5，UI 小修 |
| `101193498` | fix(theme): default theme font preset falls back to Sans instead of Serif | Batch 5，theme 小修 |
| `6b6c9904a` | feat(subscription): support balance purchases | Batch 4，subscription 专题 |
| `a8b7c92e5` | fix(logs): restore timing background badges and optimize model/token spacing | Batch 5，usage logs UI 小修 |
| `9e283ab10` | fix(logs): remove hardcoded font-mono to support global theme font inheritance | Batch 5，usage logs UI 小修 |
| `f223db933` | fix(charts): improve dark mode chart readability | Batch 5，charts UI 小修 |
| `c91ba0c4e` | fix: consolidate Waffo payment settings save flow (#5110) | Batch 4，Waffo 专题 |
| `30025aeba` | fix: use actual user id for channel tests (#5109) | Batch 2，channel test 后端/接口复核 |
| `5bc4c7481` | fix(logs): tune usage table typography | Batch 5，usage logs UI 小修 |
| `65f8afe92` | fix(system-settings): resolve save detection and number input NaN issues | Batch 5，system settings UI 小修 |
| `f8add4ca4` | feat(theme): add simple-large preset, xl scale and clean up channel badge dots | Batch 5，theme 专题 |
| `dc245ae76` | fix(web): improve channel and usage log UI | Batch 5，UI 小修集合，拆分吸纳 |
| `1d3203736` | fix: keep usage log filters exact unless wildcard is explicit (#5097) | Batch 1，优先吸纳 |
| `74985fa87` | fix: keep token log filters exact | Batch 1，优先吸纳 |
| `5b86ce0d7` | fix: optimize batch update process | Batch 1，优先吸纳前确认批量更新范围 |
| `63ead2bf7` | chore(repo): ignore playwright mcp artifacts | Batch 8，仓库卫生小修 |
| `e79cee1e9` | perf(form): focus first validation error on submit | Batch 5，表单体验小修 |
| `38bf2d8da` | feat(keys/cc-switch-dialog): 修复自定义cc-switch名称失焦后重置问题 (#5170) | Batch 5，keys UI 小修 |
| `158802708` | feat: add subscription balance redemption toggle (#3071) | Batch 4，subscription 专题 |

## 全局验证矩阵

| 触及范围 | 必跑验证 |
| --- | --- |
| 后端 controller/model/service | `go test ./controller ./model ./service` |
| relay 或协议转换 | `go test ./relay/common ./relay/helper ./relay/channel/claude ./relay/channel/gemini ./relay/channel/openai ./service` |
| compact/responses | `go test ./relay ./relay/channel/openai ./service`，再做 3001 真实请求 smoke |
| default 前端 | `cd web/default && bun run lint && bun run build` |
| classic 前端 | `cd web/classic && bun run build` |
| i18n | JSON/YAML 解析检查，必要时 `bun run i18n:sync` |
| Docker/dev 环境 | build local image，重建 3001 dev isolated，检查 `/api/status` 和 `--build-info` |
| 支付/订阅 | 后端测试、前端构建、隔离环境配置 smoke、幂等/签名/失败路径测试 |

## 风险评估

| 方案 | 风险 | 裁决 |
| --- | --- | --- |
| 直接 merge/rebase `upstream/main` | 极高 | 拒绝 |
| cherry-pick Batch 1 后端小修 | 中低 | 推荐 |
| 手工移植 Batch 2 管理和日志小修 | 中 | 推荐但需逐项验证 |
| responses/compact 整包吸纳 | 高 | 拒绝，改为专项逐行复核 |
| 支付/订阅功能整批吸纳 | 高 | 独立专题 |
| default UI 大改整包吸纳 | 高 | 独立专题 |
| Electron 替换 Tauri | 极高 | 当前拒绝 |
| 删除 dev-isolated/frontend-readonly | 极高 | 拒绝 |
| 文档/license 选择性吸纳 | 低 | 可做 |

## 完成判定

本方案完成不等于上游已吸纳完成。后续只有同时满足以下条件，才可认为对应批次完成：

- 每个吸纳项都有明确来源 commit 和本项目适配说明。
- 没有接受禁止删除路径。
- 对应测试按范围通过。
- 变更被拆成可回滚提交。
- 3001 隔离环境在需要时验证通过。
- 正式 3000 未被操作，除非用户明确授权。
