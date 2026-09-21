# Classic 与 Default 前端功能 parity 审计

采样时间：2026-09-18（Asia/Shanghai）

## 审计结论

本轮按 Classic 全量页面、路由、设置分区和 API 能力反向核对 Default。未发现仍缺少的已知业务入口；发现的缺口均已补齐，Default 继续作为主维护前端，Classic 保留兼容入口。

两套前端的功能映射包括：

- 控制台首页、聊天、Playground、Chat2Link、令牌/密钥、日志、任务、绘图任务、钱包/充值、兑换码、订阅、用户、渠道、模型、模型部署。
- 系统设置中的运营、仪表盘/内容、聊天、绘图、支付、比例与工具价格、请求限流、模型、模型部署、性能、认证、SSRF、监控、个人和其他设置。
- 顶栏和管理员侧栏模块开关、Pricing/Rankings 开关与登录门禁、Classic 旧路由到 Default 路由的重写。
- 渠道标签级 `param_override`、Header Profile、旧 Header Template 管理入口、渠道路由范围、健康路由和上游更新操作。
- 日志筛选自动补全、批量删除/清理 payload、自动日志保留策略、性能指标摘要和模型性能详情。
- `reasoning_content`/`reasoning` 兼容、结构化 SSE Viewer、旧控制台配置迁移、Telegram-only 登录、CAS/JWT Direct/Trusted Header 自定义 OAuth、桌面 OAuth handoff、钱包邀请记录和礼品码。

## 本轮新增或修正

- `web/default` 内容设置页检测 `ApiInfo`、`Announcements`、`FAQ`、`UptimeKumaUrl`、`UptimeKumaSlug` 五个旧配置键。
- 检测到旧键时显示确认弹窗；确认后调用 `/api/option/migrate_console_setting`。
- 迁移成功后刷新 system-options；请求失败时保持弹窗打开并显示错误。
- 新增检测逻辑的 node:test 覆盖：完整旧键、空值和部分 Uptime Kuma 配置。
- Classic 新增自动日志保留策略设置，并接入运营设置；支持数据库日志、请求/响应 payload 和服务器日志的保留与预览/执行。
- Classic 接入模型性能指标配置和模型广场性能展示，覆盖延迟、吞吐、成功率、TTFT、上游耗时、请求数和趋势详情；接口失败明确显示错误或无数据。
- Classic 补齐 Rankings、渠道 pin 顺序/路由权重、渠道高级设置等 parity 辅助模块及对应测试。
- 修正 Classic lint/解析兼容问题、版权头和数字字面量；完成 Classic i18n 同步。
- 修正 Default 的旧路由重写、日志自动补全配置传递和旧控制台迁移回归。
- 补齐 Default 模型部署表格对运行中部署的“同步到渠道”操作：读取公开容器地址，校验为 HTTP(S)，生成 IO.NET 渠道 payload，创建渠道后刷新渠道缓存。
- 为部署同步的成功、失败和安全校验提示补齐 `en`、`zh`、`fr`、`ja`、`ru`、`vi` 文案。
- 对 Classic 支付（Epay、Stripe、Creem、Waffo、Waffo Pancake）与 Default integrations 页面逐字段核对；对 Classic Other/System/Update 入口与 Default general/content/maintenance 页面逐项核对，未发现未映射的可写功能。

## 验证记录

| 命令 | 结果 |
| --- | --- |
| `bun test`（`web/default`） | 通过，201 pass，0 fail，866 expect() calls |
| `bun run typecheck`（`web/default`） | 通过，退出码 0 |
| `bun run lint`（`web/default`） | 通过，退出码 0 |
| `bun run format:check`（`web/default`） | 通过，所有文件均符合 Prettier 格式；此前 4 个 Playground 文件已格式化 |
| `bun run i18n:sync`（`web/default`） | 通过，退出码 0 |
| `bun run build`（`web/default`） | 通过，Rsbuild、Safari 兼容性和浏览器兼容性检查均通过 |
| `bun run test:node`（`web/classic`） | 通过，137 pass，0 fail |
| `bun run eslint`（`web/classic`） | 通过，退出码 0 |
| `bun run i18n:sync`（`web/classic`） | 通过，所有 locale 已同步 |
| `bun run build`（`web/classic`） | 通过，10817 modules transformed，Safari/浏览器兼容性检查通过，退出码 0 |
| Default locale 键集、静态 `t()` 键和占位符校验 | 通过，5067 个键；源码缺失 0；各 locale 缺失 0、多余 0、未翻译 0、占位符不一致 0 |
| `bun run i18n:lint`（`web/classic`） | 通过，基线 307 条，新增 0 条；当前存量问题 263 条 |
| `go test -count=1 ./...` | 通过，所有 Go 包通过 |
| `git diff --check` | 通过 |
| `bun test src/features/models/lib/deployment-channel.test.ts`（`web/default`） | 通过，4 pass，0 fail |
| `scripts/build-docker-local.sh new-api-local:dev` | 通过，镜像构建完成，包含当前工作区前端产物和后端二进制 |
| `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` | 通过，隔离容器重建并启动 |
| `docker inspect new-api-dev-isolated-new-api-1` | 通过，`running`、`healthy`、重启次数 `0` |
| `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` | 通过，`v1.1.0`，提交 `5cda680770543782d5d0a757b31f4e783443dc03-dirty`，构建时间 `2026-09-18T14:29:49Z` |
| `curl --connect-timeout 5 --max-time 10 http://127.0.0.1:3001/api/status` | 通过，HTTP 200，`success=true` |
| `curl --connect-timeout 5 --max-time 10 http://127.0.0.1:13000/api/status` | 通过，只读健康检查 HTTP 200，未执行写操作 |

## 隔离环境

- 镜像 `new-api-local:dev` 已重新构建。
- `new-api-dev-isolated-new-api-1` 已强制重建，状态 `healthy`，重启次数 0。
- `docker exec ... /new-api --build-info`：版本 `v1.1.0`，提交 `5cda680770543782d5d0a757b31f4e783443dc03-dirty`，构建时间 `2026-09-18T14:29:49Z`。
- `curl --connect-timeout 5 --max-time 10 http://127.0.0.1:3001/api/status`：HTTP 200。
- 隔离 `3001` 的 `/api/status` 返回 `success=true`，同时返回当前前端配置和版本信息；正式 `13000` 仅执行只读健康检查，当前容器 build-info 仍为旧提交 `4527d7666a37f9151a57f08ca0bc5e8e08b8fb46-litefix2`。

## 边界

- 当前工作区包含本任务之外的既有 dirty 修改，未回退或清理。
- 正式服务 `127.0.0.1:13000` 本轮仅执行带超时的只读 `/api/status` 健康检查，未重启、未重建、未写入数据库或 Redis；本记录不等同于正式发布验收。
- 未在真实旧配置数据上执行迁移写操作；已覆盖检测逻辑、API 调用代码路径编译和隔离服务健康检查。
- 隔离环境健康和 build-info 只能证明当前源码已构建并运行在 `3001`，不能证明正式服务已经加载本轮前端或后端改动；正式发布仍需独立部署和验收。
- 本轮 Default `bun run build` 通过，包含 Rsbuild、Safari 兼容性检查；未重建或重启正式 `13000` 服务。
- Default 源码中的静态 `t()` 调用已全部在 `en.json` 建立同名键；本轮重新同步后，六个 locale 的键集合、占位符和未翻译扫描均为 0 问题。
