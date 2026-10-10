# 全维度审查问题的修复记录

日期：2026-10-11
范围：`docs/reviews/CR-FULL-AUDIT-LATEST-SOURCE-AND-DEV-SERVICE-2026-10-10.md` 所列问题的处理
方法：逐条 TDD（先红后绿）修复，能干净提交的按原子任务提交；与他人未提交改动交错的文件保留在工作区

## 已提交的修复

### 1. NoRoute 静态扩展名误判（P2）

提交 `d801c275e`。`router/web-router.go` 的 `shouldReturnRelayNotFound` 把含查询串的原始
`RequestURI` 直接交给静态扩展名判定，导致 SPA 路由带 `?x=.png` 时返回 404 JSON。

修复：classify 前用 `stripQuery` 去掉查询串与 fragment，扩展名判定只看到路径分量；`/v1`、`/api`
前缀判定不受影响（查询串不会出现在路径之前）。

新增 5 条用例（3 条先红后绿），并保留 `/logo.png?v=2` 这类带 cache-busting 查询的根静态文件仍
判 404 的行为。

### 2. classic 注册页 2FA 缺口（P3，部署中实际活着）

提交 `efedd863d`。`web/classic/src/components/auth/RegisterForm.jsx` 三个登录出口
（微信验证码、Telegram、浏览器/桌面 OAuth 结果）把 `{require_2fa:true}` 当登录成功。

部署核对：隔离服务 3001 实际加载 classic 主题，部署产物 `RegisterForm-*.js` 中 `require_2fa`
计数为 0（LoginForm 为 6、OAuth2Callback 为 2），即该缺口在部署中是活的。

修复：三处出口补 `require_2fa` 分支，命中时打开 `TwoFAVerification` 弹窗，验证通过后完成登录。
新增 `web/tests/classicRegisterTwoFactor.test.mjs`（先红后绿）。

### 3. web/default 注册页微信登录 2FA 缺口（P3）

提交 `1d828d68f`。`sign-up-form.tsx` 的微信出口未同步 sign-in 的 `require_2fa` 分支。

修复：补分支并在命中时 `redirectTo2FA()`。新增 `web/tests/defaultSignUpTwoFactor.test.mjs`，
反向验证（去掉分支）确认转红。

### 4. 会话 Cookie 未设 Secure（P3）

提交 `74748f7f0`。`main.go` 硬编码 `Secure:false`。改为按请求判定：请求直接走 TLS 或经代理
转发 `X-Forwarded-Proto: https` 时，把该请求写出的会话 Cookie 标记 Secure；纯 HTTP 部署保持
原状。新增 `middleware/secure_session_cookie.go` 与 5 条子用例。

## 已修复但保留在工作区未提交

以下两处修复已完成并通过验证，但目标文件与其他会话的在途改动交错在同一文件，无法在不带走他人
未提交工作的前提下分离提交（与 2FA 门控那轮同样的边界）。

### 5. Gemini think-tag 重复注入（P1）

`service/convert.go`（该文件同时在途含另一会话的 think-tag 特性工作，`git diff` 的多个 hunk 均
属他人）。流式与非流式各有两个独立 `if` 都向 `content.Parts` 追加同一个 reasoning part。

修复：流式合并为一个 `if` 并跳过空 reasoning；非流式把 `ExtractThinkTagsFromTextResponse`
移到 choices 循环**之前**（range 是值拷贝，在循环内提取会丢失），并删除重复分支。

验证（新增 `service/convert_gemini_think_test.go`，先红后绿）：

- 修复前：`stream: got 2 thought parts, want 1`。
- 修复后：流式单帧 1 个 thought part、后续纯文本帧不再注入空 thought part、非流式标签 reasoning
  进入 thought part 且可见文本已剥离标签。

### 6. realtime 聚合丢失子类 token（P2）

`relay/channel/openai/relay-openai.go`（该文件同时在途含他人的流式改造）。

修复：把两处聚合抽成 `addRealtimeUsage` 并补齐 `CachedCreationTokens`、输入/输出
`ImageTokens`（tiered 表达式 `BuildTieredTokenParams` 会消费这些字段）。

验证（新增 `relay/channel/openai/realtime_usage_aggregate_test.go`，先红后绿）。

### 7. OAuth handoff token / state 熵源（安全）

