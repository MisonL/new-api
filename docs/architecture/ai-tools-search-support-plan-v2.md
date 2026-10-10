# AI 工具内置搜索支持：核实结论与实施方案（v2）

> **本文件与 `ai-tools-search-support-plan.md` 的关系**：后者是前序会话产出的初稿（1038 行，未提交）。本文件在其基础上做了独立调研与逐条核实，**修正了初稿与交接单中的若干错误结论**，并补齐了端点契约、计费落点与阶段判据。两份并存，以本文件为执行依据；初稿保留作为调研记录。
>
> **核实方式**：4 路并行只读调研（上游 `/v1/alpha/search` 实现、路由/SSRF/配置落点、Claude Code 二进制契约、Responses hosted 搜索与 Anthropic 官方约束），每条结论由本人用 `git show`/`git grep`/二进制偏移/容器实测独立复现后才采信。标注分三档：**已验证**（本轮实跑）、**引用**（前序实测，本轮未复现）、**未覆盖**。

**目标**：让 Claude Code、Codex、Kimi、Grok、omp、dsh 等 AI 工具的内置联网搜索经本网关（new-api）正常工作，并纳入既有工具按次计费体系。

**核心判断（本轮最重要的结论）**：这些工具的"搜索"分两类，**必须分开处理**：

- **服务端搜索**（Anthropic `web_search_20250305`、OpenAI Responses `web_search`）：搜索由**模型服务商**执行，网关只需**透传**或**转换协议**，本身不执行搜索。本仓库对 Anthropic 系渠道已透传；对 OpenAI Responses 透传；**真正缺的是"协议转换"与"本地仿真"两件事**，见通道 B。
- **客户端搜索**（Kimi `moonshot_search`、Codex standalone `/alpha/search`、Claude Code CCR 代理）：客户端**自己发一条 HTTP 请求**给某个端点，端点返回搜索结果。这类端点**必须由网关（或某个后端）真实实现**，否则搜索功能整体不可用。

**本机事实**：当前没有任何搜索服务或 API key（`git grep` 全仓无 Tavily/SearXNG/Serper 客户端；本机 `~/.codex`、`~/.kimi`、`~/.omp` 均未配置搜索凭据）。因此第一阶段是**起搜索后端**，第二阶段才是网关对接。

---

## 0. 本轮核实结论（相对交接单与初稿的修正）

下表是本次调研推翻或修正的结论，**执行前必读**——避免按错误前提动手。

