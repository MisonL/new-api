# 企业统一认证三阶段开发规划

## 1. 文档目标

本规划用于指导 `new-api` 在当前认证体系基础上，分阶段增加企业统一认证接入能力，并记录截至 2026-04-07 的代码实现对齐情况。

约束如下：

- 不破坏现有密码登录、OAuth/OIDC、Passkey、Telegram/WeChat 登录链路
- 不改变现有 API token 与模型中继主链
- 所有新能力默认关闭
- 每个阶段单独分支、单独验证、单独收口
- 文档结论以分支代码事实、测试文件和路由实现为准，不以原始设想为准

## 2. 现状建模

### 2.1 当前系统已存在的认证基础

- 会话登录出口在 `controller/user.go` 的 `setupLogin`
- 标准 OAuth 回调主链在 `controller/oauth.go`
- 自定义 Provider 配置中心在 `model/custom_oauth_provider.go`
- 外部身份绑定关系表在 `model/user_oauth_binding.go`
- 自定义 Provider 注册与判别在 `oauth/registry.go`
- 企业认证共享登录收口当前落在 `controller/custom_oauth_external.go`
- CAS 浏览器链路当前落在 `controller/custom_oauth_cas.go`

### 2.2 当前系统真实能力边界

当前 `main` 分支已经合入企业统一认证三阶段主能力。

当前 `main` 已具备：

- `jwt_direct`：企业 JWT / ticket exchange / userinfo 登录
- `trusted_header`：可信反向代理 Header 登录
- `cas`：独立 CAS provider kind，独立后端 `/cas/start` 与 `/cas/callback` 浏览器链路

当前 `main` 仍未具备：单点登出全链路、自动创建 group、外部身份直接映射 `root`、在中继主链上做隐式 Header 自动登录。

### 2.3 第一性原理拆解

这个需求本质上不是“多一个登录按钮”，而是新增一条受控身份链路：

1. 外部身份输入
2. 身份真实性校验
3. 身份字段归一化
4. 用户绑定 / 自动注册 / 邮箱合并
5. 权限映射
6. 会话建立
7. 审计记录

当前代码没有按原方案引入 `ExternalIdentitySource`、`ExternalIdentityResolver` 这类独立类型，但已经通过以下公共收口实现了同一控制目标：

- `controller/custom_oauth_external.go`
- `oauth/external_identity_mapping.go`
- `model/user_oauth_binding.go`

## 3. CSE 总体控制合同

### 3.1 Primary Setpoint

在不破坏现有登录能力的前提下，使合法企业身份可登录、非法企业身份 fail-closed、权限映射可审计。

### 3.2 Acceptance

- 现有密码登录链路可继续工作
- 现有 OAuth/OIDC 登录链路可继续工作
- 新增外部身份链路具备控制器测试与路由测试
- 非法身份、伪造身份、冲突绑定、非法提权场景被拒绝
- 管理端配置错误时显式报错，不做静默降级

### 3.3 Guardrails

- 不允许外部身份直接映射为 `root`
- 未命中的 group 不允许隐式创建
- 邮箱自动合并必须显式开启
- `trusted_header` 模式必须绑定来源白名单
- `jwt_direct.ticket_validate` 失败时必须显式失败

### 3.4 Sensors

- 单元测试、Gin 集成测试、登录审计日志、绑定冲突日志、group/role 映射日志、管理配置变更验证

## 4. 总体架构决策

### 4.1 当前已落地的决策

- 没有把 `jwt_direct`、`trusted_header` 塞回 `oauth_code` 主链
- 继续复用 `CustomOAuthProvider` 作为统一配置中心
- 继续复用 `user_oauth_bindings` 作为统一绑定表
- 由 `controller/custom_oauth_external.go` 统一收口绑定、自动注册、邮箱合并、属性同步、审计
- 由 `controller/custom_oauth_cas.go` 处理 CAS 浏览器跳转、ticket 回调和登录入口

### 4.2 当前实际 Provider 分类

当前 `main` 真实存在的 `kind` 有四种：

- `oauth_code`
- `jwt_direct`
- `trusted_header`
- `cas`

### 4.3 当前实际共享复用层

当前共享复用不是独立 service 目录，而是控制器与 OAuth 侧协作：

- `completeCustomOAuthIdentityLogin` 负责统一登录收口
- `oauth/external_identity_mapping.go` 负责 group/role 映射
- `findOrCreateOAuthUserWithOptions` 负责绑定、自动注册、邮箱合并
- `recordCustomOAuthJWTAudit` 负责统一审计落日志

结论：

- 原规划的“统一身份归一化控制面”已经实现
- 但实现形态与原文档设想不同，需要以当前代码形态为准

## 5. 三阶段实施方案与实现核对

### 5.1 第一阶段：JWT 直连接入

阶段目标：JWT 验签、claim 映射、用户绑定 / 自动注册 / 邮箱合并、group / role 映射、登录会话建立。

当前分支：`feat/auth-jwt-sso-mvp-submit`

当前已实现：

- 路由 `POST /api/auth/external/:provider/jwt/login`
- `kind = jwt_direct`
- `direct_token`、`ticket_exchange`、`ticket_validate` 三种令牌获取模式
- `claims`、`userinfo` 两种身份解析模式
- `issuer`、`audience`、`jwks_url`、`public_key`、`jwt_source` 等配置
- 自动注册、邮箱合并、group/role 映射、登录属性同步
- 统一审计日志，包含 `provider_kind`、`auto_register`、`email_merge`、`group_result`、`role_result`、`failure_reason`
- 管理端配置页与前端回调页

相对原始规划的变化：

