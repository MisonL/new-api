# 开发侧渠道路由与并发修复复核

## 范围与边界

- 采样日期：2026-08-27（Asia/Shanghai）。
- 分支：`dev`，复核开始时 `HEAD=b42e5f54b2fdb6d0f2d1cd44e4e1cd31af15c1aa`。
- 本轮只处理开发侧已确认的 5 项问题；保留工作区原有 tracked/untracked dirty 修改。
- 未修改、升级、重启或验收正式服务、Caddy、正式 PostgreSQL/Redis，也未处理 150/mdkj.lol 渠道。

## 已完成修复

1. `service/channel_select.go`：legacy 选择器遇到非法 `model_mapping` 时过滤该候选，继续尝试有效 fallback；无法归类为映射配置错误的异常仍保持原有 fail-open 语义。
2. `service/channel_concurrency.go`：Redis 并发租约续期遇到 timeout、连接错误或 `LOADING` 等可恢复错误时继续下一轮重试，仅在租约不存在、已释放或本地租约失效时退出；补充定向回归测试。
3. `model/ability.go`：`FixAbility`/能力重建保留 V2 `RoutePriority`、`RouteWeightBPS` 及 configured 标记，显式零值继续生效，legacy 行不会把数据库默认零值误判成显式配置。
4. `controller/relay.go`：初始渠道并发耗尽且没有可用 fallback 时统一返回 HTTP 429 与 `channel_concurrency_exceeded`；健康排除、协议不兼容和模型不存在仍保留各自错误语义。
5. `scripts/codex-real-channel-compaction-gate.sh`：根据 `DEV_COMPOSE_PROJECT_NAME` 动态解析隔离网络、PostgreSQL、Redis 和 `new-api` 容器名，并在 Compose 调用中显式传递 project name；补充 source-level 回归覆盖。

## 本轮新增修复

1. `service/channel_health_attempt.go`：`ChannelHealthAttempt.ObserveAPIError` 使用原子 finalized 门禁，重复或并发上报同一个上游错误只会分类、计数和写入一次；nil 错误仍保留原有 probe release 语义。
2. `service/channel_health_probe_lifecycle.go`：探针续期后台线程在租约丢失或连续失败达到阈值时只标记 terminated 并取消 request context，不再从后台线程读取或修改 Gin request/probe 状态；请求主 goroutine 的 release/clear 路径负责最终清理，并拒绝已终止 probe 的成功回调。
3. `service/channel_concurrency.go`：Redis 并发租约释放增加两次有界幂等尝试，覆盖命令已执行但客户端超时以及短暂连接错误，最终失败仍记录 releaseFailure，不伪造释放成功。
4. `service/channel_health_attempt_test.go`、`service/channel_health_test.go`、`service/channel_concurrency_test.go`：新增并发重复错误、终止 probe 禁止成功回调和释放瞬时错误重试回归测试。

## 本轮验证证据

以下命令均在 `/Volumes/Work/code/new-api` 执行，退出码均为 0：

```text
go test -count=1 ./controller ./model ./relay/common ./relay/helper ./service
  controller: ok (6.177s)
  model: ok (3.134s)
  relay/common: ok (0.073s)
  relay/helper: ok (14.297s)
  service: ok (10.765s)

go test -count=1 -race ./service -run 'TestChannelConcurrencyLeaseRenewLoopRetriesTransientRedisErrors|TestChannelConcurrency|TestFixAbilityPreservesV2RouteFields|TestGetChannelInitialConcurrencyExhaustionReturns429'
  service: ok

go test -count=1 -race ./controller -run '^TestGetChannelInitialConcurrencyExhaustionReturns429$'
  controller: ok

go test -count=1 -race ./model -run '^TestFixAbilityPreservesV2RouteFields$'
  model: ok

node --test scripts/codex-real-channel-compaction-gate.test.mjs
  24 passed, 0 failed

go test -count=1 ./service
  ok (15.639s)

go test -count=1 ./controller ./model ./relay/common ./relay/helper
  controller: ok (7.912s)
  model: ok (4.458s)
  relay/common: ok (0.085s)
  relay/helper: ok (14.365s)

go test -count=1 -race ./service -run 'TestChannelHealth|TestChannelConcurrency|TestChannelHealthAttemptObserveAPIErrorIsIdempotent'
  ok (11.918s)

go test -count=1 ./service -run '^TestChannelHealth(TerminatedProbeCannotBeMarkedSuccessful|AttemptObserveAPIErrorIsIdempotent|ProbeRenewalFailureThresholdCancelsRequest)$'
  ok (1.706s)

go test -count=1 -race ./service -run 'TestChannelHealthAttemptObserveAPIErrorIsIdempotent|TestChannelHealthProbeRenewalFailureThresholdCancelsRequest|TestChannelHealthProbeRenewalRestartsAfterSameContextRelease|TestChannelHealthExpiredProbeLeaseCannotClearNewProbe|TestChannelConcurrencyLeaseReleaseRetriesTransientRedisError|TestChannelConcurrencyLeaseRenewLoopRetriesTransientRedisErrors|TestChannelConcurrencyRenewFailureThresholdCancelsRequest'
  ok (11.452s)

bash -n scripts/codex-real-channel-compaction-gate.sh
python3 scripts/compose-healthcheck-contract.py
  root passed; dev-isolated passed; legacy-dev passed

git diff --check
  no output, exit code 0
```

## 未覆盖边界

- `go test -count=1 ./...` 在上一轮运行超过约 40 分钟无有效输出后被中断，没有最终退出码，因此不能表述为全量 Go 测试通过。
- 本轮未修改前端，未把历史遗留的前端 typecheck/lint 进程结果计入通过证据。
- 未执行真实渠道请求、生产数据库/Redis 验收或隔离 Compose 的破坏性重建；本轮结论是源码、定向测试和脚本契约层面已通过。
- 未提交 Git；收尾前保留现有 dirty worktree，不执行 reset、checkout、clean 或无关文件整理。
- `go test -count=1 ./...` 和 `go vet ./...` 仍未取得完整结果：仓库包含大型前端依赖树和大量历史 dirty 文件，上一轮扫描长时间无有效输出后中止；本轮未把它们表述为通过。
- 当前测试覆盖的是 miniredis、内存和源码契约；未覆盖真实生产 Redis 超时、真实跨进程并发、真实渠道请求及正式服务验收。

## 再次全面复核与新增实现（2026-08-27）

对照 `docs/architecture/channel-routing-priority-weight-concurrency-plan.md`、本文件前述记录和当前源码重新复核后，确认原实现仍存在一个会直接影响渠道 201 映射一致性的缺口：legacy/V2 选择器、健康候选过滤和 Responses compact 能力判断分别读取并解析 `model_mapping`，同一请求内可能出现重复解析，且中途编辑可能导致健康路由身份与 relay 使用的上游模型不一致。

本次已补齐：

