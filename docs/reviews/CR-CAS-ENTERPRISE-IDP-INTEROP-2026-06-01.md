# CAS 企业 IdP 互操作验证 - 2026-06-01

## 范围

采样时间保存在私有验证证据中。

本记录只覆盖隔离 `3001` new-api 环境中的 CAS 协议真实互操作。`127.0.0.1:13000` 上的正式样式容器仅被观察，未被修改。

目标环境：

- new-api container: `new-api-dev-isolated-new-api-1`
- exposed isolated port: `127.0.0.1:3001`
- running image: `<isolated-candidate-image>`
- build info: `<candidate-build-info>`
- 正式边界：另一个正式容器仅被观察，保持未触碰

## 控制契约

- 主要目标：验证 new-api 的 CAS 启动、回调、票据校验、本地登录和重放拒绝，且对接真实开源 CAS 服务器。
- 验收标准：每个通过的 provider 都必须出现真实的 `/cas/start` 重定向、IdP 登录、服务票据回调、new-api JSON 登录成功、上游 `serviceValidate` 和二次使用票据被拒绝。
- 保护条件：不得读取仓库 `.env`，不得触碰正式 `3000` 路径；没有回调和重放证据时不得将 provider 计为通过。
- 边界：临时 CAS 容器、`<temporary-interop-dir>` 下的脚本、隔离 PostgreSQL provider 行以及 `3001` 隔离 new-api 容器。
- 近似有效性：`django-cas-server` 仅作为协议参考服务器，不作为企业 IdP 替代品。

## IdP 矩阵

| IdP | 容器 | 镜像 | 端口 | 结果 |
| --- | --- | --- | --- | --- |
| Apereo CAS | `new-api-cas-apereo` | `apereo/cas:latest` | `18080:8080` | 通过 |
| LemonLDAP::NG | `new-api-cas-lemon` 加 `new-api-cas-lemon-proxy` | `lemonldapng/lemonldap-ng:latest`、`nginx:1.27-alpine` | `18081:80`、`18082:80` | 通过 |
| django-cas-server | `new-api-cas-django` | `new-api-cas-django:interop` | `18083:8000` | 作为协议参考通过 |

## Provider 配置

隔离数据库表 `custom_oauth_providers` 包含：

```text
apereo-cas  cas  enabled=true
  cas_server_url=http://127.0.0.1:18080/cas
  validate_url=http://new-api-cas-apereo:8080/cas/serviceValidate
  service_url=http://new-api-dev-isolated-new-api-1:3000/api/auth/external/apereo-cas/cas/callback

django-cas  cas  enabled=true
  cas_server_url=http://127.0.0.1:18083
  validate_url=http://new-api-cas-django:8000/serviceValidate
  service_url=http://new-api-dev-isolated-new-api-1:3000/api/auth/external/django-cas/cas/callback

lemon-cas  cas  enabled=true
  cas_server_url=http://127.0.0.1:18082/cas
  validate_url=http://new-api-cas-lemon-proxy/cas/serviceValidate
  service_url=http://new-api-dev-isolated-new-api-1:3000/api/auth/external/lemon-cas/cas/callback
```

`options.ServerAddress` 设置为 `http://new-api-dev-isolated-new-api-1:3000`，使 CAS service URL 可以在 Docker 网络内访问，同时浏览器侧启动仍使用宿主机端口。

## Apereo CAS

容器证据：

```text
docker image inspect <apereo-cas-image>
Architecture=<observed-architecture>
RepoDigests=["<immutable-image-digest>"]
```

镜像启动延迟取决于环境，启用模拟时可能增加。CAS 完全就绪前的初始登录探针曾超时；就绪后，登录页和 `serviceValidate` 端点均可访问。测试超时应依据实测就绪时间设置，不要使用固定的主机特定时长。

互操作脚本：

```bash
python3 <temporary-interop-dir>/run_apereo_cas_flow.py
```

脚本关键结果：

```text
start_status 302 http://127.0.0.1:18080/cas/login?service=...
login_page_status 200 .../cas/login?service=...
login_submit_status 302 http://new-api-dev-isolated-new-api-1:3000/api/auth/external/apereo-cas/cas/callback?state=<test-state>&ticket=<test-ticket>
callback_status 200
callback_body {"data":{"display_name":"<test-user>","group":"default","id":"<test-user-id>","role":1,"status":1,"username":"<test-user>"},"message":"","success":true}
replay_body {"message":"CAS service ticket has already been used","success":false}
```

