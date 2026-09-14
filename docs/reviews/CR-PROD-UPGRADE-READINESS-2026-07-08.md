# CR-PROD-UPGRADE-READINESS-2026-07-08

## 范围

- 检查时间和复检补充时间记录在私有审计中。
- 目标：在不变更正式服务的前提下，确认升级前门禁、备份、回滚和 Compose 流程。
- 边界：本报告只记录准备状态；未执行正式服务升级。公开版本不保存特定主机的镜像标签、绝对路径、容量或容器生命周期值；这些值应写入受保护的私有审计记录。

## 当前正式运行态

正式容器：

- 容器：`new-api`
- 镜像：`<production-image-reference>`
- build-info 提交：`<production-revision>`
- 状态接口：`http://127.0.0.1:13000/api/status` 可访问
- 发布端口：`127.0.0.1:13000 -> 3000/tcp`
- 重启策略：`always`

Compose 标签：

- 项目：`new-api`
- 配置文件：`<repository-root>/docker-compose.yml`
- 工作目录：`<repository-root>`
- 已检查服务：`new-api`、`postgres`、`redis`

挂载：

- `<production-log-dir> -> /app/logs`
- `<production-data-dir> -> /data`

数据库：

- 容器：`postgres`
- `pg_isready -U <db-user> -d <db-name>` 通过
- `select current_database(), current_user` 返回预期的数据库和账号

磁盘：

- 受监控宿主卷可用空间：`<captured-free-space>`
- Docker images 总量：`<captured-image-size>`
- Docker build cache 总量：`<captured-cache-size>`

## 升级门禁

升级候选必须满足：

- Git 工作区干净。
- 镜像 build-info revision 等于候选 Git commit，且不带 `-dirty`。
- 先在 3001 隔离开发环境完成健康检查和 compact 控制面 e2e。
- 正式服务升级必须保持 Compose project `new-api`，不能用 `docker run` 替代 Compose。
- Compose 执行时必须显式保留正式端口和数据目录变量，避免落回默认 `3000:3000` 或相对路径。

复检状态：

- 初始检查发现的 dirty 候选阻断已通过提交、重建干净镜像和 3001 复验解除。
- 每次更新本报告后，如要把报告 commit 纳入最终发布点，必须重新构建候选镜像并复跑 3001 健康检查，确保 build-info revision 等于最终 Git commit。

## 必需的正式环境变量

升级命令必须显式传入或继承当前正式变量：

- `NEW_API_IMAGE`
- `NEW_API_PORT_MAPPING=127.0.0.1:13000:3000`
- `NEW_API_DATA_DIR=<production-data-dir>`
- `NEW_API_LOG_DIR=<production-log-dir>`
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

## 备份与回滚方案

升级前必须完成：

```bash
BACKUP_DIR="${NEW_API_ARCHIVE_DIR:?set NEW_API_ARCHIVE_DIR}/production-upgrade-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$BACKUP_DIR"
docker exec postgres pg_dump -U <db-user> -d <db-name> --format=custom --file=/tmp/new-api-preupgrade.dump
docker cp postgres:/tmp/new-api-preupgrade.dump "$BACKUP_DIR/new-api-preupgrade.dump"
docker exec postgres rm -f /tmp/new-api-preupgrade.dump
docker tag "$NEW_API_IMAGE" "${NEW_API_ROLLBACK_IMAGE:?set NEW_API_ROLLBACK_IMAGE}"
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

NEW_API_IMAGE="${NEW_API_ROLLBACK_IMAGE:?set NEW_API_ROLLBACK_IMAGE}" \
NEW_API_PORT_MAPPING=127.0.0.1:13000:3000 \
NEW_API_DATA_DIR="${NEW_API_DATA_DIR:?set NEW_API_DATA_DIR}" \
NEW_API_LOG_DIR="${NEW_API_LOG_DIR:?set NEW_API_LOG_DIR}" \
docker compose -f docker-compose.yml up -d --no-deps --force-recreate new-api
```

如数据库迁移已经生效且需要数据回滚，先停止写入，再恢复 `pg_dump` 备份。

## 正式升级命令形式

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
NEW_API_DATA_DIR="${NEW_API_DATA_DIR:?set NEW_API_DATA_DIR}" \
NEW_API_LOG_DIR="${NEW_API_LOG_DIR:?set NEW_API_LOG_DIR}" \
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
- Compose 标签仍包含预期 project、service 和 `<repository-root>/docker-compose.yml` 配置来源。
- 端口仍为 `127.0.0.1:13000 -> 3000/tcp`。
- 最近日志无启动错误、迁移错误或 panic。
