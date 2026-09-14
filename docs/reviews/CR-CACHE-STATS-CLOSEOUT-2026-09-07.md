# 缓存统计与优化清单复核

## 范围与约束

- 采样日期：2026-09-07，Asia/Shanghai。
- 分支保持 `dev`，HEAD 保持 `f825f23d870df24e38362a7bd1d0a2cd4423a1a6`。
- 用户要求在当前分支优化，不拆分分支、不提交、不推送。既有 dirty/untracked 文件保留。
- 未发布、重启或修改正式服务，未回算生产计费，未调用真实计费渠道。150 渠道与 Responses WebSocket 新协议实现不在本次范围；未使用 CodeRabbit。

## 本轮修改

1. `Stat.CacheRate` 改为 `*float64`，无有效缓存读遥测或分母时输出 JSON `null`。明确的零读缓存且输入大于零仍输出数字 `0`。
2. 缺失、null、非法或负的缓存读遥测不再作为零命中样本稀释汇总缓存率；输入/输出 Token 总量仍统计全部消费日志。
3. Default/Classic 日志输入列、统计区域及 Classic 首页保留缺失语义，显示 `N/A`。
4. Default 真正首页 `overview` 在既有 SummaryCards 后新增输入、缓存读、缓存写、输出及缓存率五项统计。模型分析页的历史额度聚合数据源保持不变，避免将最近一分钟 RPM 当作请求总数。
5. 新首页卡片使用 React Query、按用户和权限隔离查询缓存、传递 AbortSignal、展示实际时间范围、支持刷新，并校验统计响应。失败或缺字段时显式显示错误，不制造零用量结果。
6. 统计接口把 HTTP 请求上下文传递到 GORM 查询；客户端取消后允许驱动终止扫描。
7. 后端仅解码缓存统计相关 JSON 字段，避免构造无关诊断字段的完整对象树。保留旧缓存写字段的既有兼容顺序。

## 本轮验证

以下已读取完成结果，退出码均为 0，除显式标记的红灯测试：

| 检查 | 命令或证据 | 结果 |
| --- | --- | --- |
| 缓存率红灯 | `go test ./model -run '^TestSumUsedQuotaLeavesCacheRateUnavailableWithoutInput$' -count=1` | 修改前退出 1，预期 nil，实际 0 |
| 缓存遥测红灯 | `go test ./model -run '^TestSumUsedQuotaCacheRateTelemetry$' -count=1` | 修改前 missing/null/invalid/negative 均失败 |
| 两前端红灯 | Default `bun test tests/usage-logs-format.test.ts`；Classic `bun test tests/usageLogsTokenStats.test.js` | 修改前缺失遥测均被当作 0，退出 1 |
| 定向绿灯 | Default `bun test tests/dashboard-token-stats.test.ts tests/usage-logs-format.test.ts` | 16 通过，0 失败 |
| Classic 绿灯 | `bun test tests/usageLogsTokenStats.test.js` | 4 通过，0 失败 |
| 后端主回归 | `go test -count=1 ./controller ./model ./service/... ./relay/... ./common ./router` | 全部列出包通过；无测试文件包不算覆盖 |
| 其他后端回归 | `go test -count=1 ./middleware ./dto ./types ./constant ./setting/... ./pkg/... ./oauth/...` | 全部列出包通过 |
| 后端静态检查 | `go vet ./controller ./model ./service/... ./relay/... ./middleware ./common` | 通过 |
| 最后统计修改回归 | `go test -count=1 ./model ./controller` | 通过，含上下文取消测试 |
| race | `go test -race -count=1 ./model ./controller ./service ./middleware ./common` | 五包通过 |
| Default 全量 lint | `bun run lint` | 通过；后续新增细节另跑定向 ESLint，通过 |
| Default 类型 | `bun run typecheck` | 初次报新增 trim 类型收窄错误，改为 unknown 后重跑退出 0 |
| Classic 格式 | `bun run lint` | 通过，脚本实际为 Prettier 检查，不是 ESLint |
| Default 构建 | `bun run build` | 通过，Safari 重写 4 个文件，兼容检查通过 |
| Classic 构建 | `bun run build` | 通过，Safari 检查通过；仍有既有大 chunk 警告 |
| 最终定向检查 | 定向 ESLint + Default 两个测试文件 | 退出 0，16 测试通过 |
| 镜像构建 | `scripts/build-docker-local.sh new-api-local:dev` | 两次成功，第二次包含最后后端与前端修改 |
| 隔离重建 | `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` | 退出 0，仅隔离 new-api 服务重建 |
| 隔离身份 | `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` | `v1.1.0`，`f825f23...-dirty`，`2026-09-07T12:49:41Z` |
| 隔离镜像 | `docker inspect` | `sha256:7f4f9ad572d8cdc790cb9b7e8785eb3b243480df814c1e7f1c027918c7a74fdc`；healthy；restart=0 |
| 本机健康 | 带 connect-timeout/max-time 的 13000、3000、3001 `/api/healthz` | 均返回 `ok`，不等于 LAN 或业务验收 |
| 差异检查 | `git diff --check` 及修改路径定向检查 | 无空白错误；未提交或推送 |