1. `model/channel_mapping.go` 新增 `ChannelModelMapping` 及 `PrepareChannelModelMapping`。每个选择请求通过 `service.newChannelRouteModelResolver` 按渠道固定已校验快照；普通模型、compact 精确候选和 compact base fallback 均从同一快照解析，映射环、空键/值、非字符串目标仍显式拒绝。
2. `model/channel_cache.go`、`model/ability.go`、`model/channel_route_selector.go` 的 cache、legacy DB、V2 DB 和 compact capability 路径统一接收 request-scoped resolver。非法映射继续排除候选，健康过滤消费已经解析的有效模型，不再自行 JSON 解析。
3. `service/channel_health.go` 增加带 `effectiveModel` 的 route builder，健康排除使用选择器给出的有效模型；relay helper 继续优先消费选择诊断中的同一结果。
4. 新增 `TestPreparedChannelModelMappingReusesValidatedSnapshot`、`TestChannelRouteModelResolverPinsMappingForRequest`，覆盖快照复用和请求内映射编辑不改变路由身份。

新增验证（命令均在仓库根目录执行，退出码为 0）：

```text
go test -p=1 -count=1 ./controller ./model ./relay/common ./relay/helper ./service ./router
go test -p=1 -race -count=1 ./model ./service -run 'TestPreparedChannelModelMappingReusesValidatedSnapshot|TestChannelRouteModelResolverPinsMappingForRequest|TestChannelHealthAttemptObserveAPIErrorIsIdempotent|TestChannelHealthProbeRenewalFailureThresholdCancelsRequest|TestChannelConcurrencyLeaseReleaseRetriesTransientRedisError'
go vet ./controller ./model ./relay/common ./relay/helper ./service ./router
git diff --check -- model/channel_mapping.go model/channel_cache.go model/channel_route_model.go model/channel_route_selector.go model/ability.go service/channel_select.go service/channel_health.go
```

逐项状态重新确认：

- 已实现并本轮复核：有效映射单次请求快照、映射环/非法配置排除、V2/cache/legacy 选择一致性、健康 route identity、重试上下文清理、半开探针与 Redis 原子租约、并发 429、基础诊断字段和隔离构建契约。
- 部分实现：`pin_order` 尚未独立建模；并发 release 仍为有界重试而非 reconciliation 队列；异步任务只固定提交/轮询路由，未覆盖完整上游运行期；shadow 记录仍缺少完整 skip reason、聚合和趋势；legacy 诊断仍不是逐候选原因矩阵；Responses/工具/Header Profile/body-limit 主要在候选过滤或 preflight 阶段生效。
- 部分实现（管理端）：Default 有 scope 工作台和容量点时观测；尚无字段级冲突 diff、最后编辑者/时间、5/15/60 分钟趋势和跨 scope 原子事务编辑。Classic 只有 scope 摘要及旧字段守卫，不是完整 scope 工作台。
- 未覆盖：正式 `13000` 未重启或替换；未对真实 201 渠道发起请求，未取得新的 504 request-id、上游响应、生产 Redis/多实例证据；隔离测试不等同于生产验收。

## 2026-08-28 再次复核与脚本边界修复

本轮继续在 `dev` 分支复核，未操作正式服务、Caddy 3000/13000、正式 PostgreSQL/Redis，也未处理 150/mdkj.lol 渠道。

确认并修复两项确定性问题：

1. `scripts/compact-control-plane-e2e.sh` 的异常清理路径现在要求应用已停止、数据库快照已恢复且 Redis 测试状态已清理，才允许重启隔离应用。停止失败、恢复失败或 Redis 清理失败时会保留应用停止状态并返回失败，避免把未恢复的测试数据重新暴露给服务。
2. `scripts/compact-control-plane-e2e.sh` 与 `scripts/codex-real-channel-compaction-gate.sh` 不再硬编码隔离 PostgreSQL 用户名/数据库名；支持从显式覆盖、进程环境和指定 env 文件读取，并支持 gate 的 `CODEX_REAL_GATE_ENV_FILE`、`CODEX_REAL_GATE_COMPOSE_FILE` 覆盖。Compose project 名称也统一使用同一 env 文件解析结果。

本轮验证（命令均在仓库根目录执行，退出码均为 0）：

```text
go test -count=1 -race ./controller ./model ./relay/common ./relay/helper ./service ./router
  controller: ok
  model: ok
  relay/common: ok
  relay/helper: ok
  service: ok
  router: ok

node --test scripts/compact-control-plane-e2e.test.mjs
  2 passed, 0 failed

node --test scripts/codex-real-channel-compaction-gate.test.mjs
  25 passed, 0 failed

bash -n scripts/compact-control-plane-e2e.sh scripts/codex-real-channel-compaction-gate.sh scripts/macos-docker-tun-recovery-watchdog.sh
python3 scripts/compose-healthcheck-contract.py
  root passed; dev-isolated passed; legacy-dev passed

bash scripts/install-macos-docker-tun-recovery.test.sh
  passed
bash scripts/install-macos-docker-tun-monitor.test.sh
  passed
bash scripts/macos-docker-tun-log-retention.test.sh
  passed
bash scripts/macos-docker-tun-recovery-watchdog.test.sh
  passed

git diff --check -- scripts/compact-control-plane-e2e.sh scripts/compact-control-plane-e2e.test.mjs scripts/codex-real-channel-compaction-gate.sh scripts/codex-real-channel-compaction-gate.test.mjs
  no output, exit code 0
```

隔离运行状态复核：`new-api-dev-isolated-new-api-1`、PostgreSQL 和 Redis 均为 healthy；应用端口仍为 `127.0.0.1:3001`，容器 build-info 为 `commit=b42e5f54b2fdb6d0f2d1cd44e4e1cd31af15c1aa-dirty`，与当前 `HEAD` 一致。当前仍未取得 `go test ./...` 的最终退出码，未执行真实渠道请求、正式环境验收或破坏性 E2E 数据重建；上述结论仅覆盖源码、隔离容器健康状态、定向 race 测试及脚本契约测试。

## 2026-08-28 再次全面复核（当前工作区）

本轮只读对照规划基线、源码、测试和隔离运行态；未重启或替换正式 `13000`，未访问正式 PostgreSQL/Redis，也未向真实 201 渠道发请求。

已复核并通过：

```text
go test -p=1 -count=1 ./controller ./model ./relay/common ./relay/helper ./service ./router
  全部通过，退出码 0

go test -p=1 -race -count=1 ./service -run 'TestChannelHealth|TestChannelConcurrency|TestChannelRouteModelResolver|TestDatabaseSelection|TestAutoGroup|TestRequestBodyLimit'
  通过，退出码 0

bun test web/default/tests/channel-health-runtime.test.ts web/default/tests/usage-logs-routing.test.ts
  34 passed, 0 failed

bun test web/classic/tests/channelHealthRuntime.test.js web/classic/tests/usageLogsResponsesCompact.test.js
  33 passed, 0 failed

go vet ./controller ./model ./relay/common ./relay/helper ./service ./router
  通过，退出码 0

web/classic: bun run lint
  通过，所有文件符合 Prettier

隔离容器：running/healthy；/new-api --build-info 为
commit=b42e5f54b2fdb6d0f2d1cd44e4e1cd31af15c1aa-dirty；
curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status
  HTTP 200
```

本轮发现并修正一个测试夹具问题：`service/channel_health_test.go` 的 `RelayInfo` 嵌入字段必须通过 `ChannelMeta` 初始化，修正后 service 定点测试和 race 测试均可编译通过。该修正不改变生产逻辑。

