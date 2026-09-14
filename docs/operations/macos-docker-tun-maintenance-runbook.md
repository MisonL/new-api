# macOS Docker 与 Caddy 维护窗口手册

## 范围与硬停止条件

本文定义 macOS Docker Desktop 部署的通用准备、取证和回滚边界，不代表已经授权正式变更。

- Caddy 可以在 `0.0.0.0:3000` 提供局域网入口；应用容器仍只发布到 `127.0.0.1:13000`。
- `web/classic` 与 `web/default` 都必须独立维护和验收。修改任一前端前先确认生产配置使用的主题。
- dirty worktree 只能用于只读预检和隔离验证。正式 `--apply` 会在执行 Docker/Caddy 变更前拒绝任何 tracked 或 untracked dirty worktree，不得用它进行同镜像运维重建、替换或晋级正式镜像。
- 没有匹配的备份、停机窗口和回滚证据时，不得执行正式 `docker compose up`、重建或升级正式容器、替换镜像、写入正式数据库、全局 prune、删除备份或在线文件系统修复。
- 每次变更前后都记录命令、退出码、UTC 时间、镜像 digest、容器生命周期值和 HTTP 探针结果。

发布来源、隔离验证、灰度和回滚门禁定义在 [new-api-production-release-gates.md](new-api-production-release-gates.md)。

## 部署配置边界

部署实例值必须放在仓库外，至少在受保护的部署环境或 LaunchAgent 安装环境中定义以下变量：

```bash
export NEW_API_RECOVERY_BACKEND_URL="http://127.0.0.1:13000/api/healthz"
export NEW_API_RECOVERY_PUBLIC_URL="http://127.0.0.1:3000/api/healthz"
export NEW_API_RECOVERY_LAN_URL="http://<lan-host>:3000/api/healthz"
export NEW_API_CODE_DIR="/absolute/path/to/source-root"
export NEW_API_DATA_DIR="/absolute/path/to/production-data"
export NEW_API_LOG_DIR="/absolute/path/to/production-logs"
export NEW_API_ARCHIVE_DIR="/absolute/path/to/backup-archive"
export NEW_API_MONITOR_DISK_PATHS="/:/absolute/path/to/data-volume"
```

脚本中的 `NEW_API_RECOVERY_LAN_URL` 可选。只有明确关闭外部局域网探针时才留空。`NEW_API_MONITOR_DISK_PATHS` 使用冒号分隔，默认检查 `/`；所有可能影响部署的卷都应加入其中。

仓库示例不得放入凭据、正式镜像身份、运行时路径或主机专用 IP。恢复和监控安装器会把这些值渲染到当前用户私有的 LaunchAgent 文件中。

## 通用预检

执行任何维护项前先运行以下检查，首次出现非预期结果就停止。

```bash
docker version
docker compose version
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
docker inspect -f '{{.Name}} {{.State.Status}} {{.State.Health.Status}} {{.RestartCount}} {{.Config.Image}}' new-api postgres redis

probe_200() {
  [ "$(curl --connect-timeout 3 --max-time 10 -fsS -o /dev/null -w '%{http_code}' "$1")" = "200" ]
}

probe_200 "$NEW_API_RECOVERY_BACKEND_URL"
probe_200 "$NEW_API_RECOVERY_PUBLIC_URL"
if [ -n "${NEW_API_RECOVERY_LAN_URL:-}" ]; then
  probe_200 "$NEW_API_RECOVERY_LAN_URL"
fi

python3 scripts/macos-docker-tun-port-audit.py --pretty
python3 scripts/macos-docker-tun-time-machine-report.py \
  --path code="$NEW_API_CODE_DIR" \
  --path data="$NEW_API_DATA_DIR" \
  --path logs="$NEW_API_LOG_DIR" \
  --path archive="$NEW_API_ARCHIVE_DIR" \
  --require-included archive \
  --pretty

latest_backup="$(tmutil latestbackup)"
[ -d "$latest_backup" ]
```

