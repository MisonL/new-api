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

> 本节列出的四项代码工作已于 2026-10-06 全部完成（`readonly-guard` 路径归一化见
> `a65345cca`，其余三项见 `4b2ad1282`、`6331a1dad`、`4dee3b71b`），详见第 9 节；
> 「未推送」一项截至第 9 节写作时仍然成立。

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

**已修**（`f874fe95e`）。判据落在「值的来源」而不是「头的名字」：
`getHeaderValueFromContext` 先查 `header_override` 再查 `request_headers`，
所以运维若已用 `set_header` 主动写入同名头，复制的是他们自己配置的值，
必须放行；只有**只可能来自客户端**的值才拒绝。一刀切拒绝头名更简单，但是错的。

既有测试 `TestApplyParamOverrideCopyHeaderFromRequestHeaders`
（`relay/common/override_test.go:1324`，由 `7633863c9` 引入）用 `Authorization`
作复制来源，正是有漏洞的那种形态；它的主题是 copy+conditions 机制，
来源头名只是顺带，已改为非凭证名 `X-Client-Auth`，覆盖的机制不变。
反向验证：新测试在撤掉守卫后失败、恢复后通过。

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

**已部分处理（2026-10-06）**：开发侧口令已轮换为独立强口令（48 位十六进制），
`DEV_POSTGRES_PASSWORD` / `DEV_REDIS_PASSWORD` / `DEV_SQL_DSN` / `DEV_REDIS_CONN_STRING`
四处同步更新。轮换后实测：

```
dev  PG    sha256[:12] = e509bc82c379   (原 8d969eef6eca，与生产相同)
prod PG    sha256[:12] = 8d969eef6eca
dev  Redis sha256[:12] = 5dc58a9ce5e9   (原与生产相同)
prod Redis sha256[:12] = 8d969eef6eca
```

从容器网络内以 scram 验证：旧口令 `FATAL: password authentication failed`，
新口令成功。数据无损（users 10 / channels 22 / logs 413163，与轮换前一致），
容器 `healthy`，启动日志 ERROR/WARN/panic 命中 0，应用持有活跃 Redis 连接
（`172.18.0.4`，`tot-cmds=8`）。

**生产侧口令未轮换**：交接约束禁止触碰生产服务与数据库，轮换需停服窗口。
生产 `.env` 未被 git 跟踪（`.gitignore:16`），未修改；生产三个容器启动时间
仍为 2026-10-05，本会话未触碰。

**用户决定（2026-10-06）：生产侧不轮换，此项关闭。** 不要再作为待办重新提出。
风险边界如实保留：生产 PG/Redis 无宿主端口映射，只在 `new-api_new-api-network`
内可达，因此弱口令不构成外部可达的越权路径；这是已知且被接受的残余风险。

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


## 9. 2026-10-06 服务重建与全维度审查（第二轮）

开发环境已重建并升级到 `a65345cca`，随后用 7 个只读子代理分维度审查源码与部署环境，
本节的每条修复都由我自己复现后落地。

### 9.1 服务重建

起因：`new-api-dev-isolated-redis-1` 与 `postgres-1` 容器创建于 09-29，凭据轮换后只更新了
env 文件与运行中的服务，容器 env 仍是旧值。redis 的 healthcheck 读容器 env，因此恒失败。

```console
$ docker inspect ...redis-1 --format '{{range .Config.Env}}...' | grep REDIS_PASSWORD  # 长度=6
$ grep DEV_REDIS_PASSWORD deploy/env/dev-isolated.env                                   # 长度=48
$ docker exec ...redis-1 redis-cli -a <env文件密码> CONFIG GET requirepass              # 长度=48
```

修复方式是重建两个容器（数据在 bind mount 上，redis 为空库）：

```console
$ docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env \
    up -d --no-deps --force-recreate redis postgres
$ docker exec ...postgres-1 psql -U root -d new-api-dev -c "SELECT count(*) FROM users/channels/logs"
users=10 channels=22 logs=413177   # 与重建前一致
```