| # | 交接单/初稿的说法 | 本轮核实结论 | 证据 |
|---|---|---|---|
| C1 | 「上游 `/v1/alpha/search` **已含搜索 provider 实现**，直接吸收即可」（交接单待确认项） | **否。上游是纯渠道透传，不含任何本地搜索后端。** 该端点把请求原样发给上游渠道（Codex 的 `backend-api/codex/alpha/search`、下游 new-api 的 `/v1/alpha/search` 等），再 `io.Copy` 回写；全文件无搜索引擎调用。**吸收它只等于"把请求转发给一个支持该端点的上游"，不等于"网关自己能搜"** | `789c97019:relay/alpha_search_handler.go:21-118`（逐行数据流）；`789c97019:relay/channel/codex/adaptor.go:148-149`；上游全仓 `searxng\|tavily\|serpapi` 零命中（唯一 `SearchProvider` 命中是前端 React Context） |
| C2 | 上游有"9 种转换器注册表"（交接单） | 注册表实为 **14 条**（6 条直接转换 + 6 条 `requestConverter*` 步进链 + 2 条 `spec` 变量），覆盖 4 格式的全部 12 个有序对 | `789c97019:relaykit/relayconvert/text_converter_registry.go`，`ID:` 计数 14 |
| C3 | 初稿 §1.2：Claude Code 边车 system 提示词为 `"You are an assistant for performing a web search tool use"` | **该串在本机二进制中 0 命中。** 实际为 `"You run a web search for another Claude model, which is waiting on the result and can search again. Search once, then reply with a brief summary of the results."`（2 处，UTF-16 串池） | 本机 `claude.exe`（v2.1.293，238MB，Bun 打包）偏移 81734296 / 210510179；`grep -a -c -F` 前者 0、后者 2 |
| C4 | 初稿 §1.1：「非 Claude 形状渠道（baidu/tencent 等**约 16 个**）显式 400」 | 该错误文案（`Claude Messages conversion is not supported by X channel`）**实测 7 个文件**（baidu/cloudflare/mokaai/tencent/jina/dify/mistral）。另 `not implemented` 型 7 个、`openai.Adaptor`/`claude.Adaptor` 内嵌转发型 8 个——口径需重述，不能笼统说"16 个显式 400" | 本仓 32 个 `ConvertClaudeRequest` 实现逐个分类（本轮扫描 `relay/channel/**`） |
| C5 | 初稿 §1.1：`parseClaudeToolsForOpenAI` 在 `service/convert.go:1178,1206-1207` | 函数定义在 **`:1192`**，拒绝分支在 **`:1221`**（`toolType != "" && toolType != "custom"` → 报错）。行号需修正 | `grep -n "cannot be converted to an OpenAI function tool" service/convert.go` → `:1221` |
| C6 | 交接单：「dsh 的符号链接目标不存在（deepseek-harness）」 | **成立，但对象要说准**：`dsh` CLI 自身的符号链接是好的（指向 `@deepseek-ai/dsh/lib/bin.js`，目标存在）；真正断链的是 **`~/.dsh/profiles/node_modules` 下的 326 个符号链接**，目标全指向不存在的 `/Volumes/Work/code/deepseek-harness/...`。`deepseek-harness` 是 npm 包的**上游仓库名**，不是本地目录 | `find ~/.dsh/profiles/node_modules -xtype l \| wc -l` → 326；`readlink` 示例目标 `/Volumes/Work/code/deepseek-harness/apps/cli/node_modules/...`；`ls /Volumes/Work/code/deepseek-harness` → No such file |
| C7 | 交接单：「Anthropic 官方立场：search backend is not configurable，换后端用 MCP」 | **未找到官方出处，标注为未证实。** 可核实的官方事实是：搜索由 API 侧执行（"The API runs the searches"）、工具参数无后端配置项、Bedrock 不支持该工具。**不应把"官方禁止换后端"当作既定约束写进方案** | platform.claude.com 官方 web-search-tool 文档；三版本 `web_search_20250305` / `web_search_20260209` / `web_search_20260318` |
| C8 | 初稿 §3.4 边车识别条件（`len(tools)==1` + `tool_choice` 强制 + 单条 user 消息） | **条件成立。** 二进制证实：边车把 `web_search_20250305` 合并进 `tools` 数组（`lK=[...extraToolSchemas]; jm=[...hr,...lK]; ...tools:jm`）、`tool_choice:{type:"tool",name:"web_search"}`、单条 user 消息正文 `Perform a web search for the query: <q>`。**但该系统提示词会随版本漂移，识别逻辑不得依赖它**（见 C3） | 二进制偏移 205040657（`tools:jm` 合并点）、210510987（`extraToolSchemas:[R]` + `toolChoice`）、81734252（`Perform a web search for the query: `） |
| C9 | 初稿隐含：CCR worker 端点响应为通用 JSON | **响应 schema 已从二进制取到（zod 定义）**：web-search → `{results?:[{title,url,snippet?}], error?}`；web-fetch → `{url, destination_url?, title?, text?, content_type?, error?}`（**客户端取 `text` 为正文，不是 HTML/SSE**）。请求体：`{query, allowed_domains?, blocked_domains?, search_profile?}`。此前的"未端到端验证"风险大幅降低 | 二进制偏移 210503661（web-search schema）、204171228（web-fetch schema `z$o`） |
| C10 | 初稿：`use_responses_lite` 模型"直接跳过全部 hosted 工具" | **本仓库源码内查无实现**（该词仅出现在 docs 与 `tools/codex-bridge/src/catalog.js`）。属**客户端行为**，网关侧无对应逻辑——方案不应依赖它做服务端判定 | `git grep supports_standalone_web_search\|SupportsStandaloneWebSearch` 零命中；`git grep use_responses_lite` 仅 docs/前端 |

---

## 1. 已核实的现状事实

### 1.1 网关侧（本仓 HEAD `52d63c24e`）

