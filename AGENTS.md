# AGENTS.md — new-api 仓库约束

## 1. 核心原则

- 全程使用中文沟通。答案直接收束当次诉求，不附加不必要的后续建议。
- 所有结论必须基于代码事实、测试结果、日志、HTTP 响应或 git 证据，不得凭印象或推测。
- Debug-First：禁止为“先跑通”添加隐藏回退、静默容错、默认降级路径。
- 禁止通过 Mock、模板化成功输出或吞没异常伪造成功路径。
- 若确需回退机制，必须同时满足：显式开启、可关闭、文档可追溯、事先与用户确认。
- 先实现核心逻辑的行为等价，再考虑性能优化、额外重构和基准测试。

## 2. 任务工作流

### 2.1 唯一任务来源与锁定

- 当前仓库尚无稳定的 `tasks.md` 或 `issues.csv` 作为单一任务文件；默认以用户当次明确要求为唯一任务驱动源。
- 若后续引入正式任务文件，再切换为“任务文件优先，用户最新边界覆盖当前任务选择”的模式。
- 当用户指令、阶段文档与代码事实冲突时，优先级顺序为：用户最新指令 > `git log` / `git diff` 证据 > 当前代码实现 > `docs/reviews/` 与其他临时文档。
- 进入任务循环前，必须先确认用户要求与相关阶段文档认知一致，再锁定该任务。
- 每次只处理一个原子任务，执行顺序固定为：读取 -> 锁定进行中 -> 开发 -> 验证 -> 自审或回退 -> 标记完成 -> 提交 -> 取下一任务。
- 筛选规则：优先选取状态为“未开始”且标签含优先级语义的任务；若没有，则按文件顺序选取第一个“未开始”任务，并在提交说明里记录依据。
- 禁止并行开发多个任务；用户最新明确边界优先于既有计划。

### 2.2 任务文件结构规范

- 若后续引入任务文件，状态必须使用文本枚举，不使用百分比进度。
- 推荐字段至少覆盖：任务 ID、标题、内容、验收标准、审查要求、状态、标签。
- 状态只允许使用：`未开始` / `进行中` / `已完成`。
- 每条任务必须明确修改边界、验收方法和审查动作；缺一项时，先补任务定义再动代码。

### 2.3 自审与回退

- 对照任务的验收标准逐条确认。
- 对照审查要求逐项检查无遗漏。
- 运行最小相关测试、构建或脚本，并记录结果。
- 用 `git diff --name-only` 确认没有任务范围外残余改动。
- 发现越界修改、静默降级、伪成功路径或验证不充分时，先回退或修正，再进入下一任务。

### 2.4 Code Review 模式

- 当任务标题包含 `[Code Review]`，或用户明确要求“review”时，切换到严格审计模式。
- 审计流程：
  1. 基于 `git diff`、目标文件或提交记录读取变更。
  2. 严格对照本文件与 `docs/reviews/` 下相关专项审计文档。
  3. 执行可用的类型检查、测试和构建命令。
- 产出默认放在 `docs/reviews/`，文件名沿用 `CR-*.md` 风格。
- 审计任务默认不修改业务代码；若需顺手修复，必须先得到用户明确许可。

## 3. 质量红线

### 3.1 开发边界

- 禁止为迎合测试而硬编码业务假设；验收标准必须基于真实业务语义推导。
- 只修改当前原子任务所需的文件，不顺手修无关问题。
- 修改后必须执行最小充分验证，不能用“看起来没问题”代替测试。
- 非兼容性要求场景下，不保留死代码、过时兼容分支或双轨逻辑。

### 3.2 工程质量基线

- 遵循 SOLID、DRY、关注点分离、YAGNI。
- 命名清晰、抽象务实；仅在关键或非直观处添加简洁注释。
- 显式处理边界条件，不得隐藏失败。
- 业务逻辑优先依赖注入，不在核心流程中硬编码具体实现。
- 优先返回新值表达状态变化，避免隐式共享可变状态和函数入参就地修改。

### 3.3 代码复杂度硬约束

