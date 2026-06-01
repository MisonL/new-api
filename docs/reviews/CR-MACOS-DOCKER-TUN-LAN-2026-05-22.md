# CR-MACOS-DOCKER-TUN-LAN-2026-05-22

## 范围

本记录验证 macOS Docker Desktop + TUN 代理场景下，使用宿主机 Caddy 反代和 Docker loopback 端口发布后，`new-api` 能否保留本机与局域网来源 IP。

本次只验证隔离开发环境，不触碰正式 `3000` 服务。

## 变更点

- `docker-compose.yml` 将 `new-api` 端口发布参数化为 `NEW_API_PORT_MAPPING`，默认保持 `3000:3000`。
- `deploy/env/macos-docker-tun.env.example` 提供 `127.0.0.1:13000:3000` 示例。
- `deploy/proxy/macos-docker-tun/Caddyfile` 由宿主机 Caddy 监听入口端口并转发到 Docker loopback 后端，且关闭 Caddy admin API。
- `scripts/deploy-macos-docker-tun.sh` 提供一次性实施脚本，避免 Codex 因重建 `new-api` 断联后无法继续操作；脚本只通过 PID 文件停止自己启动的专用 Caddy 进程。
- 脚本支持通过 env 文件配置 `COMPOSE_SERVICE`、`NEW_API_LAN_PROXY_PORT`、`NEW_API_LOOPBACK_PORT`、`NEW_API_CONTAINER_PORT`、`NEW_API_HEALTH_PATH`、`NEW_API_DEFAULT_PORT_MAPPING` 和 `NEW_API_ABNORMAL_TUN_IP_REGEX`，默认值保持适配本仓库。
- `docs/operations/macos-docker-tun-lan.md` 记录部署、验证与回滚步骤。

## 配置验证

默认 compose 渲染：

```bash
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  SESSION_SECRET=dummy CRYPTO_SECRET=dummy \
  NEW_API_PORT_MAPPING=3000:3000 \
  docker compose -f docker-compose.yml --env-file /dev/null config --format json \
  | jq '.services["new-api"].ports'
```

结果：

```json
[
  {
    "mode": "ingress",
    "target": 3000,
    "published": "3000",
    "protocol": "tcp"
  }
]
```

macOS TUN env 渲染：

```bash
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  SESSION_SECRET=dummy CRYPTO_SECRET=dummy \
  NEW_API_PORT_MAPPING=127.0.0.1:13000:3000 \
  docker compose -f docker-compose.yml --env-file /dev/null --env-file deploy/env/macos-docker-tun.env.example config --format json \
  | jq '.services["new-api"].ports'
```

结果：

```json
[
  {
    "mode": "ingress",
    "host_ip": "127.0.0.1",
    "target": 3000,
    "published": "13000",
    "protocol": "tcp"
  }
]
```

Caddyfile 验证：

```bash
NEW_API_LAN_PROXY_PORT=3001 NEW_API_LOOPBACK_PORT=13001 \
  caddy validate --config deploy/proxy/macos-docker-tun/Caddyfile
```

结果：`Valid configuration`。

实施脚本语法验证：

```bash
bash -n scripts/deploy-macos-docker-tun.sh
```

结果：退出码 `0`。

实施脚本预检：

```bash
cp deploy/env/macos-docker-tun.env.example /tmp/new-api-macos-docker-tun.env
TUN_ENV_FILE=/tmp/new-api-macos-docker-tun.env scripts/deploy-macos-docker-tun.sh --check-only
rm -f /tmp/new-api-macos-docker-tun.env
```

结果：`check-only passed`。

通用化脚本验证：

```bash
shellcheck scripts/deploy-macos-docker-tun.sh
bash -n scripts/deploy-macos-docker-tun.sh
TUN_ENV_FILE=deploy/env/macos-docker-tun.env.example scripts/deploy-macos-docker-tun.sh --check-only
```

结果：均退出码 `0`，`check-only passed`。脚本当前不再假设容器名固定为 `new-api`，而是通过 `docker compose ps -q "$COMPOSE_SERVICE"` 定位服务容器。

