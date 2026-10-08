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
| `87679b0aa` | `check-browser-compatibility.mjs`：dist 目录不存在时改为收集并 exit 1，不再静默通过 | 三个用例按退出码验证：不存在目录 1、真实 dist 0、无参数 1 |
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
- `palm`：`palmStreamHandler(c, resp)` 没有 `RelayInfo` 参数，无法写 `StreamStatus`；
  且 PaLM API 已下线。
- `openai/relay_responses_synthetic_compact.go`：合成流写出 `response.completed` 后结束，
  没有上游可截断，属另一类。

`aws` 与 `xunfei` 已在 §11 修复。

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


## 11. 2026-10-07 自建扫描器渠道收尾：xunfei 与 aws

§10.2 待办清单里的两个渠道已修复，均为 TDD（先 RED、再实现、扰动反向验证）后提交。

### 11.1 修复清单

| 提交 | 内容 | 反向验证 |
| --- | --- | --- |
| `51f7f7a67` | xunfei：WebSocket 读取器把「status==2 终止帧」与「读错误」都走同一条 `stopChan`，消费者无从区分且从不写 `StreamStatus`。改为带类型的 `xunfeiStreamResult{endedNormally}`，消费者经 `helper.RecordStreamEnd` 分类。为让 handler 拿到 `info` 且可用本地 WS 上游测试，`xunfeiStreamHandler` 改收 `*RelayInfo` 与调用方预构建的 domain/authUrl（adaptor 经 `getXunfeiAuthUrl` 构建） | 撤分类：两条用例均 FAIL（`EndReason=""`）；扰动 `endedNormally` 恒真：反向对照 FAIL |
| `6b4486923` | aws：ConverseStream 成功路径已强制要求 `message_stop`（Claude 另有 metadata）却从不写 `StreamStatus`。终止校验与收尾帧提取为 `finishConverseStream`，成功返回前经 `helper.RecordStreamEnd` 记录；能到达该返回点即意味着见过两个终止事件，标记恒真 | 撤分类：两条成功路径用例 FAIL；`aws` 包全绿 |

测试方式：xunfei 用 `httptest` + gorilla `Upgrader` 起本地 WebSocket 上游，驱动真实
读取/消费/分类链路（终止帧收尾 → 正常成功；无终止帧断开 → 中断）；aws 的 handler 需要
具体 `*bedrockruntime.Client`，测试直接驱动提取出的 `finishConverseStream`（OpenAI 与
Claude 两条成功路径 + 缺 `message_stop` 的反向对照）。

### 11.2 提交边界

两个提交都以 HEAD 为基线、只叠加本次改动：工作区里 `relay-xunfei.go` 与其他文件的
`encoding/json -> common.*` 迁移属其他会话的在途改动，未纳入提交（`git show <commit>:<file>`
计数 0 处 `common.Marshal` 对 3 处 `json.Marshal` 可复核）。

### 11.3 剩余同类未覆盖

- `palm`：`palmStreamHandler(c, resp)` 无 `RelayInfo` 参数；PaLM API 已下线，不投入。
- `openai/relay_responses_synthetic_compact.go`：合成流无上游可截断，属另一类。




## 12. 2026-10-07 前端 toast 去重与只读守卫重装

### 12.1 web/default 三层 toast 重叠（已修复 `ee8445ffe`）

`web/default` 有三处独立弹 toast：axios 拦截器、React Query 默认 `mutations.onError`
（`main.tsx` 接的是 `handleServerError`）、以及 `main.tsx` 的 `QueryCache` 401/500 分支。
一次失败的 mutation 因此弹两个文案不同的同因提示（拦截器用后端 message，React Query 用
兜底文案）；304 更是弹三个——`handleServerError` 当时根本不识别 304，而 304 无响应体，
拦截器会把 axios 的原始状态文本直接展示给用户。

修复：拦截器给已提示过的 error 打不可枚举 Symbol 标记；`handleServerError` 跳过已标记的
error，并新增 304 →「Content not modified!」分支（拦截器同步补上该分支）；`QueryCache`
的 401/500 在已标记时不再重复弹，但副作用（重置登录态、跳转）照常执行。

用例驱动真实 api 实例 + 与 `main.tsx` 同构的 QueryClient：失败 mutation 与 304 mutation
各恰好一个 toast；未标记的 error（`skipErrorHandler` 调用方）仍弹一次。
反向验证：关掉标记守卫，恰好两条去重用例失败。

**同一提交内的第二个缺陷（验证过程中发现，属产品缺口）**：只读 fetch 守卫原先用
`fetchGuardInstalled` 布尔量记录「已安装」，任何在守卫安装之后替换 `globalThis.fetch`
的代码都会让守卫永久失效——只读层被静默解除，而 `AGENTS.md` §7 要求它必须生效。
改为记录「当前包装函数」本身，当 `globalThis.fetch` 不等于它时重新安装。
反向验证：把重装条件改成「装过就永不重装」，恰好 Ollama fetch 拦截用例失败。

### 12.2 测试文件的跨文件污染（同提交一并处理）

