# CR: 开发分支全维度审计与修复

- 日期：2026-09-23
- 分支：`dev`，基线 HEAD `be1979283`
- 范围：`/Volumes/Work/code/new-api` 工作区（含未提交改动）
- 状态：已完成（代码改动未提交，按用户指令保留在工作区）

## 授权与边界

用户指令："用agents进行全维度全面检查"、"那就全部优化好"、"暂不提交"。

正式服务 `new-api`（端口 13000）全程**未重建、未重启**。本轮只做代码读写、
本地构建与测试，以及隔离开发库的只读查询。

## 审计方式

按维度拆分为 8 个只读子代理并行执行：lint 五类（errcheck / staticcheck /
unused / ineffassign+govet）、安全、并发与资源、前端、三库兼容与计费。
子代理只输出结论，**不写文件**；所有修复由主流程逐条复核后统一落地。

**重要方法论教训**：`/tmp/lintsplit/*.txt` 作为子代理输入时已被 golangci-lint
默认上限截断（`--max-issues-per-linter=50`、`--max-same-issues=3`）。子代理
报告的多处"清单只有 N 条"实为截断假象，其中**隐藏了真缺陷**（见下文
`controller/billing.go` 与 `setting/ratio_setting/model_ratio.go`）。后续对比
一律使用 `--max-issues-per-linter=0 --max-same-issues=0`。

## 真缺陷与修复

每条都经反向验证：临时还原修复，确认对应测试或现象复现。

### 计费

- `setting/ratio_setting/model_ratio.go:459,460` | `1 / 1000 * USD` 是**无类型
  整数除法**，编译期即为 0，两个 Sonar large 模型倍率归零，结算按免费处理。
  实测 `defaultModelRatio["llama-3-sonar-large-32k-chat"]` 修复前为 `0`，修复后
  `0.5`（相邻 `0.2 / 1000 * USD` 一直是 0.1，写法不一致是诱因）。
  修复：改为 `1.0 / 1000 * USD`。回归测试
  `setting/ratio_setting/model_ratio_sonar_test.go`。
- `pkg/billingexpr/run.go:112` | `expr-lang` 对除零返回 `+Inf`、对 `0/0` 返回
  `NaN` 而非报错，`int64(+Inf)` 在 amd64 上是 `-9223372036854775808`。管理员配置
  含除法的阶梯表达式且分母为 0 时，结算 delta 变成极大负数并给用户巨额退款。
  实测确认 `p * 100 / c`（c=0）→ `+Inf` → `int64` = MinInt64。
  修复：结果非有限值时返回错误。回归测试
  `pkg/billingexpr/run_nonfinite_test.go`（3 例 + 1 例有限值不受影响）。
  反向验证：移除修复后 3 例全部失败。

### 三库兼容

- `model/log.go:1166` | `Limit(100).Delete(...)` 的 LIMIT 子句只有 MySQL 方言会
  渲染；SQLite 与 PostgreSQL 的 `DeleteClauses` 不含 LIMIT，GORM 静默丢弃，
  `DeleteOldLog` 的"分批删除"退化为一次性全量删除。已核对三个驱动的
  `DeleteClauses` 并**独立实测**（SQLite：250 行中一次删掉 199 行，`Limit(100)`
  完全无效）。
  修复：先 `Pluck("id")` 取主键再按 `id IN ?` 删除，使每条 DELETE 都有界。
  回归测试 `model/log_delete_batch_test.go` 注册 GORM 删除回调逐条记录
  `RowsAffected`，断言**每条语句**不超过 limit。反向验证：还原后报
  `DELETE statement 0 removed 199 rows, want at most 100`。

### 并发

- `model/pricing.go` | `pricingMap` / `vendorsList` / `supportedEndpointMap` 在
  `updatePricing` 中被整体重建，而 `GetPricing` 在锁外返回切片、`GetVendors` 与
  `GetSupportedEndpointMap` 完全无锁读取。并发读写 map 是 Go 的**致命错误**
  （不可 recover），不是普通 panic。
  修复：`updatePricingLock` 改 `RWMutex`，三个读取口加读锁；`PricingVersion`
  改在构建循环内赋值，避免发布后再改已发布的切片。
  回归测试 `model/pricing_race_test.go`（8 读 + 1 失效，`-race`）。反向验证：
  还原读锁后 `-race` 报 `WARNING: DATA RACE`。
- `model/channel_cache.go:1075`、`model/channel_satisfy.go:28`、
  `model/channel_route_selector.go:222` | 三处 `RUnlock` 是顺序语句而非 `defer`，
  而临界区内执行调用方传入的请求级回调（candidate filter / route resolver）。
  回调 panic 被 `gin.CustomRecovery` 兜住后进程存活，但**读锁永久泄漏**，此后
  所有 `Lock()` 阻塞，整个网关渠道选择死锁。
  修复：三处改为闭包 + `defer RUnlock`。
