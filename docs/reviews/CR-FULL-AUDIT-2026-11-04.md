# 全维度审查记录（源码 + 隔离开发服务，2026-11-04）

审查对象：`dev` 分支当前工作区（HEAD `349186420`，工作区含其它会话的未提交改动，未触碰），以及隔离开发栈 `new-api-dev-isolated`（new-api 127.0.0.1:3001、postgres 5433、redis 6380）。

审查方式：8 个只读审查代理分域审查（部署/运行时、controller 与路由、认证与安全、模型层与数据库迁移、channel 数据访问与缓存、计费与额度、relay 流式链路、两套前端），主会话对 P1 级结论逐条回到代码复核，并推翻了 1 条代理误报。全程只读：未修改源码，未重启容器，未写数据库；唯一的状态变更是在 `web/classic` 跑了一次 `bun run build` 以核对构建产物（`dist/` 被 gitignore）。

标注约定：`已验证` 指代码/构建产物/驱动源码层面的直接证据；`★` 表示主会话亲自复验过；`推断` 指机理已确认但端到端触发未实测；`未覆盖` 指需要真实环境才能下结论。

---

## 结论摘要

- P0：无。
- P1：9 条（计费正确性 3、用户状态一致性 2、V2 路由 1、流式重试 1、渠道状态并发 1、classic 前端主题 1）。
- P2：约 20 条。
- P3：若干，按域汇总。
- 被推翻的代理结论 1 条（ability 播种的 `SELECT *` 覆盖）。

---

## 一、P1

### P1-1 订阅预扣把数据库错误改写成"没有活跃订阅"，请求静默改走钱包或误报 403 ★
- 位置：`model/subscription.go:1043-1049`、`service/billing_session.go:225-227`、`service/billing_session.go:422-441`。
- 机理：`forUpdate(tx).Where(...).Find(&subs).Error` 的任何错误都被替换成 `errors.New("no active subscription")`；`billing_session` 用字符串包含匹配把它映射成 `ErrorCodeInsufficientUserQuota`；`subscription_first` 分支看到"额度不足"就回退 `tryWallet()`，`subscription_only` 用户直接收到"订阅额度不足或未配置订阅"的 403。
- 触发：查询出现瞬时连接错误、锁等待超时或 context 取消。
- 后果：有有效订阅的用户被改从钱包扣费；真实错误被吞，日志无 DB 错误记录。
- 证据：上述三处代码；模型层没有哨兵错误，只有字符串匹配。

### P1-2 链式升级订阅过期只回退一档，用户永久保留中间分组 ★
- 位置：`model/subscription.go:884-914`（同型 `:409-441`）。
- 机理：过期处理在"无活跃升级订阅"时取 `end_time desc` 的最后一条已过期记录，直接把用户分组设为其 `PrevUserGroup`，不校验该分组对应的旧订阅是否仍有效。
- 触发：先买 VIP（prev=default），VIP 有效期内再买 SVIP（prev=VIP）。SVIP 到期（或管理员取消）时用户被设回 VIP，此时 VIP 订阅早已过期。
- 后果：所有订阅失效后用户仍持有 VIP 分组倍率与权益，直到下一次购买或人工修改。
- 证据：代码路径；`AdminInvalidateUserSubscription`/`AdminDeleteUserSubscription` 走同一逻辑。

### P1-3 `User.Update`/`Edit` 忽略 `First` 错误并无条件回写缓存，软删用户可被写回"启用" ★
- 位置：`model/user.go:588-595`、`:611-624`；`controller/user.go:967`、`:984-985`、`:1091-1106`；`middleware/auth.go` 的 `GetUserCache` 路径。
- 机理：`newUser := *user` 后 `DB.First(&user, user.Id)` 的返回值被丢弃。对软删除行该查询返回 not found 且不清空目标结构体（GORM 语义），随后 `Updates` 命中 0 行且 `Error == nil`，最后 `updateUserCache(*user)` 把调用方快照整哈希写入 Redis。
- 确定性触发：`ManageUser` 用 `DB.Unscoped()` 载入目标用户，管理员可对已软删用户执行 `action=enable`；`enable` 分支不失效缓存（只有 disable/promote/demote 失效）。
- 后果：Redis 启用时，该用户既有令牌在缓存 TTL（默认 60s）内重新通过 `TokenAuth`。
- 附带说明（已用探针修正）：GORM 在 `Updates` 成功后会把赋值写回模型结构体，因此成功更新时缓存内容与数据库一致，"缓存写旧值"不成立；真实缺陷只有软删行/0 行更新后仍回写快照这一条。
- 证据：上述代码；仓库内 `controller/user_manage_delete_cache_test.go` 的注释已描述 delete 分支的同类机理，模型层入口仍敞开。

### P1-4 `UpdateChannelStatus` 在锁外读取、锁内整行 `Save`，并发下丢失更新 ★
- 位置：`model/channel.go:1104`（读）→ `:1108-1111`（加锁）→ `:1114` → `:1136`（`Omit("key","used_quota","balance").Save(channel)`）。
- 机理：多密钥渠道的 `MultiKeyStatusList` 修改基于加锁前读出的整行快照；两个 key 并发被自动禁用时，后写者用旧快照覆盖先写者。`channel_info` 的整块 JSON 另有一个写者：`model/channel.go:418-427`、`:452-454` 在未启用内存缓存时用请求选择渠道时的快照写回。`Save` 只排除 3 列，`models`/`group`/`model_mapping`/`settings`/`status` 等全部来自旧快照。
- 触发：`controller/relay.go:3298-3301` 对每个失败请求 `gopool.Go(service.DisableChannel)`；同一多密钥渠道不同 key 并发失败，或自动禁用与管理员编辑并发。
- 后果：已自动禁用的 key 复活继续接流量；"全部 key 禁用→渠道禁用"状态丢失；管理员刚保存的模型列表/映射被静默回滚；手动禁用（2）可能被写成自动禁用（3）。
- 补充：`MemoryCacheEnabled` 时全局锁只覆盖"改+写"，不覆盖读；且锁内同步执行全量 `InitChannelCache()`，重建失败仍返回成功。

### P1-5 V2 路由播种可产生合计 20000 bps 的 scope，运行期静默归一化且阻塞迁移
- 位置：`model/ability.go:730-735`、`:833-846`；`model/channel_route_selector.go:109-116`；`model/channel_route_scope.go:1295-1328`、`:1154-1156`。
- 机理：判断"scope 已存在"只统计 `abilities.enabled = true AND channels.status = enabled` 的行；同 scope 现有渠道全部禁用期间新增渠道，或并发创建两个渠道时，各自拿到 `RouteWeightTotalBPS`（10000）。运行期发现合计≠10000 就静默按比例归一化，持久值仍是 20000。
- 后果：scope 列表显示 20000，实际流量被静默改成 50/50；迁移 apply 遇到合计≠10000 的 scope 计 `SkippedScopes` 并整体回滚，V2 迁移无法完成。
- 边界：仅在 `CHANNEL_ROUTING_V2_ENABLED=true` 生效；生产为 false，隔离环境默认 true。

### P1-6 prelude 重试标记跨 attempt 残留，可在"内容已下发"后重试并重复结算
- 位置：标记置 true 于 `relay-openai.go:136`、`relay_responses.go:1245`、`chat_via_responses.go:350`、`relay-claude.go`、`relay-gemini.go:1593`、`aws/relay-aws.go:398`；判定 `controller/relay.go:2079-2082`、`relay/responses_handler.go:547`、`relay/common/stream_interrupt.go:27-29`。
- 机理：该标记存于 gin context，`beginResponsesStreamRelayAttempt` 每次 attempt 只重置 `StreamStatus` 与序列号，不重置标记。attempt 1 在首个内容帧前失败时标记保持 true；attempt 2 命中从不参与置位/清除的渠道（dify、cohere、zhipu、tencent、xunfei、ollama、cloudflare、coze、palm、`aws/converse.go`、`codex/folded_sse.go`、`openai/responses_via_chat.go`），先写出内容再返回可重试的 502，`shouldRetry` 的 `Written() && !preludeSafe` 被残留的 true 短路，进入 attempt 3，同一 HTTP 响应追加第二份内容。
- 后果：客户端收到重复内容，按两次结算。
- 边界：需要"attempt 1 参与渠道早失败 + attempt 2 非参与渠道写内容后失败"的渠道组合；出现频率未实测。

### P1-7 Cloudflare 渠道 `/v1/completions` 返回 nil usage 并在调用方 panic ★
- 位置：`relay/channel/cloudflare/adaptor.go:104-126`（`DoResponse` 无 `RelayModeCompletions` 分支）、`relay/compatible_handler.go:194`（`usage.(*dto.Usage)` 无 nil 判断）。
- 机理：请求侧 `ConvertOpenAIRequest` 明确支持 Completions（`adaptor.go:58-62`），响应侧 switch 缺分支，裸 `return` 后 usage 为 nil 接口；`TextHelper` 在 `newApiErr == nil` 时直接断言。
- 后果：gin Recovery 返回 500；panic 路径下预扣费退款不执行（退款依赖 error 返回）。
- 边界：需对 Cloudflare 渠道发起 `/v1/completions`（含 stream）。

### P1-8 classic 前端默认调色板整体失效（Tailwind 4 `--color-*: initial` 后未回填）★
- 位置：`web/classic/src/index.css:19`（`--color-*: initial;`），`@theme inline` 只回填了 `semi-color-*` 与圆角。
- 证据（本轮实跑）：`web/classic` 执行 `bun run build`，退出码 0（5m55s），产物 `dist/assets/index-DDHHSVTI.css` 中 `.bg-white`、`.text-white`、`.text-gray-500`、`.bg-red-500` 选择器计数均为 0，而 `text-semi-color-*` 正常生成。源码中默认调色板工具类约 794 处（`text-gray-500` 97、`text-gray-600` 59、`bg-gray-50` 41、`bg-white` 32 等）。
- 影响：`bg-white`、`text-white`、`gray/slate/blue/red/green` 系列类全部不产出 CSS，颜色回落为继承/透明。典型可见点：`DashboardHeader.jsx:47,54` 的绿/蓝图标按钮背景、`headerbar/index.jsx:69` 的顶栏背景、`ChatArea.jsx:68-75` 深色渐变上的白字。
- 附带：`dark:` 变体未声明 `@custom-variant`，编译为 `prefers-color-scheme` 媒体查询，与应用内 `html.dark` 主题开关不一致；当前只有 3 处 semi 色 `dark:` 变体生成，同样挂在媒体查询上。
- 边界：该缺陷早于 v4 迁移（v3 用 `theme.colors` 整体替换默认调色板），但当前源码与构建产物确实如此；生产使用 classic 主题，属大面积视觉退化而不是功能/安全失效。

