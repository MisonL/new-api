# macOS Docker Desktop TUN 局域网部署模式

维护窗口的目标、备份、停机、回滚和 blocker 矩阵见
[`docs/operations/macos-docker-tun-maintenance-runbook.md`](macos-docker-tun-maintenance-runbook.md)。

## 适用范围

本模式只用于 macOS Docker Desktop 主机，同时满足以下条件：

- 主机运行 FlClash、Clash 或类似 TUN 代理。
- 旧的直连/回滚模式可以通过 Docker published port 使用 `0.0.0.0:3000`；本模式实施后，Docker 发布 `13000`（默认绑定 `127.0.0.1`，可用 `NEW_API_BIND_ADDRESS` 放宽为 `0.0.0.0`），由宿主机 Caddy 对外提供 `3000`。
- 本机访问 `http://localhost:3000` 时，`new-api` 使用日志记录到异常公网 IP。
- 仍需要局域网用户访问 `http://<Mac局域网IP>:3000`。

Linux 原生 Docker 通常不需要本模式。Windows Docker Desktop 是否需要，应先实测客户端 IP 记录是否异常。

## 设计

不要把入口反向代理放进 Docker Compose。这个问题发生在 Docker Desktop published port 入口路径上；如果 Caddy 也作为容器暴露 `0.0.0.0:3000`，Caddy 自己看到的客户端 IP 可能已经是错误值。

默认链路：

```text
本机 Codex 或局域网用户
-> macOS 宿主机 Caddy 0.0.0.0:3000
-> Docker 后端 <NEW_API_BIND_ADDRESS>:13000
-> new-api 容器 3000
```

在这个链路中：

- Docker 发布 `<NEW_API_BIND_ADDRESS>:13000:3000`，默认 `127.0.0.1`。
  当 `NEW_API_BIND_ADDRESS=0.0.0.0` 时，局域网也可以直接访问 `13000`，绕过 Caddy；
  此时客户端 IP 记录走 Docker published port 路径，`X-Real-IP` 由 Docker 提供而不是 Caddy，
  异常公网 IP 问题会重新出现。需要准确客户端 IP 时保持 loopback 绑定并只经 Caddy 访问。
- macOS 宿主机 Caddy 监听 `0.0.0.0:3000`。
- 本模式的 Caddyfile 关闭 Caddy admin API；脚本通过用户级 launchd agent 和 PID 文件管理自己启动的专用 Caddy 进程，避免重载或停止机器上可能已有的 Caddy 服务。
- Caddy 显式覆盖 `X-Real-IP`；`X-Forwarded-For` 和 `X-Forwarded-Proto` 由 Caddy 默认生成，不要改成透传客户端原始同名头。
- Caddy 转发给后端的 `Host` 必须保留入口端口，例如 `127.0.0.1:3000` 或 `localhost:3000`。Passkey、OAuth/CAS 回调和 `/api/status` 中的 `server_address` 都依赖精确的 scheme、host 和 port；如果代理只传 `127.0.0.1` 或 `localhost`，浏览器实际 origin 与后端校验 origin 会不一致。

默认参数适配本仓库的 Compose service `new-api`、容器端口 `3000` 和健康路径 `/api/status`。其他 Mac 用户如果 fork 后改过 service 名、容器端口或健康路径，只需要修改 `deploy/env/macos-docker-tun.env` 中的对应变量，不需要改脚本。

## 部署步骤

### 通用安装流程

大多数 Mac 用户只需要走脚本流程，不需要手动拆分 Docker 和 Caddy 步骤。

1. 安装并确认依赖。

```bash
brew install caddy
command -v docker
command -v caddy
command -v launchctl
command -v python3
command -v lsof
command -v curl
```

2. 准备本模式配置文件。

```bash
cp deploy/env/macos-docker-tun.env.example deploy/env/macos-docker-tun.env
```

3. 如果仓库默认端口和 service 名没有改动，直接预检。

```bash
scripts/deploy-macos-docker-tun.sh --check-only
```

预检会把当前正在运行的正式容器作为镜像基线。如果正式 `.env` 的 `NEW_API_IMAGE` 已漂移，先在命令行临时指定已核对的稳定镜像引用再检查，例如：

```bash
POSTGRES_PASSWORD='<verified-value>' REDIS_PASSWORD='<verified-value>' \
NEW_API_IMAGE='<current-production-image>' \
  scripts/deploy-macos-docker-tun.sh --check-only
```

该命令只读；不要把示例值写回仓库或正式 env 文件。预检若提示缺少正式密码或镜像 digest，必须先走配置/发布门禁处理，不能绕过检查直接执行 `--apply`。

4. 如果预检通过，再正式实施。

