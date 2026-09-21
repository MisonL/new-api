# CR: 隔离开发环境场景验证（本轮修复后重建）

- 日期：2026-09-20 / 2026-09-21（跨零点）
- 范围：`new-api-local:dev` 镜像重建、隔离 `3001` 环境真实渠道调用、`web/default` 浏览器验证
- 状态：已完成

## 环境与版本对齐

采样时间：2026-09-20 23:53 - 2026-09-21 00:30 Asia/Shanghai。

```bash
git rev-parse HEAD                 # 5cda680770543782d5d0a757b31f4e783443dc03
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
```

镜像构建退出码 0，输出：

```text
built image=new-api-local:dev version=v1.1.0 commit=5cda680770543782d5d0a757b31f4e783443dc03-dirty date=2026-09-20T15:40:05Z
```

容器内 `--build-info`：

```text
version=v1.1.0
commit=5cda680770543782d5d0a757b31f4e783443dc03-dirty
date=2026-09-20T15:40:05Z
source=https://github.com/MisonL/new-api
```

`/api/status` 返回 `success=true version=v1.1.0 theme=classic`，容器 `healthy`。
`-dirty` 后缀由构建脚本在存在未提交改动时自动追加；`commit` 与 `git HEAD` 一致，
说明运行的是本轮修复后的源码。**注意：`healthy` 只证明服务健康，版本同步由上面的
`commit` 比对证明，两者分开表述。**

正式服务（`new-api`，端口 13000）全程未重建、未重启、未写入。

## 真实渠道调用矩阵

按用户授权（"可以使用正式服务中正在启用的渠道来在开发环境测试调用验证"），
使用隔离开发库中已启用的渠道。凭证读取在开发库容器内完成，仅经 shell 变量与标准输入传递，
未写入报告或仓库。

### 渠道可达性探测

对隔离库全部 46 个启用渠道逐个发 `POST /v1/chat/completions`（`gpt-5.5`）：

| 结果 | 渠道 |
| --- | --- |
| HTTP 200 | 72、75、76、114 |
| 上游凭证失效（401/403/404） | 127、138、163、169、172、181、184、188、190、201、208、210、9207-9210 |
| 连接失败/超时（500/524/000） | 4、32、71、106、124、146、192、193、205、9211、9212、9226-9228 |
| 分组或模型不匹配（503/404） | 40、87、150、168、174、178、206、9213 |
| model_mapping 非法 | 183、200、202、203 |

### 五条协议路径（渠道 114，模型 gpt-5.5）

| 路径 | 结果 |
| --- | --- |
| `POST /v1/chat/completions`（非流式） | HTTP 200，`content="PONG"` |
| `POST /v1/chat/completions`（流式） | HTTP 200，SSE 正常，含 `finish_reason=stop` 与 `[DONE]` |
| `POST /v1/responses`（非流式） | HTTP 200，`status=completed` |
| `POST /v1/responses`（流式） | HTTP 200，10 个事件：`response.created` → `response.in_progress` → `response.output_item.added` → `response.content_part.added` → `response.output_text.delta` ×2 → `response.output_text.done` → `response.content_part.done` → `response.output_item.done` → `response.completed` |
| `POST /v1/messages` | HTTP 200，`content[0].text="PONG"`，`stop_reason=end_turn` |

渠道 72 / 75 / 76 同样 chat 与 claude 均 200；渠道 114 是唯一同时满足 Responses 路径
`pass_headers` 要求的渠道。

### Responses 路径的前置条件（非缺陷）

直接对渠道 72 / 114 发不带 Codex 动态请求头的 `/v1/responses`，返回 503
`channel:model_mapped_error`：

```text
responses Codex dynamic header passthrough unsupported: channel #114 requires
unconditional pass_headers for Codex dynamic request headers: x-codex-*
```

这是 `relay/common/override.go:636 MissingCodexDynamicRequestHeaders` 的 fail-closed 契约：
渠道 header profile 为 `codex-cli` / `codex-desktop` 时，必须通过
`param_override.operations[].mode=pass_headers` 无条件透传真实客户端动态请求头
（`x-codex-*` 等），静态 profile 无法伪造每请求身份。渠道 114 已配置该 operation
（`X-Codex-Beta-Features`、`X-Codex-Turn-Metadata`、`X-Codex-Window-Id`、
`X-Client-Request-Id`、`Originator`、`Session_id`），补齐这些头后两条 Responses 路径均 200。
与 AGENTS.md 第 5 节一致，符合预期。

## 本轮修复的端到端确认

### 保活 ping 与审计 writer（对应 CR：payload audit Unwrap）

隔离库 `general_setting.ping_interval_enabled=true`。临时将
`general_setting.ping_interval_seconds` 设为 `2`，并临时为用户 1 打开
`record_request_content_log` / `record_response_content_log`（即启用审计响应 writer），
重启隔离容器后发起一次长流式 `/v1/responses`：

