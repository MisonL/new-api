# CR-FULL-AUDIT-2026-10-05

## 背景

2026-10-05 用户指示：用最新源码部署隔离开发环境后，派 agents 对最新源码与部署情况做
全维度全场景审查。上一会话（resume `643d118d-262d-49c9-acdb-7a7f9f24387e`）完成部署并派出
6 个顶层 agent + 3 个子 agent，会话在汇总阶段因 API 错误中断。

本记录是**接手方独立复现后的结果**，不是 agent 报告的转述。agent 结论按项目规则
（`subagent-audit-claims-need-independent-verification`：曾 7 个代理中 4 个的 P0/P1 被推翻）
逐条重跑验证，**被推翻的结论单列在第 4 节**。

采样时间：2026-10-05（Asia/Shanghai）。基线 HEAD `d253d1fb9`；隔离容器
`new-api-dev-isolated-new-api-1` 运行 `b1cf361feeff3a929dfcf34cb177090f8ed96418-dirty`
（2026-10-05T09:47:53Z 构建），与 HEAD 相差 3 个提交且**仅含两个文档文件**
（`git diff --name-only b1cf361fe..HEAD` 输出即为全部差异），故容器内 Go 代码与工作区一致。

## 1. 复现成立（附可重跑命令）

### P0 — realtime(WebSocket) 重复扣费，净扣约为实际用量 2 倍

**结论：成立。** 已写成回归测试，修复前按正确原因失败。

涉及三处独立扣费：

1. `controller/relay.go:275` 通用预扣 `PreConsumeBilling` → 建立 `relayInfo.Billing`，
   扣 `QuotaToPreConsume`。
2. `relay/channel/openai/relay-openai.go:697` → `preConsumeUsage` →
   `service.PreWssConsumeQuota`（`service/quota.go:151`）→ `PostConsumeQuota`。
   这是**直扣**，绕开 `Billing`。
3. `relay/websocket.go:52` → `PostWssConsumeQuota`（`service/quota.go:231`）→
   `SettleBilling` → `BillingSession.Settle`。

净扣款 = 预扣 + Σ(逐 turn 直扣) + (实际 − 预扣) = **实际 + Σ(逐 turn 直扣)** ≈ 2 × 实际。

关键前提逐条核对（`BillingSession.Settle` 是增量补扣而非按钱包绝对值结算，
故直扣不会被抵消，见 `service/billing_session.go:47`）：

- `preConsumeUsage`（`relay-openai.go:683-698`）把 `usage` 累加进 `sumUsage` 后调用
  `PreWssConsumeQuota(ctx, info, usage)`，传的是**增量**。
- `sumUsage` 在 `relay-openai.go:610` 作为终态 usage 返回，
  `WssHelper` 用它调 `PostWssConsumeQuota`。
- `UsePrice == true`（按次计费）时 `PreWssConsumeQuota` 首行即 return，
  所以**只影响倍率计费的 realtime 模型**——这解释了为何常规测试观察不到。
- 成功会话不触发 refund（`controller/relay.go:225-234` 仅在 `newAPIError != nil` 时退）。

回归测试：`service/realtime_billing_double_charge_test.go`（本次新增）。
修复前实测输出：

```
--- FAIL: TestRealtimeBilling_NoDoubleChargeAcrossPreConsumeTurnDebitAndSettle
    expected: 995500   （= 1000000 − 4500，即 1×）
    actual  : 991000   （= 1000000 − 9000，即 2×）
--- FAIL: TestRealtimeBilling_TurnDebitAndSettleMustNotBothDebitWallet
    expected: 998500
    actual  : 997000
```

`TestPreWssConsumeQuota_DebitsImmediately` 通过，用以证明直扣真实写库
（否则「双扣」可能只是记账虚影）。

**溯源**：`origin/main` 的 `service/quota.go:231` 同样存在，非本分支引入。

**未覆盖边界**：隔离库 realtime 消费日志为 0 条，无运行时实证；正式库需按 `request_id`
比对 `users.quota` 与 `used_quota` 的背离量。

### P1 — 跨渠道重试复用 `StreamStatus`，成功流被上一次失败流污染

