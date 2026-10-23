# AI 工具内置搜索支持：架构设计与实施方案

> **已被取代（2026-10-10）**：本文件已被 `ai-tools-search-support-plan-v2.md` 取代，后者是核实修正版并修正了本文件的多处错误结论（含 Codex `commands` 形状、`parseClaudeToolsForOpenAI` 行号、边车 system 提示词、渠道拒绝数量等）。**本文件仅作为调研记录保留，不得据其实现**；执行时以 v2 为准，本文件的"任务分解"编号被 v2 引用。

> **面向执行者**：本文件是可执行计划。按「任务分解」逐条推进，每个任务独立提交、独立验收、可独立回滚。第 1-3 节内联了全部已核实的现状事实与外部协议结论，实现时直接引用，无需回查外部资料。
>
> **执行方式**：本文件落盘在 `docs/architecture/`（仓库架构方案目录，与 `enterprise-auth-three-phase-plan.md`、`codex-bridge-interactive-cli-plan.md` 同级）。

**目标**：让 Claude Code、Codex、Kimi、Grok、omp、dsh 等 AI 工具的内置联网搜索经本网关（new-api）正常工作，并纳入既有工具按次计费体系。

**架构**：新增 `service/search` 搜索后端抽象（Provider 接口 + SearXNG 实现 + 可插拔扩展）；三条落地通道：一是显式 JSON 端点（Kimi `/coding/v1/search|fetch`、Claude Code CCR 代理 `/v1/code/sessions/{id}/worker/web-search|fetch`、Codex standalone `/v1/alpha/search`）；二是 Claude Code 边车请求本地仿真（模型无关，不调用上游模型，直接回吐 Anthropic SSE 搜索事件）；三是既有透传路径保持不变（真 Claude 上游本来就支持服务端搜索）。计费复用 `tool_price_setting` 与 `text_quota.go` 的既有换算公式，按次扣费。

**技术栈**：Go 1.27.3、Gin、GORM v2（SQLite/MySQL/PostgreSQL 三库兼容）；前端 `web/default` React 19 + Rsbuild，包管理器 `bun`。

**依据**：本文件第 1-3 节。外部调研（7 份分报告、参考会话 `ab3925ce-0458-4ad8-b329-c2876526d79d`）为临时产物，关键结论已全部内联；其中「已验证」为逐条代码/命令/容器核实过的结论，「引用」为前序会话实测结论，实现时按标注区别对待。

---

## 全局约束

每个任务都隐含适用，不再逐条重复：

- Go 1.27.3、Gin、GORM v2；不得写只适配单一数据库的 SQL 或迁移语句。
- 业务代码禁止直接调用 `encoding/json`，编解码一律走 `common.Marshal`、`common.Unmarshal`、`common.UnmarshalJsonStr`、`common.GetJsonType`（AGENTS.md 第 3 节）。
- 禁止静默回退、隐藏容错、mock 成功路径；失败必须显式返回错误并被测试覆盖（AGENTS.md 第 1 节）。
- 代码、注释、日志字符串与 Markdown 文档不使用 Emoji 或装饰性 Unicode 符号。
- 新增设置项经 `config.GlobalConfig.Register` 注册；**字段名不得以 `Key`、`Secret`、`api_key` 结尾**——`controller/option.go:80-86` 会把这类键从 `GetOptions` 响应中过滤掉，前端将读不回（已验证）。
- 新增路由必须注册在 web SPA 兜底 `NoRoute` 生效前；无模型端点不得挂 `middleware.Distribute()`（`middleware/distributor.go:264-268` 无 model 时返回 400）。
- 计费换算基准（已验证）：`common.QuotaPerUnit = 500 * 1000.0 = 500000`（`common/constants.go:44`）。价格单位是「美元/1000 次调用」。
- 测试命令：后端 `go test ./controller ./model ./relay/common ./service`；前端 `cd web/default && bun run lint && bun run build`。不运行与任务无关的全量测试。
- 工作区存在并行会话的未提交改动，**不得回退、覆盖、整理与本任务无关的文件**；提交前 `git diff --name-only` 核对范围。

---

## 需求理解与范围

### 做

1. 网关自建搜索后端接入层（Provider 抽象，首个实现 SearXNG 自建，可扩展 Tavily/Brave 等）。
2. 三组显式搜索端点，覆盖 Kimi CLI、Codex standalone、Claude Code（CCR 代理模式）三种客户端协议。
3. Claude Code 默认路径（Anthropic 边车请求 `web_search_20250305`）的网关本地仿真，使 Claude Code 无需任何客户端改动即可联网搜索。
4. 搜索调用纳入按次计费，复用既有工具定价体系；修复 Claude 流式搜索不计费的既有缺口。
5. 配置面（管理员设置项）与前端设置页。
6. 每个受支持工具的客户端配置指引与本仓库文档更新。

### 不做

1. 不实现 Responses 协议的 `web_search` 完整仿真（Grok/Codex hosted 路径）——列入阶段 6 的可选后续，理由见第 3.7 节。
2. 不改动既有透传路径的行为（真 Claude 上游、OpenAI/Azure 原生搜索透传保持现状）。
3. 不引入外部搜索 SDK；HTTP 客户端用标准库。
4. 不修改 `service/convert.go` 的协议转换主体（该文件位于 relaykit 迁移热区，见第 3.7 节）；对它的改动限于一处明确的策略分支。
5. 不实现 MCP 搜索服务器（作为兜底方案写入文档，不在代码内实现）。

---

## 1. 现状事实（全部已核实）

### 1.1 网关侧现状

| 事实 | 证据 |
|---|---|
| 无任何自建搜索端点：`coding/v1`、`alpha/search`、`code/sessions` 全仓零命中 | `git grep`（已验证） |
| Claude `/v1/messages` 入口三分支：Responses 转换（全局策略默认关）/ 透传 body / adaptor 转换 | `router/relay-router.go:101`、`relay/claude_handler.go:25,131-161`、`setting/model_setting/global.go:150-152` |
| 带 `web_search_20250305` 的 Claude 请求：Anthropic 系渠道（claude/aws/vertex 等）**原样透传** | `relay/channel/claude/adaptor.go:28-30`（`return request, nil`） |
| 非 Claude 形状渠道（baidu/tencent 等约 16 个）**显式 400** | `relay/channel/baidu/adaptor.go:27-29` 等 |
| 转 OpenAI 的四个调用点经 `parseClaudeToolsForOpenAI` **显式 400**（`type != "custom"` 即拒） | `service/convert.go:1178,1206-1207`；调用点 `:87`、`relay/claude_handler.go:134`、`openai/adaptor.go:65`、`ali/adaptor.go:75`、`aws/converse.go:113` |
| `ProcessTools` 仅用于 token 估算，不影响转发 | `dto/claude.go:343`（唯一非测试调用点） |
| 生产实例 127.0.0.1:13000 跑旧构建（2026-09-08 镜像 `4527d7666-litefix2`，代码日期 2026-07-17），旧代码用 `Any2Type[[]dto.Tool]` 丢弃 error → **200 静默降级**；拒绝逻辑 2026-09-14 `a2724e3ff` 才引入 | `docker exec new-api /new-api --build-info`、`git show 4527d7666:service/convert.go`（已验证） |
| 工具定价体系完整：默认价 `web_search` 10.0、`web_search_preview` 10.0（gpt-4o*/gpt-4.1* 前缀 25.0）、`file_search` 2.5、`google_search` 14.0（美元/1000 次），支持 `tool:model*` 前缀覆盖与热更新 | `setting/operation_setting/tools.go:25-37,142-163`、`model/option.go:688,748` |
| 真实结算链：`PostTextConsumeQuota` → `calculateTextToolCallSurcharge`，与 token 费叠加；换算 `price/1000*count*QuotaPerUnit*groupRatio` | `service/text_quota.go:106,358`（已验证）；1 次 web_search = 5000 quota = $0.01 |
| `service/tool_billing.go` 的 `ComputeToolCallQuota` 全仓零调用（死代码）；legacy `GetXxxPricePerThousand` 仅测试调用 | 已验证 |
| Claude 搜索计数仅在**非流式**路径写入（流式完全不计） | 写入点 `relay/channel/claude/relay-claude.go:1379`，读取点 `service/text_quota.go:131`（已验证） |
| 存在可复用的 SSRF 防护框架：私网/保留段拦截、域名/IP 白黑名单、DNS pin 防 rebinding、`FetchSetting` 已注册 | `common/ssrf_protection.go:377-568`、`setting/system_setting/fetch_setting.go:5-17` |
| 存在可复用的搜索限流中间件（按用户、env 可配、管理员豁免） | `middleware/rate-limit.go:350-357`、`common/init.go:145-147` |
| 未注册路径行为：`/v1/*` → 404 JSON；`/coding/*` → 200 SPA HTML（实测 3001） | `router/web-router.go:98-115`、`controller/relay.go:3558` |
| 鉴权上下文键：`id`（用户 ID）、`group`、`token_id`、`token_key`、`token_name`、`username` | `middleware/auth.go:224-229,344-346,493-494` |
| 额度与日志助手：`model.GetUserQuota(id, fromDB)`、`model.DecreaseUserQuota(id, quota, db)`、`model.RecordConsumeLog(c, userId, params)` | `model/user.go:816,946`、`model/log.go:424-439` |
| ID 生成：`common.GetRandomString(n)`、`common.GetUUID()` | `common/str.go:118`、`common/utils.go:219` |