前端 Default `typecheck`、`lint` 与 Classic `build` 已启动但在本机高 I/O 负载下长时间无最终退出码；因此本轮不把它们列为通过，也不把未返回的进程视为失败。全量 `go test ./...` 仍未取得退出码。

规划状态保持不变：通用模型映射快照、V2/cache/legacy 选择一致性、健康 route identity、重试上下文隔离、半开探针、Redis 原子租约、并发 429、Scope 版本守卫和两套前端目标测试已实现并有证据；`pin_order` 独立模型、reconciliation 队列、异步任务完整运行期并发、key 级并发、完整 shadow skip reason/趋势、字段级冲突 diff/编辑者时间、Classic 完整 Scope 编辑器和通用 Header 条件 DSL 仍是部分实现或未实现。正式 `13000` 和真实 201 504 链路仍未覆盖。

## 2026-08-28 全面复核收尾记录

本节记录本轮实际复核结果。所有命令均在 `/Volumes/Work/code/new-api` 或注明的前端目录执行；本轮未修改业务代码、未提交、未重置或清理既有 dirty worktree。

已通过的本地检查：

```text
go test -p=1 -count=1 ./controller ./model ./relay/common ./relay/helper ./service ./router
  退出码 0，全部包通过
go test -p=1 -race -count=1 ./controller ./model ./relay/common ./relay/helper ./service ./router
  退出码 0，全部包通过
go vet ./controller ./model ./relay/common ./relay/helper ./service ./router
  退出码 0
node --test scripts/compact-control-plane-e2e.test.mjs scripts/codex-real-channel-compaction-gate.test.mjs
  退出码 0，28 passed，0 failed
bash -n scripts/*.sh
  退出码 0
python3 scripts/macos-docker-tun-caddy-log-report.test.py
python3 scripts/macos-docker-tun-monitor-alert.test.py
python3 scripts/macos-docker-tun-port-audit.test.py
python3 scripts/macos-docker-tun-time-machine-report.test.py
  合计 25 passed，退出码 0
bash scripts/macos-docker-tun-recovery-watchdog.test.sh
bash scripts/macos-docker-tun-monitor.test.sh
bash scripts/install-macos-docker-tun-recovery.test.sh
bash scripts/install-macos-docker-tun-monitor.test.sh
  均通过，退出码 0
python3 scripts/compose-healthcheck-contract.py
  root、dev-isolated、legacy-dev 均通过，退出码 0
git diff --check
  无输出，退出码 0
```

Default 前端定向测试：首次四文件并行运行时，`channel-health-runtime` 的本地化扫描因本机高 I/O 负载超过 15 秒单测上限；没有代码断言失败。随后单测重跑为 1 passed，最终完整重跑结果为 82 passed、0 failed、632 个断言，退出码 0。Default lint 已退出码 0；Classic Bun 用例此前记录为 22 passed、0 failed。

隔离运行态只读复核：`new-api-dev-isolated-new-api-1`、PostgreSQL、Redis 均为 running/healthy；隔离 `/new-api --build-info` 的 commit 为 `b42e5f54b2fdb6d0f2d1cd44e4e1cd31af15c1aa-dirty`，与当前 `dev` HEAD 一致；`127.0.0.1:3001/api/status` 返回 HTTP 200；3000、13000、3001 的带超时端口探测均成功。正式 new-api、Caddy、正式 PostgreSQL/Redis 未升级、重启或修改。

仍未取得最终结果、不能视为通过：

- `bun run typecheck`（session 34583）仍在运行，无退出码。
- `bun run build`（session 23756）仍在运行，无退出码。
- `go test -p=1 -count=1 ./...`（session 19333）仍在运行，无退出码。
- CodeRabbit 认证已确认，但 `coderabbit review --agent --uncommitted` 连接审查服务时返回 `WebSocket closed`，没有外部 approval；本地测试和静态检查不替代该结果。

结论边界：本轮未发现新的高置信度业务缺陷；此前五项路由/并发问题及脚本配置问题均由源码和定向测试复核通过。由于上述三个长任务仍无最终退出码，不能宣称全量类型检查、Default 构建或全量 Go 测试通过；也未覆盖真实生产渠道、真实跨进程 Redis 故障和正式环境验收。

## 2026-08-29 全面复核继续

本轮继续在 `/Volumes/Work/code/new-api` 的 `dev` 分支进行只读复核，当前 `HEAD=b42e5f54b2fdb6d0f2d1cd44e4e1cd31af15c1aa`。保留现有 tracked/untracked dirty worktree，未回退、清理、提交或修改业务代码；未触碰、升级、重启正式 new-api、Caddy、正式 PostgreSQL/Redis，也未访问真实 150/mdkj.lol 渠道。

已完成的即时检查：

```text
git diff --check
git diff --cached --check
  均无输出，退出码 0

当前工作区：341 个 tracked 修改文件，147 个 untracked 文件；均为历史工作区状态，本轮未据此删除或覆盖。

隔离运行态：new-api-dev-isolated-new-api-1、PostgreSQL、Redis 均 running/healthy；
127.0.0.1:3001/api/status
  HTTP 200
```

此前报告中列出的 5 项路由/并发问题再次对照当前实现确认：非法 `model_mapping` 会在候选装载阶段跳过并继续低优先级 fallback；Redis 租约续期对瞬时错误进行有界重试并在不可安全续期时取消请求；`FixAbility` 通过能力重建保留 V2 路由字段和显式零值；初始渠道并发耗尽统一映射为 429；V2 路由缓存在数据库临时失败时保留旧条目。对应定向测试、race 测试和脚本契约测试均已有前述退出码 0 的记录，本轮未发现新的高置信度缺陷。

仍不能计为通过的长任务：

- `go test -p=1 -count=1 ./...`（session 19333）已运行超过 3 小时，尚无退出码。只读 `lsof` 显示该进程正在遍历 `web/node_modules/@visactor/vchart/...`，属于 `./...` 对前端依赖目录的扫描等待；没有测试失败输出，也不能将其视为成功。
- `web/default` 的 `bun run typecheck` 当前仍在运行，尚无退出码；Default 构建本轮未取得新的最终退出码。
- CodeRabbit 连接此前以 `WebSocket closed` 结束，没有外部 approval；本地测试不能替代该结果。

未覆盖边界保持不变：没有真实渠道请求、真实跨进程 Redis 故障注入、正式环境验收或生产生命周期变更。以上结论只覆盖当前源码复核、已有定向验证证据、隔离容器健康状态和本轮只读检查。

## 2026-08-29 再次全面复核与迁移事务修复

本轮继续以规划文档、当前源码、回归测试和隔离运行态交叉核对。保留既有 dirty worktree，未回退、清理、提交或覆盖无关修改；未重启或替换正式 `13000`，未访问生产 PostgreSQL/Redis，未向真实 201 渠道发请求。

复核发现并修复一处公共迁移 API 的原子性缺口：`model.ApplyRouteFieldsBackfill` 在调用者传入非 nil `*gorm.DB` 时原来直接执行多个 scope 的写入，中途失败可能留下前序 scope 的部分回填。现在统一通过 `tx.Transaction` 执行；传入外部事务时由 GORM 使用嵌套事务/savepoint，保留外层事务组合能力，冲突或写入错误会回滚本次回填。新增 `TestApplyRouteFieldsBackfillRollsBackWhenLaterAbilityUpdateFails`，用第二个能力更新失败的夹具验证首个 scope 的 priority、weight 和 configured 标记均不会残留。

