# 渠道路由优先级、百分比权重与并发容量设计记录

## 1. 文档目标

本文记录截至 2026-07-06 对 `new-api` 渠道路由机制的排查结论、外部评审反馈和后续重构方案。

本次讨论覆盖以下范围：

- 上游 API 可用性与 Cloudflare 403 诊断
- 请求头与 User-Agent 匹配规则的现状
- 原版 `new-api` 与原版 `sub2api` 是否具备请求头规则匹配能力
- 当前渠道优先级与权重机制
- 百分比权重重构方案
- 单渠道最大并发数量控制方案
- 迁移、灰度、回滚和验证计划

用户曾在调试中粘贴过 API key。本文不记录任何密钥、完整凭证或可复用鉴权材料。

## 2. API 与请求头排查结论

### 2.1 上游 API 可用性

测试对象为 OpenAI-compatible 风格的 `https://muyuan.do/v1`。

排查时重点验证过：

- `/v1/models`
- `/v1/responses`
- `/v1/chat/completions`
- 不同 `User-Agent`
- Codex CLI 相关请求头画像
- WebUI 渠道测试路径

核心结论：

- 该服务存在请求头或客户端画像相关的匹配行为。
- 某些请求画像可以通过，某些请求画像会失败。
- WebUI 中出现的 `403` 响应体是 Cloudflare challenge HTML，不是标准 OpenAI-compatible JSON 错误。
- 该类 `403` 更像 Cloudflare/WAF 在 API 业务逻辑之前拦截请求，不能直接等同于 API key 错误、模型不存在或渠道配置错误。
- 不能仅凭一次 `403` 判断 IP 被永久拉黑。更准确的说法是：当前请求画像触发了 Cloudflare 的浏览器挑战或访问控制。

### 2.2 Cloudflare 403 的判断依据

截图中的响应体包含典型 Cloudflare challenge 页面特征：

- HTML 文档响应
- `Just a moment...`
- `challenges.cloudflare.com`
- `Enable JavaScript and cookies to continue`

这说明请求没有到达正常 OpenAI-compatible API 处理层，或者至少没有返回 API 层的 JSON 错误格式。

可操作判断：

- 如果同一 IP 使用浏览器访问也触发 challenge，可能是 IP、ASN、地区或 Cloudflare 规则命中。
- 如果浏览器可访问而脚本请求失败，更可能是 User-Agent、Cookie、TLS 指纹、请求头完整性或 Bot 管控规则命中。
- 如果只有 new-api WebUI 测试失败而 curl/Codex CLI 成功，更可能是 WebUI 侧请求头画像与上游匹配规则不一致。

## 3. 请求头匹配能力现状

### 3.1 原版 new-api

原版 `new-api` 已存在与请求头相关的能力，但不是完整的通用规则引擎。

已存在能力：

- 渠道侧 `header_override`
- 参数覆盖 `param_override`
- 请求头透传
- 渠道亲和或选择逻辑中可基于 `User-Agent` 做包含匹配
- 渠道测试时可配置运行画像或 Header Profile

缺口：

- 没有原生的通用请求头规则匹配 DSL。
- 没有按任意 header 做 equals、contains、regex、exists 的完整匹配矩阵。
- 没有把多条 header 条件组合成渠道命中规则的统一控制面。

因此，当前要实现“符合某个 API 的请求头匹配表现”，需要在现有 header profile 和渠道选择逻辑上扩展，而不是简单打开一个现成开关。

### 3.2 原版 sub2api

原版 `sub2api` 更偏向订阅转换、代理和特定客户端适配。

相关能力：

- 存在 Codex 客户端限制或识别相关逻辑。
- 可针对部分客户端请求画像做特殊处理。

缺口：

- 不是通用 new-api 渠道路由控制面。
- 没有可直接复用到 new-api 的完整 header 匹配规则系统。

### 3.3 建议的请求头规则方向

如果后续实现请求头匹配，建议设计为显式规则，避免静默猜测。

建议规则字段：

- `header_name`
- `operator`: `exists`、`equals`、`contains`、`regex`
- `value`
- `case_sensitive`
- `negate`
- `action`: `allow`、`deny`、`prefer_channel`、`force_header_profile`

关键约束：

- 默认关闭。
- 命中和未命中都需要可观测日志。
- 正则必须设置长度和执行时间上限。
- 不在规则里记录密钥。
- 匹配失败必须显式返回或走正常路由，不能伪造成功。

## 4. 当前优先级与权重机制

### 4.1 当前事实

当前路由相关结构主要涉及：

- `model/ability.go`
- `model/channel.go`
- `model/channel_cache.go`
- `model/channel_route_model.go`
- `controller/relay.go`
- `service/channel_select.go`
- WebUI 渠道表格与渠道编辑抽屉

当前 `Ability` 侧已经存在：

- `Group`
- `Model`
- `ChannelId`
- `Enabled`
- `Priority`
- `Weight`

当前选择流程大体是：

1. 按 group、model 或 route model 找候选渠道。
2. 过滤禁用渠道、自动封禁、请求体大小限制、重试排除等条件。
3. 按 priority 分层。
4. 在同一 priority 层内按 weight 做随机选择。

当前权重的一个重要细节是：旧逻辑使用 `weight + 10` 作为有效票数。

影响：

- `weight = 0` 仍然会有 10 票，不是真正的 0%。
- 低权重渠道不会彻底退出主动选择。
- 管理员看到的 weight 不是直观百分比。

### 4.2 WebUI 中当前权重控制位置

当前可见控制点主要有两个：

- 管理端渠道列表中的 `Weight` 列，可进行 inline 编辑。
- 新增或编辑渠道抽屉中的 Routing Strategy 区域，可设置 Weight。

当前 WebUI 没有按同一范围权重总和为 100 的联动编辑体验。

### 4.3 当前最大值边界

数据库层面 `channels.weight` 和 `abilities.weight` 类字段通常按整数存储。实际可输入上限不应只看数据库类型，还要看：

- 前端输入控件限制
- JSON number 的安全整数范围
- Go 侧 `int` 或 `int64` 转换路径
- 随机选择时总权重累加是否可能溢出

因此，不建议继续暴露“任意大整数权重”。重构后应改为固定范围百分比或 basis points。

## 5. 新设计目标

### 5.1 目标

新设计应满足：

- 渠道按路由优先级分层。
- 同一优先级层内，权重表达该层内流量分配比例。
- 同一权重范围内，总和恒等于 100%。
- 调整一个渠道权重时，同范围其他渠道联动调整。
- 权重语义直观，0% 就是不主动选中。
- 支持某个渠道最大并发数量限制。
- 支持灰度、回滚和影子验证。
- 与现有 retry、auto-ban、渠道亲和、route model 缓存逻辑兼容。

### 5.2 非目标

本轮设计不建议同时做以下事情：

- 不把权重机制和计费倍率合并。
- 不把并发限制做成排队系统。
- 不把请求头匹配、百分比权重、并发限制全部塞进一个不可拆的巨型配置。
- 不改变已有 API token 鉴权主链。
- 不在业务链路中伪造成功或静默降级。

## 6. 路由范围定义

### 6.1 不能只按全局优先级分组

“同一个优先级分层内权重总和等于 100”这个方向是对的，但范围不能定义为全局所有同优先级渠道。

原因：

- 渠道可属于不同 group。
- 渠道支持的 model 不同。
- 同一渠道可能通过 route model 或 compact pool 参与不同模型路由。
- 渠道可能因请求体大小、禁用、自动封禁、重试排除等运行时条件被过滤。

如果只按全局 priority 聚合，会导致互不竞争的渠道互相影响权重。

### 6.2 推荐 Scope Key

建议权重范围定义为静态配置池：

