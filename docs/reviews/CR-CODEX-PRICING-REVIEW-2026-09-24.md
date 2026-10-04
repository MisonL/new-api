# CR: codex 对 2026-09-24 计费改动的独立审查与核验

- 日期：2026-09-24
- 范围：本轮计费改动（`setting/ratio_setting/model_ratio.go`、`setting/billing_setting/tiered_billing.go`、正式库/隔离库 `billing_setting.billing_expr` 与 `ModelRatio` 等 option 行）经外部模型独立审查后的逐条核验
- 状态：codex 的 4 条发现全部独立复现（2 条为脚本/测试健壮性问题、无实际损害，2 条成立）。另发现 codex 因无数据库访问而无法看到的第 5 项，经用户批准已一并修复并验证
- 前置记录：[CR-DEEPSEEK-PRO-STALE-ROUTING-2026-09-24.md](CR-DEEPSEEK-PRO-STALE-ROUTING-2026-09-24.md)、[CR-GPT56-SOL-PRICE-DIRECTION-2026-09-24.md](CR-GPT56-SOL-PRICE-DIRECTION-2026-09-24.md)、[pricing-maintenance.md](../operations/pricing-maintenance.md) 的 2026-09-24 各节

## 审查方式

用 `codex exec -s read-only`（模型 `gpt-6-luna`，`reasoning=max`）对一份临时审查包做独立审查：包内 `BRIEF.md` 列 6 个编号问题，附各文件 diff、3 个 SQL 脚本、未跟踪测试文件、正式库 `billing_expr` 快照。codex 被要求读真实源码而非只信 diff。

**来源可追溯性说明**：审查包与 codex 的原始输出位于临时目录 `/tmp/codexreview/`（含 `BRIEF.md`、`codex_findings.md`、`codex_run.log`），该目录**已随本轮收尾清理，不可再取**。因此本文件完整转述了 codex 的每一条结论与其六问回答，读者无需回看原始输出；三个 SQL 脚本另归档在 `docs/reviews/artifacts/`（该目录被 gitignore，见下）。若需复现该审查，重建同构的 brief 与 diff 包即可，codex 的判定不依赖任何未公开输入。

运行时长 1h23m，中途发生一次网络重连。codex 在最终报告中明确声明「本轮未修改文件、未运行测试，也未连接数据库」，因此它对两个库的实际数据分布只能给「无法从当前快照确认」，这部分由本轮补测。

核验原则：不采信 codex 结论本身，逐条用真实数据或真实运行复现。这与既有规则「子代理审计结论需独立复现」一致。

## 结论摘要

| # | codex 发现 | 独立核验判定 | 处置 |
| --- | --- | --- | --- |
| P1 | DeepSeek SQL 只 guard 一个键，可能覆盖管理员自定义表达式 | 机制成立，实际损害为零（9 个键写入前全为旧文本，无覆盖） | 已执行不回改；脚本归档并记录局限 |
| P2 | Sonar 测试把未证实的 `$1/M` 锁定为正确值 | 成立，属测试表述问题 | 已重写测试注释与断言语义 |
| P2 | `UPDATE 0` 仍 `COMMIT`，可能静默成功 | 成立，属脚本可观测性缺陷 | 脚本归档并记录局限 |
| P2 | （codex 六问回答）并发竞争非本次引入 | 与 `-race` 实测一致（旧体/新体各 2 报告） | 已定位；「显然修法」不安全，记为独立任务 |
| 第 5 项 | （codex 未发现）隔离库 flash 14 键不被改写规则识别 | 成立且可达（1.4667 倍） | 已修复并验证，已更正文档 |
| 第 6 项 | （codex 未发现）隔离库 grok 14 键旧 `<=` 边界不被识别 | 成立且可达（恰好 200000 token 时半价） | 已修复并验证 |

无 P0。本轮改动本身没有引入计费方向性错误。

## P1：DeepSeek SQL 的 guard 范围不足