- `service/channel_concurrency.go:339` | `parentContext.Err()` 短路了其后的
  `errChannelConcurrencyStateUncertain` fail-closed 分支。请求取消（客户端断开
  或 250ms Redis 超时）导致 Eval 报错且补偿 ZREM 也失败时，本应 fail-closed，
  实际返回取消错误并静默降级为本地并发计数。
  修复：先判 state-uncertain，再判父上下文取消。
- `relay/channel/cohere/relay-cohere.go`、`relay/channel/zhipu/relay-zhipu.go`、
  `relay/channel/palm/relay-palm.go` | 三个 `c.Stream` 处理器用**无缓冲**
  producer channel，且无 `c.Request.Context().Done()` 分支。gin 的 `c.Stream`
  在客户端断开时直接 return 不再消费 channel，producer goroutine 永久阻塞在
  `dataChan <- data`，同时 `resp.Body` 不被关闭。palm 还有一处
  `responseText` 跨 goroutine 无同步写。
  修复：三处 producer 增加取消分支；palm 的文本改经带缓冲 channel 交回主流程。

### 崩溃与日志

- `dto/gemini.go:132,139,156` | `logger.LogError(nil, ...)` 传入 nil context，
  而 `logHelper` 直接调用 `ctx.Value(...)`。**畸形 tools JSON 可让网关 panic**：
  `GetTools()` 解析失败即走到这里，`relay-gemini.go:454` 会调用它。
  已实测 nil ctx 必然 panic（`invalid memory address or nil pointer dereference`）。
  修复：改传 `context.TODO()`。回归测试 `dto/gemini_tools_malformed_test.go`。
  反向验证：还原后该测试报 panic。
- 日志注入 | 未认证请求可控的模型名原样进入日志。`logger.logHelper` 与
  `common.SysLog/SysError` 都用 `"[LEVEL] time | id | msg \n"` 格式且不净化，
  构造 `{"model":"gpt-4\n[SYS] 2026/09/22 - 10:00:00 | 伪造条目"}` 即可在日志文件
  中插入与真实条目无法区分的行，污染审计证据链。
  修复：新增 `common.SanitizeLogLine`，在三个写入点统一净化换行。回归测试
  `common/log_sanitize_test.go` + `logger/log_sanitize_test.go`。
  反向验证：还原后日志测试报"expected exactly one line"。
- 支付 webhook 报文落盘 | `topup_stripe.go` 与 `topup_creem.go` 在**验签之前**
  把未认证的原始报文全量写入日志（含 PII，且长度不受限）。
  修复：新增 `common.LogPayloadDigest`，只记 `len=` 与截断 SHA-256。
- `main.go:174` | gin 默认信任全部代理，`X-Forwarded-For` 可伪造，直接决定
  登录/注册/邮件验证限流键与令牌 IP 白名单的比较对象。
  修复：默认 `SetTrustedProxies(nil)`，可用 `TRUSTED_PROXIES` 显式声明。
  同时 panic 响应不再回显 panic 值（可能含内部路径与上游地址），只回 request id。

### 静默失败

- `controller/billing.go:25` | `remainQuota, err = GetUserQuota(...)` 的错误被
  下一行 `usedQuota, err = GetUserUsedQuota(...)` 覆盖。`DisplayTokenStatEnabled=false`
  时若前者失败后者成功，接口不报错，`quota = 0 + usedQuota` 把余额报成"只剩已用量"。
  该端点被 ChatGPT-Next-Web 等客户端用于判断余额，会误判额度耗尽。
- `relay/channel/baidu/relay-baidu.go:184,209`、`relay/channel/cohere/relay-cohere.go:279`、
  `relay/channel/zhipu/relay-zhipu.go:293` | `c.Writer.Write` 的错误赋值后从未使用，
  客户端未收到响应体仍按成功返回。修复：检查并返回错误（注意 baidu 的返回顺序是
  `(*dto.Usage, *types.NewAPIError)`，与 cohere/zhipu 相反）。

### 死代码与误导性代码

- `model/main.go` | `commonTrueVal` / `commonFalseVal` / `logKeyCol` 只赋值不读取
  （`ability.go` 早已改用参数化 `enabled = ?`，字符串拼接被移除后变量残留）。
  已确认**布尔比较本身没有写错**：三个驱动的 bool 绑定均正确。
- `oauth/jwt_direct.go:952` `isSyncableRole`（与 `controller/oauth.go:864` 逐字重复）、
  `relay/channel/jimeng/sign.go` `SetPayloadHash`/`getPayloadHash`（前者还会把整个
  请求体写入日志）、`relay/channel/ali/adaptor.go` 两处 `syncModels`、
  `model/channel_route_scope.go:76` `routeScopeBuildMember`、`relay/gemini_handler.go`
  的 `inputTexts` 重复采集（`GetTokenCountMeta` 已采集同一份）。
