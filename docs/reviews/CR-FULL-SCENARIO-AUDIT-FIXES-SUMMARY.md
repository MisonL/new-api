# CR-FULL-SCENARIO-AUDIT 修复执行总结

**审查对象**: CR-FULL-SCENARIO-AUDIT-2026-09-29.md 发现的 19 个问题  
**执行时间**: 2026-09-30 ~ 2026-10-01  
**实施状态**: 19 项中 **18 项已修复**、1 项明确不改（C3）；第二轮复核另发现 3 项已修复、1 项死代码记录不改

> 本表在 2026-10-01 复核时据实修正：此前版本把 B1/B2/B3/B7/B8 记为"明确不改"、
> 把 C1/C2 记为"DEFERRED"，与代码和主报告均不符。已逐条回到源码核实后改写。

---

## 已修复问题（19 项中的 18 项）

### A 组 — 本地逻辑与资源管理

| # | 问题 | 修复内容 | 验证状态 |
|---|------|---------|---------|
| A1 | Claude 转换器静默丢弃 `n>1` | `RequestOpenAI2ClaudeMessage` 对 `N>1` 返回 `n>1 is not supported by the Claude Messages API`；**未动原生 OpenAI 路径**（上游本身只回 1 个 choice） | 已验证 |
| A2 | `temperature` 无范围校验 | `validateTemperatureRange`（`[0,2]`，含 NaN），接在文本/Responses/Compaction 三处 | 已验证 |
| A3 | `ToClaudeError` 输出字面量 `<nil>` | `ErrorTypeOpenAIError` 分支 `Code` 为 nil 时回退到 `string(e.errorCode)` | 已验证 |
| A4 | `MaskSensitiveInfo` 掩码误伤 | `isAuthSchemeProseWord` 白名单扩 `quota`/`remain`/`balance`/`limit`（非正则改动） | 已验证 |
| A5 | CORS `Allow-Origin: *` 配 `Allow-Credentials: true` | 删除 `config.AllowCredentials = true`；`gin-contrib/cors` 在 `AllowAllOrigins` 下硬编码 `*` | 已验证 |
| A6 | 429 无 `Retry-After`、无响应体 | 新增 `abortRateLimited`，**6 处**调用（`rate-limit.go` 4 + `model-rate-limit.go` 2） | 已验证 |
| A7 | SIGTERM 前不 flush 配额 | 导出幂等 `model.FlushBatchUpdates()`，在 `srv.Shutdown(ctx)` 之后调用 | 已验证 |
| A8 | 双层 `"Invalid request: Invalid request:"` | 去掉 `getModelFromRequest` 内部两层包装，保留 `Distribute` 那一层 | 已验证 |

### B 组 — 协议与错误体

| # | 问题 | 修复内容 | 验证状态 |
|---|------|---------|---------|
| B1 | `GET /v1/models/{model}` 对可路由模型返回 200 + 假消息 | 判定改为"可路由集合"，未命中返回 404 `invalid_request_error` / `model_not_found`（`controller/model.go:420`） | 已验证 |
| B2 | 未知模型 503 而非 404 | `abortNoRoutableModel` 对"分组下无该模型"返回 404 `model_not_found`；**渠道能力拒绝仍保留 503** | 已验证 |
| B3 | SSE 头配裸 JSON 正文 | 新增 `helper.ResetEventStreamHeaders`，`!c.Writer.Written()` 时显式 `Del` SSE 头（gin `v1.9.1` 只在 header 为空时才写 Content-Type） | 已验证 |
| B4 | `/v1/completions` 404 配内部哨兵 | 纯文本上游 body 的兜底 message 改用已算好的 `statusMessage` | 已验证 |
| B5 | `/v1/embeddings` 透传上游 SDK 异常串 | 透传前对 `oaiError.Type` 做词表归一；**未改 `message` 文本** | 已验证 |
| B6 | 错误 `type` 词表不统一 | 新增 `standardErrorType` / `StandardizeErrorType`，覆盖 5 个生产者（见主报告 §B6 与 §9.3） | 已验证 |
| B7 | failover 日志渠道归因错位 | `responsesRoutingAttributedChannelID` 在无 trace 时优先读尝试级 context key（`relayAttemptChannelIdKey`） | 已验证 |
| B8 | 确定性上游 404 被改写成 500 | `shouldRetry` 对 `isDeterministicModelNotFoundError` 直接 `return false`，从源头消除 | 已验证 |
| B9 | 错误体泄漏 Go 结构体名 | 新增 `common.SanitizeJSONDecodeError`，用 `errors.As` 抓 `*json.UnmarshalTypeError` | 已验证 |

