# CR-DEPLOY-BUILD-TRACEABILITY-2026-05-01

## Summary

本轮检查目标是确认正式 `new-api` 服务是否由当前本地最新源码构建，并收敛发现的部署与运行态异常。

结论：

- 运行中的旧镜像包含当前 `HEAD` 才有的代码特征，但镜像和二进制缺少直接 commit 元数据。
- `docker-compose.yml` 默认镜像仍指向旧本地标签，环境变量缺失时存在回退到旧镜像的风险。
- `redis` 容器仍在 `new-api` Compose project 内，但 Compose 路径标签存在 `/volumes/...` 小写漂移，和当前 canonical `/Volumes/...` 不一致。
- 使用日志中的 500 为上游 `api.gettoken.dev` 请求 `unexpected EOF`，不是本机服务版本不一致导致。
- `gpt-5.5 requires a newer version of Codex` 是上游返回的 400 客户端版本要求，与 500 不是同一类问题。

## Control Contract

- Primary Setpoint：正式服务的构建来源可追溯，Compose 默认值不指向旧镜像，运行态异常分类可复查。
- Acceptance：二进制输出 `--build-info`，Docker 镜像带 OCI revision label，Compose config 默认镜像为 `new-api-local:prod-main`，全量 Go 测试通过。
- Guardrail Metrics：不修改数据库 schema，不读取或输出密钥，不改变渠道配置，不在验证阶段覆盖正式镜像。
- Sampling Plan：基于 Git、Docker inspect、Docker logs、PostgreSQL logs 表、Dockerfile 实构建、测试命令多点采样。
- Recovery Target：若部署收敛失败，保留旧 `new-api-local:prod-main` 镜像可回退，Compose 仍能使用 `.env` 中的显式镜像。
- Rollback Trigger：健康检查失败、`/api/status` 不返回 success、容器脱离 Compose project、二进制无法启动。
- Boundary：允许修改构建元数据、Dockerfile、Compose 默认值、发布工作流、桌面 sidecar 构建脚本、审计文档；不改业务 API、DB schema、渠道密钥。
- Coupling Notes：构建链路影响 Docker 镜像、Release 二进制、Tauri sidecar；Compose 改动影响正式容器收敛路径。

## State Estimate / Root Cause

### 构建溯源不足

现象：

- `docker image inspect new-api-local:prod-main` 的 `Config.Labels` 为 `null`。
- 运行中 `/new-api` 可用 `go version -m` 查看依赖和 ldflags，但没有 `vcs.revision`。

根因：

- `.dockerignore` 排除了 `.git`，Docker 构建阶段无法自动嵌入 Go VCS 信息。
- Dockerfile 只写入 `common.Version`，没有写入 commit、build date、source，也没有 OCI labels。

### Compose 默认镜像漂移

现象：

- 正式容器运行 `new-api-local:prod-main`。
- `docker-compose.yml` 默认值为 `new-api-local:prod-20260412-080601`。

根因：

- 默认镜像标签没有随本地正式构建策略更新，部署实际依赖环境变量或手工指定。

### Compose 路径标签漂移

现象：

- `new-api` 与 `postgres` 的 `com.docker.compose.project.working_dir` 为 `/Volumes/Work/code/new-api`。
- `redis` 的同类标签为 `/volumes/work/code/new-api`。

根因推断：

- 历史 Compose 操作曾从错误 canonical path 或 Docker Desktop 解析路径下执行，导致 Redis 容器标签残留小写路径。

### 上游 500 与客户端 400

500 证据：

- 日志显示 `Post "https://api.gettoken.dev/v1/responses": unexpected EOF`。
- New API 记录 `status_code=500, upstream error: do request failed`。
- 涉及渠道为 `api.gettoken.dev-*`。

400 证据：

- 上游返回 `The 'gpt-5.5' model requires a newer version of Codex`。
- 这是客户端或上游协议版本要求，不是本地 Docker 构建问题。

## Changes

- 新增 `common.BuildCommit`、`common.BuildDate`、`common.BuildSource`。
- 新增 `--build-info`，保持 `--version` 只输出版本号。
- 启动日志从 `New API <version> started` 扩展为包含 commit 和 build date 的摘要。
- Dockerfile 支持 `APP_VERSION`、`VCS_REF`、`BUILD_DATE`、`SOURCE_URL` build args，并写入 OCI labels。
- 新增 `scripts/build-docker-local.sh`，本地构建默认生成 `new-api-local:prod-main` 并注入构建元数据。
- 本地构建脚本会在工作区存在未提交改动时追加 `-dirty`，避免镜像内容与 commit 元数据不一致。
- `docker-compose.yml` 默认镜像改为 `new-api-local:prod-main`。
- GitHub Docker 发布、Release 二进制、Tauri sidecar 构建都注入同一套构建元数据。
- README 增加本地 Docker 构建与核验命令。

