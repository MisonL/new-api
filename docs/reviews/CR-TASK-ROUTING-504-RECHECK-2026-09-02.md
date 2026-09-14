# 异步任务模型映射与 504 复核记录

采样时间：2026-09-02（Asia/Shanghai）

## 本轮修复

- `model_mapping` 继续使用请求级路由诊断；缺少已安装映射快照时不再回读可变渠道表，避免 dispatch 到 submit 之间发生模型漂移。
- 2xx 任务提交的响应体读取、JSON 结构错误、缺少任务 ID 和适配器 panic 统一归类为 `ambiguous_task_submission`，保留公开 task ID 关联并禁止重试/退款。
- 任务已被上游接受但本地 `task.Insert()` 失败时，保持不可重试、不可退款，并记录 task/channel/request 关联；歧义提交先完成实际预扣额度结算和消费日志。
- 提交阶段不回显非 2xx 上游正文，并为任务提交响应体设置 1 MiB 有界读取。
- Gemini/Vertex 任务结果轮询保留上游 429/503/504（524 映射 504）和合法 `Retry-After`。
- 视频 content GET 仅对瞬时状态做同一固定路由内的有限重试；direct URL、data URL、渠道删除或凭证轮换场景保持独立下载语义。
- Sora 任务 ID fallback 仅遍历明确任务容器，并支持 Location query 参数；不改变已解析的规范顶层任务对象语义。

## 已执行验证

以下命令均在当前工作树执行，未访问正式 13000 服务：

```text
GOMAXPROCS=2 go test -p 1 -count=1 ./relay ./controller ./middleware ./relay/helper ./service ./relay/channel/task/sora PASS (exit 0)
GOMAXPROCS=2 go test -race -p 1 -count=1 ./relay ./controller ./middleware ./relay/helper ./service ./relay/channel/task/sora PASS (exit 0)
go vet ./relay ./controller ./middleware ./relay/helper ./service ./relay/channel/task/sora PASS (exit 0)
go build ./...                                              INCOMPLETE (no output for >20 minutes; interrupted with SIGINT, exit 130)
git diff --check                                              PASS (exit 0)
```

此前记录中的整仓 `go test ./... PASS` 不作为本轮复核证据；本轮重新执行时整仓命令未产生包级输出，见下方边界说明。

新增回归覆盖包括：任务提交响应体上限与 exact-limit、结构错误歧义化、Sora 顶层数组/Location query/fallback、映射快照缺失 fail-closed、Gemini/Vertex 状态码与 Retry-After、视频 GET 瞬时重试和永久错误不重试。

## 未覆盖边界

- 正式服务 `docker 13000`、真实 201 上游和生产数据库/Redis 未操作、未验收。
- 当前工作树存在其他用户 dirty 修改；本记录不代表这些修改均由本轮产生或已审查。
- 未证明真实上游在 HTTP 5xx 后是否已创建任务；本轮将 504/524 提交响应视为不可确认的歧义并落本地对账行，其他 5xx 仍遵循既有可配置重试规则。
- 整仓 `go build ./...` 未完成：无输出等待超过 20 分钟后以 SIGINT 中止（退出码 130）；整仓测试已完成编译并通过。
- 未执行隔离 Docker 重建；需要在独立 `dev-isolated` 环境重建镜像后再核对 build-info 与 `/api/status`。

## 2026-09-03 增量复核与修复

- `model/channel_route_model.go` 先执行 `TrimSpace` 再判断模型名，纯空白输入不再生成空路由候选。
- `middleware/distributor.go` 对 MJ/Suno/视频查询、内容下载和 remix 等 no-select 跟进请求，在无 `specific_channel_id` 时交由任务记录恢复路由；带有 `specific_channel_id` 时仍严格读取、校验并初始化指定渠道，已删除或禁用的 pinned channel 显式失败。
- 新增纯空白模型候选、无 pinned channel 的 no-select 放行、缺失 pinned channel/禁用 pinned channel 的显式失败以及有效 pinned channel 初始化回归测试。

本次实际验证：