```bash
scripts/deploy-macos-docker-tun.sh --apply
```

脚本会自动完成 Docker loopback 端口切换、host-side Caddy launchd agent 启动、健康检查、日志 IP 检查和失败回滚。不要先手动改 Docker 端口再运行脚本。

### 参数决策表

只在实际环境和仓库默认值不一致时修改 `deploy/env/macos-docker-tun.env`。

| 场景 | 修改项 |
| ---- | ------ |
| Compose service 名不是 `new-api` | `COMPOSE_SERVICE` |
| 对外访问端口不是 `3000` | `NEW_API_LAN_PROXY_PORT`、`NEW_API_DEFAULT_PORT_MAPPING`、`NEW_API_PORT_MAPPING` |
| Caddy 运行配置需要固定到用户私有路径 | `CADDY_RUNTIME_CONFIG_FILE` |
| Docker loopback 端口 `13000` 已被占用 | `NEW_API_LOOPBACK_PORT`、`NEW_API_PORT_MAPPING` |
| 容器内服务端口不是 `3000` | `NEW_API_CONTAINER_PORT`、`NEW_API_DEFAULT_PORT_MAPPING`、`NEW_API_PORT_MAPPING` |
| 健康检查路径不是 `/api/status` | `NEW_API_HEALTH_PATH` |
| 当前 `3000` 有长连接但可以接受短暂中断 | `NEW_API_ALLOW_ACTIVE_REQUESTS=1` |
| 希望等待连接更久或立即失败 | `NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT` |
| 已知异常 TUN IP 需要自动拦截 | `NEW_API_ABNORMAL_TUN_IP_REGEX` |
| 同一 macOS 用户下运行多个相同 service/端口实例 | `CADDY_LAUNCHD_LABEL`、`CADDY_LAUNCHD_PLIST` |

端口相关变量必须保持一致。例如把对外端口改为 `3001`、loopback 改为 `13001`、容器端口仍是 `3000` 时：