本轮取得最终退出码 0 的验证：

```text
go test -count=1 ./model -run 'TestApplyRouteFieldsBackfillRollsBackWhenLaterAbilityUpdateFails|TestEnsureRouteFieldsBackfilled|TestPreviewRouteFieldsBackfill'
go test -p=1 -count=1 ./controller ./model ./relay/common ./relay/helper ./service ./router
go test -p=1 -race -count=1 ./controller ./model ./relay/common ./relay/helper ./service ./router
go vet ./controller ./model ./relay/common ./relay/helper ./service ./router
bun test web/default/tests/channel-health-runtime.test.ts web/default/tests/usage-logs-routing.test.ts
  34 passed, 0 failed
bun test --timeout=30000 web/classic/tests/channelHealthRuntime.test.js web/classic/tests/usageLogsResponsesCompact.test.js
  33 passed, 0 failed
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
  均退出码 0
docker compose ... ps
  new-api、PostgreSQL、Redis 均 running/healthy
/new-api --build-info
  version=v1.1.0
  commit=b42e5f54b2fdb6d0f2d1cd44e4e1cd31af15c1aa-dirty
curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status
  HTTP 200
nc -z -w 2 127.0.0.1 3000/3001/13000
  三个端口探测成功；仅做只读探测
git diff --check
git diff --cached --check
  均无输出，退出码 0
```

实现状态重新归类：请求级映射快照、非法映射显式排除、cache/DB/legacy/V2/compact 一致路由、健康 route identity、重试上下文清理、半开探针、Redis 原子健康与并发租约、续租/release、初始并发 429、scope 权重版本守卫、旧字段写入守卫以及 Default/Classic 目标测试已有实现和本地证据；迁移回填的外部事务原子性已在本轮补齐。`pin_order` 独立字段、并发 release reconciliation 队列、异步任务运行期配额、key 级并发、完整 shadow skip-reason 聚合/趋势、legacy 逐候选原因矩阵、Header Profile 条件 DSL、Default 字段级冲突 diff/编辑者时间/5-15-60 分钟趋势、Classic 完整 scope 编辑器仍属于规划中的部分实现或未实现，不能按已完成表述。

 Classic 首次默认 5 秒超时的本地化扫描用例随后以 `--timeout=30000` 完整重跑通过；该时序现象不作为业务断言失败。全量 `go test ./...`、Default typecheck/build 若没有本轮最终退出码仍不计为通过。隔离容器健康和 `3001` HTTP 200 仅证明当前 dirty checkout 可启动，不等同于正式 `13000`、真实 201 或生产 504 验收。

## 2026-08-29 清理错误传播复核

本轮针对上一节遗留的 E2E 清理风险做最小修正。未修改业务路由逻辑，未提交、回退或清理工作区；未触碰正式 new-api、Caddy、生产 PostgreSQL/Redis 或真实渠道。

确认的问题：`redis-cli` 在 Redis 返回 `WRONGTYPE` 等服务端错误时可能仍退出码为 0；如果清理脚本只检查进程退出码，就会把失败误判为成功并允许重启隔离应用。另一个遗漏是合成压缩状态 ID 的 PostgreSQL 查询曾用 `|| true` 吞掉连接或查询失败，可能导致 Redis 状态未被枚举却继续清理。

已完成修正：

1. `scripts/compact-control-plane-e2e.sh` 新增 `redis_exec_checked`，同时检查 `redis-cli` 传输退出码和常见 Redis 协议错误前缀；清理中的 PING、扫描、读取、删除、ZREM 和 Lua EVAL 全部改用该包装器。
2. `cleanup_redis_state` 使用 `cleanup_failed` 累积任一操作失败并最终返回非零；`cleanup` 只有在数据库恢复和 Redis 清理都成功时才允许重启隔离应用。
3. `capture_synthetic_state_ids` 不再用 `|| true` 隐藏 PostgreSQL 查询错误，查询失败会阻止 Redis 清理和隔离应用重启。
4. `scripts/compact-control-plane-e2e.test.mjs` 更新健康 metadata 拼接断言，并增加包装器、错误传播和查询失败的契约检查。

本轮实际验证（均在 `/Volumes/Work/code/new-api`，退出码均为 0）：

```text
bash -n scripts/compact-control-plane-e2e.sh scripts/compact-control-plane-e2e-lib.sh
node --test scripts/compact-control-plane-e2e.test.mjs
  4 passed，0 failed
node --test scripts/codex-real-channel-compaction-gate.test.mjs
  25 passed，0 failed
bash -n scripts/*.sh
shellcheck scripts/compact-control-plane-e2e.sh scripts/compact-control-plane-e2e-lib.sh
python3 scripts/compose-healthcheck-contract.py
  root、dev-isolated、legacy-dev 均通过
git diff --check
  无输出
```

另外在隔离 Redis `new-api-dev-isolated-redis-1` 上用一次性、唯一键做实测：先写入字符串，再让 `redis_exec_checked HGET` 触发 `WRONGTYPE`；包装器返回 `checked_rc=1`，随后删除该唯一测试键并确认不存在。Lua 清理过滤器在同一隔离 Redis 上实际编译执行并返回 0，探测键不存在。

当前边界：本轮没有运行会重建数据库和重启隔离应用的完整 `compact-control-plane-e2e.sh`，因为该流程会改变隔离数据库状态且本轮目标是修复清理门禁；没有真实生产/渠道验收、跨进程 Redis 故障注入、全量 `go test ./...`、Default typecheck/build 或 CodeRabbit 外部 approval 的新结果。上述结论仅覆盖脚本源码、契约测试、隔离 Redis 行为验证和静态检查。

## 2026-08-29 收尾复核

修正后的脚本与相关契约再次联合执行，结果为 `29 passed, 0 failed`：

```text
node --test scripts/compact-control-plane-e2e.test.mjs scripts/codex-real-channel-compaction-gate.test.mjs
  29 passed，0 failed，退出码 0
bash -n scripts/compact-control-plane-e2e.sh scripts/compact-control-plane-e2e-lib.sh
bash -n scripts/*.sh
shellcheck scripts/compact-control-plane-e2e.sh scripts/compact-control-plane-e2e-lib.sh
  均退出码 0
python3 scripts/compose-healthcheck-contract.py
  root、dev-isolated、legacy-dev 均通过，退出码 0
git diff --check
git diff --cached --check
  均无输出，退出码 0
```

隔离服务最终只读状态：`new-api-dev-isolated-new-api-1`、PostgreSQL、Redis 均为 `running/healthy`；容器 build-info 的 commit 为 `b42e5f54b2fdb6d0f2d1cd44e4e1cd31af15c1aa-dirty`；`http://127.0.0.1:3001/api/status` 返回 HTTP 200。当前分支仍为 `dev`，HEAD 未改变。工作区既有大量 tracked/untracked 修改均保留，未执行 reset、checkout、clean、提交或正式服务生命周期操作。

