# Agnes 渠道模型兼容性与正式价格修正审计

日期：2026-06-09

## 结论

- 目标 Agnes 渠道当前承载的模型为：
  - `agnes-1.5-flash`
  - `agnes-2.0-flash`
  - `agnes-image-2.0-flash`
  - `agnes-image-2.1-flash`
  - `agnes-video-v2.0`
- 本次按你的要求采用官方原价，不使用页面展示的当前促销价 `0`。
- 已核对官方文档、仓库计费公式、正式库现状与正式运行态，结论如下：
  - 文本模型 `agnes-1.5-flash`、`agnes-2.0-flash` 兼容 OpenAI `chat/completions`，并实测兼容 `responses`
  - 图片模型 `agnes-image-2.0-flash`、`agnes-image-2.1-flash` 需要 Agnes 特有的 `extra_body.response_format` 约定
  - 视频模型 `agnes-video-v2.0` 走异步任务接口 `/v1/videos`
- 仓库已补齐 Agnes 图片模型兼容补丁并通过最小相关 Go 测试；`3001` 隔离环境已重建到包含该补丁的当前工作区镜像。
- 正式库价格配置、历史消费日志、小时聚合、用户/令牌/渠道额度与视频任务预扣记录，已全部按官方原价修正。
- 正式运行态已同步到新价格。2026-06-09 再次通过正式网关发起 `agnes-1.5-flash` 请求，新日志按 `ModelRatio=0.035` 正确落库。
- 二次复核时发现 1 条促销价阶段验证日志仍为 `0` 价，已补回正式价并同步小时聚合、余额与缓存。

## 官方调用方式核对

来源页面：

- https://agnes-ai.com/doc/agnes-15-flash
- https://agnes-ai.com/doc/agnes-20-flash
- https://agnes-ai.com/doc/agnes-image-20-flash
- https://agnes-ai.com/doc/agnes-image-21-flash
- https://agnes-ai.com/doc/agnes-video-v20

结论：

- `agnes-1.5-flash`
  - `POST /v1/chat/completions`
  - 兼容 OpenAI Chat Completions
  - 实测 `POST /v1/responses` 可用
- `agnes-2.0-flash`
  - `POST /v1/chat/completions`
  - 兼容 OpenAI Chat Completions
  - 实测 `POST /v1/responses` 可用
- `agnes-image-2.0-flash`
  - `POST /v1/images/generations`
  - 图片 URL 输出必须写到 `extra_body.response_format = "url"`
  - 文生图 base64 输出用顶层 `return_base64 = true`
  - 图生图 base64 输出用 `extra_body.response_format = "b64_json"`
- `agnes-image-2.1-flash`
  - `POST /v1/images/generations`
  - 与 `agnes-image-2.0-flash` 的 `response_format` 规则一致
- `agnes-video-v2.0`
  - 创建任务：`POST /v1/videos`
  - 查询任务：`GET /agnesapi?video_id=<VIDEO_ID>`
  - 兼容旧查询路径：`GET /v1/videos/{task_id}`

## 官方原价口径

页面同时展示了当前促销价和原价。本次正式库采用原价：

- `agnes-1.5-flash`
  - 输入：`$0.07 / 1M tokens`
  - 输出：`$0.15 / 1M tokens`
- `agnes-2.0-flash`
  - 输入：`$0.10 / 1M tokens`
  - 输出：`$0.20 / 1M tokens`
- `agnes-image-2.0-flash`
  - `$0.003 / image`
- `agnes-image-2.1-flash`
  - `$0.003 / image`
- `agnes-video-v2.0`
  - `$0.005 / second`

按仓库 `docs/operations/pricing-maintenance.md` 的换算规则：

- `ModelRatio = input_usd_per_1M / 2`
- `CompletionRatio = output_usd_per_1M / input_usd_per_1M`
- `ModelPrice = 官方按次或按量价格`

因此正式配置应为：

- `agnes-1.5-flash`
  - `ModelRatio = 0.035`
  - `CompletionRatio = 2.142857142857143`
- `agnes-2.0-flash`
  - `ModelRatio = 0.05`
  - `CompletionRatio = 2`
- `agnes-image-2.0-flash`
  - `ModelPrice = 0.003`
- `agnes-image-2.1-flash`
  - `ModelPrice = 0.003`
- `agnes-video-v2.0`
  - `ModelPrice = 0.005`

## 仓库兼容性结论

### 文本模型

- 当前仓库和正式服务都兼容 `agnes-1.5-flash`、`agnes-2.0-flash`。
- 2026-06-09 复核时，上游直连和正式网关都返回 `200`。

### 图片模型

