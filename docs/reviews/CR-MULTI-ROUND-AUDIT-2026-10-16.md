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
| `4782d20b1` | 第九轮：删除未被引用的 `RelayPanicRecover` |
| `6d081bb71` | 第九轮：批量接口加上限、补齐 Ollama 拉取流缓冲头 |
| `040bf82b0` | 第九轮：Telegram 缺 id 参数时显式拒绝 |
| `4a74c8603` | 第九轮：身份占用检查改为计数判定，补 Unscoped 读取 |
| `7b4d13e0f` | 第九轮：硬删已注销用户、邮箱绑定排除自己那一行 |
| `e71808425` | 第九轮：classic 已注销用户彻底删除入口 |
| `0772c2dfa` | 第九轮：default 已注销用户彻底删除入口 |
| `827753aa5` | 第九轮：default 日志页接入按条件删除 |
| `e4d816669` | 第九轮：default 接线 playground 设置面板与调试面板 |
| `223a653d2` | 第九轮：think-tag 流用例补事件序断言 |

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

### 1.4 断言空洞（第八轮发现，已在第九轮收窄）

变异实验显示：把 usage-only 终端帧的 flush 转换结果挪到终止事件之后（Claude 与 Gemini 各一例），
整个 `./relay/channel/openai/` 包测试套件仍然全绿——`TestHandleFinalResponseFlushesOnUsageOnlyTerminalFrame`
与 Gemini 对称用例只断言 body 含字节和出现过 `message_stop`，不检查顺序。探针实测该变异产出
`... msg_stop text(<think>reason never closed)`，正是 `b47c444fd` 修复的缺陷类别。已在第九轮补齐
顺序断言，见 6.2。

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

### 3.3 web/default 未挂载组件（第八轮发现，第九轮已接线）

`520c691eb` 引入的一批组件无任何引用（import 路径 + 符号双向扫描确认）。两类有功能后果，
且部署当前加载 classic 主题（`/api/status` 实测 `theme=classic`），用户可见面在第九轮之前未受影响：

- `features/usage-logs/components/log-delete-controls.tsx`：其调用的 `deleteLogsByFilter` 与后端
  两个端点均在，但 `usage-logs-primary-buttons.tsx` 无删除入口——default 日志页整体缺"按条件删除"
  能力（classic 侧 `useUsageLogsData.jsx` 该功能正常）。
- `features/playground/components/playground-settings-panel.tsx`：其自身零引用，连带
  `playground-sse-viewer.tsx`、`playground-code-viewer.tsx`（经 `playground-debug-panel.tsx`）；
  温度/top_p/max_tokens/seed/流式/图片/自定义请求体在 default 的 playground UI 上不可达。

`520c691eb` 提交信息称"悬空组件接线后全部通过 typecheck/lint/build"——typecheck 通过不能证明接线，
该表述未兑现。第九轮选择接线而非下线，见 6.3；接线时发现设置面板承诺的"自定义请求体原样发送"
在发送路径上从未实现，一并补齐。

## 4. 验证记录

### 4.1 第九轮（2026-10-16）

命令与结果（本轮实际运行）：

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | 退出码 0 |
| `go test ./model ./controller ./service ./relay/channel/openai -count=1` | 零 FAIL |
| `gofmt -l`（改动文件） | 无差异 |
| `bun run lint`（web/default） | 退出码 0 |
| `bun run typecheck`（web/default） | 退出码 0 |
| `bun run build`（web/default） | 退出码 0，产物 18809.5 kB |
| `bun run lint`（web/classic） | 退出码 0 |
| `bun test src/features/playground/lib`（web/default） | 13 pass / 0 fail |
| `bun run i18n:sync` + `node scripts/i18nLintGate.mjs` | 未发现超出基线的新问题 |
| 反向验证探针（`go test -overlay`，9 组） | 各组仅目标用例转红 |

反向验证明细：

| 变异 | 目标用例 | 结果 |
| --- | --- | --- |
| `identityInUse` 改回 `RowsAffected == 1` | `TestIdentityInUseFailsClosedWhenCountFails` | 转红 |
| `identityInUse` 改用切片目的地 + `== 1` | `TestIdentityInUseTreatsDuplicateRowsAsTaken` | 转红 |
| `identityInUse` 计数失败时返回 false | `TestIdentityInUseFailsClosedWhenCountFails` | 转红 |
| `EmailBind` 改回 `IsEmailAlreadyTaken` | `TestEmailBindAllowsRebindingOwnEmail` | 转红 |
| Gemini flush 改到终止帧之后发送 | `TestHandleStreamFormatFlushesSplitterOnTerminalFrame/gemini` | 转红 |
| Claude 终止事件重复下发 | `TestHandleStreamFormatFlushesSplitterOnTerminalFrame/claude` | 转红 |
| Claude flush 发出后不闭合生命周期 | `TestHandleFinalResponseFlushesOnUsageOnlyTerminalFrame` | 转红 |

