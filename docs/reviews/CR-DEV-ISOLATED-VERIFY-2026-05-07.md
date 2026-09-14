# CR-DEV-ISOLATED-VERIFY-2026-05-07

## 摘要

本轮检查目标是使用最新代码重新部署完全隔离开发环境，并对后端、前端和 `web/default` Web UI 做运行态验证。

结论：

- 初始重建时，完全隔离开发环境已重建到已验证的候选提交。
- 后续最终复核时，仓库 `main` / `origin/main` 已前进，但运行中的隔离开发容器仍停留在此前的候选提交。
- 因此，截至最终复核，隔离开发环境健康，但不是当前 `main` 的最新版本。
- `new-api-dev-isolated-new-api-1`、PostgreSQL、Redis 均为 `healthy`。
- 后端 `/api/status` 返回 `success=true`。
- 后端测试和 `web/default` 前端 lint、build、i18n sync、typecheck 均通过。
- `3001` 容器入口当前按系统配置加载 classic 主题；验证 `web/default` 页面时需要独立启动前端开发服务器代理到 `3001`。
- 本轮 `web/default` 运行态验证使用 `http://127.0.0.1:5176/`，后端代理目标为 `http://127.0.0.1:3001`。

## 环境

- 代码仓库：`<repository-root>`
- 分支：`main`
- 清理前 Git 状态：`main...origin/main`
- 初始部署提交：`<initial-validated-revision>`
- 最终仓库提交：`<final-repository-revision>`
- 最终运行容器提交：`<running-container-revision>`
- 开发 Compose 文件：`deploy/compose/dev-isolated.yml`
- 开发环境变量文件：`deploy/env/dev-isolated.env`
- 开发镜像：`new-api-local:dev`
- 开发后端地址：`http://127.0.0.1:3001`
- 临时 `web/default` 开发地址：`http://127.0.0.1:5176`

## 部署命令

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker inspect new-api-dev-isolated-new-api-1 --format '{{.State.Health.Status}} {{index .Config.Labels "org.opencontainers.image.revision"}} {{.Config.Image}}'
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env ps
```

关键结果：

- `docker inspect` 输出 `healthy <validated-revision> new-api-local:dev`。
- `/new-api --build-info` 输出 `version=v1.1.0`、`commit=<validated-revision>`。
- `/api/status` 返回 `success=true`。
- Compose 状态显示 `new-api`、`postgres`、`redis` 均为 `healthy`。

## 验证命令

```bash
go test ./...
cd web/default && bun run lint
cd web/default && bun run build
cd web/default && bun run i18n:sync
cd web/default && bun run typecheck
```

关键结果：

- `go test ./...` 通过。
- `bun run lint` 通过。
- `bun run build` 通过。
- `bun run i18n:sync` 通过。
- `bun run typecheck` 通过。

## Web UI 运行态检查

`3001` 容器入口当前仍由系统配置加载 classic 主题，因此不能用 `http://127.0.0.1:3001/rankings` 代表 `web/default` 的验证结果。

`web/default` 验证命令：

```bash
cd web/default
VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001 bun run dev --host 127.0.0.1 --port 5176
```

已浏览验证：

- `/dashboard/models`
  - 数据看板主页面正常渲染。
  - 偏好设置弹窗能打开，并带出当前范围、时间粒度和图表类型。
  - 筛选弹窗能打开，并带出快速范围、自定义时间范围、时间粒度和管理员用户名筛选。
- `/rankings`
  - 排行页正常渲染。
  - 周期 Tab、热门模型、市场份额、排名上升和排名下降区域正常显示。
- `/channels`
  - 渠道表格正常渲染。
  - 搜索、状态、类型、分组、查看菜单、行级操作入口可见。
- `/keys`
  - API 密钥页正常渲染。
  - 空状态正常显示。
- `/usage-logs/common`
  - 通用日志页正常渲染。
  - 时间范围、模型、分组、类型、令牌、用户名、渠道 ID、请求 ID 筛选入口可见。
- `/usage-logs/task`
  - 任务日志页正常渲染。
  - 绘图日志和任务日志 Tab、任务 ID、渠道 ID 筛选入口可见。
- `/system-settings/models/global`
  - 全局模型配置页正常渲染。
  - 请求透传、禁用思考处理模型、响应兼容策略 JSON、保持连接心跳配置可见。

浏览器控制台：

- 未发现业务错误。
- 仅出现一条 reduced-motion 警告，属于本机系统动画偏好提示。

## 清理

已清理：

- 关闭临时 `5176` 前端开发服务器。
- 关闭本轮打开的 `5176` 浏览器页。

保留：

- 完全隔离 Docker 开发栈继续运行，便于后续验收。

收尾复核：

```bash
lsof -nP -iTCP:5176 -sTCP:LISTEN || true
git status --short --branch
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env ps
curl -fsS http://127.0.0.1:3001/api/status
```

关键结果：

- `5176` 无监听进程。
- 工作区当时为 `main...origin/main`，无业务代码残留改动。
- 隔离 Docker 栈仍为 `healthy`。
- `/api/status` 返回 `success=true`。

## 剩余风险与门禁边界

- 本轮没有执行真实渠道写入、渠道批量测试或真实模型调用，避免对隔离数据面产生额外副作用。
- 本轮没有把 `web/default` 构建产物切换成 `3001` 容器默认入口；`3001` 默认入口仍取决于系统配置。
- 若后续验证目标是容器内嵌 `web/default` 产物，需要先调整系统主题配置或构建打包入口，再重新做容器内页面验证。

## 最终当前状态检查

复核时间记录在私有审计中。

复核命令：

```bash
git status --short --branch
git rev-parse HEAD
git rev-parse origin/main
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
docker inspect new-api-dev-isolated-new-api-1 --format 'image={{.Config.Image}} created={{.Created}} status={{.State.Status}} health={{.State.Health.Status}} started={{.State.StartedAt}}'
docker image inspect new-api-local:dev --format 'id={{.Id}} created={{.Created}}'
```

关键结果：

- Git 输出 `main...origin/main`，`HEAD` 与 `origin/main` 均指向已验证的最终提交。
- 运行中开发容器 `/new-api --build-info` 输出的 `commit` 与已验证的隔离候选一致。
- 运行中容器镜像为 `new-api-local:dev`，状态为 `running`，健康状态为 `healthy`。
- 开发镜像创建时间属于历史运行快照，不在公开记录中保留。

结论：

- 当前隔离开发环境可用且健康。
- 当前隔离开发环境不是当前 `main` / `origin/main` 的最新代码。
- 需要重新执行 `scripts/build-docker-local.sh new-api-local:dev` 并重建隔离 `new-api` 服务后，再用 `/new-api --build-info` 比对运行提交与 `git rev-parse HEAD`。