| 事实 | 证据 |
|---|---|
| 无任何自建搜索端点：`/coding/**`、`/v1/alpha/search`、`/v1/code/sessions/**` 全仓零命中 | `git grep`；`curl -X POST :3001/coding/v1/search` → **200 SPA HTML**（实测） |
| 未注册路径的兜底行为按前缀分叉 | `/coding/*` 不在 `isRelayNotFoundPath` 的 `/v1`、`/api` 前缀内 → 落 `NoRoute` 返回 200 HTML；`/v1/*` → relay 404 JSON（`curl :3001/v1/alpha/search` → 404 JSON，实测）。`router/web-router.go:66,89-102`、`controller/relay.go:3558` |
| Claude `/v1/messages` 入口三分支：Responses 转换（全局策略默认关）/ 透传 body / adaptor 转换 | `relay/claude_handler.go:24-215`（`:131` 转换分支、`:153` 透传、`:158` adaptor）；`setting/model_setting/global.go:150-152`（`Enabled:false`） |
| 带 `web_search_20250305` 的 Claude 请求：Claude 系渠道**原样透传** | `relay/channel/claude/adaptor.go:28-30`（`ConvertClaudeRequest` 直接 `return request, nil`） |
| 非 Claude 形状渠道**显式拒绝**（文案与数量见 C4） | 32 个 `ConvertClaudeRequest` 实现，如 `relay/channel/baidu/adaptor.go:27-29` |
| 转 OpenAI 时 `parseClaudeToolsForOpenAI` 对 `type != "custom"` **显式报错** | `service/convert.go:1192`（定义）、`:1221`（拒绝）；注释 `:83-86` 说明"内建 server 工具没有忠实的 OpenAI function 表达，宁可拒绝不伪装" |
| 反向（OpenAI→Claude）会**注入** `web_search_20250305` | `relay/channel/claude/relay-claude.go:343-346`（`WebSearchOptions` 非空时构造 `{"type":"web_search_20250305","name":"web_search"}`）。方向不对称 |
| Responses hosted 工具（`web_search`）在转 chat 时**显式拒绝** | `service/openaicompat/responses_to_chat_request.go:547-548`（`tool type %q is not supported in chat compatibility mode`） |
| 工具定价体系完整：默认 `web_search`/`web_search_preview` 各 10.0（美元/1000 次），支持 `tool:model*` 前缀覆盖 | `setting/operation_setting/tools.go:26-37`、`:142`（`GetToolPriceForModel`）、`:78`（`ValidateToolPriceJSON`） |
| 真实结算链：`PostTextConsumeQuota` → `calculateTextToolCallSurcharge` | `service/text_quota.go:877`（入口）、`:106-135`（工具附加费，`price/1000*count*groupRatio*QuotaPerUnit`）；`common/constants.go:44`（`QuotaPerUnit = 500*1000`）。1 次 = 5000 quota = $0.01 |
| Claude 搜索计数仅在**非流式**写入 | 写入点 `relay/channel/claude/relay-claude.go:1379`（`c.Set("claude_web_search_requests", ...)`），读取点 `service/text_quota.go:131`。**流式不计** |
| hosted 工具计数字段齐备 | `relay/common/relay_info.go:81-92`（`BuildInToolInfo`/`ResponsesUsageInfo`）、`:696-697`（canonical 归一）；`dto/openai_response.go:478-480`（`BuildInToolWebSearch`/`Preview`/`Preview2025_03_11`）；计数点 `relay/channel/openai/relay_responses.go:1031,1083` |
| 无"渠道是否支持 hosted web_search"的能力位 | `dto/channel_settings.go:83-94,317-360` 的能力位只有 function/custom/namespace/tool_search；`supports_standalone_web_search` 零命中 |
| SSRF 框架可复用：私网/保留段拦截、域名/IP 黑白名单、DNS pin、端口白名单 | `common/ssrf_protection.go:204`（`isPrivateIP`）、`:522-571`（`ValidateURLWithFetchSetting*`）、`:27-45`（`WithSSRFValidation`）；配置 `setting/system_setting/fetch_setting.go:5-25`（默认 `EnableSSRFProtection=true`、`AllowPrivateIp=false`、`AllowedPorts={80,443,8080,8443}`）。现成范式 `service/download.go:53-74` |
| 搜索限流中间件已存在 | `middleware/rate-limit.go:352`（`SearchRateLimit()`）；env 配置 `common/init.go:145-147`（默认 10 次/60 秒/用户、管理员豁免） |
| 配置机制：分层注册式（struct + `config.GlobalConfig.Register`） | 范例 `setting/system_setting/fetch_setting.go:5-34`；写入 `model/option.go:724-760`（`handleConfigUpdate` 反射）；校验 switch `model/option.go:688`（`case "tool_price_setting.prices"`） |
| **密钥类键被 `GetOptions` 过滤**（前端读不回） | `controller/option.go:74-104`，`:80-85` 后缀判定（`Token`/`Secret`/`Key`/`secret`/`api_key`）。**新增密钥字段名不得以这些后缀结尾**，或走"只写不读" |
| 生产容器跑旧构建 | `docker exec new-api /new-api --build-info` → `4527d7666...-litefix2`（2026-09-08）；dev 容器 → `52d63c24e...-dirty`（= 当前 HEAD） |

### 1.2 客户端协议事实