bun 在同一进程按文件顺序执行测试，模块只求值一次，且 `import.meta.env` 的赋值不跨文件
传播。新测试文件若先加载 `@/lib/api`，会让 `readonly-guard.test.ts` 的 `beforeAll`
读到未设置的开关而失败；反之，`readonly-guard.test.ts` 替换 `globalThis.fetch` 后，
守卫若不再重装，其 Ollama 用例也会失败。两侧修法：新文件在导入前设置与对方一致的开关值；
`readonly-guard.test.ts` 在替换 fetch 后显式调用 `installReadonlyFetchGuard()`。
两种文件顺序下全套 211 通过。

### 12.3 classic 双 toast（已修复 `e9ce7e86a`）

`apiCore.js:140` 的拦截器在 reject 前调 `showError`，调用方 catch 里普遍再调一次。
`showError` 现在给 error 打不可枚举 Symbol 标记，第二次直接返回；不同 error 各弹一次，
冻结对象也能弹唯一那次。classic 全套 95 通过。

### 12.4 classic 英文界面回退中文——成立，但数字修正（未修）

上一轮审计称「951/4040 key」。实测（2026-10-07，对 `zh-CN.json` 与 `en.json` 的
`translation` 键集做差集）：`en` 缺失的中文键只有 2 个；但 1207 个键的英文值是空串，
`returnEmptyString: false`（`i18n.js:51`）会把空值解析回 `fallbackLng: 'zh-CN'`，
所以英文界面确实出现中文——机制成立，计数口径不同。其余语种缺失 184-215 键、
空值 859-1142 键。未改：属翻译补全工作。

### 12.5 POST 返回 304——成立但无故障路径，记录不修

axios 只对 2xx resolve，304 走 reject。修复后 304 有专门文案且只弹一次；但正常页面不发
条件请求（axios 不自动带 `If-None-Match`），没有已知故障路径。

## 13. 2026-10-07 自建扫描器的 64KiB 行上限（第五批）

§9.5 的 SSE 候选清单里有一条「若干渠道自写扫描器的 64KiB 上限」当时只核对了代码位置，
本节把它复现并修掉。

### 13.1 复现（成立，已修 `94c07e8b5`）

五个渠道驱动自己的 `bufio.Scanner` 且都没有调 `Buffer`：`cloudflare`、`tencent`、
`coze`、`zhipu`、`cohere`。裸 `bufio.NewScanner` 的 token 上限是
`bufio.MaxScanTokenSize` = 64KiB（`go doc bufio.MaxScanTokenSize` 实证）。上游单个
SSE 帧超过该值（典型形态是大 tool-call 参数）时 `Scan` 停在 `ErrTooLong`：

- cloudflare / tencent / zhipu / cohere：终止标记（`[DONE]`、尾包、`meta`、
  `is_finished`）永远读不到，健康流被 `RecordStreamEnd` 判成
  `upstream_transport_interrupted`；
- coze：`scanner.Err()` 被当作响应体错误返回，健康流直接变成 500。

对照：共享扫描器（`stream_scanner.go:218`）早已用
`Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())` 抬到 64MiB
并支持 `StreamScannerMaxBufferMB` 覆盖；`ollama`（16MiB）、`codex`（64MiB）也各自设了
上限，只有这五家漏了。

### 13.2 修复

`helper.GetScannerBufferSize()` 导出共享扫描器的同一判据（默认 64MiB、
`StreamScannerMaxBufferMB` 覆盖生效），五个渠道在 `NewScanner` 后调用
`scanner.Buffer(make([]byte, helper.InitialScannerBufferSize), helper.GetScannerBufferSize())`。

### 13.3 验证

每个渠道一个回归用例：70KiB 帧 + 终止标记，断言正常结束且内容完整转发。
修复前五条全 FAIL（四条 `EndReason="upstream_transport_interrupted"`，coze 为
`bad_response_body` 500）；修复后全 PASS。反向验证：把 `GetScannerBufferSize`
扰动回恒返 64KiB，六条用例（含 helper 的 env 覆盖用例）全部再次 FAIL，恢复后全绿。

`relay/channel/{cloudflare,tencent,coze,zhipu,cohere}` 与 `relay/helper` 全包通过；
最小回归集 `go test ./controller ./model ./relay/common ./relay/helper ./service` 通过。
干净树验证：`git archive HEAD` + 本提交的 12 个文件，`go test` 六包全绿。

### 13.4 提交边界

五个渠道文件与 `stream_scanner.go` 都带其他会话的在途改动（`encoding/json` →
`common.*` 迁移、ping 写入重构），提交以 HEAD 为基线、只叠加本次的 3 行/渠道与
导出函数；`git show 94c07e8b5 --stat` 可复核为纯新增。

## 14. 2026-10-07 govulncheck 首次跑通：GO-2026-6278 与 v1.5.3 假修复陷阱

### 14.1 门禁首次跑通

此前两轮审计（09-23、10-05）的 `govulncheck` 均因超时或工具链不匹配没有产出。
本机 govulncheck 是用 go1.26 构建的，而 PATH 上是 go1.27.1，包加载直接报
`method must have no type parameters`（stdlib 解析失败）。`go install
golang.org/x/vuln/cmd/govulncheck@latest` 重建后一次跑通。

### 14.2 发现：GO-2026-6278 在本仓代码可达（已修 `4c0a1e904`）

`gorilla/websocket@v1.5.0` 的客户端帧掩码密钥用 `math/rand` 生成。
govulncheck 给出 11 条本仓调用链，覆盖 `controller/relay.go` 的
`Upgrader.Upgrade`、`relay/channel/xunfei` 与 `volcengine` 的 `WriteJSON`/
`WriteMessage`、`relay/channel/api_request.go` 的 `Dialer.DialContext` 等。

