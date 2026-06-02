# CAS Enterprise IdP Interop - 2026-06-01

## Scope

Sampling time: 2026-06-01T23:12:00+08:00.

This record covers live CAS protocol interop against the isolated `3001` new-api environment only. The production-style container on `127.0.0.1:13000` was observed but not changed.

Target environment:

- new-api container: `new-api-dev-isolated-new-api-1`
- exposed isolated port: `127.0.0.1:3001`
- running image: `new-api-local:cas-interop`
- build info: `version=v1.1.0`, `commit=cas-interop-local`, `date=2026-06-01T14:26:05Z`
- production boundary: `new-api new-api-local:prod-main 127.0.0.1:13000->3000/tcp`, left untouched

## Control Contract

- Primary setpoint: verify that new-api CAS start, callback, ticket validation, local login, and replay rejection work against real open-source CAS servers.
- Acceptance: each passing provider must show a real `/cas/start` redirect, IdP login, service ticket callback, new-api JSON login success, upstream `serviceValidate`, and second-use ticket rejection.
- Guardrails: do not read repository `.env`, do not touch the production `3000` path, do not count a provider as passed without callback and replay evidence.
- Boundary: temporary CAS containers, temporary scripts under `/tmp/new-api-cas-interop`, isolated PostgreSQL provider rows, and the `3001` isolated new-api container.
- Approximation validity: `django-cas-server` is treated as a protocol reference server, not as an enterprise IdP.

## IdP Matrix

| IdP | Container | Image | Port | Result |
| --- | --- | --- | --- | --- |
| Apereo CAS | `new-api-cas-apereo` | `apereo/cas:latest` | `18080:8080` | Passed |
| LemonLDAP::NG | `new-api-cas-lemon` plus `new-api-cas-lemon-proxy` | `lemonldapng/lemonldap-ng:latest`, `nginx:1.27-alpine` | `18081:80`, `18082:80` | Passed |
| django-cas-server | `new-api-cas-django` | `new-api-cas-django:interop` | `18083:8000` | Passed as protocol reference |

## Provider Configuration

The isolated database table `custom_oauth_providers` contained:

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

`options.ServerAddress` was set to `http://new-api-dev-isolated-new-api-1:3000` so CAS service URLs are reachable inside the Docker network while browser-facing starts still use the host ports.

## Apereo CAS

Container evidence:

```text
docker image inspect apereo/cas:latest
Architecture=arm64
RepoDigests=["apereo/cas@sha256:9f00412dffaa835e7bbf55824bdbf3940cd23339aa907ae92d57e36eb5b8c770"]
```

The image runs under qemu on this host and is slow. Startup took about 469 seconds. Initial login probes timed out before CAS was fully ready; after readiness, the login page and `serviceValidate` endpoint were reachable.

Interop script:

```bash
python3 /tmp/new-api-cas-interop/run_apereo_cas_flow.py
```

Key script result:

```text
start_status 302 http://127.0.0.1:18080/cas/login?service=...
login_page_status 200 .../cas/login?service=...
login_submit_status 302 http://new-api-dev-isolated-new-api-1:3000/api/auth/external/apereo-cas/cas/callback?state=xFxtGShC3Ss2&ticket=ST-2-OC0zO5Nfc2Eg5-3ldBXQTEsPk3E-fc62053780a7
callback_status 200
callback_body {"data":{"display_name":"casuser","group":"default","id":46,"role":1,"status":1,"username":"apereo-cas_46"},"message":"","success":true}
replay_body {"message":"CAS service ticket has already been used","success":false}
```

Apereo log evidence:

```text
SERVICE_TICKET_VALIDATE_SUCCESS
PROTOCOL_SPECIFICATION_VALIDATE_SUCCESS
<cas:authenticationSuccess>
    <cas:user>casuser</cas:user>
</cas:authenticationSuccess>
```

new-api log evidence:

```text
provider_slug=apereo-cas provider_kind=cas action=login ... target_user_id=46 ... failure_reason=(empty)
GET /api/auth/external/apereo-cas/cas/callback?...ticket=ST-2-OC0zO5Nfc2Eg5-3ldBXQTEsPk3E-fc62053780a7 | 200 | 558.019056ms
provider_slug=apereo-cas provider_kind=cas action=(empty) ... failure_reason=cas_ticket_replay
```

