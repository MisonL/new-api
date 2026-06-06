# CR-HEADER-PROFILE-NPM-LATEST-2026-06-03

## 范围

- 将 AI Coding CLI 内置 Header Profile 默认版本语义调整为 `latest`。
- 后端通过后台定时任务获取并记录 npm 可用版本清单，运行时从记录清单解析 `latest`，不在请求路径直接访问 npm registry。
- classic 和 default WebUI 提供请求头模板选择入口，支持固定、轮询、随机三种模式；轮询和随机模式支持多模板。
- 覆盖 macOS、Linux、Windows 的 x64 与 arm64 平台快照，并对非法版本字符串做拒绝或回退。
- 修复 default WebUI 中启用空模板策略后无法继续选择模板的问题：编辑态允许临时空选择，提交前清理为空的策略。

## 验证

采样时间：2026-06-04 01:08:00 CST。

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go test -count=1 ./controller ./model ./service` | 0 | controller 保存链路、渠道缓存/搜索、运行时请求头策略、npm 记录清单相关回归通过 |
| `go test ./controller ./model ./relay/common ./relay/helper ./service` | 0 | 仓库要求的最小相关后端包回归通过 |
| `cd web/classic && bun test src/components/table/channels/modals/headerProfile.helpers.test.js` | 0 | classic Header Profile 测试通过，覆盖 latest 选项、平台元数据、非法版本拒绝、轮询/随机多模板和固定模式替换 |
| `cd web/classic && bun run lint` | 0 | classic Prettier 检查通过 |
| `cd web/classic && bun run eslint` | 0 | classic ESLint 通过 |
| `cd web/classic && bun run i18n:lint` | 0 | classic i18n lint 未新增超出基线的问题 |
| `bun test web/default/tests/channel-header-profile-strategy.test.ts web/default/tests/channel-responses-compact.test.ts` | 0 | default Header Profile 策略、空策略提交清理、Responses compact 回归通过 |
| `cd web/default && ./node_modules/.bin/eslint src/features/channels/components/drawers/channel-mutate-drawer.tsx src/features/channels/components/header-profile-strategy-editor.tsx src/features/channels/lib/header-profile-utils.ts tests/channel-header-profile-strategy.test.ts` | 0 | default 本次改动文件 ESLint 通过 |
| `node --test web/tests/paramOverridePresetContracts.test.mjs scripts/dockerfile-contract.test.mjs` | 0 | 参数覆盖编辑器契约与 Dockerfile 版本/metadata 契约通过 |
| `sh -n scripts/go-test-all.sh scripts/build-docker-local.sh scripts/write-frontend-release-metadata.sh` | 0 | shell 脚本语法通过 |
| `tmpdir=$(mktemp -d) && sh scripts/write-frontend-release-metadata.sh ... && node -e 'JSON.parse(...)'` | 0 | frontend release metadata 写入和 JSON 解析通过 |
| `git diff --check` | 0 | 无空白错误 |
| `scripts/build-docker-local.sh new-api-local:dev` | 0 | 最新 dirty 源码镜像构建通过；Docker 内 default 和 classic 生产前端构建、Safari compatibility check、Go binary build 均通过 |
| `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` | 0 | 3001 隔离开发环境 new-api 容器已替换并启动 |
| `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` | 0 | 运行容器 build info 为 `v1.1.0 54e22200f433132927f928c0d370cb5f4fcfb30b-dirty 2026-06-03T17:04:01Z` |
| `curl -fsS http://127.0.0.1:3001/api/status` | 0 | 3001 隔离开发环境状态接口返回 `success: true` |
| `docker inspect ... new-api-dev-isolated-new-api-1` | 0 | 3001 new-api 容器状态为 `running healthy`，镜像为最新 `new-api-local:dev` |
| `docker logs new-api-dev-isolated-new-api-1 --tail 200 \| rg -n "npm cli version"` | 0 | 定时刷新任务启动并完成，日志显示 `refreshed=5 failed=0` |
| `docker exec new-api-dev-isolated-postgres-1 psql ... NpmCLIVersionRecordedOptions ...` | 0 | 隔离 PostgreSQL 中已持久化 5 个 npm 包清单：`@openai/codex`、`@anthropic-ai/claude-code`、`@google/gemini-cli`、`@qwen-code/qwen-code`、`droid` |
| `curl -fsS http://127.0.0.1:13000/api/status` | 0 | 正式容器状态接口正常；本轮未替换正式容器 |

## 结论

本轮覆盖后端运行时、controller 保存链路、classic 和 default WebUI、参数覆盖编辑器契约、i18n、Docker 构建、3001 隔离开发环境运行态和 npm latest 清单持久化。default WebUI 已修复"启用模板但未选择时无法继续添加模板"的问题；提交前仍会清理空策略，避免写入无效配置。

当前 3001 开发环境运行的是包含未提交工作区改动的 `dirty` 镜像，可用于开发验证；正式环境容器未在本轮替换，升级正式服务前应以提交后的干净 commit 镜像为准。