开发隔离环境（`new-api-dev-isolated-new-api-1`，重建于 2026-10-16T09:04:01Z）：

| 项 | 值 |
| --- | --- |
| `git rev-parse HEAD` | `223a653d2e30413eef9c24a7de369c1675f2391b` |
| `--build-info` commit | `223a653d2e30413eef9c24a7de369c1675f2391b-dirty` |
| `--build-info` date | 2026-10-16T09:04:01Z |
| 二进制符号 `model.identityInUse` | 命中 |
| 二进制符号 `model.GetUserByIdUnscoped` | 命中 |
| 二进制符号 `model.IsEmailTakenByOther` | 命中 |
| 二进制符号 `controller.telegramParamID` | 命中 |
| 二进制字符串 `X-Accel-Buffering` | 命中 |
| 二进制字符串 `common.batch_too_many` | 命中 |
| `/api/status` | HTTP 200 |
| 容器状态 | healthy |
| 服务主题 | classic |

`-dirty` 来自工作区其他并发会话的未提交文件（`web/classic/.../EditChannelModal.jsx`），非本次改动。
容器 `healthy` 只证明服务健康，与"运行最新代码"分开表述：本表的 commit 比对与符号命中才是运行
最新代码的依据。

### 4.2 第八轮（2026-10-15）

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
  三方一致性、Telegram BotName 提交路径为代码级推演 + helper 探针；第九轮的已注销用户
  「彻底删除」按钮、default 日志页删除按钮与 playground 面板为 typecheck/lint/build + 逻辑
  走查，未做浏览器实点。
- 依赖真实数据库列类型与三库差异的逻辑只用 SQLite 内存库覆盖语义，未在 MySQL 实测。
  `identityInUse` 的计数语义三库一致（COUNT），风险低，但未实测。
- `go test -shuffle=on ./service/` 有既有跨运行测试泄漏（"sql: database is closed"），非本轮引入，
  未修。
- 第九轮前端的 dist 新鲜度以 `bun run build` 退出码 0 为准，未做产物哈希比对。
- 生产环境未做任何变更；生产运行的 commit 早于上述全部修复。
- 生产库 `users` 表是否存在重复邮箱仍未查询（需生产侧人工执行
  `SELECT email, COUNT(*) FROM users WHERE deleted_at IS NULL GROUP BY email HAVING COUNT(*) > 1;`），
  结论依赖该查询后才能给出。

## 6. 第九轮修复明细（2026-10-16）

第八轮报告后用户指示「全部优化好」，本轮按项收口，每一项独立提交并反向验证。

### 6.1 后端

- **死代码（`4782d20b1`）**：`middleware/recover.go` 的 `RelayPanicRecover` 全仓库零引用，且它把
  panic 值原样回显给客户端，与 `main.go` 的 `CustomRecovery`（只回传 `request_id`）冲突。删除。
- **批量接口上限（`6d081bb71`）**：`DeleteChannelBatch`、`BatchSetChannelTag`、`DeleteTokenBatch`
  此前只受 `/api` 组 128MB 入口兜底，可触发开事务逐条语句或巨型 IN 子句。分别加上限 1000/1000/100
  （沿用管理界面分页量级），超限返回 `common.batch_too_many`。同提交补齐
  `OllamaPullModelStream` 的 `X-Accel-Buffering: no`，与主 relay 路径的
  `SetEventStreamHeaders` 对齐，避免 nginx 缓冲进度流。
- **Telegram 缺 id 参数（`040bf82b0`）**：HMAC 校验只保证签名覆盖的键集合一致，不保证其中含
  `id`；`TelegramBind`/`TelegramLogin` 直接读 `params["id"][0]`，签名正确但去掉 `id` 的载荷
  会 panic。改为 `telegramParamID` 取值，缺失或空时返回"无效的请求"。回归测试
  `TestTelegramParamIDHandlesMissingID` 用去掉 id 的签名载荷覆盖。