```bash
NEW_API_LAN_PROXY_PORT=3001
NEW_API_LOOPBACK_PORT=13001
NEW_API_CONTAINER_PORT=3000
NEW_API_DEFAULT_PORT_MAPPING=3001:3000
NEW_API_PORT_MAPPING=127.0.0.1:13001:3000
```

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
command -v launchctl
command -v python3
command -v lsof
caddy validate --config deploy/proxy/macos-docker-tun/Caddyfile
```

本模式不使用 Caddy admin API，因此不会调用 `caddy reload` 或 `caddy stop` 影响宿主机已有 Caddy 服务。

## 主机重启后的自动恢复

Compose 的 `restart: always` 负责在 Docker Desktop 恢复后拉起已有容器，Caddy 的 launchd 作业负责保持入口进程运行。为了覆盖 Docker Desktop 启动顺序和 Caddy 后端暂时不可用的窗口，可以安装用户级恢复看门狗：

```bash
scripts/install-macos-docker-tun-recovery.sh --check-only
scripts/install-macos-docker-tun-recovery.sh --apply
```

`--check-only` 只渲染并校验 LaunchAgent plist，不写入用户目录，也不调用 `launchctl`；只有显式 `--apply` 才会安装并 kickstart recovery job。无参数调用保留为兼容模式，等同于 `--apply`。

看门狗每 30 秒检查已有的 `postgres`、`redis`、`new-api` 容器，以及 `127.0.0.1:13000` 和 `127.0.0.1:3000`。如果 Docker Desktop 应用没有运行，会尝试在后台打开一次；如果 Docker daemon 已可用，它只会启动已存在但停止的容器。它不会执行 `docker build`、`docker compose up`、容器重建、`pg_resetwal`、`REINDEX` 或删除数据。Redis 探针必须得到精确 `PONG`，PostgreSQL 探针必须用实际账号执行 `SELECT 1`；看门狗从受保护的 `NEW_API_RECOVERY_ENV_FILE`、密码文件或环境变量读取凭据，缺少凭据时明确判定为未就绪。后端健康而 Caddy 入口失败时，连续失败达到 `NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD` 后才重启本模式对应的 Caddy launchd 作业；旧的 `NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD` 仅作为未设置专用阈值时的兼容别名，并对失败操作实施指数退避和最长冷却。

正式 recovery LaunchAgent 还可检查局域网入口。通过 `NEW_API_RECOVERY_LAN_URL` 指定实际局域网地址；未设置时 LAN 探针明确禁用。LAN-only 失败会记录独立的 `lan-failed` 状态并等待下一轮，不会因为网卡、路由或防火墙问题盲目重启 Caddy。只有 backend 或本机 public 入口失败才进入对应的容器/Caddy 恢复路径。

Docker daemon 就绪阶段使用 Unix socket 的 HTTP `_ping`，要求响应正文精确为 `OK`，不再调用可能在宿主机高负载时超过命令窗口的 `docker info`。默认按 `/var/run/docker.sock` 和 `$HOME/.docker/run/docker.sock` 自动发现；特殊 Docker 上下文可通过受保护的 `NEW_API_RECOVERY_DOCKER_SOCKET` 显式指定。`_ping` 失败只记录 `docker-daemon-unavailable` 并等待下一轮，仍受 `FORCE_DOCKER_RESTART=0` 安全默认和失败冷却约束。

Caddy 公共入口恢复时，watchdog 先通过 `launchctl kill SIGTERM` 请求当前 Caddy 进程优雅退出，并读取 launchd 的 PID 变化，在最多 30 秒内等待旧进程退出或被新进程接管。只有 SIGTERM 失败或超时才使用 `launchctl kickstart -k` fallback；两条路径都会记录结果并继续经过 `3000` 健康探针确认。恢复 LaunchAgent 的 `ExitTimeOut` 为 45 秒，以覆盖 Caddy 30 秒 drain 窗口；该等待不会调用 Docker 或重建容器。可通过以下变量调整测试或维护窗口行为：

- `NEW_API_RECOVERY_CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS`：单次 `launchctl` 命令上限，默认 `5` 秒。
- `NEW_API_RECOVERY_CADDY_GRACEFUL_TIMEOUT_SECONDS`：等待旧 PID 消失或变更的上限，默认 `30` 秒。
- `NEW_API_RECOVERY_CADDY_GRACEFUL_POLL_INTERVAL_SECONDS`：PID 轮询间隔，默认 `1` 秒。

恢复看门狗在每个 recovery 周期早期检查 Caddy 活动日志，默认超过 `50 MiB` 时采用 copy-truncate 方式归档为 `.1`，最多保留 `5` 份并把活动文件保持为 `0600`，不会重启 Caddy 或改变文件描述符。可通过 `NEW_API_RECOVERY_CADDY_LOG_ROTATION_MAX_BYTES` 和 `NEW_API_RECOVERY_CADDY_LOG_ROTATION_KEEP` 调整；设为 `0` 可显式关闭大小触发。轮转失败会写入恢复日志，但不会把后端标记为不可用或触发额外重启。

恢复 LaunchAgent 自身的 `recovery.log` 也按大小执行 copy-truncate，默认阈值 `10 MiB`、保留 `3` 份，使用 `NEW_API_RECOVERY_LOG_ROTATION_MAX_BYTES` 和 `NEW_API_RECOVERY_LOG_ROTATION_KEEP` 调整，设为 `0` 可关闭。轮转保持当前日志 inode，不重启 LaunchAgent；活动文件和归档均保持 `0600`。

安装后可这样检查：

```bash
launchctl print gui/$(id -u)/com.new-api.macos-tun.new-api.recovery
tail -n 50 "$HOME/Library/Application Support/New API/recovery/recovery.log"
stat -f '%Sp %N' "$HOME/Library/LaunchAgents/com.new-api.macos-tun.new-api.recovery.plist" \
  "$HOME/Library/Application Support/New API/recovery/recovery.log"
```

若 PostgreSQL 数据文件本身损坏，看门狗会保留失败状态并记录日志，等待人工使用独立备份恢复；不会为了“自动可用”执行不可逆的数据修复。安装器将 LaunchAgent plist 和恢复日志收紧为当前用户可读写，状态文件位于同一恢复目录。

根 Compose 和隔离 Compose 的 Redis healthcheck 都必须使用 `REDISCLI_AUTH` 认证后的精确 `PONG`，不得把密码放进 `redis-cli -a` 参数；PostgreSQL healthcheck 都必须执行 `SELECT 1`。这些修改只影响未来创建或重建的容器；现有正式容器的 health 状态不会被看门狗自动改变，正式维护前必须在隔离环境渲染和验证 Compose 配置。

### 外部只读监控

需要接入 Uptime、Nagios、脚本调度或其他告警系统时，调用仓库中的只读监控包装脚本，不要直接把 recovery LaunchAgent 当作外部监控命令：

```bash
NEW_API_RECOVERY_ENV_FILE=<production-env-file> \
  scripts/macos-docker-tun-monitor.sh