重建后三容器均 healthy，`new-api` 运行二进制 commit 与 HEAD 一致，`/api/status` 与
`/api/healthz` 均 200。

**一条测试方法教训**：初次判断 postgres 凭据时在容器内用 `127.0.0.1` 连接，
`pg_hba_file_rules` 显示 `local`/`127.0.0.1`/`::1` 为 `trust`，密码根本未被校验，
因此「任意密码都能连」是假象。改从容器网络（应用实际路径）重测后，
新密码通过、旧密码 `123456` 与错误密码均被 `scram-sha-256` 拒绝。

### 9.2 本轮修复（均已提交，附反向验证）

| 提交 | 内容 | 验证方式 |
| --- | --- | --- |
| `fcc4d2fed` | `sync_fields` 与 `{client_header:}` 读取客户端凭证 | 撤护栏后两侧 10 条 FAIL；恢复后 6 包全绿 |
| `32a6fef2f` | 日志库分组列按日志库方言引用（主库 PG + 日志库 MySQL 时曾生成 `"group"`） | 真实 MySQL 8.4 实测：`SELECT "group"` 返回字面量、`WHERE "group"='g1'` 返回 0 行；干净树双向验证 |
| `6af42c521` | 每次重试前恢复客户端流式意图（`IsStream` 派生状态跨渠道泄漏） | 两个 httptest 上游端到端复现：修复前渠道 B 的 JSON 被当 SSE 处理并返回 500 |
| `8cd99b329` | 只读守卫不再拦截纯读的 `GET /api/setup`（两层同步修改） | 恢复条目后两侧新用例失败；nginx 正则用等价判定表复核 |
| `0582fb1be` | compose 契约脚本接受 `sh -c` 形式的 healthcheck | 新增单测锁定两种形态 + 6 个反例；干净树双向验证 |

`32a6fef2f` 与 `0582fb1be` 所在文件带有其他会话的在途改动，两笔提交均以 HEAD 版本为基线、
只叠加本次改动（提交信息中已注明）。

### 9.3 被推翻或修正的结论

- **跨渠道重试请求体不回绕**：不成立。`common/gin.go:76-84` 的 `GetRequestBody` 每次命中
  缓存的 `BodyStorage` 都执行 `bs.Seek(0, io.SeekStart)`，控制器循环在 `controller/relay.go:265`
  取 body 时游标已复位。提出该结论的代理已撤回。
- **「只读前端每次导航都重试并重复弹提示」**：不成立。`web/default/src/routes/__root.tsx:96-108`
  的 `getSetupStatus().catch(...)` 返回 `null` 后仍会执行 `setupStatusChecked = true` 并写入
  localStorage，所以是**一次**误报而非每次导航。缺陷本身成立（纯读被拦 + 一次无谓报错），
  但严重度按实测下调，注释按实测行为改写。
- **`logs` 表统计异常**：不需要处理。规划器读 `pg_class.reltuples` 并按物理块数缩放
  （实测推算 413589 对实际 413177，差 0.1%），`n_live_tup` 归零不影响执行计划；
  表无膨胀（填充率约 93%，`n_dead_tup=0`）。

### 9.4 复核方法教训

- **状态码不等于路由命中**：`//api/status` 返回 200 但响应体是前端 `index.html` 外壳，
  请求并未到达 API。探测路径解析必须同时看响应体，并用 `curl --path-as-is`
  关闭 curl 自身的路径归一化。
- **容器内测凭据会绕过校验**：见 9.1，`trust` 规则使密码校验失效。
- **子代理结论仍需逐条复现**：本轮子代理提出的 P0/P1 中，有 1 条被完全推翻、
  1 条的严重度被下调、1 条的影响面被证实更窄。

### 9.5 本轮未覆盖的边界

- 计费与配额维度的子代理因 API 超时未交付报告，该维度本轮**无已验证结论**。
- 中继协议转换维度、鉴权维度、桌面端与工具链维度的子代理报告尚未全部交付或未成文，
  其结论未经我复现。
