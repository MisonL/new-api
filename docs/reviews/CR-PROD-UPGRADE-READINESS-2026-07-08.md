# CR-PROD-UPGRADE-READINESS-2026-07-08

## Scope

- 检查时间：2026-07-08 16:04:34 CST
- 复检补充时间：2026-07-08 16:12:15 CST
- 目标：在不变更正式服务的前提下，确认升级前门禁、备份、回滚和 Compose 流程。
- 边界：本报告只记录准备状态；未执行正式服务升级。最终候选镜像标签、备份路径和回滚标签以本机 `.dev-docker/backups/production-upgrade-*/upgrade-prep-summary.txt` 为准，避免在 Git 文档中硬编码每次重建都会变化的本机标签。

## Current Production Runtime

正式容器：

- container: `new-api`
- image: `new-api-local:prod-20260628-232701-f19d343-dirty`
- build-info commit: `f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty`
- status endpoint: `http://127.0.0.1:13000/api/status` 可访问
- published port: `127.0.0.1:13000 -> 3000/tcp`
- restart policy: `always`

Compose 标签：

- project: `new-api`
- config file: `/Volumes/Work/code/new-api/docker-compose.yml`
- working dir: `/Volumes/Work/code/new-api`
- services checked: `new-api`, `postgres`, `redis`

挂载：

- `/Volumes/Work/code/new-api/logs -> /app/logs`
- `/Volumes/Work/code/new-api/data -> /data`

数据库：

- container: `postgres`
- `pg_isready -U root -d new-api` 通过
- `select current_database(), current_user` 返回 `new-api|root`

磁盘：

- `/Volumes/Work` 可用空间约 `781Gi`
- Docker images 总量约 `10.04GB`
- Docker build cache 总量约 `25.02GB`

## Upgrade Gate

升级候选必须满足：

- Git 工作区干净。
- 镜像 build-info revision 等于候选 Git commit，且不带 `-dirty`。
- 先在 3001 隔离开发环境完成健康检查和 compact 控制面 e2e。
- 正式服务升级必须保持 Compose project `new-api`，不能用 `docker run` 替代 Compose。
- Compose 执行时必须显式保留正式端口和数据目录变量，避免落回默认 `3000:3000` 或相对路径。

复检状态：

- 初始检查发现的 dirty 候选阻断已通过提交、重建干净镜像和 3001 复验解除。
- 每次更新本报告后，如要把报告 commit 纳入最终发布点，必须重新构建候选镜像并复跑 3001 健康检查，确保 build-info revision 等于最终 Git commit。

## Required Production Variables

升级命令必须显式传入或继承当前正式变量：

- `NEW_API_IMAGE`
- `NEW_API_PORT_MAPPING=127.0.0.1:13000:3000`
- `NEW_API_DATA_DIR=/Volumes/Work/code/new-api/data`
- `NEW_API_LOG_DIR=/Volumes/Work/code/new-api/logs`
- `SESSION_SECRET`
- `CRYPTO_SECRET`
- `SQL_DSN`
- `REDIS_CONN_STRING`

说明：本次检查只打印了变量名和非敏感端口、挂载信息，未输出密钥或连接串内容。

执行正式升级前必须先在同一个 shell 中加载受控正式环境变量文件，或确认当前 shell 已继承这些变量。不得在变量为空时执行 Compose。

非空校验：

```bash
for name in SESSION_SECRET CRYPTO_SECRET SQL_DSN REDIS_CONN_STRING; do
  value="$(printenv "$name" || true)"
  if [ -z "$value" ]; then
    echo "missing required production variable: $name" >&2
    exit 1
  fi
done
```

## Backup And Rollback Plan

升级前必须完成：

```bash
mkdir -p .dev-docker/backups/production-upgrade-<timestamp>
docker exec postgres pg_dump -U root -d new-api --format=custom --file=/tmp/new-api-preupgrade.dump
docker cp postgres:/tmp/new-api-preupgrade.dump .dev-docker/backups/production-upgrade-<timestamp>/new-api-preupgrade.dump
docker exec postgres rm -f /tmp/new-api-preupgrade.dump
docker tag new-api-local:prod-20260628-232701-f19d343-dirty new-api-local:rollback-<timestamp>-f19d343-dirty
```

回滚入口：

```bash
for name in SESSION_SECRET CRYPTO_SECRET SQL_DSN REDIS_CONN_STRING; do
  value="$(printenv "$name" || true)"
  if [ -z "$value" ]; then
    echo "missing required production variable: $name" >&2
    exit 1
  fi
done

NEW_API_IMAGE=new-api-local:rollback-<timestamp>-f19d343-dirty \
NEW_API_PORT_MAPPING=127.0.0.1:13000:3000 \
NEW_API_DATA_DIR=/Volumes/Work/code/new-api/data \
NEW_API_LOG_DIR=/Volumes/Work/code/new-api/logs \
docker compose -f docker-compose.yml up -d --no-deps --force-recreate new-api
```

如数据库迁移已经生效且需要数据回滚，先停止写入，再恢复 `pg_dump` 备份。

## Production Upgrade Command Shape

正式升级应使用同一个 Compose project 和配置文件。执行前必须先加载受控正式环境变量文件，或确认当前 shell 已继承 `SESSION_SECRET`、`CRYPTO_SECRET`、`SQL_DSN`、`REDIS_CONN_STRING`。

```bash
for name in SESSION_SECRET CRYPTO_SECRET SQL_DSN REDIS_CONN_STRING; do
  value="$(printenv "$name" || true)"
  if [ -z "$value" ]; then
    echo "missing required production variable: $name" >&2
    exit 1
  fi
done

NEW_API_IMAGE=<candidate-image> \
NEW_API_PORT_MAPPING=127.0.0.1:13000:3000 \
NEW_API_DATA_DIR=/Volumes/Work/code/new-api/data \
NEW_API_LOG_DIR=/Volumes/Work/code/new-api/logs \
docker compose -f docker-compose.yml up -d --no-deps --force-recreate new-api
```

升级后必须验证：

```bash
docker exec new-api /new-api --build-info
curl -fsS http://127.0.0.1:13000/api/status
docker inspect new-api --format '{{json .Config.Labels}}'
docker inspect new-api --format '{{json .NetworkSettings.Ports}}'
docker logs --tail=200 new-api
```

通过条件：

- build-info revision 等于候选 commit。
- `/api/status` 返回成功。
- Compose 标签仍包含 project `new-api`、service `new-api`、config file `/Volumes/Work/code/new-api/docker-compose.yml`。
- 端口仍为 `127.0.0.1:13000 -> 3000/tcp`。
- 最近日志无启动错误、迁移错误或 panic。
