# CR-STORAGE-ACCELERATION-2026-06-12

## 范围

为 new-api 的 Docker 部署实现通用存储加速和安全 PostgreSQL 迁移路径。

经审查的设计有意使用显式存储迁移，而不是透明缓存。它不把宿主路径带入 Web UI，并在 marker 校验失败时快速失败。

## 需求映射

| 需求 | 证据 |
| --- | --- |
| P0 诊断工具 | `scripts/storage-probe.sh` |
| 顺序写入、8K 随机写入和 fsync 延迟探针 | `scripts/storage-probe.sh` |
| 可选 PostgreSQL 容器 `pg_test_fsync` 探针 | `scripts/storage-probe.sh` |
| macOS、Windows/WSL2、Linux 检查 | `scripts/storage-probe.sh`、`scripts/storage-plan-linux.sh`、`scripts/storage-plan-windows.ps1`、`docs/operations/storage-acceleration.md` |
| P1 可选 Compose overlay | `deploy/compose/docker-compose.storage-postgres-bind.yml` |
| 显式 bind mount 变量 | `deploy/env/storage-postgres.example.env` |
| 无回退的 marker 校验 | `deploy/compose/docker-compose.storage-postgres-bind.yml` |
| P2 先 dry-run 的迁移流程 | `scripts/storage-migrate-postgres.sh` |
| `pg_dump` 和 `pg_restore` 标准路径 | `scripts/storage-migrate-postgres.sh` |
| 表计数、最新时间戳和 marker 验证 | `scripts/storage-verify-postgres.sh` |
| 状态、登录页和受保护日志列表验证 | `scripts/storage-verify-postgres.sh` |
| 保留旧存储 | `docs/operations/storage-acceleration.md` |
| 高级 `pg_wal` 和 Linux 缓存警告 | `docs/operations/storage-acceleration.md` |
| 可观测性指引 | `docs/operations/storage-acceleration.md` |

## 安全决策

- 标准路径是将完整 `PGDATA` 迁移到 bind mount。
- overlay 要求 `NEW_API_POSTGRES_DATA_DIR`、`NEW_API_POSTGRES_MARKER` 和 `NEW_API_POSTGRES_MARKER_VALUE`。
- marker 文件与 `PGDATA` 分开挂载，因此新的空 PostgreSQL 目录仍可安全初始化。
- marker 内容必须精确匹配。marker 缺失或不匹配时，PostgreSQL 容器会在启动前退出。
- overlay 和迁移脚本会在检查 marker 是否位于 PGDATA 内之前，拒绝非绝对 marker/PGDATA 路径及 `/./`、`/../`、`/.`、`/..` 等点号片段。
- 迁移脚本默认为 `--dry-run`。
- 执行模式要求 `CONFIRM_STORAGE_MIGRATION` 与同一容器、数据库和目标路径的 dry-run 输出确认值精确匹配。单独使用 `--execute` 会在获取锁或停止任何容器前失败。
- 最终 dump 在停止 `new-api` 后创建，先阻断写入再生成可恢复快照。
- 最终 dump 直接由 `pg_dump` 流式写入宿主备份路径，不在 PostgreSQL 容器可写层创建大型中间 dump。
- 匹配的 `pg_dumpall --globals-only --no-role-passwords` 文件写入宿主备份路径，供操作员复核角色和全局权限。
- Windows shell 中，custom-format `pg_dump` 在写入宿主备份文件前使用 base64 包装，避免二进制 stdout 换行转换。
- 流式 dump 先写入 `.incomplete` 文件，用 `pg_restore -l` 校验后才重命名为最终路径；dump 或校验失败会删除不完整文件。
- 执行模式在停止服务前校验 `ENV_FILE`、目标 PostgreSQL 主版本和备份目录剩余空间。
- 恢复路径使用原数据库的 encoding/collation/owner/locale provider/ICU locale 重建数据库，并运行 `pg_restore --exit-on-error`。
- 恢复时将宿主 dump 直接流入 `pg_restore`，不再复制到 PostgreSQL 容器可写层。
- 没有 `pg_database.datlocprovider` 的 PostgreSQL 版本按 libc locale-provider 数据库处理，兼容旧源集群，同时保留 PostgreSQL 15+ 的 locale-provider 元数据。
- 主版本低于 15 的 PostgreSQL 目标不接收 `createdb --locale-provider` 参数；目标主版本必须大于或等于源版本。
- Compose 重建后，迁移会检查 PostgreSQL 是否保持运行；如果 marker 或权限检查导致容器退出，会立即打印 PostgreSQL 最近日志。
- 如果 Compose 替换已开始但随后失败，脚本不再假设旧 PostgreSQL 容器仍存在，而是报告明确的人工恢复边界，并保持 `new-api` 停止，直到操作员恢复 PostgreSQL。
- 执行模式拒绝非空目标数据目录，除非人工复核后设置 `ALLOW_NON_EMPTY_TARGET=true`。
- `C:/new-api/postgres/data` 等 Windows 盘符路径只接受 Windows shell；macOS、Linux 和 WSL2 必须使用 Unix 绝对路径。
- 同时提供 marker 环境变量时，验证器检查挂载在 `/run/new-api/postgres-storage.marker` 的 marker。未提供这些变量时明确跳过 marker 检查，不伪称 overlay 已验证；部分 marker 环境配置会使检查失败。
- 迁移脚本把源 `users`、`channels`、`tokens`、`logs` 计数传给 `scripts/storage-verify-postgres.sh`；只有提供对应期望计数变量时，验证器才执行精确计数比较。
- 不自动删除旧卷或旧目录。

## 已知边界

