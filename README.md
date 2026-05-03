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
- 模型价格维护和历史消费金额修正流程：区分标准倍率、阶梯表达式、按次价格与历史日志回算
- 邀请奖励增强：邀请记录筛选、列表/卡片/图表视图、邀请充值返利、全部划转、礼品码专用链接
- 请求/响应内容日志：用户授权开启、弹窗查看、JSON 导出、单条删除、批量删除
- 渠道请求头模板策略：完整 `Header Profile`、轮询/随机选择、AI CLI 动态请求头透传提示；标签级请求头策略保留历史兼容能力
- `Responses` 流式首包前恢复等待与相关稳定性增强
- 用户绑定信息管理面板与用户属性入口增强
- 登录会话过期体验增强：保留当前访问路径，过期后跳转登录，登录成功后返回原页面
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

## 协作与审查

- 本仓库不面向上游仓库提交 PR。
- 本仓库语境中的 PR，默认仅指向 `MisonL/new-api` 自己的 GitHub 仓库。
- 需要 PR 审查时，默认先提交到本仓库分支，再向本仓库 `main` 发起 PR。
- 仓库已支持 CodeRabbit 参与 PR review，相关配置文件位于仓库根目录 `.coderabbit.yaml`。
- AI 可以参与开发、整理说明和生成补丁，但提交人必须核对结果、理解影响，并在出现问题时继续跟进修复。

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

## Release 下载与安装

Release 页面：

- https://github.com/MisonL/new-api/releases

当前构建产物命名规则：

- 后端服务：`new-api_<version-no-v>_<os>_<arch>[.exe]`
- 桌面客户端：`new-api-desktop_<version-no-v>_<os>_<arch>.<ext>`
- 校验文件：`new-api_<version-no-v>_checksums_<os>.txt`、`new-api-desktop_<version-no-v>_checksums.txt`

### 后端服务（二进制）安装与启动

最低要求：

- 必须设置稳定密钥：`SESSION_SECRET`、`CRYPTO_SECRET`
- 默认端口：`3000`
- 未设置 `SQL_DSN` 时默认使用 SQLite（`one-api.db`）

Linux / macOS 示例：

```bash
chmod +x ./new-api_1.1.0_linux_amd64
export SESSION_SECRET='replace-with-stable-secret'
export CRYPTO_SECRET='replace-with-stable-secret'
./new-api_1.1.0_linux_amd64 --port 3000 --log-dir ./logs
```

Windows PowerShell 示例：

```powershell
$env:SESSION_SECRET="replace-with-stable-secret"
$env:CRYPTO_SECRET="replace-with-stable-secret"
.\new-api_1.1.0_windows_amd64.exe --port 3000 --log-dir .\logs
```

可选参数：

- `--port <端口>`
- `--log-dir <日志目录>`
- `--version`
- `--help`

### 桌面客户端安装包选择

- macOS：`*.dmg` 或 `*.app.tar.gz`
- Linux：`*.AppImage` / `*.deb` / `*.rpm`（三选一）
- Windows：`*-setup.exe` 或 `*.msi`

桌面客户端是 Tauri 2 包装形态，内置 sidecar 启动后端服务能力，适合单机使用。

## Docker 正式部署（保配置）

### 新装

1. 准备配置文件：

```bash
cp .env.example .env
```

2. 在 `.env` 中至少设置：

- `NEW_API_IMAGE`（例如 `ghcr.io/misonl/new-api:v1.1.0`）
- `SESSION_SECRET`
- `CRYPTO_SECRET`
- 如需外部数据库/缓存：`SQL_DSN`、`REDIS_CONN_STRING`
- 建议显式设置：`NEW_API_DATA_DIR`、`NEW_API_LOG_DIR`

3. 启动服务：

```bash
docker compose pull
docker compose up -d
```

4. 验证：

```bash
docker compose ps
curl -fsS http://127.0.0.1:3000/api/status
```

### 本地 Docker 构建

本地正式镜像建议使用脚本构建，脚本会把版本、Git commit、构建时间和源码地址写入镜像标签与二进制构建信息：

```bash
scripts/build-docker-local.sh new-api-local:prod-main
```

构建后可核对：

```bash
docker image inspect new-api-local:prod-main
docker run --rm new-api-local:prod-main --build-info
```

如果工作区存在未提交改动，脚本会在 commit 后追加 `-dirty`，避免把未提交内容伪装成干净提交。