### C 组 — 产品决策项

| # | 问题 | 修复内容 | 验证状态 |
|---|------|---------|---------|
| C1 | codex 动态头守卫让渠道对非 Codex 客户端整条判死 | 渠道级 opt-out：`dto.ChannelOtherSettings.AllowMissingCodexDynamicRequestHeaders *bool`（`omitempty`）+ `controller/relay.go:1654` 路由预检 + 前端开关。**默认 `nil`/`false` 保持严格现状** | 已验证 |
| C2 | 同一模型跨渠道时 Responses 面 `usage` 结构漂移 | 新增 `relay/channel/openai/responses_usage_normalize.go`，在写客户端前补齐缺失的 details 对象（非流式与 compact 两条路径） | 已验证 |

---

## 明确不改（1 项）

| # | 问题 | 不改原因 |
|---|------|---------|
| C3 | `/api/channel/` 向任意 admin 明文返回 `settings` | **BY-DESIGN**: `model.Channel.OtherSettings` 的 tag 是 `json:"settings"`，前端渠道表单依赖该字段；改动需前后端联动，当前快照里的 header 值都是客户端身份伪装头、不是密钥 |

---

## 新发现问题（4 项）

### 代码审查轮（Worker 3）

**问题**: `controller/model_list_test.go` 的测试 fixture cleanup 缺失 `InvalidatePricingCache()`  
**修复**: 第 71 行新增调用  
**验证**: `go test -shuffle=on -count=3 ./controller` 通过

### 第二轮复核（独立复现后修正）

| # | 问题 | 修复 | 验证 |
| --- | --- | --- | --- |
| A4 | `token model access configuration is invalid`、`token id is required` 两条**真实文案**在掩码路径上被改写（另两条 `token endpoint is required`、`token authentication` 当前不经掩码器） | `common/str.go` 白名单补 `model`/`id`/`endpoint`/`authentication` | `TestMaskSensitiveInfoKeepsTokenConceptProse`，反向验证 FAIL |
| A6 | email-verification 的 **Redis 路径**（生产实际路径）无 `Retry-After` | 补 `Retry-After`（取 TTL 剩余秒数） | miniredis 用例，反向验证 FAIL |
| P2 | `controller/billing.go` 的 `GetUsage` 仍输出 `new_api_error`（B6 覆盖遗漏） | 改用 `types.StandardizeErrorType`，输出 `server_error` | `TestGetUsageUsesStandardErrorTypeOnFailure`，反向验证 FAIL |

**记录不改**: P1 `middleware/utils.go` 的 `abortWithMidjourneyMessage` 全仓 0 调用点，
但 `git show origin/main` 确认其在主线已存在，非本会话引入，按原子任务规则只记录不删除。

### 复查自身测试健壮性（2026-10-02）

前几轮验证只跑 `-count=1`，补做 `-shuffle` / 重复运行后**发现并修复了自己用例的一个缺陷**：

| 问题 | 修复 | 验证 |
| --- | --- | --- |
| `TestEmailVerificationMemoryRateLimitSetsRetryAfter` 在 `-count>1` 下必失败（共享 `inMemoryRateLimiter` 单例未重置，跨轮次累积计数） | 按仓库既有模式换入新 limiter 实例并在 cleanup 换回 | `-shuffle=on -count=5` 由 FAIL 转 ok |

