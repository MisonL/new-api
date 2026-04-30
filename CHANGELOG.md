# Changelog

本项目的变更记录维护在本文件中。

记录规则：

- 采用 `Keep a Changelog` 的分组方式整理
- 版本号遵循 `Semantic Versioning`
- `Unreleased` 记录当前 `main` 上已合入但尚未正式发布的改动
- 正式发布时，将 `Unreleased` 内容归档到对应版本

## [Unreleased]

### Added

- 增加渠道级与标签级请求头策略能力：
  - 支持多选 `User-Agent` 池
  - 支持 `轮询` / `随机` 两种运行时策略
  - 支持渠道优先、标签优先、合并和跟随系统默认的优先级模式
- 增加当前用户私有的请求头模板管理：
  - 支持保存、应用、覆盖、删除
  - 渠道编辑与标签编辑可复用同一套模板
- 增加邀请记录能力：
  - 支持按关键词、记录类型、来源筛选
  - 支持列表视图、卡片视图和图表视图
  - 图表按当前筛选条件聚合高价值被邀请用户
- 增加邀请充值返利能力：
  - 新增 `InviteRebateRate` 设置项
  - 被邀请用户充值成功后，按实际到账额度计算返利并进入邀请人可划转邀请额度
  - 覆盖 Epay 系列、Stripe、Creem、Waffo 与管理员手动完成订单路径
- 增加用户礼品码能力：
  - 支持用户从自身余额生成礼品码
  - 支持绑定用户名、用户 ID 或邮箱
  - 支持有效期、专用领取链接、领取留言和感谢回信通知

### Changed

- 统一 Release 构建产物命名规范：
  - 后端二进制命名改为 `new-api_<version-no-v>_<os>_<arch>[.exe]`
  - 桌面安装包命名改为 `new-api-desktop_<version-no-v>_<os>_<arch>.<ext>`
  - 桌面端发布新增统一命名后的校验和文件 `new-api-desktop_<version-no-v>_checksums.txt`
- README 补充 Release 下载安装说明、后端二进制运行说明、Docker 新装/升级/回滚说明
- 标签编辑中的请求头配置入口已切换为标签级运行时策略接口，不再复用旧的 bulk edit 写回渠道字段
- README、渠道配置说明、桌面端 README 已按当前代码现状统一口径，明确区分静态请求头模板、UA 运行时策略和 Header Profile 资源库
- README 和运维文档补充模型价格维护、标准倍率换算、历史日志金额修正与正式库复核口径
- 钱包/充值页邀请奖励区补充邀请记录入口、全部划转入口和礼品码入口
- 前端会话过期处理改为保留原访问路径，登录成功后返回原页面

### Fixed

- 修复运行时 `merge` 模式下显式禁用 UA 策略仍可能回落到另一侧策略的问题
- 修复运行时 `round_robin` 在乐观锁冲突下重试窗口过小、容易直接报错的问题
- 修复中文界面顶部语言切换按钮文案错误显示为 `common.changeLanguage` 的问题
- 为请求头模板和标签请求头策略补充管理日志，并在请求日志中补充最终生效的请求头策略审计字段
- 修复标签请求头策略中的非法正则透传 key 仅在运行期失败的问题，改为保存阶段直接拒绝
- 修复渠道保存时未校验 `header_policy_mode` / `ua_strategy` 非法配置的问题，避免脏配置延迟到运行期报错
- 修复请求头策略相关 UI 选中态、预览区和模板校验提示不清晰的问题
- 修正正式库 gpt-5.x、o4-mini、gpt-image、embedding 等模型价格配置，并完成可安全回算的历史消费日志金额修正
- 修复礼品码复制失败仍提示成功并清空表单的问题
- 修复礼品码专用链接在未登录时进入登录流程后丢失的问题
- 修复礼品码接收通知中用户输入内容未转义的问题
- 修复显式跳过全局错误处理的接口仍触发全局登录过期跳转的问题

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