进程核对未发现本轮启动的 compact E2E、Go 测试或服务重启进程；另有一个既存的 `web/default` `eslint .` 进程仍在运行，未取得最终退出码，也未被本轮终止。它不作为通过或失败证据。

最终结论：本轮遗留的 E2E metadata 契约断言、Redis 清理错误误判和 PostgreSQL 状态枚举吞错均已修复并取得本地/隔离 Redis 证据；未发现新的高置信度缺陷。全量 Go 测试、Default typecheck/build、CodeRabbit 外部审查、真实生产服务和真实上游渠道仍是明确未覆盖边界，不能据此宣称生产验收。

## 2026-08-29 再次复核：Responses 到 Chat 半开探针兼容修复

本轮继续在 `/Volumes/Work/code/new-api` 的 `dev` 分支复核，当前提交为 `b42e5f54b2fdb6d0f2d1cd44e4e1cd31af15c1aa`。保留既有 tracked/untracked dirty worktree，未回退、清理或提交无关修改；未重启或替换正式 `13000`，未访问生产 PostgreSQL/Redis，未向真实 201 渠道发请求。

### 新发现的确定性缺陷

`relay/responses_via_chat.go` 在桥接期间临时把 `RelayMode` 和 `RequestURLPath` 改为 Chat Completions；`relay/channel/api_request.go` 根据实际上游 URL 记录 `/v1/chat/completions`；桥接退出后恢复原始 Responses 模式。错误收尾调用 `channelHealthActualRouteForInfo` 时会重算实际路径和 `relay.*` operation。原来的 `channelHealthProbeRouteCompatible` 要求 operation 完全相等，因此 Responses 半开探针遇到 500、502 或流解析等 `Immediate=false` 错误时，可能无法把失败立即收束到原探针路由。504 分类器本身仍为 `Immediate=true`，所以该缺陷不能单独解释所有生产 504。

### 修复内容

- 严格保留 channel、请求模型、有效上游模型和具体多 Key 身份匹配。
- `task.*` operation 继续完全严格匹配，避免提交、轮询和内容获取互相污染。
- 普通 `relay.*` operation 允许适配器引入的协议或规范化路径变化，例如 Responses 到 Chat；若协议和路径都未变化而 operation 改变，则显式拒绝。
- 续租失败日志移除内部 half-open probe token 和 concurrency lease token，只保留 channel id 等非凭证诊断字段。
- 增加桥接半开失败回归，验证原始 Responses probe、实际 Chat 路径、RelayMode 恢复、500 失败、原路由 cooldown 和 probe 清理。

### 本轮验证证据

以下命令均在当前工作区执行并取得退出码 0：

```text
go test -count=1 ./service -run 'TestChannelHealth(ProbeRouteCompatible|ResponsesChatHalfOpenFailureCoolsOriginalProbeRoute|ActualRoute|ProbeMatchRequiresRelayRoute|CooldownTransitionsToSingleHalfOpenProbe)'
go test -p=1 -count=1 ./controller ./model ./relay/common ./relay/helper ./service ./router
go test -p=1 -race -count=1 ./controller ./model ./relay/common ./relay/helper ./service ./router
go vet ./controller ./model ./relay/common ./relay/helper ./router ./service
bun test web/default/tests/channel-health-runtime.test.ts web/default/tests/usage-logs-routing.test.ts
  34 passed, 0 failed
bun test --timeout=30000 web/classic/tests/channelHealthRuntime.test.js web/classic/tests/usageLogsResponsesCompact.test.js
  33 passed, 0 failed
cd web/default && bun run typecheck
cd web/default && bun run lint
cd web/default && bun run build
  build、Safari compatibility rewrite/check 均通过
cd web/classic && bun run lint
cd web/classic && bun run build
  build、Safari compatibility rewrite/check 均通过
gofmt -l service/channel_health.go service/channel_health_test.go service/channel_health_probe_lifecycle.go service/channel_concurrency.go
  无输出
git diff --check
git diff --cached --check
  无输出
```

隔离环境验证：

```text
scripts/build-docker-local.sh new-api-local:dev
  退出码 0，镜像构建完成
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env \
  up -d --no-deps --force-recreate new-api
  退出码 0
docker inspect new-api-dev-isolated-new-api-1
  running healthy
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
  version=v1.1.0
  commit=b42e5f54b2fdb6d0f2d1cd44e4e1cd31af15c1aa-dirty
curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status
  HTTP 200
```

### 最终状态边界

本轮确认请求级 `model_mapping` 快照、非法映射显式排除、cache/DB/legacy/V2/compact 一致解析、健康 route identity、重试上下文隔离、半开探针原子租约、TTL/续租/release、Redis fail-open 审计、失败分类、retry 排除、最终渠道归因、初始并发 429、V2 权重字段和迁移事务原子性均有源码及定向验证。普通 relay 的协议/路径桥接兼容缺陷已修复并有回归覆盖。

规划中仍未完成或未完全落地的项目保持不变：`pin_order` 独立字段、并发 release reconciliation 队列、异步任务运行期配额、key 级并发、完整 shadow skip-reason 聚合与趋势、legacy 逐候选原因矩阵、通用 Header Profile 条件 DSL、Default 字段级冲突 diff/编辑者时间/5/15/60 分钟容量趋势、Classic 完整 scope 编辑器。这些不能按已完成表述。

`go test -p=1 -count=1 ./...` 仍有一个历史长进程在扫描完整仓库依赖目录，当前没有最终退出码；因此不能将全量 Go 测试宣称为通过。正式 `13000`、真实 201、生产 Redis 多实例故障和真实上游响应仍未验收。隔离容器 healthy 和 `3001/api/status` 的 200 只证明当前 dirty checkout 可启动，不等同于生产发布或问题已在正式渠道消失。

本轮正式环境只读核对结果：`new-api` 仍使用 `new-api-local:prod-codex-lite-4527d7666`，`/new-api --build-info` 为 `commit=4527d7666a37f9151a57f08ca0bc5e8e08b8fb46`，与当前 dirty checkout 不同；`curl --connect-timeout 3 --max-time 10 http://127.0.0.1:13000/api/status` 返回 HTTP 200。该核对没有重启、替换镜像、修改配置、数据库或 Redis，也没有向渠道 201 发起业务请求。

## 2026-08-29 pin_order 独立置顶收尾

本节覆盖本轮继续完成的管理端渠道置顶顺序收尾。此前较早章节中将 `pin_order` 列为“未完成/部分实现”属于当时的历史快照；截至本节，`pin_order` 已从当前待办状态移除。既有章节不回写，以保留当时的审查时间线。

已完成的实现与边界：

1. `model.Channel` 新增独立的 `pin_order` 字段及索引。默认管理端排序为 `pin_order DESC`、再按路由 `priority DESC` 和 `id DESC`，置顶顺序不再改变请求路由优先级。
2. 新增受管理员鉴权保护的 `GET /api/channel/pins` 和 `PUT /api/channel/pins`。批量更新校验 ID、非负值和重复项，并在单个数据库事务中检查渠道存在性和写入；任一项失败会回滚整个批次。
3. 新增一次性迁移 marker `migration.channel_pin_order.20260829`。迁移只把旧的正数 `priority` 回填到空或零的 `pin_order`，不覆盖已有显式值；marker 写入后，管理员取消置顶不会在后续启动时被恢复。
4. Default 前端的置顶、上移、下移和取消置顶全部写入 `pin_order`；旧 `channel-priority` helper 仅保留兼容导出。复制渠道时显式重置 `pin_order=0`，避免克隆渠道继承管理端置顶状态。

