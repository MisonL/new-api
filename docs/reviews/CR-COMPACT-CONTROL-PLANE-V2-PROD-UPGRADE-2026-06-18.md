# Compact Control Plane v2 production upgrade gate - 2026-06-18

## Scope

This gate covers the production fix for Codex Responses compact routing:

- Distinguish local Codex `compaction_trigger`, local synthetic compact markers, and remote opaque compaction items.
- Avoid false `503` skips for legacy or empty upstream profiles.
- Keep conservative passthrough behavior for `sub2api_http`, `generic_openai`, `generic_proxy`, and `chat_only_proxy`.
- Preserve Header Profile / User-Agent strategy on channel edits.
- Verify behavior in isolated Docker dev environment before touching production.

Production must not be upgraded unless the operator explicitly authorizes it after reading this gate.

Required authorization wording before any production mutation:

```text
授权升级正式环境，按 CR-COMPACT-CONTROL-PLANE-V2-PROD-UPGRADE-2026-06-18 执行。
```

Without that explicit authorization, only read-only production checks are allowed.

## Current evidence

Sampled at 2026-06-18.

- Isolated dev backend: `new-api-dev-isolated-new-api-1`
- Dev port: `http://127.0.0.1:3001`
- Dev image currently running this gate: `new-api-local:compact-control-plane-v2-20260618`
- Candidate image built for this gate: `new-api-local:compact-control-plane-v2-20260618`
  - image id: `sha256:1131201202613a651dc703b2deb7db09e6cc7ce12885263979cc81aebec5117c`
  - image created: `2026-06-18T04:36:18.596566149Z`
  - build info:
    - `version=v1.1.0`
    - `commit=75e19a2f1a4746e8b1c63d3331e3cf2eadadd8cb-dirty`
    - `date=2026-06-18T04:34:42Z`
- Dev build info after switching 3001 to the candidate image:
  - `version=v1.1.0`
  - `commit=75e19a2f1a4746e8b1c63d3331e3cf2eadadd8cb-dirty`
  - `date=2026-06-18T04:34:42Z`
- Dev health: `/api/status` returned `success=true`, `version=v1.1.0`, `theme=classic`.
- Read-only production precheck after the final dev smoke:
  - `new-api`: `new-api-local:prod-fixed`, healthy, `127.0.0.1:13000->3000`
  - `/new-api --build-info`: `version=v1.1.0`, `commit=unknown`, `date=unknown`
  - `/api/status`: `success=true`, `version=v1.1.0`, `theme=classic`, `setup=true`
  - recent production logs did not match the known false-local-error strings.

## Requirement audit matrix

| Requirement | Evidence | Status |
| --- | --- | --- |
| Distinguish local Codex `compaction_trigger`, synthetic compact marker, and remote opaque compaction item | `dto/openai_request.go`, `service/responses_synthetic_compact.go`, `relay/responses_handler_test.go`, `relay/channel/openai/adaptor_responses_compaction_test.go` | Passed in dev |
| Avoid false `503` for legacy or empty upstream profile | `dto/channel_settings.go`, `controller/relay_retry_test.go`, `relay/responses_handler_test.go` | Passed in dev |
| Keep `sub2api_http`, `generic_openai`, `generic_proxy`, and `chat_only_proxy` conservative for remote compaction item passthrough | `dto/channel_settings.go`, `relay/responses_handler_test.go`, `service/log_info_generate_test.go` | Passed in dev |
| Preserve original new-api/sub2api upstream and downstream compatibility | `dto/openai_responses_compaction_request.go`, `dto/openai_request_zero_value_test.go`, `relay/helper/valid_request_test.go`, `scripts/compact-control-plane-e2e.sh` | Passed in dev |
| Responses-to-Chat rejects or strips unsupported compact/native-only payloads correctly | `relay/responses_via_chat.go`, `relay/responses_via_chat_test.go`, `relay/common/responses_input_filter_test.go` | Passed in dev |
| Compact after model/channel/profile switching | `controller/relay_retry_test.go`, `scripts/compact-control-plane-e2e.sh` case `model_switch_synthetic_restore` | Passed in dev |
| Expired synthetic state falls back to visible-only when possible | `service/responses_synthetic_compact.go`, `relay/responses_via_chat_test.go`, `relay/channel/codex/adaptor_test.go` | Passed in dev |
| Header Profile and User-Agent settings are not cleared on channel edits | `relay/channel/api_request_test.go`, `controller/channel_test_internal_test.go`, `web/classic/src/components/table/channels/modals/headerProfile.helpers.test.js` | Passed in dev |
| 3001 isolated Docker uses copied production-usable channels, not official OpenAI, for real Codex smoke | usage log rows `[REDACTED]`, copied channel `[REDACTED]`, base URL `[REDACTED]` | Passed in dev |
| Try to recover lost production channel config from local DB, Docker state, and Claude history | documented searches below; `[REDACTED-CHANNEL-DOMAIN]` found only in Claude-history user text, no complete channel row found | Best effort complete |
| Production upgrade and post-upgrade verification | Requires explicit user authorization before mutation | Pending authorization |