```text
scope = group + effective_route_model + route_priority
```

其中：

- `group` 是用户分组或渠道分组约束。
- `effective_route_model` 是路由实际竞争的模型键。
- `route_priority` 是路由优先级层。

对于 route model、model mapping、compact pool，需要在进入权重计算前归一化出稳定的 `effective_route_model`。

这里的 `group` 必须是最终参与候选选择的具体 group，而不是 `auto` 这类别名。

对于 auto group，请求进入权重 scope 前必须先解析到具体候选 group。跨 group retry 时，每个实际 group 内部独立计算 priority 和 weight，不允许把多个 group 的候选合并成一个 100% 权重池。

`effective_route_model` 必须明确来源：

- 用户请求模型。
- route model 或 compact route model 归一化后的模型。
- 渠道 model mapping 后用于兼容性判断的上游模型。

同一个 scope 内的候选必须具备业务可替代性。如果两个渠道虽然暴露同一请求模型，但实际映射后的上游模型在工具调用、流式、图片、上下文长度、计费或错误格式上不等价，应在进入 scope 前通过兼容性过滤或独立 scope 隔离。

`channel_type` 不建议默认加入 scope，否则会阻断跨 provider 灰度。但它必须参与兼容性判断，确保同一 scope 内候选能满足本次请求需要的协议能力。

Header Profile 不建议默认加入 scope。它应优先作为候选资格过滤条件：

- 如果某个请求画像必须使用特定 Header Profile，则不满足该 profile 的渠道在权重计算前被过滤。
- 如果 Header Profile 只是渠道请求头快照，不影响可替代性，则不进入 scope。
- Header Profile 命中、缺失和过滤原因必须进入 shadow 日志。

### 6.3 静态配置池与运行时可用池

需要区分两个概念：

- 静态配置池：用于编辑和保存权重，总和必须为 100%。
- 运行时可用池：处理单次请求时经过禁用、封禁、重试排除、body limit、`use_channel` 等条件过滤后的候选集合。

运行时如果只剩部分渠道可用，应在剩余渠道内临时归一化权重。

示例：

- 静态配置：A 50%、B 30%、C 20%。
- 运行时 C 被自动封禁。
- 本次请求可用池为 A、B。
- 本次选择比例应临时变成 A 62.5%、B 37.5%。

这不会回写静态配置。

### 6.4 固定候选过滤顺序

新选择器必须固定过滤顺序，避免不同路径实现出不同语义。

推荐顺序：

1. 根据具体 group 和 effective route model 取静态候选。
2. 过滤 ability disabled 和 channel disabled。
3. 过滤 auto-ban 或手动禁用状态。
4. 过滤协议能力、channel type 能力、Header Profile 等兼容性约束。
5. 过滤 request body limit 不满足的渠道。
6. 过滤本次请求已尝试的 `use_channel`。
7. 过滤本次请求内已经因并发满载跳过的渠道。
8. 在剩余候选中取最高可用 `route_priority`。
9. 只在该 priority 层内按 `route_weight_bps` 临时归一并抽样。
10. 如果该层无候选，再进入下一 priority 层。

0% 渠道不应在第 9 步被临时复活。若所有 priority 层都只有 0% 渠道，应返回无可用渠道，除非管理员明确配置了更低 priority 的备用渠道。

## 7. 百分比权重模型

### 7.1 存储单位

建议不要用浮点数存储百分比。

推荐使用 basis points：

```text
10000 bps = 100.00%
1 bps = 0.01%
```

字段建议：

- `route_priority`
- `route_weight_bps`

好处：

- 避免浮点误差。
- 可以精确校验总和等于 10000。
- UI 可以展示为 100.00%。
- 后端随机选择只处理整数。

### 7.2 0% 语义

新机制中，0% 必须表示不主动选择。

如果管理员希望渠道只作为备用，不应设置 0% 后还期望 fallback 自动进入，而应把该渠道放到更低优先级层。

推荐语义：

- 同层 0%：不参与主动随机选择。
- 更低优先级：高优先级层全部不可用后才进入。
- 指定渠道调用：如果明确指定该渠道，可以绕过权重，但仍受 enabled、quota、并发上限等硬约束。

### 7.3 选择算法

选择算法应变为：

1. 找出当前请求的候选渠道。
2. 按 `route_priority` 取最高可用层。
3. 过滤运行时不可用渠道。
4. 移除 `route_weight_bps = 0` 的主动候选。
5. 对剩余候选按 bps 临时归一化。
6. 使用整数随机选择。

如果最高层所有渠道都不可用或权重都为 0，则进入下一优先级层。

## 8. 权重联动调整算法

### 8.1 默认联动模式

用户提出的默认规则是：

```text
调整一个渠道权重时，同一范围内其他渠道平等地一起调整。
```

推荐保留该模式，命名为 `equal_adjust`。

示例：

- 当前 A 40%、B 30%、C 30%。
- 用户把 A 调到 50%。
- 差值为 +10%。
- B、C 平均扣减，各变为 25%。
- 结果 A 50%、B 25%、C 25%。

### 8.2 边界处理

当其他渠道不够扣减时，需要使用 water-filling 算法。

示例：

- 当前 A 40%、B 5%、C 55%。
- 用户把 A 调到 90%。
- 需要从 B、C 扣 50%。
- B 最多扣到 0%，剩余从 C 扣。
- 结果 A 90%、B 0%、C 10%。

约束：

- 单个权重范围是 0 到 10000 bps。
- 同一 scope 总和必须等于 10000 bps。
- 四舍五入产生的余数按稳定顺序分配，例如按 channel id 升序。
- 不能出现负数。

### 8.3 可选比例联动模式

除默认平等联动外，可以保留一个可选模式 `proportional_adjust`。

适用场景：

- 管理员已经调出一组比例。
- 希望调整 A 时，B、C 保持原有相对比例一起变化。

示例：

- 当前 A 50%、B 30%、C 20%。
- 用户把 A 调到 40%。
- 释放 10%。
- B、C 按 30:20 分配。
- 结果 A 40%、B 36%、C 24%。

该模式不应作为初始默认值。默认仍建议使用用户提出的平等联动，直观且容易解释。

### 8.4 新增、删除和移动渠道

新增渠道：

- 默认可从同 scope 其他渠道平均挤出权重。
- 或默认 0%，由管理员手动分配。
- 推荐 WebUI 提供两种明确选项，默认选 0% 更保守。

删除渠道：

- 被删除渠道的权重平均分配给同 scope 剩余渠道。
- 如果没有剩余渠道，则删除 scope。

移动优先级或模型范围：

- 从旧 scope 移出时，旧 scope 重新归一化到 100%。
- 移入新 scope 时，按新增渠道规则处理。

批量编辑：

- 应一次性提交整个 scope 的权重快照。
- 后端在事务里校验总和。

## 9. 最大并发数量控制

### 9.1 设计定位

单渠道最大并发不是权重的一部分，而是运行时容量约束。

权重决定“正常情况下应该分多少流量”。

最大并发决定“此刻还能不能接新的请求”。

两者必须分开，否则会出现配置语义混乱。

### 9.2 字段建议

建议在渠道层增加：

```text
max_concurrency int not null default 0
```

语义：

- `0` 表示不限制。
- `> 0` 表示同一时刻最多允许该渠道存在多少个 in-flight 请求。

后续如果需要按模型细分，可以再扩展到 route scope 级别：

```text
channel_id + group + effective_route_model + max_concurrency
```

第一阶段建议只做渠道级，避免一次引入过多维度。

### 9.3 运行时行为

普通路由请求：

- 如果选中的渠道已达到并发上限，应标记为本次请求已尝试或不可用。
- 然后重新进入选择流程，选择其他可用渠道。
- 如果所有候选渠道都满载，返回明确错误。

