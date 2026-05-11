# CR-DEV-ISOLATED-VERIFY-2026-05-11

## Summary

采样时间：2026-05-11T09:42:02Z。

本轮检查目标是确认完全隔离 Docker 开发环境是否运行当前分支最新源码。

结论：

- 当前分支为 `codex/usage-log-channel-tooltip-fix`。
- 本地 `HEAD` 与 `origin/codex/usage-log-channel-tooltip-fix` 一致，均为 `3a6c2633c0d05f6f158d114d5df03ae897e3e5b3`。
- 隔离开发容器 `new-api-dev-isolated-new-api-1` 的 `/new-api --build-info` 输出同一 commit。
- 因此，当前 `3001` 隔离开发环境运行的是当前分支最新源码。
- `3001` `/api/status` 返回 `success:true`。
- 容器健康状态为 `healthy`。
- `3001` 当前系统配置仍返回 `theme:"classic"`，所以它证明服务和镜像已同步，不代表 `web/default` 的真实页面视觉运行态。

## Environment

- Repository: `/Volumes/Work/code/new-api`
- Branch: `codex/usage-log-channel-tooltip-fix`
- Remote branch: `origin/codex/usage-log-channel-tooltip-fix`
- Current commit: `3a6c2633c0d05f6f158d114d5df03ae897e3e5b3`
- Dev Compose file: `deploy/compose/dev-isolated.yml`
- Dev env file: `deploy/env/dev-isolated.env`
- Dev image: `new-api-local:dev`
- Dev container: `new-api-dev-isolated-new-api-1`
- Dev backend URL: `http://127.0.0.1:3001`

## Current-State Commands

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
- `git rev-parse HEAD` 与 `git rev-parse origin/codex/usage-log-channel-tooltip-fix` 均为 `3a6c2633c0d05f6f158d114d5df03ae897e3e5b3`。
- `/new-api --build-info` 输出：

```text
version=v1.1.0
commit=3a6c2633c0d05f6f158d114d5df03ae897e3e5b3
date=2026-05-11T09:32:09Z
source=https://github.com/MisonL/new-api
```

- `/api/status` 摘要：

```json
{"success":true,"version":"v1.1.0","theme":"classic","server_address":"http://127.0.0.1:3001"}
```

- 容器状态摘要：

```text
new-api-dev-isolated-new-api-1 new-api-local:dev Up 5 minutes (healthy) 0.0.0.0:3001->3000/tcp, [::]:3001->3000/tcp
```

## Build And Deploy Evidence

本轮复查前已执行最新镜像构建和隔离服务重建：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
```

关键结果：

- Docker build 退出码为 `0`。
- 镜像构建输出 `built image=new-api-local:dev version=v1.1.0 commit=3a6c2633c0d05f6f158d114d5df03ae897e3e5b3 date=2026-05-11T09:32:09Z`。
- `new-api-dev-isolated-new-api-1` 已 force recreate，并恢复到 `healthy`。

## Related Verification

与当前提交相关的前端和契约验证：

```bash
bun test web/tests/defaultThemeTokenContracts.test.mjs web/tests/defaultLayoutContracts.test.mjs
cd web/default && bun run lint
cd web/default && ./node_modules/.bin/rsbuild build
```

关键结果：

- 契约测试：5 pass，0 fail。
- ESLint：退出码 `0`。
- `rsbuild build`：退出码 `0`。
- Docker build 内部 `web/default` 构建也通过，输出 `ready built in 23.1 s`。

## Boundary

- `3001` 容器入口当前按系统配置加载 classic 主题。
- 若目标是验证 `web/default` 页面视觉或交互，需要单独启动 `web/default` dev server 并代理到 `3001`，或先调整系统主题配置后再做容器入口页面验证。
- 本轮没有执行真实渠道调用、模型调用或写库类副作用验证。
