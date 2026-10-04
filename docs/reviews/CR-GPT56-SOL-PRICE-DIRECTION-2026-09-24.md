# CR: gpt-5.6-sol 价格方向纠正、生效边界补全与生产侧只读核实

- 日期：2026-09-24
- 范围：`setting/billing_setting/tiered_billing.go`、`bin/repair_channel_quota_logs.go` 的 Sol 价格常量与改写/边界逻辑；正式库 `logs` 表中 Sol 日志的只读分类核实
- 状态：三个缺陷已修复并验证。**历史 177 行已回算（见「生产写入执行记录」），正式服务未重建**
- 前置记录：[pricing-maintenance.md](../operations/pricing-maintenance.md) 的 2026-09-07「当前生效价重新核对与线上纠正」一节

## 结论摘要

发现并修复三个缺陷，其中两个是我此前一轮改动自身引入的：

| # | 缺陷 | 性质 | 验证方式 |
| --- | --- | --- | --- |
| 1 | `$4/$20` 被当作促销价、`$5/$30` 被当作应收价并主动改写 | 价格方向反了，运行时按高价多收 | 官方 Changelog + 价格页 |
| 2 | `GetBillingExprAt` 对 `gpt-5.6-*` 无时间点规则 | 历史修正会追溯性少收旧日志 | 编译探针 |
| 3 | 改写规则 `expr != current` 过宽 | 覆盖管理员自定义表达式 | 编译探针 |

缺陷定位阶段全程对正式库只读，未执行 `UPDATE`/`DELETE`，未向正式服务发请求，容器未重建。后续取得写入授权后的回算执行见「生产写入执行记录」。

## 缺陷 1：价格方向反了

### 官方依据

OpenAI Changelog（`https://developers.openai.com/api/docs/changelog`）Aug 21 条目原文：

> GPT-5.6 Sol now costs $4 per million input tokens and $20 per million output tokens, representing 20% lower input pricing and 33% lower output pricing. GPT-5.6 Sol's promotional pricing is available at least through November 21, 2026.

关键在于它能**反推出变更前的价格**：「输入降 20%、输出降 33%」对应 `$5 -> $4` 与 `$30 -> $20`。这说明 `$5/$30` 是 2026-08-21 **之前**的价格，`$4/$20` 是**之后**的价格。

官方价格页 Standard 表当前行：

```text
| gpt-5.6-sol | $4.00 | $0.40 | $5.00 | $20.00 | $8.00 | $0.80 | $10.00 | $30.00 |
```

列为短上下文 输入/缓存读/缓存写/输出，随后是长上下文同序。与 `$4/$0.40/$5/$20`、`$8/$0.80/$10/$30` 精确一致。

### 缺陷机制

源码把 `$4/$20` 放在 `GPT56SolPromotionBillingExpr`、把 `$5/$30` 放在 `GPT56SolBillingExpr`，且 `upgradeBillingExpr` 主动把前者改写成后者。结果是正式库配置是对的（`$4/$20`），运行时却按 `$5/$30` 结算。

这不是「少收」而是「多收」：input +25%、output +50%。

### 修复

- `GPT56SolBillingExpr` = 当前价 `p * 4 + c * 20 + cr * 0.4 + cc * 5`；长上下文 `p * 8 + c * 30 + cr * 0.8 + cc * 10`
- 新增 `GPT56SolFullPriceBillingExpr` = 旧价 `p * 5 + c * 30 + cr * 0.5 + cc * 6.25`；长上下文 `p * 10 + c * 45 + cr * 1 + cc * 12.5`
- `GPT56SolPromotionBillingExpr` 别名指向当前价；`GPT56SolLegacyBillingExpr` 别名指向旧价

### 编译器发现的问题

两个常量同值后，`bin/repair_channel_quota_logs.go` 的 `isKnownOpenAITieredExpr` switch 出现重复 case：

```text
duplicate case billing_setting.GPT56SolPromotionBillingExpr ... in expression switch
```

该工具的语义是「识别已知 tiered 表达式」，需要两个独立值，已改用 `GPT56SolFullPriceBillingExpr`（`knownExprMarkers` 与 `isKnownOpenAITieredExpr` 各一处）。

**这个错误测试发现不了**：两个 case 同值时行为仍「正确」（都返回 true），只有编译能拦住。

## 缺陷 2：历史修正缺少时间边界

### 探针证据

```text
gpt-5.6-sol before=... p * 4 + c * 20 ... ok=true
gpt-5.6-sol after =... p * 4 + c * 20 ... ok=true
--- FAIL: before and after the 2026-08-21 price change return the same expression
```