建议错误：

```text
HTTP 429
code: channel_concurrency_exceeded
```

指定渠道请求：

- 如果用户明确指定某个渠道，而该渠道已满载，不应静默 fallback 到其他渠道。
- 应返回 429，并说明该渠道达到最大并发。
- 指定渠道入口不经过随机 selector，因此并发校验不能只挂在随机选择器内部。
- 当前指定渠道来自 token path 中的 `specific_channel_id`，进入 distributor 后会直接 `GetChannelById`。这个路径也必须执行并发槽获取。

渠道亲和：

- 非严格亲和：亲和渠道满载时可以 fallback。
- 严格亲和：亲和渠道满载时返回 429。
- 严格亲和的来源必须显式定义，例如用户指定渠道、会话粘滞、Header Profile 绑定或管理员策略。
- 非严格亲和 fallback 时，应记录 `skipped_by_concurrency`，以便解释为什么没有使用亲和渠道。

并发满载不应直接污染 `use_channel` 的“已请求上游”语义。

建议新增本次请求内的独立排除集合：

```text
excluded_due_to_concurrency
```

含义：

- 用于防止同一次选择循环反复抽中满载渠道。
- 不等同于已真正请求过的上游渠道。
- 日志和 shadow mode 应分别展示 `use_channel` 与 `excluded_due_to_concurrency`。

### 9.4 不建议默认排队

不建议第一阶段提供队列。

原因：

- 流式请求可能持续很久。
- 排队会占用客户端连接和服务端 goroutine。
- 排队超时、取消、重试和计费边界会明显复杂化。
- API 网关更适合快速选择其他渠道或快速返回容量错误。

如果未来确实要排队，应作为独立功能设计，并提供：

- 显式开关
- 队列长度上限
- 等待超时
- 客户端取消传播
- 指标和日志

### 9.5 单实例与多实例

单实例部署：

- 可以用进程内 semaphore 或计数器。
- 实现简单，成本低。

多实例部署：

- 必须使用分布式租约，例如 Redis。
- 不能只依赖本地计数器，否则每个实例都会各自放行到上限，整体并发会超配。

建议策略：

- 未启用 Redis 时，只声明为单实例准确。
- 多实例严格模式下，如果 Redis 不可用，应 fail-closed 返回错误，不应静默降级成本地计数。
- 可选提供非严格模式，但必须显式开启并记录告警。

### 9.6 获取与释放时机

并发槽获取时机：

- 在最终选定渠道之后。
- 在真正发起上游请求之前。
- 在 request body limit、disabled、auto-ban、Header Profile 兼容性和 `use_channel` 排除完成之后。
- 指定渠道和渠道亲和路径同样需要获取并发槽。

释放时机：

- `DoRequest` 失败时释放。
- 上游返回非成功且不会进入响应流时释放。
- 非流式响应完成后释放。
- 流式响应必须在 stream 完整结束或客户端断开后释放。
- panic 或异常路径必须通过 defer 释放。

关键点：

- 不能在收到上游响应头后就释放，否则流式长连接会绕过并发限制。
- 不能在 retry 前漏释放。
- 不能让同一次请求重复释放。

Redis 分布式租约还必须满足：

- acquire 必须是原子操作，预检查不能作为准入判决。
- 不能采用 `available_slots > 0` 后再单独写入的两步逻辑。
- 单实例内存计数和 Redis 分布式租约都必须走同一类 check-and-acquire 原子路径。
- 租约有 TTL，进程崩溃后能自动回收。
- 长流式请求需要续租，或者 TTL 必须覆盖最长允许流式时长。
- 续租失败时记录错误，并按严格模式处理后续请求。
- release 必须幂等，重复释放不能导致计数为负。
- Redis 不可用时，启用并发限制的渠道必须显式失败；未启用并发限制的渠道不应依赖 Redis。

获取并发槽后必须立即安排释放：

```text
lease, ok = acquire()
if !ok:
  return 429 or reselect
defer lease.release_once()
```

释放函数必须是幂等的，可用 `sync.Once` 或等价机制保护。

流式请求的 release 仍应等到流结束、客户端断开、上游 EOF、写失败或错误返回之后执行，但 defer 必须在 acquire 成功后立即建立，避免 early return 或 panic 泄漏槽位。

流式续租建议：

- 基础 TTL 不低于 5 秒。
- 续租间隔建议为 TTL 的三分之一。
- 连续续租失败达到阈值后终止请求，释放本地状态，并记录告警。
- Redis key 必须带 TTL，作为网络分区或进程崩溃时的被动回收兜底。

### 9.7 任务型接口边界

任务型接口和同步文本接口不同。

当前任务提交链路通常是：

```text
BuildRequestBody -> DoRequest -> DoResponse -> 返回 task id
```

HTTP 请求完成不代表上游任务已经完成。

第一阶段建议把 `max_concurrency` 定义为“HTTP 提交并发”，而不是“上游任务运行中数量”。

如果后续需要限制上游任务运行中数量，应设计独立的 task runtime quota：

- 获取时机为任务创建成功后。
- 释放时机为轮询到任务完成、失败、取消或超时回收。
- 必须覆盖 Midjourney、视频任务、异步图片任务等非文本链路。

在第一阶段文档和 UI 中必须写清楚：渠道最大并发只保护网关向上游发起请求的 in-flight HTTP 生命周期。

### 9.8 多 key 渠道

多 key 渠道会带来额外容量语义。

第一阶段推荐：

- `max_concurrency` 先按 channel 级别限制。
- multi-key 仍按现有 key 选择逻辑工作。
- 文档明确该限制不保证单个 key 的并发上限。

如果要保护单 key 容量，需要增加 key 级限制：

```text
channel_id + key_index + max_concurrency
```

key 级限制必须与 multi-key auto-disable、manual-disable、polling index 和日志字段一起设计，不能用 channel 级计数替代。

### 9.9 满载重选上限

普通路由遇到满载渠道后可以跳过并重选，但必须有上限。

建议：

- 本次选择最多尝试 `min(scope_size, configured_limit)` 个候选。
- `configured_limit` 可默认等于 5。
- 所有候选都满载时返回 429。
- 日志记录 scope、尝试次数、满载渠道集合和最终错误。

不能无限循环抽样，否则当同层所有渠道都满载时会形成死循环或高 CPU 空转。

## 10. 数据模型建议

### 10.1 权重归属

推荐把路由权重的真实来源放在 `abilities` 或独立 route weight 表，而不是继续只使用 `channels.weight`。

原因：

- 同一渠道在不同 group、model、priority 范围内可能需要不同权重。
- `channels.weight` 是渠道全局属性，表达不了 route scope。

两种方案：

方案 A：扩展 `abilities`

- 增加 `route_weight_bps`
- 增加 `route_priority`
- 复用现有 group、model、channel 关系

优点：

- 与当前 `Ability` 结构贴近。
- 改动较集中。

缺点：

- 如果 `abilities` 当前还承担其他含义，字段语义会变重。

方案 B：新增 route weight 表

示例字段：

- `id`
- `group`
- `effective_route_model`
- `route_priority`
- `channel_id`
- `weight_bps`
- `created_at`
- `updated_at`

优点：

- 语义清晰。
- 更适合后续增加并发、熔断、健康检查等 scope 级配置。

缺点：

- 迁移和查询改动更大。

第一阶段建议优先评估方案 A。如果现有缓存和 compact route model 需要明显改造，再考虑方案 B。

无论选择方案 A 还是方案 B，都必须保证路由字段只有一个真实来源。

新选择器不得继续在任何路径读取 `channels.priority` 或 `channels.weight` 作为路由依据。

必须统一以下路径：