## Verification

已执行：

```bash
go test ./common
go test ./relay/...
go test ./...
node --check desktop/tauri-app/scripts/prepare-sidecar.mjs
sh -n scripts/build-docker-local.sh
ruby -e 'require "yaml"; ARGV.each { |f| YAML.load_file(f); puts "yaml_ok=#{f}" }' .github/workflows/docker-image-alpha.yml .github/workflows/docker-image-arm64.yml .github/workflows/release.yml
git diff --check
docker compose --env-file /dev/null -f docker-compose.yml config
docker build --build-arg APP_VERSION=v1.1.0 --build-arg VCS_REF=<HEAD> --build-arg BUILD_DATE=2026-05-01T00:00:00Z --build-arg SOURCE_URL=https://github.com/MisonL/new-api -t new-api-buildinfo-check:tmp .
docker run --rm new-api-buildinfo-check:tmp --build-info
docker image inspect new-api-buildinfo-check:tmp
docker image rm new-api-buildinfo-check:tmp
git commit -m "chore: add build traceability metadata"
scripts/build-docker-local.sh new-api-local:prod-main
NEW_API_IMAGE=new-api-local:prod-main docker compose up -d new-api redis
docker compose up -d --force-recreate --no-deps redis
docker exec new-api /new-api --build-info
curl -fsS http://127.0.0.1:3000/api/status
docker compose ps
```

关键结果：

- `go test ./...` 通过。
- YAML 解析通过。
- Compose 渲染默认镜像为 `new-api-local:prod-main`。
- 临时镜像 `--build-info` 输出 version、commit、date、source。
- 临时镜像 OCI labels 包含 `org.opencontainers.image.revision=<HEAD>`。
- 临时镜像已删除，未覆盖正式镜像。
- 正式镜像已重建为 `new-api-local:prod-main`。
- 运行中 `/new-api --build-info` 输出 `commit=1b5518a8c31ac12ad69e83f0096807e2d69a8223`。
- `/api/status` 返回 `success=true`。
- `new-api`、`redis`、`postgres` 的 Compose project、service、working_dir、config_files 标签均已回到 `/Volumes/Work/code/new-api`。

## Recovery Evidence

- 本轮最终通过 Compose 重建了 `new-api`，并强制重建了 `redis` 以修正 Compose 路径标签。
- 若后续部署失败，可将 `.env` 中 `NEW_API_IMAGE` 改回上一标签后执行 `docker compose up -d`。
- 回滚验证信号：容器 health、`/api/status`、Compose labels、`--build-info`。

## Observability Evidence

本轮可观测点：

- Git：`HEAD=e4bfd11b08629ddb99e662d58ec86a996e86d400`，工作区基线来自 `main...origin/main`。
- Docker：旧正式镜像无 labels，新临时镜像有 revision/version/source/created labels。
- Compose：旧 Redis 标签存在 `/volumes/...` 漂移；新 Compose 渲染默认镜像为 `new-api-local:prod-main`。
- Logs：500 来自上游 EOF；400 来自上游客户端版本要求。
- Deploy：新启动日志包含 `New API v1.1.0 commit=1b5518a8c31ac12ad69e83f0096807e2d69a8223 built=2026-04-30T16:47:49Z started`。
- Deploy：启动后的上游模型巡检任务报告 `checked_channels=17 changed_channels=3 detected_remove_models=63 failed_channels=0`，这是现有控制面任务产生的检测状态写入风险。

## Residual Risks / Gate Boundary

- 上游 `api.gettoken.dev` EOF 需要由上游渠道稳定性、网络或认证链路继续排查；本轮只做分类和证据归档。
- `gpt-5.5` 400 需要升级调用端 Codex 或调整路由到兼容渠道；本轮不改客户端。
- 部署后仍观察到渠道 `#155` 的 `gpt-5.5` 上游 429，这属于上游限流或并发容量问题，不是本轮构建溯源问题。
- 上游模型巡检任务会在服务启动后检测并持久化差异状态；若要求部署零副作用，需要单独设计启动期巡检开关或冷却窗口。
