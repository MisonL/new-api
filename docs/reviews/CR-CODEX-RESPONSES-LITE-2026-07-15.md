# Codex Responses Lite compatibility review

采样时间：2026-07-15，Asia/Shanghai。

## 范围

本次只检查正式环境当前启用且声明 `gpt-5.6-sol` 的渠道。复核时有效渠道为 `1、117、169、178、199`。`150、205、208` 已处于禁用状态，因此不纳入上线门禁：150 由用户确认暂不可用，205 的正式日志和开发副本均为上游余额不足，208 的正式日志主要为上游 429。

## 外部源码取证

- Codex stable `rust-v0.144.4`：commit `8c68d4c87dc54d38861f5114e920c3de2efa5876`。
- Codex alpha `rust-v0.145.0-alpha.13`：commit `b7a5de8ebf2770a0e21ad32d7820a29d0f0470ce`。
- 当前 Codex main：`2e1607ee2fa8099a233df7437adee5f16a741905`。
- 原版 new-api 当前 main：`a63364d156cf2a64f1c3d1ee4923d73d5f3222a1`。从此前审阅点起与本项目直接相关的变更包括 compact `prompt_cache_key` 透传及缓存计费边界修正；本项目已有等价字段和测试覆盖。
- sub2api PR #4082 已合并，确认 `additional_tools` 到 Chat bridge 的合并与回程识别方向；PR #3951 仍开放，主要处理上游错误、重试、上下文溢出和限流可靠性，不是 grammar 字段根因。

官方请求契约确认：Responses 根字段包括 `input`、`tools`、`tool_choice`、`reasoning`、`stream_options`、`include`、`prompt_cache_key`、`text`、`client_metadata`；Responses Lite 将工具放入 `input.additional_tools`。当前官方 reasoning 没有要求 `mode=adaptive`，只允许已声明的 effort/summary/context 组合；compact 后续请求使用 compact 返回的 `output` 替换历史，不依赖 REST `previous_response_id`。

## 已落地实现

- 原生 Responses 路由保持原样，只有明确配置为 Chat-only 的渠道才进入 Responses-to-Chat bridge。
- 新增/修正 reasoning、stream options、compact、嵌套未知字段和有效零值的无损 DTO 测试。
- 完整校验并保留 Codex 0.145 item ID 前缀；不匹配或无前缀 ID 只在输入规范化边界删除，`call_id` 保持不变。
- 工具计数使用顶层 `tools` 与 `input.additional_tools` 合并后的有效集合。
- 对明确不支持的 custom、namespace、tool-search 和 native compact 能力，调度器按渠道能力快照跳过；未知能力不静默降级。
- 修复 Responses 流中上游 `X-Reasoning-Included` 与 `X-Codex-Turn-State` 头的透传，提交 `7041da820`，并补充流扫描器测试。
- 新增 `scripts/codex-responses-lite-smoke.mjs` 的 core 门禁、长请求窗口、JSON/SSE 体识别、终止事件诊断和 Codex compact 历史替换语义。

## 开发环境协议结果

测试均通过隔离 3001，令牌通过 newapi `exec-token` 注入，未在输出或文件中暴露凭证。

| 渠道 | 结果 |
| --- | --- |
| 1（开发副本 219） | basic 通过；grammar 为 HTTP 200 completed 但 `output=[]`；namespace 只返回 `codex.rate_limits`/`codex.response.metadata` 后断流；controls 未按 JSON Schema 返回；tool-search `output=[]`；compact 23.74 秒通过。全程原生 Responses。 |
| 117 | basic、namespace、controls、compact 通过；grammar 被 bridge 明确拒绝并返回 503；tool-search 第一阶段可生成协议项，加载阶段上游返回 502。 |
| 169 | basic、grammar、namespace、controls、compact 通过；tool-search 返回普通 message，没有 `tool_search_call`。 |
| 178 | basic、grammar、controls、compact 通过；namespace 连续两次上游 Cloudflare 502，非网关转换错误。 |
| 199 | grammar、namespace、controls、compact 通过；tool-search 返回普通 message。最近新镜像 core 首请求连续遇到上游 503，未形成新的全量 core 通过记录。 |

所有已通过的 compact 用例均验证了 `compaction` 输出和后续 `/v1/responses` 历史替换续接；此前脚本使用 REST `previous_response_id` 造成的 400 已修正，不再作为压缩失败统计。

## 正式错误取证

最近采样窗口内 429 主要来自已禁用 208（10 次，上游 rate limit）及非 Codex DeepSeek 渠道配额；当前启用 Codex 渠道仅观察到 199 两次、178 一次上游并发限制。没有证据表明这些 429 由网关内部限流产生。

## 验证命令

```text
go test -timeout 120s ./controller ./model ./relay/common ./relay/helper ./service
node --check scripts/codex-responses-lite-smoke.mjs
git diff --check
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

后端回归在本轮 exit 0；镜像重建阶段默认和 classic 前端均构建成功，构建时镜像 commit 为 `309ae2664`。在生产升级前必须在最终提交（包含本审计文档和 smoke 门禁）上再次重建并核对 build-info，不能复用该旧 SHA 镜像。

## 尚未宣称完成的门禁

- 等上游 169/199 从 503 恢复后，在最终 SHA 镜像上重跑至少一次 basic 和 compact。
- 复核正式配置的渠道能力快照，只有有重复证据的明确不支持能力才写入 `responses_capability_registry`；不得通过禁用渠道代替能力路由。
- 生产升级前完成数据库备份、镜像 SHA 核对和应用容器替换；升级后观察 502、429、compact、tool call、reasoning header、item ID 日志。