- DB 选择路径。
- cache 选择路径。
- compact route model pool。
- retry priority 分层。
- auto group 选择。
- 渠道亲和 fallback。
- shadow mode 新旧选择器对比。

当前代码里存在混用事实：

- `Ability` 有 `Priority` 和 `Weight`。
- 部分 DB 路径可使用 `ability.Weight`。
- pooled route models 当前可能回退到 `channel.GetWeight()`。
- retry priority 过滤当前按 `channel.GetPriority()`。
- cache 当前只保存 channel id，并按 channel priority/weight 选择。
- 前端 inline 编辑的仍是 channel priority/weight。

因此，迁移任务必须先切断语义：

- `route_priority` 只用于路由层级。
- `route_weight_bps` 只用于同层流量分配。
- `pin_order` 只用于管理端展示排序和置顶。
- 旧 `priority` 和 `weight` 在迁移期只能作为兼容字段或默认模板，不再作为新选择器的最终输入。

### 10.2 渠道全局默认值

`channels.weight` 不建议立即删除。

可调整为：

- 新建 route scope 时的默认模板值。
- 老配置迁移来源。
- 兼容旧 API 的展示字段。

但新选择器不应继续直接使用 `channel.weight + 10` 作为最终路由依据。

### 10.3 缓存结构

当前缓存如果只保存 channel id，再到 channel 读取全局 weight，会无法表达 ability 级权重。

建议缓存候选项结构至少包含：

```text
channel_id
route_priority
route_weight_bps
group
effective_route_model
```

并确保 DB 路径和缓存路径选择结果一致。

缓存候选项还应能表达或快速查询以下过滤依据：

- ability enabled。
- channel status。
- auto-ban 状态。
- request body limit。
- Header Profile 或协议兼容性。
- max concurrency 配置。
- model mapping 或 route model 归一化结果。

缓存失效必须覆盖：

- route priority 或 route weight 修改。
- ability enabled 修改。
- channel status 修改。
- auto-ban 或 multi-key 状态修改。
- request body limit 策略修改。
- Header Profile 策略修改。
- model mapping、models、group 修改。
- max concurrency 修改。

shadow mode 必须记录本次选择使用的是 cache path 还是 DB path。如果两条路径候选集不同，应优先视为实现缺陷，而不是单纯权重差异。

影子读取期间新旧缓存必须隔离。

建议：

- 旧选择器继续使用现有缓存结构。
- 新选择器使用独立 v2 缓存结构和 key 前缀。
- v2 缓存候选项显式保存 route candidate 元数据。
- shadow mode 同时读取 old cache 和 v2 cache，但不让两者共享可变数据结构。
- 切换完成并经过观察期后，再清理旧缓存。

这样可以避免新旧选择器在迁移期互相踩缓存，导致 shadow 差异无法解释。

## 11. WebUI 设计

### 11.1 列表展示

渠道列表建议展示：

- 路由优先级
- 当前 scope 内权重百分比
- 最大并发
- 当前 in-flight 数
- 是否达到并发上限

对于同一 scope 的渠道，应支持按 scope 聚合展示，避免管理员误以为全局 weight 是唯一含义。

### 11.2 编辑体验

建议提供 scope 编辑器：

- 顶部选择 group、model、priority。
- 表格列出同 scope 渠道。
- 权重使用百分比输入或滑块。
- 调整一个渠道时，其余渠道联动调整。
- 始终显示总和 100.00%。
- 保存时提交整个 scope 快照。

错误处理：

- 总和不是 100.00% 时禁止保存。
- 并发上限小于 0 时禁止保存。
- 同 scope 没有任何正权重渠道时，需要明确确认或禁止保存。

当前 inline 单字段保存方式不适合新权重模型。

原因：

- 调整一个渠道会联动其他渠道。
- 同 scope 总和必须恒等于 10000 bps。
- 单字段保存会产生中间态。

因此必须提供 scope 级批量保存接口：

```text
PUT /api/channel-route-scopes/{scope_id}/weights
```

请求体应包含整个 scope 的权重快照，而不是单个渠道字段：

```json
{
  "version": "scope-version-or-hash",
  "items": [
    { "channel_id": 1, "route_weight_bps": 5000 },
    { "channel_id": 2, "route_weight_bps": 3000 },
    { "channel_id": 3, "route_weight_bps": 2000 }
  ]
}
```

服务端要求：

- 在事务内更新整个 scope。
- 校验 sum 等于 10000。
- 校验每项在 0 到 10000 之间。
- 校验 channel 属于该 scope。
- 执行服务端 rounding 或复核前端 rounding。
- 记录审计日志。

并发编辑要求：

- scope 必须有 `version`、`updated_at` 或 hash。
- 两个管理员同时编辑同一 scope 时，后提交者如果基于旧版本，应收到冲突错误。
- 前端收到冲突后刷新 scope，不应静默覆盖。
- scope hash 必须包含成员身份，而不只是权重值。
- 推荐 hash 输入为排序后的 `channel_id + route_weight_bps + item_version` 列表。
- channel 新增、删除、移动 scope 或权重变化都必须改变 hash。

前端展示要求：

- 后端返回整数 `route_weight_bps`。
- 前端展示为百分比。
- 百分比输入转 bps 后仍由服务端最终校验。
- 不用浮点数作为后端真值。

### 11.3 标签和批量编辑

现有渠道页面支持按 tag 聚合，并可批量修改 priority/weight。

新机制下，批量编辑必须改成 route scope 维度：

- 标签行不能只对当前可见子集做 100% 归一。
- 如果标签覆盖多个 scope，必须拆成多个 scope 事务。
- 如果用户只想改部分渠道，应显示会影响同 scope 其他渠道。
- 批量编辑必须记录每个 scope 的旧值、新值和操作者。

### 11.4 双前端同步

当前项目同时存在 `web/default` 和 `web/classic`。

两套前端当前都能编辑 priority/weight。

迁移时必须同步处理：

- `web/default` 的 inline priority/weight。
- `web/default` 的渠道编辑抽屉。
- `web/default` 的标签批量编辑。
- `web/classic` 的 priority/weight 列。
- `web/classic` 的渠道编辑弹窗。

如果只改一套前端，另一套仍可能写旧字段，破坏新路由语义。

### 11.5 并发控制展示

建议展示：

- `max_concurrency`
- `current_in_flight`
- `available_slots`
- 最近一次满载时间
- 满载拒绝次数

这些指标能帮助判断是权重设置不合理，还是上游本身容量不足。

### 11.6 WebUI 信息架构

新 WebUI 不应继续把路由能力塞在渠道表的两个数字列里。

建议把渠道管理拆成三个层次：

1. 渠道列表：负责渠道身份、状态、余额、置顶展示、测试、编辑、禁用等基础运维。
2. 路由 scope 工作台：负责 group、model、route priority、route weight 的联动配置。
3. 容量与并发视图：负责 max concurrency、current in-flight、满载次数、Redis 租约状态等运行时容量观测。

这样做的原因：

- 渠道列表适合快速扫描，不适合承载 scope 内 100% 权重联动。
- 权重编辑需要全量 scope 上下文，不能在单行 inline 输入里完成。
- 并发状态是运行时观测，不应和静态权重混在同一个输入控件里。

建议默认入口：

- 渠道列表保留为第一屏。
- 在渠道列表顶部增加 `Routing Scopes` 入口。
- 单个渠道行的操作菜单增加 `Routing` 操作，打开该渠道参与的 scope 视图。
- 在渠道编辑抽屉中展示路由摘要，但不直接做 scope 权重保存。

### 11.7 渠道列表改动

`web/default` 当前渠道列表由 `ChannelsTable` 和 `useChannelsColumns` 组成，当前存在 `PriorityCell` 和 `WeightCell` inline 编辑。

