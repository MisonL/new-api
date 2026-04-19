# Tauri 2 桌面端替换 Electron 迁移规划

状态：已完成迁移，`electron/` 目录与 `electron-build.yml` 已移除，桌面产物发布改为 `.github/workflows/tauri-release.yml`。本文件仅保留为迁移审计记录。

日期：2026-04-16
分支：`feat/desktop-tauri2-migration`
当前基线：`main`
目标替换对象：`electron/`
目标框架：`Tauri 2 + sidecar`

## 1. 文档目标

本规划用于指导 `new-api` PC 桌面端从 Electron 迁移到 `Tauri 2 + sidecar`。

本轮目标不是顺手重构前后端主链，而是在保持当前桌面端业务语义等价的前提下，替换桌面壳层与打包体系。

本规划以当前仓库代码事实、现有桌面端实现、前端运行时耦合点和发布链路为准，不以抽象设想为准。

桥接期具体验证矩阵见：

- `docs/architecture/tauri2-desktop-bridge-validation.md`

## 2. 代码事实

### 2.1 当前桌面端真实结构

当前桌面端不是独立原生客户端，而是：

1. Electron 主进程启动本地 `new-api` 二进制
2. Electron 轮询本地 `127.0.0.1:3000`
3. Electron 窗口加载本地 Web UI
4. Electron 承担托盘、退出、崩溃日志、资源路径和打包职责

对应代码位置：

- `electron/main.js`
- `electron/preload.js`
- `electron/package.json`
- `.github/workflows/electron-build.yml`

### 2.2 当前桌面端壳层承担的职责

当前 Electron 壳层已经承担以下职责：

- sidecar 二进制路径解析
- 本地 SQLite 数据目录生成
- 本地服务启动与端口可用性检查
- 主窗口创建与开发模式切换
- 关闭窗口时隐藏到托盘
- 托盘菜单与显隐切换
- 服务崩溃日志采集、错误类型分析、日志文件落盘、日志文件打开
- 退出应用时的 sidecar 终止流程

### 2.3 当前前端与桌面壳的耦合点

当前前端与 Electron 的直接耦合很小，但不能忽略：

- `window.electron?.isElectron`
- `window.electron?.dataDir`

当前已知使用位置：

- `web/src/components/setup/components/steps/DatabaseStep.jsx`

这意味着：

- 可以先抽象桌面运行时桥接层
- 不应让前端继续直接读 Electron 全局对象
- 后续 Tauri 可只补足一个等价运行时接口，而不必改写大量页面逻辑

### 2.4 当前不可轻易破坏的运行时语义

当前桌面端真实依赖以下运行时条件：

- 前端默认通过 `window.location.origin` 推导 API 地址
- 前端 OAuth 回调地址基于当前 origin 拼接
- 后端会话 Cookie 使用 `SameSiteStrict`
- 前端已经真实使用 SSE
- 当前桌面模式依赖本地 `localhost` 同源访问

结论：

- 本轮不能把桌面端改造成跨源前后端结构
- 本轮不能把桌面端改造成 Tauri command 直连后端替代 HTTP 主链
- 本轮应继续保持 `sidecar new-api -> localhost -> Web UI` 主路径

## 3. 第一性原理与总体设计

### 3.1 Primary Setpoint

在不改变当前桌面端业务主链、登录链路、OAuth 回调、SSE 语义和本地数据保留行为的前提下，以 `Tauri 2 + sidecar` 完整替换 Electron 桌面壳。

### 3.2 Acceptance

满足以下条件才视为迁移达成：

- Tauri 桌面应用可启动 `new-api` sidecar
- Tauri 桌面应用可稳定加载 `http://127.0.0.1:3000`
- 本地 SQLite 数据目录、会话登录、OAuth 回调、SSE、托盘、单实例、退出流程行为等价
- Windows 桌面构建链先行通过并产出安装包
- Electron 在桥接期内可保留为回退路径

### 3.3 Guardrails

本轮不能破坏：

- 后端 HTTP API 契约
- Web UI 主路由与页面行为
- `localhost` 同源访问语义
- 登录态与 OAuth 回调逻辑
- 本地 SQLite 数据文件格式与默认保存行为
- 当前根目录 `VERSION` 单一版本来源

### 3.4 Rollback Trigger

出现以下任一信号，默认停止切换并回退到 Electron：

