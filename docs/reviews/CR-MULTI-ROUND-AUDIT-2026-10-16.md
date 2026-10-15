# 多轮全场景全维度审查与修复记录（第五轮至第八轮）

采样时间：2026-10-15 至 2026-10-16。审查对象 `/Volumes/Work/code/new-api`，分支 `dev`。
方法：每轮 2 至 4 路并行只读审查代理（后端协议、安全、前端一致性、元审查），代理结论一律由主代理
用 `/tmp` 注入探针（`go test -overlay`）或运行时实测独立复核后才采信。全流程未触碰生产容器
（`new-api`、无前缀 `postgres`/`redis`），仅使用 `new-api-dev-isolated-*`。

## 0. 提交序列

| 提交 | 主题 |
| --- | --- |
| `5b1662881` | Waffo Pancake 下线收口、Telegram 登录接线 |
| `7478488f4` | Telegram 绑定 XHR 返回 JSON、清理 Pancake 死导入 |
| `b47c444fd` | 第五轮：flush 协议乱序、越权、配置缺口 |
| `4a38c50d2` | 第六轮：flush 误触发、SRC 门控遮蔽、Telegram 空钥匙 |
| `94727e102` | 第七轮：截断流终止、EmailBind 竞态、classic 排行榜初态 |
| `aba8f570e` | 第八轮：usage-only 首帧悬挂与 usage 丢失 |
| `c4256e491` | 第八轮：OAuth 自动注册不再认领已占用邮箱 |
| `69cc8dfa2` | 第八轮：补齐安全修复回归测试、修正只读联调示例 |
| `7e349e004` | 第八轮：本 CR 文档沉淀 |
| `6416e8589` | 第八轮：classic 顶栏默认值收敛到单一来源 |

## 1. relay think-tag / Claude SSE 转换（反复重构的核心面）

`relay/channel/openai/helper.go` 与 `service/convert.go` 的 `streamResponseOpenAI2Claude`
在三个提交内被连续重构，每一轮都引入过新回归，最终收敛如下。

### 1.1 已修复缺陷

- **协议乱序与重复终止（`b47c444fd`）**：flush 伪帧曾带 `finish_reason`，且在主帧之后发出。
  Anthropic SSE 的 `message_stop` 是终态，其后的事件被严格客户端丢弃，因此"修好的字节丢失"
  在协议层原样重放；伪帧的 `finish_reason` 还会覆盖 `info.FinishReason`，把 tool-use 流的
  `stop_reason` 从 `tool_use` 打成 `end_turn`。现改为伪帧不带 `finish_reason`，并在终止事件
  之前送出。
- **mid-stream 空 choices 帧误触发 flush（`4a38c50d2`）**：终端判定原先只看 `len(Choices)==0`，
  而部分上游会在流中间发空 choices 帧；此时排空缓冲会让未闭合 `<think>` 内的推理字节按"未闭合
  规则"作为可见文本归还，标签泄入正文。现终端判定与转换器一致：`len(Choices)==0 && Usage != nil`。
- **SRC 门控遮蔽导致流不终止（`4a38c50d2`）**：终端帧不经 `HandleStreamFormat`，
  `SendResponseCount` 停在最后一条普通帧上；转换器用 `SRC==1` 判定"发 message_start"，不推进则
  usage-only 终止分支被遮蔽。现在终端处理前 `info.SendResponseCount++`。
- **截断流不终止（`94727e102`）**：上游干净 EOF 但从未发 `finish_reason` 时，转换器只消费 delta
  不终止，客户端收不到 `message_delta`/`message_stop`，按 incomplete stream 报错。现在终端帧补
  `finish_reason="stop"` 走完整终止状态机。
- **usage-only 首帧悬挂（`aba8f570e`）**：见 1.2，本轮新发现并修复。
- **usage 丢失（`aba8f570e`）**：见 1.2，本轮新发现并修复。

### 1.2 第八轮新发现（本轮修复）

两处缺陷同源，都在 `streamResponseOpenAI2Claude` 的 `SRC==1` 分支：该分支发出 `message_start`
后直接 `return`，与下方的 usage-only 终止约定冲突。