新机制下建议调整为：

- `priority` 列改名或替换为 `Pin Order`，只表达管理端置顶或排序。
- `weight` 列不再作为可编辑路由字段。
- 新增 `Routing` 摘要列，展示该渠道参与的主要 scope 数量、最高 route priority、主要权重范围。
- 新增 `Capacity` 摘要列，展示 `current_in_flight / max_concurrency`。
- 行操作中增加 `Routing`，进入 scope 工作台并预选该渠道。
- 保留 `Pin to Top` 和 pinned move 操作，但只写 `pin_order` 或兼容期展示字段。

列表中不建议展示过多 scope 明细。超过一个 scope 时只展示摘要，详细配置进入 scope 工作台。

迁移期显示规则：

- 旧选择器模式：可以继续显示 legacy priority/weight，但标记为旧路由字段。
- 新选择器 shadow 模式：显示新 route summary，并保留旧字段只读对比。
- 新选择器正式模式：隐藏旧 weight 编辑入口，旧 priority 不再作为路由字段展示。

### 11.8 路由 Scope 工作台

新增或改造一个 scope 工作台页面。

页面入口：

- 渠道列表顶部按钮。
- 渠道行操作菜单。
- 渠道编辑抽屉中的路由摘要入口。

顶部筛选：

- group。
- model 或 effective route model。
- route priority。
- channel type。
- status。
- Header Profile 或请求画像。

主区域建议采用左侧 scope 列表、右侧详情编辑的结构：

- 左侧：scope 列表，显示 group、model、route priority、候选数、总权重、是否有冲突。
- 右侧：scope 详情表，列出同 scope 下所有候选渠道。

scope 详情表字段：

- channel id。
- channel name。
- channel type。
- status。
- compatibility。
- route priority。
- route weight percent。
- route weight bps。
- max concurrency。
- current in-flight。
- last saturated at。
- skipped reason preview。

空状态和特殊状态：

- 没有任何 scope 时，展示空状态并引导从渠道列表或新增渠道流程创建配置。
- scope 内只有一个候选渠道时，隐藏或禁用 Equal/Proportional 调整模式。
- scope 内所有渠道权重都是 0% 时，显示明确警告，提示该 scope 不会主动接收流量。
- scope 成员为空时，不允许保存权重快照。

编辑控件：

- 权重输入使用百分比输入框，后端真值仍为 bps。
- 调整模式使用 segmented control：`Equal`、`Proportional`。
- 每行可有锁定开关，锁定后不参与联动调整。
- 顶部显示总和：`100.00% / 100.00%`。
- 保存按钮只提交整个 scope 快照。
- 重置按钮恢复服务端当前版本。
- 刷新按钮重新拉取 scope 和 version。

锁定行交互：

- 锁定状态必须有稳定视觉标记。
- 切换 Equal 和 Proportional 时，应提示非锁定渠道会被重新分配。
- 从 Proportional 切到 Equal 时，只均分非锁定行。
- 从 Equal 切到 Proportional 时，保留当前比例作为初始比例。
- 至少需要保留一个可调整行，否则保存前提示无法联动。

保存前校验：

- 总和必须等于 10000 bps。
- 至少一个候选渠道权重大于 0。
- 所有 bps 在 0 到 10000 范围内。
- scope version 未过期。
- 成员集合未变化。

冲突处理：

- 保存时 version 冲突，显示冲突状态。
- 用户只能刷新后重新编辑，不能强行覆盖。
- 冲突信息应展示变更来源、变更时间和当前成员变化。
- 冲突弹窗应展示字段级 diff，例如本地 route weight、远端 route weight、成员变化和 max concurrency 差异。
- 默认不提供强制覆盖。若未来确实需要强制覆盖，必须作为高级操作，并带二次确认和审计日志。
- scope 详情顶部应展示最后编辑者和最后编辑时间。

### 11.9 渠道编辑抽屉改动

`web/default` 当前 `channel-mutate-drawer.tsx` 的 Routing Strategy 直接编辑 `priority` 和 `weight`。

新机制下建议改为：

- `Routing Strategy` 区域改为只读路由摘要。
- 显示该渠道参与的 scope 数量。
- 显示最高 route priority。
- 显示主要 scope 的权重百分比。
- 提供 `Open Routing Scopes` 操作进入 scope 工作台。
- 新增 `Capacity` 区域，编辑 `max_concurrency`。
- `max_concurrency = 0` 展示为 unlimited。
- multi-key 渠道显示 channel-level 限制状态，不暗示 key-level 限制。

新增渠道流程：

- 新建渠道时不直接在抽屉里设置最终 scope 权重。
- 可提供默认 route priority 和默认 weight template。
- 保存渠道后，引导进入 scope 工作台分配权重。
- 如果新增渠道加入某个 scope，默认权重建议为 0%，避免自动挤占生产流量。

编辑 models、group、model mapping 时：

- 前端应提示这会影响 route scope 成员关系。
- 保存后必须刷新 route scope cache。
- 如果某个 scope 成员变化，scope hash 必须变化。

### 11.10 标签和批量操作界面

当前两套前端都支持 tag 聚合行批量修改 priority/weight。

新机制下建议：

- 移除 tag 行上的 route priority 和 route weight inline 编辑。
- 标签批量操作只保留 channel-level 字段，例如 tag、status、remark、max concurrency。
- 如果用户想批量调整路由权重，必须跳转到 scope 工作台。
- 当选择的渠道跨多个 scope 时，界面应拆分显示每个 scope 的影响范围。
- 不允许对当前可见子集做 100% 归一，除非它正好等于 scope 全量成员。

### 11.11 容量与并发界面

并发控制需要静态配置和运行时观测同时可见。

建议在 scope 工作台和渠道详情里展示：

- `max_concurrency`。
- `current_in_flight`。
- `available_slots`。
- `last_saturated_at`。
- `saturated_reject_count`。
- `redis_lease_status`。
- `lease_renew_fail_count`。
- `release_fail_count`。
- `reselect_attempt_count`。

状态语义：

- unlimited：`max_concurrency = 0`。
- available：还有可用槽。
- saturated：达到上限。
- degraded：Redis 不可用或续租失败。

普通路由因为满载跳过某渠道时，日志详情和测试诊断里应能看到 `skipped_by_concurrency`。

容量视图需要保留时间维度：

- 展示最近 5 分钟、15 分钟、60 分钟的 in-flight 趋势。
- 如果后端暂时没有时间序列数据，先预留 `CapacityHistory` 组件位置。
- `last_saturated_at` 同时展示绝对时间和相对时间。
- scope 工作台中容量信息优先作为行展开或右侧详情 tab，不默认单独成页。

这样可以避免三层导航过重，同时保留未来扩展成独立容量视图的空间。

### 11.12 渠道测试弹窗改动

渠道测试弹窗应增加运行配置可见性：

- 当前 Header Profile。
- 当前 route scope。
- route priority。
- route weight。
- max concurrency。
- 是否因并发满载无法测试。

如果测试指定渠道且该渠道满载，应返回明确的 429 信息，不自动 fallback 到其他渠道。

如果测试是模拟普通路由，应展示本次候选过滤链：

- disabled。
- auto-ban。
- request body limit。
- Header Profile 不匹配。
- use channel 排除。
- concurrency 满载。

### 11.13 API 和前端数据模型

建议新增前端 API 封装：

```text
GET /api/channel-route-scopes
GET /api/channel-route-scopes/{scope_id}
PUT /api/channel-route-scopes/{scope_id}/weights
PUT /api/channel-route-scopes/{scope_id}/priority
GET /api/channel-route-scopes/{scope_id}/metrics
```

