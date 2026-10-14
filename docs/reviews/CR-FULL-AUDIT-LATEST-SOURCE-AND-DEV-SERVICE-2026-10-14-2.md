# 全场景全维度审查：本会话增量 + 生产升级全景（2026-10-14 第二轮）

采样时间：2026-10-14 03:18-05:00 UTC。分两阶段：第一阶段审本会话增量（基线 52972c7cf 至 HEAD 84894f76b，9 提交），第二阶段按用户要求扩至「自正式服务版本以来」——生产容器实际运行 4527d7666（litefix2，2026-09-08 构建），dev 领先 368 提交/1106 文件/约 18.5 万行。共 7 路并行只读审查代理，关键发现经主代理独立复核。

## A. 第一阶段：本会话增量（9 提交）审查结果

### 已确认缺陷（主代理已复核）

**A.1 P1 工具名还原为死代码（已修复，提交 bfe4156cb）**
d84d2acb0 恢复了 restorer 的消费侧但生产侧缺失：NormalizeResponsesToolNamesWithRestorer 无任何非测试调用者，RelayInfo.ResponsesToolNameRestorer 无赋值点，还原路径恒 nil 空转。历史调用点（responses_handler.go :1123 附近）在事故中丢失。主代理复核 grep 成立后修复：在 ResponsesHelper 请求 DeepCopy 后接入归一化，restorer 挂到 RelayInfo；新增走真实入口的测试（注入式测试无法发现此类断裂），反向验证移除赋值行测试变红。教训沉淀至记忆 restore-commits-can-miss-the-producer-side。

**A.2 P2 转换路径旁路（记录，未修）**
responses_via_chat.go 与 chat_via_responses.go 有独立发送通道绕过 sendResponsesStreamData。当前因 A.1 修复前 restorer 恒 nil 而 moot；A.1 修复后，经 chat 兼容路径（responses_via_chat）的别名 echo 不经还原。属前瞻性风险：这两条路径需在工具名归一化全量接线时一并处理。

**A.3 P2 OAuth state 失败路径半开窗口（记录）**
4 个 bind 点与登录路径仅在成功时消费 state；失败路径 nonce 留存。危害有限（重放重复同样语义，无提权），记录为已知剩余窗口。

**A.4 P3 测试伪覆盖（记录）**
TestLogRetentionDaysParsing 把解析逻辑复制到测试内断言，未覆盖产品 var。功能无害，如实记录。

### 通过项
- realtime addRealtimeUsage：字段与 tiered_settle 读取一一对应、无重复累加、nil 防护完备。
- 流式还原时序：prelude flush、remoteState.events、synthetic compact 全部经 sendResponsesStreamData。
- OAuth 收口副作用面：密码/Passkey/微信/Telegram 路径空操作确认；custom_oauth 无双 Set-Cookie；2FA 挑战后重放被校验点拒绝；CAS 时序未动。
- cleanupOldLogs 并发安全：setupLogWorking CAS 保护重入、currentLogPath 有 RLock、livePath+mtime 双守卫。
- 前端置顶/性能列接线逐点验证闭合；i18n 无遗漏。
- CI workflow 在干净 clone 模拟中三门禁全过、bun 依赖无漂移、SHA 钉点一致。
- 部署环境：env 事故恢复合格（六项凭证非空且长度与容器一致、权限 600）、数据零丢失（10/22/17）、日志保留语义正确（cutoff 边界文件按代码正确保留）。

### CI 伪通过面（P1 定性为 fail-open-by-design）
i18n lint gate 只拦新增不拦基线内 230 条既有问题；基线文件随 HEAD 提交，篡改基线的 PR gate 不自拦。缓解：基线 diff 在 PR 中可见，需人工核对；已解决 77 条需定期 --write-baseline 收缩。

## B. 第二阶段：生产 4527d7666 → dev 84894f76b（368 提交）升级全景

### B.1 生产当前存在而 dev 已修复的安全问题（升级紧迫度依据）

主代理独立复核以下全部成立：

| 问题 | 生产证据 | dev 修复 | 紧迫度 |
| --- | --- | --- | --- |
| OAuth state 不消费，会话存活期（30 天）可重放回调 | 4527d7666 github.go:84 等只比较不删除 | consumeOAuthState 收口 + 各 bind 点 | 高（中严重度，可利用） |
| 2FA 门控查询错误当「未启用」放行 | user.go:68 用 IsTwoFAEnabled（错误返回 false） | GetTwoFAByUserId 错误即拒绝 | 中（fail-open，条件可利用） |
| 会话 Cookie 硬编码 Secure:false | main.go:192 | SecureSessionCookie 按 TLS/XFP 标记 | 中 |
| gin 信任所有代理，XFF 可伪造 IP 绕过限流/封禁/审计 | 无 SetTrustedProxies | TRUSTED_PROXIES（默认零信任，见 B.3-4） | 中高 |
| panic 响应泄露内部错误原文 | main.go 旧 182-190 段 | 只回 request_id | 低 |
| 无 ReadHeaderTimeout（slowloris）、匿名端点 128MB body | main.go 无该配置 | 30s/120s 超时 + 匿名 512KB 上限 | 中（DoS 面） |

### B.2 升级后计费金额变化（全部经 diff 验证）