- 脚本面向 PostgreSQL 部署；SQLite 和 MySQL 需要独立存储流程。
- 标准迁移使用 `pg_dump` 和 `pg_restore`。物理 PGDATA 复制不自动化，因为 WAL 和恢复语义需要更窄的人工方案。
- `pg_wal` 分离只作为高级方案记录。
- Docker data-root 迁移是宿主级专家操作，不是 new-api 运行时设置。
- `NEW_API_POSTGRES_WAL_DIR` 和 `NEW_API_DOCKER_DATA_DIR` 仅为规划变量，标准 Compose overlay 不消费它们。
- Linux 和 Windows 平台规划脚本只读报告风险，不修改 mount 选项、Defender 设置、Docker data-root 或 PostgreSQL 布局。
- 迁移源应为已初始化的 new-api PostgreSQL 数据库；缺少必需表时，预检计数查询会有意失败。
- 正式迁移不得与其他 Docker 重建或 Compose 操作并发运行。`scripts/storage-migrate-postgres.sh --execute` 在停止服务前加宿主本地目录锁，并检查 Docker build 或 Compose 进程。

## 验证日志

执行命令：

```bash
bash -n scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh
bash -n scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
shellcheck scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
pwsh -NoProfile -Command '$null = [System.Management.Automation.PSParser]::Tokenize((Get-Content -Raw scripts/storage-plan-windows.ps1), [ref]$null); "powershell-parse-ok"'
docker compose -f docker-compose.yml -f deploy/compose/docker-compose.storage-postgres-bind.yml --env-file <temporary-env> config
RUN_WRITE_TESTS=false scripts/storage-probe.sh
RUN_WRITE_TESTS=true PROBE_DIR=/tmp scripts/storage-probe.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev RUN_WRITE_TESTS=false RUN_POSTGRES_FSYNC_TESTS=true PG_TEST_FSYNC_SECONDS=1 scripts/storage-probe.sh
scripts/storage-plan-linux.sh
pwsh -NoProfile -File scripts/storage-plan-windows.ps1
RUN_WRITE_TESTS=true PROBE_DIR=/tmp/new-api-probe-missing-dir scripts/storage-probe.sh
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> NEW_API_POSTGRES_DATA_DIR=<temporary-data> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=dry-marker scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> NEW_API_POSTGRES_DATA_DIR=C:/new-api/postgres/data NEW_API_POSTGRES_MARKER=C:/new-api/postgres/storage.marker NEW_API_POSTGRES_MARKER_VALUE=marker scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> NEW_API_POSTGRES_DATA_DIR=<temporary-data> NEW_API_POSTGRES_MARKER=<marker-with-newline> NEW_API_POSTGRES_MARKER_VALUE=dry-marker scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> NEW_API_POSTGRES_DATA_DIR=<path-with-trailing-dot-segment> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=marker scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> NEW_API_POSTGRES_DATA_DIR=<temporary-data-with-trailing-slash> NEW_API_POSTGRES_MARKER=<marker-inside-data-dir> NEW_API_POSTGRES_MARKER_VALUE=marker scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> SKIP_DOCKER_ACTIVITY_CHECK=true MIGRATION_LOCK_DIR=<temporary-lock> NEW_API_POSTGRES_DATA_DIR=<temporary-data> NEW_API_POSTGRES_MARKER=<missing-marker> NEW_API_POSTGRES_MARKER_VALUE=missing scripts/storage-migrate-postgres.sh --execute
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> MIGRATION_LOCK_DIR=<temporary-lock> NEW_API_POSTGRES_DATA_DIR=<temporary-data> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=dry-marker scripts/storage-migrate-postgres.sh --execute
NEW_API_STATUS_URL=not-a-url/api/status scripts/storage-verify-postgres.sh
scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_STATUS_URL=http://127.0.0.1:3001/api/status scripts/storage-verify-postgres.sh
LC_ALL=C rg -n '[^[:ascii:]]' scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh scripts/storage-plan-windows.ps1 deploy/compose/docker-compose.storage-postgres-bind.yml deploy/env/storage-postgres.example.env docs/operations/storage-acceleration.md docs/reviews/CR-STORAGE-ACCELERATION-2026-06-12.md
claude -p --permission-mode plan --tools ''
gemini -p '<read-only review prompt>' --approval-mode plan --output-format text
coderabbit review --agent --plain
git diff --check
```

结果：

