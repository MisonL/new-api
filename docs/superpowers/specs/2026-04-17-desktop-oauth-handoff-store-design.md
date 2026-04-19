# Desktop OAuth Handoff Store Design

## 控制合同

- **Primary Setpoint**：桌面端 OAuth login / bind 在单实例、Docker 多实例、Kubernetes 多副本场景下都能完成 handoff，不依赖 sticky session。
- **Acceptance**：内存 store 单测保持现有生命周期行为；Redis store 能跨进程共享 `state -> handoff` 与 `handoff -> request` 状态；OAuth callback 与 poll 落到不同实例时仍可完成。
- **Guardrail Metrics**：普通 Web OAuth 行为不变；桌面 `login` 不受系统浏览器已有会话影响；桌面 `bind` 只绑定 start 阶段记录的桌面用户；失败路径必须显式返回错误。
- **Sampling Plan**：先跑 controller 级单测，再跑全量 Go 测试；开发环境 Redis 启用后做一次真实 login / bind handoff 验证。
- **Known Delays / Delay Budget**：OAuth handoff TTL 为 10 分钟；Redis 网络延迟只影响桌面 OAuth，不应阻塞普通 Web OAuth。
- **Recovery Target**：若 Redis store 失败，停止该次桌面 OAuth 并返回显式错误；不做静默成功或假完成。
- **Rollback Trigger**：普通 Web OAuth 回归、桌面 login/bind 状态错绑、Redis 不可用时产生假成功，任一出现即回滚本轮改动。
- **Constraints**：不新增数据库表；不改变 OAuth Provider 接口；不改前端交互协议；JSON 编解码统一使用 `common.*`。
- **Boundary**：允许触碰 `controller/oauth_desktop*`、`controller/oauth.go` 的 store 调用、环境模板和架构文档；不触碰生产 Docker 配置和真实渠道配置。
- **Coupling Notes**：状态面从进程内存迁移到可选 Redis；控制面只增加 store 选择策略，不改变路由和 provider 协议。

## 第一性原理

桌面端 OAuth handoff 的不可妥协事实是：`start`、OAuth `callback`、`poll` 三个 HTTP 请求必须共享同一份短生命周期状态。单进程内存只能保证同一进程可见，不能保证多实例可见。因此多实例部署下，状态必须进入所有实例共同可访问的状态面。

该状态不需要长期持久化，不应进入业务数据库。它需要 TTL、按 handoff token 一次性消费、按 state 查找 callback、错误和成功结果可被 poll 读取。这些特征更接近短期共享状态，Redis 是合适的主实现。

## 状态机

- `pending`：`start` 创建请求，写入 provider、mode、state、handoff token、bind user、affiliate code、created at。
- `completed`：callback 成功后写入 result user id 与 completed at。
- `failed`：callback 失败后写入 error message 与 completed at。
- `consumed`：poll 读取 completed 或 failed 后删除 handoff 主记录和 state 索引。
- `expired`：TTL 到期后自动不可见。memory store 采用访问前惰性清理策略（`Create`、`GetByState`、`GetByHandoff`、`Complete`、`Fail`、`Consume` 入口统一触发 `cleanupExpiredLocked`）。

## 存储接口

统一接口由 controller 内部使用：

- `Create(request) error`
- `GetByState(state) (*desktopOAuthRequest, bool)`
- `GetByHandoff(handoffToken) (*desktopOAuthRequest, bool)`
- `Complete(state, resultUserID) error`
- `Fail(state, message) error`
- `Consume(handoffToken) (*desktopOAuthRequest, bool)`

## 后端策略

默认使用 `auto` 策略：

- Redis 已启用且客户端可用时，使用 Redis store。
- Redis 未启用时，使用 memory store。
- 若后续加入显式 `DESKTOP_OAUTH_STORE=redis`，Redis 不可用必须显式失败，不允许静默回退。

本轮先实现自动选择，避免引入不必要的配置面；文档明确多实例部署必须启用 Redis。

## Redis Key 设计

- 主记录：`new-api:desktop_oauth:v1:handoff:<handoff_token>`，值为 `desktopOAuthRequest` JSON。
- 索引：`new-api:desktop_oauth:v1:state:<state>`，值为 handoff token。
- TTL：两类 key 均使用 `desktopOAuthTTL`。
- `Consume`：使用 Lua 在 Redis 内原子读取主记录、删除主记录、删除 state 索引，并返回主记录 JSON。
- Lua 原子消费边缘处理口径：
  - 主记录不存在：返回不存在，不执行额外删除。
  - 主记录存在但 state 索引缺失：仍删除主记录并返回数据。
  - state 索引存在但主记录缺失：清理残留 state 索引并返回不存在。

## 复杂性转移账本

| 字段 | 内容 |
| --- | --- |
| 复杂性原位置 | 单进程内存 map 与同实例请求路由假设 |
| 新位置 | Redis 短生命周期共享状态 |
| 收益 | 多实例、Docker/K8s、无 sticky session 情况下 OAuth handoff 可闭环 |
| 新成本 | Redis 可用性、TTL、原子消费、key 命名和错误观测 |
| 失效模式 | Redis 抖动导致桌面 OAuth handoff 显式失败 |

## 验证矩阵

- 内存 store：创建、按 state 查询、按 handoff 查询、成功完成、失败完成、消费删除、过期清理。
- Redis store：key 编码、JSON 编解码、成功完成、失败完成、原子消费，以及 Lua 原子消费边缘情况（主缺失、索引缺失、索引残留）；无 Redis 环境时不宣称真实 Redis 已通过。
- OAuth 主链：provider 错误写入 failed；login 忽略系统浏览器 session；bind 使用 start 阶段记录的 bind user。
- 回归：普通 Web OAuth state 校验和 bind 分支不变。