建议新增类型：

```text
ChannelRouteScope
ChannelRouteScopeItem
ChannelRouteWeightUpdate
ChannelRouteScopeMetrics
ChannelCapacitySnapshot
```

建议新增 query keys：

```text
channelRouteScopes.list(filters)
channelRouteScopes.detail(scopeId)
channelRouteScopes.metrics(scopeId)
channels.capacity(channelId)
```

`updateChannel(id, { priority })` 和 `updateChannel(id, { weight })` 不应继续作为新路由编辑入口。

迁移期前端也要处理服务端拒绝旧字段写入的错误，不能只显示通用失败。

保存响应建议返回新的 scope version。

可选方式：

- 在响应体返回 `scope_version`。
- 或在响应头返回 `X-Scope-Version`。

前端保存成功后应直接使用新 version，避免为获取版本再发一次 detail 请求。

scope version 建议使用单调递增数字或可排序版本，不建议只使用不可排序 UUID。

### 11.14 default 与 classic 的改造边界

`web/default` 是主要体验，建议完整实现 scope 工作台。

`web/classic` 至少需要做到功能安全：

- 移除或禁用 priority/weight 作为新路由字段的 inline 编辑。
- 新增进入 route scope 配置的入口。
- 新增 max concurrency 编辑。
- 对服务端拒绝旧字段写入给出明确提示。
- 保持 tag 批量操作不再写 route priority/route weight。

如果短期不能完整实现 classic scope 工作台，classic 可以跳转到 default 的 scope 工作台，或提供只读摘要加安全拦截。

不能让 classic 继续写旧 priority/weight，因为这会绕过新路由语义。

classic 推荐最小方案：

- 提供只读 scope 摘要表，不实现完整编辑。
- 禁用旧 priority/weight route 编辑。
- 提供打开新版 scope 工作台的入口。
- 如果跳转到 default，应使用新窗口打开，并说明关闭后可回到 classic。

不推荐让 classic 用户在同一页面内进入半成品 scope 编辑器。

### 11.15 WebUI 上线阶段

建议分阶段推进：

1. 安全拦截阶段：前端隐藏或禁用旧 weight 编辑，服务端拒绝新模式下旧字段写入。
2. 只读摘要阶段：渠道列表展示 route summary、capacity summary、shadow 差异提示。
3. scope 工作台阶段：实现 scope 列表、详情、联动编辑和事务保存。
4. 并发观测阶段：展示 current in-flight、满载、Redis 租约和重选指标。
5. classic 兼容阶段：移除 classic 旧字段写入口，补齐跳转或简化 scope 编辑。
6. 清理阶段：移除 legacy priority/weight 路由文案和旧编辑入口。

每个阶段都必须支持 feature flag，便于和后端选择器灰度同步。

### 11.16 文案、i18n 和可访问性

命名建议：

- `pin_order` 展示为 `Pin Order` 或 `置顶顺序`。
- `route_priority` 展示为 `Route Priority` 或 `路由优先级`。
- `route_weight_bps` 展示为 `Route Weight %` 或 `路由权重 %`。
- `max_concurrency` 展示为 `Max Concurrency` 或 `最大并发`。

迁移提示建议：

```text
路由权重和路由优先级已迁移到路由 Scope 工作台。旧权重和旧优先级不再作为新路由字段编辑。
```

i18n 要求：

- 新增 WebUI 字符串必须进入 `web/default/src/i18n` 和 `web/classic` 对应文案体系。
- 至少补齐中文和英文。
- 不能在 scope 工作台、冲突弹窗、容量状态、错误提示里硬编码英文。

可访问性要求：

- 权重输入、锁定开关、调整模式、保存按钮必须支持键盘操作。
- 锁定状态不能只靠颜色表达。
- 容量状态不能只靠颜色表达。
- 冲突弹窗需要明确焦点管理。
- 表格在窄屏下需要可滚动区域和固定关键列。

窄屏策略：

- 低于 1024px 时，scope 列表从左侧栏改为下拉或抽屉。
- 详情表允许横向滚动。
- 固定 channel id、channel name、weight 三个关键列。
- 如果视口过窄，不隐藏保存和冲突提示。

## 12. 迁移方案

### 12.1 权重迁移

旧逻辑有效票数是：

```text
legacy_votes = old_weight + 10
```

迁移时不应直接用 `old_weight`，而应用 `legacy_votes` 归一化。

迁移步骤：

1. 按新 scope 聚合旧候选渠道。
2. 对每个候选计算 `legacy_votes = old_weight + 10`。
3. 计算 `weight_bps = legacy_votes / sum(legacy_votes) * 10000`。
4. 使用 largest remainder 方法分配余数，确保总和正好 10000。
5. 写入新字段。

这样能最大程度保持旧选择概率。

迁移必须处理异常数据：

- 旧 weight 为 0。
- 旧 weight 极大，累加可能溢出。
- ability weight 和 channel weight 不一致。
- scope 内只有一个候选。
- scope 内所有候选都 disabled。
- 归一化后某些渠道得到 0 bps。
- pooled route model 当前使用 channel weight，与普通 DB path 不一致。

迁移 dry-run 报告应至少输出：

- scope key。
- 候选数量。
- 旧 weight 来源。
- ability/channel weight 是否冲突。
- legacy votes 总和。
- 新 bps 总和。
- 0 bps 渠道数量。
- 最大 rounding 误差。
- 是否跳过 disabled-only scope。

### 12.2 灰度与影子模式

建议实现 shadow mode：

- 真实请求仍使用旧选择器。
- 同时用新选择器计算一次候选结果。
- 记录旧选择器和新选择器的差异。
- 聚合观察不同 group、model、priority 的分布差。

影子日志应包含：

- request id
- group
- model
- route scope
- old channel id
- new channel id
- old candidate count
- new candidate count
- skip reason
- old candidate set
- new candidate set
- priority layer
- weight source
- cache path or DB path
- excluded by disabled
- excluded by auto-ban
- excluded by request body limit
- excluded by use_channel
- excluded by concurrency

达到可接受差异后，再通过 feature flag 切换真实选择器。

### 12.3 回滚策略

必须保留：

- 旧字段读取路径
- 新选择器 feature flag
- 迁移脚本幂等性
- 回滚后不丢失旧权重含义的能力

切换失败时应能回到旧 `priority + weight` 逻辑。

### 12.4 部署顺序

推荐迁移顺序：

1. Additive schema：只新增字段和表，不删除旧字段。
2. Backfill：按旧 `weight + 10` 和当前 route scope 回填新权重。
3. Shadow read：旧选择器真实生效，新选择器只记录差异。
4. Dual write 或写入冻结：明确新旧字段在迁移期如何同步。
5. Feature flag 切换：只让新选择器真实生效。
6. 观察期：确认 DB path、cache path、retry、auto group、compact route model 一致。
7. 禁止旧 UI 写入路由字段。
8. 清理旧字段读取路径。

滚动部署风险：

- 新实例可能读 `route_weight_bps`。
- 旧实例仍读 `channel.Weight` 或 `ability.Weight`。
- 如果两个版本同时写配置，会产生分裂。

因此必须选择一种策略：

- 停机或单批发布控制面，避免新旧写路径并存。
- 或在迁移期双写旧字段和新字段。
- 或在切换前冻结 priority/weight 管理端写入。

回滚边界必须提前定义：

- 如果要求无损回滚，需要保留旧字段并持续双写。
- 如果不要求无损回滚，需要明确回滚只回滚代码，不回滚配置语义，并提供恢复旧权重的运维脚本。

每个阶段必须写明回滚口径：

- 回滚触发条件。
- 数据一致性检查项。
- 回滚命令或操作步骤。
- 是否需要重建 channel cache。
- 是否需要清理 v2 cache。
- 预计影响时间。