```text
GOMAXPROCS=2 go test -p 1 -count=1 ./model ./middleware PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./controller ./service ./relay/common ./relay/helper PASS (exit 0)
GOMAXPROCS=2 go test -race -p 1 -count=1 ./relay ./controller ./middleware ./service PASS (exit 0)
GOMAXPROCS=2 go vet ./model ./middleware ./controller ./service ./relay/... PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./common ./constant ./dto ./model ./router ./setting/... ./types PASS (exit 0)
git diff --check PASS (exit 0)
```

本次整仓 `GOMAXPROCS=2 go test -p 1 -count=1 ./...` 连续超过 16 分钟没有任何包级输出，已中断该测试命令（exit 130）；不能据此宣称整仓测试通过。正式服务 `docker 13000`、真实 201 渠道、生产 PostgreSQL/Redis 和隔离 Docker 重建仍未操作。

## 2026-09-03 追加复核与修复

- 新增的 Vidu 任务动作回归测试使用了未声明的 `TaskPlatformVidu`，已在 `constant/task.go` 补齐该平台常量。
- 该测试随后暴露 `relay.RelayInfo` 裸实例在 `bindTaskSubmissionAction` 中访问嵌入式 `TaskRelayInfo` 指针会 panic；已在 `relay/relay_task.go` 安全初始化，并让 `taskSubmissionModelName` 在缺失嵌入值时走非任务字段。
- 本轮重新通过：

```text
GOMAXPROCS=2 go test -p 1 -count=1 ./model ./middleware PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./controller ./service PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./relay/... PASS (exit 0)
GOMAXPROCS=2 go test -race -p 1 -count=1 ./controller ./middleware ./service ./relay PASS (exit 0)
GOMAXPROCS=2 go vet ./model ./middleware ./controller ./service ./relay/... PASS (exit 0)
```

- `GOMAXPROCS=2 go test -run '^$' -p 1 ./...` 用于全仓编译级检查，连续约 27 分钟无子编译进程且 CPU 几乎空闲，已发送 SIGINT 中止（exit 130）；不能据此宣称整仓编译通过。
- 外部 OMP 复核为 `no findings. EXIT=0`；Claude Code 未发现 P0/P1，并曾提示 pinned-channel no-select 语义需核对。本次按架构规划恢复“无 pin 才跳过、有 pin 必须严格初始化”的规则并补回归测试。外部审查不替代本地测试和真实渠道验收。

## 2026-09-03 最终收尾复核

- 发现并修复前端依赖审计中的高危项：Default 将 `browserslist` 固定到 `4.28.8`、`fast-uri` 固定到安全的 `3.1.7`；Classic 将 `browserslist` 固定到 `4.28.8`，并同步两份 `bun.lock`。升级前审计为 Default 6 个 high、Classic 2 个 high，升级后均为 `No vulnerabilities found`。
- Default `bun run typecheck`、`bun run lint` 和 Classic `bun run lint` 均通过（exit 0）。
- 新增/相关 Node 契约测试 46 项全部通过（exit 0）；修改过的 macOS Docker/TUN shell 契约测试 5 项及 Python 测试 4 组全部通过（exit 0）。
- 定向模型映射、分发器、视频代理和任务路由测试通过；`GOMAXPROCS=2 go test -race -p 1 -count=1 ./model ./middleware ./controller ./service ./relay/...` 全部通过（exit 0）。
- `scripts/build-docker-local.sh new-api-local:dev` 已成功完成；随后仅重建隔离 `new-api-dev-isolated-new-api-1`，容器 `healthy`、`RestartCount=0`，`/api/status` 返回 200，容器 build-info 为当前 dirty HEAD `f825f23d...-dirty`。未重启或重建正式容器。
- 最终检查 `git diff --check` 通过，未发现本轮新增的 Go/Node/Python 测试进程；现有 dirty worktree 保留不变。

最终边界：整仓 `go test -run '^$' -p 1 ./...` 仍因超过约 27 分钟无包级输出而以 exit 130 中止，不能宣称整仓编译完成；未执行真实 201 上游调用、正式 13000/Caddy/生产 PostgreSQL/Redis 操作，也未证明真实上游在 5xx 后的任务创建状态。隔离环境健康与本地测试不等于生产发布或真实渠道验收。

## 2026-09-03 续接验证

本轮仅执行本地只读验证，保留既有 dirty worktree，未提交、未重启服务、未操作正式 13000 或生产数据库/Redis：

