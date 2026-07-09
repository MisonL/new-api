# CR-NPM-CLI-VERSION-CACHE-2026-07-07

## Scope

- npm CLI version options cache and refresh contract.
- Header Profile library version source, refresh time, cache age and last error display.
- AdminAuth and rate limit protection for npm version option endpoints.
- Request header policy fallback for usage log display.
- Isolated development stack on port 3001.

## Code Review Summary

- OMP read-only review: no P0/P1/P2/P3 actionable issue remained. It confirmed GET version options is cache/DB only, POST persist failure does not pollute process cache, and request_header_policy `{}` fallback is covered.
- Claude read-only review: no P0/P1 issue. It recommended adding `request_header_policy: null` coverage, which is now covered by `web/default/tests/request-header-policy.test.ts`.
- Follow-up local review found one response contract gap: refresh failures had top-level `code` but not `data.code`. This is fixed in `controller/npm_cli_version.go` and covered by `controller/npm_cli_version_test.go`.
- Second OMP read-only review found that diagnostics exposed raw backend error messages. This is fixed by returning code-based safe summaries in `last_error.message` and preserving raw details only in backend logs.
- Second OMP read-only review also noted that MySQL text comparison can be collation-sensitive. The CAS update now uses `BINARY value = BINARY ?` when `common.UsingMySQL` is true; a real MySQL 8.4 smoke later confirmed the expected `RowsAffected` semantics and case-sensitive comparison behavior.
- Third review used the real local CLI tools through the nvm environment. `claude` 2.1.179 found that diagnostics for missing packages serialized Go's zero `time.Time` as `0001-01-01T00:00:00Z`; this is fixed by making diagnostic `refreshed_at` optional and covered by service/controller tests. It also found that this document's companion operations note described insufficient privilege as a real HTTP 403; that note now reflects the existing `AdminAuth` convention of `HTTP 200 + success:false` for insufficient privileges.
- Real `omp` 16.3.4 CLI was located at `/usr/local/bin/omp`; the default `newapi-responses/gpt-5.5` run failed with `400 Unsupported parameter: max_output_tokens`, and Google Antigravity model probes failed with provider errors. A later `newapi-chat-completions/deepseek-v4-flash` retry completed with no P0/P1 issue. It flagged the same zero-time risk on the normal GET options response; `NpmCLIVersionOptionsResponse.refreshed_at` is now optional too and covered by service/controller tests.
- Final local sweep found one remaining frontend UX edge: diagnostics GET had no explicit timeout while version GET and refresh did. Default and classic diagnostics requests now use the same 10 second bounded timeout, with source-level tests locking the contract.
- Final real CLI recheck used local `claude` 2.1.179 and `omp` 16.3.4 through the nvm environment. `omp` reported no P0/P1/P2/P3 findings. `claude` reported only non-blocking edge cases; the service now clamps diagnostic `cache_age_ms` at zero for future timestamps, and tests lock the helper contract. The `last_errors` merge behavior remains intentional: errors newer than a package refresh are preserved so diagnostics do not lose a later failure reason.
- Follow-up frontend cleanup resolved the remaining non-blocking UI observations: default and classic manual version reload handlers now no-op after unmount, and classic diagnostics HTTP errors use the same response payload/status normalization before falling back to a generic thrown error code.
- Final CLI cross-check used local `claude` 2.1.179 and `omp` 16.3.4 through the nvm environment. The only confirmed remaining cleanup was classic diagnostics passing Axios internal `error.code` into UI classification when no known backend code existed. Classic now only accepts known npm version error codes there; unknown transport codes fall back to `npm_version_load_failed`.
- Final optimization pass added diagnostics `summary` and in-process refresh `metrics` to the backend response. The management performance settings pages in both default and classic now expose a read-only npm CLI version diagnostics entry, so operators can inspect recorded/missing packages, cache age, safe last error codes and scheduled/manual refresh state without opening the Header Profile template picker.
- Final Claude CLI review later found additional context and auth-test hardening points. The claim that common-user diagnostics must return HTTP 401 was not adopted because `middleware.AdminAuth()` intentionally returns `HTTP 200 + success:false` for insufficient privileges. The actionable items are addressed: GET version options propagates the request context into the DB fallback read, refresh persist/CAS reads and writes propagate the caller context, last-error persist logging keeps the caller context, and the unauthenticated route test no longer sends a `New-Api-User` header. The remaining P3 note, successful manual refresh leaving `last_manual_code` empty, remains intentional because the field is omitted on success and the UI displays `-`; success/failure counters carry the success state. A final OMP 16.3.4 CLI recheck was invoked with `-p @prompt-file` through the same nvm environment, but it did not return output before the 180 second deadline, so it is not counted as a passing review.
- 2026-07-08 follow-up closed the remaining observability gaps from the optimization list. Same-package manual refreshes are now coalesced with in-process singleflight, diagnostics return bounded `recent_errors` history and stable `recommended_action`, and both default/classic management diagnostics display those fields. `last_errors` remains the current blocking state; `recent_errors` is retained history and is not deleted by a later successful refresh.
- 2026-07-08 final smoke found one response-contract edge: empty diagnostics `recent_errors` slices were omitted by `omitempty`, so operators could not rely on a stable array field. `NpmCLIVersionDiagnostic.recent_errors` now always serializes as an array, and the controller test checks the raw JSON contract.
- 2026-07-08 final CLI cross-check used local `omp` 16.3.4 and `claude` 2.1.179 through the nvm Node v24.14.0 environment. A large OMP bundle was interrupted after no output; the smaller diff bundle returned usable results. Both tools reported no P0/P1/P2 blockers. Claude listed only non-blocking tradeoffs: background refresh uses `context.Background()`, per-package persistence writes are more granular than one batch write, and same-package `singleflight` uses the first caller context.