### P1-9 生产/运行时安全基线（只读观察，均为已验证）
1. 生产 PostgreSQL 与 Redis 共用同一最低强度口令 `123456`（对 `new-api` 容器 `SQL_DSN`/`REDIS_CONN_STRING` 取哈希比对，与 `123456` 的 SHA-256 一致）。两个服务未发布宿主端口，属纵深防御缺口。
2. 生产 `new-api` 直接发布在 `0.0.0.0:13000`，与 `deploy/env/macos-docker-tun.env.example:51` 要求的 `127.0.0.1:13000:3000` 不一致；LAN 上存在绕过 Caddy 的第二个入口。
3. 生产运行镜像（`4527d7666-litefix2`，2026-09-08）早于非 root 化提交 `a6a0d5cf5`，`docker top` 显示 `USER=root`，Entrypoint 为 `["/new-api"]` 而非 `/entrypoint.sh`。
4. npm CLI 版本刷新对 `@openai/codex` 永久失败：`service/npm_cli_version.go:27` 上限 4 MiB，registry 返回 5,366,933 字节（accept 简版）/18,473,104 字节（完整版），日志每 10 分钟一条 `metadata exceeds maximum size`，`last_errors` 已记录 `npm_registry_decode_failed`。
5. 隔离开发镜像 build-info 为 `349186420-dirty`：工作区未提交的 `EditChannelModal.jsx` 内容被编译进二进制（`grep` 命中 3 次），"commit 与 HEAD 一致"不等于"镜像内容与 HEAD 一致"。

---

## 二、P2

### 计费与额度
- **Redis 全量回写覆盖增量扣减** `model/user.go:909-934`（`GetUserQuota` 回源后 defer 异步 HSET）+ `model/user_cache.go:215-220` vs `:152-160`（`HINCRBY`）。缓存缺失瞬间与扣减并发时，快照 HSET 可晚于增量落地，把已扣额度"复活"到缓存，最长一个 TTL（默认 60s）。`DecreaseUserQuotaGuarded` 保证预扣阶段不会把 DB 扣成负数；但终态差额走无守卫的 `DecreaseUserQuota`，理论上仍可能按虚高缓存放行后把余额推成负数。同类全量回写还有 `model/token.go:245-277`。★（机制）
- **令牌额度准入是先读后改，无 SQL 守卫** `service/quota.go:408-429`、`model/token.go:442-470`。并发请求可同时通过检查并各自相对扣减，`remain_quota` 可被扣成负数；`BATCH_UPDATE_ENABLED` 下扣减延迟 5s，窗口更大。属令牌预算超用，非资金损失。★
- **订阅重置任务在"到期但不可重置"的行满批时紧循环** `service/subscription_reset_task.go:79-92`、`model/subscription.go:1164-1205`。计划改为 `never` 或下次重置超出 `end_time` 时函数直接返回且不写 `next_reset_time`，批次永远满 300，`for` 循环无 sleep 反复开事务。推断（需真实数据触发）。
- **任务提交后结算失败只进 stderr** `controller/relay.go:3857-3859`、`:3897-3899`。文本/音频/realtime 有 `MarkSettleFailure` 写消费日志 `other`，任务路径没有，无法对账。★
- **钱包退款失败无重试、无对账记录** `service/funding_source.go:59-66`、`service/billing_session.go:107-123`。仅 `SysLog` 一行，用户损失预扣额度时无日志行可查。
- **OpenRouter Claude 计费做无下限减法** `service/text_quota.go:346-357`（`PromptTokens -= CacheTokens` 再 `-= CacheCreationTokens`）。上游 usage 自相矛盾（cached > prompt）时可为负且无 0 下限保护，最终配额可能被压低（仅当总额≤0 才置 1）。推断（依赖上游数据形态）。

### 认证与安全
- **Telegram 绑定缺 OAuth state** `router/api-router.go:67`、`controller/telegram.go:86-146`（同类的 login/WeChat 路径都校验 state）。会话 cookie `SameSite=Strict` 是当前主要缓解；同站攻击源（父域下其他子域）或放宽 SameSite 后可直接利用，绑定后攻击者可用 Telegram 登录进入受害者账户。★（代码事实；利用面推断）
- **`TRUSTED_PROXIES=auto` 信任整个容器子网** `common/trusted_proxies.go:91-107`、`:135-173`。同网段容器可伪造 `X-Forwarded-For`，绕过 IP 白名单与 IP 级限流。仅在显式启用 auto 的部署存在；生产当前为空，不受影响。★
- **搜索/建议限流把 0 当关闭时行为不一致** `middleware/rate-limit.go:316-344`（`userRateLimitFactory` 缺 `maxRequestNum <= 0` 出口，`rateLimitFactory:159-178` 有）。Redis 路径 `LIndex(-1)` 空串 → `time.Parse` 失败 → 全站 500；内存路径恒 429。★

### controller / 路由
- **`ManageUser` 的 `id=0` 落到第一行用户** `controller/user.go:961-968`。`Where(&User{Id: 0})` 不产生条件，`Unscoped().First` 返回最低 ID 行（通常是 root），`if user.Id == 0` 永不命中。角色检查使非 root 无法利用，实际是 root 的误操作/构造请求风险。★（GORM 语义）
- **`SetupContextForSelectedChannel` 失败在 distributor 直接 500，绕过 relay 重试** `middleware/distributor.go:308-319`、`:376-392`；relay 侧对同一 `ErrorCodeChannelNoAvailableKey` 是排除渠道后继续（`controller/relay.go:1018-1025`）。多密钥渠道所有 key 不可用时返回 500 而不故障转移。★
- **批量删除日志无上限、不用 WithContext** `controller/log.go:271-327` → `model/log.go:1251-1261`。`hasScopedLogFilter` 允许仅按 `log_type` 或 `start_timestamp=1` 限定，管理员可一条语句清空 logs；请求取消不传播。★
- **`GetLogsSelfStat` 按 Username 过滤** `controller/log.go:150-194`。改名或用户名复用会把他人历史日志计入聚合（行级权限仍是 user_id）。推断。

### 模型层与数据库迁移
- **`TwoFA.Update` 用部分结构体 `Save` 写零值** `model/twofa.go:84-89`、`controller/twofa.go:95-100`。控制器只填 `{Id, UserId, Secret, IsEnabled:false}`，`Save` 写全部字段：`created_at` 变成零值时间（MySQL 严格模式推断 1292 失败；PG/SQLite 写成公元 1 年），`failed_attempts`/`locked_until`/`last_used_step` 被重置，绕过锁定策略。★（代码与 GORM Save 语义）
- **quota_data 迁移先做 DML、后补列** `model/quota_data_migration.go:33-61`、`:66-74`。索引缺失时先 `mergeQuotaDataBucket`（读取全部模型列含 `token_used`），补列的 `AutoMigrate` 排在所有 merge 之后；从缺 `token_used` 的旧 schema 升级且存在重复桶时，merge 报列不存在，`InitDB` 失败。★（代码；旧 schema 场景推断）
- **prefill_groups 旧唯一索引在 PG 上不会被清掉** `model/prefill_group_migration.go:126`。`ALTER TABLE ... DROP CONSTRAINT IF EXISTS "idx_prefill_groups_name"` 对普通唯一索引是 no-op（GORM PG 驱动建的是 `CREATE UNIQUE INDEX`）；MySQL 分支只处理 `uk_prefill_name`。从 2025-08-04..05 版本升级的库上，软删组名仍被旧索引占用。★
- **V2 标签批量更新无法把权重改成 0** `model/ability.go:1063-1080`。`Updates(ability)` 用结构体，`Weight=0` 被当零值跳过。★（GORM 语义；仅兼容模式可见）
- **邀请额度转余额不同步缓存** `model/user.go:433-445`。事务内直写 `quota`，不调用 `cacheIncrUserQuota` 也不失效缓存，转移成功后最长 60s 内仍被判"额度不足"。★
- **多实例无渠道缓存失效通道** `model/channel_cache.go:305-316`、`main.go:103`。只有本进程周期全量重建，仓库无 Redis Pub/Sub；实例 A 禁用/改 key 后，实例 B 最长 `SYNC_FREQUENCY`（默认 60s）继续按旧配置转发。
- **周期重置后到达的退款抹掉重置后用量** `model/subscription.go:1288-1307`。`newUsed` 负值被静默夹到 0，退款接口仍返回成功，平台少计费。推断。

### relay 流式
- **自管扫描器渠道在客户端断开路径漏写 StreamStatus** `relay/channel/xunfei/relay-xunfei.go:187-188`（直接 return，不调 `RecordStreamEnd`）、`relay-cohere.go:147-207`、`relay-tencent.go:161-189`。断开被判成 `error`/`upstream_transport_interrupted`，健康探针与计费分类失真。
- **上游请求未绑定客户端 context，且 `RELAY_TIMEOUT` 默认 0 无超时兜底** `relay/channel/api_request.go:692/738/1081` 用 `http.NewRequest`；`service/http_client.go:380-384`、`common/init.go:122`。客户端断开后上游请求继续，挂死时连接/计费会话/并发租约长期占用。
- **错误被吞成成功**（已修，见第八节第 17-19 项）：`relay/channel/cloudflare/relay_cloudflare.go:109-125`（`scanner.Err()` 只记日志，照常 `[DONE]` 并按估算计费 ★）、`coze/relay-coze.go:222-231`（带内 error 只 SysLog，`return nil`）、`palm/relay-palm.go:148-212`（读/解析失败都只 SysLog 并发 `[DONE]`）。
- **xunfei 从不检查上游 `header.code`** `relay/channel/xunfei/dto.go:37-42`、`relay-xunfei.go`（全包无读取）。鉴权/限流类错误帧被包装成 200 空流 ★。
- **`writeRelayErrorResponse` 对 Claude/Gemini 流中错误写裸 JSON** `controller/relay.go:515-522`、`:551`。已写过帧时 Content-Type 仍是 `text/event-stream`，追加的 `{"type":"error",...}` 无 `data:` 前缀，合规 SSE 客户端会忽略。★
- **Baidu `is_end` 未映射为终止** `relay/channel/baidu/relay-baidu.go:96-113`、`:138-158`。共享扫描器只认字面 `[DONE]`，若上游不发 `[DONE]`，所有健康流被判 `UpstreamInterrupted`，渠道健康探针永远无法标记成功。推断（上游行为）。
- **Nova 流式请求被静默降级为非流式** `relay/channel/aws/adaptor.go:213-217`（`IsNova` 判定先于 `IsStream`）。

### 前端
- **read-only 守卫覆盖不到 XHR（sse.js）** `web/default/src/lib/readonly-guard.ts:211-247`、`web/classic/src/helpers/apiCore.js:74-99` 只 patch `fetch`；`sse.js` 用 `new XMLHttpRequest()`。代理层（nginx 405）仍是第二层防护，缺口在前端这一层。
- **classic GET 去重不豁免 AbortSignal** `web/classic/src/helpers/apiCore.js:103-127`（default 已在 `lib/api.ts:51-66` 豁免）。现有带 signal 的调用点都同时传了 `disableDuplicate`，属机制缺口。
- **固定像素宽度 modal 在移动端溢出** `PayloadContentModal.jsx:151`（960）、`MultiKeyManageModal.jsx:479`（900）等；Semi `Modal` 宽度为像素值且无 `max-width`。

---

## 三、P3（摘要）

