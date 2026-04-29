# AGENTS.md — new-api 仓库约束

## 项目概览

本项目是基于 Go 的统一 AI 网关，聚合 OpenAI、Claude、Gemini、Azure、AWS Bedrock 等上游接口，对外提供统一 API、用户管理、计费、限流和管理后台。

## 技术栈

- 后端：Go 1.25+、Gin、GORM v2
- 前端：React 18、Vite、Semi Design
- 数据库：SQLite、MySQL、PostgreSQL，三者都必须兼容
- 缓存：Redis + 内存缓存
- 鉴权：JWT、Passkey、OAuth、OIDC
- 前端包管理：默认使用 `bun`

## 目录结构

项目按 `router -> controller -> service -> model` 分层。

- `router/`：路由
- `controller/`：请求处理
- `service/`：业务逻辑
- `model/`：数据模型与数据库访问
- `relay/`：上游协议适配与中继
- `middleware/`：鉴权、限流、日志、分发
- `setting/`：系统、模型、计费、性能等配置
- `common/`：共享工具
- `dto/`、`types/`、`constant/`：结构体、类型和常量
- `oauth/`：OAuth 相关实现
- `pkg/`：内部通用包
- `web/`：前端项目

## 国际化

- 后端使用 `i18n/`，当前保留 `zh`、`en`
- 前端使用 `web/src/i18n/`，当前存在多语言运行时资源
- “文档中文化”与“运行时 i18n”是两件事；移除非中文文档时，不等于移除界面多语言能力

## 规则

### 1. JSON 统一使用 `common/json.go`

业务代码中禁止直接调用 `encoding/json` 的编解码函数，统一使用：

- `common.Marshal`
- `common.Unmarshal`
- `common.UnmarshalJsonStr`
- `common.DecodeJson`
- `common.GetJsonType`

`json.RawMessage` 等类型仍可作为类型引用，但实际编解码必须走 `common.*`。

### 2. 数据库必须同时兼容 SQLite、MySQL、PostgreSQL

- 优先使用 GORM，而不是手写方言 SQL
- 不直接写 `AUTO_INCREMENT`、`SERIAL`
- 必须兼容保留字列名、布尔值差异、迁移差异
- SQLite 不支持的 `ALTER COLUMN`，必须用兼容方案
- 若无法避免原生 SQL，必须同时给出三库兼容处理

### 3. 前端默认使用 Bun

- 安装依赖：`bun install`
- 开发：`bun run dev`
- 构建：`bun run build`
- i18n 工具：`bun run i18n:*`

### 4. Web UI 改动必须服从现有系统主题风格

- 所有 Web UI、Dashboard、弹窗、表格、详情区、筛选区、表单、按钮、标签、卡片等改动，必须优先复用当前系统已有的视觉语言和交互模式
- 禁止为单个功能引入与现有系统割裂的样式体系，包括但不限于：
  - 额外的重阴影、重边框、突兀底色、过大的圆角
  - 与现有主题不一致的按钮样式、卡片样式、信息块样式
  - 与现有亮色 / 暗色主题不协调的固定色值
  - 在表格、卡片、弹窗内部再次叠加“类卡片”容器，导致层级混乱
- 优先顺序必须是：
  - 先复用 Semi Design 现有组件语义
  - 再复用本仓库现有页面的布局、间距、配色、边框、圆角、交互反馈
  - 最后才允许做最小必要的定制样式
- 如果已有上游或本仓库现成页面能承载相同交互，必须先对齐其实现方式，不得凭主观偏好另起一套视觉方案
- 任何前端样式改动都必须同时检查亮色主题、暗色主题、桌面端、移动端，不得只在单一视图下看起来正常
- 任何新增 UI 入口或详情展示，都必须先判断是否应该复用现有弹窗、抽屉、展开行、描述列表、表格工具栏等模式，不得随意发明新容器形态
- 涉及列表页、日志页、管理页时，信息层级必须保持克制：
  - 主信息使用现有表格或卡片承载
  - 次级信息使用现有展开区、弹窗或描述组件承载
  - 不得用装饰性样式抢占信息层级
- 提交前必须做视觉复核，确认“看起来像本系统原生的一部分”，而不是“新拼进去的一块”

### 5. 新渠道接入时检查 `StreamOptions`

新增渠道时，先确认上游是否支持 `StreamOptions`；若支持，需要同步加入 `streamSupportedChannels`。

### 6. 保留 AGPL 与上游来源

本仓库独立演进，但必须保留：

- 许可证文本
- 必要的版权与修改声明
- 对 `QuantumNous/new-api` 的来源说明

允许独立调整：

