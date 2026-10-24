# CR: 全场景多维度审查（agent 并行 + 逐条复核）（2026-10-25）

## 审查方式

- 18 个细分维度并行派发 agent 静态审查，HEAD `8f9a40ef8`。
- 所有 P0/P1 论断由主会话逐条独立复核；能复现的用独立模块或实测复现。
- 环境隔离开发容器 `new-api-dev-isolated-new-api-1`（构建于 2026-10-24T15:26Z，与 HEAD 一致）。
- 生产容器全程只读。

## 一、已复现缺陷（独立复现，非转述）

### P1 配置写入坏 JSON 清空内存倍率表并持久化坏值

- `types/rw_map.go:77-93`：`LoadFromJsonString` / `LoadFromJsonStringWithCallback` 先
  `m.data = make(map[K]V)` 再 `Unmarshal`；解析失败时旧数据已丢失。
- 复现（独立模块调用 `ratio_setting.UpdateModelRatioByJSONString('{"gpt-4": 15.0,')`）：
  返回 `unexpected end of JSON input`，随后 `ModelRatio2JSONString()` 为 `{}`，
  `GetConfiguredModelRatio("gpt-4")` 为 `(0,false)`。
- 放大链：`controller/option.go` 对 `ModelRatio`/`ModelPrice`/`CompletionRatio`/`CacheRatio`
  无前置校验，进入 `model.UpdateOption`；后者先 `DB.Save`（model/option.go:245）再
  `updateOptionMap`，于是坏值落库、内存清空、接口返回失败。此后每 60 秒 `SyncOptions`
  重新加载坏值并再次清空，直到人工写入合法值。所有模型倍率丢失后：
  - `SelfUseModeEnabled=false`：请求被 fail-closed 拒绝（`relay/helper/price.go:95-104`）。
  - `SelfUseModeEnabled=true`：按 37.5 兜底计费（`model_ratio.go:1031`）。
- 修复方向：解析到临时 map 成功后再替换；`UpdateOption` 先在内存应用成功再落库，或对倍率类
  key 增加 `CheckGroupRatio` 式前置校验。

### P2 配置管理器读写数据竞争

- `model/option.go:764` `handleConfigUpdate` 调 `config.UpdateConfigFromMap` 改注册结构体
  字段，不持 `ConfigManager.mutex`；`ConfigManager.Read`（setting/config/config.go:64-76）
  在 RLock 下克隆同一结构体。
- 复现（独立模块 + `-race`）：`reflect.Value.SetString`（写）与 `cloneConfigValue`（读）
  并发，race detector 报 1 处竞争，退出码 66。
- 同类：`GetGeneralSetting()` 返回裸指针，`logger/logger.go:186-213` 无锁读取。

### P2 AAC 解析对 Frame_length=0 死循环

- `common/audio.go:312-347` 调 gomedia `SplitAACFrame`（util.go:74-82）：
  `start = FindSyncword(frames, start+0)` 原地踏步，goroutine 永久空转，不检查 ctx。
- 复现：构造 7 字节 `Frame_length=0` ADTS 样本调用 `common.GetAudioDuration`，3 秒不返回。
- 入口：登录用户经音频转录端点（service/token_counter.go:216）上传畸形 .aac，每请求泄漏
  一个 goroutine；panic 由 gin recovery 兜底，但死循环无兜底。

### P1 classic ESLint 升级后配置断裂（CI 必挂）

- `web/classic/package.json` devDeps 已升 `eslint ^10.13.0`（bun.lock 锁定 10.13.0），
  但仓库仍只有 `.eslintrc.cjs`（2026-10-17 修改）与 `.eslintignore`，无 `eslint.config.*`。
- 复现：`cd web/classic && bun x eslint "src/index.jsx"` 退出码 2，报
  "ESLint couldn't find an eslint.config.* file"。
- `repo-gates.yml:91-94` 在 push/PR 时运行 `bun run eslint`，本地 HEAD 推送后 CI 必挂。
  注意 origin/dev 仍为 eslint 8.57.0（本地领先 564 提交未推送），故障尚未在远端出现。
