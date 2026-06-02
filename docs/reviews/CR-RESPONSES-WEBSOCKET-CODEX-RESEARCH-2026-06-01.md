# Responses WebSocket support research for Codex

Sampling time: 2026-06-01T21:29:14+08:00

Scope: record the current evidence and design boundary for adding Codex/OpenAI Responses WebSocket support to this new-api branch. This document is research only. No production configuration or business code was changed.

## Conclusion

Codex now has a Responses WebSocket transport. It is not the same protocol as OpenAI Realtime. Codex expects a WebSocket upgrade on `/v1/responses`, with Responses stream events sent over the WebSocket connection.

Realtime on `/v1/realtime` is an audio and conversation-event protocol, while Responses WebSocket on `/v1/responses` is turn-based JSON request/response streaming. This is why the future boundary below says not to reuse realtime event semantics.

Current new-api only exposes WebSocket for `GET /v1/realtime`. The normal Responses paths remain HTTP:

- `POST /v1/responses`
- `POST /v1/responses/compact`

Therefore Codex can only use Responses WebSocket through new-api after new-api implements a dedicated WebSocket transport for `/v1/responses` and maps the Codex/OpenAI Responses WebSocket message protocol to upstream behavior.

## Local new-api facts

Current route registration:

- `router/relay-router.go:78` registers `GET /v1/realtime` and routes it to `RelayFormatOpenAIRealtime`.
- `router/relay-router.go:101` registers `POST /v1/responses`.
- `router/relay-router.go:104` registers `POST /v1/responses/compact`.

Current upstream transport split:

- `relay/channel/openai/adaptor.go:769-770` sends only `RelayModeRealtime` through `channel.DoWssRequest`.
- `relay/channel/openai/adaptor.go:772` sends all other OpenAI-compatible requests through `channel.DoApiRequest`.
- `relay/channel/api_request.go:435` builds normal HTTP upstream requests with `http.NewRequest`.
- `relay/channel/api_request.go:499` contains the WebSocket dialer helper, but it is only selected for realtime.

Current Codex channel adaptor:

- `relay/channel/codex/adaptor.go:149-156` only supports `RelayModeResponses` and `RelayModeResponsesCompact`.
- It maps to `/backend-api/codex/responses` and `/backend-api/codex/responses/compact`.
- `relay/channel/codex/adaptor.go:122` uses `channel.DoApiRequest`, not WebSocket.

Implication: adding WebSocket support is not a flag flip. It needs new route handling and protocol handling.

## Codex source facts

Source snapshot checked:

- Repository: `openai/codex`
- Local clone: `/tmp/openai-codex-src`
- Commit: `f27bbbd49c0e05e763a96cc7fa677499de32b8d8`

Codex has a Responses WebSocket client:

- `codex-rs/codex-api/src/endpoint/responses_websocket.rs` defines `ResponsesWebsocketClient` and `ResponsesWebsocketConnection`.
- The implementation uses `tokio_tungstenite` and sends JSON request frames over WebSocket.
- `codex-rs/core/src/client.rs:798-805` enables the transport when the provider advertises `supports_websockets` and the session has not disabled websockets.
- `codex-rs/core/src/client.rs:1601-1607` chooses the WebSocket Responses stream path when `wire_api = Responses` and the WebSocket transport is enabled.
- `codex-rs/core/src/client.rs:146` defines the beta header value `responses_websockets=2026-02-06`.

Codex mock server evidence:

- `scripts/mock_responses_websocket_server.py:15` sets `PATH = "/v1/responses"`.
- `scripts/mock_responses_websocket_server.py:148-152` shows a sample provider:

```toml
[model_providers.localapi_ws]
base_url = "ws://127.0.0.1:8765/v1"
name = "localapi_ws"
wire_api = "responses_websocket"
```

Provider defaults:

- OpenAI official provider sets `supports_websockets: true` in `codex-rs/model-provider-info/src/lib.rs:352`.
- OSS/custom provider defaults to `supports_websockets: false` in `codex-rs/model-provider-info/src/lib.rs:513`.

Local installed Codex:

- `codex --version` returned `codex-cli 0.135.0`.
- The npm package is a wrapper around a native binary.
- `codex doctor --json` was attempted but produced no output and was terminated to avoid leaving a hung probe process.
- Alternative verification for future work: run `codex --version` to confirm 0.135.0, inspect `~/.codex/config.toml` for `supports_websockets` or similar provider settings, and use `codex config --help` or the 0.135.0 release notes to locate documented WebSocket flags.

## New-api upstream discussion

Official repository checked:

- `QuantumNous/new-api`
- `gh repo view` confirmed `https://github.com/QuantumNous/new-api`.

Relevant issues:

- `QuantumNous/new-api#3027`: `支持OpenAI的Websocket Responses API`
  - State: open
  - Created: 2026-02-26
  - Updated: 2026-05-13
  - Label: enhancement
  - Request: support OpenAI WebSocket Responses API for Codex and other coding clients.
  - A user posted a real Codex WebSocket upgrade request:
    - `GET /v1/responses`
    - `connection: Upgrade`
    - `upgrade: websocket`
    - `openai-beta: responses_websockets=2026-02-06`
  - Maintainer/collaborator comments indicate the expected fallback is HTTP plus SSE when WebSocket is unavailable, and suggest trying Codex-side fallback configuration.

- `QuantumNous/new-api#3145`: `openai api websocket mode缺失`
  - State: closed
  - Created: 2026-03-06
  - Updated: 2026-03-06
  - Request: support OpenAI API WebSocket mode because Codex CLI can use it and the official claim was more than 30 percent speed improvement over SSE.

Observed attitude: the feature request is acknowledged and kept as an enhancement, but there is no visible merged implementation or committed schedule. The practical stance in the discussion is to rely on Codex HTTP/SSE fallback for now or wait for a contributor with test conditions.

## Sub2API comparison

Official repository checked:

- `Wei-Shaw/sub2api`
- `gh repo view` confirmed `https://github.com/Wei-Shaw/sub2api`.

Unlike new-api, sub2api already has active OpenAI/Codex Responses WebSocket implementation and issue traffic around it.

Relevant findings:

- `Wei-Shaw/sub2api#1769`: `Codex Responses WebSocket (ctx_pool) is unstable`
  - State: open
  - The issue says this is not a missing WebSocket problem. It is a stability and continuation problem after WebSocket is enabled.
  - Reported symptoms include:
    - `stream disconnected before completion: websocket closed by server before response.completed`
    - `1013 upstream websocket is busy, please retry later`
    - `upstream continuation connection is unavailable`
    - `preferred connection unavailable`
    - context repetition or lost continuity.

- `Wei-Shaw/sub2api#2195`: `OpenAI WS 模式下 tool_result 与 tool_use 不匹配导致 400 invalid_request_err 高频出现`
  - State: closed
  - Reported root cause is that recovery paths can drop `previous_response_id`, causing upstream to lose the tool-call chain and reject `function_call_output`.
  - One workaround mentioned in comments:

```yaml
gateway:
  openai_ws:
    mode_router_v2_enabled: true
    ingress_mode_default: passthrough
```

- `Wei-Shaw/sub2api#2923`: `OpenAI Responses WebSocket 多轮请求计费幂等键冲突`
  - State: open
  - Comment says it should be resolved by PR #2922.

- `Wei-Shaw/sub2api#2922`: `fix: avoid OpenAI WS usage dedup conflicts`
  - State: merged
  - Merged: 2026-06-01
  - Fix: use each OpenAI WebSocket turn's upstream `resp_*` id as the usage billing id when OpenAI WS mode is true.

- `Wei-Shaw/sub2api#1779`: `fix(openai): add fine-grained continuation recovery switches (#1769)`
  - State: open
  - Proposed switches include:
    - `ingress_preflight_ping_recovery_enabled`
    - `reconnect_prev_response_recovery_enabled`
    - `fail_close_on_continuation_lost`
    - `preserve_previous_response_id_on_http`
  - Upgrade-note example:

```yaml
gateway:
  openai_ws:
    preserve_previous_response_id_on_http: true
    fail_close_on_continuation_lost: true
```

Sub2API implication: WebSocket support is possible and actively used, but implementation complexity is real. The hard parts are continuation, tool output matching, connection reuse, failover, billing idempotency, and avoiding silent replay after losing `previous_response_id`.

## Why this matters for compact and 524

Recent production logs showed `/v1/responses/compact` failures as HTTP `524` after long waits through an upstream Cloudflare-proxied service. Responses WebSocket can reduce the risk of idle long HTTP waits only if the full path supports it:

- Codex client to new-api over WebSocket.
- new-api WebSocket handler for `/v1/responses`.
- Upstream transport that either:
  - connects to an upstream Responses WebSocket endpoint, or
  - explicitly bridges client WebSocket to upstream HTTP/SSE with its own heartbeat/status protocol.

It should not be treated as a direct fix for all `524` errors. If new-api accepts WebSocket from Codex but still performs one long blocking HTTP request to the upstream without progress or retry semantics, the same upstream Cloudflare timeout can still occur.

Short-term mitigations before a WebSocket implementation are: configure upstream idle timeouts below Cloudflare's 524 threshold; add request timeout, retry, and progress or heartbeat logging in both WebSocket handler work and regular HTTP upstream calls; and, where possible, use a direct non-Cloudflare upstream endpoint as a temporary route. These are mitigations only; WebSocket remains the long-term transport change.

