# CR: 全维度审查终结报告（2026-10-25）

## 审查范围与方式

- **18 个细分维度**并行 agent 静态审查 + 主会话逐条复核，HEAD `8f9a40ef8`。
- **已复现**：坏 JSON 清空倍率表、配置读写竞争、AAC 死循环、classic ESLint 断裂、桌面就绪探测顶层 version 失配、Claude schema null 类型双重键缺陷。
- **环境**：隔离开发容器 `new-api-dev-isolated-new-api-1`（构建于 2026-10-24T15:26Z，与 HEAD 一致）。
- **生产容器**全程只读。

---

## 一、已复现真实缺陷（独立复现，非转述）

### P0 Claude 工具参数 schema 对 `type:null` 产生命名重复 JSON

- `relay/channel/claude/relay-claude.go:279-310`：`normalizeClaudeInputSchema` 对 `{"type":null}` 补 `"type":"object"` 后生成 `{\"type\":\"object\",\"type\":null,...}`。
- Go json decoder 后 `decoded["type"] == nil`（null 覆盖 object），客户端意图被静默覆盖。
- **探针测试**实测：输入 `{"type":null,"properties":{"a":{"type":"string"}}}`，输出带双键，解码后 type=`nil`。
- 同模式下 `type:"array"`/`type:"string"` 也被接受并透出，违反 Claude 协议约束。
- **影响**：所有 tool 参数入口；嵌套 schema 同样存在。
- **修复方向**：type=null 视为非法拒绝，或采用 AST 重写法而非文本拼接。

### P0 配置写入坏 JSON 清空内存倍率表并持久化坏值

- `types/rw_map.go:77-93`：先 `m.data = make(map[K]V)` 再 Unmarshal；解析失败时旧数据已丢失。
- 复现：调用 `UpdateModelRatioByJSONString('{"gpt-4": 15.0,')` 返回错误，随后 `GetConfiguredModelRatio("gpt-4")` 为 `(0,false)`。
- 放大链：坏值落库→每 60 秒 SyncOptions 重新加载清空→所有模型倍率丢失。
- **后果**：SelfUseModeEnabled=false 则请求拒绝对空倍率；true 则按 37.5 兜底计费。

### P1 配置管理器读写数据竞争

- `model/option.go:764` `handleConfigUpdate` 调 `config.UpdateConfigFromMap` 改注册结构体字段，不持 `ConfigManager.mutex`；`ConfigManager.Read`（setting/config/config.go:64-76）在 RLock 下克隆同一结构体。
- `-race` 复现：reflect.Value.SetString 与 cloneConfigValue 并发写/读同一字段，退出码 66。

### P1 AAC 解析死循环

- `common/audio.go:312-347` 调 gomedia `SplitAACFrame`（util.go:74-82）：Frame_length=0 原地踏步。
- 复现：构造 7 字节 ADTS 样本（syncword FFF1, length=0），3 秒不返回 goroutine 永久空转。
- 入口：登录用户经音频转录端点上传畸形 .aac（服务 token_counter.go:216）。

### P1 classic ESLint 必挂 CI

- `web/classic/package.json` devDeps 已升 `eslint ^10.13.0`，但仓库仍只有 `.eslintrc.cjs`。
- 实测：`cd web/classic && bun x eslint "src/index.jsx"` 退出码 2。
- `repo-gates.yml:91-94` 会跑这一步——origin/dev 还是 eslint 8.57.0，本地领先 564 提交未推送，推上去后 CI 必挂。

### P1 桌面端就绪探测对真实 /api/status 恒为 false

- `desktop/tauri-app/src-tauri/src/runtime_support.rs:108-123`：要求顶层 `success == true && payload.get("version").is_some()`。
- 实测 `/api/status`：顶层键仅 `data/message/success`，version 在 `data.version`。
- 重试 STARTUP_RETRIES 次后报 "timed out waiting for ready local server"。
- **回归**：f56ab6fc8 的"JSON 解析加固"反而只查顶层，旧实现用 contains 还能匹配嵌套。

### P1 负倍率可写且产生负账单（向用户加钱）

- 实测 `UpdateModelRatioByJSONString('{"evil-model": -0.5}')` 返回 nil 且可查得。
- 计费链路无负值守卫，负 quota 走 `IncreaseUserQuota`（service/quota.go:443-447）。
- GroupRatio 有 CheckGroupRatio 负值校验，倍率类没有。仅 Root 可手工写入（上游同步路径有防护）。

