# macOS Docker Desktop TUN 局域网部署模式

## 适用范围

本模式只用于 macOS Docker Desktop 主机，同时满足以下条件：

- 主机运行 FlClash、Clash 或类似 TUN 代理。
- `new-api` 通过 Docker published port 暴露到 `0.0.0.0:3000`。
- 本机访问 `http://localhost:3000` 时，`new-api` 使用日志记录到异常公网 IP。
- 仍需要局域网用户访问 `http://<Mac局域网IP>:3000`。

Linux 原生 Docker 通常不需要本模式。Windows Docker Desktop 是否需要，应先实测客户端 IP 记录是否异常。

## 设计

不要把入口反向代理放进 Docker Compose。这个问题发生在 Docker Desktop published port 入口路径上；如果 Caddy 也作为容器暴露 `0.0.0.0:3000`，Caddy 自己看到的客户端 IP 可能已经是错误值。

默认链路：

```text
本机 Codex 或局域网用户
-> macOS 宿主机 Caddy 0.0.0.0:3000
-> Docker loopback 127.0.0.1:13000
-> new-api 容器 3000
```

在这个链路中：

- Docker 只发布 `127.0.0.1:13000:3000`。
- macOS 宿主机 Caddy 监听 `0.0.0.0:3000`。
- 本模式的 Caddyfile 关闭 Caddy admin API；脚本只用 PID 文件管理自己启动的专用 Caddy 进程，避免重载或停止机器上可能已有的 Caddy 服务。
- Caddy 显式覆盖 `X-Real-IP`；`X-Forwarded-For` 和 `X-Forwarded-Proto` 由 Caddy 默认生成，不要改成透传客户端原始同名头。

默认参数适配本仓库的 Compose service `new-api`、容器端口 `3000` 和健康路径 `/api/status`。其他 Mac 用户如果 fork 后改过 service 名、容器端口或健康路径，只需要修改 `deploy/env/macos-docker-tun.env` 中的对应变量，不需要改脚本。

## 部署步骤

### Codex 断联风险

如果当前 Codex 会话本身通过 `http://localhost:3000/v1` 连接本机 `new-api`，正式切换时重建 `new-api` 会短暂释放 `3000`，Codex 可能立刻断联，无法继续执行后续步骤。

正式实施不要在依赖当前 `new-api` 的 Codex 会话里逐条执行命令。应在 macOS 宿主机终端运行一次性脚本，让同一个 shell 进程完成 Docker 端口切换、Caddy 接管、健康检查和失败回滚。

### 实施前预检

确认当前正式容器和端口状态：

```bash
docker compose ps
lsof -nP -iTCP:3000 -sTCP:LISTEN
lsof -nP -iTCP:13000 -sTCP:LISTEN
ps -axo pid,command | grep '[c]addy'
```

正式切换前，`3000` 应由当前 `new-api` Compose service 占用，或已由本脚本 PID 文件管理的 Caddy 占用；`13000` 应为空闲，除非当前服务已经处于本模式的 loopback 发布状态。

确认 Caddy 可用并能解析本配置：

```bash
command -v caddy
command -v python3
command -v lsof
caddy validate --config deploy/proxy/macos-docker-tun/Caddyfile
```

本模式不使用 Caddy admin API，因此不会调用 `caddy reload` 或 `caddy stop` 影响宿主机已有 Caddy 服务。

确认 compose 默认模式和 macOS TUN 模式渲染符合预期：

```bash
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  SESSION_SECRET=dummy CRYPTO_SECRET=dummy \
  NEW_API_PORT_MAPPING=3000:3000 \
  docker compose -f docker-compose.yml --env-file /dev/null config --format json \
  | jq --arg service "${COMPOSE_SERVICE:-new-api}" '.services[$service].ports'

env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  SESSION_SECRET=dummy CRYPTO_SECRET=dummy \
  NEW_API_PORT_MAPPING=127.0.0.1:13000:3000 \
  docker compose -f docker-compose.yml --env-file /dev/null --env-file deploy/env/macos-docker-tun.env.example config --format json \
  | jq --arg service "${COMPOSE_SERVICE:-new-api}" '.services[$service].ports'
```