### 1.2 客户端协议事实（AI 工具侧）

**Claude Code 2.1.293**（本机二进制 `~/.nvm/versions/node/v24.19.0/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`）：

- WebSearch 不在主对话 `tools` 里，而是客户端另发的**边车请求**：`POST /v1/messages?beta=true`，`{type:"web_search_20250305", name:"web_search", max_uses:8}` 是唯一的工具，`tool_choice:{type:"tool",name:"web_search"}` 强制调用，system 固定为 `"You are an assistant for performing a web search tool use"`，单条 user 消息正文为 `"Perform a web search for the query: <q>"`，`thinking:{type:"disabled"}`，`enablePromptCaching:false`（引用，前序会话实测）。
- 客户端只消费三类搜索事件：`content_block_start`（`server_tool_use` 记 tool_use id；`web_search_tool_result` 收结果数组）、`content_block_delta`（`input_json_delta` 增量里用正则抠 query）。SSE 事件类型白名单共 21 类（本机 strings 复核）。
- CCR 代理开关与端点真实存在：`CLAUDE_CODE_WEBSEARCH_USE_CCR_PROXY`、`CLAUDE_CODE_WEBFETCH_USE_CCR_PROXY`，端点 `POST {base}/v1/code/sessions/{sid}/worker/web-search|web-fetch`（本机二进制 strings 复核：`/v1/code/sessions/` 出现 50 次、开关 3 次）。
- 自定义 `ANTHROPIC_BASE_URL` 不禁用 WebSearch；本机 `~/.claude/settings.json` 已 allow `WebSearch`，未设 `skipWebFetchPreflight`（引用）。
- LLM 网关协议要求：`content-type: text/event-stream`、逐事件转发不缓冲、不能在 block 中途断流、回显上游错误体原文（引用，官方 llm-gateway-protocol 文档）。

**Codex CLI 0.160.0**：

- hosted 路径：Responses API 顶层 `tools` 一项 `{"type":"web_search","external_web_access":bool,...}`（cached→false，live→true）；**`use_responses_lite` 模型直接跳过全部 hosted 工具**，本机默认模型 `gpt-6-luna` 是 lite，所以当前实际不发（引用 + 本机 config 复核：`~/.codex/config.toml` 无任何 search 键）。
- standalone 路径：客户端自己 `POST {base}/alpha/search`，请求 `{id, model, reasoning?, input?, commands?, settings?, max_output_tokens?}`，响应 `{encrypted_output?, output, results?}`；需 provider `supports_standalone_web_search = true` + feature（引用）。

**Kimi CLI 0.60**：

- `services.moonshot_search`（`POST {base}/coding/v1/search`）：请求 `{text_query, limit(1-20), enable_page_crawling, timeout_seconds}`，响应 `{search_results:[{site_name,title,url,snippet,content,date,icon,mime}]}`（引用，源码协议）。
- `services.moonshot_fetch`（`POST {base}/coding/v1/fetch`）：请求 `{url}`，响应为纯文本 markdown（引用）。
- 本机现状：两个 base_url 仍指向 `https://api.kimi.com`，api_key 已被换成 new-api token（51 字符）——推断会 401，需在配置指引中说明。

**Grok / omp / dsh**：

- Grok：内置目录默认 `web_search = grok-4.6`（`supports_backend_search: true`），自定义模型需 `[model.*]` 条目才生效（本机二进制 strings 复核）。
- omp：无搜索配置；有 grounded provider（读 `ANTHROPIC_SEARCH_*`）与 `TAVILY_API_KEY` 两条现成路径（本机 config 复核）。
- dsh：`~/.dsh/profiles/node_modules/*` 下约 100+ 断链符号链接（目标 `/Volumes/Work/code/deepseek-harness` 已不存在），先修断链再谈搜索（本机复核）。

### 1.3 上游与迁移背景

- 上游 PR #7690（`acb347a19`，2026-10-07，52 文件 +1530/-260）做的是「按渠道编码厂商搜索参数 + 厂商上报搜索计数计费」，仍依赖上游厂商的搜索能力，**不含网关自建搜索**。本 fork 无 `relaykit/` 目录（`ls-tree HEAD relaykit` 为空）、无 `relay/common/tool_usage.go`，原样合并不可行（已验证）。
- 本 fork 的 relaykit 迁移规格（`docs/superpowers/specs/2026-09-30-relaykit-migration-design.md:66`）已决定 hosted 工具转换采用上游 `toolconv`（含 `KindWebSearch`），但阶段 1 未落地。**本方案的代码不得放在将随迁移搬迁的转换热区**（`service/convert.go` 的主体、`relay/common/` 的协议转换件）。

---

## 2. 总体架构

```
AI 工具客户端
  Claude Code ──┬─ 默认路径: /v1/messages (边车 web_search_20250305)
                └─ CCR 模式: /v1/code/sessions/{id}/worker/web-search|fetch
  Codex       ──┬─ standalone: /v1/alpha/search        (hosted 路径本次不做)
  Kimi        ──── /coding/v1/search | /coding/v1/fetch
  omp         ──── /v1/messages (同 Claude Code 边车) 或 直连 TAVILY(不经网关)
  Grok        ──── /v1/responses (hosted，本次不做；standalone 不适用)
  dsh         ──── 待断链修复后评估

                       │
        ┌──────────────┴───────────────┐
        │  通道 A：显式 JSON 端点层       │  通道 B：协议仿真层（旁路在既有 relay 入口内）
        │  controller/search_*.go        │  relay/claude_search_sidecar.go
        │  · 鉴权 TokenAuth              │  · 识别 Claude Code 边车请求
        │  · 限流 SearchRateLimit        │  · 本地执行搜索，不调用上游模型
        │  · 计费 按次                   │  · 回吐 Anthropic SSE 搜索事件
        │  · SSRF 复用 common 框架       │  · 计费 按次
        └──────────────┬───────────────┘
                       │
              service/search（新增）
              · Provider 接口（Search / Fetch）
              · SearxngProvider（首个实现，可插拔扩展）
              · 配置读取、超时、结果裁剪、域名过滤
                       │
                  SearXNG（自建，无 key）
```

**请求路径矩阵**：

| 客户端 | 触发路径 | 网关处理 | 阶段 |
|---|---|---|---|
| Claude Code（默认） | `/v1/messages` 边车 | 本地仿真 SSE，不调上游 | 3 |
| Claude Code（CCR 模式） | `/v1/code/sessions/{id}/worker/web-search|fetch` | 显式端点 | 2 |
| Codex（standalone） | `/v1/alpha/search` | 显式端点 | 2 |
| Codex（hosted，非 lite 模型） | `/v1/responses` `tools:[web_search]` | 本次不改（透传或 400 由渠道能力决定） | 6 可选 |
| Kimi | `/coding/v1/search|fetch` | 显式端点 | 2 |
| omp | `/v1/messages`（同边车）或 TAVILY 直连 | 复用通道 B / 不涉及网关 | 3 |
| Grok | `/v1/responses`（hosted） | 本次不改 | 6 可选 |
| 通用 OpenAI SDK | `/v1/chat/completions` `web_search_options` | 维持现状（openai/azure 透传、claude 转换、其余 400/丢弃） | 不改动 |
| 通用 Anthropic SDK | `/v1/messages` 内置搜索工具 | 真 Claude 渠道透传（现状）；非 Claude 渠道按策略（默认 400） | 不改动，策略见 3.4 |
| dsh | 断链修复后再评估 | 未覆盖 | — |
| 任意（兜底） | MCP 搜索服务器 | 不经网关 | 文档说明 |

---

## 3. 详细设计

### 3.1 `service/search` 包

```go
package search

// Result 统一搜索结果模型。
type Result struct {
    Title    string `json:"title"`
    URL      string `json:"url"`
    Snippet  string `json:"snippet"`
    SiteName string `json:"site_name,omitempty"`
    Date     string `json:"date,omitempty"`
}

// Options 单次搜索的调用参数，由调用方按客户端协议填充。
type Options struct {
    MaxResults     int
    Language       string
    Recency        string // "", "day", "week", "month", "year"（映射到 SearXNG time_range）
    AllowedDomains []string
    BlockedDomains []string
}

// Provider 搜索后端接口。新后端实现此接口并在 Current() 中登记。
type Provider interface {
    Name() string
    Search(ctx context.Context, query string, opt Options) ([]Result, error)
    Fetch(ctx context.Context, rawURL string) (string, error) // 返回 markdown 文本
}
```

设计决策（ADR-1）：**Provider 实例每次调用按当前配置构建，不做进程级缓存**。理由：Provider 构造只是结构体赋值，HTTP 传输用包级共享 `http.Client`/`Transport`；不做缓存就不需要在 `model/option.go` 建立「配置变更 → 重建缓存」的回调（`model` 包不能反向 import `service`，而 `setting` 包不适合放网络代码）。超时通过 `context.WithTimeout` 施加。

