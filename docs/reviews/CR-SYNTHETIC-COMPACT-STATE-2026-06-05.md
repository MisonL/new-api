# Synthetic Compact State Review - 2026-06-05

## Scope

This record covers the synthetic Responses Compact state lifecycle for `resp_newapi_synthcmp_*` references.

The review input was isolated through `/tmp/new-api-synthetic-coderabbit` so external reviewers and CodeRabbit did not receive unrelated dirty working-tree changes from `/Volumes/Work/code/new-api`.

Patch artifacts for Claude and Gemini can be regenerated from the real working tree with `git diff --binary > /tmp/synthetic-compact-review-current.patch`, followed by appending untracked synthetic compact files with `git diff --no-index -- /dev/null <path>`.

## Contract

- Synthetic state creation requires `UserID`, `TokenID`, and `Group`; missing production scope returns `ErrSyntheticCompactStateScopeRequired`.
- Synthetic lookup only treats `resp_newapi_synthcmp_*` previous response ids and `newapi.synthetic.compact:resp_newapi_synthcmp_*` marker payloads as local state references.
- Ordinary upstream `previous_response_id` values and non-synthetic marker ids are not looked up in the local synthetic state store.
- When a database handle is available, state creation encrypts and persists the database record before returning a synthetic marker.
- Redis write failure is only tolerated after the database record has been persisted; the request remains recoverable through database fallback and the recovery path is logged.
- Database summaries are encrypted with AES-GCM. AAD length-prefixes and binds `ID`, `Model`, `UserID`, `TokenID`, and `Group`.
- Database ciphertext uses the model-level custom type: MySQL maps to `MEDIUMTEXT`; PostgreSQL, SQLite, and default dialects map to `TEXT`.
- Summary size is capped at `512 KiB` before storage or encryption.
- In-process cache is bounded by `syntheticCompactMemoryEntriesMax` and is not an unbounded `sync.Map`.
- Database-derived in-memory cache entries keep the database record's `ExpiresAt`; they do not get a fresh full TTL.
- Missing, expired, Redis, database, decryption, scope, and marker-cleanup marshal failures remain explicit errors.
- Expired database rows are removed by the master-only maintenance task in batches through `PruneExpiredSyntheticCompactStateRecords`.

## Files

- `main.go`
- `model/main.go`
- `model/synthetic_compact_state.go`
- `model/synthetic_compact_state_test.go`
- `service/responses_synthetic_compact.go`
- `service/responses_synthetic_compact_store.go`
- `service/responses_synthetic_compact_crypto.go`
- `service/responses_synthetic_compact_prompt.go`
- `service/responses_synthetic_compact_input.go`
- `service/responses_synthetic_compact_maintenance.go`
- `service/responses_synthetic_compact_memory.go`
- `service/responses_synthetic_compact_test.go`
- `relay/channel/openai/adaptor_responses_compaction_test.go`

## Reviewer Passes

### CodeRabbit

- Exit 0: `coderabbit --version`
  - `0.5.3`
- Exit 0: `coderabbit auth status --agent`
  - authenticated as `Mison-coder`
- Exit 0: `coderabbit review --agent -t uncommitted --dir service -c AGENTS.md .coderabbit.yaml`
  - Initial service pass raised timeout, TTL, observability, and cleanup findings.
  - Accepted: prune timeout and logging, database expiry retention in memory, marker cleanup error propagation, AES-GCM minimum length check, visible content truncation logging, content-array robust parsing, bounded local memory cache, recovery log level change, and scope validation ordering.
  - Not accepted: adding shutdown APIs to existing process-lifetime maintenance goroutines, delaying first prune until the first 24h tick, HKDF migration for an already scoped AES-GCM key, Redis backfill on DB hit, and broad debug logging for intentionally ignored non-visible input fragments.
