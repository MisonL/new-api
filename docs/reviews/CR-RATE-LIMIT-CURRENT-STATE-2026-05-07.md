# CR-RATE-LIMIT-CURRENT-STATE-2026-05-07

## 审计范围

本记录只梳理当前限流实现事实，作为后续优化基线，不包含代码修改。

读取范围：

- `middleware/rate-limit.go`
- `middleware/model-rate-limit.go`
- `middleware/email-verification-rate-limit.go`
- `router/api-router.go`
- `router/relay-router.go`
- `router/web-router.go`
- `router/dashboard.go`
- `common/constants.go`
- `common/init.go`
- `setting/rate_limit.go`
- `common/limiter/limiter.go`
- `web/classic/src/components/settings/RateLimitSetting.jsx`
- `web/default/src/features/system-settings/request-limits/*`

## 总体分层

当前系统存在三类主要限流面：

1. 页面与管理 API 的通用限流
2. 登录、支付、密钥查看、礼品码等关键动作限流
3. 模型中继请求限流

此外还有邮箱验证码、桌面 OAuth 轮询、搜索建议、上传下载等专项限流。

## 存储后端

通用限流会根据 `common.RedisEnabled` 自动选择 Redis 或进程内内存：

- Redis 启用时，使用 Redis list 保存请求时间戳。
- Redis 未启用时，使用 `common.InMemoryRateLimiter`。
- 内存限流是单进程级别，多实例部署时各进程独立计数。
- 通用 Redis key 默认过期时间为 `common.RateLimitKeyExpirationDuration`，当前常量为 20 分钟。

模型请求总量限流在 Redis 模式下还会使用 `common/limiter` 的 Lua token bucket。

## 默认配置

来自 `common/init.go` 与 `common/constants.go`：

| 类型 | 默认开关 | 默认次数 | 默认窗口 | key 口径 |
| --- | --- | ---: | ---: | --- |
| Global API | 开 | 180 | 180 秒 | IP |
| Global Web | 开 | 60 | 180 秒 | IP |
| Critical | 开 | 20 | 1200 秒 | IP |
| Desktop OAuth poll | 开 | 180 | 180 秒 | `handoff_token`，无效时回退 IP |
| Search | 开 | 10 | 60 秒 | 用户 ID |
| Suggestion | 开 | 60 | 60 秒 | 用户 ID |
| Upload | 固定启用 | 10 | 60 秒 | IP |
| Download | 固定启用 | 10 | 60 秒 | IP |
| Email verification | 固定启用 | 2 | 30 秒 | IP |

相关环境变量：

- `GLOBAL_API_RATE_LIMIT_ENABLE`
- `GLOBAL_API_RATE_LIMIT`
- `GLOBAL_API_RATE_LIMIT_DURATION`
- `GLOBAL_WEB_RATE_LIMIT_ENABLE`
- `GLOBAL_WEB_RATE_LIMIT`
- `GLOBAL_WEB_RATE_LIMIT_DURATION`
- `CRITICAL_RATE_LIMIT_ENABLE`
- `CRITICAL_RATE_LIMIT`
- `CRITICAL_RATE_LIMIT_DURATION`
- `DESKTOP_OAUTH_POLL_RATE_LIMIT_ENABLE`
- `DESKTOP_OAUTH_POLL_RATE_LIMIT`
- `DESKTOP_OAUTH_POLL_RATE_LIMIT_DURATION`
- `SEARCH_RATE_LIMIT_ENABLE`
- `SEARCH_RATE_LIMIT`
- `SEARCH_RATE_LIMIT_DURATION`
- `SUGGESTION_RATE_LIMIT_ENABLE`
- `SUGGESTION_RATE_LIMIT`
- `SUGGESTION_RATE_LIMIT_DURATION`

## 通用页面与 API 限流

`router/web-router.go` 在全站静态页面层挂载：

- `middleware.GlobalWebRateLimit()`

`router/api-router.go` 在 `/api` 分组挂载：

- `middleware.GlobalAPIRateLimit()`

`router/dashboard.go` 在旧 dashboard API 分组挂载：

- `middleware.GlobalAPIRateLimit()`

这三类都走 `rateLimitFactory`，当前 key 由 `c.ClientIP()` 组成。

## CriticalRateLimit 覆盖范围

`CriticalRateLimit()` 当前也是按 IP 计数。它覆盖高风险或高成本接口，包括但不限于：

- 注册、登录、2FA、Passkey 登录
- 重置密码、OAuth state、OAuth start/callback、外部 JWT/header/CAS 登录
- 在线充值、支付发起、订阅支付发起
- 安全验证
- 礼品码创建与领取
- token key 查看、批量 key 查看
- channel key 查看
- `/api/usage` 和 `/api/log/token` 只读 token 查询分组

礼品码相关当前状态：

- `POST /api/user/gift_codes` 使用 `CriticalRateLimit()`，按 IP。
- `GET /api/user/gift_codes/:code` 使用 `SearchRateLimit()`，按用户 ID。
- `POST /api/user/gift_codes/:code/receive` 使用 `CriticalRateLimit()`，按 IP。

## 搜索与建议限流

`SearchRateLimit()` 和 `SuggestionRateLimit()` 走 `userRateLimitFactory`，必须放在认证之后使用。

当前 key：

- Redis: `rateLimit:<mark>:user:<userId>`
- Memory: `<mark>:user:<userId>`

当前挂载点包括：

- token 搜索
- 用户日志搜索
- 管理员日志建议
- 用户日志建议
- Midjourney 日志建议
- Task 日志建议
- 礼品码查看

这一类已经是按用户计数，不受同一出口 IP 下多个用户互相影响。

## 桌面 OAuth 轮询限流

`DesktopOAuthPollRateLimit()` 专门用于：