codex 指出 `3_deepseek_pro_stale_text.sql` 的 `WHERE` 只检查 `deepseek-ai/deepseek-v4-pro` 一个键是否仍为旧文本，其余 8 个键由 `value::jsonb || ...` 无条件覆盖；若其中任一为管理员自定义表达式，会被静默覆盖。

该脚本已执行，因此可用写入前的备份表直接证伪实际损害。`pricing_repair_20260924_deepseekpro_options` 保存了完整写入前 `options` 行：

```text
key                                          pre_len pre_stale post_len changed
deepseek-ai/deepseek-v4-pro                      591      True      233    True
deepseek-ai/deepseek-v4-pro-0813                 591      True      233    True
deepseek-ai/deepseek-v4-pro-thinking             591      True      233    True
deepseek-ai/deepseek-v4-pro-nothinking           591      True      233    True
deepseek-ai/DeepSeek-V4-Pro                      591      True      233    True
deepseek-ai/DeepSeek-V4-Pro-0813                 591      True      233    True
deepseek-v4-pro-thinking                         591      True      233    True
deepseek-v4-pro-nothinking                       591      True      233    True
DeepSeek-V4-Pro-0813                             591      True      233    True

非旧文本却被改动的键：0
```

9 个键写入前全部携带 591 字符旧文本，全部被改写，无一个非旧文本键被触及。即 codex 描述的失败场景在本例中未发生，实际损害为零。

机制判断成立：guard 只约束 9 个键中的 1 个。这是真实的脚本健壮性缺陷，记为后续改进项（逐键 guard，或写入前断言 9 个键都符合预期再更新）。已执行完毕，不回滚。

## P2：Sonar 断言把未证实的价格锁定为正确值

`1 / 1000 * USD` 折叠为 0 已独立验证（编译运行，非推理）：

```text
1 / 1000 * USD   = 0
1.0 / 1000 * USD = 0.5
0.2 / 1000 * USD = 0.1
```

修复方向（消除使模型免费的零倍率）无疑。但 codex 指出 `0.5`（即 `$1/M`）本身缺少发布方现行价格依据。这一点与本文件既有结论一致：`pricing-maintenance.md` 的判定表已把这两个键写为「Perplexity 现役价格页只列 Sonar / Sonar Pro / Sonar Reasoning Pro / Sonar Deep Research，且标注 Sonar Chat Completions 支持至 2026-09-27 / 发布方无现行价格，无法判定方向 / 保持现状」。

因此 `TestSonarLargeModelRatioIsNotZero` 断言的 `0.5` 是一个本仓库文档自己声明无法核实的价格。测试应锁定「非零且等于 `$1/M` 的整除修复语义」，而不是把 `$1/M` 宣称为当前正确价。属测试表述问题，不是计费缺陷。

可达性：正式库 `abilities` 中 sonar 系列 0 条记录、`logs` 中 0 条，当前不可达。

**已处置**：重写 `setting/ratio_setting/model_ratio_sonar_test.go` 的注释与断言语义，明确该测试锁定的是整除语义与文档化的末次价格，**不主张 `$1/M` 是当前可核实的正确价**；并把「非零」断言提到价格断言之前，使回归意图（零倍率 = 免费）成为首要断言。另补 `TestSonarLargeToSmallRatioIsFive` 固化两族 1:5 比例，防止只修一侧的除法而留下不一致。

## P2：UPDATE 0 仍 COMMIT

三个 SQL 均为 `UPDATE options SET value=... WHERE key='...'` 后 `COMMIT`。若目标行不存在，PostgreSQL 返回 `UPDATE 0`，脚本仍提交并以退出码 0 结束，自动化调用无法区分成功与未命中。

本轮执行中目标行都存在（由备份表与现值比对确认 5 个 option 键全部命中），未造成实际后果。但脚本无法区分「已是新值的预期无操作」与「意外未命中」，而这正是 SQL 3 的 guard 想表达却表达不全的语义。