超时与失败语义：Provider 调用失败必须返回错误，禁止返回空结果冒充成功；调用方把错误显式回给客户端（HTTP 502 或协议内 error 字段）。

### 3.2 配置面

新增 `setting/operation_setting/search.go`，注册名 `search_setting`（DB 键前缀 `search_setting.`）：

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `enabled` | bool | false | 总开关 |
| `provider` | string | `"searxng"` | 后端标识；未知值 = 配置错误，请求时显式失败 |
| `endpoint` | string | `""` | SearXNG 基地址，如 `http://127.0.0.1:8888` |
| `credential_env` | string | `""` | 需要 key 的后端（Tavily 等）的环境变量名。**不存 key 本身**（规避 `GetOptions` 过滤） |
| `timeout_ms` | int | 8000 | 单次搜索超时，校验范围 500-60000 |
| `max_results` | int | 8 | 结果上限，校验范围 1-20（Kimi 协议上限 20） |
| `language` | string | `""` | 传给后端的语言偏好 |
| `allow_fetch` | bool | false | 是否开放 fetch 端点（抓取任意 URL 风险更高，默认关） |
| `enable_coding_endpoints` | bool | false | `/coding/v1/*` 开关 |
| `enable_worker_endpoints` | bool | false | `/v1/code/sessions/*/worker/*` 开关 |
| `enable_codex_standalone` | bool | false | `/v1/alpha/search` 开关 |
| `enable_anthropic_sidecar` | bool | false | Claude 边车仿真开关 |
| `billing_tool_name` | string | `"web_search"` | 计费用价格键（可在工具定价页覆盖价格） |
| `unmatched_tool_policy` | string | `"reject"` | 非边车请求携带内置搜索工具时的策略：`reject`（400）或 `strip`（剥离并继续） |

校验钩子加在 `model/option.go` 现有 switch（`case "tool_price_setting.prices"` 同款位置，`model/option.go:688` 附近）：`endpoint` 必须是合法 http/https URL；`provider`、`unmatched_tool_policy` 枚举校验；数值范围校验。校验失败返回错误、不落库（与 `ValidateToolPriceJSON` 同模式）。

前端：在系统设置「模型与计费」分组内新增 `search` 分区（与「工具定价」并列），文件 `web/default/src/features/system-settings/models/search-settings.tsx`，按 `section-registry.tsx:113-144` 注册，经既有 `/api/option` 读写；i18n 走 `bun run i18n:sync`。

### 3.3 端点契约（通道 A）

统一约定：全部为 `POST`，`Content-Type: application/json`（fetch 响应除外）；鉴权走 `middleware.TokenAuth()`（Bearer 或 x-api-key，与 relay 一致）；限流走 `middleware.SearchRateLimit()`；**不挂 `Distribute`**；请求体上限 64KB（处理器内检查 `c.Request.ContentLength`，超限 413）。

**Kimi**（`enable_coding_endpoints`）：

```
POST /coding/v1/search
请求: {"text_query":"...","limit":8,"enable_page_crawling":false,"timeout_seconds":30}
响应: {"search_results":[{"site_name":"example.com","title":"...","url":"https://...","snippet":"...","content":"...","date":"","icon":"","mime":"text/html"}]}
错误: 后端失败 → HTTP 502 {"error":{"message":"...","type":"search_backend_error"}}

POST /coding/v1/fetch
请求: {"url":"https://..."}
响应: 纯文本 markdown 正文，Content-Type: text/markdown; charset=utf-8
```

**Claude Code CCR 代理**（`enable_worker_endpoints`）：

```
POST /v1/code/sessions/{id}/worker/web-search
请求: {"query":"...","allowed_domains":["..."],"blocked_domains":["..."],"search_profile":"..."}
响应: {"results":[{"title":"...","url":"https://...","snippet":"..."}]}
错误: {"results":[],"error":"..."}（协议内 error 字段）或 HTTP 502

POST /v1/code/sessions/{id}/worker/web-fetch
请求: {"url":"https://..."}
响应: 抓取文本（Content-Type: text/plain; charset=utf-8）
```

该协议是从 Claude Code 二进制提取的（`/v1/code/sessions/` 出现 50 次），**未经端到端验证**——任务 8 的验收步骤包含用真实 Claude Code + CCR 开关实测并据实修正。

**Codex standalone**（`enable_codex_standalone`）：

```
POST /v1/alpha/search
请求: {"id":"...","model":"gpt-5.5","input":"...","commands":[{"type":"search_query","query":"..."}],"max_output_tokens":null}
响应: {"output":"<搜索结果文本>","results":[{"title":"...","url":"...","snippet":"..."}]}
```

处理规则：遍历 `commands`，对 `type == "search_query"` 取 `query`（兼容 `q` 字段名）逐条搜索；其余 command 类型（`open`/`click`/`find` 等）本次不实现，遇到即返回 501 + 明确错误文案。`output` 为面向模型的文本摘要（标题 + URL + 摘要逐条拼接），`results` 为结构化数组。字段名以真实 codex 0.160.0 的请求为准，任务 8 含实测校正步骤。

### 3.4 Claude 边车仿真（通道 B，本次最高价值项）

**识别条件**（`relay/claude_search_sidecar.go`，判定必须保守，宁可漏判不可误判）：

1. `len(GetTools()) == 1`，且该工具对象的 `type` 以 `web_search` 开头（覆盖 `web_search_20250305`）；
2. `tool_choice` 反序列化后 `Type == "tool"` 且 `Name` 等于该工具 `name`；
3. `len(Messages) == 1`、`Role == "user"`、正文以 `Perform a web search for the query:` 开头（强匹配，用于提取 query）。

三条全中 → 本地应答路径。若 1、2 命中但 3 不中（客户端换措辞）：不回退到上游（禁止静默降级），按 `unmatched_tool_policy` 处理（默认 400，错误文案注明「内置搜索工具只支持边车形态」）。

**拦截位置**：`relay/claude_handler.go` 的 `ClaudeHelper` 开头，早于 `:131` 的 Responses 分支与 `:158` 的 `adaptor.ConvertClaudeRequest`。仅当 `enable_anthropic_sidecar` 开启且识别命中时旁路；否则完全走原路径（保证既有真 Claude 渠道行为不变）。

**响应（流式）**：`Content-Type: text/event-stream`，逐事件 `Flush()`，事件序列（顺序与字段为客户端硬要求）：

```
event: message_start
data: {"type":"message_start","message":{"id":"msg_<random>","type":"message","role":"assistant","model":"<请求model>","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srvtoolu_<random>","name":"web_search","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"<转义后的query>\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srvtoolu_<同一id>","content":[{"type":"web_search_result","title":"...","url":"https://..."}]}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":0}}

event: message_stop
data: {"type":"message_stop"}
```

关键约束：`server_tool_use.id` 与 `web_search_tool_result.tool_use_id` 必须一致；index 从 0 递增（0=server_tool_use，1=结果块）；每个事件形如 `event: <type>\ndata: <单行JSON>\n\n`。**不附加 `encrypted_content`**（客户端不依赖它，参考实现中该项属借位填充，不照搬）。

**响应（非流式）**：`stream != true` 时返回等价单条 JSON message（同内容的 `content` 数组），`Content-Type: application/json`。

**失败语义**：搜索后端失败时，流式返回一个 `event: error` 帧（`{"type":"error","error":{"type":"api_error","message":"..."}}`）后停止，绝不返回空结果的"成功"。注意错误帧必须包含非空 message（仓库既有教训：空错误帧会中止健康流）。

### 3.5 计费

统一按次计费公式（与 `service/text_quota.go:106-128` 同口径）：

```
quota = round(price_per_1k / 1000 * call_count * common.QuotaPerUnit * groupRatio)
price_per_1k = operation_setting.GetToolPriceForModel(billing_tool_name, modelName)
groupRatio   = ratio_setting.GetGroupRatio(c.GetString("group"))
```

- `billing_tool_name` 默认 `web_search`（默认价 $10/1000 次 → 单次 5000 quota = $0.01）；管理员可在工具定价页覆盖，或改用自定义键（如 `searxng_search`）实现自建搜索免费/自定义定价。价格未配置（返回 0）→ 不计费但仍记日志（自建 SearXNG 免费是合法配置）。
- 扣费与落库：`model.DecreaseUserQuota(userId, quota, false)` + `model.RecordConsumeLog(c, userId, model.RecordConsumeLogParams{...})`，`Other` 里记 `web_search_call_count`、`search_provider`、`search_endpoint`。执行前先读 `model.GetUserQuota(userId, false)` 校验余额，不足直接 402 不执行。
- 边车仿真与显式端点的单次调用计数：一次 HTTP 请求 = 一次搜索调用（Kimi 限流参数里的 limit 影响结果条数，不影响调用次数）。
- 既有缺口修复（任务 11）：Claude 流式路径的 `server_tool_use.web_search_requests` 目前不计数（`relay-claude.go:1379` 只在非流式），补齐流式 `message_delta` 的 usage 解析与计数写入。