- 流中出现 **6 次 `: PING`** 保活事件；
- 结束日志为 `stream ended: reason=done`，**不是** `StreamEndReasonPingFail`。

即审计 writer 在场时 `SetWriteDeadline` 仍可达，ping 未失败。修复前该路径每次 ping 都会
`feature not supported` 并使流以 ping 失败结束。

验证完成后已恢复：删除 `general_setting.ping_interval_seconds` 覆盖行，用户 1 的 `setting`
按验证前备份原样写回（`{"notify_type": "email", "gotify_priority": 0, "quota_warning_threshold": 500000, "accept_unset_model_ratio_model": true}`），已复核一致。

### Classic 排行榜 i18n（对应 CR：Classic 排行榜界面缺失 i18n 键）

真实浏览器（Playwright chromium，`locale=en-US`，强制 `i18nextLng=en`）加载
`http://127.0.0.1:3001/rankings`：

- 页面正文无任何中文残留（`chineseCount=0`）；
- 渲染出 `Popularity is based on live platform usage statistics.`、`Today / This week /
  This month / This year / All time`、`Model token trend`、`Provider market share`、
  `Popular models`、`Provider ranking`、`Fastest rising`、`No ranking changes`、
  `Fastest falling` 等全部新增键；
- 表头为 `Rank / Model / Provider / Tokens / Share / Growth` 与
  `Rank / Provider / Tokens / Share / Models / Popular models`，其中 `Share` 即修复前
  在所有非 `zh-CN` 语言下为空的 `占比`；
- 无 console 错误与 pageerror。

由于缺失键会回落到键本身（中文原文），英文界面无中文残留即证明键全部解析成功。

### web/default 渲染（对应 CR：视图选项暴露行操作列 / 隐藏列持久化）

`web/scripts/browser-cross-verify.mjs --url http://127.0.0.1:5176/`
（`web/default` 开发服务器，`VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001`）：

| 浏览器 | 视口 | bodyLength | consoleText | 渲染错误 | console 错误 | 失败请求 |
| --- | --- | --- | --- | --- | --- | --- |
| chromium | desktop | 2263 | true | 无 | 0 | 0 |
| chromium | mobile | 2247 | true | 无 | 0 | 0 |
| firefox | desktop | 2263 | true | 无 | 0 | 0 |
| firefox | mobile | 2247 | true | 无 | 0 | 0 |
| webkit | desktop | 1146 | true | 无 | 0 | 0 |
| webkit | mobile | 1140 | true | 无 | 0 | 0 |

注意：`3001` 容器入口按系统配置加载 classic 主题，**不能代替 `web/default` 验证**，
因此 `web/default` 单独起了开发服务器并代理到隔离后端。

## 未完成与边界

- **`web/default` 登录态端到端未完成**：隔离开发库管理员口令未知，本次没有尝试猜测口令。
  因此"登录后在真实表格里注入 stale 列可见性、观察操作列仍可见"这一步未做；
  该修复覆盖到纯函数语义、单元测试、构建产物与无错渲染，未覆盖登录后交互。
  见 `CR-DEFAULT-PERSISTED-VISIBILITY-REQUIRED-COLUMN-2026-09-20.md` 的边界说明。
- **隔离开发库的 V2 路由未启用**：`dev-isolated.yml` 默认
  `CHANNEL_ROUTING_V2_ENABLED=true` + `CHANNEL_ROUTING_V2_STRICT_WRITES=true`，但库中
  abilities 的 `route_priority` / `route_weight_bps` 均为未回填状态
  （`*_configured=false`，`weight=0`），`buildRouteCandidateCacheV2` 会跳过未回填行，
  因此不指定渠道时选不到任何渠道（`No available channel for model ...`）。
  `GET /api/channel-route-scopes/migration/preview` 报告 `scope_count=2176`、
  `abilities_to_backfill=3453`、`channels_to_backfill=3337`；`POST .../migration/apply`
  因存在 `model_mapping` 非法的渠道而返回 `route_scope_migration_conflicts`（fail-closed）。
  本轮未修改该状态，改用管理员令牌的渠道后缀固定（`<key>-<channel_id>`）绕开选择器完成验证。
  这是开发库数据状态问题，不是本轮代码缺陷；正式环境是否启用 V2 需按
  `docs/operations/pricing-maintenance.md` 与路由迁移流程单独评估。
- 未做真实 Codex CLI 客户端端到端（仅以等价的动态请求头复现其契约）。
- 未覆盖图片、视频、任务类（Midjourney/Suno/Kling 等）渠道的真实调用。
- 隔离库中 42 个启用渠道上游凭证已失效，本轮只对可用的 4 个渠道做了调用验证。