- classic 多语言缺键（en 基准下 zh-CN 缺 10 个、zh-TW 缺 9 个，已在 i18n 基线登记）；default 两处中文串作 i18n key 无对应条目。
- classic `getUserIdFromLocalStorage` 对损坏的 localStorage 无 try/catch，模块顶层抛异常可白屏。
- classic 退出登录不重建 axios 实例，`New-API-User` 头短暂保留旧 ID。
- classic 直接 `import DOMPurify` 但 `dompurify` 不在 dependencies（靠 mermaid 传递依赖提升）。
- `DocumentRenderer` 把管理员 HTML 中的 `<style>` 在净化前注入 `document.head`（CSS 注入，非 XSS）。
- 请求层无统一超时（两个 axios 实例 `timeout` 均未设置）。
- `GetAllChannels` tag 模式、`SearchChannels` tag 分支无分页；`FetchModels` 无响应体大小上限。
- `Register` 邮箱 check-then-insert，无唯一约束/锁（并发同邮箱注册可同时通过）。
- OAuth 绑定路径不复读用户状态，被禁用用户的旧会话仍可完成第三方绑定。
- 桌面端 OAuth 回调可被持有 state 的一方置为失败（DoS + 文案反射）。
- 会话 cookie 30 天、无服务端撤销；Turnstile 通过标记随会话长期复用；TOTP 无账户级锁定。
- JWT-direct/ticket 交换错误回显上游响应体。
- 进程内邮箱验证码存储在多实例下不一致。
- `DeleteLogsByFilter` 单条无界 DELETE；`GetLogsSelfStat` 按用户名聚合。
- `DeleteDisabledChannel` 先 Pluck 后删除，不重判 status 且 `IN ?` 不分块。
- 自管流普遍不维护 `ReceivedResponseCount`、缺 `SetFirstResponseTime`，`partial` 分类永不命中。
- `responses_via_chat` 从不初始化 prelude 标记，截断流被判失败且不可重试。
- 扫描器 panic 时 `writeMutex` 不释放，ping 写协程可致 `wg.Wait()` 挂起（需 `PingIntervalEnabled`）。
- 截断的最后一个 SSE 帧被当完整帧下发。
- `BATCH_UPDATE_ENABLED` 进程崩溃窗口会丢失已换出的增量（设计取舍，需按"应扣/已扣"对账时注意）。
- `PreWssConsumeQuota` 的 `Billing==nil` 兜底分支口径不一致（当前主流程不可达）。
- `quota_data` 唯一索引迁移无并发保护；多 master 同时启动时可能报 duplicate index。
- MySQL ci 排序规则下 `GPT-4o` 与 `gpt-4o` 会进同一 quota_data 桶（未覆盖真实 MySQL）。
- V2 路由写事务与渠道写事务锁序相反（MySQL/PG ABBA 死锁，推断）。
- `ApplyRouteFieldsBackfill` 单事务全表读 + 逐行 COUNT+UPDATE，任一冲突整体回滚。
- 同一 scope 的 legacy 权重公式在列表与迁移两处不一致（列表 `Σweight+10`，迁移按 ability 累加 `Σ(weight+10)`）。
- 开启请求体限制策略后，V2 每请求全量加载启用渠道（含 key 列）。
- legacy 预扣/返还函数已无调用方，若被重新接线会绕过 BillingSession。

---

## 四、被推翻/修正的代理结论

1. **ability 播种的 `SELECT *` 覆盖（推翻）**：有代理报告 `model/ability.go:730-735` 的 `Model(&Ability{}).Joins(...).Find(...)` 生成 `SELECT *`，join 后同名 `group/priority/weight/tag` 列会让渠道值覆盖 ability 字段，导致 scope 键错误。复核结论：**不成立**。GORM v1.31.2 `callbacks/query.go:113-121` 在存在 Joins 且未显式 Select/Omit 时，会按 `Schema.DBNames` 生成带表名限定的显式列（`abilities.group, abilities.model, ...`），不会走 `SELECT *`；同名冲突不存在。该代理把 `SELECT *` 的语义当作事实，但没有验证 GORM 实际生成的 SQL。
2. **"生产 TRUSTED_PROXIES 为空是缺陷"（早前结论，已按用户决策修正）**：Caddy 暂未投入正式流量，保持为空是明确决策，不计为缺陷；本文只记录 `auto` 模式本身的信任面。
3. **"ManageUser id=0 可越权改他人账户"（降级）**：非 root 调用会被角色检查拦截，实际只有 root 自己受影响，从 P2 高危降为操作风险。

---

## 五、检查过且确认无问题的区域（摘录）

- 认证：`UserAuth`/`AdminAuth`/`RootAuth`/`TokenAuth`/`TokenOrUserAuth`/`TryUserAuth` 均回读状态；`New-Api-User` 头校验；会话 cookie HttpOnly+SameSite=Strict；`SESSION_SECRET`/`CRYPTO_SECRET` 缺失或占位即 fatal；OAuth state 校验与 nonce 消费；桌面端 handoff 的服务端 state + 确认码；CAS ticket 一次性占用；SSRF 验证覆盖用户可控 URL；日志脱敏与 CR/LF 折叠。
- 计费：`BillingSession` 的 `settled`/`fundingSettled`/`refunded` 幂等；钱包预扣走 `DecreaseUserQuotaGuarded`；预扣失败回滚顺序正确；realtime 逐 turn 与终态结算不双扣（有既有回归测试）；零用量返还预扣；配额换算饱和不溢出。
- 迁移：quota_data `ON CONFLICT` 三方言累加语义；PG `FOR UPDATE` 方言门控；保留字按方言引号；软删除部分唯一索引（PG/SQLite `WHERE deleted_at IS NULL`，MySQL 生成列）设计正确；本轮为 models/vendors 旧复合索引清理写的 PG 用例已在隔离 PG 验证。
- 流式：共享 `StreamScannerHandler` 的退出路径、单写者互斥、写截止时间与 `wg.Wait()`；`StreamStatus` 的 sync.Once 与分类契约；参与渠道的 prelude 对称性（openai chat/responses、chat_via_responses、claude、gemini、aws invoke）；Claude/Gemini/Responses 的 usage 兜底与终端帧时序；SSE 帧格式；非流式读取均走 `ReadAllWithLimit`。
- 渠道缓存：`CacheReloadChannel` 的 epoch/版本令牌；批量标签/状态路径提交后触发全量重建；三方言分页与 `DISTINCT` 查询。
- 前端：default 主题机制（`@custom-variant dark`）与构建；两套前端的 XSS 净化链路（DOMPurify / hast-util-sanitize）；read-only 守卫的路径规范化与副作用端点清单；default lint 0 error、typecheck 退出码 0；classic `i18n:lint` 无新增。
- 部署（只读）：隔离栈三容器健康、运行时 uid 1000、`/api/status` 200、迁移 2.682s、部分唯一索引就位；日志驱动轮转与 healthcheck 配置正确。

---

## 七、本会话已修复项（2026-11-04，全部 TDD + 反向验证）

以下 P1/P2 项已修复并提交，均有回归测试、`go test` 全绿、且对关键修复做了
“去掉修复→测试失败→恢复”的反向验证：

1. User.Update/Edit 吞掉 First 错误并回写软删用户缓存 → 失败即返回、更新后重读再写缓存（`05f99201e`）
2. 渠道状态更新读改写不同锁、整行 Save 丢并发更新 → per-channel 锁内读改写、只写 status/channel_info/other_info（`40c393de8`）
3. 订阅预扣把 DB 错误伪装成无订阅 → 哨兵错误 ErrNoActiveSubscription / ErrSubscriptionQuotaInsufficient（`762adfac1`）
4. 链式升级过期只回退一档保留中间分组 → 沿 PrevUserGroup 整链回退（`32ceef2d7`）
5. prelude 重试标记跨 attempt 残留致重复结算 → attempt 开始处置 false 并归零帧计数（`446685897`）
6. Cloudflare /v1/completions nil usage panic → 显式 501（`4fdbe1311`，反向验证）
7. classic 默认调色板整体失效 + dark: 绑定媒体查询 → 移除颜色清空、补 @custom-variant dark（`b16ab21f5`，重建产物验证 .bg-white 等恢复）
8. 用户级限流把 0 当关闭时 500/429 → 0/负数放行（`95e1eb79a`）
9. ManageUser id=0 落到第一行用户 → 入口拒绝 id<=0（`d0aac0ad6`）
10. TwoFA.Update 整行 Save 写零值 created_at / 清锁 → 列级更新（`1c11b8e5d`）
11. 邀请额度转余额不同步缓存 → 提交后 InvalidateUserQuotaAfterRecharge（`81c7487b6`）
12. 流式错误对 Claude/Gemini 写裸 JSON → ClaudeErrorEvent / data 帧（`f24877be8`）
13. 标签批量更新无法把权重清零 → map 更新（`9d650cc85`）
14. 日志批量删除无界 DELETE → 分批 + ctx 取消（`86d07122c`）
15. quota_data 迁移缺列启动失败 → 先补列再合并（`408de7f94`）
16. prefill_groups 旧唯一索引删不掉 → DROP INDEX IF EXISTS（`1472b1c6e`）
17. OpenRouter Claude prompt 负值压配额 → 0 下限（`b557421f7`）
18. 订阅重置任务紧循环 → 无法重置行清 next_reset_time（`ef9ea89e4`）
19. 额度缓存整值回写覆盖 HINCRBY → HSETNX 建基数 / 增量仅在存在时（`3c3d7938b`，反向验证）

保留未修（需真实 MySQL/PG/Redis/多实例，或属行为取舍，已在正文标注）：
- V2 播种 20000bps（依赖并发/禁用态创建，运行时已静默归一化；隔离环境默认开 V2）
- 多实例渠道缓存无 Redis Pub/Sub 失效、订阅套餐缓存进程内
- MySQL/PG 路由 scope 与渠道写事务 ABBA 死锁
- prefill/cohera/zhipu/tencent 各渠道终止标记与超时（需真实上游）
- TRUSTED_PROXIES=auto 信任容器子网（生产为空不受影响）
- 生产安全基线（口令/0.0.0.0:13000/root 镜像/codex npm 超限）：已写入升级方案，待用户授权执行，非本轮改动范围

---

## 八、续修项（2026-11-05）

在上一轮基础上继续修复，全部有回归测试；标注"反向验证"的条目做过
"去掉修复→测试失败→恢复"的验证：