默认模式应渲染为 `3000:3000`；macOS TUN 模式应渲染为 `127.0.0.1:13000:3000`。

也可以直接运行脚本预检：

```bash
cp deploy/env/macos-docker-tun.env.example deploy/env/macos-docker-tun.env
scripts/deploy-macos-docker-tun.sh --check-only
```

脚本预检会校验 Compose 渲染后的 `SESSION_SECRET` 和 `CRYPTO_SECRET` 非空。若只是做无正式 `.env` 的配置渲染验证，可以临时在命令前加 `SESSION_SECRET=dummy CRYPTO_SECRET=dummy`；正式 `--apply` 会拒绝这类占位值。

如果 `PROD_ENV_FILE` 指向的正式 env 文件不存在，脚本会同时给 TUN 模式和默认回滚模式传入 `--env-file /dev/null`，显式禁止 Docker Compose 隐式读取当前目录 `.env`。这种情况下，只有调用方 shell 中显式导出的变量和 `deploy/env/macos-docker-tun.env` 白名单变量会参与渲染。

`--check-only` 也会检查 LAN 入口端口上的已建立连接，并打印最多 20 条连接样本。若仍有连接且 `NEW_API_ALLOW_ACTIVE_REQUESTS=0`，正式 `--apply` 会先等待 `NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT` 秒；超时后仍有连接才拒绝继续。若设置为 `NEW_API_ALLOW_ACTIVE_REQUESTS=1`，正式实施会中断这些连接。

`NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT=0` 表示不等待连接排空；如果同时保持 `NEW_API_ALLOW_ACTIVE_REQUESTS=0`，正式 `--apply` 会在发现活跃连接后立即拒绝继续。

宿主机 Caddy 会显式重写 `X-Real-IP` 和 `X-Forwarded-For` 为当前连接的远端地址，避免客户端自带 `X-Forwarded-For` 伪造记录 IP。

`deploy/env/macos-docker-tun.env` 支持以下通用参数：

```bash
COMPOSE_SERVICE=new-api
NEW_API_LAN_PROXY_PORT=3000
NEW_API_LOOPBACK_PORT=13000
NEW_API_CONTAINER_PORT=3000
NEW_API_HEALTH_PATH=/api/status
NEW_API_DEFAULT_PORT_MAPPING=3000:3000
NEW_API_ALLOW_ACTIVE_REQUESTS=0
NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT=30
NEW_API_ABNORMAL_TUN_IP_REGEX=
NEW_API_PORT_MAPPING=127.0.0.1:13000:3000
```

其中 `NEW_API_PORT_MAPPING` 必须与 `NEW_API_LOOPBACK_PORT` 和 `NEW_API_CONTAINER_PORT` 保持一致。脚本会在预检阶段校验这三者是否匹配，并要求 Compose 渲染出的端口列表唯一且完全等于期望的 loopback 映射。`NEW_API_DEFAULT_PORT_MAPPING` 用于回滚，通常保持为原来的直接发布入口。

`deploy/env/macos-docker-tun.env` 只能保存上方列出的本模式参数。不要把 `SESSION_SECRET`、`CRYPTO_SECRET`、`SQL_DSN`、`REDIS_CONN_STRING`、镜像名、数据目录或日志目录写入这个文件；这些运行时配置必须继续放在正式 `.env` 或宿主机环境中。脚本会拒绝 TUN env 中的非白名单变量，避免主路径和回滚路径使用不同配置来源。

### 正式实施

推荐使用一次性脚本实施：

```bash
cp deploy/env/macos-docker-tun.env.example deploy/env/macos-docker-tun.env
scripts/deploy-macos-docker-tun.sh --apply
```

