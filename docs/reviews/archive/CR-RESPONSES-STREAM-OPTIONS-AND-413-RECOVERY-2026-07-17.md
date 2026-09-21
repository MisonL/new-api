# Responses stream_options 与远程压缩 413 恢复验证

采样时间：2026-07-17，Asia/Shanghai。

## 范围

本次处理两类来自 Codex Responses 流量的确定性失败：

- OpenAI 兼容渠道在 SSE 已发送 `response.created` 和 `response.in_progress` 后，以 `400 unknown_parameter` 拒绝 `stream_options.include_usage`。
- 带 `remote_compaction_v2` 和本地 `compaction_trigger` 的流式 Responses 请求被上游以 `413 upstream_request_too_large` 拒绝，原先不进入本地 synthetic compact 恢复。

未改变普通 Responses、普通 413、429、opaque compaction 或已经输出语义内容后的重试行为。

## 根因与处理

### stream_options.include_usage

Codex 会在流式 Responses 请求中发送 `stream_options.include_usage=true`。部分上游接受 Responses 路径但未实现这个可选字段，返回的错误发生在 SSE 前导事件之后。仅依赖 `Writer.Written()` 会将前导事件误判为已向客户端输出业务内容，导致不能安全重试。

处理规则：

- 仅在 OpenAI 兼容原生 Responses 路径、HTTP 400、参数精确为 `stream_options.include_usage` 且错误语义为 unknown 或 unsupported parameter 时触发。
- 同一渠道只重试一次，只删除 `include_usage`，保留其他 `stream_options` 字段、工具、历史、reasoning 和原生 Responses 路径。
- 成功恢复后，将渠道能力记录为 `supports_stream_options_include_usage=false`。管理员显式配置为 true 时不会被自动覆盖。
- 重试和字段剥离标记绑定实际渠道 ID。若该渠道后续失败并切换到其他渠道，不能把能力结论或日志标记带到新渠道。
- 后续请求在结构化转换和 `param_override` 后各执行一次剥离，防止覆盖规则重新注入该字段。
- 使用日志记录 `responses_stream_options_include_usage_retry`、`responses_stream_options_include_usage_capability_stripped` 和能力快照。

### 远程压缩 413

`413` 只有在错误码已经由上游 HTTP 状态标准化为 `upstream_request_too_large` 时才进入恢复。恢复还要求：原生 OpenAI Responses、流式请求、本地 `compaction_trigger`、请求头包含 `remote_compaction_v2`、未写出业务事件且不含远程 opaque compaction 状态。

满足条件时，保留原请求上下文并切换至既有 synthetic compact 总结链路，同时不把该语义性 413 写入 `request_body_limit`，避免后续请求错误地绕开该渠道。普通 413、未分类 413、429、已输出语义内容和 opaque 状态不会被吞掉或改写。

## 回归覆盖

- `relay/responses_handler_test.go` 覆盖 SSE 前导事件后字段拒绝、同渠道二次请求仅删除目标字段、前导 response ID 和错误文本不泄漏到客户端、流状态重置。
- `model/channel_responses_stream_options_capability_test.go` 覆盖未知能力持久化、缓存更新和显式支持配置不被观察结果覆盖。
- `controller/relay_responses_tool_observation_test.go` 覆盖恢复成功后的控制器持久化路径。
- `controller/relay_retry_test.go` 覆盖类型化 413 可以恢复，未分类 413 不可以恢复。
- `controller/relay_responses_tool_observation_test.go` 覆盖渠道切换不会误学习 `include_usage` 能力，以及可恢复 remote-compaction 413 不会写入渠道请求体上限。
- `scripts/compact-control-plane-e2e.sh` 新增两个隔离端到端场景：
  - SSE 前导事件后 `include_usage` 400，首请求两次上游调用，能力写入 false，第二次请求预剥离并只调用一次上游。
  - remote compaction 首次 413，随后 synthetic compact 返回 200，第二次上游请求不再含 `compaction_trigger`，且测试渠道未写入 `request_body_limit`。

CodeRabbit 审查发现的相邻加密上下文覆盖缺口也已处理：agent message 重试断言恢复标记，单条 reasoning 不会被剥离为空输入，日志明确 `mode=targeted_item` 或 `generic_reasoning`。

## 验证记录

以下命令均在本轮工作树执行并以退出码 0 完成：

