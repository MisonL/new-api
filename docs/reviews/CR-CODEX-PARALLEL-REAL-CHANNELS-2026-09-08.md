# Codex parallel_tool_calls 真实渠道开发验证

## 范围

- 采样时间：2026-09-08 10:59-11:01 Asia/Shanghai。
- 正式 PostgreSQL 仅 SELECT 读取渠道 213、171、145；未修改正式配置或重启正式服务。
- 开发入口：http://127.0.0.1:3001。
- 正式 171 -> 开发 9229；正式 145 -> 开发 9230；正式 213 -> 开发 9231。
- 三个源渠道均为 type=1，不能用 Codex 专用 adaptor 单测替代验收。
- 保留源凭证、地址、模型、Header Profile 和参数覆盖；开发副本使用独立测试分组和名称，取消源 tag、重置统计并关闭自动禁用。没有覆盖已有开发 145、171。
- 145 的 model_mapping 字符串为 `null`。创建接口拒绝该值，因此先以空值创建，再仅对开发新记录恢复原值进行原样行为验证。

## 请求与结果

使用临时脚本调用 newapi Skill api.js，经指定渠道令牌后缀固定路由。凭证仅通过进程内存和标准输入传递，不保存到报告或仓库。

- 模型：gpt-6-astra。
- 请求：POST /v1/responses，stream=true、store=false、function tool、强制工具调用。
- 矩阵：Lite 请求头开/关各自对照 parallel_tool_calls=true/false，共 12 次。
- 补齐 Originator、User-Agent、Session_id、X-Client-Request-Id 和客户端生成的 X-Codex-Turn-Metadata。
- 如收到工具调用，计划回传 function_call_output 并校验完成标记；本轮没有进入该阶段。

| 正式源渠道 | 开发副本 | 4 项结果 | 原因 |
| --- | --- | --- | --- |
| 171 | 9229 | 全部 HTTP 500 | do_request_failed；日志显示 SSRF 拒绝域名解析地址 |
| 145 | 9230 | 全部 HTTP 500 | channel:model_mapped_error / unmarshal_model_mapping_failed |
| 213 | 9231 | 全部 HTTP 500 | do_request_failed；日志显示 SSRF 拒绝域名解析地址 |

第一次探针遗漏 x-codex-* 元数据而产生的 503 已作废，不作为渠道故障结论。补齐后结果如上。

## 核验命令

- `node /tmp/newapi-parallel-probe.6oFlL1/probe.cjs`：退出码 0 仅表示矩阵和清理执行完毕；业务请求 12/12 失败，不能判定测试通过。
- `docker logs --since 3m --tail 160 new-api-dev-isolated-new-api-1` 定向筛选：日志含 SSRF destination validation failed / private IP address not allowed。
- `dig +time=2 +tries=1 +short api.shenwenai.com`：退出码 0，198.18.0.153。
- `docker exec new-api-dev-isolated-new-api-1 getent hosts api.shenwenai.com`：退出码 0，198.18.0.153。
- `dig @1.1.1.1 +time=2 +tries=1 +short api.shenwenai.com`：退出码 0，同一地址；不能据此证明拿到未被代理接管的公共 DNS 结果。
- 开发 SQL 核验：9229、9230、9231 状态均为 2，测试令牌未删除记录数为 0。

## 结论和边界

渠道复制已完成，副本禁用保留；临时令牌已撤销。未修改 SSRF 策略，未修改正式环境。198.18.0.153 属于基准测试保留网段，代理 Fake-IP 是待确认假设。

本轮未触达上游 Responses-Lite 参数校验，不能证明 true 被正确归一化，也不能证明 false 可用。没有真实 Codex App 端到端运行证据。本轮没有修改业务代码、提交或推送。

## 正式服务最小补丁实施（追加）

- 用户已确认实施。以正式运行提交 `4527d7666` 为基线，在临时目录构建镜像 `new-api-local:prod-codex-lite-4527d7666-litefix`。
- 补丁仅修改 `relay/channel/openai/adaptor.go`：请求带 `X-OpenAI-Internal-Codex-Responses-Lite: true` 时强制序列化 `parallel_tool_calls=false`；普通 OpenAI Responses 保留客户端值。
- 补丁基线专项测试通过：`go test ./relay/channel/openai -run 'TestConvertOpenAIResponses' -count=1`。
- 构建成功，镜像构建指纹为 `4527d7666a37f9151a57f08ca0bc5e8e08b8fb46-litefix`。
- 使用 `/tmp` 临时 Compose override 仅替换 `new-api` 镜像并执行 `docker compose -f docker-compose.yml -f /tmp/new-api-prod-lite-override.yml up -d --no-deps --force-recreate new-api`；PostgreSQL、Redis、数据卷和网络未重建。
- 正式容器核验：`healthy`、`restart=0`；`docker exec new-api /new-api --build-info` 返回 `v1.1.0` 与 `4527d7666...-litefix`；`curl --connect-timeout 3 --max-time 10 http://127.0.0.1:13000/api/healthz` 返回 `ok`。
- 本次未做正式渠道带业务额度的端到端请求，因此“容器已运行最小补丁”与“171/145 真实业务已恢复”分开记录；正式业务验收仍需实际 Codex 客户端请求或受控渠道请求证据。
