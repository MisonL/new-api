# model ratio 默认回退风险复核（2026-04-21）

## Summary

本轮针对“未配置模型是否会被错误按 `37.5` 计费”做了代码与正式库双重复核。

结论：

- `106` 渠道的历史错误日志已经修复完成
- `/api/pricing` 展示层已经不再暴露错误的 `37.5`
- 但系统级风险仍然存在：运行时真实计费链路仍可能在未配置模型时回退到 `37.5`
- 正式库中除 `106` 以外，仍有其他渠道的同类历史日志残留

这不是单点数据问题，而是“默认回退策略 + 用户允许未配置模型继续计费 + 模型别名未归一化”共同导致的系统性问题。

## Control Contract

- Primary Setpoint
  - 未显式配置价格的模型不得进入真实扣费链路
- Acceptance
  - 运行时计费与 `/api/pricing` 使用同一套“显式配置才可计费”的判定
  - 正式库中不存在因未配置模型而落下的 `model_ratio=37.5` 脏日志
  - 模型别名、命名空间前缀、大小写差异不会绕过定价匹配
- Guardrails
  - 不影响合法的 `gpt-4.5-preview` 等真实 `37.5` 模型
  - 不误伤按次计费模型
  - 不把订阅日志错误纳入钱包修复
- Boundary
  - `setting/ratio_setting/`
  - `relay/helper/price.go`
  - `model/pricing.go`
  - `bin/repair_channel_quota_logs.go`
  - 正式库 `logs` / `quota_data` / `users` / `tokens` / `channels`

## Findings

### 1. 展示层已修，但运行时计费主链未完全收口

`/api/pricing` 当前已改为只读取显式配置的 model ratio，不再把缺失模型展示成 `37.5`。

位置：

- [model/pricing.go](/Volumes/Work/code/new-api/model/pricing.go)

但运行时真实计费仍调用 `GetModelRatio()`。该函数在未命中时仍可能返回 `37.5`。

位置：

- [setting/ratio_setting/model_ratio.go](/Volumes/Work/code/new-api/setting/ratio_setting/model_ratio.go)
- [relay/helper/price.go](/Volumes/Work/code/new-api/relay/helper/price.go)

### 2. 风险触发条件已在正式库中真实存在

正式库用户 `mison` 的 `setting` 中当前包含：

- `accept_unset_model_ratio_model=true`

这意味着只要模型未配置而代码未显式拒绝，真实扣费链路就可能继续接受默认回退值。

### 3. `106` 渠道历史问题已闭环

已确认：

- `channel_id=106` 且 `other` 中 `model_ratio=37.5` 的日志数量为 `0`
- 最新 dry-run 结果为 `repaired_logs=0`
- 历史小时桶在排除当前小时后，与 `quota_data` 聚合差异为 `0`

### 4. 全库仍有其他渠道残留同类历史日志

复核时，全库仍存在 `134` 条 `model_ratio=37.5` 的历史日志，主要分布：

- `71`: 49 条
- `72`: 23 条
- `75`: 16 条
- `76`: 16 条
- `46`: 10 条
- 其余：`1/58/59/87/115/124`

这些残留不在 `106`，说明问题不是单一渠道配置异常。

### 5. 当前修复工具只覆盖“可判定的钱包链路 + 可命中的模型名”

当前工具已能修复：

- `billing_source=wallet`
- 或 `billing_source` 缺失，但 `token_id=0` 且无订阅痕迹
- 且模型名可被显式价格配置命中

位置：

- [bin/repair_channel_quota_logs.go](/Volumes/Work/code/new-api/bin/repair_channel_quota_logs.go)

因此它不是“全量历史修复器”，仍存在明确边界。

### 6. 结构性盲区是“模型别名未归一化”

剩余残留中有多条模型名与价目表键名不一致，例如：

- `z-ai/glm5`
- `z-ai/glm4.7`
- `moonshotai/kimi-k2.5`
- `moonshotai/kimi-k2-thinking`
- `moonshotai/Kimi-K2-Thinking`
- `minimaxai/minimax-m2.5`
- `deepseek-ai/deepseek-v3.1-terminus`

这类名字不会被当前 `FormatMatchingModelName()` 归一化，因此：

- 运行时计费可能继续未命中
- 历史修复工具也无法命中

位置：

- [setting/ratio_setting/model_ratio.go](/Volumes/Work/code/new-api/setting/ratio_setting/model_ratio.go)

### 7. 现有“不可信 37.5”检测只存在于比率同步流程

仓库中已有“`model_ratio=37.5` 且 `completion_ratio=1` 不可信”的识别逻辑，但它仅在 ratio sync 逻辑中使用，不能阻止运行时计费继续吃到该回退。

位置：

- [controller/ratio_sync.go](/Volumes/Work/code/new-api/controller/ratio_sync.go)

## Evidence

本轮复核已确认以下事实：

- `106` 渠道修复后，`other like '%"model_ratio":37.5%'` 结果为 `0`
- 全库仍有 `134` 条同类历史日志，且 dry-run 显示多个渠道仍可被当前工具进一步修复
- 运行时计费函数 `GetModelRatio()` 未命中时仍可返回 `37.5`
- 正式库用户当前开启了 `accept_unset_model_ratio_model`

## Fix Scope

后续修复应至少覆盖三层：

### A. 运行时计费策略收口

- 未显式配置的模型不得进入真实扣费
- 展示层与运行时层共享同一判定口径

### B. 模型名归一化

- 建立供应商前缀、命名空间、别名、大小写归一规则
- 保证运行时计费、展示、日志修复都走同一归一化链路

### C. 历史日志批量修复

- 对非 `106` 渠道的残留分批 dry-run / apply
- 修复后复核 `logs`、`users`、`tokens`、`channels`、`quota_data`

## Verification Snapshot

本轮相关验证包括：

```bash
go test ./bin ./pkg/pricingrepair ./setting/ratio_setting ./model

docker exec new-api /tmp/repair_channel_quota_logs_linux -dsn '<redacted>' -channel-id 106

docker exec postgres psql -U root -d new-api -c "select count(*) from logs where channel_id=106 and other like '%\"model_ratio\":37.5%';"

docker exec postgres psql -U root -d new-api -c "select count(*) as total, count(*) filter (where channel_id=106) as channel_106 from logs where other like '%\"model_ratio\":37.5%';"
```

## Current Status

- 当前分支用途：记录问题与后续修复边界
- 当前文档不表示问题已整体修复完成
- 当前准确状态是：
  - `106` 已修
  - 系统根因未完全修
  - 其他渠道历史残留仍待处理
