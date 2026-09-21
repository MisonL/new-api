# Synthetic Compact 状态审查 - 2026-06-05

## 范围

本记录覆盖 `resp_newapi_synthcmp_*` 引用对应的 synthetic Responses Compact 状态生命周期。

审查输入通过 `<isolated-review-dir>` 隔离，使外部审查者和 CodeRabbit 不会收到 `<repository-root>` 中无关的 dirty 工作区改动。

Claude 和 Gemini 的 patch 产物可从真实工作区重新生成：执行 `git diff --binary > /tmp/synthetic-compact-review-current.patch`，再用 `git diff --no-index -- /dev/null <path>` 追加未跟踪的 synthetic compact 文件。

## 契约

- 创建 synthetic 状态需要 `UserID`、`TokenID` 和 `Group`；缺少正式作用域时返回 `ErrSyntheticCompactStateScopeRequired`。
- Synthetic 查找只将 `resp_newapi_synthcmp_*` previous response id 和 `newapi.synthetic.compact:resp_newapi_synthcmp_*` marker payload 视为本地状态引用。
- 普通上游 `previous_response_id` 和非 synthetic marker id 不会在本地 synthetic 状态存储中查找。
- 数据库句柄可用时，创建状态会先加密并持久化数据库记录，再返回 synthetic marker。
- 只有数据库记录已持久化后才容忍 Redis 写入失败；请求仍可通过数据库回退恢复，且恢复路径会记录日志。
- 数据库摘要使用 AES-GCM 加密。AAD 对 `ID`、`Model`、`UserID`、`TokenID` 和 `Group` 加长度前缀并绑定。
- 数据库密文使用 model 层自定义类型：MySQL 映射为 `MEDIUMTEXT`，PostgreSQL、SQLite 和默认方言映射为 `TEXT`。
- 存储或加密前将摘要大小限制为 `512 KiB`。
- 进程内缓存受 `syntheticCompactMemoryEntriesMax` 限制，不是无界 `sync.Map`。
- 从数据库派生的内存缓存条目保留数据库记录的 `ExpiresAt`，不会获得新的完整 TTL。
- 缺失、过期、Redis、数据库、解密、作用域和 marker 清理 marshal 失败均保持为显式错误。
- 过期数据库行由仅 master 执行的维护任务通过 `PruneExpiredSyntheticCompactStateRecords` 分批删除。

## 文件

- `main.go`
- `model/main.go`
- `model/synthetic_compact_state.go`
- `model/synthetic_compact_state_test.go`
- `service/responses_synthetic_compact.go`
- `service/responses_synthetic_compact_store.go`
- `service/responses_synthetic_compact_crypto.go`
- `service/responses_synthetic_compact_prompt.go`
- `service/responses_synthetic_compact_input.go`
- `service/responses_synthetic_compact_maintenance.go`
- `service/responses_synthetic_compact_memory.go`
- `service/responses_synthetic_compact_test.go`
- `relay/channel/openai/adaptor_responses_compaction_test.go`

## 审查轮次

### CodeRabbit 审查

- 退出码 0：`coderabbit --version`
  - `0.5.3`
- 退出码 0：`coderabbit auth status --agent`
  - 认证身份为 `<review-account>`
- 退出码 0：`coderabbit review --agent -t uncommitted --dir service -c AGENTS.md .coderabbit.yaml`
  - 首轮 service 审查提出超时、TTL、可观测性和清理问题。
  - 已采纳：prune 超时和日志、内存中的数据库过期时间保留、marker 清理错误传播、AES-GCM 最小长度检查、可见内容截断日志、内容数组稳健解析、有界本地内存缓存、恢复日志级别调整和作用域校验顺序。
  - 未采纳：为进程生命周期维护 goroutine 增加关闭 API、将首次 prune 延迟到 24 小时周期、迁移已有作用域 AES-GCM 密钥的 HKDF、数据库命中时回填 Redis，以及对有意忽略的不可见输入片段进行大范围 debug 日志。