2026-05-22 复核时补充的部署门禁：

- `fail()` 与 `ERR` trap 都会在 Docker 端口切换开始后显式触发同一套回滚保护，避免显式 `exit 1` 绕过回滚。
- 回滚过程显式记录 Caddy 停止失败、默认 Docker published port 恢复失败和回滚后健康检查失败，不再依赖会被 `|| true` 掩盖的 `errexit` 语义。
- Compose 端口预检要求端口列表唯一且完全等于 `127.0.0.1:<loopback>:<container>`，防止额外保留 `3000:3000` 仍被误判通过。
- 默认回滚模式的 Compose 端口也会预检，防止 `NEW_API_DEFAULT_PORT_MAPPING` 被误配成 loopback 或其他端口。
- 端口值必须是 `1-65535`，且 LAN 入口端口不能等于 Docker loopback 后端端口。
- `SESSION_SECRET` 和 `CRYPTO_SECRET` 必须在 Compose 渲染结果中非空；正式 `--apply` 会拒绝 `dummy`、`changeme`、`random_string`、`your_session_secret_here` 等占位值。
- `SESSION_SECRET` 和 `CRYPTO_SECRET` 必须同时存在于 TUN 模式和默认回滚模式的渲染结果中，防止运行时变量只写在 TUN env 而导致回滚配置漂移。
- 正式 `--apply` 前会检查 LAN 入口端口占用归属：只允许当前 `COMPOSE_SERVICE` 或脚本 PID 文件管理的专用 Caddy 占用。
- 如果 `NEW_API_LOOPBACK_PORT` 已由当前 `COMPOSE_SERVICE` 的同一 loopback 映射占用，允许重跑 `--apply`；其他进程占用仍阻断。
- TUN env 文件只允许本模式端口、健康检查和异常 IP 检查参数，禁止混入密钥、数据库、镜像、数据目录等运行时变量。
- env 文件中的 `export KEY=...`、单引号和双引号包裹值都会被脚本解析时处理；引用值里的 `#` 会保留，未引用值里的空格加 `#` 才视为注释，避免脚本预检与 Compose 常见 env 写法不一致。
- TUN env 文件中的重复 key 会被拒绝，避免脚本读取一个值而人工以为另一个值生效。
- 启动和停止宿主机 Caddy 时不再只信任 PID 文件存在；必须确认 PID 对应进程仍存活、命令行匹配当前 Caddyfile，且实际监听 LAN 入口端口。启动失败时会输出脚本专用 Caddy log 的最近内容，避免等健康检查超时才暴露，也避免误停同一 Caddyfile 的其他端口实例。
- PID 文件必须只包含纯数字 PID，损坏内容不会被拼接成错误 PID。
- 默认回滚端口拒绝任意 loopback `host_ip` 变体和非法主机名，避免回滚后只绑定本机。
- Compose 配置渲染失败时由脚本直接失败，不再让后续 JSON 解析输出额外栈信息。
- 手动回滚文档显式覆盖 `NEW_API_PORT_MAPPING=3000:3000`，避免当前 shell 残留 TUN 映射时继续按 loopback 发布。
- 文档中的手动渲染、手动切换和手动回滚命令显式清除 `COMPOSE_FILE` / `COMPOSE_PROJECT_NAME` 并覆盖 `NEW_API_PORT_MAPPING`，避免 shell 中残留旧值覆盖 env 文件或切到错误 Compose project。
- 脚本内部显式使用 `-f docker-compose.yml`，并清除调用方 shell 的 `COMPOSE_FILE` / `COMPOSE_PROJECT_NAME`；如确需指定 project name，只能通过 `NEW_API_COMPOSE_PROJECT_NAME` 显式开启。
- 当 `PROD_ENV_FILE` 不存在时，脚本会同时给 TUN 模式和默认回滚模式传入 `--env-file /dev/null`，禁止 Docker Compose 隐式读取当前目录 `.env`。
- 无正式 env 文件时，脚本仍允许调用方 shell 中显式导出的 `SESSION_SECRET`、`CRYPTO_SECRET` 和其他运行时变量参与 Compose 渲染。