- Shell 语法检查通过。
- Bash 脚本的 `shellcheck` 通过。
- `scripts/storage-plan-windows.ps1` 的 PowerShell 解析检查通过。
- 使用包含显式 marker 和数据路径的临时 env 文件时，Compose 配置通过。
- 存储探针完成本地 Docker 环境检查，并报告正式 PostgreSQL bind mount 为 `<production-postgres-data-dir>`。
- PostgreSQL `pg_test_fsync` 探针路径只在隔离开发 PostgreSQL 容器验证，未在正式 PGDATA 验证；探针现在在 PGDATA 内创建临时目录并在退出时删除。
- `scripts/storage-plan-linux.sh` 以只读模式完成，报告当前 Docker Desktop data-root、PostgreSQL UID/GID 和未设置的候选存储变量。
- `scripts/storage-plan-windows.ps1` 在 PowerShell 下以只读模式完成，报告 Docker data-root、WSL 可用性、未设置的候选变量和 Windows 特定建议。
- dry-run 完成时没有停止容器或复制 dump 文件；现在会打印目标目录、marker、可用空间、当前 PGDATA 大小、挂载、表计数、原数据库 encoding/collation/owner、目标版本计划、备份空间计划、全局 dump 计划和计划操作。
- marker 比较按字节执行，因此 dry-run 拒绝末尾带换行的 marker 内容。
- dry-run 拒绝包含 `/./`、`/../`、`/.` 或 `/..` 等点号片段的非规范数据或 marker 路径。
- 迁移脚本在检查 marker 是否位于 PGDATA 内前去除末尾斜杠，因此 `/data/` 和 `/data` 视为同一目标边界。
- macOS 上使用 `C:/new-api/postgres/data` 的 dry-run 在 Docker 检查前被拒绝，避免非 Windows 主机意外创建相对 `C:` 目录。
- 存储 overlay 对 marker bind-mount 目录报告退出码 `69`，明确表示 Docker 创建目录场景。
- 执行模式在 `docker stop`、`pg_dump` 或 Compose 重建前拒绝缺失 marker，失败时删除宿主本地迁移锁。
- 执行模式在获取锁、停止容器、运行 `pg_dump` 或 Compose 重建前拒绝缺失 `CONFIRM_STORAGE_MIGRATION`。
- 验证器在 HTTP 检查前拒绝无效的 `NEW_API_BASE_URL` 或 `NEW_API_STATUS_URL`。
- 未提供 marker 环境变量时，验证器明确将 `postgres-storage-marker` 报告为跳过；提供时要求三个 marker 变量齐全，并在 PostgreSQL 容器内执行精确字节匹配。
- 验证器从 `psql` 输出中去除回车，使 Windows shell 调用时的数字比较稳定。
- 存储脚本、Compose overlay、env 示例以及存储运维/审计文档的 ASCII 扫描通过。
- 正式只读验证在采样时通过，计数为 `users=4`、`channels=77`、`tokens=8`、`logs=855023`；除非提供真实认证 cookie 和 `New-Api-User` 值，否则跳过受保护日志列表检查。
- 针对 3001 执行非破坏性恢复命令检查：导出 `new-api-dev`，使用原 encoding/collation/owner 创建临时数据库，用 `pg_restore --exit-on-error` 恢复，检查 `users=8` 和 `logs=411406` 后删除临时数据库。
- 使用存储 overlay 执行 3001 隔离开发迁移演练；最终脚本恢复 `users=8`、`channels=99`、`tokens=15`、`logs=411406`，随后 `/api/status` 返回成功。
- 演练后将 3001 恢复到原 `deploy/env/dev-isolated.env` PostgreSQL 数据目录，并确认健康。

演练中发现的问题：

- 第一版等待本地 socket 的 `pg_isready`，可能连接 PostgreSQL 官方镜像的临时初始化服务器，与 `POSTGRES_DB` 创建产生竞态。脚本现在在恢复前等待 TCP `pg_isready -h 127.0.0.1`。
- 后续审查发现，`run_check` 调用的函数内 `psql` 失败时，`scripts/storage-verify-postgres.sh` 仍可能打印某些检查的成功标签。验证器现在对失败的 `psql_query` 明确返回失败；通过使用错误 PostgreSQL 角色运行验证器并确认非零退出和 `check_failed` 输出完成验证。

外部审查跟进：

- Gemini 指出原先容器内 `/tmp` dump 存在正式磁盘风险。迁移脚本现在将 `pg_dump` 直接流式写入宿主备份路径，并在重命名前校验不完整 dump。
- Gemini 指出末尾斜杠可能绕过路径检查。迁移脚本现在在 marker-inside-PGDATA 检查前去除末尾斜杠。
- Gemini 指出 marker 示例不一致。env 示例现在与运维文档使用相同的 `new-api-prod-postgres-fast-storage-20260612` 值。
- Claude 指出 Compose 替换失败恢复问题。脚本现在把替换后启动失败作为人工恢复边界，不再假设旧 PostgreSQL 容器可以重启。
- Claude 指出不完整 dump 清理问题。dump 或校验失败时脚本删除 `.incomplete` 文件。
- Claude 指出数据库 locale-provider 保留问题。脚本现在采集 `datlocprovider` 和 `daticulocale` 并传给 `createdb`。
- CodeRabbit 指出验证器 URL 校验和清理一致性问题。验证器现在校验 HTTP/HTTPS 基础 URL，并通过退出 trap 集中清理临时文件。
- CodeRabbit 指出隐式 `URLS_READY` 状态和重复 curl 检查。验证器现在直接根据 `init_urls` 设置 URL 就绪状态，并将 curl 可用性保留为单一预检。
- CodeRabbit 指出 env 示例应明确创建目标数据目录。示例现在要求操作员创建目录并使用迁移脚本填充。
- CodeRabbit 指出容器临时 dump 清理问题。恢复路径通过直接将宿主 dump 流入 `pg_restore`，完全移除了 `/tmp/new-api-storage-migration.dump` 的需要。
- CodeRabbit 指出延迟发生的 `docker compose` 失败。迁移脚本现在在工具预检阶段检查 `docker compose version`。
- CodeRabbit 建议将默认 PostgreSQL 用户改为 `postgres`；未采用，因为本仓库 Compose 配置对正式和隔离开发 PostgreSQL 都使用 `root`。
- Gemini 指出 PostgreSQL 14 对 `datlocprovider` 的兼容性；脚本现在检测列是否存在，并回退到 libc provider 元数据。

## 开发验证 2026-06-13

采样时间：`2026-06-13T11:30:52Z`。

执行命令：

