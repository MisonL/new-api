# 最新源码与已部署隔离开发服务的全维度审查

日期：2026-10-10
范围：分支 `dev` 源码（含在途改动）与 `dev-isolated` 隔离开发服务
方法：多代理编排（6 维度取证 → 逐条对抗复核 → 完备性批判，共 37 个子代理）+ 主代理逐条独立复现

## 1. 审查对象与范围

- 源码：分支 `dev`。快照时 HEAD `52d63c24e`；审查期间并行会话继续提交，复核时 HEAD 已为 `44776b270`（新增提交均为 `docs/`，未触及本轮结论所涉代码）。领先 `origin/dev` 256 提交、领先 `origin/main` 374 提交。
- 工作区：快照时 366 个已修改文件 + 131 个未跟踪文件，约 2 万行新增。**已部署镜像正是从这份 dirty 工作区构建的**，因此在途改动就是运行中的代码。
- 服务：容器 `new-api-dev-isolated-new-api-1`，镜像 `new-api-local:dev`，`--build-info` 为 `commit=52d63c24e35512cb952aaba9cb693faee228daf6-dirty`、`date=2026-10-10T01:04:48Z`，仅监听 `127.0.0.1:3001`。同 compose project 有 `new-api-dev-isolated-postgres-1`（5433，PostgreSQL 15.17）与 `new-api-dev-isolated-redis-1`（6381）。
- 红线：全程只读。未改仓库文件、未 `git add/commit/checkout/stash`、未对数据库或 Redis 写、未重启/停止/删除容器。生产容器 `new-api`（13000）与无项目前缀的 `postgres`/`redis` 未触碰。子代理临时探针只写 `/tmp`，用 `go test -overlay` 映射，仓库零残留。
- 基线：上一轮同类审查 `docs/reviews/CR-DEV-SERVICE-AUDIT-2026-10-08.md`（基线 `fa211e334`）。本轮 HEAD 距其 34 个提交。

## 2. 部署事实（主代理独立验证）

- 容器：`RestartCount=0`、`OOMKilled=false`、`healthy`；compose 四标签齐全；挂载 `.dev-docker/runtime/isolated/{data,logs}` 均 bind RW（该目录已在 `.git/info/exclude:7` 忽略，不污染工作区）；**无 CPU/内存/PID 资源限制**（Memory/NanoCpus 均为 0）；以 root 运行。
- 新鲜度：容器内 `--build-info` 的 commit 等于当时 `git HEAD` 前缀。`-dirty` 只证明「构建时工作区有未提交改动」，**无法证明运行二进制对应哪一份工作区快照**；镜像创建后无源文件修改（mtime 比对 0 命中）。「部署代码==HEAD」不能仅凭哈希证明，此为边界而非缺陷。
- 端点：`/api/status` `/api/healthz` `/api/pricing` `/api/uptime/status` `/` 均 200；`/api/models` 401。未认证 `POST /v1/chat/completions`（坏 JSON、空 body）均先返 401 `authentication_error`，认证先于 body 解析。
- 安全头：实测响应带 `X-Content-Type-Options: nosniff`、`Referrer-Policy: strict-origin-when-cross-origin`；无 CSP/HSTS/X-Frame-Options（与代码注释一致，属有意）。
- 非规范路径：`/api/foo/../status`（--path-as-is）、`/api//status`、`/api/nosuchroute` 均 404 JSON。
- 只读数据面：`users=10 / channels=22 / tokens=17`；`two_fas`、`two_fa_backup_codes` 已迁移，`is_enabled=true` 计数 0；Redis `PONG`；`SESSION_SECRET`/`CRYPTO_SECRET` 非占位（值未回显）。
- 二进制符号：`buildTieredTokenParamsFromRealtime`、`TryTieredSettle`、`SecurityHeaders`、`AnonymousRequestBodyLimit`、`RequestBodyIngressLimit` 均在容器内二进制中。
- **部署前端主题为 classic（Semi Design），不是 `web/default`**：根页引用 `semi-ui-*.css`，无 radix/tailwind。这一点决定了 §3 中前端缺陷的部署暴露面。

## 3. 确认的缺陷（主代理已独立复现）

### P1

**3.1 Gemini 转换在 think-tag 提取后重复注入 reasoning part（流式还注入空 thought part）**
`service/convert.go:2194-2208`（流式）与 `:1895-1906`（非流式）各有两个**独立** `if` 都向 `content.Parts` 追加 `Thought: GetReasoningContent()`：前一个判断原生 reasoning 字段，后一个判断 `ThinkTagsExtracted`。think-tag 提取使两条件同时成立即重复。

主代理独立复现（`go test -overlay` 探针，仓库零写入）：