**结论：成立。** `beginResponsesStreamRelayAttempt`（`controller/relay.go:60-73`）只在
`RelayModeResponses*` 下重建 `StreamStatus`；chat / Gemini 流走
`relay/helper/stream_scanner.go:111-112` 的「仅 nil 时初始化」，而
`StreamStatus.SetEndReason` 用 `sync.Once`（`relay/common/stream_status.go:52`）锁定首次原因。

后果：`finalizeRelayChannelHealthProbe` 把成功渠道记成健康探针失败；
`relay-openai.go:178` 的 HandlerStop 分支可能把上一次错误当作本次终态返回。

回归测试：`controller/relay_stream_attempt_boundary_test.go`（本次新增）。
修复前实测：Responses 对照组 PASS，chat 与 Gemini 用例均因
`beginResponsesStreamRelayAttempt` 根本没建立 `StreamStatus` 而失败。

**归属**：3 个 agent 独立报告同一结论，是本次 agent 结论中一致性最高的一条。

### P1 — Claude 错误帧守卫漏掉无 `type` 的错误

**结论：成立。** `relay/channel/claude/relay-claude.go:1149`（流式）与 `:1262`（非流式）：

```go
if claudeError := claudeResponse.GetClaudeError(); claudeError != nil && claudeError.Type != "" {
```

`GetClaudeError()`（`dto/claude.go:594-604`）对 `map[string]interface{}` 只在存在 `type` 键时
填充 `Type`。故上游返回 `{"error":{"code":"rate_limit_exceeded","message":"..."}}` 时
`Type == ""`，守卫为假：错误帧被当正常数据转发，非流式路径随后照常
`PostTextConsumeQuota`——用户为一次限流错误被计费。

对照：OpenAI chat 路径（`relay-openai.go:159`，`responsesOpenAIErrorHasContent`）与
Responses 路径（要求 message 非空）已加固，Claude 未同步。

**未修复**，本记录只确认结论。

### P1 — 前端 `handleServerError` 丢弃全部后端错误信息

**结论：成立，且已用运行中的隔离后端实测。**

`web/default/src/lib/handle-server-error.ts:18` 读 `error.response?.data.title`，而
后端错误体是 `{message, success}`。隔离后端实测：

```console
$ curl -sS http://127.0.0.1:3001/api/user/self
{"message":"Unauthorized, not logged in and no access token provided","success":false}
```

`types.OpenAIError`（`types/error.go:15`）确认字段是 `message`。
全仓库 `"title"` 仅命中 `controller/subscription.go:226`（套餐标题）与
`controller/console_migrate.go:54`，都不是错误体。

后果：所有 AxiosError 走这一支时 `errMsg` 为 `undefined`，`toast.error(undefined)`
渲染空 toast，真实错误被丢弃。`main.tsx:49` 的 mutations `onError` 全量走此路径。

**未修复**，本记录只确认结论。

## 2. 静态可证但缺运行实证（沿用 agent 结论，未逐条复现）

以下条目我只做了代码路径核对，未构造运行时复现。列出以便排期，**不作为已验证结论**：

- `relay/channel/gemini/relay-gemini.go:1475` fallback tool-call ID 跨 chunk 冲突
  （`toolCallOrdinal` 每 chunk 重置为 0、`candidate.Index` 恒 0）。
- `service/http.go:26-34` 上游响应头整体镜像转发，`Set-Cookie` 可落到 new-api 源。
- `relay/channel/api_request.go:53-80` 通配透传拒绝名单缺 `api-key`（Azure 凭证头）。
- `relay/common/override.go:1466-1480` `pass_headers` 无凭证拒绝名单，仅校验格式。
- `relay/channel/claude/relay-claude.go:255-720` OpenAI→Claude 静默丢弃 `response_format`。
- `setting/operation_setting/status_code_ranges.go:19-27` 默认重试含 404/422。
- `controller/passkey.go:341` `AdminResetPasskey` 无目标角色校验。
- `controller/subscription.go:285-367` 订阅管理接口无目标角色校验，且可自绑高级分组。
- `controller/user.go:790` 硬删除用户不清理 Redis 缓存。
- `model/main.go:237` 连接池默认 1000 远超 PG `max_connections=100`。
- `web/default/src/lib/readonly-guard.ts:50-69` 与 classic 对应文件：尾斜杠与百分号编码可绕过。
- `web/classic/src/helpers/utils.jsx:127` `showError` 遇无 response 的网络错误自身抛 TypeError。

