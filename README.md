# new-api

基于 `QuantumNous/new-api` 的独立演进版本。

本仓库继续保留 `new-api` 的基础定位，但未来路线、功能取舍、发布节奏和上游吸纳策略由本仓库单独决定，不再以“百分百跟随上游”为目标。

## 变更记录

- 标准变更记录文件：[CHANGELOG.md](/Volumes/Work/code/new-api/CHANGELOG.md)
- 维护方式：`Unreleased` 持续记录当前 `main` 上尚未发布的改动；正式发布时归档到对应版本
- 当前稳定版本：`v1.1.0`
- 当前独立版本起点：`v1.0.0`

## 本仓库独有的新增/改动

以下条目仅列出当前相对 `upstream/main` 仍由本仓库独立维护、且可在本仓库提交历史中追溯到的功能与改动，不包含单纯同步上游后已在上游存在的能力。

- 企业 SSO 三条链路：`JWT Direct`、`Trusted Header`、`CAS`
- `OpenAI Chat` 与 `OpenAI Responses` 协议转换策略可视化配置
- 阶梯计费表达式与工具定价能力
- 请求/响应内容日志：用户授权开启、弹窗查看、JSON 导出、单条删除、批量删除
- `Responses` 流式首包前恢复等待与相关稳定性增强
- 用户绑定信息管理面板与用户属性入口增强
- Dashboard 增强：时间范围切换、通道趋势排行
- 使用日志与后台表格增强：筛选联想、横向滚动、详情显示稳定性改进
- Web UI 体验增强：可拖拽侧边栏、表单可访问性修补、亮暗主题细节修正
- Codex / 长连接异常判定与通道恢复策略优化
- 若干稳定性修复：分发缓存反同步、负延迟保护等

以上清单以当前 `main` 已合入能力为准，后续会继续按本仓库路线演进。

## 项目治理

- 本仓库独立开发，方向由维护者决定。
- 上游 `QuantumNous/new-api` 是可选输入，不是唯一产品路线来源。
- 上游新变更按需选择性吸纳，不承诺全量同步。
- 吸纳上游前，先评估对本仓库现有增强功能、配置兼容性、Web UI 和后端行为的影响。
- 吸纳上游后，默认需要在独立环境完成构建、回归和关键链路验证。

## 快速开始

```bash
git clone https://github.com/MisonL/new-api.git
cd new-api
docker compose up -d
```

默认访问地址：

```text
http://localhost:3000
```

如果你需要持久化数据或保留现有配置，请在启动前先检查并挂载数据目录、数据库连接和环境变量，不要直接覆盖生产实例。

如需显式指定宿主机绑定目录，可设置：

```bash
NEW_API_DATA_DIR=./data
NEW_API_LOG_DIR=./logs
```

跨平台建议：

- macOS：使用绝对路径，例如 `/Volumes/Work/code/new-api/data`
- Linux：可使用相对路径，或使用绝对路径如 `/srv/new-api/data`
- Windows：建议使用 Docker Compose 可识别的绝对路径
- WSL：建议使用 Linux 路径，如 `/home/<user>/new-api/data` 或 `/mnt/d/...`

## 开发环境矩阵

项目现在明确拆成两类开发环境，不再混用单个 `port3001` 样例文件：

1. 完全隔离开发环境
   - 编排文件：[deploy/compose/dev-isolated.yml](/Volumes/Work/code/new-api/deploy/compose/dev-isolated.yml)
   - 环境模板：[deploy/env/dev-isolated.env.example](/Volumes/Work/code/new-api/deploy/env/dev-isolated.env.example)
   - 目标：独立 `new-api`、独立 PostgreSQL、独立 Redis、独立数据目录、独立日志目录、独立端口
   - 适用：日常主开发、联调、迁移验证、功能测试

2. 只读前端联调环境
   - 代理编排：[deploy/compose/frontend-readonly-proxy.yml](/Volumes/Work/code/new-api/deploy/compose/frontend-readonly-proxy.yml)
   - 代理模板：[deploy/nginx/frontend-readonly.conf.template](/Volumes/Work/code/new-api/deploy/nginx/frontend-readonly.conf.template)
   - 环境模板：[deploy/env/frontend-readonly.env.example](/Volumes/Work/code/new-api/deploy/env/frontend-readonly.env.example)
   - 目标：复用正式后端，仅替换本地前端，禁止登录、登出、绑定和所有写入请求
   - 适用：样式调试、只读观察、正式数据界面联调

### 完全隔离开发环境启动

```bash
cp deploy/env/dev-isolated.env.example deploy/env/dev-isolated.env
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d
```

### 只读前端联调环境启动

先启动只读代理：

```bash
cp deploy/env/frontend-readonly.env.example deploy/env/frontend-readonly.env
docker compose -f deploy/compose/frontend-readonly-proxy.yml --env-file deploy/env/frontend-readonly.env up -d
```

再启动本地前端开发服务器：

```bash
cd web
VITE_DEV_PROXY_TARGET=http://127.0.0.1:3300 \
VITE_REACT_APP_READONLY_MODE=true \
bun run dev --host 0.0.0.0 --port 5173
```

只读联调环境的控制原则：

- 前端 UI 会显示只读提示
- 前端请求层会阻断写方法和高风险登录绑定入口
- 只读代理会再次阻断非安全方法和关键副作用路径
- 如需登录，请先在正式 Web UI 完成登录，再打开只读前端；不要在只读前端里走登录流程

## 开发命令

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service
cd web && bun install
cd web && bun run lint
cd web && bun run build
```

## CI/CD

- 版本单一来源：根目录 `VERSION`
- `release.yml`、`tauri-release.yml`、Docker 构建都会读取 `VERSION`
- 发布流程仅支持手动触发 `workflow_dispatch`，不再由 `push tag` 自动触发
- 正式发布仅允许稳定 semver tag（`vMAJOR.MINOR.PATCH`），并校验输入 tag 与 `VERSION` 完全一致
- 桌面客户端产物仅由 Tauri 2 工作流发布，Electron 产物流程已移除
- Docker 镜像默认发布到 `ghcr.io/misonl/new-api`
- alpha Docker 镜像流程固定使用 `alpha` 分支头提交作为构建来源
- 如需同时发布到 Docker Hub，需要配置仓库 secrets：
  - `DOCKERHUB_USERNAME`
  - `DOCKERHUB_TOKEN`
- 项目发布渠道仅保留 GitHub Release、GitHub Actions 与 GitHub Container Registry

## 与上游的关系

- 上游项目：`QuantumNous/new-api`
- 本仓库会继续吸纳对基础能力有价值、且不会破坏现有增强功能的上游变更
- 不符合本仓库路线或会明显影响既有能力可用性的上游改动，可以跳过

## 说明

- 项目名称、模块路径和上游归属信息继续保留
- 生产验证前，请先在独立环境完成配置迁移和联调
