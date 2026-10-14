# 全场景全维度审查（基线 52d63c24e 至 HEAD 52972c7cf）

采样时间：2026-10-14 00:30-01:00 UTC（容器运行期全程只读）。

## 1. 范围与方法

- 源码：分支 dev，快照 HEAD `52972c7cf`（距上轮基线 `52d63c24e` 68 个提交，其中 i18n 38 个、relaykit 文档约 25 个、功能修复约 5 个）。
- 工作区：1 个已修改文件（并行会话 EditChannelModal strip_codex，+111 行）+ 133 个 untracked 在途文件。untracked 文件不进入部署产物（Dockerfile 不拷贝），但影响本地 `go test` 编译。
- 服务：`new-api-dev-isolated-new-api-1`，healthy，RestartCount=0，连续运行约 32 小时，commit=175a7f32e-dirty。
- 方法：6 路并行只读审查代理（Go 后端增量、web/classic、web/default 与桌面端、数据库、部署与安全、脚本与供应链），全部结论经主代理独立复核关键项后采信。全程未改仓库、未写数据库、未触碰生产容器。

## 2. 已确认缺陷（主代理已独立复现）

### P1

**2.1 置顶渠道功能链路断裂（事故恢复提交的半成品接线，部署中 live）**
`web/classic/src/hooks/channels/useChannelsData.jsx` 定义并挂载调用了 `fetchTopChannels`/`pinChannel`/`unpinChannel`/`movePinnedChannel`（:549-605），但 hook 的 return 块（:1349-1475）不含这些函数；`ChannelsTable.jsx`（:71-88）未向 `getChannelsColumns` 传参。消费侧 `ChannelsColumnDefs.jsx:357-360` 解构后 `pinChannel` 为 undefined，用户点击置顶/上移/下移菜单（:785/:791）必抛 `TypeError: pinChannel is not a function`。另外挂载时 `:274` 请求 `/api/channel/pins` 得到的数据无消费者，每次进渠道页多一次无效请求。后端路由存在（`router/api-router.go:263,272`），缺陷纯在前端接线。根因：恢复提交 175a7f32e 的「未能恢复名单」包含 ChannelsTable/channels/index，hook 内部恢复但外部接线未恢复。主代理复核：return 块 grep 计数 0，属实。

### P2

**2.2 模型性能列静默失效（同根因，部署中 live）**
`performanceSummary` 由 `PricingPage.jsx` 注入，`PricingTableColumns.jsx:124,173` 渲染，但中间 `PricingContent.jsx`、`PricingView.jsx`、`PricingTable.jsx` 三处透传全部缺失，`getPricingTableColumns` 的入参与 useMemo 依赖均不含该字段，默认 `={}` 恒生效，性能列（模型 24 小时性能 badge）永远为空。无报错，静默失效。主代理复核：PricingContent/PricingView/PricingTable 三文件 grep 零命中，属实。

**2.3 realtime tiered 结算未覆盖 img/img_o（上轮 3.4 遗留）**
`relay/channel/openai/relay-openai.go:491-495`（turn 聚合）与 :664-668（totalUsage）只累加 Audio/Cached/Text，从不累加 `ImageTokens`（全文件唯一命中 :702 属非 realtime 路径）。`service/tiered_settle.go:105,109` 从 ImageTokens 取 img/imgO，realtime 路径恒为 0。可达性受限（唯一引用 img 的默认表达式绑定 gemini live 模型，gemini 渠道无 realtime 处理），属潜在缺口。主代理复核 grep 属实。

**2.4 relaykit 门禁与 i18n 门禁均未接入 CI，仅本地手动生效**
`.github/workflows/` 7 个 workflow、Makefile 均无 relaykit 引用；`scripts/relaykit-dto-compat.sh:10` 自述「Use it in CI once the relaykit module lands」。i18n 门禁（i18nLintGate）同样无 CI 接线。门禁当前无强制力。

**2.5 citation-check 单独运行时 ref 失效静默 fail-open**
`scripts/relaykit-citation-check.py:210-218`：`git ls-tree` 失败返回空列表，裸引用组从 ok=67 变 unresolved=70 但脚本退出码仍 0。缓解：gate-selfcheck 的声明计数比对（ok 67）会暴露不一致；单独运行时静默放行。

**2.6 c-symbols-check 的 --ref 参数与文件引用正则断联**
`scripts/relaykit-c-symbols-check.py:118` 硬编码 `^(upstream/main|789c97019|dbfbc096f):`，升锚时传新 --ref 会使带锚点文件引用组整组静默跳过（:189-191 continue），退出码 0。缓解：selfcheck 声明「带锚点文件引用 17 个」的计数比对兜底。

**2.7 部署库计费配置漂移持续（上轮 3.8 遗留，缺口收窄 76 -> 69 键）**
隔离库 ModelRatio 704 键 vs 源码默认 481 键（上轮口径 488，差异为 map 重复键去重，本轮以可执行导出为准）；源码有而库缺 69 键（`claude-opus-5-5`、`claude-fable-5-1`、`GPT-5.5`、`Kimi-K3` 等），缺键模型 fail-closed 拒绝（SelfUseModeEnabled=false 实测）。库比源码多 292 键为 DB 侧自定义。

**2.8 llama-3-sonar-large-32k-chat/-online 比率当前源码与库均为 0**
与上轮 3.9 记录（源码已修 1.0）矛盾：本轮 /tmp 独立模块导出源码默认值即 0。要么上轮「源码已修」指其他字段，要么被改回。0 比率在计费语义上为免费（该模型仅按次计费场景可达性待查），需业务侧确认 0 是否预期。

### P3（摘要）