## 3. 修复时的陷阱（本次实测踩到，记录以免重蹈）

`model/main.go:25` 的 `commonKeyCol` 由 `initCol()` 懒初始化。`service` 包的
`TestMain`（`service/task_billing_test.go:23`）直接建 sqlite 内存库，**不经过**该路径，
于是 `GetTokenByKey`（`model/token.go:275`）生成 `WHERE  = ?`：

```
Message:"SQL logic error: near \"=\": syntax error (1)"
```

任何在 `service` 包内经令牌查询的测试都必须先调一次 `model.KeyColumn()`。
这是**测试环境初始化缺口，不是产品缺陷**（生产 `InitDB` 会初始化）。

另：生产 `TokenKey` 是 `token.Key` 裸值（`middleware/auth.go:346`），
不含 `sk-` 前缀；`PreConsumeTokenQuota`（`service/quota.go:394`）按该值直接查库、
不做前缀裁剪，而 `PreWssConsumeQuota`（`service/quota.go:100`）会 `TrimPrefix`。
测试里传错格式会得到 `record not found`。

## 4. 被推翻或修正的 agent 结论

按项目规则记录，避免下一轮重复：

- 「两个只读守卫无单元测试」——**错误**。测试在 `tests/` 而非 `src/`，
  `web/default/tests/readonly-guard.test.ts`（9 例）与
  `web/classic/tests/readonlyGuard.test.js` 均存在且在 `bun test` 中运行。
  该 agent 已自行更正。正确表述是「守卫**绕过**（尾斜杠/百分号编码）未被测试覆盖」，
  不是「守卫无测试」。
- realtime 双扣费在两个 agent 中标为 SUSPECTED（缺真实 WS 上游复现），
  经本次回归测试已**升级为 CONFIRMED**。

## 5. 建议的修复顺序

1. **realtime 双扣费（P0）**。`BillingSettler` 已有 `Reserve(targetQuota)`
   （`relay/common/billing.go:23`），把逐 turn 直扣改为经会话 `Reserve` 累积，
   终态 `Settle` 自然只补差额。这样既保留「长会话中途余额不足即中断」的意图，
   又消除双扣；直接删掉直扣会改变现有中断行为，属行为变更。
2. **`StreamStatus` 跨 attempt 污染（P1）**。把
   `beginResponsesStreamRelayAttempt` 的重建条件放宽到所有流式 attempt，
   或在 chat/Claude/Gemini 的重试循环里显式重置。注意 `responses_handler.go` 的
   三个内层重试点已有各自重建，勿遗漏。
3. **Claude 错误帧守卫（P1）**：把 `claudeError.Type != ""` 改为检查
   message/code 是否非空，与 OpenAI/Responses 路径对齐。
4. **`handleServerError`（P1）**：`.data.title` → `.data.message`，一行改动，
   影响面是全部前端错误提示。
5. 其余条目按第 2 节排期，逐条先写回归测试再改。

## 6. 本轮未做

- 未修改任何生产代码（仅新增两个回归测试文件，均按「修复前失败」状态留存）。
- 未重启、未重建、未写入正式服务（`new-api`，13000）与正式数据库。
- 未回退、覆盖或整理其他并行会话的 504 项未提交改动。
- 未推送。`dev` 领先 `origin/dev` 124 个提交。

## 7. 后续修复轮次（2026-10-05 夜 - 10-06）

以下每条都先写回归测试、确认「修复前失败」，再改生产代码。测试与实现同一次提交。

| 提交 | 内容 | 回归测试 |
| --- | --- | --- |
| `ea4033114` | 前端错误提示：`web/default` 的 `handleServerError` 读 `.data.title` 改为 `.message`；`web/classic` 的 `showError` 补 `error.response` 空值守卫，并在 default 分支优先展示后端 message | `web/default/tests/handle-server-error.test.ts`、`web/classic/tests/showError.test.js` |
| `2b70bae80` | `AdminResetPasskey` 补目标角色校验（原先任何管理员可删除 root 用户的 Passkey） | `controller/passkey_admin_test.go` |
| `38fe02eba` | 凭证请求头不得透传：通配/正则透传补 `api-key`；`pass_headers` 新增凭证拒绝名单，两条路径共用 `relay/common.IsCredentialHeader` | `relay/channel/header_passthrough_credentials_test.go`、`relay/common/header_credentials_test.go` |
| `5040f4d98` | 上游响应头镜像：`ShouldCopyUpstreamHeader` 拒绝 `Set-Cookie` 与 RFC 7230 hop-by-hop 集合 | `service/http_header_mirror_test.go` |
| `aff83779d` | 订阅管理五个 admin 接口补目标角色校验；按订阅 ID 寻址的两个接口先解析归属用户 | `controller/subscription_admin_role_test.go` |
| `a80f89ac7` | Claude 通道不再丢弃 `response_format`：映射到 `output_config.format`，并与 `effort` 合并而非互相覆盖 | `relay/channel/claude/claude_response_format_test.go` |

