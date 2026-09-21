# CR-DEV-ISOLATED-VERIFY-2026-05-11

## 摘要

采样时间保存在私有验证记录中。

本轮检查目标是记录完全隔离 Docker 开发环境在采样时运行的源码版本。

结论：

- 采样时分支为 `codex/usage-log-channel-tooltip-fix`。
- 本地 `HEAD` 与对应远程分支一致，均指向已验证的候选提交。
- 隔离开发容器的 `/new-api --build-info` 输出同一 `commit`。
- 因此，采样时 `3001` 隔离开发环境运行的是该分支当时的最新应用源码。
- `3001` `/api/status` 返回 `success:true`。
- 容器健康状态为 `healthy`。
- `3001` 当前系统配置仍返回 `theme:"classic"`，所以它证明服务和镜像已同步，不代表 `web/default` 的真实页面视觉运行态。

## 环境

- 代码仓库：`<repository-root>`
- 分支：`<validated-branch>`
- 远程分支：`<validated-remote-branch>`
- 已验证应用提交：`<validated-revision>`
- 开发 Compose 文件：`deploy/compose/dev-isolated.yml`
- 开发环境变量文件：`deploy/env/dev-isolated.env`
- 开发镜像：`new-api-local:dev`
- 开发容器：`<isolated-new-api-container>`
- 开发后端地址：`http://127.0.0.1:3001`

## 当前状态命令

```bash
git rev-parse HEAD
git rev-parse origin/codex/usage-log-channel-tooltip-fix
git status --short --branch
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
docker ps --filter name=new-api-dev-isolated-new-api-1 --format '{{.Names}} {{.Image}} {{.Status}} {{.Ports}}'
```

关键结果：

- `git status --short --branch` 输出 `codex/usage-log-channel-tooltip-fix...origin/codex/usage-log-channel-tooltip-fix`，无未提交变更。
- `git rev-parse HEAD` 与对应远程分支解析结果一致，均为 `<validated-revision>`。
- `/new-api --build-info` 输出：

```text
version=v1.1.0
commit=<validated-revision>
date=<build-time>
source=https://github.com/MisonL/new-api
```

- `/api/status` 摘要：

```json
{"success":true,"version":"v1.1.0","theme":"classic","server_address":"http://127.0.0.1:3001"}
```

- 容器状态摘要：

```text
<isolated-new-api-container> new-api-local:dev Up (healthy) <isolated-port-mapping>
```

## 构建与部署证据

本轮复查前已执行最新镜像构建和隔离服务重建：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
```

关键结果：

- Docker build 退出码为 `0`。
- 镜像构建输出包含 `image`、`version`、`commit` 和 `date` 等 build-info 字段；具体运行快照保存在私有验证记录中。
- 隔离应用容器已 force recreate，并恢复到 `healthy`。

## 相关验证

与当前提交相关的前端和契约验证：

```bash
bun test web/tests/defaultThemeTokenContracts.test.mjs web/tests/defaultLayoutContracts.test.mjs
node --test web/tests/dashboardChartHoverStyle.test.mjs
cd web/default && bun run lint
cd web/default && ./node_modules/.bin/rsbuild build
```

关键结果：

- 契约测试：5 pass，0 fail。
- Dashboard hover 契约测试：2 pass，0 fail。
- ESLint：退出码 `0`。
- `rsbuild build`：退出码 `0`。
- Docker build 内部 `web/default` 构建也通过，输出 `ready built in 23.1 s`。
- `3001` classic 运行态验证覆盖“模型数据分析 / 消耗分布”：90 天范围下 canvas 非空，悬停柱子出现 tooltip。

## 范围边界

- `3001` 容器入口当前按系统配置加载 classic 主题。
- 若目标是验证 `web/default` 页面视觉或交互，需要单独启动 `web/default` dev server 并代理到 `3001`，或先调整系统主题配置后再做容器入口页面验证。
- 本轮没有执行真实渠道调用、模型调用或写库类副作用验证。
