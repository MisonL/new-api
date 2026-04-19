# Changelog

本项目的变更记录维护在本文件中。

记录规则：

- 采用 `Keep a Changelog` 的分组方式整理
- 版本号遵循 `Semantic Versioning`
- `Unreleased` 记录当前 `main` 上已合入但尚未正式发布的改动
- 正式发布时，将 `Unreleased` 内容归档到对应版本

## [Unreleased]

## [1.1.0] - 2026-04-19

### Added

- 增加完全隔离开发环境编排与模板：
  - `deploy/compose/dev-isolated.yml`
  - `deploy/env/dev-isolated.env.example`
- 增加只读前端联调环境编排、代理模板与模板环境变量：
  - `deploy/compose/frontend-readonly-proxy.yml`
  - `deploy/nginx/frontend-readonly.conf.template`
  - `deploy/env/frontend-readonly.env.example`
- 增加前端只读模式运行时常量与请求拦截逻辑，阻断写请求和高风险入口
- 增加阶梯计费表达式、工具定价配置保存前的后端校验与对应回归测试
- 增加使用日志页面的 Web UI 主题风格约束并写入 `AGENTS.md`

### Changed

- 开发环境矩阵从单一 `docker-compose-port3001.yml` 样例改为“完全隔离开发环境 + 只读前端联调环境”双模式
- Vite 开发代理支持通过 `VITE_DEV_PROXY_TARGET` 显式指定目标地址，便于只读联调环境接入
- `.env.example`、`README.md`、`.gitignore` 已同步更新到新的开发环境与本地文件管理规则
- GitHub-only 发布与文档策略已继续收敛，项目说明保持中文单入口
- 桌面端发布链路从 Electron 切换为 Tauri 2，移除 Electron 代码目录和对应产物流程
- Release CI/CD 触发机制统一调整为手动 `workflow_dispatch`，不再通过推送 tag 或分支自动发布
- 正式发布流程新增稳定版本门禁（仅允许 `vMAJOR.MINOR.PATCH`）并固定 alpha 镜像来源为 `alpha` 分支头提交

### Fixed

- 修复 Docker Compose 绑定目录在 macOS `/Volumes`、Windows、Linux、WSL 场景下的跨平台兼容问题
- 修复使用日志展开详情区与系统既有主题风格不一致的问题
- 修复使用日志展开区删除按钮在未悬停时轮廓不明显的问题

## [1.0.0] - 2026-04-16

### Added

- 发布本仓库首个独立版本 `v1.0.0`
- 建立相对上游 `QuantumNous/new-api` 的独立治理与独立发布起点
- 纳入并确认以下由本仓库独立维护的增强能力：
  - 企业 SSO 三条链路：`JWT Direct`、`Trusted Header`、`CAS`
  - `OpenAI Chat` 与 `OpenAI Responses` 协议转换策略可视化配置
  - 阶梯计费表达式与工具定价能力
  - 请求/响应内容日志：用户授权开启、弹窗查看、JSON 导出、单条删除、批量删除
  - `Responses` 流式首包前恢复等待与相关稳定性增强
  - 渠道上游模型更新检测、自动同步与忽略列表
  - 用户绑定信息管理面板与用户属性入口增强
  - Dashboard 增强：时间范围切换、通道趋势排行
  - 使用日志与后台表格增强：筛选联想、横向滚动、详情显示稳定性改进
  - Web UI 体验增强：可拖拽侧边栏、表单可访问性修补、亮暗主题细节修正
  - Codex / 长连接异常判定与通道恢复策略优化

### Changed

- 重写仓库 README，明确本仓库独立路线、功能差异和与上游的关系
- 仓库治理从“默认跟随上游”切换为“按需选择性吸纳上游”
- 版本单一来源重置为根目录 `VERSION`，并以 `v1.0.0` 作为独立发布起点
- 项目文档入口收敛为中文单入口

### Fixed

- 解耦 `Responses` 首包恢复逻辑与共享渠道 DTO，降低通道适配耦合风险
- 收敛若干已在本仓库持续维护的稳定性修复，包括分发缓存反同步、负延迟保护等

[Unreleased]: https://github.com/MisonL/new-api/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/MisonL/new-api/tree/v1.1.0
[1.0.0]: https://github.com/MisonL/new-api/tree/v1.0.0
