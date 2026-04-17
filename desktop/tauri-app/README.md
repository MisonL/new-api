# Tauri Desktop Workspace

本目录承载 `new-api` 的 Tauri 2 桌面端工程。

当前阶段目标：

- 用 Tauri 2 替换 Electron 壳层
- 保持 `new-api` 作为本地 sidecar 主链
- 继续通过 `http://127.0.0.1:3000` 加载现有 Web UI

## 开发命令

```bash
cd desktop/tauri-app
bun install
bun run dev
```

## 构建命令

```bash
cd desktop/tauri-app
bun install
bun run build
```

## 验证命令

```bash
cd desktop/tauri-app
bun run check:version
bun run test:rust
bun run check:rust
bun run smoke
```

Linux 干净环境需要先安装 Tauri 2 的系统依赖。Debian / Ubuntu 至少需要：

```bash
sudo apt install -y \
  build-essential \
  curl \
  file \
  libayatana-appindicator3-dev \
  librsvg2-dev \
  libssl-dev \
  libwebkit2gtk-4.1-dev \
  libxdo-dev \
  unzip \
  wget
```

说明：

- `unzip` 是 Bun 官方安装脚本在 Linux 上的前置依赖
- 桌面构建脚本使用 `bun install --backend=copyfile --frozen-lockfile` 安装 Web 依赖，避免 Docker bind mount、WSL、macOS 共享目录等环境下 hardlink / clonefile 语义不稳定导致安装失败，同时防止构建时改写锁文件
- GitHub Actions 当前固定使用 `Bun 1.3.12`

## 当前约束

- 桌面工程版本号由仓库根 `VERSION` 单一驱动，`package.json`、`tauri.conf.json`、`Cargo.toml` 会在预处理阶段自动同步
- `bun run dev` 和 `bun run build` 会先构建 `web/dist`，再编译 sidecar，最后运行 Tauri CLI
- `bun run check:version` 会检查 `package.json`、`tauri.conf.json`、`Cargo.toml` 是否与仓库根 `VERSION` 保持一致
- `bun run smoke` 会依次执行版本同步、Web 构建、sidecar 构建、Rust 集成测试和 Rust 编译检查
- sidecar 由 `scripts/prepare-sidecar.mjs` 先编译到 `src-tauri/binaries/`
- 如果本地已经存在 `target/debug/new-api-tauri-desktop` 或 `target/release/new-api-tauri-desktop`，`scripts/prepare-sidecar.mjs` 会同步更新同目录下的 `new-api` sidecar，避免直接运行旧 sidecar
- sidecar 构建会把 `GOCACHE` 和 `GOMODCACHE` 收口到 `desktop/tauri-app/.cache/`，避免污染宿主机默认 Go 缓存目录并减少权限噪声
- 桌面运行时会在应用数据根目录持久化生成 `desktop-secrets.json`，用于稳定注入 `SESSION_SECRET` 和 `CRYPTO_SECRET`
- 桌面运行时会在应用数据根目录持久化生成 `desktop-runtime.json`，用于保存本地监听端口；默认端口仍为 `3000`
- 桌面本地端口来源优先级为：`desktop-runtime.json` 配置、环境变量 `NEW_API_DESKTOP_PORT`、默认值 `3000`
- 桌面运行时会设置独立工作目录为应用数据目录，并注入 `NEW_API_SKIP_DOTENV=true`、`SQL_DSN=local`、`SQLITE_PATH`、空 `LOG_SQL_DSN`、空 `REDIS_CONN_STRING`，避免继承仓库根 `.env` 或正式环境数据库配置
- 桌面运行时启动 sidecar 前会预检当前配置端口是否已被占用；如已占用会直接报错，不会静默连接到已有服务或自动切换随机端口
- sidecar 启动后会等待 `/api/status` readiness 成功，再创建主窗口，避免端口已打开但服务尚未就绪时过早加载页面
- 如果启动时检测到本地端口已被占用，桌面端会打开原生“Service Management”窗口，列出占用进程，允许修改本地端口后重试启动；不会静默切换到其他端口
- 当前已实现：
  - sidecar 启动
  - `/api/status` readiness 等待
  - 主窗口创建与运行时注入
  - 数据目录注入前端运行时
  - 持久化稳定密钥注入 sidecar
  - 持久化本地端口配置注入 sidecar
  - 单实例聚焦已存在窗口
  - 托盘菜单与左键显隐切换
  - 托盘打开数据目录与日志目录
  - 原生服务管理窗口，支持查看启动状态、端口配置、占用进程、修改端口和重试启动
  - 关闭窗口改为隐藏
  - sidecar 启动前端口冲突预检，避免误连 Docker 或其他本机服务
  - 启动失败和 sidecar 异常退出弹窗
  - readiness 超时会记录最后一次探测观测值，并区分“端口被其他 HTTP 服务占用”与“sidecar 自身未就绪”
  - sidecar 错误日志缓冲、落盘与系统默认程序打开
  - GitHub Actions 多平台桌面构建链
  - 本机 macOS 构建验证
  - Docker Linux 构建验证
- 当前仍未补齐：
  - 自动更新
    - 当前未接入 updater，需先补齐签名密钥、更新清单地址、带签名产物发布链和首次升级路径
