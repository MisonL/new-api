# Codex Responses Lite compatibility review

采样时间：2026-07-15，Asia/Shanghai。

## 范围

本次只检查正式环境当前启用且声明 `gpt-5.6-sol` 的渠道。渠道状态在排查期间发生过变化；第二次生产发布前于 2026-07-15 重新读取正式数据库，当前范围只有 `1、117、155、204`。`150` 由用户确认暂不可用；`205、169、178、199、208` 当前均未启用，因此不纳入本轮门禁。

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
| 1（最终开发副本 99201） | basic 13.22 秒通过；compact 4.45 秒并成功续写；grammar 为 HTTP 200 completed 但 `output=[]`，脚本因此判为上游未执行强制工具。此前 namespace 只返回 `codex.rate_limits`/`codex.response.metadata` 后断流，controls 未按 JSON Schema 返回，tool-search `output=[]`。全程原生 Responses。 |
| 117 | 最终 SHA 上 basic 12.20 秒、compact 16.35 秒并成功续写；grammar 被 Chat bridge 能力预检明确拒绝并快速返回 503。此前 namespace、controls 通过，tool-search 第一阶段可生成协议项，加载阶段上游返回 502。 |
| 155 | 最终 SHA 上 basic 13.03 秒、grammar 2.97 秒、controls 3.10 秒、compact 4.66 秒并成功续写，均通过。grammar 返回 `function_call apply_patch`。 |
| 204 | 最终 SHA 上 basic 23.15 秒、compact 4.80 秒并成功续写；grammar 首次上游瞬时 503，复测 6.43 秒成功并返回 `function_call apply_patch`。 |
| 169 | basic、grammar、namespace、controls、compact 通过；tool-search 返回普通 message，没有 `tool_search_call`。 |
| 178 | basic、grammar、controls、compact 通过；namespace 连续两次上游 Cloudflare 502，非网关转换错误。 |
| 199 | grammar、namespace、controls、compact 通过；tool-search 返回普通 message。最近新镜像 core 首请求连续遇到上游 503，未形成新的全量 core 通过记录。 |

所有已通过的 compact 用例均验证了 `compaction` 输出和后续 `/v1/responses` 历史替换续接；此前脚本使用 REST `previous_response_id` 造成的 400 已修正，不再作为压缩失败统计。

## 正式错误取证

最近采样窗口内 429 主要来自已禁用 208（10 次，上游 rate limit）及非 Codex DeepSeek 渠道配额；该窗口当时启用的 Codex 渠道中仅观察到 199 两次、178 一次上游并发限制。没有证据表明这些 429 由网关内部限流产生。

## 第一次生产发布与回滚

- 发布前备份：`backups/new-api-pre-codex-lite-d08a7de8-20260715-192054.dump`，87,931,907 bytes，SHA-256 `a5a56b0cc4b6cef8b938fc232d354aff3ef072417588ed48d9fe130bbb9eafcf`，`pg_restore -l` 可读取 405 个条目。
- 第一次发布镜像为 `new-api-local:prod-codex-lite-d08a7de8`，应用 build commit `d08a7de8ca9...`。固定 155 的 basic、grammar、controls、compact 均通过。
- 真实流量暴露出选择器缺陷：高优先级 199 或 169 返回上游 503 后，`retry=1` 直接切换到低优先级 117。117 的 Chat bridge 随后因 grammar 或 compact 能力不兼容被安全跳过，最终请求未继续尝试仍可用的同优先级原生 Responses 渠道。
- 发现后立即只回滚应用容器至 `new-api-local:prod-20260714-item-id-f1fe96d3e`，build commit `f1fe96d3efb570eb654f36fefbfece4c22d14d61`。数据库和渠道配置未变更，正式容器当前健康。

## 重试路由修复

- 提交 `803211365` 修正重试语义：每次重试先排除已使用渠道并检查当前分组最高优先级的剩余候选；只有同级候选穷尽后才按原有 retry 索引进入低优先级。总重试次数和显式协议能力错误保持不变。
- 单元与集成测试覆盖普通分组、auto 分组、数据库查询、内存缓存、同级继续和同级穷尽后降级。
- 隔离 3001 的真实路由试验使用三个临时渠道：99215 为不可达高优先级，99216 为健康同优先级，99217 为不可达低优先级。一次请求真实记录 99215 上游失败后由 99216 完成，99217 未被选择。试验结束后临时渠道、abilities 和 logs 均删除，残留计数为 0。

## 验证命令

```text
go test -count=1 -timeout 120s ./controller ./model ./relay/common ./relay/helper ./service
node --check scripts/codex-responses-lite-smoke.mjs
git diff --check
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

后端回归在本轮 exit 0；默认和 classic 前端、Safari 兼容检查、Go 二进制均构建成功。路由修复开发镜像 build commit 为 `803211365d9b1ab12f1785b76693dead1bbb69d3`，隔离容器状态为 healthy，`/api/status` 返回 success。提交本审计记录后仍必须从新的最终 HEAD 重建并再次核对 build-info。

## 尚未宣称完成的门禁

- 第二次生产发布前创建新备份并核对 `pg_restore -l`、文件大小和 SHA-256；镜像必须从包含本审计记录的最终 HEAD 重新构建，不能复用第一次发布镜像。
- 发布后固定验证当前启用的 `1、117、155、204`，并用自动路由请求确认高优先级上游失败时不会提前结束在低优先级协议不兼容渠道。
- 发布后持续观察 502、503、429、compact、tool call、reasoning header、item ID 和 retry channel chain。出现新网关回归时立即回滚应用容器；不得通过禁用渠道代替协议修复。