- Tauri 中登录态异常丢失
- OAuth 回调在 Tauri 中失效
- SSE 请求在 Tauri 中不稳定或不可用
- 本地数据目录映射错误
- sidecar 关闭或重启流程不稳定
- 桌面包安装后无法在干净机器上启动

### 3.5 Recovery Target

- 迁移开发阶段失败时，24 小时内可回退到 Electron 桥接期基线
- 正式切流前，Electron 必须仍可重新构建并发布

## 4. CSE 控制拓扑

### 4.1 主落点与次级影响

本次迁移主落点在控制面，次级影响到状态面与数据面。

- 控制面：
  - 桌面壳主进程
  - 托盘与单实例
  - sidecar 生命周期
  - 打包、安装、发布
- 状态面：
  - 本地数据目录
  - 桌面资源路径
  - sidecar 二进制分发
- 数据面：
  - Web UI 对 localhost 的访问
  - OAuth 回调
  - SSE 与登录主链

### 4.2 复杂性转移账本

| 字段 | 内容 |
| --- | --- |
| 复杂性原位置 | Electron 主进程与 electron-builder |
| 新位置 | Tauri Rust 壳、Tauri sidecar 配置、Tauri 插件与 capability |
| 收益 | 减少 Electron 运行时负担，获得更轻量桌面壳与更标准化桌面发布能力 |
| 新成本 | 引入 Rust 工具链、sidecar 三平台资源组织、Tauri 权限配置 |
| 失效模式 | sidecar 路径错误、权限配置错误、窗口加载时序错误、平台差异导致资源丢失 |

## 5. 冻结边界

本轮默认冻结以下边界：

- 后端 API 输入输出结构
- 会话 Cookie 策略
- OAuth 浏览器回调路径语义
- Web UI 业务页面、主路由和主交互
- SQLite 数据格式
- 正式 Docker 服务与 Web 线上部署体系

本轮允许打开的边界：

- 新增 Tauri 工程与桌面目录
- 新增桌面运行时抽象层
- 新增 Tauri 桌面打包与 CI
- 调整桌面相关文档与发布说明

## 6. 分阶段开发计划

### 6.1 Phase 0：迁移 ADR 与边界确认

目标：

- 形成正式迁移规划与约束文档
- 锁定目标架构为 `Tauri 2 + sidecar + localhost`
- 明确桥接期、回退策略、切流条件

交付：

- 本文档
- 后续如有必要，再补一份更偏决策记录的 ADR

### 6.2 Phase 1：前端桌面运行时抽象层

目标：

- 把前端对 Electron 的直接访问收口到统一运行时模块
- 为 Tauri 与 Electron 并存桥接期提供同一前端接口

范围：

- 把 `window.electron` 访问收口
- 把 `isElectron` 改造成平台无关的 `isDesktopApp`
- 把 `dataDir` 改造成统一运行时属性
- 预留统一注入对象 `window.__NEW_API_DESKTOP_RUNTIME__`

运行时契约：

- 当前 Electron 桥接期继续兼容 `window.electron`
- 后续 Tauri 进入时，统一向前端注入：
  - `isDesktopApp`
  - `platform`
  - `dataDir`
- 前端业务代码不得再直接依赖 Electron 或 Tauri 专有全局对象

验收：

- 当前 Electron 桌面版行为不变
- 浏览器环境不受影响
- 前端不再直接依赖 Electron 全局对象

### 6.3 Phase 2：Tauri 工程最小骨架

目标：

- 新增 Tauri 2 工程
- 打通 sidecar 启动、端口等待、窗口加载、关闭回收

当前已落地：

- 新增 `desktop/tauri-app/`
- 新增 `scripts/prepare-sidecar.mjs`，按当前目标平台生成 sidecar 产物
- 新增 `src-tauri/` 最小 Rust 壳，可启动 sidecar、等待 `127.0.0.1:3000` 并创建主窗口
- 新增前端桌面运行时注入 `window.__NEW_API_DESKTOP_RUNTIME__`

当前最小主链：

1. Tauri 启动
2. Rust 壳准备应用数据目录
3. Rust 壳启动 `new-api` sidecar
4. 等待 `/api/status` readiness
5. 创建窗口并加载 `http://127.0.0.1:3000`

范围：

- `src-tauri` 或等价桌面目录
- sidecar 外部二进制声明
- 启动后等待 `/api/status` readiness
- 成功后加载主窗口

验收：