### 14.3 陷阱：公告标注的"已修复版本" v1.5.3 实际不含修复

OSV 条目 `GO-2026-6278` 标注 `fixed: 1.5.3`。但 v1.5.3 的 tag 提交是
`ce903f6 "Reverts to v1.5.0"`——把 crypto/rand 修复连同其它改动一起回退了，
`newMaskKey` 又变回 `math/rand.Uint32()`。三个独立来源一致：

```console
$ curl -s https://raw.githubusercontent.com/gorilla/websocket/v1.5.3/conn.go | grep -A3 "func newMaskKey"
	n := rand.Uint32()          # math/rand
$ grep -A3 "func newMaskKey" $(go env GOMODCACHE)/github.com/gorilla/websocket@v1.5.3/conn.go
	n := rand.Uint32()          # 模块缓存（go.sum 已校验）
$ 发布说明： "Important change: This reverts the websockets package back to 931041c"
```

实测两个方向都反直觉：

- 升到 v1.5.3（公告说已修复）：`govulncheck` 报 0 可达漏洞，但模块缓存里
  `newMaskKey` 仍是 `math/rand`——**扫描器绿、代码仍弱**；
- 降回 v1.5.2（代码实际含 crypto/rand 修复）：`govulncheck` 仍报
  `Your code is affected by 1 vulnerability`——**代码已修、扫描器红**。

即 OSV 的 SEMVER 区间（introduced 0 / fixed 1.5.3）与真实内容不一致：
v1.5.1 就含修复，v1.5.3 把它回退了，真正的修复提交 `d67f418`
（"Use crypto/rand for mask key"，committed 2024-07-01，晚于 v1.5.3 的
2024-06-14）只存在于 main，`git tag --contains d67f418` 为空。

### 14.4 处理

go.mod 钉到修复提交本身：`v1.5.4-0.20240701034025-d67f41855da4`
（伪版本，sum.golang.org 校验）。验证：

- 解析后的模块源码：`var maskRand = rand.Reader`（crypto/rand），
  `io.ReadFull(maskRand, k[:])`；
- `govulncheck ./...`：0 可达漏洞；
- `go build ./...`、`go test ./controller ./model ./relay/... ./service
  ./middleware`：42 包全过；`go mod verify` 全过。

边界：该伪版本比 v1.5.3 新 14 个提交（含 subprotocol 选择修复、
x/net proxy 解绑、Go 最低版本 1.20 等），均来自上游 main；未重建任何容器，
正式与隔离环境仍运行旧二进制。

### 14.5 残余（未修，非可达）

`govulncheck -show verbose` 另列 7 条"包级导入但本仓未调用"的
`golang.org/x/net@v0.47.0` 漏洞（html/idna/http2/dns 解析类）与 19 条
模块级漏洞（`x/crypto`、`klauspost/compress` 等），工具判定本仓代码不可达。
其中 `GO-2026-5932`（x/crypto）标注 `Fixed in: N/A`。未升级：属独立议题，
升级 x/net/x/crypto 会牵动 gin 等依赖树，需要单独验证。

## 15. 2026-10-07 构建工具链 stdlib 漏洞：生产 Dockerfile 钉的 go1.26.2（第六批）

### 15.1 发现方式

§14 的扫描用的是本机 PATH 上的 go1.27.1，看不到旧工具链的问题。用
`GOTOOLCHAIN` 切到生产 Dockerfile 实际钉的版本复扫：

```console
$ GOTOOLCHAIN=go1.26.2 govulncheck ./...
Your code is affected by 11 vulnerabilities from the Go standard library.
```

11 条全部 `Found in: ...@go1.26.2`，即漏洞随工具链进入产物，与依赖模块无关。
本地 go1.27.1 下同样的命令报 0 条，说明这不是代码问题而是构建链钉版问题。

### 15.2 条目与修复版本

| GO 编号 | 包 | fixed（1.26 线） |
| --- | --- | --- |
| GO-2026-6218 | net/url（解析二次复杂度） | go1.26.6 |
| GO-2026-6090 | crypto/tls | go1.26.6 |
| GO-2026-6089 | net/http | go1.26.6 |
| GO-2026-6088 | encoding/xml（递归栈溢出） | go1.26.6 |
| GO-2026-5972 | encoding/asn1 | go1.26.6 |
| GO-2026-5856 | crypto/tls | go1.26.5 |
| GO-2026-5039 | net/textproto | go1.26.4 |
| GO-2026-5037 | crypto/x509 | go1.26.4 |
| GO-2026-5026 | x/net/idna 经 net/http | go1.26.6（x/net v0.55.0） |
| GO-2026-4971 | net（Windows NUL 注入） | go1.26.3 |
| GO-2026-4918 | HTTP/2 无限循环 经 net/http | go1.26.3（x/net v0.53.0） |

修复版本上界 go1.26.6。go1.26.8 是该线最新补丁版（`go.dev/dl` 实测），覆盖全部。

### 15.3 处理（本轮改动）

- `Dockerfile:50`（builder2）：
  `golang:1.26.2-alpine@sha256:c2a1f7b2...` →
  `golang:1.26.8-alpine@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c`。
  新 digest 取自 `docker buildx imagetools inspect golang:1.26.8-alpine`，
  并经 registry API 解析确认为 index（linux/amd64 + arm 变体齐全）。