```bash
bash -n scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
shellcheck scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
pwsh -NoProfile -Command '$errors=$null; $null = [System.Management.Automation.PSParser]::Tokenize((Get-Content -Raw scripts/storage-plan-windows.ps1), [ref]$errors); if ($errors) { $errors | Format-List *; exit 1 }; "powershell-parse-ok"'
git diff --check -- .gitignore scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh scripts/storage-plan-windows.ps1 deploy/compose/docker-compose.storage-postgres-bind.yml deploy/env/storage-postgres.example.env docs/operations/storage-acceleration.md docs/reviews/CR-STORAGE-ACCELERATION-2026-06-12.md
docker compose -f deploy/compose/dev-isolated.yml -f deploy/compose/docker-compose.storage-postgres-bind.yml --env-file <temporary-env> config
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev RUN_WRITE_TESTS=false RUN_POSTGRES_FSYNC_TESTS=true PG_TEST_FSYNC_SECONDS=1 scripts/storage-probe.sh
scripts/storage-plan-linux.sh
pwsh -NoProfile -File scripts/storage-plan-windows.ps1
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 NEW_API_CONTAINER=new-api-dev-isolated-new-api-1 POSTGRES_DB=new-api-dev POSTGRES_USER=root POSTGRES_IMAGE=postgres:15 NEW_API_POSTGRES_DATA_DIR=<temporary-target> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=marker-ok BACKUP_DIR=<temporary-backups> scripts/storage-migrate-postgres.sh --dry-run
CONFIRM_STORAGE_MIGRATION=execute:<dev-postgres>:<dev-new-api>:new-api-dev:<temporary-target> POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 NEW_API_CONTAINER=new-api-dev-isolated-new-api-1 POSTGRES_DB=new-api-dev POSTGRES_USER=root POSTGRES_IMAGE=postgres:15 NEW_API_POSTGRES_DATA_DIR=<temporary-target> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=marker-ok ENV_FILE=<missing-env> BACKUP_DIR=<temporary-backups> scripts/storage-migrate-postgres.sh --execute
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_POSTGRES_MARKER_VALUE=marker-ok scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_EXPECT_USERS_COUNT=999999 scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_EXPECT_USERS_COUNT=8 NEW_API_EXPECT_CHANNELS_COUNT=99 NEW_API_EXPECT_TOKENS_COUNT=15 scripts/storage-verify-postgres.sh
COMPOSE_PROJECT_NAME=new-api-storage-migrate-test COMPOSE_FILES=<temporary-compose>:deploy/compose/docker-compose.storage-postgres-bind.yml ENV_FILE=<temporary-env> POSTGRES_CONTAINER=new-api-storage-migrate-test-postgres NEW_API_CONTAINER=new-api-storage-migrate-test-new-api POSTGRES_DB=new-api-dev POSTGRES_USER=root POSTGRES_IMAGE=postgres:15 NEW_API_POSTGRES_DATA_DIR=<temporary-target> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=marker-ok BACKUP_DIR=<temporary-backups> MIGRATION_LOCK_DIR=<temporary-lock> NEW_API_STATUS_URL=http://127.0.0.1:31234/api/status NEW_API_LOGIN_URL=http://127.0.0.1:31234/login CONFIRM_STORAGE_MIGRATION=execute:new-api-storage-migrate-test-postgres:new-api-storage-migrate-test-new-api:new-api-dev:<temporary-target> scripts/storage-migrate-postgres.sh --execute
COMPOSE_PROJECT_NAME=new-api-storage-migrate-test COMPOSE_FILES=<temporary-compose>:deploy/compose/docker-compose.storage-postgres-bind.yml ENV_FILE=<temporary-env> POSTGRES_CONTAINER=new-api-storage-migrate-test-postgres NEW_API_CONTAINER=new-api-storage-migrate-test-new-api POSTGRES_DB=new-api-dev POSTGRES_USER=root POSTGRES_IMAGE=postgres:15 NEW_API_POSTGRES_DATA_DIR=<temporary-target> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=marker-ok BACKUP_DIR=<temporary-backups> MIGRATION_LOCK_DIR=<temporary-lock> NEW_API_STATUS_URL=http://127.0.0.1:31234/api/status NEW_API_LOGIN_URL=http://127.0.0.1:31234/login CONFIRM_STORAGE_MIGRATION=execute:new-api-storage-migrate-test-postgres:new-api-storage-migrate-test-new-api:new-api-dev:<temporary-target> SKIP_DOCKER_ACTIVITY_CHECK=true scripts/storage-migrate-postgres.sh --execute
go test ./controller ./model ./relay/common ./relay/helper ./service ./relay/channel -count=1 -timeout=120s
cd web/default && bun run lint
cd web/default && bun run typecheck
cd web/default && bun test src/features/channels/lib/channel-form.test.ts
cd web/default && bun run build
cd web/classic && bun run lint
cd web/classic && bun test src/components/table/channels/modals/headerProfile.helpers.test.js
cd web/classic && bun run build
curl -fsS http://127.0.0.1:3001/api/status
curl -fsS http://127.0.0.1:3000/api/status
```

结果：

- Bash 语法、`shellcheck`、PowerShell 解析器和限定范围的 `git diff --check` 均通过。
- 使用包含显式开发 bind mount 和 marker 路径的临时 env 时，存储 Compose overlay 配置通过。
- 隔离开发 PostgreSQL 验证通过，计数为 `users=8`、`channels=99`、`tokens=15`、`logs=411416`。
- `pg_test_fsync` 针对 `new-api-dev-isolated-postgres-1` 完成；运行后删除临时 `pg_test_fsync.*` 目录。
- Linux 和 Windows 规划脚本以只读模式完成。
- marker 文件不带末尾换行时迁移 dry-run 通过；带末尾换行的 marker 另行被精确字节比较拒绝。
- `--execute` 在停止任何服务前拒绝缺失 `ENV_FILE`。
- marker 部分环境变量验证按预期失败。
- 期望计数不匹配按预期失败；匹配的期望计数通过。
- 第一次临时完整 `--execute` 在停止服务前失败，因为并发 Docker 活动保护检测到 Compose 进程。
- 验证保护行为后，第二次临时完整 `--execute` 才使用 `SKIP_DOCKER_ACTIVITY_CHECK=true`。它停止临时 HTTP 夹具，导出临时 PostgreSQL 数据库，使用存储 overlay 重建 PostgreSQL，通过流式 `pg_restore` 恢复，重启夹具，并验证 marker、HTTP 状态和精确表计数；恢复计数为 `users=2`、`channels=2`、`tokens=1`、`logs=3`。
- 执行验证后清理临时 `new-api-storage-migrate-test-*` 容器和测试网络。
- `./controller`、`./model`、`./relay/common`、`./relay/helper`、`./service` 和 `./relay/channel` 的 Go 测试通过。
- `web/default` 的 ESLint、TypeScript 构建、定向 `channel-form` 测试、生产构建和 Safari 兼容性检查通过。
- `web/classic` 的 Prettier 检查、定向 Header Profile 测试（`90 pass`）、生产构建和 Safari 兼容性检查通过。
- 验证后开发 `http://127.0.0.1:3001/api/status` 和正式 `http://127.0.0.1:3000/api/status` 均成功返回。