### 7.1 被推翻的第 2 节条目

- **「默认重试含 404/422」不成立，不改**。404 重试是**有意设计**且已有测试钉住：
  `controller/relay_retry_test.go:3333` 起的三条用例分别要求
  「model 404 不 fallback」「compact route 404 fallback」「generic compact 404 fallback」。
  更根本的是 `shouldRetry` 在到达状态码规则前已有两道拦截：
  `isDeterministicModelNotFoundError`（`controller/relay.go:672`）直接对确定性
  model-not-found 返回 false，`shouldRetryUpstreamRequestTooLargeForResponsesCompact`
  （`:2332`）对 compact 路由显式返回 true。改默认范围会破坏这两条已声明的行为。
  第 2 节该条目属**未复现即转述**，此处更正。

### 7.2 本轮实测踩到的测试环境陷阱（补充第 3 节）

两处都让「拒绝类」用例**因为错误原因通过**（SQL 语法错误被当成权限拒绝），
必须记下以免下一轮重蹈：

- `model.KeyColumn()` 不只初始化 `commonKeyCol`，还会初始化 `commonGroupCol`。
  `controller` 包测试若不调用它，`getUserGroupByIdTx`（`model/subscription.go:396`）
  会生成 `SELECT  FROM users` 并报 `near "FROM": syntax error`。
- 订阅套餐缓存是**进程级**的（`sync.Once` + 内存 hot cache，`model/subscription.go:47`）。
  每个测试各有独立 sqlite 库，但缓存不随库重建，前一个测试写入的套餐会残留到下一个。
  本轮的处理是：每个测试用互不相同的 `plan_id`，且请求体里的 `plan_id` 必须与种入的
  一致——最初只改了种入值没改请求体，用例于是因「套餐不存在」而假通过。

### 7.3 本轮未做

- 未处理 `readonly-guard` 的尾斜杠/百分号编码绕过：该守卫与其测试文件
  （`web/default/src/lib/readonly-guard.ts`、`web/classic/src/helpers/readonlyGuard.js`
  及两侧 `tests/readonly-*`）在本轮开始时**均为其他会话的未跟踪文件**，
  改动它们等于接手他人未提交的工作，按仓库规则不越界。
- 未改 `controller/user.go`（`HardDeleteUserById` 缓存）、`model/main.go`（连接池默认值）、
  `relay/channel/gemini/relay-gemini.go`（fallback tool-call ID 冲突）：
  这三个文件在本轮开始时带有其他会话的未提交改动。
- 未推送。

## 8. 2026-10-06 升级开发环境后的全场景复核

开发环境已重建到 `8584dde2b`（此前是 `b1cf361fe`，落后 18 个提交）。用 6 个只读
子代理分维度复核，我自己逐条独立复现了其中可验证的部分。

### 8.1 已确认成立的发现

**F0 — 从 `6a5374a32` 起 HEAD 一直不可编译，第一个破损提交就是我自己的（P0）**

这是本轮最严重的发现，且**归因到我自己**。

`ec666d063`（污染提交）引用了 4 个符号，而**定义方全部停留在未提交状态**：

| 符号 | 使用点（已提交） | 定义位置（未提交） |
| --- | --- | --- |
| `info.ResponsesStreamAttemptStarted` | `controller/relay.go:75,84` | `relay/common/relay_info.go` |
| `constant.ContextKeyResponsesStreamSequenceNumber` | `controller/relay.go:82` | `constant/context_key.go` |
| `helper.ResetEventStreamHeaders` | `controller/relay.go:500` | `relay/helper/common.go` |
| `settings.AllowMissingCodexDynamicRequestHeaders` | `controller/relay.go:1657` | `dto/channel_settings.go` |