- `Dockerfile.dev:4`：`golang:1.26.1-alpine` → `golang:1.26.8-alpine`
  （该文件原有风格未钉 digest，保持原样）。
- `.github/workflows/tauri-build.yml`、`tauri-release.yml`：
  `go-version: '1.25.1'` → `'1.25.14'`（桌面端 sidecar 构建线，1.25 最新补丁）。
- `.github/workflows/release.yml` 三处：`>=1.25.1` → `>=1.25.14`
  （原值解析到最新版，本就安全；显式抬高下限以固定语义）。

全仓 `Dockerfile*` 排查：仓库自有的 golang 基础镜像只有上述两处
（`desktop/tauri-app/.cache/gomod/` 下的是第三方模块自带的 Dockerfile，非本仓构建入口）。

### 15.4 验证

```console
$ GOTOOLCHAIN=go1.26.2 govulncheck ./...   # 改前基线，RED
Your code is affected by 11 vulnerabilities from the Go standard library.  (exit 3)
$ GOTOOLCHAIN=go1.26.8 govulncheck ./...   # 改后
No vulnerabilities found. Your code is affected by 0 vulnerabilities.     (exit 0)
$ GOTOOLCHAIN=go1.25.14 govulncheck ./...  # tauri 线
Your code is affected by 0 vulnerabilities.                               (exit 0)
$ GOTOOLCHAIN=go1.26.8 go build ./...      # 全仓库编译
EXIT=0
$ docker run --rm golang:1.26.8-alpine@sha256:8ac98ca5... go version
go version go1.26.8 linux/amd64
```

镜像实测同时验证了 digest 与 tag 一致（pull 时 Docker 回报同一 digest）。
三个 workflow 文件经 `yaml.safe_load` 语法校验通过。

### 15.5 边界与残余

- 未重建任何镜像或容器。正式与隔离环境仍运行旧二进制；本改动只影响后续构建。
- 7 条 package-level / 19 条 module-level 残余（§14.5）在本节时点不变；
  2026-10-07 晚间已被 §16 清到 0 / 4（x/net、compress 升级）。
- `release.yml` 的 `>=1.25.14` 下限不改变当前解析结果（仍取最新稳定版），
  只是让"必须含 stdlib 修复"成为显式约束。

## 16. 2026-10-07 依赖层收尾：Go 包级残留与前端 bun audit（第七批）

### 16.1 触发的维度

§15 只扫了工具链自带的 stdlib。本节补两个此前从未覆盖的维度：Go 依赖的
package/module 级残留，以及两个前端的 `bun audit`（前端依赖从未审计过）。

### 16.2 Go 侧：x/net 与 compress（已修 `54b81803a`）

`govulncheck -show verbose` 的 package 级 7 条全部落在
`golang.org/x/net@v0.47.0`（html 解析 XSS/DoS 5 条、idna Punycode 1 条、
HTTP/2 SETTINGS 死循环 1 条），module 级另有 `klauspost/compress@v1.18.0`
的 s2 越界读（GO-2026-5841）。

```console
$ GOTOOLCHAIN=auto go get golang.org/x/net@v0.57.0 github.com/klauspost/compress@v1.18.7
go: upgraded github.com/klauspost/compress v1.18.0 => v1.18.7
go: upgraded golang.org/x/crypto v0.45.0 => v0.54.0
go: upgraded golang.org/x/net v0.47.0 => v0.57.0
```

`go 1.25.1` 指令未被顶高（先在与仓库无关的 `/tmp` 模块副本里试跑过才动真
go.mod）。这是关键约束：只有 `x/crypto@v0.56.0` 会把 go 指令顶到 1.26.0，
而 tauri 工作流钉的是 1.25.14。x/net 只要求 go 1.25.0。

结果：package 级 7 → 0，module 级 19 → 4。剩余 4 条全在 `x/crypto@v0.54.0`
（GO-2026-6355 / 6354 / 6303 在 ssh，GO-2026-5932 openpgp 标注 `Fixed in: N/A`），
工具判定本仓不可达（本仓只用 bcrypt），且需 `x/crypto >= v0.55.0` 才能清，
即必须先接受 go 1.26.0 —— 与 tauri 钉版冲突，本轮不做。

### 16.3 前端：两个 bun audit（已修 `301eb1eb8`）

基线（改动前）：

| 前端 | 条数 | 直接依赖中的高危 |
| --- | --- | --- |
| `web/default` | 47（2 critical / 17 high / 23 moderate / 5 low） | `axios@1.19.0` 11 条 |
| `web/classic` | 27（12 high / 11 moderate / 4 low） | `axios@1.18.0` |

`seroval@1.5.3` 是 critical 且经 `@tanstack/router-core` 进主 bundle
（`fromJSON` 的 Promise thenable 同化会调用插件产出的可调用对象）。

`bun audit fix`（只升到满足所有依赖者范围的最低安全版本，不跨 major）
一次清掉 default 38 条 / 14 包、classic 22 条 / 8 包，含：

- `axios` 1.19.0 与 1.18.0 → 1.20.0（classic 是精确钉版，bun 自动改了
  package.json；advisory 声明 `<1.20.0` 全受影响）；
- `seroval` 1.5.3 → 1.6.3（`@tanstack/router-core@1.168.15` 声明 `^1.5.0`，
  1.6.3 在范围内且同时越过两条 advisory 的修复线 1.6.0 / 1.6.2）；