**Claude Code v2.1.293**（本机 `~/.nvm/.../@anthropic-ai/claude-code/bin/claude.exe`，238MB Bun 单文件）：

- **默认路径（边车）**：WebSearch 不在主对话 `tools` 里，而是客户端另发一条自包含请求：单条 user 消息（`Perform a web search for the query: <q>`）、`web_search_20250305` 经 `extraToolSchemas` 合并进 `tools` 数组、`tool_choice:{type:"tool",name:"web_search"}`、`max_uses:8`、`thinking:{type:"disabled",mechanical:true}`、`enablePromptCaching:false`。**该系统提示词随版本漂移，识别逻辑不得依赖它**（C3）。请求走常规 messages 流、base = `ANTHROPIC_BASE_URL`，**因此会打到网关**。
- **CCR 代理路径**：环境变量 `CLAUDE_CODE_WEBSEARCH_USE_CCR_PROXY` / `CLAUDE_CODE_WEBFETCH_USE_CCR_PROXY`（各 3 处命中），门控条件 `Pe()==="firstParty" && env && gDe(session)`。端点 `POST {base}/v1/code/sessions/{sessionId}/worker/{route}`，`route ∈ {web-search, web-fetch}`（**运行时拼接，二进制内无字面量**）。响应 schema 见 C9。超时 40s、最多 4 次重试（仅 429/5xx）、总预算 75s。
- **客户端消费的流事件**：`content_block_start`（`server_tool_use` 记 id、`web_search_tool_result` 收结果）、`content_block_delta`（`input_json_delta` 抠 query）。`web_search_result` 为结果条目类型；`encrypted_content` 出现在服务端错误文案与 compaction 解析中，**本地仿真不必产出**（客户端不依赖）。

**Codex CLI**：standalone 路径 `POST {base}/alpha/search`（需 provider `supports_standalone_web_search = true`）；hosted 路径为 Responses `tools:[{type:"web_search"}]`（本次不做）。

**Kimi CLI**：`services.moonshot_search` → `POST {base}/coding/v1/search`，响应 `{search_results:[{site_name,title,url,snippet,content,date,icon,mime}]}`；`services.moonshot_fetch` → `POST {base}/coding/v1/fetch`，纯文本 markdown（**引用**，前序实测；本机两个 base_url 仍指 `https://api.kimi.com`，api_key 已换成 new-api token）。

**Grok / omp / dsh**：Grok 走 `/v1/responses` hosted（本次不做）；omp 有 `TAVILY_API_KEY` 与 `ANTHROPIC_SEARCH_BASE_URL` 两条现成路径（**已验证**：`~/.omp` 与 `~/.config/omp*` 均未配置）；dsh 需先修 `~/.dsh/profiles/node_modules` 的 326 个断链（C6）。

### 1.3 上游与迁移背景

- 上游 PR #7690（`acb347a19`，2026-10-07）做的是"按渠道编码厂商搜索参数 + 厂商上报搜索计数计费"，**仍依赖厂商搜索能力，不含网关自建搜索**。本 fork 无 `relaykit/`、无 `relay/common/tool_usage.go`，原样合并不可行（**已验证**）。
- relaykit 迁移规格（`docs/superpowers/specs/2026-09-30-relaykit-migration-design.md:66`）已决定 hosted 工具转换采用上游 `toolconv`（含 `KindWebSearch`，双向完整编解码），阶段 1 未落地。**本方案的代码不得放进将随迁移搬迁的转换热区**（`service/convert.go` 主体、`relay/common/` 协议转换件）。

---

## 2. 架构

```
AI 工具客户端
  Claude Code ──┬─ 默认: /v1/messages 边车(web_search_20250305)   → 通道 B 本地仿真
                └─ CCR : /v1/code/sessions/{id}/worker/web-search|fetch → 通道 A 显式端点
  Codex       ──── /v1/alpha/search (standalone)                → 通道 A 显式端点
  Kimi        ──── /coding/v1/search | /coding/v1/fetch         → 通道 A 显式端点
  Grok/Codex  ──── /v1/responses hosted web_search              → 通道 C 透传(本次不改)
  omp         ──── TAVILY 直连(不经网关) 或 边车                 → 不涉及 / 通道 B
  dsh         ──── 待断链修复                                     → 未覆盖

        ┌───────────────────────┬────────────────────────┐
        │ 通道 A 显式 JSON 端点  │ 通道 B 边车仿真          │
        │ controller/search_*    │ relay/claude_search_*   │
        │ · TokenAuth + 限流     │ · 识别 Claude 边车请求   │
        │ · 按次计费             │ · 本地执行搜索，不调上游模型│
        │ · SSRF 复用(仅 fetch)  │ · 回吐 Anthropic SSE     │
        └───────────┬───────────┴───────────┬────────────┘
                    │                       │
             service/search（新增）          │
             · Provider 接口(Search/Fetch)  │
             · SearxngProvider(首个实现)     │
                    │                       │
              SearXNG（自建，无 key）────────┘

  通道 C：真 Claude 渠道 / OpenAI Responses hosted —— 保持现状透传，本方案不改
```

