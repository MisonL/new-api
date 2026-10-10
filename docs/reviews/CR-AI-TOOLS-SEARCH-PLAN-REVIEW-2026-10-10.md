# CR-AI-TOOLS-SEARCH-PLAN-REVIEW-2026-10-10

对 `docs/architecture/ai-tools-search-support-plan-v2.md` 的全场景全维度复核审查（第 1 轮）。

- 审查目标：规划文档本身（不是其引用门禁工具，也不是其落地实现——方案尚未落地）。
- 方法：6 路并行只读子代理（代码事实与引用、协议契约、集成可行性、计费链路、安全与 SSRF、内部一致性与覆盖缺口），每条结论由主代理用 `git show` / `git grep` / 二进制定点读取 / 容器与 curl 实测独立复现后才采信。
- 基线：本仓 HEAD `c12e4302e`（分支 dev）；上游固定引用 `789c97019`；本机二进制 `claude.exe`、`codex`（`@openai/codex-darwin-x64`）、`kimi`。本轮命令与探针在 2026-10-10 实跑。
- 结果：规划文档已就地修入本轮确认的错误；本记录留存审计过程与被推翻的子代理结论。

## 1. 被推翻 / 下调的子代理结论（独立复现后）

| 子代理结论 | 复现结果 | 证据 |
|---|---|---|
| 「`c.GetString("group")` 取不到值、group 倍率恒为 1」（一致性路最高严重度） | **推翻**。`ContextKeyUsingGroup` 的常量值就是字符串 `"group"`（`constant/context_key.go:177`），`common.SetContextKey` 实现为 `c.Set(string(key), value)`（`common/gin.go:269-271`），`middleware.TokenAuth` 在 `auth.go:477` 写入正是该键；子代理把 session store 的 `session.Set("group",…)` 与 gin context 的 `c.Set(…)` 混为一谈 | 读 `common/gin.go:269-271`、`middleware/auth.go:477`、`constant/context_key.go:177` |
| 「C4 内嵌转发型 8 个」 | **下调/更正为 9 个**（moonshot/gemini/siliconflow/volcengine/minimax/deepseek/perplexity/ollama/baidu_v2） | 逐个函数体扫描 `relay/channel/**/*.go` |
| 「`use_responses_lite` 属客户端行为」类旁证 | 与 C10 一致，保留 | `git grep use_responses_lite` 仅 docs/前端 |

（其余子代理结论经复现后成立，见下。）

## 2. 已复现并修入文档的问题

### 内容错误
- **C2**：注册表为 **12 条条目**（8 条 `ID: Converter*` + 4 条 `ID: requestConverter*`）；`grep -c "ID:"` = 14 含 2 处注册辅助函数内的 `ID: spec.ID` 赋值。原写"14 条（6+6+2）"错误。证据：`789c97019:relaykit/relayconvert/text_converter_registry.go` 逐条提取分类。
- **C3**：边车 system 提示串是 **ASCII/UTF-8 字面量**（非 UTF-16 串池）；本机 `claude.exe` 已从 v2.1.293 更新到 **v2.1.296**，同串偏移由 81734296/210510179 漂移到 81734412/210510553。字符串出现次数（旧串 0、新串 2）成立。
- **C4**：内嵌转发型为 **9 个**；补全 32 个实现的完整分类（7 该 400 文案 / 6 个 `errors.New("not implemented")`，计 replicate 则 7 / 9 内嵌转发 / 2 真转换 / 其余透传）。
- **C6 证据命令**：`find … -xtype l` 在 macOS BSD find 返回 0，不可复现；等价命令 `find … -type l ! -exec test -e {} \; -print` = 326。结论（326 个断链）成立。
- **§1.1「`/coding/**` 全仓零命中」**：字面不成立——`/coding/` 作为上游 Base URL 出现于 `constant/channel.go:223,227,231,235`；网关**路由**零命中成立。