- `dompurify` 3.4.13 → 3.4.16、`js-yaml` 4.3.1 → 4.3.2、`colord` 2.9.4、
  `smol-toml` 1.9.0、`hono` 4.13.7、`qs` 6.16.0、`proxy-addr` 2.0.8、
  `@modelcontextprotocol/sdk` 1.31.0、`brace-expansion` 5.0.12 等。

剩余项被"精确钉版"或"依赖者窄范围"挡住，用 `overrides` 收口：

- default：`fast-uri` 3.1.7 → 3.1.8、`ip-address` 10.3.1 → 10.7.3
  （**这两个 override 原先就钉在受影响版本，本身在阻止修复**）、
  新增 `decode-uri-component` 0.5.0 与 `@ai-sdk/provider-utils` 4.0.33
  （`ai` / `@ai-sdk/gateway` 精确依赖 4.0.23）；
- classic：新增 `decode-uri-component` 0.5.0 与
  `postcss-selector-parser` `^6.1.3`。

结果：default 47 → 2，classic 27 → 3。

### 16.4 无法修复并记录在案的残余

| 包 | 范围 | 原因 |
| --- | --- | --- |
| `braces@3.0.3` | 两个前端，high | 上游无已发布修复版本 |
| `katex` | 两个前端，low | 修复 0.18.2 落在所有依赖者的 `^0.16` 范围之外 |
| `postcss-selector-parser@6.1.4` | classic，moderate | `<7.1.6` 的 moderate 在 6.x 无修复，7.x 超出 tailwindcss 3 范围 |

均为 dev/build 期或低危；`braces` 仅由 fast-glob/micromatch/chokidar 等
构建期工具链使用，不进运行时产物。

### 16.5 验证

```console
$ govulncheck ./...                       # 改后
Your code is affected by 0 vulnerabilities.
This scan also found 0 vulnerabilities in packages you import and 4 in modules.
$ go build ./... && go test ./controller ./model ./relay/common ./relay/helper ./service
BUILD=0  TEST=0（5 包全 ok）  go mod verify: all modules verified

$ cd web/default
$ bun run lint          # eslint 通过
$ bun run typecheck     # tsc -b 通过
$ bun test tests/       # 211 pass / 0 fail
$ bun run build         # 产物 19099.2 kB，Safari 兼容检查通过
$ cd web/classic
$ bun run lint          # prettier --check 通过
$ bun test              # 218 pass / 0 fail
$ bun run build         # built in 2m22s，Safari 兼容检查通过
```

真实浏览器冒烟（`web/default` 经只读代理 3300、`web/classic` 直连 5177，
均按 `VITE_REACT_APP_READONLY_MODE=true` / 只读代理双重阻断）：两端首页
标题、导航、hero 均正常渲染，router 与主 bundle 加载无错。

### 16.6 边界

- 未重建镜像或容器；前端 dist 未提交，正式与隔离环境仍运行旧产物。
- 只读代理临时容器与 `deploy/env/frontend-readonly.env` 已在验证后清理
  （该 env 文件 gitignore，创建自 example；注意 example 里
  `READONLY_PROXY_TEMPLATE_PATH` 为空，启动前必须填绝对路径，属已知保真度缺口）。
- `@ai-sdk/provider-utils` 的 override 是跨 minor 强升（4.0.23 → 4.0.33），
  依赖方 `ai` / `@ai-sdk/gateway` 声明的是精确版本；构建与测试通过，
  但未做该 SDK 的运行时功能验证。

## 17. 2026-10-07 依赖与 CI 供应链收尾：Rust 与 action 钉版（第八批）

### 17.1 触发的维度

§16 覆盖了 Go 与两个前端。本节补最后两个未审计维度：桌面端 Tauri 的
Rust 依赖（`cargo-audit`），以及 GitHub Actions 的版本钉法一致性。

### 17.2 桌面端 Rust 依赖（已修 `029fa4f65`）

`cargo install cargo-audit --locked` 后对 `desktop/tauri-app/src-tauri/Cargo.lock`
（558 个 crate）审计，报 3 条：

| crate | 版本 | 严重度 | 修复版本 | 依赖链 |
| --- | --- | --- | --- | --- |
| quick-xml | 0.38.4 | high | >=0.41.0 | `plist -> tauri`（macOS bundle 构建） |
| quick-xml | 0.38.4 | high | >=0.41.0 | 同上（另一条 advisory） |
| rkyv | 0.7.46 | - | >=0.8.17 | `byte-unit -> rust_decimal`（optional feature） |

两条 quick-xml 分别是 NsReader 无界命名空间分配（内存耗尽 DoS）与
起始标签重复属性名的二次复杂度。处置：

```console
$ cargo update -p plist
      Updating plist v1.8.0 -> v1.10.1
      Updating quick-xml v0.38.4 -> v0.42.0
$ cargo update -p rust_decimal --precise 1.43.0
    Removing ahash / bitvec / bytecheck / bytecheck_derive / funty / ptr_meta ...
```

关键判断：`plist 1.8.0` 声明的是 `version = "0.38.0"`（caret 范围，允许
0.42.0），所以这是 lock 陈旧，只需更新 lockfile，不动 Cargo.toml。
`rkyv` 是 `rust_decimal` 的 optional feature 且无人启用（`cargo tree -i rkyv`
报 "nothing to print"），升 rust_decimal 到 1.43.0 即连带移除 rkyv 及
bitvec/bytecheck/funty/ptr_meta/ahash。