Apereo 日志证据：

```text
SERVICE_TICKET_VALIDATE_SUCCESS
PROTOCOL_SPECIFICATION_VALIDATE_SUCCESS
<cas:authenticationSuccess>
    <cas:user>&lt;test-user&gt;</cas:user>
</cas:authenticationSuccess>
```

new-api 日志证据：

```text
provider_slug=apereo-cas provider_kind=cas action=login ... target_user_id=<test-user-id> ... failure_reason=(empty)
GET /api/auth/external/apereo-cas/cas/callback?...ticket=<test-ticket> | 200 | <observed-latency>
provider_slug=apereo-cas provider_kind=cas action=(empty) ... failure_reason=cas_ticket_replay
```

结果：通过。运维说明：镜像架构和模拟状态因验证环境而异，因此脚本根据就绪状态使用 POST 和回调超时。

## LemonLDAP::NG

LemonLDAP 使用 `auth.example.com` 这类虚拟主机。直接请求 `127.0.0.1:18081` 会命中 Manager API 虚拟主机并返回 403；已在 `18082` 添加临时 nginx 代理，使用 `Host: auth.example.com` 转发到 `new-api-cas-lemon:80`。

CAS issuer 使用 LemonLDAP 的实际键名启用：

```text
issuerDBCASActivation = 1
issuerDBCASPath = ^/cas/
issuerDBCASRule = 1
casAccessControlPolicy = none
casTicketExpiration = 300
casAttr = uid
```

Interop script:

```bash
python3 <temporary-interop-dir>/run_lemon_cas_flow.py
```

Key script result:

```text
start_status 302 http://127.0.0.1:18082/cas/login?service=...
login_page_status 200 .../cas/login?service=...
login_submit_status 302 http://new-api-dev-isolated-new-api-1:3000/api/auth/external/lemon-cas/cas/callback?state=<test-state>&ticket=<test-ticket>
callback_status 200
callback_body {"data":{"display_name":"<test-user>","group":"default","id":"<test-user-id>","role":1,"status":1,"username":"<test-user>"},"message":"","success":true}
replay_body {"message":"CAS service ticket has already been used","success":false}
```

LemonLDAP 日志证据：

```text
User <test-user> successfully authenticated at level 1
User <test-user> is redirected to http://new-api-dev-isolated-new-api-1:3000/api/auth/external/lemon-cas/cas/callback?state=<test-state>
GET /cas/serviceValidate?...ticket=<test-ticket>
CAS validation succeeded for <test-user> as <test-user>
```

new-api 日志证据：

```text
provider_slug=lemon-cas provider_kind=cas action=login ... target_user_id=<test-user-id> ... failure_reason=(empty)
GET /api/auth/external/lemon-cas/cas/callback?...ticket=<test-ticket> | 200 | <observed-latency>
provider_slug=lemon-cas provider_kind=cas action=(empty) ... failure_reason=cas_ticket_replay
```

结果：通过。

## django-cas-server 协议参考

该容器用于对照小型 CAS Protocol 3.0 服务器验证通用 CAS 协议行为，不计为企业 IdP 替代品。

Interop script:

```bash
python3 <temporary-interop-dir>/run_django_cas_flow.py
```

Key script result:

```text
start_status 302 http://127.0.0.1:18083/login?service=...
login_submit_status 302 http://new-api-dev-isolated-new-api-1:3000/api/auth/external/django-cas/cas/callback?state=<test-state>&ticket=<test-ticket>
callback_status 200
callback_body {"data":{"display_name":"<test-user>","group":"default","id":"<test-user-id>","role":1,"status":1,"username":"<test-user>"},"message":"","success":true}
replay_body {"message":"CAS service ticket has already been used","success":false}
```

Django CAS 日志证据：

```text
GET /serviceValidate?service=...&ticket=<test-ticket> HTTP/1.1" 200 <response-size>
```

new-api 日志证据：

```text
provider_slug=django-cas provider_kind=cas action=login ... target_user_id=<test-user-id> ... failure_reason=(empty)
provider_slug=django-cas provider_kind=cas action=(empty) ... failure_reason=cas_ticket_replay
```

结果：作为协议参考通过。

## 验证命令

