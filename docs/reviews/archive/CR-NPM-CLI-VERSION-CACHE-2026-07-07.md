# CR-NPM-CLI-VERSION-CACHE-2026-07-07

## 范围

- npm CLI 版本选项缓存与刷新契约。
- Header Profile 库的版本来源、刷新时间、缓存年龄和最近错误展示。
- npm 版本选项接口的 AdminAuth 与限流保护。
- 用量日志展示的请求头策略回退。
- 3001 端口的隔离开发栈。

## 代码审查摘要

- OMP 只读审查：没有剩余 P0/P1/P2/P3 可执行问题。审查确认 GET 版本选项只读取缓存或数据库，POST 持久化失败不会污染进程缓存，且已覆盖 `request_header_policy {}` 回退。
- Claude 只读审查：没有 P0/P1 问题。建议补充 `request_header_policy: null` 覆盖，现已由 `web/default/tests/request-header-policy.test.ts` 覆盖。
- 后续本地审查发现一个响应契约缺口：刷新失败只有顶层 `code`，没有 `data.code`。已在 `controller/npm_cli_version.go` 修复，并由 `controller/npm_cli_version_test.go` 覆盖。
- 第二次 OMP 只读审查发现诊断接口暴露原始后端错误消息。现改为在 `last_error.message` 返回基于代码的安全摘要，原始细节只保留在后端日志中。
- 第二次 OMP 只读审查还指出 MySQL 文本比较可能受排序规则影响。当 `common.UsingMySQL` 为 true 时，CAS 更新现在使用 `BINARY value = BINARY ?`；后续 MySQL 8.4 真实 smoke 确认了预期的 `RowsAffected` 语义和大小写敏感比较行为。
- 第三次审查在受控验证环境使用 CLI 工具。`claude` 2.1.179 发现缺少 package 的诊断会将 Go 零值 `time.Time` 序列化为 `0001-01-01T00:00:00Z`；现通过将诊断 `refreshed_at` 改为可选字段修复，并由 service/controller 测试覆盖。该审查还发现配套运维说明把权限不足描述为真实 HTTP 403；说明已改为反映现有 `AdminAuth` 约定，即权限不足返回 `HTTP 200 + success:false`。
- 受控 `omp` 16.3.4 审查发现默认 `newapi-responses/gpt-5.5` 请求因 `400 Unsupported parameter: max_output_tokens` 失败，Google Antigravity 模型探针也因 provider 错误失败。之后 `newapi-chat-completions/deepseek-v4-flash` 重试未发现 P0/P1 问题。审查指出普通 GET 选项响应存在同样的零时间风险；`NpmCLIVersionOptionsResponse.refreshed_at` 也已改为可选并由 service/controller 测试覆盖。
- 最终扫描发现一个前端体验边界：诊断 GET 没有显式超时，而版本 GET 和刷新请求有。default 与 classic 诊断请求现在统一使用 10 秒有界超时，源码测试锁定该契约。
- 最终 CLI 复核在受控验证环境使用 `claude` 2.1.179 和 `omp` 16.3.4。`omp` 未报告 P0/P1/P2/P3；`claude` 只报告非阻断边界。服务现在将未来时间戳的诊断 `cache_age_ms` 截断为 0，测试锁定 helper 契约。`last_errors` 合并行为是有意设计：保留晚于 package 刷新的错误，避免诊断丢失后续失败原因。
- 后续前端清理解决了剩余非阻断观察项：default 和 classic 手动版本重载处理器在卸载后不再操作，classic 诊断 HTTP 错误在回退到通用抛出错误代码前使用相同的响应 payload/status 归一化。
- 最终 CLI 交叉复核使用 `claude` 2.1.179 和 `omp` 16.3.4。唯一确认的清理项是 classic 诊断在没有已知后端代码时把 Axios 内部 `error.code` 传给 UI 分类；现仅接受已知 npm 版本错误代码，未知传输代码回退为 `npm_version_load_failed`。
- 最终优化在后端响应加入诊断 `summary` 和进程内刷新 `metrics`。default 与 classic 的管理性能设置页现在提供只读 npm CLI 版本诊断入口，操作员无需打开 Header Profile 模板选择器即可查看已记录或缺失的 package、缓存年龄、安全的最近错误代码和计划/手动刷新状态。
- 后续 Claude CLI 审查发现上下文与鉴权测试加固点。没有采用“普通用户诊断必须返回 HTTP 401”的建议，因为 `middleware.AdminAuth()` 有意对权限不足返回 `HTTP 200 + success:false`。可执行项均已处理：GET 版本选项将请求上下文传入数据库回退读取，刷新持久化/CAS 读写传递调用方上下文，最近错误持久化日志保留调用方上下文，未认证路由测试不再发送 `New-Api-User` 头。剩余 P3 说明是成功手动刷新时 `last_manual_code` 为空，这是有意设计，因为成功时省略该字段且 UI 显示 `-`；成功/失败计数器承载成功状态。最后一次 OMP 16.3.4 CLI 复核使用 `-p @prompt-file` 调用，但在 180 秒截止前没有返回输出，因此不计为通过审查。
- 2026-07-08 后续工作关闭优化清单中的剩余可观测性缺口。同 package 手动刷新现在通过进程内 singleflight 合并，诊断返回有界的 `recent_errors` 历史和稳定的 `recommended_action`，default/classic 管理诊断均展示这些字段。`last_errors` 仍表示当前阻断状态；`recent_errors` 是保留历史，不会因后续刷新成功而删除。
- 2026-07-08 最终 smoke 发现一个响应契约边界：空的诊断 `recent_errors` 切片因 `omitempty` 被省略，操作员无法依赖稳定数组字段。`NpmCLIVersionDiagnostic.recent_errors` 现在始终序列化为数组，controller 测试检查原始 JSON 契约。
- 2026-07-08 最终 CLI 交叉复核使用带所需 Node 运行时的 `omp` 16.3.4 和 `claude` 2.1.179。大型 OMP bundle 因无输出被中断，小型 diff bundle 返回了可用结果。两项工具均未报告 P0/P1/P2 阻断；Claude 只列出非阻断权衡：后台刷新使用 `context.Background()`，每个 package 的持久化写入比单批写入更细粒度，同 package `singleflight` 使用第一个调用方上下文。