## 开发缓存路径验证 2026-06-13

采样时间：`2026-06-13T12:11:45Z`。

目标路径：

```text
NEW_API_POSTGRES_DATA_DIR=<fast-storage-root>/new-api-caches/postgres/data
NEW_API_POSTGRES_MARKER=<fast-storage-root>/new-api-caches/postgres/storage.marker
NEW_API_POSTGRES_MARKER_VALUE=<storage-marker-value>
```

命令与观察：

```bash
node <http-load-script> # 1000 requests, concurrency 10, before migration
docker exec new-api-dev-isolated-postgres-1 pgbench -U root -i -s 2 <temporary-db>
docker exec new-api-dev-isolated-postgres-1 pgbench -U root -c 8 -j 2 -T 20 -r <temporary-db>
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev RUN_POSTGRES_FSYNC_TESTS=true PG_TEST_FSYNC_SECONDS=1 scripts/storage-probe.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 NEW_API_CONTAINER=new-api-dev-isolated-new-api-1 POSTGRES_DB=new-api-dev scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 NEW_API_CONTAINER=new-api-dev-isolated-new-api-1 POSTGRES_DB=new-api-dev scripts/storage-migrate-postgres.sh --execute
node <http-load-script> # 1000 requests, concurrency 10, after migration
node <http-load-script-with-rotating-xff> # 2000 requests, concurrency 50, after migration
```

结果：

- 迁移前，隔离开发 PostgreSQL 容器使用 `<temporary-postgres-data-dir>`。
- 迁移到 `<fast-storage-root>/new-api-caches/postgres/data` 的 dry-run 和 execute 均通过；迁移后验证恢复精确源计数：`users=9`、`channels=102`、`tokens=15`、`logs=411416`。
- 运行中的隔离开发 PostgreSQL 容器现有 bind mount：`<fast-storage-root>/new-api-caches/postgres/data -> /var/lib/postgresql/data`，以及 `<fast-storage-root>/new-api-caches/postgres/storage.marker -> /run/new-api/postgres-storage.marker`。
- `deploy/env/dev-isolated.env` 只更新了非敏感路径变量：`DEV_POSTGRES_DATA_DIR`、`NEW_API_POSTGRES_DATA_DIR`、`NEW_API_POSTGRES_MARKER` 和 `NEW_API_POSTGRES_MARKER_VALUE`。
- 相同 IP 的 HTTP 压测触发了应用限流，不能作为干净的存储基准。迁移前并发 10 发送 1000 个请求，`/api/status` 返回 `7` 个 HTTP 200 和 `993` 个 HTTP 429，`/login` 返回 `1000` 个 HTTP 429；迁移后同一测试的 `/api/status` 为 `181` 个 HTTP 200 和 `819` 个 HTTP 429，`/login` 为 `60` 个 HTTP 200 和 `940` 个 HTTP 429。
- 迁移后使用轮换 `X-Forwarded-For` 避免单 IP 限流，并发 50 发送 2000 个请求，全部返回 HTTP 200。`/api/status` 延迟为 p50 `20.02ms`、p95 `74.26ms`、p99 `109.42ms`；`/login` 为 p50 `20.17ms`、p95 `43.17ms`、p99 `61.58ms`。
- 迁移前 `pgbench`：`1706.081971` TPS，平均延迟 `4.689ms`。
- 迁移后 `pgbench`：`1530.915552` TPS，平均延迟 `5.226ms`。
- 迁移前 `pg_test_fsync` 的单次 8KB 写入 `fdatasync` 为 `2866.289 ops/sec`、`349 usecs/op`。
- 迁移后 `pg_test_fsync` 的单次 8KB 写入 `fdatasync` 为 `2463.443 ops/sec`、`406 usecs/op`。
- 结论：请求的快速存储路径已在隔离开发环境配置并可用，但在本次验证环境中，相比之前的 `<temporary-postgres-data-dir>` 路径没有改善 PostgreSQL 热写性能，实测数据库写入路径在迁移后略慢。
- 已删除临时 `storage_perf_*` pgbench 数据库和 `pg_test_fsync.*` 目录。验证后开发与正式状态检查均成功，正式存储未被修改。

## 开发 PostgreSQL 调优验证 2026-06-14

采样时间：`2026-06-14T01:10:00Z`。

快速存储路径保持不变。隔离开发环境唯一的运行时控制输入，是通过 `NEW_API_POSTGRES_TUNING_ARGS` 显式设置 PostgreSQL 调优参数：

```text
checkpoint_timeout=15min
max_wal_size=4GB
min_wal_size=512MB
wal_compression=on
effective_io_concurrency=32
random_page_cost=1.5
log_min_duration_statement=500ms
```

存储 overlay 现在对不支持的调优键返回退出码 `70`。保持 `NEW_API_POSTGRES_TUNING_ARGS` 为空的默认部署仍使用 PostgreSQL 镜像默认值。

验证命令：