复扫 `cargo-audit`：**exit 0，0 漏洞**；剩 11 条 unmaintained/unsound 警告
（fxhash、proc-macro-error、unic-*、glib、rand、anyhow、event-listener），
全为传递依赖且无修复版本。`cargo check` 通过。

### 17.3 GitHub Actions 供应链钉版（已修 `6ed820493`）

审计发现 8 个工作流的钉法不一致：

| 工作流 | 改前 | 改后 |
| --- | --- | --- |
| docker-build / docker-image-alpha / release | 已 SHA 钉（9-12 处） | 不变 |
| docker-image-nightly / pr-check / tauri-build / tauri-release | 浮动 tag（共 21 处） | 全部 SHA 钉 |

浮动 tag（`@v4`、`@v6`、`@v0.6.2` 等）可被强制移动到不同提交，构建不可复现。
21 处逐一改为其 tag 当前指向的 commit SHA，并保留 `# vX` 尾注以便阅读。

解析方法（本沙箱 `api.github.com` 返回 403，`git ls-remote` 可用）：

```console
$ git ls-remote https://github.com/actions/checkout.git refs/tags/v4
11d5960a326750d5838078e36cf38b85af677262  refs/tags/v4
```

交叉验证：`oven-sh/setup-bun@v2` 解析出的 SHA 与仓库既有钉版
（`0c5077e5...`）完全一致，证明解析方法可靠。

唯一保留浮动的是 `dtolnay/rust-toolchain@stable`：其 README 说明 SHA 钉版
只适用于 master 历史内的提交，且 toolchain 选择默认"匹配 @rev"——该 action
的 @rev 本身就是 toolchain 规格，改为 SHA 会破坏语义。

### 17.4 边界

- 未重建镜像、容器或桌面端产物；改动只影响后续构建。
- 桌面端 `bun audit`（12 包）0 漏洞，无需改动。
- cargo-audit 的 11 条 unmaintained 警告未处理：无修复版本，且多为
  tauri/glib 等上游传递依赖，需等上游升级。
- action 钉版未在真实 CI 运行验证（本地无 CI 运行环境）；SHA 由 `git ls-remote`
  解析并做了一致性交叉验证，但工作流本身未触发。

## 18. 2026-10-08 运行时镜像底座：alpine 3.22.2 → 3.22.6（第九批）

### 18.1 触发与取证

§15 处理了构建工具链（golang builder），本轮轮到运行时底座。生产
Dockerfile 运行时阶段钉的是 `alpine:3.22.2@sha256:4bcff639...`（2026-04
构建），而 3.22 线已发布到 3.22.6（2026-09-17）。用 docker 实测两版镜像
内包版本：

```console
$ docker run --rm alpine:3.22.2 sh -c "apk info -v | grep -E '^(musl|busybox|zlib|libcrypto3|libssl3)-'"
musl-1.2.5-r10   busybox-1.37.0-r19   zlib-1.3.1-r2
libcrypto3-3.5.4-r0   libssl3-3.5.4-r0
$ docker run --rm alpine:3.22.6 sh -c "apk info -v | grep ..."
musl-1.2.5-r12   busybox-1.37.0-r20   zlib-1.3.2-r0
libcrypto3-3.5.8-r0   libssl3-3.5.8-r0
```

对照 Alpine secdb（v3.22/main.json），3.22.2 携带的是以下 CVE 的**修复前**
版本：musl（CVE-2026-6042、CVE-2026-40200）、busybox（CVE-2025-46394、
CVE-2024-58251）、zlib（CVE-2026-22184、CVE-2026-27171）、openssl
（3.5.5–3.5.8 各批共约 40 条，含 CVE-2026-14456 等）。

### 18.2 处理（`ec5c724eb`）

- `Dockerfile` 运行时 FROM：`alpine:3.22.2@sha256:4bcff639...` →
  `alpine:3.22.6@sha256:5291449c...`（index digest，buildx imagetools
  取得并经 registry API 复核）；
- 恢复 `apk upgrade --no-cache`（在 `apk add` 之前）。原因：3.22.6 镜像
  自身仍落后于仓库 —— 实测 3.22.6 内 zlib 是 1.3.2-r0、openssl 3.5.8，
  而 3.22 仓库已有 zlib 1.3.2-r1（CVE-2026-85091，`gz_vacate` 堆溢出）
  和 openssl 3.5.9。只有 upgrade 能拿到这批。该行在最初的 Dockerfile 里
  就有（`4cbef0780`），切 debian 时丢失，切回 alpine 时（`95fdc63d7`）
  没有恢复。

可达性说明：容器只运行静态 Go 二进制（CGO_ENABLED=0），zlib/openssl 的
使用方只有 apk 与 busybox ssl_client，healthcheck 的 wget 走明文 http。
所以这批修复是纵深防御 + 消除镜像扫描噪音，不是运行时可达漏洞。

### 18.3 验证