| 指标 | 上限 | 超限处理 |
| --- | --- | --- |
| 函数长度 | <= 50 行 | 拆分辅助函数 |
| 文件长度 | <= 300 行 | 按职责拆分 |
| 嵌套深度 | <= 3 层 | 使用卫语句或提前返回 |
| 位置参数 | <= 3 个 | 改为配置对象 |
| 圈复杂度 | <= 10 | 拆分分支逻辑 |
| magic number | 0 | 抽取命名常量 |

### 3.4 代码输出纯文本约束

- 严格禁止在代码、注释、日志字符串和 Markdown 文档中使用 Emoji、装饰性 Unicode 符号或非标准 ASCII 装饰字符。
- 注释和文档只使用纯文本、标准列表和标准 Markdown 强调。

### 3.5 安全基线

- 严禁在源码中硬编码密钥、凭证、Cookie 或 token。
- 数据库访问优先参数化和 GORM，禁止字符串拼接 SQL。
- 所有外部输入必须在边界处校验与净化。
- 用户在会话中临时粘贴密钥用于调试属于正常流程；只有当密钥被写入仓库文件时，才应触发泄漏风险告警。

## 4. 测试体系

### 4.1 测试代码放置位置

- Go 测试放在对应包目录下的 `_test.go` 文件中，不在生产逻辑中嵌入测试分支。
- 前端测试放在 `web/tests/` 或与前端模块同层的 `*.test.js` / `*.test.mjs` 文件。
- 不得为测试新增隐藏开关、特殊成功路径或仅测试可见的业务分支。

### 4.2 验证基线

- 代码必须可测试，优先使用可重复的自动化验证。
- 后端改动优先运行最小相关 `go test`；前端改动优先运行最小相关 `bun` 检查与构建。
- 无法覆盖真实依赖时，必须明确说明“已覆盖语义，未覆盖真实环境”。
- 不得把局部测试通过表述成“全链路已验证”。
- 主线回归验证记录应沉淀到 `docs/reviews/`，包含命令、退出码、通过或失败摘要以及跳过原因。

### 4.3 关键数据格式口径验证

- 处理 CSV、JSON、Protobuf、渠道配置或计费聚合数据时，必须明确定义字段顺序、类型和业务含义，并补格式校验测试。
- 若存在多版本格式，必须显式区分版本与处理逻辑。
- 测试至少覆盖：列数不匹配、字段错位、单位不一致、空值处理和显式零值场景。

### 4.4 Schema-Sensitive 数据库测试规则

- 只要逻辑依赖真实数据库列类型、SQL 类型转换、驱动序列化行为或跨数据库兼容差异，就不能只用 fake、stub 或纯内存对象宣称已覆盖。
- 此类改动至少补离线 Schema 契约测试，并明确 SQLite、MySQL、PostgreSQL 三库兼容口径。
- 若当前环境无法连接真实数据库，必须同时标注“已覆盖语义，未覆盖真实 Schema”，并在任务或门禁文档中记录待真实环境复验项。
- 不得把 `go test` 全绿误判为真实数据库环境已通过。

### 4.5 最新认知索引

理解当前阶段代码真相时，优先查阅 `docs/reviews/`：

- 阶段总览：`docs/reviews/CR-STAGE-ALIGNMENT-2026-05-01.md`
- 当前状态：按主题查找最新 `CR-*.md`
- 近期高价值专项：
  - `docs/reviews/CR-HEADER-PROFILE-STRATEGY-2026-04-21.md`
  - `docs/reviews/CR-DESKTOP-OAUTH-HANDOFF-STORE-2026-04-17.md`
  - `docs/reviews/CR-DEPLOY-BUILD-TRACEABILITY-2026-05-01.md`

若文档与用户最新要求或代码冲突，以用户最新要求和代码事实为准，并在需要时回写文档。

## 5. 仓库专项约束

### 5.1 JSON 统一使用 `common/json.go`

业务代码中禁止直接调用 `encoding/json` 的编解码函数，统一使用：

- `common.Marshal`
- `common.Unmarshal`
- `common.UnmarshalJsonStr`
- `common.DecodeJson`
- `common.GetJsonType`