**已处置**：三个脚本连同 P1 的 guard 局限一并归档到 `docs/reviews/artifacts/`（该目录按既有约定被 `.gitignore` 忽略，与既有 CR 处理方式一致；仓库内记录以本文件为准），并在同目录 `pricing-repair-2026-09-24-README.md` 中逐条记录这两项局限与后续同类脚本应采取的写法（逐键 guard、事务内校验受影响行数）。脚本已执行完毕，不回改。

## 第 5 项：codex 未发现的隔离库 flash 分歧

codex 声明未连接数据库，因此看不到库内实际分布。本轮补测发现：

隔离库 `new-api-dev` 的 `deepseek-v4-flash` 全部 14 个键存 370 字符 `DeepSeekV4FlashLegacyMonthBillingExpr`，**不被 `upgradeBillingExpr` 识别**。flash 分支只认 `Current` / `Production` / `Legacy` 三个常量，不含 `LegacyMonth`；而 pro 分支含 `LegacyMonth`。两个分支的常量清单不同。

实测计费差异：

```text
p=1000000 c=0 : 隔离库=220000 源码现价=150000  高 1.4667 倍
```

隔离库该模型 `billing_mode=tiered_expr`，`abilities` 中渠道 4/32/40/146 `enabled=true` 且 `status=1`，**可达**。隔离库为开发环境，不影响正式计费。

这一发现同时推翻了本仓库文档中的两处错误表述。`pricing-maintenance.md` 原文把隔离库 pro 的 373 字符文本描述为「被改写规则识别并归一到同一表达式」；实测 `LegacyMonthBillingExpr` **不在任何改写分支中**（flash 与 pro 两个分支都不含），pro 之所以不产生错账是**巧合等价**：守卫 `month("UTC") != 8` 在 9 月为真，选中 `p*1.32 + c*3.96 + cr*0.044` / `p*0.66 + c*1.98 + cr*0.022`，与 `DeepSeekV4ProBillingExpr` 系数逐项相同。差异根源是 Pro 在 V4.1 调价中价格未变、Flash 变了，因此同族的 flash 不具备这个巧合。已更正该段，并把「deepseek-v4-pro 全部 8 个键」更正为实测的 12 个键。

正式库不受影响：三个写入前备份快照（175/179/180 键）中均不含 370/373/319 这些长度；正式库现值 180 键经 `upgradeBillingExpr` 后 `upgraded=0`，即正式库不存在任何未被识别的 legacy 文本。

`MiniMax-M3` 在隔离库存 319 字符非折后表达式（标准档系数为源码 2 倍），同样不被识别，但 `abilities` 中无任何 M3 路由记录，当前不可达，仅记为同类隐患。

### 修复（本轮已实施）

用户批准后本轮一并修复。改动 `setting/billing_setting/tiered_billing.go` 的 `upgradeBillingExpr`：

- flash 分支加入 `DeepSeekV4FlashLegacyMonthBillingExpr` 与 `DeepSeekV4FlashLegacyMonthNoWeekdayExpr`。
- pro 分支加入 `DeepSeekV4ProLegacyMonthBillingExpr` 与 `DeepSeekV4ProLegacyMonthNoWeekdayExpr`。Pro 的两个快照今日系数与现价相同、不构成多收，纳入是为了避免 Pro 日后调价时同样失守。

枚举确认这是**完整的一类**，不是只补了我偶然发现的一个：两个库实际存储的 DeepSeek 快照常量只有 `LegacyMonth`（flash 370 / pro 373）两种，其余 9 个常量（`LegacyMonthNoWeekday` 318/321、`Legacy*` 393/396、`StaleRouting` 591、各 `PeakFrag`）在两个库中均未出现。即两个库存的正是被漏掉的那两个。

验证：

```text
修复前  go test -run TestDeepSeekLegacyMonthSnapshotsAreRewritten   FAIL（非空断言，反控有效）
修复后  同命令                                                      ok
隔离库  upgradeBillingExpr 改写数   12 -> 38（12 grok + 14 flash + 12 pro）
正式库  upgradeBillingExpr 改写数   0 -> 0（不受影响）
隔离库  deepseek-v4-flash 运行时计费  220000 -> 150000（与源码现价一致）
正式库  deepseek-v4-flash 运行时计费  150000（不变）
go build ./...                                                      exit 0
go test -count=1 ./setting/... ./bin/... ./pkg/billingexpr/ ./relay/helper/   ok
gofmt -l setting/ bin/ relay/                                       无输出
```

