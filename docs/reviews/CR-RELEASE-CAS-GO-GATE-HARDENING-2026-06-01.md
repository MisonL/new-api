# Release, CAS, and Go Gate Hardening - 2026-06-01

## Scope

Sampling time: 2026-06-01T23:10:00+08:00.

This record covers three follow-up items from the current `main` state:

- Harden release checks for `web/default` and `web/classic`.
- Add application-side CAS ticket replay protection.
- Replace the unreliable bare `go test ./...` gate with an explicit Go package gate.

No production environment was rebuilt or changed.

## Control Contract

- Primary setpoint: turn the three documented follow-ups into repo-local, repeatable checks.
- Guardrails: do not change existing CAS start/callback URLs, do not add database schema, do not hide failures, and do not treat offline tests as real IdP validation.
- Boundary: release scripts, Docker build path, router startup validation, CAS callback handling, tests, and documentation.
- Rollback trigger: if any check requires a real IdP or production configuration, keep the code path fail-closed and document the L2 gate.

## Changes

### Dual frontend release gate

Added `scripts/write-frontend-release-metadata.sh`.

`scripts/build-release-frontends.sh` now writes `new-api-release.json` after each frontend build. The Dockerfile does the same for both frontend builder stages. As tightened by post-review, both paths call `scripts/write-frontend-release-metadata.sh` through `sh` so the writer does not depend on executable checkout mode.

The metadata writer rejects version, commit, and build date values outside the release-safe ASCII set before writing JSON. This prevents malformed metadata if a caller passes a value containing quotes, newlines, or shell-generated decoration.

The backend validates embedded frontend metadata before registering the web router:

- default frontend metadata must identify `frontend=default`.
- classic frontend metadata must identify `frontend=classic`.
- metadata version must match `common.Version` when the backend version is known.
- metadata commit must match `common.BuildCommit` when the backend commit is known.

This makes a mismatched or missing frontend build fail at startup instead of silently serving mixed assets.

### CAS ticket replay guard

Added an application-side ticket guard in the CAS callback path.

- When Redis is enabled, CAS tickets are reserved with Redis `SETNX` and a 10 minute TTL.
- If Redis is marked enabled but the Redis client is not initialized, reservation fails explicitly instead of falling back to process-local memory.
- Redis release uses a per-reservation token and conditional delete, so a stale release callback cannot remove a newer reservation for the same guard key.
- Without Redis, the current process uses an in-memory guard with the same TTL.
- In-memory mode logs `cas_ticket_guard_mode=in_memory` when first used and is only sufficient for a single instance or sticky-session deployment. Multi-instance deployments require Redis for cross-node replay protection.
- Replay errors and guard infrastructure errors are classified separately in the callback audit path.
- If the callback exits before login completion due to validation failure, state mismatch, panic, or other errors, the reservation is released by the defer guarded by `shouldReleaseTicket`.
- Once a login completes, the reservation is retained until TTL expiry.

This does not implement full CAS single logout. It closes the local replay gap for service tickets while keeping the existing CAS provider and browser callback shape unchanged.

### Go all-package gate

Added `scripts/go-test-all.sh`.

The previous bare `go test ./...` path was observed to hang before producing test output. `go list ./...` also hung in the same workspace. The current workspace contains large frontend dependency/build directories and nested agent worktrees, so recursive Go package discovery over `./...` is not a reliable local gate here. This script is a workspace-specific workaround; a clean repository without nested worktrees or generated frontend trees should continue to use the standard `go test ./...` gate.

The new script uses `rg --files` to enumerate Go source files while excluding:

- `web/node_modules`
- `web/default/node_modules`
- `web/classic/node_modules`
- `web/default/dist`
- `web/classic/dist`
- `.claude/worktrees`
- `.worktrees`
- `testdata`
- underscore-prefixed directories
- generic `node_modules`

It then runs one `go test` command over the explicit package list after loading the package file into a Bash array.

## Verification

Commands run:

```bash
go test ./router -run TestValidateFrontendReleaseMetadata -count=1
go test ./controller -run 'TestHandleCustomOAuthCASCallback(RejectsReplayTicketBeforeValidation|CreatesUser|UsesConfiguredServiceURLWithState|RejectsInvalidState|RejectsMissingTicket)' -count=1
scripts/go-test-all.sh -run '^$' -vet=off -timeout=45s
scripts/go-test-all.sh -count=1 -timeout=90s
bash -n scripts/build-release-frontends.sh scripts/go-test-all.sh
sh -n scripts/write-frontend-release-metadata.sh
tmpdir="$(mktemp -d)" && scripts/write-frontend-release-metadata.sh default "$tmpdir" 'v1.2.3' 'abc123' '2026-06-01T00:00:00Z'
tmpdir="$(mktemp -d)" && ! scripts/write-frontend-release-metadata.sh default "$tmpdir" 'bad"version' abc 2026
```

Results:

- Router release metadata tests passed.
- CAS callback and replay tests passed.
- Explicit Go package compile gate passed for 87 packages.
- Explicit Go package full test gate passed for 87 packages.
- Shell syntax checks passed for the release frontend scripts and the Go package gate.
- Frontend release metadata writer accepted a normal release tuple and rejected a value containing a JSON-breaking quote.

Observed during diagnosis:

- `go list ./...` did not produce output within the observation window.
- `go test ./... -count=1 -run '^$' -vet=off -timeout=45s` also did not produce output within the observation window.
- `rg --files` package enumeration returned immediately.

Post-verification tightening notes:

- A follow-up review tightened metadata writer input validation to prevent malformed JSON metadata.
- A follow-up review changed the CAS Redis-enabled-but-uninitialized path from process-local fallback to explicit failure.
- A follow-up review split CAS replay errors from guard infrastructure errors and added a unit test for the missing Redis client branch.
- A follow-up CodeRabbit review tightened the Go package gate, metadata temp-file cleanup, CAS test synchronization, CAS in-memory expiry cleanup, and Traditional Chinese replay-ticket text.
- The CAS guard review also added token-checked Redis release and a miniredis-backed reservation test.

Post-review environment note:

- During the follow-up review, `gofmt`, scoped `go test`, and recursive `rg` probes entered macOS `U` state in this workspace and did not exit after `SIGTERM` or `SIGKILL`. That suspended environment did not invalidate the earlier successful Go test results, but no additional host-Go verification was claimed from that state; later checks used narrow Docker-based Go invocations.

## Residual Gate Boundary

- CAS single logout remains unimplemented. The current data model does not store CAS session indexes, so real SLO needs a separate design for session tracking and invalidation.
- Redis-backed CAS replay protection is covered by a miniredis command-level test. It is not a replacement for a long-running production Redis compatibility gate.
- Frontend metadata files are generated build artifacts under ignored `dist` directories. They are not committed; release and Docker build paths are responsible for generating them before backend embedding.
- Local backend startup from existing `dist` directories now requires frontend metadata. Developers must generate it with `scripts/write-frontend-release-metadata.sh` or the equivalent local build step before starting the backend.
- Runtime metadata mismatches detected by backend validation require rebuilding the Docker image with correct frontend metadata, or booting a documented recovery image. There is no emergency runtime bypass.
- `scripts/go-test-all.sh` is the new local all-Go gate for this repository shape. A future cleanup can reduce the need for it by moving nested agent worktrees and large generated trees outside the repo root and returning to `go test ./...`.