```bash
bash -n scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
git diff --check -- .gitignore scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh scripts/storage-plan-windows.ps1 deploy/compose/docker-compose.storage-postgres-bind.yml deploy/env/storage-postgres.example.env docs/operations/storage-acceleration.md docs/reviews/CR-STORAGE-ACCELERATION-2026-06-12.md
docker compose -f deploy/compose/dev-isolated.yml -f deploy/compose/docker-compose.storage-postgres-bind.yml --env-file <temporary-env-with-empty-tuning> config
docker compose -f deploy/compose/dev-isolated.yml -f deploy/compose/docker-compose.storage-postgres-bind.yml --env-file <temporary-env-with-invalid-tuning> run --rm --no-deps postgres
docker compose -f deploy/compose/dev-isolated.yml -f deploy/compose/docker-compose.storage-postgres-bind.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate postgres
POSTGRES_CONTAINER=<dev-postgres-container> POSTGRES_USER=<db-user> POSTGRES_DB=<db-name> NEW_API_STATUS_URL=http://127.0.0.1:3001/api/status NEW_API_POSTGRES_DATA_DIR=<fast-storage-root>/new-api-caches/postgres/data NEW_API_POSTGRES_MARKER=<fast-storage-root>/new-api-caches/postgres/storage.marker NEW_API_POSTGRES_MARKER_VALUE=<storage-marker-value> scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev RUN_POSTGRES_FSYNC_TESTS=true PG_TEST_FSYNC_SECONDS=1 scripts/storage-probe.sh
docker exec new-api-dev-isolated-postgres-1 pgbench -U root -i -s 2 <temporary-db>
docker exec new-api-dev-isolated-postgres-1 pgbench -U root -c 8 -j 2 -T 20 -r <temporary-db>
node <http-load-script-with-rotating-xff>
go test ./model ./service -count=1
```

观察结果：

- 无效调优键 `not_allowed=1` 在启动 PostgreSQL 前返回退出码 `70`。
- 重建后的隔离 PostgreSQL 容器保持 `running healthy`。
- `scripts/storage-verify-postgres.sh` 报告 marker 匹配，并新增打印 `postgres-tuning`、`postgres-write-stats` 和 `log-storage-profile`。
- 生效设置包括 `checkpoint_timeout=900 s`、`max_wal_size=4096 MB`、`min_wal_size=512 MB`、`wal_compression=pglz`、`effective_io_concurrency=32`、`random_page_cost=1.5` 和 `log_min_duration_statement=500 ms`。
- `synchronous_commit` 保持 `on`；标准配置不以持久性换取延迟。
- 当前 `logs` 表 heap 为 `362 MB`、索引 `513 MB`、总计 `875 MB`，共有 `39` 个索引；因此路径和 PostgreSQL 参数调优后，日志写放大仍是下一项优化候选。
- PGDATA 内 `pg_test_fsync` 的单次 8KB 写入 `fdatasync` 为 `2455.959 ops/sec`、`407 usecs/op`，接近迁移后基线，没有显示新的存储路径回归。
- 与上次路径验证相同形态的 `pgbench` 从迁移后基线 `1530.915552` TPS、平均延迟 `5.226ms` 提升到 `1831.598388` TPS、平均延迟 `4.368ms`。
- 调优后轮换 XFF 的 HTTP 压测全部返回 HTTP 200。`/api/status` 为 p50 `19.94ms`、p95 `33.81ms`、p99 `108.86ms`；`/login` 为 p50 `21.67ms`、p95 `43.94ms`、p99 `112.31ms`。
- 验证后开发和正式 `/api/status` 均成功返回；正式存储和正式 PostgreSQL 参数未被修改。

## 日志索引概况验证 2026-06-14

采样时间：`2026-06-14T01:45:00Z`。

下一轮优化聚焦 `logs` 写放大，未修改正式 schema。

变更：

- 新增 `scripts/storage-log-index-report.sh`，只读报告 `logs` 索引大小、扫描计数、前缀包含候选项、样本值和代表性 `EXPLAIN` 计划。
- 新增模型索引 `idx_logs_created_at_id`，按近期日志列表需要使用 `(created_at, id)` 列顺序。
- 新增模型索引 `idx_logs_upstream_request_created_at_id`，按 `(upstream_request_id, created_at, id)` 支持上游请求查询。
- 保持已有索引不变；本轮特别没有删除旧的 `idx_created_at_id` 和 `idx_logs_upstream_request_id_created_at`。

新增索引前的观察基线：

- `logs_rows=411416`, heap `362 MB`, indexes `513 MB`, total `875 MB`, index
  count `39`.
- `admin-recent` 使用 `idx_created_at_type` 加增量排序，而不是真正的 `(created_at, id)` 索引。
- `upstream-request-id` 使用 `idx_logs_upstream_request_id_created_at`，但数据库定义显示该索引实际只覆盖 `upstream_request_id`，与 `idx_logs_upstream_request_id` 重复而名不副实。

开发 PostgreSQL 验证：

```bash
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev REPORT_LIMIT=25 scripts/storage-log-index-report.sh
docker exec new-api-dev-isolated-postgres-1 psql -U root -d new-api-dev -v ON_ERROR_STOP=1 -c 'CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_created_at_id ON logs (created_at, id);' -c 'CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_upstream_request_created_at_id ON logs (upstream_request_id, created_at, id);'
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev REPORT_LIMIT=20 scripts/storage-log-index-report.sh
go test ./model -run 'TestRuntimePollingIndexesAutoMigrate|TestSumUsedQuota|TestRecordConsumeLogCopiesUpstreamRequestIdFromOther' -count=1
```

隔离开发数据库添加索引后的观察结果：

- `logs` 索引数量从 `39` 增至 `41`，索引大小从 `513 MB` 增至 `541 MB`。
- `admin-recent` 切换为 `Index Only Scan Backward using idx_logs_created_at_id`，不再需要额外排序。
- `upstream-request-id` 切换为 `Index Only Scan Backward using idx_logs_upstream_request_created_at_id`。
- 定向 Go model 测试通过。