- 退出码 0：`coderabbit review --agent -t uncommitted --dir model -c AGENTS.md .coderabbit.yaml`
  - 首轮 model 审查提出批量 prune 的事务保护问题。
  - 已采纳：批量选择/删除现在在事务内执行，并使用稳定的 `Order("id")` 和 `Pluck("id", &ids)`。
- 退出码 0：`coderabbit review --agent -t uncommitted --dir relay/channel/openai -c AGENTS.md .coderabbit.yaml`
  - 发现数：`0`
- 退出码 1：`coderabbit review --agent -t uncommitted --dir . -c AGENTS.md .coderabbit.yaml`
  - 被 CodeRabbit `rate_limit` 阻断。
- 退出码 1：最后一次尝试执行 `coderabbit review --agent -t uncommitted --dir model -c AGENTS.md .coderabbit.yaml`
  - 被 CodeRabbit `rate_limit` 阻断，报告等待时间为约 7 分 53 秒。
- 退出码 1：最后一次尝试执行 `coderabbit review --agent -t uncommitted --dir service -c AGENTS.md .coderabbit.yaml`
  - 被 CodeRabbit `rate_limit` 阻断，报告等待时间为约 4 分 50 秒。
- 退出码 0：针对隔离 synthetic compact diff 复跑 `coderabbit review --agent -t uncommitted --dir . -c AGENTS.md .coderabbit.yaml`。
  - 严重问题：无。
  - 已采纳：记录 HEAD/origin/build-info 比较。
  - 未采纳：为进程生命周期维护 goroutine 增加关闭 API、改变首次 prune 时序、迁移已有域分离 AES-GCM 密钥的 HKDF、在有意对内存测试/运行模式 no-op 的 model helper 中 `model.DB == nil` 时返回错误，以及由于工作区在任务前已 dirty 且审查输入已隔离而进行的大范围干净树重建。
- 退出码 0：针对 `/tmp/new-api-synthetic-coderabbit` 最终执行 `coderabbit review --agent -t uncommitted --dir . -c AGENTS.md .coderabbit.yaml`。
  - 发现数：`2`
  - 严重问题：无。
  - 主要问题：无。
  - 次要问题：无。
  - 仅有琐碎问题：基于 TTL 的内存清理间隔，以及 256 条 FIFO 缓存的 O(1) 队首弹出。
  - 未采纳：当前清理间隔在 24 小时 TTL 下限制为一小时，当前 FIFO 淘汰使用固定 256 条上限，切片移动仍有界且比环形队列/链表记账更简单。

### Claude 审查

- 退出码 0：Claude 首轮针对 `/tmp/synthetic-compact-review-current.patch` 的只读审查。
  - 严重问题：无。
  - 已接受警告：恢复成功路径不应使用 error 级别日志。
  - 已接受警告：`BuildSyntheticCompactResponse` 应在摘要文本前校验作用域。
  - 信息项：内存保留数据库过期时间，prune 顺序稳定。
- 历史说明：早先第二次 Claude 重跑超过 12 分钟没有文件输出而停止，已记录为结论不确定，并由下方最终成功的 Claude 只读审查取代。
- 退出码 0：Claude 最终针对 `/tmp/synthetic-compact-review-current.patch` 的只读审查。
  - 严重问题：无。
  - 已采纳：取消的加载上下文不应记录为 Redis 恢复；测试内存重置应保持原子性。
  - 未采纳：改回空字符串拆分行为、重构 prune 中的小闭包捕获，以及在当前 256 条上限之外继续优化有界缓存锁路径。
- 退出码 0：Claude 最终短版针对 `/tmp/synthetic-compact-review-current.patch` 的只读审查。
  - 严重问题：无。
  - 早期最终轮次已采纳：Redis 损坏 payload 回退数据库、Redis 命中时继承 TTL、原子内存加载/删除、显式 tool-call 类型白名单和线性无效 UTF-8 拆分。
  - 未采纳：`model.DB == nil` 时 model helper 返回 nil，这是现有底层内存/测试契约；Redis 失败且 DB 为 nil 时增加内存回退会制造外部依赖失败的伪成功；将作用域校验移动到 `storeSyntheticCompactState` 会破坏直接底层 store 测试，而且唯一生产入口 `BuildSyntheticCompactResponse` 已在存储前校验作用域，重复添加没有收益。