| 命令 | 退出码 | 结果摘要 |
| --- | --- | --- |
| `go test ./dto ./controller ./model ./relay ./relay/common ./relay/helper ./service -count=1 -timeout=120s` | 0 | 7 个包全部通过。 |
| `go test -race ./controller ./model ./relay -count=1 -timeout=180s` | 0 | 竞态检测通过。 |
| `go vet ./dto ./controller ./model ./relay ./relay/common ./relay/helper ./service` | 0 | 无 vet 诊断。 |
| `node --check scripts/compact-control-plane-fake-upstream.mjs` | 0 | 假上游脚本语法通过。 |
| `bash -n scripts/compact-control-plane-e2e.sh` | 0 | E2E 主脚本语法通过。 |
| `bash -n scripts/compact-control-plane-e2e-lib.sh` | 0 | E2E 数据准备脚本语法通过。 |
| `git diff --check` | 0 | 无空白或补丁格式错误。 |

独立审查边界：CodeRabbit 首轮发现审计记录证据不够精确，已按本节修订；relay 复核发现显式 `include_usage:false` 的字段存在性缺口，已修复并补测试，后续 CodeRabbit 调用受公共仓库额度限制。Claude 只读复核发现无引号错误消息可能对子串误匹配，已改为完整参数 token 比较并补正反用例。两项修复后重新执行了相关 Go 测试。

提交前隔离镜像和容器快照：

```text
image=new-api-local:dev
image_id=sha256:1eb2ee598eb02fcceb4638bf59df3fa3c1fdcf696145b90c41583bf449b3e19f
build_commit=4527d7666a37f9151a57f08ca0bc5e8e08b8fb46-dirty
build_date=2026-07-17T07:51:48Z
container=new-api-dev-isolated-new-api-1
endpoint=http://127.0.0.1:3001/api/status
container_health=healthy
api_status_success=true
```

该镜像在提交前从本轮全部未提交源码构建，`-dirty` 明确表示它不是 Git 提交版本。容器健康和 `/api/status` 成功只说明运行可用，不单独证明版本同步；提交后仍需按最终提交重新构建并复核 build-info。

最终端到端命令使用独占假上游端口 `18082`，并以退出码 0 完成：

```text
COMPACT_E2E_UPSTREAM_HOST_URL=http://127.0.0.1:18082 \
COMPACT_E2E_UPSTREAM_CONTAINER_URL=http://host.docker.internal:18082 \
COMPACT_E2E_FAKE_UPSTREAM_PORT=18082 \
COMPACT_E2E_CLEANUP_DELETE_LOGS=1 \
scripts/compact-control-plane-e2e.sh
```

脚本输出确认以下目标语义：首次 `include_usage` 请求两次上游调用且第二次仅剥离目标字段；能力记录为 false 后，第二次业务请求仅一次上游调用；remote compaction 的首个 413 返回 synthetic compact，第二次请求不含 `compaction_trigger`，且 `request_body_limit` 未写入。

脚本结束后使用以下独立命令复核清理，均以退出码 0 完成：

```text
docker inspect -f '{{.State.Health.Status}}' new-api-dev-isolated-new-api-1
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status | jq -r '.success'
docker exec new-api-dev-isolated-postgres-1 psql -U root -d new-api-dev -At -c "select count(*) from pg_tables where schemaname = 'public' and tablename like 'compact_e2e_20260717155525_9505_%';"
if lsof -nP -iTCP:18082 -sTCP:LISTEN >/dev/null 2>&1; then exit 1; fi
```

结果分别为 `healthy`、该预提交 `-dirty` build-info、`true`、`0` 和无假上游监听。E2E 使用本地假上游，只覆盖网关的请求、重试、状态学习和 synthetic compact 语义；不等同于真实上游或正式环境已验证。

## 上游对比

2026-07-17 通过 GitHub API 获取的原版 `QuantumNous/new-api` 最新提交为 `a63364d156cf`。其最近与 Responses 相关的 `92d3c9d18` 处理缓存计费和 `prompt_cache_key`，没有渠道级 `stream_options.include_usage` 能力学习或本次 SSE 前导重试处理。

`Wei-Shaw/sub2api` PR #3951 在采样时仍为 open。该 PR 将上下文超限等确定性错误保留为客户端语义，并修复读取失败前的账号故障转移；它没有解决字段级 `stream_options.include_usage` 不兼容。当前实现只吸收其“不要把确定性输入错误泛化为 502/503”的边界，未将所有 413 或所有 Responses 错误改为 synthetic 成功。

## 正式发布门禁

本记录只覆盖最终开发镜像和隔离环境。正式服务尚未修改。发布前仍需完成：

- 对正式 PostgreSQL 创建可恢复备份并验证 `pg_restore -l`。
- 使用最终提交构建正式镜像，部署前记录旧镜像 ID 和 build-info 作为回滚锚点。
- 升级后固定验证原生 `/v1/responses`、Codex 工具调用、remote compaction 和错误日志标记。
- 观察 502、413、429、工具丢失、`stream_options` 重试和 synthetic compact 记录。出现语义输出后重试、普通 413 被改写、工具丢失或错误率异常时立即回滚应用容器。