## Verification

Commands were run on 2026-07-07 and 2026-07-08 from `/Volumes/Work/code/new-api`.

### Go

```bash
PATH=/usr/local/bin:$PATH /usr/local/bin/go test -count=1 -timeout=180s ./controller ./service -run 'NpmCLI|NpmVersion|npmVersion'
```

Result: passed.

After the third CLI review follow-up, the same command was re-run and passed again for the optional diagnostics `refreshed_at` fix.

After the final real CLI recheck follow-up, the focused controller/service/router command was re-run and passed again for the `cache_age_ms` non-negative clamp.

After the final diagnostics summary and refresh metrics pass:

```bash
export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"; nvm use --silent v24.14.0 >/dev/null
go test -count=1 -timeout=180s ./service ./controller ./router -run 'NpmCLI|NpmVersion|npmVersion|TestChannelNpmVersionOptionRoutesUseAdminAuthAndCriticalRateLimit'
```

Result: passed.

After the final Claude CLI P2 follow-up for GET fallback request context, refresh persist context, and diagnostics auth coverage:

```bash
export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"; nvm use --silent v24.14.0 >/dev/null
go test -count=1 -timeout=180s ./service ./controller ./router -run 'NpmCLI|NpmVersion|npmVersion|TestChannelNpmVersionOptionRoutesUseAdminAuthAndCriticalRateLimit'
```

Result: passed.

After the 2026-07-08 singleflight and diagnostics-history follow-up:

```bash
export PATH="/usr/local/go/bin:$PATH"
gofmt -w service/npm_cli_version.go service/npm_cli_version_cache.go service/npm_cli_version_diagnostics.go service/npm_cli_version_test.go
go test -count=1 -timeout=180s ./service -run 'NpmCLI|NpmVersion|npmVersion'
```

Result: passed.

```bash
PATH=/usr/local/bin:$PATH /usr/local/bin/go test -count=1 -timeout=180s ./router -run TestChannelNpmVersionOptionRoutesUseAdminAuthAndCriticalRateLimit
```

Result: passed.

After the third CLI review follow-up, the same command was re-run and passed again.

After the 2026-07-08 stable `recent_errors` response-contract fix:

```bash
export PATH="/usr/local/go/bin:$PATH"
go test -count=1 -timeout=180s ./controller ./service -run 'NpmCLI|NpmVersion'
go test -count=1 -timeout=180s ./controller ./model ./relay/common ./relay/helper ./service
```

Result: passed.

### Frontend Unit Tests

