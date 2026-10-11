# 内置搜索支持：实施交接报告

> **读者**：接手实施本方案的 agent。本文件是"从哪里开始、按什么顺序、踩哪些坑"的作业单；**完整设计与论证在执行依据文档里，本文件不重复，只指路并标出易错点**。
>
> **权威来源（按优先级）**：
> 1. `docs/architecture/ai-tools-search-support-plan-v2.md`（422 行）——**执行依据**，含 §0 修正表 C1–C10、详细设计与验收。
> 2. `docs/reviews/CR-AI-TOOLS-SEARCH-PLAN-REVIEW-2026-10-10.md`（109 行）——三轮复核审计的结论与证据，含被推翻的判断。
> 3. `docs/architecture/ai-tools-search-support-plan.md`（1040 行）——初稿，**已加"已被取代"横幅，只作调研记录，不得据其实现**；但 v2 的"任务 N"编号沿用它的任务分解，对照表见 v2 §4 头部。
>
> **基线**：分支 `dev`，实施起点 HEAD 应为 `4359f2b43` 或其后（方案文档定稿处）。上游固定引用 `789c97019`。

---

## 0. 一句话任务

让 Claude Code、Codex、Kimi、Grok、omp、dsh 的内置联网搜索经本网关正常工作并纳入按次计费。做法是**网关自建搜索后端（SearXNG）**，分三条通道落地：A=显式 JSON 端点、B=Claude 边车本地仿真、C=既有透传保持不动。详细设计见 v2 §2/§3。

**本方案不吸收**上游 `/v1/alpha/search` 的实现（它是纯渠道透传，见 v2 C1）。

---

## 1. 前置条件（阶段 0）

1. **起 SearXNG**：Docker，`settings.yml` 打开 `search.formats: [html, json]`，暴露 `127.0.0.1:8888`。验收：`curl 'http://127.0.0.1:8888/search?q=test&format=json'` 返回带 `results` 的 JSON。
2. 隔离开发环境（AGENTS §7）：`deploy/compose/dev-isolated.yml` + `deploy/env/dev-isolated.env`，独立容器/PG/Redis/端口/密钥。
3. **本机当前没有任何搜索服务或 API key**（已核实），所以阶段 0 必须先起后端。

---

## 2. 实现顺序与验收

严格按 v2 §5 的阶段表推进。每阶段独立提交、独立验收。

| 阶段 | 交付 | 关键验收 |
|---|---|---|
| 0 | SearXNG 编排 | 见上方 curl |
| 1 | `setting/operation_setting/search.go`、`service/search/`、`service/search_billing.go` | 单测绿：Provider 故障/超时**上抛不吞**、计费公式精确、未知 `provider` 显式失败 |
| 2 | 三组显式端点（`/coding/v1/*`、`/v1/alpha/search`、`/v1/code/sessions/*/worker/*`） | 三协议 200+results；关开关→404 且 **Content-Type 非 text/html**；未登录→401 JSON；**CCR 用 Anthropic 风格 key 不 401** |
| 3 | 边车仿真（`relay/claude_search_sidecar.go`） | golden SSE 逐事件断言 + 真实 Claude Code 实测；**无残留预扣、`use_channel` 不含该渠道、半开探针未被记成功** |
| 4 | 流式搜索计数补齐 + 前端设置页 | 流式计数有账；设置页可存取（亮/暗主题） |
| 5 | 文档 + 集成验收 + 发布 | CR 全绿后升级生产 |

文件清单见 v2 §4（新增/修改两栏，已含 `middleware/auth.go`、测试文件、i18n）。

---

## 3. 必读实现陷阱（每条都有代码出处；忽略即出错账或功能不工作）

### 3.1 计费（P0，最容易出错）