## Verification completed in isolated dev

Commands run:

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service ./relay/channel/openai ./relay/channel/codex -count=1
cd web/classic && bun test src/components/table/channels/modals/headerProfile.helpers.test.js src/components/table/channels/modelTestRuntimeConfig.test.js src/helpers/responsesCompactSettings.test.js
cd web/default && bun test src/features/channels/lib/channel-form.test.ts
cd web/default && bun run lint
cd web/default && bun run build
bash scripts/compact-control-plane-e2e.sh
```

Results:

- Go tests passed.
- Classic channel Header Profile tests passed.
- Default channel form tests passed.
- `web/default` lint passed.
- `web/default` build passed, including Safari compatibility check.
- `scripts/compact-control-plane-e2e.sh` passed and left zero test token/channel/ability rows.
- The E2E script cleanup order was fixed and re-run. After cleanup, production copied channel abilities remained intact:
  - `compact-e2e` copied production channel `gpt-5.5` abilities count: `8`

E2E covered cases:

- `native_newapi_compact`
- `synthetic_newapi_compact_and_continue`
- `stale_local_marker_visible_only`
- `generic_openai_rejects_remote_opaque_compaction`
- `sub2api_http_rejects_rest_previous_id`
- `model_switch_synthetic_restore`
- `real_codex_history_synthetic_restore`

Real copied-production-channel smoke verification:

- The final smoke test did not use an official OpenAI channel.
- Current production-usable channels were copied into the isolated dev group `compact-e2e`.
- Copied channels included a bounded redacted ID set; the final requested smoke test isolated one production channel only.
- The successful smoke channel was:
  - production id: `[REDACTED-PROD-CHANNEL-ID]`
  - dev copied id: `[REDACTED-DEV-CHANNEL-ID]`
  - name: `[REDACTED-COPIED-CHANNEL-NAME]`
  - base URL: `[REDACTED-PROD-API-DOMAIN]`
  - group: `compact-e2e`
  - models include: `codex-auto-review`, `gpt-5.4-mini`, `gpt-5.5`, `gpt-5.5-openai-compact`
- Codex CLI version: `0.140.0`.
- Provider: custom provider `dev-new-api`, `base_url=http://127.0.0.1:3001/v1`, `wire_api=responses`.
- API token was read from the isolated dev database into process environment only. No token was printed or written.
- Fresh request:
  - `codex exec` exit code: `0`
  - events: `thread.started`, `turn.started`, `item.completed`, `turn.completed`
  - usage log row: `[REDACTED-USAGE-ROW]`
- Real history resume request:
  - session id: `[REDACTED-CODEX-SESSION-ID]`
  - `codex exec resume ...` exit code: `0`
  - events: `thread.started`, `turn.started`, `item.completed`, `turn.completed`
  - usage log row: `[REDACTED-USAGE-ROW]`
- The only Codex stderr warnings were plugin catalog and plugin manifest warnings. They were not generated by `new-api`, and both `new-api` requests completed successfully.
- No matching 3001 container log errors were found for:
  - `synthetic compact state`
  - `input item type`
  - `chat compatibility mode`
  - `not implemented`
  - `No available channel`
  - `Service Unavailable`
  - `status_code=503`
  - `panic`
  - `fatal`
  - `previous_response_id`

## Lost channel config recovery status

Searches completed:

- Production PostgreSQL current `channels` and known channel backup tables.
- Isolated dev PostgreSQL current `channels` and known channel backup tables.
- Local SQLite files under this repository.
- Claude history for session `5637996a-0918-48a5-b05c-561324492519`.
- Docker containers and images relevant to the failed rollout.

Result:

- `[REDACTED-CHANNEL-DOMAIN]` was found only in the user message inside Claude history.
- No recoverable full channel row or settings payload was found.
- Old upstream Postgres volume read-only startup failed; it was not mounted writable and was not modified.

## Production state before upgrade

Observed current production containers:

- `new-api`: `new-api-local:prod-fixed`, healthy, `127.0.0.1:13000->3000`
- `postgres`: `postgres:15`
- `redis`: `redis:latest`

Observed production compose metadata:

- Compose project: `new-api`
- Compose config file: `/Volumes/Work/code/new-api/docker-compose.yml`
- Compose working dir: `/Volumes/Work/code/new-api`

