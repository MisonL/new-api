# main 分支支付安全复核（2026-04-19）

## Summary

本轮围绕充值与订阅支付链路做了同构安全审计，重点检查以下风险：

- 伪造 webhook / notify
- 空 secret 或弱配置导致验签绕过
- 只按 `trade_no` 收口导致跨渠道串单
- 非本渠道回调修改订单状态
- 已修复漏洞面缺少回归测试，后续被回退

结论：

- 当前可达支付回调主链已经收口
- 旧的无支付方式约束订阅收口接口已移除
- 支付链路已补最小回归测试与包级验证

## Control Contract

- Primary Setpoint
  - 所有支付回调只能修改本渠道创建的订单，不能只凭 `trade_no` 完成或过期其他渠道订单
- Acceptance
  - Stripe / Creem / Epay / Waffo 对应入口均走带支付方式校验的收口路径
  - 空 webhook secret 不再形成 fail-open
  - `go test ./model -count=1` 通过
  - `go test ./controller -count=1` 通过
- Guardrails
  - 不改变正常已支付订单的幂等行为
  - 不改变 Epay 家族支付方式的兼容语义
  - 不引入 schema 变更
- Boundary
  - `controller/topup_*.go`
  - `controller/subscription_payment_*.go`
  - `model/subscription*.go`
  - `model/topup*.go`

## Findings And Fixes

### 1. Stripe 主链已具备关键防线

- 空 `StripeWebhookSecret` 直接拒绝
- webhook 强制官方验签
- 充值入账前校验 `TopUp.PaymentMethod == "stripe"`

位置：

- [controller/topup_stripe.go](/Volumes/Work/code/new-api/controller/topup_stripe.go)
- [model/topup.go](/Volumes/Work/code/new-api/model/topup.go)

### 2. 订阅支付存在同构串单风险，已收口

问题本质：

- 原先订阅订单完成 / 过期只按 `trade_no` 查单
- 若不同支付渠道共享相同订单号语义，回调处理链可能错误收口到其他渠道订单

本轮修复：

- 新增支付渠道匹配规则
- 新增带支付方式约束的订阅订单完成 / 过期入口
- 所有可达订阅支付控制器切换到新入口
- 删除旧的无约束导出接口，避免后续误用

位置：

- [model/payment_method_guard.go](/Volumes/Work/code/new-api/model/payment_method_guard.go)
- [model/subscription_payment_guard.go](/Volumes/Work/code/new-api/model/subscription_payment_guard.go)
- [controller/topup_stripe.go](/Volumes/Work/code/new-api/controller/topup_stripe.go)
- [controller/topup_creem.go](/Volumes/Work/code/new-api/controller/topup_creem.go)
- [controller/subscription_payment_epay.go](/Volumes/Work/code/new-api/controller/subscription_payment_epay.go)

### 3. Waffo 失败回调存在跨渠道状态污染风险，已收口

问题本质：

- 非成功状态时，旧逻辑会按 `trade_no` 查到任意待支付充值单并标记 `failed`
- 这会把其他支付渠道的待支付订单错误改状态

本轮修复：

- 新增带支付方式约束的失败标记 helper
- Waffo 失败回调仅允许修改 `PaymentMethod == "waffo"` 的订单

位置：

- [model/topup_payment_guard.go](/Volumes/Work/code/new-api/model/topup_payment_guard.go)
- [controller/topup_waffo.go](/Volumes/Work/code/new-api/controller/topup_waffo.go)

### 4. Stripe 过期回调存在跨渠道状态污染风险，已收口

问题本质：

- 旧逻辑在订阅单过期未命中后，会继续按 `trade_no` 查充值单并标记 `expired`
- 若订单号被其他渠道复用，可能污染非 Stripe 充值单状态

本轮修复：

- Stripe 充值过期改为走带支付方式约束的 helper

位置：

- [controller/topup_stripe.go](/Volumes/Work/code/new-api/controller/topup_stripe.go)
- [model/topup_payment_guard.go](/Volumes/Work/code/new-api/model/topup_payment_guard.go)

### 5. Creem 存在空 secret fail-open 风险，已收口

问题本质：

- 旧逻辑在 `CreemTestMode=true` 且 `CreemWebhookSecret=""` 时允许验签跳过
- 这会形成和 Stripe 空 secret 同构的可伪造回调风险

本轮修复：

- `verifyCreemSignature` 空 secret 一律返回失败
- 发起 Creem 支付前也要求 `CreemWebhookSecret` 已配置
- 订阅 Creem 支付入口同步要求 webhook secret 已配置

位置：

- [controller/topup_creem.go](/Volumes/Work/code/new-api/controller/topup_creem.go)
- [controller/subscription_payment_creem.go](/Volumes/Work/code/new-api/controller/subscription_payment_creem.go)

## Added Tests

- [controller/topup_stripe_security_test.go](/Volumes/Work/code/new-api/controller/topup_stripe_security_test.go)
- [controller/topup_creem_security_test.go](/Volumes/Work/code/new-api/controller/topup_creem_security_test.go)
- [model/topup_payment_security_test.go](/Volumes/Work/code/new-api/model/topup_payment_security_test.go)
- [model/topup_payment_guard_test.go](/Volumes/Work/code/new-api/model/topup_payment_guard_test.go)
- [model/subscription_payment_guard_test.go](/Volumes/Work/code/new-api/model/subscription_payment_guard_test.go)

## Verification

已执行：

```bash
go test ./model -run 'TestCompleteSubscriptionOrderWithPaymentMethodRejectsMismatch|TestCompleteSubscriptionOrderWithPaymentMethodAcceptsEpayFamily|TestExpireSubscriptionOrderWithPaymentMethodRejectsMismatch|TestExpirePendingTopUpByTradeNoForPaymentMethodRejectsMismatch|TestMarkPendingTopUpFailedByTradeNoForPaymentMethodRejectsMismatch|TestRechargeRejectsNonStripeTopUpOrders|TestSearchUserTopUpsRejectsRepeatedWildcardPattern|TestGetUserTopUpsOnlyReturnsRecordsWithinThirtyDays' -count=1

go test ./controller -run 'TestStripeWebhookRejects|TestVerifyCreemSignatureRejectsBlankSecretEvenInTestMode' -count=1

go test ./model -count=1

go test ./controller -count=1
```

结果：

- `go test ./model -count=1` 通过
- `go test ./controller -count=1` 通过

## Residual Risks

- 本轮没有引入真实第三方支付沙箱联调，只做了代码级和包级回归
- 因此剩余风险主要在第三方平台字段语义漂移，不在本地订单收口逻辑
- 后续新增支付渠道时，必须继续复用带支付方式校验的收口 helper，不能重新引入“只按 `trade_no` 收口”的实现