```bash
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
python3 <temporary-interop-dir>/run_django_cas_flow.py
python3 <temporary-interop-dir>/run_lemon_cas_flow.py
python3 <temporary-interop-dir>/run_apereo_cas_flow.py
docker logs --tail 100 new-api-dev-isolated-new-api-1
docker logs --tail 120 new-api-cas-apereo
docker logs --tail 160 new-api-cas-lemon
docker logs --tail 120 new-api-cas-django
docker inspect new-api-cas-lemon-proxy
docker exec new-api-cas-lemon-proxy cat /etc/nginx/nginx.conf
```

将 Apereo 超时调整为实测就绪延迟后，三个脚本均以状态码 0 退出。

## 剩余门禁边界

- 这是一次隔离 `3001` 的真实互操作运行，不表示正式 `3000` 部署已被修改或验证。
- provider 行写入隔离 PostgreSQL 数据库，仅属于环境状态，不是提交的种子数据。
- 临时脚本和 IdP 配置使用 `<temporary-interop-dir>`。
- LemonLDAP 的 Host header 转发使用临时容器 `new-api-cas-lemon-proxy`；其运行时 nginx 配置为 `/etc/nginx/nginx.conf`，并使用 `Host: auth.example.com` 转发到 `new-api-cas-lemon:80`。
- 将测试过的 Apereo 镜像 digest、架构和实测就绪时间保存到私有验证证据。模拟环境和原生环境的延迟可能明显不同。
- CAS 单点注销不在本次验证范围内，当前 CAS 回调路径也未实现该能力。若正式环境需要 SLO，应在独立发布门禁中增加 IdP 发起和应用发起的注销测试后再为租户启用；在此之前，用户必须分别退出各应用会话。
- 将隔离 provider 行复制到正式环境前，必须重新设计网络边界和 CAS URL。`options.ServerAddress`、`cas_server_url`、`validate_url` 和 `service_url` 必须使用正式可达端点；`http://new-api-dev-isolated-new-api-1:3000` 这类 Docker 内部地址在用于 `3000` 部署前需要正式 ingress、DNS、NAT 或代理路由。

## 正式部署检查清单

网络拓扑选项：

- Ingress controller：正式栈已通过 Kubernetes 或其他 ingress 层终止 TLS 时使用。`options.ServerAddress` 和 `service_url` 应为公开 HTTPS 控制台 URL，`cas_server_url` 和 `validate_url` 应为 new-api 容器可访问的正式 IdP URL。
- DNS 加负载均衡：new-api 位于托管负载均衡器后方时使用。确认用户侧 DNS 解析到负载均衡器，并确认后端可以按预期路径调用 CAS 校验端点。
- NAT 或反向代理：IdP 或 new-api 只能通过私有网络边界访问时使用。为代理配置稳定主机名和 TLS，再将这些主机名用于 `cas_server_url`、`validate_url` 和 `service_url`；不得把 Docker 内部容器名复制到正式环境。

安全验证计划：

- 确认 `options.ServerAddress` 返回正式 `3000` 控制台 URL，而不是隔离 `3001` 值。
- 确认正式 CAS 配置不包含 `new-api-dev-isolated-new-api-1`、`127.0.0.1:3001` 或其他仅隔离环境使用的主机名。
- 使用测试用户执行端到端 CAS 登录，并确认 new-api 审计日志记录 `provider_kind=cas`、`action=login` 且失败原因为空。
- 重放同一个服务票据一次，确认在第二次上游校验请求之前被拒绝。
- 验证浏览器侧 service URL 以及后端到 IdP 的 `validate_url` 的 TLS。
- 启用 provider 后执行普通 `/api/status` smoke 和一次已认证的管理页加载。

回滚与缓解：

- 验证计划通过前保持 CAS provider 禁用；禁用 provider 是第一回滚路径。
- 如果 DNS 或负载均衡路由错误，先回退 DNS 或代理变更，再考虑修改应用代码。
- 如果启用后登录失败，禁用 CAS provider，恢复之前的认证 provider 设置，并保留 `docker logs` 与 CAS IdP 日志供复核。
- 对依赖 SLO 的租户，在发布说明中记录当前限制；增加 SLO 测试前保持 CAS 禁用。

监控与告警：

- 监控 new-api 系统日志中的 `failure_reason=cas_ticket_replay`、`failure_reason=cas_ticket_guard_error`、`failure_reason=invalid_state` 和 CAS 校验超时错误。
- 对持续 CAS 登录失败告警，例如 5 分钟内超过 5 次失败，或正式环境出现任意保护基础设施错误。
- 统计 CAS 回调延迟和上游校验 HTTP 状态分布。
- 在仪表盘增加 CAS 成功数、按原因统计的失败数、重放数和校验超时数面板。