### 3.6 安全

- fetch 端点必须复用既有 SSRF 框架：`common.ValidateURLWithFetchSettingAndResolveContext`（含私网拦截、DNS pin）；配置读取 `setting/system_setting/fetch_setting.go` 的 `FetchSetting`（`EnableSSRFProtection` 默认开）。fetch 默认关闭（`allow_fetch=false`）。
- 抓取响应的内容长度上限（1MB）、超时（复用 `timeout_ms`）、重定向上限（5 次）、仅接受 `text/html` 与 `text/plain`（其余 415）。
- 密钥：`credential_env` 只存环境变量名，不落库；日志与错误信息不得回显 key。
- 限流：`SearchRateLimit()`（默认 10 次/60 秒/用户，管理员豁免）。
- 请求体上限 64KB；`/v1/alpha/search` 的 `commands` 数组元素上限 16。

### 3.7 与上游 #7690、relaykit 迁移的关系

- 本方案是「网关自建搜索」，#7690 是「厂商搜索参数编码与计费」；两者互补不冲突。**不要**在本方案中吸收 #7690 的 relaykit 侧实现（本 fork 无 relaykit，原样合并不可行，已验证）。
- 计费基础设施若未来要在 relaykit 迁移时与上游对齐（`relay/common/tool_usage.go`、`CountBillableToolCall`、10000 上限），本方案预留：计费调用集中在 `service/search_billing.go` 一个文件内，迁移时只改该文件。
- 不改动 `service/convert.go` 主体。对它的唯一改动（任务 10 的策略分支）要求：只增不改，且改动点远离 relaykit 迁移补丁 1 的目标函数（`to_oai_chat_req.go` 对应物）。若迁移规格与本改动冲突，以「策略分支移到 `claude_handler.go` 的调用点」为退路。

---

## 4. 文件结构与改动面

新增：

```
service/search/provider.go            接口、Result/Options、Current() 与错误定义
service/search/searxng.go             SearXNG 实现（Search + Fetch）
service/search/searxng_test.go
service/search/provider_test.go
service/search_billing.go             按次计费（唯一计费入口，便于迁移对齐）
service/search_billing_test.go
service/search_setting.go?            不需要；配置在 setting 包
setting/operation_setting/search.go   SearchSetting 注册与访问器
setting/operation_setting/search_test.go
controller/search_endpoints.go        三个显式端点处理器
controller/search_endpoints_test.go
relay/claude_search_sidecar.go        边车识别 + SSE/JSON 应答
relay/claude_search_sidecar_test.go
web/default/src/features/system-settings/models/search-settings.tsx
```

修改：

```
router/relay-router.go                注册三组端点（显式、带中间件）
relay/claude_handler.go               开头插入边车旁路（约 6 行）
relay/channel/claude/relay-claude.go  任务 11：流式计数字段补齐
model/option.go                       校验钩子 + （无热更新回调改动）
web/default/.../section-registry.tsx  注册 search 分区
web/default/.../models/index.tsx      分区入口（按现有模式）
```

不改：`service/convert.go` 主体、`relay/common/` 协议转换件、既有透传路径。

---

## 5. 任务分解

依赖顺序：任务 1→2→3 为基础层；4、5、6 依赖 2、3；7 依赖 2、3；8、9 可与并行任务并行；10、11 收尾。每个任务一个原子提交。

**Review Focus（五个最可能咬人的失败面，各自被所属任务的测试钉住）**：

1. Provider 把后端故障/超时空结果冒充成功 → 任务 2 的 `TestSearxngSearchReturnsErrorOnHTTPError`、`TestSearxngSearchTimeoutPropagates`。
2. 边车识别误伤正常主对话（把带普通工具的请求拦截）→ 任务 7 的 `TestSidecarDetectRejectsNormalConversation` 三条件反向用例。
3. SSE 序列契约错（id 不配对、index 错序、事件顺序错）→ 任务 7 的 `TestSidecarStreamEventSequenceGolden` 按序断言全部事件。
4. fetch 端点 SSRF/超大响应/重定向绕过 → 任务 4 的 `TestFetchRejectsPrivateIP`、`TestFetchTruncatesAt1MB`、`TestFetchStopsAfterRedirectLimit`。
5. 搜索被重复扣费（端点 + 边车双计，或与既有 text_quota 重复）→ 任务 3 的 `TestComputeSearchBillExactQuota`（纯函数钉死公式）+ 任务 11 验收步骤 7（单次日志只出现一组 `web_search_call_count`）。

### 任务 1：SearchSetting 配置与校验

**Files**:
- Create: `setting/operation_setting/search.go`
- Create: `setting/operation_setting/search_test.go`
- Modify: `model/option.go`（在校验 switch（`model/option.go:688` 的 `case "tool_price_setting.prices"` 相邻处）新增 `case "search_setting.endpoint"`、`case "search_setting.provider"`、`case "search_setting.unmatched_tool_policy"`、`case "search_setting.timeout_ms"`、`case "search_setting.max_results"`）

**Interfaces**:
- Produces: `operation_setting.GetSearchSetting() *SearchSetting`（字段见 3.2 表，JSON tag 全小写下划线）；校验函数 `ValidateSearchSettingField(key, value string) error` 供 `model/option.go` 调用。

- [ ] **Step 1: 写失败测试**

```go
package operation_setting

import (
    "testing"

    "github.com/stretchr/testify/require"
)

func TestValidateSearchSettingFields(t *testing.T) {
    require.Error(t, ValidateSearchSettingField("search_setting.endpoint", "not-a-url"))
    require.NoError(t, ValidateSearchSettingField("search_setting.endpoint", "http://127.0.0.1:8888"))
    require.Error(t, ValidateSearchSettingField("search_setting.provider", "bing"))
    require.NoError(t, ValidateSearchSettingField("search_setting.provider", "searxng"))
    require.Error(t, ValidateSearchSettingField("search_setting.unmatched_tool_policy", "drop"))
    require.NoError(t, ValidateSearchSettingField("search_setting.unmatched_tool_policy", "strip"))
    require.Error(t, ValidateSearchSettingField("search_setting.timeout_ms", "100"))   // <500
    require.Error(t, ValidateSearchSettingField("search_setting.timeout_ms", "90000")) // >60000
    require.NoError(t, ValidateSearchSettingField("search_setting.timeout_ms", "8000"))
    require.Error(t, ValidateSearchSettingField("search_setting.max_results", "0"))
    require.Error(t, ValidateSearchSettingField("search_setting.max_results", "21")) // Kimi 上限 20
}

func TestGetSearchSettingDefaults(t *testing.T) {
    s := GetSearchSetting()
    require.False(t, s.Enabled)
    require.Equal(t, "searxng", s.Provider)
    require.Equal(t, 8000, s.TimeoutMs)
    require.Equal(t, 8, s.MaxResults)
    require.Equal(t, "web_search", s.BillingToolName)
    require.Equal(t, "reject", s.UnmatchedToolPolicy)
    require.False(t, s.AllowFetch)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./setting/operation_setting -run "TestValidateSearchSettingFields|TestGetSearchSettingDefaults" -v`
Expected: FAIL（undefined: ValidateSearchSettingField / GetSearchSetting）

- [ ] **Step 3: 实现**

`setting/operation_setting/search.go`（结构与注册照抄同目录 `tools.go:40-60` 的 `ToolPriceSetting` 模式）：`SearchSetting` 结构体含 3.2 表全部字段（`Enabled bool json:"enabled"`、`Provider string json:"provider"`、`Endpoint string json:"endpoint"`、`CredentialEnv string json:"credential_env"`、`TimeoutMs int json:"timeout_ms"`、`MaxResults int json:"max_results"`、`Language string json:"language"`、`AllowFetch bool json:"allow_fetch"`、`EnableCodingEndpoints bool json:"enable_coding_endpoints"`、`EnableWorkerEndpoints bool json:"enable_worker_endpoints"`、`EnableCodexStandalone bool json:"enable_codex_standalone"`、`EnableAnthropicSidecar bool json:"enable_anthropic_sidecar"`、`BillingToolName string json:"billing_tool_name"`、`UnmatchedToolPolicy string json:"unmatched_tool_policy"`）；`init()` 里 `config.GlobalConfig.Register("search_setting", &searchSetting)`，默认值即 Step 1 断言值；`GetSearchSetting()` 返回该指针；`ValidateSearchSettingField` 按 key 分派：`endpoint` 用 `net/url` 解析且 scheme 仅 http/https、host 非空，`provider` 枚举 `searxng|tavily|brave`，`unmatched_tool_policy` 枚举 `reject|strip`，`timeout_ms`/`max_results` 按断言范围，其余 key 返回 nil（沿用仓库 `ValidateToolPriceJSON` 的报错风格，中文错误文案）。