- **边车必须走 `service.SettleBilling(ctx, relayInfo, searchQuota)` 差额结算**，不得手写 `DecreaseUserQuota` 直扣。原因：`controller/relay.go:290-296` 在 `ClaudeHelper`（`:306`）之前已 `PreConsumeBilling`，退款 defer 仅在 `newAPIError != nil`（`:228-238`）。手写直扣会**泄漏整笔模型预扣额度**。`SettleBilling` 已核实为差额结算（`service/billing.go:34-75`）。
- **直扣链会漏三样**：令牌额度（`model.DecreaseTokenQuota`，`service/quota.go:409,444`）、订阅资金源（`:419-425`）、用户 `used_quota` 统计。通道 A 复用 `service.PostConsumeQuota` 或抽共享函数。
- **`groupRatio` 不得只用 `ratio_setting.GetGroupRatio(c.GetString("group"))`**：漏 `GetGroupGroupRatio(userGroup, usingGroup)` 专属倍率与 `auto` 组解析。完整口径在 `relay/helper/price.go:39-66`。
- **额度不足返回 403 + `ErrorCodeInsufficientUserQuota`**，不是 402（全仓无 `StatusPaymentRequired`）。
- **`billing_tool_name` 必须纳入校验钩子**（`model/option.go:688` 相邻处）：键不在工具价格索引且未显式免费则校验失败。否则拼错键名 = 静默永久免费（`GetToolPriceForModel` 查不到返回 0）。
- **计费单位 = 每次实际 `Provider.Search` 调用**：`/v1/alpha/search` 一个请求可含多条 `search_query`，按实际条数计费。

### 3.2 中间件与路由

- **`SearchRateLimit()` 必须紧随 `TokenAuth()`**：`middleware/rate-limit.go:324-327,336-341` 对 `id==0` 直接 401。
- **`x-api-key` 映射只在 path 含 `/v1/messages` 或 `/v1/models` 时生效**（`middleware/auth.go:368-374`）：CCR 端点（`/v1/code/sessions/...`）用 Anthropic 风格 key 会 401，**必须扩展该判断并把 `middleware/auth.go` 计入改动面**。
- **`SetSearchRouter` 必须在 `SetRelayRouter`（`router/main.go:18`）之后、`SetWebRouter`（`:26`）之前调用**：gin 注册时快照 handlers，engine 级中间件只作用于其后注册的路由。
- **`/coding` 前缀必须加入 `router/web-router.go:97-102` 的 `isRelayNotFoundPath`**（并补 `web-router_test.go`）。否则打错路径返回 200 HTML，命中"客户端不校验 Content-Type"的伪成功。**仅对 `FRONTEND_BASE_URL` 为空的 SPA 兜底部署生效**。
- **`RouteTag("relay")` 只是日志标签**（`middleware/logger.go:38-43`），不挂中间件。

### 3.3 边车（通道 B）

- **识别三条件**（v2 §3.4）：`len(tools)==1` 且 type 以 `web_search` 开头 + `tool_choice` type=tool 且 name 匹配 + 单条 user 消息正文以 `Perform a web search for the query:` 开头。**不得依赖 system 提示词**（随版本漂移，见 C3）。
- **1、2 命中而 3 不中**：按已选渠道的 `ConvertClaudeRequest` 是否原样透传分两类——Claude 族（claude/aws/vertex）透传保持现状，其余按 `unmatched_tool_policy`（默认 400）。
- **拦截后**：在 `controller/relay.go:301` 的 `addUsedChannel` **之前**早返回（仓库约定：未发上游不得进 `use_channel`）；若所选渠道正处于半开探针（`state.Probe != nil`），调 `service.ReleaseChannelHealthProbe(c, relayInfo)`。
- **边车会额外消耗用户级模型请求限流**（`/v1/messages` 组挂了 `ModelRequestRateLimit`，`router/relay-router.go:69-73`）：这是接受 `Distribute` 前提的连带代价，不要在文档里声称"边车不消耗模型额度"。
- **SSE 硬约束**：`server_tool_use.id` 与 `web_search_tool_result.tool_use_id` 必须一致；`input_json_delta.partial_json` **必须含字面键 `"query"`**（客户端用正则抠，**不可写 `q`**）；`web_search_result` 建议带可选 `snippet`；不产出 `encrypted_content`；错误帧必须含非空 message。

### 3.4 安全