`json.RawMessage` 等类型可作为类型引用，但实际编解码必须走 `common.*`。

### 5.2 数据库必须同时兼容 SQLite、MySQL、PostgreSQL

- 优先使用 GORM，而不是手写方言 SQL。
- 不直接写 `AUTO_INCREMENT`、`SERIAL`。
- 必须兼容保留字列名、布尔值差异和迁移差异。
- SQLite 不支持的 `ALTER COLUMN`，必须用兼容方案。
- 若无法避免原生 SQL，必须同时给出三库兼容处理。

### 5.3 前端默认使用 Bun

- 安装依赖：`cd web && bun install`
- 开发：`cd web && bun run dev`
- 构建：`cd web && bun run build`
- i18n 工具：`cd web && bun run i18n:*`

### 5.4 Web UI 改动必须服从现有系统主题风格

- 所有 Web UI、Dashboard、弹窗、表格、详情区、筛选区、表单、按钮、标签、卡片等改动，必须优先复用当前系统已有的视觉语言和交互模式。
- 禁止为单个功能引入与现有系统割裂的样式体系，包括重阴影、重边框、突兀底色、过大圆角以及不协调的固定色值。
- 优先顺序固定为：先复用 Semi Design 现有组件语义，再复用本仓库现有页面实现，最后才做最小必要定制。
- 任何前端样式改动都必须同时检查亮色主题、暗色主题、桌面端和移动端。
- 任何新增 UI 入口或详情展示，都必须先判断是否应复用现有弹窗、抽屉、展开区、描述列表或表格工具栏模式。

### 5.5 新渠道接入时检查 `StreamOptions`

新增渠道前，先确认上游是否支持 `StreamOptions`；若支持，必须同步加入 `streamSupportedChannels`。

### 5.6 保留 AGPL 与上游来源

本仓库独立演进，但必须保留：

- 许可证文本
- 必要的版权与修改声明
- 对 `QuantumNous/new-api` 的来源说明

允许独立调整 README、仓库描述、文档入口、发布说明和帮助链接，但不得抹去上游来源或伪造独占归属。

### 5.7 阶梯计费相关改动前先读 `pkg/billingexpr/expr.md`

只要涉及表达式计费、阶梯计费、工具定价、预扣费或结算链路，必须先阅读 `pkg/billingexpr/expr.md`。

涉及 `ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio`、`ModelPrice`、历史消费日志金额修正、`quota_data` 聚合修正、用户或渠道用量回算时，必须同时阅读 `docs/operations/pricing-maintenance.md`。

### 5.8 中继请求 DTO 必须保留显式零值

客户端 JSON 解析后再转发给上游的可选标量字段，必须使用带 `omitempty` 的指针类型，例如：

- `*int`
- `*uint`
- `*float64`
- `*bool`

语义要求：

- 未传：`nil`，序列化时省略
- 显式传 `0` / `false`：必须保留并继续向上游发送

### 5.9 真实渠道验证必须使用隔离环境

- 禁止开发实例直连生产数据库或生产数据目录。
- 生产渠道配置只能只读导出，再导入到隔离环境。
- 必须使用独立数据库、独立运行目录、独立日志、独立端口。
- 迁移、缓存重建、Token 创建和联调测试都必须在隔离环境进行。

### 5.10 Dashboard 会话接口需要 `New-Api-User`

对于受 `middleware.UserAuth()` 保护的后台接口：

- 仅有会话 Cookie 不够
- 必须额外带上 `New-Api-User: <当前用户 ID>`
- 该值必须与当前登录用户一致，否则返回 `401`

### 5.11 不能只靠 `Content-Type` 判断是否为流式

部分上游会把普通 JSON 错误标记成 `text/event-stream`。处理规则：

- 非流式请求，不能只因响应头是 `text/event-stream` 就强行按 SSE 处理
- 先看请求意图，再看响应体前缀
- 去掉前导空白后，如果以 `{` 或 `[` 开头，按普通 JSON 处理
- 若以 `data:`、`event:`、`:` 开头，才按 SSE 处理