- 该阶段已超出原始 MVP，不再只是“直接 JWT 验签”
- `ticket_exchange` 与 `userinfo` 能力已经在该阶段一并进入 `jwt_direct`

仍未覆盖的原始扩展目标：单点登出、组织树同步。

### 5.2 第二阶段：可信 Header SSO

阶段目标：信任上游代理透传身份头部，基于可信来源白名单建立本地会话。

当前分支：`feat/auth-trusted-header-sso`

当前已实现：

- 路由 `POST /api/auth/external/:provider/header/login`
- `kind = trusted_header`
- `trusted_proxy_cidrs` 必填且必须是有效 JSON 数组
- `external_id_header` 必填
- `username_header`、`display_name_header`、`email_header`、`group_header`、`role_header`
- 重复 Header 值拒绝
- 非可信来源拒绝
- 自动注册、邮箱合并、group/role 映射、属性同步、统一审计
- 管理端按 `kind` 动态显示字段

与原规划的一致项：

- 仅提供显式登录入口
- 没有把 Header 自动登录塞进业务/中继主链
- 共享统一绑定和登录收口

当前未做：隐式 Header 自动登录。

### 5.3 第三阶段：CAS 协议接入

原规划目标：独立 `cas` provider kind、独立 `/cas/start` 与 `/cas/callback`、独立协议层处理浏览器跳转、ticket 校验、service URL 严格匹配。

当前 `main`：已合入独立 CAS provider 实现

当前已实现：

- 新增独立 `kind = cas`
- 新增后端 `/api/auth/external/:slug/cas/start`
- 新增后端 `/api/auth/external/:slug/cas/callback`
- 独立 `oauth/cas.go` 协议层处理浏览器跳转、ticket 校验和身份归一化
- 支持 CAS `serviceValidate` 风格 XML 响应解析
- 支持显式 `service_url` 覆盖与 state 合并
- CAS 返回属性继续走统一外部身份映射、绑定、自动注册、group/role 投影、审计

相对原规划的偏差：

- 没有做单点登出全链路
- 没有自动创建 group
- 仍然禁止外部身份直接映射 `root`

当前仍缺口：应用侧未看到显式 ticket 重放防护、单点登出。

结论：

- 第三阶段的“能力目标”已在 `main` 落地
- 第三阶段的“实现形态”已经升级为独立 `cas` provider 和独立浏览器链路
- 后续若必须补齐单点登出或 ticket 重放防护，应再追加独立任务

## 6. 跨阶段统一设计要求

### 6.1 审计要求

当前公共审计日志已经覆盖：

- `provider_slug`
- `provider_kind`
- `action`
- `external_id`
- `target_user_id`
- `auto_register`
- `email_merge`
- `group_result`
- `role_result`
- `failure_reason`

### 6.2 权限安全红线

- 外部身份不能直升 `root`
- group 只能映射到现有可用组
- 邮箱合并默认关闭
- Header 模式必须要求可信来源
- 所有配置错误默认 fail-closed

### 6.3 UI 与配置面

当前已按 `kind` 复用统一 Provider 管理页：`oauth_code`、`jwt_direct`、`trusted_header`、`cas`。

### 6.4 当前明确不做

- 单点登出全链路
- 多租户企业组织树同步
- 自动创建 group
- claim 到 `root` 的映射
- 在中继请求链路上做隐式 Header 自动登录

## 7. 分支策略

建议使用三个正式分支：

- `feat/auth-jwt-sso-mvp-submit`
- `feat/auth-trusted-header-sso`
- `feat/auth-cas-sso`

控制约束补充：

- 三个阶段分支都必须保持可直接合并 `main` 的拓扑条件
- 判断标准不是 review 视图是否最小，而是 `git merge-base main <branch>` 必须稳定指向同一 `main` 基线
- 阶段二、阶段三允许在本地开发时从上一阶段头部继续演进，但远端正式分支必须保证相对 `main` 可单独发起 PR
- stacked review 只能作为 fork 内的辅助审阅手段，不能替代“可直接合并 `main`”这个主约束
- 任何阶段分支都不应把“上一阶段分支必须先合入 upstream”作为唯一前提条件

### 7.1 当前分支状态快照

按 2026-04-07 的核对结果：

- 第一阶段正式分支为 `feat/auth-jwt-sso-mvp-submit`
- 第二阶段正式分支为 `feat/auth-trusted-header-sso`
- 第三阶段正式分支为 `feat/auth-cas-sso`

这三个分支当前都以同一个 `main` merge-base 为基线：`c9611c493f3112448331b43cf007ae1fc75217f5`。

补充说明：

- 第二阶段、第三阶段当前仍是累计式阶段分支，而不是“只含本阶段新增差异”的纯增量审阅分支
- 这不影响它们独立相对 `main` 发起 PR
- 如果后续需要“每个阶段 PR 只展示本阶段增量”，应拆共享基础层，而不是修改这里的主分支约束

## 8. 最终结论

对 `new-api` 而言，三阶段企业认证方案当前状态应这样理解：

- 第一阶段 JWT Direct 已实现，且能力已超过原始 MVP
- 第二阶段 Trusted Header 已按安全边界要求实现
- 第三阶段 CAS 能力已实现，且当前 `main` 已落成独立 `cas` provider、独立 `/cas/start` 与 `/cas/callback`

因此，后续评审和合并时应以“当前代码事实”而不是“最初设想形态”作为判断依据：

- 认可统一控制面已经落在现有控制器与绑定模型上
- 认可 CAS 当前是独立 provider，而不是 `jwt_direct` 的一个获取模式
- 若未来必须补齐单点登出或更严格的防重放控制，再新增独立任务，不回头篡改当前实现事实
