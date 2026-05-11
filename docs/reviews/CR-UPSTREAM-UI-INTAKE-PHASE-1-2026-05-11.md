# CR-UPSTREAM-UI-INTAKE-PHASE-1-2026-05-11

## Scope

第一阶段只吸纳低耦合 UI 改进：

- `SectionPageLayout.Description` 保留组合槽兼容，但不在页面头部渲染。
- sidebar 顶部展示层从 workspace switcher 命名收口为 `SystemBrand`。
- 保留现有 workspace registry、系统设置路由切换、dashboard drilldown 数据语义、Header Profile 和 pricing 口径。

本轮没有修改 Header Profile、pricing、dashboard 数据处理逻辑或后端 API。

## Verification

采样时间：2026-05-11

| Command | Exit | Result |
| --- | --- | --- |
| `node --test web/tests/defaultLayoutContracts.test.mjs` | 0 | `SectionPageLayout` description 隐藏合约和 `SystemBrand` 路由切换合约通过 |
| `cd web/default && bun run lint` | 0 | ESLint 通过 |
| `cd web/default && bun run build` | 0 | Rsbuild 输出 `ready built in 2 m 24.3 s` |
| `scripts/build-docker-local.sh new-api-local:dev` | 0 | 构建镜像 `new-api-local:dev`；最终运行提交以收尾审计中的 `git rev-parse HEAD` 与容器 `--build-info` 比对为准 |
| `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` | 0 | `new-api-dev-isolated-new-api-1` 已用最新镜像重建并启动 |
| `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` | 0 | `commit` 与收尾审计时的 `git rev-parse HEAD` 一致 |
| `curl -fsS --retry 5 --retry-delay 2 --retry-all-errors http://127.0.0.1:3001/api/status` | 0 | 返回 `success:true`，`theme:"classic"`，`version:"v1.1.0"` |
| `cd web/default && VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001 bun run dev --host 127.0.0.1 --port 5176` | running | `http://127.0.0.1:5176/` 可访问 |

## Browser Evidence

- `http://127.0.0.1:5176/` 渲染 default 首页，标题为 `New API`。
- `http://127.0.0.1:5176/dashboard` 在未登录状态下跳转到 `sign-in?redirect=%2Fdashboard`，登录页正常渲染。
- 由于未使用真实登录态，本轮不伪造 sidebar 登录后视觉截图；sidebar 行为由静态契约测试覆盖导出名、渲染入口和路由目标。

## Notes

- `3001` 隔离容器当前系统配置返回 `theme:"classic"`，不能用它单独代表 `web/default` 页面验证。
- `web/default` 已通过独立 dev server 代理到 `3001` 进行运行态打开验证。
- 图表 hover 黑描边防回归由既有 `web/tests/dashboardChartHoverStyle.test.mjs` 覆盖；当前第一阶段没有继续改写 dashboard 数据语义。

## Follow-up Token Intake

采样时间：2026-05-11

继续补齐 `a7475a1e6` 中可安全吸纳的 token / chart preset 部分：

- 在 `web/default/src/styles/theme.css` 中补充 `success`、`warning`、`info`、`neutral` 语义状态 token 及 Tailwind `@theme inline` 映射。
- 在现有单文件主题结构中加入兼容的 `[data-theme-preset]` surface bridge。当前仓库没有独立 `theme-presets.css` 或 `theme-customization-provider`，因此不原样搬上游运行时 preset provider。
- Dashboard 图表颜色优先读取 CSS `--chart-1` 到 `--chart-5`，无 DOM 环境时回退 VChart 默认色板。
- `processChartData` 与 `processUserChartData` 的新增 `themeKey` 参数保持可选，不改变已有数据聚合、drilldown、日志筛选或 pricing 口径。

补充验证：

| Command | Exit | Result |
| --- | --- | --- |
| `node --test web/tests/defaultThemeTokenContracts.test.mjs` | 0 | 语义 token、preset bridge、dashboard CSS chart token 合约通过 |
| `node --test web/tests/defaultThemeTokenContracts.test.mjs web/tests/dashboardChartHoverStyle.test.mjs web/tests/defaultDashboardDrilldown.test.mjs web/tests/defaultLayoutContracts.test.mjs` | 0 | 26 项通过，dashboard drilldown 语义保持 |
| `cd web/default && bun run lint` | 0 | ESLint 通过 |
| `cd web/default && bun run build` | 0 | Rsbuild 输出 `ready built in 2 m 36.1 s` |