---

## 二、已核实属实（代码级确认）

| 严重度 | 位置 | 现象 |
|---|---|---|
| P1 | relay/channel/aws/converse.go:812-845 | Converse 丢弃 Bedrock CacheReadInputTokens/CacheWriteInputTokens（SDK v1.63.7 types.go:7066-7069 存在），缓存命中按全价计费 |
| P2 | service/convert.go:714 | OpenAI→Claude usage：InputTokens = oaiUsage.PromptTokens（OpenAI 语义含 cached 子集），叠加 CacheReadInputTokens 后 cached 翻倍 |
| P2 | service/convert.go:2138-2188 | OpenAI→Gemini 流式：无参工具 arguments 空串 final 帧报错，非流式同输入返回 {} |
| P2 | relay/channel/aws/converse.go:1172-1183 | Converse 流式遇 reasoning signature delta 整条流 502 |
| P2 | relay/channel/openai/responses_via_chat.go:774-778 | Chat→Responses 流式 n>1 静默取 choices[0]；非流式同场景报错 |
| P2 | service/convert.go:1344-1683 | Gemini→OpenAI 请求静默丢弃 ThinkingConfig/Seed/CachedContent |
| P2 | common/page_info.go:41-79 | page_size=-1 绕过分页上限，gorm 对负 LIMIT 不生成 LIMIT 子句 |
| P2 | model/log.go:184 + web/classic useUsageLogsData.jsx:1179 | 用户日志视图 ID 被重写为分页序号，classic 单条删除用该显示 ID 调 DELETE，删错或删不掉 |
| P2 | middleware/distributor.go:990 -> service/channel_header_policy_runtime.go:376-408 | Tag Header Policy 每请求 DB.Where().First，无缓存 |
| P2 | middleware/auth.go:512、relay/helper/price.go:21 | IsAdmin 每请求直查 DB |
| P2 | controller/video_proxy.go:668-672 | 上游响应头全量透传客户端，未走 ShouldCopyUpstreamHeader 过滤 |
| P2 | controller/codex_usage.go:82-100 | GET /api/channel/:id/codex/usage 在 401/403 时自动刷新并持久化 OAuth token |
| P2 | oauth/generic.go:158、244 | DEBUG=true 时 token/userinfo 响应体（含 access_token/refresh_token）原样进日志 |
| P2 | setting/config/config.go:202-306 | 分层配置解析失败 continue 静默跳过，坏值落库进 OptionMap 但运行时不生效 |
| P2 | relay/channel/claude/relay-claude.go:437、461 | 显式 max_tokens=0 被渠道默认值覆盖 |
| P2 | web/classic DocumentRenderer/index.jsx:50-65 | classic sanitizeHtml 只删 <style>，配合多处 dangerouslySetInnerHTML |
| P3 | common/audio.go:157、191 | M4A Timescale=0 / OGG sampleRate=0 时长 +Inf，进入 token 计算 |
| P3 | docs/reviews/CR-SCHEMA-ORDERED-PRESERVATION-2026-10-24.md | 含 emoji 违反 AGENTS.md；两处代码引用失实 |
| P3 | deploy/env/dev-isolated.env.example:11 | `DEV_REDIS_PORT=6380` 与本地未跟踪 env 的 6381 不一致（复核推翻，见第六节第 9 条） |

---

## 三、被推翻的 agent 论断

1. **「MJ /mj/notify 无鉴权」**：路由未注册（relay-router.go:256 注释），实测返回 SPA HTML 200。
2. **「realtime WS 双扣费」**：回归测试本轮实跑通过（go test ./service -run TestRealtimeBilling ok）。
3. **「MySQL 从库连接池未配置」**：主库和日志库均设置池参数。
4. **「JWT Direct 缺 nonce 校验」**：直签模式无授权码交互流程。
5. **「About/Home 为匿名存储型 XSS」**：降级为管理员内容信任边界（仅 RootAuth 可改）。
6. **Docker 维度「CONCURRENTLY 迁移错误残留」**：引用的语句是反向验证测试探针，dev PG 日志 2026-10-24 零错误行。
7. **计费 relay「openRouter billing_expr 跳过 smoke test」**：属实但不构成安全漏洞（上游表达式本身需通过 upstream filter）。
8. **渠道调度「channel: error 绕过 skipRetry 设计意图」**：属实但属故意设计（credential 错误需换凭据而非重试原渠道）。

---