```
stream part[0] thought=true  text="R2"
stream part[1] thought=true  text="R2"      <- 同帧重复
stream part[2] thought=false text="more text"
stream parts n=3 thoughts=2
late part[0]  thought=true  text=""          <- 后续纯文本帧注入空 thought part
late part[1]  thought=false text="plain"
```

对照 Claude 路径（`convert.go:763`）用 `allowClaudeThinking` 开关而非追加块，输出正确。`service/` 下无 Gemini think-tag 测试，故 `go test ./service` 全绿不捕获。`service/convert.go` 修改时间 2026-10-09 15:08，早于镜像构建，**缺陷在部署产物中**。子代理补充的校正成立：不是「SSE 帧数翻倍」，重复发生在同一帧的 parts 内。

**3.2 2FA 前端门控未闭环入提交（HEAD 上会把挑战当登录成功）**
后端已收口（`6cc446dff`），前端 `web/default` 的两个文件未提交：
`git grep -c require_2fa HEAD -- 'web/default/src/routes/oauth/$provider.tsx'` 零匹配（退出码 1），工作区为 1；`use-oauth-login.ts` HEAD 零匹配，工作区为 13。HEAD 上挑战体 `{require_2fa:true}` 为真值，被当作已登录用户写入 store 并跳控制台。

边界（重要）：这是 **fail-closed**，后端不签发会话，2FA 门控本身未被绕过；且**当前部署不受影响**——部署由 dirty 工作区构建，含修复。缺口只存在于被提交的源码 HEAD 上（干净检出/CI 构建会命中）。因部署实际加载 classic 主题（§2），该文件的部署暴露面为零。

### P2

**3.3 NoRoute 分类回归：SPA 路由带扩展名查询串返回 404 JSON**
`router/web-router.go:89-102`（提交 `0d66b45e7`）把静态扩展名判定作用到含查询串的 `RequestURI`。主代理实测：

```
/sign-in                     -> 200 text/html
/sign-in?next=a.png          -> 404 application/json
/login?x=a.png               -> 404 application/json
/dashboard?file=a.png        -> 404 application/json
/console/token?x=a.png       -> 200 text/html     (含 / 的路径被守卫豁免)
/oauth/github?state=x.png    -> 200 text/html
/api/status?x=.png           -> 200 application/json  (反方向无误判)
```

影响面经实测收窄：仅**单段根级** SPA 路由受影响，含 `/` 的路径与真实 API 路径均不误判。子代理定 P1，主代理与复核者一致下调为 P2（触发面为长尾 URL 形态）。

**3.4 realtime tiered 结算未覆盖 img/img_o**
`relay/channel/openai/relay-openai.go` 的两处 realtime 聚合（`:528-535` turn 聚合、`:701-708` preConsumeUsage）只累加 Audio/Cached/Text，**从不累加 `ImageTokens`**（全文件仅 `:742` 非 realtime 路径命中）。而 `service/tiered_settle.go:105,109` 从 `InputTokenDetails/OutputTokenDetails.ImageTokens` 取 `Img/ImgO`，故表达式里恒为 0。默认表达式 `setting/billing_setting/tiered_billing.go:174` 确引用 `img`。主代理独立 `grep -n ImageTokens` 确认仅 `:742` 一处。可达性受限（唯一引用 img 的默认表达式绑定 gemini live 模型，而 gemini 渠道无 realtime 处理），属潜在缺口。

**3.5 `web/default` 注册页微信登录漏检 require_2fa**
`web/default/src/features/auth/sign-up/components/sign-up-form.tsx:184-198` 在 `res?.success` 后直接 `handleLoginSuccess` + `toast.success('Signed in via WeChat')`，无 `require_2fa` 分支。同一提交 `5893114ac` 在 sign-in 的等价出口已加，注册页未同步。主代理读码确认。注意：部署加载 classic 主题，故此项对当前部署无暴露面（classic 注册页见 3.7）。

### P3

**3.6 classic 注册页把 2FA 挑战当登录成功（部署中实际活着）**
`web/classic/src/components/auth/RegisterForm.jsx` 全文件 `require_2fa` 计数为 0（主代理 `grep -c` 确认；对照 LoginForm.jsx 为 7）。**部署产物核对**：`curl /assets/RegisterForm-CRtBuVig.js | grep -oc require_2fa` = 0，而 `LoginForm-DuQdERQ4.js` = 6、`OAuth2Callback-CGAgowT-.js` = 2。即部署中的注册页就是漏的。影响收窄：浏览器下 GitHub/Discord/OIDC/LinuxDO 走 `window.location.assign`，回调由已修的 `OAuth2Callback.jsx` 处理；实际受损出口为微信/Telegram/trusted_header（浏览器）与桌面端全部 OAuth。无会话绕过，属功能性回归。