## 统计查询基准

命令：`go test ./model -run '^TestSumUsedQuota' -bench '^BenchmarkSumLogTokenStats$' -benchtime=5x -count=1 -benchmem`。

使用真实 SQLite 内存数据库和 1,000 条构造日志；不是生产流量，也不是 PostgreSQL/MySQL 验收。

| 路径 | 耗时 | 分配字节 | 分配次数 |
| --- | --- | --- | --- |
| 完整 other map | 5,592,202 ns/op | 2,948,681 B/op | 51,050 allocs/op |
| 仅统计字段 | 6,972,598 ns/op | 877,784 B/op | 18,051 allocs/op |

此采样仅证明约 70% 分配字节下降和约 65% 分配次数下降，不证明耗时改善。查询仍为筛选日志的线性扫描，不宣称已经完成大规模聚合优化。

## 对此前清单的纠正与剩余项

- 分支拆分和提交推送：按用户新要求取消，不是阻塞项。未跟踪文件不等于垃圾，不能批量删除；镜像构建已包含工作区依赖，但不代表将来提交会自动包含它们。
- Ollama StreamOptions：当前源码已有专用兼容测试，本轮 relay 回归通过，不再列作确定的未修复缺陷；真实上游仍未验证。
- Responses 终态：9 月 5 日已有缺失终态显式失败等修复，本轮相关包通过；不得继续笼统标为全部未实现，也不得以本地测试替代真实客户端契约。
- replay 落盘：现有路由先鉴权，已有容量、余量、取消和排队保护；相关 common/middleware 测试通过。768 MB 是显式上限，不能仅凭数字判定未鉴权漏洞。跨进程/多实例资源竞争未覆盖。
- 任务生命周期：9 月 7 日 Midjourney claim 已覆盖响应合并、CAS 与失败退款，见 `CR-TASK-ROUTING-504-RECHECK-2026-09-02.md`；schema 5 runtime 仍使用当前渠道配置的兼容路径，完整运行时冻结和真实 201 验收未完成。
- 缓存聚合表/历史回填：未引入；需另行设计三库兼容、删除日志同步和回填可恢复性。当前减少分配及支持取消不替代此项。
- 历史计费回算：没有证实新的生产金额错误，未执行生产修复；原有计费相关测试通过不能证明历史账目正确。
- 浏览器：首次 5176 探针早于首次编译完成；2026-09-08 已确认独立 Default 入口可访问，并完成未登录移动端亮色/暗色截图检查。登录后首页统计卡和真实数据态仍未覆盖。
- LAN、真实 Codex/Claude、真实计费渠道、Redis 跨进程故障、PostgreSQL/MySQL 实际统计契约、生产发布回滚：未完成，本机 healthz 成功不能替代。
- Responses WebSocket 继续 501 fail-closed；不开放未实现协议。150 渠道不处理。未使用 CodeRabbit，也没有新的 Claude/OMP 审查报告。

本记录不是整仓或生产 approval。

## 2026-09-08 开发环境复核

本次仍在 `dev` 分支的 dirty 工作区执行，未创建分支、未提交、未推送，未重启正式服务。

| 检查 | 命令或证据 | 结果 |
| --- | --- | --- |
| Go 全量测试 | `go test -count=1 ./...` | 全部可编译包通过；无测试文件包明确标记为 `[no test files]` |
| Go 全量静态检查 | `go vet ./...` | 退出码 0 |
| Default lint/typecheck/build | `bun run lint && bun run typecheck && bun run build` | 全部通过；Safari 兼容性检查通过 |
| Classic lint/build | `bun run lint && bun run build` | Prettier、构建及 Safari 兼容性检查通过 |
| PostgreSQL 真实驱动 | `NEW_API_POSTGRES_TEST_DSN=... go test -count=1 ./model -run '^TestInsertChannelSynchronizesDriftedPostgresSequence$'` | 隔离 PostgreSQL 通过 |
| 隔离服务 | 容器 `new-api-dev-isolated-new-api-1`、`/new-api --build-info` | `healthy`，`restart=0`，运行 `f825f23...-dirty` |
| 健康探针 | `curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/healthz` | 返回 `ok` |
| 差异检查 | `git diff --check` | 通过 |
| 浏览器静态验收 | 独立 Default 开发入口；移动端亮色/暗色登录页截图 | 页面可访问，未发现控件重叠或文字溢出；未登录首页统计卡及登录后数据态仍未覆盖 |

Default 构建仍报告既有大 chunk 警告，但构建成功；本次没有证据证明通过拆分 chunk 能改善生产加载耗时，因此未做无依据的配置改动。

本次全量开发环境验证未发现新增可安全直接修复的正确性问题。局域网其他主机、正式 Caddy/13000、真实 Codex/Claude、真实上游渠道、MySQL 真实驱动、Redis 跨进程故障及生产回滚仍需真实环境证据，不能由隔离环境结果替代。