### 升级（不丢配置）

1. 备份当前配置与数据（至少包括 `.env`、数据目录、数据库卷）。
2. 仅更新 `.env` 里的 `NEW_API_IMAGE` 到新版本标签。
3. 执行：

```bash
docker compose pull
docker compose up -d
```

4. 验证健康状态与关键功能后再清理旧镜像：

```bash
docker image prune -f
```

### 回滚

1. 将 `.env` 的 `NEW_API_IMAGE` 改回旧版本标签。
2. 执行：

```bash
docker compose pull
docker compose up -d
```

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

## 请求头策略能力

当前仓库里与请求头相关的能力分为三层，文档口径以代码现状为准：

1. 静态请求头覆盖

   - 渠道字段 `header_override` 保存静态请求头 JSON。
   - 标签编辑中的“请求头覆盖”现在写入独立的标签级运行时策略，不再批量回写所有渠道。
   - 合法值要求：
     - 必须是 JSON 对象。
     - value 只支持字符串、数字、布尔值。
     - key 支持标准请求头名，以及透传规则 `*`、`re:<pattern>`、`regex:<pattern>`。
     - 非法 JSON、非法 key、非法正则会在保存阶段直接拒绝。

2. Header Profile 资源库

   - 用户设置中保存 `header_profiles` 资产，渠道 `settings.header_profile_strategy` 保存所选 Profile 引用。
   - 预置浏览器、AI Coding CLI、API SDK / Debug Profile 为只读资产，用户可新增、编辑、删除自己的 Profile。
   - 当前内置 AI Coding CLI Profile 包括 `Codex CLI`、`Claude Code`、`Gemini CLI`、`Qwen Code`、`Droid CLI`；`OpenCode` 不作为内置 Profile 提供。
   - 渠道侧支持 `fixed` / `round_robin` / `random` 三种 Profile 选择模式，保存时会做服务端校验。
   - 渠道保存时会写入已选 Profile 的运行时快照，保证该渠道后续由任意用户请求时都能按同一组完整请求头生效。
   - 真实转发链路会在旧 `header_override` 之前应用所选 Profile；如两者设置同名请求头，旧 `header_override` 仍作为显式覆盖值优先生效。
   - AI Coding CLI 预置 Profile 只代表固定请求头快照；`Codex CLI` 固定快照使用交互式 TUI 身份 `codex-tui`，不能复用 `codex exec` 的 non-interactive 身份 `codex_exec`。
   - 如果上游要求官方客户端身份、会话连续性或 SDK 元数据，还必须在参数覆盖里启用对应的 `pass_headers` 透传模板，让真实客户端动态请求头进入上游。

3. 历史 UA 运行时策略
   - 后端仍兼容渠道 `settings.header_policy_mode`、`settings.override_header_user_agent` 和 `settings.ua_strategy`。
   - 新渠道 UI 不再提供独立 UA 池入口；如需多组 User-Agent 轮询或随机，使用多个完整 Header Profile。
   - 保存阶段仍会校验历史字段，避免非法历史配置延迟到运行期失败。

补充说明：

- 旧版 `header_override` 只用于兼容查看、清空和显式导入，不再作为新配置的主路径。
- `Header Profile` 与“请求头模板”不是同一回事：
  - 旧版模板：面向 `header_override` 编辑区的 JSON 兼容入口。
  - Profile：面向用户级完整请求头资产管理与渠道选择。
- `Codex CLI`、`Claude Code` 等 Profile 不能伪造官方客户端动态头；遇到 `only allows official clients`、会话追踪或 SDK 元数据校验类错误时，应同时配置高级参数覆盖中的 CLI 真实请求头透传模板。
- 渠道模型测试窗口默认使用 `/v1/responses`，并会按开关应用请求头、参数覆盖、代理和模型映射。详细说明见 [渠道模型测试运行配置说明](docs/channel/model_test_runtime_config.md)。

## 邀请返利与礼品码

当前仓库在原有邀请码和兑换码能力之外，补充了邀请收益追踪与用户间礼品码能力：

