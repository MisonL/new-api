# 日志缓存 Token 统计验证记录

## 范围

- 分支：`dev`
- 当前代码基线：`f825f23d870df24e38362a7bd1d0a2cd4423a1a6`（工作区含既有 dirty/untracked 改动）
- 采样时间：2026-09-01，Asia/Shanghai
- 本记录只覆盖日志缓存 Token 统计、Default 日志页和 Classic 首页相关改动。
- 未操作正式服务、Caddy、生产数据库或生产容器；仅使用隔离开发容器 `new-api-dev-isolated-new-api-1`（宿主端口 `127.0.0.1:3001`）。

## 实现口径

- 后端统计输入 Token 使用 `logs.prompt_tokens`，输出 Token 使用 `logs.completion_tokens`。
- 缓存读使用 `other.cache_tokens`。
- 缓存写优先使用规范化字段 `other.cache_write_tokens`，兼容旧日志时回退到 `cache_creation_tokens_5m + cache_creation_tokens_1h`，再回退到 `cache_creation_tokens`。
- 缓存率按每条日志的分母加权汇总：Anthropic 语义为 `cache_read / (input + cache_read)`；其他语义为 `cache_read / input`。无有效分母时返回 `0`。
- `/api/log/stat` 与 `/api/log/self/stat` 新增 `input_tokens`、`cache_read_tokens`、`cache_write_tokens`、`output_tokens`、`cache_rate`。
- Default 日志输入列第三行显示缓存率；Default 与 Classic 的日志统计区域显示缓存率；Classic 首页在“资源消耗”后增加输入、缓存读、缓存写、输出和缓存率五项统计。

## 验证结果

| 检查 | 命令 | 结果 |
| --- | --- | --- |
| 后端缓存统计定向测试 | `go test ./model -run '^TestSumUsedQuota(UsesSingleFilteredConsumeStat\|AggregatesCacheTokenStatsByUsageSemantic)$' -count=1` | 退出码 0，通过 |
| Model + Controller 联合回归 | `go test ./model ./controller` | 退出码 0，通过；Model 与 Controller 全量包均通过 |
| 相关服务与 Relay 回归 | `go test ./service`、`go test ./relay/common ./relay/helper` | 退出码 0，通过 |
| 推荐范围 Go 回归 | `go test ./controller ./model ./relay/common ./relay/helper ./service` | 退出码 0，通过；五个相关包全部通过 |
| Default 缓存统计测试 | `bun test tests/usage-logs-format.test.ts` | 退出码 0，13 项通过 |
| Classic 缓存统计测试 | `bun test tests/usageLogsTokenStats.test.js` | 退出码 0，3 项通过 |
| Default 定向格式检查 | `bun x prettier --check src/features/usage-logs/components/columns/common-logs-columns.tsx src/features/usage-logs/components/common-logs-stats.tsx src/features/usage-logs/lib/format.ts src/features/usage-logs/types.ts src/features/usage-logs/constants.ts tests/usage-logs-format.test.ts` | 退出码 0，通过 |
| Default 定向 ESLint | `bun x eslint src/features/usage-logs/components/columns/common-logs-columns.tsx src/features/usage-logs/components/common-logs-stats.tsx src/features/usage-logs/lib/format.ts src/features/usage-logs/types.ts src/features/usage-logs/constants.ts tests/usage-logs-format.test.ts` | 退出码 0，通过；本机 I/O 较慢，等待约 3 分钟 |
| Classic 定向格式检查 | `bun x prettier --check src/components/table/usage-logs/UsageLogsActions.jsx src/components/table/usage-logs/UsageLogsColumnDefs.jsx src/components/table/usage-logs/modals/ColumnSelectorModal.jsx src/components/dashboard/StatsCards.jsx src/components/dashboard/index.jsx src/helpers/log.js src/hooks/dashboard/useDashboardData.js src/hooks/dashboard/useDashboardStats.jsx tests/usageLogsTokenStats.test.js` | 退出码 0，通过 |
| Default 构建与兼容性检查 | `bun run build` | 退出码 0；rsbuild 完成，Safari 兼容性重写与检查通过 |
| Classic 构建与兼容性检查 | `bun run build` | 退出码 0；此前同一工作区验证通过，Safari 兼容性检查通过 |
| Default 类型检查 | `bun run typecheck` | 退出码 0，通过；本机 I/O 较慢，等待完成 |
| Go 格式检查 | `gofmt -d model/log.go controller/log.go service/text_quota.go` | 退出码 0，无差异 |
| 差异空白检查 | `git diff --check` | 退出码 0，通过 |
| 隔离容器版本与健康 | `docker ps --filter name=new-api-dev-isolated-new-api-1`、`docker exec new-api-dev-isolated-new-api-1 /new-api --build-info`、`curl --connect-timeout 5 --max-time 15 -fsS http://127.0.0.1:3001/api/healthz` | 容器 healthy；运行 dirty HEAD `f825f23...`；健康探针返回 `ok` |
| 隔离接口语义验证 | 使用临时隔离用户和日志调用统计接口 | `input_tokens=300`、`cache_read_tokens=150`、`cache_write_tokens=50`、`output_tokens=50`、`cache_rate=0.375`；临时数据已清理，确认 0 users、0 logs |

## 未覆盖边界

- 未执行正式服务验收、真实外部渠道验收或浏览器交互回归；这些不在本次隔离验证范围内。
- 工作区存在大量与本任务无关的既有 dirty/untracked 改动，本轮未整理、回退或清理。

## 工作区边界

本次只对两个 Default 文件执行了 Prettier 格式化：

- `web/default/src/features/usage-logs/components/columns/common-logs-columns.tsx`
- `web/default/src/features/usage-logs/lib/format.ts`

其余 dirty/untracked 文件均保留，未执行 reset、checkout、清理、生产升级或生产重启。