### Gemini 审查

- 退出码 0：Gemini 首轮针对 `/tmp/synthetic-compact-review-current.patch` 的只读审查。
  - 已接受严重问题：无条件的无界本地内存缓存。
  - 已接受警告：持久化不应被客户端请求上下文取消；store 路径现在使用 `context.WithoutCancel(ctx)` 加 10 秒超时。
- 退出码 0：修复后的 Gemini 第二轮复跑。
  - 严重问题：无。
  - 警告：无。
  - 已接受信息项：使用 GORM `Pluck` 获取单列 prune ID。
- 退出码 0：Gemini 最终针对 `/tmp/synthetic-compact-review-current.patch` 的只读审查。
  - 严重问题：无。
  - 警告：无。
  - 信息项：Redis 以明文保存短期缓存的 synthetic 状态，而数据库保存加密密文；在当前运行模型中 Redis 是受信任的缓存边界，数据库持久化仍保持加密，因此接受该差异。
- 退出码 0：Gemini 最终针对 `/tmp/synthetic-compact-review-current.patch` 的只读审查。
  - 严重问题：无。
  - 警告：无。
  - 仅信息项：O(n) 缓存淘汰受 `syntheticCompactMemoryEntriesMax = 256` 限制；批量 prune 和 AAD 绑定被认为稳健，Gemini 建议直接合并，不再修改代码。

## 已采纳变更

- 新增带上下文 CRUD 和跨方言密文类型的持久化 `SyntheticCompactStateRecord`。
- 在普通和快速迁移中注册 `SyntheticCompactStateRecord`。
- 从 `main.go` 启动仅 master 执行的 synthetic compact prune 任务。
- 按职责将 synthetic compact 逻辑拆分到 store、crypto、prompt、input、maintenance 和 memory 文件。
- 创建 synthetic 状态时要求正式作用域。
- 将 marker 解析收紧为仅接受本地 synthetic 引用。
- 使用 AES-GCM 和带长度前缀的 AAD 增加加密数据库持久化。
- MySQL 密文存储使用 `MEDIUMTEXT`，其他方言使用 `TEXT`。
- 为 DB/Redis 持久化增加显式 store 超时和 `context.WithoutCancel`。
- 增加有界进程内缓存和缓存上限回归覆盖。
- 从数据库记录恢复到内存时保留数据库过期时间。
- 使 marker 清理 marshal 错误显式返回。
- 增加可见内容截断可观测性，但不记录用户内容。
- 为混合内容数组增加稳健的仅对象解析。
- 为过期行 prune 增加事务、稳定排序、删除数量返回和 `Pluck`。
- 改进 UTF-8 文本拆分回退。
- 加固隔离 SQLite 和损坏 Redis 场景的 synthetic compact 测试。
- Redis 加载后数据库回退前增加显式取消上下文处理。
- 为有界内存存储增加原子测试重置。
- 在代码注释中记录 store 上下文超时边界和畸形输入 marker 清理行为。
- 增加有界缓存回归覆盖，确认最旧条目淘汰且最新条目保留。
- 增加 Redis 加载失败时的数据库回退覆盖，并将回退切换到新的数据库超时上下文。
- 将过期行 prune 选择改为按 `expires_at, id` 排序，使过期范围保持首要查询键并保持批次确定性。
- 分离读写 store 上下文，使读取遵循调用方取消，而写入在独立超时内完成。
- 使 UTF-8 拆分回退只扫描附近 rune 边界窗口，并在线性时间内硬拆畸形 continuation-byte 序列。
- 数据库可用且 Redis 加载失败、数据库未命中时返回正常 not-found 语义，并增加回归覆盖。
- 将可见 tool-call 提取改为已知 Responses 工具类型白名单，忽略未知的类 `_call` 类型。
- 在 store 锁内原子执行内存缓存加载和过期条目删除，避免删除刚刷新的条目。
- Redis JSON payload 损坏时回退到加密数据库事实源，并增加回归覆盖。
- Redis 命中时让内存缓存条目继承 Redis 剩余 TTL，而不是获得新的完整 synthetic TTL。