- 邀请记录：用户可在钱包/充值页查看被邀请用户、注册奖励、充值返利、来源、返利比例与收益金额，并支持关键词、类型、来源筛选。
- 高价值用户识别：邀请记录支持列表、卡片和图表视图，图表按当前筛选条件聚合被邀请用户收益排行。
- 邀请充值返利：管理员在额度设置中配置 `InviteRebateRate`，被邀请用户充值成功后，按实际到账额度计算返利并进入邀请人的可划转邀请额度。
- 全部划转：邀请额度划转窗口支持一次填入当前全部可划转额度。
- 礼品码：用户可从自身余额生成专用礼品码，可绑定接收人用户名、用户 ID 或邮箱，也可设置有效期和留言；接收人登录后通过专用链接领取，并可填写感谢回信。
- 礼品码与兑换码不同：兑换码由后台生成后供用户兑换；礼品码由普通用户从自己的余额生成，领取成功后额度从生成人转给接收人。

详细说明见 [邀请返利与礼品码说明](docs/operations/invitation-rebate-gift-code.md)。

## 登录过期与返回原页面

前端会在进入受保护页面前记录当前路径、查询参数和 hash。若会话过期或接口返回未授权，页面会跳转到 `/login?expired=true`；用户重新登录后回到原访问页面。对 OAuth、绑定回调、会话探测等显式设置 `skipErrorHandler` 的请求，调用方自行处理失败，不触发全局过期跳转。

该机制用于避免页面长时间未操作后再次点击时停留在无反馈状态，同时保证礼品码专用链接这类带查询参数的入口在登录后仍能继续处理。

## 计费与价格维护

- 标准 token 价格仍使用历史倍率口径：`ModelRatio = input_usd_per_1M / 2`，输出与缓存通过 `CompletionRatio`、`CacheRatio`、`CreateCacheRatio` 表达。
- 阶梯计费表达式使用真实美元每百万 token 价格，不做 `/2` 换算。
- 价格异常后的历史日志修正不能只改 `logs`，必须同步 `users`、`tokens`、`channels`、`quota_data` 和 Redis 缓存。
- 详细操作规程见 [模型价格维护与历史日志修正规程](docs/operations/pricing-maintenance.md)。

## CI/CD

- 版本单一来源：根目录 `VERSION`
- `release.yml`、`tauri-release.yml`、Docker 构建都会读取 `VERSION`
- 发布流程仅支持手动触发 `workflow_dispatch`，不再由 `push tag` 自动触发
- 正式发布仅允许稳定 semver tag（`vMAJOR.MINOR.PATCH`），并校验输入 tag 与 `VERSION` 完全一致
- Release 构建产物命名统一为：
  - 后端：`new-api_<version-no-v>_<os>_<arch>[.exe]`
  - 桌面端：`new-api-desktop_<version-no-v>_<os>_<arch>.<ext>`
- 桌面客户端产物仅由 Tauri 2 工作流发布，Electron 产物流程已移除
- Docker 镜像默认发布到 `ghcr.io/misonl/new-api`
- alpha Docker 镜像流程固定使用 `alpha` 分支头提交作为构建来源
- 如需同时发布到 Docker Hub，需要配置仓库 secrets：
  - `DOCKERHUB_USERNAME`
  - `DOCKERHUB_TOKEN`
- 项目发布渠道仅保留 GitHub Release、GitHub Actions 与 GitHub Container Registry

### 手动发布 Stable Release

当前正式发布不再依赖推送 tag 自动触发，而是手动执行 GitHub Actions：

1. 确认 `main` 已合入目标改动。
2. 更新根目录 `VERSION` 与 [CHANGELOG.md](/Volumes/Work/code/new-api/CHANGELOG.md)。
3. 推送 `main`。
4. 在 GitHub Actions 手动触发：
   - `release.yml`
   - `tauri-release.yml`
5. 两个工作流都使用同一个稳定版本 tag，例如 `v1.1.1`。
6. 触发后核对 GitHub Release：
   - 后端二进制命名为 `new-api_<version-no-v>_<os>_<arch>[.exe]`
   - 桌面端命名为 `new-api-desktop_<version-no-v>_<os>_<arch>.<ext>`
   - 校验文件命名正确且无旧 Electron 产物残留

## 与上游的关系

- 上游项目：`QuantumNous/new-api`
- 本仓库会继续吸纳对基础能力有价值、且不会破坏现有增强功能的上游变更
- 不符合本仓库路线或会明显影响既有能力可用性的上游改动，可以跳过

## 说明

- 项目名称、模块路径和上游归属信息继续保留
- 生产验证前，请先在独立环境完成配置迁移和联调