`GetBillingExprAt` 对 `gpt-5.6`、`gpt-5.6-sol`、`gpt-daybreak-blue-latest` 只走 deepseek 分支后 fallthrough 到 `GetBillingExpr`，因此**任何**历史时间点都返回当前价。

后果：修正器会把 2026-08-21 之前按 `$5/$30` 正确结算的 114,516 行改写成 `$4/$20`，即追溯性少收。

`GPT56SolPromotionEffectiveAt`（2026-08-21T00:00:00Z）此前注释明确写着「仅作审计标记、不参与选择」，本轮改为真正参与选择。

### 修复

在 `GetBillingExprAt` 的 `createdAt > 0` 分支新增：

```go
case "gpt-5.6", "gpt-5.6-sol", "gpt-daybreak-blue-latest":
    if created.Before(GPT56SolPromotionEffectiveAt) {
        return GPT56SolFullPriceBillingExpr, true
    }
    return GPT56SolBillingExpr, true
```

## 缺陷 3：改写规则过宽

初版改写为 `if expr != GPT56SolBillingExpr { return GPT56SolBillingExpr }`——任何不等于当前价的表达式都被覆盖，包括管理员自定义表达式。

探针确认：

```text
want len <= 272000 ? tier("standard", p * 7 + c * 7) : ...
got  len <= 272000 ? tier("standard", p * 4 + c * 20 + cr * 0.4 + cc * 5) : ...
```

已收紧为只改写已知旧价：`if expr == GPT56SolFullPriceBillingExpr`。修复后探针转绿，自定义表达式原样保留。

## 生产侧只读核实

### 边界分类

对正式库 `type=2` 的 Sol 日志按 2026-08-21 边界与表达式族分类：

| 分类 | 行数 | 判定 |
| --- | --- | --- |
| 边界前 + 旧价 `$5/$30` | 114,516 | 正确，**不应改写** |
| 边界前 + 当前价 `$4/$20` | 0 | 无此类行 |
| 边界后 + 旧价 `$5/$30` | 177 | 多收，应修正到当前价 |
| 边界后 + 当前价 `$4/$20` | 126 | 正确，不应改写 |

另有 `type=5` 的 33,495 行无表达式，属于其他日志类型，不在本次范围。

### 边界前 114,516 行：确实按旧价结算

```text
sum_quota(实际)        = 12,142,800,616
按 $5/$30 重算          = 12,142,801,561
差                     = 945 quota（相对 0.000008%）
```

两者一致，证明这批行按 `$5/$30` 正确结算，追溯改写成 `$4/$20` 即少收。

### 边界后 177 行：确实多收

```text
quota 合计(实际)        = 18,640,432
按 $4/$20 重算应为       = 14,492,770
差额                    =  4,147,662 quota（约 $8.30）
```

逐行抽样复核（如 id `2288878`：p=16286、c=5，实测 quota 40790，正好等于 `(16286*5 + 5*30)/2`）。分布在 2026-08-23 至 2026-09-08。

### 修正器真实 dry-run 复核（只读）

为确认修正器的实际选择与上述 SQL 分类一致，用本轮源码构建 `linux/amd64` 修正器，复制进正式容器执行 dry-run（不带 `-apply`，`applyPlan` 不会被调用，全程只读）：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/repairprobe/repair ./bin/
docker cp /tmp/repairprobe/repair new-api:/tmp/repair-probe
docker exec new-api sh -c 'SQL_DSN="$SQL_DSN" /tmp/repair-probe \
  -dsn-env SQL_DSN -channel-id 0 -models "gpt-5.6-sol,gpt-5.6" \
  -plan-output /tmp/sol-plan.csv'
