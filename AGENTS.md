# AGENTS.md - new-api 仓库执行规则

## 0. 项目定位

`new-api` 是 Go + React 的统一 AI API 网关，基于 `QuantumNous/new-api` 独立演进。它聚合 OpenAI、Claude、Gemini、Azure、AWS Bedrock 等上游，对外提供 OpenAI 兼容接口、用户管理、计费、限流、日志和管理后台。

本仓库独立维护路线、发布节奏和增强能力，但必须保留 AGPL 许可文本、必要版权声明和上游来源说明。

## 1. 基本工作方式

- 全程使用中文沟通，结论直接收束当前问题。
- 所有判断必须基于代码、测试、日志、HTTP 响应、Docker 状态或 Git 证据。
- 禁止为了“先跑通”添加隐藏回退、静默容错、默认降级或伪成功路径。
- 不使用 mock、模板化成功输出或吞异常来制造通过结果。
- 当前仓库没有稳定的 `tasks.md` 或 `issues.csv` 作为唯一任务源；默认以用户最新明确要求为当前任务源。
- 每次只处理一个原子任务。若发现无关问题，只记录或说明，不顺手扩大修改范围。
- 修改前先读相关代码和文档；修改后必须做最小充分验证。
- 工作区可能已有用户改动。不得回退、覆盖或整理与当前任务无关的改动。

## 2. 技术栈和目录

- 后端：Go 1.25+、Gin、GORM v2
- 前端：React 18、Vite、Semi Design，默认包管理器为 `bun`
- 数据库：SQLite、MySQL、PostgreSQL，三者必须兼容
- 缓存：Redis + 内存缓存
- 鉴权：JWT、Passkey、OAuth、OIDC、CAS、Trusted Header

主要目录：

- `router/`：路由注册
- `controller/`：请求处理
- `service/`：业务逻辑
- `model/`：数据模型与数据库访问
- `relay/`：上游协议适配与中继
- `middleware/`：鉴权、限流、日志、分发
- `setting/`：系统、模型、计费、性能等配置
- `common/`：共享工具
- `dto/`、`types/`、`constant/`：结构体、类型和常量
- `oauth/`：OAuth 与桌面端 handoff 相关实现
- `pkg/`：内部通用包
- `web/default/`：当前重点维护的前端
- `web/classic/`：classic 前端入口，仍可能被系统配置加载
- `desktop/`：Tauri 2 桌面客户端
- `deploy/compose/`、`deploy/env/`：隔离开发、只读联调和部署编排
- `docs/reviews/`、`docs/architecture/`、`docs/operations/`：审计、架构与运维文档

## 3. 代码约束

- 业务代码中禁止直接调用 `encoding/json` 的编解码函数，统一使用 `common/json.go` 中的 `common.Marshal`、`common.Unmarshal`、`common.UnmarshalJsonStr`、`common.DecodeJson`、`common.GetJsonType`。
- `json.RawMessage` 等类型可作为类型引用，但实际编解码必须走 `common.*`。
- 数据库访问优先使用 GORM 和参数化查询，避免手写方言 SQL。
- 不直接写 `AUTO_INCREMENT`、`SERIAL` 或只适配单一数据库的迁移语句。
- SQLite 不支持的 `ALTER COLUMN` 必须使用兼容方案。
- 客户端 JSON 解析后再转发给上游的可选标量字段，必须用带 `omitempty` 的指针类型保留显式零值，例如 `*int`、`*uint`、`*float64`、`*bool`。
- 不能只靠 `Content-Type` 判断是否为流式响应。非流式请求要先看请求意图，再看响应体前缀；去掉前导空白后以 `{` 或 `[` 开头按 JSON 处理，以 `data:`、`event:`、`:` 开头才按 SSE 处理。
- 新渠道接入前先确认上游是否支持 `StreamOptions`；若支持，必须同步加入 `streamSupportedChannels`。
- 安全相关代码不得硬编码密钥、凭证、Cookie 或 token。用户临时粘贴密钥用于调试不等于源码泄漏；只有写入仓库文件才触发泄漏风险告警。
- 代码、注释、日志字符串和 Markdown 文档不使用 Emoji 或装饰性 Unicode 符号。Markdown 使用普通列表和标准强调。