- `/api/status` 匿名回显容器内部主机名 ServerAddress（上轮 3.12 遗留，未修复，实测仍在）。
- 旧式 OAuth 回调（github/oidc/discord/linuxdo）从不消费 oauth_state nonce，会话存活期可重放；`validateOAuthState` 为无调用方死代码（controller/oauth_state.go 补提交只统一了校验，未接线消费）。
- CAS 2FA 挑战路径 state 已消费但 ticket 释放（上轮 3.10，注释明示有意设计，遗留）。
- Rankings 路由可达（App.jsx + pages/Rankings 339 行已恢复）但 classic 顶栏/边栏无导航入口，用户不可发现（恢复名单内 headerbar/Navigation 未恢复）。
- 容器无 CPU/内存/Pids 限制、root 运行（隔离环境取舍，遗留）。
- 应用日志目录只增不减：3534 文件 84MB（上轮 3517/82MB）；FATAL 已止于 10-13 启动竞态（PG 未就绪 57P03），未再新增。
- 容器日志每 10 分钟一条 WARN：@openai/codex npm registry 元数据超 4MB 上限刷新失败（新记录）。
- relaykit-c-symbols-check 文件引用正则不支持 :204-206 范围形态（预防性，当前 8 个 C 行无该形态）。
- gate-selfcheck 宿主声明正则硬编码 baseline=dbfbc096f，升锚需双处手改（worst case 红，不漏）。
- web/default sign-up 的 2FA 后跳转落 /dashboard，与 sign-in 携带 redirect 不对称（路由无 search schema）。
- mysql_dialector.go（untracked）静态审查无问题但未接线（model/main.go:234 仍 mysql.Open）；真实 MySQL 行为未覆盖。

## 3. 上轮缺陷闭环核对

| 上轮项 | 状态 | 依据 |
| --- | --- | --- |
| 3.1 Gemini think-tag 重复注入（P1） | 缺陷代码已不存在，且从未进入提交历史 | 主代理复核：当前 convert.go 与 52d63c24e 版本 md5 一致（23264c25…），全文无 ThinkTagsExtracted；该子系统是 untracked 在途工作，上轮针对的 dirty 快照已被并行会话替换 |
| 3.2 web/default 2FA 门控未提交 | 已修复提交（6d3343956、1d828d68f） | agent 读码 + 反向验证 |
| 3.3 NoRoute SPA 查询串 404 | 已修复并实测闭合 | agent 实测 7 项矩阵全 200；已提交回归测试 4 用例逐行核对 |
| 3.4 realtime img 缺口 | 未修复（本轮 2.3） | 主代理复核 |
| 3.5 sign-up 微信漏检 2FA | 已修复 | sign-up-form.tsx:188-193 require_2fa 分支 |
| 3.6 classic 注册页 2FA 漏检 | 已修复 | RegisterForm.jsx 4 处（微信/Telegram/浏览器 OAuth 收口/桌面轮询）+ 服务端唯一签发点收口；agent 验证调用链不可绕过 |
| 3.7 会话 Cookie Secure=false | 已修复并编入部署产物 | middleware/secure_session_cookie.go + main.go:216/220 接线；容器二进制符号命中；TLS 运行时实测未覆盖（只读红线） |
| 3.8/3.9 计费漂移 | 持续（本轮 2.7/2.8） | 只读 psql 实测 |
| 3.10-3.15 | 遗留或收窄，见 P3 | - |

## 4. 测试与构建事实（主代理独立运行）

- `go build ./...` 退出码 0。
- `go test ./relay/common -count=1` ok。relay/helper、service、controller 的失败全部位于 untracked 在途测试文件（依赖未恢复实现：addRealtimeUsage、SanitizeLogLine、abortRateLimited、temperature 校验等）；已提交代码无测试失败。
- `bun run build:fast`（web/classic）退出码 0，是恢复提交未引用缺失导出的强证据。
- web/default `bun run lint` 退出码 0。
- 两个新增 2FA 契约测试经反向验证：基线源码下 fail 1+2，HEAD 源码下 pass 3，非伪测试。
- relaykit 三套测试 108 用例全过；文档自述计数与实测一致。

## 5. 被收窄的子代理结论

- web/classic agent 报告的恢复提交「双向合并残留」担忧：build 全量通过 + 逐文件核查后未发现实际实例，仅 P1/P2 两处接线缺口为实。
- 数据库 agent 指出上轮 488 vs 本轮 481 的源码默认键数差异：源码 setting/ 自基线零提交，差异为计数口径（map 重复键去重），非漂移扩大。

## 6. 未覆盖边界

- 会话 Cookie Secure 的 TLS 运行时行为（只读红线无法成功登录观察）。
- untracked 在途实现（think-tag 接线、log sanitize、token group validation 等）的最终正确性——它们尚未定稿，本审查仅记录测试失败与缺口。
- 真实 MySQL 迁移行为（无 MySQL 实例）。
- 未认证 /api/user/groups 分组信息暴露（上轮 3.15 项）本轮未复测。

## 7. 结论

- 68 个提交的增量中，NoRoute 修复、Secure Cookie、2FA 三端修复、ThinkTagSplitter 字段恢复经逐 hunk 审查无新缺陷；上轮 6 项可修缺陷闭合 5 项。
- 最高优先级是 2.1（置顶功能点击必报错，用户可见）与 2.2（性能列恒空），两者均为事故恢复提交的半成品接线，需补齐 hook return + ChannelsTable 传参 + PricingContent/PricingView/PricingTable 透传。
- 门禁体系（relaykit、i18n）功能正确但全部游离于 CI 之外，是当前质量保障的最大结构性缺口（2.4）。
- 部署服务本身健康：32 小时零重启、零 OOM、零 error 级日志，端点与安全头全部符合预期。