**请求路径矩阵**：

| 客户端 | 触发路径 | 网关处理 | 阶段 |
|---|---|---|---|
| Claude Code（默认） | `/v1/messages` 边车 | 本地仿真 SSE，不调上游 | 3 |
| Claude Code（CCR） | `/v1/code/sessions/{id}/worker/web-search\|fetch` | 显式端点 | 2 |
| Codex（standalone） | `/v1/alpha/search` | 显式端点 | 2 |
| Kimi | `/coding/v1/search\|fetch` | 显式端点 | 2 |
| omp | TAVILY 直连（不经网关）或边车 | 不涉及 / 通道 B | 3 |
| Grok / Codex（hosted） | `/v1/responses` `tools:[web_search]` | 本次不改（透传或拒绝由渠道能力决定） | 6 可选 |
| 真 Claude 渠道 | `/v1/messages` 内置搜索工具 | 透传（现状） | 不改 |
| 非 Claude 渠道 + 内置工具 | `/v1/messages` | 现状拒绝（C4）；本方案不改，仅在错误文案上加指引 | 不改 |
| dsh | 断链修复后再评估 | 未覆盖 | — |

---

## 3. 详细设计

### 3.1 `service/search` 包

```go
package search

type Result struct {
    Title    string `json:"title"`
    URL      string `json:"url"`
    Snippet  string `json:"snippet"`
    SiteName string `json:"site_name,omitempty"`
    Date     string `json:"date,omitempty"`
}

type Options struct {
    MaxResults     int
    Language       string
    Recency        string   // "", "day", "week", "month", "year" → SearXNG time_range
    AllowedDomains []string
    BlockedDomains []string
}

type Provider interface {
    Name() string
    Search(ctx context.Context, query string, opt Options) ([]Result, error)
    Fetch(ctx context.Context, rawURL string) (string, error) // 返回 markdown 文本
}
```

**决策 ADR-1**：Provider 实例每次调用按当前配置构建，不做进程级缓存。理由：构造只是结构体赋值，HTTP 传输用包级共享 `http.Client`；不做缓存就不必在 `model/option.go` 建"配置变更 → 重建缓存"回调（`model` 包不能反向 import `service`）。超时统一走 `context.WithTimeout`。

**失败语义**：Provider 调用失败必须返回错误，**禁止返回空结果冒充成功**；调用方把错误显式回给客户端。

### 3.2 配置面

新增 `setting/operation_setting/search.go`，注册名 `search_setting`（DB 键前缀 `search_setting.`），照抄 `setting/operation_setting/tools.go` 的注册模式：

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `enabled` | bool | false | 总开关 |
| `provider` | string | `"searxng"` | 后端标识；未知值 = 配置错误，请求时显式失败 |
| `endpoint` | string | `""` | SearXNG 基地址，如 `http://127.0.0.1:8888` |
| `credential_env` | string | `""` | 需 key 的后端（Tavily 等）的**环境变量名**。**不存 key 本身** |
| `timeout_ms` | int | 8000 | 单次超时，校验 500-60000 |
| `max_results` | int | 8 | 结果上限，校验 1-20（Kimi 协议上限 20） |
| `language` | string | `""` | 传给后端的语言偏好 |
| `allow_fetch` | bool | false | 是否开放 fetch 端点（抓任意 URL 风险更高，默认关） |
| `enable_coding_endpoints` | bool | false | `/coding/v1/*` 开关 |
| `enable_worker_endpoints` | bool | false | `/v1/code/sessions/*/worker/*` 开关 |
| `enable_codex_standalone` | bool | false | `/v1/alpha/search` 开关 |
| `enable_anthropic_sidecar` | bool | false | Claude 边车仿真开关 |
| `billing_tool_name` | string | `"web_search"` | 计费价格键 |
| `unmatched_tool_policy` | string | `"reject"` | 非边车请求携带内置搜索工具时的策略：`reject`（400）或 `strip`（剥离并继续） |