## Development boundary for a future task

Recommended future design boundary:

1. Do not change the existing HTTP-compatible `/v1/responses` and `/v1/responses/compact` behavior.
2. Add a WebSocket upgrade path for `/v1/responses` matching Codex/OpenAI Responses WebSocket expectations.
3. Treat `/v1/realtime` as a separate protocol. Do not reuse realtime event semantics for Responses WebSocket.
4. Add explicit provider/channel capability settings. Avoid silently enabling WebSocket for every OpenAI-compatible channel.
5. Preserve and audit these request headers:
   - `openai-beta: responses_websockets=2026-02-06`
   - `x-codex-turn-metadata`
   - `x-client-request-id`
   - `session_id`
   - `version`
6. Decide early whether new-api will:
   - proxy WebSocket end to end, or
   - bridge WebSocket client requests to upstream HTTP/SSE.
7. If bridging to HTTP/SSE, document that this does not fully solve upstream Cloudflare 524 unless the bridge can keep the client alive and manage upstream retries safely.
8. Fail closed on lost continuation by default, stricter than sub2api where it is opt-in through `fail_close_on_continuation_lost`. Do not silently drop `previous_response_id` and replay a turn as a fresh request.
9. Add per-turn idempotency and billing identifiers for WebSocket sessions. Do not use only a connection-level client request id.
10. Include tests for:
    - Codex WS handshake on `/v1/responses`.
    - first turn and subsequent turn on the same WebSocket.
    - prewarm `generate=false` followed by business continuation.
    - `previous_response_id` preservation.
    - function/tool output continuation.
    - upstream `response.completed` missing or disconnect before completion.
    - 429, 524, and explicit upstream error frames.
    - billing per turn.

Estimated scope: 2-3 weeks for basic WebSocket handshake and bridging, 1-2 weeks for continuation and billing logic, and 1 week for focused tests.

## Verification performed

Commands run:

```bash
rg -n "GET\\(\"/realtime\"|POST\\(\"/responses\"|POST\\(\"/responses/compact\"|DoWssRequest|DoApiRequest|RelayModeRealtime|ResponsesCompact" router/relay-router.go relay/channel/openai/adaptor.go relay/channel/api_request.go relay/constant/relay_mode.go relay/channel/codex/adaptor.go
git -C /tmp/openai-codex-src rev-parse HEAD
rg -n "ResponsesWebsocket|supports_websockets|responses_websockets=2026-02-06|wire_api = \"responses_websocket\"|PATH = \"/v1/responses\"" /tmp/openai-codex-src/codex-rs /tmp/openai-codex-src/scripts/mock_responses_websocket_server.py -g '!**/target/**'
gh issue view 3027 --repo QuantumNous/new-api --comments --json number,title,state,createdAt,updatedAt,author,body,comments,url,labels
gh issue view 3145 --repo QuantumNous/new-api --comments --json number,title,state,createdAt,updatedAt,author,body,comments,url,labels
gh issue view 1769 --repo Wei-Shaw/sub2api --comments --json number,title,state,createdAt,updatedAt,author,body,comments,url,labels
gh issue view 2195 --repo Wei-Shaw/sub2api --comments --json number,title,state,createdAt,updatedAt,author,body,comments,url,labels
gh issue view 2923 --repo Wei-Shaw/sub2api --comments --json number,title,state,createdAt,updatedAt,author,body,comments,url,labels
gh pr view 2922 --repo Wei-Shaw/sub2api --json number,title,state,mergedAt,createdAt,updatedAt,author,body,url,commits,files
gh pr view 1779 --repo Wei-Shaw/sub2api --json number,title,state,mergedAt,createdAt,updatedAt,author,body,url,commits,files,comments
codex --version
```

Results:

- `codex --version`: `codex-cli 0.135.0`.
- `openai/codex` source commit checked: `f27bbbd49c0e05e763a96cc7fa677499de32b8d8`.
- `QuantumNous/new-api#3027` remains open.
- `QuantumNous/new-api#3145` is closed, with no implementation evidence found in this branch.
- `Wei-Shaw/sub2api#2922` is merged.
- `Wei-Shaw/sub2api#1779` remains open.

Limitations:

- No live Codex WebSocket request was sent through this new-api instance.
- `codex doctor --json` was attempted but hung without output and was terminated.
- No sub2api runtime instance was tested.
- GitHub issue state is time-sensitive and should be refreshed before implementation.
- HTTP Responses mode header preservation was not audited. Before WebSocket implementation, verify whether HTTP mode preserves `session_id`, `x-codex-turn-metadata`, `version`, and `openai-beta`; if HTTP mode drops them, WebSocket will likely inherit the same continuation risk unless explicitly fixed.