- 本机开发环境可跑通最小 Tauri 桌面版
- sidecar 可正常终止

### 6.4 Phase 3：行为等价补齐

目标：

- 把 Electron 壳当前承担的用户可见能力迁移到 Tauri

必须补齐：

- 托盘
- 单实例
- 关闭窗口隐藏到托盘
- 崩溃日志落盘与打开
- 数据目录展示
- 端口占用与常见错误提示
- 原生服务管理窗口

当前推进重点：

- 单实例
- 托盘菜单与显隐切换
- 关闭窗口改为隐藏
- sidecar 启动失败和异常退出的可见化提示
- sidecar 错误日志落盘与打开
- 数据目录注入前端运行时
- sidecar 启动必须设置 `NEW_API_SKIP_DOTENV=true`，禁止读取仓库根 `.env`
- sidecar 必须使用应用数据目录作为独立工作目录，不得使用仓库根目录
- sidecar 默认使用独立 SQLite（`SQL_DSN=local`、`SQLITE_PATH=<app-data>/data/new-api.db`）
- sidecar 必须禁用继承 Docker / 开发环境 / 仓库 `.env` 中的数据库、Redis、端口配置
- sidecar 二进制必须与桌面主程序位于同一输出目录，避免壳与 sidecar 版本错配
- 持久化稳定密钥注入 sidecar
- 可恢复端口冲突进入原生服务管理窗口
- 桌面工程版本号与根 `VERSION` 单一对齐

验收：

- 与 Electron 当前行为逐项对照通过

### 6.5 Phase 4：构建链与资源编排

目标：

- 建立 Tauri 桌面构建链与 sidecar 二进制编排规则

策略：

- 先只完成 Windows 构建闭环
- 再补 macOS
- 最后补 Linux

原因：

- 当前仓库已有 Electron Windows 构建链作为最近发布基线
- 先完成单平台闭环最可验证、最可逆

验收：

- GitHub Actions 可产出 Windows 桌面安装包
- 版本号继续由 `VERSION` 单一驱动

当前状态更新：

- 已新增 Tauri 桌面 GitHub Actions 工作流，目标覆盖 Windows、macOS、Linux
- 已完成本机 macOS 打包验证
- 已完成 Docker Linux 干净环境打包验证
- Linux 构建补充了 `unzip` 依赖，并确认 Web 依赖安装需要固定使用 `bun install --backend=copyfile`
- sidecar 启动门禁已从端口探测升级为 `/api/status` readiness 探测
- sidecar 启动前已新增 `127.0.0.1:3000` 预检；若本机已有服务占用该端口，会直接失败并给出明确诊断，避免桌面壳误连到现有服务
- 桌面运行时已改为在应用数据根目录持久化 `desktop-secrets.json`，稳定注入 `SESSION_SECRET` 和 `CRYPTO_SECRET`
- 已新增 Rust 集成测试，覆盖 readiness 响应判定和稳定密钥生成 / 重载
- 已新增桌面 smoke 命令，串联版本同步、Web 构建、sidecar 构建、Rust 集成测试与 Rust 编译检查
- 已新增版本同步漂移检查，防止桌面版本文件被构建脚本隐式修正而未提交
- sidecar 构建已把 Go 缓存收口到桌面工程本地 `.cache/`，减少宿主机默认缓存目录的权限与脏状态扰动
- readiness 超时诊断已增强为携带最后一次探测观测值，可区分“另一个 HTTP 服务占用 3000”与“sidecar 自身未通过 readiness”
- 已新增原生服务管理窗口，可在端口冲突时展示启动状态、当前端口、占用进程、应用数据目录，并支持修改端口后重试启动

### 6.6 Phase 5：桥接期验证

目标：

- Electron 与 Tauri 同时保留一段时间
- 用同一份后端与前端做行为对照验证

桥接期必须验证：

- 新安装
- 升级后保留数据目录
- 登录与登出
- OAuth 回调
- SSE
- 端口占用
- sidecar 异常退出
- 多实例冲突

### 6.7 Phase 6：切流与清理

目标：

- 当 Tauri 达到门禁后，切换桌面主线
- 后续单独任务再删除 Electron

约束：

- 删除 `electron/` 不与主迁移混做
- 删除 Electron 构建链必须单独验证文档、CI 与发布说明

### 6.8 自动更新前置条件