但**第一个破损的提交不是 `ec666d063`，而是它前面一个**。逐提交验证
（每个提交 `git archive` 到 `/tmp` 后 `go vet ./controller/`）：

```
d253d1fb9  OK          <- 破损前的基线
6a5374a32  BROKEN      <- 我的第一个提交
ec666d063  BROKEN
2cf89cbea  BROKEN
253d93101  BROKEN
ea4033114  BROKEN
2b70bae80  BROKEN
38fe02eba  BROKEN
5040f4d98  BROKEN
aff83779d  BROKEN
a80f89ac7  BROKEN
8584dde2b  BROKEN
```

`6a5374a32` 的破损原因不同：它新增的 `controller/relay_stream_attempt_boundary_test.go`
调用了 `beginResponsesStreamRelayAttempt`，而该函数是**下一个**提交 `ec666d063` 才引入的：

```console
$ git archive 6a5374a32 | tar -x -C /tmp/hc3 && cd /tmp/hc3 && go vet ./controller/
vet: controller/relay_stream_attempt_boundary_test.go:39:2: undefined: beginResponsesStreamRelayAttempt
```

即该提交的说明「both tests are expected to fail」在 controller 包处**不成立**——
它不是「按预期失败」，而是**编译不过**。当时我用「service 包那个测试依赖齐全可编译」
来支撑整句结论，没有单独验证 controller 包。

`ec666d063` 的 4 个符号问题可复现（干净导出 HEAD 后编译）：

```console
$ git archive HEAD | tar -x -C /tmp/hc && cd /tmp/hc && go build ./controller/
controller/relay.go:75:10: info.ResponsesStreamAttemptStarted undefined
controller/relay.go:82:36: undefined: constant.ContextKeyResponsesStreamSequenceNumber
controller/relay.go:500:10: undefined: helper.ResetEventStreamHeaders
controller/relay.go:1657:14: settings.AllowMissingCodexDynamicRequestHeaders undefined
```

`git log -S` 对这 4 个符号在**全部历史**中均无命中，证明它们从未被提交过。

**为什么一直没被发现**：所有构建路径都用工作树，而工作树里那 4 个定义都在、能编译。
开发环境容器也是从工作树构建的。**没有人从 HEAD 树构建过。**

**影响范围**：任何人 `git clone` 后按官方流程建完前端再 `go build` 都会失败。
工作树与运行中的服务不受影响。

**未修**。三条可选路径都需要你决定，我都不应单方面执行：
1. 把 4 个定义文件与 `controller/relay.go` 一起补提交——但这会把其他会话更大一块
   在途工作提交进版本库，正是我一直在避免的越界；
2. 从 `ec666d063` 撤掉那 5 处引用——需改写其他会话已基于其上的历史，且会让工作树失去
   当前可编译状态；
3. 保持现状并记录，等定义方会话自己提交。

**F1 — `copy_header` / `move_header` 可绕过凭证拒绝名单（P1，本轮新发现）**

第 7 节 `38fe02eba` 只给 `pass_headers` 加了凭证拒绝名单。我实测确认
`copy_header` 走同一个数据源（`getRequestHeaderValueFromContext`，`relay/common/override.go:1450`）
却没有同样的判据：

```go
info := &RelayInfo{RequestHeaders: map[string]string{"Authorization": "Bearer client-new-api-token"}}
// param_override: [{"mode":"copy_header","from":"Authorization","to":"X-Upstream-Auth"}]
// 结果：err=<nil>，runtime headers = map[x-upstream-auth:Bearer client-new-api-token]
```

即 `{from: Authorization, to: X-Upstream-Auth}` 仍能把调用方令牌交给上游。
`move_header` 委托给 `copy_header`，同样漏。

**未修**：修复会破坏既有测试 `TestApplyParamOverrideCopyHeaderFromRequestHeaders`
（`relay/common/override_test.go:1324`，由 `7633863c9` 引入），该用例正是拿
`copy_header from Authorization` 来验证 conditions 机制——**这是一处设计冲突，
需要先决定「conditions 依赖能否读凭证头」，不是一行守卫能了**。已写好回归测试
草案但连同试探性改动一并撤回，验证方式与结论见下。

**F2 — 生产与开发复用同一套 PG/Redis 口令，且口令是 `123456`（P1）**

