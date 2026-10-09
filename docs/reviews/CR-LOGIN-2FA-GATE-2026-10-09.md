# 登录二次验证门控统一收口

日期：2026-10-09
范围：`controller/`（登录收口、2FA、OAuth、桌面 handoff）、`web/default`、`web/classic`
状态：代码与测试已完成，未提交

## 问题与根因

审计发现「2FA 只在密码登录一条路径生效」，相对上游是退化。

根因是门控位置：`controller/user.go` 的 `Login` 在密码校验后内联判断
`model.IsTwoFAEnabled(user.Id)`，而会话签发收口 `setupLoginWithResult` 自身不检查第二因素。
其余登录出口直接调用 `setupLogin`，因此 GitHub、Discord、OIDC、LinuxDO、微信、Telegram、
Passkey、自定义 OAuth（JWT/Header/CAS）以及桌面端 handoff 都能在未提供第二因素时拿到完整会话。

证据：

- 改动前 `model.IsTwoFAEnabled` 全仓只有 1 处调用点（`controller/user.go`），
  而 `setupLogin` 有 11 处调用点。
- 上游在 `setupLogin` 内通过 `service.StartLoginVerification` 收口，本分支把这段收口丢了。

## 设计约束

- 门控必须落在唯一会话签发处，而不是在 11 个调用点各加一次判断；否则新增登录方式会再次绕过。
- 不能把「查询 2FA 记录出错」当作「未开启 2FA」放行：`IsTwoFAEnabled` 出错返回 false，
  门控改读 `model.GetTwoFAByUserId` 并在出错时拒绝并报错。
- 通过标记需要限定作用域：验证码通过后放行的必须只是刚验证的那个账号，换账号登录要重新挑战。
- 桌面 handoff 不能出现「消费了 handoff 却什么都没发生」的空转：桌面端轮询本身要能拿到挑战。

## 改动清单

- `controller/user.go`
  - `setupLoginWithResult` 改为返回 `(bool, string)`，失败原因供审计使用。
  - 未签发会话的原因取值：`twofa_required`、`session_save_failed`、`twofa_check_failed`。
  - 挑战分支写入 `pending_username` / `pending_user_id`，并丢弃上一账号遗留的通过标记。
  - `Login` 删除内联 2FA 块，统一走 `setupLogin`。
- `controller/twofa.go`
  - `Verify2FALogin` 验证码通过后设置 `twofa_verified_user_id`，由 `setupLogin` 认领。
- `controller/oauth_desktop.go`
  - `PollDesktopOAuth` 接收 `(issued, _)`，挑战响应由门控写入本次 poll。
- `controller/custom_oauth_external.go`
  - 审计 reason 由硬编码 `session_save_failed` 改为门控返回的真实原因，挑战不再被记成会话失败。
- `web/default/src/routes/oauth/$provider.tsx`
  - 回调收到 `require_2fa` 时跳 `/otp`，不再当作登录成功。
- `web/default/src/features/auth/hooks/use-oauth-login.ts`
  - GitHub/Discord/OIDC/LinuxDO 桌面分支、Telegram、自定义 OAuth 桌面分支、`trusted_header` 分支均加挑战判断。
- `web/default/src/features/auth/sign-in/components/user-auth-form.tsx`
  - 微信验证码登录、Passkey 完成响应加挑战判断。
- `web/classic/src/components/auth/LoginForm.jsx`
  - 新增 `startTwoFAChallenge`；Telegram、微信验证码、Passkey 三处挑战统一打开两步验证弹窗。
  - 消费路由 state 的 `require_2fa`（OAuth 回调页跳转而来）。
- `web/classic/src/components/auth/OAuth2Callback.jsx`
  - 回调收到 `require_2fa` 时跳 `/login` 并带上 state。

## 桌面端 handoff 的处理

桌面端 OAuth 在系统浏览器完成，回到 Tauri webview 后由 `desktop-runtime.ts` 轮询
`/api/oauth/desktop/poll`。门控放进收口后，poll 在未验证时会返回 `require_2fa`：

