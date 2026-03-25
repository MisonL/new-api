# 企业统一认证三阶段开发规划

## 1. 文档目标

本规划用于指导 `new-api` 在当前认证体系基础上，分阶段增加企业统一认证接入能力。

约束如下：

- 不破坏现有密码登录、OAuth/OIDC、Passkey、Telegram/WeChat 登录链路
- 不改变现有 API token 与模型中继主链
- 所有新能力默认关闭
- 每个阶段单独分支、单独验证、单独收口

## 2. 现状建模

### 2.1 当前系统已存在的认证基础

- 会话登录出口在 `controller/user.go` 的 `setupLogin`
- 标准 OAuth 回调主链在 `controller/oauth.go`
- 自定义 OAuth/OIDC Provider 配置中心在 `model/custom_oauth_provider.go`
- 外部身份绑定关系表在 `model/user_oauth_binding.go`
- 内置 OIDC Provider 在 `oauth/oidc.go`
- 通用自定义 Provider 在 `oauth/generic.go`

### 2.2 当前系统真实能力边界

当前 `new-api` 已支持：

- 密码登录
- OAuth / OIDC 类登录
- 自定义 OAuth Provider
- Passkey / 2FA

当前 `new-api` 尚未正式支持：

- 企业 JWT 直验登录
- 可信 Header SSO 登录
- CAS 协议登录

### 2.3 第一性原理拆解

这个需求本质上不是“多一个登录按钮”，而是新增一条受控身份链路：

1. 外部身份输入
2. 身份真实性校验
3. 身份字段归一化
4. 用户绑定 / 自动注册 / 合并
5. 权限映射
6. 会话建立
7. 审计记录

只要第 2 到第 6 步没有统一抽象，后续 JWT、Header、CAS 三条链路就会重复造轮子，并且容易出现权限不一致。

## 3. CSE 总体控制合同

### 3.1 Primary Setpoint

在不破坏现有登录能力的前提下，使合法企业身份可登录、非法企业身份 fail-closed、权限映射可审计。

### 3.2 Acceptance

- 现有密码登录测试通过
- 现有 OAuth/OIDC 登录测试通过
- 新增外部身份链路测试通过
- 非法身份、伪造身份、冲突绑定、非法提权场景全部被拒绝
- 管理端配置错误时，系统显式报错，不做静默降级

### 3.3 Guardrails

- 不允许外部身份直接映射为 `root`
- 未命中的 group 不允许隐式创建
- 邮箱自动合并必须显式开启
- 可信 Header 模式必须绑定来源白名单
- CAS / JWT / Header 不复用脆弱的临时兼容分支

### 3.4 Sensors

- 单元测试
- Gin 集成测试
- 登录审计日志
- 绑定冲突日志
- group/role 映射日志
- 管理配置变更日志

### 3.5 Rollback Trigger

任一阶段若出现以下问题，立即停止推进并回退该阶段分支：

- 非法用户可登录
- 普通用户被错误提升为管理员
- 现有 OAuth/OIDC 登录回归
- 同一外部身份被绑定到多个本地用户
- 未通过可信代理的 Header 登录成功

## 4. 总体架构决策

### 4.1 总原则

不把三种协议硬塞进现有 `oauth.Provider`。

原因：

- `oauth.Provider` 的抽象前提是 `authorization code -> token -> userinfo`
- JWT 直验没有完整 code exchange
- Header 信任模式甚至没有 token exchange
- CAS 的核心是 ticket 校验，不是 OAuth token 流

### 4.2 推荐新抽象

新增一套统一的外部身份归一化服务层，建议拆成以下职责：

- `ExternalIdentitySource`
  - 描述身份来源类型：`jwt_direct` / `trusted_header` / `cas`
- `ExternalIdentityClaims`
  - 存放标准化后的身份字段
- `ExternalIdentityMappingConfig`
  - 存放 external id、email、username、group、role 等字段映射规则
- `ExternalIdentityResolver`
  - 负责查找绑定用户、自动注册、邮箱合并
- `ExternalIdentityRoleProjector`
  - 负责 group / role 投影与约束校验
- `ExternalIdentityLoginService`
  - 负责最终调用 `setupLogin`

### 4.3 状态面复用策略

优先复用：

- `custom_oauth_providers`
- `user_oauth_bindings`

建议演进：

- 给 `CustomOAuthProvider` 增加 `kind`
- 保留现有 `oauth_code` 模式
- 新增：
  - `jwt_direct`
  - `trusted_header`
  - `cas`