生产 `.env` 显式设置 `POSTGRES_PASSWORD=123456`（sha256 前 12 位 `8d969eef6eca`，长度 6），
开发 `deploy/env/dev-isolated.env` 用**同一个值**。实测比对：

```
dev  SQL_DSN  : <cred sha=e64d02ea82df>@postgres:5432/new-api-dev
prod SQL_DSN  : <cred sha=e64d02ea82df>@postgres:5432/new-api
```

**缓解事实**（实测）：生产 `postgres` 与 `redis` 均**无宿主端口映射**
（`{"5432/tcp":null}` / `{"6379/tcp":null}`），只在 `new-api_new-api-network`
（172.24.0.0/16）内可达；开发在 `172.18.0.0/16`，两个网络成员无交集，
跨项目 DNS 解析在机制上不可能。生产 PG 创建于 2026-08-06，早于编排改为 fail-closed。

**因此这不是当前的越权路径**，但根 `docker-compose.yml:206` 已要求
`POSTGRES_PASSWORD:?` 显式提供，即代码侧已经认定弱口令不可接受，只是存量实例未轮换。

**F3 — HEAD 上存在「有写入、无读取」的代码，其注释声称已被使用（P2）**

`ec666d063` 一并扫入的另一段代码在 HEAD 上是空转的。实测：

```console
$ git grep -n "relayAttemptChannelIdKey" HEAD -- '*.go'
HEAD:controller/relay.go:445:  common.SetContextKey(c, relayAttemptChannelIdKey, channel.Id)
HEAD:controller/relay.go:659:  // relayAttemptChannelIdKey carries the channel of the attempt currently being
HEAD:controller/relay.go:664:  const relayAttemptChannelIdKey constant.ContextKey = "relay_attempt_channel_id"

$ git grep -n "GetContextKeyInt(c, relayAttemptChannelIdKey)" HEAD -- '*.go'
  （无输出）
$ git grep -n "GetContextKeyInt(c, relayAttemptChannelIdKey)" -- '*.go'
controller/relay_responses_routing_trace.go:158:  if attemptChannelId := common.GetContextKeyInt(c, relayAttemptChannelIdKey); ...
```

即写入点在 HEAD、读取点只在工作树（`controller/relay_responses_routing_trace.go`
同样是未提交文件）。注释说它「carries the channel of the attempt currently
being traced」，但在已提交的树里这个 context key 没有任何消费者。

**为什么记这条**：它与 F0 同源——都是污染提交把别处的在途改动切成了碎片。
一个注释描述的行为在 HEAD 上不存在，比一段缺失的代码更容易骗过后续审查。

### 8.2 被推翻或降级的说法

- **`DEV_GLOBAL_RATE_LIMIT_ADMIN_EXEMPT=true` 与注释矛盾**——**成立但影响很小**。
  compose 第 38 行注释写「默认值与正式环境一致」，实际 `dev-isolated.env:52` 覆盖为 `true`，
  容器内实测为 `true`，而生产根本不设该变量（走代码默认 `false`）。
  结果是隔离环境不复现生产的限流豁免行为。仅影响复现保真度，不构成安全缺陷。
- **`/api/uptime` 返回 404**——**不是问题**。该路径本就不存在，
  真实路由是 `/api/uptime/status`（`router/api-router.go:26`），返回 200。
- **健康探针超时一次**——**非故障**。容器 Health.Log 有 1/5 条
  `wget: download timed out`，但同秒应用侧日志显示 `/api/healthz` 返回 200、耗时 32µs，
  且后续 4 次全部成功、`FailingStreak=0`、`RestartCount=0`。判为探针侧瞬时抖动，证据不足以定论。

### 8.3 环境侧事实（复核通过）

- **隔离性**：三个容器 Compose 标签齐备指向 `dev-isolated.yml`；端口全部绑 `127.0.0.1`
  （3001/5433/6381），无 `0.0.0.0`；挂载全用 `type: bind` + 显式变量 + `:?` 强制；
  开发无任何挂载指向生产目录；两个 Docker 网络成员无交集。
- **密钥隔离**：`SESSION_SECRET` / `CRYPTO_SECRET` 生产与开发四项哈希两两不同，已正确隔离。
- **运行健康**：`healthy`、`RestartCount=0`、启动至今 ERROR/WARN/panic/FATAL 命中 0；
  实连 Postgres（`172.18.0.4` 有活跃连接、58 张表）非 SQLite 回退；
  Redis 有真实读写（`tot-cmds=55`，键为限流器写入）；CPU 约 0.05%。