- SSE 解析相关的一批候选（多行 `data:` 不聚合、`[DONE]` 前缀匹配、若干渠道自写扫描器
  的 64KiB 上限）来自并行会话，我只核对了代码位置，**未逐条独立复现**。
- 生产容器本轮仍未触碰；生产侧配置一致性、`redis:latest` 非固定 tag、
  compose 无资源限制等部署类发现仅记录，未改动。


## 10. 2026-10-06 审计发现修复（第三批）

§9 写成之后的七个提交。每条都在本机复现、按 TDD 先写失败用例、反向验证后落地。

### 10.1 修复清单

| 提交 | 内容 | 反向验证 |
| --- | --- | --- |
| `222876349` | Claude/Gemini 健康流被误判为上游中断：共享扫描器把「干净 EOF 且无 `data: [DONE]`」判为中断，而两家协议分别以 `message_stop` 与候选 `finishReason` 收尾。Claude 见到 `message_stop` 后 `sr.Done()`；Gemini 用新增的 `sr.MarkDone()` 只记结束原因不停扫描器（终止分片后可能还有 usage 分片） | 修复前 3 条断言 `EndReason="upstream_transport_interrupted"`；缺少终止分片的反向对照仍判中断 |
| `ea334e4ce` | `ManageUser` 的 delete 分支补 `return`：原先落到 `user.Update(false)`，`DB.First` 对已软删行必然 not found（错误被忽略），末尾 `updateUserCache` 把已删用户以 `Status=enabled` 写回 Redis，TokenAuth 在 TTL 内继续放行 | 撤 `return` 后新用例失败（缓存仍在），恢复后全绿 |
| `0618c2f0c` | `check-browser-compatibility.mjs`：dist 目录不存在时改为收集并 exit 1，不再静默通过 | 三个用例按退出码验证：不存在目录 1、真实 dist 0、无参数 1 |
| `816e15f72` | 6 个自建扫描器渠道（cloudflare/coze/tencent/zhipu/cohere/ollama）从不写 `StreamStatus`，健康流被 `classifyStreamStatus` 记成 `error`、健康探针永不成功；新增 `helper.RecordStreamEnd` 按共享扫描器规则分类（见 10.2） | 修复前 6 条健康流用例 `EndReason=""` 全 FAIL；把 `RecordStreamEnd` 扰动为恒判正常后 5 条反向对照全 FAIL |
| `38f9e101a` | `AdminCompleteTopUp` 补与 `ManageUser` 相同的层级护栏：管理员不得给自己、同级或更高角色补单（此前可借补单绕过 `add_quota` 的限制） | 扰动护栏后恰好 3 条拒绝用例失败；普通用户补单与 root 补单两条正向对照不受影响 |
| `11e7fec27` | cloudflare 流解析对齐共享扫描器：接受 `data:` 后不带空格的帧（原先这类帧 Unmarshal 失败被静默丢弃） | 撤解析改动后新用例失败（内容帧丢失） |
| `b0602e4df` | `web/default` GET 去重不再吞掉 `AbortSignal`：带 signal 的请求各自成请求，不再共享 promise（共享 promise 无法同时满足两份独立取消生命周期；react-query 的 dashboard 查询会转发 signal） | 撤护栏后恰好 2 条生命周期用例失败；`bun test tests/` 207 通过 0 失败 |

### 10.2 自建扫描器渠道的终止标记

`RecordStreamEnd(c, info, sawTerminalMarker)`（`relay/helper/stream_end.go`）与共享扫描器
（`stream_scanner.go:202`）同一规则：见到协议终止标记记 `EOF`，干净结束但缺标记则记
`upstream_transport_interrupted` 并写 ERROR 日志。各渠道的标记：cloudflare `data: [DONE]`、
coze `conversation.chat.completed`、tencent 尾包 `FinishReason=stop`、zhipu `meta` 事件、
cohere `is_finished=true`、ollama `done` 帧（该渠道本就对缺帧返回错误，只在成功路径补记录）。