- Agnes 图片模型与常规 OpenAI 图片接口有两个关键差异：
  - `response_format` 不能放顶层，URL 输出必须放到 `extra_body.response_format`
  - 图片编辑需要统一走 `/v1/images/generations`
- 已在仓库中补齐兼容逻辑：
  - `dto/openai_image.go`
  - `relay/channel/openai/adaptor.go`
  - `relay/channel/openai/agnes_image_test.go`
- 新增行为：
  - Agnes 文生图 `url` 输出自动改写到 `extra_body.response_format`
  - Agnes 文生图 `b64_json` 自动改写为 `return_base64=true`
  - Agnes 图生图 JSON 输入自动搬运到 `extra_body.image`
  - Agnes 图生图统一改写到 `/v1/images/generations`
  - Agnes multipart 图片编辑显式报错，不做静默降级
- 验证命令：

```bash
go test ./relay/channel/openai ./relay/helper ./relay/common
```

### 视频模型

- 仓库已有 `POST /v1/videos` 异步任务链路。
- 正式库历史上已存在 `agnes-video-v2.0` 成功任务记录，说明主链路可走通。
- 本次未补视频协议代码；本次修正重点在价格与历史额度。

## 正式库修正前事实

修正前，Agnes 相关模型没有正确价格配置：

- `ModelRatio`
  - `agnes-1.5-flash = 0`
  - `agnes-2.0-flash = 0`
- `ModelPrice`
  - `agnes-image-2.0-flash = 0`
  - `agnes-image-2.1-flash = 0`
  - `agnes-video-v2.0 = 0`

受影响历史记录共 `6` 条。它们之前已被写成促销价 `0`，与现在要求的正式原价不符。

## 历史重算口径

按仓库真实计费代码复核：

- 文本请求走 `service/text_quota.go`
  - `new_quota = round((prompt_tokens + completion_tokens * completion_ratio) * model_ratio * group_ratio)`
- 图片请求走 `service/text_quota.go` 的 `UsePrice` 分支
  - `new_quota = round(model_price * QuotaPerUnit * group_ratio * n)`
  - `QuotaPerUnit = 500000`
  - 本次受影响图片日志 `n = 1`
- 视频任务走 `relay/relay_task.go` 和 `service/task_billing.go`
  - 基础额度：`model_price * QuotaPerUnit * group_ratio`
  - 再乘 `other_ratios.seconds`
  - 本次任务 `seconds = 4`

因此 6 条历史记录的重算结果为：

- `log:<anon-1> agnes-image-2.1-flash = 1500`
- `log:<anon-2> agnes-1.5-flash = 9`
- `log:<anon-3> agnes-video-v2.0 = 10000`
- `log:<anon-4> agnes-image-2.0-flash = 1500`
- `log:<anon-5> agnes-image-2.1-flash = 1500`
- `log:<anon-6> agnes-2.0-flash = 12`

合计修正差额：

- 用户维度：`+14521`
- 令牌维度：`+13021`
- 渠道维度：`+14521`

差异来自 1 条历史管理员请求，不计入令牌维度。

## 正式库修正动作

执行对象：

- `options`
- `logs`
- `quota_data`
- `tasks`
- `users`
- `tokens`
- `channels`
- Redis：`user:<redacted>`、`token:<redacted>`

写入后的正式价格口径：

- `ModelRatio`
  - `agnes-1.5-flash = 0.035`
  - `agnes-2.0-flash = 0.05`
- `CompletionRatio`
  - `agnes-1.5-flash = 2.142857142857143`
  - `agnes-2.0-flash = 2`
- `ModelPrice`
  - `agnes-image-2.0-flash = 0.003`
  - `agnes-image-2.1-flash = 0.003`
  - `agnes-video-v2.0 = 0.005`

历史修正结果：

- 6 条 Agnes 历史消费日志 `quota` 已更新为正式价结果
- 相关 `other` 价格快照已同步改成正式价口径
- `quota_data` 中 Agnes 相关小时聚合已同步改成正式价结果
- `tasks.id=<redacted> quota` 改为 `10000`
- `tasks.id=<redacted> private_data.billing_context` 改为：
  - `model_price = 0.005`
  - `model_ratio = 0`
  - `per_call_billing = true`
- Redis 已清理：
  - `user:<redacted>`
  - `token:<redacted>`
- 二次复核补充修正：
  - `log:<anon-7> agnes-1.5-flash quota: 0 -> 8`
  - `quota_data:<anon-hour-1> agnes-1.5-flash quota: 0 -> 8`
  - `users.id=<redacted> quota/used_quota` 再同步 `8`
  - `tokens.id=<redacted> remain_quota/used_quota` 再同步 `8`
  - `channels.id=<redacted> used_quota` 再同步 `8`

