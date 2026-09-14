# Compact Control Plane v2 正式升级门禁 - 2026-06-18

## 范围

本门禁覆盖 Codex Responses compact 路由的正式修复：

- 区分本地 Codex `compaction_trigger`、本地 synthetic compact 标记和远程 opaque compact item。
- 避免因为旧版或空的上游 profile 误判为 `503` 并跳过。
- 对 `sub2api_http`、`generic_openai`、`generic_proxy` 和 `chat_only_proxy` 保持保守的透传行为。
- 修改渠道时保留 Header Profile / User-Agent 策略。
- 先在隔离 Docker 开发环境验证，再接触正式环境。

操作员阅读本门禁并明确授权前，不得升级正式环境。

任何正式变更前必须使用以下授权措辞：

```text
授权升级正式环境，按 CR-COMPACT-CONTROL-PLANE-V2-PROD-UPGRADE-2026-06-18 执行。
```

没有该明确授权时，只允许执行正式环境只读检查。

## 当前证据

采样时间和部署实例值保存在私有验证证据中。

- 隔离开发后端为 `new-api-dev-isolated-new-api-1`，入口为 `http://127.0.0.1:3001`。
- 候选镜像为 `<isolated-candidate-image>`；不可变镜像身份、构建信息和源码 revision 必须在私有发布记录中一致。
- 开发健康检查：`/api/status` 返回 `success=true`。
- 只读正式预检确认独立识别的正式容器未改变，近期日志也没有匹配已知 compact 路径错误。运行镜像、生命周期和配置值按公开化要求省略。

## 需求审计矩阵

| 需求 | 证据 | 状态 |
| --- | --- | --- |
| 区分本地 Codex `compaction_trigger`、synthetic compact 标记和远程 opaque compact item | `dto/openai_request.go`、`service/responses_synthetic_compact.go`、`relay/responses_handler_test.go`、`relay/channel/openai/adaptor_responses_compaction_test.go` | 开发环境通过 |
| 避免旧版或空上游 profile 误报 `503` | `dto/channel_settings.go`、`controller/relay_retry_test.go`、`relay/responses_handler_test.go` | 开发环境通过 |
| 对远程 compact item 保持各兼容渠道的保守透传 | `dto/channel_settings.go`、`relay/responses_handler_test.go`、`service/log_info_generate_test.go` | 开发环境通过 |
| 保持 new-api/sub2api 上下游兼容 | `dto/openai_responses_compaction_request.go`、`dto/openai_request_zero_value_test.go`、`relay/helper/valid_request_test.go`、`scripts/compact-control-plane-e2e.sh` | 开发环境通过 |
| 正确拒绝或剥离 Responses-to-Chat 不支持的 payload | `relay/responses_via_chat.go`、`relay/responses_via_chat_test.go`、`relay/common/responses_input_filter_test.go` | 开发环境通过 |
| 模型、渠道或 profile 切换后的 compact | `controller/relay_retry_test.go`、`scripts/compact-control-plane-e2e.sh` 的 `model_switch_synthetic_restore` | 开发环境通过 |
| synthetic 状态过期时尽可能退化为 visible-only | `service/responses_synthetic_compact.go`、`relay/responses_via_chat_test.go`、`relay/channel/codex/adaptor_test.go` | 开发环境通过 |
| 修改渠道时不清除 Header Profile 和 User-Agent 设置 | `relay/channel/api_request_test.go`、`controller/channel_test_internal_test.go`、`web/classic/src/components/table/channels/modals/headerProfile.helpers.test.js` | 开发环境通过 |
| 3001 隔离 Docker 使用批准的渠道夹具执行 Codex smoke | 夹具来源和探针输出保存在私有验证证据 | 开发环境通过 |
| 之前的渠道配置恢复 | 私有事故证据中没有完整可恢复配置行 | 尽力完成 |
| 正式升级和升级后验证 | 需要正式变更前的明确授权 | 待授权 |

## 隔离开发环境验证

执行命令：

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service ./relay/channel/openai ./relay/channel/codex -count=1
cd web/classic && bun test src/components/table/channels/modals/headerProfile.helpers.test.js src/components/table/channels/modelTestRuntimeConfig.test.js src/helpers/responsesCompactSettings.test.js
cd web/default && bun test src/features/channels/lib/channel-form.test.ts
cd web/default && bun run lint
cd web/default && bun run build
bash scripts/compact-control-plane-e2e.sh
```

结果：

- Go 测试通过。
- classic 渠道 Header Profile 测试通过。
- default 渠道表单测试通过。
- `web/default` lint 通过。
- `web/default` 构建通过，包含 Safari 兼容性检查。
- `scripts/compact-control-plane-e2e.sh` 通过，清理后没有残留测试 token、渠道或能力行。
- 修正 E2E 脚本清理顺序并重新执行；清理后，与本测试无关的隔离渠道能力仍保持不变。

E2E 覆盖场景：

- `native_newapi_compact`
- `synthetic_newapi_compact_and_continue`
- `stale_local_marker_visible_only`
- `generic_openai_rejects_remote_opaque_compaction`
- `sub2api_http_rejects_rest_previous_id`
- `model_switch_synthetic_restore`
- `real_codex_history_synthetic_restore`

隔离环境真实 smoke 验证：

- 最终 smoke 使用批准的隔离渠道夹具，不使用官方 OpenAI 渠道或公开正式凭据。
- provider 使用 `base_url=http://127.0.0.1:3001/v1` 和 `wire_api=responses`。
- API 凭据只注入进程环境；本报告未打印或写入凭据、渠道标识、上游域名、会话标识或用量日志行。
- 新请求和历史恢复请求均以退出码 `0` 完成，并产生 `thread.started`、`turn.started`、`item.completed`、`turn.completed`。
- 如出现插件目录或 manifest 警告，将其归类为 CLI 警告，不视为 `new-api` 请求失败。
- 3001 容器日志未发现以下匹配错误：
  - `synthetic compact state`
  - `input item type`
  - `chat compatibility mode`
  - `not implemented`
  - `No available channel`
  - `Service Unavailable`
  - `status_code=503`
  - `panic`
  - `fatal`
  - `previous_response_id`