特别注意：

- backfill 后回滚前，应验证旧字段仍能代表旧选择概率。
- feature flag 切换后回滚，应先关闭新选择器，再重建旧缓存。
- compact route model 的 v2 缓存和旧缓存必须分别清理，避免回滚后仍读到新候选结构。

### 12.5 审计与权限

路由权重、route priority、pin order、max concurrency 都是生产流量控制项。

必须记录审计日志：

- 操作者。
- 操作时间。
- scope。
- 旧值。
- 新值。
- 调整模式。
- 是否由迁移脚本写入。
- 是否来自批量编辑。

迁移脚本必须支持 dry-run，dry-run 不写库，只输出影响范围。

### 12.6 旧字段写入守卫

只迁移前端不够。

进入新路由模式后，服务端 API 必须阻止旧字段继续影响路由。

建议策略：

- feature flag 开启新选择器后，`PUT /api/channel/` 对旧 `priority` 和 `weight` 的写入应被拒绝，返回明确错误。
- 或者只允许写入展示字段和默认模板字段，但不得影响 `route_priority` 与 `route_weight_bps`。
- 拒绝逻辑必须在 `controller/channel.go` 这类 API handler 层实现，不能依赖前端隐藏输入框。
- 旧脚本、classic UI、第三方工具命中旧字段写入时必须有审计日志。

如果迁移期需要双写，应明确：

- 哪个字段是主字段。
- 哪个字段是兼容字段。
- 双写失败时整个事务回滚。
- 何时停止双写。

## 13. 验证计划

### 13.1 后端测试

需要覆盖：

- 同一 scope 权重总和校验。
- 平等联动调整。
- water-filling 边界。
- 比例联动调整。
- 旧权重 `weight + 10` 迁移。
- 运行时过滤后的临时归一化。
- priority fallback。
- `use_channel` 排除后不会重复选择同一渠道。
- 缓存路径和 DB 路径选择一致。
- retry priority 从 route priority 读取，而不是从 channel priority 读取。
- pooled route model 不再从 channel weight 读取路由权重。
- auto group 先解析到具体 group 后再进入 scope。
- Header Profile 或协议能力不满足时在权重计算前过滤。
- 0% 渠道不会在同层被主动选择。
- 高 priority 层全部过滤后进入低 priority 层。
- request body limit 过滤发生在并发获取前。
- auto-ban 过滤发生在权重归一前。

### 13.2 并发测试

需要覆盖：

- `max_concurrency = 0` 不限制。
- `max_concurrency = 1` 时第二个并发请求被跳过或返回 429。
- 普通路由在某渠道满载时选择其他渠道。
- 指定渠道满载时不静默 fallback。
- 流式请求直到流结束才释放槽位。
- 客户端断开时释放槽位。
- retry 路径无泄漏。
- panic 或错误路径无泄漏。
- 普通路由某渠道满载后按同层剩余权重归一重选。
- 满载渠道进入 `excluded_due_to_concurrency`，但不污染 `use_channel`。
- 渠道亲和满载时，非严格亲和 fallback，严格亲和返回 429。
- `DoRequest` 成功但 `DoResponse` 失败时释放槽位。
- Redis 租约 TTL 到期、续租失败和重复 release。
- Redis 不可用时，启用并发限制的渠道显式失败。
- multi-key 渠道按 channel 级限制生效，并明确不覆盖 key 级上限。
- 任务型接口只覆盖 HTTP 提交生命周期，不误判为任务运行期并发。

### 13.3 WebUI 测试

需要覆盖：

- scope 权重编辑总和恒为 100%。
- 调整一个渠道时其他渠道联动。
- 删除渠道后权重重新分配。
- 新增渠道默认 0% 或按用户选择分配。
- 并发上限输入校验。
- 国际化文案。
- scope 级保存一次提交完整权重快照。
- 两个管理员并发保存同一 scope 时返回版本冲突。
- 标签批量编辑跨多个 scope 时拆分事务。
- classic 与 default 两套前端都不能继续把旧 priority/weight 当作路由字段编辑。
- pin order 与 route priority 分离展示。
- scope 空状态、单渠道状态、全 0% 状态展示正确。
- Equal/Proportional 切换和锁定行行为符合预期。
- 版本冲突弹窗展示字段级 diff。
- 保存成功后使用新 scope version。
- 容量趋势插槽或历史图组件不破坏表格布局。
- 窄屏下 scope 列表可切换，详情表可横向滚动。
- 键盘操作、焦点管理、非颜色状态表达符合可访问性要求。

### 13.4 运维验证

需要覆盖：

- 单实例本地计数。
- 多实例 Redis 租约。
- Redis 不可用时严格模式 fail-closed。
- 指标展示和日志可追踪。
- 灰度开关可切换。
- 迁移 dry-run 输出每个 scope 的候选数、旧值、新值和异常项。
- shadow 日志能解释 old/new selector 差异来源。
- 滚动部署期间旧实例和新实例不会同时写出冲突配置。
- 回滚时旧字段仍可用，或运维脚本能恢复旧字段语义。

需要新增或确认的指标：

- concurrency acquire success。
- concurrency acquire fail。
- skipped by concurrency。
- current lease count。
- max concurrency。
- lease ttl。
- lease renew fail。
- release fail。
- strict concurrency 429。
- Redis unavailable for concurrency。
- scope all channels saturated。
- concurrency reselect attempts。
- async task queue depth。

perf metric 与 auto-ban 的关系必须明确：

- 如果 perf metric 只用于事后观测和 auto-ban，则不应在运行时自动改写静态 route weight。
- auto-ban 后只做运行时过滤和临时归一，不回写静态权重。
- 如果未来要做连续错误临时降权，应作为独立功能设计，不能混入本轮百分比权重。

迁移验证还应提供概率分布对比：

- 按 scope 计算迁移前旧选择概率。
- 按 scope 计算迁移后新选择概率。
- 输出差异最大的渠道。
- 对选择概率分布相关性低于阈值的 scope 告警。

## 14. 外部评审记录

### 14.1 OMP 反馈

OMP 给出的主要建议：

- 权重 scope 必须是静态配置池，不能直接按运行时过滤后的集合保存。
- 运行时过滤后应临时重归一化，不回写配置。
- 使用 basis points，例如 10000 表示 100%。
- 保存权重时应以服务端事务校验总和。
- 旧权重要按 `weight + 10` 迁移，避免概率突变。

这些建议与当前设计一致，已纳入本文。

### 14.2 Claude CLI 状态

尝试通过 Claude CLI 获取评审，但命令长时间无有效输出，后续终止。

本设计不依赖该未完成评审作为依据。

### 14.3 agy 状态

尝试指定 Claude 模型时返回额度限制。

尝试 agy 默认模型时返回地区不支持。

因此 agy 没有形成可采纳评审结论。

### 14.4 第二轮 OMP 复核记录

第二轮使用 OMP 做只读架构复核。

OMP 认为方案方向可行，但必须补强以下部分：

- scope 不能只停留在 `group + effective_route_model + route_priority` 的字面定义，必须明确 auto group 解析、model mapping、channel type 能力兼容和 Header Profile 过滤。
- ability 和 channel 两套路由字段当前混用，必须先切断语义，避免新选择器继续读旧 channel priority/weight。
- cache 不能继续只缓存 channel id，必须保存或恢复完整 route candidate 元数据。
- `max_concurrency` 必须覆盖指定渠道、渠道亲和、retry、stream、任务型接口和 multi-key 边界。
- 并发满载应使用独立排除集合，不应直接污染 `use_channel` 的已请求上游语义。
- WebUI 必须从单字段 inline 保存改成 scope 级事务保存，并引入版本冲突处理。
- `web/default` 和 `web/classic` 必须同步迁移，避免旧前端继续写旧字段。
- 灰度方案必须补充 additive schema、backfill、shadow read、dual write 或写入冻结、feature flag 切换和回滚边界。