### 5.12 本地与隔离运行必须使用稳定密钥

启动本地或隔离实例前，必须显式设置：

- `SESSION_SECRET`
- `CRYPTO_SECRET`

禁止依赖隐式默认值或占位字符串，否则会影响会话与加密行为，导致联调结论失真。

### 5.13 本仓库独立于上游演进

- 仓库路线、优先级和发布时间由本仓库自行决定。
- 上游变更是可选输入，不是默认必须同步。
- 吸纳上游前，先评估其对本仓库增强能力的影响。
- 本项目不考虑向上游仓库提交 PR。
- 本项目语境中的“提交 PR”默认仅指向本仓库自己的 GitHub 仓库。
- 未经用户明确批准，不得向 `upstream` 或任何上游仓库创建 PR、issue 或 review。

重点关注的本地增强能力包括：

- 企业 SSO
- 协议转换策略
- 阶梯计费与工具定价
- 请求内容日志
- Dashboard 与 Web UI 增强

### 5.14 开发环境必须分层，不得混用

- 日常主开发只使用完全隔离的开发环境。
- 完全隔离环境必须独立使用自己的 `new-api` 容器、PostgreSQL、Redis、数据目录、日志目录、端口、`SESSION_SECRET` 和 `CRYPTO_SECRET`。
- 禁止把“只改端口”的半隔离环境当作主开发环境。

### 5.15 只读前端联调环境必须双重阻断

- 只读前端联调环境允许复用正式后端，只用于界面观察和样式联调。
- 必须同时满足两层控制：
  - 前端请求层阻断所有非安全方法，并阻断登录、登出、绑定、OAuth/CAS 回调等高风险入口
  - 代理层再次阻断所有非安全方法和关键副作用路径
- 不得让本地前端绕过只读代理直接请求正式后端。
- 不得在只读联调环境中执行任何会写库、改配置、改用户状态或改渠道状态的操作。

### 5.16 正式 Docker 服务必须保持同一 Compose 分组

正式环境中的 `new-api`、数据库和 Redis 等关联容器，必须保持在同一个 Docker Compose project 分组下。

至少保持以下标签：

- `com.docker.compose.project`
- `com.docker.compose.service`
- `com.docker.compose.project.working_dir`
- `com.docker.compose.project.config_files`

若因宿主机路径兼容问题临时改用 `docker run`，也必须补齐上述标签。

### 5.17 Docker 宿主机路径必须使用显式变量

为兼容 macOS `/Volumes`、Windows、Linux 和 WSL，Compose 中涉及宿主机目录的绑定挂载统一要求：

- 数据目录使用 `${NEW_API_DATA_DIR:-./data}`
- 日志目录使用 `${NEW_API_LOG_DIR:-./logs}`
- Compose 中优先使用 `type: bind` 长写法

macOS 正式环境必须显式设置 `NEW_API_DATA_DIR` 和 `NEW_API_LOG_DIR` 为真实绝对路径。

### 5.18 桌面端 sidecar 必须与仓库环境隔离

Tauri 桌面端启动 `new-api` sidecar 时，必须显式隔离仓库根目录和宿主环境配置：

- `NEW_API_SKIP_DOTENV=true`
- 独立工作目录使用桌面应用数据目录
- 默认使用独立 SQLite：`SQL_DSN=local`
- `SQLITE_PATH=<应用数据目录>/data/new-api.db`
- `LOG_SQL_DSN=`、`REDIS_CONN_STRING=`
- 显式注入稳定的 `SESSION_SECRET` 和 `CRYPTO_SECRET`

### 5.19 桌面端 OAuth handoff 多实例部署必须使用共享状态

- `/api/oauth/desktop/start`、OAuth callback、`/api/oauth/desktop/poll` 不能假设命中同一进程。
- 单实例本地开发可使用进程内 memory store；多实例部署必须启用 Redis 作为共享状态面。
- `/api/oauth/desktop/poll` 必须使用按 `handoff_token` 分桶的独立限流。
- Redis 不可用时必须显式暴露失败，不能伪造完成态或静默降级。

