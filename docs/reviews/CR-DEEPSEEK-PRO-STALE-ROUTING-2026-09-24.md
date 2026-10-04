# CR: deepseek-v4-pro 残留旧路由表达式修复

- 日期：2026-09-24
- 范围：`setting/billing_setting/tiered_billing.go` 的 DeepSeek Pro 常量与 `upgradeBillingExpr`；`bin/repair_channel_quota_logs.go` 的已知表达式识别；正式库核实与文本改写
- 状态：缺陷已修复并验证。**正式库 options 行已于同日后续轮次改写，正式服务未重建**
- 前置记录：[pricing-maintenance.md](../operations/pricing-maintenance.md) 的 2026-09-12「DeepSeek V4 Pro 规则修正与正式目录收尾」、2026-09-24「deepseek-v4-pro 残留旧路由表达式修复」

## 结论摘要

正式库 `billing_setting.billing_expr` 中 9 个 `deepseek-v4-pro` 别名键携带一个把 Pro 在 2026-09-14 之后切到 **Flash 价格**的表达式，与 DeepSeek 官方口径和本仓库既有结论都相反。`upgradeBillingExpr` 此前不认识该文本，配置每次加载都原样保留。

当前该缺陷**不可达**（9 个键中仅 1 个有路由，且 6 条 `abilities` 全部禁用、关联渠道 `status=2`），按该表达式结算的 2 条历史日志均在边界之前、金额正确。因此本轮是消除潜伏缺陷，不是修正已发生的错账。

## 缺陷形态

9 个键的表达式文本完全一致，长度 591 字符：

```text
(month("UTC") < 9 || (month("UTC") == 9 && (day("UTC") < 14 || (day("UTC") == 14 && hour("UTC") < 4))))
  ? <Pro 峰谷价>
  : <Flash 峰谷价>
```

受影响键：

```text
DeepSeek-V4-Pro-0813
deepseek-ai/DeepSeek-V4-Pro
deepseek-ai/DeepSeek-V4-Pro-0813
deepseek-ai/deepseek-v4-pro
deepseek-ai/deepseek-v4-pro-0813
deepseek-ai/deepseek-v4-pro-nothinking
deepseek-ai/deepseek-v4-pro-thinking
deepseek-v4-pro-nothinking
deepseek-v4-pro-thinking
```

对照组：`deepseek-v4-pro`、`DeepSeek-V4-Pro`、`deepseek-v4-pro-0813` 三个键的表达式**不含**该边界，是官方峰谷价，正确。

## 官方依据

`https://api-docs.deepseek.com/quick_start/pricing` 当前状态：

- `deepseek-v4-pro` 按自身峰谷价计费：非高峰 input miss `$0.66`、cache hit `$0.022`、output `$1.98`；高峰 `$1.32`、`$0.044`、`$3.96`。
- 页面**未提及** 2026-09-14 或任何该日之后的变更，也未说明 v4-pro 被退役或改路由。
- 页面唯一的退役说明针对 **Flash 系列**：`deepseek-v4-flash` 与 `deepseek-v4-flash-vision-exp` 已退役，请求由 DeepSeek-V4.1-Flash 承接并按 Flash 价计费。该说明不适用于 v4-pro。

本仓库 `pricing-maintenance.md` 2026-09-12 一节已记录同一判定：「确认 `deepseek-v4-pro` 对应 `DeepSeek-V4-Pro-0813`，2026-09-14 之后仍继续提供 API，计费方式保持不变；不能因服务日期变化而切换到 Flash 价格」。源码常量 `DeepSeekV4ProBillingExpr` 上方的注释也写着同一结论。

## 影响量化

按真实表达式引擎核算边界之后的同一请求：

| 时段 | Pro 应收 | Flash 实收 | 差额 |
| --- | --- | --- | --- |
| 非高峰 | `p*0.66 + c*1.98 + cr*0.022` | `p*0.30 + c*1.20 + cr*0.006` | 少收约 43% |
| 高峰 | `p*1.32 + c*3.96 + cr*0.044` | `p*0.30 + c*1.20 + cr*0.006` | 少收约 71% |

## 可达性核实（只读）

| 检查 | 结果 |
| --- | --- |
| 9 个键在 `abilities` 的路由 | 仅 `deepseek-ai/deepseek-v4-pro`，6 条，全部 `enabled=false` |
| 关联渠道状态 | 渠道 46/59/62/103/127/189，`status=2`，`model_mapping=null` |
| 其余 8 个键的路由 | 无记录 |
| 按该表达式结算的日志 | 2 条（id `2988472`、`2988491`） |

## 历史日志复核