### 协议契约（Codex 为 P1）
- **Codex `/v1/alpha/search` 的 `commands` 形状错误**：本机 codex 二进制（`codex_api::search`）显示 `commands` 是**按命令名索引的对象**（错误串 `search commands schema must be an object`），`search_query` 数组元素字段为 `q`（可选 `recency`/`domains`），单次 `search_query` 上限 **4**（工具说明 `at most 4 in each call`），非文档所写 `[{type,query}]` 数组、非上限 16。上游 `789c97019:relaykit/dto/alpha_search_request.go` 只有 `Model/Id/Stream/RawBody`，不定义 `commands`，故原"对照上游 dto"无从成立。
- **Kimi 请求体**：本机 kimi 二进制实证客户端只发 `{text_query}`（search）与 `{url}`（fetch），`limit`/`enable_page_crawling`/`timeout_seconds` 均 0 命中；且客户端要求 **HTTP 状态码恰好 200**（`response.status !== 200` 即报错）。文档原列的三字段为客户端不发送的字段。
- **§1.2 CCR「二进制内无字面量」**：`route:"web-search"`/`"web-fetch"` 与路径模板均为二进制字面量，仅连写串命中 0。
- **CCR `error_message` 客户端截断 1024 字符；`maxAttempts=4` 是"最多 4 次尝试（含首次）"**，文档原写"最多 4 次重试"。

### 计费与集成（P0）
- **边车旁路与预扣费/BillingSession 冲突**：`controller/relay.go:290-296` 在 `ClaudeHelper`（`:306`）之前已 `PreConsumeBilling`，退款 defer 仅在 `newAPIError != nil`（`:228-238`），循环结束无补结算。文档 §3.5 的三步直扣链会泄漏模型预扣额度或造成重复扣费。
- **边车仍受 `Distribute` 约束并污染渠道健康探针**：`/v1/messages` 挂 `Distribute`（`router/relay-router.go:94-98`）；`controller/relay.go:178-180` 的 `finalizeRelayChannelHealthProbe(c, relayInfo, newAPIError == nil)` 与 `:301` 的 `addUsedChannel` 无条件执行；`relayRequestProducedNormalEnd`（`:105-113`）对流式返回 `StreamStatus.IsSuccessfulEnd()`，旁路成功会把从未被请求的渠道记为探针成功。
- **CCR 端点 Anthropic 风格 key 会 401，且 §4 漏列 `middleware/auth.go`**：`middleware/auth.go:368-374` 只在 path 含 `/v1/messages` 或 `/v1/models` 时映射 `x-api-key`。
- **直扣链绕过 token 额度/订阅资金源、余额校验非原子**：`model.DecreaseUserQuota` 不碰 `model.DecreaseTokenQuota`（`service/quota.go:409,444`）与订阅源（`:419-425`）。
- **额度不足应为 403 + `ErrorCodeInsufficientUserQuota`**（`service/pre_consume_quota.go:39`、`service/billing_session.go:201,219,244`），全仓无 `StatusPaymentRequired`（0 命中）。
- **`commands` 多命令逐条搜索 vs "一次请求=一次调用"矛盾**：单请求最多 4 次后端搜索只计 1 次。
- **`billing_tool_name` 未纳入校验**：`GetToolPriceForModel` 查不到返回 0（`setting/operation_setting/tools.go:142-163`），拼错键名即静默永久免费，违反 AGENTS §1。
- **`SearchRateLimit` 对 `id==0` 直接 401**（`middleware/rate-limit.go:324-327,336-341`），必须紧挂 `TokenAuth` 之后；"管理员豁免"仅会话管理员，默认关闭（`GLOBAL_RATE_LIMIT_ADMIN_EXEMPT` 默认 false）。
- **64KB 体上限无中间件承载**：引擎级 `DecompressRequestMiddleware` 给到 `constant.MaxRequestBodyMB`（`common/init.go:190` 默认 128），必须 handler 内 `http.MaxBytesReader`/`common.ReadAllWithLimit`。