1. **首帧为空 choices（usage-only）时流悬挂**。`SRC==1` 分支在 `len(Choices)>0` 守卫之外仍无条件
   `return`，empty-choices 终止分支被遮蔽，客户端只收到 `message_start`。
   - 定性：`4a38c50d2` 为修 SRC 门控遮蔽而加的 `SendResponseCount++` **引入的回归**。
     探针实测：`4a38c50d2^`（该修复之前）为 `[message_delta message_stop]`，修复后变为裸
     `[message_start]`。对 `520c691eb`、`5b1662881`、`768e1692d`、`025a2ec74` 各版本覆盖探针
     均为 `[message_delta message_stop]`，确认回归点是 `4a38c50d2`。
2. **首帧带 `finish_reason` 但不带 `usage`、usage 放在随后的 usage-only 帧时 usage 丢失**。
   首分支立即闭合生命周期，usage 再无机会下发，客户端 `message_delta` 缺失。
   - 定性：**存量缺陷**，`520c691eb` 起各版本探针一致复现
     `[message_start content_block_start content_block_delta content_block_stop message_stop]`。

修复：空 choices 首帧不 `return`，落到 usage-only 终止分支补 `message_delta`/`message_stop`；
首帧带 `finish_reason` 但 usage 尚不可知时不闭合生命周期（与下方非首帧分支的既有规则一致），
交给终端帧补 `message_delta`。

### 1.3 回归测试与反向验证

`relay/channel/openai/think_tag_stream_test.go` 现有 12 条用例。第八轮新增两条，断言事件序而非
字节存在：

- `TestUsageOnlyFirstFrameTerminates`：断言 `[message_start message_delta message_stop]`。
- `TestFirstFinishFrameDefersUsageToTerminalFrame`：断言恰好一组终止事件，且 `message_delta`
  携带 `output_tokens=5`。

反向验证：用 `94727e102` 的 `service/convert.go` 覆盖后两条均转红，失败信息分别为
"a usage-only first frame must still terminate the message" 与 "exactly one termination group"。

### 1.4 断言空洞（第八轮发现，已随 1.3 收窄）

变异实验显示：把 usage-only 终端帧的 flush 转换结果挪到终止事件之后（Claude 与 Gemini 各一例），
整个 `./relay/channel/openai/` 包测试套件仍然全绿——`TestHandleFinalResponseFlushesOnUsageOnlyTerminalFrame`
与 Gemini 对称用例只断言 body 含字节和出现过 `message_stop`，不检查顺序。探针实测该变异产出
`... msg_stop text(<think>reason never closed)`，正是 `b47c444fd` 修复的缺陷类别。本轮未直接补
该两条的顺序断言（属独立测试改进项），在此记录为已知覆盖缺口。

## 2. 安全修复

### 2.1 EmailBind 复查（`94727e102`）

发码时查过邮箱占用，但验证码有效期 10 分钟，期间邮箱可被他人注册占用。`users.email` 非唯一索引，
重复地址落库后 `ResetUserPasswordByEmail`（`Where("email = ?")`）会更新**所有**匹配行并把新生成
的明文密码返回给请求者。修复：绑定时复查 `IsEmailAlreadyTaken`。

第八轮补充回归测试 `controller/email_bind_test.go`（`TestEmailBindRejectsEmailTakenDuringCodeWindow`
断言失败绑定不落库，`TestEmailBindAcceptsUnclaimedEmail` 作对照）。反向验证去掉复查即转红。

已知边界：复查未排除自身行（`IsEmailAlreadyTaken` 无 `id <> ?`），"把账号当前已有的邮箱再绑一次"
会被误判为他人占用；经前端难以触达（发码端对同一邮箱同样返回占用），实际影响是误导性消息。
另 `RowsAffected == 1` 的写法经实测等价于"≥1 条"，当前语义安全但脆弱（目的改为切片即变 fail-open）。

### 2.2 Telegram 登录守卫（`b47c444fd`/`4a38c50d2`）