- **身份占用检查（`4a74c8603`）**：六处 `Is*AlreadyTaken` 都写 `Find(&User{}).RowsAffected == 1`，
  把"是否被占用"绑定在"恰好命中一行"上——目的地换成切片即翻转判定。改为 `identityInUse`
  计数 `> 0`，计数失败保守判占用（与 `IsLinuxDOIdAlreadyTaken` 既有语义一致）。新增
  `model/user_identity_in_use_test.go` 四条不变量（重复行判占用、未占用判空闲、作用域由调用方
  决定、计数失败判占用），三条变异反向验证转红。同提交新增 `GetUserByIdUnscoped` 与
  `IsEmailTakenByOther` 供下一条使用。
- **硬删墓碑行 + 邮箱绑定幂等（`7b4d13e0f`）**：`DeleteUser` 原用 scoped 查询读目标，读不到
  软删行，而身份检查刻意包含软删行——被注销账号的邮箱 / OAuth 绑定永久锁死（本人无法重新绑定，
  管理员无清理入口）。改经 `GetUserByIdUnscoped` 读取。`EmailBind` 复查改用
  `IsEmailTakenByOther(email, user.Id)`：OAuth 登录会先把 IdP 邮箱同步到本地账户，用户随后
  走一次绑定确认属幂等操作，原检查会把自己判成占用者。三条新用例反向验证。

### 6.2 测试加固（`223a653d2`）

1.4 记录的断言空洞收口：Claude 侧断言恰好一组 `message_delta`/`message_stop` 且
`message_stop` 是最后事件；Gemini 侧断言承载 flush 字节的帧恰好一个且排在带 `finishReason`
的终止帧之前。反向验证两组变异各自转红。

### 6.3 前端接线

- **classic 已注销用户入口（`e71808425`）**：`UsersColumnDefs` 操作列对墓碑行返回空，改为只留
  「彻底删除」；`DeleteUserModal` 区分墓碑行（`DELETE /api/user/:id`）与普通注销
  （`manageUser('delete')`），文案分别说明后果；`useUsersData` 新增 `hardDeleteUser`。
- **default 已注销用户入口（`0772c2dfa`）**：`data-table-row-actions.tsx` 对墓碑行返回 null，
  改为只留「Delete permanently」菜单项，复用既有 `UsersDeleteDialog`；弹窗文案区分两种目标。
  新增四条文案，六个 locale 同步翻译。
- **default 日志页按条件删除（`827753aa5`）**：`LogDeleteControls` 挂进
  `UsageLogsPrimaryButtons` 并挂到 `SectionPageLayout.Actions`；删除范围复用列表的
  `buildApiParams` 保证删看到的即删掉的；只对 common 日志开放（绘图/任务日志无对应端点）。
- **default playground 面板（`e4d816669`）**：设置面板与调试面板（连同 code/sse viewer）挂到
  页面右上角图标切换，桌面端右侧栏、移动端上下堆叠。接线时发现两处"面板承诺但发送路径未实现"
  的缺口并补齐：`customRequestMode` 下请求体现在真的原样发送（`parseCustomRequestBody`，
  解析失败显式报错而非静默回落）；调试面板所需的原始 SSE 帧经 `use-stream-request` 新回调采集，
  且仅在 `showDebugPanel` 打开时采集。playground 错误文案改经 `t()`，参数标签登记
  `static-keys`，六个 locale 同步。

### 6.4 第九轮明确不修（与理由）

- **`telegramUsedAuth` 仅进程内一次性**：多实例部署下同一签名可在另一实例重放。需要 Redis 共享
  状态支撑，属产品部署形态决策，暂以文档记录。
- **`/api/status` 版本指纹、`/api/setup` 匿名窗口、WS `CheckOrigin=true`（鉴权前不可达已实测）、
  `/api/user/groups` 匿名可读**：维持第八轮结论，均为低风险或需产品决策，不在本轮范围。

## 7. 第十轮全维度审查与修复（2026-10-16/17）

六个 agent 分维度审查（开发服务运行态、部署/桌面、后端安全、relay 协议、计费与数据一致性、
前端一致性），共 34 条编号发现；每条由主会话独立复现（overlay 探针重跑、git 考古、路由与
调用点 grep），结果：已验证 30 条、推断 3 条、证伪 1 条。

### 7.1 证伪（不修）

- **ManageUser disable 墓碑行复活零值缓存（原 P2）**：探针显示 `manage(enable)` 写入的缓存是
  从 DB 重读的真实值而非 5 字段零值，随后的 `manage(disable)` 走 `InvalidateUserCache`，
  TokenAuth 对失效缓存返回 500 而非放行。不成立。