校验钩子加在 `model/option.go:688` 现有 switch 相邻处（照 `case "tool_price_setting.prices"` 模式）：`endpoint` 必须是合法 http/https URL；`provider`、`unmatched_tool_policy` 枚举校验；数值范围校验。**校验失败返回错误、不落库**。

**密钥约束（已验证）**：`credential_env` 存的是变量名不是密钥，不以敏感后缀结尾，可正常读写；若将来要存 key 本身，字段名会命中 `controller/option.go:80-85` 的过滤 → 前端读不回，须走"只写不读"。

### 3.3 通道 A：显式端点契约

统一约定：全部 `POST`；`Content-Type: application/json`（fetch 响应除外）；鉴权 `middleware.TokenAuth()`（Bearer；**注意 `x-api-key` 映射只在 path 含 `/v1/messages` 或 `/v1/models` 时生效**，见 `middleware/auth.go:367-372`，本组端点若要用 Anthropic 风格 key 需扩展该判断）；限流 `middleware.SearchRateLimit()`；**不挂 `Distribute`**（无 model 会 400）；请求体上限 64KB（超限 413）。

**Kimi**（`enable_coding_endpoints`）：

```
POST /coding/v1/search
请求: {"text_query":"...","limit":8,"enable_page_crawling":false,"timeout_seconds":30}
响应: {"search_results":[{"site_name","title","url","snippet","content","date","icon","mime"}]}
错误: HTTP 502 {"error":{"message":"...","type":"search_backend_error"}}

POST /coding/v1/fetch
请求: {"url":"https://..."}
响应: 纯文本 markdown，Content-Type: text/markdown; charset=utf-8
```

**Codex standalone**（`enable_codex_standalone`）：

```
POST /v1/alpha/search
请求: {"id","model","input","commands":[{"type":"search_query","query":"..."}],"max_output_tokens":null}
响应: {"output":"<面向模型的文本摘要>","results":[{"title","url","snippet"}]}
```

处理规则：遍历 `commands`，`type == "search_query"` 取 `query`（兼容 `q`）逐条搜索；其余 command（`open`/`click`/`find` 等）返回 501 + 明确文案。`commands` 元素上限 16。

**Claude Code CCR**（`enable_worker_endpoints`，契约来自二进制，见 C9）：

```
POST /v1/code/sessions/{id}/worker/web-search
请求: {"query","allowed_domains?","blocked_domains?","search_profile?"}
响应: {"results":[{"title","url","snippet?"}], "error":null}   // 错误时 error 字段填 {error_type,error_message}
      客户端超时 40s；错误信封 error_type 须匹配 ^\w{1,64}$ 才透传

POST /v1/code/sessions/{id}/worker/web-fetch
请求: {"url"}
响应: {"url","destination_url?","title?","text","content_type?","error?}
      客户端取 text 为正文，content_type 缺省视为 text/plain
```

`search_profile` 接受但忽略（记入日志）；fetch 内核与 Kimi fetch **复用同一私有函数**，禁止复制两份 SSRF 逻辑。

### 3.4 通道 B：Claude 边车仿真（本次最高价值项）

**识别条件**（`relay/claude_search_sidecar.go`，保守判定，宁漏勿误）：

1. `len(req.GetTools()) == 1`，且该工具 `type` 以 `web_search` 开头（覆盖 `web_search_20250305`）；
2. `tool_choice` 反序列化后 `Type == "tool"` 且 `Name` 等于该工具 `name`；
3. `len(Messages) == 1`、`Role == "user"`、正文以 `Perform a web search for the query:` 开头（用于提取 query）。

三条全中 → 本地应答。**1、2 命中而 3 不中**（客户端换措辞）：**不回退到上游**（禁止静默降级），按 `unmatched_tool_policy` 处理（默认 400，文案注明"内置搜索工具只支持边车形态"）。**不依赖 system 提示词**（C3：该串随版本漂移）。

**拦截位置**：`relay/claude_handler.go` 的 `ClaudeHelper` 开头，早于 `:131` 的 Responses 分支与 `:158` 的 `adaptor.ConvertClaudeRequest`。仅当 `enable_anthropic_sidecar` 开启且识别命中时旁路；否则完全走原路径。

**流式响应**（`text/event-stream`，逐事件 `Flush()`）：

```
event: message_start
data: {"type":"message_start","message":{"id":"msg_<rand>","type":"message","role":"assistant","model":"<req.model>","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srvtoolu_<rand>","name":"web_search","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"<escaped>\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srvtoolu_<同一 id>","content":[{"type":"web_search_result","title":"...","url":"https://..."}]}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":0}}

event: message_stop
data: {"type":"message_stop"}
```