- 空 token fail-closed：空 token 会让 HMAC 钥匙退化为 `sha256("")` 这一公开常量，任何人可离线伪造
  签名接管绑定账户。
- `auth_date` 时效 24h、超前 5 分钟上限：此前签名的查询串是无期限登录凭证，回调 URL 一旦泄漏
  （浏览器历史、代理日志）可长期重放。
- 一次性消费（`telegramAuthOnce`）：同一签名在时效窗口内不可重放。
- `hash` 比较改常量时间（`subtle.ConstantTimeCompare`）。
- 反向守卫（`controller/option.go`）：开关启用时拒绝清空 Bot Token。

第八轮补充 `controller/telegram_auth_test.go` 七条用例并逐条反向验证；补
`TestUpdateOptionTelegramTokenReverseGuard` 覆盖 option 反向守卫分支。

已知边界：`telegramUsedAuth` 为进程内 `sync.Map`，多实例部署下同一签名每实例各可用一次（窗口
24h）；票据在会话/占用校验之前消费，失败即烧毁需重新签名；`params["id"][0]` 无存在性判断（缺 id
会 panic，被 `CustomRecovery` 接住成 500，仅持 bot token 者可触达）。

### 2.3 OAuth 自动注册认领已占用邮箱（`c4256e491`，第八轮新发现）

三条自动注册路径（`findOrCreateOAuthUserWithOptions`、`controller/github.go`、`controller/oidc.go`）
把 IdP 断言的邮箱无条件写入新账户，全程无占用检查。这是 2.1 同一跨账户链的**注册侧入口**，
且无需竞态：IdP 断言一个本地已被占用的地址即成立（如企业邮箱回收后新员工经 OIDC 首登，邮箱仍挂在
离职员工本地账号上）。

- 列级证据：`model/user.go:31` 为 `gorm:"index"`；dev 隔离 PG 实测 `idx_users_email` 是普通 btree。
- 危害链：`ResetUserPasswordByEmail` 批量改密 + `controller/misc.go` `ResetPassword` 返回明文。
- 既有旁证：`SyncEmailOnLogin` 路径（`controller/oauth.go`）早已对同一冲突执行"跳过并记录"
  （`isOAuthSyncFieldAvailable`），注册路径缺这一步。

修复：新增 `resolveOAuthRegistrationEmail`——去空格、超长（对齐 `oauthSyncEmailMaxLength=50`）或
已被占用时返回空串，新账户不携带邮箱创建，用户可后续绑定。邮箱合并登录（`AllowAutoMergeByEmail`）
不受影响。

回归测试 `controller/oauth_register_email_test.go` 断言库中该地址**恰好一行**（直指批量更新面），
反向验证（守卫改为直通）转红。

### 2.4 其他安全收口

- Waffo Pancake 半下线状态 fail-closed（`5b1662881`）。
- `pkg/billingexpr/run.go` 拒绝负的有限结果：`(p-c)*k` 且 `c>p` 时结算走返还路径，用户反获额度
  （`b47c444fd`）。
- `/api/user/groups` 挂 `middleware.TryUserAuth()`（`b47c444fd`）。注意 `TryUserAuth` 不阻断匿名，
  该改动是功能修正而非越权闭合。

## 3. 前端一致性

### 3.1 classic 排行榜（`b47c444fd`/`4a38c50d2`/`94727e102`）

`/rankings` 路由与守卫早已存在，但顶栏导航与设置面板开关缺失，用户只能手输 URL。三轮修复覆盖了
三种存量形态：

- 对象格式 `pricing` 但缺 `rankings` 键（存量 JSON 中间态）。
- `HeaderNavModules` 选项整体未设置（全新部署初态，`useNavigation` 本地默认补齐）。
- 设置面板初始 state 与加载分支统一走 `parseHeaderNavModules`，避免"加载完成前保存"写入缺键 JSON。

运行时终验（匿名，未登录）：开发库 `HeaderNavModules` 为"对象 pricing 缺 rankings"形态，顶栏渲染出
排行榜入口，`/rankings` 未被重定向。

