# new-api 正式发布门禁

## 范围与硬停止条件

本文定义可复用的构建、验收、灰度和回滚门禁，不代表已经授权正式部署。

- Caddy 可以在 `3000` 提供局域网入口；正式 `new-api` 仍绑定到 `127.0.0.1:13000`。
- `web/classic` 与 `web/default` 独立维护。批准发布前必须确认生产配置使用的主题。
- dirty worktree 可以验证隔离环境或运行只读预检；正式 `--apply` 在源码和构件门禁通过前一律拒绝，不允许执行同镜像运维重建。
- 所有适用门禁通过前，不得执行正式 `docker compose up`、重建或升级正式容器、替换镜像、写入正式数据库、全局 `prune`、删除备份或在线执行文件系统修复。

## 正式基线记录

每个候选版本都必须重新采集以下值，不得把另一台机器的值复制到新的发布记录。

| 字段 | 采集命令或来源 | 必需证据 |
| ----- | ------------------------- | ----------------- |
| 正式镜像 | `docker inspect` | 镜像引用、镜像 ID、不可变 digest、OCI revision 与 `/new-api --build-info` 一致 |
| 正式容器 | `docker inspect new-api` | 容器 ID、启动时间、重启次数、OOM 状态、健康状态和发布端口 |
| 候选源码 | Git 仓库 | 提交、工作区状态、源码 URL 和构建时间 |
| 前端主题 | 正式配置和实际渲染页面 | 当前主题，以及 classic/default 分别构建的证据 |
| 入口 | 受保护的部署配置 | 后端 URL、公开 URL、可选局域网 URL、HTTP 状态、延迟和 UTC 时间 |
| Compose 来源 | Docker 标签和渲染后的 Compose 配置 | project、service、working directory、config files 和镜像引用 |

部署实例值必须放在仓库外：

```bash
export NEW_API_PRODUCTION_IMAGE="<immutable-production-image>"
export NEW_API_RECOVERY_BACKEND_URL="http://127.0.0.1:13000/api/healthz"
export NEW_API_RECOVERY_PUBLIC_URL="http://127.0.0.1:3000/api/healthz"
export NEW_API_RECOVERY_LAN_URL="http://<lan-host>:3000/api/healthz"
export NEW_API_CODE_DIR="/absolute/path/to/source-root"
export NEW_API_DATA_DIR="/absolute/path/to/production-data"
export NEW_API_LOG_DIR="/absolute/path/to/production-logs"
export NEW_API_ARCHIVE_DIR="/absolute/path/to/backup-archive"
```

## 门禁 A：源码与构件冻结

接收候选版本前先保存只读基线：

```bash
git rev-parse HEAD origin/main
git status --short --branch
docker inspect -f '{{.Name}} {{.Config.Image}} {{.Image}} {{.State.StartedAt}} {{.RestartCount}} {{.State.OOMKilled}}' new-api postgres redis
docker image inspect "$NEW_API_PRODUCTION_IMAGE"
docker exec new-api /new-api --build-info
```

工作区状态无法解释、镜像缺少 digest、OCI revision 与 build-info 不一致、容器生命周期值异常变化或入口探针失败时立即停止。

## 门禁 B：隔离验证

未完成代码必须在完全隔离的栈中验证。应用、PostgreSQL、Redis、数据目录、日志目录、端口、`SESSION_SECRET` 和 `CRYPTO_SECRET` 都必须独立，不能复用正式卷或端口。

