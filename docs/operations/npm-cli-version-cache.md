# npm CLI 版本缓存机制

采样时间：2026-07-08。

## 机制

- 后端记录的配置项为 `NpmCLIVersionRecordedOptions`。
- `GET /api/channel/npm_version_options` 只读取已记录缓存，不实时访问 npm registry。
- `POST /api/channel/npm_version_options/refresh` 触发一次手动刷新，成功后先写入数据库，再更新进程内缓存。同一进程内相同包名的并发手动刷新会通过 singleflight 合并，只执行一次 registry 读取和持久化；不同进程仍依赖数据库 CAS 合并保证一致性。
- 后台定时任务每 10 分钟执行一次，只在 master 节点访问 npm registry。
- 多节点或并发刷新时，持久化逻辑会先读取数据库中的现值，再和本次结果合并，并用 CAS 更新，避免旧内存快照覆盖数据库里更新的包记录。
- CAS 写入冲突会短退避加轻量 jitter 后重试，降低多个实例同时刷新时一起耗尽重试次数的概率。
- MySQL 下 CAS 使用 `BINARY value = BINARY ?` 做值比较，避免默认 collation 影响整段 JSON 文本比较；SQLite/PostgreSQL 使用普通 `value = ?`。
- `NpmCLIVersionRecordedOptions` 同时记录包版本快照、`last_errors` 和 `recent_errors`。`last_errors` 表示当前仍未被成功刷新覆盖的阻断错误；`recent_errors` 是每个包最多 5 条的最近失败历史。
- 成功刷新某个包只会清除 `updated_at` 不晚于本次 `fetched_at` 的当前持久化错误，不会删除 `recent_errors` 历史。若另一个节点在成功刷新之后记录了更新的失败原因，CAS 合并必须保留该当前错误。
- 进程启动会先尝试从 `OptionMap` 读取记录。若数据库还没有记录，前端只能显示内置版本。
- 读取接口在进程缓存未命中时，会只读数据库中的 `NpmCLIVersionRecordedOptions` 单项并重新加载本进程缓存。该路径不访问 npm registry，用于缩短非 master 节点等待 `OptionMap` 周期同步的窗口。
- 若单项读库因数据库未初始化或读库异常失败，后端记录 warn 并继续按未记录缓存处理。该口径避免把冷启动无缓存放大为前端硬错误；排查时应结合后端日志确认是否存在数据库异常。
- 诊断接口的只读数据库查询会使用当前 HTTP request context。若客户端取消请求，GORM 能跟随请求上下文取消后续 DB 操作。
- 诊断接口优先显示当前进程内的 `last_error`，此时 `last_error_scope=process`；若当前进程没有错误但数据库记录中存在错误，则显示 `last_error_scope=recorded`。
- 诊断接口每个包返回 `recommended_action` 和 `recent_errors`。`recommended_action` 用于区分“刷新包版本”、“检查 npm registry 连通性”、“检查数据库持久化”、“检查 registry 元数据”和“检查后台刷新或 master 节点”；`recent_errors` 用于判断故障是否刚刚恢复、重复发生或从未记录。
- 诊断接口返回的 `last_error.message` 是按错误码生成的安全摘要，不返回原始数据库、网络或 registry 错误文本；原始错误只写入后端日志。
- 诊断接口的 `cache_age_ms` 由后端限制为非负数。若记录时间来自未来时间戳，接口按 `0` 返回，避免时钟漂移污染展示。
- 诊断接口同时返回 `summary`、`metrics`、`generated_at`、`refresh_interval_ms` 和 `registry_timeout_ms`。其中 `summary` 汇总包数量、已记录数量、缺失数量、错误数量和缓存年龄；`metrics` 区分后台定时刷新与人工刷新，记录运行次数、成功/失败包数量、最近人工刷新包名与错误码。
- 后台定时刷新结束时会记录刷新指标；手动刷新无论成功、registry 失败、持久化失败、包名为空或包不支持，都会记录一次人工刷新尝试。
- `metrics` 是进程内诊断状态，用于辅助当前进程排障；跨实例长期审计仍以 `NpmCLIVersionRecordedOptions`、数据库记录和后端日志为准。