```

该脚本强制 `NEW_API_RECOVERY_DRY_RUN=1`、`NEW_API_RECOVERY_MONITOR_ONLY=1` 和 `NEW_API_RECOVERY_FORCE_DOCKER_RESTART=0`，使用独立锁目录。它检查 Docker `_ping`、已有容器状态、new-api Docker health、Redis 认证 `PONG`、PostgreSQL `SELECT 1`、本机后端、Caddy 和可选 LAN HTTP 入口，并检查 `NEW_API_MONITOR_DISK_PATHS` 配置的卷容量、`sysctl -n vm.swapusage` 的 swap 使用率、`sysctl -n vm.loadavg` 按 CPU 归一化的 1 分钟 load，以及 Caddy/recovery 活动日志和显式配置的正式应用日志目录大小。默认磁盘阈值为 `90%`、swap 阈值为 `95%`、Caddy/recovery/应用日志 hard limit 为 `5 GiB`、日志 warning 为 `4 GiB`、load warning 为每 CPU `2`、hard fail 为每 CPU `4`；可通过 `NEW_API_MONITOR_*` 变量调整。warning 输出 `monitor_status=warn` 且保持退出码 `0`，硬阈值或依赖失败输出 `monitor_status=fail` 并返回非零。默认不会执行 Docker start/restart、Caddy kill/kickstart、日志截断、状态持久化或数据库写操作。

如果外部调度器需要读取稳定文件而不是解析标准输出，可显式设置 `NEW_API_MONITOR_STATUS_FILE`，并将它放在当前用户拥有的私有目录中，例如 `$HOME/Library/Application Support/New API/recovery/monitor.status`。监控会以临时文件加原子替换方式写入 `schema_version`、UTC 检查时间、`monitor_status`、退出码、watchdog 退出码和资源失败原因；目录权限为 `0700`，状态文件为 `0600`。不要直接把状态文件放在共享目录（例如 `/tmp`），因为脚本无法安全地将共享父目录收紧到 `0700` 时会 fail-closed。该变量默认为空，因此不会改变现有调用方行为；状态文件只用于告警读取，不会触发恢复动作，也不包含 URL、凭据或请求内容。

当 `NEW_API_MONITOR_STATUS_FILE` 启用时，monitor 会同时维护同目录下的私有 `monitor.status.probes` 快照，并把以下脱敏字段合并到主状态文件：`probe_docker_daemon`、`probe_postgres`、`probe_redis`、`probe_backend`、`probe_public`、`probe_lan`、`probe_app_health`、`probe_failure_reason` 和 `probe_action`。这些字段来自 watchdog 的真实 Docker `_ping`、PostgreSQL `SELECT 1`、Redis 认证 `PONG` 与三入口探针；只包含状态和值，不包含密码、URL、请求头或 payload。monitor 会校验 `probe_checked_at_utc`，默认只接受最近 `600` 秒内的合法 UTC 时间戳；快照缺失、非法、来自未来或过期都会把 monitor 标记为失败，并清空旧探针字段，避免锁竞争时继续沿用上一次健康结果。可通过 `NEW_API_MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS` 调整正整数上限。外部告警应优先按 `monitor_status`、资源原因和 `probe_*` 字段分类，避免把宿主资源 hard fail 与业务入口失败混为一类。

如需把状态发送到受信任的外部告警端点，使用独立的 `scripts/macos-docker-tun-monitor-alert.py`，不要把外发动作放进 recovery 或 monitor LaunchAgent。端点未配置时脚本明确返回 `alert_status=disabled`；`--dry-run` 只输出脱敏 JSON，不发网络请求。正式接入前先执行：

```bash
python3 scripts/macos-docker-tun-monitor-alert.py \
  --status-file "$HOME/Library/Application Support/New API/monitor/monitor.status" \
  --dry-run --pretty
```

确认摘要无敏感字段后，再由外部调度器显式设置 `NEW_API_MONITOR_ALERT_URL=https://monitor.example.invalid/hook` 和 `NEW_API_MONITOR_ALERT_BEARER` 环境变量运行。脚本只允许 HTTP(S)，拒绝 URL 内嵌凭据，默认请求超时 `5s`，只发送状态、资源原因和探针白名单字段；资源原因中的宿主路径和 URL 会替换为 `<redacted>`，端点去重、静默和告警升级由外部系统负责。

监控同时读取 vm.loadavg 和 hw.ncpu，按 CPU 归一化 1 分钟 load；默认每 CPU 2 为 warning、每 CPU 4 为 hard fail。warning 返回 monitor_status=warn 且退出码为 0，hard fail 或依赖失败返回非零。该项只读，不会触发恢复动作。

如果没有现成的 Uptime 或 Nagios 调度器，可以选择安装用户级只读 monitor LaunchAgent。安装器默认只做检查，不写入 LaunchAgents 或 Application Support；只有明确传入 `--apply` 才会安装。它与 30 秒 recovery LaunchAgent 分离，默认登录时执行一次、之后每 `300` 秒执行一次，并设置低优先级 I/O、`Nice=10`，以免在宿主机高压时增加扫描负担：