- `model/channel.go:685` `if channel == nil` 对刚取址的指针恒假；
  `model/subscription.go:542`、`model/main.go:214`、`model/channel_cache.go:963`
  的空 `if` 分支。
- `model/task.go:52` | 常量组只有第一个带 `TaskStatus` 类型，其余 6 个是无类型
  字符串常量，**不能调用方法**、赋给 `any` 后动态类型是 `string`。
  **本轮尝试补齐类型后回退**：补齐会波及 10 个 provider adaptor 的约 40 处
  `Status string` 赋值，改动面远超收益，且当前代码在隐式转换下是正确的。
  记录为已知技术债。

### 前端

- `web/classic/src/pages/Setting/Operation/SettingsLogRetention.jsx:262` | `<Form values={inputs}>`
  **不是 Semi Form 的受控接口**（Semi 只读 `initValues`，`values` 会被透传到 DOM）。
  该页全文无 `refForm`/`setValues`，表单永远读不到已保存配置，且因本地 state 有值
  而 `changed` 为空，**无法保存任何修改**。对照 `SettingsLog.jsx:246` 有
  `refForm.current.setValues(...)`。修复：补 `getFormApi` + `setValues`。
- `web/classic/src/pages/Setting/Operation/SettingsHeaderNavModules.jsx:259` |
  rankings 的 `checked` 传入整个 `{enabled, requireAuth}` 对象（truthy），
  开关恒定显示为"开"，管理员无法判断排行榜是否已关闭。
- `web/default/src/features/usage-logs/components/filter-auto-complete.tsx:55` |
  命中缓存时提前 return，跳过负责复位 `loading` 的 `finally`，spinner 永久旋转。
- `web/default/src/features/playground/components/playground-settings-panel.tsx:361` |
  `key={`${index}-${url}`}` 把输入值编进 key，每次按键都重挂载 Input 导致失焦。
- `web/default/src/features/wallet/components/gift-code-card.tsx:145` | 未检查
  `copyToClipboard` 的布尔返回，复制失败仍提示成功。礼品码链接是唯一交付途径。
- `web/default/src/features/profile/components/dialogs/delete-account-dialog.tsx:63` |
  删号后不清 react-query 缓存与 `uid`、不 reload，`staleTime` 10s 内上一账号的
  缓存会直接渲染。对照 `sign-out-dialog.tsx` 的正确序列。
- `web/default/src/features/system-settings/general/responses-bootstrap-section.tsx:83` |
  校验 effect 依赖 `[enabled, form, t]` 不含 ping/grace，用户改回合法值后不重跑，
  错误提示永久残留且无清除路径。修复：改用 `useWatch` 订阅两个值。
- i18n | classic 侧 `SettingsLogRetention.jsx` 等新页面约 120 个 key 缺失。
  用仓库自带的 `i18next-cli extract` 生成并补齐英文值；`i18n:lint` 通过。
  default 侧用 `i18n:sync` 补齐 542 个 key。
  **顺带修复一处 key 语义冲突**：`Completion` 同时被"完成进度"（进度条）与
  "补全倍率"（表格列头）使用，sync 把它翻成"完成进度"后列头变错。
  拆为 `Completion progress` 与 `Completion ratio`。

## 第二轮补修（子代理报告的剩余项）

第一轮收口后，对三个子代理（staticcheck / unused / i18n）报告的**剩余未处理项**
逐条复读代码后落地。同样遵循"先复核、再修复、后反向验证"。

### 后端

- `relay/helper/stream_scanner.go:169` 与 `common/gopool.go:16` | SA1029 |
  停流信号通道通过**内建 `string` 键** `"stop_chan"` 在 context 中传递：写入侧在
  `relay/helper`，读取侧在 `common` 的 gopool panic handler。当前全仓无第二处
  使用该字面量，故无实际碰撞；但 panic handler 是全局的，任何未来同名字符串
  键都会静默改写行为。修复：在 `constant/context_key.go` 新增
  `ContextKeyRelayStopChan`（与仓库既有 `ContextKey` 体系一致），两侧统一引用。
  回归测试 `common/gopool_key_test.go`：写入侧改回裸字符串键时，测试以
  `stop channel not retrievable through the typed context key` 失败。
- `relay/relay_task.go:1035,1056` | SA4006 | `sunoFetchRespBodyBuilder` 与
  `sunoFetchByIDRespBodyBuilder` 对 `common.Marshal` 的 err **赋值后直接 return**，
  调用方 `RelayTaskFetch` 只判断 `len(respBody) == 0`，靠空 body 兜底。修复：
  与同文件 1118 行、1417 行两处既有写法对齐，Marshal 失败时返回
  `marshal_response_failed`。