## 前端报错口径

前端提示“npm 版本加载失败，已使用内置版本”不等于浏览器直连 npm 失败。该提示表示前端请求本机后端接口失败，或后端接口没有可用的已记录版本。

常见状态：

- `npm_version_not_recorded`：后端缓存尚未记录该 npm 包，通常发生在新部署、首次启动、后台刷新还未成功时。
- `npm_version_empty`：接口返回成功，但没有可用版本选项。
- `npm_version_auth_required`：未登录或会话失效。该 code 来自前端对认证中间件 HTTP 401 的归一化，不是 npm 业务层常量。
- `npm_version_forbidden`：当前用户无管理权限。该 code 来自前端对权限中间件响应的归一化；当前 `AdminAuth` 权限不足路径返回 `HTTP 200 + success:false`，真实 HTTP 403 也会被归一化为同一 code。
- `npm_version_rate_limited`：刷新接口被限流。该 code 来自前端对限流中间件 HTTP 429 的归一化，不是 npm 业务层常量。
- `npm_version_load_failed`：网络、超时或未知接口错误。
- `npm_version_persist_failed`：手动刷新已拿到 npm 版本，但写入数据库记录失败。
- `npm_registry_timeout`：访问 npm registry 超时。
- `npm_registry_http_status`：npm registry 返回非 2xx 状态。
- `npm_registry_decode_failed`：npm registry 元数据解码失败或超过大小限制。

未登录或没有权限时，前端不会证明后台缓存不存在；它只能说明当前请求没有拿到接口结果。

## HTTP 状态与业务错误

- npm 版本接口的业务失败使用 `HTTP 200 + success:false + code` 返回，前端按 `code` 分类并显示降级提示。
- 登录失效和限流仍由中间件拦截，分别返回真实的 `401` 或 `429`。当前 `AdminAuth` 权限不足路径返回 `HTTP 200 + success:false`，前端按响应消息归一化为 `npm_version_forbidden`；如果未来中间件返回真实 `403`，前端也会归一化为同一 code。
- default 前端请求必须同时设置 `skipBusinessError:true` 和 `skipErrorHandler:true`，避免全局 toast 与组件内 fallback 提示重复弹出。
- classic 前端至少需要设置 `skipErrorHandler:true`；classic 响应拦截器只处理 HTTP error，不处理 `HTTP 200 + success:false` 业务错误，因此不需要 `skipBusinessError:true`。
- classic 手动刷新通过 `API.post(url, null, requestOptions)` 调用 `POST /api/channel/npm_version_options/refresh`，不能退回到 GET。
- default 与 classic 手动刷新入口在组件卸载后直接忽略，避免异步请求返回前后触发无意义的状态更新。
- classic diagnostics 只把已知 npm 版本业务错误码用于 UI 分类；未知 Axios `ERR_*` transport code 会降级为 `npm_version_load_failed`。
- 管理端性能设置页提供只读 npm CLI 版本诊断入口。default 与 classic 都只调用 `GET /api/channel/npm_version_options/diagnostics`，不触发 `POST /refresh`，用于区分后台缓存缺失、权限失败、DB 持久化失败和 registry 失败，并展示建议动作和最近错误历史。
- 前端版本加载、手动刷新和诊断请求超时应高于后端 npm registry 请求超时，给解码、数据库持久化和诊断读取留出余量；当前 default 与 classic 均为 10 秒。
- 外部监控不能只看 HTTP 状态码判断刷新是否成功；应同时检查响应体 `success/code`、后端日志和缓存年龄。
- `npm_version_persist_failed` 的业务失败响应会携带 `data.source=persist`；registry 或通用刷新失败通常携带 `data.source=npm`。

## latest 与内置版本