**3.7 会话 Cookie 未设 Secure（硬编码 false）**
`main.go:216-222`：`Secure: false`。主代理读码确认。

**3.8 部署隔离库计费配置漂移（options 全量替换）**
`setting/config` 的 map 整体替换语义使源码新增键不生效。主代理对隔离 PG 只读全量键集比对：**源码默认 ModelRatio 有 488 键，部署库 704 键，其中源码有而部署库缺 76 键**（含 `gpt-6.1-sol`、`claude-opus-5-5`、`claude-fable-5-1`、`gpt-6-sol`、`gemini-3.8-flash` 等），部署库另有 292 键为 DB 侧自定义。缺键模型走 fail-closed 拒绝（`SelfUseModeEnabled=false` 实测），非「免费」。属配置漂移类潜伏缺陷，`abilities` 中这些模型计数为 0，当前不可达。

**3.9 部署库 `llama-3-sonar-large-32k-chat/-online` 比率为 0**
主代理实测部署库 `= 0`，源码默认 `1.0/1000*USD`（整数除法 bug 的传播结果，源码已修但库未回写）。因 `SelfUseModeEnabled=false`，比率 0 是 fail-closed 拒付而非免费——子代理「免费」的措辞不准确，但漂移事实成立。

**3.10 CAS 回调在 2FA 挑战路径下状态被消费但 ticket 被释放**
`controller/custom_oauth_external.go:163-171`：`consumeOAuthState(c)` 先于 `setupLoginWithResult` 执行，挑战路径下 state 已消费，重试语义与注释所述「失败可重试」不一致。主代理读码确认。

**3.11 authHelper 对同类鉴权失败混用 HTTP 200 与 401**
`middleware/auth.go` 中多处失败用 200（`:114/124/139/199/207/215`）与 401（`:98/149/173/191`）并存。主代理 `grep` 确认。

**3.12 `ServerAddress` 被持久化为容器内部主机名并对外回显**
部署库 `options.ServerAddress = http://new-api-dev-isolated-new-api-1:3000`，经匿名 `/api/status` 回显。子代理两个「功能不可用」论据被复核推翻：passkey 实测可用（`rpId=127.0.0.1`，属已测试的有意设计），OAuth 提供方均未启用故为潜在。写库来源更正为 `PostSetup`（`controller/setup.go:165`），非「回调期间」。降为 P3。

**3.13 XFF 可伪造绕过 IP 键限流**
`TRUSTED_PROXIES=auto` 下容器信任自身网段，客户端可自选 `X-Forwarded-For`。子代理实测复现（同 IP 第 3 次 429，旋转 XFF 后全 200；2FA 端点 20/1200s 同理）。已记录于 `docs/reviews/CR-ADMIN-RATE-LIMIT-EXEMPT-2026-09-29.md`（P0-2）。因隔离服务仅 loopback 可达且属已文档化编排取舍，降为 P3。

**3.14 隔离日志目录只增不减且积累过启动崩溃风暴**
`.dev-docker/runtime/isolated/logs/` 有 3517 个文件、82 MB，自 2026-06 起无轮转（`logger/logger.go:56` 按秒新建，全仓无 rotate 配置）。其中 1191 个文件含 `FATAL`，主体为启动时连不上 PG（`dial tcp ...5432: connection refused`）。2026-10-10 08:40:13–08:40:25 出现 8 连发崩溃。主代理独立发现并核实。当前容器已 healthy、`RestartCount=0`。

**3.15 其他已确认项（主代理静态核实）**
- `use-auth-redirect.ts:78-80`：OAuth 回调跳 `/otp` 后丢失原 redirect 目标。
- `TokenAuthReadOnly`（`middleware/auth.go:289-349`）不校验令牌状态/过期/额度/IP（文档标注为有意）。
- `middleware/cors.go:12-16` 注释前提与代码事实不符（前轮已记录未修）。
- classic 语言库新增 138 键，非英文语言缺 27-28 键，`i18n-cli sync` 判为已同步不补齐。
- 未认证 `/api/user/groups` 暴露非默认分组名与倍率（`router/api-router.go:96`）。
- `com.docker.compose.image` label 指向已不存在的镜像；容器以 root 运行且无资源限制。

## 4. 被推翻或收窄的子代理结论