脚本会按顺序执行：

- 校验 Caddyfile 和 compose 端口映射。
- 校验端口值合法，且 LAN 入口端口与 Docker loopback 后端端口不同。
- 校验 TUN 模式和默认回滚模式的 Compose 端口映射。
- 校验 `SESSION_SECRET` 和 `CRYPTO_SECRET` 在 TUN 模式和默认回滚模式中都已设置，正式实施时拒绝明显占位值。
- 校验 LAN 入口端口没有被非当前服务、非脚本管理 Caddy 的进程占用；校验 Docker loopback 后端端口空闲，或已由当前服务的同一 loopback 映射占用。
- 默认等待 LAN 入口端口的已建立连接排空，超时后仍拒绝重建 `new-api` 并输出连接样本；若接受中断活跃请求，可显式设置 `NEW_API_ALLOW_ACTIVE_REQUESTS=1`。
- 重建 `COMPOSE_SERVICE` 指定的服务到 `127.0.0.1:13000:3000`。
- 使用专用 PID 文件启动宿主机 Caddy 接管 `3000`。
- 确认 PID 文件指向的 Caddy 进程仍存活、命令行匹配当前 Caddyfile，且实际监听 LAN 入口端口。
- 检查 `127.0.0.1:13000/api/status` 和 `127.0.0.1:3000/api/status`。
- 若健康检查失败、无法通过 `X-Oneapi-Request-Id` 在容器日志中匹配到 public health 探针对应的 `127.0.0.1` 记录，或配置了 `NEW_API_ABNORMAL_TUN_IP_REGEX` 且异常 TUN IP 仍出现在该探针日志中，只停止 PID 文件指向的专用 Caddy 进程，并回滚到 `NEW_API_DEFAULT_PORT_MAPPING`。

以下手动步骤仅用于排障或不使用脚本时参考。

1. 准备环境文件。

```bash
cp deploy/env/macos-docker-tun.env.example deploy/env/macos-docker-tun.env
```

如果已有正式 `.env` 保存 `SESSION_SECRET`、`CRYPTO_SECRET`、`SQL_DSN`、`REDIS_CONN_STRING`、数据目录等配置，继续保留它；`deploy/env/macos-docker-tun.env` 只保存本模式需要覆盖的 service、端口和健康检查参数。

2. 先验证最终 compose 渲染，再以 macOS TUN 端口绑定重建 `new-api`。

```bash
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  NEW_API_PORT_MAPPING=127.0.0.1:13000:3000 \
  docker compose -f docker-compose.yml --env-file .env --env-file deploy/env/macos-docker-tun.env config \
  | grep -A4 'ports:'
```

确认 `new-api` 只会发布到 `127.0.0.1:13000` 后，再执行：

```bash
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  NEW_API_PORT_MAPPING=127.0.0.1:13000:3000 \
  docker compose -f docker-compose.yml --env-file .env --env-file deploy/env/macos-docker-tun.env up -d --no-deps --force-recreate new-api
```

如果 `COMPOSE_SERVICE` 改成了其他名称，手动命令里的 `new-api` 也要替换成相同 service 名。

如果没有单独 `.env`，但当前 shell 已经导出正式环境变量，可只使用：

```bash
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  NEW_API_PORT_MAPPING=127.0.0.1:13000:3000 \
  docker compose -f docker-compose.yml --env-file /dev/null --env-file deploy/env/macos-docker-tun.env up -d --no-deps --force-recreate new-api
```

如已自定义端口，应把上方 `127.0.0.1:13000:3000` 替换为与 `deploy/env/macos-docker-tun.env` 一致的映射。推荐优先使用脚本；脚本会读取 `COMPOSE_SERVICE` 并自动替换 service 名。

3. 启动宿主机 Caddy。

```bash
brew install caddy
caddy run --config deploy/proxy/macos-docker-tun/Caddyfile
```

