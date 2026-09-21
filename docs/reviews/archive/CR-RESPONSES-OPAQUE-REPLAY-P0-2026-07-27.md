# CR-RESPONSES-OPAQUE-REPLAY-P0-2026-07-27

## 审计范围

- 采样时间：2026-07-27 01:57 CST
- 仓库 HEAD：`b36cd3e2367ccbb2e8ca4caea13324057acee9a6`
- `origin/main`：`51074ac3736288ee664966e0a2109e9580d86fcf`
- 运行环境：完全隔离 Docker 开发栈 `127.0.0.1:3001`
- 正式环境：冻结，本次未部署、未改渠道、未改正式数据库

## 问题与根因

Codex Responses 会话会把上游返回的 opaque `encrypted_content` 带入下一轮请求。该状态不是普通可重建文本，任何删除、结构化重编码、Responses-to-Chat 转换、synthetic 恢复或 413 缩身都会破坏连续推理上下文，表现为会话遗忘、工具状态丢失或能力下降。

原链路缺少统一的 opaque 不变量：

- 渠道路由没有在所有 Responses 和 Responses Compact 入口检查 encrypted replay 与 compaction item passthrough 能力。
- 非字符串 `encrypted_content` 没有作为未来协议的 opaque 值统一分类。
- Chat 转换、兼容重试、413 恢复、synthetic compact 和 adaptor 转换存在各自的处理入口，缺少同一个不可变条件约束。
- 旧测试允许通过删除 encrypted reasoning 或改走 Chat 获得成功，不能证明 Codex 状态仍完整。

## 修复不变量

- opaque 定义为非空且非 synthetic marker 的字符串，或任意非 `null` 的非字符串 JSON 值。
- opaque 请求除必要的模型映射外，不修改原始请求体。
- opaque 请求不进入 Responses-to-Chat、encrypted-content 删除重试、namespace/arguments 兼容重试、413 缩身、synthetic compact 或 Codex/OpenAI adaptor 重写。
- 明确不支持 encrypted replay 的渠道在路由阶段跳过并记录 `unsupported_encrypted_reasoning_replay`。
- 明确不支持 compaction item passthrough 的渠道在路由阶段跳过并记录 `unsupported_compaction`。
- 未知 replay 能力保持原生 Responses 透传并记录未验证状态，不做隐藏降级。
- 回程保留上游 `encrypted_content` 和未知未来字段，不把响应 DTO 的解析结果重新序列化为客户端响应。

## 代码与测试覆盖

- `relay/common/responses_input_filter.go`：统一 opaque 分类，覆盖字符串、对象、数组、数字、布尔值、`null`、空值和 synthetic marker。
- `dto/channel_settings.go`、`relay/common/responses_encrypted_replay.go`：解析 encrypted replay 和 native compact 能力。
- `controller/relay.go`：在初始渠道和重试渠道选择中执行能力筛选并记录路由原因。
- `relay/responses_handler.go`：opaque 原始 body 构建，以及 Chat、重试、413、summary-model override 护栏。
- `relay/responses_via_chat.go`：在实际 Chat 转换入口再次拒绝 opaque replay。
- `relay/channel/openai`、`relay/channel/codex`：opaque 请求跳过 adaptor 改写；非流式和流式响应保留原始上游载荷。
- `scripts/compact-control-plane-fake-upstream.mjs`：只保存请求结构摘要和 SHA-256，不保存正文、密文或 Authorization。
- `scripts/compact-control-plane-e2e.sh`：覆盖非流式 Responses、流式 Responses、Responses Compact 和明确不支持渠道的零上游调用。

## 验证记录

### L0 和 L1

```text
node --test scripts/compact-control-plane-fake-upstream.test.mjs
exit=0, tests=1, pass=1

bash -n scripts/compact-control-plane-e2e.sh
exit=0

go test ./dto ./relay/common ./relay/channel/openai ./relay/channel/codex ./relay ./controller ./model ./service ./middleware -count=1 -timeout 120s
exit=0, 9 packages passed

go test ./relay/helper -count=1 -timeout 120s
exit=0

go test ./relay/common ./relay/channel/openai ./relay/channel/codex ./relay ./controller ./service ./middleware -run 'Opaque|EncryptedReasoningReplay|EncryptedContent|PreservesOpaque|RejectsOpaque|NativeOpaque' -count=3 -timeout 180s
exit=0, 7 packages passed for 3 consecutive runs

git diff --check
exit=0
```

### 镜像与隔离环境

```text
scripts/build-docker-local.sh new-api-local:dev
exit=0

version=v1.1.0
commit=b36cd3e2367ccbb2e8ca4caea13324057acee9a6-dirty
date=2026-07-26T17:14:22Z
source=https://github.com/MisonL/new-api

docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env ps
new-api=healthy
postgres=healthy
redis=healthy

curl -fsS --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status
exit=0, success=true
```

镜像首次拉取曾因 Docker Hub token 请求 `EOF` 失败，重试后完整构建成功。E2E 重启容器期间出现短暂 `curl: (56) Recv failure: Connection reset by peer`，随后受限 host bridge probe 成功，所有业务断言通过；该现象发生在强制重启窗口内，不是请求场景失败。

### L2 协议门禁

```text
COMPACT_E2E_CASES=native_opaque_passthrough,generic_openai_rejects_remote_opaque_compaction scripts/compact-control-plane-e2e.sh
exit=0

native_opaque_nonstream_upstream_calls=1 body_sha256_match=true path=/v1/responses
native_opaque_nonstream_client_encrypted_content_preserved=true

native_opaque_stream_upstream_calls=1 body_sha256_match=true path=/v1/responses
native_opaque_stream_client_encrypted_content_preserved=true

native_opaque_compact_upstream_calls=1 body_sha256_match=true path=/v1/responses/compact
native_opaque_compact_client_encrypted_content_preserved=true

generic_openai_rejects_remote_opaque_compaction status=503
generic_openai_rejects_remote_opaque_compaction_upstream_calls=0

cleanup: database state restored
cleanup: dev container restarted
```

隔离容器日志扫描未命中测试密文标记、`encrypted_content`、synthetic marker、request body 或 cipher 关键字。

## 结论与边界

开发环境已经证明 opaque Codex Responses 状态在三条原生链路中上下行保持完整，明确不兼容渠道在发起上游请求前失败，不再通过删除状态或转换协议制造成功。

当前制品包含仓库原有的大量未提交改动，build-info 明确标记为 `dirty`。本记录证明 P0 语义和隔离运行时门禁通过，不代表整个长期 Responses Lite 目标完成，也不构成正式环境升级批准。真实第三方上游能力仍需在独立生产升级门禁中验证。