- Exit 0: `coderabbit review --agent -t uncommitted --dir model -c AGENTS.md .coderabbit.yaml`
  - Initial model pass raised transaction protection for batch prune.
  - Accepted: batch select/delete now runs inside a transaction and uses stable `Order("id")` plus `Pluck("id", &ids)`.
- Exit 0: `coderabbit review --agent -t uncommitted --dir relay/channel/openai -c AGENTS.md .coderabbit.yaml`
  - `findings: 0`
- Exit 1: `coderabbit review --agent -t uncommitted --dir . -c AGENTS.md .coderabbit.yaml`
  - blocked by CodeRabbit `rate_limit`.
- Exit 1: final attempted `coderabbit review --agent -t uncommitted --dir model -c AGENTS.md .coderabbit.yaml`
  - blocked by CodeRabbit `rate_limit`, wait time reported as `7 minutes and 53 seconds`.
- Exit 1: final attempted `coderabbit review --agent -t uncommitted --dir service -c AGENTS.md .coderabbit.yaml`
  - blocked by CodeRabbit `rate_limit`, wait time reported as `4 minutes and 50 seconds`.
- Exit 0: rerun `coderabbit review --agent -t uncommitted --dir . -c AGENTS.md .coderabbit.yaml` against the isolated synthetic compact diff.
  - Critical: none.
  - Accepted: document HEAD/origin/build-info comparison.
  - Not accepted: adding shutdown APIs to process-lifetime maintenance goroutines, changing first-prune timing semantics, HKDF migration for the already domain-separated AES-GCM key, returning errors when `model.DB == nil` in model helpers that intentionally no-op for memory-only test/runtime modes, and broad clean-tree rebuild because this workspace was intentionally dirty before the task and the review input was isolated.
- Exit 0: final `coderabbit review --agent -t uncommitted --dir . -c AGENTS.md .coderabbit.yaml` against `/tmp/new-api-synthetic-coderabbit`.
  - `findings: 2`
  - Critical: none.
  - Major: none.
  - Minor: none.
  - Trivial only: memory janitor interval based on TTL, and O(1) front-pop for the 256-entry FIFO cache.
  - Not accepted: the current janitor interval is bounded to one hour on a 24h TTL, and the current FIFO eviction uses a fixed 256-entry cap where slice shifting remains bounded and simpler than ring/list bookkeeping.

### Claude

- Exit 0: first Claude read-only review against `/tmp/synthetic-compact-review-current.patch`.
  - Critical: none.
  - Warning accepted: recovery success paths should not use error-level logging.
  - Warning accepted: `BuildSyntheticCompactResponse` should validate scope before summary text.
  - Info noted: DB expiry retained in memory and stable prune ordering.
- Historical note: an earlier second Claude rerun was stopped after more than 12 minutes with no output file content. It was recorded as inconclusive and superseded by the final successful Claude read-only review below.
- Exit 0: final Claude read-only review against `/tmp/synthetic-compact-review-current.patch`.
  - Critical: none.
  - Accepted: canceled load context should not be logged as Redis recovery; test memory reset should be atomic.
  - Not accepted: changing empty-string split behavior back, refactoring small closure captures in prune, and optimizing the bounded cache lock path beyond the current 256-entry cap.
- Exit 0: final short Claude read-only review against `/tmp/synthetic-compact-review-current.patch`.
  - Critical: none.
  - Accepted in earlier final loops: Redis corrupt payload database fallback, Redis-hit TTL inheritance, atomic memory load/delete, explicit tool-call type allowlist, and linear invalid UTF-8 splitting.
  - Not accepted: model helpers returning nil when `model.DB == nil` is the existing low-level memory/test contract; adding fallback-to-memory when Redis fails and DB is nil would create a false success for an external dependency failure; moving scope validation into `storeSyntheticCompactState` would break direct low-level store tests and is redundant for the only production entry point, `BuildSyntheticCompactResponse`, which validates scope before storing.

### Gemini