1. 个人用量统计按会话 username 聚合 → 改用当前用户 id 过滤（`d1aee055f`）
2. Telegram 绑定缺 OAuth state（登录 CSRF） → 校验会话 state 并在成功后消费；两套前端改为先取 state 再发起绑定（`d0f085113`，反向验证）
3. xunfei 从不检查上游 `header.code`，错误帧被包装成 200 空流 → 识别错误帧并转成 API 错误 / SSE 错误帧（`64b435faa`，反向验证）
4. Baidu `is_end` 未映射为终止，健康流被判中断 → `sr.MarkDone()`（`655750ca6`，反向验证）
5. Nova 流式请求被静默降级为非流式 → 显式 501（`94842bf63`，反向验证）
6. 只读守卫覆盖不到 XHR（sse.js） → 两套前端 patch `XMLHttpRequest.prototype.open`（`ef4f0eef5`，反向验证）
7. 任务提交后结算失败只进 stderr → 写入消费日志 `other.settle_error`（`8e6955199`，反向验证）
8. FetchModels 无响应体上限 → `common.UpstreamSmallResponseLimit` 封顶（`16403e4ff`，反向验证）
9. classic GET 去重不豁免 AbortSignal → 抽到 `apiGetDedupe.js` 并豁免 signal（`0ba67776a`，反向验证）
10. classic 固定像素宽度弹窗在移动端溢出 → 全局 `.semi-modal { max-width: calc(100vw - 32px) }`（`02ad9d11c`，构建产物验证）
11. classic 损坏 localStorage 导致模块顶层抛异常白屏 → `userStorage.js` 全路径降级 -1（`211c738ba`）
12. classic 退出登录不重建 axios 实例，`New-API-User` 头保留旧 id → 退出后 `updateAPI()`（`a738d83dd`；hook 路径无自动化测试）
13. classic 直接 import dompurify 但未声明依赖 → 写入 dependencies 并更新 bun.lock（`ca523b787`）
14. `DeleteDisabledChannel` 先 Pluck 后删除、不重判 status 且不分块 → 删除语句重断言 status + 200 分块 + 只清理真正删除的 ability（`111e585c4`，反向验证）
15. OAuth 绑定路径不复读用户状态，被禁用账号旧会话仍可绑定 → `resolveEnabledBindUser` 统一复读（`190b43da6`，反向验证）
16. 上游请求未绑定客户端 context（客户端断开/超时传不到 net/http） → `http.NewRequestWithContext(GinRequestContext(c))`，并同步修正 jimeng/coze/replicate（`7c7e817f0`，反向验证）
17. cloudflare 流传输中断只记日志后照常发 `[DONE]` → 扫描错误转成 API 错误，已下发内容保留，不再声称完成（`0ede41626`，反向验证）
18. coze 带内 `event: error` 只记 SysLog 后当正常结束 → 转成 API 错误，不发 `[DONE]`、不记成功结束（`4013366a`，反向验证）
19. palm 单帧包装的读取/解析失败只记日志并发 `[DONE]` → `stopChan` 携带错误上抛，失败不发 `[DONE]`（`d0f707f98`，反向验证）

验证记录（2026-11-05，第 19 项之后重跑）：`go test ./controller ./model ./relay/common ./relay/helper ./middleware ./service -count=1` 六个包全绿；
流错误相关包 `go test ./relay/channel/{cloudflare,coze,palm} -count=1` 全绿；
`web/default` `bun test src` 72 通过、`bun run lint` 0 error（22 个既有 warning）、`bun run build` 成功；
`web/tests` `node --test` 153 通过；`web/classic` `bun run build` 成功。

仍未覆盖：真实 MySQL/PG/Redis 并发、多实例失效通道、真实上游渠道协议
（prefill/cohera/zhipu/tencent 超时与终止标记）、以及浏览器端视觉验证。

留待后续（本轮明确不做）：
- 共享扫描器把截断的最后一个 SSE 帧当完整帧下发：需要改 `bufio.Scanner` 的分行
  缓冲，属核心组件重构，回归面大；当前实现下这类流的结束原因已判为
  upstream_interrupted，只在客户端侧多一帧不完整内容。
- `GetAllChannels` tag 模式与 `SearchChannels` tag 分支无分页、`Register` 邮箱
  check-then-insert、请求层统一超时、会话 cookie 服务端撤销：都涉及行为或
  数据模型变更，需单独立项。


## 六、验证边界

- 本轮未运行 Go 测试、未连真实数据库或 Redis 做并发复现；计费/并发类结论均为代码语义级证据，端到端后果标注为推断。
- 运行时审查仅覆盖隔离开发栈与生产容器的只读观察（`docker logs`/`docker inspect`/`psql SELECT`），未做压测。
- 前端审查未启动浏览器，调色板与暗色主题以源码和本轮构建产物为依据，未截图比对渲染效果。
- 迁移类问题的触发依赖旧版本 schema 或多 master 同时启动，需对应环境才能定量确认。
- 代理结论中标注为推断的条目未经主会话逐条复现；引用时以本文的标注为准。

---

## 九、续修项（2026-11-05 至 2026-11-06）

在第八节之后继续修复，全部遵循先写失败测试、确认按预期原因失败、再最小修复
通过的方式；标注"反向验证"的条目做过"去掉修复→测试失败→恢复"的验证。原子
提交范围 `c1e329a2b` 至 `26271b0a5`：

1. npm CLI 版本刷新对 `@openai/codex` 永久失败（registry 元数据超 4 MiB 上限） → 上限提到 32 MiB（`c1e329a2b`）
2. 令牌额度预扣先读后改、并发可透支预算 → `DecreaseUserQuotaGuarded` 同型 SQL 守卫，相对扣减 + 阈值在 SQL 内判定（`a97f3c450`，反向验证）
3. `SetupContextForSelectedChannel` 失败直接 500、绕过 relay 重试 → distributor 改为故障转移到下一个渠道（`1c81fffe5`，反向验证）
4. 周期重置后到达的退款冲掉新周期用量 → 退款不再把 `newUsed` 静默夹到 0（`75b363e94`）
5. 任务路径结算/退款失败只进 stderr、无对账记录 → 退款失败写持久化错误日志（`bd2a2ba09`）
6. JWT-direct/ticket 交换错误回显上游原始响应体 → 不再回显原始 body（`46f524ca3`）
7. 流处理器 panic 时 `writeMutex` 不释放、ping 写协程可致 `wg.Wait()` 挂起 → panic 路径释放写锁（`0125b6012`，反向验证）
8. classic `DocumentRenderer` 把管理员 HTML 的 `<style>` 在净化前注入 `document.head`（CSS 注入） → 注入前先清洗 CSS（`252cd6c62`，构建产物验证）
9. `responses_via_chat` 从不初始化 prelude 标记、截断流被判失败且不可重试 → 初始化 prelude 标记（`450e1d1eb`，反向验证）
10. 自管流式渠道不维护 `ReceivedResponseCount`/`SetFirstResponseTime`，`partial` 分类与首包时间失真 → 新增 `relay/helper.RecordStreamFrame` 并接入 tencent/xunfei/cohere/zhipu/palm/dify/coze/ollama（`91e372171`，反向验证）
11. 同一 scope 的 legacy 权重公式在列表与迁移两处不一致（列表 `Σweight+10`、迁移按 ability 累加 `Σ(weight+10)`） → 迁移累加 `uint64(ability.Weight)` 后统一走 `saturatingLegacyVotes`，两处公式一致并新增跨列表/迁移一致性测试（`f1d3a8b11`，反向验证）
12. classic 删除账号后不重建 axios 实例、`New-API-User` 头残留旧 ID → 删除账号后 `updateAPI()`（`7699510dd`，静态契约测试覆盖 removeItem('user') 与 updateAPI 的邻接）
13. classic zh-CN/zh-TW 相对 en 缺键（zh-CN 缺 10、zh-TW 缺 9），中文界面露出英文键 → 补齐键并新增 key 覆盖测试（以 en 为基线，复数变体按 i18next 规则豁免）（`18f2b9664`）
14. Turnstile 通过标记是会话布尔值、随 30 天会话 cookie 长期免检 → 改存通过时间戳 + 1 小时 TTL，旧 bool 标记按过期处理（`ac65eeb34`，反向验证）
15. 桌面端 OAuth 失败提示反射攻击者可控文案（callback `error_description` 查询与上游 `OAuthError.RawError` 两条路径原样进请求记录，poll/确认页展示） → 失败提示改固定/i18n 文案（`desktopOAuthFailureMessage`），原始值只进服务端日志；顺带删除已无调用方的死文件 `service/pre_consume_quota.go`（`26271b0a5`，两条反射路径均反向验证）

验证记录（2026-11-06，第 15 项之后重跑）：`go test ./controller -count=1 -run 'Desktop|HandleOAuth|OAuth'` 全绿；
`go build ./...` 退出码 0。此前各原子任务提交时分别运行了对应包的最小测试。

仍未覆盖/留待后续（与第八节一致）：真实 MySQL/PG/Redis 并发、多实例失效通道、
真实上游渠道协议、浏览器端视觉验证；以及 tag 模式分页、`Register` 邮箱
check-then-insert、请求层统一超时、会话 cookie 服务端撤销等需要单独立项的行为
或数据模型变更。

---

## 十、收尾项（2026-11-06）

第九节列出的"留待后续"项按同样的 TDD 流程处理：先写失败测试、确认按预期
原因失败、最小修复通过；标注"反向验证"的条目做过"去掉修复→测试失败→恢复"
的验证。提交范围 `0cdf484d5` 至 `37a06b09e`：

1. 共享扫描器把截断的最后一个 SSE 帧当完整帧下发 → 自定义 split 函数，
   输入结束时没有行结束符的最后一行只有是 `[DONE]` 才保留，其余丢弃
   （SSE 规定文件尾部残留数据必须丢弃）；流结束原因仍判为
   upstream_transport_interrupted（`0cdf484d5`，反向验证）
2. 两套前端 axios 实例没有 timeout，挂住的请求无限期悬着 → default 与
   classic 统一 30s 默认超时；渠道测试/余额/拉模型 120s、classic 全量
   测试与更新 300s、上游模型与倍率同步 120s 在调用点显式放宽；新增静态
   契约测试守住实例默认值与各慢端点覆盖点（`fed5a4cdb`）
3. 会话 Cookie 30 天且无法服务端吊销 → `User.session_epoch` 会话世代：
   签发时记录、鉴权时比对，密码修改/重置用 SQL 相对更新递增并刷新缓存；
   绑定回调、Passkey、桌面端 OAuth 绑定等不走 `authHelper` 的路径复用同一
   判定；上线前的旧会话无该字段按世代 0 处理，不误杀在线用户
   （`869d2d12a`，反向验证）
4. tag 模式逐标签查询渠道（page_size 个标签 = page_size 次往返与排序） →
   `tag IN (...)` 一次查回后按标签页序聚合，组内顺序仍来自同一条 ORDER BY
   （`855c52d13`，反向验证，查询计数从 6/5 次降到 4/3 次）
5. 会话世代上线后自助改密会把操作者本人也踢下线 → 改密成功后把新世代写回
   当前会话，其它设备仍被吊销（`10474bb1b`，反向验证）
6. 吊销判定命中后只返回 401，浏览器仍继续携带失效 Cookie → 吊销响应清空
   会话并下发过期的会话 Cookie（`37a06b09e`，反向验证）

本轮同时完成的前置项（不在第九节列表内）：`Register` 邮箱 check-then-insert
串行化（`62812ab37`，反向验证：无锁时 8/8 并发注册产生重复行）、邮箱验证码
改 Redis 共享存储（`44b518440`）、JSON 解码接入 encoding/json/v2
（`6770dce7c`，见 `CR-JSON-V2-DECODE-2026-11-06.md`）。