自动更新暂不直接接入，原因不是代码骨架缺失，而是发布侧前置条件尚未闭合。

接入前至少需要同时满足：

- 桌面端接入 Tauri updater 插件
- 已确定稳定的更新清单地址与产物下载地址
- 已生成并安全保管 updater 签名密钥
- CI 或发布流程可产出带签名的更新元数据与安装包
- 已验证首次从手动安装版本升级到自动更新版本的迁移路径

在上述条件未满足前，默认继续保留手动安装升级路径，不做半接入。

### 6.9 桌面端口策略

当前桥接期保持 Electron 语义，桌面 sidecar 默认使用 `127.0.0.1:3000`。

端口冲突处理原则：

- 不静默切换随机端口
- 不在端口已被占用时继续加载窗口
- 不把已有本机服务当作本桌面 sidecar
- 必须显式提示用户端口冲突

当前已实现的最小安全版本：

- 应用数据根目录持久化 `desktop-runtime.json`
- 默认端口仍为 `3000`
- 启动 sidecar 前先预检配置端口是否已被占用
- 端口配置会驱动 sidecar 监听地址、ready 探测地址和窗口加载地址
- 若端口冲突，打开原生服务管理窗口，不继续加载主窗口
- 服务管理窗口会尝试列出当前占用端口的进程 PID、名称、监听地址和命令行
- 用户修改端口后必须显式点击重试，sidecar 才会再次启动

后续如需支持桌面端与本机 Docker 正式服务长期共存，应单独设计“显式端口配置”能力：

- 端口来源优先级应为用户配置、环境变量、默认端口
- 端口配置必须持久化到桌面应用数据目录
- 修改端口后必须重新验证 OAuth 回调、Cookie、SSE 和前端 origin 推导
- 如果启用了第三方 OAuth，必须提示用户同步更新 OAuth Provider 的回调地址
- 禁止使用隐式随机端口作为默认行为

## 7. 验证矩阵

### 7.1 L0 快回路

- 前端 lint
- 前端桌面抽象层相关测试
- Tauri 配置静态检查
- sidecar 路径解析与运行时参数检查

### 7.2 L1 中回路

- 本机 Windows 开发构建
- 本地 SQLite 新建、重启、数据保留
- 登录链路
- OAuth 回调链路
- SSE 主路径

### 7.3 L2 慢回路

- GitHub Actions Windows 构建
- GitHub Actions macOS 构建
- GitHub Actions Linux 构建
- 干净机器安装验证
- Electron 老用户数据目录迁移验证
- 异常退出与端口占用故障注入

## 8. 关键风险与缓解

### 风险 1：前端仍有隐藏的 Electron 直连点

缓解：

- 先做前端桌面抽象层
- 通过搜索与静态检查收敛 `window.electron`

### 风险 2：Tauri sidecar 在不同平台的资源路径差异过大

缓解：

- Windows 先行
- 先建立统一 sidecar 产物命名与目录约定

### 风险 3：Tauri 运行时下登录、OAuth、SSE 语义与 Electron 出现差异

缓解：

- 不改变 localhost 主链
- 桥接期内保留 Electron 对照验证

### 风险 4：过早删除 Electron 导致失去回退路径

缓解：

- Electron 在桥接期内继续可构建
- 切流与删除分为两个独立任务

### 风险 5：自动更新过早接入但发布链未闭合

缓解：

- 先把 updater 当作独立阶段，而不是桌面迁移主链的顺手项
- 在签名、更新端点、产物分发、回滚路径全部明确前，不开启自动更新

## 9. 建议执行顺序

建议后续按以下顺序推进：

1. 完成本文档并锁定边界
2. 开始前端桌面运行时抽象层
3. 新建 Tauri 2 工程并打通最小 sidecar 主链
4. 迁移托盘、单实例、崩溃日志等行为等价能力
5. 建立 Windows 构建链
6. 进入桥接期对照验证
7. 达到门禁后切换桌面主线
8. 单独清理 Electron 目录与相关 CI

## 10. 当前结论

当前最稳的迁移策略不是重写前后端通信模型，而是：

- 保持 `new-api` sidecar 不变
- 保持 Web UI 通过 localhost 加载不变
- 保持登录、OAuth、SSE 和本地 SQLite 数据行为不变
- 仅替换桌面壳与桌面发布体系

因此，本项目桌面迁移路线正式确定为：

`Tauri 2 + sidecar + localhost Web UI`