### 7.2 修复（第一批，8 个提交）

- **`d9db30fbb` 结算列不再随陈旧快照回写**：`User.Update` 排除
  `Quota/UsedQuota/RequestCount/AffQuota/AffCount/AffHistoryQuota`，其余字段保留"零值不写"
  语义（多处调用方持有部分结构体走 Update，全量白名单会写坏数据）。回归测试含并发扣减覆盖
  与"仍写调用方列"的反向断言。
- **`cda760577` 硬删清理 OAuth 绑定**：`DeleteUserOAuthBindingsByUserId` 从零调用点接入硬删
  路径，孤儿绑定不再让该身份在"已占用"与"用户不存在"之间永久卡死。
- **`af29d6fc3` 非流式错误体判定统一**：收敛到 `types.OpenAIError.HasContent`，三条路径同口径。
- **`73c2c8185` 流式四类修复**：thinking_to_content 同帧正文保全 + 纯推理流闭合标签；
  Claude 空 choices 判定与转换器对齐（计入累积 usage 并先排空扣留字节，反向守卫防心跳帧
  提前闭合）；include_usage=false 时剥离 usage 后仍下发终帧；Gemini 截断流补 finish_reason。
- **`a07b2dd8b` 按条件删除补齐 upstream_request_id**：前后端同口径，删除范围不再超出列表所见。
- **`dc61b2597` 性能指标开关贯通**：读接口在开关关闭时 404，`/api/status` 暴露
  `perf_metrics_enabled`，前端两个查询按开关短路。
- **`344a4cb2b` 日志路径换行中和 + 404 禁长缓存**：`sanitizeAccessLogPath` 折叠 `\r\n`；
  `RelayNotFound` 显式 `no-store`。
- **`3ae21aca4` 前端两项**：通用 Markdown 叠加 `rehype-sanitize`；playground 调试面板两条发送
  路径都在发请求前清空回复累加器。

### 7.3 修复（第二批，10 个提交，2026-10-17）

- **`6127e10e0` override 后失效用户缓存**：管理员 override 直写 DB 不走 Redis 同步，TTL 内
  `GetUserQuota` 仍按旧额度鉴权/预扣。回归断言"缓存不得仍是旧值 + DB 读为新值"。
- **`e9d5dbd0a` 退出时落盘看板聚合缓存**：`CacheQuotaData` 只在 5 分钟定时器落盘，优雅退出
  会丢最近一个窗口的 `quota_data` 聚合；在 `FlushBatchUpdates` 之后补
  `SaveQuotaDataCache`（仅 `DataExportEnabled`）。
- **`d7440123c` Claude 请求转发 tool_choice**：`ClaudeToOpenAIRequest` 此前只映射 tools，
  `{"type":"tool"/"none"/"auto"}` 全部丢失、上游按默认 auto 执行。四个子用例钉住映射。
- **`c09821b47` 路由范围保存与 RowsAffected 解耦**：MySQL 的 `RowsAffected` 是"变更行数"，
  同值重写返回 0，旧代码据此误判"行不存在"跳过更新。改为 COUNT 存在性检查 + 无条件
  `Updates`。已在一次性 MySQL 8.0 容器上实测同值重写返回 0。
- **`6fe4d0313` Telegram 一次性签名 Redis 去重**：进程内集合在多实例下各存一份，签名可跨
  实例重放；改为 `SET NX`（2s 超时），Redis 报错显式失败，仅 Redis 未启用时走进程内兜底。
- **`ef8e142e4` 前端两项**：default 微信绑定改回 POST（与后端路由一致）；classic 日志筛选补
  `upstream_request_id`（7 个 locale 同步）。
- **`ae61c8ff0` 只读代理硬化**：默认只绑 loopback，启用 `init`。
- **`ebfb9e944` 修复 `e9d5dbd0a` 的越界提交**：后者把另一会话未提交的
  `model.StopBatchUpdater()` 调用卷进索引，而该函数只存在于对方工作区，HEAD 因此无法编译。
  本提交只从提交内容移除这三行，工作区文件不动。已用干净克隆验证 HEAD `go build` 通过。
- **`a0fccdaab` 契约测试接入 CI**：新增 `operational-contracts` job 跑
  compose-healthcheck-contract(.test).py 与 dockerfile-contract.test.mjs。
  后者对健康检查的断言从精确 argv 放宽为语义检查——根 compose 在 `48c569077` 改为 shell
  形态读取 PORT 后，该测试在 HEAD 上一直红着（本次修复前确认）。反向验证：注入 `-T 5` 后
  测试按预期变红。