```bash
PATH=/usr/local/bin:/Users/mison/.bun/bin:$PATH /Users/mison/.bun/bin/bun test ./tests/channel-header-profile-strategy.test.ts ./tests/request-header-policy.test.ts
```

Result: 35 passed, 0 failed.

After the final real CLI recheck follow-up, the same default frontend command was re-run and passed again.

```bash
PATH=/usr/local/bin:/Users/mison/.bun/bin:$PATH /Users/mison/.bun/bin/bun test src/components/table/channels/modals/headerProfile.helpers.test.js
```

Result: 105 passed, 0 failed.

After the final real CLI recheck follow-up, the same classic frontend command was re-run and passed again.

After the final CLI cross-check cleanup, the same classic frontend command was re-run and passed again for the diagnostics error-code normalization.

### Frontend Build

```bash
PATH=/usr/local/bin:/Users/mison/.bun/bin:$PATH /Users/mison/.bun/bin/bun run lint
```

Directory: `web/classic`.

Result: passed, all matched files use Prettier style.

Earlier local macOS `/Volumes` commands below were interrupted after they stopped producing output for more than 15 minutes:

```bash
cd web/default && PATH=/usr/local/bin:/Users/mison/.bun/bin:$PATH /Users/mison/.bun/bin/bun run typecheck
cd web/default && PATH=/usr/local/bin:/Users/mison/.bun/bin:$PATH /Users/mison/.bun/bin/bun run build
cd web/classic && PATH=/usr/local/bin:/Users/mison/.bun/bin:$PATH /Users/mison/.bun/bin/bun run build
```

Observed boundary:

- `web/default` build printed `ready built in 4m 19.3s`, then the wrapper did not exit.
- `web/default` typecheck and `web/classic` build remained in long-running filesystem wait.
- Final build validation was completed through Docker, which built both frontends and Go successfully.

After the final diagnostics-timeout follow-up, `web/default` typecheck was re-run with the nvm Node path loaded:

```bash
cd web/default && PATH=/usr/local/bin:/Users/mison/.bun/bin:$PATH /Users/mison/.bun/bin/bun run typecheck
```

Result: passed. The command completed after the TypeScript incremental cache was populated; no script or tsconfig coverage reduction was applied.

After the final diagnostics summary and settings-page pass:

```bash
export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"; nvm use --silent v24.14.0 >/dev/null
cd web/default && bun test ./tests/channel-header-profile-strategy.test.ts ./tests/request-header-policy.test.ts
```

Result: 36 passed, 0 failed.

```bash
export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"; nvm use --silent v24.14.0 >/dev/null
cd web/classic && bun test src/components/table/channels/modals/headerProfile.helpers.test.js
```

Result: 106 passed, 0 failed.

```bash
export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"; nvm use --silent v24.14.0 >/dev/null
cd web/default && bun run lint
```

Result: passed.

```bash
export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"; nvm use --silent v24.14.0 >/dev/null
cd web/default && bun run build
```

Result: passed. `rsbuild` completed in 1m 39.5s; Safari compatibility rewrite and compatibility check passed.

```bash
export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"; nvm use --silent v24.14.0 >/dev/null
cd web/classic && bun run lint
```

Result: passed, all matched files use Prettier style.

```bash
export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"; nvm use --silent v24.14.0 >/dev/null
cd web/classic && bun run build
```

Result: passed. Vite completed in 4m 56s; Safari compatibility rewrite and compatibility check passed.

The same final pass attempted:

```bash
export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"; nvm use --silent v24.14.0 >/dev/null
cd web/default && bun run typecheck
```

Boundary: interrupted after about 7 minutes with no TypeScript diagnostics. The actual `web/default` production build passed afterward and covered the new TSX files.

### Docker Isolated Build

```bash
PATH=/usr/local/bin:/Applications/Docker.app/Contents/Resources/bin:/Users/mison/.bun/bin:$PATH scripts/build-docker-local.sh new-api-local:dev
```

Result: passed.

Build info:

```text
built image=new-api-local:dev version=v1.1.0 commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty date=2026-07-07T12:57:13Z
```

The Docker build completed:

- `web/default` rsbuild build.
- `web/default` Safari compatibility rewrite and compatibility check.
- `web/classic` vite build.
- `web/classic` Safari compatibility rewrite and compatibility check.
- Go production binary build.

After the final diagnostics summary and settings-page pass, the isolated image was rebuilt again:

```bash
export NVM_DIR="$HOME/.nvm"; . "$NVM_DIR/nvm.sh"; nvm use --silent v24.14.0 >/dev/null
scripts/build-docker-local.sh new-api-local:dev
```

Result: passed.

Build info:

```text
built image=new-api-local:dev version=v1.1.0 commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty date=2026-07-07T15:47:36Z
```

After the final context hardening pass, the isolated image was rebuilt again.

Result: passed.

Build info:

```text
built image=new-api-local:dev version=v1.1.0 commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty date=2026-07-07T17:04:15Z
```

After the 2026-07-08 stable `recent_errors` response-contract fix, the isolated image was rebuilt again.

Result: passed.

Build info:

```text
built image=new-api-local:dev version=v1.1.0 commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty date=2026-07-08T02:35:28Z
```

### 3001 Isolated Runtime

```bash
PATH=/usr/local/bin:/Applications/Docker.app/Contents/Resources/bin:$PATH docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
PATH=/usr/local/bin:/Applications/Docker.app/Contents/Resources/bin:$PATH docker inspect --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' new-api-dev-isolated-new-api-1
PATH=/usr/local/bin:/Applications/Docker.app/Contents/Resources/bin:$PATH docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

Result:

```text
running healthy
version=v1.1.0
commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty
date=2026-07-07T12:57:13Z
source=https://github.com/MisonL/new-api
/api/status success=true
```

After the final diagnostics summary and settings-page pass, the isolated container was force-recreated again.

Result:

```text
running healthy
version=v1.1.0
commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty
date=2026-07-07T15:47:36Z
source=https://github.com/MisonL/new-api
/api/status success=true theme=classic
```

After the final context hardening pass, the isolated container was force-recreated again.

Result:

```text
running healthy
version=v1.1.0
commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty
date=2026-07-07T17:04:15Z
source=https://github.com/MisonL/new-api
/api/status success=true theme=classic
```

After the 2026-07-08 stable `recent_errors` response-contract fix, the isolated container was force-recreated again.

Result:

```text
running healthy
version=v1.1.0
commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty
date=2026-07-08T02:35:28Z
source=https://github.com/MisonL/new-api
/api/status success=true version=v1.1.0 theme=classic
```

### 3001 API Smoke

Temporary admin and common users were inserted into the isolated PostgreSQL database with one-time `access_token` values, used for this smoke through the same `AdminAuth()` path, then deleted. Token values were not printed.

Result:

```text
unauth_get_http=401 success=false message=Unauthorized, not logged in and no access token provided
common_get_http=200 success=false message=Unauthorized, insufficient privileges
admin_get_http=200 success=true source=recorded latest_version=0.142.5 options=6 has_refreshed_at=true
admin_diag_http=200 success=true packages=5 sources=recorded last_error_count=1 negative_cache_age_count=0 zero_time_present=false
admin_post_unsupported_http=200 success=false top_code=npm_package_unsupported data_code=npm_package_unsupported source=npm message=unsupported npm package: @new-api/nonexistent-smoke-package
```

After the final diagnostics summary and settings-page pass, a second isolated diagnostics smoke inserted a temporary admin access token, called `GET /api/channel/npm_version_options/diagnostics`, printed only aggregate fields, then deleted the user.

Result:

```text
success=true
packages=5
summary_package_count=5
summary_recorded_count=5
summary_missing_count=0
summary_last_error_count=0
metrics_scheduled_runs=1
metrics_manual_runs=0
has_generated_at=true
refresh_interval_ms=600000
registry_timeout_ms=5000
has_negative_cache_age=false
```

After the final context hardening pass, the same diagnostics smoke was re-run against the `2026-07-07T17:04:15Z` isolated container.

Result:

```text
success=true
packages=5
summary_package_count=5
summary_recorded_count=5
summary_missing_count=0
summary_last_error_count=0
metrics_scheduled_runs=1
metrics_manual_runs=0
has_generated_at=true
refresh_interval_ms=600000
registry_timeout_ms=5000
has_negative_cache_age=false
```

After the 2026-07-08 stable `recent_errors` response-contract fix, the same AdminAuth smoke was re-run against the `2026-07-08T02:35:28Z` isolated container.

Result:

```text
unauth_http=401 success=false message=Unauthorized, not logged in and no access token provided
common_http=200 success=false message=Unauthorized, insufficient privileges
admin_diag_http=200 success=true packages=5 summary_package_count=5 summary_recorded_count=5 summary_missing_count=0 summary_last_error_count=0 metrics_scheduled_runs=1 metrics_manual_runs=0 has_generated_at=true all_have_recent_errors=true all_have_recommended_action=true has_negative_cache_age=false
admin_get_http=200 success=true source=recorded latest_version=0.143.0 options=6 has_refreshed_at=true
```

Interpretation:

- Unauthenticated requests are rejected with HTTP 401.
- Common users hit the existing middleware convention: HTTP 200 with `success=false` and insufficient privilege message.
- Admin GET reads recorded backend cache.
- Admin diagnostics returns package states.
- Admin diagnostics may include the safe last error produced by the unsupported-package POST smoke; the value remains code-based and contains no raw backend error.
- Admin POST refresh returns structured top-level and `data.code` failure for unsupported packages.

### Production Read-only Check

The local production container was checked read-only on 2026-07-07. No production state was modified.

Commands:

```bash
docker exec new-api /new-api --build-info
curl -fsS http://127.0.0.1:13000/api/status
```

Result:

```text
version=v1.1.0
commit=f19d3437c80f8b64ec04d2fa453058f2a2a36530-dirty
date=2026-06-28T15:27:03Z
source=https://github.com/MisonL/new-api
/api/status success=true theme=classic version=v1.1.0
```

Interpretation: production is still running the 2026-06-28 build, not the final isolated development build from 2026-07-07T17:04:15Z.

### PostgreSQL CAS Smoke

Real PostgreSQL container: `new-api-dev-isolated-postgres-1`, database `new-api-dev`, table `options`.

Checks:

```text
cas_first_rows=1
cas_stale_rows=0
cas_large_rows=1
```

Interpretation:

- Exact value compare-and-swap updates one row for current value.
- Stale value compare-and-swap updates zero rows.
- 200KB JSON text value comparison works on PostgreSQL.

### MySQL CAS Smoke

Real temporary MySQL container: `mysql:8.4`, table `options`, collation `utf8mb4_general_ci`. The first image pull failed with Docker registry EOF, then a retry succeeded. The container was deleted after the smoke.

Checks:

```text
cas_first_rows=1
cas_stale_rows=0
cas_case_seed_rows=1
cas_case_sensitive_rows=0
cas_large_rows=1
large_len=200015
```

Interpretation:

- Exact value compare-and-swap updates one row for current value.
- Stale value compare-and-swap updates zero rows.
- `BINARY value = BINARY ?` stays case-sensitive even under `utf8mb4_general_ci`; `"CASE"` did not match `"case"`.
- 200KB JSON text value comparison works on MySQL 8.4.

## Conclusion

The local code now covers the previously confusing npm version failure path:

- GET uses backend cache or recorded DB data; it does not fetch npm registry. If no record exists, the backend returns a stable error code and the frontend uses built-in fallback options.
- POST refresh is admin-protected, returns structured error codes, and does not pollute cache on persist failure.
- Default and classic Header Profile dialogs expose source, refresh time, cache age and safe last error summaries.
- Default and classic manual version reload handlers guard against post-unmount state updates.
- Classic diagnostics no longer treats Axios internal `ERR_*` transport codes as npm version business codes.
- Diagnostics `recent_errors` is a stable array field, even when the history is empty.
- Diagnostics `cache_age_ms` is never negative, even if a future timestamp is loaded from a skewed clock or test fixture.
- Permission/session failures are distinguishable from npm registry or backend cache failures.
- The rebuilt isolated 3001 environment is running the latest dirty image from this worktree and passes the smoke checks above.