```text
go test ./...                         PASS (exit 0)
go build ./...                        PASS (exit 0)
go vet ./...                          PASS (exit 0)
web/default bun run typecheck        PASS (exit 0)
web/default bun run lint              PASS (exit 0)
web/default bun run format:check     PASS (exit 0)
web/default bun run build             PASS (exit 0; Safari compatibility check passed)
web/classic bun run lint              PASS (exit 0)
web/classic bun run test:node         PASS (137 tests, exit 0)
web/classic bun run build             PASS (exit 0; Safari compatibility check passed)
gofmt -l (all changed/new Go files)   no output
git diff --check                      PASS (exit 0)
```

OMP 的此前只读复核未发现可复现问题。Claude Code 进程随后以 exit 0 结束，但没有输出报告，因此不将空输出解释为 approval；CodeRabbit 未纳入本轮审查。当前分支仍为 `dev`，HEAD 为 `f825f23d870df24e38362a7bd1d0a2cd4423a1a6`，工作区仍有用户既有的未提交和未跟踪改动。

本节结果覆盖文档前文因本机 I/O 中断的历史记录；历史命令和当时的未覆盖边界仍保留作审计轨迹。

## 2026-09-04 续接复核与隔离重建

- 复核发现 `middleware/distributor.go` 新增 `captureTaskRouteIntent` 曾只按 JSON 读取视频任务提交体，会把原本合法的 `application/x-www-form-urlencoded` 视频请求误判为 400。现已按 Content-Type 分支解析 URL encoded 表单，并在推断路由意图后复位可复用请求体；multipart 和 JSON 路径保持原有语义。
- 新增 URL encoded 视频任务路由意图回归测试；同时复核 JSON、multipart、provider-specific object image、显式 action 和多图片 action 分类。
- 进一步复核发现 canonical `TaskPlatformVidu` 与渠道类型 `52` 的适配器身份映射不完整：快照校验已支持 canonical platform，但任务适配器注册只接受数字平台。现已统一支持 `vidu` 身份，并补快照构建与适配器解析回归测试。
- `model/task.go` 的格式残留已用 `gofmt` 修正；所有已修改或新增 Go 文件的 `gofmt -l` 无输出，`git diff --check` 通过。

本轮新鲜验证（均为当前工作树，未调用真实 201 上游）：

```text
GOMAXPROCS=2 go test -p 1 -count=1 ./middleware ./service ./model ./controller ./relay/common ./relay PASS (exit 0)
GOMAXPROCS=2 go test -race -p 1 -count=1 ./middleware ./service ./model ./controller ./relay/common ./relay PASS (exit 0)
GOMAXPROCS=2 go vet ./middleware ./service ./model ./controller ./relay/common ./relay PASS (exit 0)
gofmt -w (modified Vidu routing files) PASS (exit 0)
go test -p 1 -count=1 ./... PASS (exit 0; all packages)
go build ./... PASS (exit 0)
go vet ./... PASS (exit 0)
web/default bun run typecheck && bun run lint && bun run format:check && bun run build PASS (exit 0; Safari compatibility passed)
web/classic bun run lint && bun run test:node && bun run build PASS (137 tests; exit 0; Safari compatibility passed)
gofmt -l (all changed/new Go files) no output
git diff --check PASS (exit 0)
```

隔离环境已按当前工作树重新构建并重建 `new-api-dev-isolated-new-api-1`：

```text
scripts/build-docker-local.sh new-api-local:dev PASS (exit 0)
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api PASS (exit 0)
container health=healthy, restart_count=0
/new-api --build-info: f825f23d870df24e38362a7bd1d0a2cd4423a1a6-dirty
curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status HTTP 200
```

正式环境仅作带超时只读探针：`127.0.0.1:13000/api/status` 和 Caddy `127.0.0.1:3000/api/status` 均 HTTP 200；正式容器 `new-api` 仍运行 `prod-codex-lite-4527d7666`，build-info 为 `4527d7666a37f9151a57f08ca0bc5e8e08b8fb46`，不是当前 dirty worktree。未重启或替换正式容器，未写入生产 PostgreSQL/Redis，未发送真实渠道 201 业务请求。

本轮仍未覆盖：整仓长时间编译/测试的完整包级输出、真实 201 渠道及上游 504/任务创建语义、生产 Redis 多实例租约故障、正式发布后的回滚验收。隔离环境通过和正式健康探针通过均不等于生产修复已上线。