预期 HTTP 入口不是 `200`、Docker `_ping` 失败、应用不是 `running`、OOM 或重启次数异常变化、受监控资源超过维护阈值，或活跃连接没有确认归属时，立即停止。

## 可用机制

- 恢复看门狗只操作已有 Docker 对象，不构建镜像、不执行 `docker compose up`、不重置 PostgreSQL WAL、不重建索引，也不删除数据。
- PostgreSQL 就绪必须通过认证后的 `SELECT 1`；Redis 就绪必须通过认证后的 `PONG`。缺少凭据时报告为未就绪。
- 监控器以 dry-run 模式运行，可报告后端、公开入口、局域网、Docker、PostgreSQL、Redis、延迟、磁盘、swap、负载和日志大小，不触发恢复动作。
- Caddy、恢复、监控和应用日志保留都由显式配置的路径和私有状态目录控制。日志保留失败必须可观测，但不得重启应用容器。
- Docker 资源清理必须逐项记录。Docker 给出的可回收估算不能直接作为删除授权。

## 维护矩阵

| 项目 | 必需证据 | 执行边界 | 停止条件 | 回滚 |
| ---- | ----------------- | ------------------ | -------------- | -------- |
| 宿主机资源压力 | 负载、swap、内存压力、Docker stats、主要进程和近期诊断 | 与工作负载负责人协调；恢复自动化不得终止无关进程 | 资源超过批准阈值或归属不明 | 恢复原工作负载时间表 |
| 备份与日志 | 可读取的最新备份、独立逻辑备份、恢复抽样和归档覆盖证明 | 分开管理源码排除和数据保留策略；验证期间不得删除备份 | 备份路径不可读或恢复证据不完整 | 恢复原保留配置；只从已验证备份恢复删除的数据 |
| Docker.raw 或 APFS | 外部可恢复备份、官方恢复方法、确认目标未挂载和批准的停机时间 | 仅在 Docker Desktop 停止且目标未挂载时执行文件系统检查 | 缺少工具、停机时间或恢复证据 | 保持服务停止并使用已验证恢复路径 |
| PostgreSQL 与 Redis 健康迁移 | 隔离 Compose 契约、真实认证探针、逻辑备份和恢复抽样 | 在维护窗口逐个重建依赖；不得从全栈正式 Compose 操作开始 | 任一依赖或入口探针失败 | 恢复原依赖配置和数据卷 |
| Docker 资源清理 | 对象归属、标签、挂载点、大小、最后使用时间和恢复证据 | 一次只删除一个已确认候选；不得全局 prune | 归属或恢复价值不确定 | 只从已验证来源或备份重建 |
| 代理与上游渠道 | 错误分类、请求关联、受影响消费者和替代路由 | 低影响窗口一次只改一个渠道或代理规则 | 无法区分客户端取消、上游中断和服务端错误 | 恢复原路由并保留冷却记录 |
| 外部监控 | 私有状态文件权限和不含凭据的 dry-run 告警载荷 | 告警投递与恢复动作分离；告警失败不得触发重启 | 状态文件或告警载荷暴露敏感数据 | 停用告警任务并保留本地状态证据 |

## 退出检查表

1. 应用为 `running/healthy`；除非批准变更明确记录，OOM 和重启计数不得改变。
2. PostgreSQL 认证后的 `SELECT 1`、Redis 认证后的 `PONG` 和 Docker `_ping` 全部通过。
3. 后端和公开入口探针通过；配置了 `NEW_API_RECOVERY_LAN_URL` 时，局域网探针也必须通过。
4. Caddy、恢复和监控 LaunchAgent 状态、最近退出码、私有状态文件和日志权限符合安装模板。
5. 变更范围、镜像 digest、Compose 渲染、配置快照、备份证据、回滚结果和延后阻断项记录在私有审计中。
6. 任一项未完成时，不得宣称维护完成，也不得删除回滚材料。