## 关键验证

### 正式库

修正后关键记录：

- `logs`
  - `log:<anon-1> agnes-image-2.1-flash quota=1500`
  - `log:<anon-2> agnes-1.5-flash quota=9`
  - `log:<anon-3> agnes-video-v2.0 quota=10000`
  - `log:<anon-4> agnes-image-2.0-flash quota=1500`
  - `log:<anon-5> agnes-image-2.1-flash quota=1500`
  - `log:<anon-6> agnes-2.0-flash quota=12`
  - `log:<anon-7> agnes-1.5-flash quota=8`
- `quota_data`
  - `quota_data:<anon-hour-1> agnes-image-2.1-flash quota=1500`
  - `quota_data:<anon-hour-2> agnes-1.5-flash quota=9`
  - `quota_data:<anon-hour-2> agnes-2.0-flash quota=12`
  - `quota_data:<anon-hour-2> agnes-image-2.0-flash quota=1500`
  - `quota_data:<anon-hour-2> agnes-image-2.1-flash quota=1500`
  - `quota_data:<anon-hour-2> agnes-video-v2.0 quota=10000`
  - `quota_data:<anon-hour-3> agnes-1.5-flash quota=8`
- `tasks.id=<redacted>`
  - `quota=10000`
  - `billing_context.model_price=0.005`
  - `billing_context.other_ratios.seconds=4`

修正后 Agnes 历史与验证日志非零计费统计：

```sql
select count(*), coalesce(sum(quota),0)
from logs
where model_name in (
  'agnes-1.5-flash',
  'agnes-2.0-flash',
  'agnes-image-2.0-flash',
  'agnes-image-2.1-flash',
  'agnes-video-v2.0'
)
and quota <> 0;
```

结果：

```text
8    14536
```

这是当前预期结果，因为正式库中 Agnes 相关历史与验证日志都应保留官方原价，而不是清零。

活跃模型缺价检查：

```sql
WITH active_models AS (
  SELECT DISTINCT trim(m) model
  FROM channels c
  CROSS JOIN LATERAL regexp_split_to_table(coalesce(c.models,''), ',') m
  WHERE c.status = 1 AND trim(m) <> ''
  UNION
  SELECT DISTINCT model FROM abilities WHERE enabled = true
), mr AS (
  SELECT value::jsonb j FROM options WHERE key = 'ModelRatio'
), mp AS (
  SELECT value::jsonb j FROM options WHERE key = 'ModelPrice'
)
SELECT count(*)
FROM active_models a, mr, mp
WHERE NOT (mr.j ? a.model) AND NOT (mp.j ? a.model);
```

结果：

```text
16
```

说明站内仍有其他历史遗留活跃模型缺价，但 Agnes 211 这组模型已经完整覆盖。

### 正式运行态

等待 `SYNC_FREQUENCY=60` 一轮同步后，通过正式网关发起 `agnes-1.5-flash` 请求：

```bash
curl -fsS http://127.0.0.1:<prod-port>/v1/chat/completions \
  -H "Authorization: Bearer <redacted-token>" \
  -H 'Content-Type: application/json' \
  -d '{"model":"agnes-1.5-flash","messages":[{"role":"user","content":"ok"}],"max_tokens":1}'
```

结果：

- HTTP `200`
- 响应正常返回
- 新日志 `id=<redacted>`
- `quota=7`
- `other.model_ratio=0.035`
- `other.completion_ratio=2.142857142857143`

这证明正式运行进程已经吃到新的正式价格配置。

### 3001 隔离环境

重建命令：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

结果：

- `3001` 已健康
- 隔离镜像版本：

```text
version=v1.1.0
commit=a40786ed67ab893cf1b1de32578156e855e11230-dirty
date=2026-06-09T08:41:19Z
```

## 边界

- 正式 `13000` 已确认 Agnes 文本模型按正式原价计费。
- 仓库 Agnes 图片兼容补丁已写入并通过单测，`3001` 已重建到包含该补丁的当前工作区镜像。
- 本次没有把 Agnes 渠道配置复制到 `3001` 的独立数据库，因此未在 `3001` 上新增一条端到端 Agnes 图片真实请求日志。
- 正式 `13000` 当前运行版本仍是：

```text
version=v1.1.0
commit=5f693fa9e4535d7de642db2198afa929e7ea445e
date=2026-06-09T01:11:54Z
```

- 因此图片兼容补丁目前只在仓库和 `3001` 隔离环境中，尚未发布到正式服务。