## 2026-09-05 Midjourney 批量轮询传输身份复核

### 发现与修复

复核确认 schema 5 的 Midjourney 任务已经冻结静态 Header Policy，但批量轮询批次键此前只在 schema 6 纳入 Proxy 和 Header Policy 指纹。不同冻结请求头或代理的 schema 5 任务因此可能被合并到同一批，并复用首个任务的传输配置。

`controller/midjourney.go` 现对 schema 5 及以上任务统一纳入：

- `HeaderPolicyFingerprint`；
- `ProxyFingerprint`（快照声明已捕获代理时）；
- 现有快照、阶段路由、凭证、Base URL 和 Provider Runtime 身份。

这样只有传输身份完全一致的任务才会共享一次批量请求；schema 5 仍按既有兼容语义不冻结 Provider Runtime。新增回归测试覆盖不同 schema 5 请求头和代理的批次键隔离，并保留 Redis 不可用时 claim fail-closed、忙任务跳过后释放 claim、缺失当前快照凭证显式失败等保护。

### 本轮实际验证

以下命令均在当前工作树执行，未调用真实 201 上游：

```text
GOMAXPROCS=2 go test -p 1 -count=1 ./controller -run 'Test(MidjourneyPolling|FetchMidjourney|ResolveMidjourney|GroupMidjourney)' -v PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./model -run 'Test(TaskRouteSnapshot|TaskPrivateData|Midjourney|.*Snapshot)' -v PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./service -run 'Test(TaskRouteSnapshot|TaskPolling|SunoTaskPolling|BuildTask|ResolveTask|.*Snapshot)' -v PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./relay -run 'Test(TaskRoute|.*Task|.*Video|.*Midjourney|.*Snapshot)' -v PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./controller ./model ./service ./relay/... PASS (exit 0)
go test -race -p 1 -count=1 ./controller ./service ./relay PASS (exit 0)
go vet ./controller ./service ./relay/... PASS (exit 0)
git diff --check PASS (exit 0)
```

上述定向测试明确通过 `TestMidjourneyPollingBatchKeySeparatesSchema5TransportIdentities`；整组 controller、model、service 和 relay 包测试及 race/vet 也通过。当前分支仍为 `dev`，工作区包含既有大量 dirty/untracked 修改；本轮未回退、清理、提交或推送。

### 尚未闭环

- schema 5 Provider Runtime 仍按兼容路径读取当前渠道配置，不能表述为完整运行时冻结；schema 1-4 和 nil snapshot 也保留旧兼容路径。
- 未访问真实 201 上游，未验证真实上游对批量请求、5xx 或任务创建状态的语义。
- 未操作正式 `13000`、Caddy、生产 PostgreSQL/Redis，也未执行正式发布或回滚验收。
- 隔离环境和本地测试通过只证明源码与语义回归，不代表生产修复已上线。

## 2026-09-05 Midjourney 批量轮询增量复核

### 本轮发现与修复

在上一节 schema 5 传输身份隔离基础上，继续检查 claim、状态合并和健康探针语义，确认并修复三处边界：

- 批量请求只持有部分任务 claim 时，响应回写现在只允许匹配本次实际持有的 `claimedTaskIDs`；被其他 worker 占用的任务即使出现在异常上游响应中也不会被本 worker 更新。
- `PENDING`、`PROCESSING`、`FAILED` 统一映射为规范的 `QUEUED`、`IN_PROGRESS`、`FAILURE`，避免健康探针已认可但任务合并因状态枚举不一致而停滞。
- Midjourney notify webhook 在 CAS 前复用同一状态标准化，别名状态不会在 webhook 路径被提前当作回退状态丢弃。
- HTTP 2xx 但响应为空、只含无关任务或状态未知时，健康观测记录失败并向轮询调用方返回明确错误；不再以 `nil` 错误继续下游处理。
- 超时失败使用独立候选副本执行 CAS 更新；CAS 失败时不污染调用方传入的内存对象。超时阈值统一使用 `time.Hour`。

### 本轮验证

以下命令均在当前 dirty 工作树执行，未调用真实 201 上游：