### 7.4 第十轮明确不修（与理由）

- **`DecreaseUserQuota` 相对扣减无余额守卫**（对照 aff 转移有 `aff_quota >= ?`）：预扣费链路
  已有余额校验，加守卫会让"预扣后补扣"在边界上失败，改动风险大于收益；现有行为不造成进一步
  损失（余额不足会拒绝后续请求）。
- **realtime/audio 结算混用运行期倍率与请求期冻结倍率**：与 text 路径口径差异存在，但修正需
  在 `PriceData` 上冻结更多倍率字段并覆盖 realtime/audio 两条链路，属独立改造。
- **Waffo Pancake 冻结调用（default wallet）与 classic 批量删部署幽灵端点**：两者都被固定
  开关断路、fail-closed，删除脚手架会让将来恢复成本更高，保留并记录。
- **Dockerfile 无 `USER`（root 运行）**：改为非 root 需要部署侧同步调整数据目录属主，单侧改动
  会让现有绑定挂载写失败，需与部署一起做。
- **`dtolnay/rust-toolchain@stable` 未钉 SHA**：该 action 的用法本身不支持 SHA 钉版，维持现状。
- **`/api/pricing` 匿名可见分组名、`/api/status` 字段暴露**：维持既有结论，属产品决策。
- **本地 `deploy/env/dev-isolated.env` 的 `DEV_REDIS_PORT=6381`**：与 example 的 6380 不一致，
  但 6380 当前无监听、legacy `docker-compose.dev.yml`（用 6381）已不运行，冲突源不存在；
  本地 env 是未跟踪的个人配置，不改。

### 7.5 第十轮验证记录

- 后端：`go test ./controller ./model ./relay/... ./service ./middleware ./types -count=1` 全绿；
  AGENTS.md 最小集 `go test ./controller ./model ./relay/common ./relay/helper ./service` 全绿。
- 前端：`bun run typecheck`、`bun run lint`、`bun run build` 均通过（含 Safari 兼容重写检查）。
- 契约脚本：`python3 -I scripts/compose-healthcheck-contract.test.py`、
  `python3 -I scripts/compose-healthcheck-contract.py`、`node --test scripts/dockerfile-contract.test.mjs`
  在修复后 HEAD 内容上全绿；`dockerfile-contract.test.mjs` 的 timeout 断言做过注入 `-T 5`
  的反向验证。
- HEAD 构建：干净克隆 `git clone --no-local --branch dev` + 复制前端 dist 后
  `go build -o /dev/null .` 退出 0（`ebfb9e944` 之前该构建因 `model.StopBatchUpdater` 未定义
  失败）。
- MySQL 语义：一次性 `mysql:8.0` 容器（127.0.0.1:33307）验证同值 `UPDATE` 的
  `RowsAffected` 为 0，容器用后移除。
- 探针：全部在内存 SQLite / miniredis / `/tmp` overlay 上运行，仓库内零残留，未触碰生产容器
  与开发库数据；工作区余下改动均属另一并发会话（`.env.example`、`docker-compose.yml`、
  `model/{batch_flush_test,channel,main,utils}.go`、`EditChannelModal.jsx`、两份新增文件）。
- 未覆盖：真实 MySQL/PG 实跑全量回归（仅 route scope 单点实测）、真实上游中间 usage 帧
  频率、带登录态浏览器点击。

## 8. 第三批：CI 门禁与工具链统一（2026-10-17）

第十轮复盘发现的"验证缺口"批次——问题不在业务代码，在于这些代码从来没有被机器强制
检查过。

- **`805316857` format:check 转绿**：`bun run format:check` 长期不在任何 CI 里，
  prettier 漂移积累到 20 个文件（3 个是上批提交插 import 引入，17 个是 520c691eb 等
  存量）。纯格式改动，lint/typecheck/build 复验通过。
- **`c5ebb906c` Go 统一到 1.27.2**：go.mod 1.25.1 / Dockerfile 1.26.8 / tauri 1.25.14 /
  release.yml 浮动 `>=1.25.14` 四处各自为政；1.27.2 是 go.dev 当前最新稳定版
  （`curl go.dev/dl/?mode=json`：1.27.2、1.26.9）。Dockerfile 换
  `golang:1.27.2-alpine@sha256:f92b6ef8...`，go.mod 与嵌套工具模块 go 指令、五个
  setup-go 全部对齐；go.sum 零变化。验证：1.27.2 容器内 `go build ./...`、
  `go vet ./...` 通过，AGENTS.md 最小集全绿。
