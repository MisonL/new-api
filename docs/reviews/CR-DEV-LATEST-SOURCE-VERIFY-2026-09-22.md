# CR: 开发环境最新源码全量验证与缺陷发现

- 日期：2026-09-22
- 范围：`dev` 分支 HEAD `85c414f34` + 未提交工作区改动，隔离开发环境
- 状态：验证完成，发现 1 个缺陷（已定位根因，未改源码）

## 环境与版本对齐

```bash
git rev-parse HEAD
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
```

构建与容器内 `--build-info` 一致：

```text
built image=new-api-local:dev version=v1.1.0 commit=85c414f34f3abf48255c451ba14681daf4e89dd2-dirty date=2026-09-22T01:02:20Z
commit=85c414f34f3abf48255c451ba14681daf4e89dd2-dirty
```

`-dirty` 来自未提交的工作区改动，`commit` 与 `git HEAD` 一致，说明容器运行的是当前源码。
容器 healthy，`/api/status` HTTP 200。**`healthy` 只证明服务健康，版本同步由 commit 比对证明。**

正式服务 `13000` 全程未重建、未重启、未写入。

## 静态检查

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `scripts/go-test-all.sh` | **27 个包 ok，0 失败** |
| `cd web/default && bun run lint` | exit 0 |
| `cd web/default && bun run typecheck` | exit 0 |
| `cd web/default && bun test` | 225 pass / 0 fail |
| `cd web/default && bun run build` | exit 0，`Safari compatibility check passed` |
| `cd web/classic && bun run lint` | prettier 全部通过 |
| `cd web/classic && bun test` | 203 pass / 0 fail |
| `cd web/classic && bun run build` | exit 0，`Safari compatibility check passed` |

## 缺陷：字面量 `"null"` 的 model_mapping 使渠道被静默排除出路由

### 现象

隔离环境启用 V2 路由后，所有模型均返回
`No available channel for model ... under group default`，即使渠道已启用且有可用 key。
容器日志出现 33 条：

```text
[SYS] route scope skipped invalid model mapping: group=default model=gpt-5.4 channel_id=183
      error=channel model mapping configuration error: unmarshal_model_mapping_failed
```

### 根因

这些渠道的 `model_mapping` 列存的是**字面量字符串 `null`（4 个字符）**，不是 SQL NULL：

```sql
select id, model_mapping is null as is_sql_null, length(model_mapping), quote_literal(model_mapping)
from channels where id in (183,200,202,203,9230);
```

```text
 id  | is_sql_null | len | literal
-----+-------------+-----+---------
 183 | f           |   4 | 'null'
 200 | f           |   4 | 'null'
 202 | f           |   4 | 'null'
 203 | f           |   4 | 'null'
 9230| f           |   4 | 'null'
```

`parseChannelModelMappingEntries`（`model/channel_mapping.go:144`）先 `TrimSpace`，
空串直接返回，随后用 JSON decoder 解析。`null` 是合法 JSON，但顶层是 `null`
而不是 `{`，于是命中：

```go
delim, ok := token.(common.JsonDelim)
if !ok || delim != '{' {
    return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
}
```

直接验证解析行为：

```text
input=""               err=<nil> mapping=true
input="null"           err=channel model mapping configuration error: unmarshal_model_mapping_failed
input="{}"             err=<nil> mapping=true
input={"a":"b"}        err=<nil> mapping=true
```

即空串与 `{}` 都接受，唯独字面量 `null` 被拒。

### 影响

`effectiveRouteModelForAbility`（`model/channel_route_scope.go:167`）返回错误后，
`buildRouteCandidateCacheV2` 走 fail-closed 分支**静默跳过**该 ability
（`:595`），只写一条 `SysError` 日志，请求侧表现为"无可用渠道"：

```go
if IsChannelModelMappingError(err) {
    common.SysError(...)
    continue
}
```

三个后果：

1. **渠道被静默排除**。渠道在后台显示"已启用"，但请求永远选不到，且错误信息
   是 `model_not_found`，会误导排查方向。
2. **路由迁移被永久阻断**。`ApplyRouteFieldsBackfill` 在
   `SkippedScopes > 0 || len(MappingConflicts) > 0 || len(Errors) > 0` 时返回
   `ErrRouteScopeMigrationConflicts`（`:1148`），迁移无法 apply，V2 路由无法启用。
3. **正式环境同样存在**。正式库 `channels` 表 78 行中有 **12 行**是字面量 `null`，
   其中 **4 行 `status=1`（启用）**：