剩余门禁：

- 由于旧的冗余索引删除前，新索引会增加写放大，正式发布不能止步于添加索引。未来迁移必须先采集真实正式流量统计，再明确决定是否删除或替换 `idx_created_at_id`、`idx_logs_upstream_request_id_created_at` 及其他前缀包含候选项。本轮有意没有执行破坏性索引删除。

## 日志索引维护验证 2026-06-14

采样时间：`2026-06-14T02:15:00Z`。

后续轮次为两个已知旧索引增加了显式 dry-run-first 维护脚本，正式环境未被修改。

变更：

- 从 `Log` 移除旧的 `idx_created_at_id` AutoMigrate 标签，目标替代索引为 `idx_logs_created_at_id(created_at, id)`。
- 从 `Log` 移除旧的 `idx_logs_upstream_request_id_created_at` AutoMigrate 标签，目标替代索引为 `idx_logs_upstream_request_created_at_id(upstream_request_id, created_at, id)`。
- 新增 `scripts/storage-log-index-maintenance.sh`。它只管理两个已知旧索引，默认为 `--dry-run`，验证替代索引，打印候选大小和使用情况，并要求 `--execute` 使用精确确认字符串。

验证命令：

```bash
bash -n scripts/storage-log-index-maintenance.sh scripts/storage-log-index-report.sh scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev scripts/storage-log-index-maintenance.sh --dry-run
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev CONFIRM_LOG_INDEX_MAINTENANCE=wrong scripts/storage-log-index-maintenance.sh --execute
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev CONFIRM_LOG_INDEX_MAINTENANCE=execute:new-api-dev-isolated-postgres-1:new-api-dev:drop-legacy-log-indexes scripts/storage-log-index-maintenance.sh --execute
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev REPORT_LIMIT=15 scripts/storage-log-index-report.sh
POSTGRES_CONTAINER=<dev-postgres-container> POSTGRES_USER=<db-user> POSTGRES_DB=<db-name> NEW_API_STATUS_URL=http://127.0.0.1:3001/api/status NEW_API_POSTGRES_DATA_DIR=<fast-storage-root>/new-api-caches/postgres/data NEW_API_POSTGRES_MARKER=<fast-storage-root>/new-api-caches/postgres/storage.marker NEW_API_POSTGRES_MARKER_VALUE=<storage-marker-value> scripts/storage-verify-postgres.sh
go test ./model -count=1
```

观察结果：

- dry-run 要求替代索引，并报告精确确认字符串 `execute:new-api-dev-isolated-postgres-1:new-api-dev:drop-legacy-log-indexes`。
- 错误确认返回退出码 `3`，没有删除索引。
- 执行模式使用 `DROP INDEX CONCURRENTLY IF EXISTS` 处理 `idx_created_at_id` 和 `idx_logs_upstream_request_id_created_at`。
- 隔离开发中，`logs` 索引大小从 `541 MB` 降至 `526 MB`，索引数量从 `41` 降至 `39`。
- 维护后计划仍对 `admin-recent` 使用 `idx_logs_created_at_id`，对 `upstream-request-id` 使用 `idx_logs_upstream_request_created_at_id`。
- `scripts/storage-verify-postgres.sh` 报告 marker 匹配，`logs_index_count=39`。
- `go test ./model -count=1` 通过。
- 验证后开发和正式 `/api/status` 均成功返回。

正式门禁：

- 在正式运行该维护脚本前，应在真实流量窗口采集 `scripts/storage-log-index-report.sh`，并将报告与操作记录一同保存。脚本有意不由服务启动或 AutoMigrate 调用。
- Claude 最终只读审查未发现阻断问题，指出 Compose overlay 命令替换边界；运维指南现已说明自定义 PostgreSQL 命令参数必须在 overlay 中同步。
- Gemini 后续审查指出恢复时容器可写层膨胀和执行时缺少 `ENV_FILE` 校验，两项均已修复。
- Claude 后续审查指出目标 PostgreSQL 主版本检查、备份剩余空间检查、Compose 后立即诊断容器退出、全局角色备份可见性和部分 marker 环境变量处理问题；已修复或记录明确的操作员边界。
- Gemini 第二次跟进指出 Windows 二进制 dump 流式写入和迁移计数伪成功风险。Windows dump 创建现在使用 base64 包装，迁移时验证会将恢复后的表计数与源计数比较；同一审查提出的 `template0` 和 TCP-auth 结论已对运行中的 PostgreSQL 容器核对，不适用于该部署。
- Claude 最终文件内容审查未发现剩余 P0/P1 缺陷。
- Gemini 最终只读跟进未发现剩余 P0/P1 缺陷。

## 日志热路径与脚本目标加固 2026-06-14

采样时间：`2026-06-14T02:45:00Z`。

本轮复核请求消费日志路径中残留的同步工作，以及日志索引脚本的运维安全性。正式环境未被修改。

变更：