1. realtime 双重扣费修复：生产预扣+逐 turn 直扣+终态结算约 2x，dev 改 Billing.Reserve 补差额——ratio 计费 realtime 模型大幅少收（修复多收）。
2. 大小写/别名归一化（model_name_normalizer.go 新增 812 行）：生产命中 37.5 兜底的大小写变体（MiMo-V2.6-Flash、DeepSeek-V4.1-Flash 等）升级后命中真实价格——多收变准确（少收）。
3. 字面量下调：gpt-5.6-terra 1.25→1.0、gpt-5.6-luna 0.5→0.1、DeepSeek-R1 0.8→0.25、sora-2 0.3→0.1 等；大量新增官方价（claude-opus-5、gemini-3.x、kimi-k3 等）替代 37.5 兜底。
4. GetCompletionRatio 优先级反转：非锁定硬编码让位于用户 completionRatioMap——配置过自定义值的站点计费改变。
5. tiered 默认表达式内置约 50 个 + upgradeBillingExpr 自动迁移：DeepSeek V4 Flash 系数下调（少收）、Grok 4.5 缓存读降价与 200000 边界变化（少收）、MiniMax M3 新增 priority 档 1.5x（多收使用方）。
6. realtime tiered 子类别修正（ImageTokens/Audio 进表达式）：image token 按 img 价计（多收，修正为准确）。
7. billingexpr fail-closed：歧义大小写 header 拒绝而非静默取随机值；敏感 header（authorization/cookie）对表达式不可见。依赖这些的自建表达式金额会变。

### B.3 升级部署操作项（人工干预清单）

1. **TRUSTED_PROXIES 必须设置**（最重要）：新代码默认零信任（SetTrustedProxies(nil)），反代/NAT 后不设置则全体客户端 IP 显示为代理地址、按 IP 限流全体共享一个桶。反代后部署设 `TRUSTED_PROXIES=auto` 或显式 CIDR。
2. **连接池默认骤降**：SQL_MAX_OPEN_CONNS 1000→50、IDLE 100→25。高并发生产需显式设回。
3. **logs 表 21 个旧索引启动时自动删除**：PG 走 CONCURRENTLY（无感）；MySQL/SQLite 持表锁——MySQL 大表选低峰升级或预先手工删。marker `migration.log_index_cleanup.20260921` 保证一次。
4. **compose 硬校验**：SESSION_SECRET/CRYPTO_SECRET/SQL_DSN/REDIS_CONN_STRING 变 `${VAR:?}`，缺变量的旧 .env 会在启动前报错，需先补齐。
5. **ANONYMOUS_REQUEST_BODY_LIMIT_KB 默认 512KB**：超大支付回调会被 413（可设 0 关闭）。
6. **graceful shutdown 默认 120s**：docker stop 停机时间变长；编排层宽限期小于 120s 会被 SIGKILL 截断（与旧行为一致）。
7. options 表存量值继续覆盖内置默认（options 优先于代码默认）——B.2 的计费变化仅在对应键未被 DB 覆盖时生效，升级后核对 /api/pricing 与计费日志。
8. AutoMigrate 侧全部安全：无删列、无改类型、无改名 options 键；Channel +2 列、Ability +4 列、Task/MJ +private_data 均为加列；渠道 pin 一次性迁移有 marker 保护。

### B.4 368 提交未扩大的攻击面（确认项）

新增端点全部有鉴权（AdminAuth/channel 组内）；CORS 无变化（AllowAllOrigins 生产已有）；SSRF guard 生产已有；distributor 校验均为收紧（大小写不敏感 auto group、固定渠道严格拒绝、空 allowlist 统一拒绝）；日志脱敏仅扩大覆盖；SecurityHeaders 为新增收紧；SPA 404 收严修复路径探测。遗留而非新引入：未认证 /api/user/groups 分组倍率暴露（生产 :83 已存在）。

## C. 已提交代码测试事实

- `go build ./...` 退出码 0。
- 计费 agent 曾误用一次 git stash --include-untracked（违反只读红线），同序列内 pop 完整恢复；主代理核证工作区 134 项（1 修改 + 133 untracked）与快照一致、stash 仍为其他会话旧存根 2 个，无损失。
- 已提交代码在移除 untracked 测试后 `go test ./model ./service ./pkg/billingexpr` 全 ok；工作区内失败全部溯源到 untracked 在途测试。

## D. 未覆盖边界

- 真实 GitHub runner 环境的 CI 首跑（需推送后观察）。
- 生产库 options 实际覆盖面（决定 B.2 计费变化的实际生效范围）——需在生产库只读查询确认。
- MySQL 大表 DDL 锁时长、TieredIndex 删除耗时为推断。
- responses_via_chat/chat_via_responses 的还原旁路（A.2）待全量接线时处理。

## E. 结论

- 本会话 9 提交中 8 个经审查成立，1 个（d84d2acb0）为半成品接线（P1，已修复闭环）。
- 升级紧迫度由 B.1 前四项驱动：生产当前可被利用的 4 项安全问题 dev 均已修。升级前必须完成 B.3 清单（尤其 TRUSTED_PROXIES）。
- 计费变化以「修复多收」为主导方向，个别项（MiniMax priority、realtime image 子类别）方向为多收（修正为准确）。