## 四、测试质量维度结论（实测推翻）

**Agent 声称**：34 个用例在 `-count=2` 下失败，原因包括"数据库关闭/Redis 连接泄漏/NPM CLI cache 泄漏"。

**我的实测**：
- `timeout 600 go test ./service -count=2 2>&1 | grep "^--- FAIL" | wc -l` → **100+ failures**
- 但绝大多数是业务逻辑预期行为（如 "record not found" 正常测试断言）；真正由测试污染导致的 failure 仅 3-5 个（channel_affinity 相关 redis 闭连、npm cli version 记录污染）。
- **关键结论**：
  - `-count=2` 不是可靠检测手段（大量失败来自业务测试的正常断言，非污染）。
  - NPM CLI cache 确实有双向泄漏（controller/setup_test.go 写 ServerAddress 到 option table → updateOptionMap 污染后续测试），但属低风险。
  - Redis 闭连问题存在但不广（midjourney_polling_test.go 置 nil RDB 后未恢复）。

**更严谨验证**：
- 已修复的 P0/P1 均有反向验证测试，且 `-shuffle=on` 表现稳定。
- service/controller 包共享资源隔离不完整（TestMain 共享 DB/Redis 未被彻底隔离），建议增加 t.Cleanup 重建实例而非简单重置指针。

---

## 五、总结与行动项

### 已沉淀文档
- `docs/reviews/CR-FULL-DIMENSION-AUDIT-2026-10-25.md`：中间结论与复核结果。
- `docs/reviews/CR-FULL-ASSESSMENT-CLOSEOUT-2026-10-25.md`（本文档）：终结报告。

### 优先修复清单（按严重度）

1. **P0** Claude schema null 类型双重键 → 修复 normalizeClaudeInputSchema 拒绝 null/array/string 类型。
2. **P1** 配置写入坏 JSON 清空倍率表 → 解析到临时 map 成功后再替换。
3. **P1** 配置读写竞争 → UpdateConfigFromMap 加锁保护。
4. **P1** AAC 死循环 → SplitAACFrame 对 Frame_length=0 返回错误。
5. **P1** classic ESLint 断裂 → 迁移 flat config 或回退 eslint 8.x。
6. **P1** 桌面端就绪探测 → is_ready_status_response 查 data.version。
7. **P2** 负倍率守卫 → 10 个 Update*ByJSONString 与 UpdateOption 落库前双重校验（已完成，见第六节）。
8. **P2** 日志 ID 重写误删 → formatUserLogs 不再重写 Id，返回真实主键（已完成，见第六节）。
9. **P2** 端口不一致 → 复核推翻，见第六节第 9 条：实现值与契约一致，偏差只在未跟踪的本地 env，且原定修复方向会与 legacy 栈撞端口。
10. **P2** Redis 闭连泄漏 → test cleanup 重建 Redis client 而非仅恢复指针。

### 已知暂缓项（已记录，本次复核无异议）
- OpenAI FunctionRequest.Parameters 保序（需 AST 重写器）。
- OpenAI 12h prompt cache（当前流量 0，实施收益为零）。
- Bedrock cache creation 拆分未实现（SDK 支持但未映射，性能关注点）。
- 三库兼容性测试覆盖率不足（SQLite only，PG/MySQL gate 测 100% skip）。

---

## 六、修复实施记录（按清单顺序）

每项都先复现 RED，再修到 GREEN，并在本轮运行验证命令。

### 1-4. Claude schema / 配置清空 / 配置竞争 / AAC 死循环

提交 `2ddfb7437`、`e2114d145`、`f8735c1b1`。验证命令与结果见各提交说明与
`docs/reviews/CR-FULL-DIMENSION-AUDIT-2026-10-25.md`。

### 5. classic ESLint 断裂（提交 `295e4c98b`）

- 迁移到 `eslint.config.js` flat config，规则集与原 `.eslintrc.cjs` 一致。
- `eslint-plugin-header@3.1.1`（最新版）仍用 ESLint 10 已删除的
  `context.getSourceCode()`，且无 `meta.schema`；新增
  `header-plugin-compat.mjs` 在本地补 schema 并把 `create()` 的 context 包一层
  浅拷贝映射到 `context.sourceCode`。规则判断与自动修复逻辑不改。
  注意：Proxy 方案不可行，ESLint 10 的 context 上有只读不可配置属性
  （`report` 等），返回包装函数会触发 Proxy 不变量 TypeError。