Result: passed. Operational note: the official `latest` image used here is arm64 and slow on the current host, so the script uses longer POST and callback timeouts.

## LemonLDAP::NG

LemonLDAP uses virtual hosts such as `auth.example.com`. Direct requests to `127.0.0.1:18081` hit the Manager API vhost and returned 403. A temporary nginx proxy was added on `18082` to forward to `new-api-cas-lemon:80` with `Host: auth.example.com`.

CAS issuer configuration was enabled with the actual LemonLDAP key names:

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
python3 /tmp/new-api-cas-interop/run_lemon_cas_flow.py
```

Key script result:

```text
start_status 302 http://127.0.0.1:18082/cas/login?service=...
login_page_status 200 .../cas/login?service=...
login_submit_status 302 http://new-api-dev-isolated-new-api-1:3000/api/auth/external/lemon-cas/cas/callback?state=QYbj7mFZYNRU&ticket=ST-68d4fac92f84f26aa4ca3cb204c710203e35095dc9c64b05f43a3def97d9eca9
callback_status 200
callback_body {"data":{"display_name":"dwho","group":"default","id":45,"role":1,"status":1,"username":"dwho"},"message":"","success":true}
replay_body {"message":"CAS service ticket has already been used","success":false}
```

LemonLDAP log evidence:

```text
User dwho successfully authenticated at level 1
User dwho is redirected to http://new-api-dev-isolated-new-api-1:3000/api/auth/external/lemon-cas/cas/callback?state=QYbj7mFZYNRU
GET /cas/serviceValidate?...ticket=ST-68d4fac92f84f26aa4ca3cb204c710203e35095dc9c64b05f43a3def97d9eca9
CAS validation succeeded for dwho as dwho
```

new-api log evidence:

```text
provider_slug=lemon-cas provider_kind=cas action=login ... target_user_id=45 ... failure_reason=(empty)
GET /api/auth/external/lemon-cas/cas/callback?...ticket=ST-68d4fac92f84f26aa4ca3cb204c710203e35095dc9c64b05f43a3def97d9eca9 | 200 | 408.833178ms
provider_slug=lemon-cas provider_kind=cas action=(empty) ... failure_reason=cas_ticket_replay
```

Result: passed.

## django-cas-server Protocol Reference

This container was used to validate the generic CAS protocol behavior against a small CAS Protocol 3.0 server. It is not counted as an enterprise IdP replacement.

Interop script:

```bash
python3 /tmp/new-api-cas-interop/run_django_cas_flow.py
```

Key script result:

```text
start_status 302 http://127.0.0.1:18083/login?service=...
login_submit_status 302 http://new-api-dev-isolated-new-api-1:3000/api/auth/external/django-cas/cas/callback?state=gKip55cBpis3&ticket=ST-gFCJ5ywaZzMpsWgXyFAVGKvjpAncLkIifKWGPfqpmqJYLlYAkgDHcMibNorth
callback_status 200
callback_body {"data":{"display_name":"casuser","group":"default","id":44,"role":1,"status":1,"username":"casuser"},"message":"","success":true}
replay_body {"message":"CAS service ticket has already been used","success":false}
```

Django CAS log evidence:

```text
GET /serviceValidate?service=...&ticket=ST-gFCJ5ywaZzMpsWgXyFAVGKvjpAncLkIifKWGPfqpmqJYLlYAkgDHcMibNorth HTTP/1.1" 200 673
```

new-api log evidence:

```text
provider_slug=django-cas provider_kind=cas action=login ... target_user_id=44 ... failure_reason=(empty)
provider_slug=django-cas provider_kind=cas action=(empty) ... failure_reason=cas_ticket_replay
```

Result: passed as protocol reference.

## Verification Commands

```bash
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
python3 /tmp/new-api-cas-interop/run_django_cas_flow.py
python3 /tmp/new-api-cas-interop/run_lemon_cas_flow.py
python3 /tmp/new-api-cas-interop/run_apereo_cas_flow.py
docker logs --tail 100 new-api-dev-isolated-new-api-1
docker logs --tail 120 new-api-cas-apereo
docker logs --tail 160 new-api-cas-lemon
docker logs --tail 120 new-api-cas-django
docker inspect new-api-cas-lemon-proxy
docker exec new-api-cas-lemon-proxy cat /etc/nginx/nginx.conf
```

All three scripts exited with status 0 after the Apereo timeout was increased to account for qemu latency.

## Residual Gate Boundary

- This was an isolated `3001` live interop run. It does not claim the production `3000` deployment was changed or validated.
- The provider rows were written into the isolated PostgreSQL database for interop. They are environment state, not committed seed data.
- Temporary scripts and IdP configuration live under `/tmp/new-api-cas-interop`.
- LemonLDAP Host header forwarding used temporary container `new-api-cas-lemon-proxy`; its runtime nginx config is `/etc/nginx/nginx.conf` and forwards to `new-api-cas-lemon:80` with `Host: auth.example.com`.
- Apereo CAS used the official `latest` image that currently resolved to arm64 on this host. It passed, but its latency is not representative of an amd64-native deployment.
- CAS single logout remains out of scope for this verification and is not implemented by the current CAS callback path. If production requires SLO, add it behind a separate rollout gate with IdP-initiated and app-initiated logout tests before enabling CAS for that tenant. Until then, users must manually sign out of each application session.
- Reproducing the isolated provider rows in production requires redesigning network boundaries and CAS URLs. `options.ServerAddress`, `cas_server_url`, `validate_url`, and `service_url` must use production-reachable endpoints; Docker-internal addresses such as `http://new-api-dev-isolated-new-api-1:3000` need production ingress, DNS, NAT, or proxy routing before use on the `3000` deployment.