关键约束：`server_tool_use.id` 与 `web_search_tool_result.tool_use_id` 必须一致；index 从 0 递增；每事件形如 `event: <type>\ndata: <单行JSON>\n\n`；**不产出 `encrypted_content`**（客户端不依赖）。

**非流式**：`stream != true` 时返回等价单条 JSON message（同内容的 `content` 数组）。

**失败语义**：搜索后端失败时，流式返回一个 `event: error` 帧后停止，绝不返回空结果的"成功"。**错误帧必须含非空 message**（仓库既有教训：空错误帧会中止健康流）。

### 3.5 计费

统一按次计费（与 `service/text_quota.go:106-135` 同口径）：

```
quota = round(price_per_1k / 1000 * call_count * common.QuotaPerUnit * groupRatio)
price_per_1k = operation_setting.GetToolPriceForModel(billing_tool_name, modelName)
groupRatio   = ratio_setting.GetGroupRatio(c.GetString("group"))
```

- `billing_tool_name` 默认 `web_search`（$10/1000 次 → 单次 5000 quota = $0.01）；管理员可覆盖，或改用自定义键（如 `searxng_search`）实现自建搜索免费/自定义定价。
- 价格未配置（返回 0）→ 不计费但仍记日志（自建 SearXNG 免费是合法配置）。
- 扣费与落库：`model.GetUserQuota` 校验余额（不足 402，不执行）→ `model.DecreaseUserQuota` → `model.RecordConsumeLog`（`Other` 记 `web_search_call_count`、`search_provider`、`search_endpoint`）。
- 一次 HTTP 请求 = 一次搜索调用（Kimi 的 `limit` 只影响结果条数，不影响调用次数）。
- **既有缺口修复**：Claude 流式路径的 `server_tool_use.web_search_requests` 目前不计数（`relay-claude.go:1379` 只在非流式），补齐流式 `message_delta` 的 usage 解析与计数写入。

### 3.6 安全

- **fetch 端点必须复用既有 SSRF 框架**：用户传入的 URL 走 `common.ValidateURLWithFetchSetting`（`common/ssrf_protection.go:522`）+ `common.WithSSRFValidation`，范式照 `service/download.go:63-72`。
- **搜索后端 base_url 属可信配置**（管理员填），**不打** `WithSSRFValidation`——否则内网部署会被默认端口白名单/私网拦截打死（`service/http_client.go:107-109` 已有同款理由）。
- 抓取响应长度上限 1MB、超时复用 `timeout_ms`、重定向上限 5 次、仅接受 `text/html` 与 `text/plain`（其余 415）。
- 密钥：`credential_env` 只存环境变量名，不落库；日志与错误信息不得回显 key。
- 限流：`SearchRateLimit()`（默认 10 次/60 秒/用户，管理员豁免）。
- 请求体上限 64KB；`/v1/alpha/search` 的 `commands` 数组上限 16。

### 3.7 路由落点（本轮修正）

- 新建 `router/search-router.go`，导出 `SetSearchRouter(router *gin.Engine)`；在 `router/main.go` 的 `SetWebRouter`（第 26 行）**之前**调用，继承 relay 的 engine 级中间件链。
- **`/coding` 前缀必须加入 `router/web-router.go:97-102` 的 `isRelayNotFoundPath`**（并补 `web-router_test.go` 用例）——否则打错路径会拿到 200 HTML，正命中"客户端不校验 Content-Type"的伪成功场景（已实测 `/coding/v1/search` 当前返回 200 HTML）。
- 端点组只挂 `RouteTag("relay")` + `TokenAuth()`（+ `SearchRateLimit()`），**不挂 `Distribute`**（无 model）。

### 3.8 与上游 #7690、relaykit 迁移的关系

- 本方案是"网关自建搜索"，#7690 是"厂商搜索参数编码与计费"，互补不冲突。**不吸收 #7690 的 relaykit 侧实现**（本 fork 无 relaykit）。
- 计费调用集中在 `service/search_billing.go` 一个文件，迁移时只改该文件。
- **不改 `service/convert.go` 主体**。唯一改动是任务 8 的错误文案包装（只增不改，远离 relaykit 迁移补丁目标函数）。若冲突，退路是"策略分支移到 `claude_handler.go` 的调用点"。

---

## 4. 文件结构与改动面

新增：

