# SSRF transport and Ollama timeout recheck - 2026-09-09

## Scope

本记录覆盖本轮 SSRF 请求边界、直接连接的 DNS 结果固定、HTTP 代理目标重写、SOCKS 远程 DNS 重定向语义，以及 Ollama 长时间模型拉取超时。采样时间为 2026-09-09 11:35 CST，分支为 `dev`，工作树保留用户已有未提交修改。

## Changes checked

- 只有显式标记的用户可控抓取请求进入 SSRF 校验；普通 relay 渠道不再继承抓取 URL 的端口限制。
- SSRF DNS 校验返回并固定已验证地址，直接连接不会在校验后再次解析原始 hostname。
- HTTP 代理请求行改写为已验证 IP；HTTPS 代理保留 CONNECT 的 origin-form，并保留原始 TLS SNI。
- SOCKS 远程 DNS 在关闭域名 IP 过滤时保留 hostname，并在重定向链中传递远程解析策略。
- Ollama 的模型列表、拉取、流式拉取、删除和版本请求使用操作级超时；较短的全局 relay 超时不会截断长时间拉取。

## Verification evidence

以下命令均在本轮执行并退出码为 0：

```text
go test ./service ./common ./relay/channel/ollama ./controller ./relay
go test ./...
go vet ./...
go vet ./service ./common ./relay/channel/ollama
git diff --check
```

新增的定向覆盖包括 SSRF DNS 快照、无 authority URL、DNS 取消、HTTP 代理绝对请求行、HTTPS CONNECT 请求形态、SOCKS 远程 DNS 重定向和 Ollama 超时复制语义。

隔离开发环境已重建：

```text
scripts/build-docker-local.sh new-api-local:dev                         # exit 0
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api  # exit 0
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl --connect-timeout 3 --max-time 10 -fsS http://127.0.0.1:3001/api/status
```

容器 `new-api-dev-isolated-new-api-1` 为 `healthy`，build-info 为：

```text
version=v1.1.0
commit=21bd76afd7429486668fdeaf1ac8c628300c6359-dirty
date=2026-09-09T03:21:19Z
source=https://github.com/MisonL/new-api
```

`/api/status` 返回 `success:true`；启动日志显示服务 ready，未见本轮修改相关错误。

## Boundaries

- 未重建、重启或写入正式服务；未执行真实付费上游请求、真实 Ollama 拉取或真实 SOCKS/HTTP 代理验收。
- 隔离容器使用 dirty worktree 构建，因此 build-info 的 `-dirty` 是预期结果；容器健康不等于正式环境验收。
- 工作区在本轮开始前已有大量无关 tracked 和 untracked 改动，均未回退、整理或删除；本轮未提交、未推送。