```text
GOMAXPROCS=2 go test -p 1 -count=1 ./controller -run 'Test(FetchMidjourney|Midjourney|HasRecognized|IsRecognized|Claimed)' -v PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./service -run 'Test(NormalizeMidjourney|MergeMidjourneyTask)' -v PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./controller ./service -run 'Test(Midjourney|FetchMidjourney|HasRecognized|IsRecognized|Claimed|NormalizeMidjourney|MergeMidjourneyTask)' PASS (exit 0)
GOMAXPROCS=2 go test -race -p 1 -count=1 ./controller ./service PASS (exit 0)
GOMAXPROCS=2 go test -race -p 1 -count=1 ./relay PASS (exit 0)
go vet ./controller ./service ./relay/... PASS (exit 0)
GOMAXPROCS=2 go test -p 1 -count=1 ./controller ./model ./service ./relay/... FAIL (exit 1; all listed packages passed except relay/helper/TestModelPriceHelperUsesOfficialAgnes25ProPricing, actual 0.008444444444444444 vs expected 0.1; unrelated pricing dirty change)
gofmt (changed Midjourney files) PASS (exit 0)
git diff --check PASS (exit 0)
```

CodeRabbit CLI 0.7.6 authenticated review of the dirty `controller` scope completed with exit 0 and 16 findings across pre-existing controller changes. The Midjourney-specific findings were low-severity: CAS failure mutation, duplicated timeout literal, and redundant local initialization were fixed; one per-claim watcher goroutine remains a bounded allocation optimization, with no observed correctness failure. Other findings belong to unrelated dirty files and were not expanded into this atomic task.

### 未覆盖边界

- 尚未访问真实 201 上游，未验证真实上游对批量请求、5xx、别名状态和任务创建状态的语义。
- 尚未操作正式 `13000`、Caddy、生产 PostgreSQL/Redis，也未执行正式发布或回滚验收。
- 既有 Agnes 定价测试失败仍未处理；它不属于本次 Midjourney 轮询任务范围。
- schema 5 Provider Runtime 仍按兼容路径读取当前渠道配置；schema 1-4 和无快照任务也保留旧兼容路径。

## 2026-09-07 Midjourney 轮询租约与超时增量复核

### 发现与修复

- 批量轮询原先在上游响应返回时就释放 task claim，随后才执行响应合并和 `UpdateWithStatus`；两个 worker 可能在同一非终态上对同一行同时成功写入。源码轮询路径现通过带回调的 claim helper，让租约覆盖请求、响应匹配、合并、CAS 和失败退款，租约丢失时停止后续合并/写入；回调回归测试确认写入阶段仍能观察到 busy claim，函数返回后才释放。
- 批量响应路径原先未检查 `SubmitTime` 是否有效，`SubmitTime=0` 会被当前时间差直接判定为超过一小时；现要求 `SubmitTime > 0` 才允许批量超时归一化，并补充 nil、零时间、已完成进度和过期任务测试。超时清扫查询原有的 `submit_time > 0` 门禁保持一致。

### 本轮实际验证

以下命令均在当前 dirty 工作树执行，未调用真实 201 上游：

```text
GOMAXPROCS=2 go test -p 1 -count=1 ./controller ./model ./service PASS (exit 0)
GOMAXPROCS=2 go test -race -p 1 -count=1 ./controller ./model ./service PASS (exit 0)
go vet ./controller ./model ./service ./relay/... PASS (exit 0)
gofmt -l controller/midjourney.go controller/midjourney_polling_test.go no output
git diff --check PASS (exit 0)
scripts/build-docker-local.sh new-api-local:dev PASS (exit 0)
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api PASS (exit 0)
isolated new-api-dev-isolated-new-api-1 health=healthy, restart_count=0, image=new-api-local:dev
/new-api --build-info commit=f825f23d870df24e38362a7bd1d0a2cd4423a1a6-dirty; /api/status HTTP 200, success=true
```

### 未覆盖边界

- 本轮未访问真实 201 上游，未验证真实上游批量响应、5xx 或任务创建状态。
- 本轮未操作正式 `13000`、Caddy、生产 PostgreSQL/Redis，也未执行正式发布或回滚验收；隔离容器通过不等于生产修复已上线。
- schema 5 Provider Runtime、schema 1-4 和无快照任务仍保留既有兼容路径；完整整仓验证和真实多实例 Redis 故障场景不在本轮覆盖范围。