验证记录（2026-11-06，第 6 项之后，`37a06b09e` 上重跑）：
`go test ./controller ./model ./relay/common ./relay/helper ./middleware ./service -count=1`
六个包全绿（controller 19.5s、model 8.2s、relay/common 0.1s、relay/helper 23.4s、
middleware 3.5s、service 25.7s）；`go build ./...` 退出码 0；
`node --test web/tests/*.test.mjs` 164/164 通过；
`web/default` lint 0 error（22 个既有 warning）、build 成功；`web/classic` build 成功。
其中一次六包并发运行出现 `service` 的
`TestChannelHealthConcurrentHalfOpenSelectionHasSingleProbe` 失败
（`channel health Redis unavailable; routing is failing open`，`context deadline
exceeded`）；单独 `-count=5` 重跑通过、之后两次六包整轮全绿，判为机器负载下的
既有偶发，与本次改动无关（未触碰 channel health 与 Redis）。

仍未覆盖：真实 MySQL/PG/Redis 下的并发改密与会话吊销、多实例缓存一致性、
真实上游渠道的截断流（本次只用 `httptest` 构造的响应体复现）、浏览器端
视觉验证。tag 模式仍未限制单个标签下的渠道数量：分页单位是标签而不是渠道，
要限制需要前端配合显示"还有 N 个渠道"，属产品决策，本轮未做。

灰度注意：会话世代上线前签发的会话没有世代字段，按世代 0 处理。用户在旧实例
改过密码（世代 > 0）后，滚动升级期间旧实例（不认识该字段）新签发的会话会被
新实例判为过期；升级完成并重新登录后不再出现。

---

## 十一、依赖与工具链复查（2026-11-06）

第十节之后按"全部优化好"的要求重新实测依赖与漏洞扫描面，只用可验证的
证据下结论：

1. 两套前端 `bun audit`：default 10 条（2 high）、classic 3 条（1 high）。
   其中 `follow-redirects@1.16.1`（axios 运行时依赖，high）与
   `fast-uri@3.1.8`（ajv 链）都在仓库自己的 overrides 里被钉在受影响
   版本；`body-parser@2.3.0`（high）、`qs`、`minimatch`、`glob` 属于
   shadcn / eslint / i18next-cli 开发链。已改 override：
   follow-redirects 1.16.2、fast-uri 3.1.9、body-parser 2.4.0、
   qs 6.17.1、minimatch 11.0.5、glob 14.0.4（`4f73e0b69`）。
   复查结果：default 2 条（0 high，1 moderate 1 low）、classic 0 条。
2. 剩余两条都在 `shadcn` CLI 的开发依赖里，不进产物：`@hono/node-server`
   修复版是 2.1.3（跨大版本，MCP SDK 的依赖区间不覆盖）；`deepmerge`
   受影响区间到 4.3.1 而 4.3.1 就是当前最新版，上游没有修复版本。
3. Go：`GOTOOLCHAIN=go1.27.3 govulncheck ./...`（与 Dockerfile 的
   `golang:1.27.3` 一致）结果 0 个可达漏洞；3 个模块级告警不在调用链上。
4. Rust 桌面端：`cargo audit` 扫描 475 个 crate，0 个 vulnerability，
   6 条 allowed warning（unmaintained 的 zvariant/zbus_names/
   proc-macro-error、yanked 的 async-executor、glib 0.18.5 unsound）。
   glib 修复需要跟随 tauri/gtk 大版本升级，未在本次范围内。
5. GitHub Actions：全部 `uses:` 已按 40 位 commit SHA 固定，无需改动。

验证：default `bun run lint` 0 error、`bun run build` 成功（Safari 兼容
检查通过）；classic `bun run build` 成功；i18next-cli 在 glob 14 下
`i18n:status` 正常执行；`node --test web/tests/*.test.mjs` 164/164 通过。
classic `bun run lint`（prettier --check）报 6 个文件的既有格式问题，
这些文件本次未改动（提交内容本身未按 prettier 格式化），单独记录。

仍未处理：tag 模式单个标签下的渠道数量没有上限（分页单位是标签，加限制
需要前端配合显示"还有 N 个渠道"，属产品决策）；生产容器镜像
`new-api-local:prod-codex-lite-4527d7666-litefix2`（commit `4527d7666`，
2026-09-08）仍是 dev 的祖先，落后 800 个提交，本系列修复未进生产，发布
时机需要单独决定。

## 十二、i18n 覆盖度与 relaykit 基线重录（2026-11-07）

第十一节之后又发现两处门禁失效：i18n 门禁对真实漂移永远通过，relaykit
dto 基线记录与代码现状不符。两处都按 TDD 思路先复现、再修、再反向验证。

### 12.1 classic i18n 门禁对真实漂移永远通过

现象：CI 的 "i18n extract idempotency" 步骤先跑 `i18next-cli extract`（把
缺失键写进工作区）再比对两次输出，只证明了 extract 幂等；仓库语言文件与
代码漂移时它永远通过。实测 7 个语言文件各缺 11 个代码中实际使用的键
（微信/Telegram 登录与绑定、Stripe/Creem/Waffo、浏览器确认码流程等），
门禁仍全绿。

根因（已用运行时探针证明）：extract 在本仓库的写库结构是**扁平**的。配置
为 `mergeNamespaces: true` + `nsSeparator: false` + 单一默认命名空间时，
写入路径把键并到顶层；而运行时 `i18n.js` 用
`resources: { en: enTranslation }` 加载，需要 `{"translation": {…}}` 嵌套
结构。实测扁平资源下 `t('绑定成功')` 返回键本身而非译文。因此**不能**把
extract 的输出提交进仓库，漂移只能靠只读检查发现，修复时手工补语言文件。
（`nsSeparator: ':'` 也不是可行改法：仓内 100 个键含 ASCII 冒号。）

修复：
- 7 个 locale JSON 手工补齐 11 键译文，保持嵌套结构与既有字节格式。
- 新增 `web/scripts/i18nCoverageGate.mjs`：只读跑 `i18next-cli status
  <locale>`，判定代码使用但文件缺失的键，并交叉核对 config 声明的
  locales；不写仓库。
- CI 用新步骤取代原 "extract 幂等性" 步骤；`i18next.config.js` 加注释禁止
  extract 写入语言文件。

验证：7 个语言 `status` 退出码 0；`i18nCoverageGate.mjs` 退出码 0；
`i18nLintGate.mjs` 62/62 通过；classic build 退出码 0；prettier 对改动
文件干净。反向验证：临时删除 zh-TW 的 `知道了` 后门禁退出码 1 并输出
`✗ 知道了 (absent)`，恢复后 `git diff` 为空。

### 12.2 relaykit dto 兼容基线记录与代码现状不符

现象：`relaykit-dto-compat.sh --strict` 退出码 1。根因是 `Tool.InputSchema`
的处置未落到基线：本地把该字段改为 `json.RawMessage` 保序实现后，它与
relaykit 的 `map[string]any` 由"等价拼写"变为"真类型差异"，而基线仍按旧
口径记 `type=6`/`equiv=21`。

决策（按实际使用场景）：`Tool.InputSchema` **保留本地保序实现，不采用
relaykit**。relaykit 用 `map[string]any` 承载该字段，Go map 在序列化时
重排键序，会改变结构化输出的字节顺序；本地的 `json.RawMessage` 原样透传，
保序。这属"反方向的不兼容：本地领先"，不是本地落后于上游。

修复：基线 `tools/relaykit-dto-compat/main.go` 记为 `wantType=7` /
`wantEquiv=20`（其余四维不变：`fieldSet=33 tag=5 embed=0 order=4 sig=12`）；
设计文档 §12.0.8 的全部计数、类别 A/B/C、结论与阶段表同步订正，并新增
`Tool.InputSchema` 行说明保留理由；CR 文档自述行数订正为 2179。

验证：`KIT_REF=789c97019 bash scripts/relaykit-dto-compat.sh --strict`
输出 `计数: fieldSet=33 tag=5 type=7 embed=0 order=4 sig=12 equiv=20` 且
`counts match the recorded expectation`（退出码 0）；`relaykit-gate-selfcheck.py`
退出码 0（`dto 七维 (33, 5, 7, 0, 4, 12, 20)`，22 处自述计数一致）；
`relaykit-gate-selfcheck.test.py` 47/47；`relaykit-citation-check.py`
`--ref 789c97019` 0 失败；`relaykit-doc-structure-check.py` 0 个结构问题。

### 12.3 tag 模式单个标签下的渠道数量上限

维持第十节的判断：tag 模式下分页单位是标签，单个标签下的渠道数量没有
上限。加限制需要前端配合显示"还有 N 个渠道"，属产品决策，本轮不加静默
截断。

## 十三、前端质量门禁转绿（2026-11-07）

第十二节之后复查 `repo-gates.yml` 各步骤，发现两条前端门禁在本地实测为红，
且 classic 的 eslint 门禁同样是红的。三条都修好并逐条验证。

先说边界：`repo-gates.yml` 只存在于本地分支，`origin/main` 与 `origin/dev`
的 `.github/workflows/` 下都没有该文件，`gh run list` 里也没有任何 repo-gates
运行记录。因此这些门禁从未在 CI 真正执行过，本地红灯没有被暴露。是否推送
以及何时推送由维护者决定，不属本次修改范围。

### 13.1 `web/default` 格式门禁（81 文件）

`bun run format:check`（CI `web/default format` 步骤）退出码 1，报 81 个
已提交文件不合 `.prettierrc`。

- 不是依赖漂移：prettier 3.9.10、`@trivago/prettier-plugin-sort-imports`
  6.0.2、`prettier-plugin-tailwindcss` 0.8.1 均与 `bun.lock` 一致；CI 用
  `--frozen-lockfile` 会装出相同版本。
- 不是规则变更：`.prettierrc` 自 v1.0（`a42b39760`）起未改。
- 差异以 sort-imports 按 `importOrder` 重排导入为主，少数是类型联合换行
  （如 `prompt-input.tsx`）。按引入提交日期看最早 2026-04-28、最多一批 45
  个在 2026-10-24，属于这些文件提交时未跑格式化。

修复：`prettier --write .`，81 个文件；改动全在 `web/default` 内，且这 81
个文件此前均无未提交改动，不与并行会话冲突。

### 13.2 `web/classic` 格式与 eslint 门禁（6 + 6）

`bun run lint`（= `prettier . --check`，CI `web/classic format`）退出码 1，
6 个文件；差异是双引号 vs 单引号、一个多余空行。

`bun run eslint`（CI `web/classic eslint`）退出码 1，6 个 error：

- 5 处是数字分隔符（`30_000` / `120_000` / `300_000`，ES2021 语法）与
  `ecmaVersion: 2020` 冲突，报 "Identifier directly after number"。这 5 处
  由本会话的 `fed5a4cdb` 引入（该提交前这 5 个文件的分隔符数量为 0）。配置
  的 2020 是刻意锁定（`5bf7dcb37` 只为 tests 目录放开到 2022），且 classic
  其余 src 全部遵守 2020，因此修法是改回纯整数（语义不变），而不是放宽
  解析器。`web/tests/frontendApiTimeout.test.mjs` 的校验正则
  `[1-9][\d_]*` 两种写法都接受。
- 1 处是 `documentCss.js` 许可证头 `support@quantumous.com` 少一个 `n`，
  由 `252cd6c62` 引入；改为 `quantumnous.com`。

验证：`bun run lint` 与 `bun run eslint` 退出码 0（0 error，5 warning 属既有
未使用 eslint-disable 提示）；`bun test` 222 通过；`bun run test:node` 164
通过；`bun run build` 退出码 0。