```bash
scripts/install-macos-docker-tun-monitor.sh --check-only
scripts/install-macos-docker-tun-monitor.sh --apply
launchctl print gui/$(id -u)/com.new-api.macos-tun.new-api.monitor
```

安装器只把 monitor 脚本和 plist 写入当前用户私有目录，目录权限为 `0700`，脚本为 `0755`，plist、状态和日志为 `0600`。plist 固定 `NEW_API_RECOVERY_DRY_RUN=1`、`NEW_API_RECOVERY_MONITOR_ONLY=1` 和 `NEW_API_RECOVERY_FORCE_DOCKER_RESTART=0`；monitor 异常只更新退出码和状态文件，不会重启 Docker/Caddy、停止容器、截断日志或写入数据库。安装前必须确认 recovery watchdog 已存在且与已验收副本一致；安装失败会尝试恢复此前的脚本、plist 和任务状态。

周期型 LaunchAgent 在两次调度之间显示 `state = not running` 或 `active count = 0` 属于正常空闲状态，不等于任务未加载。应同时确认 `launchctl list` 能看到 label、`launchctl print` 的 `run interval`/`runs` 已存在，并以 `last exit code` 和私有 `monitor.status` 判断最近一轮结果；recovery 的正常最近退出码为 `0`，monitor 在宿主资源超过 hard limit 时可以为非零，但不应因此执行恢复动作。

### Time Machine 只读报告

需要把备份复制阶段和项目路径 Included/Excluded 状态纳入外部监控时，调用 scripts/macos-docker-tun-time-machine-report.py --pretty。

脚本对 tmutil status 和每个 isexcluded 查询都使用默认 5 秒超时；只输出阶段、进度、字节/文件计数和 logs/data/repo 等标签，不输出目标 URL、目的地挂载点或原始路径。报告通过 tm_report_status=pass 表示命令和字段完整，不代表备份可恢复；恢复可用性仍需独立逻辑备份和隔离恢复演练。

### 公开端口审计

端口收敛前先运行只读审计，确认当前所有运行容器的 published ports 和 Compose 归属：

```bash
python3 scripts/macos-docker-tun-port-audit.py --pretty
```

该命令只读取 `docker ps` 和 `docker inspect`，只输出容器名、镜像、状态、健康状态、重启次数、Compose project/service/working_dir/config_files 以及端口绑定范围，不输出环境变量、密码或挂载内容。`public` 表示绑定到 `0.0.0.0` 或 `::`，`specific` 表示绑定到指定地址，`loopback` 才是本机专用。

正式入口的允许状态：Caddy 监听 `0.0.0.0:3000` 供本机和 LAN 使用；new-api 绑定 `NEW_API_BIND_ADDRESS:13000`，默认为 `127.0.0.1`。把 `NEW_API_BIND_ADDRESS` 设为 `0.0.0.0` 是受支持的配置，用于让局域网直接访问 `13000`，此时 `13000` 与 `3000` 同时对外提供入口。其余公共绑定必须逐项确认消费者、LAN 需求、Compose 配置和工作目录、env/data 归属、活跃连接、备份和回滚路径后，才能在维护窗口收敛为 loopback 或移除：

| 端口 | 当前归属 | 处理边界 |
| ---- | -------- | -------- |
| `<published-port>` | 非正式或隔离服务 | 先确认 Compose project、消费者和数据目录 |
| `<callback-port>` | 回调或测试代理 | 先确认回调来源、替代入口和回滚路径 |
| `<database-port>` | 隔离 PostgreSQL/Redis | 先确认隔离环境仍被使用，不能单独停止依赖 |

当前没有把“没有观察到已建立连接”当作可以停止的依据。收敛前必须再次用有界连接检查确认，并保留逐端口回滚记录；禁止直接停止未知容器、全局修改防火墙或使用全局 `prune`。正式 Caddy 3000 的 LAN 访问不能因非正式端口治理而中断。

确认 compose 默认模式和 macOS TUN 模式渲染符合预期：

```bash
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  SESSION_SECRET=dummy CRYPTO_SECRET=dummy \
  SQL_DSN=postgresql://root:contract-postgres-password@postgres:5432/new-api \
  POSTGRES_PASSWORD=contract-postgres-password \
  REDIS_CONN_STRING=redis://:contract-redis-password@redis:6379/0 \
  REDIS_PASSWORD=contract-redis-password \
  NEW_API_PORT_MAPPING=3000:3000 \
  docker compose -f docker-compose.yml --env-file /dev/null config --format json \
  | jq --arg service "${COMPOSE_SERVICE:-new-api}" '.services[$service].ports'

env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  SESSION_SECRET=dummy CRYPTO_SECRET=dummy \
  SQL_DSN=postgresql://root:contract-postgres-password@postgres:5432/new-api \
  POSTGRES_PASSWORD=contract-postgres-password \
  REDIS_CONN_STRING=redis://:contract-redis-password@redis:6379/0 \
  REDIS_PASSWORD=contract-redis-password \
  NEW_API_PORT_MAPPING=127.0.0.1:13000:3000 \
  docker compose -f docker-compose.yml --env-file /dev/null --env-file deploy/env/macos-docker-tun.env.example config --format json \
  | jq --arg service "${COMPOSE_SERVICE:-new-api}" '.services[$service].ports'
```

