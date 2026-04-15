# 计费表达式系统说明（billingexpr）

## 设计原则

核心原则只有一句话：一条表达式就是一条完整计费规则。

表达式本身直接决定：

- 输入与输出价格
- 阶梯条件
- 缓存、图片、音频等差异化计费
- 请求条件附加倍率
- 时间相关倍率

系统只负责严格执行，不额外引入隐藏倍率、隐式换算或散落在别处的价格逻辑。

## 核心约束

### 1. 表达式自包含

- 一条表达式必须能完整描述一个模型的计费逻辑
- 不依赖隐藏表、隐式补全倍率或约定俗成的特殊换算
- 相同输入、相同请求上下文，必须得到相同结果

### 2. 变量按需启用

基础变量：

- `p`：输入 token
- `c`：输出 token

可选细分变量：

- `cr`：缓存命中读取
- `cc`：缓存创建
- `cc1h`：1 小时缓存创建
- `img`：图片输入
- `img_o`：图片输出
- `ai`：音频输入
- `ao`：音频输出

如果表达式没有单独使用某个细分变量，对应 token 会继续留在 `p` / `c` 中按基础价格计费。

### 3. 价格就是实际价格

- 表达式系数使用供应商真实的每百万 token 价格
- 不引入 `/2`、倍率表换算等历史约定
- 例如 `p * 2.5` 就表示输入每百万 token 价格为 2.5 美元

### 4. 与上游格式解耦

表达式本身不需要关心上游是 OpenAI 风格还是 Claude 风格。

系统会在运行时做 token 归一化：

- OpenAI / GPT 风格：`prompt_tokens` 可能包含缓存、图片、音频
- Claude 风格：`input_tokens` 通常只包含纯文本

### 5. 支持版本化

- 支持 `v1:` 前缀
- 不写前缀时默认按 `v1` 处理
- 版本控制编译环境、token 归一化规则和额度换算方式

## 变量说明

### 输入侧

| 变量 | 说明 |
| --- | --- |
| `p` | 输入 token，自动排除表达式中已单独计价的子类别 |
| `cr` | 缓存命中读取 token |
| `cc` | 缓存创建 token |
| `cc1h` | 1 小时缓存创建 token |
| `img` | 图片输入 token |
| `ai` | 音频输入 token |

### 输出侧

| 变量 | 说明 |
| --- | --- |
| `c` | 输出 token，自动排除表达式中已单独计价的子类别 |
| `img_o` | 图片输出 token |
| `ao` | 音频输出 token |

## `p` / `c` 自动排除规则

`p` 和 `c` 是兜底变量，表示“没有被单独拿出来计价的剩余 token”。

规则如下：

- 表达式用了某个子类别变量，该子类别 token 就从 `p` 或 `c` 中扣除
- 表达式没用该变量，对应 token 就继续留在 `p` 或 `c` 中按基础价格计费

示例：

| 表达式 | `p` 的含义 |
| --- | --- |
| `p * 3 + c * 15` | 所有输入 token 都在 `p` 中 |
| `p * 3 + c * 15 + cr * 0.3` | 缓存读取 token 从 `p` 中扣除，单独按 `cr` 计费 |
| `p * 3 + c * 15 + cr * 0.3 + img * 2` | 缓存读取和图片输入都从 `p` 中扣除，各自单独计费 |

注意：

- OpenAI / GPT 风格需要做这类扣减
- Claude 风格输入本身更接近纯文本，通常不需要额外扣减

## 内置函数

| 函数 | 作用 |
| --- | --- |
| `tier(name, value)` | 标记命中的阶梯，并返回当前阶梯价格 |
| `param(path)` | 读取请求体中的 JSON 路径 |
| `header(key)` | 读取请求头 |
| `has(source, substr)` | 子串判断 |
| `hour(tz)` / `minute(tz)` / `weekday(tz)` / `month(tz)` / `day(tz)` | 读取时区相关时间信息 |
| `max` / `min` / `abs` / `ceil` / `floor` | 数学函数 |