### 13.3 附带：工具二进制未忽略

`tools/relaykit-dto-compat/relaykit-dto-compat` 是 `go build` 生成、与目录
同名的二进制，此前未被忽略。`.gitignore` 增加对应规则。

### 13.4 本轮验证汇总

- `web/default`：`format:check` 0、`lint` 0 error、`bun test` 293 通过、
  `typecheck` 0、`build` 0（Safari 兼容检查通过）。
- `web/classic`：`lint` 0、`eslint` 0 error、`bun test` 222 通过、
  `test:node` 164 通过、`build` 0。
- 其余 repo-gates 步骤本轮实测仍绿：compose healthcheck 契约、dockerfile
  契约、只读代理契约、bundle 版本契约、双前端 `bun install
  --frozen-lockfile`（均 no changes）、i18n 覆盖度与 lint 门禁。
- 后端 Go 代码本轮未改动，未重复跑 Go 测试套件。

## 十四、web/default 本地化与后端 JSON 规则（2026-11-07）

第十三节之后，对 web/default 的 i18n 与后端 `encoding/json` 规则执行情况做
了专项核查。四条问题里三条属实并按 TDD 修复，一条经复核后修正了口径。

### 14.1 web/default 无"代码→语言文件"覆盖度门禁（属实）

第十三节给 web/classic 补了覆盖度门禁，但那只覆盖 classic。web/default
既没有提取器（`package.json` 只有 `i18next` / `react-i18next`），
`scripts/sync-i18n.mjs` 也只做语言文件之间的对齐，从不扫描源码。结果是
"代码用了但语言文件没有"这类漂移完全不可见。

实测缺口：`src/features/auth/api.ts` 第 85、100、118 行用
`i18next.t()` 使用了三个中文键（微信登录、Telegram 登录、Telegram 绑定
的失败提示），在 en / zh / ja / ru / fr / vi 六个文件里全部不存在。
六个调用点都在 `catch` 里丢弃原错误、改用已翻译的通用文案，所以当前
不会显示给用户，属潜伏缺陷；但只要有一个调用方改为展示 `error.message`，
非中文用户就会看到原始中文。

另有第四处：`Please use WeChat\'s "Scan QR Code" ...`。语言文件里该键
被写成带一个真实反斜杠的 `WeChat\'s`，而代码里的运行时键是 `WeChat's`，
两者永不匹配，这条提示在所有语言下都原样显示键字符串。由 `b47c444fd`
引入。普通 grep 由于转义看起来"存在"，只有按运行时语义比对才能发现。

修复：新增只读门禁 `web/scripts/i18nDefaultCoverageGate.mjs`，扫
`src` 下 `t('...')` / `i18next.t('...')` 字面量键（跳过注释行、处理转义
引号），要求 6 个语言文件都有；同时校验 `static-keys.ts` 的 550 个键都在
`en.json`。CI 的 i18n-gate job 增加对应步骤。四个缺失键按 6 个语言补齐
真实译文（不是留英文靠 fallback）。

反向验证：删除 zh 的在用键后退出码 1 并输出文件名与调用点；向
`static-keys.ts` 插入不存在的键后退出码 1 并点名该键；恢复后均通过。

### 14.2 static-keys.ts 是死代码（属实，已接线）

`web/default/src/i18n/static-keys.ts` 共 629 行、550 个键，文件注释称用于
"不被正则捕获的动态键"，但全仓没有任何引用，也没有提取器读取它。而这
550 个键确实都存在 en.json，所以内容本身没有错，缺的是执行机制。

修复：14.1 的门禁把它纳入校验，并在文件头注明由谁读取。现在"登记了但
语言文件里没有"会让 CI 失败。

### 14.3 未翻译文案（属实，已全部翻译）

sync 报告的未翻译条数：zh 260、ja 270、ru 269、fr 57、vi 57，并集 270 个
键。抽查确认这些是"值与英文相同"的真实未翻译文案（`Add image URL`、
`Capacity`、`All groups` 等），且全部在源码中有使用点。

修复：逐语言补齐 913 条译文（含占位符 `{{count}}` / `{{name}}` /
`{{amount}}` / `{{page}}` / `{{index}}` / `{{userGroup}}` 原样保留）。
翻译后 `_sync-report.json` 六个语言 missing=0 extras=0 untranslated=0，
sync 脚本删除了各语言已空的 `*.untranslated.json`。

剩余与英文同值的约 90 至 340 条/语言，是品牌名、产品名、URL、技术术语
（OpenAI、Claude、Stripe、API SDK 等），属刻意保留，不计入未翻译。

（2026-11-08 复查更正：该结论不准确。复核发现其中混有 `Download Logs`、
`Join Channel`、`Pay Method Name`、`Gift Codes` 等普通界面文案，已在
第十六节按新口径补齐。）

### 14.4 后端直接调用 encoding/json（口径修正：活代码仅 1 处）

先前报告"7 处违规"，复核后修正：

- `dto/openai_request.go` 的 6 处全部位于标注 `// old code` 的块注释
  （941-1112 行）内，不是可执行代码，因此不算违规。该块已被新实现取代，
  本轮直接删除（git 历史可追溯）。
- `relay/channel/claude/relay-claude.go:294` 的 `json.NewDecoder` 是唯一
  活代码违规，改为 `common.NewJsonDecoderUseNumber`。该函数只比较 `string`
  与 `json.Delim`，数字走默认分支，`UseNumber` 不改变行为；写法与仓库其余
  6 处一致。

复核后全仓业务代码（排除 common/pkg/tools 与测试）已无直接
`json.Marshal` / `Unmarshal` / `NewDecoder` / `NewEncoder` 调用，仅剩注释
中的文字引用。

### 14.5 本轮验证汇总

- 前端：`i18nDefaultCoverageGate` 0；`bun run i18n:sync` 六语言
  missing/extras/untranslated 全 0；`typecheck` 0；`format:check` 0；
  `eslint` 0 error（22 warning 既有）；`bun test` 293 通过；`build` 0。
- 后端：`go build ./...` 通过；`go vet ./dto ./relay/channel/claude`
  通过；`go test ./dto ./relay/channel/claude ./relay/channel/task/...`
  全部 ok。
- workflow YAML 解析通过。




## 十五、i18n 译文完成度与占位符完整性（2026-11-08）

### 15.1 发现：既有门禁不检查译文值

仓库已有四道 i18n 门禁，全部只覆盖"键"而不覆盖"值"：

- `web/scripts/i18nCoverageGate.mjs`（classic）：源码用到的键是否在
  各语言文件中存在。
- `web/scripts/i18nDefaultCoverageGate.mjs`（web/default）：sources 里的
  `t('...')` 字面量是否在 6 个语言文件中存在，`static-keys.ts` 是否登记。
- `web/scripts/i18nLintGate.mjs`（classic）：源码硬编码字符串（基线 62 条）。
- `bun run i18n:sync`：对本仓库只做"键对齐"，报告 missing/extras。

没有任何一道检查"值是否仍是英文原文"。因此语言文件可以长期保留大量
英文文案而 CI 全绿。

### 15.2 量化（2026-11-08 实测）

以 `en.json` 为基准，按"值等于基准 + 去掉 {{占位符}} 后至少 3 个拉丁词
+ 至少一个小写开头词 + 非 URL/邮箱/JSON/snake_case/单 token + 不含
汉字假名"判定：

| 前端 | 语言 | 未翻译整句 |
| --- | --- | --- |
| classic | fr | 180 |
| classic | ru | 179 |
| classic | ja | 216 |
| classic | vi | 211 |
| classic | zh-CN | 4 |
| classic | zh-TW | 1 |
| default | fr | 91 |
| default | ja | 0 |
| default | vi | 91 |

### 15.3 web/default 的 sync 报告为何显示 0

`web/default/scripts/sync-i18n.mjs` 的 `untranslated` 判定对 fr/vi
走的是"句子含 `the|and|or|to|with|please` 才怀疑未翻译"的启发式。大量
未翻译文案（"My Account"、"Redeem Code"、"API Key"、"Search"）不含这些
词，因此永远不被计入。上一轮"untranslated=0"的结论只能证明启发式没命中，
不能证明译文完整；本轮改为与 en 逐值相等比对后实测 fr/vi 各 91 条。

### 15.4 修复

- 新增只读门禁 `web/scripts/i18nTranslationCompletenessGate.mjs`，
  同时覆盖两个前端：
  - 未翻译整句检测（口径见 15.2）；
  - 占位符完整性：译文 `{{...}}` 集合必须与基准一致（顺序无关），
    并检出 `__ PH_n __` 流水线残留。
  - 需刻意保留英文的条目登记在
    `web/tests/i18nUntranslatedAllowlist.json`，按 `前端:语言` 分组。
- 接入 `.github/workflows/repo-gates.yml` 的 `i18n-gate` job；
  两个前端 `package.json` 增加 `i18n:complete`。
- 补齐译文：classic fr 180 / ru 179 / ja 216 / vi 211 / zh-CN 4 /
  zh-TW 1，default fr 91 / vi 91。
- 修复既有占位符损坏 5 条：
  - `default/ja` 三条被流水线写成 `__ PH_n __`（`Failed to parse JSON
    file: {{name}}`、`Model pull failed: {{msg}}`、`Parsed {{count}}
    service account file(s)`），一条把 `&lt;` 实体丢失
    （`New API <noreply@example.com>`）。
  - `classic/fr` 两条 `_one` 复数条目丢失 `{{count}}`
    （删除所选令牌 / 模型的单数确认文案）。

### 15.5 验证

- `node web/scripts/i18nTranslationCompletenessGate.mjs` 退出码 0
  （允许清单 3 条，均为 "OpenAI, Anthropic, Google, etc." 这类厂商名
  枚举，翻译后反而难以辨认）。
- 反向验证（门禁有效性）：
  - 把 `classic/ja` 一条译文改回英文原文，退出码 1，点名键
    `价格计算失败`；
  - 删除 `classic/ja` 一条译文的 `{{tokens}}` / `{{symbol}}`，
    退出码 1，点名键并打印期望占位符；
  - 两处恢复后退出码 0。
- `bun run i18n:sync`（web/default）六语言 missing/extras/untranslated
  全 0，未改写任何语言文件。
- classic 覆盖率门禁通过（7 个语言文件均包含代码用到的全部键）。
- web/default 覆盖率门禁通过（3793 个字面量键 × 6 语言，550 静态键
  均已登记）。
- web/default：`format:check` 0；`eslint` 0 error（22 warning 既有）；
  `typecheck` 0；`bun test` 293 通过；`build` 0。
- classic：`prettier` 0；`eslint` 0 error（5 warning 既有）；
  `bun test` 222 通过；`test:node` 164 通过；`build` 0。
- `repo-gates.yml` YAML 解析通过。

边界：`repo-gates.yml` 仅本地存在，从未在 GitHub Actions 上真实运行；
上述验证均为本地执行同一命令得到的结果。译文为机器生成基础上的人工
校对，未逐条做母语者审校；语言覆盖为 fr/ru/ja/vi/zh-CN/zh-TW 六种。