已知边界（第八轮记录）：`HeaderNavModules` 被存成双重编码 JSON 字符串时，顶栏链与守卫链
解析分叉（需人为写入才触发）。默认值重复定义已在 `6416e8589` 收敛为单一来源
`DEFAULT_HEADER_NAV_MODULES`（原 `useNavigation` 与设置面板重置/catch 三处各自内联一份）。

### 3.2 其他

- `deleteLogsByFilter` 用户路径改 `/api/log/self/batch_delete`（原 `buildApiPath` 生成
  `/api/log/batch_delete/self` 必 404）。
- `buildRegenerationMessages` 保留原 content，含图消息重新生成不再丢图。
- 补登记 283 个 `t()` 键到 6 个 locale（解决缺键回退；值多为英文原文，未翻译）。
- 修正 `deploy/env/frontend-readonly.env.example` 的代理键为 `VITE_REACT_APP_SERVER_URL`
  （`web/default` 已迁 rsbuild）。

### 3.3 web/default 未挂载组件（第八轮发现，未修）

`520c691eb` 引入的一批组件无任何引用（import 路径 + 符号双向扫描确认）。其中两类有功能后果，
但**部署当前加载 classic 主题**（`/api/status` 实测 `theme=classic`），用户可见面未受影响：

- `features/usage-logs/components/log-delete-controls.tsx`：其调用的 `deleteLogsByFilter` 与后端
  两个端点均在，但 `usage-logs-primary-buttons.tsx` 无删除入口——default 日志页整体缺"按条件删除"
  能力（classic 侧 `useUsageLogsData.jsx` 该功能正常）。
- `features/playground/components/playground-settings-panel.tsx`：其自身零引用，连带
  `playground-sse-viewer.tsx`、`playground-code-viewer.tsx`；温度/top_p/max_tokens/seed/流式/
  图片/自定义请求体在 default 的 playground UI 上不可达。

`520c691eb` 提交信息称"悬空组件接线后全部通过 typecheck/lint/build"——typecheck 通过不能证明接线，
该表述未兑现。处置建议：单独一个"接线或整体下线"提交，同时清理仅这些死文件引用的 i18n 键与 API 面，
避免半删状态。

## 4. 验证记录

命令与结果（本轮实际运行）：

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | 退出码 0 |
| `go vet ./controller ./service ./relay/channel/openai` | 无输出 |
| `go test ./relay/... ./service ./controller ./model ./pkg/billingexpr ./setting/... -count=1` | 零 FAIL |
| `gofmt -l`（改动文件） | 无差异 |
| 反向验证探针（`go test -overlay`，6 组） | 各组仅目标用例转红 |

开发隔离环境（`new-api-dev-isolated-new-api-1`）：

| 项 | 值 |
| --- | --- |
| `git rev-parse HEAD` | `7f488a8c90e4a7f058b71ef0d844723c49761ffa` |
| `--build-info` commit | `7f488a8c90e4a7f058b71ef0d844723c49761ffa-dirty` |
| `--build-info` date | 2026-10-15T21:33:20Z |
| 二进制符号 `resolveOAuthRegistrationEmail` | 命中 |
| `/api/status` | HTTP 200 |
| 容器状态 | healthy |
| 服务主题 | classic |

`-dirty` 来自工作区其他并发会话的未提交文件（`web/classic/.../EditChannelModal.jsx`），非本次改动。
容器 `healthy` 只证明服务健康，与"运行最新代码"分开表述：本表的 commit 比对与符号命中才是运行
最新代码的依据。

## 5. 未覆盖边界

- 未做带管理员登录的运行态点击验证（开发库用户均非本会话可登录账户）：classic 设置面板开关的
  三方一致性、Telegram BotName 提交路径为代码级推演 + helper 探针。
- 依赖真实数据库列类型与三库差异的逻辑只用 SQLite 内存库覆盖语义，未在 MySQL 实测。
- `go test -shuffle=on ./service/` 有既有跨运行测试泄漏（"sql: database is closed"），非本轮引入，
  未修。
- 第八轮前端代理的 i18n 死键统计、dist 新鲜度为只读记录，未逐一复算。
- 生产环境未做任何变更；生产运行的 commit 早于上述全部修复。