```

```text
channel_id=0 repaired_logs=177
model=gpt-5.6-sol count=177 old_quota=18640432 new_quota=14492770 delta=-4147662
```

修正器独立选出的行数与金额同本文的 SQL 分析**完全一致**（177 行、`18,640,432 -> 14,492,770`、差额 `-4,147,662`），且只涉及 `gpt-5.6-sol`，未选中边界前的 114,516 行。这验证了两件事：边界规则确实生效（旧日志被跳过），以及改写规则确实只命中该修正的行。

plan CSV 抽样（id `2288878`）：`old_quota=40790`、`new_quota=32622`，`expr_b64` 由旧价改为当前价，与手算 `(16286*5 + 5*30)/2 = 40790`、`(16286*4 + 5*20)/2 = 32622` 一致。

dry-run 后复查正式库，那 177 行仍为旧价（`count=177`），确认该 dry-run 未发生写入。探针二进制与 plan CSV 已从容器内删除。（随后经用户授权执行的真实写入见「生产写入执行记录」。）

## 验证命令与退出码

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | exit 0 |
| `go test -count=1 ./setting/billing_setting/` | ok |
| `go test -count=1 ./bin/...` | ok |
| `go vet ./setting/billing_setting/ ./bin/...` | exit 0 |
| 探针：边界前返回旧价、边界及之后返回当前价 | PASS |
| 探针：自定义表达式原样保留 | PASS |
| 正式库 Sol 分类查询 | 只读，见上表 |

本阶段正式库查询全部为 `SELECT`；未执行写入，未向 `13000` 发请求，容器未重建。（本阶段之后的回算写入见「生产写入执行记录」。）

## 测试改动

| 测试 | 改动 |
| --- | --- |
| `TestOpenAIOfficialListPriceExpressionsSelect272KBoundary/gpt-5.6-sol` | 41750/68500 -> 29400/48800 |
| `TestGPT56SolHistoricalExpressionUsesNonPromotionalListPrice` | 改名为 `...FollowsPriceChangeBoundary`；断言边界前=旧价、边界及之后=当前价 |
| 新增 `TestGPT56SolCustomExpressionIsNotOverwritten` | 固化缺陷 3 的回归 |
| `TestRepairRowRepairsOpenAILongContextAndCacheCreation` | 1097500 -> 875000 |
| `TestRepairRowMovesPromotionalSolLogToListPrice` | 改名 `...MovesSolFullPriceLogToCurrentPrice`；输入改 `FullPrice` 常量；17500 -> 12000 |
| `TestRepairRowKeepsCorrectSolListPriceAcrossPromotionBoundary` | 改名 `...KeepsSolFullPriceLogBeforeBoundary`；输入改 `FullPrice` 常量，断言 no-op |
| 新增 `TestRepairRowRestoresFullPriceToPreBoundarySolLog` | 固化缺陷 2 的反向回归 |

## 生产写入执行记录（2026-09-24）

用户批准执行生产写入。因修正器**没有内置备份**（`applyLogUpdates` 直接 `UPDATE`），先建备份表再 apply。

### 备份表

| 表 | 行数 |
| --- | --- |
| `pricing_repair_20260924_sol_logs` | 177 |
| `pricing_repair_20260924_sol_users` | 1 |
| `pricing_repair_20260924_sol_tokens` | 1 |
| `pricing_repair_20260924_sol_channels` | 10 |
| `pricing_repair_20260924_sol_quota_data` | 26 |

### apply 输出

```text
channel_id=0 repaired_logs=177
model=gpt-5.6-sol count=177 old_quota=18640432 new_quota=14492770 delta=-4147662
redis cache invalidated: keys=2
apply completed: repaired_logs=177
```

### 逐表核实

- **logs**：177 行旧价 -> 0 行；177 行当前价；quota 合计 `14,492,770`（精确等于计划值）。
- **quota_data**：26 桶，退款合计 `4,147,662`（精确），`count` 字段 0 处变化；未变的 6 桶正是 2026-09-14..09-21 那批当前价行。
- **channels**：10 个渠道的退款额逐个精确匹配（如 ch222 `-2,193,191`、ch201 `-1,049,587`）。ch221 净 `+12,433` 是退款 `773` 被同期实时流量抵消后的结果。
- **tokens / users**：因正式服务**正在服务实时请求**，余额字段同时受退款与实时消费影响，无法用单点快照直接比对。

### 实时流量的处理

写入期间正式服务有活跃流量（`deepseek-v4-flash`、`gpt-5.6-luna` 等）。为把「工具是否算错」与「实时消费」分开，做了**前向一致性**验证：取两次带日志水位线的原子快照，间隔 45 秒：

```text
token5   remain_quota 变化 -45,015 ; used_quota 变化 +45,015
user1    quota        变化 -45,015 ; used_quota 变化 +45,015
```

两者**同步移动完全相同的金额且方向相反**，即修复后余额仍在正确跟踪实时消费，未出现漂移。（区间内日志合计 48,205，与 45,015 的差 3,190 是请求已扣费但日志尚未落库的在途部分。）

### 幂等复核

apply 后再跑一次 dry-run：`repaired_logs=0`。确认无残留待修行。

### 状态

- 正式库 `logs`、`users`、`tokens`、`channels`、`quota_data` 五表已按计划更新；Redis 2 个缓存键已清理。
- 修正器二进制与 plan CSV 已从容器内删除；本地临时文件已清理。
- **正式容器未重建、未重启**（`StartedAt` 早于本轮操作）。

## 源码默认值同步修正

`defaultModelRatio` / `defaultCompletionRatio` 中 `gpt-5.6`、`gpt-5.6-sol`、`gpt-daybreak-blue-latest` 仍是旧价（`2.5`/`6`，即 `$5/$30`），与正式库的 `2`/`5` 不一致。生产计费不受影响（这三个模型是 `tiered_expr`，`ModelPriceHelper` 直接走表达式分支，不读倍率），但源码默认值是新建部署和数据库缺键时的取值来源。已改为 `2`/`5`，三处测试断言同步更新。

## 分档表达式缺口补入（同日第二轮）

在源码与正式库的对照扫描中发现 7 个 canonical 键在源码中缺少长度分档表达式，其中三个（`gpt-6-sol`、`gpt-6-luna`、`grok-4.7`）的倍率注释明确宣称「see billing tier below 272K/200K input」——注释与代码不一致。

### 为什么是真缺陷

`setting/config/config.go` 的 Map 分支为 `field.Set(fresh.Elem())`，即配置加载**替换**而非合并 map。探针实证：

```text
VERDICT: REPLACE - source-only keys are DROPPED by a DB row
```

故 `options` 行缺少某键时，源码默认值里独有的键不会被兜底，而是整个消失。用真实表达式引擎验证退化后果：300K 输入的 `gpt-6-sol` 应收 `$1.23`，基础档只收 `$0.62`，少收 50%。

### 范围

| 项 | 值 |
| --- | --- |
| 补入 canonical 键 | 7 |
| 补入 map 项 | 8（mode + expr 各 7，其中 grok-4.20 两个键共用常量） |
| 新增常量 | 3（`GPT6SolBillingExpr`、`GPT6LunaBillingExpr`、`Grok47BillingExpr`） |
| 复用既有常量 | 4 个键 |
| 价格数值改动 | 无（全部对齐正式库现值） |

### 核实

- 补入值与正式库逐键**精确一致**。
- 每个 canonical 键下，正式库所有归一化到它的别名取值**互相一致**：`grok-4.20-0309` 8 个、`grok-4.20-multi-agent-0309` 4 个、`gemini-3.1-pro` 2 个。
- 新增两个回归测试，并**验证过移除任一条目时测试确实失败**，排除恒真断言：
  - `TestTieredModelsDeclaredInRatioCarryABillingExpr`
  - `TestLengthTieredModelsBranchOnContextLength`
- `gemini-3.1-flash-lite-latest` 官方无长上下文档，测试显式断言其**不含** `long_context`（而非套用统一模板）。

### 正式环境状态

正式库不受影响（表达式在库内）。隔离开发库 `new-api-dev` 的三个键为 ABSENT，属已受影响环境。正式环境这三个模型当前零消费日志，`abilities` 中有路由记录但从未被调用。

## 未覆盖边界

- **2026-11-21 之后的价格官方未公布**，因此无法建立到期规则。已穷尽核对 pricing 页、changelog（最新 2026-09-22）、模型页与 deprecations 页，四处均无到期后价格。`GetBillingExprAt` 在该日期之后仍返回 `$4/$20`。这需要官方信息解锁，不能用猜测填默认值；到期前应重新核对。相关证据见 `pricing-maintenance.md` 的「2026-11-21 到期后的价格：官方未公布」。
- **`GPT56SolPromotionEffectiveAt` 取 Changelog 的日级日期**（Aug 21 `00:00:00Z`），官方未给具体时刻。经查正式库在该日的 Sol 日志数为 0，**不影响任何生产行**；若日后取得更精确时刻可收紧。
- **修正器没有内置备份**：`applyLogUpdates` 直接 `UPDATE`，无自动备份步骤。本轮手动建了 5 张备份表。该工具在其他场景复用时应同样先建备份。
- **写入期间正式服务有活跃流量**，因此 `users`/`tokens` 的余额无法用单点快照比对，改用前向一致性验证（见「生产写入执行记录」）。绝对金额层面的历史对账未做。
- 本次分类依赖正式库当前数据分布（`username` 仅 1 个取值）；未在多用户形态数据上验证。
- **源码与正式库的分档配置仍是两套**。本轮只补了 7 个键；正式库 175 个 tiered 键中仍有 121 个不在源码里。按提交 `5af49fd51` 的既有决定（「分档表达式只存在于正式库 `billing_setting.billing_expr`」），这是有意的分工，但意味着**任何源码默认值改动都必须同时核对正式库**，否则会在替换语义下静默失效。未改为单一来源，属架构取舍，需另行评估。