## 十六、翻译完成度口径收紧与短标签补齐（2026-11-08）

### 16.1 上一版门禁的盲区

第十五节的门禁只统计"去掉占位符后至少 3 个拉丁词、且至少一个词小写
开头"的值。复核发现这放过了两类用户可见文案：

- 两个词以内的标签：`All groups`、`Gift Codes`、`Download Logs`；
- 全 Title Case 的标签：`Header Profile Applied`、`Change Deployment Name`、
  `Total Available Hardware Resources`。

实测（值与该语言 en.json 相同、含拉丁字母、排除 URL/JSON/标识符）：
classic fr 188 / ja 222 / ru 179 / vi 221 / zh-CN 39 / zh-TW 38；
default fr 120 / vi 108 / zh 17 / ru 9 / ja 7。抽样逐条确认了源码引用，
`Pay Method Name` 等条目在中文界面同样显示英文。

`web/default/scripts/sync-i18n.mjs` 的 `untranslated` 判定对 fr/vi 只匹配
`the|and|or|to|with|please`，同一批条目也不会出现在 sync 报告里，两套
工具都显示 0。

### 16.2 门禁口径

`web/scripts/i18nTranslationCompletenessGate.mjs` 改为：

- 值等于基准值、去掉 `{{占位符}}` 后仍含拉丁字母，且不是 URL、邮箱、
  JSON 片段、snake_case、单 token 标识符，即判为未翻译；
- 占位符集合与基准不一致、或出现 `__ PH_n __` 残留，仍然判失败；
- 豁免清单改为按基准值匹配，分两层：
  - `shared`：两个前端共用，79 条，品牌/产品名（OpenAI、Claude、Baidu、
    Telegram…）、协议字段（Client ID、Base URL、Token Endpoint…）、
    时区标识（UTC+8 Singapore…）；
  - `前端:语言`：只对特定语言豁免，16 条。法语中 `1 minute`、
    `10 / page`、`EUR (Euro)`、`Top {{count}}` 与英文同形且是正确
    法语，登记在 `classic:fr` / `default:fr`。

边界：单 token 值不计入。`Create`、`Start` 这类单词标签与 `gpt-4o`、
`bps`、`GET`、`true` 等品牌/型号/枚举无法静态区分，要覆盖需要一份
极大的豁免清单，本轮明确不做。

### 16.3 补齐

按新口径补齐 842 条：

| 前端 | 语言 | 条数 |
| --- | --- | --- |
| classic | fr | 149 |
| classic | ja | 174 |
| classic | ru | 155 |
| classic | vi | 166 |
| classic | zh-CN | 3 |
| classic | zh-TW | 3 |
| default | fr | 96 |
| default | vi | 95 |
| default | zh | 1 |

译文保留全部 `{{...}}` 占位符；写回前逐条校验键集合、占位符集合、
非空且不等于英文原文。中文条目按源串回填（`Pay Method Name` 等）。

### 16.4 验证

- `node web/scripts/i18nTranslationCompletenessGate.mjs` 退出码 0
  （允许清单 95 条）。
- 反向验证：
  - 把 `classic/ja` 两条译文改回英文，退出码 1，点名
    `下载日志 -> Download Logs` 与 `价格计算中... -> Calculating price...`；
  - 删除 `classic/fr` 一条译文的 `{{ratio}}`，退出码 1，打印期望占位符；
  - 两处恢复后退出码 0。
- `bun run i18n:sync`（web/default）六语言 missing/extras/untranslated
  全 0，未改写语言文件。
- 两个前端的覆盖率门禁 0；classic i18n lint 62/62 基线，新增 0。
- web/default：`format:check` 0；`eslint` 0 error（22 warning 既有）；
  `typecheck` 0；`bun test` 293 通过；`build` 0。
- classic：`prettier` 0；`eslint` 0 error（5 warning 既有）；
  `bun test` 222 通过；`test:node` 164 通过；`build` 0。

边界：译文为机器生成后的人工校对，未做母语者审校；单 token 值仍
可能残留英文，见 16.2；`sync-i18n.mjs` 的 fr/vi 启发式未改动，
其报告仍只作键对齐参考。

## 十七、隔离开发环境同步到 HEAD（2026-11-08）

排查时发现两个容器都落后于 HEAD：

- 隔离开发容器 `new-api-dev-isolated-new-api-1` 构建于 `3491864`
  （2026-11-04），落后 HEAD 88 个提交，其中 53 个是 Go 提交
  （会话吊销、订阅预扣、渠道状态锁等修复都不在其中）。
- 生产容器 `new-api` 仍是 `4527d7666`（2026-09-08），落后 817 个提交，
  且是 HEAD 的祖先。发布时间另定，本轮不动。

开发环境按 AGENTS.md 重建：

1. `scripts/build-docker-local.sh new-api-local:dev` 第一次在
   `go mod download` 阶段超时（`dial tcp 198.18.2.3:443: i/o timeout`）。
   客户端环境变量 `HTTPS_PROXY` 不会自动进入 BuildKit，必须显式传
   `--build-arg HTTP_PROXY/HTTPS_PROXY=http://host.docker.internal:7890`。
2. 带 build-arg 重试成功，镜像标签 `new-api-local:dev`
   （`sha256:7b9ad6e9150c`），VCS_REF 为 `9658d5ad5-dirty`——工作区有
   另一个会话未提交的 `EditChannelModal.jsx` 与 `setting/sensitive_test.go`，
   镜像因此带 dirty 标记，Go 二进制内容不受这两个文件影响。
3. `docker compose -f deploy/compose/dev-isolated.yml --env-file
   deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api`
   重建容器。

验证：

- 容器 `healthy`；`/new-api --build-info` 输出
  `commit=9658d5ad5930ef3504f62f257a6b8a4027d28a17-dirty`，与 HEAD
  `9658d5ad5` 一致。
- `curl -fsS http://127.0.0.1:3001/api/status` 返回 200 与正常 JSON。
- 旧镜像已被替换清除；`docker system df` 的 Build Cache 为 0，无需
  额外清理；本项目的容器、卷、网络均在使用中，无残留。

边界：生产尚未更新；dev 镜像带 dirty 标记，若要得到严格等于 HEAD 的
镜像，需要等并行会话提交或清理后进行干净构建。

## 十八、结构化样例守护与 ja 样例修复（2026-11-08）

### 18.1 发现

`default/ja` 有两条可直接复制进表单的 JSON 样例被翻译流水线改写：

- `"default": "us-central1", "claude-3-5-sonnet-20240620": "europe-west1"`
  → `"default ":" us - central 1 ", "claude -3-5 - sonnet -20240620 ": " europe - west 1 "`
- `{"original-model": "replacement-model"}`
  → `{" original - model ":" replacement - model "}`

两条键都在源码中真实使用（`channel-mutate-drawer.tsx` 的 region 输入、
`model-mapping-editor.tsx` 的 placeholder）。用户照抄示例会得到非法 JSON。
追溯为 2026-04-28 的 `a42b39760`（v1.0 前端首发）引入，非本轮产生。

同时发现豁免清单里的 `"ID:"` 已失效：classic 中实际值是 `"ID: "`（尾随空格），
该条目不再匹配任何基准值。

### 18.2 修复

- 新增 `web/tests/i18nStructuredValues.test.mjs`：
  - JSON / 数组 / `"key": "value"` 形式的样例值必须与基准结构等价：
    键集合、数组长度、叶子类型一致；允许样例内部的显示名本地化
    （zh 把示例里的 `Alipay` 保留为 `支付宝`），但破坏语法或改键名即失败。
  - 豁免清单的每条必须仍能匹配某个 en.json 的值，失效条目即失败。
- 恢复 ja 的两条样例为基准原文（这两条本就是 JSON，不需要翻译）。
- 删除失效的 `"ID:"` 豁免项。
- 该测试随 `web/classic` 的 `test:node` 进入 CI。

### 18.3 验证

- 修复前测试 2 条中 1 条失败（捕获 ja 两条样例）；修复后 2/2 通过。
- 反向验证：把 `default/ru` 的一条样例改坏，测试变为 1 通过 1 失败；
  恢复后再次 2/2 通过。
- `node --test web/tests/i18nStructuredValues.test.mjs` 2/2；
  `bun run test:node`（web/classic）166/166；
  `node web/scripts/i18nTranslationCompletenessGate.mjs` 退出码 0；
  `bun run i18n:sync` 六语言 missing/extras/untranslated 全 0。
- 所有 default 语言文件保留 sync 脚本的混淆键转义
  （`footer.newapi.projectAttributionSuffix`），逐文件核对通过。

### 18.4 边界

- `en.json` 中还有约 162 个键在源码中无直接字符串引用（多为 `_one`/`_other`
  复数变体与动态拼接键），无法用静态扫描区分死键与动态键，未清理也未设门禁。
- `docs/reviews/archive/CR-MAIN-PAYMENT-SECURITY-GAP-2026-04-19.md` 中 17 个
  相对链接（`../../controller/...`）解析到 `docs/controller`，属归档文档的
  既有断链，未改动历史记录。

## 十九、技术标识符守护与 ja 型号修复（2026-11-09）

### 19.1 发现

`default/ja` 有三处型号/版本号被翻译流水线插入空格：

| 基准 | ja |
| --- | --- |
| `Baidu V2` | `Baidu V 2` |
| `Zhipu V4` | `Zhipu V 4` |
| `e.g., OpenAI GPT-4 Production` | `例： OpenAI GPT -4 Production` |

三者都是品牌/型号名，不应翻译。完成度门禁只因"值已不等于基准"而放行；
第十八节的结构化样例测试只覆盖 JSON/数组形式，也不覆盖这类值。

### 19.2 修复与门禁

- `web/tests/i18nStructuredValues.test.mjs` 增加"技术标识符保留"检查：
  - token 规则：至少一段用 `-`/`_`/`.` 连接的字母数字组合（`GPT-4`、
    `gpt-4o`、`us-central1`），或字母后接数字的单元（`V2`、`V4`）；
  - 只检查正文，先剔除 URL 与路径（本地化时改写示例 URL 是刻意的）；
  - 排除 `non-2xx` 这类英文惯用语与 `xx` 结尾的占位。
- 修复 ja 三条。

### 19.3 验证

- 修复前测试 3 条中 1 条失败，精确点名上述 3 处，无其它误报；
  修复后 3/3 通过。
- 反向验证：把 `default/ru` 的 `GPT-4` 改成 `GPT -4`，测试变为
  2 通过 1 失败；恢复后 3/3。
- `bun run test:node`（web/classic）167/167；
  完成度门禁退出码 0；`i18n:sync` 六语言全 0；
  default 六个语言文件的混淆键转义完好。

### 19.4 边界

扫描时另见两处属可接受差异，未改：`non-2xx` 各语言译成自然短语
（非标识符）、classic ja 将 `socks5` 写作 `SOCKS5`（协议名大小写
不敏感，检查器按大小写不敏感比较）。单 token 标签与豁免清单中
允许翻译的品牌（fr 的 `API Key` -> `Clé API`）不在本检查范围。

## 二十、完成度门禁的两条漏判与补齐（2026-11-09）

### 20.1 发现

继续排查单 token 时发现两类**完整英文句子**一直没被门禁报出：