```console
$ docker build --check -f Dockerfile .
Check complete, no warnings found.
$ docker build -t apitest /tmp/apitest   # 同 digest + 同 RUN 序列的抛掷构建
apk upgrade + apk add + update-ca-certificates 全过，
zlib-1.3.2-r1  libcrypto3-3.5.9-r0  musl-1.2.5-r12  busybox-1.37.0-r20
$ sh scripts/build-docker-local.sh new-api-local:verify-alpine326
built image=new-api-local:verify-alpine326 version=v1.1.0 commit=ec5c724eb-dirty
```

全量镜像实测（`docker run --entrypoint sh`）：

```console
$ cat /etc/alpine-release          # 3.22.6
$ apk info -v | grep -E "^(zlib|libcrypto3|libssl3|musl|busybox)-"
busybox-1.37.0-r20  libcrypto3-3.5.9-r0  libssl3-3.5.9-r0
musl-1.2.5-r12  zlib-1.3.2-r1
$ /new-api --build-info            # version/commit/date/source 正常
```

首次构建曾因 module proxy 下载 `klauspost/compress@v1.18.7` 时 EOF 失败
（`storage.googleapis.com` 瞬时网络故障）；本地 `go mod download` 复核通过后
重试即成功，非 Dockerfile 问题。

### 18.4 边界

- 全量构建用抛掷 tag `new-api-local:verify-alpine326`，未触碰
  `new-api-local:dev`（隔离环境镜像）与任何运行中容器；构建完成后抛掷镜像
  已删除。
- 前端产物来自当前 dirty 工作区（含其他会话在途改动），构建成功只证明
  Dockerfile 可构建，不代表这些前端改动已验收。
- 生产与隔离环境仍运行旧镜像；本次只影响后续构建。

## 19. 2026-10-08 systemd 单元模板的行内注释（第十批）

### 19.1 发现

`new-api.service`（systemd 单元模板）把三处"注意修改"提示写成了行内注释：

```ini
User=ubuntu  # 注意修改用户名
WorkingDirectory=/path/to/new-api  # 注意修改路径
ExecStart=/path/to/new-api/new-api --port 3000 --log-dir /path/to/new-api/logs  # 注意修改路径和端口号
```

systemd 不把行尾 `#` 当注释。`systemd.syntax(7)` 只说明"空行与以
`#`/`;` 开头的行被忽略"，未定义行内注释；实测（`systemd-analyze verify`，
容器内离线解析）：

```console
$ systemd-analyze verify /tmp/x.service
Accepting user/group name 'ubuntu  # 注意修改用户名', which does not match
strict user/group name rules.
```

即 `User` 的值被解析成含注释文本的 `ubuntu  # 注意修改用户名`。照抄模板的
用户会得到无法启动的单元（用户不存在）。上游 `QuantumNous/new-api` 同样
如此，本仓原样继承。

### 19.2 处理（`f80e7972b`）

把三处提示移到指令上方的独立注释行。反向验证：

```console
$ systemd-analyze verify orig.service    # HEAD 版本
Accepting user/group name 'ubuntu  # 注意修改用户名', ...   # 警告
$ systemd-analyze verify fixed.service
# 无 User 警告（仅剩 "Command /path/to/new-api/new-api is not executable"，
#  那是模板占位符的预期结果）
```

### 19.3 同类排查（无发现）

- 仓库仅此一个 `.service`/`.timer`/`.socket` 文件，无同类问题；
- 29 个 `scripts/*.sh` 全部通过 `bash -n`；
- 文档引用的 21 个 `scripts/*.sh`、3 个 `deploy/compose/*.yml` 全部存在；
- `deploy/env/*.env` 的两处"缺失"（`storage-postgres.env`、
  `frontend-readonly.env`）是 gitignored 的本地文件，由 `.example` 复制生成，
  非坏引用；
- `deploy/env/storage-postgres.example.env` 的命名（`X.example.env`）与其余
  6 个（`X.env.example`）相反，但 `.gitignore:50` 已显式加
  `!deploy/env/*.example.env` 例外兼容，属既定选择，不改。

### 19.4 顺带验证（无发现）

- `go test -shuffle=on -count=2` 七个包全过（0 FAIL），无跨运行状态泄漏；
- `go vet ./...` 全仓 0 问题；
- 桌面端 `cargo test --test runtime_support` 22 项全过；
- §17 新钉的 4 个 action SHA 经 `git ls-remote` 复核，与对应 tag 一致。

## 20. 2026-10-08 桌面端与 CI 门禁复核（第十一批，无改动）

本轮针对此前未覆盖的桌面端维度做只读复核，结论全部为"已验证、无需改动"。
记录在此以免未来重复排查。

### 20.1 桌面端 CSP 的 `'unsafe-eval'` 是必需的

`desktop/tauri-app/src-tauri/tauri.conf.json` 的 CSP 含
`script-src 'self' 'unsafe-inline' 'unsafe-eval'`。该 CSP 是
`f56ab6fc8`（CodeRabbit 建议）从 `null` 收紧而来的。

初看 `'unsafe-eval'` 像是可以收紧的过度授权，但桌面端打包的是
`web/classic`（见 `desktop/tauri-app/scripts/lib/project.mjs` 的
`webDir = web/classic`），实测其生产产物有 3 处 `new Function`：

| 产物 | 位置 | 用途 |
| --- | --- | --- |
| `index-*.js` | `cost:new Function(...Object.keys(r), ...)` | 计费表达式求值 |
| `visactor-*.js` | `new Function("d","return {...}")` | 图表库表达式 |
| `semi-ui-*.js` | `new Function(String(r))(e)` | Semi Design 表达式渲染 |

