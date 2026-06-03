# CR-HEADER-PROFILE-NPM-LATEST-2026-06-03

## 范围

- 将 AI Coding CLI 内置 Header Profile 默认版本语义调整为 `latest`。
- 后端通过后台定时任务获取并记录 npm 可用版本清单，运行时从记录清单解析 `latest`，不在请求路径直接访问 npm registry。
- classic WebUI 提供 `latest` 默认选项与系统平台选择，保存 `version_meta.platform`。
- classic WebUI 在固定、轮询、随机三种模式下明确区分“替换”和“添加/管理”模板，轮询/随机模式支持多模板追加。
- 覆盖 macOS、Linux、Windows 的 x64 与 arm64 平台快照，并对非法版本字符串做拒绝或回退。

## 验证

采样时间：2026-06-03 13:56:00 CST。

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go test -timeout 60s ./dto ./service ./controller -run 'HeaderProfile\|NpmCLI\|NpmVersion\|BuildFetchModelsHeaders' -count=1` | 0 | Header Profile latest 解析、npm 记录清单、controller 保存、fetch models 请求头测试通过 |
| `go test -count=1 -timeout 120s ./dto ./controller ./model ./relay/common ./relay/helper ./service` | 0 | 仓库要求的最小相关后端包回归通过，未使用测试缓存 |
| `cd web/classic && bun test src/components/table/channels/modals/headerProfile.helpers.test.js` | 0 | 78 个 classic Header Profile 测试通过，覆盖 latest 选项、平台元数据、非法版本拒绝、轮询模式追加多模板、固定模式替换 |
| `cd web/classic && bun run lint` | 0 | classic 前端 Prettier 检查通过 |
| `cd web/classic && bun run build` | 0 | classic 前端生产构建通过，Safari compatibility rewrite 更新 0 个文件，Safari compatibility check passed |
| `git diff --check` | 0 | 无空白错误 |
| `node - <<'NODE' ... fetch('https://registry.npmjs.org/<package>') ... NODE` | 0 | `@openai/codex`、`@anthropic-ai/claude-code`、`@google/gemini-cli`、`@qwen-code/qwen-code`、`droid` 当前均可从 npm registry 获取 latest |
| `rg -n "npm view\|registry\\.npmjs\\.org\|fetch\\(\|axios\\(\|npm_version_options\|latest" ...` | 0 | 前端只调用本项目 `/api/channel/npm_version_options`；npm registry 直接访问仅保留在后端定时刷新实现和相关测试中 |
| `scripts/build-docker-local.sh new-api-local:dev` | 0 | 重新构建隔离开发镜像，build info 为 `v1.1.0 d1f32334b57f12c845ecd97dbed63437bd08b5eb-dirty 2026-06-03T05:50:27Z` |
| `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` | 0 | 3001 隔离开发环境 new-api 容器已替换并启动 |
| `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` | 0 | 运行容器 build info 为 `v1.1.0 d1f32334b57f12c845ecd97dbed63437bd08b5eb-dirty 2026-06-03T05:50:27Z` |
| `curl -fsS http://127.0.0.1:3001/api/status` | 0 | 3001 隔离开发环境状态接口返回 `success: true` |
| `docker logs new-api-dev-isolated-new-api-1 --tail 300 \| rg -n "npm cli version"` | 0 | 定时刷新任务启动，日志显示 `refreshed=5 failed=0` |
| `docker exec new-api-dev-isolated-postgres-1 psql ... NpmCLIVersionRecordedOptions ...` | 0 | 隔离 PostgreSQL 中已持久化 5 个 npm 包清单，`@openai/codex` 记录 latest 为 `0.136.0` |
| `rg -n "添加/管理模板\|点击模板可追加或取消选择\|管理客户端模板" web/classic/dist/assets` | 0 | classic 构建产物包含多模板入口与追加/取消提示 |

## 结论

本轮覆盖后端运行时、controller 保存链路、classic WebUI、i18n、文档口径和 3001 隔离开发容器运行态。`web/default` 当前未实现同一套 Header Profile 模板库，未发现需要同步的同类入口。

当前 3001 开发环境运行的是包含未提交工作区改动的 `dirty` 镜像，可用于开发验证；正式环境升级前应以提交后的干净 commit 镜像为准。