- 结构化响应会返回 `latest_version` 和 `options`。
- `latest` 选项必须带有可解析的 `resolved_version`，否则不能生成稳定的 User-Agent 快照。
- 旧数组格式兼容两种情况：有 `latest` 时保留 `latest + 5` 个固定版本；没有 `latest` 时只保留 5 个固定版本。
- 后端持久化时会从选项中推导 `latest_version`，不盲信输入字段。
- 内置版本只用于界面可选项兜底，不代表后台已经刷新成功。

## 正式环境判断

正式环境代码不是最新时，不能用本地代码结论推断正式环境已包含修复。需要至少比对以下信息：

```bash
git rev-parse HEAD
git rev-parse origin/main
docker exec <new-api-container> /new-api --build-info
curl -fsS <正式环境地址>/api/status
```

容器 `healthy` 只能证明服务健康，不能证明运行的是最新提交。

部署后还需要用管理员身份只读检查诊断接口和数据库记录：

```bash
curl -fsS <正式环境地址>/api/channel/npm_version_options/diagnostics
```

验收口径：

- `summary.package_count` 等于允许的 CLI 包数量。
- `summary.recorded_count` 等于 `summary.package_count`，且 `summary.missing_count=0`。
- `summary.last_error_count=0`；若不为 0，先看对应包的 `recommended_action` 和 `recent_errors`。
- 每个包都有 `latest_version`、`option_count > 0` 和非空 `refreshed_at`。
- 多节点部署时，至少在非 master 节点请求一次 diagnostics，确认它能从数据库加载 `NpmCLIVersionRecordedOptions`，而不是只依赖 master 进程内缓存。
- 直接读取正式数据库中的 `NpmCLIVersionRecordedOptions`，确认存在 `packages`，并按需要检查 `last_errors`、`recent_errors`。

## 隔离环境验证

本地隔离环境更新后使用：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

若要确认数据库已记录 npm 包，可在隔离 PostgreSQL 中读取 `NpmCLIVersionRecordedOptions`。该检查只能证明隔离环境，不代表正式环境。

## 测试边界

- Go 单元测试覆盖缓存读写、错误码、路由顺序、AdminAuth、CriticalRateLimit、CAS 语义和刷新失败路径。
- 路由测试覆盖 diagnostics 未认证访问返回 HTTP 401，并覆盖管理员请求在 CriticalRateLimit 下的限流语义。
- Go 单元测试覆盖 CAS 冲突重试、短退避与 jitter 语义，但不能代表多实例真实调度。
- Go 单元测试覆盖进程缓存未命中但数据库已有记录时，GET 只从数据库单项加载并且不访问 npm registry。
- Go 单元测试覆盖 diagnostics `cache_age_ms` 非负边界，避免未来时间戳返回负数。
- Go 单元测试覆盖 diagnostics `summary`、`metrics`、`recommended_action` 和 `recent_errors` 响应契约，确保人工刷新失败码能进入诊断。
- Go 单元测试覆盖相同包名并发手动刷新 singleflight 合并，确保多次并发调用只访问一次 npm registry。
- 前端 helper 测试覆盖结构化响应、旧数组兼容、错误分类、fallback/retained/source 语义、版本和平台快照，并固定 diagnostics 请求必须带 10 秒超时、手动刷新卸载保护和性能设置页只读诊断入口。
- classic helper 测试固定手动刷新端点和 `HeaderProfileLibrary.jsx` 的 POST 调用契约，防止刷新路径退回 GET，并固定 diagnostics 请求必须带 10 秒超时、HTTP 错误归一化、未知 transport code 降级、手动刷新卸载保护和性能设置页只读诊断入口。
- 2026-07-07 已在隔离 PostgreSQL 15 和临时 MySQL 8.4 容器复验 CAS `RowsAffected`、stale 更新、长 JSON value 比较和 MySQL `BINARY value = BINARY ?` 的大小写敏感语义。后续若修改 `OptionMap` 持久化逻辑，仍需重新做真实数据库复验。
- 当前测试没有引入组件级浏览器或 React Testing Library 异步渲染验证；组件 mounted guard 和请求乱序保护主要由 helper 语义测试与现有构建检查兜底。