### 前端 i18n

用脚本化差集重扫（`t('...')` 字面量 vs locale 扁平键集合）确认上一轮修复的
覆盖面，并处理子代理报告里"扫描器看不见"的部分：

- `web/default/src/i18n/static-keys.ts` | `playground-settings-panel.tsx` 的 6 个
  参数标签（`Temperature`/`Top P`/`Frequency penalty`/`Presence penalty`/
  `Max tokens`/`Seed`）经 `t(item.label)` 翻译，但键非字面量，扫描器无法发现，
  6 个键在所有 locale 中都不存在，实际表现是**任何语言下都不翻译**。
- 同因的 4 个键：`Invalid model mapping`、`Model mapping must be a valid JSON
  object`、`Model mapping must be valid JSON format`（`model-mapping-validation.ts`
  返回后由调用点 `t()`）、`These fields are saved next to operations in the same
  param_override object.`（`props.compact` 分支）。均补入 `static-keys.ts` 并同步
  到 6 个 locale。
- `param-override-editor-dialog.tsx` 的 `t(OPERATION_MODE_LABEL_MAP[...] || operation.mode || 'set')`
  | **复核后判定为不可达死代码，未改动**（更正：本记录第二版曾记为"已将 `'set'`
  改为 `'Set Field'`"，该改动实际从未落盘——写入脚本在前一处断言失败即中止，
  而当时的"验证" `grep -c "'Set Field'"` 命中的是第 100 行既有的
  `{ label: 'Set Field', value: 'set' }`，属假阳性）。
  可达性分析：`OPERATION_MODE_OPTIONS` 含 `{ value: 'set' }`，故 `operation.mode`
  为空时查表键为 `'set'`，命中并返回 `'Set Field'`（真值）；`operation.mode` 为
  非空的未知取值时该项为真值直接返回。两条路径都不经过末尾的 `|| 'set'`。
- `provider-form-dialog.tsx` | `Ticket Payload Mode` 下拉的 `Multipart` 与
  `JSON` 两个选项是裸 JSX 文本（同级 `Query`/`Form` 已走 `t()`），
  `PEM public key` 提示语也是裸字面量。三处改走 `t()`；`Multipart` 与 `JSON`
  同为协议名，加入 `sync-i18n.mjs` 的 `UNTRANSLATED_SKIP_VALUES`。
  未改动同文件的 `Authorization`/`service`/`ticket`/`X-*` 等 placeholder：
  它们是字段名/协议头名，同文件既有的 `OAuth Client ID`/`OAuth Client Secret`
  placeholder 也未翻译，属仓库既有约定。
- `web/classic` | `i18next-cli extract` 补齐本轮新增页面的 key（含
  `SettingsLogRetention.jsx` 整页 23 个、`SettingsPerformance.jsx` 11 个、
  渠道置顶 10 个、`ChannelRouteScopeWorkbench.jsx` 1 个）。补齐后 7 个 locale
  的语义 diff 为 `removed 0`，只有空值被填充（如 `占比` 从 `""` 变为 `"Share"`）。
  注：`web/classic/src/i18n/locales/zh.json` 不被 `i18n.js` 加载，不在 7 个
  locale 之内。
- 差集复核残留的 3 个"缺失键"（`管理员未设置`+title、`访问`+title、
  `和Claude不同...`）是**运行时字符串拼接**的 key（`DocumentRenderer/index.jsx`
  与 `SettingGeminiModel.jsx`，两文件本轮未改动），完整键在 locale 中存在，
  属扫描器误报，未改动。

## 第三轮：i18n 空串回退缺陷与剩余 SA 项

第三轮由子代理复核发现并实证了一个**由本会话第二轮操作引入**的缺陷，另处理了
两个剩余 SA 项。

### 空串值阻断语言回退（本会话引入，已修复）

`web/classic` 的 `i18n:extract` 会为缺失的键**写入空字符串**而不是跳过。用最小
复现隔离确认该行为（`/tmp` 下的独立 i18next-cli 工程：只有一个键缺失时，extract
在 fr.json 写入 `"Hello world": ""`），并用真实 `i18next` 实例复现后果：

```text
fr: 键不存在          -> "保存日志保留策略"（回退 zh-CN，中文可读）
fr: 键存在但值为 ""   -> ""（不回退，渲染空白）
```

即：i18next 只在**键不存在**时回退到 `fallbackLng`，键存在而值为空串时直接返回
空串。第二轮补齐 classic 缺失键时，extract 在 fr/ja/ru/vi/zh-TW 各写入 53 到 57
个空值条目（合计 274 个，涉及日志保留、模型性能、渠道置顶三块新 UI 的全部
label/按钮/toast/title/aria-label），把"显示中文"变成了"渲染空白"，比修复前更差。