- 修复方向：迁移 flat config 或回退 eslint 8.x；二者选一并同步 .eslintignore 迁移。

### P1 桌面端就绪探测对真实 /api/status 恒为 false（静态强证据）

- `desktop/tauri-app/src-tauri/src/runtime_support.rs:108-123`：
  `is_ready_status_response` 要求顶层 `success == true && payload.get("version").is_some()`。
- 实测 `/api/status`：顶层键仅 `data/message/success`，`version` 在 `data.version`。
- `runtime.rs:250-275` 重试 `STARTUP_RETRIES` 次后报 "timed out waiting for ready local server"。
- 测试样例（tests/runtime_support.rs:218-236）用的是与真实响应不符的扁平结构，掩盖了问题。
- 旧实现 `body.contains("\"version\":")` 能匹配嵌套字段；f56ab6fc8 的"JSON 解析加固"反而
  只查顶层，属修复引入的回归。未运行桌面端实测（标注边界），但代码与实测响应均确凿。

## 二、已核实属实（代码级确认，未运行复现）

| 严重度 | 位置 | 现象 |
|---|---|---|
| P1 | relay/channel/aws/converse.go:812-845 | Converse 路径丢弃 Bedrock `CacheReadInputTokens`/`CacheWriteInputTokens`（SDK v1.63.7 types.go:7066-7069 存在），缓存命中按全价计费；流式/非流式、OpenAI/Claude 分支共 4 处 |
| P2 | service/convert.go:714 | OpenAI→Claude usage：`InputTokens = oaiUsage.PromptTokens`（OpenAI 语义含 cached 子集），叠加 `CacheReadInputTokens` 后 cached 部分翻倍；反向转换（relay-claude.go:1081-1098）证明仓库知晓该语义差 |
| P2 | service/convert.go:2138-2188 | OpenAI→Gemini 流式：无参工具（arguments 空串）final 帧必报错，非流式同输入返回 `{}`（convert.go:1330-1342），行为不一致 |
| P2 | relay/channel/aws/converse.go:1172-1183 | Converse 流式遇 reasoning signature delta 整条流 502；Claude 直连路径同信号静默跳过（relay-claude.go:910-913） |
| P2 | relay/channel/openai/responses_via_chat.go:774-778 | Chat→Responses 流式 n>1 静默取 choices[0]；非流式同场景报错（chat_to_responses_response.go:125-127） |
| P2 | service/convert.go:1344-1683 | Gemini→OpenAI 请求静默丢弃 `ThinkingConfig`/`Seed`/`CachedContent`（dto 有字段、转换无引用） |
| P2 | common/page_info.go:41-79 | `page_size=-1` 绕过上限：仅处理 `==0` 与 `>100`；gorm 对负 LIMIT 不生成 LIMIT 子句，23 个查询接口可全表拉取（管理端鉴权内） |
| P2 | model/log.go:184 + web/classic useUsageLogsData.jsx:1179 | 用户日志视图 ID 被重写为分页序号（`Id = startIdx + i + 1`，8580d76d5 由 `Id % 1024` 演化），classic 单条删除用该显示 ID 调 `DELETE /api/log/self/{id}`，按真实 ID+user_id 精确删——删除失败或误删同用户其他行 |
| P2 | middleware/distributor.go:990 -> service/channel_header_policy_runtime.go:376-408 | Tag Header Policy 每请求 `DB.Where("tag = ?").First`，无缓存 |
| P2 | middleware/auth.go:512、relay/helper/price.go:21 | `IsAdmin` 每请求直查 DB（`Select("role").Find`） |
| P2 | controller/video_proxy.go:668-672、relay/channel/minimax/tts.go:186-190 | 上游响应头全量透传客户端，未走 `ShouldCopyUpstreamHeader`/denylist 过滤 |
| P2 | controller/codex_usage.go:82-100 | `GET /api/channel/:id/codex/usage` 在 401/403 时自动刷新并持久化 OAuth token（GET 带写副作用，与只读代理策略冲突；路由有 AdminAuth） |
| P2 | controller/option.go 无 ModelRatio 负值守卫 | `UpdateModelRatioByJSONString('{"evil-model": -0.5}')` 返回 nil 且可查得；负倍率使 `text_quota` 算出负 quota，`PostConsumeQuota`（service/quota.go:443-447）对负 quota 走 `IncreaseUserQuota`，即向用户加钱。仅 Root 可写（ratio_sync 上游同步有 `isValidNonNegativeCost` 防护，唯手工路径漏防）；GroupRatio 有 `CheckGroupRatio` 负值校验，倍率类无对应 guard |
| P2 | web/classic DocumentRenderer/index.jsx:50-65 等多处 | classic `sanitizeHtml` 只删 `<style>`，不过滤事件处理器/脚本，配合 About/Home 等多处 `dangerouslySetInnerHTML`（内容仅 Root 可配，属管理员信任边界，非匿名存储型 XSS） |
| P3 | common/audio.go:157、191 | M4A Timescale=0 / OGG sampleRate=0 时时长为 +Inf，进入 token 计算（上游库不拒 0） |
| P3 | oauth/generic.go:158、244 | DEBUG=true 时 token/userinfo 响应体（含 access_token/refresh_token）原样进日志（仅 debug 门控） |
| P3 | setting/config/config.go:202-306 | 分层配置解析失败 `continue` 静默跳过，`handleConfigUpdate` 丢弃返回值；坏值落库进 OptionMap 但运行时不生效，无提示 |
| P3 | relay/channel/claude/relay-claude.go:437、461 | 显式 `max_tokens=0` 被渠道默认值覆盖（`GetMaxTokens()>0` 跳过 + `==0` 补默认），违反指针保留显式零值规则 |
| P3 | docs/reviews/CR-SCHEMA-ORDERED-PRESERVATION-2026-10-24.md | 含 5 处 emoji 违反 AGENTS.md；两处代码引用失实（`toolRequestFromOpenAIFunction` 在该提交时即不存在） |
| P3 | deploy/env/dev-isolated.env.example:11 | `DEV_REDIS_PORT=6380` 与实际 env 6381 不一致 |

