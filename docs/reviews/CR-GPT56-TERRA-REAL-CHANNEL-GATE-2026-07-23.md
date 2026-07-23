# GPT-5.6 Terra 真实渠道门禁记录

## 结论

`gpt-5.6-terra` 已在当前正式库启用渠道的只读快照上完成隔离环境真实门禁。渠道 `75` 和 `178` 的 Codex CLI 工具任务、基线压缩和 33 MiB 工具定义压缩均通过。渠道 `150` 的压缩路径通过，但 Codex CLI 被已配置的 `unsupported_namespace_tools` 能力规则显式拒绝。渠道 `72` 持续返回 `503 unknown_error`。渠道 `216` 在不同采样中出现 `429 rate_limit_exceeded` 和超大压缩 `502 bad_response_status_code`。

这不是全渠道发布绿灯，正式服务未升级。所有超大请求均执行了本地 Codex ingress 压缩，未观察到本地 HTTP `413` 或压缩伪成功。

## 范围和环境

- 采样日期：2026-07-23。
- 模型：`gpt-5.6-terra`。
- 正式数据源：正式 PostgreSQL 的只读、可重复读快照。
- 隔离服务：`new-api-dev-isolated-new-api-1`，Docker 网络内入口 `http://new-api:3000`。
- 隔离构建：`8af2cd0e84c9506c83cc7dff81f16421b8cf80da-dirty`。
- 正式服务：未升级，未写入渠道、能力、选项或用户数据。
- 渠道 `204` 按当前运行要求显式排除。

候选发现命令：

```text
CODEX_REAL_GATE_MODEL=gpt-5.6-terra \
CODEX_REAL_GATE_EXCLUDE_CHANNEL_IDS=204 \
bash scripts/codex-real-channel-compaction-gate.sh --list-production-candidates
```

输出：

```text
production_eligible_channels=72,75,150,178,216
```

## 全量门禁

执行：

```text
CODEX_REAL_GATE_MODEL=gpt-5.6-terra \
CODEX_REAL_GATE_EXCLUDE_CHANNEL_IDS=204 \
CODEX_REAL_GATE_RESULT_FILE=/tmp/new-api-terra-real-gate-results.txt \
bash scripts/codex-real-channel-compaction-gate.sh
```

每个候选覆盖以下请求：

1. Codex CLI 真实工具任务，使用 Responses 协议。
2. 基线 `/v1/responses/compact` 请求。
3. 带 33 MiB 工具定义的 `/v1/responses/compact` 请求。

| 渠道 | Codex CLI | 基线压缩 | 超大压缩 | 结论 |
| --- | --- | --- | --- | --- |
| 72 | `503 unknown_error` | `503 unknown_error` | `503 unknown_error` | 渠道或上游不可用。本地超大请求已压缩后再发出。 |
| 75 | 通过 | `200 response.completed` | `200 response.completed` | Terra 的真实工具和压缩路径通过。 |
| 150 | `503 channel:model_mapped_error`，`unsupported_namespace_tools` | `200 response.completed` | `200 response.completed` | CLI 被明确能力策略拒绝，不是压缩故障。 |
| 178 | 通过 | `200 response.completed` | `200 response.completed` | Terra 的真实工具和压缩路径通过。 |
| 216 | 通过 | `429 rate_limit_exceeded` | `429 rate_limit_exceeded` | CLI 可用，连续压缩采样遇到渠道限流。 |

超大压缩请求的原始请求体为 `34,603,607` 字节，门禁记录的压缩后请求体为 `1,292` 字节。通过行只有在 `ingress_reduced=true`、`compact_mode=codex_local` 和流状态满足门禁断言时才会计为通过。失败行也记录了相同的本地 ingress 压缩结果，因此 `72` 和 `216` 的失败不属于本地请求体过大。

外层执行器没有保留全量脚本的原始退出码，但结果文件包含失败阶段，不能将本轮表述为全绿。

## 渠道 216 冷却后复测

为区分连续采样限流与可复现故障，冷却后仅导入 `216`，显式排除 `72,75,150,178,204`：

```text
CODEX_REAL_GATE_MODEL=gpt-5.6-terra \
CODEX_REAL_GATE_EXCLUDE_CHANNEL_IDS=72,75,150,178,204 \
CODEX_REAL_GATE_RESULT_FILE=/tmp/new-api-terra-216-retest-results.txt \
bash scripts/codex-real-channel-compaction-gate.sh
```

结果：

- Codex CLI：`429 rate_limit_exceeded`。
- 基线压缩：`200 response.completed`。
- 超大压缩：`502 bad_response_status_code`，同时记录 `ingress_reduced=true`、原始 `34,603,607` 字节、压缩后 `1,292` 字节和 `compact_mode=codex_local`。

这证明 `216` 的基线压缩并非稳定失败，但其真实上游或渠道状态不足以通过 Terra 的完整门禁。该 502 不能在没有上游响应证据的情况下归因于本地压缩实现。

## 清理和恢复

两次门禁均报告：

```text
cleanup=passed isolated_redis_recreated=true isolated_app_network_ready=true isolated_app_status_verified=true
```

独立复核结果：

- 隔离 PostgreSQL 中 `codex_real_gate_%` 临时表数量为 `0`。
- 门禁临时用户、令牌和用量日志数量均为 `0`。
- 隔离应用和 Redis 最终为 `healthy`。
- Docker 网络内 `GET /api/status` 返回 `200`。
- 应用重启后首个 Docker healthcheck 曾发生一次 `wget` 超时，随后下一次检查通过；最终没有残留健康失败。

## 本轮附加验证

以下命令均退出码为 `0`：

```text
bash -n scripts/codex-real-channel-compaction-gate.sh
node --test scripts/codex-real-channel-compaction-gate.test.mjs
git diff --check
```

## 发布边界

- 本记录证明 `75` 和 `178` 在本次采样中可完成 Terra 的真实 Codex 工具与压缩工作流。
- `150` 不应承担带 namespace tools 的 Codex CLI 流量，除非其能力配置和上游支持发生可验证变化。
- `72`、`216` 仍不是 Terra 全量发布绿灯；需要把渠道可用性与上游限流或 502 单独处理。
- 本轮没有修改生产配置、没有升级正式服务，也没有把渠道失败静默绕过为成功。