2 条受影响日志的时间均为 2026-09-11 04:31/04:32 UTC，**在 2026-09-14 边界之前**，因此走 Pro 分支：

```text
id 2988472  DeepSeek-V4-Pro   p=17 c=16  quota=21
id 2988491  deepseek-v4-pro   p=17 c=16  quota=21
```

非高峰（04:31 UTC 不在 `[1,4)` 或 `[6,10)` 内）重算：

```text
Pro   (17*0.66 + 16*1.98) / 1e6 * 500000 = 21.45  -> 取整 21  (与实收一致)
Flash (17*0.30 + 16*1.20) / 1e6 * 500000 = 12.15  -> 取整 12
```

实收 21 等于 Pro 分支结果，**金额正确，无需回算**。

## 修复内容

| 文件 | 改动 |
| --- | --- |
| `setting/billing_setting/tiered_billing.go` | 新增 `DeepSeekV4ProStaleRoutingBillingExpr`（精确 591 字符文本） |
| 同上 | `upgradeBillingExpr` 的 `deepseek-v4-pro` 分支加入该常量，配置加载时改写为 `DeepSeekV4ProBillingExpr` |
| `bin/repair_channel_quota_logs.go` | `isKnownDeepSeekTieredExpr` 加入该常量，使历史修正工具能识别并按行时间戳取档 |
| `setting/billing_setting/tiered_billing_test.go` | 新增 `TestDeepSeekProStaleRoutingExpressionIsRewritten`；在既有长度固定测试中加入 591 |
| 同上 | `GetBillingExprCopy` 改为返回改写后的表达式（此前返回库内原文，上游价格对比页会显示已被取代的旧价） |
| `setting/billing_setting/tiered_billing_test.go` | 新增 `TestBillingExprCopyMatchesRuntimeBilling`，并验证回退实现后确实失败 |

## 验证

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | exit 0 |
| `go test -count=1 ./setting/... ./bin/... ./pkg/billingexpr/ ./relay/helper/` | 全部 ok |
| `go vet ./setting/billing_setting/ ./bin/` | exit 0 |
| `gofmt -l`（全部已跟踪 Go 文件） | 无输出 |

探针（真实 `config.UpdateConfigFromMap` 加载正式库 `billing_expr` + 真实 `FormatMatchingModelName`）：

```text
9 个别名携带 09-14 分支：修复前 9/9  ->  修复后 0/9
```

回归测试有效性：移除 `upgradeBillingExpr` 中的新分支后，`TestDeepSeekProStaleRoutingExpressionIsRewritten` 对全部 8 个子用例失败（实际返回旧表达式）；恢复后通过。排除恒真断言。

同轮顺带完成的无关项：`setting/ratio_setting/model_ratio.go`、`cache_ratio.go` 的 gofmt 对齐（`gofmt -w` 后去空白字节比对完全一致，仅空白变更）。该格式问题在 `HEAD` 即存在，来自已提交的 `6794b63a5`。

## 未覆盖边界

- **正式库 options 行已在后续轮次改写**（本记录首版写作时尚未写入，此处按最终状态更正）。修复最初只在运行时配置加载与结算路径生效；同日后续轮次对正式库 `billing_setting.billing_expr` 执行了文本改写，9 个键的 591 字符旧表达式统一替换为源码表达式。改写后只读复核（`SELECT`）：

```text
total keys: 180
keys still containing peak_flash: 0
deepseek pro keys: 12
all 233 chars: True
```

  正式服务未重建，正式库写入以已授权为前提。
- **快照来源已查清**。该 591 字符文本从未进入源码（`git log --all -S 'day("UTC") < 14'` 无命中）。`CR-DEEPSEEK-PRICING-REPAIR-2026-09-11.md` 记录当时正式库 `/api/pricing` 的 Pro 即为「pre-2026-09-14 branch and Flash post-boundary branch」，即该表达式由 2026-09-11 那轮写入正式库。`pricing-maintenance.md` 2026-09-12 一节随后按官方口径改回不变更，但只覆盖了点名的 3 个 canonical 键（`deepseek-v4-pro`、`DeepSeek-V4-Pro`、`deepseek-v4-pro-0813`，现均为 233 字符的正确表达式），未覆盖 `deepseek-ai/` 前缀、`-thinking`、`-nothinking` 共 9 个别名键。本轮修复补齐的正是这个遗漏，因此缺陷性质是「上一轮修正覆盖不全」，而非「表达式本身判断错误」。
- **未在多用户/多渠道路由形态下验证**。本轮可达性判断基于当前正式库数据分布。
- 本轮未向正式服务（13000）发请求，未重建容器；全部正式库操作为 `SELECT`。