```
service/search/provider.go              接口、Result/Options、Current() 与错误
service/search/searxng.go               SearXNG 实现（Search + Fetch）
service/search/searxng_test.go
service/search/provider_test.go
service/search_billing.go               按次计费（唯一计费入口）
service/search_billing_test.go
setting/operation_setting/search.go     SearchSetting 注册与访问器
setting/operation_setting/search_test.go
controller/search_endpoints.go          三组显式端点处理器
controller/search_endpoints_test.go
relay/claude_search_sidecar.go          边车识别 + SSE/JSON 应答
relay/claude_search_sidecar_test.go
relay/channel/claude/server_tool_usage.go   流式搜索计数（任务 8）
router/search-router.go                 端点注册（含 /coding 前缀修正）
web/default/src/features/system-settings/models/search-settings.tsx
```

修改：

```
router/main.go                         在 SetWebRouter 前调用 SetSearchRouter
router/web-router.go                   isRelayNotFoundPath 加 /coding 前缀
relay/claude_handler.go                开头插入边车旁路（约 6 行）+ 错误文案包装
relay/channel/claude/relay-claude.go   任务 8：流式计数字段补齐
model/option.go                        校验钩子
web/default/.../section-registry.tsx   注册 search 分区
```

不改：`service/convert.go` 主体、`relay/common/` 协议转换件、既有透传路径。

---

## 5. 阶段划分与验收

| 阶段 | 内容 | 验收 |
|---|---|---|
| 0 | 起 SearXNG（Docker，`settings.yml` 开 `search.formats: [html, json]`，`127.0.0.1:8888`） | `curl 'http://127.0.0.1:8888/search?q=test&format=json'` 有 results |
| 1 | 配置 + Provider + 计费（`search_setting`、`service/search`、`search_billing`） | 单测绿：Provider 故障/超时上抛、计费公式精确、未知 provider 显式失败 |
| 2 | 三组显式端点（`/coding/v1/*`、`/v1/alpha/search`、`/v1/code/sessions/*/worker/*`） | curl 三种协议 200 + results；关闭开关 → 404 非 HTML；未登录 → 401 JSON |
| 3 | 边车仿真（Claude Code 零配置主线） | golden SSE 逐事件断言 + 真实 Claude Code 实测 |
| 4 | 缺口修复 + 前端设置页 | 流式计数有账；设置页可存取（亮/暗主题） |
| 5 | 文档 + 集成验收 + 发布 | CR 记录全绿后升级生产 |

**阶段 2 的验收必须包含**（本轮新增，针对 C1 与 C4）：

1. `/coding/v1/search` 在**未注册时**返回 200 HTML 的现状，注册后必须变为鉴权/业务响应——用 `--path-as-is` + body 检查，不能只看状态码（仓库既有教训：SPA 兜底 200 会掩盖实际处理者）。
2. 三端点关闭开关时必须是 404 **且 Content-Type 非 text/html**。
3. 非 Claude 渠道 + 内置工具的拒绝文案（任务 8）须指向 `enable_anthropic_sidecar` 配置项。

**Grok hosted、Responses `web_search` 仿真、Tavily/Brave provider、MCP 兜底**：阶段 6 可选，另行开任务。

---

## 6. 未覆盖与待实测（不得当作已解决）

1. **CCR 端点契约未端到端验证**：schema 来自二进制 zod 定义（C9），未用真实 Claude Code + CCR 开关实测。阶段 2 验收须抓包校正。
2. **Codex `/v1/alpha/search` 字段名**：来自引用（前序实测），本轮未抓真实 codex 0.160.0 请求。阶段 2 验收须校正。
3. **Kimi 协议**：来自源码引用，本机未实测（api_key 已换，会 401）。
4. **`use_responses_lite` 跳过 hosted 工具**：属客户端行为，本仓源码查无实现（C10），**网关侧不做判定**。
5. **"Kimi fetch 对 200 不校验 Content-Type"**：客户端行为，仓库内无证据（未证实，但结论保守无害）。
6. **Anthropic "search backend 不可配置"**：无官方出处（C7），不作为方案约束。
7. **dsh**：断链修复（326 个链接）不在本计划范围。
8. **上游 `/v1/alpha/search` 的吸收**：本方案**不吸收**上游的渠道透传实现（C1），而是自建本地搜索端点。若将来要让 Codex 走"转发到上游 Codex 渠道"，那是另一条独立路径，与自建搜索不冲突。

---

## 7. 与交接单工作项 A 的边界

交接单 A 项（Grok `/v1/responses` 打到 Anthropic 渠道 → 500）根因是**协议转换缺失**（Claude 原生 adaptor 不实现 Responses→Messages 转换），属 relaykit 迁移范畴，**与本搜索方案无关**，不在此处理。两者仅共享"真 Claude 渠道透传"这一条现状事实。