## 验证

命令于 2026-07-07 和 2026-07-08 从 `<repository-root>` 执行。

### Go

```bash
go test -count=1 -timeout=180s ./controller ./service -run 'NpmCLI|NpmVersion|npmVersion'
```

结果：通过。

第三次 CLI 审查跟进后，重新执行同一命令并再次通过，确认诊断可选 `refreshed_at` 修复。

最终真实 CLI 复核跟进后，重新执行 controller/service/router 定向命令并再次通过，确认 `cache_age_ms` 非负截断。

完成最终诊断摘要和刷新指标后：

```bash
go test -count=1 -timeout=180s ./service ./controller ./router -run 'NpmCLI|NpmVersion|npmVersion|TestChannelNpmVersionOptionRoutesUseAdminAuthAndCriticalRateLimit'
```

结果：通过。

完成 Claude CLI P2 跟进（GET 回退请求上下文、刷新持久化上下文和诊断鉴权覆盖）后：

```bash
go test -count=1 -timeout=180s ./service ./controller ./router -run 'NpmCLI|NpmVersion|npmVersion|TestChannelNpmVersionOptionRoutesUseAdminAuthAndCriticalRateLimit'
```

结果：通过。

完成 2026-07-08 singleflight 和诊断历史跟进后：

```bash
gofmt -w service/npm_cli_version.go service/npm_cli_version_cache.go service/npm_cli_version_diagnostics.go service/npm_cli_version_test.go
go test -count=1 -timeout=180s ./service -run 'NpmCLI|NpmVersion|npmVersion'
```

结果：通过。

```bash
go test -count=1 -timeout=180s ./router -run TestChannelNpmVersionOptionRoutesUseAdminAuthAndCriticalRateLimit
```

结果：通过。

第三次 CLI 审查跟进后，重新执行同一命令并再次通过。

完成 2026-07-08 稳定 `recent_errors` 响应契约修复后：

```bash
go test -count=1 -timeout=180s ./controller ./service -run 'NpmCLI|NpmVersion'
go test -count=1 -timeout=180s ./controller ./model ./relay/common ./relay/helper ./service
```

结果：通过。

### 前端单元测试

```bash
bun test ./tests/channel-header-profile-strategy.test.ts ./tests/request-header-policy.test.ts
```

结果：通过 35，失败 0。

最终真实 CLI 复核跟进后，重新执行同一 default 前端命令并再次通过。

```bash
bun test src/components/table/channels/modals/headerProfile.helpers.test.js
```

结果：通过 105，失败 0。

最终真实 CLI 复核跟进后，重新执行同一 classic 前端命令并再次通过。

最终 CLI 交叉复核清理后，重新执行同一 classic 前端命令并再次通过，确认诊断错误代码归一化。

### 前端构建

```bash
bun run lint
```

目录：`web/classic`。

结果：通过，所有匹配文件均符合 Prettier 风格。

以下较早的前端命令因超过 15 分钟没有继续输出而被中断：

```bash
cd web/default && bun run typecheck
cd web/default && bun run build
cd web/classic && bun run build
```

观察到的边界：

- `web/default` 构建打印 `ready built in 4m 19.3s` 后，外层包装命令没有退出。
- `web/default` typecheck 和 `web/classic` 构建停留在长时间文件系统等待。
- 最终构建验证通过 Docker 完成，两个前端和 Go 均构建成功。