| 文案 | 被放行的原因 |
| --- | --- |
| `{{count}} logs deleted` | 原规则 `^[{[":]` 把以 `{{` 开头的句子当成 JSON 片段 |
| `{{count}} API entries deleted. Click "Save Settings" to apply.` | 同上 |
| `Remove image_generation tool objects rejected by an upstream.` | 原规则 `[a-z]+_[a-z]+` 命中标识符就整条跳过 |

后一条在 classic 的 fr/ja/ru/vi 四个语言里都是英文原文；前一类在
default 的五个非英文语言里共 40 条（`{{count}} logs deleted`、
`{{count}} templates, used in order`、删除确认等）。用户在界面上会
直接看到英文句子。

单 token 排查时还发现 `default/ja` 的 `AccessKey / SecretAccessKey`
被写成 `AccessKey/SecretAccessKey`（丢了空格），属同类流水线损伤。

### 20.2 门禁修改

`looksUntranslated` 改为：

1. 先剥掉 `{{占位符}}`，再对剩余文本判定，避免"以 `{{` 开头"被误判为 JSON；
2. 在剩余文本中剥离 snake_case 标识符、路径、URL、邮箱，再判断剩下的是否
   还有至少两个英文词；
3. 整体是单个标识符（`gpt-3.5-turbo`、`checkout.session.completed`、
   `AIGC2D`）或这类标识符的 `a / b` 组合时，不算未翻译；
4. 只有多词剩余才报，单词剩余（`s`、`min`、`bps`、`GET`）视为单位/状态/品牌。

豁免清单新增两条纯键名映射说明
（`header:session_id -> json:prompt_cache_key` 及其反向）。

### 20.3 验证

- 修改前门禁在 default 上报告 8-10 条/语言、classic 1-2 条/语言；
  第一版收紧过火（把 `{{count}} min`、`bps` 也报出来），调整"单词不报"
  后只剩真实问题。
- 补齐 40 条后门禁退出码 0（允许清单 96 条）。
- 反向验证：把 `default/vi` 一条恢复成英文，门禁退出码 1 并点名；
  恢复后退出码 0。
- `bun run test:node` 167/167；classic prettier 0；default prettier 0；
  两个覆盖率门禁 0；`i18n:sync` 六语言全 0；default 六个语言文件的
  混淆键转义逐文件核对完好。

### 20.4 过程记录

写回时先用 `JSON.stringify` 整体序列化，把 sync 脚本刻意混淆的
`footer.newapi...` 键还原成了明文（与第十七、十八节同类事故）。
已从 HEAD 恢复全部语言文件，改为逐行值替换重做，diff 只包含预期改动。

### 20.5 边界

单 token 标签仍不覆盖（`Create`、`Start`、`Processing` 与 `gpt-4o`、
`bps`、`true` 无法静态区分）；本轮只保证"多词英文句子"零残留。

---

## 二十一、门禁漏判之二：改写过但仍为英文的句子（2026-11-09）

### 21.1 发现

第二十节补完后，门禁只判"值等于基准"。抽查 classic 的 fr/ru/vi 发现
另一类残留：译文值与 `en.json` 不同，但仍是英文句子，例如

- `"When token group is set to auto, groups are selected in order of priority"`
  （基准为 `"When the token group is set to auto, the system selects
  available groups in list order; earlier entries have higher priority."`）；
- `"Without rules, all users see the same dropdown:"`
  （基准为 `"When no rules are configured, all users see the same dropdown:"`）；
- `"Confirm delete this group?"`（基准为 `"Delete this group?"`）。

这是翻译流水线"顺手改写英文"的产物：删冠词、换代词、微调大小写。
因为值不等于基准，完成度门禁完全看不见。

### 21.2 判定口径的三次迭代

1. 只看英文功能词占比（≥0.35）：实测在 fr 上产生大量误报，因为法语
   也含大量拉丁字母，且 `Create and Manage Groups` 之类标签同样命中。
2. 改为"功能词占比 ≥0.25 且与基准词重合 ≥0.55"，并先排除含非 ASCII
   字母的值。全量扫描 66 条命中，逐条核对**全部是真实英文残留**，
   零误报。
3. 增加下限（至少 6 个英文词、至少 2 个功能词命中），排除
   `ex. : /var/cache/new-api` 这类只剩路径加前缀的短片段，以及
   俄文真译文中夹带 `enabled=true`、`ttl_hours=0` 的情况。

产物为 `looksLikeEnglishSentenceBody(value, baseValue)`：只检查
`value !== baseValue` 的条目，豁免清单按基准值匹配。

### 21.3 补齐范围

- 第一轮：classic fr/ru/vi 各 21 条、classic ja 1 条、default ja 1 条、
  default vi 1 条，共 67 条改写英文句子补为对应语言译文。
- 收紧后新暴露：classic fr/ru/vi 各 11 条、default vi 1 条，共 34 条，
  同样补齐。
- 两轮合计 101 条。**更正**：提交 `9f1f82a52` 的提交信息与本文初版
  此处都写作"101 条"，但按逐键 diff 复核，该提交实际改动 **120 条**：
  classic fr/ru/vi 各 37 条（33 条改写英文句子 + 4 条 Title Case 短
  标签）、classic ja 1 条、default ja 1 条、default vi 7 条。差额来自
  classic 三语中另有 18 条无功能词、但长度 ≥6 词且全 Title Case 的
  短标签（`Create and Manage Groups`、`Confirm delete this group?` 等），
  同样是改写英文，被新规则一并捕获。翻译内容正确，门禁零误报，只有
  计数口径写错。
- 所有写回都按原始文本逐行替换，未用 `JSON.stringify`
  整体序列化；`web/default` 六个语言文件的混淆键
  `footer.newapi.projectAttributionSuffix` 逐文件核对仍为转义形式。

### 21.4 验证（本轮实跑）

- `cd web && node scripts/i18nTranslationCompletenessGate.mjs`
  退出码 0，输出"允许清单 96 条"。
- 反向验证：将 `classic/fr` 的 `暂无分组，点击下方按钮添加` 从
  `"Aucun groupe pour le moment..."` 改为
  `"No groups at the moment. Click the button below to add one."`，
  门禁退出码 1 并准确点名该键；恢复后退出码 0。
  （第一次用 `There are no groups at all right now in this view.` 做变异体
  未被拦截：它与基准词重合率为 0，本就不是"改写英文"形态。改用具
  高词重合的变异体后验证通过。）
- `node --test tests/i18nStructuredValues.test.mjs`：3 通过 / 0 失败。
- `web/default` 六个语言文件的 canary 转义键逐文件核对为 `true`；
  classic 的 fr/ru/vi/ja 在 HEAD 中本来就没有该键，未作比较基准。
- 阴性样本：新规则在全部 10 个语言文件、**55332 条**"值不等于基准"
  的现有译文上无命中，即零误报（首轮在 66 条候选上报命中并逐条核对，
  第二轮在补齐后的全量上复核，两次均零误报）。

### 21.5 边界

该规则针对"英文原句被小幅改写"这一类残留，不覆盖：

- 用完全不同的英文措辞重写（词重合率低）；
- 少于 6 个英文词的短标签；
- 单 token 值（第二十节已说明）。

这三类仍需人工抽查或扩充豁免清单，本轮未扩大口径以免误伤真译文。

---

## 二十二、本轮收尾清理（2026-11-09）

### 22.1 提交计数更正

见 §21.3：`9f1f82a52` 的提交信息写作 101 条，逐键 diff 复核为 120 条。
提交历史不改写，以本文为准。验证方式：对每个 locale 文件分别 `git show
HEAD~1:<file>` 与 `git show <commit>:<file>` 逐键比较新旧值，统计
`before[key] !== after[key]` 的键数，合计 120。

### 22.2 classic 孤儿键清理

对 fr/ja/ru/vi/zh-TW 以 `en.json ∪ zh-CN.json` 为基准、按 i18next 复数
规则剥离 `_one/_few/_many/_other` 后缀后，非复数孤儿键共 171 个。逐键
在 `web/classic/src` 的 490 个源码文件里做一次全量字符串包含检查
（一次读入 4.5 MB 语料，含单双引号两种形式），并核对
`web/classic/src/constants/i18nDynamicKeys.js` 的运行时动态键登记表：

- 删除 9 个确认死键（fr 2、ja 2、ru 2、zh-TW 2、vi 1）：
  - `Responses 流启动恢复仅作用于 ...`（旧短版本，键已改长为含
    "只有已在渠道设置中启用该功能的渠道才会参与保护窗口。"的版本，
    长版本在全部 7 个语言文件里都有译文）；
  - `不勾选用户可选`（`GroupRatioSettings.jsx:617` 实际使用的是
    `假设再加两个分组 default 和 vip，但不勾选用户可选：`）；
  - `/api/option/`（源码里是 `API.get('/api/option/')` 的路径常量，
    从未作为翻译键使用）。
- 保留 vi 的 163 个孤儿键（`模型列表`、`正在使用`、`清理` 等）：均为
  2026-04-28 `a42b39760` 引入，在 en/zh-CN/fr/ru/ja 中均不存在，源码
  与 `i18nDynamicKeys.js` 都零引用。**本轮不删除**：无法证明它们将来
  不会被重新启用，且删除属于数据裁剪而非翻译正确性问题，留待单独决策。

验证：`node --check` 各 JSON 通过，classic prettier `--check` 通过。

### 22.3 测试质量修正

`web/tests/i18nStructuredValues.test.mjs`：

- 第 90 行 `typeof baseValue !== "string" || typeof baseValue !== "string"`
  重复判断同一变量，改为单个判断。
- 测试名 `structured i18n values stay byte-identical to the base locale`
  与实现不符（实现是结构比较，允许显示名本地化），改为
  `structured i18n values keep the same JSON shape as the base locale`。

### 22.4 其它核对（未改动）

- `web/default/src/i18n/locales/_reports/_sync-report.json` 是 `bun run
  i18n:sync` 的生成产物，被 `.agents/skills/i18n-translate/SKILL.md`
  的工作流读取。**不加入 .gitignore**：skill 依赖它在仓库中的位置，
  且该文件当前六语言全部 `missing=0 extras=0 untranslated=0`，无污染。
- `web/default/scripts/sync-i18n.mjs` 第 160-179 行的
  `isLikelyUntranslated` 对 fr/vi 使用 `the|and|or|to|with|please`
  弱启发式，会漏掉纯 Title Case 的改写英文。这是 sync 脚本辅助报告的
  局限，不是翻译正确性问题；完成度门禁已覆盖这一类，本轮不改。
- 三处为源码 `t()` 字面量但 en/zh-CN 均缺失的键
  （`Responses 流...首包前阶段...`、`不勾选用户可选`、
  `/api/option/`）**不是基准缺失**：它们本身就是历史死键，上一节已删除。

### 22.5 CI 接线核对

`.github/workflows/repo-gates.yml` 已包含：classic 覆盖度、classic
lint、完成度门禁（两前端）、default 覆盖度；`web/tests/*.test.mjs`
（含 `i18nStructuredValues.test.mjs`）由 `bun run test:node` 执行，该命令
在 `frontend-checks` 中接线。本轮无需改动。