本轮验证（命令均在 `/Volumes/Work/code/new-api` 执行，退出码均为 0）：

```text
go test -p=1 -count=1 ./model ./controller
  model、controller 全部通过
bun test --timeout=30000 web/default/tests/channel-pin-order.test.ts
  4 passed，0 failed
gofmt -d model/channel.go model/main.go model/channel_pin_order_migration_test.go model/channel_sort_test.go controller/channel.go controller/channel_copy_test.go controller/channel_sort_test.go
  无输出
git diff --check
git diff --cached --check
  无输出
```

未覆盖边界：本轮重新运行 `web/default` 的 `bun run typecheck`，约 34 分钟后由外部以 SIGTERM 结束（退出码 143），期间没有 TypeScript 错误输出，因此不能将其视为通过，也不能据此归因于 pin_order 代码。未运行全量 `go test ./...`，未做 MySQL/PostgreSQL 实际迁移演练，未做真实生产服务或真实渠道验收；上述 pin_order 结论由当前源码、SQLite 迁移/事务测试、controller 测试、Default helper 测试、定向 ESLint 和 Prettier 检查支持。正式 new-api、Caddy、正式 PostgreSQL/Redis 未升级、重启或修改。

## 2026-08-30 再次全面复核：任务查询分发与直接视频结果收尾

本轮继续对照规划、当前源码和实际回归结果复核，保留既有 dirty worktree；未回退、清理、提交或覆盖无关修改，未重启或替换正式 `13000`，未访问生产 PostgreSQL/Redis，也未向真实 201 渠道发请求。

### 新发现并修复的确定性问题

1. `middleware/distributor.go`：MJ task fetch/image-seed/list-by-condition、Suno fetch、视频 GET 和 video remix 等 `shouldSelectChannel=false` 路径，在没有 token 指定渠道时仍无条件调用 `SetupContextForSelectedChannel(c, nil, ...)`，会把本应由任务记录恢复渠道的请求确定性变成 500。现在只有存在已加载渠道或 `specific_channel_id` 时才初始化渠道上下文；无指定渠道的任务查询交给对应 controller，指定渠道仍保持严格初始化和错误语义。
2. `controller/video_proxy.go`：已有 provider direct `ResultURL` 的 stored-result 路径不创建 content health attempt，成功写出数据或下载内容后原来仍无条件调用 `contentAttempt.MarkSuccess()`，会触发 nil 指针 panic；渠道已删除但仍有 direct URL 时同样成立。现在 data URL 和 HTTP 内容成功收尾仅在 `contentAttempt != nil` 时结算，stored-result 仍保留 SSRF 校验和失败响应处理。

### 本轮新增回归与验证

新增 `middleware/distributor_context_reset_test.go` 的无渠道任务查询表格回归，覆盖：

- MJ task fetch、image-seed；
- Suno fetch、fetch by id；
- OpenAI 视频 GET、旧视频生成 GET；
- video remix。

已有 `controller/video_proxy_routing_test.go` 的 direct URL 回归覆盖渠道可用（credential rotation）和渠道删除两种场景；修复后两者均成功返回视频内容，不再 panic。

本轮实际取得退出码 0 的验证：

```text
go test -count=1 ./middleware -run '^TestDistributeSkipsChannelSetupForUnselectedTaskFetches$' -v
  7 个无渠道任务查询/内容入口通过
go test -count=1 ./controller -run '^TestVideoProxy(FetchesDirectResultAfterCredentialRotation|FetchesDirectResultAfterChannelDeletion)$' -v
  2 个 direct ResultURL 场景通过
go test -p=1 -count=1 ./controller ./middleware ./model ./relay/common ./relay/helper ./service ./router
  全部通过
go test -p=1 -race -count=1 ./controller ./middleware ./model ./relay/common ./relay/helper ./service ./router
  全部通过
go vet ./controller ./middleware ./model ./relay/common ./relay/helper ./service ./router
  通过
gofmt -d middleware/distributor.go controller/video_proxy.go middleware/distributor_context_reset_test.go
  无输出
git diff --check -- middleware/distributor.go controller/video_proxy.go middleware/distributor_context_reset_test.go
  无输出
```

### 复核边界

本轮确认上述两个确定性缺陷已由源码和定向/相关包 race 测试覆盖；任务型接口的异步运行期配额、key 级并发、release reconciliation 队列、完整 shadow skip-reason 聚合/趋势、通用 Header Profile 条件 DSL、Classic 完整 scope 编辑器等规划项仍未完全实现。全量 `go test ./...`、正式 `13000` 生命周期、真实 201 请求、生产 Redis 多实例故障和真实上游 504 仍未验收；隔离或本地测试不替代这些边界。

## 2026-08-30 当前工作区最终复核

当前 `HEAD=6f064edb0ca606f7e9caca2bd72163643717e394`。本轮保留 dirty worktree，未回退、清理、提交或覆盖无关修改；未重启或替换正式 `13000`，未访问生产 PostgreSQL/Redis，未向真实 201 渠道发请求。

当前重新取得退出码 0 的证据：两处新增回归、相关 Go 包普通测试、race、vet，Default 目标测试 38/38，Classic 目标测试 35/35，相关文件 gofmt 与 `git diff --check` 均通过。隔离 new-api、PostgreSQL、Redis 均 running/healthy；容器 build-info 为 `6f064edb0ca606f7e9caca2bd72163643717e394-dirty`，`3001/api/status` 返回 HTTP 200，3000/3001/13000 带超时端口探测成功。正式容器仍为 `new-api-local:prod-codex-lite-4527d7666`，build-info 为旧提交 `4527d7666a37f9151a57f08ca0bc5e8e08b8fb46`，仅做只读核对。

最终结论：无渠道任务查询的 `nil` 渠道初始化缺陷和 direct `ResultURL` 的 nil attempt panic 已由源码、定向回归、相关包 race 与只读审查代理复核通过；规划中的异步任务运行期配额、key 级并发、release reconciliation 队列、完整 shadow skip reason 聚合/趋势、通用 Header Profile 条件 DSL、Default 字段级冲突 diff/编辑者时间/5-15-60 分钟趋势、Classic 完整 scope 编辑器仍属部分实现或未实现，不能误报为已完成。全量 `go test ./...`、完整 Default 集成 build、CodeRabbit 外部 approval、正式发布、真实 201/504、生产 Redis 多实例故障和真实上游响应仍未覆盖。

## 2026-08-31 当前工作区复核：重复定义修复

本轮在继续保留既有 dirty worktree、未升级或重启正式服务的前提下，发现 `service/channel_health_attempt.go` 与 `service/task_model_identity.go` 同时定义了 `ResolveTaskOriginModelName` 和 `ResolveTaskEffectiveModelName`。该问题会在强制重新编译或 `go vet` 时触发重复声明，属于确定性构建阻断。

处理结果：保留 `service/task_model_identity.go` 作为任务模型解析的唯一实现，从 `service/channel_health_attempt.go` 删除重复实现；未改变调用方接口或解析优先级。