默认模式应渲染为 `3000:3000`；macOS TUN 模式应渲染为 `127.0.0.1:13000:3000`。

直接运行脚本预检：

```bash
scripts/deploy-macos-docker-tun.sh --check-only
```

脚本预检会校验 Compose 渲染后的 `SESSION_SECRET`、`CRYPTO_SECRET`、`SQL_DSN`、`REDIS_CONN_STRING`、`POSTGRES_PASSWORD` 和 `REDIS_PASSWORD` 非空。若只是做无正式 `.env` 的配置渲染验证，请同时提供上方命令中的临时数据库和 Redis 变量；正式 `--apply` 会拒绝占位值。

如果 `PROD_ENV_FILE` 指向的正式 env 文件不存在，脚本会同时给 TUN 模式和默认回滚模式传入 `--env-file /dev/null`，显式禁止 Docker Compose 隐式读取当前目录 `.env`。这种情况下，只有调用方 shell 中显式导出的变量和 `deploy/env/macos-docker-tun.env` 白名单变量会参与渲染。

`--check-only` 也会检查 LAN 入口端口上的已建立连接，并打印最多 20 条连接样本。若仍有连接且 `NEW_API_ALLOW_ACTIVE_REQUESTS=0`，正式 `--apply` 会先等待 `NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT` 秒；超时后仍有连接才拒绝继续。若设置为 `NEW_API_ALLOW_ACTIVE_REQUESTS=1`，正式实施会中断这些连接。

`NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT=0` 表示不等待连接排空；如果同时保持 `NEW_API_ALLOW_ACTIVE_REQUESTS=0`，正式 `--apply` 会在发现活跃连接后立即拒绝继续。

宿主机 Caddy 会显式重写 `X-Real-IP` 为当前连接的远端地址；`X-Forwarded-For` 使用 Caddy `reverse_proxy` 默认生成值，避免客户端自带 `X-Forwarded-For` 伪造记录 IP。

