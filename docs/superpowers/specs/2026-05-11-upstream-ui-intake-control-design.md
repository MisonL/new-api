# Upstream UI Intake Control Design

日期：2026-05-11

## 1. 背景

上游近期存在一组与 `web/default`、Base UI、主题 token、Dashboard、Pricing 和系统设置相关的 UI 提交。这些提交不能作为普通 cherry-pick 直接吸纳，因为本仓库已经形成了自己的前端边界：

- `web/classic` 与 `web/default` 并存，线上 `3000` 和隔离开发 `3001` 可能加载不同主题入口。
- Header Profile、渠道请求头透传、Dashboard drilldown、pricing 维护和桌面端运行态都有本仓库定制语义。
- `3001` 容器健康不等于 `web/default` 页面已被验证；`web/default` 需要单独 dev server 代理到隔离后端验证。

本设计目标是定义“如何吸纳上游 UI 思路”，不是定义一次性合并哪些提交。

## 2. Control Contract

### Primary Setpoint

在不破坏本仓库现有业务语义和运行态门禁的前提下，分阶段吸收上游 UI 改进中可复用的视觉 token、局部组件模式和信息层级。

### Acceptance

每个吸纳任务必须满足：

- 变更范围可解释为一个原子任务。
- 明确说明来自上游的内容是“直接移植”、“重写吸收”还是“仅参考”。
- `web/default` 相关改动至少通过 `bun run lint` 和 `bun run build`。
- 涉及 `web/classic` 的改动至少通过 `cd web/classic && bun run test:node` 和 `bun run build`。
- 涉及运行态 UI 的改动必须做浏览器验证，并区分 `3001` classic 入口与 `web/default` dev server。

### Guardrails

- 不改写 Header Profile、渠道请求头策略、Codex 透传语义。
- 不把 dashboard 数据语义、drilldown、日志筛选和图表点击路径当作纯视觉代码覆盖。
- 不把 pricing UI 改动和 quota、倍率、账单语义混在同一个任务里。
- 不引入 Base UI 作为横切迁移，除非先完成组件边界设计和回归清单。
- 不以“上游统一 UI”为理由删除本仓库 desktop、classic 或 admin 定制入口。

### Sampling Plan

- 设计阶段采样：对比上游提交意图、本仓库现有页面、相关审计文档。
- 实现阶段采样：每个原子任务跑最小测试、构建、浏览器截图。
- 运行阶段采样：对改动页面分别检查桌面宽度、窄宽度、亮色主题、暗色主题；若 `3001` 当前加载 classic，需要另启 `web/default` dev server。

### Known Delays

- Docker rebuild 与 `3001` 容器重启存在分钟级延迟。
- `web/default` 和 `web/classic` 构建较慢。
- 部分页面依赖线上或隔离库中的真实数据，空态和有数据态需要分别验证。

### Recovery Target

若某个吸纳任务造成核心页面不可用或渠道配置语义受损，应在 15 分钟内回滚该原子任务或恢复前一版配置。

### Rollback Trigger

- `web/default` 或 `web/classic` 构建失败且无法在当前原子任务内解释。
- 运行态出现未授权写入、渠道配置异常、dashboard drilldown 错误或 pricing 口径漂移。
- 视觉改动导致主要操作入口不可见、不可点击或移动端明显溢出。

### Boundary

本设计只允许评估和规划 UI 吸纳策略，不直接修改业务代码或上游同步代码。

## 3. 分层吸纳策略

### A. 可优先吸纳

主题 token、图表 preset、颜色语义、轻量品牌文案。

代表提交：

- `a7475a1e6 fix(web): align UI and charts with theme tokens and presets`
- `415d21d07 refactor(layout): rename workspace switcher to system brand`

吸纳方式：

- 优先重写为本仓库 token 和常量。
- 同时覆盖 `web/default` 与必要的 `web/classic` 图表状态。
- 不改变数据结构、不改路由、不改权限。

### B. 重写后吸纳

系统设置 pricing UI、Dashboard overview、Select 组件 API。

代表提交：

- `0f9f094a feat(default): reorganize system settings pricing UI`
- `a7d019e3a feat(default): redesign dashboard overview`
- `d98f0e8ac fix: migrate select to Base UI items API`

吸纳方式：

- 先写本仓库页面级设计，再实现。
- Dashboard 必须保留 drilldown、日志筛选、模型折叠和管理员筛选语义。
- Pricing 必须先读 `docs/operations/pricing-maintenance.md`，把展示改动和计费语义改动拆成两个任务。
- Select 只能在局部组件验证后推进，不做全局替换。

### C. 仅参考，不直接吸纳

大范围 UI overhaul。

代表提交：

- `8b2b03d27 feat(web/default): unified UI overhaul`

处理方式：

- 只提取设计原则、布局节奏、token 命名和组件边界。
- 不直接 cherry-pick。
- 若确需推进，必须拆成多个阶段：token、基础组件、页面、运行态验证。

## 4. 推荐推进顺序

1. 小修类可验证任务：吸纳 token、图表状态、品牌命名等低耦合改动。
2. 继续拆可验证小修：只处理单一组件或单一页面局部，不跨 dashboard/pricing/header profile。
3. 文档与门禁：把验证口径补到 `docs/reviews/`，避免后续误把 `3001` classic 当作 `web/default`。
4. 重新评估 `perf metrics` 与 `rankings` 是否作为功能专项推进，不绑定 UI 迁移。

## 5. 复杂性转移账本

| 项目 | 原位置 | 新位置 | 收益 | 新成本 | 失效模式 |
| --- | --- | --- | --- | --- | --- |
| 主题 token 吸纳 | 页面内硬编码颜色 | 共享 token / 常量 | 视觉一致、易回归 | 需要跨主题验证 | 暗色主题对比不足 |
| Dashboard overview 借鉴 | 单页面视觉结构 | Dashboard 页面架构 | 信息层级更清晰 | 数据语义耦合更强 | drilldown 或日志入口丢失 |
| Pricing UI 重组 | 系统设置页面 | pricing 专项设计 | 配置更易读 | 牵涉账单口径 | 展示口径和计费口径混淆 |
| Base UI Select 借鉴 | 组件局部实现 | 基础组件层 | 交互一致性提升 | 横向回归成本高 | 表单、弹窗、筛选器行为回退 |

## 6. Gate

开始任何上游 UI 吸纳实现前，必须先为该原子任务写明：

- 吸纳类型：直接移植 / 重写吸收 / 仅参考。
- 影响面：`web/default`、`web/classic`、desktop、backend API、数据库、运行配置。
- 最小验证命令。
- 浏览器验证入口。
- 回滚方式。

若无法写清以上内容，该上游提交暂不吸纳。