本轮验证：

```text
GOMAXPROCS=2 go test -a -p 1 -count=1 ./service
  exit=0

GOMAXPROCS=2 go test -a -p 1 -count=1 ./constant ./controller ./middleware ./model ./relay/common ./relay/helper ./service ./router
  exit=0

动态生成当前 Git 中 88 个非根源码包参数后执行 `GOMAXPROCS=2 go test -a -p 1 -count=1`
  exit=0；有测试包全部通过，无测试包明确报告 no test files

动态生成同一批 88 个非根源码包参数后执行 `go vet -p 1`
  exit=0

cd web/default && bun run lint
  exit=0

cd web/classic && bun run lint && bun run build
  exit=0；Prettier、ESLint、Vite、Safari/browser 兼容性检查通过

git diff --check && git diff --cached --check
  exit=0
```

根包 `go test -a -p 1 -count=1 .` 和默认前端 `bun run typecheck` 本轮因 macOS 文件系统在嵌入目录/依赖 `.d.ts` 上长时间阻塞，分别被停止，不能计为通过；已有历史明确通过证据仍保留。正式 `new-api`、Caddy 和渠道配置未修改，端口 `3000`、`13000`、`3001` 的只读状态检查均返回 HTTP 200。

## 2026-08-31 当前收尾复核：格式与测试生命周期竞态

本轮继续保留 `dev` 分支的既有 dirty worktree，未回退、提交、清理或覆盖无关修改；未升级、重启或修改正式 `new-api`、Caddy、正式渠道、生产 PostgreSQL/Redis。当前 `HEAD=6f064edb0ca606f7e9caca2bd72163643717e394`。

### 新发现并处理

1. `middleware/distributor_bootstrap_integration_test.go` 的测试数据库清理可能早于 channel-cache 异步刷新完成，`go test -race` 会报告清理函数恢复 `model.DB`/关闭 SQLite 与后台刷新读取 `model.DB` 的数据竞争。清理现在先在测试 DB 仍安装时调用同步 `model.InitChannelCache()`，等待后台刷新结束后再恢复全局状态并关闭连接；该改动只影响测试夹具，不改变生产路径。
2. `constant/context_key.go` 仅存在 gofmt 对齐差异，已格式化。

### 本轮新取得退出码 0 的验证

```text
逐文件 gofmt 检查当前 273 个已修改/未跟踪 Go 文件
  gofmt_checked=273

git diff --check && git diff --cached --check
  exit=0

go test -count=1 -timeout 180s ./constant ./dto ./middleware ./controller ./model ./relay/common ./relay/helper ./relay/channel/codex ./relay/channel/openai ./relay ./relay/channel/... ./relay/channel/task/... ./router ./setting/... ./types ./pkg/...
  分组执行全部通过；无测试包明确报告 no test files

go vet ./constant ./dto ./middleware ./controller ./model ./relay/common ./relay/helper ./relay/channel/codex ./relay/channel/openai ./service
  exit=0

go test -race -count=1 -timeout 180s ./middleware -run '^TestDistributeResponsesBootstrap' -v
  bootstrap 3 项通过，无 race 报告

go test -race -count=1 -timeout 240s ./controller ./middleware ./model ./relay/common ./relay/helper ./service ./router
  全部通过，exit=0

cd web/default && bun run lint
  exit=0

cd web/default && bun run format:check
  exit=0

cd web/default && bun run build
  Rsbuild、Safari compatibility、browser compatibility 全部通过，exit=0

cd web/default && bun test tests/channel-header-profile-strategy.test.ts tests/channel-responses-compact.test.ts tests/channel-health-runtime.test.ts tests/channel-pin-order.test.ts tests/usage-logs-routing.test.ts
  86 pass，0 fail，exit=0

cd web/classic && bun run lint && bun run eslint
  Prettier、ESLint 全部通过，exit=0

cd web/classic && bun test tests/responsesCompactSettings.test.js tests/usageLogsResponsesCompact.test.js tests/channelHealthRuntime.test.js tests/codexFoldedSettings.test.js
  43 pass，0 fail，exit=0
```

### 运行状态与未覆盖边界

- 只读端口核对：`3000`、`13000`、`3001` 均可连接；三者 `/api/status` 均返回 HTTP 200。Caddy 仍由既有进程监听 `*:3000`。隔离容器 `new-api-dev-isolated-new-api-1` 为 healthy，build-info 为 `6f064edb0ca606f7e9caca2bd72163643717e394-dirty`；正式容器仍为 `new-api-local:prod-codex-lite-4527d7666`，build-info 为旧提交 `4527d7666a37f9151a57f08ca0bc5e8e08b8fb46`。
- `web/default` 的 `bun run typecheck` 两次均在 `tsc -b` 长时间无输出且处于 macOS I/O 等待后由本轮停止，退出码 130，不能计为通过；Default 构建成功不等价于 TypeScript 类型检查通过。
- Classic 完整 `bun run build` 两次均在 Vite/esbuild I/O 等待且无错误输出后由本轮停止，退出码 130，不能计为通过；Classic 定向 Bun 测试、Prettier 和 ESLint 已通过。
- 根包/整仓 `go test ./...` 曾在递归扫描与构建产物 I/O 等待中无最终包输出，本轮已停止，不能表述为整仓测试通过；显式 Go 包组及 race/vet 已通过。
- CodeRabbit/其他外部审查没有取得新的 approval；本地测试不替代外部审查。未执行真实 201/504 渠道请求、生产发布、生产 Redis 多实例故障注入或真实上游验收。mdkj.lol/150 渠道按用户要求不再处理。

最终结论：本轮新增的测试生命周期竞态和 Go 格式问题已处理并取得针对性回归证据；没有发现其他可在当前本地、非生产边界内安全处理的高置信度代码缺陷。上述 I/O 阻塞、整仓测试、类型检查、Classic 完整构建、外部 approval、真实生产/上游验收仍是明确未覆盖项。

## 2026-08-31 长任务复核收尾

本轮继续在 `dev` 分支保留既有 dirty worktree；未修改业务代码，未提交、回退或清理文件，未升级、重启或修改正式 `new-api`、Caddy、正式 PostgreSQL/Redis 和渠道配置。

### 新取得的验证结果

```text
cd web/classic && bun run build
  exit=0；10809 modules transformed；Vite built in 43m 9s；Safari compatibility rewrite/check passed；browser compatibility check passed

git diff --check
  exit=0

curl --connect-timeout 2 --max-time 5 http://127.0.0.1:3000/api/status
curl --connect-timeout 2 --max-time 5 http://127.0.0.1:13000/api/status
curl --connect-timeout 2 --max-time 5 http://127.0.0.1:3001/api/status
  三者均返回 HTTP 200

docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
  commit=6f064edb0ca606f7e9caca2bd72163643717e394-dirty
```

### 长任务边界

- `go test -count=1 -timeout 900s ./...` 实际等待约 45 分钟后仍停在 Go 递归目录扫描的 `readdir/stat` 文件系统调用，未输出任何测试包结果；仅中断本轮启动的进程，退出码 130，不能计为通过。
- `cd web/default && bun run typecheck` 实际等待约 21 分钟后仍停在 `tsc -b` 文件系统 I/O，未输出类型诊断；仅中断本轮启动的进程，退出码 130，不能计为通过。
- Classic 构建过程中的大 chunk 提示是非阻断警告；本轮未做无关的拆包重构。