**当时另记录了一个与本轮改动无关的既有缺陷**：`middleware/rate-limit_test.go` 的
`TestDesktopOAuthPollRateLimitUsesHandoffTokenKey` / `...RejectsExcessRequests` 有同类问题
（`-count=3` 必失败）。已双向确认非本次引入：还原 `middleware/rate-limit.go` 到 `HEAD` 后照旧复现，
移出本轮新增的 7 个测试文件后也照旧复现。**该缺陷已在下一节一并修复。**

### 扩大修复范围（2026-10-02，"能优化的都优化好"）

用户要求把能优化的都修掉，故上一节"记录不改"的既有缺陷一并修复，并沿"跨轮次状态泄漏"
线索全仓扫描。共修 3 处，各有反向验证：

| # | 问题 | 影响面 | 修复 | 反向验证 |
| --- | --- | --- | --- | --- |
| 1 | `model/openTestDB` 用按测试名固定的 `cache=shared` 内存 DSN 且从不关闭连接 | **55 个 `model` 用例在 `-count>=2` 下失败**（插入撞 UNIQUE、断言读到上一轮数据） | 加 `t.Cleanup` 关闭底层 `sql.DB` | 修复后 `-count=3` 全绿；缺陷出自 `a5e224627`，非本会话引入 |
| 2 | `pkg/cachex.HybridCache` 构造时捕获 Redis 客户端，而 `RedisEnabled` 每次求值，客户端被替换后闸门与操作不一致 | affinity 缓存在 `sync.Once` 单例里持续写已关闭连接（探针实证 `redis: client is closed`），`RecordChannelAffinity` 静默失败 | 新增 `RedisFunc`（每次调用求值）+ 统一 `client()` 解析器，4 个调用点改用它 | 还原调用点后 `-count=2` 立刻复现原 `Should be true` 失败；另加 `pkg/cachex` 固化用例 |
| 3 | `middleware/rate-limit_test.go` 两个 desktop OAuth 用例不重置共享 limiter | `-count>=2` 必失败 | 新增 `useFreshInMemoryRateLimiter(t)` 助手 | `-count=3` 通过 |

**生产可达性说明**：第 2 项的 `common.RDB` 只在 `main.go:373` 赋值一次，线上不会中途替换，
故实际只在测试中触发；仍按源码修，因为"闸门动态、操作静态"是 `HybridCache` 自身的内部不一致。

**验证**：`go build ./...` EXIT=0；9 个包 `-shuffle=on -count=2` 全 ok；`-race` 通过。

### 推到全仓 `-count=2` 后又查出 1 处（2026-10-02）

把 `-count=2` 推到全仓（`go test ./...`）后，在之前完全没扫过的 `router` 包又查出一个同类泄漏：

| # | 问题 | 影响 | 修复 | 反向验证 |
| --- | --- | --- | --- | --- |
| 4 | `router/api_router_test.go` 把 `CriticalRateLimitNum` 设为 1 并挂真实路由，用完的限额记录留在包级单例里 | `-count>=2` 时第二轮首次 GET 返回 429（期望 200） | 在 `middleware/rate-limit.go` 新增导出钩子 `ResetInMemoryRateLimiterForTest()`（`router` 包够不到未导出变量），在测试 setup 调用 | 移除调用后立刻复现 `expected 200, actual 429` |

**全仓验证**：`go test ./... -shuffle=on -count=2` → **0 个 FAIL**；`-race ./router ./middleware` ok。

**另修一处自造问题**：上节给 `HybridCache.scanKeys` 加的 `client == nil` 守卫是不可达死代码
（三个调用点都已过 `redisOn()`），且 `return nil, nil` 是静默成功路径，违反 AGENTS.md §0，已删除。

### 推到全仓各维度 + 修掉自造缺陷（2026-10-02）

**全仓维度验证**（此前只覆盖部分包）：

```bash
go vet ./...                          # 无输出
go test ./... -shuffle=on -count=3    # 0 个 FAIL
go test -race ./... -count=1          # 0 个 DATA RACE，0 个 FAIL
```

