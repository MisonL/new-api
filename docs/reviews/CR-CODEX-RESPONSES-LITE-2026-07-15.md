# Codex Responses Lite 兼容性审查

采样时间保存在私有验证记录中。

## 范围

本次只检查受控验证环境中启用且声明目标模型的渠道。渠道标识、启用状态和临时可用性均属于运行环境数据，保存在私有验证记录中，不纳入公开仓库。

## 外部源码取证

- Codex 稳定版 `rust-v0.144.4`：commit `8c68d4c87dc54d38861f5114e920c3de2efa5876`。
- Codex alpha 版 `rust-v0.145.0-alpha.13`：commit `b7a5de8ebf2770a0e21ad32d7820a29d0f0470ce`。
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

| 测试渠道类别 | 结果 |
| --- | --- |
| 原生 Responses 候选 | basic 和 compact 成功；部分强制工具或 tool-search 输出由上游能力决定，脚本将缺失协议项判为未通过，而非伪造成功。 |
| Chat bridge 候选 | 不支持的 grammar 或 compact 在能力预检阶段明确返回可分类错误；上游 502/503 保持为上游故障，不归类为网关转换错误。 |
| 完整能力候选 | basic、grammar、controls 和 compact 通过，grammar 回程保持 `function_call` 语义。 |
| 受限能力候选 | tool-search 仅返回普通 message 时不视为工具调用成功，避免将能力缺失误判为协议通过。 |

所有已通过的 compact 用例均验证了 `compaction` 输出和后续 `/v1/responses` 历史替换续接；此前脚本使用 REST `previous_response_id` 造成的 400 已修正，不再作为压缩失败统计。

## 正式错误取证

受控采样中观察到的 429 来自上游 rate limit、并发限制或其他渠道配额；没有证据表明这些 429 由网关内部限流产生。具体渠道和计数保存在私有验证记录中。

## 第一次生产发布与回滚

- 发布前创建了可读取的 PostgreSQL 备份；文件名、大小、哈希、恢复演练和镜像 digest 保存在私有部署审计中。
- 首次候选在固定路由的 basic、grammar、controls 和 compact 场景通过。
- 真实流量暴露出选择器缺陷：高优先级候选返回上游 503 后，`retry=1` 直接切换到低优先级、协议不兼容的候选，未继续尝试同优先级可用原生 Responses 候选。
- 发现后只回滚应用容器；数据库和渠道配置未变更。实际镜像和 revision 保存在私有部署审计中。

## 重试路由修复

- 提交 `803211365` 修正重试语义：每次重试先排除已使用渠道并检查当前分组最高优先级的剩余候选；只有同级候选穷尽后才按原有 retry 索引进入低优先级。总重试次数和显式协议能力错误保持不变。
- 单元与集成测试覆盖普通分组、auto 分组、数据库查询、内存缓存、同级继续和同级穷尽后降级。
- 隔离 3001 的真实路由试验使用三个临时渠道：不可达高优先级、健康同优先级和不可达低优先级。一次请求在高优先级失败后由健康同优先级候选完成，低优先级候选未被选择。试验结束后临时渠道、abilities 和 logs 均删除，无残留。

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

后端回归在本轮退出码为 0；default 和 classic 前端、Safari 兼容性检查、Go 二进制均构建成功。路由修复开发镜像的 build-info 与候选源码一致，隔离容器状态为 healthy，`/api/status` 返回 success。提交本审计记录后仍必须从新的最终 HEAD 重建并再次核对 build-info。

## 尚未宣称完成的门禁

- 第二次生产发布前创建新备份并核对 `pg_restore -l`、文件大小和 SHA-256；镜像必须从包含本审计记录的最终 HEAD 重新构建，不能复用第一次发布镜像。
- 发布后固定验证受控的启用渠道集合，并用自动路由请求确认高优先级上游失败时不会提前结束在低优先级协议不兼容渠道。
- 发布后持续观察 502、503、429、compact、tool call、reasoning header、item ID 和 retry channel chain。出现新网关回归时立即回滚应用容器；不得通过禁用渠道代替协议修复。