- **服务面**：3001 提供的是 **classic** 前端（`/assets/index-DbfGiSNE.js` + semi-ui，
  且 `/api/status` 自报 `theme: classic`，DB 无 theme 覆盖，代码默认即 `classic`）。
  **因此不能用 3001 验证 `web/default` 的改动**，与 AGENTS.md 第 4 节一致。

### 8.4 复核方法上的一条教训

子代理报告 F1 类问题时容易只给结论。我要求每个结论附「确切命令 + 原始输出」后，
`copy_header` 那一项才能被我独立复现。**在此之前它只是一个可疑线索。**
本次已按此法逐条重跑：F1、F2 与三条被推翻项均由我自己重新取证，未直接采信子代理结论。

### 8.5 部署验证（运行中的二进制）

用二进制级证据确认 7 项修复确实编进了正在运行的服务（容器 `new-api-dev-isolated-new-api-1`），
不止是字符串匹配。关键点：该二进制被 strip，`go tool nm` 不可用（`no symbol section`），
所以证据取自 `.gopclntab`（Go 函数名表 + 源码行号表，未 strip）。

我独立复现的部分：

```console
$ docker exec ... grep -a -c -F 'pass_headers must not pass credential header' /new-api
1
$ docker exec ... grep -a -o -E '[a-zA-Z0-9_./-]*IsCredentialHeader[a-zA-Z0-9_./()-]*' /new-api
github.com/QuantumNous/new-api/relay/common.IsCredentialHeader
$ docker exec ... grep -a -o -E '[a-zA-Z0-9_./()-]*ClaudeError[).]*HasContent[a-zA-Z0-9_./()-]*' /new-api
github.com/QuantumNous/new-api/types.ClaudeError.HasContent
```

逐项字符串命中（`grep -a -c -F`）：`pass_headers must not pass credential header`=1、
`无权重置同级或更高级用户的 Passkey`=1、`无权操作同级或更高级用户的订阅`=1、
`response_format.type %q is not supported by Claude`=1、`response_format.json_schema is required`=1。
9 个相关提交全部是构建提交 `8584dde2b` 的祖先（`git merge-base --is-ancestor` 逐条 YES）。

子代理另用行号表做了更强的证明（例如 `service.ShouldCopyUpstreamHeader` 的行号从修复前
的 `http.go:26` 移到 `http.go:44-54`，且新增了 `ToLower`/`TrimSpace`/`mapaccess2_faststr`
三个修复前不可能出现的调用），以及 `PreWssConsumeQuota` 在 `quota.go:155` 的两处接口间接调用
（`FF /2`）。这些我未逐条复现，**标注为未独立验证**。

### 8.6 鉴权边界：一条必须说清的验证局限

```console
$ curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:3001/api/user/self
401
$ curl ... -H 'Cookie: session=deadbeef' ... /api/user/self
401
$ curl ... -H 'Cookie: session=deadbeef' -H 'New-Api-User: 1' ... /api/user/self
401
$ curl ... /api/status
200
```

**这证明了什么**：`/api/user/self` 挂在 `UserAuth()` 下（`router/api-router.go:89`），
未认证请求被正确拒绝，中间件链在运行二进制中生效，服务健康（`/api/status` 200 是对照）。

**这没有证明什么**：它**不能**证明 `New-Api-User` 头的强校验。伪造的 session cookie
在第一道「未登录」检查就被拦下，永远走不到 `middleware/auth.go` 里校验该头的分支。
要隔离验证需要有效登录态，而登录是 POST，本轮只读约束不允许。

### 8.7 复核未覆盖的边界

- 未做计费、鉴权、转发链路的写操作验证（只读约束）。
- 未验证 `a846e1c64` 对镜像二进制的影响——该提交只改 `docs/` 与 `tools/`
  （后者有独立 `go.mod`，根模块不 import），Dockerfile 不 COPY 这两个目录，
  判定不影响运行时；但**未重建镜像做字节级比对**。
- `logs` 表 41 万行而 `pg_stat_user_tables.n_live_tup` 仅 7、从未 autoanalyze，
  属既有批量导入数据的状态，是否需要处理未评估。