**又修一处自造缺陷**：上节把操作点写成 `if client := c.client(); c.redisOn() {`，**一行里解析了两次
客户端**（`redisOn()` 内部再解析一次）。`RedisFunc` 每次返回不同客户端时，闸门批准 A 而操作用 B——
正是上节要修的那类不一致。上节的回归用例用稳定闭包，没暴露它。

修法：新增 `redisClient() (*redis.Client, bool)` 一次解析，四个操作点统一改用它，`scanKeys`
改为接收已解析的客户端。新增 `TestHybridCacheResolvesTheClientOncePerOperation`（计数闭包断言只解析一次），
**反向验证**：改回分开调用后立即 FAIL，恢复即通过。

**结论**：新写的代码与既有代码一样会引入缺陷，且更容易因"刚写完、看着对"而漏检。

### 补齐订阅缓存的针对性覆盖（2026-10-02）

§"扩大修复范围"的第 2 项把 `RedisFunc` 落到 4 个调用点，其中 `model/subscription.go` 的
两处订阅缓存**当时没有任何测试覆盖**（`go test ./model -run 'TestSubscription'` 无匹配用例），
只有"全包 `-count=3` 通过"这一间接证据。已补 `model/subscription_plan_cache_redis_test.go`，
断言缓存写进了被替换后的当前客户端。

| 要点 | 说明 |
| --- | --- |
| 该用例的第一版**无效** | 只断言 `getSubscriptionPlanByIdTx` 的返回值；该函数在缓存未命中时回落查库，还原缺陷后**仍然通过** |
| 改为直接断言缓存内容 | `current.Get(ctx, fullKey)` 命中当前客户端才通过 |
| 反向验证 | 还原 `Redis: common.RDB` 时**立即 FAIL**；恢复 `RedisFunc` 后通过 |
| 教训 | "反向验证通过"也要看失败原因——断言点必须落在修复所改变的那一层状态上，不能落在有回落路径的返回值上 |

至此 4 个 `RedisFunc` 调用点全部有针对性反向验证。

**第二轮审查结论的修正**（详见主报告第九节）：
- A4 的 `"token balance: 50"` 例子**全仓无来源**，是构造的；真实被误伤的是另外 4 条文案。
- A6 的 `abortRateLimited` 调用点是 **6 处**，不是报告写的 7 处；真实缺口在 email-verification 的 Redis 路径。

---

## 验证结果

### 后端测试

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service ./middleware ./common ./types
```

**结果**: 8 个包全部通过，0 失败（`go build ./...` EXIT=0）

### 前端构建与测试

```bash
cd web/default && bun run lint && bun run build && bun test
```

**结果**: lint 无错误；rsbuild 构建成功（Safari 兼容改写 4 个文件，产物 `dist/` 不入库）；
`bun test` **251 pass / 0 fail**（34 个文件，951 次断言）。`web/default` 无 vitest/jest 依赖，
测试走 bun 内置 runner。

### MySQL 兼容性

已修复 `key` 列方言问题（`model.KeyColumn()` + dialector 包装），MySQL 8.4 启动测试通过。

---

## 已知边界

1. **SIGKILL 无法 flush**: A7 只覆盖 SIGTERM/SIGINT 优雅退出，compose stop 默认 10s 宽限可能不足
2. **A1 不动 OpenAI 路径**: 上游对 `n>1` 也只返回 1 个 choice，改 400 会破坏现有行为
3. **B5 的 `***` 前缀**: `***.ServiceUnavailableError` 来自域掩码正则，本轮只统一 `type`/`code` 词表，不动掩码正则
4. **C3 不改**: 需前后端联动，见上表
5. **A6 的 Redis 用例用 miniredis**: 覆盖语义，未覆盖真实 Redis 版本差异

---

## 文件清单

- 修复实施报告: `CR-FULL-SCENARIO-AUDIT-FIXES-2026-09-30.md`
- 原始审查报告: `CR-FULL-SCENARIO-AUDIT-2026-09-29.md`
- 本总结: `CR-FULL-SCENARIO-AUDIT-FIXES-SUMMARY.md`

---

**执行者**: Claude (Opus 5.5)  
**最终验证**: 2026-10-01 02:23 UTC
