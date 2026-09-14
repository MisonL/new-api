# Responses to Chat compatibility recheck - 2026-09-05

## Scope

本轮针对 Claude Code 经 Responses 到 Chat Completions 转换时的历史
`reasoning_content`、工具参数和流式终态问题做最小原子修正。正式服务
`127.0.0.1:13000` 未重建、未重启、未写入。

## Changes

- `dto.IncompleteDetails` 增加 `EffectiveReason`，标准 `reason` 为空时兼容
  旧上游的 `reasoning` 字段。
- 非流式 Responses 到 Chat 的 `content_filter` 终态映射使用兼容字段。
- 流式 Responses 到 Chat 不再以 `response.output_item.done` 加 `[DONE]`
  代替 `response.completed`、`response.done` 或 `response.incomplete`；缺少
  Responses 终态时显式返回 `bad_response_body`，避免伪造成功。
- 流式和非流式增加旧字段及截断流回归测试；状态值匹配统一大小写不敏感。

## Verification

以下命令于 2026-09-05（Asia/Shanghai）执行并退出 0：

```text
go test -count=1 ./dto ./service/openaicompat ./relay/channel/openai ./relay
go test -count=1 ./service/openaicompat ./relay/channel/openai
go test -race -count=1 ./service/openaicompat ./relay/channel/openai
go vet ./service/openaicompat ./relay/channel/openai
git diff --check
```

隔离镜像和容器验证：

```text
scripts/build-docker-local.sh new-api-local:dev                         # exit 0
docker compose -f deploy/compose/dev-isolated.yml --env-file \
  deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api   # exit 0
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
  version=v1.1.0
  commit=f825f23d870df24e38362a7bd1d0a2cd4423a1a6-dirty
  date=2026-09-04T15:57:48Z
curl --connect-timeout 5 --max-time 15 http://127.0.0.1:3001/api/status
  HTTP 200, success=true, version=v1.1.0
curl --connect-timeout 5 --max-time 15 http://127.0.0.1:3001/api/healthz
  HTTP 200
```

重建后第一次探针发生在服务启动窗口，返回 `curl: (52) Empty reply from
server`；随后容器状态为 `running healthy restart=0`，带超时的重复探针返回
HTTP 200，未将启动窗口结果当作通过。

正式服务只读检查：

```text
curl --connect-timeout 3 --max-time 8 http://127.0.0.1:13000/api/status
  HTTP 200
curl --connect-timeout 5 --max-time 15 http://127.0.0.1:13000/api/healthz
  HTTP 200
image=new-api-local:prod-codex-lite-4527d766, running healthy, restart=0
```

## Boundaries

- 本轮证明了转换函数、流式事件门禁、隔离容器启动和本地健康接口；未发送
  真实计费请求，未做真实 Claude Code 或第三方上游验收。
- 正式 13000 仍运行旧的 `new-api-local:prod-codex-lite-4527d766` 镜像，
  与隔离容器的当前 dirty 源码不是同一制品；本轮没有生产发布授权。
- 构建期间前端存在既有的大 chunk 警告；隔离容器启动日志中的 npm registry
  SSRF 解析拒绝属于当前网络策略，未影响本轮后端转换测试。