## 三、被复核推翻的 agent 论断（均为高置信反证）

1. 「MySQL 从库连接池未配置」（数据层 agent P2）：不成立。主库（model/main.go:351-353）与
   日志库（:393-395）均设置池参数，仓库无从库概念。
2. 「MJ /mj/notify 回调无鉴权可伪造任务状态」（非聊天 agent P2）：不成立。POST /mj/notify
   路由未注册（relay-router.go:256 已注释），实测返回 SPA HTML；即便注册也在
   `registerMjRouterGroup` 的 `TokenAuth()` 之后。
3. 「realtime WS 双扣费风险」（非聊天 agent P1）：回归测试本轮实跑通过
   （`go test ./service -run TestRealtimeBilling` ok）。
4. 「JWT Direct 缺 nonce 校验」（认证 agent P1）：nonce 防授权码交互重放，直签模式无该流程。
5. 「About/Home 为匿名存储型 XSS」（安全深度 agent P1）：降级为管理员内容信任边界
   （仅 RootAuth 可改），classic 清洗不完整本身属实。
6. Docker 运行时 agent 的 P2「CONCURRENTLY 迁移错误残留」：引用的时间与语句均不匹配，
   实为 2026-10-09 反向验证测试的探针语句（`audit_does_not_exist_xyz` 等），dev PG 日志
   2026-10-24 零错误行。
7. 「Telegram widget 签名去重 Redis 不可用时多实例重放」（认证 agent P1）：成立性存疑，
   降级为 P3 待验证（多实例部署才有影响，Redis 降级路径代码属实）。

## 四、运行时结论（Docker 维度）

- dev 容器 build-info 与 HEAD 一致（仅 -dirty），PG 迁移成功、Redis/PG 连通、
  `/api/status` 200；配置隔离（密钥不共享、网络不同、卷独立）成立。
- P3：compose redis 无持久卷；`rebuild-dev-isolated.sh` 硬编码 3001（未读 DEV_NEW_API_PORT）。

## 五、待完成

- 渠道调度与重试、测试质量、计费与 relay（重试）3 个维度 agent 仍在运行。
- 上述 P1/P2 修复未开始（本轮为审查任务，未动代码）。