完成最终诊断超时跟进后，在加载 nvm Node 路径的情况下重新执行 `web/default` typecheck：

```bash
cd web/default && bun run typecheck
```

结果：通过。TypeScript 增量缓存建立后命令完成；没有缩减脚本或 tsconfig 覆盖范围。

完成最终诊断摘要和设置页验证后：

```bash
cd web/default && bun test ./tests/channel-header-profile-strategy.test.ts ./tests/request-header-policy.test.ts
```

结果：通过 36，失败 0。

```bash
cd web/classic && bun test src/components/table/channels/modals/headerProfile.helpers.test.js
```

结果：通过 106，失败 0。

```bash
cd web/default && bun run lint
```

结果：通过。

```bash
cd web/default && bun run build
```

结果：通过。`rsbuild` 用时 1 分 39.5 秒完成；Safari 兼容性重写和兼容性检查通过。

```bash
cd web/classic && bun run lint
```

结果：通过，所有匹配文件均符合 Prettier 风格。

```bash
cd web/classic && bun run build
```

结果：通过。Vite 用时 4 分 56 秒完成；Safari 兼容性重写和兼容性检查通过。

同一轮最终验证还尝试执行：

```bash
cd web/default && bun run typecheck
```

边界：约 7 分钟没有 TypeScript 诊断输出后被中断。之后实际的 `web/default` 生产构建通过，并覆盖了新增 TSX 文件。

### Docker 隔离构建

```bash
scripts/build-docker-local.sh new-api-local:dev
```

结果：通过。

构建信息：

```text
built image=new-api-local:dev version=v1.1.0 commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty date=2026-07-07T12:57:13Z
```

Docker 构建完成以下步骤：

- `web/default` rsbuild 构建。
- `web/default` Safari 兼容性重写和兼容性检查。
- `web/classic` vite 构建。
- `web/classic` Safari 兼容性重写和兼容性检查。
- Go 生产二进制构建。

完成最终诊断摘要和设置页验证后，重新构建隔离镜像：

```bash
scripts/build-docker-local.sh new-api-local:dev
```

结果：通过。

构建信息：

```text
built image=new-api-local:dev version=v1.1.0 commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty date=2026-07-07T15:47:36Z
```

完成最终上下文加固后，重新构建隔离镜像。

结果：通过。

构建信息：

```text
built image=new-api-local:dev version=v1.1.0 commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty date=2026-07-07T17:04:15Z
```

完成 2026-07-08 稳定 `recent_errors` 响应契约修复后，重新构建隔离镜像。

结果：通过。

构建信息：

```text
built image=new-api-local:dev version=v1.1.0 commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty date=2026-07-08T02:35:28Z
```

### 3001 隔离运行时