这样可复用现有 Provider 管理 UI、启停逻辑、绑定表与审计中心，而不需要新造一套管理对象。

## 5. 三阶段实施方案

## 5.1 第一阶段：JWT 直连接入

### 5.1.1 阶段目标

先交付最小可用企业 SSO MVP。

这阶段只解决：

- JWT 验签
- claim 映射
- 用户绑定 / 自动注册 / 邮箱合并
- group / role 映射
- 登录会话建立

不解决：

- CAS 跳转协议
- 网关透传 Header 信任链
- 单点登出

### 5.1.2 为什么先做 JWT

- 当前项目已存在 OIDC / OAuth 与外部身份绑定设施
- JWT 直验不依赖完整浏览器协议编排
- 改动面比 CAS 小
- 维护者更容易 review

### 5.1.3 配置模型

建议新增配置字段：

- `kind = jwt_direct`
- `issuer`
- `audience`
- `jwks_url`
- `public_key`
- `jwt_source`
- `jwt_header`
- `client_id`
- `authorization_endpoint`
- `scopes`
- `external_id_field`（实现层统一落到 `user_id_field`）
- `username_field`
- `display_name_field`
- `email_field`
- `group_field`
- `role_field`
- `auto_register`
- `auto_merge_by_email`
- `group_mapping`
- `role_mapping`
- `sync_group_on_login`
- `sync_role_on_login`
- `group_mapping_mode`
- `role_mapping_mode`
- `access_policy`
- `access_denied_message`

### 5.1.4 登录入口设计

建议独立入口，不复用 `/api/oauth/:provider`：

- `POST /api/auth/external/:slug/jwt/login`

输入载荷可以支持：

- `token`
- `id_token`

如果前端从 fragment 获取 token，则由前端显式提交给后端；后端不要依赖 fragment。

补充约束：

- `jwt_source = query / fragment` 时，可以支持浏览器登录入口
- `jwt_source = body` 时，仅作为后端接口直连模式，不作为浏览器登录入口
- 浏览器模式下，前端负责从 query / fragment 取回 token，再显式提交到 `POST /api/auth/external/:slug/jwt/login`
- 因此第一阶段虽然主入口是后端直登接口，但允许在配置完整时复用统一登录页和回调页完成浏览器链路

### 5.1.5 权限映射规则

- `external_id` 为主绑定键
- `email` 只能作为可选辅助合并键
- `role_mapping` 最大只能投影到 `admin`
- `root` 只能由本地人工授予
- `group_mapping` 只能命中现有 group
- 默认使用 `explicit_only` 映射模式，不允许 claim 直通命中本地角色或分组
- 若显式开启 `mapping_first`，仅允许在“未命中 mapping”时再尝试：
  - `group` 直通命中现有本地 group
  - `role` 直通命中 `common / admin`
- `guest` 不作为第一阶段可同步目标

### 5.1.6 有限属性联动同步补充

第一阶段补充支持受控的 `group / role` 登录时同步，但边界必须写清楚：

- 同步只发生在“外部登录链路”
- 已绑定用户登录时，可按配置同步 `group / role`
- 邮箱自动合并命中的用户登录时，可按配置同步 `group / role`
- 已登录用户执行“绑定外部身份”时，只建立绑定，不同步本地属性
- claim 缺失、映射未命中、目标非法、目标 group 不存在时，只忽略该字段，不清空本地已有值
- 角色从 `common -> admin` 时，需要补齐管理员默认侧边栏块，但不能覆盖用户已有个性化配置
- 角色降回普通用户时，不重写用户个性化设置，依赖现有权限计算与会话刷新链路即时收口

### 5.1.7 测试与验证

必须覆盖：

- issuer 不匹配
- audience 不匹配
- 签名无效
- token 过期
- 缺失 `external_id`
- 绑定已存在
- 邮箱冲突
- 普通 claim 冒充管理员
- 非法 group 注入
- 默认 `explicit_only` 下的 claim 直通被拒绝
- `mapping_first` 打开后的受控直通
- 已绑定用户登录同步 `group / role`
- 邮箱自动合并后的登录同步
- 绑定动作不改本地 `group / role`

## 5.2 第二阶段：可信 Header SSO

### 5.2.1 阶段目标

支持企业网关已完成认证，由 `new-api` 信任上游代理透传的身份头部并建立本地会话。

### 5.2.2 第一性原理

这一阶段最核心的问题不是“解析 Header”，而是“谁被信任”。

所以必须先确定：

- 来源 IP / 代理链白名单
- 允许读取的 Header 名
- 非可信来源时的拒绝逻辑