### 无关环境异常

旧的 `new-api-dev-effort-dev-isolated-postgres-1` 当前 `restart_count=1501`、退出码 1。日志显示绑定的临时目录 `/private/tmp/new-api-effort-postgres-fixed` 非空但缺少可用 PostgreSQL 集群元数据，`initdb` 因此反复失败。该容器不属于当前隔离环境，修复需要确认临时数据目录的保留/删除边界；本轮未重启、删除或重建它。

最终结论：Classic 完整构建已补齐并通过；整仓 Go 测试和 Default typecheck 经充分等待仍受本机 I/O 阻塞，保留为未覆盖边界。当前隔离服务和正式服务只读状态正常，没有发现其他可在本轮安全处理的高置信度代码缺陷。

## 2026-09-01 当前 HEAD 隔离重建与长任务复核

本轮继续在 `dev` 分支保留既有 dirty worktree，未回退、提交、清理或覆盖无关修改；未升级、重启或修改正式 `new-api`、Caddy、正式 PostgreSQL/Redis、正式渠道和模型价格。

### 隔离环境重建

仅执行以下隔离服务操作：

```text
docker compose -f deploy/compose/dev-isolated.yml \
  --env-file deploy/env/dev-isolated.env \
  up -d --no-deps --force-recreate new-api
  exit=0；new-api-dev-isolated-new-api-1 Recreated/Started
```

隔离 Compose 解析结果为 `new-api-local:dev`、宿主 `127.0.0.1:3001`，项目标签为 `new-api-dev-isolated`，配置文件为 `deploy/compose/dev-isolated.yml`。重建后容器为 `running/healthy`，镜像 ID 与本地 `new-api-local:dev` 一致。

```text
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
  exit=0
  version=v1.1.0
  commit=f825f23d870df24e38362a7bd1d0a2cd4423a1a6-dirty
  date=2026-08-31T18:40:39Z

curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status
  exit=0；HTTP 200
```

### 只读服务核验

```text
nc -z -w 1 127.0.0.1 3000
nc -z -w 1 127.0.0.1 13000
nc -z -w 1 127.0.0.1 3001
  三个端口均可连接

curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3000/api/status
curl --connect-timeout 3 --max-time 10 http://127.0.0.1:13000/api/status
curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status
  三者均 exit=0；HTTP 200
```

正式容器只读信息仍为：`new-api-local:prod-codex-lite-4527d7666`，状态 `running/healthy`，容器启动时间未因本轮改变；Caddy 进程仍监听 `3000`。没有执行正式容器重建或重启。

### 长任务复核

```text
cd web/default && bun run typecheck
  实际等待约 21 分钟；tsc -b 无诊断输出，进程处于 macOS 文件系统 U 状态；仅中止本轮命令，exit=130

GOMAXPROCS=2 go test -a -p 1 -count=1 .
  首次运行在并行工作区写入期间出现 service/convert.go 对
  relaycommon.GeminiConvertInfo 的暂时性未定义诊断；当前源码已确认该类型存在。
  工作区稳定后再次运行约 21 分钟，未再输出该诊断但仍处于文件系统 I/O 等待；仅中止本轮命令，exit=130
```

同一 HEAD 的 Docker 镜像构建已成功完成主程序编译；此前动态枚举的 88 个非根 Go 包测试与 `go vet`、相关包 race 测试、Default lint/format/build/定向测试、Classic lint/eslint/build/定向测试仍保持退出码 0 的证据。根包完整 `go test` 和 Default 完整 typecheck 因本机 I/O 未取得通过退出码，不能宣称全量验证完成。

### 未覆盖边界与结论

- 未执行整仓 `go test ./...` 的完整通过验证；根包强制测试已充分等待但受文件系统 I/O 阻塞。
- 未取得 Default `bun run typecheck` 的通过结果；Default 构建成功不等价于类型检查通过。
- 未做真实生产请求、真实上游验收、外部 CodeRabbit approval、生产 Redis 多实例故障注入或正式发布。
- `new-api-dev-effort-dev-isolated-postgres-1` 的历史重启循环仍属其他隔离环境，涉及临时 PostgreSQL 数据目录保留/清理边界，本轮未重启、删除或重建。
- `mdkj.lol/150` 渠道和模型价格按用户要求不再处理。

本轮可在当前非生产边界内安全完成的工作已完成；隔离服务运行最新 dirty HEAD 且健康，正式服务保持原镜像和运行状态。剩余项目均为已明确记录的验证或外部环境边界，不存在可在不扩大范围的情况下继续安全修改的高置信度缺陷。

## 2026-09-01 源码更新后的受影响包与隔离镜像再验证

前一轮验证后发现 `service/convert.go` 曾被其他线程更新；待该写入结束后，本轮针对最新工作区重新执行受影响包测试，并重新构建隔离镜像，避免使用旧源码快照作结论。

```text
GOMAXPROCS=2 go test -a -p 1 -count=1 \
  ./service ./relay/channel/claude ./relay/channel/gemini ./relay/channel/openai ./dto
  exit=0
  service 24.198s；claude 0.064s；gemini 0.046s；openai 1.103s；dto 0.078s

scripts/build-docker-local.sh new-api-local:dev
  exit=0
  default 前端构建、Classic 前端构建、Safari/browser 兼容性检查和 Go 主程序编译全部完成
  image=new-api-local:dev
  commit=f825f23d870df24e38362a7bd1d0a2cd4423a1a6-dirty
  date=2026-08-31T20:06:31Z

docker compose -f deploy/compose/dev-isolated.yml \
  --env-file deploy/env/dev-isolated.env \
  up -d --no-deps --force-recreate new-api
  exit=0；仅重建 new-api-dev-isolated-new-api-1
```

重建后的隔离容器镜像 ID 与 `new-api-local:dev` 一致，状态为 `running/healthy`，`docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` 返回同一 `f825f23d870df24e38362a7bd1d0a2cd4423a1a6-dirty`，`curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status` 返回 HTTP 200。正式容器仍为 `new-api-local:prod-codex-lite-4527d7666`、`running/healthy`，启动时间和镜像未改变；本轮没有执行正式服务或 Caddy 的重启、重建和配置修改。

结论：最新源码的受影响 Go 包已重新通过，隔离镜像和容器已同步到当前 dirty HEAD 并健康运行；Default 完整 typecheck、根包/整仓递归测试仍因本机文件系统 I/O 无法取得通过退出码，边界继续按前文记录，不影响已取得的包级测试和 Docker 编译证据。

### 收尾稳定性核对

```text
git status --porcelain=v1、git diff --no-ext-diff --binary
  连续约 44 秒采样 6 次；每次总条目数 533，状态哈希和 diff 哈希均未变化

git diff --check && git diff --cached --check
  exit=0
```

稳定窗口内未发现新的写入或本轮遗留的 `go test`/`bun run typecheck` 进程；当前工作区的 dirty 内容属于既有并行任务集合，未被本轮回退、清理或覆盖。