修复：删除这 274 个"新增且值为空"的条目，恢复"键不存在 -> 回退 zh-CN"的原状。
删除后各 locale 相对 HEAD 均为 `removed 0`，只保留有真实译文的 64 到 120 个新增
键，新增键中**空值为 0**。

`i18n:lint` 与 `i18n:sync` 都检不出这一类：sync 只看键是否存在，lint 的
`isLikelyUntranslated` 只判 `value === baseValue`，空串不等于 base。

另确认一项**存量**问题（HEAD 即有，本轮未改动）：classic 的 `en.json` 有 1208 个
键值为空串而 `zh-CN.json` 有真实中文（如 `CAS 服务端 URL`、`GET`），英文用户看到
空白；`zh-CN` 为 0 空值，所以中文用户不受影响。范围与成因超出本轮任务边界，
仅记录。

### 剩余 SA 项

- `types/relay_format.go:6` | SA9004 | `RelayFormat` 常量组只有首个常量显式标注
  类型。先实测类型化代价：为全部 12 个常量补 `RelayFormat` 后 `go build ./...`
  与 `go vet ./...`（含测试文件）均 exit 0，无一处赋值/switch/类型断言因此失败，
  故**已修复**。
- `relay/channel/claude/relay-claude.go:1187` | SA9003 | `if PromptTokens == 0 { //上游出错 }`
  空分支，无副作用。改为说明性注释（语义不变）：缺失用量由紧随其后的分支补算。
- `common/gin.go:191` 的空 `else`：保留。它明确表达"未支持的 content-type 即跳过"
  并带有 TODO，是刻意的控制流说明，改写不会增加信息。
- 其余 SA 项经复核后**不改**：
  - `model/task.go:52` SA9004：第二轮已实测类型化会波及 10 个 provider adaptor
    的 `Status string` 字段，代价大于收益，维持现状（见"未覆盖与边界"）。
  - `middleware/request-id.go:25` SA1029：该键是 HTTP 头名 `X-Oneapi-Request-Id`，
    同时用于 `c.Set`、`c.Header` 和 context；若类型化，`c.Set`/`c.GetString` 与
    头名写入都需加转换，且跨包读取点（`logger/logger.go`）必须同步。当前无碰撞
    可能，改动收益低于风险。
  - `setting/ratio_setting/model_ratio.go:894-897` SA4000：`0.59 / 0.59` 是该 map
    的**既有书写约定**（用 `输出价 / 输入价` 表达补全倍率，比值相等时字面相同），
    HEAD 即如此，改成 `1` 会破坏可读性。
  - `controller/midjourney_polling_test.go:267`、`relay/responses_via_chat_test.go:464`
    SA1012：两处传 `nil` 给已被显式设计为接受 nil 的函数
    （`fetchMidjourneyTaskBatchForTask` 有 `if ctx == nil { ctx = context.Background() }`），
    测试意图正是覆盖该分支。
  - `router/api-router.go:345,351` SA1019：调用本仓库自己标记为 deprecated 的
    handler，属刻意保留的兼容路由。

### 根因修复：空串不再阻断语言回退

第三轮先只清理了产物，复核时实测**每次执行 `i18n:extract` 都会重新写入同样的
274 个空值**（`bun run i18n:extract` 后 fr/ja/ru/vi/zh-TW 各新增 53 到 57 个空值，
与清理前完全一致）。只清产物等于治标：任何人再跑一次 extract 缺陷就复现。

改为修运行时：在 `web/classic/src/i18n/i18n.js` 的 `init` 中加
`returnEmptyString: false`。i18next 默认只在**键不存在**时回退 `fallbackLng`；
该选项让"键存在但值为空串"也走回退。

用真实配置与真实 locale 数据实测（7 个 locale × 7 个代表键）：

```text
修复前：fr/ja/ru/vi/zh-TW 的部分键返回 ""（BLANK）
修复后：7 个 locale 全部 ok，无一个键解析为空
```

该选项同时消除了**存量**问题：`en.json` 有 1208 个键值为空串而 `zh-CN.json` 有
真实中文（`CAS 服务端 URL`、`GET` 等），英文用户此前看到空白，现在正确回退到
`zh-CN`。

安全性核对：不存在"刻意置空以抑制显示"的键——7 个 locale 全为空串的键为 0 个，
且源语言 `zh-CN` 自身空串数为 0。修复后重跑 extract，空值条目仍被写入但 UI 无
空白，证明缺陷已从"产物依赖"变为"运行时免疫"。

## 第四轮：子代理复核确认的缺陷

### ja 复数后缀被回填进译值，日语界面泄漏内部后缀