**同类缺陷未覆盖**（记录待办，均为代码级核对、未修复）：
- `aws/converse.go`：成功路径已强制要求 `message_stop`+`metadata`，但 `AwsClient` 是具体类型
  `*bedrockruntime.Client`，写回归测试需要事件流 fake；仅缺一行 `SetEndReason`。
- `xunfei`：WebSocket 读取器在消费端无法区分「收到 status=2 终止帧」与「读错误」，两者都走
  `stopChan`，修复需要跨 goroutine 传递终止标志。
- `palm`：`palmStreamHandler(c, resp)` 没有 `RelayInfo` 参数，无法写 `StreamStatus`；
  且 PaLM API 已下线。
- `openai/relay_responses_synthetic_compact.go`：合成流写出 `response.completed` 后结束，
  没有上游可截断，属另一类。

### 10.3 已核验但未由本会话修复的发现

- **`model.User.AccessToken` 的 JSON 序列化（P1，链路已复现）**：HEAD 中该字段是
  `json:"access_token"`，而 `authHelper`（`middleware/auth.go:105`）接受 `Authorization`
  头里的 access token 作为完整会话。管理员调用返回 `model.User` 列表的接口即可拿到任意用户
  （含 root）的 access token，再以该用户身份调用管理接口。工作区已有其他会话的在途修复
  （`json:"-"`，未提交），按「不回退、不整理其他会话改动」的约定，本会话只核验并记录，
  未代为提交。
- **`model.SumUsedToken`（P2，死代码）**：`model/log.go:1138` 使用 `ifnull(...)`，
  PostgreSQL 无此函数（应为 `COALESCE`）；两处调用点（`controller/log.go:134/179`）均被
  注释，当前不可达，记录不改。
- **共享扫描器不聚合多行 `data:`（P2）**：SSE 规范允许一个事件的多行 `data:` 拼接，
  `stream_scanner.go:342` 逐行独立处理。改为聚合会改变所有共享扫描器渠道的帧语义，
  本会话仅核对代码位置，未复现、未改动。
- **桌面 OAuth「内存回退」——不成立**：`controller/oauth_desktop_store.go:82-90` 的
  `currentDesktopOAuthStore` 在 Redis 未启用时使用内存存储，但 Redis 已配置而不可用时
  全部操作返回 `errDesktopOAuthStoreUnavailable`，调用方（`controller/oauth_desktop.go:121/137/162`）
  显式返回 503「状态存储暂不可用」，没有伪造完成态。内存存储是单实例/桌面 sidecar
  （按 AGENTS.md §8 显式清空 `REDIS_CONN_STRING`）的设计路径；多实例误配无 Redis 时，
  跨实例消费表现为显式 state-not-found，也不是静默降级。
- **前端 401 后不导航（未复现，记录）**：`web/default/src/lib/api.ts` 的 401 分支只做
  `auth.reset()` + toast；`_authenticated/route.tsx` 的鉴权在 `beforeLoad`（导航时）执行，
  所以 401 后页面停留到下一次导航才跳登录。属设计取舍而非功能破坏，未改动。

### 10.4 本轮方法教训

- **反向验证要「失败在正确的原因上」**：撤掉 `AdminCompleteTopUp` 护栏的第一次尝试直接
  构建失败（`i18n` 变成未使用导入），改用 `if false &&` 扰动条件后才得到干净的用例级失败。
- **bun 的 `mock.module` 对同进程其他测试文件同样生效**：新测试文件里 mock 掉
  `@/stores/auth-store` 会让 `api-session-expired.test.ts` 的 `setState` 失效；
  修复是根本不 mock 真实 store，而不是把 mock 补全。
- **测试文件间的隐藏耦合**：`api.get` 去重表是模块级状态，一个用例在断言失败提前退出后
  会把 in-flight 表项留给下一个用例；每个用例使用独立 URL 隔离。

