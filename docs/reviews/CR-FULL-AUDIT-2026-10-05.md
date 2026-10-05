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