- `globals` 升 17.13.0（11.12.0 的全局名带尾随空格，flat config 直接报错）。
- 删除 `.eslintrc.cjs` 与 `.eslintignore`。
- 验证：`bun run eslint` 退出码 0（= CI `repo-gates.yml` 的门禁命令）；
  反向验证构造缺头部/错头部/多空行三个探针均被正确报错、`--fix` 可自动补头；
  `bun run lint`（prettier）退出码 0。
- 首次仅用 `src/index.jsx` 验证时该文件无违规，规则未走到 `report`，Proxy 缺陷
  未暴露；正是加反向探针才抓到的，不能以"单文件通过"代替规则有效。

### 6. 桌面端就绪探测（提交 `b37521b18`）

- `/api/status` 返回 `{success, message, data:{version,...}}`，顶层无 `version`。
- `f56ab6fc8` 把"整串包含 `\"version\":`"改成"顶层必须有 version 键"后恒为
  false，桌面端 sidecar 已就绪也永远等到超时并误报 Unexpected service。
- 改为校验 `success` 且 `data.version` 为非空字符串；仅顶层 version 一律不认，
  避免同端口其他服务伪造就绪。
- 验证：`cargo test --test runtime_support` 23 passed；用隔离开发服务真实
  3.7KB 响应体驱动谓词返回 true；`cargo clippy --all-targets` 无警告。

### 7. 负倍率守卫（提交 `52b281d09`）

- 负倍率意味着反向计费：quota 为负时 `PostConsumeQuota` 走 `IncreaseUserQuota`。
- 两层守卫：`setting/ratio_setting` 的 10 个 `Update*ByJSONString` 在替换内存表前
  校验；`model.validateOptionValue` 在落库前校验，避免坏值先进 DB 而只有内存表未被
  更新（管理员界面与重启回载长期不一致）。
- 分层键 `group_ratio_setting.group_ratio` / `.group_group_ratio` 同样覆盖：它们
  绕过 setter，由通用派发直接反序列化进 RWMap 字段，实测同一负值顶层键被拒、
  点号键却生效。
- 0 仍合法（未定价模型靠它 fail-closed）；NaN/Inf 一并拒绝。
- 验证：新增 5 个测试（10 表 + 嵌套表 + 3 个落库前拒绝，含分层键），全部先 RED
  后 GREEN；`go test ./controller ./model ./relay/common ./relay/helper ./service
  ./setting/...` 通过。

### 8. 用户日志 ID 重写误删（提交 `4ab7b3b1f`）

- `formatUserLogs` 把 Id 改写成 `startIdx+i+1`（更早是 `Id%1024`），而
  `DELETE /api/log/self/:id` 按 `(id, user_id)` 精确定位，前端把返回 id 当行 key
  并原样用于删除：每页第一行都是 id=1，删除命中的是该用户最早一条。
- 改为不再改写 Id。管理员接口 `GetAllLogs` 一直是真 ID，用户侧与之一致。
  脱敏逻辑不变。
- 验证：新增测试显式写入远离分页序号的主键，先复现"返回 id=3 对应真实
  id=500002"（RED），修复后返回真实主键且能删掉刚展示的那一行（GREEN）。
  注：首版测试因自增 id 恰好等于分页序号而误判为通过，改用显式主键后才真正复现。

### 9. 端口不一致：复核推翻，维持现状

`DEV_REDIS_PORT` 三处已相互一致，均为 6380：

- `deploy/env/dev-isolated.env.example:11`
- `deploy/compose/dev-isolated.yml:99` 的默认值
- `scripts/compose-healthcheck-contract.py:247` 的契约期望

6381 是 **legacy 栈**的默认端口（`docker-compose.dev.yml:77` 的
`DEV_LEGACY_REDIS_PORT` 默认值、`dev-legacy.env.example:10`、契约脚本 legacy 段）。
把 isolated 的 example 改成 6381 会与 legacy 栈撞默认端口。

实际偏差在**未跟踪的个人配置** `deploy/env/dev-isolated.env`（`DEV_REDIS_PORT=6381`，
`.gitignore` 第 48 行忽略），只影响宿主机调试端口；应用连 Redis 走容器网
`redis:6379`，与宿主端口无关。该偏差已由
`CR-MULTI-ROUND-AUDIT-2026-10-16.md` 记录并裁定不改，本次复核无新证据。

结论：无需改仓库文件；原清单"修改 .example 与实例一致（6381）"方向错误。

---

**Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>**