新增测试 `TestDeepSeekLegacyMonthSnapshotsAreRewritten`（覆盖 8 个 flash 键名与 5 个 pro 键名、两个常量，并断言两族不交叉、管理员自定义表达式不被覆盖）与 `TestDeepSeekLegacyMonthSnapshotIsAnActualOvercharge`（固化「该快照确实不等于现价」，防止未来系数趋同后测试静默失效）。

**未用「巧合等价」掩盖**：pro 的 373 字符文本之所以无害，只因 Pro 在 V4.1 调价中价格未变；该等价关系已写入代码注释与测试注释。

未执行：未改写隔离库 option 行（修复只在运行时配置加载路径生效，与 pro 那轮的处理方式一致）；未重建任何容器；未向正式服务发请求。

## F1：BillingExpr 读写竞争——已定位，但「显然修法」不安全，故不改

codex 判断该竞争非本次改动引入。本地用 `-race` 实测确认，且量化了它：

```
TestProbeOldBody  (lo.Assign only, 改动前的函数体)  -> races=2  FAIL
TestProbeNewBody  (GetBillingExprCopy, 改动后的)    -> races=2  FAIL
```

改动前后竞争报告数相同，故本次改动既不引入也不改变其形态。竞争对是
`config.go:297` 的 `reflect.Value.Set`（写）与 `tiered_billing.go:493` 的
`lo.Assign`（读）。无 `-race` 时探针通过（`-count=3`），因此实际暴露面是
60 秒 `model.SyncOptions` 重载或保存配置期间的一次陈旧但自洽的读取，不是崩溃：
`field.Set` 发布的是单个 map header 存储，两个 map 都在发布前构造完成，且
grep 确认没有任何代码路径就地修改这两个 map（仅 4 处读 + 2 处 `lo.Assign` 副本）。
`upgradeBillingExpr` 作用于副本，不写共享 map。

### 为什么没有顺手修

最直觉的修法是给读侧 `GetBillingMode` / `GetBillingExpr` 加
`common.OptionMapRWMutex.RLock()`。这在本仓库会形成 ABBA 环：

```text
[A] updateOptionMap 持 OptionMapRWMutex(W)        model/option.go:254
    -> handleConfigUpdate -> billing_setting 分支  model/option.go:693
    -> InvalidatePricingCache -> updatePricingLock(W)   model/pricing.go:91

[B] GetPricing 持 updatePricingLock(W)             model/pricing.go:79
    -> updatePricing()                             model/pricing.go:84
    -> GetBillingMode()                            model/pricing.go:359
    -> 若加 OptionMapRWMutex.RLock()，即与 [A] 的写锁互等
```

`pricing.go:359` 的 `GetBillingMode` 经确认位于 `updatePricing()` 内，而
`updatePricing()` 只在 `updatePricingLock` 持有期间被调用。因此 [A] 与 [B]
的锁获取顺序相反。

### 已确定性复现（反控有效）

前三次探针都没复现，原因是探针构造缺陷，不是环不存在：B 的临界区太短，
A 到达时锁已释放；或 `updatePricing()` 在无数据库的测试环境里模型列表为空，
`GetBillingMode` 从未被调用。修正构造——**让 A 先持 `OptionMapRWMutex` 写锁**，
使 B 的 RLock 真正阻塞——后确定性复现：

```text
应用 naive fix（GetBillingMode 加 OptionMapRWMutex.RLock）
  -> ABBA DEADLOCK: A holds OptionMapRWMutex and waits on updatePricingLock;
     B holds updatePricingLock and waits on OptionMapRWMutex.RLock     8.10s FAIL

移除 naive fix（反控）
  -> A completed: no ABBA cycle                                       ok 0.186s
```