## 之前配置恢复状态

私有事故证据覆盖隔离环境和部署侧存储，但没有把凭据、渠道数据、宿主路径或助手历史写入仓库。未找到完整且可恢复的配置行；历史数据库卷未以可写方式挂载，也未被修改。

## 升级前正式状态

任何变更前，必须在私有部署审计中记录当前正式镜像、依赖镜像、Compose 来源、不可变 digest、build-info 和回滚镜像。正式验证完成前不得删除已记录的回滚镜像。

## 升级命令计划

以下命令只是模板，只有获得明确用户授权后才能执行。

首选的受保护命令：

```bash
COMPACT_PROD_CANDIDATE_IMAGE=<candidate-image> \
  scripts/compact-control-plane-prod-upgrade.sh --dry-run
COMPACT_PROD_CANDIDATE_IMAGE=<candidate-image> \
CONFIRM_PROD_UPGRADE='授权升级正式环境，按 CR-COMPACT-CONTROL-PLANE-V2-PROD-UPGRADE-2026-06-18 执行。' \
  scripts/compact-control-plane-prod-upgrade.sh --execute
```

`COMPACT_PROD_CANDIDATE_IMAGE` 必须来自通过开发门禁的完全相同源码树。受保护脚本故意不提供候选镜像默认值，避免工作区有更新的未提交修复时误部署旧标签。

手动展开步骤：

1. 记录当前正式状态。

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}' | rg 'new-api|postgres|redis|NAMES'
docker exec new-api /new-api --build-info
curl -fsS http://127.0.0.1:13000/api/status
```

2. 从当前源码构建不可变的正式候选镜像。

```bash
scripts/build-docker-local.sh <candidate-image>
docker image inspect <candidate-image> --format '{{.Id}}'
```

3. 切换镜像前备份正式数据库。

```bash
BACKUP_DIR="${NEW_API_ARCHIVE_DIR:?set NEW_API_ARCHIVE_DIR}/compact-control-plane-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$BACKUP_DIR"
docker exec postgres pg_dump -U <db-user> -d <db-name> > "$BACKUP_DIR/pre-upgrade.sql"
```

4. 只将 `new-api` 应用容器切换到候选镜像。

首选方式：将 Compose 镜像变量更新为：

```text
NEW_API_IMAGE=<candidate-image>
```

然后只重启应用服务：

```bash
cd <repository-root>
NEW_API_IMAGE=<candidate-image> \
  docker compose -f <repository-root>/docker-compose.yml up -d --no-deps --force-recreate new-api
```

5. 立即执行正式验证。

```bash
docker exec new-api /new-api --build-info
curl -fsS http://127.0.0.1:13000/api/status
docker logs --tail 300 new-api 2>&1 | rg 'panic|fatal|synthetic compact state|input item type|chat compatibility mode|not implemented|No available channel|Service Unavailable|Invalid token| 503 ' || true
```

6. 真实 Codex 验证。

- 通过正式环境执行一个简短 Codex 请求。
- 条件允许时执行一个 Codex 历史恢复请求。
- 确认正式用量日志没有出现以下本地误报：
  - `503`
  - `not implemented`
  - `synthetic compact state not found or expired`
  - `input item type "compaction" is not supported in chat compatibility mode`

## 回滚计划

回滚触发条件：

- `/api/status` fails.
- `new-api` container becomes unhealthy or exits.
- Real Codex production requests return the known local false errors above.
- Logs show a new local panic or conversion error in the compact control path.

回滚命令：

```bash
cd <repository-root>
NEW_API_IMAGE=<rollback-image> \
  docker compose -f <repository-root>/docker-compose.yml up -d --no-deps --force-recreate new-api
docker exec new-api /new-api --build-info
curl -fsS http://127.0.0.1:13000/api/status
docker logs --tail 300 new-api 2>&1 | rg 'panic|fatal| 503 ' || true
```

受保护的回滚行为：

- 如果应用重建命令、`/api/status`、build-info 或近期日志验证失败，`scripts/compact-control-plane-prod-upgrade.sh --execute` 会自动尝试回滚到已记录的回滚镜像。
- 自动回滚后，重新尝试前仍必须人工检查。

本次仅升级代码，预期不需要数据库回滚。只有意外引入独立数据变更时，才使用 `pg_dump` 文件。

## 状态

已准备好等待明确的正式升级授权。

当前门禁状态：开发验证已完成，正式环境未被修改。