上述意见已合并到本文第 6、9、10、11、12、13、16、17 节。

### 14.5 第二轮 Claude 复核记录

第二轮重新使用 Claude CLI 非交互复核，不设置预算上限。

Claude 认为方案架构方向正确，但落地前必须补强以下问题：

- 并发槽 acquire 必须原子化，预检查不能作为准入判决。
- acquire 成功后必须立即建立幂等 release，避免 panic、early return、context cancel 导致槽泄漏。
- `specific_channel_id` 指定渠道路径绕过随机 selector，必须单独纳入并发限制。
- 新路由模式下，服务端 API 必须阻止旧 `channel.priority` 和 `channel.weight` 继续作为路由字段写入。
- 迁移方案需要每个阶段的回滚触发条件、检查项、步骤和缓存清理口径。
- 新旧 selector 的 cache 必须版本化隔离，shadow 期间不可共用同一缓存结构。
- scope hash 必须包含成员身份，不能只 hash 权重值。
- 长流式 Redis 租约需要明确 TTL、续租间隔、续租失败处理和被动过期兜底。
- 并发满载重选必须设置上限，防止全满载时死循环。
- Phase 1 不覆盖异步任务运行期是可接受取舍，但必须暴露任务队列深度等指标。

上述意见已合并到本文第 9、10、11、12、13、17 节。

### 14.6 WebUI Claude 复核记录

本轮使用 Claude CLI 对 WebUI 改造规划做只读复核。

Claude 认为当前 WebUI 方向可行，但需要补强以下产品和交互细节：

- scope 工作台需要覆盖空状态、单渠道 scope、全 0% 权重等特殊状态。
- 冲突处理需要字段级 diff，展示本地值、远端值、成员变化、最后编辑者和最后编辑时间。
- 容量视图不能只看瞬时值，应预留最近 5、15、60 分钟趋势展示。
- 容量信息优先作为 scope 工作台内的展开面板或详情 tab，不必第一阶段单独成页。
- classic 推荐采用只读 scope 摘要和安全禁用旧字段编辑，而不是实现半成品编辑器。
- `PUT weights` 保存成功后应返回新 scope version，减少额外 detail 请求。
- 新增文案必须补齐 i18n，不能在 scope 工作台、冲突弹窗和容量状态里硬编码英文。
- scope 工作台需要窄屏策略、键盘操作、焦点管理和非颜色状态表达。
- 锁定行、Equal/Proportional 切换需要明确视觉状态和确认语义。

上述意见已合并到本文第 11、13 节。

### 14.7 WebUI OMP 复核状态

本轮尝试使用 OMP 对 WebUI 改造规划做只读复核。

结果：OMP 在本次 `--max-time=180` 内未返回审查内容，最终 `Deadline exceeded`。

因此本轮没有可采纳的 OMP WebUI 审查意见。本文不把该次 OMP 调用作为设计依据。

## 15. 实施拆分建议

建议按以下原子任务推进：

1. 增加新权重字段和迁移脚本。
2. 实现 scope 权重校验与联动算法。
3. 实现新选择器的纯函数测试。
4. 改造 DB 路径选择。
5. 改造缓存路径选择。
6. 增加 shadow mode。
7. 增加前端 route scope API、types、query keys。
8. `web/default` 渠道列表替换旧 priority/weight 路由编辑入口。
9. `web/default` 新增 route scope 工作台。
10. `web/default` 渠道编辑抽屉改为路由摘要和 max concurrency 编辑。
11. `web/default` 渠道测试弹窗展示 route scope、Header Profile、并发过滤链。
12. `web/classic` 禁用旧 priority/weight 路由编辑并提供 scope 入口。
13. 同步中英文 i18n 文案和静态 key。
14. 增加 `max_concurrency` 字段。
15. 实现单实例并发计数。
16. 实现 Redis 分布式租约。
17. 接入指标、日志和管理端容量展示。
18. 灰度切换并保留回滚开关。

每个任务应单独验证，避免一次性修改整个中继主链。

## 16. 关键风险

### 16.1 优先级字段语义冲突

当前 priority 可能同时承担排序、置顶、路由优先级等含义。

建议新增或明确区分：

- `route_priority`：参与路由选择。
- `pin_order`：仅用于管理端排序或置顶展示。

不要把 UI 置顶语义继续混入路由优先级。

### 16.2 缓存路径与 DB 路径不一致

如果只改 DB 路径，不改缓存路径，生产中可能出现：

- 冷缓存选择正确。
- 热缓存仍使用旧权重。
- compact route model 与普通 route model 行为不同。

因此缓存候选项必须携带 route weight 和 route priority。

### 16.3 并发槽泄漏

流式响应、客户端断开、上游异常和 retry 是最容易泄漏并发槽的路径。

必须用测试覆盖这些路径。

### 16.4 0% 与备用层混淆

管理员可能以为 0% 是备用渠道。

文案需要明确：

- 0% 是同层不主动选。
- 备用应使用更低路由优先级。

### 16.5 多实例误判

如果生产是多实例部署，而只做本地计数，则实际总并发上限会变成：

```text
max_concurrency * instance_count
```

因此多实例必须使用 Redis 或其他分布式租约。

## 17. 推荐最终方案

推荐方案如下：

- 路由按最终具体 group、effective route model 和 route priority 分 scope。
- auto group 必须先解析到具体 group。
- model mapping、channel type 能力、Header Profile 作为候选兼容性过滤条件。
- scope 内使用 `route_weight_bps`，总和固定为 10000。
- 默认联动算法使用平等调整，支持 water-filling。
- 可选提供比例调整模式，但不作为默认。
- 运行时过滤后临时重归一化，不回写配置。
- 0% 表示同层不主动选，备用使用更低 priority。
- 新选择器不得继续读取 `channels.priority` 或 `channels.weight` 作为路由依据。
- `channels.weight` 逐步退化为默认模板和兼容字段。
- `pin_order` 只服务管理端置顶和排序。
- cache 候选项必须包含 route priority、route weight 和过滤所需元数据。
- 新选择器先 shadow mode，再 feature flag 切换。
- 单渠道最大并发使用 `max_concurrency`，0 表示不限。
- 普通路由遇到满载渠道时跳过并重选。
- 指定渠道或严格亲和遇到满载时返回 429。
- 并发满载使用独立的 `excluded_due_to_concurrency`，不直接污染 `use_channel`。
- 多实例并发控制必须使用 Redis 分布式租约。
- 并发槽 acquire 必须原子化，预检查不能作为准入依据。
- 长流式请求需要续租或安全 TTL，release 必须幂等。
- acquire 成功后必须立即建立 defer release。
- 满载重选必须有上限，所有候选满载时返回 429。
- 第一阶段并发限制只保护 HTTP in-flight 生命周期，不覆盖异步任务运行期。
- WebUI 权重保存必须是 scope 级事务保存，并带版本冲突检测。
- scope hash 必须包含成员身份和权重。
- `web/default` 和 `web/classic` 必须同步迁移。
- 迁移按 additive schema、backfill、shadow read、dual write 或写入冻结、feature flag 切换、观察和清理的顺序推进。
- 新旧 selector cache 必须版本化隔离。
- 新路由模式下，服务端 API 必须守卫旧 priority/weight 写入。
- 每个迁移阶段必须有明确回滚步骤和缓存重建口径。

该方案比原版整数权重更直观，也比单纯全局百分比分配更符合 `new-api` 的 group、model、priority 多维路由现实。