先执行最小充分检查：

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service
cd web/default && bun run lint && bun run build
cd ../classic && bun run lint && bun run build
cd ../.. && bun scripts/frontend-bundle-versions.mjs
```

产物版本门禁校验 dist 里实际打包的依赖版本与 node_modules 解析一致（`bun audit` 只读 lockfile，证明不了产物内容）。该门禁同样内置在 `scripts/build-release-frontends.sh` 与 Dockerfile 的每个前端构建阶段，脚本从自己的路径解析仓库根目录，不依赖调用时的 cwd。

随后执行协议 smoke、Responses/SSE 兼容性检查、真实数据库 Schema 兼容性检查和隔离 HTTP 验收。容器健康或仅改变端口都不能证明隔离有效。必须记录实际二进制 revision 和候选提交。

## 门禁 C：前端与主题验收

- 生产主题为 classic 时，验收登录、管理日志、渠道管理、系统设置、暗色模式和移动端布局。
- 独立验收 default 的 lint、构建、typecheck 和受影响测试；default 的结果不能替代 classic 验收。
- typecheck 必须在规定的墙钟时间内以退出码 `0` 完成。超时、被信号终止或静默卡住都视为门禁失败。
- 两套前端分别记录构建 revision、日志列、筛选条件、Responses/渠道路由详情和主题配置。
- 只读前端联调必须使用仓库的只读代理。前端请求层和代理层都必须阻断不安全方法及认证副作用。

## 门禁 D：镜像来源

每个候选镜像必须记录 Git 提交、dirty 状态、构建时间、源码 URL、OCI 标签、镜像 ID、不可变 digest、二进制 build-info、Compose project、service、working directory 和 config files。

只有经过验证的提交才能获得正式标签。dirty worktree 只能创建隔离标签，不能创建正式标签。不得使用 `latest` 或没有记录的本地标签作为正式候选。

`scripts/deploy-macos-docker-tun.sh` 是端口和 Caddy 运维脚本，不是镜像发布工具。任何正式 `compose up` 前，它都会确认工作区干净，并确认 TUN 和回滚配置解析到与运行容器相同的现有镜像 digest。tracked 或 untracked dirty worktree、镜像引用、digest 或 revision 变化时都必须 fail-closed。

镜像变更必须设置 `NEW_API_ALLOW_IMAGE_CHANGE=1`，同时满足工作区干净、候选 OCI revision 已验证，并提供包含以下精确键值的私有批准记录：

```text
status=approved
image=<candidate-image-reference>
revision=<40-character-commit>
worktree=clean
```

### 运行时用户

镜像以 root 启动入口脚本，脚本处理运行时目录后立即用 `su-exec` 降权，服务进程本身是非 root（uid/gid 1000）。升级到该版本时新增一项验收：

```bash
docker top new-api -o pid,user,args | grep '/new-api'
```

输出中的 `USER` 必须是 `1000`，不能是 `root`。入口脚本会为 `/data`、`/app/logs` 和 `--log-dir` 指定目录尝试 `chown`，判定标准是目标 uid 能否真的写入；`chown` 失败或之后仍不可写时会输出明确告警。出现告警时先修正宿主机目录属主或挂载选项，只有在只读挂载、NFS root_squash 等无法交接属主的场景下，才允许设置 `NEW_API_RUN_AS_ROOT=1` 显式放弃降权，并在批准记录里写明原因。不得把该开关当作默认值。

## 门禁 E：维护窗口与灰度

正式维护窗口开始前必须同时满足：

1. 独立逻辑备份存在，并在隔离环境完成恢复抽样。
2. 上一个镜像 digest、渲染后的 Compose 配置、配置键快照和主题配置已保存到私有审计记录。
3. 已认证的 PostgreSQL/Redis 探针、Docker `_ping`、后端、公开入口和已配置的局域网入口全部通过。
4. 宿主机资源、备份、Docker 虚拟磁盘和文件系统风险处于批准的维护边界内。
5. 回滚负责人、停止条件和恢复命令明确。

使用参数化的备份覆盖检查。`tmutil isexcluded` 或历史备份路径字符串本身不能作为可恢复证明。

```bash
python3 scripts/macos-docker-tun-time-machine-report.py \
  --path code="$NEW_API_CODE_DIR" \
  --path data="$NEW_API_DATA_DIR" \
  --path logs="$NEW_API_LOG_DIR" \
  --path archive="$NEW_API_ARCHIVE_DIR" \
  --require-included archive

latest_backup="$(tmutil latestbackup)"
[ -d "$latest_backup" ]
```

如果源码树被 Time Machine 排除，正式数据和归档必须有独立的纳入位置和已验证的恢复路径。灰度必须使用独立 Compose project、端口集合和数据副本，不能把正式 Caddy 或后端回环入口指向未验收候选。

## 门禁 F：回滚

任一预期入口不是 `200`、延迟超过维护阈值、Caddy 或局域网路由失败、应用健康或生命周期值漂移、PostgreSQL/Redis/Docker 探针失败、前端行为与记录不一致，或出现未分类的错误放大、锁等待或宿主机硬故障时，立即停止或回滚。

回滚时停止候选晋级，恢复上一个不可变镜像 digest 和主题配置，确认原 Compose project 及依赖，再决定是否恢复流量。代码回滚不会自动回滚数据库数据；Schema 或数据变更必须有独立恢复方案。

## 延后条件

对未解决的备份恢复验证、依赖 healthcheck 迁移、数据库日志/索引治理、文件系统检查、监控接入、代理/渠道分类、管理员专属 UI 验收和 dirty-worktree 发布，保留私有且最新的审计记录。这些条件只阻断受影响的门禁，不能成为绕过正式安全边界的理由。