## 表达式示例

```txt
tier("base", p * 2.5 + c * 15 + cr * 0.25)

p <= 200000
  ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)
  : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12)

tier("base", p * 2 + c * 8 + img * 2.5)

tier("base", p * 0.43 + c * 3.06 + img * 0.78 + ai * 3.81 + ao * 15.11)
```

## 请求条件规则

请求条件规则通过 `|||` 拼接在主表达式后：

```txt
tier("base", p * 5 + c * 25)|||when(header("anthropic-beta") has "fast-mode") * 6
```

主表达式与请求条件规则由不同逻辑解析，但最终共同参与计费。

## 系统链路

整体链路如下：

```txt
前端编辑器 -> 存储 -> 预扣费 -> 结算 -> 日志展示
```

### 1. 前端编辑

文件：`web/src/pages/Setting/Ratio/components/TieredPricingEditor.jsx`

- 可视化模式：填写变量价格和阶梯条件，自动生成表达式
- 原始模式：直接编辑表达式字符串

最终写入时，会把计费表达式和请求条件规则组合后一起保存。

### 2. 存储

文件：`setting/billing_setting/tiered_billing.go`

主要存储两类映射：

- `ModelBillingMode`
- `ModelBillingExpr`

保存时必须完成：

- 编译校验
- 样例 token 烟雾测试
- 非负结果检查

### 3. 预扣费

文件：`relay/helper/price.go`

流程：

1. 读取模型表达式
2. 构造请求上下文，供 `param()`、`header()` 使用
3. 用预估 token 执行表达式
4. 将原始价格换算为内部额度
5. 冻结 `BillingSnapshot`，供后续结算使用

### 4. 实际结算

文件：

- `service/tiered_settle.go`
- `pkg/billingexpr/settle.go`

流程：

1. 根据真实用量构建结算参数
2. 按表达式实际用到的变量做 token 归一化
3. 基于预扣费时冻结的快照再次执行表达式
4. 完成额度换算并返回真实扣费值

### 5. 日志展示

相关文件：

- `service/log_info_generate.go`
- `web/src/helpers/render.jsx`

后端会把表达式、命中的阶梯和计费模式写入日志 `other` 字段，前端再解码并展示价格拆解。

## 关键设计决定

### AST 识别已使用变量

系统会在表达式编译后识别其实际引用了哪些变量，再决定是否从 `p` / `c` 中扣除子类别 token。

这保证了：

- 不重复计费
- 不需要作者手动理解不同上游的 token 统计差异
- 编译缓存后不会引入明显运行时额外开销

### 额度换算

表达式产出是每百万 token 的原始价格，换算内部额度时使用：

```txt
quota = exprOutput / 1000000 * QuotaPerUnit * groupRatio
```

### 版本前缀

表达式支持带版本前缀，例如：

```txt
v1:tier("base", p * 2.5 + c * 15)
```

版本前缀用于约束未来演进时的兼容行为，避免新规则破坏旧表达式。

This enables future evolution without breaking existing expressions.

---

## File Map

| Layer | Files |
|-------|-------|
| Expression engine | `pkg/billingexpr/compile.go`, `run.go`, `settle.go`, `round.go`, `types.go` |
| Storage | `setting/billing_setting/tiered_billing.go` |
| Pre-consume | `relay/helper/price.go`, `relay/helper/billing_expr_request.go` |
| Settlement | `service/tiered_settle.go`, `service/quota.go` |
| Log injection | `service/log_info_generate.go` |
| Frontend editor | `web/src/pages/Setting/Ratio/components/TieredPricingEditor.jsx` |
| Frontend display | `web/src/helpers/render.jsx`, `web/src/helpers/utils.jsx` |
| Model detail | `web/src/components/table/model-pricing/modal/components/DynamicPricingBreakdown.jsx` |
| Log display | `web/src/hooks/usage-logs/useUsageLogsData.jsx`, `web/src/components/table/usage-logs/UsageLogsColumnDefs.jsx` |