- **fetch 必须用 `service.GetHttpClient()` + `common.WithSSRFValidation`**（唯一带 `ssrfGuardedTransport` 的客户端），且**父 context 取 `c.Request.Context()` 再 `WithTimeout`**——`RELAY_TIMEOUT` 默认 0，照抄 `service/download.go:67` 的 `context.Background()` 会让 fetch 永不超时。
- **重定向上限沿用框架的 10**（非 5）；未标记 `WithSSRFValidation` 时 `checkRedirect` 不校验目标；`fetch_setting.EnableSSRFProtection == false` 时 fetch 端点应 fail-closed。
- **响应长度上限用 `common.ReadAllWithLimit`**（超限报错）而非 `io.LimitReader`（静默截断）；搜索后端响应体也要加同一上限。
- **请求体 64KB 必须 handler 内实现**（`http.MaxBytesReader`）：引擎级给到 128MB。
- **搜索后端 `endpoint` 不打 SSRF 校验**（可信配置），但须限 scheme 为 http/https、拒绝 userinfo、**错误不回显上游响应体**（Root 级 egress）。

### 3.5 客户端契约（协议细节，错了端点直接不工作）

- **Codex `/v1/alpha/search`**：`commands` 是**按命令名索引的对象**（不是 `[{type,query}]` 数组），`search_query` 字段为 `q`，**单次上限 4**。上游 dto 只定义 `Model/Id/Stream/RawBody`。
- **Kimi**：请求体仅 `{text_query}`（`/coding/v1/search`）与 `{url}`（`/coding/v1/fetch`），**成功必须恰好 HTTP 200**。
- **CCR**：`error_message` 客户端截断 1024 字符；`error_type` 须匹配 `^\w{1,64}$`。

---

## 4. 未验证项（实现时必须抓包校正，不得当作已解决）

1. **CCR 端点契约**：来自二进制 zod 定义，未端到端实测——阶段 2 用真实 Claude Code + CCR 开关抓包校正。
2. **Codex `SearchResponse`**：确为 3 字段（`output`、`results` 已确证，第三字段名未解析）；`results[]` 条目字段名（`snippet`）未确证。阶段 2 用真实 codex 抓包校正。
3. **Kimi 响应 `content`/`icon`/`mime`**：客户端只消费 `title/url/snippet/date/site_name`；多余字段无本地证据，实现时至少产出被消费的 5 个。
4. **omp 边车形态**：无证据表明其边车满足三条件，归未覆盖。
5. **gin 路由启动期是否 panic**：新增 `/v1/alpha/search`、`/v1/code/sessions/:id/worker/*` 与现有 `/v1` 通配符（仅 `POST /models/*path`、`/engines/:model/embeddings`）静态无冲突，但**必须阶段 2 真实启动确认**。
6. **边车模型限流真实用量**：`ModelRequestRateLimit` 默认关闭，启用后每次边车搜索占用一次用户额度——阶段 3 观察确认。

---

## 5. 仓库规则（AGENTS.md 摘要，违反会被打回）

- 全程中文；代码/注释/日志/Markdown **不用 Emoji 或装饰性 Unicode**（`→` 可用，箱线字符不可）。
- 业务代码**禁止直接 `encoding/json`**，统一 `common.Marshal/Unmarshal/...`。
- 客户端 JSON 解析后再转发的可选标量字段，用带 `omitempty` 的**指针类型**保留显式零值。
- 数据库兼容 SQLite/MySQL/PostgreSQL，不写方言 SQL/`AUTO_INCREMENT`。（本方案**无 schema 变更**。）
- **禁止**隐藏回退、静默容错、默认降级、伪成功。Provider 失败必须上抛。
- **每个原子任务单独提交**，提交前 `git diff --name-only` 确认只含任务边界文件。**工作区有大量其他会话的在途改动，绝不 sweep 进提交**（尤其 `docs/reviews/README.md`：多个会话在改同一文件）。
- 声称"完成/通过/可用"前，必须本轮实跑对应命令并读输出；`go test ./controller ./model ./relay/common ./relay/helper ./service` 与（前端）`cd web/default && bun run lint && bun run build`。

---

## 6. 完成判据

- 阶段 0–5 全部验收通过，`docs/reviews/` 有对应 CR 记录（命令、退出码、结论、跳过原因）。
- 三通道端到端实测：Claude Code 边车搜索可用且计费正确、Codex/Kimi/CCR 三端点按协议返回、既有透传路径零回归。
- §4 的 6 项未验证项全部转为已验证或明确记录为"未覆盖"。
- 无残留预扣额度、无多余渠道探针/用量记录、无静默降级路径。
