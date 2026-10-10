# CR-AI-TOOLS-SEARCH-PLAN-2026-10-10

AI 工具（Claude Code / Codex / Kimi / Grok / omp / dsh）内置联网搜索支持方案的调研与核实。

- 调研对象：某外部 agent 产出的交接单（工作项 B：内置搜索），以及前序会话产出的初稿 `docs/architecture/ai-tools-search-support-plan.md`。
- 交付物：`docs/architecture/ai-tools-search-support-plan-v2.md`（388 行，执行依据）。初稿保留为调研记录。
- 方法：4 路并行只读调研（上游 `/v1/alpha/search` 实现、路由与 SSRF 与配置落点、Claude Code 二进制契约、Responses hosted 搜索与 Anthropic 官方约束），每条结论由主代理用 `git show` / `git grep` / 二进制偏移 / 容器与 curl 实测独立复现后才采信。
- 基线：本仓 HEAD `52d63c24e`（分支 dev）；上游固定引用 `789c97019`。本节全部命令在 2026-10-10 实跑。

## 1. 相对交接单与初稿的修正（本轮推翻或更正）

| # | 原说法 | 核实结论 | 证据（本轮实跑） |
|---|---|---|---|
| C1 | 交接单：「上游 `/v1/alpha/search` 已含搜索 provider，直接吸收即可」 | **推翻。上游是纯渠道透传**：`AlphaSearchHelper` 只放行 4 类渠道（Sub2API/NewAPI/Codex/AdvancedCustom），其余报错；再 `io.Copy` 回写上游响应。全文件 `searxng\|tavily\|serpapi\|brave\|google\|bing\|http.Get` 计数全为 0。吸收它只等于"转发给一个支持该端点的上游"，网关自己仍不会搜 | `git show 789c97019:relay/alpha_search_handler.go`（逐行数据流 + 关键词计数） |
| C2 | 交接单：「上游 9 种转换器注册表」 | 实为 **14 条**（6 直接 + 6 步进链 + 2 spec 变量） | `789c97019:relaykit/relayconvert/text_converter_registry.go` |
| C3 | 初稿：Claude Code 边车 system 提示词为 `"You are an assistant for performing a web search tool use"` | **该串在本机二进制 0 命中**。实际为 `"You run a web search for another Claude model, ..."`（2 处）。识别逻辑不得依赖此串（会随版本漂移） | 本机 `claude.exe` v2.1.293 偏移 81734296 / 210510179 |
| C4 | 初稿：非 Claude 形状渠道「约 16 个显式 400」 | 该错误文案实测 **7 个文件**（baidu/cloudflare/mokaai/tencent/jina/dify/mistral）；另有 `not implemented` 型 7 个、内嵌转发型 8 个。口径需重述 | 本仓 32 个 `ConvertClaudeRequest` 实现逐个分类 |
| C5 | 初稿：`parseClaudeToolsForOpenAI` 在 `service/convert.go:1178,1206-1207` | 定义在 **`:1192`**，拒绝分支在 **`:1221`** | `service/convert.go` 逐行扫描（本轮复跑） |
| C6 | 交接单：「dsh 符号链接目标不存在」 | **成立，对象要说准**：`dsh` CLI 自身符号链接正常；真正断链的是 `~/.dsh/profiles/node_modules` 下 **326** 个符号链接，目标全指向不存在的 `/Volumes/Work/code/deepseek-harness/...`（那是 npm 包上游仓库名，非本地目录） | `os.walk` + `is_symlink() and not exists()` → 326；目标示例 `/Volumes/Work/code/deepseek-harness/apps/cli/node_modules/...`（本轮复跑） |
| C7 | 交接单：「Anthropic 官方：search backend 不可配置，换后端用 MCP」 | **未找到官方出处，标为未证实。** 可核实官方事实是：搜索由 API 侧执行、工具无后端配置项、Bedrock 不支持。不作为方案约束 | platform.claude.com web-search-tool 文档；三版本工具串 |
| C8 | 初稿边车识别条件（`len(tools)==1` + `tool_choice` 强制 + 单条 user 消息） | **条件成立。** 二进制证实边车把 `web_search_20250305` 合并进 `tools`、`tool_choice:{type:"tool",name:"web_search"}`、单条 user 正文 `Perform a web search for the query: <q>` | 二进制偏移 205040657 / 210510987 / 81734252 |
| C9 | 初稿隐含：CCR worker 响应为通用 JSON | **schema 已从二进制取到**（zod 定义）：web-search → `{results?:[{title,url,snippet?}], error?}`；web-fetch → `{url, ..., text?, content_type?, error?}`（客户端取 `text` 为正文）。未端到端实测 | 二进制偏移 210503661 / 204171228 |
| C10 | 初稿：`use_responses_lite` 模型"跳过全部 hosted 工具" | **本仓源码查无实现**，属客户端行为，网关侧无对应逻辑，不作为服务端判定依据 | `git grep use_responses_lite` 仅 docs 与前端 |

## 2. 本轮新确立的现状事实（写入 v2 §1）

- 本机 Claude Code 的 `ANTHROPIC_BASE_URL = http://127.0.0.1:13000`，该端口是活的 new-api 实例（Docker 转发，`/api/status` 200 JSON）。故 Claude Code 的搜索边车请求**确实会打到网关**——初期某 agent 的"边车不发给上网关"结论在二进制数据流（`tools:jm` 合并点、base = 会话 `ANTHROPIC_BASE_URL`）与端到端配置双重证据下被推翻。
- 未注册路径兜底按前缀分叉：`/coding/*` 不在 `isRelayNotFoundPath` 的 `/v1`、`/api` 前缀内，落 `NoRoute` 返回 **200 SPA HTML**；`/v1/*` 返回 relay 404 JSON。`curl :3001/coding/v1/search` → 200 HTML（实测）。
- 计费口径：`common.QuotaPerUnit = 500*1000`（`common/constants.go:44`），`web_search` 默认 $10/1000 次 → 单次 5000 quota = $0.01。Claude 搜索计数仅在非流式写入（`relay/channel/claude/relay-claude.go:1379`），流式不计——是既有缺口。
- 密钥类配置键会被 `controller/option.go:80-85` 的 `GetOptions` 过滤（后缀 `Token/Secret/Key/secret/api_key`），新增密钥字段名不得命中这些后缀。
- 生产容器跑旧构建 `4527d7666...-litefix2`（2026-09-08），dev 容器 `52d63c24e`（= 当前 HEAD）。

## 3. 未覆盖（不得当作已解决）

1. CCR worker 端点契约仅来自二进制 zod 定义，未用真实 Claude Code + CCR 开关端到端实测。
2. Codex `/v1/alpha/search` 字段名来自前序引用，本轮未抓真实 codex 请求。
3. Kimi 协议来自源码引用，本机未实测（api_key 已换为 new-api token，会 401）。
4. Anthropic "search backend 不可配置" 无官方出处。
5. dsh 326 个断链的修复不在本计划范围。

## 4. 与交接单工作项 A 的边界

工作项 A（Grok `/v1/responses` 打到 Anthropic 原生渠道反复 500）根因是**协议转换缺失**（Claude 原生 adaptor 不实现 Responses→Messages 转换），属 relaykit 迁移范畴，与搜索方案无关，不在此处理。两者仅共享"真 Claude 渠道透传"这一条现状事实。

## 5. 交付状态

- 方案文档 `docs/architecture/ai-tools-search-support-plan-v2.md` 与初稿 `ai-tools-search-support-plan.md` 均已入库，`docs/architecture/README.md` 索引已登记。
- 本记录为调研与核实阶段；方案尚未落地，无代码改动。