即 `'unsafe-eval'` 是功能必需，不能移除。`'unsafe-inline'` 同理（内联样式
与 Semi/Rsbuild 的运行时注入）。CSP 保持原样。

### 20.2 桌面端 CI 门禁本地复跑（全过）

`tauri-build.yml` 的步骤在本地逐一执行：

```console
$ cd desktop/tauri-app && bun run check:version
desktop version files are in sync: 1.1.0
$ bun run check:rust
Finished `dev` profile [unoptimized + debuginfo] target(s) in 41.66s
$ bun run test:rust          # 22 passed
$ cd web/classic && bun run test:node
ℹ tests 137  ℹ pass 137  ℹ fail 0
```

### 20.3 Tauri 权限面（最小化，无需改动）

`src-tauri/capabilities/default.json` 只授 `core:default`，未放开
fs/shell/dialog 的 webview 侧权限。Rust 侧虽然 `plugin()` 注册了
`shell`/`dialog`/`log`/`single_instance`，但前端源码（`web/default/src`、
`web/classic/src`）无任何 `@tauri-apps/plugin-*` 调用或 `invoke(` ——插件只由
Rust 侧使用，权限面与使用面一致。

### 20.4 未覆盖的边界

- 桌面端 CSP 的实际运行行为未做真机验证（需构建并启动 Tauri 应用）；
  本节的结论只来自产物静态分析与 CI 门禁复跑。
- `bun run build`（Tauri bundle）未执行：它需要 `@tauri-apps/cli` 与平台
  原生依赖，属长时构建，本轮以 `check:rust` + `test:rust` 代替。

## 21. 2026-10-08 classic 死翻译文件清理与 i18n 支持盘点（第十二批）

### 21.1 删除 `web/classic/src/i18n/locales/zh.json`

该文件是 classic 前端的死翻译文件，四条证据一致：

- `web/classic/src/i18n/i18n.js` 只 import `zh-CN.json` 与 `zh-TW.json`，
  `resources` 里没有 `zh` 键；
- 全仓检索无任何 `locales/zh.json` 引用；
- 提取配置 `web/classic/i18next.config.js` 的
  `locales: ['zh-CN','zh-TW','en','fr','ru','ja','vi']` 不含 `zh`，官方
  工具链既不会生成也不会更新它；
- 它用的是旧结构（`{"translation": {...}}`），2814 键，比 `zh-CN.json`
  少 2303 键。

更值得注意的是它仍在被写：最近五个提交分别给它追加了 +84、+3、+1、+11、
+2 行，但那些翻译永远不会渲染。删除以消除该维护陷阱。

验证：删除后无残留引用；`bun run build` 通过（含 Safari 兼容检查）；
`bun test` 218 pass / 0 fail；`prettier --check` 通过。

提交时发生并发事件：本仓有其他会话在途工作，共享暂存区。该删除先被
`git rm` 暂存，随后另一会话的提交 `67d2fa47d` 把它一并带入（该提交主体是
relaykit 文档改动）。文件已正确删除，未重写他人提交。

### 21.2 两个前端的 i18n 支持现状（盘点，未改动）

| 前端 | 语言 | 资源加载 | 提取配置 |
| --- | --- | --- | --- |
| classic | zh-CN, zh-TW, en, fr, ru, ja, vi | `i18n.js` 显式 import 7 个 json | `i18next.config.js` 的 7 项一致 |
| default | en, fr, ja, ru, vi, zh | `config.ts` 显式 import 6 个 json | `sync-i18n.mjs` |

注意两个前端的语言代码体系不同：classic 用 `zh-CN`/`zh-TW` 拆分，
default 只用 `zh`。删除 classic 的 `zh.json` 不影响 default 的
`web/default/src/i18n/locales/zh.json`（后者是正常在用的）。

后端另有独立体系 `i18n/locales/*.yaml`（en / zh-CN / zh-TW），与前端无关。

### 21.3 classic 的空值分布（既有状态，未改动）

| locale | 键数 | 空值 |
| --- | --- | --- |
| en | 5219 | 1268 |
| fr | 5057 | 914 |
| ja | 4982 | 861 |
| ru | 5108 | 943 |
| vi | 5523 | 862 |
| zh-CN | 5117 | 0 |
| zh-TW | 4955 | 1142 |

这不是回归：HEAD 版本的 `en.json` 空值数为 1272（当前工作区 1268，仍在
被其他会话修改）。空值性质分两类：

- 1246/1268 的键本身是**中文原文**（classic 采用 key-as-source 模式，
  中文即源文案，英文界面才需要填值），属设计使然；
- 22 个是纯 ASCII 键（如 `Cached tokens`、`OAuth Client ID`、`Request ID`），
  是英文界面的真实缺口：这些键在 `en.json` 为空，而 `zh-CN` 有 17/22、
  `fr`/`ja` 各 11/22 有值。

`en.json` 等 7 个 locale 文件当前均被其他会话在途修改（正在补翻译），
本轮不介入。22 个 ASCII 缺口的修复留给该会话。

### 21.4 i18n 门禁

`node web/scripts/i18nLintGate.mjs` 通过：无超出基线（307 条）的新硬编码
字符串。`web/default/src/i18n/locales/_reports/_sync-report.json` 显示
default 六个 locale 的 missing / extras / untranslated 均为 0。