因此这不是理论风险，而是照该修法改就会真实触发的进程级挂死。若要推进，
正确方向是消除锁序反转本身（例如让 `GetPricing` 不再持 `updatePricingLock` 时
调用配置读取，或把 `InvalidatePricingCache` 移出 `OptionMapRWMutex` 临界区），
而不是给热路径读侧加锁。

### 影响面

`-race` 不在任何 CI workflow（`.github/workflows/` 无 `race`）也不在发布门禁
（`docs/operations/new-api-production-release-gates.md` 的测试命令为
`go test ./controller ./model ./relay/common ./relay/helper ./service`，无 `-race`）
中，因此该竞争不会被现有流程拦下。它是 `setting/config` 对所有注册配置字段的
既有设计问题，不是计费正确性问题，也不影响任何金额。本轮不动代码，记为独立任务。

## 第 6 项：同类遗漏的第二处（grok 边界文本）

上轮修掉 `LegacyMonth` 后，我把「存储文本 vs 源码现值 vs 改写规则」的比较推广到全部常量家族，又发现一处同类遗漏。判据是：对每个库内存储文本算出改写后的有效文本，若它既不等于该模型规范化名的源码现值、也不等于任何源码常量，即为候选。

```text
正式库  total_keys=180  true_gaps=0
隔离库  total_keys=121  true_gaps=16
```

隔离库的 16 个 gap 分两组：

1. **grok 家族（14 键）**：库内存 `len <= 200000` 文本，源码现值为 `len < 200000`，**逐字差异仅在边界符号**（已逐条比对确认）。涉及 `grok-4.3`、`grok-4.20`、`grok-4.20-fast`、`grok-4.20-0309-non-reasoning`、`grok-4.6`、`grok-build-0.1`、`grok-code-fast-1`、`grok-latest` 及其 `grok/`、`x-ai/`、`xai/` 前缀变体。
2. **MiniMax-M3（4 键）**：存 319 字符非折后 priority 表达式，与上轮已记录、判定为「无路由、不可达」的隐患相同。

### 方向判定（不靠猜）

仓库文档自身口径矛盾：08-14 节写「表达式使用 `len <= 200000`，边界 token 归入标准档」，09-10 节写「使用 `len < 200000` 的同值阶梯表达式」。用三条独立证据定方向：

```text
源码 HEAD            Grok43/46/Build01/47 全为 len < 200000
正式库 prod          7 个 grok 键全为 len < 200000
已提交测试            TestGrok45OfficialLongContextExpressionSelectsPrices
                     断言 Len=200000 时 MatchedTier == "long_context"
```

且 `a2724e3ff`（2026-09-14）把各 grok 家族的边界从 `<=` 统一改为 `<`，同时为 `grok-4.5` 补了 `Grok45LegacyThresholdBillingExpr` 专门识别旧 `<=` 文本——**其余三个家族没有对应常量**。这与 `LegacyMonth` 是同一形态：源码常量改了，但已写入库的旧文本没被做成可识别。

### 影响与可达性

边界差异只落在 `len == 200000` 这一个点上，后果是半个价格：

```text
grok-4.3        len=200000  旧=250000(standard)      现=500000(long_context)
grok-4.6        len=200000  旧=400000(standard)      现=800000(long_context)
grok-build-0.1  len=200000  旧=200000(standard)      现=400000(long_context)
```

隔离库 `grok-4.20-0309-non-reasoning`、`grok-4.20-fast` 在渠道 4/32 上 `enabled=true`、`status=1`，该路径可达。正式库不受影响（grok 键全为 `<` 版，改写数 0）。

### 修复

新增 `Grok43LegacyThresholdBillingExpr`、`Grok46LegacyThresholdBillingExpr`、`GrokBuild01LegacyThresholdBillingExpr` 三个常量（逐字取自库内文本），并在 `upgradeBillingExpr` 补三个分支，覆盖归一化目标 `grok-4.20-0309`、`grok-4.20-multi-agent-0309`、`grok-code-fast-1`。