`model/option.go`：在 688 行相邻的校验 switch 中新增上述四个 `case`，转调 `operation_setting.ValidateSearchSettingField(key, value)`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./setting/operation_setting -run "TestValidateSearchSettingFields|TestGetSearchSettingDefaults" -v && go test ./model -run TestUpdateOptionRejectsInvalidToolPriceBeforePersist -v`
Expected: PASS（含既有 option 校验测试不回归）

- [ ] **Step 5: 提交**

```bash
git add setting/operation_setting/search.go setting/operation_setting/search_test.go model/option.go
git commit -m "feat(setting): register search_setting with field validation"
```

### 任务 2：service/search Provider 抽象与 SearXNG 实现

**Files**:
- Create: `service/search/provider.go`
- Create: `service/search/searxng.go`
- Create: `service/search/provider_test.go`
- Create: `service/search/searxng_test.go`

**Interfaces**:
- Consumes: 任务 1 的 `GetSearchSetting()`。
- Produces（任务 3-7 依赖，签名固定）:
  - `search.Result{Title, URL, Snippet, SiteName, Date string}`（JSON tag 同名小写）
  - `search.Options{MaxResults int, Language, Recency string, AllowedDomains, BlockedDomains []string}`
  - `search.Provider interface{ Name() string; Search(ctx, query string, opt Options) ([]Result, error); Fetch(ctx, rawURL string) (string, error) }`
  - `search.Current() (Provider, error)`——读配置构建实例；`enabled=false` 或 `provider` 未知时返回非 nil 错误
  - `search.ApplyDomainFilters(results []Result, allowed, blocked []string) []Result`

- [ ] **Step 1: 写失败测试**

```go
// provider_test.go
package search

import (
    "context"
    "testing"

    "github.com/stretchr/testify/require"

    "github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestCurrentDisabledReturnsError(t *testing.T) {
    s := operation_setting.GetSearchSetting()
    prev := *s
    t.Cleanup(func() { *s = prev })
    s.Enabled = false
    _, err := Current()
    require.Error(t, err)
    require.Contains(t, err.Error(), "search backend not enabled")
}

func TestCurrentUnknownProviderReturnsError(t *testing.T) {
    s := operation_setting.GetSearchSetting()
    prev := *s
    t.Cleanup(func() { *s = prev })
    s.Enabled = true
    s.Provider = "bing"
    _, err := Current()
    require.Error(t, err)
}

func TestApplyDomainFilters(t *testing.T) {
    in := []Result{{URL: "https://a.example.com/x"}, {URL: "https://b.test/x"}}
    out := ApplyDomainFilters(in, []string{"example.com"}, nil)
    require.Len(t, out, 1)
    require.Equal(t, "https://a.example.com/x", out[0].URL)
    out = ApplyDomainFilters(in, nil, []string{"b.test"})
    require.Len(t, out, 1)
}
```

```go
// searxng_test.go
package search

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
)

func newTestSearxng(handler http.HandlerFunc) (*searxngProvider, *httptest.Server) {
    srv := httptest.NewServer(handler)
    return &searxngProvider{endpoint: srv.URL, client: &http.Client{Timeout: 2 * time.Second}}, srv
}