- Exit 0: first Gemini read-only review against `/tmp/synthetic-compact-review-current.patch`.
  - Critical accepted: unconditional unbounded local memory cache.
  - Warning accepted: persistence should not be canceled by client request context; store path now uses `context.WithoutCancel(ctx)` plus a 10s timeout.
- Exit 0: second Gemini rerun after fixes.
  - Critical: none.
  - Warning: none.
  - Info accepted: use GORM `Pluck` for single-column prune IDs.
- Exit 0: final Gemini read-only review against `/tmp/synthetic-compact-review-current.patch`.
  - Critical: none.
  - Warning: none.
  - Info noted: Redis stores the short-lived cached synthetic state in plaintext while the database stores encrypted ciphertext; this is accepted because Redis is a trusted cache boundary in this runtime model and database persistence remains encrypted.
- Exit 0: final Gemini read-only review against `/tmp/synthetic-compact-review-current.patch`.
  - Critical: none.
  - Warning: none.
  - Info only: O(n) cache eviction remains bounded by `syntheticCompactMemoryEntriesMax = 256`; batch prune and AAD binding were accepted as robust; Gemini recommended direct merge without further code changes.

## Adopted Changes

- Added persisted `SyntheticCompactStateRecord` with context-aware CRUD and cross-dialect ciphertext type.
- Registered `SyntheticCompactStateRecord` in normal and fast migrations.
- Started the master-only synthetic compact prune task from `main.go`.
- Split synthetic compact logic by responsibility across store, crypto, prompt, input, maintenance, and memory files.
- Required production scope on synthetic state creation.
- Tightened marker parsing to local synthetic references only.
- Added encrypted database persistence with AES-GCM and length-prefixed AAD.
- Added `MEDIUMTEXT` for MySQL ciphertext storage and `TEXT` for other dialects.
- Added explicit store timeout and `context.WithoutCancel` for DB/Redis persistence.
- Added bounded in-process cache and regression coverage for cache limit.
- Preserved database expiry when DB records are restored into memory.
- Made marker cleanup marshal errors explicit.
- Added visible-content truncation observability without logging user content.
- Added robust object-only parsing for mixed content arrays.
- Added transaction, stable ordering, deleted-count return, and `Pluck` to expired-row pruning.
- Improved UTF-8 text splitting fallback.
- Hardened synthetic compact tests for isolated SQLite and broken Redis scenarios.
- Added explicit canceled-context handling on Redis load before database fallback.
- Added atomic test reset for the bounded memory store.
- Documented the store-context timeout boundary and malformed-input marker cleanup behavior in code comments.
- Added bounded-cache regression coverage that confirms the oldest item is evicted and the newest item remains present.
- Added Redis-load failure database fallback coverage and moved that fallback to a fresh DB timeout context.
- Changed expired-row pruning selection to order by `expires_at, id` so the expiry range remains the leading query key while keeping deterministic batches.
- Split read and write store contexts so reads honor caller cancellation while writes still complete under an independent timeout.
- Made UTF-8 split fallback scan only the nearby rune boundary window and hard-split malformed continuation-byte runs in linear time.
- Made Redis-load failure plus database miss return normal not-found semantics when the database is available, with regression coverage.
- Changed visible tool-call extraction to a known Responses tool type allowlist and ignored unknown tool-like `_call` types.
- Made memory-cache load and expired-entry deletion atomic under the store lock to avoid deleting a freshly refreshed entry.
- Made corrupt Redis JSON payloads fall back to the encrypted database source of truth, with regression coverage.
- Made Redis-hit memory cache entries inherit Redis remaining TTL instead of receiving a fresh full synthetic TTL.

## Verification

- Exit 0: `git diff --check`
  - Executed at: `2026-06-05T13:11:26Z`
  - no output.