## Production Deployment Checklist

Network topology options:

- Ingress controller: use when the production stack already terminates TLS through Kubernetes or another ingress layer. `options.ServerAddress` and `service_url` should be the public HTTPS console URL, while `cas_server_url` and `validate_url` should be the production IdP URL reachable from the new-api container.
- DNS plus load balancer: use when new-api runs behind a managed load balancer. Verify DNS resolves to the load balancer from users and that the backend can call the CAS validation endpoint over the expected route.
- NAT or reverse proxy: use when the IdP or new-api is only reachable through a private network boundary. Configure the proxy with stable hostnames and TLS, then use those hostnames in `cas_server_url`, `validate_url`, and `service_url`; do not copy Docker-internal container names into production.

Safe validation plan:

- Confirm `options.ServerAddress` returns the production `3000` console URL and not the isolated `3001` value.
- Confirm no production CAS setting contains `new-api-dev-isolated-new-api-1`, `127.0.0.1:3001`, or other isolated-only hostnames.
- Run an end-to-end CAS login from a test user and verify the new-api audit log records `provider_kind=cas`, `action=login`, and an empty failure reason.
- Replay the same service ticket once and verify it is rejected before a second upstream validation request.
- Verify TLS for the browser-facing service URL and for the backend-to-IdP `validate_url`.
- Run a normal `/api/status` smoke test and one authenticated admin page load after enabling the provider.

Rollback and mitigation:

- Keep the CAS provider disabled until the validation plan passes; disabling the provider is the first rollback path.
- If DNS or load-balancer routing is wrong, revert the DNS or proxy change before changing application code.
- If login fails after enablement, disable the CAS provider, restore the prior auth provider setting, and preserve `docker logs` plus CAS IdP logs for review.
- For SLO-dependent tenants, document the current limitation in the rollout note and keep CAS disabled until SLO tests are added.

Monitoring and alerting:

- Watch new-api system logs for `failure_reason=cas_ticket_replay`, `failure_reason=cas_ticket_guard_error`, `failure_reason=invalid_state`, and CAS validation timeout errors.
- Alert on sustained CAS login failures, for example more than 5 failures in 5 minutes or any guard infrastructure error in production.
- Track CAS callback latency and upstream validation HTTP status distribution.
- Add a dashboard panel for CAS success count, failure count by reason, replay count, and validation timeout count.