宿主机 Caddy 还会显式把上游 `Host` 设置为 `{hostport}`，保留入口端口。不要改成 `{host}`；Caddy 的 `{host}` 不包含端口，会导致 `http://127.0.0.1:3000` 被后端识别为 `http://127.0.0.1`，从而破坏 Passkey origin 校验和依赖回调地址精确匹配的认证链路。

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
CADDY_LAUNCHD_LABEL=com.new-api.macos-tun.new-api.3000
CADDY_LAUNCHD_PLIST=~/Library/LaunchAgents/com.new-api.macos-tun.new-api.3000.plist
```

其中 `NEW_API_PORT_MAPPING` 必须与 `NEW_API_BIND_ADDRESS`、`NEW_API_LOOPBACK_PORT` 和 `NEW_API_CONTAINER_PORT` 保持一致。脚本会在预检阶段校验这些值是否匹配，并要求 Compose 渲染出的端口列表唯一且完全等于期望映射。`NEW_API_BIND_ADDRESS` 未显式设置时，脚本从 `NEW_API_PORT_MAPPING` 的主机部分推导，因此把映射写成 `0.0.0.0:13000:3000` 即可让后端对局域网发布。`NEW_API_DEFAULT_PORT_MAPPING` 用于回滚，通常保持为原来的直接发布入口。

正式部署脚本还会执行镜像 provenance 门禁。它读取现有 Compose service 的镜像引用、容器 image digest 和 OCI revision，并要求 TUN 与回滚两条 Compose 配置解析到同一个本地 digest。正式 `--apply` 遇到任何 tracked 或 untracked dirty worktree 都会直接拒绝，即使镜像引用和 digest 与正在运行的正式容器相同；这样 Compose、env、挂载、healthcheck、Caddyfile 和脚本改动都不会被同镜像重建顺带应用。`--check-only` 可以在 dirty worktree 上完成只读预检，但不会授权正式 apply。若镜像、digest 或 revision 不一致，默认也直接拒绝，且在拒绝前不会执行 `compose up`、Caddy 启停或端口切换。镜像 guard 变量只从调用方环境读取，不放入 `deploy/env/macos-docker-tun.env`，避免拓扑文件承担发布身份。

`NEW_API_ALLOW_IMAGE_CHANGE` 默认必须保持 `0`。只有经过独立发布验收、工作区干净、候选镜像带 40 位 OCI revision，并提供当前用户拥有的 release approval 文件时，才允许显式设为 `1`。approval 文件是纯文本键值，必须同时包含以下四项，且值与候选镜像完全一致：

```text
status=approved
image=<candidate-image-reference>
revision=<40-character-commit>
worktree=clean
```

正式 `.env` 缺少 `POSTGRES_PASSWORD` 或 `REDIS_PASSWORD` 时，脚本会在 Compose 渲染阶段 fail-closed；不得用占位值执行 `--apply`。只读检查可在命令行临时提供测试值，但不要把它们写入仓库或正式配置。

`CADDY_LAUNCHD_LABEL` 和 `CADDY_LAUNCHD_PLIST` 通常不需要修改。若同一个 macOS 用户下运行多个 new-api 实例，且 service 名或 LAN 端口相同，应为每个实例配置不同的 label 和 plist 路径，避免 launchd agent 冲突。

`deploy/env/macos-docker-tun.env` 只能保存上方列出的本模式参数。不要把 `SESSION_SECRET`、`CRYPTO_SECRET`、`SQL_DSN`、`REDIS_CONN_STRING`、`POSTGRES_PASSWORD`、`REDIS_PASSWORD`、镜像名、数据目录或日志目录写入这个文件；这些运行时配置必须继续放在正式 `.env` 或宿主机环境中。根 Compose 不再提供 `123456` 默认数据库/Redis 密码，未来执行 `docker compose config` 或部署预检前，正式 `.env` 必须显式提供 `SQL_DSN`、`REDIS_CONN_STRING`、`POSTGRES_PASSWORD` 和 `REDIS_PASSWORD`。缺少其中任一项会 fail-closed；这只阻止未来创建/重建，不改变当前旧容器。脚本会拒绝 TUN env 中的非白名单变量，避免主路径和回滚路径使用不同配置来源。Caddy 日志过滤会删除请求 headers，并用 RE2 规则移除 URI query，保留路径用于问题定位；Caddy runtime warning 和访问日志均写入同一受限文件，不能把未脱敏的原始请求头写入日志。

### 正式实施

推荐使用一次性脚本实施：

```bash
cp deploy/env/macos-docker-tun.env.example deploy/env/macos-docker-tun.env
scripts/deploy-macos-docker-tun.sh --apply
```

脚本会按顺序执行：

- 校验 Caddyfile 和 compose 端口映射。
- 校验端口值合法，绑定地址属于 `127.0.0.1`、`0.0.0.0`、`::1`、`::` 之一，且 LAN 入口端口与 Docker 后端端口不同。
- 校验 TUN 模式和默认回滚模式的 Compose 端口映射。
- 校验 `SESSION_SECRET` 和 `CRYPTO_SECRET` 在 TUN 模式和默认回滚模式中都已设置，正式实施时拒绝明显占位值。
- 校验 LAN 入口端口没有被非当前服务、非脚本管理 Caddy 的进程占用；校验 Docker 后端端口空闲，或已由当前服务在相同绑定地址上的映射占用。
- 默认等待 LAN 入口端口的已建立连接排空，超时后仍拒绝重建 `new-api` 并输出连接样本；若接受中断活跃请求，可显式设置 `NEW_API_ALLOW_ACTIVE_REQUESTS=1`。
- 重建 `COMPOSE_SERVICE` 指定的服务到 `<NEW_API_BIND_ADDRESS>:13000:3000`。
- 写入用户级 launchd plist，并通过 launchd 启动宿主机 Caddy 接管 `3000`。脚本会在 Caddyfile 校验通过后复制到用户私有的 `CADDY_RUNTIME_CONFIG_FILE`，plist 使用该固定副本，不依赖 dirty worktree 的相对 `WorkingDirectory`。
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

确认 `new-api` 已按期望的绑定地址发布 `13000` 后，再执行：

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

需要后台运行时，优先使用 `scripts/deploy-macos-docker-tun.sh --apply`，让脚本创建用户级 launchd agent，并通过 `/tmp/<COMPOSE_SERVICE>-macos-tun-caddy-<LAN_PORT>.pid` 记录专用 Caddy 进程。默认 label 是 `com.new-api.macos-tun.new-api.3000`，默认 plist 是 `~/Library/LaunchAgents/com.new-api.macos-tun.new-api.3000.plist`，默认 PID 文件是 `/tmp/new-api-macos-tun-caddy-3000.pid`，默认 Caddy 日志文件是 `~/Library/Application Support/New API/caddy/new-api-macos-tun-caddy-3000.log`，默认运行配置副本是 `~/Library/Application Support/New API/caddy/new-api-macos-tun.Caddyfile`。脚本会在校验 Caddyfile 后复制该副本，使用绝对路径并从生成的 plist 中移除 `WorkingDirectory` 依赖；脚本会将 Caddy 日志和运行配置设为 `0600`，目录设为 `0700`；自定义 `CADDY_LOG_FILE` 或 `CADDY_RUNTIME_CONFIG_FILE` 时也必须使用用户私有绝对路径并保留相同权限。不要覆盖 `/usr/local/etc/Caddyfile` 或重启已有 Homebrew Caddy 服务，除非已确认这台机器没有其他 Caddy 业务。

停止本模式的专用 Caddy 时，先卸载对应 launchd agent，再按 PID 文件确认进程已退出：

```bash
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.new-api.macos-tun.new-api.3000.plist
test ! -f /tmp/new-api-macos-tun-caddy-3000.pid || kill "$(cat /tmp/new-api-macos-tun-caddy-3000.pid)"
```

## 访问方式

- 本机 Codex 推荐配置：`http://127.0.0.1:3000/v1`
- 局域网用户访问：`http://<Mac局域网IP>:3000`
- Docker 直接入口：`http://127.0.0.1:13000`

