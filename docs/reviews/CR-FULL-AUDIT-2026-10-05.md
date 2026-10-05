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