- **`5bf7dcb37` classic eslint 修复**：根配置 ecmaVersion 2020 解析不了 tests 的顶层
  await，`bun run eslint` 一直是红的；tests/** override 升 2022，src 规则不动。
- **`d3a016906` go-test-all.sh 嵌套模块修复**：全量跑出唯一失败——rg 枚举把
  `tools/relaykit-dto-compat`（独立 go.mod）当主模块包，`go test` 报
  "does not contain package"。主循环排除 + 结尾按各自 go.mod 分跑；反向验证：去掉
  排除行原失败复现。修后 exit=0（88 主模块包 + 嵌套模块，66 个 ok 行）。
- **`d9467a07c` CI 接入 Go 与双前端门禁**：repo-gates 新增 `go-tests`（go build/vet/
  全量测试，stub 两个 go:embed dist）与 `frontend-checks`（web/default format/
  lint/typecheck/build，web/classic prettier/eslint 与 140 个 node 契约测试，bun 版本
  读 `.bun-version`）。此前 CI 里 `grep "go test"` 零命中，HEAD 带着未定义函数都能
  提交（本轮 ebfb9e944 修的正是这种断裂）。
- **`85299c2a5` relaykit 钉点单一来源**：workflow 手写 `--ref 789c97019` 与
  selfcheck 的 `UPSTREAM_PIN` 是两份副本，升锚漏改就分叉。新增 `relaykit-pin.py` 从
  常量取值，workflow 改动态引用；`check_ci_pin` 对钉点做一致性核对（动态形态或字面
  相等，否则 fail-closed），补 5 个单测（42→47）。空 `--ref` 实测 exit=1，取值失败
  不会回落到会漂移的 upstream/main。
- **`9f6ee63cf` 文档与 CODEOWNERS**：AGENTS.md/架构计划的"Go 1.25+"对齐 1.27.2；
  新增 CODEOWNERS 声明 CI/部署/契约脚本的审阅面（需分支保护开关才强制）。

**最终验证（干净克隆 `9f6ee63cf`）**：`go build` OK；`go-test-all.sh -count=1`
exit=0；selfcheck 自述计数全一致、47 单测 OK；compose/dockerfile 契约全绿；镜像
（Go 1.27.2 工具链）重建后隔离容器 healthy、build-info=9f6ee63cf、healthz OK、
启动日志 0 panic。未覆盖：GitHub Actions 真实 runner 上的首次运行（本地按同一步骤
逐条跑通，workflow YAML 结构已解析校验）；双前端旧版本的 live 服务器回归。

## 9. 第四批：CI 自身缺陷（2026-10-17）

上一批把门禁接进 CI 后复查门禁本身，找到三处「门禁会红、但原因不是代码问题」的缺陷。

- **`d716acd81` go-test-all.sh 依赖 rg**：脚本开头 `command -v rg || exit 1`，而
  GitHub ubuntu runner 不预装 ripgrep（`actions/runner-images` 的 Ubuntu2404
  工具清单 126 条无 ripgrep，已实读核对）。改成三段式：rg 优先 / git 仓库退回
  `git ls-files` + 未跟踪文件 / 非 git 目录退回 find。验证：两条路径对同一仓库
  枚举出完全相同的 1166 个 .go 文件（diff 为空），88 包 + 嵌套模块各跑一次，
  两路 `-count=1` 全量 exit=0、66 个 ok。
- **`609b99f5e` i18n-gate 的 bun 未钉版**：frontend-checks 与 tauri 读
  `.bun-version`(1.4.0)，i18n-gate 不传版本取 latest，同仓库两个 job 跑不同 bun。
- **嵌套模块发现的参数误传**：非 rg 分支里 `list_files -g 'go.mod'` 会把 -g 当普通
  参数忽略、返回全部 3135 个文件，dirname 后冒出 `.agents/skills/...` 这类假模块。
  改为 `--go-mod` 专用分支。这条在无 rg 沙箱里实测复现并修复。

**未修（需你决策）**：`pr-check.yml` 声明 `contents/issues/pull-requests: read`，
但配置了 `failure-add-pr-labels` 与 `close-pr: true`——只读 token 打不了标签也关
不了 PR。要么补 `pull-requests: write` / `issues: write`，要么这两项实际不生效。
该 action 的 action.yml 在 v0.2.1 取不到（404），无法确认它到底需要哪几项，故未擅自
放权。