### 5.2.3 配置模型

建议新增：

- `kind = trusted_header`
- `trusted_proxy_cidrs`
- `external_id_header`
- `username_header`
- `display_name_header`
- `email_header`
- `group_header`
- `role_header`
- `auto_register`
- `auto_merge_by_email`
- `group_mapping`
- `role_mapping`

### 5.2.4 路由设计

第一版建议只做显式登录入口：

- `POST /api/auth/external/:slug/header/login`

不建议第一版就做“所有请求自动吃 header 并隐式登录”，否则认证面会和业务面强耦合，排障困难。

### 5.2.5 共享复用

Header 模式不再新建一套用户逻辑，直接复用第一阶段统一身份归一化服务层。

### 5.2.6 测试与验证

必须覆盖：

- 伪造 Header 但来源不可信
- 来源可信但缺少关键 Header
- role/group 越权注入
- 同一身份重复登录
- 与现有 session 共存行为

## 5.3 第三阶段：CAS 协议接入

### 5.3.1 阶段目标

支持真正的 CAS 登录，而不是把 CAS 伪装成 JWT/Header。

### 5.3.2 第一性原理

CAS 的核心控制对象是：

- 浏览器跳转
- service ticket
- ticket 校验
- service URL 严格匹配
- 校验后属性映射

所以 CAS 必须独立为协议层实现。

### 5.3.3 配置模型

建议新增：

- `kind = cas`
- `cas_server_url`
- `service_url`
- `validate_url`
- `renew`
- `gateway`
- `external_id_field`
- `username_field`
- `display_name_field`
- `email_field`
- `group_field`
- `role_field`
- `auto_register`
- `auto_merge_by_email`
- `group_mapping`
- `role_mapping`

### 5.3.4 路由设计

建议拆成两段：

- `GET /api/auth/external/:slug/cas/start`
- `GET /api/auth/external/:slug/cas/callback`

### 5.3.5 共享复用

CAS 成功验票后，只负责把属性抽取为标准化身份对象。

之后所有：

- 用户查找
- 自动注册
- 邮箱合并
- group/role 映射
- session 建立

全部复用阶段一公共服务层。

### 5.3.6 测试与验证

必须覆盖：

- ticket 缺失
- ticket 无效
- ticket 重放
- service mismatch
- 返回属性缺失
- group/role 映射非法

## 6. 跨阶段统一设计要求

### 6.1 审计要求

每次企业身份登录都应记录：

- provider slug
- provider kind
- external id
- target user id
- auto_register 是否触发
- email_merge 是否触发
- group 映射结果
- role 映射结果
- failure_reason

补充说明：

- 若当前请求不是“登录成功”，至少也要有系统级审计日志，不能静默失败
- 若已经定位到本地用户，则应补一条用户系统日志，便于后续按用户回溯
- 绑定成功 / 绑定失败也应纳入同一审计口径，避免认证面与绑定面割裂

### 6.2 权限安全红线

- 外部身份不能直升 `root`
- group 只能映射到现有可用组
- 邮箱合并必须默认关闭
- Header 模式必须要求可信来源
- 所有配置错误默认 fail-closed
- 已登录会话不能长期信任旧 `role / status / group`，需要在鉴权中间件按数据库真实状态刷新

### 6.3 UI 与配置面建议

建议继续复用自定义 OAuth Provider 管理页，但按 `kind` 动态显示字段。

这样好处是：

- 管理入口统一
- 数据模型统一
- 后续 review 成本更低

### 6.4 不做的事

当前三阶段都不建议顺手做：

- 单点登出全链路
- 多租户企业组织树同步
- 自动创建 group
- claim 到 root 的映射
- 在中继请求链路上做隐式 Header 自动登录

## 7. 分支策略

建议使用三个独立分支：

- `feat/auth-jwt-sso-mvp`
- `feat/auth-trusted-header-sso`
- `feat/auth-cas-sso`

本地开发顺序：

1. 先完成 JWT
2. 再完成 Header
3. 最后做 CAS

原因：

- JWT 最容易落地
- Header 安全边界更敏感，但协议复杂度较低
- CAS 协议复杂度最高，适合最后单独收口

## 8. 最终结论

对 `new-api` 而言，最优路线不是一次性做“企业统一认证大一统重构”，而是：

- 先补统一身份归一化控制面
- 再按 `JWT -> Header -> CAS` 三阶段递进
- 每阶段单独交付、单独验证、单独审查

这样最符合当前项目的现有结构、维护者 review 习惯和最小可验证变更原则。