```bash
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker inspect --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' new-api-dev-isolated-new-api-1
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

结果：

```text
running healthy
version=v1.1.0
commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty
date=2026-07-07T12:57:13Z
source=https://github.com/MisonL/new-api
/api/status success=true
```

完成最终诊断摘要和设置页验证后，重新强制创建隔离容器。

结果：

```text
running healthy
version=v1.1.0
commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty
date=2026-07-07T15:47:36Z
source=https://github.com/MisonL/new-api
/api/status success=true theme=classic
```

完成最终上下文加固后，重新强制创建隔离容器。

结果：

```text
running healthy
version=v1.1.0
commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty
date=2026-07-07T17:04:15Z
source=https://github.com/MisonL/new-api
/api/status success=true theme=classic
```

完成 2026-07-08 稳定 `recent_errors` 响应契约修复后，重新强制创建隔离容器。

结果：

```text
running healthy
version=v1.1.0
commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty
date=2026-07-08T02:35:28Z
source=https://github.com/MisonL/new-api
/api/status success=true version=v1.1.0 theme=classic
```

### 3001 API Smoke

临时管理员和普通用户被写入隔离 PostgreSQL 数据库，使用一次性的 `access_token` 通过同一 `AdminAuth()` 路径执行 smoke 后删除；token 值未打印。

结果：

```text
unauth_get_http=401 success=false message=Unauthorized, not logged in and no access token provided
common_get_http=200 success=false message=Unauthorized, insufficient privileges
admin_get_http=200 success=true source=recorded latest_version=0.142.5 options=6 has_refreshed_at=true
admin_diag_http=200 success=true packages=5 sources=recorded last_error_count=1 negative_cache_age_count=0 zero_time_present=false
admin_post_unsupported_http=200 success=false top_code=npm_package_unsupported data_code=npm_package_unsupported source=npm message=unsupported npm package: @new-api/nonexistent-smoke-package
```

完成最终诊断摘要和设置页验证后，第二次隔离诊断 smoke 写入临时管理员 access token，调用 `GET /api/channel/npm_version_options/diagnostics`，只打印聚合字段，然后删除用户。

结果：

```text
success=true
packages=5
summary_package_count=5
summary_recorded_count=5
summary_missing_count=0
summary_last_error_count=0
metrics_scheduled_runs=1
metrics_manual_runs=0
has_generated_at=true
refresh_interval_ms=600000
registry_timeout_ms=5000
has_negative_cache_age=false
```

完成最终上下文加固后，针对隔离容器重新执行同一诊断 smoke。

结果：

```text
success=true
packages=5
summary_package_count=5
summary_recorded_count=5
summary_missing_count=0
summary_last_error_count=0
metrics_scheduled_runs=1
metrics_manual_runs=0
has_generated_at=true
refresh_interval_ms=600000
registry_timeout_ms=5000
has_negative_cache_age=false
```

完成 2026-07-08 稳定 `recent_errors` 响应契约修复后，针对隔离容器重新执行同一 AdminAuth smoke。

结果：

```text
unauth_http=401 success=false message=Unauthorized, not logged in and no access token provided
common_http=200 success=false message=Unauthorized, insufficient privileges
admin_diag_http=200 success=true packages=5 summary_package_count=5 summary_recorded_count=5 summary_missing_count=0 summary_last_error_count=0 metrics_scheduled_runs=1 metrics_manual_runs=0 has_generated_at=true all_have_recent_errors=true all_have_recommended_action=true has_negative_cache_age=false
admin_get_http=200 success=true source=recorded latest_version=0.143.0 options=6 has_refreshed_at=true
```

解释：

- 未认证请求以 HTTP 401 拒绝。
- 普通用户遵循现有中间件约定：HTTP 200，`success=false`，并返回权限不足消息。
- 管理员 GET 读取后端已记录的缓存。
- 管理员诊断返回 package 状态。
- 管理员诊断可能包含不支持 package 的 POST smoke 产生的安全最近错误；该值仍基于错误代码，不含原始后端错误。
- 管理员 POST 刷新对不支持 package 返回结构化的顶层和 `data.code` 失败信息。

### 正式只读检查

2026-07-07 对本地正式容器执行了只读检查，未修改正式状态。

命令：

```bash
docker exec new-api /new-api --build-info
curl -fsS http://127.0.0.1:13000/api/status
```

结果：

```text
version=v1.1.0
commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty
date=2026-06-28T15:27:03Z
source=https://github.com/MisonL/new-api
/api/status success=true theme=classic version=v1.1.0
```

解释：正式环境仍运行 2026-06-28 构建，不是最终隔离开发构建。

### PostgreSQL CAS Smoke

真实 PostgreSQL 容器：`new-api-dev-isolated-postgres-1`，数据库 `new-api-dev`，表 `options`。

检查：

```text
cas_first_rows=1
cas_stale_rows=0
cas_large_rows=1
```

解释：

- 当前值的精确 compare-and-swap 更新一行。
- 过期值的 compare-and-swap 更新零行。
- PostgreSQL 支持 200KB JSON 文本值比较。

### MySQL CAS Smoke

真实临时 MySQL 容器：`mysql:8.4`，表 `options`，排序规则 `utf8mb4_general_ci`。第一次拉取镜像因 Docker registry EOF 失败，重试后成功；smoke 完成后删除容器。

检查：

```text
cas_first_rows=1
cas_stale_rows=0
cas_case_seed_rows=1
cas_case_sensitive_rows=0
cas_large_rows=1
large_len=200015
```

解释：

- 当前值的精确 compare-and-swap 更新一行。
- 过期值的 compare-and-swap 更新零行。
- 即使使用 `utf8mb4_general_ci`，`BINARY value = BINARY ?` 仍保持大小写敏感；`"CASE"` 不匹配 `"case"`。
- MySQL 8.4 支持 200KB JSON 文本值比较。

## 结论

当前代码已覆盖之前容易混淆的 npm 版本失败路径：

- GET 使用后端缓存或已记录的数据库数据，不访问 npm registry。没有记录时，后端返回稳定错误代码，前端使用内置回退选项。
- POST 刷新受管理员保护，返回结构化错误代码，持久化失败时不会污染缓存。
- default 和 classic Header Profile 对话框展示来源、刷新时间、缓存年龄和安全的最近错误摘要。
- default 和 classic 手动版本重载处理器防止组件卸载后的状态更新。
- classic 诊断不再把 Axios 内部 `ERR_*` 传输代码当作 npm 版本业务代码。
- 即使历史为空，诊断 `recent_errors` 也始终是稳定数组字段。
- 即使从时钟偏移或测试夹具加载未来时间戳，诊断 `cache_age_ms` 也不会为负数。
- 权限/会话失败可以与 npm registry 或后端缓存失败区分。
- 隔离 3001 环境使用该工作区构建的 dirty 镜像，并通过上述 smoke 检查。