补充验证：

```bash
shellcheck scripts/deploy-macos-docker-tun.sh
bash -n scripts/deploy-macos-docker-tun.sh
SESSION_SECRET=dummy CRYPTO_SECRET=dummy PROD_ENV_FILE=/dev/null TUN_ENV_FILE=deploy/env/macos-docker-tun.env.example scripts/deploy-macos-docker-tun.sh --check-only
TUN_ENV_FILE=deploy/env/macos-docker-tun.env.example scripts/deploy-macos-docker-tun.sh --check-only
```

结果：均退出码 `0`。

正式 env 缺失时禁用隐式 `.env` 的回归验证：

```bash
cd /tmp/new-api-compose-env-test
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME docker compose -f docker-compose.yml config --format json
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME docker compose -f docker-compose.yml --env-file /dev/null config --format json

PROD_ENV_FILE=/tmp/new-api-compose-env-test/missing.env \
  TUN_ENV_FILE=/tmp/new-api-compose-env-test/tun.env \
  COMPOSE_FILE_PATH=/tmp/new-api-compose-env-test/docker-compose.yml \
  scripts/deploy-macos-docker-tun.sh --check-only
```

结果：第一个 Compose 渲染能看到临时夹具 `.env` 中的非空密钥；加 `--env-file /dev/null` 后密钥为空。脚本预检退出码 `1`，报错为 `SESSION_SECRET and CRYPTO_SECRET must be set for TUN deployment`，确认没有隐式读取临时夹具 `.env`。另以 `SESSION_SECRET=dummy CRYPTO_SECRET=dummy PROD_ENV_FILE=/tmp/new-api-missing-prod.env TUN_ENV_FILE=deploy/env/macos-docker-tun.env.example scripts/deploy-macos-docker-tun.sh --check-only` 验证显式 shell 变量路径，退出码 `0`。

## 开发环境实测

临时将隔离开发环境的 `new-api` 容器切到 loopback 后端端口：

```text
127.0.0.1:13001 -> container:3000
```

宿主机 Caddy 使用同一份配置，覆盖端口变量：

```bash
NEW_API_LAN_PROXY_PORT=3001 NEW_API_LOOPBACK_PORT=13001 \
  caddy run --config deploy/proxy/macos-docker-tun/Caddyfile
```

健康检查：

```bash
curl -fsS http://127.0.0.1:3001/api/status
curl -fsS http://10.0.90.200:3001/api/status
curl -fsS http://127.0.0.1:13001/api/status
```

结果均返回 `success=true`。

容器日志中的来源 IP：

```text
127.0.0.1:3001 -> 127.0.0.1
10.0.90.200:3001 -> 10.0.90.200
127.0.0.1:13001 -> 172.21.0.1
```

通过 Caddy 入口访问时未再出现异常公网 IP `171.80.0.108`。

## 转发头复核

使用临时本地后端验证 Caddy 反代头行为。即使请求入口手工传入：

```text
X-Forwarded-For: 8.8.8.8
X-Real-IP: 9.9.9.9
```

后端收到的仍是：

```text
X-Forwarded-For: 127.0.0.1
X-Real-IP: 127.0.0.1
```

说明当前 Caddy 配置不会透传客户端伪造的来源 IP 头。

## 恢复状态

测试完成后已将隔离开发环境恢复为原状态：

```text
0.0.0.0:3001 -> container:3000
```

恢复后 `http://127.0.0.1:3001/api/status` 正常返回 `success=true`。由于恢复为 Docker Desktop published port 直连模式，TUN 开启时日志仍可能再次出现 `171.80.0.108`，这与本次修复方案的预期一致。

## 实施结论

该方案适合正式环境按 `3000 -> 13000` 端口组合实施。实施时必须保证入口 Caddy 是 macOS 宿主机进程，不能放入 Docker Compose 容器。

正式切换会短暂中断依赖 `http://localhost:3000/v1` 的 Codex 会话，因此不应由当前 Codex 会话逐条执行。应由用户在 macOS 宿主机终端运行：

```bash
scripts/deploy-macos-docker-tun.sh --apply
```