```text
 id  |             name              | status
-----+-------------------------------+--------
  35 | token.sensenova.cn-qian       |      1
  36 | token.sensenova.cn-lockming   |      1
  37 | token.sensenova.cn-guoyuan    |      1
  72 | mdkj.lol                      |      1
（另有 8 行 status=2 已禁用）
```

正式环境 `CHANNEL_ROUTING_V2_ENABLED=false`，因此该问题在正式环境目前**潜伏未触发**；
一旦按计划启用 V2 路由，这 4 个启用渠道会立即被排除，且迁移会被这 12 行阻断。

### 其它列无此问题

```text
model_mapping null-literal:        12
status_code_mapping null-literal:   0
param_override null-literal:        0
```

### 数据来源判断

`model/channel_mapping.go` 的写入侧与前端 `model-mapping-editor.tsx` 都不产生该值
（前端空映射返回 `''`）。正式库受影响渠道的创建时间为 2026-04-07 至 2026-04-11，
属早期版本写入的历史数据。当前源码未见写入字面量 `null` 的路径。

### 本轮处置

**只修复了隔离库数据，未改源码**：`update channels set model_mapping='' where model_mapping='null'`
（5 行）。同时清理了隔离库 19 行孤儿 abilities（`channel_id` 指向已删除渠道，
测试残留）。之后迁移 apply 成功：

```text
scope_count=2184  abilities_to_backfill=3483  channels_to_backfill=3367
skipped_scopes=0  mapping_conflicts=0  errors=0
```

正式库**未做任何修改**。

## 真实渠道端到端验证

迁移 apply 后，隔离环境路由恢复。用隔离库 token（`tokens.id=5`，属 root 用户）调用，
key 仅经 shell 变量传递，未写入报告或仓库。

| 路径 | 结果 |
| --- | --- |
| `POST /v1/chat/completions` 非流式 | HTTP 200，`content` 正常 |
| `POST /v1/chat/completions` 流式 | HTTP 200，12 个 `data:` 事件，含 `[DONE]` |
| `POST /v1/responses` 非流式 | HTTP 200，`object=response` |
| `POST /v1/responses` 流式 | HTTP 200，含 `response.created` → `response.completed` 完整序列 |
| `POST /v1/messages` 非流式 | HTTP 200，`content[0].text` 正常 |
| `POST /v1/messages` 流式 | HTTP 200，7 个事件，含 `message_start` |

Responses 路径需按 fail-closed 契约携带 Codex 动态请求头（`x-codex-*`、
`originator`、`session_id` 等），未携带时返回 503 并在消息中说明原因，
与 `CR-DEV-ISOLATED-VERIFY-2026-09-20.md` 记录一致，属预期行为。

### 错误路径

| 场景 | 结果 |
| --- | --- |
| 不存在的模型 | 503 `model_not_found` |
| 无效 token | 401 |
| 缺 Authorization | 401 |
| 畸形 JSON | 400，错误信息无内部细节泄露 |

均无 500，无堆栈或内部路径泄露。

## 前端渲染验证

`web/default` 开发服务器（`VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001`）
配 `web/scripts/browser-cross-verify.mjs`：

| 浏览器 | 视口 | bodyLength | consoleText |
| --- | --- | --- | --- |
| chromium | desktop | 2263 | true |
| chromium | mobile | 2247 | true |
| firefox | desktop | 2263 | true |
| firefox | mobile | 2247 | true |
| webkit | desktop | 1146 | true |
| webkit | mobile | 1122 | true |

`failures` 为空，6 组全部通过。

## 未覆盖与边界

- **未修改源码**。缺陷已定位到具体函数与行号，但修复方案（是规范化写入侧、
  容忍读取侧，还是清洗存量数据）需要单独决策，本轮不动。
- **未在正式环境做任何变更**。正式库的 12 行字面量 `null` 原样保留。
- **未验证图片、视频、音频、任务类（Midjourney/Suno/Kling 等）渠道**的真实调用。
  隔离库中这些模型的 abilities 多为 `enabled=f`，无从发起。
- **未验证真实 Codex CLI / Claude Code 客户端**端到端，仅以等价的动态请求头复现契约。
- **未验证 OAuth、Passkey、CAS、OIDC 等认证流程**的真实浏览器登录。
- 隔离库的 47 个启用渠道中，本轮只对能返回 200 的渠道做了调用；上游凭证失效的
  渠道返回 404/401，属渠道侧问题，不是本轮代码缺陷。
- `web/default` 登录态下的交互（真实表格、弹窗）未验证：隔离库管理员口令未知，
  未尝试猜测。