func TestSearxngSearchParsesResults(t *testing.T) {
    p, srv := newTestSearxng(func(w http.ResponseWriter, r *http.Request) {
        require.Equal(t, "/search", r.URL.Path)
        require.Equal(t, "json", r.URL.Query().Get("format"))
        require.Equal(t, "new-api gateway", r.URL.Query().Get("q"))
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{"results":[{"title":"T","url":"https://e.com/1","content":"S","engine":"bing","publishedDate":"2026-10-01"}]}`))
    })
    defer srv.Close()
    got, err := p.Search(context.Background(), "new-api gateway", Options{MaxResults: 8})
    require.NoError(t, err)
    require.Len(t, got, 1)
    require.Equal(t, "T", got[0].Title)
    require.Equal(t, "bing", got[0].SiteName)
}

func TestSearxngSearchReturnsErrorOnHTTPError(t *testing.T) {
    p, srv := newTestSearxng(func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "boom", http.StatusBadGateway)
    })
    defer srv.Close()
    _, err := p.Search(context.Background(), "q", Options{})
    require.Error(t, err)
    require.Contains(t, err.Error(), "502")
}

func TestSearxngSearchTimeoutPropagates(t *testing.T) {
    p, srv := newTestSearxng(func(w http.ResponseWriter, r *http.Request) {
        time.Sleep(300 * time.Millisecond)
    })
    defer srv.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
    defer cancel()
    _, err := p.Search(ctx, "q", Options{})
    require.Error(t, err) // 超时必须显式上抛，禁止返回空结果
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./service/search -v`
Expected: FAIL（无 package）

- [ ] **Step 3: 实现**

`provider.go`：按 Interfaces 节定义类型；`Current()` 读 `operation_setting.GetSearchSetting()`，`enabled=false` → `errors.New("search backend not enabled, enable it in system settings")`；`provider=="searxng"` → `&searxngProvider{endpoint: s.Endpoint, client: sharedTransportClient()}`（`sharedTransportClient` 是包内 `sync.Once` 建的 `*http.Client`，无超时——超时只走 ctx）；未知枚举 → 明确错误。`ApplyDomainFilters` 用 `net/url` 取 Hostname 后缀匹配（与 `common.ssrf_protection.go:326-339` 的 `*.example.com` 语义一致：条目精确匹配或 `.`+后缀）。

`searxng.go`：`Search` 构造 `GET {endpoint}/search?format=json&q=...`，附加 `language`（非空时 `language` 参数）、`time_range`（Recency 映射：day→`day`、week→`week`、month→`month`、year→`year`）；`context.WithTimeout` 用 `GetSearchSetting().TimeoutMs`；解析 `{"results":[{"title","url","content","engine","publishedDate"}]}`（映射到 Result，`engine`→SiteName、`publishedDate`→Date）；截断到 `MaxResults`（默认取配置 `MaxResults`）；HTTP 非 2xx 返回含状态码的错误；`Fetch`：GET 目标 URL，走 `common.ValidateURLWithFetchSettingAndResolveContext`（参数取 `system_setting.GetFetchSetting()` 全部字段，`fetch_setting.go:5-25`）+ `common.WithSSRFValidation` 标记，响应体读到 1MB 截断（超出部分丢弃不报错，但计数日志注明），`Content-Type` 非 `text/html`/`text/plain` 时返回 415 语义错误；HTML 用**内联最小提取**：`regexp` 删 `(?s)<script...>`、`(?s)<style...>`、`(?s)<[^>]+>`，`html.UnescapeString` 还原实体，压缩连续空白——仓库现无 HTML→文本工具（`git grep` 零命中），不引第三方依赖。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./service/search -v`
Expected: PASS（全部用例）

- [ ] **Step 5: 提交**

```bash
git add service/search/provider.go service/search/searxng.go service/search/provider_test.go service/search/searxng_test.go
git commit -m "feat(search): add search provider abstraction with SearXNG backend"
```

### 任务 3：按次计费 search_billing

**Files**:
- Create: `service/search_billing.go`
- Create: `service/search_billing_test.go`

**Interfaces**:
- Consumes: `operation_setting.GetToolPriceForModel`、`common.QuotaPerUnit`、`model.GetUserQuota`/`model.DecreaseUserQuota`/`model.RecordConsumeLog`（均已存在，出处见 1.1）。
- Produces（任务 4-7 依赖）:
  - `service.BillSearchParams{UserID, TokenID int; TokenName, Group, ModelName, ToolName, Provider string; CallCount int; GroupRatio float64}`
  - `service.ComputeSearchBill(p BillSearchParams) (quota int, pricePer1K float64)`（纯函数）
  - `service.BillSearchCall(c *gin.Context, p BillSearchParams) (quota int, err error)`（IO 包装：余额校验→扣减→记日志）

- [ ] **Step 1: 写失败测试**

```go
package service

import (
    "testing"

    "github.com/QuantumNous/new-api/common"
)

func TestComputeSearchBillExactQuota(t *testing.T) {
    // 默认价 web_search = $10/1K：1 次、groupRatio 1 → 10/1000*500000 = 5000
    quota, price := ComputeSearchBill(BillSearchParams{
        ToolName: "web_search", ModelName: "gpt-5.5", CallCount: 1, GroupRatio: 1,
    })
    if price != 10.0 {
        t.Fatalf("pricePer1K = %v, want 10", price)
    }
    if quota != 5000 {
        t.Fatalf("quota = %d, want 5000 (QuotaPerUnit=%v)", quota, common.QuotaPerUnit)
    }
    // 价格为 0（未定价/免费后端）→ quota 0，不视为错误
    quota, _ = ComputeSearchBill(BillSearchParams{
        ToolName: "searxng_free", ModelName: "x", CallCount: 1, GroupRatio: 1,
    })
    if quota != 0 {
        t.Fatalf("unpriced quota = %d, want 0", quota)
    }
    // groupRatio 2 倍
    quota, _ = ComputeSearchBill(BillSearchParams{
        ToolName: "web_search", ModelName: "gpt-5.5", CallCount: 3, GroupRatio: 2,
    })
    if quota != 30000 {
        t.Fatalf("quota = %d, want 30000", quota)
    }
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./service -run TestComputeSearchBillExactQuota -v`
Expected: FAIL（undefined: ComputeSearchBill）

- [ ] **Step 3: 实现**

```go
// service/search_billing.go
package service

func ComputeSearchBill(p BillSearchParams) (quota int, pricePer1K float64) {
    pricePer1K = operation_setting.GetToolPriceForModel(p.ToolName, p.ModelName)
    if pricePer1K <= 0 || p.CallCount <= 0 {
        return 0, pricePer1K
    }
    totalPrice := pricePer1K / 1000 * float64(p.CallCount)
    return int(math.Round(totalPrice * common.QuotaPerUnit * p.GroupRatio)), pricePer1K
}

// BillSearchCall: 余额（model.GetUserQuota(id,false)，err 或 <cost → 402 语义错误）
// → model.DecreaseUserQuota(id, cost, false)
// → model.RecordConsumeLog(c, p.UserID, model.RecordConsumeLogParams{
//      ModelName: "search:" + p.Provider, Quota: cost,
//      Content: fmt.Sprintf("搜索调用 %d 次，花费 %s", ...), TokenId: p.TokenID,
//      Group: p.Group, Other: map[string]any{"web_search_call_count": p.CallCount,
//      "search_provider": p.Provider, "tool_name": p.ToolName}})
// cost==0 时仍记日志（quota 0）、不扣减、返回 nil。
```

余额不足返回 `types.NewErrorWithStatusCode(..., http.StatusPaymentRequired, ...)` 风格错误（读 `service/pre_consume_quota.go:33` 现有余额检查写法后对齐，禁止静默放行）。

- [ ] **Step 4: 跑测试确认通过**（含补一个 IO 路径测试：`common.LogConsumeEnabled=false`、未播种用户时 `BillSearchCall` 返回显式错误而非成功）

Run: `go test ./service -run "TestComputeSearchBill|TestBillSearchCall" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add service/search_billing.go service/search_billing_test.go
git commit -m "feat(billing): per-call search billing on the existing tool price index"
```

### 任务 4：路由挂载 + Kimi 端点 /coding/v1/search|fetch

**Files**:
- Create: `controller/search_endpoints.go`
- Create: `controller/search_endpoints_test.go`
- Modify: `router/relay-router.go`（`SetRelayRouter` 内、web 兜底注册前——本仓库顺序 `api→dashboard→relay→video→web` 天然满足，但注册必须是显式路由）

**Interfaces**:
- Consumes: 任务 2 `Current()`、任务 3 `BillSearchCall`。
- Produces: `POST /coding/v1/search`、`POST /coding/v1/fetch`（契约见 3.3）。

- [ ] **Step 1: 写失败测试（含 Review Focus #4 的三个用例）**

```go
package controller

// 注册路由的表驱动：无鉴权时必须 401 JSON，且响应 Content-Type 不得是 text/html（防 SPA 兜底回归）。
func TestSearchEndpointsRejectUnauthenticatedAsJSON(t *testing.T) {
    r := newSearchTestRouter() // gin.New() + searchRoute(r)
    for _, path := range []string{"/coding/v1/search", "/coding/v1/fetch"} {
        w := doJSON(r, "POST", path, `{"text_query":"q","limit":1}`)
        if w.Code == 200 || w.Header().Get("Content-Type") == "text/html; charset=utf-8" {
            t.Fatalf("%s: status=%d ct=%s", path, w.Code, w.Header().Get("Content-Type"))
        }
    }
}

func TestCodingEndpointsDisabledByDefault(t *testing.T) {
    // enable_coding_endpoints=false → 404 且非 HTML
    r := newSearchTestRouter()
    w := doJSON(r, "POST", "/coding/v1/search", `{"text_query":"q","limit":1}`)
    if w.Code != http.StatusNotFound || w.Header().Get("Content-Type") == "text/html; charset=utf-8" {
        t.Fatalf("status=%d ct=%s", w.Code, w.Header().Get("Content-Type"))
    }
}
```

fetch 安全三用例（挂 `operation_setting.GetSearchSetting()` 开 `AllowFetch` + httptest 私网回环目标）：`TestFetchRejectsPrivateIP`（`http://127.0.0.1:x` 被 SSRF 拦截返回 4xx 含 "private"，依赖 `FetchSetting.EnableSSRFProtection=true` 默认值）、`TestFetchTruncatesAt1MB`（2MB 响应 → 返回体恰 1MB）、`TestFetchStopsAfterRedirectLimit`（302 环 → 超过 5 次返回 502 明确错误）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./controller -run "TestSearchEndpoints|TestCodingEndpoints|TestFetch" -v`
Expected: FAIL（路由未注册）

- [ ] **Step 3: 实现**

`controller/search_endpoints.go`：`func RegisterSearchRoutes(g *gin.Group)`——组构造见 Step 4；处理器：
- `CodingSearch(c)`：`enable_coding_endpoints` 关 → 404；body 64KB 上限 → 413；解析 `{text_query, limit, timeout_seconds}`（`limit` 缺省取 `MaxResults`，clamp 1-20；`text_query` 空 → 400）；`Current()` 失败 → 502+文案；`Search` → 失败 502 `{"error":{"message":...,"type":"search_backend_error"}}`；成功裁剪到 limit；`ApplyDomainFilters`（配置无 per-request 域名参数，Kimi 协议没有，跳过）；`BillSearchCall`，余额错误原样透传（402）；响应严格按 3.3 字段。
- `CodingFetch(c)`：`AllowFetch=false` → 404；`{url}` 解析 → `Provider.Fetch` → `text/markdown` 响应；计 1 次。
- 每个成功路径计 1 次（`CallCount: 1`，`ModelName: ""`——端点无模型，价格取默认键）。

`router/relay-router.go`：在 `SetRelayRouter` 中段（`relayV1Router` 定义前后均可，但必须在函数内、显式注册）加：

```go
codingRouter := router.Group("/coding/v1")
codingRouter.Use(middleware.RouteTag("relay"))
codingRouter.Use(middleware.TokenAuth())
codingRouter.Use(middleware.SearchRateLimit())
controller.RegisterSearchRoutes(codingRouter)
```

`RegisterSearchRoutes` 内再挂 `SearchRateLimit` 不重复——统一在组上挂一次即可（实现时二选一，测试锁住「未登录 401、关闭时 404」两个外部可观察行为）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./controller -run "TestSearchEndpoints|TestCodingEndpoints|TestFetch" -v && go test ./router/... 2>/dev/null || true`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add controller/search_endpoints.go controller/search_endpoints_test.go router/relay-router.go
git commit -m "feat(api): Kimi-compatible /coding/v1 search and fetch endpoints"
```

### 任务 5：Codex standalone 端点 /v1/alpha/search

**Files**:
- Modify: `controller/search_endpoints.go`（追加 `AlphaSearch` 处理器与注册）
- Modify: `controller/search_endpoints_test.go`

**Interfaces**:
- Consumes: 任务 2、3。
- Produces: `POST /v1/alpha/search`（契约见 3.3；字段名以 8.1 实测校正为准）。

- [ ] **Step 1: 写失败测试**

```go
func TestAlphaSearchRunsSearchQueryCommand(t *testing.T) {
    // enabled、假 provider（httptest 返回 1 条结果）：
    // 请求 commands=[{"type":"search_query","query":"hello"}]
    // 断言：200；output 含 "hello" 对应结果标题与 URL；results 数组 1 项
}
func TestAlphaSearchRejectsUnknownCommand(t *testing.T) {
    // commands=[{"type":"click","selector":"x"}] → 501，错误文案含 "unsupported command"
}
func TestAlphaSearchHonorsEnabledFlag(t *testing.T) {
    // enable_codex_standalone=false → 404 且非 HTML
}
```

- [ ] **Step 2: 跑测试确认失败**
- [ ] **Step 3: 实现**：解析 `{id, model, commands[]}`（`commands` 超 16 个 → 400）；对每个 `type=="search_query"` 条目取 `query`（字段缺失时回退试 `q`，两者都缺 → 400）；其余 type → 501；合并全部结果去重（按 URL）；`output` = 逐条 `"{i}. {title}\n{url}\n{snippet}\n"` 拼接；响应 `{"output": ..., "results": [...]}`；计费按实际执行的 search_query 条数（`CallCount = n`）。路由：在任务 4 的 `RegisterSearchRoutes` 里挂到 `/v1/alpha/search`——**用 `router` 根组单独注册**（不进 `relayV1Router`，避免继承 `Distribute`）：

```go
alphaRouter := router.Group("/v1/alpha")
alphaRouter.Use(middleware.RouteTag("relay"))
alphaRouter.Use(middleware.TokenAuth())
alphaRouter.Use(middleware.SearchRateLimit())
alphaRouter.POST("/search", controller.AlphaSearch)
```

- [ ] **Step 4: 跑测试确认通过**
- [ ] **Step 5: 提交**（`git add controller/search_endpoints*.go`，消息 `feat(api): Codex standalone /v1/alpha/search endpoint`）

### 任务 6：Claude Code CCR worker 端点

**Files**:
- Modify: `controller/search_endpoints.go`、`controller/search_endpoints_test.go`

**Interfaces**:
- Produces: `POST /v1/code/sessions/:id/worker/web-search`、`POST /v1/code/sessions/:id/worker/web-fetch`（契约见 3.3；`enable_worker_endpoints` 开关）。

- [ ] **Step 1: 写失败测试**（形状同任务 4：未登录 401 JSON、关闭 404 非 HTML、`allowed_domains` 过滤生效——入参域名列表非空时对结果调 `ApplyDomainFilters`、`blocked_domains` 排除）
- [ ] **Step 2: 跑测试确认失败**
- [ ] **Step 3: 实现**：`WorkerWebSearch(c)` 解析 `{query, allowed_domains?, blocked_domains?, search_profile?}`（`search_profile` 接受但忽略并写入日志 other，未知字段不报错）；`ApplyDomainFilters(allowed, blocked)`；计 1 次；成功 `{"results":[{"title","url","snippet"}]}`，后端失败 `{"results":[],"error":"..."}` + HTTP 502；`WorkerWebFetch` 同任务 4 的 fetch 内核（复用私有 `runFetch(c, rawURL)` 函数，禁止复制粘贴两份 SSRF 逻辑）。注册：`/v1/code/sessions` 组，同任务 5 三中间件模式。
- [ ] **Step 4: 跑测试确认通过**
- [ ] **Step 5: 提交**（`feat(api): Claude Code CCR worker search and fetch endpoints`）

### 任务 7：Claude 边车仿真

**Files**:
- Create: `relay/claude_search_sidecar.go`
- Create: `relay/claude_search_sidecar_test.go`
- Modify: `relay/claude_handler.go`（`ClaudeHelper` 开头插入旁路，约 6 行）

**Interfaces**:
- Consumes: 任务 2 `Current()`、任务 3 `BillSearchCall`、`dto.ClaudeRequest`（字段 `Tools any`、`ToolChoice any`、`Messages []ClaudeMessage`、`System any`、`Stream *bool`、`Model`，见 `dto/claude.go:207-238`）、`dto.ProcessTools`。
- Produces: `relay.detectWebSearchSidecar(req *dto.ClaudeRequest) (query string, allowed, blocked []string, ok bool)`；`relay.answerWebSearchSidecar(c *gin.Context, req *dto.ClaudeRequest, query string, allowed, blocked []string) error`。

- [ ] **Step 1: 写失败测试（含 Review Focus #2、#3）**

```go
package relay

func TestSidecarDetectMatchesClientShape(t *testing.T) {
    body := `{"model":"claude-fable-5","stream":true,
      "system":[{"type":"text","text":"You are an assistant for performing a web search tool use"}],
      "messages":[{"role":"user","content":"Perform a web search for the query: new-api gateway"}],
      "tools":[{"type":"web_search_20250305","name":"web_search","max_uses":8}],
      "tool_choice":{"type":"tool","name":"web_search"}}`
    // common.Unmarshal 到 dto.ClaudeRequest 后：
    // q, _, _, ok := detectWebSearchSidecar(req) → ok==true, q=="new-api gateway"
}

func TestSidecarDetectRejectsNormalConversation(t *testing.T) {
    // 三种反例分别断言 ok==false：
    // a) tools 含 function 工具（正常主对话）
    // b) 单个 web_search 工具但 tool_choice 为 auto
    // c) 单个 web_search 工具但 messages 有多条
}

func TestSidecarStreamEventSequenceGolden(t *testing.T) {
    // httptest.ResponseRecorder 跑 answerWebSearchSidecar（stream=true，注入假 Provider 结果 1 条）：
    // 断言输出按顺序出现：
    //   "event: message_start" → "type\":\"server_tool_use\"" → "input_json_delta"
    //   → "type\":\"web_search_tool_result\"" → "event: message_stop"
    // 且 server_tool_use 的 "id":" 与 web_search_tool_result 的 "tool_use_id":" 字符串相等
    // 且 Content-Type 为 text/event-stream
}

func TestSidecarNonStreamReturnsJSONMessage(t *testing.T) {
    // stream 缺省/ false → 200 JSON，content 数组含 server_tool_use 与 web_search_tool_result 两块
}

func TestSidecarSearchFailureEmitsErrorFrame(t *testing.T) {
    // Provider 返回错误 → 输出含 "event: error" 且 message 非空；不含成功 message_stop 假象
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./relay -run "TestSidecar" -v`
Expected: FAIL

- [ ] **Step 3: 实现**

`relay/claude_search_sidecar.go`：
- `detectWebSearchSidecar`：三条件（3.4 节）全中才 `ok=true`；query 从前缀后取 `strings.TrimSpace`；`allowed/blocked` 从工具对象的 `allowed_domains`/`blocked_domains` 取（`map[string]any` 断言，取不到为 nil）。1、2 命中而 3 不中时**不**返回 ok——返回 `ok=false`，由调用方按 `UnmatchedToolPolicy` 走 `reject`/`strip`（`claude_handler.go` 内联处理：`reject` → 400 明确文案；`strip` → 从 `req.Tools`/`req.ToolChoice` 移除内置工具后继续原路径）。
- `answerWebSearchSidecar`：查 `GetSearchSetting().EnableAnthropicSidecar`（关 → 返回 `errSidecarDisabled`，调用方按原路径继续）；`Current()` + `Search`（Options 取 allowed/blocked、`MaxResults` 取配置）；计费（`BillSearchCall`，`CallCount:1`，`ModelName: req.Model`，group 从 `c.GetString("group")`、`ratio_setting.GetGroupRatio`）；按 3.4 的事件序列表逐条 `fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ...)` + `http.Flusher` flush；ID 用 `"srvtoolu_" + common.GetRandomString(24)`、`"msg_" + common.GetRandomString(24)`；非流式分支组装等价 JSON。错误帧注意非空 message。
- JSON 编码走 `common.Marshal`。

`relay/claude_handler.go` 插入（函数体最前，`request` 构造之后、任何 adaptor 调用之前）：

```go
if query, allowed, blocked, ok := detectWebSearchSidecar(request); ok {
    if err := answerWebSearchSidecar(c, request, query, allowed, blocked); err == nil {
        return nil
    } else if !errors.Is(err, errSidecarDisabled) {
        return types.NewErrorWithStatusCode(err, types.ErrorCodeUpstreamRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
    }
}
// 未命中或未启用 → 继续原有三分支（真 Claude 渠道行为不变）
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./relay -run "TestSidecar" -v && go test ./relay -run "TestClaudeHelper" -v`
Expected: PASS（既有 Claude helper 测试不回归）

- [ ] **Step 5: 提交**

```bash
git add relay/claude_search_sidecar.go relay/claude_search_sidecar_test.go relay/claude_handler.go
git commit -m "feat(relay): local Anthropic web_search sidecar simulation"
```

### 任务 8：既有缺口修复（流式计数 + 拒绝文案）

**Files**:
- Create: `relay/channel/claude/server_tool_usage.go`、`relay/channel/claude/server_tool_usage_test.go`
- Modify: `relay/channel/claude/relay-claude.go`（非流式 `:1379` 改调新函数；流式 usage 合并点补调）
- Create: `relay/claude_tool_rejection.go` + 测试
- Modify: `relay/claude_handler.go`（两处 `ConvertClaudeRequest`/`ClaudeToOpenAIRequest` 错误返回点包一层文案改写）

**Interfaces**:
- Produces: `claude.RecordServerToolUsage(c *gin.Context, usage *dto.Usage)`（幂等：取 `usage.ServerToolUse`，非 nil 且 `WebSearchRequests>0` 时 `c.Set("claude_web_search_requests", n)`）；`relay.RewriteClaudeToolRejection(err error) error`（纯函数）。

- [ ] **Step 1: 写失败测试**

```go
// server_tool_usage_test.go
func TestRecordServerToolUsageFromStreamMessageDelta(t *testing.T) {
    // 构造 *dto.Usage{ServerToolUse:&ClaudeServerToolUse{WebSearchRequests:3}}
    // RecordServerToolUsage(ctx, usage) → ctx.GetInt("claude_web_search_requests")==3
}
func TestRewriteClaudeToolRejectionAddsGuidance(t *testing.T) {
    err := errors.New(`Claude built-in tool type "web_search_20250305" cannot be converted to an OpenAI function tool`)
    got := RewriteClaudeToolRejection(err)
    require.Contains(t, got.Error(), "web_search_20250305")
    require.Contains(t, got.Error(), "enable_anthropic_sidecar") // 指引到配置
    // 非该形态的错误原样返回
}
```

- [ ] **Step 2: 跑测试确认失败** → `go test ./relay/channel/claude ./relay -run "TestRecordServerToolUsage|TestRewriteClaudeToolRejection" -v` FAIL
- [ ] **Step 3: 实现**：新文件如 Interfaces；`relay-claude.go` 非流式 `:1379` 三行替换为 `RecordServerToolUsage(c, claudeResponse.Usage)`；流式侧定位 `message_delta` 的 usage 合并处（`git grep -n "ServerToolUse\|message_delta" relay/channel/claude/relay-claude.go`），在合并完成后同样调用。**不改 `service/convert.go` 一行**——拒绝 400 行为保留（relaykit 迁移安全，见 3.7），只在 `claude_handler.go` 两个错误返回点包 `RewriteClaudeToolRejection` 补充可操作文案。
- [ ] **Step 4: 跑测试确认通过** → 含 `go test ./relay/channel/claude -v` 全量
- [ ] **Step 5: 提交**（`fix(claude): count streaming web search usage and clarify built-in tool rejection`）

### 任务 9：前端设置页

**Files**:
- Create: `web/default/src/features/system-settings/models/search-settings.tsx`
- Modify: `web/default/src/features/system-settings/.../section-registry.tsx`（注册条目）
- Modify: `web/default/src/features/system-settings/.../models/index.tsx`（分区入口）
- Modify: `web/default/src/lib/types.ts`（如有 `SearchSetting` 类型声明位）
- Modify: i18n locale 文件（经 `bun run i18n:sync` 生成）

**Interfaces**:
- Consumes: 任务 1 的 `search_setting.*` DB 键（经既有 `/api/option` 读写）。

- [ ] **Step 1: 先读三个锚点文件**（5 分钟，必做）：`web/default/src/features/system-settings/models/tool-price-settings.tsx`（组件与 `OPTION_KEY` 模式，`:19-30`）、同目录 `section-registry.tsx:113-144`（分区注册形状）、`models/index.tsx:39`（入口挂载）。
- [ ] **Step 2: 写组件**：`search-settings.tsx` 以 `tool-price-settings.tsx` 为骨架（同样的卡片容器、字段行、保存按钮走既有 option 批量更新）；字段 3.2 表 15 项——布尔用 Switch、数值用数字输入（`timeout_ms` min 500 max 60000、`max_results` min 1 max 20）、`provider`/`unmatched_tool_policy` 用 Select（选项与校验枚举一致）、`endpoint`/`credential_env`/`billing_tool_name`/`language` 用 Input；每个字段配 i18n key `settings.search.<field>`（英文 + 中文两份进 locale 文件）；`unmatched_tool_policy` 旁加说明文案（默认 reject = 保持现状 400；strip = 剥离后继续）。
- [ ] **Step 3: 注册分区**：在 `section-registry.tsx` 中复制 tool-price 条目的对象形状，替换 `id:"search"`、组件引用与标题 i18n key；`models/index.tsx` 对应入口同理。
- [ ] **Step 4: 验证**

```bash
cd web/default
bun run i18n:sync
bun run lint
bun run build
```
Expected: 三项全过；人工冒烟：dev 起服（`VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001 bun run dev --host 127.0.0.1 --port 5176`）打开系统设置→模型与计费→搜索，保存后刷新回读（亮/暗主题都看）。

- [ ] **Step 5: 提交**（`feat(web): search backend settings section`）

### 任务 10：文档与客户端配置指引

**Files**:
- Create: `docs/operations/search-backend.md`
- Modify: `README.md`（仅在「能力」列表加一行搜索端点，遵守短而可扫读）
- Modify: `docs/architecture/ai-tools-search-support-plan.md`（把第 5 节对应复选框勾上；实测校正处回写）

**内容骨架（必须写全）**：
1. 部署 SearXNG：`deploy/compose` 下新增 `search.yml`（服务 `searxng`，镜像 `searxng/searxng:latest`，ports `127.0.0.1:8888:8080`，env `SEARXNG_SECRET`，`formats: [json]` 的 `settings.yml` 挂载），独立 compose `-f` 引入，不与 dev-isolated 耦合；验证 `curl 'http://127.0.0.1:8888/search?q=test&format=json'` 有 results。
2. 管理员配置：系统设置→搜索（字段表 + 计费键说明）。
3. 各工具配置（精确到文件与键）：
   - Claude Code：默认路径零配置（开 `enable_anthropic_sidecar`）；CCR 模式 `~/.claude/settings.json` 的 `env` 加 `"CLAUDE_CODE_WEBSEARCH_USE_CCR_PROXY":"1"`、`"CLAUDE_CODE_WEBFETCH_USE_CCR_PROXY":"1"`，另加 `"skipWebFetchPreflight": true`。
   - Codex：`~/.codex/config.toml` 顶层 `web_search = "live"`（hosted，需非 lite 模型）；standalone：`[model_providers.local-newapi]` 加 `supports_standalone_web_search = true` + `[features] standalone_web_search = true`，并换掉 `gpt-6-luna` lite 默认模型。
   - Kimi：`~/.kimi/config.json` 的 `services.moonshot_search|fetch` 的 `base_url` 改 `http://127.0.0.1:3001/coding/v1/search|fetch`，`api_key` 用 new-api token；并注明若要走官方端点需恢复原厂 key。
   - Grok：`~/.grok/config.toml` 换非默认模型时补 `[model.<name>]` 并注 `supports_backend_search` 限制（hosted 本次不支持，写明边界）。
   - omp：`TAVILY_API_KEY` 直连（不经网关）或边车路径（经任务 7）。
   - dsh：断链修复任务另行记录（不属本计划范围）。
4. 验证记录模板：命令 / 退出码 / 结果 / 边界 → 落 `docs/reviews/CR-search-support-<date>.md`。

- [ ] 写完后跑 `git diff --name-only` 确认只有文档文件，提交 `docs(search): backend ops guide and per-tool client configuration`。

### 任务 11：集成验收与发布门

**Files**:
- Create: `docs/reviews/CR-search-support-<YYYY-MM-DD>.md`

**步骤（每步记录命令与退出码）**：

1. 起 SearXNG（任务 10 的 compose），`curl` format=json 返回 results。
2. 重建并起隔离环境（AGENTS.md 第 7 节命令），`git rev-parse HEAD` vs `docker exec ... /new-api --build-info` 对齐。
3. 端点冒烟（带有效 token）：`/coding/v1/search`、`/v1/alpha/search`、`/v1/code/sessions/x/worker/web-search` 各一次 → 200 且 results 非空；关闭开关后同路径 → 404 非 HTML。
4. 边车 curl 复刻（按 3.4 请求体打 `/v1/messages`）→ SSE 序列逐事件核对（可写进验收脚本比对任务 7 的 golden 断言）。
5. Kimi 实测：改配置后在 Kimi 里触发一次搜索，结果出现且非「搜索失败」。
6. Claude Code 实测：新会话直接让它 "use WebSearch to search X"，确认引用了搜索结果；再验证 CCR 模式（开两个 env）走 worker 端点。
7. 计费断言：验收前后查消费日志——每次搜索恰一条 `web_search_call_count`，quota=5000（默认价），无双计。
8. 回归：`go test ./controller ./model ./relay/common ./relay/helper ./service ./relay ./relay/channel/claude ./setting/operation_setting` 全绿。
9. 覆盖既有缺口验证（任务 8）：流式 Claude 搜索响应（真 Claude 渠道）后日志出现搜索费用。
10. 验收记录落 `docs/reviews/CR-search-support-<date>.md`（命令、退出码、通过/失败摘要、未覆盖边界）。

未通过第 3-7 任一步即不得发布；生产 13000 升级顺序：**先合并本计划阶段 1-7 并验收，再升级镜像**（否则旧构建的 200 假活变成硬 400，Claude Code 搜索直接明坏——见 1.1 表）。

---

## 6. 阶段划分与里程碑

| 阶段 | 内容 | 任务 | 验收 |
|---|---|---|---|
| 1 | 配置 + Provider + 计费 | 1、2、3 | 单测绿 |
| 2 | 三组显式端点 | 4、5、6 | curl 三种协议 200 + 关开 404 |
| 3 | 边车仿真（Claude Code 零配置主线） | 7 | golden SSE + 真实 Claude Code 实测 |
| 4 | 缺口修复 + 前端 | 8、9 | 流式计数有账、设置页可存取 |
| 5 | 文档 + 集成验收 + 发布 | 10、11 | CR 记录全绿后升级生产 |

Grok hosted、Responses `web_search` 仿真、Tavily/Brave provider、MCP 兜底：阶段 6 可选，另行开任务，不阻塞本计划。

---

## 7. 自查记录

- 需求覆盖：3.1 后端（任务 2）、3.2 配置（任务 1、9）、3.3 三端点（任务 4-6）、3.4 边车（任务 7）、3.5 计费含流式缺口（任务 3、8）、3.6 安全（任务 2/4 测试钉住）、3.7 迁移边界（任务 8 的「不改 convert.go」硬约束）、文档（任务 10）、验收（任务 11）。逐条可回指。
- 类型一致性：`Result/Options/Provider/Current/SearchSetting/ComputeSearchBill/BillSearchParams/detectWebSearchSidecar/answerWebSearchSidecar/RecordServerToolUsage/RewriteClaudeToolRejection` 在定义与消费任务间同名同签名。
- Review Focus 五个失败面各有归属任务的测试（见第 5 节开头列表）。
- 计费公式与 1.1 的实测常量一致（1 次 = 5000 quota）；SSE 序列与 1.2 的客户端消费白名单一致。
- 已知留待实测校正的两处（非占位，验收门强制执行）：CCR worker 端点请求体字段（任务 6/11 步骤 5-6）、Codex `/v1/alpha/search` 字段名（任务 5/11 步骤 3）——两者协议提取自二进制，字段名在验收时以真实客户端抓包为准回写本文档。

