# Compact Control Plane v2 E2E Gate - 2026-06-17

## Scope

This record covers the repository-owned local E2E gate for Codex / Responses compact routing in the isolated `3001` development stack.

The gate validates:

- native `/v1/responses/compact` routing to an `official_newapi` upstream profile
- synthetic compact marker creation and continuation restore
- stale local synthetic marker visible-only fallback
- remote opaque compaction rejection for `generic_openai` without passthrough capability
- `sub2api_http` REST `previous_response_id` rejection
- synthetic restore after model switch
- synthetic compact and restore from a redacted real Codex history fixture under `~/.codex/sessions`

The gate does not claim production `3000` or `13000` validation.

## Added Gate

- `scripts/compact-control-plane-fake-upstream.mjs`
- `scripts/compact-control-plane-e2e.sh`
- `scripts/compact-control-plane-e2e-lib.sh`
- `scripts/codex-history-compact-fixture.mjs`

Default guardrails:

- Refuses non-`http://127.0.0.1:3001` targets unless `COMPACT_E2E_FORCE=1`.
- Refuses non-`dev-isolated` container names by default.
- Stores the temporary bearer token in a curl config under a private temp directory and deletes it on exit.
- Backs up and restores test token, channels, and abilities around the run.
- Restarts the isolated `new-api` container after cleanup.
- Reads Codex history with streaming I/O and writes only redacted item summaries into temporary fixtures.

## Verification

Environment:

```text
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
version=v1.1.0
commit=75e19a2f1a4746e8b1c63d3331e3cf2eadadd8cb-dirty
date=2026-06-17T08:03:04Z
source=https://github.com/MisonL/new-api
```

Commands:

```bash
bash -n scripts/compact-control-plane-e2e.sh
bash -n scripts/compact-control-plane-e2e-lib.sh
node --check scripts/codex-history-compact-fixture.mjs
node --check scripts/compact-control-plane-fake-upstream.mjs
./scripts/compact-control-plane-e2e.sh
git diff --check
curl -fsS http://127.0.0.1:3001/api/status
docker exec new-api-dev-isolated-postgres-1 psql -U root -d new-api-dev -At -F $'\t' -c "select id, channel_id, model_name, other from logs where token_name='compact-control-plane-e2e' order by id desc limit 12"
```

Frontend and related gates:

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service -count=1
cd web/default && bun run typecheck
cd web/default && bun run build
cd web/classic && bun run lint
cd web/classic && bun run build
cd web/classic && node --test src/components/table/channels/modelTestRuntimeConfig.test.js src/helpers/responsesCompactSettings.test.js
git diff --check
```

Frontend and related gate result:

```text
Go related package tests passed.
web/default typecheck passed.
web/default build passed, including Safari compatibility rewrite and compatibility check.
web/classic lint passed after formatting the compact settings test.
web/classic build passed, including Safari compatibility rewrite and compatibility check.
web/classic node tests passed: 2 tests.
git diff --check passed.
```

Result:

```text
case=native_newapi_compact
status=200
has_compaction=true
[{"url":"/v1/responses/compact","model":"gpt-5.5","previous_response_id":"","inputTypes":["message"],"hasCompaction":false,"bodyBytes":132}]

case=synthetic_newapi_compact_and_continue
compact_status=200
marker_found=true
continue_status=200
[{"url":"/v1/responses","model":"gpt-5.5","previous_response_id":"","inputTypes":["message","message"],"hasCompaction":false,"bodyBytes":1187}]

case=stale_local_marker_visible_only
status=200
[{"url":"/v1/responses","model":"gpt-5.5","previous_response_id":"","inputTypes":["message"],"hasCompaction":false,"bodyBytes":145}]

case=generic_openai_rejects_remote_opaque_compaction
status=503
[]

case=sub2api_http_rejects_rest_previous_id
status=400
[]

case=model_switch_synthetic_restore
status=200
[{"url":"/v1/responses","model":"gpt-5.4","previous_response_id":"","inputTypes":["message","message"],"hasCompaction":false,"bodyBytes":1192}]

case=real_codex_history_synthetic_restore
compact_status=200
continue_status=200
[{"url":"/v1/responses","model":"gpt-5.5","previous_response_id":"","inputTypes":["message","message"],"hasCompaction":false,"bodyBytes":1214}]

e2e_done=true
```

Real history fixture sample:

```text
source=2026/06/17/rollout-2026-06-17T12-04-31-019ed3c0-8b2a-74b1-b487-6ab6d70138cf.jsonl:594
history_items=11
fixture_items=9
first_item=codex history item 1: role=user text_chars=111 sha256=573236554e4863ec
```

Post-run cleanup checks:

```text
http://127.0.0.1:3001/api/status returned success=true.
No listener remained on 127.0.0.1:18081.
Token id 910001 was restored and does not use the temporary compacte2e key.
Test channels 910101-910104 and compact-e2e abilities were restored.
```

Cleanup SQL spot check:

```text
910001|1|codex-compact-e2e-token|f
910101|2|codex-compact-e2e-native-newapi
910102|2|codex-compact-e2e-sub2api-http
910103|1|codex-compact-e2e-synthetic-newapi
910104|2|codex-compact-e2e-base-newapi
compact-e2e ability rows=13
```

Observed compact log fields in the isolated dev DB:

```text
native_opaque_recorded lookup=recorded scope=strict profile=official_newapi marker=native_opaque capability_snapshot=true
state_restored lookup=hit scope=matched profile=trusted_newapi marker=synthetic_summary capability_snapshot=true
visible_only_fallback lookup=miss scope=not_found fallback=state_not_found_visible_input profile=trusted_newapi marker=synthetic_summary capability_snapshot=true
sub2api_http profile=sub2api_http capability_snapshot=true
```

## Boundary

This is an L1/L2-style local gate against the isolated dev stack and a deterministic fake upstream. It proves local routing semantics and cleanup behavior, but it does not replace a production canary or real upstream availability check.