```text
反控  移除三个分支 -> TestGrokLegacyThresholdSnapshotsAreRewritten  FAIL
还原  同命令                                                        ok
隔离库  全部 grok 键 FIXED，still_wrong=0
隔离库  len=200000 端到端计费与源码现值逐项一致（含 MatchedTier）
正式库  mismatches=0（不受影响）
```

新增 `TestGrokLegacyThresholdSnapshotsAreRewritten`（覆盖 13 个键名、3 个常量，断言不跨家族、不覆盖自定义表达式）与 `TestGrokLegacyThresholdBoundaryIsAnActualOvercharge`（固化「旧文本确实选标准档、现文本选长上下文档、价差 2 倍」）。

### 与 Gemini / OpenAI 的对照（说明 `<` 不是笔误）

同库中 `gemini-2.5-pro`、`gemini-3.1-pro-preview` 与全部 GPT-5.x/6.x（272000 阈值）用的都是 `<=`，且两个库一致。只有 grok 家族用 `<`。因此这不是全仓库的统一笔误，而是 grok 特有的、已被测试固化的边界语义，不能反向「统一」。

## codex 与本地实测一致的部分

- 算术：codex 独立算出 doubao `0.410958904109589` / `0.205479452054795` / `0.054794520547945`，qwen `0.1` 对应输入 `$0.20/M`、输出倍率 `4`/`12` 对应 `$0.80`/`$2.40`，sonar `0.5`。与本地结果逐项一致。
- `GetBillingExprCopy` 并发：codex 判断该竞争在原 getter 的复制操作中已存在，本次新增的本地副本遍历没有访问共享 map，不是本次引入。本地用 `-race` 实测（旧体与新体各 2 个竞争报告，数量相同）得出同一结论。codex 未运行 `-race`，两者独立同向。
- codex 未把 `GetBillingExprCopy` 的行为变更判为缺陷，与本地判断一致：该函数返回的是运行时实际求值文本，符合其服务上游价格对比页的用途。
- codex 确认 SQL 使用 `::jsonb` / `jsonb_build_object` / dollar quoting，不可直接用于 SQLite/MySQL，但脚本仅声明在 PostgreSQL 上执行，因此在其执行环境下不是缺陷。与本轮「SQL 是补救既有 PostgreSQL 部署，不是迁移步骤」的定性一致。

## 本轮处置清单

| 项 | 处置 |
| --- | --- |
| 第 5 项 LegacyMonth 漏识别 | 已改代码并验证（反控有效），已更正文档 |
| codex P2 Sonar 断言语义 | 已重写测试注释与断言语义，另补 1:5 比例测试 |
| codex P1 SQL guard 范围 | 已执行不回改；脚本归档（`docs/reviews/artifacts/`，该目录被 gitignore）+ 逐条记录局限 |
| codex P2 `UPDATE 0` | 同上，随脚本归档记录 |
| F1 读写竞争 | 已定位与量化；「显然修法」经确定性探针复现出 ABBA 死锁（反控有效），故不改，记为独立任务 |
| 文档失实（第 2 批） | 已更正：grok 边界 `<=`/`<` 的口径矛盾已由源码+正式库+测试三方定论 |
| 文档失实（3 处） | 已更正：正式库 options 已改写、sol 177 行已回算、legacy 识别描述 |

## 库内配置整体自洽性（本轮实测）

`pricing-maintenance.md` 早先几节引用过「全部表达式 smokeTest 编译 0 失败」「无孤立键」的结论。本轮用真实 `config.UpdateConfigFromMap` 同时加载 `billing_setting.billing_mode` 与 `billing_setting.billing_expr`（与运行时 `loadOptionsFromDatabase` 同路径），再用真实 `SmokeTestExpr` 逐个编译，独立复测：

```text
正式库  expr_keys=180 mode_keys=180 compile_failures=0 orphan=0 expr_not_tiered=0
隔离库  expr_keys=121 mode_keys=121 compile_failures=0 orphan=0 expr_not_tiered=0
```