### 5.20 Header Profile 必须作为完整请求头主对象管理

- 渠道请求头配置必须以完整 `Header Profile` 为主对象管理。
- `User-Agent` 只是 `Header Profile.headers` 中的一个字段，不能再单独设计独立策略体系。
- 渠道侧只保存 `settings.header_profile_strategy` 这类策略引用，不在渠道表单中维护第二套零散请求头模板状态。
- 运行时快照只能来自完整 `Header Profile`，不能演变成渠道表单里的临时请求头编辑区。
- 旧 `header_override` 仅允许显式导入为 `Header Profile`，禁止静默写库迁移或自动覆盖用户现有策略。
- AI Coding CLI 预置 Profile 只是固定请求头快照；遇到 Codex、Claude 官方客户端限制、会话追踪或 SDK 元数据校验时，必须通过 `param_override.operations[].mode=pass_headers` 透传真实客户端动态请求头，不能用固定 UA 模板伪造。

## 6. 环境与编译

### 6.1 网络与服务依赖

- 完全隔离开发环境使用 `deploy/compose/dev-isolated.yml`。
- 只读前端联调环境使用 `deploy/compose/frontend-readonly-proxy.yml`。
- 若任务依赖数据库、Redis、正式渠道或 OAuth 回调链路，先确认对应端口和网络可达性，再开始验证。

### 6.2 编译与运行环境

| 项目 | 要求 | 验证命令 |
| --- | --- | --- |
| Go | 1.25+ | `go version` |
| Bun | 用于前端依赖与构建 | `bun --version` |
| Node.js | 供部分脚本与测试使用 | `node --version` |
| Docker Compose | 隔离环境与只读联调 | `docker compose version` |

常用命令：

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service
cd web && bun install
cd web && bun run lint
cd web && bun run build
```

完全隔离开发环境：

```bash
cp deploy/env/dev-isolated.env.example deploy/env/dev-isolated.env
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d
```

只读前端联调环境：

```bash
cp deploy/env/frontend-readonly.env.example deploy/env/frontend-readonly.env
docker compose -f deploy/compose/frontend-readonly-proxy.yml --env-file deploy/env/frontend-readonly.env up -d
cd web
VITE_DEV_PROXY_TARGET=http://127.0.0.1:3300 \
VITE_REACT_APP_READONLY_MODE=true \
bun run dev --host 0.0.0.0 --port 5173
```

### 6.3 编译后生效提醒

- 前端改动后，至少重新执行一次 `cd web && bun run build` 确认可构建。
- 涉及隔离环境、Compose、代理或桌面 sidecar 的改动后，不能只看源码；必须在目标运行形态下做最小真实验证。

## 7. 提交与文件卫生

- 每个原子任务单独提交，提交信息清晰说明改动内容。
- 提交前运行 `git diff --name-only`，确认只包含任务边界内文件。
- 默认不提交临时文件、日志目录、编辑器缓存、个人配置文件，除非任务明确要求。
- 建议 `.gitignore` 覆盖 `*.log`、`*.tmp`、`__pycache__/`、`.DS_Store`、`target/` 和本地虚拟环境目录。

## 8. 历史踩坑备忘

- Header Profile、UA 预置与 `pass_headers` 透传策略相关认知，优先查 `docs/reviews/CR-HEADER-PROFILE-STRATEGY-2026-04-21.md`。
- 桌面端 OAuth handoff 与 Redis 共享状态相关认知，优先查 `docs/reviews/CR-DESKTOP-OAUTH-HANDOFF-STORE-2026-04-17.md`。
- 部署、构建、Compose 分组和路径兼容相关认知，优先查 `docs/reviews/CR-DEPLOY-BUILD-TRACEABILITY-2026-05-01.md`。

## 9. Skills 使用规则

- 开始任务前先扫描项目已知可用的 skill 文档；命中场景必须阅读对应 `SKILL.md` 并遵循。
- 启用 skill 时，在沟通中说明技能名称和用途。
- 常规开发不强制命中特定 skill；仅在语义明确匹配时启用。