Candidate image:

- Tag: `new-api-local:compact-control-plane-v2-20260618`
- Image id: `sha256:1131201202613a651dc703b2deb7db09e6cc7ce12885263979cc81aebec5117c`
- Build info: `version=v1.1.0`, `commit=75e19a2f1a4746e8b1c63d3331e3cf2eadadd8cb-dirty`, `date=2026-06-18T04:34:42Z`

Rollback image before upgrade:

- Tag: `new-api-local:prod-fixed`
- Image id: `sha256:29d2ee8f7e67606b9d227c0ea00bfce24ba8f4aeb3032eba84f14b9121a6044f`

Do not remove this image before production validation completes.

## Upgrade command plan

These commands are templates and must be executed only after explicit user authorization.

Preferred guarded command:

```bash
COMPACT_PROD_CANDIDATE_IMAGE=new-api-local:compact-control-plane-v2-20260618 \
  scripts/compact-control-plane-prod-upgrade.sh --dry-run
COMPACT_PROD_CANDIDATE_IMAGE=new-api-local:compact-control-plane-v2-20260618 \
CONFIRM_PROD_UPGRADE='授权升级正式环境，按 CR-COMPACT-CONTROL-PLANE-V2-PROD-UPGRADE-2026-06-18 执行。' \
  scripts/compact-control-plane-prod-upgrade.sh --execute
```

`COMPACT_PROD_CANDIDATE_IMAGE` must be the image built from the exact source tree that passed the dev gate. The guarded script intentionally has no default candidate image; this prevents accidentally redeploying an older tag when the working tree contains newer uncommitted fixes.

Manual expanded sequence:

1. Record current production state.

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}' | rg 'new-api|postgres|redis|NAMES'
docker exec new-api /new-api --build-info
curl -fsS http://127.0.0.1:13000/api/status
```

2. Build an immutable production candidate image from the current source.

```bash
scripts/build-docker-local.sh new-api-local:compact-control-plane-v2-20260618
docker image inspect new-api-local:compact-control-plane-v2-20260618 --format '{{.Id}}'
```

3. Backup production database before switching image.

```bash
mkdir -p backups
docker exec postgres pg_dump -U root -d new-api > backups/new-api-before-compact-control-plane-v2-20260618.sql
```

4. Switch only the `new-api` application container to the candidate image.

Preferred approach: update the compose image variable to:

```text
NEW_API_IMAGE=new-api-local:compact-control-plane-v2-20260618
```

Then restart only the app service:

```bash
cd /Volumes/Work/code/new-api
NEW_API_IMAGE=new-api-local:compact-control-plane-v2-20260618 \
  docker compose -f /Volumes/Work/code/new-api/docker-compose.yml up -d --no-deps --force-recreate new-api
```

5. Immediate production verification.

```bash
docker exec new-api /new-api --build-info
curl -fsS http://127.0.0.1:13000/api/status
docker logs --tail 300 new-api 2>&1 | rg 'panic|fatal|synthetic compact state|input item type|chat compatibility mode|not implemented|No available channel|Service Unavailable|Invalid token| 503 ' || true
```

6. Real Codex verification.

- Run one short Codex request through production.
- Run one resumed Codex history request if safe.
- Confirm production usage logs do not show false local errors:
  - `503`
  - `not implemented`
  - `synthetic compact state not found or expired`
  - `input item type "compaction" is not supported in chat compatibility mode`

## Rollback plan

Rollback trigger:

- `/api/status` fails.
- `new-api` container becomes unhealthy or exits.
- Real Codex production requests return the known local false errors above.
- Logs show a new local panic or conversion error in the compact control path.

Rollback command:

```bash
cd /Volumes/Work/code/new-api
NEW_API_IMAGE=new-api-local:prod-fixed \
  docker compose -f /Volumes/Work/code/new-api/docker-compose.yml up -d --no-deps --force-recreate new-api
docker exec new-api /new-api --build-info
curl -fsS http://127.0.0.1:13000/api/status
docker logs --tail 300 new-api 2>&1 | rg 'panic|fatal| 503 ' || true
```

Guarded rollback behavior:

- `scripts/compact-control-plane-prod-upgrade.sh --execute` automatically attempts rollback to the recorded rollback image if the app recreate command, `/api/status`, build-info, or recent-log verification fails.
- After automatic rollback, manual inspection is still required before retrying.

Database rollback is not expected for this code-only upgrade. Use the `pg_dump` file only if a separate data mutation is accidentally introduced.

## Status

Ready for explicit production upgrade authorization.

Current gate status: dev verification is complete, production has not been touched.