`common/str.go` + `common/utils.go`（两文件同时在途含他人的 `json`→`Marshal` 迁移与日志脱敏
工作）。`common.GetRandomString` 经 `lo.RandomString` 使用 `math/rand`（伪随机），而它的调用点
包含 bearer 凭证：桌面 OAuth 的 `handoff_token`（持有即得会话）与 OAuth state nonce。

修复：`GetRandomString` 改为调用新增的 `randomStringFromCrypto`（crypto/rand，同字符集、同长度、
同签名），一处覆盖全部 14 个调用点。

验证（新增 `controller/oauth_desktop_token_entropy_test.go`）：测试断言 `GetRandomString` 不再
走 `lo.RandomString`、且生成器为 crypto 源；反向验证（改回 `lo`）确认转红。

## 调查后判定「非缺陷」而回退的项

### CAS 回调在 2FA 挑战路径下的 state/ticket 不一致（原判 P3）

审查记录中列为「state 被消费但 ticket 被释放，重试语义不一致」。实施修复（让 2FA 挑战也算已消费）
后做反向验证时发现：**修复前后测试都通过**，即该不一致在 HTTP 层不可观测——首次回调已消费 state
nonce，重放时在 state 校验处就被拦下，ticket 是否释放不影响结果。

按「反向验证必须因正确原因失败」的纪律，判定该条无可观测后果，不是真缺陷：已回退
`controller/custom_oauth_external.go` 的改动并删除不区分修复的测试，避免制造伪通过。

## 未处理的项（原因）

- **authHelper 200/401 混用（P3）**：`middleware/auth.go` 多处失败用 200、多处用 401。改动会影响
  前端与既有客户端的错误处理契约，属行为变更而非缺陷修复，风险与收益需单独评估，本轮不动。
- **计费配置漂移（P3）**：部署隔离库 `ModelRatio` 缺 76 个源码默认键、`llama-3-sonar-*` 为 0。
  这是运维维护项（写库回填），不是代码缺陷；且生产库已按 `docs/operations/pricing-maintenance.md`
  手工维护。
- **日志目录无轮转（P3）**：`.dev-docker/runtime/isolated/logs` 累积 3517 文件/82MB。属隔离环境
  运维项，不影响服务正确性。
- **TokenAuthReadOnly 不校验令牌状态（P3）**、**ServerAddress 回显（P3）**、**XFF 可伪造（P3）**、
  **未认证 /api/user/groups 暴露分组（P3）**、**classic i18n 缺键（P3）**：均为已知设计边界或已
  文档化项，按审查记录的判定保留。
- **2FA 前端未提交（P1）**：`web/default/src/routes/oauth/$provider.tsx` 与 `use-oauth-login.ts`
  的 `require_2fa` 分支与其他会话的在途重写交错，仍无法分离提交（同门控那轮）。这两个文件对当前
  classic 部署无暴露面。

## 验证汇总

| 项 | 命令 | 结果 |
| --- | --- | --- |
| 构建 | `go build ./...` | 0 |
| 静态检查 | `go vet ./common ./middleware ./router ./controller` | 0 |
| 格式 | `gofmt -l`（全部改动文件） | 无输出 |
| NoRoute | `go test ./router -run TestShouldReturnRelayNotFound` | ok |
| Gemini think-tag | `go test ./service -run TestGeminiConversionEmitsSingleThoughtPart` | ok（先红后绿） |
| realtime 聚合 | `go test ./relay/channel/openai -run TestAddRealtimeUsage` | ok（先红后绿） |
| 熵源 | `go test ./controller -run TestDesktopOAuthTokensUseCryptoRandomSource` | ok（反向验证转红） |
| Cookie Secure | `go test ./middleware -run TestSecureSessionCookieMarksHTTPSRequestsSecure` | 5/5 子用例 |
| 前端契约 | `node --test web/tests/classicRegisterTwoFactor.test.mjs web/tests/defaultSignUpTwoFactor.test.mjs` | 3/3 |
| 后端回归 | `go test ./controller ./model ./relay/common ./relay/helper ./service` | 全 ok |
| 前端 lint | `cd web/default && bun run lint` | 0 |
| 前端 typecheck | `cd web/default && bun run typecheck` | 0 |
| classic lint | `bun x prettier --check` + `bun x eslint`（改动文件） | 通过 |

## 边界

- 修复 5/6/7 因文件与他人改动交错保留在工作区，未提交；其测试文件是新增文件，可单独入库。
- 未做浏览器端 2FA 交互 e2e（需 2FA 账号，建号需写库）。
- 未在真实 MySQL 上验证；本轮改动未触及 MySQL 特有路径。