- `GET /api/oauth/desktop/poll`

key 优先使用 `handoff_token`：

- token 长度范围为 1 到 128。
- 允许字符为字母、数字、`-`、`_`。
- token 为空或非法时，回退到 IP。

该中间件还会设置：

- `desktop_oauth_poll_rate_limited = true`

这是当前代码里较明确的“按业务对象限流”实现。

## 邮箱验证码限流

`EmailVerificationRateLimit()` 当前用于：

- `GET /api/verification`

当前规则：

- 30 秒内最多 2 次。
- Redis key: `emailVerification:EV:<clientIP>`。
- Redis 出错时会回退到内存限流。
- 内存 key: `EV:<clientIP>`。

该路径还叠加了 `TurnstileCheck()`。

## 模型请求限流

模型中继请求使用独立的 `ModelRequestRateLimit()`，挂载在：

- `/v1` relay 分组
- `/v1beta` Gemini relay 分组

执行顺序：

1. `SystemPerformanceCheck()`
2. `TokenAuth()`
3. `ModelRequestRateLimit()`
4. `Distribute()`

默认配置来自 `setting/rate_limit.go`：

| 配置 | 默认值 | 含义 |
| --- | ---: | --- |
| `ModelRequestRateLimitEnabled` | false | 默认关闭模型请求限流 |
| `ModelRequestRateLimitDurationMinutes` | 1 | 窗口分钟数 |
| `ModelRequestRateLimitCount` | 0 | 总请求数限制，0 表示不限制 |
| `ModelRequestRateLimitSuccessCount` | 1000 | 成功请求数限制 |
| `ModelRequestRateLimitGroup` | `{}` | 按分组覆盖 `[总请求数, 成功请求数]` |

分组覆盖逻辑：

- 先取 token group。
- token group 为空时取 user group。
- 若 `ModelRequestRateLimitGroup` 命中当前 group，则覆盖全局的 total/success 配置。

Redis 模式下：

- 成功请求限制使用 Redis list，只有响应状态码小于 400 时记录成功。
- 总请求限制使用 Redis Lua token bucket。
- 总请求 key 为 `rateLimit:<userId>`。
- 成功请求 key 为 `rateLimit:MRRLS:<userId>`。

内存模式下：

- 总请求 key 为 `MRRL<userId>`。
- 成功请求实际记录 key 为 `MRRLS<userId>`。
- 当前实现会先用 `MRRLS<userId>_check` 做检查，再在成功后写入实际 key。

## 管理界面

模型请求限流有管理界面：

- classic: `web/classic/src/components/settings/RateLimitSetting.jsx`
- classic 表单: `web/classic/src/pages/Setting/RateLimit/SettingsRequestRateLimit.jsx`
- default: `web/default/src/features/system-settings/request-limits/*`

后端更新 `ModelRequestRateLimitGroup` 时会经过：

- `controller/option.go`
- `setting.CheckModelRequestRateLimitGroup`

校验约束：

- group 覆盖 JSON 必须能解析为 `map[string][2]int`。
- 总请求数不能为负。
- 成功请求数必须大于等于 1。
- 两个值都不能超过 `math.MaxInt32`。

## 已知现状与后续优化点

这里仅记录，不在本轮修复。

1. `CriticalRateLimit()` 仍按 IP 计数。对已登录关键动作，按用户 ID 会更符合业务语义，也能减少代理出口共享 IP 的误伤。
2. `GlobalAPIRateLimit()` 挂在 `/api` 总入口，所有 `/api` 请求共享 IP 窗口。高频只读接口与写操作目前共用同一总入口限流。
3. `EmailVerificationRateLimit()` Redis 出错时会静默回退到内存限流。这个行为是可用性优先，但多实例下会弱化一致性。
4. `ModelRequestRateLimit()` 默认关闭。是否启用取决于系统配置，不是环境变量初始化项。
5. 模型请求内存模式的成功请求检查使用 `_check` 临时 key，和 Redis 模式的“只统计真实成功请求”语义不完全一致，后续需要单独验证和统一。
6. `SearchRateLimit()` 与 `SuggestionRateLimit()` 已经按用户 ID，实现方向可作为已登录业务接口优化参考。
7. `DesktopOAuthPollRateLimit()` 已经按 `handoff_token` 分桶，是业务对象限流的现有样板。

## 本轮命令记录

```bash
nl -ba middleware/rate-limit.go | sed -n '1,340p'
nl -ba middleware/model-rate-limit.go | sed -n '1,240p'
nl -ba middleware/email-verification-rate-limit.go | sed -n '1,150p'
nl -ba router/api-router.go | sed -n '1,120p'
nl -ba router/api-router.go | sed -n '160,305p'
nl -ba router/api-router.go | sed -n '315,375p'
nl -ba router/relay-router.go | sed -n '55,85p'
nl -ba router/relay-router.go | sed -n '185,200p'
nl -ba router/web-router.go | sed -n '1,45p'
nl -ba router/dashboard.go | sed -n '1,40p'
nl -ba common/constants.go | sed -n '180,235p'
nl -ba common/init.go | sed -n '120,155p'
nl -ba setting/rate_limit.go | sed -n '1,95p'
nl -ba common/limiter/limiter.go | sed -n '1,110p'
rg -n "RateLimit\\(|RateLimitEnable|RATE_LIMIT|rateLimit" router middleware common deploy docs -S
rg -n "ModelRequestRateLimit|GlobalApiRateLimit|CriticalRateLimit|SearchRateLimit|SuggestionRateLimit|DesktopOAuthPollRateLimit|RateLimit" web/default web/classic controller/option.go setting -S
```

## 本轮验证状态

本轮是只读梳理加文档记录，没有修改限流业务代码，也没有运行后端测试。