## 4. 前端约束

- 前端默认在 `web/default` 下工作，命令使用 `bun`。
- Web UI 改动必须服从现有系统视觉语言，优先复用 Semi Design 和本仓库已有页面模式。
- Dashboard、弹窗、表格、筛选区、表单、按钮、标签、卡片等不要为单个功能引入割裂样式。
- 新入口或详情展示先判断是否应复用现有弹窗、抽屉、展开区、描述列表或表格工具栏。
- 样式改动必须检查亮色主题、暗色主题、桌面端和移动端。
- `3001` 容器入口可能按系统配置加载 classic 主题，不能用它代替 `web/default` 验证。验证 `web/default` 时另启开发服务器代理到隔离后端。

```bash
cd web/default
VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001 bun run dev --host 127.0.0.1 --port 5176
```

## 5. 计费、请求头和认证专项

- 涉及表达式计费、阶梯计费、工具定价、预扣费或结算链路，先读 `pkg/billingexpr/expr.md`。
- 涉及 `ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio`、`ModelPrice`、历史消费日志金额修正、`quota_data` 聚合修正、用户或渠道用量回算，必须同时读 `docs/operations/pricing-maintenance.md`。
- 渠道请求头配置必须以完整 `Header Profile` 为主对象管理；`User-Agent` 只是 `Header Profile.headers` 中的字段，不能再设计独立 UA 策略体系。
- 渠道侧只保存 `settings.header_profile_strategy` 这类策略引用，不在渠道表单中维护第二套零散请求头模板状态。
- 旧 `header_override` 仅允许显式导入为 `Header Profile`，禁止静默写库迁移或自动覆盖用户现有策略。
- AI Coding CLI 预置 Profile 只是固定请求头快照。遇到 Codex、Claude 官方客户端限制、会话追踪或 SDK 元数据校验时，必须通过 `param_override.operations[].mode=pass_headers` 透传真实客户端动态请求头。
- 受 `middleware.UserAuth()` 保护的后台接口，仅有会话 Cookie 不够，还必须带 `New-Api-User` 请求头，值为当前登录用户 ID，否则返回 `401`。

## 6. 测试和验证