两个库的 mode 与 expr 键集完全一致，无「tiered_expr 但无表达式」的孤立键，也无「有表达式但 mode 不是 tiered_expr」的死键。

过程中先写了一版只加载 `billing_expr`、不加载 `billing_mode` 的探针，它报了 23 个 ORPHAN——那是探针缺陷（`BillingMode` 仍来自源码默认值，列出的是「源码 mode 键减库内 expr 键」的差集），不是库内问题。此处记录以说明为什么该结论必须同时加载两张表才能成立。

## 正式库日志快照全覆盖核验（本轮实测）

最后一层核验：把正式库 `logs.other` 中**全部 distinct `expr_b64` 解码后的快照文本**取出，逐个确认都能被解析路径正确归一到运行时实际计费值。这比只查 `billing_setting.billing_expr` 的当前行更彻底——历史行携带的是**当时**的快照文本，修正工具按行重算时读的就是它们。

```text
distinct 快照数                          25
探测的模型名（取自源码 BillingMode 表）  65
无法解析的快照                            0
```

其中 19 个直接命中某模型当前求值文本或 `upgradeBillingExpr` 的改写规则；其余 6 个是 DeepSeek 的**单档片段**常量（`DeepSeekV4Flash/ProPeakExpr`、`OffPeakExpr`、`LegacyPeak/OffPeakExpr`，长度 46/50/53/57），它们由时间点感知的 `GetBillingExprAt` 返回——正是修正工具调用的访问器（`bin/repair_channel_quota_logs.go` 的 `exprString, ok := billing_setting.GetBillingExprAt(...)`），因此同样正确。

过程中先写了一版用硬编码模型子集的探针，报出 12 个「未识别」；那是探针缺陷（子集漏掉 Gemini/OpenAI 分支），改为从源码 `BillingMode` 表取全部 65 个模型名后降为 6 个，再确认这 6 个属 `GetBillingExprAt` 的合法返回。此处记录以说明该结论为何必须用完整模型集才能成立。

## 未覆盖边界

- codex 未运行任何测试、未连接任何数据库，其关于库内分布的判断均为「无法确认」。本轮补测覆盖了正式库与隔离库。**codex 对 grok 变体键在执行前是否各自含 mode 与表达式的疑问已由备份表定论**：写入前 `pricing_repair_20260924_grok420_options`（expr 179 键 / mode 179 键）显示 `grok-4.20-0309-reasoning`、`grok-4.20-0309-non-reasoning`、`grok-4.20-multi-agent-0309` 三个变体键**都已各自持有** `tiered_expr` + 113 字符表达式，缺失的只有规范键 `grok-4.20-0309`。即写入前变体请求走精确变体键、不依赖规范键，那次写入是补归一化目标的预防性动作，而非修复已发生的错账。正式库 `logs` 中 `grok-4.20-0309%` 记录数为 0，与该判断一致。
- 本轮未向正式服务（13000）发请求，未重建容器。正式库读取全部为 `SELECT`。
- **隔离库的修复只在运行时配置加载路径生效，未改写隔离库 option 行**（与 deepseek-v4-pro 那轮处理方式一致）。即隔离库 `billing_setting.billing_expr` 里那 14 个 flash 键与 12 个 pro 键的库内文本仍是旧快照，靠 `upgradeBillingExpr` 在每次配置加载时归一。
- **F1 死锁已实测复现**（应用 naive fix 后 8 秒超时，移除后通过）。探针为临时文件，已清理，未纳入仓库测试；若日后修 F1，应先把该探针作为回归固化。
- codex 的 P1/P2 脚本健壮性改进（逐键 guard、`UPDATE 0` 断言）未实施，仅记录在归档 README 中；脚本已执行完毕，不回改。
- F1 的竞争未被现有 CI 或发布门禁覆盖（`-race` 不在任何 workflow 与门禁命令中），因此该竞争不会被自动拦下。
- 前端未改动，未运行 `bun run lint` / `bun run build`（本轮无前端改动）。