`i18next-cli` 抽取时对复数族生成 `_one` / `_other` 等后缀键。`ja.json` 有 18 条
的**译值本身以复数后缀结尾**，即后缀被当作源文本一字译进了值里：

```text
"已删除 {{count}} 条失效兑换码_other": "無効な引き換えコードが{{count}}件削除されました_other"
"确定要删除所选的 {{count}} 个模型吗？_other": "選択した{{count}}個のモデルを削除してもよろしいですか？_other"
```

`Intl.PluralRules('ja').select(n)` 对**任意** n 都返回 `other`，因此 `count=1/2/5`
全部命中 `_other`，用户可见文本直接带上 `_other`。实测渲染结果：

```text
修复前 ja count=1 -> "選択した1個のトークンを削除してもよろしいですか？_other"
修复后 ja count=1 -> "選択した1個のトークンを削除してもよろしいですか？"
```

影响面：`ja.json` 是**唯一**存在该缺陷的 locale（其余 6 个均为 0 条）。以"源码
引用的复数基数键 × 7 locale × count∈{1,2,5}"为判定矩阵，修复前仅 ja 有 18 处
后缀泄漏，修复后 7 个 locale 全部为 0。

该缺陷**先于本会话存在**，非本分支引入：`ja.json` 未被本会话改动
（`git diff HEAD -- web/classic/src/i18n/locales/ja.json` 只含第二轮删除空值的改动），
最后一次实质修改来自 `cac5eea9e`。

修复方式为**按行精确替换**，仅剥离 18 个值的尾随后缀：

```text
diff vs 修复前：变更行 36 行（18 处配对）
键顺序一致: True   键数: 4934   值变更条目数: 18
```

复现脚本判定矩阵（修复前后）：

```text
             修复前  修复后
zh-CN/zh-TW/en/fr/ru/vi   0   0
ja                       18   0
```

**同时确认一个非缺陷**：`web/classic` 的 `i18n.js` 未设 `keySeparator`，
i18next 默认 `'.'` 会把 163 个含点号的键（如 `common.changeLanguage`、
`0.002-1之间的小数`）按路径拆分。实测该情形**无害**：`ResourceStore.getResource`
的 `deepFind` 兜底（`ignoreJSONStructure` 默认 true）先尝试字面键
（`if (obj[path]) return obj[path]`），163 个键在 7 locale 下解析结果与显式设置
`keySeparator: false` **完全一致**（正常 701、空 0、回退成键 440）。无需改动。

### 测试注释与真实覆盖面不符

`common/gopool_key_test.go` 的 `TestRelayPoolPanicHandlerSignalsStopChan`
注释声称"panic handler 必须真正到达 stop channel，因此 relay 协程中的 panic 仍会
拆掉流"。实测该说法对**生产**不成立：唯一的 `RelayCtxGo` 调用点
（`relay/helper/stream_scanner.go`）闭包自带 `defer recover()`，它在 gopool 看到
panic 之前就吞掉并自行调用 `notifyStop()`。该测试改的是**处理器接线契约**
（闭包无自己的 recover，panic 逃逸到 gopool），并非生产热路径。

已按实际覆盖面改写两处注释，不改测试逻辑。两处测试仍通过：

```text
--- PASS: TestRelayStopChanContextKeyIsShared
--- PASS: TestRelayPoolPanicHandlerSignalsStopChan
```

### Claude 注释的因果声明为假，已改为如实描述

`relay/channel/claude/relay-claude.go` 的 `HandleStreamFinalResponse` 中，我把 HEAD
原有的空分支（`if PromptTokens == 0 { //上游出错 }`）替换成注释时，写了
"下面的分支会为缺失的字段补算，覆盖这一情形"。该声明**不成立**。

下分支的守卫是 `CompletionTokens == 0 || !Done`，而 `PromptTokens` 的回填嵌在
守卫**内部**。因此当 `Done == true` 且 `CompletionTokens > 0` 而 `PromptTokens == 0`
时整段被跳过，`PromptTokens` 保持 0：

```text
message_start: PromptTokens = Message.Usage.InputTokens      -> 0（上游未回报）
message_delta: if Usage.InputTokens > 0 { ... }              -> 不进入，保持 0
message_delta: if Usage.OutputTokens > 0 { CompletionTokens } -> 120，非 0
message_delta: Done = true
结果：CompletionTokens == 0 假、!Done 假 -> 守卫不成立 -> 不回填 PromptTokens
```

该值会一路进入结算：`ValidUsage` 只要求两个 token 之一非零，此处
`CompletionTokens` 非零即判为有效，于是按 0 输入 token 计费；`:1205` 又打上
`UsageSemantic = "anthropic"`，使下游不再走估算回退。AWS Bedrock 的
`message_delta` 缺 `input_tokens` 是仓库自述的已知常规行为（该文件
`// 解决 AWS Bedrock 等上游返回的 message_delta 缺少这些字段的问题`），并非异常。