本机 Codex 如果使用 `http://127.0.0.1:3000/v1`，使用日志应记录为 `127.0.0.1`。局域网用户访问时，使用日志应记录为该客户端的实际局域网来源 IP。

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

本模式已在隔离开发环境按 `3001 -> 13001` 端口组合验证，记录见 [CR-MACOS-DOCKER-TUN-LAN-2026-05-22.md](../reviews/archive/CR-MACOS-DOCKER-TUN-LAN-2026-05-22.md)。

## 回滚

停止宿主机 Caddy launchd agent，然后用默认端口发布重建：

```bash
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.new-api.macos-tun.new-api.3000.plist
env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
  NEW_API_PORT_MAPPING=3000:3000 \
  docker compose -f docker-compose.yml up -d --no-deps --force-recreate "${COMPOSE_SERVICE:-new-api}"
```

如果使用了自定义 `NEW_API_DEFAULT_PORT_MAPPING`，手动回滚时把上方 `3000:3000` 替换为相同值。不要在手动回滚命令中加载 `deploy/env/macos-docker-tun.env`，也不要沿用 shell 中残留的 `NEW_API_PORT_MAPPING=127.0.0.1:13000:3000`。

如果没有正式 `.env` 且依赖 shell 中显式导出的运行时变量回滚，手动回滚命令也应给 `docker compose` 增加 `--env-file /dev/null`，避免当前目录中后来出现的 `.env` 被隐式读取。

回滚变量默认是 `3000:3000`，等价于 Docker 直接监听宿主机所有地址；这只是临时回滚路径，不应作为 Caddy 模式的稳定终态。如果 TUN 仍开启，使用日志可能重新出现异常公网 IP；恢复正式 Caddy 模式前必须重新执行 loopback 映射和 Caddy 接管流程。

## 常见失败处理

| 现象 | 处理 |
| ---- | ---- |
| `caddy is required` | 先执行 `brew install caddy`，再重新跑 `--check-only` |
| `missing TUN env file` | 先复制 `deploy/env/macos-docker-tun.env.example` 到 `deploy/env/macos-docker-tun.env` |
| `SESSION_SECRET and CRYPTO_SECRET must be set` | 确认正式 `.env` 或当前 shell 环境能被 Docker Compose 读取；不要把这些 secret 写进 `deploy/env/macos-docker-tun.env` |
| `NEW_API_PORT_MAPPING ... does not match derived ...` | 同步修改 `NEW_API_LOOPBACK_PORT`、`NEW_API_CONTAINER_PORT` 和 `NEW_API_PORT_MAPPING` |
| `LAN proxy port ... is already in use by an unmanaged process` | 停止占用该端口的进程，或改用其他 `NEW_API_LAN_PROXY_PORT` |
| `loopback backend port ... is already in use` | 改用其他 `NEW_API_LOOPBACK_PORT` 并同步 `NEW_API_PORT_MAPPING` |
| `CADDY_LAUNCHD_LABEL contains unsupported characters` | label 只使用字母、数字、点、下划线和连字符 |
| `Caddy did not keep running` | 查看脚本输出的 Caddy log，通常是端口冲突、Caddyfile 语法错误或 launchd plist 路径无写入权限 |
| Codex 在切换时断联 | 切换命令仍会继续执行；重新连接后检查 `curl http://127.0.0.1:3000/api/status` 和 `launchctl print gui/$(id -u)/com.new-api.macos-tun.new-api.3000` |