## 验证

- 退出码 0：`git diff --check`
  - 执行时间保存在私有验证记录中。
  - 无输出。
- 退出码 0：`go test ./model ./service -count=1 -run 'TestSyntheticCompact|TestApplySyntheticCompactState|TestBuildSyntheticCompact|TestLoadSyntheticCompact|TestPruneExpiredSyntheticCompact|TestStoreSyntheticCompact|TestSplitSyntheticCompactTextParts'`
  - 执行时间保存在私有验证记录中。
  - `ok github.com/QuantumNous/new-api/model 0.274s`
  - `ok github.com/QuantumNous/new-api/service 1.242s`
- 退出码 0：`go test ./controller ./model ./relay/common ./relay/helper ./service`
  - 执行时间保存在私有验证记录中。
  - `ok github.com/QuantumNous/new-api/controller 4.679s`
  - `ok github.com/QuantumNous/new-api/model 2.842s`
  - `ok github.com/QuantumNous/new-api/relay/common (cached)`
  - `ok github.com/QuantumNous/new-api/relay/helper 14.113s`
  - `ok github.com/QuantumNous/new-api/service 1.574s`
- 退出码 0：`go test ./relay/channel/openai ./relay/channel/codex -count=1`
  - 执行时间保存在私有验证记录中。
  - `ok github.com/QuantumNous/new-api/relay/channel/openai 0.046s`
  - `ok github.com/QuantumNous/new-api/relay/channel/codex 0.032s`
- 退出码 0：`scripts/build-docker-local.sh new-api-local:dev`
  - 执行时间保存在私有验证记录中。
  - 构建输出包含 `image`、`version`、`commit`、`date` 等 build-info 字段，具体值保存在私有验证记录中。
- 退出码 0：`docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api`
  - 隔离应用服务已重建并启动。
- 退出码 0：等待 Docker 健康检查。
  - `health=healthy`
- 退出码 0：`docker exec new-api-dev-isolated-new-api-1 /new-api --build-info`
  - 执行时间保存在私有验证记录中。
  - `version=v1.1.0`
  - `commit=<validated-revision>-dirty`
  - `date=<build-time>`
  - `source=https://github.com/MisonL/new-api`
- 退出码 0：`git rev-parse HEAD`
  - `<local-head-revision>`
- 退出码 0：`git rev-parse origin/main`
  - `<origin-main-revision>`
- build-info 对比：
  - 隔离容器 build-info 的提交前缀与本地 `HEAD` 一致。
  - 若出现 `-dirty` 后缀，表示二进制来自含未提交改动的工作区。
  - 该验证只证明隔离开发二进制与被测候选源码一致，不代表它等于 `origin/main`。
- 退出码 0：`curl -fsS http://127.0.0.1:3001/api/status`
  - 响应包含 `\"success\":true`。
- 退出码 0：`docker ps --filter name=new-api-dev-isolated-new-api-1 --format '{{.Names}} {{.Image}} {{.Status}} {{.Ports}}'`
  - 隔离应用容器使用 `new-api-local:dev`，状态为 `healthy`，端口映射符合隔离环境配置。

## 环境边界

- 已验证环境：`127.0.0.1:3001` 上的隔离开发栈。
- 本门禁使用的运行数据库：`deploy/compose/dev-isolated.yml` 中的 PostgreSQL。
- 正式端口 `3000` 和 `13000` 未在本审查中修改，也未据此宣称已验证。
- Docker 状态为 `(healthy)` 只表示容器健康检查成功且进程有响应，不代表源码来源可追溯。
- `-dirty` build-info 后缀表示镜像来自含未提交改动的工作区；隔离审查输入只包含 synthetic compact 状态改动，但运行中的 3001 二进制可能来自完整 dirty worktree。
- 任务开始前工作区已经存在其他改动；本报告未回退、未审查这些无关改动，也不把它们宣称为可用于正式环境的版本。