- 该响应写进桌面 webview 自己的会话，`pending_user_id` 落在桌面端 cookie 里。
- 桌面端 UI 与浏览器同为 `web/default` 构建（`desktop/tauri-app/scripts/build-web.mjs` 打包 `web/dist`），
  `/otp` 页面存在，因此验证码在桌面窗口内完成，不需要后端豁免。
- 代价：handoff token 在挑战时已被消费，若桌面端不处理挑战则本次登录停在此处（需重新发起）。
  这一边界在测试中固定为「poll 返回挑战，随后同会话验证码通过即签发会话」。

## 验证

新增测试 `controller/login_twofa_gate_test.go`，8 项，全部先红后绿：

```bash
go test ./controller -run 'TwoFAGate|ChallengesTwoFactor|TwoFactorVerification|TwoFactorMarker|LoginExitWithoutTwoFactor|DesktopHandoffPollChallenges|AuditsTwoFactorChallenge' -count=1
```

RED 阶段（实现前）复现的原始缺陷：

- OAuth 回调：`expected OAuth callback to challenge a two-factor account, got data: {"id":1,...}`。
- 直连出口：同样直接返回完整用户数据。
- 桌面 poll：直接返回用户数据。
- 自定义 OAuth 审计：`failure_reason=(empty)` 且登录成功。

GREEN 阶段结果：

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go build ./...` | 0 | 通过 |
| `go vet ./controller ./model` | 0 | 无输出 |
| `go test ./controller -count=1` | 0 | ok 6.882s |
| `go test ./controller -count=2 -shuffle=on` | 0 | ok 14.815s |
| `go test ./model ./relay/common ./relay/helper -count=1` | 0 | 全部 ok |
| `cd web/default && bun run lint` | 0 | 无输出 |
| `cd web/default && bun run typecheck` | 0 | `tsc -b` 无错误 |
| `cd web/default && bun run build` | 0 | Safari 兼容检查通过 |
| `cd web/classic && bun x prettier --check <改动文件>` | 0 | 通过 |
| `cd web/classic && bun x eslint <改动文件>` | 0 | 无输出 |
| `cd web/classic && bun run build` | 0 | built in 3m 11s，Safari 兼容检查通过 |

## 回归修复

门控使登录收口开始查询 `two_fas` 表，暴露两处测试夹具缺口（属本次改动的连带修复）：

- `controller/custom_oauth_jwt_test.go` 的共享夹具 `setupCustomOAuthJWTControllerTestDB`
  未迁移 `two_fas`，导致 CAS 三项测试报 `no such table: two_fas`；已把
  `model.TwoFA`、`model.TwoFABackupCode` 加入 `AutoMigrate`。
- `controller/custom_oauth_external_test.go` 的
  `TestFinalizeCustomOAuthIdentityLoginReturnsSessionSaveError` 没有数据库，
  门控查询时 `model.DB` 为空导致空指针；已补共享夹具。

## 未覆盖边界

- 既有失败 `TestSSRFGuardedTransportDoesNotPinHTTPProxyEndpoint`（`service` 包）与本轮改动无关：
  该文件与测试相对 HEAD 未改动，在干净的 `HEAD` 检出上同样失败。
- 桌面端挑战的端到端仅在 Go 侧固定（poll 返回挑战 + 同会话验证码通过）；未在真实 Tauri 应用中走一遍。
- 前端挑战分支未做浏览器端交互测试，仅有 lint/typecheck/build 与代码走查。
- 会话通过标记的生命周期等于会话 cookie；同一会话内再次登录同一账号不再重复索要验证码，
  这是有意的（与上游「会话内一次验证」一致），未做超时收窄。

## 提交状态

未提交。改动文件：

```
controller/user.go
controller/twofa.go
controller/oauth_desktop.go
controller/custom_oauth_external.go
controller/custom_oauth_jwt_test.go
controller/custom_oauth_external_test.go
controller/login_twofa_gate_test.go   (新增)
web/default/src/routes/oauth/$provider.tsx
web/default/src/features/auth/hooks/use-oauth-login.ts
web/default/src/features/auth/sign-in/components/user-auth-form.tsx
web/classic/src/components/auth/LoginForm.jsx
web/classic/src/components/auth/OAuth2Callback.jsx
docs/reviews/README.md
```

工作区另有其他会话的大量未提交改动，本记录只覆盖上述文件。