后端改动优先运行最小相关 Go 测试：

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service
```

前端改动优先运行：

```bash
cd web/default
bun run lint
bun run build
```

必要时补充：

```bash
cd web/default
bun run i18n:sync
bun run typecheck
```

验证规则：

- 不得把局部测试通过表述成全链路已验证。
- 依赖真实数据库列类型、SQL 类型转换、驱动序列化或三库兼容差异的逻辑，不能只用 fake、stub 或纯内存对象宣称覆盖。
- 无法连接真实依赖时，必须明确说明“已覆盖语义，未覆盖真实环境”。
- 主线回归验证记录应沉淀到 `docs/reviews/`，包含命令、退出码、通过或失败摘要以及跳过原因。
- 声称完成、通过、已修复或可用前，必须在本轮运行对应验证命令并读取输出。

## 7. 运行环境

日常主开发只使用完全隔离开发环境：

```bash
cp deploy/env/dev-isolated.env.example deploy/env/dev-isolated.env
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d
```

完全隔离环境必须独立使用自己的 `new-api` 容器、PostgreSQL、Redis、数据目录、日志目录、端口、`SESSION_SECRET` 和 `CRYPTO_SECRET`。禁止把只改端口的半隔离环境当作主开发环境。

使用最新代码重建隔离开发环境：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

判断开发环境是否为最新版本时，必须比对：

```bash
git rev-parse HEAD
git rev-parse origin/main
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
```

容器 `healthy` 只能证明服务健康，不能证明运行代码已同步到最新 Git 提交。

只读前端联调环境用于复用正式后端做界面观察，必须双重阻断副作用：

- 前端请求层阻断所有非安全方法，并阻断登录、登出、绑定、OAuth/CAS 回调等高风险入口。
- 代理层再次阻断所有非安全方法和关键副作用路径。
- 不得让本地前端绕过只读代理直接请求正式后端。

```bash
cp deploy/env/frontend-readonly.env.example deploy/env/frontend-readonly.env
docker compose -f deploy/compose/frontend-readonly-proxy.yml --env-file deploy/env/frontend-readonly.env up -d
cd web/default
VITE_DEV_PROXY_TARGET=http://127.0.0.1:3300 \
VITE_REACT_APP_READONLY_MODE=true \
bun run dev --host 0.0.0.0 --port 5173
```

## 8. Docker 和桌面端

- 正式环境中的 `new-api`、数据库和 Redis 等关联容器必须保持在同一个 Docker Compose project 分组。
- 至少保留 `com.docker.compose.project`、`com.docker.compose.service`、`com.docker.compose.project.working_dir`、`com.docker.compose.project.config_files` 标签。
- Compose 中涉及宿主机目录的绑定挂载必须使用显式变量：`${NEW_API_DATA_DIR:-./data}`、`${NEW_API_LOG_DIR:-./logs}`，并优先使用 `type: bind` 长写法。
- macOS 正式环境必须显式设置 `NEW_API_DATA_DIR` 和 `NEW_API_LOG_DIR` 为真实绝对路径。
- 本地或隔离实例启动前必须显式设置 `SESSION_SECRET` 和 `CRYPTO_SECRET`，禁止依赖隐式默认值或占位字符串。
- Tauri 桌面端 sidecar 必须隔离仓库根目录和宿主环境配置：`NEW_API_SKIP_DOTENV=true`，独立工作目录使用桌面应用数据目录，默认 `SQL_DSN=local`，`SQLITE_PATH` 指向桌面应用数据目录下的 `data/new-api.db`，并清空 `LOG_SQL_DSN`、`REDIS_CONN_STRING`。
- 桌面端 OAuth handoff 多实例部署必须使用 Redis 共享状态。Redis 不可用时必须显式失败，不能伪造完成态或静默降级。

## 9. 文档规则

- README 面向首次访问者，优先回答“这是什么”、“和上游有什么区别”、“谁在用”、“怎样快速开始”。
- README 保持短而可扫读，长部署说明、专项能力和审计细节下沉到 `docs/`。
- 文档中不得引用不存在的截图、GIF、Demo 或阶段报告。
- 修改运行状态类文档时，必须写清楚采样时间、命令、结果和边界。服务 `healthy` 与“运行最新代码”必须分开表述。
- 重要验证和阶段性事实优先放到 `docs/reviews/CR-*.md`。
- 当前高价值索引：
  - `docs/reviews/CR-DEV-ISOLATED-VERIFY-2026-05-11.md`
  - `docs/reviews/CR-UPSTREAM-UI-INTAKE-PHASE-1-2026-05-11.md`
  - `docs/reviews/CR-DEV-ISOLATED-VERIFY-2026-05-07.md`
  - `docs/reviews/CR-HEADER-PROFILE-STRATEGY-2026-04-21.md`
  - `docs/reviews/CR-DESKTOP-OAUTH-HANDOFF-STORE-2026-04-17.md`
  - `docs/reviews/CR-DEPLOY-BUILD-TRACEABILITY-2026-05-01.md`
  - `docs/architecture/enterprise-auth-three-phase-plan.md`

## 10. 提交和收尾

- 每个原子任务单独提交，提交信息说明改动内容。
- 提交前运行 `git diff --name-only`，确认只包含任务边界内文件。
- 默认不提交临时文件、日志目录、编辑器缓存、个人配置文件，除非任务明确要求。
- 收尾时至少确认 `git status --short --branch`、相关测试或构建结果，以及是否存在临时服务端口未清理。

## 11. Skills

- 开始任务前扫描项目已知可用 skill；语义命中时阅读对应 `SKILL.md` 并遵循。
- 启用 skill 时，在沟通中说明技能名称和用途。
- 常规开发不强制命中特定 skill；仅在语义明确匹配时启用。
