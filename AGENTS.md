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

### 4. 新渠道接入时检查 `StreamOptions`

新增渠道时，先确认上游是否支持 `StreamOptions`；若支持，需要同步加入 `streamSupportedChannels`。

### 5. 保留 AGPL 与上游来源

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

### 6. 阶梯计费相关改动前先读 `pkg/billingexpr/expr.md`

只要涉及表达式计费、阶梯计费、工具定价、预扣费或结算链路，必须先阅读 `pkg/billingexpr/expr.md`。

### 7. 中继请求 DTO 必须保留显式零值

客户端 JSON 解析后再转发给上游的可选标量字段，必须使用带 `omitempty` 的指针类型，如：

- `*int`
- `*uint`
- `*float64`
- `*bool`

语义要求：

- 未传：`nil`，序列化时省略
- 显式传 `0` / `false`：必须保留并继续向上游发送

### 8. 真实渠道验证必须使用隔离环境

- 禁止开发实例直连生产数据库或生产数据目录
- 生产渠道配置只能只读导出，再导入到隔离环境
- 必须使用独立数据库、独立运行目录、独立日志、独立端口
- 迁移、缓存重建、Token 创建、联调测试都必须在隔离环境进行

### 9. Dashboard 会话接口需要 `New-Api-User`

对于受 `middleware.UserAuth()` 保护的后台接口：

- 仅有会话 Cookie 不够
- 必须额外带上 `New-Api-User: <当前用户 ID>`
- 该值必须与当前登录用户一致，否则会返回 `401`

### 10. 不能只靠 `Content-Type` 判断是否为流式

部分上游会把普通 JSON 错误地标记成 `text/event-stream`。

处理规则：

- 非流式请求，不能只因响应头是 `text/event-stream` 就强行按 SSE 处理
- 先看请求意图，再看响应体前缀
- 去掉前导空白后，如果以 `{` 或 `[` 开头，按普通 JSON 处理
- 若以 `data:`、`event:`、`:` 开头，才按 SSE 处理

### 11. 本地与隔离运行必须使用稳定密钥

启动本地或隔离实例前，必须显式设置：

- `SESSION_SECRET`
- `CRYPTO_SECRET`

禁止依赖隐式默认值或占位字符串，否则会影响会话与加密行为，导致联调结论失真。

### 12. 本仓库独立于上游演进

- 仓库路线、优先级、发布时间由本仓库自行决定
- 上游变更是可选输入，不是默认必须同步
- 吸纳上游前，先评估对本仓库增强能力的影响
- 优先选择性吸纳，而不是整仓盲目同步

重点关注的本地增强能力包括：

- 企业 SSO
- 协议转换策略
- 阶梯计费与工具定价
- 请求内容日志
- Dashboard 与 Web UI 增强