注释已按实际逻辑改写为"已知缺口"描述，**未改动回填条件**：解耦回填会改变计费
口径，属独立议题，应单独提交并补测试（`HandleStreamFinalResponse` 当前无测试覆盖）。

### 观察（未处理）：classic 的 zh.json 是孤儿文件

`web/classic/src/i18n/locales/zh.json` 被 git 跟踪（2814 个键、0 空值），但
`web/classic/src/i18n/i18n.js` 只加载 7 个 locale（en/fr/ja/ru/vi/zh-CN/zh-TW），
不含 `zh`；`i18next.config.js` 的 `locales` 同样不含 `zh`，因此 extract 也不再更新它。
`web/classic` 内没有任何代码引用它（grep 命中的 `zh.json` 全部位于 `web/default`，
是另一个文件）。它不参与构建产物，也不影响运行时。

未删除：该文件与本轮改动无关，删除属独立议题。记录于此供后续决定。

### 补齐 sunoFetchRespBodyBuilder 的覆盖缺口

`marshal_response_failed` 检查加在了两个 builder 上，但测试只覆盖了
`sunoFetchByIDRespBodyBuilder`，列表接口 `sunoFetchRespBodyBuilder`
（`POST /suno/fetch`）未被覆盖。已补
`TestSunoFetchListReportsMarshalFailureForInvalidStoredJSON`，走真实
`GetByTaskIds(userId, ids)` 查询路径。反向验证：临时回退该检查后测试失败，
恢复后通过。

### 反向验证

对 `marshal_response_failed` 修复做了反向验证——临时回退 `sunoFetchByID` 的检查后
测试按预期失败，恢复后通过，证明测试有效而非恒真：

```text
回退后: relay_task_marshal_test.go:75: Expected value not to be nil.
        builder must report the marshal failure instead of returning (nil, nil)
        FAIL
恢复后: ok  github.com/QuantumNous/new-api/relay
```

## 逐条复核后判定"非缺陷"

子代理提出但经实证不成立或不属本项目范围，未做改动：

- **SSRF（`FetchModels` / `uptime_kuma` / `channel-billing` / `ratio_sync` /
  `model_sync` / `jwt_direct` 的裸 `http.Client`）**：这些目标 URL 来自管理员
  配置（渠道 `base_url`、Uptime Kuma 分组、上游倍率源、自定义 OAuth 端点），
  不是攻击者可控输入。对它们施加 `fetch_setting` 的 SSRF 边界会破坏自建内网
  上游（如内网 Ollama、企业 IdP），与 `CR-SSRF-OLLAMA-RECHECK-2026-09-09` 确立
  的"只有显式标记的用户可控抓取进入校验"口径冲突。本轮曾改动两处后**已还原**。
- `model/twofa.go` 的 `is_used = false` 字面量：已在 SQLite 实测能正确匹配行。
- `relay/helper/valid_request.go` 的 nil 检查、`model/channel_cache.go` 的
  `range` nil 检查等：语义等价，属风格。
- `model/usedata.go` SQLite 负时间戳分桶偏差：`created_at < 0` 实际不可达。
- `service/task_polling_claim.go` 跨表主键 claim 键空间：dev 库 `tasks` 仅 1 行、
  `midjourneys` 0 行；正式库两表均 0 行，碰撞不可达。
- Realtime 逐 turn 预扣费与 `PostWssConsumeQuota` 的结算口径：与上游
  QuantumNous 实现逐行一致（`relay_realtime.go` 同样 5 处 `preConsumeUsage`，
  `PrepareRequestBilling` 同样无条件调用），非本分支引入；正式库无 realtime
  消费日志。改动它属于独立议题，本轮不动。

## 验证证据

全部命令在本轮执行并读取输出。

第四轮复跑（本会话）：

```text
go build ./...                                                   exit 0
go test ./common/... ./types/... -count=1                        exit 0
go test ./relay/... ./common/... ./types/... -count=1            exit 0，无 FAIL
go test ./relay/ -run TestSunoFetch -count=1                     ok（2 项，含反向验证）
go test ./common/ -run 'TestRelayStopChan|TestRelayPoolPanicHandler' -v
                                    exit 0，2 项 PASS
go test ./controller ./model ./relay/common ./relay/helper ./service ./common ./relay ./types -count=1
                                    全部 ok，0 FAIL
go vet ./relay/... ./common/... ./types/...   exit 0
gofmt -l <本轮改动文件>              无输出
```

classic i18n 判定矩阵脚本（`bun`，加载真实 locale 与真实 `init()` 选项，
抽 485 个源文件、4606 个 `t('字面量')` 键，对 7 locale × count∈{undefined,1,2,5} 求值）：