- README、仓库描述、Issue 模板、文档入口、发布说明、帮助链接
- 本仓库自己的项目定位、发布渠道和对外链接

默认原则：

- 保留法律与来源信息
- 允许独立产品化表达
- 不得抹去上游来源或伪造独占归属

### 7. 阶梯计费相关改动前先读 `pkg/billingexpr/expr.md`

只要涉及表达式计费、阶梯计费、工具定价、预扣费或结算链路，必须先阅读 `pkg/billingexpr/expr.md`。

涉及模型价格配置、`ModelRatio` / `CompletionRatio` / `CacheRatio` / `CreateCacheRatio`、`ModelPrice`、历史消费日志金额修正、`quota_data` 聚合修正、用户/令牌/渠道用量回算时，必须同时阅读 `docs/operations/pricing-maintenance.md`。

### 8. 中继请求 DTO 必须保留显式零值

客户端 JSON 解析后再转发给上游的可选标量字段，必须使用带 `omitempty` 的指针类型，如：

- `*int`
- `*uint`
- `*float64`
- `*bool`

语义要求：

- 未传：`nil`，序列化时省略
- 显式传 `0` / `false`：必须保留并继续向上游发送

### 9. 真实渠道验证必须使用隔离环境

- 禁止开发实例直连生产数据库或生产数据目录
- 生产渠道配置只能只读导出，再导入到隔离环境
- 必须使用独立数据库、独立运行目录、独立日志、独立端口
- 迁移、缓存重建、Token 创建、联调测试都必须在隔离环境进行

### 10. Dashboard 会话接口需要 `New-Api-User`

对于受 `middleware.UserAuth()` 保护的后台接口：

- 仅有会话 Cookie 不够
- 必须额外带上 `New-Api-User: <当前用户 ID>`
- 该值必须与当前登录用户一致，否则会返回 `401`

### 11. 不能只靠 `Content-Type` 判断是否为流式

部分上游会把普通 JSON 错误地标记成 `text/event-stream`。

处理规则：

- 非流式请求，不能只因响应头是 `text/event-stream` 就强行按 SSE 处理
- 先看请求意图，再看响应体前缀
- 去掉前导空白后，如果以 `{` 或 `[` 开头，按普通 JSON 处理
- 若以 `data:`、`event:`、`:` 开头，才按 SSE 处理

### 12. 本地与隔离运行必须使用稳定密钥

启动本地或隔离实例前，必须显式设置：

- `SESSION_SECRET`
- `CRYPTO_SECRET`

禁止依赖隐式默认值或占位字符串，否则会影响会话与加密行为，导致联调结论失真。

### 13. 本仓库独立于上游演进

- 仓库路线、优先级、发布时间由本仓库自行决定
- 上游变更是可选输入，不是默认必须同步
- 吸纳上游前，先评估对本仓库增强能力的影响
- 优先选择性吸纳，而不是整仓盲目同步
- 本项目不考虑向上游仓库提交 PR
- 本项目语境中的“提交 PR”，默认仅指向本仓库自己的 GitHub 仓库提交 PR，用于本仓库内部评审、自动化检查和机器人 review
- 未经用户明确批准，不得向 `upstream` 或任何上游仓库创建 PR、issue 或 review

重点关注的本地增强能力包括：

- 企业 SSO
- 协议转换策略
- 阶梯计费与工具定价
- 请求内容日志
- Dashboard 与 Web UI 增强

### 14. 开发环境必须分层，不得混用

- 日常主开发只使用完全隔离的开发环境
- 完全隔离环境必须独立使用自己的：
  - `new-api` 容器
  - PostgreSQL
  - Redis
  - 数据目录
  - 日志目录
  - 端口
  - `SESSION_SECRET`
  - `CRYPTO_SECRET`
- 禁止把“仅改端口”的半隔离环境当作开发主环境

### 15. 只读前端联调环境必须双重阻断

- 只读前端联调环境允许复用正式后端，只用于界面观察和样式联调
- 必须同时满足两层控制：
  - 前端请求层阻断所有非安全方法，并阻断登录、登出、绑定、OAuth/CAS 回调等高风险入口
  - 代理层再次阻断所有非安全方法和关键副作用路径
- 不得让本地前端直接绕过只读代理去请求正式后端
- 不得在只读前端联调环境中执行任何会写库、改配置、改用户状态、改渠道状态的操作

### 16. 正式 Docker 服务必须保持同一 Compose 分组

正式环境中的 `new-api`、数据库、Redis 等关联容器，必须保持在同一个 Docker Compose project 分组下，避免在 Docker Desktop 或其他管理界面中拆成独立条目。

要求如下：