### 安全
- **fetch 无客户端超时**：`RELAY_TIMEOUT` 默认 0（`common/init.go:120`），`GetHttpClient()` 无 Timeout（`service/http_client.go:383`）；照抄 `service/download.go:67` 的 `context.Background()` 会让 fetch 永不超时。必须用 `service.GetHttpClient()` + `c.Request.Context()` 派生 `WithTimeout`。
- **重定向上限实为 10**（`service/http_client.go:346,366`），非 5；未标记 `WithSSRFValidation` 时 `checkRedirect` 不校验目标（`:345-350`）。
- **`credential_env` "不落库"表述错误**：变量名会随 `search_setting.credential_env` 落库（`model/option.go:238-247`）；落库的是变量名非密钥值，无泄漏。
- **后端响应体无大小上限**；`endpoint` 由 Root 配置、任意 token 触发，等同 Root 级 egress，须限定 scheme、错误信息不回显上游响应体。
- **SSRF 全局开关关闭时未 fail-closed**。

### 一致性与覆盖
- **§3.8 与 §4/初稿任务 8 自相矛盾**（convert.go 文案包装落点）。
- **§4 文件清单不闭合**：缺 `server_tool_usage_test.go`、`middleware/auth.go`、阶段 0 的 SearXNG 编排、前端 i18n/types。
- **§3.2 `unmatched_tool_policy` 作用域（全量非边车）比实际唯一触发点宽**，默认 `reject` 会把真 Claude 渠道可透传请求变 400，与 §2 矩阵冲突。
- **§3.8 热区声明过窄**：`relay/claude_handler.go`（`:55-104` 思考等级渲染，迁移规格 `docs/superpowers/specs/2026-09-30-relaykit-migration-design.md:65,359`）与 `relay/channel/claude/relay-claude.go`（阶段 6 替换的 9 个通道级转换器）都在迁移改动面内。
- **omp 在 §2 矩阵归阶段 3，但 §5 阶段 3 无对应工作项**。
- **`enabled` 与分开关组合语义未定义**；**非流式边车**失败信封未定义；**context cancel 计费归属**未定义。

### 行号偏差（不影响结论）
`relay/claude_handler.go:153`（透传条件在 `:149`；函数体 24-216）、`middleware/auth.go:367-372`（实为 368-374）、`service/convert.go:83-86`（注释实为 84-86）、`service/text_quota.go:106-135`（函数延伸到 174）、`dto/channel_settings.go:83-94`（能力位字段实为 142-155/163-178）。

## 3. 已核实成立、无需改动的项

- C1（上游 `/v1/alpha/search` 纯渠道透传：allowlist 只放行 Sub2API/NewAPI/Codex/AdvancedCustom，`io.Copy` 回写，无搜索引擎调用）；C5（`:1192`/`:1221`）；C8（三个二进制偏移在当前二进制上精确命中）；C10。
- §3.4 通道 B 的 Anthropic 边车 SSE：事件名/字段/index/`server_tool_use.id` 与 `web_search_tool_result.tool_use_id` 关联/`input:{}`/`partial_json` 用 `query` 键，均与官方流协议 + 仓库 dto + 客户端消费三方一致。
- §3.3 / C9 CCR worker 端点请求响应契约、超时与重试（40s / 4 attempts / 75s / 429|5xx）、`error_type` 正则。
- 约 40 处本仓 file:line 引用逐条命中（含 `ssrf_protection.go`、`fetch_setting.go`、`download.go`、`rate-limit.go`、`web-router.go`、`relay_info.go`、`openai_response.go`、`tools.go`、`global.go` 等）。
- 三库兼容（无 schema 变更）、`SearchRateLimit` 并发安全、`Distribute` 无 model 会 400（`middleware/distributor.go:265-268`）、`NoRoute` 优先级无冲突。
- 格式：无 Emoji（`grep -P` 扫 1F300-1FAFF/2600-27BF 等区间零命中）；但 §2 架构图使用 `─┬│` 箱线字符，严格读 AGENTS §3"不使用装饰性 Unicode 符号"存疑（本次保留，待定）。