```text
修复后  渲染空=0  复数后缀泄漏=0   （7 个 locale 全部）
修复前  ja 复数后缀泄漏=18，其余 locale=0
```

前三轮的全量验证记录：

```text
go build ./...                      exit 0
go vet ./...                        exit 0（无输出）
go test ./... -count=1              exit 0，61 个包 ok，0 FAIL
go test -race ./model ./service ./relay/helper ./common ./relay -count=1
                                    exit 0，无 DATA RACE
gofmt -l <全部已跟踪 *.go>          无输出
```

前端（`web/default`）：

```text
bun run lint                        exit 0
bun run typecheck                   exit 0
bun run build                       exit 0
bun run i18n:sync                   exit 0，0 missing（4671 个键）
bun run i18n:sync 之后 _reports/*.untranslated.json 全部清空
```

前端（`web/classic`）：

```text
bun run lint                        exit 0
bun run build                       exit 0
bun run i18n:lint                   exit 0，未发现超出基线的新问题
bun run i18n:extract                exit 0
```

lint 基线对比（`--max-issues-per-linter=0 --max-same-issues=0`，HEAD 用
独立 worktree `git worktree add --detach` 复现，并补 `dist/index.html` 桩以让
embed 成功）：

```text
HEAD be1979283 : 1232 issues（errcheck 254 / govet 8 / ineffassign 29 / staticcheck 841 / unused 100）
第一轮工作区   : 1198 issues（errcheck 252 / govet 4 / ineffassign 20 / staticcheck 829 / unused  93）
第二轮工作区   : 1195 issues（errcheck 252 / govet 4 / ineffassign 18 / staticcheck 828 / unused  93）
第三轮工作区   : 1193 issues（errcheck 252 / govet 4 / ineffassign 18 / staticcheck 826 / unused  93）
```

第三轮再消除 2 条 SA9004（`types/relay_format.go`）与 1 条 SA9003
（`relay/channel/claude/relay-claude.go` 空分支）。

第二轮再消除 3 条：`relay/relay_task.go` 2 条 ineffassign（Marshal 的 err 未使用）
与 `relay/helper/stream_scanner.go` 1 条 SA1029（内建 string 作 context key）。
新增的 `common/gopool_key_test.go` 未引入新告警。

差异拆解：

- **消除 42 条**（`comm -23` 全量比对），含 `dto/gemini.go` 3 条 SA1012、
  `model/main.go` 3 个死变量、`relay/channel/*` 5 条 ineffassign、
  `setting/config/config.go` 与 `common/redis.go` 的 `reflect.Ptr` 等。
- **新增 8 条，均为假新增**：
  - 4 条在 `relay/helper/stream_scanner_test.go`，来自工作区中**用户自己未提交**
    的 ping 测试改动（该文件 mtime 17:53，早于本轮全部编辑），非本轮引入。
  - 4 条在 `web/*/node_modules/flatted/...`，是被 `.gitignore` 忽略的第三方
    Go 源码；HEAD worktree 无 `node_modules` 所以基线少报。

## 未覆盖与边界

- **未重建、未重启正式服务**，未对正式库做任何写入。
- **未做端到端运行时验证**：本轮所有结论来自静态分析、单元测试与
  `-race`。修复后的 realtime 计费、渠道并发、SSRF 口径等均未在真实流量下
  回归。隔离开发环境本轮只做了只读查询（`tasks`/`midjourneys` 行数）。
- **未运行 `govulncheck`**：两次超时（600s、500s）且一次产出 0 字节日志，
  本轮无漏洞扫描结果。
- `model/task.go` 的常量类型问题**已知但未修**，理由见上。
- 三库兼容结论（`DELETE ... LIMIT`）在 SQLite 上已实测；MySQL 与 PostgreSQL
  仅依据驱动源码的 `DeleteClauses` 判定，未连接真实实例验证。修复后的
  `id IN ?` 写法对三库都是标准 SQL，但**未在 MySQL/PG 上实跑**。
- 工作区在本轮开始前已有大量无关 tracked 与 untracked 改动，均未回退或整理。
- 第二轮补修同样**未做端到端运行时验证**：`ContextKeyRelayStopChan` 只经单元
  测试覆盖键的读写一致性，未在真实流式请求中断链路下验证停流信号到达。
- `relay/relay_task.go` 两处 Marshal 失败分支**无对应测试**：`common.Marshal`
  对这两个结构体（均为可序列化字段）不会失败，构造失败用例需要注入不可序列化
  值，本轮未做。
- i18n 空串回退的根因**已在运行时修复**（见下节），但 `i18n:extract` 仍会持续
  写入空值条目；这些条目现在是惰性的，不再影响渲染。产物层面未做抑制
  （未改 `i18next.config.js`），因为运行时修复同时覆盖了存量空值。
- 本轮未提交、未推送。