- Exit 0: `go test ./model ./service -count=1 -run 'TestSyntheticCompact|TestApplySyntheticCompactState|TestBuildSyntheticCompact|TestLoadSyntheticCompact|TestPruneExpiredSyntheticCompact|TestStoreSyntheticCompact|TestSplitSyntheticCompactTextParts'`
  - Executed at: `2026-06-05T13:00:00Z`
  - `ok github.com/QuantumNous/new-api/model 0.274s`
  - `ok github.com/QuantumNous/new-api/service 1.242s`
- Exit 0: `go test ./controller ./model ./relay/common ./relay/helper ./service`
  - Executed at: `2026-06-05T13:00:00Z`
  - `ok github.com/QuantumNous/new-api/controller 4.679s`
  - `ok github.com/QuantumNous/new-api/model 2.842s`
  - `ok github.com/QuantumNous/new-api/relay/common (cached)`
  - `ok github.com/QuantumNous/new-api/relay/helper 14.113s`
  - `ok github.com/QuantumNous/new-api/service 1.574s`
- Exit 0: `go test ./relay/channel/openai ./relay/channel/codex -count=1`
  - Executed at: `2026-06-05T13:00:00Z`
  - `ok github.com/QuantumNous/new-api/relay/channel/openai 0.046s`
  - `ok github.com/QuantumNous/new-api/relay/channel/codex 0.032s`
- Exit 0: `scripts/build-docker-local.sh new-api-local:dev`
  - Executed at: `2026-06-05T13:08:53Z`
  - `built image=new-api-local:dev version=v1.1.0 commit=54e22200f433132927f928c0d370cb5f4fcfb30b-dirty date=2026-06-05T13:08:53Z`
- Exit 0: `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api`
  - Executed at: `2026-06-05T13:10:00Z`
  - `new-api-dev-isolated-new-api-1` was recreated and started.
- Exit 0: wait for Docker healthcheck.
  - `health=healthy`
- Exit 0: `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info`
  - Executed at: `2026-06-05T13:11:26Z`
  - `version=v1.1.0`
  - `commit=54e22200f433132927f928c0d370cb5f4fcfb30b-dirty`
  - `date=2026-06-05T13:08:53Z`
  - `source=https://github.com/MisonL/new-api`
- Exit 0: `git rev-parse HEAD`
  - `54e22200f433132927f928c0d370cb5f4fcfb30b`
- Exit 0: `git rev-parse origin/main`
  - `a04937e26698d663b7d594a4dcde6ef74e85bb46`
- Build-info comparison:
  - Container build commit prefix matches local `HEAD`: `54e22200f433132927f928c0d370cb5f4fcfb30b`.
  - Build-info has `-dirty` because the binary was built from the dirty local working tree.
  - `origin/main` is different from local `HEAD`; this verification proves the 3001 isolated dev binary matches local dirty `HEAD`, not `origin/main`.
- Exit 0: `curl -fsS http://127.0.0.1:3001/api/status`
  - Executed at: `2026-06-05T13:11:26Z`
  - Response contained `"success":true`.
- Exit 0: `docker ps --filter name=new-api-dev-isolated-new-api-1 --format '{{.Names}} {{.Image}} {{.Status}} {{.Ports}}'`
  - Executed at: `2026-06-05T13:11:26Z`
  - `new-api-dev-isolated-new-api-1 new-api-local:dev Up 43 seconds (healthy) 0.0.0.0:3001->3000/tcp, [::]:3001->3000/tcp`

## Environment Boundary

- Verified environment: local isolated dev stack on `127.0.0.1:3001`.
- Runtime database in this gate: PostgreSQL from `deploy/compose/dev-isolated.yml`.
- Production ports `3000` and `13000` were not modified or claimed as verified in this review.
- Docker status `(healthy)` only means the container healthcheck succeeded and the process is responsive. It does not prove source provenance.
- The `-dirty` build-info suffix means the image was built from local uncommitted changes. The isolated review input was restricted to synthetic compact state changes, but the running 3001 binary was built from the full dirty local working tree.
- The working tree was already dirty before this task; unrelated changes were not reverted, reviewed as part of this report, or claimed as production-ready by this review.
