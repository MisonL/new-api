# Main Merge Closeout - 2026-06-01

## Snapshot

- Sampling time: 2026-06-01 20:16:37 +0800
- Branch: `main`
- Main closeout docs commit: see current `main` tip; this file intentionally avoids self-referencing a final commit hash.
- Code merge commit: `0609b96d3ced3a2fb3f670cd3f62272cc4c50b0d`
- `origin/main` before push: `13bba9b91f17a3f5df9014e08cc98067255f124f`
- Local divergence before push: `origin/main...main = 0 100`

## Branch Disposition

- `codex/upstream-intake-20260530` is contained by `main`.
- `codex/protocol-conversion-webui` is contained by `main`.
- `main` contains merge commit `9a739fa84` for upstream intake and merge commit `0609b96d3` for protocol conversion WebUI.
- Pruned stale worktree registration `worktrees/upstream-src`; locked Claude agent worktrees were left untouched.

## Verification

| Command | Result |
| --- | --- |
| `go test ./controller ./model ./relay/common ./relay/helper ./service` | Passed |
| `go test ./common ./service -run 'TestLocalLogPreview|TestRelayErrorHandler|TestResetStatusCode' -count=1` | Passed |
| `cd web/classic && bun test tests/responsesCompactSettings.test.js tests/iframeMessaging.test.js` | Passed, 13 tests |
| `cd web/classic && bun run lint` | Passed |
| `cd web/default && bun test tests/channel-responses-compact.test.ts` | Passed, 13 tests |
| `cd web/default && bun run lint` | Passed |
| `cd web/default && bun run build` | Passed, including Safari and browser compatibility checks |
| `cd web && bun test tests/headerOverridePolicy.test.mjs tests/paramOverridePresetContracts.test.mjs tests/usageLogsAudit.test.mjs` | Passed, 27 tests |
| `git diff --check --` | Passed |
| `go test ./...` | Started, but produced no progress for an extended period and was interrupted; not counted as passed |

## Isolated Dev Deployment

Built local dev image:

```text
image=new-api-local:dev
version=v1.1.0
commit=current main tip after this record is committed
date=recorded by `/new-api --build-info`
```

Recreated only the isolated development app service:

```bash
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
```

Post-deploy checks:

- `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` must match the current `main` tip after the final closeout record is committed.
- `curl -fsS http://127.0.0.1:3001/api/status` returned `success=true`, `version=v1.1.0`, `server_address=http://127.0.0.1:3001`, `setup=true`.
- `new-api-dev-isolated-new-api-1` is `running healthy`, image `new-api-local:dev`.

## Production Boundary

Production container was not rebuilt or recreated.

- `new-api` remains `running healthy`.
- Image remains `new-api-local:prod-main`.
- Created time remains `2026-06-01T10:18:44.491392594Z`.
- Port mapping remains `127.0.0.1:13000->3000/tcp`.