默认监听 `3000` 并转发到 `127.0.0.1:13000`。如需临时验证其他端口，可覆盖：

```bash
NEW_API_LAN_PROXY_PORT=3001 \
NEW_API_LOOPBACK_PORT=13001 \
  caddy run --config deploy/proxy/macos-docker-tun/Caddyfile
```

需要后台运行时，优先使用 `scripts/deploy-macos-docker-tun.sh --apply`，让脚本通过 `/tmp/<COMPOSE_SERVICE>-macos-tun-caddy-<LAN_PORT>.pid` 记录专用 Caddy 进程。默认路径是 `/tmp/new-api-macos-tun-caddy-3000.pid`。不要覆盖 `/usr/local/etc/Caddyfile` 或重启已有 Homebrew Caddy 服务，除非已确认这台机器没有其他 Caddy 业务。

停止本模式的专用 Caddy 时，按 PID 文件终止脚本启动的进程：

```bash
kill "$(cat /tmp/new-api-macos-tun-caddy-3000.pid)"
```

## 访问方式

- 本机 Codex 推荐配置：`http://127.0.0.1:3000/v1`
- 局域网用户访问：`http://<Mac局域网IP>:3000`
- Docker 直接入口：`http://127.0.0.1:13000`

本机 Codex 如果使用 `http://127.0.0.1:3000/v1`，使用日志应记录为 `127.0.0.1`。局域网用户访问时，使用日志应记录为对应的局域网来源 IP，例如 `10.0.90.x`。

## 验证

确认 Docker 只在 loopback 上发布 `new-api`：

```bash
docker inspect "$(docker compose ps -q "${COMPOSE_SERVICE:-new-api}")" --format '{{json .NetworkSettings.Ports}}'
```

期望看到 `HostIp` 为 `127.0.0.1`、`HostPort` 为 `13000`。

确认服务可访问：

```bash
curl -fsS http://127.0.0.1:3000/api/status
curl -fsS http://127.0.0.1:13000/api/status
```

确认日志 IP：

```bash
docker logs --tail 20 "$(docker compose ps -q "${COMPOSE_SERVICE:-new-api}")"
```

本机通过 `127.0.0.1:3000` 请求时，不应再出现 TUN 导致的异常公网 IP。局域网机器访问 `http://<Mac局域网IP>:3000` 后，日志中应出现该机器的局域网 IP。若服务日志中的异常 IP 不是本文示例值，可在 env 文件中配置 `NEW_API_ABNORMAL_TUN_IP_REGEX` 让脚本自动检查。

本模式已在隔离开发环境按 `3001 -> 13001` 端口组合验证，记录见 [CR-MACOS-DOCKER-TUN-LAN-2026-05-22.md](../reviews/CR-MACOS-DOCKER-TUN-LAN-2026-05-22.md)。

## 回滚

停止宿主机 Caddy，然后用默认端口发布重建：

```bash
kill "$(cat /tmp/new-api-macos-tun-caddy-3000.pid)"
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  NEW_API_PORT_MAPPING=3000:3000 \
  docker compose -f docker-compose.yml up -d --no-deps --force-recreate "${COMPOSE_SERVICE:-new-api}"
```

如果使用了自定义 `NEW_API_DEFAULT_PORT_MAPPING`，手动回滚时把上方 `3000:3000` 替换为相同值。不要在手动回滚命令中加载 `deploy/env/macos-docker-tun.env`，也不要沿用 shell 中残留的 `NEW_API_PORT_MAPPING=127.0.0.1:13000:3000`。

如果没有正式 `.env` 且依赖 shell 中显式导出的运行时变量回滚，手动回滚命令也应给 `docker compose` 增加 `--env-file /dev/null`，避免当前目录中后来出现的 `.env` 被隐式读取。

默认配置会回到 `0.0.0.0:3000:3000`。如果 TUN 仍开启，使用日志可能重新出现异常公网 IP。