- `RecordConsumeLog` 和 `RecordErrorLog` 现在优先从 `ContextKeyUserSetting` 读取 `RecordIpLog`。Token 鉴权已经将解析后的用户设置存入请求上下文，relay payload 审计初始化也使用该上下文值；对非请求或旧调用点保留回退到 `GetUserSetting(userId, false)`。
- 新增 model 测试，覆盖启用上下文的 IP 日志、上下文禁用 IP 日志覆盖已存储 true 值，以及没有上下文值时回退到已存储设置。
- `scripts/storage-log-index-report.sh` 和 `scripts/storage-log-index-maintenance.sh` 现在要求显式 `POSTGRES_CONTAINER` 和 `POSTGRES_DB`。没有显式目标的 dry-run 曾到达默认 `postgres/new-api` 容器；虽然未做修改，但在正式操作中容易误读。
- `scripts/storage-migrate-postgres.sh` 现在也要求显式 `POSTGRES_CONTAINER`、`NEW_API_CONTAINER` 和 `POSTGRES_DB`；执行后的验证调用会将相同目标值传给 `scripts/storage-verify-postgres.sh`。
- 迁移 dry-run 的计划命令现在使用 `would_run:` 前缀，执行模式使用 `run:`，避免将 dry-run 输出误认为实际停止容器或启动 Compose 命令。
- 当目标已有 PostgreSQL `PG_VERSION` 文件时，迁移 dry-run 现在打印 `target_data_dir_looks_like_pgdata=true` 和 `target_data_dir_pg_version=<major>`。
- `scripts/storage-verify-postgres.sh` 打印明确的 `target` 段，包括 PostgreSQL 容器、数据库、用户和 New API 状态 URL，并打印 `target_defaults_used` 与每个字段的来源标记，使默认目标在审查中可见。
- 运维指南的日志索引报告、维护和 PostgreSQL 验证示例现在使用明确目标占位符。
- 迁移 dry-run 现在在最终验证器调用前加 `would_run:` 前缀，并内联完整显式验证环境。

验证命令：

```bash
go test ./model -count=1
bash -n scripts/storage-log-index-maintenance.sh scripts/storage-log-index-report.sh scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
scripts/storage-log-index-maintenance.sh --dry-run
scripts/storage-log-index-report.sh
NEW_API_POSTGRES_DATA_DIR=/tmp/new-api-storage-test NEW_API_POSTGRES_MARKER=/tmp/new-api-storage-test.marker NEW_API_POSTGRES_MARKER_VALUE=test scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev scripts/storage-log-index-maintenance.sh --dry-run
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev REPORT_LIMIT=15 scripts/storage-log-index-report.sh
POSTGRES_CONTAINER=<dev-postgres-container> POSTGRES_USER=<db-user> POSTGRES_DB=<db-name> NEW_API_STATUS_URL=http://127.0.0.1:3001/api/status NEW_API_POSTGRES_DATA_DIR=<fast-storage-root>/new-api-caches/postgres/data NEW_API_POSTGRES_MARKER=<fast-storage-root>/new-api-caches/postgres/storage.marker NEW_API_POSTGRES_MARKER_VALUE=<storage-marker-value> scripts/storage-verify-postgres.sh
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev CONFIRM_LOG_INDEX_MAINTENANCE=execute:new-api-dev-isolated-postgres-1:new-api-dev:drop-legacy-log-indexes scripts/storage-log-index-maintenance.sh --execute
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env restart new-api
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev scripts/storage-log-index-maintenance.sh --dry-run
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev REPORT_LIMIT=15 scripts/storage-log-index-report.sh
POSTGRES_CONTAINER=<dev-postgres-container> POSTGRES_USER=<db-user> POSTGRES_DB=<db-name> NEW_API_STATUS_URL=http://127.0.0.1:3001/api/status NEW_API_POSTGRES_DATA_DIR=<fast-storage-root>/new-api-caches/postgres/data NEW_API_POSTGRES_MARKER=<fast-storage-root>/new-api-caches/postgres/storage.marker NEW_API_POSTGRES_MARKER_VALUE=<storage-marker-value> scripts/storage-verify-postgres.sh
go test ./controller ./model ./relay/common ./relay/helper ./service -count=1
git diff --check -- model/log.go model/log_admin_audit_test.go model/runtime_polling_index_test.go scripts/storage-log-index-maintenance.sh scripts/storage-log-index-report.sh scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh scripts/storage-plan-windows.ps1 docs/operations/storage-acceleration.md docs/reviews/CR-STORAGE-ACCELERATION-2026-06-12.md deploy/compose/docker-compose.storage-postgres-bind.yml deploy/env/storage-postgres.example.env .gitignore
```

观察结果：

- 没有 `POSTGRES_CONTAINER` 和 `POSTGRES_DB` 的两个脚本调用在任何 PostgreSQL 查询前以状态码 `2` 退出。
- 缺少 `POSTGRES_CONTAINER`、`NEW_API_CONTAINER` 和 `POSTGRES_DB` 的 `scripts/storage-migrate-postgres.sh --dry-run` 现在以 `error: POSTGRES_CONTAINER is required` 退出。
- 显式 dev-isolated 迁移 dry-run 打印 `execute:<dev-postgres-container>:<dev-new-api-container>:<db-name>:<fast-storage-root>/new-api-caches/postgres/data`，且 dry-run 模式不停止容器；计划命令带 `would_run:` 前缀。
- 同一 dry-run 对已迁移开发目标报告 `target_data_dir_looks_like_pgdata=true` 和 `target_data_dir_pg_version=15`。
- 验证期间，dev-isolated `new-api` 容器曾在旧镜像上重建，启动时重新创建 `idx_created_at_id` 和 `idx_logs_upstream_request_id_created_at`；这确认删除旧索引必须与当前镜像配套。
- 从当前工作区重建 `new-api-local:dev`，只替换 `new-api-dev-isolated-new-api-1`，执行开发维护脚本并重启开发容器。
- 重启后，dev-isolated 维护 dry-run 幂等，并报告 `drop_candidate_total_bytes=0`。
- dev-isolated 报告仍显示 `admin-recent` 使用 `idx_logs_created_at_id`，`upstream-request-id` 使用 `idx_logs_upstream_request_created_at_id`。
- 验证器报告配置的快速存储 marker 匹配，并显示 `logs_index_count=39`。
- 直接索引检查只返回 `idx_logs_created_at_id` 和 `idx_logs_upstream_request_created_at_id`，两个旧索引名均不存在。