- `new-api` 正式容器必须带有与同组基础设施一致的 Compose 标签
- 至少要保持：
  - `com.docker.compose.project`
  - `com.docker.compose.service`
  - `com.docker.compose.project.working_dir`
  - `com.docker.compose.project.config_files`
- 如果因宿主机路径兼容问题临时改用 `docker run` 替代 `docker compose up`，也必须补齐上述标签，确保容器仍归属同一项目组
- 升级后必须检查 Docker Desktop 中的分组展示是否正确，不能出现 `new-api` 单独游离在 `postgres`、`redis` 之外

### 17. Docker 宿主机路径必须使用显式变量

为兼容 macOS `/Volumes`、Windows、Linux、WSL，Compose 中涉及宿主机目录的绑定挂载，不要依赖相对路径的隐式解析结果。

统一要求：

- 数据目录使用 `${NEW_API_DATA_DIR:-./data}`
- 日志目录使用 `${NEW_API_LOG_DIR:-./logs}`
- Compose 中优先使用 `type: bind` 长写法

原因：

- 某些环境下，Compose 在解析 `./data` 这类相对路径时可能错误改写大小写或前缀
- 在 macOS 上，这会把 `/Volumes/...` 错误变成 `/volumes/...`，从而触发 Docker Desktop 文件共享报错

部署规则：

- macOS 正式环境必须显式设置 `NEW_API_DATA_DIR`、`NEW_API_LOG_DIR` 为真实绝对路径
- Windows / Linux / WSL 也建议显式设置，避免依赖当前工作目录

### 18. 桌面端 sidecar 必须与仓库环境隔离

Tauri 桌面端启动 `new-api` sidecar 时，必须显式隔离仓库根目录和宿主环境配置。

要求如下：

- sidecar 必须设置 `NEW_API_SKIP_DOTENV=true`，禁止读取仓库根 `.env`
- sidecar 必须设置独立工作目录为桌面应用数据目录，而不是仓库根目录
- 桌面本地数据必须默认走独立 SQLite：
  - `SQL_DSN=local`
  - `SQLITE_PATH=<应用数据目录>/data/new-api.db`
  - `LOG_SQL_DSN=`
  - `REDIS_CONN_STRING=`
- 桌面端必须注入稳定的 `SESSION_SECRET` 和 `CRYPTO_SECRET`
- 桌面端不得继承正式 Docker、开发环境或仓库 `.env` 中的数据库、Redis、端口等配置
- 本地直接运行 `target/release/new-api-tauri-desktop` 前，必须确保 sidecar 已同步到同一 `target/release` 目录，避免“桌面壳是新版本、sidecar 仍是旧版本”的假象

### 19. 桌面端 OAuth handoff 多实例部署必须使用共享状态

- 桌面端 OAuth handoff 包含三段请求：
  - `/api/oauth/desktop/start`
  - 第三方 OAuth callback
  - `/api/oauth/desktop/poll`
- 这三段请求在多实例、Docker、Kubernetes、负载均衡场景下不能假设会落到同一个进程
- 因此：
  - 单实例本地开发可使用进程内 memory store
  - 多实例部署必须启用 Redis 作为共享状态面
- `/api/oauth/desktop/poll` 是短周期轮询接口，不能复用 `CriticalRateLimit()` 这类低频关键接口限流；必须使用按 `handoff_token` 分桶的独立限流，避免正常授权等待期间被误伤
- 不允许把 sticky session 当作长期架构前提
- Redis 不可用时必须显式暴露失败，不能伪造完成态或静默降级成“看起来成功但 poll 永远不完成”

### 20. Header Profile 必须作为完整请求头主对象管理

- 渠道请求头配置必须以完整 `Header Profile` 为主对象管理
- `User-Agent` 只是 `Header Profile.headers` 中的一个字段，不能再单独设计独立策略体系
- 渠道侧只保存 `settings.header_profile_strategy` 这类策略引用，不在渠道表单中重新维护另一套零散请求头模板状态
- 为保证渠道全局配置在不同请求用户、不同管理员编辑场景下都可复现，`settings.header_profile_strategy` 可以保存已选 Profile 的运行时快照；该快照只能来自完整 `Header Profile`，不能演变成渠道表单里的临时请求头编辑区
- 旧 `header_override` 仅允许显式导入为 `Header Profile`，禁止静默写库迁移或自动覆盖用户现有策略
- AI Coding CLI 预置 Profile 只是固定请求头快照；遇到 Codex / Claude 官方客户端限制、会话追踪或 SDK 元数据校验时，必须通过 `param_override.operations[].mode=pass_headers` 透传真实客户端动态请求头，不能用固定 UA 模板伪造