## 4. 未覆盖（本轮未验证，不得当作已解决）

- 上游 `/v1/alpha/search` 与 Claude Code 二进制契约（C1/C3/C8/C9）依赖前序实测 + 本轮定点读取，未重跑端到端。
- Codex `SearchResponse.results` 条目字段名（`snippet`）未确证；Kimi 响应中 `content`/`icon`/`mime` 客户端不消费、无本地证据。
- 生产容器构建号与 C6 的 326 个断链未复跑。
- 各端点未起服务端到端跑通（方案尚未落地）。

## 5. 第 2 轮：开放项定案与残留清理（2026-10-10）

第 1 轮提出、由本轮定案并写入规划文档：

1. **边车预扣费处置（P0）定案**：采用"复用既有结算链"——旁路通过 `service.SettleBilling(ctx, relayInfo, searchQuota)` 差额结算到搜索实收额，不新增独立直扣；不采用"上移到 `PreConsumeBilling` 之前"（改动面更大）。
2. **边车走 `Distribute` 定案**：接受前提（该 model 必须可路由），"零配置主线"表述撤回，健康探针与 `addUsedChannel` 抑制成为必需。
3. **`unmatched_tool_policy` 作用域定案**：1、2 命中而 3 不中时，按已选渠道的 `ConvertClaudeRequest` 是否原样透传分两类——Claude 族（claude/aws/vertex）透传保持现状，其余按策略处理（默认 400）。
4. **`endpoint` 信任模型定案**：接受"Root 级 egress、不打 SSRF 校验"（与既有渠道 base_url 同级），补偿控制（scheme 限 http/https、拒绝 userinfo、错误不回显上游响应体、`MaskSensitiveInfo`）落为必做项。
5. **`search_billing.go` 收敛定案**：只作编排层，价格解析/配额换算/结算调用既有共享函数；`ComputeToolCallQuota` 死代码不动，去留归 relaykit 迁移阶段决定。
6. **`enabled` 组合语义定案**：总开关优先，`enabled=false` 使全部 `enable_*` 分开关失效。
7. **非流式边车错误信封定案**：失败返回 Anthropic 标准错误信封 `{"type":"error","error":{...}}`（HTTP 502），并把"未经真实客户端验证"写进 §6.9。
8. **初稿已加"已被取代"横幅**：`ai-tools-search-support-plan.md` 头部明确"不得据其实现，执行以 v2 为准"，消除与 v2 定位声明的矛盾。
9. **v2 架构图改纯 ASCII**：原 `─┬│└` 箱线字符（AGENTS §3 装饰性 Unicode 严格读法）改为 `+-|` 框线；`→` 箭头保留——实测 `docs/` 下 186 个已提交文件中 22 个含 `→`，属仓库通行写法，非"装饰性符号"。
10. **任务编号对照表**：v2 §4 头部补初稿任务 8/9/10 与本文件的映射，"执行依据"自此自足。

第 1 轮的行号偏差与内容错误已在第 1 轮提交中修入，本轮不再重复列出。仍保留为**未验证边界**（§4 下文）：Codex `SearchResponse` 第三字段名与 `results[]` 条目字段名、Kimi 响应 `content`/`icon`/`mime`（客户端不消费、无本地证据）、CCR 与各端点的端到端实测（方案未落地）、omp 边车形态。

## 6. 交付状态

- 两轮均为**只读审查 + 文档修正**，未改动任何生产代码。
- 第 1 轮提交 `44776b270`：修入确认错误，登记本记录。
- 第 2 轮：开放项定案 + 残留清理（初稿横幅、ASCII 图、任务对照、非流式错误信封、`enabled` 语义、Codex 第三字段措辞），v2 与本记录一并更新。