| 子代理原始结论 | 复核裁决 | 主代理判断 |
| --- | --- | --- |
| AddAbilities 的 RowsAffected 断言在 MySQL 上把正常插入判为冲突（P1） | refuted | 复核者查 gorm mysql 驱动 `ClauseOnConflict`：`OnConflict{DoNothing}` 编译为 `ON DUPLICATE KEY UPDATE pk=pk` 而非 `INSERT IGNORE`，干净插入 RowsAffected=行数。原始前提不成立。**保留缺口**：仓库自带的两条 MySQL 契约测试因未设 `NEW_API_TEST_MYSQL_DSN` 被 Skip，未在真实 MySQL 实跑。 |
| 未定价模型经 tiered_expr 绕过 model_ratio=0 的 fail-closed（P2） | refuted | 复核者实测 `gpt-5.6-sol` 的 model_ratio=2.5（非 0），tiered 路径完全不消费 model_ratio；`model_ratio=0` 是展示伪影。 |
| palm 消费端泄漏 goroutine，且「唯独 palm 遗漏」（P2） | partial | 现象成立（动态复现 5/10 概率泄漏），但两条论据被推翻：gin `c.Stream` 顶层**有** clientGone 检查（在 `step()` 之前）；cohere/zhipu 消费端同样缺 cancel 分支，是三处同源缺陷。 |
| NoRoute 查询串回归 P1 | partial → P2 | 见 3.3，影响面经实测收窄。 |
| 阶梯预扣/结算不对称、阶梯回退可能计 0、upgradeBillingExpr 静默改写历史表达式 | partial → info | 降级为观察项。 |
| ServerAddress P2、XFF P2、classic RegisterForm P2、计费配置漂移 P2 | partial → P3 | 见 3.7、3.12、3.13、3.6、3.8。 |

## 5. 未覆盖边界（诚实记录）

- **真实认证态中继端到端：整轮空白。** 未创建用户、未用真实令牌、未穿 `relay → 上游 → 预扣/结算 → 日志落库`。所有 relay 类结论只到「源码 + 单测 + 只读探测」。因只读红线，未建号。
- **三库真实连接：只有 PG 被只读连过。** MySQL 未连真实实例（`NEW_API_TEST_MYSQL_DSN` 未设），SQLite 未单独动作。§4 中 AddAbilities 的 MySQL 结论依据驱动源码 + 官方手册 + 仓库契约测试三重静态证据，非实跑。
- **前端 2FA 挑战的浏览器端 e2e 未做**（需 2FA 账号，建号需写库，红线阻断）。§3.6 的部署暴露面结论来自部署 bundle 的 `require_2fa` 计数，属源码/产物级，非交互实测。
- **生产侧完全空白**（红线）：`new-api:13000` 与无前缀 PG/Redis 未触碰，旧缺陷的生产可达性未核。
- **无并发压测**；palm 泄漏的断连 e2e 未在仓库内跑（以等价最小 handler 在 `/tmp` 复现）。
- **基线文档只抽读「未覆盖」一节**，其 3.1–3.5 缺陷未逐条复核。
- 工作区在审查期间持续变化（未跟踪文件数 131→132→…），全部结论对应 2026-10-10 快照时点。

## 6. 主代理独立验证汇总

全部退出码 0，命令与输出均已读取：

| 验证项 | 命令 | 结果 |
| --- | --- | --- |
| 构建 | `go build ./...` | 0 |
| 静态检查 | `go vet ./controller ./model` | 0 |
| 最小测试集 | `go test ./controller ./model ./relay/common ./relay/helper ./service -count=1` | 5 包全 ok |
| 跨轮泄漏 | `go test ./controller ./service -count=2 -shuffle=on` | 全 ok |
| 2FA 门控 | `go test ./controller -run 'TwoFAGate\|...' -count=1 -v` | 8/8 PASS |
| realtime 计费 | `go test ./service -run 'RealtimeBilling\|TieredSettle\|...'` | ok |
| 前端 lint | `cd web/default && bun run lint` | 0 |
| Gemini 重复注入 | `go test -overlay=... -run TestMainAuditGeminiThinkTagDuplicate` | 复现（2 thought parts + 空 thought part） |
| NoRoute 回归 | `curl --path-as-is 3001/...` | 复现（404 JSON） |
| 部署键漂移 | 只读 `psql` + 源码默认 diff | 76 键缺失 |
| 2FA 前端未提交 | `git grep -c require_2fa HEAD -- ...` | 零匹配（exit 1） |

## 7. 结论

- 部署服务本身健康、端点与安全头符合预期，当前容器无 error/warn/panic。
- 最高优先级是 **3.1（Gemini think-tag 重复注入）**：本轮在途改动引入，已进入部署产物，有客户端可见影响（reasoning 重复、空 thought part），且缺测试。
- **3.2（2FA 前端未提交）** 在 HEAD 上是真实缺口但 fail-closed 且对当前部署无暴露面；真正在部署中活着的是 **3.6（classic 注册页）**。
- 计费侧无「静默错账」类活跃缺陷，但存在配置漂移（3.8/3.9）与 realtime 子类缺口（3.4）。
- 子代理 33 条原始发现中，2 条被推翻、12 条被收窄降级；降级主因是影响面被夸大或前提未实测。
